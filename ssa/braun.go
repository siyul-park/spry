package ssa

import (
	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/types"
)

// fbuild lowers one function with the construction of Braun et al., "Simple
// and Efficient Construction of SSA Form": every variable is read and written
// per block, a read in a block with several predecessors creates a block
// parameter, and a block whose predecessors are not all known yet is
// unsealed and completes its parameters when it is sealed. Trivial
// parameters are removed when the function ends.
type fbuild struct {
	b      *builder
	fn     *Function
	spec   *types.Spec
	cur    *Block
	defs   map[*Block]map[bkey]Value
	sealed map[*Block]bool
	open   map[*Block][]pending
	in     map[*Block][]edgeRef
	undefs map[string]*Const
	loops  []*loopCtx
	// regions are the try, finally and with statements being built, outermost
	// first. caught holds the exceptions of the handlers being built, which a
	// bare raise raises again. plain counts the nested constructions that
	// emit raising instructions without exception edges.
	regions []*region
	caught  []Value
	plain   int
	// preIter is the iterator of the first loop of a generator expression,
	// which is passed in.
	preIter Value
	// eager is positive while the arguments of a call of a function that
	// consumes a whole iterable are built, so that a generator is run to its
	// end first.
	eager int
	// known holds the values of expressions that are evaluated already.
	known  map[ast.Expr]Value
	vtypes map[types.Object]Type
	cells  map[types.Object]Value
	flags  map[types.Object]Value
	origin ast.Node
	owner  *types.Named
}

type loopCtx struct {
	brk, cont *Block
	// depth is the number of regions open where the loop starts.
	depth int
}

type pending struct {
	key bkey
	p   *Param
}

type edgeRef struct {
	from *Block
	e    *Edge
}

// bkey names an SSA variable: a Python variable, or the flag that tells
// whether it is bound.
type bkey struct {
	obj   types.Object
	bound bool
}

func newFbuild(b *builder, fn *Function, spec *types.Spec) *fbuild {
	f := &fbuild{
		b: b, fn: fn, spec: spec,
		defs: map[*Block]map[bkey]Value{}, sealed: map[*Block]bool{}, open: map[*Block][]pending{},
		in: map[*Block][]edgeRef{}, undefs: map[string]*Const{}, vtypes: map[types.Object]Type{}, known: map[ast.Expr]Value{},
	}
	f.cur = f.newBlock()
	f.seal(f.cur)
	return f
}

// val emits an instruction that defines a value, or gives an undefined
// value when the code is unreachable.
func (f *fbuild) val(op Op, typ Type, args ...Value) Value {
	if in := f.emit(op, typ, args...); in != nil {
		return in
	}
	return f.undef(typ)
}

func (f *fbuild) jump(to *Block, args ...Value) {
	if f.cur == nil {
		return
	}
	in := &Instr{Op: OpJump, Succs: make([]Edge, 1)}
	f.place(in)
	f.addEdge(f.cur, &in.Succs[0], to, args)
	f.cur.Instrs = append(f.cur.Instrs, in)
	f.cur = nil
}

// finish seals what is left, drops unreachable blocks and trivial block
// parameters, and numbers the function.
func (f *fbuild) finish() {
	for _, blk := range f.fn.Blocks {
		f.seal(blk)
	}
	f.prune()
	for f.simplify() {
	}
	f.normalize()
	f.fn.finish()
}

func (f *fbuild) prune() {
	reach := map[*Block]bool{f.fn.Blocks[0]: true}
	work := []*Block{f.fn.Blocks[0]}
	for len(work) > 0 {
		blk := work[len(work)-1]
		work = work[:len(work)-1]
		for _, e := range blk.edges() {
			if !reach[e.Block] {
				reach[e.Block] = true
				work = append(work, e.Block)
			}
		}
	}
	var keep []*Block
	for _, blk := range f.fn.Blocks {
		if reach[blk] {
			keep = append(keep, blk)
		}
	}
	f.fn.Blocks = keep
	for i, blk := range keep {
		blk.Index = i
	}
}

