package types

import (
	"bytes"
	"math/big"

	"github.com/siyul-park/spry/ast"
)

type pair struct{ x, y Type }

var protocolSkipped = map[string]bool{
	"__init__": true, "__new__": true, "__init_subclass__": true, "__slots__": true,
	"__doc__": true, "__module__": true, "__class_getitem__": true, "__subclasshook__": true,
}

// Identical reports whether x and y are the same type. Unions are compared
// without regard to term order and recursive aliases coinductively.
func Identical(x, y Type) bool {
	return identical(x, y, map[pair]bool{})
}

// AssignableTo reports whether a value of type v can be used where t is
// expected, following the typing specification: nominal subtyping through
// the MRO, structural subtyping for protocols and typed dicts, covariant
// tuples, contravariant callable parameters, and gradual Any in both
// directions, so the relation is not a partial order.
func AssignableTo(v, t Type) bool {
	return assignable(v, t, map[pair]bool{})
}

// Default returns the type that a variable initialized with a value of type t
// has: the class instance of a Literal, LiteralString or Guard, and t itself
// otherwise.
func Default(t Type) Type {
	switch u := Unalias(t).(type) {
	case *Literal, *Guard, *Basic:
		if n := instanceClass(u); n != nil {
			return n
		}
	case *Union:
		terms := make([]Type, len(u.terms))
		changed := false
		for i, term := range u.terms {
			terms[i] = Default(term)
			changed = changed || terms[i] != term
		}
		if changed {
			return NewUnion(terms...)
		}
	}
	return t
}

func assignable(v, t Type, a map[pair]bool) bool {
	v, t = Unalias(v), Unalias(t)
	switch {
	case identical(v, t, map[pair]bool{}), isAny(v), isAny(t), isInvalid(v), isInvalid(t), isNever(v):
		return true
	case isNever(t):
		return false
	}
	if r, _, ok := cellAssignable(v, t, a); ok {
		return r
	}
	if u, ok := v.(*Union); ok {
		return allAssignable(u.terms, t, a)
	}
	if u, ok := t.(*Union); ok {
		if anyTarget(v, u.terms, a) {
			return true
		}
		if _, ok := v.(*TypeParam); !ok {
			return false
		}
	}
	if p, ok := v.(*TypeParam); ok {
		return typeParamAssignable(p, t, a)
	}
	if isBuiltin(t, "object") {
		return true
	}
	switch t := t.(type) {
	case *Basic:
		l, ok := v.(*Literal)
		return t.kind == LiteralString && ok && isStrLiteral(l)
	case *Meta:
		return metaAssignable(v, t, a)
	case *Tuple:
		return tupleAssignable(v, t, a)
	case *Signature, *Overload:
		return callableAssignable(v, t, a)
	case *Named:
		return namedAssignable(v, t, a)
	}
	return false
}

func allAssignable(vs []Type, t Type, a map[pair]bool) bool {
	for _, v := range vs {
		if !assignable(v, t, a) {
			return false
		}
	}
	return true
}

func anyTarget(v Type, ts []Type, a map[pair]bool) bool {
	for _, t := range ts {
		if assignable(v, t, a) {
			return true
		}
	}
	return false
}

func typeParamAssignable(p *TypeParam, t Type, a map[pair]bool) bool {
	if len(p.constraints) > 0 {
		return allAssignable(p.constraints, t, a)
	}
	if p.bound != nil {
		return assignable(p.bound, t, a)
	}
	return isBuiltin(t, "object")
}

func metaAssignable(v Type, t *Meta, a map[pair]bool) bool {
	switch v := v.(type) {
	case *Meta:
		return assignable(v.elem, t.elem, a)
	case *Named:
		return isBuiltin(v, "type") && (isAny(t.elem) || isBuiltin(t.elem, "object"))
	}
	return false
}

