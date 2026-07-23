package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/RashRAJ/all-bench/config"
)

type VLMBench struct {
	lastRunAt time.Time
}

func (v *VLMBench) Name() string { return "vlmbench" }

func (v *VLMBench) Available() bool {
	_, err := exec.LookPath("vlmbench")
	return err == nil
}

func (v *VLMBench) InstallHint() string {
	return "pip install vlmbench  (use a venv: python3 -m venv venv && source venv/bin/activate)"
}

func (v *VLMBench) PipPackage() string { return "vlmbench" }

func (v *VLMBench) HasNativeOutput() bool { return true }

func (v *VLMBench) Run(cfg *config.Config) ([]*Result, error) {
	if !v.Available() {
		return nil, fmt.Errorf("vlmbench not found on PATH\n\nTo install:\n  %s\n  or run: all-bench install vlmbench", v.InstallHint())
	}

	args := buildVLMArgs(cfg)

	v.lastRunAt = time.Now()

	cmd := exec.Command("vlmbench", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("vlmbench exited: %w", err)
	}

	return findAndParseVLMResults(cfg, v.lastRunAt)
}

func buildVLMArgs(cfg *config.Config) []string {
	vlm := cfg.VLMBench
	args := []string{"run", "-m", cfg.Defaults.Model}

	if vlm.URL != "" {
		args = append(args, "--base-url", vlm.URL)
	}
	if vlm.Concurrency != "" {
		args = append(args, "--concurrency", vlm.Concurrency)
	}
	if cfg.Defaults.OutputTokens > 0 {
		args = append(args, "--max-tokens", fmt.Sprintf("%d", cfg.Defaults.OutputTokens))
	}
	if vlm.MaxSamples > 0 {
		args = append(args, "--max-samples", fmt.Sprintf("%d", vlm.MaxSamples))
	}
	if vlm.Runs > 0 {
		args = append(args, "--runs", fmt.Sprintf("%d", vlm.Runs))
	}
	if vlm.Backend != "" {
		args = append(args, "--backend", vlm.Backend)
	}
	if vlm.Prompt != "" {
		args = append(args, "--prompt", vlm.Prompt)
	}

	if vlm.Dataset != "" {
		args = append(args, "--dataset", vlm.Dataset)
		if vlm.DatasetTextCol != "" {
			args = append(args, "--dataset-text-col", vlm.DatasetTextCol)
		}
		if vlm.DatasetSplit != "" {
			args = append(args, "--dataset-split", vlm.DatasetSplit)
		}
	} else if vlm.Input != "" {
		args = append(args, "-i", vlm.Input)
	}

	return args
}

func (v *VLMBench) RawOutput(cfg *config.Config) ([]json.RawMessage, error) {
	if v.lastRunAt.IsZero() {
		return nil, fmt.Errorf("vlmbench has not been run yet")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("finding home dir: %w", err)
	}
	benchDir := filepath.Join(home, ".vlmbench", "benchmarks")
	entries, err := os.ReadDir(benchDir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", benchDir, err)
	}
	var outputs []json.RawMessage
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if !info.ModTime().After(v.lastRunAt) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(benchDir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading vlmbench export: %w", err)
		}
		outputs = append(outputs, json.RawMessage(data))
	}
	return outputs, nil
}

// findAndParseVLMResults collects all JSON files written to ~/.vlmbench/benchmarks/
// after startedAt — one per concurrency level in a sweep — and parses each one.
func findAndParseVLMResults(cfg *config.Config, startedAt time.Time) ([]*Result, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("finding home dir: %w", err)
	}
	benchDir := filepath.Join(home, ".vlmbench", "benchmarks")

	entries, err := os.ReadDir(benchDir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", benchDir, err)
	}

	var results []*Result
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if !info.ModTime().After(startedAt) {
			continue
		}
		r, err := parseVLMExportJSON(filepath.Join(benchDir, e.Name()), cfg)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no benchmark JSON written to %s after run", benchDir)
	}
	return results, nil
}

// --- JSON parsing ---

type vlmExport struct {
	Model   vlmModel   `json:"model"`
	Results vlmResults `json:"results"`
}

type vlmModel struct {
	ID string `json:"id"`
}

type vlmResults struct {
	TTFT         vlmStat `json:"ttft_ms"`
	TPOT         vlmStat `json:"tpot_ms"`
	ITL          vlmStat `json:"itl_ms"`
	E2EL         vlmStat `json:"e2el_ms"`
	TokensPerSec float64 `json:"tokens_per_sec"`
	InputsPerSec float64 `json:"inputs_per_sec"`
	Workers      int     `json:"workers"`
}

type vlmStat struct {
	Mean float64 `json:"mean"`
	P50  float64 `json:"p50"`
	P95  float64 `json:"p95"`
	P99  float64 `json:"p99"`
}

func parseVLMExportJSON(path string, cfg *config.Config) (*Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading vlmbench export %s: %w", path, err)
	}

	var export vlmExport
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, fmt.Errorf("parsing vlmbench export: %w", err)
	}

	toPercentiles := func(s vlmStat) Percentiles {
		return Percentiles{Avg: s.Mean, P50: s.P50, P99: s.P99}
	}

	return &Result{
		Runner:      "vlmbench",
		Model:       cfg.Defaults.Model,
		Concurrency: export.Results.Workers,
		Metrics: Metrics{
			TTFT:          toPercentiles(export.Results.TTFT),
			ITL:           toPercentiles(export.Results.ITL),
			RequestLatency: toPercentiles(export.Results.E2EL),
			ReqThroughput: export.Results.InputsPerSec,
		},
	}, nil
}
