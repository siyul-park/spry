package types

// Named is a class instance type: int, None, list[int], an application class
// or a NewType. A generic class has one origin Named and one instance per
// distinct list of type arguments; all share the origin's Class.
type Named struct {
	obj     *TypeName
	origin  *Named
	tparams *TypeParamList
	targs   *TypeList
	cls     *Class
}

// Obj returns the declaration of the class.
func (n *Named) Obj() *TypeName {
	return n.origin.obj
}

// Origin returns the generic class that n instantiates, or n.
func (n *Named) Origin() *Named {
	return n.origin
}

// TypeParams returns the type parameters of the class. The result is non-nil
// for an instantiated generic class too.
func (n *Named) TypeParams() *TypeParamList {
	return n.origin.tparams
}

// TypeArgs returns the type arguments of an instance, or nil.
func (n *Named) TypeArgs() *TypeList {
	return n.targs
}

// Underlying returns the Class of n, not substituted by the type arguments.
func (n *Named) Underlying() Type {
	return n.origin.cls
}

// String implements Type.
func (n *Named) String() string {
	return TypeString(n, nil)
}

func newInstance(origin *Named, targs []Type) *Named {
	return &Named{obj: origin.obj, origin: origin, tparams: origin.tparams, targs: newTypeList(targs), cls: origin.cls}
}

func (n *Named) class() *Class {
	return n.origin.cls
}

func (n *Named) substMap() substMap {
	if n.targs.Len() == 0 || n.origin.tparams.Len() != n.targs.Len() {
		return nil
	}
	m := make(substMap, n.targs.Len())
	for i, tp := range n.origin.tparams.slice() {
		m[tp] = n.targs.At(i)
	}
	return m
}

func underlyingOf(n *Named, fallback Type) Type {
	if n == nil {
		return fallback
	}
	return n.Underlying()
}

func builtin(name string) *Named {
	tn, _ := Universe.Lookup(name).(*TypeName)
	if tn == nil {
		return nil
	}
	n, _ := tn.Type().(*Named)
	return n
}

func isBuiltin(t Type, name string) bool {
	n, ok := t.(*Named)
	if !ok {
		return false
	}
	m := n.Obj().Module()
	return n.Obj().Name() == name && (m == nil || m.name == "builtins")
}
