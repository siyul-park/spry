package ssa

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/token"
)

// PrintOption configures Print.
type PrintOption func(*printer)

type printer struct {
	b       strings.Builder
	pos     bool
	started bool
	mod     *Module
	err     error
}

// Print writes the text form of p, which Parse reads back. It returns an
// error wrapping ErrInvalid when an instruction is malformed or a constant
// has no text form.
func Print(w io.Writer, p *Program, opts ...PrintOption) error {
	pr := &printer{}
	for _, opt := range opts {
		opt(pr)
	}
	for _, c := range p.Classes {
		pr.class(c)
	}
	for _, m := range p.Modules {
		pr.module(m)
	}
	pr.mod = nil
	if p.Entry != nil {
		pr.begin()
		pr.linef("entry %s", pr.funcRef(p.Entry))
	}
	if pr.err != nil {
		return pr.err
	}
	_, err := io.WriteString(w, pr.b.String())
	return err
}

// WithPositions makes Print follow each instruction with its source span.
func WithPositions() PrintOption {
	return func(pr *printer) { pr.pos = true }
}

func (pr *printer) module(m *Module) {
	pr.mod = m
	pr.begin()
	pr.linef("module %s", part(m.Name))
	for _, g := range m.Globals {
		pr.linef("global %s : %s", symbol("", g.Name), g.Type)
	}
	if m.Init != nil {
		pr.linef("init %s", pr.funcRef(m.Init))
	}
	for _, f := range m.Functions {
		pr.function(f)
	}
}

func (pr *printer) function(f *Function) {
	pr.begin()
	head := "func " + symbol("", f.Name) + "(" + paramList(f.Params) + ")"
	if len(f.Captures) > 0 {
		head += " [" + paramList(f.Captures) + "]"
	}
	head += " -> " + f.Result.String()
	if f.Generator {
		head += " yield " + f.Yield.String() + " send " + f.Send.String()
	}
	if f.Source != "" {
		head += " source " + quote(f.Source)
	}
	if len(f.Blocks) == 0 {
		pr.linef("extern %s", head)
		return
	}
	pr.linef("%s {", head)
	for _, b := range f.Blocks {
		label := "b" + strconv.Itoa(b.Index)
		if len(b.Params) > 0 {
			label += "(" + paramList(b.Params) + ")"
		}
		pr.linef("%s:", label)
		for _, in := range b.Instrs {
			pr.instr(in)
		}
	}
	pr.linef("}")
}

func (pr *printer) instr(in *Instr) {
	if msg := in.shape(); msg != "" {
		pr.fail("%s", msg)
		return
	}
	info := in.Op.info()
	var line strings.Builder
	line.WriteString("  ")
	if info.flags&fResult != 0 {
		fmt.Fprintf(&line, "%%%d = ", in.ID)
	}
	line.WriteString(info.name)
	if body := pr.body(in, info); body != "" {
		line.WriteString(" " + body)
	}
	if info.flags&fResult != 0 {
		line.WriteString(" : " + in.Typ.String())
	}
	if in.Except != nil {
		line.WriteString(" -> normal " + pr.edge(in.Succs[0]) + ", except " + pr.edge(*in.Except))
	}
	if pr.pos && (in.Pos != token.Position{} || in.End != token.Position{}) {
		line.WriteString(" !pos " + positionText(in.Pos) + " " + positionText(in.End))
	}
	pr.linef("%s", line.String())
}

func positionText(p token.Position) string {
	return fmt.Sprintf("%s %d:%d:%d", quote(p.Filename), p.Offset, p.Line, p.Column)
}

func (pr *printer) body(in *Instr, info *opInfo) string {
	args := make([]string, len(in.Args))
	for i, a := range in.Args {
		args[i] = pr.operand(a)
	}
	switch {
	case info.succs > 0:
		items := append(args, pr.edges(in.Succs)...)
		return strings.Join(items, ", ")
	case info.aux == auxCallee && info.flags&fBracket != 0:
		return pr.funcRef(in.Callee) + " [" + strings.Join(args, ", ") + "]"
	case info.aux == auxCallee:
		return pr.funcRef(in.Callee) + "(" + strings.Join(args, ", ") + ")"
	case info.flags&fCall != 0:
		head := args[0]
		if info.flags&fDot != 0 {
			head += "." + part(in.Attr)
		}
		return head + "(" + strings.Join(args[1:], ", ") + ")"
	case info.aux == auxGlobal:
		return strings.Join(append([]string{pr.globalRef(in.Global)}, args...), ", ")
	case info.flags&fDot != 0:
		args[0] += "." + part(in.Attr)
	case info.aux == auxClass:
		args = append(args, symbol("", in.Class.Name))
	}
	return strings.Join(args, ", ")
}

func (pr *printer) globalRef(g *Global) string {
	if g.Module == nil || g.Module == pr.mod {
		return symbol("", g.Name)
	}
	return symbol(g.Module.Name, g.Name)
}

func (pr *printer) edges(es []Edge) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = pr.edge(e)
	}
	return out
}

func (pr *printer) edge(e Edge) string {
	s := "b" + strconv.Itoa(e.Block.Index)
	if len(e.Args) == 0 {
		return s
	}
	args := make([]string, len(e.Args))
	for i, a := range e.Args {
		args[i] = pr.operand(a)
	}
	return s + "(" + strings.Join(args, ", ") + ")"
}

