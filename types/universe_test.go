package types_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/types"
)

func TestUniverse(t *testing.T) {
	class := func(name string) *types.Named {
		t.Helper()
		obj := types.Universe.Lookup(name)
		require.NotNil(t, obj, name)
		n, ok := obj.Type().(*types.Named)
		require.True(t, ok, name)
		return n
	}

	t.Run("builtin classes", func(t *testing.T) {
		for _, name := range []string{"object", "type", "int", "bool", "float", "complex", "str", "bytes", "list", "dict", "set", "frozenset", "tuple", "range", "BaseException", "Exception", "ValueError"} {
			require.Equal(t, name, class(name).String()[:len(name)], name)
		}
	})

	t.Run("list parameters and MRO", func(t *testing.T) {
		list := class("list")
		require.Equal(t, 1, list.TypeParams().Len())
		var names []string
		for _, e := range list.Underlying().(*types.Class).MRO() {
			names = append(names, e.Obj().Name())
		}
		require.Equal(t, "list", names[0])
		require.Equal(t, "object", names[len(names)-1])
		require.Contains(t, names, "MutableSequence")
	})

	t.Run("bool derives int", func(t *testing.T) {
		require.True(t, types.AssignableTo(class("bool"), class("int")))
		require.False(t, types.AssignableTo(class("int"), class("bool")))
	})

	t.Run("exception hierarchy", func(t *testing.T) {
		require.True(t, types.AssignableTo(class("KeyError"), class("LookupError")))
		require.True(t, types.AssignableTo(class("ValueError"), class("BaseException")))
	})

	t.Run("method types", func(t *testing.T) {
		list := class("list")
		inst, err := types.Instantiate(list, []types.Type{class("int")})
		require.NoError(t, err)
		sel := types.LookupAttr(inst, "append")
		require.NotNil(t, sel)
		require.Equal(t, "collections.abc.Callable[[int], None]", sel.Type().String())
	})

	t.Run("descriptors", func(t *testing.T) {
		for _, name := range []string{"property", "staticmethod", "classmethod"} {
			require.NotNil(t, class(name), name)
		}
	})

	t.Run("builtin functions", func(t *testing.T) {
		for _, name := range []string{"len", "print", "isinstance", "range", "abs", "sorted", "min", "max", "sum", "any", "all"} {
			require.NotNil(t, types.Universe.Lookup(name), name)
		}
		require.IsType(t, &types.Signature{}, types.Universe.Lookup("len").Type())
	})

	t.Run("fixed types", func(t *testing.T) {
		require.Equal(t, "typing.Any", types.Typ[types.Any].String())
		require.Equal(t, "typing.Never", types.Typ[types.Never].String())
	})

	t.Run("protocol classes", func(t *testing.T) {
		c := mustCheck(t, "from collections.abc import Iterable, Sized, Mapping, Sequence\na: Iterable[int]\nb: Sized\nc: Mapping[str, int]\nd: Sequence[int]\n")
		kind := func(name string) types.ClassKind {
			return c.typeOf(t, name).(*types.Named).Origin().Underlying().(*types.Class).Kind()
		}
		require.Equal(t, types.ProtocolClass, kind("a"))
		require.Equal(t, types.ProtocolClass, kind("b"))
		require.Equal(t, types.OrdinaryClass, kind("c"))
		require.Equal(t, types.OrdinaryClass, kind("d"))
	})

	t.Run("structural assignment", func(t *testing.T) {
		c := mustCheck(t, "from collections.abc import Iterable, Sized\nxs: list[int]\nit: Iterable[int]\nsz: Sized\n")
		require.True(t, types.AssignableTo(c.typeOf(t, "xs"), c.typeOf(t, "it")))
		require.True(t, types.AssignableTo(c.typeOf(t, "xs"), c.typeOf(t, "sz")))
	})

	t.Run("variance", func(t *testing.T) {
		tp := func(name string) types.Variance {
			return class(name).TypeParams().At(0).Variance()
		}
		require.Equal(t, types.Invariant, tp("list"))
		require.Equal(t, types.Invariant, tp("set"))
		require.Equal(t, types.Covariant, tp("frozenset"))
		require.Equal(t, types.Covariant, tp("tuple"))
	})

	t.Run("special forms", func(t *testing.T) {
		names := []string{"Optional", "Union", "Literal", "Callable", "Annotated", "TypeVar", "Self"}
		c := mustCheck(t, "from typing import "+strings.Join(names, ", ")+"\n")
		for _, name := range names {
			require.IsType(t, &types.Builtin{}, c.mod.Scope().Lookup(name), name)
		}
	})

	t.Run("no mutation by checks", func(t *testing.T) {
		before := class("int").String()
		mustCheck(t, "x: int = 1\nclass C(int): ...\n")
		require.Equal(t, before, class("int").String())
	})
}
