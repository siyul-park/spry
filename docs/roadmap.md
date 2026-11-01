# Roadmap

Each phase has exit criteria that are mechanically checkable by `make check` or a named command.

| Phase | Theme | Status |
|---|---|---|
| P0 | Bootstrap | In progress |
| P1 | Frontend identical to CPython 3.14 | Planned |
| P2 | Types and SSA build | Planned |
| P3 | minivm backend, end-to-end execution | Planned |
| P4 | Optimizer | Planned |
| P5 | Toward full compatibility | Planned |

## P0 Bootstrap

Scope: design docs, ADRs, tooling (Makefile, CI, lighthouse), package stubs with `doc.go`, CPython-compatible CLI argument handling, conformance and benchmark harness skeletons.

Exit criteria:

- `make check` is green.
- `spry -V`, `--help`, `--help-xoptions` work; usage errors and exit codes match recorded `python3.14` goldens.
- `spry nonexist.py` matches CPython's message and exits 2; `spry -c 'print(1)'` exits 1 naming the missing phase.
- Lighthouse decisions encode the dependency rules and pass `lighthouse decision test`.
- `benchmarks` runs the cpython runner on one fixture; unavailable runners are reported as skipped.

## P1 Syntax

Scope: `token`, `scanner`, `ast`, `parser`, `ast.Dump`; full Python 3.14 grammar including match statements, PEP 695 type parameters and `type` statements, PEP 701 f-strings, PEP 750 t-strings, exception groups (`except*`), `async`, walrus, positional-only parameters, decorators.

Exit criteria:

- `ast.Dump` of every file in the corpus equals CPython 3.14 `ast.dump(tree, include_attributes=True, indent=1)`. Corpus: repo testdata, the conformance corpus, and CPython `Lib/test` samples (valid files; files that CPython rejects must also be rejected with a `SYN` diagnostic at the same line/column).
- Scanner/parser fuzz targets run clean for the smoke budget; no panics on arbitrary bytes.
- Positions (`lineno`, `col_offset`, `end_lineno`, `end_col_offset`) match CPython on the full corpus.

## P2 Types and SSA

Scope: `types` (scopes, whole-program inference, specialization, diagnostics), `ssa` (IR, builder, verifier, printer/parser).

Exit criteria:

- All of the phase-1 subset (docs/subset.md) infers or reports a `TYP` diagnostic; every rejected construct has a diagnostic test.
- SSA goldens exist for each supported statement/expression category; `ssa.Verify` passes on all builder output.
- Text IR round-trips: `Parse(Print(p))` equals `p`.
- `-X spry.dump=types|ssa` produce stable output.

## P3 Backend

Scope: `compiler`, `builtins`, root `spry` (`Compile`, `Run`); CLI executes files.

Exit criteria:

- `spry file.py` runs the conformance corpus: stdout and exit code equal CPython's except for cases tagged `# spry-divergence:`.
- All emitted programs pass `program.Verify`.
- Uncaught exceptions print a CPython-format traceback to stderr and exit 1; `SystemExit` codes are honored.
- Benchmark fixtures run on the `spry` runner and match expected output.

## P4 Optimize

Scope: `optimize` passes (SCCP, DCE, GVN/CSE, inline, devirtualize, unbox/escape, int range analysis) and level wiring (`-X spry.opt`).

Exit criteria:

- Every pass has unit tests and SSA goldens; the verifier runs after each pass in tests.
- Conformance results are identical at O0 and O3.
- On the benchmark suite, spry at O3 is not slower than CPython on any fixture, and the report publishes the geometric mean against CPython, PyPy and gpython. (Targets are recorded in the results; regressions fail the benchmark check once a baseline is committed.)
- Checked-int guards are elided where range analysis proves bounds (asserted by SSA goldens).

## P5 Compatibility Widening

Scope: move constructs from Restricted/Rejected/Planned (docs/subset.md) into Supported, in order of corpus coverage: dynamic attribute access, `eval`/`exec` through an embedded interpreter tier, metaclasses, `__getattr__` hooks, C-extension-free stdlib, `-m` modules (`ast`, `tokenize`), asyncio.

Exit criteria (per feature, not per phase):

- docs/subset.md row updated; `TYP` rejection removed; conformance cases added.
- The zero-cost rule holds: programs not using the feature produce identical bytecode to before.
- An ADR records any semantic compromise.

Phase P5 completes when the CPython `Lib/test` subset chosen in an ADR passes with no `spry-divergence` tags.
