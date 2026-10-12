package perf

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hit-endpoint/hit-endpoint/internal/flows"
	"github.com/hit-endpoint/hit-endpoint/internal/runner"
	"github.com/hit-endpoint/hit-endpoint/internal/types"
)

func RunFlowPerf(session *runner.Session, flowRef string, flowData map[string]any, opts PerfOptions) (*types.PerfReport, error) {
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 10
	}
	total := opts.Total
	if total <= 0 && opts.Duration <= 0 {
		total = 50
	}

	flowName := flowRef
	if n, ok := flowData["name"].(string); ok && n != "" {
		flowName = n
	}

	report := &types.PerfReport{
		Name:         flowName,
		Method:       "FLOW",
		Url:          flowRef,
		Concurrency:  concurrency,
		StatusCounts: make(map[int]int),
		Errors:       make(map[string]int),
		TestFailures: make(map[string]int),
	}

	var (
		mu        sync.Mutex
		csvRows   []csvRow
		started   int64
		completed int64
		start     = time.Now()
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if opts.Duration > 0 {
		time.AfterFunc(time.Duration(opts.Duration*float64(time.Second)), cancel)
	}

	oneFlowCall := func(record bool) {
		workerSess := session.Clone()
		t0 := time.Now()
		fr := flows.RunFlow(workerSess, flowData, nil, nil, opts.Check, 0)
		ms := float64(time.Since(t0).Nanoseconds()) / 1e6
		if !record {
			return
		}

		offset := float64(time.Since(start).Nanoseconds()) / 1e9
		good := fr.Error == ""

		mu.Lock()
		report.Completed++
		atomic.AddInt64(&completed, 1)
		report.LatenciesMs = append(report.LatenciesMs, ms)

		for _, r := range fr.Results {
			report.BytesReceived += r.Size
			if r.Error != "" {
				report.Errors[r.Error]++
				good = false
			} else if r.HasStatus {
				report.StatusCounts[r.Status]++
				if r.Status >= 400 && (!opts.Check || len(r.Tests) == 0) {
					good = false
				}
			}
			if opts.Check {
				for _, t := range r.FailedTests() {
					good = false
					report.TestFailures[t.Name]++
				}
			}
		}

		if fr.Error != "" {
			report.Errors[fr.Error]++
			good = false
		}

		if good {
			report.OK++
		} else {
			report.Failed++
		}

		statusStr := "OK"
		if !good {
			statusStr = "FAIL"
		}
		errStr := fr.Error
		if errStr == "" && !good {
			errStr = "flow step or assertion failed"
		}

		csvRows = append(csvRows, csvRow{
			offset:  offset,
			status:  statusStr,
			latency: ms,
			err:     errStr,
		})
		mu.Unlock()
	}

	// Warmup
	if opts.Warmup > 0 {
		var wg sync.WaitGroup
		for i := 0; i < opts.Warmup; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				oneFlowCall(false)
			}()
		}
		wg.Wait()
	}

	start = time.Now()

	// Monitor progress
	if opts.Progress != nil {
		go func() {
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					c := int(atomic.LoadInt64(&completed))
					el := time.Since(start).Seconds()
					opts.Progress(c, el)
				}
			}
		}()
	}

	interval := 0.0
	if opts.RPS > 0 {
		interval = float64(concurrency) / opts.RPS
	}

	var wg sync.WaitGroup
	for workerID := 0; workerID < concurrency; workerID++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if opts.RampUp > 0 && id > 0 {
				delaySec := (float64(id) / float64(concurrency)) * opts.RampUp
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Duration(delaySec * float64(time.Second))):
				}
			}

			nextSlot := time.Now()
			if interval > 0 {
				delaySec := (float64(id) / float64(concurrency)) * interval
				nextSlot = nextSlot.Add(time.Duration(delaySec * float64(time.Second)))
			}

			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				if total > 0 {
					n := atomic.AddInt64(&started, 1)
					if int(n) > total {
						return
					}
				}

				if interval > 0 {
					now := time.Now()
					if nextSlot.After(now) {
						time.Sleep(nextSlot.Sub(now))
					}
					nextSlot = nextSlot.Add(time.Duration(interval * float64(time.Second)))
				}

				oneFlowCall(true)
			}
		}(workerID)
	}

	wg.Wait()
	report.DurationS = time.Since(start).Seconds()

	if opts.CsvPath != "" {
		if err := writeCSVFile(opts.CsvPath, csvRows); err != nil {
			return report, err
		}
	}

	return report, nil
}
