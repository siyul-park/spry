package types

import (
	"slices"
	"sort"
	"strings"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/token"
)

// solver infers the types of a whole program. Each function is analyzed once
// per specialization, a spec: an abstract interpretation of its body over the
// argument types. Specs read each other's results and the types of module
// globals; a change in a result re-queues its readers until nothing changes.
// Every result only grows (the join of what was seen before), the lattice is
// of finite height under the caps, so the loop ends.
type solver struct {
	p         *program
	info      *Info
	fset      *token.FileSet
	fns       map[*Func]*fn
	specs     map[*Spec]*spec
	srcs      map[*Module]*source
	globals   map[*Var]*global
	inits     []*spec
	queue     []*spec
	total     int
	lib       map[*Func]*Func
	cur       *spec
	lambdas   map[*ast.Lambda]*fn
	escaped   int
	classes   map[*Named]*classInfo
	classList []*classInfo
	setters   map[*Func]*Func
	prelude   *source
	decos     map[*Func]*global
}

// fn is a function with a body of the program, or the body of a module.
type fn struct {
	obj      *Func
	def      *ast.FunctionDef
	src      *source
	scope    *Scope
	sig      *Signature
	ret      *TypeParam
	byKey    map[string]*spec
	list     []*spec
	gens     map[*spec]*spec
	lambda   bool
	defaults map[*Var]Type
	opaque   bool
	gen      bool
	perSite  bool
}

// global is a module variable: the join of every value stored in it.
type global struct {
	typ      Type
	declared Type
	readers  map[*spec]struct{}
}

// Caps of the inference. They are policy of the solver, not of the type
// system: a join beyond a cap widens to Any.
const (
	specCap  = 4
	unionCap = 4
	depthCap = 4
	totalCap = 4096
	passCap  = 64
	// preludeCap bounds the specializations of a prelude function, each of
	// which serves one call.
	preludeCap = 64
)

func newSolver(p *program) *solver {
	c := &solver{
		p: p, info: p.info, fset: p.fset,
		fns: map[*Func]*fn{}, specs: map[*Spec]*spec{}, srcs: map[*Module]*source{},
		globals: map[*Var]*global{}, lambdas: map[*ast.Lambda]*fn{},
	}
	for _, s := range p.order {
		c.srcs[s.mod] = s
	}
	return c
}

// run analyzes the program and publishes the facts in Info.
func (c *solver) run() {
	c.rejectSubset()
	c.layout()
	for _, src := range c.p.order {
		f := &Func{object: object{module: src.mod, name: "<module>"}, fullName: "<module>", scope: src.mod.scope, body: src.file}
		src.mod.init = f
		fn := &fn{obj: f, src: src, scope: src.mod.scope, byKey: map[string]*spec{}}
		c.fns[f] = fn
		s := c.newSpec(fn, nil, "")
		c.inits = append(c.inits, s)
		c.enqueue(s)
	}
	c.drain()
	c.finish()
}

func (c *solver) drain() {
	for len(c.queue) > 0 {
		s := c.queue[0]
		c.queue = c.queue[1:]
		s.queued = false
		c.analyze(s)
	}
}

// ensure analyzes s now unless it is done or on the stack, so that a caller
// sees the result of a callee that is not recursive.
func (c *solver) ensure(s *spec) {
	if !s.done && !s.running {
		c.analyze(s)
	}
}

// analyze runs the passes of s until none of its inputs changed during a pass.
func (c *solver) analyze(s *spec) {
	if s.running {
		s.rerun = true
		return
	}
	s.running = true
	prev := c.cur
	c.cur = s
	defer func() { c.cur = prev }()
	for {
		s.rerun = false
		s.passes++
		s.pass()
		if !s.rerun {
			break
		}
		if s.passes >= passCap {
			s.widen()
			break
		}
	}
	s.running = false
	s.done = true
}

// load reads a module variable on behalf of s.
func (c *solver) load(s *spec, v *Var) Type {
	g := c.global(v)
	g.readers[s] = struct{}{}
	if g.declared != nil {
		return g.declared
	}
	if v.module != nil && c.srcs[v.module] == nil {
		return v.typ
	}
	return g.typ
}

// store joins t into the type of a module variable.
func (c *solver) store(v *Var, t Type) {
	g := c.global(v)
	if g.declared != nil {
		return
	}
	next := capType(Join(g.typ, Default(t)))
	if !Identical(next, g.typ) {
		g.typ = next
		c.notify(g.readers)
	}
}

// notify re-queues the readers of a changed value.
func (c *solver) notify(readers map[*spec]struct{}) {
	for r := range readers {
		if r.running {
			r.rerun = true
		} else if r.done {
			c.enqueue(r)
		}
	}
}

func (c *solver) enqueue(s *spec) {
	if s.queued {
		return
	}
	s.queued = true
	c.queue = append(c.queue, s)
}

// global returns the cell of a module variable.
func (c *solver) global(v *Var) *global {
	g := c.globals[v]
	if g == nil {
		g = &global{typ: Typ[Never], readers: map[*spec]struct{}{}}
		if v.annot || v.pos == token.NoPos {
			g.declared = v.typ
		}
		c.globals[v] = g
	}
	return g
}

