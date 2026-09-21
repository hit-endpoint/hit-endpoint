package mock

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	openAPIParamRegex = regexp.MustCompile(`\{([a-zA-Z0-9_-]+)\}`)
	validHTTPMethods  = map[string]bool{
		"GET": true, "POST": true, "PUT": true, "PATCH": true,
		"DELETE": true, "OPTIONS": true, "HEAD": true,
	}
)

// OpenAPISpec holds parsed OpenAPI specification metadata and document tree.
type OpenAPISpec struct {
	Title       string
	Version     string
	Description string
	IsOpenAPI3  bool
	IsSwagger2  bool
	RawDoc      map[string]any
	Source      string
}

// OpenAPIRoute defines a single mocked endpoint operation.
type OpenAPIRoute struct {
	PathPattern string
	Regexp      *regexp.Regexp
	ParamNames  []string
	Method      string
	OperationID string
	Summary     string
	Description string
	Responses   map[string]OpenAPIResponse
	RequestBody map[string]any
	Parameters  []map[string]any
}

// OpenAPIResponse defines the response specification for an operation.
type OpenAPIResponse struct {
	StatusCode  int
	Description string
	Headers     map[string]any
	Content     map[string]OpenAPIMediaType
	Schema      any // Swagger 2.0 schema or direct schema
}

// OpenAPIMediaType defines content-type schema and examples.
type OpenAPIMediaType struct {
	Schema   any
	Example  any
	Examples map[string]any
}

// OpenAPIMockEngine manages dynamic OpenAPI routing, schema generation, and in-memory state.
type OpenAPIMockEngine struct {
	mu           sync.RWMutex
	spec         *OpenAPISpec
	routes       []*OpenAPIRoute
	stateful     bool
	stateStore   map[string][]map[string]any // collection path prefix -> items
	nextID       map[string]int              // collection path prefix -> auto-increment ID
	initialSeed  map[string][]map[string]any
	chaos        *ChaosEngine
	rawSpecBytes []byte
}

// LoadOpenAPISpec parses an OpenAPI 3.x or Swagger 2.0 specification from a file path or URL.
func LoadOpenAPISpec(src string) (*OpenAPISpec, []byte, error) {
	var data []byte
	var err error

	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		resp, httpErr := http.Get(src)
		if httpErr != nil {
			return nil, nil, fmt.Errorf("failed to fetch OpenAPI spec from %s: %w", src, httpErr)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, nil, fmt.Errorf("failed to fetch OpenAPI spec: HTTP %d %s", resp.StatusCode, resp.Status)
		}
		data, err = io.ReadAll(resp.Body)
	} else {
		data, err = os.ReadFile(src)
	}

	if err != nil {
		return nil, nil, fmt.Errorf("failed to read OpenAPI spec %s: %w", src, err)
	}

	var raw map[string]any
	if unmarshalErr := yaml.Unmarshal(data, &raw); unmarshalErr != nil {
		return nil, nil, fmt.Errorf("failed to parse OpenAPI spec as YAML or JSON: %w", unmarshalErr)
	}

	spec := &OpenAPISpec{
		RawDoc: raw,
		Source: src,
	}

	if v, ok := raw["openapi"].(string); ok && strings.HasPrefix(v, "3.") {
		spec.IsOpenAPI3 = true
	}
	if v, ok := raw["swagger"].(string); ok && strings.HasPrefix(v, "2.") {
		spec.IsSwagger2 = true
	}

	if !spec.IsOpenAPI3 && !spec.IsSwagger2 {
		return nil, nil, fmt.Errorf("%s: document is not a valid OpenAPI 3.x or Swagger 2.0 specification", src)
	}

	if info, ok := raw["info"].(map[string]any); ok {
		if t, ok := info["title"].(string); ok {
			spec.Title = t
		}
		if v, ok := info["version"].(string); ok {
			spec.Version = v
		}
		if d, ok := info["description"].(string); ok {
			spec.Description = d
		}
	}
	if spec.Title == "" {
		spec.Title = filepath.Base(src)
	}

	return spec, data, nil
}

