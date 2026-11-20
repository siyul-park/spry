package ssa

import (
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/token"
)

type parser struct {
	prog     *Program
	lines    []string
	n        int
	line     int
	s        string
	i        int
	mod      *Module
	mods     map[string]*Module
	classes  map[string]*Class
	funcs    map[member]*Function
	globals  map[member]*Global
	declared map[any]bool
	refs     []reference
	body     *body
}

type body struct {
	fn     *Function
	cur    *Block
	labels map[int]*Block
	vals   map[string]Value
	uses   []use
	instrs []located
	blocks []reference
}

type use struct {
	dst *Value
	operand
}

type located struct {
	in   *Instr
	line int
}

type operand struct {
	v         Value
	name      string
	line, col int
}

type reference struct {
	key       any
	what      string
	line, col int
}

type member struct {
	mod  *Module
	name string
}

var simpleEscapes = map[byte]byte{
	'\\': '\\', '\'': '\'', '"': '"', 'a': 7, 'b': 8, 'f': 12, 'n': '\n', 'r': '\r', 't': '\t', 'v': 11,
}

// Parse reads the text form that Print writes. It resolves names and
// numbers blocks and results, but does not run Verify. Errors wrap ErrParse
// and carry the line and column.
func Parse(r io.Reader) (*Program, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrParse, err)
	}
	p := &parser{
		prog:     &Program{},
		lines:    strings.Split(string(data), "\n"),
		mods:     map[string]*Module{},
		classes:  map[string]*Class{},
		funcs:    map[member]*Function{},
		globals:  map[member]*Global{},
		declared: map[any]bool{},
	}
	for p.next() {
		if err := p.declaration(); err != nil {
			return nil, err
		}
	}
	if err := p.undeclared(); err != nil {
		return nil, err
	}
	return p.prog, nil
}

// undeclared reports the first name that is used but never declared.
func (p *parser) undeclared() error {
	for _, r := range p.refs {
		if !p.declared[r.key] {
			return p.failAt(r.line, r.col, "%s is used but not declared", r.what)
		}
	}
	return nil
}

func (p *parser) declaration() error {
	switch w := p.word(); w {
	case "module":
		return p.moduleDecl()
	case "global":
		return p.globalDecl()
	case "init":
		return p.initDecl()
	case "entry":
		return p.entryDecl()
	case "class":
		return p.classDecl(false)
	case "func":
		return p.funcDecl(false)
	case "extern":
		switch w := p.word(); w {
		case "class":
			return p.classDecl(true)
		case "func":
			return p.funcDecl(true)
		}
		return p.fail("extern must be followed by class or func")
	default:
		return p.fail("unexpected %q at top level", w)
	}
}

func (p *parser) funcDecl(extern bool) error {
	if p.mod == nil {
		return p.fail("func outside a module")
	}
	name, err := p.localName()
	if err != nil {
		return err
	}
	f := p.function(p.mod, name)
	if p.declared[f] {
		return p.fail("function %s is declared twice", name)
	}
	p.declared[f] = true
	p.mod.Functions = append(p.mod.Functions, f)
	p.body = &body{fn: f, labels: map[int]*Block{}, vals: map[string]Value{}}
	if err := p.header(f); err != nil {
		return err
	}
	if extern {
		p.body = nil
		return p.eol()
	}
	if err := p.expect("{"); err != nil {
		return err
	}
	if err := p.eol(); err != nil {
		return err
	}
	return p.blocks(f)
}

func (p *parser) blocks(f *Function) error {
	for p.next() {
		if p.accept("}") {
			if err := p.eol(); err != nil {
				return err
			}
			return p.closeBody(f)
		}
		var err error
		if p.atLabel() {
			err = p.label()
		} else {
			err = p.instruction()
		}
		if err != nil {
			return err
		}
	}
	return p.fail("function %s is not closed", f.Name)
}

