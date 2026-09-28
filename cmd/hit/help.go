package main

import (
	"fmt"
	"strings"

	"github.com/hit-endpoint/hit-endpoint/internal/output"
)

// CommandHelp describes a subcommand's usage, description, flags, and examples.
type CommandHelp struct {
	Name        string
	Aliases     []string
	Summary     string
	Usage       string
	Flags       []string
	Examples    []string
	SeeAlso     []string
}

var commandRegistry = map[string]CommandHelp{
	"run": {
		Name:    "run",
		Summary: "Execute API requests, test suites, or scenario chains against target servers.",
		Usage:   "hit run <REF...> [flags]",
		Flags: []string{
			"-s, --server <NAME>        Target server environment (e.g. staging, prod)",
			"-z, --zone <DIR>           Specify zone root directory",
			"--var KEY=VALUE            Override variable (repeatable)",
			"-v, --verbose              Display detailed request and response headers/bodies",
			"--json                     Output structured JSON test summary",
			"--body                     Output only the raw response body",
			"--code                     Output only the HTTP status code",
			"--time                     Output only the total execution time",
			"--headers                  Display response headers",
			"--quiet                    Suppress progress indicators and pass logs",
			"--full                     Display complete un-truncated response bodies",
			"--max-body <BYTES>         Set maximum preview body length (default: 4000)",
			"--fail-fast                Stop suite execution immediately on first assertion failure",
			"--keep-going               Continue executing subsequent requests despite failures",
			"--no-persist               Do not persist captured variables to server state",
			"--lenient                  Ignore missing optional template variables",
			"--junit <FILE>             Export test results to JUnit XML file",
			"--html <FILE>              Generate standalone interactive HTML report",
			"--har <FILE>               Export HTTP archive (HAR 1.2) for debugging",
			"--webhook-on-failure <URL> Dispatch JSON alert webhook on test failure",
			"--publish[=URL]            Publish telemetry results to Team Hub dashboard",
			"--enforce-policy           Enforce .hit/policy.yaml compliance rules",
			"--format=github|agent      Format output for GitHub Actions annotations or AI agents",
		},
		Examples: []string{
			"hit run collections/pets/get-pet.yaml",
			"hit run pets/list -s staging -v",
			"hit run chains/checkout.yaml --var user_id=123",
			"hit run collections/ --junit=junit.xml --publish",
			"hit run smoke.yaml --fail-fast --format=github",
		},
		SeeAlso: []string{"perf", "mock", "replay", "assert", "report"},
	},
	"auth": {
		Name:    "auth",
		Aliases: []string{"token", "cookie"},
		Summary: "Import, inspect, paste, or clear JWT tokens and session cookies for authenticated requests.",
		Usage:   "hit auth [paste | token <TOKEN> | cookie <COOKIE> | show | clear] [flags]",
		Flags: []string{
			"-s, --server <NAME>        Target server environment to associate credentials with",
			"-z, --zone <DIR>           Specify zone root directory",
			"--global                   Store credentials globally (~/.hit/auth.json) instead of zone state",
		},
		Examples: []string{
			"hit auth                                  # Interactive paste prompt or read from stdin",
			"hit auth \"eyJhbGciOi...\"                  # Automatically detects and stores JWT token",
			"hit auth \"session_id=abc; uid=100\"       # Automatically detects and stores session cookie",
			"hit auth token \"eyJhbGci...\"             # Store JWT token & display expiration claims",
			"hit auth cookie \"connect.sid=s%3A...\"    # Store browser session cookie",
			"hit auth show                             # Inspect stored token claims, expiry TTL, and cookies",
			"hit auth clear                            # Remove stored credentials for active server",
			"pbpaste | hit auth                        # Pipe clipboard contents directly (macOS)",
		},
		SeeAlso: []string{"vars", "servers", "run"},
	},
	"perf": {
		Name:    "perf",
		Summary: "Distributed load testing and performance benchmarking engine.",
		Usage:   "hit perf <REF> [-c CONC] [-n TOTAL | -d DURATION] [flags]",
		Flags: []string{
			"-c, --concurrency <N>      Number of concurrent virtual users (default: 10)",
			"-n, --requests <N>         Total number of requests to execute",
			"-d, --duration <SECONDS>   Benchmark duration in seconds (e.g. 30s, 60)",
			"--rps <N>                  Throttle to maximum requests per second target",
			"--warmup <SECONDS>         Warmup period duration before statistics recording",
			"--ramp-up <SECONDS>        Gradually ramp concurrency up over time window",
			"--threshold <SPEC>         SLA assertion ceiling (e.g. 'p95<200ms,error_rate<1%')",
			"--hub <URL>                Auto-discover load worker fleet from Team Hub",
			"--workers <URLS>           Comma-separated explicit worker addresses",
			"--distribute <N>           Number of worker nodes to partition load across",
			"--check                    Validate thresholds and exit with code 1 if violated",
			"--csv <FILE>               Export detailed latency percentiles to CSV",
			"--junit <FILE>             Export benchmark SLA results to JUnit XML",
			"--json                     Output performance summary in JSON format",
		},
		Examples: []string{
			"hit perf pets/list -c 50 -n 5000",
			"hit perf checkout -c 100 -d 60s --threshold 'p95<150ms,p99<300ms'",
			"hit perf search --hub http://hub.internal:8080 -c 500 -n 50000",
			"hit perf worker --port 9090 --region us-east --hub http://hub.internal:8080",
		},
		SeeAlso: []string{"run", "hub", "dispatch"},
	},
	"mock": {
		Name:    "mock",
		Aliases: []string{"serve"},
		Summary: "Zero-config offline mock server with dynamic template evaluation and error injection.",
		Usage:   "hit mock [PORT] [--openapi SPEC] [flags]",
		Flags: []string{
			"-p, --port <PORT>          HTTP port to listen on (default: 8080)",
			"--openapi <SPEC>           OpenAPI 3.0/3.1 YAML/JSON file to synthesize endpoints from",
			"--stateless                Disable automatic in-memory CRUD state retention",
			"--latency <DURATION>       Inject synthetic delay on every response (e.g. 100ms)",
			"--jitter <RANGE>           Add random latency jitter range (e.g. 50ms-200ms)",
			"--flaky <RATE>             Inject random HTTP 500 failures at rate (e.g. 0.1 for 10%)",
			"--rate-limit <N/s>         Simulate 429 Too Many Requests over threshold",
			"--auth-expire <DURATION>   Expire bearer tokens automatically after duration",
		},
		Examples: []string{
			"hit mock 8080",
			"hit mock --openapi petstore.yaml --latency 50ms",
			"hit mock 3000 --flaky 0.05 --jitter 20ms-100ms",
		},
		SeeAlso: []string{"run", "learn"},
	},
	"hub": {
		Name:    "hub",
		Aliases: []string{"cloud"},
		Summary: "Launch cloud/self-hosted team fleet telemetry dashboard & job dispatch hub.",
		Usage:   "hit hub [--port PORT] [--dir DIR] [flags]",
		Flags: []string{
			"-p, --port <PORT>          Port to listen on (default: 8080)",
			"-d, --dir <DIR>            Data storage directory (default: ~/.hit/hub)",
			"-k, --api-key <KEY>        Require Bearer token authentication for telemetry ingestion",
			"--project <ID>             Default project identifier filter",
			"--url <PUBLIC_URL>         Public canonical URL for telemetry links and webhooks",
			"--logo <URL|PATH>          Custom company logo URL or local image path in header",
			"--footer <TEXT|HTML>       Custom footer text or HTML rendered on dashboard",
			"--theme, --skin <NAME>     Default skin/theme: dark, light, or night (OLED)",
			"--title <TITLE>            Custom dashboard window and header title",
		},
		Examples: []string{
			"hit hub --port 8080",
			"hit hub --port 8080 --api-key \"sec_team_token\"",
			"hit hub --logo https://company.com/logo.svg --footer \"Acme Corp Platform Team\"",
			"hit hub --theme light --dir /var/lib/hit-hub",
		},
		SeeAlso: []string{"perf", "dispatch", "run", "probe"},
	},
	"init": {
		Name:    "init",
		Summary: "Initialize a new Git-native test zone or project with starter folders and config.",
		Usage:   "hit init [DIR] [--name NAME] [--wizard]",
		Flags: []string{
			"--name <NAME>              Name of the zone (defaults to directory name)",
			"-w, --wizard               Launch interactive setup wizard",
		},
		Examples: []string{
			"hit init .                                # Initialize in current directory",
			"hit init my-api                           # Create new directory 'my-api' with starter zone",
			"hit init tests/api --name payments-api    # Scaffolds zone under tests/api",
			"hit init --wizard                         # Interactive prompt for URL and targets",
		},
		SeeAlso: []string{"zone", "sanity", "servers"},
	},
	"zone": {
		Name:    "zone",
		Summary: "Manage API test zones and scaffolding.",
		Usage:   "hit zone new [DIR] [--name NAME] [--url URL] [-y]",
		Flags: []string{
			"--name <NAME>              Zone name",
			"--url <URL>                Base URL for default local/dev server",
			"-y, --yes                  Skip interactive confirmations and accept defaults",
		},
		Examples: []string{
			"hit zone new api-tests --url http://localhost:3000 -y",
		},
		SeeAlso: []string{"init", "servers", "vars"},
	},
	"vars": {
		Name:    "vars",
		Summary: "View, set, unset, or clear captured session variables and environment variables.",
		Usage:   "hit vars [set KEY VALUE | unset KEY | clear] [--all] [flags]",
		Flags: []string{
			"--all                      Display variables from all layers (zone, server, session, cli)",
			"-s, --server <NAME>        Target server environment",
		},
		Examples: []string{
			"hit vars                                  # List captured session variables for active server",
			"hit vars --all                            # Inspect full variable resolution tree",
			"hit vars set token eyJhbGciOi...          # Manually assign variable",
			"hit vars unset token                      # Remove variable from server state",
			"hit vars clear                            # Clear all captured state for current server",
		},
		SeeAlso: []string{"auth", "servers"},
	},
	"servers": {
		Name:    "servers",
		Aliases: []string{"envs"},
		Summary: "List configured servers and environments in the active zone.",
		Usage:   "hit servers",
		Examples: []string{
			"hit servers                               # Shows local, staging, production (default marked)",
			"hit run pets/list -s production           # Run against specific server",
		},
		SeeAlso: []string{"vars", "init"},
	},
	"ls": {
		Name:    "ls",
		Summary: "List requests, chains, and test suites in the zone.",
		Usage:   "hit ls [FOLDER] [--json]",
		Flags: []string{
			"--json                     Output list in JSON format",
		},
		Examples: []string{
			"hit ls",
			"hit ls collections/pets",
			"hit ls chains",
		},
		SeeAlso: []string{"show", "run"},
	},
	"show": {
		Name:    "show",
		Summary: "Inspect and display a request specification, cURL equivalent, or code snippet.",
		Usage:   "hit show <REF> [--curl] [--json] [--lang LANG] [--extract PATH]",
		Flags: []string{
			"--curl                     Render copy-pasteable curl command",
			"--json                     Render parsed specification as JSON",
			"--lang <LANG>              Generate code snippet (python, php, js, go)",
			"--extract <PATH>           Extract specific YAML field using dot notation",
		},
		Examples: []string{
			"hit show pets/get-pet",
			"hit show pets/create-pet --curl",
			"hit show pets/list --lang python",
		},
		SeeAlso: []string{"snippet", "run"},
	},
	"diff": {
		Name:    "diff",
		Summary: "Compare response bodies and headers between two runs or against historical baseline.",
		Usage:   "hit diff <ID|INDEX> [<ID|INDEX>] [--headers] [--body-only] [--json] [--context N]",
		Flags: []string{
			"--headers                  Compare HTTP response headers",
			"--body-only                Compare only response bodies (ignore status/headers)",
			"--context <N>              Number of diff context lines to show (default: 3)",
			"--json                     Output diff in machine-readable JSON format",
		},
		Examples: []string{
			"hit diff 0                                # Diff latest run against previous run",
			"hit diff 1 2                              # Diff run index 1 against index 2",
			"hit diff run_abc123 run_xyz789",
		},
		SeeAlso: []string{"replay", "history"},
	},
	"replay": {
		Name:    "replay",
		Summary: "Replay a historical request with optional overrides, variable changes, or diffing.",
		Usage:   "hit replay <ID|INDEX> [-s SERVER] [--var k=v] [-H k:v] [--diff] [flags]",
		Flags: []string{
			"-s, --server <NAME>        Replay against a different server target",
			"--var KEY=VALUE            Override variables for replay",
			"-H, --header <HEADER>      Add or override request header",
			"--diff                     Automatically diff replayed response against original record",
			"--headers                  Show response headers",
			"--body-only                Output only response body",
			"--json                     Output result in JSON format",
		},
		Examples: []string{
			"hit replay 0                              # Replay the most recent request",
			"hit replay 0 -s staging --diff            # Replay against staging and show diff against dev",
			"hit replay run_abc123 --var user_id=99",
		},
		SeeAlso: []string{"diff", "history", "run"},
	},
	"history": {
		Name:    "history",
		Summary: "Search, view, clear, or convert historical executions into persistent tests.",
		Usage:   "hit history [ls | show <ID> | save <ID> <FILE.yaml> | clear] [flags]",
		Flags: []string{
			"--limit <N>                Maximum records to display (default: 20)",
			"--status <CODE>            Filter history by HTTP status code (e.g. 500, 404)",
			"--ref <PATTERN>            Filter history by spec ref or URL pattern",
			"--json                     Output history in JSON format",
			"--infer                    Automatically infer response assertions when saving",
		},
		Examples: []string{
			"hit history                               # List last 20 requests",
			"hit history --status 500                  # Show failed 5xx requests",
			"hit history show 0                        # View full details of latest execution",
			"hit history save 0 tests/regression.yaml  # Convert ad-hoc run into permanent test spec",
			"hit history clear                         # Wipe local history log",
		},
		SeeAlso: []string{"replay", "diff"},
	},
	"report": {
		Name:    "report",
		Summary: "Generate test and coverage reports in HTML, HAR, and latency quantile formats.",
		Usage:   "hit report [html | har | coverage | latency] [flags]",
		Flags: []string{
			"-o, --output <FILE>        Output file path",
			"-n, --limit <N>            Number of historical runs to include",
			"--openapi <SPEC>           OpenAPI spec file for endpoint coverage reporting",
			"-t, --threshold <PCT>      Tail latency threshold percent (default: 95)",
			"--json                     Output report data in JSON format",
		},
		Examples: []string{
			"hit report html -o test-report.html",
			"hit report har -o debug-session.har --status 500",
			"hit report coverage --openapi openapi.yaml",
			"hit report latency -t 99",
		},
		SeeAlso: []string{"run", "history"},
	},
	"assert": {
		Name:    "assert",
		Summary: "Run ad-hoc assertions or generate regression assertion suites from live responses.",
		Usage:   "hit assert <URL | ID | REF> [--save [FILE]] [--append [FILE]] [flags]",
		Flags: []string{
			"--save [FILE]              Generate and save inferred assertions to file",
			"--append [FILE]            Append inferred assertions to existing spec file",
			"--strict                   Generate strict assertions (exact types and non-null values)",
			"--no-latency               Omit latency assertion threshold",
			"--no-headers               Omit header assertions",
			"--json                     Output assertion results in JSON format",
		},
		Examples: []string{
			"hit assert https://httpbin.org/json",
			"hit assert pets/get-pet --save collections/pets/get-pet-assert.yaml",
			"hit assert 0 --append collections/pets/01-list.yaml",
		},
		SeeAlso: []string{"run", "validate"},
	},
	"fuzz": {
		Name:    "fuzz",
		Summary: "Autonomous API security fuzzer (SQLi, XSS, Path Traversal, Auth Bypass, Format strings).",
		Usage:   "hit fuzz <REF | URL> [-n RUNS] [--categories CATS] [flags]",
		Flags: []string{
			"-n, --runs <N>             Number of mutation iterations to execute (default: 50)",
			"--categories <CATS>        Comma-separated attack types: sqli,xss,path,format,auth,all",
			"--fail-on-5xx              Fail immediately if server responds with 500 Internal Error",
			"--sarif <FILE>             Export security vulnerability findings to SARIF 2.1.0 format",
			"--json                     Output findings in JSON format",
			"-v, --verbose              Display individual mutation request payloads",
		},
		Examples: []string{
			"hit fuzz https://httpbin.org/get -n 25",
			"hit fuzz pets/create-pet --categories sqli,xss --fail-on-5xx",
			"hit fuzz checkout --sarif results.sarif",
		},
		SeeAlso: []string{"policy", "run"},
	},
	"policy": {
		Name:    "policy",
		Aliases: []string{"lint"},
		Summary: "Lint and validate API requests and zones against governance rules.",
		Usage:   "hit policy [DIR] [--strict] [--junit FILE] [flags]",
		Flags: []string{
			"--strict                   Treat warnings as policy errors",
			"--junit <FILE>             Export policy violations to JUnit XML report",
			"--json                     Output violations in JSON format",
		},
		Examples: []string{
			"hit policy                                # Lint entire zone against .hit/policy.yaml",
			"hit policy collections/pets/ --strict",
		},
		SeeAlso: []string{"fuzz", "validate", "sanity"},
	},
	"validate": {
		Name:    "validate",
		Summary: "Validate syntax and schema of request specs and chains without sending network calls.",
		Usage:   "hit validate [REF | FOLDER...] [--json]",
		Flags: []string{
			"--json                     Output validation diagnostics in JSON format",
		},
		Examples: []string{
			"hit validate",
			"hit validate collections/pets/01-create.yaml",
		},
		SeeAlso: []string{"sanity", "policy"},
	},
	"sanity": {
		Name:    "sanity",
		Summary: "Run offline pre-flight syntax, variable, and credential checks across entire zone.",
		Usage:   "hit sanity [COLLECTION] [--offline] [--json]",
		Flags: []string{
			"--offline                  Skip DNS and network reachability checks",
			"--json                     Output health audit in JSON format",
		},
		Examples: []string{
			"hit sanity                                # Check all endpoints, servers, and secret files",
			"hit sanity --offline",
		},
		SeeAlso: []string{"validate", "init"},
	},
	"new": {
		Name:    "new",
		Summary: "Scaffold a new request spec from method, URL, or inferred template.",
		Usage:   "hit new <REF> [-m METHOD] [-u URL] [--infer]",
		Flags: []string{
			"-m, --method <METHOD>      HTTP method (GET, POST, PUT, DELETE, etc.)",
			"-u, --url <URL>            Endpoint URL or path",
			"--infer                    Send live probe request to automatically infer headers/assertions",
		},
		Examples: []string{
			"hit new pets/delete-pet -m DELETE -u /pets/{{pet_id}}",
			"hit new users/get-profile -u https://httpbin.org/json --infer",
		},
		SeeAlso: []string{"init", "assert"},
	},
	"import": {
		Name:    "import",
		Summary: "Import API collections from Postman, OpenAPI, cURL, or environment files.",
		Usage:   "hit import [collection | openapi | curl | env | globals] <FILE...>",
		Examples: []string{
			"hit import openapi petstore-v3.json",
			"hit import collection postman_collection.json",
			"hit import curl my-curl-commands.sh",
			"hit import env postman_environment.json",
		},
		SeeAlso: []string{"init", "show"},
	},
	"snippet": {
		Name:    "snippet",
		Aliases: []string{"code-block"},
		Summary: "Generate client code snippets in Python, Go, JavaScript, PHP, etc.",
		Usage:   "hit snippet <REF | URL> [--lang LANG] [--extract PATH]",
		Flags: []string{
			"--lang <LANG>              Target programming language: python, php, js, go, all (default: python)",
			"--extract <PATH>           Extract specific response field in generated snippet",
		},
		Examples: []string{
			"hit snippet pets/get-pet --lang python",
			"hit snippet https://httpbin.org/get --lang go",
			"hit snippet checkout --lang js --extract body.order_id",
		},
		SeeAlso: []string{"show"},
	},
	"graphql": {
		Name:    "graphql",
		Aliases: []string{"gql"},
		Summary: "Execute GraphQL queries, mutations, introspection, and schema generation.",
		Usage:   "hit graphql <URL | REF> [-q QUERY|@file] [-v VARS|@file] [-o OP] [flags]",
		Flags: []string{
			"-q, --query <QUERY>        GraphQL query string or @file path",
			"-v, --vars <VARS>          GraphQL variables JSON string or @file path",
			"-o, --operation <NAME>     Operation name to execute",
			"--introspect               Run full introspection query and display JSON schema",
			"--sdl                      Fetch schema and format as GraphQL Schema Definition Language",
			"--fail-on-errors           Exit with code 1 if response contains GraphQL 'errors' array",
			"--json                     Output response as raw JSON",
		},
		Examples: []string{
			"hit graphql https://countries.trevorblades.com/ -q '{ countries { name code } }'",
			"hit graphql api/graphql -q @query.gql -v @vars.json --fail-on-errors",
			"hit graphql https://api.spacex.land/graphql --sdl",
		},
		SeeAlso: []string{"run", "schema"},
	},
	"sse": {
		Name:    "sse",
		Summary: "Subscribe to Server-Sent Events (SSE) streams with filtering and event matching.",
		Usage:   "hit sse <URL> [-H k:v] [-n MAX] [-d TIMEOUT] [--event NAME] [--json]",
		Flags: []string{
			"-n, --max <N>              Stop listening after receiving N events",
			"-d, --timeout <DURATION>   Maximum duration to keep connection open (e.g. 15s)",
			"--event <NAME>             Filter events by event type name",
			"-H, --header <HEADER>      Custom request header",
			"--json                     Output events as JSON objects",
		},
		Examples: []string{
			"hit sse https://sse.example.com/events -n 5",
			"hit sse https://api.example.com/stream --event trade -d 30s",
		},
		SeeAlso: []string{"ws", "run"},
	},
	"ws": {
		Name:    "ws",
		Aliases: []string{"websocket"},
		Summary: "Connect to WebSockets, send message payloads, and assert received responses.",
		Usage:   "hit ws <URL> [-H k:v] [-m MSG]... [--expect PATTERN] [-d TIMEOUT] [-n MAX]",
		Flags: []string{
			"-m, --message <PAYLOAD>    Message payload to send upon connection (repeatable)",
			"--expect <PATTERN>         Assert that received message matches regex pattern",
			"-d, --timeout <DURATION>   Connection timeout (e.g. 10s)",
			"-n, --max <N>              Maximum number of inbound messages to read",
			"--json                     Output messages in JSON format",
		},
		Examples: []string{
			"hit ws wss://echo.websocket.events -m \"hello world\" --expect \"hello\"",
			"hit ws wss://stream.binance.com/ws/btcusdt@trade -n 3",
		},
		SeeAlso: []string{"sse", "run"},
	},
	"schedule": {
		Name:    "schedule",
		Summary: "Schedule recurring runs, intervals, or timed API executions.",
		Usage:   "hit schedule <URL | REF | SHORTHAND> [--at TIME] [--every INTERVAL] [-n COUNT] [flags]",
		Flags: []string{
			"--at <TIME>                Execute once at specific time (e.g. '14:30', '2026-10-01 09:00')",
			"--every <INTERVAL>         Recurring interval (e.g. '5m', '1h', '30s')",
			"-n, --count <N>            Stop after N executions",
			"--status <CODE>            Assert HTTP status code for each execution",
			"--expect <PATTERN>         Assert response body contains pattern",
		},
		Examples: []string{
			"hit schedule https://api.example.com/health --every 1m",
			"hit schedule smoke.yaml --at 18:00",
			"hit schedule health --every 30s -n 10 --status 200",
		},
		SeeAlso: []string{"probe", "run"},
	},
	"probe": {
		Name:    "probe",
		Summary: "Multi-region synthetic uptime monitoring, daemon mode, and alert dispatch.",
		Usage:   "hit probe [run | daemon | test-alert] <FILE | REF> [flags]",
		Flags: []string{
			"--interval <DURATION>      Check evaluation interval for daemon mode (default: 30s)",
			"--hub <URL>                Enroll probe into Team Hub for centralized monitoring",
			"--regions <LIST>           Comma-separated regional nodes for multi-region quorum",
			"--consensus <N>            Number of regions required to agree before alerting",
			"--channel <CHANNEL>        Alert channel for test-alert: all, slack, pagerduty, opsgenie",
			"--json                     Output probe checks in JSON format",
		},
		Examples: []string{
			"hit probe run probes/api-health.yaml",
			"hit probe daemon probes/api-health.yaml --hub http://hub.internal:8080",
			"hit probe test-alert probes/api-health.yaml --channel slack",
		},
		SeeAlso: []string{"schedule", "hub"},
	},
	"dispatch": {
		Name:    "dispatch",
		Summary: "Remote job dispatch to fleet workers or nodes via Team Hub.",
		Usage:   "hit dispatch <FILE | URL | REF> [--hub URL] [--node ID] [--region REGION] [flags]",
		Flags: []string{
			"--hub <URL>                Team Hub address (default: http://127.0.0.1:8080)",
			"--node <ID>                Target specific node ID for execution",
			"--region <REGION>          Target any idle worker within geographic region",
			"--perf                     Execute as a performance load test rather than functional test",
			"-c, --concurrency <N>      Concurrency for load benchmark",
			"-n, --requests <N>         Total requests for load benchmark",
			"--no-stream                Submit job asynchronously without waiting for logs",
			"--json                     Output dispatch response as JSON",
		},
		Examples: []string{
			"hit dispatch collections/pets/health.yaml --hub http://hub.internal:8080 --region us-east",
			"hit dispatch collections/checkout.yaml --hub http://hub.internal:8080 --node worker-1",
			"hit dispatch https://httpbin.org/get --hub http://hub.internal:8080",
		},
		SeeAlso: []string{"hub", "perf"},
	},
	"cost": {
		Name:    "cost",
		Aliases: []string{"estimate"},
		Summary: "API cost estimation, volume forecasting, and tier budgeting calculator.",
		Usage:   "hit cost [<REF | FILE>] [-n CALLS] [--tiers SPEC] [--pricing FILE] [--preset NAME] [--budget AMOUNT]",
		Flags: []string{
			"-n, --calls <N>            Anticipated monthly API call volume (default: 1000000)",
			"--tiers <SPEC>             Tier specification (e.g. '0-100k:0,100k-1M:0.002,1M+:0.001')",
			"--pricing <FILE>           Load pricing schema from YAML/JSON file",
			"--preset <NAME>            Use built-in preset (openai-gpt4o, stripe, aws-api-gateway, etc.)",
			"--budget <AMOUNT>          Maximum budget ceiling to check against",
		},
		Examples: []string{
			"hit cost -n 5000000 --preset openai-gpt4o",
			"hit cost collections/payments -n 250000 --tiers '0-10k:0,10k+:0.01' --budget 2000",
		},
		SeeAlso: []string{"perf"},
	},
	"shorthand": {
		Name:    "shorthand",
		Aliases: []string{"alias", "shorthands"},
		Summary: "Create quick command shortcuts and aliases for common endpoints.",
		Usage:   "hit shorthand [ls | set <NAME> <URL|REF> | rm <NAME>] [flags]",
		Flags: []string{
			"-m, --method <METHOD>      HTTP method for shorthand",
			"-s, --server <NAME>        Default server environment for shorthand",
			"-H, --header <HEADER>      Preset header (repeatable)",
			"--global                   Store shorthand globally in ~/.hit/shorthands.yaml",
			"--json                     Output shorthands in JSON format",
		},
		Examples: []string{
			"hit shorthand ls",
			"hit shorthand set health https://httpbin.org/status/200",
			"hit shorthand set me /users/me -s staging -H \"Accept: application/json\"",
			"hit health                                # Executes the 'health' shorthand!",
			"hit shorthand rm health",
		},
		SeeAlso: []string{"run", "show"},
	},
	"mcp": {
		Name:    "mcp",
		Summary: "Start Model Context Protocol (MCP) server over stdio for AI coding assistants.",
		Usage:   "hit mcp [-z ZONE_DIR]",
		Flags: []string{
			"-z, --zone <DIR>           Specify zone root directory to expose to MCP client",
		},
		Examples: []string{
			"hit mcp                                   # Run stdio MCP server for Cursor, Claude, Antigravity",
		},
		SeeAlso: []string{"run", "mock"},
	},
	"learn": {
		Name:    "learn",
		Aliases: []string{"tutorial"},
		Summary: "Interactive hands-on tutorial and verification sandbox.",
		Usage:   "hit learn [verify [1-7 | all]] [--mock URL]",
		Flags: []string{
			"--mock <URL>               Specify custom mock server URL",
		},
		Examples: []string{
			"hit learn                                 # Display tutorial lesson roadmap and instructions",
			"hit learn verify 1                        # Verify completion of Lesson 1",
			"hit learn verify all                      # Run automated verification across all 7 lessons",
		},
		SeeAlso: []string{"mock", "init"},
	},
	"schema": {
		Name:    "schema",
		Summary: "Print JSON Schema definitions for requests, chains, zones, or GraphQL endpoints.",
		Usage:   "hit schema [request | chain | zone | graphql <URL>]",
		Examples: []string{
			"hit schema request                        # Output JSON Schema for request YAMLs",
			"hit schema graphql https://api.spacex.land/graphql",
		},
		SeeAlso: []string{"validate", "graphql"},
	},
	"reference": {
		Name:    "reference",
		Aliases: []string{"docs"},
		Summary: "Display complete YAML specification reference documentation.",
		Usage:   "hit reference",
		Examples: []string{
			"hit reference",
		},
		SeeAlso: []string{"schema", "help"},
	},
}

