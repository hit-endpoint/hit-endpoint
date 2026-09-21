package shorthand

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShorthandLoadAndSave(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hit_shorthand_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// 1. Initial load should be empty
	all, err := LoadAll(tmpDir)
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("expected 0 shorthands, got %d", len(all))
	}

	// 2. Save a shorthand
	sh1 := &Shorthand{
		Name:   "petstore-prod",
		URL:    "https://api.petstore.com/v1/pets",
		Method: "GET",
		Server: "prod",
		Headers: map[string]string{
			"Authorization": "Bearer token123",
		},
	}
	if err := Save(tmpDir, sh1); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// 3. Retrieve by name
	loaded, ok := Get(tmpDir, "petstore-prod")
	if !ok {
		t.Fatalf("expected petstore-prod to exist")
	}
	if loaded.URL != "https://api.petstore.com/v1/pets" {
		t.Errorf("expected url https://api.petstore.com/v1/pets, got %s", loaded.URL)
	}
	if loaded.Server != "prod" {
		t.Errorf("expected server prod, got %s", loaded.Server)
	}
	if loaded.Headers["Authorization"] != "Bearer token123" {
		t.Errorf("expected auth header, got %v", loaded.Headers)
	}

	// 4. Save a second shorthand
	sh2 := &Shorthand{
		Name:   "local-health",
		URL:    "http://localhost:8080/health",
		Method: "GET",
	}
	if err := Save(tmpDir, sh2); err != nil {
		t.Fatalf("Save sh2 failed: %v", err)
	}

	list, err := List(tmpDir)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 shorthands, got %d", len(list))
	}
	if list[0].Name != "local-health" || list[1].Name != "petstore-prod" {
		t.Errorf("unexpected list ordering: %s, %s", list[0].Name, list[1].Name)
	}

	// 5. Delete a shorthand
	if err := Delete(tmpDir, "local-health"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	listAfter, _ := List(tmpDir)
	if len(listAfter) != 1 {
		t.Errorf("expected 1 shorthand after delete, got %d", len(listAfter))
	}
}

func TestShorthandFromZoneConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hit_shorthand_zone_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	zoneYAML := `name: my-zone
default_server: local
shorthands:
  quick-get: https://httpbin.org/get
  api-login:
    ref: auth/login
    server: staging
`
	if err := os.WriteFile(filepath.Join(tmpDir, "zone.yaml"), []byte(zoneYAML), 0644); err != nil {
		t.Fatal(err)
	}

	all, err := LoadAll(tmpDir)
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	if len(all) != 2 {
		t.Fatalf("expected 2 shorthands from zone.yaml, got %d", len(all))
	}

	qg, ok := all["quick-get"]
	if !ok || qg.URL != "https://httpbin.org/get" {
		t.Errorf("unexpected quick-get: %+v", qg)
	}

	al, ok := all["api-login"]
	if !ok || al.Ref != "auth/login" || al.Server != "staging" {
		t.Errorf("unexpected api-login: %+v", al)
	}
}
