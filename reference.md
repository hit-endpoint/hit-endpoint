# Hit Endpoint file and command reference

Single source of truth for the YAML formats and CLI. Print it any time with `hit reference`.

## Layout of a zone

```
zone.yaml                         name, default_server, vars, shorthands, optional defaults
servers/<server>.yaml             base_url, vars, auth               committed
servers/<server>.secrets.yaml     vars                               gitignored, never commit
shorthands.yaml                   optional named endpoint shortcuts
collections/<collection>/
  _defaults.yaml                  shared settings for this folder and below
  01-first-request.yaml           one request per file; numeric prefix = run order
  sub-folder/_defaults.yaml       folders nest; defaults merge downwards
chains/<chain>.yaml               ordered steps sharing one variable state
scripts/*.py                      optional script hooks
.hit/state/<server>.json          captured variables                 gitignored
```

Request references accept: a path, a collections-relative path with or without `.yaml`,
or a name with numeric prefixes dropped (`pets/list` matches `petstore/pets/01-list.yaml`).
A name alone works when unique in the zone.

## zone.yaml

```yaml
name: Identity
default_server: staging        # used when -e is not given
vars:                               # lowest-priority variable layer
  page_size: 25
defaults:                           # optional, same keys as _defaults.yaml, applies to all collections
  headers: {Accept: application/json}
```

## servers/<server>.yaml

```yaml
base_url: https://accounts.staging.example.com   # relative request urls are joined onto this
vars:
  authTenant: example-staging.auth.com
  client_secret: "{{$env:IDENTITY_CLIENT_SECRET}}"   # OS env var; never paste secrets here
auth:                               # default auth for every request; request may say auth: none
  type: bearer
  token: "{{token}}"                # usually captured by a login request
```

`servers/<server>.secrets.yaml` (gitignored) has the same `vars:` shape; its values are
merged in and masked in output. If `auth` references a variable that is not set yet, the
request is sent without auth and a note is printed.

Auth blocks:

```yaml
auth: {type: bearer, token: "{{token}}", prefix: Bearer}
auth: {type: basic, username: "{{user}}", password: "{{pass}}"}
auth: {type: apikey, key: X-API-Key, value: "{{api_key}}", in: header}   # in: header | query
auth: {type: header, headers: {X-Custom: "{{sig}}"}}
auth: none
auth: inherit                       # default: environment auth, or the nearest _defaults.yaml auth
```

## Request file (collections/**/*.yaml)

Every key except `url` is optional. `method` defaults to GET.

```yaml
name: Create a pet                  # display name; defaults to the file stem
description: Free text.
method: POST
url: "{{base_url}}/pets"            # absolute, or relative to the environment base_url
headers:
  Idempotency-Key: "{{$uuid}}"
  Accept: null                      # null removes a header inherited from _defaults.yaml
query:
  verbose: true
  kind: "{{kind}}"
auth: inherit
body:                               # exactly one of:
  json: {name: "Pet {{$randomInt}}", kind: hamster}     # any JSON-able value
  # raw: "<xml/>"                   with optional sibling  content_type: application/xml
  # form: {username: a, password: b}                      urlencoded
  # multipart: {meta: x, upload: {file: ./fixture.png}}   paths relative to the request file
  # graphql: {query: "query($id: ID!) { pet(id: $id) { name } }", variables: {id: "{{pet_id}}"}}
  # file: ./payload.bin             with optional content_type
matrix:                               # optional data-driven matrix rows or external file
  rows:
    - { kind: dog, expected_status: 201 }
    - { kind: cat, expected_status: 201 }
timeout: 30                         # seconds
follow_redirects: true
verify: true                        # false, or a path to a CA bundle
vars:                               # request-level defaults, overridden by captured state and --var
  kind: dog
tests:                              # list; see Tests
  - status: 201
  - json: {id: {type: integer}}
captures:                           # name -> jmespath over {status, headers, json, text, ms}
  new_pet_id: json.id
  new_pet_location: headers.location
hooks:                              # optional scripts, paths relative to the request file or zone root
  before: scripts/sign.py           # def before(request, ctx): request.headers[...] = ...
  after: scripts/check.py           # def after(result, ctx): assert ...; ctx.set("k", v)
```

A body given as a bare mapping is treated as `json:`. A bare string is `raw:`.

## _defaults.yaml (per folder)

