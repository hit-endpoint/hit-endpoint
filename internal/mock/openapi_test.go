package mock

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var testPetstoreOpenAPI3 = `
openapi: "3.0.3"
info:
  title: Test Petstore
  version: "1.2.0"
  description: Sample API for testing dynamic OpenAPI mocker
paths:
  /health:
    get:
      summary: Health check
      operationId: getHealth
      responses:
        "200":
          description: Server status
          content:
            application/json:
              schema:
                type: object
                properties:
                  status:
                    type: string
                    example: "healthy"
  /pets:
    get:
      summary: List pets
      operationId: listPets
      responses:
        "200":
          description: List of pets
          content:
            application/json:
              schema:
                type: array
                items:
                  $ref: '#/components/schemas/Pet'
    post:
      summary: Create pet
      operationId: createPet
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/NewPet'
      responses:
        "201":
          description: Pet created
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Pet'
  /pets/{id}:
    get:
      summary: Get pet by ID
      operationId: getPetById
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: integer
      responses:
        "200":
          description: Pet found
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Pet'
              examples:
                dog:
                  value:
                    id: 99
                    name: "Sparky"
                    kind: "dog"
                cat:
                  value:
                    id: 88
                    name: "Whiskers"
                    kind: "cat"
        "404":
          description: Pet not found
          content:
            application/json:
              schema:
                type: object
                properties:
                  error:
                    type: string
                    example: "not_found"
    put:
      summary: Update pet
      operationId: updatePet
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: integer
      responses:
        "200":
          description: Updated pet
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Pet'
    delete:
      summary: Delete pet
      operationId: deletePet
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: integer
      responses:
        "204":
          description: Pet deleted
components:
  schemas:
    Pet:
      type: object
      required:
        - id
        - name
      properties:
        id:
          type: integer
        name:
          type: string
        kind:
          type: string
          enum: [dog, cat, bird]
        created_at:
          type: string
          format: date-time
        uuid:
          type: string
          format: uuid
        email:
          type: string
          format: email
    NewPet:
      type: object
      required:
        - name
      properties:
        name:
          type: string
        kind:
          type: string
`

var testSwagger2Spec = `
swagger: "2.0"
info:
  title: Swagger 2 Petstore
  version: "2.0.0"
paths:
  /items:
    get:
      summary: List items
      responses:
        "200":
          description: Successful response
          schema:
            type: array
            items:
              type: object
              properties:
                id:
                  type: integer
                title:
                  type: string
`

func writeTempSpec(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "openapi.yaml")
	if err := os.WriteFile(f, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp spec: %v", err)
	}
	return f
}

func TestLoadOpenAPISpec(t *testing.T) {
	specFile := writeTempSpec(t, testPetstoreOpenAPI3)
	spec, raw, err := LoadOpenAPISpec(specFile)
	if err != nil {
		t.Fatalf("unexpected error loading spec: %v", err)
	}
	if !spec.IsOpenAPI3 {
		t.Fatalf("expected IsOpenAPI3 to be true")
	}
	if spec.Title != "Test Petstore" {
		t.Fatalf("expected title 'Test Petstore', got %q", spec.Title)
	}
	if spec.Version != "1.2.0" {
		t.Fatalf("expected version '1.2.0', got %q", spec.Version)
	}
	if len(raw) == 0 {
		t.Fatalf("expected non-empty raw bytes")
	}

	// Test Swagger 2.0 loading
	swagFile := writeTempSpec(t, testSwagger2Spec)
	swagSpec, _, err := LoadOpenAPISpec(swagFile)
	if err != nil {
		t.Fatalf("unexpected error loading swagger 2 spec: %v", err)
	}
	if !swagSpec.IsSwagger2 {
		t.Fatalf("expected IsSwagger2 to be true")
	}
	if swagSpec.Title != "Swagger 2 Petstore" {
		t.Fatalf("expected title 'Swagger 2 Petstore', got %q", swagSpec.Title)
	}

	// Test invalid spec
	invalidFile := writeTempSpec(t, "foo: bar\nbaz: qux\n")
	_, _, err = LoadOpenAPISpec(invalidFile)
	if err == nil {
		t.Fatalf("expected error loading invalid spec, got nil")
	}
}

