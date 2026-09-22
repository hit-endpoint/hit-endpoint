package report

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hit-endpoint/hit-endpoint/internal/history"
	"github.com/hit-endpoint/hit-endpoint/internal/types"
)

// HAR represents the root container of an HTTP Archive format (1.2).
type HAR struct {
	Log HARLog `json:"log"`
}

type HARLog struct {
	Version string     `json:"version"`
	Creator HARCreator `json:"creator"`
	Entries []HAREntry `json:"entries"`
	Comment string     `json:"comment,omitempty"`
}

type HARCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Comment string `json:"comment,omitempty"`
}

type HAREntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            float64     `json:"time"` // Total elapsed time in milliseconds
	Request         HARRequest  `json:"request"`
	Response        HARResponse `json:"response"`
	Cache           HARCache    `json:"cache"`
	Timings         HARTimings  `json:"timings"`
	ServerIPAddress string      `json:"serverIPAddress,omitempty"`
	Connection      string      `json:"connection,omitempty"`
	Comment         string      `json:"comment,omitempty"`
}

type HARRequest struct {
	Method      string          `json:"method"`
	URL         string          `json:"url"`
	HTTPVersion string          `json:"httpVersion"`
	Cookies     []HARCookie     `json:"cookies"`
	Headers     []HARHeader     `json:"headers"`
	QueryString []HARQueryParam `json:"queryString"`
	PostData    *HARPostData    `json:"postData,omitempty"`
	HeadersSize int64           `json:"headersSize"`
	BodySize    int64           `json:"bodySize"`
	Comment     string          `json:"comment,omitempty"`
}

type HARResponse struct {
	Status      int         `json:"status"`
	StatusText  string      `json:"statusText"`
	HTTPVersion string      `json:"httpVersion"`
	Cookies     []HARCookie `json:"cookies"`
	Headers     []HARHeader `json:"headers"`
	Content     HARContent  `json:"content"`
	RedirectURL string      `json:"redirectURL"`
	HeadersSize int64       `json:"headersSize"`
	BodySize    int64       `json:"bodySize"`
	Comment     string      `json:"comment,omitempty"`
}

type HARCookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Path     string `json:"path,omitempty"`
	Domain   string `json:"domain,omitempty"`
	Expires  string `json:"expires,omitempty"`
	HTTPOnly bool   `json:"httpOnly,omitempty"`
	Secure   bool   `json:"secure,omitempty"`
	Comment  string `json:"comment,omitempty"`
}

type HARHeader struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Comment string `json:"comment,omitempty"`
}

type HARQueryParam struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Comment string `json:"comment,omitempty"`
}

type HARPostData struct {
	MimeType string       `json:"mimeType"`
	Text     string       `json:"text,omitempty"`
	Params   []HARParam   `json:"params,omitempty"`
	Comment  string       `json:"comment,omitempty"`
}

