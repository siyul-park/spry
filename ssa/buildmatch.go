package ssa

import (
	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/types"
)

// matchStmt lowers match as a sequence of tests. The subject is evaluated
// once; each case tests its pattern, binds the names it captures, tries its
// guard and runs its body, and any failure goes on to the next case.
func (f *fbuild) matchStmt(st *ast.Match) {
	subject := f.expr(st.Subject)
	done := f.newBlock()
	for _, mc := range st.Cases {
		if !f.alive() {
			break
		}
		fail := f.newBlock()
		f.pattern(mc.Pattern, subject, fail)
		if mc.Guard != nil && f.alive() {
			pass := f.newBlock()
			f.cond(mc.Guard, pass, fail)
			f.seal(pass)
			f.start(pass)
		}
		f.stmts(mc.Body)
		f.jump(done)
		f.merge(fail)
	}
	f.jump(done)
	f.merge(done)
}

// pattern tests the pattern p against v and binds its captures, or goes to
// fail.
func (f *fbuild) pattern(p ast.Pattern, v Value, fail *Block) {
	if !f.alive() {
		return
	}
	switch p := p.(type) {
	case *ast.MatchValue:
		rhs := f.expr(p.Value)
		site := types.Site{Node: p.Value, Kind: types.SiteCompare}
		f.require(f.cmp(site, ast.CmpEq, v, f.patternType(p), rhs, f.typeOf(p.Value)), fail)
	case *ast.MatchSingleton:
		f.require(f.singleton(p, v), fail)
	case *ast.MatchAs:
		if p.Pattern != nil {
			f.pattern(p.Pattern, v, fail)
		}
		if p.Name != nil {
			f.bindObject(f.b.info.Defs[p], v)
		}
	case *ast.MatchOr:
		f.orPattern(p, v, fail)
	case *ast.MatchSequence:
		f.sequencePattern(p, v, fail)
	case *ast.MatchMapping:
		f.mappingPattern(p, v, fail)
	case *ast.MatchClass:
		f.classPattern(p, v, fail)
	}
}

// orPattern tries the alternatives in order; each binds the same names.
func (f *fbuild) orPattern(p *ast.MatchOr, v Value, fail *Block) {
	join := f.newBlock()
	for i, alt := range p.Patterns {
		last := i == len(p.Patterns)-1
		next := fail
		if !last {
			next = f.newBlock()
		}
		f.pattern(alt, v, next)
		f.jump(join)
		if last {
			break
		}
		f.merge(next)
	}
	f.merge(join)
}

// sequencePattern tests a list or tuple: its length, then each item.
func (f *fbuild) sequencePattern(p *ast.MatchSequence, v Value, fail *Block) {
	t := TypeOf(v)
	if t.Nullable {
		f.require(f.val(OpIsNot, tyI1, v, noneConst()), fail)
		v = f.val(OpNonNull, nonNullable(t), f.register(v))
		v.(*Instr).Attr = "sequence"
		t = TypeOf(v)
	}
	star := -1
	for i, sub := range p.Patterns {
		if _, ok := sub.(*ast.MatchStar); ok {
			star = i
		}
	}
	n := len(p.Patterns)
	fixed := t.Kind == KindTuple && !t.Variadic
	var size Value
	switch {
	case fixed && (star < 0 && len(t.Elems) != n || star >= 0 && len(t.Elems) < n-1):
		f.jump(fail)
		return
	case fixed:
		size = intConst(int64(len(t.Elems)))
	case t.Kind == KindList || t.Kind == KindTuple:
		size = f.val(OpLen, tyInt, v)
		if star < 0 {
			f.require(f.val(OpEq, tyI1, size, intConst(int64(n))), fail)
		} else {
			f.require(f.val(OpGe, tyI1, size, intConst(int64(n-1))), fail)
		}
	default:
		f.jump(fail)
		return
	}
	for i, sub := range p.Patterns {
		item := f.sequenceItem(t, v, size, i, star, n)
		if ms, ok := sub.(*ast.MatchStar); ok {
			if ms.Name != nil {
				f.bindObject(f.b.info.Defs[ms], f.starItems(t, v, size, star, n, f.b.lower(f.patternType(ms))))
			}
			continue
		}
		f.pattern(sub, item, fail)
	}
}

// mappingPattern tests that a dict has the keys of the pattern and matches
// the values; **rest takes a copy without those keys.
func (f *fbuild) mappingPattern(p *ast.MatchMapping, v Value, fail *Block) {
	t := TypeOf(v)
	if t.Nullable {
		f.require(f.val(OpIsNot, tyI1, v, noneConst()), fail)
		v = f.val(OpNonNull, nonNullable(t), f.register(v))
		v.(*Instr).Attr = "mapping"
		t = TypeOf(v)
	}
	if t.Kind != KindDict {
		f.jump(fail)
		return
	}
	keys := make([]Value, len(p.Keys))
	for i, k := range p.Keys {
		keys[i] = f.conv(f.expr(k), t.Elems[0])
		f.require(f.val(OpIn, tyI1, keys[i], v), fail)
		f.pattern(p.Patterns[i], f.val(OpIndex, t.Elems[1], v, keys[i]), fail)
	}
	if p.Rest != nil {
		rest := f.callFn(f.b.helper("dict.copy", []Type{t}, t), []Value{v})
		for _, k := range keys {
			f.emit(OpDelIndex, Type{}, rest, k)
		}
		f.bindObject(f.b.info.Defs[p], rest)
	}
}

