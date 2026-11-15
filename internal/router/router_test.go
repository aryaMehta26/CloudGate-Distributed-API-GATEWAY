package router

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/aryaMehta26/cloudgate/internal/audit"
	"github.com/aryaMehta26/cloudgate/internal/config"
	"github.com/aryaMehta26/cloudgate/internal/metrics"
	"github.com/aryaMehta26/cloudgate/internal/tenant"
	"github.com/aryaMehta26/cloudgate/pkg/redis"
	"github.com/prometheus/client_golang/prometheus/testutil"
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

// A policy change made through any replica's store must reach this gateway's
// runtimes: here the tenant moves to a new upstream while traffic flows.
func TestPolicyUpdatesReachRunningGateway(t *testing.T) {
	oldUp := upstream(t, "old", http.StatusOK)
	newUp := upstream(t, "new", http.StatusOK)
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	go h.gw.WatchPolicyUpdates(ctx, ready)
	<-ready

	// A second store stands in for another replica's admin server.
	other := tenant.NewRedisStore(h.rdb, zap.NewNop())
	p := &tenant.Policy{TenantID: "acme", Upstreams: []string{oldUp.URL}, RPSLimit: 10, Burst: 10, WindowSecs: 60,
		MaxInWindow: 100, BulkheadMax: 4, CBFailures: 3, CBOpenTimeoutMs: 1000, CBSuccesses: 1}
	if err := other.Set(ctx, p); err != nil {
		t.Fatal(err)
	}
	if w := h.do(t, "acme", "/"); w.Body.String() != "old" {
		t.Fatalf("got %q", w.Body.String())
	}
	updated := *p
	updated.Upstreams = []string{newUp.URL}
	if err := other.Set(ctx, &updated); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.do(t, "acme", "/").Body.String() == "new" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("policy update never reached the running gateway")
}

func TestRateLimitIsPerClientIPNotPerConnection(t *testing.T) {
	up := upstream(t, "ok", http.StatusOK)
	tc := tenantConfig("acme", up.URL)
	tc.RateLimit.Burst, tc.RateLimit.RequestsPerSecond = 2, 1
	h := newHarness(t, tc)
	codes := []int{}
	for port := 40000; port < 40004; port++ { // same IP, a new source port each time
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "198.51.100.9:" + strconv.Itoa(port)
		req.Header.Set(TenantHeader, "acme")
		w := httptest.NewRecorder()
		h.gw.ServeHTTP(w, req)
		codes = append(codes, w.Code)
	}
	if codes[0] != 200 || codes[1] != 200 || codes[2] != http.StatusTooManyRequests {
		t.Fatalf("new connections bypassed the limit: %v", codes)
	}
}

func TestUnreachableUpstreamCountsOneFailurePerRequest(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()                                       // connections are refused
	h := newHarness(t, tenantConfig("acme", dead.URL)) // FailureThreshold: 3
	for i := 0; i < 3; i++ {
		if w := h.do(t, "acme", "/"); w.Code != http.StatusBadGateway || w.Body.String() == "upstream unavailable (circuit open)\n" {
			t.Fatalf("request %d: circuit opened before the threshold (%d %q)", i, w.Code, w.Body.String())
		}
	}
	if w := h.do(t, "acme", "/"); w.Body.String() != "upstream unavailable (circuit open)\n" {
		t.Fatalf("circuit should be open after 3 failures: %q", w.Body.String())
	}
}

func TestForwardedForAppendsClientIP(t *testing.T) {
	var got string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Forwarded-For")
	}))
	defer up.Close()
	h := newHarness(t, tenantConfig("acme", up.URL))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.7:51000"
	req.Header.Set(TenantHeader, "acme")
	req.Header.Set("X-Forwarded-For", "192.0.2.1")
	h.gw.ServeHTTP(httptest.NewRecorder(), req)
	if got != "192.0.2.1, 203.0.113.7" {
		t.Fatalf("X-Forwarded-For = %q", got)
	}
}

func TestForwardsRequestID(t *testing.T) {
	var got string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Request-ID")
	}))
	defer up.Close()
	h := newHarness(t, tenantConfig("acme", up.URL))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(TenantHeader, "acme")
	req.Header.Set("X-Request-ID", "req-123")
	h.gw.ServeHTTP(httptest.NewRecorder(), req)
	if got != "req-123" {
		t.Fatalf("X-Request-ID = %q", got)
	}
}

func BenchmarkProxy(b *testing.B) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer up.Close()
	mr := miniredis.RunT(b)
	rdb := redis.Wrap(goredis.NewClient(&goredis.Options{Addr: mr.Addr()}))
	defer rdb.Close()
	tc := tenantConfig("acme", up.URL)
	tc.RateLimit = config.RateLimitConf{RequestsPerSecond: 1 << 30, Burst: 1 << 30, WindowSeconds: 1, MaxInWindow: 1 << 30}
	gw, err := New(&config.Config{Tenants: []config.TenantConfig{tc}}, rdb, metrics.NewRegistry(), &auditRecorder{}, zap.NewNop())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(TenantHeader, "acme")
		gw.ServeHTTP(httptest.NewRecorder(), req)
	}
}

func TestRoundRobinAcrossUpstreams(t *testing.T) {
	a := upstream(t, "a", http.StatusOK)
	b := upstream(t, "b", http.StatusOK)
	c := upstream(t, "c", http.StatusOK)
	h := newHarness(t, tenantConfig("acme", a.URL, b.URL, c.URL))
	got := ""
	for i := 0; i < 6; i++ {
		got += h.do(t, "acme", "/").Body.String()
	}
	if got != "abcabc" {
		t.Fatalf("upstream order %q", got)
	}
}

func TestRequestMetricCardinalityIndependentOfPath(t *testing.T) {
	up := upstream(t, "ok", http.StatusOK)
	h := newHarness(t, tenantConfig("acme", up.URL))
	for i := 0; i < 50; i++ {
		h.do(t, "acme", "/orders/"+strconv.Itoa(i))
	}
	if n := testutil.CollectAndCount(h.gw.reg.RequestTotal); n != 1 {
		t.Fatalf("50 distinct paths produced %d series", n)
	}
}

func TestRateLimiterOutageFailsOpen(t *testing.T) {
	up := upstream(t, "ok", http.StatusOK)
	h := newHarness(t, tenantConfig("acme", up.URL))
	h.mr.Close() // Redis is down
	if w := h.do(t, "acme", "/"); w.Code != http.StatusOK {
		t.Fatalf("redis outage rejected traffic: %d", w.Code)
	}
	if v := testutil.ToFloat64(h.gw.reg.RateLimitTotal.WithLabelValues("acme", "composite", "error_allowed")); v != 1 {
		t.Fatalf("error_allowed = %v", v)
	}
}
