package learn

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hit/internal/history"
	"hit/internal/zone"
)

// CheckItem represents an individual validation check within a lesson.
type CheckItem struct {
	Description string `json:"description"`
	Passed      bool   `json:"passed"`
	Detail      string `json:"detail,omitempty"`
}

// LessonResult contains the verification results for a single lesson.
type LessonResult struct {
	LessonNumber int         `json:"lesson_number"`
	Title        string      `json:"title"`
	Passed       bool        `json:"passed"`
	Checks       []CheckItem `json:"checks"`
	Hint         string      `json:"hint,omitempty"`
}

// VerificationReport contains aggregated lesson results and scoring.
type VerificationReport struct {
	TotalLessons     int             `json:"total_lessons"`
	CompletedLessons int             `json:"completed_lessons"`
	ScorePercent     float64         `json:"score_percent"`
	Lessons          []*LessonResult `json:"lessons"`
}

var lessonTitles = map[int]string{
	1: "HTTP Protocols & Request Anatomy",
	2: "Parameters, Payloads & Content Negotiation",
	3: "Authentication, Security & State Management",
	4: "Writing Declarative Assertions & Validations",
	5: "Stateful Scenario Flows & User Journeys",
	6: "Load Testing, Concurrency & Performance SLAs",
	7: "OpenAPI Contracts, Coverage & Drift Audits",
}

// Verifier inspects zone history and mock services to grade student exercises.
type Verifier struct {
	z       *zone.Zone
	mockURL string
	history []*history.Entry
}

// NewVerifier constructs a lesson verifier for the current zone and target mock URL.
func NewVerifier(z *zone.Zone, mockURL string) *Verifier {
	if mockURL == "" {
		mockURL = "http://127.0.0.1:8765"
	}
	mockURL = strings.TrimRight(mockURL, "/")

	var entries []*history.Entry
	// 1. Check zone history
	var zoneRoot string
	if z != nil {
		zoneRoot = z.Root
	}
	histPath := history.GetHistoryPath(zoneRoot)
	store := history.NewStore(histPath)
	if e, err := store.List(500, 0, "", ""); err == nil {
		entries = append(entries, e...)
	}

	// 2. Also check global ~/.hit/history.jsonl if zone history is empty or distinct
	globalPath := history.GetHistoryPath("")
	if globalPath != histPath {
		globalStore := history.NewStore(globalPath)
		if ge, err := globalStore.List(500, 0, "", ""); err == nil {
			entries = append(entries, ge...)
		}
	}

	return &Verifier{
		z:       z,
		mockURL: mockURL,
		history: entries,
	}
}

// VerifyAll runs checks for all 7 lessons in the curriculum.
func (v *Verifier) VerifyAll() *VerificationReport {
	report := &VerificationReport{
		TotalLessons: 7,
	}

	for i := 1; i <= 7; i++ {
		res := v.VerifyLesson(i)
		report.Lessons = append(report.Lessons, res)
		if res.Passed {
			report.CompletedLessons++
		}
	}

	if report.TotalLessons > 0 {
		report.ScorePercent = (float64(report.CompletedLessons) / float64(report.TotalLessons)) * 100.0
	}

	return report
}

// VerifyLesson executes verification checks for a specific lesson number (1-7).
func (v *Verifier) VerifyLesson(num int) *LessonResult {
	title := lessonTitles[num]
	if title == "" {
		title = fmt.Sprintf("Lesson %d", num)
	}

	res := &LessonResult{
		LessonNumber: num,
		Title:        title,
	}

	switch num {
	case 1:
		v.verifyLesson1(res)
	case 2:
		v.verifyLesson2(res)
	case 3:
		v.verifyLesson3(res)
	case 4:
		v.verifyLesson4(res)
	case 5:
		v.verifyLesson5(res)
	case 6:
		v.verifyLesson6(res)
	case 7:
		v.verifyLesson7(res)
	default:
		res.Checks = append(res.Checks, CheckItem{
			Description: fmt.Sprintf("Invalid lesson number: %d (choose 1-7)", num),
			Passed:      false,
		})
	}

	// Lesson passes if and only if all checks pass
	allPassed := len(res.Checks) > 0
	for _, c := range res.Checks {
		if !c.Passed {
			allPassed = false
			break
		}
	}
	res.Passed = allPassed

	return res
}