// finish drops the specs no root reaches and publishes the survivors.
func (c *solver) finish() {
	live := map[*spec]bool{}
	var mark func(s *spec)
	mark = func(s *spec) {
		if live[s] {
			return
		}
		live[s] = true
		for _, call := range s.calls {
			if call.Target != nil {
				mark(c.specs[call.Target])
			}
			for _, ov := range call.Overrides {
				mark(c.specs[ov.Target])
			}
		}
		for _, e := range s.escapes {
			mark(e)
		}
		if s.env != nil {
			mark(s.env)
		}
	}
	for _, s := range c.inits {
		mark(s)
	}
	var fns []*fn
	for _, fn := range c.fns {
		fns = append(fns, fn)
	}
	sort.Slice(fns, func(i, j int) bool { return fns[i].obj.pos < fns[j].obj.pos })
	seen := map[string]int{}
	var fallbacks []Fallback
	for _, fn := range fns {
		var list []*spec
		for _, s := range fn.list {
			if live[s] {
				list = append(list, s)
			}
		}
		slices.SortFunc(list, func(a, b *spec) int { return strings.Compare(a.key, b.key) })
		for _, s := range list {
			c.publish(s)
			for _, fb := range s.falls {
				// one fallback per site: a specific reason replaces any-value
				k := fb.Pos.String() + fb.End.String()
				i, ok := seen[k]
				switch {
				case !ok:
					seen[k] = len(fallbacks)
					fallbacks = append(fallbacks, fb)
				case fallbacks[i].Reason == reasonAnyValue && fb.Reason != reasonAnyValue:
					fallbacks[i] = fb
				}
			}
			for _, e := range s.errs {
				c.p.errs.Add(e.Pos, e.End, e.Code, e.Msg)
			}
			for n, d := range s.names {
				if d > c.info.Defined[n] {
					c.info.Defined[n] = d
				}
			}
			for e, tv := range s.types {
				if prev, ok := c.info.Types[e]; ok {
					tv.Type = Join(prev.Type, tv.Type)
				}
				c.info.Types[e] = tv
			}
			c.info.Specs[fn.obj] = append(c.info.Specs[fn.obj], s.Spec)
		}
	}
	slices.SortFunc(fallbacks, func(a, b Fallback) int {
		if a.Pos.Offset != b.Pos.Offset {
			return a.Pos.Offset - b.Pos.Offset
		}
		return strings.Compare(a.Reason, b.Reason)
	})
	c.info.Fallbacks = fallbacks
	c.definedness(live)
	for v, g := range c.globals {
		if g.declared == nil && c.srcs[v.module] != nil {
			v.typ = finalType(g.typ)
		}
	}
	c.p.errs = dedupe(c.p.errs)
	c.p.errs.Sort()
	c.scrub()
}

func dedupe(list ErrorList) ErrorList {
	var out ErrorList
	seen := map[string]bool{}
	for _, e := range list {
		k := e.Pos.String() + e.Code + e.Msg
		if !seen[k] {
			seen[k] = true
			out = append(out, e)
		}
	}
	return out
}

// publish completes the exported part of s.
func (c *solver) publish(s *spec) {
	for e, tv := range s.types {
		tv.Type = finalType(tv.Type)
		s.types[e] = tv
	}
	for v, t := range s.vars {
		s.vars[v] = finalType(t)
	}
	args := make([]Type, len(s.args))
	for i, a := range s.args {
		args[i] = finalType(a)
		if a == nil {
			args[i] = finalType(s.result)
		}
	}
	s.TypeArgs = newTypeList(args)
	if s.env != nil {
		s.Outer = s.env.Spec
	}
	s.Cells, s.Guarded = map[Object]Type{}, s.guarded
	for obj, loc := range s.cellLocs {
		s.Cells[obj] = finalType(loc.typ)
	}
	for _, name := range s.fn.freeNames() {
		if obj, _ := s.freeObject(name); obj != nil {
			s.Captures = append(s.Captures, obj)
		}
	}
	m := substMap{}
	for i, tp := range s.fn.sig.tparams.slice() {
		m[tp] = args[i]
	}
	if owner := c.ownerOf(s.fn.obj); owner != nil {
		m[owner.named.class().self] = owner.named
	}
	if sig, ok := subst(s.fn.sig, m).(*Signature); ok {
		s.Generator = s.fn.gen
		if s.fn.gen {
			sig = &Signature{tparams: sig.tparams, params: sig.params, result: finalType(s.result)}
		}
		s.Signature = sig
	}
	s.Types, s.Calls, s.Vars, s.Dead, s.Patterns = s.types, s.calls, s.vars, s.dead, s.pats
	for p, t := range s.pats {
		s.pats[p] = finalType(t)
	}
}

// joinCap is the join of x and y under the caps.
func joinCap(x, y Type) Type {
	return capType(Join(x, y))
}

// capType widens t to Any when it exceeds the caps: more than unionCap
// members in a union, or more than depthCap levels of type arguments.
func capType(t Type) Type {
	if typeDepth(t) > depthCap {
		return cappedAny
	}
	if u, ok := t.(*Union); ok {
		if m := mergeClasses(u); m != t {
			return capType(m)
		}
		if u.Len() > unionCap {
			return cappedAny
		}
	}
	return t
}

func typeDepth(t Type) int {
	return depthOf(t, map[*elemCell]bool{})
}

func depthOf(t Type, seen map[*elemCell]bool) int {
	switch t := t.(type) {
	case *elemCell:
		return cellDepth(t, seen)
	case *Named:
		d := 0
		for _, a := range t.targs.slice() {
			d = max(d, depthOf(a, seen))
		}
		if t.targs.Len() > 0 {
			d++
		}
		return d
	case *Tuple:
		d := 0
		for _, e := range t.elems() {
			d = max(d, depthOf(e, seen))
		}
		return d + 1
	case *Union:
		d := 0
		for _, e := range t.terms {
			d = max(d, depthOf(e, seen))
		}
		return d
	}
	return 0
}
