package types

import (
	"fmt"

	"github.com/siyul-park/spry/token"
)

// Tuple is an ordered list of variables. As a type it is the Python tuple
// type of its elements; as the parameter list of a Signature it lists the
// parameters. A nil *Tuple is empty.
type Tuple struct {
	vars      []*Var
	unbounded int
}

// TupleOption configures NewTuple.
type TupleOption func(*Tuple)

// WithUnbounded marks the variable at index i as repeated zero or more times,
// as in tuple[T, ...], *tuple[T, ...] and *Ts.
func WithUnbounded(i int) TupleOption {
	return func(t *Tuple) { t.unbounded = i }
}

// NewTuple returns a tuple of vars with a fixed length. It panics if an
// option marks an index outside vars.
func NewTuple(vars []*Var, opts ...TupleOption) *Tuple {
	t := &Tuple{vars: vars, unbounded: -1}
	for _, opt := range opts {
		opt(t)
	}
	if t.unbounded < -1 || t.unbounded >= len(vars) {
		panic(fmt.Sprintf("types: unbounded index %d out of range for %d variables", t.unbounded, len(vars)))
	}
	return t
}

// Len returns the number of variables.
func (t *Tuple) Len() int {
	if t == nil {
		return 0
	}
	return len(t.vars)
}

// At returns the i-th variable.
func (t *Tuple) At(i int) *Var {
	return t.vars[i]
}

// Unbounded returns the index of the repeated variable, or -1 for a fixed
// length.
func (t *Tuple) Unbounded() int {
	if t == nil {
		return -1
	}
	return t.unbounded
}

// Underlying returns t.
func (t *Tuple) Underlying() Type {
	return t
}

// String implements Type.
func (t *Tuple) String() string {
	return TypeString(t, nil)
}

func elemTuple(types []Type, unbounded int) *Tuple {
	vars := make([]*Var, len(types))
	for i, ty := range types {
		vars[i] = NewVar(token.NoPos, "", ty, FieldVar)
	}
	return NewTuple(vars, WithUnbounded(unbounded))
}

func (t *Tuple) elems() []Type {
	out := make([]Type, t.Len())
	for i, v := range t.slice() {
		out[i] = v.Type()
	}
	return out
}

func (t *Tuple) slice() []*Var {
	if t == nil {
		return nil
	}
	return t.vars
}
