package types

import (
	"fmt"

	"github.com/siyul-park/spry/ast"
)

// callee is what the function expression of a call denotes.
type callee struct {
	f    *Func
	form *Builtin
	meta *Meta
	sel  *Selection
	typ  Type
	// recv, exact and super describe the receiver of a method of a class of
	// the program.
	recv  Type
	exact bool
	super *classInfo
}

func (s *spec) call(e *ast.Call, v *env) Type {
	cal := s.callee(e.Func, v)
	s.markInline(e, cal)
	args, ok := s.arguments(e, v)
	switch {
	case isNever(cal.typ) && cal.f == nil && cal.form == nil && cal.meta == nil:
		return Typ[Never]
	case !ok:
		return s.opaque(e, reasonDynamicCall, "a starred argument")
	}
	for _, a := range args {
		if isNever(a.t) {
			return Typ[Never]
		}
	}
	return s.dispatchCall(e, cal, args, v)
}

// dispatchCall infers the call e of a callee with evaluated arguments.
func (s *spec) dispatchCall(e *ast.Call, cal callee, args []argv, v *env) Type {
	switch {
	case cal.form != nil:
		return s.callForm(e, cal.form, args)
	case cal.f != nil:
		return s.callFunc(e, cal, args, v)
	case cal.meta != nil:
		return s.construct(e, cal.meta, args)
	}
	if t, ok := s.callValue(e, cal.typ, args); ok {
		return t
	}
	if isAny(cal.typ) {
		for _, a := range args {
			s.escapeClosures(a.t, nil)
		}
		return s.dynamic(e, "__call__", "the callee is Any", cal.typ)
	}
	return s.dynamicReason(e, "__call__", reasonDynamicCall, "the callee is not a known function", Typ[Any])
}

// arguments evaluates the arguments of a call. It reports false for a
// starred argument, which is not inferred yet.
func (s *spec) arguments(e *ast.Call, v *env) ([]argv, bool) {
	var out []argv
	ok := true
	for _, a := range e.Args {
		if _, star := a.(*ast.Starred); star {
			ok = false
			continue
		}
		out = append(out, argv{e: a, t: s.expr(a, v)})
	}
	for _, k := range e.Keywords {
		if k.Arg == nil {
			ok = false
			continue
		}
		out = append(out, argv{e: k.Value, t: s.expr(k.Value, v), name: *k.Arg})
	}
	return out, ok
}

func (s *spec) callee(e ast.Expr, v *env) callee {
	switch e := e.(type) {
	case *ast.Name:
		obj, free := s.resolve(e.ID)
		switch {
		case free:
			return callee{typ: s.record(e, s.freeValue(e))}
		case obj == nil:
			s.names[e] = Undefined
			s.record(e, Typ[Never])
			return callee{typ: Typ[Never]}
		}
		s.c.info.Uses[e] = obj
		d := s.definedness(e, obj, v)
		return s.calleeOf(e, obj, v, d)
	case *ast.Attribute:
		return s.attributeCallee(e, v)
	}
	return callee{typ: s.expr(e, v)}
}

// attributeCallee resolves the function expression x.name of a call: a member
// of a module, a method bound to x, or a value.
func (s *spec) attributeCallee(e *ast.Attribute, v *env) callee {
	if sup, ok := s.superCall(e.Value); ok {
		if sel := LookupAttr(sup.named, e.Attr); sel != nil && sel.kind == MethodVal {
			if f := funcOf(sel); f != nil {
				s.record(e.Value, sup.named)
				s.c.info.Selections[e] = sel
				s.record(e, sel.typ)
				return callee{f: f, sel: sel, recv: sup.named, super: sup}
			}
		}
		s.record(e.Value, Typ[Any])
		return callee{typ: s.opaque(e, reasonDynamicCall, "super() without a static MRO")}
	}
	recv := s.expr(e.Value, v)
	if m, ok := recv.(*Module); ok {
		if obj := s.c.member(m, e.Attr); obj != nil {
			s.c.info.Uses[e] = obj
			return s.calleeOf(e, obj, v, Defined)
		}
	}
	orig := recv
	recv = withoutNone(recv)
	if sel := LookupAttr(Default(recv), e.Attr); sel != nil && !isAny(recv) {
		if f := funcOf(sel); f != nil && (sel.kind == MethodVal || s.isStatic(sel, f) || sel.kind == MethodExpr && s.c.funcOf(f) != nil) {
			s.c.info.Selections[e] = sel
			s.record(e, sel.typ)
			bound := Default(recv)
			if sel.kind == MethodExpr {
				bound = nil
			}
			return callee{f: f, sel: sel, recv: bound, exact: sel.kind != MethodVal || f.deco == classFunc}
		}
	}
	return callee{typ: s.attribute(e, orig)}
}

