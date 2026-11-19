package types_test

import (
	"testing"

	"github.com/siyul-park/spry/types"
	"github.com/stretchr/testify/require"
)

func TestNamed(t *testing.T) {
	w := newWorld(t, "T = TypeVar('T', covariant=True)\nclass G(Generic[T]): ...\n")
	g := w.named(t, "G")
	tp := w.at(t, "T").(*types.TypeParam)
	i := inst(t, g, w.integer).(*types.Named)

	require.Same(t, g, i.Origin())
	require.Same(t, g, g.Origin())
	require.Same(t, g.Obj(), i.Obj())
	require.Equal(t, 1, i.TypeArgs().Len())
	require.Same(t, w.integer, i.TypeArgs().At(0))
	require.Equal(t, 1, i.TypeParams().Len())
	require.Same(t, tp, i.TypeParams().At(0))
	require.Nil(t, g.TypeArgs())
	require.Same(t, g.Underlying(), i.Underlying())
	require.Equal(t, "__main__.G[int]", i.String())
}
