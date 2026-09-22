package output

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/hit-endpoint/hit-endpoint/internal/types"
)

type Printer struct {
	Color  bool
	Stream io.Writer
}

func NewPrinter(color *bool, stream io.Writer) *Printer {
	if stream == nil {
		stream = os.Stdout
	}
	var useColor bool
	if color != nil {
		useColor = *color
	} else {
		useColor = isTerminal(stream) && os.Getenv("NO_COLOR") == ""
	}
	return &Printer{
		Color:  useColor,
		Stream: stream,
	}
}

func isTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		stat, err := f.Stat()
		if err == nil {
			return (stat.Mode() & os.ModeCharDevice) != 0
		}
	}
	return false
}

func (p *Printer) c(code, text string) string {
	if p.Color {
		return fmt.Sprintf("\033[%sm%s\033[0m", code, text)
	}
	return text
}

func (p *Printer) Green(t string) string  { return p.c("32", t) }
func (p *Printer) Red(t string) string    { return p.c("31", t) }
func (p *Printer) Yellow(t string) string { return p.c("33", t) }
func (p *Printer) Dim(t string) string    { return p.c("2", t) }
func (p *Printer) Bold(t string) string   { return p.c("1", t) }
func (p *Printer) Cyan(t string) string   { return p.c("36", t) }

func (p *Printer) Out(text string) {
	fmt.Fprintln(p.Stream, text)
}

func (p *Printer) StatusText(r *types.Result) string {
	if r.Error != "" {
		return p.Red("ERROR")
	}
	s := strings.TrimSpace(fmt.Sprintf("%d %s", r.Status, r.Reason))
	if !r.HasStatus {
		return p.Red("no response")
	}
	if r.Status < 300 {
		return p.Green(s)
	}
	if r.Status < 400 {
		return p.Yellow(s)
	}
	return p.Red(s)
}