// NewOpenAPIMockEngine constructs an OpenAPIMockEngine from a parsed specification.
func NewOpenAPIMockEngine(spec *OpenAPISpec, rawBytes []byte, stateful bool, chaos *ChaosEngine) (*OpenAPIMockEngine, error) {
	engine := &OpenAPIMockEngine{
		spec:         spec,
		stateful:     stateful,
		stateStore:   make(map[string][]map[string]any),
		nextID:       make(map[string]int),
		initialSeed:  make(map[string][]map[string]any),
		chaos:        chaos,
		rawSpecBytes: rawBytes,
	}

	if err := engine.compileRoutes(); err != nil {
		return nil, err
	}

	if stateful {
		engine.seedState()
	}

	return engine, nil
}

// Title returns the specification title.
func (e *OpenAPIMockEngine) Title() string {
	if e.spec != nil && e.spec.Title != "" {
		return e.spec.Title
	}
	return "OpenAPI Specification"
}

// Version returns the specification version.
func (e *OpenAPIMockEngine) Version() string {
	if e.spec != nil {
		return e.spec.Version
	}
	return ""
}

// RouteCount returns the number of registered operation endpoints.
func (e *OpenAPIMockEngine) RouteCount() int {
	return len(e.routes)
}

func (e *OpenAPIMockEngine) compileRoutes() error {
	paths, ok := e.spec.RawDoc["paths"].(map[string]any)
	if !ok || paths == nil {
		return nil
	}

	var pathKeys []string
	for p := range paths {
		pathKeys = append(pathKeys, p)
	}
	sort.Strings(pathKeys)

	for _, pathStr := range pathKeys {
		pathItem, ok := paths[pathStr].(map[string]any)
		if !ok {
			continue
		}

		pathParamsList, _ := pathItem["parameters"].([]any)

		// Regex compiler for path
		var paramNames []string
		regexPattern := "^" + openAPIParamRegex.ReplaceAllStringFunc(pathStr, func(m string) string {
			pName := strings.Trim(m, "{}")
			paramNames = append(paramNames, pName)
			return `([^/]+)`
		}) + `/?$`
		compiledRegex, err := regexp.Compile(regexPattern)
		if err != nil {
			return fmt.Errorf("failed to compile route pattern for %s: %w", pathStr, err)
		}

		for methodKey, opAny := range pathItem {
			methodUpper := strings.ToUpper(methodKey)
			if !validHTTPMethods[methodUpper] {
				continue
			}

			opMap, ok := opAny.(map[string]any)
			if !ok {
				continue
			}

			route := &OpenAPIRoute{
				PathPattern: pathStr,
				Regexp:      compiledRegex,
				ParamNames:  paramNames,
				Method:      methodUpper,
				Responses:   make(map[string]OpenAPIResponse),
			}

			if opID, ok := opMap["operationId"].(string); ok {
				route.OperationID = opID
			}
			if sum, ok := opMap["summary"].(string); ok {
				route.Summary = sum
			}
			if desc, ok := opMap["description"].(string); ok {
				route.Description = desc
			}

			// Compile parameters
			var allParams []map[string]any
			for _, p := range pathParamsList {
				if pm, ok := p.(map[string]any); ok {
					allParams = append(allParams, pm)
				}
			}
			if opParams, ok := opMap["parameters"].([]any); ok {
				for _, p := range opParams {
					if pm, ok := p.(map[string]any); ok {
						allParams = append(allParams, pm)
					}
				}
			}
			route.Parameters = allParams

			// Compile requestBody
			if reqBody, ok := opMap["requestBody"].(map[string]any); ok {
				route.RequestBody = reqBody
			}

			// Compile responses
			if responses, ok := opMap["responses"].(map[string]any); ok {
				for statusKey, respAny := range responses {
					respMap, ok := respAny.(map[string]any)
					if !ok {
						continue
					}

					// Resolve $ref if present
					if ref, ok := respMap["$ref"].(string); ok && ref != "" {
						if resolved := resolveRef(e.spec.RawDoc, ref); resolved != nil {
							respMap = resolved
						}
					}

					resp := OpenAPIResponse{
						Headers: make(map[string]any),
						Content: make(map[string]OpenAPIMediaType),
					}
					if sCode, err := strconv.Atoi(statusKey); err == nil {
						resp.StatusCode = sCode
					}
					if desc, ok := respMap["description"].(string); ok {
						resp.Description = desc
					}
					if hdrs, ok := respMap["headers"].(map[string]any); ok {
						resp.Headers = hdrs
					}

					// OpenAPI 3.x content
					if content, ok := respMap["content"].(map[string]any); ok {
						for mediaKey, mediaAny := range content {
							mediaMap, ok := mediaAny.(map[string]any)
							if !ok {
								continue
							}
							mt := OpenAPIMediaType{
								Schema:  mediaMap["schema"],
								Example: mediaMap["example"],
							}
							if exs, ok := mediaMap["examples"].(map[string]any); ok {
								mt.Examples = exs
							}
							resp.Content[mediaKey] = mt
						}
					}

					// Swagger 2.0 schema
					if schema, ok := respMap["schema"]; ok {
						resp.Schema = schema
						// Also populate default application/json
						resp.Content["application/json"] = OpenAPIMediaType{
							Schema: schema,
						}
					}

					route.Responses[statusKey] = resp
				}
			}

			e.routes = append(e.routes, route)
		}
	}

	return nil
}

