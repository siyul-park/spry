package ssa

import (
	"strings"

	"github.com/siyul-park/spry/types"
)

type externKey struct {
	f       *types.Func
	index   int
	skipped bool
	result  string
}

var (
	tyBytes = Type{Kind: KindBytes}
	tyIntIt = Type{Kind: KindIter, Elems: []Type{tyInt}}
)

// externFunc declares the function of a shipped module that implements a
// call: its parameters are the lowered stub parameters and its name is the
// qualified name of the stub, with the index of the overload. skipFirst
// leaves out the class parameter of a constructor.
func (b *builder) externFunc(f *types.Func, index int, skipFirst bool) *Function {
	return b.externAs(f, index, skipFirst, nil)
}

// externCtor declares a constructor of a class of the standard library that
// returns an instance of the type result.
func (b *builder) externCtor(f *types.Func, index int, result Type) *Function {
	return b.externAs(f, index, true, &result)
}

func (b *builder) externAs(f *types.Func, index int, skipFirst bool, result *Type) *Function {
	key := externKey{f, index, skipFirst, ""}
	if result != nil {
		key.result = result.String()
	}
	if fn := b.externs[key]; fn != nil {
		return fn
	}
	sig := externSignature(f, index)
	name := f.FullName()
	if index >= 0 {
		name += "#" + itoa(index)
	}
	params := sig.Params()
	var ps []*Param
	for i := range params.Len() {
		if i == 0 && skipFirst {
			continue
		}
		p := params.At(i)
		ps = append(ps, &Param{Name: paramName(p.Name()), Typ: b.lowerParam(p)})
	}
	mod := b.stubModule(f.Module())
	res := b.lower(sig.Result())
	if result != nil {
		res = *result
		name = b.unique(mod, name)
	}
	fn := &Function{Name: name, Source: f.FullName(), Module: mod, Params: dedupeParams(ps), Result: res}
	mod.Functions = append(mod.Functions, fn)
	b.externs[key] = fn
	return fn
}

// lowerParam is the IR type of a parameter: *args is a tuple of its items and
// **kwargs a dict of them.
func (b *builder) lowerParam(p *types.Var) Type {
	t := b.lower(p.Type())
	switch p.ParamKind() {
	case types.VarPositional:
		return Type{Kind: KindTuple, Elems: []Type{t}, Variadic: true}
	case types.VarKeyword:
		return Type{Kind: KindDict, Elems: []Type{tyStr, t}}
	}
	return t
}

// stubModule returns the module of a shipped stub, which holds only externs.
func (b *builder) stubModule(m *types.Module) *Module {
	if mod := b.mods[m]; mod != nil {
		return mod
	}
	mod := b.named(m.Name())
	b.mods[m] = mod
	return mod
}

// exception returns the extern function that makes an exception of the
// builtins class, so that the lowering of a failure can raise it. It takes
// the message, or nothing when withMsg is not set.
func (b *builder) exception(name string, withMsg bool) *Function {
	key, fname := name, name
	var params []*Param
	if withMsg {
		params = []*Param{{Name: "msg", Typ: tyStr}}
	} else {
		key, fname = name+"()", name+"$0"
	}
	if fn := b.excs[key]; fn != nil {
		return fn
	}
	cls := b.class(name)
	mod := b.builtins()
	fn := &Function{Name: fname, Source: name, Module: mod, Params: params, Result: Type{Kind: KindClass, Class: cls}}
	mod.Functions = append(mod.Functions, fn)
	b.excs[key] = fn
	return fn
}

// lower maps a type of the checker to the IR type that holds its values:
// scalars stay unboxed, T | None of a reference type is nullable and every
// other union, and everything the builder does not represent yet, is any.
func (b *builder) lower(t types.Type) Type {
	switch t := types.Unalias(types.Default(t)).(type) {
	case *types.Basic:
		switch t.Kind() {
		case types.Never:
			return tyNone
		case types.LiteralString:
			return tyStr
		}
	case *types.Named:
		return b.lowerNamed(t)
	case *types.Tuple:
		return b.lowerTuple(t)
	case *types.Union:
		if ft, ok := b.lowerFuncUnion(t); ok {
			return ft
		}
		return b.lowerUnion(t)
	case *types.FuncValue:
		return b.lowerClosure(t)
	case *types.Meta:
		if n, ok := types.Unalias(t.Elem()).(*types.Named); ok && isUserClass(n) {
			return Type{Kind: KindTypeOf, Class: b.classOf(n)}
		}
	case *types.TypeParam:
		if t.Bound() != nil {
			return b.lower(t.Bound())
		}
	}
	return tyAny
}

func (b *builder) lowerNamed(t *types.Named) Type {
	if isUserClass(t) {
		return classType(b.classOf(t))
	}
	m := t.Obj().Module()
	if m == nil || m.Name() != "builtins" && m.Name() != "collections.abc" {
		return tyAny
	}
	if m.Name() == "builtins" && isExceptionClass(t) {
		return classType(b.class(t.Obj().Name()))
	}
	var targs []Type
	for i := range t.TypeArgs().Len() {
		targs = append(targs, b.lower(t.TypeArgs().At(i)))
	}
	if m.Name() == "collections.abc" {
		if t.Obj().Name() == "Iterator" && len(targs) == 1 {
			return Type{Kind: KindIter, Elems: targs}
		}
		if t.Obj().Name() == "Generator" && len(targs) == 3 {
			return Type{Kind: KindGen, Elems: targs}
		}
		return tyAny
	}
	switch name := t.Obj().Name(); {
	case name == "int":
		return tyInt
	case name == "bool":
		return tyI1
	case name == "float":
		return tyF64
	case name == "str":
		return tyStr
	case name == "bytes":
		return tyBytes
	case name == "NoneType":
		return tyNone
	case name == "range":
		return tyIntIt
	case name == "enumerate" && len(targs) == 1:
		return Type{Kind: KindIter, Elems: []Type{{Kind: KindTuple, Elems: []Type{tyInt, targs[0]}}}}
	case (name == "zip" || name == "reversed" || name == "map" || name == "filter") && len(targs) == 1:
		return Type{Kind: KindIter, Elems: targs}
	case (name == "list" || name == "set") && len(targs) == 1:
		kind := KindList
		if name == "set" {
			kind = KindSet
		}
		return Type{Kind: kind, Elems: targs}
	case name == "dict" && len(targs) == 2:
		return Type{Kind: KindDict, Elems: targs}
	case name == "tuple" && len(targs) == 1:
		return Type{Kind: KindTuple, Elems: targs, Variadic: true}
	}
	return tyAny
}

