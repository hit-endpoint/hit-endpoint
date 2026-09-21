package cost

import (
	"math"
	"sort"
)

// CalculateAmplification computes the request multiplier based on failure rate and retries.
func CalculateAmplification(p float64, maxRetries int, billableFailures bool) float64 {
	if maxRetries <= 0 {
		return 1.0
	}
	if p <= 0.0 {
		return 1.0
	}
	if p >= 1.0 {
		if billableFailures {
			return float64(1 + maxRetries)
		}
		// If failures aren't billable and every attempt fails, billable requests is 0
		return 0.0
	}

	if billableFailures {
		// E[attempts] = sum_{k=0}^M p^k = (1 - p^{M+1}) / (1 - p)
		return (1.0 - math.Pow(p, float64(maxRetries+1))) / (1.0 - p)
	}

	// Only successful responses are billed (at most 1 success per op)
	// Success on attempt k occurs with probability p^k * (1 - p)
	// Sum_{k=0}^M p^k * (1-p) = 1 - p^{M+1}
	return 1.0 - math.Pow(p, float64(maxRetries+1))
}

// CalculateTieredCost computes the itemized breakdown for a given request volume.
func CalculateTieredCost(totalRequests int64, cfg PricingConfig) CostBreakdown {
	breakdown := CostBreakdown{
		TotalRequests: totalRequests,
		BaseFee:       cfg.BaseFee,
		TierCosts:     []TierCostItem{},
	}

	if totalRequests <= 0 {
		breakdown.TotalCost = cfg.BaseFee
		return breakdown
	}

	// Normalize model
	model := cfg.Model
	if model == "" {
		if len(cfg.Tiers) > 0 {
			model = ModelGraduated
		} else if cfg.PackageSize > 0 && cfg.PackagePrice > 0 {
			model = ModelPackage
		} else {
			model = ModelFlat
		}
	}

	switch model {
	case ModelFlat:
		unitPrice := cfg.UnitPrice
		if unitPrice == 0 && len(cfg.Tiers) > 0 {
			unitPrice = cfg.Tiers[0].UnitPrice
		}
		reqCost := float64(totalRequests) * unitPrice
		breakdown.RequestsCost = reqCost
		breakdown.TierCosts = append(breakdown.TierCosts, TierCostItem{
			TierIndex:    1,
			From:         1,
			To:           nil,
			UnitPrice:    unitPrice,
			UnitsCharged: totalRequests,
			Cost:         reqCost,
		})

	case ModelPackage:
		pkgSize := cfg.PackageSize
		if pkgSize <= 0 {
			pkgSize = 1000
		}
		numPackages := int64(math.Ceil(float64(totalRequests) / float64(pkgSize)))
		reqCost := float64(numPackages) * cfg.PackagePrice
		breakdown.RequestsCost = reqCost
		breakdown.TierCosts = append(breakdown.TierCosts, TierCostItem{
			TierIndex:    1,
			From:         1,
			To:           nil,
			UnitPrice:    cfg.PackagePrice / float64(pkgSize),
			UnitsCharged: numPackages * pkgSize,
			Cost:         reqCost,
		})

	case ModelVolume:
		// All units billed at the single rate of the tier reached
		tiers := sortedTiers(cfg.Tiers)
		var chosenTier PricingTier
		if len(tiers) > 0 {
			chosenTier = tiers[len(tiers)-1] // default to last tier
			for _, t := range tiers {
				if t.UpTo == nil || totalRequests <= *t.UpTo {
					chosenTier = t
					break
				}
			}
		} else {
			chosenTier = PricingTier{UnitPrice: cfg.UnitPrice}
		}

		reqCost := float64(totalRequests)*chosenTier.UnitPrice + chosenTier.FlatFee
		breakdown.RequestsCost = reqCost
		breakdown.TierCosts = append(breakdown.TierCosts, TierCostItem{
			TierIndex:    1,
			From:         1,
			To:           chosenTier.UpTo,
			UnitPrice:    chosenTier.UnitPrice,
			UnitsCharged: totalRequests,
			Cost:         reqCost,
		})

	case ModelGraduated:
		// Each bracket billed at its respective unit price
		tiers := sortedTiers(cfg.Tiers)
		remaining := totalRequests
		var prevBound int64 = 0

		for i, tier := range tiers {
			if remaining <= 0 {
				break
			}

			var tierCapacity int64
			if tier.UpTo == nil {
				tierCapacity = remaining // unbounded remainder
			} else {
				tierCapacity = *tier.UpTo - prevBound
				if tierCapacity < 0 {
					tierCapacity = 0
				}
			}

			charged := remaining
			if charged > tierCapacity {
				charged = tierCapacity
			}

			tierCost := float64(charged)*tier.UnitPrice + tier.FlatFee
			breakdown.RequestsCost += tierCost
			breakdown.TierCosts = append(breakdown.TierCosts, TierCostItem{
				TierIndex:    i + 1,
				From:         prevBound + 1,
				To:           tier.UpTo,
				UnitPrice:    tier.UnitPrice,
				UnitsCharged: charged,
				Cost:         tierCost,
			})

			remaining -= charged
			if tier.UpTo != nil {
				prevBound = *tier.UpTo
			}
		}

		// If requests still remain beyond all defined tiers, bill at last tier price
		if remaining > 0 && len(tiers) > 0 {
			lastPrice := tiers[len(tiers)-1].UnitPrice
			remCost := float64(remaining) * lastPrice
			breakdown.RequestsCost += remCost
			breakdown.TierCosts = append(breakdown.TierCosts, TierCostItem{
				TierIndex:    len(tiers) + 1,
				From:         prevBound + 1,
				To:           nil,
				UnitPrice:    lastPrice,
				UnitsCharged: remaining,
				Cost:         remCost,
			})
		}
	}

	breakdown.TotalCost = breakdown.BaseFee + breakdown.RequestsCost
	return breakdown
}

