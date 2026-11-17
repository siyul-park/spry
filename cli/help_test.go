package cli_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/cli"
)

func TestHelp(t *testing.T) {
	for _, flag := range []string{"-h", "-?", "--help", "-Eh"} {
		t.Run(flag, func(t *testing.T) {
			args := []string{flag}
			if flag == "-Eh" {
				args = []string{"-h"}
			}
			code, stdout, stderr := run(t, units(cli.Help()), args...)
			require.Equal(t, cli.ExitOK, code)
			require.True(t, strings.HasPrefix(stdout, "usage: spry [option] ... [-c cmd | -m mod | file | -] [arg] ...\nOptions (and corresponding environment variables):\n"))
			require.Empty(t, stderr)
		})
	}
	t.Run("alone it does not know other flags", func(t *testing.T) {
		code, _, stderr := run(t, units(cli.Help()), "-V")
		require.Equal(t, cli.ExitUsage, code)
		require.Equal(t, "spry: option -V is not supported yet\n", stderr)
	})
}
