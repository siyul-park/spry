package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/scanner"
	"github.com/siyul-park/spry/types"
)

// storageOf describes how scope s stores name from what the scope exposes: a
// "local" or "cell" object of its own, a "free" cell of an enclosing function,
// or otherwise a "global" (explicit or implicit).
func storageOf(s *types.Scope, name string) string {
	if obj := s.Lookup(name); obj != nil {
		if v, ok := obj.(*types.Var); ok && v.IsCell() {
			return "cell"
		}
		return "local"
	}
	if scope, obj := s.LookupParent(name); scope != nil && scope.Kind() == types.FunctionScope {
		if v, ok := obj.(*types.Var); ok && v.IsCell() {
			return "free"
		}
	}
	return "global"
}

func checkSymtable(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		scope string
		want  map[string]string
	}{
		{"module", "x = 1\ndef f(): pass\n", "", map[string]string{"x": "local", "f": "local"}},
		{"parameter and local", "def f(a):\n    b = a\n", "f", map[string]string{"a": "local", "b": "local"}},
		{"global", "def f():\n    return len(x)\n", "f", map[string]string{"len": "global", "x": "global"}},
		{"explicit global", "def f():\n    global g\n    g = 1\n", "f", map[string]string{"g": "global"}},
		{"cell and free", "def outer():\n    x = 1\n    def inner():\n        return x\n", "outer", map[string]string{"x": "cell"}},
		{"free in the inner function", "def outer():\n    x = 1\n    def inner():\n        return x\n", "inner", map[string]string{"x": "free"}},
		{"nonlocal", "def outer():\n    x = 1\n    def inner():\n        nonlocal x\n        x = 2\n", "inner", map[string]string{"x": "free"}},
		{"class scope is invisible to methods", "def f():\n    x = 1\n    class C:\n        x = 2\n        def m(self):\n            return x\n", "m", map[string]string{"x": "free"}},
		{"class names stay local", "def f():\n    x = 1\n    class C:\n        x = 2\n        def m(self):\n            return x\n", "C", map[string]string{"x": "local"}},
		{"class name seen by a method is global", "class C:\n    x = 2\n    def m(self):\n        return x\n", "m", map[string]string{"x": "global"}},
		{"comprehension reads the function", "def f(y):\n    return [y + i for i in range(3)]\n", "listcomp", map[string]string{"y": "free", "i": "local"}},
		{"function parameter read by a comprehension is a cell", "def f(y):\n    return [y + i for i in range(3)]\n", "f", map[string]string{"y": "cell"}},
		{"walrus binds the function", "def f():\n    [(w := i) for i in range(3)]\n    return w\n", "f", map[string]string{"w": "cell"}},
		{"walrus in the comprehension", "def f():\n    [(w := i) for i in range(3)]\n    return w\n", "listcomp", map[string]string{"w": "free", "i": "local"}},
		{"walrus at module level is global", "[(w := i) for i in range(3)]\n", "listcomp", map[string]string{"w": "global"}},
		{"mangled name", "class A:\n    __x = 1\n    def f(self):\n        return self.__x\n", "A", map[string]string{"_A__x": "local"}},
		{"lambda", "def f(a):\n    return lambda: a\n", "lambda", map[string]string{"a": "free"}},
		{"type parameter is bound in its scope", "def f[T](x: T) -> T:\n    return x\n", "f", map[string]string{"x": "local"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := mustCheck(t, tt.src)
			s := c.mod.Scope()
			if tt.scope != "" {
				s = c.scope(t, tt.scope)
			}
			for name, want := range tt.want {
				require.Equal(t, want, storageOf(s, name), name)
			}
		})
	}

	t.Run("scope kinds", func(t *testing.T) {
		c := mustCheck(t, "class C:\n    def m(self):\n        return [i for i in self]\n")
		require.Equal(t, types.ModuleScope, c.mod.Scope().Kind())
		cls := c.scope(t, "C")
		require.Equal(t, types.ClassScope, cls.Kind())
		require.Same(t, c.mod.Scope(), cls.Parent())
		method := c.scope(t, "m")
		require.Equal(t, types.FunctionScope, method.Kind())
		require.Equal(t, types.ComprehensionScope, c.scope(t, "listcomp").Kind())
		require.Equal(t, types.UniverseScope, c.mod.Scope().Parent().Kind())
	})

	t.Run("type parameter scope", func(t *testing.T) {
		c := mustCheck(t, "def f[T](x: T) -> T:\n    return x\n")
		fn := c.scope(t, "f")
		require.Equal(t, types.TypeParamScope, fn.Parent().Kind())
		require.NotNil(t, fn.Parent().Lookup("T"))
		_, obj := fn.LookupParent("T")
		require.NotNil(t, obj)
	})

	t.Run("__class__ cell is implicit", func(t *testing.T) {
		for name, src := range map[string]string{
			"super":     "class A:\n    def f(self):\n        return super().f()\n",
			"__class__": "class A:\n    def f(self):\n        return __class__\n",
		} {
			c := mustCheck(t, src)
			v, ok := c.info.Implicits[c.node(t, "A")].(*types.Var)
			require.True(t, ok, name)
			require.True(t, v.IsCell(), name)
			require.Equal(t, "__class__", v.Name(), name)
		}
	})

	t.Run("cell variables are marked", func(t *testing.T) {
		c := mustCheck(t, "def outer():\n    x = 1\n    y = 2\n    def inner():\n        return x\n")
		fn := c.scope(t, "outer")
		require.True(t, fn.Lookup("x").(*types.Var).IsCell())
		require.False(t, fn.Lookup("y").(*types.Var).IsCell())
	})

	t.Run("definitions", func(t *testing.T) {
		c := mustCheck(t, "x = 1\nx = 2\ndef f(a): pass\nclass C: pass\n")
		require.IsType(t, &types.Var{}, c.mod.Scope().Lookup("x"))
		require.IsType(t, &types.Func{}, c.mod.Scope().Lookup("f"))
		require.IsType(t, &types.TypeName{}, c.mod.Scope().Lookup("C"))
		require.Equal(t, "/app/main.py", c.mod.Path())
		require.Equal(t, "__main__", c.mod.Name())
		for _, name := range []string{"__name__", "__file__"} {
			require.Equal(t, "str", c.mod.Scope().Lookup(name).Type().String())
		}
	})
}

func checkSyntaxErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		msg  string
		line int
	}{
		{"return outside function", "class C:\n    return 1\n", "'return' outside function", 2},
		{"duplicate argument", "def f(a, a): pass\n", "duplicate argument 'a' in function definition", 1},
		{"nonlocal at module level", "nonlocal x\n", "nonlocal declaration not allowed at module level", 1},
		{"no binding", "def f():\n    nonlocal x\n", "no binding for nonlocal 'x' found", 2},
		{"global after use", "def f():\n    print(x)\n    global x\n", "name 'x' is used prior to global declaration", 3},
		{"break outside loop", "def f():\n    break\n", "'break' outside loop", 2},
		{"yield outside function", "yield 1\n", "'yield' outside function", 1},
		{"await outside async function", "def f():\n    await x\n", "'await' outside async function", 2},
		{"yield in comprehension", "def f():\n    return [(yield i) for i in x]\n", "'yield' inside list comprehension", 2},
		{"rebinding the iteration variable", "[(i := 0) for i in range(3)]\n", "assignment expression cannot rebind comprehension iteration variable 'i'", 1},
		{"__debug__", "__debug__ = 1\n", "cannot assign to __debug__", 1},
		{"star import in a function", "def f():\n    from m import *\n", "import * only allowed at module level", 2},
		{"future after code", "x = 1\nfrom __future__ import annotations\n", "from __future__ imports must occur at the beginning of the file", 2},
		{"repeated keyword", "f(a=1, a=2)\n", "keyword argument repeated: a", 1},
		{"starred target", "*a = [1]\n", "starred assignment target must be in a list or tuple", 1},
		{"unreachable pattern", "match x:\n    case a:\n        pass\n    case 1:\n        pass\n", "name capture 'a' makes remaining patterns unreachable", 2},
		{"annotation yield", "def f(a: (yield)): pass\n", "yield expression cannot be used within an annotation", 1},
		{"return in except star", "def f():\n    try:\n        pass\n    except* E:\n        return 1\n", "'break', 'continue' and 'return' cannot appear in an except* block", 5},
		{"async comprehension in a sync function", "def f():\n    return [x async for x in y]\n", "asynchronous comprehension outside of an asynchronous function", 2},
		{"return with value in async generator", "async def f():\n    yield 1\n    return 2\n", "'return' with value in async generator", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := check(t, tt.src)
			e := syntaxError(t, c.err)
			require.Equal(t, tt.msg, e.Msg)
			require.Equal(t, tt.line, e.Pos.Line)
			require.ErrorIs(t, c.err, scanner.ErrSyntax)
		})
	}

	t.Run("symbol table errors come before code generator errors", func(t *testing.T) {
		c := check(t, "return 1\ndef f(a, a): pass\n")
		require.Equal(t, "duplicate argument 'a' in function definition", syntaxError(t, c.err).Msg)
	})
	t.Run("one error as CPython raises", func(t *testing.T) {
		c := check(t, "return 1\nbreak\n")
		require.Len(t, c.err, 1)
	})
	t.Run("valid syntax that the checker rejects", func(t *testing.T) {
		c := check(t, "async def f():\n    return [x async for x in y]\n")
		require.ErrorIs(t, c.err, types.ErrUnsupported)
		require.NotErrorIs(t, c.err, scanner.ErrSyntax)
	})
}
