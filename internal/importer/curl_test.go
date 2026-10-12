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
	if !ok || auth["type"] != "bearer" || auth["token"] != "{{token}}" {
		t.Errorf("expected parameterized bearer auth, got %+v", auth)
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

	// Test --data-urlencode and -u + Bearer override
	cmd2 := `curl https://api.example.com/oauth/token -u client:secret -H "Authorization: Bearer override-tok" --data-urlencode "grant_type=client_credentials" --data-urlencode "scope=read write"`
	_, err = ImportCurl(cmd2, tmpDir, "oauth-token")
	if err != nil {
		t.Fatalf("ImportCurl cmd2 failed: %v", err)
	}
	spec2, err := zone.LoadYAML(filepath.Join(tmpDir, "oauth-token.yaml"))
	if err != nil {
		t.Fatalf("failed to load oauth-token.yaml: %v", err)
	}
	if spec2["method"] != "POST" {
		t.Errorf("expected method POST for --data-urlencode, got %v", spec2["method"])
	}
	auth2, ok := spec2["auth"].(map[string]any)
	if !ok || auth2["type"] != "bearer" || auth2["username"] != nil {
		t.Errorf("expected clean bearer auth without mixed basic auth keys, got %+v", auth2)
	}
	body2, ok := spec2["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected body in spec2, got %+v", spec2)
	}
	form2, ok := body2["form"].(map[string]any)
	if !ok || form2["grant_type"] != "client_credentials" || form2["scope"] != "read write" {
		t.Errorf("expected form body from --data-urlencode, got %+v", form2)
	}
}
