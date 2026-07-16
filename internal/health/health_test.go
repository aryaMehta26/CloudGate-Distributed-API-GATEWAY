package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockPinger struct{ err error }

func (m *mockPinger) Ping(_ context.Context) error { return m.err }

func TestLiveness_AlwaysOK(t *testing.T) {
	h := New(&mockPinger{}, nil)
	// Inject a no-op logger to avoid nil panic
	h.log = noopLogger()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.Liveness(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("liveness: expected 200, got %d", rec.Code)
	}

	var resp response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("expected status ok, got %q", resp.Status)
	}
}

func TestReadiness_AllHealthy(t *testing.T) {
	h := New(&mockPinger{err: nil}, noopLogger())
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	h.Readiness(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("readiness: expected 200 when healthy, got %d", rec.Code)
	}
}

func TestReadiness_RedisDegraded(t *testing.T) {
	h := New(&mockPinger{err: context.DeadlineExceeded}, noopLogger())
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	h.Readiness(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness: expected 503 when redis down, got %d", rec.Code)
	}

	var resp response
	json.NewDecoder(rec.Body).Decode(&resp) //nolint:errcheck
	if resp.Status != "degraded" {
		t.Fatalf("expected status degraded, got %q", resp.Status)
	}
}
