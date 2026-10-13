package importer

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/hit-endpoint/hit-endpoint/internal/zone"
)

var (
	slugRegex = regexp.MustCompile(`[^a-z0-9]+`)
)

type ImportReport struct {
	Collection        string              `json:"collection"`
	Destination       string              `json:"destination"`
	Requests          int                 `json:"requests"`
	Folders           int                 `json:"folders"`
	TestsConverted    int                 `json:"tests_converted"`
	CapturesConverted int                 `json:"captures_converted"`
	Unconverted       map[string][]string `json:"unconverted"` // ref -> unconverted script lines
	Warnings          []string            `json:"warnings"`
	Files             []string            `json:"files"`
}

func Slugify(name string, fallback string) string {
	slug := strings.Trim(slugRegex.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(slug) > 60 {
		slug = slug[:60]
	}
	if slug == "" {
		return fallback
	}
	return slug
}

func kvList(items any) map[string]any {
	out := make(map[string]any)
	list, ok := items.([]any)
	if !ok {
		return out
	}
	for _, it := range list {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if dis, ok := m["disabled"].(bool); ok && dis {
			continue
		}
		k, ok := m["key"].(string)
		if !ok || k == "" {
			continue
		}
		val := m["value"]
		if val == nil {
			out[k] = ""
		} else {
			out[k] = val
		}
	}
	return out
}

func authParams(auth map[string]any, kind string) map[string]any {
	params, ok := auth[kind]
	if !ok {
		return make(map[string]any)
	}
	if list, ok := params.([]any); ok {
		out := make(map[string]any)
		for _, p := range list {
			if pm, ok := p.(map[string]any); ok {
				if k, ok := pm["key"].(string); ok {
					out[k] = pm["value"]
				}
			}
		}
		return out
	}
	if pm, ok := params.(map[string]any); ok {
		return pm
	}
	return make(map[string]any)
}

func ConvertAuth(auth any, report *ImportReport, ref string) any {
	if auth == nil {
		return nil
	}
	am, ok := auth.(map[string]any)
	if !ok {
		return nil
	}
	kind := strings.ToLower(fmt.Sprintf("%v", am["type"]))
	if kind == "noauth" || kind == "none" {
		return "none"
	}
	if kind == "inherit" {
		return nil
	}
	params := authParams(am, kind)
	switch kind {
	case "bearer":
		token := ""
		if t, ok := params["token"]; ok && t != nil {
			token = fmt.Sprintf("%v", t)
		}
		return map[string]any{"type": "bearer", "token": token}
	case "basic":
		user := ""
		if u, ok := params["username"]; ok && u != nil {
			user = fmt.Sprintf("%v", u)
		}
		pass := ""
		if p, ok := params["password"]; ok && p != nil {
			pass = fmt.Sprintf("%v", p)
		}
		return map[string]any{"type": "basic", "username": user, "password": pass}
	case "apikey":
		key := "X-API-Key"
		if k, ok := params["key"]; ok && k != nil && fmt.Sprintf("%v", k) != "" {
			key = fmt.Sprintf("%v", k)
		}
		val := ""
		if v, ok := params["value"]; ok && v != nil {
			val = fmt.Sprintf("%v", v)
		}
		out := map[string]any{"type": "apikey", "key": key, "value": val}
		if inVal, ok := params["in"]; ok && strings.ToLower(fmt.Sprintf("%v", inVal)) == "query" {
			out["in"] = "query"
		}
		return out
	case "oauth2":
		token := "{{access_token}}"
		if at, ok := params["accessToken"]; ok && at != nil && fmt.Sprintf("%v", at) != "" {
			token = fmt.Sprintf("%v", at)
		}
		prefix := "Bearer"
		if pr, ok := params["headerPrefix"].(string); ok && pr != "" {
			prefix = pr
		}
		report.Warnings = append(report.Warnings, fmt.Sprintf("%s: oauth2 converted to bearer token", ref))
		return map[string]any{"type": "bearer", "token": token, "prefix": prefix}
	default:
		report.Warnings = append(report.Warnings, fmt.Sprintf("%s: auth type '%s' is not supported; kept under unconverted_auth", ref, kind))
		return map[string]any{"type": "none", "unconverted_auth": auth}
	}
}

func ConvertURL(urlVal any) (string, map[string]any, map[string]any) {
	if urlVal == nil {
		return "", make(map[string]any), make(map[string]any)
	}

	var raw string
	query := make(map[string]any)
	pathVars := make(map[string]any)

	if s, ok := urlVal.(string); ok {
		raw = s
	} else if m, ok := urlVal.(map[string]any); ok {
		if r, ok := m["raw"].(string); ok {
			raw = r
		}
		if qList, ok := m["query"].([]any); ok {
			for _, item := range qList {
				if qm, ok := item.(map[string]any); ok {
					if dis, ok := qm["disabled"].(bool); ok && dis {
						continue
					}
					if k, ok := qm["key"].(string); ok && k != "" {
						query[k] = qm["value"]
					}
				}
			}
		}
		if vList, ok := m["variable"].([]any); ok {
			for _, item := range vList {
				if vm, ok := item.(map[string]any); ok {
					if k, ok := vm["key"].(string); ok && k != "" {
						pathVars[k] = vm["value"]
					}
				}
			}
		}
	}

	// If query was empty from dict, parse from query string in raw URL
	if len(query) == 0 && strings.Contains(raw, "?") {
		parts := strings.SplitN(raw, "?", 2)
		raw = parts[0]
		parsedQuery, err := url.ParseQuery(parts[1])
		if err == nil {
			for k, vs := range parsedQuery {
				if len(vs) > 0 {
					query[k] = vs[0]
				}
			}
		}
	} else if strings.Contains(raw, "?") {
		raw = strings.SplitN(raw, "?", 2)[0]
	}

	// Convert :param to {{param}}
	re := regexp.MustCompile(`/:([A-Za-z_][\w-]*)`)
	raw = re.ReplaceAllString(raw, `/{{$1}}`)

	return raw, query, pathVars
}

type ScriptConverter struct {
	Lines       []string
	Tests       []map[string]any
	Captures    map[string]any
	Vars        map[string]any
	Unconverted []string
	jsonVars    map[string]bool
}

func NewScriptConverter(lines []string) *ScriptConverter {
	return &ScriptConverter{
		Lines:    lines,
		Captures: make(map[string]any),
		Vars:     make(map[string]any),
		jsonVars: map[string]bool{
			"jsonData":     true,
			"body":         true,
			"data":         true,
			"res":          true,
			"resp":         true,
			"responseJson": true,
		},
	}
}

func (c *ScriptConverter) Convert() {
	var curTest *map[string]any

	flushTest := func() {
		if curTest != nil {
			// Only append if the test has at least one assertion (not just a "name" key)
			if len(*curTest) > 1 || (*curTest)["name"] == nil {
				c.Tests = append(c.Tests, *curTest)
			}
			curTest = nil
		}
	}

	jsonAssignRe := regexp.MustCompile(`^(?:var|let|const)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:pm\.response\.json\(\)|JSON\.parse\(responseBody\))\s*;?$`)
	pmTestHeaderRe := regexp.MustCompile(`^pm\.test\(\s*["'](.*?)["']\s*,\s*(?:function\s*\([^)]*\)\s*\{|\([^)]*\)\s*=>\s*\{?)\s*(.*)$`)
	statusRe := regexp.MustCompile(`pm\.response\.to\.(?:have\.)?status\(\s*(\d+)\s*\)`)
	codeBelowRe := regexp.MustCompile(`pm\.expect\(\s*pm\.response\.code\s*\)\.to\.be\.below\(\s*(\d+)\s*\)`)
	codeEqlRe := regexp.MustCompile(`pm\.expect\(\s*pm\.response\.code\s*\)\.to\.(?:eql|equal)\(\s*(\d+)\s*\)`)
	codeOneOfRe := regexp.MustCompile(`pm\.expect\(\s*pm\.response\.code\s*\)\.to\.be\.oneOf\(\s*\[([\d,\s]+)\]\s*\)`)
	respOkRe := regexp.MustCompile(`pm\.response\.to\.be\.(?:ok|success)\b`)
	jsonTestRe := regexp.MustCompile(`pm\.response\.to\.(?:be\.)?json\b`)
	headerRe := regexp.MustCompile(`pm\.response\.to\.have\.header\(\s*["']([^"']+)["'](?:\s*,\s*["']([^"']*)["'])?\s*\)`)
	timeBelowRe := regexp.MustCompile(`pm\.response\.responseTime\s*\)\.to\.be\.below\(\s*(\d+)\s*\)`)
	expectTypeRe := regexp.MustCompile(`pm\.expect\((.*?)\)\.to\.be\.an?\(\s*["'](.*?)["']\s*\)`)
	expectAboveRe := regexp.MustCompile(`pm\.expect\((.*?)\)\.to\.be\.(?:above|greaterThan)\(\s*(-?\d+)\s*\)`)
	expectAtLeastRe := regexp.MustCompile(`pm\.expect\((.*?)\)\.to\.be\.(?:at\.least|gte)\(\s*(-?\d+)\s*\)`)
	expectBelowRe := regexp.MustCompile(`pm\.expect\((.*?)\)\.to\.be\.(?:below|lessThan)\(\s*(-?\d+)\s*\)`)
	expectPropRe := regexp.MustCompile(`pm\.expect\((.*?)\)\.to\.have\.property\(\s*["']([^"']+)["'](?:\s*,\s*(.+?))?\s*\)`)
	expectEqlRe := regexp.MustCompile(`pm\.expect\((.*?)\)\.to\.(?:deep\.)?(?:eql|equal)\(\s*(.+?)\s*\)`)
	expectIncludeRe := regexp.MustCompile(`pm\.expect\((.*?)\)\.to\.(?:include|contain)\(\s*["'](.*?)["']\s*\)`)
	expectLenRe := regexp.MustCompile(`pm\.expect\((.*?)\)\.to\.have\.lengthOf\(\s*(\d+)\s*\)`)
	expectBoolRe := regexp.MustCompile(`pm\.expect\((.*?)\)\.to\.be\.(true|false)\b`)
	expectExistRe := regexp.MustCompile(`pm\.expect\((.*?)\)\.to\.(?:exist|not\.be\.null|not\.be\.undefined)\b`)
	envSetRe := regexp.MustCompile(`pm\.(?:environment|collectionVariables|globals|variables)\.set\(\s*["'](.*?)["']\s*,\s*(.*?)\s*\)\s*;?$`)
	setNextReqRe := regexp.MustCompile(`^\w+(?:\.\w+)*\.setNextRequest\(\s*.*?\s*\)\s*;?$`)
	legacyStatusRe := regexp.MustCompile(`^tests\[\s*["'](.*?)["']\s*\]\s*=\s*responseCode\.code\s*===?\s*(\d+)\s*;?$`)

	var expandedLines []string
	for _, rawLine := range c.Lines {
		for _, sub := range strings.Split(rawLine, "\n") {
			expandedLines = append(expandedLines, sub)
		}
	}

	processStmt := func(stmt string, origLine string) {
		trimmed := strings.TrimSpace(stmt)
		trimmed = strings.TrimSuffix(trimmed, "});")
		trimmed = strings.TrimSuffix(trimmed, "})")
		trimmed = strings.TrimSpace(trimmed)
		if trimmed == "" || trimmed == "});" || trimmed == "}" || trimmed == "{" || trimmed == ";" ||
			strings.HasPrefix(trimmed, "//") ||
			strings.HasPrefix(trimmed, "/*") ||
			strings.HasPrefix(trimmed, "*") ||
			strings.HasPrefix(trimmed, "console.log(") ||
			strings.HasPrefix(trimmed, "console.info(") ||
			strings.HasPrefix(trimmed, "console.warn(") ||
			strings.HasPrefix(trimmed, "console.error(") ||
			setNextReqRe.MatchString(trimmed) {
			return
		}

		if m := legacyStatusRe.FindStringSubmatch(trimmed); len(m) > 2 {
			code, _ := strconv.Atoi(m[2])
			c.Tests = append(c.Tests, map[string]any{"name": m[1], "status": code})
			return
		}

		if m := jsonAssignRe.FindStringSubmatch(trimmed); len(m) > 1 {
			c.jsonVars[m[1]] = true
			return
		}

		if m := statusRe.FindStringSubmatch(trimmed); len(m) > 1 {
			code, _ := strconv.Atoi(m[1])
			if curTest != nil {
				(*curTest)["status"] = code
			} else {
				c.Tests = append(c.Tests, map[string]any{"status": code})
			}
			return
		}

		if m := codeBelowRe.FindStringSubmatch(trimmed); len(m) > 1 {
			code, _ := strconv.Atoi(m[1])
			matcher := map[string]any{"lt": code}
			if curTest != nil {
				(*curTest)["status"] = matcher
			} else {
				c.Tests = append(c.Tests, map[string]any{"status": matcher})
			}
			return
		}

		if m := codeEqlRe.FindStringSubmatch(trimmed); len(m) > 1 {
			code, _ := strconv.Atoi(m[1])
			if curTest != nil {
				(*curTest)["status"] = code
			} else {
				c.Tests = append(c.Tests, map[string]any{"status": code})
			}
			return
		}

		if m := codeOneOfRe.FindStringSubmatch(trimmed); len(m) > 1 {
			var codes []int
			for _, part := range strings.Split(m[1], ",") {
				if v, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
					codes = append(codes, v)
				}
			}
			if len(codes) > 0 {
				matcher := map[string]any{"in": codes}
				if curTest != nil {
					(*curTest)["status"] = matcher
				} else {
					c.Tests = append(c.Tests, map[string]any{"status": matcher})
				}
				return
			}
		}

		if respOkRe.MatchString(trimmed) {
			matcher := map[string]any{"lt": 400}
			if curTest != nil {
				(*curTest)["status"] = matcher
			} else {
				c.Tests = append(c.Tests, map[string]any{"status": matcher})
			}
			return
		}

		if jsonTestRe.MatchString(trimmed) {
			c.addHeaderMatcher(curTest, "content-type", map[string]any{"contains": "json"})
			return
		}

		if m := headerRe.FindStringSubmatch(trimmed); len(m) > 1 {
			hName := strings.ToLower(strings.TrimSpace(m[1]))
			if len(m) > 2 && m[2] != "" {
				c.addHeaderMatcher(curTest, hName, m[2])
			} else {
				c.addHeaderMatcher(curTest, hName, map[string]any{"exists": true})
			}
			return
		}

		if m := timeBelowRe.FindStringSubmatch(trimmed); len(m) > 1 {
			ms, _ := strconv.Atoi(m[1])
			if curTest != nil {
				(*curTest)["max_ms"] = ms
			} else {
				c.Tests = append(c.Tests, map[string]any{"max_ms": ms})
			}
			return
		}

		if m := expectTypeRe.FindStringSubmatch(trimmed); len(m) > 2 {
			path := c.cleanPath(m[1])
			typeName := m[2]
			c.addJSONMatcher(curTest, path, map[string]any{"type": typeName})
			return
		}

		if m := expectAboveRe.FindStringSubmatch(trimmed); len(m) > 2 {
			path := c.cleanPath(m[1])
			val, _ := strconv.Atoi(m[2])
			c.addJSONMatcher(curTest, path, map[string]any{"gt": val})
			return
		}

		if m := expectAtLeastRe.FindStringSubmatch(trimmed); len(m) > 2 {
			path := c.cleanPath(m[1])
			val, _ := strconv.Atoi(m[2])
			c.addJSONMatcher(curTest, path, map[string]any{"gte": val})
			return
		}

		if m := expectBelowRe.FindStringSubmatch(trimmed); len(m) > 2 {
			path := c.cleanPath(m[1])
			val, _ := strconv.Atoi(m[2])
			c.addJSONMatcher(curTest, path, map[string]any{"lt": val})
			return
		}

		if m := expectPropRe.FindStringSubmatch(trimmed); len(m) > 2 {
			base := c.cleanPath(m[1])
			prop := strings.TrimSpace(m[2])
			fullPath := prop
			if base != "json" && base != "" {
				fullPath = strings.TrimPrefix(base, "json.") + "." + prop
			}
			var matcher any = map[string]any{"exists": true}
			if len(m) > 3 && strings.TrimSpace(m[3]) != "" {
				matcher = parseJSLiteral(m[3])
			}
			c.addJSONMatcher(curTest, "json."+fullPath, matcher)
			return
		}

		if m := expectEqlRe.FindStringSubmatch(trimmed); len(m) > 2 {
			path := c.cleanPath(m[1])
			c.addJSONMatcher(curTest, path, parseJSLiteral(m[2]))
			return
		}

		if m := expectIncludeRe.FindStringSubmatch(trimmed); len(m) > 2 {
			path := c.cleanPath(m[1])
			c.addJSONMatcher(curTest, path, map[string]any{"contains": m[2]})
			return
		}

		if m := expectLenRe.FindStringSubmatch(trimmed); len(m) > 2 {
			path := c.cleanPath(m[1])
			l, _ := strconv.Atoi(m[2])
			c.addJSONMatcher(curTest, path, map[string]any{"length": l})
			return
		}

		if m := expectBoolRe.FindStringSubmatch(trimmed); len(m) > 2 {
			path := c.cleanPath(m[1])
			c.addJSONMatcher(curTest, path, m[2] == "true")
			return
		}

		if m := expectExistRe.FindStringSubmatch(trimmed); len(m) > 1 {
			path := c.cleanPath(m[1])
			c.addJSONMatcher(curTest, path, map[string]any{"exists": true})
			return
		}

		if m := envSetRe.FindStringSubmatch(trimmed); len(m) > 2 {
			key := m[1]
			rawRHS := strings.TrimSpace(strings.TrimSuffix(m[2], ";"))
			if rawRHS == "Date.now()" || strings.Contains(rawRHS, "Date(") {
				c.Vars[key] = "{{$timestamp}}"
			} else if strings.Contains(strings.ToLower(rawRHS), "uuid") {
				c.Vars[key] = "{{$uuid}}"
			} else {
				c.Captures[key] = c.cleanPath(rawRHS)
			}
			return
		}

		c.Unconverted = append(c.Unconverted, origLine)
	}

	for _, line := range expandedLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "});" || trimmed == "}" || trimmed == "{" {
			if trimmed == "});" || trimmed == "})" {
				flushTest()
			}
			continue
		}

		if m := pmTestHeaderRe.FindStringSubmatch(trimmed); len(m) > 1 {
			flushTest()
			t := map[string]any{"name": m[1]}
			curTest = &t
			rest := strings.TrimSpace(m[2])
			rest = strings.TrimSuffix(rest, "});")
			rest = strings.TrimSuffix(rest, "})")
			rest = strings.TrimSuffix(rest, "}")
			rest = strings.TrimSpace(rest)
			if rest != "" {
				for _, part := range strings.Split(rest, ";") {
					if strings.TrimSpace(part) != "" {
						processStmt(part, line)
					}
				}
				flushTest()
			}
			continue
		}

		processStmt(trimmed, line)
	}

	flushTest()
}

