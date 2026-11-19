package ssa

import (
	"math/big"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/types"
)

// expr lowers e and returns its value in the IR type of its node. Code that
// cannot run is not built: an expression of type Never ends the block.
func (f *fbuild) expr(e ast.Expr) Value {
	if v, ok := f.known[e]; ok {
		return v
	}
	saved := f.origin
	f.origin = e
	defer func() { f.origin = saved }()
	v := f.exprValue(e)
	if isNever(f.typeOf(e)) && f.alive() {
		f.end(OpUnreachable)
	}
	if c := f.spec.Calls[types.Site{Node: e, Kind: types.SiteConvert}]; f.isUserCall(c) {
		return f.conv(f.invoke(c, []Value{v}), tyStr)
	}
	return v
}

func (f *fbuild) exprValue(e ast.Expr) Value {
	switch e := e.(type) {
	case *ast.Constant:
		return f.constant(e)
	case *ast.Name:
		return f.name(e)
	case *ast.BinOp:
		l, r := f.expr(e.Left), f.expr(e.Right)
		return f.arith(e, e.Op, l, f.typeOf(e.Left), r, f.typeOf(e.Right), e.Right, f.irType(e))
	case *ast.UnaryOp:
		return f.unary(e)
	case *ast.BoolOp:
		return f.boolOp(e)
	case *ast.Compare:
		return f.compare(e)
	case *ast.IfExp:
		return f.ifExp(e)
	case *ast.Call:
		return f.call(e)
	case *ast.Attribute:
		return f.attribute(e)
	case *ast.JoinedStr:
		return f.joined(e)
	case *ast.Tuple:
		return f.tuple(e)
	case *ast.Lambda:
		return f.lambda(e)
	case *ast.List:
		return f.list(e)
	case *ast.Set:
		return f.set(e)
	case *ast.Dict:
		return f.dict(e)
	case *ast.Subscript:
		return f.subscript(e)
	case *ast.NamedExpr:
		return f.walrus(e)
	case *ast.ListComp, *ast.SetComp, *ast.DictComp:
		return f.comprehension(e)
	case *ast.GeneratorExp:
		if c := f.spec.Calls[e]; c != nil && c.Slot == "inline" {
			return f.comprehension(e)
		}
		return f.genexp(e)
	case *ast.Yield:
		return f.yield(e)
	case *ast.YieldFrom:
		return f.yieldFrom(e)
	}
	return f.undef(f.irType(e))
}

func (f *fbuild) tuple(e *ast.Tuple) Value {
	want := f.irType(e)
	vals := make([]Value, len(e.Elts))
	for i, x := range e.Elts {
		vals[i] = f.expr(x)
	}
	if want.Kind != KindTuple {
		return f.undef(want)
	}
	for i := range vals {
		vals[i] = f.conv(vals[i], want.Elems[min(i, len(want.Elems)-1)])
	}
	return f.val(OpNewTuple, want, vals...)
}

func (f *fbuild) joined(e *ast.JoinedStr) Value {
	var acc Value = &Const{Value: ast.Str(""), Typ: tyStr}
	for i, part := range e.Values {
		var s Value
		if fv, ok := part.(*ast.FormattedValue); ok {
			s = f.formatted(fv)
		} else {
			s = f.expr(part)
		}
		if i == 0 {
			acc = s
		} else {
			acc = f.val(OpConcat, tyStr, acc, s)
		}
	}
	return acc
}

func (f *fbuild) formatted(fv *ast.FormattedValue) Value {
	if c := f.spec.Calls[fv]; f.isUserCall(c) {
		var spec Value = &Const{Value: ast.Str(""), Typ: tyStr}
		if fv.FormatSpec != nil {
			spec = f.expr(fv.FormatSpec)
		}
		return f.conv(f.invoke(c, []Value{f.expr(fv.Value), spec}), tyStr)
	}
	v := f.expr(fv.Value)
	if c := f.spec.Calls[types.Site{Node: fv.Value, Kind: types.SiteConvert}]; !f.isUserCall(c) {
		v = f.convertValue(fv.Conversion, v)
	}
	if fv.FormatSpec == nil && TypeOf(v).same(tyStr) {
		return v
	}
	var spec Value = &Const{Value: ast.Str(""), Typ: tyStr}
	if fv.FormatSpec != nil {
		spec = f.expr(fv.FormatSpec)
	}
	return f.val(OpFormat, tyStr, v, spec)
}

