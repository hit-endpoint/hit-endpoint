package spec

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"hit/internal/templating"
	"hit/internal/zone"
)

var (
	schemeRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)
	bodyKinds   = []string{"json", "raw", "form", "multipart", "graphql", "file"}
	knownKeys   = map[string]bool{
		"name": true, "method": true, "url": true, "headers": true, "query": true, "params": true,
		"body": true, "auth": true, "timeout": true, "follow_redirects": true, "verify": true,
		"tests": true, "captures": true, "vars": true, "hooks": true, "description": true,
	}
)

type MultipartFile struct {
	Filename    string
	Data        []byte
	ContentType string
}

type RequestSpec struct {
	Name            string
	Method          string
	Url             string
	Headers         map[string]any
	Query           map[string]any
	Body            map[string]any
	Auth            any
	Timeout         float64
	FollowRedirects bool
	Verify          any // bool or string (path to CA bundle)
	Tests           []map[string]any
	Captures        map[string]any
	Vars            map[string]any
	Hooks           map[string]string
	Description     string
	Path            string
	Extra           map[string]any
}

func NewRequestSpec() *RequestSpec {
	return &RequestSpec{
		Method:          "GET",
		Headers:         make(map[string]any),
		Query:           make(map[string]any),
		Timeout:         30.0,
		FollowRedirects: true,
		Verify:          true,
		Tests:           nil,
		Captures:        make(map[string]any),
		Vars:            make(map[string]any),
		Hooks:           make(map[string]string),
		Extra:           make(map[string]any),
	}
}

func (s *RequestSpec) BaseDir() string {
	if s.Path != "" {
		return filepath.Dir(s.Path)
	}
	cwd, _ := os.Getwd()
	return cwd
}

func NormaliseTests(tests any) ([]map[string]any, error) {
	if tests == nil {
		return nil, nil
	}
	if m, ok := tests.(map[string]any); ok {
		return []map[string]any{m}, nil
	}
	if list, ok := tests.([]any); ok {
		var out []map[string]any
		for _, item := range list {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			} else if str, ok := item.(string); ok {
				out = append(out, map[string]any{"expr": str})
			} else {
				return nil, zone.NewZoneError("Unsupported test entry: %v", item)
			}
		}
		return out, nil
	}
	return nil, zone.NewZoneError("'tests' must be a mapping or a list, got %T", tests)
}

func NormaliseBody(body any) (map[string]any, error) {
	if body == nil {
		return nil, nil
	}
	if str, ok := body.(string); ok {
		return map[string]any{"raw": str}, nil
	}
	if list, ok := body.([]any); ok {
		return map[string]any{"json": list}, nil
	}
	if m, ok := body.(map[string]any); ok {
		var kinds []string
		for _, k := range bodyKinds {
			if _, exists := m[k]; exists {
				kinds = append(kinds, k)
			}
		}
		if len(kinds) == 1 {
			return m, nil
		}
		if len(kinds) == 0 {
			// Bare mapping treated as JSON
			return map[string]any{"json": m}, nil
		}
		return nil, zone.NewZoneError("Body has more than one kind: %v", kinds)
	}
	return nil, zone.NewZoneError("Unsupported body: %v", body)
}

func MergeHeaders(base map[string]any, newHeaders map[string]any) map[string]any {
	out := make(map[string]any, len(base))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range newHeaders {
		// remove case-insensitive matches if different casing
		for existing := range out {
			if strings.EqualFold(existing, k) && existing != k {
				delete(out, existing)
			}
		}
		if v == nil {
			delete(out, k)
		} else {
			out[k] = v
		}
	}
	return out
}

func CloneSpec(s *RequestSpec) *RequestSpec {
	out := &RequestSpec{
		Name:            s.Name,
		Method:          s.Method,
		Url:             s.Url,
		Headers:         make(map[string]any, len(s.Headers)),
		Query:           make(map[string]any, len(s.Query)),
		Body:            s.Body,
		Auth:            s.Auth,
		Timeout:         s.Timeout,
		FollowRedirects: s.FollowRedirects,
		Verify:          s.Verify,
		Tests:           make([]map[string]any, len(s.Tests)),
		Captures:        make(map[string]any, len(s.Captures)),
		Vars:            make(map[string]any, len(s.Vars)),
		Hooks:           make(map[string]string, len(s.Hooks)),
		Description:     s.Description,
		Path:            s.Path,
		Extra:           make(map[string]any, len(s.Extra)),
	}
	for k, v := range s.Headers {
		out.Headers[k] = v
	}
	for k, v := range s.Query {
		out.Query[k] = v
	}
	copy(out.Tests, s.Tests)
	for k, v := range s.Captures {
		out.Captures[k] = v
	}
	for k, v := range s.Vars {
		out.Vars[k] = v
	}
	for k, v := range s.Hooks {
		out.Hooks[k] = v
	}
	for k, v := range s.Extra {
		out.Extra[k] = v
	}
	return out
}

