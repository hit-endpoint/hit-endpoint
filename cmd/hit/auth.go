package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/hit-endpoint/hit-endpoint/internal/auth"
	"github.com/hit-endpoint/hit-endpoint/internal/output"
	"github.com/hit-endpoint/hit-endpoint/internal/runner"
	"github.com/hit-endpoint/hit-endpoint/internal/zone"
)

func cmdAuth(args []string, globals *GlobalFlags, p *output.Printer) error {
	var (
		forceGlobal bool
		positional  []string
	)

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--global" || a == "-g":
			forceGlobal = true
		case strings.HasPrefix(a, "-"):
			// Unknown flag
			continue
		default:
			positional = append(positional, a)
		}
	}

	action := ""
	if len(positional) > 0 {
		action = strings.ToLower(positional[0])
	}

	z, _ := zone.FindOrNone(globals.Zone)

	switch action {
	case "show", "status", "info":
		return showAuthStatus(z, globals.Server, forceGlobal, p)
	case "clear", "unset", "rm", "delete":
		return clearStoredAuth(z, globals.Server, forceGlobal, p)
	case "token", "jwt", "bearer":
		if len(positional) > 1 {
			val := strings.Join(positional[1:], " ")
			return saveToken(val, z, globals.Server, forceGlobal, p)
		}
		return promptAndSave("token", z, globals.Server, forceGlobal, p)
	case "cookie", "cookies":
		if len(positional) > 1 {
			val := strings.Join(positional[1:], " ")
			return saveCookie(val, z, globals.Server, forceGlobal, p)
		}
		return promptAndSave("cookie", z, globals.Server, forceGlobal, p)
	case "paste":
		return promptAndSave("auto", z, globals.Server, forceGlobal, p)
	}

	// If arguments provided: hit auth "ey..." or hit auth "session=..."
	if len(positional) > 0 {
		val := strings.Join(positional, " ")
		return autoDetectAndSave(val, z, globals.Server, forceGlobal, p)
	}

	// If no arguments: check if stdin has piped data
	stat, err := os.Stdin.Stat()
	if err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		b, err := io.ReadAll(os.Stdin)
		if err == nil && len(strings.TrimSpace(string(b))) > 0 {
			return autoDetectAndSave(string(b), z, globals.Server, forceGlobal, p)
		}
	}

	// Interactive paste prompt
	return promptAndSave("auto", z, globals.Server, forceGlobal, p)
}

func promptAndSave(mode string, z *zone.Zone, serverName string, forceGlobal bool, p *output.Printer) error {
	p.Out(p.Bold("Paste your credential below and press Enter:"))
	if mode == "token" {
		p.Out(p.Dim("(Enter JWT token or Bearer API key)"))
	} else if mode == "cookie" {
		p.Out(p.Dim("(Enter browser Cookie header string, e.g. session_id=abc; uid=10)"))
	} else {
		p.Out(p.Dim("(Supports JWT tokens, browser Cookie headers, or Bearer keys - auto-detected)"))
	}
	fmt.Print("> ")

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return fmt.Errorf("failed to read input: %w", err)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		p.Out(p.Yellow("No input provided. Auth unchanged."))
		return nil
	}

	if mode == "token" {
		return saveToken(line, z, serverName, forceGlobal, p)
	} else if mode == "cookie" {
		return saveCookie(line, z, serverName, forceGlobal, p)
	}
	return autoDetectAndSave(line, z, serverName, forceGlobal, p)
}

func autoDetectAndSave(raw string, z *zone.Zone, serverName string, forceGlobal bool, p *output.Printer) error {
	kind, clean := auth.DetectAuthKind(raw)
	switch kind {
	case auth.AuthKindCookie:
		return saveCookie(clean, z, serverName, forceGlobal, p)
	case auth.AuthKindJWT:
		return saveToken(clean, z, serverName, forceGlobal, p)
	default:
		return saveToken(clean, z, serverName, forceGlobal, p)
	}
}

