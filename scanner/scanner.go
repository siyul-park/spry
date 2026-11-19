package scanner

//go:generate go run gen_tables.go

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/encoding/ianaindex"

	"github.com/siyul-park/spry/token"
)

// Scanner turns UTF-8 Python source into tokens. Use Decode first when the
// source comes from a file whose encoding is not known to be UTF-8.
//
// Scanner follows CPython 3.14's tokenizer (Parser/lexer/lexer.c): token
// boundaries, indentation rules and error messages are the same.
type Scanner struct {
	file *token.File
	src  []byte
	errs ErrorList

	extra bool // CPython's tok_extra_tokens: see WithExtraTokens
	exact bool // no implicit final line break: see WithoutImplicitNewline

	end       int // len(src), plus one for the implicit newline of a last line without one
	off       int // next byte to read; len(src)+1 once the implicit newline is read
	loaded    int // offset after the last line read
	lineno    int // number of lines read, CPython's tok->lineno
	lineStart int // offset of the line read last
	start     int // start of the current token, -1 between tokens

	pstart, pend int // text of the token being returned
	failed       bool
	dead         bool
	errPos       int // offsets of the text of the error being returned as ILLEGAL
	errEnd       int
	prevPos      int // offset of the token returned last

	atbol        bool
	commentBlank bool // the COMMENT returned last stood alone on its line
	pendin       int
	indent       int
	level        int

	indstack  [maxIndent]int
	altstack  [maxIndent]int
	parens    [maxLevel]paren
	modes     [maxFStringLevel]mode
	modeIndex int
}

// Option configures a Scanner.
type Option func(*Scanner)

// paren is an open bracket on the paren stack.
type paren struct {
	char byte
	line int
	off  int
}

// mode is one entry of the f-string stack: the base mode, or an f-string or
// t-string being scanned (CPython's tokenizer_mode).
type mode struct {
	fstring        bool // scanning literal text; otherwise an expression
	quote          byte
	quoteSize      int
	raw            bool
	tstring        bool
	start          int
	curlyDepth     int
	exprStartDepth int
	inFormatSpec   bool
	inDebug        bool
}

const (
	maxIndent       = 100
	maxLevel        = 200
	maxFStringLevel = 150
	maxExprNesting  = 3
	tabSize         = 8
	altTabSize      = 1
	eof             = -1
)

var oneChars = map[int]token.Token{
	'(': token.LPAREN, ')': token.RPAREN, '[': token.LBRACK, ']': token.RBRACK,
	':': token.COLON, ',': token.COMMA, ';': token.SEMICOLON, '+': token.ADD,
	'-': token.SUB, '*': token.MUL, '/': token.QUO, '|': token.BIT_OR,
	'&': token.BIT_AND, '<': token.LSS, '>': token.GTR, '=': token.ASSIGN,
	'.': token.PERIOD, '%': token.REM, '{': token.LBRACE, '}': token.RBRACE,
	'^': token.BIT_XOR, '~': token.TILDE, '@': token.AT, '!': token.EXCLAMATION,
}

var twoCharTokens = map[string]token.Token{
	"!=": token.NEQ, "%=": token.REM_ASSIGN, "&=": token.BIT_AND_ASSIGN,
	"**": token.POW, "*=": token.MUL_ASSIGN, "+=": token.ADD_ASSIGN,
	"-=": token.SUB_ASSIGN, "->": token.ARROW, "//": token.FLOOR_QUO,
	"/=": token.QUO_ASSIGN, ":=": token.DEFINE, "<<": token.SHL,
	"<=": token.LEQ, "==": token.EQL, ">=": token.GEQ, ">>": token.SHR,
	"@=": token.AT_ASSIGN, "^=": token.BIT_XOR_ASSIGN, "|=": token.BIT_OR_ASSIGN,
}

var threeCharTokens = map[string]token.Token{
	"**=": token.POW_ASSIGN, "//=": token.FLOOR_QUO_ASSIGN,
	"<<=": token.SHL_ASSIGN, ">>=": token.SHR_ASSIGN,
}

// Decode returns the UTF-8 text of Python source bytes, as CPython's
// tokenizer reads them: a UTF-8 byte order mark is dropped, and a PEP 263
// coding cookie on the first or second line selects the encoding, which is
// then transcoded to UTF-8. Source without a cookie must be valid UTF-8.
// Line endings are left alone.
//
// Errors are *Error values of category ErrSyntax with CPython's messages
// ("unknown encoding: X", "encoding problem: X with BOM", "Non-UTF-8 code
// starting with '\xf6' on line 1, but no encoding declared; see
// https://peps.python.org/pep-0263/ for details", "codec can't decode ...").
// Positions are relative to the text after the byte order mark.
func Decode(src []byte) ([]byte, error) {
	body, bom := bytes.CutPrefix(src, []byte("\xef\xbb\xbf"))
	name, err := cookie(body, bom)
	if err != nil {
		return nil, err
	}
	if name == "" {
		if i := invalidUTF8(body); i >= 0 {
			return nil, nonUTF8Error(body, i)
		}
		return body, nil
	}
	return transcode(body, name)
}

// WithExtraTokens makes the scanner return the stream of CPython's tokenize
// module instead of the one its parser consumes (tok_extra_tokens in
// Parser/lexer/lexer.c). The stream differs in two ways.
//
// It adds COMMENT tokens and NL tokens, the latter for newlines that do not
// end a logical line: blank lines, comment-only lines and newlines inside
// brackets.
//
// It is lenient where the parser's tokenizer is strict: a number is not
// checked for leading zeros ("0123") or for running into a name ("1abc"), a
// non-ASCII identifier is not verified (NFKC and XID rules), and a closing
// bracket may be unmatched or mismatched. Indentation, string and f-string
// errors are still reported.
func WithExtraTokens() Option {
	return func(s *Scanner) { s.extra = true }
}

