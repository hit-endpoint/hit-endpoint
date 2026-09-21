# Mutation & Fuzz Testing Engine (`hit fuzz`)

`hit fuzz` is an automated mutation and resilience fuzzer designed to expose unhandled backend exceptions, 5xx server crashes, buffer overflows, injection vulnerabilities, and hanging requests.

---

## Quickstart

```bash
# Fuzz an ad-hoc endpoint with up to 50 mutations across all categories
hit fuzz http://127.0.0.1:8765/pets -m POST -j '{"name":"Rex","age":3}'

# Fuzz a saved zone request file
hit fuzz petstore/pets/create -n 25

# Scope fuzzing to specific vulnerability categories
hit fuzz petstore/pets/create --categories boundaries,types,injection

# Fail CI/CD build if any 5xx server crash occurs
hit fuzz petstore/pets/create -n 50 --fail-on-5xx

# Real-time verbose progress of each mutation
hit fuzz petstore/pets/create -v

# Machine-readable JSON output for automated audit logs
hit fuzz petstore/pets/create --json
```

---

## Mutation Categories

`hit fuzz` synthesizes targeted boundary mutations and probe vectors across six distinct vulnerability categories:

### 1. `boundaries` (Extreme Numeric Bounds)
* **MaxInt64 / MaxInt32**: Integer overflow probes (`9223372036854775807`, `2147483647`).
* **MinInt32 / Negative Bounds**: Sub-zero values (`-2147483648`, `-1`).
* **Zero Values**: Division by zero and null-state triggers (`0`, `0.0`).
* **Float Overflow**: Extreme exponents (`1e308`) causing float conversion crashes.

### 2. `types` (Type Confusion)
* **Number where String expected**: Triggers unhandled string-method invocations.
* **String where Number expected**: Triggers unhandled type casting/parsing errors.
* **Array instead of Object**: Root and nested container confusion.
* **Boolean instead of Complex Object**: Triggers property lookup crashes.

### 3. `strings` (String & Buffer Anomalies)
* **Buffer Overflow Probe**: 10,000-character repetitive strings (`AAAA...`).
* **Embedded Null Bytes**: `\x00` characters testing C-string truncation.
* **Format String Probes**: `%s%s%s%n` specifiers testing printf vulnerabilities.
* **Unicode & Control Characters**: Right-to-left override (`\u202E`), zero-width spaces, and emoji floods.
* **Empty Strings**: `""` testing missing mandatory field handlers.

### 4. `injection` (Probe Vectors)
* **SQL Injection**: `' OR 1=1 --`, `admin'--`, `' UNION SELECT NULL--`.
* **Sleep / Time-Based Injection**: `SLEEP(1)`, `pg_sleep(1)`.
* **Cross-Site Scripting (XSS)**: `<script>alert(1)</script>`, `<svg onload=alert(1)>`.
* **Path Traversal**: `../../../../etc/passwd`, `..\\..\\windows\\win.ini`.
* **Command Injection**: `; ls -la`, `| id`, `& calc`.

### 5. `nulls` (Nulls, Collections & Depth)
* **Explicit Null Values**: Setting required keys to `null`.
* **Empty Objects & Arrays**: `{}` and `[]`.
* **Deep Nesting**: Payloads nested 30 levels deep testing JSON stack overflow / recursion limits.
* **Mass Assignment**: Injecting privilege fields (`admin: true`, `role: superuser`).

### 6. `headers` (Protocol & Header Mutations)
* **Oversized Headers**: 16KB header values testing buffer bounds.
* **Corrupt Content-Type**: `application/x-unknown`, invalid boundary delimiters.
* **Malformed Authorization**: Damaged, truncated, or garbage Bearer tokens.

---

## Command Flags

| Flag | Description |
|---|---|
| `-n, --mutations N` | Maximum number of mutations to execute (default: 50) |
| `-c, --categories CATS` | Comma-separated list of categories to test (default: all) |
| `--fail-on-5xx` | Exits with non-zero error code if any 5xx crash is detected |
| `-t, --timeout D` | Maximum per-mutation request timeout (default: 5s) |
| `-v, --verbose` | Emits live status line for every mutation executed |
| `--json` | Emits structured JSON summary and anomaly report |

---

## Anomaly Report & Severity Classification

When anomalies or server crashes are detected, `hit fuzz` groups and displays them with severity ratings and response previews:

```
⚡ hit Mutation & Fuzz Testing Engine
Target: POST http://127.0.0.1:8765/pets
Categories: boundaries, types, injection | Max Mutations: 25
----------------------------------------------------------------------
Total Mutations Executed: 25  (in 4.2ms)
  ✓ Handled (4xx client rejections): 23
  ✓ Accepted (2xx success):          1
  ✓ Transport Errors (hangs/drops):  0
  CRITICAL FAIL: Server Crashes (5xx errors): 1

⚠️ Detected Vulnerabilities & Anomalies:
  1. [CRITICAL] [boundaries] Boundary on "age": Integer MaxInt64 (HTTP 500, 1.2ms)
     Issue: Server returned HTTP 500 Internal Server Error (Unhandled internal error or crash)
     Response Preview: {"error": "integer overflow in query builder"}
```