// classPattern tests that v is an instance of the class and matches the
// attributes the patterns read.
func (f *fbuild) classPattern(p *ast.MatchClass, v Value, fail *Block) {
	meta, ok := f.typeOf(p.Cls).(*types.Meta)
	if !ok {
		f.jump(fail)
		return
	}
	n, ok := types.Unalias(meta.Elem()).(*types.Named)
	if !ok {
		f.jump(fail)
		return
	}
	cls := f.b.classFor(n)
	inst := f.b.lower(meta.Elem())
	if t := TypeOf(v); t.Kind == KindClass && !t.Nullable && !t.Class.derives(cls) && !cls.derives(t.Class) {
		f.jump(fail)
		return
	}
	if t := TypeOf(v); !(t.Kind == KindClass && !t.Nullable && t.Class.derives(cls)) && !t.same(inst) {
		in := f.emit(OpIsInstance, tyI1, f.conv(v, tyAny))
		if in == nil {
			return
		}
		in.Class = cls
		f.require(in, fail)
		v = f.conv(v, inst)
	}
	c := f.spec.Calls[p]
	attrs := f.b.info.Matched[p]
	if c == nil && len(p.Patterns) == 1 && len(attrs) == 0 {
		f.pattern(p.Patterns[0], v, fail)
		return
	}
	for i, a := range attrs {
		f.known[a.Value] = v
		var sub ast.Pattern
		if c != nil && i < len(c.Names) {
			sub = p.Patterns[i]
		} else {
			sub = p.KwdPatterns[i-len(c.Names)]
		}
		f.pattern(sub, f.expr(a), fail)
	}
}

// singleton tests v is None, True or False.
func (f *fbuild) singleton(p *ast.MatchSingleton, v Value) Value {
	var c Value
	switch x := p.Value.(type) {
	case ast.None:
		t := TypeOf(v)
		if !t.Nullable && t.Kind != KindAny && t.Kind != KindNone {
			return boolConst(false)
		}
		return f.val(OpIs, tyI1, v, noneConst())
	case ast.Bool:
		c = boolConst(bool(x))
	default:
		return boolConst(false)
	}
	return f.val(OpIs, tyI1, f.conv(v, tyAny), f.conv(c, tyAny))
}

func (f *fbuild) patternType(p ast.Pattern) types.Type {
	if t, ok := f.spec.Patterns[p]; ok {
		return t
	}
	return types.Typ[types.Any]
}

// sequenceItem reads the item that the i-th of n patterns matches: from the
// start, or from the end after the star.
func (f *fbuild) sequenceItem(t Type, v, size Value, i, star, n int) Value {
	if ms := star; ms >= 0 && i == ms {
		return v
	}
	var idx Value = intConst(int64(i))
	if star >= 0 && i > star {
		if c, ok := size.(*Const); ok {
			idx = intConst(int64(constInt(c)) - int64(n-i))
		} else {
			idx = f.val(OpSub, tyInt, size, intConst(int64(n-i)))
		}
	}
	elem := t.at(0)
	if t.Kind == KindTuple && !t.Variadic {
		k, _ := constIndex(idx, len(t.Elems))
		return f.val(OpIndex, t.Elems[k], v, idx)
	}
	return f.val(OpIndex, elem, v, idx)
}

// starItems is the list that a starred pattern captures.
func (f *fbuild) starItems(t Type, v, size Value, star, n int, want Type) Value {
	tail := n - star - 1
	if t.Kind == KindTuple && !t.Variadic {
		var vals []Value
		for i := star; i < len(t.Elems)-tail; i++ {
			vals = append(vals, f.conv(f.val(OpIndex, t.Elems[i], v, intConst(int64(i))), want.at(0)))
		}
		return f.val(OpNewList, want, vals...)
	}
	end := f.val(OpSub, tyInt, size, intConst(int64(tail)))
	part := f.val(OpSlice, t, v, intConst(int64(star)), end, noneConst())
	if t.Kind == KindList {
		return f.conv(part, want)
	}
	list := f.val(OpNewList, want)
	f.forEach(nil, nil, part, want.at(0), func(cur Value) {
		f.emit(OpAppend, Type{}, list, f.conv(cur, want.at(0)))
	})
	return list
}

// require continues where cond holds and goes to fail where it does not.
func (f *fbuild) require(cond Value, fail *Block) {
	if c, ok := cond.(*Const); ok {
		if v, isBool := c.Value.(ast.Bool); isBool && bool(v) {
			return
		} else if isBool {
			f.jump(fail)
			return
		}
	}
	next := f.newBlock()
	f.branch(cond, next, fail)
	f.seal(next)
	f.start(next)
}

func constInt(c *Const) int {
	if n, ok := c.Value.(ast.Int); ok && n.N != nil {
		return int(n.N.Int64())
	}
	return 0
}

func nonNullable(t Type) Type {
	t.Nullable = false
	return t
}
