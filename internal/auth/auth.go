package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AuthKind represents the detected type of credential.
type AuthKind string

const (
	AuthKindJWT    AuthKind = "jwt"
	AuthKindBearer AuthKind = "bearer"
	AuthKindCookie AuthKind = "cookie"
	AuthKindUnknown AuthKind = "unknown"
)

// TokenClaims represents parsed standard JWT claims.
type TokenClaims struct {
	Subject    string         `json:"sub,omitempty"`
	Issuer     string         `json:"iss,omitempty"`
	Audience   string         `json:"aud,omitempty"`
	Email      string         `json:"email,omitempty"`
	Username   string         `json:"username,omitempty"`
	Roles      []string       `json:"roles,omitempty"`
	ExpiresAt  time.Time      `json:"exp_time,omitempty"`
	IssuedAt   time.Time      `json:"iat_time,omitempty"`
	Expired    bool           `json:"expired"`
	ExpiresIn  time.Duration  `json:"expires_in"`
	RawPayload map[string]any `json:"payload,omitempty"`
}

// CookieItem represents a single parsed cookie name/value pair.
type CookieItem struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// CleanCredential sanitizes input string (trims quotes, whitespace, and header prefixes).
func CleanCredential(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, `"'` + "`")
	s = strings.TrimSpace(s)
	return s
}

// DetectAuthKind inspects the raw text and determines whether it represents a JWT, Cookie, or Bearer token.
func DetectAuthKind(raw string) (AuthKind, string) {
	clean := CleanCredential(raw)

	// Check for Cookie prefix or typical cookie format
	if strings.HasPrefix(strings.ToLower(clean), "cookie:") {
		return AuthKindCookie, CleanCredential(strings.TrimPrefix(clean[7:], " "))
	}

	// Check for Bearer prefix
	if strings.HasPrefix(strings.ToLower(clean), "bearer ") {
		tokenPart := CleanCredential(clean[7:])
		if IsJWT(tokenPart) {
			return AuthKindJWT, tokenPart
		}
		return AuthKindBearer, tokenPart
	}

	// Check if raw matches JWT pattern
	if IsJWT(clean) {
		return AuthKindJWT, clean
	}

	// Check if string contains cookie key=value pairs separated by semicolon
	if strings.Contains(clean, "=") && (strings.Contains(clean, ";") || strings.Contains(clean, "connect.sid") || strings.Contains(clean, "session")) {
		return AuthKindCookie, clean
	}

	// Default fallback
	if len(clean) > 0 {
		return AuthKindBearer, clean
	}

	return AuthKindUnknown, clean
}

// IsJWT checks if a string has the 3 dot-separated base64url segments of a JSON Web Token.
func IsJWT(s string) bool {
	s = CleanCredential(s)
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return false
	}
	// Segment 0 must be non-empty base64
	if len(parts[0]) == 0 || len(parts[1]) == 0 {
		return false
	}
	// Try decoding segment 0 as JSON
	hdrBytes, err := decodeBase64URL(parts[0])
	if err != nil {
		return false
	}
	var hdr map[string]any
	if err := json.Unmarshal(hdrBytes, &hdr); err != nil {
		return false
	}
	return true
}

func decodeBase64URL(seg string) ([]byte, error) {
	// Pad segment if needed for standard URLEncoding
	seg = strings.TrimSpace(seg)
	if rem := len(seg) % 4; rem != 0 {
		seg += strings.Repeat("=", 4-rem)
	}
	if b, err := base64.URLEncoding.DecodeString(seg); err == nil {
		return b, nil
	}
	if b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(seg, "=")); err == nil {
		return b, nil
	}
	return base64.StdEncoding.DecodeString(seg)
}