func (p *parser) instruction() error {
	in := &Instr{}
	def := ""
	if p.peek() == '%' {
		var err error
		if def, err = p.valueName(); err != nil {
			return err
		}
		if !isNumeric(def) {
			return p.fail("result name %%%s must be numeric", def)
		}
		if err := p.expect("="); err != nil {
			return err
		}
	}
	w := p.word()
	op, ok := opByName[w]
	if !ok {
		return p.fail("unknown instruction %q", w)
	}
	info := op.info()
	switch hasResult := info.flags&fResult != 0; {
	case def != "" && !hasResult:
		return p.fail("%s has no result", w)
	case def == "" && hasResult:
		return p.fail("%s needs a result name", w)
	}
	in.Op = op
	if p.body.cur == nil {
		return p.fail("instruction before the first block")
	}
	line := p.line
	if err := p.operands(in, info); err != nil {
		return err
	}
	if err := p.trailer(in, info); err != nil {
		return err
	}
	if def != "" {
		if err := p.define(def, in); err != nil {
			return err
		}
	}
	p.body.cur.Instrs = append(p.body.cur.Instrs, in)
	p.body.instrs = append(p.body.instrs, located{in: in, line: line})
	return p.eol()
}

func (p *parser) trailer(in *Instr, info *opInfo) (err error) {
	if info.flags&fResult != 0 {
		if err = p.expect(":"); err != nil {
			return err
		}
		if in.Typ, err = p.typ(); err != nil {
			return err
		}
	}
	if p.accept("->") {
		if err = p.exceptEdges(in); err != nil {
			return err
		}
	}
	if p.accept("!pos") {
		if in.Pos, err = p.position(); err != nil {
			return err
		}
		if in.End, err = p.position(); err != nil {
			return err
		}
	}
	return nil
}

func (p *parser) position() (token.Position, error) {
	var pos token.Position
	b, err := p.quoted(false)
	if err != nil {
		return pos, err
	}
	pos.Filename = string(b)
	for _, dst := range []*int{&pos.Offset, &pos.Line, &pos.Column} {
		if dst != &pos.Offset {
			if err := p.expect(":"); err != nil {
				return pos, err
			}
		}
		if *dst, err = p.number(); err != nil {
			return pos, err
		}
	}
	return pos, nil
}

func (p *parser) exceptEdges(in *Instr) error {
	if !p.acceptWord("normal") {
		return p.fail("expected normal")
	}
	normal, err := p.edge()
	if err != nil {
		return err
	}
	if err := p.expect(","); err != nil {
		return err
	}
	if !p.acceptWord("except") {
		return p.fail("expected except")
	}
	handler, err := p.edge()
	if err != nil {
		return err
	}
	in.Succs = []Edge{normal}
	in.Except = &handler
	return nil
}

// operands parses everything between the mnemonic and the result type.
func (p *parser) operands(in *Instr, info *opInfo) (err error) {
	var ops []operand
	switch {
	case info.succs > 0:
		ops, err = p.branch(in, info)
	case info.aux == auxCallee:
		ops, err = p.callee(in, info)
	case info.flags&fCall != 0:
		ops, err = p.indirect(in, info)
	case info.aux == auxGlobal:
		ops, err = p.global0(in)
	case info.flags&fDot != 0:
		ops, err = p.dotted(in)
	default:
		ops, err = p.plain(in, info)
	}
	if err != nil {
		return err
	}
	in.Args = p.collect(ops)
	return nil
}

func (p *parser) plain(in *Instr, info *opInfo) ([]operand, error) {
	var ops []operand
	for !p.atEnd() {
		if info.aux == auxClass && p.peek() == '@' {
			c, err := p.classRef()
			in.Class = c
			return ops, err
		}
		o, err := p.operand()
		if err != nil {
			return nil, err
		}
		ops = append(ops, o)
		if !p.accept(",") {
			break
		}
	}
	if info.aux == auxClass && in.Class == nil {
		return nil, p.fail("%s needs a class", info.name)
	}
	return ops, nil
}

// atEnd reports whether the operand list is over.
func (p *parser) atEnd() bool {
	p.skip()
	return p.i >= len(p.s) || p.s[p.i] == ':' || p.s[p.i] == '!' || strings.HasPrefix(p.s[p.i:], "->")
}

func (p *parser) dotted(in *Instr) ([]operand, error) {
	head, err := p.operand()
	if err != nil {
		return nil, err
	}
	if in.Attr, err = p.attr(); err != nil {
		return nil, err
	}
	ops := []operand{head}
	for p.accept(",") {
		o, err := p.operand()
		if err != nil {
			return nil, err
		}
		ops = append(ops, o)
	}
	return ops, nil
}

