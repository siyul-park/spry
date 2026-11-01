# CLI

`spry` is a drop-in for `python3.14` at the command line. Package `cli` parses and dispatches (no `os.Exit`, fully testable); `cmd/spry` is a thin `main`. spry-only features use `-X spry.*` so they cannot collide with CPython options (ADR 0007).

```text
spry [option] ... [-c cmd | -m mod | file | -] [arg] ...
```

Implementation: `cli` builds one `*cobra.Command` (github.com/spf13/cobra, flags by pflag) with no subcommands and assembles it from independent components. `Root` never calls cobra's `Execute`: `Execute` always registers the hidden `__complete` command, which would capture a script named `__complete`, and no option disables it. Instead `Root` drives the lifecycle itself, which still uses cobra for flag definitions (`SetInterspersed(false)` stops parsing at the script path or `-`), IO (`SetIn`/`SetOut`/`SetErr`), context (`SetContext`), and the hooks `ParseFlags`, `ValidateArgs`, `PreRunE` (component validation) and `RunE` (dispatch). A script named `__complete`, `__completeNoDesc`, `completion` or `help` is therefore just a script.

## Components

`DefaultComponents()` is the ordered slice the root is built from; adding or removing a mode is a one-line change there, and a root can be built from any subset (`NewRoot(process, cli.Version{}, cli.Command{})`).

| Component | Owns | Notes |
|---|---|---|
| `Help` | `-h`, `-?`, `--help`, `--help-env`, `--help-xoptions`, `--help-all` | First one wins and preempts later flag errors |
| `Version` | `-V`, `--version` | Deferred, so later errors win; `-VV` is verbose |
| `Interpreter` | `-b -B -d -E -i -I -O -P -q -s -S -u -v -W -x`, `--check-hash-based-pycs`, and the `PYTHON*` environment | `-I` implies `-E -P -s`; flags win over the environment; `-E`/`-I` ignore it |
| `XOptions` | `-X` | Validates `spry.dump`, `spry.opt`, `spry.fallback`, `spry.emit` |
| `Command` | `-c` | Terminal flag |
| `Module` | `-m` | Terminal flag |
| `File` | the script path | Prints CPython's open-file errors |
| `Stdin` | `-` or no argument | |

A component implements `Component` (`Register` its flags, `Prepare` its part of `Options`); a mode or action also implements `Handler` (`Applies`, `Run(ctx, env, state)` with injected IO, environment and a lazy working-directory resolver). `Preempter` marks components that act immediately (help); `Terminator` marks flags that end option parsing (`-c`, `-m`). Every handler is unit-tested alone against a root built from just that component.

Custom code is limited to (1) `splitArgs`, the pre-split implementing CPython's rule that everything after `-c <cmd>` / `-m <mod>` goes to `sys.argv` (also keeps `-W=x` as the value `=x`, which pflag would shorten, and rejects every long option CPython does not have), (2) the translation of pflag errors into CPython's message text, (3) the static CPython help text, and (4) the `PYTHON*` environment merge (`caarlos0/env` was considered and rejected: it parses booleans with `strconv.ParseBool`, but CPython treats any non-empty string as true).

## Options

| Option | Behavior |
|---|---|
| `-c cmd` | Run `cmd`; `sys.argv[0] = '-c'`. `-cprint(1)` (attached) works |
| `-m mod` | Run a module as a script; `sys.argv[0]` is the module path. `-m ast` and `-m tokenize` are reserved to match CPython's stdlib CLIs when those modules exist |
| `file` | Run a script file |
| `-` | Read the program from stdin |
| `-V`, `--version` | Print `Python 3.14.0 (spry <version>)`; `-VV` prints extended build info. First token stays `Python` for tools that parse it |
| `-h`, `-?`, `--help` | Usage |
| `--help-env` | Environment variable help |
| `--help-xoptions` | `-X` option help, including `spry.*` |
| `--help-all` | All help |
| `-B` | Do not write `.pyc` (no-op: spry writes none) |
| `-b` | Warn on str/bytes comparison (`-bb`: error) |
| `-d` | Debug flag (accepted; no effect initially) |
| `-E` | Ignore `PYTHON*` environment variables |
| `-I` | Isolated mode: implies `-E -P -s` |
| `-i` | Inspect interactively after running |
| `-O`, `-OO` | Optimize: strip `assert` / also docstrings; `sys.flags.optimize` |
| `-P` | Do not prepend script dir / cwd to `sys.path` |
| `-q` | Quiet interactive banner |
| `-s` | No user site directory |
| `-S` | Do not import `site` |
| `-u` | Unbuffered stdout/stderr |
| `-v` | Verbose imports (`-vv` more) |
| `-W arg` | Warning filter (`action:message:category:module:lineno`); `-Wd`, `-We` etc. |
| `-X opt` | Implementation option (below) |
| `-x` | Skip first source line (non-Unix shebang) |
| `--check-hash-based-pycs mode` | Accepted; `default`, `always`, `never` |
| `--` | End of options (interpreter-level) |

