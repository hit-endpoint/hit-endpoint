package report

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"hit/internal/spec"
	"hit/internal/zone"
)

type OperationCoverage struct {
	Path               string   `json:"path"`
	Method             string   `json:"method"`
	Summary            string   `json:"summary,omitempty"`
	Tested             bool     `json:"tested"`
	RequestRef         string   `json:"request_ref,omitempty"`
	CoveredStatuses    []int    `json:"covered_statuses,omitempty"`
	DeclaredStatuses   []string `json:"declared_statuses,omitempty"`
}

type CoverageReport struct {
	Title             string              `json:"title"`
	Version           string              `json:"version,omitempty"`
	TotalOperations   int                 `json:"total_operations"`
	CoveredOperations int                 `json:"covered_operations"`
	CoveragePercent   float64             `json:"coverage_percent"`
	Operations        []OperationCoverage `json:"operations"`
	Untested          []OperationCoverage `json:"untested"`
	Undocumented      []string            `json:"undocumented,omitempty"`
}

func normalizePath(p string) string {
	// Trim query string
	if idx := strings.Index(p, "?"); idx != -1 {
		p = p[:idx]
	}
	// Parse as URL if possible
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		if u, err := url.Parse(p); err == nil {
			p = u.Path
		}
	}
	// Remove variable prefixes like {{base_url}}
	reBase := regexp.MustCompile(`^\{\{[^}]+\}\}`)
	p = reBase.ReplaceAllString(p, "")

	// Normalize /path/{{id}} or /path/:id to /path/{id}
	reVar := regexp.MustCompile(`\{\{([a-zA-Z0-9_-]+)\}\}`)
	p = reVar.ReplaceAllString(p, "{$1}")
	reColon := regexp.MustCompile(`:([a-zA-Z0-9_-]+)`)
	p = reColon.ReplaceAllString(p, "{$1}")

	// Ensure leading slash and trim trailing slash
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if len(p) > 1 && strings.HasSuffix(p, "/") {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

func pathsMatch(specPath, reqPath string) bool {
	normSpec := normalizePath(specPath)
	normReq := normalizePath(reqPath)

	if normSpec == normReq {
		return true
	}

	// Convert {param} in spec to regex [^/]+
	reParam := regexp.MustCompile(`\{[a-zA-Z0-9_-]+\}`)
	pattern := "^" + reParam.ReplaceAllString(normSpec, `[^/]+`) + "$"
	matched, _ := regexp.MatchString(pattern, normReq)
	return matched
}

func LoadSpecData(specPathOrURL string) ([]byte, error) {
	if strings.HasPrefix(specPathOrURL, "http://") || strings.HasPrefix(specPathOrURL, "https://") {
		resp, err := http.Get(specPathOrURL)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return io.ReadAll(resp.Body)
	}
	return os.ReadFile(specPathOrURL)
}

func AuditCoverage(specBytes []byte, z *zone.Zone) (*CoverageReport, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(specBytes, &raw); err != nil {
		if err := json.Unmarshal(specBytes, &raw); err != nil {
			return nil, fmt.Errorf("spec is neither valid YAML nor JSON: %v", err)
		}
	}

	title := "API Specification"
	version := ""
	if info, ok := raw["info"].(map[string]any); ok {
		if t, ok := info["title"].(string); ok && t != "" {
			title = t
		}
		if v, ok := info["version"].(string); ok && v != "" {
			version = v
		}
	}

	rawPaths, _ := raw["paths"].(map[string]any)
	if rawPaths == nil {
		return nil, fmt.Errorf("no 'paths' found in OpenAPI specification")
	}

	var ops []OperationCoverage
	httpMethods := map[string]bool{
		"get": true, "post": true, "put": true, "delete": true,
		"patch": true, "options": true, "head": true,
	}

	for pathStr, item := range rawPaths {
		pathItem, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for m, opAny := range pathItem {
			method := strings.ToLower(m)
			if !httpMethods[method] {
				continue
			}
			opMap, _ := opAny.(map[string]any)
			summary := ""
			var declaredStatuses []string
			if opMap != nil {
				if s, ok := opMap["summary"].(string); ok {
					summary = s
				}
				if responses, ok := opMap["responses"].(map[string]any); ok {
					for statusStr := range responses {
						declaredStatuses = append(declaredStatuses, statusStr)
					}
					sort.Strings(declaredStatuses)
				}
			}

			ops = append(ops, OperationCoverage{
				Path:             pathStr,
				Method:           strings.ToUpper(method),
				Summary:          summary,
				DeclaredStatuses: declaredStatuses,
			})
		}
	}

	// Sort operations by path then method
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].Path == ops[j].Path {
			return ops[i].Method < ops[j].Method
		}
		return ops[i].Path < ops[j].Path
	})

	// Scan zone requests
	matchedReqs := make(map[string]bool)
	type zoneReqInfo struct {
		ref      string
		method   string
		url      string
		statuses []int
	}
	var zoneReqs []zoneReqInfo

	if z != nil {
		for _, path := range z.ListRequests(z.CollectionsDir()) {
			sp, err := spec.LoadSpec(path, z.DefaultsChain(path))
			if err != nil {
				continue
			}
			ref := z.RequestRef(path)
			method := sp.Method
			if method == "" {
				method = "GET"
			}
			method = strings.ToUpper(method)

			var statuses []int
			for _, t := range sp.Tests {
				if st, ok := t["status"]; ok {
					if intVal, ok := st.(int); ok {
						statuses = append(statuses, intVal)
					}
				}
			}

			zoneReqs = append(zoneReqs, zoneReqInfo{
				ref:      ref,
				method:   method,
				url:      sp.Url,
				statuses: statuses,
			})
		}
	}

	// Match zone requests to operations
	coveredCount := 0
	var untestableOps []OperationCoverage
	for i := range ops {
		op := &ops[i]
		seenStatuses := make(map[int]bool)
		for _, wr := range zoneReqs {
			if strings.EqualFold(op.Method, wr.method) && pathsMatch(op.Path, wr.url) {
				if !op.Tested {
					op.Tested = true
					op.RequestRef = wr.ref
					coveredCount++
				}
				for _, st := range wr.statuses {
					if !seenStatuses[st] {
						seenStatuses[st] = true
						op.CoveredStatuses = append(op.CoveredStatuses, st)
					}
				}
				matchedReqs[wr.ref] = true
			}
		}
		sort.Ints(op.CoveredStatuses)
		if !op.Tested {
			untestableOps = append(untestableOps, *op)
		}
	}

	var undocumented []string
	for _, wr := range zoneReqs {
		if !matchedReqs[wr.ref] {
			undocumented = append(undocumented, fmt.Sprintf("%s %s (%s)", wr.method, wr.url, wr.ref))
		}
	}

	coveragePct := 0.0
	if len(ops) > 0 {
		coveragePct = float64(coveredCount) / float64(len(ops)) * 100.0
	}

	return &CoverageReport{
		Title:             title,
		Version:           version,
		TotalOperations:   len(ops),
		CoveredOperations: coveredCount,
		CoveragePercent:   coveragePct,
		Operations:        ops,
		Untested:          untestableOps,
		Undocumented:      undocumented,
	}, nil
}

