package types

import (
	"fmt"

	"github.com/siyul-park/spry/ast"
)

// loop collects the states that leave or restart the loop being analyzed.
type loop struct {
	breaks, conts []*env
	// depth is the number of guards open where the loop starts: the ones
	// above it are left by a break or continue.
	depth int
}

// iterations bounds the passes over a loop body before every variable that
// still changes is widened to Any.
const iterations = 16

// pass analyzes the body of the spec once.
func (s *spec) pass() {
	s.types, s.calls, s.names = map[ast.Expr]TypeAndValue{}, map[ast.Node]*Call{}, map[*ast.Name]Definedness{}
	s.gnames, s.vars = map[*ast.Name]Object{}, map[*Var]Type{}
	s.falls, s.errs, s.escapes, s.loops, s.callsAt, s.stmts, s.comps, s.guards = nil, nil, nil, nil, nil, nil, nil, nil
	s.dead = map[ast.Stmt]bool{}
	s.pats, s.guarded = nil, nil
	s.m = s.fn.substMapOf(s.args)
	s.ret = Typ[Never]
	s.yields, s.sends, s.unused = Typ[Never], Typ[Never], nil
	s.declRet = nil
	v := newEnv()
	var body []ast.Stmt
	if def := s.fn.def; def != nil {
		for _, p := range s.fn.sig.params.slice() {
			t := s.selfBound(localType(p, s.m))
			v.bind(p, t)
			s.vars[p] = t
			if isCell(p) {
				s.storeCell(s, p, t)
			}
		}
		if s.fn.ret == nil {
			s.declRet = subst(s.fn.sig.result, s.m)
		}
		body = def.Body
		s.block(body, v)
	} else {
		body = s.fn.src.file.Body
		s.topLevel(body, v)
	}
	res := s.ret
	if v.live {
		res = joinCap(res, builtin("NoneType"))
	}
	if s.fn.gen {
		res = s.generatorOf(res)
	}
	if s.isInit() {
		s.end = map[Object]Definedness{}
		for o, sl := range v.vars {
			s.end[o] = sl.def
		}
		res = builtin("NoneType")
	}
	s.setResult(res)
}

// topLevel analyzes a module body, remembering what is bound before each
// statement for the definedness of what the calls of that statement read.
func (s *spec) topLevel(body []ast.Stmt, v *env) {
	for _, st := range body {
		if !v.live {
			return
		}
		snap := map[Object]Definedness{}
		for o, sl := range v.vars {
			snap[o] = sl.def
		}
		s.stmts = append(s.stmts, snap)
		s.callsAt = append(s.callsAt, nil)
		s.stmt(st, v)
	}
}

func (s *spec) setResult(res Type) {
	if s.fn.def != nil && s.fn.ret == nil && !s.fn.gen {
		return
	}
	if next := joinCap(s.result, res); !Identical(next, s.result) {
		s.result = next
		s.c.notify(s.readers)
	}
}

// widen gives up on a spec that keeps changing.
func (s *spec) widen() {
	s.result = Typ[Any]
	s.c.notify(s.readers)
}

func (s *spec) block(list []ast.Stmt, v *env) {
	for i, st := range list {
		if !v.live {
			s.markDead(list[i:])
			return
		}
		delete(s.dead, st)
		s.touch(v)
		s.stmt(st, v)
		s.touch(v)
	}
}

func (s *spec) stmt(st ast.Stmt, v *env) {
	switch st := st.(type) {
	case *ast.ExprStmt:
		if y, ok := st.Value.(*ast.Yield); ok {
			s.unusedYield(y)
		}
		s.expr(st.Value, v)
	case *ast.Assign:
		s.assignStmt(st, v)
	case *ast.AnnAssign:
		s.annAssign(st, v)
	case *ast.AugAssign:
		s.augAssign(st, v)
	case *ast.Return:
		s.returnStmt(st, v)
	case *ast.If:
		s.ifStmt(st, v)
	case *ast.While:
		s.whileStmt(st, v)
	case *ast.For:
		s.forStmt(st, v)
	case *ast.Break:
		s.leave(v, func(l *loop, e *env) { l.breaks = append(l.breaks, e) })
	case *ast.Continue:
		s.leave(v, func(l *loop, e *env) { l.conts = append(l.conts, e) })
	case *ast.Pass, *ast.Global, *ast.Nonlocal:
	case *ast.Delete:
		s.deleteStmt(st, v)
	case *ast.FunctionDef:
		s.funcDef(st, v)
	case *ast.ClassDef:
		s.classDef(st, v)
	case *ast.Import:
		s.importStmt(st, v)
	case *ast.ImportFrom:
		s.importFrom(st, v)
	case *ast.Raise:
		s.raiseStmt(st, v)
	case *ast.Try:
		s.tryStmt(st.Body, st.Handlers, st.OrElse, st.FinalBody, v)
	case *ast.With:
		s.withStmt(st.Items, st.Body, v)
	case *ast.Assert:
		s.assertStmt(st, v)
	case *ast.Match:
		s.matchStmt(st, v)
	default:
		s.clobber(st, v)
	}
}

