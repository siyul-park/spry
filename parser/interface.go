package parser

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/scanner"
	"github.com/siyul-park/spry/token"
)

// Option configures a parse.
type Option func(*parser)

// failure describes how a pass ended.
type failure struct {
	kind failKind
	err  *syntaxError
}

type failKind int

const (
	failNone failKind = iota
	failSoft
	failSyntax
	failScanner
)

// WithAllErrors reports every scanner error found in addition to the parser
// error, instead of only the first error. The parser still stops at its first
// syntax error.
func WithAllErrors() Option {
	return func(p *parser) { p.allErrors = true }
}

// WithTypeComments requests "# type:" comments in the tree. It is accepted
// and has no effect yet: the scanner does not emit comments in the parser
// stream, so type_comment fields stay empty and TypeIgnores is nil.
func WithTypeComments() Option {
	return func(p *parser) { p.typeComments = true }
}

// ParseFile parses a module (CPython's exec mode). The error is a
// scanner.ErrorList whose items are *scanner.Error.
func ParseFile(fset *token.FileSet, filename string, r io.Reader, opts ...Option) (*ast.Module, error) {
	return parse(fset, filename, r, opts, (*parser).module)
}

// ParseExpr parses one expression (CPython's eval mode).
func ParseExpr(fset *token.FileSet, filename string, r io.Reader, opts ...Option) (*ast.Expression, error) {
	return parse(fset, filename, r, opts, (*parser).eval)
}

// ParseInteractive parses one interactive statement (CPython's single mode).
func ParseInteractive(fset *token.FileSet, filename string, r io.Reader, opts ...Option) (*ast.Interactive, error) {
	return parse(fset, filename, r, slices.Concat(opts, []Option{func(p *parser) { p.single = true }}), (*parser).interactive)
}

// ParseFuncType parses a function type comment signature (CPython's
// func_type mode), such as "(int, str) -> bool".
func ParseFuncType(fset *token.FileSet, filename string, r io.Reader, opts ...Option) (*ast.FunctionType, error) {
	return parse(fset, filename, r, opts, (*parser).funcType)
}

func parse[M ast.Mod](fset *token.FileSet, filename string, r io.Reader, opts []Option, start func(*parser) M) (M, error) {
	var zero M
	raw, err := io.ReadAll(r)
	if err != nil {
		return zero, fmt.Errorf("parser: read %s: %w", filename, err)
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		var errs scanner.ErrorList
		errs.Add(token.Position{Filename: filename}, token.Position{Filename: filename}, "source code string cannot contain null bytes", scanner.ErrSyntax)
		return zero, errs
	}
	src, err := scanner.Decode(raw)
	if err != nil {
		var decodeErr *scanner.Error
		if errors.As(err, &decodeErr) {
			return zero, scanner.ErrorList{decodeErr}
		}
		return zero, err
	}
	src = translateNewlines(src)
	p := &parser{fset: fset, src: src, errIdx: -1}
	for _, opt := range opts {
		opt(p)
	}
	p.file = fset.AddFile(filename, -1, len(src))
	var scanOpts []scanner.Option
	if p.single {
		scanOpts = append(scanOpts, scanner.WithoutImplicitNewline())
	}
	sc := scanner.New(p.file, src, scanOpts...)
	for {
		pos, kind, lit := sc.Scan()
		p.toks = append(p.toks, tok{kind: kind, pos: pos, end: tokenEnd(p.file, src, pos, kind, lit), lit: lit})
		if kind == token.EOF {
			break
		}
	}
	if sc.Err() != nil {
		var list scanner.ErrorList
		errors.As(sc.Err(), &list)
		p.scanErr = list[0]
		p.errIdx = len(p.toks) - 2
	}
	if p.single {
		p.toks = singleTokens(p.toks, src)
	}
	p.levels = tokenLevels(p.toks)
	mod, errs := run(p, start)
	if len(errs) > 0 {
		return zero, errs
	}
	return mod, nil
}

