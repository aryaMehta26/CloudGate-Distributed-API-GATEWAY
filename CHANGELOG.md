# Changelog

All notable changes to CloudGate are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
Versioning follows [Semantic Versioning](https://semver.org/).

---

## [Unreleased]

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
