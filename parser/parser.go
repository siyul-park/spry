package parser

import (
	"bytes"
	"fmt"
	"math/big"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/scanner"
	"github.com/siyul-park/spry/token"
)

// parser is a recursive-descent parser over a complete token slice. Rules
// that fail without consuming a decision panic with softFailure, which only
// the few backtracking points recover; the first failure that nothing
// recovers becomes the generic "invalid syntax" error at the furthest token
// the parser examined, as in CPython's first pass.
type parser struct {
	fset *token.FileSet
	file *token.File
	src  []byte
	toks []tok

	pos     int // index of the current token (CPython's mark)
	reached int // furthest token index examined (CPython's fill-1)
	depth   int // nesting of recursive rules, see maxDepth

	allErrors    bool
	typeComments bool
	barry        bool // from __future__ import barry_as_FLUFL was seen
	single       bool // single mode: the token stream gets CPython's extra NEWLINE
	callInvalid  bool // second pass with the invalid_* rules active
	memoize      bool // second pass: memo is in use
	memo         map[memoKey]memoEntry
	inTypeAlias  bool   // parsing the type parameters of a type statement
	literalErr   string // first decoding error of the literal parts of the f-string being parsed

	scanErr *scanner.Error // the first scanner error, nil if the source scans cleanly
	errIdx  int            // index of the ILLEGAL token of scanErr, -1 if none
	levels  []int          // bracket nesting of each token, CPython's token level
}

// tok is a scanned token with its end position.
type tok struct {
	kind      token.Token
	pos, end  token.Pos
	lit       string
	synthetic bool // inserted by the parser at the end of single mode input, like the end marker it stands for
}

// softFailure is the panic value of a rule that did not match.
type softFailure struct{}

// scanFailure is the panic value of reading the token where the scanner
// stopped on an error: CPython's tokenizer raises its error as soon as the
// parser asks for that token, in either pass.
type scanFailure struct{}

// syntaxError is the panic value of an error raised by an action or an
// invalid_* rule; unlike a softFailure it is never recovered by backtracking.
// kind is the category sentinel, nil for scanner.ErrSyntax.
type syntaxError struct {
	pos, end token.Position
	msg      string
	kind     error
	cursor   bool // end is already a tokenizer cursor, which CPython reports without the extra column
}

type memoKey struct {
	pos     int
	invalid bool
}

type memoEntry struct {
	node    ast.Expr
	end     int
	reached int
	failed  bool
}

// targetKind selects the assignment targets an invalid_* rule accepts.
type targetKind int

// maxDepth bounds the nesting of expressions, patterns and blocks, the
// counterpart of CPython's MAXSTACK of 6000 rule activations.
const maxDepth = 1500

// maxIntDigits is CPython's default sys.int_info.default_max_str_digits.
const maxIntDigits = 4300

const (
	starTargets targetKind = iota
	delTargets
	forTargets
)

var binaryOperators = map[token.Token]ast.Operator{
	token.ADD: ast.OpAdd, token.SUB: ast.OpSub, token.MUL: ast.OpMult, token.AT: ast.OpMatMult,
	token.QUO: ast.OpDiv, token.REM: ast.OpMod, token.FLOOR_QUO: ast.OpFloorDiv,
	token.SHL: ast.OpLShift, token.SHR: ast.OpRShift,
	token.BIT_OR: ast.OpBitOr, token.BIT_XOR: ast.OpBitXor, token.BIT_AND: ast.OpBitAnd,
}

var augmentedOperators = map[token.Token]ast.Operator{
	token.ADD_ASSIGN: ast.OpAdd, token.SUB_ASSIGN: ast.OpSub, token.MUL_ASSIGN: ast.OpMult,
	token.AT_ASSIGN: ast.OpMatMult, token.QUO_ASSIGN: ast.OpDiv, token.REM_ASSIGN: ast.OpMod,
	token.FLOOR_QUO_ASSIGN: ast.OpFloorDiv, token.SHL_ASSIGN: ast.OpLShift, token.SHR_ASSIGN: ast.OpRShift,
	token.BIT_OR_ASSIGN: ast.OpBitOr, token.BIT_XOR_ASSIGN: ast.OpBitXor, token.BIT_AND_ASSIGN: ast.OpBitAnd,
	token.POW_ASSIGN: ast.OpPow,
}

var keywordValues = map[token.Token]ast.Value{
	token.TRUE: ast.Bool(true), token.FALSE: ast.Bool(false), token.NONE: ast.None{}, token.ELLIPSIS: ast.Ellipsis{},
}

var simpleEscapes = map[byte]byte{'a': 7, 'b': 8, 'f': 12, 'n': 10, 'r': 13, 't': 9, 'v': 11}

func (p *parser) module() *ast.Module {
	body := p.statements()
	if !p.at(token.EOF) {
		p.fail()
	}
	p.checkFuture(body)
	return &ast.Module{Body: body}
}

// checkFuture validates the feature names of the __future__ imports at the
// top of a module. The placement rule (imports must come first) is a compile
// error in CPython, not a parse error.
func (p *parser) checkFuture(body []ast.Stmt) {
	for i, s := range body {
		if e, ok := s.(*ast.ExprStmt); ok && i == 0 {
			if c, ok := e.Value.(*ast.Constant); ok {
				if _, ok := c.Value.(ast.Str); ok {
					continue
				}
			}
		}
		from, ok := s.(*ast.ImportFrom)
		if !ok || from.Module == nil || *from.Module != "__future__" || *from.Level != 0 {
			return
		}
		for _, a := range from.Names {
			switch a.Name {
			case "nested_scopes", "generators", "division", "absolute_import", "with_statement",
				"print_function", "unicode_literals", "barry_as_FLUFL", "generator_stop", "annotations":
			case "braces":
				p.raise(a.Pos(), a.End(), "not a chance")
			default:
				p.raise(a.Pos(), a.End(), "future feature "+a.Name+" is not defined")
			}
		}
	}
}

func (p *parser) eval() *ast.Expression {
	body := p.expressions()
	for p.accept(token.NEWLINE) {
	}
	p.expect(token.EOF)
	return &ast.Expression{Body: body}
}

// expressions is the "expressions" rule: expression, optionally a tuple
// without parentheses.
func (p *parser) expressions() ast.Expr {
	start := p.pos
	first := p.expression()
	if !p.at(token.COMMA) {
		return first
	}
	elts := []ast.Expr{first}
	for p.accept(token.COMMA) && p.startsExpr() {
		elts = append(elts, p.expression())
	}
	return &ast.Tuple{Span: p.span(start), Elts: elts}
}

func (p *parser) interactive() *ast.Interactive {
	var body []ast.Stmt
	switch s := p.compound(); {
	case s != nil:
		p.expect(token.NEWLINE)
		body = []ast.Stmt{s}
	case p.at(token.NEWLINE):
		start := p.pos
		p.next()
		body = []ast.Stmt{&ast.Pass{Span: p.span(start)}}
	case p.at(token.EOF):
		p.fail()
	default:
		body = p.simpleStmts(nil)
	}
	if last := p.pos - 1; p.moreSource(last) {
		p.reached = last
		p.raiseHere("multiple statements found while compiling a single statement")
	}
	return &ast.Interactive{Body: body}
}

// moreSource is bad_single_statement: after the token last, whether the
// source holds anything but white space and comments.
func (p *parser) moreSource(last int) bool {
	off := len(p.src)
	if t := &p.toks[last]; !t.synthetic && t.kind != token.EOF {
		off = min(p.offset(t.end), len(p.src))
		if t.kind == token.NEWLINE && t.lit == "" {
			off = len(p.src)
		}
	}
	for i := off; i < len(p.src); i++ {
		switch c := p.src[i]; c {
		case ' ', '\t', '\n', '\f':
		case '#':
			for i < len(p.src) && p.src[i] != '\n' {
				i++
			}
		default:
			return true
		}
	}
	return false
}

func (p *parser) statements() []ast.Stmt {
	var body []ast.Stmt
	for !p.at(token.EOF) && !p.at(token.DEDENT) {
		body = p.statement(body)
	}
	return body
}

func (p *parser) statement(out []ast.Stmt) []ast.Stmt {
	p.enter()
	defer p.leave()
	if s := p.compound(); s != nil {
		return append(out, s)
	}
	return p.simpleStmts(out)
}

// compound parses a compound statement, or returns nil without consuming
// anything when the current token starts none.
func (p *parser) compound() ast.Stmt {
	switch p.kind() {
	case token.IF:
		return p.ifStmt()
	case token.WHILE:
		return p.whileStmt()
	case token.FOR:
		return p.forStmt(false)
	case token.TRY:
		return p.tryStmt()
	case token.WITH:
		return p.withStmt(false)
	case token.DEF:
		return p.funcDef(false, nil)
	case token.CLASS:
		return p.classDef(nil)
	case token.AT:
		return p.decorated()
	case token.ASYNC:
		switch p.kindAt(1) {
		case token.DEF:
			return p.funcDef(true, nil)
		case token.FOR:
			return p.forStmt(true)
		case token.WITH:
			return p.withStmt(true)
		}
		p.fail()
	case token.NAME:
		if p.atSoft("match") {
			var m ast.Stmt
			if p.speculate(func() { m = p.matchStmt() }) {
				return m
			}
		}
	}
	return nil
}

func (p *parser) ifStmt() ast.Stmt {
	p.enter()
	defer p.leave()
	start := p.pos
	p.next()
	test := p.namedExpression()
	p.colon()
	body := p.block(start)
	var orelse []ast.Stmt
	switch {
	case p.at(token.ELIF):
		orelse = []ast.Stmt{p.ifStmt()}
	case p.at(token.ELSE):
		orelse = p.elseBlock()
	}
	return &ast.If{Span: p.span(start), Test: test, Body: body, OrElse: orelse}
}

// block is a suite: an indented statement list or simple statements on the
// same line. kw is the index of the keyword that opens the statement, for the
// message of a missing indented block.
func (p *parser) block(kw int) []ast.Stmt {
	if !p.accept(token.NEWLINE) {
		return p.simpleStmts(nil)
	}
	if p.callInvalid && !p.at(token.INDENT) {
		p.raiseIndent(p.missingBlock(kw))
	}
	p.expect(token.INDENT)
	body := p.statements()
	if len(body) == 0 {
		p.fail()
	}
	p.expect(token.DEDENT)
	return body
}

func (p *parser) elseBlock() []ast.Stmt {
	kw := p.pos
	if !p.accept(token.ELSE) {
		return nil
	}
	p.forced(token.COLON)
	body := p.block(kw)
	if p.callInvalid && p.at(token.ELIF) {
		p.raiseHere("'elif' block follows an 'else' block")
	}
	return body
}

func (p *parser) whileStmt() ast.Stmt {
	start := p.pos
	p.next()
	test := p.namedExpression()
	p.colon()
	body := p.block(start)
	orelse := p.elseBlock()
	return &ast.While{Span: p.span(start), Test: test, Body: body, OrElse: orelse}
}

func (p *parser) forStmt(isAsync bool) ast.Stmt {
	start := p.pos
	if isAsync {
		p.next()
	}
	kw := p.pos
	p.expect(token.FOR)
	target := p.forTargets()
	p.expect(token.IN)
	iter := p.starExpressions()
	p.colon()
	body := p.block(kw)
	orelse := p.elseBlock()
	if isAsync {
		return &ast.AsyncFor{Span: p.span(start), Target: target, Iter: iter, Body: body, OrElse: orelse}
	}
	return &ast.For{Span: p.span(start), Target: target, Iter: iter, Body: body, OrElse: orelse}
}

func (p *parser) tryStmt() ast.Stmt {
	start := p.pos
	p.next()
	p.forced(token.COLON)
	body := p.block(start)
	var handlers []*ast.ExceptHandler
	star := false
	for p.at(token.EXCEPT) {
		if len(handlers) > 0 && (p.kindAt(1) == token.MUL) != star {
			p.mixedExcept(star)
		}
		h, isStar := p.exceptHandler()
		star = isStar
		handlers = append(handlers, h)
	}
	var orelse, final []ast.Stmt
	if len(handlers) > 0 {
		orelse = p.elseBlock()
	}
	if fin := p.pos; p.accept(token.FINALLY) {
		p.forced(token.COLON)
		final = p.block(fin)
	} else if len(handlers) == 0 {
		if p.callInvalid {
			p.raiseHere("expected 'except' or 'finally' block")
		}
		p.fail()
	}
	if star {
		return &ast.TryStar{Span: p.span(start), Body: body, Handlers: handlers, OrElse: orelse, FinalBody: final}
	}
	return &ast.Try{Span: p.span(start), Body: body, Handlers: handlers, OrElse: orelse, FinalBody: final}
}

func (p *parser) exceptHandler() (h *ast.ExceptHandler, isStar bool) {
	start := p.pos
	p.next()
	isStar = p.accept(token.MUL)
	var typ ast.Expr
	var name *string
	if p.at(token.COLON) && isStar {
		p.noExceptTypes(true)
		p.fail()
	}
	if p.at(token.NEWLINE) {
		p.noExceptTypes(isStar)
	}
	if !p.at(token.COLON) {
		first := p.pos
		typ = p.expression()
		if p.at(token.COMMA) {
			elts := []ast.Expr{typ}
			for p.accept(token.COMMA) && p.startsExpr() {
				elts = append(elts, p.expression())
			}
			typ = &ast.Tuple{Span: p.span(first), Elts: elts}
			if p.callInvalid && p.at(token.AS) {
				p.invalidExceptAs(typ.(*ast.Tuple).Elts[0])
			}
		} else if p.accept(token.AS) {
			n := p.exceptName(start, isStar)
			name = &n
		}
	}
	p.colon()
	body := p.block(start)
	return &ast.ExceptHandler{Span: p.span(start), Type: typ, Name: name, Body: body}, isStar
}

func (p *parser) withStmt(isAsync bool) ast.Stmt {
	start := p.pos
	if isAsync {
		p.next()
	}
	kw := p.pos
	p.expect(token.WITH)
	var items []*ast.WithItem
	if p.at(token.LPAREN) {
		p.speculate(func() {
			p.next()
			parsed := p.withItems(true)
			p.expect(token.RPAREN)
			if !p.at(token.COLON) && !(p.callInvalid && p.at(token.NEWLINE)) {
				p.fail()
			}
			items = parsed
		})
	}
	if items == nil {
		items = p.withItems(false)
	}
	p.colon()
	body := p.block(kw)
	if isAsync {
		return &ast.AsyncWith{Span: p.span(start), Items: items, Body: body}
	}
	return &ast.With{Span: p.span(start), Items: items, Body: body}
}

// funcDef parses a function definition; the node spans from def (or async),
// not from its decorators.
func (p *parser) funcDef(isAsync bool, decorators []ast.Expr) ast.Stmt {
	start := p.pos
	if isAsync {
		p.next()
	}
	kw := p.pos
	p.expect(token.DEF)
	name := p.ident()
	typeParams := p.typeParams()
	p.forcedInvalid(token.LPAREN)
	args := p.parameters(token.RPAREN, false)
	p.expect(token.RPAREN)
	var returns ast.Expr
	if p.accept(token.ARROW) {
		returns = p.expression()
	}
	p.forcedInvalid(token.COLON)
	body := p.block(kw)
	span := p.span(start)
	if isAsync {
		return &ast.AsyncFunctionDef{Span: span, Name: name, Args: args, Body: body, DecoratorList: decorators, Returns: returns, TypeParams: typeParams}
	}
	return &ast.FunctionDef{Span: span, Name: name, Args: args, Body: body, DecoratorList: decorators, Returns: returns, TypeParams: typeParams}
}

func (p *parser) classDef(decorators []ast.Expr) ast.Stmt {
	start := p.pos
	p.next()
	name := p.ident()
	typeParams := p.typeParams()
	var bases []ast.Expr
	var keywords []*ast.Keyword
	if open := p.pos; p.accept(token.LPAREN) && !p.accept(token.RPAREN) {
		bases, keywords, _ = p.argumentList(open, false)
		p.expect(token.RPAREN)
	}
	p.colon()
	body := p.block(start)
	return &ast.ClassDef{Span: p.span(start), Name: name, Bases: bases, Keywords: keywords, Body: body, DecoratorList: decorators, TypeParams: typeParams}
}

func (p *parser) decorated() ast.Stmt {
	var decorators []ast.Expr
	for p.accept(token.AT) {
		decorators = append(decorators, p.namedExpression())
		p.expect(token.NEWLINE)
	}
	switch p.kind() {
	case token.DEF:
		return p.funcDef(false, decorators)
	case token.CLASS:
		return p.classDef(decorators)
	case token.ASYNC:
		if p.kindAt(1) == token.DEF {
			return p.funcDef(true, decorators)
		}
	}
	p.fail()
	return nil
}