func (s *spec) forStmt(st *ast.For, v *env) {
	it := s.expr(st.Iter, v)
	elem := s.iterate(st, Site{st, SiteNext, 0}, it)
	s.record(st.Target, elem)
	head := v.clone()
	var l *loop
	for i := 0; ; i++ {
		l = &loop{depth: len(s.guards)}
		s.loops = append(s.loops, l)
		cur := head.clone()
		s.assign(st.Target, elem, nil, cur)
		s.block(st.Body, cur)
		s.loops = s.loops[:len(s.loops)-1]
		next := head
		for _, e := range append([]*env{cur}, l.conts...) {
			next = joinEnvs(next, e)
		}
		if sameEnv(next, head) {
			break
		}
		if i >= iterations {
			widenEnv(next, head)
		}
		head = next
		if i > iterations {
			break
		}
	}
	exit := head.clone()
	s.block(st.OrElse, exit)
	*v = *joinAll(exit, l.breaks)
}

func (s *spec) whileStmt(st *ast.While, v *env) {
	head := v.clone()
	var exit *env
	var l *loop
	for i := 0; ; i++ {
		l = &loop{depth: len(s.guards)}
		s.loops = append(s.loops, l)
		_, tv, fv := s.test(st.Test, head)
		cur := tv.clone()
		s.block(st.Body, cur)
		s.loops = s.loops[:len(s.loops)-1]
		exit = fv.clone()
		next := head
		for _, e := range append([]*env{cur}, l.conts...) {
			next = joinEnvs(next, e)
		}
		if sameEnv(next, head) {
			break
		}
		if i >= iterations {
			widenEnv(next, head)
		}
		head = next
		if i > iterations {
			break
		}
	}
	s.block(st.OrElse, exit)
	*v = *joinAll(exit, l.breaks)
}

func (s *spec) ifStmt(st *ast.If, v *env) {
	_, tv, fv := s.test(st.Test, v)
	tv, fv = tv.clone(), fv.clone()
	s.block(st.Body, tv)
	s.block(st.OrElse, fv)
	*v = *joinEnvs(tv, fv)
}

func (s *spec) markDead(list []ast.Stmt) {
	for _, st := range list {
		s.dead[st] = true
	}
}

func (s *spec) augAssign(st *ast.AugAssign, v *env) {
	names := binaryNames[st.Op]
	switch target := st.Target.(type) {
	case *ast.Name:
		l := s.expr(target, v)
		r := s.expr(st.Value, v)
		s.bindName(target, s.inplace(st, l, r, names), v)
	case *ast.Subscript:
		recv := s.expr(target.Value, v)
		key := s.expr(target.Slice, v)
		l := s.record(target, s.getitem(target, target, recv, key))
		r := s.expr(st.Value, v)
		s.setitem(target, recv, key, s.inplace(st, l, r, names))
	case *ast.Attribute:
		l := s.expr(target, v)
		r := s.expr(st.Value, v)
		s.storeAttribute(target, s.inplace(st, l, r, names), v)
	default:
		s.expr(st.Value, v)
	}
}

func (s *spec) deleteStmt(st *ast.Delete, v *env) {
	for _, t := range st.Targets {
		s.deleteTarget(t, v)
	}
}

// deleteTarget infers del of a name, which unbinds it, of a subscript, and of
// the elements of a tuple or list. Deleting an attribute removes it from the
// instance, which the layout of a class does not allow.
func (s *spec) deleteTarget(t ast.Expr, v *env) {
	switch t := t.(type) {
	case *ast.Name:
		obj := s.target(t)
		if obj == nil {
			return
		}
		s.c.info.Uses[t] = obj
		s.definedness(t, obj, v)
		if s.isInit() || s.ownedHere(obj) {
			v.vars[obj] = slot{typ: Typ[Never], def: Undefined}
			s.guard(obj)
		}
	case *ast.Subscript:
		recv := s.expr(t.Value, v)
		key := s.expr(t.Slice, v)
		s.delitem(t, recv, key)
	case *ast.Attribute:
		s.expr(t.Value, v)
	case *ast.Tuple:
		for _, e := range t.Elts {
			s.deleteTarget(e, v)
		}
	case *ast.List:
		for _, e := range t.Elts {
			s.deleteTarget(e, v)
		}
	default:
	}
}

