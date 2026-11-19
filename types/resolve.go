package types

import (
	"math/big"

	"github.com/siyul-park/spry/ast"
)

type classHeader struct {
	bases   []Type
	meta    *Named
	kind    ClassKind
	tparams []*TypeParam
	generic bool
}

// implicits creates the implicit type parameters of a function: one for each
// parameter and for the result that has no annotation, named name@func. A
// call binds them, so a function is generic in everything it leaves
// unannotated.
type implicits struct {
	d      *declarer
	module *Module
	fn     string
	on     bool
	list   []*TypeParam
}

func (d *declarer) resolveAll() {
	for i := 0; i < len(d.pending); i++ {
		d.resolveObject(d.pending[i])
	}
	d.inferVariances()
	for _, obj := range d.pending {
		if tn, ok := obj.(*TypeName); ok {
			if a, ok := tn.typ.(*Alias); ok {
				a.Rhs()
			}
		}
	}
}

func (d *declarer) resolveObject(obj Object) {
	switch obj := obj.(type) {
	case *TypeName:
		switch t := obj.typ.(type) {
		case *Named:
			d.ensure(t)
		case *TypeParam:
			d.ensureParam(t)
		case *Alias:
			d.resolveParams(t.tparams)
		}
	case *Func:
		d.resolveFunc(obj)
	case *Builtin:
		d.resolveBuiltin(obj)
	case *Var:
		d.resolveVar(obj)
	}
}

func (d *declarer) resolveBuiltin(b *Builtin) {
	dc := d.decls[b]
	switch {
	case dc == nil:
	case dc.fn != nil:
		d.resolveFunc(dc.fn)
		b.typ = dc.fn.typ
	case dc.ann != nil:
		b.typ = d.typ(dc.ann, dc.scope)
	}
}

func (d *declarer) resolveFunc(f *Func) {
	dc := d.decls[f]
	if dc == nil || f.sig != nil {
		return
	}
	defs := make([]ast.Stmt, len(dc.nodes))
	for i, n := range dc.nodes {
		defs[i] = n.(ast.Stmt)
	}
	var sigs []*Signature
	for _, def := range defs {
		if len(defs) == 1 || hasDecorator(decoratorsOf(def), "overload") {
			sigs = append(sigs, d.signature(def, dc))
		}
	}
	if len(sigs) == 0 {
		sigs = append(sigs, d.signature(defs[0], dc))
	}
	f.sig = sigs[0]
	f.typ = f.sig
	if len(sigs) > 1 {
		f.typ = newOverload(sigs)
	}
	d.decorate(f, defs[0], dc)
}

// decorate sets how f binds from its decorators; a decorator that is not
// known leaves the value of the function as Any.
func (d *declarer) decorate(f *Func, def ast.Stmt, dc *decl) {
	scope := dc.scope
	for _, deco := range decoratorsOf(def) {
		switch obj := d.entity(deco, scope); {
		case obj == Universe.Lookup("staticmethod"):
			f.deco = staticFunc
		case obj == Universe.Lookup("classmethod"):
			f.deco = classFunc
		case obj == Universe.Lookup("property"):
			f.deco = propertyFunc
		case isForm(obj, formOverload, formFinalDecorator):
		case isAccessor(deco):
		default:
			f.typ = Typ[Any]
		}
	}
}

func isForm(obj Object, forms ...formKind) bool {
	b, ok := obj.(*Builtin)
	if !ok {
		return false
	}
	for _, f := range forms {
		if b.form == f {
			return true
		}
	}
	return false
}

func isAccessor(e ast.Expr) bool {
	return hasAccessor([]ast.Expr{e})
}

