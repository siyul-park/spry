package types_test

import (
	"testing"

	"github.com/siyul-park/spry/types"
	"github.com/stretchr/testify/require"
)

func TestClass_MRO(t *testing.T) {
	w := newWorld(t, `
class A: ...
class B(A): ...
class C(A): ...
class D(B, C): ...
class G[T]: ...
class S(G[int]): ...
@final
@runtime_checkable
class P(Protocol):
    def m(self) -> int: ...
class Meta: ...
class K(metaclass=Meta): ...
class KSub(K): ...
`)
	names := func(c *types.Named) []string {
		var out []string
		for _, e := range c.Underlying().(*types.Class).MRO() {
			out = append(out, e.String())
		}
		return out
	}
	a, b, d, s := w.named(t, "A"), w.named(t, "B"), w.named(t, "D"), w.named(t, "S")

	t.Run("implicit object", func(t *testing.T) {
		require.Equal(t, []string{"__main__.A", "object"}, names(a))
		require.Equal(t, []types.Type{w.object}, a.Underlying().(*types.Class).Bases())
	})
	t.Run("single inheritance", func(t *testing.T) {
		require.Equal(t, []string{"__main__.B", "__main__.A", "object"}, names(b))
	})
	t.Run("diamond", func(t *testing.T) {
		require.Equal(t, []string{"__main__.D", "__main__.B", "__main__.C", "__main__.A", "object"}, names(d))
	})
	t.Run("specialized generic base", func(t *testing.T) {
		require.Equal(t, []string{"__main__.S", "__main__.G[int]", "object"}, names(s))
	})
	t.Run("inconsistent order", func(t *testing.T) {
		c := check(t, "class A: ...\nclass B(A): ...\nclass Bad(A, B): ...\n")
		var list types.ErrorList
		require.ErrorAs(t, c.err, &list)
		require.Equal(t, "TYP112", list[0].Code)
		require.Equal(t, "Cannot create a consistent method resolution order (MRO) for bases A, B", list[0].Msg)
	})
	t.Run("accessors", func(t *testing.T) {
		p := w.named(t, "P")
		cls := p.Underlying().(*types.Class)
		require.Equal(t, types.ProtocolClass, cls.Kind())
		require.True(t, cls.Final())
		require.True(t, cls.RuntimeCheckable())
		require.Same(t, p.Obj(), cls.Obj())
		require.Nil(t, cls.Metaclass())
		require.Equal(t, 0, cls.TypeParams().Len())
		require.Equal(t, "class __main__.P", cls.String())
		require.Same(t, cls, cls.Underlying())
		require.NotNil(t, cls.Members())
	})
	t.Run("metaclass is inherited", func(t *testing.T) {
		m, sub := w.named(t, "Meta"), w.named(t, "KSub")
		require.Same(t, m, w.named(t, "K").Underlying().(*types.Class).Metaclass())
		require.Same(t, m, sub.Underlying().(*types.Class).Metaclass())
	})
}