// showGeneralHelp prints the top-level command overview.
func showGeneralHelp(p *output.Printer) {
	fmt.Print(`Hit Endpoint - A file-based, git-friendly API tester, runner, and fleet engine.

Usage:
  hit [METHOD] URL [options...]                     Ad hoc HTTP request
  hit <command> [args...] [flags...]                Execute subcommand
  hit help <command>                                Detailed help and flags for any command

Core Subcommands:
  run          Execute API requests, test suites, or scenario chains
  auth         Import, inspect, paste, or clear JWT tokens & browser cookies
  perf         Distributed load testing and performance benchmarking
  mock         Offline zero-config mock server with dynamic templating
  hub          Team fleet telemetry dashboard & remote job dispatch hub
  init         Initialize a new Git-native test zone or project
  vars         Inspect and manage captured session variables
  servers      List configured environments (local, staging, prod)
  ls           List available test requests and scenario chains
  show         Display request spec, curl command, or code snippet
  diff         Compare responses between two runs or against baseline
  replay       Replay historical requests with overrides or diffing
  history      Search, view, clear, or convert executions to tests
  report       Generate test reports (HTML, HAR 1.2, latency quantiles)
  assert       Run assertions or infer regression assertions from live API
  fuzz         Autonomous security fuzzer (SQLi, XSS, Path Traversal)
  policy       Lint and enforce governance policies on test suites
  validate     Validate syntax and schemas without network calls
  sanity       Pre-flight check for endpoints, secrets, and credentials
  new          Scaffold a new request specification
  import       Import from Postman collections, OpenAPI specs, or cURL
  snippet      Generate code snippets in Python, Go, JS, PHP
  graphql      Execute GraphQL queries, mutations, and introspection
  sse          Subscribe to Server-Sent Events (SSE) streams
  ws           Connect to WebSockets and assert messages
  schedule     Schedule recurring or timed API requests
  probe        Synthetic uptime monitoring daemon & incident alerts
  dispatch     Remote job execution via Team Hub
  cost         API volume pricing and tier budget calculator
  shorthand    Manage quick command aliases
  mcp          Model Context Protocol (MCP) server for AI assistants
  learn        Interactive hands-on learning sandbox (7 lessons)
  reference    Complete YAML specification language reference

Ad Hoc Scripting Output Shortcuts:
  hit body [METHOD] URL ...      Output response body only
  hit code [METHOD] URL ...      Output HTTP status code only (e.g. 200)
  hit time [METHOD] URL ...      Output execution time only (e.g. 42ms)
  hit headers [METHOD] URL ...   Output response headers only

Global Flags:
  -z, --zone <DIR>        Zone root directory (auto-discovered if omitted)
  -s, --server <NAME>     Server environment name (e.g. local, staging, prod)
  --var KEY=VALUE         Override variable (repeatable)
  -k, --insecure          Skip TLS certificate verification
  --no-color              Disable colored terminal output
  --no-history            Do not record requests to history log
  -h, --help              Show help information

Run 'hit help <command>' (e.g. 'hit help run', 'hit help auth') for full flags & examples.
`)
}

