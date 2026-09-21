package snippet

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// RequestInfo contains normalized request details for code snippet generation.
type RequestInfo struct {
	Method      string
	URL         string
	Headers     map[string]string
	Query       map[string]string
	Body        []byte
	ExtractPath string // Optional response element path, e.g. "items[0].id"
}

// GenerateSnippet produces runnable source code in the specified programming language.
func GenerateSnippet(lang string, req RequestInfo) (string, error) {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "python", "py":
		return GeneratePython(req), nil
	case "javascript", "js", "node":
		return GenerateJavaScript(req), nil
	case "php":
		return GeneratePHP(req), nil
	case "go", "golang":
		return GenerateGo(req), nil
	default:
		return "", fmt.Errorf("unsupported language %q: choose from python, javascript, php, go", lang)
	}
}

// GenerateAll produces snippets for all four supported languages separated by headers.
func GenerateAll(req RequestInfo) string {
	var sb strings.Builder
	sb.WriteString("# 🐍 Python (requests)\n")
	sb.WriteString(GeneratePython(req))
	sb.WriteString("\n\n// 🟨 JavaScript (fetch)\n")
	sb.WriteString(GenerateJavaScript(req))
	sb.WriteString("\n\n// 🐘 PHP (cURL)\n")
	sb.WriteString(GeneratePHP(req))
	sb.WriteString("\n\n// 🐹 Go (net/http)\n")
	sb.WriteString(GenerateGo(req))
	return sb.String()
}

// GeneratePython builds a Python snippet using the requests library.
func GeneratePython(req RequestInfo) string {
	var sb strings.Builder
	sb.WriteString("import requests\n\n")

	// URL & Query
	fullURL := req.FullURL()
	sb.WriteString(fmt.Sprintf("url = %q\n", fullURL))

	// Headers
	if len(req.Headers) > 0 {
		sb.WriteString("headers = {\n")
		var keys []string
		for k := range req.Headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sb.WriteString(fmt.Sprintf("    %q: %q,\n", k, req.Headers[k]))
		}
		sb.WriteString("}\n")
	}

	// Payload
	hasJSON := false
	if len(req.Body) > 0 {
		var jsonObj any
		if err := json.Unmarshal(req.Body, &jsonObj); err == nil {
			hasJSON = true
			prettyJSON, _ := json.MarshalIndent(jsonObj, "", "    ")
			pyBody := strings.ReplaceAll(string(prettyJSON), ": true", ": True")
			pyBody = strings.ReplaceAll(pyBody, ": false", ": False")
			pyBody = strings.ReplaceAll(pyBody, ": null", ": None")
			sb.WriteString(fmt.Sprintf("payload = %s\n\n", pyBody))
		} else {
			sb.WriteString(fmt.Sprintf("payload = %q\n\n", string(req.Body)))
		}
	} else {
		sb.WriteString("\n")
	}

	// Request execution
	methodLower := strings.ToLower(req.Method)
	args := []string{"url"}
	if len(req.Headers) > 0 {
		args = append(args, "headers=headers")
	}
	if hasJSON {
		args = append(args, "json=payload")
	} else if len(req.Body) > 0 {
		args = append(args, "data=payload")
	}

	if methodLower == "get" || methodLower == "post" || methodLower == "put" || methodLower == "delete" || methodLower == "patch" {
		sb.WriteString(fmt.Sprintf("response = requests.%s(%s)\n\n", methodLower, strings.Join(args, ", ")))
	} else {
		args = append([]string{fmt.Sprintf("%q", req.Method)}, args...)
		sb.WriteString(fmt.Sprintf("response = requests.request(%s)\n\n", strings.Join(args, ", ")))
	}

	// Response Inspection Code Block
	sb.WriteString("# --- Accessing Response Elements ---\n")
	sb.WriteString("status_code = response.status_code\n")
	sb.WriteString("duration_ms = response.elapsed.total_seconds() * 1000\n")
	sb.WriteString("response_headers = response.headers\n")
	sb.WriteString("print(f\"Status: {status_code} ({duration_ms:.1f}ms)\")\n\n")
	sb.WriteString("try:\n")
	sb.WriteString("    data = response.json()\n")
	if req.ExtractPath != "" {
		pyAccess := formatPythonAccessor("data", req.ExtractPath)
		sb.WriteString(fmt.Sprintf("    # Access extracted field: %s\n", req.ExtractPath))
		sb.WriteString(fmt.Sprintf("    extracted_value = %s\n", pyAccess))
		sb.WriteString("    print(f\"Extracted: {extracted_value}\")\n")
	} else {
		sb.WriteString("    print(\"JSON Response:\", data)\n")
	}
	sb.WriteString("except Exception:\n")
	sb.WriteString("    print(\"Raw Response:\", response.text)\n")

	return sb.String()
}

