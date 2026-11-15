// Package ratelimit provides distributed rate limiting using Redis.
// Two strategies are supported:
//   - Token Bucket: smooth burst absorption via an in-memory fast path with Redis sync
//   - Sliding Window: precise request counting via Redis sorted sets and Lua scripts
package ratelimit

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Limiter is the interface implemented by all rate limiters.
type Limiter interface {
	// Allow returns true if the request is permitted, false if rate-limited.
	// key is typically "<tenantID>:<clientIP>" or "<tenantID>:<userID>".
	Allow(ctx context.Context, key string) (bool, error)
}

// ──────────────────────────────────────────────────────────────────────────────
// Token Bucket
// ──────────────────────────────────────────────────────────────────────────────

// tokenBucketScript atomically checks and refills a token bucket stored in Redis.
// KEYS[1] = bucket key
// ARGV[1] = capacity (burst)
// ARGV[2] = refill rate (tokens per second)
// ARGV[3] = current unix timestamp (nanoseconds)
// ARGV[4] = tokens requested (always 1)
// Returns 1 if allowed, 0 if denied.
var tokenBucketScript = goredis.NewScript(`
local key       = KEYS[1]
local capacity  = tonumber(ARGV[1])
local rate      = tonumber(ARGV[2])
local now       = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])

local data = redis.call("HMGET", key, "tokens", "last_refill")
local tokens     = tonumber(data[1]) or capacity
local last_refill = tonumber(data[2]) or now

-- Refill
local elapsed = (now - last_refill) / 1e9   -- convert ns → seconds
local refill  = elapsed * rate
tokens = math.min(capacity, tokens + refill)

if tokens >= requested then
    tokens = tokens - requested
    redis.call("HMSET", key, "tokens", tokens, "last_refill", now)
    redis.call("EXPIRE", key, 60)
    return 1
else
    redis.call("HMSET", key, "tokens", tokens, "last_refill", now)
    redis.call("EXPIRE", key, 60)
    return 0
end
`)

// TokenBucket implements the Limiter interface using Redis-backed token bucket.
type TokenBucket struct {
	rdb      *goredis.Client
	capacity int
	rate     float64 // tokens per second
}

// NewTokenBucket creates a new TokenBucket limiter.
// capacity is the maximum burst size; ratePerSec is the steady-state token refill rate.
func NewTokenBucket(rdb *goredis.Client, capacity int, ratePerSec float64) *TokenBucket {
	return &TokenBucket{
		rdb:      rdb,
		capacity: capacity,
		rate:     ratePerSec,
	}
}

// Allow checks the token bucket for the given key.
func (tb *TokenBucket) Allow(ctx context.Context, key string) (bool, error) {
	now := time.Now().UnixNano()
	redisKey := fmt.Sprintf("cg:tb:%s", key)

	result, err := tokenBucketScript.Run(ctx, tb.rdb,
		[]string{redisKey},
		tb.capacity,
		tb.rate,
		now,
		1,
	).Int()
	if err != nil {
		return false, fmt.Errorf("token bucket redis script: %w", err)
	}

	return result == 1, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Sliding Window
// ──────────────────────────────────────────────────────────────────────────────

// slidingWindowScript atomically adds the current request and counts requests
// within the window using a Redis sorted set.
// KEYS[1] = sorted set key
// ARGV[1] = window start (unix nanoseconds)
// ARGV[2] = current time (unix nanoseconds) – used as score and unique member suffix
// ARGV[3] = max requests in window
// ARGV[4] = window TTL in seconds
// Returns 1 if allowed, 0 if denied.
var slidingWindowScript = goredis.NewScript(`
local key        = KEYS[1]
local win_start  = tonumber(ARGV[1])
local now        = tonumber(ARGV[2])
local max_req    = tonumber(ARGV[3])
local ttl        = tonumber(ARGV[4])

-- Remove entries outside the window
redis.call("ZREMRANGEBYSCORE", key, "-inf", win_start)

local count = redis.call("ZCARD", key)
if count < max_req then
    redis.call("ZADD", key, now, now)
    redis.call("EXPIRE", key, ttl)
    return 1
else
    return 0
end
`)

// SlidingWindow implements the Limiter interface using a Redis sorted set.
type SlidingWindow struct {
	rdb         *goredis.Client
	windowSecs  int
	maxInWindow int
}

// NewSlidingWindow creates a new SlidingWindow limiter.
func NewSlidingWindow(rdb *goredis.Client, windowSecs, maxInWindow int) *SlidingWindow {
	return &SlidingWindow{
		rdb:         rdb,
		windowSecs:  windowSecs,
		maxInWindow: maxInWindow,
	}
}

// Allow checks the sliding window for the given key.
func (sw *SlidingWindow) Allow(ctx context.Context, key string) (bool, error) {
	now := time.Now().UnixNano()
	windowStart := now - int64(sw.windowSecs)*int64(time.Second)
	redisKey := fmt.Sprintf("cg:sw:%s", key)

	result, err := slidingWindowScript.Run(ctx, sw.rdb,
		[]string{redisKey},
		windowStart,
		now,
		sw.maxInWindow,
		sw.windowSecs+1,
	).Int()
	if err != nil {
		return false, fmt.Errorf("sliding window redis script: %w", err)
	}

	return result == 1, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Composite Limiter (AND semantics — both must pass)
// ──────────────────────────────────────────────────────────────────────────────

// Composite combines multiple limiters; a request must pass all of them.
type Composite struct {
	limiters []Limiter
}

// NewComposite creates a Composite limiter from the given limiters.
func NewComposite(limiters ...Limiter) *Composite {
	return &Composite{limiters: limiters}
}

// Allow returns true only if all underlying limiters allow the request.
func (c *Composite) Allow(ctx context.Context, key string) (bool, error) {
	for _, l := range c.limiters {
		ok, err := l.Allow(ctx, key)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}
