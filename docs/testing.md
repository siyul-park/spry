# Testing

Tests are the executable specification of a feature. They define the contracts, structure, evidence, and validation required to establish correct behavior.

`coding-patterns.md` owns general code design and style; topic docs own behavior; `AGENTS.md` owns repository workflow and validation gates.

`make check` is the gate: fmt, vet, lighthouse, `go test -race ./...`, conformance, short fuzz pass.

## Contract

Tests are the executable specification of a feature: they should show what the public contract promises, not how the implementation happens to work.

- A test MUST show public usage and promised behavior.
- Test structure MUST serve the contract rather than implementation shape or coverage.
- A test MUST expose the target, input, operation, and expected result in the case.
- A case MUST represent behavior, not a branch or implementation path.
- Tests use `github.com/stretchr/testify/require`.
- CPython 3.14 is the oracle. A divergence is a bug unless tagged.
- Normal `go test` needs no Python and no network. Only generation (`-update`, `cmd/genexpected`, `make corpus-extract`) and the conformance/benchmark baselines need `python3.14` (or `SPRY_PYTHON`).

### Public Boundary

Contract tests should prove behavior through the same public boundary available to callers.

- Feature contract tests MUST use only target-package public symbols and MUST live in `package <target>_test`.
- Wrappers that hide target API usage MUST NOT be added merely for reuse.
- Setup helpers MAY exist only when the specified behavior remains visible.
- Private-symbol testing MUST be resolved at the public boundary rather than by exposing internals solely for tests.

### Readability

A readable test is a small, direct specification whose behavior can be understood without following test infrastructure.

- The target and its behavior MUST remain visible.
- Wrappers, builders, or helpers MUST NOT hide the call or result being specified.
- Tests MUST NOT mix different writing styles or abstraction levels at one level.
- Table data and generation code MUST remain simple enough to read as specification.
- A case MUST contain the behavior it claims to specify.

### Organization

Test structure should provide one obvious owner for each public contract and keep case structure shallow.

| Rule | Requirement |
|---|---|
| Owner | Each public symbol SHOULD have one top-level test function; a missing owner test is a warning, and multiple semantic owner tests are an error. |
| Depth | At most two levels: the test function and its direct cases. |
| Same behavior, many inputs | Use an inline anonymous struct table of named inputs and expected outputs; each entry is one case. |
| Different scenarios | Use one case per scenario, named for the behavior it states. |
| Similar cases | Merge cases that specify the same behavior into one case or one table. |
| Style | A test function uses either table cases or scenario cases, never both. |

- A test SHOULD use one case style per level.
- Assertions outside cases SHOULD describe setup, preconditions, or test-wide invariants, not case behavior.
- Readiness polling MUST NOT outlive teardown or retain resources after the case closes them.
- Polling conditions MUST return readiness and observed results/errors only; assertions belong after readiness is established.

### F.I.R.S.T

F.I.R.S.T. keeps tests reliable evidence rather than intermittent diagnostics.

- Tests MUST be **Fast, Independent, Repeatable, Self-validating, and Timely**.
- Tests MUST NOT depend on other tests, uncontrolled mutable state, manual inspection, or unnecessary setup.

## TDD

TDD separates the required behavior from its implementation by making the smallest missing contract fail before the implementation is generalized.

For each behavior change, the agent MUST:

1. state the contract and invariants;
2. write the narrowest falsifying test;
3. observe the expected failure when practical;
4. implement the smallest owning change;
5. run focused checks, then applicable structural and repository gates.

- Tests MUST cover applicable success, failure, boundaries, ownership/lifecycle, compatibility, parity, and architecture contracts.
- The lowest test layer that proves the contract MUST be used.
- Structure-only tests MUST NOT be added merely to exercise implementation details.

## Evidence

Different test layers prove different facts; evidence is complete only when each required fact has an appropriate proof.

- Coverage proves reachability, not quality or completeness.
- When no red phase is available, coverage SHOULD prove execution without changing production behavior to manufacture failure.
- Generated output MUST be compared against a checked-in golden; goldens are regenerated, never hand-edited.

