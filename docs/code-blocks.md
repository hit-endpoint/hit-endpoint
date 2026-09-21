# Multi-Language Code Snippets (`hit snippet`)

`hit` can export any zone request or ad-hoc URL into complete, idiomatic, runnable code blocks in **Python**, **JavaScript**, **PHP**, and **Go**.

Every generated snippet includes:
* **HTTP execution**: Correct methods, headers, query strings, and payloads.
* **Timing & Metrics**: Start-to-finish response latency measurement.
* **Status & Headers**: Inspecting status codes and response headers.
* **Safe Property Access**: Extracting target elements from JSON bodies with language-specific null safety.

---

## Usage

```bash
# Generate Python (requests) snippet
hit snippet http://127.0.0.1:8765/pets --lang python

# Generate modern JavaScript (fetch) snippet with extracted property
hit snippet http://127.0.0.1:8765/pets -m POST -j '{"name":"Rex"}' --lang js --extract 'items[0].id'

# Generate PHP (cURL) snippet
hit snippet http://127.0.0.1:8765/pets --lang php --extract 'items[0].name'

# Generate Go (net/http) standalone runnable program
hit snippet http://127.0.0.1:8765/pets --lang go

# Generate all 4 languages simultaneously with labeled headers
hit snippet petstore/pets/list --all

# Export code snippet directly from a zone request file
hit show petstore/pets/list --lang python --extract 'items[0].id'
```

---

## Response Element Extraction (`--extract`)

When `--extract <PATH>` (or `-x`) is provided, `hit` automatically parses dot and bracket paths (e.g. `items[0].id` or `data.user.email`) and converts them into the idiomatic data access syntax for each language:

### 1. Python (`requests`)
```python
import requests

url = "http://127.0.0.1:8765/pets"
response = requests.get(url)

status_code = response.status_code
duration_ms = response.elapsed.total_seconds() * 1000
print(f"Status: {status_code} ({duration_ms:.1f}ms)")

try:
    data = response.json()
    extracted_value = data["items"][0]["id"]
    print(f"Extracted: {extracted_value}")
except Exception:
    print("Raw Response:", response.text)
```

### 2. JavaScript (Modern `fetch`)
```javascript
const url = "http://127.0.0.1:8765/pets";
const options = { method: "GET" };

const startTime = performance.now();
const response = await fetch(url, options);
const durationMs = performance.now() - startTime;

console.log(`Status: ${response.status} (${durationMs.toFixed(1)}ms)`);

try {
  const data = await response.json();
  const extractedValue = data.items[0].id;
  console.log('Extracted:', extractedValue);
} catch (err) {
  console.log('Raw Response:', await response.text());
}
```

### 3. PHP (`cURL`)
```php
<?php

$ch = curl_init("http://127.0.0.1:8765/pets");
curl_setopt_array($ch, [
    CURLOPT_RETURNTRANSFER => true,
    CURLOPT_CUSTOMREQUEST => "GET",
]);

$startTime = microtime(true);
$response = curl_exec($ch);
$durationMs = (microtime(true) - $startTime) * 1000;
$statusCode = curl_getinfo($ch, CURLINFO_HTTP_CODE);
curl_close($ch);

echo "Status: {$statusCode} (" . round($durationMs, 1) . "ms)\n";

$data = json_decode($response, true);
if (json_last_error() === JSON_ERROR_NONE) {
    $extractedValue = $data["items"][0]["id"] ?? null;
    echo "Extracted: " . json_encode($extractedValue) . "\n";
} else {
    echo "Raw Response: {$response}\n";
}
```

### 4. Go (`net/http`)
```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

func main() {
	url := "http://127.0.0.1:8765/pets"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(err)
	}

	client := &http.Client{}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)

	bodyBytes, _ := io.ReadAll(resp.Body)
	fmt.Printf("Status: %d (%v)\n", resp.StatusCode, elapsed)

	var data any
	if err := json.Unmarshal(bodyBytes, &data); err == nil {
		extractedValue := data.(map[string]any)["items"].([]any)[0].(map[string]any)["id"]
		fmt.Printf("Extracted: %v\n", extractedValue)
	}
}
```
