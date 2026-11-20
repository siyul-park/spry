# Phase-1 Language Subset

The phase-1 subset is a set of Python 3.14 programs that whole-program inference can type and that lower to unboxed minivm operations. The invariant: **every accepted program is a valid Python 3.14 program with the same observable behavior under CPython.** spry never accepts a program with different semantics.

The parser accepts all of Python 3.14 (P1). Restrictions apply in `types` and are reported as `TYPnnn` diagnostics there (table below).

Status values:

- **Supported**: compiles and matches CPython.
- **Restricted**: supported under stated conditions; otherwise falls back to `Any` (boxed, dynamic dispatch) or is rejected.
- **Rejected**: diagnostic `TYP` code; compilation fails.
- **Planned**: not in phase 1, scheduled in P5.

## Types and Values

| Feature | Status | Notes / Rationale |
|---|---|---|
| `int` | Supported | Arbitrary precision semantics; the IR has one `int` type and the compiler splits fast and slow path (ADR 0004) |
| `float`, `bool`, `None` | Supported | Unboxed `f64`, `i1`, unit |
| `str`, `bytes` | Supported | Immutable; Unicode semantics via `builtins` |
| `list[T]`, `dict[K,V]`, `set[T]`, `tuple[...]` | Supported | Element types inferred; heterogeneous list widens to `list[Union]` then `list[Any]` |
| `complex` | Rejected | `TYP150`. Rare in target workloads; Planned in P5 |
| `Decimal`, `Fraction`, `datetime` | Planned | Need stdlib |
| Annotations | Supported | Optional; an upper bound checked at assignments, arguments, returns, defaults and field stores. Mismatch is `TYP140` |
| `typing` generics, `TypeVar`, PEP 695 `type`, `Protocol`, `Literal`, `TypeGuard`/`TypeIs`, overloads | Restricted | Used as hints and for specialization; no runtime reflection on them. `ParamSpec`, `TypeVarTuple`, `NewType`, `TypedDict`, `Coroutine`, `dataclass_transform` and a declared `TypeVar` variance are `TYP150` |

## Statements and Expressions

| Feature | Status | Notes / Rationale |
|---|---|---|
| Assignment, augmented assignment, unpacking, walrus | Supported | |
| `if`/`while`/`for`/`break`/`continue`/`else` | Supported | |
| Comprehensions, generator expressions | Supported | Generator form via coroutines (docs/compiler.md) |
| `def`, defaults, keyword args, `*args`, `**kwargs` | Supported | `**kwargs` forms a `dict[str, T]`; call-site arity resolved statically where possible |
| Closures, `nonlocal`, `global` | Supported | `CLOSURE_NEW`, `UPVAL_*` |
| `lambda` | Supported | |
| Decorators on functions | Restricted | Supported when the decorator is statically known and its result type is inferable; otherwise `Any` (`decorator` fallback). A decorated class is `TYP150` |
| Generators, `yield from` | Restricted | `yield` and `yield from` work. `gen.throw()`/`gen.close()` and a `yield` inside `try`/`finally` or `with` are `TYP150` (Planned) |
| `try`/`except`/`else`/`finally`/`raise` | Supported | Handler tables; `finally` is duplicated per exit. `__context__` chaining is not modeled yet. `except*` is `TYP150` (Planned) |
| `with` | Supported | Context manager protocol resolved statically when type known |
| `match` | Supported | Lowered to decision tree over inferred types |
| `async`/`await` | Rejected | `TYP150`. Planned in P5; needs an event loop model |
| `assert`, `del` | Supported | `assert` removed under `-O` |
| `eval`, `exec`, `compile` | Rejected | Needs a full dynamic tier. `TYP100` for a call or a use as a value of the builtin, with a literal argument too; a shadowed name is allowed |
| `globals()`, `locals()`, `vars()` | Restricted | Only a module-level read of the dict (subscript load, `get`, `keys`, `values`, `items`, `copy`, `in`/`not in`). Any store, `update`, binding or passing the function or dict, and any use inside a function or class is `TYP101`. `vars(obj)` is `TYP130` |

## Classes and Objects

| Feature | Status | Notes / Rationale |
|---|---|---|
| Classes with fixed attribute sets | Supported | Fixed-layout minivm structs. Attributes are those assigned in `__init__`/class body/`__slots__`, found by whole-program analysis |
| Single and multiple inheritance, `super()` | Restricted | Static MRO (C3) computed at compile time. Classes with fields must form one inheritance chain (prefix layout); fields in several unrelated bases are `TYP150` (Planned P4/P5). A class nested in a function or class is `TYP150` |
| Methods, `@staticmethod`, `@classmethod`, `@property` | Supported | Devirtualized when receiver type is exact |
| Dunder methods (`__add__`, `__eq__`, `__iter__`, `__len__`, `__repr__`, ...) | Supported | Dispatched statically when operand types known |
| `dataclasses`, `enum` | Planned | Behavior defined via stdlib modules |
| `setattr`/`delattr`, `del obj.attr`, attribute store outside the layout, other statements in a class body | Rejected | `TYP110`: layout must be fixed. A class body may hold `def`, assignments to names, annotated names, `pass`, a docstring, `...` and nested classes |
| Monkey-patching (`C.f = g`, `C.n += 1`, `cls.n += 1`, `math.pi = 3`, a class variable through an instance) | Rejected | `TYP111`: invalidates devirtualization |
| `__getattr__`, `__getattribute__`, `__setattr__` overrides | Restricted | Falls back to `Any` dispatch for instances of that class only |
| Metaclasses, `__init_subclass__` hooks that mutate classes, impossible MRO, cyclic bases | Rejected | `TYP112`. Planned in P5 |
| `__class__` assignment | Rejected | `TYP113` |
| Descriptors (user-defined `__get__`/`__set__`) | Restricted | Static when descriptor type is known |

