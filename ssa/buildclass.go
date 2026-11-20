package ssa

import (
	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/types"
)

type classDecl struct {
	class *Class
	named *types.Named
}

// declareClasses declares the classes of the modules, so that a type that
// names one finds it complete.
func (b *builder) declareClasses(order []*types.Module) {
	for _, m := range order {
		for _, name := range m.Scope().Names() {
			if tn, ok := m.Scope().Lookup(name).(*types.TypeName); ok {
				if n, ok := tn.Type().(*types.Named); ok && isUserClass(n) && n.Obj().Module() == m {
					b.classOf(n)
				}
			}
		}
	}
	b.ready = true
	b.completeClasses()
}

// fieldLoad lowers the read of an attribute that is a field, a class variable
// or a property of a class of the program.
func (f *fbuild) fieldLoad(e *ast.Attribute, want Type) (Value, bool) {
	sel := f.b.info.Selections[e]
	if sel == nil {
		return nil, false
	}
	switch obj := sel.Obj().(type) {
	case *types.Var:
		if sel.Owner() == nil || !isUserClass(sel.Owner()) {
			return nil, false
		}
		if obj.Kind() == types.FieldVar {
			return f.conv(f.loadField(e, e.Value), want), true
		}
		return f.conv(f.load(f.b.classVar(sel.Owner(), obj)), want), true
	case *types.Func:
		if c := f.spec.Calls[e]; c != nil && sel.Kind() == types.DescriptorVal {
			return f.conv(f.invoke(c, []Value{f.expr(e.Value)}), want), true
		}
	}
	return nil, false
}

// construct lowers a call of a class of the program: a new instance, with
// its class-level defaults, and the call of its __init__.
func (f *fbuild) userConstruct(e *ast.Call, c *types.Call) (Value, bool) {
	meta, ok := f.typeOf(e.Func).(*types.Meta)
	if !ok {
		return nil, false
	}
	n, ok := meta.Elem().(*types.Named)
	if !ok || !isUserClass(n) {
		return nil, false
	}
	cls := f.b.classOf(n)
	inst := f.val(OpAlloc, classType(cls))
	if in, ok := inst.(*Instr); ok {
		in.Class = cls
	}
	f.initDefaults(n, inst)
	if c.Target != nil {
		f.callInit(e, c, inst)
	}
	return inst, true
}

// initDefaults stores the class-level values of the fields that have one.
func (f *fbuild) initDefaults(n *types.Named, inst Value) {
	for _, m := range n.Underlying().(*types.Class).MRO() {
		for _, fv := range m.Underlying().(*types.Class).Fields() {
			if !fv.HasDefault() {
				continue
			}
			g := f.b.classVar(m, fv)
			if in := f.emit(OpStoreField, Type{}, inst, f.conv(f.load(g), fieldType(TypeOf(inst).Class, fv.Name()))); in != nil {
				in.Attr = fv.Name()
			}
		}
	}
}

// classDef lowers a class statement: the class variables it defines are
// stored where the class is defined.
func (f *fbuild) classDef(st *ast.ClassDef) {
	obj, _ := f.b.info.Defs[st].(*types.TypeName)
	if obj == nil {
		return
	}
	n, ok := obj.Type().(*types.Named)
	if !ok || !isUserClass(n) {
		return
	}
	f.owner = n
	defer func() { f.owner = nil }()
	for _, b := range st.Body {
		switch b := b.(type) {
		case *ast.Assign:
			if len(b.Targets) == 1 && isSlots(b.Targets[0]) {
				continue
			}
			v := f.expr(b.Value)
			for _, t := range b.Targets {
				f.classVarStore(n, t, v)
			}
		case *ast.AnnAssign:
			if b.Value != nil {
				f.classVarStore(n, b.Target, f.expr(b.Value))
			}
		}
	}
}