func tupleAssignable(v Type, t *Tuple, a map[pair]bool) bool {
	vt := asTuple(v)
	if vt == nil {
		return false
	}
	if t.unbounded < 0 {
		return vt.unbounded < 0 && vt.Len() == t.Len() && edge(vt, t, t.Len(), t.Len(), nil, nil, false, a)
	}
	pt, st := t.unbounded, t.Len()-t.unbounded-1
	ut := t.vars[pt].typ
	if vt.unbounded < 0 {
		n := vt.Len()
		if n < pt+st {
			return false
		}
		for i := pt; i < n-st; i++ {
			if !assignable(vt.vars[i].typ, ut, a) {
				return false
			}
		}
		return edge(vt, t, pt, pt, nil, nil, false, a) && edge(vt, t, st, st, nil, nil, true, a)
	}
	pv, sv := vt.unbounded, vt.Len()-vt.unbounded-1
	uv := vt.vars[pv].typ
	return pv+sv >= pt+st && assignable(uv, ut, a) && edge(vt, t, pv, pt, uv, ut, false, a) && edge(vt, t, sv, st, uv, ut, true, a)
}

func edge(v, t *Tuple, nv, nt int, uv, ut Type, fromEnd bool, a map[pair]bool) bool {
	for i := range max(nv, nt) {
		from, to := uv, ut
		if i < nv {
			from = elemAt(v, i, fromEnd)
		}
		if i < nt {
			to = elemAt(t, i, fromEnd)
		}
		if !assignable(from, to, a) {
			return false
		}
	}
	return true
}

func namedAssignable(v Type, t *Named, a map[pair]bool) bool {
	if isBuiltin(t, "tuple") {
		return tupleAssignable(v, tupleOfTarget(t), a)
	}
	vn := instanceClass(v)
	if _, ok := v.(*Meta); ok {
		vn = metaclassOf(v.(*Meta))
	}
	if vn != nil {
		if base := asBase(vn, t.origin); base != nil {
			return targsAssignable(base, t, a)
		}
		if promotes(vn, t) {
			return true
		}
	}
	switch t.class().kind {
	case ProtocolClass:
		return protocolAssignable(v, t, a)
	case TypedDictClass:
		return vn != nil && vn.class().kind == TypedDictClass && typedDictAssignable(vn, t, a)
	}
	return false
}

func targsAssignable(v, t *Named, a map[pair]bool) bool {
	if v.targs.Len() == 0 || t.targs.Len() == 0 || v.targs.Len() != t.targs.Len() {
		return true
	}
	for i, tp := range t.TypeParams().slice() {
		x, y := v.targs.At(i), t.targs.At(i)
		ok := false
		switch {
		case tp.kind != TypeVar:
			ok = identical(x, y, map[pair]bool{}) || assignable(x, y, a) && assignable(y, x, a)
		case tp.variance == Covariant:
			ok = assignable(x, y, a)
		case tp.variance == Contravariant:
			ok = assignable(y, x, a)
		default:
			ok = assignable(x, y, a) && assignable(y, x, a)
		}
		if !ok {
			return false
		}
	}
	return true
}

func protocolAssignable(v Type, t *Named, a map[pair]bool) bool {
	k := pair{v, t}
	if a[k] {
		return true
	}
	a[k] = true
	for _, name := range protocolMembers(t) {
		want, got := LookupAttr(t, name), LookupAttr(v, name)
		if want == nil || got == nil || !assignable(got.typ, want.typ, a) {
			return false
		}
	}
	return true
}

func typedDictAssignable(v, t *Named, a map[pair]bool) bool {
	for _, name := range typedDictKeys(t) {
		want := typedDictKey(t, name)
		got := typedDictKey(v, name)
		if got == nil {
			return false
		}
		ro := want.quals&ReadOnly != 0
		switch {
		case ro && !assignable(got.typ, want.typ, a):
			return false
		case !ro && !(assignable(got.typ, want.typ, a) && assignable(want.typ, got.typ, a)):
			return false
		case !(ro && want.quals&NotRequired != 0) && want.quals&NotRequired != got.quals&NotRequired:
			return false
		}
	}
	return true
}

