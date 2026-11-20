package types

import "github.com/siyul-park/spry/ast"

// hasDecorators reports whether f is a function or method that decorators
// replace: the name of a decorated function is bound to what they return. The
// decorators that bind a method, and any decorator of a method, are not
// followed.
func hasDecorators(f *Func) bool {
	if f.deco != plainFunc || len(f.defs) != 1 {
		return false
	}
	def, ok := f.defs[0].(*ast.FunctionDef)
	if !ok || len(def.DecoratorList) == 0 {
		return false
	}
	p := f.Parent()
	return p != nil && p.Kind() != ClassScope
}

// decorate infers a def with decorators: they are applied from the one nearest
// the def outward to the function object, and the name is bound to the result.
// A decorator that returns a type the analysis does not follow gives Any.
func (s *spec) decorate(st *ast.FunctionDef, obj *Func, v *env) {
	var t Type = s.valueOfFunc(obj)
	for i := len(st.DecoratorList) - 1; i >= 0; i-- {
		t = s.applyDecorator(st.DecoratorList[i], t, v)
	}
	t = Default(t)
	if isCell(obj) {
		s.storeCell(s, obj, t)
	}
	if isNestedFunc(obj) {
		v.bind(obj, t)
		return
	}
	s.c.storeDecorated(obj, t)
	v.bind(obj, t)
}

// applyDecorator infers the call deco(arg). It is made of a call expression
// that the checker creates, so that the call is recorded like any other.
func (s *spec) applyDecorator(deco ast.Expr, arg Type, v *env) Type {
	call, ok := s.c.info.Synth[deco]
	if !ok {
		span := ast.Span{StartPos: deco.Pos(), EndPos: deco.End()}
		call = &ast.Call{Span: span, Func: deco, Args: []ast.Expr{&ast.Name{Span: span, ID: "$decorated"}}}
		s.c.info.Synth[deco] = call
	}
	s.record(call.Args[0], arg)
	cal := s.callee(deco, v)
	args := []argv{{t: arg}}
	var res Type = Typ[Any]
	switch {
	case cal.f != nil && cal.recv == nil && s.c.funcOf(cal.f) != nil:
		res = s.callFunc(call, cal, args, v)
	case cal.f == nil && cal.meta == nil && cal.form == nil:
		if t, ok := s.callValue(call, cal.typ, args); ok {
			res = t
		}
	}
	res = Default(res)
	if isAny(res) {
		s.fall(deco, reasonDecorator, "the result of the decorator is not known")
		res = anyFor(reasonDecorator)
	}
	return s.record(call, res)
}

// decoratedValue is the value a decorated function name has where it is read:
// what the decorators returned.
func (s *spec) decoratedValue(o *Func, v *env, d Definedness) Type {
	if !isNestedFunc(o) {
		return s.c.loadDecorated(s, o)
	}
	if d == Undefined {
		return Typ[Never]
	}
	if isCell(o) && s.ownedHere(o) {
		return s.loadCell(s, o)
	}
	return v.lookup(o).typ
}

// storeDecorated joins t into the value of a decorated function of a module.
func (c *solver) storeDecorated(f *Func, t Type) {
	g := c.decoGlobal(f)
	next := capType(Join(g.typ, Default(t)))
	if !Identical(next, g.typ) {
		g.typ = next
		c.notify(g.readers)
	}
}

// loadDecorated reads the value of a decorated function of a module on behalf
// of s.
func (c *solver) loadDecorated(s *spec, f *Func) Type {
	g := c.decoGlobal(f)
	g.readers[s] = struct{}{}
	return g.typ
}

func (c *solver) decoGlobal(f *Func) *global {
	if c.decos == nil {
		c.decos = map[*Func]*global{}
	}
	g := c.decos[f]
	if g == nil {
		g = &global{typ: Typ[Never], readers: map[*spec]struct{}{}}
		c.decos[f] = g
	}
	return g
}
