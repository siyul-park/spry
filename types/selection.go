package types

// Selection describes the result of LookupAttr: how an attribute was found
// and the type of the expression that reads it.
type Selection struct {
	kind SelectionKind
	recv Type
	obj  Object
	typ  Type
	// owner is the class whose member was found.
	owner *Named
}

// SelectionKind classifies a Selection.
type SelectionKind uint8

// Selection kinds.
const (
	// FieldVal is an instance variable or a module member.
	FieldVal SelectionKind = iota
	// ClassVal is a class attribute that binding leaves unchanged: a
	// class variable, a staticmethod or a nested class.
	ClassVal
	// MethodVal is a method bound to its receiver; Type has no receiver
	// parameter.
	MethodVal
	// MethodExpr is a method read from its class; Type keeps the receiver
	// parameter.
	MethodExpr
	// DescriptorVal is a property; Type is the result of its getter.
	DescriptorVal
	// GetattrVal is an attribute supplied by __getattr__.
	GetattrVal
)

// Kind returns the kind of the selection.
func (s *Selection) Kind() SelectionKind {
	return s.kind
}

// Recv returns the type of the receiver that was searched.
func (s *Selection) Recv() Type {
	return s.recv
}

// Obj returns the declaration that supplies the attribute, or nil for the
// __call__ of a callable.
func (s *Selection) Obj() Object {
	return s.obj
}

// Owner returns the class that declares the member, or nil for a module
// member.
func (s *Selection) Owner() *Named {
	return s.owner
}

// Type returns the type of the attribute as read, after binding.
func (s *Selection) Type() Type {
	return s.typ
}
