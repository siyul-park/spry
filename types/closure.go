package types

import (
	"slices"

	"github.com/siyul-park/spry/ast"
)

// FuncValue is the type of a function object of the program: a def or a lambda
// together with the specialization of the function that creates it, whose
// variables it captures. A value of this type is called through the version
// of the function that serves every call through a value (Generalized).
type FuncValue struct {
	fn  *fn
	env *spec
}

// cellLoc is the type of a variable that closures share: the join of every
// value stored in it.
type cellLoc struct {
	typ     Type
	readers map[*spec]struct{}
}

// Func returns the function the closure is made of.
func (c *FuncValue) Func() *Func {
	return c.fn.obj
}

// Env returns the specialization of the enclosing function, or nil for a
// function that is not nested.
func (c *FuncValue) Env() *Spec {
	if c.env == nil {
		return nil
	}
	return c.env.Spec
}

// Underlying returns the class of function objects.
func (c *FuncValue) Underlying() Type {
	return underlyingOf(builtin("function"), c)
}

// String implements Type.
func (c *FuncValue) String() string {
	return TypeString(c, nil)
}

// storeCell joins t into a captured variable owned by e.
func (s *spec) storeCell(e *spec, obj Object, t Type) {
	loc := e.cellOf(obj)
	next := joinCap(loc.typ, Default(t))
	if !Identical(next, loc.typ) {
		loc.typ = next
		s.c.notify(loc.readers)
	}
}

// callValue infers a call of a value that is a closure or a union of them.
func (s *spec) callValue(e *ast.Call, t Type, args []argv) (Type, bool) {
	terms := unionTerms(Unalias(t))
	var res Type = Typ[Never]
	var target *Spec
	for _, term := range terms {
		cl, ok := term.(*FuncValue)
		if !ok || cl.fn.opaque {
			return nil, false
		}
		b, msg := matchArgs(cl.fn.sig, args)
		if msg != "" {
			return s.dynamicReason(e, cl.fn.obj.name, reasonDynamicCall, msg, Typ[Any]), true
		}
		targs := s.callArgs(cl.fn, b)
		s.checkArgs(cl.fn, b, cl.fn.substMapOf(targs))
		g := s.c.indirect(cl.fn, cl.env, targs)
		s.c.ensure(g)
		g.readers[s] = struct{}{}
		res = joinCap(res, g.result)
		target = g.Spec
	}
	if len(terms) > 1 {
		target = nil
	}
	s.calls[e] = &Call{Kind: CallClosure, Target: target, Overload: -1, Slot: "__call__"}
	return res, true
}

// escapeClosures makes the closures in t callable by code the analysis cannot
// follow: every parameter of the version that serves them is Any.
func (s *spec) escapeClosures(t, declared Type) {
	given := callbackParams(declared)
	for _, term := range unionTerms(Unalias(t)) {
		if cl, ok := term.(*FuncValue); ok {
			s.escapes = append(s.escapes, s.c.escape(cl.fn, cl.env, given))
		}
	}
}

// callbackParams are the types the code that calls a function passes, when
// the type it expects says: those of the positional parameters of a callable
// type. A parameter it does not say is Any.
func callbackParams(declared Type) []Type {
	for _, term := range unionTerms(Unalias(declared)) {
		sig, ok := term.(*Signature)
		if !ok {
			continue
		}
		var out []Type
		for _, p := range groupParams(sig).pos {
			t := Default(norm(p.typ))
			if _, isParam := t.(*TypeParam); isParam {
				t = Typ[Any]
			}
			out = append(out, t)
		}
		return out
	}
	return nil
}

// freeValue reads a variable of an enclosing function.
func (s *spec) freeValue(n *ast.Name) Type {
	obj, e := s.freeObject(n.ID)
	if obj == nil || e == nil {
		return Typ[Any]
	}
	s.c.info.Uses[n] = obj
	if f, ok := obj.(*Func); ok {
		if hasDecorators(f) {
			return s.loadCell(e, obj)
		}
		if fn := s.c.funcOf(f); fn != nil && !fn.opaque {
			return s.closureOf(fn)
		}
		return Typ[Any]
	}
	return s.loadCell(e, obj)
}

// freeObject finds the variable of an enclosing function that a free name of
// s refers to, and the specialization that owns it.
func (s *spec) freeObject(name string) (Object, *spec) {
	for sc := s.fn.scope.Parent(); sc != nil; sc = sc.Parent() {
		if sc.Kind() != FunctionScope && sc.Kind() != ComprehensionScope {
			continue
		}
		switch sc.storage(name) {
		case storeLocal, storeCell:
			if obj := sc.Lookup(name); obj != nil {
				return obj, s.env.owner(obj)
			}
		}
	}
	return nil, nil
}

// loadCell reads a captured variable owned by e on behalf of s.
func (s *spec) loadCell(e *spec, obj Object) Type {
	loc := e.cellOf(obj)
	loc.readers[s] = struct{}{}
	return loc.typ
}

