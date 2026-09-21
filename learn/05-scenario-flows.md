# Lesson 5: Stateful Scenario Flows & User Journeys

In microservice and cloud architectures, bugs rarely occur in isolated endpoints; they happen across multi-step user workflows where data is passed between dependent requests. In this lesson, you will learn how to build stateful integration scenarios.

---

## 🎯 Learning Objectives
1. Understand the difference between isolated unit tests and end-to-end integration flows.
2. Model the standard **CRUD Lifecycle** (Create $\to$ Read $\to$ Update $\to$ Delete $\to$ Verify).
3. Chain data between requests using dynamic captures and flow variables.
4. Use **Step Overrides** (`replace_tests`, `headers`, `vars`) to reuse existing request files in new test contexts.

---

## 🧠 Core Concept: Chaining Scenario Steps

A Scenario Flow links multiple requests together using shared state:

```
[ Step 1: Login ] 
       |
       v (captures: token)
[ Step 2: Create Pet "Tom" ]
       |
       v (captures: new_pet_id)
[ Step 3: Fetch Pet {{new_pet_id}} ]
       |
       v (assert name == "Tom")
[ Step 4: Delete Pet {{new_pet_id}} ]
       |
       v
[ Step 5: Fetch Pet {{new_pet_id}} ]
       |
       v (replace_tests: status == 404)
```

By verifying that deleting a pet causes subsequent fetch attempts to return `404 Not Found`, you prove that the entire data lifecycle functions correctly.

---

## 💻 Hands-On Sandbox Exercises

Ensure `./hit mock 8765 &` is running.

### Exercise 5.1: Inspecting a Scenario Flow

Let's look at `examples/petstore-zone/flows/login-and-crud.yaml`:

```yaml
name: Login, list, create, fetch, delete
vars: {kind: cat}
steps:
  - request: petstore/auth/login
  - request: petstore/pets/list
    tests: [{json: {"items[0].name": Tom}}]
  - request: petstore/pets/create
  - set: {first_pet_id: "{{new_pet_id}}"}
  - request: petstore/pets/get
  - request: petstore/pets/delete
  - request: petstore/pets/get
    name: Confirm it is gone
    replace_tests: [{status: 404}]
```

Notice several powerful patterns:
1. `vars: {kind: cat}`: Flow-level variables that override request defaults.
2. `set: {first_pet_id: "{{new_pet_id}}"}`: Capturing state from the create step and mapping it to the get/delete steps.
3. `replace_tests: [{status: 404}]`: Reusing the standard `pets/get` request, but replacing its default `200 OK` test with a `404 Not Found` test to confirm deletion!

---

### Exercise 5.2: Running the Flow

Execute the flow in the Petstore zone:

```bash
./hit -z examples/petstore-zone run login-and-crud
```

**Output**:
```
✓ petstore/auth/01-login (200 OK, 1ms) [5/5 passed]
✓ petstore/pets/01-list (200 OK, 1ms) [8/8 passed]
✓ petstore/pets/02-create (201 Created, 1ms) [2/2 passed]
✓ petstore/pets/03-get (200 OK, 1ms) [4/4 passed]
✓ petstore/pets/05-delete (204 No Content, 1ms) [1/1 passed]
✓ petstore/pets/03-get (Confirm it is gone) (404 Not Found, 1ms) [1/1 passed]
Summary: 6 passed, 0 failed, 6 total in 6ms
```

Every step ran sequentially, sharing variables and tokens seamlessly!

---

### Exercise 5.3: Fail-Fast vs Keep-Going

By default, scenario flows stop immediately on the first failed step (fail-fast) to prevent cascading errors or damaging downstream resources.

If you are running an audit and want to see all failures across the scenario, pass `--keep-going`:

```bash
./hit -z examples/petstore-zone run login-and-crud --keep-going
```

---

## 📝 Practice Quiz & Challenge

1. **Question**: Why is it best practice to verify a `404 Not Found` after a `DELETE` call instead of just checking that the delete request returned `204`?
   *(Answer: A server might return 204 without actually removing the entity from the database or cache. Verifying with GET confirms true persistence deletion.)*
2. **Challenge**: Run `hit -z examples/petstore-zone run lifecycle` to execute the complete pet lifecycle flow.

---

**Next Up**: In [**Lesson 6: Load Testing, Concurrency & Performance SLAs**](06-performance-and-slas.md), we will learn how to stress test APIs and establish automated performance gates!
