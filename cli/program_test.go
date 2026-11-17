package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/cli"
)

const notImplemented = "spry: execution not implemented yet (phase P3)\n"

func TestCommand(t *testing.T) {
	root := units(cli.Command())
	for _, args := range [][]string{{"-c", "print(1)"}, {"-cprint(1)"}, {"-c", "pass", "a", "-z"}} {
		code, stdout, stderr := run(t, root, args...)
		require.Equal(t, cli.ExitFailure, code, args)
		require.Empty(t, stdout)
		require.Equal(t, notImplemented, stderr)
	}
	code, _, stderr := run(t, root, "-c")
	require.Equal(t, cli.ExitUsage, code)
	require.True(t, strings.HasPrefix(stderr, "Argument expected for the -c option\n"))
}

func TestFile(t *testing.T) {
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
		stderr string
	}{
		{"exists", []string{"ok.py"}, cli.ExitFailure, notImplemented},
		{"absolute", []string{filepath.Join(dir, "ok.py")}, cli.ExitFailure, notImplemented},
		{"arguments are not options", []string{"ok.py", "-z", "a"}, cli.ExitFailure, notImplemented},
		{"missing", []string{"no.py"}, cli.ExitUsage, "spry: can't open file '" + filepath.Join(dir, "no.py") + "': [Errno 2] No such file or directory\n"},
		{"directory", []string{"pkg"}, cli.ExitFailure, "spry: can't find '__main__' module in '" + filepath.Join(dir, "pkg") + "'\n"},
		{"not a directory", []string{"ok.py/x"}, cli.ExitUsage, "spry: can't open file '" + filepath.Join(dir, "ok.py", "x") + "': [Errno 20] Not a directory\n"},
		{"permission", []string{"locked/x.py"}, cli.ExitUsage, "spry: can't open file '" + filepath.Join(locked, "x.py") + "': [Errno 13] Permission denied\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "permission" && os.Geteuid() == 0 {
				t.Skip("root ignores directory permissions")
			}
			var stdout, stderr bytes.Buffer
			code := cli.New(cli.File()).Run(context.Background(), cli.Process{Args: tt.args, Stdout: &stdout, Stderr: &stderr, Dir: dir})
			require.Equal(t, tt.code, code)
			require.Empty(t, stdout.String())
			require.Equal(t, tt.stderr, stderr.String())
		})
	}

	t.Run("defaults to the current directory", func(t *testing.T) {
		cwd, err := os.Getwd()
		require.NoError(t, err)
		var stderr bytes.Buffer
		code := cli.New(cli.File()).Run(context.Background(), cli.Process{Args: []string{"no-such-script.py"}, Stderr: &stderr})
		require.Equal(t, cli.ExitUsage, code)
		require.Equal(t, "spry: can't open file '"+filepath.Join(cwd, "no-such-script.py")+"': [Errno 2] No such file or directory\n", stderr.String())
	})
}

func TestStdin(t *testing.T) {
	root := units(cli.Stdin())
	for _, args := range [][]string{nil, {"-"}, {"-", "a"}, {"--"}} {
		code, _, stderr := run(t, root, args...)
		require.Equal(t, cli.ExitFailure, code, args)
		require.Equal(t, notImplemented, stderr)
	}
	code, _, stderr := run(t, root, "x.py")
	require.Equal(t, cli.ExitFailure, code)
	require.Equal(t, "spry: no command selected\n", stderr)
}
