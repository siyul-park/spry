# Compiler: SSA to minivm

Package: `compiler` (imports `ssa`, `builtins`, minivm; the pipeline driver is the root `spry` package). Output: `*program.Program` from minivm, built with `program.Builder` and `vmtypes.FunctionBuilder`, then passed through minivm `optimize.New(level)` and `program.Verify`. Opcode names below are minivm's (`instr/opcode.go`; see the [minivm instruction set](https://github.com/siyul-park/minivm/blob/main/docs/instruction-set.md)).

## Pipeline Inside compiler

```text
ssa.Program
  → type lowering (IR type → vmtypes.Type)
  → per function: out-of-SSA (block params → locals), block ordering, stack scheduling
  → instruction selection
  → vmtypes.FunctionBuilder (labels, Try ranges) → program.Builder
  → optimize.New(O1..O3) → program.Verify
```

minivm is a stack VM with locals. SSA values that are used once, immediately, stay on the operand stack; others get a local slot (`LOCAL_SET`/`LOCAL_GET`/`LOCAL_TEE`). Block parameters become locals assigned on each incoming edge (parallel-copy resolved with temporaries). Branches use `BR`, `BR_IF`, `BR_TABLE` with relative offsets managed by `FunctionBuilder` labels.

## Type Mapping

| SSA type | minivm type | Notes |
|---|---|---|
| `i1` | `i1` | comparisons and `bool` |
| `i32` | `i32` | internal counters, lengths |
| `int` | `i64` | inline fast path; bigint spill as `ref` (below) |
| `f64` | `f64` | |
| `str`, `bytes` | `ref` (string) | `STRING_*` ops; bytes as array of `i8` |
| `none` | none / `ref` null | `REF_NULL` when boxed |
| `list<T>` | `ref` (array of T) | `ARRAY_NEW`, `ARRAY_GET`/`SET`/`APPEND` |
| `dict<K,V>` | `ref` (map) | `MAP_NEW`, `MAP_GET`/`SET`/`LOOKUP` |
| `set<T>` | `ref` (map T→i1) | |
| `tuple<...>` | `ref` (struct) | fixed fields |
| `class C` | `ref` (struct) | `STRUCT_NEW`, `STRUCT_GET`/`SET` by field index |
| `func` | closure `ref` | `CLOSURE_NEW` |
| `any` | `any` | boxed word; `REF_TEST`/`REF_CAST` to recover |

Caveat: minivm's inline `i64` holds a 49-bit payload in the boxed word; values beyond ±2^48 are handled by minivm's wide-`i64` path (see minivm value representation). spry's bigint is an additional layer for values beyond 64 bits.

## Integers (ADR 0004, Proposed)

Python `int` has unbounded range. Checked fast path:

- `add.chk a, b`: `I64_ADD` then overflow test. The sign test `((a ^ r) & (b ^ r)) < 0` uses `I64_XOR`, `I64_AND`, `I64_LT_S`. On overflow, `BR_IF` to a slow-path block that calls host function `builtins.IntAdd(ref, ref)` producing a `bigint` ref.
- `mul.chk`: `I64_MUL` with a pre-check on operand magnitudes, or a host call when either operand exceeds a threshold (32-bit); division and shifts use the same pattern.
- Values of IR type `int` that may become bigint are carried as `any` only on the slow path; the common representation of `int` stays `i64` plus an out-of-band "is bigint" check in the slow-path block (the SSA builder splits blocks so the slow path produces a `bigint` typed value and joins through a block parameter of type `int` boxed as `any`).
- `add.nochk` and friends emit bare `I64_ADD` etc. They appear only when `optimize` range analysis proves the result fits (e.g., loop counters bounded by `range(n)` with `n` an `i32` value).

If ADR 0004 changes the representation, only this section and `builtins` int operations change.

## Instruction Selection

| SSA | minivm |
|---|---|
| `const int` | `I64_CONST` |
| `const f64` | `F64_CONST` |
| `const bool` | `I32_CONST` (0/1) typed `i1` |
| `add/sub/mul` (nochk) | `I64_ADD`, `I64_SUB`, `I64_MUL`; floats `F64_ADD`, `F64_SUB`, `F64_MUL` |
| `floordiv`, `mod` int | `I64_DIV_S`/`I64_REM_S` plus Python sign correction; zero check raises `ZeroDivisionError` via `THROW` |
| `truediv` | `I64_TO_F64_S` ×2, `F64_DIV`, zero check |
| `pow` | host function (`builtins.Pow`) or `F64_*` helper when exponent constant small |
| bit ops | `I64_AND/OR/XOR/SHL/SHR_S`, `I64_CLZ` etc. only for builtins that need them |
| comparisons | `I64_EQ/NE/LT_S/GT_S/LE_S/GE_S`, `F64_EQ/NE/LT/GT/LE/GE` |
| `select` | `SELECT` |
| `eq` on `str` | `STRING_EQ`; `lt` etc. `STRING_LT`... ; `concat` `STRING_CONCAT`; `len` `STRING_LEN` |
| `int2f64` | `I64_TO_F64_S`; `f642int` via host function with truncation checks |
| list ops | `ARRAY_NEW`, `ARRAY_LEN`, `ARRAY_GET`, `ARRAY_SET`, `ARRAY_APPEND`, `ARRAY_SLICE`, `ARRAY_COPY`, `ARRAY_DELETE`, `ARRAY_FILL` |
| dict ops | `MAP_NEW`, `MAP_GET`, `MAP_LOOKUP` (presence), `MAP_SET`, `MAP_DELETE`, `MAP_LEN`, `MAP_KEYS`, `MAP_ITER` |
| string iteration | `STRING_ITER` |
| field access | `STRUCT_GET`, `STRUCT_SET` |
| null/identity | `REF_NULL`, `REF_IS_NULL`, `REF_EQ` (`is`), `REF_NE` |
| `box`/`unbox` | `REF_NEW`/`REF_CAST`, `REF_TEST` for checks |
| `unreachable` | `UNREACHABLE` |

