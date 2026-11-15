package tenant

import (
	"context"
	"net"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/aryaMehta26/cloudgate/pkg/redis"
	adminpb "github.com/aryaMehta26/cloudgate/proto/admin"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func newTestStore(t *testing.T) (*Store, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return NewRedisStore(redis.Wrap(rdb), zap.NewNop()), mr
}

// newAdminClient serves AdminService over an in-memory connection, so the test
// covers real gRPC encoding of every request and response.
func newAdminClient(t *testing.T, store *Store) adminpb.AdminServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	adminpb.RegisterAdminServiceServer(srv, NewAdminServer(store, zap.NewNop()))
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return adminpb.NewAdminServiceClient(conn)
}

func TestAdminServiceRoundTrip(t *testing.T) {
	store, _ := newTestStore(t)
	c := newAdminClient(t, store)
	ctx := context.Background()
	req := &adminpb.UpdatePolicyRequest{
		TenantId: "acme", Upstreams: []string{"http://a:8080", "http://b:8080"},
		RpsLimit: 100, Burst: 20, WindowSecs: 60, MaxInWindow: 5000, BulkheadMax: 32,
		CbFailureThreshold: 5, CbOpenTimeoutMs: 3000, CbSuccessThreshold: 2,
	}
	up, err := c.UpdatePolicy(ctx, req)
	if err != nil || !up.Success {
		t.Fatalf("UpdatePolicy: %v %v", up, err)
	}
	got, err := c.GetPolicy(ctx, &adminpb.GetPolicyRequest{TenantId: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if got.RpsLimit != 100 || got.Burst != 20 || len(got.Upstreams) != 2 ||
		got.BulkheadMax != 32 || got.CbOpenTimeoutMs != 3000 ||
		got.GetWindowSecs() != 60 || got.GetMaxInWindow() != 5000 {
		t.Fatalf("GetPolicy returned %+v", got)
	}
	list, err := c.ListTenants(ctx, &adminpb.ListTenantsRequest{})
	if err != nil || len(list.TenantIds) != 1 || list.TenantIds[0] != "acme" {
		t.Fatalf("ListTenants: %v %v", list, err)
	}
}

func TestAdminServiceValidation(t *testing.T) {
	store, _ := newTestStore(t)
	c := newAdminClient(t, store)
	ctx := context.Background()
	if _, err := c.UpdatePolicy(ctx, &adminpb.UpdatePolicyRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing tenant: %v", err)
	}
	bad := &adminpb.UpdatePolicyRequest{TenantId: "acme", Upstreams: []string{"not a url"}, RpsLimit: 1}
	if _, err := c.UpdatePolicy(ctx, bad); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid policy: %v", err)
	}
	if _, err := c.GetPolicy(ctx, &adminpb.GetPolicyRequest{TenantId: "nobody"}); status.Code(err) != codes.NotFound {
		t.Fatalf("unknown tenant: %v", err)
	}
}
