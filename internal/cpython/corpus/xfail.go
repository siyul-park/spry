package corpus

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// XFail is the set of case IDs expected to fail until the implementation
// catches up with CPython.
type XFail map[string]bool

// LoadXFail reads a text file with one ID per line. Blank lines and text
// after '#' are ignored.
func LoadXFail(path string) (XFail, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("corpus: %w", err)
	}
	x := XFail{}
	for line := range strings.SplitSeq(string(data), "\n") {
		line, _, _ = strings.Cut(line, "#")
		if id := strings.TrimSpace(line); id != "" {
			x[id] = true
		}
	}
	return x, nil
}

// Check reports the outcome of case id, where err is nil when the case
// passed. A listed case must fail and an unlisted one must pass, so the list
// shrinks as the implementation improves.
func (x XFail) Check(t testing.TB, id string, err error) {
	t.Helper()
	switch {
	case x[id] && err == nil:
		t.Errorf("%s passes; remove from xfail", id)
	case !x[id] && err != nil:
		t.Errorf("%s: %v", id, err)
	}
}