func callableAssignable(v, t Type, a map[pair]bool) bool {
	if o, ok := t.(*Overload); ok {
		for _, s := range o.sigs {
			if !callableAssignable(v, s, a) {
				return false
			}
		}
		return true
	}
	ts := t.(*Signature)
	switch v := v.(type) {
	case *FuncValue:
		return true
	case *Signature:
		return signatureAssignable(v, ts, a)
	case *Overload:
		for _, s := range v.sigs {
			if signatureAssignable(s, ts, a) {
				return true
			}
		}
	case *Meta:
		if c := constructor(v); c != nil {
			return callableAssignable(c, ts, a)
		}
	case *Named:
		if sel := LookupAttr(v, "__call__"); sel != nil && sel.kind == MethodVal {
			return callableAssignable(sel.typ, ts, a)
		}
	}
	return false
}

func signatureAssignable(v, t *Signature, a map[pair]bool) bool {
	if v.tparams.Len() > 0 {
		v = specialize(v, t)
	}
	if !assignable(v.result, t.result, a) {
		return false
	}
	gv, gt := groupParams(v), groupParams(t)
	if isGradual(t) || isGradual(v) {
		return true
	}
	if pv, pt := paramSpecOf(gv), paramSpecOf(gt); pv != nil || pt != nil {
		return pv == pt && len(gv.pos) == len(gt.pos) && contravariant(gt.pos, gv.pos, a)
	}
	return positionalAssignable(gv, gt, a) && keywordAssignable(gv, gt, a)
}

func contravariant(ts, vs []*Var, a map[pair]bool) bool {
	for i := range ts {
		if !assignable(ts[i].typ, vs[i].typ, a) {
			return false
		}
	}
	return true
}

func positionalAssignable(gv, gt paramGroups, a map[pair]bool) bool {
	for i, tp := range gt.pos {
		vp, named := gv.varpos, false
		if i < len(gv.pos) {
			vp, named = gv.pos[i], true
		}
		switch {
		case vp == nil, !assignable(tp.typ, vp.typ, a):
			return false
		case tp.paramKind == PositionalOrKeyword && !acceptsKeyword(vp, tp.name, named, gv):
			return false
		case tp.hasDefault && named && !vp.hasDefault:
			return false
		}
	}
	for _, vp := range gv.pos[min(len(gt.pos), len(gv.pos)):] {
		if !vp.hasDefault || gt.varpos != nil && !assignable(gt.varpos.typ, vp.typ, a) {
			return false
		}
	}
	return gt.varpos == nil || gv.varpos != nil && assignable(gt.varpos.typ, gv.varpos.typ, a)
}

func keywordAssignable(gv, gt paramGroups, a map[pair]bool) bool {
	for _, tk := range gt.kwonly {
		vk := findKeyword(gv, tk.name)
		switch {
		case vk != nil:
			if !assignable(tk.typ, vk.typ, a) || tk.hasDefault && !vk.hasDefault {
				return false
			}
		case gv.varkw == nil || !assignable(tk.typ, gv.varkw.typ, a):
			return false
		}
	}
	for _, vk := range gv.kwonly {
		if findKeyword(gt, vk.name) == nil && !vk.hasDefault {
			return false
		}
	}
	return gt.varkw == nil || gv.varkw != nil && assignable(gt.varkw.typ, gv.varkw.typ, a)
}

