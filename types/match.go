package types

import (
	"github.com/siyul-park/spry/ast"
)

// matchStmt infers match. The subject is evaluated once and each case is
// tried in order in the state the earlier ones left: a pattern binds names
// where it matches, and a case that is not irrefutable lets the next one see
// the state in which this one did not match, with the names it bound before
// it failed.
func (s *spec) matchStmt(st *ast.Match, v *env) {
	t := s.expr(st.Subject, v)
	cur := v.clone()
	out := deadEnv()
	for _, mc := range st.Cases {
		if !cur.live {
			s.markDead(mc.Body)
			continue
		}
		mv := cur.clone()
		irrefutable := s.pattern(mc.Pattern, t, mv)
		s.narrowSubject(st.Subject, mc.Pattern, t, mv)
		bound := mv.clone()
		if mc.Guard != nil {
			_, tv, fv := s.test(mc.Guard, mv)
			mv = tv.clone()
			bound = joinEnvs(bound, fv)
			irrefutable = false
		}
		s.block(mc.Body, mv)
		out = joinEnvs(out, mv)
		if irrefutable {
			cur = deadEnv()
		} else {
			cur = joinEnvs(cur, bound)
		}
	}
	*v = *joinEnvs(out, cur)
}

// narrowSubject narrows the variable that is the subject of the match to the
// classes a class pattern, or the None singleton, matches.
func (s *spec) narrowSubject(subject ast.Expr, p ast.Pattern, t Type, v *env) {
	if s.narrowable(subject) == nil {
		return
	}
	narrowed := s.matched(p, t)
	if Identical(narrowed, t) {
		return
	}
	*v = *s.narrowName(subject, v, func(Type) Type { return narrowed })
}

// pattern infers a pattern against a value of type t, binding the names it
// captures in v. It reports whether the pattern matches every value.
func (s *spec) pattern(p ast.Pattern, t Type, v *env) bool {
	if s.pats == nil {
		s.pats = map[ast.Pattern]Type{}
	}
	s.pats[p] = t
	switch p := p.(type) {
	case *ast.MatchValue:
		vt := s.expr(p.Value, v)
		s.compareOne(Site{p.Value, SiteCompare, 0}, ast.CmpEq, t, vt)
	case *ast.MatchAs:
		return s.asPattern(p, t, v)
	case *ast.MatchOr:
		return s.orPattern(p, t, v)
	case *ast.MatchSequence:
		s.sequencePattern(p, t, v)
	case *ast.MatchMapping:
		s.mappingPattern(p, t, v)
	case *ast.MatchClass:
		s.classPattern(p, t, v)
	}
	return false
}

func (s *spec) asPattern(p *ast.MatchAs, t Type, v *env) bool {
	irrefutable := true
	inner := t
	if p.Pattern != nil {
		irrefutable = s.pattern(p.Pattern, t, v)
		inner = s.matched(p.Pattern, t)
	}
	if p.Name != nil {
		s.capture(p, inner, v)
	}
	return irrefutable
}

func (s *spec) orPattern(p *ast.MatchOr, t Type, v *env) bool {
	out := deadEnv()
	irrefutable := false
	for _, alt := range p.Patterns {
		av := v.clone()
		irrefutable = s.pattern(alt, t, av) || irrefutable
		out = joinEnvs(out, av)
	}
	*v = *out
	return irrefutable
}

