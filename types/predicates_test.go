package types_test

import (
	"math/big"
	"testing"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/token"
	"github.com/siyul-park/spry/types"
	"github.com/stretchr/testify/require"
)

// world is a checked program together with the builtin classes, so that
// tests build their types by writing Python.
type world struct {
	c                                          *checked
	b                                          map[string]*types.Named
	main                                       *types.Module
	integer, str, float, boolean, none, object types.Type
	anyT                                       types.Type
	animal, dog, cat                           *types.Named
}

// worldPrelude is the Python every world starts from: the typing names the
// tests use and a small class hierarchy.
const worldPrelude = `from typing import Any, Callable, ClassVar, Generic, ParamSpec, Self, Literal, NotRequired, Protocol, TypedDict, TypeVar, TypeGuard, TypeIs, final, overload, runtime_checkable
import math

class Animal: ...
class Dog(Animal): ...
class Cat(Animal): ...
`

func callable(result types.Type, ts ...types.Type) *types.Signature {
	params := make([]*types.Var, len(ts))
	for i, t := range ts {
		params[i] = pos("", t)
	}
	return sig(result, params...)
}

func pos(name string, t types.Type) *types.Var {
	return types.NewParam(token.NoPos, name, t, types.PositionalOnly)
}

func intLit(n int64) *types.Literal { return types.NewLiteral(ast.Int{N: big.NewInt(n)}) }

func param(name string, t types.Type, kind types.ParamKind) *types.Var {
	return types.NewParam(token.NoPos, name, t, kind)
}

func sig(result types.Type, params ...*types.Var) *types.Signature {
	return types.NewSignature(types.NewTuple(params), result)
}

func elems(unbounded int, ts ...types.Type) *types.Tuple {
	vars := make([]*types.Var, len(ts))
	for i, t := range ts {
		vars[i] = types.NewVar(token.NoPos, "", t, types.FieldVar)
	}
	return types.NewTuple(vars, types.WithUnbounded(unbounded))
}

func builtinClasses() map[string]*types.Named {
	out := map[string]*types.Named{}
	for _, name := range types.Universe.Names() {
		if tn, ok := types.Universe.Lookup(name).(*types.TypeName); ok {
			if n, ok := tn.Type().(*types.Named); ok {
				out[name] = n
			}
		}
	}
	return out
}

func TestIdentical(t *testing.T) {
	w := newWorld(t, `
type IntAlias = int
type L1 = list[L1] | int
type L2 = list[L2] | int
gs: TypeIs[int]
gl: TypeGuard[int]
def sa(a: int) -> int: ...
def sb(b: int) -> int: ...
def pa(a: int, /) -> int: ...
def pb(b: int, /) -> int: ...
def gp[T](x: T) -> T: ...
def gq[U](x: U) -> U: ...
@overload
def o1(x: int) -> int: ...
@overload
def o1(x: str) -> str: ...
def o1(x): ...
@overload
def o2(x: int) -> int: ...
@overload
def o2(x: str) -> str: ...
def o2(x): ...
`)
	list := func(e types.Type) types.Type { return w.list(t, e) }
	at := func(name string) types.Type { return w.at(t, name) }

	tests := []struct {
		name string
		x, y types.Type
		want bool
	}{
		{"same class", w.integer, w.integer, true},
		{"different classes", w.integer, w.str, false},
		{"instances with equal arguments", list(w.integer), list(w.integer), true},
		{"instances with different arguments", list(w.integer), list(w.str), false},
		{"bare and applied generic", w.b["list"], list(w.anyT), false},
		{"union order is irrelevant", types.NewUnion(w.integer, w.str), types.NewUnion(w.str, w.integer), true},
		{"union members differ", types.NewUnion(w.integer, w.str), types.NewUnion(w.integer, w.none), false},
		{"literal values", intLit(1), intLit(1), true},
		{"literal values differ", intLit(1), intLit(2), false},
		{"literal kinds differ", intLit(1), types.NewLiteral(ast.Bool(true)), false},
		{"tuple shapes", elems(0, w.integer), elems(-1, w.integer), false},
		{"tuple elements", elems(-1, w.integer, w.str), elems(-1, w.integer, w.str), true},
		{"meta", types.NewMeta(w.dog), types.NewMeta(w.dog), true},
		{"guard strictness", at("gs"), at("gl"), false},
		{"signature parameter names", at("sa"), at("sb"), false},
		{"positional-only names are ignored", at("pa"), at("pb"), true},
		{"generic signatures up to renaming", at("gp"), at("gq"), true},
		{"overload", at("o1"), at("o2"), true},
		{"alias is transparent", at("IntAlias"), w.integer, true},
		{"recursive aliases are compared coinductively", at("L1"), at("L2"), true},
		{"nil", nil, w.integer, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, types.Identical(tt.x, tt.y))
			require.Equal(t, tt.want, types.Identical(tt.y, tt.x))
		})
	}
}

