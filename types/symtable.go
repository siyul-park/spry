package types

import (
	"fmt"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/scanner"
	"github.com/siyul-park/spry/token"
)

// symtab is the symbol table of one module, built the way CPython's
// Python/symtable.c does: a visit that records the definitions and uses of
// every block and the errors found on the way, then an analysis that decides
// the storage of each name. The first error in that order is the one CPython
// raises, so the table keeps no other.
type symtab struct {
	fset    *token.FileSet
	cur     *block
	top     *block
	private string
	future  int
	err     *scanner.Error
	blocks  map[ast.Node]*block
}

// block is one scope of the symbol table: a module, class, function body
// (including lambdas and comprehensions), type-parameter list, annotation or
// lazily evaluated type expression.
type block struct {
	kind        blockKind
	comp        compKind
	name        string
	node        ast.Node
	scope       *Scope
	parent      *block
	children    []*block
	syms        map[string]*symbol
	order       []string
	binds       []binding
	directives  map[string]ast.Node
	info        string
	annot       *block
	nested      bool
	generator   bool
	coroutine   bool
	canSeeClass bool
	iterTarget  bool
	iterExpr    int
	classClosed bool
}

type symbol struct {
	flags symFlags
	store storage
}

type binding struct {
	node  ast.Node
	kind  bindKind
	name  string
	owner ast.Node
}

type blockKind uint8

type compKind uint8

type symFlags uint16

type storage uint8

type bindKind uint8

type nameSet map[string]struct{}

type funcDef struct {
	node       ast.Stmt
	name       string
	args       *ast.Arguments
	body       []ast.Stmt
	decorators []ast.Expr
	returns    ast.Expr
	tparams    []ast.TypeParam
	async      bool
}

// typeParamParts is the name, bound, default and kind (TypeVar, ParamSpec or
// TypeVarTuple) of a type parameter node.
type typeParamParts struct {
	name, kind string
	bound, def ast.Expr
}

const (
	moduleBlock blockKind = iota
	classBlock
	functionBlock
	typeParamsBlock
	annotationBlock
	typeAliasBlock
	typeVarBlock
)

const (
	noComp compKind = iota
	listComp
	setComp
	dictComp
	genexpComp
)

const (
	defGlobal symFlags = 1 << iota
	defLocal
	defParam
	defNonlocal
	symUse
	defImport
	defAnnot
	defCompIter
	defTypeParam
	defFreeClass

	defBound = defLocal | defParam | defImport
)

const (
	storeNone storage = iota
	storeLocal
	storeGlobalExplicit
	storeGlobalImplicit
	storeFree
	storeCell
)

const (
	bkAssign bindKind = iota
	bkDef
	bkClass
	bkImport
	bkParam
	bkTypeParam
	bkTypeAlias
	bkAnnotated
)

const (
	infoAnnotation = "an annotation"
	infoAlias      = "a type alias"
	infoGeneric    = "the definition of a generic"
)

func newSymtab(fset *token.FileSet) *symtab {
	return &symtab{fset: fset, blocks: map[ast.Node]*block{}}
}

func (st *symtab) stmts(list []ast.Stmt) {
	for _, s := range list {
		st.visit(s)
	}
}

func (st *symtab) exprs(list []ast.Expr) {
	for _, e := range list {
		st.visit(e)
	}
}

func (st *symtab) optional(e ast.Expr) {
	if e != nil {
		st.visit(e)
	}
}

