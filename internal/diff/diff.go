package diff

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/hit-endpoint/hit-endpoint/internal/output"
)

type DiffType int

const (
	DiffEqual DiffType = iota
	DiffDelete
	DiffInsert
)

type DiffLine struct {
	Type DiffType `json:"type"`
	Text string   `json:"text"`
}

type Hunk struct {
	OldStart int        `json:"old_start"`
	OldLines int        `json:"old_lines"`
	NewStart int        `json:"new_start"`
	NewLines int        `json:"new_lines"`
	Lines    []DiffLine `json:"lines"`
}

type HeaderDiff struct {
	Name     string `json:"name"`
	OldValue string `json:"old_value,omitempty"`
	NewValue string `json:"new_value,omitempty"`
	Type     string `json:"type"` // "added", "removed", "changed"
}

type ResponseDiff struct {
	OldLabel       string       `json:"old_label"`
	NewLabel       string       `json:"new_label"`
	OldStatus      int          `json:"old_status"`
	OldReason      string       `json:"old_reason"`
	NewStatus      int          `json:"new_status"`
	NewReason      string       `json:"new_reason"`
	StatusChanged  bool         `json:"status_changed"`
	OldElapsedMs   float64      `json:"old_elapsed_ms"`
	NewElapsedMs   float64      `json:"new_elapsed_ms"`
	LatencyDeltaMs float64      `json:"latency_delta_ms"`
	OldSize        int64        `json:"old_size"`
	NewSize        int64        `json:"new_size"`
	SizeDelta      int64        `json:"size_delta"`
	HeaderDiffs    []HeaderDiff `json:"header_diffs,omitempty"`
	BodyIsJSON     bool         `json:"body_is_json"`
	Hunks          []Hunk       `json:"hunks,omitempty"`
	HasDifferences bool         `json:"has_differences"`
}

type Options struct {
	CompareHeaders bool
	BodyOnly       bool
	ContextLines   int
}

// CanonicalizeBody parses JSON and outputs pretty-printed canonical JSON.
// Returns the normalized text and true if valid JSON, or the original text and false otherwise.
func CanonicalizeBody(body string) (string, bool) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return "", false
	}
	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
		pretty, err := json.MarshalIndent(parsed, "", "  ")
		if err == nil {
			return string(pretty), true
		}
	}
	return body, false
}

// DiffLines computes line-by-line diff using Longest Common Subsequence (LCS).
func DiffLines(a, b []string) []DiffLine {
	n, m := len(a), len(b)
	// Build LCS matrix
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}

	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				if dp[i-1][j] >= dp[i][j-1] {
					dp[i][j] = dp[i-1][j]
				} else {
					dp[i][j] = dp[i][j-1]
				}
			}
		}
	}

	// Backtrack to form diff
	var res []DiffLine
	i, j := n, m
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && a[i-1] == b[j-1] {
			res = append(res, DiffLine{Type: DiffEqual, Text: a[i-1]})
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			res = append(res, DiffLine{Type: DiffInsert, Text: b[j-1]})
			j--
		} else if i > 0 && (j == 0 || dp[i][j-1] < dp[i-1][j]) {
			res = append(res, DiffLine{Type: DiffDelete, Text: a[i-1]})
			i--
		}
	}

	// Reverse
	for l, r := 0, len(res)-1; l < r; l, r = l+1, r-1 {
		res[l], res[r] = res[r], res[l]
	}
	return res
}