func (p *Printer) Result(r *types.Result, verbose bool, maxBody int, showHeaders bool, quiet bool, mask func(string) string) {
	if mask == nil {
		mask = func(s string) string { return s }
	}

	head := fmt.Sprintf("%s  %s %s  →  %s", p.Bold(r.Ref), p.Cyan(r.Method), r.Url, p.StatusText(r))
	if r.Error == "" {
		head += p.Dim(fmt.Sprintf("  %.0f ms  %s", r.ElapsedMs, HumanSize(r.Size)))
	}
	p.Out(head)

	if verbose {
		var reqKeys []string
		for k := range r.RequestHeaders {
			reqKeys = append(reqKeys, k)
		}
		sort.Strings(reqKeys)
		for _, k := range reqKeys {
			p.Out(p.Dim(fmt.Sprintf("  > %s: %s", k, mask(r.RequestHeaders[k]))))
		}
		if r.RequestBody != "" {
			lines := strings.Split(r.RequestBody, "\n")
			limit := 60
			if len(lines) < limit {
				limit = len(lines)
			}
			for i := 0; i < limit; i++ {
				p.Out(p.Dim(fmt.Sprintf("  > %s", mask(lines[i]))))
			}
		}
	}

	if r.Error != "" {
		p.Out(p.Red(fmt.Sprintf("  ✗ %s", r.Error)))
	}

	for _, note := range r.Notes {
		p.Out(p.Yellow(fmt.Sprintf("  ! %s", note)))
	}

	for _, t := range r.Tests {
		if t.Passed {
			p.Out(fmt.Sprintf("  %s %s", p.Green("✓"), t.Name))
		} else {
			detail := ""
			if t.Detail != "" {
				detail = p.Dim(fmt.Sprintf("   (%s)", t.Detail))
			}
			p.Out(fmt.Sprintf("  %s %s%s", p.Red("✗"), t.Name, detail))
		}
	}

	var capKeys []string
	for k := range r.Captures {
		capKeys = append(capKeys, k)
	}
	sort.Strings(capKeys)
	for _, k := range capKeys {
		v := r.Captures[k]
		shown := mask(fmt.Sprintf("%v", v))
		if _, isStr := v.(string); !isStr {
			if b, err := json.Marshal(v); err == nil {
				shown = string(b)
			}
		}
		p.Out(fmt.Sprintf("  %s %s = %s", p.Cyan("↳"), k, shown))
	}

	var capErrKeys []string
	for k := range r.CaptureErrors {
		capErrKeys = append(capErrKeys, k)
	}
	sort.Strings(capErrKeys)
	for _, k := range capErrKeys {
		p.Out(fmt.Sprintf("  %s capture %s: %s", p.Yellow("!"), k, r.CaptureErrors[k]))
	}

	if quiet || r.Error != "" {
		return
	}

	if showHeaders || verbose {
		var respKeys []string
		for k := range r.Headers {
			respKeys = append(respKeys, k)
		}
		sort.Strings(respKeys)
		for _, k := range respKeys {
			p.Out(p.Dim(fmt.Sprintf("  < %s: %s", k, r.Headers[k])))
		}
	}

	if len(r.Matrix) > 0 {
		p.Out(p.Bold(fmt.Sprintf("  Matrix Scorecard (%d variations):", len(r.Matrix))))
		for i, mr := range r.Matrix {
			statusSymbol := p.Green("✓ PASS")
			if !mr.OK() {
				statusSymbol = p.Red("✗ FAIL")
			}
			p.Out(fmt.Sprintf("    [%d] %-30s %s (HTTP %d, %.0fms)",
				i+1,
				mr.Name,
				statusSymbol,
				mr.Status,
				mr.ElapsedMs,
			))
			for _, t := range mr.Tests {
				if !t.Passed {
					p.Out(fmt.Sprintf("        %s %s: %s", p.Red("✗"), t.Name, t.Detail))
				}
			}
		}
	}

	body := FormatBody(r)
	if body != "" {
		if maxBody > 0 && len(body) > maxBody {
			body = body[:maxBody] + p.Dim(fmt.Sprintf("\n… (%d more chars, use --full)", len(body)-maxBody))
		}
		p.Out(body)
	}
}

func (p *Printer) Summary(results []*types.Result) {
	total := len(results)
	var failed []*types.Result
	testsCount := 0
	tfailCount := 0

	for _, r := range results {
		if !r.OK() {
			failed = append(failed, r)
		}
		testsCount += len(r.Tests)
		for _, mr := range r.Matrix {
			testsCount += len(mr.Tests)
		}
		tfailCount += len(r.FailedTests())
	}

	line := fmt.Sprintf("%d request(s), %d test(s)", total, testsCount)
	if len(failed) > 0 || tfailCount > 0 {
		line += fmt.Sprintf(", %s", p.Red(fmt.Sprintf("%d failed", len(failed))))
		if tfailCount > 0 {
			line += fmt.Sprintf(" (%d failing tests)", tfailCount)
		}
	} else {
		line += fmt.Sprintf(", %s", p.Green("all passed"))
	}
	p.Out(p.Dim(strings.Repeat("─", 40)))
	p.Out(line)
}

