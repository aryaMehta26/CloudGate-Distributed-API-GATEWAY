package router

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/aryaMehta26/cloudgate/internal/audit"
	"github.com/aryaMehta26/cloudgate/internal/config"
	"github.com/aryaMehta26/cloudgate/internal/metrics"
	"github.com/aryaMehta26/cloudgate/internal/tenant"
	"github.com/aryaMehta26/cloudgate/pkg/redis"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type auditRecorder struct {
	mu     sync.Mutex
	events []audit.Event
}

func (a *auditRecorder) Log(e audit.Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, e)
}

type harness struct {
	gw    *Gateway
	mr    *miniredis.Miniredis
	rdb   *redis.Client
	audit *auditRecorder
}

func tenantConfig(id string, upstreams ...string) config.TenantConfig {
	return config.TenantConfig{
		ID:             id,
		Upstreams:      upstreams,
		RateLimit:      config.RateLimitConf{RequestsPerSecond: 1000, Burst: 1000, WindowSeconds: 60, MaxInWindow: 100000},
		CircuitBreaker: config.CBConf{FailureThreshold: 3, OpenTimeout: time.Minute, SuccessThreshold: 1},
		BulkheadMax:    16,
	}
}

func newHarness(t *testing.T, tenants ...config.TenantConfig) *harness {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.Wrap(goredis.NewClient(&goredis.Options{Addr: mr.Addr()}))
	t.Cleanup(func() { rdb.Close() })
	rec := &auditRecorder{}
	gw, err := New(&config.Config{Tenants: tenants}, rdb, metrics.NewRegistry(), rec, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	return &harness{gw: gw, mr: mr, rdb: rdb, audit: rec}
}

func (h *harness) do(t *testing.T, tenant, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "203.0.113.7:51000"
	if tenant != "" {
		req.Header.Set(TenantHeader, tenant)
	}
	w := httptest.NewRecorder()
	h.gw.ServeHTTP(w, req)
	return w
}

func upstream(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(s.Close)
	return s
}

func TestProxiesToTenantUpstream(t *testing.T) {
	up := upstream(t, "hello from acme", http.StatusOK)
	h := newHarness(t, tenantConfig("acme", up.URL))
	w := h.do(t, "acme", "/orders")
	if w.Code != http.StatusOK || w.Body.String() != "hello from acme" {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
	if len(h.audit.events) != 1 || h.audit.events[0].Path != "/orders" || h.audit.events[0].Status != 200 {
		t.Fatalf("audit events: %+v", h.audit.events)
	}
}

func TestRejectsMissingAndUnknownTenants(t *testing.T) {
	h := newHarness(t)
	if w := h.do(t, "", "/"); w.Code != http.StatusBadRequest {
		t.Fatalf("missing tenant header: %d", w.Code)
	}
	if w := h.do(t, "ghost", "/"); w.Code != http.StatusUnauthorized {
		t.Fatalf("unknown tenant: %d", w.Code)
	}
}

func TestCircuitOpensAfterUpstreamFailures(t *testing.T) {
	up := upstream(t, "boom", http.StatusInternalServerError)
	h := newHarness(t, tenantConfig("acme", up.URL))
	for i := 0; i < 3; i++ {
		if w := h.do(t, "acme", "/"); w.Code != http.StatusInternalServerError {
			t.Fatalf("request %d: %d", i, w.Code)
		}
	}
	if w := h.do(t, "acme", "/"); w.Code != http.StatusBadGateway {
		t.Fatalf("circuit should be open: %d", w.Code)
	}
}

func TestNewRejectsInvalidTenantConfig(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.Wrap(goredis.NewClient(&goredis.Options{Addr: mr.Addr()}))
	defer rdb.Close()
	bad := tenantConfig("acme") // no upstreams: used to panic on Upstreams[0]
	if _, err := New(&config.Config{Tenants: []config.TenantConfig{bad}}, rdb, metrics.NewRegistry(), &auditRecorder{}, zap.NewNop()); err == nil {
		t.Fatal("tenant without upstreams accepted")
	}
}

func TestLoadsTenantCreatedThroughStore(t *testing.T) {
	up := upstream(t, "new tenant", http.StatusOK)
	h := newHarness(t)
	if w := h.do(t, "fresh", "/"); w.Code != http.StatusUnauthorized {
		t.Fatalf("before create: %d", w.Code)
	}
	p := &tenant.Policy{TenantID: "fresh", Upstreams: []string{up.URL}, RPSLimit: 10, Burst: 10, WindowSecs: 60,
		MaxInWindow: 100, BulkheadMax: 4, CBFailures: 3, CBOpenTimeoutMs: 1000, CBSuccesses: 1}
	if err := h.gw.store.Set(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if w := h.do(t, "fresh", "/"); w.Code != http.StatusOK || w.Body.String() != "new tenant" {
		t.Fatalf("after create: %d %q", w.Code, w.Body.String())
	}
}