func FormatCoverageTable(rep *CoverageReport, color bool) string {
	var b strings.Builder

	green := func(s string) string {
		if !color {
			return s
		}
		return "\033[32m" + s + "\033[0m"
	}
	red := func(s string) string {
		if !color {
			return s
		}
		return "\033[31m" + s + "\033[0m"
	}
	dim := func(s string) string {
		if !color {
			return s
		}
		return "\033[2m" + s + "\033[0m"
	}
	bold := func(s string) string {
		if !color {
			return s
		}
		return "\033[1m" + s + "\033[0m"
	}

	pctColor := green
	if rep.CoveragePercent < 80.0 {
		pctColor = red
	}

	b.WriteString(bold(fmt.Sprintf("OpenAPI Contract Coverage Audit: %s %s\n", rep.Title, rep.Version)))
	b.WriteString(fmt.Sprintf("Coverage: %s (%d/%d endpoints covered)\n\n",
		pctColor(fmt.Sprintf("%.1f%%", rep.CoveragePercent)),
		rep.CoveredOperations, rep.TotalOperations,
	))

	b.WriteString(fmt.Sprintf("%-6s %-7s %-32s %-24s %s\n", "STATUS", "METHOD", "PATH", "REQUEST REF", "TESTED STATUSES"))
	b.WriteString(strings.Repeat("─", 88) + "\n")

	for _, op := range rep.Operations {
		statusBadge := green("COVER")
		refStr := op.RequestRef
		if !op.Tested {
			statusBadge = red("MISS ")
			refStr = dim("[no test]")
		}

		stList := ""
		if len(op.CoveredStatuses) > 0 {
			var strList []string
			for _, st := range op.CoveredStatuses {
				strList = append(strList, fmt.Sprint(st))
			}
			stList = strings.Join(strList, ", ")
		} else if op.Tested {
			stList = dim("<400")
		} else {
			stList = dim("-")
		}

		pathStr := op.Path
		if len(pathStr) > 32 {
			pathStr = pathStr[:29] + "..."
		}

		b.WriteString(fmt.Sprintf("%s  %-7s %-32s %-24s %s\n",
			statusBadge, op.Method, pathStr, refStr, stList))
	}

	if len(rep.Untested) > 0 {
		b.WriteString("\n" + bold(red(fmt.Sprintf("Untested Operations (%d missing):\n", len(rep.Untested)))))
		for _, u := range rep.Untested {
			summary := ""
			if u.Summary != "" {
				summary = " - " + u.Summary
			}
			b.WriteString(fmt.Sprintf("  • %-7s %s%s\n", u.Method, u.Path, dim(summary)))
		}
	}

	if len(rep.Undocumented) > 0 {
		b.WriteString("\n" + bold(dim(fmt.Sprintf("Undocumented Zone Requests (%d):\n", len(rep.Undocumented)))))
		for _, undoc := range rep.Undocumented {
			b.WriteString(fmt.Sprintf("  • %s\n", undoc))
		}
	}

	return b.String()
}
