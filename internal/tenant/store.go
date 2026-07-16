// Package tenant manages per-tenant policy state backed by Redis.
// Policies are hot-reloadable via the gRPC admin service without a gateway restart.
package tenant

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/aryaMehta26/cloudgate/pkg/redis"
	"go.uber.org/zap"
)

const (
	tenantKeyPrefix  = "cg:tenant:"
	tenantIndexKey   = "cg:tenants"
	policyExpiry     = 24 * time.Hour
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
	pipe.Set(ctx, key, data, policyExpiry)
	pipe.SAdd(ctx, tenantIndexKey, p.TenantID)

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
