package stream

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStreamSSE(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		// Event 1
		time.Sleep(10 * time.Millisecond)
		fmt.Fprintf(w, "event: greeting\ndata: hello mark\nid: 101\n\n")
		flusher.Flush()

		// Event 2 (multi-line data)
		time.Sleep(10 * time.Millisecond)
		fmt.Fprintf(w, "data: line 1\ndata: line 2\n\n")
		flusher.Flush()
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var receivedEvents []SSEEvent
	res, err := StreamSSE(ctx, ts.URL, SSEOptions{
		Timeout: 1 * time.Second,
		OnEvent: func(ev SSEEvent) {
			receivedEvents = append(receivedEvents, ev)
		},
	})
	if err != nil {
		t.Fatalf("StreamSSE error: %v", err)
	}

	if res.StatusCode != 200 {
		t.Errorf("expected 200, got %d", res.StatusCode)
	}
	if res.EventCount != 2 {
		t.Errorf("expected 2 events, got %d", res.EventCount)
	}
	if res.TTFT <= 0 {
		t.Errorf("expected positive TTFT, got %v", res.TTFT)
	}
	if len(receivedEvents) != 2 {
		t.Fatalf("expected 2 callback events, got %d", len(receivedEvents))
	}

	// Verify event 1
	if receivedEvents[0].Event != "greeting" || receivedEvents[0].Data != "hello mark" || receivedEvents[0].ID != "101" {
		t.Errorf("unexpected event 0: %+v", receivedEvents[0])
	}
	// Verify event 2
	if receivedEvents[1].Data != "line 1\nline 2" {
		t.Errorf("unexpected event 1 data: %q", receivedEvents[1].Data)
	}
}

func TestStreamSSEMaxEvents(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		for i := 1; i <= 10; i++ {
			fmt.Fprintf(w, "data: msg %d\n\n", i)
			flusher.Flush()
			time.Sleep(5 * time.Millisecond)
		}
	}))
	defer ts.Close()

	ctx := context.Background()
	res, err := StreamSSE(ctx, ts.URL, SSEOptions{
		MaxEvents: 3,
		Timeout:   1 * time.Second,
	})
	if err != nil {
		t.Fatalf("StreamSSE error: %v", err)
	}
	if res.EventCount != 3 {
		t.Errorf("expected exactly 3 events, got %d", res.EventCount)
	}
}

func TestWebSocketEcho(t *testing.T) {
	// Simple RFC 6455 echo server for testing
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.ToLower(r.Header.Get("Upgrade")) != "websocket" {
			http.Error(w, "not websocket", http.StatusBadRequest)
			return
		}

		key := r.Header.Get("Sec-WebSocket-Key")
		h := sha1.New()
		h.Write([]byte(key + wsGUID))
		accept := base64.StdEncoding.EncodeToString(h.Sum(nil))

		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijack not supported", http.StatusInternalServerError)
			return
		}
		conn, bufrw, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()

		// Send 101 Switching Protocols
		bufrw.WriteString("HTTP/1.1 101 Switching Protocols\r\n")
		bufrw.WriteString("Upgrade: websocket\r\n")
		bufrw.WriteString("Connection: Upgrade\r\n")
		bufrw.WriteString(fmt.Sprintf("Sec-WebSocket-Accept: %s\r\n\r\n", accept))
		bufrw.Flush()

		// Read 1 frame from client (masked) and echo back unmasked
		b0, err := bufrw.ReadByte()
		if err != nil {
			return
		}
		b1, err := bufrw.ReadByte()
		if err != nil {
			return
		}

		lenByte := int(b1 & 0x7F)
		maskKey := make([]byte, 4)
		if _, err := io.ReadFull(bufrw, maskKey); err != nil {
			return
		}

		payload := make([]byte, lenByte)
		if _, err := io.ReadFull(bufrw, payload); err != nil {
			return
		}
		for i := 0; i < lenByte; i++ {
			payload[i] ^= maskKey[i%4]
		}

		// Echo back unmasked server frame: 0x81 (FIN + text) + payloadLen + payload
		var respFrame []byte
		respFrame = append(respFrame, 0x81, byte(lenByte))
		respFrame = append(respFrame, payload...)
		_, _ = bufrw.Write(respFrame)
		bufrw.Flush()
		_ = b0
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	res, err := RunWS(ctx, wsURL, WSOptions{
		Timeout:      1 * time.Second,
		SendMessages: []string{"hello websocket"},
		Expect:       "hello websocket",
		MaxMessages:  1,
	})
	if err != nil {
		t.Fatalf("RunWS failed: %v", err)
	}

	if !res.Connected {
		t.Errorf("expected Connected == true")
	}
	if res.MessagesSent != 1 {
		t.Errorf("expected 1 sent message, got %d", res.MessagesSent)
	}
	if res.MessagesReceived != 1 {
		t.Errorf("expected 1 received message, got %d", res.MessagesReceived)
	}
	if !res.MatchedExpect {
		t.Errorf("expected MatchedExpect == true")
	}
	if len(res.Messages) > 1 && res.Messages[1].Data != "hello websocket" {
		t.Errorf("expected echo 'hello websocket', got %q", res.Messages[1].Data)
	}
}
