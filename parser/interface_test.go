package parser_test

import (
	"errors"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/internal/cpython/corpus"
	"github.com/siyul-park/spry/parser"
	"github.com/siyul-park/spry/scanner"
	"github.com/siyul-park/spry/token"
)

func TestWithAllErrors(t *testing.T) {
	src := "a b\n)\n"

	t.Run("reports the scanner error and the parser error in position order", func(t *testing.T) {
		_, err := parser.ParseFile(token.NewFileSet(), "m.py", strings.NewReader(src), parser.WithAllErrors())
		var list scanner.ErrorList
		require.ErrorAs(t, err, &list)
		require.Len(t, list, 2)
		require.Equal(t, "invalid syntax", list[0].Msg)
		require.Equal(t, "unmatched ')'", list[1].Msg)
	})

	t.Run("is off by default", func(t *testing.T) {
		_, err := parser.ParseFile(token.NewFileSet(), "m.py", strings.NewReader(src))
		var list scanner.ErrorList
		require.ErrorAs(t, err, &list)
		require.Len(t, list, 1)
	})
}

func TestWithTypeComments(t *testing.T) {
	t.Run("is accepted and leaves type comments empty", func(t *testing.T) {
		src := "x = 1  # type: int\n# type: ignore\n"
		plain := token.NewFileSet()
		a, err := parser.ParseFile(plain, "m.py", strings.NewReader(src))
		require.NoError(t, err)
		typed := token.NewFileSet()
		b, err := parser.ParseFile(typed, "m.py", strings.NewReader(src), parser.WithTypeComments())
		require.NoError(t, err)
		require.Equal(t, ast.Dump(plain, a, ast.WithAttributes()), ast.Dump(typed, b, ast.WithAttributes()))
		require.Nil(t, b.TypeIgnores)
		require.Nil(t, b.Body[0].(*ast.Assign).TypeComment)
	})
}

