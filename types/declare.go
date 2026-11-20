package types

import (
	"slices"
	"strings"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/token"
)

// declarer turns the symbol tables of a set of modules into scopes of
// objects and resolves their types, as go/types' resolver and objDecl do. It
// works in phases over all modules at once, because imports may be cyclic:
// every object exists before any type is evaluated, and evaluation pulls the
// declarations it needs on demand.
type declarer struct {
	fset    *token.FileSet
	info    *Info
	errs    *ErrorList
	prog    *program
	srcs    []*source
	decls   map[Object]*decl
	classes map[*Scope]*Named
	states  map[*Named]resolveState
	hidden  map[*Builtin]*Named
	owner   map[*Scope]*source
	pending []Object
	pstates map[*TypeParam]resolveState
	vdone   map[*Var]bool
	aliased map[*Var]*TypeName
}

// decl is what a lazily resolved object remembers of its declaration.
type decl struct {
	nodes []ast.Node
	block *block
	src   *source
	scope *Scope
	fn    *Func
	ann   ast.Expr
	value ast.Expr
}

type group struct {
	scope *Scope
	name  string
	binds []bound
	block *block
	obj   Object
}

type bound struct {
	binding
	block *block
}

type resolveState uint8

const (
	unresolved resolveState = iota
	resolving
	resolved
)

func (p *program) declare() {
	d := &declarer{
		fset: p.fset, info: p.info, errs: &p.errs, prog: p,
		decls: map[Object]*decl{}, classes: map[*Scope]*Named{}, states: map[*Named]resolveState{},
		hidden: map[*Builtin]*Named{}, owner: map[*Scope]*source{},
		pstates: map[*TypeParam]resolveState{}, vdone: map[*Var]bool{}, aliased: map[*Var]*TypeName{},
	}
	p.decl = d
	d.srcs = p.order[p.declared:]
	p.declared = len(p.order)
	for _, s := range d.srcs {
		d.collect(s)
	}
	for _, s := range d.srcs {
		d.bindImports(s)
	}
	d.resolveAll()
	p.errs.Sort()
}

func (d *declarer) collect(s *source) {
	top := s.st.top
	top.scope = s.mod.scope
	d.info.Scopes[s.file] = top.scope
	d.owner[top.scope] = s
	d.buildScopes(top, s)
	var groups []*group
	index := map[*Scope]map[string]*group{}
	var walk func(b *block)
	walk = func(b *block) {
		for _, bd := range b.binds {
			sc := targetScope(b, bd.name)
			g := index[sc][bd.name]
			if g == nil {
				g = &group{scope: sc, name: bd.name, block: b}
				if index[sc] == nil {
					index[sc] = map[string]*group{}
				}
				index[sc][bd.name] = g
				groups = append(groups, g)
			}
			g.binds = append(g.binds, bound{bd, b})
		}
		for _, c := range b.children {
			walk(c)
		}
	}
	walk(top)
	d.moduleVars(s)
	for _, pass := range []bool{true, false} {
		for _, g := range groups {
			if (g.binds[0].kind == bkTypeParam) == pass {
				d.declareGroup(s, g)
			}
		}
	}
	d.implicitClassCells(top)
}

func (d *declarer) buildScopes(b *block, s *source) {
	for _, c := range b.children {
		c.scope = newScope(scopeKindOf(c), b.scope)
		c.scope.syms = map[string]storage{}
		for name, sym := range c.syms {
			c.scope.syms[name] = sym.store
		}
		d.owner[c.scope] = s
		if c.node != nil {
			d.info.Scopes[c.node] = c.scope
		}
		d.buildScopes(c, s)
	}
	b.scope.syms = map[string]storage{}
	for name, sym := range b.syms {
		b.scope.syms[name] = sym.store
	}
}

