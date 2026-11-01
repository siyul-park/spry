package benchmarks

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// Fixture is one benchmark kernel.
type Fixture struct {
	Name     string // file name without extension
	Source   string // Python source defining run()
	Expected string // stdout of print(run()) under CPython
}

//go:embed fixtures/*.py fixtures/*.expected
var files embed.FS

// Fixtures returns every embedded fixture sorted by name.
func Fixtures() ([]Fixture, error) {
	entries, err := fs.ReadDir(files, "fixtures")
	if err != nil {
		return nil, fmt.Errorf("benchmarks: list fixtures: %w", err)
	}
	var out []Fixture
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".py")
		if !ok {
			continue
		}
		src, err := files.ReadFile("fixtures/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("benchmarks: read %s: %w", e.Name(), err)
		}
		want, err := files.ReadFile("fixtures/" + name + ".expected")
		if err != nil {
			return nil, fmt.Errorf("benchmarks: fixture %s has no .expected file: %w", name, err)
		}
		out = append(out, Fixture{Name: name, Source: string(src), Expected: string(want)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Select returns the fixtures named by patterns. A pattern matches a fixture
// whose name equals it or starts with it followed by "-", so "recursive-fib"
// selects recursive-fib-20 and recursive-fib-35. No patterns selects all.
func Select(all []Fixture, patterns []string) ([]Fixture, error) {
	if len(patterns) == 0 {
		return all, nil
	}
	var out []Fixture
	for _, p := range patterns {
		matched := false
		for _, f := range all {
			if f.Name == p || strings.HasPrefix(f.Name, p+"-") {
				out = append(out, f)
				matched = true
			}
		}
		if !matched {
			return nil, fmt.Errorf("benchmarks: no fixture matches %q", p)
		}
	}
	return out, nil
}

// Driver returns the program a runner executes: the kernel followed by a call
// that prints its result.
func (f Fixture) Driver() string {
	return f.Source + "\n\nprint(run())\n"
}
