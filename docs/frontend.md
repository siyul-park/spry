# Frontend: Tokens, AST, Parser

Packages: `token`, `scanner`, `ast`, `parser`. They import nothing else in spry.

## token

`token` follows `go/token` (file layout, symbol shape) with Python 3.14 vocabulary from CPython's `Grammar/Tokens` and `Lib/keyword.py`. Its tests cross-check the vocabulary against `token/testdata/cpython/tables.json`.

- `token.Token` is an enum of token kinds in `token/token.go`, in range-ordered groups: `ILLEGAL`, `EOF` (CPython `ENDMARKER`), `COMMENT`; literals `NAME`, `NUMBER`, `STRING`, `FSTRING_START`/`MIDDLE`/`END`, `TSTRING_START`/`MIDDLE`/`END`; layout `NEWLINE`, `NL`, `INDENT`, `DEDENT` (in no range); operators and delimiters (every CPython exact token); the 35 hard keywords.
- Operator names follow `go/token` where an equivalent exists (`ADD`, `SUB`, `MUL`, `QUO`, `REM`, `SHL`, `SHR`, `EQL`, `NEQ`, `LSS`, `LEQ`, `GTR`, `GEQ`, `ASSIGN`, `DEFINE` for `:=`, `LPAREN`, `LBRACK`, `LBRACE`, ...). Python-only or clashing names: `POW` (`**`), `FLOOR_QUO` (`//`), `AT`, `TILDE`, `EXCLAMATION`, `ARROW`, `ELLIPSIS`, `BIT_AND`/`BIT_OR`/`BIT_XOR` (so `AND`/`OR` name the keywords), and the matching `*_ASSIGN` kinds.
- `String()` returns the source spelling of operators and keywords (`"+"`, `"def"`) and the name of other kinds (`"NAME"`); an unknown kind prints `token(N)`.
- `IsLiteral`, `IsOperator`, `IsKeyword` test contiguous kind ranges. `Precedence()` covers binary arithmetic and bitwise operators only (`|` 1, `^` 2, `&` 3, shifts 4, `+ -` 5, `* @ / // %` 6, with `UnaryPrec` and `HighestPrec` above); `**`, comparisons and `not`/`and`/`or` are grammar rules in the parser and report `LowestPrec`.
- `Lookup(ident)` returns a keyword kind or `NAME`. Hard keywords are token kinds. Soft keywords (`match`, `case`, `type`, `_`) scan as `NAME`; `IsSoftKeyword` identifies them and the parser decides contextually.
- `IsIdentifier(name)` checks the PEP 3131 shape (not a hard keyword; soft keywords are identifiers) using Go's `unicode` tables as an approximation of `XID_Start`/`XID_Continue`. The exact Python tables and NFKC handling live in the scanner's generated tables.
- `token.Pos` is an `int` offset into a `token.FileSet` (go/token style); `0` is `NoPos`. `FileSet.Position(pos)` returns `{Filename, Offset, Line, Column}`. `File` records line start offsets, so `Pos` stays one word per node, and is safe for concurrent use. Out-of-range arguments panic as programmer errors.
- `Position.Column` is 1-based **bytes**, so CPython's `col_offset` is `Column-1`. CPython `SyntaxError` offsets count code points and are converted outside `token`.

Deviations from `go/token`: no line directives (`AddLineInfo`, `PositionFor`), no `Iterate`, `RemoveFile` or serialization; additions are `IsSoftKeyword`, the f-/t-string, layout and `COMMENT` kinds, and Python precedences.

## Positions

CPython attributes: `lineno` (1-based), `col_offset` (0-based, **UTF-8 bytes**), `end_lineno`, `end_col_offset`. spry stores `token.Pos` offsets (bytes into the source, which is UTF-8 after decoding) and converts on demand:

| CPython attribute | Derivation |
|---|---|
| `lineno` | line of `Pos()` via `File.Line` |
| `col_offset` | `Pos() - lineStart(lineno)` in bytes |
| `end_lineno` | line of `End()` |
| `end_col_offset` | `End() - lineStart(end_lineno)` in bytes |

