// Package router implements the multi-tenant reverse proxy.
// For each incoming request it:
//  1. Extracts the tenant ID from the X-Tenant-ID header
//  2. Loads the tenant policy from the store
//  3. Enforces bulkhead concurrency limit
//  4. Enforces rate limits (token bucket + sliding window)
//  5. Checks the circuit breaker for the selected upstream
//  6. Forwards the request via httputil.ReverseProxy
//  7. Records the audit event
package router

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aryaMehta26/cloudgate/internal/audit"
	"github.com/aryaMehta26/cloudgate/internal/bulkhead"
	"github.com/aryaMehta26/cloudgate/internal/circuitbreaker"
	"github.com/aryaMehta26/cloudgate/internal/config"
	"github.com/aryaMehta26/cloudgate/internal/metrics"
	"github.com/aryaMehta26/cloudgate/internal/ratelimit"
	"github.com/aryaMehta26/cloudgate/internal/tenant"
	"github.com/aryaMehta26/cloudgate/pkg/redis"
	"go.uber.org/zap"
)

// TenantHeader is the HTTP header used to identify the tenant.
const TenantHeader = "X-Tenant-ID"

// tenantRuntime holds the per-tenant runtime objects (limiters, CB, bulkhead).
type tenantRuntime struct {
	policy    *tenant.Policy
	limiter   *ratelimit.Composite
	cb        *circuitbreaker.CircuitBreaker
	bh        *bulkhead.Bulkhead
	upstreams []upstreamTarget
	next      atomic.Uint64 // round-robin cursor
}

type upstreamTarget struct {
	url   *url.URL
	proxy *httputil.ReverseProxy
}

// pick returns the next upstream in round-robin order.
func (rt *tenantRuntime) pick() upstreamTarget {
	return rt.upstreams[(rt.next.Add(1)-1)%uint64(len(rt.upstreams))]
}

// AuditLogger receives one event per proxied request. *audit.Writer
// implements it; tests substitute an in-memory recorder.
type AuditLogger interface {
	Log(audit.Event)
}

// Gateway is the HTTP handler implementing multi-tenant reverse proxying.
type Gateway struct {
	cfg     *config.Config
	store   *tenant.Store
	rdb     *redis.Client
	reg     *metrics.Registry
	auditor AuditLogger
	log     *zap.Logger

	mu       sync.RWMutex
	runtimes map[string]*tenantRuntime
}

// New builds a new Gateway and pre-warms tenant runtimes from config. It
// fails if any configured tenant has an invalid policy.
func New(cfg *config.Config, rdb *redis.Client, reg *metrics.Registry, auditor AuditLogger, log *zap.Logger) (*Gateway, error) {
	store := tenant.NewRedisStore(rdb, log)
	g := &Gateway{
		cfg:      cfg,
		store:    store,
		rdb:      rdb,
		reg:      reg,
		auditor:  auditor,
		log:      log,
		runtimes: make(map[string]*tenantRuntime),
	}

	// Pre-warm from static config
	for _, tc := range cfg.Tenants {
		p := &tenant.Policy{
			TenantID:        tc.ID,
			Upstreams:       tc.Upstreams,
			RPSLimit:        tc.RateLimit.RequestsPerSecond,
			Burst:           tc.RateLimit.Burst,
			WindowSecs:      tc.RateLimit.WindowSeconds,
			MaxInWindow:     tc.RateLimit.MaxInWindow,
			BulkheadMax:     tc.BulkheadMax,
			CBFailures:      tc.CircuitBreaker.FailureThreshold,
			CBOpenTimeoutMs: int(tc.CircuitBreaker.OpenTimeout.Milliseconds()),
			CBSuccesses:     tc.CircuitBreaker.SuccessThreshold,
		}
		rt, err := g.buildRuntime(p)
		if err != nil {
			return nil, fmt.Errorf("tenant %q: %w", tc.ID, err)
		}
		g.runtimes[tc.ID] = rt
	}

	return g, nil
}