func (st *symtab) visit(n ast.Node) {
	if st.err != nil {
		return
	}
	switch n := n.(type) {
	case *ast.FunctionDef:
		st.function(funcDef{n, n.Name, n.Args, n.Body, n.DecoratorList, n.Returns, n.TypeParams, false})
	case *ast.AsyncFunctionDef:
		st.function(funcDef{n, n.Name, n.Args, n.Body, n.DecoratorList, n.Returns, n.TypeParams, true})
	case *ast.ClassDef:
		st.class(n)
	case *ast.Lambda:
		st.lambda(n)
	case *ast.ListComp:
		st.comprehension(n, listComp, n.Generators, n.Elt)
	case *ast.SetComp:
		st.comprehension(n, setComp, n.Generators, n.Elt)
	case *ast.DictComp:
		st.comprehension(n, dictComp, n.Generators, n.Key, n.Value)
	case *ast.GeneratorExp:
		st.comprehension(n, genexpComp, n.Generators, n.Elt)
	case *ast.TypeAlias:
		st.typeAlias(n)
	case *ast.AnnAssign:
		st.annAssign(n)
	case *ast.Global:
		st.declare(n, n.Names, defGlobal, "global")
	case *ast.Nonlocal:
		st.declare(n, n.Names, defNonlocal, "nonlocal")
	case *ast.Import:
		st.importStmt(n)
	case *ast.ImportFrom:
		st.importFrom(n)
	case *ast.ExceptHandler:
		st.exceptHandler(n)
	case *ast.Name:
		st.nameExpr(n)
	case *ast.Attribute:
		st.attribute(n)
	case *ast.NamedExpr:
		st.namedExpr(n)
	case *ast.Yield:
		st.yield(n, n.Value)
	case *ast.YieldFrom:
		st.yield(n, n.Value)
	case *ast.Await:
		st.await(n)
	case *ast.Keyword:
		st.keyword(n)
	case *ast.MatchAs:
		st.pattern(n, n.Name)
		st.optionalPattern(n.Pattern)
	case *ast.MatchStar:
		st.pattern(n, n.Name)
	case *ast.MatchMapping:
		st.exprs(n.Keys)
		st.patterns(n.Patterns)
		st.pattern(n, n.Rest)
	case *ast.MatchClass:
		st.matchClass(n)
	default:
		children(n, st.visit)
	}
}

func (st *symtab) patterns(list []ast.Pattern) {
	for _, p := range list {
		st.visit(p)
	}
}

func (st *symtab) optionalPattern(p ast.Pattern) {
	if p != nil {
		st.visit(p)
	}
}

func (st *symtab) matchClass(n *ast.MatchClass) {
	st.visit(n.Cls)
	st.patterns(n.Patterns)
	for i, p := range n.KwdPatterns {
		if n.KwdAttrs[i] == "__debug__" {
			st.fail(p, "cannot assign to __debug__")
		}
		st.visit(p)
	}
}

func (st *symtab) keyword(k *ast.Keyword) {
	if k.Arg != nil && *k.Arg == "__debug__" {
		st.fail(k, "cannot assign to __debug__")
		return
	}
	st.visit(k.Value)
}

func (st *symtab) attribute(a *ast.Attribute) {
	if a.Attr == "__debug__" {
		switch a.Ctx {
		case ast.CtxStore:
			st.fail(a, "cannot assign to __debug__")
		case ast.CtxDel:
			st.fail(a, "cannot delete __debug__")
		}
	}
	st.visit(a.Value)
}

func (st *symtab) function(d funcDef) {
	st.addDef(d.name, defLocal, d.node)
	st.bindName(d.name, bkDef, d.node, d.node)
	st.defaults(d.args)
	st.exprs(d.decorators)
	generic := len(d.tparams) > 0
	if generic {
		st.enterTypeParams(d.name, d.tparams)
	}
	st.annotations(d)
	fb := st.enter(functionBlock, d.name, d.node)
	fb.coroutine = d.async
	st.arguments(d.args)
	st.stmts(d.body)
	st.exit()
	if generic {
		st.exit()
	}
}

func (st *symtab) defaults(a *ast.Arguments) {
	st.exprs(a.Defaults)
	for _, e := range a.KwDefaults {
		st.optional(e)
	}
}

func (st *symtab) annotations(d funcDef) {
	var list []ast.Expr
	for _, group := range [][]*ast.Arg{d.args.PosOnlyArgs, d.args.Args, {d.args.VarArg}, d.args.KwOnlyArgs, {d.args.KwArg}} {
		for _, arg := range group {
			if arg != nil && arg.Annotation != nil {
				list = append(list, arg.Annotation)
			}
		}
	}
	if d.returns != nil {
		list = append(list, d.returns)
	}
	if len(list) == 0 {
		return
	}
	parent := st.cur
	b := st.enter(annotationBlock, "__annotate__", d.args)
	b.info = infoAnnotation
	b.canSeeClass = parent.kind == classBlock || parent.canSeeClass
	st.exprs(list)
	st.exit()
}

