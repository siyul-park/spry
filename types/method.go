package types

import (
	"slices"

	"github.com/siyul-park/spry/ast"
)

// methodCall is a call of a method of a class of the program.
type methodCall struct {
	site ast.Node
	// recv is the type of the receiver: an instance, or type[C] for a class
	// method.
	recv Type
	name string
	args []argv
	// exact says that the class of the receiver is known to be recv itself,
	// as for a constructor or a class method, so that no subclass is
	// considered.
	exact bool
	// try makes a call whose arguments do not fit report false, for an
	// operation that has another method to try.
	try bool
	// accessor is "setter" to call the setter of a property.
	accessor string
	// super is the class around a super() call: the method is found after
	// it in the MRO of the receiver.
	super *classInfo
}

// impl is the method that one class of the receiver's cone runs.
type impl struct {
	class *Named
	fn    *Func
	owner *Named
}

// userMethod resolves the call of a method of a class of the program that an
// operation dispatches to, such as __add__ for +. It reports false for any
// other receiver, which the operation resolves through the standard library.
func (s *spec) userMethod(site ast.Node, recv Type, name string, args []argv) (Type, *Call, bool) {
	res, call, ok := s.userMethodTry(site, recv, name, args, false)
	if ok && notImplemented(res) {
		return s.dynamicReason(site, name, "not-implemented", "the method may return NotImplemented", Typ[Any]), &Call{Kind: Dynamic, Slot: name, Overload: -1}, true
	}
	return res, call, ok
}

func notImplemented(t Type) bool {
	for _, term := range unionTerms(t) {
		if isBuiltin(term, "NotImplementedType") {
			return true
		}
	}
	return false
}

func (s *spec) userMethodTry(site ast.Node, recv Type, name string, args []argv, try bool) (Type, *Call, bool) {
	if s.c.instanceOf(Default(recv)) == nil {
		return nil, nil, false
	}
	sel := LookupAttr(Default(recv), name)
	if sel == nil || sel.kind != MethodVal {
		return nil, nil, false
	}
	res, call, ok := s.callMethod(methodCall{site: site, recv: Default(recv), name: name, args: args, try: try})
	if ok && call.Target == nil && call.Kind == Static {
		return nil, nil, false
	}
	return res, call, ok
}

// callMethod resolves a call of a method of a class of the program: the
// implementations its receiver class and the classes below it run, and one
// specialization of each. The call is static if they are one function, and
// virtual otherwise. It reports false if the receiver is not such a class or
// the attribute is not a method.
func (s *spec) callMethod(m methodCall) (Type, *Call, bool) {
	ci := s.receiver(m.recv)
	if ci == nil || ci.bad {
		return nil, nil, false
	}
	impls, ok := s.c.impls(ci, m)
	if !ok {
		return nil, nil, false
	}
	if fn := s.c.funcOf(impls[0].fn); fn == nil {
		return s.libMethod(m, impls[0].fn)
	}
	var res Type = Typ[Never]
	call := &Call{Kind: Static, Func: impls[0].fn, Overload: -1, Slot: m.name, Super: m.superNamed()}
	bound := map[*Func]*Spec{}
	for _, im := range impls {
		t := bound[im.fn]
		if t == nil {
			fn := s.c.funcOf(im.fn)
			if fn == nil || fn.opaque {
				return nil, nil, false
			}
			key := m.site
			if len(bound) > 0 {
				key = Site{m.site, SiteOverride, len(bound)}
			}
			sp, r, msg := s.bindSpec(key, m.site, fn, m.args, im.fn.deco != staticFunc)
			if msg != "" && m.try {
				return nil, nil, false
			}
			if msg != "" {
				return s.dynamicReason(m.site, m.name, reasonDynamicCall, msg, Typ[Any]), &Call{Kind: Dynamic, Slot: m.name, Overload: -1}, true
			}
			res, t = joinCap(res, r), sp.Spec
			bound[im.fn] = t
		}
		call.Overrides = append(call.Overrides, Override{Class: im.class, Target: t})
	}
	call.Target = call.Overrides[0].Target
	if len(bound) == 1 {
		call.Overrides = nil
	} else {
		call.Kind = Virtual
	}
	return res, call, true
}

// libMethod is a call of a method of the standard library that a class of
// the program inherits, such as object.__init__.
func (s *spec) libMethod(m methodCall, f *Func) (Type, *Call, bool) {
	res, idx, ok := matchType(dropFirst(f.Type()), m.args)
	if !ok {
		return nil, nil, false
	}
	return s.fromLib(m.site, res), &Call{Kind: Static, Func: f, Overload: idx, Slot: m.name, Super: m.superNamed()}, true
}

// bindSpec binds the arguments of a call to the parameters of fn, which
// ignores its receiver when drop is set, and returns the specialization it
// calls and the join of its results at this site over the passes. The message
// is not empty when the arguments do not fit.
func (s *spec) bindSpec(key, at ast.Node, fn *fn, args []argv, drop bool) (*spec, Type, string) {
	sig := fn.sig
	if drop {
		if !takesSelf(sig) {
			return nil, nil, "the method has no parameter for the receiver"
		}
		sig, _ = dropFirst(sig).(*Signature)
	}
	b, msg := matchArgs(sig, args)
	if msg != "" {
		return nil, nil, msg
	}
	targs := s.callArgs(fn, b)
	s.checkArgs(fn, b, fn.substMapOf(targs))
	t := s.c.specFor(fn, nil, targs, s.prev[key], at)
	s.c.ensure(t)
	t.readers[s] = struct{}{}
	res := t.result
	if p := s.prev[key]; p != nil {
		res = joinCap(p, res)
	}
	s.prev[key] = res
	if s.isInit() {
		s.callsAt[len(s.callsAt)-1] = append(s.callsAt[len(s.callsAt)-1], t)
	}
	return t, res, ""
}

