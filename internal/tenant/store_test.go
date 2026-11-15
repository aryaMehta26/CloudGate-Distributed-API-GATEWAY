package tenant

import (
	"context"
	"testing"
	"time"
)

func TestStorePoliciesDoNotExpire(t *testing.T) {
	store, mr := newTestStore(t)
	ctx := context.Background()
	if err := store.Set(ctx, &Policy{TenantID: "acme", RPSLimit: 10}); err != nil {
		t.Fatal(err)
	}
	mr.FastForward(30 * 24 * time.Hour)
	store.Invalidate("acme") // force a Redis read
	p, err := store.Get(ctx, "acme")
	if err != nil || p.RPSLimit != 10 {
		t.Fatalf("policy lost after 30 days: %v %+v", err, p)
	}
	if ttl := mr.TTL(tenantKeyPrefix + "acme"); ttl != 0 {
		t.Fatalf("policy key has TTL %v", ttl)
	}
}

func TestStoreSetGetListDelete(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"a", "b"} {
		if err := store.Set(ctx, &Policy{TenantID: id, Burst: 5}); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := store.List(ctx)
	if err != nil || len(ids) != 2 {
		t.Fatalf("List: %v %v", ids, err)
	}
	if err = store.Delete(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Get(ctx, "a"); err == nil {
		t.Fatal("deleted tenant still readable")
	}
	if ids, _ = store.List(ctx); len(ids) != 1 || ids[0] != "b" {
		t.Fatalf("index after delete: %v", ids)
	}
}

func TestPolicyValidate(t *testing.T) {
	ok := Policy{TenantID: "a", Upstreams: []string{"http://svc:8080"}, RPSLimit: 1, Burst: 1, WindowSecs: 1,
		MaxInWindow: 1, BulkheadMax: 1, CBFailures: 1, CBOpenTimeoutMs: 1, CBSuccesses: 1}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Policy){
		"no tenant":      func(p *Policy) { p.TenantID = "" },
		"no upstreams":   func(p *Policy) { p.Upstreams = nil },
		"relative url":   func(p *Policy) { p.Upstreams = []string{"/svc"} },
		"bad scheme":     func(p *Policy) { p.Upstreams = []string{"ftp://svc"} },
		"zero bulkhead":  func(p *Policy) { p.BulkheadMax = 0 },
		"negative burst": func(p *Policy) { p.Burst = -1 },
	} {
		p := ok
		p.Upstreams = append([]string(nil), ok.Upstreams...)
		mutate(&p)
		if p.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
