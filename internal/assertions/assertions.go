package assertions

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/jmespath/go-jmespath"
)

type TestResult struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

var ops = map[string]bool{
	"equals":       true,
	"eq":           true,
	"not_equals":   true,
	"ne":           true,
	"contains":     true,
	"not_contains": true,
	"exists":       true,
	"type":         true,
	"gt":           true,
	"gte":          true,
	"lt":           true,
	"lte":          true,
	"matches":      true,
	"starts_with":  true,
	"ends_with":    true,
	"length":       true,
	"in":           true,
	"not_in":       true,
	"truthy":       true,
}

func normalizeForJMESPath(v any) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case int:
		return float64(val)
	case int8:
		return float64(val)
	case int16:
		return float64(val)
	case int32:
		return float64(val)
	case int64:
		return float64(val)
	case uint:
		return float64(val)
	case uint8:
		return float64(val)
	case uint16:
		return float64(val)
	case uint32:
		return float64(val)
	case uint64:
		return float64(val)
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[k] = normalizeForJMESPath(item)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = normalizeForJMESPath(item)
		}
		return out
	default:
		return v
	}
}

func Search(expr string, data any) (any, error) {
	expr = strings.TrimSpace(expr)
	if strings.HasPrefix(expr, "$.") {
		expr = "json." + expr[2:]
	} else if expr == "$" {
		expr = "json"
	}
	res, err := jmespath.Search(expr, normalizeForJMESPath(data))
	if err != nil {
		return nil, fmt.Errorf("bad expression %q: %w", expr, err)
	}
	return res, nil
}

func Short(value any, limit int) string {
	if limit <= 0 {
		limit = 80
	}
	b, err := json.Marshal(value)
	var text string
	if err == nil {
		text = string(b)
	} else {
		text = fmt.Sprintf("%v", value)
	}
	if len(text) <= limit {
		return text
	}
	return text[:limit-3] + "..."
}

func typeName(value any) string {
	if value == nil {
		return "null"
	}
	switch value.(type) {
	case bool:
		return "boolean"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "integer"
	case float32, float64:
		// Check if whole number
		f := reflect.ValueOf(value).Float()
		if math.Floor(f) == f {
			return "integer"
		}
		return "number"
	case string:
		return "string"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	default:
		rv := reflect.ValueOf(value)
		switch rv.Kind() {
		case reflect.Map:
			return "object"
		case reflect.Slice, reflect.Array:
			return "array"
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return "integer"
		case reflect.Float32, reflect.Float64:
			if math.Floor(rv.Float()) == rv.Float() {
				return "integer"
			}
			return "number"
		case reflect.Bool:
			return "boolean"
		case reflect.String:
			return "string"
		default:
			return rv.Type().String()
		}
	}
}

func toFloat(v any) (float64, bool) {
	if v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		rv := reflect.ValueOf(v)
		if rv.CanFloat() {
			return rv.Float(), true
		}
		if rv.CanInt() {
			return float64(rv.Int()), true
		}
		if rv.CanUint() {
			return float64(rv.Uint()), true
		}
		return 0, false
	}
}

func toBool(v any) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case int, int64:
		return val != 0
	case float64:
		return val != 0
	case string:
		s := strings.ToLower(strings.TrimSpace(val))
		return s == "true" || s == "1"
	default:
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Bool:
			return rv.Bool()
		case reflect.Slice, reflect.Array, reflect.Map:
			return rv.Len() > 0
		default:
			return false
		}
	}
}

func looseEqual(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	if a == nil || b == nil {
		return a == b
	}

	fa, aIsNum := toFloat(a)
	fb, bIsNum := toFloat(b)
	if aIsNum && bIsNum {
		return fa == fb
	}

	sa := strings.ToLower(fmt.Sprintf("%v", a))
	sb := strings.ToLower(fmt.Sprintf("%v", b))
	return sa == sb
}

func isMatcherMap(m map[string]any) bool {
	if len(m) == 0 {
		return false
	}
	for k := range m {
		if !ops[k] {
			return false
		}
	}
	return true
}

