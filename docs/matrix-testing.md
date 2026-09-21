# Data-Driven Matrix Testing (`matrix:`)

Data-Driven Matrix Testing allows you to parameterize any request or scenario step with multiple rows of test data. `hit` executes each row variant, substitutes templated variables, and outputs a consolidated matrix scorecard.

---

## 🎯 Use Cases

- **User & Role Permissions**: Verify that admin, manager, member, and guest accounts experience the expected HTTP statuses (`200`, `403`, etc.).
- **Boundary & Validation Testing**: Run negative and positive validation tests with boundary values (empty strings, special characters, max lengths, negative numbers).
- **CRUD Workflows**: Provision multiple entities across servers using a single declarative request file.
- **Data-Driven Regression**: Replay historic customer scenarios from external CSV or JSON exports.

---

## 📋 Inline Data Rows (`matrix.rows`)

Define matrix rows directly in your YAML request file:

```yaml
# collections/users/create-matrix.yaml
name: Create Users Matrix
method: POST
url: "{{base_url}}/users"
headers:
  Content-Type: application/json
matrix:
  rows:
    - { name: "Alice", role: "admin", expected_status: 201 }
    - { name: "Bob", role: "editor", expected_status: 201 }
    - { name: "Charlie", role: "viewer", expected_status: 201 }
    - { name: "", role: "invalid", expected_status: 400 }

body:
  json:
    name: "{{row.name}}"
    role: "{{row.role}}"

tests:
  - status: "{{row.expected_status}}"
  - json:
      name: "{{row.name}}"
```

> [!TIP]
> Both `{{row.field}}` and direct `{{field}}` syntax are supported when accessing matrix column values!

---

## 📁 External CSV / JSON Data Files

For large datasets, reference external files located relative to the request file or zone root:

```yaml
# collections/orders/discount-test.yaml
name: Discount Calculation Tests
method: POST
url: "{{base_url}}/orders/calculate-discount"
matrix: data/discounts.csv
# or: matrix: "data/discounts.json"
# or:
# matrix:
#   file: data/discounts.csv

body:
  json:
    coupon_code: "{{coupon}}"
    cart_total: "{{cart_value}}"

tests:
  - status: 200
  - json:
      discount_amount: "{{expected_discount}}"
```

### Supported CSV Format
```csv
coupon,cart_value,expected_discount
SUMMER10,100,10
FALL20,200,40
VIP50,500,250
```

---

## 📊 Matrix Scorecard Output

When running a matrix-enabled request (`hit run collections/users/create-matrix`), `hit` prints a consolidated scorecard showing each variation's name, status, duration, and test assertion outcomes:

```text
collections/users/create-matrix  POST http://localhost:8080/users  →  201 OK  12 ms  142 B
  Matrix Scorecard (4 variations):
    [1] Create Users Matrix [row 1]    ✓ PASS (HTTP 201, 4ms)
    [2] Create Users Matrix [row 2]    ✓ PASS (HTTP 201, 3ms)
    [3] Create Users Matrix [row 3]    ✓ PASS (HTTP 201, 3ms)
    [4] Create Users Matrix [row 4]    ✓ PASS (HTTP 400, 2ms)
────────────────────────────────────────
1 request(s), 8 test(s), all passed
```

If any variation fails an assertion:
```text
    [4] Create Users Matrix [row 4]    ✗ FAIL (HTTP 500, 2ms)
        ✗ status == 400: expected 400, got 500
```
`hit` marks the test suite as failed and exits with code `1`, preventing flawed builds from reaching production.

---

## 🔄 Scenario Flows Integration (`flows/`)

Matrix configurations are fully supported inside multi-step scenario flows:

```yaml
# flows/onboarding-matrix.yaml
name: Multi-User Onboarding Matrix Flow
steps:
  - request: auth/login
  
  - request: users/create
    matrix:
      rows:
        - { username: "dev_user", role: "developer" }
        - { username: "qa_user", role: "tester" }
        - { username: "prod_user", role: "operator" }
    set:
      last_created: "{{body.id}}"
```
