package ssa

import (
	"errors"
	"strconv"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/token"
)

// Program is a whole Python program: every module it imports, every class,
// and the function that starts it. Entry is nil for a library.
type Program struct {
	Modules []*Module
	Classes []*Class
	Entry   *Function
}

// Module is one Python module. Init, when set, is one of Functions and
// runs the module body. The main module is named __main__.
type Module struct {
	Name      string
	Globals   []*Global
	Functions []*Function
	Init      *Function
}

// Global is a module-level variable.
type Global struct {
	Name   string
	Type   Type
	Module *Module
}

// Class is a class with a fixed field layout. Fields lists inherited fields
// first, each with the class that declares it. Extern marks a class provided
// by the runtime.
type Class struct {
	Name    string
	Bases   []*Class
	Fields  []Field
	Methods []Method
	Extern  bool
}

// Field is one slot of a class layout.
type Field struct {
	Name  string
	Type  Type
	Owner *Class
}

// Method binds a method name to its implementation.
type Method struct {
	Name string
	Func *Function
}

// Function is a function body in SSA form. Source is the Python qualified
// name. A function without Blocks is extern. For a generator, Result is the
// type of the returned value and Yield and Send are the types it yields and
// receives.
type Function struct {
	Name      string
	Source    string
	Module    *Module
	Params    []*Param
	Captures  []*Param
	Result    Type
	Generator bool
	Yield     Type
	Send      Type
	Blocks    []*Block
}

// Block is a basic block. Params receive the arguments of its incoming
// edges. Its last instruction ends it: a terminator or an instruction with an
// exception edge. Preds and Succs hold one entry per edge.
type Block struct {
	Index  int
	Params []*Param
	Instrs []*Instr
	Preds  []*Block
	Succs  []*Block
}

// Param is a function parameter, a captured cell or a block parameter. Its
// Name is printed as %Name and is not all digits.
type Param struct {
	Name string
	Typ  Type
}

// Const is an inline constant operand. Value is one of ast.Int, ast.Float,
// ast.Bool, ast.None, ast.Str or ast.Bytes, and Typ is the type it has in the
// program. Undef marks an undefined value of type Typ and has no Value.
type Const struct {
	Value ast.Value
	Undef bool
	Typ   Type
}

// Value is an operand: a *Param, an *Instr that defines a result, or a *Const.
type Value interface {
	isValue()
}

// Instr is one instruction. Op decides which of Callee, Global, Class and
// Attr is set. Succs holds the edges of jump and br, and the normal edge of an
// instruction that carries Except. Typ is the type of the result and is the
// zero Type when the Op defines none. ID numbers the result in the function.
type Instr struct {
	Op     Op
	ID     int
	Typ    Type
	Args   []Value
	Succs  []Edge
	Except *Edge
	Callee *Function
	Global *Global
	Class  *Class
	Attr   string
	Pos    token.Position
	End    token.Position
}

// Edge is a control-flow edge. Args are passed to the Block parameters.
type Edge struct {
	Block *Block
	Args  []Value
}

// Error categories. Verify wraps ErrInvalid and Parse wraps ErrParse.
var (
	ErrInvalid = errors.New("ssa: invalid program")
	ErrParse   = errors.New("ssa: syntax error")
)

// TypeOf returns the type of v.
func TypeOf(v Value) Type {
	switch v := v.(type) {
	case *Param:
		return v.Typ
	case *Instr:
		return v.Typ
	case *Const:
		return v.Typ
	}
	return Type{}
}

// finish numbers anonymous parameters, blocks and results in order and
// recomputes Preds and Succs.
func (f *Function) finish() {
	n := 0
	name := func(p *Param) {
		if p.Name == "" {
			p.Name = "p" + strconv.Itoa(n)
			n++
		}
	}
	for _, p := range f.Params {
		name(p)
	}
	for _, p := range f.Captures {
		name(p)
	}
	id := 0
	for i, b := range f.Blocks {
		b.Index = i
		for _, p := range b.Params {
			name(p)
		}
		for _, in := range b.Instrs {
			if in.Op.info().flags&fResult != 0 {
				in.ID = id
				id++
			}
		}
	}
	preds, succs := f.wiring()
	for i, b := range f.Blocks {
		b.Preds, b.Succs = preds[i], succs[i]
	}
}

// wiring returns the predecessors and successors that the edges of f imply,
// indexed by Block.Index and ignoring edges to blocks outside f.
func (f *Function) wiring() (preds, succs [][]*Block) {
	preds = make([][]*Block, len(f.Blocks))
	succs = make([][]*Block, len(f.Blocks))
	for i, b := range f.Blocks {
		for _, e := range b.edges() {
			t := e.Block
			if t == nil || t.Index < 0 || t.Index >= len(f.Blocks) || f.Blocks[t.Index] != t {
				continue
			}
			succs[i] = append(succs[i], t)
			preds[t.Index] = append(preds[t.Index], b)
		}
	}
	return preds, succs
}

// callResult is the type of a call of f: the generator that a generator
// function makes, and the result of any other function.
func (f *Function) callResult() Type {
	if f.Generator {
		return Type{Kind: KindGen, Elems: []Type{f.Yield, f.Send, f.Result}}
	}
	return f.Result
}

// edges returns the edges that leave b: those of its last instruction.
func (b *Block) edges() []*Edge {
	if len(b.Instrs) == 0 {
		return nil
	}
	last := b.Instrs[len(b.Instrs)-1]
	if last == nil {
		return nil
	}
	var out []*Edge
	for i := range last.Succs {
		out = append(out, &last.Succs[i])
	}
	if last.Except != nil {
		out = append(out, last.Except)
	}
	return out
}

func (*Const) isValue() {}

func (*Instr) isValue() {}

func (*Param) isValue() {}

func (c *Class) derives(anc *Class) bool {
	seen := map[*Class]bool{}
	stack := []*Class{c}
	for len(stack) > 0 {
		x := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if x == anc {
			return true
		}
		if x == nil || seen[x] {
			continue
		}
		seen[x] = true
		stack = append(stack, x.Bases...)
	}
	return false
}

func (c *Class) field(name string) (Field, bool) {
	for _, f := range c.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

func (c *Class) method(name string) *Function {
	seen := map[*Class]bool{}
	stack := []*Class{c}
	for len(stack) > 0 {
		x := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if x == nil || seen[x] {
			continue
		}
		seen[x] = true
		for _, m := range x.Methods {
			if m.Name == name {
				return m.Func
			}
		}
		for i := len(x.Bases) - 1; i >= 0; i-- {
			stack = append(stack, x.Bases[i])
		}
	}
	return nil
}