func (p *parser) global0(in *Instr) ([]operand, error) {
	g, err := p.globalRef()
	if err != nil {
		return nil, err
	}
	in.Global = g
	var ops []operand
	for p.accept(",") {
		o, err := p.operand()
		if err != nil {
			return nil, err
		}
		ops = append(ops, o)
	}
	return ops, nil
}

func (p *parser) globalRef() (*Global, error) {
	m, name, err := p.target(false)
	if err != nil {
		return nil, err
	}
	return p.global(m, name), nil
}

func (p *parser) indirect(in *Instr, info *opInfo) ([]operand, error) {
	head, err := p.operand()
	if err != nil {
		return nil, err
	}
	if info.flags&fDot != 0 {
		if in.Attr, err = p.attr(); err != nil {
			return nil, err
		}
	}
	rest, err := p.operandList("(", ")")
	return append([]operand{head}, rest...), err
}

func (p *parser) attr() (string, error) {
	if p.i >= len(p.s) || p.s[p.i] != '.' {
		return "", p.fail("expected .attribute")
	}
	p.i++
	return p.part()
}

func (p *parser) callee(in *Instr, info *opInfo) ([]operand, error) {
	f, err := p.funcRef(false)
	if err != nil {
		return nil, err
	}
	in.Callee = f
	if info.flags&fBracket != 0 {
		return p.operandList("[", "]")
	}
	return p.operandList("(", ")")
}

func (p *parser) branch(in *Instr, info *opInfo) ([]operand, error) {
	var ops []operand
	for range info.arity {
		o, err := p.operand()
		if err != nil {
			return nil, err
		}
		ops = append(ops, o)
		if err := p.expect(","); err != nil {
			return nil, err
		}
	}
	for k := range info.succs {
		if k > 0 {
			if err := p.expect(","); err != nil {
				return nil, err
			}
		}
		e, err := p.edge()
		if err != nil {
			return nil, err
		}
		in.Succs = append(in.Succs, e)
	}
	return ops, nil
}

func (p *parser) edge() (Edge, error) {
	p.skip()
	if err := p.expect("b"); err != nil {
		return Edge{}, err
	}
	n, err := p.number()
	if err != nil {
		return Edge{}, err
	}
	e := Edge{Block: p.block(n)}
	if p.peek() == '(' {
		ops, err := p.operandList("(", ")")
		if err != nil {
			return e, err
		}
		e.Args = p.collect(ops)
	}
	return e, nil
}

// collect turns parsed operands into values and queues the names that refer
// to values defined later.
func (p *parser) collect(ops []operand) []Value {
	if len(ops) == 0 {
		return nil
	}
	vs := make([]Value, len(ops))
	for i, o := range ops {
		vs[i] = o.v
		if o.name != "" {
			p.body.uses = append(p.body.uses, use{dst: &vs[i], operand: o})
		}
	}
	return vs
}

func (p *parser) operandList(open, closing string) ([]operand, error) {
	if err := p.expect(open); err != nil {
		return nil, err
	}
	var ops []operand
	if p.accept(closing) {
		return ops, nil
	}
	for {
		o, err := p.operand()
		if err != nil {
			return nil, err
		}
		ops = append(ops, o)
		if p.accept(closing) {
			return ops, nil
		}
		if err := p.expect(","); err != nil {
			return nil, err
		}
	}
}

func (p *parser) operand() (operand, error) {
	p.skip()
	o := operand{line: p.line, col: p.i + 1}
	if p.peek() == '%' {
		name, err := p.valueName()
		o.name = name
		return o, err
	}
	c, err := p.constant()
	o.v = c
	return o, err
}

func (p *parser) constant() (*Const, error) {
	c := &Const{}
	natural := KindInvalid
	switch ch := p.peek(); {
	case isDigit(ch) || ch == '-':
		v, k, err := p.numberLiteral()
		if err != nil {
			return nil, err
		}
		c.Value, natural = v, k
	case ch == '"' || ch == '\'':
		b, err := p.quoted(false)
		c.Value, natural = ast.Str(b), KindStr
		if err != nil {
			return nil, err
		}
	case ch == 'b' && p.i+1 < len(p.s) && (p.s[p.i+1] == '"' || p.s[p.i+1] == '\''):
		p.i++
		b, err := p.quoted(true)
		c.Value, natural = ast.Bytes(b), KindBytes
		if err != nil {
			return nil, err
		}
	default:
		if err := p.wordLiteral(c, &natural); err != nil {
			return nil, err
		}
	}
	if p.i < len(p.s) && p.s[p.i] == ':' {
		p.i++
		t, err := p.typ()
		c.Typ = t
		return c, err
	}
	if c.Undef {
		return nil, p.fail("undef needs a type")
	}
	c.Typ = Type{Kind: natural}
	return c, nil
}

