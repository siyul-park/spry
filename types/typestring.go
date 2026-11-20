package types

import (
	"fmt"
	"strings"

	"github.com/siyul-park/spry/ast"
)

// Qualifier returns the prefix that qualifies the names of classes declared
// in module m. An empty result leaves the name unqualified.
type Qualifier func(*Module) string

type printer struct {
	qf   Qualifier
	b    strings.Builder
	open map[*elemCell]bool
}

// TypeString returns the string of t as typing's annotation printer writes it
// (typing._type_repr), with class names qualified by qf. A nil qf qualifies
// every class with its module name except for builtins.
func TypeString(t Type, qf Qualifier) string {
	if qf == nil {
		qf = qualifier
	}
	p := &printer{qf: qf}
	p.typ(t)
	return p.b.String()
}

// RelativeTo returns a Qualifier that leaves the classes of m unqualified.
func RelativeTo(m *Module) Qualifier {
	return func(o *Module) string {
		if o == m {
			return ""
		}
		return qualifier(o)
	}
}

func (p *printer) typ(t Type) {
	switch t := t.(type) {
	case nil:
		p.b.WriteString("<nil>")
	case *Basic:
		p.b.WriteString(t.name)
	case *elemCell:
		p.cell(t)
	case *FuncValue:
		p.b.WriteString("<function " + t.fn.obj.fullName + ">")
	case *Named:
		p.named(t)
	case *Class:
		p.named(t.obj.typ.(*Named))
	case *Union:
		p.union(t)
	case *Tuple:
		p.tuple(t)
	case *Signature:
		if t.tparams.Len() > 0 || !p.callable(t) {
			p.inspect(t)
		}
	case *Overload:
		p.overload(t)
	case *Literal:
		p.literals([]*Literal{t})
	case *Guard:
		p.guard(t)
	case *Meta:
		p.b.WriteString("type[")
		p.typ(t.elem)
		p.b.WriteByte(']')
	case *TypeParam:
		p.typeParam(t)
	case *ParamSpecArgs:
		p.b.WriteString(t.param.obj.name)
		if t.kwargs {
			p.b.WriteString(".kwargs")
		} else {
			p.b.WriteString(".args")
		}
	case *Alias:
		p.b.WriteString(t.Obj().name)
		p.targs(t.TypeParams(), t.targs)
	case *Module:
		p.b.WriteString("<module " + ast.Repr(t.name) + ">")
	}
}

func (p *printer) named(n *Named) {
	obj := n.Obj()
	if isBuiltin(n, "NoneType") {
		p.b.WriteString("None")
		return
	}
	if prefix := p.qf(obj.module); prefix != "" {
		p.b.WriteString(prefix + ".")
	}
	p.b.WriteString(obj.qualname)
	p.targs(n.TypeParams(), n.targs)
}

func (p *printer) targs(tparams *TypeParamList, targs *TypeList) {
	if targs.Len() == 0 {
		return
	}
	p.b.WriteByte('[')
	for i := range targs.Len() {
		if i > 0 {
			p.b.WriteString(", ")
		}
		arg := targs.At(i)
		tup, packed := arg.(*Tuple)
		switch {
		case !packed || i >= tparams.Len():
			p.typ(arg)
		case tparams.At(i).kind == ParamSpec:
			p.paramList(tup)
		case tparams.At(i).kind == TypeVarTuple && tup.Len() == 0:
			p.b.WriteString("*tuple[()]")
		case tparams.At(i).kind == TypeVarTuple:
			p.elems(tup, false)
		default:
			p.typ(arg)
		}
	}
	p.b.WriteByte(']')
}

func (p *printer) union(u *Union) {
	for i := 0; i < len(u.terms); i++ {
		if i > 0 {
			p.b.WriteString(" | ")
		}
		var run []*Literal
		for ; i < len(u.terms); i++ {
			l, ok := u.terms[i].(*Literal)
			if !ok {
				break
			}
			run = append(run, l)
		}
		if len(run) > 0 {
			p.literals(run)
			i--
			continue
		}
		p.typ(u.terms[i])
	}
}

func (p *printer) literals(lits []*Literal) {
	p.b.WriteString("typing.Literal[")
	for i, l := range lits {
		if i > 0 {
			p.b.WriteString(", ")
		}
		if l.member != nil {
			p.typ(l.member.typ)
			p.b.WriteString("." + l.member.name)
			continue
		}
		p.b.WriteString(reprValue(l.value))
	}
	p.b.WriteByte(']')
}

func (p *printer) guard(g *Guard) {
	if g.strict {
		p.b.WriteString("typing.TypeIs[")
	} else {
		p.b.WriteString("typing.TypeGuard[")
	}
	p.typ(g.typ)
	p.b.WriteByte(']')
}

func (p *printer) tuple(t *Tuple) {
	p.b.WriteString("tuple[")
	if t.Len() == 0 {
		p.b.WriteString("()")
	} else {
		p.elems(t, true)
	}
	p.b.WriteByte(']')
}

func (p *printer) elems(t *Tuple, inTuple bool) {
	for i, v := range t.slice() {
		if i > 0 {
			p.b.WriteString(", ")
		}
		if i != t.unbounded {
			p.typ(v.typ)
			continue
		}
		if tp, ok := v.typ.(*TypeParam); ok && tp.kind == TypeVarTuple {
			p.b.WriteString("typing.Unpack[")
			p.typ(v.typ)
			p.b.WriteByte(']')
			continue
		}
		if inTuple && t.Len() == 1 {
			p.typ(v.typ)
			p.b.WriteString(", ...")
			continue
		}
		p.b.WriteString("*tuple[")
		p.typ(v.typ)
		p.b.WriteString(", ...]")
	}
}

