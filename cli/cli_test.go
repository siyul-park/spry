package cli_test

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/cli"
	"github.com/siyul-park/spry/internal/cpython"
)

type goldenCase struct {
	name  string
	args  []string
	files map[string]string // written to the working directory first
	stdin string
}

const (
	goldenDir = "testdata/golden"
	cwdToken  = "$CWD"
)

var update = flag.Bool("update", false, "re-record goldens from CPython 3.14")

var (
	usageRe = regexp.MustCompile(`(?m)^usage: .*? \[option\]`)
	progRe  = regexp.MustCompile(`(?m)^\S[^\n]*?: (can't (?:open file|find '__main__' module))`)
	tryRe   = regexp.MustCompile("Try `python -h'")
)

// goldenCases are the command lines whose output must equal CPython 3.14's
// after normalization (see normalizeCPython). Cases with cpythonOnly unset are
// compared against the recorded golden and, when python3.14 is available,
// against a live CPython run.
var goldenCases = []goldenCase{
	{name: "unknown-short", args: []string{"-z"}},
	{name: "unknown-short-in-group", args: []string{"-Ez"}},
	{name: "unknown-after-flag", args: []string{"-O", "-z"}},
	{name: "unknown-before-help", args: []string{"-z", "-h"}},
	{name: "unknown-long", args: []string{"--foo"}},
	{name: "unknown-long-value", args: []string{"--foo=bar"}},
	{name: "unknown-long-abbrev", args: []string{"--help-x"}},
	{name: "unknown-version-eq", args: []string{"--version=3"}},
	{name: "unknown-after-version", args: []string{"-V", "-z"}},
	{name: "missing-c", args: []string{"-c"}},
	{name: "missing-m", args: []string{"-m"}},
	{name: "missing-W", args: []string{"-W"}},
	{name: "missing-X", args: []string{"-X"}},
	{name: "missing-hash-arg", args: []string{"--check-hash-based-pycs"}},
	{name: "missing-after-group", args: []string{"-OOW"}},
	{name: "file-not-found", args: []string{"nonexist.py"}},
	{name: "file-not-found-dashdash", args: []string{"--", "-c"}},
	{name: "file-not-found-args", args: []string{"nonexist.py", "-z", "a"}},
	{name: "help-short", args: []string{"-h"}},
	{name: "help-question", args: []string{"-?"}},
	{name: "help-long", args: []string{"--help"}},
	{name: "help-wins-over-error", args: []string{"-h", "-z"}},
	{name: "help-in-group", args: []string{"-Eh"}},

	{name: "syntax-file-never-closed", args: []string{"a.py"}, files: map[string]string{"a.py": "x = (1,\n"}},
	{name: "syntax-file-nested-never-closed", args: []string{"a.py"}, files: map[string]string{"a.py": "def f():\n    x = [1,\n  2\n"}},
	{name: "syntax-file-non-ascii-column", args: []string{"a.py"}, files: map[string]string{"a.py": "x = \"é\" + (\n"}},
	{name: "syntax-file-unterminated-string", args: []string{"a.py"}, files: map[string]string{"a.py": "s = \"abc\n"}},
	{name: "syntax-file-unterminated-triple-quoted", args: []string{"a.py"}, files: map[string]string{"a.py": "x = \"\"\"abc\ndef\n"}},
	{name: "syntax-file-invalid-digit", args: []string{"a.py"}, files: map[string]string{"a.py": "x = 0b12\n"}},
	{name: "syntax-file-invalid-decimal-literal", args: []string{"a.py"}, files: map[string]string{"a.py": "x = 1abc\n"}},
	{name: "syntax-file-leading-zeros", args: []string{"a.py"}, files: map[string]string{"a.py": "a = 0123\n"}},
	{name: "syntax-file-invalid-character", args: []string{"a.py"}, files: map[string]string{"a.py": "x = ¢\n"}},
	{name: "syntax-file-non-printable", args: []string{"a.py"}, files: map[string]string{"a.py": "x = 1 \x01\n"}},
	{name: "syntax-file-unmatched-paren", args: []string{"a.py"}, files: map[string]string{"a.py": "x = ) \n"}},
	{name: "syntax-file-mismatched-paren", args: []string{"a.py"}, files: map[string]string{"a.py": "x = (]\n"}},
	{name: "syntax-file-unindent", args: []string{"a.py"}, files: map[string]string{"a.py": "if 1:\n    x\n  y\n"}},
	{name: "syntax-file-tab-error", args: []string{"a.py"}, files: map[string]string{"a.py": "if 1:\n        x\n\ty\n"}},
	{name: "syntax-file-leading-blanks-stripped", args: []string{"a.py"}, files: map[string]string{"a.py": "if 1:\n\t  x = \"abc\n"}},
	{name: "syntax-file-crlf", args: []string{"a.py"}, files: map[string]string{"a.py": "x = 1\r\ny = (\r\n"}},
	{name: "syntax-file-unknown-encoding", args: []string{"a.py"}, files: map[string]string{"a.py": "# coding: nope\nx = 1\n"}},
	{name: "syntax-file-fstring", args: []string{"a.py"}, files: map[string]string{"a.py": "f\"{\"\n"}},
	{name: "syntax-file-relative-subdir", args: []string{"sub/a.py"}, files: map[string]string{"sub/a.py": "x = (\n"}},
	{name: "syntax-command", args: []string{"-c", "x = (1,"}},
	{name: "syntax-command-second-line", args: []string{"-c", "x = 1\ny = \"abc"}},
	{name: "syntax-stdin", args: nil, stdin: "x = (\n"},
	{name: "syntax-stdin-dash", args: []string{"-"}, stdin: "if 1:\n  x\n y\n"},
	{name: "syntax-stdin-crlf", args: nil, stdin: "x = 1\r\ny = (\r\n"},
}