// WithoutImplicitNewline stops the scanner from appending the line break that
// CPython adds to the source of exec mode. Without it a source that does not
// end in a line break is scanned as is, as for the interactive and eval
// modes: no NEWLINE is produced at its end and a last line of only
// whitespace or a final backslash is judged as CPython judges it. The
// parser uses it for single mode.
func WithoutImplicitNewline() Option {
	return func(s *Scanner) { s.exact = true }
}

// New returns a scanner for src, which belongs to file. It records the line
// starts of src into file as it scans. New panics if file.Size() differs from
// len(src).
func New(file *token.File, src []byte, opts ...Option) *Scanner {
	if file.Size() != len(src) {
		panic(fmt.Sprintf("scanner: file size (%d) does not match src size (%d)", file.Size(), len(src)))
	}
	s := &Scanner{file: file, src: src, atbol: true, start: -1}
	for _, opt := range opts {
		opt(s)
	}
	s.end = len(src)
	if len(src) > 0 && src[len(src)-1] != '\n' && !s.exact {
		s.end++
	}
	return s
}

// Scan returns the next token: its position, kind and source text. The end
// of a token is pos + len(lit), with these exceptions: INDENT's lit is the
// indentation; DEDENT and EOF have no text; NEWLINE and NL lit hold the line
// break, empty for the implicit one at the end of a source without a final
// line break; FSTRING_MIDDLE and TSTRING_MIDDLE end before the second brace
// of "{{" and "}}", which the next token skips over.
//
// Keywords are returned as their own kinds and soft keywords as NAME. After
// an error, Scan returns ILLEGAL once, recorded in Err, and EOF from then
// on. A character that begins no token, such as '$', is returned as ILLEGAL
// without an error; the parser reports it.
func (s *Scanner) Scan() (pos token.Pos, tok token.Token, lit string) {
	if s.dead {
		return s.file.Pos(len(s.src)), token.EOF, ""
	}
	s.pstart, s.pend = -1, -1
	tok = s.next()
	switch {
	case s.failed:
		s.dead = true
		start := max(s.errPos, s.prevPos)
		return s.file.Pos(min(start, len(s.src))), token.ILLEGAL, s.text(start, s.errEnd)
	case tok == token.EOF:
		s.dead = true
		return s.file.Pos(len(s.src)), token.EOF, ""
	}
	s.prevPos = min(s.pstart, len(s.src))
	lit = s.text(s.pstart, s.pend)
	if tok == token.NAME {
		tok = token.Lookup(lit)
	}
	return s.file.Pos(min(s.pstart, len(s.src))), tok, lit
}

// Err returns the errors found so far, or nil.
func (s *Scanner) Err() error {
	return s.errs.Err()
}

func (s *Scanner) text(start, end int) string {
	end = min(end, len(s.src))
	if start >= end {
		return ""
	}
	return string(s.src[start:end])
}

// next is CPython's tok_get.
func (s *Scanner) next() token.Token {
	cur := &s.modes[s.modeIndex]
	if cur.fstring {
		return s.fstringMode(cur)
	}
	return s.normalMode(cur)
}

// fstringMode is CPython's tok_get_fstring_mode: it scans the literal text of
// an f-string or t-string, or switches to expression mode.
func (s *Scanner) fstringMode(cur *mode) token.Token {
	s.start = s.off
	startChar := s.nextc()
	if startChar == '{' {
		peek := s.nextc()
		s.backup(peek)
		if peek != '{' {
			s.backup(startChar)
			cur.exprStartDepth++
			if cur.exprStartDepth >= maxExprNesting {
				return s.fail(ErrSyntax, "%c-string: expressions nested too deeply", cur.prefix())
			}
			cur.fstring = false
			return s.normalMode(cur)
		}
	}
	s.backup(startChar)

	for i := 0; i < cur.quoteSize; i++ {
		if q := s.nextc(); q != int(cur.quote) {
			s.backup(q)
			return s.fstringMiddle(cur)
		}
	}
	s.modeIndex--
	if cur.tstring {
		return s.emit(token.TSTRING_END)
	}
	return s.emit(token.FSTRING_END)
}

// fstringMiddle scans literal text up to the next brace, the closing quote or
// an error; it is the text branch of CPython's tok_get_fstring_mode.
func (s *Scanner) fstringMiddle(cur *mode) token.Token {
	endQuote := 0
	unicodeEscape := false
	for endQuote != cur.quoteSize {
		c := s.nextc()
		if s.failed {
			return token.ILLEGAL
		}
		inSpec := cur.inFormatSpec && cur.exprStartDepth >= 0
		if c == eof || (cur.quoteSize == 1 && c == '\n') {
			if inSpec && c == '\n' {
				if cur.quoteSize == 1 {
					return s.fail(ErrSyntax, "%c-string: newlines are not allowed in format specifiers for single quoted %c-strings", cur.prefix(), cur.prefix())
				}
				s.backup(c)
				cur.fstring = false
				cur.inFormatSpec = false
				return s.emit(cur.middle())
			}
			detected := s.lineno
			s.off = cur.start + 1
			if cur.quoteSize == 3 {
				return s.fail(ErrSyntax, "unterminated triple-quoted %c-string literal (detected at line %d)", cur.prefix(), detected)
			}
			return s.fail(ErrSyntax, "unterminated %c-string literal (detected at line %d)", cur.prefix(), detected)
		}
		if c == int(cur.quote) {
			endQuote++
			continue
		}
		endQuote = 0

		switch c {
		case '{':
			peek := s.nextc()
			if peek != '{' || inSpec {
				s.backup(peek)
				s.backup(c)
				cur.exprStartDepth++
				if cur.exprStartDepth >= maxExprNesting {
					return s.fail(ErrSyntax, "%c-string: expressions nested too deeply", cur.prefix())
				}
				cur.fstring = false
				cur.inFormatSpec = false
				return s.emit(cur.middle())
			}
			s.pstart, s.pend = s.start, s.off-1
			return cur.middle()
		case '}':
			if unicodeEscape {
				return s.emit(cur.middle())
			}
			peek := s.nextc()
			if peek == '}' && !inSpec && cur.curlyDepth == 0 {
				s.pstart, s.pend = s.start, s.off-1
				return cur.middle()
			}
			s.backup(peek)
			s.backup(c)
			cur.fstring = false
			cur.inFormatSpec = false
			return s.emit(cur.middle())
		case '\\':
			peek := s.nextc()
			if peek == '\r' {
				peek = s.nextc()
			}
			if peek == '{' || peek == '}' {
				s.backup(peek)
				continue
			}
			if !cur.raw && peek == 'N' {
				if peek = s.nextc(); peek == '{' {
					unicodeEscape = true
				} else {
					s.backup(peek)
				}
			}
		}
	}
	for i := 0; i < cur.quoteSize; i++ {
		s.backup(int(cur.quote))
	}
	return s.emit(cur.middle())
}