func (e *OpenAPIMockEngine) seedState() {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Look for collection endpoints: GET /prefix and POST /prefix
	for _, r := range e.routes {
		if r.Method == "GET" && !strings.Contains(r.PathPattern, "{") {
			prefix := r.PathPattern
			// Generate 2 sample seed items
			resp := e.pickSuccessResponse(r)
			schema := e.extractResponseSchema(resp)
			if schema != nil {
				// If schema is array, extract items schema
				itemSchema := schema
				if sm, ok := schema.(map[string]any); ok {
					if sm["type"] == "array" && sm["items"] != nil {
						itemSchema = sm["items"]
					} else if props, ok := sm["properties"].(map[string]any); ok {
						// Look for wrapper items
						for _, candidate := range []string{"items", "data", "results"} {
							if wrapProp, ok := props[candidate].(map[string]any); ok && wrapProp["type"] == "array" {
								itemSchema = wrapProp["items"]
								break
							}
						}
					}
				}

				item1 := e.generateSampleObject(itemSchema, 1)
				item2 := e.generateSampleObject(itemSchema, 2)
				var seeds []map[string]any
				if item1 != nil {
					seeds = append(seeds, item1)
				}
				if item2 != nil {
					seeds = append(seeds, item2)
				}
				if len(seeds) > 0 {
					e.stateStore[prefix] = seeds
					e.initialSeed[prefix] = append([]map[string]any(nil), seeds...)
					e.nextID[prefix] = len(seeds) + 1
				}
			}
		}
	}
}

func (e *OpenAPIMockEngine) generateSampleObject(schema any, id int) map[string]any {
	sample := generateSample(e.spec.RawDoc, schema, 0)
	if m, ok := sample.(map[string]any); ok {
		if _, hasID := m["id"]; hasID {
			m["id"] = id
		}
		return m
	}
	return map[string]any{"id": id, "name": fmt.Sprintf("Item %d", id)}
}

// ServeHTTP satisfies http.Handler for dynamic OpenAPI mock requests.
func (e *OpenAPIMockEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 1. CORS Preflight & Global CORS Headers
	e.setCORSHeaders(w)
	if r.Method == "OPTIONS" && r.Header.Get("Access-Control-Request-Method") != "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	reqPath := r.URL.Path

	// 2. Special Inspector Routes
	switch reqPath {
	case "/__mock/routes", "/__hit/routes":
		e.handleInspectRoutes(w)
		return
	case "/__mock/openapi", "/__hit/openapi":
		e.handleInspectOpenAPI(w)
		return
	case "/__mock/reset", "/__hit/reset":
		e.handleResetState(w, r)
		return
	}

	// 3. Match Route in OpenAPI Spec
	route, pathParams, allowedMethods := e.matchRoute(r.Method, reqPath)
	if route == nil {
		if len(allowedMethods) > 0 {
			// 405 Method Not Allowed
			w.Header().Set("Allow", strings.Join(allowedMethods, ", "))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":           "method_not_allowed",
				"message":         fmt.Sprintf("Method %s is not allowed for path %s", r.Method, reqPath),
				"allowed_methods": allowedMethods,
			})
			return
		}

		// 404 Route Not Found
		e.handleRouteNotFound(w, r)
		return
	}

	// 4. Handle Stateful CRUD if enabled
	if e.stateful && e.handleStatefulCRUD(w, r, route, pathParams) {
		return
	}

	// 5. Dynamic Response Generation
	e.handleDynamicResponse(w, r, route, pathParams)
}

func (e *OpenAPIMockEngine) setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Expose-Headers", "*")
	w.Header().Set("Access-Control-Max-Age", "86400")
}

