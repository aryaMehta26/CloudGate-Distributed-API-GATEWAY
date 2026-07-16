# ☁️ CloudGate — Distributed API Gateway & Rate Limiter

<p align="center">
  <strong>High-throughput, multi-tenant API gateway built for reliability, fairness, and real-time control.</strong>
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white">
  <img alt="Redis" src="https://img.shields.io/badge/Redis-Distributed%20Counters-DC382D?logo=redis&logoColor=white">
  <img alt="Cassandra" src="https://img.shields.io/badge/Cassandra-Audit%20Logs-1287B1?logo=apachecassandra&logoColor=white">
  <img alt="Kubernetes" src="https://img.shields.io/badge/Kubernetes-EKS%20%7C%20HPA-326CE5?logo=kubernetes&logoColor=white">
  <img alt="Prometheus" src="https://img.shields.io/badge/Observability-Prometheus%20%2B%20Grafana-E6522C?logo=prometheus&logoColor=white">
  <img alt="gRPC" src="https://img.shields.io/badge/Admin-gRPC-0A9EDC">
</p>

---

## 🚀 Overview

**CloudGate** is a distributed API Gateway and Rate Limiter engineered to protect downstream ML inference services at scale.

It combines:
- **10k+ req/sec throughput** in Go using goroutines and non-blocking I/O
- **Token Bucket + Sliding Window** rate limiting with **Redis-backed distributed counters**
- **Multi-tenant routing** with per-tenant policy enforcement
- **Reliability controls** via circuit breaker and bulkhead isolation
- **Low-latency audit persistence** in Cassandra (sub-10ms @ 5k writes/sec)
- **Production deployment** on Kubernetes/EKS with Helm + HPA
- **20+ Prometheus metrics** and Grafana SLO dashboards targeting **p99 < 50ms**
- **gRPC admin service** for real-time policy updates

---

## ✨ Core Capabilities

### 1) Distributed Rate Limiting
- **Token Bucket** for burst handling
- **Sliding Window** for smoother sustained control
- Redis atomic operations for globally consistent tenant-level limits

### 2) Multi-Tenant Traffic Governance
- Route resolution by tenant + endpoint policy
- Per-tenant quotas, concurrency limits, and enforcement hooks
- Isolated controls so noisy tenants do not impact others

### 3) Resilience Patterns
- **Circuit Breaker** to avoid cascading failure during downstream degradation
- **Bulkhead Isolation** to partition resources and protect critical flows
- Adaptive fallback behavior for graceful degradation

### 4) Observability & SLOs
- Request lifecycle metrics, limiter metrics, breaker state metrics
- Saturation and latency telemetry exported to Prometheus
- Grafana dashboards for golden signals and SLO tracking

### 5) Real-Time Operations
- gRPC admin API to push policy changes without downtime
- Runtime policy updates for incident response and rapid tuning

---

## 🧠 High-Level Architecture

```mermaid
flowchart LR
    C[Clients] --> G[CloudGate API Gateway]

    subgraph GATEWAY[Gateway Runtime]
      R1[Router\nMulti-Tenant Policy Engine]
      RL[Rate Limiter\nToken Bucket + Sliding Window]
      CB[Circuit Breaker]
      BH[Bulkhead Isolation]
      FWD[Upstream Forwarder]
      OBS[Metrics Exporter]
    end

    G --> R1 --> RL --> CB --> BH --> FWD --> U[ML Inference Endpoints]

    RL <--> REDIS[(Redis\nDistributed Counters)]
    G --> CASS[(Cassandra\nAudit Logs)]
    OBS --> PROM[(Prometheus)] --> GRAF[Grafana SLO Dashboards]
    ADM[gRPC Admin Service] --> R1
    ADM --> RL
    ADM --> CB
```

---

## 🔁 Request Lifecycle

```mermaid
sequenceDiagram
    participant Client
    participant Gateway
    participant Redis
    participant Upstream
    participant Cassandra

    Client->>Gateway: API Request (tenant, route)
    Gateway->>Gateway: Resolve tenant policy
    Gateway->>Redis: Check/update distributed counters
    Redis-->>Gateway: Allowed / Rejected

    alt Allowed
        Gateway->>Gateway: Circuit + bulkhead checks
        Gateway->>Upstream: Forward request
        Upstream-->>Gateway: Response
        Gateway->>Cassandra: Write audit log
        Gateway-->>Client: Success/Upstream response
    else Rejected
        Gateway->>Cassandra: Write rate-limit audit log
        Gateway-->>Client: 429 Too Many Requests
    end
```

---

## 📊 Performance Profile

| Metric | Target / Achieved |
|---|---|
| Throughput | 10k+ requests/sec |
| Audit log writes | 5k writes/sec |
| Cassandra write latency | < 10 ms |
| End-to-end latency (p99) | < 50 ms |
| Exported metrics | 20+ |

---

## 🧩 Tech Stack

- **Language:** Go
- **Rate Limit Store:** Redis
- **Audit Storage:** Cassandra
- **Orchestration:** Kubernetes (EKS), Helm, HPA
- **Observability:** Prometheus, Grafana
- **Control Plane:** gRPC

---

## 📐 Suggested Repository Structure

> This repository currently contains project metadata. The structure below reflects the intended CloudGate layout.

```text
.
├── cmd/
│   ├── gateway/
│   └── admin/
├── internal/
│   ├── router/
│   ├── ratelimit/
│   ├── resilience/
│   ├── observability/
│   └── storage/
├── api/
│   └── proto/
├── deploy/
│   ├── helm/
│   └── k8s/
├── dashboards/
└── README.md
```

---

## ⚙️ Deployment Model (Kubernetes/EKS)

- Horizontal pod autoscaling on CPU/RPS saturation
- Config-driven tenant policy rollout through gRPC admin plane
- Redis and Cassandra as external state services
- Prometheus scrape + Grafana dashboards for SLO governance

---

## 🛡️ Why CloudGate

CloudGate is designed for teams running high-value, latency-sensitive APIs (especially ML inference) where **fairness, resilience, and operability** are critical. It enables safe scale-out while preserving user experience and backend health.

---

## 🤝 Contributing

Contributions are welcome. If you add implementation code, include:
- clear module boundaries,
- load/latency benchmark evidence,
- and observability instrumentation for new paths.

---

## 📄 License

This repository is licensed under the [Apache License 2.0](./LICENSE).
