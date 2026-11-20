package types

import (
	"github.com/siyul-park/spry/ast"
)

// builtinDunders maps the builtin functions that call a method of their
// argument to that method.
var builtinDunders = map[string]string{
	"len": "__len__", "repr": "__repr__", "hash": "__hash__", "abs": "__abs__", "next": "__next__", "iter": "__iter__",
}

// userArgs rewrites the arguments of a call of a builtin function that
// converts to text or calls a method of its argument, and reports the result
// when the call is that method.
func (s *spec) userArgs(e *ast.Call, name string, args []argv) (Type, bool) {
	switch name {
	case "print":
		for i := range args {
			if args[i].name == "" {
				args[i].t = s.textOf(args[i].e, args[i].t, "__str__")
			}
		}
	case "bool":
		if len(args) == 1 && args[0].name == "" {
			s.truth(args[0].e, args[0].t)
		}
	case "str":
		if len(args) == 1 && args[0].name == "" {
			args[0].t = s.textOf(args[0].e, args[0].t, "__str__")
		}
	}
	dunder, ok := builtinDunders[name]
	if !ok || len(args) == 0 || !s.userHas(args[0].t, dunder) {
		return nil, false
	}
	res, call, ok := s.userMethod(e, args[0].t, dunder, args[1:])
	if !ok {
		return nil, false
	}
	s.calls[e] = call
	return res, true
}

// formatUser records how the value of a replacement field is converted to
// text: by the __format__ of its class, or by its __str__ or __repr__.
func (s *spec) formatUser(fv *ast.FormattedValue, t Type) {
	if fv.Conversion == -1 && s.userHas(t, "__format__") {
		if _, call, ok := s.userMethod(fv, t, "__format__", []argv{{t: builtin("str")}}); ok {
			s.calls[fv] = call
			return
		}
	}
	name := "__str__"
	if fv.Conversion == 'r' || fv.Conversion == 'a' {
		name = "__repr__"
	}
	s.textOf(fv.Value, t, name)
}

// textOf records how an operand that becomes text is converted when it is an
// instance of a class of the program, whose __str__ or __repr__ is code the
// host cannot call, and returns the type the operand has once converted.
// str falls back to __repr__ as object.__str__ does.
func (s *spec) textOf(e ast.Expr, t Type, name string) Type {
	if name == "__str__" && !s.userHas(t, name) {
		name = "__repr__"
	}
	if !s.userHas(t, name) {
		return t
	}
	site := Site{e, SiteConvert, 0}
	if _, call, ok := s.userMethod(site, t, name, nil); ok {
		s.calls[site] = call
		return builtin("str")
	}
	return t
}

// userHas reports whether t is an instance of a class of the program that
// defines the method itself or inherits it from another class of the program.
func (s *spec) userHas(t Type, name string) bool {
	if s.c.instanceOf(Default(t)) == nil {
		return false
	}
	sel := LookupAttr(Default(t), name)
	if sel == nil || sel.kind != MethodVal {
		return false
	}
	f := funcOf(sel)
	return f != nil && s.c.funcOf(f) != nil
}
