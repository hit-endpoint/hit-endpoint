package probe

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

type roundTripFunc func(req *http.Request) *http.Response

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}

func TestEvaluateConsensus(t *testing.T) {
	// 1. All passing -> Healthy
	c1 := []RegionCheckResult{
		{Region: "us-east", Passed: true},
		{Region: "eu-central", Passed: true},
		{Region: "ap-southeast", Passed: true},
	}
	if s := EvaluateConsensus(c1, 2); s != StatusHealthy {
		t.Errorf("expected HEALTHY, got %s", s)
	}

	// 2. 1 failure with threshold 2 -> Degraded (no false alarm)
	c2 := []RegionCheckResult{
		{Region: "us-east", Passed: false},
		{Region: "eu-central", Passed: true},
		{Region: "ap-southeast", Passed: true},
	}
	if s := EvaluateConsensus(c2, 2); s != StatusDegraded {
		t.Errorf("expected DEGRADED for single region failure, got %s", s)
	}

	// 3. 2 failures with threshold 2 -> Incident
	c3 := []RegionCheckResult{
		{Region: "us-east", Passed: false},
		{Region: "eu-central", Passed: false},
		{Region: "ap-southeast", Passed: true},
	}
	if s := EvaluateConsensus(c3, 2); s != StatusIncident {
		t.Errorf("expected INCIDENT for 2 region failures, got %s", s)
	}
}

func TestHandleStateTransition_TriggerAndResolve(t *testing.T) {
	var pagerDutyActions []string
	var slackCalls int
	var opsgenieActions []string

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) *http.Response {
			if req.URL.Host == "events.pagerduty.com" {
				var pd PagerDutyPayload
				b, _ := io.ReadAll(req.Body)
				_ = json.Unmarshal(b, &pd)
				pagerDutyActions = append(pagerDutyActions, pd.EventAction)
				return &http.Response{StatusCode: 202, Body: io.NopCloser(bytes.NewBufferString(`{"status":"success"}`))}
			}
			if req.URL.Host == "hooks.slack.com" {
				slackCalls++
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(`ok`))}
			}
			if req.URL.Host == "api.opsgenie.com" {
				if req.Method == "POST" {
					if bytes.Contains([]byte(req.URL.Path), []byte("close")) {
						opsgenieActions = append(opsgenieActions, "resolve")
					} else {
						opsgenieActions = append(opsgenieActions, "create")
					}
				}
				return &http.Response{StatusCode: 202, Body: io.NopCloser(bytes.NewBufferString(`{"result":"Request will be processed"}`))}
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(`ok`))}
		}),
	}

	cfg := &ProbeConfig{
		Name:               "Checkout API Probe",
		Ref:                "chains/checkout.yaml",
		ConsensusThreshold: 2,
		Alerts: AlertsConfig{
			PagerDuty: &PagerDutyConfig{RoutingKey: "pd_mock_routing_key"},
			Slack:     &SlackConfig{WebhookURL: "https://hooks.slack.com/services/mock"},
			Opsgenie:  &OpsgenieConfig{APIKey: "genie_mock_key"},
		},
	}

	state := &ProbeState{CurrentStatus: StatusHealthy}

	// 1. First check: Degraded (1 failure) -> No alert
	degCheck := &ProbeCheckResult{
		ProbeName:     cfg.Name,
		Timestamp:     time.Now().UTC(),
		Status:        StatusDegraded,
		TotalRegions:  3,
		FailedRegions: 1,
	}
	ev1, _ := HandleStateTransition(state, degCheck, cfg, client)
	if ev1.Action != "NONE" {
		t.Errorf("expected Action NONE on degraded, got %s", ev1.Action)
	}
	if len(pagerDutyActions) != 0 {
		t.Errorf("expected 0 PagerDuty actions on degraded, got %d", len(pagerDutyActions))
	}

	// 2. Second check: Incident (2 failures) -> Trigger alert
	failCheck := &ProbeCheckResult{
		ProbeName:     cfg.Name,
		Timestamp:     time.Now().UTC(),
		Status:        StatusIncident,
		TotalRegions:  3,
		FailedRegions: 2,
		Checks: []RegionCheckResult{
			{Region: "us-east", StatusCode: 500, Passed: false, Error: "Internal Server Error"},
			{Region: "eu-central", StatusCode: 504, Passed: false, Error: "Gateway Timeout"},
			{Region: "ap-southeast", StatusCode: 200, Passed: true},
		},
	}
	ev2, _ := HandleStateTransition(state, failCheck, cfg, client)
	if ev2.Action != "TRIGGER" {
		t.Errorf("expected Action TRIGGER, got %s", ev2.Action)
	}
	if len(pagerDutyActions) != 1 || pagerDutyActions[0] != "trigger" {
		t.Errorf("expected PagerDuty trigger, got %v", pagerDutyActions)
	}
	if slackCalls != 1 {
		t.Errorf("expected 1 Slack call, got %d", slackCalls)
	}
	if len(opsgenieActions) != 1 || opsgenieActions[0] != "create" {
		t.Errorf("expected Opsgenie create, got %v", opsgenieActions)
	}

	// 3. Third check: Healthy -> Auto-Resolve alert
	healthCheck := &ProbeCheckResult{
		ProbeName:     cfg.Name,
		Timestamp:     time.Now().UTC(),
		Status:        StatusHealthy,
		TotalRegions:  3,
		PassedRegions: 3,
	}
	ev3, _ := HandleStateTransition(state, healthCheck, cfg, client)
	if ev3.Action != "RESOLVE" {
		t.Errorf("expected Action RESOLVE, got %s", ev3.Action)
	}
	if len(pagerDutyActions) != 2 || pagerDutyActions[1] != "resolve" {
		t.Errorf("expected PagerDuty resolve, got %v", pagerDutyActions)
	}
	if slackCalls != 2 {
		t.Errorf("expected 2 Slack calls, got %d", slackCalls)
	}
	if len(opsgenieActions) != 2 || opsgenieActions[1] != "resolve" {
		t.Errorf("expected Opsgenie resolve, got %v", opsgenieActions)
	}
}
