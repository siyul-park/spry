package bench

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// cell is the outcome of one fixture under one runner.
type cell struct {
	Skipped string          // reason, when the runner was unavailable
	Failed  string          // reason, when a run failed or printed the wrong result
	Samples []time.Duration // measured runs
}

const (
	beginMarker = "<!-- BEGIN GENERATED RESULTS -->"
	endMarker   = "<!-- END GENERATED RESULTS -->"
)

func (c cell) String() string {
	switch {
	case c.Skipped != "":
		return "skipped"
	case c.Failed != "":
		return "FAIL"
	case len(c.Samples) == 0:
		return "-"
	}
	return fmt.Sprintf("%.1f ms", float64(c.median())/float64(time.Millisecond))
}

func (c cell) median() time.Duration {
	s := append([]time.Duration(nil), c.Samples...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

// table renders results as a markdown table: one row per fixture, one column
// per runner. Skip and failure reasons follow as a list.
func table(runners, fixtures []string, results map[string]map[string]cell) string {
	var b strings.Builder
	b.WriteString("| fixture | " + strings.Join(runners, " | ") + " |\n")
	b.WriteString("| --- |" + strings.Repeat(" ---: |", len(runners)) + "\n")
	var notes []string
	for _, f := range fixtures {
		row := []string{f}
		for _, r := range runners {
			c := results[f][r]
			row = append(row, c.String())
			if c.Failed != "" {
				notes = append(notes, fmt.Sprintf("- %s on %s failed: %s", r, f, c.Failed))
			}
		}
		b.WriteString("| " + strings.Join(row, " | ") + " |\n")
	}
	b.WriteString("\nMedian wall-clock time per run, interpreter startup included.\n")
	skipped := map[string]string{}
	for _, f := range fixtures {
		for _, r := range runners {
			if reason := results[f][r].Skipped; reason != "" {
				skipped[r] = reason
			}
		}
	}
	for _, r := range runners {
		if reason, ok := skipped[r]; ok {
			notes = append(notes, fmt.Sprintf("- %s skipped: %s", r, reason))
		}
	}
	if len(notes) > 0 {
		b.WriteString("\n" + strings.Join(notes, "\n") + "\n")
	}
	return b.String()
}

// writeReport updates path in place. It never creates the file and leaves it
// alone when the markers are missing; the returned message says what happened.
func writeReport(path, body string) (string, error) {
	doc, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Sprintf("bench: %s does not exist; results not written", path), nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	updated, ok := splice(string(doc), body)
	if !ok {
		return fmt.Sprintf("bench: %s has no generated-results markers; results not written", path), nil
	}
	if err := writeAtomic(path, []byte(updated)); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return "", nil
}

// writeAtomic replaces path through a temporary file in the same directory, so
// a crash never leaves a half-written report.
func writeAtomic(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), info.Mode().Perm()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// splice replaces the text between the markers of doc with body.
func splice(doc, body string) (string, bool) {
	begin := strings.Index(doc, beginMarker)
	end := strings.Index(doc, endMarker)
	if begin < 0 || end < begin {
		return "", false
	}
	return doc[:begin+len(beginMarker)] + "\n\n" + body + "\n" + doc[end:], true
}