func TestOpenAPIRouteMatching(t *testing.T) {
	specFile := writeTempSpec(t, testPetstoreOpenAPI3)
	server, err := NewOpenAPIServer(specFile, ChaosOptions{}, true)
	if err != nil {
		t.Fatalf("failed to create openapi server: %v", err)
	}

	engine := server.OpenAPIEngine()
	if engine == nil {
		t.Fatalf("expected OpenAPIEngine to be non-nil")
	}
	if engine.RouteCount() < 4 {
		t.Fatalf("expected at least 4 routes, got %d", engine.RouteCount())
	}

	// Match GET /pets
	route, params, _ := engine.matchRoute("GET", "/pets")
	if route == nil || route.OperationID != "listPets" {
		t.Fatalf("expected to match listPets, got %v", route)
	}
	if len(params) != 0 {
		t.Fatalf("expected 0 params, got %v", params)
	}

	// Match GET /pets/42
	route, params, _ = engine.matchRoute("GET", "/pets/42")
	if route == nil || route.OperationID != "getPetById" {
		t.Fatalf("expected to match getPetById, got %v", route)
	}
	if params["id"] != "42" {
		t.Fatalf("expected param id=42, got %q", params["id"])
	}

	// Match DELETE /pets/42
	route, params, _ = engine.matchRoute("DELETE", "/pets/42")
	if route == nil || route.OperationID != "deletePet" {
		t.Fatalf("expected to match deletePet, got %v", route)
	}

	// Match wrong method on known path -> 405 Method Not Allowed
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PATCH", "/health", nil)
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed, got %d", rec.Code)
	}
	if rec.Header().Get("Allow") != "GET" {
		t.Fatalf("expected Allow: GET, got %q", rec.Header().Get("Allow"))
	}

	// Match unknown route -> 404 Route Not Found
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/unknown/route", nil)
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found, got %d", rec.Code)
	}
	var errResp map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&errResp)
	if errResp["error"] != "route_not_found" {
		t.Fatalf("expected error route_not_found, got %v", errResp)
	}
}

func TestOpenAPIStatefulCRUD(t *testing.T) {
	specFile := writeTempSpec(t, testPetstoreOpenAPI3)
	server, err := NewOpenAPIServer(specFile, ChaosOptions{}, true)
	if err != nil {
		t.Fatalf("failed to create openapi server: %v", err)
	}

	// 1. GET /pets returns seeded items
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/pets", nil)
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	var pets []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&pets); err != nil {
		t.Fatalf("failed to decode pets: %v", err)
	}
	if len(pets) != 2 {
		t.Fatalf("expected 2 seeded pets, got %d", len(pets))
	}
	if pets[0]["id"] != float64(1) || pets[1]["id"] != float64(2) {
		t.Fatalf("expected pet IDs 1 and 2, got %v, %v", pets[0]["id"], pets[1]["id"])
	}

	// 2. POST /pets creates a new pet
	newPet := map[string]any{
		"name": "Barnaby",
		"kind": "dog",
	}
	bodyBytes, _ := json.Marshal(newPet)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/pets", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", rec.Code)
	}
	var createdPet map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&createdPet); err != nil {
		t.Fatalf("failed to decode created pet: %v", err)
	}
	if createdPet["id"] != float64(3) {
		t.Fatalf("expected assigned id=3, got %v", createdPet["id"])
	}
	if createdPet["name"] != "Barnaby" {
		t.Fatalf("expected name Barnaby, got %v", createdPet["name"])
	}

	// 3. GET /pets/3 retrieves the created pet
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/pets/3", nil)
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	var fetchedPet map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&fetchedPet)
	if fetchedPet["name"] != "Barnaby" {
		t.Fatalf("expected name Barnaby, got %v", fetchedPet["name"])
	}

	// 4. PUT /pets/3 updates the pet
	updateData := map[string]any{"name": "Barnaby The Great"}
	updateBytes, _ := json.Marshal(updateData)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/pets/3", bytes.NewReader(updateBytes))
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	var updatedPet map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&updatedPet)
	if updatedPet["name"] != "Barnaby The Great" {
		t.Fatalf("expected name Barnaby The Great, got %v", updatedPet["name"])
	}

	// 5. DELETE /pets/3 deletes the pet
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/pets/3", nil)
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content, got %d", rec.Code)
	}

	// 6. Reset state via POST /__mock/reset
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/__mock/reset", nil)
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	// Verify count is back to 2
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/pets", nil)
	server.ServeHTTP(rec, req)
	pets = nil
	_ = json.NewDecoder(rec.Body).Decode(&pets)
	if len(pets) != 2 {
		t.Fatalf("expected 2 pets after reset, got %d", len(pets))
	}
}

