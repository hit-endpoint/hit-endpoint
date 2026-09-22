package report

import (
	"github.com/hit-endpoint/hit-endpoint/internal/output"
	"github.com/hit-endpoint/hit-endpoint/internal/types"
)

// WriteJUnitXML generates standard JUnit XML from request execution results and writes it to destPath.
func WriteJUnitXML(results []*types.Result, destPath string) error {
	return output.WriteJUnitXML(results, destPath)
}

// WritePerfJUnitXML generates standard JUnit XML from performance benchmarking reports and writes it to destPath.
func WritePerfJUnitXML(report *types.PerfReport, thresholdMet bool, thresholdErr string, destPath string) error {
	return output.WritePerfJUnitXML(report, thresholdMet, thresholdErr, destPath)
}
