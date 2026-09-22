package policy

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"github.com/hit-endpoint/hit-endpoint/internal/spec"
	"github.com/hit-endpoint/hit-endpoint/internal/zone"
)

// PolicyConfig holds the declarative policy governance rules.
type PolicyConfig struct {
	Version                 string        `yaml:"version,omitempty" json:"version,omitempty"`
	RequireAssertions       []string      `yaml:"require_assertions,omitempty" json:"require_assertions,omitempty"`
	MaxLatency              time.Duration `yaml:"max_latency,omitempty" json:"max_latency,omitempty"`
	RequireAuth             bool          `yaml:"require_auth,omitempty" json:"require_auth,omitempty"`
	DisallowInsecureTLS     bool          `yaml:"disallow_insecure_tls,omitempty" json:"disallow_insecure_tls,omitempty"`
	DisallowHardcodedSecrets bool         `yaml:"disallow_hardcoded_secrets,omitempty" json:"disallow_hardcoded_secrets,omitempty"`
	RequireDescription      bool          `yaml:"require_description,omitempty" json:"require_description,omitempty"`
}

// rawPolicyConfig is used for flexible unmarshaling of durations and YAML variations.
type rawPolicyConfig struct {
	Version                  string   `yaml:"version"`
	RequireAssertions        []string `yaml:"require_assertions"`
	MaxLatency               any      `yaml:"max_latency"`
	RequireAuth              bool     `yaml:"require_auth"`
	DisallowInsecureTLS      bool     `yaml:"disallow_insecure_tls"`
	DisallowHardcodedSecrets bool     `yaml:"disallow_hardcoded_secrets"`
	RequireDescription       bool     `yaml:"require_description"`
}

// Severity represents the severity of a policy violation.
type Severity string

const (
	SeverityError Severity = "ERROR"
	SeverityWarn  Severity = "WARN"
)

// Violation describes a specific rule breach in an endpoint or collection file.
type Violation struct {
	File        string   `json:"file"`
	Rule        string   `json:"rule"`
	Severity    Severity `json:"severity"`
	Message     string   `json:"message"`
	Remediation string   `json:"remediation,omitempty"`
}

// PolicyReport represents the aggregated results of a policy audit.
type PolicyReport struct {
	ZoneRoot   string      `json:"zone_root"`
	Scope      string      `json:"scope"`
	TotalFiles int         `json:"total_files"`
	Passed     bool        `json:"passed"`
	Violations []Violation `json:"violations"`
	AuditTime  string      `json:"audit_time"`
}

// DefaultPolicy provides sensible default governance rules.
func DefaultPolicy() *PolicyConfig {
	return &PolicyConfig{
		Version:                  "1",
		RequireAssertions:        []string{"status"},
		MaxLatency:               2000 * time.Millisecond,
		DisallowInsecureTLS:      true,
		DisallowHardcodedSecrets: true,
	}
}

// LoadPolicy discovers and parses policy definitions from a zone.
func LoadPolicy(zoneRoot string) (*PolicyConfig, error) {
	candidates := []string{
		filepath.Join(zoneRoot, ".hit", "policy.yaml"),
		filepath.Join(zoneRoot, ".hit", "policy.yml"),
		filepath.Join(zoneRoot, "policy.yaml"),
		filepath.Join(zoneRoot, "policy.yml"),
	}

	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			data, err := os.ReadFile(c)
			if err != nil {
				return nil, fmt.Errorf("failed to read policy file %s: %w", c, err)
			}
			return ParsePolicyYAML(data)
		}
	}

	// Also check zone.yaml for embedded "policy:" mapping
	zoneConfigFile := filepath.Join(zoneRoot, "zone.yaml")
	if fi, err := os.Stat(zoneConfigFile); err == nil && !fi.IsDir() {
		data, err := os.ReadFile(zoneConfigFile)
		if err == nil {
			var wrapper struct {
				Policy *rawPolicyConfig `yaml:"policy"`
			}
			if err := yaml.Unmarshal(data, &wrapper); err == nil && wrapper.Policy != nil {
				return convertRawPolicy(wrapper.Policy)
			}
		}
	}

	// Fallback to default policy if none explicitly defined
	return DefaultPolicy(), nil
}

// ParsePolicyYAML parses raw YAML bytes into a PolicyConfig.
func ParsePolicyYAML(data []byte) (*PolicyConfig, error) {
	var raw rawPolicyConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		// Also try parsing wrapped under `policy:`
		var wrapper struct {
			Policy rawPolicyConfig `yaml:"policy"`
		}
		if err2 := yaml.Unmarshal(data, &wrapper); err2 == nil && len(wrapper.Policy.RequireAssertions) > 0 {
			return convertRawPolicy(&wrapper.Policy)
		}
		return nil, fmt.Errorf("invalid policy YAML format: %w", err)
	}
	return convertRawPolicy(&raw)
}

