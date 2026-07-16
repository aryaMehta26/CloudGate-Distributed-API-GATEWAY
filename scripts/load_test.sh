#!/usr/bin/env bash
# ── CloudGate Load Test Script ────────────────────────────────────────────────
# Sends sustained traffic to the gateway to validate rate limiting and observe
# circuit breaker behaviour under load.
#
# Requirements: wrk (brew install wrk) or hey (brew install hey)
# Usage: ./scripts/load_test.sh [host] [tenant] [duration]
# Example: ./scripts/load_test.sh localhost:8080 tenant-acme 30s
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail

HOST="${1:-localhost:8080}"
TENANT="${2:-tenant-acme}"
DURATION="${3:-30s}"
THREADS=4
CONNECTIONS=100

echo "CloudGate Load Test"
echo "=================================="
echo "Host:        $HOST"
echo "Tenant:      $TENANT"
echo "Duration:    $DURATION"
echo "Threads:     $THREADS"
echo "Connections: $CONNECTIONS"
echo "=================================="
echo ""

# Check for wrk
if command -v wrk &>/dev/null; then
    echo "Running with wrk..."
    wrk \
        -t "$THREADS" \
        -c "$CONNECTIONS" \
        -d "$DURATION" \
        -H "X-Tenant-ID: $TENANT" \
        -H "X-Request-ID: load-test" \
        --latency \
        "http://$HOST/"

# Fallback to hey
elif command -v hey &>/dev/null; then
    echo "Running with hey..."
    hey \
        -z "$DURATION" \
        -c "$CONNECTIONS" \
        -H "X-Tenant-ID: $TENANT" \
        "http://$HOST/"

# Fallback to curl in a loop
else
    echo "WARNING: wrk and hey not found. Using curl loop (less accurate)."
    echo "Install wrk for accurate results: brew install wrk"
    echo ""
    end=$((SECONDS + 30))
    count=0
    rate_limited=0
    errors=0
    while [ $SECONDS -lt $end ]; do
        status=$(curl -s -o /dev/null -w "%{http_code}" \
            -H "X-Tenant-ID: $TENANT" \
            "http://$HOST/" 2>/dev/null || echo "000")
        case $status in
            2*) ((count++)) ;;
            429) ((rate_limited++)) ;;
            *) ((errors++)) ;;
        esac
    done
    echo "Results:"
    echo "  Successful:   $count"
    echo "  Rate limited: $rate_limited (429)"
    echo "  Errors:       $errors"
fi

echo ""
echo "Metrics snapshot:"
curl -s "http://${HOST%:*}:9090/metrics" 2>/dev/null | \
    grep -E "^cloudgate_(gateway_requests_total|ratelimit_decisions|circuit_breaker_state)" | \
    head -20 || echo "  (metrics endpoint not reachable — is the gateway running?)"
