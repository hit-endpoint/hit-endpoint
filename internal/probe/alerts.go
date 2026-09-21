package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	DefaultPagerDutyEndpoint = "https://events.pagerduty.com/v2/enqueue"
	DefaultOpsgenieEndpoint  = "https://api.opsgenie.com/v2/alerts"
)

// ResolveSecret resolves environment variable placeholders like $env:VAR or returns the string.
func ResolveSecret(val string) string {
	val = strings.TrimSpace(val)
	if strings.HasPrefix(val, "$env:") {
		envName := strings.TrimPrefix(val, "$env:")
		return os.Getenv(envName)
	}
	return val
}

// PagerDutyPayload represents the standard Events API v2 payload.
type PagerDutyPayload struct {
	RoutingKey  string                 `json:"routing_key"`
	EventAction string                 `json:"event_action"` // "trigger", "acknowledge", "resolve"
	DedupKey    string                 `json:"dedup_key,omitempty"`
	Payload     PagerDutyEventDetail   `json:"payload"`
}

type PagerDutyEventDetail struct {
	Summary   string                 `json:"summary"`
	Source    string                 `json:"source"`
	Severity  string                 `json:"severity"` // "critical", "error", "warning", "info"
	Timestamp string                 `json:"timestamp"`
	CustomDetails map[string]any     `json:"custom_details,omitempty"`
}

// SendPagerDuty dispatches an event to the PagerDuty Events API v2.
func SendPagerDuty(client *http.Client, endpoint, routingKey, action, dedupKey, summary, source, severity string, customDetails map[string]any) error {
	resolvedKey := ResolveSecret(routingKey)
	if resolvedKey == "" {
		return fmt.Errorf("pagerduty routing key is empty or unresolved")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	if endpoint == "" {
		endpoint = DefaultPagerDutyEndpoint
	}

	payload := PagerDutyPayload{
		RoutingKey:  resolvedKey,
		EventAction: action,
		DedupKey:    dedupKey,
		Payload: PagerDutyEventDetail{
			Summary:       summary,
			Source:        source,
			Severity:      severity,
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
			CustomDetails: customDetails,
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal pagerduty payload: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create pagerduty request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "hit/0.1.0 (synthetic-probe)")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("pagerduty request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("pagerduty returned HTTP %d: %s", resp.StatusCode, string(snippet))
	}

	return nil
}

// SlackWebhookPayload formats a rich Slack notification.
type SlackWebhookPayload struct {
	Text        string            `json:"text"`
	Blocks      []SlackBlock      `json:"blocks,omitempty"`
	Attachments []SlackAttachment `json:"attachments,omitempty"`
}

type SlackBlock struct {
	Type string    `json:"type"`
	Text *SlackText `json:"text,omitempty"`
}

type SlackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type SlackAttachment struct {
	Color  string       `json:"color"`
	Blocks []SlackBlock `json:"blocks,omitempty"`
}

// SendSlack sends a Block Kit notification card to a Slack incoming webhook.
func SendSlack(client *http.Client, webhookURL, summary string, isIncident bool, details []string) error {
	resolvedURL := ResolveSecret(webhookURL)
	if resolvedURL == "" {
		return fmt.Errorf("slack webhook URL is empty or unresolved")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	headerText := fmt.Sprintf("🚨 *Incident Triggered:* %s", summary)
	color := "#E01E5A" // Red
	if !isIncident {
		headerText = fmt.Sprintf("✅ *Incident Resolved:* %s", summary)
		color = "#2EB886" // Green
	}

	bodyText := strings.Join(details, "\n")
	if bodyText == "" {
		bodyText = "Status transition verified by hit synthetic probe."
	}

	payload := SlackWebhookPayload{
		Text: headerText,
		Attachments: []SlackAttachment{
			{
				Color: color,
				Blocks: []SlackBlock{
					{
						Type: "section",
						Text: &SlackText{
							Type: "mrkdwn",
							Text: fmt.Sprintf("%s\n\n%s", headerText, bodyText),
						},
					},
					{
						Type: "context",
						Text: &SlackText{
							Type: "mrkdwn",
							Text: fmt.Sprintf("Engine: *hit synthetic probes* | Time: %s", time.Now().UTC().Format(time.RFC3339)),
						},
					},
				},
			},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal slack payload: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resolvedURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("slack webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("slack returned HTTP %d: %s", resp.StatusCode, string(snippet))
	}

	return nil
}

// OpsgenieCreateAlertPayload represents the standard Opsgenie alert creation model.
type OpsgenieCreateAlertPayload struct {
	Message     string            `json:"message"`
	Alias       string            `json:"alias,omitempty"`
	Description string            `json:"description,omitempty"`
	Priority    string            `json:"priority,omitempty"` // "P1", "P2", "P3"
	Source      string            `json:"source,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Details     map[string]string `json:"details,omitempty"`
}

// SendOpsgenie triggers or closes alerts in Opsgenie via the Alerts API v2.
func SendOpsgenie(client *http.Client, endpoint, apiKey, action, alias, message, description, priority string) error {
	resolvedKey := ResolveSecret(apiKey)
	if resolvedKey == "" {
		return fmt.Errorf("opsgenie API key is empty or unresolved")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	if endpoint == "" {
		endpoint = DefaultOpsgenieEndpoint
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var req *http.Request
	var err error

	if strings.ToLower(action) == "resolve" || strings.ToLower(action) == "close" {
		closeURL := fmt.Sprintf("%s/%s/close?identifierType=alias", strings.TrimRight(endpoint, "/"), alias)
		closePayload := map[string]string{
			"source": "hit-synthetic-probe",
			"note":   "Auto-resolved by hit synthetic probe",
		}
		data, _ := json.Marshal(closePayload)
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, closeURL, bytes.NewReader(data))
	} else {
		if priority == "" {
			priority = "P1"
		}
		createPayload := OpsgenieCreateAlertPayload{
			Message:     message,
			Alias:       alias,
			Description: description,
			Priority:    priority,
			Source:      "hit-synthetic-probe",
			Tags:        []string{"hit", "synthetic-probe", "api"},
		}
		data, _ := json.Marshal(createPayload)
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	}

	if err != nil {
		return fmt.Errorf("failed to create opsgenie request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("GenieKey %s", resolvedKey))

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("opsgenie request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("opsgenie returned HTTP %d: %s", resp.StatusCode, string(snippet))
	}

	return nil
}
