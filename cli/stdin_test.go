package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/cli"
)

func TestStdin(t *testing.T) {
	only := []cli.Component{cli.Stdin{}}
	opts, err := parseWith(only, nil)
	require.NoError(t, err)
	require.Equal(t, cli.ModeStdin, opts.Mode)
	require.Equal(t, []string{""}, opts.Argv())

	opts, err = parseWith(only, nil, "-", "a")
	require.NoError(t, err)
	require.Equal(t, []string{"-", "a"}, opts.Argv())

	code, _, stderr := runWith(t, only)
	require.Equal(t, cli.ExitFailure, code)
	require.Contains(t, stderr, "phase P3")

	code, _, stderr = runWith(t, only, "script.py")
	require.Equal(t, cli.ExitUsage, code)
	require.Contains(t, stderr, "no mode selected")
}