// CalculateTokenCost computes LLM prompt/completion costs.
func CalculateTokenCost(intendedCalls, totalRequests int64, tokens *TokenPricing) float64 {
	if tokens == nil {
		return 0.0
	}

	// Prompt tokens are re-transmitted on every attempt (including retries)
	totalPromptTokens := float64(totalRequests) * tokens.AvgPromptTokens
	promptCost := (totalPromptTokens / 1_000_000.0) * tokens.PromptPricePer1M

	// Completion tokens are generated for intended completed requests
	totalCompletionTokens := float64(intendedCalls) * tokens.AvgCompletionTokens
	completionCost := (totalCompletionTokens / 1_000_000.0) * tokens.CompletionPricePer1M

	return promptCost + completionCost
}

// EstimateScenario evaluates a single failure rate scenario.
func EstimateScenario(name string, intendedCalls int64, failureRate float64, cfg PricingConfig) ScenarioEstimate {
	amp := CalculateAmplification(failureRate, cfg.Retry.MaxAttempts, cfg.Retry.BillableFailures)
	totalReqs := int64(math.Round(float64(intendedCalls) * amp))

	breakdown := CalculateTieredCost(totalReqs, cfg)

	if cfg.Tokens != nil {
		tokensCost := CalculateTokenCost(intendedCalls, totalReqs, cfg.Tokens)
		breakdown.TokensCost = tokensCost
		breakdown.TotalCost += tokensCost
	}

	return ScenarioEstimate{
		Name:          name,
		FailureRate:   failureRate,
		Amplification: amp,
		TotalRequests: totalReqs,
		Breakdown:     breakdown,
	}
}

// EstimateRange produces the full range of cost estimations (Min, Expected, Worst-Case, Outage Storm).
func EstimateRange(intendedCalls int64, cfg PricingConfig, budget *float64) CostRangeResult {
	minEstimate := EstimateScenario("Min (0% Errors)", intendedCalls, 0.0, cfg)

	expRate := cfg.Retry.ExpectedFailureRate
	if expRate <= 0.0 {
		expRate = 0.03 // default 3%
	}
	expectedEstimate := EstimateScenario(
		"Expected",
		intendedCalls,
		expRate,
		cfg,
	)

	worstRate := cfg.Retry.WorstCaseFailureRate
	if worstRate <= 0.0 {
		worstRate = 0.25 // default 25%
	}
	worstEstimate := EstimateScenario(
		"Worst-Case",
		intendedCalls,
		worstRate,
		cfg,
	)

	outageEstimate := EstimateScenario(
		"Outage Storm (100% Failures)",
		intendedCalls,
		1.0,
		cfg,
	)

	// Calculate differences relative to Min
	baseCost := minEstimate.Breakdown.TotalCost
	setDiffs := func(s *ScenarioEstimate) {
		s.CostDifference = s.Breakdown.TotalCost - baseCost
		if baseCost > 0 {
			s.CostIncreasePercent = (s.CostDifference / baseCost) * 100.0
		}
	}
	setDiffs(&expectedEstimate)
	setDiffs(&worstEstimate)
	setDiffs(&outageEstimate)

	result := CostRangeResult{
		IntendedCalls: intendedCalls,
		Currency:      cfg.Currency,
		Pricing:       cfg,
		Min:           minEstimate,
		Expected:      expectedEstimate,
		WorstCase:     worstEstimate,
		OutageStorm:   outageEstimate,
		Budget:        budget,
	}

	if budget != nil && expectedEstimate.Breakdown.TotalCost > *budget {
		result.BudgetExceeded = true
	}

	return result
}

// sortedTiers returns a copy of tiers sorted by UpTo ascending.
func sortedTiers(tiers []PricingTier) []PricingTier {
	if len(tiers) == 0 {
		return nil
	}
	res := make([]PricingTier, len(tiers))
	copy(res, tiers)
	sort.Slice(res, func(i, j int) bool {
		if res[i].UpTo == nil {
			return false
		}
		if res[j].UpTo == nil {
			return true
		}
		return *res[i].UpTo < *res[j].UpTo
	})
	return res
}
