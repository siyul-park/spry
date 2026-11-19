package ssa

import (
	"slices"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/types"
)

// eagerConsumers are the functions that read a whole iterable in order and
// nothing else, so that a generator may be run to its end before the call.
var eagerConsumers = map[string]bool{
	"sum": true, "min": true, "max": true, "sorted": true, "join": true,
	"list": true, "tuple": true, "set": true, "frozenset": true, "dict": true,
}

// itemType is the type of the items an iterator or a generator gives, or of
// def when v is neither.
func itemType(t Type, def Type) Type {
	if (t.Kind == KindIter || t.Kind == KindGen) && !t.Nullable {
		return t.Elems[0]
	}
	return def
}

// yield lowers a yield expression: the value of the expression is what the
// caller sends.
func (f *fbuild) yield(e *ast.Yield) Value {
	var v Value = noneConst()
	if e.Value != nil {
		v = f.expr(e.Value)
	}
	in := f.emit(OpYield, f.fn.Send, f.conv(v, f.fn.Yield))
	if in == nil {
		return f.undef(f.irType(e))
	}
	return f.conv(in, f.irType(e))
}

// yieldFrom lowers yield from as a loop that yields what the delegate gives.
// A generator is resumed with what the caller sends, and its return value is
// the value of the expression; any other iterable gives None.
func (f *fbuild) yieldFrom(e *ast.YieldFrom) Value {
	src := f.expr(e.Value)
	t := TypeOf(src)
	if t.Kind != KindGen || t.Nullable {
		item := f.irType(e.Value)
		_ = item
		f.forEach(types.Site{Node: e, Kind: types.SiteIter}, types.Site{Node: e, Kind: types.SiteNext}, src, f.fn.Yield, func(cur Value) {
			f.emit(OpYield, f.fn.Send, f.conv(cur, f.fn.Yield))
		})
		return noneConst()
	}
	head, body, done := f.newBlock(f.fn.Send), f.newBlock(), f.newBlock()
	f.jump(head, f.conv(noneConst(), f.fn.Send))
	f.start(head)
	f.branch(f.val(OpResume, tyI1, src, f.conv(head.Params[0], t.Elems[1])), body, done)
	f.seal(body)
	f.seal(done)
	f.start(body)
	cur := f.val(OpCurrent, t.Elems[0], src)
	sent := f.emit(OpYield, f.fn.Send, f.conv(cur, f.fn.Yield))
	if sent != nil {
		f.jump(head, sent)
	}
	f.seal(head)
	f.start(done)
	return f.conv(f.val(OpCurrent, t.Elems[2], src), f.irType(e))
}

// genNext lowers next(g) and next(g, default) on a generator: the next item,
// or the default, or StopIteration where there is none.
func (f *fbuild) genNext(e *ast.Call, g Value) Value {
	want := f.irType(e)
	t := TypeOf(g)
	have, empty, join := f.newBlock(), f.newBlock(), f.newBlock(want)
	f.branch(f.advance(g), have, empty)
	f.seal(have)
	f.seal(empty)
	f.start(have)
	f.jump(join, f.conv(f.val(OpCurrent, t.Elems[0], g), want))
	f.start(empty)
	if len(e.Args) == 2 {
		f.jump(join, f.conv(f.expr(e.Args[1]), want))
	} else {
		f.raiseValue("StopIteration", nil)
	}
	return f.resume(join)
}

// advance moves an iterator or a generator to its next item and tells whether
// there is one. A generator is sent None.
func (f *fbuild) advance(it Value) Value {
	t := TypeOf(it)
	if t.Kind != KindGen {
		return f.val(OpNext, tyI1, it)
	}
	return f.val(OpResume, tyI1, it, f.conv(noneConst(), t.Elems[1]))
}

// genexp lowers a generator expression to a generator function. Its first
// iterable is evaluated here, as Python does, and passed in; the variables of
// the enclosing function that it reads are passed by value, or as their cell.
func (f *fbuild) genexp(e *ast.GeneratorExp) Value {
	want := f.irType(e)
	if want.Kind != KindGen {
		return f.undef(want)
	}
	first := e.Generators[0]
	item := f.irType(first.Target)
	it := f.iterOf(f.expr(first.Iter), item)
	free := f.freeVars(e)
	fn := &Function{
		Name: f.b.unique(f.fn.Module, f.fn.Source+".<genexpr>"), Source: f.fn.Source + ".<genexpr>", Module: f.fn.Module,
		Generator: true, Yield: want.Elems[0], Send: want.Elems[1], Result: want.Elems[2],
		Params: []*Param{{Name: "iter", Typ: TypeOf(it)}},
	}
	args := []Value{it}
	for _, obj := range free {
		for _, v := range f.freeValues(obj) {
			fn.Params = append(fn.Params, &Param{Name: paramName(obj.Name()), Typ: TypeOf(v)})
			args = append(args, v)
		}
	}
	fn.Params = dedupeParams(fn.Params)
	f.buildGenexp(e, fn, free)
	f.fn.Module.Functions = append(f.fn.Module.Functions, fn)
	in := f.emit(OpMakeGen, want, args...)
	if in == nil {
		return f.undef(want)
	}
	in.Callee = fn
	return in
}

