package types

import (
	"github.com/siyul-park/spry/ast"
)

func (s *spec) fail(n ast.Node, code, msg string) {
	s.errs = append(s.errs, &Error{Pos: s.position(n.Pos()), End: s.position(n.End()), Code: code, Msg: msg})
}

func (s *spec) tuple(e *ast.Tuple, v *env) Type {
	ts := make([]Type, len(e.Elts))
	for i, x := range e.Elts {
		if _, ok := x.(*ast.Starred); ok {
			s.items(e.Elts, v)
			return s.opaque(e, reasonDynamicItem, "a starred tuple element")
		}
		ts[i] = Default(s.expr(x, v))
	}
	return elemTuple(ts, -1)
}

func (s *spec) joined(e *ast.JoinedStr, v *env) Type {
	for _, part := range e.Values {
		fv, ok := part.(*ast.FormattedValue)
		if !ok {
			s.expr(part, v)
			continue
		}
		t := s.expr(fv.Value, v)
		if fv.FormatSpec != nil {
			s.expr(fv.FormatSpec, v)
		}
		s.calls[fv] = &Call{Kind: Static, Slot: "__format__", Overload: -1}
		s.formatUser(fv, t)
		s.record(fv, builtin("str"))
	}
	return builtin("str")
}

func (s *spec) ifExp(e *ast.IfExp, v *env) Type {
	_, tv, fv := s.test(e.Test, v)
	a := Default(s.expr(e.Body, tv))
	b := Default(s.expr(e.OrElse, fv))
	return joinCap(a, b)
}

// test infers e and the states in which it is truthy and falsy, narrowing
// the variable a test names.
func (s *spec) test(e ast.Expr, v *env) (Type, *env, *env) {
	switch e := e.(type) {
	case *ast.BoolOp:
		t, tv, fv := s.boolOp(e, v)
		s.record(e, t)
		return t, tv, fv
	case *ast.UnaryOp:
		if e.Op == ast.UnaryNot {
			_, tv, fv := s.test(e.Operand, v)
			return s.record(e, builtin("bool")), fv, tv
		}
	case *ast.Name:
		t := s.expr(e, v)
		s.truth(e, t)
		return t, s.narrowName(e, v, narrowTruthy), v
	case *ast.Compare:
		t := s.expr(e, v)
		if tv, fv, ok := s.narrowCompare(e, v); ok {
			return t, tv, fv
		}
		return t, v, v
	case *ast.Call:
		t := s.expr(e, v)
		if tv, fv, ok := s.narrowCall(e, v); ok {
			return t, tv, fv
		}
		return t, v, v
	}
	t := s.expr(e, v)
	s.truth(e, t)
	return t, v, v
}

// expr infers the type of e in v and records it.
func (s *spec) expr(e ast.Expr, v *env) Type {
	switch e := e.(type) {
	case *ast.Constant:
		return s.record(e, constantType(e.Value))
	case *ast.Name:
		return s.record(e, s.name(e, v))
	case *ast.BinOp:
		return s.record(e, s.binary(e, v))
	case *ast.UnaryOp:
		return s.record(e, s.unary(e, v))
	case *ast.BoolOp:
		t, _, _ := s.boolOp(e, v)
		return s.record(e, t)
	case *ast.Compare:
		return s.record(e, s.compare(e, v))
	case *ast.IfExp:
		return s.record(e, s.ifExp(e, v))
	case *ast.Call:
		return s.record(e, s.call(e, v))
	case *ast.Attribute:
		return s.record(e, s.attribute(e, s.expr(e.Value, v)))
	case *ast.JoinedStr:
		return s.record(e, s.joined(e, v))
	case *ast.Tuple:
		return s.record(e, s.tuple(e, v))
	case *ast.List:
		return s.record(e, s.list(e, v))
	case *ast.Set:
		return s.record(e, s.set(e, v))
	case *ast.Dict:
		return s.record(e, s.dict(e, v))
	case *ast.Slice:
		return s.record(e, s.sliceExpr(e, v))
	case *ast.Subscript:
		return s.record(e, s.subscript(e, v))
	case *ast.ListComp, *ast.SetComp, *ast.DictComp, *ast.GeneratorExp:
		return s.record(e, s.comprehension(e, v))
	case *ast.NamedExpr:
		return s.record(e, s.walrus(e, v))
	case *ast.Lambda:
		return s.record(e, s.lambdaExpr(e, v))
	case *ast.Yield:
		return s.record(e, s.yield(e, v))
	case *ast.YieldFrom:
		return s.record(e, s.yieldFrom(e, v))
	}
	return s.anyValue(e)
}

