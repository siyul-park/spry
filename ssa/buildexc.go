package ssa

import (
	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/types"
)

// region is a try, finally or with statement that is being built. An
// instruction that can raise inside it has an exception edge to landing,
// whose landingpad passes the exception to handler. A region with exit runs
// it where a return, break or continue leaves the region, once for each
// target that is left: those exits share one block per target.
type region struct {
	landing, handler *Block
	exit             func()
	rets             *Block
	jumps            []regionJump
}

type regionJump struct {
	target *Block
	depth  int
	block  *Block
}

// split ends the block with the raising instruction in, which gets an
// exception edge to the landing block of r and a normal edge to the block
// that code continues in.
func (f *fbuild) split(in *Instr, r *region) {
	next := f.newBlock()
	in.Succs = make([]Edge, 1)
	f.addEdge(f.cur, &in.Succs[0], next, nil)
	in.Except = &Edge{}
	f.addEdge(f.cur, in.Except, r.landing, nil)
	f.seal(next)
	f.cur = next
}

// tryStmt lowers try with its clauses. The body is a region that catches; a
// finally clause is an outer region that also covers the handlers and the else
// clause, and its body is built again for each way out of the statement.
func (f *fbuild) tryStmt(st *ast.Try) {
	done := f.newBlock()
	var fin *region
	if len(st.FinalBody) > 0 {
		fin = f.openRegion(true)
		fin.exit = func() { f.stmts(st.FinalBody) }
	}
	var exc *region
	if len(st.Handlers) > 0 {
		exc = f.openRegion(true)
	}
	f.stmts(st.Body)
	if exc != nil {
		f.closeRegion(exc)
	}
	f.stmts(st.OrElse)
	f.jump(done)
	if exc != nil {
		f.start(exc.handler)
		f.handlers(st.Handlers, exc.handler.Params[0], done)
	}
	if fin != nil {
		f.closeRegion(fin)
		f.start(fin.handler)
		exc := fin.handler.Params[0]
		f.caught = append(f.caught, exc)
		f.stmts(st.FinalBody)
		f.caught = f.caught[:len(f.caught)-1]
		f.throw(OpReraise, exc)
		f.finishExits(fin)
	}
	f.merge(done)
	f.stmts(st.FinalBody)
}

// handlers builds the except clauses against the exception exc: the first one
// that matches runs and goes to done, and an exception no clause matches is
// raised again.
func (f *fbuild) handlers(list []*ast.ExceptHandler, exc Value, done *Block) {
	for _, h := range list {
		var next *Block
		if h.Type != nil {
			ok := f.newBlock()
			next = f.newBlock()
			f.branch(f.catches(h, exc), ok, next)
			f.seal(ok)
			f.start(ok)
		}
		var obj types.Object
		if h.Name != nil {
			obj = f.b.info.Defs[h]
			f.bindObject(obj, exc)
		}
		f.caught = append(f.caught, exc)
		f.stmts(h.Body)
		f.caught = f.caught[:len(f.caught)-1]
		if obj != nil && f.alive() {
			f.unbindObject(obj)
		}
		f.jump(done)
		if next == nil {
			return
		}
		f.seal(next)
		f.start(next)
	}
	f.throw(OpReraise, exc)
}

// catches tests whether exc is an instance of one of the classes of an except
// clause: a class or a tuple of them.
func (f *fbuild) catches(h *ast.ExceptHandler, exc Value) Value {
	classes, ok := f.b.catchClasses(f.typeOf(h.Type))
	if !ok {
		fn := f.b.helper("isinstance", []Type{tyAny, tyAny}, tyI1)
		return f.callFn(fn, []Value{exc, f.conv(f.expr(h.Type), tyAny)})
	}
	var acc Value = boolConst(false)
	for i, c := range classes {
		in := f.emit(OpIsInstance, tyI1, exc)
		if in == nil {
			return boolConst(false)
		}
		in.Class = c
		if i == 0 {
			acc = in
			continue
		}
		acc = f.val(OpSelect, tyI1, acc, boolConst(true), in)
	}
	return acc
}

// catchClasses lists the classes that the type of an except clause names.
func (b *builder) catchClasses(t types.Type) ([]*Class, bool) {
	var elems []types.Type
	switch t := t.(type) {
	case *types.Meta:
		elems = []types.Type{t.Elem()}
	case *types.Tuple:
		for i := range t.Len() {
			m, ok := t.At(i).Type().(*types.Meta)
			if !ok {
				return nil, false
			}
			elems = append(elems, m.Elem())
		}
	default:
		return nil, false
	}
	var out []*Class
	for _, e := range elems {
		n, ok := types.Unalias(e).(*types.Named)
		if !ok {
			return nil, false
		}
		out = append(out, b.classFor(n))
	}
	return out, true
}

// classFor returns the class of a named type: one of the program, or one of
// the builtins.
func (b *builder) classFor(n *types.Named) *Class {
	if isUserClass(n) {
		return b.classOf(n)
	}
	return b.class(n.Obj().Name())
}

