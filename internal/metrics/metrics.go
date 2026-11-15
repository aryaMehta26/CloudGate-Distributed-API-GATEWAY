// Package metrics registers all CloudGate Prometheus metrics.
// All metrics are registered on a dedicated non-global registry
// to avoid conflicts in tests.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry holds all CloudGate Prometheus metrics.
type Registry struct {
	reg *prometheus.Registry

	// ── HTTP gateway metrics ─────────────────────────────────────────────────

	// RequestTotal counts all requests processed by the gateway.
	RequestTotal *prometheus.CounterVec

	// RequestDuration tracks HTTP request latency in seconds.
	RequestDuration *prometheus.HistogramVec

	// RequestInFlight tracks active in-flight requests.
	RequestInFlight *prometheus.GaugeVec

	// ResponseSize tracks response body sizes in bytes.
	ResponseSize *prometheus.HistogramVec

	// ── Rate limiting metrics ────────────────────────────────────────────────

	// RateLimitTotal counts rate-limit decisions.
	RateLimitTotal *prometheus.CounterVec

	// ── Circuit breaker metrics ──────────────────────────────────────────────

	// CircuitBreakerState tracks circuit breaker state (0=closed, 1=open, 2=half-open).
	CircuitBreakerState *prometheus.GaugeVec

	// CircuitBreakerTransitions counts circuit breaker state transitions.
	CircuitBreakerTransitions *prometheus.CounterVec

	// ── Bulkhead metrics ─────────────────────────────────────────────────────

	// BulkheadRejectedTotal counts requests rejected by the bulkhead.
	BulkheadRejectedTotal *prometheus.CounterVec

	// BulkheadCurrent tracks current bulkhead occupancy.
	BulkheadCurrent *prometheus.GaugeVec

	// ── Upstream metrics ─────────────────────────────────────────────────────

	// UpstreamRequestTotal counts requests sent to upstreams.
	UpstreamRequestTotal *prometheus.CounterVec

	// UpstreamDuration tracks upstream response latency.
	UpstreamDuration *prometheus.HistogramVec

	// UpstreamErrors counts upstream errors by type.
	UpstreamErrors *prometheus.CounterVec

	// ── Audit metrics ────────────────────────────────────────────────────────

	// AuditQueueDepth tracks the audit log queue depth.
	AuditQueueDepth prometheus.Gauge

	// AuditWriteTotal counts Cassandra audit writes.
	AuditWriteTotal *prometheus.CounterVec

	// AuditWriteDuration tracks Cassandra write latency.
	AuditWriteDuration prometheus.Histogram

	// ── Redis metrics ────────────────────────────────────────────────────────

	// RedisCommandDuration tracks Redis command latency.
	RedisCommandDuration *prometheus.HistogramVec

	// RedisErrors counts Redis errors.
	RedisErrors *prometheus.CounterVec
}

