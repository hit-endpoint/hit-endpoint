package stream

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

// WSMessage represents a WebSocket message sent or received.
type WSMessage struct {
	Direction string        `json:"direction"` // "sent" or "received"
	Type      string        `json:"type"`      // "text" or "binary"
	Data      string        `json:"data"`
	Timestamp time.Duration `json:"timestamp"`
}

// WSResult captures the aggregate outcome of a WebSocket interaction.
type WSResult struct {
	URL              string        `json:"url"`
	Connected        bool          `json:"connected"`
	HandshakeLatency time.Duration `json:"handshake_latency"`
	TotalDuration    time.Duration `json:"total_duration"`
	MessagesSent     int           `json:"messages_sent"`
	MessagesReceived int           `json:"messages_received"`
	Messages         []WSMessage   `json:"messages"`
	MatchedExpect    bool          `json:"matched_expect"`
}

// WSOptions configures WebSocket client connections and behavior.
type WSOptions struct {
	Headers      map[string]string
	Insecure     bool
	Timeout      time.Duration
	SendMessages []string
	SendInterval time.Duration
	Expect       string // substring or regex to expect in received messages
	MaxMessages  int
	OnMessage    func(msg WSMessage)
}

// WSConn encapsulates an active RFC 6455 WebSocket client connection.
type WSConn struct {
	conn      net.Conn
	reader    *bufio.Reader
	mu        sync.Mutex
	closed    bool
	startTime time.Time
}

// DialWS connects and performs the RFC 6455 handshake with a WebSocket server.
func DialWS(ctx context.Context, targetURL string, opts WSOptions) (*WSConn, time.Duration, error) {
	u, err := url.Parse(targetURL)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid URL: %w", err)
	}

	var isTLS bool
	var defaultPort string
	switch u.Scheme {
	case "ws":
		isTLS = false
		defaultPort = "80"
	case "wss":
		isTLS = true
		defaultPort = "443"
	case "http":
		u.Scheme = "ws"
		isTLS = false
		defaultPort = "80"
	case "https":
		u.Scheme = "wss"
		isTLS = true
		defaultPort = "443"
	default:
		return nil, 0, fmt.Errorf("unsupported scheme: %s (expected ws:// or wss://)", u.Scheme)
	}

	host := u.Host
	if !strings.Contains(host, ":") {
		host = net.JoinHostPort(host, defaultPort)
	}

	startHandshake := time.Now()

	var dialer net.Dialer
	var rawConn net.Conn
	if isTLS {
		rawConn, err = tls.DialWithDialer(&dialer, "tcp", host, &tls.Config{
			ServerName:         u.Hostname(),
			InsecureSkipVerify: opts.Insecure,
		})
	} else {
		rawConn, err = dialer.DialContext(ctx, "tcp", host)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("tcp dial failed: %w", err)
	}

	// Generate client key
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		rawConn.Close()
		return nil, 0, fmt.Errorf("failed to generate nonce: %w", err)
	}
	clientKey := base64.StdEncoding.EncodeToString(keyBytes)

	reqPath := u.RequestURI()
	if reqPath == "" {
		reqPath = "/"
	}

	reqBuf := fmt.Sprintf("GET %s HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\n"+
		"Sec-WebSocket-Version: 13\r\n", reqPath, u.Host, clientKey)

	for k, v := range opts.Headers {
		reqBuf += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	reqBuf += "\r\n"

	if _, err := rawConn.Write([]byte(reqBuf)); err != nil {
		rawConn.Close()
		return nil, 0, fmt.Errorf("failed to write handshake request: %w", err)
	}

	br := bufio.NewReader(rawConn)
	req, _ := http.NewRequest("GET", targetURL, nil)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		rawConn.Close()
		return nil, 0, fmt.Errorf("failed to read handshake response: %w", err)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		rawConn.Close()
		return nil, 0, fmt.Errorf("handshake failed with status %d: %s", resp.StatusCode, resp.Status)
	}

	expectedAccept := computeAcceptKey(clientKey)
	serverAccept := resp.Header.Get("Sec-WebSocket-Accept")
	if serverAccept != expectedAccept {
		rawConn.Close()
		return nil, 0, fmt.Errorf("Sec-WebSocket-Accept mismatch: expected %s, got %s", expectedAccept, serverAccept)
	}

	handshakeLatency := time.Since(startHandshake)

	return &WSConn{
		conn:      rawConn,
		reader:    br,
		startTime: time.Now(),
	}, handshakeLatency, nil
}

