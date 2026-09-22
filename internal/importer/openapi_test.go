package importer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hit-endpoint/hit-endpoint/internal/zone"
)

var sampleOpenAPI3 = `
openapi: 3.0.0
info:
  title: Petstore OpenAPI
  version: 1.0.0
servers:
  - url: http://127.0.0.1:8765
paths:
  /pets:
    get:
      tags:
        - pets
      summary: List all pets
      operationId: listPets
      parameters:
        - name: kind
          in: query
          required: false
          schema:
            type: string
            default: dog
      responses:
        '200':
          description: A paged array of pets
          content:
            application/json:
              schema:
                type: object
                required:
                  - items
                  - total
                properties:
                  total:
                    type: integer
                  items:
                    type: array
                    items:
                      $ref: '#/components/schemas/Pet'
    post:
      tags:
        - pets
      summary: Create a pet
      operationId: createPet
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/NewPet'
      responses:
        '201':
          description: Null response
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Pet'
  /pets/{petId}:
    get:
      tags:
        - pets
      summary: Info for a specific pet
      operationId: showPetById
      parameters:
        - name: petId
          in: path
          required: true
          schema:
            type: integer
      responses:
        '200':
          description: Expected response to a valid request
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Pet'
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
    NewPet:
      type: object
      required:
        - name
      properties:
        name:
          type: string
          example: Fluffy
        kind:
          type: string
          default: cat
`

var sampleSwagger2 = `
swagger: "2.0"
info:
  title: Swagger Petstore
  version: 1.0.0
host: petstore.swagger.io
basePath: /v2
schemes:
  - https
paths:
  /user/login:
    get:
      tags:
        - user
      summary: Logs user into the system
      operationId: loginUser
      parameters:
        - name: username
          in: query
          required: true
          type: string
        - name: password
          in: query
          required: true
          type: string
      responses:
        200:
          description: successful operation
          schema:
            type: string
`

func TestImportOpenAPI3(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "openapi.yaml")
	if err := os.WriteFile(specPath, []byte(sampleOpenAPI3), 0644); err != nil {
		t.Fatalf("failed to write sample spec: %v", err)
	}

	collDir := filepath.Join(tmpDir, "collections")
	report, err := ImportOpenAPI(specPath, collDir, "")
	if err != nil {
		t.Fatalf("ImportOpenAPI failed: %v", err)
	}

	if report.Collection != "Petstore OpenAPI" {
		t.Errorf("expected collection name 'Petstore OpenAPI', got '%s'", report.Collection)
	}
	if report.Requests != 3 {
		t.Errorf("expected 3 requests, got %d", report.Requests)
	}
	if report.Folders != 1 {
		t.Errorf("expected 1 folder, got %d", report.Folders)
	}

	// Verify _defaults.yaml
	defaultsPath := filepath.Join(report.Destination, zone.DefaultsFile)
	defaults, err := zone.LoadYAML(defaultsPath)
	if err != nil {
		t.Fatalf("failed to load defaults.yaml: %v", err)
	}
	vars, ok := defaults["vars"].(map[string]any)
	if !ok || vars["base_url"] != "http://127.0.0.1:8765" {
		t.Errorf("expected base_url http://127.0.0.1:8765, got %+v", vars)
	}

	// Check create pet request
	petDir := filepath.Join(report.Destination, "pets")
	files, err := os.ReadDir(petDir)
	if err != nil {
		t.Fatalf("failed to read pets dir: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 files in pets folder, found %d", len(files))
	}

	// Check request file contents
	for _, f := range files {
		reqData, err := zone.LoadYAML(filepath.Join(petDir, f.Name()))
		if err != nil {
			t.Fatalf("failed to parse %s: %v", f.Name(), err)
		}
		if reqData["name"] == "Create a pet" {
			if reqData["method"] != "POST" {
				t.Errorf("expected POST, got %v", reqData["method"])
			}
			body, ok := reqData["body"].(map[string]any)
			if !ok {
				t.Fatalf("expected body in create pet: %+v", reqData)
			}
			jsonBody, ok := body["json"].(map[string]any)
			if !ok || jsonBody["name"] != "Fluffy" {
				t.Errorf("expected body.json.name == Fluffy, got %+v", jsonBody)
			}
		} else if reqData["name"] == "Info for a specific pet" {
			vars, ok := reqData["vars"].(map[string]any)
			if !ok || vars["petId"] == nil {
				t.Errorf("expected vars.petId, got %+v", reqData)
			}
		}
	}
}

func TestImportSwagger2(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "swagger.yaml")
	if err := os.WriteFile(specPath, []byte(sampleSwagger2), 0644); err != nil {
		t.Fatalf("failed to write swagger spec: %v", err)
	}

	collDir := filepath.Join(tmpDir, "collections")
	report, err := ImportOpenAPI(specPath, collDir, "MySwagger")
	if err != nil {
		t.Fatalf("ImportSwagger failed: %v", err)
	}

	if report.Collection != "MySwagger" {
		t.Errorf("expected collection name 'MySwagger', got '%s'", report.Collection)
	}
	if report.Requests != 1 {
		t.Errorf("expected 1 request, got %d", report.Requests)
	}

	defaultsPath := filepath.Join(report.Destination, zone.DefaultsFile)
	defaults, err := zone.LoadYAML(defaultsPath)
	if err != nil {
		t.Fatalf("failed to load defaults.yaml: %v", err)
	}
	vars, ok := defaults["vars"].(map[string]any)
	if !ok || vars["base_url"] != "https://petstore.swagger.io/v2" {
		t.Errorf("expected base_url https://petstore.swagger.io/v2, got %+v", vars)
	}
}
