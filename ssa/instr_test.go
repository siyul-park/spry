package ssa_test

import (
	"testing"

	"github.com/siyul-park/spry/ssa"
	"github.com/stretchr/testify/require"
)

func TestOp_String(t *testing.T) {
	require.Equal(t, "floordiv", ssa.OpFloorDiv.String())
	require.Equal(t, "op(0)", ssa.Op(0).String())
}
