# SSA IR

Package: `ssa`. Imports `ast` and `types` (builder input); never minivm. `optimize` operates on this IR.

Design reference: `golang.org/x/tools/go/ssa` (structure), Braun et al. "Simple and Efficient Construction of SSA Form" (builder). Block parameters are used instead of phi nodes in the data model (the printer shows `phi` sugar).

## Structure

```text
Program   → Modules, Types (class table), Globals, Entry
Module    → Name, Globals, Functions
Function  → Name, Sig, Params, Captures, Blocks (entry first), Pos, Flags (generator, specialized-from)
Block     → Index, Params []*Param, Instrs []Instr, Term Terminator, Preds, Succs
Value     → every instruction producing a result; has Type, Name (%N), Pos
```

Every `Value` is defined once and dominates its uses. Blocks end in exactly one terminator. Source positions (`token.Pos`) are attached to instructions that can raise or are observable, for tracebacks.

## IR Types

IR types are lower-level than `types.Type` but keep the Python shape needed for optimization.

| IR type | Meaning | minivm target |
|---|---|---|
| `i1` | bool | `i1` |
| `i32` | internal index/small counters | `i32` |
| `int` | Python int, checked i64 fast path + bigint | `i64` / `ref` (ADR 0004) |
| `f64` | float | `f64` |
| `str`, `bytes` | immutable sequences | `ref` (string) |
| `none` | unit | no value |
| `list<T>`, `dict<K,V>`, `set<T>` | containers | `ref` (array/map) |
| `tuple<T1..Tn>` | fixed-size aggregate | `ref` (struct) |
| `class C` | instance with fixed layout | `ref` (struct) |
| `func(P...) R` | function value | closure `ref` |
| `any` | boxed dynamic value | `any` |
| `bigint` | slow-path integer | `ref` |

Types are structural for containers and nominal for classes; `any` appears only where inference yielded `Any`.

## Instruction Set

Notation: `%v = op args : type`.

| Category | Instructions |
|---|---|
| Constants | `const` (int, float, bool, str, bytes, none) |
| Arithmetic | `add sub mul floordiv mod pow lshift rshift and or xor neg invert` typed `int`/`f64`; `truediv` int,int→f64 |
| Checked arithmetic | `add.chk sub.chk mul.chk` : `int` ops that produce `int` and may promote to bigint; `add.nochk` variants proven in-range by range analysis |
| Comparison | `eq ne lt le gt ge is isnot in notin` → `i1` |
| Conversion | `int2f64`, `f642int`, `box`, `unbox` (`any`↔T, checked), `cast` |
| Memory | `alloc class`, `loadfield obj.f`, `storefield obj.f, v`, `newlist`, `newtuple`, `newdict`, `newset`, `index`, `setindex`, `slice`, `len`, `append` |
| Calls | `call f(args)` (static), `callclosure`, `callmethod` (virtual via slot), `calldyn` (any), `calldefer` (generator `next`) |
| Closures | `makeclosure f [captures]`, `loadcell c`, `storecell c, v` |
| Globals | `loadglobal g`, `storeglobal g, v` |
| Iteration | `iter x`, `next it → (value, done)`; fast paths `rangeiter`, `listiter` |
| Strings | `concat`, `format` (f-strings), `strcmp` |
| Exceptions | `raise e`, `landingpad` (first instr of a handler block, yields the exception value), `reraise`, `excmatch e, class` → `i1` |
| Generators | `yield v` (terminator-like suspension), `resume g, sent`, `genstate` |
| Dynamic | `dynbinop op, a, b`, `dyngetattr`, `dynsetattr`, `dyncall` on `any` operands |
| Misc | `phi` (printed sugar for block params), `select c, a, b`, `unreachable` |

Operations on non-exact operands are never emitted for typed values: the builder picks the specific instruction from `types.Info`, so there is no generic `add` that dispatches at runtime except `dynbinop`.

## Terminators

| Terminator | Meaning |
|---|---|
| `jump B(args)` | Unconditional, with block arguments |
| `br cond, T(args), F(args)` | Conditional on `i1` |
| `switch v, [k → B], default` | Dense integer/enum tests (match lowering) |
| `ret v` | Return (value or none) |
| `raise e` | Raise, no normal successor; goes to exception edge or unwinds |
| `unreachable` | Never reached |

