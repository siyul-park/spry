// Code generated is not used; this file is hand-maintained. Node structs follow
// CPython 3.14 Parser/Python.asdl; ast_test.go checks them against the schema.

package ast

import (
	"math/big"
	"strconv"
	"unicode/utf8"

	"github.com/siyul-park/spry/token"
)

// Node is a Python syntax tree node. Pos is the first byte of the node and
// End is one past its last byte. Nodes without CPython location attributes
// (Module, Arguments, Comprehension, WithItem, MatchCase, TypeIgnore ...)
// derive them from their children, or report token.NoPos when they have none.
// Operators and expression contexts are enums, not Nodes.
type Node interface {
	Pos() token.Pos
	End() token.Pos
}

// Mod is a top-level node: Module, Interactive, Expression or FunctionType.
type Mod interface {
	Node
	modNode()
}

// Stmt is a statement node.
type Stmt interface {
	Node
	stmtNode()
}

// Expr is an expression node.
type Expr interface {
	Node
	exprNode()
}

// Pattern is a match statement pattern node.
type Pattern interface {
	Node
	patternNode()
}

// TypeParam is a PEP 695 type parameter node: TypeVar, ParamSpec or TypeVarTuple.
type TypeParam interface {
	Node
	typeParamNode()
}

// Value is the value of a Constant or MatchSingleton: Int, Float, Complex,
// Str, Bytes, Bool, None or Ellipsis.
type Value interface {
	value()
}

// Module is the CPython Module mod node.
type Module struct {
	Body        []Stmt
	TypeIgnores []*TypeIgnore `py:"type_ignores"`
}

// Interactive is the CPython Interactive mod node.
type Interactive struct {
	Body []Stmt
}

// Expression is the CPython Expression mod node.
type Expression struct {
	Body Expr
}

// FunctionType is the CPython FunctionType mod node.
type FunctionType struct {
	ArgTypes []Expr `py:"argtypes"`
	Returns  Expr
}

// FunctionDef is the CPython FunctionDef statement node.
type FunctionDef struct {
	Span
	Name          string
	Args          *Arguments
	Body          []Stmt
	DecoratorList []Expr `py:"decorator_list"`
	Returns       Expr
	TypeComment   *string     `py:"type_comment"`
	TypeParams    []TypeParam `py:"type_params"`
}

// AsyncFunctionDef is the CPython AsyncFunctionDef statement node.
type AsyncFunctionDef struct {
	Span
	Name          string
	Args          *Arguments
	Body          []Stmt
	DecoratorList []Expr `py:"decorator_list"`
	Returns       Expr
	TypeComment   *string     `py:"type_comment"`
	TypeParams    []TypeParam `py:"type_params"`
}

// ClassDef is the CPython ClassDef statement node.
type ClassDef struct {
	Span
	Name          string
	Bases         []Expr
	Keywords      []*Keyword
	Body          []Stmt
	DecoratorList []Expr      `py:"decorator_list"`
	TypeParams    []TypeParam `py:"type_params"`
}

// Return is the CPython Return statement node.
type Return struct {
	Span
	Value Expr
}

// Delete is the CPython Delete statement node.
type Delete struct {
	Span
	Targets []Expr
}

// Assign is the CPython Assign statement node.
type Assign struct {
	Span
	Targets     []Expr
	Value       Expr
	TypeComment *string `py:"type_comment"`
}

// TypeAlias is the CPython TypeAlias statement node.
type TypeAlias struct {
	Span
	Name       Expr
	TypeParams []TypeParam `py:"type_params"`
	Value      Expr
}

// AugAssign is the CPython AugAssign statement node.
type AugAssign struct {
	Span
	Target Expr
	Op     Operator
	Value  Expr
}

// AnnAssign is the CPython AnnAssign statement node.
type AnnAssign struct {
	Span
	Target     Expr
	Annotation Expr
	Value      Expr
	Simple     int
}

// For is the CPython For statement node.
type For struct {
	Span
	Target      Expr
	Iter        Expr
	Body        []Stmt
	OrElse      []Stmt  `py:"orelse"`
	TypeComment *string `py:"type_comment"`
}

// AsyncFor is the CPython AsyncFor statement node.
type AsyncFor struct {
	Span
	Target      Expr
	Iter        Expr
	Body        []Stmt
	OrElse      []Stmt  `py:"orelse"`
	TypeComment *string `py:"type_comment"`
}

