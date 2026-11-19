package ast_test

import (
	"bufio"
	"encoding/json"
	"math"
	"math/big"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/token"
	"github.com/stretchr/testify/require"
)

// reprCase is a line of testdata/cpython/repr.jsonl: the Python repr of a
// constant, with the value as code points, bytes or float.hex strings.
type reprCase struct {
	Kind  string `json:"kind"`
	Repr  string `json:"repr"`
	Value string `json:"value"`
}

func (c reprCase) constant(t *testing.T) ast.Value {
	t.Helper()
	parseFloat := func(s string) float64 {
		f, err := strconv.ParseFloat(s, 64)
		require.NoError(t, err, s)
		return f
	}
	switch c.Kind {
	case "str":
		var b []byte
		for _, hex := range strings.Fields(c.Value) {
			r, err := strconv.ParseInt(hex, 16, 32)
			require.NoError(t, err)
			b = ast.AppendRune(b, rune(r))
		}
		return ast.Str(b)
	case "bytes":
		b := make([]byte, len(c.Value)/2)
		for i := range b {
			v, err := strconv.ParseUint(c.Value[2*i:2*i+2], 16, 8)
			require.NoError(t, err)
			b[i] = byte(v)
		}
		return ast.Bytes(b)
	case "float":
		return ast.Float(parseFloat(c.Value))
	default:
		re, im, _ := strings.Cut(c.Value, ",")
		return ast.Complex(complex(parseFloat(re), parseFloat(im)))
	}
}

