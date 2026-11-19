package types

import "github.com/siyul-park/spry/ast"

// generatorOf is the type a generator function returns: Generator[Y, S, R],
// where Y joins the values it yields, S is the type of what it is sent, and R
// is the type of its return value. A yield used as a statement takes nothing
// that is sent.
func (s *spec) generatorOf(ret Type) Type {
	sent := s.sends
	if isNever(sent) {
		sent = builtin("NoneType")
	}
	res := abcInstance("Generator", s.yields, sent, ret)
	if s.declRet != nil && !AssignableTo(res, s.declRet) {
		s.fail(s.fn.def, "TYP140", "the generator is not assignable to the declared return type")
	}
	return res
}

// unusedYield marks a yield whose value nothing reads.
func (s *spec) unusedYield(y *ast.Yield) {
	if s.unused == nil {
		s.unused = map[*ast.Yield]bool{}
	}
	s.unused[y] = true
}

// yield infers a yield expression: its value is what the caller sends.
func (s *spec) yield(e *ast.Yield, v *env) Type {
	var item Type = builtin("NoneType")
	if e.Value != nil {
		item = s.expr(e.Value, v)
	}
	s.yields = joinCap(s.yields, Default(item))
	var sent Type = Typ[Any]
	if s.unused[e] {
		sent = builtin("NoneType")
	}
	s.sends = joinCap(s.sends, sent)
	return sent
}

// yieldFrom infers yield from: the items of the iterable are yielded, and the
// value of the expression is the return value of a generator, None for any
// other iterable.
func (s *spec) yieldFrom(e *ast.YieldFrom, v *env) Type {
	t := s.expr(e.Value, v)
	var res Type = builtin("NoneType")
	if n, ok := Unalias(Default(t)).(*Named); ok {
		if gen, ok := abcInstance("Generator", Typ[Any], Typ[Any], Typ[Any]).(*Named); ok {
			if base := asBase(n, gen.origin); base != nil && base.targs.Len() == 3 {
				s.yields = joinCap(s.yields, Default(norm(base.targs.At(0))))
				s.sends = joinCap(s.sends, Typ[Any])
				return Default(norm(base.targs.At(2)))
			}
		}
	}
	item := s.iterate(Site{e, SiteIter, 0}, Site{e, SiteNext, 0}, t)
	s.yields = joinCap(s.yields, Default(item))
	s.sends = joinCap(s.sends, Typ[Any])
	return res
}
