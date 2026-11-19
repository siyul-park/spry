package types

import (
	"fmt"
	"strings"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/parser"
	"github.com/siyul-park/spry/token"
)

var qualifierBits = map[formKind]Qualifiers{
	formFinal: Final, formClassVar: 0, formReadOnly: ReadOnly, formRequired: 0,
	formNotRequired: NotRequired, formInitVar: InitVar,
}

// annotation evaluates the annotation of a variable, separating the type
// qualifiers from the type. The type is nil for a bare Final or ClassVar.
func (d *declarer) annotation(e ast.Expr, scope *Scope) (Type, Qualifiers) {
	var quals Qualifiers
	for {
		sub, ok := e.(*ast.Subscript)
		var base ast.Expr = e
		if ok {
			base = sub.Value
		}
		form := d.formOfExpr(base, scope)
		bit, isQual := qualifierBits[form]
		switch {
		case form == formAnnotated && ok:
			e = subscriptArgs(sub.Slice)[0]
		case isQual && ok:
			quals |= bit
			e = subscriptArgs(sub.Slice)[0]
		case isQual:
			return nil, quals | bit
		default:
			return d.typ(e, scope), quals
		}
	}
}

// typ evaluates an annotation or type expression in scope, reporting a TYP140
// error and returning Invalid when it is not a type.
func (d *declarer) typ(e ast.Expr, scope *Scope) Type {
	t := d.eval(e, scope)
	d.info.Types[e] = TypeAndValue{Type: t, mode: modeType}
	return t
}

func (d *declarer) eval(e ast.Expr, scope *Scope) Type {
	switch e := e.(type) {
	case *ast.Constant:
		return d.constant(e, scope)
	case *ast.Name, *ast.Attribute:
		return d.named(e, scope)
	case *ast.BinOp:
		if e.Op == ast.OpBitOr {
			return NewUnion(d.typ(e.Left, scope), d.typ(e.Right, scope))
		}
	case *ast.Subscript:
		return d.subscript(e, scope)
	}
	return d.fail(e, "TYP140", "invalid type expression")
}

func (d *declarer) constant(c *ast.Constant, scope *Scope) Type {
	switch v := c.Value.(type) {
	case ast.None:
		return builtin("NoneType")
	case ast.Str:
		return d.forward(c, string(v), scope)
	}
	return d.fail(c, "TYP140", "invalid type expression")
}

func (d *declarer) forward(at ast.Node, text string, scope *Scope) Type {
	expr, err := parser.ParseExpr(d.fset, "<annotation>", strings.NewReader(strings.TrimSpace(text)))
	if err != nil {
		return d.fail(at, "TYP140", "invalid forward reference %q", text)
	}
	return d.typ(expr.Body, scope)
}

func (d *declarer) subscript(e *ast.Subscript, scope *Scope) Type {
	args := subscriptArgs(e.Slice)
	switch obj := d.entity(e.Value, scope).(type) {
	case *Builtin:
		return d.form(e, obj.form, args, scope)
	case *Var:
		if tn := d.legacyAlias(obj); tn != nil {
			return d.generic(e, tn, args, scope)
		}
	case *TypeName:
		return d.generic(e, obj, args, scope)
	}
	return d.fail(e, "TYP140", "this is not a generic type")
}

func (d *declarer) generic(e *ast.Subscript, tn *TypeName, args []ast.Expr, scope *Scope) Type {
	switch t := tn.Type().(type) {
	case *Named:
		d.ensure(t)
		switch {
		case isBuiltin(t, "tuple"):
			return d.tuple(args, scope)
		case isBuiltin(t, "type") && len(args) == 1:
			return NewMeta(d.typ(args[0], scope))
		}
		return d.instantiate(e, t, d.typeArgs(t.tparams, args, scope))
	case *Alias:
		return d.instantiate(e, t, d.typeArgs(t.tparams, args, scope))
	}
	return d.fail(e, "TYP140", "%s is not generic", tn.name)
}

