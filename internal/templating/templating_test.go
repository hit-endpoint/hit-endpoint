package templating

import (
	"os"
	"testing"
)

func TestLaterLayersWin(t *testing.T) {
	ctx := NewContext([]Layer{
		{Name: "zone", Values: map[string]any{"a": 1}},
		{Name: "env", Values: map[string]any{"a": 2}},
	}, nil)

	res, err := Render("{{a}}", ctx, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != 2 {
		t.Errorf("expected 2, got %v", res)
	}
}

func TestNativeTypeKeptForWholePlaceholderAndStringifiedInline(t *testing.T) {
	ctx := NewContext([]Layer{
		{Name: "env", Values: map[string]any{
			"n":    5,
			"obj":  map[string]any{"x": 1},
			"flag": true,
		}},
	}, nil)

	resN, err := Render("{{n}}", ctx, true)
	if err != nil || resN != 5 {
		t.Errorf("expected 5, got %v (err: %v)", resN, err)
	}

	resId, err := Render("id-{{n}}", ctx, true)
	if err != nil || resId != "id-5" {
		t.Errorf("expected id-5, got %v (err: %v)", resId, err)
	}

	resObj, err := Render(map[string]any{"body": "{{obj}}"}, ctx, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := resObj.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", resObj)
	}
	bodyMap, ok := m["body"].(map[string]any)
	if !ok || bodyMap["x"] != 1 {
		t.Errorf("expected map with x=1, got %v", m["body"])
	}

	resFlag, err := Render("flag={{flag}}", ctx, true)
	if err != nil || resFlag != "flag=true" {
		t.Errorf("expected flag=true, got %v (err: %v)", resFlag, err)
	}
}

func TestNestedVariablesResolve(t *testing.T) {
	ctx := NewContext([]Layer{
		{Name: "env", Values: map[string]any{
			"host":     "example.com",
			"base_url": "https://{{host}}/api",
		}},
	}, nil)

	res, err := Render("{{base_url}}/pets", ctx, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != "https://example.com/api/pets" {
		t.Errorf("expected https://example.com/api/pets, got %v", res)
	}
}

func TestMissingIsCollectedAndStrict(t *testing.T) {
	ctx := NewContext(nil, nil)
	_, err := Render(map[string]any{"u": "{{a}}/{{b}}"}, ctx, true)
	if err == nil {
		t.Fatalf("expected error for missing vars")
	}
	mErr, ok := err.(*MissingVariableError)
	if !ok {
		t.Fatalf("expected MissingVariableError, got %T", err)
	}
	if len(mErr.Names) != 2 || mErr.Names[0] != "a" || mErr.Names[1] != "b" {
		t.Errorf("expected [a, b], got %v", mErr.Names)
	}

	// lenient mode
	res, err := Render("{{a}}", ctx, false)
	if err != nil {
		t.Fatalf("unexpected error in lenient mode: %v", err)
	}
	if res != "{{a}}" {
		t.Errorf("expected {{a}}, got %v", res)
	}
}

func TestDynamicValues(t *testing.T) {
	_ = os.Setenv("HIT_TEST_SECRET", "s3cret")
	defer os.Unsetenv("HIT_TEST_SECRET")

	ctx := NewContext(nil, nil)

	uuidVal, err := Render("{{$uuid}}", ctx, true)
	if err != nil || len(uuidVal.(string)) != 36 {
		t.Errorf("bad uuid: %v, err: %v", uuidVal, err)
	}

	guidVal, err := Render("{{$guid}}", ctx, true)
	if err != nil || len(guidVal.(string)) != 36 {
		t.Errorf("bad guid: %v, err: %v", guidVal, err)
	}

	tsVal, err := Render("{{$timestamp}}", ctx, true)
	if err != nil {
		t.Errorf("bad timestamp err: %v", err)
	}
	if _, ok := tsVal.(int64); !ok {
		t.Errorf("expected int64 timestamp, got %T", tsVal)
	}

	secVal, err := Render("{{$env:HIT_TEST_SECRET}}", ctx, true)
	if err != nil || secVal != "s3cret" {
		t.Errorf("expected s3cret, got %v, err: %v", secVal, err)
	}

	fbVal, err := Render("{{$env:HIT_NOT_SET:fallback}}", ctx, true)
	if err != nil || fbVal != "fallback" {
		t.Errorf("expected fallback, got %v, err: %v", fbVal, err)
	}

	_, err = Render("{{$env:HIT_NOT_SET_EITHER}}", ctx, true)
	if err == nil {
		t.Errorf("expected error for missing required env var")
	}
}

func TestMaskSecrets(t *testing.T) {
	ctx := NewContext([]Layer{
		{Name: "env", Values: map[string]any{}},
	}, map[string]bool{"tok-123": true})

	masked := ctx.Mask("Bearer tok-123")
	if masked != "Bearer ********" {
		t.Errorf("expected Bearer ********, got %s", masked)
	}
}
