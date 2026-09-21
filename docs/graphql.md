# First-Class GraphQL Support & Schema Introspection

`hit` provides native, first-class GraphQL client tooling, schema introspection, and automated GraphQL Schema Definition Language (SDL) export—all without requiring Node.js, `graphql-cli`, or heavy npm packages.

---

## ⚡ Direct GraphQL Querying (`hit graphql`)

Execute ad-hoc or parameterized GraphQL queries and mutations directly against any GraphQL endpoint with syntax-colored formatting and execution timing:

```bash
# Query an endpoint directly
hit graphql https://api.example.com/graphql -q '{ user(id: "42") { id name email } }'

# Pass variables from an inline JSON string or file
hit graphql https://api.example.com/graphql \
  -q 'query GetUser($id: ID!) { user(id: $id) { id name role } }' \
  -v '{"id": "42"}'

# Read query from a .graphql file
hit graphql https://api.example.com/graphql -q @queries/getUsers.graphql

# Pass custom authentication or headers
hit graphql https://api.example.com/graphql \
  -H "Authorization: Bearer my-secret-token" \
  -q '{ me { id username } }'

# Output raw JSON (ideal for jq piping or automation)
hit graphql https://api.example.com/graphql -q '{ me { id } }' --json | jq .data.me.id
```

---

## 🛡️ GraphQL Error Handling (`--fail-on-errors`)

Standard GraphQL servers return HTTP `200 OK` even when business errors occur, delivering errors in a top-level `errors: [...]` array. `hit` inspects response payloads and provides robust error formatting and CI failure triggers:

```bash
# Exit with non-zero status code if errors[] is populated
hit graphql https://api.example.com/graphql -q '{ invalidField }' --fail-on-errors
```

When errors occur, `hit` highlights line/column locations and field paths:
```text
POST https://api.example.com/graphql  →  200 OK  48 ms

✗ GraphQL Errors: (1)
  1. Cannot query field "invalidField" on type "Query". (line 1, col 3) [path: invalidField]
```

---

## 🔍 Schema Introspection & Overview (`--introspect`)

Introspect any GraphQL schema on demand to audit supported queries, mutations, subscriptions, and defined types:

```bash
hit graphql https://api.example.com/graphql --introspect
```

Example Output:
```text
GraphQL Schema Introspection for https://api.example.com/graphql
──────────────────────────────────────────────────
  Queries:       Query
  Mutations:     Mutation
  Subscriptions: Subscription
  Total Types:   34

Defined Types:
  • User                     [OBJECT]
  • Pet                      [OBJECT]
  • Order                    [OBJECT]
  • Status                   [ENUM]
  • CreateUserInput          [INPUT_OBJECT]

💡 Tip: Run 'hit schema graphql https://api.example.com/graphql' or pass '--sdl' to export full GraphQL SDL schema.
```

---

## 📜 SDL Export (`hit schema graphql`)

Export the full standard GraphQL Schema Definition Language (SDL) directly to stdout or save it to a file:

```bash
# Display schema SDL in terminal
hit schema graphql https://api.example.com/graphql

# Export directly to schema.graphql
hit schema graphql https://api.example.com/graphql > schema.graphql

# Pass authentication headers during introspection
hit schema graphql https://api.example.com/graphql -H "Authorization: Bearer token" > schema.graphql
```

---

## 📄 GraphQL in Declarative Request Specs (`body.graphql`)

You can save GraphQL requests inside your zone collection YAML files using the `body.graphql` block:

```yaml
# collections/users/get-profile.yaml
name: Get User Profile
method: POST
url: "{{base_url}}/graphql"
headers:
  Content-Type: application/json
body:
  graphql:
    query: |
      query GetUser($userId: ID!) {
        user(id: $userId) {
          id
          name
          email
          role
        }
      }
    variables:
      userId: "{{user_id}}"

tests:
  - status: 200
  - json:
      data.user.id: "{{user_id}}"
      data.user.role: "admin"
  - max_ms: 250
```

When executing this spec via `hit run collections/users/get-profile`, `hit` automatically packages the query and variables into the standardized `{"query": "...", "variables": {...}}` JSON POST payload.
