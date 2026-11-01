# Typing: Lattice and Whole-Program Inference

Package: `types` (depends on the frontend only). Not to be confused with minivm's `types` package (alias `vmtypes`).

## Goals

- Assign a static type to every expression, local and attribute in the reachable program.
- Keep annotations optional but verified: an annotation constrains inference, and a contradiction is a diagnostic.
- Never change Python semantics. When a type cannot be established, widen; do not guess.

## Type Lattice

```text
                         Any                    (top)
                          │
                       Union[...]               (bounded, normalized)
        ┌────────┬────────┼─────────┬──────────┬────────────┐
       int     float     bool       str       bytes        None
      (bool ⊑ int)         list[T]  dict[K,V]  set[T]  tuple[T1..Tn] | tuple[T, ...]
                        Callable[[P...], R]   class C   instance of C   module M
                          │
                       Never                    (bottom)
```

| Type | Notes |
|---|---|
| `Never` | Bottom; unreachable code, `raise`-only functions |
| `None` | Singleton |
| `bool` | Subtype of `int` as in Python; kept unboxed as `i1` unless used as `int` |
| `int` | Arbitrary precision; representation decided in `compiler` (ADR 0004) |
| `float` | IEEE-754 double |
| `str`, `bytes` | Immutable |
| `list[T]`, `set[T]` | Invariant in `T` for mutation soundness, covariant for read-only use after escape analysis |
| `dict[K,V]` | Insertion-ordered |
| `tuple[T1,...,Tn]` | Fixed-length heterogeneous; `tuple[T, ...]` variadic |
| `Callable[[P...], R]` | Includes bound methods and closures; a known callee has a precise function type |
| `class C` / instance `C` | Nominal with fixed attribute layout; subtyping by inheritance |
| `Union[A, B, ...]` | At most `UnionCap` (default 4) members, flattened and sorted; beyond that widens to `Any` |
| `Any` | Top. Boxed minivm `any`/`ref` at runtime; every use is a dynamic dispatch site |

Join (`⊔`) is least upper bound: `int ⊔ float = Union[int, float]` (not `float`; Python does not coerce implicitly), `bool ⊔ int = int`, `list[int] ⊔ list[str] = list[Union[int,str]]` (when sound) else `Any`.

## Inputs

- All reachable modules (via `fs.FS` from `spry.Compile`), already parsed.
- Declared annotations, `TypeVar`/PEP 695 parameters, `typing` constructs recognized by name resolution (not string matching).
- Built-in signatures: a table for `builtins` giving parameter and result types (generic where needed, e.g. `sorted(Iterable[T]) -> list[T]`).

## Algorithm

### 1. Symbols and scopes

