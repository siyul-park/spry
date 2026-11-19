package types

import "github.com/siyul-park/spry/ast"

// elemCell is the element type of the containers made at one allocation site. A
// list, set or dict of the program has a elemCell as each of its type arguments
// while the solver runs; containers that flow together share their Sites
// (union-find), and what is stored into one of them joins into the cell the
// elemCell holds. A elemCell is replaced by what it holds when the result is
// published, so no elemCell is visible in Info.
type elemCell struct {
	c       *solver
	parent  *elemCell
	cell    Type
	pinned  bool
	escaped bool
	readers map[*spec]struct{}
}

// cellKey names the i-th container an expression of a spec makes.
type cellKey struct {
	node ast.Node
	idx  int
}

// Underlying returns the type the site holds.
func (s *elemCell) Underlying() Type {
	return s.peek()
}

// String implements Type.
func (s *elemCell) String() string {
	return TypeString(s, nil)
}

// unifyCells merges the cells of two containers of the same class that are
// joined or passed for one another, looking through tuples.
func unifyCells(x, y Type) {
	switch x := x.(type) {
	case *elemCell:
		if y, ok := y.(*elemCell); ok {
			mergeCells(x, y)
		}
	case *Named:
		y, ok := y.(*Named)
		if !ok || x.origin != y.origin || x.targs.Len() != y.targs.Len() || !isContainerType(x) {
			return
		}
		for i := range x.targs.Len() {
			unifyCells(x.targs.At(i), y.targs.At(i))
		}
	case *Tuple:
		y, ok := y.(*Tuple)
		if !ok || x.Len() != y.Len() || x.unbounded != y.unbounded {
			return
		}
		for i := range x.Len() {
			unifyCells(x.vars[i].typ, y.vars[i].typ)
		}
	}
}

// finalType replaces every site by what it holds and every provenance Any by
// Typ[Any], for publishing.
func finalType(t Type) Type {
	return finalSeen(t, map[*elemCell]bool{})
}

func finalSeen(t Type, seen map[*elemCell]bool) Type {
	switch t := t.(type) {
	case *Basic:
		if t.kind == Any {
			return Typ[Any]
		}
	case *elemCell:
		r := t.find()
		if seen[r] {
			return Typ[Any]
		}
		seen[r] = true
		out := finalSeen(r.cell, seen)
		delete(seen, r)
		return out
	case *Named:
		args := make([]Type, t.targs.Len())
		changed := false
		for i := range args {
			args[i] = finalSeen(t.targs.At(i), seen)
			changed = changed || args[i] != t.targs.At(i)
		}
		if changed {
			return newInstance(t.origin, args)
		}
	case *Union:
		terms := make([]Type, len(t.terms))
		changed := false
		for i, e := range t.terms {
			terms[i] = finalSeen(e, seen)
			changed = changed || terms[i] != e
		}
		if changed {
			return NewUnion(terms...)
		}
	case *Tuple:
		return mapTuple(t, func(e Type) Type { return finalSeen(e, seen) })
	case *Signature:
		params := t.params.slice()
		vars := make([]*Var, len(params))
		changed := false
		for i, p := range params {
			nt := finalSeen(p.typ, seen)
			vars[i] = p
			if nt != p.typ {
				vars[i], changed = p.with(nt), true
			}
		}
		res := finalSeen(t.result, seen)
		if changed || res != t.result {
			return &Signature{tparams: t.tparams, params: NewTuple(vars), result: res}
		}
	}
	return t
}

// cellDepth is the nesting depth of the types a site holds, or a value past
// the cap for a type that contains itself.
func cellDepth(s *elemCell, seen map[*elemCell]bool) int {
	r := s.find()
	if seen[r] {
		return depthCap + 1
	}
	seen[r] = true
	d := depthOf(r.cell, seen)
	delete(seen, r)
	return d
}

// fresh gives each container of t that has no site yet a site of its own,
// holding its element types. The cells are the ones the expression node
// made before, so that the passes of the solver agree.
func (s *spec) fresh(node ast.Node, t Type) Type {
	n := 0
	var walk func(t Type) Type
	walk = func(t Type) Type {
		switch t := t.(type) {
		case *Named:
			if t.targs.Len() == 0 {
				return t
			}
			args := make([]Type, t.targs.Len())
			changed := false
			for i := range args {
				a := t.targs.At(i)
				if isContainerType(t) {
					if _, isSite := a.(*elemCell); !isSite {
						site := s.cellAt(node, n)
						n++
						site.store(walk(a))
						a = site
					}
				} else {
					a = walk(a)
				}
				args[i], changed = a, changed || a != t.targs.At(i)
			}
			if changed {
				return newInstance(t.origin, args)
			}
		case *Union:
			terms := make([]Type, len(t.terms))
			for i, e := range t.terms {
				terms[i] = walk(e)
			}
			return NewUnion(terms...)
		case *Tuple:
			return mapTuple(t, walk)
		}
		return t
	}
	return walk(t)
}

