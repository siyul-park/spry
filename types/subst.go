package types

type substMap map[*TypeParam]Type

func subst(t Type, m substMap) Type {
	if len(m) == 0 {
		return t
	}
	switch t := t.(type) {
	case *TypeParam:
		if r, ok := m[t]; ok {
			return r
		}
	case *Named:
		if args, ok := substTypes(t.targs.slice(), m); ok {
			return newInstance(t.origin, args)
		}
	case *Alias:
		if args, ok := substTypes(t.targs.slice(), m); ok {
			return newAliasInstance(t.origin, args)
		}
	case *Union:
		if terms, ok := substTypes(t.terms, m); ok {
			return NewUnion(terms...)
		}
	case *Meta:
		if e := subst(t.elem, m); e != t.elem {
			return NewMeta(e)
		}
	case *Guard:
		if e := subst(t.typ, m); e != t.typ {
			return newGuard(e, t.strict)
		}
	case *Tuple:
		return substTuple(t, m)
	case *Signature:
		return substSignature(t, m)
	case *Overload:
		return substOverload(t, m)
	case *ParamSpecArgs:
		if q, ok := m[t.param].(*TypeParam); ok {
			return newParamSpecArgs(q, t.kwargs)
		}
	}
	return t
}

func substTypes(ts []Type, m substMap) ([]Type, bool) {
	var out []Type
	for i, t := range ts {
		s := subst(t, m)
		if s != t && out == nil {
			out = append(make([]Type, 0, len(ts)), ts[:i]...)
		}
		if out != nil {
			out = append(out, s)
		}
	}
	return out, out != nil
}

func substTuple(t *Tuple, m substMap) Type {
	vars := make([]*Var, 0, t.Len())
	unbounded := -1
	changed := false
	for i, v := range t.slice() {
		nt := subst(v.typ, m)
		if i == t.unbounded {
			if tup, ok := nt.(*Tuple); ok && isVarTuple(v.typ) {
				if tup.unbounded >= 0 {
					unbounded = len(vars) + tup.unbounded
				}
				vars = append(vars, tup.vars...)
				changed = true
				continue
			}
			unbounded = len(vars)
		}
		if nt != v.typ {
			changed = true
			v = v.with(nt)
		}
		vars = append(vars, v)
	}
	if !changed {
		return t
	}
	return NewTuple(vars, WithUnbounded(unbounded))
}

func substSignature(s *Signature, m substMap) Type {
	params := s.params.slice()
	out := make([]*Var, 0, len(params))
	changed := false
	for i := 0; i < len(params); i++ {
		v := params[i]
		if spliced, skip := spliceParams(v, params[i+1:], m); spliced != nil || skip > 0 {
			out = append(out, spliced...)
			i += skip
			changed = true
			continue
		}
		if nt := subst(v.typ, m); nt != v.typ {
			changed = true
			v = v.with(nt)
		}
		out = append(out, v)
	}
	result := subst(s.result, m)
	if !changed && result == s.result {
		return s
	}
	return &Signature{tparams: s.tparams, params: NewTuple(out), result: result}
}

func substOverload(o *Overload, m substMap) Type {
	sigs := make([]*Signature, len(o.sigs))
	changed := false
	for i, s := range o.sigs {
		sigs[i] = substSignature(s, m).(*Signature)
		changed = changed || sigs[i] != s
	}
	if !changed {
		return o
	}
	return newOverload(sigs)
}

func spliceParams(v *Var, rest []*Var, m substMap) ([]*Var, int) {
	if v.paramKind != VarPositional {
		return nil, 0
	}
	switch a := v.typ.(type) {
	case *ParamSpecArgs:
		tup, ok := m[a.param].(*Tuple)
		if !ok || a.kwargs {
			return nil, 0
		}
		skip := 0
		if len(rest) > 0 && rest[0].paramKind == VarKeyword {
			skip = 1
		}
		return append([]*Var{}, tup.vars...), skip
	case *TypeParam:
		tup, ok := m[a].(*Tuple)
		if !ok || a.kind != TypeVarTuple {
			return nil, 0
		}
		out := make([]*Var, tup.Len())
		for i, e := range tup.vars {
			kind := PositionalOnly
			if i == tup.unbounded {
				kind = VarPositional
			}
			out[i] = NewParam(e.pos, e.name, e.typ, kind)
		}
		return out, 0
	}
	return nil, 0
}

func isVarTuple(t Type) bool {
	tp, ok := t.(*TypeParam)
	return ok && tp.kind == TypeVarTuple
}
