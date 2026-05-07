package cmd

import (
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/spf13/cobra"

	"github.com/RashRAJ/all-bench/history"
)

// ── Color palette ──────────────────────────────────────────────────────────────

var (
	colorCyan    = lipgloss.Color("#22d3ee")
	colorBlue    = lipgloss.Color("#60a5fa")
	colorGreen   = lipgloss.Color("#34d399")
	colorRed     = lipgloss.Color("#f87171")
	colorYellow  = lipgloss.Color("#fbbf24")
	colorMagenta = lipgloss.Color("#c084fc")
	colorDim     = lipgloss.Color("#5c5d62")
	colorText    = lipgloss.Color("#e8e6e0")
	colorSubtle  = lipgloss.Color("#8a8b8f")
	colorBorder  = lipgloss.Color("#2a2d3a")
)

// ── Reusable styles ────────────────────────────────────────────────────────────

var (
	bold      = lipgloss.NewStyle().Bold(true)
	dim       = lipgloss.NewStyle().Foreground(colorDim)
	subtle    = lipgloss.NewStyle().Foreground(colorSubtle)
	greenText = lipgloss.NewStyle().Foreground(colorGreen)
	redText   = lipgloss.NewStyle().Foreground(colorRed)
	cyanText  = lipgloss.NewStyle().Foreground(colorCyan)
	cyanBold  = lipgloss.NewStyle().Foreground(colorCyan).Bold(true)

	// indent wraps content with a 2-char left margin.
	indent = lipgloss.NewStyle().PaddingLeft(2)
)

// ── Metric definitions ─────────────────────────────────────────────────────────

var keyMetrics = []struct {
	path        string
	label       string
	unit        string
	lowerBetter bool
}{
	{"time_to_first_token.avg", "TTFT", "ms", true},
	{"request_latency.avg", "Req Latency", "ms", true},
	{"output_token_throughput.avg", "Throughput", "t/s", false},
	{"benchmark_duration.avg", "Duration", "s", true},
}

var verdictMetrics = []struct {
	path        string
	lowerBetter bool
}{
	{"time_to_first_token.avg", true},
	{"time_to_second_token.avg", true},
	{"request_latency.avg", true},
	{"inter_token_latency.avg", true},
	{"output_token_throughput_per_user.avg", false},
	{"output_token_throughput.avg", false},
	{"request_throughput.avg", false},
	{"output_sequence_length.avg", false},
}

var lowerIsBetter = map[string]bool{
	"time_to_first_token":          true,
	"time_to_second_token":         true,
	"time_to_first_output_token":   true,
	"request_latency":              true,
	"inter_token_latency":          true,
	"inter_chunk_latency":          true,
	"http_req_duration":            true,
	"http_req_waiting":             true,
	"http_req_receiving":           true,
	"http_req_sending":             true,
	"http_req_connecting":          true,
	"http_req_connection_overhead": true,
	"http_req_dns_lookup":          true,
	"benchmark_duration":           true,
}

var detailMetrics = []struct {
	prefix string
	label  string
	unit   string
}{
	{"time_to_first_token", "Time to first token", "ms"},
	{"time_to_second_token", "Time to second token", "ms"},
	{"request_latency", "Request latency", "ms"},
	{"inter_token_latency", "Inter token latency", "ms"},
	{"output_token_throughput_per_user", "Throughput/user", "t/s/u"},
	{"output_token_throughput", "Token throughput", "t/s"},
	{"request_throughput", "Request throughput", "req/s"},
	{"output_sequence_length", "Output seq length", "tokens"},
}

var pctMetrics = []struct {
	prefix string
	label  string
}{
	{"time_to_first_token", "TTFT"},
	{"time_to_second_token", "TTST"},
	{"request_latency", "Req latency"},
	{"inter_token_latency", "ITL"},
	{"inter_chunk_latency", "Chunk latency"},
	{"output_token_throughput_per_user", "Tput/user"},
	{"prefill_throughput_per_user", "Prefill tput/user"},
	{"http_req_duration", "HTTP duration"},
	{"http_req_waiting", "HTTP waiting"},
	{"http_req_receiving", "HTTP recv"},
	{"http_req_sending", "HTTP send"},
}

var statCols = []string{"avg", "p50", "p90", "p99", "min", "max"}

// ── Command ────────────────────────────────────────────────────────────────────

var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Compare the last two benchmark runs",
	Long: `Show a detailed side-by-side comparison of the two most recent benchmark
snapshots, including key metric cards, a detailed table with spark bars,
and percentile distributions.`,
	RunE: runDiff,
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
		warn := lipgloss.NewStyle().
			Foreground(colorYellow).
			Bold(true).
			PaddingLeft(2).
			Render("⚠  Not enough history — run `allbench run` at least twice first.")
		lipgloss.Println("\n" + warn + "\n")
		return nil
	}

	a, b := snaps[0], snaps[1]
	tsA := a.Timestamp.Local().Format("2006-01-02 15:04:05")
	tsB := b.Timestamp.Local().Format("2006-01-02 15:04:05")

	cfgChanges := history.DiffConfig(a.Config, b.Config)
	cfgInfo := extractConfigInfo(b.Config)

	// Collect runner names across both snapshots.
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

	type runnerSection struct {
		label  string
		rowMap map[string]history.DiffRow
	}
	var sections []runnerSection

	for _, rName := range runnerNames {
		aRaws := a.RawOutputs[rName]
		bRaws := b.RawOutputs[rName]
		if len(aRaws) == 0 || len(bRaws) == 0 {
			continue
		}
		pairs := min(len(aRaws), len(bRaws))
		for i := range pairs {
			rows := history.DiffRaw(aRaws[i], bRaws[i])
			rowMap := make(map[string]history.DiffRow, len(rows))
			for _, r := range rows {
				rowMap[r.Path] = r
			}
			lbl := strings.ToUpper(rName)
			if pairs > 1 {
				lbl = fmt.Sprintf("%s [level %d]", lbl, i+1)
			}
			sections = append(sections, runnerSection{lbl, rowMap})
		}
	}

	// ── Render ──────────────────────────────────────────────────────────────

	var durRow *history.DiffRow
	if len(sections) > 0 {
		if r, ok := sections[0].rowMap["benchmark_duration.avg"]; ok {
			durRow = &r
		}
	}

	lipgloss.Println(renderHeader())
	lipgloss.Println(renderRunInfo(cfgInfo, tsA, tsB, durRow))

	if len(cfgChanges) > 0 {
		lipgloss.Println(renderConfigChanges(cfgChanges))
	}

	for _, sec := range sections {
		imp, total := countImproved(sec.rowMap)
		lipgloss.Println(renderVerdict(imp, total))
		lipgloss.Println(renderSectionHeader("Key metrics", colorCyan, sec.label))
		lipgloss.Println(renderKeyMetricsCards(sec.rowMap))
		lipgloss.Println(renderSectionHeader("Detailed comparison", colorBlue, "Run A → Run B"))
		lipgloss.Println(renderDetailedTable(sec.rowMap))
		lipgloss.Println(renderSectionHeader("Percentile distribution", colorMagenta, "Δ %"))
		lipgloss.Println(renderPercentileTable(sec.rowMap))
	}

	lipgloss.Println(renderFooter(tsA, tsB))
	return nil
}

// ── Config extraction ──────────────────────────────────────────────────────────

func extractConfigInfo(raw json.RawMessage) map[string]string {
	var cfg map[string]interface{}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil
	}
	info := make(map[string]string)
	if defaults, ok := cfg["defaults"].(map[string]interface{}); ok {
		if m, ok := defaults["model"].(string); ok {
			parts := strings.Split(m, "/")
			info["profile"] = parts[len(parts)-1]
		}
	}
	if runners, ok := cfg["runners"].([]interface{}); ok && len(runners) > 0 {
		names := make([]string, 0, len(runners))
		for _, r := range runners {
			if s, ok := r.(string); ok {
				names = append(names, s)
			}
		}
		info["runners"] = strings.Join(names, ", ")
	}
	return info
}

func countImproved(rowMap map[string]history.DiffRow) (improved, total int) {
	for _, vm := range verdictMetrics {
		r, ok := rowMap[vm.path]
		if !ok {
			continue
		}
		total++
		if (vm.lowerBetter && r.PctChg < -0.5) || (!vm.lowerBetter && r.PctChg > 0.5) {
			improved++
		}
	}
	return
}

// ── Render: Header ─────────────────────────────────────────────────────────────
//
// Branded top bar:
//   ALL-bench  v1.0.0
//   Unified AI inference benchmarking  │  vLLM · AIPerf · GPU telemetry

func renderHeader() string {
	brand := cyanBold.Render("ALL-bench")
	ver := dim.Render("v1.0.0")
	line1 := indent.Render(brand + "  " + ver)

	tagline := dim.Render("Unified AI inference benchmarking  │  vLLM · AIPerf · GPU telemetry")
	line2 := indent.Render(tagline)

	return "\n" + line1 + "\n" + line2 + "\n"
}

// ── Render: Run info ───────────────────────────────────────────────────────────
//
// Single metadata line:
//   Profile llama-3.1-70b · Runners aiperf · Duration 82.9s → 54.5s  -34.2%

