package runner

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hit-endpoint/hit-endpoint/internal/assertions"
	internalAuth "github.com/hit-endpoint/hit-endpoint/internal/auth"
	"github.com/hit-endpoint/hit-endpoint/internal/history"
	"github.com/hit-endpoint/hit-endpoint/internal/matrix"
	"github.com/hit-endpoint/hit-endpoint/internal/spec"
	"github.com/hit-endpoint/hit-endpoint/internal/templating"
	"github.com/hit-endpoint/hit-endpoint/internal/types"
	"github.com/hit-endpoint/hit-endpoint/internal/zone"
)

func isSensitiveKey(k string) bool {
	lk := strings.ToLower(strings.TrimSpace(k))
	return lk == "token" || lk == "access_token" || lk == "refresh_token" || lk == "id_token" ||
		lk == "password" || lk == "passwd" || lk == "secret" || lk == "client_secret" ||
		lk == "api_key" || lk == "apikey" || lk == "cookie" || lk == "key" || lk == "jwt" || lk == "sid" ||
		strings.Contains(lk, "token") || strings.Contains(lk, "secret") || strings.Contains(lk, "password") || strings.Contains(lk, "apikey") || strings.Contains(lk, "api_key")
}

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
	for k, v := range sessionVars {
		if isSensitiveKey(k) {
			if strVal, ok := v.(string); ok && len(strVal) >= 4 {
				secrets[strVal] = true
			}
		}
	}
	for k, v := range cliVars {
		if isSensitiveKey(k) {
			if strVal, ok := v.(string); ok && len(strVal) >= 4 {
				secrets[strVal] = true
			}
		}
	}
	if ga, err := internalAuth.LoadGlobalAuth(); err == nil && ga != nil {
		if len(ga.Token) >= 4 {
			secrets[ga.Token] = true
		}
		if len(ga.Cookie) >= 4 {
			secrets[ga.Cookie] = true
		}
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
	cliVarsCopy := make(map[string]any, len(s.CliVars))
	for k, v := range s.CliVars {
		cliVarsCopy[k] = v
	}
	serverVarsCopy := make(map[string]any)
	if s.Server != nil {
		serverVarsCopy = make(map[string]any, len(s.Server.Vars))
		for k, v := range s.Server.Vars {
			serverVarsCopy[k] = v
		}
	}
	secretsCopy := make(map[string]bool, len(s.Secrets))
	for k, v := range s.Secrets {
		secretsCopy[k] = v
	}
	s.mu.RUnlock()

	layers := []templating.Layer{
		{Name: "zone", Values: wsVars},
		{Name: "server", Values: serverVarsCopy},
		{Name: "request", Values: requestVars},
		{Name: "session", Values: sessionVarsCopy},
		{Name: "flow", Values: flowVarsCopy},
		{Name: "step", Values: stepVars},
		{Name: "cli", Values: cliVarsCopy},
	}
	return templating.NewContext(layers, secretsCopy)
}

