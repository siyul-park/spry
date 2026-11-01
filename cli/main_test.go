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
	name string
	args []string
}

const (
	goldenDir   = "testdata/golden"
	cwdToken    = "$CWD"
	notImpl     = "spry: execution not implemented yet (phase P3)\n"
	usageLine   = "usage: spry [option] ... [-c cmd | -m mod | file | -] [arg] ...\n"
	usageHint   = "Try `spry -h' for more information.\n"
	versionLine = "Python 3.14.0 (spry " + cli.Release + ")\n"
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
	{"unknown-short", []string{"-z"}},
	{"unknown-short-in-group", []string{"-Ez"}},
	{"unknown-after-flag", []string{"-O", "-z"}},
	{"unknown-before-help", []string{"-z", "-h"}},
	{"unknown-long", []string{"--foo"}},
	{"unknown-long-value", []string{"--foo=bar"}},
	{"unknown-long-abbrev", []string{"--help-x"}},
	{"unknown-version-eq", []string{"--version=3"}},
	{"unknown-after-version", []string{"-V", "-z"}},
	{"missing-c", []string{"-c"}},
	{"missing-m", []string{"-m"}},
	{"missing-W", []string{"-W"}},
	{"missing-X", []string{"-X"}},
	{"missing-hash-arg", []string{"--check-hash-based-pycs"}},
	{"bad-hash-arg", []string{"--check-hash-based-pycs", "bad"}},
	{"missing-after-group", []string{"-OOW"}},
	{"file-not-found", []string{"nonexist.py"}},
	{"file-not-found-dashdash", []string{"--", "-c"}},
	{"file-not-found-args", []string{"-u", "nonexist.py", "-z", "a"}},
	{"help-short", []string{"-h"}},
	{"help-question", []string{"-?"}},
	{"help-long", []string{"--help"}},
	{"help-wins-over-error", []string{"-h", "-z"}},
	{"help-in-group", []string{"-Eh"}},
	{"help-env", []string{"--help-env"}},
	{"help-env-wins-over-version", []string{"-V", "--help-env"}},
}

// spryOnlyCases differ from CPython (they execute code or print spry-only
// text), so they are recorded from spry itself and never diffed against it.
var spryOnlyCases = []goldenCase{
	{"c-swallows-help", []string{"-c", "pass", "-h", "-z"}},
	{"m-swallows-flags", []string{"-m", "mod", "-z"}},
	{"W-takes-next-dash-arg", []string{"-W", "-z", "-V"}},
	{"version", []string{"-V"}},
	{"version-long", []string{"--version"}},
	{"invalid-spry-opt", []string{"-X", "spry.opt=9"}},
	{"invalid-spry-dump", []string{"-X", "spry.dump=nope"}},
	{"invalid-spry-fallback", []string{"-X", "spry.fallback=maybe"}},
	{"unknown-spry-option", []string{"-X", "spry.nothing"}},
	{"exec-command", []string{"-c", "print(1)"}},
	{"exec-module", []string{"-m", "json.tool"}},
	{"exec-stdin", nil},
	{"help-xoptions", []string{"--help-xoptions"}},
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

func TestRun(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ok.py"), []byte("pass\n"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "pkg"), 0o755))
	locked := filepath.Join(dir, "locked")
	require.NoError(t, os.Mkdir(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	tests := []struct {
		name   string
		args   []string
		code   int
		stdout string
		stderr string
	}{
		{"version", []string{"-V"}, cli.ExitOK, versionLine, ""},
		{"exists", []string{"ok.py"}, cli.ExitFailure, "", notImpl},
		{"absolute", []string{filepath.Join(dir, "ok.py")}, cli.ExitFailure, "", notImpl},
		{"missing", []string{"no.py"}, cli.ExitUsage, "", "spry: can't open file '" + filepath.Join(dir, "no.py") + "': [Errno 2] No such file or directory\n"},
		{"directory", []string{"pkg"}, cli.ExitFailure, "", "spry: can't find '__main__' module in '" + filepath.Join(dir, "pkg") + "'\n"},
		{"not a dir", []string{"ok.py/x"}, cli.ExitUsage, "", "spry: can't open file '" + filepath.Join(dir, "ok.py", "x") + "': [Errno 20] Not a directory\n"},
		{"permission", []string{"locked/x.py"}, cli.ExitUsage, "", "spry: can't open file '" + filepath.Join(locked, "x.py") + "': [Errno 13] Permission denied\n"},
		{"command", []string{"-c", "print(1)"}, cli.ExitFailure, "", notImpl},
		{"module", []string{"-m", "x"}, cli.ExitFailure, "", notImpl},
		{"stdin", []string{"-"}, cli.ExitFailure, "", notImpl},
		{"no args", nil, cli.ExitFailure, "", notImpl},
		{"usage error", []string{"-z"}, cli.ExitUsage, "", "Unknown option: -z\n" + usageLine + usageHint},
		{"x option error", []string{"-X", "spry.opt=9"}, cli.ExitUsage, "", "invalid -X option \"spry.opt=9\": expected an integer from 0 to 3\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "permission" && os.Geteuid() == 0 {
				t.Skip("root ignores directory permissions")
			}
			var stdout, stderr bytes.Buffer
			code := cli.Run(cli.Process{Args: tt.args, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Dir: dir})
			require.Equal(t, tt.code, code)
			require.Equal(t, tt.stdout, stdout.String())
			require.Equal(t, tt.stderr, stderr.String())
		})
	}
}

func TestRunDefaultsToCurrentDirectory(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)
	var stdout, stderr bytes.Buffer
	code := cli.Run(cli.Process{Args: []string{"no-such-script.py"}, Stdout: &stdout, Stderr: &stderr})
	require.Equal(t, cli.ExitUsage, code)
	require.Equal(t, "spry: can't open file '"+filepath.Join(cwd, "no-such-script.py")+"': [Errno 2] No such file or directory\n", stderr.String())
}

