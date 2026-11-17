package ast

import "iter"

// Visitor is called by Walk for each node. If Visit returns a non-nil
// visitor w, Walk visits the children of the node with w, then calls
// w.Visit(nil).
type Visitor interface {
	Visit(node Node) (w Visitor)
}

type inspector func(Node) bool

// Walk traverses the tree rooted at node depth-first. Children are visited in
// the field order of Python.asdl, which is the order of CPython's
// ast.iter_child_nodes except that operators and expression contexts are not
// nodes here and are never visited. Absent optional children and the None
// keys of a Dict are skipped.
func Walk(v Visitor, node Node) {
	if v = v.Visit(node); v == nil {
		return
	}
	eachChild(node, func(c Node) { Walk(v, c) })
	v.Visit(nil)
}

// Inspect traverses the tree rooted at node in depth-first order, calling f
// for each node, then f(nil) after its children. If f returns false, the
// children of that node are skipped.
func Inspect(node Node, f func(Node) bool) {
	Walk(inspector(f), node)
}

// Preorder returns an iterator over node and its descendants in the order
// Walk visits them. Breaking out of the loop stops the traversal.
func Preorder(root Node) iter.Seq[Node] {
	return func(yield func(Node) bool) {
		var visit func(Node) bool
		visit = func(n Node) bool {
			if !yield(n) {
				return false
			}
			ok := true
			eachChild(n, func(c Node) {
				if ok {
					ok = visit(c)
				}
			})
			return ok
		}
		visit(root)
	}
}

func (f inspector) Visit(node Node) Visitor {
	if f(node) {
		return f
	}
	return nil
}

