package conformance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
)

// Outcome is the observable result of running one case.
type Outcome struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Run executes the interpreter command (program and leading arguments, for
// example {"python3.14"} or {"spry"}) on the case file below root, which must be
// the directory the corpus was loaded from. The program reads an empty stdin
// (the null device), and Passed ignores stderr: only stdout and the exit status
// are compared.
func Run(ctx context.Context, interpreter []string, root string, c Case) (Outcome, error) {
	if len(interpreter) == 0 {
		return Outcome{}, errors.New("conformance: empty interpreter command")
	}
	args := append(append([]string(nil), interpreter[1:]...), filepath.Join(root, filepath.FromSlash(c.Path)))
	cmd := exec.CommandContext(ctx, interpreter[0], args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	out := Outcome{}
	err := cmd.Run()
	out.Stdout, out.Stderr = stdout.String(), stderr.String()
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		out.ExitCode = exit.ExitCode()
	default:
		return out, fmt.Errorf("conformance: run %s: %w", c.ID(), err)
	}
	return out, nil
}

// Passed reports whether the program exited 0 and printed exactly expected.
func (o Outcome) Passed(expected string) bool {
	return o.ExitCode == 0 && o.Stdout == expected
}
