package runner_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hit-endpoint/hit-endpoint/internal/flows"
	"github.com/hit-endpoint/hit-endpoint/internal/perf"
	"github.com/hit-endpoint/hit-endpoint/internal/runner"
	"github.com/hit-endpoint/hit-endpoint/internal/zone"
)

type mockPetstore struct {
	mu     sync.Mutex
	token  string
	pets   map[int]map[string]any
	nextID int
}

func newMockPetstore() *mockPetstore {
	return &mockPetstore{
		token: "demo-token-123",
		pets: map[int]map[string]any{
			1: {"id": 1, "name": "Rex", "kind": "dog", "age": 3},
			2: {"id": 2, "name": "Tom", "kind": "cat", "age": 5},
		},
		nextID: 3,
	}
}

func (m *mockPetstore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")

	if r.Method == "GET" && path == "health" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "time": time.Now().Unix()})
		return
	}

	if r.Method == "POST" && len(parts) == 2 && parts[0] == "auth" && parts[1] == "login" {
		var body map[string]any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		if body["username"] == "admin" && body["password"] == "hunter2" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": m.token,
				"token_type":   "bearer",
				"expires_in":   3600,
			})
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "bad credentials"})
		return
	}

	// Auth check for remaining endpoints
	authHeader := r.Header.Get("Authorization")
	if authHeader != "Bearer "+m.token {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "missing or invalid bearer token"})
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	switch r.Method {
	case "GET":
		if path == "pets" {
			kind := r.URL.Query().Get("kind")
			var items []any
			for _, p := range m.pets {
				if kind == "" || p["kind"] == kind {
					items = append(items, p)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Total-Count", strconv.Itoa(len(items)))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": items,
				"total": len(items),
			})
			return
		}
		if len(parts) == 2 && parts[0] == "pets" {
			id, err := strconv.Atoi(parts[1])
			if err == nil {
				if p, ok := m.pets[id]; ok {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(p)
					return
				}
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "no such pet"})
			return
		}

	case "POST":
		if path == "pets" {
			var pData map[string]any
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &pData)
			name, _ := pData["name"].(string)
			if name == "" {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "name is required"})
				return
			}
			pid := m.nextID
			m.nextID++
			pet := map[string]any{
				"id":   pid,
				"name": name,
				"kind": pData["kind"],
				"age":  pData["age"],
			}
			m.pets[pid] = pet
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Location", fmt.Sprintf("/pets/%d", pid))
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(pet)
			return
		}

	case "DELETE":
		if len(parts) == 2 && parts[0] == "pets" {
			id, err := strconv.Atoi(parts[1])
			if err == nil {
				if _, ok := m.pets[id]; ok {
					delete(m.pets, id)
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "no such pet"})
			return
		}
	}

	w.WriteHeader(http.StatusNotFound)
}

