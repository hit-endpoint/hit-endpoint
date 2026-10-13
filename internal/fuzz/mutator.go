package fuzz

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Mutation represents a mutated request variant designed to test API resilience.
type Mutation struct {
	Category    string            `json:"category"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Method      string            `json:"method"`
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers"`
	Body        []byte            `json:"body"`
}

// GenerateMutations synthesizes mutations across types, boundaries, strings, injection, nulls, and headers.
func GenerateMutations(method, rawURL string, headers map[string]string, query map[string]string, body []byte, categories []string) []Mutation {
	var results []Mutation

	mergedQuery := make(map[string]string)
	if u, err := url.Parse(rawURL); err == nil {
		for k, vals := range u.Query() {
			if len(vals) > 0 {
				mergedQuery[k] = vals[0]
			}
		}
	}
	for k, v := range query {
		mergedQuery[k] = v
	}
	query = mergedQuery

	catMap := make(map[string]bool)
	for _, c := range categories {
		catMap[strings.ToLower(strings.TrimSpace(c))] = true
	}
	allCats := len(catMap) == 0

	// Parse body if JSON
	var jsonBody any
	isJSON := false
	if len(body) > 0 {
		if err := json.Unmarshal(body, &jsonBody); err == nil {
			isJSON = true
		}
	}

	// 1. Boundary Mutations
	if allCats || catMap["boundaries"] {
		results = append(results, generateBoundaryMutations(method, rawURL, headers, body, jsonBody, isJSON)...)
	}

	// 2. Type Confusion Mutations
	if allCats || catMap["types"] {
		results = append(results, generateTypeMutations(method, rawURL, headers, body, jsonBody, isJSON)...)
	}

	// 3. Extreme String & Buffer Overflow Mutations
	if allCats || catMap["strings"] {
		results = append(results, generateStringMutations(method, rawURL, headers, query, body, jsonBody, isJSON)...)
	}

	// 4. Injection Probes (SQLi, XSS, Path Traversal, Command Injection)
	if allCats || catMap["injection"] {
		results = append(results, generateInjectionMutations(method, rawURL, headers, query, body, jsonBody, isJSON)...)
	}

	// 5. Null & Structure Mutations
	if allCats || catMap["nulls"] {
		results = append(results, generateNullMutations(method, rawURL, headers, jsonBody, isJSON)...)
	}

	// 6. Header & Protocol Mutations
	if allCats || catMap["headers"] {
		results = append(results, generateHeaderMutations(method, rawURL, headers, body)...)
	}

	return results
}

func cloneHeaders(h map[string]string) map[string]string {
	c := make(map[string]string, len(h))
	for k, v := range h {
		c[k] = v
	}
	return c
}

func deepCopyJSON(val any) any {
	if val == nil {
		return nil
	}
	b, err := json.Marshal(val)
	if err != nil {
		return val
	}
	var res any
	_ = json.Unmarshal(b, &res)
	return res
}

func findPathParamSegment(rawURL string) (segIdx int, segVal string, paramLabel string, isNumeric bool, ok bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Path == "" || u.Path == "/" {
		return 0, "", "", false, false
	}
	trimmed := strings.Trim(u.Path, "/")
	if trimmed == "" {
		return 0, "", "", false, false
	}
	segments := strings.Split(trimmed, "/")

	for i := len(segments) - 1; i >= 0; i-- {
		seg := segments[i]
		if seg == "" {
			continue
		}
		lower := strings.ToLower(seg)
		if lower == "api" || (strings.HasPrefix(lower, "v") && len(lower) <= 3) {
			continue
		}
		if _, err := strconv.Atoi(seg); err == nil {
			label := "/{id}"
			if i > 0 {
				label = fmt.Sprintf("/%s/{id}", segments[i-1])
			}
			return i, seg, label, true, true
		}
		hasDigit := false
		for _, r := range seg {
			if r >= '0' && r <= '9' {
				hasDigit = true
				break
			}
		}
		if hasDigit || strings.HasPrefix(seg, "{") || strings.HasPrefix(seg, ":") {
			label := "/{param}"
			if i > 0 {
				label = fmt.Sprintf("/%s/{param}", segments[i-1])
			}
			return i, seg, label, false, true
		}
	}

	if len(segments) >= 2 {
		lastIdx := len(segments) - 1
		last := strings.ToLower(segments[lastIdx])
		switch last {
		case "login", "logout", "health", "status", "echo", "slow", "register", "refresh", "token":
		default:
			label := fmt.Sprintf("/%s/{param}", segments[lastIdx-1])
			return lastIdx, segments[lastIdx], label, false, true
		}
	}

	return 0, "", "", false, false
}