func (p *parser) matchStmt() ast.Stmt {
	start := p.pos
	p.next()
	subject := p.subjectExpr()
	p.colon()
	p.expect(token.NEWLINE)
	if p.callInvalid && !p.at(token.INDENT) {
		p.raiseIndent(p.missingBlock(start))
	}
	p.expect(token.INDENT)
	var cases []*ast.MatchCase
	for p.atSoft("case") {
		cases = append(cases, p.caseBlock())
	}
	if len(cases) == 0 {
		p.fail()
	}
	p.expect(token.DEDENT)
	return &ast.Match{Span: p.span(start), Subject: subject, Cases: cases}
}

func (p *parser) caseBlock() *ast.MatchCase {
	start := p.pos
	p.next()
	c := &ast.MatchCase{Pattern: p.patterns()}
	if p.accept(token.IF) {
		c.Guard = p.namedExpression()
	}
	p.colon()
	c.Body = p.block(start)
	return c
}

// exceptName parses the NAME after "except E as". Anything else that is an
// expression followed by a block is invalid_except_stmt's last alternative.
func (p *parser) exceptName(kw int, isStar bool) string {
	if p.callInvalid && !(p.at(token.NAME) && p.kindAt(1) == token.COLON) {
		pos := p.pos
		var target ast.Expr
		if p.speculate(func() {
			target = p.expression()
			p.expect(token.COLON)
			p.block(kw)
		}) {
			what := "except"
			if isStar {
				what = "except*"
			}
			p.raiseNode(target, "cannot use "+what+" statement with "+exprName(target))
		}
		p.pos = pos
	}
	return p.ident()
}

func (p *parser) simpleStmts(out []ast.Stmt) []ast.Stmt {
	for {
		out = append(out, p.simpleStmt())
		if !p.accept(token.SEMICOLON) || p.at(token.NEWLINE) {
			break
		}
	}
	p.expect(token.NEWLINE)
	return out
}

func (p *parser) simpleStmt() ast.Stmt {
	p.invalidStatementIf()
	switch p.kind() {
	case token.IMPORT:
		return p.importStmt()
	case token.FROM:
		return p.importFrom()
	case token.DEL:
		return p.delStmt()
	case token.GLOBAL, token.NONLOCAL:
		return p.namesStmt()
	case token.RETURN, token.RAISE, token.ASSERT, token.YIELD, token.PASS, token.BREAK, token.CONTINUE:
		return p.flowStmt()
	}
	if p.atTypeAlias() {
		return p.typeAlias()
	}
	return p.exprStmt()
}

func (p *parser) importStmt() ast.Stmt {
	start := p.pos
	p.next()
	p.invalidImport(start)
	var names []*ast.Alias
	for {
		names = append(names, p.alias(p.dottedName))
		if !p.accept(token.COMMA) {
			break
		}
	}
	return &ast.Import{Span: p.span(start), Names: names}
}

func (p *parser) importFrom() ast.Stmt {
	start := p.pos
	p.next()
	level := 0
	for {
		if p.accept(token.PERIOD) {
			level++
		} else if p.accept(token.ELLIPSIS) {
			level += 3
		} else {
			break
		}
	}
	var module *string
	if p.at(token.NAME) {
		m := p.dottedName()
		module = &m
	} else if level == 0 {
		p.fail()
	}
	p.expect(token.IMPORT)
	if p.callInvalid && p.at(token.NEWLINE) {
		p.raiseFrom(p.cur().pos, "Expected one or more names after 'import'")
	}
	var names []*ast.Alias
	switch {
	case p.at(token.MUL):
		t := p.next()
		names = []*ast.Alias{{Span: ast.Span{StartPos: t.pos, EndPos: t.end}, Name: "*"}}
	case p.accept(token.LPAREN):
		names = p.importNames()
		p.accept(token.COMMA)
		p.expect(token.RPAREN)
	default:
		names = p.importNames()
		if p.at(token.COMMA) {
			if p.callInvalid && p.kindAt(1) == token.NEWLINE {
				p.raiseHere("trailing comma not allowed without surrounding parentheses")
			}
			p.fail()
		}
	}
	if module != nil && *module == "__future__" && level == 0 {
		for _, a := range names {
			p.barry = p.barry || a.Name == "barry_as_FLUFL"
		}
	}
	return &ast.ImportFrom{Span: p.span(start), Module: module, Names: names, Level: &level}
}

// importNames parses a comma-separated list of "name [as alias]". A trailing
// comma is left for the caller.
func (p *parser) importNames() []*ast.Alias {
	names := []*ast.Alias{p.alias(p.ident)}
	for p.at(token.COMMA) && p.kindAt(1) == token.NAME {
		p.next()
		names = append(names, p.alias(p.ident))
	}
	return names
}

func (p *parser) alias(name func() string) *ast.Alias {
	start := p.pos
	a := &ast.Alias{Name: name()}
	if p.accept(token.AS) {
		p.invalidImportTarget()
		as := p.ident()
		a.AsName = &as
	}
	a.Span = p.span(start)
	return a
}

func (p *parser) delStmt() ast.Stmt {
	start := p.pos
	p.next()
	var targets []ast.Expr
	if !p.callInvalid {
		targets = p.delTargets()
	} else if !p.speculate(func() { targets = p.delTargets() }) {
		p.invalidDel()
		p.fail()
	}
	return &ast.Delete{Span: p.span(start), Targets: targets}
}

// delTargets parses the targets of a del statement up to the end of the
// statement.
func (p *parser) delTargets() []ast.Expr {
	var targets []ast.Expr
	for {
		t := p.primary()
		if !setContext(t, ast.CtxDel) {
			p.fail()
		}
		targets = append(targets, t)
		if !p.accept(token.COMMA) || p.at(token.SEMICOLON) || p.at(token.NEWLINE) {
			break
		}
	}
	if !p.at(token.SEMICOLON) && !p.at(token.NEWLINE) {
		p.fail()
	}
	return targets
}

// namesStmt parses global and nonlocal.
func (p *parser) namesStmt() ast.Stmt {
	start := p.pos
	isGlobal := p.next().kind == token.GLOBAL
	names := []string{p.ident()}
	for p.accept(token.COMMA) {
		names = append(names, p.ident())
	}
	if isGlobal {
		return &ast.Global{Span: p.span(start), Names: names}
	}
	return &ast.Nonlocal{Span: p.span(start), Names: names}
}

// flowStmt parses return, raise, assert, yield, pass, break and continue.
func (p *parser) flowStmt() ast.Stmt {
	start := p.pos
	if p.at(token.YIELD) {
		y := p.yieldExpr()
		if p.callInvalid && p.at(token.ASSIGN) {
			p.raiseNode(y, "assignment to yield expression not possible")
		}
		return &ast.ExprStmt{Span: p.span(start), Value: y}
	}
	switch p.next().kind {
	case token.RETURN:
		var v ast.Expr
		if p.startsStarExpr() {
			v = p.starExpressions()
		}
		return &ast.Return{Span: p.span(start), Value: v}
	case token.RAISE:
		r := &ast.Raise{}
		if p.startsExpr() {
			r.Exc = p.expression()
			if p.accept(token.FROM) {
				r.Cause = p.expression()
			}
		}
		r.Span = p.span(start)
		return r
	case token.ASSERT:
		a := &ast.Assert{Test: p.expression()}
		if p.accept(token.COMMA) {
			a.Msg = p.expression()
		}
		a.Span = p.span(start)
		return a
	case token.PASS:
		return &ast.Pass{Span: p.span(start)}
	case token.BREAK:
		return &ast.Break{Span: p.span(start)}
	}
	return &ast.Continue{Span: p.span(start)}
}

// atTypeAlias reports whether a type statement starts here: the soft keyword
// type followed by a name.
func (p *parser) atTypeAlias() bool {
	return p.atSoft("type") && p.kindAt(1) == token.NAME
}

// atSoft reports whether the current token is the soft keyword name.
func (p *parser) atSoft(name string) bool {
	t := p.cur()
	return t.kind == token.NAME && t.lit == name
}

func (p *parser) typeAlias() ast.Stmt {
	start := p.pos
	p.next()
	name := p.name()
	name.Ctx = ast.CtxStore
	p.inTypeAlias = true
	params := p.typeParams()
	p.inTypeAlias = false
	p.expect(token.ASSIGN)
	v := p.expression()
	return &ast.TypeAlias{Span: p.span(start), Name: name, TypeParams: params, Value: v}
}

func (p *parser) typeParams() []ast.TypeParam {
	if !p.accept(token.LBRACK) {
		return nil
	}
	if p.callInvalid && p.at(token.RBRACK) {
		if p.inTypeAlias {
			p.kindAt(1)
		}
		p.raiseFrom(p.cur().pos, "Type parameter list cannot be empty")
	}
	var params []ast.TypeParam
	for {
		params = append(params, p.typeParam())
		if !p.accept(token.COMMA) || p.at(token.RBRACK) {
			break
		}
	}
	p.expect(token.RBRACK)
	return params
}

func (p *parser) typeParam() ast.TypeParam {
	start := p.pos
	switch {
	case p.accept(token.MUL):
		name := p.ident()
		p.invalidTypeParamBound("TypeVarTuple")
		var def ast.Expr
		if p.accept(token.ASSIGN) {
			def = p.starExpression()
		}
		return &ast.TypeVarTuple{Span: p.span(start), Name: name, DefaultValue: def}
	case p.accept(token.POW):
		name := p.ident()
		p.invalidTypeParamBound("ParamSpec")
		var def ast.Expr
		if p.accept(token.ASSIGN) {
			def = p.expression()
		}
		return &ast.ParamSpec{Span: p.span(start), Name: name, DefaultValue: def}
	}
	name := p.ident()
	var bound, def ast.Expr
	if p.accept(token.COLON) {
		bound = p.expression()
	}
	if p.accept(token.ASSIGN) {
		def = p.expression()
	}
	return &ast.TypeVar{Span: p.span(start), Name: name, Bound: bound, DefaultValue: def}
}

// exprStmt parses an expression statement, an assignment, an augmented
// assignment or an annotated assignment.
func (p *parser) exprStmt() ast.Stmt {
	start := p.pos
	first := p.starExpressions()
	op, isAug := augmentedOperators[p.kind()]
	switch {
	case p.at(token.ASSIGN):
		return p.assign(start, first)
	case p.at(token.COLON):
		return p.annAssign(start, first)
	case isAug:
		if !isSingleTarget(first) {
			p.invalidAssignmentNamed(start)
			p.invalidAugAssign(first)
			p.fail()
		}
		setContext(first, ast.CtxStore)
		p.next()
		v := p.rhs()
		return &ast.AugAssign{Span: p.span(start), Target: first, Op: op, Value: v}
	}
	return &ast.ExprStmt{Span: p.span(start), Value: first}
}

func (p *parser) assign(start int, first ast.Expr) ast.Stmt {
	if !setContext(first, ast.CtxStore) {
		p.invalidAssignmentNamed(start)
		p.invalidAssignTarget(first)
		p.fail()
	}
	targets := []ast.Expr{first}
	var value ast.Expr
	for {
		p.expect(token.ASSIGN)
		value = p.rhs()
		if !p.at(token.ASSIGN) {
			break
		}
		if !setContext(value, ast.CtxStore) {
			p.invalidAssignTarget(value)
			p.fail()
		}
		targets = append(targets, value)
	}
	return &ast.Assign{Span: p.span(start), Targets: targets, Value: value}
}

func (p *parser) annAssign(start int, target ast.Expr) ast.Stmt {
	if !isSingleTarget(target) {
		p.invalidAnnAssign(start, target)
		p.fail()
	}
	setContext(target, ast.CtxStore)
	simple := 0
	if _, ok := target.(*ast.Name); ok && p.toks[start].kind == token.NAME {
		simple = 1
	}
	p.next()
	a := &ast.AnnAssign{Target: target, Annotation: p.expression(), Simple: simple}
	if p.accept(token.ASSIGN) {
		a.Value = p.rhs()
	}
	a.Span = p.span(start)
	return a
}

func (p *parser) withItems(parenthesized bool) []*ast.WithItem {
	var items []*ast.WithItem
	for {
		item := &ast.WithItem{ContextExpr: p.expression()}
		if p.accept(token.AS) {
			item.OptionalVars = p.withTarget()
			if k := p.kind(); k != token.COMMA && k != token.RPAREN && k != token.COLON && !(p.callInvalid && k == token.NEWLINE) {
				p.fail()
			}
		}
		items = append(items, item)
		if !p.accept(token.COMMA) || (parenthesized && p.at(token.RPAREN)) {
			return items
		}
	}
}

func (p *parser) subjectExpr() ast.Expr {
	start := p.pos
	first := p.starNamedExpression()
	if p.at(token.COMMA) {
		return &ast.Tuple{Elts: p.starNamedExpressions(first), Span: p.span(start)}
	}
	if _, ok := first.(*ast.Starred); ok {
		p.fail()
	}
	return first
}

// patterns is the top-level pattern of a case: an open sequence or a pattern.
func (p *parser) patterns() ast.Pattern {
	start := p.pos
	first := p.maybeStarPattern()
	if p.at(token.COMMA) {
		pats := p.patternTail(first)
		return &ast.MatchSequence{Span: p.span(start), Patterns: pats}
	}
	if _, ok := first.(*ast.MatchStar); ok {
		p.fail()
	}
	return first
}

func (p *parser) maybeStarPattern() ast.Pattern {
	if !p.at(token.MUL) {
		return p.pattern()
	}
	start := p.pos
	p.next()
	t := p.expect(token.NAME)
	var name *string
	if t.lit != "_" {
		p.pos--
		n := p.captureTarget()
		name = &n
	}
	return &ast.MatchStar{Span: p.span(start), Name: name}
}

func (p *parser) pattern() ast.Pattern {
	p.enter()
	defer p.leave()
	start := p.pos
	pat := p.orPattern()
	if !p.accept(token.AS) {
		return pat
	}
	p.invalidAsPattern()
	name := p.captureTarget()
	return &ast.MatchAs{Span: p.span(start), Pattern: pat, Name: &name}
}

func (p *parser) orPattern() ast.Pattern {
	start := p.pos
	first := p.closedPattern()
	if !p.at(token.BIT_OR) {
		return first
	}
	pats := []ast.Pattern{first}
	for p.accept(token.BIT_OR) {
		pats = append(pats, p.closedPattern())
	}
	return &ast.MatchOr{Span: p.span(start), Patterns: pats}
}

func (p *parser) closedPattern() ast.Pattern {
	start := p.pos
	switch p.kind() {
	case token.NUMBER, token.SUB:
		v := p.numberPattern()
		return &ast.MatchValue{Span: p.span(start), Value: v}
	case token.STRING, token.FSTRING_START, token.TSTRING_START:
		v := p.strings()
		return &ast.MatchValue{Span: p.span(start), Value: v}
	case token.NONE, token.TRUE, token.FALSE:
		t := p.next()
		return &ast.MatchSingleton{Span: p.span(start), Value: keywordValues[t.kind]}
	case token.NAME:
		return p.namePattern()
	case token.LPAREN:
		return p.groupPattern()
	case token.LBRACK:
		p.next()
		var pats []ast.Pattern
		if !p.at(token.RBRACK) {
			pats = p.patternTail(p.maybeStarPattern())
		}
		p.expect(token.RBRACK)
		return &ast.MatchSequence{Span: p.span(start), Patterns: pats}
	case token.LBRACE:
		return p.mappingPattern()
	}
	p.fail()
	return nil
}

// namePattern parses a capture, a wildcard, a value (dotted name) or a class pattern.
func (p *parser) namePattern() ast.Pattern {
	start := p.pos
	nameTok := p.cur()
	var cls ast.Expr = p.name()
	dotted := false
	for p.accept(token.PERIOD) {
		attr := p.ident()
		cls = &ast.Attribute{Span: p.span(start), Value: cls, Attr: attr}
		dotted = true
	}
	switch {
	case p.at(token.LPAREN):
		return p.classPattern(start, cls)
	case p.at(token.ASSIGN):
		p.fail()
	case dotted:
		return &ast.MatchValue{Span: p.span(start), Value: cls}
	case nameTok.lit == "_":
		return &ast.MatchAs{Span: p.span(start)}
	}
	name := identifier(nameTok.lit)
	return &ast.MatchAs{Span: p.span(start), Name: &name}
}

