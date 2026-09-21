# Universal Importers (`hit import`)

`hit import` allows you to migrate from existing API tools, specifications, or cURL commands into structured, Git-friendly `hit` zones with a single command.

---

## Supported Formats

### 1. OpenAPI 3.0, 3.1 & Swagger 2.0
Import local YAML/JSON specification files or remote HTTP/HTTPS URLs:

```bash
# Import local OpenAPI file
hit import ./openapi.yaml

# Import from remote documentation URL
hit import https://petstore.swagger.io/v2/swagger.json
```

**What is generated**:
* **Tag-Grouped Collections**: Endpoints are organized into subfolders based on OpenAPI tags (e.g. `collections/pets/`, `collections/users/`).
* **Defaults & Base URLs**: Creates `_defaults.yaml` in each folder configuring base URLs and common headers.
* **Variable Placeholders**: Converts `{id}` URL path parameters to `{{id}}` placeholders.
* **Realistic Dummy Bodies**: Analyzes schema types, enums, formats (`uuid`, `date-time`, `email`), and `$ref` pointers to synthesize realistic sample payloads.
* **Automated Assertions**: Scaffolds HTTP status checks (200/201/204), schema validations, and dynamic ID captures.

### 2. Postman Collections & Server Servers
Import exported Postman Collection (v2.0 or v2.1) JSON files and accompanying environment files:

```bash
hit import postman_collection.json postman_environment.json
```

**What is generated**:
* Maps Postman collection folders to `collections/`.
* Converts `:id` and `{{var}}` syntax to `hit` variable format.
* Converts Bearer, Basic, and API Key authentication blocks.
* Imports raw JSON, URL-encoded forms, and multipart request bodies.
* Imports Postman server variables into `servers/`.

### 3. cURL Commands
Convert copied cURL commands (from browser DevTools, terminal history, or documentation) directly into runnable YAML request files:

```bash
hit import curl "curl -X POST https://api.example.com/login -H 'Content-Type: application/json' -d '{\"user\":\"admin\"}'"
```

* Parses HTTP method (`-X`), URL, query strings, headers (`-H`), basic auth (`-u`), bearer tokens, and JSON payloads (`-d`).
* Scaffolds an immediate request YAML ready to run with `hit run`.

### 4. Smart Auto-Detection
You do not need to specify the format manually—`hit import` inspects the file contents to automatically identify whether it is an OpenAPI specification, Swagger schema, or Postman collection:

```bash
hit import my-spec.json
```
