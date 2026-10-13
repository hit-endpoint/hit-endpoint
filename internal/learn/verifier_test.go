package learn

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hit-endpoint/hit-endpoint/internal/history"
	"github.com/hit-endpoint/hit-endpoint/internal/zone"
)

func TestVerifierOnlyReadsZoneHistoryAndFiltersMockURL(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hit_learn_verifier_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWD) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	z, err := zone.InitWizard(zone.WizardOptions{
		Dir:           tmpDir,
		Name:          "test-learn",
		BaseURL:       "http://127.0.0.1:19999",
		PrimaryServer: "local",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. With no local zone history, even if ~/.hit/history.jsonl has prior requests, Lesson 2 should NOT pass
	v := NewVerifier(z, "http://127.0.0.1:19999")
	res := v.VerifyLesson(2)
	for _, r := range res.Checks {
		if r.Passed {
			t.Fatalf("expected Lesson 2 check %q to fail when local zone history is empty, but it passed", r.Description)
		}
	}

	// 2. Write an unrelated request (different host/port) to local zone history -> still should NOT pass
	histPath := filepath.Join(tmpDir, ".hit", "history.jsonl")
	store := history.NewStore(histPath)
	_ = store.Append(&history.Entry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Source:    "adhoc",
		Method:    "GET",
		Url:       "https://api.example.com/pets?kind=dog",
		Status:    200,
	})
	_ = store.Append(&history.Entry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Source:    "adhoc",
		Method:    "POST",
		Url:       "https://api.example.com/pets",
		Body:      `{"name":"Bella","kind":"dog"}`,
		Status:    201,
	})

	v2 := NewVerifier(z, "http://127.0.0.1:19999")
	res2 := v2.VerifyLesson(2)
	for _, r := range res2.Checks {
		if r.Passed {
			t.Fatalf("expected Lesson 2 check %q to ignore unrelated host history, but it passed", r.Description)
		}
	}

	// 3. Write matching requests targeting the mock URL -> Lesson 2 should pass
	_ = store.Append(&history.Entry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Source:    "adhoc",
		Method:    "GET",
		Url:       "http://localhost:19999/pets?kind=dog",
		Status:    200,
	})
	_ = store.Append(&history.Entry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Source:    "adhoc",
		Method:    "POST",
		Url:       "http://127.0.0.1:19999/pets",
		Body:      `{"name":"Bella","kind":"dog"}`,
		Status:    201,
	})

	v3 := NewVerifier(z, "http://127.0.0.1:19999")
	res3 := v3.VerifyLesson(2)
	if !res3.Passed {
		t.Fatalf("expected Lesson 2 to pass after matching mock requests, got checks: %+v", res3.Checks)
	}
}
