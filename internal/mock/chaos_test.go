package mock

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseRate(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
		hasErr   bool
	}{
		{"20%", 0.2, false},
		{"0.2", 0.2, false},
		{"20", 0.2, false},
		{"100%", 1.0, false},
		{"1.0", 1.0, false},
		{"0%", 0.0, false},
		{"0", 0.0, false},
		{"150%", 0, true},
		{"-5%", 0, true},
		{"invalid", 0, true},
	}

	for _, tc := range tests {
		got, err := ParseRate(tc.input)
		if tc.hasErr {
			if err == nil {
				t.Errorf("ParseRate(%q) expected error, got nil", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("ParseRate(%q) unexpected error: %v", tc.input, err)
			}
			if (got-tc.expected) > 0.001 || (tc.expected-got) > 0.001 {
				t.Errorf("ParseRate(%q) = %v, expected %v", tc.input, got, tc.expected)
			}
		}
	}
}

func TestParseRateLimit(t *testing.T) {
	limit, win, err := ParseRateLimit("10/s")
	if err != nil || limit != 10 || win != time.Second {
		t.Fatalf("ParseRateLimit(10/s) = (%d, %v, %v)", limit, win, err)
	}

	limit, win, err = ParseRateLimit("100/m")
	if err != nil || limit != 100 || win != time.Minute {
		t.Fatalf("ParseRateLimit(100/m) = (%d, %v, %v)", limit, win, err)
	}

	limit, win, err = ParseRateLimit("5")
	if err != nil || limit != 5 || win != time.Second {
		t.Fatalf("ParseRateLimit(5) = (%d, %v, %v)", limit, win, err)
	}
}

func TestParseJitter(t *testing.T) {
	minD, maxD, err := ParseJitter("50ms-300ms")
	if err != nil || minD != 50*time.Millisecond || maxD != 300*time.Millisecond {
		t.Fatalf("ParseJitter(50ms-300ms) = (%v, %v, %v)", minD, maxD, err)
	}

	minD, maxD, err = ParseJitter("200ms")
	if err != nil || minD != 0 || maxD != 200*time.Millisecond {
		t.Fatalf("ParseJitter(200ms) = (%v, %v, %v)", minD, maxD, err)
	}
}

func TestChaosRateLimiter(t *testing.T) {
	s := NewChaosServer(ChaosOptions{
		RateLimit:  3,
		RateWindow: time.Second,
	})
	ts := httptest.NewServer(s)
	defer ts.Close()

	// First 3 requests should succeed
	for i := 0; i < 3; i++ {
		res, err := http.Get(ts.URL + "/health")
		if err != nil {
			t.Fatalf("request %d failed: %v", i+1, err)
		}
		if res.StatusCode != http.StatusOK {
			t.Fatalf("request %d expected 200, got %d", i+1, res.StatusCode)
		}
		if res.Header.Get("X-RateLimit-Limit") != "3" {
			t.Errorf("expected X-RateLimit-Limit 3, got %s", res.Header.Get("X-RateLimit-Limit"))
		}
	}

	// 4th request should be rate-limited (429)
	res, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("rate limited request failed: %v", err)
	}
	if res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests, got %d", res.StatusCode)
	}
	if res.Header.Get("Retry-After") == "" {
		t.Errorf("missing Retry-After header on 429 response")
	}
	if res.Header.Get("X-Hit-Chaos") != "rate-limit" {
		t.Errorf("expected X-Hit-Chaos: rate-limit, got %s", res.Header.Get("X-Hit-Chaos"))
	}

	// Test bypass header allows request through despite rate limit
	req, _ := http.NewRequest("GET", ts.URL+"/health", nil)
	req.Header.Set("X-Hit-Chaos-Bypass", "true")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("bypassed request failed: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected bypassed request to be 200 OK, got %d", res.StatusCode)
	}
}

func TestChaosFlakiness(t *testing.T) {
	// 100% flaky server returning 503
	s := NewChaosServer(ChaosOptions{
		FlakyRate:   1.0,
		FlakyStatus: 503,
	})
	ts := httptest.NewServer(s)
	defer ts.Close()

	res, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("flaky request error: %v", err)
	}
	if res.StatusCode != 503 {
		t.Fatalf("expected 503, got %d", res.StatusCode)
	}
	if res.Header.Get("X-Hit-Chaos") != "flaky" {
		t.Errorf("expected X-Hit-Chaos: flaky header, got %s", res.Header.Get("X-Hit-Chaos"))
	}
}