func TestParseFile(t *testing.T) {
	t.Run("returns a module with CPython positions in UTF-8 bytes", func(t *testing.T) {
		fset := token.NewFileSet()
		mod, err := parser.ParseFile(fset, "m.py", strings.NewReader("x = 'é'\n"))
		require.NoError(t, err)
		require.Equal(t, "Module(body=[Assign(targets=[Name(id='x', ctx=Store(), lineno=1, col_offset=0, end_lineno=1, end_col_offset=1)], "+
			"value=Constant(value='é', lineno=1, col_offset=4, end_lineno=1, end_col_offset=8), lineno=1, col_offset=0, end_lineno=1, end_col_offset=8)])",
			ast.Dump(fset, mod, ast.WithAttributes()))
	})

	t.Run("normalizes non-ASCII names to NFKC", func(t *testing.T) {
		mod, err := parser.ParseFile(token.NewFileSet(), "m.py", strings.NewReader("ｘ = 1\n"))
		require.NoError(t, err)
		require.Equal(t, "x", mod.Body[0].(*ast.Assign).Targets[0].(*ast.Name).ID)
	})

	t.Run("decodes the coding cookie", func(t *testing.T) {
		mod, err := parser.ParseFile(token.NewFileSet(), "m.py", strings.NewReader("# coding: latin-1\nx = '\xe9'\n"))
		require.NoError(t, err)
		require.Equal(t, ast.Str("é"), mod.Body[0].(*ast.Assign).Value.(*ast.Constant).Value)
	})

	t.Run("decodes named escapes", func(t *testing.T) {
		src := `x = "\N{HANGUL SYLLABLE GAG}\N{CJK UNIFIED IDEOGRAPH-4E00}\N{LF}\N{digit nine}\N{SPACE}"` + "\n"
		mod, err := parser.ParseFile(token.NewFileSet(), "m.py", strings.NewReader(src))
		require.NoError(t, err)
		require.Equal(t, ast.Str("각一\n9 "), mod.Body[0].(*ast.Assign).Value.(*ast.Constant).Value)
	})

	t.Run("rejects unknown named escapes", func(t *testing.T) {
		_, err := parser.ParseFile(token.NewFileSet(), "m.py", strings.NewReader(`x = "\N{NO SUCH NAME}"`+"\n"))
		require.ErrorContains(t, err, "unknown Unicode character name")
	})

	t.Run("reports invalid syntax at the furthest token", func(t *testing.T) {
		_, err := parser.ParseFile(token.NewFileSet(), "m.py", strings.NewReader("a b\n"))
		var list scanner.ErrorList
		require.ErrorAs(t, err, &list)
		require.Len(t, list, 1)
		require.ErrorIs(t, err, scanner.ErrSyntax)
		require.Equal(t, "invalid syntax", list[0].Msg)
		require.Equal(t, 1, list[0].Pos.Line)
		require.Equal(t, 3, list[0].Pos.Column)
		require.Equal(t, "m.py:1:3: invalid syntax", err.Error())
	})

	t.Run("reports scanner errors before parser errors", func(t *testing.T) {
		_, err := parser.ParseFile(token.NewFileSet(), "m.py", strings.NewReader("a b\n)\n"))
		var list scanner.ErrorList
		require.ErrorAs(t, err, &list)
		require.Len(t, list, 1)
		require.Equal(t, "unmatched ')'", list[0].Msg)
	})

	t.Run("keeps the indentation category", func(t *testing.T) {
		_, err := parser.ParseFile(token.NewFileSet(), "m.py", strings.NewReader("if 1:\n    a\n  b\n"))
		require.ErrorIs(t, err, scanner.ErrIndentation)
	})

	t.Run("reports errors raised while building literals", func(t *testing.T) {
		_, err := parser.ParseFile(token.NewFileSet(), "m.py", strings.NewReader("'a' b'b'\n"))
		var list scanner.ErrorList
		require.ErrorAs(t, err, &list)
		require.Equal(t, "cannot mix bytes and nonbytes literals", list[0].Msg)
	})

	t.Run("bounds the nesting of expressions", func(t *testing.T) {
		src := strings.Repeat("-", 100000) + "1\n"
		_, err := parser.ParseFile(token.NewFileSet(), "m.py", strings.NewReader(src))
		require.ErrorContains(t, err, "Parser stack overflowed")
	})

	t.Run("rejects null bytes", func(t *testing.T) {
		_, err := parser.ParseFile(token.NewFileSet(), "m.py", strings.NewReader("x = 1\x00\n"))
		require.ErrorIs(t, err, scanner.ErrSyntax)
		require.ErrorContains(t, err, "source code string cannot contain null bytes")
	})

	t.Run("wraps read errors", func(t *testing.T) {
		boom := errors.New("boom")
		_, err := parser.ParseFile(token.NewFileSet(), "m.py", iotest.ErrReader(boom))
		require.ErrorIs(t, err, boom)
	})

	t.Run("translates line endings like CPython", func(t *testing.T) {
		mod, err := parser.ParseFile(token.NewFileSet(), "m.py", strings.NewReader("a\rb\r\nc\n"))
		require.NoError(t, err)
		require.Len(t, mod.Body, 3)
	})

	// Position contract of scanner.Error for errors the parser raises: Pos is
	// the start, and CPython's end_offset is End.Column-1.
	t.Run("reports errors at the positions CPython does", func(t *testing.T) {
		tests := []struct {
			name      string
			src       string
			msg       string
			kind      error
			line, col int // Pos
			endLine   int
			endCol    int // End.Column
		}{
			{"node range", "f(a b)\n", "invalid syntax. Perhaps you forgot a comma?", scanner.ErrSyntax, 1, 3, 1, 7},
			{"string token", "x = \"\\N{foo}\"\n", "(unicode error) 'unicodeescape' codec can't decode bytes in position 0-6: unknown Unicode character name", scanner.ErrSyntax, 1, 5, 1, 15},
			{"missing block", "def f():\npass\n", "expected an indented block after function definition on line 1", scanner.ErrIndentation, 2, 1, 2, 6},
			{"unexpected indent has no end", "x = 1\n    y = 2\n", "unexpected indent", scanner.ErrIndentation, 2, 4, 2, 0},
			{"scanner error", "x = (1, 2\n", "'(' was never closed", scanner.ErrSyntax, 1, 5, 1, 6},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := parser.ParseFile(token.NewFileSet(), "m.py", strings.NewReader(tt.src))
				var list scanner.ErrorList
				require.ErrorAs(t, err, &list)
				e := list[0]
				require.Equal(t, tt.msg, e.Msg)
				require.ErrorIs(t, e, tt.kind)
				require.Equal(t, [2]int{tt.line, tt.col}, [2]int{e.Pos.Line, e.Pos.Column})
				require.Equal(t, [2]int{tt.endLine, tt.endCol}, [2]int{e.End.Line, e.End.Column})
			})
		}
	})
}

