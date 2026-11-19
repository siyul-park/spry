package cli_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/cli"
)

func units(u ...*cobra.Command) []*cobra.Command {
	return u
}

func TestDefault(t *testing.T) {
	var names []string
	for _, u := range cli.Default() {
		names = append(names, u.Name())
	}
	require.Equal(t, []string{"help", "version", "command", "file", "stdin", "xoptions"}, names)
}

func TestNew(t *testing.T) {
	t.Run("a script named like a cobra builtin is a script", func(t *testing.T) {
		for _, name := range []string{"__complete", "completion", "help"} {
			code, _, stderr := run(t, cli.Default(), name)
			require.Equal(t, cli.ExitUsage, code)
			require.Contains(t, stderr, "can't open file")
		}
	})
	t.Run("no unit selects the command line", func(t *testing.T) {
		code, _, stderr := run(t, units(cli.Version()), "x.py")
		require.Equal(t, cli.ExitFailure, code)
		require.Equal(t, "spry: no command selected\n", stderr)
	})
}

func TestRoot_Run(t *testing.T) {
	const usage = "usage: spry [option] ... [-c cmd | -m mod | file | -] [arg] ...\nTry `spry -h' for more information.\n"
	tests := []struct {
		name   string
		args   []string
		code   int
		stdout string
		stderr string
	}{
		{"grammar error", []string{"-z"}, cli.ExitUsage, "", "Unknown option: -z\n" + usage},
		{"grammar error beats unsupported", []string{"-O", "-z"}, cli.ExitUsage, "", "Unknown option: -z\n" + usage},
		{"unsupported short", []string{"-O", "x.py"}, cli.ExitUsage, "", "spry: option -O is not supported yet\n"},
		{"first unsupported is named", []string{"-b", "-O"}, cli.ExitUsage, "", "spry: option -b is not supported yet\n"},
		{"unsupported -R", []string{"-R", "x.py"}, cli.ExitUsage, "", "spry: option -R is not supported yet\n"},
		{"unsupported -t", []string{"-t", "-c", "pass"}, cli.ExitUsage, "", "spry: option -t is not supported yet\n"},
		{"help beats -R", []string{"-Rh"}, cli.ExitOK, "usage: spry", ""},
		{"help beats -t", []string{"-th"}, cli.ExitOK, "usage: spry", ""},
		{"unsupported long", []string{"--help-env"}, cli.ExitUsage, "", "spry: option --help-env is not supported yet\n"},
		{"unsupported -c value is not parsed", []string{"-m", "mod", "-z"}, cli.ExitUsage, "", "spry: option -m is not supported yet\n"},
		{"help is immediate", []string{"-h", "-z"}, cli.ExitOK, "usage: spry", ""},
		{"error before help", []string{"-z", "-h"}, cli.ExitUsage, "", "Unknown option: -z\n" + usage},
		{"help beats unsupported", []string{"-O", "-h"}, cli.ExitOK, "usage: spry", ""},
		{"unsupported beats version", []string{"-V", "-O"}, cli.ExitUsage, "", "spry: option -O is not supported yet\n"},
		{"version before command", []string{"-V", "-c", "x"}, cli.ExitOK, "Python 3.14.0 (spry " + cli.Release + ")\n", ""},
		{"flag unit beats operand unit", []string{"-V", "x.py"}, cli.ExitOK, "Python 3.14.0 (spry " + cli.Release + ")\n", ""},
		{"-c takes the rest", []string{"-c", "pass", "-h", "-z"}, cli.ExitFailure, "", "spry: execution not implemented yet (phase P3)\n"},
		{"stdin by default", nil, cli.ExitFailure, "", "spry: execution not implemented yet (phase P3)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, cli.Default(), tt.args...)
			require.Equal(t, tt.code, code)
			require.True(t, strings.HasPrefix(stdout, tt.stdout), stdout)
			require.Equal(t, tt.stderr, stderr)
		})
	}
}

func run(t *testing.T, units []*cobra.Command, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.New(units...).Run(context.Background(), cli.Process{
		Args: args, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Dir: t.TempDir(),
	})
	return code, stdout.String(), stderr.String()
}
