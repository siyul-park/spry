package types

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/scanner"
	"github.com/siyul-park/spry/token"
)

// checker runs the checks of CPython's code generator that need no types:
// jumps and yields outside their blocks, repeated keywords, starred targets
// and match patterns. It runs after the symbol table, as in CPython, because
// the table decides whether a function is a generator.
type checker struct {
	fset   *token.FileSet
	blocks map[ast.Node]*block
	err    *scanner.Error
}

// flow is the lexical context of a node: the code unit that CPython would
// compile it into and how a jump leaves it. It is copied, never shared.
type flow struct {
	unit      unitKind
	generator bool
	jump      jumpState
	star      bool
}

type unitKind uint8

type jumpState uint8

type patterns struct {
	names []string
}

const (
	unitModule unitKind = iota
	unitClass
	unitFunction
	unitAsync
	unitLambda
	unitComprehension
)

const (
	noLoop jumpState = iota
	inLoop
	behindStar
)

const exceptStarJump = "'break', 'continue' and 'return' cannot appear in an except* block"

// analyzeSyntax reports the first compile-stage SyntaxError of a module body
// in the order CPython raises them: future statements (whose feature names
// the parser already validates), the symbol table's
// visit, its analysis and then the code generator. The returned table is
// complete only when the error is nil.
func analyzeSyntax(fset *token.FileSet, root ast.Node, body []ast.Stmt) (*symtab, *scanner.Error) {
	st := newSymtab(fset)
	st.scanFuture(body)
	if st.err != nil {
		return st, st.err
	}
	st.top = st.enter(moduleBlock, "top", root)
	st.stmts(body)
	st.analyze()
	if st.err != nil {
		return st, st.err
	}
	c := &checker{fset: fset, blocks: st.blocks}
	c.stmts(body, flow{})
	return st, c.err
}

func isDocstring(s ast.Stmt) bool {
	e, ok := s.(*ast.ExprStmt)
	if !ok {
		return false
	}
	c, ok := e.Value.(*ast.Constant)
	if !ok {
		return false
	}
	_, ok = c.Value.(ast.Str)
	return ok
}

func (c *checker) stmts(list []ast.Stmt, f flow) {
	for _, s := range list {
		c.node(s, f)
	}
}

func (c *checker) exprs(list []ast.Expr, f flow) {
	for _, e := range list {
		c.node(e, f)
	}
}

func (c *checker) optional(e ast.Expr, f flow) {
	if e != nil {
		c.node(e, f)
	}
}

// elts visits the elements of a container, which consumes starred elements.
func (c *checker) elts(list []ast.Expr, f flow) {
	for _, e := range list {
		if s, ok := e.(*ast.Starred); ok {
			c.node(s.Value, f)
			continue
		}
		c.node(e, f)
	}
}

func (c *checker) node(n ast.Node, f flow) {
	if c.err != nil {
		return
	}
	switch n := n.(type) {
	case *ast.FunctionDef:
		c.function(funcDef{n, n.Name, n.Args, n.Body, n.DecoratorList, n.Returns, n.TypeParams, false}, f)
	case *ast.AsyncFunctionDef:
		c.function(funcDef{n, n.Name, n.Args, n.Body, n.DecoratorList, n.Returns, n.TypeParams, true}, f)
	case *ast.ClassDef:
		c.class(n, f)
	case *ast.Lambda:
		c.lambda(n, f)
	case *ast.ListComp:
		c.comprehension(n.Generators, f, n.Elt)
	case *ast.SetComp:
		c.comprehension(n.Generators, f, n.Elt)
	case *ast.DictComp:
		c.comprehension(n.Generators, f, n.Key, n.Value)
	case *ast.GeneratorExp:
		c.comprehension(n.Generators, f, n.Elt)
	case *ast.Return, *ast.Break, *ast.Continue:
		c.jump(n, f)
	case *ast.For:
		c.loop(n.Iter, n.Target, n.Body, n.OrElse, f)
	case *ast.AsyncFor:
		c.requireAsync(n, "'async for'", f)
		c.loop(n.Iter, n.Target, n.Body, n.OrElse, f)
	case *ast.While:
		c.loop(n.Test, nil, n.Body, n.OrElse, f)
	case *ast.AsyncWith:
		c.requireAsync(n, "'async with'", f)
		children(n, func(ch ast.Node) { c.node(ch, f) })
	case *ast.Try:
		c.try(n.Body, n.Handlers, n.OrElse, n.FinalBody, false, f)
	case *ast.TryStar:
		c.try(n.Body, n.Handlers, n.OrElse, n.FinalBody, true, f)
	case *ast.Assign:
		c.node(n.Value, f)
		c.exprs(n.Targets, f)
	case *ast.Call:
		c.call(n, f)
	case *ast.Tuple:
		c.sequence(n, n.Elts, n.Ctx, f)
	case *ast.List:
		c.sequence(n, n.Elts, n.Ctx, f)
	case *ast.Set:
		c.elts(n.Elts, f)
	case *ast.Starred:
		c.starred(n)
	case *ast.TypeVarTuple:
		if n.DefaultValue != nil {
			c.elts([]ast.Expr{n.DefaultValue}, f)
		}
	case *ast.Yield:
		c.yield(n, n.Value, false, f)
	case *ast.YieldFrom:
		c.yield(n, n.Value, true, f)
	case *ast.Await:
		c.await(n, f)
	case *ast.Match:
		c.match(n, f)
	default:
		children(n, func(ch ast.Node) { c.node(ch, f) })
	}
}