// sequencePattern infers [a, b, *rest]: it matches a list or tuple, not a str
// or bytes, of the shape the patterns have.
func (s *spec) sequencePattern(p *ast.MatchSequence, t Type, v *env) {
	star := -1
	for i, sub := range p.Patterns {
		if _, ok := sub.(*ast.MatchStar); ok {
			star = i
		}
	}
	tup, isTuple := Unalias(Default(t)).(*Tuple)
	var item Type = Typ[Any]
	switch {
	case isTuple && tup.unbounded < 0:
	case isTuple:
		item = Default(tup.vars[0].typ)
	default:
		var ok bool
		if item, ok = s.sequenceItem(t); !ok {
			s.fail(p, "TYP150", "a sequence pattern needs a list or tuple")
		}
	}
	for i, sub := range p.Patterns {
		sub_t := item
		if isTuple && tup.unbounded < 0 {
			sub_t = tupleItem(tup, i, star, len(p.Patterns))
		}
		if ms, ok := sub.(*ast.MatchStar); ok {
			rest := s.newContainer(ms, "list", s.restItem(tup, isTuple, item, star, len(p.Patterns)))
			s.pats[sub] = rest
			if ms.Name != nil {
				s.capture(ms, rest, v)
			}
			continue
		}
		s.pattern(sub, sub_t, v)
	}
}

// mappingPattern infers {"k": p, **rest}: it matches a dict whose keys are
// the ones given.
func (s *spec) mappingPattern(p *ast.MatchMapping, t Type, v *env) {
	var k, val Type = Typ[Any], Typ[Any]
	if n, ok := Unalias(Default(t)).(*Named); ok && n.targs.Len() == 2 {
		k, val = Default(norm(n.targs.At(0))), Default(norm(n.targs.At(1)))
	} else {
		s.fail(p, "TYP150", "a mapping pattern needs a dict")
	}
	for _, key := range p.Keys {
		s.expr(key, v)
	}
	for _, sub := range p.Patterns {
		s.pattern(sub, val, v)
	}
	if p.Rest != nil {
		s.capture(p, s.newContainer(p, "dict", k, val), v)
	}
}

// classPattern infers C(p, name=q): it matches an instance of C whose
// attributes match. The positional patterns take the attributes that
// __match_args__ names, and, for the classes that match the subject itself,
// the subject.
func (s *spec) classPattern(p *ast.MatchClass, t Type, v *env) {
	ct := s.expr(p.Cls, v)
	classes, ok := instancesOf(ct)
	if !ok || len(classes) != 1 {
		s.fail(p, "TYP150", "a class pattern needs a class")
		return
	}
	inst := narrowIsinstance(t, classes, true)
	if isNever(inst) {
		inst = classes[0]
	}
	names := s.matchArgs(classes[0], len(p.Patterns))
	if len(p.Patterns) > 0 && names == nil {
		if len(p.Patterns) != 1 || !matchesSelf(classes[0]) {
			s.fail(p, "TYP150", "positional patterns need __match_args__")
			return
		}
		s.pattern(p.Patterns[0], inst, v)
	}
	attrs := s.c.info.matchedAttrs(p, names, p.KwdAttrs)
	for i, a := range attrs {
		s.record(a.Value, inst)
		var sub ast.Pattern
		if i < len(names) {
			sub = p.Patterns[i]
		} else {
			sub = p.KwdPatterns[i-len(names)]
		}
		s.pattern(sub, s.record(a, s.attribute(a, inst)), v)
	}
	if len(names) > 0 || len(p.KwdAttrs) > 0 {
		s.calls[p] = &Call{Kind: Static, Overload: -1, Slot: "match", Names: names}
	}
}

// matched is the type of a value after the pattern matched it.
func (s *spec) matched(p ast.Pattern, t Type) Type {
	switch p := p.(type) {
	case *ast.MatchClass:
		if classes, ok := instancesOf(s.types[p.Cls].Type); ok {
			return narrowIsinstance(t, classes, true)
		}
	case *ast.MatchSingleton:
		if _, isNone := p.Value.(ast.None); isNone {
			return narrowIs(t, true)
		}
	case *ast.MatchAs:
		if p.Pattern != nil {
			return s.matched(p.Pattern, t)
		}
	case *ast.MatchOr:
		var out Type = Typ[Never]
		for _, alt := range p.Patterns {
			out = joinCap(out, s.matched(alt, t))
		}
		return out
	}
	return t
}