func scopeKindOf(b *block) ScopeKind {
	switch b.kind {
	case classBlock:
		return ClassScope
	case functionBlock:
		if b.comp != noComp {
			return ComprehensionScope
		}
		return FunctionScope
	case typeParamsBlock:
		return TypeParamScope
	}
	return AnnotationScope
}

func (d *declarer) moduleVars(s *source) {
	if s.mod.name == "builtins" {
		return
	}
	str := builtin("str")
	for _, name := range []string{"__name__", "__file__"} {
		if s.mod.scope.Lookup(name) == nil {
			v := NewVar(token.NoPos, name, str, GlobalVar)
			v.module = s.mod
			s.mod.scope.insert(v)
		}
	}
}

func (d *declarer) implicitClassCells(b *block) {
	if b.kind == classBlock && b.classClosed {
		if c, ok := b.node.(*ast.ClassDef); ok {
			v := NewVar(c.Pos(), "__class__", nil, LocalVar)
			v.cell = true
			d.info.Implicits[c] = v
		}
	}
	for _, c := range b.children {
		d.implicitClassCells(c)
	}
}

func (d *declarer) declareGroup(s *source, g *group) {
	obj := d.create(s, g)
	g.obj = obj
	if obj == nil {
		return
	}
	if obj.Parent() == nil || g.scope.objs[g.name] == nil {
		g.scope.insert(obj)
	} else {
		g.scope.bindAlias(g.name, obj)
	}
	for i, b := range g.binds {
		switch {
		case i == 0 || b.kind != bkAssign:
			d.info.Defs[b.node] = obj
		default:
			if e, ok := b.node.(ast.Expr); ok {
				d.info.Uses[e] = obj
			} else {
				d.info.Defs[b.node] = obj
			}
		}
	}
	if len(g.binds) > 0 && g.binds[0].kind == bkImport {
		d.implicitParent(g, obj)
	}
}

func (d *declarer) implicitParent(g *group, obj Object) {
	a, ok := g.binds[0].node.(*ast.Alias)
	if !ok {
		return
	}
	if _, isImport := g.binds[0].owner.(*ast.Import); isImport && a.AsName == nil && strings.Contains(a.Name, ".") {
		d.info.Implicits[a] = obj
	}
}

func (d *declarer) create(s *source, g *group) Object {
	first := g.binds[0]
	pos := first.node.Pos()
	switch first.kind {
	case bkTypeParam:
		return d.newTypeParamObject(s, g, first.node.(ast.TypeParam))
	case bkTypeAlias:
		return d.newAliasObject(s, g, first)
	case bkClass:
		if len(g.binds) == 1 {
			return d.newClassObject(s, g, first)
		}
	case bkDef:
		if merged(g.binds) {
			return d.newFuncObject(s, g)
		}
	case bkImport:
		if obj := d.newImportObject(s, g); obj != nil {
			return obj
		}
	}
	if form := formOf(s.mod.name, g.name); form != formNone {
		return d.newFormObject(s, g, form)
	}
	v := NewVar(pos, g.name, Typ[Any], d.varKind(g))
	v.module = s.mod
	v.cell = g.scope.storage(g.name) == storeCell
	d.decls[v] = &decl{nodes: bindNodes(g.binds), block: g.block, src: s, scope: g.scope}
	d.noteAnnotation(v, g)
	return v
}

// merged reports whether the definitions of one name are one function:
// overloads, or a property with its setter and deleter.
func merged(binds []bound) bool {
	if len(binds) == 1 {
		return true
	}
	for i, b := range binds {
		def, ok := b.node.(ast.Stmt)
		if !ok || b.kind != bkDef {
			return false
		}
		decos := decoratorsOf(def)
		if i < len(binds)-1 && !hasDecorator(decos, "overload") && !hasDecorator(decos, "") {
			nextDef, ok := binds[i+1].node.(ast.Stmt)
			if !ok || binds[i+1].kind != bkDef || !hasAccessor(decoratorsOf(nextDef)) {
				return false
			}
		}
	}
	return true
}

