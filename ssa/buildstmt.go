package ssa

import (
	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/types"
)

func (f *fbuild) stmts(list []ast.Stmt) {
	for _, st := range list {
		if !f.alive() {
			return
		}
		if f.spec.Dead[st] {
			continue
		}
		f.stmt(st)
	}
}

// stmt lowers one statement. A statement the checker does not infer yet is
// skipped, except a raise, which still ends the block.
func (f *fbuild) stmt(st ast.Stmt) {
	f.origin = st
	switch st := st.(type) {
	case *ast.ExprStmt:
		f.expr(st.Value)
	case *ast.Assign:
		f.assignStmt(st)
	case *ast.AnnAssign:
		if st.Value != nil {
			f.assign(st.Target, f.expr(st.Value), st.Value)
		}
	case *ast.AugAssign:
		f.augAssign(st)
	case *ast.Return:
		f.returnStmt(st)
	case *ast.If:
		f.ifStmt(st)
	case *ast.While:
		f.whileStmt(st)
	case *ast.For:
		f.forStmt(st)
	case *ast.Break:
		if n := len(f.loops); n > 0 {
			f.leave(f.loops[n-1].depth, f.loops[n-1].brk, nil)
		}
	case *ast.Continue:
		if n := len(f.loops); n > 0 {
			f.leave(f.loops[n-1].depth, f.loops[n-1].cont, nil)
		}
	case *ast.FunctionDef:
		f.funcDef(st)
	case *ast.ClassDef:
		f.classDef(st)
	case *ast.Import:
		f.importStmt(st)
	case *ast.ImportFrom:
		f.importFrom(st)
	case *ast.Delete:
		f.deleteStmt(st)
	case *ast.Raise:
		f.raiseStmt(st)
	case *ast.Try:
		f.tryStmt(st)
	case *ast.With:
		f.withStmt(st.Items, st.Body)
	case *ast.Assert:
		f.assertStmt(st)
	case *ast.Match:
		f.matchStmt(st)
	}
}

func (f *fbuild) ifStmt(st *ast.If) {
	yes, no, join := f.newBlock(), f.newBlock(), f.newBlock()
	f.cond(st.Test, yes, no)
	f.seal(yes)
	f.seal(no)
	f.start(yes)
	f.stmts(st.Body)
	f.jump(join)
	f.start(no)
	f.stmts(st.OrElse)
	f.jump(join)
	f.merge(join)
}

func (f *fbuild) whileStmt(st *ast.While) {
	head, body, orelse, exit := f.newBlock(), f.newBlock(), f.newBlock(), f.newBlock()
	f.jump(head)
	f.start(head)
	f.cond(st.Test, body, orelse)
	f.seal(body)
	f.seal(orelse)
	f.start(body)
	f.loops = append(f.loops, &loopCtx{brk: exit, cont: head, depth: len(f.regions)})
	f.stmts(st.Body)
	f.loops = f.loops[:len(f.loops)-1]
	f.jump(head)
	f.seal(head)
	f.start(orelse)
	f.stmts(st.OrElse)
	f.jump(exit)
	f.merge(exit)
}

func (f *fbuild) forStmt(st *ast.For) {
	item := f.b.lower(f.typeOf(st.Target))
	it := f.iterValue(st, f.expr(st.Iter), item)
	head, orelse, cur := f.items(types.Site{Node: st, Kind: types.SiteNext}, it, item)
	exit := f.newBlock()
	f.assign(st.Target, cur, nil)
	f.loops = append(f.loops, &loopCtx{brk: exit, cont: head, depth: len(f.regions)})
	f.stmts(st.Body)
	f.loops = f.loops[:len(f.loops)-1]
	f.jump(head)
	f.seal(head)
	f.start(orelse)
	f.stmts(st.OrElse)
	f.jump(exit)
	f.merge(exit)
}

func (f *fbuild) assignStmt(st *ast.Assign) {
	if len(st.Targets) == 1 {
		if tup, ok := st.Targets[0].(*ast.Tuple); ok {
			if src, ok := st.Value.(*ast.Tuple); ok && len(src.Elts) == len(tup.Elts) {
				vals := make([]Value, len(src.Elts))
				for i, x := range src.Elts {
					vals[i] = f.expr(x)
				}
				for i, t := range tup.Elts {
					f.assign(t, vals[i], src.Elts[i])
				}
				return
			}
		}
	}
	v := f.expr(st.Value)
	for _, t := range st.Targets {
		f.assign(t, v, st.Value)
	}
}

// assign stores v in a target: a name, a subscript, or a tuple or list of
// targets that v unpacks.
func (f *fbuild) assign(target ast.Expr, v Value, value ast.Expr) {
	switch target := target.(type) {
	case *ast.Name:
		f.bindName(target, v)
	case *ast.Tuple:
		f.unpack(target.Elts, v)
	case *ast.List:
		f.unpack(target.Elts, v)
	case *ast.Subscript:
		f.storeSubscript(target, v)
	case *ast.Attribute:
		f.storeAttribute(target, v)
	}
}

func (f *fbuild) augAssign(st *ast.AugAssign) {
	switch t := st.Target.(type) {
	case *ast.Name:
		l := f.expr(t)
		r := f.expr(st.Value)
		f.bindName(t, f.arith(st, st.Op, l, f.typeOf(t), r, f.typeOf(st.Value), st.Value, Type{}))
	case *ast.Subscript:
		f.augSubscript(st, t)
	case *ast.Attribute:
		f.augAttribute(st, t)
	}
}