// isStatic reports whether sel is a static method of a class of the program.
func (s *spec) isStatic(sel *Selection, f *Func) bool {
	return sel.kind == ClassVal && f.deco == staticFunc && s.c.funcOf(f) != nil
}

func (s *spec) calleeOf(e ast.Expr, obj Object, v *env, d Definedness) callee {
	switch o := obj.(type) {
	case *Func:
		if isNestedFunc(o) || hasDecorators(o) {
			return callee{typ: s.record(e, s.objectValue(obj, v, d))}
		}
		s.record(e, o.Type())
		return callee{f: o}
	case *Builtin:
		s.record(e, o.Type())
		return callee{form: o}
	}
	t := s.record(e, s.objectValue(obj, v, d))
	if m, ok := t.(*Meta); ok {
		return callee{meta: m, typ: t}
	}
	return callee{typ: t}
}

func (s *spec) callFunc(e *ast.Call, cal callee, args []argv, v *env) Type {
	fn := s.c.funcOf(cal.f)
	switch {
	case fn == nil:
		return s.callExtern(e, cal, args)
	case fn.opaque:
		return s.opaque(e, reasonDynamicCall, "a call of a closure, decorated function or generator")
	case cal.recv != nil:
		m := methodCall{site: e, recv: cal.recv, name: cal.f.name, args: args, exact: cal.exact, super: cal.super}
		if res, call, ok := s.callMethod(m); ok {
			s.calls[e] = call
			return res
		}
		return s.opaque(e, reasonDynamicCall, "a method that has no static target")
	}
	return s.callUser(e, fn, args)
}

func (s *spec) callExtern(e *ast.Call, cal callee, args []argv) Type {
	if cal.sel != nil && (cal.f.name == "throw" || cal.f.name == "close") {
		if owner := cal.sel.Owner(); owner != nil && owner.Obj().Name() == "Generator" {
			s.fail(e, "TYP150", "throwing into a generator or closing it is not supported")
		}
	}
	if p := s.preludeOf(cal, args); p != nil {
		if fn := s.c.funcOf(p); fn != nil {
		}
		if fn := s.c.funcOf(p); fn != nil && !fn.opaque {
			return s.callUser(e, fn, args)
		}
	}
	if cal.sel == nil && cal.f.module != nil && cal.f.module.name == "builtins" {
		if res, ok := s.userArgs(e, cal.f.name, args); ok {
			return res
		}
	}
	typ := cal.f.Type()
	if cal.sel != nil {
		typ = cal.sel.typ
	}
	res, idx, ok := matchType(typ, args)
	if !ok {
		return s.dynamicReason(e, cal.f.name, s.anyReason(e), "no signature of "+cal.f.name+" accepts the arguments", Typ[Any])
	}
	s.flowArgs(typ, idx, args)
	s.calls[e] = &Call{Kind: Static, Func: cal.f, Overload: idx, Slot: cal.f.name}
	return s.fromLib(e, res)
}

func (s *spec) callUser(e *ast.Call, fn *fn, args []argv) Type {
	t, res, msg := s.bindSpec(e, e, fn, args, false)
	if msg != "" {
		return s.dynamicReason(e, fn.obj.name, reasonDynamicCall, msg, Typ[Any])
	}
	s.calls[e] = &Call{Kind: Static, Target: t.Spec, Func: fn.obj, Overload: -1, Slot: fn.obj.name}
	return res
}

