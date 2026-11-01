package cli

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// Root assembles components into one cobra command and drives its lifecycle:
// flag parsing, argument validation, PreRunE (component validation) and RunE
// (dispatch to the selected handler).
//
// Root deliberately never calls cobra's Execute. Execute always registers the
// hidden "__complete" command and, with it, would treat a script named
// "__complete" as a completion request (and read COBRA_* variables); no option
// turns that off, and a CPython-compatible command line has no subcommands.
// Driving ParseFlags, ValidateArgs, PreRunE and RunE ourselves keeps cobra's
// flag, IO, context and hook machinery without its subcommand machinery.
type Root struct {
	cmd        *cobra.Command
	state      *State
	components []Component
	terminals  map[byte]Mode
	getwd      func() (string, error)
	exit       int
}

// DefaultComponents is the full python3.14 command line. Adding or removing a
// mode is a one-line change here. Handlers are tried in this order, so the
// actions that preempt a run (help, version) come first.
func DefaultComponents() []Component {
	return []Component{
		Help{},
		Version{},
		Interpreter{},
		XOptions{},
		Command{},
		Module{},
		File{},
		Stdin{},
	}
}

// Parse interprets args (the command line without the program name) the way
// CPython 3.14 does, ignoring the environment. See ParseEnv.
func Parse(args []string) (Options, error) {
	return ParseEnv(args, nil)
}

// ParseEnv interprets args like Parse and merges the PYTHON* variables of
// environ (KEY=value entries) into the options. Flags win over the environment,
// and -E or -I ignore it.
func ParseEnv(args, environ []string) (Options, error) {
	return NewRoot(Process{Env: environ}, DefaultComponents()...).Parse(args)
}

// NewRoot builds a root over the given components for one command line.
func NewRoot(p Process, components ...Component) *Root {
	cmd := &cobra.Command{Use: programName, Args: cobra.ArbitraryArgs}
	cmd.Flags().SetInterspersed(false)
	cmd.SetIn(p.Stdin)
	cmd.SetOut(p.Stdout)
	cmd.SetErr(p.Stderr)
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageFromFlagError(err)
	})

	root := &Root{
		cmd:        cmd,
		state:      &State{Flags: cmd.Flags(), Environ: p.Env, Options: Options{Spry: SpryOptions{Opt: OptDefault}}},
		components: components,
		terminals:  map[byte]Mode{},
		getwd:      workDir(p.Dir),
	}
	for _, c := range components {
		c.Register(cmd.Flags(), root.state)
		if t, ok := c.(Terminator); ok {
			letter, mode := t.Terminal()
			root.terminals[letter] = mode
		}
	}
	cmd.PreRunE = root.prepare
	cmd.RunE = root.dispatch
	return root
}

// Parse interprets args (the command line without the program name) and
// returns the options without running anything.
func (r *Root) Parse(args []string) (Options, error) {
	cut := splitArgs(args, r.state.Flags, r.terminals)
	r.state.Terminal = cut.terminal
	err := r.cmd.ParseFlags(cut.options)
	r.state.Positional = r.cmd.Flags().Args()
	for _, c := range r.components {
		if p, ok := c.(Preempter); ok && p.Preempts(r.state) {
			return r.state.Options, p.Prepare(r.state)
		}
	}
	if err != nil {
		return r.state.Options, r.cmd.FlagErrorFunc()(r.cmd, err)
	}
	if cut.err != nil {
		return r.state.Options, cut.err
	}
	if err := r.cmd.ValidateArgs(r.state.Positional); err != nil {
		return r.state.Options, err
	}
	if err := r.cmd.PreRunE(r.cmd, r.state.Positional); err != nil {
		return r.state.Options, err
	}
	return r.state.Options, nil
}

// Run parses args and runs the selected handler, returning the exit status.
// Command line errors are printed in CPython's format.
func (r *Root) Run(ctx context.Context, args []string) int {
	r.cmd.SetContext(ctx)
	if _, err := r.Parse(args); err != nil {
		return r.report(err)
	}
	if err := r.cmd.RunE(r.cmd, r.state.Positional); err != nil {
		return r.report(err)
	}
	return r.exit
}

// prepare is the PreRunE hook: every component validates its part.
func (r *Root) prepare(*cobra.Command, []string) error {
	for _, c := range r.components {
		if err := c.Prepare(r.state); err != nil {
			return err
		}
	}
	return nil
}

// dispatch is the RunE hook: the first handler that applies runs.
func (r *Root) dispatch(cmd *cobra.Command, _ []string) error {
	env := Env{
		Stdin:   cmd.InOrStdin(),
		Stdout:  cmd.OutOrStdout(),
		Stderr:  cmd.ErrOrStderr(),
		Environ: r.state.Environ,
		Getwd:   r.getwd,
	}
	for _, c := range r.components {
		if h, ok := c.(Handler); ok && h.Applies(r.state) {
			r.exit = h.Run(cmd.Context(), env, r.state)
			return nil
		}
	}
	return &UsageError{Message: programName + ": no mode selected for this command line", NoUsage: true}
}

// report prints a command line error and returns the usage exit status.
func (r *Root) report(err error) int {
	var usage *UsageError
	if !errors.As(err, &usage) {
		usage = &UsageError{Message: err.Error()}
	}
	stderr := r.cmd.ErrOrStderr()
	_, _ = io.WriteString(stderr, usage.Message+"\n")
	if !usage.NoUsage {
		_, _ = io.WriteString(stderr, usageLine+usageHint)
	}
	return ExitUsage
}

// workDir returns a lazy working-directory resolver: dir when given, else the
// process's current directory, looked up only when asked.
func workDir(dir string) func() (string, error) {
	if dir != "" {
		return func() (string, error) { return dir, nil }
	}
	return os.Getwd
}
