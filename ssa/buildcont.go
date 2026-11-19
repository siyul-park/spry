package ssa

import (
	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/types"
)

// indexKey is the subscript of a container: a value, or the bounds of a slice
// with None for the ones that are left out.
type indexKey struct {
	expr         ast.Expr
	val          Value
	lo, hi, step Value
	slice        bool
}

// subscript lowers x[k].
func (f *fbuild) subscript(e *ast.Subscript) Value {
	want := f.irType(e)
	recv := f.expr(e.Value)
	k := f.indexOf(e.Slice)
	return f.conv(f.getitem(f.spec.Calls[e], recv, k, want), want)
}

func (f *fbuild) deleteStmt(st *ast.Delete) {
	for _, t := range st.Targets {
		f.deleteTarget(t)
	}
}

// delitem lowers del recv[k].
func (f *fbuild) delitem(c *types.Call, recv Value, k indexKey) {
	if c == nil {
		return
	}
	rt := TypeOf(recv)
	plain := !k.slice && !rt.Nullable && (rt.Kind == KindList || rt.Kind == KindDict)
	switch {
	case f.isUserCall(c):
		f.invoke(c, []Value{recv, f.keyValue(k)})
	case c.Kind == types.Dynamic:
		f.dynMethod(recv, "__delitem__", f.keyValue(k))
	case plain && rt.Kind == KindDict && f.userEq(rt):
		f.dynMethod(recv, "__delitem__", k.val)
	case plain:
		keyT := tyInt
		if rt.Kind == KindDict {
			keyT = rt.at(0)
		}
		f.emit(OpDelIndex, Type{}, recv, f.conv(k.val, keyT))
	case c.Func != nil:
		f.callFn(f.b.externFunc(c.Func, c.Overload, false), []Value{recv, f.keyValue(k)})
	}
}

// storeSubscript assigns v to a subscript target.
func (f *fbuild) storeSubscript(t *ast.Subscript, v Value) {
	recv := f.expr(t.Value)
	k := f.indexOf(t.Slice)
	f.setitem(f.spec.Calls[types.Site{Node: t, Kind: types.SiteStore}], recv, k, v)
}

// augSubscript lowers recv[k] op= value, evaluating recv and k once.
func (f *fbuild) augSubscript(st *ast.AugAssign, t *ast.Subscript) {
	recv := f.expr(t.Value)
	k := f.indexOf(t.Slice)
	cur := f.getitem(f.spec.Calls[t], recv, k, f.irType(t))
	r := f.expr(st.Value)
	res := f.arith(st, st.Op, cur, f.typeOf(t), r, f.typeOf(st.Value), st.Value, Type{})
	f.setitem(f.spec.Calls[types.Site{Node: t, Kind: types.SiteStore}], recv, k, res)
}

// indexOf evaluates the subscript of a container.
func (f *fbuild) indexOf(e ast.Expr) indexKey {
	s, ok := e.(*ast.Slice)
	if !ok {
		return indexKey{expr: e, val: f.expr(e)}
	}
	return indexKey{slice: true, lo: f.bound(s.Lower), hi: f.bound(s.Upper), step: f.bound(s.Step)}
}

func (f *fbuild) bound(e ast.Expr) Value {
	if e == nil {
		return noneConst()
	}
	return f.conv(f.expr(e), tyInt)
}

