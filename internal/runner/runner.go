package runner

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hit-endpoint/hit-endpoint/internal/assertions"
	"github.com/hit-endpoint/hit-endpoint/internal/history"
	"github.com/hit-endpoint/hit-endpoint/internal/matrix"
	"github.com/hit-endpoint/hit-endpoint/internal/spec"
	"github.com/hit-endpoint/hit-endpoint/internal/templating"
	"github.com/hit-endpoint/hit-endpoint/internal/types"
	"github.com/hit-endpoint/hit-endpoint/internal/zone"
)

var stepKeys = map[string]bool{
	"vars": true, "captures": true, "tests": true, "headers": true,
	"query": true, "body": true, "auth": true, "timeout": true, "method": true, "url": true,
}

type Session struct {
	mu             sync.RWMutex
	Zone           *zone.Zone
	Server         *zone.Server
	Persist        bool
	State          *zone.StateStore
	SessionVars    map[string]any
	FlowVars       map[string]any
	CliVars        map[string]any
	Strict         bool
	VerifyOverride any
	Secrets        map[string]bool
	Clients        map[string]*http.Client
	History        []*types.Result
	HistoryStore   *history.Store
	NoHistory      bool
}

type SessionOptions struct {
	Zone              *zone.Zone
	ServerName        string
	ExtraVars         map[string]any
	Persist           bool
	Strict            bool
	ZoneOptional      bool
	Verify            any
	NoHistory         bool
}

func NewSession(opts SessionOptions) (*Session, error) {
	z := opts.Zone
	if z == nil && !opts.ZoneOptional {
		var err error
		z, err = zone.Find("")
		if err != nil {
			return nil, err
		}
	}

	var server *zone.Server
	if z != nil {
		var err error
		server, err = z.LoadServer(opts.ServerName)
		if err != nil {
			return nil, err
		}
	} else {
		server = &zone.Server{Name: "none", Vars: make(map[string]any), Secrets: make(map[string]bool)}
	}

	shouldPersist := opts.Persist && z != nil
	var state *zone.StateStore
	if z != nil {
		state = z.State(server.Name)
	}

	sessionVars := make(map[string]any)
	if state != nil {
		for k, v := range state.Load() {
			sessionVars[k] = v
		}
	}

	cliVars := make(map[string]any)
	for k, v := range opts.ExtraVars {
		cliVars[k] = v
	}

	secrets := make(map[string]bool)
	for s := range server.Secrets {
		secrets[s] = true
	}

	var histStore *history.Store
	if !opts.NoHistory {
		zoneRoot := ""
		if z != nil {
			zoneRoot = z.Root
		}
		histStore = history.NewStore(history.GetHistoryPath(zoneRoot))
	}

	return &Session{
		Zone:           z,
		Server:         server,
		Persist:        shouldPersist,
		State:          state,
		SessionVars:    sessionVars,
		FlowVars:       make(map[string]any),
		CliVars:        cliVars,
		Strict:         opts.Strict,
		VerifyOverride: opts.Verify,
		Secrets:        secrets,
		Clients:        make(map[string]*http.Client),
		History:        nil,
		HistoryStore:   histStore,
		NoHistory:      opts.NoHistory,
	}, nil
}

