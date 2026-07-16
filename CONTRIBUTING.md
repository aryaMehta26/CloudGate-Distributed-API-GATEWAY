# Contributing to CloudGate

Thank you for taking the time to contribute. This document covers everything you need to know before opening a PR.

---

## Code of Conduct

Be respectful. Disagreements on implementation details are welcome; personal attacks are not.

---

## Getting Started

```bash
# 1. Fork and clone
git clone git@github.com:<your-handle>/cloudgate.git
cd cloudgate

# 2. Install Go 1.22+
brew install go      # macOS
# or download from https://go.dev/dl/

# 3. Install tools
go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.59.1
brew install grpcurl wrk

# 4. Start dependencies and run
make deps-up
make run-gateway
```

---

## Development Workflow

1. **Create a branch** off `main`:
   ```bash
   git checkout -b feat/your-feature-name
   ```

2. **Make your changes** — keep commits small and focused.

3. **Before committing, always run:**
   ```bash
   make fmt          # format code
   make vet          # static analysis
   make lint         # golangci-lint
   make test-race    # tests + race detector
   ```

4. **Write a single-line commit message** in present tense:
   - `add sliding window rate limiter for burst protection`
   - `fix circuit breaker not resetting failure count on close`
   - `increase Cassandra batch size to 200 for higher write throughput`
   - Not: `Fixed bug` or `Update code` or `WIP`

5. **Open a PR** — the template will walk you through the checklist.

---

## Code Standards

### Style
- Follow standard Go conventions (`gofmt`, `goimports`)
- Exported symbols must have godoc comments
- Error messages are lowercase with no trailing punctuation: `"failed to connect to redis"`
- Wrap errors with context: `fmt.Errorf("load policy: %w", err)`

### Tests
- Every new package needs at least one test file
- Use table-driven tests for multiple input cases
- Benchmarks are required for hot-path changes (rate limiter, router)
- Use `miniredis` for Redis tests — never connect to a real Redis in unit tests

### Commits
- One logical change per commit
- Present tense, lowercase, no period, no emoji
- 72 character limit on the subject line

---

## Hot-Path Performance Rules

These packages are in the critical request path. Changes here require benchmark evidence:
- `internal/ratelimit/`
- `internal/router/`
- `internal/circuitbreaker/`
- `internal/bulkhead/`

Run before and after:
```bash
make bench 2>&1 | tee bench-after.txt
benchcmp bench-before.txt bench-after.txt
```

---

## PR Checklist (enforced by template)

- `make lint` passes with no new warnings
- `make test-race` passes
- Self-review completed
- No secrets in committed files
- Benchmark output attached for hot-path changes

All PRs require approval from `@aryaMehta26` via CODEOWNERS.

---

## Reporting Bugs

Open a GitHub Issue with:
1. CloudGate version / git SHA
2. Go version (`go version`)
3. Steps to reproduce
4. Expected vs actual behaviour
5. Relevant log output (with sensitive data redacted)