A symtable pass (modeled on CPython's `symtable`) resolves each name to local/global/free/cell/class-scoped, records `nonlocal`/`global`, comprehension scopes, and class bodies. Name resolution is complete before inference starts.

### 2. Constraint generation

For each function body, walk the AST and emit constraints over type variables `α` attached to expressions, locals, parameters, returns and attributes:

- `x = e` : `τ(e) ⊑ τ(x)`.
- call `f(a1..an)` : bind arguments to parameters, `τ(ai) ⊑ τ(pi)`, result `τ(call) = ret(f)`.
- attribute read `o.a` : resolved against the class layout of `τ(o)`.
- operators resolve through a static dispatch table (`int + int -> int`, `str + str -> str`, user `__add__` via method lookup).
- control flow joins at SSA-like merge points using ⊔.
- annotations add equality constraints `τ(x) = A`; a later inferred value not `⊑ A` is a diagnostic.

### 3. Call-graph fixpoint

Functions are analyzed over a worklist ordered by SCCs of the call graph (callees first).

```text
repeat until no summary changes:
    for each function F in worklist:
        solve F's constraints given current summaries of callees
        update summary(F) = (param types joined over call sites, return type)
        if summary(F) changed: enqueue callers of F
```

Summaries are monotone in the lattice and the lattice has finite height (union capped), so the fixpoint terminates. Recursive SCCs start at `Never` for return types and iterate upward.

### 4. Specialization (monomorphization)

When a function's parameter types differ across call sites, instead of joining immediately the analysis creates specializations keyed by the tuple of argument types:

| Limit | Default | Behavior when exceeded |
|---|---|---|
| Specializations per function (`SpecCap`) | 4 | Remaining call sites share one generalized version with joined (widened) parameter types |
| Type depth (`list[list[...]]`) | 4 | Widen inner type to `Any` |
| Union members (`UnionCap`) | 4 | Widen to `Any` |
| Total specializations per program | 4096 | Further new specializations use the generalized version |

Specialization keys include the exact types of arguments that influence the result (e.g., `int`, `list[int]`), not literal values. Methods specialize on the receiver class and argument types. Generic builtins specialize on element types.

### 5. Widening

On exceeding a cap or when constraints are contradictory in a way the lattice cannot express, a type variable widens in two steps: first to a bounded `Union`, then to `Any`. Widening is recorded:

```text
Widening{Pos, Var, From, To, Reason}
```

### 6. Output: `types.Info`

- `Types map[ast.Expr]Type`, `Defs`/`Uses` for names, `Selections` for attribute accesses (field offset or method), `Specs []Specialization`, `Diagnostics`.
- Per-call-site resolution: `Static(callee)`, `Virtual(slot)`, or `Dynamic` (Any).

## Annotations

- Optional everywhere. Absent annotation means "infer".
- Present annotation is **checked**: initial value, assignments, call arguments and returns must be subtypes, else `TYP` error. Annotations are also used as seeds that cut specialization (`def f(x: int)` produces one specialization for `int` and rejects other call sites).
- `Any` annotation is explicit opt-out: the value is `Any` and sites are not reported as fallbacks.
- `TypedDict`, `Protocol`, `Literal`: recognized as structural constraints in P2 where they reduce to the lattice; otherwise `Any` with a note.

## `Any` Contract

`Any` at runtime is a boxed minivm `ref`/`any` value (NaN-boxed word; ints, floats, bool inline, other types as heap refs). Rules:

1. Every conversion `T → Any` is a box; `Any → T` is a checked cast (`REF_TEST`/`REF_CAST` or kind check) that raises `TypeError` on mismatch, matching Python's dynamic behavior.
2. Operations on `Any` call runtime dispatch host functions (`builtins.BinaryOp(op, a, b)` etc.), preserving full Python semantics.
3. A fallback never changes results, only speed.
4. Each `Any` site is a diagnostic `TYP200` with reason. Policy:

| `-X spry.fallback` | Behavior |
|---|---|
| (unset) | Silent; counts shown in `-X spry.dump=types` |
| `warn` | Print each fallback site and reason to stderr, continue |
| `error` | Treat fallbacks as errors; compilation fails with exit 1 |

Reasons are structured: `param-conflict` (call sites disagree beyond caps), `container-heterogeneous`, `unresolved-attr`, `dynamic-name`, `escape` (value stored in an `Any` container), `annotation-any`, `cap-exceeded`.

## Class Layout

For each class the analysis finds the attribute set by whole-program scan of `self.x = ...` in methods, class-body assignments, and `__slots__`. Attribute types are inferred as the join over all assignments. Layout is fixed; later additions outside this set are rejected (docs/subset.md `TYP110`). Method resolution uses the static C3 MRO.

## Testing

- Table tests: program text → expected type of named expressions and diagnostics.
- Fixpoint tests: mutual recursion, polymorphic helper called with `int` and `str`, cap-exceeded widening.
- Soundness check (property test): run the program under CPython with `isinstance` probes inserted at inferred sites; no probe may fail.

## Related

- docs/subset.md, docs/ssa.md (types in IR), docs/adr/0002-whole-program-inference.md
