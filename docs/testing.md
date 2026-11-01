# Testing

`make check` is the gate: fmt, vet, lighthouse, `go test -race ./...`, conformance, short fuzz pass.

## Principles

- Table-driven tests using `github.com/stretchr/testify/require`.
- Tests of generated output compare against checked-in goldens; goldens are regenerated, never hand-edited.
- Normal `go test` needs no Python. Only generation (`-update`, `cmd/genexpected`) and the conformance/benchmark baselines need `python3.14` (or `SPRY_PYTHON`).
- CPython 3.14 is the oracle. A divergence is a bug unless tagged.

## Layers

| Layer | Location | Oracle | Needs python3.14 |
|---|---|---|---|
| Unit | `*/..._test.go` | assertions | no |
| AST golden | `ast/testdata/*.py` + `.ast` | `ast.dump` | only to regenerate |
| Syntax-error golden | `parser/testdata/errors/*.py` + `.err` | CPython `SyntaxError` line/col/msg | only to regenerate |
| SSA golden | `ssa/testdata/*.py` + `.ssa` | spry's own printer (reviewed) | no |
| Compiler golden | `compiler/testdata/` | instruction listing for key patterns | no |
| Conformance | `conformance/testdata/<category>/*.py` + `.expected` | CPython stdout/exit code | only to regenerate |
| Fuzz | `scanner`, `parser`, `ssa` | invariants | no |
| CLI golden | `cli/testdata/` | `python3.14` usage output and exit codes | only to regenerate |

## AST Golden Tests

`internal/cpython` runs `python3.14` with a small driver that parses a file and prints:

```python
ast.dump(tree, include_attributes=True, indent=1)
```

`go test ./ast/... -update` regenerates every `.ast` next to its `.py`. The ordinary test parses `.py` with spry, calls `ast.Dump(..., IncludeAttributes, Indent=" ")`, and requires byte equality with `.ast`. Because the file is stored, no Python is needed in CI after generation. A CI job with python3.14 runs `-update` and fails on any diff to detect oracle drift.

The P1 exit corpus also includes CPython `Lib/test` samples; those are fetched by a script at a pinned CPython tag into a gitignored cache and verified against checked-in `.ast` hashes, so the repo does not vendor CPython sources.

## SSA Golden Tests

`ssa/testdata/*.py` is built through `types` and `ssa.Build`, printed in text form (docs/ssa.md), and compared with `.ssa`. Optimizer tests are `.ssa` in → `.ssa` out pairs, using `ssa.Parse` to start from IR. `ssa.Verify` runs on every build and pass output.

## Conformance Corpus

```text
conformance/testdata/<category>/<name>.py
conformance/testdata/<category>/<name>.expected      # stdout of python3.14, then a final line "exit: <code>"
```

Categories mirror the subset: `arith`, `control`, `functions`, `closures`, `classes`, `exceptions`, `generators`, `containers`, `strings`, `match`, `modules`.

The harness runs `spry <name>.py` and compares stdout and exit code with `.expected`. Directives in leading comments:

| Directive | Meaning |
|---|---|
| `# spry-divergence: <reason>` | Known difference; the test checks spry against `.expected` only for non-divergent lines when `# spry-expected: <file>` is also given, otherwise it is skipped with the reason printed |
| `# spry-skip: <phase>` | Not runnable until the named phase (for example `P3`) |
| `# spry-args: <args>` | argv passed after the script |
| `# spry-stdin: <file>` | stdin content |

A hygiene test requires: every `.py` has `.expected`; every divergence has a non-empty reason; no orphan `.expected`; no `spry-skip` for a phase already complete. `cmd/genexpected` regenerates `.expected` with python3.14 and refuses to run if the interpreter is not 3.14.

Before P3, conformance only runs the hygiene test and the parse of every file (AST must not error).

## Fuzz

Go native fuzz targets:

- `FuzzScanner`: arbitrary bytes never panic; INDENT/DEDENT balanced at EOF.
- `FuzzParser`: arbitrary bytes never panic; on success `Dump` output is stable and, where python3.14 is available (opt-in tag), equals CPython's when CPython also accepts the input.
- `FuzzSSA`: `Parse(Print(p))` equals `p` for builder output of fuzz-generated valid programs.

`make fuzz` runs each for a short budget; seeds live in `testdata/fuzz/`.

## CLI Goldens

`cli/testdata/*.golden` hold stdout, stderr and exit code for `python3.14` invocations that spry must match (usage errors, `-V` shape, missing file). Argument-parsing table tests cover option combining, `--`, `-c`/`-m` argv handling, and environment variables. Goldens are recorded with `-update`.

## Coverage

Target 80% per package for logic packages (`scanner`, `parser`, `types`, `ssa`, `optimize`, `compiler`). Generated code and `testdata` are excluded. `make cover` reports it.

## Related

- docs/frontend.md for Dump rules; docs/cli.md for CLI behavior; docs/benchmarks.md for performance checks.