// checkArgs reports an argument that does not fit the annotation of its
// parameter.
func (s *spec) checkArgs(fn *fn, b *matched, m substMap) {
	for _, p := range fn.sig.params.slice() {
		t, ok := b.types[p]
		if !ok || p.paramKind > KeywordOnly {
			continue
		}
		if tp, isTP := p.typ.(*TypeParam); isTP && tp.implicit {
			continue
		}
		want := subst(p.typ, m)
		if tp, ok := want.(*TypeParam); ok && tp.isSelf && tp.bound != nil {
			want = tp.bound
		}
		if acceptsCall(want) {
			s.escapeClosures(t, want)
		} else if len(appendTypeParams(nil, want)) == 0 && !AssignableTo(t, want) && b.exprs[p] != nil {
			s.fail(b.exprs[p], "TYP140", fmt.Sprintf("argument of type %s is not assignable to parameter %s of type %s", TypeString(Default(t), nil), p.name, TypeString(want, nil)))
		}
	}
}

func (s *spec) callForm(e *ast.Call, b *Builtin, args []argv) Type {
	if b.form != formIsinstance || len(args) != 2 {
		return s.opaque(e, reasonDynamicCall, "the form "+b.name)
	}
	s.calls[e] = &Call{Kind: Static, Overload: -1, Slot: "isinstance"}
	return builtin("bool")
}

// construct infers a call of a class of the standard library through its
// __new__ or __init__.
func (s *spec) construct(e *ast.Call, m *Meta, args []argv) Type {
	if tup, ok := Unalias(m.elem).(*Tuple); ok && tup.unbounded == 0 {
		if t, ok := s.constructSeq(e, builtin("tuple"), "tuple", args); ok {
			return t
		}
	}
	if t, ok := s.constructUser(e, m, args); ok {
		return t
	}
	elem, ok := Unalias(m.elem).(*Named)
	if !ok || s.c.srcs[elem.Obj().Module()] != nil {
		return s.opaque(e, reasonDynamicCall, "a call of a class that has no static constructor")
	}
	elem = bareClass(elem)
	if name := elem.Obj().Name(); name == "str" || name == "bool" {
		s.userArgs(e, name, args)
	}
	if t, ok := s.constructBuiltin(e, elem, args); ok {
		return t
	}
	typ, slot := Type(nil), "__new__"
	if sel := LookupAttr(m, "__new__"); sel != nil && sel.kind == MethodExpr {
		typ = dropFirst(sel.typ)
		defer func() { s.noteConstructor(e, sel) }()
	} else if c := genericCtor(elem); c != nil {
		typ, slot = c, "__init__"
		defer func() { s.noteConstructor(e, LookupAttr(initSelf(elem), "__init__")) }()
	} else if c := constructor(m); c != nil {
		typ, slot = c, "__init__"
		if sel := LookupAttr(elem, "__init__"); sel != nil {
			defer func() { s.noteConstructor(e, sel) }()
		}
	}
	if typ == nil {
		return s.opaque(e, reasonDynamicCall, "a call of a class that has no static constructor")
	}
	res, idx, ok := matchType(typ, args)
	if !ok {
		return s.dynamicReason(e, slot, s.anyReason(e), "no signature accepts the arguments", Typ[Any])
	}
	s.calls[e] = &Call{Kind: Static, Overload: idx, Slot: slot}
	return s.fromLib(e, res)
}

// genericCtor is the constructor of a generic class as a function whose type
// parameters include those of the class, so that a call infers them. It is nil
// for a class that is not generic or has no __init__.
func genericCtor(origin *Named) Type {
	if origin.targs.Len() > 0 || origin.tparams.Len() == 0 {
		return nil
	}
	self := initSelf(origin)
	init := LookupAttr(self, "__init__")
	if init == nil || init.kind != MethodVal {
		return nil
	}
	wrap := func(sig *Signature) *Signature {
		all := append(append([]*TypeParam{}, origin.tparams.slice()...), sig.tparams.slice()...)
		return &Signature{tparams: newTypeParamList(all), params: sig.params, result: self}
	}
	switch t := init.typ.(type) {
	case *Signature:
		return wrap(t)
	case *Overload:
		sigs := make([]*Signature, len(t.sigs))
		for i, sg := range t.sigs {
			sigs[i] = wrap(sg)
		}
		return newOverload(sigs)
	}
	return nil
}