func convertRawPolicy(raw *rawPolicyConfig) (*PolicyConfig, error) {
	cfg := &PolicyConfig{
		Version:                  raw.Version,
		RequireAssertions:        raw.RequireAssertions,
		RequireAuth:              raw.RequireAuth,
		DisallowInsecureTLS:      raw.DisallowInsecureTLS,
		DisallowHardcodedSecrets: raw.DisallowHardcodedSecrets,
		RequireDescription:       raw.RequireDescription,
	}

	if raw.MaxLatency != nil {
		switch v := raw.MaxLatency.(type) {
		case string:
			d, err := time.ParseDuration(v)
			if err != nil {
				// try parse as ms number string
				if ms, err2 := strconv.ParseFloat(v, 64); err2 == nil {
					cfg.MaxLatency = time.Duration(ms * float64(time.Millisecond))
				} else {
					return nil, fmt.Errorf("invalid max_latency duration '%s': %w", v, err)
				}
			} else {
				cfg.MaxLatency = d
			}
		case int:
			cfg.MaxLatency = time.Duration(v) * time.Millisecond
		case float64:
			cfg.MaxLatency = time.Duration(v * float64(time.Millisecond))
		}
	}

	return cfg, nil
}

// Regex helpers for secret detection
var (
	varRegex        = regexp.MustCompile(`\{\{[^}]+\}\}|\$env:[A-Za-z0-9_]+`)
	jwtRegex        = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)
	rawAuthHeader   = regexp.MustCompile(`(?i)^(Bearer|Basic)\s+([A-Za-z0-9+/=._-]{16,})$`)
	suspiciousKeyRe = regexp.MustCompile(`(?i)(api[_-]?key|secret|token|password|passwd)`)
)

