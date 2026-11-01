package conformance

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// Case is one program of the corpus.
type Case struct {
	Category    string
	Name        string // base name without extension
	Path        string // slash-separated path of the .py file inside the corpus
	Source      string
	Expected    string
	Divergences []string // reasons from "# spry-divergence:" directives
}

const (
	// DivergencePrefix introduces a divergence directive comment.
	DivergencePrefix = "# spry-divergence"
	// SourceExt is the extension of a case program.
	SourceExt = ".py"
	// ExpectedExt is the extension of the recorded CPython stdout.
	ExpectedExt = ".expected"
)

// ErrDirective reports a malformed divergence directive.
var ErrDirective = errors.New("conformance: malformed spry-divergence directive")

// ParseDirectives extracts the divergence reasons from a program. A directive
// is a whole-line comment "# spry-divergence: <reason>" with a non-empty
// reason; any other line that starts with the prefix is an error.
func ParseDirectives(src string) ([]string, error) {
	var reasons []string
	for i, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, DivergencePrefix) {
			continue
		}
		rest := strings.TrimPrefix(trimmed, DivergencePrefix)
		reason, ok := strings.CutPrefix(rest, ":")
		reason = strings.TrimSpace(reason)
		if !ok || reason == "" {
			return nil, fmt.Errorf("%w: line %d: %q", ErrDirective, i+1, trimmed)
		}
		reasons = append(reasons, reason)
	}
	return reasons, nil
}

// Load discovers every <category>/<name>.py in fsys. A program without a
// sibling .expected file is an error, so a corpus that loads is complete.
func Load(fsys fs.FS) ([]Case, error) {
	var sources []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && path.Ext(p) == SourceExt {
			sources = append(sources, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("conformance: walk corpus: %w", err)
	}
	sort.Strings(sources)

	cases := make([]Case, 0, len(sources))
	for _, p := range sources {
		c, err := loadCase(fsys, p)
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, nil
}

// ID is the stable "category/name" identifier of the case.
func (c Case) ID() string {
	return c.Category + "/" + c.Name
}

func loadCase(fsys fs.FS, p string) (Case, error) {
	if strings.Count(p, "/") != 1 {
		return Case{}, fmt.Errorf("conformance: %s: programs live in <category>/<name>.py", p)
	}
	src, err := fs.ReadFile(fsys, p)
	if err != nil {
		return Case{}, fmt.Errorf("conformance: read %s: %w", p, err)
	}
	base := strings.TrimSuffix(p, SourceExt)
	expected, err := fs.ReadFile(fsys, base+ExpectedExt)
	if err != nil {
		return Case{}, fmt.Errorf("conformance: %s has no %s file: %w", p, ExpectedExt, err)
	}
	reasons, err := ParseDirectives(string(src))
	if err != nil {
		return Case{}, fmt.Errorf("%s: %w", p, err)
	}
	return Case{
		Category:    path.Dir(p),
		Name:        path.Base(base),
		Path:        p,
		Source:      string(src),
		Expected:    string(expected),
		Divergences: reasons,
	}, nil
}
