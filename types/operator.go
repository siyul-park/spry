package types

import (
	"math/big"

	"github.com/siyul-park/spry/ast"
)

var binaryNames = map[ast.Operator][2]string{
	ast.OpAdd: {"add", "radd"}, ast.OpSub: {"sub", "rsub"}, ast.OpMult: {"mul", "rmul"},
	ast.OpMatMult: {"matmul", "rmatmul"}, ast.OpDiv: {"truediv", "rtruediv"}, ast.OpMod: {"mod", "rmod"},
	ast.OpPow: {"pow", "rpow"}, ast.OpLShift: {"lshift", "rlshift"}, ast.OpRShift: {"rshift", "rrshift"},
	ast.OpBitOr: {"or", "ror"}, ast.OpBitXor: {"xor", "rxor"}, ast.OpBitAnd: {"and", "rand"},
	ast.OpFloorDiv: {"floordiv", "rfloordiv"},
}

var compareNames = map[ast.CmpOperator][2]string{
	ast.CmpEq: {"eq", "eq"}, ast.CmpNotEq: {"ne", "ne"}, ast.CmpLt: {"lt", "gt"},
	ast.CmpLtE: {"le", "ge"}, ast.CmpGt: {"gt", "lt"}, ast.CmpGtE: {"ge", "le"},
}

var unaryNames = map[ast.UnaryOperator]string{
	ast.UnaryInvert: "invert", ast.UnaryUAdd: "pos", ast.UnaryUSub: "neg",
}

func (s *spec) binary(e *ast.BinOp, v *env) Type {
	l := s.expr(e.Left, v)
	r := s.expr(e.Right, v)
	names := binaryNames[e.Op]
	return s.dispatch(e, l, r, names[0], names[1])
}

func (s *spec) unary(e *ast.UnaryOp, v *env) Type {
	if e.Op == ast.UnaryNot {
		t := s.expr(e.Operand, v)
		s.truth(e.Operand, t)
		return builtin("bool")
	}
	t := s.expr(e.Operand, v)
	switch {
	case isAny(t):
		return s.dynamic(e, "__"+unaryNames[e.Op]+"__", "an operand is Any", t)
	case isNever(t):
		return t
	}
	if lit, ok := t.(*Literal); ok && e.Op == ast.UnaryUSub {
		if n, ok := lit.value.(ast.Int); ok && n.N != nil {
			s.calls[e] = &Call{Kind: Static, Overload: -1, Slot: "__neg__"}
			return NewLiteral(ast.Int{N: new(big.Int).Neg(n.N)})
		}
	}
	t = Default(t)
	if res, call, ok := s.userMethod(e, t, "__"+unaryNames[e.Op]+"__", nil); ok {
		s.calls[e] = call
		return res
	}
	sel := LookupAttr(t, "__"+unaryNames[e.Op]+"__")
	if sel != nil {
		if res, idx, ok := matchType(sel.typ, nil); ok {
			s.calls[e] = &Call{Kind: Static, Func: funcOf(sel), Overload: idx, Slot: "__" + unaryNames[e.Op] + "__"}
			return res
		}
	}
	return s.dynamic(e, "__"+unaryNames[e.Op]+"__", "no such operation")
}

func (s *spec) compare(e *ast.Compare, v *env) Type {
	left := s.expr(e.Left, v)
	for i, op := range e.Ops {
		right := s.expr(e.Comparators[i], v)
		s.compareOne(Site{e, SiteCompare, i}, op, left, right)
		left = right
	}
	return builtin("bool")
}

func (s *spec) compareOne(key ast.Node, op ast.CmpOperator, l, r Type) {
	switch op {
	case ast.CmpIs, ast.CmpIsNot:
		return
	case ast.CmpIn, ast.CmpNotIn:
		s.contains(key, l, r)
		return
	}
	names := compareNames[op]
	s.dispatch(key, l, r, names[0], names[1])
}