// simplify removes the block parameters that always receive one value.
func (f *fbuild) simplify() bool {
	repl := map[*Param]Value{}
	incoming := map[*Block][]*Edge{}
	for _, blk := range f.fn.Blocks {
		for _, e := range blk.edges() {
			incoming[e.Block] = append(incoming[e.Block], e)
		}
	}
	drop := map[*Block]map[int]bool{}
	pinned := f.pinned()
	for _, blk := range f.fn.Blocks {
		for i, p := range blk.Params {
			if v := trivial(p, i, incoming[blk]); v != nil && !(isConstValue(v) && pinned[p]) {
				repl[p] = v
				if drop[blk] == nil {
					drop[blk] = map[int]bool{}
				}
				drop[blk][i] = true
			}
		}
	}
	if len(repl) == 0 {
		return false
	}
	resolve := func(v Value) Value {
		for {
			p, ok := v.(*Param)
			if !ok || repl[p] == nil {
				return v
			}
			v = repl[p]
		}
	}
	for _, blk := range f.fn.Blocks {
		for _, in := range blk.Instrs {
			for i, a := range in.Args {
				in.Args[i] = resolve(a)
			}
			for j := range in.Succs {
				rewriteEdge(&in.Succs[j], resolve)
			}
			if in.Except != nil {
				rewriteEdge(in.Except, resolve)
			}
		}
	}
	for blk, idx := range drop {
		var ps []*Param
		for i, p := range blk.Params {
			if !idx[i] {
				ps = append(ps, p)
			}
		}
		blk.Params = ps
		for _, e := range incoming[blk] {
			var args []Value
			for i, a := range e.Args {
				if !idx[i] {
					args = append(args, a)
				}
			}
			e.Args = args
		}
	}
	return true
}

func rewriteEdge(e *Edge, resolve func(Value) Value) {
	for i, a := range e.Args {
		e.Args[i] = resolve(a)
	}
}

// trivial returns the one value that every edge passes to parameter i other
// than the parameter itself, or nil.
func trivial(p *Param, i int, edges []*Edge) Value {
	var same Value
	for _, e := range edges {
		a := e.Args[i]
		if a == Value(p) || a == same {
			continue
		}
		if same != nil {
			return nil
		}
		same = a
	}
	return same
}

func isConstValue(v Value) bool {
	_, ok := v.(*Const)
	return ok
}

// pinned lists the block parameters that are operand 0 of an instruction
// that takes no constant there, so that a constant must not replace them.
func (f *fbuild) pinned() map[*Param]bool {
	out := map[*Param]bool{}
	for _, blk := range f.fn.Blocks {
		for _, in := range blk.Instrs {
			info := in.Op.info()
			if p, ok := firstArg(in).(*Param); ok && info.flags&(fCall|fDot) != 0 && info.aux != auxCallee {
				out[p] = true
			}
		}
	}
	return out
}

func firstArg(in *Instr) Value {
	if len(in.Args) == 0 {
		return nil
	}
	return in.Args[0]
}

// normalize makes an empty operand list nil, as Parse reads it.
func (f *fbuild) normalize() {
	for _, blk := range f.fn.Blocks {
		for _, in := range blk.Instrs {
			if len(in.Args) == 0 {
				in.Args = nil
			}
			for _, e := range blk.edges() {
				if len(e.Args) == 0 {
					e.Args = nil
				}
			}
		}
	}
}

// startModule opens the body of a module: it returns at once when the module
// already ran, and otherwise marks it as run and binds __name__ and __file__.
func (f *fbuild) startModule() []ast.Stmt {
	mod := f.fn.Module
	flag := f.global(mod, "$init")
	done, run := f.newBlock(), f.newBlock()
	f.branch(f.load(flag), done, run)
	f.seal(run)
	f.seal(done)
	f.start(done)
	f.end(OpRet)
	f.start(run)
	f.store(flag, boolConst(true))
	m := f.spec.Func.Module()
	for _, name := range []string{"__name__", "__file__"} {
		v, ok := m.Scope().Lookup(name).(*types.Var)
		if !ok {
			continue
		}
		text := m.Name()
		if name == "__file__" {
			text = m.Path()
		}
		f.store(f.b.globalOf(v), &Const{Value: ast.Str(text), Typ: tyStr})
	}
	return f.spec.Func.Decl().(*ast.Module).Body
}

func (f *fbuild) newBlock(explicit ...Type) *Block {
	blk := &Block{Index: len(f.fn.Blocks)}
	for _, t := range explicit {
		blk.Params = append(blk.Params, &Param{Typ: t})
	}
	f.fn.Blocks = append(f.fn.Blocks, blk)
	f.defs[blk] = map[bkey]Value{}
	return blk
}

// start makes blk the block that instructions are appended to.
func (f *fbuild) start(blk *Block) {
	f.cur = blk
}

func (f *fbuild) branch(cond Value, t, e *Block) {
	f.branchArgs(cond, t, nil, e, nil)
}

func (f *fbuild) branchArgs(cond Value, t *Block, targs []Value, e *Block, eargs []Value) {
	if f.cur == nil {
		return
	}
	in := &Instr{Op: OpBr, Args: []Value{cond}, Succs: make([]Edge, 2)}
	f.place(in)
	f.addEdge(f.cur, &in.Succs[0], t, targs)
	f.addEdge(f.cur, &in.Succs[1], e, eargs)
	f.cur.Instrs = append(f.cur.Instrs, in)
	f.cur = nil
}

