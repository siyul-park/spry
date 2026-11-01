package cli

import (
	"context"
	"io"
)

// Process is everything a command line run observes.
type Process struct {
	Args   []string // without the program name
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Env    []string // KEY=value entries
	Dir    string   // working directory for script paths; the current one when empty
}

// Exit statuses, matching CPython.
const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
)

// Main runs spry for a real process: argv includes the program name.
func Main(argv []string, stdin io.Reader, stdout, stderr io.Writer, environ []string) int {
	var args []string
	if len(argv) > 1 {
		args = argv[1:]
	}
	return Run(Process{Args: args, Stdin: stdin, Stdout: stdout, Stderr: stderr, Env: environ})
}

// Run parses the command line and executes it, returning the exit status.
func Run(p Process) int {
	return RunContext(context.Background(), p)
}

// RunContext is Run with a context that handlers may observe.
func RunContext(ctx context.Context, p Process) int {
	return NewRoot(p, DefaultComponents()...).Run(ctx, p.Args)
}