// While is the CPython While statement node.
type While struct {
	Span
	Test   Expr
	Body   []Stmt
	OrElse []Stmt `py:"orelse"`
}

// If is the CPython If statement node.
type If struct {
	Span
	Test   Expr
	Body   []Stmt
	OrElse []Stmt `py:"orelse"`
}

// With is the CPython With statement node.
type With struct {
	Span
	Items       []*WithItem
	Body        []Stmt
	TypeComment *string `py:"type_comment"`
}

// AsyncWith is the CPython AsyncWith statement node.
type AsyncWith struct {
	Span
	Items       []*WithItem
	Body        []Stmt
	TypeComment *string `py:"type_comment"`
}

// Match is the CPython Match statement node.
type Match struct {
	Span
	Subject Expr
	Cases   []*MatchCase
}

// Raise is the CPython Raise statement node.
type Raise struct {
	Span
	Exc   Expr
	Cause Expr
}

// Try is the CPython Try statement node.
type Try struct {
	Span
	Body      []Stmt
	Handlers  []*ExceptHandler
	OrElse    []Stmt `py:"orelse"`
	FinalBody []Stmt `py:"finalbody"`
}

// TryStar is the CPython TryStar statement node.
type TryStar struct {
	Span
	Body      []Stmt
	Handlers  []*ExceptHandler
	OrElse    []Stmt `py:"orelse"`
	FinalBody []Stmt `py:"finalbody"`
}

// Assert is the CPython Assert statement node.
type Assert struct {
	Span
	Test Expr
	Msg  Expr
}

// Import is the CPython Import statement node.
type Import struct {
	Span
	Names []*Alias
}

// ImportFrom is the CPython ImportFrom statement node.
type ImportFrom struct {
	Span
	Module *string
	Names  []*Alias
	Level  *int
}

// Global is the CPython Global statement node.
type Global struct {
	Span
	Names []string
}

// Nonlocal is the CPython Nonlocal statement node.
type Nonlocal struct {
	Span
	Names []string
}

// ExprStmt is the CPython Expr statement node (Python class Expr).
type ExprStmt struct {
	Span
	Value Expr
}

// Pass is the CPython Pass statement node.
type Pass struct {
	Span
}

// Break is the CPython Break statement node.
type Break struct {
	Span
}

// Continue is the CPython Continue statement node.
type Continue struct {
	Span
}

// BoolOp is the CPython BoolOp expression node.
type BoolOp struct {
	Span
	Op     BoolOperator
	Values []Expr
}

// NamedExpr is the CPython NamedExpr expression node.
type NamedExpr struct {
	Span
	Target Expr
	Value  Expr
}

// BinOp is the CPython BinOp expression node.
type BinOp struct {
	Span
	Left  Expr
	Op    Operator
	Right Expr
}

// UnaryOp is the CPython UnaryOp expression node.
type UnaryOp struct {
	Span
	Op      UnaryOperator
	Operand Expr
}

// Lambda is the CPython Lambda expression node.
type Lambda struct {
	Span
	Args *Arguments
	Body Expr
}

// IfExp is the CPython IfExp expression node.
type IfExp struct {
	Span
	Test   Expr
	Body   Expr
	OrElse Expr `py:"orelse"`
}

// Dict is the CPython Dict expression node.
type Dict struct {
	Span
	Keys   []Expr
	Values []Expr
}

// Set is the CPython Set expression node.
type Set struct {
	Span
	Elts []Expr
}

// ListComp is the CPython ListComp expression node.
type ListComp struct {
	Span
	Elt        Expr
	Generators []*Comprehension
}

// SetComp is the CPython SetComp expression node.
type SetComp struct {
	Span
	Elt        Expr
	Generators []*Comprehension
}

// DictComp is the CPython DictComp expression node.
type DictComp struct {
	Span
	Key        Expr
	Value      Expr
	Generators []*Comprehension
}

// GeneratorExp is the CPython GeneratorExp expression node.
type GeneratorExp struct {
	Span
	Elt        Expr
	Generators []*Comprehension
}

// Await is the CPython Await expression node.
type Await struct {
	Span
	Value Expr
}

// Yield is the CPython Yield expression node.
type Yield struct {
	Span
	Value Expr
}

// YieldFrom is the CPython YieldFrom expression node.
type YieldFrom struct {
	Span
	Value Expr
}