// boolOp infers a chain of and or or. The value is the left operand where it
// decides the result and the last operand otherwise.
func (s *spec) boolOp(e *ast.BoolOp, v *env) (Type, *env, *env) {
	var res Type = Typ[Never]
	cur := v
	var stop *env = deadEnv()
	last := len(e.Values) - 1
	for i, x := range e.Values {
		t, tv, fv := s.test(x, cur)
		if i == last {
			res = joinCap(res, Default(t))
			if e.Op == ast.BoolAnd {
				return res, tv, joinEnvs(stop, fv)
			}
			return res, joinEnvs(stop, tv), fv
		}
		if e.Op == ast.BoolAnd {
			res = joinCap(res, Default(t))
			stop, cur = joinEnvs(stop, fv), tv
		} else {
			res = joinCap(res, Default(narrowTruthy(t)))
			stop, cur = joinEnvs(stop, tv), fv
		}
	}
	return res, v, v
}

func (s *spec) narrowCall(e *ast.Call, v *env) (*env, *env, bool) {
	n, ok := e.Func.(*ast.Name)
	if !ok || len(e.Args) != 2 || len(e.Keywords) > 0 || s.narrowable(e.Args[0]) == nil {
		return nil, nil, false
	}
	b, ok := s.c.info.Uses[n].(*Builtin)
	if !ok || b.form != formIsinstance {
		return nil, nil, false
	}
	classes, ok := instancesOf(s.types[e.Args[1]].Type)
	if !ok {
		return nil, nil, false
	}
	return s.narrowName(e.Args[0], v, func(t Type) Type { return narrowIsinstance(t, classes, true) }),
		s.narrowName(e.Args[0], v, func(t Type) Type { return narrowIsinstance(t, classes, false) }), true
}

func (s *spec) narrowCompare(e *ast.Compare, v *env) (*env, *env, bool) {
	if len(e.Ops) != 1 || (e.Ops[0] != ast.CmpIs && e.Ops[0] != ast.CmpIsNot) {
		return nil, nil, false
	}
	c, ok := e.Comparators[0].(*ast.Constant)
	if !ok {
		return nil, nil, false
	}
	if _, none := c.Value.(ast.None); !none || s.narrowable(e.Left) == nil {
		return nil, nil, false
	}
	is := e.Ops[0] == ast.CmpIs
	return s.narrowName(e.Left, v, func(t Type) Type { return narrowIs(t, is) }),
		s.narrowName(e.Left, v, func(t Type) Type { return narrowIs(t, !is) }), true
}

func (s *spec) attribute(e *ast.Attribute, recv Type) Type {
	if isNever(recv) {
		return Typ[Never]
	}
	if m, ok := recv.(*Module); ok {
		obj := s.c.member(m, e.Attr)
		if obj == nil {
			s.fall(e, reasonUnresolvedAttr, "module "+m.name+" has no member "+e.Attr)
			return anyFor(reasonUnresolvedAttr)
		}
		s.c.info.Uses[e] = obj
		return s.memberValue(obj)
	}
	if isAny(recv) {
		if optedOut([]Type{recv}) {
			return annotatedAny
		}
		reason := s.anyReason(e, recv)
		s.fall(e, reason, "attribute of an Any value")
		return anyFor(reason)
	}
	recv = withoutNone(recv)
	if t, ok := s.instanceAttribute(e, recv); ok {
		return t
	}
	sel := LookupAttr(Default(recv), e.Attr)
	if sel == nil || sel.kind == MethodVal || sel.kind == MethodExpr || sel.kind == GetattrVal {
		if sel == nil {
			if len(unionTerms(Unalias(recv))) > 1 {
				return s.opaque(e, reasonUnion, "an attribute of a union")
			}
			return s.opaque(e, reasonUnresolvedAttr, "the value has no attribute "+e.Attr)
		}
		return s.opaque(e, reasonDynamicAttr, "a bound method or computed attribute read as a value")
	}
	s.c.info.Selections[e] = sel
	return sel.typ
}

// memberValue is the type of a module member read as a value.
func (s *spec) memberValue(obj Object) Type {
	switch o := obj.(type) {
	case *Var:
		if s.c.srcs[o.module] != nil {
			return s.c.load(s, o)
		}
		return o.typ
	case *ModuleName:
		return o.Type()
	case *TypeName:
		if t := s.c.p.decl.typeNameType(o); !isInvalid(t) {
			return NewMeta(t)
		}
	}
	return Typ[Any]
}

// opaque records an Any value that stands for a dynamic operation at e.
func (s *spec) opaque(e ast.Expr, reason, msg string) Type {
	s.fall(e, reason, msg)
	return s.record(e, anyFor(reason))
}

// anyValue records an Any value that no dynamic operation produced: the
// construct is rejected elsewhere or has no value.
func (s *spec) anyValue(e ast.Expr) Type {
	return s.record(e, Typ[Any])
}

func (s *spec) record(e ast.Expr, t Type) Type {
	s.types[e] = TypeAndValue{Type: t}
	return t
}

func (s *spec) name(n *ast.Name, v *env) Type {
	obj, free := s.resolve(n.ID)
	switch {
	case free:
		return s.freeValue(n)
	case obj == nil:
		s.names[n] = Undefined
		return Typ[Never]
	}
	s.c.info.Uses[n] = obj
	d := s.definedness(n, obj, v)
	return s.objectValue(obj, v, d)
}

