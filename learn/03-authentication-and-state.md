# Lesson 3: Authentication, Security & State Management

In real-world applications, APIs protect sensitive resources behind authentication barriers. In this lesson, you will learn how auth schemes work, how to capture dynamic tokens, and how to keep secrets out of version control.

---

## 🎯 Learning Objectives
1. Understand **Bearer Token**, **Basic Auth**, and **API Key** mechanisms.
2. Observe `401 Unauthorized` responses and test protected endpoints.
3. Automatically capture response tokens into session state using `--capture`.
4. Manage credentials securely using `.secrets.yaml` and OS environment variables with automatic secret masking.

---

## 🧠 Core Concept: The Token Lifecycle

API security typically works through temporary tokens:
```
Client (Developer)                             Server
    |                                            |
    |---- 1. POST /auth/login (credentials) ---->|
    |<--- 2. 200 OK (access_token: eyJhbGci...) -|
    |                                            |
    | [Token stored in local session state]      |
    |                                            |
    |---- 3. DELETE /pets/1 -------------------->|
    |        Header: Authorization: Bearer <tok> |
    |<--- 4. 204 No Content (Deleted) -----------|
```

### The Cardinal Rule of API Testing:
> **Never commit passwords, API keys, or tokens to git!**
> Tokens that expire should be captured dynamically by running a login request, and static credentials should live in gitignored `.secrets.yaml` files.

---

## 💻 Hands-On Sandbox Exercises

Ensure `./hit mock 8765 &` is running.

### Exercise 3.1: Hitting a Protected Endpoint Without Auth

In our mock Petstore API, deleting a pet requires authentication. Let's try sending a `DELETE` without credentials:

```bash
./hit DELETE http://127.0.0.1:8765/pets/1
```

**Output**:
```
ad hoc  DELETE http://127.0.0.1:8765/pets/1  →  401 Unauthorized  0 ms  49 B
{
  "error": "missing or invalid authorization header"
}
```

The server blocked the request with `401 Unauthorized` because no `Authorization` header was provided.

---

### Exercise 3.2: Authenticating and Capturing a Token

To authenticate, we send our credentials to `/auth/login`. We also tell `hit` to **capture** the generated `access_token` from the response JSON:

```bash
./hit POST http://127.0.0.1:8765/auth/login \
  -j '{"username": "admin", "password": "password"}' \
  --capture my_token=json.token
```

**Output**:
```
ad hoc  POST http://127.0.0.1:8765/auth/login  →  200 OK  0 ms  68 B
  ↳ my_token = mock-jwt-token-admin
{
  "token": "mock-jwt-token-admin",
  "username": "admin"
}
```

Notice the line `↳ my_token = mock-jwt-token-admin`! `hit` extracted the token and stored it in session state.

---

### Exercise 3.3: Sending an Authenticated Request (`--auth`)

Now that we have a valid token, we can pass it using `--auth bearer:<token>`:

```bash
./hit DELETE http://127.0.0.1:8765/pets/1 \
  --auth bearer:mock-jwt-token-admin
```

**Output**:
```
ad hoc  DELETE http://127.0.0.1:8765/pets/1  →  204 No Content  0 ms  0 B
```

The pet was successfully deleted with `204 No Content`!

---

### Exercise 3.4: Zone Secret Hygiene & Masking

When working inside a zone (e.g. `examples/petstore-zone`), auth is handled automatically:

1. **Committed Server** (`servers/local.yaml`):
   ```yaml
   base_url: http://127.0.0.1:8765
   auth:
     type: bearer
     token: "{{token}}"   # Dynamically resolved
   ```
2. **Gitignored Secrets** (`servers/local.secrets.yaml`):
   ```yaml
   vars:
     admin_password: "super-secret-password"
   ```
3. **Automatic Masking**:
   Whenever `hit` prints headers, logs, or reports, any value originating from a secrets file or sensitive header (`Authorization`, `Cookie`) is automatically redacted:
   ```
   Authorization: Bearer [REDACTED]
   ```

---

## 📝 Practice Quiz & Challenge

1. **Question**: What is the difference between a `401 Unauthorized` and a `403 Forbidden` status code?
   *(Answer: 401 means you are not authenticated or your token is missing/invalid. 403 means the server knows who you are, but you do not have permission to perform that action.)*
2. **Challenge**: Try sending a request with Basic Auth using `--auth basic:admin:secret` to `http://127.0.0.1:8765/echo -v`. Inspect the outgoing `Authorization` header in verbose mode.

---

**Next Up**: In [**Lesson 4: Writing Declarative Assertions & Validations**](04-assertions-and-validations.md), we will learn how to write automated test suites and validate complex JSON schemas!
