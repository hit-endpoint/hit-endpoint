package output

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hit-endpoint/hit-endpoint/internal/types"
)

type JUnitTestSuites struct {
	XMLName  xml.Name         `xml:"testsuites"`
	Name     string           `xml:"name,attr"`
	Tests    int              `xml:"tests,attr"`
	Failures int              `xml:"failures,attr"`
	Errors   int              `xml:"errors,attr"`
	Time     string           `xml:"time,attr"`
	Suites   []JUnitTestSuite `xml:"testsuite"`
}

type JUnitTestSuite struct {
	XMLName   xml.Name        `xml:"testsuite"`
	Name      string          `xml:"name,attr"`
	Tests     int             `xml:"tests,attr"`
	Failures  int             `xml:"failures,attr"`
	Errors    int             `xml:"errors,attr"`
	Time      string          `xml:"time,attr"`
	Timestamp string          `xml:"timestamp,attr"`
	Cases     []JUnitTestCase `xml:"testcase"`
}

type JUnitTestCase struct {
	XMLName   xml.Name      `xml:"testcase"`
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *JUnitFailure `xml:"failure,omitempty"`
	Error     *JUnitError   `xml:"error,omitempty"`
}

type JUnitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Content string `xml:",chardata"`
}

type JUnitError struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Content string `xml:",chardata"`
}

func flattenResults(results []*types.Result) []*types.Result {
	var out []*types.Result
	for _, r := range results {
		if len(r.Matrix) > 0 {
			for i, mr := range r.Matrix {
				if mr.Ref == "" {
					base := r.Ref
					if base == "" {
						base = r.Name
					}
					if base == "" {
						base = fmt.Sprintf("%s %s", r.Method, r.Url)
					}
					mr.Ref = fmt.Sprintf("%s [row %d]", base, i+1)
				}
				out = append(out, mr)
			}
		} else {
			out = append(out, r)
		}
	}
	return out
}

func BuildJUnitSuites(results []*types.Result) *JUnitTestSuites {
	root := &JUnitTestSuites{
		Name: "hit",
	}

	var totalTime float64
	timestamp := time.Now().UTC().Format(time.RFC3339)
	flatResults := flattenResults(results)

	for _, r := range flatResults {
		ref := r.Ref
		if ref == "" {
			ref = r.Name
		}
		if ref == "" {
			ref = fmt.Sprintf("%s %s", r.Method, r.Url)
		}

		elapsedSec := r.ElapsedMs / 1000.0
		totalTime += elapsedSec

		suite := JUnitTestSuite{
			Name:      ref,
			Timestamp: timestamp,
			Time:      fmt.Sprintf("%.3f", elapsedSec),
		}

		if r.Error != "" {
			suite.Errors++
			suite.Tests++
			suite.Cases = append(suite.Cases, JUnitTestCase{
				Name:      "Request execution",
				ClassName: ref,
				Time:      fmt.Sprintf("%.3f", elapsedSec),
				Error: &JUnitError{
					Message: r.Error,
					Type:    "TransportError",
					Content: fmt.Sprintf("HTTP request failed: %s", r.Error),
				},
			})
		} else if len(r.Tests) == 0 {
			suite.Tests++
			tc := JUnitTestCase{
				Name:      "HTTP status < 400",
				ClassName: ref,
				Time:      fmt.Sprintf("%.3f", elapsedSec),
			}
			if !r.OK() {
				suite.Failures++
				tc.Failure = &JUnitFailure{
					Message: fmt.Sprintf("HTTP status %d %s", r.Status, r.Reason),
					Type:    "StatusFailure",
					Content: fmt.Sprintf("Expected status < 400, received %d %s", r.Status, r.Reason),
				}
			}
			suite.Cases = append(suite.Cases, tc)
		} else {
			for i, t := range r.Tests {
				suite.Tests++
				cTime := "0.000"
				if i == 0 {
					cTime = fmt.Sprintf("%.3f", elapsedSec)
				}
				tc := JUnitTestCase{
					Name:      t.Name,
					ClassName: ref,
					Time:      cTime,
				}
				if !t.Passed {
					suite.Failures++
					detail := t.Detail
					if detail == "" {
						detail = fmt.Sprintf("assertion '%s' failed", t.Name)
					}
					tc.Failure = &JUnitFailure{
						Message: detail,
						Type:    "AssertionFailure",
						Content: fmt.Sprintf("Test assertion '%s' failed: %s", t.Name, detail),
					}
				}
				suite.Cases = append(suite.Cases, tc)
			}
		}

		for k, errStr := range r.CaptureErrors {
			suite.Tests++
			suite.Errors++
			suite.Cases = append(suite.Cases, JUnitTestCase{
				Name:      fmt.Sprintf("capture %s", k),
				ClassName: ref,
				Time:      "0.000",
				Error: &JUnitError{
					Message: errStr,
					Type:    "CaptureError",
					Content: fmt.Sprintf("Failed to capture '%s': %s", k, errStr),
				},
			})
		}

		root.Tests += suite.Tests
		root.Failures += suite.Failures
		root.Errors += suite.Errors
		root.Suites = append(root.Suites, suite)
	}

	root.Time = fmt.Sprintf("%.3f", totalTime)
	return root
}