func TestDump(t *testing.T) {
	fset := token.NewFileSet()
	file := fset.AddFile("t.py", -1, 6)
	file.SetLines([]int{0})
	span := func(start, end int) ast.Span { return ast.Span{StartPos: file.Pos(start), EndPos: file.Pos(end)} }
	str := func(s string) *string { return &s }
	zero := 0

	name := &ast.Name{ID: "x"}
	hi := &ast.Constant{Value: ast.Str("hi")}
	call := &ast.Call{Func: &ast.Name{ID: "print"}, Args: []ast.Expr{hi}}
	module := &ast.Module{Body: []ast.Stmt{&ast.ExprStmt{Value: call}}}
	located := &ast.Module{Body: []ast.Stmt{&ast.Assign{
		Span:    span(0, 5),
		Targets: []ast.Expr{&ast.Name{Span: span(0, 1), ID: "x", Ctx: ast.CtxStore}},
		Value:   &ast.Constant{Span: span(4, 5), Value: ast.Int{N: big.NewInt(1)}},
	}}}

	tests := []struct {
		name string
		node ast.Node
		opts []ast.DumpOption
		want string
	}{
		{"default omits empty lists and absent fields", module, nil,
			"Module(body=[Expr(value=Call(func=Name(id='print', ctx=Load()), args=[Constant(value='hi')]))])"},
		{"indent", module, []ast.DumpOption{ast.WithIndent("  ")},
			"Module(\n  body=[\n    Expr(\n      value=Call(\n        func=Name(id='print', ctx=Load()),\n        args=[\n          Constant(value='hi')]))])"},
		{"indent string", &ast.Expression{Body: &ast.BinOp{
			Left: &ast.Name{ID: "a"}, Op: ast.OpAdd,
			Right: &ast.BinOp{Left: &ast.Name{ID: "b"}, Op: ast.OpMult, Right: &ast.Name{ID: "c"}}}},
			[]ast.DumpOption{ast.WithIndent("\t")},
			"Expression(\n\tbody=BinOp(\n\t\tleft=Name(id='a', ctx=Load()),\n\t\top=Add(),\n\t\tright=BinOp(\n\t\t\tleft=Name(id='b', ctx=Load()),\n\t\t\top=Mult(),\n\t\t\tright=Name(id='c', ctx=Load()))))"},
		{"show empty", module, []ast.DumpOption{ast.WithEmpty()},
			"Module(body=[Expr(value=Call(func=Name(id='print', ctx=Load()), args=[Constant(value='hi')], keywords=[]))], type_ignores=[])"},
		{"show empty still omits absent", &ast.Return{}, []ast.DumpOption{ast.WithEmpty()}, "Return()"},
		{"without field names", &ast.Assign{Targets: []ast.Expr{&ast.Name{ID: "x", Ctx: ast.CtxStore}}, Value: &ast.Constant{Value: ast.Int{N: big.NewInt(1)}}},
			[]ast.DumpOption{ast.WithoutFieldNames()}, "Assign([Name('x', Store())], Constant(1))"},
		{"without field names flushes empty lists before a later field",
			&ast.Call{Func: name, Keywords: []*ast.Keyword{{Arg: str("k"), Value: &ast.Constant{Value: ast.Int{N: big.NewInt(1)}}}}},
			[]ast.DumpOption{ast.WithoutFieldNames()}, "Call(Name('x', Load()), [], [keyword('k', Constant(1))])"},
		{"without field names drops trailing empty lists", &ast.Call{Func: name},
			[]ast.DumpOption{ast.WithoutFieldNames()}, "Call(Name('x', Load()))"},
		{"without field names names fields after an absent one", &ast.AnnAssign{Target: name, Annotation: name, Simple: 1},
			[]ast.DumpOption{ast.WithoutFieldNames()}, "AnnAssign(Name('x', Load()), Name('x', Load()), simple=1)"},
		{"absent dict key prints None", &ast.Dict{Keys: []ast.Expr{nil, hi}, Values: []ast.Expr{name, hi}}, nil,
			"Dict(keys=[None, Constant(value='hi')], values=[Name(id='x', ctx=Load()), Constant(value='hi')])"},
		{"operators and comparison lists", &ast.UnaryOp{Op: ast.UnaryNot, Operand: &ast.Compare{Left: name, Ops: []ast.CmpOperator{ast.CmpLt, ast.CmpNotIn}, Comparators: []ast.Expr{name, name}}}, nil,
			"UnaryOp(op=Not(), operand=Compare(left=Name(id='x', ctx=Load()), ops=[Lt(), NotIn()], comparators=[Name(id='x', ctx=Load()), Name(id='x', ctx=Load())]))"},
		{"boolean operator", &ast.BoolOp{Op: ast.BoolAnd, Values: []ast.Expr{name}}, nil,
			"BoolOp(op=And(), values=[Name(id='x', ctx=Load())])"},
		{"import level zero is shown", &ast.ImportFrom{Names: []*ast.Alias{{Name: "x"}}, Level: &zero}, nil,
			"ImportFrom(names=[alias(name='x')], level=0)"},
		{"import level and module absent", &ast.ImportFrom{Module: str("m"), Names: []*ast.Alias{{Name: "x", AsName: str("y")}}}, nil,
			"ImportFrom(module='m', names=[alias(name='x', asname='y')])"},
		{"constants", &ast.Tuple{Elts: []ast.Expr{
			&ast.Constant{Value: ast.None{}}, &ast.Constant{Value: ast.Bool(true)}, &ast.Constant{Value: ast.Ellipsis{}},
			&ast.Constant{Value: ast.Int{N: new(big.Int).Exp(big.NewInt(10), big.NewInt(20), nil)}},
			&ast.Constant{Value: ast.Float(1.5)}, &ast.Constant{Value: ast.Complex(2i)}, &ast.Constant{Value: ast.Bytes("x")},
			&ast.Constant{Value: ast.Str("u"), Kind: str("u")}}}, nil,
			"Tuple(elts=[Constant(value=None), Constant(value=True), Constant(value=Ellipsis), Constant(value=100000000000000000000), Constant(value=1.5), Constant(value=2j), Constant(value=b'x'), Constant(value='u', kind='u')], ctx=Load())"},
		{"singleton None is shown", &ast.MatchSingleton{Value: ast.None{}}, nil, "MatchSingleton(value=None)"},
		{"helper nodes use their Python class names", &ast.FunctionDef{Name: "f", Args: &ast.Arguments{Args: []*ast.Arg{{Arg: "a"}}}, Body: []ast.Stmt{&ast.Pass{}}}, nil,
			"FunctionDef(name='f', args=arguments(args=[arg(arg='a')]), body=[Pass()])"},
		{"conversion minus one is shown", &ast.JoinedStr{Values: []ast.Expr{hi, &ast.FormattedValue{Value: name, Conversion: -1}}}, nil,
			"JoinedStr(values=[Constant(value='hi'), FormattedValue(value=Name(id='x', ctx=Load()), conversion=-1)])"},
		{"string lists", &ast.Global{Names: []string{"x", "y"}}, nil, "Global(names=['x', 'y'])"},
		{"attributes", located, []ast.DumpOption{ast.WithAttributes()},
			"Module(body=[Assign(targets=[Name(id='x', ctx=Store(), lineno=1, col_offset=0, end_lineno=1, end_col_offset=1)], value=Constant(value=1, lineno=1, col_offset=4, end_lineno=1, end_col_offset=5), lineno=1, col_offset=0, end_lineno=1, end_col_offset=5)])"},
		{"attributes with indent", located, []ast.DumpOption{ast.WithAttributes(), ast.WithIndent(" ")},
			"Module(\n body=[\n  Assign(\n   targets=[\n    Name(\n     id='x',\n     ctx=Store(),\n     lineno=1,\n     col_offset=0,\n     end_lineno=1,\n     end_col_offset=1)],\n   value=Constant(\n    value=1,\n    lineno=1,\n    col_offset=4,\n    end_lineno=1,\n    end_col_offset=5),\n   lineno=1,\n   col_offset=0,\n   end_lineno=1,\n   end_col_offset=5)])"},
		{"attributes without field names", located, []ast.DumpOption{ast.WithAttributes(), ast.WithoutFieldNames()},
			"Module([Assign([Name('x', Store(), lineno=1, col_offset=0, end_lineno=1, end_col_offset=1)], Constant(1, lineno=1, col_offset=4, end_lineno=1, end_col_offset=5), lineno=1, col_offset=0, end_lineno=1, end_col_offset=5)])"},
		{"attributes skip nodes without a position", &ast.Name{ID: "x"}, []ast.DumpOption{ast.WithAttributes()}, "Name(id='x', ctx=Load())"},
		{"lone end position omits end attributes", &ast.Name{Span: ast.Span{StartPos: file.Pos(0)}, ID: "x"}, []ast.DumpOption{ast.WithAttributes()},
			"Name(id='x', ctx=Load(), lineno=1, col_offset=0)"},
		{"empty int is zero", &ast.Constant{Value: ast.Int{}}, nil, "Constant(value=0)"},
		{"false", &ast.Constant{Value: ast.Bool(false)}, nil, "Constant(value=False)"},
		{"negative infinity", &ast.Constant{Value: ast.Float(math.Inf(-1))}, nil, "Constant(value=-inf)"},
	}
	for _, c := range loadReprCases(t) {
		tests = append(tests, struct {
			name string
			node ast.Node
			opts []ast.DumpOption
			want string
		}{c.Kind + " " + c.Repr, &ast.Constant{Value: c.constant(t)}, nil, "Constant(value=" + c.Repr + ")"})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ast.Dump(fset, tt.node, tt.opts...))
		})
	}
}

func loadReprCases(t *testing.T) []reprCase {
	t.Helper()
	f, err := os.Open("testdata/cpython/repr.jsonl")
	require.NoError(t, err)
	defer f.Close()
	var cases []reprCase
	scan := bufio.NewScanner(f)
	scan.Buffer(nil, 1<<20)
	for scan.Scan() {
		var c reprCase
		require.NoError(t, json.Unmarshal(scan.Bytes(), &c))
		cases = append(cases, c)
	}
	require.NoError(t, scan.Err())
	return cases
}

func TestRepr(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "abc", "'abc'"},
		{"single quote switches to double", "it's", `"it's"`},
		{"both quotes escape the single", `'"`, `'\'"'`},
		{"control characters", "\n\t\x00", `'\n\t\x00'`},
		{"non printable", "­", `'\xad'`},
		{"printable non ascii", "é", "'é'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ast.Repr(tt.in))
		})
	}
}
