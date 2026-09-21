# Git-Native Synthetic API Monitoring & Incident Alerting (`hit probe`)

Traditional API monitoring tools store synthetic probe configs, uptime checks, and alerting rules in closed third-party dashboards disconnected from your version-controlled API contracts.

`hit probe` makes synthetic API monitoring **Git-native**:
* **Version-Controlled Probes**: Define probe schedules, SLA targets, multi-region criteria, and alerting channels in declarative YAML directly within your repository.
* **Multi-Region Consensus**: Run checks against simulated or physical edge regions. Set consensus thresholds (e.g. at least 2 regions must fail) to eliminate false alarms from transient route hiccups.
* **Smart State Machine**: Tracks transitions (`HEALTHY` $\leftrightarrow$ `DEGRADED` $\leftrightarrow$ `INCIDENT`) with automated deduplication so on-call engineers are paged only when real incidents occur or resolve.
* **Multi-Channel Dispatch**: Native integrations with **PagerDuty Events v2**, **Slack Block Kit**, **Opsgenie**, and generic HTTP webhooks.
* **Secret Hygiene**: Resolves credentials at runtime using `$env:VAR_NAME` syntax so sensitive routing keys are never stored in plain text.

---

## Quickstart

```bash
# 1. Run a one-shot multi-region consensus check
hit probe run probes/checkout-sla.yaml

# 2. Test alerting integrations (sends test notifications to Slack / PagerDuty / Opsgenie)
hit probe test-alert probes/checkout-sla.yaml

# 3. Start the continuous synthetic monitoring daemon
hit probe daemon probes/checkout-sla.yaml

# 4. Start probe daemon enrolled in Fleet Hub for central health tracking
hit probe daemon probes/checkout-sla.yaml --hub http://hub.internal:8080 --region us-east

# 5. Emit machine-readable JSON results for custom loggers
hit probe run probes/checkout-sla.yaml --json
```

---

## Probe Configuration (`probe.yaml`)

Create probe configurations under a `probes/` directory or alongside your collection requests:

```yaml
# probes/checkout-sla.yaml
name: "Checkout API Multi-Region SLA Probe"
ref: "collections/checkout/process-payment.yaml"
interval: 60s
consensus_threshold: 2

# Regions to execute from during each evaluation cycle
regions:
  - us-east-1
  - us-west-2
  - eu-central-1
  - ap-southeast-1

# Service Level Agreement criteria
sla:
  max_latency: 600ms
  allowed_status:
    - 200
    - 201

# Alerting destinations
alerts:
  # PagerDuty Events API v2
  pagerduty:
    routing_key: $env:PAGERDUTY_ROUTING_KEY
    severity: error   # info | warning | error | critical

  # Slack incoming webhook with Block Kit formatting
  slack:
    webhook_url: $env:SLACK_WEBHOOK_URL

  # Opsgenie alert integration
  opsgenie:
    api_key: $env:OPSGENIE_API_KEY
    priority: P2      # P1 | P2 | P3 | P4 | P5

  # Generic webhook for internal incident systems / Datadog / ServiceNow
  webhook:
    url: "https://incidents.internal.company.com/v1/events"
```

---

## Configuration Reference

| Field | Type | Description |
|---|---|---|
| `name` | `string` | Human-readable name for the probe (appears in alerts and logs). |
| `ref` | `string` | Relative path to a saved request YAML or scenario flow chain. |
| `interval` | `duration` | Check frequency in daemon mode (e.g. `30s`, `1m`, `5m`). |
| `regions` | `[]string` | List of geographic regions to execute the probe check across. |
| `consensus_threshold` | `int` | Minimum number of failed regions required to transition probe to `INCIDENT` status. Defaults to `1`. |
| `sla.max_latency` | `duration` | Maximum acceptable response duration (e.g. `500ms`, `1.5s`). |
| `sla.allowed_status` | `[]int` | Acceptable HTTP status codes (e.g. `[200, 204]`). |
| `alerts.pagerduty` | `object` | PagerDuty Events v2 config (`routing_key`, `severity`, optional `endpoint`). |
| `alerts.slack` | `object` | Slack incoming webhook configuration (`webhook_url`). |
| `alerts.opsgenie` | `object` | Opsgenie integration config (`api_key`, `priority`, optional `endpoint`). |
| `alerts.webhook` | `object` | Custom HTTP POST target (`url`). |

