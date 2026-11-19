package ssa

import (
	"fmt"
	"strconv"
)

// Op is the operation of an Instr. The zero Op is invalid.
type Op uint8

type opInfo struct {
	name     string
	arity    int  // fixed operands; more are allowed when variadic
	variadic bool // operands beyond arity
	succs    int  // edges printed as operands, for jump and br
	aux      auxKind
	flags    flag
}

type flag uint8

type auxKind uint8

// Operations, grouped by the order of docs/ssa.md.
const (
	OpAdd Op = iota + 1
	OpSub
	OpMul
	OpFloorDiv
	OpMod
	OpPow
	OpTrueDiv
	OpLShift
	OpRShift
	OpAnd
	OpOr
	OpXor
	OpNeg
	OpInvert
	OpNot

	OpEq
	OpNe
	OpLt
	OpLe
	OpGt
	OpGe
	OpIs
	OpIsNot
	OpIn
	OpNotIn

	OpInt2F64
	OpF642Int
	OpBox
	OpUnbox
	OpCast

	OpAlloc
	OpLoadField
	OpStoreField
	OpNonNull
	OpNewList
	OpNewTuple
	OpNewDict
	OpNewSet
	OpIndex
	OpSetIndex
	OpDelIndex
	OpSlice
	OpLen
	OpAppend
	OpUnpack

	OpCall
	OpCallClosure
	OpCallMethod
	OpDynCall

	OpMakeClosure
	OpNewCell
	OpLoadCell
	OpStoreCell

	OpLoadGlobal
	OpStoreGlobal
	OpDelGlobal

	OpIter
	OpNext
	OpCurrent

	OpConcat
	OpFormat

	OpRaise
	OpReraise
	OpLandingPad
	OpIsInstance
	OpTypeOf
	OpClassObj

	OpMakeGen
	OpYield
	OpResume

	OpDynBinOp
	OpDynGetAttr
	OpDynSetAttr

	OpSelect
	OpUnreachable
	OpJump
	OpBr
	OpRet
)

const opEnd = OpRet + 1

const (
	fTerm    flag = 1 << iota // ends the block
	fRaise                    // may carry an exception edge
	fResult                   // defines a value
	fCall                     // operands after the head are printed in parentheses
	fBracket                  // operands after the callee are printed in brackets
	fDot                      // the attribute follows operand 0 after a dot
)

const (
	auxNone auxKind = iota
	auxCallee
	auxGlobal
	auxClass
	auxAttr
)

