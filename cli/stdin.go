package cli

import (
	"context"

	"github.com/spf13/pflag"
)

// Stdin runs the program read from standard input: no positional argument, or
// an explicit "-". Interactive use is not supported yet.
type Stdin struct{}

const stdinMode = "stdin"

func (Stdin) Register(*pflag.FlagSet, *State) {}

func (Stdin) Prepare(st *State) error {
	if st.Terminal != nil {
		return nil
	}
	switch {
	case len(st.Positional) == 0:
	case st.Positional[0] == "-":
		st.Options.Script = "-"
		st.Options.Args = st.Positional[1:]
	default:
		return nil
	}
	st.Options.Mode = ModeStdin
	st.claim(stdinMode)
	return nil
}

func (Stdin) Applies(st *State) bool {
	return st.claimedBy(stdinMode)
}

func (Stdin) Run(_ context.Context, env Env, _ *State) int {
	return notImplemented(env)
}
