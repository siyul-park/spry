package cli

import (
	"context"
	"fmt"
	"io"
	"runtime"

	"github.com/spf13/pflag"
)

// Version is -V and --version. It is deferred: an error elsewhere on the
// command line still wins. Given twice (-VV) it prints build details.
type Version struct{}

// Release is the spry release reported by -V.
const Release = "0.0.0-dev"

const pythonVersion = "3.14.0"

func (Version) Register(fs *pflag.FlagSet, _ *State) {
	fs.CountP("version", "V", "")
}

func (Version) Prepare(st *State) error {
	if st.Options.Action != ActionRun {
		return nil
	}
	switch count, _ := st.Flags.GetCount("version"); {
	case count >= 2:
		st.Options.Action = ActionVersionVerbose
	case count == 1:
		st.Options.Action = ActionVersion
	}
	return nil
}

func (Version) Applies(st *State) bool {
	return st.Options.Action == ActionVersion || st.Options.Action == ActionVersionVerbose
}

func (Version) Run(_ context.Context, env Env, st *State) int {
	text := fmt.Sprintf("Python %s (%s %s)", pythonVersion, programName, Release)
	if st.Options.Action == ActionVersionVerbose {
		text += fmt.Sprintf(" [Go %s %s/%s]", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	}
	_, _ = io.WriteString(env.Stdout, text+"\n")
	return ExitOK
}
