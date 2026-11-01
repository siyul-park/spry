package runner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/benchmarks"
	"github.com/siyul-park/spry/benchmarks/runner"
)

var tiny = benchmarks.Fixture{Name: "tiny", Source: "def run():\n    return 6 * 7\n", Expected: "42\n"}

func TestAll(t *testing.T) {
	var names []string
	for _, r := range runner.All() {
		names = append(names, r.Name())
	}
	require.Equal(t, []string{"cpython", "pypy3", "gpython", "spry"}, names)
}

func TestSpryUnavailable(t *testing.T) {
	r := runner.Spry()
	err := r.Available()
	require.ErrorIs(t, err, runner.ErrUnavailable)
	require.Contains(t, err.Error(), "phase P3")
	_, err = r.Run(context.Background(), tiny)
	require.ErrorIs(t, err, runner.ErrUnavailable)
}

func TestPathRunnersMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, r := range []runner.Runner{runner.PyPy3(), runner.GPython()} {
		require.ErrorIs(t, r.Available(), runner.ErrUnavailable, r.Name())
		_, err := r.Run(context.Background(), tiny)
		require.ErrorIs(t, err, runner.ErrUnavailable, r.Name())
	}
}

func TestPathRunnersFound(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"pypy3", "gpython"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\necho 42\n"), 0o755))
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, r := range []runner.Runner{runner.PyPy3(), runner.GPython()} {
		require.NoError(t, r.Available(), r.Name())
		res, err := r.Run(context.Background(), tiny)
		require.NoError(t, err)
		require.Equal(t, "42\n", res.Output)
	}
}

func TestCPython(t *testing.T) {
	r := runner.CPython()
	if err := r.Available(); err != nil {
		t.Skip(err)
	}
	res, err := r.Run(context.Background(), tiny)
	require.NoError(t, err)
	require.Equal(t, tiny.Expected, res.Output)
	require.Positive(t, res.Elapsed)

	_, err = r.Run(context.Background(), benchmarks.Fixture{Name: "bad", Source: "def run():\n    raise ValueError('x')\n"})
	require.ErrorContains(t, err, "ValueError")
}

func TestCPythonEnvOverride(t *testing.T) {
	t.Setenv(runner.PythonEnv, "definitely-not-an-interpreter")
	require.ErrorIs(t, runner.CPython().Available(), runner.ErrUnavailable)

	fake := filepath.Join(t.TempDir(), "fakepy")
	require.NoError(t, os.WriteFile(fake, []byte("#!/bin/sh\necho 42\n"), 0o755))
	t.Setenv(runner.PythonEnv, fake)
	res, err := runner.CPython().Run(context.Background(), tiny)
	require.NoError(t, err)
	require.Equal(t, "42\n", res.Output)
}
