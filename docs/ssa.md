# SSA IR

Package: `ssa`. Imports `ast` and `token` (constants and positions) and the standard library; never minivm. `optimize` operates on this IR, and the builder (`Build`, which also imports `types`) produces it.

Design reference: `golang.org/x/tools/go/ssa` (structure), Braun et al. "Simple and Efficient Construction of SSA Form" (builder). The data model has block parameters and no phi nodes; the text shows the same.

## Structure

```text
Program   → Modules, Classes, Entry
Module    → Name, Globals, Functions, Init
Global    → Name, Type, Module
Class     → Name, Bases, Fields [Name, Type, Owner], Methods [Name, Func], Extern
Function  → Name, Source, Module, Params, Captures, Result, Generator, Yield, Send, Blocks (nil = extern)
Block     → Index, Params, Instrs, Preds, Succs
Value     → *Param | *Instr (defines a result) | *Const (inline operand)
Instr     → Op, ID, Typ, Args, Succs [Edge], Except *Edge, Callee, Global, Class, Attr, Pos, End
Edge      → Block, Args
```

- One `Instr` struct and an `Op` table (name, operand count, auxiliary kind, flags). Print, Parse and Verify are driven by the table.
- Globals belong to modules; the main module is `__main__`. Classes belong to the program and their names are unique in it. `Source` is the Python qualified name.
- A block ends in exactly one terminator or in an instruction with an exception edge. The entry block takes no parameters; function parameters and captures are visible everywhere.
- Constants are inline operands, there is no `const` op. `Pos` and `End` are `token.Position` values and are printed only with `WithPositions`.
- No maps appear in the data model. One unexported `finish()` per function numbers anonymous parameters, blocks and results in order and recomputes `Preds` and `Succs` (one entry per edge); Parse and the builder both end with it.

## IR Types

| IR type | Meaning | minivm target |
|---|---|---|
| `i1` | bool | `i1` |
| `i32` | internal index/small counters | `i32` |
| `int` | Python int, full Python semantics including values beyond 64 bits | `i64` fast path / `ref` (ADR 0004) |
| `f64` | float | `f64` |
| `str`, `bytes` | immutable sequences | `ref` |
| `none` | unit | no value |
| `any` | boxed dynamic value | `any` |
| `list<T>`, `dict<K,V>`, `set<T>` | containers | `ref` |
| `tuple<T1,...,Tn>`, `tuple<T...>` | fixed-size aggregate, or a variadic tuple whose elements all have type `T` | `ref` |
| `@C` | instance of class `C` | `ref` |
| `func(P1,...) -> R` | function value | closure `ref` |
| `cell<T>` | mutable cell shared by closures | `ref` |
| `gen<Y,S,R>` | generator that yields `Y`, receives `S`, returns `R` | coroutine `ref` |
| `iter<T>` | iterator over `T` | `ref` |
| `type<@C>` | the class object `C` | `ref` |
| `T?` | `T` or None, for every reference type (`@C?`, `list<int>?`, `str?`, `(func() -> int)?`) | `ref` |

There is no `bigint` type: `int` covers it, and the compiler splits fast and slow paths. Types are structural except classes, which are nominal. `any` appears only where inference yielded `Any`.

## Instruction Set

Notation: `%v = op operands : type`. Every instruction that defines a value prints its result type. "Raises" marks operations that may carry an exception edge.

