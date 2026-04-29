package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rashee/all-bench/config"
	"github.com/rashee/all-bench/output"
	"github.com/rashee/all-bench/runner"
)

var (
	cfgFile     string
	flagRunner  string
	flagFormat  string
	flagOutFile string
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a benchmark using the configured runner(s)",
	RunE:  runBench,
}

func init() {
	runCmd.Flags().StringVarP(&cfgFile, "config", "c", "all-bench.yaml", "config file")
	runCmd.Flags().StringVar(&flagRunner, "runner", "", "override runner (e.g. aiperf)")
	runCmd.Flags().StringVar(&flagFormat, "format", "", "output format: table|json")
	runCmd.Flags().StringVar(&flagOutFile, "out", "", "write results to file")
}

func runBench(cmd *cobra.Command, args []string) error {
	cfg, err := config.FromFile(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	applyOverrides(cfg)

	runners := resolveRunners(cfg.Runners)
	if len(runners) == 0 {
		return fmt.Errorf("no runners configured — set 'runners' in %s or use --runner", cfgFile)
	}

	format := cfg.Output.Format
	if flagFormat != "" {
		format = flagFormat
	}
	outFile := cfg.Output.File
	if flagOutFile != "" {
		outFile = flagOutFile
	}

	for _, r := range runners {
		fmt.Printf("Running %s...\n", r.Name())
		results, err := r.Run(cfg)
		if err != nil {
			return fmt.Errorf("%s failed: %w", r.Name(), err)
		}

		// only render our table if the runner doesn't produce its own rich output,
		// or if the user explicitly asked for json format
		if !r.HasNativeOutput() || format == "json" {
			for _, result := range results {
				output.Print(result, format)
			}
		}

		if outFile != "" {
			if err := output.WriteFiles(results, outFile); err != nil {
				return fmt.Errorf("writing output file: %w", err)
			}
			fmt.Printf("Results written to %s\n", outFile)
		}
	}

	return nil
}

func applyOverrides(cfg *config.Config) {
	if flagRunner != "" {
		cfg.Runners = []string{flagRunner}
	}
}

func resolveRunners(names []string) []runner.Runner {
	registry := map[string]runner.Runner{
		"aiperf":   &runner.AiPerf{},
		"vlmbench": &runner.VLMBench{},
	}

	var runners []runner.Runner
	for _, name := range names {
		if r, ok := registry[name]; ok {
			runners = append(runners, r)
		} else {
			fmt.Printf("warning: unknown runner %q, skipping\n", name)
		}
	}
	return runners
}