func (e *OpenAPIMockEngine) matchRoute(method, path string) (*OpenAPIRoute, map[string]string, []string) {
	var matchingRoutes []*OpenAPIRoute
	var allowedMethods []string

	for _, route := range e.routes {
		matches := route.Regexp.FindStringSubmatch(path)
		if len(matches) > 0 {
			if !containsString(allowedMethods, route.Method) {
				allowedMethods = append(allowedMethods, route.Method)
			}
			if route.Method == method {
				params := make(map[string]string)
				for i, name := range route.ParamNames {
					if i+1 < len(matches) {
						params[name] = matches[i+1]
					}
				}
				matchingRoutes = append(matchingRoutes, route)
				return route, params, allowedMethods
			}
		}
	}

	return nil, nil, allowedMethods
}

func (e *OpenAPIMockEngine) handleRouteNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)

	var available []string
	seen := make(map[string]bool)
	for _, rt := range e.routes {
		key := fmt.Sprintf("%s %s", rt.Method, rt.PathPattern)
		if !seen[key] {
			seen[key] = true
			available = append(available, key)
		}
	}
	sort.Strings(available)

	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":            "route_not_found",
		"message":          fmt.Sprintf("Endpoint %s %s is not defined in the OpenAPI specification", r.Method, r.URL.Path),
		"available_routes": available,
	})
}

func (e *OpenAPIMockEngine) handleInspectRoutes(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	type RouteDTO struct {
		Method      string   `json:"method"`
		Path        string   `json:"path"`
		Summary     string   `json:"summary,omitempty"`
		OperationID string   `json:"operation_id,omitempty"`
		Responses   []string `json:"responses"`
	}

	var dtos []RouteDTO
	for _, rt := range e.routes {
		var respCodes []string
		for k := range rt.Responses {
			respCodes = append(respCodes, k)
		}
		sort.Strings(respCodes)

		dtos = append(dtos, RouteDTO{
			Method:      rt.Method,
			Path:        rt.PathPattern,
			Summary:     rt.Summary,
			OperationID: rt.OperationID,
			Responses:   respCodes,
		})
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"title":        e.Title(),
		"version":      e.Version(),
		"total_routes": len(e.routes),
		"routes":       dtos,
	})
}

func (e *OpenAPIMockEngine) handleInspectOpenAPI(w http.ResponseWriter) {
	if len(e.rawSpecBytes) > 0 {
		if strings.HasPrefix(strings.TrimSpace(string(e.rawSpecBytes)), "{") {
			w.Header().Set("Content-Type", "application/json")
		} else {
			w.Header().Set("Content-Type", "application/x-yaml")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(e.rawSpecBytes)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(e.spec.RawDoc)
}

func (e *OpenAPIMockEngine) handleResetState(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	e.mu.Lock()
	e.stateStore = make(map[string][]map[string]any)
	for k, v := range e.initialSeed {
		e.stateStore[k] = append([]map[string]any(nil), v...)
		e.nextID[k] = len(v) + 1
	}
	e.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"message": "in-memory mock store reset to initial seed state",
	})
}