| Category | Instructions |
|---|---|
| Arithmetic | `add sub mul` (int or f64, raise-free); `floordiv mod pow` (int or f64) and `truediv` (int,int or f64,f64 → f64) raise; `lshift rshift` raise; `and or xor` (int); `neg` (int or f64), `invert` (int), `not` (i1) |
| Comparison | `eq ne lt le gt ge` on two operands of one scalar type; `is isnot in notin`; all → `i1` |
| Conversion | `int2f64`, `f642int`, `box` (T → any), `unbox` (any → T, raises TypeError), `cast` (reference to reference) |
| Memory | `alloc @C`, `loadfield %o.f`, `storefield %o.f, %v`, `nonnull %o.name` (raises AttributeError on None; the result is the non-nullable type), `newlist`, `newtuple`, `newdict` (key, value, ...), `newset`, `index`, `setindex`, `delindex`, `slice %c, lo, hi, step` (bounds are int or none), `len`, `append`, `unpack %seq : tuple<...>` (checks the length, raises ValueError) |
| Calls | `call @f(args)` (static, never a generator), `callclosure %c(args)`, `callmethod %o.m(args)` (method found through the bases of the static class), `dyncall %f(args)` (all `any`) |
| Closures | `makeclosure @f [captures]` → `func(params) -> result`; `newcell %v`, `loadcell %c`, `storecell %c, %v` |
| Globals | `loadglobal @g`, `storeglobal @g, %v`, `delglobal @g` (the last two raise NameError) |
| Iteration | `iter %x` → `iter<T>`; `%ok = next %it : i1` advances; `%v = current %it : T` reads the value after a true `next` |
| Strings | `concat`, `format %v, %spec` |
| Exceptions | `raise %e`, `reraise %e` (terminators; they carry no edge), `landingpad : any` (first instruction of a handler block), `isinstance %v, @C` → `i1`, `typeof %v` → `any` (the class of a value, for `__exit__`), `classobj @C` → `type<@C>` (the class object, the `cls` of a classmethod) |
| Generators | `makegen @f(args)` → `gen<Y,S,R>`; `%s = yield %v : S`; `%ok = resume %g, %sent : i1`; `current %g` after `resume` reads `Y` after true and `R` after false |
| Dynamic | `dynbinop %a.op, %b`, `dyngetattr %o.name`, `dynsetattr %o.name, %v`, `dyncall`, all on `any` |
| Misc | `select %c, %a, %b`, `unreachable` |

Operations on typed values never dispatch at runtime. The builder picks the specific instruction from `types.Info`; only the `dyn*` operations work on `any`.

`.chk`/`.nochk` variants do not exist yet: every int operation has Python semantics. `.nochk` variants arrive with P4 range analysis, and so does `switch`, which `optimize` adds.

## Lowering Conventions

What the builder (`ssa.Build`) emits, which the verifier cannot check but the compiler and optimizer rely on:

- **Modules.** Each module has a function `<module>` (its `Init`) that starts with a guard on the hidden global `$init : i1`: it returns at once if set, and otherwise sets it, stores `__name__` and `__file__`, then runs the module body. Importing a module calls the `Init` of the module and of its parent packages, parents first. `Entry` is the `Init` of `__main__`. Shipped modules appear as modules of externs and of the prelude functions a call used.
- **Hidden globals.** Names containing `$` are never Python identifiers: `<qualname>$default$<param>` holds a default value that is not a constant (stored where the function is defined); `name$bound : i1` tells whether a module object that may be unbound has been bound; `<qualname>$decorated` holds the result of decorating a module-level function; class variables are `@C.attr` globals.
- **Specializations.** Each `types.Spec` is a function. A function with more than one specialization is named `name#k` (`k` counts in specialization order); `Source` stays the qualified Python name. Two functions with the same qualified name in a module (lambdas) get `$n` suffixes.
- **Closures and cells.** A variable captured by a nested function lives in a `cell<T>` created by `newcell undef` and passed as a capture parameter. A variable that may be unbound when read has a second `cell<i1>` named `x$bound`; a read of an unbound free variable raises `NameError: cannot access free variable ...`.
- **Types.** The empty tuple type has `Elems` nil, so it prints and reads back equal. Big integer constants are canonical (`big.Int` normalized), so `Parse(Print(p))` equals `p`.
- **Iteration.** Every `for`, comprehension and unpacking goes through `iter` / `next` / `current`. An iterator of a class of the program is advanced by calling its `__next__` inside a handler region for `StopIteration`; builtin iterators and generators use the extern or `resume`.
- **Virtual calls and bridges.** A call of an overridable method is a `callmethod` on the slot; every implementation in a slot takes the parameters of the first and returns the join of the results. An override whose IR signature differs is entered through a generated bridge function that converts.
- **User `__eq__`/`__hash__`.** A container operation whose items are instances of a class that defines `__eq__` or `__hash__` is built as a `dyn*` call (host containers compare by identity), and the site is noted as a fallback.
- **`finally` and `with`.** The `finally` body is built again for each way out (fall through, `return`, `break`, `continue`, exception); `with` calls `__exit__` from the exception edge and, if the result is truthy, swallows the exception (`jump` to the end) and otherwise `reraise`s it.
- **Unreachable code.** An expression of type `Never` ends its block and nothing after it is built; a statement in `Spec.Dead` is not built.

## Terminators