// raiseStmt lowers raise: an instance or a class, with an optional cause, or
// the exception that the handler being built caught.
func (f *fbuild) raiseStmt(st *ast.Raise) {
	if st.Exc == nil {
		if n := len(f.caught); n > 0 {
			f.throw(OpReraise, f.caught[n-1])
			return
		}
		f.raise("RuntimeError", "No active exception to reraise")
		return
	}
	exc := f.exceptionValue(st.Exc)
	if st.Cause != nil {
		f.setCause(exc, f.exceptionValue(st.Cause))
	}
	f.throw(OpRaise, exc)
}

// exceptionValue evaluates an operand of raise: a class is instantiated.
func (f *fbuild) exceptionValue(e ast.Expr) Value {
	if call, ok := f.b.info.Synth[e]; ok {
		return f.expr(call)
	}
	return f.expr(e)
}

// setCause sets the cause of exc, and marks the context as suppressed.
func (f *fbuild) setCause(exc, cause Value) {
	set := func(name string, v Value) {
		t := TypeOf(exc)
		if fld, ok := classField(t, name); ok {
			if in := f.emit(OpStoreField, Type{}, exc, f.conv(v, fld.Type)); in != nil {
				in.Attr = name
			}
			return
		}
		if in := f.emit(OpDynSetAttr, Type{}, f.register(f.conv(exc, tyAny)), f.conv(v, tyAny)); in != nil {
			in.Attr = name
		}
	}
	set("__cause__", cause)
	set("__suppress_context__", boolConst(true))
}

// classField returns the field of the class a value of type t is an instance of.
func classField(t Type, name string) (Field, bool) {
	if t.Kind != KindClass || t.Nullable {
		return Field{}, false
	}
	return t.Class.field(name)
}

// assertStmt lowers assert: the message is built, and AssertionError raised,
// only where the test is false.
func (f *fbuild) assertStmt(st *ast.Assert) {
	ok, fail := f.newBlock(), f.newBlock()
	f.cond(st.Test, ok, fail)
	f.seal(ok)
	f.seal(fail)
	f.start(fail)
	if st.Msg != nil {
		f.raiseValue("AssertionError", f.convertValue('s', f.expr(st.Msg)))
	} else {
		f.raiseValue("AssertionError", nil)
	}
	f.start(ok)
}

// withStmt lowers with. Each item enters its manager and covers the rest with
// a region: the exit of the manager runs where the body is left, and an
// exception of the body goes to __exit__, which can swallow it.
func (f *fbuild) withStmt(items []*ast.WithItem, body []ast.Stmt) {
	if len(items) == 0 {
		f.stmts(body)
		return
	}
	item := items[0]
	mgr := f.expr(item.ContextExpr)
	entered := f.protocol(types.Site{Node: item.ContextExpr, Kind: types.SiteEnter}, "__enter__", mgr)
	if item.OptionalVars != nil {
		f.assign(item.OptionalVars, entered, nil)
	}
	exitNormal := f.spec.Calls[types.Site{Node: item.ContextExpr, Kind: types.SiteExit}]
	exitRaise := f.spec.Calls[types.Site{Node: item.ContextExpr, Kind: types.SiteExit, Index: 1}]
	none := noneConst()
	r := f.openRegion(true)
	r.exit = func() { f.protocolCall(exitNormal, "__exit__", mgr, none, none, none) }
	done := f.newBlock()
	f.withStmt(items[1:], body)
	f.closeRegion(r)
	if f.alive() {
		f.protocolCall(exitNormal, "__exit__", mgr, none, none, none)
		f.jump(done)
	}
	f.start(r.handler)
	exc := r.handler.Params[0]
	typ := f.val(OpTypeOf, tyAny, exc)
	tb := f.val(OpDynGetAttr, tyAny, exc)
	tb.(*Instr).Attr = "__traceback__"
	res := f.protocolCall(exitRaise, "__exit__", mgr, typ, exc, tb)
	if TypeOf(res).same(tyNone) {
		f.throw(OpReraise, exc)
	} else {
		swallow, rethrow := f.newBlock(), f.newBlock()
		f.branch(f.truthValue(res), swallow, rethrow)
		f.seal(swallow)
		f.seal(rethrow)
		f.start(swallow)
		f.jump(done)
		f.start(rethrow)
		f.throw(OpReraise, exc)
	}
	f.finishExits(r)
	f.merge(done)
}

// openRegion starts a region. A region that catches has a landing block and
// a handler block that takes the exception as an any.
func (f *fbuild) openRegion(catches bool) *region {
	r := &region{}
	if catches {
		r.landing, r.handler = f.newBlock(), f.newBlock(tyAny)
	}
	f.regions = append(f.regions, r)
	return r
}

// closeRegion ends the code of r: the landing block receives the exceptions
// that were raised in it, and the exits that left it get their code.
func (f *fbuild) closeRegion(r *region) {
	f.regions = f.regions[:len(f.regions)-1]
	if r.landing != nil {
		saved := f.cur
		f.start(r.landing)
		pad := f.val(OpLandingPad, tyAny)
		f.jump(r.handler, pad)
		f.seal(r.landing)
		f.seal(r.handler)
		f.cur = saved
	}
}