// GenerateJavaScript builds a modern JavaScript / Node.js snippet using fetch.
func GenerateJavaScript(req RequestInfo) string {
	var sb strings.Builder
	fullURL := req.FullURL()

	sb.WriteString(fmt.Sprintf("const url = %q;\n", fullURL))
	sb.WriteString("const options = {\n")
	sb.WriteString(fmt.Sprintf("  method: %q,\n", req.Method))

	if len(req.Headers) > 0 {
		sb.WriteString("  headers: {\n")
		var keys []string
		for k := range req.Headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sb.WriteString(fmt.Sprintf("    %q: %q,\n", k, req.Headers[k]))
		}
		sb.WriteString("  },\n")
	}

	if len(req.Body) > 0 {
		var jsonObj any
		if err := json.Unmarshal(req.Body, &jsonObj); err == nil {
			prettyJSON, _ := json.MarshalIndent(jsonObj, "  ", "  ")
			sb.WriteString(fmt.Sprintf("  body: JSON.stringify(%s),\n", string(prettyJSON)))
		} else {
			sb.WriteString(fmt.Sprintf("  body: %q,\n", string(req.Body)))
		}
	}

	sb.WriteString("};\n\n")

	sb.WriteString("const startTime = performance.now();\n")
	sb.WriteString("const response = await fetch(url, options);\n")
	sb.WriteString("const durationMs = performance.now() - startTime;\n\n")

	sb.WriteString("// --- Accessing Response Elements ---\n")
	sb.WriteString("const statusCode = response.status;\n")
	sb.WriteString("const headers = Object.fromEntries(response.headers.entries());\n")
	sb.WriteString("console.log(`Status: ${statusCode} (${durationMs.toFixed(1)}ms)`);\n\n")

	sb.WriteString("try {\n")
	sb.WriteString("  const data = await response.json();\n")
	if req.ExtractPath != "" {
		jsAccess := formatJSAccessor("data", req.ExtractPath)
		sb.WriteString(fmt.Sprintf("  // Access extracted field: %s\n", req.ExtractPath))
		sb.WriteString(fmt.Sprintf("  const extractedValue = %s;\n", jsAccess))
		sb.WriteString("  console.log('Extracted:', extractedValue);\n")
	} else {
		sb.WriteString("  console.log('JSON Response:', data);\n")
	}
	sb.WriteString("} catch (err) {\n")
	sb.WriteString("  const rawText = await response.text();\n")
	sb.WriteString("  console.log('Raw Response:', rawText);\n")
	sb.WriteString("}\n")

	return sb.String()
}

