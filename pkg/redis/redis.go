// Package redis provides a thin wrapper around go-redis/v9 with connection pooling.
package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/aryaMehta26/cloudgate/internal/config"
	goredis "github.com/redis/go-redis/v9"
)

// Client wraps the go-redis client.
type Client struct {
	rdb *goredis.Client
}

// New creates a new Redis client from the provided config.
// It verifies connectivity with a PING before returning.
func New(cfg config.RedisConfig) (*Client, error) {
	if len(cfg.Addrs) == 0 || cfg.Addrs[0] == "" {
		return nil, fmt.Errorf("redis: at least one address is required")
	}
	addr := cfg.Addrs[0] // single-node for local; swap for cluster in prod
	rdb := goredis.NewClient(&goredis.Options{
		Addr:            addr,
		Password:        cfg.Password,
		DB:              cfg.DB,
		MaxRetries:      3,
		MinRetryBackoff: 8 * time.Millisecond,
		MaxRetryBackoff: 512 * time.Millisecond,
		DialTimeout:     5 * time.Second,
		ReadTimeout:     3 * time.Second,
		WriteTimeout:    3 * time.Second,
		PoolSize:        50,
		MinIdleConns:    10,
		ConnMaxLifetime: 30 * time.Minute,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return &Client{rdb: rdb}, nil
}

// Wrap adapts an existing go-redis client, e.g. one pointed at miniredis in tests.
func Wrap(rdb *goredis.Client) *Client {
	return &Client{rdb: rdb}
}

// Client returns the underlying go-redis client.
func (c *Client) Client() *goredis.Client {
	return c.rdb
}

// Close gracefully closes the Redis connection pool.
func (c *Client) Close() error {
	return c.rdb.Close()
}

// Ping checks connectivity.
func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}