func TestOpenAPIClientOverrides(t *testing.T) {
	specFile := writeTempSpec(t, testPetstoreOpenAPI3)
	server, err := NewOpenAPIServer(specFile, ChaosOptions{}, true)
	if err != nil {
		t.Fatalf("failed to create openapi server: %v", err)
	}

	// Override status via Prefer: status=404
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/pets/1", nil)
	req.Header.Set("Prefer", "status=404")
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}

	// Override status via X-Hit-Mock-Status: 404
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/pets/1", nil)
	req.Header.Set("X-Hit-Mock-Status", "404")
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}

	// Override example via Prefer: example=dog
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/pets/1", nil)
	req.Header.Set("Prefer", "example=dog")
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var pet map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&pet)
	if pet["name"] != "Sparky" {
		t.Fatalf("expected Sparky from dog example, got %v", pet["name"])
	}

	// Override example via X-Hit-Mock-Example: cat
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/pets/1", nil)
	req.Header.Set("X-Hit-Mock-Example", "cat")
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	pet = nil
	_ = json.NewDecoder(rec.Body).Decode(&pet)
	if pet["name"] != "Whiskers" {
		t.Fatalf("expected Whiskers from cat example, got %v", pet["name"])
	}
}

func TestOpenAPIInspectorRoutesAndCORS(t *testing.T) {
	specFile := writeTempSpec(t, testPetstoreOpenAPI3)
	server, err := NewOpenAPIServer(specFile, ChaosOptions{}, true)
	if err != nil {
		t.Fatalf("failed to create openapi server: %v", err)
	}

	// Test CORS preflight OPTIONS
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("OPTIONS", "/pets", nil)
	req.Header.Set("Access-Control-Request-Method", "POST")
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content for OPTIONS, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("expected Access-Control-Allow-Origin: *")
	}

	// Test GET /__mock/routes
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/__mock/routes", nil)
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	var routesInfo map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&routesInfo); err != nil {
		t.Fatalf("failed to decode routes: %v", err)
	}
	if routesInfo["title"] != "Test Petstore" {
		t.Fatalf("expected title Test Petstore, got %v", routesInfo["title"])
	}
	routesList, ok := routesInfo["routes"].([]any)
	if !ok || len(routesList) < 4 {
		t.Fatalf("expected at least 4 routes in inspector, got %d", len(routesList))
	}

	// Test GET /__mock/openapi
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/__mock/openapi", nil)
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Fatalf("expected non-empty OpenAPI spec body")
	}
}

func TestOpenAPIChaosIntegration(t *testing.T) {
	specFile := writeTempSpec(t, testPetstoreOpenAPI3)
	// Rate limit: 2 requests per second
	server, err := NewOpenAPIServer(specFile, ChaosOptions{
		RateLimit:  2,
		RateWindow: time.Second,
	}, true)
	if err != nil {
		t.Fatalf("failed to create openapi server: %v", err)
	}

	// Request 1: OK
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("request 1 expected 200, got %d", rec.Code)
	}

	// Request 2: OK
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("request 2 expected 200, got %d", rec.Code)
	}

	// Request 3: 429 Too Many Requests
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request 3 expected 429, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatalf("expected Retry-After header on 429")
	}

	// Bypass chaos via header
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set("X-Hit-Chaos-Bypass", "true")
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bypassed request expected 200, got %d", rec.Code)
	}
}
