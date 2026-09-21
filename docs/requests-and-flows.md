# Saved Requests, Collections & Scenario Flows

In `hit`, requests and integration flows are declarative, plain-text YAML files committed to your Git repository.

---

## Anatomy of a Request File

Request files reside inside `collections/<folder>/<name>.yaml`. All fields except `url` are optional; `method` defaults to `GET`.

```yaml
name: Create a Pet                  # Display name; defaults to filename
description: Provisions a new pet in the catalog.
method: POST
url: "{{base_url}}/pets"            # Relative URLs automatically join base_url

headers:
  Idempotency-Key: "{{$uuid}}"
  X-Client-Version: "2.4.0"

query:
  verbose: true

auth: inherit                       # Options: inherit (default), none, or custom block

body:
  json:                             # Supported: json, raw, form, multipart, graphql, file
    name: "Pet {{$randomInt}}"
    kind: "{{kind}}"
    tags: ["domestic", "friendly"]

timeout: 30                         # Timeout in seconds

vars:
  kind: dog                         # Request-level defaults

tests:                              # Declarative validations
  - status: 201
  - max_ms: 600
  - json:
      id: { type: integer, gt: 0 }
      name: { starts_with: "Pet " }

captures:                           # Extract values into session state
  new_pet_id: json.id
  new_pet_location: headers.location
```

### Folder Inheritance (`_defaults.yaml`)
To avoid repeating headers, variables, or authentication across multiple requests, place a `_defaults.yaml` file in any collection folder. Settings cascade down through nested directories:

```yaml
# collections/pets/_defaults.yaml
headers:
  Accept: application/json
vars:
  page_size: 50
auth:
  type: bearer
  token: "{{token}}"
```

---

## Executing Requests (`hit run`)

```bash
# List all discovered requests and flows in the zone
hit ls

# List requests within a specific folder
hit ls pets

# Execute a single request (using relative path or unique name)
hit run pets/create
hit run create                      # Short name works if unique in the zone

# Execute an entire collection in numeric/alphabetical order
hit run pets

# Execute multiple requests in sequence
hit run auth/login pets/create pets/list

# Render request with all variables resolved without sending
hit show pets/create
hit show pets/create --curl         # Render as runnable cURL command
```

### Execution Control Flags

| Flag | Purpose |
|---|---|
| `-e, --env <name>` | Select target environment (overrides `default_server`) |
| `--var key=value` | Override variable value at runtime (repeatable) |
| `--fail-fast` | Aborts execution immediately upon the first assertion failure |
| `--keep-going` | Continues executing remaining requests even after failures |
| `--no-persist` | Prevents writing captured variables back to `.hit/state/` |
| `--lenient` | Ignores missing variables instead of failing with an error |
| `--body` / `--code` / `--time` | Trims output to body, status code, or response time |
| `-v, --verbose` | Emits complete request and response payload headers |
| `--quiet` | Suppresses pass/fail lines, emitting only final summary |
| `--json` | Emits structured JSON results array |

---

## Stateful Scenario Flows (`flows/*.yaml`)

While collections organize independent requests, **Flows** execute multi-step user journeys and integration lifecycles where state is shared across steps:

```yaml
# flows/pet-lifecycle.yaml
name: Pet CRUD Lifecycle Journey
description: Authenticate, create pet, verify existence, update, delete, and confirm removal.

vars:
  test_species: feline

steps:
  # 1. Authenticate and capture session token
  - request: petstore/auth/login

  # 2. Create entity using captured token and flow variable
  - request: petstore/pets/create
    vars:
      kind: "{{test_species}}"

  # 3. Store the dynamically created ID for subsequent steps
  - set:
      pet_id: "{{new_pet_id}}"

  # 4. Fetch the entity and assert properties
  - request: petstore/pets/get
    tests:
      - json:
          id: "{{pet_id}}"
          kind: "{{test_species}}"

  # 5. Execute custom verification script (optional)
  - script: scripts/validate_audit_log.py

  # 6. Pause before cleanup
  - sleep: 0.5

  # 7. Delete the entity
  - request: petstore/pets/delete

  # 8. Negative verification: Confirm entity is gone (404)
  - request: petstore/pets/get
    name: Verify Pet Was Removed
    replace_tests:
      - status: 404
```

### Running Flows

```bash
hit run pet-lifecycle
hit run flows/pet-lifecycle.yaml -e production
hit perf pet-lifecycle -c 10 -n 50       # Load test complete multi-step flows
```

### Supported Step Types
* `request`: Executes a zone request spec with optional overrides (`vars`, `headers`, `query`, `body`, `tests`, `replace_tests`, `captures`, `repeat`, `continue_on_fail`).
* `set`: Assigns variables into flow execution state.
* `sleep`: Pauses execution for `N` seconds (e.g. `sleep: 1.5`).
* `script` (or `python`): Executes an external Python hook script (`def run(session): ...`).
* `flow`: Nests another flow file.

---

## Script Hooks (`hooks:`)

For complex signing algorithms (e.g. AWS SigV4, HMAC-SHA256) or advanced response validation that YAML cannot express, attach Python scripts via `hooks`:

```yaml
# collections/orders/create.yaml
name: Signed Order Placement
method: POST
url: "{{base_url}}/orders"
hooks:
  before: scripts/sign_hmac.py      # Computes and attaches HMAC signature header
  after: scripts/verify_signature.py # Validates cryptographic receipt
```

Hook scripts define a `run(session)` entrypoint with direct access to method, URL, headers, and body:

```python
# scripts/sign_hmac.py
import hmac, hashlib, time

def run(session):
    timestamp = str(int(time.time()))
    session.headers["X-Timestamp"] = timestamp
    signature = hmac.new(b"secret-key", session.body.encode(), hashlib.sha256).hexdigest()
    session.headers["X-Signature"] = signature
```

---

## Advanced Parameterization & GraphQL

* **Data-Driven Matrix Testing**: Parameterize any request or scenario flow step with multiple data rows via `matrix:` (inline rows or CSV/JSON files). See [**Data-Driven Matrix Testing**](matrix-testing.md).
* **GraphQL Request Specs**: Declare GraphQL queries and variables natively in requests via `body.graphql`. See [**First-Class GraphQL Support**](graphql.md).