// ServeHTTP is the core request handling loop.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	tenantID := r.Header.Get(TenantHeader)
	if tenantID == "" {
		http.Error(w, "missing X-Tenant-ID header", http.StatusBadRequest)
		return
	}

	rt, err := g.getRuntime(r.Context(), tenantID)
	if err != nil {
		g.log.Warn("unknown tenant", zap.String("tenant", tenantID), zap.Error(err))
		http.Error(w, "unknown tenant", http.StatusUnauthorized)
		return
	}

	// ── Bulkhead ──────────────────────────────────────────────────────────────
	release, err := rt.bh.Acquire(r.Context())
	if err != nil {
		g.reg.BulkheadRejectedTotal.WithLabelValues(tenantID).Inc()
		http.Error(w, "too many concurrent requests", http.StatusServiceUnavailable)
		return
	}
	defer release()
	g.reg.BulkheadCurrent.WithLabelValues(tenantID).Set(float64(rt.bh.Current()))

	// ── Rate limiting ─────────────────────────────────────────────────────────
	rlKey := tenantID + ":" + clientIP(r)
	allowed, err := rt.limiter.Allow(r.Context(), rlKey)
	if err != nil {
		g.log.Error("rate limiter error", zap.Error(err))
	}
	if !allowed {
		decision := "denied"
		g.reg.RateLimitTotal.WithLabelValues(tenantID, "composite", decision).Inc()
		w.Header().Set("Retry-After", "1")
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	g.reg.RateLimitTotal.WithLabelValues(tenantID, "composite", "allowed").Inc()

	// ── Circuit breaker ───────────────────────────────────────────────────────
	if err := rt.cb.Allow(); err != nil {
		http.Error(w, "upstream unavailable (circuit open)", http.StatusBadGateway)
		return
	}

	// ── Reverse proxy ─────────────────────────────────────────────────────────
	rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
	target := rt.pick()
	target.proxy.ServeHTTP(rw, r)

	latency := time.Since(start)
	// Exactly one circuit-breaker outcome per request. Proxy errors reach here
	// as a 502 from ErrorHandler; recording them there too counted every
	// connection failure twice and opened the circuit at half the threshold.
	if rw.statusCode < 500 {
		rt.cb.RecordSuccess()
	} else {
		rt.cb.RecordFailure()
	}

	// ── Metrics ───────────────────────────────────────────────────────────────
	statusStr := strconv.Itoa(rw.statusCode)
	g.reg.RequestTotal.WithLabelValues(tenantID, r.Method, statusStr).Inc()
	g.reg.RequestDuration.WithLabelValues(tenantID, r.Method, statusStr).Observe(latency.Seconds())
	g.reg.UpstreamRequestTotal.WithLabelValues(tenantID, target.url.Host, statusStr).Inc()
	g.reg.UpstreamDuration.WithLabelValues(tenantID, target.url.Host).Observe(latency.Seconds())

	// ── Audit ─────────────────────────────────────────────────────────────────
	g.auditor.Log(audit.Event{
		TenantID:  tenantID,
		RequestID: r.Header.Get("X-Request-ID"),
		Timestamp: start,
		Method:    r.Method,
		Path:      r.URL.Path,
		Status:    rw.statusCode,
		LatencyMs: latency.Milliseconds(),
		ClientIP:  r.RemoteAddr,
	})
}

// WatchPolicyUpdates applies policy changes published by any replica's admin
// API: the tenant's runtime is dropped and rebuilt from Redis on its next
// request. It blocks until ctx is cancelled; run it in a goroutine.
func (g *Gateway) WatchPolicyUpdates(ctx context.Context, ready chan<- struct{}) error {
	return g.store.Watch(ctx, ready, func(tenantID string) {
		g.mu.Lock()
		delete(g.runtimes, tenantID)
		g.mu.Unlock()
		g.log.Info("tenant policy changed; runtime will reload", zap.String("tenant", tenantID))
	})
}

// getRuntime returns the runtime for a tenant. On a miss it loads the policy
// from the Redis-backed store, so tenants created through the admin API serve
// traffic without a restart.
func (g *Gateway) getRuntime(ctx context.Context, tenantID string) (*tenantRuntime, error) {
	g.mu.RLock()
	rt, ok := g.runtimes[tenantID]
	g.mu.RUnlock()
	if ok {
		return rt, nil
	}

	p, err := g.store.Get(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	built, err := g.buildRuntime(p)
	if err != nil {
		return nil, fmt.Errorf("tenant %q has an invalid policy: %w", tenantID, err)
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if rt, ok := g.runtimes[tenantID]; ok { // another request won the race
		return rt, nil
	}
	g.runtimes[tenantID] = built
	g.log.Info("loaded tenant policy from redis", zap.String("tenant", tenantID))
	return built, nil
}

// buildRuntime constructs all per-tenant runtime objects from a validated policy.
func (g *Gateway) buildRuntime(p *tenant.Policy) (*tenantRuntime, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	rdbClient := g.rdb.Client()

	tb := ratelimit.NewTokenBucket(rdbClient, p.Burst, float64(p.RPSLimit))
	sw := ratelimit.NewSlidingWindow(rdbClient, p.WindowSecs, p.MaxInWindow)
	composite := ratelimit.NewComposite(tb, sw)

	cb := circuitbreaker.New(
		p.TenantID,
		p.CBFailures,
		time.Duration(p.CBOpenTimeoutMs)*time.Millisecond,
		p.CBSuccesses,
	).WithStateChangeCallback(func(name string, from, to circuitbreaker.State) {
		g.reg.CircuitBreakerTransitions.WithLabelValues(name, from.String(), to.String()).Inc()
		g.reg.CircuitBreakerState.WithLabelValues(name).Set(float64(to))
	})

	bh := bulkhead.New(p.BulkheadMax)

	// Requests rotate across every upstream; previously only Upstreams[0] was used.
	targets := make([]upstreamTarget, len(p.Upstreams))
	for i, raw := range p.Upstreams {
		u, _ := url.Parse(raw) // checked by Validate
		targets[i] = upstreamTarget{url: u, proxy: g.newProxy(p.TenantID, u)}
	}

	return &tenantRuntime{
		policy:    p,
		limiter:   composite,
		cb:        cb,
		bh:        bh,
		upstreams: targets,
	}, nil
}

// newProxy builds one reverse proxy per tenant upstream, reused by every
// request; building it per request allocated a proxy and two closures each
// time. The incoming request's headers, including X-Request-ID, are forwarded
// as-is, and ReverseProxy appends the client IP to X-Forwarded-For.
func (g *Gateway) newProxy(tenantID string, target *url.URL) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
		},
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			g.reg.UpstreamErrors.WithLabelValues(tenantID, target.Host, "proxy_error").Inc()
			g.log.Error("upstream proxy error", zap.Error(err), zap.String("upstream", target.Host))
			http.Error(w, "bad gateway", http.StatusBadGateway)
		},
	}
}

// clientIP returns the caller's IP without the port. Keying limits on
// RemoteAddr ("ip:port") gave every new TCP connection a fresh bucket, so a
// client could bypass rate limits just by opening more connections.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