func identical(x, y Type, seen map[pair]bool) bool {
	if x == y {
		return true
	}
	if x == nil || y == nil {
		return false
	}
	_, ax := x.(*Alias)
	_, ay := y.(*Alias)
	if ax || ay {
		k := pair{x, y}
		if seen[k] {
			return true
		}
		seen[k] = true
		return identical(Unalias(x), Unalias(y), seen)
	}
	switch x := x.(type) {
	case *Basic:
		y, ok := y.(*Basic)
		return ok && x.kind == y.kind
	case *elemCell:
		y, ok := y.(*elemCell)
		return ok && x.find() == y.find()
	case *FuncValue:
		y, ok := y.(*FuncValue)
		return ok && x.fn == y.fn && x.env == y.env
	case *Named:
		y, ok := y.(*Named)
		return ok && x.origin == y.origin && identicalLists(x.targs.slice(), y.targs.slice(), seen)
	case *Union:
		y, ok := y.(*Union)
		return ok && identicalSets(x.terms, y.terms, seen)
	case *Tuple:
		y, ok := y.(*Tuple)
		return ok && x.Len() == y.Len() && x.unbounded == y.unbounded && identicalLists(x.elems(), y.elems(), seen)
	case *Signature:
		y, ok := y.(*Signature)
		return ok && identicalSignatures(x, y, seen)
	case *Overload:
		y, ok := y.(*Overload)
		return ok && len(x.sigs) == len(y.sigs) && identicalSigLists(x.sigs, y.sigs, seen)
	case *Literal:
		y, ok := y.(*Literal)
		return ok && identicalLiterals(x, y)
	case *Guard:
		y, ok := y.(*Guard)
		return ok && x.strict == y.strict && identical(x.typ, y.typ, seen)
	case *Meta:
		y, ok := y.(*Meta)
		return ok && identical(x.elem, y.elem, seen)
	case *ParamSpecArgs:
		y, ok := y.(*ParamSpecArgs)
		return ok && x.param == y.param && x.kwargs == y.kwargs
	}
	return false
}

func identicalLists(x, y []Type, seen map[pair]bool) bool {
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if !identical(x[i], y[i], seen) {
			return false
		}
	}
	return true
}

