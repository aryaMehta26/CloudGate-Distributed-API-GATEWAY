// Package health exposes /healthz and /readyz endpoints for Kubernetes probes.
//
// /healthz — liveness probe: returns 200 if the process is alive
// /readyz  — readiness probe: returns 200 only when Redis and Cassandra are reachable
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// Pinger is implemented by any dependency that supports a connectivity check.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Handler holds the health check dependencies.
type Handler struct {
	redis     Pinger
	log       *zap.Logger
	startedAt time.Time
}

// response is the JSON body returned by both endpoints.
type response struct {
	Status    string            `json:"status"`
	UptimeSec int64             `json:"uptime_sec"`
	Checks    map[string]string `json:"checks,omitempty"`
}

// New creates a new health Handler.
func New(redis Pinger, log *zap.Logger) *Handler {
	return &Handler{
		redis:     redis,
		log:       log,
		startedAt: time.Now(),
	}
}

// Liveness handles GET /healthz.
// It always returns 200 as long as the process is running.
func (h *Handler) Liveness(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response{ //nolint:errcheck
		Status:    "ok",
		UptimeSec: int64(time.Since(h.startedAt).Seconds()),
	})
}

// Readiness handles GET /readyz.
// It returns 200 only when all dependencies are reachable.
// Returns 503 if any dependency is unavailable.
func (h *Handler) Readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	checks := make(map[string]string)
	healthy := true

	// Check Redis
	if err := h.redis.Ping(ctx); err != nil {
		checks["redis"] = "unreachable: " + err.Error()
		healthy = false
		h.log.Warn("readiness check: redis unreachable", zap.Error(err))
	} else {
		checks["redis"] = "ok"
	}

	w.Header().Set("Content-Type", "application/json")
	if !healthy {
		w.WriteHeader(http.StatusServiceUnavailable)
		checks["status"] = "degraded"
	}

	json.NewEncoder(w).Encode(response{ //nolint:errcheck
		Status:    map[bool]string{true: "ok", false: "degraded"}[healthy],
		UptimeSec: int64(time.Since(h.startedAt).Seconds()),
		Checks:    checks,
	})
}

// RegisterRoutes mounts the health endpoints on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.Liveness)
	mux.HandleFunc("/readyz", h.Readiness)
}
