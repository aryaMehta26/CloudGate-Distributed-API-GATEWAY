-- CloudGate Cassandra Schema
-- Run this manually if auto-bootstrap is disabled, or use as a reference.
-- The application bootstraps this schema automatically on startup via cassandra.go.

-- ── Keyspace ──────────────────────────────────────────────────────────────────
-- SimpleStrategy is fine for single-DC dev/staging.
-- For production multi-DC, switch to NetworkTopologyStrategy.

CREATE KEYSPACE IF NOT EXISTS cloudgate
  WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1}
  AND durable_writes = true;

-- ── Request audit log ─────────────────────────────────────────────────────────
-- Partition key:  tenant_id   — all rows for a tenant live on the same node
-- Clustering key: ts DESC     — newest requests first within a partition
-- TTL:            30 days     — automatic expiry, no manual cleanup required

CREATE TABLE IF NOT EXISTS cloudgate.request_audit (
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
  AND default_time_to_live = 2592000
  AND gc_grace_seconds     = 86400
  AND comment = 'Per-tenant request audit log. Partitioned by tenant for efficient range scans.';

-- ── Useful queries ────────────────────────────────────────────────────────────

-- Last 100 requests for a tenant:
-- SELECT * FROM cloudgate.request_audit WHERE tenant_id = 'tenant-acme' LIMIT 100;

-- Requests in the last hour for a tenant:
-- SELECT * FROM cloudgate.request_audit
--   WHERE tenant_id = 'tenant-acme'
--     AND ts > toTimestamp(now()) - 3600000
--   LIMIT 500;

-- Count requests by status for a tenant (requires ALLOW FILTERING for dev only):
-- SELECT status, COUNT(*) FROM cloudgate.request_audit
--   WHERE tenant_id = 'tenant-acme'
--   ALLOW FILTERING;
