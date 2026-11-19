package types

import (
	"github.com/siyul-park/spry/ast"
)

// guard collects what the statements a try, with or finally covers can see
// when they raise: exc joins the state before and after each statement, and
// exits the states in which a return, break or continue leaves through a
// finally.
type guard struct {
	exc   *env
	exits []*env
	fin   bool
}

// exit records that a return, or a break or continue of a loop opened at
// depth, leaves in state v through the finally clauses of the guards above
// depth.
func (s *spec) exit(v *env, depth int) {
	if !v.live {
		return
	}
	for _, g := range s.guards[min(depth, len(s.guards)):] {
		if g.fin {
			g.exits = append(g.exits, v.clone())
		}
	}
}

// raiseStmt infers raise, raise X and raise X from Y. A class is raised
// through the instance it makes.
func (s *spec) raiseStmt(st *ast.Raise, v *env) {
	if st.Exc != nil {
		s.exception(st.Exc, v)
	}
	if st.Cause != nil {
		s.exception(st.Cause, v)
	}
	s.touch(v)
	v.live = false
}

// exception infers an exception operand of raise: an instance, or a class
// that is instantiated with no arguments.
func (s *spec) exception(e ast.Expr, v *env) Type {
	t := s.expr(e, v)
	if m, ok := Unalias(t).(*Meta); ok && s.isException(m.elem) {
		call, ok := s.c.info.Synth[e]
		if !ok {
			call = &ast.Call{Span: ast.Span{StartPos: e.Pos(), EndPos: e.End()}, Func: e}
			s.c.info.Synth[e] = call
		}
		t = s.expr(call, v)
	}
	if isAny(t) || isNever(t) || s.isException(t) {
		return t
	}
	if _, isNone := t.(*Named); isNone && isNoneType(t) {
		return t
	}
	s.fall(e, reasonDynamicExcept, "only an exception can be raised")
	return anyFor(reasonDynamicExcept)
}

// tryStmt infers try with except, else and finally clauses. A handler starts
// in the join of the states in which the body can raise. The finally clause
// is analyzed for the normal path, and again for every path that reaches it
// with an exception, a return, a break or a continue, which gives the types
// of its nodes the join of all of them.
func (s *spec) tryStmt(body []ast.Stmt, handlers []*ast.ExceptHandler, orElse, final []ast.Stmt, v *env) {
	var fin *guard
	if len(final) > 0 {
		fin = openGuard(true)
		s.push(fin)
	}
	exc := openGuard(false)
	s.push(exc)
	b := v.clone()
	s.touch(b)
	s.block(body, b)
	s.pop()
	s.block(orElse, b)
	out := b
	for _, h := range handlers {
		hv := exc.exc.clone()
		s.handler(h, hv)
		out = joinEnvs(out, hv)
	}
	if fin == nil {
		*v = *out
		return
	}
	s.pop()
	abnormal := fin.exc
	for _, e := range fin.exits {
		abnormal = joinEnvs(abnormal, e)
	}
	normal := out.clone()
	s.block(final, normal)
	if all := joinEnvs(out, abnormal); all.live {
		s.block(final, all)
	}
	*v = *normal
}

// handler infers one except clause in state v: the name it binds has the
// type of the instances of the classes it catches, and is unbound again where
// the clause ends.
func (s *spec) handler(h *ast.ExceptHandler, v *env) {
	if !v.live {
		return
	}
	var caught Type = builtin("BaseException")
	if h.Type != nil {
		mt := s.expr(h.Type, v)
		classes, ok := instancesOf(mt)
		for _, c := range classes {
			ok = ok && s.isException(c)
		}
		if ok && len(classes) > 0 {
			caught = NewUnion(classes...)
		} else {
			s.fall(h.Type, reasonDynamicExcept, "the classes an except clause catches are not known")
		}
	}
	var obj Object
	if h.Name != nil {
		obj = s.c.info.Defs[h]
		s.bindTo(obj, h, caught, v)
	}
	s.block(h.Body, v)
	if obj != nil && v.live {
		v.vars[obj] = slot{typ: Typ[Never], def: Undefined}
		s.guard(obj)
	}
}

