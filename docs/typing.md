# Typing: Type System and Whole-Program Inference

Package: `types` (depends on the frontend only). Not to be confused with minivm's `types` package (alias `vmtypes`). The API shape and its deviations from `go/types` are recorded in ADR 0011; import resolution in ADR 0012.

## Goals

- Assign a static type to every expression, local and attribute in the reachable program.
- Keep annotations optional but verified: an annotation is an upper bound, and a contradiction is a diagnostic.
- Never change Python semantics. When a type cannot be established, widen to `Any`; do not guess.

## API

```go
func Check(fset *token.FileSet, filename string, file *ast.Module, opts ...Option) (*Module, *Info, error)
func WithFS(fsys fs.FS, dir string) Option
func Dump(m *Module, info *Info) string
```

- `Check` takes the parsed main module, loads every module it reaches (ADR 0012), declares scopes and objects, and runs the solver. The result is partial but non-nil when there are errors. The error is a `scanner.ErrorList` with one error when the main module has a compile-stage `SyntaxError` (for example `nonlocal x` at module level), otherwise a `types.ErrorList` of `TYPnnn` diagnostics sorted by position. `errors.Is` matches the categories `ErrUnsupported` (TYP100-TYP150), `ErrAnnotation` (TYP140) and `ErrTypeArg`.
- `Info` is generalized: `Types` (`TypeAndValue`: a type expression or a value), `Instances`, `Defs` (keyed by `ast.Node`), `Uses`, `Implicits`, `Selections`, `Scopes`, `Defined` (loads of a name that may be unbound), `Synth` (calls the checker made of a `raise C` or a decorator), `Matched` (attribute reads of class patterns), `Fallbacks`, and `Import(site)`. `Specs` maps each `*Func` to its specializations.
- `Spec` is one monomorphized function: `Signature`, `TypeArgs`, `Generalized`, `Generator`, per-expression `Types`, per-site `Calls` (`CallKind` `Static`, `Virtual` with `Overrides`, `CallClosure`, `Dynamic`; implicit dispatches are keyed by node or `Site`), `Vars` (the type each local is kept in), `Dead`, `Patterns`, and for closures `Outer`, `Captures`, `Cells`, `Guarded`.
- Published types never expose the solver's internal provenance of `Any` (explicit annotation, cap exceeded, reason). A pass at the end of the solve (`scrub`) canonicalizes every reachable `Any` in objects, `Info` and `Spec` to `Typ[Any]`, so `t == Typ[Any]` is a valid test for consumers.
- `TypeString` and `Type.String` write `python3.14` `typing._type_repr`. `Dump` writes a deterministic text of the result (modules in import order, globals, classes with `mro`, `field`, `var`, specializations, then `fallbacks N`).

## Type Lattice

```text
                          Any                  (top, gradual)
                           │
                       Union[...]              (source order, capped)
   ┌──────┬───────┬────────┼──────────┬──────────┬──────────┐
  int   float    bool     str      list[T] ... tuple[...]  class C   Meta   Literal
                           │
                        Never                  (bottom)
```

Join is `Join(x, y)`: a union with subsumption. A type assignable to another term is absorbed by it, `Any` absorbs everything, `Never` nothing.

- `bool`, `int` and `float` never absorb each other even though `AssignableTo(int, float)` holds through the numeric promotion of PEP 484: `bool | int | float` stays three terms, because the IR types differ and the difference is observable (`True + 1`, `1 / 2`). `int | float` is a union, not `float`.
- Tuples: same-length tuples with different elements are separate union terms (up to the cap); tuples of different length, or fixed and variadic, join to `tuple[T, ...]` of the joined element.
- `Literal` types join by their class (`Default` turns a `Literal` into its class instance when it is stored in a variable).
- Sibling classes of the program in one union are merged to their nearest common base other than `object` when the cap is applied, so a value has one layout; this is `capType`, not `Join`.
- Unions keep source order and print as written. Operators distribute over the terms of a union; at the IR a union that is not `T | None` is `any`, and the operation on it is a `union` fallback.

Caps are policy of the solver, not of the type system, and are unexported: union members 4, nesting depth 4, specializations per function 4 (64 per call site for prelude functions), 4096 specializations per program, 64 passes per specialization. A join past the union or depth cap widens to `Any` with reason `cap-exceeded`.