func (f *fbuild) classVarStore(n *types.Named, target ast.Expr, v Value) {
	name, ok := target.(*ast.Name)
	if !ok || isSlots(name) {
		return
	}
	obj := f.b.info.Defs[name]
	if obj == nil {
		obj = f.b.info.Uses[name]
	}
	if cv, ok := obj.(*types.Var); ok && (cv.Kind() == types.ClassVar || cv.HasDefault() || cv.Kind() == types.FieldVar) {
		f.store(f.b.classVar(n, cv), v)
	}
}

// classVar returns the hidden global that holds the class variable v of the
// class owner, declaring it in the module of the class.
func (b *builder) classVar(owner *types.Named, v *types.Var) *Global {
	if g := b.globals[v]; g != nil {
		return g
	}
	mod := b.mods[owner.Obj().Module()]
	g := &Global{Name: owner.Obj().Name() + "." + v.Name(), Type: b.lower(v.Type()), Module: mod}
	mod.Globals = append(mod.Globals, g)
	b.globals[v] = g
	return g
}

// compareCall lowers a comparison that a method of a class of the program
// implements. The method belongs to the right operand when the comparison is
// reflected, and it is inverted for != that __eq__ implements.
func (f *fbuild) compareCall(c *types.Call, op ast.CmpOperator, l, r Value) Value {
	recv, arg := l, r
	if c.Slot != cmpDunders[op] && !c.Negate {
		recv, arg = r, l
	}
	res := f.conv(f.invoke(c, []Value{recv, arg}), tyI1)
	if c.Negate {
		return f.val(OpNot, tyI1, res)
	}
	return res
}

// methodCall lowers a call x.name(args) of a method of a class of the
// program.
func (f *fbuild) methodCall(e *ast.Call, attr *ast.Attribute, c *types.Call) Value {
	target := f.b.funcs[c.Target]
	sig := c.Target.Func.Signature()
	var recv Value
	first := 0
	switch {
	case c.Super != nil:
		recv, first = f.selfValue(), 1
	case f.b.info.Selections[attr].Kind() == types.ClassVal:
	case len(target.Params) > 0 && target.Params[0].Typ.Kind == KindTypeOf:
		recv, first = f.classReceiver(attr, target.Params[0].Typ), 1
	default:
		recv, first = f.instance(f.expr(attr.Value), attr.Attr), 1
	}
	params := make([]*types.Var, sig.Params().Len()-first)
	ptypes := make([]Type, len(params))
	for i := range params {
		params[i] = sig.Params().At(i + first)
		ptypes[i] = target.Params[i+first].Typ
	}
	args := f.bindArgs(e, params, ptypes, func(i int) Value {
		return f.defaultArg(c.Target.Func, params[i], ptypes[i])
	})
	if recv != nil {
		args = append([]Value{recv}, args...)
	}
	return f.callResolved(c, args)
}

// callInit calls the __init__ the checker resolved on the new instance.
func (f *fbuild) callInit(e *ast.Call, c *types.Call, inst Value) {
	target := f.b.funcs[c.Target]
	sig := c.Target.Func.Signature()
	params := make([]*types.Var, sig.Params().Len()-1)
	ptypes := make([]Type, len(params))
	for i := range params {
		params[i] = sig.Params().At(i + 1)
		ptypes[i] = target.Params[i+1].Typ
	}
	args := f.bindArgs(e, params, ptypes, func(i int) Value {
		return f.defaultArg(c.Target.Func, params[i], ptypes[i])
	})
	f.callResolved(c, append([]Value{inst}, args...))
}

// augAttribute lowers x.attr op= v.
func (f *fbuild) augAttribute(st *ast.AugAssign, t *ast.Attribute) {
	l := f.expr(t)
	r := f.expr(st.Value)
	f.storeAttribute(t, f.arith(st, st.Op, l, f.typeOf(t), r, f.typeOf(st.Value), st.Value, Type{}))
}

// storeAttribute lowers target = v for a field or a property.
func (f *fbuild) storeAttribute(target *ast.Attribute, v Value) {
	sel := f.b.info.Selections[target]
	if sel == nil {
		return
	}
	switch obj := sel.Obj().(type) {
	case *types.Var:
		f.storeField(target, v)
	case *types.Func:
		_ = obj
		if c := f.spec.Calls[types.Site{Node: target, Kind: types.SiteStore}]; c != nil {
			f.invoke(c, []Value{f.expr(target.Value), v})
		}
	}
}

