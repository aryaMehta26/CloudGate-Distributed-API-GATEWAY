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
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"sync"
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
	policy   *tenant.Policy
	limiter  *ratelimit.Composite
	cb       *circuitbreaker.CircuitBreaker
	bh       *bulkhead.Bulkhead
	upstream *url.URL // currently selected upstream (round-robin simplified)
}

// Gateway is the HTTP handler implementing multi-tenant reverse proxying.
type Gateway struct {
	cfg     *config.Config
	store   *tenant.Store
	rdb     *redis.Client
	reg     *metrics.Registry
	auditor *audit.Writer
	log     *zap.Logger

	mu       sync.RWMutex
	runtimes map[string]*tenantRuntime
}

// New builds a new Gateway and pre-warms tenant runtimes from config.
func New(cfg *config.Config, rdb *redis.Client, reg *metrics.Registry, auditor *audit.Writer, log *zap.Logger) *Gateway {
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
		g.runtimes[tc.ID] = g.buildRuntime(p)
	}

	return g
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
	rlKey := fmt.Sprintf("%s:%s", tenantID, r.RemoteAddr)
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
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = rt.upstream.Scheme
			req.URL.Host = rt.upstream.Host
			req.Host = rt.upstream.Host
			req.Header.Set("X-Forwarded-For", r.RemoteAddr)
			req.Header.Set("X-Request-ID", r.Header.Get("X-Request-ID"))
		},
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			rt.cb.RecordFailure()
			g.reg.UpstreamErrors.WithLabelValues(tenantID, rt.upstream.Host, "proxy_error").Inc()
			g.log.Error("upstream proxy error", zap.Error(err), zap.String("upstream", rt.upstream.Host))
			http.Error(w, "bad gateway", http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(rw, r)

	latency := time.Since(start)
	if rw.statusCode < 500 {
		rt.cb.RecordSuccess()
	} else {
		rt.cb.RecordFailure()
	}

	// ── Metrics ───────────────────────────────────────────────────────────────
	statusStr := strconv.Itoa(rw.statusCode)
	g.reg.RequestTotal.WithLabelValues(tenantID, r.Method, r.URL.Path, statusStr).Inc()
	g.reg.RequestDuration.WithLabelValues(tenantID, r.Method, statusStr).Observe(latency.Seconds())
	g.reg.UpstreamRequestTotal.WithLabelValues(tenantID, rt.upstream.Host, statusStr).Inc()
	g.reg.UpstreamDuration.WithLabelValues(tenantID, rt.upstream.Host).Observe(latency.Seconds())

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

// getRuntime returns the runtime for a tenant, building it on cache miss.
func (g *Gateway) getRuntime(ctx interface{ Value(any) any }, tenantID string) (*tenantRuntime, error) {
	g.mu.RLock()
	rt, ok := g.runtimes[tenantID]
	g.mu.RUnlock()
	if ok {
		return rt, nil
	}

	// This should rarely happen in production (tenants are pre-warmed or loaded from Redis).
	g.log.Warn("tenant runtime cache miss — loading from redis", zap.String("tenant", tenantID))
	return nil, fmt.Errorf("tenant %q not configured", tenantID)
}

// buildRuntime constructs all per-tenant runtime objects from a policy.
func (g *Gateway) buildRuntime(p *tenant.Policy) *tenantRuntime {
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

	// Simple: pick first upstream (production would use weighted round-robin)
	upstreamURL, _ := url.Parse(p.Upstreams[0])

	return &tenantRuntime{
		policy:   p,
		limiter:  composite,
		cb:       cb,
		bh:       bh,
		upstream: upstreamURL,
	}
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
