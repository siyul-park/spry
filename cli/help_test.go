package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/cli"
)

func TestHelp(t *testing.T) {
	only := []cli.Component{cli.Help{}}
	tests := []struct {
		name   string
		args   []string
		action cli.Action
		prefix string
	}{
		{"short", []string{"-h"}, cli.ActionHelp, "usage: spry"},
		{"question", []string{"-?"}, cli.ActionHelp, "usage: spry"},
		{"long", []string{"--help"}, cli.ActionHelp, "usage: spry"},
		{"env", []string{"--help-env"}, cli.ActionHelpEnv, "Environment variables"},
		{"xoptions", []string{"--help-xoptions"}, cli.ActionHelpXOptions, "The following implementation-specific"},
		{"all", []string{"--help-all"}, cli.ActionHelpAll, "usage: spry"},
		{"first wins", []string{"--help-env", "-h"}, cli.ActionHelpEnv, "Environment variables"},
		{"wins over a later error", []string{"-h", "-z"}, cli.ActionHelp, "usage: spry"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := parseWith(only, nil, tt.args...)
			require.NoError(t, err)
			require.Equal(t, tt.action, opts.Action)

			code, stdout, stderr := runWith(t, only, tt.args...)
			require.Equal(t, cli.ExitOK, code)
			require.Empty(t, stderr)
			require.Contains(t, stdout, tt.prefix)
		})
	}
}

func TestHelpAbsent(t *testing.T) {
	_, err := parseWith([]cli.Component{cli.Version{}}, nil, "-h")
	require.EqualError(t, err, "Unknown option: -h")
}