// normalMode is CPython's tok_get_normal_mode.
func (s *Scanner) normalMode(cur *mode) token.Token {
	var c int
	var blank bool
nextline:
	s.start = -1
	blank = false
	if s.atbol {
		var ok bool
		if blank, ok = s.indentation(); !ok {
			return token.ILLEGAL
		}
	}

	s.start = s.off
	if s.pendin < 0 {
		s.pendin++
		s.pstart, s.pend = s.off, s.off
		return token.DEDENT
	}
	if s.pendin > 0 {
		s.pendin--
		s.pstart, s.pend = s.lineStart, s.off
		return token.INDENT
	}

	c = s.nextc()
	s.backup(c)

again:
	s.start = -1
	for {
		c = s.nextc()
		if c != ' ' && c != '\t' && c != '\f' {
			break
		}
	}
	s.start = max(s.off-1, 0)

	if c == '#' {
		for c != eof && c != '\n' && c != '\r' {
			c = s.nextc()
		}
		if s.extra {
			s.backup(c)
			s.commentBlank = blank
			return s.emit(token.COMMENT)
		}
		s.start = s.off - 1
	}

	if c == eof {
		if s.level > 0 {
			return s.failEOF()
		}
		return token.EOF
	}

	if isIdentStart(c) {
		return s.identifier(cur, c)
	}

	if c == '\r' {
		c = s.nextc()
	}

	if c == '\n' {
		s.atbol = true
		blank = blank || s.commentBlank
		s.commentBlank = false
		if blank || s.level > 0 {
			if s.extra {
				return s.emit(token.NL)
			}
			goto nextline
		}
		return s.emit(token.NEWLINE)
	}

	if c == '.' {
		c = s.nextc()
		switch {
		case isDigit(c):
			return s.fraction(c)
		case c == '.':
			if c = s.nextc(); c == '.' {
				return s.emit(token.ELLIPSIS)
			}
			s.backup(c)
			s.backup('.')
		default:
			s.backup(c)
		}
		return s.emit(token.PERIOD)
	}

	if isDigit(c) {
		return s.number(c)
	}

	if c == '\'' || c == '"' {
		return s.stringLit(cur, c)
	}

	if c == '\\' {
		if s.continuation() == -1 {
			return token.ILLEGAL
		}
		goto again
	}

	return s.punctuation(cur, c)
}

// punctuation scans operators and delimiters, the last case of CPython's
// tok_get_normal_mode.
func (s *Scanner) punctuation(cur *mode, c int) token.Token {
	if (c == ':' || c == '}' || c == '!' || c == '{') && s.modeIndex > 0 && cur.exprStartDepth >= 0 {
		cursor := cur.curlyDepth
		if c != '{' {
			cursor--
		}
		if c == ':' && cursor == cur.exprStartDepth {
			cur.fstring = true
			cur.inFormatSpec = true
			return s.emit(token.COLON)
		}
	}

	c2 := s.nextc()
	if tok := twoChars(c, c2); tok != token.ILLEGAL {
		c3 := s.nextc()
		if tok3 := threeChars(c, c2, c3); tok3 != token.ILLEGAL {
			tok = tok3
		} else {
			s.backup(c3)
		}
		return s.emit(tok)
	}
	s.backup(c2)

	switch c {
	case '(', '[', '{':
		if s.level >= maxLevel {
			return s.fail(ErrSyntax, "too many nested parentheses")
		}
		s.parens[s.level] = paren{char: byte(c), line: s.lineno, off: s.start}
		s.level++
		if s.modeIndex > 0 {
			cur.curlyDepth++
		}
	case ')', ']', '}':
		if !s.closeBracket(cur, c) {
			return token.ILLEGAL
		}
	}

	if c < ' ' || c == 0x7f {
		return s.fail(ErrSyntax, "invalid non-printable character U+%04X", c)
	}
	if c == '=' && s.modeIndex > 0 && cur.curlyDepth-cur.exprStartDepth == 1 {
		cur.inDebug = true
	}
	return s.emit(oneChars[c])
}

// closeBracket handles a closing bracket c, reporting whether it is valid.
func (s *Scanner) closeBracket(cur *mode, c int) bool {
	inside := s.modeIndex > 0
	if inside && cur.curlyDepth == 0 && c == '}' {
		s.fail(ErrSyntax, "%c-string: single '}' is not allowed", cur.prefix())
		return false
	}
	if !s.extra && s.level == 0 {
		s.fail(ErrSyntax, "unmatched '%c'", c)
		return false
	}
	if s.level > 0 {
		s.level--
		open := s.parens[s.level]
		if !s.extra && !closes(open.char, c) {
			s.failMismatch(cur, c, open)
			return false
		}
	}
	if inside {
		cur.curlyDepth--
		if cur.curlyDepth < 0 {
			s.fail(ErrSyntax, "%c-string: unmatched '%c'", cur.prefix(), c)
			return false
		}
		if c == '}' && cur.curlyDepth == cur.exprStartDepth {
			cur.exprStartDepth--
			cur.fstring = true
			cur.inFormatSpec = false
			cur.inDebug = false
		}
	}
	return true
}