func (f *fbuild) addEdge(from *Block, e *Edge, to *Block, explicit []Value) {
	e.Block = to
	e.Args = make([]Value, len(to.Params))
	copy(e.Args, explicit)
	f.in[to] = append(f.in[to], edgeRef{from, e})
}

func (f *fbuild) seal(blk *Block) {
	if f.sealed[blk] {
		return
	}
	f.sealed[blk] = true
	for _, p := range f.open[blk] {
		idx := 0
		for i, q := range blk.Params {
			if q == p.p {
				idx = i
			}
		}
		f.fill(blk, p.key, idx)
	}
	delete(f.open, blk)
}

func (f *fbuild) readVar(k bkey, blk *Block) Value {
	if blk == nil { // unreachable code reads nothing that was assigned
		return f.unbound(k)
	}
	if v, ok := f.defs[blk][k]; ok {
		return v
	}
	var v Value
	switch preds := f.in[blk]; {
	case !f.sealed[blk]:
		p, _ := f.addParam(blk, f.varType(k))
		f.open[blk] = append(f.open[blk], pending{k, p})
		v = p
	case len(preds) == 0:
		v = f.unbound(k)
	case len(preds) == 1:
		// Blocks that nothing reaches may form a cycle of single predecessors;
		// reading unbound there ends it.
		f.writeVar(k, blk, f.unbound(k))
		v = f.readVar(k, preds[0].from)
	default:
		p, idx := f.addParam(blk, f.varType(k))
		f.writeVar(k, blk, p)
		f.fill(blk, k, idx)
		v = p
	}
	f.writeVar(k, blk, v)
	return v
}

func (f *fbuild) fill(blk *Block, k bkey, idx int) {
	for _, r := range f.in[blk] {
		r.e.Args[idx] = f.readVar(k, r.from)
	}
}

func (f *fbuild) writeVar(k bkey, blk *Block, v Value) {
	if blk == nil { // unreachable code has no block to define the variable in
		return
	}
	f.defs[blk][k] = v
}

// unbound is the value of a variable no assignment reaches.
func (f *fbuild) unbound(k bkey) Value {
	if k.bound {
		return &Const{Value: ast.Bool(false), Typ: tyI1}
	}
	return f.undef(f.varType(k))
}

func (f *fbuild) undef(t Type) *Const {
	k := t.String()
	if c := f.undefs[k]; c != nil {
		return c
	}
	c := &Const{Undef: true, Typ: t}
	f.undefs[k] = c
	return c
}

func (f *fbuild) varType(k bkey) Type {
	if k.bound {
		return tyI1
	}
	if t, ok := f.vtypes[k.obj]; ok {
		return t
	}
	return tyAny
}

func (f *fbuild) addParam(blk *Block, t Type) (*Param, int) {
	p := &Param{Typ: t}
	blk.Params = append(blk.Params, p)
	for _, r := range f.in[blk] {
		r.e.Args = append(r.e.Args, nil)
	}
	return p, len(blk.Params) - 1
}

func (f *fbuild) global(m *Module, name string) *Global {
	for _, g := range m.Globals {
		if g.Name == name {
			return g
		}
	}
	return nil
}

// fallOff ends the function where the control reaches its end: with None.
func (f *fbuild) fallOff(init bool) {
	if !f.alive() {
		return
	}
	res := f.fn.Result
	switch {
	case res.same(tyNone):
		f.end(OpRet)
	case res.same(tyAny) || res.Nullable:
		f.end(OpRet, f.conv(&Const{Value: ast.None{}, Typ: tyNone}, res))
	default:
		f.end(OpUnreachable)
	}
}

func (f *fbuild) alive() bool {
	return f.cur != nil
}

// end appends a terminator without edges.
func (f *fbuild) end(op Op, args ...Value) {
	if f.emit(op, Type{}, args...) != nil {
		f.cur = nil
	}
}

func (f *fbuild) emit(op Op, typ Type, args ...Value) *Instr {
	if f.cur == nil {
		return nil
	}
	in := &Instr{Op: op, Typ: typ, Args: args}
	f.place(in)
	f.cur.Instrs = append(f.cur.Instrs, in)
	if r := f.catcher(); r != nil && f.plain == 0 && op != OpYield && op.info().flags&fRaise != 0 {
		f.split(in, r)
	}
	return in
}

func (f *fbuild) place(in *Instr) {
	if f.origin != nil && f.b.fset != nil {
		in.Pos = f.b.fset.Position(f.origin.Pos())
		in.End = f.b.fset.Position(f.origin.End())
	}
}
