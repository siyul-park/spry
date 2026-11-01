package cli

import (
	"context"

	"github.com/spf13/pflag"
)

// Command is -c: run the program text given as the flag's value. It ends
// option parsing; everything after the value is sys.argv[1:].
type Command struct{}

const commandMode = "command"

func (Command) Register(fs *pflag.FlagSet, _ *State) {
	fs.StringP("c", "c", "", "")
}

func (Command) Terminal() (letter byte, mode Mode) {
	return 'c', ModeCommand
}

func (Command) Prepare(st *State) error {
	if st.Terminal == nil || st.Terminal.Mode != ModeCommand {
		return nil
	}
	st.Options.Mode = ModeCommand
	st.Options.Command = st.Terminal.Value
	st.Options.Args = st.Terminal.Rest
	st.claim(commandMode)
	return nil
}

func (Command) Applies(st *State) bool {
	return st.claimedBy(commandMode)
}

func (Command) Run(_ context.Context, env Env, _ *State) int {
	return notImplemented(env)
}
