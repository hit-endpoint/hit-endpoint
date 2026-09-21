package mock

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const Token = "demo-token-123"

type Pet struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Age  int    `json:"age"`
}

type Server struct {
	mu            sync.RWMutex
	pets          map[int]Pet
	nextID        int
	chaos         *ChaosEngine
	openAPIEngine *OpenAPIMockEngine
}

func NewServer() *Server {
	return NewChaosServer(ChaosOptions{})
}

func NewChaosServer(opts ChaosOptions) *Server {
	return &Server{
		pets: map[int]Pet{
			1: {ID: 1, Name: "Rex", Kind: "dog", Age: 3},
			2: {ID: 2, Name: "Tom", Kind: "cat", Age: 5},
		},
		nextID: 3,
		chaos:  NewChaosEngine(opts),
	}
}

// NewOpenAPIServer constructs a mock server driven by an OpenAPI 3.x or Swagger 2.0 specification.
func NewOpenAPIServer(specPathOrURL string, chaos ChaosOptions, stateful bool) (*Server, error) {
	spec, rawBytes, err := LoadOpenAPISpec(specPathOrURL)
	if err != nil {
		return nil, err
	}

	chaosEngine := NewChaosEngine(chaos)
	engine, err := NewOpenAPIMockEngine(spec, rawBytes, stateful, chaosEngine)
	if err != nil {
		return nil, err
	}

	return &Server{
		pets:          make(map[int]Pet),
		chaos:         chaosEngine,
		openAPIEngine: engine,
	}, nil
}

func (s *Server) OpenAPIEngine() *OpenAPIMockEngine {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.openAPIEngine
}

func (s *Server) SetChaos(opts ChaosOptions) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chaos = NewChaosEngine(opts)
	if s.openAPIEngine != nil {
		s.openAPIEngine.chaos = s.chaos
	}
}

func (s *Server) ChaosOptions() ChaosOptions {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.chaos == nil {
		return ChaosOptions{}
	}
	return s.chaos.opts
}

func (s *Server) sendJSON(w http.ResponseWriter, status int, payload any, headers map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	for k, v := range headers {
		w.Header().Set(k, v)
	}

	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			http.Error(w, `{"error":"internal json error"}`, http.StatusInternalServerError)
			return
		}
	}

	shouldCorrupt := false
	if w.Header().Get("X-Chaos-Internal-Corrupt") == "true" {
		w.Header().Del("X-Chaos-Internal-Corrupt")
		shouldCorrupt = true
	} else if s.chaos != nil && s.chaos.opts.CorruptRate > 0 && status >= 200 && status < 300 && len(body) > 4 {
		if s.chaos.randomFloat() < s.chaos.opts.CorruptRate {
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
	if len(body) > 0 {
		_, _ = w.Write(body)
	}
}

func (s *Server) authed(r *http.Request) (bool, bool) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return false, false
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if s.chaos == nil || s.chaos.tokens == nil {
		return token == Token, false
	}
	return s.chaos.tokens.Validate(token)
}

