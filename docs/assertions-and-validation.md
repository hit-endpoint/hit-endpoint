# Declarative Assertions & Auto-Generation

`hit` includes a declarative, expressive assertion engine designed for REST, GraphQL, and RPC APIs. Any request without an explicit `tests:` block passes as long as the HTTP status code is below 400.

---

## Test Block Syntax

Tests are defined inside the `tests:` sequence of any request file or flow step:

```yaml
tests:
  # 1. HTTP Status code checks
  - status: 200                       # Exact match
  - status: [200, 201]                # Any of multiple statuses
  - status: "2xx"                     # Status code range

  # 2. Latency / SLA threshold
  - max_ms: 250                       # Fails if response takes > 250ms

  # 3. Header validations
  - headers:
      content-type: { contains: "application/json" }
      x-rate-limit-remaining: { gt: 0 }

  # 4. JSON Body validations (Keys are JMESPath expressions)
  - json:
      name: Rex                       # Direct scalar equality
      "items[0].id": { type: integer, gt: 0 }
      "length(items)": { gte: 1 }
      "items[*].kind": { contains: "dog" }
      deleted_at: { exists: false }
      email: { matches: "^[\\w.-]+@[\\w.-]+\\.[a-z]{2,}$" }

  # 5. Complex Invariant Expressions (over status, headers, json, text, ms)
  - expr: "json.total >= length(json.items)"
  - expr: "ms < 500 && status == 200"

  # 6. Named assertion grouping
  - name: "User profile contains valid contact info"
    status: 200
    json:
      verified: true
      phone: { exists: true }
```

---

## Assertion Operators

| Operator | Syntax Example | Meaning |
|---|---|---|
| `equals` (default) | `name: "Rex"` or `name: { equals: "Rex" }` | Value equals target |
| `not_equals` | `status: { not_equals: 500 }` | Value does not equal target |
| `contains` | `title: { contains: "API" }` | Substring or array element containment |
| `not_contains` | `errors: { not_contains: "timeout" }` | Substring or array does not contain |
| `exists` | `deleted_at: { exists: false }` | Validates field presence or absence |
| `type` | `id: { type: integer }` | Checks type: `string`, `integer`, `number`, `boolean`, `object`, `array`, `null` |
| `gt`, `gte` | `age: { gt: 0, gte: 18 }` | Greater than, greater than or equal to |
| `lt`, `lte` | `price: { lt: 100, lte: 99.99 }` | Less than, less than or equal to |
| `matches` | `uuid: { matches: "^[0-9a-f-]{36}$" }` | Regular expression match |
| `starts_with` | `sku: { starts_with: "PROD-" }` | Prefix match |
| `ends_with` | `filename: { ends_with: ".json" }` | Suffix match |
| `length` | `items: { length: 5 }` | Length of string, array, or object keys |
| `in`, `not_in` | `role: { in: ["admin", "editor"] }` | Value belongs to list |
| `truthy` | `active: { truthy: true }` | Evaluates truthiness (non-empty, non-zero) |

---

## Auto-Generating Assertions (`hit assert`)

Writing assertions manually for complex APIs can be tedious and prone to human error. `hit assert` inspects live API responses, historical records, or zone files and instantly synthesizes a comprehensive, 100%-passing assertion suite:

```bash
# Synthesize assertions from a live endpoint
hit assert https://api.example.com/users/1

# Synthesize assertions from the most recent history entry
hit assert 1

# Update an existing request YAML file with synthesized assertions
hit assert users/get-user --save

# Append new assertions to an existing file without overwriting existing tests
hit assert users/get-user --append

# Scaffold a new request file with live inferred assertions
hit new users/profile -u https://api.example.com/me --infer
```

### Options & Customization

| Flag | Description |
|---|---|
| `--save [FILE]` | Writes generated assertions to the request file |
| `--append [FILE]` | Appends new assertions to existing test block without duplicates |
| `--strict` | Asserts exact scalar literals instead of permissive type/bound checks |
| `--no-latency` | Omits the `max_ms` threshold check |
| `--no-headers` | Omits response header validations |
| `--json` | Outputs synthesized test rules in JSON format |

### What `hit assert` Automatically Infers
- **Status Code**: `status: 200`
- **Latency SLA**: `max_ms: 500` (computed dynamically from actual response time + safety buffer)
- **Content Type**: `content-type: { contains: "application/json" }`
- **Format Regexes**: Auto-detects RFC 4122 UUIDs, ISO-8601 timestamps, and email addresses
- **Types & Bounds**: Numeric bounds (`gte: 0`), non-empty strings, booleans
- **Array Contracts**: Array lengths (`length(items) >= 1`), first-item property validations (`items[0].id`)
- **Relational Invariants**: Discovers count/length invariants (e.g. `json.total >= length(json.items)`)
