# 0007: CPython-compatible CLI

- Status: Accepted

## Context

Tooling (editors, test runners, build systems, shebangs) invokes `python3` with CPython's options and parses `-V` output. A distinct CLI would block drop-in use and make conformance harnesses special-case spry.

## Decision

`spry` accepts the `python3.14` command line: `spry [option] ... [-c cmd | -m mod | file | -] [arg] ...`, with CPython's option combining, exit codes (0, 1, 2, `SystemExit`) and message formats. `-V` prints `Python 3.14.0 (spry <version>)`; `sys.implementation.name` is `spry`. spry-only features are under `-X spry.*` (`dump`, `opt`, `fallback`, `emit`, `verify`) so they never collide with CPython flags. The CLI is one `*cobra.Command` with no subcommands, assembled from independent components (help, version, interpreter options, `-X`, `-c`, `-m`, file, stdin), each registering its own flags and running with an injected environment. Its lifecycle is driven by hand (`ParseFlags`, `ValidateArgs`, `PreRunE`, `RunE`) instead of `Execute`, because `Execute` always registers a hidden `__complete` command that would capture a script of that name. `SetInterspersed(false)` stops at the script path, `CountP` handles `-O`/`-OO`, and `StringArray` handles repeatable `-W` and `-X`. Custom code is limited to a small argv pre-split for CPython's rule that everything after `-c <cmd>` or `-m <mod>` goes to `sys.argv`, the translation of flag errors into CPython's text, the static help text, and the `PYTHON*` environment merge. Details: docs/cli.md.

## Consequences

- Conformance and benchmark harnesses run `python3.14 X.py` and `spry X.py` with identical argv.
- Usage-error goldens recorded from CPython guard compatibility.
- Some CPython options are accepted as no-ops until supported.

## Alternatives

- Hand-written option parser: rejected as reinvention with a maintenance burden; cobra/pflag cover the grammar with the small custom layer above.
- Cobra subcommands (`spry run`, `spry build`): rejected because they break drop-in compatibility with `python3`.
- Spry-specific top-level flags: rejected; they could collide with CPython options (hence `-X spry.*`).
- Separate `spryc` compiler binary: possible later, but the primary entry stays CPython-shaped.