// CheckRequestSpec evaluates an individual request specification against policy rules.
func CheckRequestSpec(relPath string, sp *spec.RequestSpec, rawContent string, cfg *PolicyConfig) []Violation {
	var violations []Violation

	if cfg == nil {
		return violations
	}

	// 1. Description requirement
	if cfg.RequireDescription && strings.TrimSpace(sp.Description) == "" && strings.TrimSpace(sp.Name) == "" {
		violations = append(violations, Violation{
			File:        relPath,
			Rule:        "require_description",
			Severity:    SeverityWarn,
			Message:     "Request lacks a 'description' or descriptive 'name'",
			Remediation: "Add a 'description: ...' field explaining endpoint purpose",
		})
	}

	// 2. Insecure TLS verification
	if cfg.DisallowInsecureTLS {
		if vBool, ok := sp.Verify.(bool); ok && !vBool {
			violations = append(violations, Violation{
				File:        relPath,
				Rule:        "disallow_insecure_tls",
				Severity:    SeverityError,
				Message:     "Insecure TLS verification explicitly enabled ('verify: false')",
				Remediation: "Remove 'verify: false' or use a valid certificate authority bundle",
			})
		}
	}

	// 3. Required assertions
	hasStatusAssert := false
	hasLatencyAssert := false
	hasBodyAssert := false

	for _, t := range sp.Tests {
		if _, ok := t["status"]; ok {
			hasStatusAssert = true
		}
		if _, ok := t["max_ms"]; ok {
			hasLatencyAssert = true
		}
		if _, ok := t["latency"]; ok {
			hasLatencyAssert = true
		}
		if _, ok := t["json"]; ok {
			hasBodyAssert = true
		}
		if _, ok := t["body"]; ok {
			hasBodyAssert = true
		}
		if expr, ok := t["expr"].(string); ok {
			if strings.Contains(expr, "status") {
				hasStatusAssert = true
			}
			if strings.Contains(expr, "latency") || strings.Contains(expr, "ms") {
				hasLatencyAssert = true
			}
		}
	}

	for _, req := range cfg.RequireAssertions {
		switch strings.ToLower(req) {
		case "status":
			if !hasStatusAssert {
				violations = append(violations, Violation{
					File:        relPath,
					Rule:        "require_assertions.status",
					Severity:    SeverityError,
					Message:     "Missing required HTTP status code assertion in 'tests'",
					Remediation: "Add '- status: 200' under 'tests:'",
				})
			}
		case "latency", "max_ms":
			if !hasLatencyAssert {
				violations = append(violations, Violation{
					File:        relPath,
					Rule:        "require_assertions.latency",
					Severity:    SeverityError,
					Message:     "Missing required latency threshold assertion in 'tests'",
					Remediation: "Add '- max_ms: 1000' or '- latency: < 500ms' under 'tests:'",
				})
			}
		case "body", "json":
			if !hasBodyAssert {
				violations = append(violations, Violation{
					File:        relPath,
					Rule:        "require_assertions.body",
					Severity:    SeverityWarn,
					Message:     "Missing required body/payload assertion in 'tests'",
					Remediation: "Add '- json: ...' under 'tests:'",
				})
			}
		}
	}

	// 4. Max latency ceiling
	if cfg.MaxLatency > 0 {
		maxAllowedMs := float64(cfg.MaxLatency.Milliseconds())
		for _, t := range sp.Tests {
			var specifiedMs float64
			if mVal, ok := t["max_ms"]; ok {
				switch mv := mVal.(type) {
				case int:
					specifiedMs = float64(mv)
				case float64:
					specifiedMs = mv
				case string:
					if d, err := time.ParseDuration(mv); err == nil {
						specifiedMs = float64(d.Milliseconds())
					}
				}
			}
			if specifiedMs > maxAllowedMs {
				violations = append(violations, Violation{
					File:        relPath,
					Rule:        "max_latency",
					Severity:    SeverityError,
					Message:     fmt.Sprintf("Asserted latency threshold (%.0fms) exceeds policy ceiling (%.0fms)", specifiedMs, maxAllowedMs),
					Remediation: fmt.Sprintf("Lower 'max_ms' to be <= %.0fms", maxAllowedMs),
				})
			}
		}
	}

	// 5. Auth requirement
	if cfg.RequireAuth {
		hasAuth := sp.Auth != nil && sp.Auth != "none"
		if !hasAuth {
			for k := range sp.Headers {
				if strings.EqualFold(k, "Authorization") || strings.EqualFold(k, "X-API-Key") {
					hasAuth = true
					break
				}
			}
		}
		if !hasAuth {
			violations = append(violations, Violation{
				File:        relPath,
				Rule:        "require_auth",
				Severity:    SeverityError,
				Message:     "Endpoint does not define authentication or authorization header",
				Remediation: "Specify 'auth:' or pass an 'Authorization' header",
			})
		}
	}

	// 6. Hardcoded secrets hygiene
	if cfg.DisallowHardcodedSecrets {
		// Scan headers for static tokens
		for k, v := range sp.Headers {
			vStr := fmt.Sprintf("%v", v)
			if strings.EqualFold(k, "Authorization") {
				if m := rawAuthHeader.FindStringSubmatch(vStr); len(m) > 2 {
					tokenVal := m[2]
					if !varRegex.MatchString(tokenVal) {
						violations = append(violations, Violation{
							File:        relPath,
							Rule:        "disallow_hardcoded_secrets",
							Severity:    SeverityError,
							Message:     "Hardcoded plaintext bearer/basic token in 'Authorization' header",
							Remediation: "Replace static token with '{{token}}' or '$env:AUTH_TOKEN'",
						})
					}
				}
			} else if suspiciousKeyRe.MatchString(k) {
				if len(vStr) >= 16 && !varRegex.MatchString(vStr) {
					violations = append(violations, Violation{
						File:        relPath,
						Rule:        "disallow_hardcoded_secrets",
						Severity:    SeverityError,
						Message:     fmt.Sprintf("Potential hardcoded secret in header '%s'", k),
						Remediation: fmt.Sprintf("Store value in .secrets.yaml and use '{{%s}}'", strings.ToLower(k)),
					})
				}
			}
		}

		// Scan auth mapping
		if authMap, ok := sp.Auth.(map[string]any); ok {
			for ak, av := range authMap {
				if suspiciousKeyRe.MatchString(ak) {
					avStr := fmt.Sprintf("%v", av)
					if len(avStr) > 0 && !varRegex.MatchString(avStr) && avStr != "none" {
						violations = append(violations, Violation{
							File:        relPath,
							Rule:        "disallow_hardcoded_secrets",
							Severity:    SeverityError,
							Message:     fmt.Sprintf("Hardcoded secret in auth field '%s'", ak),
							Remediation: "Use dynamic template '{{password}}' or '$env:VAR'",
						})
					}
				}
			}
		}

		// Raw text scans for JWT tokens
		if rawContent != "" && jwtRegex.MatchString(rawContent) && !varRegex.MatchString(rawContent) {
			violations = append(violations, Violation{
				File:        relPath,
				Rule:        "disallow_hardcoded_secrets",
				Severity:    SeverityError,
				Message:     "Detected hardcoded JWT token in request file",
				Remediation: "Never commit JWTs to repository; use dynamic login chain or .secrets.yaml",
			})
		}
	}

	return violations
}

