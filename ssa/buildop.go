package ssa

import (
	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/types"
)

type numKind uint8

const (
	notNum numKind = iota
	boolNum
	intNum
	floatNum
)

var arithOps = map[ast.Operator]Op{
	ast.OpAdd: OpAdd, ast.OpSub: OpSub, ast.OpMult: OpMul, ast.OpDiv: OpTrueDiv, ast.OpMod: OpMod,
	ast.OpPow: OpPow, ast.OpLShift: OpLShift, ast.OpRShift: OpRShift, ast.OpBitOr: OpOr,
	ast.OpBitXor: OpXor, ast.OpBitAnd: OpAnd, ast.OpFloorDiv: OpFloorDiv,
}

var cmpOps = map[ast.CmpOperator]Op{
	ast.CmpEq: OpEq, ast.CmpNotEq: OpNe, ast.CmpLt: OpLt, ast.CmpLtE: OpLe, ast.CmpGt: OpGt, ast.CmpGtE: OpGe,
}

var dunders = map[ast.Operator]string{
	ast.OpAdd: "__add__", ast.OpSub: "__sub__", ast.OpMult: "__mul__", ast.OpMatMult: "__matmul__",
	ast.OpDiv: "__truediv__", ast.OpMod: "__mod__", ast.OpPow: "__pow__", ast.OpLShift: "__lshift__",
	ast.OpRShift: "__rshift__", ast.OpBitOr: "__or__", ast.OpBitXor: "__xor__", ast.OpBitAnd: "__and__",
	ast.OpFloorDiv: "__floordiv__",
}

var cmpDunders = map[ast.CmpOperator]string{
	ast.CmpEq: "__eq__", ast.CmpNotEq: "__ne__", ast.CmpLt: "__lt__", ast.CmpLtE: "__le__",
	ast.CmpGt: "__gt__", ast.CmpGtE: "__ge__",
}

// arith lowers l op r. key carries the resolution the checker recorded; want
// is the IR type the result must have, or the zero Type to take what the
// operation gives. Numbers use the arithmetic of the IR, str + str is a
// concat, anything the checker could not resolve is a dynamic operation and
// anything else a call of the method of the standard library.
func (f *fbuild) arith(key ast.Node, op ast.Operator, l Value, lt types.Type, r Value, rt types.Type, right ast.Expr, want Type) Value {
	c := f.spec.Calls[key]
	if c == nil {
		return f.undef(want)
	}
	if c.Kind == types.Dynamic {
		return f.dynBinary(dunders[op], l, r)
	}
	lk, rk := numKindOf(lt), numKindOf(rt)
	switch {
	case f.isUserCall(c):
		return f.method(c, l, r)
	case lk != notNum && rk != notNum:
		return f.numeric(op, l, lk, r, rk, right, want)
	case op == ast.OpAdd && isStrType(lt) && isStrType(rt):
		return f.val(OpConcat, tyStr, l, r)
	}
	return f.method(c, l, r)
}

// numeric lowers an operation on bool, int and float values: both operands
// take the common type, as Python converts them.
func (f *fbuild) numeric(op ast.Operator, l Value, lk numKind, r Value, rk numKind, right ast.Expr, want Type) Value {
	common := tyInt
	if lk == floatNum || rk == floatNum {
		common = tyF64
	}
	res := common
	if op == ast.OpDiv {
		res = tyF64
	}
	needsDyn := common.same(tyF64) && (op == ast.OpLShift || op == ast.OpRShift || op == ast.OpBitAnd || op == ast.OpBitOr || op == ast.OpBitXor)
	needsDyn = needsDyn || op == ast.OpPow && common.same(tyInt) && !nonNegative(right)
	boolRes := lk == boolNum && rk == boolNum && (op == ast.OpBitAnd || op == ast.OpBitOr || op == ast.OpBitXor)
	if boolRes {
		res = tyI1
	}
	if needsDyn || want.Kind != KindInvalid && !res.same(want) {
		return f.dynBinary(dunders[op], l, r)
	}
	typ := res
	if boolRes {
		typ = common
	}
	v := f.val(arithOps[op], typ, f.conv(l, common), f.conv(r, common))
	if boolRes {
		return f.val(OpNe, tyI1, v, intConst(0))
	}
	return v
}

