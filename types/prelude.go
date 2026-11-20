package types

import (
	"strings"

	"github.com/siyul-park/spry/ast"
)

// preludeOf returns the function of the prelude that implements a call of a
// builtin function: the call needs one when it passes key=, or when it works
// on instances of classes of the program, whose methods the host cannot call.
// Any other call keeps the host function. It returns nil if the call needs
// none.
func (s *spec) preludeOf(cal callee, args []argv) *Func {
	f := cal.f
	if cal.sel != nil || f.module == nil || f.module.name != "builtins" {
		return nil
	}
	positional, keyed, user := 0, false, false
	for _, a := range args {
		if a.name == "" {
			positional++
		}
		keyed = keyed || a.name == "key"
		user = user || s.holdsClass(a.t)
	}
	name := ""
	switch f.name {
	case "sorted":
		name = "__sorted__"
	case "min", "max":
		name = "__" + f.name + "__"
		if positional == 2 {
			name = "__" + f.name + "2__"
		}
	case "sum":
		name = "__sum__"
	}
	if name == "" || !keyed && !user {
		return nil
	}
	p, _ := Universe.Lookup(name).(*Func)
	return p
}

// holdsClass reports whether t is an instance of a class of the program or a
// container, tuple or union of them.
func (s *spec) holdsClass(t Type) bool {
	return s.holdsClassSeen(t, map[Type]bool{})
}

// holdsClassSeen is holdsClass that visits each type once, because the type
// of a list that holds itself refers to itself.
func (s *spec) holdsClassSeen(t Type, seen map[Type]bool) bool {
	t = norm(Unalias(t))
	if seen[t] {
		return false
	}
	seen[t] = true
	switch t := t.(type) {
	case *Named:
		if s.c.instanceOf(t) != nil {
			return true
		}
		for _, a := range t.targs.slice() {
			if s.holdsClassSeen(a, seen) {
				return true
			}
		}
	case *Tuple:
		for _, e := range t.elems() {
			if s.holdsClassSeen(e, seen) {
				return true
			}
		}
	case *Union:
		for _, e := range t.terms {
			if s.holdsClassSeen(e, seen) {
				return true
			}
		}
	}
	return false
}

// preludeSource is the source that stands for the module of the prelude.
func (c *solver) preludeSource(f *Func) *source {
	if f.module == nil || f.module.name != "builtins" || f.Parent() != Universe || !strings.HasPrefix(f.name, "__") || len(f.defs) != 1 || !hasBody(f.defs[0]) {
		return nil
	}
	if c.prelude == nil {
		c.prelude = &source{mod: f.module, stub: true}
	}
	return c.prelude
}

// adopt copies what the declarations of the prelude recorded about the nodes of
// a function into the facts of the program, so that its body is analyzed like
// the code of the program.
func (c *solver) adopt(def *ast.FunctionDef) {
	from := stubs.prog.info
	ast.Inspect(def, func(n ast.Node) bool {
		if obj := from.Defs[n]; obj != nil {
			c.info.Defs[n] = obj
		}
		if e, ok := n.(ast.Expr); ok {
			if obj := from.Uses[e]; obj != nil {
				c.info.Uses[e] = obj
			}
		}
		if sc := from.Scopes[n]; sc != nil {
			c.info.Scopes[n] = sc
		}
		if obj := from.Implicits[n]; obj != nil {
			c.info.Implicits[n] = obj
		}
		return true
	})
}

// presetDefaults gives the parameters of a function that is never defined by
// running code the types of their constant defaults.
func (f *fn) presetDefaults(def *ast.FunctionDef) {
	a := def.Args
	byName := map[string]*Var{}
	for _, p := range f.sig.params.slice() {
		byName[p.name] = p
	}
	positional := append(append([]*ast.Arg{}, a.PosOnlyArgs...), a.Args...)
	set := func(arg *ast.Arg, d ast.Expr) {
		var t Type = Typ[Any]
		if c, ok := d.(*ast.Constant); ok {
			t = Default(constantType(c.Value))
		}
		f.defaults[byName[arg.Arg]] = t
	}
	for i, d := range a.Defaults {
		set(positional[len(positional)-len(a.Defaults)+i], d)
	}
	for i, d := range a.KwDefaults {
		if d != nil {
			set(a.KwOnlyArgs[i], d)
		}
	}
}
