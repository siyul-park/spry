package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/cli"
	"github.com/siyul-park/spry/internal/cpython"
)

const dumpDir = "testdata/dump"

const usage = "usage: spry [option] ... [-c cmd | -m mod | file | -] [arg] ...\nTry `spry -h' for more information.\n"

func TestXOptions(t *testing.T) {
	t.Run("dump goldens", func(t *testing.T) {
		var python string
		if *update {
			python = lookupPython(t)
		}
		sources, err := filepath.Glob(filepath.Join(dumpDir, "*.py"))
		require.NoError(t, err)
		require.NotEmpty(t, sources)
		for _, source := range sources {
			base := strings.TrimSuffix(source, ".py")
			t.Run(filepath.Base(base), func(t *testing.T) {
				if *update {
					record(t, python, source, base)
				}
				tokens, err := os.ReadFile(base + ".tokens")
				require.NoError(t, err)
				tree, err := os.ReadFile(base + ".ast")
				require.NoError(t, err)

				require.Equal(t, string(tokens), dump(t, "tokens", source))
				require.Equal(t, string(tree), dump(t, "ast", source))
				require.Equal(t, string(tokens)+string(tree), dump(t, "tokens,ast", source))
				require.Equal(t, string(tokens)+string(tree), dump(t, "ast,tokens", source))
			})
		}
	})

	t.Run("dump matches CPython", func(t *testing.T) {
		python := lookupPython(t)
		sources, err := filepath.Glob(filepath.Join(dumpDir, "*.py"))
		require.NoError(t, err)
		for _, source := range sources {
			t.Run(filepath.Base(source), func(t *testing.T) {
				tokens, tree := oracle(t, python, source)
				require.Equal(t, tokens, dump(t, "tokens", source))
				require.Equal(t, tree, dump(t, "ast", source))
			})
		}
	})

	tests := []struct {
		name   string
		args   []string
		stdin  string
		code   int
		stderr string
	}{
		{
			"command",
			[]string{"-X", "spry.dump=tokens", "-X", "spry.dump.exit", "-c", "x = 1"},
			"", cli.ExitOK,
			"1,0-1,1:            NAME           'x'            \n" +
				"1,2-1,3:            OP             '='            \n" +
				"1,4-1,5:            NUMBER         '1'            \n" +
				"1,5-1,6:            NEWLINE        ''             \n" +
				"2,0-2,0:            ENDMARKER      ''             \n",
		},
		{
			"stdin",
			[]string{"-Xspry.dump=ast", "-Xspry.dump.exit"},
			"x\n", cli.ExitOK,
			"Module(\n   body=[\n      Expr(\n         value=Name(id='x', ctx=Load()))])\n",
		},
		{
			"stdin operand",
			[]string{"-Xspry.dump=ast", "-Xspry.dump.exit", "-"},
			"pass\n", cli.ExitOK,
			"Module(\n   body=[\n      Pass()])\n",
		},
		{"continues without spry.dump.exit", []string{"-X", "spry.dump=ast", "-c", "pass"}, "", cli.ExitFailure,
			"Module(\n   body=[\n      Pass()])\nspry: execution not implemented yet (phase P3)\n"},
		{"spry.dump.exit alone", []string{"-X", "spry.dump.exit", "-c", "pass"}, "", cli.ExitOK, ""},
		{"the last spry.dump wins", []string{"-X", "spry.dump=tokens", "-X", "spry.dump=ast", "-X", "spry.dump.exit", "-c", "pass"}, "", cli.ExitOK,
			"Module(\n   body=[\n      Pass()])\n"},
		{
			"tokens before the syntax error",
			[]string{"-X", "spry.dump=tokens", "-c", "x = ("}, "", cli.ExitFailure,
			"1,0-1,1:            NAME           'x'            \n" +
				"1,2-1,3:            OP             '='            \n" +
				"1,4-1,5:            OP             '('            \n" +
				"1,5-1,6:            NL             ''             \n" +
				"  File \"<string>\", line 1\n    x = (\n        ^\nSyntaxError: '(' was never closed\n",
		},
		{"no ast for a syntax error", []string{"-X", "spry.dump=ast", "-c", "x = ("}, "", cli.ExitFailure,
			"  File \"<string>\", line 1\n    x = (\n        ^\nSyntaxError: '(' was never closed\n"},
		{"unsupported stage", []string{"-X", "spry.dump=types", "-c", "pass"}, "", cli.ExitUsage, "spry: option -X is not supported yet\n"},
		{"unsupported stage in list", []string{"-X", "spry.dump=ast,ssa", "-c", "pass"}, "", cli.ExitUsage, "spry: option -X is not supported yet\n"},
		{"empty stage", []string{"-X", "spry.dump=", "-c", "pass"}, "", cli.ExitUsage, "spry: option -X is not supported yet\n"},
		{"unknown stage", []string{"-X", "spry.dump=nope", "-c", "pass"}, "", cli.ExitUsage, "spry: option -X is not supported yet\n"},
		{"other spry option", []string{"-X", "spry.opt=2", "-c", "pass"}, "", cli.ExitUsage, "spry: option -X is not supported yet\n"},
		{"cpython option", []string{"-X", "dev", "-c", "pass"}, "", cli.ExitUsage, "spry: option -X is not supported yet\n"},
		{"missing value", []string{"-X"}, "", cli.ExitUsage, "Argument expected for the -X option\n" + usage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := cli.Run(context.Background(), cli.Process{Args: tt.args, Stdin: strings.NewReader(tt.stdin), Stdout: &stdout, Stderr: &stderr, Dir: t.TempDir()})
			require.Equal(t, tt.code, code)
			require.Empty(t, stdout.String())
			require.Equal(t, tt.stderr, stderr.String())
		})
	}

	t.Run("help beats an unsupported value", func(t *testing.T) {
		code, stdout, _ := run(t, cli.Default(), "-X", "dev", "-h")
		require.Equal(t, cli.ExitOK, code)
		require.True(t, strings.HasPrefix(stdout, "usage: spry"))
	})
	t.Run("without the unit -X is unsupported", func(t *testing.T) {
		code, _, stderr := run(t, []*cobra.Command{cli.Command()}, "-X", "spry.dump=ast", "-c", "pass")
		require.Equal(t, cli.ExitUsage, code)
		require.Equal(t, "spry: option -X is not supported yet\n", stderr)
	})
	t.Run("the unit selects no action", func(t *testing.T) {
		code, _, stderr := run(t, []*cobra.Command{cli.XOptions()}, "-X", "spry.dump=ast")
		require.Equal(t, cli.ExitFailure, code)
		require.Equal(t, "spry: no command selected\n", stderr)
	})
	t.Run("file", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a.py"), []byte("pass\n"), 0o644))
		var stderr bytes.Buffer
		code := cli.Run(context.Background(), cli.Process{Args: []string{"-X", "spry.dump=ast", "-X", "spry.dump.exit", "a.py"}, Stderr: &stderr, Dir: dir})
		require.Equal(t, cli.ExitOK, code)
		require.Equal(t, "Module(\n   body=[\n      Pass()])\n", stderr.String())
	})
}

