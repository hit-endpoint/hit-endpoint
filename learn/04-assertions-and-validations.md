# Lesson 4: Writing Declarative Assertions & Validations

Automated API testing is only as good as the assertions you write. In this lesson, you will learn how to write robust, declarative assertions verifying status codes, headers, response schemas, and execution timings.

---

## 🎯 Learning Objectives
1. Understand the anatomy of an API assertion.
2. Validate HTTP status codes, status ranges (`2xx`), and latency SLAs (`max_ms`).
3. Use JMESPath queries to assert deep JSON payload structures, types, and array lengths.
4. Practice **Negative Testing** to ensure APIs fail gracefully with proper error schemas.

---

## 🧠 Core Concept: Declarative Assertions

Instead of writing imperative code (`if response.status != 200: raise Exception`), modern API testing tools use declarative specifications:

```yaml
tests:
  - status: 200                       # Status code check
  - max_ms: 500                       # Performance SLA
  - headers:                          # Header verification
      content-type: {contains: json}
  - json:                             # Body structure & value assertions
      name: "Rex"                     # Equality
      "items[0].id": {type: integer}  # Type validation
      "length(items)": {gte: 1}       # Array length validation
```

### Supported Comparison Operators
* `equals`, `not_equals`: Exact value matching.
* `type`: Type checks (`string`, `integer`, `number`, `boolean`, `array`, `object`).
* `exists`: Presence check (`{exists: true}` or `{exists: false}`).
* `contains`, `not_contains`: Substring or array membership.
* `gt`, `gte`, `lt`, `lte`: Numeric range comparisons.
* `matches`: Regular expression pattern matching.
* `length`: String, array, or object key length.

---

## 💻 Hands-On Sandbox Exercises

Ensure `./hit mock 8765 &` is running.

### Exercise 4.1: Ad-Hoc Status Assertions (`--status`)

When exploring from the CLI, you can assert expected status codes on the fly:

```bash
# Pass assertion (exits 0):
./hit http://127.0.0.1:8765/health --status 200

# Fail assertion (exits 1):
./hit http://127.0.0.1:8765/health --status 201
```

When an assertion fails, `hit` flags the failure and exits with code `1`:
```
ad hoc  GET http://127.0.0.1:8765/health  →  200 OK  0 ms  39 B
✗ status is 201 (expected 201, got 200)
```

---

### Exercise 4.2: Deep JSON Assertions in YAML

Now let's look at a version-controlled request file in `examples/petstore-zone/collections/petstore/00-health.yaml`:

```yaml
name: Health check
method: GET
url: "{{base_url}}/health"
tests:
  - status: 200
  - max_ms: 200
  - headers:
      content-type: {contains: json}
  - json:
      status: ok
      time: {type: number, gt: 0}
```

Let's execute this test suite:

```bash
./hit -z examples/petstore-zone run health
```

**Output**:
```
✓ petstore/00-health (200 OK, 1ms) [4/4 passed]
```
All 4 assertions passed!

---

### Exercise 4.3: Validating Arrays and Lists

Now run the Pet List request:

```bash
./hit -z examples/petstore-zone run pets/list
```

Notice the tests declared in `collections/petstore/pets/01-list.yaml`:
```yaml
tests:
  - status: 200
  - json:
      items: {type: array}
      "length(items)": {gte: 1}
      "items[0].id": {type: integer}
      "items[0].name": {type: string}
      "items[0].kind": {type: string}
```
JMESPath expressions like `"items[0].id"` allow you to reach deep into nested JSON payloads to assert types and values without writing custom code!

---

### Exercise 4.4: Negative Testing (Testing Unhappy Paths)

Great API testers test what happens when things go wrong:
- Does the API return `400 Bad Request` when required fields are missing?
- Does the error response conform to a standard schema (`{"error": "message"}`)?

Let's inspect how to test a 404 response:

```yaml
name: Get non-existent pet
method: GET
url: "{{base_url}}/pets/99999"
tests:
  - status: 404
  - json:
      error: {contains: "not found"}
```

In `hit`, a status code $\ge 400$ is only treated as a test failure if you *didn't* expect it. When your test asserts `- status: 404`, getting a 404 means your test **PASSED**!

---

## 📝 Practice Quiz & Challenge

1. **Question**: What operator would you use to verify that an array named `users` contains at least 5 items?
   *(Answer: `"length(users)": {gte: 5}`)*
2. **Challenge**: Use `hit show petstore/pets/01-list` to inspect the full rendered YAML spec. Try running it with `--full` to view the raw test evaluation breakdown.

---

**Next Up**: In [**Lesson 5: Stateful Scenario Flows & User Journeys**](05-scenario-flows.md), we will link isolated requests into end-to-end integration scenarios!
