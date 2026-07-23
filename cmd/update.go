package cmd

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update all-bench to the latest version",
	Long: `Download and install the latest release of all-bench from GitHub.

Detects your OS and architecture automatically, fetches the latest
release from github.com/RashRAJ/all-bench, and replaces the current
binary in-place.`,
	RunE: runUpdate,
}

func init() {
	rootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, args []string) error {
	// ── Detect platform ────────────────────────────────────────────────────
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	// Map arch to GoReleaser naming
	arch := goarch
	if arch == "aarch64" {
		arch = "arm64"
	}

	// ── Fetch latest release info ──────────────────────────────────────────
	fmt.Println("  Checking for updates...")

	repo := "RashRAJ/all-bench"
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetching latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return fmt.Errorf("decoding release info: %w", err)
	}

	fmt.Printf("  Latest version: %s\n", release.TagName)

	// ── Find the right asset ───────────────────────────────────────────────
	// Asset name pattern: all-bench_<version>_<os>_<arch>.tar.gz
	// e.g. all-bench_0.2.0_darwin_arm64.tar.gz
	suffix := fmt.Sprintf("%s_%s.tar.gz", goos, arch)
	var assetURL string
	for _, a := range release.Assets {
		if strings.HasSuffix(a.Name, suffix) {
			assetURL = a.BrowserDownloadURL
			break
		}
	}
	if assetURL == "" {
		return fmt.Errorf("no release asset found for %s/%s", goos, arch)
	}

	// ── Download the archive ───────────────────────────────────────────────
	fmt.Println("  Downloading...")

	archiveResp, err := http.Get(assetURL)
	if err != nil {
		return fmt.Errorf("downloading archive: %w", err)
	}
	defer archiveResp.Body.Close()

	if archiveResp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %s", archiveResp.Status)
	}

	// ── Extract the binary from tar.gz ─────────────────────────────────────
	gzr, err := gzip.NewReader(archiveResp.Body)
	if err != nil {
		return fmt.Errorf("decompressing archive: %w", err)
	}
	defer gzr.Close()

	tarReader := tar.NewReader(gzr)

	var binaryContent []byte
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading archive: %w", err)
		}
		// The binary inside the archive is named "all-bench" (or "all-bench.exe" on Windows)
		if header.Typeflag == tar.TypeReg && (header.Name == "all-bench" || header.Name == "all-bench.exe") {
			binaryContent, err = io.ReadAll(tarReader)
			if err != nil {
				return fmt.Errorf("reading binary from archive: %w", err)
			}
			break
		}
	}
	if binaryContent == nil {
		return fmt.Errorf("could not find all-bench binary in archive")
	}

	// ── Find the current executable path ───────────────────────────────────
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("getting current executable path: %w", err)
	}

	// Resolve symlinks (common with `go install` placing binary in a symlinked dir)
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("resolving executable path: %w", err)
	}

	// ── Replace the binary ─────────────────────────────────────────────────
	// On Windows, we can't overwrite a running executable, so we write to a temp
	// file and rename. On Unix, we can write directly.
	tmpPath := execPath + ".tmp"
	if err := os.WriteFile(tmpPath, binaryContent, 0755); err != nil {
		return fmt.Errorf("writing new binary: %w", err)
	}

	if err := os.Rename(tmpPath, execPath); err != nil {
		// Rename might fail on some systems (e.g. Windows, or cross-device).
		// Fall back to copy + remove.
		if err := os.WriteFile(execPath, binaryContent, 0755); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("replacing binary: %w", err)
		}
		os.Remove(tmpPath)
	}

	fmt.Printf("  Updated to %s\n", release.TagName)

	// ── Verify the new version ─────────────────────────────────────────────
	verOut, _ := exec.Command(execPath, "--version").Output()
	if len(verOut) > 0 {
		fmt.Printf("  all-bench %s", strings.TrimSpace(string(verOut)))
	}

	return nil
}
