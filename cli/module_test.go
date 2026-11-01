package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/cli"
)

func TestModule(t *testing.T) {
	only := []cli.Component{cli.Module{}}
	opts, err := parseWith(only, nil, "-m", "pkg.mod", "-h")
	require.NoError(t, err)
	require.Equal(t, cli.ModeModule, opts.Mode)
	require.Equal(t, "pkg.mod", opts.Module)
	require.Equal(t, []string{"pkg.mod", "-h"}, opts.Argv())

	code, _, stderr := runWith(t, only, "-mx")
	require.Equal(t, cli.ExitFailure, code)
	require.Contains(t, stderr, "phase P3")

	_, err = parseWith(only, nil, "-m")
	require.EqualError(t, err, "Argument expected for the -m option")
}
