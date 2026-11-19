package ssa

import (
	"fmt"
	"strings"

	"github.com/siyul-park/spry/ast"
)

type verifier struct {
	diags   []string
	where   string
	funcs   map[*Function]bool
	classes map[*Class]bool
	globals map[*Global]bool
	fn      *Function
	fs      *funcState
}

type funcState struct {
	blocks map[*Block]int
	defs   map[Value]def
	npreds []int
	idom   []int
	reach  []bool
}

type def struct {
	block int // -1 for function parameters
	pos   int
}

type edgeRole uint8

// Diagnostic codes of Verify: the error text lists them as SSAnnn.
const (
	codeShape   = 1 + iota // malformed instruction
	codeBlock              // block structure
	codeReach              // entry block and reachability
	codeDef                // value definitions and names
	codeDom                // dominance
	codeEdge               // edge targets and arguments
	codeType               // operand and result types
	codeCall               // call signatures
	codeLanding            // landingpad and exception-edge targets
	codeInvoke             // instructions with an exception edge
	codeGen                // generators
	codeWiring             // block numbers, Preds and Succs
	codeBadType            // malformed types
	codeMember             // program membership and declarations
	codeConst              // constants
	codeField              // field access
)

const (
	roleBranch edgeRole = iota // edge of jump or br
	roleNormal                 // normal edge of an instruction with an exception edge
	roleExcept                 // exception edge
)

// Verify checks the invariants of docs/ssa.md and returns an error wrapping
// ErrInvalid that lists every violation as a line "SSAnnn where: message".
func Verify(p *Program) error {
	v := &verifier{
		funcs:   map[*Function]bool{},
		classes: map[*Class]bool{},
		globals: map[*Global]bool{},
	}
	v.program(p)
	if len(v.diags) == 0 {
		return nil
	}
	return fmt.Errorf("%w:\n%s", ErrInvalid, strings.Join(v.diags, "\n"))
}

func (v *verifier) program(p *Program) {
	v.collect(p)
	for _, c := range p.Classes {
		v.class(c)
	}
	for _, m := range p.Modules {
		v.module(m)
	}
	v.where = "program"
	if p.Entry != nil && !v.funcs[p.Entry] {
		v.report(codeMember, "entry function is not a member of the program")
	}
}

func (v *verifier) module(m *Module) {
	v.where = "module " + m.Name
	for _, g := range m.Globals {
		if g.Module != m {
			v.report(codeMember, "global @%s names another module", g.Name)
		}
		v.typ(g.Type)
	}
	if m.Init != nil && (!v.funcs[m.Init] || m.Init.Module != m) {
		v.report(codeMember, "init is not a function of the module")
	}
	for _, f := range m.Functions {
		if f.Module != m {
			v.report(codeMember, "function @%s names another module", f.Name)
		}
		v.function(f)
	}
}

func (v *verifier) function(f *Function) {
	v.where = "function @" + f.Name
	v.fn = f
	names := map[string]bool{}
	for _, p := range f.Params {
		v.param(p, names)
	}
	for _, p := range f.Captures {
		v.param(p, names)
	}
	v.typ(f.Result)
	switch {
	case f.Generator:
		v.typ(f.Yield)
		v.typ(f.Send)
	case f.Yield.Kind != KindInvalid || f.Send.Kind != KindInvalid:
		v.report(codeGen, "only a generator has yield and send types")
	}
	if len(f.Blocks) > 0 {
		v.body(f, names)
	}
}

func (v *verifier) body(f *Function, names map[string]bool) {
	v.fs = &funcState{blocks: map[*Block]int{}, defs: map[Value]def{}}
	for i, b := range f.Blocks {
		if b == nil || b.Index != i {
			v.report(codeWiring, "block %d is nil or has the wrong Index", i)
			return
		}
		if _, dup := v.fs.blocks[b]; dup {
			v.report(codeBlock, "block b%d is listed twice", i)
			return
		}
		v.fs.blocks[b] = i
	}
	for _, p := range f.Params {
		v.fs.defs[p] = def{block: -1}
	}
	for _, p := range f.Captures {
		v.fs.defs[p] = def{block: -1}
	}
	if !v.defineAll(f, names) {
		return
	}
	v.wire(f)
	for i, b := range f.Blocks {
		v.block(i, b)
	}
}

