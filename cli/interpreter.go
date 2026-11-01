package cli

import (
	"github.com/spf13/pflag"
)

// Interpreter holds the options that configure the interpreter without
// selecting a program: -b -B -d -E -i -I -O -P -q -s -S -u -v -W -x and
// --check-hash-based-pycs. It applies the implications between them (-I implies
// -E, -P and -s) and merges the PYTHON* environment variables below the flags.
type Interpreter struct{}

const checkHashFlag = "check-hash-based-pycs"

var (
	hashChoices = []string{"default", "always", "never"}
	boolLetters = map[string]func(*Options) *bool{
		"B": func(o *Options) *bool { return &o.DontWriteByte },
		"d": func(o *Options) *bool { return &o.Debug },
		"E": func(o *Options) *bool { return &o.IgnoreEnv },
		"i": func(o *Options) *bool { return &o.Inspect },
		"I": func(o *Options) *bool { return &o.Isolated },
		"P": func(o *Options) *bool { return &o.SafePath },
		"q": func(o *Options) *bool { return &o.Quiet },
		"s": func(o *Options) *bool { return &o.NoUserSite },
		"S": func(o *Options) *bool { return &o.NoSite },
		"u": func(o *Options) *bool { return &o.Unbuffered },
		"x": func(o *Options) *bool { return &o.SkipFirstLine },
	}
)

// Register declares the options. Short-only options are registered under the
// name of their letter; a one-letter name is never accepted as a long option.
func (Interpreter) Register(fs *pflag.FlagSet, st *State) {
	for letter := range boolLetters {
		fs.BoolP(letter, letter, false, "")
	}
	for _, letter := range []string{"b", "O", "v"} {
		fs.CountP(letter, letter, "")
	}
	fs.StringArrayP("W", "W", nil, "")
	fs.VarPF(&choiceValue{target: &st.Options.CheckHashPycs, allowed: hashChoices, message: hashMessage}, checkHashFlag, "", "")
}

func (Interpreter) Prepare(st *State) error {
	opts := &st.Options
	for letter, field := range boolLetters {
		*field(opts), _ = st.Flags.GetBool(letter)
	}
	opts.Bytes, _ = st.Flags.GetCount("b")
	opts.Optimize, _ = st.Flags.GetCount("O")
	opts.Verbose, _ = st.Flags.GetCount("v")
	opts.Warnings, _ = st.Flags.GetStringArray("W")
	if opts.Isolated {
		opts.IgnoreEnv, opts.SafePath, opts.NoUserSite = true, true, true
	}
	if !opts.IgnoreEnv {
		mergeEnv(opts, st.Environ)
	}
	return nil
}
