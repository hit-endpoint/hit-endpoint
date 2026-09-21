package cost

import "fmt"

// PricingModel represents how volume pricing is calculated.
type PricingModel string

const (
	// ModelGraduated bills each bracket at its corresponding tier price.
	ModelGraduated PricingModel = "graduated"
	// ModelVolume bills all units at the tier price reached by total volume.
	ModelVolume PricingModel = "volume"
	// ModelFlat bills every unit at a fixed single price.
	ModelFlat PricingModel = "flat"
	// ModelPackage bills in fixed unit bundles (e.g. $10 per 1,000 calls).
	ModelPackage PricingModel = "package"
)

// PricingTier defines a volume bracket and its unit price.
type PricingTier struct {
	// UpTo is the upper bound of the tier (nil or 0 means unbounded/infinity).
	UpTo *int64 `json:"up_to" yaml:"up_to"`
	// UnitPrice is the cost per single request or unit within this tier.
	UnitPrice float64 `json:"unit_price" yaml:"unit_price"`
	// FlatFee is an optional base surcharge for entering this tier.
	FlatFee float64 `json:"flat_fee,omitempty" yaml:"flat_fee,omitempty"`
}

// TokenPricing defines costs for token-based APIs (e.g., LLMs).
type TokenPricing struct {
	// PromptPricePer1M is the cost per 1,000,000 input/prompt tokens.
	PromptPricePer1M float64 `json:"prompt_price_per_1m" yaml:"prompt_price_per_1m"`
	// CompletionPricePer1M is the cost per 1,000,000 output/completion tokens.
	CompletionPricePer1M float64 `json:"completion_price_per_1m" yaml:"completion_price_per_1m"`
	// AvgPromptTokens is the average number of input tokens per request.
	AvgPromptTokens float64 `json:"avg_prompt_tokens" yaml:"avg_prompt_tokens"`
	// AvgCompletionTokens is the average number of output tokens per request.
	AvgCompletionTokens float64 `json:"avg_completion_tokens" yaml:"avg_completion_tokens"`
}

// RetryConfig defines failure assumptions and retry behavior.
type RetryConfig struct {
	// MaxAttempts is the maximum number of retries configured in the client (e.g. 3).
	MaxAttempts int `json:"max_attempts" yaml:"max_attempts"`
	// ExpectedFailureRate is the baseline production failure rate (0.0 to 1.0, e.g. 0.03 = 3%).
	ExpectedFailureRate float64 `json:"expected_failure_rate" yaml:"expected_failure_rate"`
	// WorstCaseFailureRate is the degraded/spike failure rate (0.0 to 1.0, e.g. 0.25 = 25%).
	WorstCaseFailureRate float64 `json:"worst_case_failure_rate" yaml:"worst_case_failure_rate"`
	// BillableFailures indicates whether the provider charges for failed/429/5xx attempts.
	BillableFailures bool `json:"billable_failures" yaml:"billable_failures"`
}

// DefaultRetryConfig returns sensible defaults for commercial APIs.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:          3,
		ExpectedFailureRate:  0.03, // 3% typical baseline
		WorstCaseFailureRate: 0.25, // 25% during downstream incidents
		BillableFailures:     true, // Commercial gateways bill on received requests
	}
}

// PricingConfig is the top-level configuration for pricing and retries.
type PricingConfig struct {
	Name         string        `json:"name,omitempty" yaml:"name,omitempty"`
	Currency     string        `json:"currency" yaml:"currency"`
	BaseFee      float64       `json:"base_fee,omitempty" yaml:"base_fee,omitempty"`
	Model        PricingModel  `json:"pricing_model" yaml:"pricing_model"`
	Unit         string        `json:"unit" yaml:"unit"` // e.g. "request", "call", "token"
	PackageSize  int64         `json:"package_size,omitempty" yaml:"package_size,omitempty"`
	PackagePrice float64       `json:"package_price,omitempty" yaml:"package_price,omitempty"`
	UnitPrice    float64       `json:"unit_price,omitempty" yaml:"unit_price,omitempty"` // For flat model
	Tiers        []PricingTier `json:"tiers,omitempty" yaml:"tiers,omitempty"`
	Tokens       *TokenPricing `json:"tokens,omitempty" yaml:"tokens,omitempty"`
	Retry        RetryConfig   `json:"retry" yaml:"retry"`
}

// CurrencySymbol returns a display symbol for common currency codes.
func (c PricingConfig) CurrencySymbol() string {
	switch c.Currency {
	case "USD", "$", "":
		return "$"
	case "EUR", "€":
		return "€"
	case "GBP", "£":
		return "£"
	case "JPY", "¥":
		return "¥"
	default:
		return c.Currency + " "
	}
}

// TierCostItem represents the cost breakdown for a specific tier.
type TierCostItem struct {
	TierIndex    int     `json:"tier_index"`
	From         int64   `json:"from"`
	To           *int64  `json:"to"`
	UnitPrice    float64 `json:"unit_price"`
	UnitsCharged int64   `json:"units_charged"`
	Cost         float64 `json:"cost"`
}

// CostBreakdown holds the itemized cost calculation for a given volume.
type CostBreakdown struct {
	TotalRequests int64          `json:"total_requests"`
	BaseFee       float64        `json:"base_fee"`
	TierCosts     []TierCostItem `json:"tier_costs,omitempty"`
	RequestsCost  float64        `json:"requests_cost"`
	TokensCost    float64        `json:"tokens_cost,omitempty"`
	TotalCost     float64        `json:"total_cost"`
}

// ScenarioEstimate captures the estimate for one failure scenario.
type ScenarioEstimate struct {
	Name                string        `json:"name"`
	FailureRate         float64       `json:"failure_rate"`
	Amplification       float64       `json:"amplification"`
	TotalRequests       int64         `json:"total_requests"`
	Breakdown           CostBreakdown `json:"breakdown"`
	CostDifference      float64       `json:"cost_difference"`       // Compared to Min
	CostIncreasePercent float64       `json:"cost_increase_percent"` // Compared to Min
}

// CostRangeResult captures the complete multi-scenario estimation.
type CostRangeResult struct {
	Target         string           `json:"target,omitempty"`
	IntendedCalls  int64            `json:"intended_calls"`
	Currency       string           `json:"currency"`
	Pricing        PricingConfig    `json:"pricing"`
	Min            ScenarioEstimate `json:"min"`
	Expected       ScenarioEstimate `json:"expected"`
	WorstCase      ScenarioEstimate `json:"worst_case"`
	OutageStorm    ScenarioEstimate `json:"outage_storm"`
	Budget         *float64         `json:"budget,omitempty"`
	BudgetExceeded bool             `json:"budget_exceeded"`
}

// FormatMoney formats an amount using the currency symbol.
func (r CostRangeResult) FormatMoney(amount float64) string {
	sym := r.Pricing.CurrencySymbol()
	return fmt.Sprintf("%s%.2f", sym, amount)
}
