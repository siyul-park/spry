# spry

A Python implementation in Go that treats Python as a statically analyzable language.

Python 3.14 source is parsed, type-inferred as a whole program, lowered to a typed SSA IR, optimized, and emitted as [minivm](https://github.com/siyul-park/minivm) bytecode.

## Goals

- **Full CPython compatibility is the end state.** Phase 1 targets a statically analyzable subset; every phase-1 program is also a valid Python 3.14 program with identical observable behavior.
- **Zero-cost abstraction.** A feature costs nothing in programs that do not use it. Statically resolved types lower to unboxed minivm operations; boxed `any` paths exist only where inference yields `Any`, and diagnostics say where and why.
- **Reuse minivm as the only backend.** spry owns its frontend, type system, SSA IR and optimizer, and the Python object model on top of minivm.
- **Drop-in CLI.** `spry` accepts the `python3.14` command line. spry-only features live under `-X spry.*`.
- **CPython, PyPy and gpython are first-class baselines** for conformance and benchmarks.

## Status

| Phase | Scope | Status |
|---|---|---|
| P0 | Bootstrap: docs, tooling, package stubs, CLI argument handling | In progress |
| P1 | Frontend: scanner, parser, AST identical to CPython 3.14 `ast.dump` | Planned |
| P2 | Whole-program type inference and SSA construction | Planned |
| P3 | minivm backend: end-to-end `spry file.py` | Planned |
| P4 | SSA optimizer | Planned |
| P5 | Widen the subset toward full compatibility | Planned |

Exit criteria per phase are in [docs/roadmap.md](docs/roadmap.md).

## Pipeline

```text
source (.py, Python 3.14)
  │  token, scanner        tokens (INDENT/DEDENT, f-/t-strings)
  ▼
  parser ──► ast           1:1 with CPython `ast`
  │
  ▼  types                 scopes, whole-program inference, specialization
  │
  ▼  ssa (build)           typed SSA IR, exception edges, generators
  │
  ▼  optimize              SCCP, DCE, GVN, inline, devirtualize, unbox, range analysis
  │
  ▼  compiler              SSA → minivm bytecode, then minivm optimize + Verify
  │
  ▼  minivm interp         with `builtins` host functions (bigint, str, list, dict, ...)
```

The root `spry` package drives the pipeline (`spry.Compile`, `spry.Run`); `cli` and `cmd/spry` expose it.

## Quickstart

```sh
go install ./cmd/spry
spry -V        # Python 3.14.0 (spry <version>)
spry --help
```

Until P3, execution paths report which phase implements them and exit with status 1. The CLI (cobra and pflag, assembled from independent components, with CPython-compatible formatting) already handles options, `-V`, help output and usage errors as CPython 3.14 does.

Development: `make check` runs formatting, vet, lighthouse, tests, conformance and fuzz smoke checks. See [AGENTS.md](AGENTS.md).

## Documentation

| Document | Use it for |
|---|---|
| [docs/architecture.md](docs/architecture.md) | Pipeline, packages, dependency rules, stage contracts |
| [docs/roadmap.md](docs/roadmap.md) | Phases P0-P5 and exit criteria |
| [docs/subset.md](docs/subset.md) | Phase-1 language subset: supported, restricted, rejected |
| [docs/frontend.md](docs/frontend.md) | Tokens, AST, positions, `ast.Dump` |
| [docs/typing.md](docs/typing.md) | Type lattice and whole-program inference |
| [docs/ssa.md](docs/ssa.md) | SSA IR specification |
| [docs/compiler.md](docs/compiler.md) | SSA → minivm mapping |
| [docs/cli.md](docs/cli.md) | CPython-compatible command line |
| [docs/testing.md](docs/testing.md) | Test strategy and goldens |
| [docs/benchmarks.md](docs/benchmarks.md) | Benchmark methodology and results |
| [docs/adr/](docs/adr/README.md) | Architecture decision records |

## Baselines

| Implementation | Role |
|---|---|
| CPython 3.14 (`python3.14` or `SPRY_PYTHON`) | Semantic oracle for goldens and conformance; performance baseline |
| PyPy 3 (`pypy3`) | Performance baseline (JIT) |
| gpython | Performance baseline (Go interpreter) |

## License

See LICENSE.
