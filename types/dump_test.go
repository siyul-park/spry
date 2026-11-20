package types_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/parser"
	"github.com/siyul-park/spry/token"
	"github.com/siyul-park/spry/types"
)

var update = flag.Bool("update", false, "rewrite the testdata/*.types goldens")

func TestDump(t *testing.T) {
	files, err := filepath.Glob("testdata/*.py")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".py")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(file)
			require.NoError(t, err)
			m, info, err := checkSource(t, filepath.Base(file), string(src))
			require.NoError(t, err)
			got := types.Dump(m, info)
			golden := strings.TrimSuffix(file, ".py") + ".types"
			if *update {
				require.NoError(t, os.WriteFile(golden, []byte(got), 0o644))
			}
			want, err := os.ReadFile(golden)
			require.NoError(t, err)
			require.Equal(t, string(want), got)
		})
	}

	t.Run("unsupported construct falls back", func(t *testing.T) {
		m, info, err := checkSource(t, "u.py", "def f(n):\n    return [n]\nf(1)\n")
		require.NoError(t, err)
		require.Contains(t, types.Dump(m, info), "fallbacks")
	})
}

func checkSource(t *testing.T, name, src string) (*types.Module, *types.Info, error) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, strings.NewReader(src))
	require.NoError(t, err)
	return types.Check(fset, name, file)
}
