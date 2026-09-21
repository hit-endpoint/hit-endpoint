package shorthand

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	ShorthandsFile = "shorthands.yaml"
)

// Shorthand represents a named preset for hitting an endpoint with pre-configured settings.
type Shorthand struct {
	Name        string            `yaml:"name,omitempty" json:"name"`
	Description string            `yaml:"description,omitempty" json:"description,omitempty"`
	URL         string            `yaml:"url,omitempty" json:"url,omitempty"`
	Ref         string            `yaml:"ref,omitempty" json:"ref,omitempty"`
	Method      string            `yaml:"method,omitempty" json:"method,omitempty"`
	Server      string            `yaml:"server,omitempty" json:"server,omitempty"`
	Headers     map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Query       map[string]string `yaml:"query,omitempty" json:"query,omitempty"`
	Body        string            `yaml:"body,omitempty" json:"body,omitempty"`
}

// TargetDisplay returns a concise string describing the destination (URL or Ref).
func (s *Shorthand) TargetDisplay() string {
	if s.URL != "" {
		return s.URL
	}
	if s.Ref != "" {
		return s.Ref
	}
	return "<unspecified>"
}

// MethodDisplay returns the method or default "GET".
func (s *Shorthand) MethodDisplay() string {
	if s.Method != "" {
		return strings.ToUpper(s.Method)
	}
	return "GET"
}

// LoadAll loads and merges all defined shorthands in order:
// 1. Global (~/.hit/shorthands.yaml)
// 2. Zone root config (zone.yaml under `shorthands:`)
// 3. Zone root shorthands file (shorthands.yaml)
func LoadAll(zoneRoot string) (map[string]*Shorthand, error) {
	out := make(map[string]*Shorthand)

	// 1. Global shorthands
	if home, err := os.UserHomeDir(); err == nil {
		globalPath := filepath.Join(home, ".hit", ShorthandsFile)
		loadFromFile(globalPath, out)
	}

	if zoneRoot == "" {
		return out, nil
	}

	// 2. Zone config (zone.yaml)
	for _, cand := range []string{"zone.yaml"} {
		cfgPath := filepath.Join(zoneRoot, cand)
		if b, err := os.ReadFile(cfgPath); err == nil {
			var raw map[string]any
			if err := yaml.Unmarshal(b, &raw); err == nil {
				if shMap, ok := raw["shorthands"].(map[string]any); ok {
					for k, v := range shMap {
						if sh := parseShorthand(k, v); sh != nil {
							out[k] = sh
						}
					}
				}
			}
			break
		}
	}

	// 3. Zone shorthands.yaml
	zoneShorthandsPath := filepath.Join(zoneRoot, ShorthandsFile)
	loadFromFile(zoneShorthandsPath, out)

	return out, nil
}

func loadFromFile(path string, dst map[string]*Shorthand) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var raw map[string]any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return
	}
	for k, v := range raw {
		if sh := parseShorthand(k, v); sh != nil {
			dst[k] = sh
		}
	}
}

func parseShorthand(name string, val any) *Shorthand {
	sh := &Shorthand{Name: name}

	switch v := val.(type) {
	case string:
		// Bare string can be a URL or a request ref
		if strings.Contains(v, "://") || strings.HasPrefix(v, "localhost:") || strings.HasPrefix(v, "127.0.0.1:") {
			sh.URL = v
		} else {
			sh.Ref = v
		}
		return sh

	case map[string]any:
		if u, ok := v["url"].(string); ok {
			sh.URL = u
		}
		if r, ok := v["ref"].(string); ok {
			sh.Ref = r
		}
		if m, ok := v["method"].(string); ok {
			sh.Method = strings.ToUpper(m)
		}
		if s, ok := v["server"].(string); ok {
			sh.Server = s
		} else if env, ok := v["env"].(string); ok {
			sh.Server = env
		}
		if d, ok := v["description"].(string); ok {
			sh.Description = d
		}
		if b, ok := v["body"].(string); ok {
			sh.Body = b
		}
		if h, ok := v["headers"].(map[string]any); ok {
			sh.Headers = make(map[string]string)
			for hk, hv := range h {
				sh.Headers[hk] = fmt.Sprintf("%v", hv)
			}
		}
		if q, ok := v["query"].(map[string]any); ok {
			sh.Query = make(map[string]string)
			for qk, qv := range q {
				sh.Query[qk] = fmt.Sprintf("%v", qv)
			}
		}
		return sh
	}

	return nil
}

// Get finds a shorthand by name across all sources.
func Get(zoneRoot string, name string) (*Shorthand, bool) {
	all, err := LoadAll(zoneRoot)
	if err != nil {
		return nil, false
	}
	sh, ok := all[name]
	return sh, ok
}

// List returns all shorthands sorted by name.
func List(zoneRoot string) ([]*Shorthand, error) {
	all, err := LoadAll(zoneRoot)
	if err != nil {
		return nil, err
	}
	var res []*Shorthand
	for _, sh := range all {
		res = append(res, sh)
	}
	sort.Slice(res, func(i, j int) bool {
		return res[i].Name < res[j].Name
	})
	return res, nil
}

// Save saves or updates a shorthand in `<zoneRoot>/shorthands.yaml` (or `~/.hit/shorthands.yaml` if zoneRoot is empty).
func Save(zoneRoot string, sh *Shorthand) error {
	filePath := filepath.Join(zoneRoot, ShorthandsFile)
	if zoneRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		dir := filepath.Join(home, ".hit")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		filePath = filepath.Join(dir, ShorthandsFile)
	}

	existing := make(map[string]any)
	if b, err := os.ReadFile(filePath); err == nil {
		_ = yaml.Unmarshal(b, &existing)
	}
	if existing == nil {
		existing = make(map[string]any)
	}

	entry := map[string]any{}
	if sh.URL != "" {
		entry["url"] = sh.URL
	}
	if sh.Ref != "" {
		entry["ref"] = sh.Ref
	}
	if sh.Method != "" {
		entry["method"] = strings.ToUpper(sh.Method)
	}
	if sh.Server != "" {
		entry["server"] = sh.Server
	}
	if sh.Description != "" {
		entry["description"] = sh.Description
	}
	if len(sh.Headers) > 0 {
		entry["headers"] = sh.Headers
	}
	if len(sh.Query) > 0 {
		entry["query"] = sh.Query
	}
	if sh.Body != "" {
		entry["body"] = sh.Body
	}

	existing[sh.Name] = entry

	outBytes, err := yaml.Marshal(existing)
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, outBytes, 0644)
}

// Delete removes a shorthand from `<zoneRoot>/shorthands.yaml` or global file.
func Delete(zoneRoot string, name string) error {
	filePath := filepath.Join(zoneRoot, ShorthandsFile)
	if zoneRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		filePath = filepath.Join(home, ".hit", ShorthandsFile)
	}

	b, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("shorthand '%s' not found (file does not exist)", name)
	}

	var existing map[string]any
	if err := yaml.Unmarshal(b, &existing); err != nil {
		return err
	}

	if _, ok := existing[name]; !ok {
		return fmt.Errorf("shorthand '%s' not found in %s", name, filePath)
	}

	delete(existing, name)
	outBytes, err := yaml.Marshal(existing)
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, outBytes, 0644)
}
