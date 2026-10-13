package sanity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hit-endpoint/hit-endpoint/internal/zone"
)

func TestRunSanityDetectsUndefinedVarInNonDefaultServer(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hit_sanity_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	z, err := zone.InitWizard(zone.WizardOptions{
		Dir:           tmpDir,
		Name:          "sanity-test",
		BaseURL:       "http://127.0.0.1:8765",
		PrimaryServer: "local",
	})
	if err != nil {
		t.Fatalf("RunWizard failed: %v", err)
	}

	// Baseline sanity check should succeed
	rep := RunSanity(z, "", "", false, 1.0)
	if !rep.Ready || rep.Failures != 0 {
		t.Fatalf("expected baseline sanity to pass with 0 failures, got failures=%d checks=%+v", rep.Failures, rep.Checks)
	}

	// 1. Add an undefined variable reference directly inside non-default server (production.yaml)
	prodPath := filepath.Join(tmpDir, "servers", "production.yaml")
	prodContent := "base_url: https://api.example.com\nvars:\n  api_version: v1\n  broken_var: \"{{undefined_in_prod}}\"\n"
	if err := os.WriteFile(prodPath, []byte(prodContent), 0644); err != nil {
		t.Fatal(err)
	}

	rep2 := RunSanity(z, "", "", false, 1.0)
	if rep2.Ready || rep2.Failures == 0 {
		t.Fatalf("expected sanity to fail when non-default server references undefined variable, got checks=%+v", rep2.Checks)
	}
	foundProdErr := false
	for _, c := range rep2.Checks {
		if c.Status == "fail" && strings.Contains(c.Title, "production") && strings.Contains(c.Title, "undefined_in_prod") {
			foundProdErr = true
			break
		}
	}
	if !foundProdErr {
		t.Errorf("expected fail check mentioning production and undefined_in_prod, got: %+v", rep2.Checks)
	}

	// 2. Remove api_version from zone.yaml and production.yaml while keeping it in local.yaml
	_ = os.WriteFile(filepath.Join(tmpDir, "zone.yaml"), []byte("name: sanity-test\ndefault_server: local\n"), 0644)
	_ = os.WriteFile(prodPath, []byte("base_url: https://api.example.com\n"), 0644)
	zReloaded, err := zone.Find(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	rep3 := RunSanity(zReloaded, "", "", false, 1.0)
	if rep3.Ready || rep3.Failures == 0 {
		t.Fatalf("expected sanity to fail when request variable is defined in default server but missing in production, got checks=%+v", rep3.Checks)
	}
}