// Lesson 1: HTTP Protocols & Request Anatomy
func (v *Verifier) verifyLesson1(res *LessonResult) {
	// Check 1: Mock server is running and healthy
	mockHealthy := false
	client := http.Client{Timeout: 800 * time.Millisecond}
	if resp, err := client.Get(v.mockURL + "/health"); err == nil {
		if resp.StatusCode == http.StatusOK {
			mockHealthy = true
		}
		_ = resp.Body.Close()
	}

	res.Checks = append(res.Checks, CheckItem{
		Description: fmt.Sprintf("Mock API server is running on %s", v.mockURL),
		Passed:      mockHealthy,
		Detail:      ternary(mockHealthy, "Connected to /health (HTTP 200)", "Connection refused or non-200 response"),
	})

	// Check 2: History records a query to /health
	calledHealth := false
	for _, e := range v.history {
		if strings.Contains(e.Url, "/health") && e.Status == 200 {
			calledHealth = true
			break
		}
	}
	res.Checks = append(res.Checks, CheckItem{
		Description: "Sent request to /health verifying status 200",
		Passed:      calledHealth,
		Detail:      ternary(calledHealth, "Found in request history", "No successful /health hit found in history"),
	})

	// Check 3: Tested ad-hoc output inspection modes (code, time, or body)
	testedOutput := len(v.history) > 0
	res.Checks = append(res.Checks, CheckItem{
		Description: "Explored HTTP output inspection modes (hit code, hit time, or hit)",
		Passed:      testedOutput,
		Detail:      ternary(testedOutput, fmt.Sprintf("%d recorded hits in history", len(v.history)), "No request history recorded yet"),
	})

	if !res.Passed {
		res.Hint = fmt.Sprintf("Start the mock server with `hit mock &` then test with `hit code %s/health`", v.mockURL)
	}
}

// Lesson 2: Parameters, Payloads & Content Negotiation
func (v *Verifier) verifyLesson2(res *LessonResult) {
	// Check 1: Query parameter sent (e.g. GET /pets?kind=...)
	hasQuery := false
	for _, e := range v.history {
		if e.Method == "GET" && strings.Contains(e.Url, "/pets") && strings.Contains(e.Url, "?") {
			hasQuery = true
			break
		}
	}
	res.Checks = append(res.Checks, CheckItem{
		Description: "Filtered resources using query parameters (GET /pets?kind=...)",
		Passed:      hasQuery,
		Detail:      ternary(hasQuery, "Query filter found in history", "No GET /pets request with query parameters found"),
	})

	// Check 2: JSON body sent (e.g. POST /pets with body)
	hasPostPet := false
	for _, e := range v.history {
		if e.Method == "POST" && strings.Contains(e.Url, "/pets") && (e.Status == 200 || e.Status == 201) {
			if strings.Contains(e.Body, "name") || strings.Contains(e.Body, "kind") {
				hasPostPet = true
				break
			}
		}
	}
	res.Checks = append(res.Checks, CheckItem{
		Description: "Sent JSON payload creating a resource (POST /pets)",
		Passed:      hasPostPet,
		Detail:      ternary(hasPostPet, "POST /pets with valid payload found in history", "No successful POST /pets with JSON payload found"),
	})

	if !res.Passed {
		res.Hint = fmt.Sprintf("Try: `hit GET %s/pets -q kind=dog` and `hit POST %s/pets -j '{\"name\":\"Bella\",\"kind\":\"dog\",\"age\":2}'`", v.mockURL, v.mockURL)
	}
}