func mutateURLPathSegment(rawURL string, segIdx int, replacement string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	trimmed := strings.Trim(u.Path, "/")
	segments := strings.Split(trimmed, "/")
	if segIdx < 0 || segIdx >= len(segments) {
		return rawURL
	}
	rawSegments := make([]string, len(segments))
	for i, s := range segments {
		rawSegments[i] = url.PathEscape(s)
	}
	segments[segIdx] = replacement
	rawSegments[segIdx] = url.PathEscape(replacement)
	u.Path = "/" + strings.Join(segments, "/")
	u.RawPath = "/" + strings.Join(rawSegments, "/")
	return u.String()
}

func generateBoundaryMutations(method, rawURL string, headers map[string]string, body []byte, jsonBody any, isJSON bool) []Mutation {
	var mutations []Mutation

	boundaries := []struct {
		name string
		val  any
	}{
		{"Integer MaxInt32 (2147483647)", 2147483647},
		{"Integer MinInt32 (-2147483648)", -2147483648},
		{"Integer Zero (0)", 0},
		{"Integer Negative One (-1)", -1},
		{"Integer MaxInt64 (9223372036854775807)", 9223372036854775807},
		{"Float Overflow (1e308)", 1e308},
	}

	if isJSON {
		if obj, ok := jsonBody.(map[string]any); ok {
			for key, currentVal := range obj {
				// If key or current value looks numeric
				isNum := false
				switch currentVal.(type) {
				case float64, int, int64:
					isNum = true
				}
				if isNum || strings.Contains(strings.ToLower(key), "id") || strings.Contains(strings.ToLower(key), "age") || strings.Contains(strings.ToLower(key), "count") {
					for _, b := range boundaries {
						mutated := deepCopyJSON(obj).(map[string]any)
						mutated[key] = b.val
						bBytes, _ := json.Marshal(mutated)
						mutations = append(mutations, Mutation{
							Category:    "boundaries",
							Name:        fmt.Sprintf("Boundary on %q: %s", key, b.name),
							Description: fmt.Sprintf("Tests server numeric overflow and boundary handling on property %q", key),
							Method:      method,
							URL:         rawURL,
							Headers:     cloneHeaders(headers),
							Body:        bBytes,
						})
					}
					break // one numeric field is enough to test
				}
			}
		}
	}

	if segIdx, _, paramLabel, _, ok := findPathParamSegment(rawURL); ok {
		for _, b := range boundaries {
			valStr := fmt.Sprintf("%v", b.val)
			mutURL := mutateURLPathSegment(rawURL, segIdx, valStr)
			mutations = append(mutations, Mutation{
				Category:    "boundaries",
				Name:        fmt.Sprintf("Path boundary on %s: %s", paramLabel, b.name),
				Description: fmt.Sprintf("Tests URL path parameter boundary handling on %s", paramLabel),
				Method:      method,
				URL:         mutURL,
				Headers:     cloneHeaders(headers),
				Body:        body,
			})
		}
	}

	return mutations
}