func (st *symtab) annotation(e ast.Expr) {
	parent := st.cur
	b := parent.annot
	if b == nil {
		b = st.enter(annotationBlock, "__annotate__", nil)
		b.info = infoAnnotation
		b.canSeeClass = parent.kind == classBlock
		parent.annot = b
	} else {
		st.cur = b
	}
	st.visit(e)
	st.exit()
}

func (st *symtab) class(c *ast.ClassDef) {
	st.addDef(c.Name, defLocal, c)
	st.bindName(c.Name, bkClass, c, c)
	st.exprs(c.DecoratorList)
	generic := len(c.TypeParams) > 0
	if generic {
		st.enterTypeParams(c.Name, c.TypeParams)
	}
	st.exprs(c.Bases)
	for _, k := range c.Keywords {
		st.visit(k)
	}
	st.enter(classBlock, c.Name, c)
	outer := st.private
	st.private = c.Name
	st.stmts(c.Body)
	st.private = outer
	st.exit()
	if generic {
		st.exit()
	}
}

func (st *symtab) lambda(l *ast.Lambda) {
	st.defaults(l.Args)
	st.enter(functionBlock, "lambda", l)
	st.arguments(l.Args)
	st.visit(l.Body)
	st.exit()
}

func (st *symtab) typeAlias(t *ast.TypeAlias) {
	if name, ok := t.Name.(*ast.Name); ok {
		st.addDef(name.ID, defLocal, name)
		st.bindName(name.ID, bkTypeAlias, name, t)
	}
	generic := len(t.TypeParams) > 0
	if generic {
		name, _ := t.Name.(*ast.Name)
		st.enterTypeParams(name.ID, t.TypeParams)
	}
	inClass := st.cur.kind == classBlock || st.cur.canSeeClass
	b := st.enter(typeAliasBlock, "alias", t.Value)
	b.info = infoAlias
	b.canSeeClass = inClass
	st.visit(t.Value)
	st.exit()
	if generic {
		st.exit()
	}
}

func (st *symtab) enterTypeParams(name string, list []ast.TypeParam) {
	parent := st.cur
	b := st.enter(typeParamsBlock, name, list[0])
	b.info = infoGeneric
	b.canSeeClass = parent.kind == classBlock
	hasDefault := false
	for _, tp := range list {
		parts := typeParamPartsOf(tp)
		pname, bound, def, kind := parts.name, parts.bound, parts.def, parts.kind
		switch {
		case def != nil:
			hasDefault = true
		case hasDefault:
			st.fail(tp, "non-default type parameter '%s' follows default type parameter", pname)
		}
		st.addDef(pname, defTypeParam|defLocal, tp)
		st.bindName(pname, bkTypeParam, tp, tp)
		st.typeParamBlock(bound, boundInfo(bound))
		st.typeParamBlock(def, "a "+kind+" default")
	}
}

func (st *symtab) typeParamBlock(e ast.Expr, info string) {
	if e == nil {
		return
	}
	inClass := st.cur.canSeeClass
	b := st.enter(typeVarBlock, "typevar", e)
	b.info = info
	b.canSeeClass = inClass
	st.visit(e)
	st.exit()
}

func (st *symtab) annAssign(s *ast.AnnAssign) {
	name, ok := s.Target.(*ast.Name)
	if !ok {
		st.visit(s.Target)
		st.annotation(s.Annotation)
		st.optional(s.Value)
		return
	}
	if cur := st.curFlags(name.ID); cur&(defGlobal|defNonlocal) != 0 && st.cur.kind != moduleBlock && s.Simple != 0 {
		kind := "nonlocal"
		if cur&defGlobal != 0 {
			kind = "global"
		}
		st.fail(s, "annotated name '%s' can't be %s", name.ID, kind)
		return
	}
	if s.Simple != 0 {
		st.addDef(name.ID, defAnnot|defLocal, name)
	}
	if s.Value != nil {
		st.addDef(name.ID, defLocal, name)
	}
	st.bindName(name.ID, bkAnnotated, name, s)
	st.annotation(s.Annotation)
	st.optional(s.Value)
}

