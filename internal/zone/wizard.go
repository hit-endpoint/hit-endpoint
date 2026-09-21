package zone

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WizardOptions configures the zone creation wizard.
type WizardOptions struct {
	Dir           string
	Name          string
	BaseURL       string
	PrimaryServer string
}

// InitWizard creates a complete boilerplate zone with servers, collections, chains,
// shorthands, and secrets templates, designed so that 'hit sanity' will guide the user on next steps.
func InitWizard(opts WizardOptions) (*Zone, error) {
	dir := opts.Dir
	if dir == "" {
		dir = "."
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path %s: %w", dir, err)
	}

	if err := os.MkdirAll(absDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", absDir, err)
	}

	// Check if already a zone
	zoneFile := filepath.Join(absDir, ZoneFile)
	if _, err := os.Stat(zoneFile); err == nil {
		return nil, NewZoneError("zone already exists in %s (%s found)", absDir, ZoneFile)
	}

	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = filepath.Base(absDir)
		if name == "." || name == "/" || name == "" {
			name = "my-api"
		}
	}

	baseURL := strings.TrimSpace(opts.BaseURL)
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8000"
	}

	primaryServer := strings.TrimSpace(opts.PrimaryServer)
	if primaryServer == "" {
		primaryServer = "local"
	}

	// 1. zone.yaml
	zoneCfg := map[string]any{
		"name":           name,
		"default_server": primaryServer,
		"vars": map[string]any{
			"api_version": "v1",
		},
		"shorthands": map[string]any{
			"health": "default/00-health",
			"sample": "default/01-get-sample",
		},
	}
	zoneYAML, err := DumpYAML(zoneCfg)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(zoneFile, []byte(zoneYAML), 0644); err != nil {
		return nil, err
	}

	// 2. .gitignore
	giPath := filepath.Join(absDir, ".gitignore")
	if _, err := os.Stat(giPath); os.IsNotExist(err) {
		giContent := ".hit/\n*.secrets.yaml\n*.secrets.yml\n"
		_ = os.WriteFile(giPath, []byte(giContent), 0644)
	}

	// 3. shorthands.yaml
	shorthandsPath := filepath.Join(absDir, "shorthands.yaml")
	shContent := "health: default/00-health\nsample: default/01-get-sample\n"
	_ = os.WriteFile(shorthandsPath, []byte(shContent), 0644)

	// 4. servers/
	serversDir := filepath.Join(absDir, "servers")
	if err := os.MkdirAll(serversDir, 0755); err != nil {
		return nil, err
	}

	primaryServerPath := filepath.Join(serversDir, primaryServer+".yaml")
	primaryYAML, _ := DumpYAML(map[string]any{
		"base_url": baseURL,
		"vars": map[string]any{
			"api_version": "v1",
		},
	})
	_ = os.WriteFile(primaryServerPath, []byte(primaryYAML), 0644)

	primarySecExPath := filepath.Join(serversDir, primaryServer+".secrets.example.yaml")
	primarySecContent := fmt.Sprintf("# Secrets template for %s.\n# Copy to %s.secrets.yaml (gitignored) and fill in real credentials:\nvars:\n  auth_token: your-secret-token-here\n", primaryServer, primaryServer)
	_ = os.WriteFile(primarySecExPath, []byte(primarySecContent), 0644)

	prodServerPath := filepath.Join(serversDir, "production.yaml")
	prodYAML, _ := DumpYAML(map[string]any{
		"base_url": "https://api.example.com",
		"vars": map[string]any{
			"api_version": "v1",
		},
	})
	_ = os.WriteFile(prodServerPath, []byte(prodYAML), 0644)

	prodSecExPath := filepath.Join(serversDir, "production.secrets.example.yaml")
	prodSecContent := "# Secrets template for production.\n# Copy to production.secrets.yaml (gitignored) and fill in real credentials:\nvars:\n  auth_token: your-production-token-here\n"
	_ = os.WriteFile(prodSecExPath, []byte(prodSecContent), 0644)

	// 5. collections/default/
	collDir := filepath.Join(absDir, "collections", "default")
	if err := os.MkdirAll(collDir, 0755); err != nil {
		return nil, err
	}

	defYAML, _ := DumpYAML(map[string]any{
		"description": "Default API collection",
		"headers": map[string]any{
			"Accept":     "application/json",
			"User-Agent": "hit/1.0",
		},
	})
	_ = os.WriteFile(filepath.Join(collDir, "_defaults.yaml"), []byte(defYAML), 0644)

	healthReq, _ := DumpYAML(map[string]any{
		"name":   "Health check",
		"method": "GET",
		"url":    "{{base_url}}/health",
		"tests": []any{
			map[string]any{"status": 200},
		},
	})
	_ = os.WriteFile(filepath.Join(collDir, "00-health.yaml"), []byte(healthReq), 0644)

	sampleReq, _ := DumpYAML(map[string]any{
		"name":   "Get sample item",
		"method": "GET",
		"url":    "{{base_url}}/{{api_version}}/items/{{item_id}}",
		"vars": map[string]any{
			"item_id": "1",
		},
		"tests": []any{
			map[string]any{"status": 200},
		},
		"captures": map[string]any{
			"sample_id": "json.id",
		},
	})
	_ = os.WriteFile(filepath.Join(collDir, "01-get-sample.yaml"), []byte(sampleReq), 0644)

	// 6. chains/
	chainsDir := filepath.Join(absDir, "chains")
	if err := os.MkdirAll(chainsDir, 0755); err != nil {
		return nil, err
	}

	smokeChain, _ := DumpYAML(map[string]any{
		"name":        "Smoke test chain",
		"description": "Initial smoke test verifying health and sample request",
		"steps": []any{
			map[string]any{"request": "default/00-health"},
			map[string]any{"request": "default/01-get-sample"},
		},
	})
	_ = os.WriteFile(filepath.Join(chainsDir, "smoke.yaml"), []byte(smokeChain), 0644)

	return &Zone{
		Root:   absDir,
		Config: zoneCfg,
	}, nil
}
