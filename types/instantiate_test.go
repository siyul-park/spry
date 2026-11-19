package types_test

import (
	"testing"

	"github.com/siyul-park/spry/types"
	"github.com/stretchr/testify/require"
)

func TestInstantiate(t *testing.T) {
	w := newWorld(t, `
class Pair[K, V]: ...
class Defaulted[F, D = str]: ...
class Chained[F, E = list[F]]: ...
class Bound[B: Animal]: ...
class Constraint[C: (int, str)]: ...
class Variadic[*Ts, Z]: ...
class Spec[**P]: ...
class Spec2[**P2, V2]: ...
class Plain: ...
Q = ParamSpec('Q')
def genericFn[X](a: X) -> X | None: ...
type Mapping[K] = list[K]
def forwarding[**P](*args: P.args, **kwargs: P.kwargs) -> None: ...
def forward[A, **P](*args: P.args, **kwargs: P.kwargs) -> None: ...
`)
	at := func(name string) types.Type { return w.at(t, name) }
	pair, defaulted, chained, bound := at("Pair"), at("Defaulted"), at("Chained"), at("Bound")
	constraint, variadic, spec, spec2, plain := at("Constraint"), at("Variadic"), at("Spec"), at("Spec2"), at("Plain")
	genericFn, alias, forward, forwarding := at("genericFn"), at("Mapping"), at("forward"), at("forwarding")

	tests := []struct {
		name string
		orig types.Type
		args []types.Type
		want string
	}{
		{"class", pair, []types.Type{w.integer, w.str}, "__main__.Pair[int, str]"},
		{"default fills the missing argument", defaulted, []types.Type{w.integer}, "__main__.Defaulted[int, str]"},
		{"explicit argument beats the default", defaulted, []types.Type{w.integer, w.integer}, "__main__.Defaulted[int, int]"},
		{"default refers to an earlier argument", chained, []types.Type{w.integer}, "__main__.Chained[int, list[int]]"},
		{"bound is satisfied", bound, []types.Type{w.dog}, "__main__.Bound[__main__.Dog]"},
		{"constraint is satisfied", constraint, []types.Type{w.str}, "__main__.Constraint[str]"},
		{"TypeVarTuple packs its arguments", variadic, []types.Type{w.integer, w.str, w.float}, "__main__.Variadic[int, str, float]"},
		{"TypeVarTuple may be empty", variadic, []types.Type{w.float}, "__main__.Variadic[*tuple[()], float]"},
		{"single ParamSpec takes loose parameters", spec, []types.Type{w.integer, w.str}, "__main__.Spec[[int, str]]"},
		{"ParamSpec takes a parameter list", spec, []types.Type{elems(-1, w.integer)}, "__main__.Spec[[int]]"},
		{"ParamSpec takes another ParamSpec", spec, []types.Type{at("Q")}, "__main__.Spec[~Q]"},
		{"signature", genericFn, []types.Type{w.integer}, "(a: int) -> int | None"},
		{"alias", alias, []types.Type{w.str}, "Mapping[str]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := types.Instantiate(tt.orig, tt.args)
			require.NoError(t, err)
			require.Equal(t, tt.want, got.String())
		})
	}

	t.Run("alias expands with its arguments", func(t *testing.T) {
		got, err := types.Instantiate(alias, []types.Type{w.str})
		require.NoError(t, err)
		require.Equal(t, "list[str]", types.Unalias(got).String())
	})
	t.Run("signature loses its type parameters", func(t *testing.T) {
		got, err := types.Instantiate(genericFn, []types.Type{w.integer})
		require.NoError(t, err)
		require.Nil(t, got.(*types.Signature).TypeParams())
	})
	t.Run("ParamSpec forwarding is spliced", func(t *testing.T) {
		got, err := types.Instantiate(forwarding, []types.Type{elems(-1, w.integer, w.str)})
		require.NoError(t, err)
		require.Equal(t, "collections.abc.Callable[[int, str], None]", got.String())
	})

	errs := []struct {
		name string
		orig types.Type
		args []types.Type
	}{
		{"too many arguments", pair, []types.Type{w.integer, w.str, w.float}},
		{"too few arguments", pair, []types.Type{w.integer}},
		{"too few for TypeVarTuple", variadic, nil},
		{"bound violated", bound, []types.Type{w.integer}},
		{"constraint violated", constraint, []types.Type{w.float}},
		{"not generic", plain, []types.Type{w.integer}},
		{"already instantiated", mustInst(t, pair, w.integer, w.str), []types.Type{w.integer}},
		{"not a class", w.anyT, []types.Type{w.integer}},
		{"ParamSpec needs a parameter list", spec2, []types.Type{w.integer, w.str}},
		{"forward signature without arguments", forward, nil},
	}
	for _, tt := range errs {
		t.Run(tt.name, func(t *testing.T) {
			_, err := types.Instantiate(tt.orig, tt.args)
			require.ErrorIs(t, err, types.ErrTypeArg)
		})
	}
}

