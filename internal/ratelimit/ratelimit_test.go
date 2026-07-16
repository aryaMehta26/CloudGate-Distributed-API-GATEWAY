package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

func TestTokenBucket_Allow(t *testing.T) {
	rdb := newTestRedis(t)
	// capacity=3, rate=1 token/sec
	tb := NewTokenBucket(rdb, 3, 1.0)
	ctx := context.Background()

	// First 3 requests should succeed
	for i := 0; i < 3; i++ {
		ok, err := tb.Allow(ctx, "tenant:user1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	// 4th request should be denied
	ok, err := tb.Allow(ctx, "tenant:user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("4th request should be rate limited")
	}
}

func TestTokenBucket_Refill(t *testing.T) {
	rdb := newTestRedis(t)
	tb := NewTokenBucket(rdb, 1, 10.0) // 10 tokens/sec = 1 token per 100ms
	ctx := context.Background()

	// Consume the 1 token
	ok, _ := tb.Allow(ctx, "tenant:refill")
	if !ok {
		t.Fatal("first request should succeed")
	}

	// Should be denied
	ok, _ = tb.Allow(ctx, "tenant:refill")
	if ok {
		t.Fatal("should be rate limited after burst")
	}

	// Wait for refill
	time.Sleep(150 * time.Millisecond)

	ok, _ = tb.Allow(ctx, "tenant:refill")
	if !ok {
		t.Fatal("should be allowed after refill")
	}
}

func TestSlidingWindow_Allow(t *testing.T) {
	rdb := newTestRedis(t)
	// 3 requests per 1-second window
	sw := NewSlidingWindow(rdb, 1, 3)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		ok, err := sw.Allow(ctx, "tenant:sw")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	ok, err := sw.Allow(ctx, "tenant:sw")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("4th request should be denied")
	}
}

func TestComposite_BothMustPass(t *testing.T) {
	rdb := newTestRedis(t)
	// Composite: both token bucket (cap=5) and sliding window (3/sec)
	tb := NewTokenBucket(rdb, 5, 5.0)
	sw := NewSlidingWindow(rdb, 1, 3)
	c := NewComposite(tb, sw)
	ctx := context.Background()

	// Only 3 should pass (sliding window is stricter)
	for i := 0; i < 3; i++ {
		ok, _ := c.Allow(ctx, "composite:test")
		if !ok {
			t.Fatalf("request %d should be allowed by composite", i+1)
		}
	}
	ok, _ := c.Allow(ctx, "composite:test")
	if ok {
		t.Fatal("composite should deny after sliding window limit")
	}
}

func BenchmarkTokenBucket(b *testing.B) {
	rdb := newTestRedis(&testing.T{})
	tb := NewTokenBucket(rdb, 1_000_000, 1_000_000)
	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			tb.Allow(ctx, "bench:key") //nolint:errcheck
		}
	})
}
