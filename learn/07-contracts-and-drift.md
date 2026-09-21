# Lesson 7: OpenAPI Contracts, Coverage & Drift Audits

Modern APIs represent agreements between backend teams, frontend clients, and external third parties. In this lesson, you will learn how to use OpenAPI specifications to audit your test coverage and detect **API Drift** before breaking changes reach production.

---

## 🎯 Learning Objectives
1. Understand the concept of **API Contracts** (OpenAPI 3.x & Swagger 2.0).
2. Define **API Drift** and identify the common risks of undocumented changes.
3. Audit zone test suites against OpenAPI specifications using `hit report coverage`.
4. Enforce 100% contract compliance in CI pipelines using `--strict` gates.
5. Auto-scaffold runnable test collections directly from OpenAPI specs using `hit import`.

---

## 🧠 Core Concept: API Drift

An API contract is a formal specification describing every route, verb, query parameter, and payload schema:

```
OpenAPI Specification                    Live Backend Implementation
(The Documented Contract)                 (What Actually Runs)
        |                                          |
   GET /pets                                  GET /pets
   POST /pets                                 POST /pets
   GET /pets/{id}                             GET /pets/{id}
   GET /pets/{id}/toys                        GET /pets/{id}/toys (Untested!)
                                              GET /health         (Undocumented!)
```

**Two Critical Problems Arise in Real Projects**:
1. **Untested Backlog**: Routes exist in the specification that developers forgot to write automated tests for.
2. **API Drift**: Developers add new endpoints (like `/health` or `/internal/metrics`) or modify status codes without updating the official OpenAPI specification.

---

## 💻 Hands-On Sandbox Exercises

Ensure you are in the project root. We will use the sample OpenAPI spec in `examples/petstore-zone/openapi.yaml`.

### Exercise 7.1: Running a Contract Coverage Audit

Run `hit report coverage` to evaluate how well our zone requests cover the OpenAPI spec:

```bash
./hit -z examples/petstore-zone report coverage \
  --openapi examples/petstore-zone/openapi.yaml
```

**Output**:
```
OpenAPI Contract Coverage & Drift Audit
Specification: examples/petstore-zone/openapi.yaml
Operations in Spec:    5
Covered in Zone:  4
Coverage:              80.0%

Untested Operations (1):
  [ ] GET    /pets/{id}/toys  - Get pet toys

Undocumented Endpoints in Zone (1):
  [!] GET    /health          - petstore/00-health.yaml
```

### What This Audit Tells Us:
1. **80.0% Coverage**: 4 of the 5 documented operations have automated test requests in `collections/`.
2. **Untested Backlog**: `GET /pets/{id}/toys` is defined in the OpenAPI contract, but has zero test coverage!
3. **API Drift**: `GET /health` is implemented and tested in the zone, but was never documented in the OpenAPI specification!

---

### Exercise 7.2: Enforcing Strict Contract Compliance in CI (`--strict`)

In production CI/CD pipelines, you can prevent PRs from merging if they introduce undocumented routes or drop test coverage below 100%.

Add the `--strict` flag:

```bash
./hit -z examples/petstore-zone report coverage \
  --openapi examples/petstore-zone/openapi.yaml \
  --strict
```

`hit` flags the violations and exits with **code 1**, failing the CI build until the team either:
1. Writes a test for `GET /pets/{id}/toys`, AND
2. Adds `GET /health` to `openapi.yaml`.

---

### Exercise 7.3: Emitting Machine-Readable JSON for Dashboards

If you want to feed coverage metrics into internal developer portals, Datadog, or SonarQube, pass `--json`:

```bash
./hit -z examples/petstore-zone report coverage \
  --openapi examples/petstore-zone/openapi.yaml \
  --json
```

**Output**:
```json
{
  "spec_file": "examples/petstore-zone/openapi.yaml",
  "total_operations": 5,
  "covered_operations": 4,
  "coverage_percentage": 80.0,
  "untested_operations": [
    {
      "method": "GET",
      "path": "/pets/{id}/toys",
      "summary": "Get pet toys"
    }
  ],
  "undocumented_routes": [
    {
      "method": "GET",
      "path": "/health",
      "file": "petstore/00-health.yaml"
    }
  ]
}
```

---

### Exercise 7.4: Auto-Scaffolding Tests from OpenAPI (`hit import`)

Don't want to write tests by hand? You can import any local or remote OpenAPI spec directly:

```bash
# Auto-generates collection folders, request YAMLs, bodies, and assertions:
./hit import openapi.yaml
```

`hit` reads the schemas, generates sample JSON request payloads, resolves `$ref` models, and scaffolds status code validation tests automatically!

---

## 🎓 Summary & Graduation

Congratulations! You have completed the entire **API Testing Learning Sandbox** curriculum:
1. Mastered HTTP protocols, status codes, and request anatomy.
2. Learned query/path parameter handling, JSON payloads, and form submissions.
3. Mastered token authentication, dynamic captures, and secret hygiene.
4. Built declarative assertions with JMESPath expressions and negative test patterns.
5. Built stateful multi-step integration flows.
6. Conducted high-throughput load tests and enforced SLO threshold gates.
7. Audited contract coverage and detected API drift using OpenAPI specifications.

You are now equipped with the tools and techniques used by top engineering teams to build rock-solid, high-performance APIs!
