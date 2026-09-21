package templating

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const MaxDepth = 10

var varRegex = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

type MissingVariableError struct {
	Names []string
}

func (e *MissingVariableError) Error() string {
	return "Unresolved variable(s): " + strings.Join(e.Names, ", ")
}

type Layer struct {
	Name   string
	Values map[string]any
}

type Context struct {
	Layers  []Layer
	Secrets map[string]bool
	Missing map[string]bool
}

func NewContext(layers []Layer, secrets map[string]bool) *Context {
	ctx := &Context{
		Layers:  make([]Layer, len(layers)),
		Secrets: make(map[string]bool),
		Missing: make(map[string]bool),
	}
	copy(ctx.Layers, layers)

	hasSession := false
	for _, l := range ctx.Layers {
		if l.Name == "session" {
			hasSession = true
			break
		}
	}
	if !hasSession {
		ctx.Layers = append(ctx.Layers, Layer{Name: "session", Values: make(map[string]any)})
	}

	for k, v := range secrets {
		if v && k != "" {
			ctx.Secrets[k] = true
		}
	}
	return ctx
}

func (c *Context) Push(name string, values map[string]any) {
	cpy := make(map[string]any, len(values))
	for k, v := range values {
		cpy[k] = v
	}
	c.Layers = append(c.Layers, Layer{Name: name, Values: cpy})
}

func (c *Context) Pop(name string) {
	for i := len(c.Layers) - 1; i >= 0; i-- {
		if c.Layers[i].Name == name {
			c.Layers = append(c.Layers[:i], c.Layers[i+1:]...)
			return
		}
	}
}

func (c *Context) Layer(name string) map[string]any {
	for _, l := range c.Layers {
		if l.Name == name {
			return l.Values
		}
	}
	return nil
}

func (c *Context) Lookup(name string) (any, bool) {
	for i := len(c.Layers) - 1; i >= 0; i-- {
		if val, ok := c.Layers[i].Values[name]; ok {
			return val, true
		}
	}
	return nil, false
}

func (c *Context) Get(name string, def any) any {
	if val, ok := c.Lookup(name); ok {
		return val
	}
	return def
}

func (c *Context) Set(name string, val any) {
	sess := c.Layer("session")
	if sess == nil {
		sess = make(map[string]any)
		c.Layers = append(c.Layers, Layer{Name: "session", Values: sess})
	}
	sess[name] = val
}

func (c *Context) Flat() map[string]any {
	out := make(map[string]any)
	for _, l := range c.Layers {
		for k, v := range l.Values {
			out[k] = v
		}
	}
	return out
}

func (c *Context) SourceOf(name string) string {
	for i := len(c.Layers) - 1; i >= 0; i-- {
		if _, ok := c.Layers[i].Values[name]; ok {
			return c.Layers[i].Name
		}
	}
	return ""
}

func (c *Context) Mask(text string) string {
	var secrets []string
	for s := range c.Secrets {
		if s != "" {
			secrets = append(secrets, s)
		}
	}
	sort.Slice(secrets, func(i, j int) bool {
		return len(secrets[i]) > len(secrets[j])
	})
	for _, s := range secrets {
		text = strings.ReplaceAll(text, s, "********")
	}
	return text
}

func generateUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant RFC4122
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func strftimeToGoLayout(format string) string {
	replacements := []struct {
		token  string
		layout string
	}{
		{"%Y", "2006"},
		{"%y", "06"},
		{"%m", "01"},
		{"%d", "02"},
		{"%H", "15"},
		{"%I", "03"},
		{"%M", "04"},
		{"%S", "05"},
		{"%p", "PM"},
		{"%b", "Jan"},
		{"%B", "January"},
		{"%a", "Mon"},
		{"%A", "Monday"},
		{"%z", "-0700"},
		{"%Z", "MST"},
	}
	res := format
	for _, r := range replacements {
		res = strings.ReplaceAll(res, r.token, r.layout)
	}
	return res
}