func (v *verifier) block(i int, b *Block) {
	for j, in := range b.Instrs {
		v.where = fmt.Sprintf("function @%s b%d %s", v.fn.Name, i, in.Op)
		for _, a := range in.Args {
			v.use(a, i, j, in, false)
		}
		if j == len(b.Instrs)-1 {
			v.edges(i, j, in)
		}
		if check := typing[in.Op]; check != nil {
			check(v, in)
		}
	}
}

// wire computes predecessor counts and dominators, and compares Preds and
// Succs with the edges.
func (v *verifier) wire(f *Function) {
	n := len(f.Blocks)
	succs := make([][]int, n)
	v.fs.npreds = make([]int, n)
	for i, b := range f.Blocks {
		for _, e := range b.edges() {
			t, ok := v.fs.blocks[e.Block]
			if !ok {
				v.where = fmt.Sprintf("function @%s b%d", f.Name, i)
				v.report(codeEdge, "edge leaves the function")
				continue
			}
			succs[i] = append(succs[i], t)
			v.fs.npreds[t]++
		}
	}
	v.fs.idom, v.fs.reach = dominators(succs)
	wantPreds, wantSuccs := f.wiring()
	for i, b := range f.Blocks {
		v.where = fmt.Sprintf("function @%s b%d", f.Name, i)
		if !sameBlocks(wantPreds[i], b.Preds) || !sameBlocks(wantSuccs[i], b.Succs) {
			v.report(codeWiring, "Preds or Succs disagree with the edges")
		}
		if !v.fs.reach[i] {
			v.report(codeReach, "block is unreachable from the entry block")
		}
	}
	if v.fs.npreds[0] > 0 {
		v.where = fmt.Sprintf("function @%s b0", f.Name)
		v.report(codeReach, "the entry block has predecessors")
	}
}

func sameBlocks(a, b []*Block) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// dominators returns the immediate dominator of every block reachable from
// block 0 (Cooper, Harvey and Kennedy) and which blocks are reachable.
func dominators(succs [][]int) (idom []int, reach []bool) {
	n := len(succs)
	reach = make([]bool, n)
	post := make([]int, n)
	var order []int
	var visit func(int)
	visit = func(b int) {
		reach[b] = true
		for _, s := range succs[b] {
			if !reach[s] {
				visit(s)
			}
		}
		post[b] = len(order)
		order = append(order, b)
	}
	visit(0)
	preds := make([][]int, n)
	for b := range succs {
		if !reach[b] {
			continue
		}
		for _, s := range succs[b] {
			preds[s] = append(preds[s], b)
		}
	}
	idom = make([]int, n)
	for i := range idom {
		idom[i] = -1
	}
	idom[0] = 0
	for changed := true; changed; {
		changed = false
		for k := len(order) - 2; k >= 0; k-- {
			b, next := order[k], -1
			for _, p := range preds[b] {
				switch {
				case idom[p] == -1:
				case next == -1:
					next = p
				default:
					next = intersect(idom, post, p, next)
				}
			}
			if idom[b] != next {
				idom[b], changed = next, true
			}
		}
	}
	return idom, reach
}

func intersect(idom, post []int, a, b int) int {
	for a != b {
		for post[a] < post[b] {
			a = idom[a]
		}
		for post[b] < post[a] {
			b = idom[b]
		}
	}
	return a
}

// edges checks the targets and arguments of the edges that leave block i.
func (v *verifier) edges(i, j int, in *Instr) {
	role := roleBranch
	if in.Except != nil {
		role = roleNormal
	}
	for _, e := range in.Succs {
		v.edge(i, j, in, e, role)
	}
	if in.Except == nil {
		return
	}
	if v.fs.npreds[v.fs.blocks[in.Succs[0].Block]] != 1 {
		v.report(codeInvoke, "the normal successor must have exactly one predecessor")
	}
	v.edge(i, j, in, *in.Except, roleExcept)
}

func (v *verifier) edge(i, j int, in *Instr, e Edge, role edgeRole) {
	t := e.Block
	if _, ok := v.fs.blocks[t]; !ok {
		return
	}
	landing := len(t.Instrs) > 0 && t.Instrs[0] != nil && t.Instrs[0].Op == OpLandingPad
	if landing != (role == roleExcept) {
		v.report(codeLanding, "b%d must start with landingpad exactly when an exception edge targets it", t.Index)
	}
	if len(e.Args) != len(t.Params) {
		v.report(codeEdge, "edge to b%d passes %d arguments for %d parameters", t.Index, len(e.Args), len(t.Params))
		return
	}
	for k, a := range e.Args {
		if !TypeOf(a).same(t.Params[k].Typ) {
			v.report(codeEdge, "argument %d of the edge to b%d is %s, want %s", k, t.Index, TypeOf(a), t.Params[k].Typ)
		}
		v.use(a, i, j, in, role == roleNormal)
	}
}

