package types

import "fmt"

func infer(tparams []*TypeParam, params, args []Type) (substMap, error) {
	set := make(substMap, len(tparams))
	for _, tp := range tparams {
		set[tp] = Typ[Never]
	}
	m := make(substMap, len(tparams))
	for i := range min(len(params), len(args)) {
		unify(params[i], args[i], set, m)
	}
	for _, tp := range tparams {
		arg, ok := m[tp]
		switch {
		case !ok && tp.def != nil:
			m[tp] = subst(tp.def, m)
		case !ok:
			m[tp] = Typ[Any]
		default:
			if err := checkArg(tp, arg, m); err != nil {
				return nil, fmt.Errorf("cannot infer %s: %w", tp.obj.name, err)
			}
		}
	}
	return m, nil
}

func unify(p, a Type, set, m substMap) {
	p, a = Unalias(p), Unalias(a)
	switch p := p.(type) {
	case *TypeParam:
		if _, ok := set[p]; !ok {
			return
		}
		if prev, ok := m[p]; ok {
			m[p] = Join(prev, a)
		} else {
			m[p] = a
		}
	case *Named:
		unifyNamed(p, a, set, m)
	case *Union:
		unifyUnion(p, a, set, m)
	case *Tuple:
		unifyTuple(p, a, set, m)
	case *Signature:
		unifySignature(p, a, set, m)
	case *Meta:
		if am, ok := a.(*Meta); ok {
			unify(p.elem, am.elem, set, m)
		}
	}
}

func unifyNamed(p *Named, a Type, set, m substMap) {
	if p.targs.Len() == 0 {
		return
	}
	an := instanceClass(a)
	if an == nil {
		return
	}
	base := asBase(an, p.origin)
	if base == nil || base.targs.Len() != p.targs.Len() {
		return
	}
	for i := range p.targs.Len() {
		unify(p.targs.At(i), base.targs.At(i), set, m)
	}
}

func unifyUnion(p *Union, a Type, set, m substMap) {
	var generic, concrete []Type
	for _, t := range p.terms {
		if _, isCell := t.(*elemCell); isCell || subst(t, set) != t {
			generic = append(generic, t)
		} else {
			concrete = append(concrete, t)
		}
	}
	for _, atom := range unionTerms(a) {
		if anyAssignable(atom, concrete) {
			continue
		}
		for _, g := range generic {
			unify(g, atom, set, m)
		}
	}
}

func unifyTuple(p *Tuple, a Type, set, m substMap) {
	at, ok := a.(*Tuple)
	if !ok {
		return
	}
	switch {
	case p.Len() == 1 && p.unbounded == 0 && !isVarTuple(p.vars[0].typ):
		for _, e := range at.elems() {
			unify(p.vars[0].typ, e, set, m)
		}
	case p.unbounded >= 0 && isVarTuple(p.vars[p.unbounded].typ):
		unifyVarTuple(p, at, set, m)
	case p.Len() == at.Len() && p.unbounded == at.unbounded:
		for i, v := range p.vars {
			unify(v.typ, at.vars[i].typ, set, m)
		}
	}
}

func unifyVarTuple(p, a *Tuple, set, m substMap) {
	k := p.unbounded
	suffix := p.Len() - k - 1
	if a.unbounded >= 0 || a.Len() < k+suffix {
		return
	}
	for i := range k {
		unify(p.vars[i].typ, a.vars[i].typ, set, m)
	}
	for i := range suffix {
		unify(p.vars[k+1+i].typ, a.vars[a.Len()-suffix+i].typ, set, m)
	}
	ts := p.vars[k].typ.(*TypeParam)
	if _, ok := set[ts]; ok {
		m[ts] = elemTuple(a.elems()[k:a.Len()-suffix], -1)
	}
}

func unifySignature(p *Signature, a Type, set, m substMap) {
	as, ok := a.(*Signature)
	if !ok {
		return
	}
	pp, ap := groupParams(p).pos, groupParams(as).pos
	for i := range min(len(pp), len(ap)) {
		unify(pp[i].typ, ap[i].typ, set, m)
	}
	unify(p.result, as.result, set, m)
}
