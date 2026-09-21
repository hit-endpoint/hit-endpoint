package zone

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	ZoneFile     = "zone.yaml"
	DefaultsFile = "_defaults.yaml"
	StateDir     = ".hit"
)

var (
	YamlSuffixes = []string{".yaml", ".yml"}
	orderPrefix  = regexp.MustCompile(`^\d+[-_.]\s*`)
)

type ZoneError struct {
	Message string
}

func (e *ZoneError) Error() string {
	return e.Message
}

func NewZoneError(format string, a ...any) *ZoneError {
	return &ZoneError{Message: fmt.Sprintf(format, a...)}
}

func LoadYAML(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, NewZoneError("%s: failed to parse YAML: %v", path, err)
	}
	if out == nil {
		out = make(map[string]any)
	}
	return out, nil
}

func DumpYAML(data any) (string, error) {
	b, err := yaml.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func StripOrder(segment string) string {
	return orderPrefix.ReplaceAllString(segment, "")
}

func isYAMLSuffix(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

type Server struct {
	Name    string         `json:"name"`
	Vars    map[string]any `json:"vars"`
	Auth    any            `json:"auth"`
	Secrets map[string]bool
	Path    string         `json:"path"`
}

type StateStore struct {
	Path string
	data map[string]any
}

func NewStateStore(path string) *StateStore {
	return &StateStore{Path: path}
}

func (s *StateStore) Load() map[string]any {
	if s.data != nil {
		return s.data
	}
	s.data = make(map[string]any)
	if b, err := os.ReadFile(s.Path); err == nil {
		_ = json.Unmarshal(b, &s.data)
	}
	return s.data
}

func (s *StateStore) Save() error {
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	b, err := json.MarshalIndent(s.Load(), "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

func (s *StateStore) Set(key string, val any) {
	s.Load()[key] = val
}

func (s *StateStore) Update(m map[string]any) {
	d := s.Load()
	for k, v := range m {
		d[k] = v
	}
}

func (s *StateStore) Unset(key string) bool {
	d := s.Load()
	if _, ok := d[key]; ok {
		delete(d, key)
		return true
	}
	return false
}

func (s *StateStore) Clear() {
	s.data = make(map[string]any)
	_ = os.Remove(s.Path)
}

type Zone struct {
	Root   string
	Config map[string]any
}

func Find(start string) (*Zone, error) {
	z, err := FindOrNone(start)
	if err != nil {
		return nil, err
	}
	if z == nil {
		cwd, _ := os.Getwd()
		if start != "" {
			cwd = start
		}
		abs, _ := filepath.Abs(cwd)
		return nil, NewZoneError("No %s found in %s or any parent. Run 'hit wizard' to create a zone, or pass -z/--zone.", ZoneFile, abs)
	}
	return z, nil
}

func FindOrNone(start string) (*Zone, error) {
	target := start
	if target == "" {
		var err error
		target, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return nil, err
	}

	fi, err := os.Stat(abs)
	if err == nil && !fi.IsDir() {
		abs = filepath.Dir(abs)
	}

	curr := abs
	for {
		candidate := filepath.Join(curr, ZoneFile)
		if s, err := os.Stat(candidate); err == nil && !s.IsDir() {
			cfg, err := LoadYAML(candidate)
			if err != nil {
				return nil, err
			}
			return &Zone{
				Root:   curr,
				Config: cfg,
			}, nil
		}
		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}
	return nil, nil
}

func (z *Zone) Name() string {
	if n, ok := z.Config["name"].(string); ok && n != "" {
		return n
	}
	return filepath.Base(z.Root)
}

func (z *Zone) CollectionsDir() string {
	d := "collections"
	if s, ok := z.Config["collections_dir"].(string); ok && s != "" {
		d = s
	}
	return filepath.Join(z.Root, d)
}

func (z *Zone) ServersDir() string {
	if s, ok := z.Config["servers_dir"].(string); ok && s != "" {
		return filepath.Join(z.Root, s)
	}
	return filepath.Join(z.Root, "servers")
}

func (z *Zone) ChainsDir() string {
	if s, ok := z.Config["chains_dir"].(string); ok && s != "" {
		return filepath.Join(z.Root, s)
	}
	cand := filepath.Join(z.Root, "chains")
	if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
		return cand
	}
	return filepath.Join(z.Root, "chains")
}

func (z *Zone) Vars() map[string]any {
	if v, ok := z.Config["vars"].(map[string]any); ok {
		out := make(map[string]any, len(v))
		for k, val := range v {
			out[k] = val
		}
		return out
	}
	return make(map[string]any)
}

func (z *Zone) ServerNames() []string {
	dir := z.ServersDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "_") || strings.Contains(name, ".secrets") {
			continue
		}
		if isYAMLSuffix(name) {
			stem := strings.TrimSuffix(name, filepath.Ext(name))
			names = append(names, stem)
		}
	}
	sort.Strings(names)
	return names
}

func (z *Zone) LoadServer(name string) (*Server, error) {
	if name == "" {
		if def, ok := z.Config["default_server"].(string); ok && def != "" {
			name = def
		}
	}
	if name == "" {
		return &Server{Name: "none", Vars: make(map[string]any), Secrets: make(map[string]bool)}, nil
	}

	serverDir := z.ServersDir()
	var serverPath string
	for _, suffix := range YamlSuffixes {
		cand := filepath.Join(serverDir, name+suffix)
		if s, err := os.Stat(cand); err == nil && !s.IsDir() {
			serverPath = cand
			break
		}
	}

	if serverPath == "" {
		known := strings.Join(z.ServerNames(), ", ")
		if known == "" {
			known = "(none defined)"
		}
		return nil, NewZoneError("Server '%s' not found in %s. Known: %s", name, serverDir, known)
	}

	data, err := LoadYAML(serverPath)
	if err != nil {
		return nil, err
	}

	serverVars := make(map[string]any)
	if v, ok := data["vars"].(map[string]any); ok {
		for k, val := range v {
			serverVars[k] = val
		}
	}
	if bu, ok := data["base_url"]; ok {
		serverVars["base_url"] = bu
	}

	secrets := make(map[string]bool)
	for _, suffix := range YamlSuffixes {
		secPath := filepath.Join(serverDir, name+".secrets"+suffix)
		if s, err := os.Stat(secPath); err == nil && !s.IsDir() {
			secData, err := LoadYAML(secPath)
			if err == nil {
				sv, ok := secData["vars"].(map[string]any)
				if !ok {
					sv = secData
				}
				for k, v := range sv {
					serverVars[k] = v
					if strVal, ok := v.(string); ok && strVal != "" {
						secrets[strVal] = true
					}
				}
			}
		}
	}

	return &Server{
		Name:    name,
		Vars:    serverVars,
		Auth:    data["auth"],
		Secrets: secrets,
		Path:    serverPath,
	}, nil
}

func (z *Zone) ListRequests(under string) []string {
	base := under
	if base == "" {
		base = z.CollectionsDir()
	}
	if s, err := os.Stat(base); err != nil || !s.IsDir() {
		return nil
	}

	var found []string
	_ = filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if isYAMLSuffix(path) && !strings.HasPrefix(filepath.Base(path), "_") {
			found = append(found, path)
		}
		return nil
	})

	sort.Slice(found, func(i, j int) bool {
		relI, _ := filepath.Rel(base, found[i])
		relJ, _ := filepath.Rel(base, found[j])
		return strings.ToLower(relI) < strings.ToLower(relJ)
	})
	return found
}

