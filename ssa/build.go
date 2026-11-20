package ssa

import (
	"slices"
	"strings"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/token"
	"github.com/siyul-park/spry/types"
)

// builder lowers a checked program to SSA: one Module per module of the
// program, one Function per specialization, and the externs the program
// calls.
type builder struct {
	fset     *token.FileSet
	info     *types.Info
	prog     *Program
	mods     map[*types.Module]*Module
	funcs    map[*types.Spec]*Function
	externs  map[externKey]*Function
	excs     map[string]*Function
	classes  map[string]*Class
	globals  map[*types.Var]*Global
	flags    map[types.Object]*Global
	defaults map[defaultKey]*Global
	maybe    map[types.Object]bool
	stubs    map[string]*Module
	rng      *Function
	lowering map[*types.Spec]bool
	names    map[*Module]map[string]int
	helpers  map[string]*Function
	pending  []classDecl
	slots    map[string]bool
	ready    bool
	complete map[*Class]bool
	origins  map[*Class]*types.Named
}

type defaultKey struct {
	f *types.Func
	p *types.Var
}

// Build lowers the program that types.Check checked to SSA and verifies the
// result. Every module becomes a module of the program whose Init runs the
// module body once; the Entry runs the main module.
func Build(fset *token.FileSet, main *types.Module, info *types.Info) (*Program, error) {
	b := &builder{
		fset: fset, info: info, prog: &Program{},
		mods: map[*types.Module]*Module{}, funcs: map[*types.Spec]*Function{}, externs: map[externKey]*Function{},
		excs: map[string]*Function{}, classes: map[string]*Class{}, globals: map[*types.Var]*Global{},
		flags: map[types.Object]*Global{}, defaults: map[defaultKey]*Global{}, maybe: map[types.Object]bool{}, stubs: map[string]*Module{},
		lowering: map[*types.Spec]bool{}, names: map[*Module]map[string]int{}, helpers: map[string]*Function{}, slots: map[string]bool{}, origins: map[*Class]*types.Named{},
	}
	for n, d := range info.Defined {
		if obj := info.Uses[n]; d == types.MaybeDefined && obj != nil {
			b.maybe[obj] = true
		}
	}
	order := userModules(main)
	for _, m := range order {
		mod := &Module{Name: m.Name()}
		b.mods[m] = mod
		b.prog.Modules = append(b.prog.Modules, mod)
	}
	for _, m := range order {
		b.declare(m)
	}
	b.declareClasses(order)
	prelude := b.preludeModules(order)
	for _, m := range prelude {
		b.declareFuncs(m, b.stubModule(m))
	}
	for _, m := range append(order, prelude...) {
		for _, f := range b.funcsOf(m) {
			for _, s := range info.Specs[f] {
				b.lowerSpec(s)
			}
		}
	}
	b.prog.Entry = b.mods[main].Init
	if err := Verify(b.prog); err != nil {
		return b.prog, err
	}
	return b.prog, nil
}

// userModules lists main and what it imports that has a body, each module
// after its imports.
func userModules(main *types.Module) []*types.Module {
	var out []*types.Module
	seen := map[*types.Module]bool{}
	var walk func(m *types.Module)
	walk = func(m *types.Module) {
		if seen[m] {
			return
		}
		seen[m] = true
		for _, x := range m.Imports() {
			walk(x)
		}
		if m.Init() != nil {
			out = append(out, m)
		}
	}
	walk(main)
	return out
}

// declare creates the globals of m and the shells of its functions, so that
// calls can refer to functions that are lowered later.
func (b *builder) declare(m *types.Module) {
	mod := b.mods[m]
	mod.Globals = append(mod.Globals, &Global{Name: "$init", Type: tyI1, Module: mod})
	for _, name := range m.Scope().Names() {
		if v, ok := m.Scope().Lookup(name).(*types.Var); ok && v.Kind() == types.GlobalVar && v.Module() == m {
			b.globalOf(v)
		}
	}
	for _, obj := range b.maybeOf(m) {
		g := &Global{Name: obj.Name() + "$bound", Type: tyI1, Module: mod}
		mod.Globals = append(mod.Globals, g)
		b.flags[obj] = g
	}
	b.declareFuncs(m, mod)
}

// declareFuncs declares the specializations of the functions of m in mod.
func (b *builder) declareFuncs(m *types.Module, mod *Module) {
	for _, f := range b.funcsOf(m) {
		specs := b.info.Specs[f]
		base := b.unique(mod, f.FullName())
		for i, s := range specs {
			name := base
			if len(specs) > 1 {
				name += "#" + itoa(i)
			}
			b.lowering[s] = true
			fn := &Function{Name: name, Source: f.FullName(), Module: mod, Result: b.lower(resultOf(s))}
			if t := fn.Result; s.Generator && t.Kind == KindGen {
				fn.Generator, fn.Yield, fn.Send, fn.Result = true, t.Elems[0], t.Elems[1], t.Elems[2]
			}
			if f == m.Init() {
				mod.Init = fn
				fn.Result = tyNone
			} else {
				fn.Params = b.params(s)
			}
			delete(b.lowering, s)
			if f != m.Init() {
				fn.Captures = b.captures(s)
			}
			mod.Functions = append(mod.Functions, fn)
			b.funcs[s] = fn
		}
	}
}

