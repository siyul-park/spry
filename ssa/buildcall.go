package ssa

import (
	"math/big"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/types"
)

func (f *fbuild) call(e *ast.Call) Value {
	want := f.irType(e)
	if n, ok := e.Func.(*ast.Name); ok && !f.calleeBound(n) {
		return f.undef(want)
	}
	c := f.spec.Calls[e]
	switch {
	case c == nil:
		return f.undef(want)
	case c.Kind == types.Dynamic:
		return f.conv(f.dynCall(e), want)
	case c.Kind == types.CallClosure:
		return f.callValue(e, c)
	case c.Slot == "__init__" && f.isUserCtor(e):
		v, _ := f.userConstruct(e, c)
		return f.conv(v, want)
	case f.isUserCall(c) && f.isMethodCall(e):
		return f.conv(f.methodCall(e, e.Func.(*ast.Attribute), c), want)
	case c.Target != nil:
		return f.conv(f.callUser(e, c), want)
	case c.Slot == "isinstance":
		return f.isinstance(e)
	case c.Slot == "__new__" || c.Slot == "__init__":
		return f.conv(f.construct(e, c), want)
	}
	return f.conv(f.callExtern(e, c), want)
}

// calleeBound checks that the name of a callee is bound where it is called,
// and raises the error of Python if it is not.
func (f *fbuild) calleeBound(n *ast.Name) bool {
	obj := f.b.info.Uses[n]
	if obj == nil {
		if f.b.info.Defined[n] == types.Undefined {
			f.raise("NameError", "name '"+n.ID+"' is not defined")
			return false
		}
		return true
	}
	return f.guard(n, obj)
}

// dynCall calls a value of type any. A callee the builder cannot make a value
// of, such as a function name, raises the TypeError that Python would.
func (f *fbuild) dynCall(e *ast.Call) Value {
	callee := f.expr(e.Func)
	args := make([]Value, 0, len(e.Args))
	for _, a := range e.Args {
		args = append(args, f.conv(f.expr(a), tyAny))
	}
	if _, isConst := callee.(*Const); isConst {
		f.raise("TypeError", "the call does not match a signature")
		return f.undef(tyAny)
	}
	in := f.emit(OpDynCall, tyAny, append([]Value{f.conv(callee, tyAny)}, args...)...)
	if in == nil {
		return f.undef(tyAny)
	}
	return in
}

func (f *fbuild) callUser(e *ast.Call, c *types.Call) Value {
	target := f.b.funcs[c.Target]
	if target == nil {
		return f.undef(tyAny)
	}
	callee := c.Target.Func
	sig := callee.Signature()
	params := make([]*types.Var, sig.Params().Len())
	ptypes := make([]Type, len(params))
	for i := range params {
		params[i] = sig.Params().At(i)
		ptypes[i] = target.Params[i].Typ
	}
	args := f.bindArgs(e, params, ptypes, func(i int) Value {
		return f.defaultArg(callee, params[i], ptypes[i])
	})
	return f.callResolved(c, args)
}

// callExtern calls a function or method of a shipped module.
func (f *fbuild) callExtern(e *ast.Call, c *types.Call) Value {
	if c.Func == nil {
		return f.undef(tyAny)
	}
	if v, ok := f.intrinsic(e, c); ok {
		return v
	}
	if attr, ok := e.Func.(*ast.Attribute); ok && eqMethods[c.Func.Name()] && f.b.info.Selections[attr] != nil && c.Func.Module().Name() == "builtins" {
		if t := f.irType(attr.Value); f.userEq(t) {
			return f.dynContainerCall(e, attr, c.Func.Name())
		}
	}
	fn := f.b.externFunc(c.Func, c.Overload, false)
	sig := externSignature(c.Func, c.Overload)
	var recv []Value
	first := 0
	if attr, ok := e.Func.(*ast.Attribute); ok && f.b.info.Selections[attr] != nil {
		recv = []Value{f.conv(f.expr(attr.Value), fn.Params[0].Typ)}
		first = 1
	}
	params := make([]*types.Var, 0, sig.Params().Len())
	for i := first; i < sig.Params().Len(); i++ {
		params = append(params, sig.Params().At(i))
	}
	ptypes := make([]Type, len(params))
	for i := range params {
		ptypes[i] = fn.Params[i+first].Typ
	}
	if eagerConsumers[c.Func.Name()] {
		f.eager++
		defer func() { f.eager-- }()
	}
	args := f.bindArgs(e, params, ptypes, func(i int) Value {
		return f.defaultArg(c.Func, params[i], ptypes[i])
	})
	return f.callFn(fn, append(recv, args...))
}

