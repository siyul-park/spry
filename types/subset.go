package types

import (
	"slices"

	"github.com/siyul-park/spry/ast"
)

// Subset rules that need only the syntax and the resolution of names. Each
// TYP code of such a rule is emitted here and nowhere else; the rules that
// need types (the layout of a class, annotations, MRO, imports, generators)
// are emitted where that information lives. docs/subset.md lists them all.
//
//	TYP100  eval, exec and compile
//	TYP101  globals(), locals() and vars() other than a module-level read
//	TYP110  setattr/delattr, del obj.attr, statements of a class body
//	TYP112  metaclasses, an __init_subclass__ that mutates the class
//	TYP113  assignment to __class__
//	TYP120  __import__
//	TYP130  frames and introspection: _getframe, __dict__, __annotations__, vars(obj)
//	TYP150  planned constructs

// subsetWalker walks one module, tracking the scope that resolves names.
type subsetWalker struct {
	c      *solver
	src    *source
	scopes []*Scope
	path   []ast.Node
	pushed []bool
}

var (
	dynamicCode    = []string{"eval", "exec", "compile"}
	namespaceCalls = []string{"globals", "locals", "vars"}
	frameAttrs     = []string{"_getframe", "_current_frames", "__dict__", "__annotations__"}
	readOnlyDict   = []string{"get", "keys", "values", "items", "copy", "__getitem__", "__contains__", "__len__"}
	variance       = []string{"covariant", "contravariant", "infer_variance"}
)

// rejectSubset reports the subset violations of the modules of the program.
func (c *solver) rejectSubset() {
	if c.p.stubs {
		return
	}
	for _, src := range c.p.order {
		if src.stub {
			continue
		}
		w := &subsetWalker{c: c, src: src}
		ast.Inspect(src.file, w.visit)
	}
}

// visit walks the tree, keeping the scope and the path of the node.
func (w *subsetWalker) visit(n ast.Node) bool {
	if n == nil {
		last := len(w.path) - 1
		w.path = w.path[:last]
		if w.pushed[last] {
			w.scopes = w.scopes[:len(w.scopes)-1]
		}
		w.pushed = w.pushed[:last]
		return false
	}
	w.check(n)
	w.path = append(w.path, n)
	sc := w.c.info.Scopes[n]
	w.pushed = append(w.pushed, sc != nil)
	if sc != nil {
		w.scopes = append(w.scopes, sc)
	}
	return true
}

// check applies the rules of the subset to one node.
func (w *subsetWalker) check(n ast.Node) {
	switch n := n.(type) {
	case *ast.AsyncFunctionDef, *ast.AsyncFor, *ast.AsyncWith, *ast.Await:
		w.report(n, "TYP150", "async and await are not supported")
	case *ast.TryStar:
		w.report(n, "TYP150", "except* is not supported")
	case *ast.TemplateStr:
		w.report(n, "TYP150", "template strings are not supported")
	case *ast.Constant:
		if _, ok := n.Value.(ast.Complex); ok {
			w.report(n, "TYP150", "complex numbers are not supported")
		}
	case *ast.Try:
		if len(n.FinalBody) > 0 {
			w.rejectYield(n, "a yield inside a try with a finally clause is not supported")
		}
	case *ast.With:
		w.rejectYield(n, "a yield inside a with is not supported")
	case *ast.ParamSpec, *ast.TypeVarTuple:
		w.report(n, "TYP150", "ParamSpec and TypeVarTuple are not supported")
	case *ast.Name:
		w.name(n)
	case *ast.Attribute:
		w.attribute(n)
	case *ast.Call:
		w.call(n)
	case *ast.ImportFrom:
		w.importFrom(n)
	case *ast.Delete:
		w.deleteTargets(n.Targets)
	case *ast.ClassDef:
		w.class(n)
	}
}

// name checks a use of a name that resolves to a builtin.
func (w *subsetWalker) name(n *ast.Name) {
	if n.Ctx != ast.CtxLoad {
		return
	}
	call, _ := w.parent(1).(*ast.Call)
	isCall := call != nil && call.Func == ast.Expr(n)
	switch {
	case slices.Contains(dynamicCode, n.ID) && w.isBuiltinName(n.ID):
		w.report(n, "TYP100", n.ID+"() is not supported: it needs a dynamic tier")
	case slices.Contains(namespaceCalls, n.ID) && w.isBuiltinName(n.ID):
		w.namespace(n, call, isCall)
	case n.ID == "__import__" && w.isBuiltinName(n.ID):
		if !isCall || len(call.Args) == 0 || !isStrConstant(call.Args[0]) {
			w.report(n, "TYP120", "__import__() needs a constant module name")
		}
	case (n.ID == "setattr" || n.ID == "delattr") && w.isBuiltinName(n.ID):
		w.report(n, "TYP110", n.ID+"() changes the layout of the class; assign the attribute instead")
	case n.ID == "dataclass_transform":
		w.report(n, "TYP150", "dataclass_transform is not supported")
	default:
		w.unsupportedForm(n)
	}
}