func (p *Printer) Perf(rep *types.PerfReport) {
	st := rep.Stats()
	p.Out(p.Bold(fmt.Sprintf("%s %s", rep.Method, rep.Url)))

	failStr := p.Dim(fmt.Sprintf("%d failed", rep.Failed))
	if rep.Failed > 0 {
		failStr = p.Red(fmt.Sprintf("%d failed", rep.Failed))
	}
	p.Out(fmt.Sprintf("  requests   %d  (%s, %s)", rep.Completed, p.Green(fmt.Sprintf("%d ok", rep.OK)), failStr))
	p.Out(fmt.Sprintf("  duration   %.2f s   concurrency %d   throughput %.1f req/s", rep.DurationS, rep.Concurrency, rep.RPS()))
	p.Out(fmt.Sprintf("  received   %s", HumanSize(rep.BytesReceived)))

	var latKeys = []string{"min", "mean", "p50", "p90", "p95", "p99", "max"}
	var latParts []string
	for _, k := range latKeys {
		latParts = append(latParts, fmt.Sprintf("%s %.1f", k, st[k]))
	}
	p.Out("  latency    " + strings.Join(latParts, "  ") + "  (ms)")

	if len(rep.StatusCounts) > 0 {
		var statusCodes []int
		for k := range rep.StatusCounts {
			statusCodes = append(statusCodes, k)
		}
		sort.Ints(statusCodes)
		var scParts []string
		for _, k := range statusCodes {
			scParts = append(scParts, fmt.Sprintf("%d: %d", k, rep.StatusCounts[k]))
		}
		p.Out("  status     " + strings.Join(scParts, "  "))
	}

	if len(rep.Errors) > 0 {
		var errKeys []string
		for k := range rep.Errors {
			errKeys = append(errKeys, k)
		}
		sort.Slice(errKeys, func(i, j int) bool {
			return rep.Errors[errKeys[i]] > rep.Errors[errKeys[j]]
		})
		var errParts []string
		for i := 0; i < len(errKeys) && i < 5; i++ {
			errParts = append(errParts, fmt.Sprintf("%s: %d", errKeys[i], rep.Errors[errKeys[i]]))
		}
		p.Out("  errors     " + strings.Join(errParts, "  "))
	}

	if len(rep.TestFailures) > 0 {
		var tfKeys []string
		for k := range rep.TestFailures {
			tfKeys = append(tfKeys, k)
		}
		sort.Slice(tfKeys, func(i, j int) bool {
			return rep.TestFailures[tfKeys[i]] > rep.TestFailures[tfKeys[j]]
		})
		var tfParts []string
		for i := 0; i < len(tfKeys) && i < 5; i++ {
			tfParts = append(tfParts, fmt.Sprintf("%s ×%d", tfKeys[i], rep.TestFailures[tfKeys[i]]))
		}
		p.Out("  failing    " + strings.Join(tfParts, "; "))
	}

	p.Out("  histogram")
	for _, line := range Histogram(rep.LatenciesMs, 8, 30) {
		p.Out("    " + line)
	}
}

func FormatBody(r *types.Result) string {
	if r.JSON != nil {
		b, err := json.MarshalIndent(r.JSON, "", "  ")
		if err == nil {
			return string(b)
		}
	}
	return r.Text
}

func HumanSize(n int64) string {
	f := float64(n)
	units := []string{"B", "KB", "MB", "GB"}
	for _, unit := range units {
		if f < 1024 || unit == "GB" {
			if unit == "B" {
				return fmt.Sprintf("%.0f %s", f, unit)
			}
			return fmt.Sprintf("%.1f %s", f, unit)
		}
		f /= 1024
	}
	return fmt.Sprintf("%d B", n)
}

func Histogram(values []float64, bins int, width int) []string {
	if len(values) == 0 {
		return nil
	}
	if bins <= 0 {
		bins = 8
	}
	if width <= 0 {
		width = 30
	}

	lo := values[0]
	hi := values[0]
	for _, v := range values {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}

	if hi-lo < 1e-9 {
		bar := strings.Repeat("█", width)
		return []string{fmt.Sprintf("%8.1f ms  %s %d", lo, bar, len(values))}
	}

	step := (hi - lo) / float64(bins)
	counts := make([]int, bins)
	for _, v := range values {
		idx := int((v - lo) / step)
		if idx >= bins {
			idx = bins - 1
		}
		if idx < 0 {
			idx = 0
		}
		counts[idx]++
	}

	peak := 0
	for _, c := range counts {
		if c > peak {
			peak = c
		}
	}

	var lines []string
	for i, c := range counts {
		barLen := 0
		if peak > 0 {
			barLen = int(math.Round(float64(width*c) / float64(peak)))
		}
		bar := strings.Repeat("█", barLen)
		valAt := lo + float64(i)*step
		lines = append(lines, fmt.Sprintf("%8.1f ms  %-*s %d", valAt, width, bar, c))
	}
	return lines
}