Combining follows CPython: `-OO`, `-Wd`, `-bu`, `-cprint(1)` (the rest of the argument is the option value). Unknown options print `Unknown option: -Z` plus the short usage hint to stderr and exit 2. Missing option values print `Argument expected for the -W option` and exit 2.

Everything after the script, `-c cmd` or `-m mod` goes to `sys.argv`.

## -X Options

CPython options are accepted and stored in `sys._xoptions` (`dev`, `utf8`, `faulthandler`, `importtime`, `warn_default_encoding`, `int_max_str_digits=N`, ...); those with no meaning in spry are no-ops. spry-only:

| Option | Values | Meaning |
|---|---|---|
| `spry.dump` | `tokens`, `ast`, `types`, `ssa`, `asm` (comma list) | Print the stage output to stderr, then continue (or exit with `spry.dump.exit`) |
| `spry.opt` | `0`..`3` | Optimization level for `optimize` and minivm `optimize` (default 2) |
| `spry.fallback` | `warn`, `error` | Report or forbid `Any` fallbacks (docs/typing.md) |
| `spry.emit` | path | Write the minivm program to the path |
| `spry.verify` | (flag) | Run `ssa.Verify` after every optimization pass |

`-X spry.<unknown>` is a usage error (exit 2). All `spry.*` options appear in `--help-xoptions`.

## Environment Variables

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
| `PYTHONVERBOSE`, `PYTHONDEBUG`, `PYTHONDEVMODE`, `PYTHONX`-style `PYTHON_*` options | Accepted as in CPython |
| `PYTHONSTARTUP` | Interactive only |
| `NO_COLOR`, `FORCE_COLOR`, `PYTHON_COLORS` | Tracebacks/help color |
| `SPRY_PYTHON` | Not read by `spry`; used by tooling to locate the python3.14 oracle |
| `SPRY_OPT`, `SPRY_FALLBACK` | Optional environment forms of `-X spry.opt` / `spry.fallback` (the `-X` option wins) |

`-E`/`-I` ignore all `PYTHON*` variables but not `SPRY_*`.

## Exit Codes

| Code | Meaning |
|---|---|
| 0 | Success (`SystemExit(0)`/`None`, normal end) |
| 1 | Uncaught exception (traceback on stderr); also `SystemExit` with other nonzero int / string messages per CPython (string printed to stderr, exit 1); spry-specific: feature not implemented in this phase, or compile error from the subset checker |
| 2 | Usage error, or script/module file cannot be opened: `spry: can't open file '/abs/path.py': [Errno 2] No such file or directory` |
| `n` | `SystemExit(n)` for integer `n` is honored (mod 256) |
| 130/143-style | Termination by signal follows the OS convention (128+signal) |

Compile errors (syntax, subset violations) print a CPython-format `SyntaxError` traceback for syntax errors (`File "x.py", line N` plus caret) and `file:line:col: error TYPnnn: message` for subset diagnostics, then exit 1.

## Identity

- `sys.implementation.name == "spry"`, `sys.version` starts with `3.14.0`, `sys.version_info[:2] == (3, 14)`, `sys.platform` as CPython.
- `-V` output: `Python 3.14.0 (spry <version>)`.

## Phase Behavior

| Phase | Behavior |
|---|---|
| P0 | Parsing of all options, help, version, usage errors, file-not-found are final. Executing code prints `spry: execution is not implemented (planned for P3)` and exits 1 |
| P1 | `-X spry.dump=tokens\|ast` works; `-m ast` reserved |
| P2 | `-X spry.dump=types\|ssa` works; type diagnostics are produced |
| P3 | Execution of files, `-c`, `-`, `-m` of compiled modules |
| P5 | Interactive mode (`-i`, REPL) and stdlib `-m` entry points |

## Testing

`cli/testdata` goldens are recorded from `python3.14` for usage errors, unknown options, missing arguments and missing files; table tests assert the exit code and stderr text. See docs/testing.md.
