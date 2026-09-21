package probe

import (
	"crypto/rand"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"hit/internal/flows"
	"hit/internal/runner"
	"hit/internal/spec"
	"hit/internal/types"
)

type ProbeStatus string

const (
	StatusHealthy  ProbeStatus = "HEALTHY"
	StatusDegraded ProbeStatus = "DEGRADED"
	StatusIncident ProbeStatus = "INCIDENT"
)

// ProbeConfig defines the parameters of a synthetic monitoring probe.
type ProbeConfig struct {
	Name               string        `yaml:"name" json:"name"`
	Ref                string        `yaml:"ref" json:"ref"`
	Interval           time.Duration `yaml:"interval" json:"interval"`
	Regions            []string      `yaml:"regions" json:"regions"`
	ConsensusThreshold int           `yaml:"consensus_threshold" json:"consensus_threshold"`
	SLA                ProbeSLA      `yaml:"sla" json:"sla"`
	Alerts             AlertsConfig  `yaml:"alerts" json:"alerts"`
}

type rawProbeConfig struct {
	Name               string       `yaml:"name"`
	Ref                string       `yaml:"ref"`
	Interval           any          `yaml:"interval"`
	Regions            []string     `yaml:"regions"`
	ConsensusThreshold int          `yaml:"consensus_threshold"`
	SLA                rawProbeSLA  `yaml:"sla"`
	Alerts             AlertsConfig `yaml:"alerts"`
}

type rawProbeSLA struct {
	MaxLatency    any   `yaml:"max_latency"`
	AllowedStatus []int `yaml:"allowed_status"`
}

type ProbeSLA struct {
	MaxLatency    time.Duration `yaml:"max_latency" json:"max_latency"`
	AllowedStatus []int         `yaml:"allowed_status" json:"allowed_status"`
}

type AlertsConfig struct {
	PagerDuty *PagerDutyConfig `yaml:"pagerduty,omitempty" json:"pagerduty,omitempty"`
	Slack     *SlackConfig     `yaml:"slack,omitempty" json:"slack,omitempty"`
	Opsgenie  *OpsgenieConfig  `yaml:"opsgenie,omitempty" json:"opsgenie,omitempty"`
	Webhook   *WebhookConfig   `yaml:"webhook,omitempty" json:"webhook,omitempty"`
}

type PagerDutyConfig struct {
	RoutingKey string `yaml:"routing_key" json:"routing_key"`
	Endpoint   string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	Severity   string `yaml:"severity,omitempty" json:"severity,omitempty"`
}

type SlackConfig struct {
	WebhookURL string `yaml:"webhook_url" json:"webhook_url"`
}

type OpsgenieConfig struct {
	APIKey   string `yaml:"api_key" json:"api_key"`
	Endpoint string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	Priority string `yaml:"priority,omitempty" json:"priority,omitempty"`
}

type WebhookConfig struct {
	URL string `yaml:"url" json:"url"`
}

// RegionCheckResult records the outcome of a probe check execution in an individual region.
type RegionCheckResult struct {
	Region           string   `json:"region"`
	StatusCode       int      `json:"status_code"`
	ElapsedMs        float64  `json:"elapsed_ms"`
	Passed           bool     `json:"passed"`
	Error            string   `json:"error,omitempty"`
	FailedAssertions []string `json:"failed_assertions,omitempty"`
}

// ProbeCheckResult aggregates the multi-region execution and consensus status.
type ProbeCheckResult struct {
	ProbeName     string              `json:"probe_name"`
	Timestamp     time.Time           `json:"timestamp"`
	Status        ProbeStatus         `json:"status"`
	PassedRegions int                 `json:"passed_regions"`
	FailedRegions int                 `json:"failed_regions"`
	TotalRegions  int                 `json:"total_regions"`
	Checks        []RegionCheckResult `json:"checks"`
	Summary       string              `json:"summary"`
}

// ProbeState maintains state machine history across consecutive check runs.
type ProbeState struct {
	CurrentStatus       ProbeStatus `json:"current_status"`
	ConsecutiveFailures int         `json:"consecutive_failures"`
	IncidentDedupKey    string      `json:"incident_dedup_key,omitempty"`
	LastChecked         time.Time   `json:"last_checked"`
	LastIncident        time.Time   `json:"last_incident,omitempty"`
	LastResolved        time.Time   `json:"last_resolved,omitempty"`
}

