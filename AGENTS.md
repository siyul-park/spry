# AGENTS.md

## Scope and Precedence

This file is the repo contract for `github.com/siyul-park/spry`. Precedence:

1. System, developer, and user instructions.
2. This file.

Keywords MUST/SHOULD/MAY are normative.

Delegation: agents SHOULD delegate broad investigation and multi-file or large code changes to suitable sub-agents when the environment provides them, while the coordinating agent stays responsible for task framing, integration, validation and accurate reporting.

## Setup (fresh clone)

- Go: install the version in `.go-version`.
- CPython oracle: Python 3.14 as `python3.14` (for example `uv python install 3.14`), or point `SPRY_PYTHON` at it. Needed only to regenerate goldens and run conformance/benchmark baselines.
- Lighthouse: install from https://github.com/siyul-park/lighthouse (see its README).
- Optional benchmark baselines: `pypy3`, gpython (https://github.com/go-python/gpython).

## Workflow

1. Read the doc named in the Task Router row for your task before touching code.
2. Design changes land in `docs/` (and an ADR if a decision changes) in the same change as the code.
3. Write the failing test first, then the implementation.
4. Run the package test command, then `lighthouse check --changed`, then `make check` before reporting success.
5. Report only checks that actually passed. Say what was not run.

## Task Router

| Task | Read | Package | Test command |
|---|---|---|---|
| Token kinds, positions, FileSet | docs/frontend.md | `token` | `go test ./token/...` |
| Tokenizer, indentation, f-/t-strings | docs/frontend.md | `scanner` | `go test ./scanner/...` |
| AST nodes, Walk, Dump | docs/frontend.md | `ast` | `go test ./ast/...` |
| Parsing, error recovery | docs/frontend.md | `parser` | `go test ./parser/...` |
| Regenerate AST goldens | docs/testing.md | `internal/cpython`, `ast` | `go test ./ast/... -update` (needs python3.14) |
| Scopes, inference, specialization | docs/typing.md, docs/subset.md | `types` | `go test ./types/...` |
| SSA IR, builder, verifier, printer | docs/ssa.md | `ssa` | `go test ./ssa/...` |
| Optimization passes | docs/ssa.md, docs/architecture.md | `optimize` | `go test ./optimize/...` |
| Bytecode emission | docs/compiler.md | `compiler` | `go test ./compiler/...` |
| Python object model, builtins | docs/compiler.md, docs/subset.md | `builtins` | `go test ./builtins/...` |
| Driver API (`Compile`, `Run`), stage dumps | docs/architecture.md | `spry` (root) | `go test .` |
| Command line | docs/cli.md | `cli`, `cmd/spry` | `go test ./cli/... ./cmd/...` |
| Conformance corpus | docs/testing.md | `conformance` | `make conformance` |
| Benchmarks | docs/benchmarks.md | `benchmarks` | `make bench` |
| New or changed decision | docs/adr/README.md | n/a | `lighthouse decision test` |

## Dependency Rules

Enforced by lighthouse decisions; see docs/architecture.md.

- `token`, `scanner`, `ast`, `parser` (frontend) import nothing else in spry besides each other (`scanner`→`token`; `ast`→`token`; `parser`→`token`,`scanner`,`ast`).
- `types` depends on the frontend only.
- `ssa` imports `ast` and `types` (builder input) and never minivm.
- `optimize` imports `ssa` only.
- `compiler` imports `ssa` and `builtins`.
- Only `compiler`, `builtins`, the root `spry` package, `cli` and `cmd/spry` import minivm.
- Code importing both spry `types` and minivm `types` aliases minivm as `vmtypes`.
- `internal/cpython` is a test/tooling helper and imports nothing from spry.

## Verification Gate

`make check` runs fmt, vet, lighthouse, `go test -race ./...`, conformance, and a short fuzz pass. It MUST be green before a change is reported done. Use `lighthouse check --changed` while iterating.

## Conventions

- Tests are table-driven and use `github.com/stretchr/testify/require`.
- Files stay under 800 lines; split by responsibility, not by arbitrary size.
- No speculative abstractions: add an interface or option when a second user exists.
- Prefer well-maintained existing libraries over reimplementation; justify any hand-rolled replacement (for example in an ADR).
- No `init()` registration magic; wire dependencies explicitly.
- Public API gets doc comments; each package has a `doc.go` stating role, inputs/outputs, allowed imports.
- Goldens (`.ast`, `.ssa`, `.expected`) are generated, never hand-edited; regenerate with `-update` or `cmd/genexpected`.
- Commits: `<type>: <description>` (feat, fix, refactor, docs, test, chore, perf, ci).
- CLI behavior mirrors `python3.14`; deviations need a `# spry-divergence:` note or an ADR.