func (d *declarer) signature(def ast.Stmt, dc *decl) *Signature {
	var (
		name    string
		args    *ast.Arguments
		returns ast.Expr
		tparams []ast.TypeParam
		async   bool
	)
	switch def := def.(type) {
	case *ast.FunctionDef:
		name, args, returns, tparams = def.Name, def.Args, def.Returns, def.TypeParams
	case *ast.AsyncFunctionDef:
		name, args, returns, tparams, async = def.Name, def.Args, def.Returns, def.TypeParams, true
	}
	fb := d.blockOf(def)
	scope := fb.parent.scope
	var tps []*TypeParam
	for _, tp := range tparams {
		if o, ok := d.info.Defs[tp].(*TypeName); ok {
			p := o.typ.(*TypeParam)
			d.ensureParam(p)
			tps = append(tps, p)
		}
	}
	var self Type
	owner := d.classes[dc.scope]
	if owner != nil {
		self = d.selfParam(owner, name, decoratorsOf(def), dc.scope)
	}
	imp := &implicits{d: d, module: dc.src.mod, fn: name, on: !d.prog.stubs || hasBody(def)}
	params := d.paramVars(args, fb, scope, self, imp)
	result := Type(Typ[Any])
	switch {
	case returns != nil:
		result = d.typ(returns, scope)
	case name == "__init__":
		result = builtin("NoneType")
	case !async:
		result = imp.param("return")
	}
	if async {
		result = d.coroutine(result)
	}
	return newSignature(append(tps, imp.list...), NewTuple(params), result)
}

// hasBody reports whether a function of a stub has code of its own: a body
// other than ..., which marks a function the host implements.
func hasBody(def ast.Stmt) bool {
	fd, ok := def.(*ast.FunctionDef)
	if !ok || len(fd.Body) == 0 {
		return false
	}
	return !isEllipsis(fd.Body[len(fd.Body)-1]) && !isDocstring(fd.Body[len(fd.Body)-1]) || len(fd.Body) > 1
}

func (d *declarer) coroutine(result Type) Type {
	if d.prog.stubs {
		return result
	}
	m := shippedModule("collections.abc")
	if m == nil {
		return result
	}
	tn, ok := m.scope.Lookup("Coroutine").(*TypeName)
	if !ok {
		return result
	}
	n, ok := tn.typ.(*Named)
	if !ok || n.tparams.Len() != 3 {
		return result
	}
	return newInstance(n, []Type{Typ[Any], Typ[Any], result})
}

// selfParam is the type of the unannotated first parameter of a method.
func (d *declarer) selfParam(owner *Named, name string, decos []ast.Expr, scope *Scope) Type {
	self := owner.class().self
	for _, deco := range decos {
		switch d.entity(deco, scope) {
		case Universe.Lookup("staticmethod"):
			if name != "__new__" {
				return Typ[Any]
			}
		case Universe.Lookup("classmethod"):
			return NewMeta(self)
		}
	}
	switch name {
	case "__new__", "__init_subclass__", "__class_getitem__":
		return NewMeta(self)
	}
	return self
}

func (d *declarer) paramVars(a *ast.Arguments, fb *block, scope *Scope, self Type, imp *implicits) []*Var {
	var out []*Var
	positional := len(a.PosOnlyArgs) + len(a.Args)
	firstDefault := positional - len(a.Defaults)
	add := func(arg *ast.Arg, kind ParamKind, hasDefault bool) {
		v, ok := d.info.Defs[arg].(*Var)
		if !ok {
			return
		}
		v.paramKind, v.hasDefault = kind, hasDefault
		switch {
		case arg.Annotation != nil:
			v.typ = d.paramType(arg.Annotation, scope)
		case len(out) == 0 && self != nil && kind <= PositionalOrKeyword:
			v.typ = self
		default:
			v.typ = imp.param(arg.Arg)
		}
		out = append(out, v)
	}
	i := 0
	for _, arg := range a.PosOnlyArgs {
		add(arg, PositionalOnly, i >= firstDefault)
		i++
	}
	for _, arg := range a.Args {
		add(arg, PositionalOrKeyword, i >= firstDefault)
		i++
	}
	if a.VarArg != nil {
		add(a.VarArg, VarPositional, false)
	}
	for j, arg := range a.KwOnlyArgs {
		add(arg, KeywordOnly, j < len(a.KwDefaults) && a.KwDefaults[j] != nil)
	}
	if a.KwArg != nil {
		add(a.KwArg, VarKeyword, false)
	}
	return out
}

func (im *implicits) param(name string) Type {
	if !im.on {
		return Typ[Any]
	}
	p := newTypeParam(im.module, name+"@"+im.fn, TypeVar)
	p.implicit = true
	im.list = append(im.list, p)
	return p
}

func (d *declarer) paramType(e ast.Expr, scope *Scope) Type {
	if s, ok := e.(*ast.Starred); ok {
		d.typ(s.Value, scope)
		return Typ[Any]
	}
	return d.typ(e, scope)
}