func generateTypeMutations(method, rawURL string, headers map[string]string, body []byte, jsonBody any, isJSON bool) []Mutation {
	var mutations []Mutation

	if isJSON {
		if obj, ok := jsonBody.(map[string]any); ok {
			for key, currentVal := range obj {
				var replacement any
				var repName string

				switch currentVal.(type) {
				case string:
					replacement = 12345
					repName = "Number where String expected"
				case float64:
					replacement = "not-a-number"
					repName = "String where Number expected"
				case bool:
					replacement = "true"
					repName = "String where Boolean expected"
				default:
					replacement = []any{"unexpected", "array"}
					repName = "Array where Object/Scalar expected"
				}

				mutated := deepCopyJSON(obj).(map[string]any)
				mutated[key] = replacement
				bBytes, _ := json.Marshal(mutated)
				mutations = append(mutations, Mutation{
					Category:    "types",
					Name:        fmt.Sprintf("Type confusion on %q: %s", key, repName),
					Description: fmt.Sprintf("Sends unexpected data type on field %q to verify schema enforcement", key),
					Method:      method,
					URL:         rawURL,
					Headers:     cloneHeaders(headers),
					Body:        bBytes,
				})
				break
			}

			// Array where Object expected for the root body
			arrayBody, _ := json.Marshal([]any{obj})
			mutations = append(mutations, Mutation{
				Category:    "types",
				Name:        "Root payload as Array instead of Object",
				Description: "Sends root JSON array to endpoints expecting a JSON object",
				Method:      method,
				URL:         rawURL,
				Headers:     cloneHeaders(headers),
				Body:        arrayBody,
			})
		}
	}

	if segIdx, _, paramLabel, isNum, ok := findPathParamSegment(rawURL); ok {
		pathTypeCases := []struct {
			name string
			val  string
		}{
			{"String where Number expected", "not-a-number"},
			{"Boolean where ID expected", "true"},
			{"Object literal in path parameter", "{}"},
		}
		if !isNum {
			pathTypeCases[0] = struct {
				name string
				val  string
			}{"Number where String expected", "12345"}
		}
		for _, tc := range pathTypeCases {
			mutURL := mutateURLPathSegment(rawURL, segIdx, tc.val)
			mutations = append(mutations, Mutation{
				Category:    "types",
				Name:        fmt.Sprintf("Path type confusion on %s: %s", paramLabel, tc.name),
				Description: fmt.Sprintf("Sends unexpected parameter type in URL path segment %s", paramLabel),
				Method:      method,
				URL:         mutURL,
				Headers:     cloneHeaders(headers),
				Body:        body,
			})
		}
	}

	return mutations
}

func isBodilessMethod(method string, body []byte) bool {
	if len(body) > 0 {
		return false
	}
	m := strings.ToUpper(strings.TrimSpace(method))
	return m == "GET" || m == "DELETE" || m == "HEAD" || m == "OPTIONS"
}

func generateStringMutations(method, rawURL string, headers map[string]string, query map[string]string, body []byte, jsonBody any, isJSON bool) []Mutation {
	var mutations []Mutation

	stringPayloads := []struct {
		name string
		val  string
	}{
		{"Buffer overflow probe (10,000 'A's)", strings.Repeat("A", 10000)},
		{"Empty string", ""},
		{"Embedded null byte (\\x00)", "valid_prefix\x00malicious_suffix"},
		{"Emoji flood", strings.Repeat("🚀💥🔥🎉", 100)},
		{"Format string specifier probe", "%s%s%s%s%s%x%x%n"},
		{"Unicode control characters", "prefix\u0000\u001f\u007fsuffix"},
	}

	if isJSON {
		if obj, ok := jsonBody.(map[string]any); ok {
			for key, currentVal := range obj {
				if _, ok := currentVal.(string); ok || true {
					for _, sp := range stringPayloads {
						mutated := deepCopyJSON(obj).(map[string]any)
						mutated[key] = sp.val
						bBytes, _ := json.Marshal(mutated)
						mutations = append(mutations, Mutation{
							Category:    "strings",
							Name:        fmt.Sprintf("String anomaly on %q: %s", key, sp.name),
							Description: fmt.Sprintf("Tests string parser resilience with extreme input on %q", key),
							Method:      method,
							URL:         rawURL,
							Headers:     cloneHeaders(headers),
							Body:        bBytes,
						})
					}
					break
				}
			}
		}
	} else if isBodilessMethod(method, body) {
		if segIdx, _, paramLabel, _, ok := findPathParamSegment(rawURL); ok {
			for _, sp := range stringPayloads {
				if sp.val == "" {
					continue
				}
				mutURL := mutateURLPathSegment(rawURL, segIdx, sp.val)
				mutations = append(mutations, Mutation{
					Category:    "strings",
					Name:        fmt.Sprintf("Path string on %s: %s", paramLabel, sp.name),
					Description: fmt.Sprintf("Tests URL path parameter parser resilience with extreme input on %s", paramLabel),
					Method:      method,
					URL:         mutURL,
					Headers:     cloneHeaders(headers),
					Body:        nil,
				})
			}
		}
		targetParam := "q"
		if len(query) > 0 {
			var qKeys []string
			for k := range query {
				qKeys = append(qKeys, k)
			}
			sort.Strings(qKeys)
			targetParam = qKeys[0]
		}
		for _, sp := range stringPayloads {
			if u, err := url.Parse(rawURL); err == nil {
				q := u.Query()
				q.Set(targetParam, sp.val)
				u.RawQuery = q.Encode()
				mutations = append(mutations, Mutation{
					Category:    "strings",
					Name:        fmt.Sprintf("Query string ?%s: %s", targetParam, sp.name),
					Description: fmt.Sprintf("Tests query string resilience with extreme input on ?%s", targetParam),
					Method:      method,
					URL:         u.String(),
					Headers:     cloneHeaders(headers),
					Body:        nil,
				})
			}
		}
	} else {
		for _, sp := range stringPayloads {
			mutations = append(mutations, Mutation{
				Category:    "strings",
				Name:        fmt.Sprintf("Raw body: %s", sp.name),
				Description: "Sends extreme raw string payload to endpoint",
				Method:      method,
				URL:         rawURL,
				Headers:     cloneHeaders(headers),
				Body:        []byte(sp.val),
			})
		}
	}

	return mutations
}