func (f *fbuild) getitem(c *types.Call, recv Value, k indexKey, want Type) Value {
	if c == nil {
		return f.undef(want)
	}
	if c.Kind == types.Dynamic {
		return f.dynBinary("__getitem__", recv, f.keyValue(k))
	}
	if f.isUserCall(c) {
		return f.invoke(c, []Value{recv, f.keyValue(k)})
	}
	rt := TypeOf(recv)
	seq := rt.Kind == KindList || rt.Kind == KindStr || rt.Kind == KindBytes || rt.Kind == KindTuple && rt.Variadic
	switch {
	case rt.Kind == KindDict && !k.slice && f.userEq(rt):
		return f.dynBinary("__getitem__", recv, k.val)
	case rt.Nullable:
	case k.slice && seq:
		return f.val(OpSlice, rt, recv, k.lo, k.hi, k.step)
	case k.slice:
	case rt.Kind == KindTuple && !rt.Variadic:
		if cv, ok := f.constExpr(k.expr); ok {
			if i, ok := constIndex(cv, len(rt.Elems)); ok {
				return f.val(OpIndex, rt.Elems[i], recv, cv)
			}
		}
		return f.undef(want)
	case rt.Kind == KindDict:
		return f.val(OpIndex, rt.Elems[1], recv, f.conv(k.val, rt.Elems[0]))
	case seq:
		return f.val(OpIndex, elemOfIndex(rt), recv, f.conv(k.val, tyInt))
	}
	if c.Func == nil {
		return f.undef(want)
	}
	return f.callFn(f.b.externFunc(c.Func, c.Overload, false), []Value{recv, f.keyValue(k)})
}

// elemOfIndex is the type of an item of a sequence.
func elemOfIndex(t Type) Type {
	switch t.Kind {
	case KindStr:
		return t
	case KindBytes:
		return tyInt
	}
	return t.at(0)
}

// setitem lowers recv[k] = v; c is the call the checker recorded for it.
func (f *fbuild) setitem(c *types.Call, recv Value, k indexKey, v Value) {
	if c == nil {
		return
	}
	rt := TypeOf(recv)
	plain := !k.slice && !rt.Nullable && (rt.Kind == KindList || rt.Kind == KindDict)
	switch {
	case f.isUserCall(c):
		f.invoke(c, []Value{recv, f.keyValue(k), v})
	case c.Kind == types.Dynamic:
		f.dynMethod(recv, "__setitem__", f.keyValue(k), v)
	case plain && rt.Kind == KindDict && f.userEq(rt):
		f.dynMethod(recv, "__setitem__", k.val, v)
	case plain:
		elem, keyT := rt.at(0), tyInt
		if rt.Kind == KindDict {
			elem, keyT = rt.at(1), rt.at(0)
		}
		f.emit(OpSetIndex, Type{}, recv, f.conv(k.val, keyT), f.conv(v, elem))
	case c.Func != nil:
		f.callFn(f.b.externFunc(c.Func, c.Overload, false), []Value{recv, f.keyValue(k), v})
	}
}

// keyValue is the subscript as one value: a slice becomes a slice object.
func (f *fbuild) keyValue(k indexKey) Value {
	if k.slice {
		return f.sliceValue(k)
	}
	return k.val
}

// sliceValue makes the slice object of a subscript, for the operations that
// are calls of the standard library.
func (f *fbuild) sliceValue(k indexKey) Value {
	args := []Value{f.conv(k.lo, tyAny), f.conv(k.hi, tyAny), f.conv(k.step, tyAny)}
	return f.callFn(f.b.sliceFunc(), args)
}

func (b *builder) sliceFunc() *Function {
	return b.helper("slice", []Type{tyAny, tyAny, tyAny}, tyAny)
}

// comprehension lowers a comprehension in place: the loops of its generators
// fill a new container.
func (f *fbuild) comprehension(e ast.Expr) Value {
	var gens []*ast.Comprehension
	var elts []ast.Expr
	var acc Value
	switch e := e.(type) {
	case *ast.ListComp:
		gens, elts = e.Generators, []ast.Expr{e.Elt}
	case *ast.SetComp:
		gens, elts = e.Generators, []ast.Expr{e.Elt}
	case *ast.DictComp:
		gens, elts = e.Generators, []ast.Expr{e.Key, e.Value}
	case *ast.GeneratorExp:
		gens, elts = e.Generators, []ast.Expr{e.Elt}
	}
	want := f.irType(e)
	if _, ok := e.(*ast.GeneratorExp); ok {
		want = Type{Kind: KindList, Elems: []Type{f.irType(elts[0])}}
	}
	switch want.Kind {
	case KindList:
		acc = f.val(OpNewList, want)
	case KindSet:
		acc = f.val(OpNewSet, want)
	case KindDict:
		acc = f.val(OpNewDict, want)
	default:
		return f.undef(want)
	}
	if sc := f.b.info.Scopes[e]; sc != nil {
		for _, obj := range cellsOf(f.spec) {
			if obj.Parent() == sc {
				f.newCell(obj)
			}
		}
	}
	f.compLoop(gens, 0, func() { f.collect(acc, want, elts) })
	return acc
}

