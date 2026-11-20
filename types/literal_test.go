package types_test

import (
	"testing"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/types"
	"github.com/stretchr/testify/require"
)

func TestGuard(t *testing.T) {
	w := newWorld(t, "g: TypeIs[int]\ng2: TypeGuard[int]\n")
	g := w.at(t, "g").(*types.Guard)
	require.Same(t, w.integer, g.Type())
	require.True(t, g.Strict())
	require.Same(t, w.boolean.(*types.Named).Underlying(), g.Underlying())
	require.Equal(t, "typing.TypeIs[int]", g.String())
	require.False(t, w.at(t, "g2").(*types.Guard).Strict())
}

func TestNewLiteral(t *testing.T) {
	w := newWorld(t, "class E:\n    A: 'E'\nm: Literal[E.A]\n")
	l := types.NewLiteral(ast.Str("a"))
	require.Equal(t, ast.Str("a"), l.Value())
	require.Nil(t, l.Member())
	require.Same(t, w.str.(*types.Named).Underlying(), l.Underlying())
	require.Panics(t, func() { types.NewLiteral(ast.Float(1)) })

	m := w.at(t, "m").(*types.Literal)
	require.Equal(t, "A", m.Member().Name())
	require.Same(t, w.at(t, "E").(*types.Named).Underlying().(*types.Class).Members().Lookup("A"), m.Member())
	require.Nil(t, m.Value())
	require.Equal(t, "typing.Literal[b'a\\x00']", types.NewLiteral(ast.Bytes("a\x00")).String())
	require.Equal(t, "typing.Literal[None]", types.NewLiteral(ast.None{}).String())
}
