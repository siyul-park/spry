package ssa

import (
	"slices"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/token"
	"github.com/siyul-park/spry/types"
)

// captureSlot is one parameter of a nested function that takes a captured
// variable: its cell, or the cell that tells whether it is bound.
type captureSlot struct {
	obj  types.Object
	flag bool
}

var tyBoundCell = Type{Kind: KindCell, Elems: []Type{tyI1}}

// lambda lowers a lambda expression to a closure.
func (f *fbuild) lambda(e *ast.Lambda) Value {
	obj, ok := f.b.info.Defs[e].(*types.Func)
	if !ok {
		return f.undef(f.irType(e))
	}
	f.storeDefaults(obj, e.Args)
	return f.conv(f.makeClosure(obj, f.spec), f.irType(e))
}

// callValue calls a closure: the callee is a value of a function type, and
// the arguments are bound to the parameters of the function it holds. When it
// may hold one of several functions the arguments are positional.
func (f *fbuild) callValue(e *ast.Call, c *types.Call) Value {
	want := f.irType(e)
	ft := f.irType(e.Func)
	callee := f.conv(f.expr(e.Func), ft)
	if ft.Kind != KindFunc || ft.Nullable {
		f.b.note(f.b.fset.Position(e.Pos()), f.b.fset.Position(e.End()), "union", "the callee is one of several functions")
		return f.conv(f.dynCallOf(callee, e), want)
	}
	n := len(ft.Elems) - 1
	var args []Value
	if c.Target != nil {
		fn := c.Target.Func
		sig := fn.Signature()
		params := make([]*types.Var, sig.Params().Len())
		for i := range params {
			params[i] = sig.Params().At(i)
		}
		if len(params) != n {
			return f.undef(want)
		}
		args = f.bindArgs(e, params, ft.Elems[:n], func(i int) Value {
			return f.defaultArg(fn, params[i], ft.Elems[i])
		})
	} else {
		if len(e.Args) != n || len(e.Keywords) > 0 {
			return f.undef(want)
		}
		for i, a := range e.Args {
			args = append(args, f.conv(f.expr(a), ft.Elems[i]))
		}
	}
	in := f.emit(OpCallClosure, ft.Elems[n], append([]Value{callee}, args...)...)
	if in == nil {
		return f.undef(want)
	}
	return f.conv(in, want)
}

// dynCallOf calls a value of any with the arguments of e.
func (f *fbuild) dynCallOf(callee Value, e *ast.Call) Value {
	args := make([]Value, 0, len(e.Args))
	for _, a := range e.Args {
		args = append(args, f.conv(f.expr(a), tyAny))
	}
	in := f.emit(OpDynCall, tyAny, append([]Value{f.register(f.conv(callee, tyAny))}, args...)...)
	if in == nil {
		return f.undef(tyAny)
	}
	return in
}

// funcName reads a name that denotes a function: a variable that holds a
// nested function, or a function of the module made into a value.
func (f *fbuild) funcName(n *ast.Name, obj *types.Func) Value {
	want := f.irType(n)
	if g := f.b.decoratedGlobal(obj); g != nil {
		return f.conv(f.load(g), want)
	}
	if !isNestedFunc(obj) {
		return f.conv(f.makeClosure(obj, nil), want)
	}
	if c := f.cells[obj]; c != nil {
		f.checkCaptured(n, obj)
		return f.conv(f.loadCell(c), want)
	}
	if _, ok := f.vtypes[obj]; ok {
		return f.conv(f.readVar(bkey{obj: obj}, f.cur), want)
	}
	return f.undef(want)
}

// openCells makes the cells the function owns, and takes the ones it
// captures from its parameters. A variable of a comprehension is given its
// cell where the comprehension starts.
func (f *fbuild) openCells() {
	f.cells = map[types.Object]Value{}
	f.flags = map[types.Object]Value{}
	if f.spec == nil {
		return
	}
	for i, slot := range captureSlots(f.spec) {
		if i >= len(f.fn.Captures) {
			break
		}
		if slot.flag {
			f.flags[slot.obj] = f.fn.Captures[i]
		} else {
			f.cells[slot.obj] = f.fn.Captures[i]
		}
	}
	for _, obj := range cellsOf(f.spec) {
		if p := obj.Parent(); p != nil && p.Kind() == types.ComprehensionScope {
			continue
		}
		f.newCell(obj)
	}
}

// newCell allocates the cell of obj, holding the parameter that it is when
// it is one and an undefined value otherwise. A variable that a nested
// function may read before it is bound has a second cell that tells whether
// it is.
func (f *fbuild) newCell(obj types.Object) {
	t := f.b.lower(f.spec.Cells[obj])
	var init Value = f.undef(t)
	isParam := false
	if p, ok := obj.(*types.Var); ok && p.Kind() == types.ParamVar {
		if v := f.readVar(bkey{obj: p}, f.cur); v != nil {
			init, isParam = f.conv(v, t), true
		}
	}
	f.cells[obj] = f.val(OpNewCell, Type{Kind: KindCell, Elems: []Type{t}}, init)
	if f.spec.Guarded[obj] {
		f.flags[obj] = f.val(OpNewCell, tyBoundCell, boolConst(isParam))
	}
}