`End()` is the offset just past the last byte of the node. Because source is held as UTF-8 bytes, byte columns equal CPython's `col_offset` directly; no rune counting is needed. A leading UTF-8 BOM is stripped before offsets are assigned and CPython counts from after it, so offsets are relative to the stripped text. Encoding declarations (PEP 263) are handled by `scanner.Decode`, which transcodes to UTF-8 so offsets are in the transcoded text, matching CPython's behavior of reporting positions in the UTF-8 re-encoding.

Helper: `ast.Locate(fset, node) ast.Location`.

## ast

`ast` follows `go/ast` (file layout, symbol shape) with CPython 3.14.9 behavior: the node set, field order and `Dump` output of `Parser/Python.asdl` and `Lib/ast.py` `ast.dump`. It is three hand-written files, `ast.go` (nodes, enums, values, `Locate`), `walk.go` and `dump.go`, plus the generated `tables.go`. `ast` imports only `token`.

### Nodes

One struct per ASDL constructor, always used as a pointer, with fields in ASDL order. Category interfaces embed `ast.Node` and carry an unexported marker method (`stmtNode()`, ...) as in `go/ast`:

| CPython category | Go | Members |
|---|---|---|
| `mod` | `ast.Mod` | `Module`, `Interactive`, `Expression`, `FunctionType` |
| `stmt` | `ast.Stmt` | `FunctionDef` ... `Continue`; the `Expr` statement is `ExprStmt` |
| `expr` | `ast.Expr` | `BoolOp` ... `Slice` |
| `pattern` | `ast.Pattern` | `MatchValue` ... `MatchOr` |
| `type_param` | `ast.TypeParam` | `TypeVar`, `ParamSpec`, `TypeVarTuple` |
| `excepthandler`, `type_ignore` | `*ast.ExceptHandler`, `*ast.TypeIgnore` | one constructor each, so no interface |
| product types | `*ast.Comprehension`, `*ast.Arguments`, `*ast.Arg`, `*ast.Keyword`, `*ast.Alias`, `*ast.WithItem`, `*ast.MatchCase` | |

```go
type Node interface {
    Pos() token.Pos // first byte
    End() token.Pos // one past the last byte
}
```

Nodes with CPython location attributes (`lineno`, `col_offset`, `end_lineno`, `end_col_offset`) embed `ast.Span{StartPos, EndPos token.Pos}`, which provides `Pos` and `End`. Nodes without them are still `Node`s, as `go/ast` derives positions from fields: `Module`, `Interactive`, `Expression`, `FunctionType`, `Arguments`, `Comprehension`, `WithItem` and `MatchCase` report the smallest start and largest end of their children, or `NoPos` when empty (so keywords such as `for` and `case` are not included); `TypeIgnore` reports `NoPos`.

`ast.Locate(fset, node)` returns `ast.Location{Lineno, ColOffset, EndLineno, EndColOffset}` from the span: lines are 1-based, columns are `Position.Column-1` in UTF-8 bytes, and the result is zero for a node without a valid position.

### Field mapping

| CPython | Go |
|---|---|
| Field `snake_case` | exported name; a `py:"<asdl name>"` struct tag when the lower-camel Go name differs (`OrElse` is `orelse`, `ID` is `id`, `TypeComment` is `type_comment`) |
| `identifier`, `string` | `string` |
| `identifier?` / `string?` (`ImportFrom.Module`, `Alias.AsName`, `Keyword.Arg`, `ExceptHandler.Name`, `MatchAs.Name`, `MatchStar.Name`, `MatchMapping.Rest`, `type_comment`, `Constant.Kind`) | `*string`: `nil` is `None`, a pointer to `""` is the empty string |
| `int` (`simple`, `conversion`, `is_async`, `TypeIgnore.lineno`) | `int` |
| `int?` (`ImportFrom.level`) | `*int`: `nil` is `None`, `0` prints as `level=0` |
| `expr?`, `pattern?`, `arg?`, `arguments` | interface or pointer, `nil` when absent |
| `expr*` etc. | slice; `nil` and empty are equal. The `Dict` keys of `{**a}` are `nil` elements, printed `None` |
| `constant`, `MatchSingleton.value` | `ast.Value` (below) |
| `Interpolation.str` | `string` |

