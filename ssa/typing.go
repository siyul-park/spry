package ssa

import (
	"math"

	"github.com/siyul-park/spry/ast"
)

var (
	tyI1    = Type{Kind: KindI1}
	tyInt   = Type{Kind: KindInt}
	tyF64   = Type{Kind: KindF64}
	tyStr   = Type{Kind: KindStr}
	tyNone  = Type{Kind: KindNone}
	tyAny   = Type{Kind: KindAny}
	tyVoid  = Type{}
	scalars = [...]Kind{KindI1, KindI32, KindInt, KindF64, KindStr, KindBytes}
)

// typing holds the operand and result rules of each Op that Verify checks
// beyond its shape.
var typing = [opEnd]func(*verifier, *Instr){
	OpAdd:      (*verifier).arith,
	OpSub:      (*verifier).arith,
	OpMul:      (*verifier).arith,
	OpFloorDiv: (*verifier).arith,
	OpMod:      (*verifier).arith,
	OpPow:      (*verifier).arith,
	OpTrueDiv:  (*verifier).trueDiv,
	OpLShift:   signature(tyInt, tyInt, tyInt),
	OpRShift:   signature(tyInt, tyInt, tyInt),
	OpAnd:      signature(tyInt, tyInt, tyInt),
	OpOr:       signature(tyInt, tyInt, tyInt),
	OpXor:      signature(tyInt, tyInt, tyInt),
	OpNeg:      (*verifier).neg,
	OpInvert:   signature(tyInt, tyInt),
	OpNot:      signature(tyI1, tyI1),

	OpEq:    (*verifier).compare,
	OpNe:    (*verifier).compare,
	OpLt:    (*verifier).compare,
	OpLe:    (*verifier).compare,
	OpGt:    (*verifier).compare,
	OpGe:    (*verifier).compare,
	OpIs:    (*verifier).resultI1,
	OpIsNot: (*verifier).resultI1,
	OpIn:    (*verifier).resultI1,
	OpNotIn: (*verifier).resultI1,

	OpInt2F64: signature(tyF64, tyInt),
	OpF642Int: signature(tyInt, tyF64),
	OpBox:     (*verifier).box,
	OpUnbox:   (*verifier).unbox,
	OpCast:    (*verifier).cast,

	OpAlloc:      (*verifier).alloc,
	OpLoadField:  (*verifier).loadField,
	OpStoreField: (*verifier).storeField,
	OpNonNull:    (*verifier).nonNull,
	OpNewList:    (*verifier).newSeq,
	OpNewSet:     (*verifier).newSeq,
	OpNewTuple:   (*verifier).newTuple,
	OpNewDict:    (*verifier).newDict,
	OpIndex:      (*verifier).index,
	OpSetIndex:   (*verifier).setIndex,
	OpDelIndex:   (*verifier).delIndex,
	OpSlice:      (*verifier).slice,
	OpLen:        (*verifier).length,
	OpAppend:     (*verifier).appendTo,
	OpUnpack:     (*verifier).unpack,

	OpCall:        (*verifier).call,
	OpCallClosure: (*verifier).callClosure,
	OpCallMethod:  (*verifier).callMethod,
	OpDynCall:     (*verifier).dynamic,

	OpMakeClosure: (*verifier).makeClosure,
	OpNewCell:     (*verifier).newCell,
	OpLoadCell:    (*verifier).loadCell,
	OpStoreCell:   (*verifier).storeCell,

	OpLoadGlobal:  (*verifier).global,
	OpStoreGlobal: (*verifier).global,
	OpDelGlobal:   (*verifier).global,

	OpIter:    (*verifier).iter,
	OpNext:    (*verifier).next,
	OpCurrent: (*verifier).current,

	OpConcat: signature(tyStr, tyStr, tyStr),
	OpFormat: (*verifier).format,

	OpLandingPad: signature(tyAny),
	OpTypeOf:     signature(tyAny, tyAny),
	OpClassObj:   (*verifier).classObj,
	OpIsInstance: (*verifier).isInstance,

	OpMakeGen: (*verifier).makeGen,
	OpYield:   (*verifier).yield,
	OpResume:  (*verifier).resume,

	OpDynBinOp:   (*verifier).dynamic,
	OpDynGetAttr: (*verifier).dynamic,
	OpDynSetAttr: (*verifier).dynamic,

	OpSelect: (*verifier).selectOp,
	OpBr:     signature(tyVoid, tyI1),
	OpRet:    (*verifier).ret,
}

