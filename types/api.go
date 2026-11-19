package types

import (
	"io/fs"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/scanner"
	"github.com/siyul-park/spry/token"
)

// Info holds the facts Check establishes about a program. Scopes maps a
// module, class, function, lambda or comprehension node to its scope, the
// first type parameter of a generic declaration to its type-parameter scope,
// the Arguments of a function with annotations to its annotation scope and
// the value or bound of a type alias or type parameter to its scope.
type Info struct {
	Types      map[ast.Expr]TypeAndValue
	Instances  map[ast.Expr]Instance
	Defs       map[ast.Node]Object
	Uses       map[ast.Expr]Object
	Implicits  map[ast.Node]Object
	Selections map[*ast.Attribute]*Selection
	Scopes     map[ast.Node]*Scope
	Specs      map[*Func][]*Spec
	Fallbacks  []Fallback
	// Defined holds the loads of a name that are not always bound: a name
	// that is possibly bound or never bound when it is read.
	Defined map[*ast.Name]Definedness
	// Synth maps an expression that is not a call to the call the checker made
	// of it: the class of a raise statement (raise ValueError) to its
	// instantiation without arguments, and a decorator to its application to
	// the function it decorates.
	Synth map[ast.Expr]*ast.Call
	// Matched maps a class pattern to the attribute reads it makes: the
	// positional patterns first, then the keyword ones, all on one placeholder
	// receiver.
	Matched map[ast.Pattern][]*ast.Attribute

	imports map[ast.Node]importResult
}

// TypeAndValue is the type of an expression and how it is used: as a type, a
// value or a builtin.
type TypeAndValue struct {
	Type Type
	mode typeMode
}

// Instance is a use of a generic class, alias or function with type
// arguments.
type Instance struct {
	TypeArgs *TypeList
	Type     Type
}

// Spec is one specialization of a function.
type Spec struct {
	Func        *Func
	TypeArgs    *TypeList
	Signature   *Signature
	Generalized bool
	// Generator says that the function yields: its result is the
	// Generator[Y, S, R] it makes when called.
	Generator bool
	Types     map[ast.Expr]TypeAndValue
	Calls     map[ast.Node]*Call
	// Vars maps each local variable and parameter to the join of the types
	// stored in it. A builder keeps the variable in that type and converts
	// where an expression is narrower.
	Vars map[*Var]Type
	// Dead holds the statements the analysis proved unreachable in this
	// specialization, such as the branch that a narrowing to Never leaves.
	Dead map[ast.Stmt]bool
	// Patterns maps each pattern of a match to the type of the value it is
	// matched against.
	Patterns map[ast.Pattern]Type
	// Outer is the specialization of the enclosing function that made the
	// closures of this one, or nil.
	Outer *Spec
	// Captures lists the variables of enclosing functions this
	// specialization reads or writes, and Cells the type of each variable
	// of a function that a nested function captures.
	Captures []Object
	Cells    map[Object]Type
	// Guarded holds the variables of Cells that may be unbound when a nested
	// function reads them.
	Guarded map[Object]bool
}

// Call is the resolution of one call site, or of an implicit dispatch: an
// operator, a truth test, an iteration, a conversion in an f-string. Func is
// the extern function or method of a shipped module that implements it, with
// Overload the index of the selected overload (-1 if not overloaded). Slot is
// the dunder or method name; for an operator it is the one that was chosen,
// so a reflected operand order shows as __radd__. A Static call without
// Target or Func is a builtin operation on scalars.
type Call struct {
	Kind     CallKind
	Target   *Spec
	Func     *Func
	Overload int
	Slot     string
	// Overrides lists, for a Virtual call, the implementation each class in
	// the cone of the receiver runs: the class that holds the entry of the
	// slot and the specialization it calls. Super is the class around a call
	// through super(), whose entries sit on the receiver classes themselves.
	Overrides []Override
	Super     *Named
	// Names lists the attributes that the positional patterns of a class
	// pattern read.
	Names []string
	// Negate says that the result of the method is inverted: != that __eq__
	// implements.
	Negate bool
}

// Override is one implementation of a virtual slot.
type Override struct {
	Class  *Named
	Target *Spec
}

// Site keys an implicit call that no node of its own stands for: the truth
// test of an expression, the i-th comparison of a chain, the __next__ call of
// a for loop. It is an ast.Node at the position of the node it concerns, so
// that it cannot collide with the *ast.Call of that node. The other implicit
// calls are keyed by their node: a BinOp, UnaryOp, AugAssign, FormattedValue
// or, for __iter__, the For statement.
type Site struct {
	Node  ast.Node
	Kind  SiteKind
	Index int
}

