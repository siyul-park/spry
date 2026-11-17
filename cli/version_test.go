package cli_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/cli"
)

func TestVersion(t *testing.T) {
	line := "Python 3.14.0 (spry " + cli.Release + ")"
	for _, flag := range []string{"-V", "--version"} {
		t.Run(flag, func(t *testing.T) {
			code, stdout, stderr := run(t, units(cli.Version()), flag)
			require.Equal(t, cli.ExitOK, code)
			require.Equal(t, line+"\n", stdout)
			require.Empty(t, stderr)
		})
	}
	t.Run("-VV is verbose", func(t *testing.T) {
		_, stdout, _ := run(t, units(cli.Version()), "-VV")
		require.True(t, strings.HasPrefix(stdout, line+" [Go go"), stdout)
	})
	t.Run("-V -V is verbose", func(t *testing.T) {
		_, stdout, _ := run(t, units(cli.Version()), "-V", "-V")
		require.True(t, strings.HasPrefix(stdout, line+" [Go go"), stdout)
	})
}