func computeAcceptKey(key string) string {
	h := sha1.New()
	h.Write([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// SendText sends a text frame to the WebSocket server with RFC 6455 client masking.
func (ws *WSConn) SendText(text string) error {
	return ws.sendFrame(opText, []byte(text))
}

// SendBinary sends a binary frame to the WebSocket server.
func (ws *WSConn) SendBinary(data []byte) error {
	return ws.sendFrame(opBinary, data)
}

// Close gracefully closes the WebSocket connection.
func (ws *WSConn) Close() error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.closed {
		return nil
	}
	ws.closed = true
	// Send close frame
	_ = ws.sendFrameLocked(opClose, []byte{})
	return ws.conn.Close()
}

func (ws *WSConn) sendFrame(opcode byte, payload []byte) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return ws.sendFrameLocked(opcode, payload)
}

func (ws *WSConn) sendFrameLocked(opcode byte, payload []byte) error {
	if ws.closed && opcode != opClose {
		return errors.New("connection closed")
	}

	var header []byte
	header = append(header, 0x80|opcode) // FIN + opcode

	payloadLen := len(payload)
	// Client frames MUST be masked (0x80)
	if payloadLen <= 125 {
		header = append(header, 0x80|byte(payloadLen))
	} else if payloadLen <= 65535 {
		header = append(header, 0x80|126)
		b := make([]byte, 2)
		binary.BigEndian.PutUint16(b, uint16(payloadLen))
		header = append(header, b...)
	} else {
		header = append(header, 0x80|127)
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, uint64(payloadLen))
		header = append(header, b...)
	}

	// 4 bytes masking key
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return fmt.Errorf("failed to generate mask: %w", err)
	}
	header = append(header, mask...)

	maskedPayload := make([]byte, payloadLen)
	for i := 0; i < payloadLen; i++ {
		maskedPayload[i] = payload[i] ^ mask[i%4]
	}

	frame := append(header, maskedPayload...)
	_, err := ws.conn.Write(frame)
	return err
}

// ReadMessage reads the next text or binary message from the WebSocket server, handling ping/pong and close frames automatically.
func (ws *WSConn) ReadMessage(timeout time.Duration) (*WSMessage, error) {
	if timeout > 0 {
		_ = ws.conn.SetReadDeadline(time.Now().Add(timeout))
	} else {
		_ = ws.conn.SetReadDeadline(time.Time{})
	}

	for {
		b0, err := ws.reader.ReadByte()
		if err != nil {
			return nil, err
		}
		b1, err := ws.reader.ReadByte()
		if err != nil {
			return nil, err
		}

		opcode := b0 & 0x0F
		masked := (b1 & 0x80) != 0
		lenByte := b1 & 0x7F

		var payloadLen int64
		if lenByte <= 125 {
			payloadLen = int64(lenByte)
		} else if lenByte == 126 {
			b := make([]byte, 2)
			if _, err := io.ReadFull(ws.reader, b); err != nil {
				return nil, err
			}
			payloadLen = int64(binary.BigEndian.Uint16(b))
		} else if lenByte == 127 {
			b := make([]byte, 8)
			if _, err := io.ReadFull(ws.reader, b); err != nil {
				return nil, err
			}
			payloadLen = int64(binary.BigEndian.Uint64(b))
		}

		var maskKey []byte
		if masked {
			maskKey = make([]byte, 4)
			if _, err := io.ReadFull(ws.reader, maskKey); err != nil {
				return nil, err
			}
		}

		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(ws.reader, payload); err != nil {
			return nil, err
		}

		if masked {
			for i := int64(0); i < payloadLen; i++ {
				payload[i] ^= maskKey[i%4]
			}
		}

		switch opcode {
		case opPing:
			// Automatically send Pong
			_ = ws.sendFrame(opPong, payload)
			continue
		case opPong:
			continue
		case opClose:
			_ = ws.Close()
			return nil, io.EOF
		case opText:
			return &WSMessage{
				Direction: "received",
				Type:      "text",
				Data:      string(payload),
				Timestamp: time.Since(ws.startTime),
			}, nil
		case opBinary:
			return &WSMessage{
				Direction: "received",
				Type:      "binary",
				Data:      string(payload),
				Timestamp: time.Since(ws.startTime),
			}, nil
		case opContinuation:
			// Return as text for now
			return &WSMessage{
				Direction: "received",
				Type:      "continuation",
				Data:      string(payload),
				Timestamp: time.Since(ws.startTime),
			}, nil
		default:
			// Unrecognized opcode
			continue
		}
	}
}

