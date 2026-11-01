package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/cli"
)

func TestVersion(t *testing.T) {
	only := []cli.Component{cli.Version{}}
	tests := []struct {
		name   string
		args   []string
		action cli.Action
		want   string
	}{
		{"short", []string{"-V"}, cli.ActionVersion, "Python 3.14.0 (spry " + cli.Release + ")\n"},
		{"long", []string{"--version"}, cli.ActionVersion, "Python 3.14.0 (spry " + cli.Release + ")\n"},
		{"verbose", []string{"-VV"}, cli.ActionVersionVerbose, "Python 3.14.0 (spry " + cli.Release + ") [Go go"},
		{"twice", []string{"-V", "--version"}, cli.ActionVersionVerbose, "Python 3.14.0 (spry " + cli.Release + ") [Go go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := parseWith(only, nil, tt.args...)
			require.NoError(t, err)
			require.Equal(t, tt.action, opts.Action)

			code, stdout, _ := runWith(t, only, tt.args...)
			require.Equal(t, cli.ExitOK, code)
			require.Contains(t, stdout, tt.want)
		})
	}
}

func TestVersionDeferred(t *testing.T) {
	_, err := parseWith([]cli.Component{cli.Version{}}, nil, "-V", "-z")
	require.EqualError(t, err, "Unknown option: -z")
}
