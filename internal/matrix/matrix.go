package matrix

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"hit/internal/spec"
	"hit/internal/types"
)

// Row represents one data record in a parameterized matrix.
type Row = map[string]any

// MatrixResult aggregates the execution metrics of a data-driven test matrix.
type MatrixResult struct {
	SpecName   string          `json:"spec_name"`
	TotalRows  int             `json:"total_rows"`
	PassedRows int             `json:"passed_rows"`
	FailedRows int             `json:"failed_rows"`
	RowResults []*types.Result `json:"row_results"`
	RowData    []Row           `json:"row_data"`
}

// HasFailures returns true if any row execution failed.
func (mr *MatrixResult) HasFailures() bool {
	return mr.FailedRows > 0
}

// LoadMatrix parses inline rows or external CSV/JSON files into a slice of rows.
func LoadMatrix(cfg any, baseDir string) ([]Row, error) {
	if cfg == nil {
		return nil, nil
	}

	switch v := cfg.(type) {
	case string:
		return loadMatrixFile(v, baseDir)

	case []any:
		return parseRowList(v)

	case []map[string]any:
		return v, nil

	case map[string]any:
		if rowsRaw, ok := v["rows"]; ok && rowsRaw != nil {
			if list, ok := rowsRaw.([]any); ok {
				return parseRowList(list)
			}
		}
		if fileRaw, ok := v["file"]; ok && fileRaw != nil {
			return loadMatrixFile(fmt.Sprintf("%v", fileRaw), baseDir)
		}
		return nil, fmt.Errorf("matrix map must contain 'rows' list or 'file' path")

	default:
		return nil, fmt.Errorf("unsupported matrix definition: %T", cfg)
	}
}

func parseRowList(list []any) ([]Row, error) {
	var rows []Row
	for i, item := range list {
		if m, ok := item.(map[string]any); ok {
			rows = append(rows, m)
		} else if mStr, ok := item.(map[any]any); ok {
			converted := make(Row)
			for k, val := range mStr {
				converted[fmt.Sprintf("%v", k)] = val
			}
			rows = append(rows, converted)
		} else {
			return nil, fmt.Errorf("matrix row %d is not an object: %T", i+1, item)
		}
	}
	return rows, nil
}

func loadMatrixFile(relOrAbsPath, baseDir string) ([]Row, error) {
	filePath := relOrAbsPath
	if !filepath.IsAbs(filePath) && baseDir != "" {
		filePath = filepath.Join(baseDir, filePath)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read matrix file %s: %w", filePath, err)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".csv" {
		return parseCSV(data)
	} else if ext == ".json" {
		return parseJSONMatrix(data)
	}

	// Try JSON first, then CSV
	if rows, err := parseJSONMatrix(data); err == nil {
		return rows, nil
	}
	return parseCSV(data)
}

func parseCSV(data []byte) ([]Row, error) {
	reader := csv.NewReader(strings.NewReader(string(data)))
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to parse CSV data: %w", err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("CSV file is empty")
	}

	headers := records[0]
	for i := range headers {
		headers[i] = strings.TrimSpace(headers[i])
	}

	var rows []Row
	for lineIdx, record := range records[1:] {
		if len(record) == 0 || (len(record) == 1 && strings.TrimSpace(record[0]) == "") {
			continue // skip empty lines
		}
		row := make(Row)
		for colIdx, header := range headers {
			val := ""
			if colIdx < len(record) {
				val = strings.TrimSpace(record[colIdx])
			}
			row[header] = coerce(val)
		}
		if len(row) > 0 {
			rows = append(rows, row)
		}
		_ = lineIdx
	}
	return rows, nil
}

func parseJSONMatrix(data []byte) ([]Row, error) {
	var list []map[string]any
	if err := json.Unmarshal(data, &list); err == nil {
		return list, nil
	}
	var wrapper struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.Unmarshal(data, &wrapper); err == nil && len(wrapper.Rows) > 0 {
		return wrapper.Rows, nil
	}
	return nil, fmt.Errorf("JSON matrix must be an array of objects or have a 'rows' array")
}

// ExpandSpec creates a parameterized variant of the base RequestSpec for a given matrix row.
func ExpandSpec(base *spec.RequestSpec, row Row, index int) *spec.RequestSpec {
	cloned := spec.CloneSpec(base)

	// Set row context
	cloned.Vars["row"] = row

	// Also promote row keys directly into vars for convenient {{key}} interpolation
	for k, v := range row {
		if _, exists := cloned.Vars[k]; !exists {
			cloned.Vars[k] = v
		}
	}

	// Format row description
	var descParts []string
	for k, v := range row {
		descParts = append(descParts, fmt.Sprintf("%s=%v", k, v))
		if len(descParts) >= 3 {
			break
		}
	}
	summary := strings.Join(descParts, ", ")
	if summary != "" {
		cloned.Name = fmt.Sprintf("%s [Row %d: %s]", base.Name, index+1, summary)
	} else {
		cloned.Name = fmt.Sprintf("%s [Row %d]", base.Name, index+1)
	}

	return cloned
}

func coerce(val string) any {
	if val == "true" {
		return true
	}
	if val == "false" {
		return false
	}
	if val == "null" {
		return nil
	}
	if num, err := strconv.Atoi(val); err == nil {
		return num
	}
	if f, err := strconv.ParseFloat(val, 64); err == nil && strings.Contains(val, ".") {
		return f
	}
	return val
}