// intrinsic lowers the calls that are an instruction rather than a call.
func (f *fbuild) intrinsic(e *ast.Call, c *types.Call) (Value, bool) {
	if attr, ok := e.Func.(*ast.Attribute); ok && c.Func.Name() == "append" && len(e.Args) == 1 && len(e.Keywords) == 0 && f.b.info.Selections[attr] != nil {
		if t := f.irType(attr.Value); t.Kind == KindList && !t.Nullable && c.Func.Module().Name() == "builtins" {
			recv := f.expr(attr.Value)
			f.emit(OpAppend, Type{}, recv, f.conv(f.expr(e.Args[0]), t.Elems[0]))
			return noneConst(), true
		}
	}
	if attr, ok := e.Func.(*ast.Attribute); ok && c.Func.Name() == "send" && len(e.Args) == 1 && f.b.info.Selections[attr] != nil {
		if g := f.irType(attr.Value); g.Kind == KindGen && !g.Nullable {
			return f.genSend(e, attr), true
		}
	}
	if c.Func.Name() == "next" && len(e.Args) >= 1 && len(e.Args) <= 2 && len(e.Keywords) == 0 && c.Func.Module().Name() == "builtins" {
		if g := f.typeOf(e.Args[0]); f.b.lower(g).Kind == KindGen && !f.b.lower(g).Nullable {
			return f.genNext(e, f.expr(e.Args[0])), true
		}
	}
	if c.Func.Name() == "len" && len(e.Args) == 1 && len(e.Keywords) == 0 && c.Func.Module().Name() == "builtins" {
		switch t := f.irType(e.Args[0]); {
		case t.Nullable:
		case t.Kind == KindStr || t.Kind == KindBytes || t.Kind == KindList || t.Kind == KindDict || t.Kind == KindSet || t.Kind == KindTuple:
			return f.val(OpLen, tyInt, f.expr(e.Args[0])), true
		}
	}
	return nil, false
}

// construct lowers a call of a class of the standard library.
func (f *fbuild) construct(e *ast.Call, c *types.Call) Value {
	meta, ok := f.typeOf(e.Func).(*types.Meta)
	if !ok {
		return f.undef(f.irType(e))
	}
	name := "tuple"
	if n, ok := meta.Elem().(*types.Named); ok {
		name = n.Obj().Name()
	} else if _, ok := meta.Elem().(*types.Tuple); !ok {
		return f.undef(f.irType(e))
	}
	switch name {
	case "list", "set", "tuple", "frozenset", "dict":
		return f.containerCtor(e, name)
	}
	if len(e.Keywords) == 0 {
		switch name {
		case "range", "int", "float", "bool", "str":
			vals := make([]Value, len(e.Args))
			for i, a := range e.Args {
				vals[i] = f.expr(a)
			}
			if name == "range" {
				return f.rangeOf(vals)
			}
			if v, ok := f.scalarCtor(name, vals, e.Args); ok {
				return v
			}
			return f.externCtor(e, c, vals)
		}
	}
	return f.externCtor(e, c, nil)
}