// dump runs spry on a file with the given spry.dump stages and returns what
// it printed to stderr.
func dump(t *testing.T, stages, source string) string {
	t.Helper()
	abs, err := filepath.Abs(source)
	require.NoError(t, err)
	var stdout, stderr bytes.Buffer
	code := cli.Run(context.Background(), cli.Process{
		Args:   []string{"-X", "spry.dump=" + stages, "-X", "spry.dump.exit", abs},
		Stdout: &stdout, Stderr: &stderr,
	})
	require.Equal(t, cli.ExitOK, code, stderr.String())
	require.Empty(t, stdout.String())
	return stderr.String()
}

func record(t *testing.T, python, source, base string) {
	t.Helper()
	tokens, tree := oracle(t, python, source)
	require.NoError(t, os.WriteFile(base+".tokens", []byte(tokens), 0o644))
	require.NoError(t, os.WriteFile(base+".ast", []byte(tree), 0o644))
}

// oracle returns `python -m tokenize < source` and `python -m ast source`.
func oracle(t *testing.T, python, source string) (tokens, tree string) {
	t.Helper()
	abs, err := filepath.Abs(source)
	require.NoError(t, err)
	raw, err := os.ReadFile(abs)
	require.NoError(t, err)
	res, err := cpython.Run(context.Background(), python, "", raw, "-m", "tokenize")
	require.NoError(t, err)
	require.Zero(t, res.ExitCode, res.Stderr)
	tokens = res.Stdout
	res, err = cpython.Run(context.Background(), python, "", nil, "-m", "ast", abs)
	require.NoError(t, err)
	require.Zero(t, res.ExitCode, res.Stderr)
	return tokens, res.Stdout
}
