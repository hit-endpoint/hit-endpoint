package perf

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"hit/internal/runner"
	"hit/internal/spec"
	"hit/internal/types"
)

type PerfOptions struct {
	Concurrency int
	Total       int
	Duration    float64 // seconds
	RPS         float64
	Warmup      int
	Check       bool
	Progress    func(completed int, elapsedSec float64)
	CsvPath     string
	RampUp      float64 // seconds
}

type csvRow struct {
	offset  float64
	status  string
	latency float64
	err     string
}

func RunPerf(session *runner.Session, sp *spec.RequestSpec, opts PerfOptions) (*types.PerfReport, error) {
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 10
	}
	total := opts.Total
	if total <= 0 && opts.Duration <= 0 {
		total = 100
	}

	rendered, prepared, _, err := session.Prepare(sp, nil)
	if err != nil {
		return nil, err
	}

	report := &types.PerfReport{
		Name:         rendered.Name,
		Method:       prepared.Method,
		Url:          prepared.FullURL(),
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

	oneCall := func(record bool) {
		t0 := time.Now()
		r := session.RunSpec(sp, nil)
		ms := float64(time.Since(t0).Nanoseconds()) / 1e6
		if !record {
			return
		}

		offset := float64(time.Since(start).Nanoseconds()) / 1e9
		good := r.HasStatus && r.Status < 400 && r.Error == ""

		mu.Lock()
		report.Completed++
		atomic.AddInt64(&completed, 1)
		report.LatenciesMs = append(report.LatenciesMs, ms)
		report.BytesReceived += r.Size

		if r.Error != "" {
			report.Errors[r.Error]++
			good = false
		} else if r.HasStatus {
			report.StatusCounts[r.Status]++
		}

		if opts.Check && len(rendered.Tests) > 0 {
			for _, t := range r.Tests {
				if !t.Passed {
					good = false
					report.TestFailures[t.Name]++
				}
			}
		}

		if good {
			report.OK++
		} else {
			report.Failed++
		}

		statusStr := ""
		if r.HasStatus {
			statusStr = fmt.Sprintf("%d", r.Status)
		}
		csvRows = append(csvRows, csvRow{
			offset:  offset,
			status:  statusStr,
			latency: ms,
			err:     r.Error,
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
				oneCall(false)
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

				oneCall(true)
			}
		}(workerID)
	}

	wg.Wait()
	report.DurationS = time.Since(start).Seconds()

	if opts.CsvPath != "" {
		f, err := os.Create(opts.CsvPath)
		if err == nil {
			defer f.Close()
			w := csv.NewWriter(f)
			_ = w.Write([]string{"offset_s", "status", "latency_ms", "error"})
			for _, r := range csvRows {
				_ = w.Write([]string{
					fmt.Sprintf("%.4f", r.offset),
					r.status,
					fmt.Sprintf("%.2f", r.latency),
					r.err,
				})
			}
			w.Flush()
		}
	}

	return report, nil
}
