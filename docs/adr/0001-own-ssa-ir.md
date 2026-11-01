# 0001: Own SSA IR instead of minivm's internal SSA

- Status: Accepted

## Context

minivm has an SSA used by its optimizer and JIT, but it lives in `internal/ssa` and cannot be imported. It models minivm's value kinds (`i1`/`i32`/`i64`/`f64`/`ref`/`any`), not Python types such as `list[int]`, class layouts, or generators. Python-level optimizations (devirtualization, unboxing, range analysis for `int`, generator fusion) need Python type information.

## Decision

spry defines its own typed SSA IR in package `ssa`, built from the typed AST, optimized by package `optimize`, and lowered by package `compiler` to minivm bytecode through `program.Builder` and `FunctionBuilder`. After emission, minivm `optimize` and `program.Verify` run.

## Consequences

- Python-aware optimization happens where types are still known.
- `ssa` and `optimize` have no minivm dependency and are testable in isolation with text goldens.
- Cost: a second IR and an SSA-to-stack-code lowering (out-of-SSA, stack scheduling) that minivm's own passes partly duplicate.
- minivm's JIT constraints (for example, coroutines are not natively compiled) must be handled in `compiler`/`optimize`.

## Alternatives

- Emit bytecode directly from the AST (minipy's approach): no IR for optimization, monolithic emitter; rejected.
- Ask minivm to export its SSA: couples the two release cycles and still lacks Python types; rejected.
- Use LLVM or another backend: contradicts the goal of minivm as the only backend.