func renderRunInfo(info map[string]string, tsA, tsB string, dur *history.DiffRow) string {
	sep := dim.Render(" · ")
	var parts []string

	if p := info["profile"]; p != "" {
		parts = append(parts, dim.Render("Profile")+" "+p)
	}
	if r := info["runners"]; r != "" {
		parts = append(parts, dim.Render("Runners")+" "+r)
	}
	if dur != nil {
		clr := greenText
		if dur.PctChg > 0 {
			clr = redText
		}
		durStr := fmt.Sprintf("%s → %s  %s",
			formatVal(dur.Before, "s"),
			bold.Render(formatVal(dur.After, "s")),
			clr.Render(fmtPct(dur.PctChg)),
		)
		parts = append(parts, dim.Render("Duration")+" "+durStr)
	}

	if len(parts) == 0 {
		return ""
	}
	return indent.Render(strings.Join(parts, sep)) + "\n"
}

// ── Render: Config changes ─────────────────────────────────────────────────────
//
//   ▍ Config changes
//     AiPerf.RequestRate  40  →  35

func renderConfigChanges(changes [][3]string) string {
	out := renderSectionHeader("Config changes", colorCyan, "") + "\n"

	strike := lipgloss.NewStyle().Foreground(colorDim).Strikethrough(true)
	greenBold := lipgloss.NewStyle().Foreground(colorGreen).Bold(true)

	for _, ch := range changes {
		line := indent.Render(
			"  " + cyanText.Render(ch[0]) + "  " +
				strike.Render(ch[1]) + "  →  " +
				greenBold.Render(ch[2]),
		)
		out += line + "\n"
	}
	return out
}

// ── Render: Verdict banner ─────────────────────────────────────────────────────
//
//   ✓  Improved  ─  5 of 8 key metrics improved

func renderVerdict(improved, total int) string {
	if total == 0 {
		return ""
	}

	label := "Improved"
	icon := "✓"
	clr := colorGreen
	if improved <= total/2 {
		label = "Regressed"
		icon = "✗"
		clr = colorRed
	}

	iconStyle := lipgloss.NewStyle().Foreground(clr)
	labelStyle := lipgloss.NewStyle().Foreground(clr).Bold(true)
	detail := fmt.Sprintf("%d of %d key metrics improved", improved, total)

	return "\n" + indent.Render(
		iconStyle.Render(icon)+"  "+
			labelStyle.Render(label)+
			dim.Render("  ─  "+detail),
	) + "\n"
}

// ── Render: Section headers ────────────────────────────────────────────────────
//
//   ▍ Key metrics  AIPERF

func renderSectionHeader(title string, iconClr color.Color, badge string) string {
	icon := lipgloss.NewStyle().Foreground(iconClr).Render("▍")
	t := bold.Render(title)

	result := icon + " " + t
	if badge != "" {
		result += "  " + dim.Render(badge)
	}

	return "\n" + indent.Render(result)
}

// ── Render: Key metric cards ───────────────────────────────────────────────────
//
// Four compact cards joined horizontally with lipgloss.JoinHorizontal.
//
//   ╭────────────────╮ ╭────────────────╮ ╭────────────────╮ ╭────────────────╮
//   │ TTFT           │ │ Req Latency    │ │ Throughput      │ │ Duration       │
//   │ 7.62s          │ │ 37.02s         │ │ 355.2           │ │ 54.5s          │
//   │ ▼ +9.4%        │ │ ▲ -3.9%        │ │ ▲ +39.5%        │ │ ▲ -34.2%       │
//   ╰────────────────╯ ╰────────────────╯ ╰────────────────╯ ╰────────────────╯

func renderKeyMetricsCards(rowMap map[string]history.DiffRow) string {
	const cardWidth = 16

	cardBorder := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Width(cardWidth).
		Padding(0, 1)

	var cards []string

	for _, km := range keyMetrics {
		r, ok := rowMap[km.path]
		if !ok {
			content := dim.Render(km.label) + "\n" +
				dim.Render("—") + "\n" +
				dim.Render("—")
			cards = append(cards, cardBorder.Render(content))
			continue
		}

		improved := (km.lowerBetter && r.PctChg < 0) || (!km.lowerBetter && r.PctChg > 0)
		clr := colorGreen
		arrow := "▲"
		if !improved {
			clr = colorRed
			arrow = "▼"
		}

		valStyle := lipgloss.NewStyle().Foreground(clr).Bold(true)
		chgStyle := lipgloss.NewStyle().Foreground(clr)

		label := dim.Render(km.label)
		val := valStyle.Render(formatVal(r.After, km.unit))
		chg := chgStyle.Render(arrow + " " + fmtPct(r.PctChg))

		content := label + "\n" + val + "\n" + chg
		cards = append(cards, cardBorder.Render(content))
	}

	joined := lipgloss.JoinHorizontal(lipgloss.Top, cards...)
	return indent.Render(joined) + "\n"
}

// ── Render: Detailed comparison table ──────────────────────────────────────────
//
// Uses lipgloss/table with StyleFunc for per-cell coloring.
// The "Change" column contains a spark bar + colored percentage.
//
//   ╭────────────────────────┬──────────┬──────────┬────────────────╮
//   │ Metric                 │   Run A  │   Run B  │         Change │
//   ├────────────────────────┼──────────┼──────────┼────────────────┤
//   │ Time to first token    │   6.96s  │   7.62s  │  █░░░░   +9.4% │
//   │ Request latency        │  38.53s  │  37.02s  │  █░░░░   -3.9% │
//   │ Token throughput       │  254.5   │  355.2   │  ████░  +39.5% │
//   ╰────────────────────────┴──────────┴──────────┴────────────────╯

func renderDetailedTable(rowMap map[string]history.DiffRow) string {
	headerStyle := lipgloss.NewStyle().
		Foreground(colorSubtle).
		Bold(true).
		Padding(0, 1).
		Align(lipgloss.Center)

	metricCellStyle := lipgloss.NewStyle().
		Foreground(colorSubtle).
		Padding(0, 1).
		Width(24)

	valueCellStyle := lipgloss.NewStyle().
		Padding(0, 1).
		Width(10).
		Align(lipgloss.Right)

	changeCellStyle := lipgloss.NewStyle().
		Padding(0, 1).
		Width(16).
		Align(lipgloss.Right)

	type rowMeta struct {
		clr color.Color
	}

	var rows [][]string
	var metas []rowMeta

	for _, dm := range detailMetrics {
		r, ok := rowMap[dm.prefix+".avg"]
		if !ok {
			continue
		}

		improved := (lowerIsBetter[dm.prefix] && r.PctChg < 0) ||
			(!lowerIsBetter[dm.prefix] && r.PctChg > 0)
		clr := colorGreen
		if !improved {
			clr = colorRed
		}
		if math.Abs(r.PctChg) < 0.5 {
			clr = colorDim
		}

		bar := renderSparkBar(r.PctChg, lowerIsBetter[dm.prefix], 5)
		change := bar + " " + fmtPct(r.PctChg)

		rows = append(rows, []string{
			dm.label,
			formatVal(r.Before, dm.unit),
			formatVal(r.After, dm.unit),
			change,
		})
		metas = append(metas, rowMeta{clr})
	}

	if len(rows) == 0 {
		return indent.Render(dim.Render("  (no data)")) + "\n"
	}

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(colorBorder)).
		Headers("Metric", "Run A", "Run B", "Change").
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			switch col {
			case 0:
				return metricCellStyle
			case 1:
				return valueCellStyle
			case 2:
				s := valueCellStyle
				if row >= 0 && row < len(metas) {
					s = s.Foreground(metas[row].clr)
				}
				return s
			case 3:
				s := changeCellStyle
				if row >= 0 && row < len(metas) {
					s = s.Foreground(metas[row].clr)
				}
				return s
			default:
				return lipgloss.NewStyle()
			}
		})

	return indent.Render(t.String()) + "\n"
}

// ── Render: Percentile distribution table ──────────────────────────────────────
//
// Per-cell coloring based on whether each metric's change at each
// percentile is an improvement or regression.
//
//   ╭──────────────────┬────────┬────────┬────────┬────────┬────────┬────────╮
//   │ Metric           │   avg  │   p50  │   p90  │   p99  │   min  │   max  │
//   ├──────────────────┼────────┼────────┼────────┼────────┼────────┼────────┤
//   │ TTFT             │  +9.4% │  +9.1% │  +7.6% │  +4.4% │ +55.8% │  +3.8% │
//   │ Req latency      │  -3.9% │  +4.4% │  -8.7% │ -29.0% │ +15.9% │ -33.9% │
//   ╰──────────────────┴────────┴────────┴────────┴────────┴────────┴────────╯

func renderPercentileTable(rowMap map[string]history.DiffRow) string {
	headerStyle := lipgloss.NewStyle().
		Foreground(colorSubtle).
		Bold(true).
		Padding(0, 1).
		Align(lipgloss.Center)

	metricCellStyle := lipgloss.NewStyle().
		Foreground(colorSubtle).
		Padding(0, 1).
		Width(18)

	pctCellStyle := lipgloss.NewStyle().
		Padding(0, 1).
		Width(8).
		Align(lipgloss.Right)

	headers := []string{"Metric"}
	headers = append(headers, statCols...)

	var rows [][]string
	// cellColors[row][col] stores the color for each data cell.
	var cellColors [][]color.Color

	for _, pm := range pctMetrics {
		hasAny := false
		for _, stat := range statCols {
			r, ok := rowMap[pm.prefix+"."+stat]
			if ok && math.Abs(r.PctChg) >= 0.05 {
				hasAny = true
				break
			}
		}
		if !hasAny {
			continue
		}

		row := []string{pm.label}
		colors := []color.Color{colorSubtle} // metric name column

		for _, stat := range statCols {
			r, ok := rowMap[pm.prefix+"."+stat]
			if !ok || math.Abs(r.PctChg) < 0.05 {
				row = append(row, "—")
				colors = append(colors, colorDim)
			} else {
				improved := (lowerIsBetter[pm.prefix] && r.PctChg < 0) ||
					(!lowerIsBetter[pm.prefix] && r.PctChg > 0)
				clr := colorGreen
				if !improved {
					clr = colorRed
				}
				row = append(row, fmtPct(r.PctChg))
				colors = append(colors, clr)
			}
		}

		rows = append(rows, row)
		cellColors = append(cellColors, colors)
	}

	if len(rows) == 0 {
		return indent.Render(dim.Render("  (no data)")) + "\n"
	}

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(colorBorder)).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			if col == 0 {
				return metricCellStyle
			}
			s := pctCellStyle
			if row >= 0 && row < len(cellColors) && col < len(cellColors[row]) {
				s = s.Foreground(cellColors[row][col])
			}
			return s
		})

	return indent.Render(t.String()) + "\n"
}