func (d *declarer) resolveVar(v *Var) {
	dc := d.decls[v]
	if dc == nil || d.vdone[v] {
		return
	}
	d.vdone[v] = true
	if tn := d.legacyAlias(v); tn != nil {
		return
	}
	switch {
	case dc.ann != nil:
		t, quals := d.annotation(dc.ann, d.annotationScope(dc))
		v.quals |= quals
		switch {
		case t != nil:
			v.typ, v.annot = t, true
		case dc.value != nil:
			v.typ = d.valueType(dc.value)
		}
	case dc.value != nil && v.kind != ParamVar:
		v.typ = d.valueType(dc.value)
	}
}

func (d *declarer) annotationScope(dc *decl) *Scope {
	return dc.scope
}

// valueType is the type of a constant assigned to a variable, and Any for
// any other value, which inference refines.
func (d *declarer) valueType(e ast.Expr) Type {
	c, ok := e.(*ast.Constant)
	if !ok {
		return Typ[Any]
	}
	switch c.Value.(type) {
	case ast.Int:
		return builtin("int")
	case ast.Float:
		return builtin("float")
	case ast.Complex:
		return builtin("complex")
	case ast.Str:
		return builtin("str")
	case ast.Bytes:
		return builtin("bytes")
	case ast.Bool:
		return builtin("bool")
	case ast.None:
		return builtin("NoneType")
	}
	return Typ[Any]
}

// legacyAlias turns a variable that is assigned a type expression, a TypeVar
// or a NewType call once, or annotated TypeAlias, into a TypeName, as
// Python's typing treats such a name as the type itself.
func (d *declarer) legacyAlias(v *Var) *TypeName {
	if tn, ok := d.aliased[v]; ok {
		return tn
	}
	d.aliased[v] = nil
	dc := d.decls[v]
	if dc == nil || len(dc.nodes) != 1 || dc.value == nil || dc.scope.Kind() == FunctionScope {
		return nil
	}
	if dc.ann != nil && d.formOfExpr(dc.ann, dc.scope) != formTypeAlias {
		return nil
	}
	tn := d.aliasObject(v, dc)
	if tn == nil {
		return nil
	}
	d.aliased[v] = tn
	dc.scope.replace(tn)
	for _, n := range dc.nodes {
		d.info.Defs[n] = tn
	}
	d.decls[tn] = dc
	return tn
}

func (d *declarer) aliasObject(v *Var, dc *decl) *TypeName {
	if call, ok := dc.value.(*ast.Call); ok {
		return d.callAlias(v, call, dc)
	}
	if dc.ann == nil && !typeLike(dc.value) {
		return nil
	}
	if origin, ok := d.entity(dc.value, dc.scope).(*TypeName); ok && isReference(dc.value) {
		if n, ok := origin.Type().(*Named); ok {
			tn := newTypeName(v.pos, v.module, v.name, n)
			tn.alias = true
			tn.qualname = v.name
			return tn
		}
	}
	t, ok := d.try(func() Type { return d.typ(dc.value, dc.scope) })
	if !ok || isInvalid(t) {
		return nil
	}
	tn := newTypeName(v.pos, v.module, v.name, t)
	tn.alias = true
	tn.qualname = v.name
	return tn
}

func isReference(e ast.Expr) bool {
	switch e.(type) {
	case *ast.Name, *ast.Attribute:
		return true
	}
	return false
}

func typeLike(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.Name, *ast.Attribute, *ast.Subscript:
		return true
	case *ast.BinOp:
		return e.Op == ast.OpBitOr && typeLike(e.Left) && typeLike(e.Right)
	}
	return false
}

func (d *declarer) try(f func() Type) (Type, bool) {
	n := len(*d.errs)
	t := f()
	if len(*d.errs) > n {
		*d.errs = (*d.errs)[:n]
		return t, false
	}
	return t, true
}

func (d *declarer) callAlias(v *Var, call *ast.Call, dc *decl) *TypeName {
	b, ok := d.entity(call.Func, dc.scope).(*Builtin)
	if !ok {
		return nil
	}
	switch b.form {
	case formTypeVar, formParamSpec, formTypeVarTuple:
		return d.typeVarAlias(v, call, b.form, dc)
	case formNewType:
		return d.newTypeAlias(v, call, dc)
	}
	return nil
}