// use checks that a is defined in the function and available at instruction
// j of block i. An instruction with an exception edge defines its result on
// the normal edge only: ownNormal says that the use is that edge's argument.
func (v *verifier) use(a Value, i, j int, owner *Instr, ownNormal bool) {
	if c, ok := a.(*Const); ok {
		v.constant(c)
		return
	}
	if a == Value(owner) {
		switch {
		case ownNormal:
		case owner.Except != nil:
			v.report(codeInvoke, "the result is not available on the exception edge or as an operand")
		default:
			v.report(codeDom, "%s uses its own result", owner.Op)
		}
		return
	}
	d, ok := v.fs.defs[a]
	switch {
	case !ok:
		v.report(codeDef, "operand is not defined in this function")
	case d.block < 0 || !v.fs.reach[i]:
	case d.block == i && d.pos >= j:
		v.report(codeDom, "operand is used before its definition")
	case d.block != i && !v.dominates(d.block, i):
		v.report(codeDom, "operand does not dominate its use")
	}
}

func (v *verifier) dominates(a, b int) bool {
	for a != b {
		if b == 0 || v.fs.idom[b] < 0 {
			return false
		}
		b = v.fs.idom[b]
	}
	return true
}

func (v *verifier) constant(c *Const) {
	v.typ(c.Typ)
	if c.Undef {
		if c.Value != nil {
			v.report(codeConst, "undef has a value")
		}
		return
	}
	var kind Kind
	switch c.Value.(type) {
	case ast.Int:
		kind = KindInt
		if c.Typ.Kind == KindI32 {
			kind = KindI32
		}
	case ast.Float:
		kind = KindF64
	case ast.Bool:
		kind = KindI1
	case ast.Str:
		kind = KindStr
	case ast.Bytes:
		kind = KindBytes
	case ast.None:
		if c.Typ.Nullable {
			return
		}
		kind = KindNone
	default:
		v.report(codeConst, "constant %T is not supported", c.Value)
		return
	}
	if c.Typ.Kind != kind && c.Typ.Kind != KindAny {
		v.report(codeConst, "%T cannot have type %s", c.Value, c.Typ)
	}
}

// defineAll records the definition of every value and checks the structure
// of each block. It reports false when the instructions cannot be analysed.
func (v *verifier) defineAll(f *Function, names map[string]bool) bool {
	ok := true
	ids := map[int]bool{}
	for i, b := range f.Blocks {
		v.where = fmt.Sprintf("function @%s b%d", f.Name, i)
		if i == 0 && len(b.Params) > 0 {
			v.report(codeBlock, "the entry block takes no parameters")
		}
		for _, p := range b.Params {
			v.param(p, names)
			v.fs.defs[p] = def{block: i, pos: -1}
		}
		if len(b.Instrs) == 0 {
			v.report(codeBlock, "block has no instructions")
		}
		for j, in := range b.Instrs {
			if in == nil {
				v.report(codeShape, "instruction %d is nil", j)
				return false
			}
			if msg := in.shape(); msg != "" {
				v.report(codeShape, "%s: %s", in.Op, msg)
				ok = false
				continue
			}
			v.structure(b, j, in)
			v.define(i, j, in, ids)
		}
	}
	return ok
}

func (v *verifier) define(i, j int, in *Instr, ids map[int]bool) {
	if in.Op.info().flags&fResult == 0 {
		if !in.Typ.same(Type{}) {
			v.report(codeShape, "%s defines no value but has a type", in.Op)
		}
		return
	}
	v.typ(in.Typ)
	if _, dup := v.fs.defs[in]; dup || ids[in.ID] {
		v.report(codeDef, "%%%d is defined twice", in.ID)
	}
	ids[in.ID] = true
	if in.Except != nil {
		v.fs.defs[in] = def{block: v.fs.blocks[in.Succs[0].Block], pos: -1}
		return
	}
	v.fs.defs[in] = def{block: i, pos: j}
}

func (v *verifier) structure(b *Block, j int, in *Instr) {
	info := in.Op.info()
	last := j == len(b.Instrs)-1
	ends := info.flags&fTerm != 0 || in.Except != nil
	switch {
	case last && !ends:
		v.report(codeBlock, "block does not end in a terminator or an instruction with an exception edge")
	case !last && info.flags&fTerm != 0:
		v.report(codeBlock, "%s is not the last instruction of its block", in.Op)
	case !last && in.Except != nil:
		v.report(codeInvoke, "%s has an exception edge and must end its block", in.Op)
	}
	if in.Op == OpLandingPad && j != 0 {
		v.report(codeLanding, "landingpad must be the first instruction of its block")
	}
}