func (s *Session) recordResult(r *types.Result) {
	if s.NoHistory {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.History = append(s.History, r)
}

func (s *Session) Context(requestVars map[string]any, stepVars map[string]any) *templating.Context {
	wsVars := make(map[string]any)
	if s.Zone != nil {
		wsVars = s.Zone.Vars()
	}
	s.mu.RLock()
	sessionVarsCopy := make(map[string]any, len(s.SessionVars))
	for k, v := range s.SessionVars {
		sessionVarsCopy[k] = v
	}
	flowVarsCopy := make(map[string]any, len(s.FlowVars))
	for k, v := range s.FlowVars {
		flowVarsCopy[k] = v
	}
	s.mu.RUnlock()

	layers := []templating.Layer{
		{Name: "zone", Values: wsVars},
		{Name: "server", Values: s.Server.Vars},
		{Name: "request", Values: requestVars},
		{Name: "session", Values: sessionVarsCopy},
		{Name: "flow", Values: flowVarsCopy},
		{Name: "step", Values: stepVars},
		{Name: "cli", Values: s.CliVars},
	}
	return templating.NewContext(layers, s.Secrets)
}

func (s *Session) SetVar(name string, value any, persist *bool) {
	s.mu.Lock()
	s.SessionVars[name] = value
	s.mu.Unlock()

	p := s.Persist
	if persist != nil {
		p = *persist
	}
	if p && s.State != nil {
		s.State.Set(name, value)
		_ = s.State.Save()
	}
}

func (s *Session) Variables() map[string]any {
	return s.Context(nil, nil).Flat()
}

func (s *Session) Clone() *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sessionVars := make(map[string]any, len(s.SessionVars))
	for k, v := range s.SessionVars {
		sessionVars[k] = v
	}
	flowVars := make(map[string]any, len(s.FlowVars))
	for k, v := range s.FlowVars {
		flowVars[k] = v
	}
	cliVars := make(map[string]any, len(s.CliVars))
	for k, v := range s.CliVars {
		cliVars[k] = v
	}
	secrets := make(map[string]bool, len(s.Secrets))
	for k, v := range s.Secrets {
		secrets[k] = v
	}
	return &Session{
		Zone:           s.Zone,
		Server:         s.Server,
		Persist:        false,
		State:          nil,
		SessionVars:    sessionVars,
		FlowVars:       flowVars,
		CliVars:        cliVars,
		Strict:         s.Strict,
		VerifyOverride: s.VerifyOverride,
		Secrets:        secrets,
		Clients:        make(map[string]*http.Client),
		History:        nil,
		HistoryStore:   nil,
		NoHistory:      true,
	}
}

