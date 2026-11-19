package types

import (
	"strconv"
	"strings"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/token"
)

// spec is one specialization under analysis: the exported Spec plus the
// state of the fixpoint. args holds one type per type parameter of the
// function, nil for the implicit result parameter.
type spec struct {
	*Spec
	c       *solver
	fn      *fn
	key     string
	args    []Type
	result  Type
	readers map[*spec]struct{}
	prev    map[ast.Node]Type

	running, rerun, queued, done bool
	passes                       int

	types    map[ast.Expr]TypeAndValue
	calls    map[ast.Node]*Call
	names    map[*ast.Name]Definedness
	gnames   map[*ast.Name]Object
	vars     map[*Var]Type
	falls    []Fallback
	errs     []*Error
	escapes  []*spec
	loops    []*loop
	guards   []*guard
	yields   Type
	sends    Type
	unused   map[*ast.Yield]bool
	pats     map[ast.Pattern]Type
	guarded  map[Object]bool
	m        substMap
	declRet  Type
	ret      Type
	reach    bool
	stmts    []map[Object]Definedness
	callsAt  [][]*spec
	end      map[Object]Definedness
	cells    map[cellKey]*elemCell
	env      *spec
	cellLocs map[Object]*cellLoc
	escaped  bool
	inline   map[*ast.GeneratorExp]bool
	comps    []*Scope
	dead     map[ast.Stmt]bool
}

// argv is an argument of a call: its expression, type and keyword.
type argv struct {
	e    ast.Expr
	t    Type
	name string
}

// matched is the result of matching the arguments of a call to a signature.
type matched struct {
	types     map[*Var]Type
	exprs     map[*Var]ast.Expr
	extra     []Type
	kwTypes   []Type
	defaulted map[*Var]bool
}

// funcOf returns the analysis state of a function of the program, or nil
// for an extern.
func (c *solver) funcOf(f *Func) *fn {
	if fn, ok := c.fns[f]; ok {
		return fn
	}
	src := c.srcs[f.module]
	if src == nil {
		src = c.preludeSource(f)
	}
	if src == nil || len(f.defs) == 0 {
		return nil
	}
	def, ok := f.defs[0].(*ast.FunctionDef)
	fn := &fn{obj: f, def: def, src: src, scope: f.scope, sig: f.sig, byKey: map[string]*spec{}, defaults: map[*Var]Type{}}
	if src == c.prelude && ok {
		c.adopt(def)
		fn.presetDefaults(def)
		fn.perSite = true
	}
	fn.opaque = !ok || fn.scope == nil || awaits(def.Body) || c.decorated(f, def)
	fn.gen = ok && isGenerator(def.Body)
	if fn.sig != nil {
		if r, ok := fn.sig.result.(*TypeParam); ok && r.implicit {
			fn.ret = r
		}
	} else {
		fn.opaque = true
	}
	c.fns[f] = fn
	return fn
}

// decorated reports whether f has decorators or definitions that make it more
// than a plain function: the ones that bind a method, and the accessors of a
// property, are part of the language the analysis follows.
func (c *solver) decorated(f *Func, def *ast.FunctionDef) bool {
	if f.deco != plainFunc {
		return len(f.defs) > 1 && f.deco != propertyFunc
	}
	return len(f.defs) > 1 || len(def.DecoratorList) > 0 && !hasDecorators(f)
}

// specFor returns the specialization of f made in env for the argument types,
// creating it or, past a cap, the generalized one. seed starts the result of a
// new spec.
func (c *solver) specFor(f *fn, env *spec, args []Type, seed Type, at ast.Node) *spec {
	key := argsKey(args) + envKey(env)
	if f.perSite && at != nil {
		key += "@" + strconv.Itoa(int(at.Pos()))
	}
	if s := f.byKey[key]; s != nil {
		mergeArgs(s.args, args)
		return s
	}
	if len(f.list) < f.cap()+len(f.gens) && c.total < totalCap {
		s := c.newSpec(f, args, key)
		s.env = env
		if seed != nil {
			s.result = seed
		}
		c.enqueue(s)
		return s
	}
	return c.indirect(f, env, args)
}

