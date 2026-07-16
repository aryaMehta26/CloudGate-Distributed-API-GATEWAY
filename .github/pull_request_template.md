## What does this PR do?

<!-- A clear, concise description of the change and why it was made. -->

## Type of change

- [ ] Bug fix (non-breaking change that fixes an issue)
- [ ] New feature (non-breaking change that adds functionality)
- [ ] Breaking change (fix or feature that causes existing functionality to change)
- [ ] Refactor (code restructure without behavior change)
- [ ] Performance improvement
- [ ] Documentation / config update

## Related issue(s)

<!-- Link to the issue(s) this PR addresses. -->
Closes #

## How was this tested?

- [ ] Unit tests added / updated (`make test`)
- [ ] Race-detector tests pass (`make test-race`)
- [ ] Manually tested locally (`make deps-up && make run-gateway`)
- [ ] Benchmark results attached (if performance-sensitive path)

## Performance impact

<!-- If touching the hot path (rate limiter, router, circuit breaker), include before/after benchmark output. -->

```
Before:
<paste benchmark output>

After:
<paste benchmark output>
```

## Checklist

- [ ] `make lint` passes without new warnings
- [ ] `make vet` passes
- [ ] `make fmt` applied (code is formatted)
- [ ] `make tidy` applied (go.mod / go.sum are clean)
- [ ] Self-review completed
- [ ] Comments added for non-obvious logic
- [ ] No secrets, credentials, or PII committed
- [ ] Breaking changes documented in commit message
