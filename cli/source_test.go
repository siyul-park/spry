package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/cli"
)

const (
	stagesDir = "testdata/stages"
	dirToken  = "$DIR"
)

// TestRun compares the types and ssa dumps of the programs in
// testdata/stages with their goldens. The goldens are recorded from spry
// itself (-update) since CPython has no such stages; absolute paths are
// written as $DIR.
func TestRun(t *testing.T) {
	sources, err := filepath.Glob(filepath.Join(stagesDir, "*.py"))
	require.NoError(t, err)
	require.NotEmpty(t, sources)
	for _, source := range sources {
		base := strings.TrimSuffix(source, ".py")
		t.Run(filepath.Base(base), func(t *testing.T) {
			typesText := stageDump(t, "types", source)
			ssaText := stageDump(t, "ssa", source)
			if *update {
				require.NoError(t, os.WriteFile(base+".types", []byte(typesText), 0o644))
				require.NoError(t, os.WriteFile(base+".ssa", []byte(ssaText), 0o644))
			}
			wantTypes, err := os.ReadFile(base + ".types")
			require.NoError(t, err)
			wantSSA, err := os.ReadFile(base + ".ssa")
			require.NoError(t, err)

			require.Equal(t, string(wantTypes), typesText)
			require.Equal(t, string(wantSSA), ssaText)
			require.Equal(t, string(wantTypes)+string(wantSSA), stageDump(t, "ssa,types", source))
		})
	}
}

// stageDump runs spry on a file with the given spry.dump stages and returns
// stderr, which must hold the dumps of a program that compiles.
func stageDump(t *testing.T, stages, source string) string {
	t.Helper()
	abs, err := filepath.Abs(source)
	require.NoError(t, err)
	var stdout, stderr bytes.Buffer
	code := cli.Run(context.Background(), cli.Process{
		Args:   []string{"-X", "spry.dump=" + stages, "-X", "spry.dump.exit", abs},
		Stdout: &stdout, Stderr: &stderr,
	})
	require.Equal(t, cli.ExitOK, code, stderr.String())
	require.Empty(t, stdout.String())
	return strings.ReplaceAll(stderr.String(), filepath.Dir(abs), dirToken)
}
