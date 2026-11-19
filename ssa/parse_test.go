package ssa_test

import (
	"bytes"
	"math"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/ssa"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	t.Run("round trips every testdata program", func(t *testing.T) {
		for _, file := range irFiles(t) {
			src, err := os.ReadFile(file)
			require.NoError(t, err)
			p := mustParse(t, string(src))
			var out bytes.Buffer
			require.NoError(t, ssa.Print(&out, p, ssa.WithPositions()), file)

			q, err := ssa.Parse(&out)

			require.NoError(t, err, file)
			if !strings.Contains(string(src), "nan") {
				require.Equal(t, p, q, file)
			}
		}
	})

	t.Run("links references to declarations that come later", func(t *testing.T) {
		p := mustParse(t, "module a\n\nfunc @f() -> none {\nb0:\n  %0 = call @b:g() : none\n  ret\n}\n\nmodule b\n\nfunc @g() -> none {\nb0:\n  ret\n}\n")

		call := p.Modules[0].Functions[0].Blocks[0].Instrs[0]

		require.Same(t, p.Modules[1].Functions[0], call.Callee)
		require.Same(t, p.Modules[1], call.Callee.Module)
	})

	t.Run("numbers blocks and results in order and links edges", func(t *testing.T) {
		p := mustParse(t, "module m\n\nfunc @f(%a: i1) -> int {\nb5:\n  br %a, b9(1), b7\nb9(%x: int):\n  ret %x\nb7:\n  %40 = add 1, 2 : int\n  jump b9(%40)\n}\n")

		f := p.Modules[0].Functions[0]

		require.Equal(t, []int{0, 1, 2}, []int{f.Blocks[0].Index, f.Blocks[1].Index, f.Blocks[2].Index})
		require.Equal(t, 0, f.Blocks[2].Instrs[0].ID)
		require.Equal(t, []*ssa.Block{f.Blocks[1], f.Blocks[2]}, f.Blocks[0].Succs)
		require.Equal(t, []*ssa.Block{f.Blocks[0], f.Blocks[2]}, f.Blocks[1].Preds)
	})

	t.Run("reads constants exactly", func(t *testing.T) {
		p := mustParse(t, "module m\n\nfunc @f() -> none {\nb0:\n  %0 = add 340282366920938463463374607431768211456, -0.0 : int\n  %1 = add inf, -inf : f64\n  %2 = add 'a\\x00\\ud800', b'\\xff' : str\n  ret\n}\n")

		args := p.Modules[0].Functions[0].Blocks[0].Instrs
		big1, _ := new(big.Int).SetString("340282366920938463463374607431768211456", 10)

		require.Equal(t, ast.Int{N: big1}, args[0].Args[0].(*ssa.Const).Value)
		require.True(t, math.Signbit(float64(args[0].Args[1].(*ssa.Const).Value.(ast.Float))))
		require.True(t, math.IsInf(float64(args[1].Args[1].(*ssa.Const).Value.(ast.Float)), -1))
		require.Equal(t, ast.Str("a\x00\xed\xa0\x80"), args[2].Args[0].(*ssa.Const).Value)
		require.Equal(t, ast.Bytes{0xff}, args[2].Args[1].(*ssa.Const).Value)
	})

	t.Run("rejects malformed text with line and column", func(t *testing.T) {
		cases := []struct{ name, src, want string }{
			{"unknown top-level word", "bogus\n", "1:6: unexpected"},
			{"function outside a module", "func @f() -> none {\n", "1:5: func outside a module"},
			{"unknown instruction", "module m\nfunc @f() -> none {\nb0:\n  frob 1\n}\n", "4:7: unknown instruction"},
			{"missing result name", "module m\nfunc @f() -> int {\nb0:\n  add 1, 2 : int\n}\n", "needs a result name"},
			{"result on a no-result op", "module m\nfunc @f() -> int {\nb0:\n  %0 = ret\n}\n", "has no result"},
			{"undefined value", "module m\nfunc @f() -> none {\nb0:\n  %0 = add %q, 1 : int\n  ret\n}\n", "4:12: undefined value %q"},
			{"duplicate value", "module m\nfunc @f() -> none {\nb0:\n  %0 = add 1, 1 : int\n  %0 = add 1, 1 : int\n  ret\n}\n", "defined twice"},
			{"numeric parameter", "module m\nfunc @f(%0: int) -> none {\nb0:\n  ret\n}\n", "must not be numeric"},
			{"non-numeric result", "module m\nfunc @f() -> none {\nb0:\n  %x = add 1, 1 : int\n  ret\n}\n", "must be numeric"},
			{"undefined block", "module m\nfunc @f() -> none {\nb0:\n  jump b3\n}\n", "block b3 is used but not defined"},
			{"duplicate block", "module m\nfunc @f() -> none {\nb0:\n  ret\nb0:\n  ret\n}\n", "defined twice"},
			{"instruction before a block", "module m\nfunc @f() -> none {\n  ret\n}\n", "before the first block"},
			{"function without blocks", "module m\nfunc @f() -> none {\n}\n", "has no blocks"},
			{"unclosed function", "module m\nfunc @f() -> none {\nb0:\n  ret\n", "is not closed"},
			{"wrong operand count", "module m\nfunc @f() -> none {\nb0:\n  %0 = add 1 : int\n  ret\n}\n", "takes 2 operands"},
			{"undeclared callee", "module m\nfunc @f() -> none {\nb0:\n  %0 = call @g() : none\n  ret\n}\n", "function @g is used but not declared"},
			{"undeclared module", "module m\nfunc @f() -> none {\nb0:\n  %0 = call @x:g() : none\n  ret\n}\n", "module x is used but not declared"},
			{"duplicate function", "module m\nextern func @f() -> none\nextern func @f() -> none\n", "declared twice"},
			{"duplicate module", "module m\nmodule m\n", "declared twice"},
			{"unknown type", "module m\nglobal @g : string\n", "unknown type"},
			{"nullable scalar", "module m\nglobal @g : int?\n", "cannot be nullable"},
			{"unterminated string", "module m\nfunc @f() -> none {\nb0:\n  %0 = add 'x, 1 : int\n  ret\n}\n", "unterminated string"},
			{"bad escape", "module m\nfunc @f() -> none {\nb0:\n  %0 = add 'x\\q', 1 : int\n  ret\n}\n", "invalid escape"},
			{"float out of range", "module m\nfunc @f() -> none {\nb0:\n  %0 = add 1e999, 1.0 : f64\n  ret\n}\n", "bad float"},
			{"undef without a type", "module m\nfunc @f() -> none {\nb0:\n  %0 = add undef, 1 : int\n  ret\n}\n", "undef needs a type"},
			{"unqualified method target", "class @C {\n  method m -> @f\n}\n", "needs a module"},
			{"class not closed", "class @C {\n  field x : int\n", "is not closed"},
			{"exception edge on a plain op", "module m\nfunc @f() -> none {\nb0:\n  %0 = add 1, 2 : int -> normal b0, except b0\n}\n", "cannot carry an exception edge"},
			{"trailing text", "module m extra\n", "unexpected"},
		}
		for _, c := range cases {
			_, err := ssa.Parse(strings.NewReader(c.src))

			require.ErrorIs(t, err, ssa.ErrParse, c.name)
			require.ErrorContains(t, err, c.want, c.name)
		}
	})
}

func FuzzParse(f *testing.F) {
	for _, file := range []string{"arith", "classes", "exceptions", "generators", "literals", "modules", "quoting", "types"} {
		src, err := os.ReadFile("testdata/ir/" + file + ".ssa")
		require.NoError(f, err)
		f.Add(src)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := ssa.Parse(bytes.NewReader(data))
		if err != nil {
			return
		}
		var first bytes.Buffer
		if err := ssa.Print(&first, p); err != nil {
			return
		}
		q, err := ssa.Parse(bytes.NewReader(first.Bytes()))
		require.NoError(t, err)
		var second bytes.Buffer
		require.NoError(t, ssa.Print(&second, q))
		require.Equal(t, first.String(), second.String())
	})
}
