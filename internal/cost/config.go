package cost

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseQuantity converts strings like "10k", "50k", "1.5M", "100000" into int64.
func ParseQuantity(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, fmt.Errorf("empty quantity string")
	}

	multiplier := int64(1)
	if strings.HasSuffix(s, "k") {
		multiplier = 1_000
		s = strings.TrimSuffix(s, "k")
	} else if strings.HasSuffix(s, "m") {
		multiplier = 1_000_000
		s = strings.TrimSuffix(s, "m")
	} else if strings.HasSuffix(s, "b") {
		multiplier = 1_000_000_000
		s = strings.TrimSuffix(s, "b")
	}

	// Handle float if present, e.g. "1.5m"
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int64(f * float64(multiplier)), nil
	}

	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid quantity %q: %w", s, err)
	}
	return n * multiplier, nil
}

// ParseTierString parses comma-separated tier definitions like "10k:0.01,50k:0.005,+:0.002".
func ParseTierString(tierSpec string) ([]PricingTier, error) {
	tierSpec = strings.TrimSpace(tierSpec)
	if tierSpec == "" {
		return nil, nil
	}

	var tiers []PricingTier
	parts := strings.Split(tierSpec, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.Split(part, ":")
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid tier syntax %q: expected UP_TO:PRICE", part)
		}
		boundStr := strings.TrimSpace(kv[0])
		priceStr := strings.TrimSpace(kv[1])

		price, err := strconv.ParseFloat(priceStr, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid tier unit price %q in %q: %w", priceStr, part, err)
		}

		if boundStr == "+" || boundStr == "*" || strings.ToLower(boundStr) == "max" || strings.ToLower(boundStr) == "null" {
			tiers = append(tiers, PricingTier{
				UpTo:      nil,
				UnitPrice: price,
			})
		} else {
			bound, err := ParseQuantity(boundStr)
			if err != nil {
				return nil, fmt.Errorf("invalid tier bound %q in %q: %w", boundStr, part, err)
			}
			tiers = append(tiers, PricingTier{
				UpTo:      &bound,
				UnitPrice: price,
			})
		}
	}
	return tiers, nil
}

// LoadConfigFile loads a PricingConfig from a YAML file.
func LoadConfigFile(path string) (*PricingConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read pricing file %s: %w", path, err)
	}

	var cfg PricingConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse pricing YAML %s: %w", path, err)
	}

	normalizeConfig(&cfg)
	return &cfg, nil
}

// FromMap loads a PricingConfig from an arbitrary YAML-parsed map (e.g. spec Extra["cost"]).
func FromMap(m map[string]any) (*PricingConfig, error) {
	data, err := yaml.Marshal(m)
	if err != nil {
		return nil, err
	}
	var cfg PricingConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	normalizeConfig(&cfg)
	return &cfg, nil
}

func normalizeConfig(cfg *PricingConfig) {
	if cfg.Currency == "" {
		cfg.Currency = "USD"
	}
	if cfg.Unit == "" {
		cfg.Unit = "request"
	}
	if cfg.Model == "" {
		if len(cfg.Tiers) > 0 {
			cfg.Model = ModelGraduated
		} else if cfg.PackageSize > 0 && cfg.PackagePrice > 0 {
			cfg.Model = ModelPackage
		} else {
			cfg.Model = ModelFlat
		}
	}
	if cfg.Retry.MaxAttempts <= 0 {
		cfg.Retry.MaxAttempts = 3
	}
	if cfg.Retry.ExpectedFailureRate <= 0.0 {
		cfg.Retry.ExpectedFailureRate = 0.03
	}
	if cfg.Retry.WorstCaseFailureRate <= 0.0 {
		cfg.Retry.WorstCaseFailureRate = 0.25
	}
}

// GetPreset returns a built-in pricing configuration for popular commercial APIs.
func GetPreset(name string) (PricingConfig, error) {
	name = strings.TrimSpace(strings.ToLower(name))
	var u10k int64 = 10_000
	var u50k int64 = 50_000
	var u100k int64 = 100_000
	var u500k int64 = 500_000

	switch name {
	case "openai-gpt4o", "gpt4o", "gpt-4o":
		return PricingConfig{
			Name:     "OpenAI GPT-4o",
			Currency: "USD",
			Model:    ModelFlat,
			Unit:     "request",
			Tokens: &TokenPricing{
				PromptPricePer1M:     2.50,  // $2.50 / 1M prompt
				CompletionPricePer1M: 10.00, // $10.00 / 1M output
				AvgPromptTokens:      800,
				AvgCompletionTokens:  200,
			},
			Retry: DefaultRetryConfig(),
		}, nil

	case "openai-gpt4o-mini", "gpt4o-mini", "gpt-4o-mini":
		return PricingConfig{
			Name:     "OpenAI GPT-4o mini",
			Currency: "USD",
			Model:    ModelFlat,
			Unit:     "request",
			Tokens: &TokenPricing{
				PromptPricePer1M:     0.15, // $0.15 / 1M prompt
				CompletionPricePer1M: 0.60, // $0.60 / 1M output
				AvgPromptTokens:      800,
				AvgCompletionTokens:  200,
			},
			Retry: DefaultRetryConfig(),
		}, nil

	case "claude-3-5-sonnet", "claude-sonnet", "sonnet-3-5":
		return PricingConfig{
			Name:     "Anthropic Claude 3.5 Sonnet",
			Currency: "USD",
			Model:    ModelFlat,
			Unit:     "request",
			Tokens: &TokenPricing{
				PromptPricePer1M:     3.00,  // $3.00 / 1M prompt
				CompletionPricePer1M: 15.00, // $15.00 / 1M output
				AvgPromptTokens:      800,
				AvgCompletionTokens:  200,
			},
			Retry: DefaultRetryConfig(),
		}, nil

	case "stripe-charges", "stripe":
		return PricingConfig{
			Name:     "Stripe Payments API",
			Currency: "USD",
			Model:    ModelGraduated,
			Unit:     "transaction",
			Tiers: []PricingTier{
				{UpTo: &u10k, UnitPrice: 0.30},
				{UpTo: &u50k, UnitPrice: 0.25},
				{UpTo: nil, UnitPrice: 0.20},
			},
			Retry: DefaultRetryConfig(),
		}, nil

	case "twilio-sms", "twilio":
		return PricingConfig{
			Name:      "Twilio SMS",
			Currency:  "USD",
			Model:     ModelFlat,
			Unit:      "message",
			UnitPrice: 0.0079, // $0.0079 per message
			Retry:     DefaultRetryConfig(),
		}, nil

	case "google-maps", "maps":
		return PricingConfig{
			Name:     "Google Maps Geocoding/Directions",
			Currency: "USD",
			Model:    ModelGraduated,
			Unit:     "request",
			Tiers: []PricingTier{
				{UpTo: &u100k, UnitPrice: 0.0050},
				{UpTo: &u500k, UnitPrice: 0.0040},
				{UpTo: nil, UnitPrice: 0.0032},
			},
			Retry: DefaultRetryConfig(),
		}, nil

	default:
		return PricingConfig{}, fmt.Errorf("unknown preset %q. Available: openai-gpt4o, openai-gpt4o-mini, claude-3-5-sonnet, stripe-charges, twilio-sms, google-maps", name)
	}
}
