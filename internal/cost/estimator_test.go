package cost

import (
	"math"
	"testing"

	"github.com/hit-endpoint/hit-endpoint/internal/output"
)

func TestCalculateAmplification(t *testing.T) {
	// 0 retries
	if amp := CalculateAmplification(0.05, 0, true); amp != 1.0 {
		t.Errorf("expected 1.0 with 0 retries, got %f", amp)
	}

	// 0% failure rate
	if amp := CalculateAmplification(0.0, 3, true); amp != 1.0 {
		t.Errorf("expected 1.0 with zero failure rate, got %f", amp)
	}

	// 100% failure rate with 3 retries -> 1 + 3 = 4.0
	if amp := CalculateAmplification(1.0, 3, true); amp != 4.0 {
		t.Errorf("expected 4.0 with 100 percent error and 3 retries, got %f", amp)
	}

	// 3% failure rate with 3 retries:
	// sum_{k=0}^3 (0.03)^k = 1 + 0.03 + 0.0009 + 0.000027 = 1.030927
	expected := 1.0 + 0.03 + 0.0009 + 0.000027
	amp := CalculateAmplification(0.03, 3, true)
	if math.Abs(amp-expected) > 1e-6 {
		t.Errorf("expected ~%f, got %f", expected, amp)
	}
}

func TestCalculateTieredCost_Graduated(t *testing.T) {
	var u10k int64 = 10_000
	var u50k int64 = 50_000

	cfg := PricingConfig{
		Currency: "USD",
		Model:    ModelGraduated,
		Tiers: []PricingTier{
			{UpTo: &u10k, UnitPrice: 0.010}, // 10k @ $0.01 = $100
			{UpTo: &u50k, UnitPrice: 0.005}, // 40k @ $0.005 = $200
			{UpTo: nil, UnitPrice: 0.002},   // remainder @ $0.002
		},
	}

	// Test within tier 1: 5,000 requests
	b1 := CalculateTieredCost(5_000, cfg)
	if b1.TotalCost != 50.0 {
		t.Errorf("expected $50.0 for 5k requests, got $%.2f", b1.TotalCost)
	}

	// Test within tier 2: 25,000 requests (10k @ 0.01 = $100 + 15k @ 0.005 = $75 => $175)
	b2 := CalculateTieredCost(25_000, cfg)
	if b2.TotalCost != 175.0 {
		t.Errorf("expected $175.0 for 25k requests, got $%.2f", b2.TotalCost)
	}

	// Test into tier 3: 100,000 requests
	// Tier 1: 10,000 @ 0.010 = $100
	// Tier 2: 40,000 @ 0.005 = $200
	// Tier 3: 50,000 @ 0.002 = $100
	// Total: $400
	b3 := CalculateTieredCost(100_000, cfg)
	if b3.TotalCost != 400.0 {
		t.Errorf("expected $400.0 for 100k requests, got $%.2f", b3.TotalCost)
	}
	if len(b3.TierCosts) != 3 {
		t.Errorf("expected 3 tier breakdowns, got %d", len(b3.TierCosts))
	}
}

func TestCalculateTieredCost_Volume(t *testing.T) {
	var u10k int64 = 10_000
	var u50k int64 = 50_000

	cfg := PricingConfig{
		Currency: "USD",
		Model:    ModelVolume,
		Tiers: []PricingTier{
			{UpTo: &u10k, UnitPrice: 0.010},
			{UpTo: &u50k, UnitPrice: 0.006},
			{UpTo: nil, UnitPrice: 0.003},
		},
	}

	// 5,000 requests -> all at 0.010 = $50
	b1 := CalculateTieredCost(5_000, cfg)
	if b1.TotalCost != 50.0 {
		t.Errorf("expected $50, got $%.2f", b1.TotalCost)
	}

	// 25,000 requests -> all at 0.006 = $150
	b2 := CalculateTieredCost(25_000, cfg)
	if b2.TotalCost != 150.0 {
		t.Errorf("expected $150, got $%.2f", b2.TotalCost)
	}

	// 100,000 requests -> all at 0.003 = $300
	b3 := CalculateTieredCost(100_000, cfg)
	if b3.TotalCost != 300.0 {
		t.Errorf("expected $300, got $%.2f", b3.TotalCost)
	}
}

func TestCalculateTieredCost_Package(t *testing.T) {
	cfg := PricingConfig{
		Currency:     "USD",
		Model:        ModelPackage,
		PackageSize:  1000,
		PackagePrice: 15.0, // $15 per 1,000 calls
	}

	// 2,500 requests -> 3 packages = $45
	b := CalculateTieredCost(2_500, cfg)
	if b.TotalCost != 45.0 {
		t.Errorf("expected $45.0 for 2,500 reqs (3 packages), got $%.2f", b.TotalCost)
	}
}

