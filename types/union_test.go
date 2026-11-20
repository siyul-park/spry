package types_test

import (
	"testing"

	"github.com/siyul-park/spry/types"
	"github.com/stretchr/testify/require"
)

func TestNewUnion(t *testing.T) {
	w := newWorld(t, "")

	t.Run("flattens and dedupes in source order", func(t *testing.T) {
		u := types.NewUnion(types.NewUnion(w.str, w.integer), w.integer, w.none).(*types.Union)
		require.Equal(t, 3, u.Len())
		require.Equal(t, "str | int | None", u.String())
		require.Same(t, w.none, u.Term(2))
		require.Same(t, u, u.Underlying())
	})
	t.Run("degenerate", func(t *testing.T) {
		require.Same(t, types.Typ[types.Never], types.NewUnion())
		require.Same(t, w.integer, types.NewUnion(w.integer, w.integer))
	})
}
