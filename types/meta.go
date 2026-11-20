package types

// Meta is the type type[T] of a class object whose instances have type T.
type Meta struct {
	elem Type
}

// NewMeta returns type[t].
func NewMeta(t Type) *Meta {
	return &Meta{elem: t}
}

// Elem returns the instance type T of type[T].
func (m *Meta) Elem() Type {
	return m.elem
}

// Underlying returns m.
func (m *Meta) Underlying() Type {
	return m
}

// String implements Type.
func (m *Meta) String() string {
	return TypeString(m, nil)
}
