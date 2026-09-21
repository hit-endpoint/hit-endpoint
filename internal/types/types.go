package types

import (
	"math"
	"sort"
	"strconv"

	"hit/internal/assertions"
)

type Result struct {
	Name           string                  `json:"name"`
	Ref            string                  `json:"ref"`
	Method         string                  `json:"method"`
	Url            string                  `json:"url"`
	RequestHeaders map[string]string       `json:"request_headers,omitempty"`
	RequestBody    string                  `json:"request_body,omitempty"`
	Status         int                     `json:"status"`
	HasStatus      bool                    `json:"has_status"`
	Reason         string                  `json:"reason"`
	Headers        map[string]string       `json:"headers"`
	Text           string                  `json:"text"`
	JSON           any                     `json:"json"`
	ElapsedMs      float64                 `json:"elapsed_ms"`
	Size           int64                   `json:"size"`
	Tests          []assertions.TestResult `json:"tests"`
	Captures       map[string]any          `json:"captures"`
	CaptureErrors  map[string]string       `json:"capture_errors"`
	Notes          []string                `json:"notes"`
	Error          string                  `json:"error,omitempty"`
	Matrix         []*Result               `json:"matrix,omitempty"`
}

func (r *Result) OK() bool {
	if r.Error != "" {
		return false
	}
	if len(r.Matrix) > 0 {
		for _, child := range r.Matrix {
			if !child.OK() {
				return false
			}
		}
		return true
	}
	if len(r.Tests) > 0 {
		for _, t := range r.Tests {
			if !t.Passed {
				return false
			}
		}
		return true
	}
	return r.HasStatus && r.Status < 400
}

func (r *Result) FailedTests() []assertions.TestResult {
	var out []assertions.TestResult
	if len(r.Matrix) > 0 {
		for _, child := range r.Matrix {
			out = append(out, child.FailedTests()...)
		}
		return out
	}
	for _, t := range r.Tests {
		if !t.Passed {
			out = append(out, t)
		}
	}
	return out
}

func (r *Result) Root() map[string]any {
	var statusVal any
	if r.HasStatus {
		statusVal = r.Status
	}
	headers := make(map[string]any, len(r.Headers))
	for k, v := range r.Headers {
		headers[k] = v
	}
	return map[string]any{
		"status":  statusVal,
		"headers": headers,
		"json":    r.JSON,
		"text":    r.Text,
		"ms":      r.ElapsedMs,
		"ok":      r.HasStatus && r.Status < 400,
	}
}

func (r *Result) ToDict() map[string]any {
	var statusVal any
	if r.HasStatus {
		statusVal = r.Status
	}
	var errVal any
	if r.Error != "" {
		errVal = r.Error
	}
	var textVal any
	if r.JSON == nil {
		textVal = r.Text
	}

	testsList := make([]map[string]any, len(r.Tests))
	for i, t := range r.Tests {
		testsList[i] = map[string]any{
			"name":   t.Name,
			"passed": t.Passed,
			"detail": t.Detail,
		}
	}

	reqHeaders := make(map[string]any, len(r.RequestHeaders))
	for k, v := range r.RequestHeaders {
		reqHeaders[k] = v
	}

	resHeaders := make(map[string]any, len(r.Headers))
	for k, v := range r.Headers {
		resHeaders[k] = v
	}

	dict := map[string]any{
		"name": r.Name,
		"ref":  r.Ref,
		"ok":   r.OK(),
		"request": map[string]any{
			"method":  r.Method,
			"url":     r.Url,
			"headers": reqHeaders,
			"body":    r.RequestBody,
		},
		"status":         statusVal,
		"reason":         r.Reason,
		"headers":        resHeaders,
		"elapsed_ms":     math.Round(r.ElapsedMs*10) / 10,
		"size":           r.Size,
		"json":           r.JSON,
		"text":           textVal,
		"tests":          testsList,
		"captures":       r.Captures,
		"capture_errors": r.CaptureErrors,
		"notes":          r.Notes,
		"error":          errVal,
	}
	if len(r.Matrix) > 0 {
		var mList []map[string]any
		for _, child := range r.Matrix {
			mList = append(mList, child.ToDict())
		}
		dict["matrix"] = mList
	}
	return dict
}

type PerfReport struct {
	Name          string          `json:"name"`
	Method        string          `json:"method"`
	Url           string          `json:"url"`
	Concurrency   int             `json:"concurrency"`
	Completed     int             `json:"completed"`
	OK            int             `json:"ok"`
	Failed        int             `json:"failed"`
	DurationS     float64         `json:"duration_s"`
	LatenciesMs   []float64       `json:"latencies_ms"`
	StatusCounts  map[int]int     `json:"status_counts"`
	Errors        map[string]int  `json:"errors"`
	TestFailures  map[string]int  `json:"test_failures"`
	BytesReceived int64           `json:"bytes_received"`
}

func (p *PerfReport) RPS() float64 {
	if p.DurationS > 0 {
		return float64(p.Completed) / p.DurationS
	}
	return 0.0
}

func (p *PerfReport) Percentile(q float64) float64 {
	if len(p.LatenciesMs) == 0 {
		return 0.0
	}
	sorted := make([]float64, len(p.LatenciesMs))
	copy(sorted, p.LatenciesMs)
	sort.Float64s(sorted)

	idx := int(math.Round(q * float64(len(sorted)-1)))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func (p *PerfReport) Stats() map[string]float64 {
	if len(p.LatenciesMs) == 0 {
		return map[string]float64{
			"min": 0, "mean": 0, "p50": 0, "p90": 0, "p95": 0, "p99": 0, "max": 0, "stdev": 0,
		}
	}
	min := p.LatenciesMs[0]
	max := p.LatenciesMs[0]
	sum := 0.0
	for _, v := range p.LatenciesMs {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
		sum += v
	}
	mean := sum / float64(len(p.LatenciesMs))

	varianceSum := 0.0
	for _, v := range p.LatenciesMs {
		diff := v - mean
		varianceSum += diff * diff
	}
	stdev := math.Sqrt(varianceSum / float64(len(p.LatenciesMs)))

	return map[string]float64{
		"min":   min,
		"mean":  mean,
		"p50":   p.Percentile(0.50),
		"p90":   p.Percentile(0.90),
		"p95":   p.Percentile(0.95),
		"p99":   p.Percentile(0.99),
		"max":   max,
		"stdev": stdev,
	}
}

func (p *PerfReport) ToDict() map[string]any {
	stats := p.Stats()
	roundedStats := make(map[string]any, len(stats))
	for k, v := range stats {
		roundedStats[k] = math.Round(v*100) / 100
	}

	sc := make(map[string]int, len(p.StatusCounts))
	for k, v := range p.StatusCounts {
		sc[strconv.Itoa(k)] = v
	}

	return map[string]any{
		"name":           p.Name,
		"method":         p.Method,
		"url":            p.Url,
		"concurrency":    p.Concurrency,
		"completed":      p.Completed,
		"ok":             p.OK,
		"failed":         p.Failed,
		"duration_s":     math.Round(p.DurationS*1000) / 1000,
		"rps":            math.Round(p.RPS()*100) / 100,
		"latency_ms":     roundedStats,
		"status_counts":  p.StatusCounts,
		"errors":         p.Errors,
		"test_failures":  p.TestFailures,
		"bytes_received": p.BytesReceived,
	}
}