func (c *checker) function(d funcDef, f flow) {
	c.exprs(d.decorators, f)
	c.arguments(d.args, f)
	c.optional(d.returns, f)
	for _, tp := range d.tparams {
		c.node(tp, f)
	}
	unit := unitFunction
	if d.async {
		unit = unitAsync
	}
	c.stmts(d.body, flow{unit: unit, generator: c.blocks[d.node].generator})
}

func (c *checker) arguments(a *ast.Arguments, f flow) {
	c.exprs(a.Defaults, f)
	for _, e := range a.KwDefaults {
		c.optional(e, f)
	}
	for _, group := range [][]*ast.Arg{a.PosOnlyArgs, a.Args, {a.VarArg}, a.KwOnlyArgs, {a.KwArg}} {
		for _, arg := range group {
			if arg != nil && arg.Annotation != nil {
				c.elts([]ast.Expr{arg.Annotation}, f)
			}
		}
	}
}

func (c *checker) class(n *ast.ClassDef, f flow) {
	c.exprs(n.DecoratorList, f)
	c.elts(n.Bases, f)
	c.keywords(n.Keywords, f)
	for _, tp := range n.TypeParams {
		c.node(tp, f)
	}
	c.stmts(n.Body, flow{unit: unitClass})
}

func (c *checker) lambda(n *ast.Lambda, f flow) {
	c.arguments(n.Args, f)
	c.node(n.Body, flow{unit: unitLambda, generator: c.blocks[n].generator})
}

func (c *checker) comprehension(gens []*ast.Comprehension, f flow, elts ...ast.Expr) {
	c.node(gens[0].Iter, f)
	inner := flow{unit: unitComprehension}
	for i, g := range gens {
		c.node(g.Target, inner)
		if i > 0 {
			c.node(g.Iter, inner)
		}
		c.exprs(g.Ifs, inner)
	}
	for _, e := range elts {
		c.node(e, inner)
	}
}

func (c *checker) jump(n ast.Node, f flow) {
	switch n := n.(type) {
	case *ast.Return:
		switch {
		case f.unit == unitModule || f.unit == unitClass:
			c.fail(n, "'return' outside function")
			return
		case n.Value != nil && f.unit == unitAsync && f.generator:
			c.fail(n, "'return' with value in async generator")
			return
		case f.star && n.Value != nil:
			c.optional(n.Value, f)
			c.fail(n.Value, exceptStarJump)
			return
		case f.star:
			c.fail(n, exceptStarJump)
			return
		}
		c.optional(n.Value, f)
	case *ast.Break:
		c.leave(n, "'break' outside loop", f)
	case *ast.Continue:
		c.leave(n, "'continue' not properly in loop", f)
	}
}

func (c *checker) loop(head ast.Expr, target ast.Expr, body, orElse []ast.Stmt, f flow) {
	c.node(head, f)
	if target != nil {
		c.node(target, f)
	}
	inner := f
	inner.jump = inLoop
	c.stmts(body, inner)
	c.stmts(orElse, f)
}