// ── Render: Footer ─────────────────────────────────────────────────────────────
//
//   ● Benchmark completed    2026-05-04 12:59 → 13:07  │  allbench diff

func renderFooter(tsA, tsB string) string {
	dot := lipgloss.NewStyle().Foreground(colorGreen).Render("●")
	ts := dim.Render(tsA + " → " + tsB)
	cmd := dim.Render("allbench diff")

	return indent.Render(
		dot+" "+dim.Render("Benchmark completed")+"    "+ts+"  │  "+cmd,
	) + "\n"
}

// ── Spark bar ──────────────────────────────────────────────────────────────────
//
// Renders a small visual bar like "███░░" colored green (improved) / red (regressed).

func renderSparkBar(pct float64, lowerBetter bool, w int) string {
	improved := (lowerBetter && pct < 0) || (!lowerBetter && pct > 0)
	filled := min(int(math.Round(math.Abs(pct)/50.0*float64(w))), w)
	if filled == 0 && math.Abs(pct) >= 0.5 {
		filled = 1
	}

	clr := colorGreen
	if !improved {
		clr = colorRed
	}
	if math.Abs(pct) < 0.5 {
		clr = colorDim
	}

	active := lipgloss.NewStyle().Foreground(clr).Render(strings.Repeat("█", filled))
	inactive := dim.Render(strings.Repeat("░", w-filled))
	return active + inactive
}

// ── Value formatting ───────────────────────────────────────────────────────────

// fmtPct formats a percentage with a sign prefix.
// Drops the decimal for |pct| ≥ 100 to save column width.
func fmtPct(pct float64) string {
	if math.Abs(pct) >= 100 {
		if pct > 0 {
			return fmt.Sprintf("+%.0f%%", pct)
		}
		return fmt.Sprintf("%.0f%%", pct)
	}
	sign := "+"
	if pct < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%.1f%%", sign, pct)
}

// formatVal renders a raw float in a human-readable form for the given unit.
func formatVal(v float64, unit string) string {
	switch unit {
	case "ms":
		if math.Abs(v) >= 1000 {
			return fmt.Sprintf("%.2fs", v/1000)
		}
		return fmt.Sprintf("%.1fms", v)
	case "s":
		return fmt.Sprintf("%.1fs", v)
	case "t/s", "t/s/u":
		return fmt.Sprintf("%.1f", v)
	case "tokens":
		return fmt.Sprintf("%.0f", v)
	case "req/s":
		return fmt.Sprintf("%.2f", v)
	default:
		return fmt.Sprintf("%.2f", v)
	}
}





// ############## 2
// package cmd

// import (
// 	"encoding/json"
// 	"fmt"
// 	"math"
// 	"sort"
// 	"strings"

// 	"github.com/spf13/cobra"

// 	"github.com/RashRAJ/all-bench/history"
// )

// // ── ANSI escape codes ──────────────────────────────────────────────────────────

// const (
// 	ansiReset   = "\033[0m"
// 	ansiRed     = "\033[31m"
// 	ansiGreen   = "\033[32m"
// 	ansiYellow  = "\033[33m"
// 	ansiBlue    = "\033[34m"
// 	ansiMagenta = "\033[35m"
// 	ansiCyan    = "\033[36m"
// 	ansiBold    = "\033[1m"
// 	ansiDim     = "\033[2m"
// 	ansiStrike  = "\033[9m"
// 	ansiItalic  = "\033[3m"
// 	ansiBgGreen = "\033[48;5;22m"
// 	ansiBgRed   = "\033[48;5;52m"
// )

// // ── Metric definitions ─────────────────────────────────────────────────────────

// var keyMetrics = []struct {
// 	path        string
// 	label       string
// 	unit        string
// 	lowerBetter bool
// }{
// 	{"time_to_first_token.avg", "TTFT", "ms", true},
// 	{"request_latency.avg", "Req Latency", "ms", true},
// 	{"output_token_throughput.avg", "Throughput", "t/s", false},
// 	{"benchmark_duration.avg", "Duration", "s", true},
// }

// var verdictMetrics = []struct {
// 	path        string
// 	lowerBetter bool
// }{
// 	{"time_to_first_token.avg", true},
// 	{"time_to_second_token.avg", true},
// 	{"request_latency.avg", true},
// 	{"inter_token_latency.avg", true},
// 	{"output_token_throughput_per_user.avg", false},
// 	{"output_token_throughput.avg", false},
// 	{"request_throughput.avg", false},
// 	{"output_sequence_length.avg", false},
// }

// var lowerIsBetter = map[string]bool{
// 	"time_to_first_token":          true,
// 	"time_to_second_token":         true,
// 	"time_to_first_output_token":   true,
// 	"request_latency":              true,
// 	"inter_token_latency":          true,
// 	"inter_chunk_latency":          true,
// 	"http_req_duration":            true,
// 	"http_req_waiting":             true,
// 	"http_req_receiving":           true,
// 	"http_req_sending":             true,
// 	"http_req_connecting":          true,
// 	"http_req_connection_overhead": true,
// 	"http_req_dns_lookup":          true,
// 	"benchmark_duration":           true,
// }

// var detailMetrics = []struct {
// 	prefix string
// 	label  string
// 	unit   string
// }{
// 	{"time_to_first_token", "Time to first token", "ms"},
// 	{"time_to_second_token", "Time to second token", "ms"},
// 	{"request_latency", "Request latency", "ms"},
// 	{"inter_token_latency", "Inter token latency", "ms"},
// 	{"output_token_throughput_per_user", "Throughput/user", "t/s/u"},
// 	{"output_token_throughput", "Token throughput", "t/s"},
// 	{"request_throughput", "Request throughput", "req/s"},
// 	{"output_sequence_length", "Output seq length", "tokens"},
// }

// var pctMetrics = []struct {
// 	prefix string
// 	label  string
// }{
// 	{"time_to_first_token", "TTFT"},
// 	{"time_to_second_token", "TTST"},
// 	{"request_latency", "Req latency"},
// 	{"inter_token_latency", "ITL"},
// 	{"inter_chunk_latency", "Chunk latency"},
// 	{"output_token_throughput_per_user", "Tput/user"},
// 	{"prefill_throughput_per_user", "Prefill tput/user"},
// 	{"http_req_duration", "HTTP duration"},
// 	{"http_req_waiting", "HTTP waiting"},
// 	{"http_req_receiving", "HTTP recv"},
// 	{"http_req_sending", "HTTP send"},
// }

// var statCols = []string{"avg", "p50", "p90", "p99", "min", "max"}

// // ── Command ────────────────────────────────────────────────────────────────────

// var diffCmd = &cobra.Command{
// 	Use:   "diff",
// 	Short: "Compare the last two benchmark runs",
// 	Long:  "Show a detailed side-by-side comparison of the two most recent benchmark snapshots, including key metric cards, a detailed table with spark bars, and percentile distributions.",
// 	RunE:  runDiff,
// }

// func init() {
// 	rootCmd.AddCommand(diffCmd)
// }

// func runDiff(cmd *cobra.Command, args []string) error {
// 	snaps, err := history.LoadLast(2)
// 	if err != nil {
// 		return fmt.Errorf("loading history: %w", err)
// 	}
// 	if len(snaps) < 2 {
// 		fmt.Println()
// 		fmt.Printf("  %s⚠%s  Not enough history — run %sallbench run%s at least twice first.\n\n",
// 			ansiYellow, ansiReset, ansiBold, ansiReset)
// 		return nil
// 	}

// 	a, b := snaps[0], snaps[1]
// 	tsA := a.Timestamp.Local().Format("2006-01-02 15:04:05")
// 	tsB := b.Timestamp.Local().Format("2006-01-02 15:04:05")

// 	cfgChanges := history.DiffConfig(a.Config, b.Config)
// 	cfgInfo := extractConfigInfo(b.Config)

// 	// Collect all runner names across both snapshots.
// 	seen := make(map[string]bool)
// 	var runnerNames []string
// 	for k := range a.RawOutputs {
// 		if !seen[k] {
// 			seen[k] = true
// 			runnerNames = append(runnerNames, k)
// 		}
// 	}
// 	for k := range b.RawOutputs {
// 		if !seen[k] {
// 			seen[k] = true
// 			runnerNames = append(runnerNames, k)
// 		}
// 	}
// 	sort.Strings(runnerNames)

// 	type runnerSection struct {
// 		label  string
// 		rowMap map[string]history.DiffRow
// 	}
// 	var sections []runnerSection