// collect adds the value of a comprehension item to its container.
func (f *fbuild) collect(acc Value, t Type, elts []ast.Expr) {
	switch t.Kind {
	case KindList:
		f.emit(OpAppend, Type{}, acc, f.conv(f.expr(elts[0]), t.Elems[0]))
	case KindSet:
		if f.userEq(t) {
			f.dynMethod(acc, "add", f.expr(elts[0]))
			return
		}
		f.callFn(f.b.setAdd(t), []Value{acc, f.expr(elts[0])})
	case KindDict:
		k := f.conv(f.expr(elts[0]), t.Elems[0])
		if f.userEq(t) {
			f.dynMethod(acc, "__setitem__", k, f.conv(f.expr(elts[1]), t.Elems[1]))
			return
		}
		f.emit(OpSetIndex, Type{}, acc, k, f.conv(f.expr(elts[1]), t.Elems[1]))
	}
}

func (b *builder) setAdd(t Type) *Function {
	return b.helper("set.add", []Type{t, t.Elems[0]}, tyNone)
}

// helper declares an extern function of the builtins that the lowering of a
// construct calls: the name and the types identify it.
func (b *builder) helper(name string, params []Type, result Type) *Function {
	key := name + "|" + result.String()
	for _, p := range params {
		key += "|" + p.String()
	}
	if fn := b.helpers[key]; fn != nil {
		return fn
	}
	mod := b.builtins()
	label := b.unique(mod, name)
	ps := make([]*Param, len(params))
	for i, t := range params {
		ps[i] = &Param{Name: string(rune('a' + i)), Typ: t}
	}
	fn := &Function{Name: label, Source: name, Module: mod, Params: ps, Result: result}
	mod.Functions = append(mod.Functions, fn)
	b.helpers[key] = fn
	return fn
}

// dynMethod calls a method of a value of type any.
func (f *fbuild) dynMethod(recv Value, name string, args ...Value) Value {
	m := f.emit(OpDynGetAttr, tyAny, f.register(f.conv(recv, tyAny)))
	if m == nil {
		return f.undef(tyAny)
	}
	m.Attr = name
	vals := []Value{m}
	for _, a := range args {
		vals = append(vals, f.conv(a, tyAny))
	}
	return f.val(OpDynCall, tyAny, vals...)
}

// compLoop runs the generators from i on, and emit for each combination of
// their items that passes the conditions.
func (f *fbuild) compLoop(gens []*ast.Comprehension, i int, emit func()) {
	if i == len(gens) {
		emit()
		return
	}
	g := gens[i]
	item := f.irType(g.Target)
	it := f.preIter
	if i == 0 {
		f.preIter = nil
	}
	if i > 0 || it == nil {
		it = f.iterValue(types.Site{Node: g.Iter, Kind: types.SiteIter}, f.expr(g.Iter), item)
	}
	head, exit, cur := f.items(types.Site{Node: g.Iter, Kind: types.SiteNext}, it, item)
	f.assign(g.Target, cur, nil)
	for _, c := range g.Ifs {
		pass := f.newBlock()
		f.cond(c, pass, head)
		f.seal(pass)
		f.start(pass)
	}
	f.compLoop(gens, i+1, emit)
	f.jump(head)
	f.seal(head)
	f.merge(exit)
}