// GroupHunks groups diff lines into unified diff hunks with context lines.
func GroupHunks(diffs []DiffLine, contextLines int) []Hunk {
	if contextLines <= 0 {
		contextLines = 3
	}

	type linePos struct {
		oldLine int
		newLine int
	}

	positions := make([]linePos, len(diffs))
	oldNum, newNum := 1, 1
	var changeIndices []int

	for i, d := range diffs {
		positions[i] = linePos{oldLine: oldNum, newLine: newNum}
		switch d.Type {
		case DiffEqual:
			oldNum++
			newNum++
		case DiffDelete:
			changeIndices = append(changeIndices, i)
			oldNum++
		case DiffInsert:
			changeIndices = append(changeIndices, i)
			newNum++
		}
	}

	if len(changeIndices) == 0 {
		return nil
	}

	type hunkRange struct {
		startChange int
		endChange   int
	}

	var ranges []hunkRange
	curRange := hunkRange{startChange: changeIndices[0], endChange: changeIndices[0]}

	for k := 1; k < len(changeIndices); k++ {
		idx := changeIndices[k]
		numEqual := idx - curRange.endChange - 1
		if numEqual <= 2*contextLines {
			curRange.endChange = idx
		} else {
			ranges = append(ranges, curRange)
			curRange = hunkRange{startChange: idx, endChange: idx}
		}
	}
	ranges = append(ranges, curRange)

	var hunks []Hunk
	for _, r := range ranges {
		sliceStart := r.startChange - contextLines
		if sliceStart < 0 {
			sliceStart = 0
		}
		sliceEnd := r.endChange + contextLines + 1
		if sliceEnd > len(diffs) {
			sliceEnd = len(diffs)
		}

		oldStart := positions[sliceStart].oldLine
		newStart := positions[sliceStart].newLine
		oldLines := 0
		newLines := 0

		lines := make([]DiffLine, 0, sliceEnd-sliceStart)
		for j := sliceStart; j < sliceEnd; j++ {
			lines = append(lines, diffs[j])
			switch diffs[j].Type {
			case DiffEqual:
				oldLines++
				newLines++
			case DiffDelete:
				oldLines++
			case DiffInsert:
				newLines++
			}
		}

		if oldLines == 0 {
			oldStart = 0
		}
		if newLines == 0 {
			newStart = 0
		}

		hunks = append(hunks, Hunk{
			OldStart: oldStart,
			OldLines: oldLines,
			NewStart: newStart,
			NewLines: newLines,
			Lines:    lines,
		})
	}

	return hunks
}

// CompareHeaders finds additions, deletions, and modifications between headers.
func CompareHeaders(oldH, newH map[string]string) []HeaderDiff {
	var diffs []HeaderDiff
	displayNames := make(map[string]string)
	for k := range oldH {
		displayNames[strings.ToLower(k)] = k
	}
	for k := range newH {
		if _, exists := displayNames[strings.ToLower(k)]; !exists {
			displayNames[strings.ToLower(k)] = k
		}
	}

	var sortedLowerKeys []string
	for k := range displayNames {
		sortedLowerKeys = append(sortedLowerKeys, k)
	}
	sort.Strings(sortedLowerKeys)

	findVal := func(m map[string]string, key string) (string, bool) {
		for k, v := range m {
			if strings.EqualFold(k, key) {
				return v, true
			}
		}
		return "", false
	}

	for _, lowerKey := range sortedLowerKeys {
		disp := displayNames[lowerKey]
		oldV, hasOld := findVal(oldH, lowerKey)
		newV, hasNew := findVal(newH, lowerKey)

		if hasOld && !hasNew {
			diffs = append(diffs, HeaderDiff{Name: disp, OldValue: oldV, Type: "removed"})
		} else if !hasOld && hasNew {
			diffs = append(diffs, HeaderDiff{Name: disp, NewValue: newV, Type: "added"})
		} else if hasOld && hasNew && oldV != newV {
			diffs = append(diffs, HeaderDiff{Name: disp, OldValue: oldV, NewValue: newV, Type: "changed"})
		}
	}
	return diffs
}

// Compare computes differences between two responses.
func Compare(
	oldLabel, newLabel string,
	oldStatus int, oldReason string, oldElapsed float64, oldSize int64, oldHeaders map[string]string, oldBody string,
	newStatus int, newReason string, newElapsed float64, newSize int64, newHeaders map[string]string, newBody string,
	opts Options,
) *ResponseDiff {
	diff := &ResponseDiff{
		OldLabel:       oldLabel,
		NewLabel:       newLabel,
		OldStatus:      oldStatus,
		OldReason:      oldReason,
		NewStatus:      newStatus,
		NewReason:      newReason,
		StatusChanged:  oldStatus != newStatus,
		OldElapsedMs:   oldElapsed,
		NewElapsedMs:   newElapsed,
		LatencyDeltaMs: newElapsed - oldElapsed,
		OldSize:        oldSize,
		NewSize:        newSize,
		SizeDelta:      newSize - oldSize,
	}

	if diff.StatusChanged {
		diff.HasDifferences = true
	}

	// Compare Headers
	diff.HeaderDiffs = CompareHeaders(oldHeaders, newHeaders)
	if len(diff.HeaderDiffs) > 0 {
		diff.HasDifferences = true
	}

	// Canonicalize bodies
	canonOld, oldJSON := CanonicalizeBody(oldBody)
	canonNew, newJSON := CanonicalizeBody(newBody)
	diff.BodyIsJSON = oldJSON && newJSON

	var oldLines, newLines []string
	if canonOld != "" {
		oldLines = strings.Split(canonOld, "\n")
	}
	if canonNew != "" {
		newLines = strings.Split(canonNew, "\n")
	}

	rawDiffs := DiffLines(oldLines, newLines)
	diff.Hunks = GroupHunks(rawDiffs, opts.ContextLines)
	if len(diff.Hunks) > 0 {
		diff.HasDifferences = true
	}

	return diff
}