// AuditZone executes a policy audit across all request files and scenario chains in a zone.
func AuditZone(z *zone.Zone, scope string, cfg *PolicyConfig) (*PolicyReport, error) {
	if cfg == nil {
		var err error
		cfg, err = LoadPolicy(z.Root)
		if err != nil {
			return nil, err
		}
	}

	scopeName := scope
	if scopeName == "" {
		scopeName = "all collections"
	}

	report := &PolicyReport{
		ZoneRoot:  z.Root,
		Scope:     scopeName,
		Passed:    true,
		AuditTime: time.Now().UTC().Format(time.RFC3339),
	}

	var requestPaths []string
	if scope != "" {
		target, err := z.ResolveRequest(scope)
		if err == nil {
			fi, err := os.Stat(target)
			if err == nil && fi.IsDir() {
				requestPaths = z.ListRequests(target)
			} else {
				requestPaths = []string{target}
			}
		} else {
			requestPaths = z.ListRequests("")
		}
	} else {
		requestPaths = z.ListRequests("")
	}

	report.TotalFiles = len(requestPaths)

	for _, path := range requestPaths {
		relPath, _ := filepath.Rel(z.Root, path)
		if relPath == "" {
			relPath = filepath.Base(path)
		}

		rawBytes, _ := os.ReadFile(path)
		rawContent := string(rawBytes)

		sp, err := spec.LoadSpec(path, z.DefaultsChain(path))
		if err != nil {
			report.Violations = append(report.Violations, Violation{
				File:        relPath,
				Rule:        "valid_yaml_spec",
				Severity:    SeverityError,
				Message:     fmt.Sprintf("Failed to parse request YAML: %v", err),
				Remediation: "Ensure file syntax matches hit request specification",
			})
			report.Passed = false
			continue
		}

		vList := CheckRequestSpec(relPath, sp, rawContent, cfg)
		for _, v := range vList {
			if v.Severity == SeverityError {
				report.Passed = false
			}
			report.Violations = append(report.Violations, v)
		}
	}

	return report, nil
}

// JUnit export representation for policy audit
type policyJUnitSuites struct {
	XMLName  xml.Name           `xml:"testsuites"`
	Name     string             `xml:"name,attr"`
	Tests    int                `xml:"tests,attr"`
	Failures int                `xml:"failures,attr"`
	Errors   int                `xml:"errors,attr"`
	Suites   []policyJUnitSuite `xml:"testsuite"`
}

type policyJUnitSuite struct {
	XMLName  xml.Name          `xml:"testsuite"`
	Name     string            `xml:"name,attr"`
	Tests    int               `xml:"tests,attr"`
	Failures int               `xml:"failures,attr"`
	Cases    []policyJUnitCase `xml:"testcase"`
}

type policyJUnitCase struct {
	XMLName   xml.Name             `xml:"testcase"`
	Name      string               `xml:"name,attr"`
	ClassName string               `xml:"classname,attr"`
	Failure   *policyJUnitFailure  `xml:"failure,omitempty"`
}

type policyJUnitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Content string `xml:",chardata"`
}

// WriteJUnitReport exports the policy report as standard JUnit XML.
func (r *PolicyReport) WriteJUnitReport(destPath string) error {
	root := policyJUnitSuites{
		Name:  "hit-policy",
		Tests: r.TotalFiles,
	}

	suite := policyJUnitSuite{
		Name: "policy-governance",
	}

	for _, v := range r.Violations {
		if v.Severity == SeverityError {
			root.Failures++
			suite.Failures++
		}
		suite.Tests++
		tc := policyJUnitCase{
			Name:      fmt.Sprintf("%s: %s", v.Rule, v.File),
			ClassName: v.File,
			Failure: &policyJUnitFailure{
				Message: v.Message,
				Type:    string(v.Severity),
				Content: fmt.Sprintf("Rule '%s' violated in %s: %s (Fix: %s)", v.Rule, v.File, v.Message, v.Remediation),
			},
		}
		suite.Cases = append(suite.Cases, tc)
	}

	if len(suite.Cases) == 0 {
		suite.Tests = 1
		root.Tests = 1
		suite.Cases = append(suite.Cases, policyJUnitCase{
			Name:      "Policy compliance check",
			ClassName: "policy",
		})
	}

	root.Suites = append(root.Suites, suite)

	data, err := xml.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to generate policy junit xml: %w", err)
	}

	content := []byte(xml.Header + string(data) + "\n")
	if dir := filepath.Dir(destPath); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}
	return os.WriteFile(destPath, content, 0644)
}
