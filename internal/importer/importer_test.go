package importer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/hit-endpoint/hit-endpoint/internal/zone"
)

var sampleCollection = map[string]any{
	"info": map[string]any{
		"name":   "Demo API",
		"schema": "https://schema.example.com/collection/v2.1.0/collection.json",
	},
	"auth": map[string]any{
		"type": "bearer",
		"bearer": []any{
			map[string]any{"key": "token", "value": "{{token}}", "type": "string"},
		},
	},
	"variable": []any{
		map[string]any{"key": "baseUrl", "value": "https://api.example.com"},
	},
	"item": []any{
		map[string]any{
			"name": "Auth",
			"item": []any{
				map[string]any{
					"name": "Login",
					"event": []any{
						map[string]any{
							"listen": "test",
							"script": map[string]any{
								"exec": []any{
									"var jsonData = pm.response.json();",
									"pm.test(\"Status code is 200\", function () {",
									"    pm.response.to.have.status(200);",
									"});",
									"pm.test(\"has token\", function () {",
									"    pm.expect(jsonData.access_token).to.be.a('string');",
									"    pm.expect(jsonData.expires_in).to.be.above(0);",
									"});",
									"pm.environment.set(\"token\", jsonData.access_token);",
									"pm.collectionVariables.set(\"user_id\", jsonData.user.id);",
									"someCustomHelper(jsonData);",
								},
							},
						},
					},
					"request": map[string]any{
						"method": "POST",
						"auth":   map[string]any{"type": "noauth"},
						"header": []any{
							map[string]any{"key": "Content-Type", "value": "application/json"},
							map[string]any{"key": "X-Off", "value": "no", "disabled": true},
						},
						"body": map[string]any{
							"mode": "raw",
							"raw":  `{"username": "{{user}}", "password": "{{pass}}"}`,
						},
						"url": map[string]any{
							"raw":  "{{baseUrl}}/auth/login",
							"host": []any{"{{baseUrl}}"},
							"path": []any{"auth", "login"},
						},
					},
				},
			},
		},
		map[string]any{
			"name": "Get Pet",
			"request": map[string]any{
				"method": "GET",
				"url": map[string]any{
					"raw":  "{{baseUrl}}/pets/:petId?verbose=true&x=1",
					"host": []any{"{{baseUrl}}"},
					"path": []any{"pets", ":petId"},
					"query": []any{
						map[string]any{"key": "verbose", "value": "true"},
						map[string]any{"key": "x", "value": "1", "disabled": true},
					},
					"variable": []any{
						map[string]any{"key": "petId", "value": "42"},
					},
				},
			},
			"event": []any{
				map[string]any{
					"listen": "test",
					"script": map[string]any{
						"exec": []any{
							"pm.test(\"fast\", () => pm.expect(pm.response.responseTime).to.be.below(300));",
							"pm.test(\"json\", function () { pm.response.to.be.json; });",
							"pm.expect(pm.response.json().name).to.eql(\"Rex\");",
							"pm.expect(pm.response.json().tags).to.have.lengthOf(2);",
						},
					},
				},
			},
		},
		map[string]any{
			"name": "Upload",
			"request": map[string]any{
				"method": "POST",
				"url":    "https://files.example.com/upload",
				"auth": map[string]any{
					"type": "apikey",
					"apikey": []any{
						map[string]any{"key": "key", "value": "X-Key"},
						map[string]any{"key": "value", "value": "{{apikey}}"},
						map[string]any{"key": "in", "value": "header"},
					},
				},
				"body": map[string]any{
					"mode": "formdata",
					"formdata": []any{
						map[string]any{"key": "meta", "value": "x", "type": "text"},
						map[string]any{"key": "file", "type": "file", "src": "/tmp/a.png"},
					},
				},
			},
		},
	},
}

func TestURLConversion(t *testing.T) {
	reqMap := sampleCollection["item"].([]any)[1].(map[string]any)["request"].(map[string]any)
	u, query, pathVars := ConvertURL(reqMap["url"])
	if u != "{{baseUrl}}/pets/{{petId}}" {
		t.Errorf("expected {{baseUrl}}/pets/{{petId}}, got %q", u)
	}
	if query["verbose"] != "true" {
		t.Errorf("expected verbose=true, got %v", query)
	}
	if pathVars["petId"] != "42" {
		t.Errorf("expected petId=42, got %v", pathVars)
	}

	u2, _, _ := ConvertURL("http://h:8080/a/:id?q=1")
	if u2 != "http://h:8080/a/{{id}}" {
		t.Errorf("expected http://h:8080/a/{{id}}, got %q", u2)
	}
}

func TestScriptConversion(t *testing.T) {
	item0 := sampleCollection["item"].([]any)[0].(map[string]any)["item"].([]any)[0].(map[string]any)
	execList := item0["event"].([]any)[0].(map[string]any)["script"].(map[string]any)["exec"].([]any)
	var lines []string
	for _, l := range execList {
		lines = append(lines, l.(string))
	}
	conv := NewScriptConverter(lines)
	conv.Convert()

	if len(conv.Tests) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(conv.Tests))
	}
	if conv.Tests[0]["name"] != "Status code is 200" || conv.Tests[0]["status"] != 200 {
		t.Errorf("bad test 0: %v", conv.Tests[0])
	}
	if conv.Captures["token"] != "json.access_token" || conv.Captures["user_id"] != "json.user.id" {
		t.Errorf("bad captures: %v", conv.Captures)
	}
	if len(conv.Unconverted) != 1 || conv.Unconverted[0] != "someCustomHelper(jsonData);" {
		t.Errorf("bad unconverted: %v", conv.Unconverted)
	}
}