// TestInfer observes the inference of type arguments through AssignableTo: a
// generic signature is assignable to a callable when the arguments inferred
// from the callable's parameters give a matching result.
func TestInfer(t *testing.T) {
	w := newWorld(t, `
class MyList(list[str]): ...
def direct[T](x: T) -> T: ...
def generic[T](x: list[T]) -> T: ...
def joined[T](x: T, y: T) -> T: ...
def pair[T, U](x: T, y: U) -> tuple[T, U]: ...
def optional[T](x: T | None) -> T: ...
def unbounded[T](x: tuple[T, ...]) -> T: ...
def fixed[T, U](x: tuple[T, U]) -> tuple[T, U]: ...
def variadic[T, *Ts](x: tuple[T, *Ts]) -> tuple[*Ts]: ...
def callback[T, U](x: Callable[[T], U]) -> tuple[T, U]: ...
def instance[T](x: type[T]) -> T: ...
def defaulted[D = str]() -> D: ...
def unbound[T]() -> T: ...
def limited[B: Animal](x: B) -> B: ...
`)
	list := func(e types.Type) types.Type { return w.list(t, e) }
	opt := func(x types.Type) types.Type { return types.NewUnion(x, w.none) }

	tests := []struct {
		name  string
		fn    string
		args  []types.Type
		want  types.Type
		wrong types.Type
	}{
		{"direct", "direct", []types.Type{w.integer}, w.integer, w.str},
		{"through a generic class", "generic", []types.Type{list(w.integer)}, w.integer, w.str},
		{"through a base class", "generic", []types.Type{w.at(t, "MyList")}, w.str, w.integer},
		{"multiple bindings are joined", "joined", []types.Type{w.integer, w.str}, types.NewUnion(w.integer, w.str), w.integer},
		{"two parameters", "pair", []types.Type{w.integer, w.str}, elems(-1, w.integer, w.str), elems(-1, w.str, w.integer)},
		{"optional parameter against a plain argument", "optional", []types.Type{w.integer}, w.integer, w.str},
		{"optional parameter against optional argument", "optional", []types.Type{opt(w.integer)}, w.integer, w.str},
		{"tuple element", "unbounded", []types.Type{elems(-1, w.integer, w.integer)}, w.integer, w.str},
		{"fixed tuple", "fixed", []types.Type{elems(-1, w.integer, w.str)}, elems(-1, w.integer, w.str), elems(-1, w.str, w.integer)},
		{"variadic tuple", "variadic", []types.Type{elems(-1, w.integer, w.str, w.float)}, elems(-1, w.str, w.float), elems(-1, w.integer)},
		{"callable", "callback", []types.Type{callable(w.str, w.integer)}, elems(-1, w.integer, w.str), elems(-1, w.str, w.integer)},
		{"class object", "instance", []types.Type{types.NewMeta(w.dog)}, w.dog, w.cat},
		{"default for an unbound parameter", "defaulted", nil, w.str, w.integer},
		{"Any for an unbound parameter", "unbound", nil, w.anyT, nil},
		{"bound is satisfied", "limited", []types.Type{w.dog}, w.dog, w.integer},
		{"bound violation leaves the signature generic", "limited", []types.Type{w.integer}, nil, w.integer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := w.at(t, tt.fn)
			if tt.want != nil {
				require.True(t, types.AssignableTo(fn, callable(tt.want, tt.args...)), "result %s", tt.want)
			}
			if tt.wrong != nil {
				require.False(t, types.AssignableTo(fn, callable(tt.wrong, tt.args...)), "result %s", tt.wrong)
			}
		})
	}
}

func mustInst(t *testing.T, orig types.Type, args ...types.Type) types.Type {
	t.Helper()
	return inst(t, orig, args...)
}