func (d *declarer) typeArgs(tparams *TypeParamList, args []ast.Expr, scope *Scope) []Type {
	out := make([]Type, 0, len(args))
	for i, a := range args {
		if i < tparams.Len() && tparams.At(i).kind == ParamSpec {
			out = append(out, d.params(a, scope))
			continue
		}
		out = append(out, d.typeArg(a, scope))
	}
	return out
}

func (d *declarer) typeArg(a ast.Expr, scope *Scope) Type {
	if s, ok := a.(*ast.Starred); ok {
		return d.typ(s.Value, scope)
	}
	return d.typ(a, scope)
}

// params evaluates the parameter list of a ParamSpec argument: [A, B], ...,
// or a ParamSpec.
func (d *declarer) params(a ast.Expr, scope *Scope) Type {
	switch a := a.(type) {
	case *ast.List:
		vars := make([]*Var, len(a.Elts))
		for i, el := range a.Elts {
			vars[i] = NewParam(token.NoPos, "", d.typ(el, scope), PositionalOnly)
		}
		return NewTuple(vars)
	case *ast.Constant:
		if _, ok := a.Value.(ast.Ellipsis); ok {
			return gradualParams()
		}
	}
	return d.typ(a, scope)
}

func (d *declarer) tuple(args []ast.Expr, scope *Scope) Type {
	if len(args) == 1 {
		if t, ok := args[0].(*ast.Tuple); ok && len(t.Elts) == 0 {
			return elemTuple(nil, -1)
		}
	}
	var elems []Type
	unbounded := -1
	for i, a := range args {
		switch a := a.(type) {
		case *ast.Constant:
			if _, ok := a.Value.(ast.Ellipsis); ok && i == 1 && len(args) == 2 {
				return elemTuple(elems, 0)
			}
		case *ast.Starred:
			inner := d.typ(a.Value, scope)
			if tup, ok := inner.(*Tuple); ok {
				if tup.unbounded >= 0 {
					unbounded = len(elems) + tup.unbounded
				}
				elems = append(elems, tup.elems()...)
				continue
			}
			unbounded = len(elems)
			elems = append(elems, inner)
			continue
		}
		elems = append(elems, d.typ(a, scope))
	}
	return elemTuple(elems, unbounded)
}

func (d *declarer) form(e *ast.Subscript, form formKind, args []ast.Expr, scope *Scope) Type {
	switch form {
	case formOptional:
		return NewUnion(d.typ(args[0], scope), builtin("NoneType"))
	case formUnion:
		terms := make([]Type, len(args))
		for i, a := range args {
			terms[i] = d.typ(a, scope)
		}
		return NewUnion(terms...)
	case formLiteral:
		return d.literals(args, scope)
	case formAnnotated:
		return d.typ(args[0], scope)
	case formCallable:
		return d.callable(e, args, scope)
	case formTypeGuard:
		return newGuard(d.typ(args[0], scope), false)
	case formTypeIs:
		return newGuard(d.typ(args[0], scope), true)
	case formUnpack:
		return d.typ(args[0], scope)
	case formFinal, formClassVar, formReadOnly, formRequired, formNotRequired, formInitVar:
		return d.fail(e, "TYP140", "a qualifier is not valid in a type argument")
	}
	return d.fail(e, "TYP140", "this form takes no type arguments")
}

func (d *declarer) literals(args []ast.Expr, scope *Scope) Type {
	terms := make([]Type, 0, len(args))
	for _, a := range args {
		terms = append(terms, d.literal(a, scope))
	}
	return NewUnion(terms...)
}

func (d *declarer) literal(a ast.Expr, scope *Scope) Type {
	switch a := a.(type) {
	case *ast.Constant:
		switch v := a.Value.(type) {
		case ast.None:
			return builtin("NoneType")
		case ast.Int, ast.Str, ast.Bytes, ast.Bool:
			return NewLiteral(v)
		}
	case *ast.UnaryOp:
		if c, ok := a.Operand.(*ast.Constant); ok && a.Op == ast.UnaryUSub {
			if v, ok := c.Value.(ast.Int); ok {
				return NewLiteral(ast.Int{N: negate(v)})
			}
		}
	case *ast.Attribute:
		if v, ok := d.entity(a, scope).(*Var); ok {
			return newMemberLiteral(v)
		}
	case *ast.Subscript:
		return d.typ(a, scope)
	}
	return d.fail(a, "TYP140", "invalid Literal value")
}