func (d *declarer) newFuncObject(s *source, g *group) Object {
	f := &Func{object: object{pos: g.binds[0].node.Pos(), module: s.mod, name: g.name}, fullName: qualPrefix(g.block) + g.name}
	for _, b := range g.binds {
		if def, ok := b.node.(ast.Stmt); ok {
			f.defs = append(f.defs, def)
		}
	}
	if fb := d.blockOf(g.binds[0].node); fb != nil {
		f.scope = fb.scope
	}
	d.decls[f] = &decl{nodes: bindNodes(g.binds), block: g.block, src: s, scope: g.scope, fn: f}
	d.pending = append(d.pending, f)
	if form := formOf(s.mod.name, g.name); form != formNone {
		bi := newBuiltin(g.name, Typ[Any], form)
		bi.pos, bi.module = f.pos, s.mod
		d.decls[bi] = d.decls[f]
		d.pending = append(d.pending, bi)
		return bi
	}
	return f
}

func (d *declarer) newFormObject(s *source, g *group, form formKind) Object {
	b := newBuiltin(g.name, Typ[Any], form)
	b.pos, b.module = g.binds[0].node.Pos(), s.mod
	d.decls[b] = &decl{nodes: bindNodes(g.binds), block: g.block, src: s, scope: g.scope}
	d.noteAnnotation(b, g)
	return b
}

func (d *declarer) noteAnnotation(v Object, g *group) {
	dc := d.decls[v]
	for _, b := range g.binds {
		switch owner := b.owner.(type) {
		case *ast.AnnAssign:
			dc.ann, dc.value = owner.Annotation, owner.Value
		case *ast.Assign:
			dc.value = owner.Value
		}
		if b.kind == bkAnnotated {
			break
		}
	}
	if dc.ann == nil && len(g.binds) == 1 {
		if n, ok := g.binds[0].node.(*ast.Name); ok {
			dc.value = d.assignedValue(n, g.block)
		}
	}
	d.pending = append(d.pending, v)
}

// assignedValue finds the right-hand side of a plain assignment to name.
func (d *declarer) assignedValue(n *ast.Name, b *block) ast.Expr {
	var out ast.Expr
	var scan func(list []ast.Stmt)
	scan = func(list []ast.Stmt) {
		for _, s := range list {
			if a, ok := s.(*ast.Assign); ok && len(a.Targets) == 1 && a.Targets[0] == ast.Expr(n) {
				out = a.Value
			}
			for _, body := range nestedBodies(s) {
				scan(body)
			}
		}
	}
	switch node := b.node.(type) {
	case *ast.Module:
		scan(node.Body)
	case *ast.ClassDef:
		scan(node.Body)
	case *ast.FunctionDef:
		scan(node.Body)
	case *ast.AsyncFunctionDef:
		scan(node.Body)
	}
	return out
}

func (d *declarer) varKind(g *group) VarKind {
	switch g.scope.Kind() {
	case ModuleScope, UniverseScope:
		return GlobalVar
	case ClassScope:
		for _, b := range g.binds {
			if a, ok := b.owner.(*ast.AnnAssign); ok {
				if isQualifier(a.Annotation, "ClassVar") {
					return ClassVar
				}
				return FieldVar
			}
		}
		return ClassVar
	}
	if g.binds[0].kind == bkParam {
		return ParamVar
	}
	return LocalVar
}

// isQualifier reports whether annotation is spelled as the typing qualifier
// name, bare or subscripted, possibly through a module.
func isQualifier(e ast.Expr, name string) bool {
	if sub, ok := e.(*ast.Subscript); ok {
		e = sub.Value
	}
	switch e := e.(type) {
	case *ast.Name:
		return e.ID == name
	case *ast.Attribute:
		return e.Attr == name
	}
	return false
}