// mergeArgs joins the argument types of a call into those of the version it
// shares with earlier calls, so that the containers they hold are one.
func mergeArgs(have, args []Type) {
	for i := range min(len(have), len(args)) {
		if have[i] != nil && args[i] != nil {
			Join(have[i], args[i])
		}
	}
}

// escape makes the function available to code the analysis cannot follow:
// the generalized version with every parameter Any.
func (c *solver) escape(f *fn, env *spec, given []Type) *spec {
	args := make([]Type, f.sig.tparams.Len())
	for i, tp := range f.sig.tparams.slice() {
		if tp != f.ret {
			args[i] = Typ[Any]
		}
	}
	for i, p := range f.sig.params.slice() {
		if tp, ok := p.typ.(*TypeParam); ok && tp.implicit && i < len(given) && p.paramKind <= PositionalOrKeyword {
			args[tp.index] = given[i]
		}
	}
	g := c.indirect(f, env, args)
	g.escaped = true
	c.escaped++
	return g
}

// indirect returns the version of f made in env that serves every call that
// does not name f: calls through a value, calls past the caps, and the code
// the analysis cannot follow. Its parameters are the join of the arguments
// of those calls. A nil entry of args leaves a parameter as it is.
func (c *solver) indirect(f *fn, env *spec, args []Type) *spec {
	g := f.gens[env]
	if g == nil {
		fresh := make([]Type, f.sig.tparams.Len())
		for i, tp := range f.sig.tparams.slice() {
			if tp != f.ret {
				fresh[i] = Typ[Never]
			}
		}
		g = c.newSpec(f, fresh, "*"+envKey(env))
		g.env = env
		g.Generalized = true
		delete(f.byKey, "*"+envKey(env))
		g.queued = false
		if f.gens == nil {
			f.gens = map[*spec]*spec{}
		}
		f.gens[env] = g
		c.enqueue(g)
	}
	changed := false
	for i, a := range args {
		if a == nil {
			continue
		}
		j := joinCap(g.args[i], a)
		if !Identical(j, g.args[i]) {
			g.args[i], changed = j, true
		}
	}
	if changed {
		if g.running {
			g.rerun = true
		} else {
			c.enqueue(g)
		}
	}
	return g
}

func (c *solver) newSpec(f *fn, args []Type, key string) *spec {
	s := &spec{
		Spec: &Spec{Func: f.obj}, c: c, fn: f, key: key, args: args,
		result: Typ[Never], readers: map[*spec]struct{}{}, prev: map[ast.Node]Type{},
	}
	if f.sig == nil {
		f.sig = &Signature{params: NewTuple(nil), result: builtin("NoneType")}
	}
	if f.def != nil && f.ret == nil {
		s.result = subst(f.sig.result, f.substMapOf(args))
	}
	c.specs[s.Spec] = s
	f.byKey[key] = s
	f.list = append(f.list, s)
	c.total++
	return s
}

// isGenerator reports whether the body of a function yields.
func isGenerator(body []ast.Stmt) bool {
	return anyIn(body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.Yield, *ast.YieldFrom:
			return true
		}
		return false
	})
}

// awaits reports whether the body of a function awaits.
func awaits(body []ast.Stmt) bool {
	return anyIn(body, func(n ast.Node) bool {
		_, ok := n.(*ast.Await)
		return ok
	})
}

// anyIn reports whether a node of the body, not looking into nested
// functions and classes, satisfies f.
func anyIn(body []ast.Stmt, f func(ast.Node) bool) bool {
	found := false
	for _, s := range body {
		ast.Inspect(s, func(n ast.Node) bool {
			switch n.(type) {
			case *ast.FunctionDef, *ast.AsyncFunctionDef, *ast.ClassDef, *ast.Lambda:
				return false
			}
			found = found || f(n)
			return !found
		})
	}
	return found
}

func argsKey(args []Type) string {
	var parts []string
	for _, a := range args {
		if a != nil {
			parts = append(parts, keyString(a))
		}
	}
	return strings.Join(parts, ";")
}