func setupTestZone(t *testing.T, serverURL string) (string, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "hit_e2e_ws_*")
	if err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join("..", "..", "examples", "petstore-zone")
	absSrc, err := filepath.Abs(srcDir)
	if err != nil {
		t.Fatal(err)
	}

	err = filepath.Walk(absSrc, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(absSrc, path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(rel, ".hit") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dest := filepath.Join(tmpDir, rel)
		if info.IsDir() {
			return os.MkdirAll(dest, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}

	// Update local.yaml base_url in servers
	for _, dir := range []string{"servers"} {
		localEnv := filepath.Join(tmpDir, dir, "local.yaml")
		if b, err := os.ReadFile(localEnv); err == nil {
			newContent := strings.ReplaceAll(string(b), "http://127.0.0.1:8765", serverURL)
			_ = os.WriteFile(localEnv, []byte(newContent), 0644)
		}
	}

	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}
	return tmpDir, cleanup
}

func TestChainedRequestsShareCapturedState(t *testing.T) {
	mock := newMockPetstore()
	server := httptest.NewServer(mock)
	defer server.Close()

	zonePath, cleanup := setupTestZone(t, server.URL)
	defer cleanup()

	z, err := zone.Find(zonePath)
	if err != nil {
		t.Fatal(err)
	}

	sess, err := runner.NewSession(runner.SessionOptions{
		Zone: z,
		ServerName:   "local",
		Persist:   true,
	})
	if err != nil {
		t.Fatal(err)
	}

	health := sess.Run("health", nil)
	if !health.OK() || health.Status != 200 {
		t.Fatalf("health failed: %v", health)
	}

	unauth := sess.Run("pets/list", nil)
	if unauth.Status != 401 || unauth.OK() {
		t.Fatalf("expected unauth 401, got %v", unauth.Status)
	}

	login := sess.Run("petstore/auth/login", nil)
	if !login.OK() {
		t.Fatalf("login failed: %v", login.FailedTests())
	}
	if login.Captures["token"] != "demo-token-123" {
		t.Fatalf("token capture failed: %v", login.Captures["token"])
	}

	pets := sess.Run("pets/list", map[string]any{"vars": map[string]any{"kind": "cat"}})
	if !pets.OK() {
		t.Fatalf("pets/list failed: %v", pets.FailedTests())
	}
	if pets.RequestHeaders["Authorization"] != "Bearer demo-token-123" {
		t.Fatalf("expected Bearer demo-token-123 header, got %v", pets.RequestHeaders["Authorization"])
	}
	sess.Close()

	// State persisted to disk: brand new session still has token
	sess2, err := runner.NewSession(runner.SessionOptions{
		Zone: z,
		ServerName:   "local",
		Persist:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess2.Close()

	if sess2.Variables()["token"] != "demo-token-123" {
		t.Fatalf("persisted token not found in new session: %v", sess2.Variables()["token"])
	}

	sess2.SetVar("first_pet_id", 1, nil)
	getPet := sess2.Run("pets/get", nil)
	if !getPet.OK() {
		t.Fatalf("pets/get failed: %v", getPet.FailedTests())
	}
}

func TestFlowRunsAllSteps(t *testing.T) {
	mock := newMockPetstore()
	server := httptest.NewServer(mock)
	defer server.Close()

	zonePath, cleanup := setupTestZone(t, server.URL)
	defer cleanup()

	z, err := zone.Find(zonePath)
	if err != nil {
		t.Fatal(err)
	}

	sess, err := runner.NewSession(runner.SessionOptions{
		Zone: z,
		ServerName:   "local",
		Persist:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	flowPath := filepath.Join(z.ChainsDir(), "login-and-crud.yaml")
	flowData, err := flows.LoadFlow(flowPath)
	if err != nil {
		t.Fatal(err)
	}

	fr := flows.RunFlow(sess, flowData, nil, nil, true, 0)
	if fr.Error != "" {
		t.Fatalf("flow error: %s", fr.Error)
	}
	if !fr.OK() {
		t.Fatalf("flow not ok: %v", fr)
	}
	if len(fr.Results) != 6 {
		t.Fatalf("expected 6 results, got %d", len(fr.Results))
	}

	expectedStatuses := []int{200, 200, 201, 200, 204, 404}
	for i, r := range fr.Results {
		if r.Status != expectedStatuses[i] {
			t.Errorf("step %d: expected status %d, got %d", i, expectedStatuses[i], r.Status)
		}
	}

	createdName, _ := fr.Results[2].Captures["last_created_name"].(string)
	if !strings.HasPrefix(createdName, "Pet ") {
		t.Errorf("expected after hook last_created_name starting with 'Pet ', got %q", createdName)
	}
}

func TestPerfReportsLatency(t *testing.T) {
	mock := newMockPetstore()
	server := httptest.NewServer(mock)
	defer server.Close()

	zonePath, cleanup := setupTestZone(t, server.URL)
	defer cleanup()

	z, err := zone.Find(zonePath)
	if err != nil {
		t.Fatal(err)
	}

	sess, err := runner.NewSession(runner.SessionOptions{
		Zone: z,
		ServerName:   "local",
		Persist:   false,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	sp, err := sess.Load("health")
	if err != nil {
		t.Fatal(err)
	}

	report, err := perf.RunPerf(sess, sp, perf.PerfOptions{
		Concurrency: 5,
		Total:       40,
	})
	if err != nil {
		t.Fatal(err)
	}

	if report.Completed != 40 || report.Failed != 0 {
		t.Errorf("expected 40 completed and 0 failed, got %d and %d", report.Completed, report.Failed)
	}
	stats := report.Stats()
	if stats["p95"] < stats["p50"] || stats["p50"] < 0 || stats["max"] <= 0 {
		t.Errorf("bad stats: p95=%v, p50=%v, max=%v", stats["p95"], stats["p50"], stats["max"])
	}
	if report.StatusCounts[200] != 40 {
		t.Errorf("expected 40 status 200, got %v", report.StatusCounts)
	}
}

func TestAfterHookDoesNotOverwriteFreshCaptures(t *testing.T) {
	mock := newMockPetstore()
	server := httptest.NewServer(mock)
	defer server.Close()

	zonePath, cleanup := setupTestZone(t, server.URL)
	defer cleanup()

	z, err := zone.Find(zonePath)
	if err != nil {
		t.Fatal(err)
	}

	sess, err := runner.NewSession(runner.SessionOptions{
		Zone: z,
		ServerName:   "local",
		Persist:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	_ = sess.Run("auth/login", nil)
	first := sess.Run("pets/create", nil)
	second := sess.Run("pets/create", nil)

	if !first.OK() || !second.OK() {
		t.Fatalf("create failed: first=%v, second=%v", first.FailedTests(), second.FailedTests())
	}

	firstID := int(first.JSON.(map[string]any)["id"].(float64))
	secondID := int(second.JSON.(map[string]any)["id"].(float64))
	if secondID <= firstID {
		t.Errorf("expected secondID > firstID (%d vs %d)", secondID, firstID)
	}
	if int(second.Captures["new_pet_id"].(float64)) != secondID {
		t.Errorf("second capture new_pet_id %v != secondID %d", second.Captures["new_pet_id"], secondID)
	}
}

func TestRunnerMatrixExecution(t *testing.T) {
	mock := newMockPetstore()
	server := httptest.NewServer(mock)
	defer server.Close()

	zonePath, cleanup := setupTestZone(t, server.URL)
	defer cleanup()

	// Write a matrix request spec into collections/petstore/matrix-create.yaml
	matrixFile := filepath.Join(zonePath, "collections", "petstore", "matrix-create.yaml")
	matrixYAML := `name: Matrix Pet Creation
method: POST
url: "{{base_url}}/pets"
matrix:
  rows:
    - { name: "Buddy", kind: "dog", age: 2 }
    - { name: "Whiskers", kind: "cat", age: 4 }
    - { name: "Goldie", kind: "fish", age: 1 }
body:
  json:
    name: "{{row.name}}"
    kind: "{{row.kind}}"
    age: "{{row.age}}"
tests:
  - status: 201
  - json:
      name: "{{row.name}}"
`
	if err := os.WriteFile(matrixFile, []byte(matrixYAML), 0644); err != nil {
		t.Fatal(err)
	}

	z, err := zone.Find(zonePath)
	if err != nil {
		t.Fatal(err)
	}

	sess, err := runner.NewSession(runner.SessionOptions{
		Zone: z,
		ServerName:   "local",
		Persist:   false,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	_ = sess.Run("petstore/auth/login", nil)
	result := sess.Run("petstore/matrix-create", nil)

	if !result.OK() {
		t.Fatalf("expected matrix result to pass, failed: %v, error: %s", result.FailedTests(), result.Error)
	}

	if len(result.Matrix) != 3 {
		t.Fatalf("expected 3 matrix child results, got %d", len(result.Matrix))
	}

	for i, child := range result.Matrix {
		if !child.OK() {
			t.Errorf("child %d failed: %v", i, child.FailedTests())
		}
		if child.Status != 201 {
			t.Errorf("child %d status = %d, expected 201", i, child.Status)
		}
	}
}

func TestDeclarativeSpecExecution(t *testing.T) {
	mock := newMockPetstore()
	server := httptest.NewServer(mock)
	defer server.Close()

	zonePath, cleanup := setupTestZone(t, server.URL)
	defer cleanup()

	declFile := filepath.Join(zonePath, "collections", "petstore", "declarative.yaml")
	declYAML := `name: Declarative Health
method: GET
path: /health
assert:
  status: 200
  latency: < 500ms
  body.status: ok
`
	if err := os.WriteFile(declFile, []byte(declYAML), 0644); err != nil {
		t.Fatal(err)
	}

	z, err := zone.Find(zonePath)
	if err != nil {
		t.Fatal(err)
	}

	sess, err := runner.NewSession(runner.SessionOptions{
		Zone:       z,
		ServerName: "local",
		Persist:    false,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	result := sess.Run("petstore/declarative", nil)
	if !result.OK() {
		t.Fatalf("expected declarative spec to pass, failed: %v, error: %s", result.FailedTests(), result.Error)
	}
	if len(result.Tests) != 4 {
		t.Fatalf("expected 4 assertions (1 default + 3 spec), got %d", len(result.Tests))
	}
}

