package types

import (
	"fmt"

	"github.com/siyul-park/spry/ast"
)

// Literal is the type of one literal value: an int, str, bytes, bool or None
// constant, or an enum member.
type Literal struct {
	value  ast.Value
	member *Var
}

// Guard is the return type TypeGuard[T] or, when strict, TypeIs[T].
type Guard struct {
	typ    Type
	strict bool
}

// NewLiteral returns the type of the constant v. It panics unless v is an
// ast.Int, ast.Str, ast.Bytes, ast.Bool or ast.None.
func NewLiteral(v ast.Value) *Literal {
	switch v.(type) {
	case ast.Int, ast.Str, ast.Bytes, ast.Bool, ast.None:
		return &Literal{value: v}
	}
	panic(fmt.Sprintf("types: %T is not a literal value", v))
}

// Value returns the constant, or nil for an enum member.
func (l *Literal) Value() ast.Value {
	return l.value
}

// Member returns the enum member, or nil for a constant.
func (l *Literal) Member() *Var {
	return l.member
}

// Underlying returns the class of the value.
func (l *Literal) Underlying() Type {
	return underlyingOf(literalClass(l), l)
}

// Type returns T.
func (g *Guard) Type() Type {
	return g.typ
}

// Strict reports whether g is TypeIs rather than TypeGuard.
func (g *Guard) Strict() bool {
	return g.strict
}

// Underlying returns the bool class.
func (g *Guard) Underlying() Type {
	return underlyingOf(builtin("bool"), g)
}

// String implements Type.
func (l *Literal) String() string {
	return TypeString(l, nil)
}

// String implements Type.
func (g *Guard) String() string {
	return TypeString(g, nil)
}

func newGuard(t Type, strict bool) *Guard {
	return &Guard{typ: t, strict: strict}
}

func newMemberLiteral(member *Var) *Literal {
	return &Literal{member: member}
}

func literalClass(l *Literal) *Named {
	if l.member != nil {
		n, _ := l.member.Type().(*Named)
		return n
	}
	switch l.value.(type) {
	case ast.Int:
		return builtin("int")
	case ast.Str:
		return builtin("str")
	case ast.Bytes:
		return builtin("bytes")
	case ast.Bool:
		return builtin("bool")
	}
	return builtin("NoneType")
}
