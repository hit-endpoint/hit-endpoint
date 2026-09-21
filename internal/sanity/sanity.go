package sanity

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"hit/internal/flows"
	"hit/internal/runner"
	"hit/internal/spec"
	"hit/internal/templating"
	"hit/internal/zone"
)

const Version = "0.1.0"

type Check struct {
	Status string `json:"status"` // ok | warn | fail | info
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

type SanityReport struct {
	Zone        string  `json:"zone"`
	Root        string  `json:"root"`
	Server      string  `json:"server"`
	Scope       string  `json:"scope"`
	Ready       bool    `json:"ready"`
	Failures    int     `json:"failures"`
	Warnings    int     `json:"warnings"`
	Checks      []Check `json:"checks"`
}

func (r *SanityReport) Add(status, title, detail, fix string) {
	r.Checks = append(r.Checks, Check{
		Status: status,
		Title:  title,
		Detail: detail,
		Fix:    fix,
	})
	if status == "fail" {
		r.Failures++
		r.Ready = false
	} else if status == "warn" {
		r.Warnings++
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func RunSanity(z *zone.Zone, serverName string, scope string, ping bool, timeoutSec float64) *SanityReport {
	scopeStr := scope
	if scopeStr == "" {
		scopeStr = "all collections"
	}
	rep := &SanityReport{
		Zone:        z.Name(),
		Root:        z.Root,
		Server:      "",
		Scope:       scopeStr,
		Ready:       true,
	}

	// 1. Tool info
	rep.Add("info", fmt.Sprintf("hit %s on Go %s (%s/%s)", Version, runtime.Version(), runtime.GOOS, runtime.GOARCH), z.Root, "")

	// 2. Servers
	serverNames := z.ServerNames()
	defaultServer, _ := z.Config["default_server"].(string)
	chosen := serverName
	if chosen == "" {
		chosen = defaultServer
	}

	if len(serverNames) == 0 {
		relServer, _ := filepath.Rel(z.Root, z.ServersDir())
		rep.Add("fail", "no servers defined", fmt.Sprintf("%s/ has no <name>.yaml", relServer),
			"create servers/<name>.yaml with base_url, vars and auth (hit reference shows the format), then set default_server in zone.yaml")
	} else {
		rep.Add("ok", fmt.Sprintf("servers: %s", strings.Join(serverNames, ", ")), "", "")
	}

	if defaultServer == "" {
		status := "info"
		detail := ""
		fix := ""
		if len(serverNames) > 0 {
			status = "warn"
			detail = "every command needs -s <name>"
			fix = fmt.Sprintf("add 'default_server: %s' to zone.yaml", serverNames[0])
		}
		rep.Add(status, "default_server is not set in zone.yaml", detail, fix)
	} else {
		found := false
		for _, n := range serverNames {
			if n == defaultServer {
				found = true
				break
			}
		}
		if !found {
			relServer, _ := filepath.Rel(z.Root, z.ServersDir())
			rep.Add("fail", fmt.Sprintf("default_server '%s' has no file", defaultServer),
				fmt.Sprintf("expected %s/%s.yaml", relServer, defaultServer),
				"create it, or change default_server to one of: "+strings.Join(serverNames, ", "))
		}
	}

	var session *runner.Session
	var server *zone.Server
	if chosen != "" {
		var err error
		session, err = runner.NewSession(runner.SessionOptions{
			Zone:       z,
			ServerName: chosen,
			Persist:    false,
		})
		if err == nil {
			server = session.Server
			rep.Server = server.Name
			relPath, _ := filepath.Rel(z.Root, server.Path)
			rep.Add("ok", fmt.Sprintf("server '%s' loads", server.Name), relPath, "")
		} else {
			rep.Add("fail", fmt.Sprintf("server '%s' does not load", chosen), err.Error(), "fix the YAML in that file")
		}
	}

	if session == nil {
		session, _ = runner.NewSession(runner.SessionOptions{
			Zone:         z,
			ZoneOptional: true,
			Persist:      false,
		})
		rep.Server = "none"
	}

	// 3. Server contents
	if server != nil {
		if bu, ok := server.Vars["base_url"]; ok && fmt.Sprintf("%v", bu) != "" {
			rep.Add("ok", fmt.Sprintf("base_url %v", bu), "", "")
		} else {
			rep.Add("warn", "server has no base_url",
				"requests with relative urls (e.g. /pets) will fail; absolute urls and {{baseUrl}}-style variables still work",
				"add 'base_url: https://...' to the server file if any request uses a relative url")
		}

		// $env references
		placeholders := templating.FindPlaceholders(map[string]any{"v": server.Vars, "a": server.Auth})
		var missingOS []string
		for _, name := range placeholders {
			lower := strings.ToLower(name)
			if strings.HasPrefix(lower, "$env:") {
				rest := name[5:]
				if !strings.Contains(rest, ":") {
					if _, set := os.LookupEnv(rest); !set {
						missingOS = append(missingOS, rest)
					}
				}
			}
		}
		sort.Strings(missingOS)
		if len(missingOS) > 0 {
			rep.Add("fail", "OS environment variables referenced but not set: "+strings.Join(missingOS, ", "),
				"the server file uses {{$env:NAME}} for these",
				"export them in your shell (export NAME=...) or add them to your shell profile / .env")
		}

		// Secrets file
		secFile := filepath.Join(z.ServersDir(), server.Name+".secrets.yaml")
		secYml := filepath.Join(z.ServersDir(), server.Name+".secrets.yml")
		exampleFile := filepath.Join(z.ServersDir(), server.Name+".secrets.example.yaml")
		exampleYml := filepath.Join(z.ServersDir(), server.Name+".secrets.example.yml")

		var activeSec string
		if fi, err := os.Stat(secFile); err == nil && !fi.IsDir() {
			activeSec = secFile
		} else if fi, err := os.Stat(secYml); err == nil && !fi.IsDir() {
			activeSec = secYml
		}

		var activeEx string
		if fi, err := os.Stat(exampleFile); err == nil && !fi.IsDir() {
			activeEx = exampleFile
		} else if fi, err := os.Stat(exampleYml); err == nil && !fi.IsDir() {
			activeEx = exampleYml
		}

		if activeSec != "" {
			rep.Add("ok", fmt.Sprintf("secrets file present (%s, %s)", filepath.Base(activeSec), plural(len(server.Secrets), "value")), "", "")
			checkNotTracked(z, activeSec, rep)
		} else if activeEx != "" {
			relEx, _ := filepath.Rel(z.Root, activeEx)
			relServer, _ := filepath.Rel(z.Root, z.ServersDir())
			rep.Add("fail", fmt.Sprintf("secrets file missing: copy %s to %s.secrets.yaml and fill it in", filepath.Base(activeEx), server.Name),
				"", fmt.Sprintf("cp %s %s/%s.secrets.yaml", relEx, relServer, server.Name))
		}

		// Auth
		if server.Auth == nil {
			rep.Add("info", "server defines no default auth", "requests send no credentials unless they declare their own", "")
		} else if am, ok := server.Auth.(map[string]any); ok {
			kind := strings.ToLower(fmt.Sprintf("%v", am["type"]))
			switch kind {
			case "bearer", "basic", "apikey", "api_key", "api-key", "header", "none", "noauth":
				rep.Add("ok", fmt.Sprintf("server auth: %s", kind), "", "")
			default:
				rep.Add("fail", fmt.Sprintf("server auth type '%s' is not supported", kind), "", "use bearer, basic, apikey, header or none")
			}
		}
	}

	// 4. Collections
	scopeDir := z.CollectionsDir()
	if scope != "" {
		target, err := z.ResolveRequest(scope)
		if err == nil {
			fi, err := os.Stat(target)
			if err == nil && fi.IsDir() {
				scopeDir = target
			} else {
				scopeDir = filepath.Dir(target)
			}
			rep.Scope, _ = filepath.Rel(z.Root, scopeDir)
		} else {
			rep.Add("fail", fmt.Sprintf("scope '%s' not found", scope), err.Error(), "")
		}
	}

	var collections []string
	if entries, err := os.ReadDir(z.CollectionsDir()); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				collections = append(collections, e.Name())
			}
		}
	}
	requests := z.ListRequests(scopeDir)

	if len(collections) == 0 {
		relColl, _ := filepath.Rel(z.Root, z.CollectionsDir())
		rep.Add("fail", "no collections", fmt.Sprintf("%s/ is empty", relColl),
			"import a collection export (hit import collection file.json) or hit new <collection>/<request>")
	} else if len(requests) == 0 {
		rep.Add("warn", fmt.Sprintf("no request files under %s", rep.Scope), "", "hit new <collection>/<request> to add one")
	} else {
		extra := ""
		if scope != "" {
			extra = fmt.Sprintf(" in %s", rep.Scope)
		}
		rep.Add("ok", fmt.Sprintf("%s, %s%s", plural(len(collections), "collection"), plural(len(requests), "request file"), extra), "", "")
	}

	// Captures available anywhere
	captured := make(map[string][]string)
	for _, p := range z.ListRequests("") {
		sp, err := spec.LoadSpec(p, z.DefaultsChain(p))
		if err == nil {
			ref := z.RequestRef(p)
			for name := range sp.Captures {
				captured[name] = append(captured[name], ref)
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

	var parseErrors []string
	var drafts []string
	var unconvertedLeftovers []string
	noTests := 0
	undefinedCounts := make(map[string]int)
	needsCaptureCounts := make(map[string]int)
	var defaultsErrors []string

	for _, p := range requests {
		ref := z.RequestRef(p)
		raw, err := zone.LoadYAML(p)
		if err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("%s: %v", ref, err))
			continue
		}
		if _, hasUnconverted := raw["unconverted"]; hasUnconverted {
			unconvertedLeftovers = append(unconvertedLeftovers, ref)
		}

		sp, err := spec.LoadSpec(p, z.DefaultsChain(p))
		if err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("%s: %v", ref, err))
			continue
		}
		if sp.Url == "" {
			drafts = append(drafts, ref)
			continue
		}
		if len(sp.Tests) == 0 {
			noTests++
		}

		_, _, _, err = session.Prepare(sp, nil)
		if err != nil {
			if mErr, ok := err.(*templating.MissingVariableError); ok {
				for _, name := range mErr.Names {
					if _, isCap := captured[name]; isCap {
						needsCaptureCounts[name]++
					} else {
						undefinedCounts[name]++
					}
				}
			} else {
				parseErrors = append(parseErrors, fmt.Sprintf("%s: %v", ref, err))
			}
		}
	}

	folderSet := make(map[string]bool)
	for _, p := range requests {
		folderSet[filepath.Dir(p)] = true
	}
	for folder := range folderSet {
		df := filepath.Join(folder, zone.DefaultsFile)
		if _, err := os.Stat(df); err == nil {
			if _, err := zone.LoadYAML(df); err != nil {
				relDF, _ := filepath.Rel(z.Root, df)
				defaultsErrors = append(defaultsErrors, fmt.Sprintf("%s: %v", relDF, err))
			}
		}
	}

	allErrors := append(parseErrors, defaultsErrors...)
	if len(allErrors) > 0 {
		limit := 8
		if len(allErrors) < limit {
			limit = len(allErrors)
		}
		rep.Add("fail", fmt.Sprintf("%s do not load", plural(len(allErrors), "file")),
			strings.Join(allErrors[:limit], "\n"), "fix the YAML; hit validate <ref> shows the full error")
	} else if len(requests) > 0 {
		rep.Add("ok", "all request files parse", "", "")
	}

	if len(undefinedCounts) > 0 {
		var undefList []string
		for k, count := range undefinedCounts {
			undefList = append(undefList, fmt.Sprintf("{{%s}} (%d)", k, count))
		}
		sort.Strings(undefList)
		rep.Add("fail", fmt.Sprintf("undefined variables used by requests: %s", strings.Join(undefList, ", ")),
			"counts are the number of requests affected; nothing defines these in the zone, server, defaults or captures",
			"add them to the environment file (or its secrets file) with the values for this server")
	} else if len(requests) > 0 {
		rep.Add("ok", "every variable is defined or captured", "", "")
	}

	if len(needsCaptureCounts) > 0 {
		state := session.SessionVars
		var pending []string
		var detailLines []string
		for n := range needsCaptureCounts {
			if _, set := state[n]; !set {
				pending = append(pending, n)
				sources := captured[n]
				limit := 2
				if len(sources) < limit {
					limit = len(sources)
				}
				detailLines = append(detailLines, fmt.Sprintf("{{%s}} → run %s", n, strings.Join(sources[:limit], " or ")))
			}
		}
		sort.Strings(pending)
		if len(pending) > 0 {
			var wrapped []string
			for _, n := range pending {
				wrapped = append(wrapped, "{{"+n+"}}")
			}
			rep.Add("warn", fmt.Sprintf("%s not captured yet: %s", plural(len(pending), "variable"), strings.Join(wrapped, ", ")),
				strings.Join(detailLines, "\n"), "run the capturing request(s) first, e.g. the login; hit vars shows what is stored")
		}
	}

	// Auth token readiness
	if server != nil && server.Auth != nil {
		if am, ok := server.Auth.(map[string]any); ok {
			var authVars []string
			for _, n := range templating.FindPlaceholders(am) {
				if !strings.HasPrefix(n, "$") {
					authVars = append(authVars, n)
				}
			}
			known := session.Variables()
			for _, n := range authVars {
				if _, ok := known[n]; ok {
					isCap := false
					if _, c := session.SessionVars[n]; c {
						isCap = true
					}
					capStr := ""
					if isCap {
						capStr = " (captured)"
					}
					rep.Add("ok", fmt.Sprintf("auth variable {{%s}} is set%s", n, capStr), "", "")
				} else if sources, ok := captured[n]; ok && len(sources) > 0 {
					rep.Add("warn", fmt.Sprintf("not logged in: environment auth needs {{%s}}", n),
						"requests will be sent without auth until it is captured",
						fmt.Sprintf("hit run %s", sources[0]))
				} else {
					rep.Add("fail", fmt.Sprintf("environment auth needs {{%s}} but nothing defines or captures it", n),
						"", fmt.Sprintf("add %s to %s.secrets.yaml, or add a login request with captures: {%s: json.<field>}", n, server.Name, n))
				}
			}
		}
	}

	if len(drafts) > 0 {
		rep.Add("warn", fmt.Sprintf("%s have no url", plural(len(drafts), "request")),
			strings.Join(drafts, "\n"), "fill in the url or delete the file")
	}

	if len(unconvertedLeftovers) > 0 {
		rep.Add("warn", fmt.Sprintf("%s still carry untranslated scripts (unconverted: block)", plural(len(unconvertedLeftovers), "request")),
			strings.Join(unconvertedLeftovers, "\n"), "port them to tests:/captures:/flows and delete the block")
	}

	if len(requests) > 0 && noTests > 0 {
		rep.Add("info", fmt.Sprintf("%s have no tests", plural(noTests, "request")), "they pass on any status below 400", "")
	}

	// 5. Flows
	fls := z.ListChains()
	if len(fls) > 0 {
		var broken []string
		for _, fp := range fls {
			fData, err := flows.LoadFlow(fp)
			stem := strings.TrimSuffix(filepath.Base(fp), filepath.Ext(fp))
			if err != nil {
				broken = append(broken, fmt.Sprintf("%s: %v", stem, err))
				continue
			}
			steps, _ := fData["steps"].([]any)
			for i, rawStep := range steps {
				if sm, ok := rawStep.(map[string]any); ok {
					if req, ok := sm["request"].(string); ok {
						if _, err := z.ResolveRequest(req); err != nil {
							broken = append(broken, fmt.Sprintf("%s step %d: request '%s' not found", stem, i+1, req))
						}
					}
					if subf, ok := sm["flow"].(string); ok {
						if _, err := z.ResolveChain(subf); err != nil {
							broken = append(broken, fmt.Sprintf("%s step %d: flow '%s' not found", stem, i+1, subf))
						}
					}
				}
			}
		}
		if len(broken) > 0 {
			rep.Add("fail", plural(len(broken), "flow problem"), strings.Join(broken, "\n"), "fix the step references")
		} else {
			rep.Add("ok", fmt.Sprintf("%s, all steps resolve", plural(len(fls), "flow")), "", "")
		}
	}

	// 6. Hygiene
	giPath := filepath.Join(z.Root, ".gitignore")
	giBytes, _ := os.ReadFile(giPath)
	giText := string(giBytes)
	if strings.Contains(giText, "secrets") && strings.Contains(giText, ".hit") {
		rep.Add("ok", ".gitignore excludes *.secrets.yaml and .hit/", "", "")
	} else {
		rep.Add("warn", ".gitignore does not exclude secrets and state", "", "add '*.secrets.yaml' and '.hit/' to .gitignore")
	}

	stateFile := z.State(rep.Server).Path
	if fi, err := os.Stat(stateFile); err == nil {
		stored := z.State(rep.Server).Load()
		if len(stored) > 0 {
			ageH := time.Since(fi.ModTime()).Hours()
			relState, _ := filepath.Rel(z.Root, stateFile)
			fix := ""
			if ageH > 24 {
				fix = "hit vars clear to start fresh"
			}
			rep.Add("info", fmt.Sprintf("captured state: %s, last updated %.0f h ago", plural(len(stored), "variable"), ageH), relState, fix)
		}
	}

	// 7. Connectivity
	if ping && server != nil {
		if bu, ok := server.Vars["base_url"]; ok && fmt.Sprintf("%v", bu) != "" {
			buStr := fmt.Sprintf("%v", bu)
			timeout := 5 * time.Second
			if timeoutSec > 0 {
				timeout = time.Duration(timeoutSec * float64(time.Second))
			}
			client := &http.Client{
				Timeout: timeout,
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}
			t0 := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, "GET", buStr, nil)
			if err == nil {
				resp, err := client.Do(req)
				ms := float64(time.Since(t0).Nanoseconds()) / 1e6
				if err == nil {
					resp.Body.Close()
					rep.Add("ok", fmt.Sprintf("server reachable: %s → %d in %.0f ms", buStr, resp.StatusCode, ms), "", "")
				} else {
					rep.Add("fail", fmt.Sprintf("server not reachable: %s", buStr), err.Error(),
						"check VPN / network, the base_url value, or run with --offline to skip this check")
				}
			}
		} else {
			rep.Add("info", "connectivity not checked (no base_url)", "", "")
		}
	}

	return rep
}

func checkNotTracked(z *zone.Zone, secPath string, rep *SanityReport) {
	rel, err := filepath.Rel(z.Root, secPath)
	if err != nil {
		return
	}
	cmd := exec.Command("git", "ls-files", "--error-unmatch", rel)
	cmd.Dir = z.Root
	if err := cmd.Run(); err == nil {
		rep.Add("fail", fmt.Sprintf("%s is tracked by git", filepath.Base(secPath)),
			"secrets must never be committed",
			fmt.Sprintf("git rm --cached %s and make sure .gitignore has *.secrets.yaml", rel))
	}
}