// finishExits builds the blocks through which returns, breaks and continues
// leave r, each running the exit of r and then going on outward.
func (f *fbuild) finishExits(r *region) {
	saved := f.cur
	if r.rets != nil {
		f.seal(r.rets)
		f.start(r.rets)
		r.exit()
		var v Value
		if len(r.rets.Params) > 0 {
			v = r.rets.Params[0]
		}
		f.leave(0, nil, v)
	}
	for _, j := range r.jumps {
		f.seal(j.block)
		f.start(j.block)
		r.exit()
		f.leave(j.depth, j.target, nil)
	}
	f.cur = saved
}

// leave returns v from the function when target is nil, and otherwise jumps
// to target, a block of a loop opened at depth. The exits of the regions it
// leaves run first, innermost first.
func (f *fbuild) leave(depth int, target *Block, v Value) {
	if !f.alive() {
		return
	}
	for i := len(f.regions) - 1; i >= depth; i-- {
		if r := f.regions[i]; r.exit != nil {
			var args []Value
			if v != nil {
				args = []Value{v}
			}
			f.jump(f.exitBlock(r, depth, target), args...)
			return
		}
	}
	switch {
	case target != nil:
		f.jump(target)
	case v != nil:
		f.end(OpRet, v)
	default:
		f.end(OpRet)
	}
}

// exitBlock returns the block through which a return or a jump to target
// leaves r.
func (f *fbuild) exitBlock(r *region, depth int, target *Block) *Block {
	if target == nil {
		if r.rets == nil {
			if res := f.fn.Result; !res.same(tyNone) {
				r.rets = f.newBlock(res)
			} else {
				r.rets = f.newBlock()
			}
		}
		return r.rets
	}
	for _, j := range r.jumps {
		if j.target == target && j.depth == depth {
			return j.block
		}
	}
	blk := f.newBlock()
	r.jumps = append(r.jumps, regionJump{target: target, depth: depth, block: blk})
	return blk
}

// throw raises v: it goes to the handler of the innermost region that has one,
// and leaves the function otherwise. op is OpRaise or OpReraise.
func (f *fbuild) throw(op Op, v Value) {
	if !f.alive() {
		return
	}
	if r := f.catcher(); r != nil {
		f.jump(r.handler, f.conv(v, tyAny))
		return
	}
	f.end(op, v)
}

// catcher is the innermost region that handles exceptions.
func (f *fbuild) catcher() *region {
	for i := len(f.regions) - 1; i >= 0; i-- {
		if f.regions[i].landing != nil {
			return f.regions[i]
		}
	}
	return nil
}

// protocol calls the method that an operation of the language dispatches to,
// as the checker resolved it at site.
func (f *fbuild) protocol(site types.Site, name string, recv Value, args ...Value) Value {
	return f.protocolCall(f.spec.Calls[site], name, recv, args...)
}

// protocolCall calls the method c resolved to: a method of a class of the
// program, one of the standard library, or a dynamic one.
func (f *fbuild) protocolCall(c *types.Call, name string, recv Value, args ...Value) Value {
	all := append([]Value{recv}, args...)
	switch {
	case c == nil:
		return f.undef(tyAny)
	case f.isUserCall(c):
		return f.invoke(c, all)
	case c.Kind == types.Dynamic || c.Func == nil:
		return f.dynMethod(recv, name, args...)
	}
	return f.callFn(f.b.externFunc(c.Func, c.Overload, false), all)
}

// deleteTarget lowers del of a name or a subscript, and of the elements of a
// tuple or list.
func (f *fbuild) deleteTarget(t ast.Expr) {
	switch t := t.(type) {
	case *ast.Name:
		obj := f.b.info.Uses[t]
		if obj == nil || !f.guard(t, obj) {
			return
		}
		f.unbindObject(obj)
	case *ast.Subscript:
		recv := f.expr(t.Value)
		k := f.indexOf(t.Slice)
		f.delitem(f.spec.Calls[types.Site{Node: t, Kind: types.SiteDelete}], recv, k)
	case *ast.Tuple:
		for _, e := range t.Elts {
			f.deleteTarget(e)
		}
	case *ast.List:
		for _, e := range t.Elts {
			f.deleteTarget(e)
		}
	}
}

// unbindObject ends the binding of a variable, as del does.
func (f *fbuild) unbindObject(obj types.Object) {
	if v, ok := obj.(*types.Var); ok && v.Kind() == types.GlobalVar {
		if in := f.emit(OpDelGlobal, Type{}); in != nil {
			in.Global = f.b.globalOf(v)
		}
	}
	f.setBound(obj, false)
	if !f.b.maybe[obj] {
		return
	}
	if f.isLocal(obj) {
		f.writeVar(bkey{obj: obj, bound: true}, f.cur, boolConst(false))
	} else if g := f.b.flags[obj]; g != nil {
		f.store(g, boolConst(false))
	}
}