func (s *spec) leave(v *env, add func(*loop, *env)) {
	if len(s.loops) > 0 {
		l := s.loops[len(s.loops)-1]
		add(l, v.clone())
		s.exit(v, l.depth)
	}
	v.live = false
}

func (s *spec) assignStmt(st *ast.Assign, v *env) {
	t := s.expr(st.Value, v)
	for _, target := range st.Targets {
		s.assign(target, t, st.Value, v)
	}
}

func (s *spec) annAssign(st *ast.AnnAssign, v *env) {
	if st.Value == nil {
		return
	}
	t := s.expr(st.Value, v)
	s.assign(st.Target, t, st.Value, v)
}

// assign binds target to a value of type t. value is the expression that
// produced it, used to unpack a tuple display without building the tuple.
func (s *spec) assign(target ast.Expr, t Type, value ast.Expr, v *env) {
	switch target := target.(type) {
	case *ast.Name:
		s.record(target, t)
		s.bindName(target, t, v)
	case *ast.Tuple:
		s.record(target, t)
		s.unpack(target, target.Elts, t, v)
	case *ast.List:
		s.record(target, t)
		s.unpack(target, target.Elts, t, v)
	case *ast.Subscript:
		s.record(target, t)
		recv := s.expr(target.Value, v)
		key := s.expr(target.Slice, v)
		s.setitem(target, recv, key, t)
	case *ast.Attribute:
		s.storeAttribute(target, t, v)
	default:
	}
}

// unpack binds the targets of a tuple or list target to the items of a value
// of type t. At most one target is starred and takes a list of the rest.
func (s *spec) unpack(node ast.Expr, targets []ast.Expr, t Type, v *env) {
	star := -1
	for i, x := range targets {
		if _, ok := x.(*ast.Starred); ok {
			star = i
		}
	}
	elems, rest := s.unpackTypes(node, targets, star, t)
	if star >= 0 {
		rest = s.starList(targets[star], rest)
	}
	for i, x := range targets {
		if st, ok := x.(*ast.Starred); ok {
			s.assign(st.Value, rest, nil, v)
			s.record(st, rest)
			continue
		}
		s.assign(x, elems[i], nil, v)
	}
}

// unpackTypes returns the type each target takes and, for a starred target,
// the element type of what it collects.
func (s *spec) unpackTypes(node ast.Expr, targets []ast.Expr, star int, t Type) ([]Type, Type) {
	n := len(targets)
	elems := make([]Type, n)
	tup, isTuple := Unalias(t).(*Tuple)
	switch {
	case isTuple && tup.unbounded < 0 && (star < 0 && tup.Len() == n || star >= 0 && tup.Len() >= n-1):
		var rest Type = Typ[Never]
		tail := n - star - 1
		for i, x := range targets {
			switch {
			case star < 0 || i < star:
				elems[i] = tup.vars[i].typ
			case i == star:
				for _, e := range tup.vars[star : tup.Len()-tail] {
					rest = joinCap(rest, Default(e.typ))
				}
			default:
				elems[i] = tup.vars[tup.Len()-(n-i)].typ
			}
			_ = x
		}
		return elems, rest
	case isNever(t):
		fillNever(elems)
		return elems, Typ[Never]
	case isTuple && tup.unbounded < 0:
		s.fall(node, reasonDynamicItem, "this unpacking has the wrong number of items")
	default:
		item := Default(s.iterate(Site{node, SiteIter, 0}, Site{node, SiteNext, 0}, t))
		for i := range elems {
			elems[i] = item
		}
		return elems, item
	}
	fillAny(elems)
	return elems, Typ[Any]
}

func fillNever(ts []Type) {
	for i := range ts {
		ts[i] = Typ[Never]
	}
}

// starList is the list a starred target takes: a new list of the items.
func (s *spec) starList(target ast.Expr, item Type) Type {
	return s.newContainer(target, "list", item)
}