// restItem is the item type of what a starred pattern captures.
func (s *spec) restItem(tup *Tuple, isTuple bool, item Type, star, n int) Type {
	if !isTuple || tup.unbounded >= 0 {
		return item
	}
	var out Type = Typ[Never]
	for _, e := range tup.vars[star:max(star, tup.Len()-(n-star-1))] {
		out = joinCap(out, Default(e.typ))
	}
	return out
}

// sequenceItem is the type of the items of a list or sequence type.
func (s *spec) sequenceItem(t Type) (Type, bool) {
	n, ok := Unalias(Default(t)).(*Named)
	if !ok || n.targs.Len() != 1 || isBuiltin(n, "str") || isBuiltin(n, "bytes") {
		return Typ[Any], false
	}
	return Default(norm(n.targs.At(0))), true
}

// capture binds the variable that the pattern node declares.
func (s *spec) capture(p ast.Node, t Type, v *env) {
	if obj := s.c.info.Defs[p]; obj != nil {
		s.bindTo(obj, p, Default(t), v)
	}
}

// matchArgs returns the names of the first n positional attributes of a
// class, or nil when the class declares none.
func (s *spec) matchArgs(t Type, n int) []string {
	if n == 0 {
		return nil
	}
	named, ok := Unalias(t).(*Named)
	if !ok {
		return nil
	}
	for _, m := range resolvedMRO(named) {
		ci := s.c.classes[m.origin]
		if ci == nil || ci.def == nil {
			continue
		}
		if names, found := matchArgsOf(ci.def); found {
			if n > len(names) {
				return nil
			}
			return names[:n]
		}
	}
	return nil
}

// tupleItem is the type of the item of a fixed tuple that the i-th of n
// patterns, one of which may be starred, is matched against.
func tupleItem(tup *Tuple, i, star, n int) Type {
	switch {
	case star < 0 || i < star:
		if i < tup.Len() {
			return tup.vars[i].typ
		}
	case i > star:
		if at := tup.Len() - (n - i); at >= 0 && at < tup.Len() {
			return tup.vars[at].typ
		}
	}
	return Typ[Any]
}

// matchArgsOf reads __match_args__ from the body of a class.
func matchArgsOf(cd *ast.ClassDef) ([]string, bool) {
	for _, st := range cd.Body {
		a, ok := st.(*ast.Assign)
		if !ok || len(a.Targets) != 1 || !isName(a.Targets[0], "__match_args__") {
			continue
		}
		names, ok := stringElems(a.Value)
		return names, ok
	}
	return nil, false
}

// matchesSelf reports whether a class that is a pattern with one positional
// pattern matches the subject itself, as int and str do.
func matchesSelf(t Type) bool {
	n, ok := Unalias(t).(*Named)
	if !ok {
		return false
	}
	switch n.Obj().Name() {
	case "bool", "bytearray", "bytes", "dict", "float", "frozenset", "int", "list", "set", "str", "tuple":
		return isBuiltin(n, n.Obj().Name())
	}
	return false
}

// matchedAttrs returns the attribute reads that a class pattern makes, the
// positional ones first, creating them on first use. Their receiver is one
// placeholder name.
func (info *Info) matchedAttrs(p *ast.MatchClass, positional, keywords []string) []*ast.Attribute {
	if attrs, ok := info.Matched[p]; ok {
		return attrs
	}
	recv := &ast.Name{Span: ast.Span{StartPos: p.Pos(), EndPos: p.End()}, ID: "$subject", Ctx: ast.CtxLoad}
	var attrs []*ast.Attribute
	for _, name := range append(append([]string{}, positional...), keywords...) {
		attrs = append(attrs, &ast.Attribute{Span: ast.Span{StartPos: p.Pos(), EndPos: p.End()}, Value: recv, Attr: name, Ctx: ast.CtxLoad})
	}
	info.Matched[p] = attrs
	return attrs
}