func (z *Zone) RequestRef(path string) string {
	collDir := z.CollectionsDir()
	rel, err := filepath.Rel(collDir, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return strings.TrimSuffix(path, filepath.Ext(path))
	}
	ref := strings.TrimSuffix(rel, filepath.Ext(rel))
	return filepath.ToSlash(ref)
}

func (z *Zone) ResolveRequest(ref string) (string, error) {
	candidates := []string{
		ref,
		filepath.Join(z.CollectionsDir(), ref),
		filepath.Join(z.Root, ref),
	}
	for _, c := range append([]string{}, candidates...) {
		if !isYAMLSuffix(c) {
			for _, s := range YamlSuffixes {
				candidates = append(candidates, c+s)
			}
		}
	}
	for _, c := range candidates {
		if s, err := os.Stat(c); err == nil {
			if s.IsDir() || !strings.HasPrefix(filepath.Base(c), "_") {
				return c, nil
			}
		}
	}

	// Fuzzy search ignoring numeric order prefixes
	parts := strings.Split(strings.Trim(filepath.ToSlash(ref), "/"), "/")
	var stripped []string
	for _, p := range parts {
		stripped = append(stripped, StripOrder(p))
	}
	needle := strings.Join(stripped, "/")
	needle = strings.TrimSuffix(needle, ".yaml")
	needle = strings.TrimSuffix(needle, ".yml")
	needle = strings.ToLower(needle)

	var matches []string
	for _, p := range z.ListRequests("") {
		rel, err := filepath.Rel(z.CollectionsDir(), p)
		if err != nil {
			continue
		}
		relWithoutExt := strings.TrimSuffix(rel, filepath.Ext(rel))
		relParts := strings.Split(filepath.ToSlash(relWithoutExt), "/")
		var cleanParts []string
		for _, seg := range relParts {
			cleanParts = append(cleanParts, StripOrder(seg))
		}
		clean := strings.ToLower(strings.Join(cleanParts, "/"))
		if clean == needle || strings.HasSuffix(clean, "/"+needle) {
			matches = append(matches, p)
		}
	}

	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		var list []string
		for _, m := range matches {
			list = append(list, "  "+z.RequestRef(m))
		}
		return "", NewZoneError("'%s' is ambiguous, matches:\n%s", ref, strings.Join(list, "\n"))
	}
	return "", NewZoneError("No request or folder matches '%s' (looked in %s)", ref, z.CollectionsDir())
}

