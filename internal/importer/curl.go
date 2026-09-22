package importer

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/hit-endpoint/hit-endpoint/internal/zone"
)

func ImportCurl(curlInput string, targetDir string, nameOverride string) (*ImportReport, error) {
	rawCmd := strings.TrimSpace(curlInput)
	if fi, err := os.Stat(rawCmd); err == nil && !fi.IsDir() {
		b, err := os.ReadFile(rawCmd)
		if err == nil {
			rawCmd = strings.TrimSpace(string(b))
		}
	}

	tokens := tokenizeCmd(rawCmd)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty curl command")
	}

	startIndex := 0
	if tokens[0] == "curl" {
		startIndex = 1
	}

	method := ""
	rawURL := ""
	headers := make(map[string]any)
	bodyStr := ""
	authMap := make(map[string]any)

	for i := startIndex; i < len(tokens); i++ {
		t := tokens[i]
		switch {
		case (t == "-X" || t == "--request") && i+1 < len(tokens):
			method = strings.ToUpper(tokens[i+1])
			i++
		case (t == "-H" || t == "--header") && i+1 < len(tokens):
			h := tokens[i+1]
			idx := strings.Index(h, ":")
			if idx > 0 {
				k := strings.TrimSpace(h[:idx])
				v := strings.TrimSpace(h[idx+1:])
				if strings.EqualFold(k, "Authorization") && strings.HasPrefix(v, "Bearer ") {
					authMap["type"] = "bearer"
					authMap["token"] = strings.TrimPrefix(v, "Bearer ")
				} else {
					headers[k] = v
				}
			}
			i++
		case (t == "-d" || t == "--data" || t == "--data-raw" || t == "--data-binary") && i+1 < len(tokens):
			bodyStr = tokens[i+1]
			i++
		case (t == "-u" || t == "--user") && i+1 < len(tokens):
			u := tokens[i+1]
			parts := strings.SplitN(u, ":", 2)
			authMap["type"] = "basic"
			authMap["username"] = parts[0]
			if len(parts) > 1 {
				authMap["password"] = parts[1]
			}
			i++
		default:
			if !strings.HasPrefix(t, "-") && rawURL == "" {
				rawURL = t
			}
		}
	}

	if rawURL == "" {
		return nil, fmt.Errorf("could not find target URL in curl command")
	}

	if method == "" {
		if bodyStr != "" {
			method = "POST"
		} else {
			method = "GET"
		}
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %s: %w", rawURL, err)
	}

	queryParams := make(map[string]any)
	for k, v := range parsedURL.Query() {
		if len(v) == 1 {
			queryParams[k] = v[0]
		} else {
			queryParams[k] = v
		}
	}

	// Clean URL without query string
	parsedURL.RawQuery = ""
	baseURLWithoutQuery := parsedURL.String()

	reqName := nameOverride
	if reqName == "" {
		path := parsedURL.Path
		if path == "" || path == "/" {
			path = parsedURL.Host
		}
		reqName = fmt.Sprintf("%s %s", method, path)
	}

	fileSlug := Slugify(reqName, "request")
	destFile := filepath.Join(targetDir, fileSlug+".yaml")
	_ = os.MkdirAll(targetDir, 0755)

	reqSpec := make(map[string]any)
	reqSpec["name"] = reqName
	reqSpec["method"] = method
	reqSpec["url"] = baseURLWithoutQuery

	if len(headers) > 0 {
		reqSpec["headers"] = headers
	}
	if len(queryParams) > 0 {
		reqSpec["query"] = queryParams
	}
	if len(authMap) > 0 {
		reqSpec["auth"] = authMap
	}

	if bodyStr != "" {
		var parsedJSON any
		if err := json.Unmarshal([]byte(bodyStr), &parsedJSON); err == nil && (strings.HasPrefix(strings.TrimSpace(bodyStr), "{") || strings.HasPrefix(strings.TrimSpace(bodyStr), "[")) {
			reqSpec["body"] = map[string]any{"json": parsedJSON}
		} else {
			reqSpec["body"] = map[string]any{"raw": bodyStr}
		}
	}

	// Default status test
	reqSpec["tests"] = []any{
		map[string]any{"status": 200},
	}

	yStr, err := zone.DumpYAML(reqSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to generate YAML: %w", err)
	}

	if err := os.WriteFile(destFile, []byte(yStr), 0644); err != nil {
		return nil, fmt.Errorf("failed to write %s: %w", destFile, err)
	}

	report := &ImportReport{
		Collection:     fileSlug,
		Destination:    targetDir,
		Requests:       1,
		TestsConverted: 1,
		Files:          []string{destFile},
		Unconverted:    make(map[string][]string),
	}
	return report, nil
}

func tokenizeCmd(cmd string) []string {
	var tokens []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	runes := []rune(cmd)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && !inSingle {
			escaped = true
			continue
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if (r == ' ' || r == '\t' || r == '\n' || r == '\r') && !inSingle && !inDouble {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}
