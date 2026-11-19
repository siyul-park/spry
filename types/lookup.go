package types

// LookupAttr finds attribute name of a value of type t: the members of the
// classes in the MRO, bound the way Python binds descriptors; for a type[T]
// receiver the members of T and then of its metaclass; then __getattr__. It
// returns nil if there is no such attribute or t is Any, Never or a Union.
func LookupAttr(t Type, name string) *Selection {
	switch u := Unalias(t).(type) {
	case *Meta:
		return lookupClass(u, name)
	case *TypeParam:
		return lookupBound(u, name)
	case *Module:
		obj := u.scope.Lookup(name)
		if obj == nil {
			return nil
		}
		return &Selection{kind: FieldVal, recv: u, obj: obj, typ: obj.Type()}
	case *Signature, *Overload:
		if name == "__call__" {
			return &Selection{kind: MethodVal, recv: u, typ: u}
		}
	}
	if n := instanceClass(Unalias(t)); n != nil {
		return lookupInstance(n, name, Unalias(t))
	}
	return nil
}

func lookupBound(p *TypeParam, name string) *Selection {
	switch {
	case len(p.constraints) > 0:
		return nil
	case p.bound != nil:
		return LookupAttr(p.bound, name)
	}
	if o := builtin("object"); o != nil {
		return lookupInstance(o, name, p)
	}
	return nil
}

func lookupClass(meta *Meta, name string) *Selection {
	var elem *Named
	switch e := Unalias(meta.elem).(type) {
	case *Named:
		elem = e
	case *TypeParam:
		elem, _ = e.bound.(*Named)
	}
	if elem == nil {
		return nil
	}
	for _, m := range resolvedMRO(elem) {
		if obj := m.class().members.Lookup(name); obj != nil {
			return bindClass(m, obj, elem, meta)
		}
	}
	mc := elem.class().meta
	if mc == nil {
		mc = builtin("type")
	}
	if mc == nil {
		return nil
	}
	return lookupInstance(mc, name, meta)
}

func lookupInstance(n *Named, name string, recv Type) *Selection {
	mro := resolvedMRO(n)
	for _, m := range mro {
		if obj := m.class().members.Lookup(name); obj != nil {
			return bindInstance(m, obj, recv)
		}
	}
	for _, m := range mro {
		if f, ok := m.class().members.Lookup("__getattr__").(*Func); ok {
			sel := bindInstance(m, f, recv)
			if sig, ok := sel.typ.(*Signature); ok {
				sel.typ = sig.result
			}
			sel.kind = GetattrVal
			return sel
		}
	}
	return nil
}

func bindClass(owner *Named, obj Object, elem *Named, meta *Meta) *Selection {
	sel := bindClassMember(owner, obj, elem, meta)
	sel.owner = owner
	return sel
}

func bindClassMember(owner *Named, obj Object, elem *Named, meta *Meta) *Selection {
	f, ok := obj.(*Func)
	if !ok {
		return bindInstance(owner, obj, elem)
	}
	sm := receiverMap(owner, elem)
	typ := subst(f.Type(), sm)
	switch f.deco {
	case classFunc:
		return &Selection{kind: MethodVal, recv: meta, obj: f, typ: dropFirst(typ)}
	case plainFunc:
		return &Selection{kind: MethodExpr, recv: meta, obj: f, typ: typ}
	}
	return &Selection{kind: ClassVal, recv: meta, obj: f, typ: typ}
}

func bindInstance(owner *Named, obj Object, recv Type) *Selection {
	sel := bindInstanceMember(owner, obj, recv)
	sel.owner = owner
	return sel
}

func bindInstanceMember(owner *Named, obj Object, recv Type) *Selection {
	sm := receiverMap(owner, recv)
	switch obj := obj.(type) {
	case *Func:
		typ := subst(obj.Type(), sm)
		switch obj.deco {
		case staticFunc:
			return &Selection{kind: ClassVal, recv: recv, obj: obj, typ: typ}
		case propertyFunc:
			return &Selection{kind: DescriptorVal, recv: recv, obj: obj, typ: getterResult(typ)}
		case plainFunc, classFunc:
			return &Selection{kind: MethodVal, recv: recv, obj: obj, typ: dropFirst(typ)}
		}
	case *Var:
		kind := FieldVal
		if obj.kind != FieldVar {
			kind = ClassVal
		}
		return &Selection{kind: kind, recv: recv, obj: obj, typ: subst(obj.typ, sm)}
	case *TypeName:
		return &Selection{kind: ClassVal, recv: recv, obj: obj, typ: NewMeta(obj.Type())}
	}
	return &Selection{kind: ClassVal, recv: recv, obj: obj, typ: subst(obj.Type(), sm)}
}

func receiverMap(owner *Named, recv Type) substMap {
	m := owner.substMap()
	if self := owner.class().self; self != nil {
		if m == nil {
			m = substMap{}
		}
		m[self] = recv
	}
	return m
}

func dropFirst(t Type) Type {
	switch t := t.(type) {
	case *Signature:
		params := t.params.slice()
		if len(params) == 0 || params[0].paramKind == VarPositional || params[0].paramKind > PositionalOrKeyword {
			return t
		}
		rest := NewTuple(params[1:])
		return &Signature{tparams: t.tparams, params: rest, result: t.result}
	case *Overload:
		sigs := make([]*Signature, len(t.sigs))
		for i, s := range t.sigs {
			sigs[i] = dropFirst(s).(*Signature)
		}
		return newOverload(sigs)
	}
	return t
}

func getterResult(t Type) Type {
	if s, ok := t.(*Signature); ok {
		return s.result
	}
	return t
}
