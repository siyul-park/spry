package ssa

import "strings"

// Type is an IR type. Elems holds the element type of list, set, cell and
// iter, K and V of dict, Y, S and R of gen, the fields of tuple, and for func
// the parameter types followed by the result type. Class names the class of
// KindClass and KindTypeOf. Variadic marks tuple<T...>, whose only element
// type repeats. Nullable adds None to a reference type.
type Type struct {
	Kind     Kind
	Elems    []Type
	Class    *Class
	Nullable bool
	Variadic bool
}

// Kind selects the shape of a Type.
type Kind uint8

type kindInfo struct {
	name  string
	elems int // number of Elems, or -1 for any number
	ref   bool
}

// Type kinds. KindInvalid is the zero Kind and stands for the absence of a
// value.
const (
	KindInvalid Kind = iota
	KindI1
	KindI32
	KindInt
	KindF64
	KindStr
	KindBytes
	KindNone
	KindAny
	KindList
	KindDict
	KindSet
	KindTuple
	KindClass
	KindFunc
	KindCell
	KindGen
	KindIter
	KindTypeOf
)

var kinds = [...]kindInfo{
	KindI1:     {name: "i1"},
	KindI32:    {name: "i32"},
	KindInt:    {name: "int"},
	KindF64:    {name: "f64"},
	KindStr:    {name: "str", ref: true},
	KindBytes:  {name: "bytes", ref: true},
	KindNone:   {name: "none"},
	KindAny:    {name: "any"},
	KindList:   {name: "list", elems: 1, ref: true},
	KindDict:   {name: "dict", elems: 2, ref: true},
	KindSet:    {name: "set", elems: 1, ref: true},
	KindTuple:  {name: "tuple", elems: -1, ref: true},
	KindClass:  {name: "class", ref: true},
	KindFunc:   {name: "func", elems: -1, ref: true},
	KindCell:   {name: "cell", elems: 1, ref: true},
	KindGen:    {name: "gen", elems: 3, ref: true},
	KindIter:   {name: "iter", elems: 1, ref: true},
	KindTypeOf: {name: "type", ref: true},
}

// String returns the text form of t, which Parse reads back.
func (t Type) String() string {
	var b strings.Builder
	t.write(&b)
	return b.String()
}

func (t Type) isNum() bool {
	return !t.Nullable && (t.Kind == KindInt || t.Kind == KindF64)
}

func (t Type) same(u Type) bool {
	if t.Kind != u.Kind || t.Class != u.Class || t.Nullable != u.Nullable || t.Variadic != u.Variadic || len(t.Elems) != len(u.Elems) {
		return false
	}
	for i := range t.Elems {
		if !t.Elems[i].same(u.Elems[i]) {
			return false
		}
	}
	return true
}

func (t Type) write(b *strings.Builder) {
	paren := t.Kind == KindFunc && t.Nullable
	if paren {
		b.WriteByte('(')
	}
	switch t.Kind {
	case KindClass:
		b.WriteString(symbol("", t.className()))
	case KindTypeOf:
		b.WriteString("type<" + symbol("", t.className()) + ">")
	case KindFunc:
		t.writeFunc(b)
	case KindTuple:
		t.writeTuple(b)
	default:
		b.WriteString(t.Kind.info().name)
		t.writeElems(b, t.Elems)
	}
	if paren {
		b.WriteByte(')')
	}
	if t.Nullable {
		b.WriteByte('?')
	}
}

func (t Type) writeTuple(b *strings.Builder) {
	if !t.Variadic || len(t.Elems) != 1 {
		b.WriteString("tuple")
		if len(t.Elems) == 0 {
			b.WriteString("<>")
		}
		t.writeElems(b, t.Elems)
		return
	}
	e := t.Elems[0]
	b.WriteString("tuple<")
	if e.Kind == KindClass && !e.Nullable {
		b.WriteString("@" + quote(e.className()))
	} else {
		e.write(b)
	}
	b.WriteString("...>")
}

func (t Type) writeElems(b *strings.Builder, elems []Type) {
	if len(elems) == 0 {
		return
	}
	b.WriteByte('<')
	for i, e := range elems {
		if i > 0 {
			b.WriteByte(',')
		}
		e.write(b)
	}
	b.WriteByte('>')
}

func (t Type) writeFunc(b *strings.Builder) {
	b.WriteString("func(")
	n := max(len(t.Elems)-1, 0)
	for i, p := range t.Elems[:n] {
		if i > 0 {
			b.WriteByte(',')
		}
		p.write(b)
	}
	b.WriteString(") -> ")
	t.at(n).write(b)
}

func (t Type) at(i int) Type {
	if i < 0 || i >= len(t.Elems) {
		return Type{}
	}
	return t.Elems[i]
}

func (t Type) className() string {
	if t.Class == nil {
		return ""
	}
	return t.Class.Name
}

func (k Kind) info() kindInfo {
	if int(k) < len(kinds) {
		return kinds[k]
	}
	return kindInfo{}
}