// invoke calls the method an operation dispatches to: args holds the
// receiver and the arguments.
func (f *fbuild) invoke(c *types.Call, args []Value) Value {
	return f.callResolved(c, args)
}

// items starts a loop over the items of an iterator and returns the block that
// jumps back to start the next turn, the block where the loop goes when the
// items are exhausted, and the item. Code continues in the body. An iterator
// of a class of the program is advanced by its __next__, which raises
// StopIteration at the end.
func (f *fbuild) items(nextKey ast.Node, it Value, item Type) (head, done *Block, cur Value) {
	head = f.newBlock()
	f.jump(head)
	f.start(head)
	if t := TypeOf(it); t.Kind == KindClass && !t.Nullable {
		return f.userNext(head, nextKey, it)
	}
	body := f.newBlock()
	done = f.newBlock()
	f.branch(f.advance(it), body, done)
	f.seal(body)
	f.seal(done)
	f.start(body)
	return head, done, f.val(OpCurrent, itemType(TypeOf(it), item), it)
}

// userNext calls the __next__ of an iterator of a class of the program. The
// call is in a region that handles StopIteration, which ends the loop; any
// other exception goes on outward.
func (f *fbuild) userNext(head *Block, nextKey ast.Node, it Value) (*Block, *Block, Value) {
	done := f.newBlock()
	next := f.spec.Calls[nextKey]
	if !f.isUserCall(next) {
		f.end(OpUnreachable)
		f.seal(done)
		return head, done, f.undef(tyAny)
	}
	r := f.openRegion(true)
	val := f.callResolved(next, []Value{it})
	body := f.cur
	f.closeRegion(r)
	f.start(r.handler)
	exc := r.handler.Params[0]
	match := f.val(OpIsInstance, tyI1, exc)
	match.(*Instr).Class = f.b.class("StopIteration")
	other := f.newBlock()
	f.branch(match, done, other)
	f.seal(done)
	f.seal(other)
	f.start(other)
	f.throw(OpReraise, exc)
	f.start(body)
	return head, done, val
}

// callResolved emits the call the checker resolved: a static call, or a call
// of a slot of the class of the receiver.
func (f *fbuild) callResolved(c *types.Call, args []Value) Value {
	in, res := f.callInstr(c, args)
	if in == nil {
		return f.undef(res)
	}
	return f.conv(in, res)
}

// callInstr emits the call and returns it with the result type the checker
// gave the call, which may differ from the type of the instruction.
func (f *fbuild) callInstr(c *types.Call, args []Value) (*Instr, Type) {
	root := f.b.funcs[c.Target]
	if root == nil {
		return nil, tyAny
	}
	if c.Kind != types.Virtual {
		conv := make([]Value, len(args))
		for i, a := range args {
			conv[i] = f.conv(a, root.Params[i].Typ)
		}
		op := OpCall
		if root.Generator {
			op = OpMakeGen
		}
		in := f.emit(op, root.callResult(), conv...)
		if in != nil {
			in.Callee = root
		}
		return in, root.callResult()
	}
	slot := f.b.enter(c)
	recv := f.instance(args[0], slot)
	cls := TypeOf(recv).Class
	if TypeOf(recv).Kind != KindClass || cls == nil || cls.method(slot) == nil {
		return nil, root.Result
	}
	entry := cls.method(slot)
	vals := []Value{recv}
	for i, a := range args[1:] {
		vals = append(vals, f.conv(a, entry.Params[i+1].Typ))
	}
	in := f.emit(OpCallMethod, entry.Result, vals...)
	if in != nil {
		in.Attr = slot
	}
	return in, entry.Result
}

