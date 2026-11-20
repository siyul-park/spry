package types

import (
	"slices"

	"github.com/siyul-park/spry/ast"
)

// classInfo is what the solver knows of a class of the program: its fixed
// layout and the classes that derive from it.
type classInfo struct {
	named   *Named
	def     *ast.ClassDef
	src     *source
	fields  []*Var
	bad     bool
	dynamic bool
	subs    []*classInfo
}

// Fields returns the instance variables the class declares itself, in
// layout order. The fields of its bases are theirs.
func (c *Class) Fields() []*Var {
	return append([]*Var(nil), c.fields...)
}

// layout finds the classes of the program, fixes the attributes of each and
// rejects what the subset does not allow. A class that cannot be laid out
// keeps no info and is unsupported where it is used.
func (c *solver) layout() {
	c.classes = map[*Named]*classInfo{}
	for _, src := range c.p.order {
		c.scanClasses(src, src.file.Body, false)
	}
	for _, ci := range c.classList {
		c.claimFields(ci)
	}
	for _, ci := range c.classList {
		c.checkBases(ci)
		c.checkDynamic(ci)
		for _, b := range c.userMRO(ci.named) {
			b.subs = append(b.subs, ci)
		}
	}
}

// scanClasses registers the classes of a body, and rejects those that are
// nested in a function or in another class.
func (c *solver) scanClasses(src *source, body []ast.Stmt, nested bool) {
	for _, st := range body {
		if cd, ok := st.(*ast.ClassDef); ok {
			c.register(src, cd, nested)
			c.scanClasses(src, cd.Body, true)
			continue
		}
		_, inFunc := st.(*ast.FunctionDef)
		for _, b := range nestedBodies(st) {
			c.scanClasses(src, b, nested || inFunc)
		}
	}
}

func (c *solver) register(src *source, cd *ast.ClassDef, nested bool) {
	obj, ok := c.info.Defs[cd].(*TypeName)
	if !ok {
		return
	}
	named, ok := obj.typ.(*Named)
	if !ok {
		return
	}
	ci := &classInfo{named: named, def: cd, src: src}
	switch {
	case nested, len(cd.DecoratorList) > 0, hasMetaclass(cd):
		// rejected by rejectSubset
		ci.bad = true
	}
	c.classes[named.origin] = ci
	c.classList = append(c.classList, ci)
}

func hasMetaclass(cd *ast.ClassDef) bool {
	for _, b := range cd.Bases {
		if n, ok := b.(*ast.Name); ok && n.ID == "type" {
			return true
		}
	}
	for _, k := range cd.Keywords {
		if k.Arg != nil && *k.Arg == "metaclass" {
			return true
		}
	}
	return false
}

// claimFields declares the instance variables of a class: the attributes
// stored through the first parameter of any method, and the __slots__. A name
// that a base class has already claimed belongs to the base.
func (c *solver) claimFields(ci *classInfo) {
	if ci.bad {
		return
	}
	members := ci.named.class().members
	claim := func(name string, pos ast.Node, dflt bool) {
		if c.fieldOf(ci, name) != nil {
			return
		}
		switch prev := members.Lookup(name).(type) {
		case *Var:
			if prev.kind == ClassVar || dflt {
				prev.hasDefault = true
			}
			prev.kind = FieldVar
			ci.fields = append(ci.fields, prev)
		case nil:
			v := NewVar(pos.Pos(), name, Typ[Never], FieldVar)
			v.module = ci.named.Obj().module
			members.insert(v)
			ci.fields = append(ci.fields, v)
		}
	}
	for _, name := range slotNames(ci.def) {
		claim(name, ci.def, false)
	}
	for _, st := range ci.def.Body {
		if a, ok := st.(*ast.AnnAssign); ok && !isQualifier(a.Annotation, "ClassVar") {
			if n, ok := a.Target.(*ast.Name); ok {
				claim(n.ID, n, a.Value != nil)
			}
		}
	}
	for _, st := range ci.def.Body {
		def, ok := st.(*ast.FunctionDef)
		if !ok {
			continue
		}
		self := selfName(def)
		if self == "" {
			continue
		}
		ast.Inspect(def, func(n ast.Node) bool {
			if a, ok := n.(*ast.Attribute); ok && a.Ctx == ast.CtxStore {
				if base, ok := a.Value.(*ast.Name); ok && base.ID == self {
					claim(a.Attr, a, false)
				}
			}
			if a, ok := n.(*ast.AnnAssign); ok {
				if t, ok := a.Target.(*ast.Attribute); ok {
					if base, ok := t.Value.(*ast.Name); ok && base.ID == self {
						claim(t.Attr, t, false)
					}
				}
				c.annotateField(ci, def, self, a)
			}
			return true
		})
	}
	ci.named.class().fields = ci.fields
}

// annotateField gives a field the type of its annotation: self.a: int = 1.
func (c *solver) annotateField(ci *classInfo, def *ast.FunctionDef, self string, a *ast.AnnAssign) {
	target, ok := a.Target.(*ast.Attribute)
	if !ok {
		return
	}
	if base, ok := target.Value.(*ast.Name); !ok || base.ID != self {
		return
	}
	v := c.fieldOf(ci, target.Attr)
	scope := c.info.Scopes[def]
	if v == nil || v.annot || scope == nil {
		return
	}
	if t, _ := c.p.decl.annotation(a.Annotation, scope); t != nil {
		v.typ, v.annot = t, true
	}
}

