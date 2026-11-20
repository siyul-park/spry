package types_test

import (
	"testing"

	"github.com/siyul-park/spry/types"
	"github.com/stretchr/testify/require"
)

func TestClass_Fields(t *testing.T) {
	t.Run("fields come from stores through the first parameter and slots", func(t *testing.T) {
		w := newWorld(t, `
class A:
    __slots__ = ("a",)
    k: int = 1
    def __init__(self, x: int):
        self.x = x
class B(A):
    def m(this):
        this.y = 1.5
`)
		names := func(n *types.Named) []string {
			var out []string
			for _, f := range n.Underlying().(*types.Class).Fields() {
				out = append(out, f.Name())
			}
			return out
		}
		require.Equal(t, []string{"a", "k", "x"}, names(w.named(t, "A")))
		require.Equal(t, []string{"y"}, names(w.named(t, "B")))
	})
}

// checkClassSubset checks the diagnostics of the class layout rules.
func checkClassSubset(t *testing.T) {
	tests := []struct {
		name string
		src  string
		code string
	}{
		{"statement in class body", "class C:\n    for i in range(3): pass\n", "TYP110"},
		{"attribute outside the layout", "class C:\n    def __init__(self): self.a = 1\nc = C()\nc.b = 2\n", "TYP110"},
		{"class attribute store", "class C:\n    k = 1\nC.k = 2\n", "TYP111"},
		{"nested class", "def f():\n    class C: ...\n    return C()\n", "TYP150"},
		{"decorated class", "def d(c): return c\n@d\nclass C: ...\n", "TYP150"},
		{"fields in two bases", "class A:\n    def __init__(self): self.a = 1\nclass B:\n    def __init__(self): self.b = 1\nclass C(A, B): ...\n", "TYP150"},
		{"inconsistent mro", "class A: ...\nclass B(A): ...\nclass C(A, B): ...\n", "TYP112"},
		{"metaclass", "class M(type): ...\nclass C(metaclass=M): ...\n", "TYP112"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := check(t, tt.src)
			var list types.ErrorList
			require.ErrorAs(t, c.err, &list)
			codes := map[string]bool{}
			for _, e := range list {
				codes[e.Code] = true
			}
			require.True(t, codes[tt.code], "%v", c.err)
		})
	}
}
