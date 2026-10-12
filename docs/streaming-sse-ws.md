# Real-Time & Streaming Testing (`hit sse` & `hit ws`)

Modern APIs increasingly rely on real-time streaming architectures—such as LLM token generation via Server-Sent Events (SSE) and bi-directional messaging with WebSockets. `hit` provides dedicated, native tools for streaming and real-time protocol testing with zero external dependencies.

---

## 📡 Server-Sent Events (SSE) Client (`hit sse`)

Connect to any SSE (`text/event-stream`) endpoint to monitor streaming events in real time, benchmark Time-To-First-Token (TTFT), and analyze inter-chunk latency.

### Basic Usage
```bash
# Stream events until server closes connection or timeout
hit sse https://api.example.com/v1/chat/completions/stream

# Pass headers (e.g. Bearer auth)
hit sse https://api.example.com/stream -H "Authorization: Bearer my-key"

# Stop after receiving N events
hit sse https://api.example.com/stream -n 10

# Set a hard timeout
hit sse https://api.example.com/stream -d 15s

# Filter by event name
hit sse https://api.example.com/events --event "user_updated"

# Send a POST payload to trigger LLM streaming
hit sse https://api.openai.com/v1/chat/completions \
  -m POST \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -j '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}], "stream": true}'
```

### Live Terminal Output
```text
📡 Connecting to SSE stream: https://api.example.com/stream
──────────────────────────────────────────────────
▶ Event [id: 1] (chunk) +42.5ms
    data: {"token": "The"}
▶ Event [id: 2] (chunk) +18.2ms
    data: {"token": " future"}
▶ Event [id: 3] (chunk) +15.1ms
    data: {"token": " of"}

──────────────────────────────────────────────────
Stream Metrics & Scorecard:
  HTTP Status:         200 OK
  TTFT (First Token):  42.5 ms
  Events Received:     3
  Avg Event Latency:   25.3 ms
  Total Duration:      0.08 s
  Total Bytes:         142 B
```

### Automation & JSON Mode (`--json`)
Export complete stream session metrics directly as machine-readable JSON (including millisecond float fields `ttft_ms`, `total_duration_ms`, `avg_delta_ms`, and `close_reason`):
```bash
hit sse http://127.0.0.1:8765/sse -n 5 --json | jq .ttft_ms
```

---

## 🔌 RFC 6455 WebSocket Client (`hit ws`)

`hit` includes a built-in pure Go standard library implementation of the RFC 6455 WebSocket protocol. Test duplex connections, handshake latency, message pipelines, and automated response assertions without needing third-party packages. The built-in mock server (`hit mock 8765 &`) also provides an offline WebSocket echo server at `ws://127.0.0.1:8765/ws` and an SSE stream at `http://127.0.0.1:8765/sse`.

### Basic Usage
```bash
# Connect to the local mock WebSocket echo endpoint (default 5s timeout)
hit ws ws://127.0.0.1:8765/ws

# Send a single message upon connection
hit ws ws://127.0.0.1:8765/ws -m "ping"

# Send multiple sequential messages
hit ws ws://127.0.0.1:8765/ws -m "hello" -m "world"

# Add custom authentication headers
hit ws wss://api.example.com/ws -H "Sec-WebSocket-Protocol: v1.json"

# Stop after receiving N messages
hit ws ws://127.0.0.1:8765/ws -m "test" -n 1
```

### Automated Message Assertions (`--expect`)
Verify that the WebSocket server sends back a message containing an expected substring or regular expression pattern. If the pattern is not matched before timeout, `hit` exits with exit code `1`:

```bash
# Assert that server echoes back the greeting
hit ws ws://127.0.0.1:8765/ws -m "greeting_payload" --expect "greeting_payload"

# Regex pattern assertion
hit ws wss://api.example.com/ticker --expect '\"price\":\s*\d+'
```

### Terminal Output
```text
🔌 Connecting to WebSocket: ws://127.0.0.1:8765/ws
  → SENT: greeting_payload
  ← RECV: greeting_payload (+35ms)

──────────────────────────────────────────────────
WebSocket Summary:
  Handshake Latency: 28.4ms
  Messages Sent:     1
  Messages Received: 1
  Session Duration:  35.2ms
  Expectation ("greeting_payload"): ✓ PASS
```

### CI / Scripting JSON Export (`--json`)
Pipe structured session diagnostics to downstream tools:
```bash
hit ws ws://127.0.0.1:8765/ws -m "ping" -n 1 --json
```
Output:
```json
{
  "url": "ws://127.0.0.1:8765/ws",
  "connected": true,
  "handshake_latency": 28400000,
  "handshake_latency_ms": 28.4,
  "total_duration": 35200000,
  "total_duration_ms": 35.2,
  "messages_sent": 1,
  "messages_received": 1,
  "messages": [
    {
      "direction": "sent",
      "type": "text",
      "data": "ping",
      "timestamp": 0,
      "timestamp_ms": 0
    },
    {
      "direction": "received",
      "type": "text",
      "data": "ping",
      "timestamp": 35200000,
      "timestamp_ms": 35.2
    }
  ],
  "matched_expect": true
}
```
