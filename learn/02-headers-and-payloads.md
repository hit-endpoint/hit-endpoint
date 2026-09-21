# Lesson 2: Parameters, Payloads & Content Negotiation

In this lesson, you will learn how to send data to an API using query strings, URL path segments, headers, and request bodies. You will also learn how HTTP content negotiation works.

---

## 🎯 Learning Objectives
1. Distinguish between **Path Parameters**, **Query Parameters**, and **Request Bodies**.
2. Learn how to format and send JSON payloads and urlencoded form data.
3. Understand HTTP request headers and content negotiation (`Content-Type`, `Accept`).
4. Inspect outgoing HTTP requests using verbose mode (`-v`).

---

## 🧠 Core Concept: The Three Ways to Pass Data

When calling an API endpoint, data is transmitted in three distinct locations:

```
                      Query Parameters (?kind=dog&limit=5)
                                  vvvvvvvvvvvvvvvvvvv
POST https://api.example.com/v1/shelters/42/pets?kind=dog
                                         ^^
                                    Path Parameter (/shelters/42)

Headers:
  Content-Type: application/json
  X-Request-ID: req_abc123

Body:
  {"name": "Rex", "age": 3}
```

1. **Path Parameters**: Identifies a specific hierarchy or entity (e.g. `/pets/1`).
2. **Query Parameters**: Modifies or filters the request (e.g. `?limit=10&sort=desc`).
3. **Request Body**: The payload representing new or updated state (JSON, form fields, XML).

---

## 💻 Hands-On Sandbox Exercises

Make sure the mock server is running (`./hit mock 8765 &`).

### Exercise 2.1: Query Parameters (Filtering & Pagination)

Query parameters can be appended directly to the URL or passed cleanly via `-q`:

```bash
# Append to URL:
./hit body http://127.0.0.1:8765/pets?kind=dog

# Or using the -q flag (repeatable):
./hit body http://127.0.0.1:8765/pets -q kind=dog -q limit=2
```

Notice that the mock API filters its dataset and returns only dogs matching the query!

---

### Exercise 2.2: Path Parameters (Resource Identity)

Path parameters locate a single entity. Let's fetch Pet #1:

```bash
./hit http://127.0.0.1:8765/pets/1
```

Now try fetching a pet ID that doesn't exist:
```bash
./hit code http://127.0.0.1:8765/pets/999
# Output: 404
```

---

### Exercise 2.3: Sending JSON Payloads (`POST`)

To create a new resource, send an HTTP `POST` with a JSON body using the `-j` flag:

```bash
./hit POST http://127.0.0.1:8765/pets \
  -j '{"name": "Luna", "kind": "cat", "age": 2}'
```

**Expected Response**:
```
ad hoc  POST http://127.0.0.1:8765/pets  →  201 Created  1 ms  64 B
  < content-type: application/json
{
  "age": 2,
  "id": 4,
  "kind": "cat",
  "name": "Luna"
}
```

Notice the server returned `201 Created` and generated a unique `id` for Luna!

---

### Exercise 2.4: Inspecting What Was Sent (`-v` Verbose Mode)

To see the exact HTTP request `hit` formatted and sent over the wire, add `-v`:

```bash
./hit POST http://127.0.0.1:8765/pets \
  -j '{"name": "Barnaby", "kind": "hamster"}' \
  -H 'X-Client-Version: 2.0.0' \
  -v
```

**Look at the output**:
```
ad hoc  POST http://127.0.0.1:8765/pets  →  201 Created  1 ms  68 B
  > POST /pets HTTP/1.1
  > Host: 127.0.0.1:8765
  > Content-Type: application/json
  > X-Client-Version: 2.0.0
  {"kind":"hamster","name":"Barnaby"}
```
- Lines prefixed with `>` represent the outgoing request sent by the client.
- Lines prefixed with `<` represent the incoming response returned by the server.

---

### Exercise 2.5: URL-Encoded Forms (`-f`)

Some legacy or authentication endpoints expect `application/x-www-form-urlencoded` data instead of JSON. Use `-f`:

```bash
./hit POST http://127.0.0.1:8765/echo \
  -f grant_type=password \
  -f username=developer \
  -v
```

In the verbose output, notice that `hit` automatically set `Content-Type: application/x-www-form-urlencoded`!

---

## 📝 Practice Quiz & Challenge

1. **Question**: If you want to update *only* the `age` of a pet without re-sending the whole object, should you use `PUT` or `PATCH`?
   *(Answer: `PATCH`. `PUT` replaces the entire representation.)*
2. **Challenge**: Create a pet named "Rocky" (kind: "dog") using `-j`. Then use `hit body` to fetch `/pets` with `-q kind=dog` and verify Rocky is present in the list!

---

**Next Up**: In [**Lesson 3: Authentication, Security & State Management**](03-authentication-and-state.md), we will tackle token-based security, OAuth workflows, and automated token capture!