func MergeLayer(spec *RequestSpec, layer map[string]any) (*RequestSpec, error) {
	s := CloneSpec(spec)
	if n, ok := layer["name"]; ok && n != nil {
		s.Name = fmt.Sprintf("%v", n)
	}
	if d, ok := layer["description"]; ok && d != nil {
		s.Description = fmt.Sprintf("%v", d)
	}
	if m, ok := layer["method"]; ok && m != nil {
		s.Method = strings.ToUpper(fmt.Sprintf("%v", m))
	}
	if u, ok := layer["url"]; ok && u != nil {
		s.Url = fmt.Sprintf("%v", u)
	}
	if h, ok := layer["headers"].(map[string]any); ok {
		s.Headers = MergeHeaders(s.Headers, h)
	}
	for _, qKey := range []string{"query", "params"} {
		if qm, ok := layer[qKey].(map[string]any); ok {
			for k, v := range qm {
				if v == nil {
					delete(s.Query, k)
				} else {
					s.Query[k] = v
				}
			}
		}
	}
	if b, ok := layer["body"]; ok {
		nb, err := NormaliseBody(b)
		if err != nil {
			return nil, err
		}
		s.Body = nb
	}
	if a, ok := layer["auth"]; ok {
		s.Auth = a
	}
	if t, ok := layer["timeout"]; ok && t != nil {
		switch tv := t.(type) {
		case float64:
			s.Timeout = tv
		case int:
			s.Timeout = float64(tv)
		case string:
			if f, err := strconv.ParseFloat(tv, 64); err == nil {
				s.Timeout = f
			}
		}
	}
	if fr, ok := layer["follow_redirects"]; ok && fr != nil {
		if b, ok := fr.(bool); ok {
			s.FollowRedirects = b
		}
	}
	if v, ok := layer["verify"]; ok {
		s.Verify = v
	}
	if t, ok := layer["tests"]; ok && t != nil {
		norm, err := NormaliseTests(t)
		if err != nil {
			return nil, err
		}
		s.Tests = append(s.Tests, norm...)
	}
	if c, ok := layer["captures"].(map[string]any); ok {
		for k, v := range c {
			s.Captures[k] = v
		}
	}
	if v, ok := layer["vars"].(map[string]any); ok {
		for k, val := range v {
			s.Vars[k] = val
		}
	}
	if h, ok := layer["hooks"]; ok && h != nil {
		if str, ok := h.(string); ok {
			s.Hooks["before"] = str
			s.Hooks["after"] = str
		} else if hm, ok := h.(map[string]any); ok {
			for k, v := range hm {
				if strVal, ok := v.(string); ok {
					s.Hooks[k] = strVal
				}
			}
		}
	}
	for k, v := range layer {
		if !knownKeys[k] {
			s.Extra[k] = v
		}
	}
	return s, nil
}

func LoadSpec(path string, defaults []map[string]any) (*RequestSpec, error) {
	data, err := zone.LoadYAML(path)
	if err != nil {
		return nil, err
	}
	if _, hasUrl := data["url"]; !hasUrl {
		if _, hasMethod := data["method"]; !hasMethod {
			return nil, zone.NewZoneError("%s: not a request (needs at least 'url')", path)
		}
	}

	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	spec := NewRequestSpec()
	spec.Path = path
	spec.Name = stem

	for _, d := range defaults {
		spec, err = MergeLayer(spec, d)
		if err != nil {
			return nil, err
		}
	}
	return MergeLayer(spec, data)
}

func SpecFromDict(data map[string]any, defaults []map[string]any, baseDir string) (*RequestSpec, error) {
	name := "ad hoc"
	if n, ok := data["name"].(string); ok && n != "" {
		name = n
	}
	spec := NewRequestSpec()
	if baseDir != "" {
		spec.Path = filepath.Join(baseDir, "adhoc.yaml")
	}
	spec.Name = name

	var err error
	for _, d := range defaults {
		spec, err = MergeLayer(spec, d)
		if err != nil {
			return nil, err
		}
	}
	return MergeLayer(spec, data)
}