// spryOnlyCases differ from CPython (they execute code, print spry-only text,
// or hit an option whose meaning is not implemented yet), so they are recorded
// from spry itself and never diffed against it. The cases that use an
// unsupported option move back to goldenCases, re-recorded from CPython, when
// the unit that implements the option lands: -b -B -d -E -i -I -O -P -q -s -S
// -u -v -x (interpreter options), -W, -X, -m, --help-env, --help-xoptions,
// --help-all and --check-hash-based-pycs.
var spryOnlyCases = []goldenCase{
	{name: "c-swallows-help", args: []string{"-c", "pass", "-h", "-z"}},
	{name: "m-swallows-flags", args: []string{"-m", "mod", "-z"}},
	{name: "W-takes-next-dash-arg", args: []string{"-W", "-z", "-V"}},
	{name: "version", args: []string{"-V"}},
	{name: "version-long", args: []string{"--version"}},
	{name: "invalid-spry-opt", args: []string{"-X", "spry.opt=9"}},
	{name: "invalid-spry-dump", args: []string{"-X", "spry.dump=nope"}},
	{name: "invalid-spry-fallback", args: []string{"-X", "spry.fallback=maybe"}},
	{name: "unknown-spry-option", args: []string{"-X", "spry.nothing"}},
	{name: "bad-hash-arg", args: []string{"--check-hash-based-pycs", "bad"}},
	{name: "unsupported-before-file", args: []string{"-u", "nonexist.py", "-z", "a"}},
	{name: "exec-command", args: []string{"-c", "print(1)"}},
	{name: "exec-module", args: []string{"-m", "json.tool"}},
	{name: "exec-stdin", args: nil},
	{name: "exec-file", args: []string{"ok.py"}, files: map[string]string{"ok.py": "print(1)\n"}},
	{name: "help-env", args: []string{"--help-env"}},
	{name: "help-env-wins-over-version", args: []string{"-V", "--help-env"}},
	{name: "help-xoptions", args: []string{"--help-xoptions"}},
}

