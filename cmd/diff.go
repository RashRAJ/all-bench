package cmd

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/RashRAJ/all-bench/history"
)

const (
	ansiReset  = "\033[0m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
)

type headlineMetric struct {
	path  string
	label string
	unit  string
}

var headlineMetrics = []headlineMetric{
	{"time_to_first_token.avg", "Time to First Token", "ms"},
	{"time_to_second_token.avg", "Time to Second Token", "ms"},
	{"request_latency.avg", "Request Latency", "ms"},
	{"inter_token_latency.avg", "Inter Token Latency", "ms"},
	{"output_token_throughput_per_user.avg", "Output Throughput/User", "t/s/u"},
	{"output_token_throughput.avg", "Output Token Throughput", "t/s"},
	{"request_throughput.avg", "Request Throughput", "req/s"},
	{"output_sequence_length.avg", "Output Seq Length", "tokens"},
	{"benchmark_duration.avg", "Benchmark Duration", "s"},
}

// compactMetrics drives the rows in the compact columnar table.
var compactMetrics = []struct{ prefix, label string }{
	{"time_to_first_token", "Time to First Token (ms)"},
	{"time_to_second_token", "Time to Second Token (ms)"},
	{"request_latency", "Request Latency (ms)"},
	{"inter_token_latency", "Inter Token Latency (ms)"},
	{"inter_chunk_latency", "Inter Chunk Latency (ms)"},
	{"output_token_throughput_per_user", "Output Throughput/User (t/s/u)"},
	{"output_token_throughput", "Output Token Throughput (t/s)"},
	{"request_throughput", "Request Throughput (req/s)"},
	{"prefill_throughput_per_user", "Prefill Throughput/User"},
	{"output_sequence_length", "Output Seq Length (tokens)"},
	{"output_token_count", "Output Token Count"},
	{"input_sequence_length", "Input Seq Length (tokens)"},
	{"http_req_duration", "HTTP Duration (ms)"},
	{"http_req_waiting", "HTTP Waiting / TTFT (ms)"},
	{"http_req_receiving", "HTTP Receiving (ms)"},
	{"http_req_sending", "HTTP Sending (ms)"},
	{"http_req_connecting", "HTTP Connecting (ms)"},
	{"http_req_connection_overhead", "HTTP Conn Overhead (ms)"},
	{"http_req_dns_lookup", "HTTP DNS Lookup (ms)"},
	{"http_req_data_received", "HTTP Data Received (KB)"},
	{"http_req_data_sent", "HTTP Data Sent (KB)"},
	{"benchmark_duration", "Benchmark Duration (s)"},
	{"total_token_throughput", "Total Token Throughput"},
	{"total_osl", "Total Output Length"},
	{"total_output_tokens", "Total Output Tokens"},
	{"total_isl", "Total Input Length"},
}

// stat columns shown in the compact table
var statCols = []string{"avg", "p50", "p90", "p99", "min", "max"}

var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Compare the last two benchmark runs",
	RunE:  runDiff,
}

func init() {
	rootCmd.AddCommand(diffCmd)
}

