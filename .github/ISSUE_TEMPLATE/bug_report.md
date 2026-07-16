---
name: Bug Report
about: Something is broken or behaving unexpectedly
title: "bug: "
labels: ["bug", "needs-triage"]
assignees: aryaMehta26
---

## Describe the bug

<!-- A clear and concise description of what is broken. -->

## Environment

| Field | Value |
|---|---|
| CloudGate version / SHA | |
| Go version (`go version`) | |
| OS | |
| Redis version | |
| Cassandra version | |

## Steps to reproduce

1. Start gateway with config: `...`
2. Send request: `curl -H "X-Tenant-ID: ..." ...`
3. See error

## Expected behaviour

<!-- What should have happened. -->

## Actual behaviour

<!-- What actually happened. Include log output (redact sensitive data). -->

```
paste logs here
```

## Metrics at time of incident

```
# curl http://localhost:9090/metrics | grep cloudgate
paste relevant metrics here
```