// 	for _, rName := range runnerNames {
// 		aRaws := a.RawOutputs[rName]
// 		bRaws := b.RawOutputs[rName]
// 		if len(aRaws) == 0 || len(bRaws) == 0 {
// 			continue
// 		}
// 		pairs := min(len(aRaws), len(bRaws))
// 		for i := range pairs {
// 			rows := history.DiffRaw(aRaws[i], bRaws[i])
// 			rowMap := make(map[string]history.DiffRow, len(rows))
// 			for _, r := range rows {
// 				rowMap[r.Path] = r
// 			}
// 			lbl := strings.ToUpper(rName)
// 			if pairs > 1 {
// 				lbl = fmt.Sprintf("%s [level %d]", lbl, i+1)
// 			}
// 			sections = append(sections, runnerSection{lbl, rowMap})
// 		}
// 	}

// 	// ── Render ──────────────────────────────────────────────────────────────

// 	printHeader()

// 	var durRow *history.DiffRow
// 	if len(sections) > 0 {
// 		if r, ok := sections[0].rowMap["benchmark_duration.avg"]; ok {
// 			durRow = &r
// 		}
// 	}
// 	printRunInfo(cfgInfo, tsA, tsB, durRow)

// 	if len(cfgChanges) > 0 {
// 		printConfigChanges(cfgChanges)
// 	}

// 	for _, sec := range sections {
// 		imp, total := countImproved(sec.rowMap)
// 		printVerdict(imp, total)

// 		printSectionHeader("Key metrics", ansiCyan, sec.label)
// 		printKeyMetricsCards(sec.rowMap)

// 		printSectionHeader("Detailed comparison", ansiBlue, "Run A → Run B")
// 		printDetailedTable(sec.rowMap)

// 		printSectionHeader("Percentile distribution", ansiMagenta, "Δ %")
// 		printPercentileTable(sec.rowMap)
// 	}

// 	printFooter(tsA, tsB)
// 	return nil
// }

// // ── Config extraction ──────────────────────────────────────────────────────────

// func extractConfigInfo(raw json.RawMessage) map[string]string {
// 	var cfg map[string]interface{}
// 	if err := json.Unmarshal(raw, &cfg); err != nil {
// 		return nil
// 	}
// 	info := make(map[string]string)
// 	if defaults, ok := cfg["defaults"].(map[string]interface{}); ok {
// 		if m, ok := defaults["model"].(string); ok {
// 			parts := strings.Split(m, "/")
// 			info["profile"] = parts[len(parts)-1]
// 		}
// 	}
// 	if runners, ok := cfg["runners"].([]interface{}); ok && len(runners) > 0 {
// 		names := make([]string, 0, len(runners))
// 		for _, r := range runners {
// 			if s, ok := r.(string); ok {
// 				names = append(names, s)
// 			}
// 		}
// 		info["runners"] = strings.Join(names, ", ")
// 	}
// 	return info
// }

// func countImproved(rowMap map[string]history.DiffRow) (improved, total int) {
// 	for _, vm := range verdictMetrics {
// 		r, ok := rowMap[vm.path]
// 		if !ok {
// 			continue
// 		}
// 		total++
// 		if (vm.lowerBetter && r.PctChg < -0.5) || (!vm.lowerBetter && r.PctChg > 0.5) {
// 			improved++
// 		}
// 	}
// 	return
// }

// // ── Print: Header & Footer ─────────────────────────────────────────────────────

// func printHeader() {
// 	fmt.Println()
// 	// Brand bar with gradient-style accent
// 	fmt.Printf("  %s%s╭─ ALL-bench%s  %sv1.0.0%s\n",
// 		ansiBold, ansiCyan, ansiReset, ansiDim, ansiReset)
// 	fmt.Printf("  %s│  Unified AI inference benchmarking%s\n",
// 		ansiDim, ansiReset)
// 	fmt.Printf("  %s╰─ vLLM · AIPerf · GPU telemetry%s\n",
// 		ansiDim, ansiReset)
// 	fmt.Println()
// }

// func printRunInfo(info map[string]string, tsA, tsB string, dur *history.DiffRow) {
// 	// Compact metadata line with labeled fields
// 	fields := []struct{ label, value string }{}

// 	if p := info["profile"]; p != "" {
// 		fields = append(fields, struct{ label, value string }{"Profile", p})
// 	}
// 	if r := info["runners"]; r != "" {
// 		fields = append(fields, struct{ label, value string }{"Runners", r})
// 	}

// 	if dur != nil {
// 		color := ansiGreen
// 		if dur.PctChg > 0 {
// 			color = ansiRed
// 		}
// 		durStr := fmt.Sprintf("%s → %s%s%s  %s%s%s",
// 			formatVal(dur.Before, "s"),
// 			ansiBold, formatVal(dur.After, "s"), ansiReset,
// 			color, fmtPct(dur.PctChg), ansiReset,
// 		)
// 		fields = append(fields, struct{ label, value string }{"Duration", durStr})
// 	}

// 	if len(fields) > 0 {
// 		line := "  "
// 		for i, f := range fields {
// 			if i > 0 {
// 				line += fmt.Sprintf("  %s·%s  ", ansiDim, ansiReset)
// 			}
// 			line += fmt.Sprintf("%s%s%s %s", ansiDim, f.label, ansiReset, f.value)
// 		}
// 		fmt.Println(line)
// 		fmt.Println()
// 	}
// }

// func printFooter(tsA, tsB string) {
// 	dot := ansiGreen + "●" + ansiReset
// 	fmt.Printf("  %s Benchmark completed    %s%s → %s%s  %s│%s  allbench diff\n\n",
// 		dot, ansiDim, tsA, tsB, ansiReset, ansiDim, ansiReset)
// }

// // ── Print: Config Changes ──────────────────────────────────────────────────────

// func printConfigChanges(changes [][3]string) {
// 	printSectionHeader("Config changes", ansiCyan, "")
// 	fmt.Println()
// 	for _, ch := range changes {
// 		fmt.Printf("    %s%s%s  %s%s%s%s → %s%s%s\n",
// 			ansiCyan, ch[0], ansiReset,
// 			ansiDim+ansiStrike, ch[1], ansiReset,
// 			"  ",
// 			ansiGreen+ansiBold, ch[2], ansiReset)
// 	}
// 	fmt.Println()
// }

// // ── Print: Verdict Banner ──────────────────────────────────────────────────────

// func printVerdict(improved, total int) {
// 	if total == 0 {
// 		return
// 	}

// 	label := "Improved"
// 	color := ansiGreen
// 	icon := "✓"
// 	if improved <= total/2 {
// 		label = "Regressed"
// 		color = ansiRed
// 		icon = "✗"
// 	}

// 	detail := fmt.Sprintf("%d of %d key metrics improved", improved, total)

// 	// Render as a banner line with background tint
// 	fmt.Println()
// 	fmt.Printf("  %s %s%s %s%s  %s%s%s\n",
// 		color+icon+ansiReset,
// 		ansiBold+color, label, ansiReset,
// 		ansiDim+"─"+ansiReset,
// 		ansiDim, detail, ansiReset)
// 	fmt.Println()
// }

// // ── Print: Section Header ──────────────────────────────────────────────────────

// func printSectionHeader(title, iconColor, badge string) {
// 	const lineWidth = 62
// 	icon := iconColor + "▍" + ansiReset + " "
// 	t := ansiBold + title + ansiReset

// 	used := len(title) + 3 // icon(2) + space(1)
// 	if badge != "" {
// 		used += len(badge) + 3 // space + badge + space
// 	}
// 	lineLen := lineWidth - used
// 	if lineLen < 1 {
// 		lineLen = 1
// 	}

// 	line := "  " + icon + t
// 	if badge != "" {
// 		line += " " + ansiDim + strings.Repeat("─", lineLen) + " " + badge + ansiReset
// 	} else {
// 		line += " " + ansiDim + strings.Repeat("─", lineLen) + ansiReset
// 	}
// 	fmt.Println(line)
// }

// // ── Print: Key Metric Cards ────────────────────────────────────────────────────
// //
// // Layout: 4 cards side-by-side, each 15 inner chars, box-drawn borders.
// //
// //   ┌───────────────┐ ┌───────────────┐ ┌───────────────┐ ┌───────────────┐
// //   │ TTFT          │ │ Req Latency   │ │ Throughput     │ │ Duration      │
// //   │ 7.62s         │ │ 37.02s        │ │ 355.2         │ │ 54.5s         │
// //   │ ▼ +9.4%       │ │ ▲ -3.9%       │ │ ▲ +39.5%      │ │ ▲ -34.2%      │
// //   └───────────────┘ └───────────────┘ └───────────────┘ └───────────────┘

// func printKeyMetricsCards(rowMap map[string]history.DiffRow) {
// 	const inner = 15 // chars inside each card border
// 	const pad = 13   // usable after 1-char left + 1-char right padding

// 	type card struct {
// 		label string
// 		val   string
// 		chStr string
// 		color string
// 	}

// 	cards := make([]card, len(keyMetrics))
// 	for i, km := range keyMetrics {
// 		r, ok := rowMap[km.path]
// 		cards[i].label = km.label
// 		if !ok {
// 			cards[i].val = "—"
// 			cards[i].chStr = "—"
// 			cards[i].color = ansiDim
// 			continue
// 		}
// 		cards[i].val = formatVal(r.After, km.unit)
// 		improved := (km.lowerBetter && r.PctChg < 0) || (!km.lowerBetter && r.PctChg > 0)
// 		cards[i].color = ansiGreen
// 		arrow := "▲"
// 		if !improved {
// 			cards[i].color = ansiRed
// 			arrow = "▼"
// 		}
// 		cards[i].chStr = fmt.Sprintf("%s %s", arrow, fmtPct(r.PctChg))
// 	}

