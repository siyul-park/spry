package types_test

import (
	"testing"

	"github.com/siyul-park/spry/types"
	"github.com/stretchr/testify/require"
)

func TestNewSignature(t *testing.T) {
	w := newWorld(t, `
@overload
def ov() -> int: ...
@overload
def ov(x: int) -> str: ...
def ov(x=0): ...
`)

	t.Run("accessors", func(t *testing.T) {
		s := sig(w.integer, param("a", w.integer, types.PositionalOnly))
		require.Equal(t, 1, s.Params().Len())
		require.Same(t, w.integer, s.Result())
		require.Nil(t, s.TypeParams())
		require.Same(t, s, s.Underlying())
	})
	t.Run("bad orders panic", func(t *testing.T) {
		tests := []struct {
			name   string
			params []*types.Var
		}{
			{"keyword before positional", []*types.Var{param("a", w.integer, types.KeywordOnly), param("b", w.integer, types.PositionalOrKeyword)}},
			{"two var positional", []*types.Var{param("a", w.integer, types.VarPositional), param("b", w.integer, types.VarPositional)}},
			{"two var keyword", []*types.Var{param("a", w.integer, types.VarKeyword), param("b", w.integer, types.VarKeyword)}},
			{"not a parameter", []*types.Var{types.NewVar(0, "a", w.integer, types.LocalVar)}},
			{"positional after variadic", []*types.Var{param("a", w.integer, types.VarPositional), param("b", w.integer, types.PositionalOrKeyword)}},
		}
		for _, tt := range tests {
			require.Panics(t, func() { sig(w.integer, tt.params...) }, tt.name)
		}
	})
	t.Run("overload", func(t *testing.T) {
		o, ok := w.c.typeOf(t, "ov").(*types.Overload)
		require.True(t, ok)
		require.Equal(t, 2, o.Len())
		require.Same(t, o, o.Underlying())
	})
}