// ParseJWT extracts claims and expiration status from a raw JWT string.
func ParseJWT(raw string) (*TokenClaims, error) {
	clean := CleanCredential(raw)
	if strings.HasPrefix(strings.ToLower(clean), "bearer ") {
		clean = CleanCredential(clean[7:])
	}

	parts := strings.Split(clean, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid JWT: expected 3 dot-separated segments")
	}

	payloadBytes, err := decodeBase64URL(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse JWT payload JSON: %w", err)
	}

	claims := &TokenClaims{
		RawPayload: payload,
	}

	if sub, ok := payload["sub"].(string); ok {
		claims.Subject = sub
	}
	if iss, ok := payload["iss"].(string); ok {
		claims.Issuer = iss
	}
	if aud, ok := payload["aud"].(string); ok {
		claims.Audience = aud
	}
	if email, ok := payload["email"].(string); ok {
		claims.Email = email
	}
	if user, ok := payload["preferred_username"].(string); ok {
		claims.Username = user
	} else if user, ok := payload["username"].(string); ok {
		claims.Username = user
	} else if name, ok := payload["name"].(string); ok {
		claims.Username = name
	}

	// Roles / groups
	if rolesRaw, ok := payload["roles"].([]any); ok {
		for _, r := range rolesRaw {
			if rs, ok := r.(string); ok {
				claims.Roles = append(claims.Roles, rs)
			}
		}
	} else if groupsRaw, ok := payload["groups"].([]any); ok {
		for _, g := range groupsRaw {
			if gs, ok := g.(string); ok {
				claims.Roles = append(claims.Roles, gs)
			}
		}
	}

	now := time.Now()

	// Parse exp
	if expVal, ok := payload["exp"]; ok {
		var expSec int64
		switch v := expVal.(type) {
		case float64:
			expSec = int64(v)
		case json.Number:
			expSec, _ = v.Int64()
		}
		if expSec > 0 {
			// If exp is in milliseconds (> year 2200 in seconds)
			if expSec > 10000000000 {
				expSec = expSec / 1000
			}
			claims.ExpiresAt = time.Unix(expSec, 0)
			if now.After(claims.ExpiresAt) {
				claims.Expired = true
				claims.ExpiresIn = -now.Sub(claims.ExpiresAt)
			} else {
				claims.Expired = false
				claims.ExpiresIn = claims.ExpiresAt.Sub(now)
			}
		}
	}

	// Parse iat
	if iatVal, ok := payload["iat"]; ok {
		var iatSec int64
		switch v := iatVal.(type) {
		case float64:
			iatSec = int64(v)
		case json.Number:
			iatSec, _ = v.Int64()
		}
		if iatSec > 0 {
			if iatSec > 10000000000 {
				iatSec = iatSec / 1000
			}
			claims.IssuedAt = time.Unix(iatSec, 0)
		}
	}

	return claims, nil
}

// ParseCookies parses a raw Cookie string into key-value items and returns normalized header value.
func ParseCookies(raw string) ([]CookieItem, string) {
	clean := CleanCredential(raw)
	if strings.HasPrefix(strings.ToLower(clean), "cookie:") {
		clean = CleanCredential(clean[7:])
	}

	parts := strings.Split(clean, ";")
	var items []CookieItem
	var normalized []string

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, "=", 2)
		name := strings.TrimSpace(kv[0])
		val := ""
		if len(kv) > 1 {
			val = strings.TrimSpace(kv[1])
		}
		if name != "" {
			items = append(items, CookieItem{Name: name, Value: val})
			normalized = append(normalized, fmt.Sprintf("%s=%s", name, val))
		}
	}

	return items, strings.Join(normalized, "; ")
}

// GlobalAuthFile returns the path to global auth file ~/.hit/auth.json.
func GlobalAuthFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".hit", "auth.json"), nil
}

// StoredAuth contains saved token and cookie data.
type StoredAuth struct {
	Token     string    `json:"token,omitempty"`
	Cookie    string    `json:"cookie,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// LoadGlobalAuth loads credentials from ~/.hit/auth.json.
func LoadGlobalAuth() (*StoredAuth, error) {
	p, err := GlobalAuthFile()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return &StoredAuth{}, nil
	}
	var sa StoredAuth
	if err := json.Unmarshal(b, &sa); err != nil {
		return &StoredAuth{}, nil
	}
	return &sa, nil
}

// SaveGlobalAuth saves credentials to ~/.hit/auth.json.
func SaveGlobalAuth(sa *StoredAuth) error {
	p, err := GlobalAuthFile()
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	sa.UpdatedAt = time.Now()
	b, err := json.MarshalIndent(sa, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0600)
}
