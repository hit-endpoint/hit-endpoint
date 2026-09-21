# API Cost & Retry Amplification Estimator (`hit cost`)

When software engineering and FinOps teams integrate commercial third-party APIs (e.g., OpenAI, Anthropic, Stripe, Twilio, Google Maps, AWS API Gateway, SendGrid), calculating projected expenses with naive multiplication:

$$\text{Projected Cost} = \text{Intended Operations} \times \text{Unit Price}$$

consistently leads to severe budget overruns in production. Real-world costs diverge due to three primary dynamics:

1. **Volume Tier Curves**: Most commercial providers do not charge flat rates. They employ **graduated brackets** (where requests 1–10,000 cost \$0.010, but requests 10,001–50,000 cost \$0.005) or **volume-tier cliffs**.
2. **Client-Side Retry Amplification**: When downstream services experience transient hiccups, 429 rate limits, or 5xx gateway timeouts, client SDKs and microservice retry policies (e.g. up to 3 retries with exponential backoff) trigger additional attempts. A modest 3% error rate inflates total traffic; a degraded outage can amplify billable requests up to **$(1 + M)\times$**.
3. **Billable Invocations**: Commercial API gateways bill for every HTTP invocation received—including requests that terminated with a 429 Too Many Requests or 503 Service Unavailable.

`hit cost` is a dedicated API Cost & Retry Estimator engine built directly into `hit`. It models your pricing tiers and client retry policies to calculate **Min, Expected, and Worst-Case Cost Ranges**, identifies financial exposure to outage retry storms, and enforces **FinOps Budget Gates (`--budget`)** directly in CI/CD pipelines.

---

## Quickstart

```bash
# 1. Quick ad-hoc estimation with graduated tiers and retry modeling
hit cost -n 100k --tiers "10k:0.01,50k:0.005,+:0.002" --retries 3 --failure-rate 5%

# 2. Estimate LLM costs using built-in presets (e.g. OpenAI GPT-4o)
hit cost --preset openai-gpt4o -n 50k --tokens 800:200

# 3. Use an external pricing configuration file
hit cost --pricing ./pricing.yaml -n 250k

# 4. Enforce a FinOps budget gate in CI/CD (fails with exit code 1 if exceeded)
hit cost -n 100k --tiers "10k:0.01,+:0.005" --budget 400

# 5. Output machine-readable JSON for dashboards or custom scripts
hit cost collections/petstore/00-health.yaml -n 50k --json
```

---

## Mathematical Retry Amplification Model

When an application attempts $N$ unique operations with a client configured for up to $M$ retries (total potential attempts per operation = $1 + M$), and encounters a per-attempt failure probability $p$ ($0 \le p \le 1$):

1. Initial attempt occurs with probability $1$.
2. First retry occurs only if the initial attempt failed (probability $p$).
3. Second retry occurs only if both initial and first retry failed (probability $p^2$).
4. Attempt $k$ ($0 \le k \le M$) occurs with probability $p^k$.

The expected number of attempts per operation follows a finite geometric series:

$$E[\text{attempts}] = \sum_{k=0}^{M} p^k = \begin{cases} 1 + M & \text{if } p = 1 \\ \frac{1 - p^{M+1}}{1 - p} & \text{if } p < 1 \end{cases}$$

### Total Request Volume & Amplification Factor

$$\text{Total Expected Requests } N_{\text{billed}} = N \times E[\text{attempts}]$$

$$\text{Amplification Multiplier } \alpha(p, M) = \frac{N_{\text{billed}}}{N} = \sum_{k=0}^M p^k$$

### The Cost Spectrum: Min, Expected, Worst-Case & Outage Storm

`hit cost` computes four distinct points on the risk curve:

| Scenario | Failure Rate ($p$) | Formula $\alpha(p, M)$ | Meaning |
|---|---|---|---|
| **Min (Best Case)** | $0.0\%$ | $1.00\times$ | Perfect upstream health. Zero retries required. |
| **Expected** | $p_{\text{expected}}$ (e.g. $2\text{–}5\%$) | $\frac{1 - p^{M+1}}{1 - p}$ | Baseline production operation with normal transient retries. |
| **Worst-Case** | $p_{\text{worst}}$ (e.g. $15\text{–}30\%$) | $\frac{1 - p_{\text{worst}}^{M+1}}{1 - p_{\text{worst}}}$ | Degraded upstream, partial outage, or peak rate-limiting period. |
| **Outage Storm** | $100.0\%$ | $1 + M$ | Complete outage where 100% of operations exhaust all retries. Shows max financial liability. |