func TestImportCollection(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hit_test_coll_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcFile := filepath.Join(tmpDir, "demo.collection.json")
	b, _ := json.Marshal(sampleCollection)
	_ = os.WriteFile(srcFile, b, 0644)

	collDir := filepath.Join(tmpDir, "collections")
	rep, err := ImportCollection(srcFile, collDir, "")
	if err != nil {
		t.Fatalf("ImportCollection failed: %v", err)
	}

	if rep.Requests != 3 || rep.Folders != 1 {
		t.Errorf("expected 3 requests and 1 folder, got %d and %d", rep.Requests, rep.Folders)
	}

	dest := filepath.Join(collDir, "demo-api")
	defYAML, err := zone.LoadYAML(filepath.Join(dest, "_defaults.yaml"))
	if err != nil {
		t.Fatalf("failed to load _defaults.yaml: %v", err)
	}
	if auth, ok := defYAML["auth"].(map[string]any); !ok || auth["type"] != "bearer" {
		t.Errorf("bad defaults auth: %v", defYAML["auth"])
	}

	loginYAML, err := zone.LoadYAML(filepath.Join(dest, "01-auth", "01-login.yaml"))
	if err != nil {
		t.Fatalf("failed to load 01-login.yaml: %v", err)
	}
	if loginYAML["method"] != "POST" || loginYAML["auth"] != "none" {
		t.Errorf("bad login yaml: %v", loginYAML)
	}
	caps, ok := loginYAML["captures"].(map[string]any)
	if !ok || caps["token"] != "json.access_token" {
		t.Errorf("bad captures in login yaml: %v", loginYAML["captures"])
	}
	unc, ok := loginYAML["unconverted"].(map[string]any)
	if !ok {
		t.Errorf("expected unconverted key in login yaml, got %v", loginYAML)
	} else if tests, ok := unc["test"].([]any); !ok || len(tests) == 0 {
		t.Errorf("expected unconverted test lines: %v", unc)
	}
}

func TestImportEnvironment(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hit_test_env_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	envInput := map[string]any{
		"name": "Dev Env",
		"values": []any{
			map[string]any{"key": "baseUrl", "value": "https://dev.example.com", "enabled": true, "type": "default"},
			map[string]any{"key": "pass", "value": "hunter2", "enabled": true, "type": "secret"},
			map[string]any{"key": "off", "value": "x", "enabled": false},
		},
	}
	srcFile := filepath.Join(tmpDir, "dev.environment.json")
	b, _ := json.Marshal(envInput)
	_ = os.WriteFile(srcFile, b, 0644)

	envDir := filepath.Join(tmpDir, "servers")
	envPath, secPath, err := ImportEnvironment(srcFile, envDir, "")
	if err != nil {
		t.Fatalf("ImportEnvironment failed: %v", err)
	}

	if filepath.Base(envPath) != "dev-env.yaml" {
		t.Errorf("expected dev-env.yaml, got %s", filepath.Base(envPath))
	}
	envData, err := zone.LoadYAML(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if envData["base_url"] != "https://dev.example.com" {
		t.Errorf("expected base_url https://dev.example.com, got %v", envData["base_url"])
	}

	secData, err := zone.LoadYAML(secPath)
	if err != nil {
		t.Fatal(err)
	}
	secVars, ok := secData["vars"].(map[string]any)
	if !ok || secVars["pass"] != "hunter2" {
		t.Errorf("expected pass=hunter2 in secrets, got %v", secData)
	}
	if _, hasOff := secVars["off"]; hasOff {
		t.Errorf("disabled key 'off' should not be present")
	}
}

func TestImportGlobals(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hit_test_globals_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	zoneFile := filepath.Join(tmpDir, "zone.yaml")
	_ = os.WriteFile(zoneFile, []byte("name: Test\nvars:\n  existing: hello\n"), 0644)

	globalsInput := map[string]any{
		"values": []any{
			map[string]any{"key": "gKey", "value": "gVal", "enabled": true},
			map[string]any{"key": "gDisabled", "value": "none", "enabled": false},
		},
	}
	globFile := filepath.Join(tmpDir, "globals.json")
	b, _ := json.Marshal(globalsInput)
	_ = os.WriteFile(globFile, b, 0644)

	count, err := ImportGlobals(globFile, zoneFile)
	if err != nil {
		t.Fatalf("ImportGlobals failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 global imported, got %d", count)
	}

	updated, err := zone.LoadYAML(zoneFile)
	if err != nil {
		t.Fatal(err)
	}
	vars := updated["vars"].(map[string]any)
	if vars["existing"] != "hello" || vars["gKey"] != "gVal" {
		t.Errorf("bad merged vars: %v", vars)
	}
	if !reflect.DeepEqual(vars["gKey"], "gVal") {
		t.Errorf("expected gKey == gVal")
	}
}