func (st *symtab) exceptHandler(h *ast.ExceptHandler) {
	st.optional(h.Type)
	if h.Name != nil {
		st.addDef(*h.Name, defLocal, h)
		st.bindName(*h.Name, bkAssign, h, nil)
	}
	st.stmts(h.Body)
}

func (st *symtab) yield(e ast.Expr, value ast.Expr) {
	if st.raiseIfScoped("yield expression", e) {
		return
	}
	st.optional(value)
	st.cur.generator = true
	if st.cur.comp == noComp {
		return
	}
	switch st.cur.comp {
	case listComp:
		st.fail(e, "'yield' inside list comprehension")
	case setComp:
		st.fail(e, "'yield' inside set comprehension")
	case dictComp:
		st.fail(e, "'yield' inside dict comprehension")
	default:
		st.fail(e, "'yield' inside generator expression")
	}
}

func (st *symtab) await(e *ast.Await) {
	if st.raiseIfScoped("await expression", e) {
		return
	}
	st.visit(e.Value)
	st.cur.coroutine = true
}

func (st *symtab) namedExpr(e *ast.NamedExpr) {
	if st.raiseIfScoped("named expression", e) {
		return
	}
	if st.cur.iterExpr > 0 {
		st.fail(e, "assignment expression cannot be used in a comprehension iterable expression")
		return
	}
	target, _ := e.Target.(*ast.Name)
	if st.cur.comp != noComp && target != nil {
		st.extendNamedExprScope(target)
	}
	st.visit(e.Value)
	st.visit(e.Target)
}

func (st *symtab) comprehension(e ast.Expr, kind compKind, gens []*ast.Comprehension, elts ...ast.Expr) {
	first := gens[0]
	st.cur.iterExpr++
	st.visit(first.Iter)
	st.cur.iterExpr--
	if st.err != nil {
		return
	}
	b := st.enter(functionBlock, comprehensionName(kind), e)
	b.comp = kind
	b.coroutine = first.IsAsync != 0
	st.bindTarget(first.Target)
	for _, cond := range first.Ifs {
		st.visit(cond)
	}
	for _, g := range gens[1:] {
		st.bindTarget(g.Target)
		st.cur.iterExpr++
		st.visit(g.Iter)
		st.cur.iterExpr--
		for _, cond := range g.Ifs {
			st.visit(cond)
		}
		if g.IsAsync != 0 {
			st.cur.coroutine = true
		}
	}
	for _, elt := range elts {
		st.visit(elt)
	}
	b.generator = kind == genexpComp
	async := b.coroutine && kind != genexpComp
	st.exit()
	if !async || st.err != nil {
		return
	}
	switch p := st.cur; {
	case p.comp != noComp:
		p.coroutine = true
	case p.isFunctionLike() && p.coroutine:
	default:
		st.fail(e, "asynchronous comprehension outside of an asynchronous function")
	}
}
func (st *symtab) bindTarget(e ast.Expr) {
	st.cur.iterTarget = true
	st.visit(e)
	st.cur.iterTarget = false
}

func (st *symtab) enter(kind blockKind, name string, n ast.Node) *block {
	b := &block{kind: kind, name: name, node: n, parent: st.cur, syms: map[string]*symbol{}}
	if st.cur != nil {
		st.cur.children = append(st.cur.children, b)
		b.nested = st.cur.nested || st.cur.isFunctionLike()
	}
	if n != nil {
		st.blocks[n] = b
	}
	st.cur = b
	return b
}

func (st *symtab) extendNamedExprScope(target *ast.Name) {
	name := mangle(st.private, target.ID)
	for b := st.cur; b != nil; b = b.parent {
		switch {
		case b.comp != noComp:
			if f := b.flagsOf(name); f&defCompIter != 0 && f&defLocal != 0 {
				st.fail(target, "assignment expression cannot rebind comprehension iteration variable '%s'", target.ID)
				return
			}
		case b.kind == annotationBlock:
		case b.kind == functionBlock:
			if b.flagsOf(name)&defGlobal != 0 {
				st.addDef(target.ID, defGlobal, target)
			} else {
				st.addDef(target.ID, defNonlocal, target)
			}
			st.cur.record(name, target)
			st.addDefIn(b, target.ID, defLocal, target)
			return
		case b.kind == moduleBlock:
			st.addDef(target.ID, defGlobal, target)
			st.cur.record(name, target)
			st.addDefIn(b, target.ID, defGlobal, target)
			return
		default:
			st.failScope(b, target)
			return
		}
	}
}

