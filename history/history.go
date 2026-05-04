package history

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RashRAJ/all-bench/config"
	"github.com/RashRAJ/all-bench/runner"
)

// Snapshot captures everything from one all-bench run: the config used,
// each runner's raw profiler JSON, and our normalized results.
type Snapshot struct {
	Timestamp  time.Time                    `json:"timestamp"`
	Config     json.RawMessage              `json:"config"`
	RawOutputs map[string][]json.RawMessage `json:"raw_outputs"`
	Results    []*runner.Result             `json:"results"`
}

func New(cfg *config.Config) *Snapshot {
	cfgJSON, _ := json.Marshal(cfg)
	return &Snapshot{
		Timestamp:  time.Now(),
		Config:     json.RawMessage(cfgJSON),
		RawOutputs: make(map[string][]json.RawMessage),
	}
}

func (s *Snapshot) AddRunner(name string, raws []json.RawMessage, results []*runner.Result) {
	s.RawOutputs[name] = raws
	s.Results = append(s.Results, results...)
}

func dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".all-bench", "history"), nil
}

func Save(snap *Snapshot) error {
	d, err := dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d, 0755); err != nil {
		return err
	}
	name := snap.Timestamp.UTC().Format("2006-01-02T15-04-05Z") + ".json"
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(d, name), data, 0644)
}

// LoadLast loads up to n snapshots, most recent last.
func LoadLast(n int) ([]*Snapshot, error) {
	d, err := dir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(d)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			files = append(files, filepath.Join(d, e.Name()))
		}
	}
	sort.Strings(files) // ISO filenames sort chronologically
	if len(files) > n {
		files = files[len(files)-n:]
	}

	snaps := make([]*Snapshot, 0, len(files))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var s Snapshot
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", f, err)
		}
		snaps = append(snaps, &s)
	}
	return snaps, nil
}

// --- diff logic ---

// DiffRow is one row in the metric diff table.
type DiffRow struct {
	Path   string
	Before float64
	After  float64
	PctChg float64
}

// DiffRaw compares two raw profiler JSON blobs and returns diff rows for every numeric field.
// It walks the full JSON tree so fields not in our Result struct are included.
func DiffRaw(before, after json.RawMessage) []DiffRow {
	a := flattenNumeric(before)
	b := flattenNumeric(after)

	keys := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		keys[k] = struct{}{}
	}
	for k := range b {
		keys[k] = struct{}{}
	}

	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	var rows []DiffRow
	for _, k := range sorted {
		va, hasA := a[k]
		vb, hasB := b[k]
		if !hasA || !hasB {
			continue
		}
		var pct float64
		if va != 0 {
			pct = (vb - va) / math.Abs(va) * 100
		}
		rows = append(rows, DiffRow{Path: k, Before: va, After: vb, PctChg: pct})
	}
	return rows
}

// DiffConfig compares two config JSON blobs and returns [path, before, after] for changed fields.
func DiffConfig(before, after json.RawMessage) [][3]string {
	a := flattenStrings(before)
	b := flattenStrings(after)

	keys := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		keys[k] = struct{}{}
	}
	for k := range b {
		keys[k] = struct{}{}
	}

	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	var changes [][3]string
	for _, k := range sorted {
		va, hasA := a[k]
		vb, hasB := b[k]
		switch {
		case hasA && hasB && va != vb:
			changes = append(changes, [3]string{k, va, vb})
		case hasA && !hasB:
			changes = append(changes, [3]string{k, va, "(removed)"})
		case !hasA && hasB:
			changes = append(changes, [3]string{k, "(added)", vb})
		}
	}
	return changes
}

func flattenNumeric(raw json.RawMessage) map[string]float64 {
	result := make(map[string]float64)
	var obj interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return result
	}
	walkNumeric("", obj, result)
	return result
}

func walkNumeric(prefix string, v interface{}, out map[string]float64) {
	switch val := v.(type) {
	case map[string]interface{}:
		for k, child := range val {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			walkNumeric(key, child, out)
		}
	case []interface{}:
		for i, child := range val {
			walkNumeric(fmt.Sprintf("%s[%d]", prefix, i), child, out)
		}
	case float64:
		out[prefix] = val
	}
}

func flattenStrings(raw json.RawMessage) map[string]string {
	result := make(map[string]string)
	var obj interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return result
	}
	walkStrings("", obj, result)
	return result
}

func walkStrings(prefix string, v interface{}, out map[string]string) {
	switch val := v.(type) {
	case map[string]interface{}:
		for k, child := range val {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			walkStrings(key, child, out)
		}
	case []interface{}:
		for i, child := range val {
			walkStrings(fmt.Sprintf("%s[%d]", prefix, i), child, out)
		}
	default:
		out[prefix] = fmt.Sprintf("%v", val)
	}
}