// matchArgs matches arguments to the parameters of sig as a call does. The
// message is empty when the call is well formed.
func matchArgs(sig *Signature, args []argv) (*matched, string) {
	b := &matched{types: map[*Var]Type{}, exprs: map[*Var]ast.Expr{}, defaulted: map[*Var]bool{}}
	g := groupParams(sig)
	i := 0
	for _, a := range args {
		if a.name != "" {
			continue
		}
		if i < len(g.pos) && g.pos[i].paramKind != VarPositional {
			b.types[g.pos[i]], b.exprs[g.pos[i]] = a.t, a.e
			i++
			continue
		}
		if g.varpos == nil {
			return nil, "too many positional arguments"
		}
		b.extra = append(b.extra, a.t)
	}
	for _, a := range args {
		if a.name == "" {
			continue
		}
		p := findKeyword(g, a.name)
		switch {
		case p != nil && b.types[p] == nil:
			b.types[p], b.exprs[p] = a.t, a.e
		case p != nil:
			return nil, "multiple values for argument " + a.name
		case g.varkw != nil:
			b.kwTypes = append(b.kwTypes, a.t)
		default:
			return nil, "unexpected keyword argument " + a.name
		}
	}
	for _, p := range append(append([]*Var{}, g.pos...), g.kwonly...) {
		if b.types[p] != nil {
			continue
		}
		if !p.hasDefault {
			return nil, "missing argument " + p.name
		}
		b.defaulted[p] = true
	}
	return b, ""
}

// localType is the type a parameter has inside the function once the type
// parameters are bound: *args is a tuple and **kwargs a dict.
func localType(p *Var, m substMap) Type {
	t := subst(p.typ, m)
	if tp, ok := p.typ.(*TypeParam); ok && tp.implicit {
		return t
	}
	switch p.paramKind {
	case VarPositional:
		return elemTuple([]Type{t}, 0)
	case VarKeyword:
		return newInstance(builtin("dict"), []Type{builtin("str"), t})
	}
	return t
}

// callArgs computes the type arguments of f for a call: what the implicit
// parameters take from the arguments, and what the explicit ones infer.
func (s *spec) callArgs(f *fn, b *matched) []Type {
	tps := f.sig.tparams.slice()
	args := make([]Type, len(tps))
	var explicit []*TypeParam
	for i, tp := range tps {
		args[i] = Typ[Any]
		switch {
		case tp == f.ret:
			args[i] = nil
		case !tp.implicit:
			explicit = append(explicit, tp)
		}
	}
	var from, to []Type
	for _, p := range f.sig.params.slice() {
		t, ok := b.types[p]
		if b.defaulted[p] {
			t, ok = f.defaults[p], true
			if t == nil {
				t = Typ[Any]
			}
		}
		if !ok {
			switch p.paramKind {
			case VarPositional:
				t = elemTuple(widenAll(b.extra), -1)
				if tp, isTP := p.typ.(*TypeParam); isTP && tp.implicit {
					args[tp.index] = capType(t)
				}
				continue
			case VarKeyword:
				var v Type = Typ[Never]
				for _, x := range b.kwTypes {
					v = joinCap(v, Default(x))
				}
				if isNever(v) {
					v = Typ[Any]
				}
				if tp, isTP := p.typ.(*TypeParam); isTP && tp.implicit {
					args[tp.index] = newInstance(builtin("dict"), []Type{builtin("str"), v})
				}
			}
			continue
		}
		t = Default(t)
		if tp, isTP := p.typ.(*TypeParam); isTP && tp.implicit {
			args[tp.index] = capType(t)
		} else {
			from, to = append(from, p.typ), append(to, t)
		}
	}
	if len(explicit) > 0 {
		if m, err := infer(explicit, from, to); err == nil {
			for _, tp := range explicit {
				args[tp.index] = capType(m[tp])
			}
		}
	}
	return args
}

func widenAll(ts []Type) []Type {
	out := make([]Type, len(ts))
	for i, t := range ts {
		out[i] = Default(t)
	}
	return out
}

// position returns the source position of p.
func (s *spec) position(p token.Pos) token.Position {
	return s.c.fset.Position(p)
}

// substMapOf pairs the type parameters of f with args.
func (f *fn) substMapOf(args []Type) substMap {
	m := substMap{}
	for i, tp := range f.sig.tparams.slice() {
		if args[i] != nil {
			m[tp] = args[i]
		}
	}
	return m
}

// cap is the number of specializations of f before the rest share one.
func (f *fn) cap() int {
	if f.perSite {
		return preludeCap
	}
	return specCap
}
