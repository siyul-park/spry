# 0004: Integer representation

- Status: Proposed

## Context

Python `int` is unbounded. minivm has `i64` (inline up to 49 bits, wider values handled by its wide-i64 path) and no bigint. Most programs use small ints in hot loops; correctness requires bigint semantics on overflow.

## Decision

`int` is a checked `i64` fast path: arithmetic emits an overflow check that branches to a slow path creating a bigint `ref` implemented in package `builtins`. Range analysis in `optimize` removes the checks where bounds are provable (loop counters, `len()` results, masked values). Values of type `int` that may be bigint are distinguished by a representation check in the slow path blocks; the exact encoding (boxed `any` versus a tagged pair) is to be fixed by a prototype and benchmark before acceptance.

## Consequences

- Hot integer code is bare `I64_*` ops once ranges are known.
- Unprovable arithmetic pays a compare and a never-taken branch.
- Full Python integer semantics, including `**` and `//`, are preserved.
- The encoding decision touches `ssa` types, `compiler` and `builtins`; docs/compiler.md isolates it.

## Alternatives

- Plain `i64` with wraparound: fast but incorrect; violates the compatibility goal.
- Always bigint-capable boxed ints: correct but defeats zero-cost.
- Trap on overflow (`OverflowError`): deviates from CPython; may be offered as an opt-in flag later.