func (c *checker) try(body []ast.Stmt, handlers []*ast.ExceptHandler, orElse, final []ast.Stmt, star bool, f flow) {
	c.stmts(body, f)
	hf := f
	if star {
		hf.star = true
		hf.jump = behindStar
	}
	for i, h := range handlers {
		if h.Type == nil && i < len(handlers)-1 {
			c.fail(h, "default 'except:' must be last")
			return
		}
		c.optional(h.Type, f)
		c.stmts(h.Body, hf)
	}
	c.stmts(orElse, f)
	c.stmts(final, f)
}

func (c *checker) call(n *ast.Call, f flow) {
	c.node(n.Func, f)
	c.elts(n.Args, f)
	c.keywords(n.Keywords, f)
}

func (c *checker) keywords(list []*ast.Keyword, f flow) {
	seen := map[string]bool{}
	for _, k := range list {
		if k.Arg == nil {
			continue
		}
		if seen[*k.Arg] {
			c.fail(k, "keyword argument repeated: %s", *k.Arg)
			return
		}
		seen[*k.Arg] = true
	}
	for _, k := range list {
		c.node(k.Value, f)
	}
}

func (c *checker) sequence(n ast.Expr, elts []ast.Expr, ctx ast.ExprContext, f flow) {
	if ctx != ast.CtxStore {
		c.elts(elts, f)
		return
	}
	star, stars := -1, 0
	for i, e := range elts {
		if _, ok := e.(*ast.Starred); ok {
			if stars == 0 {
				star = i
			}
			stars++
		}
	}
	switch {
	case stars > 1:
		c.fail(n, "multiple starred expressions in assignment")
		return
	case star >= 1<<8 || (star >= 0 && len(elts)-star-1 >= math.MaxInt32>>8):
		c.fail(n, "too many expressions in star-unpacking assignment")
		return
	}
	c.elts(elts, f)
}

func (c *checker) yield(n ast.Expr, value ast.Expr, from bool, f flow) {
	word := "'yield'"
	if from {
		word = "'yield from'"
	}
	switch {
	case f.unit == unitModule || f.unit == unitClass:
		c.fail(n, "%s outside function", word)
		return
	case from && f.unit == unitAsync:
		c.fail(n, "'yield from' inside async function")
		return
	}
	c.optional(value, f)
}

func (c *checker) await(n *ast.Await, f flow) {
	switch f.unit {
	case unitModule, unitClass:
		c.fail(n, "'await' outside function")
		return
	case unitFunction, unitLambda:
		c.fail(n, "'await' outside async function")
		return
	}
	c.node(n.Value, f)
}

func (c *checker) match(m *ast.Match, f flow) {
	c.node(m.Subject, f)
	for i, mc := range m.Cases {
		var pc patterns
		c.pattern(mc.Pattern, mc.Guard != nil || i == len(m.Cases)-1, &pc, f)
		c.optional(mc.Guard, f)
		c.stmts(mc.Body, f)
	}
}

func (c *checker) pattern(p ast.Pattern, irrefutable bool, pc *patterns, f flow) {
	if c.err != nil {
		return
	}
	switch p := p.(type) {
	case *ast.MatchValue:
		c.node(p.Value, f)
	case *ast.MatchSequence:
		c.sequencePattern(p, pc, f)
	case *ast.MatchMapping:
		c.mappingPattern(p, pc, f)
	case *ast.MatchClass:
		c.classPattern(p, pc, f)
	case *ast.MatchStar:
		pc.store(c, p, p.Name)
	case *ast.MatchAs:
		c.asPattern(p, irrefutable, pc, f)
	case *ast.MatchOr:
		c.orPattern(p, irrefutable, pc, f)
	}
}

func (c *checker) sequencePattern(p *ast.MatchSequence, pc *patterns, f flow) {
	stars := 0
	for _, sub := range p.Patterns {
		if _, ok := sub.(*ast.MatchStar); ok {
			stars++
		}
	}
	if stars > 1 {
		c.fail(p, "multiple starred names in sequence pattern")
		return
	}
	for _, sub := range p.Patterns {
		c.pattern(sub, true, pc, f)
	}
}

func (c *checker) mappingPattern(p *ast.MatchMapping, pc *patterns, f flow) {
	seen := map[string]bool{}
	for _, k := range p.Keys {
		if id, repr, ok := literalKey(k); ok {
			if seen[id] {
				c.fail(p, "mapping pattern checks duplicate key (%s)", repr)
				return
			}
			seen[id] = true
		}
		c.node(k, f)
	}
	for _, sub := range p.Patterns {
		c.pattern(sub, true, pc, f)
	}
	pc.store(c, p, p.Rest)
}