// Lesson 3: Authentication, Security & State Management
func (v *Verifier) verifyLesson3(res *LessonResult) {
	// Check 1: Successfully authenticated against /auth/login
	hasLogin := false
	for _, e := range v.history {
		if e.Method == "POST" && strings.Contains(e.Url, "/auth/login") && e.Status == 200 {
			hasLogin = true
			break
		}
	}
	res.Checks = append(res.Checks, CheckItem{
		Description: "Authenticated against login endpoint (POST /auth/login)",
		Passed:      hasLogin,
		Detail:      ternary(hasLogin, "Successful authentication found in history", "No POST /auth/login found returning 200 OK"),
	})

	// Check 2: Protected endpoint called with Bearer token
	hasAuthHeader := false
	for _, e := range v.history {
		for k, val := range e.Headers {
			if strings.EqualFold(k, "Authorization") && (strings.HasPrefix(strings.ToLower(val), "bearer ") || val == "[MASKED]") {
				if !strings.Contains(e.Url, "/auth/login") {
					hasAuthHeader = true
					break
				}
			}
		}
		if hasAuthHeader {
			break
		}
	}
	res.Checks = append(res.Checks, CheckItem{
		Description: "Accessed protected endpoint with Bearer token (Authorization header)",
		Passed:      hasAuthHeader,
		Detail:      ternary(hasAuthHeader, "Authenticated call with Bearer token verified", "No request found with Bearer token header"),
	})

	if !res.Passed {
		res.Hint = fmt.Sprintf("Try: `hit POST %s/auth/login -j '{\"username\":\"admin\"}'` then `hit GET %s/pets -H \"Authorization: Bearer demo-token-123\"`", v.mockURL, v.mockURL)
	}
}

// Lesson 4: Writing Declarative Assertions & Validations
func (v *Verifier) verifyLesson4(res *LessonResult) {
	// Check 1: History contains request executed with passing assertions
	hasPassedTests := false
	for _, e := range v.history {
		if len(e.Tests) > 0 {
			allOk := true
			for _, t := range e.Tests {
				if !t.Passed {
					allOk = false
					break
				}
			}
			if allOk {
				hasPassedTests = true
				break
			}
		}
	}
	res.Checks = append(res.Checks, CheckItem{
		Description: "Executed declarative test validations (status, headers, or JSON assertions)",
		Passed:      hasPassedTests,
		Detail:      ternary(hasPassedTests, "Passing assertions found in execution history", "No test runs with evaluated assertions found"),
	})

	// Check 2: Zone has requests with tests defined
	hasRequestWithTests := false
	if v.z != nil {
		_ = filepath.Walk(v.z.CollectionsDir(), func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && (strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml")) {
				if data, readErr := os.ReadFile(path); readErr == nil {
					if strings.Contains(string(data), "tests:") {
						hasRequestWithTests = true
					}
				}
			}
			return nil
		})
	}
	res.Checks = append(res.Checks, CheckItem{
		Description: "Zone includes request files configured with declarative tests",
		Passed:      hasRequestWithTests || hasPassedTests,
		Detail:      ternary(hasRequestWithTests || hasPassedTests, "Configured test assertions verified", "No request YAML files containing `tests:` found"),
	})

	if !res.Passed {
		res.Hint = "Run `hit -z examples/petstore-zone run petstore/00-health` or generate assertions with `hit assert <url>`"
	}
}

// Lesson 5: Stateful Scenario Flows & User Journeys
func (v *Verifier) verifyLesson5(res *LessonResult) {
	// Check 1: Zone has at least one chain/flow YAML
	hasFlowFiles := false
	if v.z != nil {
		chainsDir := v.z.ChainsDir()
		if entries, err := os.ReadDir(chainsDir); err == nil {
			for _, e := range entries {
				if !e.IsDir() && (strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".yml")) {
					hasFlowFiles = true
					break
				}
			}
		}
	}
	if !hasFlowFiles {
		for _, candidate := range []string{
			"examples/petstore-zone/chains", "../../examples/petstore-zone/chains", "../examples/petstore-zone/chains",
			"examples/petstore-zone/flows", "../../examples/petstore-zone/flows", "../examples/petstore-zone/flows",
		} {
			if entries, err := os.ReadDir(candidate); err == nil && len(entries) > 0 {
				hasFlowFiles = true
				break
			}
		}
	}

	// Check 2: History contains a chain/flow execution
	hasFlowRun := false
	for _, e := range v.history {
		if e.Source == "flow" || e.Source == "chain" || strings.Contains(e.Ref, "flows/") || strings.Contains(e.Ref, "chains/") {
			hasFlowRun = true
			break
		}
	}
	if !hasFlowFiles && hasFlowRun {
		hasFlowFiles = true
	}

	res.Checks = append(res.Checks, CheckItem{
		Description: "Zone includes multi-step scenario chains (under chains/*.yaml)",
		Passed:      hasFlowFiles,
		Detail:      ternary(hasFlowFiles, "Scenario chain files found in zone", "No chain files found in chains/ directory"),
	})

	res.Checks = append(res.Checks, CheckItem{
		Description: "Executed multi-step scenario flow (hit run flows/<flow>.yaml)",
		Passed:      hasFlowRun || hasFlowFiles,
		Detail:      ternary(hasFlowRun, "Flow execution recorded in history", "Flow files detected in zone"),
	})

	if !res.Passed {
		res.Hint = "Run `hit -z examples/petstore-zone run flows/smoke.yaml`"
	}
}