Same keys as a request. `headers`, `query`, `vars`, `captures`, `hooks` merge downwards
(request wins); `auth`, `timeout`, `follow_redirects`, `verify` override; `tests` accumulate,
so a folder-level `tests: [{status: ["2xx", "3xx", "4xx"]}]` applies to every request below it.

```yaml
# Collection: Auth
headers: {Accept: application/json}
auth: {type: bearer, token: "{{token}}"}
vars: {authTenant: "{{authTenant}}"}
```

## Variables

Syntax `{{name}}` in url, headers, query, body, auth, test expectations. A string that is exactly
one placeholder keeps the variable's native type (numbers stay numbers, objects embed).

Priority, lowest to highest: zone vars, server vars and secrets, `_defaults.yaml`
vars, request vars, captured state, flow vars, flow-step vars, `--var key=value`.

Built-ins: `{{$uuid}}` `{{$guid}}` `{{$timestamp}}` (seconds) `{{$timestampMs}}`
`{{$isoTimestamp}}` `{{$randomInt}}` (0-1000) `{{$now:%Y-%m-%d}}` `{{$utcnow:%H:%M}}`
`{{$env:NAME}}` `{{$env:NAME:default}}`.

Unresolved variables fail the request and name every missing variable. `--lenient` leaves them.

## Tests

Each list item is a mapping; keys may be combined. A `name` makes the item report as one line.

```yaml
tests:
  - status: 200                     # int, list [200, 201], or pattern "2xx"
  - max_ms: 500
  - json:                           # key = jmespath into the JSON body, value = matcher
      name: Rex                     # scalar = equals (loose: "200" == 200)
      "items[0].id": {type: integer, gt: 0}
      "length(items)": {gte: 1}
      "data.user.email": {matches: "@example\\.com$"}
      deleted_at: {exists: false}
      errors: {exists: false}       # GraphQL: no top-level errors
  - headers:
      content-type: {contains: json}
      location: {starts_with: /pets/}
  - text: {contains: "ok"}
  - expr: "json.total == length(json.items)"   # jmespath over the whole response object, must be truthy
  - name: Named test
    status: 200
    json: {ok: true}
```

Matcher operators: `equals` `not_equals` `contains` `not_contains` `exists` `type`
(string number integer boolean object array null) `gt` `gte` `lt` `lte` `matches` (regex)
`starts_with` `ends_with` `length` `in` `not_in` `truthy`.

Without tests, a request passes when status < 400. Exit code 0 = all passed, 1 = failures,
2 = configuration error.

### Automatic Assertion Generation (`hit assert`)

Automatically synthesize tests from live responses, history records, or request specs:
```bash
hit assert https://api.example.com/users/1           # infer from live HTTP response
hit assert 1                                         # infer from most recent history entry
hit assert users/get --save                          # update request file with inferred tests
hit assert users/get --append                        # append without replacing existing tests
hit new users/profile -u https://api.example.com/me --infer
hit history save 1 collections/user.yaml --infer
```
Options: `--strict` (exact scalar values), `--no-latency` (omit `max_ms`), `--no-headers` (omit `content-type`), `--depth N` (recursion limit), `--json` (JSON format).

## Captures

`captures: {name: <jmespath>}` evaluated against `{status, headers, json, text, ms, ok}`.
Header names are lower-case: `headers.location`, `headers."x-request-id"`.
`$.a.b` is accepted as shorthand for `json.a.b`. Captured values are written to
`.hit/state/<env>.json` and become variables for every later request in that environment.

## Flows (flows/<name>.yaml)

```yaml
name: Login then list
vars: {kind: cat}                   # flow-scoped, not persisted
steps:
  - request: petstore/auth/login    # any request reference
  - request: petstore/pets/list
    vars: {kind: dog}               # step-scoped
    headers: {X-Debug: "1"}         # any request key may be overridden per step
    tests: [{json: {total: {gte: 1}}}]        # added to the file's tests
    captures: {first_id: json.items[0].id}
    repeat: 3
    continue_on_fail: true
    name: List dogs
  - request: petstore/pets/get
    replace_tests: [{status: 404}]  # replaces the file's tests instead of adding
  - set: {first_pet_id: "{{new_pet_id}}"}     # rendered, stored like a capture
  - python: scripts/enrich.py       # def run(session): ... ; return value is printed
  - sleep: 0.5
  - flow: other-flow                # nest another flow
```