type Prepared struct {
	Method          string
	Url             string
	Params          map[string]any
	Headers         map[string]string
	Content         []byte
	Data            map[string]string
	Files           map[string]MultipartFile
	Timeout         float64
	FollowRedirects bool
	Verify          any
	BodyPreview     string
	Notes           []string
}

func (p *Prepared) Header(name string) string {
	for k, v := range p.Headers {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

func (p *Prepared) SetHeaderDefault(name, val string) {
	if p.Header(name) == "" {
		p.Headers[name] = val
	}
}

func (p *Prepared) FullURL() string {
	if len(p.Params) == 0 {
		return p.Url
	}
	joiner := "?"
	if strings.Contains(p.Url, "?") {
		joiner = "&"
	}
	vals := url.Values{}
	var keys []string
	for k := range p.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := p.Params[k]
		if list, ok := v.([]any); ok {
			for _, item := range list {
				vals.Add(k, fmt.Sprintf("%v", item))
			}
		} else {
			vals.Add(k, fmt.Sprintf("%v", v))
		}
	}
	return p.Url + joiner + vals.Encode()
}

func (p *Prepared) ToCurl(ctx *templating.Context) string {
	var lines []string
	fullURL := p.FullURL()
	lines = append(lines, fmt.Sprintf("curl -X %s %q", p.Method, fullURL))

	var headerKeys []string
	for k := range p.Headers {
		headerKeys = append(headerKeys, k)
	}
	sort.Strings(headerKeys)
	for _, k := range headerKeys {
		v := p.Headers[k]
		shown := v
		if ctx != nil {
			shown = ctx.Mask(v)
		}
		lines = append(lines, fmt.Sprintf("-H %q", fmt.Sprintf("%s: %s", k, shown)))
	}

	if p.Content != nil {
		shownContent := string(p.Content)
		if ctx != nil {
			shownContent = ctx.Mask(shownContent)
		}
		lines = append(lines, fmt.Sprintf("--data-raw %q", shownContent))
	} else if len(p.Data) > 0 {
		var dataKeys []string
		for k := range p.Data {
			dataKeys = append(dataKeys, k)
		}
		sort.Strings(dataKeys)
		for _, k := range dataKeys {
			lines = append(lines, fmt.Sprintf("-d %q", fmt.Sprintf("%s=%s", k, p.Data[k])))
		}
	}

	if len(p.Files) > 0 {
		var fileKeys []string
		for k := range p.Files {
			fileKeys = append(fileKeys, k)
		}
		sort.Strings(fileKeys)
		for _, k := range fileKeys {
			lines = append(lines, fmt.Sprintf("-F %q", fmt.Sprintf("%s=@%s", k, p.Files[k].Filename)))
		}
	}

	if p.FollowRedirects {
		lines = append(lines, "-L")
	}

	return strings.Join(lines, " \\\n  ")
}

func RenderSpec(spec *RequestSpec, ctx *templating.Context, strict bool) (*RequestSpec, error) {
	rendered := CloneSpec(spec)

	u, err := templating.Render(spec.Url, ctx, strict)
	if err != nil {
		return nil, err
	}
	rendered.Url = fmt.Sprintf("%v", u)

	h, err := templating.Render(spec.Headers, ctx, strict)
	if err != nil {
		return nil, err
	}
	if hm, ok := h.(map[string]any); ok {
		rendered.Headers = hm
	}

	q, err := templating.Render(spec.Query, ctx, strict)
	if err != nil {
		return nil, err
	}
	if qm, ok := q.(map[string]any); ok {
		rendered.Query = qm
	}

	if spec.Body != nil {
		b, err := templating.Render(spec.Body, ctx, strict)
		if err != nil {
			return nil, err
		}
		if bm, ok := b.(map[string]any); ok {
			rendered.Body = bm
		}
	}

	if spec.Auth != nil {
		a, err := templating.Render(spec.Auth, ctx, strict)
		if err != nil {
			return nil, err
		}
		rendered.Auth = a
	}

	if len(spec.Tests) > 0 {
		var testsAny []any
		for _, t := range spec.Tests {
			testsAny = append(testsAny, t)
		}
		rt, err := templating.Render(testsAny, ctx, strict)
		if err != nil {
			return nil, err
		}
		if list, ok := rt.([]any); ok {
			var newTests []map[string]any
			for _, item := range list {
				if m, ok := item.(map[string]any); ok {
					newTests = append(newTests, m)
				}
			}
			rendered.Tests = newTests
		}
	}

	return rendered, nil
}

func Prepare(spec *RequestSpec, ctx *templating.Context, envAuth any) (*Prepared, error) {
	u := spec.Url
	if u == "" {
		where := ""
		if spec.Path != "" {
			where = fmt.Sprintf(" in %s", spec.Path)
		}
		return nil, zone.NewZoneError("request '%s' has no url%s", spec.Name, where)
	}

	if !schemeRegex.MatchString(u) {
		base := ctx.Get("base_url", nil)
		if base == nil || fmt.Sprintf("%v", base) == "" {
			return nil, zone.NewZoneError("URL '%s' is relative and no 'base_url' variable is defined for this server", u)
		}
		baseStr := strings.TrimRight(fmt.Sprintf("%v", base), "/")
		u = baseStr + "/" + strings.TrimLeft(u, "/")
	}

	params := make(map[string]any)
	for k, v := range spec.Query {
		if v != nil {
			params[k] = v
		}
	}

	headers := make(map[string]string)
	for k, v := range spec.Headers {
		if v != nil {
			headers[k] = fmt.Sprintf("%v", v)
		}
	}

	p := &Prepared{
		Method:          spec.Method,
		Url:             u,
		Params:          params,
		Headers:         headers,
		Timeout:         spec.Timeout,
		FollowRedirects: spec.FollowRedirects,
		Verify:          spec.Verify,
	}

	if err := applyAuth(p, spec.Auth, envAuth, ctx); err != nil {
		return nil, err
	}
	if err := applyBody(p, spec); err != nil {
		return nil, err
	}
	return p, nil
}

func applyAuth(p *Prepared, auth any, envAuth any, ctx *templating.Context) error {
	if auth == nil || auth == "inherit" {
		if envAuth == nil {
			return nil
		}
		rAuth, err := templating.Render(envAuth, ctx, false)
		if err != nil {
			return err
		}
		if len(ctx.Missing) > 0 {
			var missing []string
			for n := range ctx.Missing {
				missing = append(missing, "{{"+n+"}}")
			}
			sort.Strings(missing)
			p.Notes = append(p.Notes, "environment auth skipped: "+strings.Join(missing, ", ")+" not set yet (run the request that captures it)")
			return nil
		}
		auth = rAuth
	}

	if auth == nil || auth == "none" {
		return nil
	}

	authStr, isStr := auth.(string)
	if isStr {
		if authStr == "none" {
			return nil
		}
		return zone.NewZoneError("Unknown auth shorthand '%s'. Use none, inherit, or a mapping with 'type'.", authStr)
	}

	authMap, ok := auth.(map[string]any)
	if !ok {
		return nil
	}

	kind := strings.ToLower(fmt.Sprintf("%v", authMap["type"]))
	if kind == "none" || kind == "noauth" {
		return nil
	}

	switch kind {
	case "bearer":
		token := fmt.Sprintf("%v", authMap["token"])
		prefix := "Bearer"
		if pr, ok := authMap["prefix"].(string); ok && pr != "" {
			prefix = pr
		}
		p.Headers["Authorization"] = strings.TrimSpace(fmt.Sprintf("%s %s", prefix, token))

	case "basic":
		user := fmt.Sprintf("%v", authMap["username"])
		pass := fmt.Sprintf("%v", authMap["password"])
		raw := []byte(fmt.Sprintf("%s:%s", user, pass))
		p.Headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString(raw)

	case "apikey", "api_key", "api-key":
		key := "X-API-Key"
		if k, ok := authMap["key"].(string); ok && k != "" {
			key = k
		} else if k, ok := authMap["name"].(string); ok && k != "" {
			key = k
		}
		val := fmt.Sprintf("%v", authMap["value"])
		inLoc := "header"
		if inStr, ok := authMap["in"].(string); ok {
			inLoc = strings.ToLower(inStr)
		}
		if inLoc == "query" {
			p.Params[key] = val
		} else {
			p.Headers[key] = val
		}

	case "header":
		if hm, ok := authMap["headers"].(map[string]any); ok {
			for k, v := range hm {
				p.Headers[k] = fmt.Sprintf("%v", v)
			}
		}

	default:
		return zone.NewZoneError("Unsupported auth type '%s' (bearer, basic, apikey, header, none)", kind)
	}

	return nil
}

func applyBody(p *Prepared, spec *RequestSpec) error {
	body := spec.Body
	if len(body) == 0 {
		return nil
	}

	var kind string
	for _, k := range bodyKinds {
		if _, ok := body[k]; ok {
			kind = k
			break
		}
	}
	if kind == "" {
		return nil
	}

	val := body[kind]
	switch kind {
	case "json":
		b, err := json.Marshal(val)
		if err != nil {
			return err
		}
		p.Content = b
		p.SetHeaderDefault("Content-Type", "application/json")
		var buf bytes.Buffer
		if err := json.Indent(&buf, b, "", "  "); err == nil {
			p.BodyPreview = buf.String()
		} else {
			p.BodyPreview = string(b)
		}

	case "raw":
		text := fmt.Sprintf("%v", val)
		p.Content = []byte(text)
		ct := "text/plain"
		if c, ok := body["content_type"].(string); ok && c != "" {
			ct = c
		}
		p.SetHeaderDefault("Content-Type", ct)
		p.BodyPreview = text

	case "form":
		data := make(map[string]string)
		if fm, ok := val.(map[string]any); ok {
			for k, v := range fm {
				if v == nil {
					data[k] = ""
				} else {
					data[k] = fmt.Sprintf("%v", v)
				}
			}
		}
		p.Data = data
		p.SetHeaderDefault("Content-Type", "application/x-www-form-urlencoded")
		vals := url.Values{}
		for k, v := range data {
			vals.Add(k, v)
		}
		p.BodyPreview = vals.Encode()

	case "multipart":
		data := make(map[string]string)
		files := make(map[string]MultipartFile)
		if mm, ok := val.(map[string]any); ok {
			for k, v := range mm {
				if vm, ok := v.(map[string]any); ok && vm["file"] != nil {
					filePath := filepath.Join(spec.BaseDir(), fmt.Sprintf("%v", vm["file"]))
					fileBytes, err := os.ReadFile(filePath)
					if err != nil {
						return zone.NewZoneError("multipart file not found: %s", filePath)
					}
					ct := "application/octet-stream"
					if c, ok := vm["content_type"].(string); ok && c != "" {
						ct = c
					}
					files[k] = MultipartFile{
						Filename:    filepath.Base(filePath),
						Data:        fileBytes,
						ContentType: ct,
					}
				} else {
					if v == nil {
						data[k] = ""
					} else {
						data[k] = fmt.Sprintf("%v", v)
					}
				}
			}
		}
		p.Data = data
		p.Files = files
		var prevParts []string
		for k := range data {
			prevParts = append(prevParts, k)
		}
		for k, f := range files {
			prevParts = append(prevParts, fmt.Sprintf("%s=@%s", k, f.Filename))
		}
		p.BodyPreview = "multipart: " + strings.Join(prevParts, ", ")

	case "graphql":
		payload := make(map[string]any)
		if gm, ok := val.(map[string]any); ok {
			payload["query"] = gm["query"]
			if gm["variables"] != nil {
				payload["variables"] = gm["variables"]
			}
			if gm["operationName"] != nil {
				payload["operationName"] = gm["operationName"]
			}
		} else {
			payload["query"] = fmt.Sprintf("%v", val)
		}
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		p.Content = b
		p.SetHeaderDefault("Content-Type", "application/json")
		var buf bytes.Buffer
		if err := json.Indent(&buf, b, "", "  "); err == nil {
			p.BodyPreview = buf.String()
		} else {
			p.BodyPreview = string(b)
		}

	case "file":
		filePath := filepath.Join(spec.BaseDir(), fmt.Sprintf("%v", val))
		fileBytes, err := os.ReadFile(filePath)
		if err != nil {
			return zone.NewZoneError("body file not found: %s", filePath)
		}
		p.Content = fileBytes
		ct := "application/octet-stream"
		if c, ok := body["content_type"].(string); ok && c != "" {
			ct = c
		}
		p.SetHeaderDefault("Content-Type", ct)
		p.BodyPreview = fmt.Sprintf("<%d bytes from %s>", len(fileBytes), filepath.Base(filePath))
	}

	return nil
}

func BuildMultipartBody(data map[string]string, files map[string]MultipartFile) ([]byte, string, error) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	for k, v := range data {
		if err := w.WriteField(k, v); err != nil {
			return nil, "", err
		}
	}
	for k, f := range files {
		part, err := w.CreateFormFile(k, f.Filename)
		if err != nil {
			return nil, "", err
		}
		if _, err := part.Write(f.Data); err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return b.Bytes(), w.FormDataContentType(), nil
}