func (s *Session) Client(verify any, timeoutSec float64) *http.Client {
	if s.VerifyOverride != nil {
		verify = s.VerifyOverride
	}
	key := fmt.Sprintf("%v:%.1f", verify, timeoutSec)

	s.mu.RLock()
	if s.Clients != nil {
		if c, ok := s.Clients[key]; ok && c != nil {
			s.mu.RUnlock()
			return c
		}
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Clients == nil {
		s.Clients = make(map[string]*http.Client)
	} else if c, ok := s.Clients[key]; ok && c != nil {
		return c
	}

	tlsConfig := &tls.Config{}
	switch v := verify.(type) {
	case bool:
		if !v {
			tlsConfig.InsecureSkipVerify = true
		}
	case string:
		if v != "" {
			caCert, err := os.ReadFile(v)
			if err == nil {
				caCertPool := x509.NewCertPool()
				caCertPool.AppendCertsFromPEM(caCert)
				tlsConfig.RootCAs = caCertPool
			}
		}
	}

	transport := &http.Transport{
		TLSClientConfig:     tlsConfig,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
	}

	timeout := 30 * time.Second
	if timeoutSec > 0 {
		timeout = time.Duration(timeoutSec * float64(time.Second))
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}
	s.Clients[key] = client
	return client
}

func (s *Session) Close() {
	if s.Persist && s.State != nil {
		s.State.Update(s.SessionVars)
		_ = s.State.Save()
	}
}

func (s *Session) Load(ref string) (*spec.RequestSpec, error) {
	if s.Zone == nil {
		return nil, zone.NewZoneError("No zone; only ad hoc requests are available")
	}
	p, err := s.Zone.ResolveRequest(ref)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(p)
	if err == nil && fi.IsDir() {
		return nil, zone.NewZoneError("'%s' is a folder; use run_folder()", ref)
	}
	return spec.LoadSpec(p, s.Zone.DefaultsChain(p))
}

func (s *Session) RefOf(sp *spec.RequestSpec) string {
	if sp.Path != "" && s.Zone != nil {
		return s.Zone.RequestRef(sp.Path)
	}
	return sp.Name
}

func (s *Session) Run(ref string, step map[string]any) *types.Result {
	sp, err := s.Load(ref)
	if err != nil {
		r := &types.Result{
			Name:  ref,
			Ref:   ref,
			Error: err.Error(),
		}
		s.recordResult(r)
		return r
	}
	return s.RunSpec(sp, step)
}

func (s *Session) Request(method, reqURL string, headers map[string]any, query map[string]any,
	body any, jsonBody any, form map[string]any, auth any, tests []any, captures map[string]any,
	timeout any, name string) *types.Result {

	data := map[string]any{
		"name":   name,
		"method": method,
		"url":    reqURL,
	}
	if headers != nil {
		data["headers"] = headers
	}
	if query != nil {
		data["query"] = query
	}
	if jsonBody != nil {
		data["body"] = map[string]any{"json": jsonBody}
	} else if form != nil {
		data["body"] = map[string]any{"form": form}
	} else if body != nil {
		data["body"] = body
	}
	if auth != nil {
		data["auth"] = auth
	}
	if tests != nil {
		data["tests"] = tests
	}
	if captures != nil {
		data["captures"] = captures
	}
	if timeout != nil {
		data["timeout"] = timeout
	}

	var defaults []map[string]any
	if s.Zone != nil {
		if def, ok := s.Zone.Config["defaults"].(map[string]any); ok {
			defaults = append(defaults, def)
		}
	}
	cwd, _ := os.Getwd()
	sp, err := spec.SpecFromDict(data, defaults, cwd)
	if err != nil {
		r := &types.Result{
			Name:   name,
			Ref:    name,
			Method: method,
			Url:    reqURL,
			Error:  err.Error(),
		}
		s.recordResult(r)
		return r
	}
	return s.RunSpec(sp, nil)
}

func (s *Session) Prepare(sp *spec.RequestSpec, step map[string]any) (*spec.RequestSpec, *spec.Prepared, *templating.Context, error) {
	stepCopy := make(map[string]any)
	for k, v := range step {
		stepCopy[k] = v
	}
	var stepVars map[string]any
	if sv, ok := stepCopy["vars"].(map[string]any); ok {
		stepVars = sv
		delete(stepCopy, "vars")
	}
	var replaceTests any
	if rt, ok := stepCopy["replace_tests"]; ok {
		replaceTests = rt
		delete(stepCopy, "replace_tests")
	}

	overrides := make(map[string]any)
	for k, v := range stepCopy {
		if stepKeys[k] {
			overrides[k] = v
		}
	}

	curSpec := sp
	if len(overrides) > 0 {
		var err error
		curSpec, err = spec.MergeLayer(curSpec, overrides)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	if replaceTests != nil {
		norm, err := spec.NormaliseTests(replaceTests)
		if err != nil {
			return nil, nil, nil, err
		}
		curSpec = spec.CloneSpec(curSpec)
		curSpec.Tests = norm
	}

	ctx := s.Context(curSpec.Vars, stepVars)
	rendered, err := spec.RenderSpec(curSpec, ctx, s.Strict)
	if err != nil {
		return nil, nil, nil, err
	}

	prepared, err := spec.Prepare(rendered, ctx, s.Server.Auth)
	if err != nil {
		return nil, nil, nil, err
	}

	if s.VerifyOverride != nil {
		prepared.Verify = s.VerifyOverride
	}

	s.collectSecrets(rendered.Auth)
	if rendered.Auth == nil || rendered.Auth == "inherit" {
		if s.Server.Auth != nil {
			rEnvAuth, _ := templating.Render(s.Server.Auth, ctx, false)
			s.collectSecrets(rEnvAuth)
		}
	}

	return rendered, prepared, ctx, nil
}

func (s *Session) collectSecrets(auth any) {
	if am, ok := auth.(map[string]any); ok {
		for _, k := range []string{"token", "password", "value"} {
			if strVal, ok := am[k].(string); ok && len(strVal) >= 4 {
				s.Secrets[strVal] = true
			}
		}
	}
}

func (s *Session) RunSpec(sp *spec.RequestSpec, step map[string]any) *types.Result {
	// Check for data-driven matrix definition
	var matrixDef any
	if step != nil && step["matrix"] != nil {
		matrixDef = step["matrix"]
	} else if sp.Extra != nil && sp.Extra["matrix"] != nil {
		matrixDef = sp.Extra["matrix"]
	}

	if matrixDef != nil {
		rows, err := matrix.LoadMatrix(matrixDef, sp.BaseDir())
		if err != nil {
			r := &types.Result{
				Name:   sp.Name,
				Ref:    s.RefOf(sp),
				Method: sp.Method,
				Url:    sp.Url,
				Error:  fmt.Sprintf("matrix error: %v", err),
			}
			s.recordResult(r)
			return r
		}
		if len(rows) > 0 {
			parentResult := &types.Result{
				Name:   sp.Name,
				Ref:    s.RefOf(sp),
				Method: sp.Method,
				Url:    sp.Url,
			}
			stepCopy := make(map[string]any)
			for k, v := range step {
				if k != "matrix" {
					stepCopy[k] = v
				}
			}
			for i, row := range rows {
				expanded := matrix.ExpandSpec(sp, row, i)
				delete(expanded.Extra, "matrix")
				childRes := s.runSingleSpec(expanded, stepCopy)
				parentResult.Matrix = append(parentResult.Matrix, childRes)
			}
			if len(parentResult.Matrix) > 0 {
				last := parentResult.Matrix[len(parentResult.Matrix)-1]
				parentResult.Status = last.Status
				parentResult.HasStatus = last.HasStatus
				parentResult.Reason = last.Reason
				for _, cr := range parentResult.Matrix {
					parentResult.ElapsedMs += cr.ElapsedMs
				}
			}
			return parentResult
		}
	}

	return s.runSingleSpec(sp, step)
}

func (s *Session) runSingleSpec(sp *spec.RequestSpec, step map[string]any) *types.Result {
	ref := s.RefOf(sp)
	rendered, prepared, ctx, err := s.Prepare(sp, step)
	if err != nil {
		r := &types.Result{
			Name:   sp.Name,
			Ref:    ref,
			Method: sp.Method,
			Url:    sp.Url,
			Error:  err.Error(),
		}
		s.recordResult(r)
		return r
	}

	// Before hook
	if beforeScript, ok := rendered.Hooks["before"]; ok && beforeScript != "" {
		hookPath := s.hookPath(sp, beforeScript)
		if err := runBeforeHook(hookPath, prepared, ctx); err != nil {
			r := &types.Result{
				Name:   rendered.Name,
				Ref:    ref,
				Method: prepared.Method,
				Url:    prepared.FullURL(),
				Error:  fmt.Sprintf("before hook failed: %v", err),
			}
			s.recordResult(r)
			return r
		}
	}

	reqHeadersCopy := make(map[string]string, len(prepared.Headers))
	for k, v := range prepared.Headers {
		reqHeadersCopy[k] = v
	}

	result := &types.Result{
		Name:           rendered.Name,
		Ref:            ref,
		Method:         prepared.Method,
		Url:            prepared.FullURL(),
		RequestHeaders: reqHeadersCopy,
		RequestBody:    prepared.BodyPreview,
		Notes:          prepared.Notes,
		Captures:       make(map[string]any),
		CaptureErrors:  make(map[string]string),
	}

	baseClient := s.Client(prepared.Verify, prepared.Timeout)
	reqClient := *baseClient
	if !prepared.FollowRedirects {
		reqClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	} else {
		reqClient.CheckRedirect = nil
	}

	var reqBody io.Reader
	if prepared.Content != nil {
		reqBody = bytes.NewReader(prepared.Content)
	} else if len(prepared.Data) > 0 && len(prepared.Files) == 0 {
		vals := url.Values{}
		for k, v := range prepared.Data {
			vals.Add(k, v)
		}
		reqBody = strings.NewReader(vals.Encode())
	} else if len(prepared.Files) > 0 {
		b, ct, err := spec.BuildMultipartBody(prepared.Data, prepared.Files)
		if err != nil {
			result.Error = err.Error()
			s.recordResult(result)
			return result
		}
		reqBody = bytes.NewReader(b)
		prepared.Headers["Content-Type"] = ct
	}

	req, err := http.NewRequest(prepared.Method, prepared.FullURL(), reqBody)
	if err != nil {
		result.Error = err.Error()
		s.recordResult(result)
		return result
	}

	for k, v := range prepared.Headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := reqClient.Do(req)
	result.ElapsedMs = float64(time.Since(start).Nanoseconds()) / 1e6

	if err != nil {
		result.Error = err.Error()
		s.recordResult(result)
		return result
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	result.Status = resp.StatusCode
	result.HasStatus = true
	result.Reason = http.StatusText(resp.StatusCode)
	result.Size = int64(len(respBytes))
	result.Text = string(respBytes)

	respHeaders := make(map[string]string)
	for k, v := range resp.Header {
		respHeaders[strings.ToLower(k)] = strings.Join(v, ", ")
	}
	result.Headers = respHeaders

	// Parse JSON body
	ct := resp.Header.Get("Content-Type")
	textTrim := strings.TrimSpace(result.Text)
	if strings.Contains(strings.ToLower(ct), "json") || strings.HasPrefix(textTrim, "{") || strings.HasPrefix(textTrim, "[") {
		var j any
		if err := json.Unmarshal(respBytes, &j); err == nil {
			result.JSON = j
		}
	}

	root := result.Root()

	// Captures
	for key, expr := range rendered.Captures {
		val, err := assertions.Search(fmt.Sprintf("%v", expr), root)
		if err != nil {
			result.CaptureErrors[key] = err.Error()
			continue
		}
		if val == nil {
			result.CaptureErrors[key] = fmt.Sprintf("%v matched nothing", expr)
			continue
		}
		result.Captures[key] = val
		s.mu.Lock()
		s.SessionVars[key] = val
		s.mu.Unlock()
	}

	// Tests
	result.Tests = assertions.Evaluate(rendered.Tests, root)

	// After hook
	if afterScript, ok := rendered.Hooks["after"]; ok && afterScript != "" {
		hookPath := s.hookPath(sp, afterScript)
		testRes, capturesUpdate := runAfterHook(hookPath, result, ctx)
		s.mu.Lock()
		for k, v := range capturesUpdate {
			s.SessionVars[k] = v
			result.Captures[k] = v
		}
		s.mu.Unlock()
		result.Tests = append(result.Tests, testRes)
	}

	if s.Persist && s.State != nil && len(result.Captures) > 0 {
		s.State.Update(result.Captures)
		_ = s.State.Save()
	}

	s.recordResult(result)

	if !s.NoHistory && s.HistoryStore != nil {
		zoneName := ""
		if s.Zone != nil {
			zoneName = s.Zone.Name()
		}
		serverName := ""
		if s.Server != nil {
			serverName = s.Server.Name
		}
		source := "run"
		if sp != nil && sp.Path == "" {
			source = "adhoc"
		}
		entry := history.NewEntryFromResult(result, source, zoneName, serverName, ctx.Mask)
		_ = s.HistoryStore.Append(entry)
	}

	return result
}

func (s *Session) hookPath(sp *spec.RequestSpec, name string) string {
	if filepath.IsAbs(name) {
		return name
	}
	candidates := []string{
		filepath.Join(sp.BaseDir(), name),
	}
	if s.Zone != nil {
		candidates = append(candidates, filepath.Join(s.Zone.Root, name))
	}
	cwd, _ := os.Getwd()
	candidates = append(candidates, filepath.Join(cwd, name))

	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return filepath.Join(sp.BaseDir(), name)
}

func (s *Session) RunFolder(folder string, onResult func(*types.Result), failFast bool) []*types.Result {
	var results []*types.Result
	for _, path := range s.Zone.ListRequests(folder) {
		sp, err := spec.LoadSpec(path, s.Zone.DefaultsChain(path))
		if err != nil {
			r := &types.Result{
				Name:  filepath.Base(path),
				Ref:   s.Zone.RequestRef(path),
				Error: err.Error(),
			}
			results = append(results, r)
			if onResult != nil {
				onResult(r)
			}
			if failFast {
				break
			}
			continue
		}
		r := s.RunSpec(sp, nil)
		results = append(results, r)
		if onResult != nil {
			onResult(r)
		}
		if failFast && !r.OK() {
			break
		}
	}
	return results
}

func runBeforeHook(hookPath string, prepared *spec.Prepared, ctx *templating.Context) error {
	if !strings.HasSuffix(hookPath, ".py") {
		return nil
	}
	// Run python script bridge
	pythonCode := `
import sys, json, importlib.util
data = json.load(sys.stdin)
path = sys.argv[1]
spec = importlib.util.spec_from_file_location("hook", path)
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)
if hasattr(mod, "before"):
    class Prep: pass
    p = Prep()
    p.method = data["method"]
    p.url = data["url"]
    p.headers = data["headers"]
    p.params = data["params"]
    class Ctx:
        def __init__(self, m): self.m = m
        def get(self, k, d=None): return self.m.get(k, d)
        def set(self, k, v): self.m[k] = v
    c = Ctx(data["context"])
    mod.before(p, c)
    json.dump({"headers": p.headers, "params": p.params, "context": c.m}, sys.stdout)
`
	inputData := map[string]any{
		"method":  prepared.Method,
		"url":     prepared.Url,
		"headers": prepared.Headers,
		"params":  prepared.Params,
		"context": ctx.Flat(),
	}
	inputBytes, _ := json.Marshal(inputData)

	cmd := exec.Command("python3", "-c", pythonCode, hookPath)
	cmd.Stdin = bytes.NewReader(inputBytes)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%v: %s", err, errBuf.String())
	}

	var outputData struct {
		Headers map[string]string `json:"headers"`
		Params  map[string]any    `json:"params"`
		Context map[string]any    `json:"context"`
	}
	if err := json.Unmarshal(outBuf.Bytes(), &outputData); err == nil {
		if outputData.Headers != nil {
			prepared.Headers = outputData.Headers
		}
		if outputData.Params != nil {
			prepared.Params = outputData.Params
		}
		for k, v := range outputData.Context {
			ctx.Set(k, v)
		}
	}
	return nil
}

func runAfterHook(hookPath string, result *types.Result, ctx *templating.Context) (assertions.TestResult, map[string]any) {
	testName := fmt.Sprintf("after hook %s", filepath.Base(hookPath))
	capturesUpdate := make(map[string]any)

	if !strings.HasSuffix(hookPath, ".py") {
		return assertions.TestResult{Name: testName, Passed: true}, capturesUpdate
	}

	pythonCode := `
import sys, json, importlib.util
data = json.load(sys.stdin)
path = sys.argv[1]
spec = importlib.util.spec_from_file_location("hook", path)
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)
if hasattr(mod, "after"):
    class Res: pass
    r = Res()
    r.status = data["status"]
    r.headers = data["headers"]
    r.json = data["json"]
    r.text = data["text"]
    r.elapsed_ms = data["elapsed_ms"]
    class Ctx:
        def __init__(self, m):
            self.orig = dict(m)
            self.curr = dict(m)
        def get(self, k, d=None): return self.curr.get(k, d)
        def set(self, k, v): self.curr[k] = v
    c = Ctx(data["context"])
    try:
        mod.after(r, c)
        new_vars = {k: v for k, v in c.curr.items() if k not in c.orig or c.orig[k] != v}
        json.dump({"ok": True, "new_vars": new_vars}, sys.stdout)
    except AssertionError as exc:
        json.dump({"ok": False, "error": str(exc) or "assertion failed"}, sys.stdout)
    except Exception as exc:
        json.dump({"ok": False, "error": f"{exc.__class__.__name__}: {exc}"}, sys.stdout)
`
	inputData := map[string]any{
		"status":     result.Status,
		"headers":    result.Headers,
		"json":       result.JSON,
		"text":       result.Text,
		"elapsed_ms": result.ElapsedMs,
		"context":    ctx.Flat(),
	}
	inputBytes, _ := json.Marshal(inputData)

	cmd := exec.Command("python3", "-c", pythonCode, hookPath)
	cmd.Stdin = bytes.NewReader(inputBytes)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		errStr := strings.TrimSpace(errBuf.String())
		if errStr == "" {
			errStr = err.Error()
		}
		return assertions.TestResult{Name: testName, Passed: false, Detail: errStr}, capturesUpdate
	}

	var outputData struct {
		OK      bool           `json:"ok"`
		Error   string         `json:"error"`
		NewVars map[string]any `json:"new_vars"`
	}
	if err := json.Unmarshal(outBuf.Bytes(), &outputData); err != nil {
		return assertions.TestResult{Name: testName, Passed: false, Detail: err.Error()}, capturesUpdate
	}

	if !outputData.OK {
		return assertions.TestResult{Name: testName, Passed: false, Detail: outputData.Error}, capturesUpdate
	}

	for k, v := range outputData.NewVars {
		capturesUpdate[k] = v
	}
	return assertions.TestResult{Name: testName, Passed: true}, capturesUpdate
}
