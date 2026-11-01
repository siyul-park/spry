package cli

import (
	"context"
	"io"

	"github.com/spf13/pflag"
)

// Component is one independent piece of the command line: it registers its own
// flags on the shared flag set and, once flags are parsed, turns what it owns
// into Options. Components do not know each other; they communicate only
// through State.
type Component interface {
	// Register declares the component's flags.
	Register(fs *pflag.FlagSet, st *State)
	// Prepare validates the parsed command line and fills the component's
	// part of st.Options. Components run in the order of the root's slice.
	Prepare(st *State) error
}

// Handler is a component that can be the action of a run: a mode (-c, -m,
// file, stdin) or an action (help, version). The first handler of the root's
// slice that applies runs.
type Handler interface {
	Component
	// Applies reports whether the parsed command line selects this handler.
	Applies(st *State) bool
	// Run executes the handler and returns the exit status.
	Run(ctx context.Context, env Env, st *State) int
}

// Preempter is a component whose request wins over everything else, including
// flag errors that come after it, like CPython's immediate -h.
type Preempter interface {
	Component
	// Preempts reports whether the flags parsed so far make the component act.
	Preempts(st *State) bool
}

// Terminator is a component whose flag ends option parsing: everything after
// its value belongs to sys.argv, like CPython's -c and -m.
type Terminator interface {
	Component
	// Terminal names the flag letter and the mode it selects.
	Terminal() (letter byte, mode Mode)
}

// State is what components share while one command line is parsed.
type State struct {
	// Options is the parsed result, filled by the components.
	Options Options
	// Flags is the shared flag set.
	Flags *pflag.FlagSet
	// Positional holds the arguments after the options: the script path and
	// its arguments. It is empty when Terminal is set.
	Positional []string
	// Terminal is set when -c or -m ended option parsing.
	Terminal *Terminal
	// Environ is the process environment as KEY=value entries.
	Environ []string

	help    Action
	claimed string
}

// Terminal is the program selector that ended option parsing.
type Terminal struct {
	Mode  Mode
	Value string
	Rest  []string // arguments after the value, for sys.argv[1:]
}

// Env is the injected environment a handler runs in.
type Env struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Environ []string
	// Getwd resolves the working directory lazily.
	Getwd func() (string, error)
}

// claim records that the named mode owns this command line.
func (st *State) claim(name string) {
	st.claimed = name
}

// claimedBy reports whether the named mode owns the command line and the
// command line asks to run a program.
func (st *State) claimedBy(name string) bool {
	return st.Options.Action == ActionRun && st.claimed == name
}

// notImplemented is what every mode does until phase P3 lands execution.
func notImplemented(env Env) int {
	_, _ = io.WriteString(env.Stderr, programName+": execution not implemented yet (phase P3)\n")
	return ExitFailure
}