type HARParam struct {
	Name        string `json:"name"`
	Value       string `json:"value,omitempty"`
	FileName    string `json:"fileName,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	Comment     string `json:"comment,omitempty"`
}

type HARContent struct {
	Size        int64  `json:"size"`
	Compression int64  `json:"compression,omitempty"`
	MimeType    string `json:"mimeType"`
	Text        string `json:"text,omitempty"`
	Encoding    string `json:"encoding,omitempty"`
	Comment     string `json:"comment,omitempty"`
}

type HARCache struct{}

type HARTimings struct {
	Blocked float64 `json:"blocked,omitempty"`
	DNS     float64 `json:"dns,omitempty"`
	Connect float64 `json:"connect,omitempty"`
	Send    float64 `json:"send"`
	Wait    float64 `json:"wait"`
	Receive float64 `json:"receive"`
	SSL     float64 `json:"ssl,omitempty"`
	Comment string  `json:"comment,omitempty"`
}

const (
	HARVersion     = "1.2"
	HARCreatorName = "hit-api-tester"
	HARCreatorVer  = "0.1.0"
)

func newBaseHAR() *HAR {
	return &HAR{
		Log: HARLog{
			Version: HARVersion,
			Creator: HARCreator{
				Name:    HARCreatorName,
				Version: HARCreatorVer,
				Comment: "Git-friendly API testing and execution archive",
			},
			Entries: []HAREntry{},
		},
	}
}

func parseQueryParams(rawURL string) []HARQueryParam {
	params := []HARQueryParam{}
	u, err := url.Parse(rawURL)
	if err != nil {
		return params
	}

	q := u.Query()
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		for _, v := range q[k] {
			params = append(params, HARQueryParam{
				Name:  k,
				Value: v,
			})
		}
	}
	return params
}

func parseHeaders(headerMap map[string]string) ([]HARHeader, []HARCookie) {
	headers := []HARHeader{}
	cookies := []HARCookie{}

	if len(headerMap) == 0 {
		return headers, cookies
	}

	keys := make([]string, 0, len(headerMap))
	for k := range headerMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		val := headerMap[k]
		headers = append(headers, HARHeader{
			Name:  k,
			Value: val,
		})

		lower := strings.ToLower(k)
		if lower == "cookie" {
			// e.g. "a=b; c=d"
			parts := strings.Split(val, ";")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				kv := strings.SplitN(part, "=", 2)
				cName := strings.TrimSpace(kv[0])
				cVal := ""
				if len(kv) == 2 {
					cVal = strings.TrimSpace(kv[1])
				}
				cookies = append(cookies, HARCookie{
					Name:  cName,
					Value: cVal,
				})
			}
		} else if lower == "set-cookie" {
			// e.g. "session=123; Path=/; HttpOnly"
			parts := strings.Split(val, ";")
			if len(parts) > 0 {
				kv := strings.SplitN(strings.TrimSpace(parts[0]), "=", 2)
				cName := strings.TrimSpace(kv[0])
				cVal := ""
				if len(kv) == 2 {
					cVal = strings.TrimSpace(kv[1])
				}
				cookie := HARCookie{
					Name:  cName,
					Value: cVal,
				}
				for _, attr := range parts[1:] {
					attr = strings.TrimSpace(attr)
					attrLower := strings.ToLower(attr)
					if strings.HasPrefix(attrLower, "path=") {
						cookie.Path = strings.TrimSpace(attr[5:])
					} else if strings.HasPrefix(attrLower, "domain=") {
						cookie.Domain = strings.TrimSpace(attr[7:])
					} else if strings.HasPrefix(attrLower, "expires=") {
						cookie.Expires = strings.TrimSpace(attr[8:])
					} else if attrLower == "httponly" {
						cookie.HTTPOnly = true
					} else if attrLower == "secure" {
						cookie.Secure = true
					}
				}
				cookies = append(cookies, cookie)
			}
		}
	}

	return headers, cookies
}

func detectMimeType(headers map[string]string, body string) string {
	for k, v := range headers {
		if strings.EqualFold(k, "content-type") {
			return v
		}
	}
	trimmed := strings.TrimSpace(body)
	if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
		return "application/json"
	}
	if strings.HasPrefix(trimmed, "<") && strings.HasSuffix(trimmed, ">") {
		return "application/xml"
	}
	return "text/plain; charset=utf-8"
}

func estimateHeadersSize(headers []HARHeader) int64 {
	if len(headers) == 0 {
		return -1
	}
	var size int64
	for _, h := range headers {
		size += int64(len(h.Name) + 2 + len(h.Value) + 2)
	}
	return size
}

// BuildHARFromResults converts test execution results into a HAR 1.2 archive.
func BuildHARFromResults(results []*types.Result) *HAR {
	har := newBaseHAR()
	now := time.Now().UTC()

	for i, r := range results {
		// Stagger timestamps slightly if startedDateTime is derived from current run
		entryTime := now.Add(-time.Duration(len(results)-i) * time.Millisecond)
		startedStr := entryTime.Format(time.RFC3339Nano)

		reqHeaders, reqCookies := parseHeaders(r.RequestHeaders)
		respHeaders, respCookies := parseHeaders(r.Headers)
		queryParams := parseQueryParams(r.Url)

		reqMime := detectMimeType(r.RequestHeaders, r.RequestBody)
		var postData *HARPostData
		if r.RequestBody != "" {
			postData = &HARPostData{
				MimeType: reqMime,
				Text:     r.RequestBody,
				Params:   []HARParam{},
			}
		}

		respMime := detectMimeType(r.Headers, r.Text)
		contentSize := r.Size
		if contentSize <= 0 && r.Text != "" {
			contentSize = int64(len(r.Text))
		}

		statusText := r.Reason
		if statusText == "" && r.Status > 0 {
			statusText = http.StatusText(r.Status)
		}

		redirectURL := ""
		for k, v := range r.Headers {
			if strings.EqualFold(k, "location") {
				redirectURL = v
				break
			}
		}

		comment := r.Ref
		if comment == "" {
			comment = r.Name
		}

		entry := HAREntry{
			StartedDateTime: startedStr,
			Time:            r.ElapsedMs,
			Request: HARRequest{
				Method:      r.Method,
				URL:         r.Url,
				HTTPVersion: "HTTP/1.1",
				Cookies:     reqCookies,
				Headers:     reqHeaders,
				QueryString: queryParams,
				PostData:    postData,
				HeadersSize: estimateHeadersSize(reqHeaders),
				BodySize:    int64(len(r.RequestBody)),
			},
			Response: HARResponse{
				Status:      r.Status,
				StatusText:  statusText,
				HTTPVersion: "HTTP/1.1",
				Cookies:     respCookies,
				Headers:     respHeaders,
				Content: HARContent{
					Size:     contentSize,
					MimeType: respMime,
					Text:     r.Text,
				},
				RedirectURL: redirectURL,
				HeadersSize: estimateHeadersSize(respHeaders),
				BodySize:    contentSize,
			},
			Cache: HARCache{},
			Timings: HARTimings{
				Send:    0,
				Wait:    r.ElapsedMs,
				Receive: 0,
			},
			Comment: comment,
		}

		har.Log.Entries = append(har.Log.Entries, entry)
	}

	return har
}

// BuildHARFromHistory converts history log entries into a HAR 1.2 archive.
func BuildHARFromHistory(entries []*history.Entry) *HAR {
	har := newBaseHAR()

	for _, e := range entries {
		if e == nil {
			continue
		}
		startedStr := e.Timestamp
		if startedStr == "" {
			startedStr = time.Now().UTC().Format(time.RFC3339Nano)
		}

		reqHeaders, reqCookies := parseHeaders(e.Headers)
		respHeaders, respCookies := parseHeaders(e.ResponseHeaders)
		queryParams := parseQueryParams(e.Url)

		reqMime := detectMimeType(e.Headers, e.Body)
		var postData *HARPostData
		if e.Body != "" {
			postData = &HARPostData{
				MimeType: reqMime,
				Text:     e.Body,
				Params:   []HARParam{},
			}
		}

		respMime := detectMimeType(e.ResponseHeaders, e.ResponseBody)
		contentSize := e.ResponseSize
		if contentSize <= 0 && e.ResponseBody != "" {
			contentSize = int64(len(e.ResponseBody))
		}

		statusText := e.Reason
		if statusText == "" && e.Status > 0 {
			statusText = http.StatusText(e.Status)
		}

		redirectURL := ""
		for k, v := range e.ResponseHeaders {
			if strings.EqualFold(k, "location") {
				redirectURL = v
				break
			}
		}

		comment := e.ID
		if e.Ref != "" {
			comment = fmt.Sprintf("%s (%s)", e.Ref, e.ID)
		}

		entry := HAREntry{
			StartedDateTime: startedStr,
			Time:            e.ElapsedMs,
			Request: HARRequest{
				Method:      e.Method,
				URL:         e.Url,
				HTTPVersion: "HTTP/1.1",
				Cookies:     reqCookies,
				Headers:     reqHeaders,
				QueryString: queryParams,
				PostData:    postData,
				HeadersSize: estimateHeadersSize(reqHeaders),
				BodySize:    int64(len(e.Body)),
			},
			Response: HARResponse{
				Status:      e.Status,
				StatusText:  statusText,
				HTTPVersion: "HTTP/1.1",
				Cookies:     respCookies,
				Headers:     respHeaders,
				Content: HARContent{
					Size:     contentSize,
					MimeType: respMime,
					Text:     e.ResponseBody,
				},
				RedirectURL: redirectURL,
				HeadersSize: estimateHeadersSize(respHeaders),
				BodySize:    contentSize,
			},
			Cache: HARCache{},
			Timings: HARTimings{
				Send:    0,
				Wait:    e.ElapsedMs,
				Receive: 0,
			},
			Comment: comment,
		}

		har.Log.Entries = append(har.Log.Entries, entry)
	}

	return har
}

// ToJSON marshals the HAR structure into indented JSON.
func ToJSON(har *HAR) ([]byte, error) {
	return json.MarshalIndent(har, "", "  ")
}

// WriteHAR writes the HAR data directly to the specified file path.
func WriteHAR(har *HAR, path string) error {
	data, err := ToJSON(har)
	if err != nil {
		return fmt.Errorf("serialize HAR: %w", err)
	}

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write HAR file: %w", err)
	}

	return nil
}