func identicalSets(x, y []Type, seen map[pair]bool) bool {
	if len(x) != len(y) {
		return false
	}
	for _, a := range x {
		found := false
		for _, b := range y {
			if identical(a, b, seen) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func identicalSigLists(x, y []*Signature, seen map[pair]bool) bool {
	for i := range x {
		if !identicalSignatures(x[i], y[i], seen) {
			return false
		}
	}
	return true
}

func identicalSignatures(x, y *Signature, seen map[pair]bool) bool {
	if x.tparams.Len() != y.tparams.Len() || x.params.Len() != y.params.Len() {
		return false
	}
	if x.tparams.Len() > 0 {
		rename := make(substMap, y.tparams.Len())
		for i, tp := range y.tparams.slice() {
			rename[tp] = x.tparams.At(i)
		}
		y = subst(&Signature{params: y.params, result: y.result}, rename).(*Signature)
	}
	for i, p := range x.params.slice() {
		q := y.params.At(i)
		if p.paramKind != q.paramKind || p.hasDefault != q.hasDefault ||
			(p.paramKind != PositionalOnly && p.name != q.name) || !identical(p.typ, q.typ, seen) {
			return false
		}
	}
	return identical(x.result, y.result, seen)
}

func identicalLiterals(x, y *Literal) bool {
	if x.member != nil || y.member != nil {
		return x.member == y.member
	}
	switch a := x.value.(type) {
	case ast.Int:
		b, ok := y.value.(ast.Int)
		return ok && intValue(a).Cmp(intValue(b)) == 0
	case ast.Str:
		b, ok := y.value.(ast.Str)
		return ok && a == b
	case ast.Bytes:
		b, ok := y.value.(ast.Bytes)
		return ok && bytes.Equal(a, b)
	case ast.Bool:
		b, ok := y.value.(ast.Bool)
		return ok && a == b
	case ast.None:
		_, ok := y.value.(ast.None)
		return ok
	}
	return false
}

func intValue(i ast.Int) *big.Int {
	if i.N == nil {
		return new(big.Int)
	}
	return i.N
}

func isInvalid(t Type) bool {
	b, ok := t.(*Basic)
	return ok && b.kind == Invalid
}

func isNever(t Type) bool {
	b, ok := t.(*Basic)
	return ok && b.kind == Never
}

func isStrLiteral(l *Literal) bool {
	_, ok := l.value.(ast.Str)
	return ok
}

func elemAt(t *Tuple, i int, fromEnd bool) Type {
	if fromEnd {
		return t.vars[t.Len()-1-i].typ
	}
	return t.vars[i].typ
}

func asTuple(v Type) *Tuple {
	switch v := v.(type) {
	case *Tuple:
		return v
	case *Named:
		return tupleOf(v)
	}
	return nil
}

func tupleOfTarget(t *Named) *Tuple {
	if tup := tupleOf(t); tup != nil {
		return tup
	}
	return elemTuple([]Type{Typ[Any]}, 0)
}

func tupleOf(n *Named) *Tuple {
	for _, e := range resolvedMRO(n) {
		if isBuiltin(e, "tuple") {
			var elem Type = Typ[Any]
			if e.targs.Len() == 1 {
				elem = e.targs.At(0)
			}
			return elemTuple([]Type{elem}, 0)
		}
		for _, b := range e.class().bases {
			if t, ok := Unalias(b).(*Tuple); ok {
				return t
			}
		}
	}
	return nil
}

func metaclassOf(m *Meta) *Named {
	if n, ok := Unalias(m.elem).(*Named); ok && n.class().meta != nil {
		return n.class().meta
	}
	return builtin("type")
}

func promotes(v, t *Named) bool {
	switch {
	case isBuiltin(t, "float"):
		return hasBuiltinBase(v, "int")
	case isBuiltin(t, "complex"):
		return hasBuiltinBase(v, "int") || hasBuiltinBase(v, "float")
	}
	return false
}

func hasBuiltinBase(n *Named, name string) bool {
	for _, e := range n.class().mro {
		if isBuiltin(e, name) {
			return true
		}
	}
	return false
}

func protocolMembers(t *Named) []string {
	var names []string
	seen := map[string]bool{}
	for _, e := range resolvedMRO(t) {
		c := e.class()
		if c.kind != ProtocolClass || c.obj.name == "Protocol" || c.obj.name == "Generic" {
			continue
		}
		for _, name := range c.members.names {
			if !seen[name] && !protocolSkipped[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names
}

func typedDictKeys(t *Named) []string {
	var names []string
	seen := map[string]bool{}
	for _, e := range resolvedMRO(t) {
		if e.class().kind != TypedDictClass {
			continue
		}
		for _, name := range e.class().members.names {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names
}

func typedDictKey(t *Named, name string) *Var {
	for _, e := range resolvedMRO(t) {
		if v, ok := e.class().members.Lookup(name).(*Var); ok && e.class().kind == TypedDictClass {
			return v
		}
	}
	return nil
}

func constructor(m *Meta) Type {
	elem, ok := Unalias(m.elem).(*Named)
	if !ok {
		return nil
	}
	init := LookupAttr(elem, "__init__")
	if init == nil || init.kind != MethodVal {
		return &Signature{params: NewTuple(nil), result: elem}
	}
	switch t := init.typ.(type) {
	case *Signature:
		return &Signature{tparams: t.tparams, params: t.params, result: elem}
	case *Overload:
		sigs := make([]*Signature, len(t.sigs))
		for i, s := range t.sigs {
			sigs[i] = &Signature{tparams: s.tparams, params: s.params, result: elem}
		}
		return newOverload(sigs)
	}
	return nil
}

func specialize(v, t *Signature) *Signature {
	var from, to []Type
	for i, p := range groupParams(v).pos {
		if i < len(groupParams(t).pos) {
			from = append(from, p.typ)
			to = append(to, groupParams(t).pos[i].typ)
		}
	}
	m, err := infer(v.tparams.slice(), from, to)
	if err != nil {
		return v
	}
	return subst(&Signature{params: v.params, result: v.result}, m).(*Signature)
}

func acceptsKeyword(vp *Var, name string, named bool, gv paramGroups) bool {
	if named {
		return vp.paramKind == PositionalOrKeyword && vp.name == name
	}
	return gv.varkw != nil
}

func findKeyword(g paramGroups, name string) *Var {
	for _, p := range g.kwonly {
		if p.name == name {
			return p
		}
	}
	for _, p := range g.pos {
		if p.paramKind == PositionalOrKeyword && p.name == name {
			return p
		}
	}
	return nil
}