func (p *printer) paramList(t *Tuple) {
	if isGradual(&Signature{params: t}) {
		p.b.WriteString("...")
		return
	}
	p.b.WriteByte('[')
	for i, v := range t.slice() {
		if i > 0 {
			p.b.WriteString(", ")
		}
		p.typ(v.typ)
	}
	p.b.WriteByte(']')
}

func (p *printer) callable(s *Signature) bool {
	g := groupParams(s)
	ps := paramSpecOf(g)
	switch {
	case isGradual(s):
		p.b.WriteString("collections.abc.Callable[...")
	case ps != nil && allPlain(g.pos):
		p.b.WriteString("collections.abc.Callable[")
		if len(g.pos) == 0 {
			p.typ(ps)
			break
		}
		p.b.WriteString("typing.Concatenate[")
		for _, v := range g.pos {
			p.typ(v.typ)
			p.b.WriteString(", ")
		}
		p.typ(ps)
		p.b.WriteByte(']')
	case g.varpos == nil && g.varkw == nil && len(g.kwonly) == 0 && allPlain(g.pos):
		p.b.WriteString("collections.abc.Callable[[")
		for i, v := range g.pos {
			if i > 0 {
				p.b.WriteString(", ")
			}
			p.typ(v.typ)
		}
		p.b.WriteByte(']')
	default:
		return false
	}
	p.b.WriteString(", ")
	p.typ(s.result)
	p.b.WriteByte(']')
	return true
}

func (p *printer) inspect(s *Signature) {
	if s.tparams.Len() > 0 {
		p.b.WriteByte('[')
		for i, tp := range s.tparams.slice() {
			if i > 0 {
				p.b.WriteString(", ")
			}
			p.b.WriteString(map[TypeParamKind]string{TypeVar: "", ParamSpec: "**", TypeVarTuple: "*"}[tp.kind] + tp.obj.name)
		}
		p.b.WriteByte(']')
	}
	p.b.WriteByte('(')
	params := s.params.slice()
	hasVarPos := groupParams(s).varpos != nil
	for i, v := range params {
		if i > 0 {
			p.b.WriteString(", ")
		}
		if v.paramKind == KeywordOnly && !hasVarPos && (i == 0 || params[i-1].paramKind != KeywordOnly) {
			p.b.WriteString("*, ")
		}
		p.param(v)
		if v.paramKind == PositionalOnly && (i == len(params)-1 || params[i+1].paramKind != PositionalOnly) {
			p.b.WriteString(", /")
		}
	}
	p.b.WriteString(") -> ")
	p.typ(s.result)
}

func (p *printer) param(v *Var) {
	switch v.paramKind {
	case VarPositional:
		p.b.WriteByte('*')
	case VarKeyword:
		p.b.WriteString("**")
	}
	p.b.WriteString(v.name + ": ")
	p.typ(v.typ)
	if v.hasDefault {
		p.b.WriteString(" = ...")
	}
}

func (p *printer) overload(o *Overload) {
	p.b.WriteString("Overload[")
	for i, s := range o.sigs {
		if i > 0 {
			p.b.WriteString(", ")
		}
		p.inspect(s)
	}
	p.b.WriteByte(']')
}

// cell prints what a site holds, and ... for a site inside itself.
func (p *printer) cell(c *elemCell) {
	r := c.find()
	if p.open[r] {
		p.b.WriteString("...")
		return
	}
	if p.open == nil {
		p.open = map[*elemCell]bool{}
	}
	p.open[r] = true
	p.typ(r.cell)
	delete(p.open, r)
}

func (p *printer) typeParam(t *TypeParam) {
	switch {
	case t.isSelf:
		p.b.WriteString("typing.Self")
		return
	case t.kind != TypeVarTuple && !t.inferred && !t.implicit:
		p.b.WriteString([]string{"~", "+", "-"}[t.variance])
	}
	p.b.WriteString(t.obj.name)
}

func reprValue(v ast.Value) string {
	switch v := v.(type) {
	case ast.Int:
		if v.N == nil {
			return "0"
		}
		return v.N.String()
	case ast.Str:
		return ast.Repr(string(v))
	case ast.Bytes:
		return reprBytes(v)
	case ast.Bool:
		if v {
			return "True"
		}
		return "False"
	}
	return "None"
}

func reprBytes(b ast.Bytes) string {
	quote := byte('\'')
	if strings.IndexByte(string(b), '\'') >= 0 && strings.IndexByte(string(b), '"') < 0 {
		quote = '"'
	}
	var sb strings.Builder
	sb.WriteString("b")
	sb.WriteByte(quote)
	for _, c := range b {
		switch {
		case c == quote || c == '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		case c == '\t':
			sb.WriteString(`\t`)
		case c == '\n':
			sb.WriteString(`\n`)
		case c == '\r':
			sb.WriteString(`\r`)
		case c < 0x20 || c >= 0x7f:
			fmt.Fprintf(&sb, `\x%02x`, c)
		default:
			sb.WriteByte(c)
		}
	}
	sb.WriteByte(quote)
	return sb.String()
}

func allPlain(params []*Var) bool {
	for _, v := range params {
		if v.paramKind != PositionalOnly || v.hasDefault {
			return false
		}
	}
	return true
}

func qualifier(m *Module) string {
	if m == nil || m.name == "builtins" {
		return ""
	}
	return m.name
}
