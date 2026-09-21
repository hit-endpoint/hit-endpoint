package history

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"hit/internal/types"
)

const (
	DefaultMaxEntries = 1000
	PruneThreshold    = 1200
)

type TestSummary struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

type Entry struct {
	ID              string            `json:"id"`
	Timestamp       string            `json:"timestamp"` // ISO 8601 UTC
	Source          string            `json:"source"`    // "adhoc", "run", "flow"
	Ref             string            `json:"ref,omitempty"`
	Zone            string            `json:"zone,omitempty"`
	Server          string            `json:"server,omitempty"`
	Method          string            `json:"method"`
	Url             string            `json:"url"`
	Headers         map[string]string `json:"headers,omitempty"`
	Body            string            `json:"body,omitempty"`
	Status          int               `json:"status"`
	Reason          string            `json:"reason"`
	ElapsedMs       float64           `json:"elapsed_ms"`
	ResponseSize    int64             `json:"response_size"`
	ResponseHeaders map[string]string `json:"response_headers,omitempty"`
	ResponseBody    string            `json:"response_body,omitempty"`
	Tests           []TestSummary     `json:"tests,omitempty"`
	Captures        map[string]any    `json:"captures,omitempty"`
	Error           string            `json:"error,omitempty"`
}

type Store struct {
	filePath   string
	maxEntries int
	mu         sync.Mutex
	opCount    int
}

func GetHistoryPath(zoneRoot string) string {
	if zoneRoot != "" {
		return filepath.Join(zoneRoot, ".hit", "history.jsonl")
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		return filepath.Join(home, ".hit", "history.jsonl")
	}
	return filepath.Join(".", ".hit", "history.jsonl")
}

func NewStore(filePath string) *Store {
	if filePath == "" {
		filePath = GetHistoryPath("")
	}
	return &Store{
		filePath:   filePath,
		maxEntries: DefaultMaxEntries,
	}
}

func (s *Store) FilePath() string {
	return s.filePath
}

func generateID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("hit_%d_%s", time.Now().Unix(), hex.EncodeToString(b))
}

func NewEntryFromResult(r *types.Result, source, zoneName, serverName string, maskFn func(string) string) *Entry {
	if maskFn == nil {
		maskFn = func(s string) string { return s }
	}

	headers := make(map[string]string)
	for k, v := range r.RequestHeaders {
		lk := strings.ToLower(k)
		if lk == "authorization" || lk == "proxy-authorization" || strings.Contains(lk, "secret") || strings.Contains(lk, "key") {
			headers[k] = "[MASKED]"
		} else {
			headers[k] = maskFn(v)
		}
	}

	reqBody := maskFn(r.RequestBody)
	respText := r.Text
	if len(respText) > 4000 {
		respText = respText[:4000] + "... [truncated]"
	}

	var tests []TestSummary
	for _, t := range r.Tests {
		tests = append(tests, TestSummary{
			Name:   t.Name,
			Passed: t.Passed,
			Detail: t.Detail,
		})
	}

	ref := r.Ref
	if ref == "" {
		ref = r.Name
	}

	return &Entry{
		ID:              generateID(),
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		Source:          source,
		Ref:             ref,
		Zone:            zoneName,
		Server:          serverName,
		Method:          r.Method,
		Url:             maskFn(r.Url),
		Headers:         headers,
		Body:            reqBody,
		Status:          r.Status,
		Reason:          r.Reason,
		ElapsedMs:       r.ElapsedMs,
		ResponseSize:    r.Size,
		ResponseHeaders: r.Headers,
		ResponseBody:    respText,
		Tests:           tests,
		Captures:        r.Captures,
		Error:           r.Error,
	}
}

func (s *Store) Append(entry *Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(s.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}

	s.opCount++
	if s.opCount%50 == 0 {
		_ = s.pruneLocked()
	}

	return nil
}

func (s *Store) List(limit int, statusFilter int, methodFilter string, refFilter string) ([]*Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.readAllLocked()
	if err != nil {
		return nil, err
	}

	// Filter and reverse so newest entries are first
	var matched []*Entry
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if statusFilter > 0 && e.Status != statusFilter {
			continue
		}
		if methodFilter != "" && !strings.EqualFold(e.Method, methodFilter) {
			continue
		}
		if refFilter != "" {
			if !strings.Contains(strings.ToLower(e.Ref), strings.ToLower(refFilter)) &&
				!strings.Contains(strings.ToLower(e.Url), strings.ToLower(refFilter)) {
				continue
			}
		}
		matched = append(matched, e)
		if limit > 0 && len(matched) >= limit {
			break
		}
	}

	return matched, nil
}

func (s *Store) Get(idOrIndex string) (*Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.readAllLocked()
	if err != nil {
		return nil, err
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("history is empty")
	}

	// Check if 1-based index (1 = most recent)
	if idx, err := strconv.Atoi(idOrIndex); err == nil && idx > 0 {
		realIdx := len(entries) - idx
		if realIdx >= 0 && realIdx < len(entries) {
			return entries[realIdx], nil
		}
		return nil, fmt.Errorf("history index %d out of range (1..%d)", idx, len(entries))
	}

	// Match by ID
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].ID == idOrIndex {
			return entries[i], nil
		}
	}

	return nil, fmt.Errorf("no history entry found matching '%s'", idOrIndex)
}

func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.Remove(s.filePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Store) Prune(maxEntries int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxEntries = maxEntries
	return s.pruneLocked()
}

func (s *Store) pruneLocked() error {
	entries, err := s.readAllLocked()
	if err != nil || len(entries) <= s.maxEntries {
		return err
	}

	keep := entries[len(entries)-s.maxEntries:]
	tmpFile := s.filePath + ".tmp"
	f, err := os.Create(tmpFile)
	if err != nil {
		return err
	}

	for _, e := range keep {
		data, err := json.Marshal(e)
		if err == nil {
			_, _ = f.Write(append(data, '\n'))
		}
	}
	f.Close()

	return os.Rename(tmpFile, s.filePath)
}

func (s *Store) readAllLocked() ([]*Entry, error) {
	f, err := os.Open(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []*Entry
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err == nil {
			entries = append(entries, &e)
		}
	}

	return entries, scanner.Err()
}
