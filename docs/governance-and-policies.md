# Enterprise Governance & Policy Gates (`hit policy` / `hit lint`)

As API repositories grow across teams and microservices, maintaining consistent contract assertions, latency budgets, and security hygiene becomes critical. 

`hit` includes a built-in static analysis governance engine and runtime enforcement gate that prevents non-compliant API tests and hardcoded secrets from entering your codebase.

---

## Quickstart

```bash
# Audit all requests in the current zone against policies
hit policy

# Alias for policy check
hit lint

# Audit specific folder or request
hit policy collections/users/

# Enable strict mode (treat warnings as pipeline-blocking errors)
hit policy --strict

# Export policy compliance report to JUnit XML for CI test summary tabs
hit policy --junit policy-report.xml

# Output compliance results as machine-readable JSON
hit policy --json

# Runtime enforcement during test execution (fails if response violates policy SLA)
hit run collections/ --enforce-policy
```

---

## Policy Configuration (`policy.yaml`)

Define your team or organization standards in `.hit/policy.yaml`, `policy.yaml` (in the zone root), or directly inside `zone.yaml` under the `policy:` key:

```yaml
# policy.yaml
version: 1

rules:
  # Mandate specific test assertions on every saved endpoint
  require_assertions:
    - status      # Every endpoint must assert HTTP status code
    - latency     # Every endpoint must declare a max latency SLA
    - body        # Every endpoint must assert JSON/body structure

  # Maximum allowed latency ceiling across all test definitions
  max_latency: 2000ms

  # Block insecure TLS configurations (verify: false) in git
  disallow_insecure_tls: true

  # Block hardcoded bearer tokens, JWTs, and plaintext API keys
  disallow_hardcoded_secrets: true

  # Ensure every endpoint has a description documented
  require_description: true

  # Require explicit auth block (e.g. bearer, basic, or none)
  require_auth: true
```

---

## Policy Rules Reference

| Rule | Type | Description |
|---|---|---|
| `require_assertions` | `[]string` | Required assertions (`status`, `latency`, `body`). Flags any request missing these checks. |
| `max_latency` | `duration` | Upper threshold for declared latency assertions. Fails if a test allows e.g. `< 5000ms` when policy specifies `2000ms`. |
| `disallow_insecure_tls` | `bool` | Flags `verify: false` in test files. Enforces HTTPS validation in CI. |
| `disallow_hardcoded_secrets` | `bool` | Scans headers and auth configurations for raw JWT tokens (`ey...`), bearer tokens, and static API keys. |
| `require_auth` | `bool` | Ensures every request explicitly defines its authentication scheme or marks `auth: none`. |
| `require_description` | `bool` | Mandates human-readable descriptions for documentation and cataloging. |

---

## CI/CD Pipeline Integration

### GitHub Actions Pre-Commit & PR Check

```yaml
name: API Governance Gate
on: [pull_request]

jobs:
  policy-check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      - name: Install hit
        run: go install github.com/your-org/hit-endpoint/cmd/hit@latest

      - name: Run Policy Linter
        run: hit policy --strict --junit results/policy-junit.xml

      - name: Publish Test Summary
        uses: dorny/test-reporter@v1
        if: always()
        with:
          name: API Governance
          path: results/policy-junit.xml
          reporter: java-junit
```
