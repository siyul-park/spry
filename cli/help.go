package cli

import (
	"context"
	"io"

	"github.com/spf13/pflag"
)

// Help is -h, -?, --help, --help-env, --help-xoptions and --help-all. The
// first one on the command line wins, and it wins immediately, even over an
// invalid option that follows.
type Help struct{}

const (
	programName = "spry"

	usageLine = "usage: " + programName + " [option] ... [-c cmd | -m mod | file | -] [arg] ...\n"
	usageHint = "Try `" + programName + " -h' for more information.\n"

	xOptionsHeader   = "The following implementation-specific options are available:\n"
	spryXOptionsBody = `-X spry.dump=STAGE: print an intermediate form (tokens, ast, types, ssa or asm)
         and stop before running
-X spry.emit=PATH: write the compiled minivm program to PATH
-X spry.fallback=MODE: warn (default) or forbid (error) dynamic Any fallbacks
-X spry.opt=N: optimization level 0, 1, 2 or 3
`
)

var helpFlags = []struct {
	name, short string
	action      Action
}{
	{"help", "h", ActionHelp},
	{"?", "?", ActionHelp},
	{"help-env", "", ActionHelpEnv},
	{"help-xoptions", "", ActionHelpXOptions},
	{"help-all", "", ActionHelpAll},
}

func (Help) Register(fs *pflag.FlagSet, st *State) {
	for _, h := range helpFlags {
		flag := fs.VarPF(actionValue{state: st, action: h.action}, h.name, h.short, "")
		flag.NoOptDefVal = "true"
	}
}

func (Help) Preempts(st *State) bool {
	return st.help != ActionRun
}

func (Help) Prepare(st *State) error {
	if st.help != ActionRun {
		st.Options.Action = st.help
	}
	return nil
}

func (Help) Applies(st *State) bool {
	switch st.Options.Action {
	case ActionHelp, ActionHelpEnv, ActionHelpXOptions, ActionHelpAll:
		return true
	}
	return false
}

func (Help) Run(_ context.Context, env Env, st *State) int {
	_, _ = io.WriteString(env.Stdout, helpText(st.Options.Action))
	return ExitOK
}

func helpText(action Action) string {
	switch action {
	case ActionHelpEnv:
		return envHelp
	case ActionHelpXOptions:
		return xOptionsHelp()
	case ActionHelpAll:
		return usageLine + optionsHelp + "\n" + envHelp + "\n" + xOptionsHelp()
	}
	return usageLine + optionsHelp
}

func xOptionsHelp() string {
	return xOptionsHeader + spryXOptionsBody + cpythonXOptionsBody
}