// Stateful CRUD handling for standard REST patterns (/collection, /collection/{id})
func (e *OpenAPIMockEngine) handleStatefulCRUD(w http.ResponseWriter, r *http.Request, route *OpenAPIRoute, pathParams map[string]string) bool {
	// Identify parent collection pattern
	collectionPrefix := route.PathPattern
	isItemEndpoint := len(route.ParamNames) > 0 && strings.HasSuffix(route.PathPattern, "/{"+route.ParamNames[len(route.ParamNames)-1]+"}")

	if isItemEndpoint {
		param := route.ParamNames[len(route.ParamNames)-1]
		collectionPrefix = strings.TrimSuffix(route.PathPattern, "/{"+param+"}")
	}

	// If client explicitly requested a status or example override, bypass stateful CRUD
	// so handleDynamicResponse can serve the requested spec status/example.
	if r.Header.Get("X-Hit-Mock-Status") != "" ||
		strings.Contains(r.Header.Get("Prefer"), "status=") ||
		strings.Contains(r.Header.Get("Prefer"), "code=") ||
		r.URL.Query().Get("__status") != "" ||
		r.URL.Query().Get("mock_status") != "" ||
		r.Header.Get("X-Hit-Mock-Example") != "" ||
		strings.Contains(r.Header.Get("Prefer"), "example=") ||
		r.URL.Query().Get("__example") != "" ||
		r.URL.Query().Get("mock_example") != "" {
		return false
	}

	e.mu.Lock()
	_, hasCollection := e.stateStore[collectionPrefix]
	e.mu.Unlock()

	if !hasCollection && route.Method != "POST" {
		return false
	}

	// 1. POST /collection -> Create item
	if route.Method == "POST" && !isItemEndpoint {
		var reqBody map[string]any
		bodyBytes, _ := io.ReadAll(r.Body)
		if len(bodyBytes) > 0 {
			_ = json.Unmarshal(bodyBytes, &reqBody)
		}
		if reqBody == nil {
			reqBody = make(map[string]any)
		}

		e.mu.Lock()
		defer e.mu.Unlock()

		next := e.nextID[collectionPrefix]
		if next == 0 {
			next = 1
		}
		e.nextID[collectionPrefix] = next + 1

		// Assign ID if not set
		if _, hasID := reqBody["id"]; !hasID {
			reqBody["id"] = next
		}

		e.stateStore[collectionPrefix] = append(e.stateStore[collectionPrefix], reqBody)

		resp := e.pickSuccessResponse(route)
		status := resp.StatusCode
		if status == 0 {
			status = http.StatusCreated
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(reqBody)
		return true
	}

	// 2. GET /collection -> List items
	if route.Method == "GET" && !isItemEndpoint {
		e.mu.RLock()
		currentItems := append([]map[string]any(nil), e.stateStore[collectionPrefix]...)
		e.mu.RUnlock()

		resp := e.pickSuccessResponse(route)
		schema := e.extractResponseSchema(resp)
		status := resp.StatusCode
		if status == 0 {
			status = http.StatusOK
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Total-Count", strconv.Itoa(len(currentItems)))
		w.WriteHeader(status)

		// Check if response schema expects wrapper object or direct array
		if sm, ok := schema.(map[string]any); ok && sm["type"] == "object" {
			wrapper := make(map[string]any)
			if props, hasProps := sm["properties"].(map[string]any); hasProps {
				for propName, propSchemaAny := range props {
					if propSchema, ok := propSchemaAny.(map[string]any); ok && propSchema["type"] == "array" {
						wrapper[propName] = currentItems
					} else if propName == "total" || propName == "count" {
						wrapper[propName] = len(currentItems)
					}
				}
				if len(wrapper) > 0 {
					_ = json.NewEncoder(w).Encode(wrapper)
					return true
				}
			}
		}

		_ = json.NewEncoder(w).Encode(currentItems)
		return true
	}

	// 3. GET /collection/{id} -> Retrieve item by ID
	if route.Method == "GET" && isItemEndpoint {
		idParam := pathParams[route.ParamNames[len(route.ParamNames)-1]]

		e.mu.RLock()
		defer e.mu.RUnlock()

		for _, item := range e.stateStore[collectionPrefix] {
			if fmt.Sprintf("%v", item["id"]) == idParam {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(item)
				return true
			}
		}

		// Not found in active store: return 404 if requested or synthesize
		if r.Header.Get("X-Hit-Mock-Status") == "404" || strings.Contains(r.Header.Get("Prefer"), "status=404") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":   "not_found",
				"message": fmt.Sprintf("Item %s not found in %s", idParam, collectionPrefix),
			})
			return true
		}

		// Synthesize sample matching requested ID
		resp := e.pickSuccessResponse(route)
		schema := e.extractResponseSchema(resp)
		sample := generateSample(e.spec.RawDoc, schema, 0)
		if m, ok := sample.(map[string]any); ok {
			if numID, err := strconv.Atoi(idParam); err == nil {
				m["id"] = numID
			} else {
				m["id"] = idParam
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(m)
			return true
		}
		return false
	}

	// 4. PUT / PATCH /collection/{id} -> Update item
	if (route.Method == "PUT" || route.Method == "PATCH") && isItemEndpoint {
		idParam := pathParams[route.ParamNames[len(route.ParamNames)-1]]

		var updates map[string]any
		bodyBytes, _ := io.ReadAll(r.Body)
		if len(bodyBytes) > 0 {
			_ = json.Unmarshal(bodyBytes, &updates)
		}

		e.mu.Lock()
		defer e.mu.Unlock()

		for i, item := range e.stateStore[collectionPrefix] {
			if fmt.Sprintf("%v", item["id"]) == idParam {
				for k, v := range updates {
					if k != "id" {
						item[k] = v
					}
				}
				e.stateStore[collectionPrefix][i] = item

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(item)
				return true
			}
		}

		// Item not found
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":   "not_found",
			"message": fmt.Sprintf("Item %s not found in %s", idParam, collectionPrefix),
		})
		return true
	}

	// 5. DELETE /collection/{id} -> Delete item
	if route.Method == "DELETE" && isItemEndpoint {
		idParam := pathParams[route.ParamNames[len(route.ParamNames)-1]]

		e.mu.Lock()
		defer e.mu.Unlock()

		var updated []map[string]any
		found := false
		for _, item := range e.stateStore[collectionPrefix] {
			if fmt.Sprintf("%v", item["id"]) == idParam {
				found = true
			} else {
				updated = append(updated, item)
			}
		}

		if found {
			e.stateStore[collectionPrefix] = updated
			resp := e.pickSuccessResponse(route)
			status := resp.StatusCode
			if status == 0 {
				status = http.StatusNoContent
			}
			w.WriteHeader(status)
			return true
		}

		// Not found
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":   "not_found",
			"message": fmt.Sprintf("Item %s not found in %s", idParam, collectionPrefix),
		})
		return true
	}

	return false
}

