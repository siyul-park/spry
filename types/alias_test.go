package types_test

import (
	"testing"

	"github.com/siyul-park/spry/types"
	"github.com/stretchr/testify/require"
)

func TestUnalias(t *testing.T) {
	w := newWorld(t, "type Inner = int\ntype Outer = Inner\ntype Loop = Loop\n")
	inner, outer, loop := w.at(t, "Inner").(*types.Alias), w.at(t, "Outer"), w.at(t, "Loop")

	t.Run("expands chains", func(t *testing.T) {
		require.Same(t, w.integer, types.Unalias(outer))
		require.Same(t, w.integer, outer.Underlying().(*types.Class).Obj().Type())
	})
	t.Run("keeps other types", func(t *testing.T) {
		require.Same(t, w.dog, types.Unalias(w.dog))
	})
	t.Run("endless alias is invalid", func(t *testing.T) {
		require.Same(t, types.Typ[types.Invalid], types.Unalias(loop))
	})
	t.Run("accessors", func(t *testing.T) {
		require.Equal(t, "Inner", inner.Obj().Name())
		require.Same(t, inner, inner.Origin())
		require.Equal(t, 0, inner.TypeParams().Len())
		require.Equal(t, 0, inner.TypeArgs().Len())
		require.Equal(t, "Inner", inner.String())
		require.Same(t, w.integer, inner.Rhs())
	})
}
