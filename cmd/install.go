package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/RashRAJ/all-bench/runner"
)

var installCmd = &cobra.Command{
	Use:   "install [runner...]",
	Short: "Install runner tools via pip",
	Long: `Install one or more runner tools (aiperf, vlmbench) with
"python3 -m pip install". With no arguments, installs every known runner
that isn't already on PATH.`,
	RunE: runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	registry := map[string]runner.Runner{
		"aiperf":   &runner.AiPerf{},
		"vlmbench": &runner.VLMBench{},
	}

	var targets []runner.Runner
	if len(args) == 0 {
		for _, r := range registry {
			if !r.Available() {
				targets = append(targets, r)
			}
		}
		if len(targets) == 0 {
			fmt.Println("all known runners are already installed")
			return nil
		}
	} else {
		for _, name := range args {
			r, ok := registry[name]
			if !ok {
				return fmt.Errorf("unknown runner %q (known: aiperf, vlmbench)", name)
			}
			targets = append(targets, r)
		}
	}

	if _, err := exec.LookPath("python3"); err != nil {
		return fmt.Errorf("python3 not found on PATH — install Python 3 first")
	}

	for _, r := range targets {
		pkg := r.PipPackage()
		fmt.Printf("Installing %s (pip package %q)...\n", r.Name(), pkg)

		pipCmd := exec.Command("python3", "-m", "pip", "install", "--upgrade", pkg)
		pipCmd.Stdout = os.Stdout
		pipCmd.Stderr = os.Stderr

		if err := pipCmd.Run(); err != nil {
			return fmt.Errorf("installing %s: %w", r.Name(), err)
		}
	}

	return nil
}