// failMismatch reports a closing bracket c that does not close the open one.
func (s *Scanner) failMismatch(cur *mode, c int, open paren) {
	if s.modeIndex > 0 && open.char == '{' && cur.curlyDepth-1 == cur.exprStartDepth {
		s.fail(ErrSyntax, "%c-string: unmatched '%c'", cur.prefix(), c)
		return
	}
	if open.line != s.lineno {
		s.fail(ErrSyntax, "closing parenthesis '%c' does not match opening parenthesis '%c' on line %d", c, open.char, open.line)
		return
	}
	s.fail(ErrSyntax, "closing parenthesis '%c' does not match opening parenthesis '%c'", c, open.char)
}

// number scans a number whose first digit c was read.
func (s *Scanner) number(c int) token.Token {
	if c != '0' {
		return s.decimal()
	}
	switch c = s.nextc(); c {
	case 'x', 'X':
		return s.radix(16, "hexadecimal")
	case 'o', 'O':
		return s.radix(8, "octal")
	case 'b', 'B':
		return s.radix(2, "binary")
	}
	return s.zeros(c)
}

// zeros scans a number that starts with zeros and no radix prefix; c is the
// character after the first zero.
func (s *Scanner) zeros(c int) token.Token {
	nonzero := false
	for {
		if c == '_' {
			if c = s.nextc(); !isDigit(c) {
				s.backup(c)
				return s.fail(ErrSyntax, "invalid decimal literal")
			}
		}
		if c != '0' {
			break
		}
		c = s.nextc()
	}
	zerosEnd := s.off
	if isDigit(c) {
		nonzero = true
		if c = s.decimalTail(); c == 0 {
			return token.ILLEGAL
		}
	}
	switch {
	case c == '.':
		return s.fraction(s.nextc())
	case c == 'e' || c == 'E':
		return s.exponent(c)
	case c == 'j' || c == 'J':
		return s.imaginary(c)
	case nonzero && !s.extra:
		s.backup(c)
		return s.failAt(s.start, zerosEnd, "leading zeros in decimal integer literals are not permitted; use an 0o prefix for octal integers", ErrSyntax)
	}
	return s.numberEnd(c, "decimal")
}

// radix scans the digits of a 0x, 0o or 0b literal.
func (s *Scanner) radix(base int, kind string) token.Token {
	valid := func(c int) bool {
		switch base {
		case 16:
			return isDigit(c) || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
		case 8:
			return '0' <= c && c < '8'
		}
		return c == '0' || c == '1'
	}
	c := s.nextc()
	for {
		if c == '_' {
			c = s.nextc()
		}
		if !valid(c) {
			if base != 16 && isDigit(c) {
				return s.fail(ErrSyntax, "invalid digit '%c' in %s literal", c, kind)
			}
			s.backup(c)
			return s.fail(ErrSyntax, "invalid %s literal", kind)
		}
		for c = s.nextc(); valid(c); c = s.nextc() {
		}
		if c != '_' {
			break
		}
	}
	if base != 16 && isDigit(c) {
		return s.fail(ErrSyntax, "invalid digit '%c' in %s literal", c, kind)
	}
	return s.numberEnd(c, kind)
}

// decimal scans a number whose first digit, 1-9, was read.
func (s *Scanner) decimal() token.Token {
	c := s.decimalTail()
	if c == 0 {
		return token.ILLEGAL
	}
	if c == '.' {
		return s.fraction(s.nextc())
	}
	return s.exponentPart(c)
}

// fraction continues a number after the point; c is the character after it.
func (s *Scanner) fraction(c int) token.Token {
	if isDigit(c) {
		if c = s.decimalTail(); c == 0 {
			return token.ILLEGAL
		}
	}
	return s.exponentPart(c)
}

// exponentPart scans the optional exponent; c is the character after the
// digits.
func (s *Scanner) exponentPart(c int) token.Token {
	if c == 'e' || c == 'E' {
		return s.exponent(c)
	}
	return s.imaginary(c)
}

// exponent scans an exponent; e is the 'e' just read.
func (s *Scanner) exponent(e int) token.Token {
	c := s.nextc()
	switch {
	case c == '+' || c == '-':
		if c = s.nextc(); !isDigit(c) {
			s.backup(c)
			return s.fail(ErrSyntax, "invalid decimal literal")
		}
	case !isDigit(c):
		s.backup(c)
		if !s.verifyEnd(e, "decimal") {
			return token.ILLEGAL
		}
		s.backup(e)
		return s.emit(token.NUMBER)
	}
	if c = s.decimalTail(); c == 0 {
		return token.ILLEGAL
	}
	return s.imaginary(c)
}

// imaginary scans the optional j suffix; c is the character after the digits.
func (s *Scanner) imaginary(c int) token.Token {
	if c == 'j' || c == 'J' {
		return s.numberEnd(s.nextc(), "imaginary")
	}
	return s.numberEnd(c, "decimal")
}

// numberEnd finishes a number; c is the character after it.
func (s *Scanner) numberEnd(c int, kind string) token.Token {
	if !s.verifyEnd(c, kind) {
		return token.ILLEGAL
	}
	s.backup(c)
	return s.emit(token.NUMBER)
}

