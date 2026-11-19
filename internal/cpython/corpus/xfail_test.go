package corpus_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/internal/cpython/corpus"
)

// recorder captures failures instead of failing the test.
type recorder struct {
	testing.TB
	failures []string
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.failures = append(r.failures, format)
}

func TestLoadXFail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "xfail.txt")
	require.NoError(t, os.WriteFile(path, []byte("# header\na\n\n  b  # why\n"), 0o644))
	x, err := corpus.LoadXFail(path)
	require.NoError(t, err)
	require.Equal(t, corpus.XFail{"a": true, "b": true}, x)

	_, err = corpus.LoadXFail(filepath.Join(t.TempDir(), "none"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestXFail_Check(t *testing.T) {
	x := corpus.XFail{"known": true}
	boom := errors.New("boom")
	tests := []struct {
		name string
		id   string
		err  error
		fail bool
	}{
		{"listed fails", "known", boom, false},
		{"listed passes", "known", nil, true},
		{"unlisted passes", "other", nil, false},
		{"unlisted fails", "other", boom, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &recorder{TB: t}
			x.Check(r, tt.id, tt.err)
			require.Equal(t, tt.fail, len(r.failures) > 0)
		})
	}
}
