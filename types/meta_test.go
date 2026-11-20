package types_test

import (
	"testing"

	"github.com/siyul-park/spry/types"
	"github.com/stretchr/testify/require"
)

func TestNewMeta(t *testing.T) {
	w := newWorld(t, "")
	m := types.NewMeta(w.dog)
	require.Same(t, w.dog, m.Elem())
	require.Same(t, m, m.Underlying())
	require.Equal(t, "type[__main__.Dog]", m.String())
}