// AlertTransitionEvent describes an alert action taken on a state transition.
type AlertTransitionEvent struct {
	Action      string      `json:"action"` // "TRIGGER", "RESOLVE", "NONE"
	OldStatus   ProbeStatus `json:"old_status"`
	NewStatus   ProbeStatus `json:"new_status"`
	Summary     string      `json:"summary"`
	Notified    []string    `json:"notified"` // List of alert channels successfully notified
	AlertErrors []string    `json:"alert_errors,omitempty"`
}

// DefaultRegions returns the standard simulated or edge probe locations.
func DefaultRegions() []string {
	return []string{"us-east", "eu-central", "ap-southeast"}
}

// LoadProbeConfig reads and parses a YAML probe specification.
func LoadProbeConfig(path string) (*ProbeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read probe file: %w", err)
	}

	var raw rawProbeConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse probe YAML: %w", err)
	}

	cfg := &ProbeConfig{
		Name:               raw.Name,
		Ref:                raw.Ref,
		Regions:            raw.Regions,
		ConsensusThreshold: raw.ConsensusThreshold,
		Alerts:             raw.Alerts,
	}

	if len(cfg.Regions) == 0 {
		cfg.Regions = DefaultRegions()
	}
	if cfg.ConsensusThreshold <= 0 {
		cfg.ConsensusThreshold = 2
	}
	if cfg.ConsensusThreshold > len(cfg.Regions) {
		cfg.ConsensusThreshold = len(cfg.Regions)
	}

	// Parse Interval
	if raw.Interval != nil {
		switch iv := raw.Interval.(type) {
		case string:
			if d, err := time.ParseDuration(iv); err == nil {
				cfg.Interval = d
			}
		case int:
			cfg.Interval = time.Duration(iv) * time.Second
		}
	}
	if cfg.Interval == 0 {
		cfg.Interval = 60 * time.Second
	}

	// Parse SLA
	if raw.SLA.MaxLatency != nil {
		switch ml := raw.SLA.MaxLatency.(type) {
		case string:
			if d, err := time.ParseDuration(ml); err == nil {
				cfg.SLA.MaxLatency = d
			}
		case int:
			cfg.SLA.MaxLatency = time.Duration(ml) * time.Millisecond
		}
	}
	cfg.SLA.AllowedStatus = raw.SLA.AllowedStatus

	return cfg, nil
}

// EvaluateConsensus determines if failures represent DEGRADED or a confirmed INCIDENT.
func EvaluateConsensus(checks []RegionCheckResult, threshold int) ProbeStatus {
	failed := 0
	for _, c := range checks {
		if !c.Passed {
			failed++
		}
	}

	if failed == 0 {
		return StatusHealthy
	}
	if failed >= threshold {
		return StatusIncident
	}
	return StatusDegraded
}