func (d *declarer) callable(e *ast.Subscript, args []ast.Expr, scope *Scope) Type {
	if len(args) != 2 {
		return d.fail(e, "TYP140", "Callable takes a parameter list and a result type")
	}
	result := d.typ(args[1], scope)
	var params *Tuple
	switch p := args[0].(type) {
	case *ast.List, *ast.Constant:
		params, _ = d.params(p, scope).(*Tuple)
	default:
		params = d.concatenated(p, scope)
	}
	if params == nil {
		return d.fail(args[0], "TYP140", "invalid Callable parameter list")
	}
	return NewSignature(params, result)
}

// concatenated evaluates a ParamSpec, or Concatenate[A, B, P], as the
// parameter list of a callable.
func (d *declarer) concatenated(e ast.Expr, scope *Scope) *Tuple {
	var lead []ast.Expr
	last := e
	if sub, ok := e.(*ast.Subscript); ok && d.formOfExpr(sub.Value, scope) == formConcatenate {
		items := subscriptArgs(sub.Slice)
		lead, last = items[:len(items)-1], items[len(items)-1]
	}
	ps, ok := d.typ(last, scope).(*TypeParam)
	if !ok || ps.kind != ParamSpec {
		return nil
	}
	vars := make([]*Var, 0, len(lead)+2)
	for _, a := range lead {
		vars = append(vars, NewParam(token.NoPos, "", d.typ(a, scope), PositionalOnly))
	}
	vars = append(vars,
		NewParam(token.NoPos, "args", newParamSpecArgs(ps, false), VarPositional),
		NewParam(token.NoPos, "kwargs", newParamSpecArgs(ps, true), VarKeyword))
	return NewTuple(vars)
}

func (d *declarer) formOfExpr(e ast.Expr, scope *Scope) formKind {
	switch e.(type) {
	case *ast.Name, *ast.Attribute:
		if b, ok := d.entity(e, scope).(*Builtin); ok {
			return b.form
		}
	}
	return formNone
}

func (d *declarer) named(e ast.Expr, scope *Scope) Type {
	if a, ok := e.(*ast.Attribute); ok {
		if t := d.paramSpecArgs(a, scope); t != nil {
			return t
		}
	}
	obj := d.entity(e, scope)
	switch obj := obj.(type) {
	case nil:
		return d.fail(e, "TYP140", "unknown name in type expression")
	case *TypeName:
		return d.typeNameType(obj)
	case *Var:
		if tn := d.legacyAlias(obj); tn != nil {
			return d.typeNameType(tn)
		}
		return d.fail(e, "TYP140", "variable %s is not a type", obj.name)
	case *Builtin:
		return d.bareForm(e, obj, scope)
	}
	return d.fail(e, "TYP140", "%s is not a type", obj.Name())
}

// paramSpecArgs evaluates P.args and P.kwargs.
func (d *declarer) paramSpecArgs(a *ast.Attribute, scope *Scope) Type {
	if a.Attr != "args" && a.Attr != "kwargs" {
		return nil
	}
	tn, ok := d.entity(a.Value, scope).(*TypeName)
	if !ok {
		return nil
	}
	if p, ok := tn.typ.(*TypeParam); ok && p.kind == ParamSpec {
		return newParamSpecArgs(p, a.Attr == "kwargs")
	}
	return nil
}