| Layer | Proves | Location | Oracle | Needs python3.14 |
|---|---|---|---|---|
| Public | exported behavior, errors, lifecycle | `*/..._test.go` | assertions | no |
| Frontend | tokens, AST shape, positions, syntax errors | `token`, `scanner`, `ast`, `parser` tests | assertions and CPython manifests | no |
| Oracle | CPython 3.14 differential behavior | `*/testdata/cpython/` | `python3.14` on extracted inputs | only to regenerate |
| Golden | exact output for a specified input shape | see Golden Layers | see Golden Layers | see Golden Layers |
| Conformance | program stdout and exit code | `conformance/testdata/<category>/` | CPython stdout/exit code | only to regenerate |
| Fuzz | bounded trust-boundary or differential properties | `scanner`, `parser`, `ssa` | invariants | no |
| Integration | public end-to-end behavior | `cli`, `cmd/spry` | assertions | no |

### Golden Layers

| Golden | Location | Oracle | Needs python3.14 |
|---|---|---|---|
| AST | `parser/testdata/ast/*.py` + `.ast` | `ast.dump` | only to regenerate |
| Syntax error | `parser/testdata/errors/*.py` + `.err` | CPython `SyntaxError` line/col/msg | only to regenerate |
| Types | `types/testdata/*.py` + `.types` | spry's own `types.Dump` (reviewed) | no |
| Type text | `types/testdata/cpython/typerepr.jsonl` | `typing._type_repr` | only to regenerate |
| SSA | `ssa/testdata/*.py` + `.ssa`, `ssa/testdata/ir/*.ssa` | spry's own printer (reviewed) | no |
| Stages | `cli/testdata/stages/*.py` + `.types`, `.ssa` | spry's own dumps, `$DIR` normalized | no |
| Compiler | `compiler/testdata/` | instruction listing for key patterns | no |
| CLI | `cli/testdata/golden/` | `python3.14` usage output and exit codes | only to regenerate |

#### AST Golden Tests

`internal/cpython` runs `python3.14` with a small driver that parses a file and prints:

```python
ast.dump(tree, include_attributes=True, indent=1)
```

`go test ./parser/... -update` regenerates every `.ast` next to its `.py` under `parser/testdata/ast/`. The goldens live in `parser` because `ast` cannot import `parser`. The ordinary test parses `.py` with spry, calls `ast.Dump(..., IncludeAttributes, Indent=" ")`, and requires byte equality with `.ast`. Because the file is stored, no Python is needed in CI after generation. A CI job with python3.14 runs `-update` and fails on any diff to detect oracle drift. Syntax-error goldens stay at `parser/testdata/errors/` and are regenerated by the same command.

#### Types Golden Tests

`types/testdata/*.py` is checked and written with `types.Dump`, then compared with `.types`; `go test ./types/... -update` regenerates them. They are reviewed, not oracle-recorded. `typerepr.jsonl` is the exception: `internal/cpython/driver/typerepr.py` records the `typing._type_repr` of a set of annotations, `make corpus-extract` regenerates it, and `TestTypeString` checks `TypeString` and `String` against every row. The compile-stage manifests of the parser corpus (`parser/testdata/cpython/snippets.jsonl`, `syntax.jsonl`) are consumed by `types`: each case that CPython accepts at `ast.parse` and rejects at `compile` must give the same `SyntaxError` from `Check`, and every other case must check without a compile error. Its `types/testdata/cpython/xfail.txt` is empty.

#### SSA Golden Tests

`ssa/testdata/*.py` is built through `types` and `ssa.Build`, printed in text form (docs/ssa.md), and compared with `.ssa`; `go test ./ssa/... -update` regenerates them. Each build is verified and `Parse(Print(p, WithPositions()))` must equal `p`. `ssa/testdata/ir/*.ssa` are hand-written texts for `Print`, `Parse` and `Verify`. Optimizer tests are `.ssa` in → `.ssa` out pairs, using `ssa.Parse` to start from IR. `ssa.Verify` runs on every build and pass output.

