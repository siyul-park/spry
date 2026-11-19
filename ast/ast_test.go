package ast_test

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/token"
	"github.com/stretchr/testify/require"
)

type schemaNode struct {
	Name       string   `json:"name"`
	Category   string   `json:"category"`
	Attributes []string `json:"attributes"`
	Fields     []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"fields"`
}

// goNames maps the Go names that differ from CPython's to the CPython class.
var goNames = map[string]string{
	"ExprStmt": "Expr", "Alias": "alias", "Arg": "arg", "Arguments": "arguments",
	"Comprehension": "comprehension", "Keyword": "keyword", "MatchCase": "match_case", "WithItem": "withitem",
}

var operatorCategories = map[string]bool{"operator": true, "unaryop": true, "cmpop": true, "boolop": true, "expr_context": true}

// allNodes returns one zero value of every node struct.
func allNodes() []ast.Node {
	return []ast.Node{
		&ast.Module{},
		&ast.Interactive{},
		&ast.Expression{},
		&ast.FunctionType{},
		&ast.FunctionDef{},
		&ast.AsyncFunctionDef{},
		&ast.ClassDef{},
		&ast.Return{},
		&ast.Delete{},
		&ast.Assign{},
		&ast.TypeAlias{},
		&ast.AugAssign{},
		&ast.AnnAssign{},
		&ast.For{},
		&ast.AsyncFor{},
		&ast.While{},
		&ast.If{},
		&ast.With{},
		&ast.AsyncWith{},
		&ast.Match{},
		&ast.Raise{},
		&ast.Try{},
		&ast.TryStar{},
		&ast.Assert{},
		&ast.Import{},
		&ast.ImportFrom{},
		&ast.Global{},
		&ast.Nonlocal{},
		&ast.ExprStmt{},
		&ast.Pass{},
		&ast.Break{},
		&ast.Continue{},
		&ast.BoolOp{},
		&ast.NamedExpr{},
		&ast.BinOp{},
		&ast.UnaryOp{},
		&ast.Lambda{},
		&ast.IfExp{},
		&ast.Dict{},
		&ast.Set{},
		&ast.ListComp{},
		&ast.SetComp{},
		&ast.DictComp{},
		&ast.GeneratorExp{},
		&ast.Await{},
		&ast.Yield{},
		&ast.YieldFrom{},
		&ast.Compare{},
		&ast.Call{},
		&ast.FormattedValue{},
		&ast.Interpolation{},
		&ast.JoinedStr{},
		&ast.TemplateStr{},
		&ast.Constant{},
		&ast.Attribute{},
		&ast.Subscript{},
		&ast.Starred{},
		&ast.Name{},
		&ast.List{},
		&ast.Tuple{},
		&ast.Slice{},
		&ast.MatchValue{},
		&ast.MatchSingleton{},
		&ast.MatchSequence{},
		&ast.MatchMapping{},
		&ast.MatchClass{},
		&ast.MatchStar{},
		&ast.MatchAs{},
		&ast.MatchOr{},
		&ast.TypeVar{},
		&ast.ParamSpec{},
		&ast.TypeVarTuple{},
		&ast.Comprehension{},
		&ast.ExceptHandler{},
		&ast.Arguments{},
		&ast.Arg{},
		&ast.Keyword{},
		&ast.Alias{},
		&ast.WithItem{},
		&ast.MatchCase{},
		&ast.TypeIgnore{},
	}
}

func pyName(n ast.Node) string {
	name := reflect.TypeOf(n).Elem().Name()
	if py, ok := goNames[name]; ok {
		return py
	}
	return name
}

func pyField(f reflect.StructField) string {
	if tag, ok := f.Tag.Lookup("py"); ok {
		return tag
	}
	return strings.ToLower(f.Name[:1]) + f.Name[1:]
}

// goType is the Go type an ASDL field type maps to.
func goType(node, asdl string) reflect.Type {
	optional := strings.HasSuffix(asdl, " | None")
	asdl = strings.TrimSuffix(asdl, " | None")
	if elem, ok := strings.CutPrefix(asdl, "list["); ok {
		return reflect.SliceOf(goType(node, strings.TrimSuffix(elem, "]")))
	}
	var t reflect.Type
	switch asdl {
	case "expr":
		t = reflect.TypeFor[ast.Expr]()
	case "stmt":
		t = reflect.TypeFor[ast.Stmt]()
	case "pattern":
		t = reflect.TypeFor[ast.Pattern]()
	case "type_param":
		t = reflect.TypeFor[ast.TypeParam]()
	case "arguments":
		t = reflect.TypeFor[*ast.Arguments]()
	case "arg":
		t = reflect.TypeFor[*ast.Arg]()
	case "alias":
		t = reflect.TypeFor[*ast.Alias]()
	case "keyword":
		t = reflect.TypeFor[*ast.Keyword]()
	case "withitem":
		t = reflect.TypeFor[*ast.WithItem]()
	case "match_case":
		t = reflect.TypeFor[*ast.MatchCase]()
	case "comprehension":
		t = reflect.TypeFor[*ast.Comprehension]()
	case "excepthandler":
		t = reflect.TypeFor[*ast.ExceptHandler]()
	case "type_ignore":
		t = reflect.TypeFor[*ast.TypeIgnore]()
	case "operator":
		t = reflect.TypeFor[ast.Operator]()
	case "unaryop":
		t = reflect.TypeFor[ast.UnaryOperator]()
	case "cmpop":
		t = reflect.TypeFor[ast.CmpOperator]()
	case "boolop":
		t = reflect.TypeFor[ast.BoolOperator]()
	case "expr_context":
		t = reflect.TypeFor[ast.ExprContext]()
	case "str":
		t = reflect.TypeFor[string]()
	case "int":
		t = reflect.TypeFor[int]()
	case "object":
		t = reflect.TypeFor[ast.Value]()
		if node == "Interpolation" {
			t = reflect.TypeFor[string]()
		}
	}
	if optional && (t.Kind() == reflect.String || t.Kind() == reflect.Int) {
		return reflect.PointerTo(t)
	}
	return t
}

func TestNode(t *testing.T) {
	schema := loadSchema(t)
	byName := map[string]schemaNode{}
	for _, n := range schema {
		byName[n.Name] = n
	}
	nodes := allNodes()

	t.Run("structs are exactly the schema nodes without operators", func(t *testing.T) {
		want := map[string]bool{}
		for _, n := range schema {
			if !operatorCategories[n.Category] && n.Name != "slice" {
				want[n.Name] = true
			}
		}
		got := map[string]bool{}
		for _, n := range nodes {
			got[pyName(n)] = true
		}
		require.Equal(t, want, got)
		require.Len(t, nodes, len(got))
	})

	t.Run("fields follow ASDL order and types", func(t *testing.T) {
		for _, n := range nodes {
			want := byName[pyName(n)]
			typ := reflect.TypeOf(n).Elem()
			var names []string
			var wantNames []string
			hasSpan := false
			for i := range typ.NumField() {
				f := typ.Field(i)
				if f.Type == reflect.TypeFor[ast.Span]() {
					hasSpan = f.Anonymous
					continue
				}
				names = append(names, pyField(f))
				if len(names) <= len(want.Fields) {
					require.Equal(t, goType(want.Name, want.Fields[len(names)-1].Type), f.Type, "%s.%s", want.Name, f.Name)
				}
			}
			for _, f := range want.Fields {
				wantNames = append(wantNames, f.Name)
			}
			require.Equal(t, wantNames, names, want.Name)
			require.Equal(t, len(want.Attributes) > 0, hasSpan, "%s Span", want.Name)
		}
	})

	t.Run("each node implements exactly its category", func(t *testing.T) {
		for _, n := range nodes {
			var got []string
			if _, ok := n.(ast.Mod); ok {
				got = append(got, "mod")
			}
			if _, ok := n.(ast.Stmt); ok {
				got = append(got, "stmt")
			}
			if _, ok := n.(ast.Expr); ok {
				got = append(got, "expr")
			}
			if _, ok := n.(ast.Pattern); ok {
				got = append(got, "pattern")
			}
			if _, ok := n.(ast.TypeParam); ok {
				got = append(got, "type_param")
			}
			want := byName[pyName(n)].Category
			switch want {
			case "excepthandler", "type_ignore", "":
				require.Empty(t, got, pyName(n))
			default:
				require.Equal(t, []string{want}, got, pyName(n))
			}
		}
	})

	t.Run("nodes without a span derive their position from children", func(t *testing.T) {
		a := &ast.Name{Span: ast.Span{StartPos: 3, EndPos: 4}}
		b := &ast.Name{Span: ast.Span{StartPos: 8, EndPos: 12}}
		stmtA := &ast.ExprStmt{Span: a.Span, Value: a}
		stmtB := &ast.ExprStmt{Span: b.Span, Value: b}
		items := []ast.Node{
			&ast.Module{Body: []ast.Stmt{stmtA, stmtB}},
			&ast.Interactive{Body: []ast.Stmt{stmtA, stmtB}},
			&ast.Expression{Body: &ast.BinOp{Span: ast.Span{StartPos: 3, EndPos: 12}, Left: a, Right: b}},
			&ast.FunctionType{ArgTypes: []ast.Expr{a}, Returns: b},
			&ast.Arguments{Args: []*ast.Arg{{Span: ast.Span{StartPos: 3, EndPos: 4}}}, Defaults: []ast.Expr{b}},
			&ast.Comprehension{Target: a, Iter: b},
			&ast.WithItem{ContextExpr: a, OptionalVars: b},
			&ast.MatchCase{Pattern: &ast.MatchAs{Span: ast.Span{StartPos: 3, EndPos: 4}}, Body: []ast.Stmt{stmtB}},
		}
		for _, n := range items {
			require.Equal(t, token.Pos(3), n.Pos(), pyName(n))
			require.Equal(t, token.Pos(12), n.End(), pyName(n))
		}
		for _, n := range []ast.Node{&ast.Module{}, &ast.Arguments{}, &ast.TypeIgnore{Lineno: 4}} {
			require.Equal(t, token.NoPos, n.Pos(), pyName(n))
			require.Equal(t, token.NoPos, n.End(), pyName(n))
		}
	})
}

func TestSpan(t *testing.T) {
	s := ast.Span{StartPos: 5, EndPos: 9}
	require.Equal(t, token.Pos(5), s.Pos())
	require.Equal(t, token.Pos(9), s.End())
}

func TestLocate(t *testing.T) {
	src := "x = 'é'\nyz = (1 +\n 2)\n"
	fset := token.NewFileSet()
	file := fset.AddFile("t.py", -1, len(src))
	file.SetLines([]int{0, 9, 19})
	span := func(start, end int) ast.Span { return ast.Span{StartPos: file.Pos(start), EndPos: file.Pos(end)} }

	tests := []struct {
		name string
		node ast.Node
		want ast.Location
	}{
		{"first line", &ast.Name{Span: span(0, 1)}, ast.Location{Lineno: 1, ColOffset: 0, EndLineno: 1, EndColOffset: 1}},
		{"columns count bytes", &ast.Constant{Span: span(4, 8)}, ast.Location{Lineno: 1, ColOffset: 4, EndLineno: 1, EndColOffset: 8}},
		{"later line", &ast.Name{Span: span(9, 11)}, ast.Location{Lineno: 2, ColOffset: 0, EndLineno: 2, EndColOffset: 2}},
		{"multi-line", &ast.BinOp{Span: span(15, 21)}, ast.Location{Lineno: 2, ColOffset: 6, EndLineno: 3, EndColOffset: 2}},
		{"no position", &ast.Name{}, ast.Location{}},
		{"no end", &ast.Name{Span: ast.Span{StartPos: file.Pos(9)}}, ast.Location{Lineno: 2}},
		{"derived from children", &ast.Expression{Body: &ast.Name{Span: span(9, 11)}}, ast.Location{Lineno: 2, ColOffset: 0, EndLineno: 2, EndColOffset: 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ast.Locate(fset, tt.node))
		})
	}
}

func TestAppendRune(t *testing.T) {
	tests := []struct {
		name string
		r    rune
		want []byte
	}{
		{"ascii", 'a', []byte("a")},
		{"two bytes", 'é', []byte("é")},
		{"astral", 0x1F600, []byte("😀")},
		{"low surrogate", 0xD800, []byte{0xED, 0xA0, 0x80}},
		{"high surrogate", 0xDFFF, []byte{0xED, 0xBF, 0xBF}},
		{"out of range", 0x110000, []byte("�")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, append([]byte("<"), tt.want...), ast.AppendRune([]byte("<"), tt.r))
		})
	}
}

func TestOperator_String(t *testing.T) {
	names := []string{"Add", "Sub", "Mult", "MatMult", "Div", "Mod", "Pow", "LShift", "RShift", "BitOr", "BitXor", "BitAnd", "FloorDiv"}
	testEnumString(t, "operator", names, func(i int) string { return ast.Operator(i).String() })
	require.Equal(t, ast.OpAdd, ast.Operator(0))
	require.Equal(t, ast.OpFloorDiv, ast.Operator(12))
	require.Equal(t, "Operator(13)", ast.Operator(13).String())
	require.Equal(t, "Operator(-1)", ast.Operator(-1).String())
}

func TestUnaryOperator_String(t *testing.T) {
	names := []string{"Invert", "Not", "UAdd", "USub"}
	testEnumString(t, "unaryop", names, func(i int) string { return ast.UnaryOperator(i).String() })
	require.Equal(t, ast.UnaryUSub, ast.UnaryOperator(3))
	require.Equal(t, "UnaryOperator(4)", ast.UnaryOperator(4).String())
}

func TestCmpOperator_String(t *testing.T) {
	names := []string{"Eq", "NotEq", "Lt", "LtE", "Gt", "GtE", "Is", "IsNot", "In", "NotIn"}
	testEnumString(t, "cmpop", names, func(i int) string { return ast.CmpOperator(i).String() })
	require.Equal(t, ast.CmpNotIn, ast.CmpOperator(9))
	require.Equal(t, "CmpOperator(10)", ast.CmpOperator(10).String())
}

func TestBoolOperator_String(t *testing.T) {
	testEnumString(t, "boolop", []string{"And", "Or"}, func(i int) string { return ast.BoolOperator(i).String() })
	require.Equal(t, ast.BoolOr, ast.BoolOperator(1))
	require.Equal(t, "BoolOperator(2)", ast.BoolOperator(2).String())
}

func TestExprContext_String(t *testing.T) {
	testEnumString(t, "expr_context", []string{"Load", "Store", "Del"}, func(i int) string { return ast.ExprContext(i).String() })
	require.Equal(t, ast.CtxLoad, ast.ExprContext(0), "the zero value is Load")
	require.Equal(t, ast.CtxDel, ast.ExprContext(2))
	require.Equal(t, "ExprContext(3)", ast.ExprContext(3).String())
}

func testEnumString(t *testing.T, category string, names []string, str func(i int) string) {
	t.Helper()
	var want []string
	for _, n := range loadSchema(t) {
		if n.Category == category {
			want = append(want, n.Name)
		}
	}
	require.ElementsMatch(t, want, names)
	for i, name := range names {
		require.Equal(t, name, str(i))
	}
}

func loadSchema(t *testing.T) []schemaNode {
	t.Helper()
	raw, err := os.ReadFile("testdata/cpython/schema.json")
	require.NoError(t, err)
	var schema struct {
		Nodes []schemaNode `json:"nodes"`
	}
	require.NoError(t, json.Unmarshal(raw, &schema))
	return schema.Nodes
}
