package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPServerHandshakeAndTools(t *testing.T) {
	wsDir, err := filepath.Abs("../../examples/petstore-zone")
	if err != nil {
		t.Fatalf("failed to get ws path: %v", err)
	}

	// 1. Test initialize
	initReq := `{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {}}` + "\n"
	// 2. Test tools/list
	toolsReq := `{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}` + "\n"
	// 3. Test tools/call hit_list
	callReq := fmt.Sprintf(`{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "hit_list", "arguments": {"zone": "%s"}}}`, wsDir) + "\n"

	in := bytes.NewBufferString(initReq + toolsReq + callReq)
	var out bytes.Buffer

	err = ServeStdio(in, &out, wsDir)
	if err != nil {
		t.Fatalf("ServeStdio error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected 3 responses, got %d. Output: %s", len(lines), out.String())
	}

	// Check response 1 (initialize)
	var resp1 JSONRPCResponse
	if err := json.Unmarshal([]byte(lines[0]), &resp1); err != nil {
		t.Fatalf("failed to parse init resp: %v", err)
	}
	resMap, ok := resp1.Result.(map[string]any)
	if !ok || resMap["protocolVersion"] != "2024-11-05" {
		t.Errorf("unexpected init result: %+v", resp1.Result)
	}

	// Check response 2 (tools/list)
	var resp2 JSONRPCResponse
	if err := json.Unmarshal([]byte(lines[1]), &resp2); err != nil {
		t.Fatalf("failed to parse tools resp: %v", err)
	}
	toolsMap, ok := resp2.Result.(map[string]any)
	toolsList, _ := toolsMap["tools"].([]any)
	if !ok || len(toolsList) < 6 {
		t.Errorf("expected at least 6 tools, got %d", len(toolsList))
	}

	// Check response 3 (hit_list call)
	var resp3 JSONRPCResponse
	if err := json.Unmarshal([]byte(lines[2]), &resp3); err != nil {
		t.Fatalf("failed to parse call resp: %v", err)
	}
	callMap, ok := resp3.Result.(map[string]any)
	contentList, _ := callMap["content"].([]any)
	if !ok || len(contentList) == 0 {
		t.Fatalf("expected content in tool call: %+v", resp3)
	}
}
