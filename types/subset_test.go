package types_test

import (
	"errors"
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/types"
)

// subsetCase is a construct a TYP code rejects, with the position of the
// diagnostic, or the nearest construct that stays allowed (want is empty).
type subsetCase struct {
	name string
	src  string
	want string // "TYPnnn@line:col" of a rejected construct, empty if accepted
	opts []types.Option
}

// diagnostics lists the errors of a check as "TYPnnn@line:col".
func diagnostics(err error) []string {
	var list types.ErrorList
	if !errors.As(err, &list) {
		return nil
	}
	out := make([]string, len(list))
	for i, e := range list {
		out[i] = fmt.Sprintf("%s@%d:%d", e.Code, e.Pos.Line, e.Pos.Column)
	}
	return out
}

// checkSubset runs one row per rejected construct of every TYP code and the
// allowed near miss next to it.
func checkSubset(t *testing.T) {
	t.Run("TYP100", func(t *testing.T) {
		runSubsetCases(t, []subsetCase{
			{"eval of a literal", "eval(\"1\")\n", "TYP100@1:1", nil},
			{"eval of input", "eval(input())\n", "TYP100@1:1", nil},
			{"exec", "exec(\"x = 1\")\n", "TYP100@1:1", nil},
			{"compile", "compile(\"1\", \"f\", \"eval\")\n", "TYP100@1:1", nil},
			{"eval as a value", "f = eval\n", "TYP100@1:5", nil},
			{"eval in a function", "def f(s):\n    return eval(s)\n", "TYP100@2:12", nil},
			{"shadowed by a def", "def eval(x):\n    return x\nprint(eval(1))\n", "", nil},
			{"shadowed by a parameter", "def f(exec):\n    return exec\nprint(f(1))\n", "", nil},
			{"shadowed by an assignment", "compile = 1\nprint(compile)\n", "", nil},
		})
	})
	t.Run("TYP101", func(t *testing.T) {
		runSubsetCases(t, []subsetCase{
			{"subscript store", "globals()[\"x\"] = 1\n", "TYP101@1:1", nil},
			{"escape to a name", "d = globals()\n", "TYP101@1:5", nil},
			{"update", "globals().update(a=1)\n", "TYP101@1:1", nil},
			{"locals subscript store", "locals()[\"a\"] = 1\n", "TYP101@1:1", nil},
			{"vars store", "vars()[\"a\"] = 1\n", "TYP101@1:1", nil},
			{"function value", "f = locals\n", "TYP101@1:5", nil},
			{"inside a function", "def f():\n    return globals()\n", "TYP101@2:12", nil},
			{"passed as an argument", "print(globals())\n", "TYP101@1:7", nil},
			{"subscript read", "x = 1\nprint(globals()[\"x\"])\n", "", nil},
			{"get", "x = 1\nprint(globals().get(\"x\"))\n", "", nil},
			{"membership", "x = 1\nprint(\"x\" in globals())\n", "", nil},
		})
	})
	t.Run("TYP110", func(t *testing.T) {
		const cls = "class C:\n    def __init__(self):\n        self.a = 1\nc = C()\n"
		runSubsetCases(t, []subsetCase{
			{"store outside the layout", cls + "c.b = 2\n", "TYP110@5:1", nil},
			{"setattr", cls + "setattr(c, \"a\", 1)\n", "TYP110@5:1", nil},
			{"setattr of a variable name", cls + "n = \"a\"\nsetattr(c, n, 1)\n", "TYP110@6:1", nil},
			{"delattr", cls + "delattr(c, \"a\")\n", "TYP110@5:1", nil},
			{"del attribute", cls + "del c.a\n", "TYP110@5:5", nil},
			{"expression in a class body", "class C:\n    print(1)\n", "TYP110@2:5", nil},
			{"loop in a class body", "class C:\n    for i in [1]:\n        pass\n", "TYP110@2:5", nil},
			{"attribute target in a class body", "class C:\n    pass\nclass D:\n    C.x = 1\n", "TYP110@4:5", nil},
			{"store inside the layout", cls + "c.a = 2\n", "", nil},
			{"getattr", cls + "print(getattr(c, \"a\"))\n", "", nil},
			{"class body of defs and variables", "class C:\n    \"\"\"doc\"\"\"\n    n = 1\n    m: int = 2\n    def f(self):\n        pass\n", "", nil},
		})
	})
	t.Run("TYP111", func(t *testing.T) {
		runSubsetCases(t, []subsetCase{
			{"method of a class", "class C:\n    def f(self):\n        pass\ndef g(self):\n    pass\nC.f = g\n", "TYP111@6:1", nil},
			{"variable of a class", "class C:\n    n = 0\nC.n = 1\n", "TYP111@3:1", nil},
			{"module attribute", "import math\nmath.pi = 3\n", "TYP111@2:1", nil},
			{"augmented module attribute", "import math\nmath.pi += 3\n", "TYP111@2:1", nil},
			{"class variable through an instance", "class C:\n    n = 0\nC().n = 1\n", "TYP111@3:1", nil},
			{"read of a module attribute", "import math\nprint(math.pi)\n", "", nil},
			{"store to a field", "class C:\n    def __init__(self):\n        self.a = 1\nc = C()\nc.a = 2\n", "", nil},
		})
	})
	t.Run("TYP112", func(t *testing.T) {
		runSubsetCases(t, []subsetCase{
			{"metaclass keyword", "class M(type):\n    pass\nclass C(metaclass=M):\n    pass\n", "TYP112@3:1", nil},
			{"deriving from type", "class M(type):\n    pass\n", "TYP112@1:1", nil},
			{"init_subclass storing to cls", "class B:\n    def __init_subclass__(cls):\n        cls.x = 1\n", "TYP112@3:9", nil},
			{"impossible MRO", "class A: pass\nclass B: pass\nclass C(A, B): pass\nclass D(B, A): pass\nclass E(C, D): pass\n", "TYP112@5:1", nil},
			{"init_subclass without mutation", "class B:\n    def __init_subclass__(cls):\n        print(1)\nclass C(B):\n    pass\n", "", nil},
			{"consistent MRO", "class A: pass\nclass B(A): pass\nclass C(B, A): pass\n", "", nil},
		})
	})
	t.Run("TYP113", func(t *testing.T) {
		const cls = "class C:\n    def __init__(self):\n        self.a = 1\nclass D(C):\n    pass\nc = C()\n"
		runSubsetCases(t, []subsetCase{
			{"assignment", cls + "c.__class__ = D\n", "TYP113@7:1", nil},
			{"assignment through self", "class C:\n    def f(self):\n        self.__class__ = int\n", "TYP113@3:9", nil},
			{"read", cls + "print(c.__class__)\n", "", nil},
		})
	})
	t.Run("TYP120", func(t *testing.T) {
		star := fstest.MapFS{"m.py": {Data: []byte("def names():\n    return [\"a\"]\n__all__ = names()\na = 1\n")}}
		ok := fstest.MapFS{"m.py": {Data: []byte("__all__ = [\"a\"]\na = 1\n")}}
		runSubsetCases(t, []subsetCase{
			{"__import__ of a variable", "n = \"math\"\n__import__(n)\n", "TYP120@2:1", nil},
			{"__import__ as a value", "f = __import__\n", "TYP120@1:5", nil},
			{"__import__ without arguments", "__import__()\n", "TYP120@1:1", nil},
			{"star import without a literal __all__", "from m import *\n", "TYP120@1:1", []types.Option{types.WithFS(star, "/app")}},
			{"star import with a literal __all__", "from m import *\nprint(a)\n", "", []types.Option{types.WithFS(ok, "/app")}},
		})
	})
	t.Run("TYP121", func(t *testing.T) {
		runSubsetCases(t, []subsetCase{
			{"unshipped module", "import os\n", "TYP121@1:8", nil},
			{"from an unshipped module", "from os import path\n", "TYP121@1:1", nil},
			{"importlib", "import importlib\n", "TYP121@1:8", nil},
			{"guarded unshipped module", "try:\n    import numpy\nexcept ImportError:\n    numpy = None\n", "", nil},
			{"shipped module", "import math\n", "", nil},
		})
	})
	t.Run("TYP130", func(t *testing.T) {
		runSubsetCases(t, []subsetCase{
			{"sys._getframe", "import sys\nsys._getframe()\n", "TYP130@2:1", nil},
			{"from sys import _getframe", "from sys import _getframe\n", "TYP130@1:17", nil},
			{"inspect", "import inspect\n", "TYP130@1:8", nil},
			{"gc", "import gc\n", "TYP130@1:8", nil},
			{"__dict__", "class C:\n    pass\nprint(C().__dict__)\n", "TYP130@3:7", nil},
			{"__annotations__", "class C:\n    pass\nprint(C.__annotations__)\n", "TYP130@3:7", nil},
			{"vars of an object", "class C:\n    pass\nvars(C())\n", "TYP130@3:1", nil},
			{"sys.argv", "import sys\nprint(sys.argv)\n", "", nil},
		})
	})
	t.Run("TYP140", func(t *testing.T) {
		runSubsetCases(t, []subsetCase{
			{"assignment", "x: int = \"a\"\n", "TYP140@1:1", nil},
			{"argument", "def f(a: int):\n    pass\nf(\"s\")\n", "TYP140@3:3", nil},
			{"return", "def f() -> int:\n    return \"s\"\nf()\n", "TYP140@2:5", nil},
			{"default", "def f(a: int = \"s\"):\n    pass\n", "TYP140@1:16", nil},
			{"field", "class C:\n    def __init__(self):\n        self.a: int = 1\nc = C()\nc.a = \"s\"\n", "TYP140@5:1", nil},
			{"invalid annotation", "x: 1 = 1\n", "TYP140@1:4", nil},
			{"float accepts int", "x: float = 1\ndef g(a: float):\n    return a\ng(2)\n", "", nil},
			{"upper bound", "x: object = 1\nclass A: pass\nclass B(A): pass\ny: A = B()\n", "", nil},
			{"Any accepts everything", "from typing import Any\nx: Any = 1\nx = \"s\"\n", "", nil},
		})
	})
	t.Run("TYP150", func(t *testing.T) {
		const gen = "def g():\n    yield 1\n"
		runSubsetCases(t, []subsetCase{
			{"async def", "async def f():\n    pass\n", "TYP150@1:1", nil},
			{"async for", "async def f(x):\n    async for i in x:\n        pass\n", "TYP150@2:5", nil},
			{"async with", "async def f(x):\n    async with x:\n        pass\n", "TYP150@2:5", nil},
			{"await", "async def f(x):\n    await x\n", "TYP150@2:5", nil},
			{"except star", "try:\n    pass\nexcept* ValueError:\n    pass\n", "TYP150@1:1", nil},
			{"complex", "x = 1j\n", "TYP150@1:5", nil},
			{"template string", "x = t\"a\"\n", "TYP150@1:5", nil},
			{"generator throw", gen + "x = g()\nx.throw(ValueError)\n", "TYP150@4:1", nil},
			{"generator close", gen + "x = g()\nx.close()\n", "TYP150@4:1", nil},
			{"yield in try finally", "def g():\n    try:\n        yield 1\n    finally:\n        pass\n", "TYP150@3:9", nil},
			{"yield in with", "def g(m):\n    with m:\n        yield 1\n", "TYP150@3:9", nil},
			{"nested class", "def f():\n    class C:\n        pass\n", "TYP150@2:5", nil},
			{"class in a class", "class A:\n    class B:\n        pass\n", "TYP150@2:5", nil},
			{"decorated class", "def d(c):\n    return c\n@d\nclass C:\n    pass\n", "TYP150@4:1", nil},
			{"fields in several bases", "class A:\n    def __init__(self):\n        self.a = 1\nclass B:\n    def __init__(self):\n        self.b = 1\nclass C(A, B):\n    pass\n", "TYP150@7:1", nil},
			{"ParamSpec", "from typing import ParamSpec\nP = ParamSpec(\"P\")\n", "TYP150@2:5", nil},
			{"TypeVarTuple", "from typing import TypeVarTuple\nTs = TypeVarTuple(\"Ts\")\n", "TYP150@2:6", nil},
			{"PEP 695 ParamSpec", "def f[**P](x):\n    pass\n", "TYP150@1:7", nil},
			{"declared variance", "from typing import TypeVar\nT = TypeVar(\"T\", covariant=True)\n", "TYP150@2:18", nil},
			{"TypedDict", "from typing import TypedDict\nclass T(TypedDict):\n    a: int\n", "TYP150@2:9", nil},
			{"NewType", "from typing import NewType\nU = NewType(\"U\", int)\n", "TYP150@2:5", nil},
			{"dataclass_transform", "from typing import dataclass_transform\n", "TYP150@1:20", nil},
			{"Coroutine", "from typing import Coroutine\ndef f(x: Coroutine):\n    pass\n", "TYP150@2:10", nil},
			{"generator next", gen + "x = g()\nprint(next(x))\n", "", nil},
			{"yield in try except", "def g():\n    try:\n        yield 1\n    except ValueError:\n        pass\n", "", nil},
			{"plain TypeVar", "from typing import TypeVar\nT = TypeVar(\"T\")\ndef f(x: T) -> T:\n    return x\nf(1)\n", "", nil},
		})
	})
}