// Compare is the CPython Compare expression node.
type Compare struct {
	Span
	Left        Expr
	Ops         []CmpOperator
	Comparators []Expr
}

// Call is the CPython Call expression node.
type Call struct {
	Span
	Func     Expr
	Args     []Expr
	Keywords []*Keyword
}

// FormattedValue is the CPython FormattedValue expression node.
type FormattedValue struct {
	Span
	Value      Expr
	Conversion int
	FormatSpec Expr `py:"format_spec"`
}

// Interpolation is the CPython Interpolation expression node.
type Interpolation struct {
	Span
	Value      Expr
	Str        string
	Conversion int
	FormatSpec Expr `py:"format_spec"`
}

// JoinedStr is the CPython JoinedStr expression node.
type JoinedStr struct {
	Span
	Values []Expr
}

// TemplateStr is the CPython TemplateStr expression node.
type TemplateStr struct {
	Span
	Values []Expr
}

// Constant is the CPython Constant expression node.
type Constant struct {
	Span
	Value Value
	Kind  *string
}

// Attribute is the CPython Attribute expression node.
type Attribute struct {
	Span
	Value Expr
	Attr  string
	Ctx   ExprContext
}

// Subscript is the CPython Subscript expression node.
type Subscript struct {
	Span
	Value Expr
	Slice Expr
	Ctx   ExprContext
}

// Starred is the CPython Starred expression node.
type Starred struct {
	Span
	Value Expr
	Ctx   ExprContext
}

// Name is the CPython Name expression node.
type Name struct {
	Span
	ID  string `py:"id"`
	Ctx ExprContext
}

// List is the CPython List expression node.
type List struct {
	Span
	Elts []Expr
	Ctx  ExprContext
}

// Tuple is the CPython Tuple expression node.
type Tuple struct {
	Span
	Elts []Expr
	Ctx  ExprContext
}

// Slice is the CPython Slice expression node.
type Slice struct {
	Span
	Lower Expr
	Upper Expr
	Step  Expr
}

// MatchValue is the CPython MatchValue pattern node.
type MatchValue struct {
	Span
	Value Expr
}

// MatchSingleton is the CPython MatchSingleton pattern node.
type MatchSingleton struct {
	Span
	Value Value
}

// MatchSequence is the CPython MatchSequence pattern node.
type MatchSequence struct {
	Span
	Patterns []Pattern
}

// MatchMapping is the CPython MatchMapping pattern node.
type MatchMapping struct {
	Span
	Keys     []Expr
	Patterns []Pattern
	Rest     *string
}

// MatchClass is the CPython MatchClass pattern node.
type MatchClass struct {
	Span
	Cls         Expr
	Patterns    []Pattern
	KwdAttrs    []string  `py:"kwd_attrs"`
	KwdPatterns []Pattern `py:"kwd_patterns"`
}

// MatchStar is the CPython MatchStar pattern node.
type MatchStar struct {
	Span
	Name *string
}

// MatchAs is the CPython MatchAs pattern node.
type MatchAs struct {
	Span
	Pattern Pattern
	Name    *string
}

// MatchOr is the CPython MatchOr pattern node.
type MatchOr struct {
	Span
	Patterns []Pattern
}

// TypeVar is the CPython TypeVar type parameter node.
type TypeVar struct {
	Span
	Name         string
	Bound        Expr
	DefaultValue Expr `py:"default_value"`
}

// ParamSpec is the CPython ParamSpec type parameter node.
type ParamSpec struct {
	Span
	Name         string
	DefaultValue Expr `py:"default_value"`
}

// TypeVarTuple is the CPython TypeVarTuple type parameter node.
type TypeVarTuple struct {
	Span
	Name         string
	DefaultValue Expr `py:"default_value"`
}

// Comprehension is the CPython comprehension helper node (Python class comprehension).
type Comprehension struct {
	Target  Expr
	Iter    Expr
	Ifs     []Expr
	IsAsync int `py:"is_async"`
}

// ExceptHandler is the CPython ExceptHandler helper node.
type ExceptHandler struct {
	Span
	Type Expr
	Name *string
	Body []Stmt
}

// Arguments is the CPython arguments helper node (Python class arguments).
type Arguments struct {
	PosOnlyArgs []*Arg `py:"posonlyargs"`
	Args        []*Arg
	VarArg      *Arg   `py:"vararg"`
	KwOnlyArgs  []*Arg `py:"kwonlyargs"`
	KwDefaults  []Expr `py:"kw_defaults"`
	KwArg       *Arg   `py:"kwarg"`
	Defaults    []Expr
}

