package mock

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ChaosOptions configures simulated instability and failure behaviors in the mock server.
type ChaosOptions struct {
	FlakyRate   float64       // 0.0 to 1.0 (probability of 5xx failure)
	FlakyStatus int           // Specific status code (500, 502, 503, 504) or 0 for random 5xx
	RateLimit   int           // Max requests per window (0 = disabled)
	RateWindow  time.Duration // Duration window for rate limiter (e.g. 1s)
	Latency     time.Duration // Fixed latency delay added to every request
	JitterMin   time.Duration // Minimum random jitter
	JitterMax   time.Duration // Maximum random jitter
	AuthExpire  time.Duration // Duration before issued tokens expire (0 = never expire)
	CorruptRate float64       // 0.0 to 1.0 (probability of corrupted response payloads)
}

// IsEnabled returns true if any chaos simulation feature is active.
func (c ChaosOptions) IsEnabled() bool {
	return c.FlakyRate > 0 || c.RateLimit > 0 || c.Latency > 0 || c.JitterMax > 0 || c.AuthExpire > 0 || c.CorruptRate > 0
}

// Summary returns a human-readable list of active chaos settings for CLI banners.
func (c ChaosOptions) Summary() string {
	var parts []string
	if c.FlakyRate > 0 {
		statusStr := "5xx"
		if c.FlakyStatus > 0 {
			statusStr = strconv.Itoa(c.FlakyStatus)
		}
		parts = append(parts, fmt.Sprintf("flaky=%.0f%% (%s)", c.FlakyRate*100, statusStr))
	}
	if c.RateLimit > 0 {
		windowStr := c.RateWindow.String()
		if c.RateWindow == time.Second {
			windowStr = "s"
		} else if c.RateWindow == time.Minute {
			windowStr = "m"
		}
		parts = append(parts, fmt.Sprintf("rate-limit=%d/%s", c.RateLimit, windowStr))
	}
	if c.Latency > 0 {
		parts = append(parts, fmt.Sprintf("latency=%v", c.Latency))
	}
	if c.JitterMax > 0 {
		if c.JitterMin > 0 {
			parts = append(parts, fmt.Sprintf("jitter=%v-%v", c.JitterMin, c.JitterMax))
		} else {
			parts = append(parts, fmt.Sprintf("jitter=%v", c.JitterMax))
		}
	}
	if c.AuthExpire > 0 {
		parts = append(parts, fmt.Sprintf("auth-expire=%v", c.AuthExpire))
	}
	if c.CorruptRate > 0 {
		parts = append(parts, fmt.Sprintf("corrupt=%.0f%%", c.CorruptRate*100))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

// ParseRate parses strings like "20%", "0.2", "20", or "1.0" into a float between 0.0 and 1.0.
func ParseRate(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty rate string")
	}

	isPercent := false
	if strings.HasSuffix(s, "%") {
		isPercent = true
		s = strings.TrimSuffix(s, "%")
	}

	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid rate '%s': %w", s, err)
	}

	if isPercent || val > 1.0 {
		val = val / 100.0
	}

	if val < 0.0 || val > 1.0 {
		return 0, fmt.Errorf("rate must be between 0.0 (0%%) and 1.0 (100%%), got %v", val)
	}

	return val, nil
}

// ParseRateLimit parses strings like "10/s", "100/m", "5", "20/min", "50/sec".
func ParseRateLimit(s string) (int, time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, fmt.Errorf("empty rate limit string")
	}

	parts := strings.SplitN(s, "/", 2)
	limit, err := strconv.Atoi(parts[0])
	if err != nil || limit <= 0 {
		return 0, 0, fmt.Errorf("invalid rate limit number '%s'", parts[0])
	}

	window := time.Second
	if len(parts) == 2 {
		unit := strings.ToLower(strings.TrimSpace(parts[1]))
		switch unit {
		case "s", "sec", "second", "seconds", "1s":
			window = time.Second
		case "m", "min", "minute", "minutes", "1m":
			window = time.Minute
		case "h", "hr", "hour", "hours", "1h":
			window = time.Hour
		default:
			parsed, err := time.ParseDuration(unit)
			if err != nil {
				return 0, 0, fmt.Errorf("invalid rate limit window '%s'", unit)
			}
			window = parsed
		}
	}

	return limit, window, nil
}

// ParseJitter parses strings like "50ms-300ms", "200ms", "1s".
func ParseJitter(s string) (time.Duration, time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, fmt.Errorf("empty jitter string")
	}

	if strings.Contains(s, "-") {
		parts := strings.SplitN(s, "-", 2)
		minD, err := time.ParseDuration(strings.TrimSpace(parts[0]))
		if err != nil {
			return 0, 0, fmt.Errorf("invalid jitter min duration '%s': %w", parts[0], err)
		}
		maxD, err := time.ParseDuration(strings.TrimSpace(parts[1]))
		if err != nil {
			return 0, 0, fmt.Errorf("invalid jitter max duration '%s': %w", parts[1], err)
		}
		if minD > maxD {
			minD, maxD = maxD, minD
		}
		return minD, maxD, nil
	}

	maxD, err := time.ParseDuration(s)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid jitter duration '%s': %w", s, err)
	}
	return 0, maxD, nil
}

// RateLimiter is a thread-safe token bucket rate limiter.
type RateLimiter struct {
	mu         sync.Mutex
	limit      int
	window     time.Duration
	tokens     float64
	lastRefill time.Time
}