// RunWS executes a full WebSocket test session: connects, sends queued messages, collects received responses, and checks expectations.
func RunWS(ctx context.Context, targetURL string, opts WSOptions) (*WSResult, error) {
	startTime := time.Now()
	conn, handshakeLatency, err := DialWS(ctx, targetURL, opts)
	if err != nil {
		return &WSResult{
			URL:       targetURL,
			Connected: false,
		}, err
	}
	defer conn.Close()

	res := &WSResult{
		URL:              targetURL,
		Connected:        true,
		HandshakeLatency: handshakeLatency,
		Messages:         make([]WSMessage, 0),
	}

	var expectRegex *regexp.Regexp
	if opts.Expect != "" {
		r, err := regexp.Compile(opts.Expect)
		if err == nil {
			expectRegex = r
		}
	}

	checkExpect := func(msg string) {
		if opts.Expect == "" {
			return
		}
		if expectRegex != nil && expectRegex.MatchString(msg) {
			res.MatchedExpect = true
		} else if strings.Contains(msg, opts.Expect) {
			res.MatchedExpect = true
		}
	}

	// Sender goroutine
	if len(opts.SendMessages) > 0 {
		go func() {
			interval := opts.SendInterval
			if interval <= 0 {
				interval = 50 * time.Millisecond
			}
			for _, msg := range opts.SendMessages {
				time.Sleep(interval)
				if err := conn.SendText(msg); err != nil {
					return
				}
				sentMsg := WSMessage{
					Direction: "sent",
					Type:      "text",
					Data:      msg,
					Timestamp: time.Since(startTime),
				}
				res.muLock(func() {
					res.Messages = append(res.Messages, sentMsg)
					res.MessagesSent++
				})
				if opts.OnMessage != nil {
					opts.OnMessage(sentMsg)
				}
			}
		}()
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			break
		}
		remaining := time.Until(deadline)
		msg, err := conn.ReadMessage(remaining)
		if err != nil {
			if errors.Is(err, io.EOF) || strings.Contains(err.Error(), "closed") {
				break
			}
			// Read timeout or other error
			break
		}

		res.muLock(func() {
			res.Messages = append(res.Messages, *msg)
			res.MessagesReceived++
		})
		checkExpect(msg.Data)
		if opts.OnMessage != nil {
			opts.OnMessage(*msg)
		}

		if opts.MaxMessages > 0 && res.MessagesReceived >= opts.MaxMessages {
			break
		}
	}

	res.TotalDuration = time.Since(startTime)
	return res, nil
}

var resMu sync.Mutex

func (res *WSResult) muLock(f func()) {
	resMu.Lock()
	defer resMu.Unlock()
	f()
}
