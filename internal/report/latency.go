package report

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/hit-endpoint/hit-endpoint/internal/history"
)

type EndpointLatency struct {
	Target       string    `json:"target"`
	Method       string    `json:"method"`
	Ref          string    `json:"ref,omitempty"`
	Count        int       `json:"count"`
	MinMs        float64   `json:"min_ms"`
	MeanMs       float64   `json:"mean_ms"`
	P50Ms        float64   `json:"p50_ms"`
	P90Ms        float64   `json:"p90_ms"`
	P95Ms        float64   `json:"p95_ms"`
	P99Ms        float64   `json:"p99_ms"`
	MaxMs        float64   `json:"max_ms"`
	BaselineP95  float64   `json:"baseline_p95,omitempty"`
	RecentP95    float64   `json:"recent_p95,omitempty"`
	DeltaMs      float64   `json:"delta_ms,omitempty"`
	DeltaPercent float64   `json:"delta_percent,omitempty"`
	Regressed    bool      `json:"regressed"`
	Latencies    []float64 `json:"-"`
}

type LatencyTrendReport struct {
	TotalHits          int               `json:"total_hits"`
	EndpointsCount     int               `json:"endpoints_count"`
	RegressionAlerts   int               `json:"regression_alerts"`
	ThresholdPercent   float64           `json:"threshold_percent"`
	Endpoints          []EndpointLatency `json:"endpoints"`
}

func calculatePercentiles(vals []float64) (min, mean, p50, p90, p95, p99, max float64) {
	if len(vals) == 0 {
		return 0, 0, 0, 0, 0, 0, 0
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)

	min = sorted[0]
	max = sorted[len(sorted)-1]

	sum := 0.0
	for _, v := range sorted {
		sum += v
	}
	mean = sum / float64(len(sorted))

	p := func(pct float64) float64 {
		idx := int(math.Ceil(pct/100.0*float64(len(sorted)))) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sorted) {
			idx = len(sorted) - 1
		}
		return sorted[idx]
	}

	p50 = p(50)
	p90 = p(90)
	p95 = p(95)
	p99 = p(99)
	return
}

func AnalyzeLatency(entries []*history.Entry, thresholdPercent float64) *LatencyTrendReport {
	if thresholdPercent <= 0 {
		thresholdPercent = 20.0 // default 20% degradation triggers alert
	}

	// Group entries chronologically (oldest to newest)
	chrono := make([]*history.Entry, len(entries))
	copy(chrono, entries)
	sort.SliceStable(chrono, func(i, j int) bool {
		if chrono[i].Timestamp != "" && chrono[j].Timestamp != "" {
			return chrono[i].Timestamp < chrono[j].Timestamp
		}
		return false
	})

	groups := make(map[string][]*history.Entry)
	var order []string

	for _, e := range chrono {
		target := e.Ref
		if target == "" {
			target = fmt.Sprintf("%s %s", e.Method, e.Url)
		}
		if _, exists := groups[target]; !exists {
			order = append(order, target)
		}
		groups[target] = append(groups[target], e)
	}

	var endpointList []EndpointLatency
	alerts := 0

	for _, target := range order {
		eList := groups[target]
		var latencies []float64
		method := ""
		ref := ""

		for _, e := range eList {
			latencies = append(latencies, e.ElapsedMs)
			if method == "" {
				method = e.Method
			}
			if ref == "" && e.Ref != "" {
				ref = e.Ref
			}
		}

		min, mean, p50, p90, p95, p99, max := calculatePercentiles(latencies)

		ep := EndpointLatency{
			Target:    target,
			Method:    method,
			Ref:       ref,
			Count:     len(latencies),
			MinMs:     min,
			MeanMs:    mean,
			P50Ms:     p50,
			P90Ms:     p90,
			P95Ms:     p95,
			P99Ms:     p99,
			MaxMs:     max,
			Latencies: latencies,
		}

		// Trend analysis if we have at least 4 entries
		if len(latencies) >= 4 {
			split := len(latencies) / 2
			baseline := latencies[:split]
			recent := latencies[split:]

			_, _, _, _, baseP95, _, _ := calculatePercentiles(baseline)
			_, _, _, _, recP95, _, _ := calculatePercentiles(recent)

			ep.BaselineP95 = baseP95
			ep.RecentP95 = recP95
			ep.DeltaMs = recP95 - baseP95

			if baseP95 > 0 {
				ep.DeltaPercent = (recP95 - baseP95) / baseP95 * 100.0
			}

			if ep.DeltaPercent >= thresholdPercent && ep.DeltaMs >= 5.0 {
				ep.Regressed = true
				alerts++
			}
		}

		endpointList = append(endpointList, ep)
	}

	// Sort endpoints by target
	sort.Slice(endpointList, func(i, j int) bool {
		return endpointList[i].Target < endpointList[j].Target
	})

	return &LatencyTrendReport{
		TotalHits:        len(entries),
		EndpointsCount:   len(endpointList),
		RegressionAlerts: alerts,
		ThresholdPercent: thresholdPercent,
		Endpoints:        endpointList,
	}
}

func FormatLatencyTable(rep *LatencyTrendReport, color bool) string {
	var b strings.Builder

	green := func(s string) string {
		if !color {
			return s
		}
		return "\033[32m" + s + "\033[0m"
	}
	red := func(s string) string {
		if !color {
			return s
		}
		return "\033[31m" + s + "\033[0m"
	}
	dim := func(s string) string {
		if !color {
			return s
		}
		return "\033[2m" + s + "\033[0m"
	}
	bold := func(s string) string {
		if !color {
			return s
		}
		return "\033[1m" + s + "\033[0m"
	}

	b.WriteString(bold("API Latency & Regression Trend Report\n"))
	b.WriteString(fmt.Sprintf("Analyzed %d historical hits across %d endpoints (alert threshold: +%.0f%%)\n\n",
		rep.TotalHits, rep.EndpointsCount, rep.ThresholdPercent))

	b.WriteString(fmt.Sprintf("%-28s %-5s %8s %8s %8s %8s %12s\n",
		"ENDPOINT / REF", "HITS", "MIN", "P50", "P95", "MAX", "TREND (Δ)"))
	b.WriteString(strings.Repeat("─", 84) + "\n")

	for _, ep := range rep.Endpoints {
		targetStr := ep.Target
		if len(targetStr) > 28 {
			targetStr = targetStr[:25] + "..."
		}

		trendStr := dim("-")
		if ep.Count >= 4 {
			if ep.Regressed {
				trendStr = red(fmt.Sprintf("+%.1f%% ⚠️", ep.DeltaPercent))
			} else if ep.DeltaPercent <= -5.0 {
				trendStr = green(fmt.Sprintf("%.1f%%", ep.DeltaPercent))
			} else {
				trendStr = dim(fmt.Sprintf("%+.1f%%", ep.DeltaPercent))
			}
		}

		b.WriteString(fmt.Sprintf("%-28s %-5d %7.1fms %7.1fms %7.1fms %7.1fms %12s\n",
			targetStr, ep.Count, ep.MinMs, ep.P50Ms, ep.P95Ms, ep.MaxMs, trendStr))
	}

	if rep.RegressionAlerts > 0 {
		b.WriteString("\n" + bold(red(fmt.Sprintf("⚠️  %d performance regression(s) detected exceeding +%.0f%% threshold\n",
			rep.RegressionAlerts, rep.ThresholdPercent))))
	} else if rep.TotalHits > 0 {
		b.WriteString("\n" + bold(green("✓  No performance regressions detected across historical runs\n")))
	}

	return b.String()
}
