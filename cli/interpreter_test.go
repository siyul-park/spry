package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/cli"
)

func TestInterpreter(t *testing.T) {
	only := []cli.Component{cli.Interpreter{}}
	tests := []struct {
		name    string
		args    []string
		environ []string
		check   func(t *testing.T, o cli.Options)
	}{
		{"flags", []string{"-bbBdEiOOPqsSuvvx"}, nil, func(t *testing.T, o cli.Options) {
			require.Equal(t, 2, o.Bytes)
			require.Equal(t, 2, o.Optimize)
			require.Equal(t, 2, o.Verbose)
			require.True(t, o.DontWriteByte && o.Debug && o.IgnoreEnv && o.Inspect && o.SafePath && o.Quiet && o.NoUserSite && o.NoSite && o.Unbuffered && o.SkipFirstLine)
		}},
		{"isolated implies", []string{"-I"}, nil, func(t *testing.T, o cli.Options) {
			require.True(t, o.Isolated && o.IgnoreEnv && o.SafePath && o.NoUserSite)
		}},
		{"warnings", []string{"-Wd", "-W", "error"}, nil, func(t *testing.T, o cli.Options) {
			require.Equal(t, []string{"d", "error"}, o.Warnings)
		}},
		{"hash", []string{"--check-hash-based-pycs", "always"}, nil, func(t *testing.T, o cli.Options) {
			require.Equal(t, "always", o.CheckHashPycs)
		}},
		{"env booleans", nil, []string{"PYTHONDEBUG=1", "PYTHONDONTWRITEBYTECODE=x", "PYTHONINSPECT=0", "PYTHONNOUSERSITE=1", "PYTHONSAFEPATH=1", "PYTHONUNBUFFERED=1"}, func(t *testing.T, o cli.Options) {
			require.True(t, o.Debug && o.DontWriteByte && o.Inspect && o.NoUserSite && o.SafePath && o.Unbuffered)
			require.False(t, o.IgnoreEnv)
		}},
		{"env empty is unset", nil, []string{"PYTHONSAFEPATH=", "PYTHONOPTIMIZE="}, func(t *testing.T, o cli.Options) {
			require.False(t, o.SafePath)
			require.Zero(t, o.Optimize)
		}},
		{"env levels", nil, []string{"PYTHONOPTIMIZE=2", "PYTHONVERBOSE=yes"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, 2, o.Optimize)
			require.Equal(t, 1, o.Verbose)
		}},
		{"flags raise the env level", []string{"-OOO"}, []string{"PYTHONOPTIMIZE=1", "PYTHONVERBOSE=-4"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, 3, o.Optimize)
			require.Zero(t, o.Verbose)
		}},
		{"env warnings come first", []string{"-Wlast"}, []string{"PYTHONWARNINGS=a,b"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, []string{"a", "b", "last"}, o.Warnings)
		}},
		{"-E ignores env", []string{"-E"}, []string{"PYTHONSAFEPATH=1", "PYTHONWARNINGS=a"}, func(t *testing.T, o cli.Options) {
			require.False(t, o.SafePath)
			require.Empty(t, o.Warnings)
		}},
		{"-I ignores env", []string{"-I"}, []string{"PYTHONOPTIMIZE=2", "malformed"}, func(t *testing.T, o cli.Options) {
			require.Zero(t, o.Optimize)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := parseWith(only, tt.environ, tt.args...)
			require.NoError(t, err)
			tt.check(t, opts)
		})
	}
}

func TestInterpreterErrors(t *testing.T) {
	only := []cli.Component{cli.Interpreter{}}
	_, err := parseWith(only, nil, "--check-hash-based-pycs", "bad")
	require.EqualError(t, err, "--check-hash-based-pycs must be one of 'default', 'always', or 'never'")
	_, err = parseWith(only, nil, "--check-hash-based-pycs")
	require.EqualError(t, err, "Argument expected for the --check-hash-based-pycs options")
	_, err = parseWith(only, nil, "-W")
	require.EqualError(t, err, "Argument expected for the -W option")
	_, err = parseWith(only, nil, "-X", "dev")
	require.EqualError(t, err, "Unknown option: -X")
}
