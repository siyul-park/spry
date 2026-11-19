package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/types"
)

const typexprPrelude = `
import typing
from typing import Any, Callable, ClassVar, Final, Literal, LiteralString, Never, Optional, Self, TypeGuard, TypeIs, Union, Annotated, Concatenate, TypeAlias, TypeVar, ParamSpec
from collections.abc import Iterable, Iterator, Generator, Mapping

class C: ...
class D[X]: ...
class E[A, B = int]: ...
class Bounded[T: int]: ...
`

func checkAnnotations(t *testing.T) {
	tests := []struct {
		name string
		ann  string
		want string
	}{
		{"int", "int", "int"},
		{"None", "None", "None"},
		{"class", "C", "__main__.C"},
		{"list", "list[int]", "list[int]"},
		{"dict", "dict[str, int]", "dict[str, int]"},
		{"bare generic is Any", "list", "list[typing.Any]"},
		{"bare dict", "dict", "dict[typing.Any, typing.Any]"},
		{"nested", "list[dict[str, tuple[int, ...]]]", "list[dict[str, tuple[int, ...]]]"},
		{"user generic", "D[int]", "__main__.D[int]"},
		{"default argument", "E[str]", "__main__.E[str, int]"},
		{"abc", "Iterable[int]", "collections.abc.Iterable[int]"},
		{"generator", "Generator[int, None, str]", "collections.abc.Generator[int, None, str]"},
		{"union operator", "int | None", "int | None"},
		{"union three", "int | str | None", "int | str | None"},
		{"Optional", "Optional[int]", "int | None"},
		{"Union", "Union[int, str]", "int | str"},
		{"Union duplicates", "Union[int, int]", "int"},
		{"Literal", "Literal[1, 'a']", "typing.Literal[1, 'a']"},
		{"Literal negative", "Literal[-3]", "typing.Literal[-3]"},
		{"Literal bool and None", "Literal[True, None]", "typing.Literal[True] | None"},
		{"Callable", "Callable[[int, str], bool]", "collections.abc.Callable[[int, str], bool]"},
		{"Callable empty", "Callable[[], int]", "collections.abc.Callable[[], int]"},
		{"Callable gradual", "Callable[..., int]", "collections.abc.Callable[..., int]"},
		{"abc Callable", "typing.Callable[[int], None]", "collections.abc.Callable[[int], None]"},
		{"tuple fixed", "tuple[int, str]", "tuple[int, str]"},
		{"tuple unbounded", "tuple[int, ...]", "tuple[int, ...]"},
		{"tuple empty", "tuple[()]", "tuple[()]"},
		{"bare tuple", "tuple", "tuple[typing.Any, ...]"},
		{"type", "type[C]", "type[__main__.C]"},
		{"bare type", "type", "type[typing.Any]"},
		{"Annotated", "Annotated[int, 'meta']", "int"},
		{"Any", "Any", "typing.Any"},
		{"Never", "Never", "typing.Never"},
		{"LiteralString", "LiteralString", "typing.LiteralString"},
		{"TypeGuard", "TypeGuard[int]", "typing.TypeGuard[int]"},
		{"TypeIs", "TypeIs[int]", "typing.TypeIs[int]"},
		{"float", "float", "float"},
		{"forward reference", "'list[C]'", "list[__main__.C]"},
		{"forward reference in union", "'C' | None", "__main__.C | None"},
		{"module attribute", "typing.List[int]", "list[int]"},
		{"typing alias", "typing.Dict[str, int]", "dict[str, int]"},
		{"typing tuple alias", "typing.Tuple[int, str]", "tuple[int, str]"},
		{"typing type alias", "typing.Type[int]", "type[int]"},
		{"bounded argument", "Bounded[bool]", "__main__.Bounded[bool]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := mustCheck(t, typexprPrelude+"x: "+tt.ann+"\n")
			require.Equal(t, tt.want, c.typeOf(t, "x").String())
		})
	}

	declarations := []struct {
		name string
		src  string
		want map[string]string
	}{
		{"type statement", "type X = list[int]\ny: X\n", map[string]string{"X": "X", "y": "X"}},
		{"generic type statement", "type X[T] = list[T]\ny: X[int]\n", map[string]string{"y": "X[int]"}},
		{"legacy alias", "Y = list[int]\ny: Y\n", map[string]string{"y": "list[int]"}},
		{"explicit alias", "Y: TypeAlias = int | None\ny: Y\n", map[string]string{"y": "int | None"}},
		{"legacy type variable", "T = TypeVar('T')\ny: list[T]\n", map[string]string{"y": "list[~T]"}},
		{"covariant type variable", "T = TypeVar('T', covariant=True)\ny: list[T]\n", map[string]string{"y": "list[+T]"}},
		{"ParamSpec", "P = ParamSpec('P')\ny: Callable[P, int]\n", map[string]string{"y": "collections.abc.Callable[~P, int]"}},
		{"Concatenate", "P = ParamSpec('P')\ny: Callable[Concatenate[int, P], int]\n", map[string]string{"y": "collections.abc.Callable[typing.Concatenate[int, ~P], int]"}},
		{"function signature", "def f(a: int, b: str = 'x', *args: int, c: bool, **kw: str) -> list[int]: ...\n", map[string]string{"f": "(a: int, b: str = ..., *args: int, c: bool, **kw: str) -> list[int]"}},
		{"positional only", "def f(a: int, /, b: int) -> None: ...\n", map[string]string{"f": "(a: int, /, b: int) -> None"}},
		{"generic function", "def f[T](x: T) -> T: ...\n", map[string]string{"f": "[T](x: T) -> T"}},
		{"legacy generic function", "T = TypeVar('T')\ndef f(x: T) -> T: ...\n", map[string]string{"f": "(x: ~T) -> ~T"}},
		{"unannotated is implicit", "def f(a, b=1): pass\n", map[string]string{"f": "[a@f, b@f, return@f](a: a@f, b: b@f = ...) -> return@f"}},
		{"overload", "from typing import overload\n@overload\ndef f(x: int) -> int: ...\n@overload\ndef f(x: str) -> str: ...\ndef f(x): return x\n", map[string]string{"f": "Overload[(x: int) -> int, (x: str) -> str]"}},
		{"Final", "x: Final[int] = 1\n", map[string]string{"x": "int"}},
		{"Final inferred", "x: Final = 1\n", map[string]string{"x": "int"}},
		{"constant", "x = 1.5\ns = 'a'\nn = None\n", map[string]string{"x": "float", "s": "str", "n": "None"}},
		{"NewType", "from typing import NewType\nUserId = NewType('UserId', int)\ny: UserId\n", map[string]string{"y": "__main__.UserId"}},
		{"Protocol", "from typing import Protocol\nclass P(Protocol):\n    def m(self) -> int: ...\ny: P\n", map[string]string{"y": "__main__.P"}},
		{"Self", "class K:\n    def m(self) -> Self: ...\n", map[string]string{}},
		{"classmethod receiver", "class K:\n    @classmethod\n    def make(cls) -> int: ...\n", map[string]string{}},
		{"generic class members", "class Box[T]:\n    item: T\n    def get(self) -> T: ...\nb: Box[int]\n", map[string]string{"b": "__main__.Box[int]"}},
	}
	for _, tt := range declarations {
		t.Run(tt.name, func(t *testing.T) {
			c := mustCheck(t, typexprPrelude+tt.src)
			for name, want := range tt.want {
				require.Equal(t, want, c.typeOf(t, name).String(), name)
			}
		})
	}

	t.Run("qualifiers", func(t *testing.T) {
		c := mustCheck(t, typexprPrelude+"class K:\n    a: ClassVar[int] = 0\n    b: int\n    c: Final[int] = 0\n")
		members := c.mod.Scope().Lookup("K").Type().(*types.Named).Underlying().(*types.Class).Members()
		require.Equal(t, types.ClassVar, members.Lookup("a").(*types.Var).Kind())
		require.Equal(t, types.FieldVar, members.Lookup("b").(*types.Var).Kind())
		require.Equal(t, types.Final, members.Lookup("c").(*types.Var).Qualifiers())
	})

	t.Run("methods bind Self", func(t *testing.T) {
		c := mustCheck(t, typexprPrelude+"class K:\n    def m(self, a: int) -> Self: ...\n    @classmethod\n    def make(cls) -> Self: ...\n    @staticmethod\n    def s(a: int) -> int: ...\n    @property\n    def p(self) -> int: ...\n")
		k := c.mod.Scope().Lookup("K").Type().(*types.Named)
		require.Equal(t, "(a: int) -> __main__.K", types.LookupAttr(k, "m").Type().String())
		require.Equal(t, types.MethodVal, types.LookupAttr(k, "m").Kind())
		require.Equal(t, types.MethodVal, types.LookupAttr(types.NewMeta(k), "make").Kind())
		require.Equal(t, types.ClassVal, types.LookupAttr(k, "s").Kind())
		require.Equal(t, types.DescriptorVal, types.LookupAttr(k, "p").Kind())
		require.Equal(t, "int", types.LookupAttr(k, "p").Type().String())
	})

	t.Run("bases and MRO", func(t *testing.T) {
		c := mustCheck(t, typexprPrelude+"class A: ...\nclass B(A): ...\nclass M(B, Iterable[int]): ...\n")
		m := c.mod.Scope().Lookup("M").Type().(*types.Named)
		var names []string
		for _, e := range m.Underlying().(*types.Class).MRO() {
			names = append(names, e.String())
		}
		require.Equal(t, []string{"__main__.M", "__main__.B", "__main__.A", "collections.abc.Iterable[int]", "object"}, names)
		require.True(t, types.AssignableTo(m, c.mod.Scope().Lookup("A").Type()))
	})

	t.Run("legacy generic base", func(t *testing.T) {
		c := mustCheck(t, typexprPrelude+"from typing import Generic\nT = TypeVar('T')\nclass G(Generic[T]):\n    pass\ny: G[int]\n")
		require.Equal(t, "__main__.G[int]", c.typeOf(t, "y").String())
	})

	t.Run("variance is inferred", func(t *testing.T) {
		c := mustCheck(t, typexprPrelude+"class Out[T]:\n    def get(self) -> T: ...\nclass In[T]:\n    def put(self, x: T) -> None: ...\nclass Both[T]:\n    def get(self) -> T: ...\n    def put(self, x: T) -> None: ...\n")
		variance := func(name string) types.Variance {
			return c.mod.Scope().Lookup(name).Type().(*types.Named).TypeParams().At(0).Variance()
		}
		require.Equal(t, types.Covariant, variance("Out"))
		require.Equal(t, types.Contravariant, variance("In"))
		require.Equal(t, types.Invariant, variance("Both"))
	})

	t.Run("type parameter bound and default", func(t *testing.T) {
		c := mustCheck(t, typexprPrelude+"def f[T: int, U = str](x: T, y: U) -> T: ...\n")
		sig := c.mod.Scope().Lookup("f").Type().(*types.Signature)
		require.Equal(t, "int", sig.TypeParams().At(0).Bound().String())
		require.Equal(t, "str", sig.TypeParams().At(1).Default().String())
	})

	t.Run("recursive alias", func(t *testing.T) {
		c := mustCheck(t, typexprPrelude+"type Tree = list[Tree] | int\ny: Tree\n")
		require.Equal(t, "Tree", c.typeOf(t, "y").String())
	})

	t.Run("expressions are types or values", func(t *testing.T) {
		c := mustCheck(t, "x: list[int] = [1]\nprint(x)\n")
		modes := map[string]string{}
		for e, tv := range c.info.Types {
			n, ok := e.(*ast.Name)
			if !ok {
				continue
			}
			switch {
			case tv.IsType():
				modes[n.ID] = "type"
			case tv.IsValue():
				modes[n.ID] = "value"
			}
		}
		require.Equal(t, map[string]string{"int": "type", "print": "value", "x": "value"}, modes)
	})

	t.Run("annotation expressions are recorded", func(t *testing.T) {
		c := mustCheck(t, typexprPrelude+"x: list[int]\n")
		var found bool
		for e, tv := range c.info.Types {
			if tv.Type != nil && tv.Type.String() == "list[int]" && tv.IsType() {
				found = true
				_ = e
			}
		}
		require.True(t, found)
		require.NotEmpty(t, c.info.Instances)
	})

	errors := []struct {
		name string
		ann  string
	}{
		{"too many arguments", "list[int, str]"},
		{"too few arguments", "dict[int]"},
		{"bound violated", "Bounded[str]"},
		{"unknown name", "Nope"},
		{"not a type", "1"},
		{"not generic", "int[str]"},
		{"variable", "x2"},
		{"bad Literal", "Literal[1.5]"},
		{"qualifier in argument", "list[Final[int]]"},
		{"Self outside class", "Self"},
	}
	for _, tt := range errors {
		t.Run("error "+tt.name, func(t *testing.T) {
			c := check(t, typexprPrelude+"x2 = 1\nx: "+tt.ann+"\n")
			var list types.ErrorList
			require.ErrorAs(t, c.err, &list)
			require.Equal(t, "TYP140", list[0].Code)
			require.ErrorIs(t, c.err, types.ErrAnnotation)
		})
	}

	t.Run("type argument errors wrap ErrTypeArg", func(t *testing.T) {
		c := check(t, typexprPrelude+"x: list[int, str]\n")
		var list types.ErrorList
		require.ErrorAs(t, c.err, &list)
		require.Contains(t, list[0].Msg, "too many type arguments")
	})

	t.Run("inconsistent MRO", func(t *testing.T) {
		c := check(t, "class A: ...\nclass B(A): ...\nclass C(A, B): ...\n")
		var list types.ErrorList
		require.ErrorAs(t, c.err, &list)
		require.Contains(t, list[0].Msg, "method resolution order")
	})
}
