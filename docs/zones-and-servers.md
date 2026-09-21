# Zones, Servers & Secrets Management

In Hit Endpoint, a **Zone** organizes requests, target servers, authentication credentials, shorthands, and variables into a version-controlled directory.

Target deployment targets (staging, production, local) are called **Servers**.

---

## Zone Layout

```
my-api-zone/
  zone.yaml                         # Zone name, default server, global vars, shorthands
  servers/
    dev.yaml                        # Base URL, public vars, default auth (committed)
    dev.secrets.yaml                # Secret tokens, API keys (gitignored, NEVER commit)
    prod.yaml
    prod.secrets.yaml               # (gitignored)
  collections/
    auth/
      _defaults.yaml                # Shared headers and auth for this folder and subfolders
      01-login.yaml                 # Request specs (numeric prefix dictates run order)
      02-refresh.yaml
    users/
      _defaults.yaml
      01-list.yaml
      02-create.yaml
  chains/                           # Multi-step scenario user journeys
    smoke.yaml
  shorthands.yaml                   # Optional named endpoint presets
  scripts/                          # Optional Python / shell hooks
    sign_request.py
  .hit/
    state/dev.json                  # Dynamic captured variables (gitignored)
    history.jsonl                   # Execution history log (gitignored)
```

Hit Endpoint discovers the active zone by walking upwards from your current working directory (like Git). You can also explicitly target any zone from anywhere via `-z /path/to/zone`.

---

## Zone Creation Wizard (`hit wizard` / `hit zone new`)

To scaffold a complete, boilerplate zone layout with zero guesswork, run the creation wizard:

```bash
# Interactive wizard (prompts for zone name & base URL, with immediate defaults)
hit wizard my-api-zone

# Non-interactive mode (uses defaults immediately, no prompts)
hit wizard my-api-zone -y

# Alternatively, using the zone subcommand:
hit zone new my-api-zone -y
```

The wizard sets up:
- `zone.yaml` (default server `local`)
- `.gitignore` (safely ignoring `.hit/` and `*.secrets.yaml`)
- `servers/local.yaml` & `servers/production.yaml`
- `servers/local.secrets.example.yaml` & `servers/production.secrets.example.yaml`
- `collections/default/` with `_defaults.yaml`, `00-health.yaml`, and `01-get-sample.yaml`
- `chains/smoke.yaml`
- `shorthands.yaml` (ready-to-run presets)

### Pre-flight Guidance with `hit sanity`

The wizard does not ask a barrage of questions. Instead, immediately run `hit sanity` inside your new zone to inspect readiness and see exactly what information to provide:

```bash
cd my-api-zone
hit sanity
```

`hit sanity` will report actionable steps, such as copying `.secrets.example.yaml` templates to `.secrets.yaml` and supplying your auth tokens or API keys:

```
[Zone: My API]
  Server: local (http://127.0.0.1:8080)
  ✓ zone.yaml loaded
  ✗ secrets file missing: copy local.secrets.example.yaml to local.secrets.yaml and fill it in
    → cp servers/local.secrets.example.yaml servers/local.secrets.yaml
```

---

## Zone Configuration (`zone.yaml`)

```yaml
name: Petstore API
default_server: dev                 # Used when -s is omitted

vars:                               # Global variables (lowest precedence)
  page_size: 25
  default_locale: en_US

defaults:                           # Optional defaults applied across all collections
  headers:
    Accept: application/json
    User-Agent: hit/1.0

shorthands:                         # Named endpoint shortcuts
  health: https://api.petstore.com/health
  list-pets:
    ref: petstore/pets/01-list-pets
    server: dev
```

---

## Servers & Secrets Hygiene

### 1. Servers (Committed)
Create `servers/<name>.yaml` for each target infrastructure server:

```yaml
# servers/dev.yaml
base_url: https://dev-api.example.com
vars:
  tenant: dev-tenant
  client_id: pub_client_abc123      # Public values are committed here
auth:                               # Default authentication scheme for this server
  type: bearer
  token: "{{token}}"                # Replaced by captured login token or secrets file
```

Commit your server YAML files to git. **Never commit credentials, private tokens, or passwords to these files.**

### 2. Private Credentials (Never Committed)
Hit Endpoint provides two complementary mechanisms to inject secrets safely:

#### Option A: Local Secrets File (`.secrets.yaml`)
Create an uncommitted `servers/<name>.secrets.yaml` alongside your server file. Its values merge automatically into the active server variables and are **strictly masked** (`[MASKED]`) across all terminal outputs, reports, and history logs:

```yaml
# servers/dev.secrets.yaml (gitignored)
vars:
  client_secret: sk_dev_987654321
  token: eyJhbGciOi...
```

#### Option B: Host Environment Variables
Reference host environment variables directly within committed server files:

```yaml
vars:
  client_secret: "{{$env:API_CLIENT_SECRET}}"
  admin_pass: "{{$env:ADMIN_PASSWORD:fallback_pass}}"  # Supports fallback defaults
```

#### Ephemeral Token Capture
Tokens that expire (e.g. OAuth 2.0 access tokens) should be captured dynamically rather than copy-pasted:

```yaml
# collections/auth/01-login.yaml
name: Authenticate
method: POST
url: "{{base_url}}/oauth/token"
auth: none
body:
  json:
    client_id: "{{client_id}}"
    client_secret: "{{client_secret}}"
    grant_type: client_credentials
captures:
  token: json.access_token
```

When you execute `hit run auth/login`, the token is stored locally in `.hit/state/<server>.json` and automatically attached to all subsequent calls.

---

## Variables Precedence

Variables can be referenced anywhere (`{{var_name}}`) in URLs, headers, query strings, request bodies, auth blocks, and tests. Variables resolve from lowest to highest priority:

1. `zone.yaml` `vars`
2. Server `vars` (merged with `<server>.secrets.yaml`)
3. Collection folder `_defaults.yaml` `vars`
4. Request file `vars`
5. Captured runtime state (`.hit/state/<server>.json`)
6. Scenario chain `vars` and chain step `vars`
7. CLI command-line overrides (`--var key=value`)

### Built-in Dynamic Generators

| Placeholder | Generated Value |
|---|---|
| `{{$uuid}}` or `{{$guid}}` | Random RFC 4122 version 4 UUID |
| `{{$timestamp}}` | Current UNIX epoch timestamp in seconds |
| `{{$timestampMs}}` | Current UNIX epoch timestamp in milliseconds |
| `{{$isoTimestamp}}` | Current UTC timestamp formatted as ISO 8601 (RFC 3339) |
| `{{$randomInt}}` | Random integer between 1000 and 999999 |
| `{{$now:%Y-%m-%d}}` | Current date/time formatted with strftime tokens |
| `{{$env:NAME}}` | Value of host environment variable `$NAME` |
| `{{$env:NAME:default}}` | Value of `$NAME`, or `default` if unset |

### Variable & Server Management Commands

```bash
# List available servers in current zone
hit servers

# View captured session variables for active server
hit vars

# View full variable resolution hierarchy with layer sources
hit vars --all

# Manually set a captured variable
hit vars set token eyJhbGci...

# Remove a specific captured variable
hit vars unset token

# Clear all captured state for active server
hit vars clear
```

---

## Zone Sanity Check (`hit sanity`)

Before executing test suites or committing changes, run `hit sanity` to execute an automated 8-point pre-flight checklist:

1. **Server & Defaults**: Confirms `zone.yaml` exists, specifies `default_server`, and loads the active server profile.
2. **Credentials & Secrets**: Verifies required secrets files exist, checks that referenced host OS variables (`{{$env:...}}`) are present, and validates auth configuration.
3. **Spec & Inheritance**: Validates YAML syntax across all request files and folder `_defaults.yaml` files.
4. **Variable Resolution**: Scans requests for undefined variables, identifying missing definitions vs values expected from `captures`.
5. **Auth Readiness**: Checks if auth requires dynamic tokens that have not yet been captured.
6. **Chain Integrity**: Validates that all scenario chain steps in `chains/*.yaml` resolve to existing requests.
7. **Git Hygiene**: Verifies that `.gitignore` excludes `*.secrets.yaml` and `.hit/` state files.
8. **Live Connectivity**: Sends a lightweight ping to `base_url` measuring latency (can be skipped with `--offline`).

```bash
hit sanity                          # Run full checklist on default server
hit sanity -s prod                  # Check against production server
hit sanity auth                     # Scope checks to the 'auth' collection folder
hit sanity --offline                # Skip live network ping
hit sanity --offline --json         # Emit machine-readable JSON (exits 0 if ready, 1 on failure)
```
