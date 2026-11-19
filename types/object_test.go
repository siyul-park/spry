package types_test

import (
	"testing"

	"github.com/siyul-park/spry/types"
	"github.com/stretchr/testify/require"
)

func TestObject(t *testing.T) {
	w := newWorld(t, `
import typing
class K:
    x: int
def f(a: int) -> int: ...
class T: ...
Alias = list[int]
type A = int
Q = TypeVar('Q')
`)
	members := w.named(t, "K").Underlying().(*types.Class).Members()
	v := members.Lookup("x").(*types.Var)
	f := w.c.mod.Scope().Lookup("f").(*types.Func)
	mn := w.c.mod.Scope().Lookup("math").(*types.ModuleName)
	bi := w.c.mod.Scope().Lookup("Generic").(*types.Builtin)

	t.Run("accessors", func(t *testing.T) {
		require.Equal(t, "x", v.Name())
		require.NotZero(t, v.Pos())
		require.Equal(t, types.FieldVar, v.Kind())
		require.Same(t, v, v.Origin())
		require.False(t, v.HasDefault())
		require.False(t, v.IsCell())
		require.Zero(t, v.Qualifiers())
		require.Same(t, w.c.mod, f.Module())
		require.Equal(t, "f", f.FullName())
		require.Equal(t, types.FunctionScope, f.Scope().Kind())
		require.Equal(t, "(a: int) -> int", f.Signature().String())
		require.Equal(t, "math", mn.Imported().Name())
		require.Equal(t, "type[typing.Generic]", bi.Type().String())
		require.False(t, w.named(t, "T").Obj().IsAlias())
	})
	t.Run("strings", func(t *testing.T) {
		require.Equal(t, "var x: int", v.String())
		require.Equal(t, "def f(a: int) -> int", f.String())
		require.Equal(t, "module math", mn.String())
		require.Equal(t, "builtin Generic", bi.String())
		require.Equal(t, "class Dog", w.dog.Obj().String())
		require.Equal(t, "typevar Q", w.c.mod.Scope().Lookup("Q").String())
		al := w.c.mod.Scope().Lookup("A").(*types.TypeName)
		require.Equal(t, "type A = int", al.String())
		require.True(t, al.IsAlias())
	})
}

func TestNewVar(t *testing.T) {
	w := newWorld(t, "")
	v := types.NewVar(5, "v", w.integer, types.GlobalVar)
	require.Equal(t, types.GlobalVar, v.Kind())
	require.Same(t, w.integer, v.Type())
	require.EqualValues(t, 5, v.Pos())
}

func TestNewParam(t *testing.T) {
	w := newWorld(t, "")
	p := types.NewParam(3, "a", w.integer, types.KeywordOnly, types.WithDefault())
	require.Equal(t, types.ParamVar, p.Kind())
	require.Equal(t, types.KeywordOnly, p.ParamKind())
	require.True(t, p.HasDefault())
	require.Equal(t, "a", p.Name())
	require.False(t, types.NewParam(0, "a", w.integer, types.PositionalOnly).HasDefault())
}
