# Endpoint Shorthands & Named Presets (`hit shorthand`)

Hit Endpoint allows you to define **Shorthands**—friendly names with pre-configured settings (URLs, methods, target servers, headers, query parameters, and bodies) so you can hit endpoints with minimal typing.

---

## 🚀 Quick Start

### 1. Define a Shorthand
Save an endpoint with a friendly name:

```bash
hit shorthand set petstore-prod https://api.petstore.com/v1/pets \
  -m GET \
  -H "Authorization: Bearer my-prod-token" \
  -d "Production pet list"
```

### 2. Invoke Directly
Run the shorthand with standard `hit` or any dedicated output mode:

```bash
# Full response (status line, headers, formatted JSON body)
hit petstore-prod

# Just the response body
hit body petstore-prod

# Just the HTTP status code
hit code petstore-prod

# Just the execution duration
hit time petstore-prod
```

### 3. Use in Scheduler
Pass shorthands directly to `hit schedule`:

```bash
hit schedule petstore-prod --every 30s -n 10 --status 200
```

---

## 📋 Shorthand Configuration Sources

Hit Endpoint resolves shorthands across three cascading levels:

1. **Global Shorthands (`~/.hit/shorthands.yaml`)**:
   Available across all directories and zones on your machine.
   Created with `hit shorthand set <name> <url> --global`.

2. **Zone Configuration (`zone.yaml` / `zone.yaml`)**:
   Committed directly in your repository under the `shorthands:` key:
   ```yaml
   name: Petstore API
   default_server: dev

   shorthands:
     prod-health: https://api.petstore.com/health
     dev-pets:
       ref: petstore/pets/01-list-pets
       server: dev
     new-order:
       url: https://api.petstore.com/v1/orders
       method: POST
       server: prod
       headers:
         Content-Type: application/json
       body: '{"item_id": 101, "quantity": 1}'
   ```

3. **Zone Shorthands File (`shorthands.yaml`)**:
   Placed at the root of your zone directory.

---

## 🛠️ CLI Management Commands

### List Defined Shorthands
```bash
hit shorthand ls
# or: hit shorthands
```

Displays a formatted table of all available shorthands, their target methods, URLs/refs, target servers, and descriptions.

Pass `--json` for machine-readable output:
```bash
hit shorthand ls --json
```

### Inspect a Shorthand
```bash
hit shorthand get petstore-prod
```

Prints the parsed configuration in YAML or JSON (`--json`).

### Add or Update a Shorthand
```bash
hit shorthand set <NAME> <URL|REF> [options]

Options:
  -m, --method METHOD     HTTP method (GET, POST, PUT, DELETE, etc.)
  -s, --server SERVER     Target server profile (alias: -e, --env)
  -H, --header "K: V"     Header (repeatable)
  -q, --query "K=V"       Query parameter (repeatable)
  -b, --body TEXT         Request body payload
  -d, --description DESC  Human-readable description
  --global, -g            Save globally in ~/.hit/shorthands.yaml
```

### Remove a Shorthand
```bash
hit shorthand rm petstore-prod

# Delete from global configuration
hit shorthand rm petstore-prod --global
```