func (p *parser) classPattern(start int, cls ast.Expr) ast.Pattern {
	p.next()
	m := &ast.MatchClass{Cls: cls}
	for !p.at(token.RPAREN) {
		if p.at(token.NAME) && p.kindAt(1) == token.ASSIGN {
			m.KwdAttrs = append(m.KwdAttrs, p.ident())
			p.next()
			m.KwdPatterns = append(m.KwdPatterns, p.pattern())
		} else {
			if len(m.KwdAttrs) > 0 {
				p.invalidClassPattern()
				p.fail()
			}
			m.Patterns = append(m.Patterns, p.pattern())
		}
		if !p.accept(token.COMMA) {
			break
		}
	}
	p.expect(token.RPAREN)
	m.Span = p.span(start)
	return m
}

// groupPattern parses "(" pattern ")" or a parenthesized sequence pattern.
func (p *parser) groupPattern() ast.Pattern {
	start := p.pos
	p.next()
	if p.accept(token.RPAREN) {
		return &ast.MatchSequence{Span: p.span(start)}
	}
	first := p.maybeStarPattern()
	if p.at(token.COMMA) {
		pats := p.patternTail(first)
		p.expect(token.RPAREN)
		return &ast.MatchSequence{Span: p.span(start), Patterns: pats}
	}
	if _, ok := first.(*ast.MatchStar); ok {
		p.fail()
	}
	p.expect(token.RPAREN)
	return first
}

// patternTail parses the elements after the first of a sequence pattern,
// allowing a trailing comma.
func (p *parser) patternTail(first ast.Pattern) []ast.Pattern {
	pats := []ast.Pattern{first}
	for p.accept(token.COMMA) && p.startsPattern() {
		pats = append(pats, p.maybeStarPattern())
	}
	return pats
}

func (p *parser) mappingPattern() ast.Pattern {
	start := p.pos
	p.next()
	m := &ast.MatchMapping{}
	for !p.at(token.RBRACE) {
		if p.accept(token.POW) {
			rest := p.captureTarget()
			m.Rest = &rest
			p.accept(token.COMMA)
			break
		}
		m.Keys = append(m.Keys, p.mappingKey())
		p.expect(token.COLON)
		m.Patterns = append(m.Patterns, p.pattern())
		if !p.accept(token.COMMA) {
			break
		}
	}
	p.expect(token.RBRACE)
	m.Span = p.span(start)
	return m
}

// invalidClassPattern is invalid_class_pattern: positional patterns after
// keyword patterns.
func (p *parser) invalidClassPattern() {
	if !p.callInvalid {
		return
	}
	pos := p.pos
	var pats []ast.Pattern
	if p.speculate(func() {
		pats = append(pats, p.pattern())
		for p.accept(token.COMMA) {
			if !p.speculate(func() { pats = append(pats, p.pattern()) }) {
				break
			}
		}
	}) {
		p.raise(pats[0].Pos(), pats[len(pats)-1].End(), "positional patterns follow keyword patterns")
	}
	p.pos = pos
}

// captureTarget parses a NAME that binds: not "_" and not the start of a
// dotted name, class pattern or keyword.
func (p *parser) captureTarget() string {
	t := p.expect(token.NAME)
	if t.lit == "_" {
		p.fail()
	}
	if k := p.kind(); k == token.PERIOD || k == token.LPAREN || k == token.ASSIGN {
		p.fail()
	}
	return identifier(t.lit)
}

func (p *parser) startsPattern() bool {
	switch p.kind() {
	case token.NAME, token.NUMBER, token.SUB, token.STRING, token.FSTRING_START, token.TSTRING_START,
		token.NONE, token.TRUE, token.FALSE, token.LPAREN, token.LBRACK, token.LBRACE, token.MUL:
		return true
	}
	return false
}

// mappingKey is literal_expr or attr.
func (p *parser) mappingKey() ast.Expr {
	start := p.pos
	switch p.kind() {
	case token.NUMBER, token.SUB:
		return p.numberPattern()
	case token.STRING, token.FSTRING_START, token.TSTRING_START:
		return p.strings()
	case token.NONE, token.TRUE, token.FALSE:
		t := p.next()
		return &ast.Constant{Span: p.span(start), Value: keywordValues[t.kind]}
	}
	var e ast.Expr = p.name()
	if !p.at(token.PERIOD) {
		p.fail()
	}
	for p.accept(token.PERIOD) {
		attr := p.ident()
		e = &ast.Attribute{Span: p.span(start), Value: e, Attr: attr}
	}
	return e
}

// numberPattern is a signed number or a complex literal "real +/- imag".
func (p *parser) numberPattern() ast.Expr {
	start := p.pos
	left := p.signedNumber()
	if !p.at(token.ADD) && !p.at(token.SUB) {
		return left
	}
	if isImaginary(left) {
		p.raiseAt(&p.toks[start], "real number required in complex literal")
	}
	op := binaryOperators[p.next().kind]
	rightStart := p.pos
	right := p.number()
	if !isImaginary(right) {
		p.raiseAt(&p.toks[rightStart], "imaginary number required in complex literal")
	}
	return &ast.BinOp{Span: p.span(start), Left: left, Op: op, Right: right}
}

func (p *parser) signedNumber() ast.Expr {
	if !p.at(token.SUB) {
		return p.number()
	}
	start := p.pos
	p.next()
	v := p.number()
	return &ast.UnaryOp{Span: p.span(start), Op: ast.UnaryUSub, Operand: v}
}

func (p *parser) funcType() *ast.FunctionType {
	p.expect(token.LPAREN)
	var args []ast.Expr
	for !p.at(token.RPAREN) {
		switch {
		case p.accept(token.MUL):
			args = append(args, p.expression())
			if p.accept(token.COMMA) {
				p.expect(token.POW)
				args = append(args, p.expression())
			}
		case p.accept(token.POW):
			args = append(args, p.expression())
		default:
			args = append(args, p.expression())
			if p.accept(token.COMMA) && p.at(token.RPAREN) {
				p.fail()
			}
			continue
		}
		break
	}
	p.expect(token.RPAREN)
	p.expect(token.ARROW)
	returns := p.expression()
	for p.accept(token.NEWLINE) {
	}
	p.expect(token.EOF)
	return &ast.FunctionType{ArgTypes: args, Returns: returns}
}

// errorAt converts a failure into the error of the result.
func (p *parser) errorAt(f failure) *scanner.Error {
	kind := f.err.kind
	if kind == nil {
		kind = scanner.ErrSyntax
	}
	// scanner.Error.End follows one contract for every error: CPython's
	// end_offset is End.Column-1. The end of a node is CPython's exclusive
	// end column plus one, so the parser moves End one column past it.
	end := f.err.end
	if end.Column > 0 && !f.err.cursor {
		end.Column++
		end.Offset++
	}
	return &scanner.Error{Pos: f.err.pos, End: end, Msg: f.err.msg, Err: kind}
}

// generic is the error of pegen's _Pypegen_set_syntax_error when no rule
// raised one: the first pass's last token is the offending one, except that a
// failure at an INDENT or DEDENT is an IndentationError.
func (p *parser) generic(last int) failure {
	switch t := &p.toks[last]; t.kind {
	case token.INDENT:
		return failure{kind: failSyntax, err: p.hereError("unexpected indent", scanner.ErrIndentation)}
	case token.DEDENT:
		return failure{kind: failSyntax, err: p.hereError("unexpected unindent", scanner.ErrIndentation)}
	default:
		pos, end := p.tokRange(t)
		return failure{kind: failSyntax, err: &syntaxError{pos: pos, end: end, msg: "invalid syntax"}}
	}
}

// tokenizeRest is _PyPegen_tokenize_full_source_to_check_for_errors: after a
// parser error the rest of the source is tokenized and a scanner error found
// there replaces it. An unclosed bracket does so only when the parser had
// read past the line of the bracket; running out of input after a line
// continuation never does.
func (p *parser) tokenizeRest() *scanner.Error {
	e := p.scanErr
	switch {
	case e == nil, e.Msg == "unexpected EOF while parsing":
		return nil
	case p.fstringDepth(p.errIdx) > 0:
		return nil
	case strings.HasSuffix(e.Msg, " was never closed"):
		if p.fset.Position(p.toks[min(p.reached, len(p.toks)-1)].pos).Line <= e.Pos.Line {
			return nil
		}
	}
	return e
}

// fstringDepth is the nesting of f-strings and t-strings at token i.
func (p *parser) fstringDepth(i int) int {
	depth := 0
	for _, t := range p.toks[:i] {
		switch t.kind {
		case token.FSTRING_START, token.TSTRING_START:
			depth++
		case token.FSTRING_END, token.TSTRING_END:
			depth--
		}
	}
	return depth
}

// raiseIndent is RAISE_INDENTATION_ERROR.
func (p *parser) raiseIndent(msg string) {
	panic(p.hereError(msg, scanner.ErrIndentation))
}

// colon consumes the ':' that ends a compound statement header. In the
// second pass a header that ends at the line break is "expected ':'".
func (p *parser) colon() {
	if p.callInvalid && p.at(token.NEWLINE) {
		p.raiseHere("expected ':'")
	}
	p.expect(token.COLON)
}

// forcedInvalid is forced in the second pass and expect in the first.
func (p *parser) forcedInvalid(k token.Token) {
	if p.callInvalid {
		p.forced(k)
		return
	}
	p.expect(k)
}

// forced is _PyPegen_expect_forced_token: the token must be there.
func (p *parser) forced(k token.Token) {
	if !p.at(k) {
		p.raiseNext("expected '" + k.String() + "'")
	}
	p.next()
}

// missingBlock is the message of an IndentationError for the statement whose
// keyword is the token kw.
func (p *parser) missingBlock(kw int) string {
	t := &p.toks[kw]
	var what string
	switch {
	case t.kind == token.IF, t.kind == token.ELIF, t.kind == token.ELSE, t.kind == token.WHILE,
		t.kind == token.FOR, t.kind == token.WITH, t.kind == token.TRY, t.kind == token.FINALLY:
		what = "'" + t.kind.String() + "' statement"
	case t.kind == token.EXCEPT:
		what = "'except' statement"
		if p.toks[kw+1].kind == token.MUL {
			what = "'except*' statement"
		}
	case t.kind == token.DEF:
		what = "function definition"
	case t.kind == token.CLASS:
		what = "class definition"
	case t.kind == token.NAME:
		what = "'" + t.lit + "' statement"
	default:
		return "expected an indented block"
	}
	return fmt.Sprintf("expected an indented block after %s on line %d", what, p.fset.Position(t.pos).Line)
}

// noExceptTypes handles an except clause without a type at the line break
// (or, for except*, at the colon).
func (p *parser) noExceptTypes(isStar bool) {
	if !p.callInvalid {
		return
	}
	if isStar {
		p.raiseHere("expected one or more exception types")
	}
	p.raiseHere("expected ':'")
}

// mixedExcept is invalid_try_stmt for an except clause of the other kind
// than the previous ones; star is the kind of the previous ones.
func (p *parser) mixedExcept(star bool) {
	if !p.callInvalid {
		p.fail()
	}
	a := p.pos
	p.next()
	ok := p.speculate(func() {
		if !star {
			p.expect(token.MUL)
			p.expression()
		} else if !p.at(token.COLON) {
			p.expression()
		}
		if p.accept(token.AS) {
			p.expect(token.NAME)
		}
		p.expect(token.COLON)
	})
	if !ok {
		p.fail()
	}
	const msg = "cannot have both 'except' and 'except*' on the same 'try'"
	if star {
		p.raiseAt(&p.toks[a], msg)
	}
	p.raiseTokens(&p.toks[a], &p.toks[a+1], msg)
}

// invalidExceptAs is the first alternative of invalid_except_stmt: "except
// A, B as e:".
func (p *parser) invalidExceptAs(first ast.Expr) {
	pos := p.pos
	if p.speculate(func() {
		p.next()
		p.expect(token.NAME)
		p.expect(token.COLON)
	}) {
		p.raiseFrom(first.Pos(), "multiple exception types must be parenthesized when using 'as'")
	}
	p.pos = pos
}

// withTarget parses the target after "as" of a with item, with
// invalid_with_item in the second pass.
func (p *parser) withTarget() ast.Expr {
	if !p.callInvalid {
		return p.starTarget()
	}
	pos := p.pos
	var target ast.Expr
	if p.speculate(func() { target = p.starTarget() }) {
		if k := p.kind(); k == token.COMMA || k == token.RPAREN || k == token.COLON || k == token.NEWLINE {
			return target
		}
		p.pos = pos
	}
	var e ast.Expr
	if p.speculate(func() { e = p.expression() }) {
		if k := p.kind(); k == token.COMMA || k == token.RPAREN || k == token.COLON {
			p.raiseInvalidTarget(starTargets, e)
		}
	}
	p.pos = pos
	p.fail()
	return nil
}

// invalidDel is invalid_del_stmt after the targets of a del failed to parse.
func (p *parser) invalidDel() {
	e := p.starExpressions()
	p.raiseInvalidTarget(delTargets, e)
}

// invalidImport is invalid_import: "import a from b" and a bare "import".
func (p *parser) invalidImport(start int) {
	if !p.callInvalid {
		return
	}
	pos := p.pos
	if p.speculate(func() {
		p.dottedName()
		for p.accept(token.COMMA) {
			p.dottedName()
		}
		p.expect(token.FROM)
		p.dottedName()
	}) {
		p.raiseFrom(p.toks[start].pos, "Did you mean to use 'from ... import ...' instead?")
	}
	p.pos = pos
	if p.at(token.NEWLINE) {
		p.raiseFrom(p.cur().pos, "Expected one or more names after 'import'")
	}
}

func (p *parser) dottedName() string {
	name := p.ident()
	for p.accept(token.PERIOD) {
		name += "." + p.ident()
	}
	return name
}

// invalidImportTarget is invalid_dotted_as_name and invalid_import_from_as_name
// after "as": anything but a name that ends the alias is an expression where
// a target was expected.
func (p *parser) invalidImportTarget() {
	if !p.callInvalid {
		return
	}
	if p.at(token.NAME) {
		switch p.kindAt(1) {
		case token.COMMA, token.RPAREN, token.SEMICOLON, token.NEWLINE:
			return
		}
	}
	pos := p.pos
	var e ast.Expr
	if p.speculate(func() { e = p.expression() }) {
		p.raiseNode(e, "cannot use "+exprName(e)+" as import target")
	}
	p.pos = pos
}

// invalidAssignTarget is the "(star_targets '=')* a=star_expressions '='"
// and yield alternatives of invalid_assignment for the expression e that is
// followed by '='.
func (p *parser) invalidAssignTarget(e ast.Expr) {
	if !p.callInvalid {
		return
	}
	switch e.(type) {
	case *ast.Yield, *ast.YieldFrom:
		p.raiseNode(e, "assignment to yield expression not possible")
	}
	p.raiseInvalidTarget(starTargets, e)
}

// invalidAugAssign is the augmented assignment alternative of
// invalid_assignment; the current token is the operator.
func (p *parser) invalidAugAssign(first ast.Expr) {
	if !p.callInvalid {
		return
	}
	pos := p.pos
	if p.speculate(func() {
		p.next()
		p.rhs()
	}) {
		p.raiseNode(first, "'"+exprName(first)+"' is an illegal expression for augmented assignment")
	}
	p.pos = pos
}

// invalidAnnAssign is the annotation alternatives of invalid_assignment for
// the expression e that starts at token start and is followed by ':'.
func (p *parser) invalidAnnAssign(start int, e ast.Expr) {
	if !p.callInvalid {
		return
	}
	colon := p.pos
	annotated := p.speculate(func() {
		p.next()
		p.expression()
	})
	p.pos = colon
	_, isList := e.(*ast.List)
	_, isTuple := e.(*ast.Tuple)
	if k := p.toks[start].kind; annotated && (isList || isTuple) && (k == token.LPAREN || k == token.LBRACK) && p.matching(start) == colon-1 {
		p.raiseNode(e, "only single target (not "+exprName(e)+") can be annotated")
	}
	p.invalidAssignmentNamed(start)
	if !annotated {
		return
	}
	switch n := e.(type) {
	case *ast.List, *ast.Tuple:
		if t, ok := n.(*ast.Tuple); ok {
			p.raiseNode(t.Elts[0], "only single target (not tuple) can be annotated")
		}
	case *ast.Starred:
	default:
		p.raiseNode(e, "illegal target for annotation")
	}
}

// matching returns the index of the bracket that closes the one at token i.
func (p *parser) matching(i int) int {
	for j := i + 1; j < len(p.toks); j++ {
		if p.levels[j] < p.levels[i] {
			return j
		}
	}
	return -1
}

// invalidAssignmentNamed is the second alternative of invalid_assignment:
// its first element is a star_named_expression, which gives
// invalid_named_expression a chance at the start of the statement.
func (p *parser) invalidAssignmentNamed(start int) {
	if !p.callInvalid {
		return
	}
	pos := p.pos
	p.pos = start
	p.speculate(func() { p.starNamedExpression() })
	p.pos = pos
}