func (s *Server) checkAuth(w http.ResponseWriter, r *http.Request) bool {
	valid, expired := s.authed(r)
	if !valid {
		if expired {
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token", error_description="The token has expired"`)
			s.sendJSON(w, http.StatusUnauthorized, map[string]any{
				"error": "token expired",
				"code":  "TOKEN_EXPIRED",
				"chaos": true,
			}, map[string]string{"X-Hit-Chaos": "auth-expired"})
			return false
		}
		s.sendJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing or invalid bearer token"}, nil)
		return false
	}
	return true
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 1. Check bypass header
	bypass := r.Header.Get("X-Hit-Chaos-Bypass")
	isBypassed := bypass == "true" || bypass == "1"

	if !isBypassed && s.chaos != nil {
		// 2. Delay / Jitter
		delayHeader := r.Header.Get("X-Hit-Chaos-Delay")
		if delayHeader != "" {
			if d, err := time.ParseDuration(delayHeader); err == nil && d > 0 {
				time.Sleep(d)
			}
		} else {
			delay := s.chaos.opts.Latency
			if s.chaos.opts.JitterMax > 0 {
				delay += s.chaos.randomJitter()
			}
			if delay > 0 {
				time.Sleep(delay)
			}
		}

		// 3. Direct status override header
		statusOverride := r.Header.Get("X-Hit-Chaos-Status")
		if statusOverride != "" {
			if code, err := strconv.Atoi(statusOverride); err == nil && code >= 100 && code <= 599 {
				s.sendJSON(w, code, map[string]any{
					"error":  fmt.Sprintf("chaos: forced status %d", code),
					"status": code,
					"chaos":  true,
				}, map[string]string{"X-Hit-Chaos": "status-override"})
				return
			}
		}

		// 4. Rate limiting
		forceRateLimit := r.Header.Get("X-Hit-Chaos-RateLimit") == "true" || r.Header.Get("X-Hit-Chaos-RateLimit") == "1"
		if s.chaos.limiter != nil || forceRateLimit {
			allowed, remaining, retryAfter, resetEpoch := true, 0, 1, time.Now().Add(time.Second).Unix()
			if s.chaos.limiter != nil {
				allowed, remaining, retryAfter, resetEpoch = s.chaos.limiter.Allow()
			}
			if forceRateLimit {
				allowed = false
				retryAfter = 1
			}

			if !allowed {
				headers := map[string]string{
					"Retry-After":           strconv.Itoa(retryAfter),
					"X-RateLimit-Limit":     strconv.Itoa(s.chaos.opts.RateLimit),
					"X-RateLimit-Remaining": "0",
					"X-RateLimit-Reset":     strconv.FormatInt(resetEpoch, 10),
					"X-Hit-Chaos":           "rate-limit",
				}
				s.sendJSON(w, http.StatusTooManyRequests, map[string]any{
					"error":       "rate limit exceeded",
					"retry_after": retryAfter,
					"chaos":       true,
				}, headers)
				return
			}

			// Add rate limit headers to successful responses
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(s.chaos.opts.RateLimit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetEpoch, 10))
		}

		// 5. Flakiness / Fault injection
		if s.chaos.opts.FlakyRate > 0 {
			if s.chaos.randomFloat() < s.chaos.opts.FlakyRate {
				flakyStatus := s.chaos.pickFlakyStatus()
				s.sendJSON(w, flakyStatus, map[string]any{
					"error":  "chaos: simulated transient failure",
					"status": flakyStatus,
					"chaos":  true,
				}, map[string]string{"X-Hit-Chaos": "flaky"})
				return
			}
		}

		// 6. Request header corruption flag
		if r.Header.Get("X-Hit-Chaos-Corrupt") == "true" || r.Header.Get("X-Hit-Chaos-Corrupt") == "1" {
			w.Header().Set("X-Chaos-Internal-Corrupt", "true")
		}
	}

	s.mu.RLock()
	openapi := s.openAPIEngine
	s.mu.RUnlock()
	if openapi != nil {
		openapi.ServeHTTP(w, r)
		return
	}

	u, err := url.Parse(r.RequestURI)
	if err != nil {
		s.sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid url"}, nil)
		return
	}

	path := strings.Trim(u.Path, "/")
	parts := strings.Split(path, "/")
	if path == "" {
		parts = []string{}
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGet(w, r, parts, u)
	case http.MethodPost:
		s.handlePost(w, r, parts, u)
	case http.MethodDelete:
		s.handleDelete(w, r, parts)
	default:
		s.sendJSON(w, http.StatusNotFound, map[string]string{"error": "not found"}, nil)
	}
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request, parts []string, u *url.URL) {
	if len(parts) == 1 && parts[0] == "health" {
		s.sendJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"time":   float64(time.Now().UnixNano()) / 1e9,
		}, nil)
		return
	}

	if len(parts) == 1 && parts[0] == "slow" {
		ms := 100
		if val := u.Query().Get("ms"); val != "" {
			if parsed, err := strconv.Atoi(val); err == nil && parsed >= 0 {
				ms = parsed
			}
		}
		time.Sleep(time.Duration(ms) * time.Millisecond)
		s.sendJSON(w, http.StatusOK, map[string]any{"slept_ms": ms}, nil)
		return
	}

	if len(parts) == 1 && parts[0] == "echo" {
		headers := make(map[string]string)
		for k, v := range r.Header {
			headers[k] = strings.Join(v, ", ")
		}
		s.sendJSON(w, http.StatusOK, map[string]any{
			"headers": headers,
			"query":   u.Query(),
		}, nil)
		return
	}

	if !s.checkAuth(w, r) {
		return
	}

	if len(parts) == 1 && parts[0] == "pets" {
		kindFilter := u.Query().Get("kind")
		s.mu.RLock()
		var items []Pet
		for id := 1; id < s.nextID; id++ {
			if p, ok := s.pets[id]; ok {
				if kindFilter == "" || p.Kind == kindFilter {
					items = append(items, p)
				}
			}
		}
		s.mu.RUnlock()

		if items == nil {
			items = []Pet{}
		}

		headers := map[string]string{
			"X-Total-Count": strconv.Itoa(len(items)),
		}
		s.sendJSON(w, http.StatusOK, map[string]any{
			"items": items,
			"total": len(items),
		}, headers)
		return
	}

	if len(parts) == 2 && parts[0] == "pets" {
		id, err := strconv.Atoi(parts[1])
		if err == nil {
			s.mu.RLock()
			pet, ok := s.pets[id]
			s.mu.RUnlock()
			if ok {
				s.sendJSON(w, http.StatusOK, pet, nil)
				return
			}
			s.sendJSON(w, http.StatusNotFound, map[string]string{"error": "no such pet"}, nil)
			return
		}
	}

	s.sendJSON(w, http.StatusNotFound, map[string]string{"error": "not found"}, nil)
}

