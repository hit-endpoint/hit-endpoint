package graphql

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// GraphQLErrorLocation records the line and column of a syntax or validation error.
type GraphQLErrorLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// GraphQLError represents an execution error returned in the GraphQL response "errors" array.
type GraphQLError struct {
	Message    string                 `json:"message"`
	Locations  []GraphQLErrorLocation `json:"locations,omitempty"`
	Path       []any                  `json:"path,omitempty"`
	Extensions map[string]any         `json:"extensions,omitempty"`
}

// GraphQLResult contains the parsed GraphQL execution response and performance metrics.
type GraphQLResult struct {
	StatusCode int               `json:"status_code"`
	ElapsedMs  float64           `json:"elapsed_ms"`
	Headers    map[string]string `json:"headers"`
	Data       any               `json:"data"`
	Errors     []GraphQLError    `json:"errors,omitempty"`
	RawBody    []byte            `json:"raw_body"`
}

// HasErrors returns true if the GraphQL response contains any errors.
func (r *GraphQLResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// FormatErrors returns a human-readable, formatted summary of GraphQL errors.
func (r *GraphQLResult) FormatErrors() string {
	if len(r.Errors) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d GraphQL Error(s) detected:\n", len(r.Errors)))
	for i, err := range r.Errors {
		sb.WriteString(fmt.Sprintf("  %d. %s", i+1, err.Message))
		if len(err.Path) > 0 {
			var pathParts []string
			for _, p := range err.Path {
				pathParts = append(pathParts, fmt.Sprintf("%v", p))
			}
			sb.WriteString(fmt.Sprintf(" (path: %s)", strings.Join(pathParts, ".")))
		}
		if len(err.Locations) > 0 {
			var locParts []string
			for _, loc := range err.Locations {
				locParts = append(locParts, fmt.Sprintf("%d:%d", loc.Line, loc.Column))
			}
			sb.WriteString(fmt.Sprintf(" [line:col %s]", strings.Join(locParts, ", ")))
		}
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

// ExecuteOptions configures GraphQL execution.
type ExecuteOptions struct {
	URL           string
	Query         string
	Variables     map[string]any
	OperationName string
	Headers       map[string]string
	Timeout       time.Duration
	Insecure      bool
}

// Execute sends an HTTP POST request to a GraphQL endpoint and returns the parsed result.
func Execute(opts ExecuteOptions) (*GraphQLResult, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 15 * time.Second
	}

	payload := map[string]any{
		"query": opts.Query,
	}
	if len(opts.Variables) > 0 {
		payload["variables"] = opts.Variables
	}
	if opts.OperationName != "" {
		payload["operationName"] = opts.OperationName
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode GraphQL payload: %w", err)
	}

	req, err := http.NewRequest("POST", opts.URL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create GraphQL request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, application/graphql-response+json")

	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{
		Timeout: opts.Timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: opts.Insecure},
		},
	}

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := float64(time.Since(start).Microseconds()) / 1000.0
	if err != nil {
		return nil, fmt.Errorf("GraphQL transport error: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read GraphQL response: %w", err)
	}

	respHeaders := make(map[string]string)
	for k, vals := range resp.Header {
		respHeaders[k] = strings.Join(vals, ", ")
	}

	result := &GraphQLResult{
		StatusCode: resp.StatusCode,
		ElapsedMs:  elapsed,
		Headers:    respHeaders,
		RawBody:    respBytes,
	}

	var parsed struct {
		Data   any            `json:"data"`
		Errors []GraphQLError `json:"errors"`
	}
	if err := json.Unmarshal(respBytes, &parsed); err == nil {
		result.Data = parsed.Data
		result.Errors = parsed.Errors
	}

	return result, nil
}