// checkCaptured raises NameError where a variable of an enclosing function
// that is not bound is read.
func (f *fbuild) checkCaptured(n *ast.Name, obj types.Object) {
	flag := f.flags[obj]
	if flag == nil || f.ownsVar(obj) {
		return
	}
	ok, fail := f.newBlock(), f.newBlock()
	f.branch(f.loadCell(flag), ok, fail)
	f.seal(ok)
	f.seal(fail)
	f.start(fail)
	f.raise("NameError", "cannot access free variable '"+n.ID+"' where it is not associated with a value in enclosing scope")
	f.start(ok)
}

// loadCell reads the variable of a cell.
func (f *fbuild) loadCell(c Value) Value {
	return f.val(OpLoadCell, TypeOf(c).at(0), c)
}

// declareFuncs gives the variable of each function defined in body, which is
// a variable of the function being built, the type of its closure.
func (f *fbuild) declareFuncs(body []ast.Stmt) {
	for _, st := range body {
		ast.Inspect(st, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FunctionDef:
				if obj, ok := f.b.info.Defs[n].(*types.Func); ok && f.spec.Cells[obj] == nil {
					if len(n.DecoratorList) > 0 && f.b.info.Synth[n.DecoratorList[0]] != nil {
						f.vtypes[obj] = f.irType(f.b.info.Synth[n.DecoratorList[0]])
					} else if g := f.b.valueSpec(obj, f.spec); g != nil && f.b.funcs[g] != nil {
						f.vtypes[obj] = funcType(f.b.funcs[g])
					}
				}
				return false
			case *ast.ClassDef, *ast.Lambda:
				return false
			}
			return true
		})
	}
}

// decoratedDef lowers a def with decorators: the function object is passed
// through them, nearest the def first, and the name is bound to the result.
func (f *fbuild) decoratedDef(obj *types.Func, st *ast.FunctionDef) {
	var env *types.Spec
	if isNestedFunc(obj) {
		env = f.spec
	}
	v := f.makeClosure(obj, env)
	for i := len(st.DecoratorList) - 1; i >= 0; i-- {
		call := f.b.info.Synth[st.DecoratorList[i]]
		if call == nil {
			return
		}
		f.known[call.Args[0]] = v
		v = f.expr(call)
		delete(f.known, call.Args[0])
	}
	if isNestedFunc(obj) {
		f.bindFunc(obj, v)
		return
	}
	if g := f.b.decoratedGlobal(obj); g != nil {
		f.store(g, f.conv(v, g.Type))
	}
	f.markBound(obj)
}

// bindFunc binds a variable that holds a function to its closure.
func (f *fbuild) bindFunc(obj *types.Func, v Value) {
	if c := f.cells[obj]; c != nil {
		f.storeCell(c, v)
		f.setBound(obj, true)
	} else if t, ok := f.vtypes[obj]; ok {
		conv := f.conv(v, t)
		f.writeVar(bkey{obj: obj}, f.cur, conv)
	}
	f.markBound(obj)
}

// makeClosure creates the closure of a function defined in env, capturing
// the cells it uses.
func (f *fbuild) makeClosure(fn *types.Func, env *types.Spec) Value {
	g := f.b.valueSpec(fn, env)
	target := f.b.funcs[g]
	if g == nil || target == nil {
		return f.undef(tyAny)
	}
	var caps []Value
	for _, slot := range captureSlots(g) {
		c := f.cells[slot.obj]
		if slot.flag {
			c = f.flags[slot.obj]
		}
		if c == nil {
			c = f.undef(target.Captures[len(caps)].Typ)
		}
		caps = append(caps, c)
	}
	in := f.emit(OpMakeClosure, funcType(target), caps...)
	if in == nil {
		return f.undef(funcType(target))
	}
	in.Callee = target
	return in
}

// setBound records in the flag cell of obj, if it has one, whether the
// variable is bound.
func (f *fbuild) setBound(obj types.Object, bound bool) {
	if c := f.flags[obj]; c != nil {
		f.emit(OpStoreCell, Type{}, c, boolConst(bound))
	}
}

// storeCell writes the variable of a cell.
func (f *fbuild) storeCell(c, v Value) {
	f.emit(OpStoreCell, Type{}, c, f.conv(v, TypeOf(c).at(0)))
}

// note records a fallback the checker could not foresee: an operation the
// lowering made dynamic because a type has no IR form.
func (b *builder) note(pos, end token.Position, reason, msg string) {
	b.info.Fallbacks = append(b.info.Fallbacks, types.Fallback{Pos: pos, End: end, Reason: reason, Msg: msg})
}

