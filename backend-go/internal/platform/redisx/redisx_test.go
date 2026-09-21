package redisx

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/shilin414/cas/backend-go/internal/platform/config"
)

func TestNewUniversalClientSelectsStandalone(t *testing.T) {
	cfg := config.RedisConfig{
		Mode:           config.RedisModeStandalone,
		Host:           "127.0.0.1",
		Port:           6381,
		DB:             3,
		Password:       "secret",
		PoolSize:       20,
		MinIdleConns:   5,
		MaxIdleConns:   10,
		ConnectTimeout: 10 * time.Second,
		Timeout:        5 * time.Second,
		PoolTimeout:    3 * time.Second,
	}
	client, err := newUniversalClient(cfg)
	if err != nil {
		t.Fatalf("newUniversalClient: %v", err)
	}
	defer client.Close()

	standalone, ok := client.(*goredis.Client)
	if !ok {
		t.Fatalf("client type = %T, want *redis.Client", client)
	}
	opts := standalone.Options()
	if opts.Addr != "127.0.0.1:6381" || opts.DB != 3 || opts.PoolSize != 20 {
		t.Fatalf("standalone options = %+v", opts)
	}
}

func TestNewUniversalClientSelectsCluster(t *testing.T) {
	cfg := config.RedisConfig{
		Mode:                config.RedisModeCluster,
		ClusterNodes:        []string{"redis-1.example:6379", "redis-2.example:6379"},
		ClusterMaxRedirects: 5,
		Password:            "secret",
		PoolSize:            20,
		MinIdleConns:        5,
		MaxIdleConns:        10,
		ConnectTimeout:      10 * time.Second,
		Timeout:             5 * time.Second,
		PoolTimeout:         3 * time.Second,
	}
	client, err := newUniversalClient(cfg)
	if err != nil {
		t.Fatalf("newUniversalClient: %v", err)
	}
	defer client.Close()

	cluster, ok := client.(*goredis.ClusterClient)
	if !ok {
		t.Fatalf("client type = %T, want *redis.ClusterClient", client)
	}
	opts := cluster.Options()
	if len(opts.Addrs) != 2 || opts.MaxRedirects != 5 || opts.PoolSize != 20 {
		t.Fatalf("cluster options = %+v", opts)
	}
}

func TestKeyWithSlotKeepsRelatedKeysInOneClusterSlot(t *testing.T) {
	client := NewWithPrefix("studio")
	key := client.KeyWithSlot("token-42", "provider", "aily", "uat", "42")
	if key != "studio:{token-42}:provider:aily:uat:42" {
		t.Fatalf("key = %q", key)
	}
	if !strings.Contains(key, "{token-42}") || !strings.Contains(key+":refresh", "{token-42}") {
		t.Fatalf("cache and lease keys lost their common Redis Cluster hash tag")
	}
}

func TestOpenAgainstConfiguredRedis(t *testing.T) {
	if os.Getenv("STUDIO_TEST_REDIS") != "1" {
		t.Skip("set STUDIO_TEST_REDIS=1 to verify the configured Redis")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	client, err := Open(context.Background(), cfg.Redis)
	if err != nil {
		t.Fatalf("open configured Redis: %v", err)
	}
	key := client.Key("itest", "redis-topology", strconv.FormatInt(time.Now().UnixNano(), 10))
	t.Cleanup(func() {
		_ = client.Del(context.Background(), key).Err()
		_ = client.Close()
	})
	if err := client.Set(context.Background(), key, "ok", time.Minute).Err(); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got, err := client.Get(context.Background(), key).Result(); err != nil || got != "ok" {
		t.Fatalf("get = %q, %v", got, err)
	}
}