// showCommandHelp prints comprehensive help for a specific command.
func showCommandHelp(cmdName string, p *output.Printer) {
	cmdName = strings.TrimPrefix(cmdName, "-")
	cmdName = strings.TrimPrefix(cmdName, "-")
	cmdLower := strings.ToLower(cmdName)

	// Check if this is an adhoc shortcut
	switch cmdLower {
	case "body", "code", "time", "headers", "response":
		p.Out(fmt.Sprintf("%s - Ad hoc scripting shortcut for %s", p.Bold("hit "+cmdLower), cmdLower))
		p.Out(fmt.Sprintf("\n%s\n  hit %s [METHOD] URL [options...]", p.Bold("Usage:"), cmdLower))
		p.Out(fmt.Sprintf("\n%s", p.Bold("Description:")))
		p.Out(fmt.Sprintf("  Executes an ad hoc HTTP request and outputs strictly the %s,", cmdLower))
		p.Out("  making it ideal for shell piping, scripts, and automation without extra formatting.")
		p.Out(fmt.Sprintf("\n%s", p.Bold("Examples:")))
		p.Out(fmt.Sprintf("  hit %s https://httpbin.org/get", cmdLower))
		p.Out(fmt.Sprintf("  hit %s POST https://httpbin.org/post -j '{\"name\":\"mark\"}'", cmdLower))
		p.Out(fmt.Sprintf("  STATUS=$(hit code https://api.example.com/health)"))
		return
	}

	// Lookup in registry
	var found *CommandHelp
	for k, help := range commandRegistry {
		if k == cmdLower {
			h := help
			found = &h
			break
		}
		for _, alias := range help.Aliases {
			if alias == cmdLower {
				h := help
				found = &h
				break
			}
		}
		if found != nil {
			break
		}
	}

	if found == nil {
		p.Out(p.Yellow(fmt.Sprintf("Unknown command '%s'.", cmdName)))
		p.Out("Run 'hit help' to see all available commands.")
		return
	}

	// Print structured help
	p.Out(fmt.Sprintf("%s - %s", p.Bold("hit "+found.Name), found.Summary))
	if len(found.Aliases) > 0 {
		p.Out(p.Dim(fmt.Sprintf("Aliases: %s", strings.Join(found.Aliases, ", "))))
	}

	p.Out(fmt.Sprintf("\n%s\n  %s", p.Bold("Usage:"), found.Usage))

	if len(found.Flags) > 0 {
		p.Out(fmt.Sprintf("\n%s", p.Bold("Flags & Options:")))
		for _, f := range found.Flags {
			p.Out(fmt.Sprintf("  %s", f))
		}
	}

	if len(found.Examples) > 0 {
		p.Out(fmt.Sprintf("\n%s", p.Bold("Examples:")))
		for _, ex := range found.Examples {
			p.Out(fmt.Sprintf("  %s", ex))
		}
	}

	if len(found.SeeAlso) > 0 {
		p.Out(fmt.Sprintf("\n%s", p.Bold("See Also:")))
		p.Out(fmt.Sprintf("  hit help %s", strings.Join(found.SeeAlso, ", hit help ")))
	}
}

// hasHelpFlag returns true if args contains --help or -h.
func hasHelpFlag(args []string) bool {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			return true
		}
	}
	return false
}