// newContainer is a list, set or dict whose elements join into the cells of
// node, holding elems.
func (s *spec) newContainer(node ast.Node, class string, elems ...Type) Type {
	args := make([]Type, len(elems))
	for i, e := range elems {
		site := s.cellAt(node, i)
		site.store(e)
		args[i] = site
	}
	return newInstance(builtin(class), args)
}

// cellAt returns the i-th site of a node in this spec.
func (s *spec) cellAt(node ast.Node, i int) *elemCell {
	k := cellKey{node, i}
	if site := s.cells[k]; site != nil {
		return site
	}
	site := s.c.newCell()
	if s.cells == nil {
		s.cells = map[cellKey]*elemCell{}
	}
	s.cells[k] = site
	return site
}

func (c *solver) newCell() *elemCell {
	return &elemCell{c: c, cell: Typ[Never], readers: map[*spec]struct{}{}}
}

// flowIn records that an argument of type arg is passed for a parameter of
// type param: what the parameter holds in a site of a container flows into
// the site. Invariant (mutable) containers share cells; read-only protocols
// only copy what they hold.
func (s *spec) flowIn(param, arg Type) {
	arg = Unalias(arg)
	switch p := Unalias(param).(type) {
	case *elemCell:
		p.store(norm(arg))
	case *Union:
		for _, term := range p.terms {
			if _, ok := term.(*elemCell); ok && !s.fitsOther(p, term, arg) {
				s.flowIn(term, arg)
			}
		}
	case *Named:
		s.flowNamed(p, arg)
	case *Tuple:
		if a, ok := arg.(*Tuple); ok && a.Len() == p.Len() && a.unbounded == p.unbounded {
			for i := range p.vars {
				s.flowIn(p.vars[i].typ, a.vars[i].typ)
			}
		}
	}
}

func (s *spec) flowNamed(p *Named, arg Type) {
	if p.targs.Len() == 0 {
		return
	}
	a := instanceClass(arg)
	if a == nil {
		return
	}
	base := asBase(a, p.origin)
	if base == nil || base.targs.Len() != p.targs.Len() {
		return
	}
	shared := isContainerType(p)
	for i := range p.targs.Len() {
		pa, aa := p.targs.At(i), base.targs.At(i)
		switch pt := pa.(type) {
		case *elemCell:
			if as, ok := aa.(*elemCell); ok && shared {
				mergeCells(pt, as)
			} else {
				pt.store(norm(aa))
			}
		default:
			s.flowIn(pa, aa)
		}
	}
}

// merge makes a and b one site.
func mergeCells(a, b *elemCell) {
	a, b = a.find(), b.find()
	if a == b {
		return
	}
	if b.pinned && !a.pinned {
		a, b = b, a
	}
	b.parent = a
	a.pinned = a.pinned || b.pinned
	a.escaped = a.escaped || b.escaped
	for r := range b.readers {
		a.readers[r] = struct{}{}
	}
	changed := false
	if !a.pinned {
		next := capType(Join(a.cell, b.cell))
		if !Identical(next, a.cell) {
			a.cell, changed = next, true
		}
	}
	if changed || !Identical(a.cell, b.cell) {
		a.c.notify(a.readers)
	}
}

func (s *spec) fitsOther(u *Union, skip, arg Type) bool {
	for _, term := range u.terms {
		if term != skip && AssignableTo(arg, term) {
			return true
		}
	}
	return false
}

// constrain pins the cells of a value to the container type its annotation
// declares, so that its elements have exactly that type.
func constrain(actual, declared Type) {
	actual, declared = Unalias(actual), Unalias(declared)
	a, ok1 := actual.(*Named)
	d, ok2 := declared.(*Named)
	if !ok1 || !ok2 || a.origin != d.origin || a.targs.Len() != d.targs.Len() || !isContainerType(a) {
		return
	}
	for i := range a.targs.Len() {
		site, ok := a.targs.At(i).(*elemCell)
		if !ok {
			continue
		}
		inner := site.peek()
		constrain(inner, d.targs.At(i))
		site.pin(d.targs.At(i))
	}
}

// peek returns what the site holds without recording a read.
func (s *elemCell) peek() Type {
	return s.find().cell
}

// pin fixes the cell to a declared type.
func (s *elemCell) pin(t Type) {
	r := s.find()
	if r.pinned {
		return
	}
	r.store(t)
	r.pinned = true
}