func Match(actual any, matcher any) (bool, string) {
	mMap, isMap := matcher.(map[string]any)
	if !isMap || !isMatcherMap(mMap) {
		ok := looseEqual(actual, matcher)
		if ok {
			return true, ""
		}
		return false, fmt.Sprintf("expected %s, got %s", Short(matcher, 40), Short(actual, 40))
	}

	for op, expected := range mMap {
		ok := true
		switch op {
		case "equals", "eq":
			ok = looseEqual(actual, expected)
		case "not_equals", "ne":
			ok = !looseEqual(actual, expected)
		case "contains":
			if actual == nil {
				ok = false
			} else if str, isStr := actual.(string); isStr {
				ok = strings.Contains(str, fmt.Sprintf("%v", expected))
			} else {
				rv := reflect.ValueOf(actual)
				if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
					found := false
					for i := 0; i < rv.Len(); i++ {
						if looseEqual(rv.Index(i).Interface(), expected) {
							found = true
							break
						}
					}
					ok = found
				} else if rv.Kind() == reflect.Map {
					ok = rv.MapIndex(reflect.ValueOf(fmt.Sprintf("%v", expected))).IsValid()
				} else {
					ok = false
				}
			}
		case "not_contains":
			if actual == nil {
				ok = true
			} else if str, isStr := actual.(string); isStr {
				ok = !strings.Contains(str, fmt.Sprintf("%v", expected))
			} else {
				rv := reflect.ValueOf(actual)
				if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
					found := false
					for i := 0; i < rv.Len(); i++ {
						if looseEqual(rv.Index(i).Interface(), expected) {
							found = true
							break
						}
					}
					ok = !found
				} else if rv.Kind() == reflect.Map {
					ok = !rv.MapIndex(reflect.ValueOf(fmt.Sprintf("%v", expected))).IsValid()
				} else {
					ok = true
				}
			}
		case "exists":
			expectedBool := toBool(expected)
			ok = (actual != nil) == expectedBool
		case "type":
			want := strings.ToLower(fmt.Sprintf("%v", expected))
			have := typeName(actual)
			ok = have == want || (want == "number" && have == "integer")
		case "gt", "gte", "lt", "lte":
			aFloat, aOk := toFloat(actual)
			eFloat, eOk := toFloat(expected)
			if !aOk || !eOk {
				ok = false
			} else {
				switch op {
				case "gt":
					ok = aFloat > eFloat
				case "gte":
					ok = aFloat >= eFloat
				case "lt":
					ok = aFloat < eFloat
				case "lte":
					ok = aFloat <= eFloat
				}
			}
		case "matches":
			if actual == nil {
				ok = false
			} else {
				matched, err := regexp.MatchString(fmt.Sprintf("%v", expected), fmt.Sprintf("%v", actual))
				ok = err == nil && matched
			}
		case "starts_with":
			if str, isStr := actual.(string); isStr {
				ok = strings.HasPrefix(str, fmt.Sprintf("%v", expected))
			} else {
				ok = false
			}
		case "ends_with":
			if str, isStr := actual.(string); isStr {
				ok = strings.HasSuffix(str, fmt.Sprintf("%v", expected))
			} else {
				ok = false
			}
		case "length":
			eInt, eOk := toFloat(expected)
			if !eOk {
				ok = false
			} else {
				rv := reflect.ValueOf(actual)
				if !rv.IsValid() {
					ok = false
				} else {
					switch rv.Kind() {
					case reflect.String, reflect.Slice, reflect.Array, reflect.Map:
						ok = rv.Len() == int(eInt)
					default:
						ok = false
					}
				}
			}
		case "in":
			rv := reflect.ValueOf(expected)
			found := false
			if rv.IsValid() && (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) {
				for i := 0; i < rv.Len(); i++ {
					if looseEqual(actual, rv.Index(i).Interface()) {
						found = true
						break
					}
				}
			}
			ok = found
		case "not_in":
			rv := reflect.ValueOf(expected)
			found := false
			if rv.IsValid() && (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) {
				for i := 0; i < rv.Len(); i++ {
					if looseEqual(actual, rv.Index(i).Interface()) {
						found = true
						break
					}
				}
			}
			ok = !found
		case "truthy":
			ok = toBool(actual) == toBool(expected)
		}

		if !ok {
			return false, fmt.Sprintf("%s %s failed, got %s", op, Short(expected, 40), Short(actual, 40))
		}
	}
	return true, ""
}

func StatusOk(status int, expected any) bool {
	switch e := expected.(type) {
	case []any:
		for _, item := range e {
			if StatusOk(status, item) {
				return true
			}
		}
		return false
	case string:
		pattern := strings.ToLower(strings.TrimSpace(e))
		if strings.HasSuffix(pattern, "xx") && len(pattern) == 3 && pattern[0] >= '0' && pattern[0] <= '9' {
			return status/100 == int(pattern[0]-'0')
		}
		s, err := strconv.Atoi(pattern)
		return err == nil && status == s
	case int:
		return status == e
	case float64:
		return status == int(e)
	case int64:
		return status == int(e)
	default:
		f, ok := toFloat(expected)
		return ok && status == int(f)
	}
}

func describe(matcher any) string {
	if mMap, ok := matcher.(map[string]any); ok && isMatcherMap(mMap) {
		var parts []string
		for op, v := range mMap {
			parts = append(parts, fmt.Sprintf("%s %s", op, Short(v, 40)))
		}
		return strings.Join(parts, " and ")
	}
	return fmt.Sprintf("== %s", Short(matcher, 40))
}

