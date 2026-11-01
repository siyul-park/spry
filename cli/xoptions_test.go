package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/cli"
)

func TestXOptions(t *testing.T) {
	only := []cli.Component{cli.XOptions{}}
	opts, err := parseWith(only, nil, "-Xdev", "-X", "spry.opt=2", "-X", "spry.opt=3", "-X", "spry.dump=ssa", "-X", "spry.fallback=error", "-X", "spry.emit=out.bin", "-X", "other=1")
	require.NoError(t, err)
	require.Len(t, opts.XOptions, 7)
	require.Equal(t, cli.SpryOptions{Dump: "ssa", Opt: 3, Fallback: "error", Emit: "out.bin"}, opts.Spry)

	opts, err = parseWith(only, nil)
	require.NoError(t, err)
	require.Equal(t, cli.SpryOptions{Opt: cli.OptDefault}, opts.Spry)
}

func TestXOptionsInvalid(t *testing.T) {
	_, err := parseWith([]cli.Component{cli.XOptions{}}, nil, "-X", "spry.opt=4")
	var usage *cli.UsageError
	require.ErrorAs(t, err, &usage)
	require.True(t, usage.NoUsage)
}
