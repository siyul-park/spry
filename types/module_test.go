package types_test

import (
	"testing"
	"testing/fstest"

	"github.com/siyul-park/spry/types"
	"github.com/stretchr/testify/require"
)

func TestModule(t *testing.T) {
	c := mustCheck(t, "import pkg.mod\n", types.WithFS(fstest.MapFS{
		"pkg/__init__.py": {},
		"pkg/mod.py":      {Data: []byte("x = 1\n")},
	}, "/app"))
	m := c.mod.Imports()[1]
	require.Equal(t, "pkg.mod", m.Name())
	require.Equal(t, "/app/pkg/mod.py", m.Path())
	require.Empty(t, m.Imports())
	require.Same(t, m, m.Underlying())
	require.Equal(t, "<module 'pkg.mod'>", m.String())
	require.Equal(t, "__main__", c.mod.Name())
	require.Same(t, c.file, c.mod.Init().Decl())
}
