package types

// Join returns the least upper bound of x and y as a union with subsumption:
// a type that is assignable to the other is absorbed by it, Any absorbs
// everything and Never nothing. The observable numeric types do not absorb
// each other: bool, int and float keep their own term. Tuples of different
// lengths join into tuple[T, ...] of the joined elements.
func Join(x, y Type) Type {
	x, y = Unalias(x), Unalias(y)
	switch {
	case isNever(x):
		return y
	case isNever(y):
		return x
	}
	var terms []Type
	var anys []Type
	for _, t := range append(append([]Type{}, unionTerms(x)...), unionTerms(y)...) {
		if isAny(t) {
			anys = append(anys, t)
			continue
		}
		terms = insertTerm(terms, t)
	}
	if len(anys) > 0 {
		for _, a := range anys[1:] {
			if a != anys[0] {
				return Typ[Any]
			}
		}
		return anys[0]
	}
	return NewUnion(terms...)
}

func insertTerm(terms []Type, t Type) []Type {
	for _, e := range terms {
		unifyCells(e, t)
	}
	for _, e := range terms {
		if absorbs(e, t) {
			return terms
		}
	}
	for i, e := range terms {
		te, ok1 := e.(*Tuple)
		tt, ok2 := t.(*Tuple)
		if ok1 && ok2 && !absorbs(t, e) && (te.Len() != tt.Len() || te.unbounded != tt.unbounded) {
			terms[i] = joinTuples(te, tt)
			return terms
		}
	}
	out := make([]Type, 0, len(terms)+1)
	placed := false
	for _, e := range terms {
		if absorbs(t, e) {
			if !placed {
				out = append(out, t)
				placed = true
			}
			continue
		}
		out = append(out, e)
	}
	if !placed {
		out = append(out, t)
	}
	return out
}

func absorbs(wide, narrow Type) bool {
	if Identical(wide, narrow) {
		return true
	}
	return !keepsNumeric(wide, narrow) && AssignableTo(narrow, wide)
}

func keepsNumeric(wide, narrow Type) bool {
	w, ok := wide.(*Named)
	if !ok || !(isBuiltin(w, "int") || isBuiltin(w, "float") || isBuiltin(w, "complex")) {
		return false
	}
	n := instanceClass(narrow)
	return n != nil && n.origin != w.origin && (isBuiltin(n, "bool") || isBuiltin(n, "int") || isBuiltin(n, "float"))
}

func joinTuples(x, y *Tuple) Type {
	var elem Type = Typ[Never]
	for _, e := range append(x.elems(), y.elems()...) {
		elem = Join(elem, e)
	}
	return elemTuple([]Type{elem}, 0)
}
