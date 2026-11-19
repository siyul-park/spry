package ast

//go:generate go run gen_tables.go

import (
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/siyul-park/spry/token"
)

// DumpOption configures Dump.
type DumpOption func(*dumper)

type dumper struct {
	fset       *token.FileSet
	indent     *string
	attributes bool
	empty      bool
	noNames    bool
}

var spanType = reflect.TypeFor[Span]()

// classNames holds the CPython class names that differ from the Go names.
var classNames = map[string]string{
	"ExprStmt": "Expr", "Alias": "alias", "Arg": "arg", "Arguments": "arguments",
	"Comprehension": "comprehension", "Keyword": "keyword", "MatchCase": "match_case", "WithItem": "withitem",
}

// Dump formats the tree rooted at node as CPython 3.14's ast.dump does, with
// annotate_fields=True, include_attributes=False, indent=None and
// show_empty=False unless options say otherwise. fset locates the node
// positions for WithAttributes and may be nil without it. Operators and
// expression contexts print as their singleton classes, for example Add().
func Dump(fset *token.FileSet, node Node, opts ...DumpOption) string {
	d := &dumper{fset: fset}
	for _, opt := range opts {
		opt(d)
	}
	s, _ := d.format(reflect.ValueOf(node), 0)
	return s
}

// Repr is the repr() of the Python str s, given as WTF-8 as the Str constants
// are. Dump uses it for string constants; the command line uses it to print
// token text.
func Repr(s string) string {
	runes := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		r, size := decodeRune(s[i:])
		runes = append(runes, r)
		i += size
	}
	quote := quoteFor(strings.ContainsRune(s, '\''), strings.ContainsRune(s, '"'))
	var b strings.Builder
	b.WriteByte(quote)
	for _, r := range runes {
		switch {
		case r == rune(quote) || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x7f && !isEscaped(byte(r)):
			b.WriteRune(r)
		case r >= 0x7f && unicode.Is(printable, r):
			b.WriteRune(r)
		default:
			b.WriteString(escapeRune(r))
		}
	}
	b.WriteByte(quote)
	return b.String()
}

// WithIndent selects the multi-line form of ast.dump, indenting each level
// with indent. ast.dump's integer indent n is strings.Repeat(" ", n).
func WithIndent(indent string) DumpOption {
	return func(d *dumper) { d.indent = &indent }
}

// WithAttributes adds lineno, col_offset, end_lineno and end_col_offset
// (include_attributes=True). It needs the FileSet passed to Dump.
func WithAttributes() DumpOption {
	return func(d *dumper) { d.attributes = true }
}

// WithEmpty prints empty lists (show_empty=True). A field that is absent
// (None) is omitted regardless, as in CPython.
func WithEmpty() DumpOption {
	return func(d *dumper) { d.empty = true }
}

// WithoutFieldNames prints unambiguous leading fields without their names
// (annotate_fields=False).
func WithoutFieldNames() DumpOption {
	return func(d *dumper) { d.noNames = true }
}

// format returns the text of v and whether it is "simple": a scalar, an
// empty list or a node without arguments, which lets a parent stay on one line.
func (d *dumper) format(v reflect.Value, level int) (string, bool) {
	if val, ok := v.Interface().(Value); ok {
		return repr(val), true
	}
	switch x := v.Interface().(type) {
	case Operator:
		return x.String() + "()", true
	case UnaryOperator:
		return x.String() + "()", true
	case CmpOperator:
		return x.String() + "()", true
	case BoolOperator:
		return x.String() + "()", true
	case ExprContext:
		return x.String() + "()", true
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return "None", true
		}
		return d.format(v.Elem(), level)
	case reflect.Struct:
		return d.node(v, level)
	case reflect.Slice:
		return d.list(v, level)
	case reflect.String:
		return repr(Str(v.String())), true
	default:
		return strconv.FormatInt(v.Int(), 10), true
	}
}

