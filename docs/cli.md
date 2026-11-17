# CLI

`spry` is a drop-in for `python3.14` at the command line. Package `cli` is the command layer (no `os.Exit`, fully testable); `cmd/spry` is a thin `main`. spry-only features will use `-X spry.*` so they cannot collide with CPython options (ADR 0007, ADR 0010).

```text
spry [option] ... [-c cmd | -m mod | file | -] [arg] ...
```

## Structure

Each action a python3.14 command line can select is one `*cobra.Command` unit that owns its pflag flags. `cli.New(units...)` composes them under a `Root` with `AddCommand`; `cli.Default()` is the supported command line, one line per unit.

| Unit | Selected by | Behavior |
|---|---|---|
| `Help()` | `-h`, `-?`, `--help` | Immediate: runs the moment the flag is met |
| `Version()` | `-V`, `--version` | `-VV` prints build details |
| `Command()` | `-c cmd` | Everything after the value is `sys.argv[1:]` |
| `File()` | first operand other than `-` | Prints CPython's open-file errors |
| `Stdin()` | no operand, or `-` | |
| `XOptions()` | `-X opt` | Selects nothing; the program units read its values (see -X Options) |

| Need | Library use |
|---|---|
| Unit and composition | `*cobra.Command`, `root.AddCommand(unit)`; IO and context are inherited from the parent. Selection priority is the composition order kept by `Root` (cobra sorts subcommands by name) |
| Flags a unit owns | `unit.Flags()` (pflag): Count (`-V`, later `-O`, `-v`, `-b`), String (`-c`), StringArray (later `-W`, `-X`) |
| Recording values | The parser passes each `(flag, value)` to `Flag.Value.Set` (`"+1"` for Count). pflag's parser is not called |
| Is an option supported | `ShorthandLookup`/`Lookup` on the flags of all composed units |
| Operand units | `Command.Args` (PositionalArgs) validator; `nil` selects the unit |
| Running | `unit.RunE(unit, args)`; `args` are the operands after the options |
| Immediate units | `Annotations["spry.immediate"]` |
| Written here | `getopt.go` (port of CPython `Python/getopt.c`) and the CPython help text |

`Root` never calls cobra's `Execute`: `Execute` always registers the hidden `__complete` command, which would capture a script of that name, and no option disables it. A script named `__complete`, `completion` or `help` is therefore just a script.

## Option Grammar