func (pr *printer) operand(v Value) string {
	switch v := v.(type) {
	case *Param:
		return "%" + v.Name
	case *Instr:
		return "%" + strconv.Itoa(v.ID)
	case *Const:
		s, err := constText(v)
		if err != nil {
			pr.fail("%v", err)
		}
		return s
	}
	return ""
}

func constText(c *Const) (string, error) {
	if c.Undef {
		return "undef:" + c.Typ.String(), nil
	}
	lit, natural, err := literal(c.Value)
	if err != nil {
		return "", err
	}
	if c.Typ.same(Type{Kind: natural}) {
		return lit, nil
	}
	return lit + ":" + c.Typ.String(), nil
}

// literal returns the text of v and the kind that text means on its own.
func literal(v ast.Value) (string, Kind, error) {
	switch v := v.(type) {
	case ast.Int:
		if v.N == nil {
			return "", 0, fmt.Errorf("constant int has no value")
		}
		return v.N.String(), KindInt, nil
	case ast.Float:
		return floatText(float64(v)), KindF64, nil
	case ast.Bool:
		if v {
			return "True", KindI1, nil
		}
		return "False", KindI1, nil
	case ast.None:
		return "None", KindNone, nil
	case ast.Str:
		return ast.Repr(string(v)), KindStr, nil
	case ast.Bytes:
		return bytesRepr(v), KindBytes, nil
	}
	return "", 0, fmt.Errorf("constant %T has no text form", v)
}

// bytesRepr is the repr() of a Python bytes.
func bytesRepr(b []byte) string {
	quote := byte('\'')
	if strings.IndexByte(string(b), '\'') >= 0 && strings.IndexByte(string(b), '"') < 0 {
		quote = '"'
	}
	var s strings.Builder
	s.WriteString("b")
	s.WriteByte(quote)
	for _, c := range b {
		switch {
		case c == quote || c == '\\':
			s.WriteByte('\\')
			s.WriteByte(c)
		case c == '\t':
			s.WriteString(`\t`)
		case c == '\n':
			s.WriteString(`\n`)
		case c == '\r':
			s.WriteString(`\r`)
		case c < 0x20 || c >= 0x7f:
			fmt.Fprintf(&s, `\x%02x`, c)
		default:
			s.WriteByte(c)
		}
	}
	s.WriteByte(quote)
	return s.String()
}

func floatText(f float64) string {
	switch {
	case math.IsNaN(f):
		return "nan"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".e") {
		s += ".0"
	}
	return s
}

func (pr *printer) fail(format string, args ...any) {
	if pr.err == nil {
		pr.err = fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
	}
}

func (pr *printer) class(c *Class) {
	pr.begin()
	head := "class " + symbol("", c.Name)
	if c.Extern {
		head = "extern " + head
	}
	if len(c.Bases) > 0 {
		bases := make([]string, len(c.Bases))
		for i, b := range c.Bases {
			bases[i] = symbol("", b.Name)
		}
		head += " : " + strings.Join(bases, ", ")
	}
	pr.linef("%s {", head)
	for _, f := range c.Fields {
		line := fmt.Sprintf("  field %s : %s", part(f.Name), f.Type)
		if f.Owner != nil && f.Owner != c {
			line += " from " + symbol("", f.Owner.Name)
		}
		pr.linef("%s", line)
	}
	for _, m := range c.Methods {
		pr.linef("  method %s -> %s", part(m.Name), pr.funcRef(m.Func))
	}
	pr.linef("}")
}

func (pr *printer) funcRef(f *Function) string {
	if f.Module == nil || f.Module == pr.mod {
		return symbol("", f.Name)
	}
	return symbol(f.Module.Name, f.Name)
}

// symbol is the text of the reference @mod:name, or @name when mod is empty.
func symbol(mod, name string) string {
	if mod == "" {
		return "@" + part(name)
	}
	return "@" + part(mod) + ":" + part(name)
}

// part is name as it appears in text: bare when it only has name characters
// and balanced angle brackets, and quoted otherwise.
func part(name string) string {
	depth := 0
	for i := 0; i < len(name); i++ {
		switch c := name[i]; {
		case c == '<':
			depth++
		case c == '>':
			depth--
		case !isNameByte(c):
			return quote(name)
		}
		if depth < 0 {
			return quote(name)
		}
	}
	if name == "" || depth != 0 {
		return quote(name)
	}
	return name
}

func (pr *printer) linef(format string, args ...any) {
	fmt.Fprintf(&pr.b, format+"\n", args...)
}

func (pr *printer) begin() {
	if pr.started {
		pr.b.WriteByte('\n')
	}
	pr.started = true
}

func isNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '_' || c == '.' || c == '$' || c == '#' || c == '<' || c == '>'
}

// quote is the double-quoted form of s, with the escapes of a Python repr.
func quote(s string) string {
	r := ast.Repr(s)
	if r[0] == '"' {
		return r
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 1; i < len(r)-1; i++ {
		switch c := r[i]; {
		case c == '\\' && r[i+1] == '\'':
			b.WriteByte('\'')
			i++
		case c == '\\':
			b.WriteString(r[i : i+2])
			i++
		case c == '"':
			b.WriteString(`\"`)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func paramList(ps []*Param) string {
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = "%" + p.Name + ": " + p.Typ.String()
	}
	return strings.Join(parts, ", ")
}
