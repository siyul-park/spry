# Frontend: Tokens, AST, Parser

Packages: `token`, `scanner`, `ast`, `parser`. They import nothing else in spry.

## token

- `token.Token` is an enum of token kinds: names, numbers, strings, operators, delimiters, `NEWLINE`, `INDENT`, `DEDENT`, `COMMENT`, `ENDMARKER`, plus f-string/t-string tokens (`FSTRING_START`, `FSTRING_MIDDLE`, `FSTRING_END`, `TSTRING_START`, `TSTRING_MIDDLE`, `TSTRING_END`) mirroring CPython's `token` module.
- Hard keywords are token kinds. Soft keywords (`match`, `case`, `type`, `_`) scan as `NAME`; the parser decides contextually.
- `token.Pos` is an `int` offset into a `token.FileSet` (go/token style); `0` is `NoPos`. `FileSet.Position(pos)` returns `{Filename, Offset, Line, Column}`.
- `File` records line start offsets, so `Pos` stays one word per node.

## Positions

CPython attributes: `lineno` (1-based), `col_offset` (0-based, **UTF-8 bytes**), `end_lineno`, `end_col_offset`. spry stores `token.Pos` offsets (bytes into the source, which is UTF-8 after decoding) and converts on demand:

| CPython attribute | Derivation |
|---|---|
| `lineno` | line of `Pos()` via `File.Line` |
| `col_offset` | `Pos() - lineStart(lineno)` in bytes |
| `end_lineno` | line of `End()` |
| `end_col_offset` | `End() - lineStart(end_lineno)` in bytes |

`End()` is the offset just past the last byte of the node. Because source is held as UTF-8 bytes, byte columns equal CPython's `col_offset` directly; no rune counting is needed. A leading UTF-8 BOM is stripped before offsets are assigned and CPython counts from after it, so offsets are relative to the stripped text. Encoding declarations (PEP 263) are handled by the file loader, which transcodes to UTF-8 and keeps offsets in the transcoded text, matching CPython's behavior of reporting positions in the UTF-8 re-encoding.

Helper: `ast.Location(fset, node) (lineno, col, endLineno, endCol int)`.

## ast: Node Set

Node set is 1:1 with CPython 3.14 `ast` (`Parser/Python.asdl`). Category interfaces:

| CPython category | Go interface | Members (examples) |
|---|---|---|
| `mod` | `ast.Mod` | `Module`, `Interactive`, `Expression`, `FunctionType` |
| `stmt` | `ast.Stmt` | `FunctionDef`, `AsyncFunctionDef`, `ClassDef`, `Return`, `Delete`, `Assign`, `TypeAlias`, `AugAssign`, `AnnAssign`, `For`, `AsyncFor`, `While`, `If`, `With`, `AsyncWith`, `Match`, `Raise`, `Try`, `TryStar`, `Assert`, `Import`, `ImportFrom`, `Global`, `Nonlocal`, `Expr`, `Pass`, `Break`, `Continue` |
| `expr` | `ast.Expr` | `BoolOp`, `NamedExpr`, `BinOp`, `UnaryOp`, `Lambda`, `IfExp`, `Dict`, `Set`, `ListComp`, `SetComp`, `DictComp`, `GeneratorExp`, `Await`, `Yield`, `YieldFrom`, `Compare`, `Call`, `FormattedValue`, `Interpolation`, `JoinedStr`, `TemplateStr`, `Constant`, `Attribute`, `Subscript`, `Starred`, `Name`, `List`, `Tuple`, `Slice` |
| `pattern` | `ast.Pattern` | `MatchValue`, `MatchSingleton`, `MatchSequence`, `MatchMapping`, `MatchClass`, `MatchStar`, `MatchAs`, `MatchOr` |
| `type_param` | `ast.TypeParam` | `TypeVar`, `ParamSpec`, `TypeVarTuple` |
| helpers (no category) | concrete | `Comprehension`, `ExceptHandler`, `Arguments`, `Arg`, `Keyword`, `Alias`, `WithItem`, `MatchCase`, `TypeIgnore` |

All interfaces embed `ast.Node`:

```go
type Node interface {
    Pos() token.Pos // first byte
    End() token.Pos // one past last byte
}
```

Marker methods (`stmtNode()`, `exprNode()`, ...) seal each category. As in CPython's ASDL, position attributes exist on `stmt`, `expr`, `pattern`, `type_param`, `ExceptHandler`, `Arg`, `Keyword` and `Alias`. `Comprehension`, `Arguments`, `WithItem`, `MatchCase` and `TypeIgnore` carry no positions and are not given `ast.Location`.

## Mapping Conventions

| CPython | Go |
|---|---|
| Node class `BinOp` | struct `ast.BinOp`; always used as `*ast.BinOp` |
| Field `snake_case` | exported `CamelCase`: `orelse`→`OrElse`, `finalbody`→`FinalBody`, `kw_defaults`→`KwDefaults`, `type_comment`→`TypeComment`, `type_params`→`TypeParams` |
| Position attributes | embedded `ast.Span{StartPos, EndPos token.Pos}` giving `Pos()`/`End()` |
| `identifier` | `string` |
| `string` | `string` |
| `int` (flags, `level`, `kind`-style ints) | `int` |
| `constant` | `ast.Constant.Value` of sealed interface `ast.Value`: `ast.Int` (arbitrary precision via `*big.Int`), `ast.Float`, `ast.Complex`, `ast.Str`, `ast.Bytes`, `ast.Bool`, `ast.NoneValue`, `ast.EllipsisValue` |
| Optional field `expr?` | interface or pointer field, `nil` when absent |
| Sequence `expr*` | slice, `nil` and empty are equivalent; `Dump` prints `[]` for both |
| Expression context `expr_context` | enum `ast.ExprContext`: `Load`, `Store`, `Del` |
| Operators | enum types, below |

