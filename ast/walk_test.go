package ast_test

import (
	"reflect"
	"testing"

	"github.com/siyul-park/spry/ast"
	"github.com/stretchr/testify/require"
)

// recorder logs Visit calls: the node's CPython name, or "end" for the nil
// call that closes a node. It stops descending into nodes named in prune.
type recorder struct {
	log   *[]string
	prune string
}

func (r recorder) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		*r.log = append(*r.log, "end")
		return nil
	}
	*r.log = append(*r.log, pyName(n))
	if pyName(n) == r.prune {
		return nil
	}
	return r
}

// tree is "x = f(a, k=1)" followed by a dict with a ** entry.
func tree() *ast.Module {
	return &ast.Module{Body: []ast.Stmt{
		&ast.Assign{
			Targets: []ast.Expr{&ast.Name{ID: "x", Ctx: ast.CtxStore}},
			Value: &ast.Call{
				Func:     &ast.Name{ID: "f"},
				Args:     []ast.Expr{&ast.Name{ID: "a"}},
				Keywords: []*ast.Keyword{{Arg: new(string), Value: &ast.Constant{Value: ast.Int{}}}},
			},
		},
		&ast.ExprStmt{Value: &ast.Dict{Keys: []ast.Expr{nil}, Values: []ast.Expr{&ast.Name{ID: "m"}}}},
	}}
}

func TestWalk(t *testing.T) {
	t.Run("visits nodes depth first in field order and closes each with nil", func(t *testing.T) {
		var log []string
		ast.Walk(recorder{log: &log}, tree())
		require.Equal(t, []string{
			"Module", "Assign", "Name", "end", "Call", "Name", "end", "Name", "end", "keyword", "Constant", "end", "end", "end", "end",
			"Expr", "Dict", "Name", "end", "end", "end", "end",
		}, log)
	})

	t.Run("skips the children of a node when Visit returns nil", func(t *testing.T) {
		var log []string
		ast.Walk(recorder{log: &log, prune: "Call"}, tree())
		require.Equal(t, []string{"Module", "Assign", "Name", "end", "Call", "end", "Expr", "Dict", "Name", "end", "end", "end", "end"}, log)
	})
}

func TestInspect(t *testing.T) {
	t.Run("calls f for every node, then nil after its children", func(t *testing.T) {
		var log []string
		ast.Inspect(&ast.Expression{Body: &ast.Attribute{Value: &ast.Name{ID: "a"}, Attr: "b"}}, func(n ast.Node) bool {
			if n == nil {
				log = append(log, "end")
				return false
			}
			log = append(log, pyName(n))
			return true
		})
		require.Equal(t, []string{"Expression", "Attribute", "Name", "end", "end", "end"}, log)
	})

	t.Run("false skips the children", func(t *testing.T) {
		var names []string
		ast.Inspect(tree(), func(n ast.Node) bool {
			if n != nil {
				names = append(names, pyName(n))
			}
			_, isCall := n.(*ast.Call)
			return !isCall
		})
		require.Equal(t, []string{"Module", "Assign", "Name", "Call", "Expr", "Dict", "Name"}, names)
	})
}

func TestPreorder(t *testing.T) {
	t.Run("yields nodes in Walk order", func(t *testing.T) {
		var names []string
		for n := range ast.Preorder(tree()) {
			names = append(names, pyName(n))
		}
		require.Equal(t, []string{"Module", "Assign", "Name", "Call", "Name", "Name", "keyword", "Constant", "Expr", "Dict", "Name"}, names)
	})

	t.Run("break stops the traversal", func(t *testing.T) {
		var names []string
		for n := range ast.Preorder(tree()) {
			names = append(names, pyName(n))
			if len(names) == 3 {
				break
			}
		}
		require.Equal(t, []string{"Module", "Assign", "Name"}, names)
	})

	t.Run("a node with every field set yields each child in ASDL field order", func(t *testing.T) {
		for _, node := range allNodes() {
			v := reflect.ValueOf(node).Elem()
			var want []ast.Node
			for i := range v.NumField() {
				f := v.Field(i)
				switch f.Kind() {
				case reflect.Slice:
					if f.Type().Elem().Implements(reflect.TypeFor[ast.Node]()) {
						f.Set(reflect.MakeSlice(f.Type(), 2, 2))
						for j := range 2 {
							f.Index(j).Set(leaf(t, f.Type().Elem()))
							want = append(want, f.Index(j).Interface().(ast.Node))
						}
					}
				case reflect.Interface, reflect.Pointer:
					if f.Type().Implements(reflect.TypeFor[ast.Node]()) {
						f.Set(leaf(t, f.Type()))
						want = append(want, f.Interface().(ast.Node))
					}
				}
			}
			var got []ast.Node
			for n := range ast.Preorder(node) {
				got = append(got, n)
			}
			require.Equal(t, append([]ast.Node{node}, want...), got, pyName(node))
		}
	})
}

// leaf returns a childless node assignable to typ.
func leaf(t *testing.T, typ reflect.Type) reflect.Value {
	t.Helper()
	switch typ {
	case reflect.TypeFor[ast.Expr]():
		return reflect.ValueOf(&ast.Name{})
	case reflect.TypeFor[ast.Stmt]():
		return reflect.ValueOf(&ast.Pass{})
	case reflect.TypeFor[ast.Pattern]():
		return reflect.ValueOf(&ast.MatchAs{})
	case reflect.TypeFor[ast.TypeParam]():
		return reflect.ValueOf(&ast.TypeVar{})
	}
	return reflect.New(typ.Elem())
}
