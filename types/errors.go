package types

import (
	"errors"
	"fmt"
	"sort"

	"github.com/siyul-park/spry/token"
)

// Error is one type or subset diagnostic. Pos and End delimit the offending
// text, End exclusive, with byte columns; Code is the TYP code.
type Error struct {
	Pos, End token.Position
	Code     string
	Msg      string
}

// ErrorList is a list of *Error. The zero value is an empty list ready to use.
type ErrorList []*Error

// Error categories reported by errors.Is for the codes they cover.
var (
	ErrUnsupported = errors.New("unsupported construct")
	ErrAnnotation  = errors.New("invalid annotation")
	ErrTypeArg     = errors.New("invalid type argument")
)

var categories = map[string]error{
	"TYP100": ErrUnsupported, "TYP101": ErrUnsupported, "TYP110": ErrUnsupported,
	"TYP111": ErrUnsupported, "TYP112": ErrUnsupported, "TYP113": ErrUnsupported,
	"TYP120": ErrUnsupported, "TYP121": ErrUnsupported, "TYP130": ErrUnsupported,
	"TYP150": ErrUnsupported,
	"TYP140": ErrAnnotation,
}

// Add appends an error covering [pos, end) to the list.
func (p *ErrorList) Add(pos, end token.Position, code, msg string) {
	*p = append(*p, &Error{Pos: pos, End: end, Code: code, Msg: msg})
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

// Error formats the error as "file:line:col: error CODE: msg".
func (e *Error) Error() string {
	if e.Pos.Filename != "" || e.Pos.IsValid() {
		return e.Pos.String() + ": error " + e.Code + ": " + e.Msg
	}
	return "error " + e.Code + ": " + e.Msg
}

// Is reports whether target is the category of e.
func (e *Error) Is(target error) bool {
	return categories[e.Code] == target
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
