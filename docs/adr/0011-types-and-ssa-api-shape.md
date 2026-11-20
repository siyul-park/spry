# 0011: Types and SSA API shape

- Status: Accepted

## Context

`types` and `ssa` need a public shape before the optimizer and the backend depend on them. Three references pull in different directions:

- `go/types` and `golang.org/x/tools/go/ssa` give Go readers a familiar vocabulary (`Type`, `Underlying`, `Named`, `Info`, `Selection`, `Scope`, `Program`, `Function`, `Block`).
- Python's `typing` module defines the types themselves: classes with an MRO, unions that keep their order, `Literal`, `TypeGuard`, `Protocol`, overloads, a gradual `Any`.
- The repo's coding rules (docs/coding-patterns.md) ask for construction as initialization, functional options and no callbacks where nothing needs them.

Two further constraints shape the decision. First, the library the checker reasons about (`builtins`, `typing`, `math`, ...) must be data, not Go tables, so that its signatures are the ones CPython's own tools read. Second, the IR must be printable and parseable in a stable text form, since goldens and optimizer tests start from text (ADR 0001).

## Decision

### Type system

`types` follows `go/types` for file layout and symbol roles, and `typing` for meaning.

- `Type` is `interface{ Underlying() Type; String() string }`. `Underlying` returns what decides attribute lookup and representation: the `*Class` of a `*Named`, the base class of a `Literal`, `Guard` or `LiteralString`, the bound of a `TypeParam`, and the type itself otherwise. `Typ` indexes the `Basic` types (`Invalid`, `Any`, `Never`, `LiteralString`); `Universe` is the scope of the `builtins` module.
- `Named` is an instance type of a class (`int`, `None`, `list[int]`, an application class). A generic class has one origin and one `Named` per list of type arguments, all sharing the origin's `Class`. `TypeName` declares a class, alias, type parameter or `NewType`; `Class` is the declaration structure and the `Underlying` of its `Named`.
- `Signature` has a parameter `*Tuple` whose variables carry the parameter kind (positional-only, keyword-only, `*args`, `**kwargs`) and one `Result`. `Tuple` is both the parameter list and the Python `tuple` type; `WithUnbounded` marks `tuple[T, ...]` and `*Ts`. `TypeParam` has the three kinds (`TypeVar`, `ParamSpec`, `TypeVarTuple`), bound, constraints, default and variance. `Alias` is the type introduced by a `type` statement.
- `Selection` (from `LookupAttr`) says how an attribute was found and its type as read after binding. `Info` holds the facts of `Check`: `Types` (`TypeAndValue`), `Instances`, `Defs`, `Uses`, `Implicits`, `Selections`, `Scopes`, and the spry additions `Specs`, `Fallbacks`, `Defined`, `Synth`, `Matched` and `Import`. `Instance` records a use of a generic with type arguments.
- `Identical`, `AssignableTo`, `Join`, `Default`, `Unalias`, `Instantiate` and `LookupAttr` are package functions, not methods.

Deviations from `go/types`, each forced by Python:

| `go/types` | spry | Reason |
|---|---|---|
| A type name in an expression denotes a type | A class used as a value has type `Meta` (`type[T]`); `TypeAndValue` has a mode: a type expression (annotation) or a value | Python classes are values; the same name is both |
| `Named.Underlying` is substituted for instances | `Named.Underlying` is the unsubstituted `*Class` | The runtime class object of `list[int]` is the one of `list` |
| `Interface` for protocols | `Class` with `ClassKind` (`OrdinaryClass`, `ProtocolClass`, `TypedDictClass`, `NewTypeClass`) | A protocol is a class with an MRO, members and runtime checkability; one structure serves all |
| Union is a set of terms | `Union` keeps source order and prints as written | `typing` prints `int \| str` in the order given |
| No equivalent | `Literal`, `Guard` (`TypeGuard`/`TypeIs`), `Overload`, `ParamSpecArgs` | Python typing constructs the checker evaluates |
| Assignability is a preorder | `AssignableTo` treats `Any` as assignable both ways, so it is not a partial order | Gradual typing |
| `Signature.Recv` | No receiver; methods keep `self` as the first parameter and `Selection` kinds `MethodVal` (bound, receiver dropped) and `MethodExpr` (read from the class, receiver kept) | Python binds by descriptor, not by receiver syntax |
| `Signature.Results` is a tuple | One `Result` | Python returns one value; a tuple type stands for several |
| Selection kinds `FieldVal`, `MethodVal`, `MethodExpr` | Adds `ClassVal`, `DescriptorVal` (property) and `GetattrVal` (`__getattr__`) | Python attribute lookup |
| Packages | `Module` is also a `Type`, the type of the module object | A module is a first-class value |
| `Scope.Names` is sorted | `Scope.Names` is declaration order | Python namespaces are ordered and dumps must be deterministic |
| `Defs`/`Uses` keyed by `*ast.Ident` | `Defs` is keyed by `ast.Node`, `Uses` by `ast.Expr` | A definition can be a `Name`, `Alias`, `FunctionDef`, `ClassDef`, `Arg` or `ExceptHandler` |
| `Config.Check` | `Check` is a package function with options | See Entry points |

### Universe from stubs

The library is Python source embedded from `types/lib` (`builtins`, `typing`, `math`, `sys`, `__future__`, `collections`, `collections.abc`), parsed and declared by the same loader as a program. A function body of `...` is an extern: the host implements it and the builder lowers a call to an extern function. A function with a real body (`__sorted__`, `__min__`, `__max__`, `__sum__` in `builtins`) is prelude: calls that pass `key=` or work on instances of program classes resolve to it, and it is compiled with the program, because a host function cannot call back into Python. `builtins` and the modules it imports are declared when the package initializes so that `Universe` exists for every file; other shipped modules are declared when a program first imports them.

