package ssa_test

import (
	"testing"

	"github.com/siyul-park/spry/ssa"
	"github.com/stretchr/testify/require"
)

func TestTypeOf(t *testing.T) {
	i := ssa.Type{Kind: ssa.KindInt}

	require.Equal(t, i, ssa.TypeOf(&ssa.Param{Typ: i}))
	require.Equal(t, i, ssa.TypeOf(&ssa.Instr{Typ: i}))
	require.Equal(t, i, ssa.TypeOf(&ssa.Const{Typ: i}))
	require.Equal(t, ssa.Type{}, ssa.TypeOf(nil))
}
