package bench_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/benchmarks"
	"github.com/siyul-park/spry/benchmarks/bench"
	"github.com/siyul-park/spry/benchmarks/runner"
)

type fake struct {
	name   string
	avail  error
	output string
	err    error
}

const (
	beginMarker = "<!-- BEGIN GENERATED RESULTS -->"
	endMarker   = "<!-- END GENERATED RESULTS -->"
)

func TestRunTable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runners := []runner.Runner{fake{name: "a", output: "6765\n"}, runner.Spry()}
	err := bench.Run(context.Background(), []string{"-fixture", "recursive-fib-20", "-count", "1", "-warmup", "0"}, &stdout, &stderr, runners)
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "| recursive-fib-20 | 5.0 ms | skipped |")
	require.Contains(t, stdout.String(), "- spry skipped:")
}

func TestRunFailures(t *testing.T) {
	tests := []struct {
		name   string
		runner fake
		want   string
	}{
		{"wrong output", fake{name: "a", output: "1\n"}, "| FAIL |"},
		{"run error", fake{name: "a", err: errors.New("boom")}, "a on recursive-fib-20 failed: boom"},
		{"unavailable", fake{name: "a", avail: errors.New("nope")}, "- a skipped: nope"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := []string{"-fixture", "recursive-fib-20", "-count", "2"}
			require.NoError(t, bench.Run(context.Background(), args, &stdout, &stderr, []runner.Runner{tt.runner}))
			require.Contains(t, stdout.String(), tt.want)
		})
	}
}

func TestRunErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"bad flag", []string{"-nope"}},
		{"bad count", []string{"-count", "0"}},
		{"unknown runner", []string{"-runner", "ghost"}},
		{"unknown fixture", []string{"-fixture", "ghost"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			require.Error(t, bench.Run(context.Background(), tt.args, &out, &out, runner.All()))
		})
	}
}

func TestRunSelectsRunner(t *testing.T) {
	var stdout, stderr bytes.Buffer
	args := []string{"-runner", "a", "-fixture", "sieve", "-count", "1", "-warmup", "0"}
	runners := []runner.Runner{fake{name: "a", output: "x"}, fake{name: "b"}}
	require.NoError(t, bench.Run(context.Background(), args, &stdout, &stderr, runners))
	require.NotContains(t, stdout.String(), "| b |")
}

func TestRunWritesReport(t *testing.T) {
	dir := t.TempDir()
	args := func(path string) []string {
		return []string{"-runner", "a", "-fixture", "recursive-fib-20", "-count", "1", "-warmup", "0", "-o", path}
	}
	runners := []runner.Runner{fake{name: "a", output: "6765\n"}}

	missing := filepath.Join(dir, "none.md")
	var stdout, stderr bytes.Buffer
	require.NoError(t, bench.Run(context.Background(), args(missing), &stdout, &stderr, runners))
	require.Contains(t, stderr.String(), "does not exist")
	require.NoFileExists(t, missing)

	plain := filepath.Join(dir, "plain.md")
	require.NoError(t, os.WriteFile(plain, []byte("# doc\n"), 0o644))
	stderr.Reset()
	require.NoError(t, bench.Run(context.Background(), args(plain), &stdout, &stderr, runners))
	require.Contains(t, stderr.String(), "markers")
	got, err := os.ReadFile(plain)
	require.NoError(t, err)
	require.Equal(t, "# doc\n", string(got))

	marked := filepath.Join(dir, "marked.md")
	require.NoError(t, os.WriteFile(marked, []byte("a\n"+beginMarker+"\nold\n"+endMarker+"\nz\n"), 0o644))
	stderr.Reset()
	require.NoError(t, bench.Run(context.Background(), args(marked), &stdout, &stderr, runners))
	require.Empty(t, stderr.String())
	got, err = os.ReadFile(marked)
	require.NoError(t, err)
	require.Contains(t, string(got), "| recursive-fib-20 | 5.0 ms |")
	require.True(t, bytes.HasPrefix(got, []byte("a\n"+beginMarker)))
	require.True(t, bytes.HasSuffix(got, []byte(endMarker+"\nz\n")))
	require.NotContains(t, string(got), "old")
}

func (f fake) Name() string {
	return f.name
}

func (f fake) Available() error {
	return f.avail
}

func (f fake) Run(context.Context, benchmarks.Fixture) (runner.Result, error) {
	return runner.Result{Output: f.output, Elapsed: 5 * time.Millisecond}, f.err
}