// bindName assigns t to the variable a name binds, checking it against the
// annotation of the variable.
func (s *spec) bindName(n *ast.Name, t Type, v *env) {
	s.bindTo(s.target(n), n, t, v)
}

// bindTo assigns t to obj, the variable that the node at binds.
func (s *spec) bindTo(obj Object, n ast.Node, t Type, v *env) {
	o, ok := obj.(*Var)
	if !ok {
		if obj != nil {
			v.bind(obj, Typ[Any])
		}
		return
	}
	stored := capType(Default(t))
	if o.annot {
		constrain(t, o.typ)
		if !AssignableTo(t, o.typ) {
			s.fail(n, "TYP140", fmt.Sprintf("value of type %s is not assignable to %s", TypeString(Default(t), nil), TypeString(o.typ, nil)))
		}
		stored = o.typ
		if _, isUnion := o.typ.(*Union); isUnion && AssignableTo(t, o.typ) {
			stored = Default(t)
		}
	}
	if isCell(o) {
		if e := s.owner(o); e != nil {
			s.storeCell(e, o, stored)
		}
	}
	if o.kind == GlobalVar || s.ownedHere(o) {
		s.bindObject(o, stored, v)
	}
	if o.kind != GlobalVar && s.ownedHere(o) {
		varType := stored
		if o.annot {
			varType = o.typ
		}
		s.vars[o] = joinCap(orNever(s.vars[o]), varType)
	}
}

func orNever(t Type) Type {
	if t == nil {
		return Typ[Never]
	}
	return t
}

// inplace resolves x op= y: the in-place method of x if it has one, else the
// binary operation.
func (s *spec) inplace(st *ast.AugAssign, l, r Type, names [2]string) Type {
	if !isAny(l) && !isAny(r) && len(unionTerms(l)) == 1 {
		name := "__i" + names[0] + "__"
		if res, call, ok := s.userMethodTry(st, l, name, []argv{{t: r}}, true); ok {
			s.calls[st] = call
			return res
		}
		if sel := LookupAttr(Default(l), name); sel != nil {
			if res, idx, ok := matchType(sel.typ, []argv{{t: r}}); ok {
				s.calls[st] = &Call{Kind: Static, Func: funcOf(sel), Overload: idx, Slot: name}
				return res
			}
		}
	}
	return s.dispatch(st, l, r, names[0], names[1])
}

func (s *spec) returnStmt(st *ast.Return, v *env) {
	var t Type = builtin("NoneType")
	if st.Value != nil {
		t = s.expr(st.Value, v)
	}
	if s.declRet != nil && !s.fn.gen && !AssignableTo(t, s.declRet) {
		s.fail(st, "TYP140", fmt.Sprintf("return value of type %s is not assignable to %s", TypeString(Default(t), nil), TypeString(s.declRet, nil)))
	}
	s.ret = joinCap(s.ret, Default(t))
	s.exit(v, 0)
	v.live = false
}

// iterate resolves the iteration protocol of an iterable and returns the type
// of the items. The __iter__ call is recorded at iterKey and the __next__ call
// at nextKey.
func (s *spec) iterate(iterKey, nextKey ast.Node, t Type) Type {
	if isAny(t) {
		return s.dynamic(iterKey, "__iter__", "iteration over Any", t)
	}
	if isNever(t) {
		return t
	}
	if it, call, ok := s.userMethod(iterKey, t, "__iter__", nil); ok {
		s.calls[iterKey] = call
		return s.nextItem(nextKey, it)
	}
	sel := LookupAttr(Default(t), "__iter__")
	if sel == nil || len(unionTerms(t)) > 1 {
		s.dynamic(iterKey, "__iter__", "the value is not iterable")
		return Typ[Any]
	}
	it, idx, ok := matchType(sel.typ, nil)
	if !ok {
		s.dynamic(iterKey, "__iter__", "the value is not iterable")
		return Typ[Any]
	}
	s.calls[iterKey] = &Call{Kind: Static, Func: funcOf(sel), Overload: idx, Slot: "__iter__"}
	return s.nextItem(nextKey, it)
}

// nextItem is the item type of the iterator an __iter__ returned.
func (s *spec) nextItem(nextKey ast.Node, it Type) Type {
	if item, call, ok := s.userMethod(nextKey, it, "__next__", nil); ok {
		s.calls[nextKey] = call
		return item
	}
	next := LookupAttr(it, "__next__")
	if next == nil {
		return s.dynamic(nextKey, "__next__", "the iterator has no __next__")
	}
	item, idx, ok := matchType(next.typ, nil)
	if !ok {
		return s.dynamic(nextKey, "__next__", "the iterator has no __next__")
	}
	s.calls[nextKey] = &Call{Kind: Static, Func: funcOf(next), Overload: idx, Slot: "__next__"}
	return norm(item)
}