func (v *verifier) format(in *Instr) {
	v.want(in, codeType, arg(in, 1).same(tyStr) && in.Typ.same(tyStr), "format takes a value and a str spec and returns str")
}

func (v *verifier) ret(in *Instr) {
	r := v.fn.Result
	ok := len(in.Args) == 1 && arg(in, 0).same(r)
	if r.same(tyNone) {
		ok = len(in.Args) == 0
	}
	v.want(in, codeType, ok, "ret must match the result type %s of the function", r)
}

func (v *verifier) selectOp(in *Instr) {
	v.want(in, codeType, arg(in, 0).same(tyI1) && arg(in, 1).same(arg(in, 2)) && in.Typ.same(arg(in, 1)), "select takes an i1 and two operands of the result type")
}

func (v *verifier) resume(in *Instr) {
	g := arg(in, 0)
	v.want(in, codeGen, g.Kind == KindGen && !g.Nullable && arg(in, 1).same(g.at(1)) && in.Typ.same(tyI1), "resume takes a gen<Y,S,R> and an S, and returns i1")
}

func (v *verifier) yield(in *Instr) {
	f := v.fn
	if !f.Generator {
		v.report(codeGen, "yield outside a generator")
		return
	}
	v.want(in, codeGen, arg(in, 0).same(f.Yield) && in.Typ.same(f.Send), "yield takes %s and returns %s", f.Yield, f.Send)
}

func (v *verifier) makeGen(in *Instr) {
	if !v.member(in) {
		return
	}
	f := in.Callee
	v.want(in, codeGen, f.Generator, "makegen needs a generator function")
	v.callTypes(in, paramTypes(f.Params), in.Args, Type{Kind: KindGen, Elems: []Type{f.Yield, f.Send, f.Result}})
}

func (v *verifier) classObj(in *Instr) {
	v.want(in, codeMember, v.classes[in.Class], "class @%s is not a member of the program", in.Class.Name)
	v.want(in, codeType, in.Typ.Kind == KindTypeOf && in.Typ.Class == in.Class && !in.Typ.Nullable, "classobj returns the type of its class")
}

func (v *verifier) isInstance(in *Instr) {
	v.want(in, codeMember, v.classes[in.Class], "class @%s is not a member of the program", in.Class.Name)
	v.want(in, codeType, in.Typ.same(tyI1), "isinstance returns i1")
}

func (v *verifier) current(in *Instr) {
	src := arg(in, 0)
	if src.Nullable {
		v.report(codeType, "current needs a non-nullable iterator")
		return
	}
	switch src.Kind {
	case KindIter:
		v.want(in, codeType, in.Typ.same(src.at(0)), "current returns the element type of the iterator")
	case KindGen:
		v.want(in, codeType, in.Typ.same(src.at(0)) || in.Typ.same(src.at(2)), "current of a generator returns its yield or result type")
	default:
		v.report(codeType, "current takes an iter<T> or a gen<Y,S,R>")
	}
}

func (v *verifier) next(in *Instr) {
	it := arg(in, 0)
	v.want(in, codeType, it.Kind == KindIter && !it.Nullable && in.Typ.same(tyI1), "next takes an iter<T> and returns i1")
}

func (v *verifier) iter(in *Instr) {
	v.want(in, codeType, in.Typ.Kind == KindIter && !in.Typ.Nullable, "iter returns an iter<T>")
}

// global checks loadglobal, storeglobal and delglobal.
func (v *verifier) global(in *Instr) {
	if !v.globals[in.Global] {
		v.report(codeMember, "global @%s is not a member of the program", in.Global.Name)
		return
	}
	switch in.Op {
	case OpLoadGlobal:
		v.want(in, codeType, in.Typ.same(in.Global.Type), "global @%s has type %s", in.Global.Name, in.Global.Type)
	case OpStoreGlobal:
		v.want(in, codeType, arg(in, 0).same(in.Global.Type), "global @%s has type %s", in.Global.Name, in.Global.Type)
	}
}

