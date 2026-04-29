package runner

import (
	"github.com/rashee/all-bench/config"
)

// Result holds normalized metrics from any runner.
type Result struct {
	Runner      string
	Model       string
	RequestRate int
	Concurrency int
	Metrics     Metrics
}

// Metrics are the common LLM benchmark stats every runner must produce.
type Metrics struct {
	RequestLatency Percentiles
	TTFT           Percentiles // Time to First Token
	ITL            Percentiles // Inter Token Latency
	OutputTokens   Percentiles
	ReqThroughput  float64 // requests/sec
}

type Percentiles struct {
	Avg float64
	Min float64
	Max float64
	P50 float64
	P99 float64
}

// Runner is the interface each tool adapter must implement.
type Runner interface {
	// Name returns the runner identifier (e.g. "aiperf", "vllm").
	Name() string
	// Available reports whether the tool is installed and on PATH.
	Available() bool
	// InstallHint returns a short install instruction shown when Available is false.
	InstallHint() string
	// HasNativeOutput returns true if the tool prints its own rich output.
	// When true, all-bench skips its own table renderer in table mode.
	HasNativeOutput() bool
	// Run executes the benchmark and returns one result per run/concurrency level.
	Run(cfg *config.Config) ([]*Result, error)
}