// invalidTypeParamBound is invalid_type_param: a bound on a TypeVarTuple or
// ParamSpec; the current token is the ':' if there is one.
func (p *parser) invalidTypeParamBound(kind string) {
	if !p.callInvalid || !p.at(token.COLON) {
		return
	}
	pos := p.pos
	colon := p.next()
	var e ast.Expr
	if p.speculate(func() { e = p.expression() }) {
		what := "bound"
		if _, ok := e.(*ast.Tuple); ok {
			what = "constraints"
		}
		p.raiseFrom(colon.pos, "cannot use "+what+" with "+kind)
	}
	p.pos = pos
}

// invalidAsPattern is invalid_as_pattern after "or_pattern as".
func (p *parser) invalidAsPattern() {
	if !p.callInvalid {
		return
	}
	if t := p.cur(); t.kind == token.NAME && t.lit == "_" {
		p.raiseAt(t, "cannot use '_' as a target")
	}
	if p.at(token.NAME) {
		switch p.kindAt(1) {
		case token.PERIOD, token.LPAREN, token.ASSIGN:
		default:
			return
		}
	}
	pos := p.pos
	var e ast.Expr
	if p.speculate(func() { e = p.expression() }) {
		p.raiseNode(e, "cannot use "+exprName(e)+" as pattern target")
	}
	p.pos = pos
}

// namedExpression is assignment_expression or expression.
func (p *parser) namedExpression() ast.Expr {
	if p.at(token.NAME) && p.kindAt(1) == token.DEFINE {
		start := p.pos
		target := p.name()
		target.Ctx = ast.CtxStore
		p.next()
		v := p.expression()
		return &ast.NamedExpr{Span: p.span(start), Target: target, Value: v}
	}
	start := p.pos
	e := p.expression()
	p.invalidNamedExpression(start, e)
	return e
}

// argExpression is assignment_expression or expression, which call
// arguments use instead of named_expression.
func (p *parser) argExpression() ast.Expr {
	if p.at(token.NAME) && p.kindAt(1) == token.DEFINE {
		return p.namedExpression()
	}
	return p.expression()
}

// expression is the expression rule. In the second pass its results, and its
// failures, are memoized by position as pegen's (memo) does, because the
// invalid_* rules parse the same text again and again.
func (p *parser) expression() ast.Expr {
	if !p.memoize {
		return p.expressionRule()
	}
	key := memoKey{p.pos, p.callInvalid}
	if m, ok := p.memo[key]; ok {
		p.reached = max(p.reached, m.reached)
		if m.failed {
			p.fail()
		}
		p.pos = m.end
		return m.node
	}
	m := memoEntry{}
	defer func() {
		if r := recover(); r != nil {
			if _, soft := r.(softFailure); soft {
				p.memo[key] = memoEntry{failed: true, reached: p.reached}
			}
			panic(r)
		}
	}()
	m.node = p.expressionRule()
	m.end, m.reached = p.pos, p.reached
	p.memo[key] = m
	return m.node
}

func (p *parser) expressionRule() ast.Expr {
	p.enter()
	defer p.leave()
	if p.at(token.LAMBDA) {
		return p.lambda()
	}
	start := p.pos
	p.invalidStatementIf()
	p.invalidStringTail()
	body := p.disjunction()
	p.invalidExpression(start, body)
	if !p.at(token.IF) {
		return body
	}
	p.next()
	test := p.disjunction()
	if p.callInvalid && !p.at(token.ELSE) && !p.at(token.COLON) {
		p.raiseRange(body, test, "expected 'else' after 'if' expression")
	}
	p.expect(token.ELSE)
	orelse := p.expressionAfterElse()
	return &ast.IfExp{Span: p.span(start), Test: test, Body: body, OrElse: orelse}
}

func (p *parser) lambda() ast.Expr {
	start := p.pos
	p.next()
	args := p.parameters(token.COLON, true)
	if p.callInvalid && p.at(token.COLON) {
		switch p.kindAt(1) {
		case token.FSTRING_MIDDLE:
			p.raiseTokens(&p.toks[start], p.cur(), "f-string: lambda expressions are not allowed without parentheses")
		case token.TSTRING_MIDDLE:
			p.raiseTokens(&p.toks[start], p.cur(), "t-string: lambda expressions are not allowed without parentheses")
		}
	}
	p.expect(token.COLON)
	body := p.expression()
	return &ast.Lambda{Span: p.span(start), Args: args, Body: body}
}

// parameters parses a parameter list up to, not including, the terminator
// token: ")" for def and ":" for lambda. Lambda parameters have no
// annotations.
func (p *parser) parameters(terminator token.Token, isLambda bool) *ast.Arguments {
	a := &ast.Arguments{}
	var seenSlash, seenStar, seenDefault, bareStar bool
	p.invalidParameters(terminator, isLambda)
	for !p.at(terminator) {
		start := p.pos
		switch {
		case p.at(token.QUO):
			if seenSlash || seenStar || len(a.Args) == 0 {
				p.fail()
			}
			p.next()
			a.PosOnlyArgs, a.Args = a.Args, nil
			seenSlash = true
		case p.at(token.MUL):
			if seenStar {
				p.fail()
			}
			p.invalidStarEtc(start, terminator, isLambda)
			p.next()
			seenStar = true
			if p.at(token.COMMA) {
				bareStar = true
				break
			}
			a.VarArg = p.param(start+1, isLambda, true)
		case p.at(token.POW):
			p.invalidKwds(isLambda)
			p.next()
			a.KwArg = p.param(start+1, isLambda, false)
			p.accept(token.COMMA)
			if bareStar || !p.at(terminator) {
				p.fail()
			}
			return a
		default:
			arg := p.param(start, isLambda, false)
			def := p.defaultValue()
			switch {
			case seenStar:
				a.KwOnlyArgs = append(a.KwOnlyArgs, arg)
				a.KwDefaults = append(a.KwDefaults, def)
				bareStar = false
			case def != nil:
				a.Args = append(a.Args, arg)
				a.Defaults = append(a.Defaults, def)
				seenDefault = true
			case seenDefault:
				p.fail()
			default:
				a.Args = append(a.Args, arg)
			}
		}
		if !p.accept(token.COMMA) {
			break
		}
	}
	if bareStar {
		p.fail()
	}
	return a
}

// param parses one parameter name with its annotation; start is the token
// index where the node begins.
func (p *parser) param(start int, isLambda, starAnnotation bool) *ast.Arg {
	name := p.ident()
	arg := &ast.Arg{Arg: name}
	if !isLambda && p.accept(token.COLON) {
		if starAnnotation {
			arg.Annotation = p.starExpression()
		} else {
			arg.Annotation = p.expression()
		}
	}
	arg.Span = p.span(start)
	return arg
}

func (p *parser) starExpression() ast.Expr {
	if p.at(token.MUL) {
		return p.starred(p.bitwiseOr)
	}
	return p.expression()
}

func (p *parser) bitwiseOr() ast.Expr {
	return p.binary(token.BIT_OR.Precedence())
}

// binary parses left-associative binary operators of precedence at least
// minPrec, with unary factors as operands.
func (p *parser) binary(minPrec int) ast.Expr {
	start := p.pos
	left := p.factor()
	for {
		prec := p.kind().Precedence()
		if prec == token.LowestPrec || prec < minPrec {
			return left
		}
		op := binaryOperators[p.kind()]
		p.next()
		p.invalidNotOperand(op == ast.OpAdd || op == ast.OpSub || op == ast.OpMult || op == ast.OpDiv || op == ast.OpMod || op == ast.OpFloorDiv || op == ast.OpMatMult, p.inversion)
		right := p.binary(prec + 1)
		left = &ast.BinOp{Span: p.span(start), Left: left, Op: op, Right: right}
	}
}

func (p *parser) factor() ast.Expr {
	var op ast.UnaryOperator
	switch p.kind() {
	case token.ADD:
		op = ast.UnaryUAdd
	case token.SUB:
		op = ast.UnaryUSub
	case token.TILDE:
		op = ast.UnaryInvert
	default:
		return p.power()
	}
	p.enter()
	defer p.leave()
	start := p.pos
	p.next()
	p.invalidNotOperand(true, p.factor)
	v := p.factor()
	return &ast.UnaryOp{Span: p.span(start), Op: op, Operand: v}
}

func (p *parser) power() ast.Expr {
	start := p.pos
	base := p.awaitPrimary()
	if !p.at(token.POW) {
		return base
	}
	p.next()
	p.enter()
	defer p.leave()
	exp := p.factor()
	return &ast.BinOp{Span: p.span(start), Left: base, Op: ast.OpPow, Right: exp}
}

func (p *parser) awaitPrimary() ast.Expr {
	if !p.at(token.AWAIT) {
		return p.primary()
	}
	start := p.pos
	p.next()
	v := p.primary()
	return &ast.Await{Span: p.span(start), Value: v}
}

func (p *parser) primary() ast.Expr {
	start := p.pos
	e := p.atom()
	for {
		switch p.kind() {
		case token.PERIOD:
			p.next()
			attr := p.ident()
			e = &ast.Attribute{Span: p.span(start), Value: e, Attr: attr}
		case token.LPAREN:
			e = p.call(start, e)
		case token.LBRACK:
			p.next()
			s := p.slices()
			p.expect(token.RBRACK)
			e = &ast.Subscript{Span: p.span(start), Value: e, Slice: s}
		default:
			return e
		}
	}
}

func (p *parser) atom() ast.Expr {
	t := p.cur()
	switch t.kind {
	case token.NAME:
		return p.name()
	case token.TRUE, token.FALSE, token.NONE, token.ELLIPSIS:
		p.next()
		return &ast.Constant{Span: ast.Span{StartPos: t.pos, EndPos: t.end}, Value: keywordValues[t.kind]}
	case token.NUMBER:
		return p.number()
	case token.STRING, token.FSTRING_START, token.TSTRING_START:
		return p.strings()
	case token.LPAREN, token.LBRACK, token.LBRACE:
		p.enter()
		defer p.leave()
		switch t.kind {
		case token.LPAREN:
			return p.parenthesized()
		case token.LBRACK:
			return p.list()
		}
		return p.braced()
	}
	p.fail()
	return nil
}

// strings parses adjacent string literals into one Constant, JoinedStr or TemplateStr.
func (p *parser) strings() ast.Expr {
	start := p.pos
	template := p.at(token.TSTRING_START)
	var items []ast.Expr
	for {
		switch k := p.kind(); {
		case k == token.STRING && !template:
			items = append(items, p.stringConstant())
		case k == token.FSTRING_START && !template:
			items = append(items, p.interpolated(false))
		case k == token.TSTRING_START && template:
			items = append(items, p.interpolated(true))
		case k == token.STRING || k == token.FSTRING_START || k == token.TSTRING_START:
			p.fail()
		default:
			return p.concatenate(start, items, template)
		}
	}
}

// interpolated parses an f-string or t-string up to its end token. Debug
// fields are already flattened into their text and value.
func (p *parser) interpolated(template bool) ast.Expr {
	startTok := p.next()
	raw := strings.ContainsAny(startTok.lit, "rR")
	middle, end := token.FSTRING_MIDDLE, token.FSTRING_END
	if template {
		middle, end = token.TSTRING_MIDDLE, token.TSTRING_END
	}
	var values []ast.Expr
	for !p.at(end) {
		switch p.kind() {
		case middle:
			if c := p.middle(p.next(), raw, false); c != nil {
				values = append(values, c)
			}
		case token.LBRACE:
			values = append(values, p.field(template, raw, false)...)
		default:
			p.fail()
		}
	}
	span := ast.Span{StartPos: startTok.pos, EndPos: p.next().end}
	if msg := p.literalErr; msg != "" {
		p.literalErr = ""
		p.raiseHere(msg)
	}
	if template {
		return &ast.TemplateStr{Span: span, Values: values}
	}
	return &ast.JoinedStr{Span: span, Values: values}
}

// field parses a replacement field (inSpec: inside a format specifier, where
// a t-string field is a FormattedValue) "{expr[=][!c][:spec]}" and returns its
// values: the debug text and the formatted value, or the formatted value alone.
func (p *parser) field(template, raw, inSpec bool) []ast.Expr {
	lbrace := p.next()
	value := p.fieldValue(template)
	p.invalidFieldEnd(template, token.ASSIGN, token.EXCLAMATION, token.COLON, token.RBRACE)
	var debug *tok
	if p.at(token.ASSIGN) {
		debug = p.next()
		p.invalidFieldEnd(template, token.EXCLAMATION, token.COLON, token.RBRACE)
	}
	term := p.cur()
	text := stripComments(string(p.src[p.offset(lbrace.end):p.offset(term.pos)]))
	conversion := -1
	var convName *tok
	if p.at(token.EXCLAMATION) {
		bang := p.next()
		p.invalidConversion(template)
		convName = p.expect(token.NAME)
		prefix := "f"
		if template {
			prefix = "t"
		}
		if convName.pos != bang.end {
			p.raise(bang.pos, convName.end, prefix+"-string: conversion type must come right after the exclamation mark")
		}
		if convName.lit != "s" && convName.lit != "r" && convName.lit != "a" {
			p.raiseAt(convName, prefix+"-string: invalid conversion character "+quoteRepr(convName.lit)+": expected 's', 'r', or 'a'")
		}
		conversion = int(convName.lit[0])
	}
	p.invalidFieldEnd(template, token.COLON, token.RBRACE)
	var spec ast.Expr
	if p.at(token.COLON) {
		colon := p.pos
		p.next()
		spec = p.formatSpec(colon, template, raw)
	}
	p.invalidFieldClose(template, spec != nil)
	rbrace := p.expect(token.RBRACE)
	if conversion == -1 && debug != nil && spec == nil {
		conversion = 'r'
	}
	span := ast.Span{StartPos: lbrace.pos, EndPos: rbrace.end}
	var v ast.Expr
	if template && !inSpec {
		str := text
		if debug != nil {
			str = stripDebug(text)
		}
		v = &ast.Interpolation{Span: span, Value: value, Str: str, Conversion: conversion, FormatSpec: spec}
	} else {
		v = &ast.FormattedValue{Span: span, Value: value, Conversion: conversion, FormatSpec: spec}
	}
	if debug == nil {
		return []ast.Expr{v}
	}
	dbg := &ast.Constant{Span: ast.Span{StartPos: lbrace.end, EndPos: term.pos}, Value: ast.Str(text)}
	return []ast.Expr{dbg, v}
}

// rhs is annotated_rhs: a yield expression or star expressions.
func (p *parser) rhs() ast.Expr {
	if p.at(token.YIELD) {
		return p.yieldExpr()
	}
	return p.starExpressions()
}

func (p *parser) yieldExpr() ast.Expr {
	start := p.pos
	p.expect(token.YIELD)
	if p.accept(token.FROM) {
		v := p.expression()
		return &ast.YieldFrom{Span: p.span(start), Value: v}
	}
	var v ast.Expr
	if p.startsStarExpr() {
		v = p.starExpressions()
	}
	return &ast.Yield{Span: p.span(start), Value: v}
}

func (p *parser) starExpressions() ast.Expr {
	start := p.pos
	first := p.starExpression()
	if !p.at(token.COMMA) {
		return first
	}
	elts := []ast.Expr{first}
	for p.accept(token.COMMA) && p.startsStarExpr() {
		elts = append(elts, p.starExpression())
	}
	return &ast.Tuple{Span: p.span(start), Elts: elts}
}

// formatSpec parses the format specifier after the colon at token index colon.
func (p *parser) formatSpec(colon int, template, raw bool) ast.Expr {
	middle := token.FSTRING_MIDDLE
	if template {
		middle = token.TSTRING_MIDDLE
	}
	var values []ast.Expr
	for !p.at(token.RBRACE) {
		switch p.kind() {
		case middle:
			if c := p.middle(p.next(), raw, true); c != nil {
				values = append(values, c)
			}
		case token.LBRACE:
			values = append(values, p.field(template, raw, true)...)
		default:
			p.invalidFieldClose(template, true)
			p.fail()
		}
	}
	return &ast.JoinedStr{Span: p.span(colon), Values: foldValues(values)}
}