func (d *declarer) typeVarAlias(v *Var, call *ast.Call, form formKind, dc *decl) *TypeName {
	kind := map[formKind]TypeParamKind{formTypeVar: TypeVar, formParamSpec: ParamSpec, formTypeVarTuple: TypeVarTuple}[form]
	p := newTypeParam(v.module, v.name, kind)
	p.obj.pos = v.pos
	if len(call.Args) > 1 && kind == TypeVar {
		for _, a := range call.Args[1:] {
			p.constraints = append(p.constraints, d.typ(a, dc.scope))
		}
	}
	for _, k := range call.Keywords {
		if k.Arg == nil {
			continue
		}
		switch *k.Arg {
		case "bound":
			p.bound = d.typ(k.Value, dc.scope)
		case "default":
			p.def = d.typ(k.Value, dc.scope)
		case "covariant":
			p.variance = Covariant
		case "contravariant":
			p.variance = Contravariant
		}
	}
	d.pstates[p] = resolved
	return p.obj
}

func (d *declarer) newTypeAlias(v *Var, call *ast.Call, dc *decl) *TypeName {
	if len(call.Args) != 2 {
		return nil
	}
	base := d.typ(call.Args[1], dc.scope)
	obj := newTypeName(v.pos, v.module, v.name, nil)
	n := newClass(obj, nil, NewTypeClass, newScope(ClassScope, dc.scope))
	d.states[n] = resolved
	if err := n.class().finish([]Type{base}, nil); err != nil {
		return nil
	}
	return obj
}

// ensure resolves the header of a class declared in this run: its type
// parameters, bases, metaclass and decorators. A class of an earlier run is
// resolved already.
func (d *declarer) ensure(n *Named) {
	n = n.origin
	if d.states[n] != unresolved {
		return
	}
	dc := d.decls[n.obj]
	if dc == nil {
		return
	}
	d.states[n] = resolving
	d.header(n, dc)
	d.states[n] = resolved
}

func (d *declarer) header(n *Named, dc *decl) {
	cd := dc.nodes[0].(*ast.ClassDef)
	cls := n.class()
	d.resolveParams(cls.tparams)
	scope := dc.block.parent.scope
	h := &classHeader{kind: OrdinaryClass}
	for _, be := range cd.Bases {
		d.base(h, cd, be, scope)
	}
	for _, k := range cd.Keywords {
		if k.Arg != nil && *k.Arg == "metaclass" {
			if m, ok := d.typ(k.Value, scope).(*Named); ok {
				h.meta = m
			}
		}
	}
	for _, deco := range cd.DecoratorList {
		switch d.formOfExpr(deco, scope) {
		case formFinalDecorator:
			cls.final = true
		case formRuntimeCheckable:
			cls.checkable = true
		}
	}
	if len(cd.TypeParams) == 0 && len(h.tparams) > 0 {
		setTypeParams(n, h.tparams)
	}
	cls.kind = h.kind
	if err := cls.finish(h.bases, h.meta); err != nil {
		d.errs.Add(d.fset.Position(cd.Pos()), d.fset.Position(cd.End()), "TYP112", err.Error())
		_ = cls.finish(nil, nil)
	}
}

func (d *declarer) base(h *classHeader, cd *ast.ClassDef, be ast.Expr, scope *Scope) {
	expr := be
	var args []ast.Expr
	if sub, ok := be.(*ast.Subscript); ok {
		expr, args = sub.Value, subscriptArgs(sub.Slice)
	}
	switch d.formOfExpr(expr, scope) {
	case formProtocol:
		h.kind = ProtocolClass
		d.legacyParams(h, args, scope)
	case formGeneric:
		d.legacyParams(h, args, scope)
	case formNamedTuple:
		h.bases = append(h.bases, d.namedTupleBase(cd, scope))
	case formTypedDict:
		h.kind = TypedDictClass
		h.bases = append(h.bases, typedDictBase())
	default:
		d.ordinaryBase(h, be, scope)
	}
}