func (f *fbuild) ifExp(e *ast.IfExp) Value {
	want := f.irType(e)
	yes, no, join := f.newBlock(), f.newBlock(), f.newBlock(want)
	f.cond(e.Test, yes, no)
	f.seal(yes)
	f.seal(no)
	f.start(yes)
	f.jump(join, f.conv(f.expr(e.Body), want))
	f.start(no)
	f.jump(join, f.conv(f.expr(e.OrElse), want))
	return f.resume(join)
}

// boolOp lowers and and or with short-circuit blocks that pass the value of
// the operand that decided the result.
func (f *fbuild) boolOp(e *ast.BoolOp) Value {
	want := f.irType(e)
	join := f.newBlock(want)
	last := len(e.Values) - 1
	for i, x := range e.Values {
		v := f.expr(x)
		if i == last {
			f.jump(join, f.conv(v, want))
			break
		}
		next := f.newBlock()
		t := f.truth(x, v)
		if e.Op == ast.BoolAnd {
			f.branchArgs(t, next, nil, join, []Value{f.conv(v, want)})
		} else {
			f.branchArgs(t, join, []Value{f.conv(v, want)}, next, nil)
		}
		f.seal(next)
		f.start(next)
	}
	return f.resume(join)
}

// cond branches to yes where e is truthy and to no otherwise.
func (f *fbuild) cond(e ast.Expr, yes, no *Block) {
	switch e := e.(type) {
	case *ast.BoolOp:
		last := len(e.Values) - 1
		for i, x := range e.Values {
			if i == last {
				f.cond(x, yes, no)
				return
			}
			next := f.newBlock()
			if e.Op == ast.BoolAnd {
				f.cond(x, next, no)
			} else {
				f.cond(x, yes, next)
			}
			f.seal(next)
			f.start(next)
		}
		return
	case *ast.UnaryOp:
		if e.Op == ast.UnaryNot {
			f.cond(e.Operand, no, yes)
			return
		}
	}
	v := f.expr(e)
	f.branch(f.truth(e, v), yes, no)
}

// convertValue applies the conversion of a replacement field: !r and !a
// call repr and ascii, and !s makes the string of the value.
func (f *fbuild) convertValue(conversion int, v Value) Value {
	name := map[int]string{'r': "repr", 'a': "ascii"}[conversion]
	if name != "" {
		if fn, ok := types.Universe.Lookup(name).(*types.Func); ok {
			return f.callFn(f.b.externFunc(fn, -1, false), []Value{v})
		}
	}
	if conversion == 's' && !TypeOf(v).same(tyStr) {
		return f.val(OpFormat, tyStr, v, &Const{Value: ast.Str(""), Typ: tyStr})
	}
	return v
}

func (f *fbuild) constant(c *ast.Constant) Value {
	switch v := c.Value.(type) {
	case ast.Int:
		return &Const{Value: copyInt(v), Typ: tyInt}
	case ast.Float:
		return &Const{Value: v, Typ: tyF64}
	case ast.Bool:
		return &Const{Value: v, Typ: tyI1}
	case ast.Str:
		return &Const{Value: v, Typ: tyStr}
	case ast.Bytes:
		return &Const{Value: v, Typ: tyBytes}
	case ast.None:
		return &Const{Value: v, Typ: tyNone}
	}
	return f.undef(tyAny)
}

// markBound records that the object is bound, if any read needs to know.
func (f *fbuild) markBound(obj types.Object) {
	if !f.b.maybe[obj] {
		return
	}
	if f.isLocal(obj) {
		f.writeVar(bkey{obj: obj, bound: true}, f.cur, boolConst(true))
	} else if g := f.b.flags[obj]; g != nil {
		f.store(g, boolConst(true))
	}
}

func (f *fbuild) store(g *Global, v Value) {
	if in := f.emit(OpStoreGlobal, Type{}, f.conv(v, g.Type)); in != nil {
		in.Global = g
	}
}

func (f *fbuild) name(n *ast.Name) Value {
	obj := f.b.info.Uses[n]
	want := f.irType(n)
	if fn, ok := obj.(*types.Func); ok {
		if !f.guard(n, obj) {
			return f.undef(want)
		}
		return f.funcName(n, fn)
	}
	v, ok := obj.(*types.Var)
	if !ok {
		if obj == nil && f.b.info.Defined[n] == types.Undefined {
			f.raise("NameError", "name '"+n.ID+"' is not defined")
		}
		return f.undef(want)
	}
	if !f.guard(n, obj) {
		return f.undef(want)
	}
	if v.Kind() == types.GlobalVar {
		return f.conv(f.load(f.b.globalOf(v)), want)
	}
	if v.Kind() == types.ClassVar && f.owner != nil {
		return f.conv(f.load(f.b.classVar(f.owner, v)), want)
	}
	if c := f.cells[v]; c != nil {
		f.checkCaptured(n, v)
		return f.conv(f.loadCell(c), want)
	}
	return f.conv(f.readVar(bkey{obj: v}, f.cur), want)
}