func (d *declarer) newTypeParamObject(s *source, g *group, tp ast.TypeParam) Object {
	kind := TypeVar
	switch typeParamPartsOf(tp).kind {
	case "ParamSpec":
		kind = ParamSpec
	case "TypeVarTuple":
		kind = TypeVarTuple
	}
	p := newTypeParam(s.mod, g.name, kind)
	p.inferred = true
	p.obj.pos = tp.Pos()
	d.decls[p.obj] = &decl{nodes: []ast.Node{tp}, block: g.block, src: s, scope: g.scope}
	d.pending = append(d.pending, p.obj)
	return p.obj
}

func (d *declarer) newAliasObject(s *source, g *group, b bound) Object {
	stmt := b.owner.(*ast.TypeAlias)
	obj := newTypeName(b.node.Pos(), s.mod, g.name, nil)
	obj.qualname = qualPrefix(g.block) + g.name
	var tparams []*TypeParam
	for _, tp := range stmt.TypeParams {
		if o, ok := d.info.Defs[tp].(*TypeName); ok {
			tparams = append(tparams, o.typ.(*TypeParam))
		}
	}
	scope := d.info.Scopes[stmt.Value]
	newAlias(obj, tparams, func() Type { return d.typ(stmt.Value, scope) })
	d.decls[obj] = &decl{nodes: []ast.Node{stmt}, block: g.block, src: s, scope: scope}
	d.pending = append(d.pending, obj)
	return obj
}

func (d *declarer) newClassObject(s *source, g *group, b bound) Object {
	cd := b.node.(*ast.ClassDef)
	obj := newTypeName(cd.Pos(), s.mod, g.name, nil)
	obj.qualname = qualPrefix(g.block) + g.name
	var tparams []*TypeParam
	for _, tp := range cd.TypeParams {
		if o, ok := d.info.Defs[tp].(*TypeName); ok {
			tparams = append(tparams, o.typ.(*TypeParam))
		}
	}
	cblock := d.blockOf(cd)
	n := newClass(obj, tparams, OrdinaryClass, cblock.scope)
	d.classes[cblock.scope] = n
	d.decls[obj] = &decl{nodes: []ast.Node{cd}, block: cblock, src: s, scope: g.scope}
	d.pending = append(d.pending, obj)
	if f := formOf(s.mod.name, g.name); f != formNone {
		bi := newBuiltin(g.name, NewMeta(n), f)
		bi.pos, bi.module = cd.Pos(), s.mod
		d.hidden[bi] = n
		return bi
	}
	return obj
}

func (d *declarer) blockOf(n ast.Node) *block {
	for _, s := range d.srcs {
		if b := s.st.blocks[n]; b != nil {
			return b
		}
	}
	return nil
}

func (d *declarer) newImportObject(s *source, g *group) Object {
	var target *Module
	for _, b := range g.binds {
		a, ok := b.node.(*ast.Alias)
		if !ok {
			return nil
		}
		imp, ok := b.owner.(*ast.Import)
		if !ok {
			return nil
		}
		m := d.importedModule(imp, a)
		if target != nil && m != target {
			return nil
		}
		target = m
	}
	obj := newModuleName(g.binds[0].node.Pos(), g.name, target)
	obj.module = s.mod
	return obj
}

// importedModule is the module that an alias of an import statement binds:
// the top package of a dotted name, or the module itself under as.
func (d *declarer) importedModule(imp *ast.Import, a *ast.Alias) *Module {
	res := d.info.imports[a]
	if res.kind != ImportOK || len(res.chain) == 0 {
		return nil
	}
	if a.AsName != nil {
		return res.module
	}
	return res.chain[0]
}

func (d *declarer) bindImports(s *source) {
	for _, g := range d.groupsOf(s) {
		if g.binds[0].kind != bkImport || g.obj != nil && isModuleName(g.obj) {
			continue
		}
		d.bindFrom(s, g)
	}
	d.bindStars(s)
}

