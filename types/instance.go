package types

import (
	"fmt"

	"github.com/siyul-park/spry/ast"
)

// instanceAttribute infers the read of an attribute of an instance of a class
// of the program: a field or class variable, or the getter of a property.
// It reports false for any other receiver.
func (s *spec) instanceAttribute(e *ast.Attribute, recv Type) (Type, bool) {
	ci := s.receiver(recv)
	if ci == nil || isMeta(recv) && ci.bad {
		return nil, false
	}
	if ci.bad {
		return s.anyValue(e), true
	}
	if ci.dynamic {
		return s.opaque(e, reasonDynamicAttr, "the class computes its attributes"), true
	}
	sel := LookupAttr(Default(recv), e.Attr)
	if sel == nil {
		return s.opaque(e, reasonUnresolvedAttr, "the class has no attribute "+e.Attr), true
	}
	switch v := sel.obj.(type) {
	case *Var:
		if sel.kind != FieldVal && sel.kind != ClassVal || s.c.srcs[v.module] == nil {
			return nil, false
		}
		s.c.info.Selections[e] = sel
		return s.c.load(s, v), true
	case *Func:
		if sel.kind != DescriptorVal {
			return nil, false
		}
		s.c.info.Selections[e] = sel
		res, call, ok := s.callMethod(methodCall{site: e, recv: Default(recv), name: e.Attr})
		if !ok {
			return s.opaque(e, reasonDynamicAttr, "a property that cannot be resolved"), true
		}
		s.calls[e] = call
		return res, true
	}
	return nil, false
}

// storeAttribute infers target = value for an attribute target: a store to a
// field, or a call of the setter of a property.
func (s *spec) storeAttribute(target *ast.Attribute, t Type, v *env) {
	recv := withoutNone(s.expr(target.Value, v))
	s.record(target, t)
	if target.Attr == "__class__" {
		return
	}
	if _, ok := recv.(*Module); ok {
		s.fail(target, "TYP111", "assigning to an attribute of a module invalidates devirtualization")
		return
	}
	ci := s.receiver(recv)
	switch {
	case isAny(recv):
		if !optedOut([]Type{recv}) {
			s.fall(target, s.anyReason(target, recv), "an attribute store on an Any value")
		}
		return
	case ci == nil || ci.bad || ci.dynamic:
		if ci == nil || ci.dynamic {
			s.fall(target, reasonDynamicAttr, "an attribute store on a value the layout does not cover")
		}
		return
	case isMeta(recv):
		s.fail(target, "TYP111", "assigning to an attribute of a class invalidates devirtualization")
		return
	}
	sel := LookupAttr(Default(recv), target.Attr)
	if sel == nil {
		s.fail(target, "TYP110", fmt.Sprintf("attribute %s is not in the layout of the class", target.Attr))
		return
	}
	s.c.info.Selections[target] = sel
	switch obj := sel.obj.(type) {
	case *Var:
		s.storeField(target, obj, sel, t)
	case *Func:
		site := Site{target, SiteStore, 0}
		_, call, ok := s.callMethod(methodCall{site: site, recv: Default(recv), name: target.Attr, accessor: "setter", args: []argv{{t: t, e: target}}})
		if !ok {
			s.fall(target, reasonDynamicAttr, "this property has no setter")
			return
		}
		s.calls[site] = call
	}
}

// withoutNone is t without None if what is left is one type: the receiver of
// an attribute of C | None, whose access raises on None.
func withoutNone(t Type) Type {
	u, ok := Unalias(t).(*Union)
	if !ok {
		return t
	}
	rest := filterTerms(u, func(x Type) bool { return !isNoneType(x) })
	if len(unionTerms(rest)) == 1 && len(u.terms) > 1 {
		return rest
	}
	return t
}

