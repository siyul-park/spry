package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/cli"
)

func TestCommand(t *testing.T) {
	only := []cli.Component{cli.Command{}}
	opts, err := parseWith(only, nil, "-cprint(1)", "a")
	require.NoError(t, err)
	require.Equal(t, cli.ModeCommand, opts.Mode)
	require.Equal(t, "print(1)", opts.Command)
	require.Equal(t, []string{"-c", "a"}, opts.Argv())

	code, _, stderr := runWith(t, only, "-c", "pass")
	require.Equal(t, cli.ExitFailure, code)
	require.Contains(t, stderr, "phase P3")

	_, err = parseWith(only, nil, "-c")
	require.EqualError(t, err, "Argument expected for the -c option")
	_, err = parseWith(only, nil, "-m", "mod")
	require.EqualError(t, err, "Unknown option: -m")
}