func TestParseExpr(t *testing.T) {
	t.Run("parses one expression", func(t *testing.T) {
		fset := token.NewFileSet()
		expr, err := parser.ParseExpr(fset, "e.py", strings.NewReader("a if b else c"))
		require.NoError(t, err)
		require.Equal(t, "Expression(body=IfExp(test=Name(id='b', ctx=Load(), lineno=1, col_offset=5, end_lineno=1, end_col_offset=6), "+
			"body=Name(id='a', ctx=Load(), lineno=1, col_offset=0, end_lineno=1, end_col_offset=1), "+
			"orelse=Name(id='c', ctx=Load(), lineno=1, col_offset=12, end_lineno=1, end_col_offset=13), lineno=1, col_offset=0, end_lineno=1, end_col_offset=13))",
			ast.Dump(fset, expr, ast.WithAttributes()))
	})

	t.Run("accepts an unparenthesized tuple and trailing newlines", func(t *testing.T) {
		expr, err := parser.ParseExpr(token.NewFileSet(), "e.py", strings.NewReader("1, 2,\n\n"))
		require.NoError(t, err)
		require.Len(t, expr.Body.(*ast.Tuple).Elts, 2)
	})

	t.Run("rejects statements and indentation", func(t *testing.T) {
		for _, src := range []string{"x = 1", " 1", "a b"} {
			_, err := parser.ParseExpr(token.NewFileSet(), "e.py", strings.NewReader(src))
			require.ErrorIs(t, err, scanner.ErrSyntax, src)
		}
	})
}

func TestParseInteractive(t *testing.T) {
	t.Run("parses one statement", func(t *testing.T) {
		fset := token.NewFileSet()
		mod, err := parser.ParseInteractive(fset, "i.py", strings.NewReader("x = 1\n"))
		require.NoError(t, err)
		require.Equal(t, "Interactive(body=[Assign(targets=[Name(id='x', ctx=Store(), lineno=1, col_offset=0, end_lineno=1, end_col_offset=1)], "+
			"value=Constant(value=1, lineno=1, col_offset=4, end_lineno=1, end_col_offset=5), lineno=1, col_offset=0, end_lineno=1, end_col_offset=5)])",
			ast.Dump(fset, mod, ast.WithAttributes()))
	})

	t.Run("accepts a compound statement closed by the end of input", func(t *testing.T) {
		for _, src := range []string{"if 1:\n pass\n", "def f():\n yield 1"} {
			_, err := parser.ParseInteractive(token.NewFileSet(), "i.py", strings.NewReader(src))
			require.NoError(t, err, src)
		}
	})

	t.Run("rejects a one-line compound statement without a line break", func(t *testing.T) {
		_, err := parser.ParseInteractive(token.NewFileSet(), "i.py", strings.NewReader("def f(): pass"))
		require.ErrorIs(t, err, scanner.ErrSyntax)
	})

	t.Run("rejects several statements", func(t *testing.T) {
		_, err := parser.ParseInteractive(token.NewFileSet(), "i.py", strings.NewReader("a\nb\n"))
		require.ErrorContains(t, err, "multiple statements found while compiling a single statement")
	})

	t.Run("rejects empty input", func(t *testing.T) {
		_, err := parser.ParseInteractive(token.NewFileSet(), "i.py", strings.NewReader("\n"))
		require.ErrorIs(t, err, scanner.ErrSyntax)
	})

	t.Run("reports errors of incomplete and multiple statements", func(t *testing.T) {
		parse := func(src string) error {
			_, err := parser.ParseInteractive(token.NewFileSet(), "m.py", strings.NewReader(src))
			return err
		}

		t.Run("a source without a final line break is complete", func(t *testing.T) {
			require.NoError(t, parse("x = 1"))
			require.NoError(t, parse("def f():\n  pass"))
			require.NoError(t, parse("1 + 2\t\t\n        "))
		})

		t.Run("a last line of only white space is judged by CPython's rules", func(t *testing.T) {
			require.ErrorIs(t, parse("def x():\n  pass\n "), scanner.ErrIndentation)
			require.ErrorIs(t, parse("def x():\n  pass\n  "), scanner.ErrSyntax)
		})

		t.Run("a second statement is an error", func(t *testing.T) {
			var list scanner.ErrorList
			require.ErrorAs(t, parse("a = 1\nb = 2"), &list)
			require.Equal(t, "multiple statements found while compiling a single statement", list[0].Msg)
		})
	})
}

