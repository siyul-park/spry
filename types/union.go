package types

import "slices"

// Union is a union of two or more distinct types in source order.
type Union struct {
	terms []Type
}

// NewUnion returns the union of terms: nested unions are flattened, terms
// identical to an earlier one are dropped and source order is kept. No terms
// give Never and one term gives that term.
func NewUnion(terms ...Type) Type {
	var out []Type
	for _, t := range flattenUnion(terms) {
		if !slices.ContainsFunc(out, func(u Type) bool { return Identical(t, u) }) {
			out = append(out, t)
		}
	}
	switch len(out) {
	case 0:
		return Typ[Never]
	case 1:
		return out[0]
	}
	return &Union{terms: out}
}

// Len returns the number of terms.
func (u *Union) Len() int {
	return len(u.terms)
}

// Term returns the i-th term.
func (u *Union) Term(i int) Type {
	return u.terms[i]
}

// Underlying returns u.
func (u *Union) Underlying() Type {
	return u
}

// String implements Type.
func (u *Union) String() string {
	return TypeString(u, nil)
}

func flattenUnion(terms []Type) []Type {
	var out []Type
	for _, t := range terms {
		if u, ok := t.(*Union); ok {
			out = append(out, u.terms...)
			continue
		}
		out = append(out, t)
	}
	return out
}

func unionTerms(t Type) []Type {
	if u, ok := t.(*Union); ok {
		return u.terms
	}
	return []Type{t}
}
