package output

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/rashee/all-bench/runner"
)

func Print(r *runner.Result, format string) {
	switch format {
	case "json":
		printJSON(r)
	default:
		printTable(r)
	}
}

func WriteFile(r *runner.Result, path string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func WriteFiles(results []*runner.Result, path string) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func printTable(r *runner.Result) {
	fmt.Printf("\n  all-bench | %s | %s\n\n", r.Runner, r.Model)

	header := fmt.Sprintf("  %-30s %8s %8s %8s %8s %8s", "Metric", "avg", "min", "max", "p50", "p99")
	divider := fmt.Sprintf("  %s", repeat("-", 74))

	fmt.Println(header)
	fmt.Println(divider)

	row(r.Metrics.RequestLatency, "Request Latency (ms)")
	row(r.Metrics.TTFT, "Time to First Token (ms)")
	row(r.Metrics.ITL, "Inter Token Latency (ms)")
	row(r.Metrics.OutputTokens, "Output Token Count")

	fmt.Println(divider)
	fmt.Printf("  %-30s %8.2f req/s\n\n", "Request Throughput", r.Metrics.ReqThroughput)
}

func row(p runner.Percentiles, label string) {
	fmt.Printf("  %-30s %8.2f %8.2f %8.2f %8.2f %8.2f\n",
		label, p.Avg, p.Min, p.Max, p.P50, p.P99)
}

func printJSON(r *runner.Result) {
	data, _ := json.MarshalIndent(r, "", "  ")
	fmt.Println(string(data))
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