// 	seg := strings.Repeat("─", inner)

// 	// Build each row across all 4 cards
// 	renderRow := func(fn func(c card) string) string {
// 		line := "  "
// 		for i, c := range cards {
// 			if i > 0 {
// 				line += " "
// 			}
// 			line += "│" + fn(c) + "│"
// 		}
// 		return line
// 	}

// 	// Top border
// 	top := "  "
// 	bot := "  "
// 	for i := range cards {
// 		if i > 0 {
// 			top += " "
// 			bot += " "
// 		}
// 		top += "┌" + seg + "┐"
// 		bot += "└" + seg + "┘"
// 	}

// 	fmt.Println()
// 	fmt.Println(top)

// 	// Row 1: Label (dim)
// 	fmt.Println(renderRow(func(c card) string {
// 		return " " + ansiDim + padRight(c.label, pad) + ansiReset + " "
// 	}))

// 	// Row 2: Value (bold + colored)
// 	fmt.Println(renderRow(func(c card) string {
// 		return " " + ansiBold + c.color + padRight(c.val, pad) + ansiReset + " "
// 	}))

// 	// Row 3: Change arrow + percentage
// 	fmt.Println(renderRow(func(c card) string {
// 		return " " + c.color + padUnicode(c.chStr, pad) + ansiReset + " "
// 	}))

// 	fmt.Println(bot)
// 	fmt.Println()
// }

// // ── Print: Detailed Comparison Table ───────────────────────────────────────────
// //
// // ┌──────────────────────────────┬──────────┬──────────┬──────────────┐
// // │ Metric                       │    Run A │    Run B │       Change │
// // ├──────────────────────────────┼──────────┼──────────┼──────────────┤
// // │ Time to first token          │   6.96s  │   7.62s  │ ██░░░  +9.4% │
// // │ Request latency              │  38.53s  │  37.02s  │ █░░░░  -3.9% │
// // └──────────────────────────────┴──────────┴──────────┴──────────────┘

// func printDetailedTable(rowMap map[string]history.DiffRow) {
// 	const mW = 28 // metric column visible width
// 	const vW = 9  // value column visible width
// 	const barW = 5
// 	const pctW = 7
// 	const cW = barW + 1 + pctW // change column = 13

// 	mkLine := func(l, mi, r string) string {
// 		return "  " + l +
// 			strings.Repeat("─", mW+2) + mi +
// 			strings.Repeat("─", vW+2) + mi +
// 			strings.Repeat("─", vW+2) + mi +
// 			strings.Repeat("─", cW+2) + r
// 	}

// 	top := mkLine("┌", "┬", "┐")
// 	sep := mkLine("├", "┼", "┤")
// 	bot := mkLine("└", "┴", "┘")

// 	hdr := fmt.Sprintf("  │ %-*s │ %*s │ %*s │ %*s │",
// 		mW, ansiDim+"Metric"+ansiReset+strings.Repeat(" ", mW-6),
// 		vW, "Run A",
// 		vW, "Run B",
// 		cW, "Change")

// 	fmt.Println()
// 	fmt.Println(top)
// 	fmt.Println(hdr)
// 	fmt.Println(sep)

// 	printed := false
// 	for _, dm := range detailMetrics {
// 		r, ok := rowMap[dm.prefix+".avg"]
// 		if !ok {
// 			continue
// 		}
// 		lbl := dm.label
// 		if len(lbl) > mW {
// 			lbl = lbl[:mW-1] + "…"
// 		}

// 		aStr := formatVal(r.Before, dm.unit)
// 		bStr := formatVal(r.After, dm.unit)

// 		improved := (lowerIsBetter[dm.prefix] && r.PctChg < 0) || (!lowerIsBetter[dm.prefix] && r.PctChg > 0)
// 		color := ansiGreen
// 		if !improved {
// 			color = ansiRed
// 		}
// 		if math.Abs(r.PctChg) < 0.5 {
// 			color = ansiDim
// 		}

// 		bar := sparkBar(r.PctChg, lowerIsBetter[dm.prefix], barW)
// 		pStr := fmtPct(r.PctChg)
// 		changeCell := bar + " " + color + fmt.Sprintf("%*s", pctW, pStr) + ansiReset

// 		fmt.Printf("  │ %-*s │ %*s │ %s%*s%s │ %s │\n",
// 			mW, lbl,
// 			vW, aStr,
// 			color, vW, bStr, ansiReset,
// 			changeCell)
// 		printed = true
// 	}

// 	if !printed {
// 		totalW := (mW + 2) + (vW+2)*2 + (cW + 2) + 3
// 		fmt.Println("  │" + center("(no data)", totalW) + "│")
// 	}
// 	fmt.Println(bot)
// 	fmt.Println()
// }

// // ── Print: Percentile Distribution ─────────────────────────────────────────────
// //
// // ┌──────────────────────┬────────┬────────┬────────┬────────┬────────┬────────┐
// // │ Metric               │   avg  │   p50  │   p90  │   p99  │   min  │   max  │
// // ├──────────────────────┼────────┼────────┼────────┼────────┼────────┼────────┤
// // │ TTFT                 │  +9.4% │  +9.1% │  +7.6% │  +4.4% │ +55.8% │  +3.8% │
// // └──────────────────────┴────────┴────────┴────────┴────────┴────────┴────────┘

// func printPercentileTable(rowMap map[string]history.DiffRow) {
// 	const mW = 18
// 	const cW = 7
// 	nCols := len(statCols)

// 	mkLine := func(l, mi, r string) string {
// 		s := "  " + l + strings.Repeat("─", mW+2)
// 		for range nCols {
// 			s += mi + strings.Repeat("─", cW+2)
// 		}
// 		return s + r
// 	}

// 	top := mkLine("┌", "┬", "┐")
// 	sep := mkLine("├", "┼", "┤")
// 	bot := mkLine("└", "┴", "┘")

// 	hdr := fmt.Sprintf("  │ %-*s", mW, ansiDim+"Metric"+ansiReset+strings.Repeat(" ", mW-6))
// 	for _, col := range statCols {
// 		hdr += " │" + center(col, cW+1)
// 	}
// 	hdr += " │"

// 	fmt.Println()
// 	fmt.Println(top)
// 	fmt.Println(hdr)
// 	fmt.Println(sep)

// 	printed := false
// 	for _, pm := range pctMetrics {
// 		hasAny := false
// 		for _, stat := range statCols {
// 			r, ok := rowMap[pm.prefix+"."+stat]
// 			if ok && math.Abs(r.PctChg) >= 0.05 {
// 				hasAny = true
// 				break
// 			}
// 		}
// 		if !hasAny {
// 			continue
// 		}

// 		lbl := pm.label
// 		if len(lbl) > mW {
// 			lbl = lbl[:mW-1] + "…"
// 		}

// 		row := fmt.Sprintf("  │ %-*s", mW, lbl)
// 		for _, stat := range statCols {
// 			r, ok := rowMap[pm.prefix+"."+stat]
// 			if !ok || math.Abs(r.PctChg) < 0.05 {
// 				row += " │" + dashInCell(cW+1)
// 			} else {
// 				row += " │" + pctCell(r.PctChg, lowerIsBetter[pm.prefix], cW+1)
// 			}
// 		}
// 		row += " │"
// 		fmt.Println(row)
// 		printed = true
// 	}

// 	if !printed {
// 		totalW := mW + 2 + nCols*(cW+3)
// 		fmt.Println("  │" + center("(no data)", totalW) + "│")
// 	}
// 	fmt.Println(bot)
// 	fmt.Println()
// }

// // ── Cell helpers ───────────────────────────────────────────────────────────────

// // sparkBar renders a colored mini-bar of visual width w based on percentage.
// //
// //	████░  (green = improved, red = regressed)
// func sparkBar(pct float64, lowerBetter bool, w int) string {
// 	improved := (lowerBetter && pct < 0) || (!lowerBetter && pct > 0)
// 	filled := min(int(math.Round(math.Abs(pct)/50.0*float64(w))), w)
// 	if filled == 0 && math.Abs(pct) >= 0.5 {
// 		filled = 1
// 	}

// 	color := ansiGreen
// 	if !improved {
// 		color = ansiRed
// 	}
// 	if math.Abs(pct) < 0.5 {
// 		color = ansiDim
// 	}

// 	return color + strings.Repeat("█", filled) + ansiDim + strings.Repeat("░", w-filled) + ansiReset
// }

// // pctCell returns a right-aligned colored percentage in a cell of visible width w.
// func pctCell(pct float64, lowerBetter bool, w int) string {
// 	raw := fmtPct(pct)
// 	improved := (lowerBetter && pct < 0) || (!lowerBetter && pct > 0)
// 	regressed := (lowerBetter && pct > 0) || (!lowerBetter && pct < 0)

// 	var color string
// 	switch {
// 	case math.Abs(pct) < 0.5:
// 		color = ansiDim
// 	case improved:
// 		color = ansiGreen
// 	case regressed:
// 		color = ansiRed
// 	default:
// 		color = ansiDim
// 	}

// 	padLeft := max(w-len(raw), 1)
// 	return strings.Repeat(" ", padLeft) + color + raw + ansiReset
// }

// // dashInCell returns a centered dim "—" in a cell of visible width w.
// func dashInCell(w int) string {
// 	left := (w - 1) / 2
// 	right := w - 1 - left
// 	return strings.Repeat(" ", left) + ansiDim + "—" + ansiReset + strings.Repeat(" ", right)
// }