// Arg is the CPython arg helper node (Python class arg).
type Arg struct {
	Span
	Arg         string
	Annotation  Expr
	TypeComment *string `py:"type_comment"`
}

// Keyword is the CPython keyword helper node (Python class keyword).
type Keyword struct {
	Span
	Arg   *string
	Value Expr
}

// Alias is the CPython alias helper node (Python class alias).
type Alias struct {
	Span
	Name   string
	AsName *string `py:"asname"`
}

// WithItem is the CPython withitem helper node (Python class withitem).
type WithItem struct {
	ContextExpr  Expr `py:"context_expr"`
	OptionalVars Expr `py:"optional_vars"`
}

// MatchCase is the CPython match_case helper node (Python class match_case).
type MatchCase struct {
	Pattern Pattern
	Guard   Expr
	Body    []Stmt
}

// TypeIgnore is the CPython TypeIgnore helper node.
type TypeIgnore struct {
	Lineno int
	Tag    string
}

// Span is the extent [StartPos, EndPos) of a node that has CPython location
// attributes (lineno, col_offset, end_lineno, end_col_offset); see Location.
type Span struct {
	StartPos, EndPos token.Pos
}

// Location holds the CPython location attributes of a node: Lineno and
// EndLineno are 1-based lines, ColOffset and EndColOffset are 0-based UTF-8
// byte columns. See Locate.
type Location struct {
	Lineno, ColOffset, EndLineno, EndColOffset int
}

// Int is a Python int of arbitrary size.
type Int struct{ N *big.Int }

// Float is a Python float.
type Float float64

// Complex is a Python complex. A literal only has an imaginary part, but
// constant folding and hand-built trees may set both.
type Complex complex128

// Str is a Python str in WTF-8: UTF-8 in which a lone surrogate (U+D800 to
// U+DFFF, which "\ud800" produces) is encoded as its three-byte form. Use
// AppendRune to build one from code points.
type Str string

// Bytes is a Python bytes.
type Bytes []byte

// Bool is True or False.
type Bool bool

// None is Python None.
type None struct{}

// Ellipsis is Python Ellipsis (the literal ...).
type Ellipsis struct{}

// Operator is a binary operator: CPython's operator.
type Operator int

// UnaryOperator is a unary operator: CPython's unaryop.
type UnaryOperator int

// CmpOperator is a comparison operator: CPython's cmpop.
type CmpOperator int

// BoolOperator is and or or: CPython's boolop.
type BoolOperator int

// ExprContext is how a name, attribute, subscript, starred, list or tuple is
// used. The zero value is CtxLoad, CPython's default.
type ExprContext int

// Operator values, named as CPython's operator classes with an Op prefix.
const (
	OpAdd Operator = iota
	OpSub
	OpMult
	OpMatMult
	OpDiv
	OpMod
	OpPow
	OpLShift
	OpRShift
	OpBitOr
	OpBitXor
	OpBitAnd
	OpFloorDiv
)

// UnaryOperator values, with a Unary prefix.
const (
	UnaryInvert UnaryOperator = iota
	UnaryNot
	UnaryUAdd
	UnaryUSub
)

// CmpOperator values, with a Cmp prefix.
const (
	CmpEq CmpOperator = iota
	CmpNotEq
	CmpLt
	CmpLtE
	CmpGt
	CmpGtE
	CmpIs
	CmpIsNot
	CmpIn
	CmpNotIn
)

// BoolOperator values, with a Bool prefix.
const (
	BoolAnd BoolOperator = iota
	BoolOr
)

// ExprContext values, with a Ctx prefix.
const (
	CtxLoad ExprContext = iota
	CtxStore
	CtxDel
)

var operatorNames = [...]string{"Add", "Sub", "Mult", "MatMult", "Div", "Mod", "Pow", "LShift", "RShift", "BitOr", "BitXor", "BitAnd", "FloorDiv"}
var unaryOperatorNames = [...]string{"Invert", "Not", "UAdd", "USub"}
var cmpOperatorNames = [...]string{"Eq", "NotEq", "Lt", "LtE", "Gt", "GtE", "Is", "IsNot", "In", "NotIn"}
var boolOperatorNames = [...]string{"And", "Or"}
var exprContextNames = [...]string{"Load", "Store", "Del"}

