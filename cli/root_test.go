package cli_test

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/cli"
)

func TestDefaultComponents(t *testing.T) {
	var names []string
	for _, c := range cli.DefaultComponents() {
		names = append(names, reflect.TypeOf(c).Name())
	}
	require.Equal(t, []string{"Help", "Version", "Interpreter", "XOptions", "Command", "Module", "File", "Stdin"}, names)
}

func TestParse(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		check func(t *testing.T, o cli.Options)
	}{
		{"empty is stdin", nil, func(t *testing.T, o cli.Options) {
			require.Equal(t, cli.ModeStdin, o.Mode)
			require.Equal(t, []string{""}, o.Argv())
		}},
		{"dash is stdin", []string{"-", "a"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, cli.ModeStdin, o.Mode)
			require.Equal(t, "-", o.Script)
			require.Equal(t, []string{"a"}, o.Args)
		}},
		{"script and args", []string{"-B", "x.py", "-z", "--help"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, cli.ModeScript, o.Mode)
			require.True(t, o.DontWriteByte)
			require.Equal(t, []string{"x.py", "-z", "--help"}, o.Argv())
		}},
		{"double dash script", []string{"--", "-c"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, cli.ModeScript, o.Mode)
			require.Equal(t, "-c", o.Script)
		}},
		{"double dash alone", []string{"--"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, cli.ModeStdin, o.Mode)
		}},
		{"command separate", []string{"-c", "print(1)", "-z"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, cli.ModeCommand, o.Mode)
			require.Equal(t, "print(1)", o.Command)
			require.Equal(t, []string{"-c", "-z"}, o.Argv())
		}},
		{"command attached", []string{"-Ecprint(1)", "a"}, func(t *testing.T, o cli.Options) {
			require.True(t, o.IgnoreEnv)
			require.Equal(t, "print(1)", o.Command)
			require.Equal(t, []string{"a"}, o.Args)
		}},
		{"module", []string{"-m", "pkg.mod", "-h"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, cli.ModeModule, o.Mode)
			require.Equal(t, []string{"pkg.mod", "-h"}, o.Argv())
		}},
		{"counted flags", []string{"-OObbvv", "-d", "-i", "-q", "-s", "-S", "-u", "-x", "-P"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, 2, o.Optimize)
			require.Equal(t, 2, o.Bytes)
			require.Equal(t, 2, o.Verbose)
			require.True(t, o.Debug && o.Inspect && o.Quiet && o.NoUserSite && o.NoSite && o.Unbuffered && o.SkipFirstLine && o.SafePath)
		}},
		{"isolated implies", []string{"-I"}, func(t *testing.T, o cli.Options) {
			require.True(t, o.Isolated && o.IgnoreEnv && o.SafePath && o.NoUserSite)
		}},
		{"warnings attached and separate", []string{"-Wd", "-W", "error::X", "-c", "pass"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, []string{"d", "error::X"}, o.Warnings)
		}},
		{"W swallows dash arg", []string{"-W", "-c", "x"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, []string{"-c"}, o.Warnings)
			require.Equal(t, "x", o.Script)
		}},
		{"xoptions", []string{"-Xdev", "-X", "spry.opt=3", "-X", "spry.dump=ast", "-X", "spry.fallback=error", "-X", "spry.emit=out.bin", "-X", "utf8=0"}, func(t *testing.T, o cli.Options) {
			require.Len(t, o.XOptions, 6)
			require.Equal(t, cli.SpryOptions{Dump: "ast", Opt: 3, Fallback: "error", Emit: "out.bin"}, o.Spry)
		}},
		{"spry defaults", nil, func(t *testing.T, o cli.Options) {
			require.Equal(t, cli.SpryOptions{Opt: cli.OptDefault}, o.Spry)
		}},
		{"hash pycs", []string{"--check-hash-based-pycs", "never", "x.py"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, "never", o.CheckHashPycs)
			require.Equal(t, "x.py", o.Script)
		}},
		{"version", []string{"-V", "-c", "pass"}, func(t *testing.T, o cli.Options) { require.Equal(t, cli.ActionVersion, o.Action) }},
		{"version long", []string{"--version"}, func(t *testing.T, o cli.Options) { require.Equal(t, cli.ActionVersion, o.Action) }},
		{"version verbose", []string{"-VV"}, func(t *testing.T, o cli.Options) { require.Equal(t, cli.ActionVersionVerbose, o.Action) }},
		{"version twice", []string{"-V", "--version"}, func(t *testing.T, o cli.Options) { require.Equal(t, cli.ActionVersionVerbose, o.Action) }},
		{"help", []string{"-h", "-z"}, func(t *testing.T, o cli.Options) { require.Equal(t, cli.ActionHelp, o.Action) }},
		{"help question", []string{"-?"}, func(t *testing.T, o cli.Options) { require.Equal(t, cli.ActionHelp, o.Action) }},
		{"help long", []string{"--help"}, func(t *testing.T, o cli.Options) { require.Equal(t, cli.ActionHelp, o.Action) }},
		{"help env", []string{"-V", "--help-env"}, func(t *testing.T, o cli.Options) { require.Equal(t, cli.ActionHelpEnv, o.Action) }},
		{"help xoptions", []string{"--help-xoptions"}, func(t *testing.T, o cli.Options) { require.Equal(t, cli.ActionHelpXOptions, o.Action) }},
		{"help all", []string{"--help-all"}, func(t *testing.T, o cli.Options) { require.Equal(t, cli.ActionHelpAll, o.Action) }},
		{"cut inside group separate", []string{"-Ec", "pass", "-z"}, func(t *testing.T, o cli.Options) {
			require.True(t, o.IgnoreEnv)
			require.Equal(t, "pass", o.Command)
			require.Equal(t, []string{"-z"}, o.Args)
		}},
		{"cut group keeps earlier letters", []string{"-OOcx", "-O"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, 2, o.Optimize)
			require.Equal(t, "x", o.Command)
			require.Equal(t, []string{"-O"}, o.Args)
		}},
		{"W value is not a cut", []string{"-Wc", "-c", "pass"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, []string{"c"}, o.Warnings)
			require.Equal(t, "pass", o.Command)
		}},
		{"X swallows -c", []string{"-X", "-c", "x.py"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, []string{"-c"}, o.XOptions)
			require.Equal(t, "x.py", o.Script)
		}},
		{"c after script is an argument", []string{"x.py", "-c", "y"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, cli.ModeScript, o.Mode)
			require.Equal(t, []string{"-c", "y"}, o.Args)
		}},
		{"c after double dash is a script", []string{"--", "-c", "y"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, "-c", o.Script)
			require.Equal(t, []string{"y"}, o.Args)
		}},
		{"hash value is skipped by the cut", []string{"--check-hash-based-pycs", "never", "-m", "mod"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, "never", o.CheckHashPycs)
			require.Equal(t, "mod", o.Module)
		}},
		{"first help wins", []string{"--help-env", "--help"}, func(t *testing.T, o cli.Options) { require.Equal(t, cli.ActionHelpEnv, o.Action) }},
		{"first help wins reversed", []string{"-h", "--help-all"}, func(t *testing.T, o cli.Options) { require.Equal(t, cli.ActionHelp, o.Action) }},
		{"W equals is part of the value", []string{"-W=x", "-X=y", "-EX=z", "-c", "pass"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, []string{"=x"}, o.Warnings)
			require.Equal(t, []string{"=y", "=z"}, o.XOptions)
		}},
		{"W separate equals", []string{"-W", "=x", "a.py"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, []string{"=x"}, o.Warnings)
		}},
		{"script named __complete", []string{"__complete", "x"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, cli.ModeScript, o.Mode)
			require.Equal(t, "__complete", o.Script)
			require.Equal(t, []string{"x"}, o.Args)
		}},
		{"script named __completeNoDesc", []string{"__completeNoDesc"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, "__completeNoDesc", o.Script)
		}},
		{"script named completion", []string{"completion"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, cli.ModeScript, o.Mode)
			require.Equal(t, "completion", o.Script)
		}},
		{"script named help", []string{"help", "me"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, "help", o.Script)
			require.Equal(t, []string{"me"}, o.Args)
		}},
		{"flag then __complete", []string{"-E", "__complete"}, func(t *testing.T, o cli.Options) {
			require.True(t, o.IgnoreEnv)
			require.Equal(t, "__complete", o.Script)
		}},
		{"argv of stdin", []string{"-", "a"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, []string{"-", "a"}, o.Argv())
		}},
		{"command swallows help", []string{"-c", "pass", "-h"}, func(t *testing.T, o cli.Options) {
			require.Equal(t, cli.ActionRun, o.Action)
			require.Equal(t, []string{"-c", "-h"}, o.Argv())
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := cli.Parse(tt.args)
			require.NoError(t, err)
			tt.check(t, opts)
		})
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		message string
		noUsage bool
	}{
		{"unknown short", []string{"-z"}, "Unknown option: -z", false},
		{"unknown in group", []string{"-OOz"}, "Unknown option: -z", false},
		{"unknown long", []string{"--foo=bar"}, "Unknown option: --foo=bar", false},
		{"unknown after version", []string{"-V", "-z"}, "Unknown option: -z", false},
		{"missing c", []string{"-c"}, "Argument expected for the -c option", false},
		{"missing m", []string{"-Em"}, "Argument expected for the -m option", false},
		{"missing W", []string{"-W"}, "Argument expected for the -W option", false},
		{"missing X", []string{"-X"}, "Argument expected for the -X option", false},
		{"missing hash", []string{"--check-hash-based-pycs"}, "Argument expected for the --check-hash-based-pycs options", false},
		{"bad hash", []string{"--check-hash-based-pycs", "x"}, "--check-hash-based-pycs must be one of 'default', 'always', or 'never'", false},
		{"long flag name of a short option", []string{"--optimize"}, "Unknown option: --optimize", false},
		{"hash with equals", []string{"--check-hash-based-pycs=never"}, "Unknown option: --check-hash-based-pycs=never", false},
		{"error before cut missing value", []string{"-z", "-c"}, "Unknown option: -z", false},
		{"bad hash before unknown", []string{"--check-hash-based-pycs", "x", "-z"}, "--check-hash-based-pycs must be one of 'default', 'always', or 'never'", false},
		{"signed opt", []string{"-X", "spry.opt=+1"}, `invalid -X option "spry.opt=+1": expected an integer from 0 to 3`, true},
		{"negative zero opt", []string{"-X", "spry.opt=-0"}, `invalid -X option "spry.opt=-0": expected an integer from 0 to 3`, true},
		{"missing W in group", []string{"-OW"}, "Argument expected for the -W option", false},
		{"unknown after valid group", []string{"-Eqz"}, "Unknown option: -z", false},
		{"bad opt", []string{"-X", "spry.opt=4"}, `invalid -X option "spry.opt=4": expected an integer from 0 to 3`, true},
		{"bad opt text", []string{"-X", "spry.opt=x"}, `invalid -X option "spry.opt=x": expected an integer from 0 to 3`, true},
		{"opt no value", []string{"-X", "spry.opt"}, `invalid -X option "spry.opt": expected an integer from 0 to 3`, true},
		{"bad dump", []string{"-X", "spry.dump=x"}, `invalid -X option "spry.dump=x": expected one of tokens, ast, types, ssa, asm`, true},
		{"bad fallback", []string{"-X", "spry.fallback"}, `invalid -X option "spry.fallback": expected one of warn, error`, true},
		{"empty emit", []string{"-X", "spry.emit="}, `invalid -X option "spry.emit=": expected a file path`, true},
		{"unknown spry", []string{"-X", "spry.x=1"}, `invalid -X option "spry.x=1": unknown spry option`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := cli.Parse(tt.args)
			var usage *cli.UsageError
			require.ErrorAs(t, err, &usage)
			require.Equal(t, tt.message, usage.Message)
			require.Equal(t, tt.noUsage, usage.NoUsage)
			require.EqualError(t, err, tt.message)
		})
	}
}