func (v *verifier) param(p *Param, names map[string]bool) {
	if !validName(p.Name) || isNumeric(p.Name) {
		v.report(codeDef, "parameter name %q is empty, numeric or has bad characters", p.Name)
	}
	if names[p.Name] {
		v.report(codeDef, "parameter %%%s is defined twice", p.Name)
	}
	names[p.Name] = true
	v.typ(p.Typ)
}

func validName(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; !isLetter(c) && !isDigit(c) && c != '_' && c != '$' {
			return false
		}
	}
	return s != ""
}

func (v *verifier) class(c *Class) {
	v.where = "class @" + c.Name
	for _, b := range c.Bases {
		switch {
		case !v.classes[b]:
			v.report(codeMember, "base is not a member of the program")
		case b.derives(c):
			v.report(codeMember, "base @%s makes the hierarchy cyclic", b.Name)
		}
	}
	fields := map[string]bool{}
	for _, f := range c.Fields {
		if fields[f.Name] {
			v.report(codeMember, "field %s is repeated", f.Name)
		}
		fields[f.Name] = true
		if !v.classes[f.Owner] || !c.derives(f.Owner) {
			v.report(codeMember, "field %s has an owner that is neither the class nor a base", f.Name)
		}
		v.typ(f.Type)
	}
	methods := map[string]bool{}
	for _, m := range c.Methods {
		if methods[m.Name] {
			v.report(codeMember, "method %s is repeated", m.Name)
		}
		methods[m.Name] = true
		if !v.funcs[m.Func] {
			v.report(codeMember, "method %s is not a function of the program", m.Name)
		}
	}
}

// typ checks that t is well formed and names only classes of the program.
func (v *verifier) typ(t Type) {
	info := t.Kind.info()
	isClass := t.Kind == KindClass || t.Kind == KindTypeOf
	switch {
	case info.name == "":
		v.report(codeBadType, "type has no kind")
		return
	case info.elems >= 0 && len(t.Elems) != info.elems:
		v.report(codeBadType, "%s takes %d element types, has %d", info.name, info.elems, len(t.Elems))
	case t.Kind == KindFunc && len(t.Elems) == 0:
		v.report(codeBadType, "func needs a result type")
	case t.Variadic && (t.Kind != KindTuple || len(t.Elems) != 1):
		v.report(codeBadType, "only tuple<T...> is variadic")
	case t.Nullable && !info.ref:
		v.report(codeBadType, "%s cannot be nullable", info.name)
	case isClass != (t.Class != nil):
		v.report(codeBadType, "only class and type<C> name a class")
	case t.Class != nil && !v.classes[t.Class]:
		v.report(codeMember, "class @%s is not a member of the program", t.Class.Name)
	}
	for _, e := range t.Elems {
		v.typ(e)
	}
}

// collect registers the members of p and reports duplicate names.
func (v *verifier) collect(p *Program) {
	modules := map[string]bool{}
	for _, m := range p.Modules {
		v.where = "module " + m.Name
		if modules[m.Name] || m.Name == "" {
			v.report(codeMember, "module name %q is empty or repeated", m.Name)
		}
		modules[m.Name] = true
		names := map[string]bool{}
		for _, f := range m.Functions {
			if v.funcs[f] || names[f.Name] || f.Name == "" {
				v.report(codeMember, "function @%s is repeated or unnamed", f.Name)
			}
			v.funcs[f], names[f.Name] = true, true
		}
		names = map[string]bool{}
		for _, g := range m.Globals {
			if v.globals[g] || names[g.Name] || g.Name == "" {
				v.report(codeMember, "global @%s is repeated or unnamed", g.Name)
			}
			v.globals[g], names[g.Name] = true, true
		}
	}
	v.where = "program"
	names := map[string]bool{}
	for _, c := range p.Classes {
		if v.classes[c] || names[c.Name] || c.Name == "" {
			v.report(codeMember, "class @%s is repeated or unnamed", c.Name)
		}
		v.classes[c], names[c.Name] = true, true
	}
}

func (v *verifier) report(code int, format string, args ...any) {
	v.diags = append(v.diags, fmt.Sprintf("SSA%03d %s: %s", code, v.where, fmt.Sprintf(format, args...)))
}
