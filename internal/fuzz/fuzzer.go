package fuzz

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// FuzzOptions configures the mutation fuzzer.
type FuzzOptions struct {
	MaxMutations int
	Categories   []string
	FailOn5xx    bool
	Timeout      time.Duration
	Insecure     bool
	Verbose      bool
	OnMutation   func(m Mutation, statusCode int, durationMs float64, err error)
}

// Anomaly records an unexpected or dangerous response during fuzzing.
type Anomaly struct {
	MutationName string  `json:"mutation_name"`
	Category     string  `json:"category"`
	StatusCode   int     `json:"status_code"`
	DurationMs   float64 `json:"duration_ms"`
	ResponseBody string  `json:"response_body"`
	Issue        string  `json:"issue"`
	Severity     string  `json:"severity"` // "CRITICAL", "HIGH", "MEDIUM"
}

// FuzzResult aggregates the execution metrics and detected vulnerabilities.
type FuzzResult struct {
	TargetMethod    string    `json:"target_method"`
	TargetURL       string    `json:"target_url"`
	TotalMutations  int       `json:"total_mutations"`
	Handled4xx      int       `json:"handled_4xx"`
	Success2xx      int       `json:"success_2xx"`
	ServerErrors5xx int       `json:"server_errors_5xx"`
	TransportErrors int       `json:"transport_errors"`
	Anomalies       []Anomaly `json:"anomalies"`
	ElapsedMs       float64   `json:"elapsed_ms"`
}

// Run executes the fuzzer against the target endpoint using the synthesized mutation suite.
func Run(method, rawURL string, headers map[string]string, query map[string]string, body []byte, opts FuzzOptions) (*FuzzResult, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Second
	}

	mutations := GenerateMutations(method, rawURL, headers, query, body, opts.Categories)
	if opts.MaxMutations > 0 && len(mutations) > opts.MaxMutations {
		mutations = mutations[:opts.MaxMutations]
	}

	client := &http.Client{
		Timeout: opts.Timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: opts.Insecure},
		},
	}

	result := &FuzzResult{
		TargetMethod:   method,
		TargetURL:      rawURL,
		TotalMutations: len(mutations),
	}

	startTime := time.Now()

	for _, m := range mutations {
		var reqBody io.Reader
		if len(m.Body) > 0 {
			reqBody = bytes.NewReader(m.Body)
		}

		req, err := http.NewRequest(m.Method, m.URL, reqBody)
		if err != nil {
			result.TransportErrors++
			continue
		}

		for k, v := range m.Headers {
			req.Header.Set(k, v)
		}

		reqStart := time.Now()
		resp, err := client.Do(req)
		reqElapsed := float64(time.Since(reqStart).Microseconds()) / 1000.0

		if err != nil {
			result.TransportErrors++
			// Flag connection reset or timeout as anomaly
			issue := "Transport error: " + err.Error()
			severity := "HIGH"
			if strings.Contains(err.Error(), "timeout") {
				issue = "Server hang / Request timed out (> " + opts.Timeout.String() + ")"
			}
			result.Anomalies = append(result.Anomalies, Anomaly{
				MutationName: m.Name,
				Category:     m.Category,
				StatusCode:   0,
				DurationMs:   reqElapsed,
				Issue:        issue,
				Severity:     severity,
			})
			if opts.OnMutation != nil {
				opts.OnMutation(m, 0, reqElapsed, err)
			}
			continue
		}

		bodyBytes, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		bodyPreview := string(bodyBytes)
		if len(bodyPreview) > 200 {
			bodyPreview = bodyPreview[:200] + "..."
		}

		if resp.StatusCode >= 500 {
			result.ServerErrors5xx++
			result.Anomalies = append(result.Anomalies, Anomaly{
				MutationName: m.Name,
				Category:     m.Category,
				StatusCode:   resp.StatusCode,
				DurationMs:   reqElapsed,
				ResponseBody: bodyPreview,
				Issue:        fmt.Sprintf("Server returned HTTP %d %s (Unhandled internal error or crash)", resp.StatusCode, resp.Status),
				Severity:     "CRITICAL",
			})
		} else if resp.StatusCode >= 400 {
			result.Handled4xx++
		} else if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			result.Success2xx++
		}

		if opts.OnMutation != nil {
			opts.OnMutation(m, resp.StatusCode, reqElapsed, nil)
		}
	}

	result.ElapsedMs = float64(time.Since(startTime).Milliseconds())
	return result, nil
}
