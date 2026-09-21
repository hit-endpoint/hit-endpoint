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

	"hit/internal/zone"
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
	Unconverted []string
}

func NewScriptConverter(lines []string) *ScriptConverter {
	return &ScriptConverter{
		Lines:    lines,
		Captures: make(map[string]any),
	}
}

func (c *ScriptConverter) Convert() {
	var curTest *map[string]any

	flushTest := func() {
		if curTest != nil {
			c.Tests = append(c.Tests, *curTest)
			curTest = nil
		}
	}

	pmTestRe := regexp.MustCompile(`pm\.test\(\s*["'](.*?)["']`)
	statusRe := regexp.MustCompile(`pm\.response\.to\.(?:have\.)?status\((\d+)\)`)
	jsonTestRe := regexp.MustCompile(`pm\.response\.to\.(?:be\.)?json`)
	timeBelowRe := regexp.MustCompile(`pm\.response\.responseTime\)\.to\.be\.below\((\d+)\)`)
	expectTypeRe := regexp.MustCompile(`pm\.expect\((.*?)\)\.to\.be\.a\(["'](.*?)["']\)`)
	expectAboveRe := regexp.MustCompile(`pm\.expect\((.*?)\)\.to\.be\.above\((\d+)\)`)
	expectEqlRe := regexp.MustCompile(`pm\.expect\((.*?)\)\.to\.eql\(["']?(.*?)["']?\)`)
	expectLenRe := regexp.MustCompile(`pm\.expect\((.*?)\)\.to\.have\.lengthOf\((\d+)\)`)
	envSetRe := regexp.MustCompile(`pm\.(?:environment|collectionVariables)\.set\(["'](.*?)["'],\s*(.*?)\)`)

	for _, line := range c.Lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "});" || trimmed == "}" || trimmed == "var jsonData = pm.response.json();" {
			continue
		}

		if m := pmTestRe.FindStringSubmatch(trimmed); len(m) > 1 {
			flushTest()
			t := map[string]any{"name": m[1]}
			curTest = &t
			continue
		}

		if m := statusRe.FindStringSubmatch(trimmed); len(m) > 1 {
			code, _ := strconv.Atoi(m[1])
			if curTest != nil {
				(*curTest)["status"] = code
			} else {
				c.Tests = append(c.Tests, map[string]any{"status": code})
			}
			continue
		}

		if jsonTestRe.MatchString(trimmed) {
			target := curTest
			if target == nil {
				t := map[string]any{}
				target = &t
				defer flushTest()
			}
			(*target)["headers"] = map[string]any{
				"content-type": map[string]any{"contains": "json"},
			}
			continue
		}

		if m := timeBelowRe.FindStringSubmatch(trimmed); len(m) > 1 {
			ms, _ := strconv.Atoi(m[1])
			if curTest != nil {
				(*curTest)["max_ms"] = ms
			} else {
				c.Tests = append(c.Tests, map[string]any{"max_ms": ms})
			}
			continue
		}

		if m := expectTypeRe.FindStringSubmatch(trimmed); len(m) > 2 {
			path := cleanJSONPath(m[1])
			typeName := m[2]
			c.addJSONMatcher(curTest, path, map[string]any{"type": typeName})
			continue
		}

		if m := expectAboveRe.FindStringSubmatch(trimmed); len(m) > 2 {
			path := cleanJSONPath(m[1])
			val, _ := strconv.Atoi(m[2])
			c.addJSONMatcher(curTest, path, map[string]any{"gt": val})
			continue
		}

		if m := expectEqlRe.FindStringSubmatch(trimmed); len(m) > 2 {
			path := cleanJSONPath(m[1])
			c.addJSONMatcher(curTest, path, m[2])
			continue
		}

		if m := expectLenRe.FindStringSubmatch(trimmed); len(m) > 2 {
			path := cleanJSONPath(m[1])
			l, _ := strconv.Atoi(m[2])
			c.addJSONMatcher(curTest, path, map[string]any{"length": l})
			continue
		}

		if m := envSetRe.FindStringSubmatch(trimmed); len(m) > 2 {
			key := m[1]
			rhs := cleanJSONPath(strings.TrimSuffix(m[2], ";"))
			c.Captures[key] = rhs
			continue
		}

		c.Unconverted = append(c.Unconverted, line)
	}

	flushTest()
}

func cleanJSONPath(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "jsonData.")
	s = strings.TrimPrefix(s, "pm.response.json().")
	if !strings.HasPrefix(s, "json.") {
		s = "json." + s
	}
	return s
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
	if events, ok := item["event"].([]any); ok {
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
			}
		}
		if len(scriptLines) > 0 {
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
			if len(conv.Unconverted) > 0 {
				out["unconverted"] = map[string]any{"test": conv.Unconverted}
				report.Unconverted[ref] = conv.Unconverted
			}
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
