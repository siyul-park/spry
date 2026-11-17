package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

// xoptions records the values of every -X given, in order.
type xoptions struct {
	values []string
}

const (
	xoptionFlag = "xoption"

	dumpTokens = "tokens"
	dumpAST    = "ast"

	optionDump     = "spry.dump"
	optionDumpExit = "spry.dump.exit"
)

// dumpStages are the spry.dump stages the front end can print.
var dumpStages = map[string]bool{dumpTokens: true, dumpAST: true}

// XOptions is -X. It only contributes the flag: it selects no action, and the
// units that run a program read the spry-only options from it. An option or
// value spry does not implement yet is reported as unsupported, as for -O.
func XOptions() *cobra.Command {
	cmd := &cobra.Command{Use: "xoptions"}
	cmd.Flags().VarP(&xoptions{}, xoptionFlag, "X", "set implementation-specific option")
	return cmd
}

// Accepts reports whether spry implements the option value: spry.dump with
// the stages it can print, and spry.dump.exit.
func (x *xoptions) Accepts(value string) bool {
	if value == optionDumpExit {
		return true
	}
	list, ok := strings.CutPrefix(value, optionDump+"=")
	if !ok {
		return false
	}
	for _, stage := range strings.Split(list, ",") {
		if !dumpStages[stage] {
			return false
		}
	}
	return true
}

func (x *xoptions) Set(value string) error {
	x.values = append(x.values, value)
	return nil
}

func (x *xoptions) Type() string {
	return "stringArray"
}

func (x *xoptions) String() string {
	return "[" + strings.Join(x.values, ",") + "]"
}

// dumps reports whether the last spry.dump option names the stage.
func (x *xoptions) dumps(stage string) bool {
	if x == nil {
		return false
	}
	for i := len(x.values) - 1; i >= 0; i-- {
		if list, ok := strings.CutPrefix(x.values[i], optionDump+"="); ok {
			for _, name := range strings.Split(list, ",") {
				if name == stage {
					return true
				}
			}
			return false
		}
	}
	return false
}

// exits reports whether spry.dump.exit was given.
func (x *xoptions) exits() bool {
	if x == nil {
		return false
	}
	for _, value := range x.values {
		if value == optionDumpExit {
			return true
		}
	}
	return false
}

// xoptionsOf returns the -X values of the root that cmd is composed under, or
// nil when no XOptions unit is composed.
func xoptionsOf(cmd *cobra.Command) *xoptions {
	if cmd.Parent() == nil {
		return nil
	}
	for _, sibling := range cmd.Parent().Commands() {
		if flag := sibling.Flags().Lookup(xoptionFlag); flag != nil {
			x, _ := flag.Value.(*xoptions)
			return x
		}
	}
	return nil
}
