# Security Policy

## Supported Versions

| Version | Supported |
|---|---|
| `main` branch | ✅ Active development |
| Tagged releases | ✅ Patch fixes for latest tag |
| Older tags | ❌ No backports |

## Reporting a Vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Email: `aryamehta456@gmail.com`

Include:
1. Description of the vulnerability and impact
2. Steps to reproduce
3. Affected versions / components
4. Suggested fix (if any)

You will receive a response within **48 hours**. If the vulnerability is confirmed, a patch will be released within **7 days** for critical issues.

## Security Scanning

CloudGate runs automated security checks on every PR and weekly:

- **[govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck)** — scans Go dependencies for known CVEs
- **[Trivy](https://github.com/aquasecurity/trivy)** — scans the Docker image for OS-level CVEs (fails on CRITICAL/HIGH)
- **[gosec](https://github.com/securego/gosec)** — static analysis for security anti-patterns in Go code

All findings are uploaded to the GitHub Security tab as SARIF reports.