## Exception Edges

Any instruction that can raise carries an optional **exception edge** to a handler block, invoke-style:

```text
b1:
  %2 = call @g(%1) : int  ->  normal b2, except b5
b2: ...
b5:                                 ; handler
  %e = landingpad : any
  %m = excmatch %e, class ValueError
  br %m, b6(%e), b7(%e)
```

Rules:

- An instruction with an exception edge is the last instruction of its block (like a terminator `invoke`); the normal continuation is its fallthrough successor.
- Instructions that cannot raise (typed arithmetic proven in range, field loads on non-null exact types, comparisons of primitives) are ordinary and do not split blocks. This keeps blocks large in numeric code (zero-cost).
- `try`/`finally` is lowered by the builder: the `finally` body is duplicated for normal, exceptional and `return`/`break`/`continue` exits (or reached via a state-selecting block param) so the CFG is explicit.
- `with` lowers to the enter/exit calls plus an exception edge that calls `__exit__`.
- Uncaught exceptions leave the function through `raise`; `compiler` maps both to minivm `THROW` and handler tables.

## Generators

A generator function is a function with `Flags.Generator`. Representation in SSA:

- The function body is a normal CFG; `yield %v` is an instruction with one normal successor (the resume block) and produces a value (the sent value) in the successor via block parameter.
- `return v` inside a generator terminates the generator, delivering `StopIteration(v)`.
- Exceptions thrown in (`gen.throw`) arrive at the `yield` as an exception edge on the yield instruction.
- `yield from` is lowered by the builder to a loop around `yield` plus delegation to `resume`.
- The caller creates a generator object with `makegen f(args)` (value of type `generator<Y,S,R>`) and drives it with `resume` (`next`/`send`). `compiler` maps this to a minivm coroutine (docs/compiler.md).
- Generator expressions and comprehensions with generator form are synthesized generator functions.
- Optimization note: a generator consumed immediately by `for` in the same function and not escaping may be inlined into a loop (optimize `genfuse`).

## Text Format

Stable, parseable, used for goldens and `-X spry.dump=ssa`.

```text
module main

func @fib(%n: int) -> int {
b0:
  %0 = lt %n, 2 : i1
  br %0, b1, b2
b1:
  ret %n
b2:
  %1 = sub.chk %n, 1 : int
  %2 = call @fib(%1) : int
  %3 = sub.chk %n, 2 : int
  %4 = call @fib(%3) : int
  %5 = add.chk %2, %4 : int
  ret %5
}

func @main() -> none {
b0:
  %0 = call @fib(20) : int  -> normal b1, except b2
b1:
  %1 = call @print(%0) : none
  ret
b2:
  %e = landingpad : any
  raise %e
}
```

Grammar sketch: `func @name(params) -> type [flags] { block* }`; `block := label(params)? ':' instr* terminator`; values are `%name`; globals and functions `@name`; positions are not printed unless `-X spry.dump=ssa+pos`. `ssa.Parse` accepts exactly what `ssa.Print` produces (round-trip tested), which lets optimizer tests start from text.

## Verifier Invariants

`ssa.Verify(p)` checks:

1. Every block has exactly one terminator and it is last; no instruction after a terminator.
2. Entry block has no predecessors; all blocks reachable from entry (unreachable blocks are removed by `optimize`, but Verify flags them in strict mode).
3. Every value is defined once; every use is dominated by its definition (dominator tree computed).
4. Block argument count and types match the target block's parameters at every edge.
5. Operand types match instruction signatures (e.g. `add.chk` takes two `int`).
6. Terminator operand `cond` is `i1`.
7. Exception edges target blocks whose first instruction is `landingpad`; `landingpad` appears only there.
8. `call` argument arity/types match callee signature; `callclosure` operand has a function type.
9. Generator functions: `yield` only in functions flagged generator; `resume` only on generator values.
10. `phi` sugar is only in the printer; the data model has no phi nodes.
11. Types are closed: `any` is never implicitly converted to another type; conversions use `box`/`unbox`.
12. Predecessor/successor lists are consistent with terminators and exception edges.
13. No references to `ast` nodes remain (positions only).

Verification runs after build and, in tests or with `-X spry.verify`, after each optimization pass.

## Related

- docs/typing.md, docs/compiler.md, docs/adr/0001-own-ssa-ir.md