func saveToken(rawToken string, z *zone.Zone, serverName string, forceGlobal bool, p *output.Printer) error {
	clean := auth.CleanCredential(rawToken)
	if strings.HasPrefix(strings.ToLower(clean), "bearer ") {
		clean = auth.CleanCredential(clean[7:])
	}

	if z != nil && !forceGlobal {
		sess, err := runner.NewSession(runner.SessionOptions{
			Zone:       z,
			ServerName: serverName,
			Persist:    true,
		})
		if err != nil {
			return err
		}
		defer sess.Close()

		sess.SetVar("token", clean, nil)
		p.Out(fmt.Sprintf("%s Saved auth token for server %s", p.Green("✓"), p.Bold(sess.Server.Name)))
	} else {
		sa, _ := auth.LoadGlobalAuth()
		if sa == nil {
			sa = &auth.StoredAuth{}
		}
		sa.Token = clean
		if err := auth.SaveGlobalAuth(sa); err != nil {
			return fmt.Errorf("failed to save global auth: %w", err)
		}
		p.Out(fmt.Sprintf("%s Saved global auth token (~/.hit/auth.json)", p.Green("✓")))
	}

	// If valid JWT, display parsed claims
	if claims, err := auth.ParseJWT(clean); err == nil {
		p.Out(fmt.Sprintf("  • %s:  %s", p.Dim("Format"), p.Cyan("JSON Web Token (JWT)")))
		if claims.Subject != "" {
			p.Out(fmt.Sprintf("  • %s: %s", p.Dim("Subject"), claims.Subject))
		}
		if claims.Email != "" {
			p.Out(fmt.Sprintf("  • %s:   %s", p.Dim("Email"), claims.Email))
		}
		if claims.Username != "" {
			p.Out(fmt.Sprintf("  • %s:    %s", p.Dim("User"), claims.Username))
		}
		if len(claims.Roles) > 0 {
			p.Out(fmt.Sprintf("  • %s:   %s", p.Dim("Roles"), strings.Join(claims.Roles, ", ")))
		}
		if !claims.ExpiresAt.IsZero() {
			if claims.Expired {
				p.Out(fmt.Sprintf("  • %s:  %s (expired %s ago at %s)",
					p.Dim("Status"),
					p.Red("EXPIRED"),
					(-claims.ExpiresIn).Round(time.Second),
					claims.ExpiresAt.UTC().Format("2006-01-02 15:04:05 UTC"),
				))
			} else {
				p.Out(fmt.Sprintf("  • %s:  %s (expires in %s at %s)",
					p.Dim("Status"),
					p.Green("VALID"),
					claims.ExpiresIn.Round(time.Second),
					claims.ExpiresAt.UTC().Format("2006-01-02 15:04:05 UTC"),
				))
			}
		}
	} else {
		preview := clean
		if len(preview) > 12 {
			preview = preview[:4] + "..." + preview[len(preview)-4:]
		}
		p.Out(fmt.Sprintf("  • %s:  %s (%s)", p.Dim("Format"), p.Cyan("Bearer Token"), preview))
	}

	p.Out(p.Dim("  Used automatically in requests, or via {{token}} in YAML files."))
	return nil
}

func saveCookie(rawCookie string, z *zone.Zone, serverName string, forceGlobal bool, p *output.Printer) error {
	items, normalized := auth.ParseCookies(rawCookie)
	if len(items) == 0 {
		return fmt.Errorf("no valid cookie name=value pairs found in input")
	}

	if z != nil && !forceGlobal {
		sess, err := runner.NewSession(runner.SessionOptions{
			Zone:       z,
			ServerName: serverName,
			Persist:    true,
		})
		if err != nil {
			return err
		}
		defer sess.Close()

		sess.SetVar("cookie", normalized, nil)
		p.Out(fmt.Sprintf("%s Saved %d session cookie(s) for server %s", p.Green("✓"), len(items), p.Bold(sess.Server.Name)))
	} else {
		sa, _ := auth.LoadGlobalAuth()
		if sa == nil {
			sa = &auth.StoredAuth{}
		}
		sa.Cookie = normalized
		if err := auth.SaveGlobalAuth(sa); err != nil {
			return fmt.Errorf("failed to save global auth: %w", err)
		}
		p.Out(fmt.Sprintf("%s Saved %d global session cookie(s) (~/.hit/auth.json)", p.Green("✓"), len(items)))
	}

	var names []string
	for _, it := range items {
		names = append(names, it.Name)
	}
	p.Out(fmt.Sprintf("  • %s: %s", p.Dim("Cookies"), strings.Join(names, ", ")))
	p.Out(p.Dim("  Injected automatically in requests, or via {{cookie}} in YAML files."))
	return nil
}

