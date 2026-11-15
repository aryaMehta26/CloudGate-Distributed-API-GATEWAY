// Package tenant — gRPC AdminServer implementation.
package tenant

import (
	"context"

	adminpb "github.com/aryaMehta26/cloudgate/proto/admin"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
		RpsLimit:           int32(p.RPSLimit),
		Burst:              int32(p.Burst),
		BulkheadMax:        int32(p.BulkheadMax),
		CbFailureThreshold: int32(p.CBFailures),
		CbOpenTimeoutMs:    int32(p.CBOpenTimeoutMs),
		CbSuccessThreshold: int32(p.CBSuccesses),
	}, nil
}

// ListTenants returns all registered tenant IDs.
func (s *AdminServer) ListTenants(ctx context.Context, _ *adminpb.ListTenantsRequest) (*adminpb.ListTenantsResponse, error) {
	ids, err := s.store.List(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list tenants: %v", err)
	}
	return &adminpb.ListTenantsResponse{TenantIds: ids}, nil
}
