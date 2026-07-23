package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/RashRAJ/all-bench/cmd"
)

var version = "dev"

func main() {
	cmd.SetVersion(resolveVersion())
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// resolveVersion prefers the version injected via -ldflags by the GoReleaser
// release build. When that's absent (e.g. `go install
// github.com/RashRAJ/all-bench@v0.2.0`), it falls back to the module version
// the Go toolchain embeds in the binary automatically.
func resolveVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}
