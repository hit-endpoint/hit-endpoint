package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"hit/internal/flows"
	"hit/internal/perf"
	"hit/internal/runner"
	"hit/internal/sanity"
	"hit/internal/spec"
	"hit/internal/types"
	"hit/internal/zone"
)

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema *JSONSchema `json:"inputSchema"`
}

type JSONSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type CallToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

func ServeStdio(in io.Reader, out io.Writer, defaultZone string) error {
	reader := bufio.NewReader(in)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Handle Content-Length header if client uses header framing
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			parts := strings.SplitN(line, ":", 2)
			length, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err == nil && length > 0 {
				_, _ = reader.ReadString('\n') // read trailing empty line
				buf := make([]byte, length)
				if _, err := io.ReadFull(reader, buf); err != nil {
					return err
				}
				line = strings.TrimSpace(string(buf))
			}
		}

		var req JSONRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		resp := handleRequest(&req, defaultZone)
		if resp != nil {
			b, _ := json.Marshal(resp)
			fmt.Fprintf(out, "%s\n", string(b))
		}
	}
}

func handleRequest(req *JSONRPCRequest, defaultZone string) *JSONRPCResponse {
	isNotification := req.ID == nil

	switch req.Method {
	case "initialize":
		result := map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "hit-api-tester",
				"version": "0.1.0",
			},
		}
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}

	case "notifications/initialized", "initialized":
		return nil

	case "ping":
		if isNotification {
			return nil
		}
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}

	case "tools/list":
		if isNotification {
			return nil
		}
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": getTools()}}

	case "tools/call":
		if isNotification {
			return nil
		}
		var callParams struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(req.Params, &callParams)
		callRes := executeTool(callParams.Name, callParams.Arguments, defaultZone)
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: callRes}

	default:
		if isNotification {
			return nil
		}
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: map[string]any{
				"code":    -32601,
				"message": fmt.Sprintf("method '%s' not found", req.Method),
			},
		}
	}
}

func getTools() []Tool {
	return []Tool{
		{
			Name:        "hit_sanity",
			Description: "Run zone sanity checklist: server validation, credentials inspection, YAML syntax checking, and server connectivity ping.",
			InputSchema: &JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"zone": {Type: "string", Description: "Path to zone directory (defaults to current dir)"},
					"env":       {Type: "string", Description: "Environment name to validate against (defaults to default_environment)"},
					"offline":   {Type: "boolean", Description: "Skip live server ping check"},
				},
			},
		},
		{
			Name:        "hit_list",
			Description: "List all requests, chains, and servers discovered in the zone.",
			InputSchema: &JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"zone": {Type: "string", Description: "Path to zone directory"},
					"folder":    {Type: "string", Description: "Optional subfolder to filter requests"},
				},
			},
		},
		{
			Name:        "hit_show",
			Description: "Inspect a request specification with all variables rendered, headers resolved, and auth configured without sending.",
			InputSchema: &JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"ref":       {Type: "string", Description: "Request reference (e.g. 'pets/create' or 'auth/login')"},
					"zone": {Type: "string", Description: "Path to zone directory"},
					"env":       {Type: "string", Description: "Target environment name"},
					"curl":      {Type: "boolean", Description: "Render as executable curl command"},
					"vars":      {Type: "object", Description: "Optional key-value variable overrides"},
				},
				Required: []string{"ref"},
			},
		},
		{
			Name:        "hit_run",
			Description: "Execute an API request or flow, evaluate tests and captures, and return full execution response details.",
			InputSchema: &JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"ref":       {Type: "string", Description: "Request or flow reference (e.g. 'login-and-crud' or 'pets/list')"},
					"zone": {Type: "string", Description: "Path to zone directory"},
					"env":       {Type: "string", Description: "Target environment name"},
					"vars":      {Type: "object", Description: "Optional key-value variable overrides"},
				},
				Required: []string{"ref"},
			},
		},
		{
			Name:        "hit_perf",
			Description: "Benchmark latency and throughput for an endpoint or scenario flow, evaluating SLA/SLO thresholds.",
			InputSchema: &JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"ref":         {Type: "string", Description: "Request or flow reference to benchmark"},
					"zone":   {Type: "string", Description: "Path to zone directory"},
					"env":         {Type: "string", Description: "Target environment name"},
					"concurrency": {Type: "integer", Description: "Number of concurrent workers (default 10)"},
					"requests":    {Type: "integer", Description: "Total number of iterations to run (default 50)"},
					"threshold":   {Type: "string", Description: "SLO threshold rules (e.g. 'p95<200ms,errors<1%')"},
					"vars":        {Type: "object", Description: "Optional key-value variable overrides"},
				},
				Required: []string{"ref"},
			},
		},
		{
			Name:        "hit_vars",
			Description: "Inspect all variables in the zone, server, and captured state.",
			InputSchema: &JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"zone": {Type: "string", Description: "Path to zone directory"},
					"env":       {Type: "string", Description: "Target environment name"},
					"vars":      {Type: "object", Description: "Optional key-value variable overrides"},
				},
			},
		},
	}
}