// externCtor calls the constructor of a class of the standard library, with
// the arguments evaluated already when done is set.
func (f *fbuild) externCtor(e *ast.Call, c *types.Call, done []Value) Value {
	want := f.irType(e)
	if c.Func == nil {
		return f.undef(want)
	}
	fn := f.b.externCtor(c.Func, c.Overload, want)
	if done != nil {
		return f.callFn(fn, done)
	}
	sig := externSignature(c.Func, c.Overload)
	params := make([]*types.Var, 0, sig.Params().Len())
	for i := 1; i < sig.Params().Len(); i++ {
		params = append(params, sig.Params().At(i))
	}
	ptypes := make([]Type, len(params))
	for i := range params {
		ptypes[i] = fn.Params[i].Typ
	}
	args := f.bindArgs(e, params, ptypes, func(i int) Value {
		return f.defaultArg(c.Func, params[i], ptypes[i])
	})
	return f.callFn(fn, args)
}

// bindArgs evaluates the arguments of e in source order and arranges them as
// the parameters take them: extra positionals in the *args tuple, extra
// keywords in the **kwargs dict, missing arguments from their defaults.
func (f *fbuild) bindArgs(e *ast.Call, params []*types.Var, ptypes []Type, dflt func(i int) Value) []Value {
	vals := make([]Value, len(params))
	var extra []Value
	var kwNames []string
	var kwVals []Value
	next := 0
	for _, a := range e.Args {
		v := f.expr(a)
		for next < len(params) && params[next].ParamKind() > types.PositionalOrKeyword {
			break
		}
		if next < len(params) && params[next].ParamKind() <= types.PositionalOrKeyword {
			vals[next] = f.conv(v, ptypes[next])
			next++
			continue
		}
		extra = append(extra, v)
	}
	for _, k := range e.Keywords {
		v := f.expr(k.Value)
		i := keywordParam(params, *k.Arg)
		if i < 0 {
			kwNames, kwVals = append(kwNames, *k.Arg), append(kwVals, v)
			continue
		}
		vals[i] = f.conv(v, ptypes[i])
	}
	for i, p := range params {
		switch {
		case p.ParamKind() == types.VarPositional:
			vals[i] = f.packTuple(extra, ptypes[i])
		case p.ParamKind() == types.VarKeyword:
			vals[i] = f.packDict(kwNames, kwVals, ptypes[i])
		case vals[i] == nil:
			vals[i] = dflt(i)
		}
	}
	return vals
}

func keywordParam(params []*types.Var, name string) int {
	for i, p := range params {
		if p.Name() == name && (p.ParamKind() == types.PositionalOrKeyword || p.ParamKind() == types.KeywordOnly) {
			return i
		}
	}
	return -1
}

func (f *fbuild) packTuple(vals []Value, t Type) Value {
	if t.Kind != KindTuple {
		return f.conv(f.val(OpNewTuple, Type{Kind: KindTuple, Elems: []Type{tyAny}, Variadic: true}, f.convAll(vals, tyAny)...), t)
	}
	args := make([]Value, len(vals))
	for i, v := range vals {
		args[i] = f.conv(v, t.Elems[min(i, len(t.Elems)-1)])
	}
	return f.val(OpNewTuple, t, args...)
}

func (f *fbuild) packDict(names []string, vals []Value, t Type) Value {
	if t.Kind != KindDict {
		return f.undef(t)
	}
	var args []Value
	for i, n := range names {
		args = append(args, &Const{Value: ast.Str(n), Typ: tyStr}, f.conv(vals[i], t.Elems[1]))
	}
	return f.val(OpNewDict, t, args...)
}

func (f *fbuild) convAll(vals []Value, t Type) []Value {
	out := make([]Value, len(vals))
	for i, v := range vals {
		out[i] = f.conv(v, t)
	}
	return out
}

// defaultArg is the value of a parameter the call leaves out: a constant
// default inline, any other one from the global where the definition stored
// it.
func (f *fbuild) defaultArg(callee *types.Func, p *types.Var, t Type) Value {
	e := defaultExpr(callee, p.Name())
	if e == nil {
		return f.undef(t)
	}
	if v, ok := f.constExpr(e); ok {
		return f.conv(v, t)
	}
	if callee.Module() == nil || f.b.mods[callee.Module()] == nil {
		return f.undef(t)
	}
	return f.conv(f.load(f.b.defaultGlobal(callee, p, t)), t)
}