### Info and Spec

`Info` is generalized: `Info.Types` joins the type of an expression over every specialization that analyzes it. `Spec` is monomorphized: one specialization of a function, an `Instance` keyed by `(Func, TypeArgs)` (and, for a nested function, by the specialization that made its closure, `Spec.Outer`). Every parameter and the result of an unannotated function is an implicit `TypeParam` named `x@f` and `return@f`; specialization is instantiation of those parameters, so the generic machinery (`Instantiate`, substitution, inference) serves it. `Spec` carries what the builder reads: its `Signature`, `Types`, `Calls` (how each call site and implicit dispatch is resolved), `Vars`, `Dead`, `Patterns`, `Captures`, `Cells` and `Guarded`. A `Spec` is always built from published types: `Any` provenance markers the solver uses internally never leak, so published `Info` and `Spec` types carry `Typ[Any]`.

### Entry points

```go
func Check(fset *token.FileSet, filename string, file *ast.Module, opts ...Option) (*Module, *Info, error)
func WithFS(fsys fs.FS, dir string) Option
func Build(fset *token.FileSet, main *types.Module, info *types.Info) (*Program, error) // package ssa
```

`Check` takes the already parsed main module, loads everything it imports through the `WithFS` entries (ADR 0012), and returns a possibly partial `*Module` and `*Info` with an `ErrorList`. The error is a `scanner.ErrorList` instead when the main module has a compile-stage `SyntaxError`, which CPython raises after parsing. `ssa.Build` lowers the checked program and verifies its result.

Precedents:

- `go/types` `Config.Check(path, fset, files, info)` takes a `Config` with an `Importer` and an `Error` callback and fills a caller-supplied `*Info`. spry has no consumer that supplies an importer or error policy: imports are an `fs.FS`, errors are collected in a list, and `Info` is created by `Check` (docs/coding-patterns.md: options over config structs, no callbacks).
- `go/parser.ParseFile(fset, filename, src, mode)` takes a `FileSet` and a file name; `Check` takes the same pair and parsed input, as ADR 0009 does for `parser`.
- `x/tools/go/ssa/ssautil.BuildPackage(...)` builds one package from the type-checked result. `ssa.Build` builds the whole program from `(fset, main, info)`.

### Text is Stringer-only

`Type.String` and `TypeString(t, qf)` write the representation of `python3.14` `typing._type_repr` (`collections.abc.Callable[[int], str]`, `int | str`), qualified by an optional `Qualifier`. The oracle is a checked-in manifest, `types/testdata/cpython/typerepr.jsonl`, recorded from CPython (docs/testing.md). Types do not implement `encoding.TextMarshaler`: the text is lossy (a class name does not identify the class across modules) and there is no `UnmarshalText` that could rebuild a type without a program.

### SSA

- One `Instr` struct and an op table (`ops`) of name, operand count, auxiliary kind and flags (terminator, may raise, defines a result, printed form). `Print`, `Parse`, `Verify` and the builder are driven by the table.
- Blocks take parameters; there are no phi nodes. The builder uses Braun et al. construction and ends every function with one `finish()` that numbers anonymous names and recomputes `Preds` and `Succs`.
- An instruction that can raise carries an invoke-style exception edge: it ends its block, `Succs[0]` is the normal edge, `Except` is the handler edge. The result exists only on the normal edge, the normal successor has one predecessor, and a handler block starts with `landingpad` and is reached only by exception edges. Rules and codes are in docs/ssa.md.
- `int` operations have Python semantics: there is no `.chk` variant and no bigint type in the IR. The compiler expands the fast and slow path (docs/compiler.md; ADR 0004); `.nochk` arrives with range analysis in P4.
- `Print(w, p, opts...)` and `Parse(r)` are a pair of functions, not `MarshalText`/`UnmarshalText`: they take an `io.Writer` and `io.Reader`, `Parse` resolves forward references over a whole program, and positions are opt-in (`WithPositions`).

## Consequences

- Go readers find the expected names; every difference from `go/types` is a row above and a reason.
- The library surface is checked as Python: stubs are parsed, declared and typed by the code they describe, and a missing builtin is a stub edit, not a Go change.
- `Info` stays small for consumers that ignore specialization, and the builder reads one `Spec` at a time.
- `Check` depends on the order of `WithFS` entries and on the embedded stubs; both are covered by tests.
- The IR text format is a public contract: changing the op table changes goldens, `Parse`, and `Verify` together.

## Alternatives

- `Config` struct with `Importer` and `Error` callback, as `go/types`. Rejected: nothing varies except the file system; the callback only collects.
- `TextMarshaler` on `Type` and on the IR. Rejected: lossy for types, and the IR needs a whole-program reader.
- Builtins as Go tables (names and signatures in Go). Rejected: duplicates `typing` semantics in a second notation and cannot express overloads, protocols or generics without reimplementing the checker's own input.
- `Interface` type for `Protocol`. Rejected: a protocol is a class with an MRO and may be a base class; two representations would need conversions everywhere.
- One struct per op (`*Add`, `*Call`, ...) as `x/tools/go/ssa`. Rejected: Print, Parse and Verify would each need a switch over every struct; a table gives one place to add an op.
- Singleton `Ref` types for classes in the IR. Rejected: classes are nominal and program-wide, so the IR points at `*Class`, and printed names are module-qualified.