// Dynamic response generator using OpenAPI schemas, examples, and client overrides.
func (e *OpenAPIMockEngine) handleDynamicResponse(w http.ResponseWriter, r *http.Request, route *OpenAPIRoute, pathParams map[string]string) {
	// 1. Resolve Target Status Code
	status := e.resolveStatusCode(r, route)

	// 2. Select Response Definition from Spec
	resp, hasResp := route.Responses[strconv.Itoa(status)]
	if !hasResp {
		resp, hasResp = route.Responses["default"]
	}

	// 3. Check for 204 No Content
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}

	// 4. Set Declared Response Headers
	for k, v := range resp.Headers {
		if headerMap, ok := v.(map[string]any); ok {
			if val := generateSample(e.spec.RawDoc, headerMap["schema"], 0); val != nil {
				w.Header().Set(k, fmt.Sprintf("%v", val))
			}
		}
	}

	// 5. Extract Media Type & Payload
	mediaTypeKey := "application/json"
	var mediaType OpenAPIMediaType

	if len(resp.Content) > 0 {
		if mt, ok := resp.Content["application/json"]; ok {
			mediaType = mt
			mediaTypeKey = "application/json"
		} else {
			for k, mt := range resp.Content {
				mediaTypeKey = k
				mediaType = mt
				break
			}
		}
	} else if resp.Schema != nil {
		mediaType = OpenAPIMediaType{Schema: resp.Schema}
	}

	w.Header().Set("Content-Type", mediaTypeKey)

	// 6. Example Selection (Level 1)
	reqExample := e.resolveExamplePreference(r)
	if reqExample != "" && len(mediaType.Examples) > 0 {
		if exAny, ok := mediaType.Examples[reqExample]; ok {
			if exMap, ok := exAny.(map[string]any); ok && exMap["value"] != nil {
				e.writePayload(w, status, mediaTypeKey, exMap["value"])
				return
			}
		}
	}

	if mediaType.Example != nil {
		e.writePayload(w, status, mediaTypeKey, mediaType.Example)
		return
	}

	// 7. Schema-driven Sample Generation (Level 2 & 3)
	if mediaType.Schema != nil {
		sample := generateSample(e.spec.RawDoc, mediaType.Schema, 0)
		// If object has id field and path has id param, align them
		if m, ok := sample.(map[string]any); ok {
			for pName, pVal := range pathParams {
				if _, hasField := m[pName]; hasField {
					if num, err := strconv.Atoi(pVal); err == nil {
						m[pName] = num
					} else {
						m[pName] = pVal
					}
				}
			}
		}
		e.writePayload(w, status, mediaTypeKey, sample)
		return
	}

	// 8. Fallback payload if no schema declared
	if status >= 400 {
		e.writePayload(w, status, "application/json", map[string]any{
			"error":   http.StatusText(status),
			"code":    status,
			"message": fmt.Sprintf("Response for HTTP %d", status),
		})
		return
	}

	w.WriteHeader(status)
}