func (b *builder) lowerTuple(t *types.Tuple) Type {
	var elems []Type // nil when empty: the form Parse reads back
	for i := 0; i < t.Len(); i++ {
		elems = append(elems, b.lower(t.At(i).Type()))
	}
	switch u := t.Unbounded(); {
	case u < 0:
		return Type{Kind: KindTuple, Elems: elems}
	case u == 0 && len(elems) == 1:
		return Type{Kind: KindTuple, Elems: elems, Variadic: true}
	}
	return tyAny
}

func (b *builder) lowerUnion(t *types.Union) Type {
	if t.Len() != 2 {
		return tyAny
	}
	for i := range 2 {
		if !isNoneType(t.Term(i)) {
			continue
		}
		other := b.lower(t.Term(1 - i))
		if other.Kind.info().ref && !other.Nullable {
			other.Nullable = true
			return other
		}
	}
	return tyAny
}

// class returns the extern class of a builtin type. An exception class has
// its bases and the fields of BaseException.
func (b *builder) class(name string) *Class {
	if c := b.classes[name]; c != nil {
		return c
	}
	c := &Class{Name: name, Extern: true}
	b.classes[name] = c
	b.prog.Classes = append(b.prog.Classes, c)
	if n := builtinException(name); n != nil {
		b.completeExtern(c, n)
	}
	return c
}

// completeExtern gives the extern class c of the exception n its bases and
// the fields they and it declare.
func (b *builder) completeExtern(c *Class, n *types.Named) {
	cls := n.Underlying().(*types.Class)
	for _, base := range cls.Bases() {
		bn, ok := types.Unalias(base).(*types.Named)
		if !ok || bn.Obj().Name() == "object" {
			continue
		}
		bc := b.class(bn.Obj().Name())
		c.Bases = append(c.Bases, bc)
		for _, fld := range bc.Fields {
			if _, have := c.field(fld.Name); !have {
				c.Fields = append(c.Fields, fld)
			}
		}
	}
	for _, v := range cls.Fields() {
		c.Fields = append(c.Fields, Field{Name: v.Name(), Type: b.lower(v.Type()), Owner: c})
	}
}

func isNoneType(t types.Type) bool {
	n, ok := t.(*types.Named)
	return ok && n.Obj().Name() == "NoneType"
}

// builtinException returns the exception class of the builtins that has the
// name, or nil.
func builtinException(name string) *types.Named {
	tn, ok := types.Universe.Lookup(name).(*types.TypeName)
	if !ok {
		return nil
	}
	if n, ok := tn.Type().(*types.Named); ok && isExceptionClass(n) {
		return n
	}
	return nil
}

// isExceptionClass reports whether n derives from BaseException.
func isExceptionClass(n *types.Named) bool {
	cls, ok := n.Underlying().(*types.Class)
	if !ok {
		return false
	}
	for _, m := range cls.MRO() {
		if m.Obj().Name() == "BaseException" && m.Obj().Module() != nil && m.Obj().Module().Name() == "builtins" {
			return true
		}
	}
	return false
}

func (b *builder) builtins() *Module {
	return b.named("builtins")
}

// named returns the stub module of that name, creating it.
func (b *builder) named(name string) *Module {
	if mod := b.stubs[name]; mod != nil {
		return mod
	}
	mod := &Module{Name: name}
	b.stubs[name] = mod
	b.prog.Modules = append(b.prog.Modules, mod)
	return mod
}

func externSignature(f *types.Func, index int) *types.Signature {
	if o, ok := f.Type().(*types.Overload); ok && index >= 0 {
		return o.At(index)
	}
	if sig, ok := f.Type().(*types.Signature); ok {
		return sig
	}
	return f.Signature()
}

// paramName makes a Python parameter name a valid IR name that cannot clash
// with the names given to anonymous parameters.
func paramName(name string) string {
	var sb strings.Builder
	for _, r := range name {
		if r < 128 && (r == '_' || r == '$' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			sb.WriteRune(r)
		} else {
			sb.WriteByte('_')
		}
	}
	out := sb.String()
	if out == "" {
		return ""
	}
	if out[0] == 'p' && strings.Trim(out[1:], "0123456789") == "" {
		return "p_" + out[1:]
	}
	if out[0] >= '0' && out[0] <= '9' {
		return "_" + out
	}
	return out
}

func dedupeParams(ps []*Param) []*Param {
	seen := map[string]bool{}
	for _, p := range ps {
		for p.Name != "" && seen[p.Name] {
			p.Name += "_"
		}
		seen[p.Name] = true
	}
	return ps
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for ; n > 0; n /= 10 {
		d = append([]byte{byte('0' + n%10)}, d...)
	}
	return string(d)
}
