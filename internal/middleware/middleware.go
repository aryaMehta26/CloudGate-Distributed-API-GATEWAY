// Package middleware provides composable HTTP middleware for the CloudGate gateway.
package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/aryaMehta26/cloudgate/internal/metrics"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Chain applies middleware in left-to-right order (outermost to innermost).
func Chain(h http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

// RequestID injects a unique X-Request-ID header if one is not already present.
func RequestID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if id == "" {
				id = uuid.NewString()
				r.Header.Set("X-Request-ID", id)
			}
			w.Header().Set("X-Request-ID", id)
			next.ServeHTTP(w, r)
		})
	}
}

// Logger logs each request using structured zap logging.
func Logger(log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := &statusWriter{ResponseWriter: w, code: http.StatusOK}
			next.ServeHTTP(wrapped, r)
			log.Info("request",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.String("tenant", r.Header.Get("X-Tenant-ID")),
				zap.String("request_id", r.Header.Get("X-Request-ID")),
				zap.Int("status", wrapped.code),
				zap.Duration("latency", time.Since(start)),
				zap.String("remote", r.RemoteAddr),
			)
		})
	}
}

// Recovery catches panics, logs them, and returns a 500 response.
func Recovery(log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if rec == http.ErrAbortHandler {
						// Deliberate abort (e.g. ReverseProxy on a broken
						// upstream stream): let net/http drop the connection.
						panic(rec)
					}
					log.Error("panic recovered",
						zap.Any("panic", rec),
						zap.String("stack", string(debug.Stack())),
					)
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Metrics tracks in-flight requests per tenant using the Prometheus registry.
func Metrics(reg *metrics.Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantID := r.Header.Get("X-Tenant-ID")
			if tenantID == "" {
				tenantID = "unknown"
			}
			reg.RequestInFlight.WithLabelValues(tenantID).Inc()
			defer reg.RequestInFlight.WithLabelValues(tenantID).Dec()
			next.ServeHTTP(w, r)
		})
	}
}

// CORS adds permissive CORS headers for development environments.
// Replace with a strict policy before production use.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			allowed := false
			for _, o := range allowedOrigins {
				if o == "*" || o == origin {
					allowed = true
					break
				}
			}
			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Tenant-ID, X-Request-ID, Authorization")
				w.Header().Set("Access-Control-Max-Age", strconv.Itoa(86400))
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimit informs the client of its limit status via standard headers.
func RateLimitHeaders(limit, remaining int, resetAt time.Time) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetAt.Unix()))
			next.ServeHTTP(w, r)
		})
	}
}

// statusWriter wraps ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	code int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.code = code
	sw.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach Flush and Hijack on the
// underlying writer.
func (sw *statusWriter) Unwrap() http.ResponseWriter { return sw.ResponseWriter }