func generateInjectionMutations(method, rawURL string, headers map[string]string, query map[string]string, body []byte, jsonBody any, isJSON bool) []Mutation {
	var mutations []Mutation

	injections := []struct {
		name string
		val  string
	}{
		{"SQL Injection probe (' OR 1=1 --)", "' OR 1=1 --"},
		{"SQL Injection sleep (SLEEP(1))", "1; SELECT SLEEP(1);--"},
		{"Cross-Site Scripting (<script>alert(1)</script>)", "<script>alert(1)</script>"},
		{"XSS SVG payload", "\"><svg onload=alert(1)>"},
		{"Path Traversal (../../../../etc/passwd)", "../../../../etc/passwd"},
		{"Command Injection (; ls -la)", "; ls -la"},
	}

	// 1. In JSON body
	if isJSON {
		if obj, ok := jsonBody.(map[string]any); ok {
			for key := range obj {
				for _, inj := range injections {
					mutated := deepCopyJSON(obj).(map[string]any)
					mutated[key] = inj.val
					bBytes, _ := json.Marshal(mutated)
					mutations = append(mutations, Mutation{
						Category:    "injection",
						Name:        fmt.Sprintf("Injection probe in %q: %s", key, inj.name),
						Description: fmt.Sprintf("Tests input sanitization for %s on %q", inj.name, key),
						Method:      method,
						URL:         rawURL,
						Headers:     cloneHeaders(headers),
						Body:        bBytes,
					})
				}
				break
			}
		}
	}

	// 2. In URL Path parameters
	if segIdx, _, paramLabel, _, ok := findPathParamSegment(rawURL); ok {
		for _, inj := range injections {
			mutURL := mutateURLPathSegment(rawURL, segIdx, inj.val)
			mutations = append(mutations, Mutation{
				Category:    "injection",
				Name:        fmt.Sprintf("Path injection in %s: %s", paramLabel, inj.name),
				Description: fmt.Sprintf("Tests URL path parameter sanitization against %s on %s", inj.name, paramLabel),
				Method:      method,
				URL:         mutURL,
				Headers:     cloneHeaders(headers),
				Body:        body,
			})
		}
	}

	// 3. In Query parameters (or synthesized query for bodiless requests)
	if len(query) > 0 || !isJSON {
		targetKey := "id"
		if len(query) > 0 {
			var qKeys []string
			for k := range query {
				qKeys = append(qKeys, k)
			}
			sort.Strings(qKeys)
			targetKey = qKeys[0]
		}

		for _, inj := range injections {
			mutatedQ := make(map[string]string, len(query))
			for k, v := range query {
				mutatedQ[k] = v
			}
			mutatedQ[targetKey] = inj.val

			// Build new URL
			u, err := url.Parse(rawURL)
			if err == nil {
				q := u.Query()
				for k, v := range mutatedQ {
					q.Set(k, v)
				}
				u.RawQuery = q.Encode()
				mutations = append(mutations, Mutation{
					Category:    "injection",
					Name:        fmt.Sprintf("Query injection in ?%s: %s", targetKey, inj.name),
					Description: fmt.Sprintf("Tests query parameter handling against %s", inj.name),
					Method:      method,
					URL:         u.String(),
					Headers:     cloneHeaders(headers),
					Body:        nil,
				})
			}
		}
	}

	return mutations
}

