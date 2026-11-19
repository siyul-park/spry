package types_test

import (
	"strings"
	"testing"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/types"
	"github.com/stretchr/testify/require"
)

func TestScope(t *testing.T) {
	c := mustCheck(t, "b = 1\na = 2\nb = 'x'\n")
	s := c.mod.Scope()

	t.Run("names keep declaration order", func(t *testing.T) {
		require.Equal(t, []string{"__name__", "__file__", "b", "a"}, s.Names())
	})
	t.Run("redeclaration keeps one object", func(t *testing.T) {
		require.Same(t, s.Lookup("b"), s.Lookup("b"))
		require.Equal(t, 1, strings.Count(","+strings.Join(s.Names(), ",")+",", ",b,"))
	})
	t.Run("lookup and parent", func(t *testing.T) {
		b := s.Lookup("b")
		require.Same(t, b, s.Lookup("b"))
		require.Nil(t, s.Lookup("zz"))
		require.Same(t, s, b.Parent())
		require.Same(t, types.Universe, s.Parent())
		require.Equal(t, types.ModuleScope, s.Kind())
		require.Equal(t, types.UniverseScope, types.Universe.Kind())
		require.Nil(t, types.Universe.Parent())
	})
}

func TestScope_LookupParent(t *testing.T) {
	c := mustCheck(t, `
g = 1
shadowed = 0
class C:
    attr = 1
    shadowed = 2
    def method(self):
        return [i for i in self]
    def generic[T](self, x: T) -> T:
        return x
`)
	mod := c.mod.Scope()
	class := c.scope(t, "C")
	method := c.scope(t, "method")
	comp := c.scope(t, "listcomp")
	fnInTparams := c.scope(t, "generic")
	tparams := fnInTparams.Parent()
	generic := c.node(t, "generic").(*ast.FunctionDef)
	annotation := c.info.Scopes[generic.Args]
	require.Equal(t, types.TypeParamScope, tparams.Kind())
	require.NotNil(t, annotation)

	attr, shadow := class.Lookup("attr"), class.Lookup("shadowed")
	tests := []struct {
		name  string
		from  *types.Scope
		attr  string
		scope *types.Scope
		obj   types.Object
	}{
		{"own scope", class, "attr", class, attr},
		{"class sees its own shadow", class, "shadowed", class, shadow},
		{"method skips the class scope", method, "attr", nil, nil},
		{"method skips the class shadow", method, "shadowed", mod, mod.Lookup("shadowed")},
		{"method sees module", method, "g", mod, mod.Lookup("g")},
		{"comprehension skips the class scope", comp, "attr", nil, nil},
		{"type parameter scope sees its class", tparams, "attr", class, attr},
		{"annotation scope sees the class through type parameters", annotation, "attr", class, attr},
		{"function in type parameters skips the class", fnInTparams, "attr", nil, nil},
		{"missing name", method, "nope", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope, obj := tt.from.LookupParent(tt.attr)
			require.Same(t, tt.scope, scope)
			require.Equal(t, tt.obj, obj)
		})
	}
}