// store joins t into the cell and reports whether it changed.
func (s *elemCell) store(t Type) bool {
	r := s.find()
	t = Default(norm(t))
	if r.pinned {
		return false
	}
	next := capType(Join(r.cell, t))
	if Identical(next, r.cell) {
		return false
	}
	r.cell = next
	r.c.notify(r.readers)
	return true
}

// norm replaces the cells that stand for a value of their element type: all
// but the type arguments of a list, set or dict.
func norm(t Type) Type {
	return normSeen(t, map[*elemCell]bool{})
}

func normSeen(t Type, seen map[*elemCell]bool) Type {
	switch t := t.(type) {
	case *elemCell:
		r := t.find()
		if seen[r] {
			return Typ[Any]
		}
		seen[r] = true
		out := normSeen(t.value(), seen)
		delete(seen, r)
		return out
	case *Named:
		if t.targs.Len() == 0 {
			return t
		}
		keep := isContainerType(t)
		args := make([]Type, t.targs.Len())
		changed := false
		for i := range args {
			a := t.targs.At(i)
			if _, isSite := a.(*elemCell); !(keep && isSite) {
				a = normSeen(a, seen)
			}
			args[i], changed = a, changed || a != t.targs.At(i)
		}
		if changed {
			return newInstance(t.origin, args)
		}
	case *Union:
		terms := make([]Type, len(t.terms))
		changed := false
		for i, e := range t.terms {
			terms[i] = normSeen(e, seen)
			changed = changed || terms[i] != e
		}
		if changed {
			return NewUnion(terms...)
		}
	case *Tuple:
		return mapTuple(t, func(e Type) Type { return normSeen(e, seen) })
	}
	return t
}

func mapTuple(t *Tuple, f func(Type) Type) Type {
	vars := make([]*Var, t.Len())
	changed := false
	for i, v := range t.vars {
		nt := f(v.typ)
		if nt != v.typ {
			changed = true
			v = v.with(nt)
		}
		vars[i] = v
	}
	if !changed {
		return t
	}
	return NewTuple(vars, WithUnbounded(t.unbounded))
}

// cellAssignable decides assignability where one side is a site: a site as
// the source stands for what it holds, a site as the target accepts what is
// stored into it, and the store happens when the call is recorded.
func cellAssignable(v, t Type, a map[pair]bool) (bool, Type, bool) {
	if c, ok := t.(*elemCell); ok {
		if vc, ok := v.(*elemCell); ok && vc.find() == c.find() {
			return true, nil, true
		}
		if !c.find().pinned {
			return true, nil, true
		}
		k := pair{v, t}
		if a[k] {
			return true, nil, true
		}
		a[k] = true
		return assignable(v, c.value(), a), nil, true
	}
	if c, ok := v.(*elemCell); ok {
		k := pair{v, t}
		if a[k] {
			return true, nil, true
		}
		a[k] = true
		return assignable(c.value(), t, a), nil, true
	}
	return false, nil, false
}

// value returns what the site holds and records that the running spec
// depends on it.
func (s *elemCell) value() Type {
	r := s.find()
	if r.c != nil && r.c.cur != nil {
		r.readers[r.c.cur] = struct{}{}
	}
	return r.cell
}

func (s *elemCell) find() *elemCell {
	for s.parent != nil {
		if s.parent.parent != nil {
			s.parent = s.parent.parent
		}
		s = s.parent
	}
	return s
}

// keyString writes t for the key of a specialization: the element types of
// the containers are left out, because the containers that reach one version
// are joined into one.
func keyString(t Type) string {
	switch t := t.(type) {
	case *elemCell:
		return "~"
	case *Named:
		if t.targs.Len() == 0 || !isContainerType(t) {
			return TypeString(t, nil)
		}
		out := TypeString(t.origin, nil) + "["
		for i := range t.targs.Len() {
			if i > 0 {
				out += ", "
			}
			out += keyString(t.targs.At(i))
		}
		return out + "]"
	case *Union:
		out := ""
		for i, e := range t.terms {
			if i > 0 {
				out += " | "
			}
			out += keyString(e)
		}
		return out
	case *Tuple:
		out := "tuple["
		for i, e := range t.elems() {
			if i > 0 {
				out += ", "
			}
			out += keyString(e)
			if i == t.unbounded {
				out += ", ..."
			}
		}
		return out + "]"
	case *FuncValue:
		return "fn:" + t.fn.obj.fullName + envKey(t.env)
	}
	return TypeString(t, nil)
}

// isContainerType reports whether t is a list, set or dict of builtins.
func isContainerType(t Type) bool {
	return isBuiltin(t, "list") || isBuiltin(t, "set") || isBuiltin(t, "dict")
}