func (st *symtab) declare(s ast.Stmt, names []string, flag symFlags, word string) {
	for _, name := range names {
		cur := st.curFlags(name)
		if cur&(defParam|defLocal|symUse|defAnnot) != 0 {
			var why string
			switch {
			case cur&defParam != 0:
				st.fail(s, "name '%s' is parameter and %s", name, word)
				return
			case cur&symUse != 0:
				why = "used prior to"
			case cur&defAnnot != 0:
				st.fail(s, "annotated name '%s' can't be %s", name, word)
				return
			default:
				why = "assigned to before"
			}
			st.fail(s, "name '%s' is %s %s declaration", name, why, word)
			return
		}
		st.addDef(name, flag, s)
		st.cur.record(mangle(st.private, name), s)
	}
}

func (st *symtab) analyze() {
	if st.err != nil {
		return
	}
	st.analyzeBlock(st.top, nil, nameSet{}, nil)
}
func (st *symtab) analyzeBlock(b *block, bound, global nameSet, classEntry *block) nameSet {
	local, newFree := nameSet{}, nameSet{}
	newGlobal, newBound := nameSet{}, nameSet{}
	if b.kind == classBlock {
		union(newGlobal, global)
		union(newBound, bound)
	}
	for _, name := range b.order {
		st.analyzeName(b, name, bound, local, newFree, global, classEntry)
	}
	if b.kind == classBlock {
		newBound["__class__"] = struct{}{}
	} else {
		if b.isFunctionLike() {
			union(newBound, local)
		}
		union(newBound, bound)
		union(newGlobal, global)
	}
	for _, c := range b.children {
		entry := classEntry
		if b.kind == classBlock {
			entry = b
		}
		union(newFree, st.analyzeBlock(c, copySet(newBound), copySet(newGlobal), entry))
	}
	switch {
	case b.isFunctionLike():
		analyzeCells(b, newFree)
	case b.kind == classBlock:
		if _, ok := newFree["__class__"]; ok {
			delete(newFree, "__class__")
			b.classClosed = true
		}
	}
	updateSymbols(b, bound, newFree)
	return newFree
}
func copySet(s nameSet) nameSet {
	out := make(nameSet, len(s))
	union(out, s)
	return out
}

func (st *symtab) analyzeName(b *block, name string, bound, local, free, global nameSet, classEntry *block) {
	s := b.syms[name]
	flags := s.flags
	free2 := func(n string) {
		s.store = storeFree
		free[n] = struct{}{}
	}
	switch {
	case flags&defGlobal != 0:
		if flags&defNonlocal != 0 {
			st.failDirective(b, name, "name '%s' is nonlocal and global", name)
			return
		}
		s.store = storeGlobalExplicit
		global[name] = struct{}{}
		delete(bound, name)
	case flags&defNonlocal != 0:
		switch {
		case bound == nil:
			st.failDirective(b, name, "nonlocal declaration not allowed at module level")
		case !has(bound, name):
			st.failDirective(b, name, "no binding for nonlocal '%s' found", name)
		default:
			free2(name)
		}
	case flags&defBound != 0:
		s.store = storeLocal
		local[name] = struct{}{}
		delete(global, name)
	case classEntry != nil && b.canSeeClass && classEntry.flagsOf(name)&defBound != 0:
		s.store = storeGlobalImplicit
	case bound != nil && has(bound, name):
		free2(name)
	default:
		s.store = storeGlobalImplicit
	}
}

func (st *symtab) curFlags(name string) symFlags {
	return st.cur.flagsOf(mangle(st.private, name))
}

func (st *symtab) pattern(n ast.Node, name *string) {
	if name == nil {
		return
	}
	st.addDef(*name, defLocal, n)
	st.bindName(*name, bkAssign, n, nil)
}

