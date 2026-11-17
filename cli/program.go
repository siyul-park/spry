package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
)

var (
	// ErrNotImplemented is returned by every program source until phase P3
	// lands execution.
	ErrNotImplemented = errors.New("execution not implemented yet (phase P3)")

	errNotOperand = errors.New("operand does not select this command")
)

// Command is -c: run the program text given as the flag's value. Everything
// after the value is sys.argv[1:].
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "command",
		Args: cobra.ArbitraryArgs,
		RunE: func(*cobra.Command, []string) error { return ErrNotImplemented },
	}
	cmd.Flags().StringP("command", "c", "", "program passed in as string (terminates option list)")
	return cmd
}

// File runs the script named by the first operand; the rest is sys.argv[1:].
func File() *cobra.Command {
	return &cobra.Command{
		Use: "file",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 || args[0] == "-" {
				return errNotOperand
			}
			return nil
		},
		RunE: runFile,
	}
}

// Stdin runs the program read from standard input: no operand, or an explicit
// "-". Interactive use is not supported yet.
func Stdin() *cobra.Command {
	return &cobra.Command{
		Use: "stdin",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 && args[0] != "-" {
				return errNotOperand
			}
			return nil
		},
		RunE: func(*cobra.Command, []string) error { return ErrNotImplemented },
	}
}

// runFile mirrors CPython's messages for a script it cannot open, then reports
// that execution is missing until phase P3.
func runFile(cmd *cobra.Command, args []string) error {
	path := args[0]
	if !filepath.IsAbs(path) {
		dir := processOf(cmd.Context()).Dir
		if dir == "" {
			var err error
			if dir, err = os.Getwd(); err != nil {
				return fmt.Errorf("cannot determine working directory: %w", err)
			}
		}
		// Not filepath.Join: cleaning would drop a trailing separator and
		// with it ENOTDIR for a regular file, which CPython reports.
		path = dir + string(filepath.Separator) + path
	}
	file, err := os.Open(path)
	if err != nil {
		return &exitError{code: ExitUsage, err: fmt.Errorf("can't open file '%s': %s", path, errnoText(err))}
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return &exitError{code: ExitUsage, err: fmt.Errorf("can't open file '%s': %s", path, errnoText(err))}
	}
	if info.IsDir() {
		return fmt.Errorf("can't find '__main__' module in '%s'", path)
	}
	return ErrNotImplemented
}

func errnoText(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "[Errno 2] No such file or directory"
	case errors.Is(err, fs.ErrPermission):
		return "[Errno 13] Permission denied"
	case errors.Is(err, syscall.ENOTDIR):
		return "[Errno 20] Not a directory"
	}
	return "[Errno 5] Input/output error"
}
