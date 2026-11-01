package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/cli"
)

func TestFile(t *testing.T) {
	only := []cli.Component{cli.File{}}
	opts, err := parseWith(only, nil, "x.py", "-z", "a")
	require.NoError(t, err)
	require.Equal(t, cli.ModeScript, opts.Mode)
	require.Equal(t, []string{"x.py", "-z", "a"}, opts.Argv())

	code, _, stderr := runWith(t, only, "missing.py")
	require.Equal(t, cli.ExitUsage, code)
	require.Contains(t, stderr, "can't open file")

	code, _, stderr = runWith(t, only, "-")
	require.Equal(t, cli.ExitUsage, code)
	require.Contains(t, stderr, "no mode selected")
}