func TestRunHelpTexts(t *testing.T) {
	_, help, _ := runSpryRaw(t, "-h")
	_, env, _ := runSpryRaw(t, "--help-env")
	_, xopts, _ := runSpryRaw(t, "--help-xoptions")
	_, all, _ := runSpryRaw(t, "--help-all")
	require.Equal(t, help+"\n"+env+"\n"+xopts, all)
	for _, name := range []string{"spry.dump", "spry.emit", "spry.fallback", "spry.opt"} {
		require.Contains(t, xopts, "-X "+name)
	}
}

func TestRunVersionVerbose(t *testing.T) {
	_, stdout, _ := runSpryRaw(t, "-VV")
	require.True(t, strings.HasPrefix(stdout, "Python 3.14.0 (spry "+cli.Release+") [Go go"), stdout)
}

func TestMain_(t *testing.T) {
	var stdout, stderr bytes.Buffer
	require.Equal(t, cli.ExitOK, cli.Main([]string{"spry", "-V"}, strings.NewReader(""), &stdout, &stderr, nil))
	require.Equal(t, versionLine, stdout.String())

	stderr.Reset()
	require.Equal(t, cli.ExitFailure, cli.Main(nil, strings.NewReader(""), &stdout, &stderr, nil))
	require.Equal(t, notImpl, stderr.String())
}

func TestGolden(t *testing.T) {
	var python string
	if *update {
		python = lookupPython(t)
		require.NoError(t, os.MkdirAll(goldenDir, 0o755))
	}
	for _, tt := range goldenCases {
		t.Run(tt.name, func(t *testing.T) {
			got := runSpry(t, tt.args)
			if *update {
				require.NoError(t, os.WriteFile(goldenPath(tt.name), []byte(runCPython(t, python, tt.args)), 0o644))
			}
			want, err := os.ReadFile(goldenPath(tt.name))
			require.NoError(t, err)
			require.Equal(t, string(want), got)
		})
	}
	for _, tt := range spryOnlyCases {
		t.Run(tt.name, func(t *testing.T) {
			got := runSpry(t, tt.args)
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
			require.Equal(t, runCPython(t, python, tt.args), runSpry(t, tt.args))
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

func TestRunContext(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cli.RunContext(context.Background(), cli.Process{Args: []string{"-V"}, Stdout: &stdout, Stderr: &stderr})
	require.Equal(t, cli.ExitOK, code)
	require.Equal(t, versionLine, stdout.String())
}

func runSpryRaw(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Run(cli.Process{Args: args, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Dir: t.TempDir()})
	return code, stdout.String(), stderr.String()
}

func runSpry(t *testing.T, args []string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	var stdout, stderr bytes.Buffer
	code := cli.Run(cli.Process{Args: args, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Dir: dir})
	return strings.ReplaceAll(format(stdout.String(), stderr.String(), code), dir, cwdToken)
}

func runCPython(t *testing.T, python string, args []string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	res, err := cpython.Run(context.Background(), python, dir, nil, args...)
	require.NoError(t, err)
	return format(normalizeCPython(res.Stdout, dir), normalizeCPython(res.Stderr, dir), res.ExitCode)
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