func (v *verifier) storeCell(in *Instr) {
	c := arg(in, 0)
	v.want(in, codeType, c.Kind == KindCell && !c.Nullable && arg(in, 1).same(c.at(0)), "storecell takes a cell and a value of its content type")
}

func (v *verifier) loadCell(in *Instr) {
	c := arg(in, 0)
	v.want(in, codeType, c.Kind == KindCell && !c.Nullable && in.Typ.same(c.at(0)), "loadcell takes a cell and returns its content type")
}

func (v *verifier) newCell(in *Instr) {
	v.want(in, codeType, in.Typ.same(Type{Kind: KindCell, Elems: []Type{arg(in, 0)}}), "newcell returns a cell of its operand's type")
}

func (v *verifier) makeClosure(in *Instr) {
	if !v.member(in) {
		return
	}
	f := in.Callee
	ok := len(in.Args) == len(f.Captures)
	for i := 0; ok && i < len(in.Args); i++ {
		ok = arg(in, i).same(f.Captures[i].Typ)
	}
	v.want(in, codeCall, ok, "captures do not match those of @%s", f.Name)
	want := Type{Kind: KindFunc, Elems: append(paramTypes(f.Params), f.callResult())}
	v.want(in, codeCall, in.Typ.same(want), "makeclosure returns %s", want)
}

// dynamic checks the operations on any: every operand and the result is any.
func (v *verifier) dynamic(in *Instr) {
	ok := true
	for i := range in.Args {
		ok = ok && arg(in, i).same(tyAny)
	}
	if in.Op.info().flags&fResult != 0 {
		ok = ok && in.Typ.same(tyAny)
	}
	v.want(in, codeType, ok, "%s works on any", in.Op)
}

func (v *verifier) callMethod(in *Instr) {
	recv := arg(in, 0)
	if recv.Kind != KindClass || recv.Nullable {
		v.report(codeCall, "callmethod needs a non-nullable class instance")
		return
	}
	fn := recv.Class.method(in.Attr)
	if fn == nil || len(fn.Params) == 0 || fn.Generator {
		v.report(codeCall, "class @%s has no plain method %s taking a receiver", recv.Class.Name, in.Attr)
		return
	}
	self := fn.Params[0].Typ
	v.want(in, codeCall, self.Kind == KindClass && recv.Class.derives(self.Class), "the receiver is not an instance of the method's class")
	v.callTypes(in, paramTypes(fn.Params[1:]), in.Args[1:], fn.Result)
}

func (v *verifier) callClosure(in *Instr) {
	f := arg(in, 0)
	if f.Kind != KindFunc || f.Nullable || len(f.Elems) == 0 {
		v.report(codeCall, "callclosure needs a function value")
		return
	}
	n := len(f.Elems) - 1
	v.callTypes(in, f.Elems[:n], in.Args[1:], f.Elems[n])
}

func (v *verifier) call(in *Instr) {
	if v.member(in) {
		v.want(in, codeCall, !in.Callee.Generator, "a generator is started with makegen")
		v.callTypes(in, paramTypes(in.Callee.Params), in.Args, in.Callee.Result)
	}
}

func paramTypes(ps []*Param) []Type {
	ts := make([]Type, len(ps))
	for i, p := range ps {
		ts[i] = p.Typ
	}
	return ts
}

// callTypes checks that args match params and the result matches result.
func (v *verifier) callTypes(in *Instr, params []Type, args []Value, result Type) {
	ok := len(params) == len(args)
	for i := 0; ok && i < len(args); i++ {
		ok = TypeOf(args[i]).same(params[i])
	}
	v.want(in, codeCall, ok, "arguments do not match the parameters of the callee")
	v.want(in, codeCall, in.Typ.same(result), "result must be %s", result)
}

func (v *verifier) member(in *Instr) bool {
	ok := in.Callee == nil || v.funcs[in.Callee]
	v.want(in, codeMember, ok, "callee is not a function of the program")
	return ok
}

func (v *verifier) unpack(in *Instr) {
	v.want(in, codeType, in.Typ.Kind == KindTuple && !in.Typ.Variadic && !in.Typ.Nullable, "unpack returns a fixed-size tuple")
}

func (v *verifier) appendTo(in *Instr) {
	c := arg(in, 0)
	v.want(in, codeType, c.Kind == KindList && !c.Nullable && arg(in, 1).same(c.at(0)) && in.Typ.same(tyVoid), "append takes a list and one of its elements")
}