func (d *declarer) ordinaryBase(h *classHeader, be ast.Expr, scope *Scope) {
	t := d.typ(be, scope)
	switch t := t.(type) {
	case *Named:
		d.ensure(t)
		if t.class().kind == TypedDictClass {
			h.kind = TypedDictClass
		}
	case *Tuple:
	default:
		if n, ok := be.(*ast.Name); ok && n.ID == "type" {
			return // a metaclass, rejected by rejectSubset
		}
		if !isInvalid(t) {
			d.fail(be, "TYP140", "invalid base class")
		}
		return
	}
	h.bases = append(h.bases, t)
	if !h.generic {
		h.tparams = appendTypeParams(h.tparams, t)
	}
}

func (d *declarer) resolveParams(list *TypeParamList) {
	for _, tp := range list.slice() {
		d.ensureParam(tp)
	}
}

func (d *declarer) legacyParams(h *classHeader, args []ast.Expr, scope *Scope) {
	h.generic = true
	h.tparams = nil
	for _, a := range args {
		if tp, ok := d.typ(a, scope).(*TypeParam); ok {
			h.tparams = append(h.tparams, tp)
		}
	}
}

func (d *declarer) namedTupleBase(cd *ast.ClassDef, scope *Scope) Type {
	var fields []*Var
	for _, s := range cd.Body {
		if a, ok := s.(*ast.AnnAssign); ok {
			if name, ok := a.Target.(*ast.Name); ok {
				fields = append(fields, NewVar(name.Pos(), name.ID, d.typ(a.Annotation, scope), FieldVar))
			}
		}
	}
	return NewTuple(fields)
}

func (d *declarer) ensureParam(p *TypeParam) {
	if d.pstates[p] != unresolved {
		return
	}
	dc := d.decls[p.obj]
	if dc == nil {
		return
	}
	d.pstates[p] = resolving
	scope := dc.block.scope
	switch tp := dc.nodes[0].(type) {
	case *ast.TypeVar:
		d.bounds(p, tp.Bound, scope)
		p.def = d.optionalType(tp.DefaultValue, scope)
	case *ast.ParamSpec:
		p.def = d.optionalParams(tp.DefaultValue, scope)
	case *ast.TypeVarTuple:
		p.def = d.optionalType(tp.DefaultValue, scope)
	}
	d.pstates[p] = resolved
}

func (d *declarer) bounds(p *TypeParam, bound ast.Expr, scope *Scope) {
	if bound == nil {
		return
	}
	if t, ok := bound.(*ast.Tuple); ok {
		for _, e := range t.Elts {
			p.constraints = append(p.constraints, d.typ(e, scope))
		}
		return
	}
	p.bound = d.typ(bound, scope)
}

func (d *declarer) optionalType(e ast.Expr, scope *Scope) Type {
	switch e := e.(type) {
	case nil:
		return nil
	case *ast.Starred:
		return d.typ(e.Value, scope)
	}
	return d.typ(e, scope)
}

func (d *declarer) optionalParams(e ast.Expr, scope *Scope) Type {
	if e == nil {
		return nil
	}
	return d.params(e, scope)
}

func appendTypeParams(out []*TypeParam, t Type) []*TypeParam {
	switch t := t.(type) {
	case *TypeParam:
		for _, p := range out {
			if p == t {
				return out
			}
		}
		return append(out, t)
	case *Named:
		for _, a := range t.targs.slice() {
			out = appendTypeParams(out, a)
		}
	case *Tuple:
		for _, e := range t.elems() {
			out = appendTypeParams(out, e)
		}
	case *Union:
		for _, e := range t.terms {
			out = appendTypeParams(out, e)
		}
	}
	return out
}

func negate(v ast.Int) *big.Int {
	return new(big.Int).Neg(v.N)
}

func setTypeParams(n *Named, tparams []*TypeParam) {
	list := newTypeParamList(tparams)
	for i, tp := range tparams {
		tp.index = i
	}
	n.tparams, n.origin.tparams, n.cls.tparams = list, list, list
}

// typedDictBase is Mapping[str, object]: a TypedDict is not a dict.
func typedDictBase() Type {
	if m := builtin("Mapping"); m != nil && m.tparams.Len() == 2 {
		return newInstance(m, []Type{builtin("str"), builtin("object")})
	}
	return newInstance(builtin("dict"), []Type{builtin("str"), builtin("object")})
}