func (f *fbuild) dict(e *ast.Dict) Value {
	want := f.irType(e)
	if want.Kind != KindDict || want.Nullable {
		return f.undef(want)
	}
	var vals []Value
	for i, k := range e.Keys {
		if k == nil {
			return f.undef(want)
		}
		vals = append(vals, f.conv(f.expr(k), want.Elems[0]), f.conv(f.expr(e.Values[i]), want.Elems[1]))
	}
	if f.userEq(want) {
		var entries [][]Value
		for i := 0; i < len(vals); i += 2 {
			entries = append(entries, vals[i:i+2])
		}
		return f.dynItems(OpNewDict, want, "__setitem__", entries)
	}
	return f.val(OpNewDict, want, vals...)
}

func (f *fbuild) set(e *ast.Set) Value {
	want := f.irType(e)
	if want.Kind != KindSet || want.Nullable {
		return f.undef(want)
	}
	vals := make([]Value, len(e.Elts))
	for i, x := range e.Elts {
		vals[i] = f.conv(f.expr(x), want.Elems[0])
	}
	if f.userEq(want) {
		entries := make([][]Value, len(vals))
		for i, v := range vals {
			entries[i] = []Value{v}
		}
		return f.dynItems(OpNewSet, want, "add", entries)
	}
	return f.val(OpNewSet, want, vals...)
}

// list lowers a list display.
func (f *fbuild) list(e *ast.List) Value {
	want := f.irType(e)
	if want.Kind != KindList || want.Nullable {
		return f.undef(want)
	}
	elem := want.Elems[0]
	star := false
	for _, x := range e.Elts {
		_, isStar := x.(*ast.Starred)
		star = star || isStar
	}
	if !star {
		vals := make([]Value, len(e.Elts))
		for i, x := range e.Elts {
			vals[i] = f.conv(f.expr(x), elem)
		}
		return f.val(OpNewList, want, vals...)
	}
	acc := f.val(OpNewList, want)
	for _, x := range e.Elts {
		if st, ok := x.(*ast.Starred); ok {
			f.forEach(types.Site{Node: st, Kind: types.SiteIter}, types.Site{Node: st, Kind: types.SiteNext}, f.expr(st.Value), elem, func(cur Value) {
				f.emit(OpAppend, Type{}, acc, f.conv(cur, elem))
			})
			continue
		}
		f.emit(OpAppend, Type{}, acc, f.conv(f.expr(x), elem))
	}
	return acc
}

// walrus lowers (name := value).
func (f *fbuild) walrus(e *ast.NamedExpr) Value {
	v := f.expr(e.Value)
	if n, ok := e.Target.(*ast.Name); ok {
		f.bindName(n, v)
	}
	return v
}

// unpack stores the items of v in the targets of a tuple or list target. A
// starred target takes a list of the items the others leave.
func (f *fbuild) unpack(targets []ast.Expr, v Value) {
	star := -1
	for i, x := range targets {
		if _, ok := x.(*ast.Starred); ok {
			star = i
		}
	}
	if t := TypeOf(v); t.Kind == KindTuple && !t.Nullable && !t.Variadic {
		f.unpackTuple(targets, star, v)
		return
	}
	if star < 0 {
		var elems []Type // nil when empty: the form Parse reads back
		for _, x := range targets {
			elems = append(elems, f.irType(x))
		}
		u := f.val(OpUnpack, Type{Kind: KindTuple, Elems: elems}, v)
		for i, x := range targets {
			f.assign(x, f.val(OpIndex, elems[i], u, intConst(int64(i))), nil)
		}
		return
	}
	f.unpackStar(targets, star, v)
}

// unpackTuple unpacks a tuple whose items are known by position.
func (f *fbuild) unpackTuple(targets []ast.Expr, star int, v Value) {
	t := TypeOf(v)
	n := len(targets)
	if star < 0 && len(t.Elems) != n || star >= 0 && len(t.Elems) < n-1 {
		f.raise("ValueError", "wrong number of values to unpack")
		return
	}
	tail := n - star - 1
	for i, x := range targets {
		switch {
		case star < 0 || i < star:
			f.assign(x, f.val(OpIndex, t.Elems[i], v, intConst(int64(i))), nil)
		case i == star:
			f.assign(x.(*ast.Starred).Value, f.tupleRest(x, v, star, len(t.Elems)-tail), nil)
		default:
			at := len(t.Elems) - (n - i)
			f.assign(x, f.val(OpIndex, t.Elems[at], v, intConst(int64(at))), nil)
		}
	}
}