// namespace checks a use of globals(), locals() or vars(). Only a read of the
// dict at module level is a snapshot; vars(obj) reads obj.__dict__.
func (w *subsetWalker) namespace(n *ast.Name, call *ast.Call, isCall bool) {
	switch {
	case isCall && n.ID == "vars" && (len(call.Args) > 0 || len(call.Keywords) > 0):
		w.report(n, "TYP130", "vars(obj) reads __dict__, which the compiled program does not have")
	case !isCall:
		w.report(n, "TYP101", n.ID+" is not supported as a value; call it at module level and read the result")
	case w.scope() == nil || w.scope().Kind() != ModuleScope:
		w.report(call, "TYP101", n.ID+"() is only supported at module level")
	case !w.readsOnly(call):
		w.report(call, "TYP101", "the dict of "+n.ID+"() may only be read: a subscript, get, keys, values, items, copy or in")
	}
}

// readsOnly reports whether the call, the parent of the name being visited,
// is only read from.
func (w *subsetWalker) readsOnly(call *ast.Call) bool {
	switch p := w.parent(2).(type) {
	case *ast.Subscript:
		return p.Value == ast.Expr(call) && p.Ctx == ast.CtxLoad
	case *ast.Attribute:
		outer, ok := w.parent(3).(*ast.Call)
		return p.Value == ast.Expr(call) && ok && outer.Func == ast.Expr(p) && slices.Contains(readOnlyDict, p.Attr)
	case *ast.Compare:
		for i, x := range p.Comparators {
			if x == ast.Expr(call) {
				return p.Ops[i] == ast.CmpIn || p.Ops[i] == ast.CmpNotIn
			}
		}
	}
	return false
}

// parent returns the ancestor of the node being visited: 1 is its parent.
func (w *subsetWalker) parent(up int) ast.Node {
	if i := len(w.path) - up; i >= 0 {
		return w.path[i]
	}
	return nil
}

// attribute checks the attributes that expose frames or change the class.
func (w *subsetWalker) attribute(n *ast.Attribute) {
	switch {
	case n.Attr == "__class__" && n.Ctx == ast.CtxStore:
		w.report(n, "TYP113", "assigning to __class__ changes the layout of the object")
	case slices.Contains(frameAttrs, n.Attr):
		w.report(n, "TYP130", n.Attr+" exposes frames or the attribute dict, which the compiled program does not have")
	case n.Attr == "dataclass_transform":
		w.report(n, "TYP150", "dataclass_transform is not supported")
	default:
		w.unsupportedForm(n)
	}
}

// importFrom checks the names taken from sys.
func (w *subsetWalker) importFrom(n *ast.ImportFrom) {
	if n.Module == nil || *n.Module != "sys" {
		if n.Module != nil && (*n.Module == "typing" || *n.Module == "typing_extensions") {
			for _, a := range n.Names {
				if a.Name == "dataclass_transform" {
					w.report(a, "TYP150", "dataclass_transform is not supported")
				}
			}
		}
		return
	}
	for _, a := range n.Names {
		if slices.Contains(frameAttrs, a.Name) {
			w.report(a, "TYP130", a.Name+" exposes frames, which the compiled program does not have")
		}
	}
}

// call checks the keyword forms of a call: a TypeVar with a declared variance.
func (w *subsetWalker) call(n *ast.Call) {
	sc := w.scope()
	if sc == nil || w.c.p.decl.formOfExpr(n.Func, sc) != formTypeVar {
		return
	}
	for _, k := range n.Keywords {
		if k.Arg != nil && slices.Contains(variance, *k.Arg) {
			w.report(k, "TYP150", "a declared variance is not supported")
		}
	}
}

// unsupportedForm reports the typing forms the checker does not support.
func (w *subsetWalker) unsupportedForm(e ast.Expr) {
	sc := w.scope()
	if sc == nil {
		return
	}
	d := w.c.p.decl
	switch d.formOfExpr(e, sc) {
	case formParamSpec, formTypeVarTuple:
		w.report(e, "TYP150", "ParamSpec and TypeVarTuple are not supported")
	case formNewType:
		w.report(e, "TYP150", "NewType is not supported")
	case formTypedDict:
		w.report(e, "TYP150", "TypedDict is not supported")
	default:
		if tn, ok := d.entity(e, sc).(*TypeName); ok && tn.Name() == "Coroutine" {
			if src := w.c.srcs[tn.Module()]; src == nil || src.stub {
				w.report(e, "TYP150", "Coroutine is not supported")
			}
		}
	}
}