func TestChaosLatencyAndJitter(t *testing.T) {
	s := NewChaosServer(ChaosOptions{
		Latency: 50 * time.Millisecond,
	})
	ts := httptest.NewServer(s)
	defer ts.Close()

	start := time.Now()
	res, err := http.Get(ts.URL + "/health")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("healthcheck failed: err=%v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("healthcheck failed: status=%d", res.StatusCode)
	}
	if elapsed < 40*time.Millisecond {
		t.Errorf("expected at least 40ms elapsed with 50ms latency, got %v", elapsed)
	}
}

func TestChaosAuthExpiration(t *testing.T) {
	// Token expires after 100ms
	s := NewChaosServer(ChaosOptions{
		AuthExpire: 100 * time.Millisecond,
	})
	ts := httptest.NewServer(s)
	defer ts.Close()

	// 1. Login to get expiring token
	loginBody := bytes.NewBufferString(`{"username":"admin","password":"hunter2"}`)
	res, err := http.Post(ts.URL+"/auth/login", "application/json", loginBody)
	if err != nil {
		t.Fatalf("login failed: err=%v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("login failed: status=%v", res.StatusCode)
	}
	var loginResp map[string]any
	_ = json.NewDecoder(res.Body).Decode(&loginResp)
	token, _ := loginResp["access_token"].(string)
	if token == "" {
		t.Fatalf("missing access_token")
	}

	// 2. Immediate request succeeds
	client := &http.Client{}
	req, _ := http.NewRequest("GET", ts.URL+"/pets", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err = client.Do(req)
	if err != nil {
		t.Fatalf("expected 200 before token expiration, err=%v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("expected 200 before token expiration, got %d", res.StatusCode)
	}

	// 3. Wait for token to expire
	time.Sleep(150 * time.Millisecond)

	req, _ = http.NewRequest("GET", ts.URL+"/pets", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err = client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if res.StatusCode != 401 {
		t.Fatalf("expected 401 after token expiration, got %d", res.StatusCode)
	}
	if res.Header.Get("X-Hit-Chaos") != "auth-expired" {
		t.Errorf("expected X-Hit-Chaos: auth-expired, got %s", res.Header.Get("X-Hit-Chaos"))
	}
	if !strings.Contains(res.Header.Get("WWW-Authenticate"), "invalid_token") {
		t.Errorf("expected WWW-Authenticate invalid_token header, got %s", res.Header.Get("WWW-Authenticate"))
	}
}

func TestChaosHeaderOverrides(t *testing.T) {
	s := NewServer() // Normal server without global chaos
	ts := httptest.NewServer(s)
	defer ts.Close()

	client := &http.Client{}

	// Force 503 via header
	req, _ := http.NewRequest("GET", ts.URL+"/health", nil)
	req.Header.Set("X-Hit-Chaos-Status", "503")
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("expected 503 from X-Hit-Chaos-Status, err=%v", err)
	}
	if res.StatusCode != 503 {
		t.Fatalf("expected 503 from X-Hit-Chaos-Status, got status=%d", res.StatusCode)
	}

	// Force 429 via header
	req, _ = http.NewRequest("GET", ts.URL+"/health", nil)
	req.Header.Set("X-Hit-Chaos-RateLimit", "true")
	res, err = client.Do(req)
	if err != nil {
		t.Fatalf("expected 429 from X-Hit-Chaos-RateLimit, err=%v", err)
	}
	if res.StatusCode != 429 {
		t.Fatalf("expected 429 from X-Hit-Chaos-RateLimit, got status=%d", res.StatusCode)
	}

	// Force payload corruption via header
	req, _ = http.NewRequest("GET", ts.URL+"/health", nil)
	req.Header.Set("X-Hit-Chaos-Corrupt", "true")
	res, err = client.Do(req)
	if err != nil {
		t.Fatalf("expected 200, err=%v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got status=%d", res.StatusCode)
	}
	if res.Header.Get("X-Hit-Chaos") != "corrupt" {
		t.Errorf("expected X-Hit-Chaos: corrupt header, got %s", res.Header.Get("X-Hit-Chaos"))
	}
	var doc map[string]any
	// Should fail JSON decoding due to truncation
	if err := json.NewDecoder(res.Body).Decode(&doc); err == nil {
		t.Errorf("expected corrupted payload to fail JSON unmarshaling, but it succeeded: %+v", doc)
	}
}
