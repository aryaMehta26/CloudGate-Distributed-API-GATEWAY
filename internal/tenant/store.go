// Package tenant manages per-tenant policy state backed by Redis.
// Policies are hot-reloadable via the gRPC admin service without a gateway restart.
package tenant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"

	"go.uber.org/zap"

	"github.com/aryaMehta26/cloudgate/pkg/redis"
)

const (
	tenantKeyPrefix = "cg:tenant:"
	tenantIndexKey  = "cg:tenants"
	// UpdatesChannel carries the ID of every tenant whose policy changed, so
	// each gateway replica can drop its cached copy.
	UpdatesChannel = "cg:tenant-updates"
)

// Policy is the runtime policy for a tenant, stored in Redis and
// loaded into memory on first request (then refreshed periodically).
type Policy struct {
	TenantID        string   `json:"tenant_id"`
	Upstreams       []string `json:"upstreams"`
	RPSLimit        int      `json:"rps_limit"`
	Burst           int      `json:"burst"`
	WindowSecs      int      `json:"window_secs"`
	MaxInWindow     int      `json:"max_in_window"`
	BulkheadMax     int      `json:"bulkhead_max"`
	CBFailures      int      `json:"cb_failures"`
	CBOpenTimeoutMs int      `json:"cb_open_timeout_ms"`
	CBSuccesses     int      `json:"cb_successes"`
}

// Validate reports whether the policy can be enforced: it needs a tenant ID,
// at least one absolute http(s) upstream URL, and positive limits.
func (p *Policy) Validate() error {
	if p.TenantID == "" {
		return errors.New("tenant_id is required")
	}
	if len(p.Upstreams) == 0 {
		return errors.New("at least one upstream is required")
	}
	for _, u := range p.Upstreams {
		parsed, err := url.Parse(u)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("upstream %q must be an absolute http(s) URL", u)
		}
	}
	for name, v := range map[string]int{
		"rps_limit": p.RPSLimit, "burst": p.Burst, "window_secs": p.WindowSecs,
		"max_in_window": p.MaxInWindow, "bulkhead_max": p.BulkheadMax,
		"cb_failures": p.CBFailures, "cb_open_timeout_ms": p.CBOpenTimeoutMs, "cb_successes": p.CBSuccesses,
	} {
		if v <= 0 {
			return fmt.Errorf("%s must be positive, got %d", name, v)
		}
	}
	return nil
}

// Store manages tenant policies backed by Redis with an in-memory cache.
type Store struct {
	mu    sync.RWMutex
	cache map[string]*Policy
	rdb   *redis.Client
	log   *zap.Logger
}

// NewRedisStore creates a Store backed by the given Redis client.
func NewRedisStore(rdb *redis.Client, log *zap.Logger) *Store {
	return &Store{
		cache: make(map[string]*Policy),
		rdb:   rdb,
		log:   log,
	}
}

// Get returns the policy for a tenant ID, loading from Redis on cache miss.
func (s *Store) Get(ctx context.Context, tenantID string) (*Policy, error) {
	s.mu.RLock()
	if p, ok := s.cache[tenantID]; ok {
		s.mu.RUnlock()
		return p, nil
	}
	s.mu.RUnlock()

	return s.load(ctx, tenantID)
}

// Set writes a policy to Redis and updates the local cache.
func (s *Store) Set(ctx context.Context, p *Policy) error {
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal policy: %w", err)
	}

	key := tenantKeyPrefix + p.TenantID
	pipe := s.rdb.Client().Pipeline()
	// No expiry: a policy lives until it is replaced or deleted. (An earlier
	// 24h TTL silently dropped tenants that were still listed in the index.)
	pipe.Set(ctx, key, data, 0)
	pipe.SAdd(ctx, tenantIndexKey, p.TenantID)
	pipe.Publish(ctx, UpdatesChannel, p.TenantID)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis pipeline: %w", err)
	}

	s.mu.Lock()
	s.cache[p.TenantID] = p
	s.mu.Unlock()

	s.log.Info("tenant policy updated", zap.String("tenant", p.TenantID))
	return nil
}

// Delete removes a tenant policy from Redis and evicts the cache entry.
func (s *Store) Delete(ctx context.Context, tenantID string) error {
	key := tenantKeyPrefix + tenantID
	pipe := s.rdb.Client().Pipeline()
	pipe.Del(ctx, key)
	pipe.SRem(ctx, tenantIndexKey, tenantID)
	pipe.Publish(ctx, UpdatesChannel, tenantID)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis pipeline: %w", err)
	}

	s.mu.Lock()
	delete(s.cache, tenantID)
	s.mu.Unlock()

	return nil
}

// List returns all known tenant IDs from the Redis index.
func (s *Store) List(ctx context.Context) ([]string, error) {
	return s.rdb.Client().SMembers(ctx, tenantIndexKey).Result()
}

// Invalidate evicts a tenant from the local cache, forcing a Redis reload on next Get.
func (s *Store) Invalidate(tenantID string) {
	s.mu.Lock()
	delete(s.cache, tenantID)
	s.mu.Unlock()
}

// Watch subscribes to policy changes made by any replica. For each changed
// tenant it evicts the local cache entry and then calls onChange. It blocks
// until ctx is canceled. ready, if non-nil, is closed once the subscription
// is active.
func (s *Store) Watch(ctx context.Context, ready chan<- struct{}, onChange func(tenantID string)) error {
	sub := s.rdb.Client().Subscribe(ctx, UpdatesChannel)
	defer sub.Close()
	if _, err := sub.Receive(ctx); err != nil { // wait for the subscription confirmation
		return fmt.Errorf("subscribe %s: %w", UpdatesChannel, err)
	}
	if ready != nil {
		close(ready)
	}
	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			s.Invalidate(msg.Payload)
			if onChange != nil {
				onChange(msg.Payload)
			}
		}
	}
}

// load fetches a policy from Redis and populates the cache.
func (s *Store) load(ctx context.Context, tenantID string) (*Policy, error) {
	key := tenantKeyPrefix + tenantID
	data, err := s.rdb.Client().Get(ctx, key).Bytes()
	if err != nil {
		return nil, fmt.Errorf("tenant %q not found in redis: %w", tenantID, err)
	}

	var p Policy
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("unmarshal policy: %w", err)
	}

	s.mu.Lock()
	s.cache[tenantID] = &p
	s.mu.Unlock()

	return &p, nil
}