func (e *OpenAPIMockEngine) resolveStatusCode(r *http.Request, route *OpenAPIRoute) int {
	// A. Header override: X-Hit-Mock-Status: 404
	if sHeader := r.Header.Get("X-Hit-Mock-Status"); sHeader != "" {
		if c, err := strconv.Atoi(sHeader); err == nil && c >= 100 && c <= 599 {
			return c
		}
	}

	// B. Header override: Prefer: status=404 or Prefer: code=404
	if prefer := r.Header.Get("Prefer"); prefer != "" {
		re := regexp.MustCompile(`(?:status|code)=([0-9]{3})`)
		if m := re.FindStringSubmatch(prefer); len(m) > 1 {
			if c, err := strconv.Atoi(m[1]); err == nil {
				return c
			}
		}
	}

	// C. Query param override: ?__status=404 or ?mock_status=404
	q := r.URL.Query()
	for _, key := range []string{"__status", "mock_status"} {
		if sParam := q.Get(key); sParam != "" {
			if c, err := strconv.Atoi(sParam); err == nil && c >= 100 && c <= 599 {
				return c
			}
		}
	}

	// D. Pick Default Success Code (200, 201, 204, etc.)
	successResp := e.pickSuccessResponse(route)
	if successResp.StatusCode > 0 {
		return successResp.StatusCode
	}

	return http.StatusOK
}

func (e *OpenAPIMockEngine) resolveExamplePreference(r *http.Request) string {
	if exHeader := r.Header.Get("X-Hit-Mock-Example"); exHeader != "" {
		return exHeader
	}
	if prefer := r.Header.Get("Prefer"); prefer != "" {
		re := regexp.MustCompile(`example=([a-zA-Z0-9_-]+)`)
		if m := re.FindStringSubmatch(prefer); len(m) > 1 {
			return m[1]
		}
	}
	q := r.URL.Query()
	for _, key := range []string{"__example", "mock_example"} {
		if val := q.Get(key); val != "" {
			return val
		}
	}
	return ""
}

func (e *OpenAPIMockEngine) pickSuccessResponse(route *OpenAPIRoute) OpenAPIResponse {
	for _, codeStr := range []string{"200", "201", "204", "202"} {
		if resp, ok := route.Responses[codeStr]; ok {
			resp.StatusCode, _ = strconv.Atoi(codeStr)
			return resp
		}
	}

	var codes []int
	for k := range route.Responses {
		if c, err := strconv.Atoi(k); err == nil && c >= 200 && c < 400 {
			codes = append(codes, c)
		}
	}
	if len(codes) > 0 {
		sort.Ints(codes)
		codeStr := strconv.Itoa(codes[0])
		resp := route.Responses[codeStr]
		resp.StatusCode = codes[0]
		return resp
	}

	return OpenAPIResponse{StatusCode: http.StatusOK}
}

func (e *OpenAPIMockEngine) extractResponseSchema(resp OpenAPIResponse) any {
	if len(resp.Content) > 0 {
		if mt, ok := resp.Content["application/json"]; ok && mt.Schema != nil {
			return mt.Schema
		}
		for _, mt := range resp.Content {
			if mt.Schema != nil {
				return mt.Schema
			}
		}
	}
	return resp.Schema
}

