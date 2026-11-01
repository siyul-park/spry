package cli_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/cli"
)

// recorder is a component written outside the package: it adds a flag, a
// handler and a Prepare hook using only the exported contract.
type recorder struct {
	prepared *bool
}

// parseWith parses a command line through a root over exactly the given components.
func parseWith(components []cli.Component, environ []string, args ...string) (cli.Options, error) {
	return cli.NewRoot(cli.Process{Env: environ}, components...).Parse(args)
}

func TestState(t *testing.T) {
	var prepared bool
	root := cli.NewRoot(cli.Process{Stdout: io.Discard, Stderr: io.Discard}, recorder{&prepared}, cli.XOptions{}, cli.File{})
	opts, err := root.Parse([]string{"--ping", "-X", "dev", "script.py", "a"})
	require.NoError(t, err)
	require.True(t, prepared)
	require.Equal(t, []string{"dev"}, opts.XOptions)
	require.Equal(t, "script.py", opts.Script)
	require.Equal(t, []string{"a"}, opts.Args)
}

func TestHandlerOrder(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := cli.NewRoot(cli.Process{Stdout: &stdout, Stderr: &stderr}, recorder{new(bool)}, cli.Version{})
	require.Equal(t, 7, root.Run(context.Background(), []string{"--ping", "-V"}))
	require.Equal(t, "pong\n", stdout.String())

	stdout.Reset()
	require.Equal(t, 7, root.Run(context.Background(), nil))
}

func TestNoModeSelected(t *testing.T) {
	code, stdout, stderr := runWith(t, []cli.Component{cli.Version{}}, "script.py")
	require.Equal(t, cli.ExitUsage, code)
	require.Empty(t, stdout)
	require.Equal(t, "spry: no mode selected for this command line\n", stderr)
}

func (recorder) Register(fs *pflag.FlagSet, _ *cli.State) {
	fs.Bool("ping", false, "")
}

func (r recorder) Prepare(*cli.State) error {
	*r.prepared = true
	return nil
}

func (recorder) Applies(st *cli.State) bool {
	ping, _ := st.Flags.GetBool("ping")
	return ping
}

func (recorder) Run(_ context.Context, env cli.Env, _ *cli.State) int {
	_, _ = io.WriteString(env.Stdout, "pong\n")
	return 7
}

// runWith runs a command line through a root over exactly the given components.
func runWith(t *testing.T, components []cli.Component, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	p := cli.Process{Args: args, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Dir: t.TempDir()}
	code := cli.NewRoot(p, components...).Run(context.Background(), args)
	return code, stdout.String(), stderr.String()
}