func (v *verifier) length(in *Instr) {
	c := arg(in, 0)
	ok := !c.Nullable && in.Typ.same(tyInt)
	switch c.Kind {
	case KindList, KindDict, KindSet, KindTuple, KindStr, KindBytes:
	default:
		ok = false
	}
	v.want(in, codeType, ok, "len takes a sized container and returns int")
}

func (v *verifier) slice(in *Instr) {
	c := arg(in, 0)
	ok := !c.Nullable && (c.Kind == KindList || c.Kind == KindStr || c.Kind == KindBytes || c.Kind == KindTuple && c.Variadic) && in.Typ.same(c)
	for i := 1; i < 4; i++ {
		ok = ok && (arg(in, i).same(tyInt) || arg(in, i).same(tyNone))
	}
	v.want(in, codeType, ok, "slice needs a sequence, three int or none bounds, and returns the sequence type")
}

func (v *verifier) delIndex(in *Instr) {
	c := arg(in, 0)
	_, ok := subscript(c, in.Args[1])
	v.want(in, codeType, ok && (c.Kind == KindList || c.Kind == KindDict), "delindex needs a list or dict and a matching key")
}

func (v *verifier) setIndex(in *Instr) {
	c := arg(in, 0)
	elem, ok := subscript(c, in.Args[1])
	ok = ok && (c.Kind == KindList || c.Kind == KindDict) && arg(in, 2).same(elem)
	v.want(in, codeType, ok, "setindex needs a list or dict, a matching key, and a matching value")
}

func (v *verifier) index(in *Instr) {
	elem, ok := subscript(arg(in, 0), in.Args[1])
	v.want(in, codeType, ok && in.Typ.same(elem), "index needs a subscriptable operand, a matching key, and the element type as result")
}

// subscript returns the type of c[k], or false when c cannot be indexed
// by k.
func subscript(c Type, k Value) (Type, bool) {
	if c.Nullable {
		return Type{}, false
	}
	kt := TypeOf(k)
	switch c.Kind {
	case KindList:
		return c.at(0), kt.same(tyInt)
	case KindDict:
		return c.at(1), kt.same(c.at(0))
	case KindStr:
		return c, kt.same(tyInt)
	case KindBytes:
		return tyInt, kt.same(tyInt)
	case KindTuple:
		if c.Variadic {
			return c.at(0), kt.same(tyInt)
		}
		if i, ok := constIndex(k, len(c.Elems)); ok {
			return c.at(i), true
		}
	}
	return Type{}, false
}

func constIndex(k Value, n int) (int, bool) {
	c, ok := k.(*Const)
	if !ok {
		return 0, false
	}
	i, ok := c.Value.(ast.Int)
	if !ok || i.N == nil || !i.N.IsInt64() || i.N.Int64() < -math.MaxInt32 || i.N.Int64() > math.MaxInt32 {
		return 0, false
	}
	idx := int(i.N.Int64())
	if idx < 0 {
		idx += n
	}
	return idx, idx >= 0 && idx < n
}

func (v *verifier) newTuple(in *Instr) {
	t := in.Typ
	ok := t.Kind == KindTuple && !t.Nullable
	for i := range in.Args {
		want := t.at(i)
		if t.Variadic {
			want = t.at(0)
		}
		ok = ok && arg(in, i).same(want)
	}
	ok = ok && (t.Variadic || len(in.Args) == len(t.Elems))
	v.want(in, codeType, ok, "newtuple returns a tuple of its operands' types")
}

func (v *verifier) newDict(in *Instr) {
	ok := in.Typ.Kind == KindDict && !in.Typ.Nullable && len(in.Args)%2 == 0
	for i := range in.Args {
		ok = ok && arg(in, i).same(in.Typ.at(i%2))
	}
	v.want(in, codeType, ok, "newdict takes key and value pairs of the result's types")
}

func (v *verifier) newSeq(in *Instr) {
	elem := in.Typ.at(0)
	ok := (in.Typ.Kind == KindList && in.Op == OpNewList || in.Typ.Kind == KindSet && in.Op == OpNewSet) && !in.Typ.Nullable
	for i := range in.Args {
		ok = ok && arg(in, i).same(elem)
	}
	v.want(in, codeType, ok, "%s returns a container of its operands' type", in.Op)
}

