package types

import "fmt"

// Overload is the type of an @overload function: its signatures in
// declaration order.
type Overload struct {
	sigs []*Signature
}

// Signature is the type of a callable: its parameters, which carry the
// parameter kinds, and its single result. A generic signature has type
// parameters of its own.
type Signature struct {
	tparams *TypeParamList
	params  *Tuple
	result  Type
}

type paramGroups struct {
	pos    []*Var
	varpos *Var
	kwonly []*Var
	varkw  *Var
}

// NewSignature returns a signature of params, whose variables must be
// parameters, and result. It panics if the parameter kinds are not in the
// order of a Python parameter list, with at most one variadic parameter of each
// kind.
func NewSignature(params *Tuple, result Type) *Signature {
	prev := ParamKind(0)
	for i, p := range params.slice() {
		switch {
		case p.kind != ParamVar:
			panic(fmt.Sprintf("types: signature variable %q is not a parameter", p.name))
		case p.paramKind < prev:
			panic(fmt.Sprintf("types: parameter %q is out of order", p.name))
		case i > 0 && p.paramKind == prev && (prev == VarPositional || prev == VarKeyword):
			panic(fmt.Sprintf("types: duplicate variadic parameter %q", p.name))
		}
		prev = p.paramKind
	}
	return &Signature{params: params, result: result}
}

// Len returns the number of signatures.
func (o *Overload) Len() int {
	return len(o.sigs)
}

// At returns the i-th signature.
func (o *Overload) At(i int) *Signature {
	return o.sigs[i]
}

// Underlying returns o.
func (o *Overload) Underlying() Type {
	return o
}

// TypeParams returns the type parameters of a generic signature.
func (s *Signature) TypeParams() *TypeParamList {
	return s.tparams
}

// Params returns the parameters.
func (s *Signature) Params() *Tuple {
	return s.params
}

// Result returns the result type.
func (s *Signature) Result() Type {
	return s.result
}

// Underlying returns s.
func (s *Signature) Underlying() Type {
	return s
}

// String implements Type.
func (o *Overload) String() string {
	return TypeString(o, nil)
}

// String implements Type.
func (s *Signature) String() string {
	return TypeString(s, nil)
}

func newSignature(tparams []*TypeParam, params *Tuple, result Type) *Signature {
	s := NewSignature(params, result)
	s.tparams = newTypeParamList(tparams)
	for i, tp := range tparams {
		tp.index = i
	}
	return s
}

func newOverload(sigs []*Signature) *Overload {
	return &Overload{sigs: sigs}
}

func isGradual(s *Signature) bool {
	g := groupParams(s)
	return len(g.pos) == 0 && len(g.kwonly) == 0 && g.varpos != nil && g.varkw != nil &&
		isAny(g.varpos.typ) && isAny(g.varkw.typ)
}

func groupParams(s *Signature) paramGroups {
	var g paramGroups
	for _, p := range s.params.slice() {
		switch p.paramKind {
		case PositionalOnly, PositionalOrKeyword:
			g.pos = append(g.pos, p)
		case VarPositional:
			g.varpos = p
		case KeywordOnly:
			g.kwonly = append(g.kwonly, p)
		case VarKeyword:
			g.varkw = p
		}
	}
	return g
}

func paramSpecOf(g paramGroups) *TypeParam {
	if g.varpos == nil || g.varkw == nil || len(g.kwonly) > 0 {
		return nil
	}
	a, ok := g.varpos.typ.(*ParamSpecArgs)
	k, ok2 := g.varkw.typ.(*ParamSpecArgs)
	if !ok || !ok2 || a.param != k.param || a.kwargs || !k.kwargs {
		return nil
	}
	return a.param
}

func isAny(t Type) bool {
	b, ok := t.(*Basic)
	return ok && b.kind == Any
}
