package types

// Type is a Python type: the type of a value or the meaning of an annotation.
// Underlying returns the type that decides attribute lookup and runtime
// representation: the *Class of a Named, the base class of a Literal, Guard or
// LiteralString, the bound of a TypeParam, and the type itself otherwise.
type Type interface {
	Underlying() Type
	String() string
}
