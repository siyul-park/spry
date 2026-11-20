package ssa_test

import (
	"testing"

	"github.com/siyul-park/spry/ssa"
	"github.com/stretchr/testify/require"
)

func TestKind(t *testing.T) {
	for kind, want := range map[ssa.Kind]string{
		ssa.KindI1: "i1", ssa.KindI32: "i32", ssa.KindInt: "int", ssa.KindF64: "f64",
		ssa.KindStr: "str", ssa.KindBytes: "bytes", ssa.KindNone: "none", ssa.KindAny: "any",
	} {
		require.Equal(t, want, ssa.Type{Kind: kind}.String())
	}
}

func TestType_String(t *testing.T) {
	c := &ssa.Class{Name: "C"}
	i := ssa.Type{Kind: ssa.KindInt}
	cases := []struct {
		name string
		typ  ssa.Type
		want string
	}{
		{"scalar", i, "int"},
		{"list", ssa.Type{Kind: ssa.KindList, Elems: []ssa.Type{i}}, "list<int>"},
		{"dict", ssa.Type{Kind: ssa.KindDict, Elems: []ssa.Type{{Kind: ssa.KindStr}, i}}, "dict<str,int>"},
		{"empty tuple", ssa.Type{Kind: ssa.KindTuple}, "tuple<>"},
		{"variadic tuple", ssa.Type{Kind: ssa.KindTuple, Elems: []ssa.Type{i}, Variadic: true}, "tuple<int...>"},
		{"nullable class", ssa.Type{Kind: ssa.KindClass, Class: c, Nullable: true}, "@C?"},
		{"type of class", ssa.Type{Kind: ssa.KindTypeOf, Class: c}, "type<@C>"},
		{"func", ssa.Type{Kind: ssa.KindFunc, Elems: []ssa.Type{i, {Kind: ssa.KindNone}}}, "func(int) -> none"},
		{"nullable func", ssa.Type{Kind: ssa.KindFunc, Elems: []ssa.Type{i}, Nullable: true}, "(func() -> int)?"},
	}
	for _, c := range cases {
		require.Equal(t, c.want, c.typ.String(), c.name)
	}
}