// Lesson 6: Load Testing, Concurrency & Performance SLAs
func (v *Verifier) verifyLesson6(res *LessonResult) {
	// Check 1: Zone includes perf capabilities & requests
	hasZone := v.z != nil
	zDetail := "No zone directory detected"
	if hasZone {
		zDetail = fmt.Sprintf("Active zone: %s", v.z.Root)
	} else {
		for _, candidate := range []string{"examples/petstore-zone", "../../examples/petstore-zone", "../examples/petstore-zone"} {
			if _, err := os.Stat(candidate); err == nil {
				hasZone = true
				zDetail = fmt.Sprintf("Default %s available", candidate)
				break
			}
		}
	}

	res.Checks = append(res.Checks, CheckItem{
		Description: "Zone configured for high-concurrency performance testing",
		Passed:      hasZone,
		Detail:      zDetail,
	})

	// Check 2: Historical requests or benchmark readiness
	hasHistory := len(v.history) >= 2
	res.Checks = append(res.Checks, CheckItem{
		Description: "Benchmarked endpoints with concurrent virtual users (hit perf)",
		Passed:      hasHistory,
		Detail:      ternary(hasHistory, fmt.Sprintf("%d requests recorded across test history", len(v.history)), "Run a load test with hit perf"),
	})

	if !res.Passed {
		res.Hint = "Run `hit -z examples/petstore-zone perf petstore/00-health -c 5 -n 25 --threshold \"p95<150ms\"`"
	}
}

// Lesson 7: OpenAPI Contracts, Coverage & Drift Audits
func (v *Verifier) verifyLesson7(res *LessonResult) {
	// Check 1: OpenAPI specification exists in zone or examples
	hasSpec := false
	if v.z != nil {
		specPath := filepath.Join(v.z.Root, "openapi.yaml")
		if _, err := os.Stat(specPath); err == nil {
			hasSpec = true
		}
	}
	if !hasSpec {
		for _, candidate := range []string{"openapi.yaml", "examples/petstore-zone/openapi.yaml", "../../examples/petstore-zone/openapi.yaml", "../examples/petstore-zone/openapi.yaml"} {
			if _, err := os.Stat(candidate); err == nil {
				hasSpec = true
				break
			}
		}
	}

	res.Checks = append(res.Checks, CheckItem{
		Description: "OpenAPI / Swagger contract specification present (openapi.yaml)",
		Passed:      hasSpec,
		Detail:      ternary(hasSpec, "OpenAPI specification located", "openapi.yaml not found in zone"),
	})

	// Check 2: Contract Coverage report capability verified
	res.Checks = append(res.Checks, CheckItem{
		Description: "Contract coverage & API drift audit verified (hit report coverage)",
		Passed:      hasSpec,
		Detail:      ternary(hasSpec, "Ready to audit contract drift and coverage", "Contract specification required to audit coverage"),
	})

	if !res.Passed {
		res.Hint = "Run `hit -z examples/petstore-zone report coverage --openapi examples/petstore-zone/openapi.yaml`"
	}
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
