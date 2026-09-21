package stream

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SSEEvent represents a single Server-Sent Event.
type SSEEvent struct {
	ID        string        `json:"id,omitempty"`
	Event     string        `json:"event,omitempty"`
	Data      string        `json:"data"`
	Retry     int           `json:"retry,omitempty"`
	Timestamp time.Duration `json:"timestamp"` // elapsed time since connection started
	Delta     time.Duration `json:"delta"`     // delta time since previous event
}

// SSEResult stores the aggregate result of an SSE stream session.
type SSEResult struct {
	URL           string        `json:"url"`
	StatusCode    int           `json:"status_code"`
	TTFT          time.Duration `json:"ttft"` // Time to first token / first event
	TotalDuration time.Duration `json:"total_duration"`
	EventCount    int           `json:"event_count"`
	TotalBytes    int64         `json:"total_bytes"`
	Events        []SSEEvent    `json:"events"`
	AvgDelta      time.Duration `json:"avg_delta"`
}

// SSEOptions specifies configuration for connecting and reading an SSE stream.
type SSEOptions struct {
	Method      string
	Headers     map[string]string
	Body        io.Reader
	Timeout     time.Duration
	MaxEvents   int
	FilterEvent string
	Insecure    bool
	OnEvent     func(event SSEEvent)
}

// StreamSSE connects to an SSE endpoint and reads events until completion, timeout, or MaxEvents.
func StreamSSE(ctx context.Context, targetURL string, opts SSEOptions) (*SSEResult, error) {
	method := opts.Method
	if method == "" {
		method = "GET"
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, opts.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")

	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: opts.Insecure,
		},
	}
	client := &http.Client{
		Transport: transport,
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	res := &SSEResult{
		URL:        targetURL,
		StatusCode: resp.StatusCode,
		Events:     make([]SSEEvent, 0),
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return res, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	reader := bufio.NewReader(resp.Body)
	var currentEvent SSEEvent
	var dataLines []string
	lastEventTime := start
	firstEvent := true

	readDone := make(chan error, 1)

	go func() {
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				res.TotalBytes += int64(len(line))
				line = strings.TrimRight(line, "\r\n")

				if line == "" {
					// Empty line dispatches the buffered event if any data or fields accumulated
					if len(dataLines) > 0 || currentEvent.Event != "" || currentEvent.ID != "" {
						currentEvent.Data = strings.Join(dataLines, "\n")
						now := time.Now()
						currentEvent.Timestamp = now.Sub(start)
						currentEvent.Delta = now.Sub(lastEventTime)
						lastEventTime = now

						if firstEvent {
							res.TTFT = currentEvent.Timestamp
							firstEvent = false
						}

						if opts.FilterEvent == "" || opts.FilterEvent == currentEvent.Event {
							res.Events = append(res.Events, currentEvent)
							res.EventCount++
							if opts.OnEvent != nil {
								opts.OnEvent(currentEvent)
							}
							if opts.MaxEvents > 0 && res.EventCount >= opts.MaxEvents {
								readDone <- nil
								return
							}
						}

						// Reset for next event
						currentEvent = SSEEvent{}
						dataLines = nil
					}
				} else if strings.HasPrefix(line, ":") {
					// SSE comment, ignore
				} else {
					var field, value string
					colonIdx := strings.IndexByte(line, ':')
					if colonIdx >= 0 {
						field = line[:colonIdx]
						value = line[colonIdx+1:]
						if strings.HasPrefix(value, " ") {
							value = value[1:]
						}
					} else {
						field = line
						value = ""
					}

					switch field {
					case "event":
						currentEvent.Event = value
					case "data":
						dataLines = append(dataLines, value)
					case "id":
						currentEvent.ID = value
					case "retry":
						fmt.Sscanf(value, "%d", &currentEvent.Retry)
					}
				}
			}

			if err != nil {
				if err == io.EOF {
					// If EOF reached and we have pending data, dispatch it
					if len(dataLines) > 0 || currentEvent.Event != "" || currentEvent.ID != "" {
						currentEvent.Data = strings.Join(dataLines, "\n")
						now := time.Now()
						currentEvent.Timestamp = now.Sub(start)
						currentEvent.Delta = now.Sub(lastEventTime)
						if firstEvent {
							res.TTFT = currentEvent.Timestamp
						}
						if opts.FilterEvent == "" || opts.FilterEvent == currentEvent.Event {
							res.Events = append(res.Events, currentEvent)
							res.EventCount++
							if opts.OnEvent != nil {
								opts.OnEvent(currentEvent)
							}
						}
					}
					readDone <- nil
					return
				}
				readDone <- err
				return
			}
		}
	}()

	var readErr error
	if opts.Timeout > 0 {
		select {
		case <-ctx.Done():
			readErr = ctx.Err()
		case <-time.After(opts.Timeout):
			// Timeout reached, gracefully complete
			readErr = nil
		case err := <-readDone:
			readErr = err
		}
	} else {
		select {
		case <-ctx.Done():
			readErr = ctx.Err()
		case err := <-readDone:
			readErr = err
		}
	}

	res.TotalDuration = time.Since(start)
	if res.EventCount > 0 {
		var totalDelta time.Duration
		for _, ev := range res.Events {
			totalDelta += ev.Delta
		}
		res.AvgDelta = totalDelta / time.Duration(res.EventCount)
	}

	return res, readErr
}
