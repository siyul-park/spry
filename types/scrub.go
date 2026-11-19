package types

import (
	"reflect"

	"github.com/siyul-park/spry/ast"
)

// scrubber replaces, in place, every provenance Any reachable from the
// declared objects and the published facts by Typ[Any]. The checker keeps the
// provenance while it runs, because an explicit Any annotation opts an
// operation out of a fallback report; once the program is solved nothing reads
// it again, and no public value may expose it.
//
// The shipped stubs and the Universe are shared by every Check and hold no
// provenance Any (the declarer gives them Typ[Any]), so the scrubber never
// enters an object or class of a module the program does not own and writes
// a field only when its value changes: a Check never writes shared state.
type scrubber struct {
	seen map[any]bool
	own  map[*Module]bool
}

// scrub canonicalizes every Any that the facts of info reach.
func (c *solver) scrub() {
	w := &scrubber{seen: map[any]bool{}, own: map[*Module]bool{}}
	for _, s := range c.p.order {
		w.own[s.mod] = true
	}
	for _, s := range c.p.order {
		w.module(s.mod)
	}
	info := c.info
	for e, tv := range info.Types {
		tv.Type = w.fix(tv.Type)
		info.Types[e] = tv
	}
	for k, in := range info.Instances {
		in.Type = w.fix(in.Type)
		w.list(in.TypeArgs)
		info.Instances[k] = in
	}
	for _, objs := range []map[ast.Node]Object{info.Defs, info.Implicits} {
		for _, o := range objs {
			w.object(o)
		}
	}
	for _, o := range info.Uses {
		w.object(o)
	}
	for _, sel := range info.Selections {
		w.set(&sel.recv)
		w.set(&sel.typ)
		w.object(sel.obj)
		if sel.owner != nil {
			w.fix(sel.owner)
		}
	}
	for _, sc := range info.Scopes {
		w.scope(sc)
	}
	for f, specs := range info.Specs {
		w.object(f)
		for _, s := range specs {
			w.spec(s)
		}
	}
}

func (w *scrubber) module(m *Module) {
	if m == nil || !w.own[m] || w.visit(m) {
		return
	}
	w.scope(m.scope)
	w.object(m.init)
	for _, im := range m.imports {
		w.module(im)
	}
}

func (w *scrubber) spec(s *Spec) {
	if s == nil || w.visit(s) {
		return
	}
	w.object(s.Func)
	w.list(s.TypeArgs)
	if s.Signature != nil {
		w.fix(s.Signature)
	}
	for e, tv := range s.Types {
		tv.Type = w.fix(tv.Type)
		s.Types[e] = tv
	}
	for v, t := range s.Vars {
		w.object(v)
		s.Vars[v] = w.fix(t)
	}
	for p, t := range s.Patterns {
		s.Patterns[p] = w.fix(t)
	}
	for o, t := range s.Cells {
		w.object(o)
		s.Cells[o] = w.fix(t)
	}
	for _, o := range s.Captures {
		w.object(o)
	}
	w.spec(s.Outer)
}

func (w *scrubber) scope(s *Scope) {
	if s == nil || w.visit(s) {
		return
	}
	for _, o := range s.objs {
		w.object(o)
	}
}

func (w *scrubber) object(o Object) {
	switch o := o.(type) {
	case *Var:
		if o == nil || w.shared(o.module) || w.visit(o) {
			return
		}
		w.set(&o.typ)
		w.object(o.origin)
	case *Func:
		if o == nil || w.shared(o.module) || w.visit(o) {
			return
		}
		w.set(&o.typ)
		if o.sig != nil {
			w.fix(o.sig)
		}
		w.scope(o.scope)
	case *TypeName:
		if o == nil || w.shared(o.module) || w.visit(o) {
			return
		}
		w.set(&o.typ)
	case *Builtin:
		if o == nil || w.shared(o.module) || w.visit(o) {
			return
		}
		w.set(&o.typ)
	case *ModuleName:
		if o == nil || w.shared(o.module) || w.visit(o) {
			return
		}
		w.set(&o.typ)
		w.module(o.imported)
	}
}

func (w *scrubber) list(l *TypeList) {
	if l == nil {
		return
	}
	w.types(l.types)
}

func (w *scrubber) tparams(l *TypeParamList) {
	if l == nil {
		return
	}
	for _, p := range l.tparams {
		if p != nil {
			w.fix(p)
		}
	}
}

func (w *scrubber) types(ts []Type) {
	for i := range ts {
		w.set(&ts[i])
	}
}

// fix returns Typ[Any] for a provenance Any and t otherwise, after
// canonicalizing what t holds.
func (w *scrubber) fix(t Type) Type {
	switch t := t.(type) {
	case *Basic:
		if t != nil && t.kind == Any {
			return Typ[Any]
		}
		return t
	case nil:
		return nil
	}
	if reflect.ValueOf(t).IsNil() || w.visit(t) {
		return t
	}
	switch t := t.(type) {
	case *Alias:
		if !w.shared(t.obj.module) {
			t.Rhs()
			w.set(&t.rhs)
		}
		w.list(t.targs)
		w.tparams(t.tparams)
		w.object(t.obj)
	case *Named:
		w.list(t.targs)
		if w.shared(t.obj.module) {
			break
		}
		w.tparams(t.tparams)
		w.object(t.obj)
		if t.origin != nil {
			w.fix(t.origin)
		}
		w.class(t.cls)
	case *Union:
		w.types(t.terms)
	case *Tuple:
		for _, v := range t.vars {
			w.object(v)
		}
	case *Signature:
		w.tparams(t.tparams)
		if t.params != nil {
			w.fix(t.params)
		}
		w.set(&t.result)
	case *Overload:
		for _, s := range t.sigs {
			w.fix(s)
		}
	case *Meta:
		w.set(&t.elem)
	case *Guard:
		w.set(&t.typ)
	case *TypeParam:
		if t.obj != nil && w.shared(t.obj.module) {
			break
		}
		w.set(&t.bound)
		w.set(&t.def)
		w.types(t.constraints)
		w.object(t.obj)
	case *ParamSpecArgs:
		if t.param != nil {
			w.fix(t.param)
		}
	case *FuncValue:
		w.object(t.fn.obj)
		if t.fn.sig != nil {
			w.fix(t.fn.sig)
		}
		if t.env != nil {
			w.spec(t.env.Spec)
		}
	}
	return t
}

// set canonicalizes *t, writing only when it changes.
func (w *scrubber) set(t *Type) {
	if n := w.fix(*t); n != *t {
		*t = n
	}
}

func (w *scrubber) class(c *Class) {
	if c == nil || (c.obj != nil && w.shared(c.obj.module)) || w.visit(c) {
		return
	}
	w.object(c.obj)
	w.tparams(c.tparams)
	w.types(c.bases)
	for _, n := range c.mro {
		w.fix(n)
	}
	if c.meta != nil {
		w.fix(c.meta)
	}
	w.scope(c.members)
	if c.self != nil {
		w.fix(c.self)
	}
	for _, f := range c.fields {
		w.object(f)
	}
}

// shared reports whether m is a module the program does not own: a shipped
// stub, whose objects every Check reads and none may write.
func (w *scrubber) shared(m *Module) bool {
	return m != nil && !w.own[m]
}

// visit marks k and reports whether it was marked before.
func (w *scrubber) visit(k any) bool {
	if w.seen[k] {
		return true
	}
	w.seen[k] = true
	return false
}