func (s *spec) contains(key ast.Node, item, container Type) {
	switch {
	case isAny(item) || isAny(container):
		s.dynamic(key, "__contains__", "an operand is Any", item, container)
		return
	case isNever(item) || isNever(container):
		return
	}
	if _, call, ok := s.userMethod(key, container, "__contains__", []argv{{t: item}}); ok {
		s.calls[key] = call
		return
	}
	sel := LookupAttr(Default(container), "__contains__")
	if sel != nil {
		if _, idx, ok := matchType(sel.typ, []argv{{t: item}}); ok {
			s.calls[key] = &Call{Kind: Static, Func: funcOf(sel), Overload: idx, Slot: "__contains__"}
			return
		}
	}
	s.dynamic(key, "__contains__", "no such operation")
}

// dispatch resolves a binary operation through the dunder methods of its
// operands: the forward method of the left, then the reflected one of the
// right.
func (s *spec) dispatch(key ast.Node, l, r Type, fwd, rev string) Type {
	switch {
	case isAny(l) || isAny(r):
		return s.dynamic(key, "__"+fwd+"__", "an operand is Any", l, r)
	case isNever(l) || isNever(r):
		return Typ[Never]
	}
	lts, rts := unionTerms(Default(l)), unionTerms(Default(r))
	var res Type = Typ[Never]
	var call *Call
	for _, lt := range lts {
		for _, rt := range rts {
			t, c, ok := s.pair(key, lt, rt, r, fwd, rev)
			if !ok {
				switch {
				case s.Generalized:
					return s.dynamicReason(key, "__"+fwd+"__", reasonParamConflict, "the call sites disagree on a parameter", Typ[Any])
				case len(lts)*len(rts) > 1:
					return s.dynamicReason(key, "__"+fwd+"__", reasonUnion, "an operand is a union", Typ[Any])
				}
				return s.dynamic(key, "__"+fwd+"__", "no such operation")
			}
			res, call = joinCap(res, t), c
		}
	}
	if len(lts)*len(rts) > 1 {
		return s.dynamicReason(key, "__"+fwd+"__", reasonUnion, "an operand is a union", res)
	}
	s.calls[key] = call
	return res
}

func (s *spec) pair(key ast.Node, l, r Type, rawRight Type, fwd, rev string) (Type, *Call, bool) {
	arg := []argv{{t: r}}
	if _, lit := rawRight.(*Literal); lit && len(unionTerms(rawRight)) == 1 {
		arg = []argv{{t: rawRight}}
	}
	if t, c, ok := s.userMethodTry(key, l, "__"+fwd+"__", arg, true); ok && !notImplemented(t) {
		return t, c, true
	} else if ok {
		return nil, nil, false
	}
	if fwd == "ne" {
		if t, c, ok := s.userMethodTry(key, l, "__eq__", arg, true); ok && !notImplemented(t) {
			c.Negate = true
			return builtin("bool"), c, true
		}
	}
	if sel := LookupAttr(l, "__"+fwd+"__"); sel != nil {
		if res, idx, ok := matchType(sel.typ, arg); ok {
			return powResult(fwd, l, arg[0].t, res), &Call{Kind: Static, Func: funcOf(sel), Overload: idx, Slot: "__" + fwd + "__"}, true
		}
	}
	if t, c, ok := s.userMethodTry(key, r, "__"+rev+"__", []argv{{t: l}}, true); ok && !notImplemented(t) {
		return t, c, true
	}
	if sel := LookupAttr(r, "__"+rev+"__"); sel != nil {
		if res, idx, ok := matchType(sel.typ, []argv{{t: l}}); ok {
			return res, &Call{Kind: Static, Func: funcOf(sel), Overload: idx, Slot: "__" + rev + "__"}, true
		}
	}
	return nil, nil, false
}

// powResult refines int ** n for a literal n >= 0, which is an int.
func powResult(name string, l, r, res Type) Type {
	lit, ok := r.(*Literal)
	if name != "pow" || !ok || !isBuiltin(l, "int") && !isBuiltin(l, "bool") {
		return res
	}
	if n, ok := lit.value.(ast.Int); ok && n.N.Sign() >= 0 {
		return builtin("int")
	}
	return res
}

