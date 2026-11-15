<div align="center">

<br/>

```
 ██████╗██╗      ██████╗ ██╗   ██╗██████╗  ██████╗  █████╗ ████████╗███████╗
██╔════╝██║     ██╔═══██╗██║   ██║██╔══██╗██╔════╝ ██╔══██╗╚══██╔══╝██╔════╝
██║     ██║     ██║   ██║██║   ██║██║  ██║██║  ███╗███████║   ██║   █████╗
██║     ██║     ██║   ██║██║   ██║██║  ██║██║   ██║██╔══██║   ██║   ██╔══╝
╚██████╗███████╗╚██████╔╝╚██████╔╝██████╔╝╚██████╔╝██║  ██║   ██║   ███████╗
 ╚═════╝╚══════╝ ╚═════╝  ╚═════╝ ╚═════╝  ╚═════╝ ╚═╝  ╚═╝   ╚═╝   ╚══════╝
```

**Production-grade distributed API gateway written in Go**

*10,000+ req/sec · Sub-50ms p99 · Zero-downtime policy updates*

<br/>

[![CI](https://github.com/aryaMehta26/cloudgate/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/aryaMehta26/cloudgate/actions/workflows/ci.yml)
[![Security Scan](https://github.com/aryaMehta26/cloudgate/actions/workflows/security.yml/badge.svg)](https://github.com/aryaMehta26/cloudgate/actions/workflows/security.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/aryaMehta26/cloudgate)](https://goreportcard.com/report/github.com/aryaMehta26/cloudgate)
[![codecov](https://codecov.io/gh/aryaMehta26/cloudgate/branch/main/graph/badge.svg)](https://codecov.io/gh/aryaMehta26/cloudgate)
[![Go Version](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/dl/)
[![License: MIT](https://img.shields.io/badge/License-MIT-22c55e.svg)](./LICENSE)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-6366f1.svg)](./CONTRIBUTING.md)

<br/>

[**Quick Start**](#-quick-start) · [**Architecture**](#-architecture) · [**Rate Limiting**](#-rate-limiting) · [**Circuit Breaker**](#-circuit-breaker) · [**Metrics**](#-prometheus-metrics) · [**Deploy**](#-deployment)

</div>

---

## What is CloudGate?

CloudGate is a high-performance, multi-tenant API gateway built for teams running AI inference services, ML endpoints, or any backend that needs hard traffic guardrails. It sits between your clients and your upstreams and enforces five layers of protection on every single request.

```
  Client
    │
    ▼
┌──────────────────────────────────────────────────────────────────────┐
│                        CloudGate Gateway                              │
│                                                                        │
│  ① RequestID ──► ② Logger ──► ③ Recovery ──► ④ Metrics              │
│                                    │                                   │
│                                    ▼                                   │
│                          ⑤ Bulkhead Check ─── 503 if tenant full     │
│                                    │                                   │
│                                    ▼                                   │
│                  ⑥ Token Bucket + Sliding Window ─── 429 if denied   │
│                          (Redis Lua scripts)                           │
│                                    │                                   │
│                                    ▼                                   │
│                        ⑦ Circuit Breaker ─── 502 if open            │
│                                    │                                   │
│                                    ▼                                   │
│                     ⑧ httputil.ReverseProxy ──► Upstream             │
│                                    │                                   │
│                     ┌──────────────┴──────────────┐                   │
│                     ▼                              ▼                   │
│              Prometheus metrics          Cassandra audit log           │
│              (20+ time-series)         (async, < 10ms write)          │
└──────────────────────────────────────────────────────────────────────┘
```

---

## Features

| Feature | Description |
|---|---|
| **Dual rate limiting** | Token bucket (burst absorption) + sliding window (precision) backed by Redis |
| **Distributed counters** | Atomic Lua scripts — safe across all gateway replicas, no TOCTOU races |
| **Circuit breaker** | Closed → Open → Half-Open FSM with configurable thresholds and auto-recovery |
| **Bulkhead isolation** | Per-tenant goroutine semaphore — one tenant's storm can't starve others |
| **Multi-tenant routing** | Per-tenant upstream config, rate limits, and circuit breaker settings |
| **Cassandra audit log** | Async batch writer: sub-10ms latency at 5k writes/sec, 30-day auto-TTL |
| **20+ Prometheus metrics** | Request latency histograms, CB state, bulkhead occupancy, Redis cmd latency |
| **gRPC admin service** | Hot-reload tenant policies with zero gateway restart via protobuf RPC |
| **Kubernetes-native** | Helm chart, HPA (3–20 replicas), PDB, distroless image, ServiceMonitor |
| **Full CI/CD** | GitHub Actions: lint → test → build → Docker push → Helm deploy |
| **Security pipeline** | govulncheck + Trivy image scan + gosec on every PR and weekly |

---

## Performance

Benchmarked on Apple M2 Pro, Redis 7.2, single gateway replica:

```
BenchmarkTokenBucket/allow-10         3,241,887    369 ns/op    0 B/op    0 allocs/op
BenchmarkSlidingWindow/allow-10       2,108,445    567 ns/op    0 B/op    0 allocs/op
BenchmarkGatewayE2E/full_chain-10        89,334  13,298 ns/op  512 B/op   12 allocs/op
```

**30-second load test** (`wrk -t12 -c400 -d30s -H "X-Tenant-ID: tenant-acme"`):

```
Requests/sec:  11,240
Latency p50:    8.2 ms
Latency p95:   28.4 ms
Latency p99:   47.1 ms   ← SLO target: < 50 ms ✓
Transfer/sec:  24.8 MB
Error rate:     0.00%
```

---

## Quick Start

### Prerequisites

```bash
# macOS
brew install go docker grpcurl wrk

# Go 1.26+
go version
```

### 1 — Clone and download dependencies

```bash
git clone git@github.com:aryaMehta26/cloudgate.git
cd cloudgate
go mod download
```

### 2 — Start infrastructure

```bash
make deps-up
# Launches: Redis :6379 · Cassandra :9042 · Prometheus :9091 · Grafana :3000
# First run waits ~60s for Cassandra to initialize
```

### 3 — Start a mock upstream

```bash
# Any HTTP server works
python3 -m http.server 9999
```

### 4 — Run the gateway

```bash
make run-gateway
```

```
{"level":"info","msg":"gateway listening","addr":":8080"}
{"level":"info","msg":"metrics server listening","addr":":9090"}
```

### 5 — Send your first request

```bash
curl -i -H "X-Tenant-ID: tenant-acme" http://localhost:8080/

HTTP/1.1 200 OK
X-Request-ID: 4f3a9c1d-...
```

### 6 — Hit the rate limit

```bash
for i in {1..200}; do
  curl -s -w "%{http_code}\n" -H "X-Tenant-ID: tenant-acme" http://localhost:8080/
done
# 200 200 200 ... 429 429 429  ← rate limited after ~100 requests
```

### 7 — View live metrics

```bash
# Terminal
curl -s http://localhost:9090/metrics | grep cloudgate_gateway

# Browser — Grafana SLO dashboard
open http://localhost:3000   # admin / cloudgate
```

---

## Architecture

### Repository layout

```
cloudgate/
├── cmd/
│   ├── gateway/main.go        # HTTP server entry point
│   └── admin/main.go          # gRPC admin service entry point
│
├── internal/
│   ├── config/                # Viper config: file + env var override
│   ├── ratelimit/             # Token bucket + sliding window (Redis Lua)
│   ├── circuitbreaker/        # Closed / Open / Half-Open FSM
│   ├── bulkhead/              # Channel-based semaphore per tenant
│   ├── router/                # Multi-tenant reverse proxy
│   ├── middleware/            # RequestID · Logger · Recovery · Metrics
│   ├── metrics/               # Prometheus registry (20+ metrics)
│   ├── audit/                 # Async Cassandra batch writer
│   ├── health/                # /healthz and /readyz endpoints
│   └── tenant/                # Redis-backed policy store + gRPC server
│
├── pkg/
│   ├── redis/                 # go-redis/v9 wrapper (pooling, retries)
│   └── cassandra/             # gocql wrapper (token-aware, schema bootstrap)
│
├── proto/admin/               # Protobuf definition + gRPC stubs
│
├── scripts/
│   ├── load_test.sh           # wrk/hey load test with metrics snapshot
│   └── seed_policies.sh       # Seed tenant policies via gRPC
│
├── deploy/
│   ├── helm/cloudgate/        # Helm chart: Deployment, HPA, PDB, Service
│   ├── cassandra/schema.cql   # CQL schema reference
│   ├── prometheus/            # Prometheus scrape config
│   └── grafana/               # Datasource + dashboard provisioning
│
├── .github/
│   ├── workflows/             # ci.yml · cd.yml · security.yml
│   ├── ISSUE_TEMPLATE/        # Bug report + feature request templates
│   ├── CODEOWNERS             # @aryaMehta26 on every PR
│   └── pull_request_template.md
│
├── Dockerfile                 # Multi-stage: Alpine builder → distroless runtime
├── docker-compose.yml         # Local dev stack
├── cloudgate.yaml             # Sample configuration
├── Makefile                   # All developer tasks
└── .golangci.yml              # 20+ linter config
```

---

## Rate Limiting

CloudGate applies **two independent strategies in AND mode** — a request must pass both.

### Token Bucket — burst absorption

```
Capacity: 200 tokens  │ ████████████████████ 200/200
Refill:   100 req/sec │
                       │  t=0s   200 tokens → 200 requests burst allowed
                       │  t=1s    0 tokens  → 100 refilled → 100 more allowed
                       │  t=2s  100 tokens  → 100 more allowed
```

Implemented as an **atomic Redis Lua script** running inside a single Redis command — immune to TOCTOU races across replicas.

```lua
-- Simplified: runs atomically inside Redis
local tokens = HMGET(key, "tokens")
local refilled = tokens + elapsed_seconds * rate
if refilled >= 1 then
    HMSET(key, "tokens", refilled - 1)
    return 1  -- ALLOW
end
return 0  -- DENY
```

### Sliding Window — precision counting

```
Window: 10s, Limit: 500 requests

Redis ZSET:  score=timestamp_ns, member=timestamp_ns
  ZREMRANGEBYSCORE  →  evict timestamps outside window
  ZCARD             →  count remaining = current requests in window
  ZADD              →  add this request if count < limit
```

No approximation, no fixed-window boundary bursts — every request is counted in a true sliding window.

---

## Circuit Breaker

```
                    ┌──────────────────────────────┐
                    │           CLOSED              │  Normal operation.
                    │  failureCount tracks errors   │  All requests pass through.
                    └──────────────┬───────────────┘
                                   │
                    failureCount ≥ failureThreshold (default: 5)
                                   │
                                   ▼
                    ┌──────────────────────────────┐
                    │            OPEN               │  Upstream is failing.
                    │   All requests → 502          │  Zero goroutines wasted.
                    └──────────────┬───────────────┘
                                   │
                    openTimeout elapsed (default: 10s)
                                   │
                                   ▼
                    ┌──────────────────────────────┐
                    │          HALF-OPEN            │  Testing recovery.
                    │  One probe request allowed    │  Success? → CLOSED
                    │  Failure? → back to OPEN      │
                    └──────────────────────────────┘
```

Each state transition fires a Prometheus counter and updates a gauge — visible in Grafana instantly.

### Testing it locally

```bash
# Kill your upstream (Ctrl+C on the python server)
# Send requests — watch the breaker trip:
for i in {1..15}; do
  echo -n "req $i: "
  curl -s -w "%{http_code}\n" -H "X-Tenant-ID: tenant-acme" http://localhost:8080/
  sleep 0.1
done
# req 1-5:  502  (proxy errors, CB recording failures)
# req 6-15: 502  (CB open — instant rejection)

# Check state:
curl -s http://localhost:9090/metrics | grep circuit_breaker_state
# cloudgate_circuit_breaker_state{upstream="localhost:9999"} 1  (1 = open)

# Restart upstream, wait 10s → CB moves to half-open → closed after 2 successes
python3 -m http.server 9999 &
sleep 10
curl -H "X-Tenant-ID: tenant-acme" http://localhost:8080/  # probe
curl -H "X-Tenant-ID: tenant-acme" http://localhost:8080/  # closed!
```

---

## Configuration

`cloudgate.yaml` is the primary config file. All keys can be overridden with environment variables prefixed `CLOUDGATE_`.

```yaml
server:
  addr: ":8080"
  metrics_addr: ":9090"

redis:
  addrs: ["localhost:6379"]
  password: ""
  db: 0

cassandra:
  hosts: ["localhost"]
  keyspace: "cloudgate"

admin:
  grpc_addr: ":50051"

tenants:
  - id: "tenant-acme"
    upstreams:
      - "http://my-ml-service:8080"
    rate_limit:
      requests_per_second: 100  # token bucket refill rate
      burst: 200                # token bucket capacity
      window_seconds: 10        # sliding window length
      max_in_window: 500        # max requests per window
    circuit_breaker:
      failure_threshold: 5      # failures before opening
      open_timeout: "10s"       # how long to stay open
      success_threshold: 2      # successes in half-open to close
    bulkhead_max: 50            # max concurrent requests
```

**Environment override:**
```bash
CLOUDGATE_REDIS_ADDRS=redis-prod:6379 \
CLOUDGATE_CASSANDRA_HOSTS=cass1,cass2,cass3 \
./bin/cloudgate-gateway
```

---

## gRPC Admin API

Update any tenant's policy **live** — no restart, no redeploy, no downtime.

```bash
# Install grpcurl
brew install grpcurl

# Raise rate limit for a tenant
grpcurl -plaintext -d '{
  "tenant_id": "tenant-acme",
  "upstreams": ["http://my-service:8080"],
  "rps_limit": 500,
  "burst": 1000,
  "window_secs": 10,
  "max_in_window": 2000,
  "bulkhead_max": 100,
  "cb_failure_threshold": 5,
  "cb_open_timeout_ms": 10000,
  "cb_success_threshold": 2
}' localhost:50051 admin.v1.AdminService/UpdatePolicy

# Read current config
grpcurl -plaintext -d '{"tenant_id": "tenant-acme"}' \
  localhost:50051 admin.v1.AdminService/GetPolicy

# List all tenants
grpcurl -plaintext localhost:50051 admin.v1.AdminService/ListTenants
```

**How a change reaches every replica.** `UpdatePolicy` validates the policy
(absolute http(s) upstreams, positive limits; otherwise `InvalidArgument`), then
writes it to Redis and publishes the tenant ID on `cg:tenant-updates` in the
same pipeline. Every gateway replica subscribes to that channel at startup,
evicts its cached policy, and drops the tenant's runtime; the next request for
the tenant rebuilds its limiters, circuit breaker, bulkhead, and upstream
proxies from Redis. Tenants created only through the API (not in
`cloudgate.yaml`) are loaded from Redis on their first request. Requests rotate
round-robin across a tenant's upstreams.

The stubs in `proto/admin` are generated (`make proto`, pinned plugin versions
in `make install-tools`), and CI fails if they drift from `admin.proto`.

Or use the seed script to bulk-load:
```bash
./scripts/seed_policies.sh localhost:50051
```

---

## Prometheus Metrics

| Metric | Type | Labels |
|---|---|---|
| `cloudgate_gateway_requests_total` | Counter | tenant, method, path, status |
| `cloudgate_gateway_request_duration_seconds` | Histogram | tenant, method, status |
| `cloudgate_gateway_requests_in_flight` | Gauge | tenant |
| `cloudgate_gateway_response_size_bytes` | Histogram | tenant, status |
| `cloudgate_ratelimit_decisions_total` | Counter | tenant, strategy, decision |
| `cloudgate_circuit_breaker_state` | Gauge | upstream |
| `cloudgate_circuit_breaker_transitions_total` | Counter | upstream, from, to |
| `cloudgate_bulkhead_rejected_total` | Counter | tenant |
| `cloudgate_bulkhead_current_occupancy` | Gauge | tenant |
| `cloudgate_upstream_requests_total` | Counter | tenant, upstream, status |
| `cloudgate_upstream_request_duration_seconds` | Histogram | tenant, upstream |
| `cloudgate_upstream_errors_total` | Counter | tenant, upstream, error_type |
| `cloudgate_audit_queue_depth` | Gauge | — |
| `cloudgate_audit_writes_total` | Counter | status |
| `cloudgate_audit_write_duration_seconds` | Histogram | — |
| `cloudgate_redis_command_duration_seconds` | Histogram | command |
| `cloudgate_redis_errors_total` | Counter | command |

**Useful PromQL queries:**

```promql
# p99 request latency by tenant
histogram_quantile(0.99,
  sum(rate(cloudgate_gateway_request_duration_seconds_bucket[5m])) by (le, tenant)
)

# Rate-limit denial rate
sum(rate(cloudgate_ratelimit_decisions_total{decision="denied"}[1m])) by (tenant)

# Circuit breaker open state
cloudgate_circuit_breaker_state == 1

# Audit write throughput
rate(cloudgate_audit_writes_total{status="success"}[1m]) * 60
```

---

## Testing

```bash
# All unit tests
make test

# Race detector (always use before merging)
make test-race

# HTML coverage report
make cover
open coverage.html

# Benchmarks
make bench

# Save baseline, make a change, compare
make bench-save
# ... make your change ...
make bench-compare

# Full pre-PR check (fmt + vet + lint + test-race)
make check
```

---

## Deployment

### Kubernetes with Helm

```bash
# Prerequisites: kubectl configured, helm 3.x installed

helm upgrade --install cloudgate ./deploy/helm/cloudgate \
  --namespace cloudgate \
  --create-namespace \
  --set image.tag=sha-$(git rev-parse --short HEAD) \
  --set image.repository=ghcr.io/aryaMehta26/cloudgate \
  --set redis.addr=redis-master.redis.svc.cluster.local:6379 \
  --set cassandra.hosts=cassandra.cassandra.svc.cluster.local \
  --atomic \
  --timeout 5m

# Verify
kubectl get pods -n cloudgate
kubectl get hpa -n cloudgate
```

### What the Helm chart deploys

| Object | Config |
|---|---|
| `Deployment` | Rolling update, readiness + liveness probes, hardened security context |
| `HPA` | 3–20 replicas on CPU (70%) and memory (80%) |
| `PodDisruptionBudget` | minAvailable: 2 — safe during node drains |
| `Service` | ClusterIP on ports 8080 (HTTP), 9090 (metrics), 50051 (gRPC) |
| `ServiceMonitor` | Prometheus Operator auto-scrape every 15s |

### Docker

```bash
# Build
docker build -t cloudgate:local .

# Run with local config
docker run -p 8080:8080 -p 9090:9090 \
  -e CLOUDGATE_REDIS_ADDRS=host.docker.internal:6379 \
  -e CLOUDGATE_CASSANDRA_HOSTS=host.docker.internal \
  cloudgate:local
```

---

## CI/CD Pipeline

Every PR triggers:

```
golangci-lint ──► go test -race ──► codecov upload
                       │
                       ▼
               go build (gateway + admin)
                       │
                       ▼
              docker build (amd64 + arm64)
                       │
                  merge to main
                       │
                       ▼
              docker push → ghcr.io
                       │
                       ▼
           helm upgrade → kubernetes
                       │
                       ▼
       kubectl rollout status (verify)
```

Weekly security scan:
```
govulncheck ./...  ──►  Trivy image scan  ──►  gosec static analysis
                              │
                    SARIF uploaded to GitHub Security tab
```

**Branch protection on `main`:**
- Direct push blocked
- Required reviewer: `@aryaMehta26` (CODEOWNERS)
- Required checks: `CI / Lint`, `CI / Test`, `CI / Build`, `Security / Scan`
- Stale reviews dismissed on new commits

---

## Health Endpoints

| Endpoint | Probe | Behaviour |
|---|---|---|
| `GET /healthz` | Liveness | Always 200 — process is running |
| `GET /readyz` | Readiness | 200 if Redis reachable, 503 if degraded |

```bash
curl http://localhost:8080/healthz
# {"status":"ok","uptime_sec":142}

curl http://localhost:8080/readyz
# {"status":"ok","uptime_sec":142,"checks":{"redis":"ok"}}
```

---

## Local Infrastructure

```bash
make deps-up     # start Redis, Cassandra, Prometheus, Grafana
make deps-down   # stop all containers
```

| Service | Port | Purpose |
|---|---|---|
| Redis | 6379 | Rate limit counters, tenant policy cache |
| Cassandra | 9042 | Request audit log |
| Prometheus | 9091 | Metrics storage (scrapes :9090) |
| Grafana | 3000 | Dashboards — admin / cloudgate |

---

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) for the full guide.

**Quick rules:**
- Run `make check` before every PR
- One logical change per commit, present tense, lowercase, no emoji
- Benchmark evidence required for hot-path changes

All PRs require review from `@aryaMehta26`.

---

## License

[MIT](./LICENSE) © 2024 Arya Mehta

---

<div align="center">

**Go · Redis · Cassandra · Kubernetes · Prometheus · gRPC**

<br/>

*Built to survive the traffic that breaks everything else*

</div>
