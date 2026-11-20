package types

import (
	"fmt"
	"slices"
	"strings"
)

// Dump writes the result of Check as deterministic text: the modules of the
// program in import order, each with its globals, the specializations of its
// functions and, at the end, the fallbacks.
func Dump(m *Module, info *Info) string {
	var b strings.Builder
	for _, mod := range importOrder(m) {
		if mod.init == nil {
			continue
		}
		dumpModule(&b, mod, info)
	}
	dumpPrelude(&b, info)
	fmt.Fprintf(&b, "fallbacks %d\n", len(info.Fallbacks))
	for _, fb := range info.Fallbacks {
		fmt.Fprintf(&b, "  %d:%d %s: %s\n", fb.Pos.Line, fb.Pos.Column, fb.Reason, fb.Msg)
	}
	return b.String()
}

// importOrder lists m and what it imports, each module after its imports.
func importOrder(m *Module) []*Module {
	var out []*Module
	seen := map[*Module]bool{}
	var walk func(x *Module)
	walk = func(x *Module) {
		if seen[x] {
			return
		}
		seen[x] = true
		for _, y := range x.imports {
			walk(y)
		}
		out = append(out, x)
	}
	walk(m)
	return out
}

func dumpModule(b *strings.Builder, mod *Module, info *Info) {
	fmt.Fprintf(b, "module %s\n", mod.name)
	for _, name := range mod.scope.Names() {
		if v, ok := mod.scope.Lookup(name).(*Var); ok && v.kind == GlobalVar && v.module == mod {
			fmt.Fprintf(b, "  var %s: %s\n", name, typeName(v.typ))
		}
	}
	classes := classesOf(mod)
	var funcs []*Func
	methods := map[*Class][]*Func{}
	for f := range info.Specs {
		if f.module != mod || f == mod.init {
			continue
		}
		if cls := classOfMethod(f, classes); cls != nil {
			methods[cls] = append(methods[cls], f)
			continue
		}
		funcs = append(funcs, f)
	}
	slices.SortFunc(funcs, func(x, y *Func) int { return int(x.pos) - int(y.pos) })
	for _, f := range funcs {
		fmt.Fprintf(b, "  def %s\n", f.fullName)
		dumpSpecs(b, info.Specs[f], "    ")
	}
	for _, n := range classes {
		dumpClass(b, n, methods[n.class()], info)
	}
}

// classesOf lists the classes declared in the module, in source order.
func classesOf(mod *Module) []*Named {
	var out []*Named
	for _, name := range mod.scope.Names() {
		if tn, ok := mod.scope.Lookup(name).(*TypeName); ok {
			if n, ok := tn.typ.(*Named); ok && n.Obj().module == mod && n.origin == n {
				out = append(out, n)
			}
		}
	}
	slices.SortFunc(out, func(x, y *Named) int { return int(x.obj.pos) - int(y.obj.pos) })
	return out
}

// classOfMethod finds the class whose body declares f.
func classOfMethod(f *Func, classes []*Named) *Class {
	if f.scope == nil || f.scope.parent == nil || f.scope.parent.kind != ClassScope {
		return nil
	}
	for _, n := range classes {
		if n.class().members == f.scope.parent {
			return n.class()
		}
	}
	return nil
}

func dumpClass(b *strings.Builder, n *Named, methods []*Func, info *Info) {
	cls := n.class()
	head := "  class " + n.obj.name
	var bases []string
	for _, base := range cls.bases {
		if bn, ok := Unalias(base).(*Named); ok && bn.obj.name != "object" {
			bases = append(bases, bn.obj.name)
		}
	}
	if len(bases) > 0 {
		head += "(" + strings.Join(bases, ", ") + ")"
	}
	fmt.Fprintln(b, head)
	var mro []string
	for _, m := range cls.mro {
		mro = append(mro, m.obj.name)
	}
	fmt.Fprintf(b, "    mro %s\n", strings.Join(mro, ", "))
	for _, v := range cls.fields {
		fmt.Fprintf(b, "    field %s: %s\n", v.name, typeName(v.typ))
	}
	for _, name := range cls.members.Names() {
		if v, ok := cls.members.Lookup(name).(*Var); ok && v.kind == ClassVar && name != "__slots__" {
			fmt.Fprintf(b, "    var %s: %s\n", name, typeName(v.typ))
		}
	}
	slices.SortFunc(methods, func(x, y *Func) int { return int(x.pos) - int(y.pos) })
	for _, f := range methods {
		fmt.Fprintf(b, "    def %s\n", f.name)
		dumpSpecs(b, info.Specs[f], "      ")
	}
}

// dumpPrelude writes the specializations of the functions of the prelude
// that the program calls.
func dumpPrelude(b *strings.Builder, info *Info) {
	var funcs []*Func
	for f := range info.Specs {
		if f.module != nil && f.module.init == nil {
			funcs = append(funcs, f)
		}
	}
	slices.SortFunc(funcs, func(x, y *Func) int { return strings.Compare(x.name, y.name) })
	for _, f := range funcs {
		fmt.Fprintf(b, "prelude %s\n", f.name)
		dumpSpecs(b, info.Specs[f], "  ")
	}
}

func dumpSpecs(b *strings.Builder, specs []*Spec, indent string) {
	for _, s := range specs {
		tag := "spec"
		if s.Generalized {
			tag = "general"
		}
		fmt.Fprintf(b, "%s%s %s\n", indent, tag, signatureText(s.Signature))
	}
}

func signatureText(sig *Signature) string {
	if sig == nil {
		return "<nil>"
	}
	p := &printer{qf: qualifier}
	p.inspect(&Signature{params: sig.params, result: sig.result})
	return p.b.String()
}