---

## Multi-Region Consensus & State Machine

False alerts kill developer trust. Transient packet loss or routing hiccups to a single edge region shouldn't page the on-call engineer at 3 AM.

```
                  ┌────────────────────────┐
                  │        HEALTHY         │
                  │   (All regions pass)   │
                  └───────────┬────────────┘
                              │
              Failed regions < ConsensusThreshold
                              ▼
                  ┌────────────────────────┐
                  │        DEGRADED        │
                  │  (Logged, no page out) │
                  └───────────┬────────────┘
                              │
             Failed regions >= ConsensusThreshold
                              ▼
                  ┌────────────────────────┐
                  │        INCIDENT        │──────► [Triggers PagerDuty, Slack, Opsgenie]
                  │ (Alert dispatched once)│
                  └───────────┬────────────┘
                              │
                      All regions pass
                              ▼
                  ┌────────────────────────┐
                  │        RESOLVED        │──────► [Dispatches resolution notifications]
                  └────────────────────────┘
```

1. **`HEALTHY`**: All configured regions pass assertions and latency SLAs.
2. **`DEGRADED`**: Fewer than `consensus_threshold` regions failed. The daemon logs the degraded state, but does not trigger high-urgency incident alerts.
3. **`INCIDENT`**: `consensus_threshold` or more regions failed simultaneously. Triggers PagerDuty incidents, Slack Block Kit notifications, and Opsgenie alerts.
4. **`RESOLVED`**: When all regions pass again on subsequent cycles, resolution alerts are sent automatically, closing the PagerDuty incident using its deduplication key.

---

## Verifying Integrations (`test-alert`)

Before deploying a probe to production, verify that webhook endpoints and API keys are functioning with `test-alert`:

```bash
export PAGERDUTY_ROUTING_KEY="pd-key-example"
export SLACK_WEBHOOK_URL="https://hooks.slack.com/services/T00/B00/X00"
export OPSGENIE_API_KEY="opsgenie-key-example"

hit probe test-alert probes/checkout-sla.yaml
```

Output:
```text
Dispatching dry-run test alerts for probe: Checkout API Multi-Region SLA Probe
  ✓ PagerDuty: event sent (status: success)
  ✓ Slack: message posted via Block Kit
  ✓ Opsgenie: alert queued (priority: P2)
  ✓ Webhook: HTTP 200 OK from https://incidents.internal.company.com/v1/events
```

---

## Production Daemon Deployment

### Running as a systemd Service

```ini
# /etc/systemd/system/hit-probe-checkout.service
[Unit]
Description=Hit Synthetic API Probe (Checkout)
After=network.target

[Service]
Type=simple
User=monitoring
WorkingDirectory=/opt/api-probes
EnvironmentFile=/etc/hit-probes/secrets.env
ExecStart=/usr/local/bin/hit probe daemon probes/checkout-sla.yaml
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

### Running with Docker

```bash
docker run -d \
  --name hit-probe-checkout \
  --restart always \
  -e PAGERDUTY_ROUTING_KEY="${PAGERDUTY_ROUTING_KEY}" \
  -e SLACK_WEBHOOK_URL="${SLACK_WEBHOOK_URL}" \
  -v $(pwd)/probes:/probes:ro \
  -v $(pwd)/collections:/collections:ro \
  ghcr.io/markjordan/hit-endpoint:latest \
  probe daemon /probes/checkout-sla.yaml
```