func (st *symtab) nameExpr(n *ast.Name) {
	if n.Ctx == ast.CtxLoad {
		st.addDef(n.ID, symUse, n)
		if st.cur.isFunctionLike() && n.ID == "super" {
			st.addDef("__class__", symUse, n)
		}
		return
	}
	if n.ID == "__debug__" && n.Ctx == ast.CtxDel {
		st.fail(n, "cannot delete __debug__")
		return
	}
	st.addDef(n.ID, defLocal, n)
	st.bindName(n.ID, bkAssign, n, nil)
}

func (st *symtab) arguments(a *ast.Arguments) {
	for _, group := range [][]*ast.Arg{a.PosOnlyArgs, a.Args, a.KwOnlyArgs, {a.VarArg}, {a.KwArg}} {
		for _, arg := range group {
			if arg != nil {
				st.addDef(arg.Arg, defParam, arg)
				st.bindName(arg.Arg, bkParam, arg, nil)
			}
		}
	}
}

func (st *symtab) importStmt(s *ast.Import) {
	for _, a := range s.Names {
		name := a.Name
		if a.AsName != nil {
			name = *a.AsName
		} else if i := indexDot(name); i >= 0 {
			name = name[:i]
		}
		st.addDef(name, defImport, a)
		st.bindName(name, bkImport, a, s)
	}
}

func (st *symtab) importFrom(s *ast.ImportFrom) {
	for _, a := range s.Names {
		if a.Name == "*" {
			if st.cur.kind != moduleBlock {
				st.fail(a, "import * only allowed at module level")
				return
			}
			continue
		}
		name := a.Name
		if a.AsName != nil {
			name = *a.AsName
		}
		st.addDef(name, defImport, a)
		st.bindName(name, bkImport, a, s)
	}
	if s.Module != nil && *s.Module == "__future__" && (s.Level == nil || *s.Level == 0) {
		if st.fset.Position(s.Pos()).Line > st.future {
			st.fail(s, "from __future__ imports must occur at the beginning of the file")
		}
	}
}

func (st *symtab) addDef(name string, flag symFlags, n ast.Node) {
	st.addDefIn(st.cur, name, flag, n)
}

func (st *symtab) addDefIn(b *block, name string, flag symFlags, n ast.Node) {
	if st.err != nil {
		return
	}
	mangled := mangle(st.private, name)
	if mangled == "__debug__" && flag&defBound != 0 {
		st.fail(n, "cannot assign to __debug__")
		return
	}
	s := b.entry(mangled)
	switch {
	case flag&defParam != 0 && s.flags&defParam != 0:
		st.fail(n, "duplicate argument '%s' in function definition", name)
		return
	case flag&defTypeParam != 0 && s.flags&defTypeParam != 0:
		st.fail(n, "duplicate type parameter '%s'", name)
		return
	}
	s.flags |= flag
	if b.iterTarget {
		if s.flags&(defGlobal|defNonlocal) != 0 {
			st.fail(n, "comprehension inner loop cannot rebind assignment expression target '%s'", name)
			return
		}
		s.flags |= defCompIter
	}
}

func (st *symtab) bindName(name string, kind bindKind, n ast.Node, owner ast.Node) {
	st.cur.bind(mangle(st.private, name), kind, n, owner)
}

func (st *symtab) failScope(b *block, target *ast.Name) {
	switch b.kind {
	case classBlock:
		st.fail(target, "assignment expression within a comprehension cannot be used in a class body")
	case typeAliasBlock:
		st.fail(target, "assignment expression within a comprehension cannot be used in a type alias")
	case typeVarBlock:
		st.fail(target, "assignment expression within a comprehension cannot be used in a TypeVar bound")
	default:
		st.fail(target, "assignment expression within a comprehension cannot be used within the definition of a generic")
	}
}

func (st *symtab) failDirective(b *block, name, format string, args ...any) {
	n := b.directives[name]
	if n == nil {
		n = b.node
	}
	st.fail(n, format, args...)
}

func (st *symtab) exit() {
	st.cur = st.cur.parent
}

func (st *symtab) raiseIfScoped(what string, e ast.Expr) bool {
	switch st.cur.kind {
	case annotationBlock, typeAliasBlock, typeVarBlock, typeParamsBlock:
		st.fail(e, "%s cannot be used within %s", what, st.cur.info)
		return true
	}
	return false
}