// constExpr evaluates a literal constant, possibly negated.
func (f *fbuild) constExpr(e ast.Expr) (Value, bool) {
	switch e := e.(type) {
	case *ast.Constant:
		if _, ok := f.constant(e).(*Const); ok && !f.constant(e).(*Const).Undef {
			return f.constant(e), true
		}
	case *ast.UnaryOp:
		c, ok := e.Operand.(*ast.Constant)
		if !ok || e.Op != ast.UnaryUSub {
			return nil, false
		}
		switch v := c.Value.(type) {
		case ast.Int:
			return &Const{Value: ast.Int{N: new(big.Int).Neg(v.N)}, Typ: tyInt}, true
		case ast.Float:
			return &Const{Value: ast.Float(-float64(v)), Typ: tyF64}, true
		}
	}
	return nil, false
}

// containerCtor lowers list(...), set(...), tuple(...), frozenset(...) and
// dict(...): the empty container, a copy of a list, a dict of keywords, and
// otherwise a call that converts its argument.
func (f *fbuild) containerCtor(e *ast.Call, name string) Value {
	want := f.irType(e)
	if len(e.Keywords) > 0 {
		return f.dictOfKeywords(e, want)
	}
	vals := make([]Value, len(e.Args))
	for i, a := range e.Args {
		vals[i] = f.expr(a)
	}
	if len(vals) == 1 && TypeOf(vals[0]).Kind == KindGen && !TypeOf(vals[0]).Nullable {
		return f.fromGenerator(name, vals[0], want)
	}
	empty := map[string]Op{"list": OpNewList, "set": OpNewSet, "dict": OpNewDict, "tuple": OpNewTuple}
	switch {
	case len(vals) == 0 && empty[name] != 0 && want.Kind != KindAny:
		return f.val(empty[name], want)
	case len(vals) == 1 && name == "list" && TypeOf(vals[0]).same(want):
		return f.val(OpSlice, want, vals[0], noneConst(), noneConst(), noneConst())
	case len(vals) == 1:
		fn := f.b.helper(name, []Type{tyAny}, want)
		return f.callFn(fn, vals)
	}
	return f.undef(want)
}

// fromGenerator lowers list(g), set(g), tuple(g) and frozenset(g): the items
// of the generator are collected in a list first.
func (f *fbuild) fromGenerator(name string, g Value, want Type) Value {
	items := f.drain(g)
	switch {
	case name == "list":
		return f.conv(items, want)
	case name == "set" && want.Kind == KindSet:
		set := f.val(OpNewSet, want)
		f.forEach(nil, nil, items, want.Elems[0], func(cur Value) {
			f.callFn(f.b.setAdd(want), []Value{set, cur})
		})
		return set
	}
	return f.callFn(f.b.helper(name, []Type{tyAny}, want), []Value{items})
}

// dictOfKeywords lowers dict(a=1, b=2).
func (f *fbuild) dictOfKeywords(e *ast.Call, want Type) Value {
	if want.Kind != KindDict || len(e.Args) > 0 {
		return f.undef(want)
	}
	var vals []Value
	for _, k := range e.Keywords {
		if k.Arg == nil {
			return f.undef(want)
		}
		vals = append(vals, &Const{Value: ast.Str(*k.Arg), Typ: tyStr}, f.conv(f.expr(k.Value), want.Elems[1]))
	}
	return f.val(OpNewDict, want, vals...)
}

// scalarCtor lowers int(x), float(x), bool(x) and str(x) on the scalar types
// that convert by an instruction.
func (f *fbuild) scalarCtor(name string, vals []Value, args []ast.Expr) (Value, bool) {
	if len(vals) == 0 {
		return map[string]Value{
			"int": intConst(0), "float": &Const{Value: ast.Float(0), Typ: tyF64},
			"bool": boolConst(false), "str": &Const{Value: ast.Str(""), Typ: tyStr},
		}[name], true
	}
	if len(vals) != 1 {
		return nil, false
	}
	v := vals[0]
	t := TypeOf(v)
	scalar := t.same(tyI1) || t.same(tyInt) || t.same(tyF64)
	switch {
	case name == "int" && t.same(tyF64):
		return f.val(OpF642Int, tyInt, v), true
	case name == "int" && scalar:
		return f.conv(v, tyInt), true
	case name == "float" && scalar:
		return f.conv(v, tyF64), true
	case name == "bool":
		return f.truth(args[0], v), true
	case name == "str" && t.same(tyStr):
		return v, true
	case name == "str" && scalar:
		return f.val(OpFormat, tyStr, v, &Const{Value: ast.Str(""), Typ: tyStr}), true
	}
	return nil, false
}