func (s *spec) storeField(target *ast.Attribute, obj *Var, sel *Selection, t Type) {
	if sel.kind != FieldVal {
		s.fail(target, "TYP111", "assigning to a class variable through an instance is not supported")
		return
	}
	if obj.annot && !AssignableTo(t, obj.typ) {
		s.fail(target, "TYP140", fmt.Sprintf("value of type %s is not assignable to %s", TypeString(Default(t), nil), TypeString(obj.typ, nil)))
		return
	}
	s.c.store(obj, t)
}

// constructUser infers a call of a class of the program: the __init__ of the
// class, found in its MRO, on a new instance. It reports false for a class of
// the standard library.
func (s *spec) constructUser(e *ast.Call, m *Meta, args []argv) (Type, bool) {
	ci := s.c.instanceOf(m.elem)
	if ci == nil {
		return nil, false
	}
	if ci.bad {
		return s.anyValue(e), true
	}
	inst := ci.named
	if f, _ := s.c.definer(inst, "__new__", nil); f != nil && s.c.funcOf(f) != nil {
		return s.opaque(e, reasonDynamicCall, "a class with __new__"), true
	}
	res, call, ok := s.callMethod(methodCall{site: e, recv: inst, name: "__init__", args: args, exact: true})
	if !ok {
		return s.opaque(e, reasonDynamicCall, "a call that no __init__ accepts"), true
	}
	if call.Kind == Dynamic {
		return anyFor(reasonDynamicCall), true
	}
	_ = res
	call.Slot = "__init__"
	s.calls[e] = call
	if ci.dynamic {
		return anyFor(reasonDynamicAttr), true
	}
	return inst, true
}

// classDef analyzes a class statement: the class body runs once, where the
// class is defined.
func (s *spec) classDef(st *ast.ClassDef, v *env) {
	obj, _ := s.c.info.Defs[st].(*TypeName)
	var ci *classInfo
	if obj != nil {
		if n, ok := obj.typ.(*Named); ok {
			ci = s.c.classes[n.origin]
		}
	}
	if ci == nil || !s.isInit() {
		s.clobber(st, v)
		return
	}
	if !ci.bad {
		s.classBody(ci, st, v)
	}
	v.bind(obj, NewMeta(ci.named))
}

// classBody infers the statements of a class body: methods, class variables,
// pass and the docstring.
func (s *spec) classBody(ci *classInfo, st *ast.ClassDef, v *env) {
	scope := ci.named.class().members
	s.comps = append(s.comps, scope)
	defer func() { s.comps = s.comps[:len(s.comps)-1] }()
	for _, b := range st.Body {
		switch b := b.(type) {
		case *ast.FunctionDef:
			if f, ok := s.c.info.Defs[b].(*Func); ok {
				if fn := s.c.funcOf(f); fn != nil {
					s.defaults(fn, b.Args, v)
				}
			}
		case *ast.Assign:
			t := s.expr(b.Value, v)
			for _, target := range b.Targets {
				s.classVar(target, t, v)
			}
		case *ast.AnnAssign:
			if b.Value != nil {
				s.classVar(b.Target, s.expr(b.Value, v), v)
			}
		}
	}
}

// classVar binds a name of a class body.
func (s *spec) classVar(target ast.Expr, t Type, v *env) {
	n, ok := target.(*ast.Name)
	if !ok {
		return
	}
	s.record(n, t)
	if obj, ok := s.target(n).(*Var); ok {
		s.c.store(obj, t)
		v.bind(obj, Default(t))
	}
}

// selfBound is t with the Self of the class of the method being analyzed
// replaced by the class: a method is analyzed for the class that defines it.
func (s *spec) selfBound(t Type) Type {
	owner := s.c.ownerOf(s.fn.obj)
	if owner == nil {
		return t
	}
	return subst(t, substMap{owner.named.class().self: owner.named})
}

func isEllipsis(st ast.Stmt) bool {
	e, ok := st.(*ast.ExprStmt)
	if !ok {
		return false
	}
	c, ok := e.Value.(*ast.Constant)
	if !ok {
		return false
	}
	_, ok = c.Value.(ast.Ellipsis)
	return ok
}
