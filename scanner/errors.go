package scanner

import (
	"errors"
	"fmt"
	"sort"

	"github.com/siyul-park/spry/token"
)

// Error is one tokenizer or decoding failure. Pos and End delimit the
// offending text, End exclusive, with byte columns. CPython's SyntaxError
// offset is Pos.Column and its end offset is End.Column-1, both counted in
// code points; a zero-width error has End equal to Pos.
type Error struct {
	Pos, End token.Position
	Msg      string
	Err      error // ErrSyntax, ErrIndentation or ErrTab
}

// ErrorList is a list of *Error. The zero value is an empty list ready to use.
type ErrorList []*Error

// Error categories, named after the CPython exception classes. ErrTab is a
// kind of ErrIndentation, which is a kind of ErrSyntax: errors.Is reports
// true along that chain.
var (
	ErrSyntax      = errors.New("SyntaxError")
	ErrIndentation = errors.New("IndentationError")
	ErrTab         = errors.New("TabError")
)

// Add appends an error covering [pos, end) to the list.
func (p *ErrorList) Add(pos, end token.Position, msg string, err error) {
	*p = append(*p, &Error{Pos: pos, End: end, Msg: msg, Err: err})
}

// Len implements sort.Interface.
func (p ErrorList) Len() int {
	return len(p)
}

// Swap implements sort.Interface.
func (p ErrorList) Swap(i, j int) {
	p[i], p[j] = p[j], p[i]
}

// Less implements sort.Interface: by filename, line, column, then message.
func (p ErrorList) Less(i, j int) bool {
	e, f := &p[i].Pos, &p[j].Pos
	switch {
	case e.Filename != f.Filename:
		return e.Filename < f.Filename
	case e.Line != f.Line:
		return e.Line < f.Line
	case e.Column != f.Column:
		return e.Column < f.Column
	}
	return p[i].Msg < p[j].Msg
}

// Sort sorts the list by position.
func (p ErrorList) Sort() {
	sort.Sort(p)
}

// Err returns the list as an error, or nil if it is empty.
func (p ErrorList) Err() error {
	if len(p) == 0 {
		return nil
	}
	return p
}

// Error formats the error as "file:line:col: msg".
func (e *Error) Error() string {
	if e.Pos.Filename != "" || e.Pos.IsValid() {
		return e.Pos.String() + ": " + e.Msg
	}
	return e.Msg
}

// Unwrap returns the category sentinel.
func (e *Error) Unwrap() error {
	return e.Err
}

// Is reports whether target is the category of e or one of its parents.
func (e *Error) Is(target error) bool {
	switch target {
	case ErrSyntax:
		return e.Err == ErrSyntax || e.Err == ErrIndentation || e.Err == ErrTab
	case ErrIndentation:
		return e.Err == ErrIndentation || e.Err == ErrTab
	}
	return false
}

// Error implements the error interface.
func (p ErrorList) Error() string {
	switch len(p) {
	case 0:
		return "no errors"
	case 1:
		return p[0].Error()
	}
	return fmt.Sprintf("%s (and %d more errors)", p[0], len(p)-1)
}

// Unwrap returns the errors of the list so that errors.Is and errors.As see
// each category.
func (p ErrorList) Unwrap() []error {
	errs := make([]error, len(p))
	for i, e := range p {
		errs[i] = e
	}
	return errs
}