func (p *parser) parenthesized() ast.Expr {
	start := p.pos
	p.next()
	if p.accept(token.RPAREN) {
		return &ast.Tuple{Span: p.span(start)}
	}
	if p.at(token.YIELD) {
		y := p.yieldExpr()
		p.expect(token.RPAREN)
		return y
	}
	p.invalidGroupDoubleStar()
	first := p.starNamedExpression()
	p.invalidStarredComprehension(first)
	switch {
	case p.startsComprehension(first):
		gens := p.comprehension(token.RPAREN)
		return &ast.GeneratorExp{Span: p.span(start), Elt: first, Generators: gens}
	case p.at(token.COMMA):
		elts := p.starNamedExpressions(first)
		p.expect(token.RPAREN)
		return &ast.Tuple{Span: p.span(start), Elts: elts}
	}
	if _, ok := first.(*ast.Starred); ok {
		p.invalidGroupStar(start + 1)
		p.fail()
	}
	p.expect(token.RPAREN)
	return first
}

func (p *parser) starNamedExpression() ast.Expr {
	if p.at(token.MUL) {
		return p.starred(p.bitwiseOr)
	}
	return p.namedExpression()
}

// comprehension parses the for clauses of a comprehension and its closing token.
func (p *parser) comprehension(closing token.Token) []*ast.Comprehension {
	gens := p.forIfClauses()
	p.expect(closing)
	return gens
}

// forIfClauses parses one or more "for target in iterable if cond" clauses.
func (p *parser) forIfClauses() []*ast.Comprehension {
	var gens []*ast.Comprehension
	for p.at(token.FOR) || p.at(token.ASYNC) {
		isAsync := 0
		if p.accept(token.ASYNC) {
			isAsync = 1
		}
		p.expect(token.FOR)
		target := p.comprehensionTarget()
		p.expect(token.IN)
		iter := p.disjunction()
		var ifs []ast.Expr
		for p.accept(token.IF) {
			ifs = append(ifs, p.disjunction())
		}
		gens = append(gens, &ast.Comprehension{Target: target, Iter: iter, Ifs: ifs, IsAsync: isAsync})
	}
	if len(gens) == 0 {
		p.fail()
	}
	return gens
}

// starTargets parses star_targets: a target or a tuple of targets without
// parentheses.
func (p *parser) starTargets() ast.Expr {
	start := p.pos
	first := p.starTarget()
	if !p.at(token.COMMA) {
		return first
	}
	elts := []ast.Expr{first}
	for p.accept(token.COMMA) && p.startsStarExpr() {
		elts = append(elts, p.starTarget())
	}
	return &ast.Tuple{Span: p.span(start), Elts: elts, Ctx: ast.CtxStore}
}

func (p *parser) starTarget() ast.Expr {
	if p.at(token.MUL) {
		start := p.pos
		p.next()
		if p.at(token.MUL) {
			p.fail()
		}
		v := p.starTarget()
		return &ast.Starred{Span: p.span(start), Value: v, Ctx: ast.CtxStore}
	}
	e := p.primary()
	if !setContext(e, ast.CtxStore) {
		p.fail()
	}
	return e
}

func (p *parser) disjunction() ast.Expr {
	start := p.pos
	first := p.conjunction()
	if !p.at(token.OR) {
		return first
	}
	values := []ast.Expr{first}
	for p.accept(token.OR) {
		values = append(values, p.conjunction())
	}
	return &ast.BoolOp{Span: p.span(start), Op: ast.BoolOr, Values: values}
}

func (p *parser) conjunction() ast.Expr {
	start := p.pos
	first := p.inversion()
	if !p.at(token.AND) {
		return first
	}
	values := []ast.Expr{first}
	for p.accept(token.AND) {
		values = append(values, p.inversion())
	}
	return &ast.BoolOp{Span: p.span(start), Op: ast.BoolAnd, Values: values}
}

func (p *parser) inversion() ast.Expr {
	if !p.at(token.NOT) {
		return p.comparison()
	}
	p.enter()
	defer p.leave()
	start := p.pos
	p.next()
	v := p.inversion()
	return &ast.UnaryOp{Span: p.span(start), Op: ast.UnaryNot, Operand: v}
}

func (p *parser) comparison() ast.Expr {
	start := p.pos
	left := p.bitwiseOr()
	var ops []ast.CmpOperator
	var rest []ast.Expr
	for {
		op, ok := p.compareOp()
		if !ok {
			break
		}
		ops = append(ops, op)
		rest = append(rest, p.bitwiseOr())
	}
	if len(ops) == 0 {
		return left
	}
	return &ast.Compare{Span: p.span(start), Left: left, Ops: ops, Comparators: rest}
}

// starNamedExpressions parses a comma-separated list with an optional
// trailing comma, up to but excluding the closing token.
func (p *parser) starNamedExpressions(first ast.Expr) []ast.Expr {
	elts := []ast.Expr{first}
	for p.accept(token.COMMA) && p.startsStarExpr() {
		elts = append(elts, p.starNamedExpression())
	}
	return elts
}

func (p *parser) list() ast.Expr {
	start := p.pos
	p.next()
	if p.accept(token.RBRACK) {
		return &ast.List{Span: p.span(start)}
	}
	first := p.starNamedExpression()
	p.invalidStarredComprehension(first)
	if p.startsComprehension(first) {
		gens := p.comprehension(token.RBRACK)
		return &ast.ListComp{Span: p.span(start), Elt: first, Generators: gens}
	}
	elts := p.starNamedExpressions(first)
	p.invalidComprehensionTarget(first, elts)
	p.expect(token.RBRACK)
	return &ast.List{Span: p.span(start), Elts: elts}
}

func (p *parser) braced() ast.Expr {
	start := p.pos
	p.next()
	switch {
	case p.accept(token.RBRACE):
		return &ast.Dict{Span: p.span(start)}
	case p.at(token.POW):
		p.invalidDictComprehension()
		return p.dict(start, nil, nil)
	case p.at(token.MUL) || p.at(token.NAME) && p.kindAt(1) == token.DEFINE:
		return p.set(start, p.starNamedExpression())
	}
	firstTok := p.pos
	first := p.expression()
	if !p.accept(token.COLON) {
		p.invalidNamedExpression(firstTok, first)
		return p.set(start, first)
	}
	p.invalidDictValue(p.pos - 1)
	value := p.expression()
	if p.at(token.FOR) || p.at(token.ASYNC) {
		gens := p.comprehension(token.RBRACE)
		return &ast.DictComp{Span: p.span(start), Key: first, Value: value, Generators: gens}
	}
	return p.dict(start, []ast.Expr{first}, []ast.Expr{value})
}

// dict parses the remaining items of a dict display whose first items, if
// any, are already parsed and whose next token is a comma or the closing brace.
func (p *parser) dict(start int, keys, values []ast.Expr) ast.Expr {
	for len(keys) == 0 || p.accept(token.COMMA) {
		if len(keys) > 0 && p.at(token.RBRACE) {
			break
		}
		if p.accept(token.POW) {
			keys = append(keys, nil)
			values = append(values, p.bitwiseOr())
			continue
		}
		key := p.expression()
		if p.callInvalid && !p.at(token.COLON) && len(keys) > 0 {
			p.raiseKeyWithoutColon(key)
		}
		keys = append(keys, key)
		p.expect(token.COLON)
		p.invalidDictValue(p.pos - 1)
		values = append(values, p.expression())
	}
	p.expect(token.RBRACE)
	return &ast.Dict{Span: p.span(start), Keys: keys, Values: values}
}

// set parses the rest of a set display or set comprehension after its first element.
func (p *parser) set(start int, first ast.Expr) ast.Expr {
	p.invalidStarredComprehension(first)
	if p.startsComprehension(first) {
		gens := p.comprehension(token.RBRACE)
		return &ast.SetComp{Span: p.span(start), Elt: first, Generators: gens}
	}
	elts := p.starNamedExpressions(first)
	p.invalidComprehensionTarget(first, elts)
	p.expect(token.RBRACE)
	return &ast.Set{Span: p.span(start), Elts: elts}
}

// call parses the argument list of a call whose function expression is fn.
func (p *parser) call(start int, fn ast.Expr) ast.Expr {
	open := p.pos
	p.next()
	if p.accept(token.RPAREN) {
		return &ast.Call{Span: p.span(start), Func: fn}
	}
	args, keywords, closed := p.argumentList(open, true)
	if !closed {
		p.expect(token.RPAREN)
	}
	return &ast.Call{Span: p.span(start), Func: fn, Args: args, Keywords: keywords}
}

// arguments parses call arguments after the opening parenthesis at token
// open. A lone generator expression is its own argument and spans the
// parentheses; the closing parenthesis is then consumed and closed is true.
// Without allowGenexp the generator form is not accepted, as for class bases.
func (p *parser) arguments(open int, allowGenexp bool) (args []ast.Expr, keywords []*ast.Keyword, closed bool) {
	phase := 0 // 1 after a keyword argument, 2 after a ** argument
	for first := true; ; first = false {
		start := p.pos
		switch {
		case p.at(token.POW):
			star := p.next()
			v := p.expression()
			p.invalidKwargUnpacking(star, v)
			keywords = append(keywords, &ast.Keyword{Span: p.span(start), Value: v})
			phase = 2
		case p.at(token.MUL):
			if phase == 2 {
				p.fail()
			}
			args = append(args, p.starredExpression())
		case p.at(token.NAME) && p.kindAt(1) == token.ASSIGN:
			nameTok := p.cur()
			name := p.ident()
			eq := p.next()
			p.invalidKwarg(nameTok, eq)
			v := p.expression()
			p.invalidKwargComprehension(nameTok, eq)
			keywords = append(keywords, &ast.Keyword{Span: p.span(start), Arg: &name, Value: v})
			phase = max(phase, 1)
		default:
			if phase != 0 {
				p.fail()
			}
			p.invalidKeywordTarget()
			e := p.argExpression()
			p.invalidKwargExpression(e)
			if p.at(token.FOR) || p.at(token.ASYNC) {
				if !first || !allowGenexp {
					p.fail()
				}
				gens := p.comprehension(token.RPAREN)
				return []ast.Expr{&ast.GeneratorExp{Span: p.span(open), Elt: e, Generators: gens}}, nil, true
			}
			args = append(args, e)
		}
		if !p.at(token.COMMA) {
			return args, keywords, false
		}
		p.next()
		if p.at(token.RPAREN) {
			return args, keywords, false
		}
	}
}

// slices is the subscript of a[...]: one slice, or a tuple of slices and
// starred expressions.
func (p *parser) slices() ast.Expr {
	start := p.pos
	var elts []ast.Expr
	tuple := false
	for {
		if p.at(token.MUL) {
			elts = append(elts, p.starredExpression())
			tuple = true
		} else {
			elts = append(elts, p.slice())
		}
		if !p.accept(token.COMMA) {
			break
		}
		tuple = true
		if p.at(token.RBRACK) {
			break
		}
	}
	if !tuple {
		return elts[0]
	}
	return &ast.Tuple{Span: p.span(start), Elts: elts}
}

func (p *parser) slice() ast.Expr {
	start := p.pos
	var lower ast.Expr
	if !p.at(token.COLON) {
		if p.at(token.NAME) && p.kindAt(1) == token.DEFINE {
			return p.namedExpression()
		}
		lower = p.expression()
		if !p.at(token.COLON) {
			return lower
		}
	}
	p.next()
	var upper, step ast.Expr
	if p.startsExpr() {
		upper = p.expression()
	}
	if p.accept(token.COLON) && p.startsExpr() {
		step = p.expression()
	}
	return &ast.Slice{Span: p.span(start), Lower: lower, Upper: upper, Step: step}
}

// forTargets parses the target of a for statement; in the second pass a
// target that is not valid is reported through invalid_for_target.
func (p *parser) forTargets() ast.Expr {
	if !p.callInvalid {
		return p.starTargets()
	}
	var target ast.Expr
	if p.speculate(func() {
		target = p.starTargets()
		if !p.at(token.IN) {
			p.fail()
		}
	}) {
		return target
	}
	e := p.starExpressions()
	p.raiseInvalidTarget(forTargets, e)
	p.fail()
	return nil
}

// defaultValue parses an optional "= expression" of a parameter, with
// invalid_default in the second pass.
func (p *parser) defaultValue() ast.Expr {
	if !p.at(token.ASSIGN) {
		return nil
	}
	if p.callInvalid {
		if k := p.kindAt(1); k == token.RPAREN || k == token.COMMA {
			p.raiseAt(p.cur(), "expected default value expression")
		}
	}
	p.next()
	return p.expression()
}

// invalidParameters is invalid_parameters and invalid_lambda_parameters for
// a list that ends at term.
func (p *parser) invalidParameters(term token.Token, isLambda bool) {
	if !p.callInvalid {
		return
	}
	start := p.pos
	end := func() {
		if !p.accept(token.COMMA) && !p.at(term) {
			p.fail()
		}
	}
	noDefault := func() (a *ast.Arg) {
		var got *ast.Arg
		if p.speculate(func() {
			got = p.param(p.pos, isLambda, false)
			end()
		}) {
			a = got
		}
		return a
	}
	withDefault := func() bool {
		return p.speculate(func() {
			p.param(p.pos, isLambda, false)
			if p.defaultValue() == nil {
				p.fail()
			}
			end()
		})
	}
	maybeDefault := func() bool {
		return p.speculate(func() {
			p.param(p.pos, isLambda, false)
			p.defaultValue()
			end()
		})
	}
	slash := func() {
		p.expect(token.QUO)
		if !p.accept(token.COMMA) && !p.at(term) {
			p.fail()
		}
	}
	slashNoDefault := func() bool {
		return p.speculate(func() {
			n := 0
			for noDefault() != nil {
				n++
			}
			if n == 0 {
				p.fail()
			}
			slash()
		})
	}
	slashWithDefault := func() bool {
		return p.speculate(func() {
			for noDefault() != nil {
			}
			n := 0
			for withDefault() {
				n++
			}
			if n == 0 {
				p.fail()
			}
			slash()
		})
	}
	anySlash := func() bool { return slashNoDefault() || slashWithDefault() }
	alt := func(f func()) {
		p.pos = start
		p.speculate(f)
		p.pos = start
	}

	alt(func() {
		a := p.expect(token.QUO)
		p.expect(token.COMMA)
		p.raiseAt(a, "at least one argument must precede /")
	})
	alt(func() {
		if !anySlash() {
			p.fail()
		}
		for maybeDefault() {
		}
		p.raiseAt(p.expect(token.QUO), "/ may appear only once")
	})
	alt(func() {
		slashNoDefault()
		for noDefault() != nil {
		}
		if !slashWithDefault() {
			n := 0
			for withDefault() {
				n++
			}
			if n == 0 {
				p.fail()
			}
		}
		if a := noDefault(); a != nil {
			p.raiseNode(a, "parameter without a default follows parameter with a default")
		}
	})
	alt(func() {
		for noDefault() != nil {
		}
		open := p.expect(token.LPAREN)
		var closing *tok
		if isLambda {
			p.param(p.pos, true, false)
			for p.at(token.COMMA) && p.kindAt(1) == token.NAME {
				p.next()
				p.param(p.pos, true, false)
			}
			p.accept(token.COMMA)
		} else {
			n := 0
			for noDefault() != nil {
				n++
			}
			if n == 0 {
				p.fail()
			}
			p.accept(token.COMMA)
		}
		closing = p.expect(token.RPAREN)
		what := "Function parameters"
		if isLambda {
			what = "Lambda expression parameters"
		}
		p.raiseTokens(open, closing, what+" cannot be parenthesized")
	})
	alt(func() {
		anySlash()
		for maybeDefault() {
		}
		p.expect(token.MUL)
		if !p.accept(token.COMMA) && noDefault() == nil {
			p.fail()
		}
		for maybeDefault() {
		}
		p.raiseAt(p.expect(token.QUO), "/ must be ahead of *")
	})
	alt(func() {
		n := 0
		for maybeDefault() {
			n++
		}
		if n == 0 {
			p.fail()
		}
		p.expect(token.QUO)
		p.raiseAt(p.expect(token.MUL), "expected comma between / and *")
	})
}

// invalidStarEtc is invalid_star_etc and invalid_lambda_star_etc at the '*'
// that is the token start.
func (p *parser) invalidStarEtc(start int, term token.Token, isLambda bool) {
	if !p.callInvalid {
		return
	}
	pos := p.pos
	defer func() { p.pos = pos }()
	star := &p.toks[start]
	p.next()
	bare := p.at(term) || p.at(token.COMMA) && (p.kindAt(1) == term || p.kindAt(1) == token.POW)
	if isLambda {
		bare = p.at(token.COLON) || p.at(token.COMMA) && (p.kindAt(1) == token.COLON || p.kindAt(1) == token.POW)
	}
	if bare {
		if isLambda {
			p.raiseHere("named arguments must follow bare *")
		}
		p.raiseAt(star, "named arguments must follow bare *")
	}
	var def *tok
	if p.speculate(func() {
		p.param(p.pos, isLambda, false)
		def = p.expect(token.ASSIGN)
	}) {
		p.raiseAt(def, "var-positional argument cannot have default value")
	}
	end := func() {
		if !p.accept(token.COMMA) && !p.at(term) {
			p.fail()
		}
	}
	p.speculate(func() {
		if !p.accept(token.COMMA) {
			p.param(p.pos, isLambda, false)
			end()
		}
		for p.speculate(func() {
			p.param(p.pos, isLambda, false)
			p.defaultValue()
			end()
		}) {
		}
		second := p.expect(token.MUL)
		if !p.accept(token.COMMA) {
			p.param(p.pos, isLambda, false)
			end()
		}
		p.raiseAt(second, "* argument may appear only once")
	})
}

