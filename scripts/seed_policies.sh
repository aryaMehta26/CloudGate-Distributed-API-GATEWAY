#!/usr/bin/env bash
# ── CloudGate Policy Seed Script ─────────────────────────────────────────────
# Seeds initial tenant policies into Redis via the gRPC admin service.
# Run after `make deps-up` and `make run-admin`.
#
# Requirements: grpcurl (brew install grpcurl)
# Usage: ./scripts/seed_policies.sh [grpc-addr]
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail

GRPC_ADDR="${1:-localhost:50051}"

if ! command -v grpcurl &>/dev/null; then
    echo "Error: grpcurl is not installed. Run: brew install grpcurl"
    exit 1
fi

seed_tenant() {
    local tenant_id="$1"
    local upstream="$2"
    local rps="$3"
    local burst="$4"

    echo "Seeding policy for: $tenant_id"
    grpcurl -plaintext -d "{
        \"tenant_id\": \"$tenant_id\",
        \"upstreams\": [\"$upstream\"],
        \"rps_limit\": $rps,
        \"burst\": $burst,
        \"window_secs\": 10,
        \"max_in_window\": $((rps * 10)),
        \"bulkhead_max\": 50,
        \"cb_failure_threshold\": 5,
        \"cb_open_timeout_ms\": 10000,
        \"cb_success_threshold\": 2
    }" "$GRPC_ADDR" admin.v1.AdminService/UpdatePolicy
}

echo "CloudGate Policy Seeder"
echo "========================="
echo "gRPC addr: $GRPC_ADDR"
echo ""

seed_tenant "tenant-acme"   "http://localhost:9999" 100  200
seed_tenant "tenant-beta"   "http://localhost:9998" 500  1000
seed_tenant "tenant-gamma"  "http://localhost:9997" 50   100

echo ""
echo "Listing all tenants:"
grpcurl -plaintext "$GRPC_ADDR" admin.v1.AdminService/ListTenants

echo ""
echo "Done. Policies are now live in Redis."