// // fmtPct formats a percentage with sign prefix.
// // Drops decimal for |pct| >= 100 to save width.
// func fmtPct(pct float64) string {
// 	if math.Abs(pct) >= 100 {
// 		if pct > 0 {
// 			return fmt.Sprintf("+%.0f%%", pct)
// 		}
// 		return fmt.Sprintf("%.0f%%", pct)
// 	}
// 	sign := "+"
// 	if pct < 0 {
// 		sign = ""
// 	}
// 	return fmt.Sprintf("%s%.1f%%", sign, pct)
// }

// // formatVal renders a float in human-readable form for the given unit.
// func formatVal(v float64, unit string) string {
// 	switch unit {
// 	case "ms":
// 		if math.Abs(v) >= 1000 {
// 			return fmt.Sprintf("%.2fs", v/1000)
// 		}
// 		return fmt.Sprintf("%.1fms", v)
// 	case "s":
// 		return fmt.Sprintf("%.1fs", v)
// 	case "t/s", "t/s/u":
// 		return fmt.Sprintf("%.1f", v)
// 	case "tokens":
// 		return fmt.Sprintf("%.0f", v)
// 	case "req/s":
// 		return fmt.Sprintf("%.2f", v)
// 	default:
// 		return fmt.Sprintf("%.2f", v)
// 	}
// }

// // ── String helpers ─────────────────────────────────────────────────────────────

// // padRight pads s with spaces to exactly w visible characters.
// func padRight(s string, w int) string {
// 	n := len(s)
// 	if n >= w {
// 		return s[:w]
// 	}
// 	return s + strings.Repeat(" ", w-n)
// }

// // padUnicode pads a string that may contain multi-byte runes (▲/▼).
// // Assumes each rune occupies 1 terminal column (true for narrow BMP symbols).
// func padUnicode(s string, w int) string {
// 	runes := []rune(s)
// 	cols := len(runes)
// 	if cols >= w {
// 		return string(runes[:w])
// 	}
// 	return s + strings.Repeat(" ", w-cols)
// }

// // center returns s centered within a field of visible width w.
// func center(s string, w int) string {
// 	n := len(s)
// 	if n >= w {
// 		return s
// 	}
// 	total := w - n
// 	left := total / 2
// 	return strings.Repeat(" ", left) + s + strings.Repeat(" ", total-left)
// }

// // package cmd

// // import (
// // 	"encoding/json"
// // 	"fmt"
// // 	"math"
// // 	"sort"
// // 	"strings"

// // 	"github.com/spf13/cobra"

// // 	"github.com/RashRAJ/all-bench/history"
// // )

// // const (
// // 	ansiReset   = "\033[0m"
// // 	ansiRed     = "\033[31m"
// // 	ansiGreen   = "\033[32m"
// // 	ansiYellow  = "\033[33m"
// // 	ansiBlue    = "\033[34m"
// // 	ansiMagenta = "\033[35m"
// // 	ansiCyan    = "\033[36m"
// // 	ansiBold    = "\033[1m"
// // 	ansiDim     = "\033[2m"
// // 	ansiStrike  = "\033[9m"
// // )

// // var keyMetrics = []struct {
// // 	path        string
// // 	label       string
// // 	unit        string
// // 	lowerBetter bool
// // }{
// // 	{"time_to_first_token.avg", "TTFT", "ms", true},
// // 	{"request_latency.avg", "Req latency", "ms", true},
// // 	{"output_token_throughput.avg", "Throughput", "t/s", false},
// // 	{"benchmark_duration.avg", "Duration", "s", true},
// // }

// // var verdictMetrics = []struct {
// // 	path        string
// // 	lowerBetter bool
// // }{
// // 	{"time_to_first_token.avg", true},
// // 	{"time_to_second_token.avg", true},
// // 	{"request_latency.avg", true},
// // 	{"inter_token_latency.avg", true},
// // 	{"output_token_throughput_per_user.avg", false},
// // 	{"output_token_throughput.avg", false},
// // 	{"request_throughput.avg", false},
// // 	{"output_sequence_length.avg", false},
// // }

// // var lowerIsBetter = map[string]bool{
// // 	"time_to_first_token":          true,
// // 	"time_to_second_token":         true,
// // 	"time_to_first_output_token":   true,
// // 	"request_latency":              true,
// // 	"inter_token_latency":          true,
// // 	"inter_chunk_latency":          true,
// // 	"http_req_duration":            true,
// // 	"http_req_waiting":             true,
// // 	"http_req_receiving":           true,
// // 	"http_req_sending":             true,
// // 	"http_req_connecting":          true,
// // 	"http_req_connection_overhead": true,
// // 	"http_req_dns_lookup":          true,
// // 	"benchmark_duration":           true,
// // }

// // var detailMetrics = []struct {
// // 	prefix      string
// // 	label       string
// // 	unit        string
// // }{
// // 	{"time_to_first_token", "Time to first token (ms)", "ms"},
// // 	{"time_to_second_token", "Time to second token (ms)", "ms"},
// // 	{"request_latency", "Request latency (ms)", "ms"},
// // 	{"inter_token_latency", "Inter token latency (ms)", "ms"},
// // 	{"output_token_throughput_per_user", "Output throughput/user (t/s/u)", "t/s/u"},
// // 	{"output_token_throughput", "Output token throughput (t/s)", "t/s"},
// // 	{"request_throughput", "Request throughput (req/s)", "req/s"},
// // 	{"output_sequence_length", "Output seq length (tokens)", "tokens"},
// // }

// // var pctMetrics = []struct {
// // 	prefix string
// // 	label  string
// // }{
// // 	{"time_to_first_token", "TTFT (ms)"},
// // 	{"time_to_second_token", "TTST (ms)"},
// // 	{"request_latency", "Req latency (ms)"},
// // 	{"inter_token_latency", "ITL (ms)"},
// // 	{"inter_chunk_latency", "Chunk latency (ms)"},
// // 	{"output_token_throughput_per_user", "Throughput/user"},
// // 	{"prefill_throughput_per_user", "Prefill tput/user"},
// // 	{"http_req_duration", "HTTP duration (ms)"},
// // }

// // var statCols = []string{"avg", "p50", "p90", "p99", "min", "max"}

// // var diffCmd = &cobra.Command{
// // 	Use:   "diff",
// // 	Short: "Compare the last two benchmark runs",
// // 	RunE:  runDiff,
// // }

// // func init() {
// // 	rootCmd.AddCommand(diffCmd)
// // }

// // func runDiff(cmd *cobra.Command, args []string) error {
// // 	snaps, err := history.LoadLast(2)
// // 	if err != nil {
// // 		return fmt.Errorf("loading history: %w", err)
// // 	}
// // 	if len(snaps) < 2 {
// // 		fmt.Println("  Not enough history — run all-bench run at least twice first.")
// // 		return nil
// // 	}

// // 	a, b := snaps[0], snaps[1]

// // 	tsA := a.Timestamp.Local().Format("2006-01-02 15:04")
// // 	tsB := b.Timestamp.Local().Format("15:04")

// // 	cfgChanges := history.DiffConfig(a.Config, b.Config)
// // 	cfgInfo := extractConfigInfo(b.Config)

// // 	seen := make(map[string]bool)
// // 	var runnerNames []string
// // 	for k := range a.RawOutputs {
// // 		if !seen[k] {
// // 			seen[k] = true
// // 			runnerNames = append(runnerNames, k)
// // 		}
// // 	}
// // 	for k := range b.RawOutputs {
// // 		if !seen[k] {
// // 			seen[k] = true
// // 			runnerNames = append(runnerNames, k)
// // 		}
// // 	}
// // 	sort.Strings(runnerNames)

// // 	type runnerSection struct {
// // 		label  string
// // 		rowMap map[string]history.DiffRow
// // 	}
// // 	var sections []runnerSection

// // 	for _, rName := range runnerNames {
// // 		aRaws := a.RawOutputs[rName]
// // 		bRaws := b.RawOutputs[rName]
// // 		if len(aRaws) == 0 || len(bRaws) == 0 {
// // 			continue
// // 		}
// // 		pairs := min(len(aRaws), len(bRaws))
// // 		for i := range pairs {
// // 			rows := history.DiffRaw(aRaws[i], bRaws[i])
// // 			rowMap := make(map[string]history.DiffRow, len(rows))
// // 			for _, r := range rows {
// // 				rowMap[r.Path] = r
// // 			}
// // 			lbl := strings.ToUpper(rName)
// // 			if pairs > 1 {
// // 				lbl = fmt.Sprintf("%s [level %d]", lbl, i+1)
// // 			}
// // 			sections = append(sections, runnerSection{lbl, rowMap})
// // 		}
// // 	}

// // 	printHeader()

// // 	var durRow *history.DiffRow
// // 	if len(sections) > 0 {
// // 		if r, ok := sections[0].rowMap["benchmark_duration.avg"]; ok {
// // 			durRow = &r
// // 		}
// // 	}
// // 	printRunInfo(cfgInfo, tsA, tsB, durRow)

// // 	if len(cfgChanges) > 0 {
// // 		printSectionHeader("Config changes", ansiCyan, "")
// // 		fmt.Println()
// // 		for _, ch := range cfgChanges {
// // 			fmt.Printf("    %s%-40s%s  %s%s%v%s%s  →  %s%v%s\n",
// // 				ansiCyan, ch[0], ansiReset,
// // 				ansiRed, ansiStrike, ch[1], "\033[29m", ansiReset,
// // 				ansiGreen+ansiBold, ch[2], ansiReset)
// // 		}
// // 		fmt.Println()
// // 	}