func WriteJUnitXML(results []*types.Result, destPath string) error {
	suites := BuildJUnitSuites(results)
	data, err := xml.MarshalIndent(suites, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to generate junit xml: %w", err)
	}

	content := []byte(xml.Header + string(data) + "\n")

	if dir := filepath.Dir(destPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory for junit xml: %w", err)
		}
	}

	if err := os.WriteFile(destPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write junit xml to %s: %w", destPath, err)
	}
	return nil
}

func BuildPerfJUnitSuites(report *types.PerfReport, thresholdMet bool, thresholdErr string) *JUnitTestSuites {
	root := &JUnitTestSuites{
		Name: "hit-perf",
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	durationSec := report.DurationS
	suiteName := report.Name
	if suiteName == "" {
		suiteName = fmt.Sprintf("hit perf %s %s", report.Method, report.Url)
	}

	suite := JUnitTestSuite{
		Name:      suiteName,
		Timestamp: timestamp,
		Time:      fmt.Sprintf("%.3f", durationSec),
	}

	// Case 1: Throughput & completion
	suite.Tests++
	tcCompletion := JUnitTestCase{
		Name:      "Throughput & Completion",
		ClassName: suiteName,
		Time:      fmt.Sprintf("%.3f", durationSec),
	}
	if report.Completed == 0 {
		suite.Failures++
		tcCompletion.Failure = &JUnitFailure{
			Message: "No requests completed",
			Type:    "ZeroThroughputFailure",
			Content: "Performance benchmark ran but 0 requests completed successfully",
		}
	}
	suite.Cases = append(suite.Cases, tcCompletion)

	// Case 2: Zero Errors
	suite.Tests++
	tcErrors := JUnitTestCase{
		Name:      "Zero Errors",
		ClassName: suiteName,
		Time:      "0.000",
	}
	if report.Failed > 0 || len(report.Errors) > 0 {
		suite.Failures++
		var errDetails []string
		for errStr, count := range report.Errors {
			errDetails = append(errDetails, fmt.Sprintf("%s (%d)", errStr, count))
		}
		for code, count := range report.StatusCounts {
			if code >= 400 {
				errDetails = append(errDetails, fmt.Sprintf("HTTP %d (%d)", code, count))
			}
		}
		failMsg := fmt.Sprintf("%d of %d requests failed", report.Failed, report.Completed)
		if len(errDetails) > 0 {
			failMsg = fmt.Sprintf("%s: %s", failMsg, strings.Join(errDetails, ", "))
		}
		tcErrors.Failure = &JUnitFailure{
			Message: failMsg,
			Type:    "ErrorRateFailure",
			Content: failMsg,
		}
	}
	suite.Cases = append(suite.Cases, tcErrors)

	// Case 3: SLA Threshold Gate
	if thresholdErr != "" || !thresholdMet {
		suite.Tests++
		tcThreshold := JUnitTestCase{
			Name:      "SLA Threshold Gate",
			ClassName: suiteName,
			Time:      "0.000",
		}
		if !thresholdMet {
			suite.Failures++
			msg := thresholdErr
			if msg == "" {
				msg = "SLA threshold condition not satisfied"
			}
			tcThreshold.Failure = &JUnitFailure{
				Message: msg,
				Type:    "SLABreachFailure",
				Content: fmt.Sprintf("SLA threshold gate failed: %s", msg),
			}
		}
		suite.Cases = append(suite.Cases, tcThreshold)
	}

	root.Tests = suite.Tests
	root.Failures = suite.Failures
	root.Errors = suite.Errors
	root.Time = fmt.Sprintf("%.3f", durationSec)
	root.Suites = append(root.Suites, suite)
	return root
}

func WritePerfJUnitXML(report *types.PerfReport, thresholdMet bool, thresholdErr string, destPath string) error {
	suites := BuildPerfJUnitSuites(report, thresholdMet, thresholdErr)
	data, err := xml.MarshalIndent(suites, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to generate perf junit xml: %w", err)
	}

	content := []byte(xml.Header + string(data) + "\n")

	if dir := filepath.Dir(destPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory for perf junit xml: %w", err)
		}
	}

	if err := os.WriteFile(destPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write perf junit xml to %s: %w", destPath, err)
	}
	return nil
}