func isModuleName(o Object) bool {
	_, ok := o.(*ModuleName)
	return ok
}

// groupsOf lists the import groups of s by rescanning its binding records.
func (d *declarer) groupsOf(s *source) []*group {
	var out []*group
	var walk func(b *block)
	seen := map[*Scope]map[string]*group{}
	walk = func(b *block) {
		for _, bd := range b.binds {
			if bd.kind != bkImport {
				continue
			}
			sc := targetScope(b, bd.name)
			g := seen[sc][bd.name]
			if g == nil {
				g = &group{scope: sc, name: bd.name, block: b}
				if seen[sc] == nil {
					seen[sc] = map[string]*group{}
				}
				seen[sc][bd.name] = g
				g.obj = sc.Lookup(bd.name)
				out = append(out, g)
			}
			g.binds = append(g.binds, bound{bd, b})
		}
		for _, c := range b.children {
			walk(c)
		}
	}
	walk(s.st.top)
	return out
}

func targetScope(b *block, name string) *Scope {
	switch b.storage(name) {
	case storeGlobalExplicit, storeGlobalImplicit:
		top := b
		for top.parent != nil {
			top = top.parent
		}
		return top.scope
	case storeFree:
		for p := b.parent; p != nil; p = p.parent {
			if p.kind == classBlock {
				continue
			}
			if st := p.storage(name); st == storeLocal || st == storeCell {
				return p.scope
			}
		}
		top := b
		for top.parent != nil {
			top = top.parent
		}
		return top.scope
	}
	return b.scope
}

func (d *declarer) bindFrom(s *source, g *group) {
	if len(g.binds) != 1 {
		return
	}
	a, ok := g.binds[0].node.(*ast.Alias)
	if !ok {
		return
	}
	from, ok := g.binds[0].owner.(*ast.ImportFrom)
	if !ok {
		return
	}
	var obj Object
	if res := d.info.imports[from]; res.kind == ImportOK {
		obj = res.module.scope.Lookup(a.Name)
	}
	if sub, ok := d.info.imports[a]; obj == nil && ok && sub.kind == ImportOK {
		obj = newModuleName(a.Pos(), g.name, sub.module)
	}
	if obj == nil {
		return
	}
	g.scope.bindAlias(g.name, obj)
	d.info.Defs[a] = obj
}

func (d *declarer) bindStars(s *source) {
	ast.Inspect(s.file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FunctionDef, *ast.AsyncFunctionDef, *ast.ClassDef, *ast.Lambda:
			return false
		case *ast.ImportFrom:
			if len(n.Names) == 1 && n.Names[0].Name == "*" {
				d.bindStar(s, n)
			}
		}
		return true
	})
}

func (d *declarer) bindStar(s *source, imp *ast.ImportFrom) {
	res := d.info.imports[imp]
	if res.kind != ImportOK {
		return
	}
	names, ok := d.starNames(res.module)
	if !ok {
		d.errs.Add(d.fset.Position(imp.Pos()), d.fset.Position(imp.End()), "TYP120",
			"from "+res.module.name+" import * needs a literal __all__ list or tuple")
		return
	}
	for _, name := range names {
		if obj := res.module.scope.Lookup(name); obj != nil && s.mod.scope.Lookup(name) == nil {
			s.mod.scope.bindAlias(name, obj)
		}
	}
}

// starNames lists what import * takes from m: the names of a literal
// __all__, else the public names. It reports false for a __all__ that is
// not a literal.
func (d *declarer) starNames(m *Module) ([]string, bool) {
	var file *ast.Module
	for _, s := range d.prog.mods {
		if s.mod == m {
			file = s.file
		}
	}
	if file != nil {
		if names, found, ok := literalAll(file.Body); found || !ok {
			return names, ok
		}
	}
	var out []string
	for _, name := range m.scope.Names() {
		if !strings.HasPrefix(name, "_") {
			out = append(out, name)
		}
	}
	return out, true
}