// rangeOf builds the iterator of range(...): the extern @builtins:range takes
// start, stop and step.
func (f *fbuild) rangeOf(vals []Value) Value {
	var start, stop, step Value = intConst(0), nil, intConst(1)
	switch len(vals) {
	case 1:
		stop = vals[0]
	case 2:
		start, stop = vals[0], vals[1]
	case 3:
		start, stop, step = vals[0], vals[1], vals[2]
	default:
		return f.undef(tyIntIt)
	}
	return f.callFn(f.b.rangeFunc(), []Value{start, stop, step})
}

func (b *builder) rangeFunc() *Function {
	if b.rng == nil {
		mod := b.builtins()
		ps := []*Param{{Name: "start", Typ: tyInt}, {Name: "stop", Typ: tyInt}, {Name: "step", Typ: tyInt}}
		b.rng = &Function{Name: "range", Source: "range", Module: mod, Params: ps, Result: tyIntIt}
		mod.Functions = append(mod.Functions, b.rng)
	}
	return b.rng
}

// isinstance folds the test where the type of the value decides it and is an
// instruction on a value of unknown type.
func (f *fbuild) isinstance(e *ast.Call) Value {
	v := f.expr(e.Args[0])
	f.expr(e.Args[1])
	at := f.typeOf(e.Args[0])
	var classes []types.Type
	switch t := f.typeOf(e.Args[1]).(type) {
	case *types.Meta:
		classes = []types.Type{t.Elem()}
	case *types.Tuple:
		for i := range t.Len() {
			if m, ok := t.At(i).Type().(*types.Meta); ok {
				classes = append(classes, m.Elem())
			}
		}
	}
	_, isUnion := at.(*types.Union)
	_, isAny := at.(*types.Basic)
	if !isUnion && !isAny {
		for _, c := range classes {
			if types.AssignableTo(at, c) {
				return boolConst(true)
			}
		}
		return boolConst(false)
	}
	if len(classes) != 1 {
		return boolConst(false)
	}
	in := f.emit(OpIsInstance, tyI1, f.conv(v, tyAny))
	if in == nil {
		return boolConst(false)
	}
	in.Class = f.b.class(types.TypeString(classes[0], nil))
	return in
}

// isUserCtor reports whether e calls a class of the program.
func (f *fbuild) isUserCtor(e *ast.Call) bool {
	meta, ok := f.typeOf(e.Func).(*types.Meta)
	if !ok {
		return false
	}
	n, ok := meta.Elem().(*types.Named)
	return ok && isUserClass(n)
}

// isMethodCall reports whether e calls an attribute the checker resolved to a
// method or a super method.
func (f *fbuild) isMethodCall(e *ast.Call) bool {
	attr, ok := e.Func.(*ast.Attribute)
	return ok && f.b.info.Selections[attr] != nil && f.b.info.Selections[attr].Kind() != types.MethodExpr
}

// defaultExpr finds the default value of the parameter named name in the
// definition of f.
func defaultExpr(f *types.Func, name string) ast.Expr {
	def, ok := f.Decl().(*ast.FunctionDef)
	if !ok {
		return nil
	}
	a := def.Args
	positional := append(append([]*ast.Arg{}, a.PosOnlyArgs...), a.Args...)
	for i, d := range a.Defaults {
		if positional[len(positional)-len(a.Defaults)+i].Arg == name {
			return d
		}
	}
	for i, d := range a.KwDefaults {
		if d != nil && a.KwOnlyArgs[i].Arg == name {
			return d
		}
	}
	return nil
}