// NewRateLimiter initializes a rate limiter.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	if limit <= 0 {
		return nil
	}
	if window <= 0 {
		window = time.Second
	}
	return &RateLimiter{
		limit:      limit,
		window:     window,
		tokens:     float64(limit),
		lastRefill: time.Now(),
	}
}

// Allow checks if a request is permitted under the rate limit.
// Returns (allowed, remaining, retryAfterSeconds, resetEpoch).
func (rl *RateLimiter) Allow() (bool, int, int, int64) {
	if rl == nil || rl.limit <= 0 {
		return true, 0, 0, 0
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastRefill)
	rl.lastRefill = now

	// Refill tokens based on elapsed time
	fillRate := float64(rl.limit) / rl.window.Seconds()
	rl.tokens += elapsed.Seconds() * fillRate
	if rl.tokens > float64(rl.limit) {
		rl.tokens = float64(rl.limit)
	}

	resetEpoch := now.Add(rl.window).Unix()

	if rl.tokens >= 1.0 {
		rl.tokens -= 1.0
		remaining := int(rl.tokens)
		return true, remaining, 0, resetEpoch
	}

	// Rate limit exceeded: calculate wait duration
	missing := 1.0 - rl.tokens
	waitSec := missing / fillRate
	retryAfter := int(waitSec + 0.999) // ceil
	if retryAfter < 1 {
		retryAfter = 1
	}

	return false, 0, retryAfter, resetEpoch
}

// TokenInfo holds metadata about an issued token.
type TokenInfo struct {
	Token     string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// TokenManager manages token issuance and lifecycle expiration.
type TokenManager struct {
	mu         sync.RWMutex
	duration   time.Duration
	tokens     map[string]TokenInfo
	legacyUsed time.Time
}

// NewTokenManager initializes a token manager.
func NewTokenManager(duration time.Duration) *TokenManager {
	return &TokenManager{
		duration: duration,
		tokens:   make(map[string]TokenInfo),
	}
}

// Issue registers a new token with its expiration window.
func (tm *TokenManager) Issue(customToken string) string {
	if tm == nil {
		return customToken
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tok := customToken
	if tok == "" {
		tok = fmt.Sprintf("chaos-token-%d", time.Now().UnixNano())
	}

	now := time.Now()
	var expires time.Time
	if tm.duration > 0 {
		expires = now.Add(tm.duration)
	}

	tm.tokens[tok] = TokenInfo{
		Token:     tok,
		IssuedAt:  now,
		ExpiresAt: expires,
	}
	return tok
}

// Validate checks if a token is authorized and whether it has expired.
// Returns (isAuthorized, isExpired).
func (tm *TokenManager) Validate(token string) (bool, bool) {
	if tm == nil || tm.duration <= 0 {
		return token == Token, false
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	now := time.Now()

	// Check dynamic token table
	if info, ok := tm.tokens[token]; ok {
		if !info.ExpiresAt.IsZero() && now.After(info.ExpiresAt) {
			return false, true
		}
		return true, false
	}

	// Support default static token "demo-token-123" with expiry
	if token == Token {
		if tm.legacyUsed.IsZero() {
			tm.legacyUsed = now
			return true, false
		}
		if now.Sub(tm.legacyUsed) > tm.duration {
			return false, true
		}
		return true, false
	}

	return false, false
}

// Reset clears all token records.
func (tm *TokenManager) Reset() {
	if tm == nil {
		return
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.tokens = make(map[string]TokenInfo)
	tm.legacyUsed = time.Time{}
}

// ChaosEngine evaluates and applies active chaos conditions for incoming requests.
type ChaosEngine struct {
	opts    ChaosOptions
	limiter *RateLimiter
	tokens  *TokenManager
	rnd     *rand.Rand
	rndMu   sync.Mutex
}

// NewChaosEngine initializes a chaos engine.
func NewChaosEngine(opts ChaosOptions) *ChaosEngine {
	var limiter *RateLimiter
	if opts.RateLimit > 0 {
		limiter = NewRateLimiter(opts.RateLimit, opts.RateWindow)
	}

	tokens := NewTokenManager(opts.AuthExpire)

	return &ChaosEngine{
		opts:    opts,
		limiter: limiter,
		tokens:  tokens,
		rnd:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (ce *ChaosEngine) randomFloat() float64 {
	ce.rndMu.Lock()
	defer ce.rndMu.Unlock()
	return ce.rnd.Float64()
}

func (ce *ChaosEngine) randomJitter() time.Duration {
	if ce.opts.JitterMax <= ce.opts.JitterMin {
		return ce.opts.JitterMin
	}
	ce.rndMu.Lock()
	diff := int64(ce.opts.JitterMax - ce.opts.JitterMin)
	jitter := time.Duration(ce.rnd.Int63n(diff)) + ce.opts.JitterMin
	ce.rndMu.Unlock()
	return jitter
}

func (ce *ChaosEngine) pickFlakyStatus() int {
	if ce.opts.FlakyStatus > 0 {
		return ce.opts.FlakyStatus
	}
	statuses := []int{500, 502, 503, 504}
	ce.rndMu.Lock()
	status := statuses[ce.rnd.Intn(len(statuses))]
	ce.rndMu.Unlock()
	return status
}
