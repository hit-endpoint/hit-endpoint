package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hit-endpoint/hit-endpoint/internal/history"
	"github.com/hit-endpoint/hit-endpoint/internal/types"
)

func TestBuildHARFromResults(t *testing.T) {
	results := []*types.Result{
		{
			Ref:            "pets/list",
			Method:         "GET",
			Url:            "https://api.example.com/pets?kind=dog&limit=10",
			RequestHeaders: map[string]string{"Accept": "application/json", "Cookie": "session=abc; theme=dark"},
			RequestBody:    "",
			Status:         200,
			Reason:         "OK",
			Headers:        map[string]string{"Content-Type": "application/json", "Set-Cookie": "auth=xyz; Path=/; HttpOnly; Secure"},
			Text:           `[{"id": 1, "name": "Rex"}]`,
			ElapsedMs:      32.5,
			Size:           26,
		},
		{
			Ref:            "pets/create",
			Method:         "POST",
			Url:            "https://api.example.com/pets",
			RequestHeaders: map[string]string{"Content-Type": "application/json"},
			RequestBody:    `{"name": "Bella", "kind": "dog"}`,
			Status:         201,
			Reason:         "Created",
			Headers:        map[string]string{"Content-Type": "application/json", "Location": "/pets/2"},
			Text:           `{"id": 2, "name": "Bella"}`,
			ElapsedMs:      45.0,
			Size:           27,
		},
	}

	har := BuildHARFromResults(results)

	if har.Log.Version != "1.2" {
		t.Fatalf("expected version 1.2, got %q", har.Log.Version)
	}
	if har.Log.Creator.Name != "hit-api-tester" {
		t.Fatalf("expected creator hit-api-tester, got %q", har.Log.Creator.Name)
	}
	if len(har.Log.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(har.Log.Entries))
	}

	// Verify entry 0 (GET with query string and cookies)
	e0 := har.Log.Entries[0]
	if e0.Request.Method != "GET" {
		t.Errorf("entry 0: expected method GET, got %s", e0.Request.Method)
	}
	if len(e0.Request.QueryString) != 2 {
		t.Errorf("entry 0: expected 2 query params, got %d", len(e0.Request.QueryString))
	}
	if len(e0.Request.Cookies) != 2 {
		t.Errorf("entry 0: expected 2 request cookies, got %d", len(e0.Request.Cookies))
	}
	if len(e0.Response.Cookies) != 1 {
		t.Errorf("entry 0: expected 1 response cookie, got %d", len(e0.Response.Cookies))
	}
	if !e0.Response.Cookies[0].HTTPOnly || !e0.Response.Cookies[0].Secure {
		t.Errorf("entry 0: expected HttpOnly and Secure cookie attributes")
	}
	if e0.Response.Content.MimeType != "application/json" {
		t.Errorf("entry 0: expected MIME application/json, got %s", e0.Response.Content.MimeType)
	}
	if e0.Time != 32.5 || e0.Timings.Wait != 32.5 {
		t.Errorf("entry 0: expected timing 32.5, got %v", e0.Time)
	}

	// Verify entry 1 (POST with body and redirect/location)
	e1 := har.Log.Entries[1]
	if e1.Request.Method != "POST" {
		t.Errorf("entry 1: expected method POST, got %s", e1.Request.Method)
	}
	if e1.Request.PostData == nil || e1.Request.PostData.Text != `{"name": "Bella", "kind": "dog"}` {
		t.Errorf("entry 1: expected postData text, got %+v", e1.Request.PostData)
	}
	if e1.Response.RedirectURL != "/pets/2" {
		t.Errorf("entry 1: expected redirectURL /pets/2, got %s", e1.Response.RedirectURL)
	}

	// Verify JSON serialization round-trip
	data, err := ToJSON(har)
	if err != nil {
		t.Fatalf("failed to serialize HAR to JSON: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse generated HAR JSON: %v", err)
	}
}

func TestBuildHARFromHistory(t *testing.T) {
	entries := []*history.Entry{
		{
			ID:              "hit_0123456789_abcdef",
			Timestamp:       "2026-09-09T20:15:00.000Z",
			Method:          "GET",
			Url:             "https://api.example.com/health",
			Headers:         map[string]string{"User-Agent": "hit/0.1.0"},
			Status:          200,
			Reason:          "OK",
			ElapsedMs:       15.2,
			ResponseHeaders: map[string]string{"Content-Type": "application/json"},
			ResponseBody:    `{"status": "healthy"}`,
			ResponseSize:    22,
		},
	}

	har := BuildHARFromHistory(entries)
	if len(har.Log.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(har.Log.Entries))
	}

	e := har.Log.Entries[0]
	if e.StartedDateTime != "2026-09-09T20:15:00.000Z" {
		t.Errorf("expected timestamp preserved, got %s", e.StartedDateTime)
	}
	if e.Response.Status != 200 {
		t.Errorf("expected status 200, got %d", e.Response.Status)
	}
	if e.Response.Content.Text != `{"status": "healthy"}` {
		t.Errorf("expected body preserved, got %s", e.Response.Content.Text)
	}
}

func TestWriteHAR(t *testing.T) {
	tempDir := t.TempDir()
	outPath := filepath.Join(tempDir, "sub", "archive.har")

	har := newBaseHAR()
	har.Log.Entries = append(har.Log.Entries, HAREntry{
		StartedDateTime: "2026-09-09T23:00:00.000Z",
		Time:            10.0,
		Request: HARRequest{
			Method:      "GET",
			URL:         "https://example.com",
			HTTPVersion: "HTTP/1.1",
			Cookies:     []HARCookie{},
			Headers:     []HARHeader{},
			QueryString: []HARQueryParam{},
		},
		Response: HARResponse{
			Status:      200,
			StatusText:  "OK",
			HTTPVersion: "HTTP/1.1",
			Cookies:     []HARCookie{},
			Headers:     []HARHeader{},
			Content: HARContent{
				Size:     2,
				MimeType: "text/plain",
				Text:     "OK",
			},
		},
		Cache: HARCache{},
		Timings: HARTimings{
			Send:    0,
			Wait:    10.0,
			Receive: 0,
		},
	})

	if err := WriteHAR(har, outPath); err != nil {
		t.Fatalf("WriteHAR failed: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("written HAR is invalid JSON: %v", err)
	}
}