func (p *parser) wordLiteral(c *Const, natural *Kind) error {
	switch w := p.word(); w {
	case "True", "False":
		c.Value, *natural = ast.Bool(w == "True"), KindI1
	case "None":
		c.Value, *natural = ast.None{}, KindNone
	case "undef":
		c.Undef = true
	case "inf":
		c.Value, *natural = ast.Float(math.Inf(1)), KindF64
	case "nan":
		c.Value, *natural = ast.Float(math.NaN()), KindF64
	default:
		return p.fail("expected an operand, found %q", w)
	}
	return nil
}

func (p *parser) numberLiteral() (ast.Value, Kind, error) {
	start := p.i
	if p.s[p.i] == '-' {
		p.i++
		if strings.HasPrefix(p.s[p.i:], "inf") {
			p.i += 3
			return ast.Float(math.Inf(-1)), KindF64, nil
		}
	}
	p.digits()
	isFloat := false
	if p.i < len(p.s) && p.s[p.i] == '.' {
		isFloat = true
		p.i++
		p.digits()
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		isFloat = true
		p.i++
		if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
			p.i++
		}
		p.digits()
	}
	text := p.s[start:p.i]
	if isFloat {
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, 0, p.fail("bad float %q", text)
		}
		return ast.Float(f), KindF64, nil
	}
	n, ok := new(big.Int).SetString(text, 10)
	if !ok {
		return nil, 0, p.fail("bad int %q", text)
	}
	return copyInt(ast.Int{N: n}), KindInt, nil
}

func (p *parser) label() error {
	p.i++
	n, err := p.number()
	if err != nil {
		return err
	}
	b := p.block(n)
	if b.Index == -1 {
		return p.fail("block b%d is defined twice", n)
	}
	b.Index = -1
	if p.peek() == '(' {
		if b.Params, err = p.params("(", ")"); err != nil {
			return err
		}
	}
	if err := p.expect(":"); err != nil {
		return err
	}
	p.body.cur = b
	p.body.fn.Blocks = append(p.body.fn.Blocks, b)
	return p.eol()
}

// block returns the block labelled b<n>, creating it on first use. A defined
// block has Index -1 until finish numbers it.
func (p *parser) block(n int) *Block {
	b, ok := p.body.labels[n]
	if !ok {
		b = &Block{Index: n}
		p.body.labels[n] = b
		p.body.blocks = append(p.body.blocks, reference{key: b, what: "block b" + strconv.Itoa(n), line: p.line, col: p.i + 1})
	}
	return b
}

func (p *parser) number() (int, error) {
	p.skip()
	start := p.i
	p.digits()
	n, err := strconv.Atoi(p.s[start:p.i])
	if err != nil {
		return 0, p.fail("expected a number")
	}
	return n, nil
}

func (p *parser) digits() {
	for p.i < len(p.s) && isDigit(p.s[p.i]) {
		p.i++
	}
}

func (p *parser) atLabel() bool {
	return p.i+1 < len(p.s) && p.s[p.i] == 'b' && isDigit(p.s[p.i+1])
}

func (p *parser) closeBody(f *Function) error {
	b := p.body
	p.body = nil
	if len(f.Blocks) == 0 {
		return p.fail("function %s has no blocks", f.Name)
	}
	for _, u := range b.uses {
		v, ok := b.vals[u.name]
		if !ok {
			return p.failAt(u.line, u.col, "undefined value %%%s", u.name)
		}
		*u.dst = v
	}
	for _, l := range b.instrs {
		if msg := l.in.shape(); msg != "" {
			return p.failAt(l.line, 1, "%s", msg)
		}
	}
	for _, r := range b.blocks {
		if r.key.(*Block).Index != -1 {
			return p.failAt(r.line, r.col, "%s is used but not defined", r.what)
		}
	}
	f.finish()
	return nil
}

