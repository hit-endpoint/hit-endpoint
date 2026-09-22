package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hit-endpoint/hit-endpoint/internal/zone"
)

func TestAuditCoverage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hit-coverage-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	z, err := zone.Init(tmpDir, "coverage-test")
	if err != nil {
		t.Fatal(err)
	}

	// Create two request files in collections:
	// 1. GET /pets
	petsListYAML := `name: List Pets
method: GET
url: "{{base_url}}/pets"
tests:
  - status: 200
`
	req1Dir := filepath.Join(z.CollectionsDir(), "pets")
	_ = os.MkdirAll(req1Dir, 0755)
	_ = os.WriteFile(filepath.Join(req1Dir, "01-list.yaml"), []byte(petsListYAML), 0644)

	// 2. GET /pets/{id}
	petGetYAML := `name: Get Pet
method: GET
url: "{{base_url}}/pets/{{id}}"
tests:
  - status: 200
`
	_ = os.WriteFile(filepath.Join(req1Dir, "02-get.yaml"), []byte(petGetYAML), 0644)

	// Sample OpenAPI spec with 3 operations:
	// GET /pets (covered)
	// GET /pets/{id} (covered)
	// POST /pets (untested!)
	sampleSpec := `
openapi: "3.0.0"
info:
  title: "Petstore API"
  version: "1.0.0"
paths:
  /pets:
    get:
      summary: "List all pets"
      responses:
        "200":
          description: "A paged array of pets"
    post:
      summary: "Create a pet"
      responses:
        "201":
          description: "Created"
  /pets/{id}:
    get:
      summary: "Info for a specific pet"
      responses:
        "200":
          description: "Expected response"
`

	rep, err := AuditCoverage([]byte(sampleSpec), z)
	if err != nil {
		t.Fatal(err)
	}

	if rep.TotalOperations != 3 {
		t.Fatalf("expected 3 operations, got %d", rep.TotalOperations)
	}
	if rep.CoveredOperations != 2 {
		t.Fatalf("expected 2 covered operations, got %d", rep.CoveredOperations)
	}
	expectedPct := 2.0 / 3.0 * 100.0
	if rep.CoveragePercent < expectedPct-0.1 || rep.CoveragePercent > expectedPct+0.1 {
		t.Fatalf("expected %.2f%% coverage, got %.2f%%", expectedPct, rep.CoveragePercent)
	}

	if len(rep.Untested) != 1 || rep.Untested[0].Method != "POST" {
		t.Fatalf("expected 1 untested POST operation, got %+v", rep.Untested)
	}

	tableStr := FormatCoverageTable(rep, false)
	if !strings.Contains(tableStr, "66.7%") {
		t.Errorf("missing coverage percent in table: %s", tableStr)
	}
	if !strings.Contains(tableStr, "Untested Operations (1 missing)") {
		t.Errorf("missing untested section in table: %s", tableStr)
	}
}
