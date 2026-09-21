package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hit/internal/fuzz"
)

const (
	SarifSchemaVersion = "2.1.0"
	SarifSchemaURI     = "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json"
	HitToolName        = "hit-fuzz"
	HitToolVersion     = "0.1.0"
	HitInformationURI  = "https://github.com/hit-endpoint/hit"
)

// SarifDocument represents the root OASIS SARIF v2.1.0 JSON document.
type SarifDocument struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []SarifRun `json:"runs"`
}

type SarifRun struct {
	Tool    SarifTool     `json:"tool"`
	Results []SarifResult `json:"results"`
}

type SarifTool struct {
	Driver SarifDriver `json:"driver"`
}

type SarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version,omitempty"`
	InformationURI string      `json:"informationUri,omitempty"`
	Rules          []SarifRule `json:"rules"`
}

type SarifRule struct {
	ID                   string                  `json:"id"`
	Name                 string                  `json:"name"`
	ShortDescription     SarifText               `json:"shortDescription"`
	FullDescription      SarifText               `json:"fullDescription,omitempty"`
	DefaultConfiguration *SarifRuleConfiguration `json:"defaultConfiguration,omitempty"`
	Properties           map[string]any          `json:"properties,omitempty"`
}

type SarifRuleConfiguration struct {
	Level string `json:"level"` // "error", "warning", "note", "none"
}

type SarifText struct {
	Text string `json:"text"`
}

type SarifResult struct {
	RuleID     string          `json:"ruleId"`
	Level      string          `json:"level"` // "error", "warning", "note"
	Message    SarifText       `json:"message"`
	Locations  []SarifLocation `json:"locations,omitempty"`
	Properties map[string]any  `json:"properties,omitempty"`
}

type SarifLocation struct {
	PhysicalLocation SarifPhysicalLocation `json:"physicalLocation"`
}

type SarifPhysicalLocation struct {
	ArtifactLocation SarifArtifactLocation `json:"artifactLocation"`
}

type SarifArtifactLocation struct {
	URI string `json:"uri"`
}

// Predefined SARIF rules for mutation fuzzing
var fuzzRules = []SarifRule{
	{
		ID:   "HIT-FUZZ-5XX",
		Name: "ServerErrorCrash",
		ShortDescription: SarifText{
			Text: "Server returned unhandled 5xx internal error during mutation fuzzing",
		},
		FullDescription: SarifText{
			Text: "The server endpoint raised an unhandled internal server error (HTTP 5xx) or crashed when supplied with malformed, boundary, or malicious mutation payloads.",
		},
		DefaultConfiguration: &SarifRuleConfiguration{
			Level: "error",
		},
		Properties: map[string]any{
			"tags": []string{"security", "stability", "crash", "5xx"},
		},
	},
	{
		ID:   "HIT-FUZZ-HANG",
		Name: "ServerHangTimeout",
		ShortDescription: SarifText{
			Text: "Server hung or timed out during mutation fuzzing",
		},
		FullDescription: SarifText{
			Text: "The endpoint failed to respond within the configured timeout window when supplied with mutated input, indicating potential denial-of-service, deadlock, or unhandled loop.",
		},
		DefaultConfiguration: &SarifRuleConfiguration{
			Level: "warning",
		},
		Properties: map[string]any{
			"tags": []string{"security", "dos", "timeout"},
		},
	},
	{
		ID:   "HIT-FUZZ-TRANSPORT",
		Name: "TransportAnomaly",
		ShortDescription: SarifText{
			Text: "Connection reset or transport error during mutation fuzzing",
		},
		FullDescription: SarifText{
			Text: "The server abruptly terminated the connection or reset the socket during request processing.",
		},
		DefaultConfiguration: &SarifRuleConfiguration{
			Level: "warning",
		},
		Properties: map[string]any{
			"tags": []string{"stability", "network"},
		},
	},
	{
		ID:   "HIT-FUZZ-ANOMALY",
		Name: "UnexpectedMutationResponse",
		ShortDescription: SarifText{
			Text: "Unexpected response anomaly detected during mutation fuzzing",
		},
		FullDescription: SarifText{
			Text: "The endpoint returned an unexpected status code or response pattern in response to mutation fuzzing.",
		},
		DefaultConfiguration: &SarifRuleConfiguration{
			Level: "note",
		},
		Properties: map[string]any{
			"tags": []string{"quality", "fuzzing"},
		},
	},
}

// BuildFuzzSARIF maps a FuzzResult into a complete OASIS SARIF v2.1.0 document.
func BuildFuzzSARIF(result *fuzz.FuzzResult, targetLocation string) *SarifDocument {
	if targetLocation == "" {
		targetLocation = result.TargetURL
	}

	doc := &SarifDocument{
		Schema:  SarifSchemaURI,
		Version: SarifSchemaVersion,
		Runs: []SarifRun{
			{
				Tool: SarifTool{
					Driver: SarifDriver{
						Name:           HitToolName,
						Version:        HitToolVersion,
						InformationURI: HitInformationURI,
						Rules:          fuzzRules,
					},
				},
				Results: make([]SarifResult, 0, len(result.Anomalies)),
			},
		},
	}

	for _, a := range result.Anomalies {
		ruleID := "HIT-FUZZ-ANOMALY"
		level := "note"

		switch {
		case a.StatusCode >= 500:
			ruleID = "HIT-FUZZ-5XX"
			level = "error"
		case strings.Contains(strings.ToLower(a.Issue), "hang") || strings.Contains(strings.ToLower(a.Issue), "timeout"):
			ruleID = "HIT-FUZZ-HANG"
			level = "warning"
		case strings.Contains(strings.ToLower(a.Issue), "transport"):
			ruleID = "HIT-FUZZ-TRANSPORT"
			level = "warning"
		default:
			if a.Severity == "CRITICAL" {
				level = "error"
			} else if a.Severity == "HIGH" {
				level = "warning"
			}
		}

		msgText := fmt.Sprintf("[%s] %s (Mutation: %s)", a.Category, a.Issue, a.MutationName)
		if a.StatusCode > 0 {
			msgText = fmt.Sprintf("[%s] HTTP %d: %s (Mutation: %s)", a.Category, a.StatusCode, a.Issue, a.MutationName)
		}

		sr := SarifResult{
			RuleID: ruleID,
			Level:  level,
			Message: SarifText{
				Text: msgText,
			},
			Locations: []SarifLocation{
				{
					PhysicalLocation: SarifPhysicalLocation{
						ArtifactLocation: SarifArtifactLocation{
							URI: targetLocation,
						},
					},
				},
			},
			Properties: map[string]any{
				"category":      a.Category,
				"mutation_name": a.MutationName,
				"duration_ms":   a.DurationMs,
				"severity":      a.Severity,
			},
		}

		if a.StatusCode > 0 {
			sr.Properties["status_code"] = a.StatusCode
		}
		if a.ResponseBody != "" {
			sr.Properties["response_preview"] = a.ResponseBody
		}

		doc.Runs[0].Results = append(doc.Runs[0].Results, sr)
	}

	return doc
}

// WriteFuzzSARIF formats the fuzz results into SARIF JSON and writes them to destPath.
func WriteFuzzSARIF(result *fuzz.FuzzResult, targetLocation, destPath string) error {
	doc := BuildFuzzSARIF(result, targetLocation)
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal SARIF JSON: %w", err)
	}

	if dir := filepath.Dir(destPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory for SARIF export: %w", err)
		}
	}

	if err := os.WriteFile(destPath, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("failed to write SARIF export to %s: %w", destPath, err)
	}
	return nil
}