func TestParseEnv(t *testing.T) {
	opts, err := cli.ParseEnv([]string{"-c", "pass"}, []string{"PYTHONOPTIMIZE=2", "PYTHONWARNINGS=error"})
	require.NoError(t, err)
	require.Equal(t, 2, opts.Optimize)
	require.Equal(t, []string{"error"}, opts.Warnings)

	opts, err = cli.ParseEnv([]string{"-E", "-c", "pass"}, []string{"PYTHONOPTIMIZE=2"})
	require.NoError(t, err)
	require.Zero(t, opts.Optimize)

	_, err = cli.ParseEnv([]string{"-z"}, nil)
	require.Error(t, err)
}

func TestNewRoot(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := cli.NewRoot(cli.Process{Stdout: &stdout, Stderr: &stderr}, cli.DefaultComponents()...)
	require.Equal(t, cli.ExitOK, root.Run(context.Background(), []string{"-V"}))
	require.Contains(t, stdout.String(), "Python 3.14.0")
}

func TestRoot(t *testing.T) {
	// Removing a component removes exactly its command line surface.
	without := func(drop string) []cli.Component {
		var out []cli.Component
		for _, c := range cli.DefaultComponents() {
			if reflect.TypeOf(c).Name() != drop {
				out = append(out, c)
			}
		}
		return out
	}
	_, err := parseWith(without("Module"), nil, "-m", "x")
	require.EqualError(t, err, "Unknown option: -m")
	_, err = parseWith(without("Interpreter"), nil, "-O")
	require.EqualError(t, err, "Unknown option: -O")
	_, err = parseWith(without("Help"), nil, "--help-env")
	require.EqualError(t, err, "Unknown option: --help-env")
	opts, err := parseWith(without("Command"), nil, "-E", "x.py")
	require.NoError(t, err)
	require.Equal(t, "x.py", opts.Script)
}