// lowerFuncUnion lowers a union of closures: the function type they share,
// or any.
func (b *builder) lowerFuncUnion(t *types.Union) (Type, bool) {
	var out Type
	for i := range t.Len() {
		cl, ok := t.Term(i).(*types.FuncValue)
		if !ok {
			return Type{}, false
		}
		ft := b.lowerClosure(cl)
		if i > 0 && !ft.same(out) {
			return tyAny, true
		}
		out = ft
	}
	return out, true
}

// lowerClosure is the function type of a closure. A closure whose version is
// being lowered, as the parameter of that version is, is any: the type would
// contain itself.
func (b *builder) lowerClosure(cl *types.FuncValue) Type {
	g := b.valueSpec(cl.Func(), cl.Env())
	if g == nil || b.lowering[g] {
		return tyAny
	}
	b.lowering[g] = true
	defer delete(b.lowering, g)
	ps := b.params(g)
	elems := make([]Type, 0, len(ps)+1)
	for _, p := range ps {
		elems = append(elems, p.Typ)
	}
	return Type{Kind: KindFunc, Elems: append(elems, b.lower(resultOf(g)))}
}

// captures lists the cells a specialization of a nested function takes.
func (b *builder) captures(s *types.Spec) []*Param {
	var out []*Param
	for _, slot := range captureSlots(s) {
		if slot.flag {
			out = append(out, &Param{Name: paramName(slot.obj.Name()) + "$bound", Typ: tyBoundCell})
			continue
		}
		t := b.lower(ownerCell(s, slot.obj))
		out = append(out, &Param{Name: paramName(slot.obj.Name()), Typ: Type{Kind: KindCell, Elems: []Type{t}}})
	}
	return out
}

// ownerCell is the type of the variable obj in the function that owns it.
func ownerCell(s *types.Spec, obj types.Object) types.Type {
	for e := s.Outer; e != nil; e = e.Outer {
		if t, ok := e.Cells[obj]; ok {
			return t
		}
	}
	return types.Typ[types.Any]
}

// captureSlots lists the captures of a specialization: for each variable of
// an enclosing function that it reads or writes the cell, and after it the
// flag cell if a read may find the variable unbound.
func captureSlots(s *types.Spec) []captureSlot {
	var out []captureSlot
	for _, obj := range s.Captures {
		out = append(out, captureSlot{obj: obj})
		if guarded(s, obj) {
			out = append(out, captureSlot{obj: obj, flag: true})
		}
	}
	return out
}

// guarded reports whether the owner of obj marked it as one that may be read
// unbound.
func guarded(s *types.Spec, obj types.Object) bool {
	for e := s; e != nil; e = e.Outer {
		if _, ok := e.Cells[obj]; ok {
			return e.Guarded[obj]
		}
	}
	return false
}

// valueSpec finds the specialization that serves the closures of fn made in
// env.
func (b *builder) valueSpec(fn *types.Func, env *types.Spec) *types.Spec {
	for _, s := range b.info.Specs[fn] {
		if s.Generalized && s.Outer == env {
			return s
		}
	}
	return nil
}

// decoratedGlobal is the global that holds what the decorators of a function
// of a module returned, or nil when it has none.
func (b *builder) decoratedGlobal(obj *types.Func) *Global {
	if isNestedFunc(obj) {
		return nil
	}
	def, ok := obj.Decl().(*ast.FunctionDef)
	if !ok || len(def.DecoratorList) == 0 || b.info.Synth[def.DecoratorList[0]] == nil {
		return nil
	}
	k := defaultKey{obj, nil}
	if g := b.defaults[k]; g != nil {
		return g
	}
	t := b.decoratedType(obj, def)
	mod := b.mods[obj.Module()]
	g := &Global{Name: obj.FullName() + "$decorated", Type: t, Module: mod}
	mod.Globals = append(mod.Globals, g)
	b.defaults[k] = g
	return g
}

func isNestedFunc(fn *types.Func) bool {
	p := fn.Parent()
	return p != nil && (p.Kind() == types.FunctionScope || p.Kind() == types.ComprehensionScope)
}

// decoratedType is the type of the value the decorators of def return.
func (b *builder) decoratedType(obj *types.Func, def *ast.FunctionDef) Type {
	call := b.info.Synth[def.DecoratorList[0]]
	for _, s := range b.info.Specs[obj.Module().Init()] {
		if tv, ok := s.Types[call]; ok {
			return b.lower(tv.Type)
		}
	}
	return tyAny
}

// cellsOf lists the variables of a specialization that closures capture, in
// the order of their declaration.
func cellsOf(s *types.Spec) []types.Object {
	var out []types.Object
	for obj := range s.Cells {
		out = append(out, obj)
	}
	slices.SortFunc(out, func(x, y types.Object) int { return int(x.Pos()) - int(y.Pos()) })
	return out
}

// funcType is the type of a closure of the version fn of a function.
func funcType(fn *Function) Type {
	elems := make([]Type, 0, len(fn.Params)+1)
	for _, p := range fn.Params {
		elems = append(elems, p.Typ)
	}
	return Type{Kind: KindFunc, Elems: append(elems, fn.callResult())}
}
