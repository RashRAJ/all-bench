package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rashee/all-bench/config"
)

type AiPerf struct{}

func (a *AiPerf) Name() string { return "aiperf" }

func (a *AiPerf) Available() bool {
	_, err := exec.LookPath("aiperf")
	return err == nil
}

func (a *AiPerf) InstallHint() string {
	return "pip install aiperf  (use a venv: python3 -m venv venv && source venv/bin/activate)"
}

func (a *AiPerf) HasNativeOutput() bool { return true }

func (a *AiPerf) Run(cfg *config.Config) ([]*Result, error) {
	if !a.Available() {
		return nil, fmt.Errorf("aiperf not found on PATH\n\nTo install:\n  %s", a.InstallHint())
	}

	args := buildAiPerfArgs(cfg)

	cmd := exec.Command("aiperf", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("aiperf exited: %w", err)
	}

	jsonPath, err := findAiPerfExportJSON(cfg)
	if err != nil {
		return nil, err
	}
	result, err := parseAiPerfExportJSON(jsonPath, cfg)
	if err != nil {
		return nil, err
	}
	return []*Result{result}, nil
}

func buildAiPerfArgs(cfg *config.Config) []string {
	ap := cfg.AiPerf
	args := []string{
		"profile",
		"--model", cfg.Defaults.Model,
		"--url", ap.URL,
		"--endpoint-type", ap.EndpointType,
		"--endpoint", ap.Endpoint,
		"--request-count", fmt.Sprintf("%d", ap.RequestCount),
	}

	if ap.Streaming {
		args = append(args, "--streaming")
	}

	// concurrency takes priority over request-rate if both set
	if ap.Concurrency > 0 {
		args = append(args, "--concurrency", fmt.Sprintf("%d", ap.Concurrency))
	} else if ap.RequestRate > 0 {
		args = append(args, "--request-rate", fmt.Sprintf("%d", ap.RequestRate))
	}

	return args
}

// findAiPerfExportJSON globs artifacts/ for the newest directory matching the model.
func findAiPerfExportJSON(cfg *config.Config) (string, error) {
	modelSanitized := strings.ReplaceAll(cfg.Defaults.Model, "/", "_")

	entries, err := os.ReadDir("artifacts")
	if err != nil {
		return "", fmt.Errorf("reading artifacts/: %w", err)
	}

	var newest os.DirEntry
	var newestInfo os.FileInfo
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), modelSanitized) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if newestInfo == nil || info.ModTime().After(newestInfo.ModTime()) {
			newest = e
			newestInfo = info
		}
	}

	if newest == nil {
		return "", fmt.Errorf("no artifacts directory found for model %q", cfg.Defaults.Model)
	}
	return filepath.Join("artifacts", newest.Name(), "profile_export_aiperf.json"), nil
}

// --- JSON parsing ---

type aiPerfExport struct {
	RequestLatency    *aiPerfMetric `json:"request_latency"`
	TimeToFirstToken  *aiPerfMetric `json:"time_to_first_token"`
	InterTokenLatency *aiPerfMetric `json:"inter_token_latency"`
	OutputTokenCount  *aiPerfMetric `json:"output_token_count"`
	RequestThroughput *aiPerfMetric `json:"request_throughput"`
}

type aiPerfMetric struct {
	Unit string  `json:"unit"`
	Avg  float64 `json:"avg"`
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	P50  float64 `json:"p50"`
	P99  float64 `json:"p99"`
}

func parseAiPerfExportJSON(path string, cfg *config.Config) (*Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading aiperf export %s: %w", path, err)
	}

	var export aiPerfExport
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, fmt.Errorf("parsing aiperf export: %w", err)
	}

	result := &Result{
		Runner:      "aiperf",
		Model:       cfg.Defaults.Model,
		RequestRate: cfg.AiPerf.RequestRate,
		Concurrency: cfg.AiPerf.Concurrency,
	}

	if m := export.RequestLatency; m != nil {
		result.Metrics.RequestLatency = Percentiles{Avg: m.Avg, Min: m.Min, Max: m.Max, P50: m.P50, P99: m.P99}
	}
	if m := export.TimeToFirstToken; m != nil {
		result.Metrics.TTFT = Percentiles{Avg: m.Avg, Min: m.Min, Max: m.Max, P50: m.P50, P99: m.P99}
	}
	if m := export.InterTokenLatency; m != nil {
		result.Metrics.ITL = Percentiles{Avg: m.Avg, Min: m.Min, Max: m.Max, P50: m.P50, P99: m.P99}
	}
	if m := export.OutputTokenCount; m != nil {
		result.Metrics.OutputTokens = Percentiles{Avg: m.Avg, Min: m.Min, Max: m.Max, P50: m.P50, P99: m.P99}
	}
	if m := export.RequestThroughput; m != nil {
		result.Metrics.ReqThroughput = m.Avg
	}

	return result, nil
}
