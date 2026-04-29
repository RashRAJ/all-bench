package main

import (
	"fmt"
	"os"

	"github.com/rashee/all-bench/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