// NewRegistry creates and registers all CloudGate metrics.
func NewRegistry() *Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	r := &Registry{reg: reg}

	// ── HTTP gateway ─────────────────────────────────────────────────────────
	r.RequestTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cloudgate",
		Subsystem: "gateway",
		Name:      "requests_total",
		Help:      "Total number of HTTP requests processed by the gateway.",
		// No raw URL path label: paths carry IDs (/orders/8f3a...), so every
		// distinct URL created a new time series. Per-path detail is in the audit log.
	}, []string{"tenant", "method", "status"})

	r.RequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cloudgate",
		Subsystem: "gateway",
		Name:      "request_duration_seconds",
		Help:      "HTTP request latency in seconds.",
		Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5},
	}, []string{"tenant", "method", "status"})

	r.RequestInFlight = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cloudgate",
		Subsystem: "gateway",
		Name:      "requests_in_flight",
		Help:      "Current number of in-flight requests.",
	}, []string{"tenant"})

	r.ResponseSize = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cloudgate",
		Subsystem: "gateway",
		Name:      "response_size_bytes",
		Help:      "HTTP response body size in bytes.",
		Buckets:   prometheus.ExponentialBuckets(100, 10, 7), //nolint:mnd // 100 B to 100 MB in 7 decade buckets
	}, []string{"tenant", "status"})

	// ── Rate limiting ─────────────────────────────────────────────────────────
	r.RateLimitTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cloudgate",
		Subsystem: "ratelimit",
		Name:      "decisions_total",
		Help:      "Total rate limit decisions (allowed/denied) by strategy.",
	}, []string{"tenant", "strategy", "decision"})

	// ── Circuit breaker ───────────────────────────────────────────────────────
	r.CircuitBreakerState = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cloudgate",
		Subsystem: "circuit_breaker",
		Name:      "state",
		Help:      "Current circuit breaker state: 0=closed, 1=open, 2=half-open.",
	}, []string{"upstream"})

	r.CircuitBreakerTransitions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cloudgate",
		Subsystem: "circuit_breaker",
		Name:      "transitions_total",
		Help:      "Number of circuit breaker state transitions.",
	}, []string{"upstream", "from", "to"})

	// ── Bulkhead ──────────────────────────────────────────────────────────────
	r.BulkheadRejectedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cloudgate",
		Subsystem: "bulkhead",
		Name:      "rejected_total",
		Help:      "Total requests rejected by the bulkhead.",
	}, []string{"tenant"})

	r.BulkheadCurrent = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cloudgate",
		Subsystem: "bulkhead",
		Name:      "current_occupancy",
		Help:      "Current number of occupied bulkhead slots.",
	}, []string{"tenant"})

	// ── Upstream ──────────────────────────────────────────────────────────────
	r.UpstreamRequestTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cloudgate",
		Subsystem: "upstream",
		Name:      "requests_total",
		Help:      "Total requests forwarded to upstreams.",
	}, []string{"tenant", "upstream", "status"})

	r.UpstreamDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cloudgate",
		Subsystem: "upstream",
		Name:      "request_duration_seconds",
		Help:      "Upstream response latency in seconds.",
		Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
	}, []string{"tenant", "upstream"})

	r.UpstreamErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cloudgate",
		Subsystem: "upstream",
		Name:      "errors_total",
		Help:      "Total upstream errors by type.",
	}, []string{"tenant", "upstream", "error_type"})

	// ── Audit ─────────────────────────────────────────────────────────────────
	r.AuditQueueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cloudgate",
		Subsystem: "audit",
		Name:      "queue_depth",
		Help:      "Current depth of the audit log write queue.",
	})

	r.AuditWriteTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cloudgate",
		Subsystem: "audit",
		Name:      "writes_total",
		Help:      "Total Cassandra audit log writes.",
	}, []string{"status"})

	r.AuditWriteDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "cloudgate",
		Subsystem: "audit",
		Name:      "write_duration_seconds",
		Help:      "Cassandra audit write latency in seconds.",
		Buckets:   []float64{.001, .005, .01, .025, .05},
	})

	// ── Redis ─────────────────────────────────────────────────────────────────
	r.RedisCommandDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cloudgate",
		Subsystem: "redis",
		Name:      "command_duration_seconds",
		Help:      "Redis command latency in seconds.",
		Buckets:   []float64{.0001, .0005, .001, .005, .01},
	}, []string{"command"})

	r.RedisErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cloudgate",
		Subsystem: "redis",
		Name:      "errors_total",
		Help:      "Total Redis errors by command.",
	}, []string{"command"})

	// Register all
	reg.MustRegister(
		r.RequestTotal,
		r.RequestDuration,
		r.RequestInFlight,
		r.ResponseSize,
		r.RateLimitTotal,
		r.CircuitBreakerState,
		r.CircuitBreakerTransitions,
		r.BulkheadRejectedTotal,
		r.BulkheadCurrent,
		r.UpstreamRequestTotal,
		r.UpstreamDuration,
		r.UpstreamErrors,
		r.AuditQueueDepth,
		r.AuditWriteTotal,
		r.AuditWriteDuration,
		r.RedisCommandDuration,
		r.RedisErrors,
	)

	return r
}

// Handler returns an HTTP handler for the /metrics endpoint.
func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.reg, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}
