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

Execution is not implemented yet: running a program prints `spry: execution not implemented yet (phase P3)` and exits 1.

## Boundary

- cli owns: argv grammar, unit composition, flag to stage-option mapping, help, usage and version text, opening the source and the `can't open file` messages (as in CPython `Modules/main.c`), mapping stage errors to output and exit codes, and the order stages are called in.
- cli does not own: the meaning of grammar, types, optimization or execution, nor the meaning of an option value (for example what `-O` removes). A policy that spans stages (such as import resolution over several modules) belongs to the stage that owns it (for example `types`).

## Options

| Option | Status | Behavior |
|---|---|---|
| `-c cmd` | supported | Run `cmd`; `sys.argv[0] = '-c'`. `-cprint(1)` (attached) works |
| `file` | supported | Run a script file |
| `-` | supported | Read the program from stdin |
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
| `-X opt` | pending | Implementation option (below) |
| `-x` | pending | Skip first source line |
| `--check-hash-based-pycs mode` | pending | `default`, `always`, `never` |

Everything after the script, `-c cmd` or `-m mod` goes to `sys.argv`.

## Adding an Option

1. Add a `*cobra.Command` unit, or a flag on an existing unit, that registers the flag and (if it selects an action) a `RunE`. A unit that only changes settings (`-O`, `-W`, `-E`) has no `RunE`: it contributes its flags and is never selected; the unit that runs reads the value from the flags and maps it to a stage option.
2. Add the unit to `Default()` at its priority.
3. Add a unit test composed from that unit alone, and move its cases from `spryOnlyCases` to `goldenCases` in `cli/cli_test.go`, re-recorded from `python3.14`.
4. Change its row above to `supported`.

## -X Options

Pending. CPython options will be accepted and stored in `sys._xoptions`; spry-only:

| Option | Values | Meaning |
|---|---|---|
| `spry.dump` | `tokens`, `ast`, `types`, `ssa`, `asm` (comma list) | Print the stage output to stderr, then continue (or exit with `spry.dump.exit`) |
| `spry.opt` | `0`..`3` | Optimization level for `optimize` and minivm `optimize` (default 2) |
| `spry.fallback` | `warn`, `error` | Report or forbid `Any` fallbacks (docs/typing.md) |
| `spry.emit` | path | Write the minivm program to the path |
| `spry.verify` | (flag) | Run `ssa.Verify` after every optimization pass |

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
| P1 | `-X spry.dump=tokens\|ast` works; `-m ast` reserved |
| P2 | `-X spry.dump=types\|ssa` works; type diagnostics are produced |
| P3 | Execution of files, `-c`, `-`, `-m` of compiled modules |
| P5 | Interactive mode (`-i`, REPL) and stdlib `-m` entry points |

## Testing

`cli/testdata/golden` holds stdout, stderr and exit code. Cases whose behavior equals `python3.14` (usage errors, help, missing files) are recorded from it and compared live when it is installed; cases that depend on an unsupported option or print spry-only text are recorded from spry. Unit tests compose a root from one unit. See docs/testing.md.