| Terminator | Meaning |
|---|---|
| `jump bN(args)` | Unconditional, with block arguments |
| `br %c, bT(args), bF(args)` | Conditional on `i1` |
| `ret` / `ret %v` | Return; no operand when the function returns `none` |
| `raise %e`, `reraise %e` | Unwind; no successor and no exception edge |
| `unreachable` | Never reached |

## Exception Edges

An instruction that can raise may carry an exception edge, invoke-style:

```text
b0:
  %0 = call @g(%x) : int -> normal b1(%0), except b2(%x)
b1(%r: int):
  ret %r
b2(%y: int):
  %1 = landingpad : any
  %2 = isinstance %1, @ValueError : i1
  br %2, b3, b4(%1)
```

Rules:

- The instruction is the last of its block, and `Succs[0]` is its normal edge; `Except` is the handler edge.
- The normal successor has exactly one predecessor.
- The result exists only on the normal edge: it may be an argument of the normal edge and is available in blocks dominated by the normal successor, but it is not an operand of the instruction and not an argument of the exception edge.
- A handler block starts with `landingpad`, is reached only by exception edges, and `landingpad` appears only there. Handler parameters are passed by the edge; the exception value comes from `landingpad`.
- Instructions that cannot raise (add/sub/mul, comparisons of scalars, `loadfield` on a non-nullable class, `len`) never split blocks. `loadfield` on a nullable `C?` is invalid; use `nonnull` first.
- `try`/`finally` is lowered by the builder: the `finally` body is duplicated per exit (or selected by a block parameter). `with` calls `__exit__` from the exception edge. A `raise` caught in the same function is lowered to a `jump` to the handler's code.
- `compiler` copies edge arguments into the handler's block-parameter locals when it lowers the edge (docs/compiler.md).

## Generators

A generator function has `Generator`, `Yield` and `Send`; its `Result` is the type of the returned value (`StopIteration(v)`).

- `%s = yield %v : S` is a value instruction: it yields `v` and the sent value is its result. It may carry an exception edge for an exception raised while the generator is resumed. `gen.throw()` and `gen.close()` are not supported (`TYP150`).
- `return v` ends the generator.
- `yield from` is lowered to a loop that resumes the delegate with what the caller sent and yields what it gives; the value of the expression is the delegate's return value (`None` for any other iterable). `throw` and `close` are not delegated.
- The caller creates the generator with `makegen`, drives it with `resume` and reads `current`. `compiler` maps this to a minivm coroutine (docs/compiler.md).
- Generator expressions are synthesized generator functions. A generator consumed by a `for` in the same function may be inlined by `optimize` (`genfuse`).

## Text Format

Stable and parseable; used for goldens and `-X spry.dump=ssa`. `Print` writes a canonical form and `Parse(Print(p))` equals `p` (round-trip tested), which lets optimizer tests start from text.

```text
class @Point : @Base {
  field x : int from @Base
  field y : int
  method norm -> @geo:norm
}

module geo
global @origin : @Point?
init @$init

func @norm(%self: @Point) -> int {
b0:
  %0 = loadfield %self.x : int
  ret %0
}

extern func @sqrt(%x: f64) -> f64

module __main__

func @<module>() -> none {
b0:
  %0 = alloc @Point : @Point
  %1 = call @geo:norm(%0) : int
  ret
}

entry @__main__:<module>
```

Grammar (line oriented; blank lines and lines starting with `;` are ignored):

```text
program  := decl*
decl     := class | 'module' NAME | 'global' '@'NAME ':' type | 'init' ref | func | 'entry' qref
class    := ['extern'] 'class' '@'NAME [':' cref (',' cref)*] '{' member* '}'
member   := 'field' NAME ':' type ['from' cref] | 'method' NAME '->' qref
func     := ['extern'] 'func' '@'NAME '(' params ')' ['[' params ']'] '->' type
            ['yield' type 'send' type] ['source' STRING] ['{' block+ '}']
block    := 'b'N ['(' params ')'] ':' instr*
params   := [ '%'NAME ':' type (',' '%'NAME ':' type)* ]
instr    := ['%'N '='] OP body [':' type] ['->' 'normal' edge ',' 'except' edge] ['!pos' pos pos]
edge     := 'b'N ['(' operand,* ')']
operand  := '%'NAME | constant
constant := INT | FLOAT | 'True' | 'False' | 'None' | STRING | BYTES | 'undef'  [':' type]
```