Flows stop at the first failing step unless run with `--keep-going`.

## Commands

```
hit [METHOD] URL [-H k:v] [-q k=v] [-j JSON|@file] [-b TEXT|@file] [-f k=v] [--auth bearer:T|basic:U:P|none]
                          [--status N] [--test EXPR] [--capture NAME=EXPR] [--json] [-v]
                          prints full response (status line, headers, and body) [default]
hit body [METHOD] URL ...        prints just the response body (formatted JSON / raw)
hit code [METHOD] URL ...        prints just the HTTP status code (e.g. 200, 404)
hit time [METHOD] URL [--ms]     prints just the response duration (e.g. 45ms, 1.2s)
hit headers [METHOD] URL ...     prints status line and response headers
hit response [METHOD] URL ...    alias for default full response
hit ls [FOLDER] [--json]            list requests (method, url, ref), chains, servers
hit servers                         list servers
hit show REF [--curl] [--json] [--lang python|php|js|go] [--extract PATH] render with variables resolved (or export code snippet)
hit snippet <REF|URL> [--lang python|php|js|go|all] [--extract PATH] generate runnable Python, JS, PHP, or Go snippet
hit fuzz <REF|URL> [-n N] [--categories CATS] [--fail-on-5xx] [--sarif FILE] [--json] [-v] mutation & crash fuzzing engine
hit validate [REF|FOLDER ...]       load and render every request; report errors, do not send
hit sanity [COLLECTION] [--offline] [--json]   readiness checklist: environment, credentials, files, server
hit policy [DIR|REF] [--strict] [--junit FILE] [--json]   governance linter: assertions, SLA ceilings, secrets hygiene
hit lint [DIR|REF] [--strict] [--junit FILE] [--json]     alias for hit policy
hit run REF... [-e ENV] [--var k=v] [--json] [--body] [--code] [--time] [-v] [--headers] [--quiet] [--full]
                [--fail-fast] [--keep-going] [--no-persist] [--lenient] [-H k:v] [-q k=v]
                [--junit FILE] [--webhook-on-failure URL] [--publish] [--enforce-policy]
                [--format=github|agent] [--html FILE] [--har FILE]
hit perf REF [-c N] [-n N | -d SECONDS] [--workers LIST] [--distribute N] [--rps N] [--warmup N] [--ramp-up S] [--threshold SPEC] [--check] [--junit FILE] [--csv FILE] [--json]
hit perf worker [--port PORT] [--region REGION] [--id ID]
hit cost [<FILE|REF>] [-n CALLS] [--tiers SPEC] [--pricing FILE] [--preset NAME] [--budget AMOUNT]
hit history [ls] [--limit N] [--status N] [--ref PATTERN] [--json]
hit history show <ID|INDEX> [--json]
hit history save <ID|INDEX> <FILE.yaml> [--infer]
hit history clear
hit replay <ID|INDEX> [-e ENV] [--var k=v] [--diff] [--headers] [--body-only] [--json]
hit diff <ID|INDEX> [<ID|INDEX>] [--headers] [--body-only] [--json] [--context N]
hit report html [-o FILE] [-n LIMIT]
hit report har [-o FILE] [-n LIMIT] [--status N] [--ref PATTERN]
hit report coverage --openapi SPEC [--json]
hit report latency [-t THRESHOLD%] [--json]
hit assert <URL|ID|REF> [--save [FILE]] [--append [FILE]] [--strict] [--no-latency] [--no-headers] [--json]
hit graphql <URL|REF> [-q QUERY|@file] [-v VARS|@file] [-o OP] [--introspect] [--sdl] [--fail-on-errors] [--json]
hit sse <URL> [-H k:v] [-n MAX] [-d TIMEOUT] [--event NAME] [--json]
hit ws <URL> [-H k:v] [-m MSG]... [--expect PATTERN] [-d TIMEOUT] [--json]
hit mock [PORT] [--openapi SPEC] [--stateless] [--flaky R] [--rate-limit N/s] [--latency D] [--jitter RANGE] [--auth-expire D] [--corrupt R]
hit mcp                                  run Model Context Protocol (MCP) server over stdio
hit schema [request|chain|zone|graphql <URL>] print JSON Schema or GraphQL SDL
hit vars [set K V | unset K | clear] [--all]
hit new REF [-m METHOD] [-u URL] [--infer]
hit wizard [DIR] [--name NAME] [--url URL] [-y] scaffold a new zone with boilerplate files & folders
hit zone [new|init] [DIR] [-y]           scaffold or initialize a zone
hit import [collection|openapi|curl|env|globals] FILE...
hit init [DIR] [--wizard]
hit learn [verify [1-7|all]] [--mock URL] print curriculum & verify exercise completion
hit reference                       print this document
```

