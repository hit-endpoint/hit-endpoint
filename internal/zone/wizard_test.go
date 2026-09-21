package zone

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitWizard(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hit-wizard-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	zoneDir := filepath.Join(tmpDir, "my-service")
	opts := WizardOptions{
		Dir:           zoneDir,
		Name:          "my-service",
		BaseURL:       "http://127.0.0.1:3000",
		PrimaryServer: "local",
	}

	z, err := InitWizard(opts)
	if err != nil {
		t.Fatalf("InitWizard failed: %v", err)
	}

	if z.Name() != "my-service" {
		t.Errorf("expected zone name 'my-service', got '%s'", z.Name())
	}

	// Verify required files exist
	expectedFiles := []string{
		"zone.yaml",
		".gitignore",
		"shorthands.yaml",
		"servers/local.yaml",
		"servers/local.secrets.example.yaml",
		"servers/production.yaml",
		"servers/production.secrets.example.yaml",
		"collections/default/_defaults.yaml",
		"collections/default/00-health.yaml",
		"collections/default/01-get-sample.yaml",
		"chains/smoke.yaml",
	}

	for _, rel := range expectedFiles {
		p := filepath.Join(zoneDir, rel)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected file %s to exist, err: %v", rel, err)
		}
	}

	// Verify Find discovers it
	foundZone, err := Find(zoneDir)
	if err != nil {
		t.Fatalf("Find failed on wizard zone: %v", err)
	}
	if foundZone.Name() != "my-service" {
		t.Errorf("expected found zone name 'my-service', got '%s'", foundZone.Name())
	}

	// Verify server names
	servers := foundZone.ServerNames()
	if len(servers) != 2 {
		t.Errorf("expected 2 servers (local, production), got %v", servers)
	}

	// Verify already exists error
	_, err = InitWizard(opts)
	if err == nil {
		t.Errorf("expected error when initializing over existing zone")
	}
}