// invalidKwds is invalid_kwds and invalid_lambda_kwds at the '**'.
func (p *parser) invalidKwds(isLambda bool) {
	if !p.callInvalid {
		return
	}
	pos := p.pos
	defer func() { p.pos = pos }()
	p.next()
	p.speculate(func() {
		p.param(p.pos, isLambda, false)
		if p.at(token.ASSIGN) {
			p.raiseAt(p.cur(), "var-keyword argument cannot have default value")
		}
		p.expect(token.COMMA)
		const msg = "arguments cannot follow var-keyword argument"
		if k := p.kind(); k == token.MUL || k == token.POW || k == token.QUO {
			p.raiseAt(p.cur(), msg)
		}
		a := p.param(p.pos, isLambda, false)
		p.raiseNode(a, msg)
	})
}

// invalidStringTail is the first alternative of invalid_expression: a string
// followed by expressions and another string, as in "a" b "c".
func (p *parser) invalidStringTail() {
	if !p.callInvalid || !p.at(token.STRING) {
		return
	}
	pos := p.pos
	p.next()
	var items []ast.Expr
	for !p.at(token.STRING) {
		var e ast.Expr
		if !p.withoutInvalid(func() { e = p.expression() }) {
			break
		}
		items = append(items, e)
	}
	if len(items) > 0 && p.at(token.STRING) {
		p.raiseRange(items[0], items[len(items)-1], "invalid syntax. Is this intended to be part of the string?")
	}
	p.pos = pos
}

// invalidExpression is the invalid_expression alternative with two
// expressions in a row, then invalid_legacy_expression, for the expression
// that starts at token start and whose disjunction is a.
func (p *parser) invalidExpression(start int, a ast.Expr) {
	if !p.callInvalid {
		return
	}
	pos := p.pos
	first := &p.toks[start]
	legacy := false
	if n, ok := a.(*ast.Name); ok && (n.ID == "print" || n.ID == "exec") {
		legacy = true
	}
	skip := first.kind == token.NAME && (p.toks[min(start+1, len(p.toks)-1)].kind == token.STRING || isSoftKeyword(first.lit))
	var b ast.Expr
	if !skip && !legacy && p.withoutInvalid(func() { b = p.expression() }) {
		if p.levels[p.pos-1] != 0 {
			p.raiseRange(a, b, "invalid syntax. Perhaps you forgot a comma?")
		}
	}
	p.pos = pos
	if first.kind == token.NAME && (first.lit == "print" || first.lit == "exec") && p.toks[min(start+1, len(p.toks)-1)].kind != token.LPAREN {
		p.pos = start + 1
		var rest ast.Expr
		if p.speculate(func() { rest = p.starExpressions() }) {
			p.raiseTokenNode(first, rest, "Missing parentheses in call to '"+first.lit+"'. Did you mean "+first.lit+"(...)?")
		}
		p.pos = pos
	}
}

// expressionAfterElse parses the expression after "else" of a conditional
// expression; in the second pass a missing one is reported.
func (p *parser) expressionAfterElse() ast.Expr {
	if !p.callInvalid {
		return p.expression()
	}
	var e ast.Expr
	if !p.speculate(func() { e = p.expression() }) {
		p.raiseNext("expected expression after 'else', but statement is given")
	}
	return e
}

// invalidNamedExpression is invalid_named_expression for the expression e
// that starts at token start.
func (p *parser) invalidNamedExpression(start int, e ast.Expr) {
	if !p.callInvalid {
		return
	}
	pos := p.pos
	if p.at(token.DEFINE) {
		if p.speculate(func() {
			p.next()
			p.expression()
		}) {
			p.raiseNode(e, "cannot use assignment expressions with "+exprName(e))
		}
		p.pos = pos
		return
	}
	if !p.at(token.ASSIGN) {
		return
	}
	notAssign := func() bool { return !p.at(token.ASSIGN) && !p.at(token.DEFINE) }
	p.pos = start
	first := &p.toks[start]
	if first.kind == token.NAME && p.toks[start+1].kind == token.ASSIGN {
		var b ast.Expr
		if p.speculate(func() {
			p.next()
			p.next()
			b = p.bitwiseOr()
			if !notAssign() {
				p.fail()
			}
		}) {
			p.raiseTokenNode(first, b, "invalid syntax. Maybe you meant '==' or ':=' instead of '='?")
		}
		p.pos = start
	}
	if !p.startsListTupleOrKeyword(start) {
		var a ast.Expr
		if p.speculate(func() {
			a = p.bitwiseOr()
			p.expect(token.ASSIGN)
			p.bitwiseOr()
			if !notAssign() {
				p.fail()
			}
		}) {
			p.raiseNode(a, "cannot assign to "+exprName(a)+" here. Maybe you meant '==' instead of '='?")
		}
	}
	p.pos = pos
}

// startsListTupleOrKeyword is the lookahead !(list|tuple|genexp|'True'|
// 'None'|'False') of invalid_named_expression at token start.
func (p *parser) startsListTupleOrKeyword(start int) bool {
	switch p.toks[start].kind {
	case token.TRUE, token.NONE, token.FALSE:
		return true
	case token.LBRACK, token.LPAREN:
		pos := p.pos
		p.pos = start
		defer func() { p.pos = pos }()
		var e ast.Expr
		if !p.speculate(func() { e = p.atom() }) {
			return false
		}
		if e.Pos() != p.toks[start].pos {
			return false
		}
		switch e.(type) {
		case *ast.List, *ast.Tuple, *ast.GeneratorExp:
			return true
		}
	}
	return false
}

// invalidGroupDoubleStar is the second alternative of invalid_group, after
// the '('.
func (p *parser) invalidGroupDoubleStar() {
	if !p.callInvalid || !p.at(token.POW) {
		return
	}
	pos := p.pos
	a := p.next()
	if p.speculate(func() {
		p.expression()
		p.expect(token.RPAREN)
	}) {
		p.raiseAt(a, "cannot use double starred expression here")
	}
	p.pos = pos
}

// invalidGroupStar is the first alternative of invalid_group: a starred
// expression alone in parentheses; from is the token after the '('.
func (p *parser) invalidGroupStar(from int) {
	if !p.callInvalid {
		return
	}
	pos := p.pos
	p.pos = from
	var a ast.Expr
	if p.speculate(func() {
		a = p.starredExpression()
		if !p.at(token.RPAREN) {
			p.fail()
		}
	}) {
		p.raiseNode(a, "cannot use starred expression here")
	}
	p.pos = pos
}

// invalidStarredComprehension is the first alternative of
// invalid_comprehension: a starred element followed by for clauses.
func (p *parser) invalidStarredComprehension(first ast.Expr) {
	if !p.callInvalid || !p.atFor() {
		return
	}
	if _, ok := first.(*ast.Starred); !ok {
		return
	}
	pos := p.pos
	if p.speculate(func() { p.forIfClauses() }) {
		p.raiseNode(first, "iterable unpacking cannot be used in comprehension")
	}
	p.pos = pos
}

// invalidComprehensionTarget is the other alternatives of
// invalid_comprehension: several elements, or one and a comma, before for
// clauses of a list or set display; elts are all the elements parsed.
func (p *parser) invalidComprehensionTarget(first ast.Expr, elts []ast.Expr) {
	if !p.callInvalid || !p.atFor() {
		return
	}
	pos := p.pos
	comma := &p.toks[pos-1]
	if p.speculate(func() { p.forIfClauses() }) {
		const msg = "did you forget parentheses around the comprehension target?"
		switch {
		case len(elts) > 1:
			p.raiseRange(first, elts[len(elts)-1], msg)
		case comma.kind == token.COMMA:
			p.raise(first.Pos(), comma.end, msg)
		}
	}
	p.pos = pos
}

// invalidDictComprehension is invalid_dict_comprehension at the '**'.
func (p *parser) invalidDictComprehension() {
	if !p.callInvalid {
		return
	}
	pos := p.pos
	a := p.next()
	if p.speculate(func() {
		p.bitwiseOr()
		p.forIfClauses()
		p.expect(token.RBRACE)
	}) {
		p.raiseAt(a, "dict unpacking cannot be used in dict comprehension")
	}
	p.pos = pos
}

// invalidDictValue is the starred and the missing value alternatives of
// invalid_double_starred_kvpairs after the ':' at token colon.
func (p *parser) invalidDictValue(colon int) {
	if !p.callInvalid {
		return
	}
	switch {
	case p.at(token.MUL):
		pos := p.pos
		star := p.next()
		if p.speculate(func() { p.bitwiseOr() }) {
			p.raiseFrom(star.pos, "cannot use a starred expression in a dictionary value")
		}
		p.pos = pos
	case p.at(token.RBRACE), p.at(token.COMMA):
		p.raiseAt(&p.toks[colon], "expression expected after dictionary key and ':'")
	}
}

// starredExpression is starred_expression: '*' expression, with its invalid
// alternatives.
func (p *parser) starredExpression() ast.Expr {
	start := p.pos
	star := p.next()
	var v ast.Expr
	if !p.speculate(func() { v = p.expression() }) {
		if p.callInvalid {
			p.raiseHere("Invalid star expression")
		}
		p.fail()
	}
	if p.callInvalid && p.at(token.ASSIGN) {
		pos := p.pos
		var b ast.Expr
		if p.speculate(func() {
			p.next()
			b = p.expression()
		}) {
			p.raiseTokenNode(star, b, "cannot assign to iterable argument unpacking")
		}
		p.pos = pos
	}
	return &ast.Starred{Span: p.span(start), Value: v}
}

// argumentList is arguments, with invalid_arguments in the second pass when
// the arguments do not parse or are not followed by ')'.
func (p *parser) argumentList(open int, allowGenexp bool) (args []ast.Expr, keywords []*ast.Keyword, closed bool) {
	if !p.callInvalid {
		return p.arguments(open, allowGenexp)
	}
	start := p.pos
	if p.speculate(func() {
		args, keywords, closed = p.arguments(open, allowGenexp)
		if !closed && !p.at(token.RPAREN) {
			p.fail()
		}
	}) {
		return args, keywords, closed
	}
	p.pos = start
	p.invalidArguments()
	p.fail()
	return nil, nil, false
}

// argsRule is the args rule: positional arguments, then keyword arguments.
// It stops before an element that may not follow, leaving the position
// before the comma that precedes it; stop is the index of that comma, or -1.
func (p *parser) argsRule() (args []ast.Expr, keywords []*ast.Keyword, stop int) {
	phase, stop := 0, -1
	for first := true; ; first = false {
		save := p.pos
		if !first && !p.accept(token.COMMA) {
			break
		}
		var arg ast.Expr
		var kw *ast.Keyword
		ok := p.speculate(func() {
			start := p.pos
			switch {
			case p.at(token.POW):
				p.next()
				kw = &ast.Keyword{Span: p.span(start), Value: p.expression()}
				phase = 2
			case p.at(token.MUL):
				if phase == 2 {
					p.fail()
				}
				arg = p.starredExpression()
			case p.at(token.NAME) && p.kindAt(1) == token.ASSIGN:
				name := p.ident()
				p.next()
				kw = &ast.Keyword{Arg: &name, Value: p.expression()}
				phase = max(phase, 1)
			default:
				if phase != 0 {
					p.fail()
				}
				arg = p.argExpression()
				if p.at(token.DEFINE) || p.at(token.ASSIGN) {
					p.fail()
				}
			}
		})
		if !ok {
			p.pos = save
			if !first {
				stop = save
			}
			break
		}
		if arg != nil {
			args = append(args, arg)
		} else {
			keywords = append(keywords, kw)
		}
	}
	return args, keywords, stop
}

// invalidArguments is invalid_arguments, tried from the first argument.
func (p *parser) invalidArguments() {
	start := p.pos
	alt := func(f func()) {
		p.pos = start
		p.speculate(f)
		p.pos = start
	}
	alt(func() { // iterable argument unpacking after keyword argument unpacking
		_, kws, stop := p.argsRule()
		if stop < 0 || len(kws) == 0 || !p.at(token.COMMA) || p.kindAt(1) != token.MUL {
			p.fail()
		}
		comma := p.pos
		p.next()
		p.starredExpression()
		if p.at(token.ASSIGN) {
			p.fail()
		}
		p.raiseFrom(p.toks[comma].pos, "iterable argument unpacking follows keyword argument unpacking")
	})
	alt(func() { // f(a for a in b, c)
		a := p.expression()
		gens := p.forIfClauses()
		p.expect(token.COMMA)
		if !p.speculate(func() { p.argsRule() }) {
			p.speculate(func() {
				p.expression()
				p.forIfClauses()
			})
		}
		p.raiseRange(a, lastComprehensionItem(gens), "Generator expression must be parenthesized")
	})
	alt(func() { // f(a=)
		if args, kws, _ := p.argsRule(); len(args)+len(kws) > 0 {
			p.expect(token.COMMA)
		}
		name := p.expect(token.NAME)
		eq := p.expect(token.ASSIGN)
		if !p.at(token.COMMA) && !p.at(token.RPAREN) {
			p.fail()
		}
		p.raiseTokens(name, eq, "expected argument value expression")
	})
	alt(func() { // f(a, b for b in c)
		args, _, _ := p.argsRule()
		gens := p.forIfClauses()
		if len(args) > 1 {
			p.raiseRange(args[len(args)-1], lastComprehensionItem(gens), "Generator expression must be parenthesized")
		}
	})
	alt(func() { // f(a=1, b for b in c)
		p.argsRule()
		p.expect(token.COMMA)
		a := p.expression()
		gens := p.forIfClauses()
		p.raiseRange(a, lastComprehensionItem(gens), "Generator expression must be parenthesized")
	})
	alt(func() { // positional argument after keyword argument
		_, kws, _ := p.argsRule()
		p.expect(token.COMMA)
		if args, kws2, _ := p.argsRule(); len(args)+len(kws2) == 0 {
			p.fail()
		}
		for _, kw := range kws {
			if kw.Arg == nil {
				p.raiseHere("positional argument follows keyword argument unpacking")
			}
		}
		p.raiseHere("positional argument follows keyword argument")
	})
}

// invalidKwargUnpacking is the last alternative of invalid_kwarg after
// "** expression".
func (p *parser) invalidKwargUnpacking(star *tok, v ast.Expr) {
	if !p.callInvalid || !p.at(token.ASSIGN) {
		return
	}
	pos := p.pos
	var b ast.Expr
	if p.speculate(func() {
		p.next()
		b = p.expression()
	}) {
		p.raiseTokenNode(star, b, "cannot assign to keyword argument unpacking")
	}
	p.pos = pos
}

// invalidKwargComprehension is "name = expression for_if_clauses".
func (p *parser) invalidKwargComprehension(name, eq *tok) {
	if !p.callInvalid || !p.atFor() {
		return
	}
	pos := p.pos
	if p.speculate(func() { p.forIfClauses() }) {
		p.raiseTokens(name, eq, "invalid syntax. Maybe you meant '==' or ':=' instead of '='?")
	}
	p.pos = pos
}

// fieldValue parses the expression of a replacement field after its '{',
// with the first alternatives of invalid_fstring_replacement_field.
func (p *parser) fieldValue(template bool) ast.Expr {
	if !p.callInvalid {
		return p.rhs()
	}
	switch k := p.kind(); k {
	case token.ASSIGN, token.EXCLAMATION, token.COLON, token.RBRACE:
		p.raiseAt(p.cur(), stringKind(template)+": valid expression required before '"+k.String()+"'")
	}
	var v ast.Expr
	if !p.speculate(func() { v = p.rhs() }) {
		p.raiseNext(stringKind(template) + ": expecting a valid expression after '{'")
	}
	return v
}

// comprehensionTarget parses the target of a for clause of a comprehension,
// with invalid_for_if_clause in the second pass.
func (p *parser) comprehensionTarget() ast.Expr {
	if p.callInvalid {
		pos := p.pos
		p.speculate(func() {
			p.bitwiseOr()
			for p.at(token.COMMA) {
				save := p.pos
				p.next()
				if !p.speculate(func() { p.bitwiseOr() }) {
					p.pos = save
					break
				}
			}
			p.accept(token.COMMA)
			if !p.at(token.IN) {
				p.raiseHere("'in' expected after for-loop variables")
			}
		})
		p.pos = pos
	}
	return p.forTargets()
}