func (e *spec) cellOf(obj Object) *cellLoc {
	if e.cellLocs == nil {
		e.cellLocs = map[Object]*cellLoc{}
	}
	loc := e.cellLocs[obj]
	if loc == nil {
		loc = &cellLoc{typ: Typ[Never], readers: map[*spec]struct{}{}}
		e.cellLocs[obj] = loc
	}
	return loc
}

// lambdaExpr infers a lambda expression: a function object.
func (s *spec) lambdaExpr(e *ast.Lambda, v *env) Type {
	f := s.c.lambdaFn(s.fn.src, e)
	if f == nil {
		return s.anyValue(e)
	}
	s.defaults(f, e.Args, v)
	s.noteCaptures(f, v)
	return s.closureOf(f)
}

// lambdaFn returns the function of a lambda expression.
func (c *solver) lambdaFn(src *source, l *ast.Lambda) *fn {
	if f := c.lambdas[l]; f != nil {
		return f
	}
	obj := c.p.decl.newLambda(src, l)
	if obj == nil {
		return nil
	}
	c.info.Defs[l] = obj
	f := c.funcOf(obj)
	f.lambda = true
	c.lambdas[l] = f
	return f
}

// valueOfFunc is the value a nested def binds its name to.
func (s *spec) valueOfFunc(obj *Func) Type {
	if fn := s.c.funcOf(obj); fn != nil && !fn.opaque {
		return s.closureOf(fn)
	}
	return Typ[Any]
}

// closureOf is the value of a function object created in s.
func (s *spec) closureOf(f *fn) Type {
	env := s.owner(f.obj)
	if f.obj.Parent() == nil || f.obj.Parent().Kind() == ModuleScope {
		env = nil
	}
	if f.lambda {
		env = s
	}
	cl := &FuncValue{fn: f, env: env}
	s.escapes = append(s.escapes, s.c.indirect(f, env, nil))
	return cl
}

// owner finds the specialization whose body owns obj among s and the
// functions around it.
func (s *spec) owner(obj Object) *spec {
	for e := s; e != nil; e = e.env {
		if e.ownedHere(obj) {
			return e
		}
	}
	return nil
}

// noteCaptures records the variables of s that the function f, which is made
// here, reads while they may be unbound: the first read can happen before they
// are assigned.
func (s *spec) noteCaptures(f *fn, v *env) {
	for _, name := range f.freeNames() {
		if obj := capturedBy(f, name); obj != nil && s.ownedHere(obj) && v.lookup(obj).def != Defined {
			s.guard(obj)
		}
	}
}

// freeNames lists the names s captures, in a fixed order.
func (f *fn) freeNames() []string {
	var out []string
	for name, st := range f.scope.syms {
		if st == storeFree {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// ownedHere reports whether obj is a variable of the body of s, including the
// comprehensions inlined in it.
func (s *spec) ownedHere(obj Object) bool {
	for sc := obj.Parent(); sc != nil; sc = sc.Parent() {
		if sc == s.fn.scope {
			return true
		}
		if sc.Kind() != ComprehensionScope {
			return false
		}
	}
	return false
}

// guard records that a read of the captured variable obj needs to check that
// it is bound.
func (s *spec) guard(obj Object) {
	if !isCell(obj) {
		return
	}
	if s.guarded == nil {
		s.guarded = map[Object]bool{}
	}
	s.guarded[obj] = true
}

// capturedBy finds the variable of an enclosing function that a free name of
// f refers to.
func capturedBy(f *fn, name string) Object {
	for sc := f.scope.Parent(); sc != nil; sc = sc.Parent() {
		if sc.Kind() != FunctionScope && sc.Kind() != ComprehensionScope {
			continue
		}
		switch sc.storage(name) {
		case storeLocal, storeCell:
			return sc.Lookup(name)
		}
	}
	return nil
}

// acceptsCall reports whether a parameter of type t is something the callee
// may call: a callable type or Any.
func acceptsCall(t Type) bool {
	for _, term := range unionTerms(Unalias(t)) {
		switch term.(type) {
		case *Signature, *Overload:
			return true
		}
		if isAny(term) {
			return true
		}
	}
	return false
}

// envKey identifies a specialization for the key of the closures it makes.
func envKey(e *spec) string {
	if e == nil {
		return ""
	}
	return "@" + e.fn.obj.fullName + "(" + e.key + ")" + envKey(e.env)
}

// isNestedFunc reports whether f is defined inside a function.
func isNestedFunc(f *Func) bool {
	p := f.Parent()
	return p != nil && (p.Kind() == FunctionScope || p.Kind() == ComprehensionScope)
}

// isCell reports whether obj is a variable that a nested function captures.
func isCell(obj Object) bool {
	p := obj.Parent()
	if p == nil || p.Kind() != FunctionScope && p.Kind() != ComprehensionScope {
		return false
	}
	switch obj.(type) {
	case *Var, *Func:
		return p.storage(obj.Name()) == storeCell
	}
	return false
}