// deleteTargets reports del of an attribute: it removes a name from the
// layout of the class.
func (w *subsetWalker) deleteTargets(targets []ast.Expr) {
	for _, t := range targets {
		switch t := t.(type) {
		case *ast.Attribute:
			w.report(t, "TYP110", "deleting an attribute changes the layout of the class")
		case *ast.Tuple:
			w.deleteTargets(t.Elts)
		case *ast.List:
			w.deleteTargets(t.Elts)
		}
	}
}

// class checks a class statement: where it is, its decorators, its
// metaclass and its body.
func (w *subsetWalker) class(cd *ast.ClassDef) {
	switch sc := w.scope(); {
	case sc != nil && sc.Kind() != ModuleScope:
		w.report(cd, "TYP150", "a class defined inside a function or another class is not supported")
	case len(cd.DecoratorList) > 0:
		w.report(cd, "TYP150", "a decorated class is not supported")
	}
	if hasMetaclass(cd) {
		w.report(cd, "TYP112", "a metaclass is not supported")
	}
	for _, st := range cd.Body {
		w.classStatement(st)
	}
}

// classStatement allows in a class body the methods, class variables, pass
// and the docstring that the layout of the class is built from.
func (w *subsetWalker) classStatement(st ast.Stmt) {
	switch st := st.(type) {
	case *ast.FunctionDef:
		if st.Name == "__init_subclass__" {
			w.initSubclass(st)
		}
	case *ast.Pass, *ast.ClassDef:
	case *ast.Assign:
		for _, t := range st.Targets {
			if _, ok := t.(*ast.Name); !ok {
				w.report(t, "TYP110", "this assignment target is not allowed in a class body")
			}
		}
	case *ast.AnnAssign:
		if _, ok := st.Target.(*ast.Name); !ok {
			w.report(st.Target, "TYP110", "this assignment target is not allowed in a class body")
		}
	case *ast.ExprStmt:
		if !isDocstring(st) && !isEllipsis(st) {
			w.report(st, "TYP110", "this statement is not allowed in a class body")
		}
	default:
		w.report(st, "TYP110", "this statement is not allowed in a class body")
	}
}

// initSubclass reports a store to the class a hook receives: the hook of a
// base class would change the layout of every subclass.
func (w *subsetWalker) initSubclass(fd *ast.FunctionDef) {
	params := append(append([]*ast.Arg{}, fd.Args.PosOnlyArgs...), fd.Args.Args...)
	if len(params) == 0 {
		return
	}
	cls := params[0].Arg
	ast.Inspect(fd, func(n ast.Node) bool {
		if a, ok := n.(*ast.Attribute); ok && a.Ctx != ast.CtxLoad {
			if v, ok := a.Value.(*ast.Name); ok && v.ID == cls {
				w.report(a, "TYP112", "__init_subclass__ that mutates the class is not supported")
			}
		}
		return true
	})
}

// rejectYield reports the first yield of a statement, not looking into the
// functions it defines: the VM cannot suspend inside try/finally or with.
func (w *subsetWalker) rejectYield(st ast.Stmt, msg string) {
	found := false
	ast.Inspect(st, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FunctionDef, *ast.AsyncFunctionDef, *ast.ClassDef, *ast.Lambda:
			return n == ast.Node(st)
		case *ast.Yield, *ast.YieldFrom:
			if !found {
				found = true
				w.report(n, "TYP150", msg)
			}
		}
		return !found
	})
}

// isBuiltinName reports whether a use of name resolves to the builtins: no
// enclosing scope of the program binds it.
func (w *subsetWalker) isBuiltinName(name string) bool {
	sc := w.scope()
	if sc == nil {
		return false
	}
	_, obj := sc.LookupParent(name)
	return obj == nil || obj == Universe.Lookup(name)
}

// scope is the scope that resolves the names of the node being visited.
func (w *subsetWalker) scope() *Scope {
	if len(w.scopes) == 0 {
		return nil
	}
	return w.scopes[len(w.scopes)-1]
}

// report adds a diagnostic covering n.
func (w *subsetWalker) report(n ast.Node, code, msg string) {
	c := w.c
	c.p.errs.Add(c.fset.Position(n.Pos()), c.fset.Position(n.End()), code, msg)
}

// isStrConstant reports whether e is a string literal.
func isStrConstant(e ast.Expr) bool {
	c, ok := e.(*ast.Constant)
	if !ok {
		return false
	}
	_, ok = c.Value.(ast.Str)
	return ok
}