func literalAll(body []ast.Stmt) (names []string, found, ok bool) {
	ok = true
	for _, s := range body {
		var value ast.Expr
		switch s := s.(type) {
		case *ast.Assign:
			if len(s.Targets) == 1 && isName(s.Targets[0], "__all__") {
				names, value = nil, s.Value
			}
		case *ast.AugAssign:
			if isName(s.Target, "__all__") {
				value = s.Value
			}
		}
		if value == nil {
			continue
		}
		found = true
		list, good := stringElems(value)
		if !good {
			return nil, true, false
		}
		names = append(names, list...)
	}
	return names, found, ok
}

func isName(e ast.Expr, id string) bool {
	n, ok := e.(*ast.Name)
	return ok && n.ID == id
}

func stringElems(e ast.Expr) ([]string, bool) {
	var elts []ast.Expr
	switch e := e.(type) {
	case *ast.List:
		elts = e.Elts
	case *ast.Tuple:
		elts = e.Elts
	default:
		return nil, false
	}
	var out []string
	for _, el := range elts {
		c, ok := el.(*ast.Constant)
		if !ok {
			return nil, false
		}
		s, ok := c.Value.(ast.Str)
		if !ok {
			return nil, false
		}
		out = append(out, string(s))
	}
	return out, true
}

// newLambda declares the function of a lambda expression: its parameters are
// the ones the symbol table made, and its body is a return of the expression.
func (d *declarer) newLambda(src *source, l *ast.Lambda) *Func {
	fb := src.st.blocks[l]
	if fb == nil || fb.parent == nil {
		return nil
	}
	imp := &implicits{d: d, module: src.mod, fn: "<lambda>", on: !d.prog.stubs}
	params := d.paramVars(l.Args, fb, fb.parent.scope, nil, imp)
	ret := &ast.Return{Span: ast.Span{StartPos: l.Body.Pos(), EndPos: l.Body.End()}, Value: l.Body}
	def := &ast.FunctionDef{Span: l.Span, Name: "<lambda>", Args: l.Args, Body: []ast.Stmt{ret}}
	sig := newSignature(imp.list, NewTuple(params), imp.param("return"))
	f := &Func{object: object{pos: l.Pos(), module: src.mod, name: "<lambda>", typ: sig}, sig: sig, defs: []ast.Stmt{def}, scope: fb.scope, fullName: qualPrefix(fb.parent) + "<lambda>"}
	return f
}

func decoratorsOf(s ast.Stmt) []ast.Expr {
	switch s := s.(type) {
	case *ast.FunctionDef:
		return s.DecoratorList
	case *ast.AsyncFunctionDef:
		return s.DecoratorList
	}
	return nil
}

func hasDecorator(decos []ast.Expr, name string) bool {
	if name == "" {
		return false
	}
	for _, d := range decos {
		switch d := d.(type) {
		case *ast.Name:
			if d.ID == name {
				return true
			}
		case *ast.Attribute:
			if d.Attr == name {
				return true
			}
		}
	}
	return false
}

func hasAccessor(decos []ast.Expr) bool {
	for _, d := range decos {
		if a, ok := d.(*ast.Attribute); ok && slices.Contains([]string{"setter", "getter", "deleter"}, a.Attr) {
			return true
		}
	}
	return false
}

func bindNodes(bs []bound) []ast.Node {
	out := make([]ast.Node, len(bs))
	for i, b := range bs {
		out[i] = b.node
	}
	return out
}

func qualPrefix(b *block) string {
	var parts []string
	for c := b; c != nil && c.kind != moduleBlock; c = c.parent {
		switch c.kind {
		case classBlock:
			parts = append(parts, c.name+".")
		case functionBlock:
			parts = append(parts, c.name+".<locals>.")
		}
	}
	slices.Reverse(parts)
	return strings.Join(parts, "")
}