func (s *Server) handlePost(w http.ResponseWriter, r *http.Request, parts []string, u *url.URL) {
	raw, _ := io.ReadAll(r.Body)
	var data map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &data)
	}
	if data == nil {
		data = make(map[string]any)
	}

	if len(parts) == 2 && parts[0] == "auth" && parts[1] == "login" {
		username, _ := data["username"].(string)
		password, _ := data["password"].(string)
		if username == "admin" && password == "hunter2" {
			tok := Token
			expiresIn := 3600
			if s.chaos != nil && s.chaos.opts.AuthExpire > 0 {
				tok = s.chaos.tokens.Issue("")
				expiresIn = int(s.chaos.opts.AuthExpire.Seconds())
				if expiresIn <= 0 {
					expiresIn = 1
				}
			}
			s.sendJSON(w, http.StatusOK, map[string]any{
				"access_token": tok,
				"token_type":   "bearer",
				"expires_in":   expiresIn,
			}, nil)
			return
		}
		s.sendJSON(w, http.StatusUnauthorized, map[string]string{"error": "bad credentials"}, nil)
		return
	}

	if !s.checkAuth(w, r) {
		return
	}

	if len(parts) == 1 && parts[0] == "pets" {
		name, _ := data["name"].(string)
		if strings.TrimSpace(name) == "" {
			s.sendJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"}, nil)
			return
		}

		kind, _ := data["kind"].(string)
		if kind == "" {
			kind = "unknown"
		}

		age := 0
		if ageNum, ok := data["age"].(float64); ok {
			age = int(ageNum)
		}

		s.mu.Lock()
		pid := s.nextID
		s.nextID++
		pet := Pet{
			ID:   pid,
			Name: name,
			Kind: kind,
			Age:  age,
		}
		s.pets[pid] = pet
		s.mu.Unlock()

		headers := map[string]string{
			"Location": fmt.Sprintf("/pets/%d", pid),
		}
		s.sendJSON(w, http.StatusCreated, pet, headers)
		return
	}

	s.sendJSON(w, http.StatusNotFound, map[string]string{"error": "not found"}, nil)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request, parts []string) {
	if !s.checkAuth(w, r) {
		return
	}

	if len(parts) == 2 && parts[0] == "pets" {
		id, err := strconv.Atoi(parts[1])
		if err == nil {
			s.mu.Lock()
			_, existed := s.pets[id]
			if existed {
				delete(s.pets, id)
			}
			s.mu.Unlock()

			if existed {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			s.sendJSON(w, http.StatusNotFound, map[string]string{"error": "no such pet"}, nil)
			return
		}
	}

	s.sendJSON(w, http.StatusNotFound, map[string]string{"error": "not found"}, nil)
}

func (s *Server) ListenAndServe(port int) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler: s,
	}
	return server.Serve(listener)
}