func TestParseFuncType(t *testing.T) {
	t.Run("parses argument and return types", func(t *testing.T) {
		fset := token.NewFileSet()
		ft, err := parser.ParseFuncType(fset, "t.py", strings.NewReader("(int, str) -> bool"))
		require.NoError(t, err)
		require.Equal(t, "FunctionType(argtypes=[Name(id='int', ctx=Load(), lineno=1, col_offset=1, end_lineno=1, end_col_offset=4), "+
			"Name(id='str', ctx=Load(), lineno=1, col_offset=6, end_lineno=1, end_col_offset=9)], "+
			"returns=Name(id='bool', ctx=Load(), lineno=1, col_offset=14, end_lineno=1, end_col_offset=18))",
			ast.Dump(fset, ft, ast.WithAttributes()))
	})

	t.Run("accepts starred argument types", func(t *testing.T) {
		for _, src := range []string{"() -> None", "(*a) -> None", "(**k) -> None", "(a, *b, **k) -> None", "(*a, **k) -> None"} {
			_, err := parser.ParseFuncType(token.NewFileSet(), "t.py", strings.NewReader(src))
			require.NoError(t, err, src)
		}
	})

	t.Run("rejects malformed signatures", func(t *testing.T) {
		for _, src := range []string{"(int,) -> x", "(int)", "int -> x", "(**k, a) -> x"} {
			_, err := parser.ParseFuncType(token.NewFileSet(), "t.py", strings.NewReader(src))
			require.ErrorIs(t, err, scanner.ErrSyntax, src)
		}
	})
}

func FuzzParseFile(f *testing.F) {
	for _, manifest := range []string{"snippets", "syntax"} {
		cases, err := corpus.Load("testdata/cpython/" + manifest + ".jsonl")
		require.NoError(f, err)
		for _, c := range cases {
			if c.File == "" && c.Mode == "exec" {
				f.Add([]byte(c.Src))
			}
		}
	}
	for _, seed := range []string{"", "x = (", "f'{a!r:>{w}}'", "t'''{x", "match x:\n case [1, *r]: pass\n", "\\\n", "\x00", "def f[T](a, /, *, b=1): ...\n"} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, src []byte) {
		fset := token.NewFileSet()
		mod, err := parser.ParseFile(fset, "f.py", strings.NewReader(string(src)))
		if err != nil {
			var list scanner.ErrorList
			require.ErrorAs(t, err, &list)
			require.NotEmpty(t, list)
			return
		}
		first := ast.Dump(fset, mod, ast.WithIndent(" "), ast.WithAttributes())
		require.Equal(t, first, ast.Dump(fset, mod, ast.WithIndent(" "), ast.WithAttributes()), "Dump is not deterministic for %q", src)

		file := fset.File(mod.Pos())
		ast.Inspect(mod, func(n ast.Node) bool {
			if n == nil {
				return false
			}
			if n.Pos().IsValid() && n.End().IsValid() {
				require.LessOrEqual(t, n.Pos(), n.End(), "%T ends before it starts in %q", n, src)
				require.NotNil(t, file, "no file for %T in %q", n, src)
				require.LessOrEqual(t, int(n.End()), file.Base()+file.Size(), "%T ends past the file in %q", n, src)
			}
			return true
		})
	})
}
