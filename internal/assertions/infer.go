package assertions

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	uuidRegex  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	dateRegex  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?)?$`)
	emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
)

type InferOptions struct {
	Strict         bool // If true, assert exact scalar values in addition to types
	IncludeLatency bool // If true, include max_ms latency threshold
	IncludeHeaders bool // If true, include content-type header matcher
	MaxDepth       int  // Max recursion depth for nested objects (default: 3)
}

func DefaultInferOptions() InferOptions {
	return InferOptions{
		Strict:         false,
		IncludeLatency: true,
		IncludeHeaders: true,
		MaxDepth:       3,
	}
}

// InferFromResponse analyzes an HTTP response and synthesizes test assertions.
func InferFromResponse(status int, elapsedMs float64, headers map[string]string, body string, opts InferOptions) ([]map[string]any, string, error) {
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = 3
	}

	var tests []map[string]any

	// 1. Status Code Assertion
	if status > 0 {
		tests = append(tests, map[string]any{
			"status": status,
		})
	}

	// 2. Latency SLA Assertion (max_ms)
	if opts.IncludeLatency && elapsedMs > 0 {
		// Generous upper bound: max(500, ceil(elapsedMs * 2.5 rounded up to next 100))
		bound := math.Ceil(elapsedMs * 2.5 / 100.0) * 100.0
		if bound < 500 {
			bound = 500
		}
		tests = append(tests, map[string]any{
			"max_ms": int(bound),
		})
	}

	// 3. Header Assertions (Content-Type)
	contentType := ""
	for k, v := range headers {
		if strings.EqualFold(k, "content-type") {
			contentType = v
			break
		}
	}

	if opts.IncludeHeaders && contentType != "" {
		if strings.Contains(contentType, "application/json") {
			tests = append(tests, map[string]any{
				"headers": map[string]any{
					"content-type": map[string]any{
						"contains": "application/json",
					},
				},
			})
		} else if strings.Contains(contentType, "text/html") {
			tests = append(tests, map[string]any{
				"headers": map[string]any{
					"content-type": map[string]any{
						"contains": "text/html",
					},
				},
			})
		} else if strings.Contains(contentType, "xml") {
			tests = append(tests, map[string]any{
				"headers": map[string]any{
					"content-type": map[string]any{
						"contains": "xml",
					},
				},
			})
		}
	}

	// 4. Body Assertions
	trimmedBody := strings.TrimSpace(body)
	if trimmedBody != "" {
		var parsed any
		if err := json.Unmarshal([]byte(trimmedBody), &parsed); err == nil {
			jsonMatchers := make(map[string]any)
			var relationalExprs []string

			switch val := parsed.(type) {
			case map[string]any:
				inferObjectMatchers("", val, jsonMatchers, 1, opts)
				// Check for common relational fields like total and items
				itemsVal, hasItems := val["items"]
				if !hasItems {
					itemsVal, hasItems = val["data"]
				}
				if !hasItems {
					itemsVal, hasItems = val["results"]
				}
				totalVal, hasTotal := val["total"]
				if !hasTotal {
					totalVal, hasTotal = val["count"]
				}
				if hasItems && hasTotal {
					if _, isSlice := itemsVal.([]any); isSlice {
						if _, isNum := toFloat(totalVal); isNum {
							if _, hasItemsKey := val["items"]; hasItemsKey {
								relationalExprs = append(relationalExprs, "json.total >= length(json.items)")
							} else if _, hasDataKey := val["data"]; hasDataKey {
								relationalExprs = append(relationalExprs, "json.total >= length(json.data)")
							} else if _, hasResultsKey := val["results"]; hasResultsKey {
								relationalExprs = append(relationalExprs, "json.total >= length(json.results)")
							}
						}
					}
				}

			case []any:
				if len(val) > 0 {
					jsonMatchers["length(@)"] = map[string]any{"gte": 1}
					if firstObj, ok := val[0].(map[string]any); ok {
						inferObjectMatchers("[0]", firstObj, jsonMatchers, 1, opts)
					}
				} else {
					jsonMatchers["length(@)"] = 0
				}
			}

			if len(jsonMatchers) > 0 {
				// Sort keys deterministically
				keys := make([]string, 0, len(jsonMatchers))
				for k := range jsonMatchers {
					keys = append(keys, k)
				}
				sort.Strings(keys)

				sortedMap := make(map[string]any, len(jsonMatchers))
				for _, k := range keys {
					sortedMap[k] = jsonMatchers[k]
				}
				tests = append(tests, map[string]any{
					"json": sortedMap,
				})
			}

			for _, expr := range relationalExprs {
				tests = append(tests, map[string]any{
					"expr": expr,
				})
			}

		} else {
			// Plain text or HTML
			if strings.Contains(contentType, "html") {
				titleMatch := regexp.MustCompile(`(?i)<title>(.*?)</title>`).FindStringSubmatch(trimmedBody)
				if len(titleMatch) > 1 && len(titleMatch[1]) > 0 {
					tests = append(tests, map[string]any{
						"text": map[string]any{
							"contains": titleMatch[1],
						},
					})
				}
			} else if len(trimmedBody) < 120 && !strings.Contains(trimmedBody, "\n") {
				if opts.Strict {
					tests = append(tests, map[string]any{
						"text": trimmedBody,
					})
				} else {
					tests = append(tests, map[string]any{
						"text": map[string]any{
							"not_equals": "",
						},
					})
				}
			}
		}
	}

	yamlStr, err := InferToYAML(tests)
	if err != nil {
		return tests, "", err
	}

	return tests, yamlStr, nil
}

func inferObjectMatchers(prefix string, obj map[string]any, matchers map[string]any, depth int, opts InferOptions) {
	if depth > opts.MaxDepth {
		return
	}

	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := obj[k]
		path := k
		if prefix != "" {
			if strings.HasPrefix(prefix, "[") {
				path = fmt.Sprintf("%s.%s", prefix, k)
			} else {
				path = fmt.Sprintf("%s.%s", prefix, k)
			}
		}

		if v == nil {
			matchers[path] = map[string]any{"exists": true}
			continue
		}

		switch val := v.(type) {
		case string:
			lowerKey := strings.ToLower(k)
			if uuidRegex.MatchString(val) {
				matchers[path] = map[string]any{
					"type":    "string",
					"matches": `^[0-9a-fA-F-]{36}$`,
				}
			} else if dateRegex.MatchString(val) {
				matchers[path] = map[string]any{
					"type":    "string",
					"matches": `^\d{4}-\d{2}-\d{2}`,
				}
			} else if emailRegex.MatchString(val) {
				matchers[path] = map[string]any{
					"type":    "string",
					"matches": `^[^@]+@[^@]+\.[^@]+$`,
				}
			} else if opts.Strict {
				matchers[path] = val
			} else if lowerKey == "status" || lowerKey == "state" || lowerKey == "type" || lowerKey == "kind" || lowerKey == "role" {
				// High-value semantic enum-like property: test exact value or non-empty string
				matchers[path] = val
			} else {
				matchers[path] = map[string]any{
					"type": "string",
				}
			}

		case float64:
			lowerKey := strings.ToLower(k)
			if math.Trunc(val) == val {
				// Integer
				if opts.Strict {
					matchers[path] = int(val)
				} else if strings.Contains(lowerKey, "id") || strings.Contains(lowerKey, "count") ||
					strings.Contains(lowerKey, "total") || strings.Contains(lowerKey, "age") {
					if val >= 0 {
						matchers[path] = map[string]any{
							"type": "integer",
							"gte":  0,
						}
					} else {
						matchers[path] = map[string]any{"type": "integer"}
					}
				} else {
					matchers[path] = map[string]any{"type": "integer"}
				}
			} else {
				if opts.Strict {
					matchers[path] = val
				} else {
					matchers[path] = map[string]any{"type": "number"}
				}
			}

		case bool:
			if opts.Strict {
				matchers[path] = val
			} else {
				matchers[path] = map[string]any{"type": "boolean"}
			}

		case []any:
			lenPath := fmt.Sprintf("length(%s)", path)
			if len(val) > 0 {
				matchers[lenPath] = map[string]any{"gte": 1}
				// Inspect the first element of the array
				if firstItem, isMap := val[0].(map[string]any); isMap && depth < opts.MaxDepth {
					firstItemPrefix := fmt.Sprintf("%s[0]", path)
					inferObjectMatchers(firstItemPrefix, firstItem, matchers, depth+1, opts)
				}
			} else {
				matchers[lenPath] = 0
			}

		case map[string]any:
			inferObjectMatchers(path, val, matchers, depth+1, opts)
		}
	}
}

// InferToYAML formats a slice of test definitions into clean YAML.
func InferToYAML(tests []map[string]any) (string, error) {
	wrapper := map[string]any{
		"tests": tests,
	}
	data, err := yaml.Marshal(wrapper)
	if err != nil {
		return "", fmt.Errorf("format tests to YAML: %w", err)
	}
	return string(data), nil
}
