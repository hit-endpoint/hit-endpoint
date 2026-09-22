package cost

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hit-endpoint/hit-endpoint/internal/output"
)

// FormatTerminal generates human-readable colored terminal output.
func FormatTerminal(res CostRangeResult, p *output.Printer) string {
	var sb strings.Builder
	sym := res.Pricing.CurrencySymbol()

	title := "API Cost & Retry Amplification Estimator"
	sb.WriteString(fmt.Sprintf("%s %s\n", p.Cyan("💰"), p.Bold(title)))
	sb.WriteString(p.Dim(strings.Repeat("─", 68)) + "\n")

	target := res.Target
	if target == "" {
		if res.Pricing.Name != "" {
			target = res.Pricing.Name
		} else {
			target = "Ad-hoc Configuration"
		}
	}
	sb.WriteString(fmt.Sprintf("  • %-20s %s\n", "Target:", p.Bold(target)))
	sb.WriteString(fmt.Sprintf("  • %-20s %s %s\n", "Intended Calls:", p.Cyan(formatInt(res.IntendedCalls)), res.Pricing.Unit+"s"))
	sb.WriteString(fmt.Sprintf("  • %-20s %s\n", "Pricing Model:", strings.Title(string(res.Pricing.Model))))
	sb.WriteString(fmt.Sprintf("  • %-20s %s (%s)\n", "Currency:", res.Pricing.Currency, sym))
	sb.WriteString(fmt.Sprintf("  • %-20s Max %d retries (Expected: %.1f%%, Worst-case: %.1f%%)\n",
		"Retry Policy:",
		res.Pricing.Retry.MaxAttempts,
		res.Expected.FailureRate*100.0,
		res.WorstCase.FailureRate*100.0,
	))

	sb.WriteString("\n" + p.Bold("📊 Cost Range Summary:") + "\n")

	// Table Header
	colMetric := 18
	colMin := 14
	colExp := 16
	colWorst := 16

	lineSep := fmt.Sprintf("┌%s┬%s┬%s┬%s┐\n",
		strings.Repeat("─", colMetric+2),
		strings.Repeat("─", colMin+2),
		strings.Repeat("─", colExp+2),
		strings.Repeat("─", colWorst+2),
	)
	midSep := fmt.Sprintf("├%s┼%s┼%s┼%s┤\n",
		strings.Repeat("─", colMetric+2),
		strings.Repeat("─", colMin+2),
		strings.Repeat("─", colExp+2),
		strings.Repeat("─", colWorst+2),
	)
	botSep := fmt.Sprintf("└%s┴%s┴%s┴%s┘\n",
		strings.Repeat("─", colMetric+2),
		strings.Repeat("─", colMin+2),
		strings.Repeat("─", colExp+2),
		strings.Repeat("─", colWorst+2),
	)

	sb.WriteString(lineSep)
	sb.WriteString(fmt.Sprintf("│ %-*s │ %-*s │ %-*s │ %-*s │\n",
		colMetric, "Metric",
		colMin, "Min (0% Fail)",
		colExp, fmt.Sprintf("Expected (%.1f%%)", res.Expected.FailureRate*100),
		colWorst, fmt.Sprintf("Worst (%.1f%%)", res.WorstCase.FailureRate*100),
	))
	sb.WriteString(midSep)

	// Total Requests Row
	sb.WriteString(fmt.Sprintf("│ %-*s │ %-*s │ %-*s │ %-*s │\n",
		colMetric, "Total Reqs",
		colMin, formatInt(res.Min.TotalRequests),
		colExp, formatInt(res.Expected.TotalRequests),
		colWorst, formatInt(res.WorstCase.TotalRequests),
	))

	// Amplification Row
	sb.WriteString(fmt.Sprintf("│ %-*s │ %-*s │ %-*s │ %-*s │\n",
		colMetric, "Amplification",
		colMin, fmt.Sprintf("%.2fx", res.Min.Amplification),
		colExp, fmt.Sprintf("%.2fx", res.Expected.Amplification),
		colWorst, fmt.Sprintf("%.2fx", res.WorstCase.Amplification),
	))

	// Base Fee Row (if non-zero)
	if res.Pricing.BaseFee > 0 {
		sb.WriteString(fmt.Sprintf("│ %-*s │ %-*s │ %-*s │ %-*s │\n",
			colMetric, "Base Fee",
			colMin, res.FormatMoney(res.Min.Breakdown.BaseFee),
			colExp, res.FormatMoney(res.Expected.Breakdown.BaseFee),
			colWorst, res.FormatMoney(res.WorstCase.Breakdown.BaseFee),
		))
	}

	// Requests Cost Row
	sb.WriteString(fmt.Sprintf("│ %-*s │ %-*s │ %-*s │ %-*s │\n",
		colMetric, "Requests Cost",
		colMin, res.FormatMoney(res.Min.Breakdown.RequestsCost),
		colExp, res.FormatMoney(res.Expected.Breakdown.RequestsCost),
		colWorst, res.FormatMoney(res.WorstCase.Breakdown.RequestsCost),
	))

	// Tokens Cost Row (if tokens defined)
	if res.Pricing.Tokens != nil {
		sb.WriteString(fmt.Sprintf("│ %-*s │ %-*s │ %-*s │ %-*s │\n",
			colMetric, "Tokens Cost",
			colMin, res.FormatMoney(res.Min.Breakdown.TokensCost),
			colExp, res.FormatMoney(res.Expected.Breakdown.TokensCost),
			colWorst, res.FormatMoney(res.WorstCase.Breakdown.TokensCost),
		))
	}

	// Total Estimated Cost
	sb.WriteString(midSep)
	sb.WriteString(fmt.Sprintf("│ %s │ %s │ %s │ %s │\n",
		p.Bold(fmt.Sprintf("%-*s", colMetric, "Total Est. Cost")),
		p.Bold(fmt.Sprintf("%-*s", colMin, res.FormatMoney(res.Min.Breakdown.TotalCost))),
		p.Cyan(p.Bold(fmt.Sprintf("%-*s", colExp, res.FormatMoney(res.Expected.Breakdown.TotalCost)))),
		p.Yellow(p.Bold(fmt.Sprintf("%-*s", colWorst, res.FormatMoney(res.WorstCase.Breakdown.TotalCost)))),
	))

	// Retry Cost Tax (Delta)
	sb.WriteString(fmt.Sprintf("│ %-*s │ %-*s │ %-*s │ %-*s │\n",
		colMetric, "Retry Cost Tax",
		colMin, "$0.00 (base)",
		colExp, fmt.Sprintf("+%s (+%.1f%%)", res.FormatMoney(res.Expected.CostDifference), res.Expected.CostIncreasePercent),
		colWorst, fmt.Sprintf("+%s (+%.1f%%)", res.FormatMoney(res.WorstCase.CostDifference), res.WorstCase.CostIncreasePercent),
	))
	sb.WriteString(botSep)

	// Itemized Tier Breakdown for Expected Scenario
	if len(res.Expected.Breakdown.TierCosts) > 0 {
		sb.WriteString("\n" + p.Bold(fmt.Sprintf("📈 Tier Breakdown (Expected: %s requests):", formatInt(res.Expected.TotalRequests))) + "\n")
		for _, item := range res.Expected.Breakdown.TierCosts {
			boundStr := "+"
			if item.To != nil {
				boundStr = formatInt(*item.To)
			}
			bracket := fmt.Sprintf("Tier %d (%s - %s)", item.TierIndex, formatInt(item.From), boundStr)
			sb.WriteString(fmt.Sprintf("  • %-26s %10s reqs @ %s = %s\n",
				bracket,
				formatInt(item.UnitsCharged),
				fmt.Sprintf("%s%.4f", sym, item.UnitPrice),
				p.Cyan(res.FormatMoney(item.Cost)),
			))
		}
	}

	// Outage Storm Warning (100% failure scenario)
	sb.WriteString("\n" + p.Yellow("⚠️  Outage Storm Exposure (100% Failures):") + "\n")
	sb.WriteString(fmt.Sprintf("  If downstream suffers an outage and all %d retries exhaust:\n", res.Pricing.Retry.MaxAttempts))
	sb.WriteString(fmt.Sprintf("  • Requests surge to: %s (%s, %.1fx amplification)\n",
		p.Bold(formatInt(res.OutageStorm.TotalRequests)),
		p.Red(res.FormatMoney(res.OutageStorm.Breakdown.TotalCost)),
		res.OutageStorm.Amplification,
	))
	sb.WriteString(fmt.Sprintf("  • Maximum Financial Liability: +%s (+%.1f%% cost spike)\n",
		res.FormatMoney(res.OutageStorm.CostDifference),
		res.OutageStorm.CostIncreasePercent,
	))

	// FinOps Budget Check
	if res.Budget != nil {
		sb.WriteString("\n" + p.Dim(strings.Repeat("─", 68)) + "\n")
		if res.BudgetExceeded {
			overage := res.Expected.Breakdown.TotalCost - *res.Budget
			sb.WriteString(p.Red(p.Bold(fmt.Sprintf("❌ BUDGET EXCEEDED: Expected cost %s exceeds budget of %s by %s!\n",
				res.FormatMoney(res.Expected.Breakdown.TotalCost),
				res.FormatMoney(*res.Budget),
				res.FormatMoney(overage),
			))))
		} else {
			headroom := *res.Budget - res.Expected.Breakdown.TotalCost
			sb.WriteString(p.Green(p.Bold(fmt.Sprintf("✓ BUDGET OK: Expected cost %s is within budget ceiling of %s (headroom: %s).\n",
				res.FormatMoney(res.Expected.Breakdown.TotalCost),
				res.FormatMoney(*res.Budget),
				res.FormatMoney(headroom),
			))))
		}
	}

	return sb.String()
}

// FormatJSON serializes the CostRangeResult as indented JSON.
func FormatJSON(res CostRangeResult) (string, error) {
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func formatInt(n int64) string {
	in := fmt.Sprintf("%d", n)
	var out []byte
	l := len(in)
	for i := 0; i < l; i++ {
		if i > 0 && (l-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, in[i])
	}
	return string(out)
}