// verifyEnd is CPython's verify_end_of_number: a number must not run into a
// name. A keyword that may follow a number only warns in CPython and is
// accepted here.
func (s *Scanner) verifyEnd(c int, kind string) bool {
	if s.extra {
		return true
	}
	switch c {
	case 'a':
		if s.lookahead("nd") {
			return true
		}
	case 'e':
		if s.lookahead("lse") {
			return true
		}
	case 'f':
		if s.lookahead("or") {
			return true
		}
	case 'i':
		c2 := s.nextc()
		s.backup(c2)
		if c2 == 'f' || c2 == 'n' || c2 == 's' {
			return true
		}
	case 'o':
		if s.lookahead("r") {
			return true
		}
	case 'n':
		if s.lookahead("ot") {
			return true
		}
	}
	if c < 128 && isIdentChar(c) {
		s.backup(c)
		s.fail(ErrSyntax, "invalid %s literal", kind)
		return false
	}
	return true
}

// lookahead reports whether test follows and is not followed by a name
// character; it consumes nothing.
func (s *Scanner) lookahead(test string) bool {
	n := 0
	res := false
	for {
		c := s.nextc()
		if n == len(test) {
			res = !isIdentChar(c)
		} else if c == int(test[n]) {
			n++
			continue
		}
		s.backup(c)
		for ; n > 0; n-- {
			s.backup(int(test[n-1]))
		}
		return res
	}
}

// decimalTail is CPython's tok_decimal_tail: it reads digits with single
// underscores and returns the character after, or 0 after an error.
func (s *Scanner) decimalTail() int {
	var c int
	for {
		for c = s.nextc(); isDigit(c); c = s.nextc() {
		}
		if c != '_' {
			return c
		}
		if c = s.nextc(); !isDigit(c) {
			s.backup(c)
			s.fail(ErrSyntax, "invalid decimal literal")
			return 0
		}
	}
}

// identifier scans a name, or the prefix of a string.
func (s *Scanner) identifier(cur *mode, c int) token.Token {
	var b, r, u, f, t bool
	for {
		switch {
		case !b && (c == 'b' || c == 'B'):
			b = true
		case !u && (c == 'u' || c == 'U'):
			u = true
		case !r && (c == 'r' || c == 'R'):
			r = true
		case !f && (c == 'f' || c == 'F'):
			f = true
		case !t && (c == 't' || c == 'T'):
			t = true
		default:
			return s.name(c)
		}
		c = s.nextc()
		if c != '"' && c != '\'' {
			continue
		}
		if bad := incompatiblePrefix(b, r, u, f, t); bad != "" {
			return s.failAt(s.start, s.off, bad, ErrSyntax)
		}
		if f || t {
			return s.fstringStart(cur, c)
		}
		return s.stringLit(cur, c)
	}
}

// stringLit scans a string whose opening quote c was read.
func (s *Scanner) stringLit(cur *mode, quote int) token.Token {
	quoteSize, endQuote := 1, 0
	escapedQuote := false
	c := s.nextc()
	if c == quote {
		if c = s.nextc(); c == quote {
			quoteSize = 3
		} else {
			endQuote = 1
		}
	}
	if c != quote {
		s.backup(c)
	}
	for endQuote != quoteSize {
		c = s.nextc()
		if s.failed {
			return token.ILLEGAL
		}
		if c == eof || (quoteSize == 1 && c == '\n') {
			return s.failString(cur, quote, quoteSize, escapedQuote)
		}
		if c == quote {
			endQuote++
			continue
		}
		endQuote = 0
		if c == '\\' {
			if c = s.nextc(); c == quote {
				escapedQuote = true
			}
			if c == '\r' {
				s.nextc()
			}
		}
	}
	return s.emit(token.STRING)
}

// failString reports an unterminated string at its start.
func (s *Scanner) failString(cur *mode, quote, quoteSize int, escapedQuote bool) token.Token {
	detected := s.lineno
	s.off = s.start + 1
	if s.modeIndex > 0 && int(cur.quote) == quote && cur.quoteSize == quoteSize {
		return s.fail(ErrSyntax, "%c-string: expecting '}'", cur.prefix())
	}
	switch {
	case quoteSize == 3:
		return s.fail(ErrSyntax, "unterminated triple-quoted string literal (detected at line %d)", detected)
	case escapedQuote:
		return s.fail(ErrSyntax, "unterminated string literal (detected at line %d); perhaps you escaped the end quote?", detected)
	}
	return s.fail(ErrSyntax, "unterminated string literal (detected at line %d)", detected)
}

// fstringStart pushes an f-string or t-string; quote is its opening quote.
func (s *Scanner) fstringStart(cur *mode, quote int) token.Token {
	quoteSize := 1
	if after := s.nextc(); after != quote {
		s.backup(after)
	} else if after2 := s.nextc(); after2 == quote {
		quoteSize = 3
	} else {
		// An empty string: its closing quote is scanned in the new mode.
		s.backup(after2)
		s.backup(after)
	}
	s.pstart, s.pend = s.start, s.off
	if s.modeIndex+1 >= maxFStringLevel {
		return s.fail(ErrSyntax, "too many nested f-strings or t-strings")
	}
	s.modeIndex++
	m := &s.modes[s.modeIndex]
	*m = mode{fstring: true, quote: byte(quote), quoteSize: quoteSize, start: s.start, exprStartDepth: -1}
	second := byte(0)
	if s.start+1 < len(s.src) {
		second = s.src[s.start+1] | 0x20
	}
	switch first := s.src[s.start] | 0x20; first {
	case 't':
		m.tstring = true
		m.raw = second == 'r'
	case 'f':
		m.raw = second == 'r'
	default:
		m.raw = true
		m.tstring = second == 't'
	}
	if m.tstring {
		return token.TSTRING_START
	}
	return token.FSTRING_START
}

// name finishes a name whose first characters were read; c is the character
// after the prefix letters already consumed.
func (s *Scanner) name(c int) token.Token {
	nonascii := false
	for isIdentChar(c) {
		if c >= 128 {
			nonascii = true
		}
		c = s.nextc()
	}
	s.backup(c)
	if nonascii && !s.extra && !s.verifyIdentifier() {
		return token.ILLEGAL
	}
	return s.emit(token.NAME)
}