func (p *parser) header(f *Function) (err error) {
	if f.Params, err = p.params("(", ")"); err != nil {
		return err
	}
	if p.peek() == '[' {
		if f.Captures, err = p.params("[", "]"); err != nil {
			return err
		}
	}
	if err = p.expect("->"); err != nil {
		return err
	}
	if f.Result, err = p.typ(); err != nil {
		return err
	}
	if p.acceptWord("yield") {
		f.Generator = true
		if f.Yield, err = p.typ(); err != nil {
			return err
		}
		if !p.acceptWord("send") {
			return p.fail("expected send")
		}
		if f.Send, err = p.typ(); err != nil {
			return err
		}
	}
	if p.acceptWord("source") {
		var b []byte
		if b, err = p.quoted(false); err != nil {
			return err
		}
		f.Source = string(b)
	}
	return nil
}

func (p *parser) params(open, closing string) ([]*Param, error) {
	if err := p.expect(open); err != nil {
		return nil, err
	}
	var out []*Param
	if p.accept(closing) {
		return out, nil
	}
	for {
		name, err := p.valueName()
		if err != nil {
			return nil, err
		}
		if isNumeric(name) {
			return nil, p.fail("parameter name %%%s must not be numeric", name)
		}
		if err := p.expect(":"); err != nil {
			return nil, err
		}
		t, err := p.typ()
		if err != nil {
			return nil, err
		}
		prm := &Param{Name: name, Typ: t}
		if err := p.define(name, prm); err != nil {
			return nil, err
		}
		out = append(out, prm)
		if p.accept(closing) {
			return out, nil
		}
		if err := p.expect(","); err != nil {
			return nil, err
		}
	}
}

func (p *parser) define(name string, v Value) error {
	if _, ok := p.body.vals[name]; ok {
		return p.fail("%%%s is defined twice", name)
	}
	p.body.vals[name] = v
	return nil
}

func (p *parser) valueName() (string, error) {
	if err := p.expect("%"); err != nil {
		return "", err
	}
	start := p.i
	for p.i < len(p.s) && (isDigit(p.s[p.i]) || p.s[p.i] == '_' || p.s[p.i] == '$' || isLetter(p.s[p.i])) {
		p.i++
	}
	if p.i == start {
		return "", p.fail("expected a value name")
	}
	return p.s[start:p.i], nil
}

func (p *parser) classDecl(extern bool) error {
	if err := p.expect("@"); err != nil {
		return err
	}
	name, err := p.part()
	if err != nil {
		return err
	}
	c := p.class(name)
	if p.declared[c] {
		return p.fail("class %s is declared twice", name)
	}
	p.declared[c] = true
	c.Extern = extern
	p.prog.Classes = append(p.prog.Classes, c)
	if p.accept(":") {
		if err := p.bases(c); err != nil {
			return err
		}
	}
	if err := p.expect("{"); err != nil {
		return err
	}
	if err := p.eol(); err != nil {
		return err
	}
	return p.members(c)
}

func (p *parser) members(c *Class) error {
	for p.next() {
		if p.accept("}") {
			return p.eol()
		}
		switch w := p.word(); w {
		case "field":
			if err := p.field(c); err != nil {
				return err
			}
		case "method":
			if err := p.method(c); err != nil {
				return err
			}
		default:
			return p.fail("expected field, method or }, found %q", w)
		}
	}
	return p.fail("class %s is not closed", c.Name)
}

func (p *parser) method(c *Class) error {
	name, err := p.part()
	if err != nil {
		return err
	}
	if err := p.expect("->"); err != nil {
		return err
	}
	f, err := p.funcRef(true)
	if err != nil {
		return err
	}
	c.Methods = append(c.Methods, Method{Name: name, Func: f})
	return p.eol()
}

func (p *parser) field(c *Class) error {
	name, err := p.part()
	if err != nil {
		return err
	}
	f := Field{Name: name, Owner: c}
	if err := p.expect(":"); err != nil {
		return err
	}
	if f.Type, err = p.typ(); err != nil {
		return err
	}
	if p.acceptWord("from") {
		if f.Owner, err = p.classRef(); err != nil {
			return err
		}
	}
	c.Fields = append(c.Fields, f)
	return p.eol()
}

