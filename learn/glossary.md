# 📖 The Comprehensive API & API Testing Glossary

A definitive reference guide to standard terminology, protocol concepts, architectural patterns, and testing principles used across modern backend and API engineering.

---

## Table of Contents
1. [HTTP Protocols & Anatomy](#1-http-protocols--anatomy)
2. [HTTP Status Codes Reference](#2-http-status-codes-reference)
3. [API Architectures & Contracts](#3-api-architectures--contracts)
4. [Authentication & Security](#4-authentication--security)
5. [Testing Methodologies & Assertion Patterns](#5-testing-methodologies--assertion-patterns)
6. [Performance, Load Testing & Reliability](#6-performance-load-testing--reliability)

---

## 1. HTTP Protocols & Anatomy

### Client-Server Model
A distributed computing architecture where the **Client** (e.g. CLI tool like `hit`, web browser, or mobile app) sends a request to a **Server** hosting API resources, and the server computes and returns a response.

### Request Line / URL Anatomy
A standard HTTP request targets a Uniform Resource Locator (URL) comprised of:
- **Scheme**: Protocol used (`http://` or `https://`).
- **Host**: Server domain name or IP address (e.g. `api.example.com` or `127.0.0.1:8765`).
- **Port**: Networking port (`80` for plain HTTP, `443` for HTTPS, or custom like `8765`).
- **Path**: Hierarchical route locating the resource (e.g. `/v1/pets/42`).
- **Query String**: Key-value parameters following `?`, separated by `&` (e.g. `?status=active&limit=10`).

### HTTP Methods (Verbs)
The action the client intends to perform on the target resource:
* **`GET`**: Retrieve resource representations without altering server state (**Safe** and **Idempotent**).
* **`POST`**: Submit data to create a new subordinate resource or trigger server-side side effects (**Neither Safe nor Idempotent**).
* **`PUT`**: Replace the target resource in its entirety with the request payload (**Idempotent**, not Safe).
* **`PATCH`**: Apply partial modifications to a resource (**Usually not Idempotent**, not Safe).
* **`DELETE`**: Remove the target resource (**Idempotent**, not Safe).
* **`HEAD`**: Identical to `GET`, but requests that the server return only status code and headers with zero response body (**Safe** and **Idempotent**).
* **`OPTIONS`**: Inquires about communication options and permitted HTTP methods for the target resource.

### Idempotency vs Safety
* **Safe Methods**: Calling the method produces zero side effects on server resource state (`GET`, `HEAD`, `OPTIONS`).
* **Idempotent Methods**: Making $N$ identical requests has the exact same side-effect on the server as making 1 request (`GET`, `PUT`, `DELETE`, `HEAD`, `OPTIONS`). `POST` is non-idempotent because calling it 5 times creates 5 separate resources.

### Request and Response Headers
Key-value metadata pairs transmitting protocol-level instructions:
* **`Content-Type`**: Identifies the MIME media type of the payload body (e.g. `application/json`, `application/x-www-form-urlencoded`, `multipart/form-data`, `text/xml`).
* **`Accept`**: Informs the server which media types the client understands and accepts.
* **`Authorization`**: Transmits credentials (e.g. `Bearer <token>` or `Basic <base64>`).
* **`User-Agent`**: Identifies the client software initiating the request.
* **`Idempotency-Key`**: A unique UUID provided by the client allowing servers to deduplicate accidental retry submissions safely.

---

## 2. HTTP Status Codes Reference

Status codes are 3-digit integers categorized into five classes:

### 1xx Informational
* **`100 Continue`**: Server received request headers and client should proceed to send the body.
* **`101 Switching Protocols`**: Server agreed to switch protocols (e.g. HTTP to WebSocket).

### 2xx Success
* **`200 OK`**: Standard successful response for `GET`, `PUT`, `PATCH`, or general execution.
* **`201 Created`**: The request succeeded and a new resource was created (typically for `POST`). Usually accompanied by a `Location` header pointing to the new resource.
* **`202 Accepted`**: Request accepted for processing, but processing has not yet completed (common in asynchronous job queues).
* **`204 No Content`**: Request succeeded, but server deliberately returns an empty body (standard for `DELETE` or state updates).

### 3xx Redirection
* **`301 Moved Permanently`**: Target resource has been permanently assigned a new URI.
* **`302 Found`**: Resource temporarily resides under a different URI.
* **`304 Not Modified`**: Tells the client its cached representation (based on `If-None-Match` or `If-Modified-Since`) is up-to-date; zero body sent.

### 4xx Client Errors
* **`400 Bad Request`**: Server cannot process request due to client error (e.g. malformed JSON syntax, invalid schema).
* **`401 Unauthorized`**: Authentication is missing or invalid. Client must authenticate to access the resource.
* **`403 Forbidden`**: Client is authenticated, but lacks permissions/roles to access the resource.
* **`404 Not Found`**: Server cannot find the requested resource or route.
* **`405 Method Not Allowed`**: Target endpoint exists, but does not support the HTTP verb used (e.g. `POST` on a read-only endpoint).
* **`409 Conflict`**: Request conflicts with current state of server (e.g. duplicate username, version collision).
* **`422 Unprocessable Entity`**: Request syntax was valid (e.g. valid JSON), but semantic validation failed (e.g. required field missing, number out of bounds).
* **`429 Too Many Requests`**: Rate limiting exceeded. Server often provides `Retry-After` header.

### 5xx Server Errors
* **`500 Internal Server Error`**: Generic unhandled exception or crash on the server.
* **`502 Bad Gateway`**: Server received an invalid response from an upstream server (e.g. reverse proxy / API gateway error).
* **`503 Service Unavailable`**: Server is temporarily overloaded or down for maintenance.
* **`504 Gateway Timeout`**: Server acting as gateway timed out waiting for upstream service to respond.

---

## 3. API Architectures & Contracts

### REST (Representational State Transfer)
An architectural style for network applications relying on stateless, client-server communications over standard HTTP verbs targeting identifiable URIs.

### OpenAPI Specification (OAS 3.0 / 3.1)
The industry standard machine-readable definition language (YAML or JSON) for describing RESTful APIs: endpoints, supported verbs, query/path parameters, request payloads, response schemas, and authentication schemes. Formerly known as Swagger 2.0.

### JSON Schema
A vocabulary for annotating and validating JSON documents. Defines property types, required fields, regular expression patterns, and numeric ranges.

### API Drift
The divergence between an API's published specification (OpenAPI spec) and the real-world behavior implemented by backend services. Can manifest as undocumented endpoints, missing query parameters, changed status codes, or payload schema mismatches.

### Contract Testing
Testing that ensures an API implementation strictly honors the agreements declared in its formal contract (OpenAPI specification) without regressions.

---

## 4. Authentication & Security

### Authentication (AuthN) vs Authorization (AuthZ)
* **Authentication**: Verifying *who you are* (identity).
* **Authorization**: Determining *what you are allowed to do* (permissions, scopes, roles).

### Basic Authentication (RFC 7617)
Transmits credentials encoded in Base64: `Authorization: Basic base64(username:password)`. Must only be used over HTTPS.

### Bearer Token Authentication (RFC 6750)
Transmits a security token in the request header: `Authorization: Bearer <token>`. The server or gateway validates the token without needing client credentials on every call.

### JSON Web Token (JWT)
A compact, URL-safe means of representing claims between parties. Formatted as three Base64URL strings separated by dots (`.`):
1. **Header**: Algorithmic signing mechanism (`alg`, `typ`).
2. **Payload**: Token claims (`sub`, `exp`, `roles`, user attributes).
3. **Signature**: Cryptographic proof guaranteeing payload tampering has not occurred.

### Secret Masking
The automated redaction of sensitive credentials (tokens, private keys, passwords) from terminal outputs, logs, and generated test reports to prevent credential leakage into CI/CD logs or screenshots.

---

## 5. Testing Methodologies & Assertion Patterns

### Happy Path vs Unhappy Path (Negative Testing)
* **Happy Path**: Verifying expected successful execution when valid inputs and credentials are provided (e.g. 200 OK, 201 Created).
* **Unhappy Path (Negative Testing)**: Verifying the API gracefully rejects bad inputs, expired tokens, missing parameters, and unauthorized access with correct status codes (400, 401, 403, 404, 422).

### Smoke Testing
A minimal set of non-destructive checks run against a deployed environment to ensure core services are operational before initiating deeper test suites.

### Regression Testing
Rerunning comprehensive test suites against new releases or code commits to confirm existing functionality has not been broken by recent changes.

### Assertion
A programmatic check declaring that a condition *must* hold true. Common API assertion types:
* **Status Assertion**: Verifying HTTP status code matches expected (e.g. `status: 200`, `status: [200, 201]`, `status: "2xx"`).
* **Header Assertion**: Verifying expected headers exist and have correct values (e.g. `Content-Type: application/json`).
* **Body / Schema Assertion**: Verifying payload structure and data types using expression languages (e.g. JMESPath or JSONPath).
* **Latency Assertion**: Verifying execution time completes within an acceptable threshold (e.g. `max_ms: 200`).

### JMESPath
A query language for JSON allowing declarative evaluation, filtering, projection, and extraction of nested objects and arrays (e.g. `items[0].id`, `length(items)`, `total == length(items)`).

---

## 6. Performance, Load Testing & Reliability

### Latency vs Throughput
* **Latency**: The duration required for a single request to travel from client to server, be processed, and return (measured in milliseconds `ms`).
* **Throughput**: The volume of requests a server can successfully process over a unit of time (measured in Requests Per Second, `RPS`).

### Concurrency & Virtual Users (VUs)
The number of parallel client workers simultaneously sending requests to the server.

### Latency Percentiles (p50, p90, p95, p99)
Statistical distributions representing real-world user experiences without distortion from averages:
* **p50 (Median)**: 50% of all requests were faster than this value.
* **p90**: 90% of requests were faster; only 10% experienced higher latency.
* **p95**: 95% of requests were faster; 5% tail latency.
* **p99**: 99% of requests were faster; represents the worst 1% edge-case user experience.

### SLA & SLO Threshold Gates
* **SLA (Service Level Agreement)**: A formal commitment between service provider and client.
* **SLO (Service Level Objective)**: Target performance goals (e.g. "95% of requests must complete under 200ms with error rate < 0.1%").
* **Threshold Gate**: An automated CI check that evaluates SLO metrics and fails the build if performance degrades.

### Thundering Herd & Ramp-Up Staging
* **Thundering Herd**: The sudden, simultaneous arrival of concurrent workers overloading a cold cache or uninitialized connection pool.
* **Ramp-Up**: Gradually increasing concurrency from 1 to $N$ over a designated interval (e.g. `--ramp-up 10`) to smoothly warm up server caches and worker pools.