func TestAssignableTo(t *testing.T) {
	w := newWorld(t, `
class Reader(Protocol):
    def read(self) -> int: ...
class File:
    def read(self) -> int: ...
class BadFile:
    def read(self) -> str: ...
class Plain: ...
G_co = TypeVar('G_co', covariant=True)
class Getter(Protocol[G_co]):
    def get(self) -> G_co: ...
class Box:
    def get(self) -> int: ...
C_co = TypeVar('C_co', covariant=True)
class ReadBox(Generic[C_co]): ...
N_contra = TypeVar('N_contra', contravariant=True)
class Sink(Generic[N_contra]): ...
class Movie(TypedDict):
    name: str
    year: int
class Titled(TypedDict):
    name: str
class OptionalTitled(TypedDict):
    name: NotRequired[str]
def bounded[B: Animal](x: B) -> B: ...
def free[F](x: F) -> F: ...
class CallMe:
    def __call__(self, x: int) -> int: ...
class Ctor:
    def __init__(self, x: int) -> None: ...
class Fn(Protocol):
    def __call__(self, x: int, /) -> int: ...
type Tree = list[Tree] | int
gs: TypeIs[int]
@overload
def ov1(x: str, /) -> str: ...
@overload
def ov1(x: int, /) -> int: ...
def ov1(x): ...
@overload
def ov2(x: int, /) -> int: ...
@overload
def ov2(x: str, /) -> int: ...
def ov2(x): ...
`)
	list := func(e types.Type) types.Type { return w.list(t, e) }
	opt := func(x types.Type) types.Type { return types.NewUnion(x, w.none) }
	complexT := types.Type(w.b["complex"])
	at := func(name string) types.Type { return w.at(t, name) }

	reader, file, badFile, plain := at("Reader"), at("File"), at("BadFile"), at("Plain")
	getter, box := at("Getter"), at("Box")
	ro, sink := at("ReadBox"), at("Sink")
	movie, titled, optional := at("Movie"), at("Titled"), at("OptionalTitled")
	bound, free := w.tparam(t, "bounded", 0), w.tparam(t, "free", 0)
	callMe, ctor, ctorAlias := at("CallMe"), at("Ctor"), at("Tree")

	tests := []struct {
		name string
		v, t types.Type
		want bool
	}{
		{"subclass to base", w.dog, w.animal, true},
		{"base to subclass", w.animal, w.dog, false},
		{"anything to object", w.dog, w.object, true},
		{"bool to int", w.boolean, w.integer, true},
		{"int to bool", w.integer, w.boolean, false},
		{"int to float by promotion", w.integer, w.float, true},
		{"bool to float by promotion", w.boolean, w.float, true},
		{"int to complex by promotion", w.integer, complexT, true},
		{"float to int", w.float, w.integer, false},
		{"Any to int", w.anyT, w.integer, true},
		{"int to Any", w.integer, w.anyT, true},
		{"Never to int", types.Typ[types.Never], w.integer, true},
		{"int to Never", w.integer, types.Typ[types.Never], false},
		{"None to int", w.none, w.integer, false},
		{"int to optional int", w.integer, opt(w.integer), true},
		{"None to optional int", w.none, opt(w.integer), true},
		{"optional int to int", opt(w.integer), w.integer, false},
		{"union to wider union", types.NewUnion(w.integer, w.str), types.NewUnion(w.str, w.integer, w.none), true},
		{"union to narrower union", types.NewUnion(w.integer, w.str), types.NewUnion(w.integer, w.none), false},
		{"literal to its class", intLit(1), w.integer, true},
		{"literal to the same literal", intLit(1), intLit(1), true},
		{"literal to another literal", intLit(1), intLit(2), false},
		{"class to literal", w.integer, intLit(1), false},
		{"str literal to LiteralString", types.NewLiteral(ast.Str("a")), types.Typ[types.LiteralString], true},
		{"str to LiteralString", w.str, types.Typ[types.LiteralString], false},
		{"LiteralString to str", types.Typ[types.LiteralString], w.str, true},
		{"guard to bool", at("gs"), w.boolean, true},
		{"invariant argument", list(w.dog), list(w.animal), false},
		{"invariant Any argument", list(w.integer), list(w.anyT), true},
		{"bare generic to applied", w.b["list"], list(w.integer), true},
		{"covariant argument", inst(t, ro, w.dog), inst(t, ro, w.animal), true},
		{"covariant argument reversed", inst(t, ro, w.animal), inst(t, ro, w.dog), false},
		{"contravariant argument", inst(t, sink, w.animal), inst(t, sink, w.dog), true},
		{"contravariant argument reversed", inst(t, sink, w.dog), inst(t, sink, w.animal), false},
		{"tuple covariance", elems(-1, w.dog, w.cat), elems(-1, w.animal, w.animal), true},
		{"tuple length", elems(-1, w.integer), elems(-1, w.integer, w.integer), false},
		{"tuple to unbounded", elems(-1, w.integer, w.integer), elems(0, w.integer), true},
		{"empty tuple to unbounded", elems(-1), elems(0, w.integer), true},
		{"heterogeneous tuple to joined unbounded", elems(-1, w.integer, w.str), elems(0, types.NewUnion(w.integer, w.str)), true},
		{"unbounded to fixed", elems(0, w.integer), elems(-1, w.integer, w.integer), false},
		{"unbounded to unbounded", elems(0, w.dog), elems(0, w.animal), true},
		{"unbounded may be shorter than the prefix", elems(0, w.integer), elems(0, w.integer, w.integer), false},
		{"fixed to prefix and unbounded suffix", elems(-1, w.integer, w.str, w.str), elems(1, w.integer, w.str), true},
		{"fixed with wrong suffix", elems(-1, w.str, w.integer), elems(1, w.integer, w.str), false},
		{"tuple to bare tuple", elems(-1, w.integer, w.str), w.b["tuple"], true},
		{"tuple to object", elems(-1, w.integer), w.object, true},
		{"meta covariance", types.NewMeta(w.dog), types.NewMeta(w.animal), true},
		{"meta covariance reversed", types.NewMeta(w.animal), types.NewMeta(w.dog), false},
		{"meta to type", types.NewMeta(w.dog), w.b["type"], true},
		{"parameter contravariance", callable(w.integer, w.animal), callable(w.integer, w.dog), true},
		{"parameter contravariance reversed", callable(w.integer, w.dog), callable(w.integer, w.animal), false},
		{"result covariance", callable(w.dog), callable(w.animal), true},
		{"result covariance reversed", callable(w.animal), callable(w.dog), false},
		{"extra parameter with default", sig(w.integer, pos("x", w.integer), types.NewParam(0, "y", w.integer, types.PositionalOnly, types.WithDefault())), callable(w.integer, w.integer), true},
		{"extra parameter without default", callable(w.integer, w.integer, w.integer), callable(w.integer, w.integer), false},
		{"missing parameter", callable(w.integer), callable(w.integer, w.integer), false},
		{"variadic accepts positionals", sig(w.integer, param("args", w.integer, types.VarPositional)), callable(w.integer, w.integer, w.integer), true},
		{"gradual signature", sig(w.integer, param("args", w.anyT, types.VarPositional), param("kwargs", w.anyT, types.VarKeyword)), callable(w.integer, w.str), true},
		{"positional-only to keyword-callable", callable(w.integer, w.integer), sig(w.integer, param("x", w.integer, types.PositionalOrKeyword)), false},
		{"keyword-callable to positional-only", sig(w.integer, param("x", w.integer, types.PositionalOrKeyword)), callable(w.integer, w.integer), true},
		{"keyword-only parameters", sig(w.integer, param("k", w.integer, types.KeywordOnly)), sig(w.integer, param("k", w.integer, types.KeywordOnly)), true},
		{"missing keyword-only parameter", sig(w.integer), sig(w.integer, param("k", w.integer, types.KeywordOnly)), false},
		{"generic signature is specialized", at("free"), callable(w.integer, w.integer), true},
		{"overload to signature", at("ov1"), callable(w.integer, w.integer), true},
		{"signature to overload", callable(w.integer, w.anyT), at("ov2"), true},
		{"object with __call__ to signature", callMe, callable(w.integer, w.integer), true},
		{"class to constructor signature", types.NewMeta(ctor), callable(ctor, w.integer), true},
		{"class with wrong constructor signature", types.NewMeta(ctor), callable(ctor, w.str), false},
		{"structural protocol", file, reader, true},
		{"structural protocol with wrong member type", badFile, reader, false},
		{"structural protocol with missing member", plain, reader, false},
		{"generic protocol", box, inst(t, getter, w.integer), true},
		{"generic protocol with other argument", box, inst(t, getter, w.str), false},
		{"signature to protocol with __call__", callable(w.integer, w.integer), at("Fn"), true},
		{"typed dict structural", movie, titled, true},
		{"typed dict missing key", titled, movie, false},
		{"typed dict requiredness", titled, optional, false},
		{"typed dict is not a dict", movie, w.b["dict"], false},
		{"bounded type parameter to its bound", bound, w.animal, true},
		{"bounded type parameter to unrelated", bound, w.integer, false},
		{"unbounded type parameter to object", free, w.object, true},
		{"unbounded type parameter to int", free, w.integer, false},
		{"type parameter to itself", free, free, true},
		{"type parameter in union", free, opt(free), true},
		{"class to type parameter", w.dog, bound, false},
		{"recursive alias member", w.integer, ctorAlias, true},
		{"recursive alias list", list(ctorAlias), ctorAlias, true},
		{"module to class", at("math"), w.integer, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, types.AssignableTo(tt.v, tt.t))
		})
	}
}

