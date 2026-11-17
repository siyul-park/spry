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

type processKey struct{}

// Exit statuses, matching CPython.
const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
)

// Release is the spry release reported by -V.
const Release = "0.0.0-dev"

const pythonVersion = "3.14.0"

// Run executes the supported python3.14 command line and returns the exit status.
func Run(ctx context.Context, p Process) int {
	return New(Default()...).Run(ctx, p)
}

// processOf returns the Process a Root run attached to ctx.
func processOf(ctx context.Context) Process {
	p, _ := ctx.Value(processKey{}).(Process)
	return p
}