`getopt.go` accepts the full CPython 3.14 grammar: short options `bBc:dEhiIm:OPqRsStuvVW:xX:?`, and long options `help`, `version`, `help-env`, `help-xoptions`, `help-all`, `check-hash-based-pycs` (needs a value). Long options match exactly (no abbreviations, no `--name=value`). `-c` and `-m` take the rest of the command line, `-W=x` has the value `=x`, `--` ends options and `-` is an operand. Errors use CPython's text and exit 2: `Unknown option: -z`, `Argument expected for the -W option`, `Argument expected for the --check-hash-based-pycs options`, followed by the usage line and `Try \`spry -h' for more information.`

## Run Order

1. Read options. A grammar error prints the message and usage, exit 2. A registered flag is recorded and marks its unit selected; an immediate unit (help) runs at once, so `-h -z` prints help while `-z -h` is an error. An option in the grammar that no unit registers is held as unsupported.
2. If an option was unsupported: `spry: option -O is not supported yet` (long: `spry: option --help-env is not supported yet`), exit 2, no usage line.
3. Run the first selected flag unit in composition order (`-V -c x` prints the version, as CPython does). Otherwise run the first operand unit whose validator accepts the operands.
4. `RunE` errors become exit codes: an `exitError` keeps its code, any other error prints `spry: <error>` and exits 1.

A program unit (`-c`, file, stdin) reads its source, then runs the front end on it in exec mode with the filename CPython shows: `<string>` for `-c`, `<stdin>` for stdin, and for a file the path made absolute against the working directory (CPython prints it absolute too). A failure is reported as CPython prints a `SyntaxError` (below) and exits 1. When the source parses, the `-X spry.dump` output is printed and execution would follow; it is not implemented yet, so the program prints `spry: execution not implemented yet (phase P3)` and exits 1.

## Syntax Errors

The first error of the parse is printed to stderr in the layout of `TracebackException._format_syntax_error` (`Lib/traceback.py`, 3.14), with no `spry:` prefix:

```text
  File "/path/x.py", line 1
    x = "é" + (
              ^
SyntaxError: '(' was never closed
```

- The source line is the line of the error as the tokenizer saw it (decoded, line breaks translated to `\n`), with leading spaces and form feeds removed. Tabs are kept, and the caret line copies each tab of the text before the caret so the caret stays aligned.
- Offsets are code points. They are converted from `scanner.Error` byte columns: `offset` is `Pos.Column` and `end_offset` is `End.Column-1` (docs/frontend.md). The caret run is `end_offset - offset` wide, at least 1. An error that ends on another line carets to the end of the line.
- The exception name is `TabError`, `IndentationError` or `SyntaxError` by the category of the error. An empty message prints `<no detail available>`.
- A position without a source line (line 0, or a line past the end of the text) prints the `File` line and the message only: `File "x.py", line 0` for `unknown encoding: nope`.
- Source that cannot be decoded is shown with U+FFFD for each bad byte.

The wording of a message belongs to the stage that produced it (docs/frontend.md); the layout is owned here.

## Boundary

- cli owns: argv grammar, unit composition, flag to stage-option mapping, help, usage and version text, opening the source and the `can't open file` messages (as in CPython `Modules/main.c`), mapping stage errors to output and exit codes, and the order stages are called in.
- cli does not own: the meaning of grammar, types, optimization or execution, nor the meaning of an option value (for example what `-O` removes). A policy that spans stages (such as import resolution over several modules) belongs to the stage that owns it (for example `types`).

## Options

| Option | Status | Behavior |
|---|---|---|
| `-c cmd` | supported | Run `cmd`; `sys.argv[0] = '-c'`. `-cprint(1)` (attached) works. Parsed; execution pending (P3) |
| `file` | supported | Run a script file. Parsed; execution pending (P3) |
| `-` | supported | Read the program from stdin. Parsed; execution pending (P3) |
| `-V`, `--version` | supported | Print `Python 3.14.0 (spry <version>)`; `-VV` prints extended build info. First token stays `Python` for tools that parse it |
| `-h`, `-?`, `--help` | supported | Usage |
| `--` | supported | End of options |
| `-m mod` | pending | Run a module as a script; `-m ast` and `-m tokenize` are reserved for CPython's stdlib CLIs |
| `--help-env` | pending | Environment variable help |
| `--help-xoptions` | pending | `-X` option help, including `spry.*` |
| `--help-all` | pending | All help |
| `-B` | pending | Do not write `.pyc` (no-op: spry writes none) |
| `-b` | pending | Warn on str/bytes comparison (`-bb`: error) |
| `-d` | pending | Debug flag (accepted; no effect initially) |
| `-E` | pending | Ignore `PYTHON*` environment variables |
| `-I` | pending | Isolated mode: implies `-E -P -s` |
| `-i` | pending | Inspect interactively after running |
| `-O`, `-OO` | pending | Optimize: strip `assert` / also docstrings |
| `-P` | pending | Do not prepend script dir / cwd to `sys.path` |
| `-q` | pending | Quiet interactive banner |
| `-R` | pending | Hash randomization (accepted; spry has no hash seed to set) |
| `-s` | pending | No user site directory |
| `-S` | pending | Do not import `site` |
| `-t` | pending | Accepted by CPython 3.14 as a no-op (obsolete tab check) |
| `-u` | pending | Unbuffered stdout/stderr |
| `-v` | pending | Verbose imports (`-vv` more) |
| `-W arg` | pending | Warning filter (`action:message:category:module:lineno`) |
| `-X opt` | partial | Implementation option (below). Only `spry.dump=tokens\|ast` and `spry.dump.exit` are accepted; any other `-X` value prints `spry: option -X is not supported yet` and exits 2 |
| `-x` | pending | Skip first source line |
| `--check-hash-based-pycs mode` | pending | `default`, `always`, `never` |

Everything after the script, `-c cmd` or `-m mod` goes to `sys.argv`.

## Adding an Option

1. Add a `*cobra.Command` unit, or a flag on an existing unit, that registers the flag and (if it selects an action) a `RunE`. A unit that only changes settings (`-O`, `-W`, `-E`) has no `RunE`: it contributes its flags and is never selected; the unit that runs reads the value from the flags and maps it to a stage option.
2. Add the unit to `Default()` at its priority.
3. Add a unit test composed from that unit alone, and move its cases from `spryOnlyCases` to `goldenCases` in `cli/cli_test.go`, re-recorded from `python3.14`.
4. Change its row above to `supported`.

## -X Options

CPython options will be accepted and stored in `sys._xoptions` later. Today `-X` accepts the spry-only values marked "supported"; any other value, including a stage that is not implemented yet, is reported as unsupported like `-O` (the option error is held back, so `-X dev -h` prints help). `-X` is repeatable.

| Option | Values | Status | Meaning |
|---|---|---|---|
| `spry.dump` | `tokens`, `ast` (comma list); later `types`, `ssa`, `asm` | `tokens`, `ast` supported | Print the stage output to stderr, then continue. The last `spry.dump` wins; stages print in pipeline order (tokens, then ast) whatever the order in the list |
| `spry.dump.exit` | (flag) | supported | After the dumps, exit 0 instead of continuing to execution |
| `spry.opt` | `0`..`3` | pending | Optimization level for `optimize` and minivm `optimize` (default 2) |
| `spry.fallback` | `warn`, `error` | pending | Report or forbid `Any` fallbacks (docs/typing.md) |
| `spry.emit` | path | pending | Write the minivm program to the path |
| `spry.verify` | (flag) | pending | Run `ssa.Verify` after every optimization pass |

Dump formats follow the CPython command line tools so that the oracle is `python3.14` itself:

- `tokens` is the output of `python3.14 -m tokenize < file` (the stdin form, so no `ENCODING` row): one line per token, `start-end:` padded to 20 columns, the tokenize type name (`NAME`, `OP`, `NEWLINE`, `NL`, `COMMENT`, `INDENT`, ...) padded to 15 and the `repr` of the text padded to 15, with 1-based lines and 0-based code point columns. The scanner runs with `WithExtraTokens` on the decoded text, line breaks untranslated. A scanner error ends the dump; the syntax error follows.
- `ast` is the output of `python3.14 -m ast file`: `ast.Dump` with a 3-space indent and no attributes, plus a newline. It is printed only when the parse succeeds.

Goldens for both are in `cli/testdata/dump` (`*.tokens`, `*.ast`), recorded from those commands (`go test ./cli -run TestXOptions -update`) and compared live when `python3.14` is installed.

## Environment Variables

Pending, together with `-E` and `-I`.

| Variable | Handling |
|---|---|
| `PYTHONPATH` | Module search path additions |
| `PYTHONSAFEPATH` | Same as `-P` |
| `PYTHONOPTIMIZE` | Same as `-O` (`=2` for `-OO`) |
| `PYTHONWARNINGS` | Comma-separated warning filters |
| `PYTHONUNBUFFERED` | Same as `-u` |
| `PYTHONDONTWRITEBYTECODE` | Same as `-B` |
| `PYTHONIOENCODING` | `encoding[:errors]` for stdio |
| `PYTHONUTF8` | UTF-8 mode |
| `PYTHONHASHSEED` | Hash randomization seed (`random` or integer) |
| `PYTHONINSPECT` | Same as `-i` |
| `PYTHONNOUSERSITE` | Same as `-s` |
| `SPRY_PYTHON` | Not read by `spry`; used by tooling to locate the python3.14 oracle |

## Exit Codes

| Code | Meaning |
|---|---|
| 0 | Success (`SystemExit(0)`/`None`, normal end) |
| 1 | Uncaught exception; spry-specific: feature not implemented in this phase, or compile error from the subset checker |
| 2 | Usage error, unsupported option, or script file cannot be opened: `spry: can't open file '/abs/path.py': [Errno 2] No such file or directory` |
| `n` | `SystemExit(n)` for integer `n` is honored (mod 256) |
| 130/143-style | Termination by signal follows the OS convention (128+signal) |

## Identity

- `sys.implementation.name == "spry"`, `sys.version` starts with `3.14.0`, `sys.version_info[:2] == (3, 14)`, `sys.platform` as CPython.
- `-V` output: `Python 3.14.0 (spry <version>)`.

## Phase Behavior

| Phase | Behavior |
|---|---|
| P0 | Grammar, help, version, usage errors, file-not-found are final; options other than the supported ones report "not supported yet" |
| P1 | `-X spry.dump=tokens\|ast` works (done), with `spry.dump.exit`; files, `-c` and stdin are parsed and report `SyntaxError` as CPython does (done); `-m ast` reserved |
| P2 | `-X spry.dump=types\|ssa` works; type diagnostics are produced |
| P3 | Execution of files, `-c`, `-`, `-m` of compiled modules |
| P5 | Interactive mode (`-i`, REPL) and stdlib `-m` entry points |

## Testing

`cli/testdata/golden` holds stdout, stderr and exit code. Cases whose behavior equals `python3.14` (usage errors, help, missing files) are recorded from it and compared live when it is installed; cases that depend on an unsupported option or print spry-only text are recorded from spry. `syntax-*` cases (files, `-c`, stdin with a syntax error from the scanner) run both in a work directory and are compared live with `python3.14`; their messages come from the scanner, so they do not depend on parser wording. Unit tests compose a root from one unit. See docs/testing.md.
