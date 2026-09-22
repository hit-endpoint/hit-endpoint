package report

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hit-endpoint/hit-endpoint/internal/types"
)

type HTMLReportStats struct {
	Total      int
	Passed     int
	Failed     int
	DurationMs float64
	P50Ms      float64
	P95Ms      float64
}

func calculateStats(results []*types.Result) HTMLReportStats {
	stats := HTMLReportStats{
		Total: len(results),
	}

	var latencies []float64
	for _, r := range results {
		if r.OK() {
			stats.Passed++
		} else {
			stats.Failed++
		}
		stats.DurationMs += r.ElapsedMs
		latencies = append(latencies, r.ElapsedMs)
	}

	if len(latencies) > 0 {
		sort.Float64s(latencies)
		stats.P50Ms = latencies[len(latencies)*50/100]
		stats.P95Ms = latencies[len(latencies)*95/100]
	}

	return stats
}

func GenerateHTMLReport(results []*types.Result, title string) string {
	if title == "" {
		title = "hit API Test Report"
	}
	stats := calculateStats(results)
	passRate := 0.0
	if stats.Total > 0 {
		passRate = float64(stats.Passed) / float64(stats.Total) * 100
	}

	var rows strings.Builder
	for i, r := range results {
		ref := r.Ref
		if ref == "" {
			ref = r.Name
		}
		if ref == "" {
			ref = r.Url
		}

		statusClass := "badge-success"
		statusText := fmt.Sprintf("%d %s", r.Status, r.Reason)
		if !r.HasStatus {
			statusClass = "badge-danger"
			statusText = "NO_RESP"
		} else if r.Status >= 500 {
			statusClass = "badge-danger"
		} else if r.Status >= 400 {
			statusClass = "badge-warning"
		} else if r.Status >= 300 {
			statusClass = "badge-info"
		}

		outcomeClass := "outcome-pass"
		outcomeBadge := "PASS"
		if !r.OK() {
			outcomeClass = "outcome-fail"
			outcomeBadge = "FAIL"
		}

		passedTests := 0
		for _, t := range r.Tests {
			if t.Passed {
				passedTests++
			}
		}
		testSummary := fmt.Sprintf("%d/%d", passedTests, len(r.Tests))
		if len(r.Tests) == 0 {
			testSummary = "-"
		}

		// Body preview formatting
		formattedBody := r.Text
		var parsedJSON any
		if json.Unmarshal([]byte(r.Text), &parsedJSON) == nil {
			if b, err := json.MarshalIndent(parsedJSON, "", "  "); err == nil {
				formattedBody = string(b)
			}
		}

		// Assertions markup
		var testMarkup strings.Builder
		if len(r.Tests) > 0 {
			testMarkup.WriteString(`<div class="section-title">Assertions</div><ul class="test-list">`)
			for _, t := range r.Tests {
				icon := `<span class="icon-pass">✓</span>`
				detail := ""
				if !t.Passed {
					icon = `<span class="icon-fail">✗</span>`
					if t.Detail != "" {
						detail = fmt.Sprintf(`<span class="test-detail">%s</span>`, html.EscapeString(t.Detail))
					}
				}
				testMarkup.WriteString(fmt.Sprintf(`<li>%s <span class="test-name">%s</span> %s</li>`, icon, html.EscapeString(t.Name), detail))
			}
			testMarkup.WriteString(`</ul>`)
		}

		// Headers markup
		var reqHeadersMarkup strings.Builder
		if len(r.RequestHeaders) > 0 {
			reqHeadersMarkup.WriteString(`<div class="section-title">Request Headers</div><table class="kv-table">`)
			for k, v := range r.RequestHeaders {
				reqHeadersMarkup.WriteString(fmt.Sprintf(`<tr><th>%s</th><td>%s</td></tr>`, html.EscapeString(k), html.EscapeString(v)))
			}
			reqHeadersMarkup.WriteString(`</table>`)
		}

		var respHeadersMarkup strings.Builder
		if len(r.Headers) > 0 {
			respHeadersMarkup.WriteString(`<div class="section-title">Response Headers</div><table class="kv-table">`)
			for k, v := range r.Headers {
				respHeadersMarkup.WriteString(fmt.Sprintf(`<tr><th>%s</th><td>%s</td></tr>`, html.EscapeString(k), html.EscapeString(v)))
			}
			respHeadersMarkup.WriteString(`</table>`)
		}

		// Captures markup
		var capturesMarkup strings.Builder
		if len(r.Captures) > 0 {
			capturesMarkup.WriteString(`<div class="section-title">Captures</div><table class="kv-table">`)
			for k, v := range r.Captures {
				capturesMarkup.WriteString(fmt.Sprintf(`<tr><th>%s</th><td>%v</td></tr>`, html.EscapeString(k), html.EscapeString(fmt.Sprint(v))))
			}
			capturesMarkup.WriteString(`</table>`)
		}

		errorMarkup := ""
		if r.Error != "" {
			errorMarkup = fmt.Sprintf(`<div class="error-box"><strong>Error:</strong> %s</div>`, html.EscapeString(r.Error))
		}

		reqBodyMarkup := ""
		if r.RequestBody != "" {
			reqBodyMarkup = fmt.Sprintf(`<div class="section-title">Request Body</div><pre class="code-block"><code>%s</code></pre>`, html.EscapeString(r.RequestBody))
		}

		respBodyMarkup := ""
		if formattedBody != "" {
			respBodyMarkup = fmt.Sprintf(`<div class="section-title">Response Body (%d bytes)</div><pre class="code-block"><code>%s</code></pre>`, r.Size, html.EscapeString(formattedBody))
		}

		row := fmt.Sprintf(`
<tr class="result-row %s" onclick="toggleDetails(%d)" data-status="%d" data-pass="%t" data-method="%s">
  <td class="col-outcome"><span class="badge %s">%s</span></td>
  <td class="col-method"><span class="method-tag method-%s">%s</span></td>
  <td class="col-status"><span class="badge %s">%s</span></td>
  <td class="col-ref"><strong>%s</strong></td>
  <td class="col-url">%s</td>
  <td class="col-latency">%.1f ms</td>
  <td class="col-tests">%s</td>
</tr>
<tr id="details-%d" class="details-row" style="display:none;">
  <td colspan="7">
    <div class="details-container">
      %s
      <div class="details-grid">
        <div>
          %s
          %s
        </div>
        <div>
          %s
          %s
        </div>
      </div>
      %s
      %s
    </div>
  </td>
</tr>`,
			outcomeClass, i, r.Status, r.OK(), r.Method,
			outcomeClass, outcomeBadge,
			strings.ToLower(r.Method), r.Method,
			statusClass, statusText,
			html.EscapeString(ref),
			html.EscapeString(r.Url),
			r.ElapsedMs,
			testSummary,
			i,
			errorMarkup,
			reqHeadersMarkup.String(), reqBodyMarkup,
			respHeadersMarkup.String(), respBodyMarkup,
			testMarkup.String(),
			capturesMarkup.String(),
		)
		rows.WriteString(row)
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>%s</title>
<style>
:root {
  --bg-primary: #0f172a;
  --bg-secondary: #1e293b;
  --bg-tertiary: #334155;
  --text-primary: #f8fafc;
  --text-secondary: #94a3b8;
  --border-color: #334155;
  --success: #10b981;
  --success-bg: rgba(16, 185, 129, 0.15);
  --danger: #ef4444;
  --danger-bg: rgba(239, 68, 68, 0.15);
  --warning: #f59e0b;
  --warning-bg: rgba(245, 158, 11, 0.15);
  --info: #3b82f6;
  --info-bg: rgba(59, 130, 246, 0.15);
  --font-mono: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}
body {
  margin: 0;
  padding: 24px;
  background-color: var(--bg-primary);
  color: var(--text-primary);
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
  line-height: 1.5;
}
.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-bottom: 1px solid var(--border-color);
  padding-bottom: 20px;
  margin-bottom: 24px;
}
.header h1 {
  margin: 0;
  font-size: 24px;
  font-weight: 700;
  display: flex;
  align-items: center;
  gap: 10px;
}
.timestamp {
  color: var(--text-secondary);
  font-size: 14px;
}
.stats-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 16px;
  margin-bottom: 24px;
}
.stat-card {
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  padding: 16px;
  text-align: center;
}
.stat-val {
  font-size: 28px;
  font-weight: 700;
  margin-top: 4px;
}
.stat-label {
  font-size: 12px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-secondary);
}
.filter-bar {
  display: flex;
  gap: 12px;
  align-items: center;
  margin-bottom: 16px;
  flex-wrap: wrap;
}
.search-input {
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  color: var(--text-primary);
  padding: 8px 14px;
  font-size: 14px;
  flex-grow: 1;
  max-width: 400px;
}
.filter-btn {
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  color: var(--text-secondary);
  padding: 8px 16px;
  font-size: 13px;
  cursor: pointer;
  transition: all 0.15s;
}
.filter-btn.active, .filter-btn:hover {
  background: var(--info-bg);
  border-color: var(--info);
  color: var(--info);
}
.table-container {
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  overflow: hidden;
}
table {
  width: 100%%;
  border-collapse: collapse;
  text-align: left;
}
th {
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-size: 12px;
  text-transform: uppercase;
  padding: 12px 16px;
}
td {
  padding: 12px 16px;
  border-bottom: 1px solid var(--border-color);
  font-size: 14px;
}
tr.result-row {
  cursor: pointer;
  transition: background 0.1s;
}
tr.result-row:hover {
  background: rgba(255, 255, 255, 0.04);
}
.badge {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 600;
  font-family: var(--font-mono);
}
.outcome-pass .badge { background: var(--success-bg); color: var(--success); }
.outcome-fail .badge { background: var(--danger-bg); color: var(--danger); }
.badge-success { background: var(--success-bg); color: var(--success); }
.badge-danger { background: var(--danger-bg); color: var(--danger); }
.badge-warning { background: var(--warning-bg); color: var(--warning); }
.badge-info { background: var(--info-bg); color: var(--info); }
.method-tag {
  font-family: var(--font-mono);
  font-weight: 700;
  font-size: 12px;
}
.method-get { color: #38bdf8; }
.method-post { color: #4ade80; }
.method-put { color: #facc15; }
.method-delete { color: #f87171; }
.method-patch { color: #c084fc; }
.col-url {
  font-family: var(--font-mono);
  color: var(--text-secondary);
  max-width: 320px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.details-row td {
  padding: 0;
  background: #090d16;
}
.details-container {
  padding: 20px;
  border-bottom: 2px solid var(--info);
}
.details-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 20px;
}
@media(max-width: 800px) {
  .details-grid { grid-template-columns: 1fr; }
}
.section-title {
  font-size: 13px;
  font-weight: 600;
  text-transform: uppercase;
  color: var(--text-secondary);
  margin-top: 16px;
  margin-bottom: 8px;
}
.kv-table {
  width: 100%%;
  border: 1px solid var(--border-color);
  border-radius: 4px;
  font-family: var(--font-mono);
  font-size: 12px;
}
.kv-table th, .kv-table td {
  padding: 6px 10px;
  border-bottom: 1px solid var(--border-color);
}
.kv-table th {
  width: 35%%;
  background: var(--bg-secondary);
  color: var(--text-secondary);
}
.code-block {
  background: var(--bg-primary);
  border: 1px solid var(--border-color);
  border-radius: 4px;
  padding: 12px;
  margin: 0;
  font-family: var(--font-mono);
  font-size: 12px;
  max-height: 240px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
}
.test-list {
  list-style: none;
  padding: 0;
  margin: 0;
}
.test-list li {
  padding: 6px 0;
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  font-size: 13px;
}
.icon-pass { color: var(--success); font-weight: bold; margin-right: 6px; }
.icon-fail { color: var(--danger); font-weight: bold; margin-right: 6px; }
.test-detail { color: var(--danger); font-family: var(--font-mono); font-size: 12px; margin-left: 8px; }
.error-box {
  background: var(--danger-bg);
  border: 1px solid var(--danger);
  color: #fca5a5;
  padding: 12px;
  border-radius: 6px;
  margin-bottom: 16px;
}
</style>
</head>
<body>

<div class="header">
  <h1>⚡ %s</h1>
  <div class="timestamp">Generated on %s</div>
</div>

<div class="stats-grid">
  <div class="stat-card">
    <div class="stat-label">Pass Rate</div>
    <div class="stat-val" style="color: %s;">%.1f%%</div>
  </div>
  <div class="stat-card">
    <div class="stat-label">Total Requests</div>
    <div class="stat-val">%d</div>
  </div>
  <div class="stat-card">
    <div class="stat-label">Passed</div>
    <div class="stat-val" style="color: var(--success);">%d</div>
  </div>
  <div class="stat-card">
    <div class="stat-label">Failed</div>
    <div class="stat-val" style="color: var(--danger);">%d</div>
  </div>
  <div class="stat-card">
    <div class="stat-label">Latency (P50 / P95)</div>
    <div class="stat-val" style="font-size: 22px;">%.0f / %.0f ms</div>
  </div>
</div>

<div class="filter-bar">
  <input type="text" id="searchBox" class="search-input" placeholder="Search by name, URL, or method..." oninput="filterResults()">
  <button class="filter-btn active" onclick="setFilter('all', this)">All (%d)</button>
  <button class="filter-btn" onclick="setFilter('pass', this)">Passed (%d)</button>
  <button class="filter-btn" onclick="setFilter('fail', this)">Failed (%d)</button>
  <button class="filter-btn" onclick="setFilter('2xx', this)">2xx</button>
  <button class="filter-btn" onclick="setFilter('4xx', this)">4xx</button>
  <button class="filter-btn" onclick="setFilter('5xx', this)">5xx</button>
</div>

<div class="table-container">
  <table>
    <thead>
      <tr>
        <th>Outcome</th>
        <th>Method</th>
        <th>Status</th>
        <th>Request</th>
        <th>URL</th>
        <th>Latency</th>
        <th>Assertions</th>
      </tr>
    </thead>
    <tbody id="resultsBody">
      %s
    </tbody>
  </table>
</div>

<script>
let currentFilter = 'all';

function toggleDetails(idx) {
  const row = document.getElementById('details-' + idx);
  if (row) {
    row.style.display = (row.style.display === 'none') ? 'table-row' : 'none';
  }
}

function setFilter(filter, btn) {
  currentFilter = filter;
  document.querySelectorAll('.filter-btn').forEach(b => b.classList.remove('active'));
  if (btn) btn.classList.add('active');
  filterResults();
}

function filterResults() {
  const q = document.getElementById('searchBox').value.toLowerCase();
  const rows = document.querySelectorAll('tr.result-row');

  rows.forEach((row, i) => {
    const text = row.innerText.toLowerCase();
    const isPass = row.getAttribute('data-pass') === 'true';
    const status = parseInt(row.getAttribute('data-status')) || 0;
    const detailsRow = document.getElementById('details-' + i);

    let matchFilter = true;
    if (currentFilter === 'pass') matchFilter = isPass;
    else if (currentFilter === 'fail') matchFilter = !isPass;
    else if (currentFilter === '2xx') matchFilter = (status >= 200 && status < 300);
    else if (currentFilter === '4xx') matchFilter = (status >= 400 && status < 500);
    else if (currentFilter === '5xx') matchFilter = (status >= 500 && status < 600);

    const matchSearch = text.includes(q);

    if (matchFilter && matchSearch) {
      row.style.display = '';
    } else {
      row.style.display = 'none';
      if (detailsRow) detailsRow.style.display = 'none';
    }
  });
}
</script>

</body>
</html>`,
		html.EscapeString(title),
		html.EscapeString(title),
		time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		func() string {
			if passRate == 100 {
				return "var(--success)"
			}
			return "var(--danger)"
		}(),
		passRate,
		stats.Total,
		stats.Passed,
		stats.Failed,
		stats.P50Ms,
		stats.P95Ms,
		stats.Total,
		stats.Passed,
		stats.Failed,
		rows.String(),
	)
}

func WriteHTMLReport(results []*types.Result, title, filePath string) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	content := GenerateHTMLReport(results, title)
	return os.WriteFile(filePath, []byte(content), 0644)
}