// tokenLevels returns the bracket nesting of each token as CPython's
// tokenizer reports it: an opening bracket counts itself, a closing one does
// not.
func tokenLevels(toks []tok) []int {
	levels := make([]int, len(toks))
	level := 0
	for i, t := range toks {
		switch t.kind {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			level++
		case token.RPAREN, token.RBRACK, token.RBRACE:
			level = max(level-1, 0)
		}
		levels[i] = level
	}
	return levels
}

// run executes the start rule as pegen's _PyPegen_run_parser does: a first
// pass without the invalid_* rules and, when it fails, a second pass with
// them. It returns the tree, or the errors with the error CPython reports
// first.
func run[M ast.Mod](p *parser, start func(*parser) M) (mod M, errs scanner.ErrorList) {
	mod, f := attempt(p, start)
	if f.kind == failNone {
		return mod, nil
	}
	if f.kind == failSoft {
		last := min(p.reached, len(p.toks)-1)
		p.callInvalid, p.pos, p.depth = true, 0, 0
		p.memoize, p.memo = true, map[memoKey]memoEntry{}
		_, f = attempt(p, start)
		if f.kind == failSoft || f.kind == failNone {
			f = p.generic(last)
		}
	}
	if f.kind == failScanner {
		return mod, scanner.ErrorList{p.scanErr}
	}
	perr := p.errorAt(f)
	if over := p.tokenizeRest(); over != nil {
		errs = scanner.ErrorList{over}
		if p.allErrors {
			errs = append(errs, perr)
			errs.Sort()
		}
		return mod, errs
	}
	return mod, scanner.ErrorList{perr}
}

// attempt runs one pass of the start rule, converting a panic into a failure.
func attempt[M ast.Mod](p *parser, start func(*parser) M) (mod M, f failure) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		switch v := r.(type) {
		case softFailure:
			f = failure{kind: failSoft}
		case scanFailure:
			f = failure{kind: failScanner}
		case *syntaxError:
			f = failure{kind: failSyntax, err: v}
		default:
			panic(r)
		}
	}()
	return start(p), failure{}
}

// translateNewlines converts "\r\n" and "\r" to "\n" as CPython does before
// tokenizing a string, so all positions refer to the translated text.
func translateNewlines(src []byte) []byte {
	if bytes.IndexByte(src, '\r') < 0 {
		return src
	}
	src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(src, []byte("\r"), []byte("\n"))
}

// singleTokens adds the NEWLINE that CPython's parser inserts at the end of
// the token stream in single mode (_PyPegen_fill_token), where the end marker
// becomes a NEWLINE. A source that does not end in a line break gets no
// dedents from the tokenizer, so the parser supplies the ones that close the
// open blocks after it.
func singleTokens(toks []tok, src []byte) []tok {
	n := len(toks)
	if n < 2 {
		return toks
	}
	eof := toks[n-1]
	depth := 0
	for _, t := range toks {
		switch t.kind {
		case token.INDENT:
			depth++
		case token.DEDENT:
			depth--
		}
	}
	newline := tok{kind: token.NEWLINE, pos: eof.pos, end: eof.pos, synthetic: true}
	out := append(toks[:n-1:n-1], newline)
	if terminated := len(src) > 0 && src[len(src)-1] == '\n'; !terminated && depth > 0 {
		for ; depth > 0; depth-- {
			out = append(out, tok{kind: token.DEDENT, pos: eof.pos, end: eof.pos, synthetic: true})
		}
		out = append(out, newline)
	}
	return append(out, eof)
}

// tokenEnd returns the end of a token. A literal part of an f-string that
// ends at a doubled brace spans both braces although its text holds one.
func tokenEnd(file *token.File, src []byte, pos token.Pos, kind token.Token, lit string) token.Pos {
	end := pos + token.Pos(len(lit))
	if (kind == token.FSTRING_MIDDLE || kind == token.TSTRING_MIDDLE) && lit != "" {
		off, last := int(end)-file.Base(), lit[len(lit)-1]
		if (last == '{' || last == '}') && off < len(src) && src[off] == last && !strings.Contains(lit, "\\N{") {
			end++
		}
	}
	return end
}
