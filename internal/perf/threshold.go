package perf

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/hit-endpoint/hit-endpoint/internal/types"
)

var thresholdRegex = regexp.MustCompile(`^\s*([a-zA-Z0-9_]+)\s*(<=|>=|<|>|==|=)\s*([0-9.]+)\s*(ms|s|%)?\s*$`)

type ThresholdRule struct {
	Metric    string
	Op        string
	Target    float64
	Unit      string
	IsPercent bool
	Raw       string
}

type ThresholdResult struct {
	Rule    ThresholdRule
	Actual  float64
	Passed  bool
	Message string
}

func ParseThresholds(spec string) ([]ThresholdRule, error) {
	parts := strings.Split(spec, ",")
	var rules []ThresholdRule

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}

		matches := thresholdRegex.FindStringSubmatch(trimmed)
		if matches == nil {
			return nil, fmt.Errorf("invalid threshold expression '%s' (expected format: p95<200ms, errors<1%%, rps>50)", trimmed)
		}

		metric := strings.ToLower(matches[1])
		op := matches[2]
		if op == "=" {
			op = "=="
		}
		val, err := strconv.ParseFloat(matches[3], 64)
		if err != nil {
			return nil, fmt.Errorf("invalid threshold value in '%s': %w", trimmed, err)
		}

		unit := strings.ToLower(matches[4])
		isPercent := unit == "%"
		if isPercent {
			val = val / 100.0 // 1% -> 0.01
		}

		rules = append(rules, ThresholdRule{
			Metric:    metric,
			Op:        op,
			Target:    val,
			Unit:      unit,
			IsPercent: isPercent,
			Raw:       trimmed,
		})
	}

	return rules, nil
}

func EvaluateThresholds(rep *types.PerfReport, rules []ThresholdRule) []ThresholdResult {
	stats := rep.Stats()
	var results []ThresholdResult

	for _, rule := range rules {
		var actual float64
		var actualDisplay string

		switch rule.Metric {
		case "min", "mean", "p50", "p90", "p95", "p99", "max":
			actual = stats[rule.Metric]
			actualDisplay = fmt.Sprintf("%.1fms", actual)
		case "rps":
			actual = rep.RPS()
			actualDisplay = fmt.Sprintf("%.1f req/s", actual)
		case "errors", "failed":
			if rule.IsPercent {
				if rep.Completed > 0 {
					actual = float64(rep.Failed) / float64(rep.Completed)
				}
				actualDisplay = fmt.Sprintf("%.1f%%", actual*100.0)
			} else {
				actual = float64(rep.Failed)
				actualDisplay = fmt.Sprintf("%d", int(actual))
			}
		case "completed", "ok":
			if rule.IsPercent {
				if rep.Completed > 0 {
					actual = float64(rep.OK) / float64(rep.Completed)
				}
				actualDisplay = fmt.Sprintf("%.1f%%", actual*100.0)
			} else {
				actual = float64(rep.OK)
				actualDisplay = fmt.Sprintf("%d", int(actual))
			}
		default:
			// Try matching against stats map
			if v, ok := stats[rule.Metric]; ok {
				actual = v
				actualDisplay = fmt.Sprintf("%.1f", actual)
			} else {
				results = append(results, ThresholdResult{
					Rule:    rule,
					Actual:  0,
					Passed:  false,
					Message: fmt.Sprintf("unknown metric '%s'", rule.Metric),
				})
				continue
			}
		}

		passed := checkCondition(actual, rule.Op, rule.Target)
		targetDisplay := fmt.Sprintf("%v%s", rule.Target, rule.Unit)
		if rule.IsPercent {
			targetDisplay = fmt.Sprintf("%.1f%%", rule.Target*100.0)
		}

		msg := fmt.Sprintf("%s %s %s (actual: %s)", rule.Metric, rule.Op, targetDisplay, actualDisplay)
		results = append(results, ThresholdResult{
			Rule:    rule,
			Actual:  actual,
			Passed:  passed,
			Message: msg,
		})
	}

	return results
}

func checkCondition(actual float64, op string, target float64) bool {
	// Floating point epsilon
	eps := 1e-9
	switch op {
	case "<":
		return actual < target
	case "<=":
		return actual <= target+eps
	case ">":
		return actual > target
	case ">=":
		return actual >= target-eps
	case "==":
		diff := actual - target
		return diff > -eps && diff < eps
	default:
		return false
	}
}