// enter makes the implementations of a virtual call entries of the slot of
// their classes, and returns the slot. Every entry of a slot takes the
// parameters of the first implementation and returns the join of their
// results; an implementation of another signature is entered through a bridge
// that converts.
func (b *builder) enter(c *types.Call) string {
	slot := b.slotName(c)
	if b.slots[slot] {
		return slot
	}
	b.slots[slot] = true
	root := b.funcs[c.Overrides[0].Target]
	params := paramTypes(root.Params[1:])
	var joined types.Type = types.Typ[types.Never]
	for _, ov := range c.Overrides {
		joined = types.Join(joined, resultOf(ov.Target))
	}
	result := b.lower(joined)
	for _, ov := range c.Overrides {
		cls := b.classOf(ov.Class)
		fn := b.bridge(b.funcs[ov.Target], params, result, slot)
		cls.Methods = append(cls.Methods, Method{Name: slot, Func: fn})
	}
	return slot
}

// slotName names the vtable slot of a virtual call.
func (b *builder) slotName(c *types.Call) string {
	root := b.funcs[c.Overrides[0].Target]
	name := root.Module.Name + "." + root.Name
	if c.Super != nil {
		name = "super." + b.classOf(c.Super).Name + "." + name
	}
	return name
}

// bridge returns fn if it takes the parameters and returns the result of its
// slot, and otherwise a function that calls it through conversions.
func (b *builder) bridge(fn *Function, params []Type, result Type, slot string) *Function {
	same := fn.Result.same(result) && len(fn.Params) == len(params)+1
	for i := 0; same && i < len(params); i++ {
		same = fn.Params[i+1].Typ.same(params[i])
	}
	if same {
		return fn
	}
	br := &Function{Name: b.unique(fn.Module, fn.Name+"$bridge"), Source: fn.Source, Module: fn.Module, Result: result}
	br.Params = append(br.Params, &Param{Name: "self", Typ: fn.Params[0].Typ})
	for i, t := range params {
		br.Params = append(br.Params, &Param{Name: "a" + itoa(i), Typ: t})
	}
	f := newFbuild(b, br, nil)
	args := make([]Value, len(br.Params))
	for i, p := range br.Params {
		args[i] = p
	}
	call := f.callFn(fn, args)
	if result.same(tyNone) {
		f.end(OpRet)
	} else {
		f.end(OpRet, f.conv(call, result))
	}
	f.finish()
	fn.Module.Functions = append(fn.Module.Functions, br)
	return br
}

// completeClasses gives each declared class its bases and the fields its
// bases and it lay out, in that order.
func (b *builder) completeClasses() {
	if b.complete == nil {
		b.complete = map[*Class]bool{}
	}
	var complete func(d classDecl)
	complete = func(d classDecl) {
		if b.complete[d.class] {
			return
		}
		b.complete[d.class] = true
		cls := d.named.Underlying().(*types.Class)
		for _, base := range cls.Bases() {
			bn, ok := types.Unalias(base).(*types.Named)
			if !ok {
				continue
			}
			if isUserClass(bn) {
				bc := b.classOf(bn)
				d.class.Bases = append(d.class.Bases, bc)
				complete(b.declOf(bc))
				for _, fld := range bc.Fields {
					if _, have := d.class.field(fld.Name); !have {
						d.class.Fields = append(d.class.Fields, fld)
					}
				}
			} else if bn.Obj().Name() != "object" {
				d.class.Bases = append(d.class.Bases, b.class(bn.Obj().Name()))
			}
		}
		for _, v := range cls.Fields() {
			d.class.Fields = append(d.class.Fields, Field{Name: v.Name(), Type: b.lower(v.Type()), Owner: d.class})
		}
	}
	for i := 0; i < len(b.pending); i++ {
		complete(b.pending[i])
	}
}

// classOf returns the class of the program that n names. Its bases and fields
// are filled in once the classes of the modules are declared.
func (b *builder) classOf(n *types.Named) *Class {
	name := types.TypeString(n.Origin(), nil)
	if c := b.classes[name]; c != nil {
		return c
	}
	c := &Class{Name: name}
	b.classes[name] = c
	b.origins[c] = n.Origin()
	b.prog.Classes = append(b.prog.Classes, c)
	b.pending = append(b.pending, classDecl{c, n.Origin()})
	if b.ready {
		b.completeClasses()
	}
	return c
}