### Operators and values

`operator`, `unaryop`, `cmpop`, `boolop` and `expr_context` are int enums whose `String()` is the CPython class name. Their constants carry a prefix so they cannot clash with node types (`Mod`, `Not`, `In`): `OpAdd` ... `OpFloorDiv`, `UnaryInvert`, `UnaryNot`, `UnaryUAdd`, `UnaryUSub`, `CmpEq` ... `CmpNotIn`, `BoolAnd`, `BoolOr`, `CtxLoad`, `CtxStore`, `CtxDel`. Zero values are `OpAdd`, `UnaryInvert`, `CmpEq`, `BoolAnd` and `CtxLoad` (CPython's default context); an out-of-range value prints `Operator(N)`.

`Constant.Value` is a sealed `ast.Value`: `Int{N *big.Int}`, `Float`, `Complex`, `Str`, `Bytes`, `Bool`, `None`, `Ellipsis`. `Constant.Kind` is `*string`, `"u"` or `nil`. `Str` is **WTF-8**: UTF-8 in which a lone surrogate (`"\ud800"`) is its three-byte form. `ast.AppendRune(dst, r)` builds one from code points; Python never joins two escaped surrogates, so neither does `Str`.

### Deviations

- From `go/ast`: no `File`, `Scope` or `Object`; `Locate` returns a struct (four results exceed the signature-size rule); `Dump` replaces `Print`/`Fprint` and takes the `FileSet` first, as `Fprint` does.
- From CPython: operators and expression contexts are enums, not nodes, so `Walk` never visits them (`iter_child_nodes` yields their singleton instances); the `Expr` statement is `ExprStmt` because `Expr` is the expression interface; `slice`, the deprecated class, has no counterpart.

### Metadata

Nodes are hand-written; there is no ASDL generator (ADR 0009). `ast_test.go` loads `ast/testdata/cpython/schema.json`, extracted from CPython, and checks by reflection that the node structs are exactly the schema nodes (operators aside), that every struct's fields match the ASDL names, order and types, that `Span` is present exactly when the node has attributes, and that each node implements exactly its category. `Walk` is a hand-written type switch, checked by a test that fills every child field of every node. The printable table behind `repr` is generated by `ast/gen_tables.go` (`go generate ./ast`) from `scanner/testdata/cpython/unicode.json`.

## Walk and Inspect

`ast.Walk(v Visitor, n Node)`, `ast.Inspect(n, func(Node) bool)` and `ast.Preorder(n) iter.Seq[Node]` follow `go/ast`. Children are visited in ASDL field order, as `ast.iter_child_nodes` does, skipping absent children and `nil` `Dict` keys.

## Dump

```go
func Dump(fset *token.FileSet, node Node, opts ...DumpOption) string
func WithIndent(indent string) DumpOption // indent=; an integer n is strings.Repeat(" ", n)
func WithAttributes() DumpOption          // include_attributes=True; needs fset
func WithEmpty() DumpOption               // show_empty=True
func WithoutFieldNames() DumpOption       // annotate_fields=False
```

The defaults are those of 3.14's `ast.dump(node, annotate_fields=True, include_attributes=False, *, indent=None, show_empty=False)`, and the loop is its `_format` line for line. Notable behavior: a `None` field is always omitted (even with `show_empty`), and it makes later fields named; with `show_empty` off an empty list is omitted, and without field names it is held back and printed only if a later field prints unnamed; a node with at most three simple arguments stays on one line even with an indent; `ctx=Load()` and `op=Add()` always print; `Constant(value=None)` and `MatchSingleton(value=None)` print the value. Helper nodes print their lower-case class names (`arguments`, `arg`, `keyword`, `alias`, `withitem`, `match_case`, `comprehension`) and `ExprStmt` prints `Expr`. Attributes are omitted for a node with no start position, and the end pair when there is no end.

Constants use Python `repr`: shortest round-trip floats (`1e+16`, `1e-05`, `-0.0`, `inf`, `nan`), complex per `complex_repr` (`1j`, `(1+2j)`, `(-0-2.5j)`, `infj`), `str` with quote choice and `\x`/`\u`/`\U` escapes of non-printable characters (the generated table), `bytes` with `\x` escapes. All 5125 cases of `testdata/cpython/repr.jsonl` are compared in `TestDump`, along with hand-built trees checked against `python3.14`'s `ast.dump`.

### AST goldens

The end-to-end goldens are `parser/testdata/ast/*.py` with `.ast`, produced by `ast.dump(tree, include_attributes=True, indent=1)` and compared with `ast.Dump(fset, tree, ast.WithAttributes(), ast.WithIndent(" "))` once `parser` lands (docs/testing.md). `ast/testdata/` holds only the schema and repr oracle data.

## Scanner

`scanner` follows `go/scanner` for its API shape and CPython 3.14's tokenizer (`Parser/lexer/lexer.c`, `state.c`, `Parser/tokenizer/helpers.c`) for behavior: token boundaries, indentation rules and error messages are CPython's. It is three source files: `scanner.go` (scanner, options, `Decode`), `errors.go`, and the generated `tables.go`.

```go
func New(file *token.File, src []byte, opts ...Option) *Scanner
func (s *Scanner) Scan() (pos token.Pos, tok token.Token, lit string)
func (s *Scanner) Err() error
func WithExtraTokens() Option
func WithoutImplicitNewline() Option
func Decode(src []byte) ([]byte, error)
```

- Construction is initialization (`New` instead of `go/scanner.Init`) and panics, like `Init`, if `file.Size() != len(src)`. The scanner records line starts into `file` as it reads each line.
- Input is `[]byte`, not `string`: sources come from files, `Decode` returns bytes, and tokens are located by `token.Pos` offsets. `src` must be UTF-8 (the output of `Decode`); invalid bytes outside names never panic but are not diagnosed.
- Default options give the stream the parser consumes (CPython's C tokenizer). `WithExtraTokens` gives the stream of CPython's `tokenize` module (`tok_extra_tokens`): it adds `COMMENT` and `NL` (blank lines, comment-only lines, newlines inside brackets) and switches on that module's leniency: numbers and names are not verified (`1abc`, `0123`, `a€` scan), unmatched or mismatched brackets are not errors. Indentation, string and f-string errors are raised in every mode.
- `WithoutImplicitNewline` keeps the scanner from appending the `NEWLINE` it normally supplies when the source does not end in a line break. The parser uses it in single mode, where CPython's tokenizer does not add one; a last line of only white space is then judged by CPython's rules (`IndentationError: unindent does not match` versus accepted) instead of the implicit newline. It affects only that case.

### Tokens

- `lit` is the exact source text of the token and its end is `pos + len(lit)`. Documented exceptions: `INDENT` carries the indentation; `DEDENT` and `EOF` have no width and sit at the next token (`EOF`: the end of the source); `NEWLINE` and `NL` carry the line break (`\n` or `\r\n`), and the implicit `NEWLINE` at the end of a source without a final line break has `lit == ""` and sits at `len(src)`; `FSTRING_MIDDLE` and `TSTRING_MIDDLE` for `{{` and `}}` carry one brace and end before the second, which the next token skips.
- Keywords are returned as their own kinds through `token.Lookup`; soft keywords (`match`, `case`, `type`, `_`) are `NAME`.
- A character that begins no token (`$`, `?`, a backtick) is `ILLEGAL` with its text and no error, as CPython hands it to the parser as an `OP` it rejects.
- Line breaks are `\n` and `\r\n`. A lone `\r` is not a line break, as in CPython's file tokenizer; `compile()` translates it before tokenizing, so callers that want that translate first.
- `INDENT`/`DEDENT` follow CPython's dual stacks (tab size 8, alternate tab size 1) so that a source whose meaning depends on the tab size is a `TabError`. Open brackets (200 at most), indentation levels (100) and f-string nesting (150) have CPython's limits.
- f-strings and t-strings (PEP 701, PEP 750) use a mode stack: `FSTRING_START` or `TSTRING_START`, literal `*_MIDDLE` text, the replacement fields as ordinary tokens (`{`, expression, `!`, conversion name, `:`, nested format spec, `}`), and `*_END`. An empty `*_MIDDLE` can follow a nested field inside a format spec, exactly as CPython emits it.

### Errors

The scanner does not take an error handler. `go/scanner`'s `ErrorHandler` is a callback the caller must supply behavior for; every tokenizer error here is fatal (CPython stops at the first), so `Err()` returns the accumulated `ErrorList.Err()` instead, `nil` if clean. After an error `Scan` returns `ILLEGAL` once, covering the offending text, and `EOF` from then on, so callers never loop forever. `INDENT` and `DEDENT` are balanced whenever `Err()` is `nil`.

`scanner.Error{Pos, End, Msg, Err}` locates the offending text (`End` exclusive; byte columns, as in `token`) and carries a category sentinel: `ErrSyntax`, `ErrIndentation` (`unindent does not match any outer indentation level`, `too many levels of indentation`) and `ErrTab` (`inconsistent use of tabs and spaces in indentation`), nested as in CPython: `errors.Is(tabErr, ErrSyntax)` is true. CPython's `SyntaxError.offset` is `Pos.Column` and `end_offset` is `End.Column-1`, both in code points, converted by the consumer. Messages are CPython's, for example `invalid decimal literal`, `invalid digit '8' in octal literal`, `leading zeros in decimal integer literals are not permitted; use an 0o prefix for octal integers`, `invalid character '€' (U+20AC)`, `invalid non-printable character U+00A0`, `unterminated string literal (detected at line N)`, `unterminated triple-quoted string literal (detected at line N)`, `'(' was never closed`, `unmatched ')'`, `closing parenthesis ')' does not match opening parenthesis '['` (with "on line N" appended when the brackets are on different lines), `unexpected character after line continuation character`, `unexpected EOF while parsing`, `f-string: expecting '}'`, `f-string: single '}' is not allowed`, and the t-string counterparts. A keyword right after a number (`1if x else 2`) is accepted, where CPython only warns.

### Decode

`Decode(src)` is what CPython does before tokenizing: it drops a UTF-8 byte order mark, finds a PEP 263 cookie on the first or second line (the `get_coding_spec` rules, including that a code line ends the search) and transcodes with `golang.org/x/text` to UTF-8, leaving line endings alone. Source without a cookie, or with a `utf-8` cookie, must be valid UTF-8. Errors are `*scanner.Error` with `ErrSyntax` and CPython's messages: `unknown encoding: X`, `encoding problem: X with BOM`, `Non-UTF-8 code starting with '\xf6' on line 1, but no encoding declared; see https://peps.python.org/pep-0263/ for details`, and the codec messages (`'ascii' codec can't decode byte 0xe9 in position 19: ordinal not in range(128)`). Positions are relative to the text after the byte order mark.

### Generated tables

`scanner/tables.go` holds `*unicode.RangeTable` values for `XID_Start` (plus `_`), `XID_Continue` and the printable characters of Python's `unicodedata` (Unicode 16.0.0), generated by `scanner/gen_tables.go` (`go generate ./scanner`) from `scanner/testdata/cpython/unicode.json`. Names with non-ASCII characters are checked with them rather than with Go's `unicode` tables, which differ in version and in the `XID` rules. NFKC normalization of names is the parser's concern.

### Tests

The oracle is CPython 3.14 (docs/testing.md). The `tokenize` manifest cases are rendered from a scanner with both options in the dump format of `internal/cpython/driver/tokens.py`; the file cases compare the SHA-256 of that dump, and `testdata/cpython/xfail.txt` is empty. The `encoding` manifest checks `Decode` and scanner errors against `compile()`. `testdata/*.py` with `*.tokens` are hand-written goldens, regenerated with `go test ./scanner -update` and verified against `python3.14 -m tokenize` output when created. `FuzzScanner` checks termination, positions and balanced indentation.

## Parser

`parser` follows `go/parser` for its file layout (`interface.go`, `parser.go`) and CPython 3.14 (`Grammar/python.gram`, `Parser/action_helpers.c`, `Parser/pegen.c`, `Parser/string_parser.c`) for behavior: it builds trees that `ast.Dump(..., WithAttributes)` prints exactly as `ast.dump(tree, include_attributes=True)` does. It imports only `token`, `scanner`, `ast`, the standard library and `golang.org/x/text/unicode/norm`.

```go
func ParseFile(fset *token.FileSet, filename string, r io.Reader, opts ...Option) (*ast.Module, error)
func ParseExpr(fset *token.FileSet, filename string, r io.Reader, opts ...Option) (*ast.Expression, error)
func ParseInteractive(fset *token.FileSet, filename string, r io.Reader, opts ...Option) (*ast.Interactive, error)
func ParseFuncType(fset *token.FileSet, filename string, r io.Reader, opts ...Option) (*ast.FunctionType, error)
func WithAllErrors() Option
func WithTypeComments() Option
```

- One entry point per CPython compile mode (`exec`, `eval`, `single`, `func_type`), each reading the whole source from the `io.Reader` (read failures are wrapped with `%w`). The source is decoded with `scanner.Decode` (byte order mark and coding cookie, as for bytes passed to `ast.parse`), then `\r\n` and `\r` are translated to `\n` as CPython does before tokenizing, so all positions refer to the translated text. A NUL byte is a `SyntaxError` (`source code string cannot contain null bytes`, without a position, the wording of `compile` for `bytes` and `str`; running a file reports `source code cannot contain null bytes` with a line, which belongs to the file-running layer). The file is added to `fset`, the tokens are scanned eagerly into a slice, and the start rule runs on it.
- The error is a `scanner.ErrorList` of `*scanner.Error` (`errors.Is` with `scanner.ErrSyntax`, `ErrIndentation`, `ErrTab`); the tree is nil on error. Scanner errors take priority over parser errors, as in CPython, where the generic failure re-tokenizes the rest of the source and reports a tokenizer error found there. `WithAllErrors` returns the scanner error and the parser error together, sorted by position, instead of only the first; the parser still stops at its first error, so "all" is best effort.
- Error positions follow `scanner.Error`: `Pos` is the start (1-based byte column) and `End.Column-1` is CPython's `end_offset`, for every error including the parser's own; an error CPython reports without an end has `End.Column == 0`. Code point offsets are derived from the source at the boundary.
- A non-UTF-8 source is a decode error as `scanner.Decode` reports it. CPython adds `in file X on line N` when it runs a file; the parser has no such context and keeps the `compile(bytes)` message.
- `WithTypeComments` is accepted and does nothing yet: the parser stream carries no comments, so `type_comment` fields and `Module.TypeIgnores` stay empty. Deferred.

### Design

The parser is hand-written recursive descent, not a PEG interpreter. Where `python.gram` backtracks, the code decides by lookahead or by parsing a superset and validating, and the result is the same tree for every accepted input:

- Assignment, augmented assignment and annotation: the statement starts as `star_expressions`; a following `=`, augmented operator or `:` selects the form, and `setContext` turns the parsed expression into a `Store` (or `Del`) target, failing for nodes that are not targets. `for` and comprehension targets, `with ... as` and `except`-free targets use `primary` plus `setContext` instead of expression precedence, which also keeps `in` out of the target.
- Binary operators use precedence climbing with `token.Token.Precedence`; `**`, unary operators, comparisons, `not`, `and`, `or`, the conditional and `lambda` are grammar rules, as in `python.gram`.
- Real backtracking is limited to `speculate`: a `match` statement (falls back to an expression statement, so `match` and `case` stay ordinary names) and a parenthesized `with` item list (falls back to one parenthesized expression). `type` is a statement only when a name follows. No memoization was needed: the 1873 stdlib files parse in well under a second without `-race`, so the grammar's `(memo)` rules are not ported.
- A rule that fails panics with `softFailure`, recovered only by `speculate` and at the entry point; an error raised by an action (bad string escape, `cannot mix bytes and nonbytes literals`, invalid conversion, nesting too deep) panics with a `syntaxError` that nothing backtracks over, as with `RAISE_SYNTAX_ERROR` in pegen. `maxDepth` bounds the nesting of expressions, patterns and blocks like CPython's `MAXSTACK`; exceeding it is `Parser stack overflowed - Python source too complex to parse`.
- Two passes, as pegen's `_PyPegen_run_parser`: the first pass runs without the `invalid_*` rules. When it fails softly, the start rule reruns from the first token with `parser.callInvalid` set, and the `invalid_*` rules, written as hooks at the point of the recursive descent where the grammar tries them, raise CPython's specific errors (`expected ':'`, `cannot assign to ...`, `expected an indented block after ...`, `Perhaps you forgot a comma?`). A second pass that raises nothing yields `invalid syntax` at the furthest token examined in the first pass (`p->fill - 1`), or `unexpected indent` / `unindent does not match` when that token is an `INDENT` or `DEDENT`. `*_without_invalid` rules are `withoutInvalid`. In the second pass `expression` is memoized by position, as the grammar's `(memo)` rule, to avoid exponential reparsing under nesting.
- Scanner errors follow `tokenize_full_source_to_check_for_errors`: a scanner error is raised when the parser fetches the offending token, in either pass, and after a parser error the rest of the source is tokenized and its error wins, except an unclosed bracket (unless the parser read past its line), an unexpected end of file, and errors inside an f-string. Locations use CPython's macros: the last token read (`RAISE_SYNTAX_ERROR`), the current token, node ranges, or the tokenizer cursor for `INDENT`, `DEDENT` and the end marker.

### Positions

Node positions follow CPython's `EXTRA` macro: the start of the rule's first token to the end of the last token consumed that is not `NEWLINE`, `INDENT`, `DEDENT` or `ENDMARKER`. Consequences that differ from "start of the first child":

- A binary operation, attribute, call or subscript starts at the first token of its rule, so `(a).b` starts at the parenthesis while `a` keeps its own span; a parenthesized expression is the inner node unchanged, a parenthesized tuple and a generator expression include the parentheses.
- A tuple without parentheses ends at its trailing comma; a subscript tuple spans from its first element after `[`; `f(x for x in y)` spans the generator expression over the call's parentheses.
- A decorated function or class spans from `def`, `async` or `class`, not from its decorators. `elif` creates a nested `If` starting at `elif`. A statement ends at its last token, never at the `NEWLINE`.
- `Constant` nodes made from adjacent string literals span all of them. In f-strings a `FSTRING_MIDDLE` constant spans its token (a doubled brace belongs to the token before it), runs of adjacent constants merge into one spanning the first to the last, empty ones disappear, and the debug text of `{x=}` is a `Constant` spanning from after `{` to the `!`, `:` or `}` that ends the expression text. A format spec `JoinedStr` starts at its `:`.
- Columns are UTF-8 byte offsets (`ast.Locate` and `token.Position`), matching `col_offset`.

### Names, literals and strings

- Non-ASCII identifiers are normalized to NFKC (`norm.NFKC`), as `_PyPegen_new_identifier` does.
- Numbers: underscores are removed; `int` is arbitrary precision (`math/big`), decimal literals over 4300 digits fail with CPython's message, floats overflow to infinity, a `j` suffix makes a `Complex` with a zero real part.
- String literals are decoded as `_PyPegen_parse_string` does: raw, bytes (ASCII only) and `u` prefixes, all escapes of the `unicode_escape` codec including `\N{...}`, octal values above `0o377`, and lone surrogates kept as WTF-8 (`ast.AppendRune`). Invalid escapes are accepted (CPython only warns; warnings are not reported). `\N{...}` resolves names, aliases, Hangul syllables and the ideograph ranges from `names.txt.gz`, generated by `go generate ./parser` (`gen_names.go`, needs `python3.14` or `SPRY_PYTHON`) from `unicodedata`, so the Unicode version is CPython's (16.0.0); named sequences are rejected, as in `\N`. The alias list is a fixed set of control-character and common aliases, not the full `NameAliases.txt`.
- Implicit concatenation merges `str` with `str` and `bytes` with `bytes`; mixing is an error. An f-string present anywhere makes a `JoinedStr`; a t-string group makes a `TemplateStr`; mixing f/str with t is an error. `Constant.kind` is `u` when the first literal has a lowercase `u` prefix.
- f-strings and t-strings are parsed from the scanner's token stream: the replacement field expression is parsed by the ordinary expression rules, so inner nodes already carry source positions (no offset mapping). `FormattedValue` and `Interpolation` take the conversion character, the format spec (`JoinedStr`, also for t-strings, whose nested fields in a spec are `FormattedValue`) and, for `=`, a debug text constant with `conversion` `r` when no conversion or spec is given. The `Interpolation.str` and the debug text are the source text between `{` and the closing `!`, `:` or `}`, comments removed, as the tokenizer records it.

### `__future__`, single mode and other CPython details

- After parsing a module the leading `from __future__ import` names are validated (`future feature X is not defined`, `not a chance` for `braces`), as `ast.parse` does; placement errors are compile-stage in CPython and are not reported. `from __future__ import barry_as_FLUFL` switches `!=` off and accepts `<>` written as two adjacent tokens (the scanner has no `<>` token); both are minimal and untested beyond the corpus.
- Single mode follows `_PyPegen_fill_token`: the stream gets one extra `NEWLINE` before the end marker, which closes a compound statement and supplies the missing line break of an unterminated simple statement. A source with more than one statement is `multiple statements found while compiling a single statement`.
- `ParseExpr` and `ParseFuncType` accept trailing newlines; leading indentation is an error, as in `compile`.

### Tests

The oracle is CPython 3.14 (docs/testing.md). `testdata/cpython/snippets.jsonl`, `syntax.jsonl` and `files.jsonl` run in `TestCorpus_Snippets`, `TestCorpus_Syntax` and `TestCorpus_Files` (the last needs the cache and is skipped without it): accepted inputs must dump identically (whole files by SHA-256), inputs rejected at the `parse` stage must fail with a `SyntaxError` (exception type, message, `lineno`, `offset`, `end_lineno` and `end_offset`, with offsets in code points), and `compile`-stage rejections must parse and dump. `testdata/cpython/xfail.txt` is empty. One case, `test_source_encoding/call/0001`, is skipped in the test as a documented divergence: its source is a `str`, for which CPython ignores the coding cookie, while the parser reads bytes and honors the cookie as `compile` does for bytes. `testdata/errors/*.py` with `.err` are hand-written error goldens in the form `Type: msg (lineno, offset)-(end_lineno, end_offset)`, regenerated with `go test ./parser -update` and verified against `python3.14`. `testdata/ast/*.py` with `.ast` are hand-written goldens (statements, expressions with f-strings and t-strings, `match`, type parameters, non-ASCII positions), regenerated with `go test ./parser -update` and verified against `python3.14` when created. `FuzzParseFile` checks that parsing terminates without panics, that the dump is deterministic and that every node ends inside the file after it starts.

### Deferred

- Type comments (`# type:` and `# type: ignore`), the `Module.TypeIgnores` list and `func_type_comment`.
- Warnings (`SyntaxWarning` for invalid escapes, `is` with a literal), and `from __future__` placement errors.
- Compile-stage errors (for example `keyword argument repeated`) are not reported: `ast.parse` accepts them.

## Naming Conventions (Go)

- Constructors only where invariants exist; nodes are plain structs with exported fields.
- No generics or reflection in hot paths; `Walk` uses a hand-written type switch.
- Parser rules are methods named after the `python.gram` rule in lower camel case (`starTargets`, `forIfClauses`); scanner methods are unexported except `Scan` and `Err`.