// invalidStatementIf is the invalid_expression alternative for a statement
// used where an expression is expected: "pass if x else y".
func (p *parser) invalidStatementIf() {
	if !p.callInvalid {
		return
	}
	switch p.kind() {
	case token.PASS, token.BREAK, token.CONTINUE:
	default:
		return
	}
	if p.kindAt(1) != token.IF {
		return
	}
	pos := p.pos
	a := p.next()
	if p.speculate(func() {
		p.next()
		p.disjunction()
		p.expect(token.ELSE)
		p.statementOperand()
	}) {
		p.raiseAt(a, "expected expression before 'if', but statement is given")
	}
	p.pos = pos
}

// statementOperand matches what follows "else" in invalid_expression's
// statement alternative: a simple statement, where an expression is enough
// except for the statements that start with a keyword.
func (p *parser) statementOperand() {
	switch p.kind() {
	case token.PASS, token.BREAK, token.CONTINUE, token.RETURN, token.RAISE, token.DEL, token.IMPORT,
		token.FROM, token.GLOBAL, token.NONLOCAL, token.ASSERT, token.YIELD:
		p.next()
		return
	}
	p.starExpressions()
}

// name consumes a NAME token and returns it as a Name expression.
func (p *parser) name() *ast.Name {
	t := p.expect(token.NAME)
	return &ast.Name{Span: ast.Span{StartPos: t.pos, EndPos: t.end}, ID: identifier(t.lit)}
}

func (p *parser) enter() {
	if p.depth++; p.depth > maxDepth {
		t := p.cur()
		p.raiseAt(t, "Parser stack overflowed - Python source too complex to parse")
	}
}

func (p *parser) leave() {
	p.depth--
}

func (p *parser) accept(k token.Token) bool {
	if p.kind() != k {
		return false
	}
	p.next()
	return true
}

// ident consumes a NAME token and returns its identifier.
func (p *parser) ident() string {
	return identifier(p.expect(token.NAME).lit)
}

// starred parses '*' operand.
func (p *parser) starred(operand func() ast.Expr) ast.Expr {
	start := p.pos
	p.next()
	v := operand()
	return &ast.Starred{Span: p.span(start), Value: v}
}

func (p *parser) number() ast.Expr {
	t := p.expect(token.NUMBER)
	v, err := parseNumber(t.lit)
	if err != nil {
		p.raiseAt(t, err.Error())
	}
	return &ast.Constant{Span: ast.Span{StartPos: t.pos, EndPos: t.end}, Value: v}
}

func (p *parser) expect(k token.Token) *tok {
	if p.kind() != k {
		p.fail()
	}
	return p.next()
}

func (p *parser) fail() {
	panic(softFailure{})
}

func (p *parser) stringConstant() ast.Expr {
	t := p.next()
	v, msg := decodeLiteral(t.lit)
	if msg != "" {
		p.raiseAt(t, msg)
	}
	c := &ast.Constant{Span: ast.Span{StartPos: t.pos, EndPos: t.end}, Value: v}
	if t.lit[0] == 'u' {
		u := "u"
		c.Kind = &u
	}
	return c
}

// concatenate joins the literals of one strings rule as _PyPegen_concatenate_strings does.
func (p *parser) concatenate(start int, items []ast.Expr, template bool) ast.Expr {
	span := p.span(start)
	var hasStr, hasBytes, hasFormat bool
	for _, it := range items {
		c, ok := it.(*ast.Constant)
		switch {
		case !ok:
			hasFormat = true
		case isBytes(c):
			hasBytes = true
		default:
			hasStr = true
		}
	}
	if hasBytes && (hasStr || hasFormat) {
		p.raiseHere("cannot mix bytes and nonbytes literals")
	}
	switch {
	case template:
		return &ast.TemplateStr{Span: span, Values: foldValues(items)}
	case hasFormat:
		return &ast.JoinedStr{Span: span, Values: foldValues(items)}
	case len(items) == 1:
		return items[0]
	case hasBytes:
		var b []byte
		for _, it := range items {
			b = append(b, it.(*ast.Constant).Value.(ast.Bytes)...)
		}
		return &ast.Constant{Span: span, Value: ast.Bytes(b), Kind: items[0].(*ast.Constant).Kind}
	}
	var sb strings.Builder
	for _, it := range items {
		sb.WriteString(string(it.(*ast.Constant).Value.(ast.Str)))
	}
	return &ast.Constant{Span: span, Value: ast.Str(sb.String()), Kind: items[0].(*ast.Constant).Kind}
}

// span returns the extent from token start to the last token consumed that is
// not layout, CPython's EXTRA macro.
func (p *parser) span(start int) ast.Span {
	return ast.Span{StartPos: p.toks[start].pos, EndPos: p.lastEnd()}
}

func (p *parser) lastEnd() token.Pos {
	for i := p.pos - 1; i >= 0; i-- {
		t := &p.toks[i]
		if t.kind == token.EOF || (t.kind >= token.NEWLINE && t.kind <= token.DEDENT) {
			continue
		}
		return t.end
	}
	return token.NoPos
}

// middle decodes literal text of an f-string; it returns nil for empty text.
func (p *parser) middle(t *tok, raw, inSpec bool) ast.Expr {
	text := t.lit
	if !raw {
		var msg string
		if text, msg = decodeEscapes(text); msg != "" {
			// CPython decodes the literal parts of an f-string when its
			// last token is read, and those of a format spec at once.
			if inSpec {
				p.raiseHere(msg)
			}
			if p.literalErr == "" {
				p.literalErr = msg
			}
			return nil
		}
	}
	if text == "" {
		return nil
	}
	return &ast.Constant{Span: ast.Span{StartPos: t.pos, EndPos: t.end}, Value: ast.Str(text)}
}

func (p *parser) startsStarExpr() bool {
	return p.startsExpr() || p.at(token.MUL)
}

// startsExpr reports whether the current token can begin an expression other
// than a starred one.
func (p *parser) startsExpr() bool {
	switch p.kind() {
	case token.NAME, token.NUMBER, token.STRING, token.FSTRING_START, token.TSTRING_START,
		token.LPAREN, token.LBRACK, token.LBRACE, token.ELLIPSIS, token.ADD, token.SUB,
		token.TILDE, token.NOT, token.LAMBDA, token.AWAIT, token.NONE, token.TRUE, token.FALSE:
		return true
	}
	return false
}

// startsComprehension reports whether a for clause follows the element e.
func (p *parser) startsComprehension(e ast.Expr) bool {
	if _, ok := e.(*ast.Starred); ok {
		return false
	}
	return p.at(token.FOR) || p.at(token.ASYNC)
}

// compareOp consumes a comparison operator if there is one.
func (p *parser) compareOp() (ast.CmpOperator, bool) {
	var op ast.CmpOperator
	switch p.kind() {
	case token.EQL:
		op = ast.CmpEq
	case token.NEQ:
		if p.barry {
			p.raiseAt(p.cur(), "with Barry as BDFL, use '<>' instead of '!='")
		}
		op = ast.CmpNotEq
	case token.LEQ:
		op = ast.CmpLtE
	case token.LSS:
		op = ast.CmpLt
		if p.callInvalid && !p.barry && p.kindAt(1) == token.GTR && p.toks[p.pos].end == p.toks[p.pos+1].pos {
			p.raiseTokens(p.cur(), &p.toks[p.pos+1], "invalid syntax.  Maybe you meant '!=' instead of '<>'?")
		}
		if p.barry && p.kindAt(1) == token.GTR && p.toks[p.pos].end == p.toks[p.pos+1].pos {
			p.next()
			op = ast.CmpNotEq
		}
	case token.GEQ:
		op = ast.CmpGtE
	case token.GTR:
		op = ast.CmpGt
	case token.IN:
		op = ast.CmpIn
	case token.NOT:
		if p.kindAt(1) != token.IN {
			return 0, false
		}
		p.next()
		op = ast.CmpNotIn
	case token.IS:
		op = ast.CmpIs
		if p.kindAt(1) == token.NOT {
			p.next()
			op = ast.CmpIsNot
		}
	default:
		return 0, false
	}
	p.next()
	return op, true
}

// raiseRange is RAISE_SYNTAX_ERROR_KNOWN_RANGE from the start of a to the end of b.
func (p *parser) raiseRange(a, b ast.Node, msg string) {
	p.raise(a.Pos(), b.End(), msg)
}

// raiseHere is RAISE_SYNTAX_ERROR: the location is the last token read.
func (p *parser) raiseHere(msg string) {
	panic(p.hereError(msg, nil))
}

// hereError builds the error of RAISE_SYNTAX_ERROR: the location is the last
// token read. An INDENT, DEDENT or end marker has no column, so the position
// of the tokenizer cursor stands in for it and the error has no end.
func (p *parser) hereError(msg string, kind error) *syntaxError {
	t := &p.toks[min(p.reached, len(p.toks)-1)]
	switch t.kind {
	case token.INDENT:
		pos := p.cursor(t.end)
		return &syntaxError{pos: pos, end: noEnd(pos), msg: msg, kind: kind}
	case token.DEDENT, token.EOF:
		pos := p.cursor(t.pos)
		if p.offset(t.pos) >= len(p.src) {
			pos = p.eofCursor()
		}
		return &syntaxError{pos: pos, end: noEnd(pos), msg: msg, kind: kind}
	}
	if t.synthetic {
		pos := p.eofCursor()
		return &syntaxError{pos: pos, end: noEnd(pos), msg: msg, kind: kind}
	}
	pos, end := p.tokRange(t)
	return &syntaxError{pos: pos, end: end, msg: msg, kind: kind}
}

// cursor returns the position of the tokenizer cursor at offset off, which
// CPython reports as a zero-based column: the one-based column minus one.
func (p *parser) cursor(off token.Pos) token.Position {
	pos := p.fset.Position(off)
	pos.Column--
	pos.Offset--
	return pos
}

// eofCursor is the position of the tokenizer cursor at the end of the
// source: on the last line, after its line break.
func (p *parser) eofCursor() token.Position {
	off := len(p.src)
	if off > 0 && p.src[off-1] == '\n' {
		return p.fset.Position(p.file.Pos(off - 1))
	}
	pos := p.fset.Position(p.file.Pos(off))
	pos.Column--
	pos.Offset--
	return pos
}

// raiseFrom is RAISE_SYNTAX_ERROR_STARTING_FROM: from pos to the end of the
// last token read.
func (p *parser) raiseFrom(pos token.Pos, msg string) {
	t := &p.toks[min(p.reached, len(p.toks)-1)]
	_, end := p.tokRange(t)
	panic(&syntaxError{pos: p.fset.Position(pos), end: end, msg: msg, cursor: true})
}

// raiseInvalidTarget is RAISE_SYNTAX_ERROR_INVALID_TARGET.
func (p *parser) raiseInvalidTarget(kind targetKind, e ast.Expr) {
	t := invalidTarget(e, kind)
	if t == nil {
		return
	}
	verb := "assign to "
	if kind == delTargets {
		verb = "delete "
	}
	p.raiseNode(t, "cannot "+verb+exprName(t))
}

// raiseNode is RAISE_SYNTAX_ERROR_KNOWN_LOCATION for a node.
func (p *parser) raiseNode(n ast.Node, msg string) {
	p.raise(n.Pos(), n.End(), msg)
}

// withoutInvalid runs f speculatively with the invalid_* rules off, as the
// *_without_invalid rules do, and reports whether it matched.
func (p *parser) withoutInvalid(f func()) bool {
	saved := p.callInvalid
	p.callInvalid = false
	defer func() { p.callInvalid = saved }()
	return p.speculate(f)
}

// invalidNotOperand is invalid_arithmetic and invalid_factor: "not" right
// after an arithmetic operator. It is called when the operator is consumed.
func (p *parser) invalidNotOperand(applies bool, operand func() ast.Expr) {
	if !p.callInvalid || !applies || !p.at(token.NOT) {
		return
	}
	pos := p.pos
	not := p.next()
	var b ast.Expr
	if p.speculate(func() { b = operand() }) {
		p.raiseTokenNode(not, b, "'not' after an operator must be parenthesized")
	}
	p.pos = pos
}

// speculate runs f and reports whether it matched; on a soft failure it
// restores the position and the nesting depth.
func (p *parser) speculate(f func()) (ok bool) {
	pos, depth := p.pos, p.depth
	defer func() {
		if r := recover(); r != nil {
			if _, soft := r.(softFailure); !soft {
				panic(r)
			}
			p.pos, p.depth = pos, depth
			ok = false
		}
	}()
	f()
	return true
}

func (p *parser) next() *tok {
	t := &p.toks[p.pos]
	if t.kind != token.EOF {
		p.pos++
	}
	return t
}

// raiseTokenNode raises over the range from token t to the end of node n.
func (p *parser) raiseTokenNode(t *tok, n ast.Node, msg string) {
	p.raise(t.pos, n.End(), msg)
}

func (p *parser) atFor() bool {
	return p.at(token.FOR) || p.at(token.ASYNC)
}

// raiseKeyWithoutColon is invalid_kvpair's first alternative: a key not
// followed by ':'. CPython puts the error at the last column of the key.
func (p *parser) raiseKeyWithoutColon(key ast.Expr) {
	end := p.fset.Position(key.End())
	pos := p.fset.Position(key.Pos())
	pos.Column, pos.Offset = end.Column-1, end.Offset-1
	stop := end
	stop.Column, stop.Offset = 1, end.Offset-(end.Column-1)
	panic(&syntaxError{pos: pos, end: stop, msg: "':' expected after dictionary key", cursor: true})
}

// invalidKwarg is the "expected argument value expression" alternative of
// invalid_arguments, after "name =".
func (p *parser) invalidKwarg(name, eq *tok) {
	if p.callInvalid && (p.at(token.COMMA) || p.at(token.RPAREN)) {
		p.raiseTokens(name, eq, "expected argument value expression")
	}
}

// invalidKeywordTarget is the first alternative of invalid_kwarg.
func (p *parser) invalidKeywordTarget() {
	if !p.callInvalid {
		return
	}
	if k := p.kind(); (k == token.TRUE || k == token.FALSE || k == token.NONE) && p.kindAt(1) == token.ASSIGN {
		p.raiseTokens(p.cur(), &p.toks[p.pos+1], "cannot assign to "+p.cur().lit)
	}
}

func (p *parser) kindAt(n int) token.Token {
	i := min(p.pos+n, len(p.toks)-1)
	p.touch(i)
	return p.toks[i].kind
}

// raiseTokens is RAISE_SYNTAX_ERROR_KNOWN_RANGE for two tokens.
func (p *parser) raiseTokens(a, b *tok, msg string) {
	p.raise(a.pos, b.end, msg)
}

// invalidKwargExpression is "!(NAME '=') expression '='" of invalid_kwarg.
func (p *parser) invalidKwargExpression(e ast.Expr) {
	if p.callInvalid && p.at(token.ASSIGN) {
		p.raise(e.Pos(), p.cur().end, "expression cannot contain assignment, perhaps you meant \"==\"?")
	}
}

func (p *parser) raise(pos, end token.Pos, msg string) {
	panic(&syntaxError{pos: p.fset.Position(pos), end: p.fset.Position(end), msg: msg})
}

// invalidFieldEnd raises the "expecting ..." alternatives of
// invalid_fstring_replacement_field when the current token is none of the
// allowed ones.
func (p *parser) invalidFieldEnd(template bool, allowed ...token.Token) {
	if !p.callInvalid || slices.Contains(allowed, p.kind()) {
		return
	}
	names := make([]string, len(allowed))
	for i, k := range allowed {
		names[i] = "'" + k.String() + "'"
	}
	list := names[0] + " or " + names[1]
	if len(allowed) > 2 {
		list = strings.Join(names[:len(allowed)-1], ", or ") + ", or " + names[len(allowed)-1]
	}
	p.raiseNext(stringKind(template) + ": expecting " + list)
}

// invalidFieldClose raises the "expecting '}'" alternatives when the
// replacement field does not end; afterSpec is set after a format spec.
func (p *parser) invalidFieldClose(template, afterSpec bool) {
	if !p.callInvalid || p.at(token.RBRACE) {
		return
	}
	msg := "expecting '}'"
	if afterSpec {
		msg = "expecting '}', or format specs"
	}
	p.raiseNext(stringKind(template) + ": " + msg)
}

// invalidConversion is invalid_fstring_conversion_character after the '!'.
func (p *parser) invalidConversion(template bool) {
	if !p.callInvalid {
		return
	}
	switch {
	case p.at(token.COLON), p.at(token.RBRACE):
		p.raiseNext(stringKind(template) + ": missing conversion character")
	case !p.at(token.NAME):
		p.raiseNext(stringKind(template) + ": invalid conversion character")
	}
}