func runDiff(cmd *cobra.Command, args []string) error {
	snaps, err := history.LoadLast(2)
	if err != nil {
		return fmt.Errorf("loading history: %w", err)
	}
	if len(snaps) < 2 {
		fmt.Println("  Not enough history — run all-bench run at least twice first.")
		return nil
	}

	a, b := snaps[0], snaps[1]

	tsA := a.Timestamp.Local().Format(time.DateTime)
	tsB := b.Timestamp.Local().Format(time.DateTime)

	fmt.Println()
	fmt.Println(ansiBold + ansiCyan + "  " + strings.Repeat("━", 60) + ansiReset)
	fmt.Println(ansiBold + "  Benchmark Diff" + ansiReset)
	fmt.Println(ansiDim + fmt.Sprintf("  %s  →  %s", tsA, tsB) + ansiReset)
	fmt.Println(ansiBold + ansiCyan + "  " + strings.Repeat("━", 60) + ansiReset)
	fmt.Println()

	cfgChanges := history.DiffConfig(a.Config, b.Config)
	if len(cfgChanges) > 0 {
		fmt.Println(ansiBold + "  Config Changes" + ansiReset)
		for _, ch := range cfgChanges {
			fmt.Printf("    %-40s %s%v%s  →  %s%v%s\n",
				ch[0],
				ansiYellow, ch[1], ansiReset,
				ansiCyan, ch[2], ansiReset)
		}
		fmt.Println()
	}

	seen := make(map[string]bool)
	var runnerNames []string
	for k := range a.RawOutputs {
		if !seen[k] {
			seen[k] = true
			runnerNames = append(runnerNames, k)
		}
	}
	for k := range b.RawOutputs {
		if !seen[k] {
			seen[k] = true
			runnerNames = append(runnerNames, k)
		}
	}
	sort.Strings(runnerNames)

	for _, rName := range runnerNames {
		aRaws := a.RawOutputs[rName]
		bRaws := b.RawOutputs[rName]

		if len(aRaws) == 0 || len(bRaws) == 0 {
			fmt.Printf("  %s: only present in one run, skipping\n\n", rName)
			continue
		}

		pairs := min(len(aRaws), len(bRaws))
		for i := range pairs {
			label := strings.ToUpper(rName)
			if pairs > 1 {
				label = fmt.Sprintf("%s  [level %d]", label, i+1)
			}

			rows := history.DiffRaw(aRaws[i], bRaws[i])
			if len(rows) == 0 {
				fmt.Printf("  %s: (no numeric fields)\n\n", label)
				continue
			}

			rowMap := make(map[string]history.DiffRow, len(rows))
			for _, r := range rows {
				rowMap[r.Path] = r
			}

			printHeadlineTable(label, rowMap)
			printCompactTable(rowMap)
		}
	}

	return nil
}

// ── Headline summary table (avg only, Run A | Run B | Change) ─────────────────

func printHeadlineTable(label string, rowMap map[string]history.DiffRow) {
	const mW, vW, cW = 38, 12, 10
	bl := makeBox(mW, vW, vW, cW)

	boxW := 1 + (mW + 2) + 1 + (vW+2)*2 + 1 + (cW + 2) + 1
	title := fmt.Sprintf(" %s | Key Metrics ", label)
	pad := max((boxW-len(title))/2, 0)
	fmt.Println(strings.Repeat(" ", pad+2) + ansiBold + title + ansiReset)

	fmt.Println(bl.top)
	fmt.Println(ansiDim + bl.hdr(mW, vW, vW, cW) + ansiReset)
	fmt.Println(bl.sep)

	for _, hm := range headlineMetrics {
		row, ok := rowMap[hm.path]
		if !ok {
			continue
		}
		lbl := hm.label
		if hm.unit != "" {
			lbl = fmt.Sprintf("%s (%s)", lbl, hm.unit)
		}
		if len(lbl) > mW {
			lbl = lbl[:mW-1] + "…"
		}
		aStr := fmt.Sprintf("%.2f", row.Before)
		bStr := fmt.Sprintf("%.2f", row.After)
		ch := signCell(row.PctChg, cW)

		fmt.Printf("  │ %s%-*s%s │ %*s │ %s%*s%s │ %s │\n",
			ansiCyan, mW, lbl, ansiReset,
			vW, aStr,
			ansiBold, vW, bStr, ansiReset,
			ch)
	}

	fmt.Println(bl.bot)
	fmt.Println()
}

// ── Compact table: metric rows × stat columns ─────────────────────────────────