func (c *checker) classPattern(p *ast.MatchClass, pc *patterns, f flow) {
	c.node(p.Cls, f)
	seen := map[string]bool{}
	for i, name := range p.KwdAttrs {
		if seen[name] {
			c.fail(p.KwdPatterns[i], "attribute name repeated in class pattern: %s", name)
			return
		}
		seen[name] = true
	}
	for _, sub := range p.Patterns {
		c.pattern(sub, true, pc, f)
	}
	for _, sub := range p.KwdPatterns {
		c.pattern(sub, true, pc, f)
	}
}

func (c *checker) asPattern(p *ast.MatchAs, irrefutable bool, pc *patterns, f flow) {
	if p.Pattern == nil {
		switch {
		case irrefutable:
		case p.Name != nil:
			c.fail(p, "name capture '%s' makes remaining patterns unreachable", *p.Name)
			return
		default:
			c.fail(p, "wildcard makes remaining patterns unreachable")
			return
		}
	} else {
		c.pattern(p.Pattern, true, pc, f)
	}
	pc.store(c, p, p.Name)
}

func (c *checker) orPattern(p *ast.MatchOr, irrefutable bool, pc *patterns, f flow) {
	var first []string
	for i, alt := range p.Patterns {
		var sub patterns
		c.pattern(alt, irrefutable && i == len(p.Patterns)-1, &sub, f)
		if c.err != nil {
			return
		}
		if i == 0 {
			first = sub.names
		} else if !sameNames(first, sub.names) {
			c.fail(p, "alternative patterns bind different names")
			return
		}
	}
	for _, name := range first {
		pc.store(c, p, &name)
	}
}

func (c *checker) leave(n ast.Node, msg string, f flow) {
	switch f.jump {
	case noLoop:
		c.fail(n, "%s", msg)
	case behindStar:
		c.fail(n, exceptStarJump)
	}
}

func (c *checker) requireAsync(n ast.Stmt, what string, f flow) {
	if f.unit != unitAsync {
		c.fail(n, "%s outside async function", what)
	}
}

func (c *checker) starred(n *ast.Starred) {
	if n.Ctx == ast.CtxStore {
		c.fail(n, "starred assignment target must be in a list or tuple")
		return
	}
	c.fail(n, "can't use starred expression here")
}

func (pc *patterns) store(c *checker, n ast.Node, name *string) {
	if name == nil {
		return
	}
	for _, s := range pc.names {
		if s == *name {
			c.fail(n, "multiple assignments to name '%s' in pattern", *name)
			return
		}
	}
	pc.names = append(pc.names, *name)
}

func (c *checker) fail(n ast.Node, format string, args ...any) {
	if c.err == nil {
		c.err = syntaxErrorAt(c.fset, n, fmt.Sprintf(format, args...))
	}
}

func sameNames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, s := range a {
		set[s] = true
	}
	for _, s := range b {
		if !set[s] {
			return false
		}
	}
	return true
}

// literalKey returns an identity under which Python compares two mapping
// pattern keys equal (1, 1.0 and True are one key) and the repr of the key.
func literalKey(e ast.Expr) (id, repr string, ok bool) {
	neg := false
	if u, isUnary := e.(*ast.UnaryOp); isUnary && u.Op == ast.UnaryUSub {
		neg, e = true, u.Operand
	}
	k, isConst := e.(*ast.Constant)
	if !isConst {
		return "", "", false
	}
	switch v := k.Value.(type) {
	case ast.Int:
		n := new(big.Int).Set(v.N)
		if neg {
			n.Neg(n)
		}
		return "n" + n.String(), n.String(), true
	case ast.Float:
		f := float64(v)
		if neg {
			f = -f
		}
		if f == math.Trunc(f) && !math.IsInf(f, 0) {
			n, _ := big.NewFloat(f).Int(nil)
			return "n" + n.String(), floatRepr(f), true
		}
		return "f" + floatRepr(f), floatRepr(f), true
	case ast.Bool:
		if neg {
			return "", "", false
		}
		if v {
			return "n1", "True", true
		}
		return "n0", "False", true
	case ast.Str, ast.Bytes, ast.None:
		if neg {
			return "", "", false
		}
		return fmt.Sprintf("%T%v", v, v), reprValue(v), true
	}
	return "", "", false
}

func floatRepr(f float64) string {
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".en") {
		s += ".0"
	}
	return s
}