// Locate returns the CPython location attributes of n. It returns the zero
// Location when n has no valid position in fset, and zero end fields when only
// the end is missing.
func Locate(fset *token.FileSet, n Node) Location {
	if !n.Pos().IsValid() {
		return Location{}
	}
	start, end := fset.Position(n.Pos()), fset.Position(n.End())
	if !start.IsValid() {
		return Location{}
	}
	if !end.IsValid() {
		return Location{Lineno: start.Line, ColOffset: start.Column - 1}
	}
	return Location{Lineno: start.Line, ColOffset: start.Column - 1, EndLineno: end.Line, EndColOffset: end.Column - 1}
}

// AppendRune appends the WTF-8 encoding of r to dst, as the bytes of a Str.
// Unlike utf8.AppendRune it keeps a lone surrogate instead of replacing it.
// A rune outside [0, 0x10FFFF] becomes U+FFFD.
func AppendRune(dst []byte, r rune) []byte {
	if r >= 0xD800 && r <= 0xDFFF {
		return append(dst, 0xE0|byte(r>>12), 0x80|byte(r>>6)&0x3F, 0x80|byte(r)&0x3F)
	}
	return utf8.AppendRune(dst, r)
}

// Pos returns the position of the first statement or type ignore, or NoPos.
func (m *Module) Pos() token.Pos {
	return first(m)
}

// End returns the end of the last child, or NoPos.
func (m *Module) End() token.Pos {
	return last(m)
}

// Pos returns the position of the first statement, or NoPos.
func (m *Interactive) Pos() token.Pos {
	return first(m)
}

// End returns the end of the last statement, or NoPos.
func (m *Interactive) End() token.Pos {
	return last(m)
}

// Pos returns the position of the body, or NoPos.
func (m *Expression) Pos() token.Pos {
	return first(m)
}

// End returns the end of the body, or NoPos.
func (m *Expression) End() token.Pos {
	return last(m)
}

// Pos returns the position of the first argument type, or the return type, or NoPos.
func (m *FunctionType) Pos() token.Pos {
	return first(m)
}

// End returns the end of the return type, or NoPos.
func (m *FunctionType) End() token.Pos {
	return last(m)
}

// Pos returns the position of the target; the for keyword is not included, or NoPos.
func (c *Comprehension) Pos() token.Pos {
	return first(c)
}

// End returns the end of the last condition or the iterable, or NoPos.
func (c *Comprehension) End() token.Pos {
	return last(c)
}

// Pos returns the position of the first parameter or default, or NoPos.
func (a *Arguments) Pos() token.Pos {
	return first(a)
}

// End returns the end of the last parameter or default, or NoPos.
func (a *Arguments) End() token.Pos {
	return last(a)
}

// Pos returns the position of the context expression, or NoPos.
func (w *WithItem) Pos() token.Pos {
	return first(w)
}

// End returns the end of the variables, or the context expression, or NoPos.
func (w *WithItem) End() token.Pos {
	return last(w)
}

// Pos returns the position of the pattern; the case keyword is not included, or NoPos.
func (c *MatchCase) Pos() token.Pos {
	return first(c)
}

// End returns the end of the last body statement, or NoPos.
func (c *MatchCase) End() token.Pos {
	return last(c)
}

// Pos returns NoPos: a TypeIgnore has a line number but no byte position.
func (*TypeIgnore) Pos() token.Pos {
	return token.NoPos
}

// End returns NoPos.
func (*TypeIgnore) End() token.Pos {
	return token.NoPos
}

// Pos returns the start of s.
func (s Span) Pos() token.Pos {
	return s.StartPos
}

// End returns the end of s.
func (s Span) End() token.Pos {
	return s.EndPos
}

// String returns the CPython class name, such as "Add".
func (o Operator) String() string {
	return enumName(operatorNames[:], int(o), "Operator")
}

// String returns the CPython class name, such as "Not".
func (o UnaryOperator) String() string {
	return enumName(unaryOperatorNames[:], int(o), "UnaryOperator")
}

// String returns the CPython class name, such as "NotEq".
func (o CmpOperator) String() string {
	return enumName(cmpOperatorNames[:], int(o), "CmpOperator")
}

