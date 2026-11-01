# 0002: Whole-program type inference

- Status: Accepted

## Context

Zero-cost abstraction requires knowing types statically. Python programs are mostly unannotated, so annotations alone are insufficient. spry compiles whole programs (all reachable modules are available), unlike a JIT that observes runtime types.

## Decision

Types are determined by whole-program inference: constraint generation per function, a call-graph fixpoint over function summaries, call-site specialization (monomorphization) with caps, and widening to `Union` then `Any`. Annotations are optional but checked and used as seeds. `Any` lowers to boxed minivm values with dynamic dispatch, and each fallback is a diagnostic (`-X spry.fallback=warn|error`). Details: docs/typing.md.

## Consequences

- Typed code lowers to unboxed operations; untyped dynamic code still works via `Any`.
- Compilation cost grows with program size and specialization count; caps bound it.
- Programs depend on the full module set at compile time; dynamic imports are rejected or restricted (docs/subset.md).
- Inference results must be sound: a property test checks inferred types against CPython runs.

## Alternatives

- Annotations required (mypyc style): simple and predictable but burdens users and rejects ordinary Python.
- Runtime type feedback and JIT: needs a deoptimizing runtime that minivm does not provide for Python types.
- Gradual typing with no inference: leaves most code on the slow path.
