package conformance_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/conformance"
	"github.com/siyul-park/spry/internal/cpython"
)

func TestRunErrors(t *testing.T) {
	_, err := conformance.Run(context.Background(), nil, corpusDir, conformance.Case{})
	require.Error(t, err)
	_, err = conformance.Run(context.Background(), []string{"/nonexistent/interpreter"}, corpusDir, conformance.Case{Category: "a", Name: "b", Path: "a/b.py"})
	require.Error(t, err)
}

func TestOutcomePassed(t *testing.T) {
	require.True(t, conformance.Outcome{Stdout: "1\n"}.Passed("1\n"))
	require.False(t, conformance.Outcome{Stdout: "1\n", ExitCode: 1}.Passed("1\n"))
	require.False(t, conformance.Outcome{Stdout: "2\n"}.Passed("1\n"))
}

// TestCPythonSelfCheck proves the recorded .expected files are what CPython
// 3.14 prints now.
func TestCPythonSelfCheck(t *testing.T) {
	python, err := cpython.Lookup()
	if err != nil {
		t.Skip("python 3.14 unavailable: ", err)
	}
	cases, err := conformance.Load(os.DirFS(corpusDir))
	require.NoError(t, err)
	for _, c := range cases {
		t.Run(c.ID(), func(t *testing.T) {
			out, err := conformance.Run(context.Background(), []string{python}, corpusDir, c)
			require.NoError(t, err)
			require.True(t, out.Passed(c.Expected), "stdout %q stderr %q", out.Stdout, out.Stderr)
		})
	}
}

// TestSpry runs the corpus through spry. It stays skipped until phase P3 makes
// `spry file.py` execute programs.
func TestSpry(t *testing.T) {
	t.Skip("spry cannot execute programs before phase P3")
}
