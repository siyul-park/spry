package ssa_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/siyul-park/spry/ssa"
	"github.com/siyul-park/spry/token"
	"github.com/stretchr/testify/require"
)

func TestPrint(t *testing.T) {
	t.Run("writes the text that Parse read", func(t *testing.T) {
		for _, file := range irFiles(t) {
			src, err := os.ReadFile(file)
			require.NoError(t, err)
			p, err := ssa.Parse(bytes.NewReader(src))
			require.NoError(t, err, file)
			var out bytes.Buffer
			opts := []ssa.PrintOption{}
			if strings.Contains(file, "positions") {
				opts = append(opts, ssa.WithPositions())
			}

			require.NoError(t, ssa.Print(&out, p, opts...), file)

			require.Equal(t, string(src), out.String(), file)
		}
	})

	t.Run("omits positions by default", func(t *testing.T) {
		src, err := os.ReadFile("testdata/ir/positions.ssa")
		require.NoError(t, err)
		p, err := ssa.Parse(bytes.NewReader(src))
		require.NoError(t, err)
		var out bytes.Buffer

		require.NoError(t, ssa.Print(&out, p))

		require.NotContains(t, out.String(), "!pos")
	})

	t.Run("rejects a malformed instruction", func(t *testing.T) {
		p := mustParse(t, "module m\n\nfunc @f() -> none {\nb0:\n  ret\n}\n")
		p.Modules[0].Functions[0].Blocks[0].Instrs[0].Args = []ssa.Value{&ssa.Const{}, &ssa.Const{}}

		err := ssa.Print(&bytes.Buffer{}, p)

		require.ErrorIs(t, err, ssa.ErrInvalid)
	})
}

func TestWithPositions(t *testing.T) {
	for _, file := range irFiles(t) {
		src, err := os.ReadFile(file)
		require.NoError(t, err)
		if strings.Contains(string(src), "nan") {
			continue
		}
		p := mustParse(t, string(src))
		n := 0
		for _, m := range p.Modules {
			for _, f := range m.Functions {
				for _, b := range f.Blocks {
					for _, in := range b.Instrs {
						n++
						in.Pos = token.Position{Filename: "t.py", Offset: n, Line: n, Column: 1}
						in.End = token.Position{Filename: "t.py", Offset: n + 1, Line: n, Column: 2}
					}
				}
			}
		}
		var out bytes.Buffer

		require.NoError(t, ssa.Print(&out, p, ssa.WithPositions()), file)
		q, err := ssa.Parse(&out)

		require.NoError(t, err, file)
		require.Equal(t, p, q, file)
	}
}

func irFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("testdata/ir/*.ssa")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	return files
}

func mustParse(t *testing.T, src string) *ssa.Program {
	t.Helper()
	p, err := ssa.Parse(strings.NewReader(src))
	require.NoError(t, err)
	return p
}