func TestEstimateRange_FullSpectrum(t *testing.T) {
	var u10k int64 = 10_000
	cfg := PricingConfig{
		Currency: "USD",
		Model:    ModelGraduated,
		Tiers: []PricingTier{
			{UpTo: &u10k, UnitPrice: 0.01},
			{UpTo: nil, UnitPrice: 0.005},
		},
		Retry: RetryConfig{
			MaxAttempts:          3,
			ExpectedFailureRate:  0.05, // 5%
			WorstCaseFailureRate: 0.20, // 20%
			BillableFailures:     true,
		},
	}

	budget := 600.0
	res := EstimateRange(100_000, cfg, &budget)

	// Min (0% errors): 100,000 reqs
	// 10,000 @ 0.01 = $100 + 90,000 @ 0.005 = $450 => $550
	if res.Min.TotalRequests != 100_000 {
		t.Errorf("expected 100,000 min reqs, got %d", res.Min.TotalRequests)
	}
	if res.Min.Breakdown.TotalCost != 550.0 {
		t.Errorf("expected $550.0 min cost, got $%.2f", res.Min.Breakdown.TotalCost)
	}

	// Expected: with 5% failure rate, amp = 1.0526
	if res.Expected.TotalRequests <= 100_000 {
		t.Errorf("expected requests > 100,000 with 5 percent errors, got %d", res.Expected.TotalRequests)
	}
	if res.Expected.Breakdown.TotalCost <= 550.0 {
		t.Errorf("expected cost > $550 with retries, got $%.2f", res.Expected.Breakdown.TotalCost)
	}

	// Worst case: with 20% failure rate, amp is higher
	if res.WorstCase.TotalRequests <= res.Expected.TotalRequests {
		t.Errorf("worst case reqs (%d) should be > expected reqs (%d)", res.WorstCase.TotalRequests, res.Expected.TotalRequests)
	}

	// Outage storm: 100% failure rate with 3 retries => exactly 4.0x
	if res.OutageStorm.TotalRequests != 400_000 {
		t.Errorf("expected 400,000 outage reqs, got %d", res.OutageStorm.TotalRequests)
	}
}

func TestParseQuantityAndTiers(t *testing.T) {
	q1, err := ParseQuantity("10k")
	if err != nil || q1 != 10_000 {
		t.Errorf("ParseQuantity(10k) = %d, err = %v", q1, err)
	}

	q2, err := ParseQuantity("2.5m")
	if err != nil || q2 != 2_500_000 {
		t.Errorf("ParseQuantity(2.5m) = %d, err = %v", q2, err)
	}

	tiers, err := ParseTierString("10k:0.01, 50k:0.005, +:0.002")
	if err != nil {
		t.Fatalf("ParseTierString error: %v", err)
	}
	if len(tiers) != 3 {
		t.Fatalf("expected 3 tiers, got %d", len(tiers))
	}
	if *tiers[0].UpTo != 10_000 || tiers[0].UnitPrice != 0.01 {
		t.Errorf("unexpected tier 0: %+v", tiers[0])
	}
	if tiers[2].UpTo != nil || tiers[2].UnitPrice != 0.002 {
		t.Errorf("unexpected tier 2: %+v", tiers[2])
	}
}

func TestPresets(t *testing.T) {
	p1, err := GetPreset("openai-gpt4o")
	if err != nil {
		t.Fatalf("failed to get preset: %v", err)
	}
	if p1.Tokens == nil || p1.Tokens.PromptPricePer1M != 2.50 {
		t.Errorf("unexpected preset tokens: %+v", p1.Tokens)
	}

	p2, err := GetPreset("stripe")
	if err != nil {
		t.Fatalf("failed to get stripe preset: %v", err)
	}
	if len(p2.Tiers) != 3 {
		t.Errorf("expected 3 tiers for stripe, got %d", len(p2.Tiers))
	}
}

func TestFormatTerminalAndJSON(t *testing.T) {
	cfg, _ := GetPreset("openai-gpt4o")
	budget := 50.0
	res := EstimateRange(10_000, cfg, &budget)

	jsonStr, err := FormatJSON(res)
	if err != nil {
		t.Fatalf("FormatJSON error: %v", err)
	}
	if len(jsonStr) == 0 {
		t.Errorf("empty JSON output")
	}

	p := &output.Printer{Color: false}
	termStr := FormatTerminal(res, p)
	if len(termStr) == 0 {
		t.Errorf("empty terminal output")
	}
}