func (p *parser) acceptWord(w string) bool {
	p.skip()
	rest := p.s[p.i:]
	if !strings.HasPrefix(rest, w) || (len(rest) > len(w) && isWordByte(rest[len(w)])) {
		return false
	}
	p.i += len(w)
	return true
}

func (p *parser) bases(c *Class) error {
	for {
		b, err := p.classRef()
		if err != nil {
			return err
		}
		c.Bases = append(c.Bases, b)
		if !p.accept(",") {
			return nil
		}
	}
}

func (p *parser) entryDecl() error {
	if p.prog.Entry != nil {
		return p.fail("two entry functions")
	}
	f, err := p.funcRef(true)
	if err != nil {
		return err
	}
	p.prog.Entry = f
	return p.eol()
}

func (p *parser) initDecl() error {
	if p.mod == nil {
		return p.fail("init outside a module")
	}
	if p.mod.Init != nil {
		return p.fail("module %s has two init functions", p.mod.Name)
	}
	f, err := p.funcRef(false)
	if err != nil {
		return err
	}
	p.mod.Init = f
	return p.eol()
}

// funcRef parses @name, or @mod:name. A bare name belongs to the current
// module and is not allowed outside one or when qualified is set.
func (p *parser) funcRef(qualified bool) (*Function, error) {
	m, name, err := p.target(qualified)
	if err != nil {
		return nil, err
	}
	return p.function(m, name), nil
}

func (p *parser) function(m *Module, name string) *Function {
	f, ok := p.funcs[member{m, name}]
	if !ok {
		f = &Function{Name: name, Module: m}
		p.funcs[member{m, name}] = f
		p.refer(f, "function @"+name)
	}
	return f
}

func (p *parser) target(qualified bool) (*Module, string, error) {
	mod, name, err := p.symbol()
	if err != nil {
		return nil, "", err
	}
	if mod != "" {
		return p.module(mod), name, nil
	}
	if qualified || p.mod == nil {
		return nil, "", p.fail("@%s needs a module", name)
	}
	return p.mod, name, nil
}

func (p *parser) globalDecl() error {
	if p.mod == nil {
		return p.fail("global outside a module")
	}
	name, err := p.localName()
	if err != nil {
		return err
	}
	g := p.global(p.mod, name)
	if p.declared[g] {
		return p.fail("global %s is declared twice", name)
	}
	p.declared[g] = true
	if err := p.expect(":"); err != nil {
		return err
	}
	if g.Type, err = p.typ(); err != nil {
		return err
	}
	p.mod.Globals = append(p.mod.Globals, g)
	return p.eol()
}

func (p *parser) typ() (Type, error) {
	p.skip()
	var t Type
	var err error
	switch {
	case p.accept("("):
		if t, err = p.typ(); err != nil {
			return t, err
		}
		if err = p.expect(")"); err != nil {
			return t, err
		}
	case p.peek() == '@':
		c, cerr := p.classRef()
		t, err = Type{Kind: KindClass, Class: c}, cerr
	default:
		t, err = p.namedType()
	}
	if err != nil {
		return t, err
	}
	if p.i < len(p.s) && p.s[p.i] == '?' {
		p.i++
		if !t.Kind.info().ref {
			return t, p.fail("%s cannot be nullable", t)
		}
		t.Nullable = true
	}
	return t, nil
}

func (p *parser) namedType() (Type, error) {
	w := p.word()
	for k := KindI1; int(k) < len(kinds); k++ {
		if kinds[k].name != w || k == KindClass {
			continue
		}
		switch {
		case k == KindTypeOf:
			return p.typeOf()
		case k == KindTuple:
			return p.tuple()
		case k == KindFunc:
			return p.funcType()
		case kinds[k].elems > 0:
			return p.generic(k)
		}
		return Type{Kind: k}, nil
	}
	return Type{}, p.fail("unknown type %q", w)
}

func (p *parser) generic(k Kind) (Type, error) {
	t := Type{Kind: k}
	if err := p.expect("<"); err != nil {
		return t, err
	}
	for i := range kinds[k].elems {
		if i > 0 {
			if err := p.expect(","); err != nil {
				return t, err
			}
		}
		e, err := p.typ()
		if err != nil {
			return t, err
		}
		t.Elems = append(t.Elems, e)
	}
	return t, p.expect(">")
}

