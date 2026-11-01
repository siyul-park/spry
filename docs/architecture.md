# Architecture

spry compiles Python 3.14 to minivm bytecode through a typed SSA IR.

```text
source → scanner → parser → ast → types → ssa (build) → optimize → compiler → minivm program → interp
```

## Packages

Module: `github.com/siyul-park/spry`.

| Package | Role | Modeled on |
|---|---|---|
| `token` | Token kinds, `Pos`/`File`/`FileSet`, keywords, soft keywords (`match`, `case`, `type`, `_`) | go/token |
| `scanner` | Tokenizer: INDENT/DEDENT, implicit line joining, PEP 701 f-string and PEP 750 t-string tokens | go/scanner, CPython tokenizer |
| `ast` | Node set 1:1 with CPython `ast`; `Walk`/`Inspect`; `Dump` in `ast.dump` format | Python `ast`, go/ast |
| `parser` | Hand-written recursive descent following `Grammar/python.gram` (3.14); error recovery; `ParseFile`/`ParseExpr` | python.gram, go/parser |
| `types` | Scopes and symbols, whole-program inference, specialization, `Any` fallback, diagnostics | go/types, CPython symtable |
| `ssa` | Typed SSA IR, builder from typed AST, verifier, text printer/parser | x/tools/go/ssa |
| `optimize` | Pass manager, `O0`..`O3`, SSA→SSA passes | minivm `optimize` |
| `compiler` | SSA → minivm bytecode (the only backend); then minivm `optimize` + `program.Verify` | n/a |
| `builtins` | Python object model on minivm: int (bigint slow path), str, list, dict, tuple, set, exception hierarchy, builtin functions as host functions; mirrors Python's `builtins` module | Python `builtins` |
| `spry` (root) | Public embedding API and pipeline driver: `spry.Compile(fsys, entry, opts) (*program.Program, error)`, `spry.Run(ctx, ...)`, stage dumps | n/a |
| `cli` | CPython-compatible command line: cobra command driven without `Execute`, assembled from independent components (help, version, interpreter options, `-X`, `-c`, `-m`, file, stdin); no `os.Exit` | CPython `Modules/main.c` |
| `cmd/spry` | Thin `main` calling `cli` | n/a |
| `internal/cpython` | Invokes `python3.14` for golden/conformance generation; tooling only | n/a |

Both `types` packages exist: spry `types` and minivm `types`. Code importing both aliases minivm as `vmtypes`.

## Dependency Rules

Enforced by lighthouse decisions (ADR 0005).

1. `token`, `scanner`, `ast`, `parser` (the frontend) import nothing else in spry. Within the frontend: `scanner`→`token`; `ast`→`token`; `parser`→`token`,`scanner`,`ast`.
2. `types` depends on the frontend only.
3. `ssa` imports `ast` and `types` (builder input) but never minivm.
4. `optimize` imports `ssa` only.
5. `compiler` imports `ssa` and `builtins`.
6. Only `compiler`, `builtins`, the root `spry` package, `cli` and `cmd/spry` import minivm.
7. `internal/cpython` imports nothing from spry.

Consequences (derived): the root `spry` package orchestrates all stages and is the only place where `optimize` and `compiler` meet; `cli` imports `spry` and `builtins`. There is no package named `runtime` or `codegen`.

```text
token ◄─ scanner ◄─ parser ─► ast ─► token
                     ast ◄─ types ◄─ ssa ◄─ optimize
                                      ▲
                builtins ◄─ compiler ─┘
                     ▲         ▲
                     └── spry ◄─ cli ◄─ cmd/spry
```

## Stage Contracts

| Stage | Input | Output | Invariants |
|---|---|---|---|
| scanner | `[]byte`, `*token.File` | token stream | Positions are offsets into the file; INDENT/DEDENT balanced; errors are tokens or diagnostics, never panics |
| parser | token stream | `*ast.Module` (or `Expression`, `Interactive`) | Dump equals CPython `ast.dump` on valid input; invalid input yields a diagnostic list with partial AST |
| types | `ast.Module` set (whole program) | `types.Info`: symbol table, type per expression, specialization set, diagnostics | Every expression has a type (possibly `Any`); every `Any` site carries a reason |
| ssa build | `ast` + `types.Info` | `*ssa.Program` | Passes `ssa.Verify`; no AST references retained except source positions |
| optimize | `*ssa.Program` | `*ssa.Program` | Semantics-preserving; output passes `ssa.Verify` after every pass in debug mode |
| compiler | `*ssa.Program` | minivm `*program.Program` | Passes `program.Verify`; unboxed types stay unboxed |
| builtins | n/a | host functions + Python object layouts registered into a program | Python-visible semantics match CPython for supported features |

Whole-program means the compiler sees all reachable modules before inference. Imports resolve through a `fs.FS` supplied to `spry.Compile`.

## Zero-Cost Rule

- A statically resolved `int`/`float`/`bool` lowers to `i64`/`f64`/`i1` operations with no boxing. `int` keeps a checked fast path unless range analysis proves it unnecessary (ADR 0004).
- Boxed `any` paths and dynamic dispatch are emitted only where inference yields `Any`.
- Builtins machinery (bigint, exceptions, generators) is linked in only when reachable from the entry module.
- The compiler reports every fallback with source position and reason; `-X spry.fallback=warn|error` surfaces or forbids them.

## Diagnostics

One `Diagnostic{Pos, Severity, Code, Message}` type per stage, collected not thrown. Codes are namespaced: `SYN` (syntax), `TYP` (types/subset), `SSA` (internal verifier), `GEN` (compiler). Subset rejections use `TYP` codes listed in docs/subset.md.

## Backend Boundary

minivm's SSA lives in `internal/ssa` and cannot be imported (ADR 0001). spry emits bytecode through `program.Builder` and `types.FunctionBuilder` (minivm), then runs minivm `optimize` and `program.Verify`. spry-side optimization happens at SSA level, where Python types are still known.

## Related

- docs/frontend.md, docs/typing.md, docs/ssa.md, docs/compiler.md
- docs/adr/README.md