func TestDefault(t *testing.T) {
	w := newWorld(t, `
class E:
    A: 'E'
member: Literal[E.A]
gs: TypeIs[int]
`)
	tests := []struct {
		name string
		in   types.Type
		want types.Type
	}{
		{"int literal", intLit(1), w.integer},
		{"bool literal", types.NewLiteral(ast.Bool(true)), w.boolean},
		{"str literal", types.NewLiteral(ast.Str("a")), w.str},
		{"bytes literal", types.NewLiteral(ast.Bytes("a")), w.b["bytes"]},
		{"enum member", w.at(t, "member"), w.at(t, "E")},
		{"LiteralString", types.Typ[types.LiteralString], w.str},
		{"guard", w.at(t, "gs"), w.boolean},
		{"union of literals", types.NewUnion(intLit(1), intLit(2), w.none), types.NewUnion(w.integer, w.none)},
		{"class", w.dog, w.dog},
		{"Any", w.anyT, w.anyT},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.True(t, types.Identical(tt.want, types.Default(tt.in)), types.Default(tt.in).String())
		})
	}
}

// newWorld checks worldPrelude followed by src.
func newWorld(t *testing.T, src string) *world {
	t.Helper()
	b := builtinClasses()
	c := mustCheck(t, worldPrelude+src)
	w := &world{
		c: c, b: b, main: c.mod,
		integer: b["int"], str: b["str"], float: b["float"], boolean: b["bool"], none: b["NoneType"], object: b["object"],
		anyT: types.Typ[types.Any],
	}
	w.animal, w.dog, w.cat = w.named(t, "Animal"), w.named(t, "Dog"), w.named(t, "Cat")
	return w
}

