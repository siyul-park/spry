package cli_test

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/cli"
)

type trace struct {
	name  string
	items *[]string
}

func (v trace) String() string { return "" }

func (v trace) Type() string { return "trace" }

func (v trace) Set(value string) error {
	*v.items = append(*v.items, v.name+"="+value)
	return nil
}

func register(flags *pflag.FlagSet, items *[]string) {
	for _, letter := range "bBdEhiIOPqsSuvVxX?" {
		name := string(letter)
		flags.VarPF(trace{name: name, items: items}, name, name, "").NoOptDefVal = "1"
	}
	for _, letter := range "cmW" {
		name := string(letter)
		flags.VarPF(trace{name: name, items: items}, name, name, "")
	}
	for _, name := range []string{"help", "version", "help-env", "help-xoptions", "help-all"} {
		flags.VarPF(trace{name: name, items: items}, name, "", "").NoOptDefVal = "1"
	}
	flags.VarPF(trace{name: "check-hash-based-pycs", items: items}, "check-hash-based-pycs", "", "")
}

// The grammar is CPython 3.14 Python/getopt.c: one probe unit registers every
// option of that grammar and records what the parser hands to the flags.
func TestGetopt(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		options  []string
		operands []string
		stderr   string
	}{
		{"empty", nil, nil, nil, ""},
		{"combined", []string{"-Ec", "x"}, []string{"E=1", "c=x"}, nil, ""},
		{"attached value", []string{"-cprint(1)"}, []string{"c=print(1)"}, nil, ""},
		{"count", []string{"-OOW", "x"}, []string{"O=1", "O=1", "W=x"}, nil, ""},
		{"equals stays in value", []string{"-W=x"}, []string{"W==x"}, nil, ""},
		{"c takes the rest", []string{"-c", "pass", "-h", "-z"}, []string{"c=pass"}, []string{"-h", "-z"}, ""},
		{"m takes the rest", []string{"-m", "mod", "-z"}, []string{"m=mod"}, []string{"-z"}, ""},
		{"group before c", []string{"-Ecpass", "-z"}, []string{"E=1", "c=pass"}, []string{"-z"}, ""},
		{"operand stops options", []string{"-u", "x.py", "-z"}, []string{"u=1"}, []string{"x.py", "-z"}, ""},
		{"double dash", []string{"--", "-c"}, nil, []string{"-c"}, ""},
		{"dash is an operand", []string{"-", "-z"}, nil, []string{"-", "-z"}, ""},
		{"long help", []string{"--help"}, []string{"help=1"}, nil, ""},
		{"long version", []string{"--version"}, []string{"version=1"}, nil, ""},
		{"long help-env", []string{"--help-env"}, []string{"help-env=1"}, nil, ""},
		{"long help-all", []string{"--help-all"}, []string{"help-all=1"}, nil, ""},
		{"long value", []string{"--check-hash-based-pycs", "always"}, []string{"check-hash-based-pycs=always"}, nil, ""},
		{"question", []string{"-?"}, []string{"?=1"}, nil, ""},
		{"unknown short", []string{"-z"}, nil, nil, "Unknown option: -z\n"},
		{"unknown in group", []string{"-Ez"}, nil, nil, "Unknown option: -z\n"},
		{"unknown long", []string{"--foo"}, nil, nil, "Unknown option: --foo\n"},
		{"long with equals", []string{"--version=3"}, nil, nil, "Unknown option: --version=3\n"},
		{"long abbreviation", []string{"--help-x"}, nil, nil, "Unknown option: --help-x\n"},
		{"long inside a group", []string{"-E-help"}, nil, nil, "Unknown option: -E-help\n"},
		{"colon", []string{"-:"}, nil, nil, "Unknown option: -:\n"},
		{"jython", []string{"-J"}, nil, nil, "-J is reserved for Jython\n"},
		{"missing short value", []string{"-W"}, nil, nil, "Argument expected for the -W option\n"},
		{"missing value in group", []string{"-OOW"}, nil, nil, "Argument expected for the -W option\n"},
		{"missing c value", []string{"-c"}, nil, nil, "Argument expected for the -c option\n"},
		{"missing long value", []string{"--check-hash-based-pycs"}, nil, nil, "Argument expected for the --check-hash-based-pycs options\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var items []string
			var operands []string
			collect := func(_ *cobra.Command, args []string) error {
				operands = args
				return nil
			}
			probe := &cobra.Command{Use: "probe", RunE: collect}
			tail := &cobra.Command{Use: "tail", Args: cobra.ArbitraryArgs, RunE: collect}
			register(probe.Flags(), &items)

			code, _, stderr := run(t, units(probe, tail), tt.args...)
			if tt.stderr != "" {
				require.Equal(t, cli.ExitUsage, code)
				require.True(t, strings.HasPrefix(stderr, tt.stderr), stderr)
				return
			}
			require.Equal(t, cli.ExitOK, code, stderr)
			require.Equal(t, tt.options, items)
			if len(operands) == 0 {
				operands = nil
			}
			require.Equal(t, tt.operands, operands)
		})
	}
}