var ops = [opEnd]opInfo{
	OpAdd:      {name: "add", arity: 2, flags: fResult},
	OpSub:      {name: "sub", arity: 2, flags: fResult},
	OpMul:      {name: "mul", arity: 2, flags: fResult},
	OpFloorDiv: {name: "floordiv", arity: 2, flags: fResult | fRaise},
	OpMod:      {name: "mod", arity: 2, flags: fResult | fRaise},
	OpPow:      {name: "pow", arity: 2, flags: fResult | fRaise},
	OpTrueDiv:  {name: "truediv", arity: 2, flags: fResult | fRaise},
	OpLShift:   {name: "lshift", arity: 2, flags: fResult | fRaise},
	OpRShift:   {name: "rshift", arity: 2, flags: fResult | fRaise},
	OpAnd:      {name: "and", arity: 2, flags: fResult},
	OpOr:       {name: "or", arity: 2, flags: fResult},
	OpXor:      {name: "xor", arity: 2, flags: fResult},
	OpNeg:      {name: "neg", arity: 1, flags: fResult},
	OpInvert:   {name: "invert", arity: 1, flags: fResult},
	OpNot:      {name: "not", arity: 1, flags: fResult},

	OpEq:    {name: "eq", arity: 2, flags: fResult},
	OpNe:    {name: "ne", arity: 2, flags: fResult},
	OpLt:    {name: "lt", arity: 2, flags: fResult},
	OpLe:    {name: "le", arity: 2, flags: fResult},
	OpGt:    {name: "gt", arity: 2, flags: fResult},
	OpGe:    {name: "ge", arity: 2, flags: fResult},
	OpIs:    {name: "is", arity: 2, flags: fResult},
	OpIsNot: {name: "isnot", arity: 2, flags: fResult},
	OpIn:    {name: "in", arity: 2, flags: fResult},
	OpNotIn: {name: "notin", arity: 2, flags: fResult},

	OpInt2F64: {name: "int2f64", arity: 1, flags: fResult | fRaise},
	OpF642Int: {name: "f642int", arity: 1, flags: fResult | fRaise},
	OpBox:     {name: "box", arity: 1, flags: fResult},
	OpUnbox:   {name: "unbox", arity: 1, flags: fResult | fRaise},
	OpCast:    {name: "cast", arity: 1, flags: fResult | fRaise},

	OpAlloc:      {name: "alloc", aux: auxClass, flags: fResult},
	OpLoadField:  {name: "loadfield", arity: 1, aux: auxAttr, flags: fResult | fDot},
	OpStoreField: {name: "storefield", arity: 2, aux: auxAttr, flags: fDot},
	OpNonNull:    {name: "nonnull", arity: 1, aux: auxAttr, flags: fResult | fRaise | fDot},
	OpNewList:    {name: "newlist", variadic: true, flags: fResult},
	OpNewTuple:   {name: "newtuple", variadic: true, flags: fResult},
	OpNewDict:    {name: "newdict", variadic: true, flags: fResult},
	OpNewSet:     {name: "newset", variadic: true, flags: fResult},
	OpIndex:      {name: "index", arity: 2, flags: fResult | fRaise},
	OpSetIndex:   {name: "setindex", arity: 3, flags: fRaise},
	OpDelIndex:   {name: "delindex", arity: 2, flags: fRaise},
	OpSlice:      {name: "slice", arity: 4, flags: fResult | fRaise},
	OpLen:        {name: "len", arity: 1, flags: fResult},
	OpAppend:     {name: "append", arity: 2},
	OpUnpack:     {name: "unpack", arity: 1, flags: fResult | fRaise},

	OpCall:        {name: "call", variadic: true, aux: auxCallee, flags: fResult | fRaise | fCall},
	OpCallClosure: {name: "callclosure", arity: 1, variadic: true, flags: fResult | fRaise | fCall},
	OpCallMethod:  {name: "callmethod", arity: 1, variadic: true, aux: auxAttr, flags: fResult | fRaise | fCall | fDot},
	OpDynCall:     {name: "dyncall", arity: 1, variadic: true, flags: fResult | fRaise | fCall},

	OpMakeClosure: {name: "makeclosure", variadic: true, aux: auxCallee, flags: fResult | fBracket},
	OpNewCell:     {name: "newcell", arity: 1, flags: fResult},
	OpLoadCell:    {name: "loadcell", arity: 1, flags: fResult},
	OpStoreCell:   {name: "storecell", arity: 2},

	OpLoadGlobal:  {name: "loadglobal", aux: auxGlobal, flags: fResult | fRaise},
	OpStoreGlobal: {name: "storeglobal", arity: 1, aux: auxGlobal},
	OpDelGlobal:   {name: "delglobal", aux: auxGlobal, flags: fRaise},

	OpIter:    {name: "iter", arity: 1, flags: fResult | fRaise},
	OpNext:    {name: "next", arity: 1, flags: fResult | fRaise},
	OpCurrent: {name: "current", arity: 1, flags: fResult},

	OpConcat: {name: "concat", arity: 2, flags: fResult},
	OpFormat: {name: "format", arity: 2, flags: fResult | fRaise},

	OpRaise:      {name: "raise", arity: 1, flags: fTerm},
	OpReraise:    {name: "reraise", arity: 1, flags: fTerm},
	OpLandingPad: {name: "landingpad", flags: fResult},
	OpIsInstance: {name: "isinstance", arity: 1, aux: auxClass, flags: fResult},
	OpTypeOf:     {name: "typeof", arity: 1, flags: fResult},
	OpClassObj:   {name: "classobj", aux: auxClass, flags: fResult},

	OpMakeGen: {name: "makegen", variadic: true, aux: auxCallee, flags: fResult | fCall},
	OpYield:   {name: "yield", arity: 1, flags: fResult | fRaise},
	OpResume:  {name: "resume", arity: 2, flags: fResult | fRaise},

	OpDynBinOp:   {name: "dynbinop", arity: 2, aux: auxAttr, flags: fResult | fRaise | fDot},
	OpDynGetAttr: {name: "dyngetattr", arity: 1, aux: auxAttr, flags: fResult | fRaise | fDot},
	OpDynSetAttr: {name: "dynsetattr", arity: 2, aux: auxAttr, flags: fRaise | fDot},

	OpSelect:      {name: "select", arity: 3, flags: fResult},
	OpUnreachable: {name: "unreachable", flags: fTerm},
	OpJump:        {name: "jump", succs: 1, flags: fTerm},
	OpBr:          {name: "br", arity: 1, succs: 2, flags: fTerm},
	OpRet:         {name: "ret", variadic: true, flags: fTerm},
}

