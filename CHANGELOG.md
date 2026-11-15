# Changelog

All notable changes to CloudGate are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
Versioning follows [Semantic Versioning](https://semver.org/).

---

## [Unreleased]

### Fixed
- Admin gRPC API: regenerated stubs with protoc; the hand-written `admin.pb.go` did not implement `proto.Message`, so every admin RPC failed to marshal
- `GetPolicy` now returns `window_secs` and `max_in_window`
- Tenant policies no longer expire from Redis after 24 hours while still listed
- Tenants created through the admin API are loaded from Redis instead of rejected with 401
- Policy changes reach every gateway replica via Redis pub/sub (`cg:tenant-updates`)
- Invalid policies (no upstreams, bad URLs, non-positive limits) are rejected instead of panicking the proxy
- Rate limits are keyed by client IP, not IP:port, so new connections cannot bypass them
- Proxy connection errors are counted once, not twice, by the circuit breaker
- `X-Forwarded-For` chains are preserved instead of overwritten with `ip:port`
- Streamed responses (SSE) are flushed through the gateway's response writers
- A Redis outage no longer rejects all traffic as 429; the limiter fails open and counts `decision=error_allowed`
- `requests_total` no longer has an unbounded `path` label
- Bulkhead release is idempotent; the in-flight gauge drops when requests finish
- Audit `Stop` waits for queued events to be written
- `Recovery` re-panics `http.ErrAbortHandler`
- Missing Redis address returns an error instead of panicking
- Committed `go.sum` so clean clones and the Docker build resolve modules

### Changed
- Requests round-robin across all of a tenant's upstreams (previously only the first)
- Each tenant upstream reuses one `ReverseProxy` instead of building one per request
- CI checks that generated gRPC code matches `admin.proto`

### Added
- Token bucket and sliding window rate limiting backed by Redis Lua scripts
- Circuit breaker FSM (Closed / Open / Half-Open) with configurable thresholds
- Bulkhead isolation via per-tenant goroutine semaphore
- Multi-tenant reverse proxy with per-tenant upstream routing
- Async Cassandra audit writer with 5ms batch flushing and 30-day TTL
- gRPC admin service for hot-reloading tenant policies
- 20+ Prometheus metrics across all subsystems
- Grafana SLO dashboard provisioning
- Kubernetes Helm chart with HPA, PDB, and ServiceMonitor
- GitHub Actions CI/CD pipeline (lint, test, build, docker push, helm deploy)
- Trivy image scan and govulncheck security pipeline
- CODEOWNERS enforcing PR review by @aryaMehta26
- Health endpoints: /healthz (liveness) and /readyz (readiness)
- Load test script using wrk/hey with post-run metrics snapshot
- Policy seed script for bulk gRPC tenant configuration

---

## [0.1.0] — 2024

Initial release.