// unique makes the name of a function distinct from the earlier functions of
// the module that have the same qualified name, as two lambdas have.
// preludeModules lists the shipped modules that have functions with code,
// whose specializations are compiled with the program.
func (b *builder) preludeModules(order []*types.Module) []*types.Module {
	var out []*types.Module
	for f := range b.info.Specs {
		m := f.Module()
		if m != nil && !slices.Contains(order, m) && !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	slices.SortFunc(out, func(x, y *types.Module) int { return strings.Compare(x.Name(), y.Name()) })
	return out
}

func (b *builder) unique(m *Module, name string) string {
	if b.names[m] == nil {
		b.names[m] = map[string]int{}
	}
	n := b.names[m][name]
	b.names[m][name]++
	if n > 0 {
		return name + "$" + itoa(n)
	}
	return name
}

// funcsOf lists the functions of m that have specializations, the body of the
// module first.
func (b *builder) funcsOf(m *types.Module) []*types.Func {
	var out []*types.Func
	for f := range b.info.Specs {
		if f.Module() == m {
			out = append(out, f)
		}
	}
	slices.SortFunc(out, func(x, y *types.Func) int { return int(x.Pos()) - int(y.Pos()) })
	return out
}

// maybeOf lists, in declaration order, the objects of m that a read may find
// unbound.
func (b *builder) maybeOf(m *types.Module) []types.Object {
	var out []types.Object
	for _, name := range m.Scope().Names() {
		if obj := m.Scope().Lookup(name); b.maybe[obj] && obj.Module() == m {
			out = append(out, obj)
		}
	}
	return out
}

// params lists the IR parameters of a specialization: each takes the type its
// variable has in the body.
func (b *builder) params(s *types.Spec) []*Param {
	var ps []*Param
	sig := s.Func.Signature()
	for i := range sig.Params().Len() {
		p := sig.Params().At(i)
		t, ok := s.Vars[p]
		if !ok {
			t = s.Signature.Params().At(i).Type()
		}
		ps = append(ps, &Param{Name: paramName(p.Name()), Typ: b.lower(t)})
	}
	return dedupeParams(ps)
}

// defaultGlobal returns the global that holds the value of a default
// argument that is not a constant, stored where the function is defined.
func (b *builder) defaultGlobal(f *types.Func, p *types.Var, t Type) *Global {
	k := defaultKey{f, p}
	if g := b.defaults[k]; g != nil {
		return g
	}
	mod := b.mods[f.Module()]
	g := &Global{Name: f.FullName() + "$default$" + p.Name(), Type: t, Module: mod}
	mod.Globals = append(mod.Globals, g)
	b.defaults[k] = g
	return g
}

// lowerSpec builds the body of one specialization.
func (b *builder) lowerSpec(s *types.Spec) {
	fn := b.funcs[s]
	f := newFbuild(b, fn, s)
	for obj, t := range s.Vars {
		f.vtypes[obj] = b.lower(t)
	}
	init := s.Func == s.Func.Module().Init()
	var body []ast.Stmt
	if init {
		body = f.startModule()
	} else {
		sig := s.Func.Signature()
		for i := range sig.Params().Len() {
			p := sig.Params().At(i)
			f.vtypes[p] = fn.Params[i].Typ
			f.writeVar(bkey{obj: p}, f.cur, fn.Params[i])
		}
		body = s.Func.Decl().(*ast.FunctionDef).Body
	}
	f.openCells()
	f.declareFuncs(body)
	f.stmts(body)
	f.fallOff(init)
	f.finish()
}

// globalOf returns the global of a module variable, creating it in the module
// that owns the variable.
func (b *builder) globalOf(v *types.Var) *Global {
	if g := b.globals[v]; g != nil {
		return g
	}
	var mod *Module
	if m := v.Module(); b.mods[m] != nil {
		mod = b.mods[m]
	} else {
		mod = b.stubModule(m)
	}
	g := &Global{Name: v.Name(), Type: b.lower(v.Type()), Module: mod}
	mod.Globals = append(mod.Globals, g)
	b.globals[v] = g
	return g
}

func resultOf(s *types.Spec) types.Type {
	if s.Signature == nil {
		return types.Typ[types.Any]
	}
	return s.Signature.Result()
}