func runSubsetCases(t *testing.T, cases []subsetCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := check(t, tc.src, tc.opts...)
			if tc.want == "" {
				require.NoError(t, c.err)
				return
			}
			require.Contains(t, diagnostics(c.err), tc.want)
		})
	}
}

// fallbackSites lists the fallbacks of a check as "reason@line:col".
func fallbackSites(info *types.Info) []string {
	out := make([]string, len(info.Fallbacks))
	for i, f := range info.Fallbacks {
		out[i] = fmt.Sprintf("%s@%d:%d", f.Reason, f.Pos.Line, f.Pos.Column)
	}
	return out
}

// checkFallbacks runs one minimal program per reason of a TYP200 fallback and
// the Any annotation, which opts out of the report.
func checkFallbacks(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"param-conflict", "def f(a):\n    return a + 1\nf(1)\nf(\"s\")\nf([1])\nf(2.5)\nf(None)\nf({1})\n", "param-conflict@2:12"},
		{"cap-exceeded", "a = [1, \"a\", 2.5, None, (1,), [1], {1}]\nprint(a[0] + 1)\n", "cap-exceeded@2:7"},
		{"union", "x = 1 if input() else \"a\"\nprint(x + x)\n", "union@2:7"},
		{"union attribute", "x = 1 if input() else \"a\"\nprint(x.foo)\n", "union@2:7"},
		{"escape", "import math\ndef g(x):\n    return x + 1\nmath.nothing(g)\n", "escape@3:12"},
		{"dynamic-attr", "class C:\n    def __getattr__(self, n):\n        return 1\nprint(C().foo + 1)\n", "dynamic-attr@4:7"},
		{"unresolved-attr module", "import math\nprint(math.nothing)\n", "unresolved-attr@2:7"},
		{"unresolved-attr class", "class C:\n    pass\nprint(C().x)\n", "unresolved-attr@3:7"},
		{"decorator", "import math\n@math.nothing\ndef f(x):\n    return x\nprint(f(1))\n", "decorator@5:7"},
		{"dynamic-except", "def f(x):\n    try:\n        pass\n    except x:\n        pass\nf(1)\n", "dynamic-except@4:12"},
		{"dynamic-call starred", "def f(*a):\n    return a\nargs = (1, 2)\nprint(f(*args))\n", "dynamic-call@4:7"},
		{"dynamic-call __new__", "class C:\n    def __new__(cls):\n        return object.__new__(cls)\nC()\n", "dynamic-call@4:1"},
		{"dynamic-item set", "a = {1}\nb = {*a}\n", "dynamic-item@2:5"},
		{"dynamic-item tuple", "a = [1]\nb = (*a, 1)\n", "dynamic-item@2:5"},
		{"dynamic-item dict", "d = {**{1: 2}}\n", "dynamic-item@1:5"},
		{"any-value", "x = getattr(object(), \"a\")\nprint(x + 1)\n", "any-value@2:7"},
	}
	t.Run("reasons", func(t *testing.T) {
		seen := map[string]bool{}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				c := check(t, tc.src)
				require.NoError(t, c.err)
				require.Contains(t, fallbackSites(c.info), tc.want)
				seen[tc.want[:indexAt(tc.want)]] = true
			})
		}
		for _, tc := range cases {
			c := check(t, tc.src)
			for _, f := range c.info.Fallbacks {
				require.True(t, seen[f.Reason], "reason %s has no case", f.Reason)
			}
		}
	})
	t.Run("one fallback per site", func(t *testing.T) {
		c := check(t, cases[0].src)
		sites := map[string]bool{}
		for _, f := range c.info.Fallbacks {
			k := f.Pos.String() + "-" + f.End.String()
			require.False(t, sites[k], "two fallbacks at %s", k)
			sites[k] = true
		}
	})
	t.Run("Any annotation opts out", func(t *testing.T) {
		const body = "    return a.b + a.c(1)[0]\n"
		c := check(t, "from typing import Any\ndef f(a: Any):\n"+body+"f(1)\n")
		require.NoError(t, c.err)
		require.Empty(t, c.info.Fallbacks)
		c = check(t, "from typing import Any\nx: Any = 1\nprint(x.foo + 1)\nfor i in x:\n    print(i)\n")
		require.NoError(t, c.err)
		require.Empty(t, c.info.Fallbacks)
		c = check(t, "def f(a):\n"+body+"f(1)\n")
		require.NoError(t, c.err)
		require.NotEmpty(t, c.info.Fallbacks)
	})
	t.Run("a typed program has none", func(t *testing.T) {
		c := check(t, "def f(a: int, b: int) -> int:\n    return a + b\nprint(f(1, 2))\n")
		require.NoError(t, c.err)
		require.Empty(t, c.info.Fallbacks)
	})
}

// indexAt is the index of the "@" of a "reason@line:col" string.
func indexAt(s string) int {
	for i := range s {
		if s[i] == '@' {
			return i
		}
	}
	return len(s)
}