// // 	for _, sec := range sections {
// // 		imp, total := countImproved(sec.rowMap)
// // 		printVerdict(imp, total)

// // 		printSectionHeader("Key metrics", ansiBlue, sec.label)
// // 		printKeyMetricsCards(sec.rowMap)

// // 		printSectionHeader("Detailed comparison", ansiMagenta, "Run A → Run B")
// // 		printDetailedTable(sec.rowMap)

// // 		printSectionHeader("Percentile distribution", ansiYellow, "Change %")
// // 		printPercentileTable(sec.rowMap)
// // 	}

// // 	printFooter(tsA, tsB)
// // 	return nil
// // }

// // func extractConfigInfo(raw json.RawMessage) map[string]string {
// // 	var cfg map[string]interface{}
// // 	if err := json.Unmarshal(raw, &cfg); err != nil {
// // 		return nil
// // 	}
// // 	info := make(map[string]string)
// // 	if defaults, ok := cfg["defaults"].(map[string]interface{}); ok {
// // 		if m, ok := defaults["model"].(string); ok {
// // 			parts := strings.Split(m, "/")
// // 			info["profile"] = parts[len(parts)-1]
// // 		}
// // 	}
// // 	if runners, ok := cfg["runners"].([]interface{}); ok && len(runners) > 0 {
// // 		names := make([]string, 0, len(runners))
// // 		for _, r := range runners {
// // 			if s, ok := r.(string); ok {
// // 				names = append(names, s)
// // 			}
// // 		}
// // 		info["runners"] = strings.Join(names, ", ")
// // 	}
// // 	return info
// // }

// // func countImproved(rowMap map[string]history.DiffRow) (improved, total int) {
// // 	for _, vm := range verdictMetrics {
// // 		r, ok := rowMap[vm.path]
// // 		if !ok {
// // 			continue
// // 		}
// // 		total++
// // 		if (vm.lowerBetter && r.PctChg < -0.5) || (!vm.lowerBetter && r.PctChg > 0.5) {
// // 			improved++
// // 		}
// // 	}
// // 	return
// // }

// // // ── Print functions ────────────────────────────────────────────────────────────

// // func printHeader() {
// // 	fmt.Println()
// // 	fmt.Printf("  %s%sALL-bench%s  %sv1.0.0%s\n",
// // 		ansiBold, ansiCyan, ansiReset, ansiDim, ansiReset)
// // 	fmt.Printf("  %sUnified AI inference benchmarking  %s|%s  vLLM + AIPerf + GPU telemetry%s\n\n",
// // 		ansiDim, ansiReset+ansiDim, ansiReset+ansiDim, ansiReset)
// // }

// // func printRunInfo(info map[string]string, tsA, tsB string, dur *history.DiffRow) {
// // 	sep := fmt.Sprintf("  %s·%s ", ansiDim, ansiReset)
// // 	var parts []string
// // 	if p := info["profile"]; p != "" {
// // 		parts = append(parts, fmt.Sprintf("%sProfile%s  %s", ansiDim, ansiReset, p))
// // 	}
// // 	if r := info["runners"]; r != "" {
// // 		parts = append(parts, fmt.Sprintf("%sRunners%s  %s", ansiDim, ansiReset, r))
// // 	}
// // 	if dur != nil {
// // 		color := ansiGreen
// // 		if dur.PctChg > 0 {
// // 			color = ansiRed
// // 		}
// // 		parts = append(parts, fmt.Sprintf(
// // 			"%sDuration%s  %s%s%s → %s%s%s  %s%+.1f%%%s",
// // 			ansiDim, ansiReset,
// // 			ansiDim, formatVal(dur.Before, "s"), ansiReset,
// // 			ansiBold, formatVal(dur.After, "s"), ansiReset,
// // 			color, dur.PctChg, ansiReset,
// // 		))
// // 	}
// // 	fmt.Println("  " + strings.Join(parts, sep))
// // 	fmt.Println()
// // }

// // func printSectionHeader(title, iconColor, badge string) {
// // 	const lineWidth = 58
// // 	icon := iconColor + "▌" + ansiReset + " "
// // 	t := ansiBold + title + ansiReset
// // 	badgeLen := len(badge)
// // 	lineLen := lineWidth - len(title) - 2
// // 	if badgeLen > 0 {
// // 		lineLen -= badgeLen + 1
// // 	}
// // 	if lineLen < 1 {
// // 		lineLen = 1
// // 	}
// // 	line := "  " + icon + t + " "
// // 	if badge != "" {
// // 		line += ansiDim + strings.Repeat("─", lineLen) + " " + badge + ansiReset
// // 	} else {
// // 		line += ansiDim + strings.Repeat("─", lineLen) + ansiReset
// // 	}
// // 	fmt.Println(line)
// // }

// // func printVerdict(improved, total int) {
// // 	if total == 0 {
// // 		return
// // 	}
// // 	overall := "Improved"
// // 	color := ansiGreen
// // 	if improved <= total/2 {
// // 		overall = "Regressed"
// // 		color = ansiRed
// // 	}
// // 	detail := fmt.Sprintf("  %d of %d key metrics improved", improved, total)
// // 	fmt.Printf("\n  %s◆%s  %s%s%-12s%s%s%s%s\n\n",
// // 		color, ansiReset,
// // 		ansiBold, color, overall, ansiReset,
// // 		ansiDim, detail, ansiReset)
// // }

// // // printKeyMetricsCards renders 4 side-by-side metric cards.
// // func printKeyMetricsCards(rowMap map[string]history.DiffRow) {
// // 	// Each card: │ + 14 inner chars + │ = 16 wide. 4 cards + 3 gaps = 67 + 2 indent = 69.
// // 	const inner = 14  // chars between card borders
// // 	const usable = 12 // inner - 2 padding spaces

// // 	type card struct {
// // 		label  string
// // 		val    string
// // 		chStr  string // e.g. "▲ -3.9%"
// // 		color  string
// // 		hasVal bool
// // 	}

// // 	cards := make([]card, len(keyMetrics))
// // 	for i, km := range keyMetrics {
// // 		r, ok := rowMap[km.path]
// // 		cards[i].label = km.label
// // 		if !ok {
// // 			cards[i].val = "—"
// // 			cards[i].chStr = "—"
// // 			cards[i].color = ansiDim
// // 			continue
// // 		}
// // 		cards[i].hasVal = true
// // 		cards[i].val = formatVal(r.After, km.unit)
// // 		improved := (km.lowerBetter && r.PctChg < 0) || (!km.lowerBetter && r.PctChg > 0)
// // 		cards[i].color = ansiGreen
// // 		arrow := "▲"
// // 		if !improved {
// // 			cards[i].color = ansiRed
// // 			arrow = "▼"
// // 		}
// // 		sign := "+"
// // 		if r.PctChg < 0 {
// // 			sign = ""
// // 		}
// // 		cards[i].chStr = fmt.Sprintf("%s %s%.1f%%", arrow, sign, r.PctChg)
// // 	}

// // 	bld := func(rows ...func(c card) string) []string {
// // 		lines := make([]string, len(rows))
// // 		for ri, rowFn := range rows {
// // 			line := "  "
// // 			for i, c := range cards {
// // 				line += "│" + rowFn(c)
// // 				if i < len(cards)-1 {
// // 					line += " "
// // 				}
// // 			}
// // 			line += "│"
// // 			lines[ri] = line
// // 		}
// // 		return lines
// // 	}

// // 	border := "─"
// // 	top := "  " + strings.Join(strings.Fields(strings.Repeat("┌"+strings.Repeat(border, inner)+"┐ ", len(keyMetrics))), " ")
// // 	// Rebuild top/bot properly
// // 	top = "  "
// // 	bot := "  "
// // 	for i := range cards {
// // 		top += "┌" + strings.Repeat(border, inner) + "┐"
// // 		bot += "└" + strings.Repeat(border, inner) + "┘"
// // 		if i < len(cards)-1 {
// // 			top += " "
// // 			bot += " "
// // 		}
// // 	}

// // 	rows := bld(
// // 		func(c card) string {
// // 			return " " + ansiDim + padRight(c.label, usable) + ansiReset + " "
// // 		},
// // 		func(c card) string {
// // 			return " " + ansiBold + c.color + padRight(c.val, usable) + ansiReset + " "
// // 		},
// // 		func(c card) string {
// // 			// chStr contains a unicode arrow (▲/▼, 3 bytes but 1 col wide).
// // 			// Pad the pct portion to fill usable cols: arrow(1) + space(1) + pct + padding.
// // 			return " " + c.color + padUnicode(c.chStr, usable) + ansiReset + " "
// // 		},
// // 	)

// // 	fmt.Println()
// // 	fmt.Println(top)
// // 	for _, r := range rows {
// // 		fmt.Println(r)
// // 	}
// // 	fmt.Println(bot)
// // 	fmt.Println()
// // }

// // // padRight pads s to exactly w visible chars (ASCII only, safe for left-padded cells).
// // func padRight(s string, w int) string {
// // 	if len(s) >= w {
// // 		return s[:w]
// // 	}
// // 	return s + strings.Repeat(" ", w-len(s))
// // }