func parseJSLiteral(raw string) any {
	s := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(raw), ";"))
	if (strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`)) || (strings.HasPrefix(s, `'`) && strings.HasSuffix(s, `'`)) {
		return s[1 : len(s)-1]
	}
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

func (c *ScriptConverter) cleanPath(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "pm.response.json()" {
		return "json"
	}
	s = strings.TrimPrefix(s, "pm.response.json().")
	for vName := range c.jsonVars {
		if s == vName {
			return "json"
		}
		if strings.HasPrefix(s, vName+".") {
			s = strings.TrimPrefix(s, vName+".")
			break
		}
	}
	if !strings.HasPrefix(s, "json.") {
		s = "json." + s
	}
	return s
}

func cleanJSONPath(raw string) string {
	conv := NewScriptConverter(nil)
	return conv.cleanPath(raw)
}

func (c *ScriptConverter) addHeaderMatcher(curTest *map[string]any, headerName string, matcher any) {
	if curTest != nil {
		hm, ok := (*curTest)["headers"].(map[string]any)
		if !ok {
			hm = make(map[string]any)
			(*curTest)["headers"] = hm
		}
		hm[headerName] = matcher
	} else {
		c.Tests = append(c.Tests, map[string]any{
			"headers": map[string]any{
				headerName: matcher,
			},
		})
	}
}

func (c *ScriptConverter) addJSONMatcher(curTest *map[string]any, path string, matcher any) {
	relPath := strings.TrimPrefix(path, "json.")
	if curTest != nil {
		jm, ok := (*curTest)["json"].(map[string]any)
		if !ok {
			jm = make(map[string]any)
			(*curTest)["json"] = jm
		}
		if existing, ok := jm[relPath].(map[string]any); ok {
			if mm, ok := matcher.(map[string]any); ok {
				for k, v := range mm {
					existing[k] = v
				}
				return
			}
		}
		jm[relPath] = matcher
	} else {
		c.Tests = append(c.Tests, map[string]any{
			"json": map[string]any{
				relPath: matcher,
			},
		})
	}
}

func extractEventLines(eventsAny any) []string {
	events, ok := eventsAny.([]any)
	if !ok {
		return nil
	}
	var scriptLines []string
	for _, ev := range events {
		em, ok := ev.(map[string]any)
		if !ok {
			continue
		}
		script, ok := em["script"].(map[string]any)
		if !ok {
			continue
		}
		if execList, ok := script["exec"].([]any); ok {
			for _, line := range execList {
				scriptLines = append(scriptLines, fmt.Sprintf("%v", line))
			}
		} else if execStr, ok := script["exec"].(string); ok {
			scriptLines = append(scriptLines, execStr)
		}
	}
	return scriptLines
}

func ImportCollection(srcPath string, collectionsDir string, nameOverride string) (*ImportReport, error) {
	dataBytes, err := os.ReadFile(srcPath)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return nil, err
	}

	collName := ""
	if nameOverride != "" {
		collName = nameOverride
	} else if info, ok := data["info"].(map[string]any); ok {
		if n, ok := info["name"].(string); ok {
			collName = n
		}
	}
	if collName == "" {
		collName = strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
	}

	collSlug := Slugify(collName, "collection")
	destDir := filepath.Join(collectionsDir, collSlug)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, err
	}

	report := &ImportReport{
		Collection:  collName,
		Destination: destDir,
		Unconverted: make(map[string][]string),
	}

	defaults := make(map[string]any)
	if auth := ConvertAuth(data["auth"], report, collName); auth != nil {
		defaults["auth"] = auth
	}
	if vars := kvList(data["variable"]); len(vars) > 0 {
		defaults["vars"] = vars
	}
	if lines := extractEventLines(data["event"]); len(lines) > 0 {
		conv := NewScriptConverter(lines)
		conv.Convert()
		if len(conv.Tests) > 0 {
			defaults["tests"] = conv.Tests
			report.TestsConverted += len(conv.Tests)
		}
	}
	if len(defaults) > 0 {
		dYAML, _ := zone.DumpYAML(defaults)
		_ = os.WriteFile(filepath.Join(destDir, zone.DefaultsFile), []byte(dYAML), 0644)
	}

	items, _ := data["item"].([]any)
	importItems(items, destDir, "", report)

	return report, nil
}

func importItems(items []any, parentDir string, prefixRef string, report *ImportReport) {
	order := 1
	for _, item := range items {
		im, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := im["name"].(string)
		if name == "" {
			name = fmt.Sprintf("item-%d", order)
		}

		if subItems, ok := im["item"].([]any); ok && len(subItems) > 0 {
			// Folder
			report.Folders++
			folderSlug := fmt.Sprintf("%02d-%s", order, Slugify(name, "folder"))
			folderDir := filepath.Join(parentDir, folderSlug)
			_ = os.MkdirAll(folderDir, 0755)

			folderDefaults := make(map[string]any)
			folderRef := filepath.Join(prefixRef, folderSlug)
			if auth := ConvertAuth(im["auth"], report, folderRef); auth != nil {
				folderDefaults["auth"] = auth
			}
			if vars := kvList(im["variable"]); len(vars) > 0 {
				folderDefaults["vars"] = vars
			}
			if lines := extractEventLines(im["event"]); len(lines) > 0 {
				conv := NewScriptConverter(lines)
				conv.Convert()
				if len(conv.Tests) > 0 {
					folderDefaults["tests"] = conv.Tests
					report.TestsConverted += len(conv.Tests)
				}
			}
			if len(folderDefaults) > 0 {
				fYAML, _ := zone.DumpYAML(folderDefaults)
				_ = os.WriteFile(filepath.Join(folderDir, zone.DefaultsFile), []byte(fYAML), 0644)
			}

			importItems(subItems, folderDir, folderRef, report)
			order++
			continue
		}

		// Request item
		reqData, ok := im["request"].(map[string]any)
		if !ok {
			continue
		}
		report.Requests++

		reqSlug := fmt.Sprintf("%02d-%s", order, Slugify(name, "request"))
		reqPath := filepath.Join(parentDir, reqSlug+".yaml")
		reqRef := filepath.Join(prefixRef, reqSlug)

		reqYAMLData := convertRequest(im, reqData, report, reqRef)
		yStr, _ := zone.DumpYAML(reqYAMLData)
		_ = os.WriteFile(reqPath, []byte(yStr), 0644)
		report.Files = append(report.Files, reqPath)
		order++
	}
}

func convertRequest(item map[string]any, req map[string]any, report *ImportReport, ref string) map[string]any {
	out := make(map[string]any)
	name, _ := item["name"].(string)
	if name != "" {
		out["name"] = name
	}
	method := "GET"
	if m, ok := req["method"].(string); ok && m != "" {
		method = strings.ToUpper(m)
	}
	out["method"] = method

	u, query, pathVars := ConvertURL(req["url"])
	if u != "" {
		out["url"] = u
	}
	if len(query) > 0 {
		out["query"] = query
	}
	if len(pathVars) > 0 {
		out["vars"] = pathVars
	}

	// Headers
	if hList, ok := req["header"].([]any); ok {
		headers := make(map[string]any)
		for _, it := range hList {
			hm, ok := it.(map[string]any)
			if !ok {
				continue
			}
			if dis, ok := hm["disabled"].(bool); ok && dis {
				continue
			}
			if k, ok := hm["key"].(string); ok && k != "" {
				headers[k] = hm["value"]
			}
		}
		if len(headers) > 0 {
			out["headers"] = headers
		}
	}

	// Auth
	if auth := ConvertAuth(req["auth"], report, ref); auth != nil {
		out["auth"] = auth
	}

	// Body
	if bodyMap, ok := req["body"].(map[string]any); ok {
		mode, _ := bodyMap["mode"].(string)
		switch mode {
		case "raw":
			rawText, _ := bodyMap["raw"].(string)
			var parsedJSON any
			if err := json.Unmarshal([]byte(rawText), &parsedJSON); err == nil && (strings.HasPrefix(strings.TrimSpace(rawText), "{") || strings.HasPrefix(strings.TrimSpace(rawText), "[")) {
				out["body"] = map[string]any{"json": parsedJSON}
			} else {
				out["body"] = map[string]any{"raw": rawText}
			}
		case "formdata":
			fdList, _ := bodyMap["formdata"].([]any)
			mp := make(map[string]any)
			for _, it := range fdList {
				fd, ok := it.(map[string]any)
				if !ok {
					continue
				}
				k, _ := fd["key"].(string)
				if k == "" {
					continue
				}
				if fdType, _ := fd["type"].(string); fdType == "file" {
					src, _ := fd["src"].(string)
					mp[k] = map[string]any{"file": src}
					report.Warnings = append(report.Warnings, fmt.Sprintf("%s: multipart file %s references %s", ref, k, src))
				} else {
					mp[k] = fd["value"]
				}
			}
			if len(mp) > 0 {
				out["body"] = map[string]any{"multipart": mp}
			}
		case "urlencoded":
			out["body"] = map[string]any{"form": kvList(bodyMap["urlencoded"])}
		case "graphql":
			if gm, ok := bodyMap["graphql"].(map[string]any); ok {
				out["body"] = map[string]any{"graphql": gm}
			}
		}
	}

	// Scripts / Events
	if scriptLines := extractEventLines(item["event"]); len(scriptLines) > 0 {
		conv := NewScriptConverter(scriptLines)
		conv.Convert()
		if len(conv.Tests) > 0 {
			out["tests"] = conv.Tests
			report.TestsConverted += len(conv.Tests)
		}
		if len(conv.Captures) > 0 {
			out["captures"] = conv.Captures
			report.CapturesConverted += len(conv.Captures)
		}
		if len(conv.Vars) > 0 {
			varsMap, ok := out["vars"].(map[string]any)
			if !ok {
				varsMap = make(map[string]any)
				out["vars"] = varsMap
			}
			for k, v := range conv.Vars {
				varsMap[k] = v
			}
		}
		if len(conv.Unconverted) > 0 {
			out["unconverted"] = map[string]any{"test": conv.Unconverted}
			report.Unconverted[ref] = conv.Unconverted
		}
	}

	return out
}

func ImportEnvironment(srcPath string, envDir string, nameOverride string) (string, string, error) {
	b, err := os.ReadFile(srcPath)
	if err != nil {
		return "", "", err
	}
	var data map[string]any
	if err := json.Unmarshal(b, &data); err != nil {
		return "", "", err
	}

	envName := nameOverride
	if envName == "" {
		if n, ok := data["name"].(string); ok {
			envName = n
		}
	}
	if envName == "" {
		envName = strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
	}
	slug := Slugify(envName, "env")

	_ = os.MkdirAll(envDir, 0755)
	envFile := filepath.Join(envDir, slug+".yaml")
	secretFile := filepath.Join(envDir, slug+".secrets.yaml")

	envVars := make(map[string]any)
	secretVars := make(map[string]any)

	if vals, ok := data["values"].([]any); ok {
		for _, item := range vals {
			vm, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if en, ok := vm["enabled"].(bool); ok && !en {
				continue
			}
			k, _ := vm["key"].(string)
			if k == "" {
				continue
			}
			v := vm["value"]
			if t, _ := vm["type"].(string); t == "secret" {
				secretVars[k] = v
			} else {
				envVars[k] = v
			}
		}
	}

	envData := map[string]any{"vars": envVars}
	for _, k := range []string{"baseUrl", "base_url"} {
		if v, ok := envVars[k]; ok {
			envData["base_url"] = v
			break
		}
	}

	yEnv, _ := zone.DumpYAML(envData)
	_ = os.WriteFile(envFile, []byte(yEnv), 0644)

	var writtenSecret string
	if len(secretVars) > 0 {
		ySec, _ := zone.DumpYAML(map[string]any{"vars": secretVars})
		_ = os.WriteFile(secretFile, []byte(ySec), 0644)
		writtenSecret = secretFile
	}

	return envFile, writtenSecret, nil
}

func ImportGlobals(srcPath string, zoneYAMLPath string) (int, error) {
	b, err := os.ReadFile(srcPath)
	if err != nil {
		return 0, err
	}
	var data map[string]any
	if err := json.Unmarshal(b, &data); err != nil {
		return 0, err
	}

	newVars := make(map[string]any)
	if vals, ok := data["values"].([]any); ok {
		for _, it := range vals {
			vm, ok := it.(map[string]any)
			if !ok {
				continue
			}
			if en, ok := vm["enabled"].(bool); ok && !en {
				continue
			}
			k, _ := vm["key"].(string)
			if k != "" {
				newVars[k] = vm["value"]
			}
		}
	}

	zoneConfig, err := zone.LoadYAML(zoneYAMLPath)
	if err != nil {
		zoneConfig = make(map[string]any)
	}

	existingVars, ok := zoneConfig["vars"].(map[string]any)
	if !ok {
		existingVars = make(map[string]any)
		zoneConfig["vars"] = existingVars
	}
	for k, v := range newVars {
		existingVars[k] = v
	}

	dumped, err := zone.DumpYAML(zoneConfig)
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(zoneYAMLPath, []byte(dumped), 0644); err != nil {
		return 0, err
	}
	return len(newVars), nil
}