func (d *dumper) list(v reflect.Value, level int) (string, bool) {
	if v.Len() == 0 {
		return "[]", true
	}
	prefix, sep, level := d.layout(level)
	items := make([]string, v.Len())
	for i := range items {
		items[i], _ = d.format(v.Index(i), level)
	}
	return "[" + prefix + strings.Join(items, sep) + "]", false
}

// node follows the field loop of ast.dump: absent fields are skipped and make
// later fields named; with show_empty off, empty lists are skipped and
// buffered so that they print only when a later field prints unnamed.
func (d *dumper) node(v reflect.Value, level int) (string, bool) {
	prefix, sep, level := d.layout(level)
	t := v.Type()
	keywords := !d.noNames
	allSimple := true
	var args, buffer []string
	var span *Span
	for i := range t.NumField() {
		field := t.Field(i)
		fv := v.Field(i)
		if field.Type == spanType {
			s := fv.Interface().(Span)
			span = &s
			continue
		}
		if (fv.Kind() == reflect.Interface || fv.Kind() == reflect.Pointer) && fv.IsNil() {
			keywords = true
			continue
		}
		if !d.empty {
			if fv.Kind() == reflect.Slice && fv.Len() == 0 {
				if !keywords {
					buffer = append(buffer, "[]")
				}
				continue
			}
			if !keywords {
				args = append(args, buffer...)
				buffer = nil
			}
		}
		s, simple := d.format(fv, level)
		allSimple = allSimple && simple
		if keywords {
			s = fieldName(field) + "=" + s
		}
		args = append(args, s)
	}
	if d.attributes && span != nil {
		args = append(args, d.attributeArgs(*span)...)
	}
	name := className(t)
	if allSimple && len(args) <= 3 {
		return name + "(" + strings.Join(args, ", ") + ")", len(args) == 0
	}
	return name + "(" + prefix + strings.Join(args, sep) + ")", false
}

func (d *dumper) attributeArgs(span Span) []string {
	if !span.StartPos.IsValid() {
		return nil
	}
	loc := Locate(d.fset, span)
	args := []string{"lineno=" + strconv.Itoa(loc.Lineno), "col_offset=" + strconv.Itoa(loc.ColOffset)}
	if span.EndPos.IsValid() {
		args = append(args, "end_lineno="+strconv.Itoa(loc.EndLineno), "end_col_offset="+strconv.Itoa(loc.EndColOffset))
	}
	return args
}

// layout returns the line prefix and separator for the children of a value at
// level, and the level the children are formatted at.
func (d *dumper) layout(level int) (prefix, sep string, next int) {
	if d.indent == nil {
		return "", ", ", level
	}
	next = level + 1
	prefix = "\n" + strings.Repeat(*d.indent, next)
	return prefix, "," + prefix, next
}

// className is the CPython class name of a node struct.
func className(t reflect.Type) string {
	if name, ok := classNames[t.Name()]; ok {
		return name
	}
	return t.Name()
}

// fieldName is the CPython field name: the py tag, else the Go name with a
// lower-case first letter.
func fieldName(f reflect.StructField) string {
	if tag, ok := f.Tag.Lookup("py"); ok {
		return tag
	}
	return strings.ToLower(f.Name[:1]) + f.Name[1:]
}

// repr is Python's repr of a constant value.
func repr(v Value) string {
	switch x := v.(type) {
	case Int:
		if x.N == nil {
			return "0"
		}
		return x.N.String()
	case Float:
		return formatFloat(float64(x), true)
	case Complex:
		return reprComplex(complex128(x))
	case Str:
		return Repr(string(x))
	case Bytes:
		return reprBytes(x)
	case Bool:
		if x {
			return "True"
		}
		return "False"
	case None:
		return "None"
	default:
		return "Ellipsis"
	}
}