// ExecuteProbeCheck runs the probe against target across configured regions.
func ExecuteProbeCheck(sess *runner.Session, cfg *ProbeConfig) *ProbeCheckResult {
	res := &ProbeCheckResult{
		ProbeName:    cfg.Name,
		Timestamp:    time.Now().UTC(),
		TotalRegions: len(cfg.Regions),
		Checks:       make([]RegionCheckResult, 0, len(cfg.Regions)),
	}

	for _, region := range cfg.Regions {
		// Add regional metadata header
		overrides := map[string]any{
			"headers": map[string]string{
				"X-Hit-Probe-Region": region,
			},
		}

		check := RegionCheckResult{
			Region: region,
			Passed: true,
		}

		var runResult *types.Result

		// Check if ref is chain or request
		if sess.Zone != nil {
			if chainPath, err := sess.Zone.ResolveChain(cfg.Ref); err == nil && chainPath != "" {
				if flowData, err := flows.LoadFlow(chainPath); err == nil {
					fr := flows.RunFlow(sess, flowData, nil, nil, true, 0)
					if fr.Error != "" {
						check.Passed = false
						check.Error = fr.Error
					} else if len(fr.Results) > 0 {
						runResult = fr.Results[len(fr.Results)-1]
					}
				}
			}
		}

		if runResult == nil {
			if strings.Contains(cfg.Ref, "://") {
				regHeaders := map[string]any{
					"X-Hit-Probe-Region": region,
				}
				runResult = sess.Request("GET", cfg.Ref, regHeaders, nil, nil, nil, nil, nil, nil, nil, nil, "probe")
			} else {
				sp, err := sess.Load(cfg.Ref)
				if err != nil {
					// Fallback to loading file directly
					if directSp, dErr := spec.LoadSpec(cfg.Ref, nil); dErr == nil {
						runResult = sess.RunSpec(directSp, overrides)
					} else {
						check.Passed = false
						check.Error = err.Error()
					}
				} else {
					runResult = sess.RunSpec(sp, overrides)
				}
			}
		}

		if runResult != nil {
			check.StatusCode = runResult.Status
			check.ElapsedMs = math.Round(runResult.ElapsedMs*10) / 10
			if !runResult.OK() {
				check.Passed = false
				if runResult.Error != "" {
					check.Error = runResult.Error
				}
				for _, ft := range runResult.FailedTests() {
					check.FailedAssertions = append(check.FailedAssertions, fmt.Sprintf("%s: %s", ft.Name, ft.Detail))
				}
			}

			// Check custom SLA criteria
			if cfg.SLA.MaxLatency > 0 {
				slaMs := float64(cfg.SLA.MaxLatency.Milliseconds())
				if check.ElapsedMs > slaMs {
					check.Passed = false
					check.FailedAssertions = append(check.FailedAssertions, fmt.Sprintf("Latency %.1fms exceeded SLA ceiling %.0fms", check.ElapsedMs, slaMs))
				}
			}

			if len(cfg.SLA.AllowedStatus) > 0 {
				statusAllowed := false
				for _, allowed := range cfg.SLA.AllowedStatus {
					if check.StatusCode == allowed {
						statusAllowed = true
						break
					}
				}
				if !statusAllowed {
					check.Passed = false
					check.FailedAssertions = append(check.FailedAssertions, fmt.Sprintf("HTTP %d not in allowed statuses %v", check.StatusCode, cfg.SLA.AllowedStatus))
				}
			}
		}

		if check.Passed {
			res.PassedRegions++
		} else {
			res.FailedRegions++
		}

		res.Checks = append(res.Checks, check)
	}

	res.Status = EvaluateConsensus(res.Checks, cfg.ConsensusThreshold)

	switch res.Status {
	case StatusHealthy:
		res.Summary = fmt.Sprintf("Healthy: All %d regions passed", res.TotalRegions)
	case StatusDegraded:
		res.Summary = fmt.Sprintf("Degraded: %d of %d regions failed (consensus threshold %d not met)", res.FailedRegions, res.TotalRegions, cfg.ConsensusThreshold)
	case StatusIncident:
		res.Summary = fmt.Sprintf("Incident: %d of %d regions failed (consensus threshold %d breached)", res.FailedRegions, res.TotalRegions, cfg.ConsensusThreshold)
	}

	return res
}

func generateDedupKey(probeName string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	cleanName := strings.ReplaceAll(strings.ToLower(probeName), " ", "-")
	return fmt.Sprintf("hit-%s-%x", cleanName, b)
}