// at returns the type of the module-level name.
func (w *world) at(t *testing.T, name string) types.Type {
	t.Helper()
	return w.c.typeOf(t, name)
}

func (w *world) named(t *testing.T, name string) *types.Named {
	t.Helper()
	n, ok := w.at(t, name).(*types.Named)
	require.True(t, ok, "%s is not a class", name)
	return n
}

// tparam returns the i-th type parameter of the generic function name.
func (w *world) tparam(t *testing.T, name string, i int) *types.TypeParam {
	t.Helper()
	s, ok := w.at(t, name).(*types.Signature)
	require.True(t, ok, "%s is not a function", name)
	return s.TypeParams().At(i)
}

func (w *world) list(t *testing.T, e types.Type) types.Type {
	return inst(t, w.b["list"], e)
}

// method returns the declared method of class c, as the checker made it.
func (w *world) method(t *testing.T, c *types.Named, name string) *types.Func {
	t.Helper()
	f, ok := c.Underlying().(*types.Class).Members().Lookup(name).(*types.Func)
	require.True(t, ok, "%s.%s is not a method", c.Obj().Name(), name)
	return f
}

func inst(t *testing.T, orig types.Type, targs ...types.Type) types.Type {
	t.Helper()
	out, err := types.Instantiate(orig, targs)
	require.NoError(t, err)
	return out
}