func DynamicValue(name string) (any, bool, error) {
	lower := strings.ToLower(name)
	switch lower {
	case "$uuid", "$guid", "$randomuuid":
		return generateUUID(), true, nil
	case "$timestamp":
		return int64(time.Now().Unix()), true, nil
	case "$timestampms":
		return int64(time.Now().UnixMilli()), true, nil
	case "$isotimestamp":
		return time.Now().UTC().Format(time.RFC3339), true, nil
	case "$randomint":
		n, _ := rand.Int(rand.Reader, big.NewInt(1001))
		return int(n.Int64()), true, nil
	}

	if strings.HasPrefix(lower, "$env:") {
		rest := name[5:]
		var key, def string
		hasDef := false
		if idx := strings.Index(rest, ":"); idx >= 0 {
			key = rest[:idx]
			def = rest[idx+1:]
			hasDef = true
		} else {
			key = rest
		}

		if val, ok := os.LookupEnv(key); ok {
			return val, true, nil
		}
		if hasDef {
			return def, true, nil
		}
		return nil, false, &MissingVariableError{Names: []string{fmt.Sprintf("$env:%s (environment variable %s is not set)", key, key)}}
	}

	if strings.HasPrefix(lower, "$now:") {
		layout := strftimeToGoLayout(name[5:])
		return time.Now().Format(layout), true, nil
	}
	if strings.HasPrefix(lower, "$utcnow:") {
		layout := strftimeToGoLayout(name[8:])
		return time.Now().UTC().Format(layout), true, nil
	}

	return nil, false, nil
}

func toString(val any) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case map[string]any, []any:
		b, err := json.Marshal(v)
		if err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func lookup(name string, ctx *Context) (any, bool, error) {
	if strings.HasPrefix(name, "$") {
		val, ok, err := DynamicValue(name)
		if err != nil {
			return nil, false, err
		}
		if ok {
			return val, true, nil
		}
	}
	val, ok := ctx.Lookup(name)
	if !ok {
		ctx.Missing[name] = true
	}
	return val, ok, nil
}

func Render(obj any, ctx *Context, strict bool) (any, error) {
	ctx.Missing = make(map[string]bool)
	res, err := renderInternal(obj, ctx, 0)
	if err != nil {
		return nil, err
	}
	if strict && len(ctx.Missing) > 0 {
		var names []string
		for n := range ctx.Missing {
			names = append(names, n)
		}
		sort.Strings(names)
		return nil, &MissingVariableError{Names: names}
	}
	return res, nil
}

func renderInternal(obj any, ctx *Context, depth int) (any, error) {
	if depth > MaxDepth {
		return obj, nil
	}

	switch v := obj.(type) {
	case string:
		matches := varRegex.FindAllStringSubmatchIndex(v, -1)
		if len(matches) == 0 {
			return v, nil
		}

		// Check if the entire string is exactly one placeholder
		if len(matches) == 1 && matches[0][0] == 0 && matches[0][1] == len(v) {
			key := strings.TrimSpace(v[matches[0][2]:matches[0][3]])
			val, ok, err := lookup(key, ctx)
			if err != nil {
				return nil, err
			}
			if !ok {
				return v, nil
			}
			return renderInternal(val, ctx, depth+1)
		}

		// Multiple placeholders or embedded in text
		var sb strings.Builder
		lastIndex := 0
		for _, m := range matches {
			sb.WriteString(v[lastIndex:m[0]])
			key := strings.TrimSpace(v[m[2]:m[3]])
			val, ok, err := lookup(key, ctx)
			if err != nil {
				return nil, err
			}
			if !ok {
				sb.WriteString(v[m[0]:m[1]])
			} else {
				rendered, err := renderInternal(val, ctx, depth+1)
				if err != nil {
					return nil, err
				}
				sb.WriteString(toString(rendered))
			}
			lastIndex = m[1]
		}
		sb.WriteString(v[lastIndex:])
		return sb.String(), nil

	case map[string]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			renderedKey, err := renderInternal(k, ctx, depth)
			if err != nil {
				return nil, err
			}
			keyStr := toString(renderedKey)
			renderedVal, err := renderInternal(val, ctx, depth)
			if err != nil {
				return nil, err
			}
			out[keyStr] = renderedVal
		}
		return out, nil

	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			renderedItem, err := renderInternal(item, ctx, depth)
			if err != nil {
				return nil, err
			}
			out[i] = renderedItem
		}
		return out, nil

	default:
		return obj, nil
	}
}

func FindPlaceholders(obj any) []string {
	namesMap := make(map[string]bool)
	findPlaceholdersInternal(obj, namesMap)
	var names []string
	for n := range namesMap {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func findPlaceholdersInternal(obj any, acc map[string]bool) {
	switch v := obj.(type) {
	case string:
		matches := varRegex.FindAllStringSubmatch(v, -1)
		for _, m := range matches {
			if len(m) > 1 {
				acc[strings.TrimSpace(m[1])] = true
			}
		}
	case map[string]any:
		for k, val := range v {
			findPlaceholdersInternal(k, acc)
			findPlaceholdersInternal(val, acc)
		}
	case []any:
		for _, item := range v {
			findPlaceholdersInternal(item, acc)
		}
	}
}