// fieldOf finds the field a class has, or inherits, under name.
func (c *solver) fieldOf(ci *classInfo, name string) *Var {
	for _, b := range c.userMRO(ci.named) {
		for _, f := range b.fields {
			if f.name == name {
				return f
			}
		}
	}
	return nil
}

// checkBases rejects multiple inheritance in which the classes that have
// fields are not one chain: the layout of such a class is not a prefix of its
// bases. A base that several bases share, as in a diamond, is one class.
func (c *solver) checkBases(ci *classInfo) {
	if ci.bad {
		return
	}
	var owners []*classInfo
	for _, b := range ci.named.class().bases {
		base, ok := Unalias(b).(*Named)
		if !ok {
			continue
		}
		for _, anc := range c.userMRO(base) {
			if anc.bad {
				c.reject(ci, ci.def, "TYP150", "a base class is not supported")
				return
			}
			if len(anc.fields) > 0 && !slices.Contains(owners, anc) {
				owners = append(owners, anc)
			}
		}
	}
	for _, o := range owners {
		for _, p := range owners {
			if !slices.ContainsFunc(c.userMRO(o.named), func(x *classInfo) bool { return x == p }) &&
				!slices.ContainsFunc(c.userMRO(p.named), func(x *classInfo) bool { return x == o }) {
				c.reject(ci, ci.def, "TYP150", "multiple inheritance from more than one base with fields is not supported")
				return
			}
		}
	}
}

func (c *solver) reject(ci *classInfo, n ast.Node, code, msg string) {
	ci.bad = true
	c.p.errs.Add(c.fset.Position(n.Pos()), c.fset.Position(n.End()), code, msg)
}

// checkDynamic marks a class whose instances compute their attributes.
func (c *solver) checkDynamic(ci *classInfo) {
	for _, b := range c.userMRO(ci.named) {
		for _, name := range []string{"__getattr__", "__getattribute__", "__setattr__"} {
			if _, ok := b.named.class().members.Lookup(name).(*Func); ok {
				ci.dynamic = true
			}
		}
	}
}

// userMRO lists the classes of the program in the MRO of n, n first.
func (c *solver) userMRO(n *Named) []*classInfo {
	var out []*classInfo
	for _, m := range resolvedMRO(n) {
		if ci := c.classes[m.origin]; ci != nil {
			out = append(out, ci)
		}
	}
	return out
}

// instanceOf returns the info of the class of an instance type, or nil.
func (c *solver) instanceOf(t Type) *classInfo {
	if n, ok := Unalias(t).(*Named); ok {
		return c.classes[n.origin]
	}
	return nil
}

// selfName is the name of the first parameter of a method that binds an
// instance, or "" for a static method or a method without parameters.
func selfName(def *ast.FunctionDef) string {
	for _, d := range def.DecoratorList {
		if n, ok := d.(*ast.Name); ok && (n.ID == "staticmethod" || n.ID == "classmethod") {
			return ""
		}
	}
	switch {
	case len(def.Args.PosOnlyArgs) > 0:
		return def.Args.PosOnlyArgs[0].Arg
	case len(def.Args.Args) > 0:
		return def.Args.Args[0].Arg
	}
	return ""
}

// slotNames lists the names of a literal __slots__ of the class body.
func slotNames(cd *ast.ClassDef) []string {
	for _, st := range cd.Body {
		if a, ok := st.(*ast.Assign); ok && len(a.Targets) == 1 && isName(a.Targets[0], "__slots__") {
			names, _ := stringElems(a.Value)
			return names
		}
	}
	return nil
}

// mergeClasses joins the instances of classes of the program that a union
// holds into their nearest common base, so that a value has one layout: a
// union of siblings cannot be a field access or a method call in the IR.
func mergeClasses(u *Union) Type {
	terms := append([]Type(nil), u.terms...)
	for i := 0; i < len(terms); i++ {
		for j := i + 1; j < len(terms); j++ {
			if base := commonBase(terms[i], terms[j]); base != nil {
				terms[i] = base
				terms = slices.Delete(terms, j, j+1)
				return Join(NewUnion(terms...), base)
			}
		}
	}
	return u
}

// commonBase is the nearest class that two instances of classes of the
// program have in common other than object, or nil. Joining them as that
// class keeps a value of either an object of one class layout prefix, which
// a union of the two does not.
func commonBase(x, y Type) *Named {
	nx, ok1 := x.(*Named)
	ny, ok2 := y.(*Named)
	if !ok1 || !ok2 || !inProgram(nx) || !inProgram(ny) {
		return nil
	}
	mro := resolvedMRO(ny)
	for _, m := range resolvedMRO(nx) {
		if !inProgram(m) {
			continue
		}
		for _, o := range mro {
			if o.origin == m.origin {
				return m
			}
		}
	}
	return nil
}

// inProgram reports whether n is a class of the program being checked, as
// opposed to one of the standard library.
func inProgram(n *Named) bool {
	m := n.Obj().Module()
	return m != nil && m.init != nil
}