## Modules and Runtime

| Feature | Status | Notes / Rationale |
|---|---|---|
| `import` of modules reachable at compile time | Supported | Whole-program: all modules reachable from the entry module |
| `__import__` with a non-constant name or used as a value; `from m import *` without a literal `__all__` | Rejected | `TYP120` |
| `importlib` | Rejected | `TYP121`, even guarded |
| Import of a missing local module or of a module with a `SyntaxError` | Supported | Not an error: the import site raises `ModuleNotFoundError` (`SyntaxError`) at run time, as CPython does; a relative import in `__main__` raises likewise (ADR 0012) |
| Conditional imports (`try: import x except ImportError`) | Supported | An unshipped standard library module in a `try` that catches `ImportError`, `ModuleNotFoundError`, `Exception`, `BaseException` or bare is not `TYP121` |
| Namespace packages | Planned | A directory without `__init__.py` is not found (P4/P5) |
| Module dunders | Supported | `__name__` and `__file__` are module globals; the main module is `__main__` |
| Builtins (`print`, `len`, `range`, `enumerate`, `zip`, `sorted`, `min`, `max`, `sum`, `isinstance`, ...) | Supported | Host functions or inlined |
| `sys.argv`, `sys.exit`, `sys.stdout`/`stderr`/`stdin` | Supported | |
| Stdlib | Restricted | Shipped: `builtins`, `typing`, `math`, `sys`, `__future__`, `collections`, `collections.abc`. Any other module of `sys.stdlib_module_names` is `TYP121` unless the import is guarded, until P5 |
| C extensions, `ctypes` | Rejected | Out of scope |
| Threads, multiprocessing | Planned | minivm execution is single-threaded |
| Frame/introspection (`sys._getframe`, `sys._current_frames`, `__dict__`, `__annotations__`, `vars(obj)`, `import inspect`, `import gc`) | Rejected | `TYP130`; breaks SSA register allocation assumptions |

## Diagnostics

Every rejected construct has a code and a test in `types/subset_test.go`. Errors print as `file:line:col: error TYPnnn: message` (docs/cli.md).

| Code | Rule |
|---|---|
| `TYP100` | `eval`, `exec`, `compile` resolved to the builtin |
| `TYP101` | `globals()`, `locals()`, `vars()` other than a module-level read |
| `TYP110` | Layout violation: `setattr`/`delattr`, `del obj.attr`, store outside the layout, class-body statement not allowed |
| `TYP111` | Store to a class attribute, module attribute, or class variable through an instance |
| `TYP112` | Metaclass, mutating `__init_subclass__`, impossible MRO, cyclic base |
| `TYP113` | Store to `x.__class__` |
| `TYP120` | `__import__` with a non-constant name; `from m import *` without a literal `__all__` |
| `TYP121` | Unshipped standard library module imported unguarded; `importlib` |
| `TYP130` | Frames and introspection: `_getframe`, `_current_frames`, `__dict__`, `__annotations__`, `vars(obj)`, `inspect`, `gc` |
| `TYP140` | Annotation violation: invalid type expression, assignment, argument, return, parameter default, field store, generator not assignable to its declared return |
| `TYP150` | Not supported yet: `async`/`await`, `except*`, `complex`, t-strings, `yield` inside `try`/`finally` or `with`, `gen.throw`/`gen.close`, nested class, decorated class, fields in several bases, unsupported base, `ParamSpec`, `TypeVarTuple`, `NewType`, `TypedDict`, `dataclass_transform`, `Coroutine`, a `TypeVar` variance keyword, a sequence/mapping/class pattern the subject cannot satisfy |
| `TYP200` | Fallback to `Any` (below); a warning |

## Fallback Policy

A construct marked Restricted that cannot be proven statically becomes `Any` and is reported:

```text
foo.py:12:5: warning TYP200: <reason>: <message>
```

Reasons are a closed set (docs/typing.md). An explicit `Any` annotation is an opt-out and is not reported. `-X spry.fallback=error` will upgrade these to errors and `warn` will print them; the option is pending and the default is silent but counted in `-X spry.dump=types`. Errors always use `error TYPnnn`, warnings `warning TYP200`.

## Related

- docs/typing.md for inference and specialization limits
- docs/adr/0002-whole-program-inference.md