// // // padUnicode pads a string that may start with a multi-byte unicode rune (▲/▼ = 3 bytes, 1 col).
// // // It counts rune columns correctly for the arrow + ASCII rest.
// // func padUnicode(s string, w int) string {
// // 	runes := []rune(s)
// // 	visibleCols := len(runes) // each rune is 1 column for narrow BMP chars
// // 	if visibleCols >= w {
// // 		return string(runes[:w])
// // 	}
// // 	return s + strings.Repeat(" ", w-visibleCols)
// // }

// // func printDetailedTable(rowMap map[string]history.DiffRow) {
// // 	const mW = 30 // metric label visible width
// // 	const vW = 8  // Run A / Run B column width
// // 	const barW = 5
// // 	const pctW = 7
// // 	const cW = barW + 1 + pctW // change column: 13

// // 	mkLine := func(l, mi, r string) string {
// // 		return "  " + l +
// // 			strings.Repeat("─", mW+2) + mi +
// // 			strings.Repeat("─", vW+2) + mi +
// // 			strings.Repeat("─", vW+2) + mi +
// // 			strings.Repeat("─", cW+2) + r
// // 	}

// // 	top := mkLine("┌", "┬", "┐")
// // 	sep := mkLine("├", "┼", "┤")
// // 	bot := mkLine("└", "┴", "┘")
// // 	hdr := fmt.Sprintf("  │ %s%-*s%s │ %*s │ %*s │ %*s │",
// // 		ansiDim, mW, "Metric", ansiReset,
// // 		vW, "Run A", vW, "Run B", cW, "Change")

// // 	fmt.Println()
// // 	fmt.Println(top)
// // 	fmt.Println(hdr)
// // 	fmt.Println(sep)

// // 	printed := false
// // 	for _, dm := range detailMetrics {
// // 		r, ok := rowMap[dm.prefix+".avg"]
// // 		if !ok {
// // 			continue
// // 		}
// // 		lbl := dm.label
// // 		if len(lbl) > mW {
// // 			lbl = lbl[:mW-1] + "…"
// // 		}

// // 		aStr := formatVal(r.Before, dm.unit)
// // 		bStr := formatVal(r.After, dm.unit)

// // 		improved := (lowerIsBetter[dm.prefix] && r.PctChg < 0) || (!lowerIsBetter[dm.prefix] && r.PctChg > 0)
// // 		color := ansiGreen
// // 		if !improved {
// // 			color = ansiRed
// // 		}
// // 		if math.Abs(r.PctChg) < 0.5 {
// // 			color = ansiDim
// // 		}

// // 		bar := sparkBar(r.PctChg, lowerIsBetter[dm.prefix], barW)
// // 		pStr := fmtPct(r.PctChg)
// // 		changeCell := bar + " " + color + fmt.Sprintf("%*s", pctW, pStr) + ansiReset

// // 		fmt.Printf("  │ %-*s │ %*s │ %s%*s%s │ %s │\n",
// // 			mW, lbl,
// // 			vW, aStr,
// // 			color, vW, bStr, ansiReset,
// // 			changeCell)
// // 		printed = true
// // 	}

// // 	if !printed {
// // 		fmt.Println("  │" + center("(no changes)", (mW+2)+(vW+2)*2+(cW+2)+3) + "│")
// // 	}
// // 	fmt.Println(bot)
// // 	fmt.Println()
// // }

// // func printPercentileTable(rowMap map[string]history.DiffRow) {
// // 	const mW = 20
// // 	const cW = 6
// // 	nCols := len(statCols)

// // 	mkLine := func(l, mi, r string) string {
// // 		s := "  " + l + strings.Repeat("─", mW+2)
// // 		for range nCols {
// // 			s += mi + strings.Repeat("─", cW+2)
// // 		}
// // 		return s + r
// // 	}

// // 	top := mkLine("┌", "┬", "┐")
// // 	sep := mkLine("├", "┼", "┤")
// // 	bot := mkLine("└", "┴", "┘")

// // 	hdr := fmt.Sprintf("  │ %s%-*s%s ", ansiDim, mW, "Metric", ansiReset)
// // 	for _, col := range statCols {
// // 		hdr += "│" + center(col, cW+2)
// // 	}
// // 	hdr += "│"

// // 	fmt.Println()
// // 	fmt.Println(top)
// // 	fmt.Println(hdr)
// // 	fmt.Println(sep)

// // 	printed := false
// // 	for _, pm := range pctMetrics {
// // 		hasAny := false
// // 		for _, stat := range statCols {
// // 			r, ok := rowMap[pm.prefix+"."+stat]
// // 			if ok && math.Abs(r.PctChg) >= 0.05 {
// // 				hasAny = true
// // 				break
// // 			}
// // 		}
// // 		if !hasAny {
// // 			continue
// // 		}

// // 		lbl := pm.label
// // 		if len(lbl) > mW {
// // 			lbl = lbl[:mW-1] + "…"
// // 		}

// // 		row := fmt.Sprintf("  │ %-*s ", mW, lbl)
// // 		for _, stat := range statCols {
// // 			r, ok := rowMap[pm.prefix+"."+stat]
// // 			if !ok || math.Abs(r.PctChg) < 0.05 {
// // 				row += "│" + dashInCell(cW+2)
// // 			} else {
// // 				row += "│" + pctCell(r.PctChg, lowerIsBetter[pm.prefix], cW+2)
// // 			}
// // 		}
// // 		row += "│"
// // 		fmt.Println(row)
// // 		printed = true
// // 	}

// // 	if !printed {
// // 		fmt.Println("  │" + center("(no changes)", mW+2+nCols*(cW+3)) + "│")
// // 	}
// // 	fmt.Println(bot)
// // 	fmt.Println()
// // }

// // func printFooter(tsA, tsB string) {
// // 	dot := ansiGreen + "●" + ansiReset
// // 	fmt.Printf("  %s %sBenchmark completed%s    %s → %s  %s|%s  allbench diff\n\n",
// // 		dot, ansiDim, ansiReset,
// // 		tsA, tsB,
// // 		ansiDim, ansiReset)
// // }

// // // ── Cell helpers ───────────────────────────────────────────────────────────────

// // // sparkBar renders a colored mini-bar of visual width w based on percentage magnitude.
// // func sparkBar(pct float64, lowerBetter bool, w int) string {
// // 	improved := (lowerBetter && pct < 0) || (!lowerBetter && pct > 0)
// // 	filled := min(int(math.Round(math.Abs(pct)/50.0*float64(w))), w)
// // 	if filled == 0 && math.Abs(pct) >= 0.5 {
// // 		filled = 1
// // 	}
// // 	color := ansiGreen
// // 	if !improved {
// // 		color = ansiRed
// // 	}
// // 	if math.Abs(pct) < 0.5 {
// // 		color = ansiDim
// // 	}
// // 	return color + strings.Repeat("█", filled) + ansiDim + strings.Repeat("░", w-filled) + ansiReset
// // }

// // // pctCell returns a right-aligned colored percentage in a cell of visible width w.
// // func pctCell(pct float64, lowerBetter bool, w int) string {
// // 	raw := fmtPct(pct)
// // 	improved := (lowerBetter && pct < 0) || (!lowerBetter && pct > 0)
// // 	regressed := (lowerBetter && pct > 0) || (!lowerBetter && pct < 0)
// // 	var color string
// // 	switch {
// // 	case math.Abs(pct) < 0.5:
// // 		color = ansiDim
// // 	case improved:
// // 		color = ansiGreen
// // 	case regressed:
// // 		color = ansiRed
// // 	}
// // 	padLeft := max(w-len(raw)-1, 1)
// // 	return strings.Repeat(" ", padLeft) + color + raw + ansiReset + " "
// // }

// // // dashInCell returns a centered dim "—" of visible width w.
// // func dashInCell(w int) string {
// // 	left := (w - 1) / 2
// // 	right := w - 1 - left
// // 	return strings.Repeat(" ", left) + ansiDim + "—" + ansiReset + strings.Repeat(" ", right)
// // }

// // // fmtPct formats a percentage, dropping the decimal for values ≥ 100 to save width.
// // func fmtPct(pct float64) string {
// // 	if math.Abs(pct) >= 100 {
// // 		if pct > 0 {
// // 			return fmt.Sprintf("+%.0f%%", pct)
// // 		}
// // 		return fmt.Sprintf("%.0f%%", pct)
// // 	}
// // 	sign := "+"
// // 	if pct < 0 {
// // 		sign = ""
// // 	}
// // 	return fmt.Sprintf("%s%.1f%%", sign, pct)
// // }

// // // formatVal renders a raw float in a human-readable form for the given unit.
// // func formatVal(v float64, unit string) string {
// // 	switch unit {
// // 	case "ms":
// // 		if math.Abs(v) >= 1000 {
// // 			return fmt.Sprintf("%.2fs", v/1000)
// // 		}
// // 		return fmt.Sprintf("%.1fms", v)
// // 	case "s":
// // 		return fmt.Sprintf("%.1fs", v)
// // 	case "t/s", "t/s/u":
// // 		return fmt.Sprintf("%.1f", v)
// // 	case "tokens":
// // 		return fmt.Sprintf("%.0f", v)
// // 	case "req/s":
// // 		return fmt.Sprintf("%.2f", v)
// // 	default:
// // 		return fmt.Sprintf("%.2f", v)
// // 	}
// // }

// // // center returns s centered within a string of visible width w.
// // func center(s string, w int) string {
// // 	if len(s) >= w {
// // 		return s
// // 	}
// // 	total := w - len(s)
// // 	left := total / 2
// // 	return strings.Repeat(" ", left) + s + strings.Repeat(" ", total-left)
// // }
