# Phase-1 Language Subset

The phase-1 subset is a set of Python 3.14 programs that whole-program inference can type and that lower to unboxed minivm operations. The invariant: **every accepted program is a valid Python 3.14 program with the same observable behavior under CPython.** spry never accepts a program with different semantics.

The parser accepts all of Python 3.14 (P1). Restrictions apply in `types` and are reported as diagnostics there.

Status values:

- **Supported**: compiles and matches CPython.
- **Restricted**: supported under stated conditions; otherwise falls back to `Any` (boxed, dynamic dispatch) or is rejected.
- **Rejected**: diagnostic `TYP` code; compilation fails.
- **Planned**: not in phase 1, scheduled in P5.

## Types and Values

| Feature | Status | Notes / Rationale |
|---|---|---|
| `int` | Supported | Arbitrary precision semantics. Checked i64 fast path, bigint slow path (ADR 0004) |
| `float`, `bool`, `None` | Supported | Unboxed `f64`, `i1`, unit |
| `str`, `bytes` | Supported | Immutable; Unicode semantics via `builtins` |
| `list[T]`, `dict[K,V]`, `set[T]`, `tuple[...]` | Supported | Element types inferred; heterogeneous list widens to `list[Union]` then `list[Any]` |
| `complex` | Planned | Rare in target workloads |
| `Decimal`, `Fraction`, `datetime` | Planned | Need stdlib |
| Annotations | Supported | Optional; checked against inference. Mismatch is `TYP` error |
| `typing` generics, `TypeVar`, PEP 695 `type` | Restricted | Used as hints and for specialization; no runtime reflection on them |

## Statements and Expressions

| Feature | Status | Notes / Rationale |
|---|---|---|
| Assignment, augmented assignment, unpacking, walrus | Supported | |
| `if`/`while`/`for`/`break`/`continue`/`else` | Supported | |
| Comprehensions, generator expressions | Supported | Generator form via coroutines (docs/compiler.md) |
| `def`, defaults, keyword args, `*args`, `**kwargs` | Supported | `**kwargs` forms a `dict[str, T]`; call-site arity resolved statically where possible |
| Closures, `nonlocal`, `global` | Supported | `CLOSURE_NEW`, `UPVAL_*` |
| `lambda` | Supported | |
| Decorators | Restricted | Supported when the decorator is statically known and its result type is inferable; otherwise `Any` |
| Generators, `yield from` | Supported | |
| `try`/`except`/`else`/`finally`/`raise` | Supported | Handler tables; `except*` Planned |
| `with` | Supported | Context manager protocol resolved statically when type known |
| `match` | Supported | Lowered to decision tree over inferred types |
| `async`/`await` | Planned | Needs an event loop model |
| `assert`, `del` | Supported | `assert` removed under `-O` |
| `eval`, `exec`, `compile` on non-literal strings | Rejected | Needs a full dynamic tier. Diagnostic `TYP100`. Literal-string `eval` may be constant-folded (Planned) |
| `globals()`, `locals()`, `vars()` writes | Rejected | `TYP101`; reads are Restricted to module-level read-only snapshots |

## Classes and Objects

| Feature | Status | Notes / Rationale |
|---|---|---|
| Classes with fixed attribute sets | Supported | Fixed-layout minivm structs. Attributes are those assigned in `__init__`/class body/`__slots__`, found by whole-program analysis |
| Single and multiple inheritance, `super()` | Supported | Static MRO (C3) computed at compile time |
| Methods, `@staticmethod`, `@classmethod`, `@property` | Supported | Devirtualized when receiver type is exact |
| Dunder methods (`__add__`, `__eq__`, `__iter__`, `__len__`, `__repr__`, ...) | Supported | Dispatched statically when operand types known |
| `dataclasses`, `enum` | Planned | Behavior defined via stdlib modules |
| `setattr`/assignment adding attributes not found by analysis | Rejected | `TYP110`: layout must be fixed. Rationale: keeps struct layout and zero-cost attribute access |
| Monkey-patching (assigning to a class/module attribute from outside) | Rejected | `TYP111`: invalidates devirtualization |
| `__getattr__`, `__getattribute__`, `__setattr__` overrides | Restricted | Falls back to `Any` dispatch for instances of that class only |
| Metaclasses, `__init_subclass__` hooks that mutate classes | Rejected | `TYP112`. Planned in P5 |
| `__class__` assignment | Rejected | `TYP113` |
| Descriptors (user-defined `__get__`/`__set__`) | Restricted | Static when descriptor type is known |

## Modules and Runtime

| Feature | Status | Notes / Rationale |
|---|---|---|
| `import` of modules reachable at compile time | Supported | Whole-program: all modules known to `spry.Compile` |
| `importlib`, `__import__` with non-constant names | Rejected | `TYP120` |
| Conditional imports (`try: import x except ImportError`) | Restricted | Supported when the module set is decidable at compile time |
| Builtins (`print`, `len`, `range`, `enumerate`, `zip`, `sorted`, `min`, `max`, `sum`, `isinstance`, ...) | Supported | Host functions or inlined |
| `sys.argv`, `sys.exit`, `sys.stdout`/`stderr`/`stdin` | Supported | |
| Stdlib | Restricted | Only modules shipped in `builtins` (future stdlib modules become their own packages); others are `TYP121` import errors until P5 |
| C extensions, `ctypes` | Rejected | Out of scope |
| Threads, multiprocessing | Planned | minivm execution is single-threaded |
| Frame/introspection (`sys._getframe`, `inspect`, `gc`) | Rejected | `TYP130`; breaks SSA register allocation assumptions |

## Fallback Policy

A construct marked Restricted that cannot be proven statically becomes `Any` and is reported:

```text
foo.py:12:5: warning TYP200: dynamic attribute access on Any value `x` (reason: x is a parameter with 3 incompatible call-site types)
```

`-X spry.fallback=error` upgrades these to errors; `warn` prints them; the default is silent but counted in `-X spry.dump=types`.

## Related

- docs/typing.md for inference and specialization limits
- docs/adr/0002-whole-program-inference.md