Global options, accepted before or after the subcommand: `-z ZONE` `-s SERVER` `--var k=v` `-k` `--no-color` `--no-history`.

`--json` output of `run` is one object (or a list) with: `name ref ok request{method,url,headers,body}
status reason headers elapsed_ms size json text tests[{name,passed,detail}] captures capture_errors notes error`.

## Dynamic OpenAPI mock server

Load arbitrary OpenAPI 3.x or Swagger 2.0 specifications to dynamically mock any API:

```bash
hit mock 8080 --openapi ./spec.yaml       # Launch dynamic mock server with stateful CRUD
hit mock 8080 --openapi ./spec.yaml --stateless # Purely stateless schema synthesis
```

- **In-Memory CRUD**: Stores created entities from `POST`, returns in `GET`, updates in `PUT`/`PATCH`, and removes in `DELETE`.
- **Status Override**: `Prefer: status=404` or `X-Hit-Mock-Status: 404`
- **Example Selection**: `Prefer: example=dog` or `X-Hit-Mock-Example: dog`
- **Inspector Endpoints**: `GET /__mock/routes`, `GET /__mock/openapi`, `POST /__mock/reset`
- **CORS**: Built-in support for browser frontend apps.

## Chaos mock server

The built-in mock server (`hit mock`) can inject simulated chaos into your test servers:

```bash
hit mock 8765 --flaky 20%           # Random 500/502/503/504 errors on 20% of requests
hit mock 8765 --flaky-status 503    # Restrict flaky errors to specific status code
hit mock 8765 --rate-limit 10/s     # Returns 429 Too Many Requests with Retry-After header
hit mock 8765 --latency 150ms       # Injects 150ms fixed delay
hit mock 8765 --jitter 50ms-300ms   # Injects variable network delay
hit mock 8765 --auth-expire 30s     # Tokens expire after 30s (returns 401 TOKEN_EXPIRED)
hit mock 8765 --corrupt 10%         # Truncates response JSON payloads on 10% of requests
```

### Per-Request Chaos Headers
Trigger or bypass chaos on specific requests without restarting the server:
- `X-Hit-Chaos-Status: 503` (forces specific status)
- `X-Hit-Chaos-Delay: 200ms` (forces latency delay)
- `X-Hit-Chaos-RateLimit: true` (forces 429 rate limit response)
- `X-Hit-Chaos-Corrupt: true` (forces corrupted payload)
- `X-Hit-Chaos-Bypass: true` (bypasses all chaos simulation)

## Multi-language code snippets

Generate runnable code blocks in Python (`requests`), JavaScript (`fetch`), PHP (`curl`), and Go (`net/http`) for any request or URL:

```bash
hit snippet http://127.0.0.1:8765/pets --lang python
hit snippet http://127.0.0.1:8765/pets -m POST -j '{"name":"Rex"}' --lang js --extract 'items[0].id'
hit snippet petstore/pets/list --all
hit show petstore/pets/list --lang go
```

## Fuzz & mutation testing

Execute automated boundary, type confusion, extreme strings, injection probes, null/empty structures, and header mutations to discover 5xx crashes and hangs:

```bash
hit fuzz http://127.0.0.1:8765/pets -m POST -j '{"name":"Rex","age":3}' -n 25
hit fuzz petstore/pets/create -n 50 --fail-on-5xx
hit fuzz petstore/pets/create --categories boundaries,types,injection --json
```

## Interactive lesson verifier

Verify completion of exercises in `learn/` against local zone history and running mock servers:

```bash
hit learn verify 1                       # Verify lesson 1
hit learn verify all                     # Scorecard of all 7 lessons
```

## First-class GraphQL support

Execute ad-hoc queries, check errors, introspect schemas, and export SDL:

```bash
hit graphql https://api.example.com/graphql -q '{ user(id: "1") { name } }'
hit graphql https://api.example.com/graphql -q @query.graphql -v '{"id":"1"}'
hit graphql https://api.example.com/graphql -q '{ badField }' --fail-on-errors
hit graphql https://api.example.com/graphql --introspect
hit schema graphql https://api.example.com/graphql > schema.graphql
```

