package mock

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
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
	startedAt     time.Time
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
		nextID:    3,
		chaos:     NewChaosEngine(opts),
		startedAt: time.Now().UTC(),
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
		startedAt:     time.Now().UTC(),
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

	if len(parts) == 1 && parts[0] == "echo" {
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			s.handleWS(w, r)
			return
		}
		s.handleEcho(w, r, u)
		return
	}
	if len(parts) >= 1 && parts[0] == "ws" {
		s.handleWS(w, r)
		return
	}
	if len(parts) == 1 && (parts[0] == "sse" || parts[0] == "events") {
		s.handleSSE(w, r, u)
		return
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		s.handleGet(w, r, parts, u)
	case http.MethodPost:
		s.handlePost(w, r, parts, u)
	case http.MethodPut, http.MethodPatch:
		s.handlePut(w, r, parts)
	case http.MethodDelete:
		s.handleDelete(w, r, parts)
	default:
		s.sendJSON(w, http.StatusNotFound, map[string]string{"error": "not found"}, nil)
	}
}

func (s *Server) handleEcho(w http.ResponseWriter, r *http.Request, u *url.URL) {
	headers := make(map[string]string)
	for k, v := range r.Header {
		headers[k] = strings.Join(v, ", ")
	}
	raw, _ := io.ReadAll(r.Body)
	resp := map[string]any{
		"method":  r.Method,
		"headers": headers,
		"query":   u.Query(),
	}
	if len(raw) > 0 {
		resp["body"] = string(raw)
		var parsed any
		if err := json.Unmarshal(raw, &parsed); err == nil {
			resp["json"] = parsed
		} else if vals, err := url.ParseQuery(string(raw)); err == nil && len(vals) > 0 {
			formMap := make(map[string]any, len(vals))
			for k, v := range vals {
				if len(v) == 1 {
					formMap[k] = v[0]
				} else {
					formMap[k] = v
				}
			}
			resp["form"] = formMap
		}
	}
	s.sendJSON(w, http.StatusOK, resp, nil)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request, parts []string, u *url.URL) {
	if len(parts) == 1 && parts[0] == "health" {
		startedAt := s.startedAt
		if startedAt.IsZero() {
			startedAt = time.Now().UTC()
		}
		s.sendJSON(w, http.StatusOK, map[string]any{
			"status":     "ok",
			"time":       float64(time.Now().UnixNano()) / 1e9,
			"started_at": startedAt.Format(time.RFC3339Nano),
		}, nil)
		return
	}

	if len(parts) == 1 && parts[0] == "slow" {
		ms := 100
		if val := u.Query().Get("ms"); val != "" {
			if parsed, err := strconv.Atoi(val); err == nil && parsed >= 0 {
				ms = parsed
			} else if d, err := time.ParseDuration(val); err == nil && d >= 0 {
				ms = int(d.Milliseconds())
			}
		} else if val := u.Query().Get("delay"); val != "" {
			if d, err := time.ParseDuration(val); err == nil && d >= 0 {
				ms = int(d.Milliseconds())
			} else if parsed, err := strconv.Atoi(val); err == nil && parsed >= 0 {
				ms = parsed
			}
		}
		time.Sleep(time.Duration(ms) * time.Millisecond)
		s.sendJSON(w, http.StatusOK, map[string]any{"slept_ms": ms}, nil)
		return
	}

	if len(parts) == 1 && parts[0] == "pets" {
		if !s.checkAuth(w, r) {
			return
		}
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
		total := len(items)

		offset := 0
		hasOffset := false
		if offStr := u.Query().Get("offset"); offStr != "" {
			if v, err := strconv.Atoi(offStr); err == nil && v >= 0 {
				offset = v
				hasOffset = true
			}
		}
		limit := 0
		for _, k := range []string{"limit", "per_page", "page_size", "size"} {
			if limStr := u.Query().Get(k); limStr != "" {
				if v, err := strconv.Atoi(limStr); err == nil && v > 0 {
					limit = v
					break
				}
			}
		}
		page := 1
		if pageStr := u.Query().Get("page"); pageStr != "" {
			if pNum, err := strconv.Atoi(pageStr); err == nil && pNum >= 1 {
				page = pNum
				if limit <= 0 {
					limit = 1
				}
				if !hasOffset {
					offset = (page - 1) * limit
				}
			}
		}
		if offset > 0 {
			if offset >= len(items) {
				items = []Pet{}
			} else {
				items = items[offset:]
			}
		}
		if limit > 0 && limit < len(items) {
			items = items[:limit]
		}

		effectiveLimit := limit
		if effectiveLimit <= 0 {
			effectiveLimit = total
			if effectiveLimit == 0 {
				effectiveLimit = 10
			}
		}
		totalPages := 1
		if total > 0 && effectiveLimit > 0 {
			totalPages = (total + effectiveLimit - 1) / effectiveLimit
		}
		hasMore := offset+len(items) < total

		headers := map[string]string{
			"X-Total-Count": strconv.Itoa(total),
			"X-Page":        strconv.Itoa(page),
			"X-Per-Page":    strconv.Itoa(effectiveLimit),
		}
		s.sendJSON(w, http.StatusOK, map[string]any{
			"items":       items,
			"total":       total,
			"page":        page,
			"limit":       effectiveLimit,
			"offset":      offset,
			"total_pages": totalPages,
			"has_more":    hasMore,
		}, headers)
		return
	}

	if len(parts) == 2 && parts[0] == "pets" {
		id, err := strconv.Atoi(parts[1])
		if err == nil {
			if !s.checkAuth(w, r) {
				return
			}
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
		if username == "admin" && (password == "hunter2" || password == "password" || password == "") {
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
				"token":        tok,
				"token_type":   "bearer",
				"expires_in":   expiresIn,
				"username":     username,
			}, nil)
			return
		}
		s.sendJSON(w, http.StatusUnauthorized, map[string]string{"error": "bad credentials"}, nil)
		return
	}

	if len(parts) == 1 && parts[0] == "pets" {
		if !s.checkAuth(w, r) {
			return
		}
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

func (s *Server) handlePut(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) == 2 && parts[0] == "pets" {
		id, err := strconv.Atoi(parts[1])
		if err == nil {
			if !s.checkAuth(w, r) {
				return
			}
			raw, _ := io.ReadAll(r.Body)
			var data map[string]any
			if len(raw) > 0 {
				_ = json.Unmarshal(raw, &data)
			}
			s.mu.Lock()
			pet, existed := s.pets[id]
			if existed {
				if name, ok := data["name"].(string); ok && strings.TrimSpace(name) != "" {
					pet.Name = name
				}
				if kind, ok := data["kind"].(string); ok && strings.TrimSpace(kind) != "" {
					pet.Kind = kind
				}
				if ageNum, ok := data["age"].(float64); ok {
					pet.Age = int(ageNum)
				}
				s.pets[id] = pet
			}
			s.mu.Unlock()

			if existed {
				s.sendJSON(w, http.StatusOK, pet, nil)
				return
			}
			s.sendJSON(w, http.StatusNotFound, map[string]string{"error": "no such pet"}, nil)
			return
		}
	}

	s.sendJSON(w, http.StatusNotFound, map[string]string{"error": "not found"}, nil)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) == 2 && parts[0] == "pets" {
		id, err := strconv.Atoi(parts[1])
		if err == nil {
			if !s.checkAuth(w, r) {
				return
			}
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

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request, u *url.URL) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	count := 3
	if cStr := u.Query().Get("count"); cStr != "" {
		if parsed, err := strconv.Atoi(cStr); err == nil && parsed > 0 {
			count = parsed
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for i := 1; i <= count; i++ {
		select {
		case <-r.Context().Done():
			return
		default:
		}
		fmt.Fprintf(w, "id: %d\nevent: message\ndata: {\"seq\":%d,\"text\":\"token-%d\"}\n\n", i, i, i)
		flusher.Flush()
		if i < count {
			time.Sleep(15 * time.Millisecond)
		}
	}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		s.sendJSON(w, http.StatusBadRequest, map[string]string{"error": "expected WebSocket Upgrade header"}, nil)
		return
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		s.sendJSON(w, http.StatusBadRequest, map[string]string{"error": "missing Sec-WebSocket-Key"}, nil)
		return
	}
	const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	h := sha1.New()
	h.Write([]byte(key + wsGUID))
	accept := base64.StdEncoding.EncodeToString(h.Sum(nil))

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}
	conn, bufrw, err := hj.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()

	bufrw.WriteString("HTTP/1.1 101 Switching Protocols\r\n")
	bufrw.WriteString("Upgrade: websocket\r\n")
	bufrw.WriteString("Connection: Upgrade\r\n")
	bufrw.WriteString(fmt.Sprintf("Sec-WebSocket-Accept: %s\r\n\r\n", accept))
	_ = bufrw.Flush()

	for {
		b0, err := bufrw.ReadByte()
		if err != nil {
			return
		}
		b1, err := bufrw.ReadByte()
		if err != nil {
			return
		}
		opcode := b0 & 0x0F
		masked := (b1 & 0x80) != 0
		lenByte := b1 & 0x7F

		var payloadLen int64
		if lenByte <= 125 {
			payloadLen = int64(lenByte)
		} else if lenByte == 126 {
			ext := make([]byte, 2)
			if _, err := io.ReadFull(bufrw, ext); err != nil {
				return
			}
			payloadLen = int64(binary.BigEndian.Uint16(ext))
		} else {
			ext := make([]byte, 8)
			if _, err := io.ReadFull(bufrw, ext); err != nil {
				return
			}
			payloadLen = int64(binary.BigEndian.Uint64(ext))
		}

		var maskKey []byte
		if masked {
			maskKey = make([]byte, 4)
			if _, err := io.ReadFull(bufrw, maskKey); err != nil {
				return
			}
		}

		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(bufrw, payload); err != nil {
			return
		}
		if masked {
			for i := int64(0); i < payloadLen; i++ {
				payload[i] ^= maskKey[i%4]
			}
		}

		if opcode == 0x8 { // Close
			return
		}
		respOpcode := opcode
		if opcode == 0x9 { // Ping -> Pong
			respOpcode = 0xA
		}

		var frame []byte
		frame = append(frame, 0x80|respOpcode)
		if payloadLen <= 125 {
			frame = append(frame, byte(payloadLen))
		} else if payloadLen <= 65535 {
			frame = append(frame, 126)
			ext := make([]byte, 2)
			binary.BigEndian.PutUint16(ext, uint16(payloadLen))
			frame = append(frame, ext...)
		} else {
			frame = append(frame, 127)
			ext := make([]byte, 8)
			binary.BigEndian.PutUint64(ext, uint64(payloadLen))
			frame = append(frame, ext...)
		}
		frame = append(frame, payload...)
		if _, err := bufrw.Write(frame); err != nil {
			return
		}
		_ = bufrw.Flush()
	}
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
