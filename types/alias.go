package types

// Alias is the type introduced by a type statement, type X[T] = ...; legacy
// aliases such as X = list[int] have no Alias and use the right-hand side.
type Alias struct {
	obj     *TypeName
	rhs     Type
	resolve func() Type
	origin  *Alias
	tparams *TypeParamList
	targs   *TypeList
}

const maxAliasDepth = 64

// Unalias returns t with all top-level aliases expanded. An alias that never
// reaches a type yields Invalid.
func Unalias(t Type) Type {
	for range maxAliasDepth {
		a, ok := t.(*Alias)
		if !ok {
			return t
		}
		t = a.Rhs()
	}
	return Typ[Invalid]
}

// Obj returns the declaration of the alias.
func (a *Alias) Obj() *TypeName {
	return a.origin.obj
}

// Rhs returns the aliased type, evaluating it on first use so that aliases
// can be recursive. For an instance it is the right-hand side substituted by
// the type arguments.
func (a *Alias) Rhs() Type {
	if a.rhs == nil {
		switch {
		case a.origin != a:
			a.rhs = subst(a.origin.Rhs(), a.origin.substMap(a.targs))
		case a.resolve != nil:
			resolve := a.resolve
			a.resolve = nil
			a.rhs = resolve()
		}
		if a.rhs == nil {
			a.rhs = Typ[Invalid]
		}
	}
	return a.rhs
}

// Origin returns the generic alias that a instantiates, or a.
func (a *Alias) Origin() *Alias {
	return a.origin
}

// TypeParams returns the type parameters of the alias.
func (a *Alias) TypeParams() *TypeParamList {
	return a.origin.tparams
}

// TypeArgs returns the type arguments of an instance, or nil.
func (a *Alias) TypeArgs() *TypeList {
	return a.targs
}

// Underlying returns the underlying type of the aliased type.
func (a *Alias) Underlying() Type {
	return Unalias(a).Underlying()
}

// String implements Type.
func (a *Alias) String() string {
	return TypeString(a, nil)
}

func newAlias(obj *TypeName, tparams []*TypeParam, resolve func() Type) *Alias {
	a := &Alias{obj: obj, resolve: resolve, tparams: newTypeParamList(tparams)}
	for i, tp := range tparams {
		tp.index = i
	}
	a.origin = a
	obj.typ = a
	obj.alias = true
	return a
}

func newAliasInstance(origin *Alias, targs []Type) *Alias {
	return &Alias{obj: origin.obj, origin: origin, tparams: origin.tparams, targs: newTypeList(targs)}
}

func (a *Alias) substMap(targs *TypeList) substMap {
	m := make(substMap, targs.Len())
	for i, tp := range a.tparams.slice() {
		if i < targs.Len() {
			m[tp] = targs.At(i)
		}
	}
	return m
}
