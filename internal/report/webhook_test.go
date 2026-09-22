package report

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/hit-endpoint/hit-endpoint/internal/assertions"
	"github.com/hit-endpoint/hit-endpoint/internal/types"
)

type roundTripFunc func(req *http.Request) *http.Response

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}

func TestSendFailureWebhook(t *testing.T) {
	var receivedPayload FailureWebhookPayload
	var receivedContentType string
	var receivedUserAgent string
	called := false

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) *http.Response {
			called = true
			receivedContentType = req.Header.Get("Content-Type")
			receivedUserAgent = req.Header.Get("User-Agent")

			b, err := io.ReadAll(req.Body)
			if err != nil {
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(bytes.NewBufferString("read error")),
				}
			}

			if err := json.Unmarshal(b, &receivedPayload); err != nil {
				return &http.Response{
					StatusCode: http.StatusBadRequest,
					Body:       io.NopCloser(bytes.NewBufferString("json error")),
				}
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(`{"status":"ok"}`)),
			}
		}),
	}

	results := []*types.Result{
		{
			Ref:       "users/list",
			Method:    "GET",
			Url:       "https://api.example.com/users",
			Status:    200,
			HasStatus: true,
			ElapsedMs: 45.0,
		},
		{
			Ref:       "users/create",
			Method:    "POST",
			Url:       "https://api.example.com/users",
			Status:    500,
			HasStatus: true,
			ElapsedMs: 120.0,
			Tests: []assertions.TestResult{
				{Name: "status == 201", Passed: false, Detail: "expected 201 got 500"},
			},
		},
	}

	err := SendFailureWebhookWithClient(client, "http://mock.webhook/api", results, []string{"flow failed at step 2"}, "petstore-zone", "staging")
	if err != nil {
		t.Fatalf("unexpected webhook error: %v", err)
	}

	if !called {
		t.Fatal("expected webhook client to be called")
	}

	if receivedContentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", receivedContentType)
	}
	if receivedUserAgent != "hit/0.1.0 (failure-webhook)" {
		t.Errorf("expected User-Agent hit/0.1.0 (failure-webhook), got %s", receivedUserAgent)
	}

	if receivedPayload.Event != "hit.run.failure" {
		t.Errorf("expected event hit.run.failure, got %s", receivedPayload.Event)
	}
	if receivedPayload.Zone != "petstore-zone" {
		t.Errorf("expected zone petstore-zone, got %s", receivedPayload.Zone)
	}
	if receivedPayload.Server != "staging" {
		t.Errorf("expected server staging, got %s", receivedPayload.Server)
	}
	if receivedPayload.TotalRequests != 2 {
		t.Errorf("expected 2 total requests, got %d", receivedPayload.TotalRequests)
	}
	if receivedPayload.FailedCount != 1 {
		t.Errorf("expected 1 failed request, got %d", receivedPayload.FailedCount)
	}
	if len(receivedPayload.Failures) != 1 {
		t.Fatalf("expected 1 failure item, got %d", len(receivedPayload.Failures))
	}
	if receivedPayload.Failures[0].Ref != "users/create" {
		t.Errorf("expected failed ref users/create, got %s", receivedPayload.Failures[0].Ref)
	}
	if len(receivedPayload.Failures[0].FailedTests) != 1 {
		t.Fatalf("expected 1 failed test check, got %d", len(receivedPayload.Failures[0].FailedTests))
	}
	if receivedPayload.Failures[0].FailedTests[0].Name != "status == 201" {
		t.Errorf("expected test name status == 201, got %s", receivedPayload.Failures[0].FailedTests[0].Name)
	}
}

func TestSendFailureWebhook_ServerError(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) *http.Response {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewBufferString("internal server error")),
			}
		}),
	}

	results := []*types.Result{
		{
			Ref:    "test/req",
			Method: "GET",
			Url:    "http://example.com",
			Error:  "connection error",
		},
	}

	err := SendFailureWebhookWithClient(client, "http://mock.webhook/api", results, nil, "", "")
	if err == nil {
		t.Fatal("expected error on 500 response from webhook server")
	}
}
