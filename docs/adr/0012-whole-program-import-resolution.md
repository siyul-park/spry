# 0012: Whole-program import resolution

- Status: Accepted

## Context

spry compiles whole programs (ADR 0002): every module the entry module imports must be known before inference. CPython resolves imports at run time through `sys.path`, finders and `sys.modules`; a missing module is a run-time `ModuleNotFoundError`, and code can catch it. Standard library modules that spry does not ship cannot be compiled, and silently running a program that imports one would change its meaning. Import resolution spans stages (the loader reads files, the checker types them, the builder lowers import sites), so one stage must own it (docs/cli.md, Boundary). That stage is `types`.

## Decision

`types.Check` owns import resolution. The file system is an option, `WithFS(fsys fs.FS, dir string)`, which adds a `sys.path` entry: modules are read from `fsys`, and `dir` is the absolute directory it stands for in `__file__` and in positions. `cli` passes `os.DirFS(dir)` for the directory of the script, and for `-c` and stdin the working directory of the process.

- **Order.** Entries are searched in the order they are given, which is `sys.path` order. For a dotted name each component is imported, parent first, and a parent must be a package. Within an entry a package directory `a/b/__init__.py` is tried before the module `a/b.py`.
- **Search order for one name.** `builtins` and `sys` are always the shipped modules and cannot be shadowed. Any other name is looked up in the `WithFS` entries first and then in the modules shipped with spry, so a local file shadows a shipped module as it does in CPython with the script directory first.
- **Shipped modules.** `builtins`, `typing`, `math`, `sys`, `__future__`, `collections` and `collections.abc`, embedded from `types/lib` (ADR 0011).
- **Standard library list.** `types/gen_stdlib.go` generates `types/stdlib.go` from `sys.stdlib_module_names` of CPython 3.14 (`go run types/gen_stdlib.go` from `types/`, needs `python3.14`). A name whose top-level component is in that list, is not found locally and is not shipped, is an unshipped standard library module.
- **Outcome per import site.** `Info.Import(site)` returns an `Import` for each `*ast.Alias` of an `import` statement and each `*ast.ImportFrom`: `ImportOK` with the `Module`, `ImportNotFound`, `ImportSyntax` (the module has a compile-stage `SyntaxError`, in `Err`), `ImportRelative` (a relative import where there is no parent package, which includes `__main__`) and `ImportUnshipped`.
- **Missing and broken modules are not errors of the program.** A module that is not found, whose file has a `SyntaxError`, or a relative import outside a package does not make `Check` fail. The checker treats the rest of the block as unreachable, and the builder lowers the import site to the `raise` Python would execute: `ModuleNotFoundError` (`SyntaxError` for a module that does not parse). `try: import x except ImportError:` therefore works.
- **Unshipped standard library modules are errors.** An import of one is `TYP121` unless the import is guarded: inside a `try` whose handlers catch `ImportError`, `ModuleNotFoundError`, `Exception`, `BaseException` or are bare. A guarded import is lowered like a missing module. `importlib` is always `TYP121`, and `inspect` and `gc` are always `TYP130`, guarded or not (docs/subset.md).
- **`from m import *`** needs a literal `__all__` list or tuple in `m`, else `TYP120`.
- **Namespace packages** (a directory without `__init__.py`) are deferred; such a directory is not found.
- **Order of modules.** Modules are loaded and typed in the deterministic depth-first post-order Python would finish importing them, main last. `Module.Imports` and `types.Dump` follow it; an import cycle makes the names of its modules possibly unbound (`Info.Defined`).

## Consequences

- The result of `Check` depends only on the program text and the `fs.FS` entries; tests use `fstest.MapFS` and `os.DirFS` of a temp directory.
- A program that uses a missing module only on a path that never runs still compiles, as in CPython.
- Adding a shipped module is adding a stub to `types/lib`; the stdlib list needs regeneration only for a new CPython version.
- `cli` does not know the search rules, so `-m` and `PYTHONPATH` later add entries without a stage change.

## Alternatives

- Resolve imports in `cli` and pass parsed files to `types`. Rejected: the policy spans stages and belongs to the stage that interprets it (docs/cli.md, Boundary).
- Treat a missing module as a compile error. Rejected: it changes behavior of programs that catch `ImportError` or never reach the import.
- Treat every unshipped standard library module as a run-time error. Rejected: the program could not be compiled to the semantics its author wrote; `TYP121` reports the gap at compile time unless the program already handles it.
- Accept namespace packages now. Deferred: they need directory listing semantics across several entries; no corpus program needs them yet.
