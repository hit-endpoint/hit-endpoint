package diff

import (
	"strings"
	"testing"

	"hit/internal/output"
)

func TestCanonicalizeBody(t *testing.T) {
	json1 := `{"b": 2, "a": 1}`
	json2 := `{"a": 1, "b": 2}`

	canon1, ok1 := CanonicalizeBody(json1)
	canon2, ok2 := CanonicalizeBody(json2)

	if !ok1 || !ok2 {
		t.Fatalf("expected both to be valid JSON: ok1=%v, ok2=%v", ok1, ok2)
	}

	if canon1 != canon2 {
		t.Errorf("expected canonical JSON to match regardless of key order:\n1: %s\n2: %s", canon1, canon2)
	}

	rawText := "plain text body"
	canonRaw, okRaw := CanonicalizeBody(rawText)
	if okRaw {
		t.Errorf("expected plain text not to be parsed as JSON")
	}
	if canonRaw != rawText {
		t.Errorf("expected raw text to remain unchanged: got %q, want %q", canonRaw, rawText)
	}
}

func TestDiffLines(t *testing.T) {
	oldLines := []string{"apple", "banana", "cherry"}
	newLines := []string{"apple", "blueberry", "cherry", "date"}

	diffs := DiffLines(oldLines, newLines)

	expected := []struct {
		typ  DiffType
		text string
	}{
		{DiffEqual, "apple"},
		{DiffDelete, "banana"},
		{DiffInsert, "blueberry"},
		{DiffEqual, "cherry"},
		{DiffInsert, "date"},
	}

	if len(diffs) != len(expected) {
		t.Fatalf("expected %d diff lines, got %d", len(expected), len(diffs))
	}

	for i, exp := range expected {
		if diffs[i].Type != exp.typ || diffs[i].Text != exp.text {
			t.Errorf("line %d: got (%v, %q), want (%v, %q)", i, diffs[i].Type, diffs[i].Text, exp.typ, exp.text)
		}
	}
}

func TestGroupHunks(t *testing.T) {
	diffs := []DiffLine{
		{DiffEqual, "line1"},
		{DiffEqual, "line2"},
		{DiffDelete, "line3"},
		{DiffInsert, "line3_modified"},
		{DiffEqual, "line4"},
		{DiffEqual, "line5"},
	}

	hunks := GroupHunks(diffs, 1)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hunks))
	}

	h := hunks[0]
	// with contextLines=1, lines included should be line2, line3 (del), line3_modified (ins), line4
	if len(h.Lines) != 4 {
		t.Errorf("expected 4 lines in hunk, got %d", len(h.Lines))
	}
	if h.OldStart != 2 || h.NewStart != 2 {
		t.Errorf("expected OldStart=2, NewStart=2; got OldStart=%d, NewStart=%d", h.OldStart, h.NewStart)
	}

	// No changes test
	noChangeDiffs := []DiffLine{
		{DiffEqual, "a"},
		{DiffEqual, "b"},
	}
	if GroupHunks(noChangeDiffs, 3) != nil {
		t.Errorf("expected nil hunks when no differences exist")
	}
}

func TestCompareHeaders(t *testing.T) {
	oldH := map[string]string{
		"Content-Type":  "application/json",
		"Cache-Control": "no-cache",
		"X-Old-Header":  "deprecated",
	}
	newH := map[string]string{
		"content-type":  "application/json; charset=utf-8",
		"Cache-Control": "no-cache",
		"X-New-Header":  "fresh",
	}

	diffs := CompareHeaders(oldH, newH)
	if len(diffs) != 3 {
		t.Fatalf("expected 3 header differences, got %d", len(diffs))
	}

	typeMap := make(map[string]string)
	for _, d := range diffs {
		typeMap[strings.ToLower(d.Name)] = d.Type
	}

	if typeMap["content-type"] != "changed" {
		t.Errorf("expected content-type changed, got %q", typeMap["content-type"])
	}
	if typeMap["x-old-header"] != "removed" {
		t.Errorf("expected x-old-header removed, got %q", typeMap["x-old-header"])
	}
	if typeMap["x-new-header"] != "added" {
		t.Errorf("expected x-new-header added, got %q", typeMap["x-new-header"])
	}
}

func TestCompareAndFormatTerminal(t *testing.T) {
	oldBody := `{"status": "pending", "count": 5}`
	newBody := `{"status": "completed", "count": 5}`

	noColor := false
	p := output.NewPrinter(&noColor, nil)
	opts := Options{CompareHeaders: true, ContextLines: 2}

	diffResult := Compare(
		"Hit #1", "Hit #2",
		200, "OK", 50.0, 30, map[string]string{"Server": "v1"}, oldBody,
		200, "OK", 45.0, 32, map[string]string{"Server": "v2"}, newBody,
		opts,
	)

	if !diffResult.HasDifferences {
		t.Errorf("expected diffResult.HasDifferences to be true")
	}

	formatted := diffResult.FormatTerminal(p, opts)
	if !strings.Contains(formatted, "Diff: Hit #1  ←  Hit #2") {
		t.Errorf("formatted diff missing header: %s", formatted)
	}
	if !strings.Contains(formatted, "Server") {
		t.Errorf("formatted diff missing header changes: %s", formatted)
	}
	if !strings.Contains(formatted, "status") {
		t.Errorf("formatted diff missing body diff: %s", formatted)
	}
}
