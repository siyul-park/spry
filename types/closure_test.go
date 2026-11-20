package types_test

import (
	"testing"

	"github.com/siyul-park/spry/types"
	"github.com/stretchr/testify/require"
)

func TestFuncValue(t *testing.T) {
	t.Run("top-level def has no environment", func(t *testing.T) {
		c := mustCheck(t, "def f(x: int) -> int:\n    return x\ng = f\n")
		fv, ok := c.typeOf(t, "g").(*types.FuncValue)
		require.True(t, ok)
		require.Equal(t, "f", fv.Func().Name())
		require.Nil(t, fv.Env())
		require.NotEmpty(t, fv.String())
		require.NotNil(t, fv.Underlying())
	})
	t.Run("lambda is a function value", func(t *testing.T) {
		c := mustCheck(t, "g = lambda x: x\n")
		fv, ok := c.typeOf(t, "g").(*types.FuncValue)
		require.True(t, ok)
		require.NotNil(t, fv.Env())
	})
}
