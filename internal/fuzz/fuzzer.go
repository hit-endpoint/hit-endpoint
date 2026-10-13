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

// MutationResult records the outcome of an individual fuzz mutation.
type MutationResult struct {
	Name       string  `json:"name"`
	Category   string  `json:"category"`
	Method     string  `json:"method"`
	URL        string  `json:"url"`
	StatusCode int     `json:"status_code"`
	DurationMs float64 `json:"duration_ms"`
	Error      string  `json:"error,omitempty"`
}

// FuzzResult aggregates the execution metrics and detected vulnerabilities.
type FuzzResult struct {
	TargetMethod    string           `json:"target_method"`
	TargetURL       string           `json:"target_url"`
	TotalMutations  int              `json:"total_mutations"`
	Handled4xx      int              `json:"handled_4xx"`
	Success2xx      int              `json:"success_2xx"`
	ServerErrors5xx int              `json:"server_errors_5xx"`
	TransportErrors int              `json:"transport_errors"`
	Anomalies       []Anomaly        `json:"anomalies"`
	Mutations       []MutationResult `json:"mutations"`
	ElapsedMs       float64          `json:"elapsed_ms"`
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
		Anomalies:      []Anomaly{},
		Mutations:      []MutationResult{},
	}

	hasBaselineAuth := false
	for k := range headers {
		if strings.EqualFold(k, "Authorization") {
			hasBaselineAuth = true
			break
		}
	}
	upperMethod := strings.ToUpper(strings.TrimSpace(method))
	isWriteEndpoint := upperMethod == "POST" || upperMethod == "PUT" || upperMethod == "PATCH" || upperMethod == "DELETE"

	startTime := time.Now()

	for _, m := range mutations {
		var reqBody io.Reader
		if len(m.Body) > 0 {
			reqBody = bytes.NewReader(m.Body)
		}

		req, err := http.NewRequest(m.Method, m.URL, reqBody)
		if err != nil {
			result.TransportErrors++
			result.Mutations = append(result.Mutations, MutationResult{
				Name:     m.Name,
				Category: m.Category,
				Method:   m.Method,
				URL:      m.URL,
				Error:    err.Error(),
			})
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
			result.Mutations = append(result.Mutations, MutationResult{
				Name:       m.Name,
				Category:   m.Category,
				Method:     m.Method,
				URL:        m.URL,
				DurationMs: reqElapsed,
				Error:      err.Error(),
			})
			if opts.OnMutation != nil {
				opts.OnMutation(m, 0, reqElapsed, err)
			}
			continue
		}

		bodyBytes, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		fullBody := string(bodyBytes)
		bodyPreview := fullBody
		if len(bodyPreview) > 200 {
			bodyPreview = bodyPreview[:200] + "..."
		}

		result.Mutations = append(result.Mutations, MutationResult{
			Name:       m.Name,
			Category:   m.Category,
			Method:     m.Method,
			URL:        m.URL,
			StatusCode: resp.StatusCode,
			DurationMs: reqElapsed,
		})

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
			if strings.Contains(m.Name, "Corrupted Bearer token") && hasBaselineAuth {
				result.Anomalies = append(result.Anomalies, Anomaly{
					MutationName: m.Name,
					Category:     m.Category,
					StatusCode:   resp.StatusCode,
					DurationMs:   reqElapsed,
					ResponseBody: bodyPreview,
					Issue:        fmt.Sprintf("Authentication bypass: endpoint accepted corrupted Bearer token with HTTP %d", resp.StatusCode),
					Severity:     "HIGH",
				})
			} else if m.Category == "injection" {
				sev := "MEDIUM"
				issue := fmt.Sprintf("Endpoint accepted injection payload without validation rejection (HTTP %d)", resp.StatusCode)
				if strings.Contains(fullBody, "<script>alert(1)</script>") || strings.Contains(fullBody, "onload=alert(1)") {
					sev = "HIGH"
					issue = fmt.Sprintf("Reflected XSS / unescaped script payload in HTTP %d response", resp.StatusCode)
				} else if strings.Contains(fullBody, "root:x:") {
					sev = "HIGH"
					issue = fmt.Sprintf("Path traversal file disclosure in HTTP %d response", resp.StatusCode)
				}
				result.Anomalies = append(result.Anomalies, Anomaly{
					MutationName: m.Name,
					Category:     m.Category,
					StatusCode:   resp.StatusCode,
					DurationMs:   reqElapsed,
					ResponseBody: bodyPreview,
					Issue:        issue,
					Severity:     sev,
				})
			} else if m.Category == "types" || strings.Contains(m.Name, "10,000 'A's") || strings.Contains(m.Name, "Embedded null byte") || strings.Contains(m.Name, "Unicode control characters") || (isWriteEndpoint && (strings.Contains(m.Name, "Empty JSON object") || strings.Contains(m.Name, "Unexpected properties"))) {
				result.Anomalies = append(result.Anomalies, Anomaly{
					MutationName: m.Name,
					Category:     m.Category,
					StatusCode:   resp.StatusCode,
					DurationMs:   reqElapsed,
					ResponseBody: bodyPreview,
					Issue:        fmt.Sprintf("Missing input validation: endpoint accepted %s with HTTP %d", m.Name, resp.StatusCode),
					Severity:     "MEDIUM",
				})
			}
		}

		if opts.OnMutation != nil {
			opts.OnMutation(m, resp.StatusCode, reqElapsed, nil)
		}
	}

	result.ElapsedMs = float64(time.Since(startTime).Milliseconds())
	return result, nil
}

