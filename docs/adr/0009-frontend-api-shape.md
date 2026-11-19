# 0009: Frontend API shape

- Status: Accepted

## Context

The frontend packages (`token`, `scanner`, `ast`, `parser`) need a public shape before code lands. Two references pull in different directions. Go's `go/token`, `go/scanner`, `go/ast` and `go/parser` give Go readers a familiar file layout and symbol vocabulary. CPython 3.14 (`Parser/lexer`, `Parser/pegen*`, `Parser/action_helpers.c`, `Parser/Python.asdl`) defines the behavior spry must match: token boundaries, AST shape, and error categories, messages and positions. The repo's coding rules (docs/coding-patterns.md) also differ from some Go standard library idioms: construction is initialization, optional settings are functional options, error categories are sentinels.

## Decision

Follow the Go standard library for file layout and symbol roles, CPython for behavior, and docs/coding-patterns.md where the two Go conventions conflict.

- Layout mirrors the Go packages: `token/token.go` and `position.go`; `scanner/scanner.go`, `errors.go`, generated `tables.go`; `ast/ast.go`, `walk.go`, `dump.go`; `parser/interface.go` and `parser.go`. Files are split by responsibility, never by size.
- `token` keeps `go/token` names (`Token`, `Pos`, `Position`, `File`, `FileSet`, `Lookup`, `IsKeyword`, `IsIdentifier`, `Precedence`) and adds `IsSoftKeyword`. `Position.Column` is 1-based bytes; CPython `col_offset` is `Column-1`, and SyntaxError code-point offsets are converted at the boundary.
- `scanner.New(file, src, opts...)` replaces `Init`; `WithExtraTokens` replaces `Mode`, and `WithoutImplicitNewline` lets single mode see a source that ends without a line break exactly as CPython's tokenizer does. Errors accumulate and are read with `Err()` instead of an `ErrorHandler` callback, since no consumer supplies behavior there. The input is `[]byte`: the file size is known up front, `Decode` already yields a buffer, and CPython's error rules re-scan the remaining source.
- `scanner.ErrSyntax`, `ErrIndentation` and `ErrTab` are sentinels; `*scanner.Error` carries start and end positions and implements `Is` for the CPython hierarchy (TabError within IndentationError within SyntaxError). `ErrorList` keeps the `go/scanner` shape and is also the parser's error type.
- `ast` nodes are hand-written structs in ASDL field order, sealed by category interfaces as in `go/ast`. A test compares them field by field with the CPython schema instead of generating them from ASDL. `Dump` reproduces `ast.dump` through functional options.
- `parser` has one entry point per CPython compile mode (`ParseFile`, `ParseExpr`, `ParseInteractive`, `ParseFuncType`), each taking `(fset, filename, io.Reader, opts...)`. `io.Reader` replaces `go/parser`'s `src any`, and options replace `Mode`.
- CPython internal names (`tok_state`, `_PyPegen_*`, `LPAR`) stay out of the public API; they appear only in tests and comments that cite the C source.

## Consequences

- Go readers find the expected files and names. CPython differences are concentrated in documented behavior, not in naming.
- No callbacks or mode bitmasks to configure; tests construct scanners and parsers in one call.
- Hand-written nodes need the schema test to stay in sync when the pinned CPython version changes.
- Deviations from `go/*` are recorded in docs/frontend.md next to each package.

## Alternatives

- Mirror CPython names and structure (`tok_state`, generated pegen parser). Rejected: unidiomatic in Go, and the C layout follows a code generator spry does not use.
- Copy `go/*` APIs verbatim (`Init`, `ErrorHandler`, `Mode`, `src any`). Rejected: these conflict with docs/coding-patterns.md and add surface no consumer needs.
- Generate AST nodes from ASDL. Rejected: a generator plus templates is more code than the nodes, and the schema test gives the same guarantee.