// verifyIdentifier checks the name read against the PEP 3131 character sets.
func (s *Scanner) verifyIdentifier() bool {
	text := s.src[s.start:min(s.off, len(s.src))]
	for i, n := 0, 0; i < len(text); i += n {
		r, size := utf8.DecodeRune(text[i:])
		n = size
		if r == utf8.RuneError && size == 1 {
			s.off = s.start + i + 1
			s.fail(ErrSyntax, "'utf-8' codec can't decode byte 0x%02x: invalid start byte", text[i])
			return false
		}
		if unicode.Is(idContinue, r) && (i > 0 || unicode.Is(idStart, r)) {
			continue
		}
		s.off = s.start + i + n
		s.failChar(r)
		return false
	}
	return true
}

// failChar reports a character that cannot appear where it does.
func (s *Scanner) failChar(r rune) token.Token {
	if unicode.Is(printable, r) {
		return s.fail(ErrSyntax, "invalid character '%c' (U+%04X)", r, r)
	}
	return s.fail(ErrSyntax, "invalid non-printable character U+%04X", r)
}

// emit records the span of the token just scanned and returns tok.
func (s *Scanner) emit(tok token.Token) token.Token {
	s.pstart, s.pend = s.start, s.off
	return tok
}

// indentation is the line-start part of CPython's tok_get_normal_mode. It
// reports whether the line is blank (only whitespace or a comment).
func (s *Scanner) indentation() (blank, ok bool) {
	col, altcol, contCol := 0, 0, 0
	s.atbol = false
	var c int
loop:
	for {
		c = s.nextc()
		switch {
		case c == ' ':
			col++
			altcol++
		case c == '\t':
			col = (col/tabSize + 1) * tabSize
			altcol = (altcol/altTabSize + 1) * altTabSize
		case c == '\f':
			col, altcol = 0, 0
		case c == '\\':
			if contCol == 0 {
				contCol = col
			}
			if s.continuation() == -1 {
				return false, false
			}
		case c == eof && s.failed:
			return false, false
		default:
			break loop
		}
	}
	s.backup(c)
	if c == '#' || c == '\n' || c == '\r' {
		return true, true
	}
	if s.level > 0 {
		return false, true
	}
	if contCol != 0 {
		col, altcol = contCol, contCol
	}
	return false, s.checkIndent(col, altcol)
}

// checkIndent compares an indentation with the indentation stacks.
func (s *Scanner) checkIndent(col, altcol int) bool {
	switch top := s.indstack[s.indent]; {
	case col == top:
		if altcol != s.altstack[s.indent] {
			s.failTab()
		}
	case col > top:
		if s.indent+1 >= maxIndent {
			s.failIndent(ErrIndentation, "too many levels of indentation")
		} else if altcol <= s.altstack[s.indent] {
			s.failTab()
		} else {
			s.pendin++
			s.indent++
			s.indstack[s.indent] = col
			s.altstack[s.indent] = altcol
		}
	default:
		for s.indent > 0 && col < s.indstack[s.indent] {
			s.pendin--
			s.indent--
		}
		if col != s.indstack[s.indent] {
			s.off = s.loaded
			s.fail(ErrIndentation, "unindent does not match any outer indentation level")
		} else if altcol != s.altstack[s.indent] {
			s.failTab()
		}
	}
	return !s.failed
}

// failTab reports inconsistent tabs and spaces in the indentation.
func (s *Scanner) failTab() token.Token {
	return s.failIndent(ErrTab, "inconsistent use of tabs and spaces in indentation")
}

// failIndent reports an indentation error at the start of the line.
func (s *Scanner) failIndent(err error, msg string) token.Token {
	return s.failAt(s.lineStart, s.lineStart, msg, err)
}

// continuation is CPython's tok_continuation_line. It returns the character
// after the line break, or -1 after recording an error.
func (s *Scanner) continuation() int {
	c := s.nextc()
	if c == '\r' {
		c = s.nextc()
	}
	if c != '\n' {
		s.fail(ErrSyntax, "unexpected character after line continuation character")
		return -1
	}
	c = s.nextc()
	if c == eof {
		s.off = s.end
		s.failEOF()
		return -1
	}
	s.backup(c)
	return c
}

// failEOF reports the end of input inside a bracket or after a backslash.
func (s *Scanner) failEOF() token.Token {
	if s.level > 0 {
		p := s.parens[s.level-1]
		return s.failAt(p.off, p.off+1, fmt.Sprintf("'%c' was never closed", p.char), ErrSyntax)
	}
	return s.fail(ErrSyntax, "unexpected EOF while parsing")
}

// fail records a formatted error at the character read last.
func (s *Scanner) fail(err error, format string, args ...any) token.Token {
	return s.failAt(s.lastChar(), s.off, fmt.Sprintf(format, args...), err)
}

// lastChar returns the offset of the character read last, where CPython puts
// the position of most tokenizer errors.
func (s *Scanner) lastChar() int {
	i := min(s.off, len(s.src)+1) - 1
	if i >= len(s.src) {
		return len(s.src)
	}
	for i > s.lineStart && i > 0 && !utf8.RuneStart(s.src[i]) {
		i--
	}
	return max(i, 0)
}

// backup returns c, the character read last, to the input.
func (s *Scanner) backup(c int) {
	if c != eof {
		s.off--
	}
}

// nextc is CPython's tok_nextc.
func (s *Scanner) nextc() int {
	if s.off >= s.end || s.failed {
		return eof
	}
	if s.off == s.loaded && !s.load() {
		return eof
	}
	c := s.byteAt(s.off)
	s.off++
	return int(c)
}

// byteAt is the byte at off; the source ends with an implicit line break.
func (s *Scanner) byteAt(off int) byte {
	if off >= len(s.src) {
		return '\n'
	}
	return s.src[off]
}