func (s *Session) SetVar(name string, value any, persist *bool) {
	s.mu.Lock()
	s.SessionVars[name] = value
	if isSensitiveKey(name) {
		if strVal, ok := value.(string); ok && len(strVal) >= 4 {
			s.Secrets[strVal] = true
		}
	}
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
	var serverCopy *zone.Server
	if s.Server != nil {
		sVars := make(map[string]any, len(s.Server.Vars))
		for k, v := range s.Server.Vars {
			sVars[k] = v
		}
		sSecs := make(map[string]bool, len(s.Server.Secrets))
		for k, v := range s.Server.Secrets {
			sSecs[k] = v
		}
		serverCopy = &zone.Server{
			Name:    s.Server.Name,
			Vars:    sVars,
			Auth:    s.Server.Auth,
			Secrets: sSecs,
			Path:    s.Server.Path,
		}
	}
	return &Session{
		Zone:           s.Zone,
		Server:         serverCopy,
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
		s.mu.RLock()
		varsCopy := make(map[string]any, len(s.SessionVars))
		for k, v := range s.SessionVars {
			varsCopy[k] = v
		}
		s.mu.RUnlock()
		s.State.Update(varsCopy)
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
	} else if len(form) > 0 {
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
	s.collectBodySecrets(rendered.Body)
	s.collectBodySecrets(prepared.BodyPreview)
	if rendered.Auth == nil || rendered.Auth == "inherit" {
		if s.Server != nil && s.Server.Auth != nil {
			rEnvAuth, _ := templating.Render(s.Server.Auth, ctx, false)
			s.collectSecrets(rEnvAuth)
		}
	}

	// Refresh context so ctx.Mask includes all secrets collected from auth and body
	ctx = s.Context(curSpec.Vars, stepVars)

	return rendered, prepared, ctx, nil
}

func (s *Session) collectSecrets(auth any) {
	am, ok := auth.(map[string]any)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range []string{"token", "password", "value", "cookie", "secret", "key"} {
		if strVal, ok := am[k].(string); ok && len(strVal) >= 4 {
			s.Secrets[strVal] = true
		}
	}
}

func (s *Session) collectBodySecrets(body any) {
	if body == nil {
		return
	}
	var found []string
	var walk func(v any)
	walk = func(v any) {
		switch val := v.(type) {
		case map[string]any:
			for k, item := range val {
				if isSensitiveKey(k) {
					if strVal, ok := item.(string); ok && len(strVal) >= 4 && !strings.Contains(strVal, "{{") {
						found = append(found, strVal)
					}
				} else {
					walk(item)
				}
			}
		case []any:
			for _, item := range val {
				walk(item)
			}
		case string:
			trimmed := strings.TrimSpace(val)
			if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
				var parsed map[string]any
				if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
					walk(parsed)
				}
			}
		}
	}
	walk(body)
	if len(found) > 0 {
		s.mu.Lock()
		for _, sec := range found {
			s.Secrets[sec] = true
		}
		s.mu.Unlock()
	}
}