func (p *parser) at(k token.Token) bool {
	return p.kind() == k
}

func (p *parser) kind() token.Token {
	return p.cur().kind
}

// raiseNext is RAISE_SYNTAX_ERROR_ON_NEXT_TOKEN: the location is the current token.
func (p *parser) raiseNext(msg string) {
	p.raiseAt(p.cur(), msg)
}

func (p *parser) cur() *tok {
	p.touch(p.pos)
	return &p.toks[p.pos]
}

// touch records that token i was read, as CPython's token buffer does, and
// raises the scanner error when i is the token where the scanner stopped.
func (p *parser) touch(i int) {
	if i > p.reached {
		p.reached = i
	}
	if p.errIdx >= 0 && i >= p.errIdx {
		panic(scanFailure{})
	}
}

func (p *parser) raiseAt(t *tok, msg string) {
	pos, end := p.tokRange(t)
	panic(&syntaxError{pos: pos, end: end, msg: msg})
}

// tokRange returns the location CPython records for token t. INDENT, DEDENT
// and the end marker have no column, which becomes offset 0; a NEWLINE ends
// one column after it starts, on its own line.
func (p *parser) tokRange(t *tok) (pos, end token.Position) {
	pos = p.fset.Position(t.pos)
	switch {
	case t.kind == token.INDENT, t.kind == token.DEDENT, t.kind == token.EOF, t.synthetic:
		pos.Column = 0
		return pos, pos
	case t.kind == token.NEWLINE:
		end = pos
		end.Column++
		end.Offset++
		// The token of a line break after a comment starts at the comment.
		if i := p.indexOf(t); i > 0 {
			gap := p.src[p.offset(p.toks[i-1].end):p.offset(t.pos)]
			if j := bytes.IndexByte(gap, '#'); j >= 0 {
				pos = p.fset.Position(p.toks[i-1].end + token.Pos(j))
			}
		}
		return pos, end
	}
	return pos, p.fset.Position(t.end)
}

func (p *parser) offset(pos token.Pos) int {
	return int(pos) - p.file.Base()
}

// indexOf returns the index of token t of p.toks.
func (p *parser) indexOf(t *tok) int {
	i := sort.Search(len(p.toks), func(i int) bool { return p.toks[i].pos >= t.pos })
	for ; i < len(p.toks) && p.toks[i].pos == t.pos; i++ {
		if &p.toks[i] == t {
			return i
		}
	}
	return -1
}

// stringKind is the prefix of the messages of an f-string or t-string.
func stringKind(template bool) string {
	if template {
		return "t-string"
	}
	return "f-string"
}

func isSingleTarget(e ast.Expr) bool {
	switch e.(type) {
	case *ast.Name, *ast.Attribute, *ast.Subscript:
		return true
	}
	return false
}

// isImaginary reports whether e is a (possibly negated) imaginary literal.
func isImaginary(e ast.Expr) bool {
	if u, ok := e.(*ast.UnaryOp); ok {
		e = u.Operand
	}
	c, ok := e.(*ast.Constant)
	if !ok {
		return false
	}
	_, ok = c.Value.(ast.Complex)
	return ok
}

// identifier returns the NFKC form of a non-ASCII name, as CPython does.
func identifier(lit string) string {
	for i := 0; i < len(lit); i++ {
		if lit[i] >= utf8.RuneSelf {
			return norm.NFKC.String(lit)
		}
	}
	return lit
}

// setContext marks e as a Store or Del target and reports whether it can be one.
func setContext(e ast.Expr, ctx ast.ExprContext) bool {
	switch n := e.(type) {
	case *ast.Name:
		n.Ctx = ctx
	case *ast.Attribute:
		n.Ctx = ctx
	case *ast.Subscript:
		n.Ctx = ctx
	case *ast.Starred:
		if ctx == ast.CtxDel || !setContext(n.Value, ctx) {
			return false
		}
		n.Ctx = ctx
	case *ast.Tuple:
		n.Ctx = ctx
		return setContexts(n.Elts, ctx)
	case *ast.List:
		n.Ctx = ctx
		return setContexts(n.Elts, ctx)
	default:
		return false
	}
	return true
}

func setContexts(elts []ast.Expr, ctx ast.ExprContext) bool {
	for _, e := range elts {
		if !setContext(e, ctx) {
			return false
		}
	}
	return true
}

// parseNumber converts the text of a NUMBER token to a value.
func parseNumber(lit string) (ast.Value, error) {
	s := strings.ReplaceAll(lit, "_", "")
	if last := s[len(s)-1]; last == 'j' || last == 'J' {
		f, _ := strconv.ParseFloat(s[:len(s)-1], 64)
		return ast.Complex(complex(0, f)), nil
	}
	base := 10
	digits := s
	if len(s) > 2 && s[0] == '0' {
		switch s[1] {
		case 'x', 'X':
			base, digits = 16, s[2:]
		case 'o', 'O':
			base, digits = 8, s[2:]
		case 'b', 'B':
			base, digits = 2, s[2:]
		}
	}
	if base != 10 || !strings.ContainsAny(s, ".eE") {
		if base == 10 && len(digits) > maxIntDigits {
			return nil, fmt.Errorf("Exceeds the limit (%d digits) for integer string conversion: value has %d digits; use sys.set_int_max_str_digits() to increase the limit - Consider hexadecimal for huge integer literals to avoid decimal conversion limits.", maxIntDigits, len(digits))
		}
		n, ok := new(big.Int).SetString(digits, base)
		if !ok {
			return nil, fmt.Errorf("invalid number %q", lit)
		}
		return ast.Int{N: n}, nil
	}
	f, _ := strconv.ParseFloat(s, 64)
	return ast.Float(f), nil
}

// decodeLiteral decodes a STRING token: prefix, quotes and body. The second
// result is an error message, empty on success.
func decodeLiteral(lit string) (ast.Value, string) {
	i := 0
	isBytes, raw := false, false
	for ; i < len(lit) && lit[i] != '\'' && lit[i] != '"'; i++ {
		switch lit[i] {
		case 'b', 'B':
			isBytes = true
		case 'r', 'R':
			raw = true
		}
	}
	quote := 1
	if len(lit)-i >= 6 && lit[i+1] == lit[i] && lit[i+2] == lit[i] {
		quote = 3
	}
	body := lit[i+quote : len(lit)-quote]
	if !isBytes {
		if raw {
			return ast.Str(body), ""
		}
		s, msg := decodeEscapes(body)
		return ast.Str(s), msg
	}
	for j := 0; j < len(body); j++ {
		if body[j] >= utf8.RuneSelf {
			return nil, "bytes can only contain ASCII literal characters"
		}
	}
	if raw {
		return ast.Bytes(body), ""
	}
	b, msg := decodeByteEscapes(body)
	return ast.Bytes(b), msg
}

func isBytes(c *ast.Constant) bool {
	_, ok := c.Value.(ast.Bytes)
	return ok
}

// foldValues flattens the values of f-strings and t-strings, merges adjacent
// constants (the first one's kind wins) and drops empty ones.
func foldValues(items []ast.Expr) []ast.Expr {
	var flat []ast.Expr
	for _, it := range items {
		switch n := it.(type) {
		case *ast.JoinedStr:
			flat = append(flat, n.Values...)
		case *ast.TemplateStr:
			flat = append(flat, n.Values...)
		default:
			flat = append(flat, it)
		}
	}
	var out []ast.Expr
	for i := 0; i < len(flat); i++ {
		c, ok := flat[i].(*ast.Constant)
		if !ok {
			out = append(out, flat[i])
			continue
		}
		merged := *c
		text := string(c.Value.(ast.Str))
		for i+1 < len(flat) {
			next, ok := flat[i+1].(*ast.Constant)
			if !ok {
				break
			}
			text += string(next.Value.(ast.Str))
			merged.EndPos = next.EndPos
			i++
		}
		if text != "" {
			merged.Value = ast.Str(text)
			out = append(out, &merged)
		}
	}
	return out
}

// decodeEscapes decodes the escapes of a str literal body (CPython's
// unicode_escape codec). A lone trailing backslash stays, as in f-string text.
func decodeEscapes(s string) (string, string) {
	if !strings.Contains(s, "\\") {
		return s, ""
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c != '\\' {
			out = append(out, c)
			i++
			continue
		}
		if i+1 >= len(s) {
			out = append(out, '\\')
			break
		}
		i++
		switch c = s[i]; c {
		case '\n':
			i++
		case '\\', '\'', '"':
			out = append(out, c)
			i++
		case 'a', 'b', 'f', 'n', 'r', 't', 'v':
			out = append(out, simpleEscapes[c])
			i++
		case '0', '1', '2', '3', '4', '5', '6', '7':
			v := 0
			for n := 0; n < 3 && i < len(s) && s[i] >= '0' && s[i] <= '7'; n++ {
				v = v<<3 | int(s[i]-'0')
				i++
			}
			out = ast.AppendRune(out, rune(v))
		case 'x', 'u', 'U':
			width := map[byte]int{'x': 2, 'u': 4, 'U': 8}[c]
			v, n := hexValue(s[i+1:], width)
			if n < width {
				return "", fmt.Sprintf("(unicode error) 'unicodeescape' codec can't decode bytes in position %d-%d: truncated \\%s escape", i-1, i+n, map[byte]string{'x': "xXX", 'u': "uXXXX", 'U': "UXXXXXXXX"}[c])
			}
			if v > unicode.MaxRune {
				return "", fmt.Sprintf("(unicode error) 'unicodeescape' codec can't decode bytes in position %d-%d: illegal Unicode character", i-1, i+n)
			}
			out = ast.AppendRune(out, rune(v))
			i += 1 + n
		case 'N':
			end := strings.IndexByte(s[i:], '}')
			if i+1 >= len(s) || s[i+1] != '{' || end < 0 {
				last := i
				if i+1 < len(s) && s[i+1] == '{' {
					last = len(s) - 1
				}
				return "", fmt.Sprintf("(unicode error) 'unicodeescape' codec can't decode bytes in position %d-%d: malformed \\N character escape", i-1, last)
			}
			r, ok := unicodeName(s[i+2 : i+end])
			if !ok {
				return "", fmt.Sprintf("(unicode error) 'unicodeescape' codec can't decode bytes in position %d-%d: unknown Unicode character name", i-1, i+end)
			}
			out = ast.AppendRune(out, r)
			i += end + 1
		default:
			out = append(out, '\\')
		}
	}
	return string(out), ""
}

// decodeByteEscapes decodes the escapes of a bytes literal body.
func decodeByteEscapes(s string) ([]byte, string) {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c != '\\' {
			out = append(out, c)
			i++
			continue
		}
		if i++; i >= len(s) {
			out = append(out, '\\')
			break
		}
		switch c = s[i]; c {
		case '\n':
			i++
		case '\\', '\'', '"':
			out = append(out, c)
			i++
		case 'a', 'b', 'f', 'n', 'r', 't', 'v':
			out = append(out, simpleEscapes[c])
			i++
		case '0', '1', '2', '3', '4', '5', '6', '7':
			v := 0
			for n := 0; n < 3 && i < len(s) && s[i] >= '0' && s[i] <= '7'; n++ {
				v = v<<3 | int(s[i]-'0')
				i++
			}
			out = append(out, byte(v))
		case 'x':
			v, n := hexValue(s[i+1:], 2)
			if n < 2 {
				return nil, fmt.Sprintf("(value error) invalid \\x escape at position %d", i-1)
			}
			out = append(out, byte(v))
			i += 3
		default:
			out = append(out, '\\')
		}
	}
	return out, ""
}

// hexValue parses up to width hex digits at the start of s and returns the
// value and the number of digits read.
func hexValue(s string, width int) (int, int) {
	v, n := 0, 0
	for n < width && n < len(s) {
		d := strings.IndexByte("0123456789abcdef", s[n]|0x20)
		if d < 0 || !isHexDigit(s[n]) {
			break
		}
		v = v<<4 | d
		n++
	}
	return v, n
}

func isHexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// stripDebug removes the trailing "=" of a t-string debug expression and the
// whitespace and line continuations after it.
func stripDebug(s string) string {
	n := len(s)
	for n > 0 {
		newline := false
		for n > 0 && isSpace(s[n-1]) {
			newline = newline || s[n-1] == '\n' || s[n-1] == '\r'
			n--
		}
		if !newline || n == 0 || s[n-1] != '\\' {
			break
		}
		n--
	}
	if n == 0 || s[n-1] != '=' {
		return s
	}
	return s[:n-1]
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

// stripComments removes "#" comments outside quotes from the text of an
// f-string expression, as the tokenizer does before recording it.
func stripComments(s string) string {
	if !strings.Contains(s, "#") {
		return s
	}
	var out strings.Builder
	var quote byte
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\':
			out.WriteByte(c)
			if i++; i < len(s) {
				out.WriteByte(s[i])
			}
		case c == '"' || c == '\'':
			if quote == 0 {
				quote = c
			} else if quote == c {
				quote = 0
			}
			out.WriteByte(c)
		case c == '#' && quote == 0:
			for i < len(s) && s[i] != '\n' {
				i++
			}
			if i < len(s) {
				out.WriteByte('\n')
			}
		default:
			out.WriteByte(c)
		}
	}
	return out.String()
}

func quoteRepr(s string) string {
	return "'" + s + "'"
}

// noEnd is the end of an error that CPython reports without an end column
// (end_offset -1): a position with column 0.
func noEnd(pos token.Position) token.Position {
	pos.Column = 0
	return pos
}

// invalidTarget is _PyPegen_get_invalid_target: the first part of e that
// cannot be a target.
func invalidTarget(e ast.Expr, kind targetKind) ast.Expr {
	switch n := e.(type) {
	case nil:
		return nil
	case *ast.List:
		return firstInvalidTarget(n.Elts, kind)
	case *ast.Tuple:
		return firstInvalidTarget(n.Elts, kind)
	case *ast.Starred:
		if kind == delTargets {
			return e
		}
		return invalidTarget(n.Value, kind)
	case *ast.Compare:
		if kind == forTargets {
			if n.Ops[0] == ast.CmpIn {
				return invalidTarget(n.Left, kind)
			}
			return nil
		}
		return e
	case *ast.Name, *ast.Subscript, *ast.Attribute:
		return nil
	}
	return e
}

func firstInvalidTarget(elts []ast.Expr, kind targetKind) ast.Expr {
	for _, e := range elts {
		if t := invalidTarget(e, kind); t != nil {
			return t
		}
	}
	return nil
}

// exprName is _PyPegen_get_expr_name.
func exprName(e ast.Expr) string {
	switch n := e.(type) {
	case *ast.Attribute:
		return "attribute"
	case *ast.Subscript:
		return "subscript"
	case *ast.Starred:
		return "starred"
	case *ast.Name:
		return "name"
	case *ast.List:
		return "list"
	case *ast.Tuple:
		return "tuple"
	case *ast.Lambda:
		return "lambda"
	case *ast.Call:
		return "function call"
	case *ast.BoolOp, *ast.BinOp, *ast.UnaryOp:
		return "expression"
	case *ast.GeneratorExp:
		return "generator expression"
	case *ast.Yield, *ast.YieldFrom:
		return "yield expression"
	case *ast.Await:
		return "await expression"
	case *ast.ListComp:
		return "list comprehension"
	case *ast.SetComp:
		return "set comprehension"
	case *ast.DictComp:
		return "dict comprehension"
	case *ast.Dict:
		return "dict literal"
	case *ast.Set:
		return "set display"
	case *ast.JoinedStr, *ast.FormattedValue:
		return "f-string expression"
	case *ast.TemplateStr, *ast.Interpolation:
		return "t-string expression"
	case *ast.Constant:
		switch v := n.Value.(type) {
		case ast.None:
			return "None"
		case ast.Bool:
			if v {
				return "True"
			}
			return "False"
		case ast.Ellipsis:
			return "ellipsis"
		}
		return "literal"
	case *ast.Compare:
		return "comparison"
	case *ast.IfExp:
		return "conditional expression"
	case *ast.NamedExpr:
		return "named expression"
	}
	return "expression"
}

func isSoftKeyword(lit string) bool {
	return lit == "match" || lit == "case" || lit == "type" || lit == "_"
}

// lastComprehensionItem is _PyPegen_get_last_comprehension_item.
func lastComprehensionItem(gens []*ast.Comprehension) ast.Expr {
	last := gens[len(gens)-1]
	if len(last.Ifs) == 0 {
		return last.Iter
	}
	return last.Ifs[len(last.Ifs)-1]
}
