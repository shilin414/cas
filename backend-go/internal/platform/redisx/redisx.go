// Package redisx wraps the shared Redis client.
//
// One logical key namespace is shared by standalone and cluster deployments.
// Redis Cluster has no logical databases, so cluster mode is validated to DB 0.
package redisx

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/shilin414/cas/backend-go/internal/platform/config"
)

// Client is the prefixed Redis client used everywhere. UniversalClient keeps
// application code topology-agnostic while still using the native standalone
// or cluster implementation selected at startup.
type Client struct {
	goredis.UniversalClient
	prefix string
}

func newUniversalClient(cfg config.RedisConfig) (goredis.UniversalClient, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	switch cfg.Mode {
	case config.RedisModeStandalone:
		return goredis.NewClient(&goredis.Options{
			Addr:         cfg.Addr(),
			Password:     cfg.Password,
			DB:           cfg.DB,
			PoolSize:     cfg.PoolSize,
			MinIdleConns: cfg.MinIdleConns,
			MaxIdleConns: cfg.MaxIdleConns,
			DialTimeout:  cfg.ConnectTimeout,
			ReadTimeout:  cfg.Timeout,
			WriteTimeout: cfg.Timeout,
			PoolTimeout:  cfg.PoolTimeout,
		}), nil
	case config.RedisModeCluster:
		return goredis.NewClusterClient(&goredis.ClusterOptions{
			Addrs:        cfg.Addrs(),
			Password:     cfg.Password,
			MaxRedirects: cfg.ClusterMaxRedirects,
			PoolSize:     cfg.PoolSize,
			MinIdleConns: cfg.MinIdleConns,
			MaxIdleConns: cfg.MaxIdleConns,
			DialTimeout:  cfg.ConnectTimeout,
			ReadTimeout:  cfg.Timeout,
			WriteTimeout: cfg.Timeout,
			PoolTimeout:  cfg.PoolTimeout,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported Redis mode %q", cfg.Mode)
	}
}

func Open(ctx context.Context, cfg config.RedisConfig) (*Client, error) {
	client, err := newUniversalClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("create Redis client: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout+time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping Redis %s (%s): %w", cfg.Target(), cfg.Mode, err)
	}
	return &Client{UniversalClient: client, prefix: cfg.KeyPrefix}, nil
}

// NewWithPrefix builds a client wrapper without a connection (tests,
// key-naming assertions only). All other methods would panic on nil.
func NewWithPrefix(prefix string) *Client { return &Client{prefix: prefix} }

// Key builds a namespaced key: prefix:part1:part2.
func (c *Client) Key(parts ...string) string {
	out := c.prefix
	for _, p := range parts {
		out += ":" + p
	}
	return out
}

// KeyWithSlot builds a namespaced key with an explicit Redis Cluster hash tag.
// Related keys (for example a value and its lease) must use the same slot tag
// when they participate in one Lua script or multi-key command.
func (c *Client) KeyWithSlot(slot string, parts ...string) string {
	out := c.prefix + ":{" + slot + "}"
	for _, p := range parts {
		out += ":" + p
	}
	return out
}

// PubSub channel names (must match the reference implementation so mixed
// fleets during cutover stay observable):
//
//	xiaoan3:run:{id}:events
func (c *Client) RunEventsChannel(runID string) string {
	return c.Key("run", runID, "events")
}
