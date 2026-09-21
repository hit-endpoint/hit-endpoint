# Ad Hoc Requests & CLI Ergonomics

`hit` allows you to send requests directly from the terminal without pre-configuring zones or YAML files. It is optimized for speed, shell scripting, and instant inspection.

---

## Output Modes

`hit` features five specialized output modes, named after the specific data they print:

| Command | Output Description |
|---|---|
| `hit [METHOD] URL` (or `hit response`) | Full response: HTTP status line, response headers, and formatted body (default) |
| `hit body [METHOD] URL` | Response body only (pretty-printed JSON or raw text) |
| `hit code [METHOD] URL` | HTTP status code only (e.g. `200`, `404`) |
| `hit time [METHOD] URL` | Elapsed response duration (e.g. `45ms`, `1.2s`) |
| `hit headers [METHOD] URL` | HTTP status line and response headers only |

All output modes can also be activated using flags: `--body`, `--code`, `--time`, or `--headers`.

---

## Ad Hoc Flags & Options

| Option | Description |
|---|---|
| `-H, --header 'K: V'` | Adds a request header (repeatable) |
| `-q, --query 'k=v'` | Adds a query parameter (repeatable) |
| `-j, --json-body '...'` | Sends JSON payload (raw string or `@file.json`) |
| `-b, --body '...'` | Sends raw body text (or `@file.txt`) |
| `-f, --form 'k=v'` | Sends URL-encoded form data (repeatable) |
| `--auth <scheme>` | `bearer:TOKEN`, `basic:USER:PASS`, or `none` |
| `--status N` | Assertion: fails (exit code 1) if status code does not match `N` |
| `--raw` or `--ms` | Outputs raw numeric duration in milliseconds without unit suffix (`hit time`) |
| `--capture name=expr` | Captures a response value into session state for subsequent requests |
| `--json` | Outputs complete structured execution result as JSON |
| `-v, --verbose` | Displays outgoing request details (method, URL, headers, body) |
| `-k, --insecure` | Skips TLS certificate verification |
| `--no-history` | Bypasses logging this request to the `.hit/history.jsonl` log |

---

## Examples

```bash
# Full response (status line, headers, formatted body)
hit https://httpbin.org/get

# Extract just the JSON response body
hit body https://httpbin.org/get -q page=2 -H 'Accept: application/json'

# Status code query
hit code https://httpbin.org/status/404
# 404

# Timing spot check
hit time https://httpbin.org/delay/1
# 1.05s

# Send POST request with JSON payload
hit POST https://httpbin.org/post -j '{"name": "Rex", "type": "dog"}' -H 'X-Trace: 1'

# Send form data with basic auth
hit POST https://httpbin.org/post -f username=admin -f password=secret --auth basic:admin:secret

# Parse complete execution result using jq
hit body https://httpbin.org/json --json | jq .elapsed_ms
```

---

## Key Ergonomics & Scripting Features

### 1. Shell Scripting Friendliness
`hit code` and `hit time` are built specifically for automation, shell health checks, and CI pipeline gates:
* **Informative queries exit with code 0**: Querying an endpoint that returns HTTP 404 or 500 prints the status code (`404`) and exits with code `0`. It only exits with code `1` if an explicit assertion fails (e.g. `--status 200`) or if a network/DNS connection error occurs. This prevents strict shell scripts (`set -e`) from aborting prematurely when probing non-200 responses.
* **Direct shell condition evaluation**:
  ```bash
  # Health check in bash
  if [ "$(hit code https://api.example.com/health)" = "200" ]; then
    echo "Service is operational"
  fi

  # Branching based on HTTP status
  case $(hit code https://api.example.com/items/123) in
    200) echo "Found item" ;;
    404) echo "Item does not exist" ;;
    500) echo "Server error" ;;
    *)   echo "Unexpected status" ;;
  esac
  ```

### 2. Numeric Milliseconds (`--raw` or `--ms`)
By default, `hit time` formats durations with human-readable units (e.g. `45ms`, `1.2s`, `0.4ms`). When piping into math operations, monitoring agents, or graphing tools, pass `--raw` or `--ms` to emit pure numbers:
```bash
hit time https://httpbin.org/get          # "124ms" (human formatted)
hit time https://httpbin.org/get --raw    # "124.5" (numeric milliseconds)

# Latency threshold check in bash
LATENCY=$(hit time https://api.example.com/health --raw)
if (( $(echo "$LATENCY > 200.0" | bc -l) )); then
  echo "Warning: High latency detected ($LATENCY ms)"
fi
```

### 3. Interchangeable Zone Execution
When run from inside a zone directory, all output modifiers work seamlessly on relative URLs and saved request files:
```bash
# Relative URLs (automatically prepends base_url and injects environment auth)
hit code /v1/users/me
hit time /v1/users/me

# Saved zone request specs (runs spec and extracts metric)
hit code pets/list
hit time pets/list
hit body pets/list

# Test execution with hit run
hit run pets/list --code
hit run pets/list --time
```
