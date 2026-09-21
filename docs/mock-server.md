# Mock Server, Chaos Engineering & Dynamic OpenAPI Mocker

`hit` includes a built-in, zero-dependency mock API server (`hit mock`) embedded directly in the compiled binary. It runs completely offline without requiring Python, Node.js, Docker, or external cloud services.

---

## 1. Built-in Sandbox Mock API

Start the default sandbox server on port 8765:

```bash
hit mock 8765 &
```

### Pre-Configured Endpoints
* `POST /auth/login`: Authenticates user credentials (`username: admin`) and issues Bearer access tokens.
* `GET /pets`: Returns paginated pet collections (supports `?kind=dog` and emits `X-Total-Count`).
* `POST /pets`: Creates pets with automatic ID assignment.
* `GET /pets/{id}`: Retrieves pet by ID (returns 404 if not found).
* `DELETE /pets/{id}`: Deletes pet from in-memory state.
* `GET /health`: Instant health check endpoint (200 OK).
* `GET /slow?delay=500ms`: Latency simulator for timeout testing.
* `POST /echo`: Mirrors request headers and payload.

---

## 2. Chaos Engineering Simulation

Simulate real-world production turbulence, transient network errors, rate limits, and expired credentials to test client resilience:

```bash
# Randomly fail 25% of requests with 500/502/503/504 errors
hit mock 8765 --flaky 25%

# Restrict flaky errors to a specific code (e.g. 503 Service Unavailable)
hit mock 8765 --flaky 25% --flaky-status 503

# Enforce rate limiting (returns 429 Too Many Requests with Retry-After header)
hit mock 8765 --rate-limit 10/s

# Inject fixed network latency
hit mock 8765 --latency 150ms

# Inject variable network jitter
hit mock 8765 --jitter 50ms-300ms

# Expire authentication tokens after 30 seconds (returns 401 TOKEN_EXPIRED)
hit mock 8765 --auth-expire 30s

# Corrupt 10% of response JSON payloads to test deserialization error handling
hit mock 8765 --corrupt 10%
```

### Per-Request Chaos Headers (`X-Hit-Chaos-*`)
Target or bypass chaos on individual steps without restarting the server:

| Header | Example | Behavior |
|---|---|---|
| `X-Hit-Chaos-Status` | `503` | Forces exact HTTP status code |
| `X-Hit-Chaos-Delay` | `250ms` | Injects custom delay before responding |
| `X-Hit-Chaos-RateLimit` | `true` | Forces immediate 429 Too Many Requests |
| `X-Hit-Chaos-Corrupt` | `true` | Truncates/corrupts response payload |
| `X-Hit-Chaos-Bypass` | `true` | Completely bypasses active chaos simulation |

---

## 3. Dynamic OpenAPI Mock Server (`--openapi`)

Instantly spin up an interactive mock server directly from any **OpenAPI 3.0**, **OpenAPI 3.1**, or **Swagger 2.0** specification (local file or URL) without writing a single line of backend code:

```bash
# Mock from a local specification with in-memory stateful CRUD
hit mock 8080 --openapi ./openapi.yaml

# Mock from a remote specification URL
hit mock 8080 --openapi https://api.example.com/openapi.json

# Combine dynamic OpenAPI mocking with chaos injection
hit mock 8080 --openapi ./openapi.yaml --rate-limit 20/s --flaky 10%

# Run purely stateless without in-memory CRUD persistence
hit mock 8080 --openapi ./openapi.yaml --stateless
```

### Key Capabilities
* **Smart Schema Synthesis**: Generates realistic dummy data matching types, string formats (`uuid`, `date-time`, `date`, `email`, `uri`, `ipv4`), enums, numeric bounds (`minimum`/`maximum`), and recurses complex `$ref` pointers and `allOf`/`oneOf` compositions.
* **Declared Example Extraction**: Automatically serves declared `example` or multi-item `examples` from media schemas.
* **Stateful In-Memory CRUD**:
  - `POST /items`: Parses payload, generates new ID, stores entity in memory, returns `201 Created`.
  - `GET /items`: Returns stored entities array with `X-Total-Count` header.
  - `GET /items/{id}`: Returns exact matching stored entity.
  - `PUT`/`PATCH /items/{id}`: Applies updates to stored entity.
  - `DELETE /items/{id}`: Removes entity, returning `204 No Content`.
* **Client Overrides**:
  - Force status code: `-H "Prefer: status=404"` or `-H "X-Hit-Mock-Status: 404"` or `?mock_status=404`
  - Choose named example: `-H "Prefer: example=dog"` or `-H "X-Hit-Mock-Example: dog"`
* **Browser-Ready CORS**: Automatic preflight `OPTIONS` handling and permissive CORS headers on all routes.
* **Live Inspection Endpoints**:
  - `GET /__mock/routes`: JSON catalog of all registered routes, HTTP methods, and summaries.
  - `GET /__mock/openapi`: Returns the raw or parsed specification.
  - `POST /__mock/reset`: Resets in-memory CRUD state back to initial seed data.