// nonNegative reports whether e is a literal int that is not negative, the
// exponent for which an int power is an int.
func nonNegative(e ast.Expr) bool {
	c, ok := e.(*ast.Constant)
	if !ok {
		return false
	}
	n, ok := c.Value.(ast.Int)
	return ok && n.N.Sign() >= 0
}

func (f *fbuild) unary(e *ast.UnaryOp) Value {
	v := f.expr(e.Operand)
	t := f.typeOf(e.Operand)
	if e.Op == ast.UnaryNot {
		return f.val(OpNot, tyI1, f.truth(e.Operand, v))
	}
	c := f.spec.Calls[e]
	if f.isUserCall(c) {
		return f.invoke(c, []Value{v})
	}
	k := numKindOf(t)
	if c == nil || c.Kind == types.Dynamic || k == notNum {
		return f.dynUnary(e.Op, v)
	}
	common := tyInt
	if k == floatNum {
		common = tyF64
	}
	operand := f.conv(v, common)
	switch {
	case e.Op == ast.UnaryUSub:
		return f.val(OpNeg, common, operand)
	case e.Op == ast.UnaryInvert && common.same(tyInt):
		return f.val(OpInvert, tyInt, operand)
	case e.Op == ast.UnaryUAdd:
		return operand
	}
	return f.dynUnary(e.Op, v)
}

func (f *fbuild) dynUnary(op ast.UnaryOperator, v Value) Value {
	attr := map[ast.UnaryOperator]string{ast.UnaryInvert: "__invert__", ast.UnaryUAdd: "__pos__", ast.UnaryUSub: "__neg__"}[op]
	return f.dynBinary(attr, v, v)
}

// compare lowers a comparison or a chain of them; the middle operands are
// evaluated once.
func (f *fbuild) compare(e *ast.Compare) Value {
	left := f.expr(e.Left)
	lt := f.typeOf(e.Left)
	if len(e.Ops) == 1 {
		right := f.expr(e.Comparators[0])
		return f.cmp(types.Site{Node: e, Kind: types.SiteCompare}, e.Ops[0], left, lt, right, f.typeOf(e.Comparators[0]))
	}
	join := f.newBlock(tyI1)
	for i, op := range e.Ops {
		rx := e.Comparators[i]
		right := f.expr(rx)
		res := f.cmp(types.Site{Node: e, Kind: types.SiteCompare, Index: i}, op, left, lt, right, f.typeOf(rx))
		if i == len(e.Ops)-1 {
			f.jump(join, res)
			break
		}
		next := f.newBlock()
		f.branchArgs(res, next, nil, join, []Value{res})
		f.seal(next)
		f.start(next)
		left, lt = right, f.typeOf(rx)
	}
	return f.resume(join)
}

// cmp lowers one comparison; key is the site where the checker recorded how
// it is dispatched.
func (f *fbuild) cmp(key types.Site, op ast.CmpOperator, l Value, lt types.Type, r Value, rt types.Type) Value {
	switch op {
	case ast.CmpIs:
		return f.val(OpIs, tyI1, l, r)
	case ast.CmpIsNot:
		return f.val(OpIsNot, tyI1, l, r)
	case ast.CmpIn, ast.CmpNotIn:
		return f.contains(key, op, l, r)
	}
	c := f.spec.Calls[key]
	if c == nil {
		return f.undef(tyI1)
	}
	if c.Kind == types.Dynamic {
		return f.val(OpUnbox, tyI1, f.dynBinary(cmpDunders[op], l, r))
	}
	if f.isUserCall(c) {
		return f.compareCall(c, op, l, r)
	}
	if f.isIdentity(c, lt, rt) {
		if op == ast.CmpEq {
			return f.val(OpIs, tyI1, l, r)
		}
		return f.val(OpIsNot, tyI1, l, r)
	}
	lk, rk := numKindOf(lt), numKindOf(rt)
	switch {
	case lk != notNum && rk != notNum:
		common := tyInt
		if lk == floatNum || rk == floatNum {
			common = tyF64
		} else if lk == boolNum && rk == boolNum {
			common = tyI1
		}
		return f.val(cmpOps[op], tyI1, f.conv(l, common), f.conv(r, common))
	case isStrType(lt) && isStrType(rt):
		return f.val(cmpOps[op], tyI1, l, r)
	case (op == ast.CmpEq || op == ast.CmpNotEq) && (TypeOf(l).same(tyNone) || TypeOf(r).same(tyNone)):
		if op == ast.CmpEq {
			return f.val(OpIs, tyI1, l, r)
		}
		return f.val(OpIsNot, tyI1, l, r)
	}
	return f.conv(f.method(c, l, r), tyI1)
}

