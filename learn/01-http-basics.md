# Lesson 1: HTTP Protocols & Request Anatomy

Welcome to your first lesson in API testing! In this lesson, you will learn how HTTP communications work under the hood, how requests and responses are structured, and how to inspect them in terminals.

---

## 🎯 Learning Objectives
By the end of this lesson, you will:
1. Understand the HTTP Request-Response lifecycle.
2. Master the core HTTP verbs (`GET`, `POST`, `PUT`, `DELETE`).
3. Differentiate between transport-level failures and HTTP-level responses.
4. Use `hit` output modes (`hit`, `hit code`, `hit time`, `hit headers`, `hit body`) to inspect API behavior.

---

## 🧠 Core Concept: The HTTP Lifecycle

When an API client communicates with a server:
```
Client (hit)                     Server (hit mock)
    |                                   |
    |---- 1. HTTP Request ------------->|  (Method, URL, Headers, Body)
    |                                   |  [Server processes request]
    |<--- 2. HTTP Response -------------|  (Status Line, Headers, Body)
    |                                   |
```

Every response returns:
1. **Status Line**: HTTP protocol version, 3-digit status code, and reason phrase (e.g. `HTTP/1.1 200 OK`).
2. **Response Headers**: Metadata about the response (e.g. `Content-Type: application/json`, `Content-Length: 39`).
3. **Response Body**: The actual data payload returned (JSON object, HTML text, or empty).

---

## 💻 Hands-On Sandbox Exercises

### Setup: Ensure the Mock API Server is Running
Open a terminal in the project directory:
```bash
./hit mock 8765 &
```

---

### Exercise 1.1: Sending Your First Request (`GET`)

A `GET` request asks the server to retrieve a resource. Run:

```bash
./hit http://127.0.0.1:8765/health
```

**Expected Output**:
```
ad hoc  GET http://127.0.0.1:8765/health  →  200 OK  1 ms  39 B
  < content-length: 39
  < content-type: application/json
  < date: Wed, 09 Sep 2026 23:35:00 GMT
{
  "status": "ok",
  "time": 1788996900.123456
}
```

Notice what `hit` displayed:
- The status line: `200 OK`
- Execution duration: `1 ms`
- Response size: `39 B`
- Headers: denoted by `<`
- Body: formatted, syntax-highlighted JSON

---

### Exercise 1.2: Using Targeted Output Modes

In testing and shell automation, you frequently only care about a specific aspect of the response:

#### 1. Extract Only the Status Code (`hit code`)
```bash
./hit code http://127.0.0.1:8765/health
# Output: 200
```
*Why this matters*: In CI scripts or shell healthchecks, you can write:
```bash
if [ "$(./hit code http://127.0.0.1:8765/health)" = "200" ]; then
  echo "Server is ready!"
fi
```

#### 2. Measure Response Latency (`hit time`)
```bash
./hit time http://127.0.0.1:8765/health
# Output: 0.5ms
```
Want pure numbers for math or alerts? Add `--raw`:
```bash
./hit time http://127.0.0.1:8765/health --raw
# Output: 0.5
```

#### 3. View Headers Only (`hit headers`)
```bash
./hit headers http://127.0.0.1:8765/health
# Output:
# HTTP/1.1 200 OK
# content-length: 39
# content-type: application/json
```

#### 4. Extract Body Only (`hit body`)
```bash
./hit body http://127.0.0.1:8765/health
# Output:
# {
#   "status": "ok",
#   "time": 1788996900.123456
# }
```

---

### Exercise 1.3: Understanding HTTP Errors vs Transport Errors

A common confusion in API testing is the difference between a **Transport Error** and an **HTTP Error Response**.

#### Case A: HTTP 404 Not Found (Server Responded)
Ask for an endpoint that doesn't exist:
```bash
./hit http://127.0.0.1:8765/nonexistent-route
```
Notice the server successfully received your request and returned `404 Not Found`.

Now query just the code:
```bash
./hit code http://127.0.0.1:8765/nonexistent-route
# Output: 404
```
*Notice that the command exited with code 0!* Querying what status code an endpoint returns is an informative query.

#### Case B: Transport Error (Network / DNS / Connection Failure)
Now try connecting to a port where no server is listening:
```bash
./hit http://127.0.0.1:9999/health
```
**Output**:
`error: dial tcp 127.0.0.1:9999: connect: connection refused`

Here, no HTTP response was ever received because the TCP connection could not be established. `hit` exits with code 1.

---

## 📝 Practice Quiz & Challenge

1. **Question**: Is an HTTP `GET` request idempotent? What about `POST`?
   *(Answer: Yes, `GET` is safe and idempotent. `POST` is neither.)*
2. **Challenge**: The mock API has a simulated slow endpoint at `/slow?ms=250`. Run a command using `hit time` to measure the response duration.
   ```bash
   ./hit time http://127.0.0.1:8765/slow
   # Should output ~250ms
   ```

---

**Next Up**: In [**Lesson 2: Parameters, Payloads & Content Negotiation**](02-headers-and-payloads.md), we will learn how to send parameters, construct JSON request bodies, and test RESTful mutations!