var opByName = func() map[string]Op {
	m := make(map[string]Op, len(ops))
	for op := OpAdd; op < opEnd; op++ {
		m[ops[op].name] = op
	}
	return m
}()

// String returns the mnemonic of o.
func (o Op) String() string {
	if name := o.info().name; name != "" {
		return name
	}
	return "op(" + strconv.Itoa(int(o)) + ")"
}

// shape reports why in cannot be printed or analysed: wrong operand count,
// missing or stray auxiliary fields, wrong edge count, or a nil operand. It
// returns "" for a well-formed instruction.
func (in *Instr) shape() string {
	info := in.Op.info()
	if info.name == "" {
		return "unknown operation"
	}
	n := len(in.Args)
	switch {
	case n < info.arity || (!info.variadic && n > info.arity):
		return fmt.Sprintf("%s takes %d operands, has %d", info.name, info.arity, n)
	case in.Op == OpRet && n > 1:
		return "ret takes at most one operand"
	}
	if msg := in.auxShape(info); msg != "" {
		return msg
	}
	return in.edgeShape(info)
}

func (in *Instr) edgeShape(info *opInfo) string {
	want := info.succs
	if in.Except != nil {
		if info.flags&fRaise == 0 {
			return info.name + " cannot carry an exception edge"
		}
		want = 1
	}
	if len(in.Succs) != want {
		return fmt.Sprintf("%s takes %d edges, has %d", info.name, want, len(in.Succs))
	}
	for i, a := range in.Args {
		if a == nil {
			return fmt.Sprintf("operand %d is nil", i)
		}
		if _, ok := a.(*Const); ok && i == 0 && info.flags&(fCall|fDot) != 0 && info.aux != auxCallee {
			return "operand 0 must not be a constant"
		}
	}
	edges := in.Succs
	if in.Except != nil {
		edges = append(append([]Edge(nil), edges...), *in.Except)
	}
	for _, e := range edges {
		if e.Block == nil {
			return "edge without a target block"
		}
		for _, a := range e.Args {
			if a == nil {
				return "edge argument is nil"
			}
		}
	}
	return ""
}

func (in *Instr) auxShape(info *opInfo) string {
	aux := [...]struct {
		kind auxKind
		set  bool
		name string
	}{
		{auxCallee, in.Callee != nil, "callee"},
		{auxGlobal, in.Global != nil, "global"},
		{auxClass, in.Class != nil, "class"},
		{auxAttr, in.Attr != "", "attribute"},
	}
	for _, a := range aux {
		if want := info.aux == a.kind; want != a.set {
			if want {
				return fmt.Sprintf("%s needs a %s", info.name, a.name)
			}
			return fmt.Sprintf("%s takes no %s", info.name, a.name)
		}
	}
	return ""
}

func (o Op) info() *opInfo {
	if o < opEnd {
		return &ops[o]
	}
	return &ops[0]
}
