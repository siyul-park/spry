package ssa_test

import (
	"os"
	"strings"
	"testing"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/ssa"
	"github.com/stretchr/testify/require"
)

const valid = `class @C {
  field x : int
}

module m

func @f(%a: int, %o: @C, %n: @C?) -> int {
b0:
  %0 = call @g(%a) : int -> normal b1(%0), except b2
b1(%r: int):
  %1 = loadfield %o.x : int
  %2 = add %r, %1 : int
  ret %2
b2:
  %3 = landingpad : any
  raise %3
}

func @g(%x: int) -> int {
b0:
  ret %x
}

func @gen() -> none yield int send none {
b0:
  %0 = yield 1 : none
  ret
}
`

func fn(p *ssa.Program, name string) *ssa.Function {
	for _, f := range p.Modules[0].Functions {
		if f.Name == name {
			return f
		}
	}
	panic(name)
}

// entry returns the first block of the named function.
func entry(p *ssa.Program, name string) *ssa.Block {
	return fn(p, name).Blocks[0]
}

func TestVerify(t *testing.T) {
	t.Run("accepts every testdata program", func(t *testing.T) {
		for _, file := range irFiles(t) {
			src, err := os.ReadFile(file)
			require.NoError(t, err)

			require.NoError(t, ssa.Verify(mustParse(t, string(src))), file)
		}
	})

	t.Run("accepts the valid program", func(t *testing.T) {
		require.NoError(t, ssa.Verify(mustParse(t, valid)))
	})

	t.Run("reports each violation of the text form with its code", func(t *testing.T) {
		cases := []struct {
			name, old, new, code string
		}{
			{"block without terminator", "  ret %2\n", "  %9 = add %2, 1 : int\n", "SSA002"},
			{"terminator before the end", "  ret %2\n", "  ret %2\n  ret %2\n", "SSA002"},
			{"unreachable block", "b2:\n", "b9:\n  ret 0\nb2:\n", "SSA003"},
			{"use does not dominate", "raise %3", "raise %1", "SSA005"},
			{"edge argument type", "b1(%0),", "b1(1.5),", "SSA006"},
			{"operand type", "add %r, %1 : int", "add %r, %1 : f64", "SSA007"},
			{"call argument type", "call @g(%a)", "call @g(1.5)", "SSA008"},
			{"exception edge to a plain block", "except b2", "except b1(%0)", "SSA009"},
			{"normal successor with two predecessors", "raise %3", "jump b1(1)", "SSA010"},
			{"result on the exception edge", "b2:\n  %3", "b2(%q: int):\n  %3", "SSA010"},
			{"yield outside a generator", "  ret %x", "  %5 = yield 1 : none\n  ret %x", "SSA011"},
			{"constant of the wrong type", "add %r, %1", "add %r, 'x':int", "SSA015"},
			{"loadfield on a nullable class", "loadfield %o.x", "loadfield %n.x", "SSA016"},
		}
		for _, c := range cases {
			src := strings.Replace(valid, c.old, c.new, 1)
			if c.name == "result on the exception edge" {
				src = strings.Replace(src, "except b2", "except b2(%0)", 1)
			}
			require.NotEqual(t, valid, src, c.name)

			err := ssa.Verify(mustParse(t, src))

			require.ErrorIs(t, err, ssa.ErrInvalid, c.name)
			require.ErrorContains(t, err, c.code, c.name)
		}
	})

	t.Run("reports each violation built in memory with its code", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(p *ssa.Program)
			code   string
		}{
			{"wrong operand count", func(p *ssa.Program) { fn(p, "f").Blocks[1].Instrs[1].Args = nil }, "SSA001"},
			{"operand from another function", func(p *ssa.Program) {
				entry(p, "g").Instrs[0].Args = []ssa.Value{&ssa.Param{Name: "z", Typ: ssa.Type{Kind: ssa.KindInt}}}
			}, "SSA004"},
			{"duplicate parameter name", func(p *ssa.Program) { fn(p, "f").Params[1].Name = "a" }, "SSA004"},
			{"stale Preds", func(p *ssa.Program) { fn(p, "f").Blocks[1].Preds = nil }, "SSA012"},
			{"wrong block index", func(p *ssa.Program) { fn(p, "f").Blocks[1].Index = 5 }, "SSA012"},
			{"nullable int", func(p *ssa.Program) { fn(p, "f").Params[0].Typ.Nullable = true }, "SSA013"},
			{"callee outside the program", func(p *ssa.Program) { entry(p, "f").Instrs[0].Callee = &ssa.Function{Name: "z"} }, "SSA014"},
			{"entry outside the program", func(p *ssa.Program) { p.Entry = &ssa.Function{Name: "z"} }, "SSA014"},
			{"repeated class", func(p *ssa.Program) { p.Classes = append(p.Classes, p.Classes[0]) }, "SSA014"},
			{"undef with a value", func(p *ssa.Program) {
				fn(p, "g").Blocks[0].Instrs[0].Args = []ssa.Value{&ssa.Const{Undef: true, Value: ast.None{}, Typ: ssa.Type{Kind: ssa.KindInt}}}
			}, "SSA015"},
			{"generator types on a plain function", func(p *ssa.Program) { fn(p, "g").Yield = ssa.Type{Kind: ssa.KindInt} }, "SSA011"},
		}
		for _, c := range cases {
			p := mustParse(t, valid)
			c.mutate(p)

			err := ssa.Verify(p)

			require.ErrorIs(t, err, ssa.ErrInvalid, c.name)
			require.ErrorContains(t, err, c.code, c.name)
		}
	})
}