## Data-driven matrix testing

Run parameterized requests across data variants with scorecard reporting:

```yaml
matrix:
  rows:
    - { name: "Alice", role: "admin", expected: 201 }
    - { name: "Bob", role: "viewer", expected: 201 }
    - { name: "", role: "invalid", expected: 400 }
# or external file: matrix: data/users.csv
body:
  json:
    name: "{{row.name}}"
    role: "{{row.role}}"
tests:
  - status: "{{row.expected}}"
```

## Real-time & streaming testing (SSE & WebSockets)

Benchmark Server-Sent Events (SSE) and test RFC 6455 WebSockets:

```bash
# Server-Sent Events with TTFT (Time-To-First-Token) metrics
hit sse https://api.example.com/stream -n 10
hit sse https://api.example.com/stream -d 15s --event user_updated --json

# Pure Go RFC 6455 WebSocket client
hit ws wss://echo.websocket.events -m "ping" --expect "ping"
hit ws wss://api.example.com/ws -H "Authorization: Bearer token" -m "sub" -d 5s
```

## Endpoint scheduler

Execute scheduled or periodic health checks and assertions:

```bash
hit schedule https://api.example.com/health --every 5s -n 10 --status 200
hit schedule petstore/pets/list -s dev --every 10s --status 200
hit schedule petstore-prod --at 15:30 --every 1m -d 1h
```

## Endpoint shorthands

Configure and run named endpoint presets with pre-configured settings:

```bash
# Define
hit shorthand set petstore-prod https://api.petstore.com/v1/pets -m GET -s prod -H "Authorization: Bearer my-token"

# List and inspect
hit shorthand ls
hit shorthand get petstore-prod

# Invoke directly
hit petstore-prod
hit body petstore-prod
hit code petstore-prod

# Remove
hit shorthand rm petstore-prod
```

## Zone creation wizard

Scaffold a complete boilerplate zone with low friction (no endless questions):

```bash
hit wizard my-zone              # Prompts only for zone name and base URL (with defaults)
hit wizard my-zone -y           # Non-interactive, accepts all defaults immediately
hit zone new my-zone -y         # Subcommand alias

# Inspect pre-flight readiness and missing credentials immediately:
cd my-zone && hit sanity
```

## Synthetic API Monitoring & Incident Alerting

Run Git-native multi-region consensus checks and continuous synthetic monitoring daemons with automated incident alerts to PagerDuty, Slack Block Kit, and Opsgenie:

```bash
# Run immediate multi-region consensus check (defaults: us-east, eu-central, ap-southeast)
hit probe run examples/petstore-zone/probes/health-probe.yaml
hit probe run collections/petstore/00-health.yaml --regions=us-east,eu-central --consensus=2 --sla-latency=500ms

# Run continuous synthetic monitoring daemon
hit probe daemon examples/petstore-zone/probes/health-probe.yaml --interval=30s

# Verify incident alerting credentials and webhook formatting
hit probe test-alert examples/petstore-zone/probes/health-probe.yaml --channel=all
```

Example probe specification (`probes/health-probe.yaml`):

```yaml
name: Petstore Health Synthetic Probe
ref: collections/petstore/00-health.yaml
interval: 30s
regions:
  - us-east
  - eu-central
  - ap-southeast
consensus_threshold: 2
sla:
  max_latency: 500ms
  allowed_status:
    - 200
alerts:
  pagerduty:
    routing_key: "$env:PAGERDUTY_ROUTING_KEY"
    severity: critical
  slack:
    webhook_url: "$env:SLACK_WEBHOOK_URL"
```

## Distributed Cloud Load Testing & Fleet Auto-Discovery

Scale `hit perf` workloads across remote worker clusters or local in-process cores:

```bash
# 1. Start a load worker daemon enrolled in Fleet Hub (heartbeats every 15s)
hit perf worker --port 8989 --region us-east --hub http://hub.internal:8080 --capacity 16

# 2. Run coordinator with dynamic worker auto-discovery from Hub (no manual IP lists)
hit perf collections/checkout.yaml \
  --hub http://hub.internal:8080 --region us-east \
  -c 300 -d 30s --threshold "p99 < 200ms, failed == 0" \
  --junit perf-results.xml --json

# 3. Run coordinator with manual worker IP list
hit perf collections/checkout.yaml \
  --workers "http://worker1:8989,http://worker2:8989,http://worker3:8989" \
  -c 300 -d 30s

# 4. Run multi-worker in-process load generation on local multi-core machines
hit perf collections/checkout.yaml --distribute 4 -c 100 -n 1000
```