// guard raises the error of a name that is not bound where it is read. It
// returns false when the name is never bound and the block ends.
func (f *fbuild) guard(n *ast.Name, obj types.Object) bool {
	switch d := f.b.info.Defined[n]; d {
	case types.Undefined:
		f.raise(f.unboundError(obj), "name '"+n.ID+"' is not defined")
		return false
	case types.MaybeDefined:
		flag := f.isBound(obj)
		ok, fail := f.newBlock(), f.newBlock()
		f.branch(flag, ok, fail)
		f.seal(ok)
		f.seal(fail)
		f.start(fail)
		f.raise(f.unboundError(obj), "name '"+n.ID+"' is not bound")
		f.start(ok)
	}
	return true
}

func (f *fbuild) unboundError(obj types.Object) string {
	if f.isLocal(obj) {
		return "UnboundLocalError"
	}
	return "NameError"
}

// isBound reads the flag that tells whether the object is bound.
func (f *fbuild) isBound(obj types.Object) Value {
	if f.isLocal(obj) {
		return f.readVar(bkey{obj: obj, bound: true}, f.cur)
	}
	if g := f.b.flags[obj]; g != nil {
		return f.load(g)
	}
	return boolConst(true)
}

// isLocal reports whether the object is a local of the function being built.
func (f *fbuild) isLocal(obj types.Object) bool {
	sc := f.spec.Func.Scope()
	return sc != nil && sc.Kind() == types.FunctionScope && obj.Parent() == sc
}

// raise raises a new exception of a builtins class and ends the block.
func (f *fbuild) raise(class, msg string) {
	f.raiseValue(class, &Const{Value: ast.Str(msg), Typ: tyStr})
}

// raiseValue raises a new exception of a builtins class made from msg, or
// from nothing when msg is nil.
func (f *fbuild) raiseValue(class string, msg Value) {
	if !f.alive() {
		return
	}
	var args []Value
	if msg != nil {
		args = append(args, msg)
	}
	f.plain++
	call := f.val(OpCall, Type{Kind: KindClass, Class: f.b.class(class)}, args...)
	f.plain--
	call.(*Instr).Callee = f.b.exception(class, msg != nil)
	f.throw(OpRaise, call)
}

func (f *fbuild) attribute(e *ast.Attribute) Value {
	want := f.irType(e)
	if v, ok := f.b.info.Uses[e].(*types.Var); ok && v.Kind() == types.GlobalVar {
		return f.conv(f.load(f.b.globalOf(v)), want)
	}
	if v, ok := f.fieldLoad(e, want); ok {
		return v
	}
	return f.undef(want)
}

func (f *fbuild) irType(e ast.Expr) Type {
	return f.b.lower(f.typeOf(e))
}

func (f *fbuild) typeOf(e ast.Expr) types.Type {
	if tv, ok := f.spec.Types[e]; ok {
		return tv.Type
	}
	return types.Typ[types.Any]
}

func (f *fbuild) load(g *Global) Value {
	in := f.emit(OpLoadGlobal, g.Type)
	if in == nil {
		return f.undef(g.Type)
	}
	in.Global = g
	return in
}

// resume continues in a block whose predecessors are all known, if it is
// reachable, and returns its first parameter.
func (f *fbuild) resume(join *Block) Value {
	f.seal(join)
	if len(f.in[join]) == 0 {
		f.cur = nil
		return f.undef(tyNone)
	}
	f.start(join)
	if len(join.Params) == 0 {
		return f.undef(tyNone)
	}
	return join.Params[0]
}

// truth turns the value of e into an i1 by the truth rules of its type.
func (f *fbuild) truth(e ast.Expr, v Value) Value {
	if c := f.spec.Calls[types.Site{Node: e, Kind: types.SiteTruth}]; c != nil && c.Kind == types.Dynamic {
		return f.dynTruth(v)
	} else if f.isUserCall(c) {
		res := f.invoke(c, []Value{v})
		if c.Slot == "__len__" {
			return f.val(OpNe, tyI1, f.conv(res, tyInt), intConst(0))
		}
		return f.conv(res, tyI1)
	}
	return f.truthValue(v)
}