// truth records how e is tested for truth when it is not a bool.
func (s *spec) truth(e ast.Expr, t Type) {
	site := Site{e, SiteTruth, 0}
	t = Default(t)
	switch {
	case isAny(t):
		s.dynamic(site, "__bool__", "a truth test of Any", t)
		return
	case isBuiltin(t, "bool") || isNever(t):
		return
	case len(unionTerms(t)) > 1:
		s.dynamicReason(site, "__bool__", reasonUnion, "a truth test of a union", Typ[Any])
		return
	}
	for _, name := range []string{"__bool__", "__len__"} {
		if _, call, ok := s.userMethod(site, t, name, nil); ok {
			s.calls[site] = call
			return
		}
		if sel := LookupAttr(t, name); sel != nil && sel.kind == MethodVal {
			if _, idx, ok := matchType(sel.typ, nil); ok {
				s.calls[site] = &Call{Kind: Static, Func: funcOf(sel), Overload: idx, Slot: name}
				return
			}
		}
	}
}

func funcOf(sel *Selection) *Func {
	f, _ := sel.obj.(*Func)
	return f
}

// dynamic records an operation that is dispatched at run time on Any.
// An operand that is an explicit Any annotation opts out of the report, and
// so does the result.
func (s *spec) dynamic(key ast.Node, slot, msg string, ops ...Type) Type {
	if optedOut(ops) {
		s.calls[key] = &Call{Kind: Dynamic, Slot: slot, Overload: -1}
		return annotatedAny
	}
	return s.dynamicReason(key, slot, s.anyReason(key, ops...), msg, Typ[Any])
}

// optedOut reports whether the Any operands of an operation are all explicit
// Any annotations, with at least one.
func optedOut(ops []Type) bool {
	found := false
	for _, t := range ops {
		if !isAny(Unalias(t)) {
			continue
		}
		if Unalias(t) != Type(annotatedAny) {
			return false
		}
		found = true
	}
	return found
}

func (s *spec) dynamicReason(key ast.Node, slot, reason, msg string, res Type) Type {
	s.calls[key] = &Call{Kind: Dynamic, Slot: slot, Overload: -1}
	s.fall(key, reason, msg)
	if isNever(res) {
		return anyFor(reason)
	}
	return capType(res)
}

// anyReason says why an operand of the operation is Any: a parameter of a
// version that was widened, or a value the checker does not infer.
func (s *spec) anyReason(key ast.Node, ops ...Type) string {
	for _, t := range ops {
		if b, ok := Unalias(t).(*Basic); ok && b.kind == Any && b.why != "" {
			return b.why
		}
	}
	hasAny := false
	for _, t := range ops {
		hasAny = hasAny || isAny(Unalias(t))
	}
	if !hasAny {
		return reasonDynamicCall
	}
	switch {
	case s.escaped || s.c.escaped > 0:
		return reasonEscape
	case s.Generalized:
		return reasonParamConflict
	}
	return reasonAnyValue
}

// matchType picks the first overload of t that accepts args and returns its
// result and index, or -1 when t is a single signature.
func matchType(t Type, args []argv) (Type, int, bool) {
	switch t := t.(type) {
	case *Signature:
		res, ok := matchSig(t, args)
		return res, -1, ok
	case *Overload:
		for i, sig := range t.sigs {
			if res, ok := matchSig(sig, args); ok {
				return res, i, true
			}
		}
	}
	return nil, 0, false
}

// matchSig checks that args fit sig and returns the result type, inferring
// the type parameters of sig from them.
func matchSig(sig *Signature, args []argv) (Type, bool) {
	b, msg := matchArgs(sig, args)
	if msg != "" {
		return nil, false
	}
	m, err := inferArgs(sig, b)
	if err != nil {
		return nil, false
	}
	for p, t := range b.types {
		if !AssignableTo(t, subst(p.typ, m)) {
			return nil, false
		}
	}
	if g := groupParams(sig); g.varpos != nil {
		for _, t := range b.extra {
			if !AssignableTo(t, subst(g.varpos.typ, m)) {
				return nil, false
			}
		}
	}
	return subst(sig.result, m), true
}

// inferArgs infers the type parameters of sig from the arguments matched to
// its parameters.
func inferArgs(sig *Signature, b *matched) (substMap, error) {
	if sig.tparams.Len() == 0 {
		return nil, nil
	}
	var from, to []Type
	for p, t := range b.types {
		from, to = append(from, p.typ), append(to, Default(t))
	}
	return infer(sig.tparams.slice(), from, to)
}