// HandleStateTransition processes state transitions and dispatches incident trigger or resolve alerts.
func HandleStateTransition(state *ProbeState, checkResult *ProbeCheckResult, cfg *ProbeConfig, client *http.Client) (*AlertTransitionEvent, error) {
	if state == nil {
		state = &ProbeState{CurrentStatus: StatusHealthy}
	}

	oldStatus := state.CurrentStatus
	newStatus := checkResult.Status
	state.CurrentStatus = newStatus
	state.LastChecked = checkResult.Timestamp

	event := &AlertTransitionEvent{
		Action:    "NONE",
		OldStatus: oldStatus,
		NewStatus: newStatus,
		Summary:   checkResult.Summary,
	}

	// 1. Transition into INCIDENT
	if newStatus == StatusIncident && oldStatus != StatusIncident {
		state.ConsecutiveFailures++
		state.LastIncident = checkResult.Timestamp
		if state.IncidentDedupKey == "" {
			state.IncidentDedupKey = generateDedupKey(cfg.Name)
		}

		event.Action = "TRIGGER"
		event.Summary = fmt.Sprintf("Production Incident: %s is FAILING (%s)", cfg.Name, checkResult.Summary)

		var failedDetails []string
		for _, c := range checkResult.Checks {
			if !c.Passed {
				errStr := c.Error
				if len(c.FailedAssertions) > 0 {
					errStr = strings.Join(c.FailedAssertions, ", ")
				}
				failedDetails = append(failedDetails, fmt.Sprintf("• [%s] HTTP %d (%.1fms): %s", c.Region, c.StatusCode, c.ElapsedMs, errStr))
			}
		}

		// Dispatch PagerDuty
		if cfg.Alerts.PagerDuty != nil && cfg.Alerts.PagerDuty.RoutingKey != "" {
			severity := cfg.Alerts.PagerDuty.Severity
			if severity == "" {
				severity = "critical"
			}
			customDetails := map[string]any{
				"probe":          cfg.Name,
				"ref":            cfg.Ref,
				"failed_regions": checkResult.FailedRegions,
				"total_regions":  checkResult.TotalRegions,
				"failures":       failedDetails,
			}
			if err := SendPagerDuty(client, cfg.Alerts.PagerDuty.Endpoint, cfg.Alerts.PagerDuty.RoutingKey, "trigger", state.IncidentDedupKey, event.Summary, "hit-probe", severity, customDetails); err != nil {
				event.AlertErrors = append(event.AlertErrors, fmt.Sprintf("PagerDuty: %v", err))
			} else {
				event.Notified = append(event.Notified, "PagerDuty")
			}
		}

		// Dispatch Slack
		if cfg.Alerts.Slack != nil && cfg.Alerts.Slack.WebhookURL != "" {
			if err := SendSlack(client, cfg.Alerts.Slack.WebhookURL, cfg.Name, true, failedDetails); err != nil {
				event.AlertErrors = append(event.AlertErrors, fmt.Sprintf("Slack: %v", err))
			} else {
				event.Notified = append(event.Notified, "Slack")
			}
		}

		// Dispatch Opsgenie
		if cfg.Alerts.Opsgenie != nil && cfg.Alerts.Opsgenie.APIKey != "" {
			desc := strings.Join(failedDetails, "\n")
			if err := SendOpsgenie(client, cfg.Alerts.Opsgenie.Endpoint, cfg.Alerts.Opsgenie.APIKey, "create", state.IncidentDedupKey, event.Summary, desc, cfg.Alerts.Opsgenie.Priority); err != nil {
				event.AlertErrors = append(event.AlertErrors, fmt.Sprintf("Opsgenie: %v", err))
			} else {
				event.Notified = append(event.Notified, "Opsgenie")
			}
		}
	}

	// 2. Transition back to HEALTHY (Resolution)
	if newStatus == StatusHealthy && oldStatus == StatusIncident {
		state.LastResolved = checkResult.Timestamp
		event.Action = "RESOLVE"
		event.Summary = fmt.Sprintf("Incident Resolved: %s has RECOVERED and is now healthy", cfg.Name)

		resolveDetails := []string{
			fmt.Sprintf("• All %d regions successfully verified passing status and assertions.", checkResult.TotalRegions),
			fmt.Sprintf("• Resolution timestamp: %s", checkResult.Timestamp.Format(time.RFC3339)),
		}

		// Resolve PagerDuty
		if cfg.Alerts.PagerDuty != nil && cfg.Alerts.PagerDuty.RoutingKey != "" && state.IncidentDedupKey != "" {
			if err := SendPagerDuty(client, cfg.Alerts.PagerDuty.Endpoint, cfg.Alerts.PagerDuty.RoutingKey, "resolve", state.IncidentDedupKey, event.Summary, "hit-probe", "info", nil); err != nil {
				event.AlertErrors = append(event.AlertErrors, fmt.Sprintf("PagerDuty: %v", err))
			} else {
				event.Notified = append(event.Notified, "PagerDuty (Resolved)")
			}
		}

		// Resolve Slack
		if cfg.Alerts.Slack != nil && cfg.Alerts.Slack.WebhookURL != "" {
			if err := SendSlack(client, cfg.Alerts.Slack.WebhookURL, cfg.Name, false, resolveDetails); err != nil {
				event.AlertErrors = append(event.AlertErrors, fmt.Sprintf("Slack: %v", err))
			} else {
				event.Notified = append(event.Notified, "Slack (Resolved)")
			}
		}

		// Resolve Opsgenie
		if cfg.Alerts.Opsgenie != nil && cfg.Alerts.Opsgenie.APIKey != "" && state.IncidentDedupKey != "" {
			if err := SendOpsgenie(client, cfg.Alerts.Opsgenie.Endpoint, cfg.Alerts.Opsgenie.APIKey, "resolve", state.IncidentDedupKey, event.Summary, "Recovered", ""); err != nil {
				event.AlertErrors = append(event.AlertErrors, fmt.Sprintf("Opsgenie: %v", err))
			} else {
				event.Notified = append(event.Notified, "Opsgenie (Resolved)")
			}
		}

		// Reset incident dedup key
		state.IncidentDedupKey = ""
		state.ConsecutiveFailures = 0
	}

	return event, nil
}