func (z *Zone) ResolveChain(ref string) (string, error) {
	candidates := []string{
		ref,
		filepath.Join(z.ChainsDir(), ref),
		filepath.Join(z.Root, ref),
	}
	if strings.HasPrefix(ref, "chains/") {
		candidates = append(candidates, filepath.Join(z.Root, "chains", strings.TrimPrefix(ref, "chains/")))
	}
	for _, c := range append([]string{}, candidates...) {
		if !isYAMLSuffix(c) {
			for _, s := range YamlSuffixes {
				candidates = append(candidates, c+s)
			}
		}
	}
	for _, c := range candidates {
		if s, err := os.Stat(c); err == nil && !s.IsDir() && isYAMLSuffix(c) {
			data, err := LoadYAML(c)
			if err == nil {
				if _, ok := data["steps"]; ok {
					return c, nil
				}
			}
		}
	}
	return "", NewZoneError("No chain matches '%s' (looked in %s)", ref, z.ChainsDir())
}

func (z *Zone) ListChains() []string {
	dir := z.ChainsDir()
	if s, err := os.Stat(dir); err != nil || !s.IsDir() {
		return nil
	}
	var found []string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if isYAMLSuffix(path) {
			found = append(found, path)
		}
		return nil
	})
	sort.Strings(found)
	return found
}

func (z *Zone) DefaultsChain(requestPath string) []map[string]any {
	var chain []map[string]any
	if def, ok := z.Config["defaults"].(map[string]any); ok {
		cpy := make(map[string]any, len(def))
		for k, v := range def {
			cpy[k] = v
		}
		chain = append(chain, cpy)
	}

	collDir := z.CollectionsDir()
	rel, err := filepath.Rel(collDir, filepath.Dir(requestPath))
	if err != nil || strings.HasPrefix(rel, "..") {
		f := filepath.Join(filepath.Dir(requestPath), DefaultsFile)
		if s, err := os.Stat(f); err == nil && !s.IsDir() {
			if d, err := LoadYAML(f); err == nil {
				chain = append(chain, d)
			}
		}
		return chain
	}

	parts := strings.Split(filepath.ToSlash(rel), "/")
	folder := collDir
	checkFiles := []string{filepath.Join(folder, DefaultsFile)}
	for _, part := range parts {
		if part != "" && part != "." {
			folder = filepath.Join(folder, part)
			checkFiles = append(checkFiles, filepath.Join(folder, DefaultsFile))
		}
	}

	for _, f := range checkFiles {
		if s, err := os.Stat(f); err == nil && !s.IsDir() {
			if d, err := LoadYAML(f); err == nil {
				chain = append(chain, d)
			}
		}
	}
	return chain
}

func (z *Zone) State(serverName string) *StateStore {
	if serverName == "" {
		serverName = "default"
	}
	p := filepath.Join(z.Root, StateDir, "state", serverName+".json")
	return NewStateStore(p)
}

func Init(root string, name string) (*Zone, error) {
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, err
	}
	marker := filepath.Join(root, ZoneFile)
	if _, err := os.Stat(marker); err == nil {
		return nil, NewZoneError("%s already exists", marker)
	}

	if name == "" {
		name = filepath.Base(root)
	}

	cfg := map[string]any{
		"name":           name,
		"default_server": "dev",
		"vars": map[string]any{
			"app_name": name,
		},
	}
	cfgYAML, err := DumpYAML(cfg)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(marker, []byte(cfgYAML), 0644); err != nil {
		return nil, err
	}

	serverDir := filepath.Join(root, "servers")
	_ = os.MkdirAll(serverDir, 0755)
	devYAML, _ := DumpYAML(map[string]any{
		"base_url": "https://httpbin.org",
		"vars": map[string]any{
			"example_var": "hello",
		},
		"auth": map[string]any{
			"type": "none",
		},
	})
	_ = os.WriteFile(filepath.Join(serverDir, "dev.yaml"), []byte(devYAML), 0644)

	exDir := filepath.Join(root, "collections", "example")
	_ = os.MkdirAll(exDir, 0755)
	defYAML, _ := DumpYAML(map[string]any{
		"headers": map[string]any{
			"Accept": "application/json",
		},
	})
	_ = os.WriteFile(filepath.Join(exDir, "_defaults.yaml"), []byte(defYAML), 0644)

	getReq, _ := DumpYAML(map[string]any{
		"name":   "Example GET",
		"method": "GET",
		"url":    "{{base_url}}/get",
		"query": map[string]any{
			"greeting": "{{example_var}}",
		},
		"tests": []any{
			map[string]any{"status": 200},
			map[string]any{"json": map[string]any{"args.greeting": "hello"}},
		},
		"captures": map[string]any{
			"my_origin": "json.origin",
		},
	})
	_ = os.WriteFile(filepath.Join(exDir, "01-get.yaml"), []byte(getReq), 0644)

	_ = os.MkdirAll(filepath.Join(root, "chains"), 0755)

	gi := filepath.Join(root, ".gitignore")
	if _, err := os.Stat(gi); os.IsNotExist(err) {
		_ = os.WriteFile(gi, []byte(".hit/\n*.secrets.yaml\n*.secrets.yml\n"), 0644)
	}

	return &Zone{
		Root:   root,
		Config: cfg,
	}, nil
}