func (e *OpenAPIMockEngine) writePayload(w http.ResponseWriter, status int, mediaType string, payload any) {
	var body []byte
	var err error

	if strings.Contains(mediaType, "json") {
		body, err = json.MarshalIndent(payload, "", "  ")
	} else if str, ok := payload.(string); ok {
		body = []byte(str)
	} else {
		body, err = json.Marshal(payload)
	}

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"failed to serialize mock response"}`))
		return
	}

	// Payload corruption support if chaos enabled
	shouldCorrupt := false
	if w.Header().Get("X-Chaos-Internal-Corrupt") == "true" {
		w.Header().Del("X-Chaos-Internal-Corrupt")
		shouldCorrupt = true
	} else if e.chaos != nil && e.chaos.opts.CorruptRate > 0 && status >= 200 && status < 300 && len(body) > 4 {
		if e.chaos.randomFloat() < e.chaos.opts.CorruptRate {
			shouldCorrupt = true
		}
	}

	if shouldCorrupt && len(body) > 4 {
		w.Header().Set("X-Hit-Chaos", "corrupt")
		cut := len(body) / 2
		if cut < 1 {
			cut = 1
		}
		body = body[:cut]
	}

	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// Helpers for schema resolution and sample synthesis
func resolveRef(doc map[string]any, ref string) map[string]any {
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	parts := strings.Split(ref[2:], "/")
	var current any = doc
	for _, p := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[p]
	}
	if res, ok := current.(map[string]any); ok {
		return res
	}
	return nil
}

func generateSample(doc map[string]any, schema any, depth int) any {
	if depth > 6 || schema == nil {
		return nil
	}
	sm, ok := schema.(map[string]any)
	if !ok {
		return nil
	}

	// 1. Resolve $ref
	if ref, ok := sm["$ref"].(string); ok && ref != "" {
		if resolved := resolveRef(doc, ref); resolved != nil {
			return generateSample(doc, resolved, depth+1)
		}
	}

	// 2. allOf composition
	if allOf, ok := sm["allOf"].([]any); ok && len(allOf) > 0 {
		merged := make(map[string]any)
		for _, part := range allOf {
			samplePart := generateSample(doc, part, depth+1)
			if pm, ok := samplePart.(map[string]any); ok {
				for k, v := range pm {
					merged[k] = v
				}
			}
		}
		return merged
	}

	// 3. oneOf / anyOf selection
	if oneOf, ok := sm["oneOf"].([]any); ok && len(oneOf) > 0 {
		return generateSample(doc, oneOf[0], depth+1)
	}
	if anyOf, ok := sm["anyOf"].([]any); ok && len(anyOf) > 0 {
		return generateSample(doc, anyOf[0], depth+1)
	}

	// 4. Explicit example or default
	if ex, ok := sm["example"]; ok && ex != nil {
		return ex
	}
	if def, ok := sm["default"]; ok && def != nil {
		return def
	}

	// 5. Schema Types
	sType, _ := sm["type"].(string)
	switch sType {
	case "string":
		format, _ := sm["format"].(string)
		switch format {
		case "date-time":
			return time.Now().UTC().Format(time.RFC3339)
		case "date":
			return time.Now().UTC().Format("2006-01-02")
		case "time":
			return time.Now().UTC().Format("15:04:05Z07:00")
		case "uuid":
			return randomUUID()
		case "email":
			return "user@example.com"
		case "uri", "url":
			return "https://example.com/api/v1/resource"
		case "hostname":
			return "api.example.com"
		case "ipv4":
			return "192.168.1.1"
		case "ipv6":
			return "::1"
		case "byte", "binary":
			return "dGVzdC1ieXRlcw=="
		}
		if enums, ok := sm["enum"].([]any); ok && len(enums) > 0 {
			return enums[0]
		}
		return "string"

	case "integer":
		if enums, ok := sm["enum"].([]any); ok && len(enums) > 0 {
			return enums[0]
		}
		if minVal, ok := sm["minimum"].(int); ok {
			return minVal
		}
		if minVal, ok := sm["minimum"].(float64); ok {
			return int(minVal)
		}
		return 1

	case "number":
		if enums, ok := sm["enum"].([]any); ok && len(enums) > 0 {
			return enums[0]
		}
		if minVal, ok := sm["minimum"].(float64); ok {
			return minVal
		}
		return 1.0

	case "boolean":
		return true

	case "array":
		if items := sm["items"]; items != nil {
			item1 := generateSample(doc, items, depth+1)
			item2 := generateSample(doc, items, depth+1)
			var arr []any
			if item1 != nil {
				arr = append(arr, item1)
			}
			if item2 != nil {
				arr = append(arr, item2)
			}
			if len(arr) > 0 {
				return arr
			}
		}
		return []any{}

	case "object":
		props, ok := sm["properties"].(map[string]any)
		if !ok {
			return make(map[string]any)
		}
		obj := make(map[string]any)
		for propName, propSchema := range props {
			val := generateSample(doc, propSchema, depth+1)
			if val != nil {
				obj[propName] = val
			} else {
				obj[propName] = generateValueHeuristic(propName)
			}
		}
		return obj

	default:
		if props, ok := sm["properties"].(map[string]any); ok {
			obj := make(map[string]any)
			for propName, propSchema := range props {
				obj[propName] = generateSample(doc, propSchema, depth+1)
			}
			return obj
		}
		return "value"
	}
}

func generateValueHeuristic(fieldName string) any {
	lower := strings.ToLower(fieldName)
	switch {
	case lower == "id":
		return 1
	case lower == "name":
		return "Sample Name"
	case lower == "title":
		return "Sample Title"
	case lower == "description":
		return "Sample description text"
	case lower == "status":
		return "active"
	case lower == "kind" || lower == "type":
		return "standard"
	case lower == "age":
		return 3
	case lower == "count" || lower == "total":
		return 10
	case lower == "email":
		return "user@example.com"
	case lower == "phone":
		return "+1-555-0199"
	default:
		return "sample"
	}
}

func randomUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]))
}

func containsString(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}
