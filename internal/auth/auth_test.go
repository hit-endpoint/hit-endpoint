package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

func makeTestJWT(payload map[string]any) string {
	hdr := map[string]any{"alg": "HS256", "typ": "JWT"}
	hBytes, _ := json.Marshal(hdr)
	pBytes, _ := json.Marshal(payload)
	hB64 := base64.RawURLEncoding.EncodeToString(hBytes)
	pB64 := base64.RawURLEncoding.EncodeToString(pBytes)
	sig := base64.RawURLEncoding.EncodeToString([]byte("signature"))
	return fmt.Sprintf("%s.%s.%s", hB64, pB64, sig)
}

func TestDetectAuthKind(t *testing.T) {
	jwtStr := makeTestJWT(map[string]any{"sub": "user_123", "exp": time.Now().Add(1 * time.Hour).Unix()})

	tests := []struct {
		input    string
		wantKind AuthKind
	}{
		{jwtStr, AuthKindJWT},
		{"Bearer " + jwtStr, AuthKindJWT},
		{"Bearer some_opaque_api_token", AuthKindBearer},
		{"Cookie: session_id=abc; uid=10", AuthKindCookie},
		{"session_id=abc; uid=10", AuthKindCookie},
		{"connect.sid=s%3A12345", AuthKindCookie},
	}

	for _, tc := range tests {
		kind, _ := DetectAuthKind(tc.input)
		if kind != tc.wantKind {
			t.Errorf("DetectAuthKind(%q) = %v; want %v", tc.input, kind, tc.wantKind)
		}
	}
}

func TestParseJWT(t *testing.T) {
	expTime := time.Now().Add(2 * time.Hour)
	jwtStr := makeTestJWT(map[string]any{
		"sub":   "usr_42",
		"iss":   "https://auth.acme.com",
		"email": "developer@acme.com",
		"roles": []any{"admin", "editor"},
		"exp":   expTime.Unix(),
	})

	claims, err := ParseJWT(jwtStr)
	if err != nil {
		t.Fatalf("unexpected error parsing JWT: %v", err)
	}

	if claims.Subject != "usr_42" {
		t.Errorf("expected sub usr_42, got %s", claims.Subject)
	}
	if claims.Issuer != "https://auth.acme.com" {
		t.Errorf("expected iss https://auth.acme.com, got %s", claims.Issuer)
	}
	if claims.Email != "developer@acme.com" {
		t.Errorf("expected email developer@acme.com, got %s", claims.Email)
	}
	if len(claims.Roles) != 2 || claims.Roles[0] != "admin" {
		t.Errorf("unexpected roles: %v", claims.Roles)
	}
	if claims.Expired {
		t.Errorf("expected token to not be expired")
	}
	if claims.ExpiresIn <= 0 {
		t.Errorf("expected positive ExpiresIn, got %v", claims.ExpiresIn)
	}

	// Test expired token
	expiredJWT := makeTestJWT(map[string]any{
		"sub": "usr_expired",
		"exp": time.Now().Add(-1 * time.Hour).Unix(),
	})
	expClaims, err := ParseJWT(expiredJWT)
	if err != nil {
		t.Fatalf("unexpected error parsing expired JWT: %v", err)
	}
	if !expClaims.Expired {
		t.Errorf("expected expired to be true")
	}
}

func TestParseCookies(t *testing.T) {
	raw := "Cookie: session_id=abc12345; user=alice; token=xyz"
	items, normalized := ParseCookies(raw)

	if len(items) != 3 {
		t.Fatalf("expected 3 cookies, got %d", len(items))
	}
	if items[0].Name != "session_id" || items[0].Value != "abc12345" {
		t.Errorf("unexpected first cookie: %+v", items[0])
	}
	if normalized != "session_id=abc12345; user=alice; token=xyz" {
		t.Errorf("unexpected normalized cookie: %s", normalized)
	}
}

func TestGlobalAuth(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hit-auth-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	sa := &StoredAuth{
		Token:  "test_token_123",
		Cookie: "session=xyz",
	}
	if err := SaveGlobalAuth(sa); err != nil {
		t.Fatalf("SaveGlobalAuth failed: %v", err)
	}

	loaded, err := LoadGlobalAuth()
	if err != nil {
		t.Fatalf("LoadGlobalAuth failed: %v", err)
	}
	if loaded.Token != "test_token_123" || loaded.Cookie != "session=xyz" {
		t.Fatalf("unexpected loaded auth: %+v", loaded)
	}
}
