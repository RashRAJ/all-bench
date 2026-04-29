package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/RashRAJ/all-bench/runner"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List available runners and their install status",
	Run: func(cmd *cobra.Command, args []string) {
		all := []runner.Runner{
			&runner.AiPerf{},
			&runner.VLMBench{},
		}

		fmt.Println()
		fmt.Printf("  %-12s %-10s %s\n", "Runner", "Status", "Note")
		fmt.Printf("  %s\n", strings.Repeat("-", 60))

		for _, r := range all {
			if r.Available() {
				fmt.Printf("  %-12s %-10s\n", r.Name(), "installed")
			} else {
				fmt.Printf("  %-12s %-10s %s\n", r.Name(), "missing", r.InstallHint())
			}
		}
		fmt.Println()
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}
