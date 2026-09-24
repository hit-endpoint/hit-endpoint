package assertions

import (
	"testing"
)

var resp = map[string]any{
	"status": 201,
	"headers": map[string]any{
		"content-type": "application/json",
		"location":     "/pets/7",
	},
	"json": map[string]any{
		"id":   7,
		"name": "Rex",
		"tags": []any{"a", "b"},
		"meta": map[string]any{"ok": true},
	},
	"text": `{"id": 7}`,
	"ms":   42.0,
}

func TestMatchers(t *testing.T) {
	ok, _ := Match(7, 7)
	if !ok {
		t.Errorf("expected 7 == 7")
	}

	ok, _ = Match(7, "7")
	if !ok {
		t.Errorf("expected 7 == '7' (loose equality)")
	}

	ok, _ = Match("Rex", map[string]any{"contains": "Re"})
	if !ok {
		t.Errorf("expected Rex contains Re")
	}

	ok, _ = Match([]any{"a"}, map[string]any{"contains": "a"})
	if !ok {
		t.Errorf("expected [a] contains a")
	}

	ok, _ = Match(nil, map[string]any{"exists": false})
	if !ok {
		t.Errorf("expected nil exists: false")
	}

	ok, _ = Match(7, map[string]any{"type": "number", "gt": 5, "lte": 7})
	if !ok {
		t.Errorf("expected 7 type number gt 5 lte 7")
	}

	ok, _ = Match(7, map[string]any{"gt": 10})
	if ok {
		t.Errorf("expected 7 > 10 to be false")
	}

	ok, _ = Match("abc", map[string]any{"matches": "^a.c$"})
	if !ok {
		t.Errorf("expected abc matches ^a.c$")
	}

	ok, _ = Match([]any{1, 2}, map[string]any{"length": 2})
	if !ok {
		t.Errorf("expected [1, 2] length 2")
	}

	ok, _ = Match("x", map[string]any{"in": []any{"x", "y"}})
	if !ok {
		t.Errorf("expected x in [x, y]")
	}
}

func TestSearchShortcuts(t *testing.T) {
	val, err := Search("$.name", resp)
	if err != nil || val != "Rex" {
		t.Errorf("expected Rex, got %v (err: %v)", val, err)
	}

	val, err = Search("json.tags[1]", resp)
	if err != nil || val != "b" {
		t.Errorf("expected b, got %v (err: %v)", val, err)
	}

	val, err = Search(`headers."content-type"`, resp)
	if err != nil || val != "application/json" {
		t.Errorf("expected application/json, got %v (err: %v)", val, err)
	}
}

func TestEvaluateMixed(t *testing.T) {
	tests := []map[string]any{
		{"status": "2xx"},
		{"status": []any{200, 201}},
		{"max_ms": 100},
		{"json": map[string]any{
			"name":    "Rex",
			"tags":    map[string]any{"length": 2},
			"meta.ok": true,
			"missing": map[string]any{"exists": false},
		}},
		{"headers": map[string]any{
			"Location": map[string]any{"starts_with": "/pets/"},
		}},
		{"text": map[string]any{"contains": "id"}},
		{"expr": "json.id == `7`"},
		{"name": "custom", "status": 500},
	}

	results := Evaluate(tests, resp)
	var failed []TestResult
	for _, r := range results {
		if !r.Passed {
			failed = append(failed, r)
		}
	}

	if len(results) != 11 {
		t.Fatalf("expected 11 results, got %d", len(results))
	}
	if len(failed) != 1 || failed[0].Name != "custom" {
		t.Errorf("expected 1 failed test 'custom', got %v", failed)
	}
	if failed[0].Detail != "got 201" {
		t.Errorf("expected detail 'got 201', got %q", failed[0].Detail)
	}
}

func TestUnknownKeyAndBadPathFailGracefully(t *testing.T) {
	tests := []map[string]any{
		{"bogus": 1},
		{"json": map[string]any{"[[[": 1}},
	}
	results := Evaluate(tests, resp)
	for _, r := range results {
		if r.Passed {
			t.Errorf("expected test to fail gracefully, but passed: %v", r)
		}
	}
}

func TestDeclarativeAssertionsStyle(t *testing.T) {
	tests := []map[string]any{
		{
			"status":    201,
			"latency":   "< 250ms",
			"body.name": "Rex",
		},
	}
	results := Evaluate(tests, resp)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for _, r := range results {
		if !r.Passed {
			t.Errorf("expected test '%s' to pass, but failed: %s", r.Name, r.Detail)
		}
	}
}
