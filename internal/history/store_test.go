package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hit/internal/types"
)

func TestStoreAppendAndList(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hit-history-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	histFile := filepath.Join(tmpDir, "history.jsonl")
	store := NewStore(histFile)

	// Append 3 entries
	e1 := &Entry{
		ID:        "hit_1",
		Timestamp: "2026-09-09T10:00:00Z",
		Method:    "GET",
		Url:       "https://api.example.com/users",
		Status:    200,
		Ref:       "users/list",
	}
	e2 := &Entry{
		ID:        "hit_2",
		Timestamp: "2026-09-09T10:01:00Z",
		Method:    "POST",
		Url:       "https://api.example.com/users",
		Status:    201,
		Ref:       "users/create",
	}
	e3 := &Entry{
		ID:        "hit_3",
		Timestamp: "2026-09-09T10:02:00Z",
		Method:    "GET",
		Url:       "https://api.example.com/users/42",
		Status:    404,
		Ref:       "users/get",
	}

	if err := store.Append(e1); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(e2); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(e3); err != nil {
		t.Fatal(err)
	}

	// List all (newest first)
	list, err := store.List(0, 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(list))
	}
	if list[0].ID != "hit_3" || list[1].ID != "hit_2" || list[2].ID != "hit_1" {
		t.Fatalf("expected newest first order, got %s, %s, %s", list[0].ID, list[1].ID, list[2].ID)
	}

	// Filter by status 200
	list200, err := store.List(0, 200, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list200) != 1 || list200[0].ID != "hit_1" {
		t.Fatalf("expected only hit_1, got %v", list200)
	}

	// Filter by method POST
	listPost, err := store.List(0, 0, "POST", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(listPost) != 1 || listPost[0].ID != "hit_2" {
		t.Fatalf("expected only hit_2, got %v", listPost)
	}

	// Get by 1-based index (1 = most recent = hit_3)
	got1, err := store.Get("1")
	if err != nil {
		t.Fatal(err)
	}
	if got1.ID != "hit_3" {
		t.Fatalf("expected hit_3 for index 1, got %s", got1.ID)
	}

	// Get by ID
	got2, err := store.Get("hit_2")
	if err != nil {
		t.Fatal(err)
	}
	if got2.Ref != "users/create" {
		t.Fatalf("expected users/create for hit_2, got %s", got2.Ref)
	}

	// Clear
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	emptyList, _ := store.List(0, 0, "", "")
	if len(emptyList) != 0 {
		t.Fatalf("expected 0 entries after clear, got %d", len(emptyList))
	}
}

func TestStorePrune(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hit-history-prune-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	histFile := filepath.Join(tmpDir, "history.jsonl")
	store := NewStore(histFile)

	for i := 1; i <= 10; i++ {
		_ = store.Append(&Entry{
			ID:     strings.Repeat("a", i),
			Status: 200,
		})
	}

	// Prune to max 4 entries
	if err := store.Prune(4); err != nil {
		t.Fatal(err)
	}

	list, err := store.List(0, 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 4 {
		t.Fatalf("expected 4 entries after prune, got %d", len(list))
	}
	// Newest is 10 'a's
	if list[0].ID != strings.Repeat("a", 10) {
		t.Fatalf("expected newest to remain, got %s", list[0].ID)
	}
}

func TestNewEntryFromResultAndMasking(t *testing.T) {
	r := &types.Result{
		Method: "POST",
		Url:    "https://api.example.com/pets?token=secret123",
		RequestHeaders: map[string]string{
			"Authorization": "Bearer supersecret",
			"Content-Type":  "application/json",
		},
		RequestBody: `{"password": "mypassword"}`,
		Status:      201,
		Reason:      "Created",
		ElapsedMs:   18.5,
		Size:        50,
	}

	maskFn := func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(s, "secret123", "[MASKED]"), "mypassword", "[MASKED]")
	}

	entry := NewEntryFromResult(r, "run", "my-ws", "staging", maskFn)
	if entry.Headers["Authorization"] != "[MASKED]" {
		t.Errorf("expected Authorization header to be [MASKED], got %s", entry.Headers["Authorization"])
	}
	if !strings.Contains(entry.Url, "[MASKED]") {
		t.Errorf("expected URL query to be masked, got %s", entry.Url)
	}
	if !strings.Contains(entry.Body, "[MASKED]") {
		t.Errorf("expected Body to be masked, got %s", entry.Body)
	}
}

func TestPromoteToYAML(t *testing.T) {
	entry := &Entry{
		Method: "POST",
		Url:    "https://api.example.com/pets",
		Headers: map[string]string{
			"Content-Type": "application/json",
			"User-Agent":   "hit/1.0",
		},
		Body:   `{"name":"Tom","kind":"cat"}`,
		Status: 201,
	}

	yamlStr, err := PromoteToYAML(entry, "Create Pet")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(yamlStr, "name: Create Pet") {
		t.Errorf("missing name: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "method: POST") {
		t.Errorf("missing method: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "status: 201") {
		t.Errorf("missing test status: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "name: Tom") {
		t.Errorf("missing JSON body content: %s", yamlStr)
	}
	// User-Agent should be filtered out
	if strings.Contains(yamlStr, "User-Agent") {
		t.Errorf("User-Agent should have been filtered out: %s", yamlStr)
	}
}