// isException reports whether t is an instance of an exception class.
func (s *spec) isException(t Type) bool {
	base := builtin("BaseException")
	for _, term := range unionTerms(Unalias(t)) {
		if n, ok := term.(*Named); !ok || base == nil || asBase(n, base.origin) == nil {
			return false
		}
	}
	return true
}

// assertStmt infers assert: the message is evaluated only where the test is
// false, and the test narrows what follows.
func (s *spec) assertStmt(st *ast.Assert, v *env) {
	_, tv, fv := s.test(st.Test, v)
	if st.Msg != nil {
		s.expr(st.Msg, fv.clone())
	}
	s.touch(fv)
	*v = *tv.clone()
}

// withStmt infers with: each item enters a context manager whose __exit__
// runs when the body is left. A manager whose __exit__ can return true
// swallows what the body raises, so the statement can also end in a state of
// the body that raised.
func (s *spec) withStmt(items []*ast.WithItem, body []ast.Stmt, v *env) {
	if len(items) == 0 {
		s.block(body, v)
		return
	}
	item := items[0]
	t := s.expr(item.ContextExpr, v)
	entered := s.protocol(Site{item.ContextExpr, SiteEnter, 0}, t, "__enter__", nil)
	if item.OptionalVars != nil {
		s.assign(item.OptionalVars, entered, nil, v)
	}
	none := builtin("NoneType")
	s.protocol(Site{item.ContextExpr, SiteExit, 0}, t, "__exit__", []argv{{t: none}, {t: none}, {t: none}})
	res := s.protocol(Site{item.ContextExpr, SiteExit, 1}, t, "__exit__", []argv{{t: Typ[Any]}, {t: Typ[Any]}, {t: Typ[Any]}})
	g := openGuard(false)
	s.push(g)
	s.touch(v)
	s.withStmt(items[1:], body, v)
	s.pop()
	if swallows(res) {
		*v = *joinEnvs(v, g.exc)
	}
}

func (s *spec) push(g *guard) {
	s.guards = append(s.guards, g)
}

func (s *spec) pop() {
	s.guards = s.guards[:len(s.guards)-1]
}

// touch records the state v in every open guard: any statement can raise, so
// a handler sees the state before and after each one.
func (s *spec) touch(v *env) {
	if !v.live {
		return
	}
	for _, g := range s.guards {
		g.exc = joinEnvs(g.exc, v)
	}
}

// protocol resolves the call of a method that an operation of the language
// makes on a value, such as __enter__ for with, and returns its result.
func (s *spec) protocol(site Site, recv Type, name string, args []argv) Type {
	recv = Default(recv)
	switch {
	case isNever(recv):
		return recv
	case isAny(recv):
		return s.dynamic(site, name, "the value is Any", recv)
	}
	if isMeta(recv) { // the method is looked up on the type of the class, not on the class
		return s.dynamicReason(site, name, reasonUnresolvedAttr, "the value is a class", Typ[Any])
	}
	if res, call, ok := s.userMethod(site, recv, name, args); ok {
		s.calls[site] = call
		return res
	}
	sel := LookupAttr(recv, name)
	if sel == nil || len(unionTerms(recv)) > 1 {
		return s.dynamicReason(site, name, reasonUnresolvedAttr, "the value has no "+name, Typ[Any])
	}
	res, idx, ok := matchType(sel.typ, args)
	if !ok {
		return s.dynamicReason(site, name, reasonDynamicCall, "no signature of "+name+" accepts the arguments", Typ[Any])
	}
	s.calls[site] = &Call{Kind: Static, Func: funcOf(sel), Overload: idx, Slot: name}
	return norm(res)
}

func openGuard(fin bool) *guard {
	return &guard{exc: deadEnv(), fin: fin}
}

// swallows reports whether the result of __exit__ can be true, so that the
// exception of the body stops there.
func swallows(res Type) bool {
	if l, ok := res.(*Literal); ok {
		b, isBool := l.Value().(ast.Bool)
		return !isBool || bool(b)
	}
	return !isNever(res) && !isNoneType(res)
}