### Operator enums

CPython's `operator`, `unaryop`, `cmpop`, `boolop` are separate ASDL sum types. Each becomes a small enum so nodes are precisely typed and `Dump` can print the singleton class name (`Add()`):

| Go type | Values |
|---|---|
| `ast.Operator` | `Add Sub Mult MatMult Div Mod Pow LShift RShift BitOr BitXor BitAnd FloorDiv` |
| `ast.UnaryOperator` | `Invert Not UAdd USub` |
| `ast.CmpOperator` | `Eq NotEq Lt LtE Gt GtE Is IsNot In NotIn` |
| `ast.BoolOperator` | `And Or` |

Example: `ast.BinOp{Left Expr; Op Operator; Right Expr}`. Operator-to-token mapping lives in `ast` helper tables, not in the parser.

### Metadata

A single generated table `ast/schema.go` (from a checked-in description of `Python.asdl`) lists, per node: Python class name, field names in order, field kind (node, node list, optional, identifier, constant, int, enum). `Dump`, `Walk`, and `Inspect` are written against this table so the three cannot drift. A test asserts the table against `ast.__dict__`-derived data produced by `internal/cpython` (nodes, field order).

## Walk and Inspect

`ast.Walk(v Visitor, n Node)` and `ast.Inspect(n, func(Node) bool)` follow go/ast. Child order is the field order in the ASDL, matching CPython `ast.iter_child_nodes`.

## Dump

`ast.Dump(n Node, opts DumpOptions) string` with `DumpOptions{Indent *string; IncludeAttributes bool; AnnotateFields bool; ShowEmpty bool}` mirroring Python 3.14's `ast.dump(node, annotate_fields=True, include_attributes=False, *, indent=None, show_empty=False)`.

The golden contract (ADR 0003, docs/testing.md) is byte-for-byte equality with:

```python
ast.dump(tree, include_attributes=True, indent=1)
```

Rules to reproduce exactly: fields in ASDL order; `name=value` form; optional `None` fields omitted when `show_empty` is false per 3.14 semantics; empty lists omitted likewise; string and bytes reprs follow Python `repr` quoting rules; `Constant` values printed with `repr` (`1`, `1.5`, `'a'`, `b'a'`, `True`, `None`, `Ellipsis`, `(1+2j)`, big ints in decimal); `kind=None` omitted per show_empty; attribute order `lineno, col_offset, end_lineno, end_col_offset`. Any divergence found against 3.14 is fixed in `Dump`, never normalized in the golden.

## Scanner

- Input: `[]byte` + `*token.File`. Output: tokens via `Scan() (pos token.Pos, tok token.Token, lit string)`.
- Indentation stack with tab size 8 and CPython's TabError rules; implicit line joining inside brackets; backslash continuation; `NEWLINE` vs `NL` handling; form feed reset.
- PEP 701: f-string and t-string are tokenized as `*_START`, `*_MIDDLE`, `*_END` with nested expression tokens using a mode stack; `{{`/`}}` escapes and `!r`/`:spec` handled in the scanner.
- Number literals: underscores, hex/octal/binary, imaginary suffix, exponent; error messages follow CPython where tests assert them.
- Errors are reported through an `ErrorHandler`; scanning continues after recoverable errors.

## Parser

- Hand-written recursive descent with explicit precedence climbing for expressions, following `Grammar/python.gram` (3.14). Rule names in code mirror grammar rule names (`parseStarTargets`, `parseSlices`) for traceability.
- Backtracking is bounded: lookahead tokens with `mark/reset` only where the PEG grammar needs it (e.g., distinguishing assignment targets, `match` soft keyword, lambda/default parameter forms, `with (...)`). Memoization is added only if profiling shows need.
- Context fixup: expression nodes parsed as `Load` are converted to `Store`/`Del` by a `setContext` pass on assignment, `for`, `with`, `del` targets, with CPython's error messages for invalid targets.
- Entry points: `ParseFile(fset, filename, src, mode) (*ast.Module, error)`, `ParseExpr`. Modes: `exec`, `eval`, `single`, `func_type`.
- Error recovery: on a statement-level syntax error, skip to the next `NEWLINE`/`DEDENT` boundary and continue so multiple errors are reported; the returned `error` is an `ErrorList` sorted by position. Messages follow CPython 3.14 wording for the common cases ("invalid syntax", "expected ':'", "unterminated string literal", "unindent does not match"); exact wording equality is tracked per-case in the syntax-error corpus.
- `__future__` and encoding cookie handling are performed by the file loader, not the parser.

## Naming Conventions (Go)

- Constructors only where invariants exist; nodes are plain structs with exported fields.
- No generics or reflection in hot paths; `Walk` uses a type switch generated from the schema.
- Parser methods `parseXxx` return `ast.Expr`/`ast.Stmt`; scanner methods are unexported except `Scan` and `Init`.