func Evaluate(tests []map[string]any, response map[string]any) []TestResult {
	var results []TestResult
	for _, test := range tests {
		label, _ := test["name"].(string)
		start := len(results)
		evaluateOne(test, response, &results)
		if label != "" && len(results)-start > 1 {
			group := results[start:]
			var failed []TestResult
			for _, r := range group {
				if !r.Passed {
					failed = append(failed, r)
				}
			}
			results = results[:start]
			var details []string
			for _, f := range failed {
				if f.Detail != "" {
					details = append(details, f.Detail)
				}
			}
			results = append(results, TestResult{
				Name:   label,
				Passed: len(failed) == 0,
				Detail: strings.Join(details, "; "),
			})
		}
	}
	return results
}

func evaluateOne(test map[string]any, response map[string]any, results *[]TestResult) {
	label, _ := test["name"].(string)

	for key, expected := range test {
		if key == "name" {
			continue
		}
		switch key {
		case "status":
			status, _ := toFloat(response["status"])
			ok := StatusOk(int(status), expected)
			name := label
			if name == "" {
				name = fmt.Sprintf("status is %v", expected)
			}
			detail := ""
			if !ok {
				detail = fmt.Sprintf("got %d", int(status))
			}
			*results = append(*results, TestResult{Name: name, Passed: ok, Detail: detail})

		case "max_ms":
			ms, _ := toFloat(response["ms"])
			maxMs, _ := toFloat(expected)
			ok := ms <= maxMs
			name := label
			if name == "" {
				name = fmt.Sprintf("responds within %v ms", expected)
			}
			detail := ""
			if !ok {
				detail = fmt.Sprintf("took %.0f ms", ms)
			}
			*results = append(*results, TestResult{Name: name, Passed: ok, Detail: detail})

		case "json":
			expMap, ok := expected.(map[string]any)
			if !ok {
				name := label
				if name == "" {
					name = "json"
				}
				*results = append(*results, TestResult{Name: name, Passed: false, Detail: "'json' test must map paths to matchers"})
				continue
			}
			for path, matcher := range expMap {
				if response["json"] == nil {
					name := label
					if name == "" {
						name = fmt.Sprintf("json %s", path)
					}
					*results = append(*results, TestResult{Name: name, Passed: false, Detail: "response body is not JSON"})
					continue
				}
				actual, err := Search(path, response["json"])
				if err != nil {
					name := label
					if name == "" {
						name = fmt.Sprintf("json %s", path)
					}
					*results = append(*results, TestResult{Name: name, Passed: false, Detail: err.Error()})
					continue
				}
				matched, detail := Match(actual, matcher)
				name := label
				if name == "" {
					name = fmt.Sprintf("json %s %s", path, describe(matcher))
				}
				*results = append(*results, TestResult{Name: name, Passed: matched, Detail: detail})
			}

		case "headers":
			respHeaders, _ := response["headers"].(map[string]any)
			if respHeaders == nil {
				// try map[string]string
				if sh, ok := response["headers"].(map[string]string); ok {
					respHeaders = make(map[string]any, len(sh))
					for k, v := range sh {
						respHeaders[strings.ToLower(k)] = v
					}
				}
			}
			expHeaders, _ := expected.(map[string]any)
			for name, matcher := range expHeaders {
				var actual any
				if respHeaders != nil {
					actual = respHeaders[strings.ToLower(name)]
				}
				matched, detail := Match(actual, matcher)
				testName := label
				if testName == "" {
					testName = fmt.Sprintf("header %s %s", name, describe(matcher))
				}
				*results = append(*results, TestResult{Name: testName, Passed: matched, Detail: detail})
			}

		case "text":
			matched, detail := Match(response["text"], expected)
			name := label
			if name == "" {
				name = fmt.Sprintf("body %s", describe(expected))
			}
			*results = append(*results, TestResult{Name: name, Passed: matched, Detail: detail})

		case "expr":
			val, err := Search(fmt.Sprintf("%v", expected), response)
			ok := false
			detail := ""
			if err != nil {
				detail = err.Error()
			} else {
				ok = toBool(val)
				if !ok {
					detail = fmt.Sprintf("evaluated to %s", Short(val, 40))
				}
			}
			name := label
			if name == "" {
				name = fmt.Sprintf("expr %v", expected)
			}
			*results = append(*results, TestResult{Name: name, Passed: ok, Detail: detail})

		default:
			name := label
			if name == "" {
				name = key
			}
			*results = append(*results, TestResult{Name: name, Passed: false, Detail: fmt.Sprintf("unknown test key '%s'", key)})
		}
	}
}