#### Stage Goldens

`cli/testdata/stages/*.py` are run through `cli.Run` with `-X spry.dump=types` and `-X spry.dump=ssa`; stderr is compared with `.types` and `.ssa`, with the directory of the file written as `$DIR`. See docs/cli.md.

#### CLI Goldens

`cli/testdata/golden/*.golden` hold stdout, stderr and exit code for `python3.14` invocations that spry must match (usage errors, `-V` shape, missing file). Cases that depend on an option spry does not support yet are recorded from spry and move to CPython-recorded goldens when the option lands. Table tests cover the getopt grammar (option combining, `--`, `-c`/`-m` argv handling) and each command unit alone. Goldens are recorded with `-update`.

## CPython Test Corpus

CPython's own tests are the broadest available specification of the language front end. spry uses them as inputs only: expectations always come from running `python3.14`, never from expected strings inside CPython's test files. The decision is recorded in ADR 0008.

### Pinned Source

The corpus is pinned to CPython 3.14.9. The pinned tag tarball and its sha256 are recorded in `internal/cpython`.

### Hybrid Strategy

| Kind | Examples | Storage |
|---|---|---|
| Small cases | `test_ast/snippets.py` lists; `test_syntax.py` and other doctests; string arguments of syntax-check helper calls in `test_grammar`, `test_syntax`, `test_exceptions`, `test_fstring`, `test_tstring`, `test_patma`, `test_type_params` and similar; `test_tokenize` `check_tokenize` inputs | checked-in JSONL manifests, with PSF attribution in `testdata/cpython/NOTICE` |
| Whole files | stdlib `Lib/**/*.py`, `Lib/test` files, `tokenizedata`, badsyntax files | fetched from the pinned tag tarball into the gitignored `.cache/cpython/3.14.9/` (override with `SPRY_CPYTHON_CACHE`), verified by sha256, skipped when absent |

Normal `go test` therefore needs neither network nor Python: manifests are checked in, and cache-backed tests skip when the cache is absent.

### Classification

Each extracted input is run through `python3.14` once, at extraction time:

- `ast.parse` succeeds: the frontend MUST accept it, and the manifest records the `ast.dump` result.
- `ast.parse` fails: the input is a frontend error case; the manifest records `type`, `msg`, `lineno`, a 1-based code-point `offset`, `end_lineno`, and `end_offset`.
- Failures that only appear in `compile` (not `ast.parse`) are recorded with stage `compile` and are not frontend errors.

### Manifests

Each manifest lives with the package that owns the contract:

| Manifest | Content |
|---|---|
| `token/testdata/cpython/tables.json` | token kinds, names, operator table |
| `scanner/testdata/cpython/tokenize.jsonl` | `check_tokenize` inputs and token streams |
| `scanner/testdata/cpython/encoding.jsonl` | source encoding and BOM cases |
| `scanner/testdata/cpython/unicode.json` | identifier and NFKC cases |
| `ast/testdata/cpython/schema.json` | node classes, field order, field kinds |
| `ast/testdata/cpython/repr.jsonl` | `ast.dump` and repr cases |
| `parser/testdata/cpython/snippets.jsonl` | `test_ast` snippet acceptance and dumps |
| `parser/testdata/cpython/syntax.jsonl` | syntax-check inputs and error positions |
| `parser/testdata/cpython/files.jsonl` | whole-file expectations for cache-backed inputs |

### Known-Failure Ratchet

`<pkg>/testdata/cpython/xfail.txt` lists case IDs that spry is known to fail.

- Every listed ID MUST fail; a listed case that passes fails the test, so entries are removed as fixes land.
- The list only shrinks. New entries are not added to hide regressions.
- A layer exits when its `xfail.txt` is empty.

### Commands

- `make corpus` fetches and verifies the pinned tarball into the cache.
- `make corpus-extract` regenerates the manifests with `python3.14`.
- A CI drift job runs `make corpus-extract` and fails on any diff to the checked-in manifests.

