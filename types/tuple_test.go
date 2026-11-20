package types_test

import (
	"testing"

	"github.com/siyul-park/spry/token"
	"github.com/siyul-park/spry/types"
	"github.com/stretchr/testify/require"
)

func TestNewTuple(t *testing.T) {
	w := newWorld(t, "")
	vars := []*types.Var{types.NewVar(token.NoPos, "", w.integer, types.FieldVar)}

	t.Run("fixed", func(t *testing.T) {
		tup := types.NewTuple(vars)
		require.Equal(t, 1, tup.Len())
		require.Equal(t, -1, tup.Unbounded())
		require.Same(t, vars[0], tup.At(0))
		require.Same(t, tup, tup.Underlying())
	})
	t.Run("unbounded", func(t *testing.T) {
		require.Equal(t, 0, types.NewTuple(vars, types.WithUnbounded(0)).Unbounded())
	})
	t.Run("nil is empty", func(t *testing.T) {
		var tup *types.Tuple
		require.Equal(t, 0, tup.Len())
		require.Equal(t, -1, tup.Unbounded())
	})
	t.Run("index out of range", func(t *testing.T) {
		require.Panics(t, func() { types.NewTuple(vars, types.WithUnbounded(1)) })
	})
}