// truthValue turns a value into an i1 by the truth rules of its type.
func (f *fbuild) truthValue(v Value) Value {
	t := TypeOf(v)
	switch {
	case t.same(tyI1):
		return v
	case t.same(tyInt):
		return f.val(OpNe, tyI1, v, intConst(0))
	case t.same(tyF64):
		return f.val(OpNe, tyI1, v, &Const{Value: ast.Float(0), Typ: tyF64})
	case t.same(tyNone):
		return boolConst(false)
	case t.Kind == KindAny:
		return f.dynTruth(v)
	case t.Kind == KindStr || t.Kind == KindBytes || t.Kind == KindList || t.Kind == KindDict || t.Kind == KindSet || t.Kind == KindTuple:
		if !t.Nullable {
			return f.val(OpNe, tyI1, f.val(OpLen, tyInt, v), intConst(0))
		}
	}
	if t.Nullable {
		return f.val(OpIsNot, tyI1, v, &Const{Value: ast.None{}, Typ: tyNone})
	}
	return boolConst(true)
}

func (f *fbuild) dynTruth(v Value) Value {
	a := f.register(f.conv(v, tyAny))
	in := f.emit(OpDynBinOp, tyAny, a, a)
	if in == nil {
		return boolConst(false)
	}
	in.Attr = "__bool__"
	return f.val(OpUnbox, tyI1, in)
}

func boolConst(b bool) *Const {
	return &Const{Value: ast.Bool(b), Typ: tyI1}
}

// conv converts v to the IR type to: numbers promote as Python does, a
// reference becomes nullable, and anything else goes through any.
func (f *fbuild) conv(v Value, to Type) Value {
	from := TypeOf(v)
	switch c, isConst := v.(*Const); {
	case from.same(to):
		return v
	case isConst && c.Undef:
		return f.undef(to)
	case isConst:
		if r := constConv(c, to); r != nil {
			return r
		}
	}
	switch {
	case to.Kind == KindAny && from.Kind == KindGen && !from.Nullable:
		return f.val(OpBox, tyAny, f.spill(v))
	case to.Kind == KindAny:
		return f.val(OpBox, tyAny, v)
	case from.Kind == KindAny:
		return f.val(OpUnbox, to, v)
	case from.same(tyI1) && to.same(tyInt):
		return f.val(OpSelect, tyInt, v, intConst(1), intConst(0))
	case from.same(tyI1) && to.same(tyF64):
		return f.conv(f.conv(v, tyInt), tyF64)
	case from.same(tyInt) && to.same(tyF64):
		return f.val(OpInt2F64, tyF64, v)
	case from.same(tyNone) && to.Nullable:
		return &Const{Value: ast.None{}, Typ: to}
	case from.Kind.info().ref && to.Kind.info().ref:
		return f.val(OpCast, to, v)
	}
	return f.val(OpUnbox, to, f.val(OpBox, tyAny, v))
}

func isNever(t types.Type) bool {
	b, ok := t.(*types.Basic)
	return ok && b.Kind() == types.Never
}

func constConv(c *Const, to Type) Value {
	switch v := c.Value.(type) {
	case ast.Int:
		if to.same(tyF64) {
			if fl, acc := new(big.Float).SetInt(v.N).Float64(); acc == big.Exact {
				return &Const{Value: ast.Float(fl), Typ: to}
			}
		}
	case ast.Bool:
		if to.same(tyInt) {
			return intConst(map[bool]int64{false: 0, true: 1}[bool(v)])
		}
	case ast.None:
		if to.Nullable {
			return &Const{Value: v, Typ: to}
		}
	case ast.Str, ast.Bytes:
		if to.Kind == c.Typ.Kind {
			return &Const{Value: v, Typ: to}
		}
	}
	if to.Kind == KindAny {
		return &Const{Value: c.Value, Typ: tyAny}
	}
	return nil
}

func intConst(n int64) *Const {
	return &Const{Value: ast.Int{N: big.NewInt(n)}, Typ: tyInt}
}

// copyInt gives a number the canonical form big.Int has for its value, which
// SetString does not always give to zero, so that Parse reads back what Build
// made.
func copyInt(v ast.Int) ast.Int {
	return ast.Int{N: new(big.Int).Set(v.N)}
}
