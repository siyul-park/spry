package types_test

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
	"testing/fstest"

	"github.com/siyul-park/spry/types"
	"github.com/stretchr/testify/require"
)

type typeRepr struct {
	ID   string
	Kind string
	Repr string
	Src  string
}

// reprOverrides gives the rows whose CPython source cannot be checked as it
// is (a value expression, a runtime attribute, a TypeVar named differently
// from its variable) a source of the same type that can, and where its type
// lives.
var reprOverrides = map[string]struct {
	src  string
	pick func(t *testing.T, c *checked) types.Type
}{
	"union-optional":             {"from typing import TypeAlias\nT: TypeAlias = int | None\n", pickVar("T")},
	"union-three":                {"from typing import TypeAlias\nT: TypeAlias = int | str | None\n", pickVar("T")},
	"literal-merged":             {"from typing import Literal\nx: Literal[1, 2] | None\n", pickVar("x")},
	"typeparam-covariant":        {"from typing import TypeVar\nT0 = TypeVar('T0', covariant=True)\n", pickVar("T0")},
	"typeparam-contravariant":    {"from typing import TypeVar\nT0 = TypeVar('T0', contravariant=True)\n", pickVar("T0")},
	"typeparam-pep695":           {"type G[X] = list[X]\nclass K[X]: ...\n", pickTypeParam("K")},
	"typeparam-paramspec-legacy": {"from typing import ParamSpec\nP0 = ParamSpec('P0')\n", pickVar("P0")},
	"typeparam-paramspec-pep695": {"class K[**P0]: ...\n", pickTypeParam("K")},
	"typeparam-typevartuple":     {"from typing import TypeVarTuple\nTs = TypeVarTuple('Ts')\n", pickVar("Ts")},
	"typeparam-self": {"from typing import Self\nclass K:\n    def m(self) -> Self: ...\n", func(t *testing.T, c *checked) types.Type {
		m := c.typeOf(t, "K").(*types.Named).Underlying().(*types.Class).Members().Lookup("m")
		return m.Type().(*types.Signature).Result()
	}},
}

func pickVar(name string) func(*testing.T, *checked) types.Type {
	return func(t *testing.T, c *checked) types.Type { return c.typeOf(t, name) }
}

func pickTypeParam(class string) func(*testing.T, *checked) types.Type {
	return func(t *testing.T, c *checked) types.Type { return c.typeOf(t, class).(*types.Named).TypeParams().At(0) }
}

func TestTypeString(t *testing.T) {
	f, err := os.Open("testdata/cpython/typerepr.jsonl")
	require.NoError(t, err)
	defer f.Close()
	var rows []typeRepr
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r typeRepr
		require.NoError(t, json.Unmarshal(sc.Bytes(), &r))
		rows = append(rows, r)
	}
	require.NoError(t, sc.Err())
	require.NotEmpty(t, rows)

	for _, r := range rows {
		t.Run(r.ID, func(t *testing.T) {
			src, pick := r.Src, pickVar("T")
			if o, ok := reprOverrides[r.ID]; ok {
				src, pick = o.src, o.pick
			}
			c := mustCheck(t, src)
			typ := pick(t, c)
			require.Equal(t, r.Repr, types.TypeString(typ, nil))
			require.Equal(t, r.Repr, typ.String())
		})
	}

	tests := []struct {
		name string
		src  string
		of   string
		want string
	}{
		{"generic signature", "T = TypeVar('T')\ndef f(x: T) -> T: ...\n", "f", "(x: ~T) -> ~T"},
		{"PEP 695 generic signature", "def f[T](x: T) -> T: ...\n", "f", "[T](x: T) -> T"},
		{"default parameter", "def f(a: int, b: str = 'x') -> int: ...\n", "f", "(a: int, b: str = ...) -> int"},
		{"overload", "@overload\ndef f(x: int) -> int: ...\n@overload\ndef f(x: str) -> str: ...\ndef f(x): ...\n", "f", "Overload[(x: int) -> int, (x: str) -> str]"},
		{"enum literal", "class E:\n    A: 'E'\nx: Literal[E.A]\n", "x", "typing.Literal[__main__.E.A]"},
		{"module", "import math\n", "math", "<module 'math'>"},
		{"positional only keyword only", "def f(a: int, b: int, /, *, c: int) -> int: ...\n", "f", "(a: int, b: int, /, *, c: int) -> int"},
		{"tuple argument", "x: list[tuple[int, str]]\n", "x", "list[tuple[int, str]]"},
		{"empty signature", "def f() -> int: ...\n", "f", "collections.abc.Callable[[], int]"},
	}
	for _, tt := range tests {
		t.Run("deviation "+tt.name, func(t *testing.T) {
			c := mustCheck(t, worldPrelude+tt.src)
			require.Equal(t, tt.want, types.TypeString(c.typeOf(t, tt.of), nil))
		})
	}
	t.Run("deviation nil", func(t *testing.T) {
		require.Equal(t, "<nil>", types.TypeString(nil, nil))
	})
}

func TestRelativeTo(t *testing.T) {
	c := mustCheck(t, "from pkg.mod import D\nclass C: ...\nu: C | D | int\n", types.WithFS(fstest.MapFS{
		"pkg/__init__.py": {},
		"pkg/mod.py":      {Data: []byte("class D: ...\n")},
	}, "/app"))
	u := c.typeOf(t, "u")

	tests := []struct {
		name string
		qf   types.Qualifier
		want string
	}{
		{"relative to the declaring module", types.RelativeTo(c.mod), "C | pkg.mod.D | int"},
		{"relative to another module", types.RelativeTo(c.mod.Imports()[1]), "__main__.C | D | int"},
		{"custom qualifier", func(m *types.Module) string { return "m" }, "m.C | m.D | m.int"},
		{"default qualifier", nil, "__main__.C | pkg.mod.D | int"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, types.TypeString(u, tt.qf))
		})
	}
}
