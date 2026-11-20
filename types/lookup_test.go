package types_test

import (
	"testing"

	"github.com/siyul-park/spry/types"
	"github.com/stretchr/testify/require"
)

func TestLookupAttr(t *testing.T) {
	w := newWorld(t, `
class Base:
    x: int
    count: ClassVar[int] = 0
    def m(self, a: int) -> str: ...
    def clone(self) -> Self: ...
    @staticmethod
    def s(a: int) -> int: ...
    @classmethod
    def make(cls, a: int) -> Self: ...
    @property
    def name(self) -> str: ...
    @overload
    def ov(self, a: int) -> int: ...
    @overload
    def ov(self, a: str) -> str: ...
    def ov(self, a): ...
class Derived(Base): ...
class Stack[T]:
    items: list[T]
    def push(self, x: T) -> None: ...
class IntStack(Stack[int]): ...
class Sub[U](Stack[list[U]]): ...
class Dyn:
    real: str
    def __getattr__(self, name: str) -> int: ...
class Meta1:
    def registry(self) -> int: ...
class WithMeta(metaclass=Meta1): ...
def bounded[B: Base](x: B) -> B: ...
`)
	base, derived := w.named(t, "Base"), w.named(t, "Derived")
	stack, intStack, sub := w.named(t, "Stack"), w.named(t, "IntStack"), w.named(t, "Sub")
	dyn, withMeta := w.named(t, "Dyn"), w.named(t, "WithMeta")
	bound := w.tparam(t, "bounded", 0)
	mod := w.at(t, "math")
	field := base.Underlying().(*types.Class).Members().Lookup("x")

	tests := []struct {
		name     string
		recv     types.Type
		attr     string
		kind     types.SelectionKind
		typ      string
		params   []string
		notFound bool
	}{
		{name: "instance variable", recv: base, attr: "x", kind: types.FieldVal, typ: "int"},
		{name: "class variable", recv: base, attr: "count", kind: types.ClassVal, typ: "int"},
		{name: "method bound to an instance drops the receiver", recv: base, attr: "m", kind: types.MethodVal, params: []string{"a"}},
		{name: "inherited method", recv: derived, attr: "m", kind: types.MethodVal, params: []string{"a"}},
		{name: "method read from the class keeps the receiver", recv: types.NewMeta(base), attr: "m", kind: types.MethodExpr, params: []string{"self", "a"}},
		{name: "Self becomes the receiver", recv: derived, attr: "clone", kind: types.MethodVal, params: nil},
		{name: "staticmethod through an instance", recv: base, attr: "s", kind: types.ClassVal, params: []string{"a"}},
		{name: "staticmethod through the class", recv: types.NewMeta(base), attr: "s", kind: types.ClassVal, params: []string{"a"}},
		{name: "classmethod through an instance", recv: derived, attr: "make", kind: types.MethodVal, params: []string{"a"}},
		{name: "classmethod through the class", recv: types.NewMeta(derived), attr: "make", kind: types.MethodVal, params: []string{"a"}},
		{name: "property reads the getter result", recv: derived, attr: "name", kind: types.DescriptorVal, typ: "str"},
		{name: "generic field", recv: inst(t, stack, w.integer), attr: "items", kind: types.FieldVal, typ: "list[int]"},
		{name: "generic method", recv: inst(t, stack, w.integer), attr: "push", kind: types.MethodVal, params: []string{"x"}},
		{name: "method of a specialized base", recv: intStack, attr: "items", kind: types.FieldVal, typ: "list[int]"},
		{name: "method of a generic subclass", recv: inst(t, sub, w.str), attr: "items", kind: types.FieldVal, typ: "list[list[str]]"},
		{name: "__getattr__ fallback", recv: dyn, attr: "anything", kind: types.GetattrVal, typ: "int"},
		{name: "declared attribute beats __getattr__", recv: dyn, attr: "real", kind: types.FieldVal, typ: "str"},
		{name: "metaclass method", recv: types.NewMeta(withMeta), attr: "registry", kind: types.MethodVal, params: nil},
		{name: "type attribute", recv: types.NewMeta(base), attr: "__name__", kind: types.FieldVal, typ: "str"},
		{name: "literal receiver", recv: intLit(1), attr: "bit_length", kind: types.MethodVal, params: nil},
		{name: "tuple receiver", recv: elems(-1, w.integer, w.str), attr: "count", kind: types.MethodVal, params: []string{"value"}},
		{name: "bound of a type parameter", recv: bound, attr: "x", kind: types.FieldVal, typ: "int"},
		{name: "module member", recv: mod, attr: "pi", kind: types.FieldVal, typ: "float"},
		{name: "call of a signature", recv: callable(w.integer, w.integer), attr: "__call__", kind: types.MethodVal, typ: "collections.abc.Callable[[int], int]", params: []string{""}},
		{name: "missing attribute", recv: base, attr: "nope", notFound: true},
		{name: "Any", recv: w.anyT, attr: "x", notFound: true},
		{name: "Never", recv: types.Typ[types.Never], attr: "x", notFound: true},
		{name: "union", recv: types.NewUnion(base, w.integer), attr: "x", notFound: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel := types.LookupAttr(tt.recv, tt.attr)
			if tt.notFound {
				require.Nil(t, sel)
				return
			}
			require.NotNil(t, sel)
			require.Equal(t, tt.kind, sel.Kind())
			if tt.typ != "" {
				require.Equal(t, tt.typ, sel.Type().String())
			}
			if tt.params != nil || tt.kind == types.MethodVal || tt.kind == types.MethodExpr {
				require.Equal(t, tt.params, paramNames(t, sel.Type()))
			}
		})
	}

	t.Run("selection accessors", func(t *testing.T) {
		sel := types.LookupAttr(base, "x")
		require.Same(t, field, sel.Obj())
		require.Same(t, base, sel.Recv())
		require.Same(t, base, sel.Owner())
	})
	t.Run("Self result", func(t *testing.T) {
		s := types.LookupAttr(derived, "clone").Type().(*types.Signature)
		require.Equal(t, "__main__.Derived", s.Result().String())
		c := types.LookupAttr(types.NewMeta(derived), "make").Type().(*types.Signature)
		require.Equal(t, "__main__.Derived", c.Result().String())
	})
	t.Run("generic method parameter", func(t *testing.T) {
		s := types.LookupAttr(inst(t, stack, w.integer), "push").Type().(*types.Signature)
		require.Equal(t, "int", s.Params().At(0).Type().String())
	})
	t.Run("overload binding", func(t *testing.T) {
		o, ok := types.LookupAttr(base, "ov").Type().(*types.Overload)
		require.True(t, ok)
		require.Equal(t, 2, o.Len())
		require.Equal(t, 1, o.At(1).Params().Len())
	})
}

func paramNames(t *testing.T, typ types.Type) []string {
	t.Helper()
	s, ok := typ.(*types.Signature)
	require.True(t, ok, "%s is not a signature", typ)
	var names []string
	for i := range s.Params().Len() {
		names = append(names, s.Params().At(i).Name())
	}
	return names
}
