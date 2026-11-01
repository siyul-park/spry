package cli

import (
	"context"

	"github.com/spf13/pflag"
)

// Module is -m: run a library module as a script. It ends option parsing;
// everything after the module name is sys.argv[1:].
type Module struct{}

const moduleMode = "module"

func (Module) Register(fs *pflag.FlagSet, _ *State) {
	fs.StringP("m", "m", "", "")
}

func (Module) Terminal() (letter byte, mode Mode) {
	return 'm', ModeModule
}

func (Module) Prepare(st *State) error {
	if st.Terminal == nil || st.Terminal.Mode != ModeModule {
		return nil
	}
	st.Options.Mode = ModeModule
	st.Options.Module = st.Terminal.Value
	st.Options.Args = st.Terminal.Rest
	st.claim(moduleMode)
	return nil
}

func (Module) Applies(st *State) bool {
	return st.claimedBy(moduleMode)
}

func (Module) Run(_ context.Context, env Env, _ *State) int {
	return notImplemented(env)
}
