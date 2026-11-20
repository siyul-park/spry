package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/types"
)

func TestJoin(t *testing.T) {
	w := newWorld(t, "")
	list := func(e types.Type) types.Type { return w.list(t, e) }
	u := types.NewUnion

	tests := []struct {
		name string
		x, y types.Type
		want types.Type
	}{
		{"identical", w.integer, w.integer, w.integer},
		{"unrelated classes", w.integer, w.str, u(w.integer, w.str)},
		{"unrelated classes keep order", w.str, w.integer, u(w.str, w.integer)},
		{"subclass is absorbed", w.dog, w.animal, w.animal},
		{"base absorbs subclass", w.animal, w.dog, w.animal},
		{"siblings", w.dog, w.cat, u(w.dog, w.cat)},
		{"bool stays observable next to int", w.boolean, w.integer, u(w.boolean, w.integer)},
		{"int stays observable next to bool", w.integer, w.boolean, u(w.integer, w.boolean)},
		{"int stays observable next to float", w.integer, w.float, u(w.integer, w.float)},
		{"literal is absorbed by its class", intLit(1), w.integer, w.integer},
		{"literals", intLit(1), intLit(2), u(intLit(1), intLit(2))},
		{"Any absorbs", w.integer, w.anyT, w.anyT},
		{"Any inside a union absorbs", u(w.integer, w.anyT), w.str, w.anyT},
		{"Never is the identity", types.Typ[types.Never], w.integer, w.integer},
		{"None", w.integer, w.none, u(w.integer, w.none)},
		{"union with member", u(w.integer, w.str), w.integer, u(w.integer, w.str)},
		{"union with union", u(w.integer, w.str), u(w.str, w.none), u(w.integer, w.str, w.none)},
		{"union absorbs subclass", u(w.integer, w.animal), w.dog, u(w.integer, w.animal)},
		{"tuples of different lengths", elems(-1, w.integer, w.integer), elems(-1, w.integer), elems(0, w.integer)},
		{"tuples of different element types and lengths", elems(-1, w.integer), elems(-1, w.str, w.str), elems(0, u(w.integer, w.str))},
		{"tuple absorbed by unbounded", elems(-1, w.integer), elems(0, w.integer), elems(0, w.integer)},
		{"tuples of one length", elems(-1, w.integer), elems(-1, w.str), u(elems(-1, w.integer), elems(-1, w.str))},
		{"instances with different arguments", list(w.integer), list(w.str), u(list(w.integer), list(w.str))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := types.Join(tt.x, tt.y)
			require.Equal(t, tt.want.String(), got.String())
		})
	}
}