func executeTool(name string, args map[string]any, defaultZone string) CallToolResult {
	zPath, _ := args["zone"].(string)
	if zPath == "" {
		zPath = defaultZone
	}

	z, err := zone.Find(zPath)
	if err != nil {
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("error finding zone: %v", err)}},
			IsError: true,
		}
	}

	serverName, _ := args["server"].(string)
	if serverName == "" {
		serverName, _ = args["env"].(string)
	}

	extraVars := make(map[string]any)
	if vMap, ok := args["vars"].(map[string]any); ok {
		for k, v := range vMap {
			extraVars[k] = v
		}
	} else if vMap, ok := args["extra_vars"].(map[string]any); ok {
		for k, v := range vMap {
			extraVars[k] = v
		}
	}

	switch name {
	case "hit_sanity":
		offline, _ := args["offline"].(bool)
		rep := sanity.RunSanity(z, serverName, "", !offline, 5.0)
		b, _ := json.MarshalIndent(rep, "", "  ")
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: string(b)}},
			IsError: !rep.Ready,
		}

	case "hit_list":
		base := z.CollectionsDir()
		folder, _ := args["folder"].(string)
		if folder != "" {
			if r, err := z.ResolveRequest(folder); err == nil {
				base = r
			}
		}

		var reqList []map[string]any
		for _, path := range z.ListRequests(base) {
			sp, err := spec.LoadSpec(path, nil)
			if err == nil {
				reqList = append(reqList, map[string]any{
					"ref":    z.RequestRef(path),
					"name":   sp.Name,
					"method": sp.Method,
					"url":    sp.Url,
				})
			}
		}

		var flowList []string
		for _, f := range z.ListChains() {
			rel, _ := filepath.Rel(z.ChainsDir(), f)
			flowList = append(flowList, strings.TrimSuffix(rel, filepath.Ext(rel)))
		}

		data := map[string]any{
			"zone":         z.Name(),
			"servers":      z.ServerNames(),
			"requests":     reqList,
			"chains":       flowList,
		}
		b, _ := json.MarshalIndent(data, "", "  ")
		return CallToolResult{Content: []ContentItem{{Type: "text", Text: string(b)}}}

	case "hit_show":
		ref, _ := args["ref"].(string)
		curlMode, _ := args["curl"].(bool)

		sess, err := runner.NewSession(runner.SessionOptions{
			Zone: z,
			ServerName: serverName,
			ExtraVars: extraVars,
			Persist:   false,
		})
		if err != nil {
			return CallToolResult{Content: []ContentItem{{Type: "text", Text: err.Error()}}, IsError: true}
		}
		defer sess.Close()

		sp, err := sess.Load(ref)
		if err != nil {
			return CallToolResult{Content: []ContentItem{{Type: "text", Text: err.Error()}}, IsError: true}
		}

		_, prepared, ctx, err := sess.Prepare(sp, nil)
		if err != nil {
			return CallToolResult{Content: []ContentItem{{Type: "text", Text: err.Error()}}, IsError: true}
		}

		if curlMode {
			return CallToolResult{Content: []ContentItem{{Type: "text", Text: prepared.ToCurl(ctx)}}}
		}

		dict := map[string]any{
			"name":    sp.Name,
			"method":  prepared.Method,
			"url":     prepared.FullURL(),
			"headers": prepared.Headers,
			"body":    prepared.BodyPreview,
		}
		b, _ := json.MarshalIndent(dict, "", "  ")
		return CallToolResult{Content: []ContentItem{{Type: "text", Text: string(b)}}}

	case "hit_run":
		ref, _ := args["ref"].(string)
		sess, err := runner.NewSession(runner.SessionOptions{
			Zone: z,
			ServerName: serverName,
			ExtraVars: extraVars,
			Persist:   true,
		})
		if err != nil {
			return CallToolResult{Content: []ContentItem{{Type: "text", Text: err.Error()}}, IsError: true}
		}
		defer sess.Close()

		flowPath, err := z.ResolveChain(ref)
		if err == nil && flowPath != "" {
			flowData, err := flows.LoadFlow(flowPath)
			if err != nil {
				return CallToolResult{Content: []ContentItem{{Type: "text", Text: err.Error()}}, IsError: true}
			}
			fr := flows.RunFlow(sess, flowData, nil, nil, false, 0)
			var list []any
			for _, r := range fr.Results {
				list = append(list, r.ToDict())
			}
			data := map[string]any{
				"flow":    fr.Name,
				"ok":      fr.OK(),
				"results": list,
				"error":   fr.Error,
			}
			b, _ := json.MarshalIndent(data, "", "  ")
			return CallToolResult{Content: []ContentItem{{Type: "text", Text: string(b)}}, IsError: !fr.OK()}
		}

		sp, err := sess.Load(ref)
		if err != nil {
			return CallToolResult{Content: []ContentItem{{Type: "text", Text: err.Error()}}, IsError: true}
		}
		res := sess.RunSpec(sp, nil)
		b, _ := json.MarshalIndent(res.ToDict(), "", "  ")
		return CallToolResult{Content: []ContentItem{{Type: "text", Text: string(b)}}, IsError: !res.OK()}

	case "hit_perf":
		ref, _ := args["ref"].(string)
		concurrency := 10
		if c, ok := args["concurrency"].(float64); ok && c > 0 {
			concurrency = int(c)
		}
		requests := 50
		if r, ok := args["requests"].(float64); ok && r > 0 {
			requests = int(r)
		}
		thresholdStr, _ := args["threshold"].(string)

		opts := perf.PerfOptions{
			Concurrency: concurrency,
			Total:       requests,
		}

		sess, err := runner.NewSession(runner.SessionOptions{
			Zone: z,
			ServerName: serverName,
			ExtraVars: extraVars,
			Persist:   false,
		})
		if err != nil {
			return CallToolResult{Content: []ContentItem{{Type: "text", Text: err.Error()}}, IsError: true}
		}
		defer sess.Close()

		var rep *types.PerfReport
		flowPath, flowErr := z.ResolveChain(ref)
		if flowErr == nil && flowPath != "" {
			flowData, err := flows.LoadFlow(flowPath)
			if err != nil {
				return CallToolResult{Content: []ContentItem{{Type: "text", Text: err.Error()}}, IsError: true}
			}
			rep, err = perf.RunFlowPerf(sess, ref, flowData, opts)
			if err != nil {
				return CallToolResult{Content: []ContentItem{{Type: "text", Text: err.Error()}}, IsError: true}
			}
		} else {
			sp, err := sess.Load(ref)
			if err != nil {
				return CallToolResult{Content: []ContentItem{{Type: "text", Text: err.Error()}}, IsError: true}
			}
			rep, err = perf.RunPerf(sess, sp, opts)
			if err != nil {
				return CallToolResult{Content: []ContentItem{{Type: "text", Text: err.Error()}}, IsError: true}
			}
		}

		dict := rep.ToDict()
		allPassed := true
		if thresholdStr != "" {
			rules, err := perf.ParseThresholds(thresholdStr)
			if err != nil {
				return CallToolResult{Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("invalid threshold: %v", err)}}, IsError: true}
			}
			evalResults := perf.EvaluateThresholds(rep, rules)
			var trList []map[string]any
			for _, tr := range evalResults {
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
		return CallToolResult{Content: []ContentItem{{Type: "text", Text: string(b)}}, IsError: !allPassed || rep.Failed > 0}

	case "hit_vars":
		sess, err := runner.NewSession(runner.SessionOptions{
			Zone: z,
			ServerName: serverName,
			ExtraVars: extraVars,
			Persist:   false,
		})
		if err != nil {
			return CallToolResult{Content: []ContentItem{{Type: "text", Text: err.Error()}}, IsError: true}
		}
		defer sess.Close()
		vars := sess.Variables()
		b, _ := json.MarshalIndent(vars, "", "  ")
		return CallToolResult{Content: []ContentItem{{Type: "text", Text: string(b)}}}

	default:
		return CallToolResult{Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("unknown tool: %s", name)}}, IsError: true}
	}
}