func (f *fbuild) bindName(n *ast.Name, v Value) {
	obj := f.b.info.Defs[n]
	if obj == nil {
		obj = f.b.info.Uses[n]
	}
	f.bindObject(obj, v)
}

// bindObject assigns v to the variable obj.
func (f *fbuild) bindObject(obj types.Object, v Value) {
	if va, ok := obj.(*types.Var); ok {
		if va.Kind() == types.GlobalVar {
			f.store(f.b.globalOf(va), v)
		} else if c := f.cells[va]; c != nil {
			f.storeCell(c, v)
			f.setBound(va, true)
		} else {
			conv := f.conv(v, f.varType(bkey{obj: va}))
			f.writeVar(bkey{obj: va}, f.cur, conv)
		}
	}
	if obj != nil {
		f.markBound(obj)
	}
}

func (f *fbuild) returnStmt(st *ast.Return) {
	res := f.fn.Result
	var v Value
	if st.Value != nil {
		v = f.expr(st.Value)
	}
	if !f.alive() {
		return
	}
	if res.same(tyNone) {
		f.leave(0, nil, nil)
		return
	}
	if v == nil {
		v = &Const{Value: ast.None{}, Typ: tyNone}
	}
	f.leave(0, nil, f.conv(v, res))
}

// merge continues in a block whose predecessors are all known, if it is
// reachable.
func (f *fbuild) merge(blk *Block) {
	f.seal(blk)
	if len(f.in[blk]) == 0 {
		f.cur = nil
		return
	}
	f.start(blk)
}

func (f *fbuild) funcDef(st *ast.FunctionDef) {
	obj, ok := f.b.info.Defs[st].(*types.Func)
	if !ok {
		return
	}
	f.storeDefaults(obj, st.Args)
	if len(st.DecoratorList) > 0 && f.b.info.Synth[st.DecoratorList[0]] != nil {
		f.decoratedDef(obj, st)
		return
	}
	if isNestedFunc(obj) {
		f.bindFunc(obj, f.makeClosure(obj, f.spec))
		return
	}
	f.markBound(obj)
}

// storeDefaults evaluates the defaults of a function where it is defined and
// keeps those that are not constants in the globals its calls read.
func (f *fbuild) storeDefaults(obj *types.Func, a *ast.Arguments) {
	positional := append(append([]*ast.Arg{}, a.PosOnlyArgs...), a.Args...)
	var defaults []ast.Expr
	var names []string
	for i, d := range a.Defaults {
		defaults, names = append(defaults, d), append(names, positional[len(positional)-len(a.Defaults)+i].Arg)
	}
	for i, d := range a.KwDefaults {
		if d != nil {
			defaults, names = append(defaults, d), append(names, a.KwOnlyArgs[i].Arg)
		}
	}
	for i, d := range defaults {
		if _, isConst := f.constExpr(d); isConst {
			continue
		}
		if p := paramNamed(obj, names[i]); p != nil {
			v := f.expr(d)
			f.store(f.b.defaultGlobal(obj, p, TypeOf(v)), v)
		}
	}
}

func paramNamed(f *types.Func, name string) *types.Var {
	sig := f.Signature()
	for i := range sig.Params().Len() {
		if p := sig.Params().At(i); p.Name() == name {
			return p
		}
	}
	return nil
}

func (f *fbuild) importStmt(st *ast.Import) {
	for _, a := range st.Names {
		res, _ := f.b.info.Import(a)
		if !f.runImport(res, a.Name) {
			return
		}
		obj := f.b.info.Defs[a]
		if obj == nil {
			obj = f.b.info.Implicits[a]
		}
		if obj != nil {
			f.markBound(obj)
		}
	}
}

func (f *fbuild) importFrom(st *ast.ImportFrom) {
	name := ""
	if st.Module != nil {
		name = *st.Module
	}
	res, _ := f.b.info.Import(st)
	if !f.runImport(res, name) {
		return
	}
	for _, a := range st.Names {
		if obj := f.b.info.Defs[a]; obj != nil {
			f.markBound(obj)
		}
	}
}

// runImport runs the bodies of the modules an import brings in, parents
// first, or raises the error of an import that fails.
func (f *fbuild) runImport(res types.Import, name string) bool {
	if res.Kind != types.ImportOK {
		class, msg := "ModuleNotFoundError", "No module named '"+name+"'"
		switch {
		case res.Kind == types.ImportRelative:
			class, msg = "ImportError", "attempted relative import with no known parent package"
		case res.Kind == types.ImportSyntax && res.Err != nil:
			class, msg = "SyntaxError", res.Err.Error()
		}
		f.raise(class, msg)
		return false
	}
	for _, m := range f.b.chain(res.Module) {
		if mod := f.b.mods[m]; mod != nil && mod.Init != nil && mod != f.fn.Module {
			f.callFn(mod.Init, nil)
		}
	}
	return true
}

// chain lists a module and its parent packages, outermost first.
func (b *builder) chain(m *types.Module) []*types.Module {
	var out []*types.Module
	name := m.Name()
	for i := 0; i <= len(name); i++ {
		if i < len(name) && name[i] != '.' {
			continue
		}
		prefix := name[:i]
		for k := range b.mods {
			if k.Name() == prefix {
				out = append(out, k)
			}
		}
	}
	return out
}
