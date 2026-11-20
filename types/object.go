package types

import (
	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/token"
)

// Object is a named language entity: a class, type parameter, alias,
// variable, function, module name or builtin.
type Object interface {
	Parent() *Scope
	Pos() token.Pos
	Module() *Module
	Name() string
	Type() Type
	String() string

	setParent(*Scope)
}

// TypeName is a class, type parameter, type alias or NewType. As an
// annotation it denotes Type(); the class object itself is type[Type()].
type TypeName struct {
	object
	qualname string
	alias    bool
}

// Var is a variable, parameter or attribute.
type Var struct {
	object
	kind       VarKind
	paramKind  ParamKind
	hasDefault bool
	quals      Qualifiers
	cell       bool
	annot      bool
	origin     *Var
}

// Func is a function or method declaration.
type Func struct {
	object
	sig      *Signature
	defs     []ast.Stmt
	body     *ast.Module
	scope    *Scope
	fullName string
	deco     funcKind
}

// ModuleName is the binding of an imported module.
type ModuleName struct {
	object
	imported *Module
}

// Builtin is a special form or function known to the checker, such as
// isinstance, super, cast, Union, Literal or TypeVar.
type Builtin struct {
	object
	form formKind
}

// VarKind classifies a Var by where it lives.
type VarKind uint8

// ParamKind classifies a parameter like inspect.Parameter.kind. The order is
// the order in which kinds appear in a parameter list.
type ParamKind uint8

// Qualifiers is a set of type qualifiers of a variable.
type Qualifiers uint8

// ParamOption configures NewParam.
type ParamOption func(*Var)

type object struct {
	parent *Scope
	pos    token.Pos
	module *Module
	name   string
	typ    Type
}

type funcKind uint8

// Variable kinds.
const (
	GlobalVar VarKind = iota
	LocalVar
	ParamVar
	FieldVar
	ClassVar
)

// Parameter kinds.
const (
	PositionalOnly ParamKind = iota
	PositionalOrKeyword
	VarPositional
	KeywordOnly
	VarKeyword
)

// Type qualifiers.
const (
	Final Qualifiers = 1 << iota
	ReadOnly
	NotRequired
	InitVar
)

const (
	plainFunc funcKind = iota
	staticFunc
	classFunc
	propertyFunc
)

// WithDefault marks a parameter as having a default value.
func WithDefault() ParamOption {
	return func(v *Var) { v.hasDefault = true }
}

// NewVar returns a variable of the given kind.
func NewVar(pos token.Pos, name string, typ Type, kind VarKind) *Var {
	return &Var{object: object{pos: pos, name: name, typ: typ}, kind: kind}
}

// NewParam returns a parameter of the given kind.
func NewParam(pos token.Pos, name string, typ Type, kind ParamKind, opts ...ParamOption) *Var {
	v := &Var{object: object{pos: pos, name: name, typ: typ}, kind: ParamVar, paramKind: kind}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// IsAlias reports whether the name was declared as an alias.
func (n *TypeName) IsAlias() bool {
	return n.alias
}

// Kind returns the kind of v.
func (v *Var) Kind() VarKind {
	return v.kind
}

// ParamKind returns the parameter kind; it is meaningful for ParamVar only.
func (v *Var) ParamKind() ParamKind {
	return v.paramKind
}

// HasDefault reports whether the parameter has a default value.
func (v *Var) HasDefault() bool {
	return v.hasDefault
}

// Qualifiers returns the type qualifiers of v.
func (v *Var) Qualifiers() Qualifiers {
	return v.quals
}

// IsCell reports whether the variable lives in a closure cell.
func (v *Var) IsCell() bool {
	return v.cell
}

// Origin returns the variable v was copied from by substitution, or v.
func (v *Var) Origin() *Var {
	if v.origin == nil {
		return v
	}
	return v.origin
}

// Signature returns the signature of the function as declared, before its
// decorators apply.
func (f *Func) Signature() *Signature {
	return f.sig
}

// Decl returns the first definition of the function: its *ast.FunctionDef, or
// the *ast.Module for the body of a module. It is nil for a synthesized
// function without source.
func (f *Func) Decl() ast.Node {
	if len(f.defs) == 0 {
		return f.body
	}
	return f.defs[0]
}

// Scope returns the scope of the function body.
func (f *Func) Scope() *Scope {
	return f.scope
}

// FullName returns the qualified name, including the enclosing classes and
// functions.
func (f *Func) FullName() string {
	return f.fullName
}

// Imported returns the module bound by the name.
func (n *ModuleName) Imported() *Module {
	return n.imported
}

// Parent returns the scope that declares the object.
func (o *object) Parent() *Scope {
	return o.parent
}

// Pos returns the position of the declaration.
func (o *object) Pos() token.Pos {
	return o.pos
}

// Module returns the module that declares the object.
func (o *object) Module() *Module {
	return o.module
}

// Name returns the declared name.
func (o *object) Name() string {
	return o.name
}

// Type returns the type of the object.
func (o *object) Type() Type {
	return o.typ
}

// String implements Object.
func (n *TypeName) String() string {
	switch t := n.typ.(type) {
	case *TypeParam:
		return "typevar " + n.name
	case *Alias:
		return "type " + n.name + " = " + t.Rhs().String()
	case *Named:
		if t.class().kind == NewTypeClass {
			return "newtype " + n.name
		}
		return "class " + n.name
	}
	return "type " + n.name
}

// String implements Object.
func (v *Var) String() string {
	return "var " + v.name + ": " + typeName(v.typ)
}

// String implements Object.
func (f *Func) String() string {
	return "def " + f.name + typeName(f.sig)
}

// String implements Object.
func (n *ModuleName) String() string {
	return "module " + n.name
}

// String implements Object.
func (b *Builtin) String() string {
	return "builtin " + b.name
}

func (o *object) setParent(s *Scope) {
	o.parent = s
}

func newTypeName(pos token.Pos, module *Module, name string, typ Type) *TypeName {
	return &TypeName{object: object{pos: pos, module: module, name: name, typ: typ}, qualname: name}
}

func newModuleName(pos token.Pos, name string, imported *Module) *ModuleName {
	var typ Type = Typ[Any]
	if imported != nil {
		typ = imported
	}
	return &ModuleName{object: object{pos: pos, name: name, typ: typ}, imported: imported}
}

func newBuiltin(name string, typ Type, form formKind) *Builtin {
	return &Builtin{object: object{name: name, typ: typ}, form: form}
}

func (v *Var) with(typ Type) *Var {
	c := *v
	c.typ = typ
	c.origin = v.Origin()
	return &c
}

func typeName(t Type) string {
	if t == nil {
		return "<nil>"
	}
	return t.String()
}
