package report

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"hit/internal/types"
)

// FailureItem details an individual request or assertion failure.
type FailureItem struct {
	Name        string            `json:"name,omitempty"`
	Ref         string            `json:"ref,omitempty"`
	Method      string            `json:"method"`
	URL         string            `json:"url"`
	Status      int               `json:"status,omitempty"`
	ElapsedMs   float64           `json:"elapsed_ms"`
	Error       string            `json:"error,omitempty"`
	FailedTests []FailedTestCheck `json:"failed_tests,omitempty"`
}

type FailedTestCheck struct {
	Name   string `json:"name"`
	Detail string `json:"detail"`
}

// FailureWebhookPayload is sent to the configured webhook endpoint when test assertions fail.
type FailureWebhookPayload struct {
	Event         string        `json:"event"`
	Timestamp     string        `json:"timestamp"`
	Zone          string        `json:"zone,omitempty"`
	Server        string        `json:"server,omitempty"`
	TotalRequests int           `json:"total_requests"`
	FailedCount   int           `json:"failed_count"`
	FlowErrors    []string      `json:"flow_errors,omitempty"`
	Failures      []FailureItem `json:"failures"`
}

// BuildFailureWebhookPayload constructs the JSON event payload from execution results.
func BuildFailureWebhookPayload(results []*types.Result, flowErrors []string, zoneName, serverName string) *FailureWebhookPayload {
	payload := &FailureWebhookPayload{
		Event:         "hit.run.failure",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Zone:          zoneName,
		Server:        serverName,
		TotalRequests: len(results),
		FlowErrors:    flowErrors,
		Failures:      make([]FailureItem, 0),
	}

	for _, r := range results {
		if r.OK() {
			continue
		}

		item := FailureItem{
			Name:      r.Name,
			Ref:       r.Ref,
			Method:    r.Method,
			URL:       r.Url,
			Status:    r.Status,
			ElapsedMs: r.ElapsedMs,
			Error:     r.Error,
		}

		failedTests := r.FailedTests()
		for _, ft := range failedTests {
			item.FailedTests = append(item.FailedTests, FailedTestCheck{
				Name:   ft.Name,
				Detail: ft.Detail,
			})
		}

		payload.Failures = append(payload.Failures, item)
	}

	payload.FailedCount = len(payload.Failures)
	return payload
}

// SendFailureWebhook posts the failure event to the specified webhook URL with a 5-second timeout.
func SendFailureWebhook(webhookURL string, results []*types.Result, flowErrors []string, zoneName, serverName string) error {
	return SendFailureWebhookWithClient(&http.Client{}, webhookURL, results, flowErrors, zoneName, serverName)
}

// SendFailureWebhookWithClient posts the failure event using a custom http.Client (e.g. for testing).
func SendFailureWebhookWithClient(client *http.Client, webhookURL string, results []*types.Result, flowErrors []string, zoneName, serverName string) error {
	if webhookURL == "" {
		return nil
	}
	if client == nil {
		client = &http.Client{}
	}

	payload := BuildFailureWebhookPayload(results, flowErrors, zoneName, serverName)
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal failure webhook payload: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "hit/0.1.0 (failure-webhook)")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodySnippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("webhook server returned HTTP %d: %s", resp.StatusCode, string(bodySnippet))
	}

	return nil
}
