package aily

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shilin414/cas/backend-go/internal/platform/config"
	"github.com/shilin414/cas/backend-go/internal/platform/ids"
	"github.com/shilin414/cas/backend-go/internal/platform/redisx"
)

// Run only against an explicitly opted-in Redis. CI provides an ephemeral Redis;
// the unique prefix and exact-key cleanup also avoid touching unrelated data.
func authLiveRedis(t *testing.T) *redisx.Client {
	t.Helper()
	if os.Getenv("STUDIO_TEST_REDIS") != "1" {
		t.Skip("set STUDIO_TEST_REDIS=1 to verify auth lease Lua against real Redis")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Redis.KeyPrefix = "itest-auth-" + ids.New().Hex()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := redisx.Open(ctx, cfg.Redis)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		key := (&AuthResolver{Redis: client}).tatKey("exam")
		if err := client.Del(cleanup, key, key+":refresh").Err(); err != nil {
			t.Errorf("cleanup auth fixture: %v", err)
		}
		_ = client.Close()
	})
	return client
}

func TestAuthRedisConcurrentResolversPublishOnce(t *testing.T) {
	rdb := authLiveRedis(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var calls atomic.Int32
	api := authTokenAPIStub{tenant: func(context.Context) (*TokenResult, error) {
		calls.Add(1)
		return &TokenResult{AccessToken: "fixture-token", ExpiresIn: 3600}, nil
	}}
	start := make(chan struct{})
	results := make(chan error, 16)
	for range 16 {
		go func() {
			<-start
			r := &AuthResolver{Redis: rdb, Feishu: api}
			token, err := r.TenantAccessToken(ctx, "exam")
			if err == nil && token != "fixture-token" {
				err = fmt.Errorf("unexpected token result")
			}
			results <- err
		}()
	}
	close(start)
	for range 16 {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream token requests=%d, want 1", calls.Load())
	}
	key := (&AuthResolver{Redis: rdb}).tatKey("exam")
	if n, err := rdb.Exists(ctx, key+":refresh").Result(); err != nil || n != 0 {
		t.Fatalf("lease not released: count=%d err=%v", n, err)
	}
	if ttl, err := rdb.TTL(ctx, key).Result(); err != nil || ttl <= 0 || ttl > time.Hour {
		t.Fatalf("invalid token cache TTL: %v err=%v", ttl, err)
	}
}

func TestAuthRedisLostLeaseCannotPublishOrUnlockSuccessor(t *testing.T) {
	rdb := authLiveRedis(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	key := (&AuthResolver{Redis: rdb}).tatKey("exam")
	api := authTokenAPIStub{tenant: func(ctx context.Context) (*TokenResult, error) {
		if err := rdb.Set(ctx, key+":refresh", "successor", time.Minute).Err(); err != nil {
			return nil, err
		}
		return &TokenResult{AccessToken: "must-not-publish", ExpiresIn: 3600}, nil
	}}
	r := &AuthResolver{Redis: rdb, Feishu: api}
	token, err := r.TenantAccessToken(ctx, "exam")
	if err == nil || !strings.Contains(err.Error(), "lease lost") || token != "" {
		t.Fatalf("stale owner returned success: token=%q err=%v", token, err)
	}
	if value, err := rdb.Get(ctx, key+":refresh").Result(); err != nil || value != "successor" {
		t.Fatalf("successor lease removed: %q %v", value, err)
	}
	if n, err := rdb.Exists(ctx, key).Result(); err != nil || n != 0 {
		t.Fatalf("stale owner cached token: count=%d err=%v", n, err)
	}
}

func TestAuthRedisWaitingCallerCanCancel(t *testing.T) {
	rdb := authLiveRedis(t)
	key := (&AuthResolver{Redis: rdb}).tatKey("exam")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Set(ctx, key+":refresh", "busy-owner", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	wait, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stop()
	r := &AuthResolver{Redis: rdb}
	token, err := r.TenantAccessToken(wait, "exam")
	if !errors.Is(err, context.DeadlineExceeded) || token != "" {
		t.Fatalf("waiting caller did not cancel: %v", err)
	}
	if owner, err := rdb.Get(ctx, key+":refresh").Result(); err != nil || owner != "busy-owner" {
		t.Fatalf("waiter changed current owner: %q %v", owner, err)
	}
}