func (s *Session) Mask(text string) string {
	return s.Context(nil, nil).Mask(text)
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
		newVars, err := runBeforeHook(hookPath, prepared, ctx)
		if err != nil {
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
		if len(newVars) > 0 {
			s.mu.Lock()
			for k, v := range newVars {
				s.SessionVars[k] = v
			}
			s.mu.Unlock()
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
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "hit/1.1.0")
		if result.RequestHeaders != nil {
			result.RequestHeaders["User-Agent"] = "hit/1.1.0"
		}
	}

	var (
		dnsStart, dnsDone         time.Time
		connectStart, connectDone time.Time
		tlsStart, tlsDone         time.Time
		gotConn, wroteReq         time.Time
		firstByte                 time.Time
	)
	trace := &httptrace.ClientTrace{
		DNSStart:          func(_ httptrace.DNSStartInfo) { dnsStart = time.Now() },
		DNSDone:           func(_ httptrace.DNSDoneInfo) { dnsDone = time.Now() },
		ConnectStart:      func(_, _ string) { connectStart = time.Now() },
		ConnectDone:       func(_, _ string, _ error) { connectDone = time.Now() },
		TLSHandshakeStart: func() { tlsStart = time.Now() },
		TLSHandshakeDone:  func(_ tls.ConnectionState, _ error) { tlsDone = time.Now() },
		GotConn:           func(_ httptrace.GotConnInfo) { gotConn = time.Now() },
		WroteRequest:      func(_ httptrace.WroteRequestInfo) { wroteReq = time.Now() },
		GotFirstResponseByte: func() {
			firstByte = time.Now()
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	start := time.Now()
	resp, err := reqClient.Do(req)
	if err != nil {
		result.ElapsedMs = float64(time.Since(start).Nanoseconds()) / 1e6
		result.Error = err.Error()
		s.recordResult(result)
		return result
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	end := time.Now()
	result.ElapsedMs = float64(end.Sub(start).Nanoseconds()) / 1e6

	timings := &types.RequestTimings{}
	if !dnsStart.IsZero() && !dnsDone.IsZero() && dnsDone.After(dnsStart) {
		timings.DNS = float64(dnsDone.Sub(dnsStart).Nanoseconds()) / 1e6
	}
	if !connectStart.IsZero() && !connectDone.IsZero() && connectDone.After(connectStart) {
		timings.Connect = float64(connectDone.Sub(connectStart).Nanoseconds()) / 1e6
	}
	if !tlsStart.IsZero() && !tlsDone.IsZero() && tlsDone.After(tlsStart) {
		timings.SSL = float64(tlsDone.Sub(tlsStart).Nanoseconds()) / 1e6
	}
	if !gotConn.IsZero() && !wroteReq.IsZero() && wroteReq.After(gotConn) {
		timings.Send = float64(wroteReq.Sub(gotConn).Nanoseconds()) / 1e6
	}
	waitStart := wroteReq
	if waitStart.IsZero() {
		waitStart = gotConn
	}
	if waitStart.IsZero() {
		waitStart = start
	}
	if !firstByte.IsZero() && firstByte.After(waitStart) {
		timings.Wait = float64(firstByte.Sub(waitStart).Nanoseconds()) / 1e6
	} else {
		timings.Wait = result.ElapsedMs
	}
	if !firstByte.IsZero() && end.After(firstByte) {
		timings.Receive = float64(end.Sub(firstByte).Nanoseconds()) / 1e6
	}
	result.Timings = timings
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

	if result.JSON != nil {
		s.collectBodySecrets(result.JSON)
	}

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
		if isSensitiveKey(key) || isSensitiveKey(fmt.Sprintf("%v", expr)) {
			if strVal, ok := val.(string); ok && len(strVal) >= 4 {
				s.Secrets[strVal] = true
			}
		}
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
			if isSensitiveKey(k) {
				if strVal, ok := v.(string); ok && len(strVal) >= 4 {
					s.Secrets[strVal] = true
				}
			}
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
		entry := history.NewEntryFromResult(result, source, zoneName, serverName, s.Mask)
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

func runBeforeHook(hookPath string, prepared *spec.Prepared, ctx *templating.Context) (map[string]any, error) {
	if !strings.HasSuffix(hookPath, ".py") {
		return nil, nil
	}
	var bodyVal any
	if len(prepared.Content) > 0 {
		var parsed any
		if err := json.Unmarshal(prepared.Content, &parsed); err == nil {
			bodyVal = parsed
		} else {
			bodyVal = string(prepared.Content)
		}
	}

	pythonCode := `
import sys, json, inspect, importlib.util
real_out = sys.stdout
sys.stdout = sys.stderr
data = json.load(sys.stdin)
path = sys.argv[1]
spec = importlib.util.spec_from_file_location("hook", path)
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)
class Ctx:
    def __init__(self, m):
        self.orig = dict(m)
        self.m = dict(m)
        self.vars = self.m
    def get(self, k, d=None): return self.m.get(k, d)
    def set(self, k, v): self.m[k] = v
    def __getitem__(self, k): return self.m[k]
    def __setitem__(self, k, v): self.m[k] = v
c = Ctx(data.get("context") or {})
class Prep:
    def __init__(self):
        self.method = data.get("method", "GET")
        self.url = data.get("url", "")
        self.headers = dict(data.get("headers") or {})
        self.params = dict(data.get("params") or {})
        self.body = data.get("body")
        self.json = self.body
        self.vars = c.m
    def get(self, k, d=None): return c.get(k, d)
    def set(self, k, v): c.set(k, v)
    def __getitem__(self, k): return c.m[k]
    def __setitem__(self, k, v): c.m[k] = v
p = Prep()
fn = getattr(mod, "before", None) or getattr(mod, "run", None)
if fn is not None:
    sig = inspect.signature(fn)
    if len(sig.parameters) == 0:
        fn()
    elif len(sig.parameters) == 1:
        fn(p)
    else:
        fn(p, c)
body_out = p.body if p.body != data.get("body") else (p.json if p.json != data.get("body") else data.get("body"))
body_changed = (body_out != data.get("body"))
new_vars = {k: v for k, v in c.m.items() if k not in c.orig or c.orig[k] != v}
headers_out = {str(k): str(v) for k, v in (p.headers or {}).items()}
json.dump({
    "method": str(p.method),
    "url": str(p.url),
    "headers": headers_out,
    "params": p.params,
    "body": body_out,
    "body_changed": body_changed,
    "context": c.m,
    "new_vars": new_vars,
}, real_out)
`
	inputData := map[string]any{
		"method":  prepared.Method,
		"url":     prepared.Url,
		"headers": prepared.Headers,
		"params":  prepared.Params,
		"body":    bodyVal,
		"context": ctx.Flat(),
	}
	inputBytes, _ := json.Marshal(inputData)

	cmd := exec.Command("python3", "-c", pythonCode, hookPath)
	cmd.Stdin = bytes.NewReader(inputBytes)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(errBuf.String()))
	}

	var outputData struct {
		Method      string            `json:"method"`
		Url         string            `json:"url"`
		Headers     map[string]string `json:"headers"`
		Params      map[string]any    `json:"params"`
		Body        any               `json:"body"`
		BodyChanged bool              `json:"body_changed"`
		Context     map[string]any    `json:"context"`
		NewVars     map[string]any    `json:"new_vars"`
	}
	if err := json.Unmarshal(outBuf.Bytes(), &outputData); err != nil {
		return nil, fmt.Errorf("invalid before hook output: %w", err)
	}
	if outputData.Method != "" {
		prepared.Method = outputData.Method
	}
	if outputData.Url != "" {
		prepared.Url = outputData.Url
	}
	if outputData.Headers != nil {
		prepared.Headers = outputData.Headers
	}
	if outputData.Params != nil {
		prepared.Params = outputData.Params
	}
	if outputData.BodyChanged {
		switch b := outputData.Body.(type) {
		case nil:
			prepared.Content = nil
			prepared.BodyPreview = ""
		case string:
			prepared.Content = []byte(b)
			prepared.BodyPreview = b
		default:
			if jb, err := json.Marshal(b); err == nil {
				prepared.Content = jb
				prepared.BodyPreview = string(jb)
				if _, hasCT := prepared.Headers["Content-Type"]; !hasCT {
					prepared.Headers["Content-Type"] = "application/json"
				}
			}
		}
	}
	for k, v := range outputData.Context {
		ctx.Set(k, v)
	}
	return outputData.NewVars, nil
}

func runAfterHook(hookPath string, result *types.Result, ctx *templating.Context) (assertions.TestResult, map[string]any) {
	testName := fmt.Sprintf("after hook %s", filepath.Base(hookPath))
	capturesUpdate := make(map[string]any)

	if !strings.HasSuffix(hookPath, ".py") {
		return assertions.TestResult{Name: testName, Passed: true}, capturesUpdate
	}

	pythonCode := `
import sys, json, inspect, importlib.util
real_out = sys.stdout
sys.stdout = sys.stderr
data = json.load(sys.stdin)
path = sys.argv[1]
spec = importlib.util.spec_from_file_location("hook", path)
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)
class Ctx:
    def __init__(self, m):
        self.orig = dict(m)
        self.curr = dict(m)
        self.vars = self.curr
    def get(self, k, d=None): return self.curr.get(k, d)
    def set(self, k, v): self.curr[k] = v
    def __getitem__(self, k): return self.curr[k]
    def __setitem__(self, k, v): self.curr[k] = v
c = Ctx(data.get("context") or {})
class Res:
    def __init__(self):
        self.status = data["status"]
        self.headers = data["headers"]
        self.json = data["json"]
        self.text = data["text"]
        self.body = self.json if self.json is not None else self.text
        self.elapsed_ms = data["elapsed_ms"]
        self.vars = c.curr
    def get(self, k, d=None): return c.get(k, d)
    def set(self, k, v): c.set(k, v)
r = Res()
fn = getattr(mod, "after", None) or getattr(mod, "run", None)
if fn is not None:
    try:
        sig = inspect.signature(fn)
        if len(sig.parameters) == 0:
            fn()
        elif len(sig.parameters) == 1:
            fn(r)
        else:
            fn(r, c)
        new_vars = {k: v for k, v in c.curr.items() if k not in c.orig or c.orig[k] != v}
        json.dump({"ok": True, "new_vars": new_vars}, real_out)
    except AssertionError as exc:
        json.dump({"ok": False, "error": str(exc) or "assertion failed"}, real_out)
    except Exception as exc:
        json.dump({"ok": False, "error": f"{exc.__class__.__name__}: {exc}"}, real_out)
else:
    json.dump({"ok": True, "new_vars": {}}, real_out)
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
