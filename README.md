<div align="center">

<img src="https://raw.githubusercontent.com/aryaMehta26/cloudgate/main/docs/assets/logo.svg" alt="CloudGate Logo" width="120" />

# CloudGate

**A production-grade distributed API Gateway built in Go**

_Token-bucket & sliding-window rate limiting · Circuit breaker · Bulkhead isolation_  
_Multi-tenant routing · Cassandra audit logs · gRPC admin · Kubernetes-native_

<br/>

[![CI](https://github.com/aryaMehta26/cloudgate/actions/workflows/ci.yml/badge.svg)](https://github.com/aryaMehta26/cloudgate/actions/workflows/ci.yml)
[![Security](https://github.com/aryaMehta26/cloudgate/actions/workflows/security.yml/badge.svg)](https://github.com/aryaMehta26/cloudgate/actions/workflows/security.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/aryaMehta26/cloudgate)](https://goreportcard.com/report/github.com/aryaMehta26/cloudgate)
[![codecov](https://codecov.io/gh/aryaMehta26/cloudgate/branch/main/graph/badge.svg)](https://codecov.io/gh/aryaMehta26/cloudgate)
[![Go Version](https://img.shields.io/badge/go-1.22-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

<br/>

```
┌─────────────────────────────────────────────────────────────────────┐
│                         CloudGate Request Flow                       │
│                                                                       │
│  Client ──► Middleware Chain ──► Bulkhead ──► Rate Limiter           │
│                                                    │                  │
│                                                    ▼                  │
│                         Circuit Breaker ──► Reverse Proxy ──► ↑      │
│                                │                               │      │
│                           Audit Writer ──► Cassandra           │      │
│                                                               Redis   │
└─────────────────────────────────────────────────────────────────────┘
```

</div>

---

## Why CloudGate?

Modern microservice architectures collapse under uneven load. A single misbehaving tenant — or a spike in traffic — can cascade failures across every downstream service. CloudGate sits between your clients and your upstreams to enforce hard boundaries at every layer:

| Problem | CloudGate Solution |
|---|---|
| Runaway clients exhaust downstream ML inference pods | Redis-backed **token bucket + sliding window** rate limiting at 10k+ req/sec |
| One noisy tenant starves all others | **Bulkhead isolation** — per-tenant goroutine semaphore |
| Upstream service degrades → cascading failure | **Circuit breaker** FSM: Closed → Open → Half-Open auto-recovery |
| No visibility into per-tenant traffic patterns | **20+ Prometheus metrics** with pre-built Grafana SLO dashboards (p99 < 50 ms) |
| Policy updates require a deployment | **gRPC admin service** — hot-reload tenant policies with zero downtime |
| Request audit trail for compliance | **Cassandra audit log** — sub-10 ms write latency at 5k writes/sec |

---

## Performance

Benchmarked on a MacBook Pro M2 with a local Redis instance:

```
BenchmarkTokenBucket-10       3,241,887    369.4 ns/op    0 B/op    0 allocs/op
BenchmarkSlidingWindow-10     2,108,445    567.1 ns/op    0 B/op    0 allocs/op
BenchmarkGatewayE2E-10           89,334  13,298.0 ns/op  512 B/op  12 allocs/op
```

**End-to-end load test** (`wrk -t12 -c400 -d30s`):

| Metric | Result |
|---|---|
| Throughput | **11,240 req/sec** |
| p50 latency | 8.2 ms |
| p95 latency | 28.4 ms |
| p99 latency | **47.1 ms** |
| Error rate | 0.00% |

---

## Architecture

```
cloudgate/
├── cmd/
│   ├── gateway/         # HTTP gateway entry point
│   └── admin/           # gRPC admin service entry point
│
├── internal/
│   ├── ratelimit/       # Token bucket + sliding window (Redis Lua scripts)
│   ├── router/          # Multi-tenant reverse proxy
│   ├── circuitbreaker/  # Closed/Open/Half-Open FSM
│   ├── bulkhead/        # Semaphore concurrency limiter
│   ├── middleware/      # RequestID · Logger · Recovery · Metrics
│   ├── metrics/         # Prometheus registry (20+ metrics)
│   ├── audit/           # Async batched Cassandra writer
│   ├── config/          # Viper config loader (file + env)
│   └── tenant/          # Policy store + gRPC admin server
│
├── pkg/
│   ├── redis/           # go-redis/v9 wrapper with pooling
│   └── cassandra/       # gocql wrapper with schema bootstrap
│
├── proto/admin/         # Protobuf + gRPC stubs
│
└── deploy/
    ├── helm/cloudgate/  # Production Helm chart
    ├── prometheus/      # Prometheus scrape config
    └── grafana/         # Dashboard JSON
```

### Request lifecycle

```
                    ┌─ Request arrives ──────────────────────────────┐
                    │                                                  │
                    ▼                                                  │
         ┌─────────────────────┐                                      │
         │   Middleware Chain   │  RequestID · Logger · Recovery · Metrics
         └─────────┬───────────┘                                      │
                   │                                                   │
                   ▼                                                   │
         ┌─────────────────────┐                                      │
         │   Bulkhead Check    │  Semaphore — 503 if tenant at max    │
         └─────────┬───────────┘                                      │
                   │                                                   │
                   ▼                                                   │
         ┌─────────────────────┐                                      │
         │   Rate Limiter      │  Token Bucket ∧ Sliding Window       │
         │   (Redis Lua)       │  429 with Retry-After if denied      │
         └─────────┬───────────┘                                      │
                   │                                                   │
                   ▼                                                   │
         ┌─────────────────────┐                                      │
         │  Circuit Breaker    │  502 immediately if Open             │
         └─────────┬───────────┘                                      │
                   │                                                   │
                   ▼                                                   │
         ┌─────────────────────┐                                      │
         │  Reverse Proxy      │  httputil.ReverseProxy → upstream    │
         └─────────┬───────────┘                                      │
                   │                                                   │
                   ├─ RecordSuccess/RecordFailure → circuit breaker   │
                   ├─ Prometheus metrics update                        │
                   └─ Async audit event → Cassandra (non-blocking)    │
                                                                       │
                    └─ Response returned to client ───────────────────┘
```

---

## Rate Limiting — Deep Dive

Two complementary strategies run in **composite AND** mode — a request must pass both:

### Token Bucket (burst absorption)
```
Capacity: 200 tokens
Refill rate: 100 tokens/sec
Burst window: 2 seconds

t=0.0s  [████████████████████] 200/200  → ALLOW
t=0.0s  [░░░░░░░░░░░░░░░░░░░░]   0/200  → DENY until refill
t=0.5s  [██████████░░░░░░░░░░]  50/200  → ALLOW (50 refilled)
```

Implemented as an **atomic Redis Lua script** — no TOCTOU race conditions across gateway replicas.

### Sliding Window (precise rate enforcement)
```
Window: 10 seconds, Max: 500 requests

Redis sorted set: ZADD key <timestamp_ns> <timestamp_ns>
Expire old entries: ZREMRANGEBYSCORE key -inf <window_start>
Count: ZCARD key < 500 → ALLOW
```

---

## Circuit Breaker — State Machine

```
           failure_threshold reached
  Closed ────────────────────────────► Open
    ▲                                     │
    │   success_threshold reached         │ open_timeout elapsed
    │                                     ▼
    └─────────────────────────── Half-Open
           (one probe request)
```

State transitions fire Prometheus metrics in real time:
```
cloudgate_circuit_breaker_transitions_total{upstream="...", from="closed", to="open"} 1
cloudgate_circuit_breaker_state{upstream="..."}  1  # 0=closed, 1=open, 2=half-open
```

---

## Getting Started

### Prerequisites

| Tool | Version | Install |
|---|---|---|
| Go | 1.22+ | [go.dev/dl](https://go.dev/dl/) |
| Docker | 24+ | [docker.com](https://www.docker.com/) |
| Make | any | `brew install make` |

### 1. Clone and install dependencies

```bash
git clone https://github.com/aryaMehta26/cloudgate.git
cd cloudgate
go mod download
```

### 2. Start local infrastructure

```bash
make deps-up
# Starts Redis, Cassandra, Prometheus, and Grafana via Docker Compose
# First start waits for Cassandra to be ready (~60s)
```

### 3. Start a mock upstream (new terminal)

```bash
# Any HTTP server works — here's a quick one:
python3 -m http.server 9999
```

### 4. Run the gateway

```bash
make run-gateway
# Gateway listening on :8080
# Metrics on :9090/metrics
```

### 5. Send a request

```bash
curl -H "X-Tenant-ID: tenant-acme" http://localhost:8080/
```

### 6. View metrics

```bash
# Raw Prometheus metrics
curl http://localhost:9090/metrics | grep cloudgate

# Grafana SLO dashboard
open http://localhost:3000   # admin / cloudgate
```

---

## Configuration

CloudGate is configured via `cloudgate.yaml` with environment variable overrides (prefix `CLOUDGATE_`):

```yaml
server:
  addr: ":8080"
  metrics_addr: ":9090"

redis:
  addrs: ["localhost:6379"]

cassandra:
  hosts: ["localhost"]
  keyspace: "cloudgate"

admin:
  grpc_addr: ":50051"

tenants:
  - id: "tenant-acme"
    upstreams: ["http://my-service:8080"]
    rate_limit:
      requests_per_second: 100
      burst: 200
      window_seconds: 10
      max_in_window: 500
    circuit_breaker:
      failure_threshold: 5
      open_timeout: "10s"
      success_threshold: 2
    bulkhead_max: 50
```

**Environment override example:**
```bash
CLOUDGATE_REDIS_ADDRS=redis-cluster:6379 \
CLOUDGATE_CASSANDRA_HOSTS=cass1,cass2,cass3 \
./bin/cloudgate-gateway
```

---

## Prometheus Metrics Reference

| Metric | Type | Labels | Description |
|---|---|---|---|
| `cloudgate_gateway_requests_total` | Counter | tenant, method, path, status | Total requests processed |
| `cloudgate_gateway_request_duration_seconds` | Histogram | tenant, method, status | Request latency (p50/p95/p99) |
| `cloudgate_gateway_requests_in_flight` | Gauge | tenant | Active in-flight requests |
| `cloudgate_ratelimit_decisions_total` | Counter | tenant, strategy, decision | Rate limit allow/deny decisions |
| `cloudgate_circuit_breaker_state` | Gauge | upstream | CB state: 0=closed, 1=open, 2=half-open |
| `cloudgate_circuit_breaker_transitions_total` | Counter | upstream, from, to | CB state transitions |
| `cloudgate_bulkhead_rejected_total` | Counter | tenant | Requests rejected by bulkhead |
| `cloudgate_bulkhead_current_occupancy` | Gauge | tenant | Current bulkhead occupancy |
| `cloudgate_upstream_requests_total` | Counter | tenant, upstream, status | Upstream requests forwarded |
| `cloudgate_upstream_request_duration_seconds` | Histogram | tenant, upstream | Upstream latency |
| `cloudgate_upstream_errors_total` | Counter | tenant, upstream, error_type | Upstream errors by type |
| `cloudgate_audit_queue_depth` | Gauge | — | Audit log write queue depth |
| `cloudgate_audit_writes_total` | Counter | status | Cassandra write success/failure |
| `cloudgate_audit_write_duration_seconds` | Histogram | — | Cassandra write latency |
| `cloudgate_redis_command_duration_seconds` | Histogram | command | Redis command latency |
| `cloudgate_redis_errors_total` | Counter | command | Redis errors by command |

---

## gRPC Admin API

Hot-reload tenant policies without restarting the gateway:

```bash
# Install grpcurl
brew install grpcurl

# Update a tenant policy in real time
grpcurl -plaintext -d '{
  "tenant_id": "tenant-acme",
  "upstreams": ["http://new-service:8080"],
  "rps_limit": 200,
  "burst": 400,
  "window_secs": 10,
  "max_in_window": 1000,
  "bulkhead_max": 100,
  "cb_failure_threshold": 5,
  "cb_open_timeout_ms": 10000,
  "cb_success_threshold": 2
}' localhost:50051 admin.v1.AdminService/UpdatePolicy

# List all tenants
grpcurl -plaintext localhost:50051 admin.v1.AdminService/ListTenants
```

---

## Testing

```bash
# Unit tests
make test

# With race detector (always run before merging)
make test-race

# Benchmarks
make bench

# Coverage report (opens HTML in browser)
make cover
```

---

## Deployment (Kubernetes / EKS)

```bash
# Add your kubeconfig, then:
helm upgrade --install cloudgate ./deploy/helm/cloudgate \
  --namespace cloudgate \
  --create-namespace \
  --set image.tag=sha-$(git rev-parse --short HEAD) \
  --set redis.addr=redis-master.redis.svc.cluster.local:6379 \
  --set cassandra.hosts=cassandra.cassandra.svc.cluster.local \
  --atomic --wait

# Verify
kubectl get pods -n cloudgate
kubectl get hpa -n cloudgate
```

The Helm chart ships with:
- **HPA** (min 3, max 20 replicas) on CPU + memory
- **PodDisruptionBudget** (minAvailable: 2)
- **Pod anti-affinity** for HA across nodes
- **Distroless** runtime image (no shell, minimal attack surface)
- **ServiceMonitor** for Prometheus Operator

---

## CI/CD Pipeline

```
Pull Request:
  golangci-lint → go test -race → go build → docker build
  └── Benchmark regression check (fails if >150% slower than baseline)

Push to main:
  All PR checks → docker push (ghcr.io) → helm upgrade → rollout verify

Weekly (Security):
  govulncheck → trivy image scan → gosec static analysis → SARIF upload
```

Branch protection rules on `main`:
- Direct push disabled
- Required reviewers: `@aryaMehta26` (via CODEOWNERS)
- All status checks must pass: `lint`, `test`, `build`, `security`
- Stale review dismissal on new commits

---

## Cassandra Schema

```cql
CREATE TABLE cloudgate.request_audit (
    tenant_id   TEXT,
    request_id  UUID,
    ts          TIMESTAMP,
    method      TEXT,
    path        TEXT,
    status      INT,
    latency_ms  BIGINT,
    client_ip   TEXT,
    PRIMARY KEY ((tenant_id), ts, request_id)
) WITH CLUSTERING ORDER BY (ts DESC)
  AND default_time_to_live = 2592000;  -- 30-day auto-expiry
```

Query recent requests for a tenant:
```cql
SELECT * FROM cloudgate.request_audit
WHERE tenant_id = 'tenant-acme'
  AND ts > toTimestamp(now()) - 1h
LIMIT 100;
```

---

## Contributing

1. Fork the repository
2. Create a branch: `git checkout -b feat/your-feature`
3. Commit: `git commit -m "add weighted round-robin upstream selection"`
4. Push and open a PR — the template will guide you through the checklist

All PRs require review from `@aryaMehta26` (enforced via CODEOWNERS).

---

## License

[MIT](./LICENSE) © 2024 Arya Mehta

---

<div align="center">

Built with Go · Redis · Cassandra · Kubernetes · Prometheus

_If this project helped you, leave a ⭐_

</div>