func (b *builder) declOf(c *Class) classDecl {
	for _, d := range b.pending {
		if d.class == c {
			return d
		}
	}
	return classDecl{class: c}
}

// isIdentity reports whether c is the == or != that object supplies, which
// two instances of classes of the program compare by identity.
func (f *fbuild) isIdentity(c *types.Call, lt, rt types.Type) bool {
	if c == nil || c.Func == nil || c.Func.Module() == nil || c.Func.Module().Name() != "builtins" {
		return false
	}
	l, r := f.b.lower(lt), f.b.lower(rt)
	return (c.Slot == "__eq__" || c.Slot == "__ne__") && l.Kind == KindClass && r.Kind == KindClass
}

// selfValue is the first parameter of the method being built.
func (f *fbuild) selfValue() Value {
	p := f.spec.Func.Signature().Params().At(0)
	return f.readVar(bkey{obj: p}, f.cur)
}

// classReceiver is the class object that a class method takes: the class of
// the receiver of the call, which is the class named or the static class of the
// instance.
func (f *fbuild) classReceiver(attr *ast.Attribute, param Type) Value {
	cls := param.Class
	switch t := f.irType(attr.Value); t.Kind {
	case KindTypeOf:
		cls = t.Class
	case KindClass:
		f.expr(attr.Value)
		cls = t.Class
	}
	in := f.emit(OpClassObj, Type{Kind: KindTypeOf, Class: cls})
	if in == nil {
		return f.undef(param)
	}
	in.Class = cls
	return f.conv(in, param)
}

func (f *fbuild) storeField(target *ast.Attribute, v Value) {
	recv := f.instance(f.expr(target.Value), target.Attr)
	t := TypeOf(recv)
	if t.Kind != KindClass {
		return
	}
	fld, ok := t.Class.field(target.Attr)
	if !ok {
		return
	}
	if in := f.emit(OpStoreField, Type{}, recv, f.conv(v, fld.Type)); in != nil {
		in.Attr = target.Attr
	}
}

// loadField reads the field named by e from the instance recv evaluates to.
func (f *fbuild) loadField(e *ast.Attribute, recv ast.Expr) Value {
	obj := f.instance(f.expr(recv), e.Attr)
	t := TypeOf(obj)
	if t.Kind != KindClass {
		return f.undef(f.irType(e))
	}
	fld, ok := t.Class.field(e.Attr)
	if !ok {
		return f.undef(f.irType(e))
	}
	in := f.emit(OpLoadField, fld.Type, obj)
	if in == nil {
		return f.undef(fld.Type)
	}
	in.Attr = e.Attr
	return in
}

// instance converts the receiver of a field access or a method call to a
// non-nullable instance: a nullable reference raises AttributeError on None.
func (f *fbuild) instance(v Value, attr string) Value {
	t := TypeOf(v)
	if t.Kind != KindClass || !t.Nullable {
		return v
	}
	non := t
	non.Nullable = false
	in := f.emit(OpNonNull, non, f.register(v))
	if in == nil {
		return f.undef(non)
	}
	in.Attr = attr
	return in
}

// isUserCall reports whether c resolved to a method of a class of the
// program.
func (f *fbuild) isUserCall(c *types.Call) bool {
	return c != nil && c.Kind != types.Dynamic && (c.Target != nil || c.Kind == types.Virtual)
}

func isSlots(e ast.Expr) bool {
	n, ok := e.(*ast.Name)
	return ok && n.ID == "__slots__"
}

func fieldType(c *Class, name string) Type {
	fld, _ := c.field(name)
	return fld.Type
}

// classType is the IR type of an instance of c.
func classType(c *Class) Type {
	return Type{Kind: KindClass, Class: c}
}

// isUserClass reports whether n is a class of the program, as opposed to one
// of the standard library.
func isUserClass(n *types.Named) bool {
	m := n.Obj().Module()
	return m != nil && m.Init() != nil
}
