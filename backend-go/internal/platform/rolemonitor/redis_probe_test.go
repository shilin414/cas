package rolemonitor

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

func probeConfig() Config {
	c := configForTest()
	c.Address = "127.0.0.1:9091"
	c.ProbeTimeout = 80 * time.Millisecond
	c.RequestTimeout = 200 * time.Millisecond
	return c
}
func TestRedisProbeDisabledCreatesNothing(t *testing.T) {
	p, err := NewRedisProbe(configForTest(), nil)
	if err != nil || p != nil {
		t.Fatalf("disabled probe allocated: %v", err)
	}
	if err = p.Close(); err != nil {
		t.Fatal(err)
	}
	if p.Ping(context.Background()) == nil {
		t.Fatal("disabled probe must not claim a successful Redis check")
	}
}
func TestRedisProbeClonesStandaloneOptionsAndOwnsOnlyItsClient(t *testing.T) {
	source := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1", Username: "probe-user", Password: "private-value", DB: 3, PoolSize: 7, MaxRetries: 4, ReadTimeout: 5 * time.Second, WriteTimeout: 4 * time.Second, DialTimeout: 3 * time.Second, PoolTimeout: 6 * time.Second, TLSConfig: &tls.Config{ServerName: "private.example", MinVersion: tls.VersionTLS12}})
	defer source.Close()
	original := *source.Options()
	p, err := NewRedisProbe(probeConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	client, ok := p.client.(*goredis.Client)
	if !ok || client == source {
		t.Fatal("business pool was reused")
	}
	o := client.Options()
	if !o.ContextTimeoutEnabled || o.PoolSize != 1 || o.MinIdleConns != 0 || o.MaxActiveConns != 1 || o.MaxRetries != 0 || o.DialerRetries != 1 {
		t.Fatal("unsafe standalone probe options")
	}
	for _, d := range []time.Duration{o.DialTimeout, o.ReadTimeout, o.WriteTimeout, o.PoolTimeout} {
		if d <= 0 || d > p.budget {
			t.Fatal("probe timeout exceeds budget")
		}
	}
	if o.Username != original.Username || o.Password != original.Password || o.DB != original.DB || o.Addr != original.Addr {
		t.Fatal("connection identity lost")
	}
	if o.TLSConfig == original.TLSConfig || o.TLSConfig.ServerName != original.TLSConfig.ServerName {
		t.Fatal("TLS options not independently cloned")
	}
	if source.Options().ReadTimeout != original.ReadTimeout || source.Options().ContextTimeoutEnabled || source.Options().PoolSize != 7 {
		t.Fatal("business options modified")
	}
	if source.PoolStats().TotalConns != 0 || client.PoolStats().TotalConns != 0 {
		t.Fatal("construction dialed Redis")
	}
	if err = p.Close(); err != nil {
		t.Fatal(err)
	}
	if err = p.Close(); err != nil {
		t.Fatal("close is not idempotent")
	}
	if !errors.Is(client.Ping(context.Background()).Err(), goredis.ErrClosed) {
		t.Fatal("probe client left open")
	}
	if p.Ping(context.Background()) == nil {
		t.Fatal("closed probe returned healthy")
	}
	if err = source.Close(); err != nil {
		t.Fatal("probe closed business client")
	}
}
func TestRedisProbeClonesClusterOptions(t *testing.T) {
	source := goredis.NewClusterClient(&goredis.ClusterOptions{Addrs: []string{"unused.invalid:1"}, Username: "u", Password: "private-value", PoolSize: 9, MaxRetries: 4, MaxRedirects: 6, ReadTimeout: 5 * time.Second, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}})
	defer source.Close()
	p, err := NewRedisProbe(probeConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	client, ok := p.client.(*goredis.ClusterClient)
	if !ok || client == source {
		t.Fatal("cluster pool reused")
	}
	o := client.Options()
	if !o.ContextTimeoutEnabled || o.PoolSize != 1 || o.MinIdleConns != 0 || o.MaxActiveConns != 1 || o.MaxRetries != -1 || o.MaxRedirects != 0 || o.DialerRetries != 1 {
		t.Fatal("unsafe cluster probe options")
	}
	for _, d := range []time.Duration{o.DialTimeout, o.ReadTimeout, o.WriteTimeout, o.PoolTimeout} {
		if d <= 0 || d > p.budget {
			t.Fatal("cluster budget exceeded")
		}
	}
	if o.Password != source.Options().Password || o.Username != source.Options().Username || o.TLSConfig == source.Options().TLSConfig {
		t.Fatal("cluster identity/TLS clone failed")
	}
	o.Addrs[0] = "changed.invalid:1"
	if source.Options().Addrs[0] != "unused.invalid:1" {
		t.Fatal("shared cluster address slice")
	}
	if source.Options().ReadTimeout != 5*time.Second || source.Options().PoolSize != 9 || source.Options().ContextTimeoutEnabled {
		t.Fatal("business cluster modified")
	}
	if err = p.Close(); err != nil {
		t.Fatal(err)
	}
	if err = source.Close(); err != nil {
		t.Fatal("source was closed")
	}
}

type unsupportedRedisProbeSource struct{ goredis.UniversalClient }

func TestRedisProbeRejectsMissingAndUnsupportedSource(t *testing.T) {
	for _, source := range []goredis.UniversalClient{nil, (*goredis.Client)(nil), (*goredis.ClusterClient)(nil), &unsupportedRedisProbeSource{}} {
		if _, err := NewRedisProbe(probeConfig(), source); err == nil {
			t.Fatal("unsupported source accepted")
		}
	}
	c := probeConfig()
	c.ProbeTimeout = 0
	if _, err := NewRedisProbe(c, nil); err == nil {
		t.Fatal("zero probe budget accepted")
	}
}
func TestRedisProbeBlackholeConnectionBudgetAndLifecycle(t *testing.T) {
	for _, cluster := range []bool{false, true} {
		t.Run(map[bool]string{false: "standalone", true: "cluster"}[cluster], func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var connections []net.Conn
			var workers sync.WaitGroup
			var accepted atomic.Int32
			acceptDone := make(chan struct{})
			closed := make(chan struct{}, 16)
			go func() {
				defer close(acceptDone)
				for {
					conn, e := ln.Accept()
					if e != nil {
						return
					}
					accepted.Add(1)
					mu.Lock()
					connections = append(connections, conn)
					mu.Unlock()
					workers.Add(1)
					go func() {
						defer workers.Done()
						defer conn.Close()
						_, _ = io.Copy(io.Discard, conn)
						closed <- struct{}{}
					}()
				}
			}()
			defer func() {
				ln.Close()
				<-acceptDone
				mu.Lock()
				for _, c := range connections {
					c.Close()
				}
				mu.Unlock()
				workers.Wait()
			}()
			var source goredis.UniversalClient
			var businessDials atomic.Int32
			businessDialer := func(context.Context, string, string) (net.Conn, error) {
				businessDials.Add(1)
				return nil, errors.New("business dialing forbidden in probe test")
			}
			if cluster {
				source = goredis.NewClusterClient(&goredis.ClusterOptions{Addrs: []string{ln.Addr().String()}, ReadTimeout: 5 * time.Second, Dialer: businessDialer})
			} else {
				source = goredis.NewClient(&goredis.Options{Addr: ln.Addr().String(), ReadTimeout: 5 * time.Second, Dialer: businessDialer})
			}
			defer source.Close()
			p, err := NewRedisProbe(probeConfig(), source)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			start := time.Now()
			err = p.Ping(context.Background())
			elapsed := time.Since(start)
			t.Logf("blackhole probe elapsed=%s, configured budget=%s", elapsed, p.budget)
			if err == nil || elapsed > 350*time.Millisecond {
				t.Fatalf("blackhole budget not honored: elapsed=%s err=%v", elapsed, err)
			}
			if accepted.Load() == 0 {
				t.Fatal("test never exercised a real connection")
			}
			select {
			case <-closed:
			case <-time.After(350 * time.Millisecond):
				t.Fatal("probe retained blackhole connection after failure")
			}
			// ClusterClient.PoolStats is NOT read-only memory: it lazily fetches
			// cluster state with context.TODO. Never call it on the business client.
			if businessDials.Load() != 0 {
				t.Fatal("probe used the business dialer/pool")
			}
		})
	}
}

func TestRedisProbePreservesShortTimeoutAndClosesLateDial(t *testing.T) {
	if probeTimeout(10*time.Millisecond, time.Second) != 10*time.Millisecond {
		t.Fatal("shorter existing timeout was extended")
	}
	tracker := &probeSockets{connections: make(map[*probeConn]struct{})}
	tracker.closeAll()
	left, right := net.Pipe()
	defer right.Close()
	if _, err := tracker.track(left); !errors.Is(err, goredis.ErrClosed) {
		t.Fatal("late dial accepted after close")
	}
	if _, err := right.Write([]byte("x")); err == nil {
		t.Fatal("late transport was not closed")
	}
}
