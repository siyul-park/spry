package ssa

import (
	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/types"
)

// eqMethods are the methods of the containers that compare or hash their items.
var eqMethods = map[string]bool{
	"get": true, "pop": true, "setdefault": true, "add": true, "remove": true,
	"discard": true, "index": true, "count": true, "update": true,
}

// userEq reports whether the operation on a container of type t has to be
// dispatched at run time, and notes the fallback.
func (f *fbuild) userEq(t Type) bool {
	if t.Nullable || !f.b.holdsUserEq(t) {
		return false
	}
	if f.origin != nil && f.b.fset != nil {
		f.b.note(f.b.fset.Position(f.origin.Pos()), f.b.fset.Position(f.origin.End()), "user-eq", "the items or keys define __eq__ or __hash__")
	}
	return true
}

// holdsUserEq reports whether the items or keys of a container of type t are
// instances of a class of the program that defines __eq__ or __hash__. The
// host compares and hashes by identity, so operations on such a container are
// dispatched at run time, where the methods of the program can run.
func (b *builder) holdsUserEq(t Type) bool {
	switch t.Kind {
	case KindList, KindSet:
		return b.definesEq(t.Elems[0])
	case KindDict:
		return b.definesEq(t.Elems[0])
	case KindTuple:
		for _, e := range t.Elems {
			if b.definesEq(e) {
				return true
			}
		}
	}
	return false
}

// definesEq reports whether the instances of t are compared by a method of
// the program: t is a class that has, or has a subclass that has, one.
func (b *builder) definesEq(t Type) bool {
	switch t.Kind {
	case KindClass:
		for _, k := range b.prog.Classes {
			n := b.origins[k]
			if n == nil || !k.derives(t.Class) {
				continue
			}
			for _, name := range []string{"__eq__", "__hash__"} {
				if sel := types.LookupAttr(n, name); sel != nil {
					if f, ok := sel.Obj().(*types.Func); ok && f.Module() != nil && f.Module().Init() != nil {
						return true
					}
				}
			}
		}
	case KindTuple:
		return b.holdsUserEq(t)
	}
	return false
}

// dynItems builds a container of type want from entries one at a time with
// the method add, which the runtime dispatches to the methods of the items.
func (f *fbuild) dynItems(op Op, want Type, method string, entries [][]Value) Value {
	acc := f.val(op, want)
	for _, e := range entries {
		f.dynMethod(acc, method, e...)
	}
	return acc
}

// dynContainerCall calls a method of a container whose items define __eq__ or
// __hash__ at run time.
func (f *fbuild) dynContainerCall(e *ast.Call, attr *ast.Attribute, name string) Value {
	recv := f.expr(attr.Value)
	args := make([]Value, len(e.Args))
	for i, a := range e.Args {
		args[i] = f.expr(a)
	}
	return f.dynMethod(recv, name, args...)
}
