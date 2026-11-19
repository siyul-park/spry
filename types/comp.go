package types

import "github.com/siyul-park/spry/ast"

// inlineConsumers are the functions that consume a whole iterable in order
// and have no effect of their own, so that a generator expression passed to
// one may be evaluated into a list first without a difference.
var inlineConsumers = map[string]bool{
	"sum": true, "min": true, "max": true, "sorted": true, "join": true,
	"list": true, "tuple": true, "set": true, "frozenset": true,
}

// comprehension infers a list, set or dict comprehension or a generator
// expression. The body is analyzed in place, as if inlined into the function:
// the iteration variables are variables of the comprehension scope that live
// for the evaluation, and what the body binds in the function (a walrus
// target) is joined into the state after it.
func (s *spec) comprehension(e ast.Expr, v *env) Type {
	gens, elts := compParts(e)
	if g, ok := e.(*ast.GeneratorExp); ok {
		if s.inline[g] {
			s.calls[g] = &Call{Kind: Static, Overload: -1, Slot: "inline"}
		}
	}
	sc := s.c.info.Scopes[e]
	cv := v.clone()
	for i, g := range gens {
		it := s.expr(g.Iter, cv)
		if i == 0 && sc != nil {
			s.comps = append(s.comps, sc)
		}
		item := s.iterate(Site{g.Iter, SiteIter, 0}, Site{g.Iter, SiteNext, 0}, it)
		s.record(g.Target, item)
		s.assign(g.Target, item, nil, cv)
		for _, c := range g.Ifs {
			_, tv, _ := s.test(c, cv)
			cv = tv.clone()
		}
	}
	items := make([]Type, len(elts))
	for i, x := range elts {
		items[i] = Default(s.expr(x, cv))
	}
	if sc != nil {
		s.comps = s.comps[:len(s.comps)-1]
	}
	s.leaveComp(v, cv)
	switch e.(type) {
	case *ast.ListComp:
		return s.newContainer(e, "list", items...)
	case *ast.SetComp:
		return s.newContainer(e, "set", items...)
	case *ast.DictComp:
		return s.newContainer(e, "dict", items...)
	}
	return abcInstance("Generator", items[0], builtin("NoneType"), builtin("NoneType"))
}

// compParts returns the generators of a comprehension and the expressions
// whose values it collects.
func compParts(e ast.Expr) ([]*ast.Comprehension, []ast.Expr) {
	switch e := e.(type) {
	case *ast.ListComp:
		return e.Generators, []ast.Expr{e.Elt}
	case *ast.SetComp:
		return e.Generators, []ast.Expr{e.Elt}
	case *ast.DictComp:
		return e.Generators, []ast.Expr{e.Key, e.Value}
	case *ast.GeneratorExp:
		return e.Generators, []ast.Expr{e.Elt}
	}
	return nil, nil
}

// leaveComp drops the variables of the comprehension from its state and
// joins the others into the state of the function: the body may not run.
func (s *spec) leaveComp(v, cv *env) {
	for k, sl := range cv.vars {
		if p := k.Parent(); p != nil && p.Kind() == ComprehensionScope {
			continue
		}
		v.vars[k] = joinSlots(v.lookup(k), sl)
	}
}

// walrus infers (name := value).
func (s *spec) walrus(e *ast.NamedExpr, v *env) Type {
	t := s.expr(e.Value, v)
	if n, ok := e.Target.(*ast.Name); ok {
		s.bindName(n, t, v)
	}
	return t
}

// markInline records that the generator expression that is the only
// argument of a call is lowered as the list of its items.
func (s *spec) markInline(e *ast.Call, cal callee) {
	if len(e.Args) != 1 || len(e.Keywords) > 0 {
		return
	}
	gen, ok := e.Args[0].(*ast.GeneratorExp)
	if !ok {
		return
	}
	name := ""
	switch {
	case cal.f != nil && cal.f.module != nil && cal.f.module.name == "builtins":
		name = cal.f.name
	case cal.meta != nil:
		if n, ok := Unalias(cal.meta.elem).(*Named); ok && isBuiltin(n, n.Obj().Name()) {
			name = n.Obj().Name()
		}
	}
	if inlineConsumers[name] {
		if s.inline == nil {
			s.inline = map[*ast.GeneratorExp]bool{}
		}
		s.inline[gen] = true
	}
}

// abcInstance is the class of collections.abc with the type arguments.
func abcInstance(name string, targs ...Type) Type {
	m := shippedModule("collections.abc")
	if m == nil {
		return Typ[Any]
	}
	tn, ok := m.scope.Lookup(name).(*TypeName)
	if !ok {
		return Typ[Any]
	}
	n, ok := tn.typ.(*Named)
	if !ok || n.tparams.Len() != len(targs) {
		return Typ[Any]
	}
	return newInstance(n, targs)
}