func (p *parser) funcType() (Type, error) {
	t := Type{Kind: KindFunc}
	if err := p.expect("("); err != nil {
		return t, err
	}
	for !p.accept(")") {
		if len(t.Elems) > 0 {
			if err := p.expect(","); err != nil {
				return t, err
			}
		}
		e, err := p.typ()
		if err != nil {
			return t, err
		}
		t.Elems = append(t.Elems, e)
	}
	if err := p.expect("->"); err != nil {
		return t, err
	}
	r, err := p.typ()
	t.Elems = append(t.Elems, r)
	return t, err
}

func (p *parser) tuple() (Type, error) {
	t := Type{Kind: KindTuple}
	if err := p.expect("<"); err != nil {
		return t, err
	}
	if p.accept(">") {
		return t, nil
	}
	for {
		e, err := p.typ()
		if err != nil {
			return t, err
		}
		t.Elems = append(t.Elems, e)
		if len(t.Elems) == 1 && p.accept("...") {
			t.Variadic = true
			return t, p.expect(">")
		}
		if p.accept(">") {
			return t, nil
		}
		if err := p.expect(","); err != nil {
			return t, err
		}
	}
}

func (p *parser) typeOf() (Type, error) {
	if err := p.expect("<"); err != nil {
		return Type{}, err
	}
	c, err := p.classRef()
	if err != nil {
		return Type{}, err
	}
	return Type{Kind: KindTypeOf, Class: c}, p.expect(">")
}

func (p *parser) classRef() (*Class, error) {
	mod, name, err := p.symbol()
	if err != nil {
		return nil, err
	}
	if mod != "" {
		return nil, p.fail("class @%s is not module qualified", name)
	}
	return p.class(name), nil
}

func (p *parser) class(name string) *Class {
	c, ok := p.classes[name]
	if !ok {
		c = &Class{Name: name}
		p.classes[name] = c
		p.refer(c, "class @"+name)
	}
	return c
}

func (p *parser) global(m *Module, name string) *Global {
	g, ok := p.globals[member{m, name}]
	if !ok {
		g = &Global{Name: name, Module: m}
		p.globals[member{m, name}] = g
		p.refer(g, "global @"+name)
	}
	return g
}

// localName parses @name of a declaration.
func (p *parser) localName() (string, error) {
	mod, name, err := p.symbol()
	if err == nil && mod != "" {
		err = p.fail("declaration of @%s:%s is qualified", mod, name)
	}
	return name, err
}

// symbol parses @name or @mod:name; each part is bare or quoted.
func (p *parser) symbol() (mod, name string, err error) {
	if err = p.expect("@"); err != nil {
		return "", "", err
	}
	if name, err = p.part(); err != nil {
		return "", "", err
	}
	if p.i < len(p.s) && p.s[p.i] == ':' {
		p.i++
		mod = name
		name, err = p.part()
	}
	return mod, name, err
}

func (p *parser) expect(tok string) error {
	if !p.accept(tok) {
		return p.fail("expected %q", tok)
	}
	return nil
}

func (p *parser) accept(tok string) bool {
	p.skip()
	if strings.HasPrefix(p.s[p.i:], tok) {
		p.i += len(tok)
		return true
	}
	return false
}

func (p *parser) moduleDecl() error {
	name, err := p.part()
	if err != nil {
		return err
	}
	m := p.module(name)
	if p.declared[m] {
		return p.fail("module %s is declared twice", name)
	}
	p.declared[m] = true
	p.prog.Modules = append(p.prog.Modules, m)
	p.mod = m
	return p.eol()
}

func (p *parser) eol() error {
	p.skip()
	if p.i < len(p.s) {
		return p.fail("unexpected %q", p.s[p.i:])
	}
	return nil
}

func (p *parser) module(name string) *Module {
	m, ok := p.mods[name]
	if !ok {
		m = &Module{Name: name}
		p.mods[name] = m
		p.refer(m, "module "+name)
	}
	return m
}

func (p *parser) refer(key any, what string) {
	p.refs = append(p.refs, reference{key: key, what: what, line: p.line, col: p.i + 1})
}

