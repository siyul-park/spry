package types

import (
	"github.com/siyul-park/spry/ast"
)

func (s *spec) list(e *ast.List, v *env) Type {
	elem, _ := s.items(e.Elts, v)
	return s.newContainer(e, "list", elem)
}

func (s *spec) set(e *ast.Set, v *env) Type {
	elem, star := s.items(e.Elts, v)
	if star {
		return s.opaque(e, reasonDynamicItem, "a starred set element")
	}
	return s.newContainer(e, "set", elem)
}

// items joins the types of the items of a list, tuple or set display. A
// starred item contributes the items of its iterable. The second result
// reports a starred item.
func (s *spec) items(elts []ast.Expr, v *env) (Type, bool) {
	var elem Type = Typ[Never]
	star := false
	for _, x := range elts {
		if st, ok := x.(*ast.Starred); ok {
			star = true
			t := s.expr(st.Value, v)
			s.record(st, t)
			elem = joinCap(elem, Default(s.iterate(Site{st, SiteIter, 0}, Site{st, SiteNext, 0}, t)))
			continue
		}
		elem = joinCap(elem, Default(s.expr(x, v)))
	}
	return elem, star
}

func (s *spec) dict(e *ast.Dict, v *env) Type {
	var k, val Type = Typ[Never], Typ[Never]
	for i, key := range e.Keys {
		if key == nil {
			s.expr(e.Values[i], v)
			return s.opaque(e, reasonDynamicItem, "a ** item of a dict display")
		}
		k = joinCap(k, Default(s.expr(key, v)))
		val = joinCap(val, Default(s.expr(e.Values[i], v)))
	}
	return s.newContainer(e, "dict", k, val)
}

// sliceExpr infers the bounds of a slice and gives it its class.
func (s *spec) sliceExpr(e *ast.Slice, v *env) Type {
	for _, x := range []ast.Expr{e.Lower, e.Upper, e.Step} {
		if x != nil {
			s.expr(x, v)
		}
	}
	return builtin("slice")
}

// subscript infers x[k].
func (s *spec) subscript(e *ast.Subscript, v *env) Type {
	recv := s.expr(e.Value, v)
	key := s.expr(e.Slice, v)
	return s.getitem(e, e, recv, key)
}

// getitem resolves recv[key]; the call is recorded at site.
func (s *spec) getitem(site ast.Node, node ast.Expr, recv, key Type) Type {
	switch {
	case isNever(recv) || isNever(key):
		return Typ[Never]
	case isAny(recv):
		return s.dynamic(site, "__getitem__", "subscript of Any", recv)
	}
	if tup, ok := Unalias(recv).(*Tuple); ok && tup.unbounded < 0 {
		if lit, ok := key.(*Literal); ok {
			if i, ok := tupleIndex(lit, tup.Len()); ok {
				s.calls[site] = &Call{Kind: Static, Overload: -1, Slot: "__getitem__"}
				return tup.vars[i].typ
			}
		}
		if _, ok := key.(*Literal); !ok && !isBuiltin(Default(key), "slice") {
			return s.dynamicReason(site, "__getitem__", reasonDynamicItem, "a variable index of a tuple", s.tupleJoin(tup))
		}
	}
	if res, call, ok := s.userMethod(site, recv, "__getitem__", []argv{{t: key}}); ok {
		s.calls[site] = call
		return res
	}
	sel := LookupAttr(Default(recv), "__getitem__")
	if sel == nil || len(unionTerms(Default(recv))) > 1 {
		return s.dynamicReason(site, "__getitem__", s.unionOr(recv), "no __getitem__", Typ[Any])
	}
	res, idx, ok := matchType(sel.typ, []argv{{t: key}})
	if !ok {
		return s.dynamic(site, "__getitem__", "no __getitem__ accepts the key")
	}
	s.calls[site] = &Call{Kind: Static, Func: funcOf(sel), Overload: idx, Slot: "__getitem__"}
	return s.fromLib(node, res)
}

// fromLib completes the type a call or operation of the standard library
// returns: standalone sites become what they hold and containers that are new
// get sites of their own.
func (s *spec) fromLib(node ast.Node, t Type) Type {
	return s.fresh(node, norm(t))
}

func (s *spec) tupleJoin(t *Tuple) Type {
	var out Type = Typ[Never]
	for _, e := range t.elems() {
		out = joinCap(out, Default(e))
	}
	return out
}

// setitem resolves recv[key] = val; the call is recorded at the store site
// of target.
func (s *spec) setitem(target *ast.Subscript, recv, key, val Type) {
	site := Site{target, SiteStore, 0}
	s.mutate(site, "__setitem__", recv, []argv{{t: key}, {t: val}})
}

// delitem resolves del recv[key].
func (s *spec) delitem(target *ast.Subscript, recv, key Type) {
	site := Site{target, SiteDelete, 0}
	s.mutate(site, "__delitem__", recv, []argv{{t: key}})
}

// mutate calls a method of recv that stores its arguments.
func (s *spec) mutate(site ast.Node, name string, recv Type, args []argv) {
	if isNever(recv) {
		return
	}
	if isAny(recv) {
		s.dynamic(site, name, "an Any container", recv)
		return
	}
	if _, call, ok := s.userMethod(site, recv, name, args); ok {
		s.calls[site] = call
		return
	}
	sel := LookupAttr(Default(recv), name)
	if sel == nil || len(unionTerms(Default(recv))) > 1 {
		s.dynamicReason(site, name, s.unionOr(recv), "no "+name, Typ[Any])
		return
	}
	_, idx, ok := matchType(sel.typ, args)
	if !ok {
		s.dynamic(site, name, "no "+name+" accepts the arguments")
		return
	}
	s.flowArgs(sel.typ, idx, args)
	s.calls[site] = &Call{Kind: Static, Func: funcOf(sel), Overload: idx, Slot: name}
}

// unionOr is the reason an operation on t is dynamic: a union operand or a
// value the checker does not infer.
func (s *spec) unionOr(t Type) string {
	if len(unionTerms(Default(t))) > 1 {
		return reasonUnion
	}
	return s.anyReason(nil)
}

// flowArgs lets what a call passes flow into the containers its parameters
// name.
func (s *spec) flowArgs(typ Type, idx int, args []argv) {
	var sig *Signature
	switch t := typ.(type) {
	case *Signature:
		sig = t
	case *Overload:
		if idx >= 0 && idx < len(t.sigs) {
			sig = t.sigs[idx]
		}
	}
	if sig == nil {
		return
	}
	b, msg := matchArgs(sig, args)
	if msg != "" {
		return
	}
	m, _ := inferArgs(sig, b)
	for _, p := range sig.params.slice() {
		t, ok := b.types[p]
		if !ok {
			continue
		}
		if acceptsCall(p.typ) {
			s.escapeClosures(t, subst(p.typ, m))
		}
		s.flowIn(p.typ, t)
	}
	if g := groupParams(sig); g.varpos != nil {
		for _, t := range b.extra {
			s.flowIn(g.varpos.typ, t)
		}
	}
}

// tupleIndex resolves a literal index of a tuple of n items.
func tupleIndex(l *Literal, n int) (int, bool) {
	i, ok := l.value.(ast.Int)
	if !ok || i.N == nil || !i.N.IsInt64() {
		return 0, false
	}
	idx := int(i.N.Int64())
	if idx < 0 {
		idx += n
	}
	return idx, idx >= 0 && idx < n
}