// GeneratePHP builds an idiomatic PHP snippet using cURL and json_decode.
func GeneratePHP(req RequestInfo) string {
	var sb strings.Builder
	fullURL := req.FullURL()

	sb.WriteString("<?php\n\n")
	sb.WriteString(fmt.Sprintf("$ch = curl_init(%q);\n\n", fullURL))

	// Options array
	sb.WriteString("$headers = [\n")
	var keys []string
	for k := range req.Headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sb.WriteString(fmt.Sprintf("    %q,\n", fmt.Sprintf("%s: %s", k, req.Headers[k])))
	}
	sb.WriteString("];\n\n")

	sb.WriteString("curl_setopt_array($ch, [\n")
	sb.WriteString("    CURLOPT_RETURNTRANSFER => true,\n")
	sb.WriteString(fmt.Sprintf("    CURLOPT_CUSTOMREQUEST => %q,\n", req.Method))
	sb.WriteString("    CURLOPT_HTTPHEADER => $headers,\n")

	if len(req.Body) > 0 {
		var jsonObj any
		if err := json.Unmarshal(req.Body, &jsonObj); err == nil {
			sb.WriteString(fmt.Sprintf("    CURLOPT_POSTFIELDS => json_encode(%s),\n", formatPHPArray(jsonObj, 1)))
		} else {
			sb.WriteString(fmt.Sprintf("    CURLOPT_POSTFIELDS => %q,\n", string(req.Body)))
		}
	}
	sb.WriteString("]);\n\n")

	sb.WriteString("$startTime = microtime(true);\n")
	sb.WriteString("$response = curl_exec($ch);\n")
	sb.WriteString("$durationMs = (microtime(true) - $startTime) * 1000;\n")
	sb.WriteString("$statusCode = curl_getinfo($ch, CURLINFO_HTTP_CODE);\n")
	sb.WriteString("curl_close($ch);\n\n")

	sb.WriteString("// --- Accessing Response Elements ---\n")
	sb.WriteString("echo \"Status: {$statusCode} (\" . round($durationMs, 1) . \"ms)\\n\";\n\n")

	sb.WriteString("$data = json_decode($response, true);\n")
	sb.WriteString("if (json_last_error() === JSON_ERROR_NONE) {\n")
	if req.ExtractPath != "" {
		phpAccess := formatPHPAccessor("$data", req.ExtractPath)
		sb.WriteString(fmt.Sprintf("    // Access extracted field: %s\n", req.ExtractPath))
		sb.WriteString(fmt.Sprintf("    $extractedValue = %s;\n", phpAccess))
		sb.WriteString("    echo \"Extracted: \" . json_encode($extractedValue) . \"\\n\";\n")
	} else {
		sb.WriteString("    print_r($data);\n")
	}
	sb.WriteString("} else {\n")
	sb.WriteString("    echo \"Raw Response: {$response}\\n\";\n")
	sb.WriteString("}\n")

	return sb.String()
}

// GenerateGo builds an idiomatic Go snippet using net/http.
func GenerateGo(req RequestInfo) string {
	var sb strings.Builder
	fullURL := req.FullURL()

	sb.WriteString("package main\n\n")
	sb.WriteString("import (\n")
	if len(req.Body) > 0 {
		sb.WriteString("\t\"bytes\"\n")
	}
	sb.WriteString("\t\"encoding/json\"\n")
	sb.WriteString("\t\"fmt\"\n")
	sb.WriteString("\t\"io\"\n")
	sb.WriteString("\t\"net/http\"\n")
	sb.WriteString("\t\"time\"\n")
	sb.WriteString(")\n\n")

	sb.WriteString("func main() {\n")
	sb.WriteString(fmt.Sprintf("\turl := %q\n", fullURL))

	bodyArg := "nil"
	if len(req.Body) > 0 {
		var jsonObj any
		if err := json.Unmarshal(req.Body, &jsonObj); err == nil {
			compactJSON, _ := json.Marshal(jsonObj)
			sb.WriteString(fmt.Sprintf("\tpayload := []byte(%q)\n", string(compactJSON)))
		} else {
			sb.WriteString(fmt.Sprintf("\tpayload := []byte(%q)\n", string(req.Body)))
		}
		bodyArg = "bytes.NewBuffer(payload)"
	}

	sb.WriteString(fmt.Sprintf("\treq, err := http.NewRequest(%q, url, %s)\n", req.Method, bodyArg))
	sb.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n\n")

	var keys []string
	for k := range req.Headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sb.WriteString(fmt.Sprintf("\treq.Header.Set(%q, %q)\n", k, req.Headers[k]))
	}

	sb.WriteString("\n\tclient := &http.Client{}\n")
	sb.WriteString("\tstart := time.Now()\n")
	sb.WriteString("\tresp, err := client.Do(req)\n")
	sb.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n")
	sb.WriteString("\tdefer resp.Body.Close()\n")
	sb.WriteString("\telapsed := time.Since(start)\n\n")

	sb.WriteString("\tbodyBytes, err := io.ReadAll(resp.Body)\n")
	sb.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n\n")

	sb.WriteString("\t// --- Accessing Response Elements ---\n")
	sb.WriteString("\tstatusCode := resp.StatusCode\n")
	sb.WriteString("\tfmt.Printf(\"Status: %d (%v)\\n\", statusCode, elapsed)\n\n")

	sb.WriteString("\tvar data any\n")
	sb.WriteString("\tif err := json.Unmarshal(bodyBytes, &data); err == nil {\n")
	if req.ExtractPath != "" {
		goAccess := formatGoAccessor("data", req.ExtractPath)
		sb.WriteString(fmt.Sprintf("\t\t// Access extracted field: %s\n", req.ExtractPath))
		sb.WriteString(fmt.Sprintf("\t\textractedValue := %s\n", goAccess))
		sb.WriteString("\t\tfmt.Printf(\"Extracted: %v\\n\", extractedValue)\n")
	} else {
		sb.WriteString("\t\tfmt.Printf(\"JSON Response: %+v\\n\", data)\n")
	}
	sb.WriteString("\t} else {\n")
	sb.WriteString("\t\tfmt.Printf(\"Raw Response: %s\\n\", string(bodyBytes))\n")
	sb.WriteString("\t}\n")
	sb.WriteString("}\n")

	return sb.String()
}

