package main

import (
	_ "embed"
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hit-endpoint/hit-endpoint/internal/assertions"
	"github.com/hit-endpoint/hit-endpoint/internal/cost"
	"github.com/hit-endpoint/hit-endpoint/internal/diff"
	"github.com/hit-endpoint/hit-endpoint/internal/flows"
	"github.com/hit-endpoint/hit-endpoint/internal/fuzz"
	"github.com/hit-endpoint/hit-endpoint/internal/graphql"
	"github.com/hit-endpoint/hit-endpoint/internal/history"
	"github.com/hit-endpoint/hit-endpoint/internal/hub"
	"github.com/hit-endpoint/hit-endpoint/internal/importer"
	"github.com/hit-endpoint/hit-endpoint/internal/learn"
	"github.com/hit-endpoint/hit-endpoint/internal/mcp"
	"github.com/hit-endpoint/hit-endpoint/internal/mock"
	"github.com/hit-endpoint/hit-endpoint/internal/output"
	"github.com/hit-endpoint/hit-endpoint/internal/perf"
	"github.com/hit-endpoint/hit-endpoint/internal/policy"
	"github.com/hit-endpoint/hit-endpoint/internal/probe"
	"github.com/hit-endpoint/hit-endpoint/internal/report"
	"github.com/hit-endpoint/hit-endpoint/internal/runner"
	"github.com/hit-endpoint/hit-endpoint/internal/sanity"
	"github.com/hit-endpoint/hit-endpoint/internal/schedule"
	"github.com/hit-endpoint/hit-endpoint/internal/schema"
	"github.com/hit-endpoint/hit-endpoint/internal/shorthand"
	"github.com/hit-endpoint/hit-endpoint/internal/snippet"
	"github.com/hit-endpoint/hit-endpoint/internal/spec"
	"github.com/hit-endpoint/hit-endpoint/internal/stream"
	"github.com/hit-endpoint/hit-endpoint/internal/telemetry"
	"github.com/hit-endpoint/hit-endpoint/internal/templating"
	"github.com/hit-endpoint/hit-endpoint/internal/types"
	"github.com/hit-endpoint/hit-endpoint/internal/zone"

	"gopkg.in/yaml.v3"
)

//go:embed reference.md
var referenceDoc string

const (
	Version   = "0.1.0"
	ExitOK    = 0
	ExitFail  = 1
	ExitUsage = 2
)

type GlobalFlags struct {
	Zone   string
	Server string
	Vars      map[string]any
	NoColor   bool
	Insecure  bool
	NoHistory bool
}

func parseKV(items []string, sep string) (map[string]any, error) {
	out := make(map[string]any)
	for _, it := range items {
		idx := strings.Index(it, sep)
		if idx < 0 {
			return nil, zone.NewZoneError("expected key%svalue, got '%s'", sep, it)
		}
		k := strings.TrimSpace(it[:idx])
		v := coerce(strings.TrimSpace(it[idx+len(sep):]))
		out[k] = v
	}
	return out, nil
}