func (st *symtab) fail(n ast.Node, format string, args ...any) {
	if st.err != nil {
		return
	}
	st.err = syntaxErrorAt(st.fset, n, fmt.Sprintf(format, args...))
}

func syntaxErrorAt(fset *token.FileSet, n ast.Node, msg string) *scanner.Error {
	pos, end := fset.Position(n.Pos()), fset.Position(n.End())
	end.Column++
	end.Offset++
	return &scanner.Error{Pos: pos, End: end, Msg: msg, Err: scanner.ErrSyntax}
}

func (st *symtab) scanFuture(body []ast.Stmt) {
	for i, s := range body {
		if i == 0 && isDocstring(s) {
			continue
		}
		imp, ok := s.(*ast.ImportFrom)
		if !ok || imp.Module == nil || *imp.Module != "__future__" || (imp.Level != nil && *imp.Level > 0) {
			return
		}
		st.future = st.fset.Position(imp.Pos()).Line
	}
}

func children(n ast.Node, f func(ast.Node)) {
	root := true
	ast.Inspect(n, func(c ast.Node) bool {
		switch {
		case c == nil:
			return false
		case root:
			root = false
			return true
		}
		f(c)
		return false
	})
}

func indexDot(s string) int {
	for i := range len(s) {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}

func union(dst, src nameSet) {
	for k := range src {
		dst[k] = struct{}{}
	}
}

func comprehensionName(kind compKind) string {
	switch kind {
	case listComp:
		return "listcomp"
	case setComp:
		return "setcomp"
	case dictComp:
		return "dictcomp"
	}
	return "genexpr"
}

func analyzeCells(b *block, free nameSet) {
	for _, name := range b.order {
		if s := b.syms[name]; s.store == storeLocal && has(free, name) {
			s.store = storeCell
			delete(free, name)
		}
	}
}

func updateSymbols(b *block, bound, free nameSet) {
	for name := range free {
		s := b.syms[name]
		switch {
		case s != nil:
			if b.kind == classBlock && s.flags&(defBound|defGlobal) != 0 {
				s.flags |= defFreeClass
			}
		case bound == nil || has(bound, name):
			b.entry(name).store = storeFree
		}
	}
}

func (b *block) storage(name string) storage {
	if s := b.syms[name]; s != nil {
		return s.store
	}
	return storeNone
}

func (b *block) entry(name string) *symbol {
	s := b.syms[name]
	if s == nil {
		s = &symbol{}
		b.syms[name] = s
		b.order = append(b.order, name)
	}
	return s
}

func (b *block) record(name string, n ast.Node) {
	if b.directives == nil {
		b.directives = map[string]ast.Node{}
	}
	if _, ok := b.directives[name]; !ok {
		b.directives[name] = n
	}
}
func (b *block) bind(name string, kind bindKind, n ast.Node, owner ast.Node) {
	b.binds = append(b.binds, binding{node: n, kind: kind, name: name, owner: owner})
}
func (b *block) flagsOf(name string) symFlags {
	if s := b.syms[name]; s != nil {
		return s.flags
	}
	return 0
}
func (b *block) isFunctionLike() bool {
	return b.kind != moduleBlock && b.kind != classBlock
}

func has(s nameSet, name string) bool {
	_, ok := s[name]
	return ok
}

func mangle(private, name string) string {
	if private == "" || len(name) < 3 || name[:2] != "__" || name[len(name)-2:] == "__" {
		return name
	}
	for i := range len(private) {
		if private[i] != '_' {
			return "_" + private[i:] + name
		}
	}
	return name
}

func typeParamPartsOf(tp ast.TypeParam) typeParamParts {
	switch tp := tp.(type) {
	case *ast.TypeVar:
		return typeParamParts{tp.Name, "TypeVar", tp.Bound, tp.DefaultValue}
	case *ast.ParamSpec:
		return typeParamParts{tp.Name, "ParamSpec", nil, tp.DefaultValue}
	case *ast.TypeVarTuple:
		return typeParamParts{tp.Name, "TypeVarTuple", nil, tp.DefaultValue}
	}
	return typeParamParts{}
}

func boundInfo(bound ast.Expr) string {
	if _, ok := bound.(*ast.Tuple); ok {
		return "a TypeVar constraint"
	}
	return "a TypeVar bound"
}