- `module NAME` starts a module; the functions, globals and `init` that follow belong to it. A function or global defined in another module is `@mod:name`; inside its own module it is `@name`. Methods, `entry` and class references outside a module context are qualified. Class names are program-wide: a class reference is `@Name` and never has a module, so the builder names each class by its module-qualified Python name (`@__main__.C`, `@pkg.mod.Outer.Inner`, builtins unqualified as `@int`), which is unique across modules.
- Operand forms by op: `add %a, %b`; `call @f(%a)`; `makeclosure @f [%c]`; `callclosure %c(%a)`; `callmethod %o.m(%a)`; `loadfield %o.x`; `storefield %o.x, %v`; `loadglobal @g`; `storeglobal @g, %v`; `alloc @C`; `isinstance %v, @C`; `jump b1(%a)`; `br %c, b1, b2(%a)`.
- Values are `%name`. Instruction results are numbered `%0, %1, ...` in block order; parameters have non-numeric names. Blocks are `b0, b1, ...`.
- `ret` takes no operand for a `none` function. A function with `extern` has no body.
- Name quoting: a name made only of `[A-Za-z0-9_.$#<>]` with balanced `<>` is written bare, anything else (spaces, `:`, `-`, non-ASCII, empty) as `@"..."` with Python repr escapes. `:` separates the module and is not part of a bare name. The same applies to module, field, method and attribute names. A variadic tuple of a non-nullable class writes the class quoted: `tuple<@"C"...>`.
- Constants: ints are decimal big integers (`-17`, `340282366920938463463374607431768211456`). Floats print so that they read back exactly and never look like ints (`1.0`, `1e+21`, `-0.0`, `inf`, `-inf`, `nan`). Strings are Python reprs (`ast.Repr`, either quote on input) and bytes are `b'...'` reprs. A constant is typed by its literal (`2` int, `2.5` f64, `True` i1, `None` none, `'x'` str, `b'x'` bytes); any other type is suffixed without spaces: `0:i32`, `None:@C?`, `'x':any`. `undef:T` is an undefined value of type `T`.
- Positions print as `!pos "file" offset:line:col "file" offset:line:col` after the instruction and only with `WithPositions`.

`ssa.Parse` accepts what `ssa.Print` produces and resolves forward references; it does not verify.

## Verifier Invariants

`ssa.Verify(p)` returns an error wrapping `ErrInvalid` that lists every violation as `SSAnnn where: message`:

| Code | Invariant |
|---|---|
| SSA001 | Every instruction is well formed: operand count, auxiliary fields the op needs, edge count, no nil operands, no constant as operand 0 of dotted or indirect forms |
| SSA002 | Every block ends in one terminator or an instruction with an exception edge, with nothing after it; the entry block has no parameters |
| SSA003 | The entry block has no predecessors; every block is reachable from it |
| SSA004 | Values are defined once; every operand is defined in the function; parameter names are unique, non-numeric names of `[A-Za-z0-9_$]` |
| SSA005 | Every use is dominated by its definition (block parameters and function parameters dominate their block; an invoke result is defined at the start of its normal successor) |
| SSA006 | Edge arguments match the target block parameters in count and type |
| SSA007 | Operand and result types match the operation: arithmetic, comparison, containers, memory, cells, globals, iteration, `select`, `ret`, conditions are `i1` |
| SSA008 | `call`, `callclosure`, `callmethod` match the callee signature; `makeclosure` matches the captures |
| SSA009 | Exception edges target blocks whose first instruction is `landingpad`, and only exception edges target them; `landingpad` appears only first in a block |
| SSA010 | An instruction with an exception edge ends its block, its normal successor has one predecessor, and its result is not an operand or an exception-edge argument |
| SSA011 | `yield` only in generator functions, with the yield and send types; only generators have `Yield` and `Send` |
| SSA012 | `Block.Index` is the position; `Preds` and `Succs` match the edges |
| SSA013 | Types are well formed: element counts, `Nullable` only on reference types, `Variadic` only on one-element tuples, class types name a class |
| SSA014 | Membership: every referenced `*Function`, `*Class` and `*Global` (operands, types, methods, `Entry`, `Init`) is a member of the program; names are unique and non-empty; the class hierarchy is acyclic; field owners are the class or a base |
| SSA015 | Constants fit their type; `undef` has no value |
| SSA016 | `loadfield`/`storefield` receivers are non-nullable classes with the field |

`Build` verifies its result. After the optimizer lands, verification also runs after each pass in tests and with `-X spry.verify` (pending).

## Related

- docs/typing.md, docs/compiler.md, docs/adr/0001-own-ssa-ir.md