Operations without a direct opcode (hashing, repr, bigint, formatting, sorting) are host functions.

## Calls and Closures

- Static calls: `CALL` to a function constant (`CONST_GET` for function addresses as needed); tail calls in tail position that are provably self-or-mutual recursion use `RETURN_CALL`.
- Closures: free variables become captures. `CLOSURE_NEW` builds the closure from a function and captured values; inside, `UPVAL_GET`/`UPVAL_SET` access them. Captured mutable variables are cells (`REF_NEW`, `REF_GET`, `REF_SET`), shared between inner and outer function.
- Methods: virtual dispatch uses a per-class table of closures in a struct field; exact-type receivers devirtualize to a static `CALL`.
- Default arguments and keyword arguments are resolved at compile time into positional calls where the callee is known; unknown callees use `calldyn` through a `builtins` helper.
- Globals: `GLOBAL_GET`, `GLOBAL_SET`, `GLOBAL_TEE`. Module-level constants use `CONST_GET`.

## Classes

Each class compiles to a minivm struct type with one field per attribute in analyzed layout order, plus a hidden class-id field when virtual dispatch or `isinstance` is required. Inheritance is prefix layout (base fields first). Method tables are global constants. Attribute access with a static type is a single `STRUCT_GET`/`STRUCT_SET`.

## Generators

A generator function becomes a minivm coroutine function.

- `makegen` creates the coroutine by calling the function through minivm coroutine creation (`CALL` on a coroutine-typed function yields a coroutine ref).
- `yield %v` maps to `YIELD`; the sent value is delivered on resume.
- `resume g, sent` maps to `RESUME`; completion is tested with `CORO_DONE`; the yielded or final value is read with `CORO_VALUE`.
- `return v` inside a generator sets the final value and completes the coroutine; `for` over a generator is a loop of `RESUME` / `CORO_DONE` / `CORO_VALUE`, with `StopIteration` not materialized unless the program catches it.
- Note: `YIELD`/`RESUME` functions are not eligible for minivm ARM64 native compilation (the translator declines the unit), so spry's `genfuse` optimization (SSA level) fuses simple generator loops into plain loops to keep hot paths native.

## Exceptions

- `raise e` → `THROW` with an error payload (`ERROR_NEW` code + message); exception objects (instances of exception classes) are carried as `ref`. `ERROR_CODE` and `ERROR_GET` read a thrown error. Codes at or above `ErrorCodeUserBase` are Python exception class ids; VM traps (negative) map to `ZeroDivisionError`, `IndexError`, etc. by the `builtins` trap translator.
- Exception edges become handler-table ranges: `FunctionBuilder.Try(start, end, catch, depth)` where `[start,end)` covers the code between the raising instruction and its normal successor, `catch` is the label of the handler block (`landingpad` becomes the pushed error), and `depth` is the operand-stack depth to restore. The builder keeps stack depth at 0 across raising instructions (values live in locals) so ranges are simple.
- `excmatch` is `ERROR_CODE` comparison against the class id set (including subclasses) or a `builtins` call for non-trivial hierarchies.
- `finally`/`with` are already explicit in the CFG.

## Builtins Host Functions

`builtins` registers typed host functions (`interp.NewHostFunction` with `vmtypes.FunctionType`): bigint ops, `str` methods, list/dict/set methods without opcodes, `print`, `repr`, `hash`, formatting, `sorted`, exceptions, `dynbinop`/`dyngetattr`. Host functions must not call `vm.Run` recursively; operations that call back into Python (e.g., `sorted(key=...)`) are compiled as Python-level helper functions (written in the supported subset and compiled with the program), not as host calls.

## After Emission

1. `optimize.New(level).Optimize(prog)` with level from `-X spry.opt` (default O2). minivm passes: constant folding, dedup, DCE, GVN depending on level.
2. `program.Verify(prog)` always runs; failure is an internal compiler error (`GEN` diagnostic) with a dump of the offending function.
3. `-X spry.emit=<path>` serializes the program; `-X spry.dump=asm` prints the disassembly.

## Testing

Per-construct tests compile a snippet, run it on `interp`, and compare with CPython-recorded output; golden tests assert emitted instruction sequences for key patterns (typed loop = no boxing, `REF_*` absent).

## Related

- docs/ssa.md, docs/adr/0001-own-ssa-ir.md, docs/adr/0004-int-representation.md
