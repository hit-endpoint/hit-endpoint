package flows

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hit-endpoint/hit-endpoint/internal/runner"
	"github.com/hit-endpoint/hit-endpoint/internal/templating"
	"github.com/hit-endpoint/hit-endpoint/internal/types"
	"github.com/hit-endpoint/hit-endpoint/internal/zone"
)

type FlowResult struct {
	Name     string          `json:"name"`
	Results  []*types.Result `json:"results"`
	Messages []string        `json:"messages"`
	Error    string          `json:"error,omitempty"`
}

func (fr *FlowResult) OK() bool {
	if fr.Error != "" {
		return false
	}
	for _, r := range fr.Results {
		if !r.OK() {
			return false
		}
	}
	return true
}

func LoadFlow(path string) (map[string]any, error) {
	data, err := zone.LoadYAML(path)
	if err != nil {
		return nil, err
	}
	steps, ok := data["steps"].([]any)
	if !ok || len(steps) == 0 {
		return nil, zone.NewZoneError("%s: a flow needs a 'steps' list", path)
	}
	if _, ok := data["name"]; !ok {
		stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		data["name"] = stem
	}
	data["_path"] = path
	return data, nil
}

func RunFlow(
	session *runner.Session,
	flowData map[string]any,
	onResult func(*types.Result),
	onMessage func(string),
	failFast bool,
	depth int,
) *FlowResult {
	if depth > 5 {
		return &FlowResult{Name: "flow", Error: "flows nested too deeply"}
	}

	flowName := "flow"
	if n, ok := flowData["name"].(string); ok && n != "" {
		flowName = n
	}
	fr := &FlowResult{Name: flowName}

	baseDir := ""
	if p, ok := flowData["_path"].(string); ok && p != "" {
		baseDir = filepath.Dir(p)
	}
	if baseDir == "" {
		baseDir, _ = os.Getwd()
	}

	savedFlowVars := make(map[string]any, len(session.FlowVars))
	for k, v := range session.FlowVars {
		savedFlowVars[k] = v
	}
	defer func() {
		session.FlowVars = savedFlowVars
	}()

	if vMap, ok := flowData["vars"].(map[string]any); ok {
		ctx := session.Context(nil, nil)
		rendered, err := templating.Render(vMap, ctx, session.Strict)
		if err != nil {
			fr.Error = err.Error()
			return fr
		}
		if rm, ok := rendered.(map[string]any); ok {
			for k, v := range rm {
				session.FlowVars[k] = v
			}
		}
	}

	say := func(msg string) {
		fr.Messages = append(fr.Messages, msg)
		if onMessage != nil {
			onMessage(msg)
		}
	}

	steps, _ := flowData["steps"].([]any)
	for index, rawStep := range steps {
		stepIndex := index + 1
		step, ok := rawStep.(map[string]any)
		if !ok {
			fr.Error = fmt.Sprintf("step %d: must be a mapping", stepIndex)
			return fr
		}

		if reqRef, ok := step["request"]; ok && reqRef != nil {
			overrides := make(map[string]any)
			for k, v := range step {
				if k != "request" && k != "repeat" && k != "continue_on_fail" && k != "name" {
					overrides[k] = v
				}
			}
			repeat := 1
			if repVal, ok := step["repeat"]; ok && repVal != nil {
				if rInt, ok := repVal.(int); ok && rInt > 0 {
					repeat = rInt
				} else if rFloat, ok := repVal.(float64); ok && rFloat > 0 {
					repeat = int(rFloat)
				}
			}

			for i := 0; i < repeat; i++ {
				r := session.Run(fmt.Sprintf("%v", reqRef), overrides)
				if customName, ok := step["name"].(string); ok && customName != "" {
					r.Name = customName
				}
				fr.Results = append(fr.Results, r)
				if onResult != nil {
					onResult(r)
				}
				continueOnFail := false
				if cof, ok := step["continue_on_fail"].(bool); ok {
					continueOnFail = cof
				}
				if !r.OK() && failFast && !continueOnFail {
					fr.Error = fmt.Sprintf("step %d (%s) failed", stepIndex, r.Ref)
					return fr
				}
			}
		} else if setMap, ok := step["set"].(map[string]any); ok {
			ctx := session.Context(nil, nil)
			rendered, err := templating.Render(setMap, ctx, session.Strict)
			if err != nil {
				fr.Error = fmt.Sprintf("step %d: %v", stepIndex, err)
				return fr
			}
			var parts []string
			if rm, ok := rendered.(map[string]any); ok {
				for k, v := range rm {
					noPersist := false
					session.SetVar(k, v, &noPersist)
					parts = append(parts, fmt.Sprintf("%s=%v", k, v))
				}
			}
			if session.Persist && session.State != nil {
				_ = session.State.Save()
			}
			say(fmt.Sprintf("set %s", strings.Join(parts, ", ")))
		} else if scriptVal, ok := step["python"]; ok {
			scriptPath := fmt.Sprintf("%v", scriptVal)
			resolvedPath := resolveScriptPath(scriptPath, baseDir, session.Zone)
			funcName := "run"
			if fn, ok := step["function"].(string); ok && fn != "" {
				funcName = fn
			}
			outcome, err := runPythonStep(resolvedPath, funcName, session)
			if err != nil {
				fr.Error = fmt.Sprintf("step %d: %v", stepIndex, err)
				return fr
			}
			if outcome != "" {
				say(fmt.Sprintf("%s: %s", filepath.Base(resolvedPath), outcome))
			} else {
				say(fmt.Sprintf("ran %s", filepath.Base(resolvedPath)))
			}
		} else if sleepVal, ok := step["sleep"]; ok {
			ctx := session.Context(nil, nil)
			rendered, err := templating.Render(sleepVal, ctx, session.Strict)
			if err != nil {
				fr.Error = fmt.Sprintf("step %d: %v", stepIndex, err)
				return fr
			}
			sec := 0.0
			switch sv := rendered.(type) {
			case float64:
				sec = sv
			case int:
				sec = float64(sv)
			case string:
				if f, err := strconv.ParseFloat(sv, 64); err == nil {
					sec = f
				}
			}
			time.Sleep(time.Duration(sec * float64(time.Second)))
			say(fmt.Sprintf("slept %.2fs", sec))
		} else if flowRef, ok := step["flow"]; ok {
			if session.Zone == nil {
				fr.Error = fmt.Sprintf("step %d: no zone for nested flow", stepIndex)
				return fr
			}
			subPath, err := session.Zone.ResolveChain(fmt.Sprintf("%v", flowRef))
			if err != nil {
				fr.Error = fmt.Sprintf("step %d: flow '%v' not found", stepIndex, flowRef)
				return fr
			}
			subFlowData, err := LoadFlow(subPath)
			if err != nil {
				fr.Error = fmt.Sprintf("step %d: %v", stepIndex, err)
				return fr
			}
			sub := RunFlow(session, subFlowData, onResult, onMessage, failFast, depth+1)
			fr.Results = append(fr.Results, sub.Results...)
			if sub.Error != "" {
				fr.Error = fmt.Sprintf("step %d (flow %s): %s", stepIndex, sub.Name, sub.Error)
				if failFast {
					return fr
				}
			}
		} else {
			fr.Error = fmt.Sprintf("step %d: unknown step type", stepIndex)
			return fr
		}
	}
	return fr
}