func coerce(text string) any {
	if text == "true" {
		return true
	}
	if text == "false" {
		return false
	}
	if text == "null" {
		return nil
	}
	if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") || strings.HasPrefix(text, `"`) {
		var j any
		if err := json.Unmarshal([]byte(text), &j); err == nil {
			return j
		}
	}
	if i, err := strconv.Atoi(text); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(text, 64); err == nil {
		return f
	}
	return text
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	globals := GlobalFlags{
		Vars: make(map[string]any),
	}

	var nonGlobalArgs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--version" || arg == "-V":
			fmt.Printf("hit %s\n", Version)
			return ExitOK
		case arg == "--help" || arg == "-h":
			printHelp(nil)
			return ExitOK
		case arg == "-z" || arg == "--zone" || arg == "-w":
			if i+1 < len(args) {
				globals.Zone = args[i+1]
				i++
			}
		case strings.HasPrefix(arg, "--zone="):
			globals.Zone = strings.TrimPrefix(arg, "--zone=")
		case arg == "-s" || arg == "--server" || arg == "-e":
			if i+1 < len(args) {
				globals.Server = args[i+1]
				i++
			}
		case strings.HasPrefix(arg, "--server="):
			globals.Server = strings.TrimPrefix(arg, "--server=")
		case arg == "--var":
			if i+1 < len(args) {
				kv, err := parseKV([]string{args[i+1]}, "=")
				if err == nil {
					for k, v := range kv {
						globals.Vars[k] = v
					}
				}
				i++
			}
		case strings.HasPrefix(arg, "--var="):
			kv, err := parseKV([]string{strings.TrimPrefix(arg, "--var=")}, "=")
			if err == nil {
				for k, v := range kv {
					globals.Vars[k] = v
				}
			}
		case arg == "--no-color":
			globals.NoColor = true
		case arg == "-k" || arg == "--insecure":
			globals.Insecure = true
		case arg == "--no-history":
			globals.NoHistory = true
		default:
			nonGlobalArgs = append(nonGlobalArgs, arg)
		}
	}

	colorFlag := !globals.NoColor
	printer := output.NewPrinter(&colorFlag, os.Stdout)

	if len(nonGlobalArgs) == 0 {
		printHelp(printer)
		return ExitUsage
	}

	cmd := nonGlobalArgs[0]
	cmdArgs := nonGlobalArgs[1:]

	if isAdhocInvocation(cmd, nonGlobalArgs) {
		cmd = "response"
		cmdArgs = nonGlobalArgs
	} else if !isKnownSubcommand(cmd) {
		zoneRoot := globals.Zone
		if zoneRoot == "" {
			if z, _ := zone.FindOrNone(""); z != nil {
				zoneRoot = z.Root
			}
		}
		if sh, ok := shorthand.Get(zoneRoot, cmd); ok {
			if sh.Server != "" && globals.Server == "" {
				globals.Server = sh.Server
			}
			if sh.Ref != "" {
				cmd = "run"
				cmdArgs = append([]string{sh.Ref}, cmdArgs...)
			} else if sh.URL != "" {
				cmd = "response"
				var expanded []string
				method := sh.MethodDisplay()
				expanded = append(expanded, method, sh.URL)
				for k, v := range sh.Headers {
					expanded = append(expanded, "-H", fmt.Sprintf("%s: %s", k, v))
				}
				for k, v := range sh.Query {
					expanded = append(expanded, "-q", fmt.Sprintf("%s=%s", k, v))
				}
				if sh.Body != "" {
					expanded = append(expanded, "-b", sh.Body)
				}
				cmdArgs = append(expanded, cmdArgs...)
			}
		}
	}

	err := dispatch(cmd, cmdArgs, &globals, printer)
	if err != nil {
		fmt.Fprintln(os.Stderr, printer.Red(fmt.Sprintf("error: %v", err)))
		return ExitUsage
	}
	return ExitOK
}

func isHTTPMethod(s string) bool {
	switch strings.ToUpper(s) {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS":
		return true
	}
	return false
}

func isURLOrPath(s string) bool {
	return strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "/") ||
		strings.HasPrefix(s, "localhost:") ||
		strings.HasPrefix(s, "127.0.0.1:") ||
		strings.Contains(s, "://")
}

func isKnownSubcommand(s string) bool {
	switch s {
	case "help", "--help", "-h",
		"wizard", "zone",
		"reference", "docs",
		"schema",
		"mcp",
		"history",
		"replay",
		"diff",
		"report",
		"assert",
		"init",
		"body",
		"response",
		"code",
		"status",
		"time",
		"headers",
		"mock", "serve",
		"hub", "cloud",
		"ls",
		"envs", "servers",
		"vars",
		"show",
		"run",
		"perf",
		"import",
		"validate",
		"sanity",
		"new",
		"snippet", "code-block",
		"fuzz",
		"policy", "lint",
		"learn", "tutorial",
		"graphql", "gql",
		"sse",
		"ws", "websocket",
		"schedule",
		"probe",
		"dispatch",
		"cost", "estimate",
		"shorthand", "shorthands", "alias":
		return true
	}
	return false
}

func findFirstPositional(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			switch a {
			case "-H", "--header", "-q", "--query", "-f", "--form",
				"-b", "--body", "-j", "--json-body", "--auth",
				"-m", "--method", "-v", "--vars", "--variables",
				"--expect", "--event",
				"--test", "--capture", "--status", "-w", "-z", "--zone",
				"-e", "-s", "--server", "--var", "--junit", "--webhook-on-failure", "--sarif", "--html", "--har", "-o", "--output",
				"-c", "-n", "-d", "--rps", "--warmup", "--ramp-up", "--threshold", "--max-body",
				"--at", "--start", "--every", "--interval", "-i", "--count":
				i++
			}
			continue
		}
		return a
	}
	return ""
}

func isAdhocInvocation(cmd string, args []string) bool {
	if isKnownSubcommand(cmd) {
		return false
	}
	if isHTTPMethod(cmd) || isURLOrPath(cmd) {
		return true
	}
	if strings.HasPrefix(cmd, "-") {
		firstPos := findFirstPositional(args)
		if firstPos != "" && (isHTTPMethod(firstPos) || isURLOrPath(firstPos)) {
			return true
		}
	}
	return false
}

func printHelp(p *output.Printer) {
	fmt.Print(`Hit Endpoint - A file-based, git-friendly API tester and runner.

Usage:
  hit [METHOD] URL [-H k:v] [-q k=v] [-j JSON|@file] [-b TEXT|@file] [--auth ...]
  hit body [METHOD] URL ...
  hit code [METHOD] URL ...
  hit time [METHOD] URL [--ms]
  hit headers [METHOD] URL ...
  hit response [METHOD] URL ...
  hit run REF... [-s SERVER] [--junit FILE] [--webhook-on-failure URL] [--publish] [--enforce-policy]
                 [--format=github|agent] [--html FILE] [--har FILE] [--var k=v]
                 [--json] [--body] [--code] [--time] [-v] [--headers] [--quiet] [--full]
  hit schedule <URL|REF> [--at TIME] [--every INTERVAL] [-n COUNT] [--status CODE] [--expect PATTERN]
  hit probe [run|daemon|test-alert] <FILE|REF> [--regions LIST] [--consensus N] [--interval D] [--json]
  hit shorthand [ls] [--json]
  hit shorthand set <NAME> <URL|REF> [-m METHOD] [-s SERVER] [-H k:v]
  hit shorthand rm <NAME>
  hit ls [FOLDER] [--json]
  hit servers
  hit vars [set K V | unset K | clear] [--all]
  hit show REF [--curl] [--json] [--lang python|php|js|go] [--extract PATH]
  hit snippet <REF|URL> [--lang python|php|js|go|all] [--extract PATH]
  hit fuzz <REF|URL> [-n N] [--categories CATS] [--fail-on-5xx] [--sarif FILE] [--json] [-v]
  hit validate [REF|FOLDER ...] [--json]
  hit sanity [COLLECTION] [--offline] [--json]
  perf REF [-c N] [-n N | -d SECONDS] [--workers LIST] [--distribute N] [--threshold SPEC] [--check] [--junit FILE] [--csv FILE] [--json]
  hit perf worker [--port PORT] [--region REGION]
  hit history [ls] [--limit N] [--status N] [--ref PATTERN] [--json]
  hit history show <ID|INDEX> [--json]
  hit history save <ID|INDEX> <FILE.yaml> [--infer]
  hit history clear
  hit replay <ID|INDEX> [-s SERVER] [--var k=v] [--diff] [--headers] [--body-only] [--json]
  hit diff <ID|INDEX> [<ID|INDEX>] [--headers] [--body-only] [--json] [--context N]
  hit report html [-o FILE] [-n LIMIT]
  hit report har [-o FILE] [-n LIMIT] [--status N] [--ref PATTERN]
  hit report coverage --openapi SPEC [--json]
  hit report latency [-t THRESHOLD%] [--json]
  hit assert <URL|ID|REF> [--save [FILE]] [--append [FILE]] [--strict] [--no-latency] [--no-headers] [--json]
  hit graphql <URL|REF> [-q QUERY|@file] [-v VARS|@file] [-o OP] [--introspect] [--sdl] [--fail-on-errors] [--json]
  hit sse <URL> [-H k:v] [-n MAX] [-d TIMEOUT] [--event NAME] [--json]
  hit ws <URL> [-H k:v] [-m MSG]... [--expect PATTERN] [-d TIMEOUT] [--json]
  hit mock [PORT] [--openapi SPEC] [--stateless] [--flaky RATE] [--rate-limit N/s] [--latency D] [--jitter RANGE] [--auth-expire D]
  hit hub [--port PORT] [--dir DIR] [--api-key KEY] [--project ID]
  hit cost [<REF|FILE>] [-n CALLS] [--tiers SPEC] [--pricing FILE] [--preset NAME] [--budget AMOUNT]
  hit mcp
  hit schema [request|chain|zone|graphql <URL>]
  hit new REF [-m METHOD] [-u URL] [--infer]
  hit import [collection|openapi|curl|env|globals] FILE...
  hit wizard [DIR] [--name NAME] [--url URL] [-y]
  hit zone new [DIR] [--name NAME] [--url URL] [-y]
  hit init [DIR] [--wizard]
  hit learn [verify [1-7|all]] [--mock URL]
  hit reference

Global options:
  -z, --zone DIR        Zone directory
  -s, --server SERVER   Server name
  --var KEY=VALUE       Override variable (repeatable)
  -k, --insecure        Skip TLS certificate verification
  --no-color            Disable colored output
  --no-history          Do not record requests to history
`)
}

func dispatch(cmd string, args []string, globals *GlobalFlags, p *output.Printer) error {
	switch cmd {
	case "help", "--help", "-h":
		printHelp(p)
		return nil

	case "learn", "tutorial":
		return cmdLearn(args, globals, p)

	case "snippet", "code-block":
		return cmdSnippet(args, globals, p)

	case "fuzz":
		return cmdFuzz(args, globals, p)

	case "graphql", "gql":
		return cmdGraphQL(args, globals, p)

	case "sse":
		return cmdSSE(args, globals, p)

	case "ws", "websocket":
		return cmdWS(args, globals, p)

	case "reference", "docs":
		p.Out(strings.TrimSpace(referenceDoc))
		return nil

	case "schema":
		if len(args) > 0 && (args[0] == "graphql" || args[0] == "gql") {
			return cmdSchemaGraphQL(args[1:], globals, p)
		}
		kind := "request"
		if len(args) > 0 {
			kind = args[0]
		}
		s, err := schema.GetSchema(kind)
		if err != nil {
			return err
		}
		p.Out(s)
		return nil

	case "mcp":
		return mcp.ServeStdio(os.Stdin, os.Stdout, globals.Zone)

	case "history":
		return cmdHistory(args, globals, p)

	case "replay":
		return cmdReplay(args, globals, p)

	case "diff":
		return cmdDiff(args, globals, p)

	case "report":
		return cmdReport(args, globals, p)

	case "schedule":
		return cmdSchedule(args, globals, p)

	case "probe":
		return cmdProbe(args, globals, p)

	case "shorthand", "shorthands", "alias":
		return cmdShorthand(args, globals, p)

	case "assert":
		return cmdAssert(args, globals, p)

	case "wizard":
		return cmdWizard(args, globals, p)

	case "zone":
		if len(args) > 0 {
			sub := strings.ToLower(args[0])
			if sub == "new" || sub == "init" || sub == "wizard" || sub == "create" {
				return cmdWizard(args[1:], globals, p)
			}
		}
		p.Out("Usage: hit zone new [DIR] [--name NAME] [--url URL] [-y]")
		return nil

	case "init":
		for _, a := range args {
			if a == "--wizard" || a == "-w" || a == "wizard" {
				return cmdWizard(args, globals, p)
			}
		}
		dir := "."
		name := ""
		for i := 0; i < len(args); i++ {
			if args[i] == "--name" && i+1 < len(args) {
				name = args[i+1]
				i++
			} else if !strings.HasPrefix(args[i], "-") {
				dir = args[i]
			}
		}
		z, err := zone.Init(dir, name)
		if err != nil {
			return err
		}
		p.Out(fmt.Sprintf("Created zone '%s' in %s", z.Name(), z.Root))
		p.Out("  edit servers/dev.yaml, then try:  hit run example")
		return nil

	case "body", "response", "code", "status", "time", "headers":
		return cmdAdhoc(cmd, args, globals, p)

	case "mock", "serve":
		return cmdMock(args, p)

	case "hub", "cloud":
		return cmdHub(args, p)

	case "dispatch":
		return cmdDispatch(args, globals, p)

	case "cost", "estimate":
		return cmdCost(args, globals, p)

	case "perf":
		if len(args) > 0 && (args[0] == "worker" || args[0] == "--worker") {
			return cmdPerfWorker(args[1:], globals, p)
		}
	}

	z, err := zone.Find(globals.Zone)
	if err != nil {
		return err
	}

	switch cmd {
	case "ls":
		return cmdLs(args, globals, p, z)
	case "servers", "envs":
		defaultServer, _ := z.Config["default_server"].(string)
		for _, name := range z.ServerNames() {
			mark := ""
			if name == defaultServer {
				mark = " (default)"
			}
			p.Out(fmt.Sprintf("%s%s", name, mark))
		}
		return nil
	case "vars":
		return cmdVars(args, globals, p, z)
	case "show":
		return cmdShow(args, globals, p, z)
	case "run":
		return cmdRun(args, globals, p, z)
	case "perf":
		return cmdPerf(args, globals, p, z)
	case "import":
		return cmdImport(args, globals, p, z)
	case "validate":
		return cmdValidate(args, globals, p, z)
	case "sanity":
		return cmdSanity(args, globals, p, z)
	case "policy", "lint":
		return cmdPolicy(args, globals, p, z)
	case "new":
		return cmdNew(args, globals, p, z)
	default:
		if targetPath, err := z.ResolveRequest(cmd); err == nil && targetPath != "" {
			return cmdRun(append([]string{cmd}, args...), globals, p, z)
		}
		if chainPath, err := z.ResolveChain(cmd); err == nil && chainPath != "" {
			return cmdRun(append([]string{cmd}, args...), globals, p, z)
		}
		return zone.NewZoneError("unknown command '%s'", cmd)
	}
}

func readArg(val string) (string, error) {
	if strings.HasPrefix(val, "@") {
		b, err := os.ReadFile(val[1:])
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	return val, nil
}

func cmdLearn(args []string, globals *GlobalFlags, p *output.Printer) error {
	if len(args) > 0 && args[0] == "verify" {
		return cmdLearnVerify(args[1:], globals, p)
	}

	p.Out(p.Bold("🎓 hit API Testing Learning Sandbox & Curriculum"))
	p.Out("")
	p.Out("An offline, interactive curriculum designed to teach API testing fundamentals,")
	p.Out("authentication, assertions, scenario flows, performance benchmarking, and contracts.")
	p.Out("")
	p.Out(p.Cyan("Offline Mock Server:") + " Start the local sandbox API anytime:")
	p.Out("  hit mock 8765 &")
	p.Out("")
	p.Out(p.Bold("Curriculum Lessons & Reference (under learn/):"))
	p.Out("  📖 learn/glossary.md                     Comprehensive A-Z API & testing encyclopedia")
	p.Out("  🧪 learn/01-http-basics.md               HTTP methods, URLs, status codes & output modes")
	p.Out("  📦 learn/02-headers-and-payloads.md      Query params, JSON bodies, forms & content types")
	p.Out("  🔐 learn/03-authentication-and-state.md  Bearer auth, secret masking & dynamic captures")
	p.Out("  🎯 learn/04-assertions-and-validations.md Status checks, JMESPath & negative testing")
	p.Out("  🔄 learn/05-scenario-flows.md            Multi-step user journeys & CRUD lifecycles")
	p.Out("  ⚡ learn/06-performance-and-slas.md       Concurrency, RPS, p95/p99 percentiles & SLO gates")
	p.Out("  📜 learn/07-contracts-and-drift.md       OpenAPI 3.x contract coverage & drift audits")
	p.Out("")
	p.Out(p.Cyan("Interactive Verifier:") + " Test your progress on exercises:")
	p.Out("  hit learn verify [lesson-number | all]")
	p.Out("")
	p.Out("Read the overview: " + p.Dim("learn/README.md"))
	return nil
}

func cmdLearnVerify(args []string, globals *GlobalFlags, p *output.Printer) error {
	mockURL := "http://127.0.0.1:8765"
	targetLesson := 0 // 0 means all

	for i := 0; i < len(args); i++ {
		a := args[i]
		if (a == "--mock" || a == "-m") && i+1 < len(args) {
			mockURL = args[i+1]
			i++
		} else if strings.HasPrefix(a, "--mock=") {
			mockURL = strings.TrimPrefix(a, "--mock=")
		} else if a == "all" {
			targetLesson = 0
		} else if num, err := strconv.Atoi(a); err == nil && num >= 1 && num <= 7 {
			targetLesson = num
		}
	}

	z, _ := zone.FindOrNone(globals.Zone)
	v := learn.NewVerifier(z, mockURL)

	p.Out(p.Bold("🎓 API Testing Sandbox — Lesson Verification"))
	p.Out(p.Dim("---------------------------------------------"))

	if targetLesson > 0 {
		res := v.VerifyLesson(targetLesson)
		printLessonResult(res, p)
		if !res.Passed {
			return zone.NewZoneError("lesson %d not completed yet", targetLesson)
		}
		return nil
	}

	report := v.VerifyAll()
	for _, l := range report.Lessons {
		printLessonResult(l, p)
		p.Out("")
	}

	p.Out(p.Dim("---------------------------------------------"))
	scoreColor := p.Green
	if report.ScorePercent < 50.0 {
		scoreColor = p.Red
	} else if report.ScorePercent < 100.0 {
		scoreColor = p.Yellow
	}
	p.Out(scoreColor(fmt.Sprintf("Score: %d/%d Lessons Completed (%.0f%%)",
		report.CompletedLessons, report.TotalLessons, report.ScorePercent)))

	if report.CompletedLessons < report.TotalLessons {
		return zone.NewZoneError("%d lessons still incomplete", report.TotalLessons-report.CompletedLessons)
	}
	return nil
}

func printLessonResult(l *learn.LessonResult, p *output.Printer) {
	if l.Passed {
		p.Out(p.Green(fmt.Sprintf("[PASS] Lesson %d: %s", l.LessonNumber, l.Title)))
	} else {
		p.Out(p.Red(fmt.Sprintf("[FAIL] Lesson %d: %s", l.LessonNumber, l.Title)))
	}

	for _, c := range l.Checks {
		if c.Passed {
			p.Out(p.Green("       ✓ ") + c.Description + p.Dim(" ("+c.Detail+")"))
		} else {
			p.Out(p.Red("       ✗ ") + c.Description + p.Dim(" ("+c.Detail+")"))
		}
	}

	if !l.Passed && l.Hint != "" {
		p.Out(p.Yellow("       💡 Hint: ") + l.Hint)
	}
}

func cmdAdhoc(cmd string, args []string, globals *GlobalFlags, p *output.Printer) error {
	var (
		method       = "GET"
		reqURL       string
		headersList  []string
		queryList    []string
		formList     []string
		bodyRaw      string
		jsonBodyRaw  string
		authStr      string
		testList     []string
		captureList  []string
		statusFilter int
		jsonOutput   bool
		verbose      bool
		noPersist    bool
		agentFormat  bool
		rawTime      bool
	)

	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "-H" || a == "--header") && i+1 < len(args):
			headersList = append(headersList, args[i+1])
			i++
		case (a == "-q" || a == "--query") && i+1 < len(args):
			queryList = append(queryList, args[i+1])
			i++
		case (a == "-f" || a == "--form") && i+1 < len(args):
			formList = append(formList, args[i+1])
			i++
		case (a == "-b" || a == "--body") && i+1 < len(args):
			bodyRaw = args[i+1]
			i++
		case (a == "-j" || a == "--json-body") && i+1 < len(args):
			jsonBodyRaw = args[i+1]
			i++
		case a == "--auth" && i+1 < len(args):
			authStr = args[i+1]
			i++
		case a == "--test" && i+1 < len(args):
			testList = append(testList, args[i+1])
			i++
		case a == "--capture" && i+1 < len(args):
			captureList = append(captureList, args[i+1])
			i++
		case a == "--status" && i+1 < len(args):
			statusFilter, _ = strconv.Atoi(args[i+1])
			i++
		case a == "--json":
			jsonOutput = true
		case a == "--format=agent" || a == "--agent":
			agentFormat = true
		case a == "-v" || a == "--verbose":
			verbose = true
		case a == "--no-persist":
			noPersist = true
		case a == "--code" || a == "--status-only":
			cmd = "code"
		case a == "--time":
			cmd = "time"
		case a == "--body":
			cmd = "body"
		case a == "--headers":
			cmd = "headers"
		case a == "--ms" || a == "--raw":
			rawTime = true
		default:
			if !strings.HasPrefix(a, "-") {
				positional = append(positional, a)
			}
		}
	}

	if len(positional) == 0 {
		return zone.NewZoneError("usage: hit %s [METHOD] URL", cmd)
	} else if len(positional) == 1 {
		reqURL = positional[0]
	} else {
		method = strings.ToUpper(positional[0])
		reqURL = positional[1]
	}

	z, _ := zone.FindOrNone(globals.Zone)
	zoneRoot := globals.Zone
	if zoneRoot == "" && z != nil {
		zoneRoot = z.Root
	}
	if len(positional) == 1 && !strings.Contains(reqURL, "://") && !strings.HasPrefix(reqURL, "/") {
		if sh, ok := shorthand.Get(zoneRoot, reqURL); ok {
			if sh.Server != "" && globals.Server == "" {
				globals.Server = sh.Server
			}
			for k, v := range sh.Headers {
				headersList = append(headersList, fmt.Sprintf("%s: %s", k, v))
			}
			for k, v := range sh.Query {
				queryList = append(queryList, fmt.Sprintf("%s=%s", k, v))
			}
			if bodyRaw == "" && jsonBodyRaw == "" && sh.Body != "" {
				bodyRaw = sh.Body
			}
			if sh.URL != "" {
				reqURL = sh.URL
				if sh.Method != "" {
					method = sh.MethodDisplay()
				}
			} else if sh.Ref != "" {
				reqURL = sh.Ref
			}
		}
	}

	sess, err := runner.NewSession(runner.SessionOptions{
		Zone:              z,
		ServerName:        globals.Server,
		ExtraVars:         globals.Vars,
		Persist:           !noPersist,
		ZoneOptional:      true,
		Verify:            !globals.Insecure,
		NoHistory:         globals.NoHistory,
	})
	if err != nil {
		return err
	}
	defer sess.Close()

	headers, err := parseKV(headersList, ":")
	if err != nil {
		return err
	}
	query, err := parseKV(queryList, "=")
	if err != nil {
		return err
	}
	form, err := parseKV(formList, "=")
	if err != nil {
		return err
	}
	captures, err := parseKV(captureList, "=")
	if err != nil {
		return err
	}

	var body any
	var jsonBody any
	if jsonBodyRaw != "" {
		text, err := readArg(jsonBodyRaw)
		if err != nil {
			return err
		}
		var parsed any
		if err := json.Unmarshal([]byte(text), &parsed); err != nil {
			return err
		}
		jsonBody = parsed
	} else if bodyRaw != "" {
		text, err := readArg(bodyRaw)
		if err != nil {
			return err
		}
		body = map[string]any{"raw": text}
	}

	var auth any
	if authStr != "" {
		parts := strings.SplitN(authStr, ":", 2)
		kind := strings.ToLower(parts[0])
		switch kind {
		case "bearer":
			rest := ""
			if len(parts) > 1 {
				rest = parts[1]
			}
			auth = map[string]any{"type": "bearer", "token": rest}
		case "basic":
			user, pass := "", ""
			if len(parts) > 1 {
				up := strings.SplitN(parts[1], ":", 2)
				user = up[0]
				if len(up) > 1 {
					pass = up[1]
				}
			}
			auth = map[string]any{"type": "basic", "username": user, "password": pass}
		case "none":
			auth = "none"
		default:
			return zone.NewZoneError("--auth must be bearer:TOKEN, basic:USER:PASS or none")
		}
	}

	var tests []any
	for _, t := range testList {
		tests = append(tests, map[string]any{"expr": t})
	}
	if statusFilter > 0 {
		tests = append(tests, map[string]any{"status": statusFilter})
	}

	var r *types.Result
	if z != nil && len(positional) == 1 && !strings.Contains(reqURL, "://") && !strings.HasPrefix(reqURL, "/") {
		if specPath, err := z.ResolveRequest(reqURL); err == nil && specPath != "" {
			sp, err := spec.LoadSpec(specPath, z.DefaultsChain(specPath))
			if err == nil {
				stepOverrides := make(map[string]any)
				if len(headers) > 0 {
					stepOverrides["headers"] = headers
				}
				if len(query) > 0 {
					stepOverrides["query"] = query
				}
				r = sess.RunSpec(sp, stepOverrides)
			}
		}
	}
	if r == nil {
		r = sess.Request(method, reqURL, headers, query, body, jsonBody, form, auth, tests, captures, nil, "ad hoc")
	}
	mask := sess.Context(nil, nil).Mask

	if jsonOutput {
		b, _ := json.MarshalIndent(r.ToDict(), "", "  ")
		p.Out(string(b))
	} else if agentFormat {
		output.PrintAgentResult(r, os.Stdout)
	} else if (cmd == "code" || cmd == "status") && !verbose {
		if r.Error != "" {
			fmt.Fprintln(os.Stderr, p.Red(r.Error))
		} else {
			p.Out(strconv.Itoa(r.Status))
			for _, t := range r.FailedTests() {
				fmt.Fprintln(os.Stderr, p.Red(fmt.Sprintf("✗ %s %s", t.Name, t.Detail)))
			}
		}
	} else if cmd == "time" && !verbose {
		if r.Error != "" {
			fmt.Fprintln(os.Stderr, p.Red(r.Error))
		} else {
			if rawTime {
				p.Out(fmt.Sprintf("%.1f", r.ElapsedMs))
			} else if r.ElapsedMs < 10 {
				p.Out(fmt.Sprintf("%.1fms", r.ElapsedMs))
			} else if r.ElapsedMs < 1000 {
				p.Out(fmt.Sprintf("%.0fms", r.ElapsedMs))
			} else {
				p.Out(fmt.Sprintf("%.2fs", r.ElapsedMs/1000.0))
			}
			for _, t := range r.FailedTests() {
				fmt.Fprintln(os.Stderr, p.Red(fmt.Sprintf("✗ %s %s", t.Name, t.Detail)))
			}
		}
	} else if cmd == "headers" && !verbose {
		if r.Error != "" {
			fmt.Fprintln(os.Stderr, p.Red(r.Error))
		} else {
			p.Out(fmt.Sprintf("HTTP/1.1 %d %s", r.Status, r.Reason))
			for k, v := range r.Headers {
				p.Out(fmt.Sprintf("%s: %s", k, v))
			}
		}
	} else if cmd == "body" && !verbose {
		if r.Error != "" {
			fmt.Fprintln(os.Stderr, p.Red(r.Error))
		} else {
			p.Out(output.FormatBody(r))
			for _, t := range r.FailedTests() {
				fmt.Fprintln(os.Stderr, p.Red(fmt.Sprintf("✗ %s %s", t.Name, t.Detail)))
			}
			for k, v := range r.Captures {
				fmt.Fprintln(os.Stderr, p.Dim(fmt.Sprintf("↳ %s = %v", k, v)))
			}
		}
	} else {
		p.Result(r, verbose, 0, true, false, mask)
	}

	if r.Error != "" {
		os.Exit(ExitFail)
	}
	if len(r.Tests) > 0 {
		if len(r.FailedTests()) > 0 {
			os.Exit(ExitFail)
		}
	} else if cmd != "code" && cmd != "status" && cmd != "time" {
		if r.Status >= 400 {
			os.Exit(ExitFail)
		}
	}
	return nil
}

func cmdLs(args []string, globals *GlobalFlags, p *output.Printer, z *zone.Zone) error {
	prefix := ""
	jsonOutput := false
	for _, a := range args {
		if a == "--json" {
			jsonOutput = true
		} else if !strings.HasPrefix(a, "-") {
			prefix = a
		}
	}

	base := z.CollectionsDir()
	if prefix != "" {
		var err error
		base, err = z.ResolveRequest(prefix)
		if err != nil {
			return err
		}
		fi, err := os.Stat(base)
		if err == nil && !fi.IsDir() {
			base = filepath.Dir(base)
		}
	}

	if jsonOutput {
		listing := map[string]any{
			"zone":                z.Name(),
			"root":                z.Root,
			"default_environment": z.Config["default_environment"],
			"servers":             z.ServerNames(),
			"requests":            []any{},
			"flows":               []any{},
		}
		var reqs []any
		for _, path := range z.ListRequests(base) {
			rel, _ := filepath.Rel(z.Root, path)
			sp, err := spec.LoadSpec(path, nil)
			if err == nil {
				reqs = append(reqs, map[string]any{
					"ref":    z.RequestRef(path),
					"name":   sp.Name,
					"method": sp.Method,
					"url":    sp.Url,
					"file":   rel,
				})
			} else {
				reqs = append(reqs, map[string]any{
					"ref":   z.RequestRef(path),
					"error": err.Error(),
					"file":  rel,
				})
			}
		}
		listing["requests"] = reqs
		var fls []any
		for _, f := range z.ListChains() {
			rel, _ := filepath.Rel(z.ChainsDir(), f)
			stem := strings.TrimSuffix(rel, filepath.Ext(rel))
			fls = append(fls, stem)
		}
		listing["flows"] = fls
		listing["chains"] = fls

		b, _ := json.MarshalIndent(listing, "", "  ")
		p.Out(string(b))
		return nil
	}

	p.Out(p.Bold(fmt.Sprintf("zone %s  (%s)", z.Name(), z.Root)))
	servers := z.ServerNames()
	serversStr := strings.Join(servers, ", ")
	if serversStr == "" {
		serversStr = p.Dim("none")
	}
	p.Out(p.Bold("servers: ") + serversStr)
	p.Out(p.Bold("requests:"))
	for _, path := range z.ListRequests(base) {
		sp, err := spec.LoadSpec(path, nil)
		ref := z.RequestRef(path)
		if err == nil {
			p.Out(fmt.Sprintf("  %-50s %-6s %s", ref, p.Cyan(sp.Method), p.Dim(sp.Url)))
		} else {
			p.Out(fmt.Sprintf("  %-50s %s %v", ref, p.Red("invalid"), err))
		}
	}
	chainsList := z.ListChains()
	if len(chainsList) > 0 && prefix == "" {
		p.Out(p.Bold("chains:"))
		for _, f := range chainsList {
			rel, _ := filepath.Rel(z.ChainsDir(), f)
			p.Out("  " + strings.TrimSuffix(rel, filepath.Ext(rel)))
		}
	}
	return nil
}

func cmdVars(args []string, globals *GlobalFlags, p *output.Printer, z *zone.Zone) error {
	action := ""
	key := ""
	val := ""
	all := false

	var positional []string
	for _, a := range args {
		if a == "--all" {
			all = true
		} else if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
		}
	}
	if len(positional) > 0 {
		action = positional[0]
	}
	if len(positional) > 1 {
		key = positional[1]
	}
	if len(positional) > 2 {
		val = positional[2]
	}

	sess, err := runner.NewSession(runner.SessionOptions{
		Zone: z,
		ServerName: globals.Server,
		ExtraVars: globals.Vars,
		Persist:   true,
		Verify:    !globals.Insecure,
	})
	if err != nil {
		return err
	}
	defer sess.Close()

	switch action {
	case "set":
		if key == "" {
			return zone.NewZoneError("usage: hit vars set KEY VALUE")
		}
		sess.SetVar(key, coerce(val), nil)
		p.Out(fmt.Sprintf("%s = %v", key, sess.SessionVars[key]))
		return nil
	case "unset":
		if key == "" {
			return zone.NewZoneError("usage: hit vars unset KEY")
		}
		if sess.State != nil {
			sess.State.Unset(key)
			_ = sess.State.Save()
		}
		return nil
	case "clear":
		if sess.State != nil {
			sess.State.Clear()
		}
		p.Out(fmt.Sprintf("cleared captured state for server '%s'", sess.Server.Name))
		return nil
	}

	ctx := sess.Context(nil, nil)
	var values map[string]any
	if all {
		values = ctx.Flat()
	} else {
		values = sess.SessionVars
	}

	if len(values) == 0 {
		p.Out(p.Dim(fmt.Sprintf("no captured variables for server '%s' (use --all for every layer)", sess.Server.Name)))
		return nil
	}

	maxWidth := 0
	for k := range values {
		if len(k) > maxWidth {
			maxWidth = len(k)
		}
	}
	var sortedKeys []string
	for k := range values {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	for _, k := range sortedKeys {
		v := values[k]
		vStr := fmt.Sprintf("%v", v)
		if _, isStr := v.(string); !isStr {
			if b, err := json.Marshal(v); err == nil {
				vStr = string(b)
			}
		}
		shown := ctx.Mask(vStr)
		src := ""
		if all {
			src = p.Dim(fmt.Sprintf("  [%s]", ctx.SourceOf(k)))
		}
		p.Out(fmt.Sprintf("%-*s  %s%s", maxWidth, k, shown, src))
	}
	return nil
}

func cmdShow(args []string, globals *GlobalFlags, p *output.Printer, z *zone.Zone) error {
	ref := ""
	asCurl := false
	asJSON := false
	lang := ""
	extractPath := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--curl":
			asCurl = true
		case a == "--json":
			asJSON = true
		case (a == "--lang" || a == "-l") && i+1 < len(args):
			lang = args[i+1]
			i++
		case strings.HasPrefix(a, "--lang="):
			lang = strings.TrimPrefix(a, "--lang=")
		case a == "--python" || a == "--py":
			lang = "python"
		case a == "--php":
			lang = "php"
		case a == "--js" || a == "--javascript" || a == "--node":
			lang = "javascript"
		case a == "--go" || a == "--golang":
			lang = "go"
		case (a == "--extract" || a == "-x") && i+1 < len(args):
			extractPath = args[i+1]
			i++
		case strings.HasPrefix(a, "--extract="):
			extractPath = strings.TrimPrefix(a, "--extract=")
		default:
			if !strings.HasPrefix(a, "-") {
				ref = a
			}
		}
	}
	if ref == "" {
		return zone.NewZoneError("usage: hit show REF [--curl] [--json] [--lang python|php|js|go] [--extract PATH]")
	}

	sess, err := runner.NewSession(runner.SessionOptions{
		Zone: z,
		ServerName: globals.Server,
		ExtraVars: globals.Vars,
		Persist:   false,
		Verify:    !globals.Insecure,
	})
	if err != nil {
		return err
	}
	defer sess.Close()

	sp, err := sess.Load(ref)
	if err != nil {
		return err
	}
	rendered, prepared, ctx, err := sess.Prepare(sp, nil)
	if err != nil {
		return err
	}

	if lang != "" {
		reqInfo := snippet.RequestInfo{
			Method:      prepared.Method,
			URL:         prepared.FullURL(),
			Headers:     prepared.Headers,
			Body:        prepared.Content,
			ExtractPath: extractPath,
		}
		if strings.ToLower(lang) == "all" {
			p.Out(snippet.GenerateAll(reqInfo))
			return nil
		}
		code, err := snippet.GenerateSnippet(lang, reqInfo)
		if err != nil {
			return err
		}
		p.Out(code)
		return nil
	}

	if asCurl {
		p.Out(prepared.ToCurl(ctx))
		return nil
	}
	if asJSON {
		maskedHeaders := make(map[string]string)
		for k, v := range prepared.Headers {
			maskedHeaders[k] = ctx.Mask(v)
		}
		outData := map[string]any{
			"name":     rendered.Name,
			"method":   prepared.Method,
			"url":      prepared.FullURL(),
			"headers":  maskedHeaders,
			"body":     prepared.BodyPreview,
			"tests":    rendered.Tests,
			"captures": rendered.Captures,
		}
		b, _ := json.MarshalIndent(outData, "", "  ")
		p.Out(string(b))
		return nil
	}

	p.Out(p.Bold(rendered.Name) + p.Dim(fmt.Sprintf("  (%s)", sess.RefOf(sp))))
	if rendered.Description != "" {
		p.Out(p.Dim(rendered.Description))
	}
	p.Out(fmt.Sprintf("%s %s", p.Cyan(prepared.Method), prepared.FullURL()))
	var sortedHeaders []string
	for k := range prepared.Headers {
		sortedHeaders = append(sortedHeaders, k)
	}
	sort.Strings(sortedHeaders)
	for _, k := range sortedHeaders {
		p.Out(fmt.Sprintf("  %s: %s", k, ctx.Mask(prepared.Headers[k])))
	}
	if prepared.BodyPreview != "" {
		p.Out("")
		p.Out(ctx.Mask(prepared.BodyPreview))
	}
	if len(rendered.Tests) > 0 {
		p.Out(p.Bold("tests:"))
		y, _ := zone.DumpYAML(map[string]any{"tests": rendered.Tests})
		p.Out(strings.TrimSpace(y))
	}
	if len(rendered.Captures) > 0 {
		p.Out(p.Bold("captures:"))
		for k, v := range rendered.Captures {
			p.Out(fmt.Sprintf("  %s ← %v", k, v))
		}
	}
	return nil
}

func cmdRun(args []string, globals *GlobalFlags, p *output.Printer, z *zone.Zone) error {
	var (
		refs         []string
		jsonOutput   bool
		bodyOnly     bool
		codeOnly     bool
		timeOnly     bool
		showHeaders  bool
		verbose      bool
		quietMode    bool
		fullOutput   bool
		maxBody      = 4000
		failFast     bool
		keepGoing    bool
		noPersist    bool
		lenient      bool
		headerFlags  []string
		queryFlags   []string
		junitPath    string
		webhookURL   string
		publish      bool
		publishURL   string
		enforcePolicy bool
		githubFormat bool
		agentFormat  bool
		htmlPath     string
		harPath      string
	)

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
			jsonOutput = true
		case a == "--body":
			bodyOnly = true
		case a == "--code" || a == "--status-only":
			codeOnly = true
		case a == "--time":
			timeOnly = true
		case a == "--headers":
			showHeaders = true
		case a == "-v" || a == "--verbose":
			verbose = true
		case a == "--quiet":
			quietMode = true
		case a == "--full":
			fullOutput = true
		case a == "--max-body" && i+1 < len(args):
			maxBody, _ = strconv.Atoi(args[i+1])
			i++
		case a == "--fail-fast":
			failFast = true
		case a == "--keep-going":
			keepGoing = true
		case a == "--no-persist":
			noPersist = true
		case a == "--lenient":
			lenient = true
		case a == "--junit" && i+1 < len(args):
			junitPath = args[i+1]
			i++
		case strings.HasPrefix(a, "--junit="):
			junitPath = strings.TrimPrefix(a, "--junit=")
		case a == "--webhook-on-failure" && i+1 < len(args):
			webhookURL = args[i+1]
			i++
		case strings.HasPrefix(a, "--webhook-on-failure="):
			webhookURL = strings.TrimPrefix(a, "--webhook-on-failure=")
		case a == "--publish":
			publish = true
		case strings.HasPrefix(a, "--publish="):
			publish = true
			publishURL = strings.TrimPrefix(a, "--publish=")
		case a == "--enforce-policy":
			enforcePolicy = true
		case a == "--format=github" || a == "--github":
			githubFormat = true
		case a == "--format=agent" || a == "--agent":
			agentFormat = true
		case a == "--html" && i+1 < len(args):
			htmlPath = args[i+1]
			i++
		case a == "--har" && i+1 < len(args):
			harPath = args[i+1]
			i++
		case (a == "-H" || a == "--header") && i+1 < len(args):
			headerFlags = append(headerFlags, args[i+1])
			i++
		case (a == "-q" || a == "--query") && i+1 < len(args):
			queryFlags = append(queryFlags, args[i+1])
			i++
		default:
			if !strings.HasPrefix(a, "-") {
				refs = append(refs, a)
			}
		}
	}

	if len(refs) == 0 {
		return zone.NewZoneError("usage: hit run REF...")
	}

	if fullOutput {
		maxBody = 0
	}
	effectiveQuiet := quietMode || len(refs) > 1

	sess, err := runner.NewSession(runner.SessionOptions{
		Zone: z,
		ServerName: globals.Server,
		ExtraVars: globals.Vars,
		Persist:   !noPersist,
		Strict:    !lenient,
		Verify:    !globals.Insecure,
		NoHistory: globals.NoHistory,
	})
	if err != nil {
		return err
	}
	defer sess.Close()

	showResult := func(r *types.Result) {
		if jsonOutput || bodyOnly || codeOnly || timeOnly {
			return
		}
		if agentFormat {
			output.PrintAgentResult(r, os.Stdout)
			return
		}
		p.Result(r, verbose, maxBody, showHeaders, effectiveQuiet, sess.Context(nil, nil).Mask)
	}

	stepOverrides := make(map[string]any)
	if len(headerFlags) > 0 {
		h, err := parseKV(headerFlags, ":")
		if err == nil {
			stepOverrides["headers"] = h
		}
	}
	if len(queryFlags) > 0 {
		q, err := parseKV(queryFlags, "=")
		if err == nil {
			stepOverrides["query"] = q
		}
	}

	var results []*types.Result
	var flowErrors []string

	for _, ref := range refs {
		flowPath, err := z.ResolveChain(ref)
		if err == nil && flowPath != "" {
			flowData, err := flows.LoadFlow(flowPath)
			if err != nil {
				return err
			}
			savedQuiet := effectiveQuiet
			effectiveQuiet = !fullOutput
			fr := flows.RunFlow(sess, flowData, showResult, func(msg string) {
				if !jsonOutput && !bodyOnly && !agentFormat {
					p.Out(p.Dim(fmt.Sprintf("  · %s", msg)))
				}
			}, !keepGoing, 0)
			effectiveQuiet = savedQuiet
			results = append(results, fr.Results...)
			if fr.Error != "" {
				flowErrors = append(flowErrors, fmt.Sprintf("%s: %s", fr.Name, fr.Error))
				if !jsonOutput && !bodyOnly && !agentFormat {
					p.Out(p.Red(fmt.Sprintf("flow stopped: %s", fr.Error)))
				}
			}
			continue
		}

		targetPath, err := z.ResolveRequest(ref)
		if err != nil {
			return err
		}
		fi, err := os.Stat(targetPath)
		if err == nil && fi.IsDir() {
			savedQuiet := effectiveQuiet
			effectiveQuiet = !fullOutput
			folderResults := sess.RunFolder(targetPath, showResult, failFast)
			effectiveQuiet = savedQuiet
			results = append(results, folderResults...)
			continue
		}

		sp, err := spec.LoadSpec(targetPath, z.DefaultsChain(targetPath))
		if err != nil {
			return err
		}
		r := sess.RunSpec(sp, stepOverrides)
		results = append(results, r)
		showResult(r)
	}

	allOK := len(flowErrors) == 0
	for _, r := range results {
		if codeOnly || timeOnly {
			if r.Error != "" {
				allOK = false
				break
			}
		} else if !r.OK() {
			allOK = false
			break
		}
	}

	if enforcePolicy {
		zRoot := ""
		if z != nil {
			zRoot = z.Root
		}
		if pol, err := policy.LoadPolicy(zRoot); err == nil && pol.MaxLatency > 0 {
			maxMs := float64(pol.MaxLatency.Milliseconds())
			for _, r := range results {
				if r.ElapsedMs > maxMs {
					allOK = false
					if !jsonOutput && !bodyOnly && !codeOnly && !timeOnly && !agentFormat {
						p.Out(p.Red(fmt.Sprintf("✗ Policy breach: %s latency %.1fms exceeds policy ceiling %.0fms", r.Ref, r.ElapsedMs, maxMs)))
					}
				}
			}
		}
	}

	if jsonOutput {
		if len(results) == 1 {
			b, _ := json.MarshalIndent(results[0].ToDict(), "", "  ")
			p.Out(string(b))
		} else {
			var list []any
			for _, r := range results {
				list = append(list, r.ToDict())
			}
			b, _ := json.MarshalIndent(list, "", "  ")
			p.Out(string(b))
		}
	} else if codeOnly {
		for _, r := range results {
			if r.Error != "" {
				fmt.Fprintln(os.Stderr, p.Red(r.Error))
			} else {
				p.Out(strconv.Itoa(r.Status))
			}
		}
	} else if timeOnly {
		for _, r := range results {
			if r.Error != "" {
				fmt.Fprintln(os.Stderr, p.Red(r.Error))
			} else {
				if r.ElapsedMs < 10 {
					p.Out(fmt.Sprintf("%.1fms", r.ElapsedMs))
				} else if r.ElapsedMs < 1000 {
					p.Out(fmt.Sprintf("%.0fms", r.ElapsedMs))
				} else {
					p.Out(fmt.Sprintf("%.2fs", r.ElapsedMs/1000.0))
				}
			}
		}
	} else if bodyOnly {
		for _, r := range results {
			if r.Error != "" {
				fmt.Fprintln(os.Stderr, r.Error)
			} else {
				p.Out(output.FormatBody(r))
			}
		}
	} else if agentFormat {
		output.PrintAgentSummary(results, os.Stdout)
	} else if len(results) > 1 {
		p.Summary(results)
	}

	if junitPath != "" {
		if err := report.WriteJUnitXML(results, junitPath); err != nil {
			fmt.Fprintf(os.Stderr, "error writing junit xml: %v\n", err)
		} else if !jsonOutput && !bodyOnly && !codeOnly && !timeOnly && !agentFormat {
			p.Out(p.Green(fmt.Sprintf("✓ JUnit XML report written to %s", junitPath)))
		}
	}
	if githubFormat || os.Getenv("GITHUB_ACTIONS") == "true" {
		output.EmitGitHubAnnotations(results, os.Stdout)
		_ = output.AppendGitHubStepSummary(results)
	}
	if htmlPath != "" {
		if err := report.WriteHTMLReport(results, fmt.Sprintf("hit run: %s", strings.Join(refs, ", ")), htmlPath); err != nil {
			fmt.Fprintf(os.Stderr, "error writing html report: %v\n", err)
		} else {
			p.Out(p.Green(fmt.Sprintf("✓ HTML report written to %s", htmlPath)))
		}
	}
	if harPath != "" {
		if err := report.WriteHAR(report.BuildHARFromResults(results), harPath); err != nil {
			fmt.Fprintf(os.Stderr, "error writing har archive: %v\n", err)
		} else {
			p.Out(p.Green(fmt.Sprintf("✓ HAR archive written to %s", harPath)))
		}
	}

	if !publish && os.Getenv("HIT_PUBLISH") == "true" {
		publish = true
	}

	if publish {
		apiKey := os.Getenv("HIT_API_KEY")
		projectID := os.Getenv("HIT_PROJECT_ID")
		cloudURL := publishURL
		if cloudURL == "" {
			cloudURL = os.Getenv("HIT_CLOUD_URL")
		}
		if cloudURL == "" {
			cloudURL = telemetry.DefaultCloudURL
		}

		cfg := telemetry.PublishConfig{
			APIKey:    apiKey,
			ProjectID: projectID,
			CloudURL:  cloudURL,
		}

		maskFn := sess.Context(nil, nil).Mask
		payload := telemetry.BuildTelemetryPayload(results, flowErrors, z, globals.Server, maskFn)

		res, err := telemetry.PublishRun(context.Background(), nil, cfg, payload)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to publish telemetry to Hit Cloud: %v\n", err)
		} else if !jsonOutput && !bodyOnly && !codeOnly && !timeOnly && !agentFormat {
			p.Out(p.Green(fmt.Sprintf("✓ Telemetry published to Hit Cloud: %s", res.DashboardURL)))
		}
	}

	if webhookURL == "" {
		webhookURL = os.Getenv("HIT_WEBHOOK_URL")
	}

	if !allOK {
		if webhookURL != "" {
			zName := ""
			if z != nil {
				zName = z.Name()
			}
			if err := report.SendFailureWebhook(webhookURL, results, flowErrors, zName, globals.Server); err != nil {
				fmt.Fprintf(os.Stderr, "error delivering failure webhook: %v\n", err)
			} else if !jsonOutput && !bodyOnly && !codeOnly && !timeOnly && !agentFormat {
				p.Out(p.Green(fmt.Sprintf("✓ Failure webhook delivered to %s", webhookURL)))
			}
		}
		os.Exit(ExitFail)
	}
	return nil
}

func cmdMock(args []string, p *output.Printer) error {
	port := 8765
	var chaos mock.ChaosOptions
	var openapiSpec string
	stateless := false

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "-p" || a == "--port") && i+1 < len(args):
			if val, err := strconv.Atoi(args[i+1]); err == nil && val > 0 {
				port = val
			}
			i++
		case strings.HasPrefix(a, "--port="):
			if val, err := strconv.Atoi(strings.TrimPrefix(a, "--port=")); err == nil && val > 0 {
				port = val
			}
		case (a == "--openapi" || a == "-o") && i+1 < len(args):
			openapiSpec = args[i+1]
			i++
		case strings.HasPrefix(a, "--openapi="):
			openapiSpec = strings.TrimPrefix(a, "--openapi=")
		case a == "--stateless":
			stateless = true
		case a == "--flaky" && i+1 < len(args):
			rate, err := mock.ParseRate(args[i+1])
			if err != nil {
				return zone.NewZoneError("invalid --flaky rate: %v", err)
			}
			chaos.FlakyRate = rate
			i++
		case strings.HasPrefix(a, "--flaky="):
			rate, err := mock.ParseRate(strings.TrimPrefix(a, "--flaky="))
			if err != nil {
				return zone.NewZoneError("invalid --flaky rate: %v", err)
			}
			chaos.FlakyRate = rate
		case a == "--flaky-status" && i+1 < len(args):
			if code, err := strconv.Atoi(args[i+1]); err == nil && code >= 400 && code <= 599 {
				chaos.FlakyStatus = code
			} else {
				return zone.NewZoneError("invalid --flaky-status: must be HTTP 4xx or 5xx code")
			}
			i++
		case strings.HasPrefix(a, "--flaky-status="):
			if code, err := strconv.Atoi(strings.TrimPrefix(a, "--flaky-status=")); err == nil && code >= 400 && code <= 599 {
				chaos.FlakyStatus = code
			} else {
				return zone.NewZoneError("invalid --flaky-status: must be HTTP 4xx or 5xx code")
			}
		case (a == "-r" || a == "--rate-limit") && i+1 < len(args):
			limit, window, err := mock.ParseRateLimit(args[i+1])
			if err != nil {
				return zone.NewZoneError("invalid --rate-limit: %v", err)
			}
			chaos.RateLimit = limit
			chaos.RateWindow = window
			i++
		case strings.HasPrefix(a, "--rate-limit="):
			limit, window, err := mock.ParseRateLimit(strings.TrimPrefix(a, "--rate-limit="))
			if err != nil {
				return zone.NewZoneError("invalid --rate-limit: %v", err)
			}
			chaos.RateLimit = limit
			chaos.RateWindow = window
		case a == "--latency" && i+1 < len(args):
			d, err := time.ParseDuration(args[i+1])
			if err != nil {
				return zone.NewZoneError("invalid --latency duration: %v", err)
			}
			chaos.Latency = d
			i++
		case strings.HasPrefix(a, "--latency="):
			d, err := time.ParseDuration(strings.TrimPrefix(a, "--latency="))
			if err != nil {
				return zone.NewZoneError("invalid --latency duration: %v", err)
			}
			chaos.Latency = d
		case a == "--jitter" && i+1 < len(args):
			minD, maxD, err := mock.ParseJitter(args[i+1])
			if err != nil {
				return zone.NewZoneError("invalid --jitter range: %v", err)
			}
			chaos.JitterMin = minD
			chaos.JitterMax = maxD
			i++
		case strings.HasPrefix(a, "--jitter="):
			minD, maxD, err := mock.ParseJitter(strings.TrimPrefix(a, "--jitter="))
			if err != nil {
				return zone.NewZoneError("invalid --jitter range: %v", err)
			}
			chaos.JitterMin = minD
			chaos.JitterMax = maxD
		case a == "--auth-expire" && i+1 < len(args):
			d, err := time.ParseDuration(args[i+1])
			if err != nil {
				return zone.NewZoneError("invalid --auth-expire duration: %v", err)
			}
			chaos.AuthExpire = d
			i++
		case strings.HasPrefix(a, "--auth-expire="):
			d, err := time.ParseDuration(strings.TrimPrefix(a, "--auth-expire="))
			if err != nil {
				return zone.NewZoneError("invalid --auth-expire duration: %v", err)
			}
			chaos.AuthExpire = d
		case a == "--corrupt" && i+1 < len(args):
			rate, err := mock.ParseRate(args[i+1])
			if err != nil {
				return zone.NewZoneError("invalid --corrupt rate: %v", err)
			}
			chaos.CorruptRate = rate
			i++
		case strings.HasPrefix(a, "--corrupt="):
			rate, err := mock.ParseRate(strings.TrimPrefix(a, "--corrupt="))
			if err != nil {
				return zone.NewZoneError("invalid --corrupt rate: %v", err)
			}
			chaos.CorruptRate = rate
		default:
			if !strings.HasPrefix(a, "-") {
				if val, err := strconv.Atoi(a); err == nil && val > 0 {
					port = val
				}
			}
		}
	}

	var server *mock.Server
	if openapiSpec != "" {
		var err error
		server, err = mock.NewOpenAPIServer(openapiSpec, chaos, !stateless)
		if err != nil {
			return zone.NewZoneError("failed to load OpenAPI spec: %v", err)
		}
		engine := server.OpenAPIEngine()
		title := engine.Title()
		version := engine.Version()
		versionStr := ""
		if version != "" {
			versionStr = " " + version
		}
		p.Out(fmt.Sprintf("hit dynamic openapi mock listening on http://127.0.0.1:%d  (Ctrl+C to stop)", port))
		p.Out(p.Cyan(fmt.Sprintf("📄 Specification: %s%s (%d routes loaded)", title, versionStr, engine.RouteCount())))
		if !stateless {
			p.Out(p.Dim("💾 State: in-memory CRUD enabled (use --stateless to disable)"))
		} else {
			p.Out(p.Dim("💾 State: stateless mode"))
		}
	} else {
		server = mock.NewChaosServer(chaos)
		p.Out(fmt.Sprintf("mock petstore listening on http://127.0.0.1:%d  (Ctrl+C to stop)", port))
	}

	if chaos.IsEnabled() {
		p.Out(p.Yellow(fmt.Sprintf("⚡ Chaos enabled: %s", chaos.Summary())))
	}
	return server.ListenAndServe(port)
}

func cmdPerf(args []string, globals *GlobalFlags, p *output.Printer, z *zone.Zone) error {
	if len(args) > 0 && (args[0] == "worker" || args[0] == "--worker") {
		return cmdPerfWorker(args[1:], globals, p)
	}

	ref := ""
	opts := perf.PerfOptions{
		Concurrency: 10,
	}
	jsonOutput := false
	thresholdStr := ""
	junitPath := ""
	workersStr := ""
	distributeCount := 0
	hubURL := os.Getenv("HIT_HUB_URL")
	hubToken := os.Getenv("HIT_HUB_TOKEN")
	region := ""

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "-c" || a == "--concurrency") && i+1 < len(args):
			opts.Concurrency, _ = strconv.Atoi(args[i+1])
			i++
		case (a == "-n" || a == "--requests") && i+1 < len(args):
			opts.Total, _ = strconv.Atoi(args[i+1])
			i++
		case (a == "-d" || a == "--duration") && i+1 < len(args):
			opts.Duration, _ = strconv.ParseFloat(args[i+1], 64)
			i++
		case a == "--rps" && i+1 < len(args):
			opts.RPS, _ = strconv.ParseFloat(args[i+1], 64)
			i++
		case a == "--warmup" && i+1 < len(args):
			opts.Warmup, _ = strconv.Atoi(args[i+1])
			i++
		case a == "--ramp-up" && i+1 < len(args):
			opts.RampUp, _ = strconv.ParseFloat(args[i+1], 64)
			i++
		case (a == "--threshold" || a == "--thresholds") && i+1 < len(args):
			thresholdStr = args[i+1]
			i++
		case a == "--check":
			opts.Check = true
		case a == "--junit" && i+1 < len(args):
			junitPath = args[i+1]
			i++
		case strings.HasPrefix(a, "--junit="):
			junitPath = strings.TrimPrefix(a, "--junit=")
		case a == "--csv" && i+1 < len(args):
			opts.CsvPath = args[i+1]
			i++
		case a == "--workers" && i+1 < len(args):
			workersStr = args[i+1]
			i++
		case strings.HasPrefix(a, "--workers="):
			workersStr = strings.TrimPrefix(a, "--workers=")
		case a == "--distribute" && i+1 < len(args):
			distributeCount, _ = strconv.Atoi(args[i+1])
			i++
		case strings.HasPrefix(a, "--distribute="):
			distributeCount, _ = strconv.Atoi(strings.TrimPrefix(a, "--distribute="))
		case a == "--hub" && i+1 < len(args):
			hubURL = args[i+1]
			i++
		case strings.HasPrefix(a, "--hub="):
			hubURL = strings.TrimPrefix(a, "--hub=")
		case a == "--hub-token" && i+1 < len(args):
			hubToken = args[i+1]
			i++
		case strings.HasPrefix(a, "--hub-token="):
			hubToken = strings.TrimPrefix(a, "--hub-token=")
		case a == "--region" && i+1 < len(args):
			region = args[i+1]
			i++
		case strings.HasPrefix(a, "--region="):
			region = strings.TrimPrefix(a, "--region=")
		case a == "--worker":
			return cmdPerfWorker(args[i+1:], globals, p)
		case a == "--json":
			jsonOutput = true
		default:
			if !strings.HasPrefix(a, "-") {
				ref = a
			}
		}
	}

	if ref == "" {
		return zone.NewZoneError("usage: hit perf REF [-c N] [-n N] [-d SECONDS] [--workers URLS] [--hub URL] [--distribute N] [--threshold SPEC]")
	}

	sess, err := runner.NewSession(runner.SessionOptions{
		Zone:         z,
		ServerName:   globals.Server,
		ExtraVars:    globals.Vars,
		Persist:      false,
		ZoneOptional: true,
		Verify:       !globals.Insecure,
		NoHistory:    true,
	})
	if err != nil {
		return err
	}
	defer sess.Close()

	opts.Progress = func(completed int, elapsed float64) {
		if !jsonOutput && p.Color {
			fmt.Fprintf(os.Stderr, "\r  %d done, %.0fs elapsed", completed, elapsed)
		}
	}

	var report *types.PerfReport
	var distReport *perf.DistributedPerfReport

	if workersStr != "" || distributeCount > 0 || hubURL != "" {
		var workerList []string
		if workersStr != "" {
			for _, w := range strings.Split(workersStr, ",") {
				trimmed := strings.TrimSpace(w)
				if trimmed != "" {
					workerList = append(workerList, trimmed)
				}
			}
		}

		var sp *spec.RequestSpec
		if z != nil {
			if flowPath, err := z.ResolveChain(ref); err == nil && flowPath != "" {
				return fmt.Errorf("distributed load testing currently supports request specs (chains coming soon)")
			}
			sp, _ = sess.Load(ref)
		}
		if sp == nil {
			if directSp, dErr := spec.LoadSpec(ref, nil); dErr == nil {
				sp = directSp
			} else {
				return fmt.Errorf("failed to load request spec %q", ref)
			}
		}

		// Pre-render request with coordinator session variables and base URL
		renderedSp, prep, _, pErr := sess.Prepare(sp, nil)
		if pErr == nil && prep != nil {
			spCopy := *renderedSp
			spCopy.Url = prep.FullURL()
			sp = &spCopy
		}

		coordOpts := &perf.CoordinatorOptions{
			HubURL:   hubURL,
			HubToken: hubToken,
			Region:   region,
			OnProgress: func(completed int, aggregateRPS float64, wm []perf.WorkerMetrics) {
				if !jsonOutput && p.Color {
					fmt.Fprintf(os.Stderr, "\r  %d done, %.1f req/s across %d workers", completed, aggregateRPS, len(wm))
				}
			},
		}

		ctx := context.Background()
		if opts.Duration > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, time.Duration(opts.Duration*1.5)*time.Second)
			defer cancel()
		}

		if !jsonOutput {
			targetStr := fmt.Sprintf("%d requests", opts.Total)
			if opts.Total <= 0 && opts.Duration > 0 {
				targetStr = fmt.Sprintf("%.1f s", opts.Duration)
			}
			workerDesc := fmt.Sprintf("%d in-process workers", distributeCount)
			if len(workerList) > 0 {
				workerDesc = fmt.Sprintf("%d cluster workers (%s)", len(workerList), strings.Join(workerList, ", "))
			} else if hubURL != "" {
				workerDesc = fmt.Sprintf("auto-discovered cluster workers from hub %s", hubURL)
				if region != "" {
					workerDesc += fmt.Sprintf(" (region: %s)", region)
				}
			}
			p.Out(p.Dim(fmt.Sprintf("distributed load testing %s: %s, concurrency %d, %s",
				sess.RefOf(sp), targetStr, opts.Concurrency, workerDesc)))
		}

		if distributeCount > 0 {
			distReport, err = perf.RunDistributedLocal(ctx, distributeCount, sp, opts, coordOpts)
		} else {
			distReport, err = perf.DistributeJob(ctx, workerList, sp, opts, coordOpts)
		}
		if err != nil {
			return err
		}
		report = distReport.Global
	} else {
		// Single-node execution path
		flowPath := ""
		if z != nil {
			flowPath, _ = z.ResolveChain(ref)
		}
		if flowPath != "" {
			flowData, err := flows.LoadFlow(flowPath)
			if err != nil {
				return err
			}

			if !jsonOutput {
				targetStr := fmt.Sprintf("%d iterations", opts.Total)
				if opts.Total <= 0 && opts.Duration > 0 {
					targetStr = fmt.Sprintf("%.1f s", opts.Duration)
				}
				rpsStr := ""
				if opts.RPS > 0 {
					rpsStr = fmt.Sprintf(", rps cap %.1f", opts.RPS)
				}
				p.Out(p.Dim(fmt.Sprintf("load testing flow %s: %s, concurrency %d%s", ref, targetStr, opts.Concurrency, rpsStr)))
			}

			report, err = perf.RunFlowPerf(sess, ref, flowData, opts)
			if err != nil {
				return err
			}
		} else {
			var sp *spec.RequestSpec
			if z != nil {
				sp, _ = sess.Load(ref)
			}
			if sp == nil {
				if directSp, dErr := spec.LoadSpec(ref, nil); dErr == nil {
					sp = directSp
				} else {
					return fmt.Errorf("failed to load %q", ref)
				}
			}

			if !jsonOutput {
				targetStr := fmt.Sprintf("%d requests", opts.Total)
				if opts.Total <= 0 && opts.Duration > 0 {
					targetStr = fmt.Sprintf("%.1f s", opts.Duration)
				}
				rpsStr := ""
				if opts.RPS > 0 {
					rpsStr = fmt.Sprintf(", rps cap %.1f", opts.RPS)
				}
				p.Out(p.Dim(fmt.Sprintf("load testing %s: %s, concurrency %d%s", sess.RefOf(sp), targetStr, opts.Concurrency, rpsStr)))
			}

			report, err = perf.RunPerf(sess, sp, opts)
			if err != nil {
				return err
			}
		}
	}

	if !jsonOutput && p.Color {
		fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", 50))
	}

	if jsonOutput {
		var dict map[string]any
		if distReport != nil {
			b, _ := json.Marshal(distReport)
			_ = json.Unmarshal(b, &dict)
		} else {
			dict = report.ToDict()
		}
		if thresholdStr != "" {
			rules, _ := perf.ParseThresholds(thresholdStr)
			resList := perf.EvaluateThresholds(report, rules)
			var trList []map[string]any
			allPassed := true
			for _, tr := range resList {
				if !tr.Passed {
					allPassed = false
				}
				trList = append(trList, map[string]any{
					"metric": tr.Rule.Metric,
					"op":     tr.Rule.Op,
					"target": tr.Rule.Target,
					"actual": tr.Actual,
					"passed": tr.Passed,
				})
			}
			dict["thresholds"] = trList
			dict["thresholds_passed"] = allPassed
		}
		b, _ := json.MarshalIndent(dict, "", "  ")
		p.Out(string(b))
	} else {
		p.Perf(report)
		if distReport != nil && len(distReport.Workers) > 0 {
			p.Out("")
			p.Out(p.Bold("  Worker Breakdown:"))
			for _, w := range distReport.Workers {
				passPct := 100.0
				if w.Report.Completed > 0 {
					passPct = (float64(w.Report.OK) / float64(w.Report.Completed)) * 100.0
				}
				p.Out(fmt.Sprintf("   • %-16s [%-12s]  %d reqs (%.0f%% ok)  p95: %6.1fms  throughput: %6.1f req/s",
					w.WorkerID, w.Region, w.Report.Completed, passPct, w.Report.Percentile(0.95), w.Report.RPS()))
			}
		}
		if opts.CsvPath != "" {
			p.Out(p.Dim(fmt.Sprintf("  per-request rows written to %s", opts.CsvPath)))
		}
	}

	thresholdsPassed := true
	if thresholdStr != "" {
		rules, err := perf.ParseThresholds(thresholdStr)
		if err != nil {
			p.Out(p.Red(fmt.Sprintf("error parsing thresholds: %v", err)))
			thresholdsPassed = false
		} else {
			results := perf.EvaluateThresholds(report, rules)
			for _, r := range results {
				if r.Passed {
					p.Out(p.Green(fmt.Sprintf("  ✓ threshold passed: %s", r.Message)))
				} else {
					thresholdsPassed = false
					p.Out(p.Red(fmt.Sprintf("  ✗ threshold breached: %s", r.Message)))
				}
			}
		}
	}

	if junitPath != "" {
		thresholdErr := ""
		if !thresholdsPassed {
			thresholdErr = "one or more SLA thresholds were breached"
		}
		if err := output.WritePerfJUnitXML(report, thresholdsPassed, thresholdErr, junitPath); err != nil {
			fmt.Fprintf(os.Stderr, "error writing perf junit xml: %v\n", err)
		} else if !jsonOutput {
			p.Out(p.Green(fmt.Sprintf("  ✓ JUnit XML written to %s", junitPath)))
		}
	}

	if report.Failed > 0 || !thresholdsPassed {
		os.Exit(ExitFail)
	}
	return nil
}

func cmdPerfWorker(args []string, globals *GlobalFlags, p *output.Printer) error {
	port := 8989
	region := "local"
	id := ""
	hubURL := os.Getenv("HIT_HUB_URL")
	hubToken := os.Getenv("HIT_HUB_TOKEN")
	advertiseURL := ""
	capacity := 0
	owner := os.Getenv("USER")
	if owner == "" {
		owner = "perf-worker"
	}

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "-p" || a == "--port") && i+1 < len(args):
			port, _ = strconv.Atoi(args[i+1])
			i++
		case strings.HasPrefix(a, "--port="):
			port, _ = strconv.Atoi(strings.TrimPrefix(a, "--port="))
		case a == "--region" && i+1 < len(args):
			region = args[i+1]
			i++
		case strings.HasPrefix(a, "--region="):
			region = strings.TrimPrefix(a, "--region=")
		case a == "--id" && i+1 < len(args):
			id = args[i+1]
			i++
		case strings.HasPrefix(a, "--id="):
			id = strings.TrimPrefix(a, "--id=")
		case a == "--hub" && i+1 < len(args):
			hubURL = args[i+1]
			i++
		case strings.HasPrefix(a, "--hub="):
			hubURL = strings.TrimPrefix(a, "--hub=")
		case a == "--hub-token" && i+1 < len(args):
			hubToken = args[i+1]
			i++
		case strings.HasPrefix(a, "--hub-token="):
			hubToken = strings.TrimPrefix(a, "--hub-token=")
		case a == "--advertise-url" && i+1 < len(args):
			advertiseURL = args[i+1]
			i++
		case strings.HasPrefix(a, "--advertise-url="):
			advertiseURL = strings.TrimPrefix(a, "--advertise-url=")
		case a == "--capacity" && i+1 < len(args):
			capacity, _ = strconv.Atoi(args[i+1])
			i++
		case strings.HasPrefix(a, "--capacity="):
			capacity, _ = strconv.Atoi(strings.TrimPrefix(a, "--capacity="))
		case a == "--owner" && i+1 < len(args):
			owner = args[i+1]
			i++
		case strings.HasPrefix(a, "--owner="):
			owner = strings.TrimPrefix(a, "--owner=")
		}
	}

	ws := perf.NewWorkerServerWithConfig(perf.WorkerServerConfig{
		ID:           id,
		Region:       region,
		Port:         port,
		HubURL:       hubURL,
		HubToken:     hubToken,
		AdvertiseURL: advertiseURL,
		Capacity:     capacity,
		Owner:        owner,
	})

	p.Out(fmt.Sprintf("%s Starting Hit Load Worker [%s] (region: %s) on port %d...", p.Cyan("⚡"), p.Bold(ws.ID), p.Green(region), port))
	if hubURL != "" {
		p.Out(fmt.Sprintf("   Fleet Enrollment: Enrolled in Hub %s (heartbeat every 15s)", p.Cyan(hubURL)))
	}
	p.Out(p.Dim("   Ready to execute coordinator workloads. Press Ctrl+C to terminate."))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errChan := make(chan error, 1)
	go func() {
		errChan <- ws.Start(nil)
	}()

	select {
	case <-ctx.Done():
		p.Out("\nShutting down worker daemon...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = ws.Stop(shutdownCtx)
		return nil
	case err := <-errChan:
		return err
	}
}

func cmdHub(args []string, p *output.Printer) error {
	cfg := hub.Config{
		Addr:           ":8080",
		DashboardTitle: "Hit Cloud Fleet Hub",
	}

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "-p" || a == "--port") && i+1 < len(args):
			port, _ := strconv.Atoi(args[i+1])
			cfg.Addr = fmt.Sprintf(":%d", port)
			i++
		case strings.HasPrefix(a, "--port="):
			port, _ := strconv.Atoi(strings.TrimPrefix(a, "--port="))
			cfg.Addr = fmt.Sprintf(":%d", port)
		case (a == "-d" || a == "--dir") && i+1 < len(args):
			cfg.DataDir = args[i+1]
			i++
		case strings.HasPrefix(a, "--dir="):
			cfg.DataDir = strings.TrimPrefix(a, "--dir=")
		case (a == "-k" || a == "--api-key") && i+1 < len(args):
			cfg.APIKey = args[i+1]
			i++
		case strings.HasPrefix(a, "--api-key="):
			cfg.APIKey = strings.TrimPrefix(a, "--api-key=")
		case a == "--project" && i+1 < len(args):
			cfg.ProjectID = args[i+1]
			i++
		case strings.HasPrefix(a, "--project="):
			cfg.ProjectID = strings.TrimPrefix(a, "--project=")
		case a == "--url" && i+1 < len(args):
			cfg.PublicURL = args[i+1]
			i++
		case strings.HasPrefix(a, "--url="):
			cfg.PublicURL = strings.TrimPrefix(a, "--url=")
		case !strings.HasPrefix(a, "-"):
			if port, err := strconv.Atoi(a); err == nil && port > 0 {
				cfg.Addr = fmt.Sprintf(":%d", port)
			}
		}
	}

	if cfg.DataDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cfg.DataDir = filepath.Join(home, ".hit", "hub")
		}
	} else if strings.HasPrefix(cfg.DataDir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			cfg.DataDir = filepath.Join(home, cfg.DataDir[2:])
		}
	}

	server, err := hub.NewServer(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize hub: %w", err)
	}

	displayURL := server.URL()
	p.Out(fmt.Sprintf("%s Starting Hit Cloud Fleet Hub on %s...", p.Cyan("🚀"), p.Bold(cfg.Addr)))
	p.Out(fmt.Sprintf("  • Web Dashboard:    %s", p.Cyan(displayURL+"/")))
	p.Out(fmt.Sprintf("  • Telemetry Ingest: %s", p.Dim(displayURL+"/api/v1/runs")))
	if cfg.DataDir != "" {
		p.Out(fmt.Sprintf("  • Storage:          %s", p.Dim(cfg.DataDir)))
	}
	if cfg.APIKey != "" {
		p.Out(fmt.Sprintf("  • Auth:             %s", p.Green("Bearer token enabled")))
	} else {
		p.Out(fmt.Sprintf("  • Auth:             %s", p.Yellow("Open (no API key required)")))
	}
	p.Out(p.Dim("  Press Ctrl+C to stop."))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start()
	}()

	select {
	case <-ctx.Done():
		p.Out("\nShutting down Fleet Hub...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errChan:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}
}

func cmdDispatch(args []string, globals *GlobalFlags, p *output.Printer) error {
	hubURL := os.Getenv("HIT_HUB_URL")
	if hubURL == "" {
		hubURL = "http://127.0.0.1:8080"
	}
	hubToken := os.Getenv("HIT_HUB_TOKEN")
	if hubToken == "" {
		hubToken = os.Getenv("HIT_API_KEY")
	}

	target := ""
	targetNode := ""
	targetRegion := ""
	targetRole := ""
	workloadType := ""
	runName := ""
	concurrency := 0
	requests := 0
	var durationSec float64
	streamOutput := true
	jsonOutput := false
	timeout := 2 * time.Minute

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--hub" && i+1 < len(args):
			hubURL = args[i+1]
			i++
		case strings.HasPrefix(a, "--hub="):
			hubURL = strings.TrimPrefix(a, "--hub=")
		case a == "--hub-token" && i+1 < len(args):
			hubToken = args[i+1]
			i++
		case strings.HasPrefix(a, "--hub-token="):
			hubToken = strings.TrimPrefix(a, "--hub-token=")
		case a == "--node" && i+1 < len(args):
			targetNode = args[i+1]
			i++
		case strings.HasPrefix(a, "--node="):
			targetNode = strings.TrimPrefix(a, "--node=")
		case a == "--region" && i+1 < len(args):
			targetRegion = args[i+1]
			i++
		case strings.HasPrefix(a, "--region="):
			targetRegion = strings.TrimPrefix(a, "--region=")
		case a == "--role" && i+1 < len(args):
			targetRole = args[i+1]
			i++
		case strings.HasPrefix(a, "--role="):
			targetRole = strings.TrimPrefix(a, "--role=")
		case a == "--name" && i+1 < len(args):
			runName = args[i+1]
			i++
		case strings.HasPrefix(a, "--name="):
			runName = strings.TrimPrefix(a, "--name=")
		case (a == "-c" || a == "--concurrency") && i+1 < len(args):
			concurrency, _ = strconv.Atoi(args[i+1])
			workloadType = "perf"
			i++
		case strings.HasPrefix(a, "--concurrency="):
			concurrency, _ = strconv.Atoi(strings.TrimPrefix(a, "--concurrency="))
			workloadType = "perf"
		case (a == "-n" || a == "--requests") && i+1 < len(args):
			requests, _ = strconv.Atoi(args[i+1])
			workloadType = "perf"
			i++
		case strings.HasPrefix(a, "--requests="):
			requests, _ = strconv.Atoi(strings.TrimPrefix(a, "--requests="))
			workloadType = "perf"
		case (a == "-d" || a == "--duration") && i+1 < len(args):
			d, err := time.ParseDuration(args[i+1])
			if err == nil {
				durationSec = d.Seconds()
				workloadType = "perf"
			}
			i++
		case strings.HasPrefix(a, "--duration="):
			d, err := time.ParseDuration(strings.TrimPrefix(a, "--duration="))
			if err == nil {
				durationSec = d.Seconds()
				workloadType = "perf"
			}
		case a == "--perf":
			workloadType = "perf"
		case a == "--no-stream":
			streamOutput = false
		case a == "--stream":
			streamOutput = true
		case a == "--json":
			jsonOutput = true
		case a == "--timeout" && i+1 < len(args):
			if d, err := time.ParseDuration(args[i+1]); err == nil {
				timeout = d
			}
			i++
		case !strings.HasPrefix(a, "-"):
			if target == "" {
				target = a
			}
		}
	}

	if target == "" {
		return fmt.Errorf("usage: hit dispatch <FILE|URL|REF> [--hub URL] [--node ID] [--region REGION] [--perf] [-c CONC] [-n REQS]")
	}

	// Resolve spec YAML
	specYAML := ""
	if b, err := os.ReadFile(target); err == nil {
		specYAML = string(b)
		if runName == "" {
			runName = filepath.Base(target)
		}
	} else if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		specYAML = fmt.Sprintf("name: %q\nmethod: GET\nurl: %q\ntests:\n  - status < 400\n", target, target)
		if runName == "" {
			runName = target
		}
	} else {
		// Attempt zone lookup if available
		if z, err := zone.FindOrNone(globals.Zone); z != nil && err == nil {
			specPath := filepath.Join(z.CollectionsDir(), target)
			if !strings.HasSuffix(specPath, ".yaml") && !strings.HasSuffix(specPath, ".yml") {
				specPath += ".yaml"
			}
			if b, err := os.ReadFile(specPath); err == nil {
				specYAML = string(b)
				if runName == "" {
					runName = target
				}
			}
		}
	}

	if specYAML == "" {
		return fmt.Errorf("unable to read spec or file from %q", target)
	}

	if workloadType == "" {
		workloadType = "test"
	}

	payload := map[string]any{
		"type":           workloadType,
		"name":           runName,
		"target_node_id": targetNode,
		"target_region":  targetRegion,
		"target_role":    targetRole,
		"spec_yaml":      specYAML,
		"concurrency":    concurrency,
		"requests":       requests,
		"duration_s":     durationSec,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode dispatch payload: %w", err)
	}

	submitURL := strings.TrimRight(hubURL, "/") + "/api/v1/dispatch"
	req, err := http.NewRequest(http.MethodPost, submitURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create dispatch request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if hubToken != "" {
		req.Header.Set("Authorization", "Bearer "+hubToken)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Hub at %s: %w", hubURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		snippet, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("hub returned HTTP %d on dispatch: %s", resp.StatusCode, string(snippet))
	}

	var createdJob hub.DispatchJob
	if err := json.NewDecoder(resp.Body).Decode(&createdJob); err != nil {
		return fmt.Errorf("failed to decode dispatch response: %w", err)
	}

	if !streamOutput {
		if jsonOutput {
			jsonBytes, _ := json.MarshalIndent(createdJob, "", "  ")
			p.Out(string(jsonBytes))
		} else {
			p.Out(fmt.Sprintf("%s Dispatched job %s to Fleet Hub (%s)", p.Cyan("⚡"), p.Bold(createdJob.ID), hubURL))
			p.Out(fmt.Sprintf("   Status: %s", p.Yellow(createdJob.State)))
		}
		return nil
	}

	// Stream output loop
	if !jsonOutput {
		targetDesc := targetNode
		if targetDesc == "" {
			targetDesc = "any in region " + targetRegion
			if targetRegion == "" {
				targetDesc = "next available idle worker"
			}
		}
		p.Out(fmt.Sprintf("%s Dispatched %s to Fleet Hub (%s)", p.Cyan("⚡"), p.Bold(createdJob.Name), p.Dim(hubURL)))
		p.Out(fmt.Sprintf("   Job ID:  %s", p.Cyan(createdJob.ID)))
		p.Out(fmt.Sprintf("   Target:  %s · Workload: %s", p.Bold(targetDesc), p.Green(workloadType)))
		p.Out(p.Dim("   Streaming execution events in real-time..."))
	}

	deadline := time.Now().Add(timeout)
	jobURL := fmt.Sprintf("%s/api/v1/dispatch/%s", strings.TrimRight(hubURL, "/"), createdJob.ID)
	printedLogs := 0

	for {
		if time.Now().After(deadline) {
			// Try to cancel
			cancelURL := fmt.Sprintf("%s/api/v1/dispatch/%s/cancel", strings.TrimRight(hubURL, "/"), createdJob.ID)
			cReq, _ := http.NewRequest(http.MethodPost, cancelURL, nil)
			if hubToken != "" {
				cReq.Header.Set("Authorization", "Bearer "+hubToken)
			}
			_, _ = client.Do(cReq)
			return fmt.Errorf("dispatch timed out after %v", timeout)
		}

		time.Sleep(350 * time.Millisecond)

		jReq, err := http.NewRequest(http.MethodGet, jobURL, nil)
		if err != nil {
			continue
		}
		if hubToken != "" {
			jReq.Header.Set("Authorization", "Bearer "+hubToken)
		}

		jResp, err := client.Do(jReq)
		if err != nil {
			continue
		}
		var currentJob hub.DispatchJob
		decErr := json.NewDecoder(jResp.Body).Decode(&currentJob)
		jResp.Body.Close()
		if decErr != nil {
			continue
		}

		// Print new logs
		if !jsonOutput && len(currentJob.Logs) > printedLogs {
			for i := printedLogs; i < len(currentJob.Logs); i++ {
				ev := currentJob.Logs[i]
				switch ev.Level {
				case "pass":
					p.Out(p.Green("   ✓ " + ev.Message))
				case "fail":
					p.Out(p.Red("   ✗ " + ev.Message))
				case "error":
					p.Out(p.Red("   ! " + ev.Message))
				default:
					p.Out(p.Dim("   • " + ev.Message))
				}
			}
			printedLogs = len(currentJob.Logs)
		}

		if currentJob.State == "completed" {
			if jsonOutput {
				out, _ := json.MarshalIndent(currentJob, "", "  ")
				p.Out(string(out))
				return nil
			}
			assigned := currentJob.AssignedNode
			if assigned == "" {
				assigned = "fleet worker"
			}
			p.Out(fmt.Sprintf("\n%s Dispatched job completed successfully on node %s", p.Green("✓"), p.Bold(assigned)))
			if currentJob.TelemetryRunID != "" {
				p.Out(fmt.Sprintf("   Telemetry Run: %s/#/run/%s", hubURL, currentJob.TelemetryRunID))
			}
			return nil
		}

		if currentJob.State == "failed" {
			if jsonOutput {
				out, _ := json.MarshalIndent(currentJob, "", "  ")
				p.Out(string(out))
			} else {
				p.Out(fmt.Sprintf("\n%s Dispatched job failed on node %s: %s", p.Red("✗"), p.Bold(currentJob.AssignedNode), currentJob.Error))
			}
			return fmt.Errorf("job failed on remote node: %s", currentJob.Error)
		}

		if currentJob.State == "canceled" {
			return fmt.Errorf("job was canceled")
		}
	}
}

func cmdCost(args []string, globals *GlobalFlags, p *output.Printer) error {
	var (
		targetRef       string
		pricingFile     string
		presetName      string
		callsStr        string
		tiersStr        string
		unitPriceStr    string
		modelStr        string
		baseFeeStr      string
		pkgStr          string
		retriesStr      string
		failRateStr     string
		worstRateStr    string
		tokensStr       string
		tokenPriceStr   string
		budgetStr       string
		currencyStr     string
		unitName        string
		noBillableFails bool
		fromPerf        bool
		jsonOutput      bool
	)

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "-n" || a == "--calls" || a == "--requests") && i+1 < len(args):
			callsStr = args[i+1]
			i++
		case strings.HasPrefix(a, "--calls="):
			callsStr = strings.TrimPrefix(a, "--calls=")
		case strings.HasPrefix(a, "--requests="):
			callsStr = strings.TrimPrefix(a, "--requests=")
		case strings.HasPrefix(a, "-n="):
			callsStr = strings.TrimPrefix(a, "-n=")

		case (a == "-f" || a == "--pricing" || a == "--config") && i+1 < len(args):
			pricingFile = args[i+1]
			i++
		case strings.HasPrefix(a, "--pricing="):
			pricingFile = strings.TrimPrefix(a, "--pricing=")
		case strings.HasPrefix(a, "--config="):
			pricingFile = strings.TrimPrefix(a, "--config=")

		case (a == "-p" || a == "--preset") && i+1 < len(args):
			presetName = args[i+1]
			i++
		case strings.HasPrefix(a, "--preset="):
			presetName = strings.TrimPrefix(a, "--preset=")

		case a == "--tiers" && i+1 < len(args):
			tiersStr = args[i+1]
			i++
		case strings.HasPrefix(a, "--tiers="):
			tiersStr = strings.TrimPrefix(a, "--tiers=")

		case a == "--unit-price" && i+1 < len(args):
			unitPriceStr = args[i+1]
			i++
		case strings.HasPrefix(a, "--unit-price="):
			unitPriceStr = strings.TrimPrefix(a, "--unit-price=")

		case a == "--model" && i+1 < len(args):
			modelStr = args[i+1]
			i++
		case strings.HasPrefix(a, "--model="):
			modelStr = strings.TrimPrefix(a, "--model=")

		case a == "--base-fee" && i+1 < len(args):
			baseFeeStr = args[i+1]
			i++
		case strings.HasPrefix(a, "--base-fee="):
			baseFeeStr = strings.TrimPrefix(a, "--base-fee=")

		case a == "--package" && i+1 < len(args):
			pkgStr = args[i+1]
			i++
		case strings.HasPrefix(a, "--package="):
			pkgStr = strings.TrimPrefix(a, "--package=")

		case (a == "-r" || a == "--retries") && i+1 < len(args):
			retriesStr = args[i+1]
			i++
		case strings.HasPrefix(a, "--retries="):
			retriesStr = strings.TrimPrefix(a, "--retries=")

		case a == "--failure-rate" && i+1 < len(args):
			failRateStr = args[i+1]
			i++
		case strings.HasPrefix(a, "--failure-rate="):
			failRateStr = strings.TrimPrefix(a, "--failure-rate=")

		case a == "--worst-case-rate" && i+1 < len(args):
			worstRateStr = args[i+1]
			i++
		case strings.HasPrefix(a, "--worst-case-rate="):
			worstRateStr = strings.TrimPrefix(a, "--worst-case-rate=")

		case a == "--no-billable-failures":
			noBillableFails = true

		case a == "--tokens" && i+1 < len(args):
			tokensStr = args[i+1]
			i++
		case strings.HasPrefix(a, "--tokens="):
			tokensStr = strings.TrimPrefix(a, "--tokens=")

		case a == "--token-pricing" && i+1 < len(args):
			tokenPriceStr = args[i+1]
			i++
		case strings.HasPrefix(a, "--token-pricing="):
			tokenPriceStr = strings.TrimPrefix(a, "--token-pricing=")

		case (a == "-b" || a == "--budget") && i+1 < len(args):
			budgetStr = args[i+1]
			i++
		case strings.HasPrefix(a, "--budget="):
			budgetStr = strings.TrimPrefix(a, "--budget=")

		case a == "--currency" && i+1 < len(args):
			currencyStr = args[i+1]
			i++
		case strings.HasPrefix(a, "--currency="):
			currencyStr = strings.TrimPrefix(a, "--currency=")

		case a == "--unit" && i+1 < len(args):
			unitName = args[i+1]
			i++
		case strings.HasPrefix(a, "--unit="):
			unitName = strings.TrimPrefix(a, "--unit=")

		case a == "--from-perf":
			fromPerf = true

		case a == "--json":
			jsonOutput = true

		default:
			if !strings.HasPrefix(a, "-") && targetRef == "" {
				targetRef = a
			}
		}
	}

	cfg := cost.PricingConfig{
		Currency: "USD",
		Unit:     "request",
		Model:    cost.ModelGraduated,
		Retry:    cost.DefaultRetryConfig(),
	}

	if presetName != "" {
		preset, err := cost.GetPreset(presetName)
		if err != nil {
			return err
		}
		cfg = preset
	}

	if pricingFile != "" {
		loaded, err := cost.LoadConfigFile(pricingFile)
		if err != nil {
			return err
		}
		cfg = *loaded
	}

	targetName := ""
	if targetRef != "" {
		targetName = targetRef
		var sp *spec.RequestSpec
		if _, statErr := os.Stat(targetRef); statErr == nil {
			loadedSp, err := spec.LoadSpec(targetRef, nil)
			if err == nil {
				sp = loadedSp
			}
		} else if z, zErr := zone.Find(globals.Zone); zErr == nil && z != nil {
			if resolved, rErr := z.ResolveRequest(targetRef); rErr == nil {
				loadedSp, err := spec.LoadSpec(resolved, z.DefaultsChain(resolved))
				if err == nil {
					sp = loadedSp
				}
			}
		}

		if sp != nil {
			if sp.Name != "" {
				targetName = sp.Name
			}
			if sp.Extra != nil && sp.Extra["cost"] != nil {
				if costMap, ok := sp.Extra["cost"].(map[string]any); ok {
					parsed, err := cost.FromMap(costMap)
					if err == nil {
						cfg = *parsed
					}
				}
			}
		}
	} else if z, zErr := zone.Find(globals.Zone); zErr == nil && z != nil {
		if z.Config["cost"] != nil {
			if costMap, ok := z.Config["cost"].(map[string]any); ok {
				parsed, err := cost.FromMap(costMap)
				if err == nil {
					cfg = *parsed
				}
			}
		}
	}

	// CLI overrides
	if modelStr != "" {
		cfg.Model = cost.PricingModel(modelStr)
	}
	if unitPriceStr != "" {
		price, err := strconv.ParseFloat(unitPriceStr, 64)
		if err == nil {
			cfg.UnitPrice = price
			cfg.Model = cost.ModelFlat
		}
	}
	if tiersStr != "" {
		parsedTiers, err := cost.ParseTierString(tiersStr)
		if err != nil {
			return err
		}
		cfg.Tiers = parsedTiers
		if cfg.Model == "" || cfg.Model == cost.ModelFlat {
			cfg.Model = cost.ModelGraduated
		}
	}
	if baseFeeStr != "" {
		fee, _ := strconv.ParseFloat(baseFeeStr, 64)
		cfg.BaseFee = fee
	}
	if pkgStr != "" {
		parts := strings.Split(pkgStr, ":")
		if len(parts) == 2 {
			size, _ := cost.ParseQuantity(parts[0])
			price, _ := strconv.ParseFloat(parts[1], 64)
			cfg.PackageSize = size
			cfg.PackagePrice = price
			cfg.Model = cost.ModelPackage
		}
	}
	if retriesStr != "" {
		retries, _ := strconv.Atoi(retriesStr)
		cfg.Retry.MaxAttempts = retries
	}
	if failRateStr != "" {
		val := strings.TrimSuffix(failRateStr, "%")
		rate, err := strconv.ParseFloat(val, 64)
		if err == nil {
			if strings.HasSuffix(failRateStr, "%") || rate > 1.0 {
				rate = rate / 100.0
			}
			cfg.Retry.ExpectedFailureRate = rate
		}
	}
	if worstRateStr != "" {
		val := strings.TrimSuffix(worstRateStr, "%")
		rate, err := strconv.ParseFloat(val, 64)
		if err == nil {
			if strings.HasSuffix(worstRateStr, "%") || rate > 1.0 {
				rate = rate / 100.0
			}
			cfg.Retry.WorstCaseFailureRate = rate
		}
	}
	if noBillableFails {
		cfg.Retry.BillableFailures = false
	}
	if tokensStr != "" {
		parts := strings.Split(tokensStr, ":")
		if len(parts) == 2 {
			promptTok, _ := strconv.ParseFloat(parts[0], 64)
			compTok, _ := strconv.ParseFloat(parts[1], 64)
			if cfg.Tokens == nil {
				cfg.Tokens = &cost.TokenPricing{PromptPricePer1M: 2.50, CompletionPricePer1M: 10.00}
			}
			cfg.Tokens.AvgPromptTokens = promptTok
			cfg.Tokens.AvgCompletionTokens = compTok
		}
	}
	if tokenPriceStr != "" {
		parts := strings.Split(tokenPriceStr, ":")
		if len(parts) == 2 {
			pPrice, _ := strconv.ParseFloat(parts[0], 64)
			cPrice, _ := strconv.ParseFloat(parts[1], 64)
			if cfg.Tokens == nil {
				cfg.Tokens = &cost.TokenPricing{AvgPromptTokens: 800, AvgCompletionTokens: 200}
			}
			cfg.Tokens.PromptPricePer1M = pPrice
			cfg.Tokens.CompletionPricePer1M = cPrice
		}
	}
	if currencyStr != "" {
		cfg.Currency = currencyStr
	}
	if unitName != "" {
		cfg.Unit = unitName
	}

	// Volume parsing
	calls := int64(100_000)
	if callsStr != "" {
		parsedCalls, err := cost.ParseQuantity(callsStr)
		if err != nil {
			return fmt.Errorf("invalid calls count %q: %w", callsStr, err)
		}
		calls = parsedCalls
	}

	// Budget parsing
	var budget *float64
	if budgetStr != "" {
		bVal, err := strconv.ParseFloat(strings.TrimPrefix(budgetStr, "$"), 64)
		if err != nil {
			return fmt.Errorf("invalid budget value %q: %w", budgetStr, err)
		}
		budget = &bVal
	}

	if fromPerf {
		zoneRoot := ""
		if z, err := zone.Find(globals.Zone); err == nil && z != nil {
			zoneRoot = z.Root
		}
		hStore := history.NewStore(history.GetHistoryPath(zoneRoot))
		entries, _ := hStore.List(100, 0, "", "")
		if len(entries) > 0 {
				failCount := 0
				totalCount := len(entries)
				for _, r := range entries {
					if r.Error != "" || (r.Status >= 400 && r.Status != 404) {
						failCount++
					}
				}
				if totalCount > 0 {
					observedRate := float64(failCount) / float64(totalCount)
					cfg.Retry.ExpectedFailureRate = observedRate
					if !jsonOutput {
						p.Out(fmt.Sprintf("%s Inferred expected failure rate %.1f%% from %d historical runs",
							p.Cyan("ℹ"), observedRate*100.0, totalCount))
					}
				}
			}
		}

	if len(cfg.Tiers) == 0 && cfg.UnitPrice == 0 && cfg.Tokens == nil && cfg.PackagePrice == 0 {
		var u10k int64 = 10_000
		var u50k int64 = 50_000
		cfg.Model = cost.ModelGraduated
		cfg.Tiers = []cost.PricingTier{
			{UpTo: &u10k, UnitPrice: 0.0050},
			{UpTo: &u50k, UnitPrice: 0.0030},
			{UpTo: nil, UnitPrice: 0.0010},
		}
	}

	res := cost.EstimateRange(calls, cfg, budget)
	if targetName != "" {
		res.Target = targetName
	}

	if jsonOutput {
		jsonStr, err := cost.FormatJSON(res)
		if err != nil {
			return err
		}
		p.Out(jsonStr)
	} else {
		p.Out(cost.FormatTerminal(res, p))
	}

	if budget != nil && res.BudgetExceeded {
		return fmt.Errorf("budget exceeded: expected cost %s exceeds budget ceiling %s",
			res.FormatMoney(res.Expected.Breakdown.TotalCost),
			res.FormatMoney(*budget),
		)
	}

	return nil
}

func cmdImport(args []string, globals *GlobalFlags, p *output.Printer, z *zone.Zone) error {
	kind := ""
	var files []string
	globalsFile := ""
	nameOverride := ""

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--globals" && i+1 < len(args):
			globalsFile = args[i+1]
			i++
		case a == "--name" && i+1 < len(args):
			nameOverride = args[i+1]
			i++
		default:
			if !strings.HasPrefix(a, "-") {
				if kind == "" {
					kind = a
				} else {
					files = append(files, a)
				}
			}
		}
	}

	// Auto-detect kind if first argument is a file, URL, or curl command
	if kind != "collection" && kind != "env" && kind != "environment" && kind != "globals" && kind != "openapi" && kind != "swagger" && kind != "curl" {
		trimmed := strings.TrimSpace(kind)
		if strings.HasPrefix(trimmed, "curl ") || strings.HasPrefix(trimmed, "curl\t") {
			files = append([]string{kind}, files...)
			kind = "curl"
		} else if fi, err := os.Stat(kind); err == nil && !fi.IsDir() {
			filePath := kind
			files = append([]string{filePath}, files...)
			if b, err := os.ReadFile(filePath); err == nil {
				content := string(b)
				if strings.Contains(content, "openapi:") || strings.Contains(content, "\"openapi\"") || strings.Contains(content, "swagger:") || strings.Contains(content, "\"swagger\"") {
					kind = "openapi"
				} else if strings.Contains(content, "\"_postman_id\"") || (strings.Contains(content, "\"item\"") && strings.Contains(content, "\"info\"")) {
					kind = "collection"
				} else if strings.Contains(content, "\"values\"") && strings.Contains(content, "\"name\"") {
					kind = "env"
				} else {
					kind = "openapi"
				}
			}
		} else if strings.HasPrefix(kind, "http://") || strings.HasPrefix(kind, "https://") {
			files = append([]string{kind}, files...)
			kind = "openapi"
		}
	}

	if kind == "" || (len(files) == 0 && globalsFile == "") {
		return zone.NewZoneError("usage: hit import [collection|openapi|curl|env|globals] FILE...")
	}

	switch kind {
	case "openapi", "swagger":
		for _, f := range files {
			report, err := importer.ImportOpenAPI(f, z.CollectionsDir(), nameOverride)
			if err != nil {
				return err
			}
			relDest, _ := filepath.Rel(z.Root, report.Destination)
			p.Out(p.Bold(fmt.Sprintf("imported OpenAPI spec '%s' → %s", report.Collection, relDest)))
			p.Out(fmt.Sprintf("  %d requests, %d tag folders, %d assertions and %d captures generated",
				report.Requests, report.Folders, report.TestsConverted, report.CapturesConverted))
			for _, w := range report.Warnings {
				p.Out(p.Yellow(fmt.Sprintf("  ! %s", w)))
			}
		}
		return nil

	case "curl":
		targetDir := z.CollectionsDir()
		if nameOverride != "" {
			targetDir = filepath.Join(z.CollectionsDir(), importer.Slugify(nameOverride, "curl"))
		}
		for _, cmdStr := range files {
			report, err := importer.ImportCurl(cmdStr, targetDir, nameOverride)
			if err != nil {
				return err
			}
			relDest, _ := filepath.Rel(z.Root, report.Destination)
			p.Out(p.Bold(fmt.Sprintf("imported curl command → %s", relDest)))
			for _, f := range report.Files {
				relF, _ := filepath.Rel(z.Root, f)
				p.Out(fmt.Sprintf("  created %s", relF))
			}
		}
		return nil

	case "collection":
		for _, f := range files {
			report, err := importer.ImportCollection(f, z.CollectionsDir(), nameOverride)
			if err != nil {
				return err
			}
			relDest, _ := filepath.Rel(z.Root, report.Destination)
			p.Out(p.Bold(fmt.Sprintf("imported collection '%s' → %s", report.Collection, relDest)))
			p.Out(fmt.Sprintf("  %d requests, %d folders, %d assertions and %d captures converted",
				report.Requests, report.Folders, report.TestsConverted, report.CapturesConverted))
			for _, w := range report.Warnings {
				p.Out(p.Yellow(fmt.Sprintf("  ! %s", w)))
			}
			if len(report.Unconverted) > 0 {
				total := 0
				for _, v := range report.Unconverted {
					total += len(v)
				}
				p.Out(p.Yellow(fmt.Sprintf("  %d script line(s) in %d file(s) need hand porting (kept under 'unconverted:' in each file):", total, len(report.Unconverted))))
				for rRef, lines := range report.Unconverted {
					p.Out(fmt.Sprintf("    %s", rRef))
					limit := 4
					if len(lines) < limit {
						limit = len(lines)
					}
					for i := 0; i < limit; i++ {
						line := strings.TrimSpace(lines[i])
						if len(line) > 110 {
							line = line[:110]
						}
						p.Out(p.Dim(fmt.Sprintf("      %s", line)))
					}
					if len(lines) > 4 {
						p.Out(p.Dim(fmt.Sprintf("      … %d more", len(lines)-4)))
					}
				}
			}
		}
		if globalsFile != "" {
			n, err := importer.ImportGlobals(globalsFile, filepath.Join(z.Root, zone.ZoneFile))
			if err != nil {
				return err
			}
			p.Out(fmt.Sprintf("merged %d globals into zone.yaml vars", n))
		}
		return nil

	case "env", "environment":
		for _, f := range files {
			envPath, secPath, err := importer.ImportEnvironment(f, z.ServersDir(), nameOverride)
			if err != nil {
				return err
			}
			relEnv, _ := filepath.Rel(z.Root, envPath)
			p.Out(fmt.Sprintf("imported environment → %s", relEnv))
			if secPath != "" {
				relSec, _ := filepath.Rel(z.Root, secPath)
				p.Out(p.Yellow(fmt.Sprintf("  secrets written to %s (gitignored)", relSec)))
			}
		}
		return nil

	case "globals":
		for _, f := range files {
			n, err := importer.ImportGlobals(f, filepath.Join(z.Root, zone.ZoneFile))
			if err != nil {
				return err
			}
			p.Out(fmt.Sprintf("merged %d globals into zone.yaml vars", n))
		}
		return nil

	default:
		return zone.NewZoneError("unknown import kind '%s' (use collection, openapi, curl, env, or globals)", kind)
	}
}

func cmdValidate(args []string, globals *GlobalFlags, p *output.Printer, z *zone.Zone) error {
	var refs []string
	jsonOutput := false
	for _, a := range args {
		if a == "--json" {
			jsonOutput = true
		} else if !strings.HasPrefix(a, "-") {
			refs = append(refs, a)
		}
	}

	if len(refs) == 0 {
		refs = []string{z.CollectionsDir()}
	}

	sess, err := runner.NewSession(runner.SessionOptions{
		Zone: z,
		ServerName: globals.Server,
		ExtraVars: globals.Vars,
		Persist:   false,
	})
	if err != nil {
		return err
	}
	defer sess.Close()

	var paths []string
	for _, ref := range refs {
		target, err := z.ResolveRequest(ref)
		if err != nil {
			return err
		}
		fi, err := os.Stat(target)
		if err == nil && fi.IsDir() {
			paths = append(paths, z.ListRequests(target)...)
		} else {
			paths = append(paths, target)
		}
	}

	captured := make(map[string][]string)
	for _, path := range z.ListRequests("") {
		sp, err := spec.LoadSpec(path, z.DefaultsChain(path))
		if err == nil {
			for name := range sp.Captures {
				captured[name] = append(captured[name], z.RequestRef(path))
			}
		}
	}
	for _, flowPath := range z.ListChains() {
		flowData, err := flows.LoadFlow(flowPath)
		if err == nil {
			stem := strings.TrimSuffix(filepath.Base(flowPath), filepath.Ext(flowPath))
			if steps, ok := flowData["steps"].([]any); ok {
				for _, rawStep := range steps {
					if sm, ok := rawStep.(map[string]any); ok {
						for k := range sm {
							if k == "set" || k == "captures" {
								if vm, ok := sm[k].(map[string]any); ok {
									for name := range vm {
										captured[name] = append(captured[name], "flow "+stem)
									}
								}
							}
						}
					}
				}
			}
		}
	}

	var problems []map[string]any
	var depends []map[string]any

	for _, path := range paths {
		ref := z.RequestRef(path)
		rel, _ := filepath.Rel(z.Root, path)
		sp, err := spec.LoadSpec(path, z.DefaultsChain(path))
		if err != nil {
			problems = append(problems, map[string]any{
				"ref":   ref,
				"file":  rel,
				"kind":  "spec",
				"error": err.Error(),
			})
			continue
		}

		_, _, _, err = sess.Prepare(sp, nil)
		if err != nil {
			if mErr, ok := err.(*templating.MissingVariableError); ok {
				var undefined []string
				needs := make(map[string][]string)
				for _, n := range mErr.Names {
					if sources, ok := captured[n]; ok {
						needs[n] = sources
					} else {
						undefined = append(undefined, n)
					}
				}
				if len(needs) > 0 {
					depends = append(depends, map[string]any{
						"ref":   ref,
						"needs": needs,
					})
				}
				if len(undefined) > 0 {
					problems = append(problems, map[string]any{
						"ref":   ref,
						"file":  rel,
						"kind":  "variables",
						"error": fmt.Sprintf("Undefined variable(s): %s", strings.Join(undefined, ", ")),
					})
				}
			} else {
				problems = append(problems, map[string]any{
					"ref":   ref,
					"file":  rel,
					"kind":  "spec",
					"error": err.Error(),
				})
			}
		}
	}

	if jsonOutput {
		report := map[string]any{
			"checked":             len(paths),
			"server":              sess.Server.Name,
			"problems":            problems,
			"depends_on_captures": depends,
		}
		b, _ := json.MarshalIndent(report, "", "  ")
		p.Out(string(b))
	} else {
		for _, d := range depends {
			ref := d["ref"]
			needsMap := d["needs"].(map[string][]string)
			var parts []string
			for n, src := range needsMap {
				parts = append(parts, fmt.Sprintf("{{%s}} from %s", n, strings.Join(src, " or ")))
			}
			p.Out(fmt.Sprintf("%s %s  %s", p.Yellow("·"), ref, p.Dim("needs "+strings.Join(parts, ", "))))
		}
		for _, pr := range problems {
			p.Out(fmt.Sprintf("%s %s  %s\n    %s", p.Red("✗"), pr["ref"], p.Dim(fmt.Sprintf("%v", pr["file"])), pr["error"]))
		}
		okCount := len(paths) - len(problems)
		summary := fmt.Sprintf("%d/%d request(s) valid for server '%s'", okCount, len(paths), sess.Server.Name)
		if len(problems) > 0 {
			summary += fmt.Sprintf(", %s", p.Red(fmt.Sprintf("%d with problems", len(problems))))
		} else {
			summary += fmt.Sprintf(", %s", p.Green("all good"))
		}
		p.Out(summary)
	}

	if len(problems) > 0 {
		os.Exit(ExitFail)
	}
	return nil
}

func cmdSanity(args []string, globals *GlobalFlags, p *output.Printer, z *zone.Zone) error {
	scope := ""
	offline := false
	jsonOutput := false

	for _, a := range args {
		if a == "--offline" {
			offline = true
		} else if a == "--json" {
			jsonOutput = true
		} else if !strings.HasPrefix(a, "-") {
			scope = a
		}
	}

	rep := sanity.RunSanity(z, globals.Server, scope, !offline, 5.0)

	if jsonOutput {
		b, _ := json.MarshalIndent(rep, "", "  ")
		p.Out(string(b))
		if !rep.Ready {
			return zone.NewZoneError("sanity check failed: %d problem(s) found", rep.Failures)
		}
		return nil
	}

	p.Out(p.Bold(fmt.Sprintf("Sanity check: zone '%s'", rep.Zone)) + p.Dim(fmt.Sprintf("  (%s)", rep.Root)))
	p.Out(fmt.Sprintf("server: %s    scope: %s", rep.Server, rep.Scope))
	p.Out("")

	icons := map[string]string{
		"ok":   p.Green("✓"),
		"warn": p.Yellow("!"),
		"fail": p.Red("✗"),
		"info": p.Dim("·"),
	}

	for _, c := range rep.Checks {
		p.Out(fmt.Sprintf(" %s %s", icons[c.Status], c.Title))
		if c.Detail != "" {
			for _, line := range strings.Split(c.Detail, "\n") {
				p.Out(p.Dim(fmt.Sprintf("     %s", line)))
			}
		}
		if c.Fix != "" && (c.Status == "warn" || c.Status == "fail") {
			p.Out(fmt.Sprintf("     → %s", c.Fix))
		}
	}
	p.Out("")

	if rep.Ready {
		extra := ""
		if rep.Warnings > 0 {
			extra = fmt.Sprintf(" (%d warnings)", rep.Warnings)
		}
		p.Out(p.Green(p.Bold("Ready to run")) + extra)
		return nil
	}

	p.Out(p.Red(p.Bold("Not ready")) + fmt.Sprintf(": fix the %d problem(s) marked ✗ above", rep.Failures))
	return zone.NewZoneError("sanity check failed: %d problem(s) found", rep.Failures)
}

func cmdPolicy(args []string, globals *GlobalFlags, p *output.Printer, z *zone.Zone) error {
	scope := ""
	jsonOutput := false
	strictMode := false
	junitPath := ""

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
			jsonOutput = true
		case a == "--strict":
			strictMode = true
		case a == "--junit" && i+1 < len(args):
			junitPath = args[i+1]
			i++
		case strings.HasPrefix(a, "--junit="):
			junitPath = strings.TrimPrefix(a, "--junit=")
		default:
			if !strings.HasPrefix(a, "-") {
				scope = a
			}
		}
	}

	cfg, err := policy.LoadPolicy(z.Root)
	if err != nil {
		return err
	}

	rep, err := policy.AuditZone(z, scope, cfg)
	if err != nil {
		return err
	}

	if junitPath != "" {
		if err := rep.WriteJUnitReport(junitPath); err != nil {
			fmt.Fprintf(os.Stderr, "error writing policy junit xml: %v\n", err)
		} else if !jsonOutput {
			p.Out(p.Green(fmt.Sprintf("✓ Policy JUnit XML written to %s", junitPath)))
		}
	}

	if jsonOutput {
		b, _ := json.MarshalIndent(rep, "", "  ")
		p.Out(string(b))
		if !rep.Passed || (strictMode && len(rep.Violations) > 0) {
			return zone.NewZoneError("policy check failed: %d violation(s) found", len(rep.Violations))
		}
		return nil
	}

	p.Out(p.Bold(fmt.Sprintf("🛡️  hit Policy & Governance Linter (Zone: '%s')", z.Name())))
	p.Out(fmt.Sprintf("Scope: %s | Files checked: %d", rep.Scope, rep.TotalFiles))
	p.Out(p.Dim("----------------------------------------------------------------------"))

	if len(rep.Violations) == 0 {
		p.Out(p.Green(p.Bold("✓ All requests and collections comply with governance policy!")))
		return nil
	}

	for _, v := range rep.Violations {
		badge := p.Red("✗ ERROR")
		if v.Severity == policy.SeverityWarn {
			badge = p.Yellow("! WARN ")
		}
		p.Out(fmt.Sprintf(" %s %s [%s]", badge, p.Bold(v.File), p.Dim(v.Rule)))
		p.Out(fmt.Sprintf("         %s", v.Message))
		if v.Remediation != "" {
			p.Out(fmt.Sprintf("         → %s", p.Cyan(v.Remediation)))
		}
	}

	p.Out(p.Dim("----------------------------------------------------------------------"))
	errCount := 0
	warnCount := 0
	for _, v := range rep.Violations {
		if v.Severity == policy.SeverityError {
			errCount++
		} else {
			warnCount++
		}
	}

	summary := fmt.Sprintf("Violations: %d error(s), %d warning(s) across %d file(s)", errCount, warnCount, rep.TotalFiles)
	if !rep.Passed || (strictMode && warnCount > 0) {
		p.Out(p.Red(p.Bold("Policy check failed: ")) + summary)
		return zone.NewZoneError("policy check failed: %d violation(s)", len(rep.Violations))
	}

	p.Out(p.Yellow(p.Bold("Policy passed with warnings: ")) + summary)
	return nil
}

func cmdNew(args []string, globals *GlobalFlags, p *output.Printer, z *zone.Zone) error {
	ref := ""
	method := "GET"
	reqURL := "{{base_url}}/"
	infer := false

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "-m" || a == "--method") && i+1 < len(args):
			method = strings.ToUpper(args[i+1])
			i++
		case (a == "-u" || a == "--url") && i+1 < len(args):
			reqURL = args[i+1]
			i++
		case a == "--infer":
			infer = true
		default:
			if !strings.HasPrefix(a, "-") {
				ref = a
			}
		}
	}

	if ref == "" {
		return zone.NewZoneError("usage: hit new REF [-m METHOD] [-u URL] [--infer]")
	}

	if !strings.HasSuffix(ref, ".yaml") && !strings.HasSuffix(ref, ".yml") {
		ref += ".yaml"
	}
	targetPath := ref
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(z.CollectionsDir(), ref)
	}

	if _, err := os.Stat(targetPath); err == nil {
		return zone.NewZoneError("%s already exists", targetPath)
	}

	_ = os.MkdirAll(filepath.Dir(targetPath), 0755)

	name := strings.Title(strings.ReplaceAll(strings.TrimSuffix(filepath.Base(targetPath), filepath.Ext(targetPath)), "-", " "))
	tests := []any{
		map[string]any{"status": 200},
	}
	inferredCount := 0

	if infer {
		sess, err := runner.NewSession(runner.SessionOptions{
			Zone:              z,
			ServerName:        globals.Server,
			ExtraVars:         globals.Vars,
			ZoneOptional:      true,
			Verify:            !globals.Insecure,
			NoHistory:         globals.NoHistory,
		})
		if err != nil {
			return err
		}
		defer sess.Close()

		r := sess.Request(method, reqURL, nil, nil, nil, nil, nil, nil, nil, nil, nil, "infer")
		if r.Error != "" {
			return zone.NewZoneError("failed to infer assertions: request to %s failed: %s", reqURL, r.Error)
		}
		if r.Status > 0 {
			inferredTests, _, err := assertions.InferFromResponse(
				r.Status,
				r.ElapsedMs,
				r.Headers,
				r.Text,
				assertions.DefaultInferOptions(),
			)
			if err == nil && len(inferredTests) > 0 {
				tests = make([]any, len(inferredTests))
				for idx, t := range inferredTests {
					tests[idx] = t
				}
				inferredCount = len(inferredTests)
			}
		}
	}

	data := map[string]any{
		"name":    name,
		"method":  method,
		"url":     reqURL,
		"headers": map[string]any{},
		"tests":   tests,
	}
	if method == "POST" || method == "PUT" || method == "PATCH" {
		data["body"] = map[string]any{"json": map[string]any{}}
	}

	y, err := zone.DumpYAML(data)
	if err != nil {
		return err
	}
	if err := os.WriteFile(targetPath, []byte(y), 0644); err != nil {
		return err
	}

	rel, _ := filepath.Rel(z.Root, targetPath)
	if inferredCount > 0 {
		p.Out(fmt.Sprintf("created %s (with %d inferred assertions)", rel, inferredCount))
	} else {
		p.Out(fmt.Sprintf("created %s", rel))
	}
	return nil
}

func cmdHistory(args []string, globals *GlobalFlags, p *output.Printer) error {
	z, _ := zone.FindOrNone(globals.Zone)
	zoneRoot := ""
	if z != nil {
		zoneRoot = z.Root
	}
	store := history.NewStore(history.GetHistoryPath(zoneRoot))

	if len(args) > 0 {
		sub := args[0]
		switch sub {
		case "clear":
			if err := store.Clear(); err != nil {
				return err
			}
			p.Out(p.Dim("history cleared"))
			return nil

		case "show":
			if len(args) < 2 {
				return zone.NewZoneError("usage: hit history show <ID|INDEX> [--json]")
			}
			idOrIdx := args[1]
			jsonOutput := false
			for _, a := range args[2:] {
				if a == "--json" {
					jsonOutput = true
				}
			}
			entry, err := store.Get(idOrIdx)
			if err != nil {
				return err
			}
			if jsonOutput {
				b, _ := json.MarshalIndent(entry, "", "  ")
				p.Out(string(b))
				return nil
			}
			printHistoryEntry(entry, p)
			return nil

		case "save":
			if len(args) < 3 {
				return zone.NewZoneError("usage: hit history save <ID|INDEX> <TARGET_FILE.yaml> [--infer]")
			}
			idOrIdx := ""
			targetFile := ""
			infer := false
			for _, a := range args[1:] {
				if a == "--infer" {
					infer = true
				} else if idOrIdx == "" {
					idOrIdx = a
				} else if targetFile == "" {
					targetFile = a
				}
			}
			if idOrIdx == "" || targetFile == "" {
				return zone.NewZoneError("usage: hit history save <ID|INDEX> <TARGET_FILE.yaml> [--infer]")
			}
			entry, err := store.Get(idOrIdx)
			if err != nil {
				return err
			}
			if z != nil && !filepath.IsAbs(targetFile) && !strings.HasPrefix(targetFile, "collections") {
				targetFile = filepath.Join(z.CollectionsDir(), targetFile)
			}
			if !strings.HasSuffix(targetFile, ".yaml") && !strings.HasSuffix(targetFile, ".yml") {
				targetFile += ".yaml"
			}
			if err := history.SaveAsRequestWithInference(entry, targetFile, infer); err != nil {
				return err
			}
			if infer {
				p.Out(p.Green(fmt.Sprintf("✓ saved hit %s to %s with inferred assertions", idOrIdx, targetFile)))
			} else {
				p.Out(p.Green(fmt.Sprintf("✓ saved hit %s to %s", idOrIdx, targetFile)))
			}
			return nil

		case "ls", "list":
			args = args[1:]
		}
	}

	limit := 20
	statusFilter := 0
	methodFilter := ""
	refFilter := ""
	jsonOutput := false

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "-n" || a == "--limit") && i+1 < len(args):
			limit, _ = strconv.Atoi(args[i+1])
			i++
		case a == "--status" && i+1 < len(args):
			statusFilter, _ = strconv.Atoi(args[i+1])
			i++
		case (a == "-m" || a == "--method") && i+1 < len(args):
			methodFilter = args[i+1]
			i++
		case a == "--ref" && i+1 < len(args):
			refFilter = args[i+1]
			i++
		case a == "--json":
			jsonOutput = true
		default:
			if !strings.HasPrefix(a, "-") {
				refFilter = a
			}
		}
	}

	entries, err := store.List(limit, statusFilter, methodFilter, refFilter)
	if err != nil {
		return err
	}

	if jsonOutput {
		b, _ := json.MarshalIndent(entries, "", "  ")
		p.Out(string(b))
		return nil
	}

	if len(entries) == 0 {
		p.Out(p.Dim("no history entries found"))
		return nil
	}

	printHistoryList(entries, p)
	return nil
}

func printHistoryList(entries []*history.Entry, p *output.Printer) {
	p.Out(fmt.Sprintf("%-4s %-12s %-7s %-7s %-9s %s", "#", "TIME", "METHOD", "STATUS", "LATENCY", "TARGET"))
	p.Out(strings.Repeat("─", 72))

	for i, e := range entries {
		idxStr := fmt.Sprintf("%d", i+1)
		statusStr := fmt.Sprintf("%d", e.Status)
		if e.Status == 0 {
			statusStr = "ERR"
		}
		if p.Color {
			if e.Status >= 200 && e.Status < 300 {
				statusStr = p.Green(statusStr)
			} else if e.Status >= 300 && e.Status < 400 {
				statusStr = p.Cyan(statusStr)
			} else {
				statusStr = p.Red(statusStr)
			}
		}

		timeStr := e.Timestamp
		if t, err := time.Parse(time.RFC3339, e.Timestamp); err == nil {
			elapsed := time.Since(t)
			if elapsed < time.Minute {
				timeStr = fmt.Sprintf("%.0fs ago", elapsed.Seconds())
			} else if elapsed < time.Hour {
				timeStr = fmt.Sprintf("%.0fm ago", elapsed.Minutes())
			} else if elapsed < 24*time.Hour {
				timeStr = fmt.Sprintf("%.0fh ago", elapsed.Hours())
			} else {
				timeStr = t.Format("Jan 02")
			}
		}

		targetStr := e.Ref
		if targetStr == "" {
			targetStr = e.Url
		}
		if len(targetStr) > 30 {
			targetStr = targetStr[:27] + "..."
		}

		p.Out(fmt.Sprintf("%-4s %-12s %-7s %-7s %7.1fms  %s",
			idxStr, timeStr, e.Method, statusStr, e.ElapsedMs, targetStr))
	}
}

func printHistoryEntry(e *history.Entry, p *output.Printer) {
	statusStr := fmt.Sprintf("%d %s", e.Status, e.Reason)
	if p.Color {
		if e.Status >= 200 && e.Status < 300 {
			statusStr = p.Green(statusStr)
		} else {
			statusStr = p.Red(statusStr)
		}
	}

	p.Out(p.Bold(fmt.Sprintf("Hit %s (%s)", e.ID, e.Timestamp)))
	if e.Ref != "" {
		p.Out(fmt.Sprintf("  Ref:      %s", e.Ref))
	}
	p.Out(fmt.Sprintf("  Endpoint: %s %s", e.Method, e.Url))
	p.Out(fmt.Sprintf("  Status:   %s (%.1fms, %d bytes)", statusStr, e.ElapsedMs, e.ResponseSize))

	if len(e.Headers) > 0 {
		p.Out(p.Dim("  Request Headers:"))
		for k, v := range e.Headers {
			p.Out(fmt.Sprintf("    %s: %s", k, v))
		}
	}
	if e.Body != "" {
		p.Out(p.Dim("  Request Body:"))
		p.Out(fmt.Sprintf("    %s", strings.ReplaceAll(e.Body, "\n", "\n    ")))
	}
	if e.ResponseBody != "" {
		p.Out(p.Dim("  Response Body:"))
		p.Out(fmt.Sprintf("    %s", strings.ReplaceAll(e.ResponseBody, "\n", "\n    ")))
	}
	if len(e.Tests) > 0 {
		p.Out(p.Dim("  Assertions:"))
		for _, t := range e.Tests {
			if t.Passed {
				p.Out(p.Green(fmt.Sprintf("    ✓ %s", t.Name)))
			} else {
				p.Out(p.Red(fmt.Sprintf("    ✗ %s (%s)", t.Name, t.Detail)))
			}
		}
	}
	if len(e.Captures) > 0 {
		p.Out(p.Dim("  Captures:"))
		for k, v := range e.Captures {
			p.Out(p.Dim(fmt.Sprintf("    ↳ %s = %v", k, v)))
		}
	}
	if e.Error != "" {
		p.Out(p.Red(fmt.Sprintf("  Error: %s", e.Error)))
	}
}

func cmdReplay(args []string, globals *GlobalFlags, p *output.Printer) error {
	var (
		idOrIdx         string
		diffMode        bool
		compareHeaders  bool
		bodyOnly        bool
		jsonOut         bool
		contextLines    = 3
		overrideHeaders = make(map[string]any)
	)

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--diff":
			diffMode = true
		case arg == "--headers":
			compareHeaders = true
		case arg == "--body-only":
			bodyOnly = true
		case arg == "--json":
			jsonOut = true
		case arg == "--context" && i+1 < len(args):
			if n, err := strconv.Atoi(args[i+1]); err == nil && n > 0 {
				contextLines = n
			}
			i++
		case (arg == "-H" || arg == "--header") && i+1 < len(args):
			parts := strings.SplitN(args[i+1], ":", 2)
			if len(parts) == 2 {
				overrideHeaders[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
			i++
		case strings.HasPrefix(arg, "-"):
			// Ignore other flags
		default:
			if idOrIdx == "" {
				idOrIdx = arg
			}
		}
	}

	if idOrIdx == "" {
		return zone.NewZoneError("usage: hit replay <ID|INDEX> [-e ENV] [--var k=v] [-H k:v] [--diff] [--headers] [--body-only] [--json]")
	}
	z, _ := zone.FindOrNone(globals.Zone)
	zoneRoot := ""
	if z != nil {
		zoneRoot = z.Root
	}
	store := history.NewStore(history.GetHistoryPath(zoneRoot))
	entry, err := store.Get(idOrIdx)
	if err != nil {
		return err
	}

	if !jsonOut {
		p.Out(p.Dim(fmt.Sprintf("replaying hit %s: %s %s", entry.ID, entry.Method, entry.Url)))
	}

	sess, err := runner.NewSession(runner.SessionOptions{
		Zone:              z,
		ServerName:        globals.Server,
		ExtraVars:         globals.Vars,
		Persist:           true,
		ZoneOptional:      true,
		Verify:            !globals.Insecure,
		NoHistory:         globals.NoHistory,
	})
	if err != nil {
		return err
	}
	defer sess.Close()

	var r *types.Result
	if z != nil && entry.Ref != "" {
		if path, err := z.ResolveRequest(entry.Ref); err == nil && path != "" {
			if sp, err := spec.LoadSpec(path, z.DefaultsChain(path)); err == nil {
				r = sess.RunSpec(sp, nil)
			}
		}
	}

	if r == nil {
		var headers map[string]any
		if len(entry.Headers) > 0 {
			headers = make(map[string]any)
			for k, v := range entry.Headers {
				if v != "[MASKED]" {
					headers[k] = v
				}
			}
		}
		for k, v := range overrideHeaders {
			if headers == nil {
				headers = make(map[string]any)
			}
			headers[k] = v
		}

		var body any
		var jsonBody any
		if entry.Body != "" {
			var parsed any
			if err := json.Unmarshal([]byte(entry.Body), &parsed); err == nil {
				jsonBody = parsed
			} else {
				body = entry.Body
			}
		}

		var tests []any
		if entry.Status > 0 {
			tests = append(tests, map[string]any{"status": entry.Status})
		}

		r = sess.Request(entry.Method, entry.Url, headers, nil, body, jsonBody, nil, nil, tests, nil, nil, entry.Ref)
	}

	if !diffMode {
		p.Result(r, false, 4000, true, false, sess.Context(nil, nil).Mask)
		if !r.OK() {
			os.Exit(ExitFail)
		}
		return nil
	}

	labelOld := fmt.Sprintf("Hit #%s (recorded)", idOrIdx)
	labelNew := fmt.Sprintf("Live Replay (%s)", time.Now().Format("15:04:05"))

	diffOpts := diff.Options{
		CompareHeaders: compareHeaders,
		BodyOnly:       bodyOnly,
		ContextLines:   contextLines,
	}

	diffRes := diff.Compare(
		labelOld, labelNew,
		entry.Status, entry.Reason, entry.ElapsedMs, entry.ResponseSize, entry.ResponseHeaders, entry.ResponseBody,
		r.Status, r.Reason, r.ElapsedMs, r.Size, r.Headers, r.Text,
		diffOpts,
	)

	if jsonOut {
		b, err := json.MarshalIndent(diffRes, "", "  ")
		if err != nil {
			return err
		}
		p.Out(string(b))
	} else {
		p.Out(diffRes.FormatTerminal(p, diffOpts))
	}

	if !r.OK() {
		os.Exit(ExitFail)
	}
	return nil
}

func cmdDiff(args []string, globals *GlobalFlags, p *output.Printer) error {
	var (
		posArgs        []string
		compareHeaders bool
		bodyOnly       bool
		jsonOut        bool
		contextLines   = 3
	)

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--headers":
			compareHeaders = true
		case arg == "--body-only":
			bodyOnly = true
		case arg == "--json":
			jsonOut = true
		case arg == "--context" && i+1 < len(args):
			if n, err := strconv.Atoi(args[i+1]); err == nil && n > 0 {
				contextLines = n
			}
			i++
		case strings.HasPrefix(arg, "-"):
			// Ignore other flags
		default:
			posArgs = append(posArgs, arg)
		}
	}

	if len(posArgs) == 0 {
		return zone.NewZoneError("usage: hit diff <ID|INDEX> [<ID|INDEX>] [--headers] [--body-only] [--json] [--context N]")
	}

	var id1, id2 string
	if len(posArgs) == 1 {
		if posArgs[0] == "1" {
			id1, id2 = "2", "1"
		} else {
			id1, id2 = posArgs[0], "1"
		}
	} else {
		id1, id2 = posArgs[0], posArgs[1]
	}

	z, _ := zone.FindOrNone(globals.Zone)
	zoneRoot := ""
	if z != nil {
		zoneRoot = z.Root
	}
	store := history.NewStore(history.GetHistoryPath(zoneRoot))

	entry1, err := store.Get(id1)
	if err != nil {
		return fmt.Errorf("diff hit %s: %w", id1, err)
	}
	entry2, err := store.Get(id2)
	if err != nil {
		return fmt.Errorf("diff hit %s: %w", id2, err)
	}

	label1 := fmt.Sprintf("Hit #%s [%s %s]", id1, entry1.Method, entry1.Url)
	label2 := fmt.Sprintf("Hit #%s [%s %s]", id2, entry2.Method, entry2.Url)

	diffOpts := diff.Options{
		CompareHeaders: compareHeaders,
		BodyOnly:       bodyOnly,
		ContextLines:   contextLines,
	}

	res := diff.Compare(
		label1, label2,
		entry1.Status, entry1.Reason, entry1.ElapsedMs, entry1.ResponseSize, entry1.ResponseHeaders, entry1.ResponseBody,
		entry2.Status, entry2.Reason, entry2.ElapsedMs, entry2.ResponseSize, entry2.ResponseHeaders, entry2.ResponseBody,
		diffOpts,
	)

	if jsonOut {
		b, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return err
		}
		p.Out(string(b))
	} else {
		p.Out(res.FormatTerminal(p, diffOpts))
	}

	return nil
}

func cmdReport(args []string, globals *GlobalFlags, p *output.Printer) error {
	if len(args) == 0 {
		return zone.NewZoneError("usage: hit report [html|har|coverage|latency] [OPTIONS]")
	}
	sub := args[0]
	subArgs := args[1:]
	z, _ := zone.FindOrNone(globals.Zone)

	switch sub {
	case "html":
		outPath := "hit-report.html"
		limit := 50
		for i := 0; i < len(subArgs); i++ {
			a := subArgs[i]
			if (a == "-o" || a == "--out") && i+1 < len(subArgs) {
				outPath = subArgs[i+1]
				i++
			} else if (a == "-n" || a == "--limit") && i+1 < len(subArgs) {
				limit, _ = strconv.Atoi(subArgs[i+1])
				i++
			} else if !strings.HasPrefix(a, "-") {
				outPath = a
			}
		}

		zoneRoot := ""
		if z != nil {
			zoneRoot = z.Root
		}
		store := history.NewStore(history.GetHistoryPath(zoneRoot))
		entries, err := store.List(limit, 0, "", "")
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return fmt.Errorf("no historical hits found to report. Run some requests first")
		}

		var results []*types.Result
		for _, e := range entries {
			var tests []assertions.TestResult
			for _, t := range e.Tests {
				tests = append(tests, assertions.TestResult{
					Name:   t.Name,
					Passed: t.Passed,
					Detail: t.Detail,
				})
			}
			r := &types.Result{
				Name:           e.Ref,
				Ref:            e.Ref,
				Method:         e.Method,
				Url:            e.Url,
				RequestHeaders: e.Headers,
				RequestBody:    e.Body,
				Status:         e.Status,
				HasStatus:      e.Status > 0,
				Reason:         e.Reason,
				Headers:        e.ResponseHeaders,
				Text:           e.ResponseBody,
				ElapsedMs:      e.ElapsedMs,
				Size:           e.ResponseSize,
				Tests:          tests,
				Captures:       e.Captures,
				Error:          e.Error,
			}
			results = append(results, r)
		}

		if err := report.WriteHTMLReport(results, "hit Historical Test Report", outPath); err != nil {
			return err
		}
		p.Out(p.Green(fmt.Sprintf("✓ HTML report written to %s (included %d recent hits)", outPath, len(results))))
		return nil

	case "har":
		outPath := "hit-history.har"
		limit := 50
		statusFilter := 0
		refFilter := ""
		for i := 0; i < len(subArgs); i++ {
			a := subArgs[i]
			if (a == "-o" || a == "--out" || a == "--output") && i+1 < len(subArgs) {
				outPath = subArgs[i+1]
				i++
			} else if (a == "-n" || a == "--limit") && i+1 < len(subArgs) {
				limit, _ = strconv.Atoi(subArgs[i+1])
				i++
			} else if a == "--status" && i+1 < len(subArgs) {
				statusFilter, _ = strconv.Atoi(subArgs[i+1])
				i++
			} else if (a == "--ref" || a == "--folder") && i+1 < len(subArgs) {
				refFilter = subArgs[i+1]
				i++
			} else if !strings.HasPrefix(a, "-") {
				outPath = a
			}
		}

		zoneRoot := ""
		if z != nil {
			zoneRoot = z.Root
		}
		store := history.NewStore(history.GetHistoryPath(zoneRoot))
		entries, err := store.List(limit, statusFilter, "", refFilter)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return fmt.Errorf("no historical hits found to export to HAR. Run some requests first")
		}

		har := report.BuildHARFromHistory(entries)
		if err := report.WriteHAR(har, outPath); err != nil {
			return err
		}
		p.Out(p.Green(fmt.Sprintf("✓ HAR archive written to %s (included %d recent hits)", outPath, len(entries))))
		return nil

	case "coverage":
		specFile := ""
		jsonOutput := false
		for i := 0; i < len(subArgs); i++ {
			a := subArgs[i]
			if (a == "-s" || a == "--openapi" || a == "--spec") && i+1 < len(subArgs) {
				specFile = subArgs[i+1]
				i++
			} else if a == "--json" {
				jsonOutput = true
			} else if !strings.HasPrefix(a, "-") {
				specFile = a
			}
		}

		if specFile == "" {
			return zone.NewZoneError("usage: hit report coverage --openapi <SPEC.yaml|url>")
		}

		data, err := report.LoadSpecData(specFile)
		if err != nil {
			return fmt.Errorf("error loading spec %s: %v", specFile, err)
		}

		rep, err := report.AuditCoverage(data, z)
		if err != nil {
			return err
		}

		if jsonOutput {
			b, _ := json.MarshalIndent(rep, "", "  ")
			p.Out(string(b))
			return nil
		}

		p.Out(report.FormatCoverageTable(rep, p.Color))
		return nil

	case "latency", "perf", "trends":
		threshold := 20.0
		limit := 500
		jsonOutput := false
		for i := 0; i < len(subArgs); i++ {
			a := subArgs[i]
			if (a == "-t" || a == "--threshold") && i+1 < len(subArgs) {
				threshold, _ = strconv.ParseFloat(subArgs[i+1], 64)
				i++
			} else if (a == "-n" || a == "--limit") && i+1 < len(subArgs) {
				limit, _ = strconv.Atoi(subArgs[i+1])
				i++
			} else if a == "--json" {
				jsonOutput = true
			}
		}

		zoneRoot := ""
		if z != nil {
			zoneRoot = z.Root
		}
		store := history.NewStore(history.GetHistoryPath(zoneRoot))
		entries, err := store.List(limit, 0, "", "")
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return fmt.Errorf("no historical hits found to analyze. Run some requests first")
		}

		rep := report.AnalyzeLatency(entries, threshold)
		if jsonOutput {
			b, _ := json.MarshalIndent(rep, "", "  ")
			p.Out(string(b))
			return nil
		}

		p.Out(report.FormatLatencyTable(rep, p.Color))
		if rep.RegressionAlerts > 0 {
			os.Exit(ExitFail)
		}
		return nil

	default:
		return zone.NewZoneError("unknown report type '%s'. Available: html, coverage, latency", sub)
	}
}

func cmdAssert(args []string, globals *GlobalFlags, p *output.Printer) error {
	opts := assertions.DefaultInferOptions()
	var (
		positional  []string
		method      string
		saveMode    bool
		appendMode  bool
		destFile    string
		jsonOutput  bool
		headersList []string
		queryList   []string
		bodyRaw     string
		jsonBodyRaw string
	)

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-h":
			p.Out(`Usage:
  hit assert <URL | ID | REF> [flags]

Flags:
  -s, --strict        Assert exact scalar values instead of general types
  --no-latency        Do not generate latency SLA (max_ms) assertion
  --no-headers        Do not generate header assertions
  --depth N           Maximum depth for nested JSON properties (default: 3)
  --save [FILE]       Save generated assertions into a request YAML file
  -a, --append [FILE] Append assertions to existing tests in a request YAML file
  --json              Output generated assertions in JSON format
  -m, --method METHOD HTTP method for ad hoc request (default: GET)
  -H, --header K:V    Header for ad hoc request (repeatable)
  -q, --query K=V     Query param for ad hoc request (repeatable)
  -j, --json-body J   JSON body for ad hoc request
  -b, --body B        Raw body for ad hoc request`)
			return nil

		case a == "-s" || a == "--strict":
			opts.Strict = true
		case a == "--no-latency":
			opts.IncludeLatency = false
		case a == "--no-headers":
			opts.IncludeHeaders = false
		case a == "--depth" && i+1 < len(args):
			if d, err := strconv.Atoi(args[i+1]); err == nil && d > 0 {
				opts.MaxDepth = d
			}
			i++
		case a == "--json":
			jsonOutput = true
		case a == "--save":
			saveMode = true
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				destFile = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--save="):
			saveMode = true
			destFile = strings.TrimPrefix(a, "--save=")
		case a == "-a" || a == "--append":
			appendMode = true
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				destFile = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--append="):
			appendMode = true
			destFile = strings.TrimPrefix(a, "--append=")
		case (a == "-m" || a == "--method") && i+1 < len(args):
			method = strings.ToUpper(args[i+1])
			i++
		case (a == "-H" || a == "--header") && i+1 < len(args):
			headersList = append(headersList, args[i+1])
			i++
		case (a == "-q" || a == "--query") && i+1 < len(args):
			queryList = append(queryList, args[i+1])
			i++
		case (a == "-b" || a == "--body") && i+1 < len(args):
			bodyRaw = args[i+1]
			i++
		case (a == "-j" || a == "--json-body") && i+1 < len(args):
			jsonBodyRaw = args[i+1]
			i++
		default:
			if !strings.HasPrefix(a, "-") {
				positional = append(positional, a)
			}
		}
	}

	if len(positional) == 0 {
		return zone.NewZoneError("usage: hit assert <URL | ID | REF> [--strict] [--no-latency] [--no-headers] [--save [FILE]] [--append [FILE]] [--json]")
	}

	var target string
	if len(positional) >= 2 && isHTTPMethod(positional[0]) {
		method = strings.ToUpper(positional[0])
		target = positional[1]
	} else {
		target = positional[0]
	}

	z, _ := zone.FindOrNone(globals.Zone)
	zoneRoot := ""
	if z != nil {
		zoneRoot = z.Root
	}

	var (
		respStatus    int
		respElapsedMs float64
		respHeaders   = make(map[string]string)
		respBody      string
		historyEntry  *history.Entry
		sourceURL     = target
		sourceMethod  = method
	)

	isHistoryIndex := false
	if idx, err := strconv.Atoi(target); err == nil && idx > 0 {
		isHistoryIndex = true
	}

	if isHistoryIndex {
		store := history.NewStore(history.GetHistoryPath(zoneRoot))
		entry, err := store.Get(target)
		if err != nil {
			return err
		}
		historyEntry = entry
		respStatus = entry.Status
		respElapsedMs = entry.ElapsedMs
		respHeaders = entry.ResponseHeaders
		respBody = entry.ResponseBody
		sourceURL = entry.Url
		sourceMethod = entry.Method
	} else if isURLOrPath(target) && strings.Contains(target, "://") {
		headers, err := parseKV(headersList, ":")
		if err != nil {
			return err
		}
		query, err := parseKV(queryList, "=")
		if err != nil {
			return err
		}
		var body any
		var jsonBody any
		if jsonBodyRaw != "" {
			text, err := readArg(jsonBodyRaw)
			if err != nil {
				return err
			}
			var parsed any
			if err := json.Unmarshal([]byte(text), &parsed); err != nil {
				return zone.NewZoneError("invalid JSON for --json-body: %v", err)
			}
			jsonBody = parsed
		} else if bodyRaw != "" {
			text, err := readArg(bodyRaw)
			if err != nil {
				return err
			}
			body = text
		}

		sess, err := runner.NewSession(runner.SessionOptions{
			Zone:              z,
			ServerName:        globals.Server,
			ExtraVars:         globals.Vars,
			ZoneOptional:      true,
			Verify:            !globals.Insecure,
			NoHistory:         globals.NoHistory,
		})
		if err != nil {
			return err
		}
		defer sess.Close()

		if method == "" {
			method = "GET"
		}
		sourceMethod = method

		r := sess.Request(method, target, headers, query, body, jsonBody, nil, nil, nil, nil, nil, "assert")
		if r.Error != "" {
			return fmt.Errorf("request to %s failed: %s", target, r.Error)
		}
		respStatus = r.Status
		respElapsedMs = r.ElapsedMs
		respHeaders = r.Headers
		respBody = r.Text
	} else {
		resolvedSpecPath := ""
		if z != nil {
			if sp, err := z.ResolveRequest(target); err == nil && sp != "" {
				if s, err := os.Stat(sp); err == nil && !s.IsDir() {
					resolvedSpecPath = sp
				}
			}
		}
		if resolvedSpecPath == "" {
			if s, err := os.Stat(target); err == nil && !s.IsDir() && (strings.HasSuffix(target, ".yaml") || strings.HasSuffix(target, ".yml")) {
				resolvedSpecPath = target
			}
		}

		if resolvedSpecPath != "" {
			if destFile == "" {
				destFile = resolvedSpecPath
			}
			var defaults []map[string]any
			if z != nil {
				defaults = z.DefaultsChain(resolvedSpecPath)
			}
			sp, err := spec.LoadSpec(resolvedSpecPath, defaults)
			if err != nil {
				return err
			}
			sourceURL = sp.Url
			sourceMethod = sp.Method

			sess, err := runner.NewSession(runner.SessionOptions{
				Zone:              z,
				ServerName:        globals.Server,
				ExtraVars:         globals.Vars,
				ZoneOptional:      true,
				Verify:            !globals.Insecure,
				NoHistory:         globals.NoHistory,
			})
			if err != nil {
				return err
			}
			defer sess.Close()

			r := sess.RunSpec(sp, nil)
			if r.Error != "" {
				return fmt.Errorf("request '%s' failed: %s", target, r.Error)
			}
			respStatus = r.Status
			respElapsedMs = r.ElapsedMs
			respHeaders = r.Headers
			respBody = r.Text
		} else {
			store := history.NewStore(history.GetHistoryPath(zoneRoot))
			entry, err := store.Get(target)
			if err == nil {
				historyEntry = entry
				respStatus = entry.Status
				respElapsedMs = entry.ElapsedMs
				respHeaders = entry.ResponseHeaders
				respBody = entry.ResponseBody
				sourceURL = entry.Url
				sourceMethod = entry.Method
			} else if z != nil && strings.HasPrefix(target, "/") {
				sess, err := runner.NewSession(runner.SessionOptions{
					Zone:              z,
					ServerName:        globals.Server,
					ExtraVars:         globals.Vars,
					ZoneOptional:      true,
					Verify:            !globals.Insecure,
					NoHistory:         globals.NoHistory,
				})
				if err != nil {
					return err
				}
				defer sess.Close()
				if method == "" {
					method = "GET"
				}
				sourceMethod = method

				r := sess.Request(method, target, nil, nil, nil, nil, nil, nil, nil, nil, nil, "assert")
				if r.Error != "" {
					return fmt.Errorf("request to %s failed: %s", target, r.Error)
				}
				respStatus = r.Status
				respElapsedMs = r.ElapsedMs
				respHeaders = r.Headers
				respBody = r.Text
			} else {
				return fmt.Errorf("target '%s' not found: not a valid URL, zone request, file, or history ID", target)
			}
		}
	}

	inferredTests, yamlStr, err := assertions.InferFromResponse(
		respStatus,
		respElapsedMs,
		respHeaders,
		respBody,
		opts,
	)
	if err != nil {
		return fmt.Errorf("failed to infer assertions: %w", err)
	}

	if !saveMode && !appendMode {
		if jsonOutput {
			b, err := json.MarshalIndent(map[string]any{"tests": inferredTests}, "", "  ")
			if err != nil {
				return err
			}
			p.Out(string(b))
			return nil
		}
		p.Out(strings.TrimRight(yamlStr, "\n"))
		return nil
	}

	if destFile == "" {
		return zone.NewZoneError("--save or --append requires a target file path when asserting a URL or history entry")
	}

	if z != nil && !filepath.IsAbs(destFile) && !strings.HasPrefix(destFile, ".") && !strings.HasPrefix(destFile, "collections") {
		if _, err := os.Stat(destFile); os.IsNotExist(err) {
			destFile = filepath.Join(z.CollectionsDir(), destFile)
		}
	}
	if !strings.HasSuffix(destFile, ".yaml") && !strings.HasSuffix(destFile, ".yml") {
		destFile += ".yaml"
	}

	addedCount, err := updateFileTests(destFile, inferredTests, appendMode, sourceURL, sourceMethod, historyEntry)
	if err != nil {
		return err
	}

	rel := destFile
	if z != nil {
		if r, err := filepath.Rel(z.Root, destFile); err == nil {
			rel = r
		}
	}

	if appendMode {
		p.Out(p.Green(fmt.Sprintf("✓ appended %d assertions to %s", addedCount, rel)))
	} else {
		p.Out(p.Green(fmt.Sprintf("✓ saved %d assertions to %s", len(inferredTests), rel)))
	}
	return nil
}

func updateFileTests(destFile string, newTests []map[string]any, appendMode bool, sourceURL, sourceMethod string, historyEntry *history.Entry) (int, error) {
	if _, err := os.Stat(destFile); os.IsNotExist(err) {
		if historyEntry != nil {
			return len(newTests), history.SaveAsRequestWithInference(historyEntry, destFile, true)
		}
		_ = os.MkdirAll(filepath.Dir(destFile), 0755)
		name := strings.Title(strings.ReplaceAll(strings.TrimSuffix(filepath.Base(destFile), filepath.Ext(destFile)), "-", " "))
		reqMethod := "GET"
		if sourceMethod != "" {
			reqMethod = sourceMethod
		}
		reqURL := "{{base_url}}/"
		if isURLOrPath(sourceURL) && strings.Contains(sourceURL, "://") {
			reqURL = sourceURL
		}
		data := map[string]any{
			"name":   name,
			"method": reqMethod,
			"url":    reqURL,
			"tests":  newTests,
		}
		y, err := zone.DumpYAML(data)
		if err != nil {
			return 0, err
		}
		return len(newTests), os.WriteFile(destFile, []byte(y), 0644)
	}

	fileBytes, err := os.ReadFile(destFile)
	if err != nil {
		return 0, err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(fileBytes, &doc); err != nil {
		return 0, fmt.Errorf("failed to parse YAML in %s: %w", destFile, err)
	}

	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return 0, fmt.Errorf("invalid request file %s: root must be a YAML mapping", destFile)
	}

	m := doc.Content[0]

	newTestsBytes, err := yaml.Marshal(newTests)
	if err != nil {
		return 0, err
	}
	var tempDoc yaml.Node
	if err := yaml.Unmarshal(newTestsBytes, &tempDoc); err != nil {
		return 0, err
	}
	if len(tempDoc.Content) == 0 {
		return 0, fmt.Errorf("failed to marshal inferred tests")
	}
	newSeqNode := tempDoc.Content[0]

	foundIdx := -1
	for i := 0; i < len(m.Content); i += 2 {
		if m.Content[i].Value == "tests" {
			foundIdx = i
			break
		}
	}

	addedCount := len(newTests)

	if foundIdx != -1 {
		if appendMode && m.Content[foundIdx+1].Kind == yaml.SequenceNode {
			existingSeq := m.Content[foundIdx+1]
			existingSet := make(map[string]bool)
			for _, item := range existingSeq.Content {
				b, _ := yaml.Marshal(item)
				existingSet[string(b)] = true
			}

			addedCount = 0
			for _, newItem := range newSeqNode.Content {
				b, _ := yaml.Marshal(newItem)
				if !existingSet[string(b)] {
					existingSeq.Content = append(existingSeq.Content, newItem)
					existingSet[string(b)] = true
					addedCount++
				}
			}
		} else {
			m.Content[foundIdx+1] = newSeqNode
		}
	} else {
		m.Content = append(m.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "tests"},
			newSeqNode,
		)
	}

	outBytes, err := yaml.Marshal(&doc)
	if err != nil {
		return 0, err
	}

	return addedCount, os.WriteFile(destFile, outBytes, 0644)
}

func cmdSnippet(args []string, globals *GlobalFlags, p *output.Printer) error {
	var (
		method      = ""
		target      = ""
		headersList []string
		queryList   []string
		bodyRaw     string
		jsonBodyRaw string
		authStr     string
		lang        = ""
		extractPath = ""
	)

	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "-m" || a == "--method") && i+1 < len(args):
			method = strings.ToUpper(args[i+1])
			i++
		case (a == "-H" || a == "--header") && i+1 < len(args):
			headersList = append(headersList, args[i+1])
			i++
		case (a == "-q" || a == "--query") && i+1 < len(args):
			queryList = append(queryList, args[i+1])
			i++
		case (a == "-b" || a == "--body") && i+1 < len(args):
			bodyRaw = args[i+1]
			i++
		case (a == "-j" || a == "--json-body") && i+1 < len(args):
			jsonBodyRaw = args[i+1]
			i++
		case a == "--auth" && i+1 < len(args):
			authStr = args[i+1]
			i++
		case (a == "-l" || a == "--lang") && i+1 < len(args):
			lang = args[i+1]
			i++
		case strings.HasPrefix(a, "--lang="):
			lang = strings.TrimPrefix(a, "--lang=")
		case a == "--python" || a == "--py":
			lang = "python"
		case a == "--php":
			lang = "php"
		case a == "--js" || a == "--javascript" || a == "--node":
			lang = "javascript"
		case a == "--go" || a == "--golang":
			lang = "go"
		case a == "--all":
			lang = "all"
		case (a == "-x" || a == "--extract") && i+1 < len(args):
			extractPath = args[i+1]
			i++
		case strings.HasPrefix(a, "--extract="):
			extractPath = strings.TrimPrefix(a, "--extract=")
		default:
			if !strings.HasPrefix(a, "-") {
				positional = append(positional, a)
			}
		}
	}

	if len(positional) == 0 {
		return zone.NewZoneError("usage: hit snippet <REF|URL> [--lang python|php|js|go|all] [--extract PATH]")
	} else if len(positional) == 1 {
		target = positional[0]
	} else {
		if isHTTPMethod(positional[0]) {
			method = strings.ToUpper(positional[0])
			target = positional[1]
		} else {
			target = positional[0]
		}
	}

	reqInfo, err := resolveSnippetOrFuzzRequest(target, method, headersList, queryList, jsonBodyRaw, bodyRaw, authStr, extractPath, globals)
	if err != nil {
		return err
	}

	if lang == "" || strings.ToLower(lang) == "all" {
		p.Out(snippet.GenerateAll(reqInfo))
		return nil
	}

	code, err := snippet.GenerateSnippet(lang, reqInfo)
	if err != nil {
		return err
	}
	p.Out(code)
	return nil
}

func cmdFuzz(args []string, globals *GlobalFlags, p *output.Printer) error {
	var (
		method       = ""
		target       = ""
		headersList  []string
		queryList    []string
		bodyRaw      string
		jsonBodyRaw  string
		authStr      string
		maxMutations = 50
		categories   []string
		failOn5xx    = false
		timeout      = 5 * time.Second
		jsonOut      = false
		verbose      = false
		sarifPath    = ""
	)

	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "-m" || a == "--method") && i+1 < len(args):
			method = strings.ToUpper(args[i+1])
			i++
		case (a == "-H" || a == "--header") && i+1 < len(args):
			headersList = append(headersList, args[i+1])
			i++
		case (a == "-q" || a == "--query") && i+1 < len(args):
			queryList = append(queryList, args[i+1])
			i++
		case (a == "-b" || a == "--body") && i+1 < len(args):
			bodyRaw = args[i+1]
			i++
		case (a == "-j" || a == "--json-body") && i+1 < len(args):
			jsonBodyRaw = args[i+1]
			i++
		case a == "--auth" && i+1 < len(args):
			authStr = args[i+1]
			i++
		case (a == "-n" || a == "--mutations") && i+1 < len(args):
			maxMutations, _ = strconv.Atoi(args[i+1])
			i++
		case strings.HasPrefix(a, "-n="):
			maxMutations, _ = strconv.Atoi(strings.TrimPrefix(a, "-n="))
		case strings.HasPrefix(a, "--mutations="):
			maxMutations, _ = strconv.Atoi(strings.TrimPrefix(a, "--mutations="))
		case (a == "-c" || a == "--categories" || a == "--category") && i+1 < len(args):
			for _, cat := range strings.Split(args[i+1], ",") {
				if ct := strings.TrimSpace(cat); ct != "" {
					categories = append(categories, ct)
				}
			}
			i++
		case strings.HasPrefix(a, "--categories="):
			for _, cat := range strings.Split(strings.TrimPrefix(a, "--categories="), ",") {
				if ct := strings.TrimSpace(cat); ct != "" {
					categories = append(categories, ct)
				}
			}
		case a == "--fail-on-5xx":
			failOn5xx = true
		case (a == "-t" || a == "--timeout") && i+1 < len(args):
			if d, err := time.ParseDuration(args[i+1]); err == nil {
				timeout = d
			}
			i++
		case a == "--sarif" && i+1 < len(args):
			sarifPath = args[i+1]
			i++
		case strings.HasPrefix(a, "--sarif="):
			sarifPath = strings.TrimPrefix(a, "--sarif=")
		case a == "--json":
			jsonOut = true
		case a == "-v" || a == "--verbose":
			verbose = true
		default:
			if !strings.HasPrefix(a, "-") {
				positional = append(positional, a)
			}
		}
	}

	if len(positional) == 0 {
		return zone.NewZoneError("usage: hit fuzz <REF|URL> [-n N] [--categories CATS] [--fail-on-5xx] [--json] [-v]")
	} else if len(positional) == 1 {
		target = positional[0]
	} else {
		if isHTTPMethod(positional[0]) {
			method = strings.ToUpper(positional[0])
			target = positional[1]
		} else {
			target = positional[0]
		}
	}

	reqInfo, err := resolveSnippetOrFuzzRequest(target, method, headersList, queryList, jsonBodyRaw, bodyRaw, authStr, "", globals)
	if err != nil {
		return err
	}

	fuzzOpts := fuzz.FuzzOptions{
		MaxMutations: maxMutations,
		Categories:   categories,
		FailOn5xx:    failOn5xx,
		Timeout:      timeout,
		Insecure:     globals.Insecure,
		Verbose:      verbose,
	}

	if verbose && !jsonOut {
		fuzzOpts.OnMutation = func(m fuzz.Mutation, statusCode int, durationMs float64, reqErr error) {
			statusStr := fmt.Sprintf("%d", statusCode)
			if reqErr != nil {
				statusStr = "ERR"
			}
			catTag := fmt.Sprintf("[%s]", m.Category)
			p.Out(fmt.Sprintf("  %s %-12s %s %s (%.1fms)", p.Dim(m.Method), p.Dim(catTag), statusStr, m.Name, durationMs))
		}
	}

	if !jsonOut {
		p.Out(p.Bold("⚡ hit Mutation & Fuzz Testing Engine"))
		p.Out(fmt.Sprintf("Target: %s %s", p.Cyan(reqInfo.Method), reqInfo.URL))
		p.Out(fmt.Sprintf("Categories: %s | Max Mutations: %d", formatCategories(categories), maxMutations))
		p.Out(p.Dim("----------------------------------------------------------------------"))
	}

	res, err := fuzz.Run(reqInfo.Method, reqInfo.URL, reqInfo.Headers, reqInfo.Query, reqInfo.Body, fuzzOpts)
	if err != nil {
		return err
	}

	if jsonOut {
		b, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return err
		}
		p.Out(string(b))
	} else {
		printFuzzSummary(res, p)
	}

	if sarifPath != "" {
		targetLoc := target
		if reqInfo.URL != "" {
			targetLoc = reqInfo.URL
		}
		if err := report.WriteFuzzSARIF(res, targetLoc, sarifPath); err != nil {
			fmt.Fprintf(os.Stderr, "error writing SARIF report: %v\n", err)
		} else if !jsonOut {
			p.Out(p.Green(fmt.Sprintf("✓ SARIF v2.1.0 security report written to %s", sarifPath)))
		}
	}

	if failOn5xx && res.ServerErrors5xx > 0 {
		return zone.NewZoneError("fuzzing failed: detected %d server 5xx crash(es)", res.ServerErrors5xx)
	}

	return nil
}

func resolveSnippetOrFuzzRequest(target, method string, headersList, queryList []string, jsonBodyRaw, bodyRaw, authStr, extractPath string, globals *GlobalFlags) (snippet.RequestInfo, error) {
	z, _ := zone.FindOrNone(globals.Zone)

	// Check if target is a zone ref
	if !strings.Contains(target, "://") && !strings.HasPrefix(target, "localhost:") && !strings.HasPrefix(target, "127.0.0.1:") {
		sess, err := runner.NewSession(runner.SessionOptions{
			Zone: z,
			ServerName: globals.Server,
			ExtraVars: globals.Vars,
			Persist:   false,
			Verify:    !globals.Insecure,
		})
		if err == nil {
			defer sess.Close()
			if sp, err := sess.Load(target); err == nil {
				_, prepared, _, err := sess.Prepare(sp, nil)
				if err == nil {
					finalMethod := prepared.Method
					if method != "" {
						finalMethod = method
					}
					reqHeaders := make(map[string]string)
					for k, v := range prepared.Headers {
						reqHeaders[k] = v
					}
					if len(headersList) > 0 {
						extraHeaders, err := parseStringKV(headersList, ":")
						if err == nil {
							for k, v := range extraHeaders {
								reqHeaders[k] = v
							}
						}
					}
					finalBody := prepared.Content
					if jsonBodyRaw != "" {
						t, _ := readArg(jsonBodyRaw)
						finalBody = []byte(t)
					} else if bodyRaw != "" {
						t, _ := readArg(bodyRaw)
						finalBody = []byte(t)
					}
					return snippet.RequestInfo{
						Method:      finalMethod,
						URL:         prepared.FullURL(),
						Headers:     reqHeaders,
						Body:        finalBody,
						ExtractPath: extractPath,
					}, nil
				}
			}
		}
	}

	// Ad-hoc URL target
	rawURL := target
	if !strings.Contains(rawURL, "://") {
		rawURL = "http://" + rawURL
	}

	reqHeaders, err := parseStringKV(headersList, ":")
	if err != nil {
		return snippet.RequestInfo{}, err
	}
	if reqHeaders == nil {
		reqHeaders = make(map[string]string)
	}

	reqQuery, err := parseStringKV(queryList, "=")
	if err != nil {
		return snippet.RequestInfo{}, err
	}

	if authStr != "" {
		if strings.HasPrefix(strings.ToLower(authStr), "bearer:") {
			reqHeaders["Authorization"] = "Bearer " + strings.TrimPrefix(authStr, "bearer:")
		} else if strings.HasPrefix(strings.ToLower(authStr), "basic:") {
			creds := strings.TrimPrefix(authStr, "basic:")
			reqHeaders["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(creds))
		}
	}

	var reqBody []byte
	if jsonBodyRaw != "" {
		t, err := readArg(jsonBodyRaw)
		if err != nil {
			return snippet.RequestInfo{}, err
		}
		reqBody = []byte(t)
		if _, exists := reqHeaders["Content-Type"]; !exists {
			reqHeaders["Content-Type"] = "application/json"
		}
	} else if bodyRaw != "" {
		t, err := readArg(bodyRaw)
		if err != nil {
			return snippet.RequestInfo{}, err
		}
		reqBody = []byte(t)
	}

	finalMethod := "GET"
	if method != "" {
		finalMethod = method
	} else if len(reqBody) > 0 {
		finalMethod = "POST"
	}

	return snippet.RequestInfo{
		Method:      finalMethod,
		URL:         rawURL,
		Headers:     reqHeaders,
		Query:       reqQuery,
		Body:        reqBody,
		ExtractPath: extractPath,
	}, nil
}

func parseStringKV(items []string, sep string) (map[string]string, error) {
	if len(items) == 0 {
		return make(map[string]string), nil
	}
	out := make(map[string]string)
	for _, it := range items {
		idx := strings.Index(it, sep)
		if idx < 0 {
			return nil, zone.NewZoneError("expected key%svalue, got '%s'", sep, it)
		}
		k := strings.TrimSpace(it[:idx])
		v := strings.TrimSpace(it[idx+len(sep):])
		out[k] = v
	}
	return out, nil
}

func printFuzzSummary(res *fuzz.FuzzResult, p *output.Printer) {
	p.Out(p.Dim("----------------------------------------------------------------------"))
	p.Out(fmt.Sprintf("Total Mutations Executed: %d  (in %.1fms)", res.TotalMutations, res.ElapsedMs))
	p.Out(fmt.Sprintf("  %s Handled (4xx client rejections): %d", p.Green("✓"), res.Handled4xx))
	p.Out(fmt.Sprintf("  %s Accepted (2xx success):          %d", p.Green("✓"), res.Success2xx))

	if res.TransportErrors > 0 {
		p.Out(fmt.Sprintf("  %s Transport Errors (hangs/drops):  %d", p.Red("✗"), res.TransportErrors))
	} else {
		p.Out(fmt.Sprintf("  %s Transport Errors (hangs/drops):  0", p.Green("✓")))
	}

	if res.ServerErrors5xx > 0 {
		p.Out(fmt.Sprintf("  %s Server Crashes (5xx errors):     %d", p.Red("CRITICAL FAIL:"), res.ServerErrors5xx))
	} else {
		p.Out(fmt.Sprintf("  %s Server Crashes (5xx errors):     0", p.Green("✓")))
	}

	if len(res.Anomalies) > 0 {
		p.Out("")
		p.Out(p.Bold("⚠️ Detected Vulnerabilities & Anomalies:"))
		for i, a := range res.Anomalies {
			sevColor := p.Red
			if a.Severity == "MEDIUM" {
				sevColor = p.Yellow
			}
			p.Out(fmt.Sprintf("  %d. %s [%s] %s (HTTP %d, %.1fms)",
				i+1,
				sevColor(fmt.Sprintf("[%s]", a.Severity)),
				a.Category,
				a.MutationName,
				a.StatusCode,
				a.DurationMs,
			))
			p.Out(p.Dim(fmt.Sprintf("     Issue: %s", a.Issue)))
			if a.ResponseBody != "" {
				p.Out(p.Dim(fmt.Sprintf("     Response Preview: %s", a.ResponseBody)))
			}
		}
	} else {
		p.Out("")
		p.Out(p.Green("🎉 Clean Bill of Health! No 5xx server crashes, hangs, or unexpected drops detected."))
	}
}

func formatCategories(cats []string) string {
	if len(cats) == 0 {
		return "all (boundaries, types, strings, injection, nulls, headers)"
	}
	return strings.Join(cats, ", ")
}

func parseDurationFlag(s string) time.Duration {
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return time.Duration(f * float64(time.Second))
	}
	return 0
}

func readFileOrLiteral(s string) (string, error) {
	if strings.HasPrefix(s, "@") {
		filePath := strings.TrimPrefix(s, "@")
		b, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
		}
		return string(b), nil
	}
	return s, nil
}

func cmdGraphQL(args []string, globals *GlobalFlags, p *output.Printer) error {
	var (
		target       = ""
		queryStr     = ""
		varsStr      = ""
		opName       = ""
		introspect   = false
		sdl          = false
		failOnErrors = false
		jsonOutput   = false
		headersList  []string
	)

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "-q" || a == "--query") && i+1 < len(args):
			queryStr = args[i+1]
			i++
		case (a == "-v" || a == "--vars" || a == "--variables") && i+1 < len(args):
			varsStr = args[i+1]
			i++
		case (a == "-o" || a == "--op" || a == "--operation") && i+1 < len(args):
			opName = args[i+1]
			i++
		case a == "--introspect":
			introspect = true
		case a == "--sdl":
			sdl = true
		case a == "--fail-on-errors":
			failOnErrors = true
		case a == "--json":
			jsonOutput = true
		case (a == "-H" || a == "--header") && i+1 < len(args):
			headersList = append(headersList, args[i+1])
			i++
		default:
			if !strings.HasPrefix(a, "-") && target == "" {
				target = a
			}
		}
	}

	if target == "" {
		return zone.NewZoneError("usage: hit graphql <URL|REF> [-q QUERY|@file] [-v VARS|@file] [-o OP] [--introspect] [--sdl] [--fail-on-errors] [--json]")
	}

	headers := make(map[string]string)
	for _, h := range headersList {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) == 2 {
			headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	targetURL := target
	// If target does not have a scheme and is not localhost/IP, try zone resolution
	if !strings.Contains(target, "://") && !strings.HasPrefix(target, "localhost:") && !strings.HasPrefix(target, "127.0.0.1:") {
		z, err := zone.Find(globals.Zone)
		if err == nil {
			reqPath, rErr := z.ResolveRequest(target)
			if rErr == nil {
				sp, sErr := spec.LoadSpec(reqPath, z.DefaultsChain(reqPath))
				if sErr == nil {
					targetURL = sp.Url
					// Inherit spec headers if not overridden
					for k, v := range sp.Headers {
						if _, ok := headers[k]; !ok {
							headers[k] = fmt.Sprintf("%v", v)
						}
					}
					// If spec has body.graphql, use it if queryStr not specified
					if queryStr == "" && sp.Body != nil {
						if gql, ok := sp.Body["graphql"].(map[string]any); ok {
							if q, ok := gql["query"].(string); ok && queryStr == "" {
								queryStr = q
							}
							if v, ok := gql["variables"]; ok && varsStr == "" {
								if b, mErr := json.Marshal(v); mErr == nil {
									varsStr = string(b)
								}
							}
						}
					}
				}
			}
		}
	}

	if introspect {
		if sdl {
			sdlStr, err := graphql.Introspect(targetURL, headers, 15*time.Second, globals.Insecure)
			if err != nil {
				return fmt.Errorf("introspection failed: %w", err)
			}
			p.Out(sdlStr)
			return nil
		}

		introSchema, err := graphql.IntrospectSchema(targetURL, headers, 15*time.Second, globals.Insecure)
		if err != nil {
			return fmt.Errorf("introspection failed: %w", err)
		}

		if jsonOutput {
			b, err := json.MarshalIndent(introSchema, "", "  ")
			if err != nil {
				return err
			}
			p.Out(string(b))
			return nil
		}

		p.Out(p.Bold("GraphQL Schema Introspection for ") + p.Cyan(targetURL))
		p.Out(p.Dim(strings.Repeat("─", 50)))
		queryType := "<none>"
		if introSchema.QueryType != nil {
			queryType = introSchema.QueryType.Name
		}
		mutationType := "<none>"
		if introSchema.MutationType != nil {
			mutationType = introSchema.MutationType.Name
		}
		subType := "<none>"
		if introSchema.SubscriptionType != nil {
			subType = introSchema.SubscriptionType.Name
		}
		p.Out(fmt.Sprintf("  Queries:       %s", p.Green(queryType)))
		p.Out(fmt.Sprintf("  Mutations:     %s", p.Yellow(mutationType)))
		p.Out(fmt.Sprintf("  Subscriptions: %s", p.Cyan(subType)))
		p.Out(fmt.Sprintf("  Total Types:   %d", len(introSchema.Types)))
		p.Out("")
		p.Out(p.Bold("Defined Types:"))
		for _, t := range introSchema.Types {
			if strings.HasPrefix(t.Name, "__") {
				continue // Skip built-in introspection types in default listing
			}
			p.Out(fmt.Sprintf("  • %-24s %s", p.Bold(t.Name), p.Dim(fmt.Sprintf("[%s]", t.Kind))))
		}
		p.Out("")
		p.Out(p.Dim("💡 Tip: Run 'hit schema graphql " + targetURL + "' or pass '--sdl' to export full GraphQL SDL schema."))
		return nil
	}

	q, err := readFileOrLiteral(queryStr)
	if err != nil {
		return err
	}
	if q == "" {
		return fmt.Errorf("no GraphQL query provided. Use -q/--query <query|@file>, or --introspect")
	}

	var variables map[string]any
	if varsStr != "" {
		vRaw, err := readFileOrLiteral(varsStr)
		if err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(vRaw), &variables); err != nil {
			return fmt.Errorf("invalid variables JSON: %w", err)
		}
	}

	res, err := graphql.Execute(graphql.ExecuteOptions{
		URL:           targetURL,
		Query:         q,
		Variables:     variables,
		OperationName: opName,
		Headers:       headers,
		Insecure:      globals.Insecure,
	})
	if err != nil {
		return err
	}

	if jsonOutput {
		if res.RawBody != nil {
			p.Out(string(res.RawBody))
		}
	} else {
		statusText := fmt.Sprintf("%d OK", res.StatusCode)
		if res.StatusCode >= 400 {
			statusText = fmt.Sprintf("%d ERROR", res.StatusCode)
		}
		p.Out(fmt.Sprintf("%s %s  →  %s  %s",
			p.Bold("POST"),
			targetURL,
			p.Green(statusText),
			p.Dim(fmt.Sprintf("%.0f ms", res.ElapsedMs)),
		))

		if len(res.Errors) > 0 {
			p.Out("")
			p.Out(p.Red("✗ GraphQL Errors:") + p.Dim(fmt.Sprintf(" (%d)", len(res.Errors))))
			for i, e := range res.Errors {
				locStr := ""
				if len(e.Locations) > 0 {
					var locs []string
					for _, l := range e.Locations {
						locs = append(locs, fmt.Sprintf("line %d, col %d", l.Line, l.Column))
					}
					locStr = p.Dim(" (" + strings.Join(locs, "; ") + ")")
				}
				pathStr := ""
				if len(e.Path) > 0 {
					var pParts []string
					for _, part := range e.Path {
						pParts = append(pParts, fmt.Sprintf("%v", part))
					}
					pathStr = p.Dim(" [path: " + strings.Join(pParts, ".") + "]")
				}
				p.Out(fmt.Sprintf("  %d. %s%s%s", i+1, p.Red(e.Message), locStr, pathStr))
			}
		}

		if res.Data != nil {
			p.Out("")
			b, err := json.MarshalIndent(res.Data, "", "  ")
			if err == nil {
				p.Out(string(b))
			}
		}
	}

	if failOnErrors && len(res.Errors) > 0 {
		return fmt.Errorf("graphql execution returned %d error(s)", len(res.Errors))
	}
	return nil
}

func cmdSchemaGraphQL(args []string, globals *GlobalFlags, p *output.Printer) error {
	var (
		target      = ""
		headersList []string
		jsonOutput  = false
	)

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "-H" || a == "--header") && i+1 < len(args):
			headersList = append(headersList, args[i+1])
			i++
		case a == "--json":
			jsonOutput = true
		default:
			if !strings.HasPrefix(a, "-") && target == "" {
				target = a
			}
		}
	}

	if target == "" {
		return zone.NewZoneError("usage: hit schema graphql <URL> [-H k:v] [--json]")
	}

	headers := make(map[string]string)
	for _, h := range headersList {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) == 2 {
			headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	if jsonOutput {
		introSchema, err := graphql.IntrospectSchema(target, headers, 15*time.Second, globals.Insecure)
		if err != nil {
			return fmt.Errorf("graphql introspection failed: %w", err)
		}
		b, err := json.MarshalIndent(introSchema, "", "  ")
		if err != nil {
			return err
		}
		p.Out(string(b))
		return nil
	}

	sdl, err := graphql.Introspect(target, headers, 15*time.Second, globals.Insecure)
	if err != nil {
		return fmt.Errorf("graphql introspection failed: %w", err)
	}

	p.Out(sdl)
	return nil
}

func cmdSSE(args []string, globals *GlobalFlags, p *output.Printer) error {
	var (
		target      = ""
		method      = "GET"
		headersList []string
		bodyRaw     = ""
		maxEvents   = 0
		timeout     time.Duration
		filterEvent = ""
		jsonOutput  = false
	)

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "-m" || a == "--method") && i+1 < len(args):
			method = strings.ToUpper(args[i+1])
			i++
		case (a == "-H" || a == "--header") && i+1 < len(args):
			headersList = append(headersList, args[i+1])
			i++
		case (a == "-b" || a == "--body" || a == "-j" || a == "--json-body") && i+1 < len(args):
			bodyRaw = args[i+1]
			i++
		case (a == "-n" || a == "--max" || a == "--limit") && i+1 < len(args):
			maxEvents, _ = strconv.Atoi(args[i+1])
			i++
		case (a == "-d" || a == "--timeout") && i+1 < len(args):
			timeout = parseDurationFlag(args[i+1])
			i++
		case a == "--event" && i+1 < len(args):
			filterEvent = args[i+1]
			i++
		case a == "--json":
			jsonOutput = true
		default:
			if !strings.HasPrefix(a, "-") && target == "" {
				target = a
			}
		}
	}

	if target == "" {
		return zone.NewZoneError("usage: hit sse <URL> [-H k:v] [-m METHOD] [-n MAX] [-d TIMEOUT] [--event NAME] [--json]")
	}

	headers := make(map[string]string)
	for _, h := range headersList {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) == 2 {
			headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	if !jsonOutput {
		p.Out(fmt.Sprintf("%s %s", p.Bold("📡 Connecting to SSE stream:"), p.Cyan(target)))
		p.Out(p.Dim(strings.Repeat("─", 50)))
	}

	onEvent := func(ev stream.SSEEvent) {
		if jsonOutput {
			return
		}
		meta := ""
		if ev.ID != "" {
			meta += p.Dim(" [id: " + ev.ID + "]")
		}
		if ev.Event != "" {
			meta += p.Yellow(" (" + ev.Event + ")")
		}
		deltaMs := float64(ev.Delta.Microseconds()) / 1000.0
		p.Out(fmt.Sprintf("%s%s %s", p.Cyan("▶ Event"), meta, p.Dim(fmt.Sprintf("+%.1fms", deltaMs))))
		for _, line := range strings.Split(ev.Data, "\n") {
			p.Out(fmt.Sprintf("    %s", line))
		}
	}

	ctx := context.Background()
	res, err := stream.StreamSSE(ctx, target, stream.SSEOptions{
		Method:      method,
		Headers:     headers,
		Body:        strings.NewReader(bodyRaw),
		Timeout:     timeout,
		MaxEvents:   maxEvents,
		FilterEvent: filterEvent,
		Insecure:    globals.Insecure,
		OnEvent:     onEvent,
	})
	if err != nil && res == nil {
		return fmt.Errorf("stream failed: %w", err)
	}

	if jsonOutput {
		b, mErr := json.MarshalIndent(res, "", "  ")
		if mErr != nil {
			return mErr
		}
		p.Out(string(b))
		return nil
	}

	p.Out("")
	p.Out(p.Dim(strings.Repeat("─", 50)))
	p.Out(p.Bold("Stream Metrics & Scorecard:"))
	p.Out(fmt.Sprintf("  HTTP Status:         %s", p.Green(fmt.Sprintf("%d OK", res.StatusCode))))
	p.Out(fmt.Sprintf("  TTFT (First Token):  %s", p.Cyan(fmt.Sprintf("%.1f ms", float64(res.TTFT.Microseconds())/1000.0))))
	p.Out(fmt.Sprintf("  Events Received:     %d", res.EventCount))
	p.Out(fmt.Sprintf("  Avg Event Latency:   %.1f ms", float64(res.AvgDelta.Microseconds())/1000.0))
	p.Out(fmt.Sprintf("  Total Duration:      %.2f s", res.TotalDuration.Seconds()))
	p.Out(fmt.Sprintf("  Total Bytes:         %s", output.HumanSize(res.TotalBytes)))

	return nil
}

func cmdWS(args []string, globals *GlobalFlags, p *output.Printer) error {
	var (
		target       = ""
		headersList  []string
		sendMessages []string
		expect       = ""
		timeout      time.Duration = 5 * time.Second
		maxMessages  = 0
		jsonOutput   = false
	)

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "-H" || a == "--header") && i+1 < len(args):
			headersList = append(headersList, args[i+1])
			i++
		case (a == "-m" || a == "--message" || a == "--send") && i+1 < len(args):
			sendMessages = append(sendMessages, args[i+1])
			i++
		case a == "--expect" && i+1 < len(args):
			expect = args[i+1]
			i++
		case (a == "-d" || a == "--timeout") && i+1 < len(args):
			timeout = parseDurationFlag(args[i+1])
			i++
		case (a == "-n" || a == "--max" || a == "--limit") && i+1 < len(args):
			maxMessages, _ = strconv.Atoi(args[i+1])
			i++
		case a == "--json":
			jsonOutput = true
		default:
			if !strings.HasPrefix(a, "-") && target == "" {
				target = a
			}
		}
	}

	if target == "" {
		return zone.NewZoneError("usage: hit ws <URL> [-H k:v] [-m MSG]... [--expect PATTERN] [-d TIMEOUT] [-n MAX] [--json]")
	}

	headers := make(map[string]string)
	for _, h := range headersList {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) == 2 {
			headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	onMessage := func(msg stream.WSMessage) {
		if jsonOutput {
			return
		}
		if msg.Direction == "sent" {
			p.Out(fmt.Sprintf("  %s %s", p.Cyan("→ SENT:"), msg.Data))
		} else {
			p.Out(fmt.Sprintf("  %s %s %s", p.Green("← RECV:"), msg.Data, p.Dim(fmt.Sprintf("(+%v)", msg.Timestamp))))
		}
	}

	ctx := context.Background()
	if !jsonOutput {
		p.Out(fmt.Sprintf("%s %s", p.Bold("🔌 Connecting to WebSocket:"), p.Cyan(target)))
	}

	res, err := stream.RunWS(ctx, target, stream.WSOptions{
		Headers:      headers,
		Insecure:     globals.Insecure,
		Timeout:      timeout,
		SendMessages: sendMessages,
		Expect:       expect,
		MaxMessages:  maxMessages,
		OnMessage:    onMessage,
	})
	if err != nil && (res == nil || !res.Connected) {
		return fmt.Errorf("websocket connection failed: %w", err)
	}

	if jsonOutput {
		b, mErr := json.MarshalIndent(res, "", "  ")
		if mErr != nil {
			return mErr
		}
		p.Out(string(b))
		return nil
	}

	p.Out("")
	p.Out(p.Dim(strings.Repeat("─", 50)))
	p.Out(p.Bold("WebSocket Summary:"))
	p.Out(fmt.Sprintf("  Handshake Latency: %v", res.HandshakeLatency))
	p.Out(fmt.Sprintf("  Messages Sent:     %d", res.MessagesSent))
	p.Out(fmt.Sprintf("  Messages Received: %d", res.MessagesReceived))
	p.Out(fmt.Sprintf("  Session Duration:  %v", res.TotalDuration))

	if expect != "" {
		if res.MatchedExpect {
			p.Out(fmt.Sprintf("  Expectation (%q): %s", expect, p.Green("✓ PASS")))
		} else {
			p.Out(fmt.Sprintf("  Expectation (%q): %s", expect, p.Red("✗ FAIL (not matched)")))
			return fmt.Errorf("websocket expectation failed: %q was not received", expect)
		}
	}

	return nil
}

func cmdSchedule(args []string, globals *GlobalFlags, p *output.Printer) error {
	var (
		target        string
		method        string
		headersList   []string
		bodyRaw       string
		jsonBodyRaw   string
		server        string
		atStr         string
		everyStr      string
		count         int
		durationStr   string
		expectStatus  int
		expectPattern string
		jsonOutput    bool
		quiet         bool
	)

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "--at" || a == "--start") && i+1 < len(args):
			atStr = args[i+1]
			i++
		case (a == "--every" || a == "--interval" || a == "-i") && i+1 < len(args):
			everyStr = args[i+1]
			i++
		case (a == "-n" || a == "--count") && i+1 < len(args):
			count, _ = strconv.Atoi(args[i+1])
			i++
		case (a == "-d" || a == "--duration") && i+1 < len(args):
			durationStr = args[i+1]
			i++
		case a == "--status" && i+1 < len(args):
			expectStatus, _ = strconv.Atoi(args[i+1])
			i++
		case a == "--expect" && i+1 < len(args):
			expectPattern = args[i+1]
			i++
		case (a == "-m" || a == "--method") && i+1 < len(args):
			method = strings.ToUpper(args[i+1])
			i++
		case (a == "-H" || a == "--header") && i+1 < len(args):
			headersList = append(headersList, args[i+1])
			i++
		case (a == "-b" || a == "--body") && i+1 < len(args):
			bodyRaw = args[i+1]
			i++
		case (a == "-j" || a == "--json-body") && i+1 < len(args):
			jsonBodyRaw = args[i+1]
			i++
		case (a == "-s" || a == "--server" || a == "-e" || a == "--env") && i+1 < len(args):
			server = args[i+1]
			i++
		case a == "--json":
			jsonOutput = true
		case a == "-q" || a == "--quiet":
			quiet = true
		default:
			if !strings.HasPrefix(a, "-") && target == "" {
				target = a
			}
		}
	}

	if target == "" {
		return zone.NewZoneError("usage: hit schedule <URL|REF|SHORTHAND> [--at TIME] [--every INTERVAL] [--count N] [--status CODE] [--expect PATTERN]")
	}

	zoneRoot := globals.Zone
	if zoneRoot == "" {
		if z, _ := zone.FindOrNone(""); z != nil {
			zoneRoot = z.Root
		}
	}

	headersMap := make(map[string]string)
	for _, h := range headersList {
		if parts := strings.SplitN(h, ":", 2); len(parts) == 2 {
			headersMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	var reqBody string
	if jsonBodyRaw != "" {
		txt, err := readArg(jsonBodyRaw)
		if err != nil {
			return err
		}
		reqBody = txt
		if _, ok := headersMap["Content-Type"]; !ok {
			headersMap["Content-Type"] = "application/json"
		}
	} else if bodyRaw != "" {
		txt, err := readArg(bodyRaw)
		if err != nil {
			return err
		}
		reqBody = txt
	}

	// Resolve shorthand if target matches a shorthand
	if sh, ok := shorthand.Get(zoneRoot, target); ok {
		if sh.URL != "" {
			target = sh.URL
			if method == "" && sh.Method != "" {
				method = sh.Method
			}
		} else if sh.Ref != "" {
			target = sh.Ref
		}
		if server == "" && sh.Server != "" {
			server = sh.Server
		}
		for k, v := range sh.Headers {
			if _, exists := headersMap[k]; !exists {
				headersMap[k] = v
			}
		}
		if reqBody == "" && sh.Body != "" {
			reqBody = sh.Body
		}
	}

	if server == "" {
		server = globals.Server
	}

	startTime := time.Now()
	if atStr != "" {
		t, err := schedule.ParseStartTime(atStr, time.Now())
		if err != nil {
			return err
		}
		startTime = t
	}

	var interval time.Duration
	if everyStr != "" {
		d, err := time.ParseDuration(everyStr)
		if err != nil {
			if sec, sErr := strconv.ParseFloat(everyStr, 64); sErr == nil {
				d = time.Duration(sec * float64(time.Second))
			} else {
				return fmt.Errorf("invalid interval %q: %w", everyStr, err)
			}
		}
		interval = d
	}

	var totalDuration time.Duration
	if durationStr != "" {
		d, err := time.ParseDuration(durationStr)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", durationStr, err)
		}
		totalDuration = d
	}

	ctx := context.Background()

	opts := schedule.ScheduleOptions{
		Target:        target,
		Method:        method,
		Headers:       headersMap,
		Body:          reqBody,
		Server:        server,
		StartTime:     startTime,
		Interval:      interval,
		Count:         count,
		Duration:      totalDuration,
		ExpectStatus:  expectStatus,
		ExpectPattern: expectPattern,
		Insecure:      globals.Insecure,
		ZoneRoot:      zoneRoot,
	}

	if !jsonOutput && !quiet {
		if startTime.After(time.Now()) {
			p.Out(fmt.Sprintf("%s Target %s scheduled for %s (in %v)",
				p.Cyan("⏰"), p.Bold(target), startTime.Format("15:04:05"), time.Until(startTime).Round(time.Second)))
		} else {
			p.Out(fmt.Sprintf("%s Scheduling executions for %s", p.Cyan("⏰"), p.Bold(target)))
		}
		if interval > 0 {
			countStr := "infinite"
			if count > 0 {
				countStr = strconv.Itoa(count)
			}
			p.Out(fmt.Sprintf("   Interval: %v | Max Runs: %s", interval, countStr))
		}
		if expectStatus > 0 || expectPattern != "" {
			expectMsg := "   Expected:"
			if expectStatus > 0 {
				expectMsg += fmt.Sprintf(" status=%d", expectStatus)
			}
			if expectPattern != "" {
				expectMsg += fmt.Sprintf(" pattern=%q", expectPattern)
			}
			p.Out(p.Dim(expectMsg))
		}
		p.Out("")
	}

	opts.OnTick = func(tick schedule.ScheduleTick) {
		if jsonOutput {
			return
		}
		statusColor := p.Green(strconv.Itoa(tick.StatusCode))
		if tick.StatusCode >= 400 {
			statusColor = p.Red(strconv.Itoa(tick.StatusCode))
		} else if tick.StatusCode >= 300 {
			statusColor = p.Yellow(strconv.Itoa(tick.StatusCode))
		}

		statusBadge := p.Green("✔ PASS")
		if !tick.Passed {
			statusBadge = p.Red("✘ FAIL")
		}

		timeStr := tick.Timestamp.Format("15:04:05")
		tickCountStr := fmt.Sprintf("[%d]", tick.Index)
		if count > 0 {
			tickCountStr = fmt.Sprintf("[%d/%d]", tick.Index, count)
		}

		detailStr := ""
		if tick.Detail != "" {
			detailStr = fmt.Sprintf(" - %s", tick.Detail)
		}

		p.Out(fmt.Sprintf("%s %s  %s  %s (%.1fms)  %s%s",
			p.Dim(tickCountStr), p.Dim(timeStr), p.Bold(target), statusColor, tick.ElapsedMs, statusBadge, p.Dim(detailStr)))
	}

	summary, err := schedule.Run(ctx, opts)
	if err != nil && err != context.Canceled {
		return err
	}

	if jsonOutput {
		b, _ := json.MarshalIndent(summary, "", "  ")
		p.Out(string(b))
		if summary.FailedRuns > 0 {
			return fmt.Errorf("%d of %d scheduled runs failed expectation", summary.FailedRuns, summary.TotalRuns)
		}
		return nil
	}

	if !quiet {
		p.Out("")
		p.Out(p.Dim(strings.Repeat("─", 60)))
		passRate := 0.0
		if summary.TotalRuns > 0 {
			passRate = (float64(summary.PassedRuns) / float64(summary.TotalRuns)) * 100.0
		}
		p.Out(fmt.Sprintf("%s %d runs | %s passed | %s failed (%.0f%% success rate)",
			p.Bold("Schedule Summary:"),
			summary.TotalRuns,
			p.Green(strconv.Itoa(summary.PassedRuns)),
			p.Red(strconv.Itoa(summary.FailedRuns)),
			passRate,
		))
	}

	if summary.FailedRuns > 0 {
		return fmt.Errorf("%d of %d scheduled runs failed expectation", summary.FailedRuns, summary.TotalRuns)
	}
	return nil
}

func cmdProbe(args []string, globals *GlobalFlags, p *output.Printer) error {
	if len(args) == 0 {
		printProbeHelp(p)
		return nil
	}

	action := args[0]
	switch action {
	case "run":
		return cmdProbeRun(args[1:], globals, p)
	case "daemon", "watch":
		return cmdProbeDaemon(args[1:], globals, p)
	case "test-alert", "test":
		return cmdProbeTestAlert(args[1:], globals, p)
	case "-h", "--help", "help":
		printProbeHelp(p)
		return nil
	default:
		return cmdProbeRun(args, globals, p)
	}
}

func printProbeHelp(p *output.Printer) {
	p.Out(`Usage: hit probe <subcommand> [flags]

Git-Native Synthetic API Monitoring & Multi-Region Incident Alerting

Subcommands:
  run <file|ref>        Run immediate multi-region consensus check
  daemon <file>         Run continuous monitoring daemon with automated incident alerting
  test-alert <file>     Send test alert to configured channels (PagerDuty, Slack, Opsgenie)

Flags:
  --regions LIST        Comma-separated regions (default: us-east,eu-central,ap-southeast)
  --consensus INT       Consensus threshold required to declare an incident (default: 2)
  --sla-latency DURATION Maximum allowed latency SLA (e.g. 500ms, 1s)
  --interval DURATION   Polling interval for daemon mode (default: 60s)
  -n, --count INT       Number of iterations to run in daemon mode (default: infinite)
  --channel NAME        Channel to test in test-alert (all, slack, pagerduty, opsgenie)
  --json                Output results as JSON
  -q, --quiet           Suppress non-essential output
  -v, --verbose         Verbose output`)
}

func cmdProbeRun(args []string, globals *GlobalFlags, p *output.Printer) error {
	target := ""
	jsonOutput := false
	quiet := false
	regionsStr := ""
	consensusVal := 0
	slaLatencyStr := ""

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
			jsonOutput = true
		case a == "--quiet" || a == "-q":
			quiet = true
		case a == "--regions" && i+1 < len(args):
			regionsStr = args[i+1]
			i++
		case strings.HasPrefix(a, "--regions="):
			regionsStr = strings.TrimPrefix(a, "--regions=")
		case a == "--consensus" && i+1 < len(args):
			consensusVal, _ = strconv.Atoi(args[i+1])
			i++
		case strings.HasPrefix(a, "--consensus="):
			consensusVal, _ = strconv.Atoi(strings.TrimPrefix(a, "--consensus="))
		case a == "--sla-latency" && i+1 < len(args):
			slaLatencyStr = args[i+1]
			i++
		case strings.HasPrefix(a, "--sla-latency="):
			slaLatencyStr = strings.TrimPrefix(a, "--sla-latency=")
		default:
			if !strings.HasPrefix(a, "-") && target == "" {
				target = a
			}
		}
	}

	if target == "" {
		return fmt.Errorf("usage: hit probe run <probe.yaml | ref> [flags]")
	}

	var cfg *probe.ProbeConfig
	if strings.HasSuffix(target, ".yaml") || strings.HasSuffix(target, ".yml") {
		if c, err := probe.LoadProbeConfig(target); err == nil && c.Ref != "" {
			cfg = c
		}
	}

	if cfg == nil {
		cfg = &probe.ProbeConfig{
			Name:               filepath.Base(target),
			Ref:                target,
			Regions:            probe.DefaultRegions(),
			ConsensusThreshold: 2,
		}
	}

	if regionsStr != "" {
		parts := strings.Split(regionsStr, ",")
		var reg []string
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				reg = append(reg, trimmed)
			}
		}
		if len(reg) > 0 {
			cfg.Regions = reg
		}
	}
	if consensusVal > 0 {
		cfg.ConsensusThreshold = consensusVal
	}
	if slaLatencyStr != "" {
		if d, err := time.ParseDuration(slaLatencyStr); err == nil {
			cfg.SLA.MaxLatency = d
		} else if ms, err := strconv.Atoi(slaLatencyStr); err == nil {
			cfg.SLA.MaxLatency = time.Duration(ms) * time.Millisecond
		}
	}

	z, _ := zone.FindOrNone(globals.Zone)
	if z == nil && target != "" {
		if foundZ, _ := zone.Find(filepath.Dir(target)); foundZ != nil {
			z = foundZ
		}
	}

	sess, err := runner.NewSession(runner.SessionOptions{
		Zone:         z,
		ServerName:   globals.Server,
		ExtraVars:    globals.Vars,
		Persist:      false,
		ZoneOptional: true,
		Verify:       !globals.Insecure,
		NoHistory:    true,
	})
	if err != nil {
		return err
	}
	defer sess.Close()

	if !quiet && !jsonOutput {
		p.Out(fmt.Sprintf("%s Synthetic API Probe: %s (%s)",
			p.Cyan("🎯"), p.Bold(cfg.Name), p.Dim(cfg.Ref)))
		p.Out(fmt.Sprintf("   Regions: %s | Consensus Threshold: %d/%d",
			strings.Join(cfg.Regions, ", "), cfg.ConsensusThreshold, len(cfg.Regions)))
		if cfg.SLA.MaxLatency > 0 {
			p.Out(fmt.Sprintf("   SLA Latency Ceiling: %v", cfg.SLA.MaxLatency))
		}
		p.Out("")
	}

	result := probe.ExecuteProbeCheck(sess, cfg)

	if jsonOutput {
		b, _ := json.MarshalIndent(result, "", "  ")
		p.Out(string(b))
		if result.Status == probe.StatusIncident {
			return fmt.Errorf("probe check failed consensus: %s", result.Summary)
		}
		return nil
	}

	for _, c := range result.Checks {
		statusStr := p.Green(fmt.Sprintf("HTTP %d", c.StatusCode))
		if c.StatusCode >= 400 || c.StatusCode == 0 {
			statusStr = p.Red(fmt.Sprintf("HTTP %d", c.StatusCode))
		} else if c.StatusCode >= 300 {
			statusStr = p.Yellow(fmt.Sprintf("HTTP %d", c.StatusCode))
		}

		icon := p.Green("✔")
		if !c.Passed {
			icon = p.Red("✘")
		}

		errDetail := ""
		if !c.Passed {
			if len(c.FailedAssertions) > 0 {
				errDetail = " - " + p.Red(strings.Join(c.FailedAssertions, "; "))
			} else if c.Error != "" {
				errDetail = " - " + p.Red(c.Error)
			}
		}

		p.Out(fmt.Sprintf("   %s [%-12s] %s (%6.1fms)%s",
			icon, c.Region, statusStr, c.ElapsedMs, errDetail))
	}

	p.Out("")
	switch result.Status {
	case probe.StatusHealthy:
		p.Out(fmt.Sprintf("Status: %s - %s", p.Green("HEALTHY"), result.Summary))
	case probe.StatusDegraded:
		p.Out(fmt.Sprintf("Status: %s - %s", p.Yellow("DEGRADED"), result.Summary))
	case probe.StatusIncident:
		p.Out(fmt.Sprintf("Status: %s - %s", p.Red("INCIDENT"), result.Summary))
	}

	if result.Status == probe.StatusIncident {
		return fmt.Errorf("synthetic probe failed: %s", result.Summary)
	}
	return nil
}

func cmdProbeDaemon(args []string, globals *GlobalFlags, p *output.Printer) error {
	target := ""
	intervalStr := ""
	count := 0
	jsonOutput := false
	quiet := false
	hubURL := os.Getenv("HIT_HUB_URL")
	hubToken := os.Getenv("HIT_HUB_TOKEN")
	region := "local"
	nodeID := ""
	owner := os.Getenv("USER")
	if owner == "" {
		owner = "probe-daemon"
	}

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
			jsonOutput = true
		case a == "--quiet" || a == "-q":
			quiet = true
		case a == "--interval" && i+1 < len(args):
			intervalStr = args[i+1]
			i++
		case strings.HasPrefix(a, "--interval="):
			intervalStr = strings.TrimPrefix(a, "--interval=")
		case (a == "-n" || a == "--count") && i+1 < len(args):
			count, _ = strconv.Atoi(args[i+1])
			i++
		case strings.HasPrefix(a, "--count="):
			count, _ = strconv.Atoi(strings.TrimPrefix(a, "--count="))
		case a == "--hub" && i+1 < len(args):
			hubURL = args[i+1]
			i++
		case strings.HasPrefix(a, "--hub="):
			hubURL = strings.TrimPrefix(a, "--hub=")
		case a == "--hub-token" && i+1 < len(args):
			hubToken = args[i+1]
			i++
		case strings.HasPrefix(a, "--hub-token="):
			hubToken = strings.TrimPrefix(a, "--hub-token=")
		case a == "--region" && i+1 < len(args):
			region = args[i+1]
			i++
		case strings.HasPrefix(a, "--region="):
			region = strings.TrimPrefix(a, "--region=")
		case a == "--node-id" && i+1 < len(args):
			nodeID = args[i+1]
			i++
		case strings.HasPrefix(a, "--node-id="):
			nodeID = strings.TrimPrefix(a, "--node-id=")
		case a == "--owner" && i+1 < len(args):
			owner = args[i+1]
			i++
		case strings.HasPrefix(a, "--owner="):
			owner = strings.TrimPrefix(a, "--owner=")
		default:
			if !strings.HasPrefix(a, "-") && target == "" {
				target = a
			}
		}
	}

	if target == "" {
		return fmt.Errorf("usage: hit probe daemon <probe.yaml> [--interval 30s] [-n count] [--hub URL]")
	}

	cfg, err := probe.LoadProbeConfig(target)
	if err != nil {
		return err
	}

	if intervalStr != "" {
		if d, err := time.ParseDuration(intervalStr); err == nil {
			cfg.Interval = d
		} else if sec, err := strconv.Atoi(intervalStr); err == nil {
			cfg.Interval = time.Duration(sec) * time.Second
		}
	}

	z, _ := zone.FindOrNone(globals.Zone)
	if z == nil && target != "" {
		if foundZ, _ := zone.Find(filepath.Dir(target)); foundZ != nil {
			z = foundZ
		}
	}

	sess, err := runner.NewSession(runner.SessionOptions{
		Zone:         z,
		ServerName:   globals.Server,
		ExtraVars:    globals.Vars,
		Persist:      false,
		ZoneOptional: true,
		Verify:       !globals.Insecure,
		NoHistory:    true,
	})
	if err != nil {
		return err
	}
	defer sess.Close()

	httpClient := &http.Client{Timeout: 5 * time.Second}
	state := &probe.ProbeState{CurrentStatus: probe.StatusHealthy}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if hubURL != "" {
		host, _ := os.Hostname()
		cleanName := strings.ReplaceAll(strings.ToLower(cfg.Name), " ", "-")
		if nodeID == "" {
			nodeID = fmt.Sprintf("probe-%s-%d", cleanName, time.Now().UnixNano()%10000)
		}
		regPayload := map[string]any{
			"id":           nodeID,
			"common_name":  fmt.Sprintf("probe-%s", cleanName),
			"owner":        owner,
			"type":         "probe-daemon",
			"region":       region,
			"capacity":     len(cfg.Regions),
			"capabilities": []string{"probe-runner", "synthetic-monitoring"},
			"hostname":     host,
			"os":           runtime.GOOS,
		}
		regBytes, _ := json.Marshal(regPayload)
		regReq, err := http.NewRequest(http.MethodPost, strings.TrimRight(hubURL, "/")+"/api/v1/nodes/register", bytes.NewReader(regBytes))
		if err == nil {
			regReq.Header.Set("Content-Type", "application/json")
			if hubToken != "" {
				regReq.Header.Set("Authorization", "Bearer "+hubToken)
			}
			if resp, err := httpClient.Do(regReq); err == nil {
				resp.Body.Close()
			}
		}
	}

	if !quiet && !jsonOutput {
		p.Out(fmt.Sprintf("%s Starting synthetic probe daemon: %s", p.Cyan("📡"), p.Bold(cfg.Name)))
		p.Out(fmt.Sprintf("   Target: %s | Interval: %v | Regions: %v", cfg.Ref, cfg.Interval, cfg.Regions))
		p.Out(fmt.Sprintf("   Consensus Threshold: %d/%d failures", cfg.ConsensusThreshold, len(cfg.Regions)))
		if hubURL != "" {
			p.Out(fmt.Sprintf("   Fleet Enrollment: Enrolled in Hub %s (node: %s)", p.Cyan(hubURL), p.Bold(nodeID)))
		}
		var channels []string
		if cfg.Alerts.PagerDuty != nil {
			channels = append(channels, "PagerDuty")
		}
		if cfg.Alerts.Slack != nil {
			channels = append(channels, "Slack")
		}
		if cfg.Alerts.Opsgenie != nil {
			channels = append(channels, "Opsgenie")
		}
		if len(channels) > 0 {
			p.Out(fmt.Sprintf("   Alert Channels: %s", strings.Join(channels, ", ")))
		}
		p.Out(p.Dim("   Press Ctrl+C to terminate probe daemon."))
		p.Out("")
	}

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	iteration := 0
	runCheck := func() bool {
		iteration++
		res := probe.ExecuteProbeCheck(sess, cfg)
		evt, _ := probe.HandleStateTransition(state, res, cfg, httpClient)

		if hubURL != "" {
			hbState := "idle"
			if res.Status == probe.StatusIncident {
				hbState = "degraded"
			}
			hbPayload := map[string]any{
				"state":       hbState,
				"active_jobs": 1,
				"metrics": map[string]any{
					"status":         string(res.Status),
					"passed_regions": res.PassedRegions,
					"total_regions":  res.TotalRegions,
					"iteration":      iteration,
				},
			}
			hbBytes, _ := json.Marshal(hbPayload)
			hbReq, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/nodes/%s/heartbeat", strings.TrimRight(hubURL, "/"), nodeID), bytes.NewReader(hbBytes))
			if err == nil {
				hbReq.Header.Set("Content-Type", "application/json")
				if hubToken != "" {
					hbReq.Header.Set("Authorization", "Bearer "+hubToken)
				}
				if resp, err := httpClient.Do(hbReq); err == nil {
					resp.Body.Close()
				}
			}
		}

		if jsonOutput {
			payload := map[string]any{
				"iteration": iteration,
				"result":    res,
				"event":     evt,
				"state":     state,
			}
			b, _ := json.Marshal(payload)
			p.Out(string(b))
		} else if !quiet {
			timeStr := res.Timestamp.Format("15:04:05")
			statusBadge := p.Green("HEALTHY")
			if res.Status == probe.StatusDegraded {
				statusBadge = p.Yellow("DEGRADED")
			} else if res.Status == probe.StatusIncident {
				statusBadge = p.Red("INCIDENT")
			}

			iterStr := fmt.Sprintf("[%d]", iteration)
			if count > 0 {
				iterStr = fmt.Sprintf("[%d/%d]", iteration, count)
			}

			p.Out(fmt.Sprintf("%s %s  %-8s  %d/%d regions passed  (%s)",
				p.Dim(iterStr), p.Dim(timeStr), statusBadge, res.PassedRegions, res.TotalRegions, p.Dim(res.Summary)))

			if evt.Action == "TRIGGER" {
				p.Out(p.Red(fmt.Sprintf("   🚨 [INCIDENT TRIGGERED] %s", evt.Summary)))
				if len(evt.Notified) > 0 {
					p.Out(p.Cyan(fmt.Sprintf("      → Alerts dispatched to: %s", strings.Join(evt.Notified, ", "))))
				}
				if len(evt.AlertErrors) > 0 {
					p.Out(p.Red(fmt.Sprintf("      ✘ Alert failures: %s", strings.Join(evt.AlertErrors, "; "))))
				}
			} else if evt.Action == "RESOLVE" {
				p.Out(p.Green(fmt.Sprintf("   ✅ [INCIDENT RESOLVED] %s", evt.Summary)))
				if len(evt.Notified) > 0 {
					p.Out(p.Cyan(fmt.Sprintf("      → Resolution dispatched to: %s", strings.Join(evt.Notified, ", "))))
				}
				if len(evt.AlertErrors) > 0 {
					p.Out(p.Red(fmt.Sprintf("      ✘ Resolution alert failures: %s", strings.Join(evt.AlertErrors, "; "))))
				}
			}
		}

		if count > 0 && iteration >= count {
			return false
		}
		return true
	}

	if !runCheck() {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			if hubURL != "" {
				deregReq, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/nodes/%s/deregister", strings.TrimRight(hubURL, "/"), nodeID), nil)
				if err == nil {
					if hubToken != "" {
						deregReq.Header.Set("Authorization", "Bearer "+hubToken)
					}
					if resp, err := httpClient.Do(deregReq); err == nil {
						resp.Body.Close()
					}
				}
			}
			if !quiet && !jsonOutput {
				p.Out("")
				p.Out(p.Dim("Daemon stopped gracefully."))
			}
			return nil
		case <-ticker.C:
			if !runCheck() {
				return nil
			}
		}
	}
}

func cmdProbeTestAlert(args []string, globals *GlobalFlags, p *output.Printer) error {
	target := ""
	channel := "all"

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--channel" && i+1 < len(args):
			channel = strings.ToLower(args[i+1])
			i++
		case strings.HasPrefix(a, "--channel="):
			channel = strings.ToLower(strings.TrimPrefix(a, "--channel="))
		default:
			if !strings.HasPrefix(a, "-") && target == "" {
				target = a
			}
		}
	}

	if target == "" {
		return fmt.Errorf("usage: hit probe test-alert <probe.yaml> [--channel all|slack|pagerduty|opsgenie]")
	}

	cfg, err := probe.LoadProbeConfig(target)
	if err != nil {
		return err
	}

	p.Out(fmt.Sprintf("%s Testing incident alert dispatchers for probe %s...", p.Cyan("🔔"), p.Bold(cfg.Name)))
	tested := 0
	httpClient := &http.Client{Timeout: 5 * time.Second}

	if (channel == "all" || channel == "pagerduty") && cfg.Alerts.PagerDuty != nil && cfg.Alerts.PagerDuty.RoutingKey != "" {
		tested++
		routingKey := probe.ResolveSecret(cfg.Alerts.PagerDuty.RoutingKey)
		if routingKey == "" {
			p.Out(p.Red(fmt.Sprintf("✘ PagerDuty: Routing key unresolved (%s)", cfg.Alerts.PagerDuty.RoutingKey)))
		} else {
			customDetails := map[string]any{
				"test":       true,
				"probe_name": cfg.Name,
				"source":     "hit probe test-alert",
			}
			err := probe.SendPagerDuty(httpClient, cfg.Alerts.PagerDuty.Endpoint, cfg.Alerts.PagerDuty.RoutingKey, "trigger", "hit-test-alert", "Test Alert: synthetic probe verification from hit CLI", "hit-probe", "warning", customDetails)
			if err != nil {
				p.Out(p.Red(fmt.Sprintf("✘ PagerDuty: failed to dispatch: %v", err)))
			} else {
				p.Out(p.Green("✔ PagerDuty: Events API v2 test event delivered successfully"))
			}
		}
	}

	if (channel == "all" || channel == "slack") && cfg.Alerts.Slack != nil && cfg.Alerts.Slack.WebhookURL != "" {
		tested++
		webhookURL := probe.ResolveSecret(cfg.Alerts.Slack.WebhookURL)
		if webhookURL == "" {
			p.Out(p.Red(fmt.Sprintf("✘ Slack: Webhook URL unresolved (%s)", cfg.Alerts.Slack.WebhookURL)))
		} else {
			details := []string{
				"• Test verification message sent via `hit probe test-alert`.",
				"• Block Kit formatting and connectivity verified.",
			}
			err := probe.SendSlack(httpClient, cfg.Alerts.Slack.WebhookURL, fmt.Sprintf("Synthetic Probe Test: %s", cfg.Name), true, details)
			if err != nil {
				p.Out(p.Red(fmt.Sprintf("✘ Slack: failed to dispatch: %v", err)))
			} else {
				p.Out(p.Green("✔ Slack: Block Kit notification card delivered successfully"))
			}
		}
	}

	if (channel == "all" || channel == "opsgenie") && cfg.Alerts.Opsgenie != nil && cfg.Alerts.Opsgenie.APIKey != "" {
		tested++
		apiKey := probe.ResolveSecret(cfg.Alerts.Opsgenie.APIKey)
		if apiKey == "" {
			p.Out(p.Red(fmt.Sprintf("✘ Opsgenie: API key unresolved (%s)", cfg.Alerts.Opsgenie.APIKey)))
		} else {
			err := probe.SendOpsgenie(httpClient, cfg.Alerts.Opsgenie.Endpoint, cfg.Alerts.Opsgenie.APIKey, "create", "hit-test-alert", fmt.Sprintf("Test Alert: %s synthetic probe verification", cfg.Name), "Test verification message sent via hit probe test-alert", "P3")
			if err != nil {
				p.Out(p.Red(fmt.Sprintf("✘ Opsgenie: failed to dispatch: %v", err)))
			} else {
				p.Out(p.Green("✔ Opsgenie: Alerts API v2 test alert delivered successfully"))
			}
		}
	}

	if tested == 0 {
		p.Out(p.Yellow("⚠ No alert channels matched or configured in probe specification."))
		p.Out("  Add 'alerts:' section with 'pagerduty:', 'slack:', or 'opsgenie:' in the probe YAML.")
	}

	return nil
}

func cmdShorthand(args []string, globals *GlobalFlags, p *output.Printer) error {
	zoneRoot := globals.Zone
	if zoneRoot == "" {
		if z, _ := zone.FindOrNone(""); z != nil {
			zoneRoot = z.Root
		}
	}

	action := "ls"
	var actionArgs []string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "ls", "list", "set", "add", "rm", "remove", "delete", "get", "show":
			action = args[0]
			actionArgs = args[1:]
		default:
			action = "get"
			actionArgs = args
		}
	} else if len(args) > 0 {
		actionArgs = args
	}

	jsonOutput := false
	for _, a := range args {
		if a == "--json" {
			jsonOutput = true
		}
	}

	switch action {
	case "ls", "list":
		shs, err := shorthand.List(zoneRoot)
		if err != nil {
			return err
		}
		if jsonOutput {
			b, _ := json.MarshalIndent(shs, "", "  ")
			p.Out(string(b))
			return nil
		}
		if len(shs) == 0 {
			p.Out("No shorthands defined.")
			p.Out("Create one with:")
			p.Out("  hit shorthand set <name> <url|ref> [-m METHOD] [-s SERVER]")
			return nil
		}
		p.Out(p.Bold("Defined Shorthands:"))
		for _, s := range shs {
			srv := ""
			if s.Server != "" {
				srv = fmt.Sprintf(" [%s]", p.Cyan(s.Server))
			}
			desc := ""
			if s.Description != "" {
				desc = fmt.Sprintf(" - %s", p.Dim(s.Description))
			}
			p.Out(fmt.Sprintf("  %s %s %s%s%s",
				p.Bold(fmt.Sprintf("%-16s", s.Name)),
				p.Yellow(fmt.Sprintf("%-6s", s.MethodDisplay())),
				s.TargetDisplay(),
				srv,
				desc,
			))
		}
		return nil

	case "get", "show":
		if len(actionArgs) == 0 {
			return zone.NewZoneError("usage: hit shorthand get <NAME>")
		}
		name := actionArgs[0]
		sh, ok := shorthand.Get(zoneRoot, name)
		if !ok {
			return zone.NewZoneError("shorthand '%s' not found", name)
		}
		if jsonOutput {
			b, _ := json.MarshalIndent(sh, "", "  ")
			p.Out(string(b))
			return nil
		}
		out, err := yaml.Marshal(sh)
		if err != nil {
			return err
		}
		p.Out(string(out))
		return nil

	case "set", "add":
		var (
			name        string
			target      string
			method      string
			server      string
			description string
			headersList []string
			queryList   []string
			body        string
			isGlobal    bool
		)

		var pos []string
		for i := 0; i < len(actionArgs); i++ {
			a := actionArgs[i]
			switch {
			case (a == "-m" || a == "--method") && i+1 < len(actionArgs):
				method = strings.ToUpper(actionArgs[i+1])
				i++
			case (a == "-s" || a == "--server" || a == "-e" || a == "--env") && i+1 < len(actionArgs):
				server = actionArgs[i+1]
				i++
			case (a == "-d" || a == "--description" || a == "--desc") && i+1 < len(actionArgs):
				description = actionArgs[i+1]
				i++
			case (a == "-H" || a == "--header") && i+1 < len(actionArgs):
				headersList = append(headersList, actionArgs[i+1])
				i++
			case (a == "-q" || a == "--query") && i+1 < len(actionArgs):
				queryList = append(queryList, actionArgs[i+1])
				i++
			case (a == "-b" || a == "--body") && i+1 < len(actionArgs):
				body = actionArgs[i+1]
				i++
			case a == "--global" || a == "-g":
				isGlobal = true
			default:
				if !strings.HasPrefix(a, "-") {
					pos = append(pos, a)
				}
			}
		}

		if len(pos) < 2 {
			return zone.NewZoneError("usage: hit shorthand set <NAME> <URL|REF> [-m METHOD] [-s SERVER] [-H k:v] [--global]")
		}
		name = pos[0]
		target = pos[1]

		headersMap := make(map[string]string)
		for _, h := range headersList {
			if parts := strings.SplitN(h, ":", 2); len(parts) == 2 {
				headersMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}

		queryMap := make(map[string]string)
		for _, q := range queryList {
			if parts := strings.SplitN(q, "=", 2); len(parts) == 2 {
				queryMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}

		sh := &shorthand.Shorthand{
			Name:        name,
			Description: description,
			Server:      server,
			Body:        body,
			Headers:     headersMap,
			Query:       queryMap,
		}
		if isURLOrPath(target) || strings.Contains(target, "://") {
			sh.URL = target
			sh.Method = method
			if sh.Method == "" {
				sh.Method = "GET"
			}
		} else {
			sh.Ref = target
			if method != "" {
				sh.Method = method
			}
		}

		saveRoot := zoneRoot
		if isGlobal {
			saveRoot = ""
		}
		if err := shorthand.Save(saveRoot, sh); err != nil {
			return fmt.Errorf("failed to save shorthand: %w", err)
		}
		p.Out(fmt.Sprintf("%s Shorthand '%s' saved successfully!", p.Green("✔"), name))
		p.Out(fmt.Sprintf("  Try running: hit %s", name))
		return nil

	case "rm", "remove", "delete":
		isGlobal := false
		var name string
		for _, a := range actionArgs {
			if a == "--global" || a == "-g" {
				isGlobal = true
			} else if !strings.HasPrefix(a, "-") && name == "" {
				name = a
			}
		}
		if name == "" {
			return zone.NewZoneError("usage: hit shorthand rm <NAME> [--global]")
		}
		delRoot := zoneRoot
		if isGlobal {
			delRoot = ""
		}
		if err := shorthand.Delete(delRoot, name); err != nil {
			return err
		}
		p.Out(fmt.Sprintf("%s Shorthand '%s' removed.", p.Green("✔"), name))
		return nil

	default:
		return zone.NewZoneError("unknown shorthand command '%s'", action)
	}
}

func cmdWizard(args []string, globals *GlobalFlags, p *output.Printer) error {
	dir := "."
	name := ""
	baseURL := ""
	server := "local"
	autoYes := false

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "--name" || a == "-n") && i+1 < len(args):
			name = args[i+1]
			i++
		case strings.HasPrefix(a, "--name="):
			name = strings.TrimPrefix(a, "--name=")
		case (a == "--url" || a == "-u") && i+1 < len(args):
			baseURL = args[i+1]
			i++
		case strings.HasPrefix(a, "--url="):
			baseURL = strings.TrimPrefix(a, "--url=")
		case (a == "--server" || a == "-s") && i+1 < len(args):
			server = args[i+1]
			i++
		case strings.HasPrefix(a, "--server="):
			server = strings.TrimPrefix(a, "--server=")
		case a == "-y" || a == "--yes":
			autoYes = true
		case a == "--wizard" || a == "-w" || a == "wizard":
			// skip wizard flag keyword
		case !strings.HasPrefix(a, "-"):
			dir = a
		}
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	isInteractive := false
	if fi, err := os.Stdin.Stat(); err == nil {
		isInteractive = (fi.Mode() & os.ModeCharDevice) != 0
	}

	if !autoYes && isInteractive {
		p.Out(p.Bold("🧙 Hit Endpoint Zone Wizard"))
		p.Out(p.Dim("Scaffolds boilerplate folders and files for a new zone.\nPress Enter to accept default values in brackets.\n"))

		defaultName := name
		if defaultName == "" {
			defaultName = filepath.Base(absDir)
			if defaultName == "." || defaultName == "/" || defaultName == "" {
				defaultName = "my-api"
			}
		}
		fmt.Printf("Zone name [%s]: ", defaultName)
		reader := bufio.NewReader(os.Stdin)
		if line, err := reader.ReadString('\n'); err == nil {
			line = strings.TrimSpace(line)
			if line != "" {
				name = line
			}
		}
		if name == "" {
			name = defaultName
		}

		defaultURL := baseURL
		if defaultURL == "" {
			defaultURL = "http://127.0.0.1:8000"
		}
		fmt.Printf("Base URL [%s]: ", defaultURL)
		if line, err := reader.ReadString('\n'); err == nil {
			line = strings.TrimSpace(line)
			if line != "" {
				baseURL = line
			}
		}
		if baseURL == "" {
			baseURL = defaultURL
		}
	}

	z, err := zone.InitWizard(zone.WizardOptions{
		Dir:           dir,
		Name:          name,
		BaseURL:       baseURL,
		PrimaryServer: server,
	})
	if err != nil {
		return err
	}

	p.Out("")
	p.Out(p.Green(fmt.Sprintf("✨ Created new zone '%s' in %s", z.Name(), z.Root)))
	p.Out("")
	p.Out(p.Bold("📁 Scaffolded boilerplate structure:"))
	p.Out(fmt.Sprintf("   ├── %-30s %s", "zone.yaml", p.Dim("(zone configuration & shorthands)")))
	p.Out(fmt.Sprintf("   ├── %-30s %s", "servers/", p.Dim("(target deployment servers)")))
	p.Out(fmt.Sprintf("   │   ├── %-26s %s", server+".yaml", p.Dim("(server base_url & vars)")))
	p.Out(fmt.Sprintf("   │   ├── %-26s %s", server+".secrets.example.yaml", p.Dim("(credentials template)")))
	p.Out(fmt.Sprintf("   │   ├── %-26s %s", "production.yaml", p.Dim("(production server config)")))
	p.Out(fmt.Sprintf("   │   └── %-26s %s", "production.secrets.example.yaml", p.Dim("(credentials template)")))
	p.Out(fmt.Sprintf("   ├── %-30s %s", "collections/default/", p.Dim("(request collections)")))
	p.Out(fmt.Sprintf("   │   ├── %-26s %s", "_defaults.yaml", p.Dim("(collection headers)")))
	p.Out(fmt.Sprintf("   │   ├── %-26s %s", "00-health.yaml", p.Dim("(health check GET)")))
	p.Out(fmt.Sprintf("   │   └── %-26s %s", "01-get-sample.yaml", p.Dim("(parameterized GET with tests & captures)")))
	p.Out(fmt.Sprintf("   ├── %-30s %s", "chains/", p.Dim("(multi-step scenario journeys)")))
	p.Out(fmt.Sprintf("   │   └── %-26s %s", "smoke.yaml", p.Dim("(smoke test chain)")))
	p.Out(fmt.Sprintf("   ├── %-30s %s", "shorthands.yaml", p.Dim("(presets: hit health, hit sample)")))
	p.Out(fmt.Sprintf("   └── %-30s %s", ".gitignore", p.Dim("(secrets & cache hygiene)")))
	p.Out("")
	p.Out(p.Bold("👉 Next step: Run sanity check to inspect configuration and missing values:"))
	targetDisplay := dir
	if targetDisplay == "." {
		p.Out(p.Cyan("   hit sanity"))
	} else {
		p.Out(p.Cyan(fmt.Sprintf("   hit -z %s sanity", targetDisplay)))
	}
	p.Out("")
	return nil
}
