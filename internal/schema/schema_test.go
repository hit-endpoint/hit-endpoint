package schema

import (
	"encoding/json"
	"testing"
)

func TestGetSchema(t *testing.T) {
	kinds := []string{"request", "chain", "zone"}
	for _, kind := range kinds {
		s, err := GetSchema(kind)
		if err != nil {
			t.Fatalf("GetSchema(%q) returned error: %v", kind, err)
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(s), &parsed); err != nil {
			t.Fatalf("GetSchema(%q) returned invalid JSON: %v", kind, err)
		}
		if parsed["$schema"] != "http://json-schema.org/draft-07/schema#" {
			t.Errorf("expected $schema draft-07, got %v", parsed["$schema"])
		}
	}

	_, err := GetSchema("invalid_kind")
	if err == nil {
		t.Errorf("expected error for invalid_kind, got nil")
	}
}