func resolveScriptPath(p string, baseDir string, z *zone.Zone) string {
	if filepath.IsAbs(p) {
		return p
	}
	candidates := []string{
		filepath.Join(baseDir, p),
	}
	if z != nil {
		candidates = append(candidates, filepath.Join(z.Root, p))
	}
	cwd, _ := os.Getwd()
	candidates = append(candidates, filepath.Join(cwd, p))

	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return filepath.Join(baseDir, p)
}

func runPythonStep(scriptPath, funcName string, session *runner.Session) (string, error) {
	pythonCode := fmt.Sprintf(`
import sys, json, importlib.util
path = sys.argv[1]
fn_name = %q
spec = importlib.util.spec_from_file_location("flow_step", path)
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)
fn = getattr(mod, fn_name, None)
if fn is None:
    sys.exit(1)
# Proxy session if needed
class DummySession:
    def __init__(self, vars_map):
        self.v = vars_map
    def get(self, k, d=None): return self.v.get(k, d)
    def set(self, k, v): self.v[k] = v
ret = fn(DummySession(json.load(sys.stdin)))
if ret is not None:
    print(str(ret), end="")
`, funcName)

	inputBytes, _ := json.Marshal(session.Variables())
	cmd := exec.Command("python3", "-c", pythonCode, scriptPath)
	cmd.Stdin = bytes.NewReader(inputBytes)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("hook %s failed: %s %v", filepath.Base(scriptPath), errBuf.String(), err)
	}
	return strings.TrimSpace(outBuf.String()), nil
}