func (s *spec) noteConstructor(e *ast.Call, sel *Selection) {
	if c := s.calls[e]; c != nil && c.Func == nil && sel != nil {
		c.Func = funcOf(sel)
	}
}

// constructBuiltin infers the calls of list, set, dict and tuple, whose
// elements the program tracks.
func (s *spec) constructBuiltin(e *ast.Call, elem *Named, args []argv) (Type, bool) {
	if elem.targs.Len() > 0 {
		return nil, false
	}
	name := elem.Obj().Name()
	switch name {
	case "list", "set", "tuple", "frozenset":
		return s.constructSeq(e, elem, name, args)
	case "dict":
		return s.constructDict(e, elem, args)
	}
	return nil, false
}

func (s *spec) constructSeq(e *ast.Call, elem *Named, name string, args []argv) (Type, bool) {
	if len(args) > 1 || len(args) == 1 && args[0].name != "" {
		return nil, false
	}
	var item Type = Typ[Never]
	if len(args) == 1 {
		item = Default(s.iterate(Site{e, SiteIter, 0}, Site{e, SiteNext, 0}, args[0].t))
	}
	s.calls[e] = &Call{Kind: Static, Overload: -1, Slot: "__init__"}
	switch name {
	case "tuple":
		return elemTuple([]Type{item}, 0), true
	case "frozenset":
		return newInstance(elem, []Type{item}), true
	}
	return s.newContainer(e, name, item), true
}

func (s *spec) constructDict(e *ast.Call, elem *Named, args []argv) (Type, bool) {
	var k, v Type = Typ[Never], Typ[Never]
	for _, a := range args {
		switch {
		case a.name != "":
			k, v = joinCap(k, builtin("str")), joinCap(v, Default(a.t))
		default:
			ak, av, ok := s.pairs(e, a.t)
			if !ok {
				return nil, false
			}
			k, v = joinCap(k, ak), joinCap(v, av)
		}
	}
	s.calls[e] = &Call{Kind: Static, Overload: -1, Slot: "__init__"}
	return s.newContainer(e, "dict", k, v), true
}

// pairs gives the key and value types of what dict(x) takes: a mapping, or
// an iterable of pairs.
func (s *spec) pairs(e *ast.Call, t Type) (Type, Type, bool) {
	if n := instanceClass(Unalias(t)); n != nil {
		if mp, ok := abcInstance("Mapping", Typ[Any], Typ[Any]).(*Named); ok {
			if base := asBase(n, mp.origin); base != nil && base.targs.Len() == 2 {
				return Default(norm(base.targs.At(0))), Default(norm(base.targs.At(1))), true
			}
		}
	}
	item := Unalias(s.iterate(Site{e, SiteIter, 0}, Site{e, SiteNext, 0}, t))
	if tup, ok := item.(*Tuple); ok && tup.unbounded < 0 && tup.Len() == 2 {
		return Default(tup.vars[0].typ), Default(tup.vars[1].typ), true
	}
	return nil, nil, false
}

// initSelf is a generic class applied to its own type parameters.
func initSelf(origin *Named) *Named {
	tps := origin.tparams.slice()
	targs := make([]Type, len(tps))
	for i, tp := range tps {
		targs[i] = tp
	}
	return newInstance(origin, targs)
}

// bareClass is the generic origin of a class applied to nothing but Any,
// which is what a class name without type arguments stands for.
func bareClass(n *Named) *Named {
	for _, a := range n.targs.slice() {
		if !isAny(a) {
			return n
		}
	}
	return n.origin
}
