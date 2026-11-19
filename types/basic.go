package types

// Basic is a type with no structure: Invalid, Any, Never or LiteralString.
type Basic struct {
	kind BasicKind
	name string
	why  string // for an Any made by a fallback site: its reason
}

// BasicKind identifies a Basic type.
type BasicKind uint8

// Basic kinds, indexes into Typ.
const (
	Invalid BasicKind = iota
	Any
	Never
	LiteralString
)

// Two Any values remember where they came from, so that an operation on them
// is reported with the right reason: an explicit Any annotation opts out of
// the report, and a type widened past a cap reports cap-exceeded.
var (
	annotatedAny = &Basic{Any, "typing.Any", ""}
	cappedAny    = anyFor(reasonCapExceeded)
)

// Typ holds the Basic types indexed by BasicKind.
var Typ = []*Basic{
	Invalid:       {Invalid, "<invalid>", ""},
	Any:           {Any, "typing.Any", ""},
	Never:         {Never, "typing.Never", ""},
	LiteralString: {LiteralString, "typing.LiteralString", ""},
}

var reasonAny = map[string]*Basic{}

// Kind returns the kind of b.
func (b *Basic) Kind() BasicKind {
	return b.kind
}

// Name returns the qualified name of b.
func (b *Basic) Name() string {
	return b.name
}

// Underlying returns the str class for LiteralString and b itself otherwise.
func (b *Basic) Underlying() Type {
	if b.kind == LiteralString {
		return underlyingOf(builtin("str"), b)
	}
	return b
}

// String implements Type.
func (b *Basic) String() string {
	return TypeString(b, nil)
}

// anyFor is the Any that a site of the given reason produces: an operation on
// it reports the same reason.
func anyFor(reason string) *Basic {
	if b := reasonAny[reason]; b != nil {
		return b
	}
	b := &Basic{Any, "typing.Any", reason}
	reasonAny[reason] = b
	return b
}