func (s *spec) funcDef(st *ast.FunctionDef, v *env) {
	obj, ok := s.c.info.Defs[st].(*Func)
	if !ok {
		s.clobber(st, v)
		return
	}
	if fn := s.c.funcOf(obj); fn != nil {
		s.defaults(fn, st.Args, v)
		s.noteCaptures(fn, v)
	}
	if hasDecorators(obj) {
		s.decorate(st, obj, v)
		return
	}
	if isCell(obj) {
		s.storeCell(s, obj, s.valueOfFunc(obj))
	}
	v.bind(obj, Typ[Any])
}

// clobber stands in for a statement that is not inferred yet: what it can
// bind becomes Any, and its returns widen the result.
func (s *spec) clobber(st ast.Stmt, v *env) {
	ast.Inspect(st, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Name:
			if n.Ctx == ast.CtxStore {
				s.bindAny(n, v)
			}
		case *ast.Return:
			s.ret = joinCap(s.ret, Typ[Any])
		case *ast.FunctionDef, *ast.ClassDef:
			if obj := s.c.info.Defs[n]; obj != nil {
				v.bind(obj, Typ[Any])
			}
			return false
		}
		return true
	})
}

func (s *spec) bindAny(n *ast.Name, v *env) {
	if obj := s.target(n); obj != nil {
		s.bindObject(obj, Typ[Any], v)
	}
}

func (s *spec) bindObject(obj Object, t Type, v *env) {
	if o, ok := obj.(*Var); ok && o.kind == GlobalVar {
		s.c.store(o, t)
	}
	v.bind(obj, t)
}

// target returns the object a name binds.
func (s *spec) target(n *ast.Name) Object {
	if obj := s.c.info.Defs[n]; obj != nil {
		return obj
	}
	return s.c.info.Uses[n]
}

// defaults evaluates the default values of a function where it is defined
// and remembers their types for the calls that use them.
func (s *spec) defaults(fn *fn, a *ast.Arguments, v *env) {
	params := fn.sig.params.slice()
	byName := map[string]*Var{}
	for _, p := range params {
		byName[p.name] = p
	}
	positional := append(append([]*ast.Arg{}, a.PosOnlyArgs...), a.Args...)
	for i, d := range a.Defaults {
		s.defaultOf(fn, byName[positional[len(positional)-len(a.Defaults)+i].Arg], d, v)
	}
	for i, d := range a.KwDefaults {
		if d != nil {
			s.defaultOf(fn, byName[a.KwOnlyArgs[i].Arg], d, v)
		}
	}
}

func (s *spec) defaultOf(fn *fn, p *Var, d ast.Expr, v *env) {
	t := Default(s.expr(d, v))
	if p != nil && len(appendTypeParams(nil, p.typ)) == 0 && !AssignableTo(t, p.typ) {
		s.fail(d, "TYP140", fmt.Sprintf("default of type %s is not assignable to parameter %s of type %s", TypeString(t, nil), p.name, TypeString(p.typ, nil)))
	}
	if prev, ok := fn.defaults[p]; ok {
		t = joinCap(prev, t)
	}
	fn.defaults[p] = t
}

func (s *spec) importStmt(st *ast.Import, v *env) {
	for _, a := range st.Names {
		res, _ := s.c.info.Import(a)
		if res.Kind != ImportOK {
			v.live = false
			return
		}
		obj := s.c.info.Defs[a]
		if obj == nil {
			obj = s.c.info.Implicits[a]
		}
		if obj != nil {
			v.bind(obj, Typ[Any])
		}
	}
}

func (s *spec) importFrom(st *ast.ImportFrom, v *env) {
	res, _ := s.c.info.Import(st)
	if res.Kind != ImportOK {
		v.live = false
		return
	}
	for _, a := range st.Names {
		if a.Name == "*" {
			continue
		}
		if obj := s.c.info.Defs[a]; obj != nil {
			v.bind(obj, Typ[Any])
		}
	}
}

func joinAll(first *env, rest []*env) *env {
	out := first
	for _, e := range rest {
		out = joinEnvs(out, e)
	}
	return out
}

func fillAny(ts []Type) {
	for i := range ts {
		ts[i] = Typ[Any]
	}
}