// takesSelf reports whether a method of the signature can take its receiver:
// its first parameter is positional, or collects the positional arguments.
func takesSelf(sig *Signature) bool {
	params := sig.params.slice()
	return len(params) > 0 && params[0].paramKind <= VarPositional
}

// receiver returns the class of the receiver of a method call.
func (s *spec) receiver(t Type) *classInfo {
	switch t := Unalias(Default(t)).(type) {
	case *Meta:
		return s.c.instanceOf(t.elem)
	case *Named:
		return s.c.classes[t.origin]
	}
	return nil
}

// superCall recognizes super() and super(C, self) as the receiver of a method
// call, and returns the class around the call.
func (s *spec) superCall(e ast.Expr) (*classInfo, bool) {
	call, ok := e.(*ast.Call)
	if !ok || len(call.Keywords) > 0 || len(call.Args) != 0 && len(call.Args) != 2 {
		return nil, false
	}
	n, ok := call.Func.(*ast.Name)
	if !ok || n.ID != "super" {
		return nil, false
	}
	if obj, _ := s.resolve("super"); obj == nil || obj.Parent() != Universe {
		return nil, false
	}
	owner := s.c.ownerOf(s.fn.obj)
	if len(call.Args) == 2 {
		if t, ok := s.expr(call.Args[0], deadEnv()).(*Meta); ok {
			owner = s.c.instanceOf(t.elem)
		}
	}
	if owner == nil || owner.bad {
		return nil, false
	}
	return owner, true
}

// impls lists the distinct methods the classes of the cone of the receiver
// run for the call, the one of the receiver class first. It reports false if
// the name is not a method in one of them.
func (c *solver) impls(ci *classInfo, m methodCall) ([]impl, bool) {
	cone := ci.subs
	if m.exact || m.super == nil && isMeta(m.recv) {
		cone = []*classInfo{ci}
	}
	var after *Named
	if m.super != nil {
		cone, after = m.super.subs, m.super.named
	}
	var out []impl
	for _, k := range cone {
		if k.bad {
			continue
		}
		f, owner := c.definer(k.named, m.name, after)
		if m.accessor == "setter" && f != nil {
			f = c.setterOf(f)
		}
		if f == nil {
			return nil, false
		}
		entry := owner
		if m.super != nil {
			entry = k.named
		}
		if m.super == nil && slices.ContainsFunc(out, func(x impl) bool { return x.fn == f }) {
			continue
		}
		out = append(out, impl{class: entry, fn: f, owner: owner})
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// definer finds the class of k's MRO, after the given class if there is one,
// that defines name, and the function it defines.
func (c *solver) definer(k *Named, name string, after *Named) (*Func, *Named) {
	mro := resolvedMRO(k)
	if after != nil {
		i := slices.IndexFunc(mro, func(n *Named) bool { return n.origin == after.origin })
		if i < 0 {
			return nil, nil
		}
		mro = mro[i+1:]
	}
	for _, n := range mro {
		if obj := n.class().members.Lookup(name); obj != nil {
			f, _ := obj.(*Func)
			return f, n
		}
	}
	return nil, nil
}

// setterOf returns the function of the setter of a property, or nil.
func (c *solver) setterOf(f *Func) *Func {
	if s, ok := c.setters[f]; ok {
		return s
	}
	if c.setters == nil {
		c.setters = map[*Func]*Func{}
	}
	c.setters[f] = nil
	d := c.p.decl
	dc := d.decls[f]
	if f.deco != propertyFunc || dc == nil {
		return nil
	}
	for _, def := range f.defs[1:] {
		fd, ok := def.(*ast.FunctionDef)
		if !ok || !isSetter(fd) {
			continue
		}
		fb := d.blockOf(fd)
		if fb == nil {
			return nil
		}
		set := &Func{object: object{pos: fd.Pos(), module: f.module, name: f.name}, fullName: f.fullName, defs: []ast.Stmt{fd}, scope: fb.scope, deco: propertyFunc}
		set.sig = d.signature(fd, dc)
		set.typ = set.sig
		d.decls[set] = dc
		c.setters[f] = set
		return set
	}
	return nil
}
func isSetter(def *ast.FunctionDef) bool {
	for _, d := range def.DecoratorList {
		if a, ok := d.(*ast.Attribute); ok && a.Attr == "setter" {
			return true
		}
	}
	return false
}

// ownerOf returns the class a method is declared in.
func (c *solver) ownerOf(f *Func) *classInfo {
	d := c.p.decl
	dc := d.decls[f]
	if dc == nil || dc.scope == nil {
		return nil
	}
	n := d.classes[dc.scope]
	if n == nil {
		return nil
	}
	return c.classes[n.origin]
}

func isMeta(t Type) bool {
	_, ok := Unalias(t).(*Meta)
	return ok
}

func (m methodCall) superNamed() *Named {
	if m.super == nil {
		return nil
	}
	return m.super.named
}
