// Package tenant — gRPC AdminServer implementation.
package tenant

import (
	"context"
	"math"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	adminpb "github.com/aryaMehta26/cloudgate/proto/admin"
)

// AdminServer implements the gRPC AdminService for hot policy updates.
type AdminServer struct {
	adminpb.UnimplementedAdminServiceServer
	store *Store
	log   *zap.Logger
}

// NewAdminServer constructs an AdminServer.
func NewAdminServer(store *Store, log *zap.Logger) *AdminServer {
	return &AdminServer{store: store, log: log}
}

// UpdatePolicy applies a new policy for the given tenant.
func (s *AdminServer) UpdatePolicy(ctx context.Context, req *adminpb.UpdatePolicyRequest) (*adminpb.UpdatePolicyResponse, error) {
	if req.GetTenantId() == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}

	policy := &Policy{
		TenantID:        req.GetTenantId(),
		Upstreams:       req.GetUpstreams(),
		RPSLimit:        int(req.GetRpsLimit()),
		Burst:           int(req.GetBurst()),
		WindowSecs:      int(req.GetWindowSecs()),
		MaxInWindow:     int(req.GetMaxInWindow()),
		BulkheadMax:     int(req.GetBulkheadMax()),
		CBFailures:      int(req.GetCbFailureThreshold()),
		CBOpenTimeoutMs: int(req.GetCbOpenTimeoutMs()),
		CBSuccesses:     int(req.GetCbSuccessThreshold()),
	}

	if err := policy.Validate(); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid policy: %v", err)
	}

	if err := s.store.Set(ctx, policy); err != nil {
		s.log.Error("failed to update policy", zap.Error(err), zap.String("tenant", req.GetTenantId()))
		return nil, status.Errorf(codes.Internal, "failed to update policy: %v", err)
	}

	return &adminpb.UpdatePolicyResponse{
		Success: true,
		Message: "policy updated successfully",
	}, nil
}

// GetPolicy retrieves the current policy for a tenant.
func (s *AdminServer) GetPolicy(ctx context.Context, req *adminpb.GetPolicyRequest) (*adminpb.GetPolicyResponse, error) {
	if req.GetTenantId() == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}

	p, err := s.store.Get(ctx, req.GetTenantId())
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "tenant %q not found: %v", req.GetTenantId(), err)
	}

	return &adminpb.GetPolicyResponse{
		TenantId:           p.TenantID,
		Upstreams:          p.Upstreams,
		RpsLimit:           toInt32(p.RPSLimit),
		Burst:              toInt32(p.Burst),
		WindowSecs:         toInt32(p.WindowSecs),
		MaxInWindow:        toInt32(p.MaxInWindow),
		BulkheadMax:        toInt32(p.BulkheadMax),
		CbFailureThreshold: toInt32(p.CBFailures),
		CbOpenTimeoutMs:    toInt32(p.CBOpenTimeoutMs),
		CbSuccessThreshold: toInt32(p.CBSuccesses),
	}, nil
}

// toInt32 converts a policy value for the int32 wire fields, saturating
// instead of wrapping. Policies written by config or older clients could hold
// values above MaxInt32, which int32() silently turned negative.
func toInt32(v int) int32 {
	switch {
	case v > math.MaxInt32:
		return math.MaxInt32
	case v < math.MinInt32:
		return math.MinInt32
	default:
		return int32(v)
	}
}

// ListTenants returns all registered tenant IDs.
func (s *AdminServer) ListTenants(ctx context.Context, _ *adminpb.ListTenantsRequest) (*adminpb.ListTenantsResponse, error) {
	ids, err := s.store.List(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list tenants: %v", err)
	}
	return &adminpb.ListTenantsResponse{TenantIds: ids}, nil
}