// EmitGitHubAnnotations outputs GitHub Actions workflow commands (::error ...::) for failing tests and transport errors.
func EmitGitHubAnnotations(results []*types.Result, stream io.Writer) {
	if stream == nil {
		stream = os.Stdout
	}
	for _, r := range results {
		file := r.Ref
		if file == "" {
			file = r.Name
		}
		if r.Error != "" {
			fmt.Fprintf(stream, "::error title=Request Failed (%s)::HTTP request failed: %s\n", file, r.Error)
		} else if !r.OK() && len(r.Tests) == 0 {
			fmt.Fprintf(stream, "::error title=Status Error (%s)::Expected HTTP status < 400, received %d %s\n", file, r.Status, r.Reason)
		}
		for _, t := range r.FailedTests() {
			detail := t.Detail
			if detail == "" {
				detail = fmt.Sprintf("assertion '%s' failed", t.Name)
			}
			fmt.Fprintf(stream, "::error title=Test Failed (%s)::%s - %s\n", file, t.Name, detail)
		}
		for k, errStr := range r.CaptureErrors {
			fmt.Fprintf(stream, "::warning title=Capture Failed (%s)::Failed to capture '%s': %s\n", file, k, errStr)
		}
	}
}

// GenerateGitHubStepSummary formats a GitHub Flavored Markdown summary table.
func GenerateGitHubStepSummary(results []*types.Result) string {
	var sb strings.Builder
	sb.WriteString("### 🚀 hit API Test Results\n\n")

	sb.WriteString("| Request | Method | Status | Duration | Tests | Result |\n")
	sb.WriteString("| :--- | :--- | :--- | :--- | :--- | :--- |\n")

	totalRequests := len(results)
	totalTests := 0
	failedTests := 0
	passedRequests := 0

	for _, r := range results {
		ref := r.Ref
		if ref == "" {
			ref = r.Name
		}
		statusStr := fmt.Sprintf("%d %s", r.Status, r.Reason)
		if !r.HasStatus {
			statusStr = "Error"
		}

		passedTestsInReq := 0
		for _, t := range r.Tests {
			totalTests++
			if t.Passed {
				passedTestsInReq++
			} else {
				failedTests++
			}
		}

		testSummary := "-"
		if len(r.Tests) > 0 {
			testSummary = fmt.Sprintf("%d/%d", passedTestsInReq, len(r.Tests))
		}

		resultBadge := "✅ Pass"
		if !r.OK() {
			resultBadge = "❌ Fail"
		} else {
			passedRequests++
		}

		sb.WriteString(fmt.Sprintf("| `%s` | `%s` | %s | %.0fms | %s | %s |\n",
			ref, r.Method, statusStr, r.ElapsedMs, testSummary, resultBadge))
	}

	sb.WriteString("\n")
	if failedTests > 0 || passedRequests < totalRequests {
		sb.WriteString(fmt.Sprintf("**Status:** ❌ **%d of %d requests passed** (%d failing assertions)\n", passedRequests, totalRequests, failedTests))
	} else {
		sb.WriteString(fmt.Sprintf("**Status:** ✅ **All %d requests and %d assertions passed**\n", totalRequests, totalTests))
	}

	return sb.String()
}

// AppendGitHubStepSummary writes the markdown summary to the file pointed by $GITHUB_STEP_SUMMARY.
func AppendGitHubStepSummary(results []*types.Result) error {
	summaryPath := os.Getenv("GITHUB_STEP_SUMMARY")
	if summaryPath == "" {
		return nil
	}
	content := GenerateGitHubStepSummary(results)
	f, err := os.OpenFile(summaryPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content + "\n")
	return err
}

