package cli

// Options is the result of parsing a command line.
type Options struct {
	Action Action
	Mode   Mode

	Command string // -c program text
	Module  string // -m module name
	Script  string // script path, or "-" when stdin was named explicitly
	Args    []string

	Bytes         int // -b count
	Debug         bool
	DontWriteByte bool
	IgnoreEnv     bool
	Inspect       bool
	Isolated      bool
	Optimize      int // -O count
	SafePath      bool
	Quiet         bool
	NoUserSite    bool
	NoSite        bool
	Unbuffered    bool
	Verbose       int // -v count
	SkipFirstLine bool
	Warnings      []string // -W values, in order
	CheckHashPycs string   // --check-hash-based-pycs value, "" when unset

	XOptions []string // raw -X values, in order
	Spry     SpryOptions
}

// Action is what the command line asks spry to do.
type Action int

// Mode selects where the program text comes from.
type Mode int

// The actions a parsed command line can request.
const (
	// ActionRun executes the selected program.
	ActionRun Action = iota
	// ActionHelp prints the usage and options help.
	ActionHelp
	// ActionHelpEnv prints the environment variable help.
	ActionHelpEnv
	// ActionHelpXOptions prints the -X option help.
	ActionHelpXOptions
	// ActionHelpAll prints every help text.
	ActionHelpAll
	// ActionVersion prints the version.
	ActionVersion
	// ActionVersionVerbose prints the extended version (-VV).
	ActionVersionVerbose
)

// The program sources, in CPython terms.
const (
	// ModeStdin reads the program from standard input (no argument or "-").
	ModeStdin Mode = iota
	// ModeCommand runs the string given with -c.
	ModeCommand
	// ModeModule runs the library module given with -m.
	ModeModule
	// ModeScript runs the file named by the first positional argument.
	ModeScript
)

// Argv returns what sys.argv holds for the selected program.
func (o Options) Argv() []string {
	var first string
	switch o.Mode {
	case ModeCommand:
		first = "-c"
	case ModeModule:
		first = o.Module
	case ModeStdin, ModeScript:
		first = o.Script
	}
	return append([]string{first}, o.Args...)
}