// load reads the next line, like the underflow functions of CPython.
func (s *Scanner) load() bool {
	end := s.end
	if i := bytes.IndexByte(s.src[s.loaded:], '\n'); i >= 0 {
		end = s.loaded + i + 1
	}
	s.lineno++
	s.lineStart = s.loaded
	s.file.AddLine(s.loaded)
	s.loaded = end
	if bytes.IndexByte(s.src[s.lineStart:min(end, len(s.src))], 0) >= 0 {
		s.off = end
		s.failAt(end-1, end, "source code cannot contain null bytes", ErrSyntax)
		return false
	}
	return true
}

// failAt records an error covering [start, end) and returns ILLEGAL.
func (s *Scanner) failAt(start, end int, msg string, err error) token.Token {
	if s.failed {
		return token.ILLEGAL
	}
	s.failed = true
	s.errPos = min(start, len(s.src))
	s.errEnd = max(min(end, len(s.src)), s.errPos)
	s.errs.Add(s.position(s.errPos), s.position(s.errEnd), msg, err)
	return token.ILLEGAL
}

// position is the Position of the byte offset off.
func (s *Scanner) position(off int) token.Position {
	return s.file.Position(s.file.Pos(off))
}

// middle is the token kind of the literal text of the string.
func (m *mode) middle() token.Token {
	if m.tstring {
		return token.TSTRING_MIDDLE
	}
	return token.FSTRING_MIDDLE
}

// prefix is the letter naming the string kind in messages.
func (m *mode) prefix() byte {
	if m.tstring {
		return 't'
	}
	return 'f'
}

// closes reports whether c is the bracket that closes open.
func closes(open byte, c int) bool {
	return open == '(' && c == ')' || open == '[' && c == ']' || open == '{' && c == '}'
}

// threeChars and twoChars return the operator spelled by the characters, or
// ILLEGAL; a negative character is eof and spells nothing.
func threeChars(c, c2, c3 int) token.Token {
	if c < 0 || c2 < 0 || c3 < 0 {
		return token.ILLEGAL
	}
	return threeCharTokens[string([]byte{byte(c), byte(c2), byte(c3)})]
}

func twoChars(c, c2 int) token.Token {
	if c < 0 || c2 < 0 {
		return token.ILLEGAL
	}
	return twoCharTokens[string([]byte{byte(c), byte(c2)})]
}

func incompatiblePrefix(b, r, u, f, t bool) string {
	pairs := []struct {
		x, y   bool
		nx, ny string
	}{
		{u, b, "u", "b"}, {u, r, "u", "r"}, {u, f, "u", "f"}, {u, t, "u", "t"},
		{b, f, "b", "f"}, {b, t, "b", "t"}, {f, t, "f", "t"},
	}
	for _, p := range pairs {
		if p.x && p.y {
			return fmt.Sprintf("'%s' and '%s' prefixes are incompatible", p.nx, p.ny)
		}
	}
	return ""
}

func isIdentChar(c int) bool {
	return isIdentStart(c) || isDigit(c)
}

func isDigit(c int) bool {
	return '0' <= c && c <= '9'
}

func isIdentStart(c int) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || c == '_' || c >= 128
}

// transcode converts body from the named codec to UTF-8.
func transcode(body []byte, name string) ([]byte, error) {
	enc, codec := lookupCodec(name)
	if enc == nil {
		return nil, &Error{Msg: "unknown encoding: " + name, Err: ErrSyntax}
	}
	if codec == "utf-8" {
		if i, reason, n := utf8Problem(body); n > 0 {
			return nil, decodeError(codec, body, i, n, reason)
		}
		return body, nil
	}
	if codec == "ascii" {
		for i, c := range body {
			if c >= 0x80 {
				return nil, decodeError(codec, body, i, 1, "ordinal not in range(128)")
			}
		}
		return body, nil
	}
	out, err := enc.NewDecoder().Bytes(body)
	if err != nil {
		return nil, &Error{Msg: err.Error(), Err: ErrSyntax}
	}
	if i := bytes.Index(out, []byte("\uFFFD")); i >= 0 {
		return nil, undefinedError(enc, codec, body, out, i)
	}
	return out, nil
}

// undefinedError locates the first byte the decoder replaced.
func undefinedError(enc encoding.Encoding, codec string, body, out []byte, i int) error {
	reason, name := "illegal multibyte sequence", codec
	pos := 0
	if _, ok := enc.(*charmap.Charmap); ok {
		reason, name = "character maps to <undefined>", "charmap"
		pos = utf8.RuneCount(out[:i])
	} else if prefix, err := enc.NewEncoder().Bytes(out[:i]); err == nil {
		pos = len(prefix)
	}
	return decodeError(name, body, min(pos, len(body)-1), 1, reason)
}

func decodeError(codec string, body []byte, i, n int, reason string) error {
	where := fmt.Sprintf("byte 0x%02x in position %d", body[i], i)
	if n > 1 {
		where = fmt.Sprintf("bytes in position %d-%d", i, i+n-1)
	}
	return &Error{Msg: fmt.Sprintf("'%s' codec can't decode %s: %s", codec, where, reason), Err: ErrSyntax}
}

// utf8Problem finds the first invalid UTF-8 sequence like CPython's decoder:
// its start, reason, and length (the lead byte and the valid continuation
// bytes after it). The length is 0 for valid input.
func utf8Problem(b []byte) (start int, reason string, n int) {
	for i := 0; i < len(b); {
		c := b[i]
		need, lo, hi := 0, byte(0x80), byte(0xBF)
		switch {
		case c < 0x80:
			i++
			continue
		case 0xC2 <= c && c <= 0xDF:
			need = 1
		case 0xE0 <= c && c <= 0xEF:
			need = 2
			if c == 0xE0 {
				lo = 0xA0
			} else if c == 0xED {
				hi = 0x9F
			}
		case 0xF0 <= c && c <= 0xF4:
			need = 3
			if c == 0xF0 {
				lo = 0x90
			} else if c == 0xF4 {
				hi = 0x8F
			}
		default:
			return i, "invalid start byte", 1
		}
		k := 1
		for ; k <= need; k++ {
			if i+k >= len(b) {
				return i, "unexpected end of data", k
			}
			if b[i+k] < lo || b[i+k] > hi {
				return i, "invalid continuation byte", k
			}
			lo, hi = 0x80, 0xBF
		}
		i += need + 1
	}
	return 0, "", 0
}