func (d *declarer) bareForm(e ast.Expr, b *Builtin, scope *Scope) Type {
	switch b.form {
	case formAny:
		if src := d.owner[scope]; src != nil && !src.stub && !d.prog.stubs {
			return annotatedAny
		}
		return Typ[Any]
	case formNever:
		return Typ[Never]
	case formLiteralString:
		return Typ[LiteralString]
	case formSelf:
		return d.selfType(e, scope)
	case formCallable:
		return NewSignature(gradualParams(), Typ[Any])
	}
	if n := d.hidden[b]; n != nil {
		return d.typeNameType(n.obj)
	}
	return d.fail(e, "TYP140", "%s is not valid here", b.name)
}

// entity resolves a name or a chain of attributes to the object it denotes.
func (d *declarer) entity(e ast.Expr, scope *Scope) Object {
	switch e := e.(type) {
	case *ast.Name:
		_, obj := scope.LookupParent(e.ID)
		if obj != nil {
			d.info.Uses[e] = obj
		}
		return obj
	case *ast.Attribute:
		return d.member(d.entity(e.Value, scope), e.Attr)
	}
	return nil
}

func (d *declarer) member(base Object, name string) Object {
	switch b := base.(type) {
	case *ModuleName:
		if b.imported == nil {
			return nil
		}
		if obj := b.imported.scope.Lookup(name); obj != nil {
			return obj
		}
		if s := d.prog.mods[b.imported.name+"."+name]; s != nil {
			return newModuleName(token.NoPos, name, s.mod)
		}
	case *TypeName:
		if n, ok := b.typ.(*Named); ok {
			return n.class().members.Lookup(name)
		}
	}
	return nil
}

func (d *declarer) typeNameType(tn *TypeName) Type {
	switch t := tn.Type().(type) {
	case *Named:
		d.ensure(t)
		return d.bare(t)
	case *TypeParam:
		d.ensureParam(t)
		return t
	case nil:
		return Typ[Invalid]
	default:
		return t
	}
}

// bare gives a generic class used without arguments its default arguments:
// Any, except for the parameters that have a PEP 696 default.
func (d *declarer) bare(n *Named) Type {
	switch {
	case n.targs != nil:
		return n
	case isBuiltin(n, "tuple"):
		return elemTuple([]Type{Typ[Any]}, 0)
	case isBuiltin(n, "type"):
		return NewMeta(Typ[Any])
	case n.tparams.Len() == 0:
		return n
	}
	args := make([]Type, n.tparams.Len())
	m := substMap{}
	for i, tp := range n.tparams.slice() {
		d.ensureParam(tp)
		switch {
		case tp.def != nil:
			args[i] = subst(tp.def, m)
		case tp.kind == ParamSpec:
			args[i] = gradualParams()
		case tp.kind == TypeVarTuple:
			args[i] = elemTuple([]Type{Typ[Any]}, 0)
		default:
			args[i] = Typ[Any]
		}
		m[tp] = args[i]
	}
	return newInstance(n, args)
}

func gradualParams() *Tuple {
	return NewTuple([]*Var{
		NewParam(token.NoPos, "args", Typ[Any], VarPositional),
		NewParam(token.NoPos, "kwargs", Typ[Any], VarKeyword),
	})
}

func (d *declarer) selfType(e ast.Expr, scope *Scope) Type {
	for s := scope; s != nil; s = s.parent {
		if n := d.classes[s]; n != nil {
			return n.class().self
		}
	}
	return d.fail(e, "TYP140", "Self is only valid in a class")
}

func (d *declarer) instantiate(e ast.Expr, orig Type, targs []Type) Type {
	t, err := Instantiate(orig, targs)
	if err != nil {
		return d.fail(e, "TYP140", "%v", err)
	}
	d.info.Instances[e] = Instance{TypeArgs: newTypeList(targs), Type: t}
	return t
}

func (d *declarer) fail(n ast.Node, code, format string, args ...any) Type {
	d.errs.Add(d.fset.Position(n.Pos()), d.fset.Position(n.End()), code, fmt.Sprintf(format, args...))
	return Typ[Invalid]
}

func subscriptArgs(slice ast.Expr) []ast.Expr {
	if t, ok := slice.(*ast.Tuple); ok {
		return t.Elts
	}
	return []ast.Expr{slice}
}