// String returns the CPython class name, "And" or "Or".
func (o BoolOperator) String() string {
	return enumName(boolOperatorNames[:], int(o), "BoolOperator")
}

// String returns the CPython class name, "Load", "Store" or "Del".
func (c ExprContext) String() string {
	return enumName(exprContextNames[:], int(c), "ExprContext")
}

func (*Module) modNode()             {}
func (*Interactive) modNode()        {}
func (*Expression) modNode()         {}
func (*FunctionType) modNode()       {}
func (*FunctionDef) stmtNode()       {}
func (*AsyncFunctionDef) stmtNode()  {}
func (*ClassDef) stmtNode()          {}
func (*Return) stmtNode()            {}
func (*Delete) stmtNode()            {}
func (*Assign) stmtNode()            {}
func (*TypeAlias) stmtNode()         {}
func (*AugAssign) stmtNode()         {}
func (*AnnAssign) stmtNode()         {}
func (*For) stmtNode()               {}
func (*AsyncFor) stmtNode()          {}
func (*While) stmtNode()             {}
func (*If) stmtNode()                {}
func (*With) stmtNode()              {}
func (*AsyncWith) stmtNode()         {}
func (*Match) stmtNode()             {}
func (*Raise) stmtNode()             {}
func (*Try) stmtNode()               {}
func (*TryStar) stmtNode()           {}
func (*Assert) stmtNode()            {}
func (*Import) stmtNode()            {}
func (*ImportFrom) stmtNode()        {}
func (*Global) stmtNode()            {}
func (*Nonlocal) stmtNode()          {}
func (*ExprStmt) stmtNode()          {}
func (*Pass) stmtNode()              {}
func (*Break) stmtNode()             {}
func (*Continue) stmtNode()          {}
func (*BoolOp) exprNode()            {}
func (*NamedExpr) exprNode()         {}
func (*BinOp) exprNode()             {}
func (*UnaryOp) exprNode()           {}
func (*Lambda) exprNode()            {}
func (*IfExp) exprNode()             {}
func (*Dict) exprNode()              {}
func (*Set) exprNode()               {}
func (*ListComp) exprNode()          {}
func (*SetComp) exprNode()           {}
func (*DictComp) exprNode()          {}
func (*GeneratorExp) exprNode()      {}
func (*Await) exprNode()             {}
func (*Yield) exprNode()             {}
func (*YieldFrom) exprNode()         {}
func (*Compare) exprNode()           {}
func (*Call) exprNode()              {}
func (*FormattedValue) exprNode()    {}
func (*Interpolation) exprNode()     {}
func (*JoinedStr) exprNode()         {}
func (*TemplateStr) exprNode()       {}
func (*Constant) exprNode()          {}
func (*Attribute) exprNode()         {}
func (*Subscript) exprNode()         {}
func (*Starred) exprNode()           {}
func (*Name) exprNode()              {}
func (*List) exprNode()              {}
func (*Tuple) exprNode()             {}
func (*Slice) exprNode()             {}
func (*MatchValue) patternNode()     {}
func (*MatchSingleton) patternNode() {}
func (*MatchSequence) patternNode()  {}
func (*MatchMapping) patternNode()   {}
func (*MatchClass) patternNode()     {}
func (*MatchStar) patternNode()      {}
func (*MatchAs) patternNode()        {}
func (*MatchOr) patternNode()        {}
func (*TypeVar) typeParamNode()      {}
func (*ParamSpec) typeParamNode()    {}
func (*TypeVarTuple) typeParamNode() {}

func (Int) value()      {}
func (Float) value()    {}
func (Complex) value()  {}
func (Str) value()      {}
func (Bytes) value()    {}
func (Bool) value()     {}
func (None) value()     {}
func (Ellipsis) value() {}

// first returns the smallest valid position among the children of n.
func first(n Node) token.Pos {
	pos := token.NoPos
	eachChild(n, func(c Node) {
		if p := c.Pos(); p.IsValid() && (!pos.IsValid() || p < pos) {
			pos = p
		}
	})
	return pos
}

// last returns the largest end among the children of n.
func last(n Node) token.Pos {
	end := token.NoPos
	eachChild(n, func(c Node) {
		if e := c.End(); e > end {
			end = e
		}
	})
	return end
}

func enumName(names []string, i int, typ string) string {
	if i < 0 || i >= len(names) {
		return typ + "(" + strconv.Itoa(i) + ")"
	}
	return names[i]
}
