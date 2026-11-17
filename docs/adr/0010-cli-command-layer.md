# 0010: CLI command layer

- Status: Accepted

## Context

ADR 0007 made the CLI CPython-compatible and built it from cobra and pflag. The implementation grew custom code to make pflag behave like CPython: a pre-split of argv for `-c` and `-m`, a translation of pflag error text, and a component interface set with shared state. It also mixed in domain code (`PYTHON*` environment merge, `-I` implication, `-X spry.*` validation) although no stage can execute anything yet. A root `spry` package was planned as a pipeline driver, with nothing to own.

## Decision

1. Compose cobra units, keep pflag for values, port only the parser. Each action a command line can select (help, version, `-c`, file, stdin) is one `*cobra.Command` that owns its pflag flags and is added to a `Root`. `Root` keeps the composition order as selection priority. Flag values are recorded with `Flag.Value.Set`. pflag's parser and cobra's `Execute` are not used; `getopt.go` ports CPython `Python/getopt.c`.
2. The CLI is the command layer only. The root `spry` driver package is removed. `cli` calls the stage packages in order and maps results to CPython output and exit codes; it owns argv grammar, unit composition, flag to stage-option mapping, help, usage and version text, opening the source, and stage error to exit code mapping. Language and option semantics, and policies that span stages (for example import resolution over modules), belong to the stage packages.
3. The grammar is complete, the meaning is incremental. `getopt` knows the whole CPython 3.14 option grammar. An option in the grammar that no composed unit registers prints `spry: option -O is not supported yet` and exits 2. Adding an option means one unit (or one flag), one line in `Default()`, a unit test and a row in docs/cli.md.

## Consequences

- No argv pre-split, no regular expressions over library error text, no shared parse state; the parser is one file that can be compared with the C source.
- A command line spry cannot honor fails loudly instead of being accepted and ignored.
- `-h -z` prints help and `-z -h` is an error, as in CPython; an unsupported option does not hide a grammar error.
- Unsupported-option cases are recorded from spry and move to CPython-recorded goldens when their unit lands.
- Environment variables, `-X`, `-m` and the interpreter options return later as units.
- Supersedes the implementation parts of ADR 0007 (components, argv pre-split, pflag error translation, `Execute`-free lifecycle hooks); its compatibility goals stand.

## Alternatives

- Keep pflag's parser: needed the pre-split and error translation, the cause of the complexity.
- Own `Command` type: duplicates cobra.
- A separate driver or `interp` package: a middle layer with no rule to own.
- Let `compiler` own the pipeline: violates the compiler import rules (docs/architecture.md).
- Treat unknown options as unknown, or accept and ignore them: the first misreports valid CPython options, the second hides missing behavior.
