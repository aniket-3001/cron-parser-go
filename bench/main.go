// Command bench measures the Go port on the corpus the original library
// benchmarks itself with, reporting distributions rather than averages.
//
// A mean hides the behaviour that matters when a scheduler is deciding what to
// run next, so every figure here is a percentile taken from per-operation
// timings. The matching measurement of the original lives in bench/original.js,
// and both emit the same JSON shape so bench/report.js can put them side by
// side.
//
// Usage:
//
//	go run ./bench                       # parse and iterate, JSON to stdout
//	go run ./bench -mode=coldstart       # one occurrence, then exit
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sort"
	"time"

	_ "time/tzdata"

	"github.com/aniket-3001/cron-parser-go/cron"
)

// patterns are copied from the original's benchmarks/benchmark-inputs.ts, so
// neither side is measured on a corpus chosen to flatter it.
var patterns = []string{
	"* * * * * *",
	"0 15 */5 5 *",
	"10-30/2 2 12 8 0",
	"10 2 12 8 7",
	"0 12 */5 6 *",
	"0 * * 1,4-10,L * *",
	"0 0 0 * * 4,6L",
	"0 0 0 * * 1L,5L",
	"0 0 6-20/2,L 2 *",
	"0 H * * *",
	"0 H/3 * * *",
	"H H H(9-20)/3 1-11 *",
	"0 0 0 * * 5#3",
	"0 0 0 8 * 5#3",
	"0 0 0 15 * 5#3",
}

var start = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

type measurement struct {
	Pattern   string  `json:"pattern"`
	Samples   int     `json:"samples"`
	BatchSize int     `json:"batchSize"`
	MeanNs    float64 `json:"meanNs"`
	P50Ns     float64 `json:"p50Ns"`
	P90Ns     float64 `json:"p90Ns"`
	P99Ns     float64 `json:"p99Ns"`
	MaxNs     float64 `json:"maxNs"`
}

type report struct {
	Implementation string        `json:"implementation"`
	Runtime        string        `json:"runtime"`
	Workload       string        `json:"workload"`
	SampleUnit     string        `json:"sampleUnit"`
	Batches        int           `json:"batches"`
	Warmup         int           `json:"warmup"`
	Measurements   []measurement `json:"measurements"`
	MemoryBytes    uint64        `json:"memoryBytes"`
	MemoryMetric   string        `json:"memoryMetric"`
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(p * float64(len(sorted)-1))
	return sorted[i]
}

// targetBatch is how long one batch should take. The clock on this platform
// reports in steps of roughly half a millisecond, and a single operation costs
// far less than that, so timing operations individually yields mostly zeros.
// Batching until each measurement is well above the step removes the granularity
// from the result.
const targetBatch = 10 * time.Millisecond

// calibrate finds a batch size large enough to time reliably.
//
// It escalates well past the target before deciding, then derives the batch
// size from the measured cost of one operation. Stopping as soon as a batch
// first exceeds the target is not reliable: a single scheduling stall during
// calibration is enough to make a far too small batch look large enough, and
// every later sample from that batch size then reads as zero.
func calibrate(pattern string) int {
	const calibrationTarget = 5 * targetBatch

	n := 1
	var elapsed time.Duration
	for n < 1<<22 {
		t0 := time.Now()
		for i := 0; i < n; i++ {
			_ = parseAndIterate(pattern)
		}
		elapsed = time.Since(t0)
		if elapsed >= calibrationTarget {
			break
		}
		n *= 2
	}

	perOp := float64(elapsed) / float64(n)
	batch := int(float64(targetBatch)/perOp + 0.5)
	if batch < 1 {
		batch = 1
	}
	return batch
}

func summarise(pattern string, samples []float64) measurement {
	sort.Float64s(samples)
	var total float64
	for _, v := range samples {
		total += v
	}
	return measurement{
		Pattern: pattern,
		Samples: len(samples),
		MeanNs:  total / float64(len(samples)),
		P50Ns:   percentile(samples, 0.50),
		P90Ns:   percentile(samples, 0.90),
		P99Ns:   percentile(samples, 0.99),
		MaxNs:   samples[len(samples)-1],
	}
}

// parseAndIterate is one unit of work: parse an expression and take ten
// occurrences from it. It matches what the original's own harness times.
func parseAndIterate(pattern string) error {
	e, err := cron.Parse(pattern,
		cron.WithLocation(time.UTC), cron.WithHashSeed("bench"), cron.WithCurrent(start))
	if err != nil {
		return err
	}
	e.Take(10)
	return nil
}

func runThroughput(iterations, warmup int) report {
	out := report{
		Implementation: "go-port",
		Runtime:        runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH,
		Workload:       "parse an expression, then take 10 occurrences",
		SampleUnit:     "mean nanoseconds per operation across one batch",
		Batches:        iterations,
		Warmup:         warmup,
		MemoryMetric:   "go runtime Sys: total memory obtained from the OS",
	}

	for _, pattern := range patterns {
		// Warm up before measuring, so the first samples do not carry
		// one-time costs such as loading the zone database.
		for i := 0; i < warmup; i++ {
			if err := parseAndIterate(pattern); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", pattern, err)
				os.Exit(1)
			}
		}

		batch := calibrate(pattern)
		samples := make([]float64, 0, iterations)
		for i := 0; i < iterations; i++ {
			t0 := time.Now()
			for j := 0; j < batch; j++ {
				_ = parseAndIterate(pattern)
			}
			// One sample is the mean cost of an operation across the batch.
			samples = append(samples, float64(time.Since(t0).Nanoseconds())/float64(batch))
		}
		m := summarise(pattern, samples)
		m.BatchSize = batch
		out.Measurements = append(out.Measurements, m)
	}

	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	out.MemoryBytes = stats.Sys
	return out
}

func main() {
	mode := flag.String("mode", "throughput", "throughput or coldstart")
	iterations := flag.Int("batches", 200, "measured batches per pattern")
	warmup := flag.Int("warmup", 200, "unmeasured iterations before each pattern")
	flag.Parse()

	if *mode == "coldstart" {
		// The whole point is the work a process does before it can answer once,
		// so nothing is warmed and nothing is repeated. The caller times the
		// process, not this function.
		e, err := cron.Parse("*/15 9-17 * * 1-5",
			cron.WithLocation(time.UTC), cron.WithCurrent(start))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		next, err := e.Next()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(next.UTC().Format(time.RFC3339))
		return
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(runThroughput(*iterations, *warmup)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