> [!WARNING]
> **Outage Storm Exposure**: If your client configures 3 retries ($M=3$) and the third-party service enters an outage, your application will fire **400,000 requests for every 100,000 intended operations** ($4.0\times$ amplification). If the provider bills for 5xx/429 attempts, your cost can spike by hundreds of percent!

---

## Supported Pricing Models

`hit cost` natively supports four commercial pricing structures:

### 1. Graduated / Tiered Volume (Default)
Each volume bracket is charged at its own rate. Reaching a higher tier only discounts requests beyond that tier's threshold.
* Example:
  * First 10,000 calls: \$0.010 / call
  * Next 40,000 calls (10,001–50,000): \$0.005 / call
  * Calls beyond 50,000: \$0.002 / call

### 2. Volume-Tier / Cliff Discount (`--model volume`)
All requests are billed at the single rate of the highest tier bracket reached by total volume.
* Example:
  * Total volume = 25,000 requests. Reaches Tier 2 (10,001–50,000 @ \$0.006).
  * All 25,000 requests are billed at \$0.006 (\$150.00).

### 3. Flat Rate (`--model flat` or `--unit-price`)
Every request is billed at a fixed unit rate regardless of volume.
* Example: \$0.002 per request.

### 4. Package / Block Pricing (`--model package` or `--package SIZE:PRICE`)
Services billed in bundles (e.g., \$15.00 per bundle of 1,000 requests), rounded up to the nearest package.

### 5. Multi-Dimension LLM Token Pricing (`--tokens` / `tokens:`)
Models both prompt (input) and completion (output) token costs:
* **Prompt Tokens**: Retransmitted with *every* HTTP retry attempt.
* **Completion Tokens**: Generated for intended completed requests.

---

## Configuration Schema (`cost.yaml` or embedded `cost:`)

Pricing and retry parameters can be defined in a standalone file, or directly embedded inside a request spec or `zone.yaml`:

```yaml
# cost.yaml
name: "Stripe Payment Processing"
currency: USD          # USD ($), EUR (€), GBP (£), JPY (¥)
base_fee: 25.00        # Monthly base/platform fee
pricing_model: graduated # 'graduated', 'volume', 'flat', 'package'
unit: transaction

# Volume tiers
tiers:
  - up_to: 10000       # 1 to 10,000
    unit_price: 0.30   # $0.30 per transaction
  - up_to: 50000       # 10,001 to 50,000
    unit_price: 0.25   # $0.25 per transaction
  - up_to: null        # 50,001+ (unbounded remainder)
    unit_price: 0.20   # $0.20 per transaction

# Optional LLM Token parameters
tokens:
  prompt_price_per_1m: 2.50      # $2.50 / 1M prompt tokens
  completion_price_per_1m: 10.00 # $10.00 / 1M completion tokens
  avg_prompt_tokens: 800         # Average input tokens per call
  avg_completion_tokens: 200     # Average output tokens per call

# Retry policy assumptions
retry:
  max_attempts: 3                # Max client retries
  expected_failure_rate: 0.03    # 3% baseline failure rate
  worst_case_failure_rate: 0.25  # 25% degraded failure rate
  billable_failures: true        # Commercial gateway charges for 429/5xx
```

---

## Built-In Commercial Presets (`--preset`)

`hit cost` ships with pre-configured pricing profiles for popular commercial APIs:

| Preset | Provider & Model | Pricing Structure |
|---|---|---|
| `openai-gpt4o` | OpenAI GPT-4o | \$2.50 / 1M prompt, \$10.00 / 1M completion (default 800:200 tokens) |
| `openai-gpt4o-mini` | OpenAI GPT-4o mini | \$0.15 / 1M prompt, \$0.60 / 1M completion (default 800:200 tokens) |
| `claude-3-5-sonnet` | Anthropic Claude 3.5 Sonnet | \$3.00 / 1M prompt, \$15.00 / 1M completion (default 800:200 tokens) |
| `stripe-charges` | Stripe Payments API | Graduated: 0–10k @ \$0.30, 10k–50k @ \$0.25, 50k+ @ \$0.20 |
| `twilio-sms` | Twilio Programmable SMS | Flat: \$0.0079 per outbound message |
| `google-maps` | Google Maps Geocoding | Graduated: 0–100k @ \$0.0050, 100k–500k @ \$0.0040, 500k+ @ \$0.0032 |

Usage:
```bash
hit cost --preset openai-gpt4o -n 100k --tokens 1200:350
hit cost --preset stripe-charges -n 75k
```

---

## FinOps CI/CD Budget Guardrails (`--budget`)

Prevent unexpected architectural or pricing changes from breaking financial limits. Pass `--budget <AMOUNT>` to enforce a spending ceiling:

```bash
# Verify planned 500k calls do not exceed $1,500 monthly budget
hit cost -n 500k --pricing ./pricing.yaml --budget 1500
```

* If **Expected Cost $\le$ Budget**: Output displays `✓ BUDGET OK` with available headroom, exiting with code `0`.
* If **Expected Cost $>$ Budget**: Output displays `❌ BUDGET EXCEEDED` with overage calculations, terminating the pipeline with exit code `2`.

---

## Empirical Inference from Test Telemetry (`--from-perf`)

Instead of guessing your application's failure rate, use `--from-perf` to automatically inspect recent test runs in `.hit/history.jsonl`:

```bash
# 1. Run a load benchmark or integration test suite
hit perf collections/checkout.yaml -c 50 -n 2000

# 2. Feed empirical failure rates directly into cost estimation
hit cost collections/checkout.yaml -n 500k --from-perf
```

`hit cost` detects observed error rates (connection drops, 429s, 5xx) and uses that empirical rate as the Expected Failure Rate.

---

## Command Flags & Tuning Reference

| Flag | Shorthand | Description | Default |
|---|---|---|---|
| `-n, --calls N` | `-n` | Intended operational request volume (e.g. `50k`, `1M`) | `100,000` |
| `-f, --pricing FILE` | `-f` | Path to custom YAML pricing configuration file | — |
| `-p, --preset NAME` | `-p` | Use built-in pricing preset (e.g. `openai-gpt4o`, `stripe`) | — |
| `--tiers SPEC` | — | Inline tier list (`"10k:0.01,50k:0.005,+:0.002"`) | Standard 3-tier default |
| `--unit-price P` | — | Flat price per request/unit | — |
| `--model TYPE` | — | `graduated`, `volume`, `flat`, `package` | `graduated` |
| `--package SIZE:PRICE`| — | Package bundle size and cost (e.g. `1000:15`) | — |
| `--base-fee FEE` | — | Monthly or flat platform base fee | `0.00` |
| `-r, --retries N` | `-r` | Maximum retry attempts per failed operation | `3` |
| `--failure-rate PCT` | — | Baseline expected failure rate (e.g. `0.03` or `3%`) | `3.0%` |
| `--worst-case-rate PCT`| — | Degraded/outage failure rate (e.g. `0.25` or `25%`) | `25.0%` |
| `--no-billable-failures`| — | Disables billing on 4xx/5xx failures (only bill successes) | `false` |
| `--tokens IN:OUT` | — | Average prompt:completion tokens per call (e.g. `800:200`) | — |
| `--token-pricing P:C` | — | Price per 1M prompt:completion tokens (e.g. `2.50:10.00`) | — |
| `-b, --budget AMOUNT` | `-b` | FinOps budget ceiling; fails pipeline if exceeded | — |
| `--currency CURR` | — | Currency code/symbol (`USD`, `EUR`, `GBP`, `JPY`) | `USD` |
| `--unit NAME` | — | Unit noun (`request`, `message`, `token`) | `request` |
| `--from-perf` | — | Infers failure rate from recent local test history | `false` |
| `--json` | — | Outputs complete results as machine-readable JSON | `false` |