func numKindOf(t types.Type) numKind {
	n, ok := types.Default(t).(*types.Named)
	if !ok || n.Obj().Module() == nil || n.Obj().Module().Name() != "builtins" {
		return notNum
	}
	switch n.Obj().Name() {
	case "bool":
		return boolNum
	case "int":
		return intNum
	case "float":
		return floatNum
	}
	return notNum
}

func isStrType(t types.Type) bool {
	n, ok := types.Default(t).(*types.Named)
	return ok && n.Obj().Name() == "str" && n.Obj().Module() != nil && n.Obj().Module().Name() == "builtins"
}

// method calls the extern method that implements an operation, with the
// receiver first: the right operand for a reflected method.
func (f *fbuild) method(c *types.Call, l, r Value) Value {
	recv, arg := l, r
	if len(c.Slot) > 3 && c.Slot[:3] == "__r" && c.Slot != "__rshift__" {
		recv, arg = r, l
	}
	if c.Target != nil {
		return f.invoke(c, []Value{recv, arg})
	}
	if c.Func == nil {
		return f.undef(tyAny)
	}
	return f.callFn(f.b.externFunc(c.Func, c.Overload, false), []Value{recv, arg})
}

// callFn calls fn with args converted to its parameters.
func (f *fbuild) callFn(fn *Function, args []Value) Value {
	conv := make([]Value, len(args))
	for i, a := range args {
		if i < len(fn.Params) {
			a = f.conv(a, fn.Params[i].Typ)
		}
		conv[i] = a
	}
	in := f.emit(OpCall, fn.Result, conv...)
	if in == nil {
		return f.undef(fn.Result)
	}
	in.Callee = fn
	return in
}

func (f *fbuild) contains(key types.Site, op ast.CmpOperator, item, container Value) Value {
	c := f.spec.Calls[key]
	if f.isUserCall(c) {
		res := f.conv(f.invoke(c, []Value{container, item}), tyI1)
		if op == ast.CmpNotIn {
			return f.val(OpNot, tyI1, res)
		}
		return res
	}
	if c != nil && c.Kind == types.Dynamic {
		res := f.val(OpUnbox, tyI1, f.dynBinary("__contains__", container, item))
		if op == ast.CmpNotIn {
			return f.val(OpNot, tyI1, res)
		}
		return res
	}
	if f.userEq(TypeOf(container)) {
		res := f.val(OpUnbox, tyI1, f.dynBinary("__contains__", container, item))
		if op == ast.CmpNotIn {
			return f.val(OpNot, tyI1, res)
		}
		return res
	}
	if op == ast.CmpNotIn {
		return f.val(OpNotIn, tyI1, item, container)
	}
	return f.val(OpIn, tyI1, item, container)
}

// dynBinary is an operation on any: both operands are boxed.
func (f *fbuild) dynBinary(attr string, l, r Value) Value {
	in := f.emit(OpDynBinOp, tyAny, f.register(f.conv(l, tyAny)), f.conv(r, tyAny))
	if in == nil {
		return f.undef(tyAny)
	}
	in.Attr = attr
	return in
}

// register makes a constant a value an instruction can take as its first
// operand, which dotted and indirect forms require.
func (f *fbuild) register(v Value) Value {
	if _, isConst := v.(*Const); isConst {
		return f.val(OpSelect, TypeOf(v), boolConst(true), v, v)
	}
	return v
}