## Conformance Corpus

```text
conformance/testdata/<category>/<name>.py
conformance/testdata/<category>/<name>.expected      # stdout of python3.14, then a final line "exit: <code>"
```

Categories mirror the subset. Present now: `basics` (`arith`, `strings`), `control` (`loops`), `functions` (`closures`). Planned: `classes`, `exceptions`, `generators`, `containers`, `match`, `modules`; their `.expected` files need `python3.14` and arrive with P3.

The harness runs `spry <name>.py` and compares stdout and exit code with `.expected`. Directives in leading comments:

| Directive | Meaning |
|---|---|
| `# spry-divergence: <reason>` | Known difference; the test checks spry against `.expected` only for non-divergent lines when `# spry-expected: <file>` is also given, otherwise it is skipped with the reason printed |
| `# spry-skip: <phase>` | Not runnable until the named phase (for example `P3`) |
| `# spry-args: <args>` | argv passed after the script |
| `# spry-stdin: <file>` | stdin content |

A hygiene test requires: every `.py` has `.expected`; every divergence has a non-empty reason; no orphan `.expected`; no `spry-skip` for a phase already complete. `cmd/genexpected` regenerates `.expected` with python3.14 and refuses to run if the interpreter is not 3.14.

Before P3, conformance runs the hygiene test and, for every file, parses, type checks (`types.Check` with the file's directory as the import root), builds (`ssa.Build`) and verifies it. A type diagnostic or build error fails the case; no directive expects one yet.

## Fuzz

Go native fuzz targets:

- `FuzzScanner` (`scanner`): arbitrary bytes never panic; INDENT/DEDENT balanced at EOF.
- `FuzzParseFile` (`parser`): arbitrary bytes never panic; on success `Dump` output is stable and, where python3.14 is available (opt-in tag), equals CPython's when CPython also accepts the input.
- `FuzzSSA` (`ssa`): arbitrary source goes through parse, `Check` and `Build`; a program `Check` accepts must build and verify, and `Print` then `Parse` then `Print` must be stable and read back to the same program. Seeds are the `testdata/*.py` files and `testdata/fuzz/FuzzSSA`.
- `FuzzParse` (`ssa`): arbitrary text never panics `Parse`; what parses prints and re-parses to the same text.

`make fuzz` runs each for a short budget; seeds live in `testdata/fuzz/`.

## Validation

Validation should start with the smallest useful falsification and expand only as far as the affected contract requires.

- Run the smallest falsifying check first.
- Apply package, race, coverage, architecture, and repository checks when relevant.
- Skip only genuinely inapplicable checks, and state the reason.

### Coverage

Target 80% per package for logic packages (`scanner`, `parser`, `types`, `ssa`, `optimize`, `compiler`). Generated code and `testdata` are excluded. `make cover` reports it.

## Completeness

A complete change has evidence for every contract that the change affects, including cross-layer obligations.

- Exported contracts MUST have an owning test.
- Verification policy (`ssa.Verify`) MUST be covered at its defined boundaries.
- Every frontend behavior change MUST keep the affected `xfail.txt` consistent with the tests.
- Generated output and documentation freshness are repository gates, not test-completeness rules.

## Ownership

Test contracts and structure have one owner and should remain independent of incidental implementation details.

| Concern | Owner |
|---|---|
| Test contracts and structure | `testing.md` |
| General test code style | `coding-patterns.md` |
| CPython extraction and oracle driver | `internal/cpython` |
| Frontend contracts | `docs/frontend.md` and package contract tests |
| SSA verification | `ssa.Verify` and `ssa` tests |
| CLI behavior | `docs/cli.md` and `cli` tests |
| Performance evidence | `docs/benchmarks.md` and benchmarks |

## Related

- `coding-patterns.md` — general code design and style
- `refactoring.md` — structural review
- `frontend.md` — Dump rules and frontend contracts
- `cli.md` — CLI behavior
- `benchmarks.md` — performance evidence
- `adr/0008-cpython-test-corpus.md` — corpus decision