// lookupCodec finds the codec Python knows by name, and its canonical name.
func lookupCodec(name string) (encoding.Encoding, string) {
	n := strings.ToLower(strings.NewReplacer("-", "_", " ", "_").Replace(name))
	switch n {
	case "utf_8", "utf8", "u8", "utf", "utf8_ucs2", "utf8_ucs4", "cp65001":
		return charmap.ISO8859_1, "utf-8"
	case "ascii", "us_ascii", "646", "iso646_us", "ansi_x3.4_1968", "cp367", "ibm367", "us":
		return charmap.ISO8859_1, "ascii"
	case "latin_1", "latin1", "iso_8859_1", "iso8859_1", "8859", "cp819", "latin", "l1", "iso_ir_100", "ibm819":
		return charmap.ISO8859_1, "iso8859-1"
	}
	dashed := strings.ReplaceAll(n, "_", "-")
	candidates := []string{dashed, n}
	if digits, ok := strings.CutPrefix(n, "cp"); ok {
		candidates = append(candidates, "windows-"+digits, "ibm"+digits)
	}
	if digits, ok := strings.CutPrefix(n, "iso8859_"); ok {
		candidates = append(candidates, "iso-8859-"+digits)
	}
	for _, c := range candidates {
		if e, err := htmlindex.Get(c); err == nil && e != nil {
			return e, n
		}
		if e, err := ianaindex.IANA.Encoding(c); err == nil && e != nil {
			return e, n
		}
	}
	return nil, ""
}

func nonUTF8Error(body []byte, i int) error {
	line := bytes.Count(body[:i], []byte("\n")) + 1
	start := bytes.LastIndexByte(body[:i], '\n') + 1
	pos := token.Position{Offset: i, Line: line, Column: i - start + 1}
	end := token.Position{Offset: i + 1, Line: line, Column: pos.Column + 1}
	msg := fmt.Sprintf("Non-UTF-8 code starting with '\\x%02x' on line %d, but no encoding declared; see https://peps.python.org/pep-0263/ for details", body[i], line)
	return &Error{Pos: pos, End: end, Msg: msg, Err: ErrSyntax}
}

func invalidUTF8(b []byte) int {
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size <= 1 {
			return i
		}
		i += size
	}
	return -1
}

// cookie finds the coding cookie of the first two lines, like
// decode_str and check_coding_spec of CPython. It returns the codec name to
// transcode from, or "" for plain UTF-8.
func cookie(body []byte, bom bool) (string, error) {
	rest := body
	for lineno := 1; lineno <= 2 && len(rest) > 0; lineno++ {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			i = len(rest)
		}
		line, size := rest[:min(i+1, len(rest))], i
		if lineno == 2 {
			size++
		}
		spec, stop := codingSpec(line, size)
		switch {
		case spec != "":
			if !bom {
				return transcodeName(spec), nil
			}
			if spec != "utf-8" {
				off := len(body) - len(rest)
				pos := token.Position{Offset: off, Line: lineno, Column: 1}
				end := token.Position{Offset: off + size, Line: lineno, Column: size + 1}
				return "", &Error{Pos: pos, End: end, Msg: "encoding problem: " + spec + " with BOM", Err: ErrSyntax}
			}
			return "", nil
		case stop:
			return "", nil
		}
		rest = rest[len(line):]
	}
	return "", nil
}

// transcodeName is "" for the cookie CPython decodes as plain UTF-8.
func transcodeName(spec string) string {
	if spec == "utf-8" {
		return ""
	}
	return spec
}

// codingSpec is get_coding_spec and the stop rule of check_coding_spec: it
// returns the cookie of a comment line, and whether the line holds code, which
// ends the search.
func codingSpec(line []byte, size int) (spec string, stop bool) {
	i := 0
	for ; i < size-6; i++ {
		if line[i] == '#' {
			break
		}
		if line[i] != ' ' && line[i] != '\t' && line[i] != '\f' {
			return "", true
		}
	}
	for ; i < size-6; i++ {
		t := line[i:]
		if !bytes.HasPrefix(t, []byte("coding")) || len(t) < 7 || t[6] != ':' && t[6] != '=' {
			continue
		}
		j := 7
		for j < len(t) && (t[j] == ' ' || t[j] == '\t') {
			j++
		}
		begin := j
		for j < len(t) && isCookieChar(t[j]) {
			j++
		}
		if begin < j {
			return normalName(string(t[begin:j])), false
		}
	}
	for _, c := range line[:min(size, len(line))] {
		switch {
		case c == '#' || c == '\n' || c == '\r':
			return "", false
		case c != ' ' && c != '\t' && c != '\f':
			return "", true
		}
	}
	return "", false
}

// normalName is get_normal_name of CPython.
func normalName(name string) string {
	n := strings.ToLower(strings.ReplaceAll(name[:min(len(name), 12)], "_", "-"))
	switch {
	case n == "utf-8" || strings.HasPrefix(n, "utf-8-"):
		return "utf-8"
	case n == "latin-1" || n == "iso-8859-1" || n == "iso-latin-1",
		strings.HasPrefix(n, "latin-1-"), strings.HasPrefix(n, "iso-8859-1-"), strings.HasPrefix(n, "iso-latin-1-"):
		return "iso-8859-1"
	}
	return name
}

func isCookieChar(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '-' || c == '_' || c == '.'
}