func (v *verifier) nonNull(in *Instr) {
	a := arg(in, 0)
	want := a
	want.Nullable = false
	v.want(in, codeType, a.Nullable && in.Typ.same(want), "nonnull takes a nullable reference and returns its non-nullable form")
}

func (v *verifier) storeField(in *Instr) {
	if f, ok := v.fieldOf(in); ok {
		v.want(in, codeType, in.Typ.same(tyVoid) && arg(in, 1).same(f.Type), "field %s has type %s", in.Attr, f.Type)
	}
}

func (v *verifier) loadField(in *Instr) {
	if f, ok := v.fieldOf(in); ok {
		v.want(in, codeType, in.Typ.same(f.Type), "field %s has type %s", in.Attr, f.Type)
	}
}

// fieldOf returns the field that in accesses, reporting a nullable receiver
// or a missing field.
func (v *verifier) fieldOf(in *Instr) (Field, bool) {
	recv := arg(in, 0)
	if recv.Kind != KindClass {
		v.report(codeType, "%s needs a class instance", in.Op)
		return Field{}, false
	}
	if recv.Nullable {
		v.report(codeField, "%s on a nullable receiver; use nonnull first", in.Op)
		return Field{}, false
	}
	f, ok := recv.Class.field(in.Attr)
	v.want(in, codeField, ok, "class @%s has no field %s", recv.Class.Name, in.Attr)
	return f, ok
}

func (v *verifier) alloc(in *Instr) {
	v.want(in, codeMember, v.classes[in.Class], "class @%s is not a member of the program", in.Class.Name)
	v.want(in, codeType, in.Typ.Kind == KindClass && in.Typ.Class == in.Class && !in.Typ.Nullable, "alloc returns an instance of its class")
}

func (v *verifier) cast(in *Instr) {
	v.want(in, codeType, arg(in, 0).Kind.info().ref && in.Typ.Kind.info().ref, "cast converts between reference types")
}

func (v *verifier) unbox(in *Instr) {
	v.want(in, codeType, arg(in, 0).same(tyAny) && in.Typ.Kind != KindAny, "unbox turns any into a typed value")
}

func (v *verifier) box(in *Instr) {
	v.want(in, codeType, in.Typ.same(tyAny) && arg(in, 0).Kind != KindAny, "box turns a typed value into any")
}

func (v *verifier) resultI1(in *Instr) {
	v.want(in, codeType, in.Typ.same(tyI1), "%s returns i1", in.Op)
}

func (v *verifier) compare(in *Instr) {
	a := arg(in, 0)
	v.want(in, codeType, isScalar(a) && a.same(arg(in, 1)) && in.Typ.same(tyI1), "%s takes two operands of one scalar type and returns i1", in.Op)
}

func isScalar(t Type) bool {
	if t.Nullable {
		return false
	}
	for _, k := range scalars {
		if t.Kind == k {
			return true
		}
	}
	return false
}

func (v *verifier) neg(in *Instr) {
	a := arg(in, 0)
	v.want(in, codeType, a.isNum() && in.Typ.same(a), "neg takes an int or f64 and returns the same type")
}

func (v *verifier) trueDiv(in *Instr) {
	a := arg(in, 0)
	v.want(in, codeType, a.isNum() && a.same(arg(in, 1)) && in.Typ.same(tyF64), "truediv takes two ints or two f64 and returns f64")
}

func (v *verifier) arith(in *Instr) {
	a := arg(in, 0)
	v.want(in, codeType, a.isNum() && a.same(arg(in, 1)) && in.Typ.same(a), "%s takes two ints or two f64 and returns the same type", in.Op)
}

func arg(in *Instr, i int) Type {
	return TypeOf(in.Args[i])
}

// signature returns a rule for an Op with fixed operand and result types.
func signature(result Type, args ...Type) func(*verifier, *Instr) {
	return func(v *verifier, in *Instr) {
		ok := in.Typ.same(result)
		for i, a := range args {
			ok = ok && TypeOf(in.Args[i]).same(a)
		}
		v.want(in, codeType, ok, "operands and result do not match the signature of %s", in.Op)
	}
}

func (v *verifier) want(in *Instr, code int, ok bool, format string, args ...any) {
	if !ok {
		v.report(code, format, args...)
	}
}