func generateNullMutations(method, rawURL string, headers map[string]string, jsonBody any, isJSON bool) []Mutation {
	var mutations []Mutation
	if !isJSON {
		return mutations
	}

	if obj, ok := jsonBody.(map[string]any); ok {
		// A. Null injection on fields
		for key := range obj {
			mutated := deepCopyJSON(obj).(map[string]any)
			mutated[key] = nil
			bBytes, _ := json.Marshal(mutated)
			mutations = append(mutations, Mutation{
				Category:    "nulls",
				Name:        fmt.Sprintf("Null value on %q", key),
				Description: fmt.Sprintf("Sends null on field %q to verify nil-dereference protection", key),
				Method:      method,
				URL:         rawURL,
				Headers:     cloneHeaders(headers),
				Body:        bBytes,
			})
			break
		}

		// B. Empty JSON object
		emptyObj, _ := json.Marshal(map[string]any{})
		mutations = append(mutations, Mutation{
			Category:    "nulls",
			Name:        "Empty JSON object ({})",
			Description: "Sends empty JSON object to verify required field validation",
			Method:      method,
			URL:         rawURL,
			Headers:     cloneHeaders(headers),
			Body:        emptyObj,
		})

		// C. Deep nesting recursion probe (30 levels deep)
		deep := make(map[string]any)
		cur := deep
		for i := 0; i < 30; i++ {
			next := make(map[string]any)
			cur["nested"] = next
			cur = next
		}
		cur["leaf"] = "overflow"
		deepBytes, _ := json.Marshal(deep)
		mutations = append(mutations, Mutation{
			Category:    "nulls",
			Name:        "Deep JSON nesting (30 levels)",
			Description: "Tests recursive parser stack limits with deep nesting",
			Method:      method,
			URL:         rawURL,
			Headers:     cloneHeaders(headers),
			Body:        deepBytes,
		})

		// D. Privilege escalation / Prototype pollution probes
		polluted := deepCopyJSON(obj).(map[string]any)
		polluted["isAdmin"] = true
		polluted["role"] = "admin"
		polluted["__proto__"] = map[string]any{"admin": true}
		pollutedBytes, _ := json.Marshal(polluted)
		mutations = append(mutations, Mutation{
			Category:    "nulls",
			Name:        "Unexpected properties (isAdmin: true, role: admin)",
			Description: "Tests mass assignment and privilege escalation property filtering",
			Method:      method,
			URL:         rawURL,
			Headers:     cloneHeaders(headers),
			Body:        pollutedBytes,
		})
	}

	return mutations
}

func generateHeaderMutations(method, rawURL string, headers map[string]string, body []byte) []Mutation {
	var mutations []Mutation

	// 1. Invalid Content-Type
	h1 := cloneHeaders(headers)
	h1["Content-Type"] = "application/xml; charset=unsupported"
	mutations = append(mutations, Mutation{
		Category:    "headers",
		Name:        "Invalid Content-Type header (application/xml)",
		Description: "Verifies Content-Type negotiation and rejects unsupported media types",
		Method:      method,
		URL:         rawURL,
		Headers:     h1,
		Body:        body,
	})

	// 2. Oversized Header
	h2 := cloneHeaders(headers)
	h2["X-Fuzz-Oversized"] = strings.Repeat("B", 8192)
	mutations = append(mutations, Mutation{
		Category:    "headers",
		Name:        "Oversized request header (8KB value)",
		Description: "Tests server HTTP header buffer size limits",
		Method:      method,
		URL:         rawURL,
		Headers:     h2,
		Body:        body,
	})

	// 3. Corrupted Bearer Authorization
	h3 := cloneHeaders(headers)
	h3["Authorization"] = "Bearer malformed.jwt.token.truncated"
	mutations = append(mutations, Mutation{
		Category:    "headers",
		Name:        "Corrupted Bearer token",
		Description: "Tests authentication token parser exception handling",
		Method:      method,
		URL:         rawURL,
		Headers:     h3,
		Body:        body,
	})

	return mutations
}
