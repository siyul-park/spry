package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/spf13/pflag"
)

// File runs the script named by the first positional argument; the rest is
// sys.argv[1:].
type File struct{}

const fileMode = "file"

func (File) Register(*pflag.FlagSet, *State) {}

func (File) Prepare(st *State) error {
	if st.Terminal != nil || len(st.Positional) == 0 || st.Positional[0] == "-" {
		return nil
	}
	st.Options.Mode = ModeScript
	st.Options.Script = st.Positional[0]
	st.Options.Args = st.Positional[1:]
	st.claim(fileMode)
	return nil
}

func (File) Applies(st *State) bool {
	return st.claimedBy(fileMode)
}

// Run mirrors CPython's messages for a script it cannot open, then reports
// that execution is missing until phase P3.
func (File) Run(_ context.Context, env Env, st *State) int {
	path := st.Options.Script
	if !filepath.IsAbs(path) {
		dir, err := env.Getwd()
		if err != nil {
			fmt.Fprintf(env.Stderr, "%s: cannot determine working directory: %v\n", programName, err)
			return ExitFailure
		}
		path = filepath.Join(dir, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		fmt.Fprintf(env.Stderr, "%s: can't open file '%s': %s\n", programName, path, errnoText(err))
		return ExitUsage
	}
	if info.IsDir() {
		fmt.Fprintf(env.Stderr, "%s: can't find '__main__' module in '%s'\n", programName, path)
		return ExitFailure
	}
	return notImplemented(env)
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
