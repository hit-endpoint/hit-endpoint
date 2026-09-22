package schedule

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hit-endpoint/hit-endpoint/internal/history"
	"github.com/hit-endpoint/hit-endpoint/internal/runner"
	"github.com/hit-endpoint/hit-endpoint/internal/spec"
	"github.com/hit-endpoint/hit-endpoint/internal/types"
	"github.com/hit-endpoint/hit-endpoint/internal/zone"
)

// ScheduleOptions configures a scheduled request execution session.
type ScheduleOptions struct {
	Target        string // URL or request REF
	Method        string
	Headers       map[string]string
	Body          string
	Server        string // Server/Environment name
	StartTime     time.Time
	Interval      time.Duration
	Count         int // Stop after N runs (0 = infinite if interval set, or 1 if no interval)
	Duration      time.Duration
	ExpectStatus  int
	ExpectPattern string
	Insecure      bool
	ZoneRoot      string
	OnTick        func(tick ScheduleTick)
}

// ScheduleTick represents the outcome of a single scheduled execution.
type ScheduleTick struct {
	Index           int           `json:"index"`
	Timestamp       time.Time     `json:"timestamp"`
	StatusCode      int           `json:"status_code"`
	ElapsedMs       float64       `json:"elapsed_ms"`
	Passed          bool          `json:"passed"`
	StatusMatch     bool          `json:"status_match"`
	PatternMatch    bool          `json:"pattern_match"`
	SpecTestsPassed bool          `json:"spec_tests_passed"`
	Detail          string        `json:"detail,omitempty"`
	Result          *types.Result `json:"result,omitempty"`
}

// ScheduleSummary holds the aggregate outcome across all scheduled ticks.
type ScheduleSummary struct {
	Target     string         `json:"target"`
	TotalRuns  int            `json:"total_runs"`
	PassedRuns int            `json:"passed_runs"`
	FailedRuns int            `json:"failed_runs"`
	StartTime  time.Time      `json:"start_time"`
	EndTime    time.Time      `json:"end_time"`
	Ticks      []ScheduleTick `json:"ticks"`
}

// ParseStartTime parses an absolute time, relative time (+5m), 24h clock (15:04), or "now".
func ParseStartTime(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "now") {
		return now, nil
	}

	// Relative duration, e.g. "+10s", "+5m", "+1h"
	if strings.HasPrefix(s, "+") {
		d, err := time.ParseDuration(s[1:])
		if err != nil {
			// Try parsing as number of seconds
			sec, sErr := strconv.ParseFloat(s[1:], 64)
			if sErr != nil {
				return time.Time{}, fmt.Errorf("invalid relative start time: %w", err)
			}
			d = time.Duration(sec * float64(time.Second))
		}
		return now.Add(d), nil
	}

	// 24-hour clock formats: "15:04" or "15:04:05"
	for _, layout := range []string{"15:04:05", "15:04"} {
		if t, err := time.ParseInLocation(layout, s, now.Location()); err == nil {
			cand := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), t.Second(), 0, now.Location())
			if cand.Before(now) {
				// Time has already passed today; schedule for tomorrow
				cand = cand.AddDate(0, 0, 1)
			}
			return cand, nil
		}
	}

	// Full timestamp formats
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02T15:04:05",
		time.RFC3339,
	} {
		if t, err := time.ParseInLocation(layout, s, now.Location()); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unrecognized start time format: %q (expected 'now', '+10s', '15:04', or 'YYYY-MM-DD HH:MM')", s)
}