// buildGenexp builds the body of the generator function of e.
func (f *fbuild) buildGenexp(e *ast.GeneratorExp, fn *Function, free []types.Object) {
	g := newFbuild(f.b, fn, f.spec)
	g.origin = e
	g.cells, g.flags = map[types.Object]Value{}, map[types.Object]Value{}
	for obj, t := range f.vtypes {
		g.vtypes[obj] = t
	}
	g.preIter = fn.Params[0]
	next := 1
	for _, obj := range free {
		if f.cells[obj] != nil {
			g.cells[obj] = fn.Params[next]
			next++
			if f.flags[obj] != nil {
				g.flags[obj] = fn.Params[next]
				next++
			}
			continue
		}
		g.writeVar(bkey{obj: obj}, g.cur, fn.Params[next])
		next++
		if f.b.maybe[obj] {
			g.writeVar(bkey{obj: obj, bound: true}, g.cur, fn.Params[next])
			next++
		}
	}
	if sc := f.b.info.Scopes[e]; sc != nil {
		for _, obj := range cellsOf(f.spec) {
			if obj.Parent() == sc {
				g.newCell(obj)
			}
		}
	}
	g.compLoop(e.Generators, 0, func() {
		v := g.expr(e.Elt)
		g.emit(OpYield, fn.Send, g.conv(v, fn.Yield))
	})
	g.fallOff(false)
	g.finish()
}

// freeValues are the values the generator function takes for a variable of the
// enclosing function: its cell, or its value and whether it is bound.
func (f *fbuild) freeValues(obj types.Object) []Value {
	if c := f.cells[obj]; c != nil {
		if flag := f.flags[obj]; flag != nil {
			return []Value{c, flag}
		}
		return []Value{c}
	}
	out := []Value{f.conv(f.readVar(bkey{obj: obj}, f.cur), f.varType(bkey{obj: obj}))}
	if f.b.maybe[obj] {
		out = append(out, f.readVar(bkey{obj: obj, bound: true}, f.cur))
	}
	return out
}

// freeVars lists the variables of the enclosing function that a generator
// expression reads, in the order of their declaration.
func (f *fbuild) freeVars(e *ast.GeneratorExp) []types.Object {
	inner := map[*types.Scope]bool{}
	ast.Inspect(e, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.ListComp, *ast.SetComp, *ast.DictComp, *ast.GeneratorExp:
			if sc := f.b.info.Scopes[n]; sc != nil {
				inner[sc] = true
			}
		}
		return true
	})
	seen := map[types.Object]bool{}
	var out []types.Object
	ast.Inspect(e, func(n ast.Node) bool {
		name, ok := n.(*ast.Name)
		if !ok {
			return true
		}
		obj := f.b.info.Uses[name]
		if obj == nil {
			obj = f.b.info.Defs[name]
		}
		if obj == nil || seen[obj] || obj.Parent() == nil || inner[obj.Parent()] || !f.ownsVar(obj) {
			return true
		}
		seen[obj] = true
		out = append(out, obj)
		return true
	})
	slices.SortFunc(out, func(x, y types.Object) int { return int(x.Pos()) - int(y.Pos()) })
	return out
}

// ownsVar reports whether obj is a variable or nested function of the
// function being built, or of a comprehension in it.
func (f *fbuild) ownsVar(obj types.Object) bool {
	switch o := obj.(type) {
	case *types.Var:
		if o.Kind() == types.GlobalVar || o.Kind() == types.ClassVar || o.Kind() == types.FieldVar {
			return false
		}
	case *types.Func:
		if !isNestedFunc(o) {
			return false
		}
	default:
		return false
	}
	for sc := obj.Parent(); sc != nil; sc = sc.Parent() {
		if sc == f.spec.Func.Scope() {
			return true
		}
		if sc.Kind() != types.ComprehensionScope {
			return false
		}
	}
	return false
}

// spill makes a generator a value that a host function can take: its items in
// a list where the call consumes it whole. Any other call gets the generator
// itself, which only the runtime can run, and is noted as a fallback.
func (f *fbuild) spill(g Value) Value {
	if f.eager > 0 {
		return f.drain(g)
	}
	if f.origin != nil && f.b.fset != nil {
		f.b.note(f.b.fset.Position(f.origin.Pos()), f.b.fset.Position(f.origin.End()), "generator", "a generator is passed to a function that may not consume it whole")
	}
	return g
}

// drain collects the items of a generator in a list.
func (f *fbuild) drain(g Value) Value {
	item := TypeOf(g).Elems[0]
	list := f.val(OpNewList, Type{Kind: KindList, Elems: []Type{item}})
	f.forEach(nil, nil, g, item, func(cur Value) {
		f.emit(OpAppend, Type{}, list, cur)
	})
	return list
}

// genSend lowers g.send(v): the generator is resumed with v, and gives its next
// item or raises StopIteration.
func (f *fbuild) genSend(e *ast.Call, attr *ast.Attribute) Value {
	g := f.expr(attr.Value)
	t := TypeOf(g)
	sent := f.conv(f.expr(e.Args[0]), t.Elems[1])
	have, empty := f.newBlock(), f.newBlock()
	f.branch(f.val(OpResume, tyI1, g, sent), have, empty)
	f.seal(have)
	f.seal(empty)
	f.start(empty)
	f.raiseValue("StopIteration", nil)
	f.start(have)
	return f.conv(f.val(OpCurrent, t.Elems[0], g), f.irType(e))
}