// tupleRest is the list of the items of a tuple from lo to hi.
func (f *fbuild) tupleRest(x ast.Expr, v Value, lo, hi int) Value {
	lt := f.irType(x)
	t := TypeOf(v)
	var vals []Value
	for i := lo; i < hi && lt.Kind == KindList; i++ {
		vals = append(vals, f.conv(f.val(OpIndex, t.Elems[i], v, intConst(int64(i))), lt.Elems[0]))
	}
	return f.val(OpNewList, lt, vals...)
}

// unpackStar unpacks a sequence of unknown length into targets, one of which
// is starred.
func (f *fbuild) unpackStar(targets []ast.Expr, star int, v Value) {
	lt := f.irType(targets[star])
	if lt.Kind != KindList {
		return
	}
	list := v
	if !TypeOf(v).same(lt) {
		list = f.val(OpNewList, lt)
		f.forEach(types.Site{Node: targets[star], Kind: types.SiteIter}, types.Site{Node: targets[star], Kind: types.SiteNext}, v, lt.Elems[0], func(cur Value) {
			f.emit(OpAppend, Type{}, list, f.conv(cur, lt.Elems[0]))
		})
	}
	n, tail := len(targets), len(targets)-star-1
	size := f.val(OpLen, tyInt, list)
	ok, fail := f.newBlock(), f.newBlock()
	f.branch(f.val(OpGe, tyI1, size, intConst(int64(n-1))), ok, fail)
	f.seal(ok)
	f.seal(fail)
	f.start(fail)
	f.raise("ValueError", "not enough values to unpack")
	f.start(ok)
	for i, x := range targets {
		switch {
		case i < star:
			f.assign(x, f.val(OpIndex, lt.Elems[0], list, intConst(int64(i))), nil)
		case i == star:
			end := f.val(OpSub, tyInt, size, intConst(int64(tail)))
			f.assign(x.(*ast.Starred).Value, f.val(OpSlice, lt, list, intConst(int64(star)), end, noneConst()), nil)
		default:
			at := f.val(OpSub, tyInt, size, intConst(int64(n-i)))
			f.assign(x, f.val(OpIndex, lt.Elems[0], list, at), nil)
		}
	}
}

func noneConst() *Const {
	return &Const{Value: ast.None{}, Typ: tyNone}
}

// forEach runs body for each item of v, in a loop. The checker resolved the
// iteration of v at iterKey and nextKey.
func (f *fbuild) forEach(iterKey, nextKey ast.Node, v Value, item Type, body func(cur Value)) {
	it := f.iterValue(iterKey, v, item)
	head, exit, cur := f.items(nextKey, it, item)
	body(cur)
	f.jump(head)
	f.seal(head)
	f.merge(exit)
}

// iterValue makes the iterator of v over items of type item: the __iter__ of a
// class of the program, or an iterator or generator, which is its own.
func (f *fbuild) iterValue(iterKey ast.Node, v Value, item Type) Value {
	if c := f.spec.Calls[iterKey]; f.isUserCall(c) {
		v = f.invoke(c, []Value{v})
		if k := TypeOf(v).Kind; k == KindClass || k == KindIter || k == KindGen {
			return v
		}
	}
	return f.iterOf(v, item)
}

// iterOf makes the iterator of v over items of type item: an iterator is its
// own.
func (f *fbuild) iterOf(v Value, item Type) Value {
	if t := TypeOf(v); (t.Kind == KindIter || t.Kind == KindGen) && !t.Nullable {
		return v
	}
	return f.val(OpIter, Type{Kind: KindIter, Elems: []Type{item}}, v)
}