## Inputs

- All reachable modules, parsed. Shipped library modules are Python stubs (`types/lib`); see Universe below.
- Annotations, `TypeVar`/PEP 695 parameters and `typing` constructs, recognized by resolving names to the stub declarations, not by string matching.
- `sys.path` entries from `WithFS`.

## Universe

`Universe` is the scope of `builtins`, parsed from `types/lib/builtins.py` when the package initializes together with the modules it imports (`typing`, `collections.abc`). `math`, `sys`, `__future__` and `collections` are declared when first imported. Stub functions with `...` bodies are extern (implemented by the host); `__sorted__`, `__min__`, `__min2__`, `__max__`, `__max2__` and `__sum__` have bodies and are the prelude.

Prelude rule: a call of `sorted`, `min`, `max` or `sum` resolves to its prelude function when it passes `key=` or when an argument holds instances of a class of the program (the host cannot call user `__lt__`, `__add__` or key functions). Otherwise it stays on the host function. A prelude function is specialized per call site and is compiled with the program (docs/compiler.md). A builtin missing from the stub tables is a `dynamic-call` fallback, not `TYP150`.

## Algorithm

### 1. Symbols and scopes

A symtable pass (modeled on CPython's `symtable`) resolves each name to local/global/free/cell/class-scoped and finds compile-stage errors (`nonlocal` at module level, and so on) before inference. The declarer builds scopes, classes (C3 `MRO`), aliases and signatures. A class scope is skipped by nested function scopes, as in CPython.

### 2. Implicit type parameters and specialization

Every parameter without an annotation, and the result of a function without a return annotation, is an implicit `TypeParam` (`x@f`, `return@f`). A specialization is an instantiation of the function's type parameters: `Spec.TypeArgs` is keyed by the argument types (not by values). Annotated parameters fix their type, so `def f(x: int)` has one specialization and other call sites are checked against it. For a nested function the key includes the specialization that made its closure.

When a function reaches the specialization cap, or is called through a value, a single `Generalized` specialization serves those calls with the joined parameter types. Code the analysis cannot follow (`escape`) gets a generalized version with `Any` parameters.

### 3. Monotone fixpoint

The solver analyzes each specialization by abstract interpretation of its body over the argument types. Specializations read each other's results, the types of module globals, the types stored in captured cells and the element types of containers; each such read registers a location dependency. When a location grows (the join of what was stored), its readers are queued again.

Termination: every location only grows by joining, every type is capped (union, depth), so the lattice has finite height for a program; the number of specializations is capped; and a specialization that does not stabilize in 64 passes is widened to `Any`. The result of a recursive function starts at `Never` and rises. A specialization no root reaches is dropped before publishing.

### 4. Narrowing and definedness

Narrowing applies to non-cell local variables: `x is None` and `is not None`, truthiness (removes `None`), `isinstance(x, C)` (a supertype narrows to subclasses; a negative test removes them) and the subject of `match` per case. A branch narrowed to `Never` is recorded in `Spec.Dead` and not built. Each environment tracks whether a name is `Defined`, `MaybeDefined` or `Undefined`; `Info.Defined` holds the loads that are not always bound, which lower to a `NameError`/`UnboundLocalError` check. A module-level name read in a function is bound only if the module body binds it unconditionally before the function can run; import cycles make every name of the cycle possibly unbound.

### 5. Classes

Attributes come from `__init__`/method assignments to `self.x`, class-body assignments and `__slots__`; the layout is fixed and recorded as `Class.Fields` (own fields in layout order; base fields belong to the base). A store outside the layout is `TYP110`. Layout is a prefix chain: classes with fields must form one inheritance chain, otherwise `TYP150`. `super()` resolves statically through the MRO of the defining class; the `cls` of a classmethod is typed from the defining class. Method calls are `Static` when the receiver class has no overriding subclass in the program and `Virtual` (with the `Overrides` of the cone) otherwise. A class with `__getattr__`/`__getattribute__`/`__setattr__` is dynamic and its unresolved attributes are `dynamic-attr` fallbacks. Operators, truth tests, iteration, `with` and f-string conversions resolve to `Call`s through dunder lookup; `__eq__`/`__hash__` of program classes inside containers dispatch dynamically (docs/ssa.md).

### 6. Containers

A list, set or dict made at an allocation site has an element cell. Containers that flow into one another (assignment, argument, join, return) are unified (union-find) and share one cell; what is stored joins into it. Cells are replaced by what they hold when the result is published (a cell that contains itself becomes `Any`). A container whose element type exceeds the caps keeps `Any` and reports `cap-exceeded`.

### 7. Closures, decorators, generators, exceptions, match

- A captured variable lives in a cell whose type is the join of every store (`Spec.Cells`); a read that may happen before the store is `Guarded`.
- A decorator is applied if it is statically known and its result is inferable; `Info.Synth` records the application. A decorated module-level function is stored in a `$decorated` global. Otherwise the result is `Any` (`decorator`). A decorated class is `TYP150`.
- A generator function has result `Generator[Y, S, R]`, where Y joins the yields, S the sends and R the return. `yield from` over a generator takes its return type; delegation of `throw`/`close` is not modeled.
- The type of an `except` handler variable is the join of the matched classes; a handler whose classes are not known is `dynamic-except`. `raise C` is the instantiation of `C`.
- `match` types each pattern against the subject (`Spec.Patterns`); a class pattern with positional patterns reads `__match_args__` (`Info.Matched`).

### 8. Annotations

An annotation is an upper bound. Assignments, call arguments, returns, parameter defaults and field stores must be assignable to it, else `TYP140`; `int` is assignable to `float` by promotion. A variable with a union annotation keeps the narrower assigned type where it is assignable. `Any` as an annotation is an explicit opt-out: the value is `Any` and operations on it are not reported. `TypedDict`, `NewType`, `ParamSpec`, `TypeVarTuple`, `Coroutine` and `dataclass_transform` are `TYP150`; `Protocol`, `Literal`, `TypeGuard`/`TypeIs`, `Callable`, overloads and PEP 695 aliases are supported. A `TypedDict` base is `Mapping[str, object]`, not `dict`.

## `Any` Contract

`Any` lowers to the boxed `any` at the IR (docs/ssa.md); `T` to `any` is `box`, back is `unbox` and raises `TypeError` on mismatch. Operations on `any` are the `dyn*` instructions, which preserve full Python semantics. A fallback never changes results, only speed.

Each site where an operation is dispatched at run time on an `Any` operand is a `Fallback{Pos, End, Reason, Msg}` and diagnostic `TYP200`. The set of reasons is closed:

| Reason | Meaning |
|---|---|
| `param-conflict` | A parameter was widened because its call sites disagree beyond the caps |
| `cap-exceeded` | A type was widened past the union or depth cap |
| `union` | The operation is dispatched on a union of operand types |
| `escape` | The value flows through code the analysis cannot follow |
| `dynamic-attr` | The class computes its attributes, or a property cannot be resolved |
| `unresolved-attr` | The module or class has no such member |
| `decorator` | The result of a decorator is not known |
| `dynamic-except` | The classes an `except` catches, or a raised value, are not known |
| `dynamic-call` | No signature binds the arguments, or the callee has no static target |
| `dynamic-item` | An item that cannot be shaped statically (variable tuple index, wrong unpacking count, starred set/tuple item, `**` in a dict display) |
| `any-value` | An operand is an `Any` that an earlier site or a library signature produced |

One fallback is reported per site; a specific reason replaces `any-value`. Format (docs/subset.md): `file:line:col: warning TYP200: ...`. Fallbacks are counted in `-X spry.dump=types` (`fallbacks N`). The `-X spry.fallback=warn|error` policy is still pending: the option is not accepted yet (docs/cli.md).

## Testing

- Goldens: `types/testdata/*.py` with `.types` (docs/testing.md), regenerated with `go test ./types/... -update`.
- Table tests per feature in `types/*_test.go`; subset tests give every `TYP` rule at least one rejecting and one accepting case.
- `TypeString` is compared with the CPython manifest `typerepr.jsonl`; the compile-stage corpus checks the main-module `SyntaxError` cases with an empty `xfail.txt`.
- The soundness property test (run under CPython with `isinstance` probes at inferred sites; no probe may fail) needs execution and is deferred to P3.

## Related

- docs/subset.md, docs/ssa.md (types in IR), docs/adr/0002-whole-program-inference.md, docs/adr/0011-types-and-ssa-api-shape.md, docs/adr/0012-whole-program-import-resolution.md