// SiteKind says which implicit call a Site is.
type SiteKind uint8

// Definedness tells whether a name is bound when it is read.
type Definedness uint8

// CallKind classifies how a call is dispatched.
type CallKind uint8

// Fallback is a site where a value had to be Any.
type Fallback struct {
	Pos, End token.Position
	Reason   string
	Msg      string
}

// Import is the outcome of one import site. A site that cannot import is not
// an error of the program: Python raises at run time, and so does the code
// generated for it.
type Import struct {
	Kind ImportKind
	// Module is the module the site binds, set for ImportOK.
	Module *Module
	// Err is the SyntaxError of the module for ImportSyntax, and the reason
	// for ImportUnshipped.
	Err error
}

// ImportKind is what an import site resolved to.
type ImportKind uint8

// Option configures Check.
type Option func(*config)

type config struct {
	roots []root
}

type typeMode uint8

// Site kinds.
const (
	SiteTruth SiteKind = iota
	SiteCompare
	SiteNext
	SiteIter
	SiteStore
	SiteDelete
	SiteConvert
	SiteOverride
	SiteEnter
	SiteExit
)

// Definedness values.
const (
	Defined Definedness = iota
	MaybeDefined
	Undefined
)

// Call kinds.
const (
	Static CallKind = iota
	Virtual
	CallClosure
	Dynamic
)

// Import kinds.
const (
	ImportOK        ImportKind = iota // the module was found and parsed
	ImportNotFound                    // no module of that name exists
	ImportSyntax                      // the module has a compile-stage SyntaxError
	ImportRelative                    // a relative import outside a package
	ImportUnshipped                   // a standard library module spry does not ship
)

const (
	modeValue typeMode = iota
	modeType
)

// WithFS adds a sys.path entry: modules are read from fsys, and dir is the
// absolute path that fsys stands for in __file__ and in positions. Entries
// are searched in the order they are given; without any, only the modules
// shipped with spry can be imported.
func WithFS(fsys fs.FS, dir string) Option {
	return func(c *config) { c.roots = append(c.roots, root{fsys: fsys, dir: dir}) }
}

// Check loads every module reachable from file, the module named filename,
// and establishes its scopes and declarations. A compile-stage SyntaxError of
// the main module is returned first, as a scanner.ErrorList that holds the
// one error CPython raises; otherwise the error is an ErrorList of the
// diagnostics found. The result is partial but not nil when there are errors.
func Check(fset *token.FileSet, filename string, file *ast.Module, opts ...Option) (*Module, *Info, error) {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	info := newInfo()
	p := newProgram(fset, info, cfg.roots)
	main, err := p.addMain(filename, file)
	if err != nil {
		return main.mod, info, err
	}
	p.declare()
	newSolver(p).run()
	return main.mod, info, p.errs.Err()
}

// Import returns the outcome of the import site: the *ast.Alias of an import
// statement or an *ast.ImportFrom. The second result is false for any other
// node.
func (info *Info) Import(site ast.Node) (Import, bool) {
	res, ok := info.imports[site]
	if !ok {
		return Import{}, false
	}
	return Import{Kind: res.kind, Module: res.module, Err: res.err}, true
}

// IsType reports whether the expression denotes a type.
func (tv TypeAndValue) IsType() bool {
	return tv.mode == modeType
}

// IsValue reports whether the expression is a value.
func (tv TypeAndValue) IsValue() bool {
	return tv.mode == modeValue
}

// Pos returns the position of the node of the site.
func (s Site) Pos() token.Pos {
	return s.Node.Pos()
}

// End returns the end of the node of the site.
func (s Site) End() token.Pos {
	return s.Node.End()
}

func newInfo() *Info {
	return &Info{
		Types:      map[ast.Expr]TypeAndValue{},
		Instances:  map[ast.Expr]Instance{},
		Defs:       map[ast.Node]Object{},
		Uses:       map[ast.Expr]Object{},
		Implicits:  map[ast.Node]Object{},
		Selections: map[*ast.Attribute]*Selection{},
		Scopes:     map[ast.Node]*Scope{},
		Specs:      map[*Func][]*Spec{},
		Defined:    map[*ast.Name]Definedness{},
		Synth:      map[ast.Expr]*ast.Call{},
		Matched:    map[ast.Pattern][]*ast.Attribute{},
		imports:    map[ast.Node]importResult{},
	}
}

func scannerList(err *scanner.Error) scanner.ErrorList {
	return scanner.ErrorList{err}
}