// reprComplex follows complex_repr: a zero real part with a positive sign is
// omitted together with the parentheses.
func reprComplex(c complex128) string {
	re, im := real(c), imag(c)
	imStr := formatFloat(im, false)
	if re == 0 && !math.Signbit(re) {
		return imStr + "j"
	}
	if !strings.HasPrefix(imStr, "-") {
		imStr = "+" + imStr
	}
	return "(" + formatFloat(re, false) + imStr + "j)"
}

// formatFloat is PyOS_double_to_string(f, 'r', ...): the shortest digits that
// round-trip, in exponent form when the decimal point is at or before -4 or
// after 16 digits. addDot appends ".0" to integral values, as float_repr does.
func formatFloat(f float64, addDot bool) string {
	switch {
	case math.IsNaN(f):
		return "nan"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	}
	sign := ""
	if math.Signbit(f) {
		sign = "-"
	}
	mantissa, exp, _ := strings.Cut(strconv.FormatFloat(math.Abs(f), 'e', -1, 64), "e")
	digits := strings.Replace(mantissa, ".", "", 1)
	exponent, _ := strconv.Atoi(exp)
	point := exponent + 1
	switch {
	case point <= -4 || point > 16:
		if len(digits) > 1 {
			digits = digits[:1] + "." + digits[1:]
		}
		return sign + digits + "e" + expSuffix(exponent)
	case point <= 0:
		return sign + "0." + strings.Repeat("0", -point) + digits
	case point < len(digits):
		return sign + digits[:point] + "." + digits[point:]
	case addDot:
		return sign + digits + strings.Repeat("0", point-len(digits)) + ".0"
	default:
		return sign + digits + strings.Repeat("0", point-len(digits))
	}
}

// expSuffix is the exponent with a sign and at least two digits.
func expSuffix(exponent int) string {
	sign := "+"
	if exponent < 0 {
		sign, exponent = "-", -exponent
	}
	if exponent < 10 {
		return sign + "0" + strconv.Itoa(exponent)
	}
	return sign + strconv.Itoa(exponent)
}

// reprBytes is bytes_repr.
func reprBytes(s []byte) string {
	quote := quoteFor(strings.ContainsRune(string(s), '\''), strings.ContainsRune(string(s), '"'))
	var b strings.Builder
	b.WriteString("b")
	b.WriteByte(quote)
	for _, c := range s {
		switch {
		case c == quote || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c < 0x7f && !isEscaped(c):
			b.WriteByte(c)
		default:
			b.WriteString(escapeRune(rune(c)))
		}
	}
	b.WriteByte(quote)
	return b.String()
}

// quoteFor picks ' unless the text has a ' and no ".
func quoteFor(hasSingle, hasDouble bool) byte {
	if hasSingle && !hasDouble {
		return '"'
	}
	return '\''
}

// isEscaped reports whether an ASCII byte is written as an escape: the control characters.
func isEscaped(c byte) bool {
	return c < ' ' || c == 0x7f
}

func escapeRune(r rune) string {
	switch {
	case r == '\t':
		return `\t`
	case r == '\n':
		return `\n`
	case r == '\r':
		return `\r`
	case r <= 0xff:
		return `\x` + hex(r, 2)
	case r <= 0xffff:
		return `\u` + hex(r, 4)
	default:
		return `\U` + hex(r, 8)
	}
}

func hex(r rune, width int) string {
	s := strconv.FormatInt(int64(r), 16)
	return strings.Repeat("0", width-len(s)) + s
}

// decodeRune decodes the first code point of WTF-8 text, including a lone
// surrogate. Bytes that are not WTF-8 decode as U+FFFD of width 1.
func decodeRune(s string) (rune, int) {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError && size == 1 && len(s) >= 3 && s[0] == 0xED && s[1]&0xE0 == 0xA0 && s[2]&0xC0 == 0x80 {
		return rune(s[0]&0x0F)<<12 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
	}
	return r, size
}