func (s *spec) fall(n ast.Node, reason, msg string) {
	s.falls = append(s.falls, Fallback{Pos: s.position(n.Pos()), End: s.position(n.End()), Reason: reason, Msg: msg})
}

// objectValue is the type of the value an object stands for.
func (s *spec) objectValue(obj Object, v *env, d Definedness) Type {
	switch o := obj.(type) {
	case *Var:
		if o.kind == GlobalVar {
			return s.c.load(s, o)
		}
		if d == Undefined {
			return Typ[Never]
		}
		if isCell(o) && s.ownedHere(o) {
			return s.loadCell(s, o)
		}
		return v.lookup(o).typ
	case *Func:
		if hasDecorators(o) {
			return s.decoratedValue(o, v, d)
		}
		if fn := s.c.funcOf(o); fn != nil {
			if fn.opaque {
				s.escapes = append(s.escapes, s.c.escape(fn, nil, nil))
				return Typ[Any]
			}
			return s.closureOf(fn)
		}
		return Typ[Any]
	case *TypeName:
		if t := s.c.p.decl.typeNameType(o); !isInvalid(t) {
			return NewMeta(t)
		}
	case *ModuleName:
		return o.Type()
	case *Builtin:
		return o.Type()
	}
	return Typ[Any]
}

// definedness records whether the name is bound where it is read.
func (s *spec) definedness(n *ast.Name, obj Object, v *env) Definedness {
	var d Definedness
	switch {
	case obj.Parent() == Universe:
	case s.isInit() || s.ownedHere(obj):
		d = v.lookup(obj).def
	default:
		s.gnames[n] = obj
	}
	if d != Defined {
		s.names[n] = d
	}
	return d
}

func (s *spec) isInit() bool {
	return s.fn.def == nil
}

// narrowName returns v with the variable that e names narrowed by f, or v.
func (s *spec) narrowName(e ast.Expr, v *env, f func(Type) Type) *env {
	obj := s.narrowable(e)
	if obj == nil {
		return v
	}
	out := v.clone()
	sl := out.lookup(obj)
	narrowed := f(sl.typ)
	out.vars[obj] = slot{typ: narrowed, def: sl.def}
	if isNever(narrowed) && !isNever(sl.typ) {
		out.live = false
	}
	return out
}

// narrowable returns the local variable that e names if it is not a cell.
func (s *spec) narrowable(e ast.Expr) *Var {
	n, ok := e.(*ast.Name)
	if !ok {
		return nil
	}
	obj, _ := s.resolve(n.ID)
	if v, ok := obj.(*Var); ok && !v.cell && v.kind != GlobalVar {
		return v
	}
	return nil
}

// resolve finds the object a name refers to in the scope of the spec. The
// second result is true for a name of an enclosing function.
func (s *spec) resolve(name string) (Object, bool) {
	for i := len(s.comps) - 1; i >= 0; i-- {
		sc := s.comps[i]
		switch sc.storage(name) {
		case storeLocal, storeCell:
			return sc.Lookup(name), false
		case storeGlobalExplicit, storeGlobalImplicit:
			return s.moduleName(name), false
		}
	}
	sc := s.fn.scope
	switch sc.Kind() {
	case FunctionScope:
		switch sc.storage(name) {
		case storeLocal, storeCell:
			return sc.Lookup(name), false
		case storeFree:
			return nil, true
		}
	default:
		if obj := sc.Lookup(name); obj != nil {
			return obj, false
		}
	}
	return s.moduleName(name), false
}

func (s *spec) moduleName(name string) Object {
	if obj := s.fn.src.mod.scope.Lookup(name); obj != nil {
		return obj
	}
	return Universe.Lookup(name)
}

// member finds a member of a module, including a submodule.
func (c *solver) member(m *Module, name string) Object {
	if obj := m.scope.Lookup(name); obj != nil {
		return obj
	}
	if src := c.p.mods[m.name+"."+name]; src != nil {
		return newModuleName(0, name, src.mod)
	}
	return nil
}

func constantType(val ast.Value) Type {
	switch val := val.(type) {
	case ast.Int, ast.Str, ast.Bytes, ast.Bool:
		return NewLiteral(val)
	case ast.Float:
		return builtin("float")
	case ast.Complex:
		return builtin("complex")
	case ast.Ellipsis:
		return builtin("EllipsisType")
	}
	return builtin("NoneType")
}

// instancesOf turns the type of a class or a tuple of classes into the
// instance types they stand for.
func instancesOf(t Type) ([]Type, bool) {
	switch t := t.(type) {
	case *Meta:
		return []Type{t.elem}, true
	case *Tuple:
		var out []Type
		for _, e := range t.elems() {
			m, ok := e.(*Meta)
			if !ok {
				return nil, false
			}
			out = append(out, m.elem)
		}
		return out, true
	}
	return nil, false
}
