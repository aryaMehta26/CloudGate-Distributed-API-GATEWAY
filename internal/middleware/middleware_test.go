package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

func TestChainOrder(t *testing.T) {
	var order string
	mk := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order += name
				next.ServeHTTP(w, r)
			})
		}
	}
	h := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { order += "h" }), mk("a"), mk("b"), mk("c"))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if order != "abch" {
		t.Fatalf("order %q", order)
	}
}

func TestRequestIDGeneratesAndPreserves(t *testing.T) {
	var seen string
	h := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = r.Header.Get("X-Request-ID") }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if seen == "" || w.Header().Get("X-Request-ID") != seen {
		t.Fatalf("generated id %q, response %q", seen, w.Header().Get("X-Request-ID"))
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "given")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if seen != "given" || w.Header().Get("X-Request-ID") != "given" {
		t.Fatal("existing id replaced")
	}
}

func TestRecoveryReturns500(t *testing.T) {
	h := Recovery(zap.NewNop())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("code %d", w.Code)
	}
}

func TestRecoveryRepanicsAbortHandler(t *testing.T) {
	h := Recovery(zap.NewNop())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }))
	defer func() {
		if rec := recover(); rec != http.ErrAbortHandler {
			t.Fatalf("recovered %v, want http.ErrAbortHandler", rec)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	t.Fatal("ErrAbortHandler was swallowed")
}

func TestCORS(t *testing.T) {
	h := CORS([]string{"https://app.example"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(299) }))
	pre := httptest.NewRequest(http.MethodOptions, "/", nil)
	pre.Header.Set("Origin", "https://app.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, pre)
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "https://app.example" {
		t.Fatalf("preflight: %d %v", w.Code, w.Header())
	}
	other := httptest.NewRequest(http.MethodGet, "/", nil)
	other.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, other)
	if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Code != 299 {
		t.Fatal("disallowed origin got CORS headers")
	}
}