func printCompactTable(rowMap map[string]history.DiffRow) {
	const (
		mW    = 34 // metric label visible width
		cW    = 7  // each stat cell visible width (fits "+999.9%")
		nCols = 6
	)
	mBox := mW + 2 // 36 — metric col box (includes 1-space padding each side)
	sBox := cW + 2 // 9  — stat col box

	top := "  ┌" + rep("─", mBox) + strings.Repeat("┬"+rep("─", sBox), nCols) + "┐"
	hdrRow := fmt.Sprintf("  │ %-*s ", mW, "Metric")
	for _, col := range statCols {
		hdrRow += "│" + center(col, sBox)
	}
	hdrRow += "│"
	sep := "  ├" + rep("─", mBox) + strings.Repeat("┼"+rep("─", sBox), nCols) + "┤"
	bot := "  └" + rep("─", mBox) + strings.Repeat("┴"+rep("─", sBox), nCols) + "┘"

	title := " All Metrics  (+ green  − red) "
	boxW := 1 + mBox + nCols*(1+sBox) + 1
	pad := max((boxW-len(title))/2, 0)
	fmt.Println(strings.Repeat(" ", pad+2) + ansiBold + title + ansiReset)

	fmt.Println(top)
	fmt.Println(ansiDim + hdrRow + ansiReset)
	fmt.Println(sep)

	printed := false
	for _, cm := range compactMetrics {
		// skip groups where every stat is zero or missing
		hasAny := false
		for _, stat := range statCols {
			r, ok := rowMap[cm.prefix+"."+stat]
			if ok && !(r.Before == 0 && r.After == 0) && math.Abs(r.PctChg) >= 0.05 {
				hasAny = true
				break
			}
		}
		if !hasAny {
			continue
		}

		lbl := cm.label
		if len(lbl) > mW {
			lbl = lbl[:mW-1] + "…"
		}

		row := fmt.Sprintf("  │ %s%-*s%s ", ansiCyan, mW, lbl, ansiReset)
		for _, stat := range statCols {
			path := cm.prefix + "." + stat
			r, ok := rowMap[path]
			if !ok || (r.Before == 0 && r.After == 0) || math.Abs(r.PctChg) < 0.05 {
				row += "│" + dashCell(sBox)
			} else {
				row += "│" + signCell(r.PctChg, sBox)
			}
		}
		row += "│"
		fmt.Println(row)
		printed = true
	}

	if !printed {
		fmt.Println("  │" + ansiDim + center("(no changes)", mBox+nCols*(1+sBox)+1) + ansiReset + "│")
	}

	fmt.Println(bot)
	fmt.Println()
}

// ── Cell helpers ──────────────────────────────────────────────────────────────

// signCell returns a right-aligned colored % change of visible width w.
// positive → green, negative → red, ~zero → dim.
func signCell(pct float64, w int) string {
	sign := "+"
	if pct < 0 {
		sign = ""
	}
	raw := fmt.Sprintf("%s%.1f%%", sign, pct)
	pad := strings.Repeat(" ", max(w-len(raw), 0))

	var color string
	switch {
	case math.Abs(pct) < 0.05:
		color = ansiDim
	case pct > 0:
		color = ansiGreen
	default:
		color = ansiRed
	}
	return pad + color + raw + ansiReset
}

// dashCell returns a centered dim "—" of visible width w.
func dashCell(w int) string {
	left := (w - 1) / 2
	right := w - 1 - left
	return ansiDim + strings.Repeat(" ", left) + "—" + strings.Repeat(" ", right) + ansiReset
}

// center returns s centered within a string of visible width w.
func center(s string, w int) string {
	if len(s) >= w {
		return s
	}
	total := w - len(s)
	left := total / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", total-left)
}

// ── Box drawing helpers ───────────────────────────────────────────────────────

type tableBox struct{ top, bot, sep string }

func (b tableBox) hdr(mW, v1W, v2W, cW int) string {
	return fmt.Sprintf("  │ %-*s │ %*s │ %*s │ %*s │",
		mW, "Metric", v1W, "Run A", v2W, "Run B", cW, "Change")
}

func makeBox(mW, v1W, v2W, cW int) tableBox {
	m := rep("─", mW+2)
	v1 := rep("─", v1W+2)
	v2 := rep("─", v2W+2)
	c := rep("─", cW+2)
	return tableBox{
		top: "  ┌" + m + "┬" + v1 + "┬" + v2 + "┬" + c + "┐",
		bot: "  └" + m + "┴" + v1 + "┴" + v2 + "┴" + c + "┘",
		sep: "  ├" + m + "┼" + v1 + "┼" + v2 + "┼" + c + "┤",
	}
}

func rep(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(s, n)
}
