package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/types"
)

// checkRejections checks the TYP codes of constructs the subset rejects.
func checkRejections(t *testing.T) {
	tests := []struct {
		name string
		src  string
		code string
	}{
		{"generator throw", "def g():\n    yield 1\nx = g()\nx.throw(ValueError())\n", "TYP150"},
		{"generator close", "def g():\n    yield 1\nx = g()\nx.close()\n", "TYP150"},
		{"yield in try finally", "def g():\n    try:\n        yield 1\n    finally:\n        pass\n", "TYP150"},
		{"yield in with", "def g(m):\n    with m:\n        yield 1\n", "TYP150"},
		{"await", "async def f():\n    pass\n", "TYP150"},
		{"except star", "try:\n    pass\nexcept* ValueError:\n    pass\n", "TYP150"},
		{"complex literal", "x = 1j\n", "TYP150"},
		{"t-string", "x = t'a'\n", "TYP150"},
		{"del attribute", "class C:\n    def __init__(self): self.a = 1\nc = C()\ndel c.a\n", "TYP110"},
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

// checkRedefinedName checks that a name bound by def and again by assignment
// does not panic the checker.
func checkRedefinedName(t *testing.T) {
	for _, src := range []string{
		"def A():\n    pass\nA: int\n",
		"def A():\n    pass\nA = 0\n",
		"@d\ndef A():\n    pass\nA = 0\n",
	} {
		require.NotPanics(t, func() { check(t, src) }, "%q", src)
	}
}

// checkClassInheritingItself checks that a class that is its own base is
// TYP112.
func checkClassInheritingItself(t *testing.T) {
	c := check(t, "class S(S):\n    pass\n")

	var list types.ErrorList
	require.ErrorAs(t, c.err, &list)
	require.Equal(t, "TYP112", list[0].Code)
}

// checkSelfContainingList checks that a list holding itself terminates.
func checkSelfContainingList(t *testing.T) {
	require.NotPanics(t, func() {
		check(t, "xs = [3, 1]\nxs.append(xs)\nsorted(xs, reverse=True)\n")
	})
}