// normalizeCPython maps CPython output onto what spry prints: the executable
// path becomes "spry", the working directory becomes $CWD.
func normalizeCPython(text, dir string) string {
	text = usageRe.ReplaceAllString(text, "usage: spry [option]")
	text = progRe.ReplaceAllString(text, "spry: $1")
	text = tryRe.ReplaceAllString(text, "Try `spry -h'")
	return strings.ReplaceAll(text, dir, cwdToken)
}

func format(stdout, stderr string, code int) string {
	return fmt.Sprintf("exit: %d\n--- stdout\n%s--- stderr\n%s", code, stdout, stderr)
}

func goldenPath(name string) string {
	return filepath.Join(goldenDir, name+".golden")
}

func TestGolden(t *testing.T) {
	var python string
	if *update {
		python = lookupPython(t)
		require.NoError(t, os.MkdirAll(goldenDir, 0o755))
	}
	for _, tt := range goldenCases {
		t.Run(tt.name, func(t *testing.T) {
			got := runSpry(t, tt)
			if *update {
				require.NoError(t, os.WriteFile(goldenPath(tt.name), []byte(runCPython(t, python, tt)), 0o644))
			}
			want, err := os.ReadFile(goldenPath(tt.name))
			require.NoError(t, err)
			require.Equal(t, string(want), got)
		})
	}
	for _, tt := range spryOnlyCases {
		t.Run(tt.name, func(t *testing.T) {
			got := runSpry(t, tt)
			if *update {
				require.NoError(t, os.WriteFile(goldenPath(tt.name), []byte(got), 0o644))
			}
			want, err := os.ReadFile(goldenPath(tt.name))
			require.NoError(t, err)
			require.Equal(t, string(want), got)
		})
	}
}

func TestGoldenMatchesCPython(t *testing.T) {
	python := lookupPython(t)
	for _, tt := range goldenCases {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, runCPython(t, python, tt), runSpry(t, tt))
		})
	}
}

func TestGoldenFiles(t *testing.T) {
	seen := map[string]bool{}
	for _, tt := range append(append([]goldenCase(nil), goldenCases...), spryOnlyCases...) {
		require.False(t, seen[tt.name], tt.name)
		seen[tt.name] = true
	}
	entries, err := os.ReadDir(goldenDir)
	require.NoError(t, err)
	for _, e := range entries {
		require.True(t, seen[strings.TrimSuffix(e.Name(), ".golden")], "orphan golden %s", e.Name())
	}
}

func runSpry(t *testing.T, tt goldenCase) string {
	t.Helper()
	dir := workDir(t, tt.files)
	var stdout, stderr bytes.Buffer
	code := cli.Run(context.Background(), cli.Process{Args: tt.args, Stdin: strings.NewReader(tt.stdin), Stdout: &stdout, Stderr: &stderr, Dir: dir})
	return strings.ReplaceAll(format(stdout.String(), stderr.String(), code), dir, cwdToken)
}

func runCPython(t *testing.T, python string, tt goldenCase) string {
	t.Helper()
	dir := workDir(t, tt.files)
	res, err := cpython.Run(context.Background(), python, dir, []byte(tt.stdin), tt.args...)
	require.NoError(t, err)
	return format(normalizeCPython(res.Stdout, dir), normalizeCPython(res.Stderr, dir), res.ExitCode)
}

// workDir returns a fresh working directory holding files.
func workDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	for name, text := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(text), 0o644))
	}
	return dir
}

func lookupPython(t *testing.T) string {
	t.Helper()
	python, err := cpython.Lookup()
	if err != nil {
		t.Skip("python 3.14 unavailable: ", err)
	}
	res, err := cpython.Run(context.Background(), python, "", nil, "-c", "import sys; print(sys.version_info[:2] == (3, 14))")
	require.NoError(t, err)
	if strings.TrimSpace(res.Stdout) != "True" {
		t.Skip("interpreter is not CPython 3.14: ", python)
	}
	return python
}