// eachChild calls f for each present child of n in ASDL field order.
func eachChild(node Node, f func(Node)) {
	switch n := node.(type) {
	case *Module:
		each(f, n.Body)
		each(f, n.TypeIgnores)
	case *Interactive:
		each(f, n.Body)
	case *Expression:
		if n.Body != nil {
			f(n.Body)
		}
	case *FunctionType:
		each(f, n.ArgTypes)
		if n.Returns != nil {
			f(n.Returns)
		}
	case *FunctionDef:
		if n.Args != nil {
			f(n.Args)
		}
		each(f, n.Body)
		each(f, n.DecoratorList)
		if n.Returns != nil {
			f(n.Returns)
		}
		each(f, n.TypeParams)
	case *AsyncFunctionDef:
		if n.Args != nil {
			f(n.Args)
		}
		each(f, n.Body)
		each(f, n.DecoratorList)
		if n.Returns != nil {
			f(n.Returns)
		}
		each(f, n.TypeParams)
	case *ClassDef:
		each(f, n.Bases)
		each(f, n.Keywords)
		each(f, n.Body)
		each(f, n.DecoratorList)
		each(f, n.TypeParams)
	case *Return:
		if n.Value != nil {
			f(n.Value)
		}
	case *Delete:
		each(f, n.Targets)
	case *Assign:
		each(f, n.Targets)
		if n.Value != nil {
			f(n.Value)
		}
	case *TypeAlias:
		if n.Name != nil {
			f(n.Name)
		}
		each(f, n.TypeParams)
		if n.Value != nil {
			f(n.Value)
		}
	case *AugAssign:
		if n.Target != nil {
			f(n.Target)
		}
		if n.Value != nil {
			f(n.Value)
		}
	case *AnnAssign:
		if n.Target != nil {
			f(n.Target)
		}
		if n.Annotation != nil {
			f(n.Annotation)
		}
		if n.Value != nil {
			f(n.Value)
		}
	case *For:
		if n.Target != nil {
			f(n.Target)
		}
		if n.Iter != nil {
			f(n.Iter)
		}
		each(f, n.Body)
		each(f, n.OrElse)
	case *AsyncFor:
		if n.Target != nil {
			f(n.Target)
		}
		if n.Iter != nil {
			f(n.Iter)
		}
		each(f, n.Body)
		each(f, n.OrElse)
	case *While:
		if n.Test != nil {
			f(n.Test)
		}
		each(f, n.Body)
		each(f, n.OrElse)
	case *If:
		if n.Test != nil {
			f(n.Test)
		}
		each(f, n.Body)
		each(f, n.OrElse)
	case *With:
		each(f, n.Items)
		each(f, n.Body)
	case *AsyncWith:
		each(f, n.Items)
		each(f, n.Body)
	case *Match:
		if n.Subject != nil {
			f(n.Subject)
		}
		each(f, n.Cases)
	case *Raise:
		if n.Exc != nil {
			f(n.Exc)
		}
		if n.Cause != nil {
			f(n.Cause)
		}
	case *Try:
		each(f, n.Body)
		each(f, n.Handlers)
		each(f, n.OrElse)
		each(f, n.FinalBody)
	case *TryStar:
		each(f, n.Body)
		each(f, n.Handlers)
		each(f, n.OrElse)
		each(f, n.FinalBody)
	case *Assert:
		if n.Test != nil {
			f(n.Test)
		}
		if n.Msg != nil {
			f(n.Msg)
		}
	case *Import:
		each(f, n.Names)
	case *ImportFrom:
		each(f, n.Names)
	case *Global:
	case *Nonlocal:
	case *ExprStmt:
		if n.Value != nil {
			f(n.Value)
		}
	case *Pass:
	case *Break:
	case *Continue:
	case *BoolOp:
		each(f, n.Values)
	case *NamedExpr:
		if n.Target != nil {
			f(n.Target)
		}
		if n.Value != nil {
			f(n.Value)
		}
	case *BinOp:
		if n.Left != nil {
			f(n.Left)
		}
		if n.Right != nil {
			f(n.Right)
		}
	case *UnaryOp:
		if n.Operand != nil {
			f(n.Operand)
		}
	case *Lambda:
		if n.Args != nil {
			f(n.Args)
		}
		if n.Body != nil {
			f(n.Body)
		}
	case *IfExp:
		if n.Test != nil {
			f(n.Test)
		}
		if n.Body != nil {
			f(n.Body)
		}
		if n.OrElse != nil {
			f(n.OrElse)
		}
	case *Dict:
		each(f, n.Keys)
		each(f, n.Values)
	case *Set:
		each(f, n.Elts)
	case *ListComp:
		if n.Elt != nil {
			f(n.Elt)
		}
		each(f, n.Generators)
	case *SetComp:
		if n.Elt != nil {
			f(n.Elt)
		}
		each(f, n.Generators)
	case *DictComp:
		if n.Key != nil {
			f(n.Key)
		}
		if n.Value != nil {
			f(n.Value)
		}
		each(f, n.Generators)
	case *GeneratorExp:
		if n.Elt != nil {
			f(n.Elt)
		}
		each(f, n.Generators)
	case *Await:
		if n.Value != nil {
			f(n.Value)
		}
	case *Yield:
		if n.Value != nil {
			f(n.Value)
		}
	case *YieldFrom:
		if n.Value != nil {
			f(n.Value)
		}
	case *Compare:
		if n.Left != nil {
			f(n.Left)
		}
		each(f, n.Comparators)
	case *Call:
		if n.Func != nil {
			f(n.Func)
		}
		each(f, n.Args)
		each(f, n.Keywords)
	case *FormattedValue:
		if n.Value != nil {
			f(n.Value)
		}
		if n.FormatSpec != nil {
			f(n.FormatSpec)
		}
	case *Interpolation:
		if n.Value != nil {
			f(n.Value)
		}
		if n.FormatSpec != nil {
			f(n.FormatSpec)
		}
	case *JoinedStr:
		each(f, n.Values)
	case *TemplateStr:
		each(f, n.Values)
	case *Constant:
	case *Attribute:
		if n.Value != nil {
			f(n.Value)
		}
	case *Subscript:
		if n.Value != nil {
			f(n.Value)
		}
		if n.Slice != nil {
			f(n.Slice)
		}
	case *Starred:
		if n.Value != nil {
			f(n.Value)
		}
	case *Name:
	case *List:
		each(f, n.Elts)
	case *Tuple:
		each(f, n.Elts)
	case *Slice:
		if n.Lower != nil {
			f(n.Lower)
		}
		if n.Upper != nil {
			f(n.Upper)
		}
		if n.Step != nil {
			f(n.Step)
		}
	case *MatchValue:
		if n.Value != nil {
			f(n.Value)
		}
	case *MatchSingleton:
	case *MatchSequence:
		each(f, n.Patterns)
	case *MatchMapping:
		each(f, n.Keys)
		each(f, n.Patterns)
	case *MatchClass:
		if n.Cls != nil {
			f(n.Cls)
		}
		each(f, n.Patterns)
		each(f, n.KwdPatterns)
	case *MatchStar:
	case *MatchAs:
		if n.Pattern != nil {
			f(n.Pattern)
		}
	case *MatchOr:
		each(f, n.Patterns)
	case *TypeVar:
		if n.Bound != nil {
			f(n.Bound)
		}
		if n.DefaultValue != nil {
			f(n.DefaultValue)
		}
	case *ParamSpec:
		if n.DefaultValue != nil {
			f(n.DefaultValue)
		}
	case *TypeVarTuple:
		if n.DefaultValue != nil {
			f(n.DefaultValue)
		}
	case *Comprehension:
		if n.Target != nil {
			f(n.Target)
		}
		if n.Iter != nil {
			f(n.Iter)
		}
		each(f, n.Ifs)
	case *ExceptHandler:
		if n.Type != nil {
			f(n.Type)
		}
		each(f, n.Body)
	case *Arguments:
		each(f, n.PosOnlyArgs)
		each(f, n.Args)
		if n.VarArg != nil {
			f(n.VarArg)
		}
		each(f, n.KwOnlyArgs)
		each(f, n.KwDefaults)
		if n.KwArg != nil {
			f(n.KwArg)
		}
		each(f, n.Defaults)
	case *Arg:
		if n.Annotation != nil {
			f(n.Annotation)
		}
	case *Keyword:
		if n.Value != nil {
			f(n.Value)
		}
	case *Alias:
	case *WithItem:
		if n.ContextExpr != nil {
			f(n.ContextExpr)
		}
		if n.OptionalVars != nil {
			f(n.OptionalVars)
		}
	case *MatchCase:
		if n.Pattern != nil {
			f(n.Pattern)
		}
		if n.Guard != nil {
			f(n.Guard)
		}
		each(f, n.Body)
	case *TypeIgnore:
	}
}

func each[T Node](f func(Node), nodes []T) {
	for _, n := range nodes {
		if any(n) != nil {
			f(n)
		}
	}
}
