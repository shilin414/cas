package rolemonitor

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/maintnotifications"
)

// RedisProbe owns a dedicated, lazy-connecting client. It never uses or closes
// the business pool. Options (including credentials) stay in memory only.
type RedisProbe struct {
	mu      sync.Mutex
	client  goredis.UniversalClient
	create  func() (goredis.UniversalClient, *probeSockets)
	sockets *probeSockets
	budget  time.Duration
	closed  bool
}

// NewRedisProbe returns nil when monitoring is disabled, without inspecting or
// constructing a client. Only the standalone/cluster clients used by redisx are
// supported; arbitrary wrappers must not silently fall back to the business pool.
func NewRedisProbe(cfg Config, source goredis.UniversalClient) (*RedisProbe, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if cfg.Address == "" {
		return nil, nil
	}
	p := &RedisProbe{budget: cfg.ProbeTimeout}
	switch source := source.(type) {
	case *goredis.Client:
		if source == nil {
			return nil, errors.New("Redis probe requires a source client")
		}
		options := *source.Options()
		if options.TLSConfig != nil {
			options.TLSConfig = options.TLSConfig.Clone()
		}
		options.ContextTimeoutEnabled = true
		options.DialTimeout = probeTimeout(options.DialTimeout, p.budget)
		options.ReadTimeout = probeTimeout(options.ReadTimeout, p.budget)
		options.WriteTimeout = probeTimeout(options.WriteTimeout, p.budget)
		options.PoolTimeout = probeTimeout(options.PoolTimeout, p.budget)
		options.PoolSize, options.MinIdleConns, options.MaxIdleConns, options.MaxActiveConns, options.MaxConcurrentDials = 1, 0, 1, 1, 1
		options.MaxRetries, options.MinRetryBackoff, options.MaxRetryBackoff = -1, -1, -1
		// In this pinned go-redis version DialerRetries is total attempts; <=0
		// restores five attempts, so 1 (not -1) disables dial retries.
		options.DialerRetries = 1
		options.DialerRetryBackoff = nil
		// Options().Dialer is already initialized and closes over the ORIGINAL
		// options. Rebuild it or the clone could still use the business dial budget.
		options.OnConnect, options.Limiter = nil, nil
		options.ClientSideCache, options.ClientSideCacheConfig = nil, nil
		options.PushNotificationProcessor = nil
		options.MaintNotificationsConfig = &maintnotifications.Config{Mode: maintnotifications.ModeDisabled}
		options.PipelineReadBufferSize, options.PipelineWriteBufferSize, options.PipelinePoolSize = 0, 0, 0
		options.DisableIdentity = true
		p.create = func() (goredis.UniversalClient, *probeSockets) {
			copy := options
			sockets := &probeSockets{connections: make(map[*probeConn]struct{})}
			copy.Dialer = probeDialer(copy.DialTimeout, copy.TLSConfig, sockets)
			return goredis.NewClient(&copy), sockets
		}
	case *goredis.ClusterClient:
		if source == nil {
			return nil, errors.New("Redis probe requires a source client")
		}
		options := *source.Options()
		options.Addrs = append([]string(nil), options.Addrs...)
		if options.TLSConfig != nil {
			options.TLSConfig = options.TLSConfig.Clone()
		}
		options.ContextTimeoutEnabled = true
		options.DialTimeout = probeTimeout(options.DialTimeout, p.budget)
		options.ReadTimeout = probeTimeout(options.ReadTimeout, p.budget)
		options.WriteTimeout = probeTimeout(options.WriteTimeout, p.budget)
		options.PoolTimeout = probeTimeout(options.PoolTimeout, p.budget)
		// Cluster pool limits apply per node, not globally. The monitor gate admits
		// one PING at a time; neither node pools nor hooks are shared with business.
		options.PoolSize, options.MinIdleConns, options.MaxIdleConns, options.MaxActiveConns, options.MaxConcurrentDials = 1, 0, 1, 1, 1
		options.MaxRetries, options.MaxRedirects = -1, -1
		options.MinRetryBackoff, options.MaxRetryBackoff = -1, -1
		options.DialerRetries = 1
		options.DialerRetryBackoff = nil
		options.OnConnect, options.NewClient, options.ClusterSlots = nil, nil, nil
		options.RouteByLatency, options.RouteRandomly = false, false
		options.ShardPicker = nil
		options.PushNotificationProcessor = nil
		options.MaintNotificationsConfig = &maintnotifications.Config{Mode: maintnotifications.ModeDisabled}
		options.PipelineReadBufferSize, options.PipelineWriteBufferSize, options.PipelinePoolSize = 0, 0, 0
		options.DisableIdentity = true
		p.create = func() (goredis.UniversalClient, *probeSockets) {
			copy := options
			copy.Addrs = append([]string(nil), options.Addrs...)
			sockets := &probeSockets{connections: make(map[*probeConn]struct{})}
			copy.Dialer = probeDialer(copy.DialTimeout, copy.TLSConfig, sockets)
			return goredis.NewClusterClient(&copy), sockets
		}
	default:
		return nil, errors.New("Redis probe requires a standalone or cluster client")
	}
	p.client, p.sockets = p.create()
	return p, nil
}
func probeTimeout(value, budget time.Duration) time.Duration {
	if value <= 0 || value > budget {
		return budget
	}
	return value
}
func probeDialer(timeout time.Duration, tlsConfig *tls.Config, sockets *probeSockets) func(context.Context, string, string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		var conn net.Conn
		var err error
		if tlsConfig != nil {
			conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsConfig}).DialContext(ctx, network, addr)
		} else {
			conn, err = dialer.DialContext(ctx, network, addr)
		}
		if err != nil {
			return nil, err
		}
		return sockets.track(conn)
	}
}
func (p *RedisProbe) Ping(ctx context.Context) error {
	if p == nil {
		return errors.New("Redis probe is disabled")
	}
	ctx, cancel := context.WithTimeout(ctx, p.budget)
	defer cancel()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errors.New("Redis probe is closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.client == nil {
		p.client, p.sockets = p.create()
	}
	err := p.client.Ping(ctx).Err()
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		// Close failed probes, including any library background pool redial/state
		// reload work. The next HTTP probe can create a fresh isolated client.
		_ = p.client.Close()
		p.sockets.closeAll()
		p.client = nil
	}
	return err
}
func (p *RedisProbe) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	if p.client != nil {
		err := p.client.Close()
		p.sockets.closeAll()
		p.client = nil
		return err
	}
	return nil
}

// Own the physical sockets too: a failed handshake in the pinned Redis library
// can mark its pool connection closed before actually closing the transport.
// This tracker also closes late background dials from an already closed client.
type probeSockets struct {
	mu          sync.Mutex
	connections map[*probeConn]struct{}
	closed      bool
}
type probeConn struct {
	net.Conn
	owner *probeSockets
	once  sync.Once
	err   error
}

func (s *probeSockets) track(conn net.Conn) (net.Conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		_ = conn.Close()
		return nil, goredis.ErrClosed
	}
	c := &probeConn{Conn: conn, owner: s}
	s.connections[c] = struct{}{}
	return c, nil
}
func (c *probeConn) Close() error {
	c.once.Do(func() { c.err = c.Conn.Close(); c.owner.mu.Lock(); delete(c.owner.connections, c); c.owner.mu.Unlock() })
	return c.err
}
func (s *probeSockets) closeAll() {
	s.mu.Lock()
	s.closed = true
	connections := make([]*probeConn, 0, len(s.connections))
	for conn := range s.connections {
		connections = append(connections, conn)
	}
	s.mu.Unlock()
	for _, conn := range connections {
		_ = conn.Close()
	}
}
