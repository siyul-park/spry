package types_test

import (
	"testing"

	"github.com/siyul-park/spry/types"
	"github.com/stretchr/testify/require"
)

func TestTyp(t *testing.T) {
	w := newWorld(t, "")
	require.Equal(t, types.Any, types.Typ[types.Any].Kind())
	require.Equal(t, "typing.Never", types.Typ[types.Never].Name())
	require.Equal(t, "<invalid>", types.Typ[types.Invalid].String())
	require.Same(t, types.Typ[types.Any], types.Typ[types.Any].Underlying())
	require.Same(t, w.str.(*types.Named).Underlying(), types.Typ[types.LiteralString].Underlying())
}