func showAuthStatus(z *zone.Zone, serverName string, forceGlobal bool, p *output.Printer) error {
	var token, cookie string
	scope := "global (~/.hit/auth.json)"

	if z != nil && !forceGlobal {
		sess, err := runner.NewSession(runner.SessionOptions{
			Zone:       z,
			ServerName: serverName,
			Persist:    false,
		})
		if err == nil {
			defer sess.Close()
			scope = fmt.Sprintf("zone server '%s'", sess.Server.Name)
			if t, ok := sess.SessionVars["token"].(string); ok {
				token = t
			}
			if c, ok := sess.SessionVars["cookie"].(string); ok {
				cookie = c
			}
		}
	}

	if token == "" && cookie == "" {
		sa, _ := auth.LoadGlobalAuth()
		if sa != nil {
			token = sa.Token
			cookie = sa.Cookie
			if token != "" || cookie != "" {
				scope = "global (~/.hit/auth.json)"
			}
		}
	}

	p.Out(fmt.Sprintf("%s Active Authentication (%s)", p.Bold("Hit Auth:"), p.Cyan(scope)))

	if token == "" && cookie == "" {
		p.Out(p.Dim("  No active token or cookies stored."))
		p.Out("  Try: hit auth \"eyJ...\" or hit auth cookie \"session_id=...\"")
		return nil
	}

	if token != "" {
		p.Out(p.Bold("  Token:"))
		if claims, err := auth.ParseJWT(token); err == nil {
			p.Out(fmt.Sprintf("    • %s:   JSON Web Token (JWT)", p.Dim("Type")))
			if claims.Subject != "" {
				p.Out(fmt.Sprintf("    • %s:    %s", p.Dim("Sub"), claims.Subject))
			}
			if claims.Email != "" {
				p.Out(fmt.Sprintf("    • %s:  %s", p.Dim("Email"), claims.Email))
			}
			if !claims.ExpiresAt.IsZero() {
				if claims.Expired {
					p.Out(fmt.Sprintf("    • %s: %s (expired %s ago)", p.Dim("Expiry"), p.Red("EXPIRED"), (-claims.ExpiresIn).Round(time.Second)))
				} else {
					p.Out(fmt.Sprintf("    • %s: %s (expires in %s)", p.Dim("Expiry"), p.Green("VALID"), claims.ExpiresIn.Round(time.Second)))
				}
			}
		} else {
			preview := token
			if len(preview) > 12 {
				preview = preview[:4] + "..." + preview[len(preview)-4:]
			}
			p.Out(fmt.Sprintf("    • %s:   Bearer (%s)", p.Dim("Type"), preview))
		}
	}

	if cookie != "" {
		items, _ := auth.ParseCookies(cookie)
		var names []string
		for _, it := range items {
			names = append(names, it.Name)
		}
		p.Out(p.Bold("  Cookies:"))
		p.Out(fmt.Sprintf("    • %s:  %d stored (%s)", p.Dim("Count"), len(items), strings.Join(names, ", ")))
	}

	p.Out(p.Dim("\n  Run 'hit auth clear' to remove stored credentials."))
	return nil
}

func clearStoredAuth(z *zone.Zone, serverName string, forceGlobal bool, p *output.Printer) error {
	if z != nil && !forceGlobal {
		sess, err := runner.NewSession(runner.SessionOptions{
			Zone:       z,
			ServerName: serverName,
			Persist:    true,
		})
		if err == nil {
			defer sess.Close()
			if sess.State != nil {
				sess.State.Unset("token")
				sess.State.Unset("cookie")
				_ = sess.State.Save()
			}
			p.Out(fmt.Sprintf("%s Cleared credentials for server %s", p.Green("✓"), p.Bold(sess.Server.Name)))
			return nil
		}
	}

	sa, _ := auth.LoadGlobalAuth()
	if sa != nil {
		sa.Token = ""
		sa.Cookie = ""
		_ = auth.SaveGlobalAuth(sa)
	}
	p.Out(fmt.Sprintf("%s Cleared global credentials (~/.hit/auth.json)", p.Green("✓")))
	return nil
}