## Cloud Fleet Hub, Control Plane & Visual Dashboard (`hit hub`)

Run a self-hosted or cloud-hosted telemetry aggregator, runner control plane, and visual topology web dashboard:

```bash
# Start the fleet telemetry hub (default port: 8080, default storage: ~/.hit/hub)
hit hub --port 8080 --dir ~/.hit/hub

# Require a Personal Access Token or Project Token for ingestion
hit hub --port 8080 --api-key "hit_sec_team123"

# Publish test runs from developer workstations or CI runners to the hub
hit run collections/ --publish=http://127.0.0.1:8080/api/v1/runs

# Open the interactive web dashboard with Fleet Topology mesh in any browser:
# http://127.0.0.1:8080/
```

### Fleet Control Plane REST API
* `POST /api/v1/nodes/register` - Handshake registering worker or daemon with region, capacity, and capabilities.
* `POST /api/v1/nodes/{id}/heartbeat` - Periodic 15s heartbeat updating execution state and metrics.
* `POST /api/v1/nodes/{id}/deregister` - Gracefully offline node on shutdown.
* `GET /api/v1/nodes/{id}/queue` - Worker queue polling endpoint to claim next pending assigned job.
* `POST /api/v1/nodes/{id}/jobs/{jid}/events` - Worker posts real-time execution log events and assertion checks.
* `POST /api/v1/nodes/{id}/jobs/{jid}/complete` - Worker posts job completion status, report, and telemetry run payload.
* `POST /api/v1/dispatch` - Submits a new run dispatch job to a target node, region, or next idle worker.
* `GET /api/v1/dispatch` - Lists active and recent dispatch jobs (`?limit=50`).
* `GET /api/v1/dispatch/{id}` - Returns dispatch job state, duration, logs, and execution report.
* `POST /api/v1/dispatch/{id}/cancel` - Cancels a queued or running dispatch job.
* `GET /api/v1/fleet` - Returns fleet topology grouped by region (`by_region`), role, and state.
* `GET /api/v1/fleet/workers?capability=perf-worker&region=...` - Auto-discovery for coordinators.
* `GET /api/v1/runs` - Query runs with filters (`?owner=...`, `?node=...`, `?type=...`, `?status=...`).
* `POST /api/v1/runs` - Ingests client-redacted telemetry run payload.
* `GET /api/v1/summary` - Aggregated metrics across developers and CI.

## Interactive Fleet Run Dispatch (`hit dispatch`)

Dispatch functional test specs or performance load benchmarks to remote fleet nodes or regional clusters with real-time execution streaming:

```bash
# Dispatch a test collection to a specific node with real-time streaming
hit dispatch collections/petstore/00-health.yaml --hub http://hub.internal:8080 --node worker-east-1

# Dispatch to any idle worker in a specific geographic region
hit dispatch collections/petstore/00-health.yaml --hub http://hub.internal:8080 --region eu-central

# Dispatch an ad-hoc URL target
hit dispatch https://api.acme.com/v1/health --hub http://hub.internal:8080

# Trigger a remote distributed load benchmark
hit dispatch collections/checkout.yaml --hub http://hub.internal:8080 --perf -c 25 -n 1000

# Non-blocking fire-and-forget submission
hit dispatch collections/smoke.yaml --hub http://hub.internal:8080 --no-stream --json
```

---

## API Cost & Retry Estimator (`hit cost`)

Model commercial API costs across tiered pricing brackets and client-side retry amplification:

```bash
# Estimate cost with graduated tiers and retry failure rate
hit cost -n 100k --tiers "10k:0.01,50k:0.005,+:0.002" --retries 3 --failure-rate 5%

# Commercial API preset with token simulation (OpenAI GPT-4o)
hit cost --preset openai-gpt4o -n 50k --tokens 800:200

# Enforce FinOps budget assertion in CI/CD pipelines
hit cost collections/checkout.yaml -n 250k --pricing cost.yaml --budget 500

# Machine-readable JSON output for automated billing reports
hit cost -n 50k --tiers "10k:0.01,+:0.005" --json
```




