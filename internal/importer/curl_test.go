package importer

import (
	"path/filepath"
	"testing"

	"github.com/hit-endpoint/hit-endpoint/internal/zone"
)

func TestImportCurl(t *testing.T) {
	cmd := `curl -X POST https://api.example.com/v1/users?verbose=1 -H "Authorization: Bearer mysecret" -H "Content-Type: application/json" -d '{"name": "Alice", "role": "admin"}'`

	tmpDir := t.TempDir()
	report, err := ImportCurl(cmd, tmpDir, "create-user")
	if err != nil {
		t.Fatalf("ImportCurl failed: %v", err)
	}

	if report.Requests != 1 {
		t.Errorf("expected 1 request, got %d", report.Requests)
	}

	reqFile := filepath.Join(tmpDir, "create-user.yaml")
	spec, err := zone.LoadYAML(reqFile)
	if err != nil {
		t.Fatalf("failed to load created request: %v", err)
	}

	if spec["name"] != "create-user" {
		t.Errorf("expected name create-user, got %v", spec["name"])
	}
	if spec["method"] != "POST" {
		t.Errorf("expected method POST, got %v", spec["method"])
	}
	if spec["url"] != "https://api.example.com/v1/users" {
		t.Errorf("expected url https://api.example.com/v1/users, got %v", spec["url"])
	}

	auth, ok := spec["auth"].(map[string]any)
	if !ok || auth["type"] != "bearer" || auth["token"] != "mysecret" {
		t.Errorf("expected bearer auth, got %+v", auth)
	}

	query, ok := spec["query"].(map[string]any)
	if !ok || query["verbose"] != "1" {
		t.Errorf("expected query verbose=1, got %+v", query)
	}

	body, ok := spec["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected body, got %+v", spec)
	}
	jsonBody, ok := body["json"].(map[string]any)
	if !ok || jsonBody["name"] != "Alice" || jsonBody["role"] != "admin" {
		t.Errorf("expected json body with Alice, got %+v", jsonBody)
	}
}