// FormatTerminal generates human-readable ANSI colored diff output.
func (d *ResponseDiff) FormatTerminal(p *output.Printer, opts Options) string {
	var sb strings.Builder

	if !opts.BodyOnly {
		sb.WriteString(p.Bold(fmt.Sprintf("Diff: %s  ←  %s\n", d.OldLabel, d.NewLabel)))

		// Status
		oldStatusStr := fmt.Sprintf("%d %s", d.OldStatus, d.OldReason)
		newStatusStr := fmt.Sprintf("%d %s", d.NewStatus, d.NewReason)
		if d.StatusChanged {
			sb.WriteString(fmt.Sprintf("  Status:   %s → %s\n", p.Red(oldStatusStr), p.Green(newStatusStr)))
		} else {
			sb.WriteString(fmt.Sprintf("  Status:   %s (unchanged)\n", p.Cyan(oldStatusStr)))
		}

		// Latency
		deltaSign := "+"
		if d.LatencyDeltaMs < 0 {
			deltaSign = ""
		}
		pctChange := 0.0
		if d.OldElapsedMs > 0 {
			pctChange = ((d.NewElapsedMs - d.OldElapsedMs) / d.OldElapsedMs) * 100.0
		}
		sb.WriteString(fmt.Sprintf("  Duration: %.1fms → %.1fms (%s%.1fms, %s%.1f%%)\n",
			d.OldElapsedMs, d.NewElapsedMs, deltaSign, d.LatencyDeltaMs, deltaSign, pctChange))

		// Size
		sizeSign := "+"
		if d.SizeDelta < 0 {
			sizeSign = ""
		}
		sb.WriteString(fmt.Sprintf("  Size:     %d B → %d B (%s%d B)\n",
			d.OldSize, d.NewSize, sizeSign, d.SizeDelta))

		// Headers
		if opts.CompareHeaders || len(d.HeaderDiffs) > 0 {
			if len(d.HeaderDiffs) == 0 {
				if opts.CompareHeaders {
					sb.WriteString(p.Dim("  Headers:  identical\n"))
				}
			} else {
				sb.WriteString(p.Bold("  Header Differences:\n"))
				for _, h := range d.HeaderDiffs {
					switch h.Type {
					case "added":
						sb.WriteString(p.Green(fmt.Sprintf("    + %s: %s\n", h.Name, h.NewValue)))
					case "removed":
						sb.WriteString(p.Red(fmt.Sprintf("    - %s: %s\n", h.Name, h.OldValue)))
					case "changed":
						sb.WriteString(p.Cyan(fmt.Sprintf("    ~ %s: %s → %s\n", h.Name, p.Red(h.OldValue), p.Green(h.NewValue))))
					}
				}
			}
		}
		sb.WriteString("\n")
	}

	// Body Hunks
	if len(d.Hunks) == 0 {
		if !d.HasDifferences {
			sb.WriteString(p.Green("✓ Responses are identical (no differences)\n"))
		} else {
			sb.WriteString(p.Dim("  (Response bodies are identical)\n"))
		}
		return sb.String()
	}

	bodyHeader := "Response Body Diff"
	if d.BodyIsJSON {
		bodyHeader += " (Canonical JSON)"
	}
	sb.WriteString(p.Bold(fmt.Sprintf("--- %s\n+++ %s\n", d.OldLabel, d.NewLabel)))
	sb.WriteString(p.Dim(fmt.Sprintf("=== %s ===\n", bodyHeader)))

	for _, hunk := range d.Hunks {
		sb.WriteString(p.Cyan(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", hunk.OldStart, hunk.OldLines, hunk.NewStart, hunk.NewLines)))
		for _, line := range hunk.Lines {
			switch line.Type {
			case DiffEqual:
				sb.WriteString(fmt.Sprintf("  %s\n", line.Text))
			case DiffDelete:
				sb.WriteString(p.Red(fmt.Sprintf("- %s\n", line.Text)))
			case DiffInsert:
				sb.WriteString(p.Green(fmt.Sprintf("+ %s\n", line.Text)))
			}
		}
	}

	return sb.String()
}