// part parses a bare name, with balanced angle brackets, or a quoted one.
func (p *parser) part() (string, error) {
	p.skip()
	if p.peek() == '"' || p.peek() == '\'' {
		b, err := p.quoted(false)
		return string(b), err
	}
	start, depth := p.i, 0
scan:
	for p.i < len(p.s) {
		switch c := p.s[p.i]; {
		case c == '<':
			depth++
		case c == '>':
			if depth == 0 {
				break scan
			}
			depth--
		case !isNameByte(c):
			break scan
		}
		p.i++
	}
	if p.i == start {
		return "", p.fail("expected a name")
	}
	return p.s[start:p.i], nil
}

// quoted reads a Python string literal of either quote and returns its
// contents: WTF-8 text, or the bytes of a bytes literal.
func (p *parser) quoted(isBytes bool) ([]byte, error) {
	p.skip()
	if p.i >= len(p.s) || (p.s[p.i] != '"' && p.s[p.i] != '\'') {
		return nil, p.fail("expected a quoted string")
	}
	delim := p.s[p.i]
	p.i++
	out := []byte{}
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == delim:
			p.i++
			return out, nil
		case c == '\\':
			var err error
			if out, err = p.escape(out, isBytes); err != nil {
				return nil, err
			}
		case isBytes && (c < 0x20 || c >= 0x7f):
			return nil, p.fail("bytes literal has a byte outside printable ASCII")
		case isBytes:
			out = append(out, c)
			p.i++
		default:
			r, size := utf8.DecodeRuneInString(p.s[p.i:])
			if r == utf8.RuneError && size == 1 {
				return nil, p.fail("invalid UTF-8 in string")
			}
			out = append(out, p.s[p.i:p.i+size]...)
			p.i += size
		}
	}
	return nil, p.fail("unterminated string")
}

func (p *parser) escape(out []byte, isBytes bool) ([]byte, error) {
	p.i++
	if p.i >= len(p.s) {
		return nil, p.fail("unterminated string")
	}
	c := p.s[p.i]
	p.i++
	if v, ok := simpleEscapes[c]; ok {
		return append(out, v), nil
	}
	width := map[byte]int{'x': 2, 'u': 4, 'U': 8}[c]
	if width == 0 || (isBytes && c != 'x') {
		return nil, p.fail("invalid escape \\%c", c)
	}
	if p.i+width > len(p.s) {
		return nil, p.fail("truncated escape")
	}
	v, err := strconv.ParseUint(p.s[p.i:p.i+width], 16, 32)
	if err != nil || strings.ContainsAny(p.s[p.i:p.i+width], "+-_") || v > utf8.MaxRune {
		return nil, p.fail("invalid escape \\%c%s", c, p.s[p.i:p.i+width])
	}
	p.i += width
	if isBytes {
		return append(out, byte(v)), nil
	}
	return ast.AppendRune(out, rune(v)), nil
}

func (p *parser) fail(format string, args ...any) error {
	return p.failAt(p.line, p.i+1, format, args...)
}

func (p *parser) failAt(line, col int, format string, args ...any) error {
	return fmt.Errorf("%w: %d:%d: %s", ErrParse, line, col, fmt.Sprintf(format, args...))
}

func (p *parser) peek() byte {
	p.skip()
	if p.i < len(p.s) {
		return p.s[p.i]
	}
	return 0
}

func (p *parser) word() string {
	p.skip()
	start := p.i
	for p.i < len(p.s) && isWordByte(p.s[p.i]) && (p.i > start || !isDigit(p.s[p.i])) {
		p.i++
	}
	return p.s[start:p.i]
}

func isWordByte(c byte) bool {
	return isLetter(c) || isDigit(c) || c == '_'
}

func isLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// next loads the next line that is neither blank nor a comment.
func (p *parser) next() bool {
	for p.n < len(p.lines) {
		p.line = p.n + 1
		line := strings.TrimSuffix(p.lines[p.n], "\r")
		p.n++
		if t := strings.TrimSpace(line); t != "" && t[0] != ';' {
			p.s, p.i = line, 0
			p.skip()
			return true
		}
	}
	return false
}

func (p *parser) skip() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t') {
		p.i++
	}
}

func isNumeric(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return false
		}
	}
	return s != ""
}
func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
