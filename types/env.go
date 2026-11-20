package types

// env is the abstract state at one point of a function body: whether the
// point is reachable, and the type and definedness of each variable. A
// variable missing from the map is unbound.
type env struct {
	live bool
	vars map[Object]slot
}

type slot struct {
	typ Type
	def Definedness
}

func newEnv() *env {
	return &env{live: true, vars: map[Object]slot{}}
}

func deadEnv() *env {
	return &env{vars: map[Object]slot{}}
}

func (f *env) bind(o Object, t Type) {
	f.vars[o] = slot{typ: t, def: Defined}
}

// joinEnvs merges the states of two paths that meet.
func joinEnvs(a, b *env) *env {
	switch {
	case !a.live:
		return b.clone()
	case !b.live:
		return a.clone()
	}
	out := &env{live: true, vars: make(map[Object]slot, len(a.vars))}
	for k := range a.vars {
		out.vars[k] = joinSlots(a.lookup(k), b.lookup(k))
	}
	for k := range b.vars {
		if _, ok := out.vars[k]; !ok {
			out.vars[k] = joinSlots(a.lookup(k), b.lookup(k))
		}
	}
	return out
}

func (f *env) clone() *env {
	out := &env{live: f.live, vars: make(map[Object]slot, len(f.vars))}
	for k, v := range f.vars {
		out.vars[k] = v
	}
	return out
}

func (f *env) lookup(o Object) slot {
	if sl, ok := f.vars[o]; ok {
		return sl
	}
	return slot{typ: Typ[Never], def: Undefined}
}

func joinSlots(x, y slot) slot {
	def := MaybeDefined
	if x.def == y.def {
		def = x.def
	}
	return slot{typ: joinCap(x.typ, y.typ), def: def}
}

func sameEnv(a, b *env) bool {
	if a.live != b.live || len(a.vars) != len(b.vars) {
		return false
	}
	for k, x := range a.vars {
		y, ok := b.vars[k]
		if !ok || x.def != y.def || !Identical(x.typ, y.typ) {
			return false
		}
	}
	return true
}

// widenEnv sets every variable of f that differs from prev to Any.
func widenEnv(f, prev *env) {
	for k, x := range f.vars {
		if y, ok := prev.vars[k]; !ok || !Identical(x.typ, y.typ) {
			f.vars[k] = slot{typ: Typ[Any], def: x.def}
		}
	}
}

// narrowIs is the type of x after x is None holds (want) or does not.
func narrowIs(t Type, want bool) Type {
	if isAny(t) {
		if want {
			return builtin("NoneType")
		}
		return t
	}
	return filterTerms(t, func(term Type) bool { return isNoneType(term) == want })
}

// narrowIsinstance is the type of x after isinstance(x, classes) holds
// (want) or does not.
func narrowIsinstance(t Type, classes []Type, want bool) Type {
	if isAny(t) {
		if want {
			return NewUnion(classes...)
		}
		return t
	}
	var out []Type
	for _, term := range unionTerms(t) {
		hit := false
		var subs []Type
		for _, c := range classes {
			if AssignableTo(term, c) {
				hit = true
			} else if AssignableTo(c, term) {
				subs = append(subs, c)
			}
		}
		switch {
		case want && hit:
			out = append(out, term)
		case want:
			out = append(out, subs...)
		case !hit:
			out = append(out, term)
		}
	}
	return NewUnion(out...)
}

// narrowTruthy removes None from t where x is known to be truthy.
func narrowTruthy(t Type) Type {
	if isAny(t) {
		return t
	}
	return filterTerms(t, func(term Type) bool { return !isNoneType(term) })
}

func isNoneType(t Type) bool {
	return isBuiltin(t, "NoneType")
}

// filterTerms keeps the terms of t that satisfy keep.
func filterTerms(t Type, keep func(Type) bool) Type {
	var out []Type
	for _, term := range unionTerms(t) {
		if keep(term) {
			out = append(out, term)
		}
	}
	return NewUnion(out...)
}
