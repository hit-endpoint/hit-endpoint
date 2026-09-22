package matrix

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hit-endpoint/hit-endpoint/internal/spec"
)

func TestLoadMatrixInline(t *testing.T) {
	cfg := []any{
		map[string]any{"user": "alice", "role": "admin"},
		map[string]any{"user": "bob", "role": "editor"},
	}

	rows, err := LoadMatrix(cfg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0]["user"] != "alice" || rows[1]["role"] != "editor" {
		t.Errorf("unexpected rows content: %v", rows)
	}
}

func TestLoadMatrixCSV(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "cases.csv")
	content := `user,pass,expected,active
admin,secret,200,true
guest,wrong,401,false
`
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp CSV: %v", err)
	}

	cfg := map[string]any{"file": "cases.csv"}
	rows, err := LoadMatrix(cfg, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error loading CSV: %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	row1 := rows[0]
	if row1["user"] != "admin" {
		t.Errorf("expected user=admin, got %v", row1["user"])
	}
	if row1["expected"] != 200 {
		t.Errorf("expected expected=200 (coerced int), got %v (%T)", row1["expected"], row1["expected"])
	}
	if row1["active"] != true {
		t.Errorf("expected active=true (coerced bool), got %v", row1["active"])
	}

	row2 := rows[1]
	if row2["user"] != "guest" || row2["expected"] != 401 || row2["active"] != false {
		t.Errorf("unexpected row2: %v", row2)
	}
}

func TestExpandSpec(t *testing.T) {
	base := spec.NewRequestSpec()
	base.Name = "Login Test"
	base.Method = "POST"
	base.Url = "https://api.example.com/login"
	base.Vars["default_var"] = "fixed"

	row := Row{
		"username": "charlie",
		"tier":     "pro",
	}

	expanded := ExpandSpec(base, row, 0)
	if expanded.Vars["default_var"] != "fixed" {
		t.Errorf("expected default_var preserved")
	}
	if expanded.Vars["username"] != "charlie" {
		t.Errorf("expected promoted username variable")
	}
	rowCtx, ok := expanded.Vars["row"].(Row)
	if !ok || rowCtx["tier"] != "pro" {
		t.Errorf("expected row context in Vars['row'], got %v", expanded.Vars["row"])
	}
	if expanded.Name != "Login Test [Row 1: tier=pro, username=charlie]" && expanded.Name != "Login Test [Row 1: username=charlie, tier=pro]" {
		t.Errorf("unexpected expanded name: %s", expanded.Name)
	}
}