// Run executes the scheduled test loop until completion, count limit, duration expiry, or context cancel.
func Run(ctx context.Context, opts ScheduleOptions) (*ScheduleSummary, error) {
	now := time.Now()
	startTime := opts.StartTime
	if startTime.IsZero() || startTime.Before(now) {
		startTime = now
	}

	summary := &ScheduleSummary{
		Target:    opts.Target,
		StartTime: startTime,
		Ticks:     make([]ScheduleTick, 0),
	}

	// Wait until scheduled start time
	if waitDuration := time.Until(startTime); waitDuration > 0 {
		select {
		case <-ctx.Done():
			return summary, ctx.Err()
		case <-time.After(waitDuration):
		}
	}

	var patternRegex *regexp.Regexp
	if opts.ExpectPattern != "" {
		if r, err := regexp.Compile(opts.ExpectPattern); err == nil {
			patternRegex = r
		}
	}

	// Prepare zone session if target is not an external URL or if zone is found
	var z *zone.Zone
	if opts.ZoneRoot != "" {
		z, _ = zone.Find(opts.ZoneRoot)
	} else {
		z, _ = zone.FindOrNone("")
	}

	isURL := strings.Contains(opts.Target, "://") || strings.HasPrefix(opts.Target, "localhost:") || strings.HasPrefix(opts.Target, "127.0.0.1:")
	isSpecRef := z != nil && !isURL

	var sess *runner.Session
	if isSpecRef {
		var sErr error
		sess, sErr = runner.NewSession(runner.SessionOptions{
			Zone:       z,
			ServerName: opts.Server,
			Verify:     !opts.Insecure,
		})
		if sErr != nil {
			return summary, fmt.Errorf("failed to create session: %w", sErr)
		}
		defer sess.Close()
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: opts.Insecure,
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	sessionStart := time.Now()
	runIndex := 0

	for {
		select {
		case <-ctx.Done():
			summary.EndTime = time.Now()
			return summary, ctx.Err()
		default:
		}

		runIndex++
		tick := ScheduleTick{
			Index:           runIndex,
			Timestamp:       time.Now(),
			Passed:          true,
			StatusMatch:     true,
			PatternMatch:    true,
			SpecTestsPassed: true,
		}

		if isSpecRef {
			// Execute through runner (evaluates assertions, state, and records to history)
			res := sess.Run(opts.Target, nil)
			tick.Result = res
			tick.StatusCode = res.Status
			tick.ElapsedMs = res.ElapsedMs

			if !res.OK() {
				tick.SpecTestsPassed = false
				tick.Passed = false
				var fails []string
				for _, t := range res.FailedTests() {
					fails = append(fails, t.Name)
				}
				tick.Detail = fmt.Sprintf("tests failed: %s", strings.Join(fails, ", "))
			}

			if opts.ExpectStatus > 0 && res.Status != opts.ExpectStatus {
				tick.StatusMatch = false
				tick.Passed = false
				tick.Detail = fmt.Sprintf("expected status %d, got %d", opts.ExpectStatus, res.Status)
			}

			if opts.ExpectPattern != "" {
				bodyStr := res.Text
				matched := false
				if patternRegex != nil && patternRegex.MatchString(bodyStr) {
					matched = true
				} else if strings.Contains(bodyStr, opts.ExpectPattern) {
					matched = true
				}
				if !matched {
					tick.PatternMatch = false
					tick.Passed = false
					if tick.Detail == "" {
						tick.Detail = fmt.Sprintf("pattern %q not found in body", opts.ExpectPattern)
					}
				}
			}
		} else {
			// Direct HTTP execution
			method := opts.Method
			if method == "" {
				method = "GET"
			}
			targetURL := opts.Target
			if !strings.Contains(targetURL, "://") {
				targetURL = "http://" + targetURL
			}

			var bodyReader io.Reader
			if opts.Body != "" {
				bodyReader = bytes.NewReader([]byte(opts.Body))
			}

			req, err := http.NewRequestWithContext(ctx, method, targetURL, bodyReader)
			if err != nil {
				tick.Passed = false
				tick.Detail = err.Error()
			} else {
				for k, v := range opts.Headers {
					req.Header.Set(k, v)
				}
				startReq := time.Now()
				resp, err := client.Do(req)
				tick.ElapsedMs = float64(time.Since(startReq).Microseconds()) / 1000.0

				if err != nil {
					tick.Passed = false
					tick.Detail = err.Error()
				} else {
					tick.StatusCode = resp.StatusCode
					respBytes, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					bodyStr := string(respBytes)

					if opts.ExpectStatus > 0 && resp.StatusCode != opts.ExpectStatus {
						tick.StatusMatch = false
						tick.Passed = false
						tick.Detail = fmt.Sprintf("expected status %d, got %d", opts.ExpectStatus, resp.StatusCode)
					}

					if opts.ExpectPattern != "" {
						matched := false
						if patternRegex != nil && patternRegex.MatchString(bodyStr) {
							matched = true
						} else if strings.Contains(bodyStr, opts.ExpectPattern) {
							matched = true
						}
						if !matched {
							tick.PatternMatch = false
							tick.Passed = false
							if tick.Detail == "" {
								tick.Detail = fmt.Sprintf("pattern %q not found in body", opts.ExpectPattern)
							}
						}
					}

					// Record to history if zone exists
					if z != nil {
						store := history.NewStore(history.GetHistoryPath(z.Root))
						_ = store.Append(&history.Entry{
							ID:           fmt.Sprintf("hit_sched_%d", time.Now().UnixNano()),
							Timestamp:    time.Now().UTC().Format(time.RFC3339),
							Source:       "schedule",
							Ref:          opts.Target,
							Method:       method,
							Url:          targetURL,
							Status:       resp.StatusCode,
							ElapsedMs:    tick.ElapsedMs,
							ResponseBody: bodyStr,
						})
					}
				}
			}
		}

		if tick.Passed {
			summary.PassedRuns++
		} else {
			summary.FailedRuns++
		}
		summary.TotalRuns++
		summary.Ticks = append(summary.Ticks, tick)

		if opts.OnTick != nil {
			opts.OnTick(tick)
		}

		// Exit conditions:
		// 1. Max run count reached
		if opts.Count > 0 && runIndex >= opts.Count {
			break
		}
		// 2. Max total duration reached
		if opts.Duration > 0 && time.Since(sessionStart) >= opts.Duration {
			break
		}
		// 3. No interval set: single execution
		if opts.Interval <= 0 {
			break
		}

		// Sleep for Interval
		select {
		case <-ctx.Done():
			summary.EndTime = time.Now()
			return summary, ctx.Err()
		case <-time.After(opts.Interval):
		}
	}

	summary.EndTime = time.Now()
	return summary, nil
}

func init() {
	// Reference types to satisfy compiler
	_ = spec.RequestSpec{}
}