// FullURL appends query parameters to the URL if not already present.
func (r *RequestInfo) FullURL() string {
	u := r.URL
	if len(r.Query) == 0 {
		return u
	}
	separator := "?"
	if strings.Contains(u, "?") {
		separator = "&"
	}
	var qPairs []string
	var keys []string
	for k := range r.Query {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		qPairs = append(qPairs, fmt.Sprintf("%s=%s", k, r.Query[k]))
	}
	return u + separator + strings.Join(qPairs, "&")
}

// Path accessor formatters for each language
func parsePathSegments(path string) []string {
	// Cleans "json.items[0].id" or "items[0].id" into ["items", "[0]", "id"]
	clean := strings.TrimPrefix(path, "json.")
	clean = strings.TrimPrefix(clean, "body.")

	re := regexp.MustCompile(`([a-zA-Z0-9_-]+|\[[0-9]+\])`)
	matches := re.FindAllString(clean, -1)
	return matches
}

func formatPythonAccessor(root, path string) string {
	segments := parsePathSegments(path)
	if len(segments) == 0 {
		return root
	}
	var sb strings.Builder
	sb.WriteString(root)
	for _, seg := range segments {
		if strings.HasPrefix(seg, "[") && strings.HasSuffix(seg, "]") {
			sb.WriteString(seg)
		} else {
			sb.WriteString(fmt.Sprintf("[%q]", seg))
		}
	}
	return sb.String()
}

func formatJSAccessor(root, path string) string {
	segments := parsePathSegments(path)
	if len(segments) == 0 {
		return root
	}
	var sb strings.Builder
	sb.WriteString(root)
	for _, seg := range segments {
		if strings.HasPrefix(seg, "[") && strings.HasSuffix(seg, "]") {
			sb.WriteString(seg)
		} else {
			sb.WriteString(fmt.Sprintf(".%s", seg))
		}
	}
	return sb.String()
}

func formatPHPAccessor(root, path string) string {
	segments := parsePathSegments(path)
	if len(segments) == 0 {
		return root
	}
	var sb strings.Builder
	sb.WriteString(root)
	for _, seg := range segments {
		if strings.HasPrefix(seg, "[") && strings.HasSuffix(seg, "]") {
			idx := strings.Trim(seg, "[]")
			sb.WriteString(fmt.Sprintf("[%s]", idx))
		} else {
			sb.WriteString(fmt.Sprintf("[%q]", seg))
		}
	}
	sb.WriteString(" ?? null")
	return sb.String()
}

func formatGoAccessor(root, path string) string {
	segments := parsePathSegments(path)
	if len(segments) == 0 {
		return root
	}
	var current = root
	for _, seg := range segments {
		if strings.HasPrefix(seg, "[") && strings.HasSuffix(seg, "]") {
			idxStr := strings.Trim(seg, "[]")
			current = fmt.Sprintf("%s.([]any)[%s]", current, idxStr)
		} else {
			current = fmt.Sprintf("%s.(map[string]any)[%q]", current, seg)
		}
	}
	return current
}

func formatPHPArray(val any, indent int) string {
	spaces := strings.Repeat("    ", indent)
	switch v := val.(type) {
	case map[string]any:
		var lines []string
		var keys []string
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			lines = append(lines, fmt.Sprintf("%s    %q => %s", spaces, k, formatPHPArray(v[k], indent+1)))
		}
		return fmt.Sprintf("[\n%s\n%s]", strings.Join(lines, ",\n"), spaces)
	case []any:
		var items []string
		for _, item := range v {
			items = append(items, formatPHPArray(item, indent+1))
		}
		return fmt.Sprintf("[%s]", strings.Join(items, ", "))
	case string:
		return fmt.Sprintf("%q", v)
	case float64:
		if v == float64(int(v)) {
			return strconv.Itoa(int(v))
		}
		return fmt.Sprintf("%v", v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%v", v)
	}
}
