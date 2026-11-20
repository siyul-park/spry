package types

type varianceTarget struct {
	class *Class
	param *TypeParam
}

type usage struct {
	co, contra bool
}

// inferVariances sets the variance of the PEP 695 type parameters of the
// classes declared in this run from how their members use them, starting
// from covariant and iterating, because the variance of one class depends on
// that of the classes its members mention.
func (d *declarer) inferVariances() {
	var params []*varianceTarget
	for _, obj := range d.pending {
		tn, ok := obj.(*TypeName)
		if !ok {
			continue
		}
		n, ok := tn.typ.(*Named)
		if !ok || d.decls[tn] == nil {
			continue
		}
		for _, tp := range n.tparams.slice() {
			if tp.kind == TypeVar && tp.inferred && d.decls[tp.obj] != nil {
				tp.variance = Covariant
				params = append(params, &varianceTarget{n.cls, tp})
			}
		}
	}
	for range 4 {
		changed := false
		for _, t := range params {
			if v := t.infer(); v != t.param.variance {
				t.param.variance = v
				changed = true
			}
		}
		if !changed {
			return
		}
	}
}

func (t *varianceTarget) infer() Variance {
	var u usage
	for _, b := range t.class.bases {
		t.visit(&u, b, 1)
	}
	for _, name := range t.class.members.Names() {
		switch obj := t.class.members.Lookup(name).(type) {
		case *Func:
			t.function(&u, obj)
		case *Var:
			t.visit(&u, obj.typ, 1)
			if obj.kind == FieldVar && obj.quals&(Final|ReadOnly) == 0 {
				t.visit(&u, obj.typ, -1)
			}
		}
	}
	switch {
	case u.co && u.contra:
		return Invariant
	case u.contra:
		return Contravariant
	}
	return Covariant
}

func (t *varianceTarget) function(u *usage, f *Func) {
	switch f.name {
	case "__init__", "__new__", "__init_subclass__":
		return
	}
	var sigs []*Signature
	switch typ := f.Type().(type) {
	case *Signature:
		sigs = []*Signature{typ}
	case *Overload:
		sigs = typ.sigs
	}
	for _, s := range sigs {
		params := s.params.slice()
		if f.deco != staticFunc && len(params) > 0 {
			params = params[1:]
		}
		if f.deco != propertyFunc {
			for _, p := range params {
				t.visit(u, p.typ, -1)
			}
		}
		t.visit(u, s.result, 1)
	}
}

func (t *varianceTarget) visit(u *usage, typ Type, pol int) {
	switch typ := typ.(type) {
	case *TypeParam:
		if typ == t.param {
			u.record(pol)
		}
	case *Named:
		for i, a := range typ.targs.slice() {
			t.visit(u, a, combine(pol, typ.origin.tparams.At(i).variance))
		}
	case *Union:
		for _, term := range typ.terms {
			t.visit(u, term, pol)
		}
	case *Tuple:
		for _, e := range typ.elems() {
			t.visit(u, e, pol)
		}
	case *Meta:
		t.visit(u, typ.elem, pol)
	case *Guard:
		t.visit(u, typ.typ, pol)
	case *Alias:
		t.visit(u, Unalias(typ), pol)
	case *Signature:
		for _, p := range typ.params.slice() {
			t.visit(u, p.typ, -pol)
		}
		t.visit(u, typ.result, pol)
	}
}

func combine(pol int, v Variance) int {
	switch v {
	case Covariant:
		return pol
	case Contravariant:
		return -pol
	}
	return 0
}

func (u *usage) record(pol int) {
	switch pol {
	case 1:
		u.co = true
	case -1:
		u.contra = true
	default:
		u.co, u.contra = true, true
	}
}
