package types

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Class is the structure of a class declaration, the Underlying of its Named.
// It is not substituted by type arguments, as the runtime class object of
// list[int] is the one of list.
type Class struct {
	obj       *TypeName
	tparams   *TypeParamList
	bases     []Type
	mro       []*Named
	meta      *Named
	members   *Scope
	kind      ClassKind
	final     bool
	checkable bool
	self      *TypeParam
	fields    []*Var
}

// ClassKind distinguishes the kinds of class declaration.
type ClassKind uint8

// Class kinds.
const (
	OrdinaryClass ClassKind = iota
	ProtocolClass
	TypedDictClass
	NewTypeClass
)

var (
	errCyclic = errors.New("a class cannot inherit from itself")
	errMRO    = errors.New("Cannot create a consistent method resolution order (MRO)")
)

// Obj returns the declaration of the class.
func (c *Class) Obj() *TypeName {
	return c.obj
}

// TypeParams returns the type parameters of the class.
func (c *Class) TypeParams() *TypeParamList {
	return c.tparams
}

// Bases returns the declared base classes; a class without any has object.
func (c *Class) Bases() []Type {
	return append([]Type(nil), c.bases...)
}

// MRO returns the C3 method resolution order, starting with the class
// itself. Entries are expressed in the type parameters of the class.
func (c *Class) MRO() []*Named {
	return append([]*Named(nil), c.mro...)
}

// Metaclass returns the metaclass, or nil for type.
func (c *Class) Metaclass() *Named {
	return c.meta
}

// Members returns the class scope.
func (c *Class) Members() *Scope {
	return c.members
}

// Kind returns the kind of class declaration.
func (c *Class) Kind() ClassKind {
	return c.kind
}

// Final reports whether the class is decorated with @final.
func (c *Class) Final() bool {
	return c.final
}

// RuntimeCheckable reports whether the class is decorated with
// @runtime_checkable.
func (c *Class) RuntimeCheckable() bool {
	return c.checkable
}

// Underlying returns c.
func (c *Class) Underlying() Type {
	return c
}

// String implements Type.
func (c *Class) String() string {
	return "class " + TypeString(c.obj.Type(), nil)
}

func newClass(obj *TypeName, tparams []*TypeParam, kind ClassKind, members *Scope) *Named {
	c := &Class{obj: obj, tparams: newTypeParamList(tparams), kind: kind, members: members}
	for i, tp := range tparams {
		tp.index = i
	}
	n := &Named{obj: obj, tparams: c.tparams, cls: c}
	n.origin = n
	obj.typ = n
	c.mro = []*Named{n}
	c.self = newTypeParam(obj.module, "Self", TypeVar)
	c.self.bound = n
	c.self.isSelf = true
	c.self.inferred = true
	return n
}

func (c *Class) finish(bases []Type, meta *Named) error {
	self := c.obj.typ.(*Named)
	named := namedBases(bases)
	if len(named) == 0 {
		if o := builtin("object"); o != nil && o != self {
			named = []*Named{o}
			bases = []Type{o}
		}
	}
	mro, err := linearize(self, named)
	if err != nil {
		return err
	}
	if meta == nil {
		for _, b := range named {
			if meta = b.class().meta; meta != nil {
				break
			}
		}
	}
	c.bases, c.meta, c.mro = bases, meta, mro
	return nil
}

func namedBases(bases []Type) []*Named {
	var out []*Named
	for _, b := range bases {
		switch b := Unalias(b).(type) {
		case *Named:
			out = append(out, b)
		case *Tuple:
			if t := builtin("tuple"); t != nil {
				out = append(out, t)
			}
		}
	}
	return out
}

func linearize(self *Named, bases []*Named) ([]*Named, error) {
	seqs := make([][]*Named, 0, len(bases)+1)
	for _, b := range bases {
		mro := resolvedMRO(b)
		if b.origin == self.origin || slices.ContainsFunc(mro, func(n *Named) bool { return n.origin == self.origin }) {
			return nil, fmt.Errorf("%w: %s", errCyclic, self.Obj().Name())
		}
		seqs = append(seqs, mro)
	}
	seqs = append(seqs, bases)
	out := []*Named{self}
	for {
		seqs = slicesNonEmpty(seqs)
		if len(seqs) == 0 {
			return out, nil
		}
		var next *Named
		for _, s := range seqs {
			if !inTail(s[0], seqs) {
				next = s[0]
				break
			}
		}
		if next == nil {
			names := make([]string, len(bases))
			for i, b := range bases {
				names[i] = b.Obj().Name()
			}
			return nil, fmt.Errorf("%w for bases %s", errMRO, strings.Join(names, ", "))
		}
		out = append(out, next)
		for i, s := range seqs {
			if s[0].origin == next.origin {
				seqs[i] = s[1:]
			}
		}
	}
}

func slicesNonEmpty(seqs [][]*Named) [][]*Named {
	out := seqs[:0]
	for _, s := range seqs {
		if len(s) > 0 {
			out = append(out, s)
		}
	}
	return out
}

func inTail(n *Named, seqs [][]*Named) bool {
	for _, s := range seqs {
		for _, e := range s[1:] {
			if e.origin == n.origin {
				return true
			}
		}
	}
	return false
}

func asBase(n, origin *Named) *Named {
	for _, e := range resolvedMRO(n) {
		if e.origin == origin {
			return e
		}
	}
	return nil
}

func resolvedMRO(n *Named) []*Named {
	c := n.class()
	m := n.substMap()
	out := make([]*Named, len(c.mro))
	for i, e := range c.mro {
		switch {
		case i == 0:
			out[i] = n
		case m == nil:
			out[i] = e
		default:
			out[i] = subst(e, m).(*Named)
		}
	}
	return out
}

func instanceClass(t Type) *Named {
	switch t := t.(type) {
	case *Named:
		return t
	case *Literal:
		return literalClass(t)
	case *Basic:
		if t.kind == LiteralString {
			return builtin("str")
		}
	case *Guard:
		return builtin("bool")
	case *Tuple:
		return tupleClass(t)
	case *Signature, *Overload, *FuncValue:
		return builtin("function")
	}
	return nil
}

func tupleClass(t *Tuple) *Named {
	c := builtin("tuple")
	if c == nil || c.tparams.Len() != 1 {
		return c
	}
	var elem Type = Typ[Never]
	for _, e := range t.elems() {
		elem = Join(elem, e)
	}
	return newInstance(c, []Type{elem})
}
