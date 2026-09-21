package graphql

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGraphQLExecuteSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %s", r.Header.Get("Content-Type"))
		}

		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)

		if payload["query"] != "query { viewer { name } }" {
			t.Errorf("unexpected query: %v", payload["query"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"viewer":{"name":"Alice"}}}`))
	}))
	defer srv.Close()

	res, err := Execute(ExecuteOptions{
		URL:     srv.URL,
		Query:   "query { viewer { name } }",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.StatusCode != 200 {
		t.Errorf("expected 200, got %d", res.StatusCode)
	}
	if res.HasErrors() {
		t.Errorf("expected no errors, got %d", len(res.Errors))
	}

	dataMap, ok := res.Data.(map[string]any)
	if !ok || dataMap["viewer"].(map[string]any)["name"] != "Alice" {
		t.Errorf("unexpected data: %v", res.Data)
	}
}

func TestGraphQLExecuteWithErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data": null,
			"errors": [
				{
					"message": "Cannot query field 'invalid' on type 'Query'",
					"locations": [{"line": 2, "column": 5}],
					"path": ["viewer", "invalid"]
				}
			]
		}`))
	}))
	defer srv.Close()

	res, err := Execute(ExecuteOptions{
		URL:     srv.URL,
		Query:   "query { viewer { invalid } }",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !res.HasErrors() {
		t.Fatalf("expected errors, got none")
	}
	if len(res.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(res.Errors))
	}

	errSummary := res.FormatErrors()
	if !strings.Contains(errSummary, "Cannot query field 'invalid'") {
		t.Errorf("expected message in summary, got:\n%s", errSummary)
	}
	if !strings.Contains(errSummary, "path: viewer.invalid") {
		t.Errorf("expected path in summary, got:\n%s", errSummary)
	}
	if !strings.Contains(errSummary, "line:col 2:5") {
		t.Errorf("expected locations in summary, got:\n%s", errSummary)
	}
}

func TestGraphQLIntrospect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data": {
				"__schema": {
					"queryType": { "name": "Query" },
					"mutationType": { "name": "Mutation" },
					"types": [
						{
							"kind": "OBJECT",
							"name": "Query",
							"fields": [
								{
									"name": "pet",
									"args": [
										{ "name": "id", "type": { "kind": "NON_NULL", "ofType": { "kind": "SCALAR", "name": "ID" } } }
									],
									"type": { "kind": "OBJECT", "name": "Pet" }
								}
							]
						},
						{
							"kind": "OBJECT",
							"name": "Pet",
							"fields": [
								{ "name": "id", "type": { "kind": "NON_NULL", "ofType": { "kind": "SCALAR", "name": "ID" } } },
								{ "name": "name", "type": { "kind": "SCALAR", "name": "String" } }
							]
						},
						{
							"kind": "ENUM",
							"name": "PetKind",
							"enumValues": [
								{ "name": "DOG" },
								{ "name": "CAT" }
							]
						}
					]
				}
			}
		}`))
	}))
	defer srv.Close()

	sdl, err := Introspect(srv.URL, nil, 5*time.Second, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sdl, "schema {") || !strings.Contains(sdl, "query: Query") {
		t.Errorf("expected schema block in SDL, got:\n%s", sdl)
	}
	if !strings.Contains(sdl, "type Pet {") || !strings.Contains(sdl, "id: ID!") {
		t.Errorf("expected Pet type in SDL, got:\n%s", sdl)
	}
	if !strings.Contains(sdl, "enum PetKind {") || !strings.Contains(sdl, "DOG") {
		t.Errorf("expected PetKind enum in SDL, got:\n%s", sdl)
	}
	if !strings.Contains(sdl, "pet(id: ID!): Pet") {
		t.Errorf("expected pet query in SDL, got:\n%s", sdl)
	}
}
