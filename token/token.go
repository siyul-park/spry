package token

import (
	"strconv"
	"unicode"
)

// Token is the lexical kind of a Python 3.14 token.
type Token int

// Special, literal and layout tokens. EOF is CPython's ENDMARKER.
//
// The layout tokens NEWLINE, NL, INDENT and DEDENT sit after the literals and
// before the operators; they belong to no range, so IsLiteral, IsOperator and
// IsKeyword are all false for them.
const (
	ILLEGAL Token = iota
	EOF
	COMMENT

	NAME           // spam
	NUMBER         // 12.3, 0x1f, 1j
	STRING         // "abc", b'abc'
	FSTRING_START  // f"
	FSTRING_MIDDLE // literal text inside an f-string
	FSTRING_END    // closing quote of an f-string
	TSTRING_START  // t"
	TSTRING_MIDDLE // literal text inside a t-string
	TSTRING_END    // closing quote of a t-string

	NEWLINE // end of a logical line
	NL      // non-logical newline (blank line, inside brackets)
	INDENT  // increase of indentation
	DEDENT  // decrease of indentation

	ADD // +
	SUB // -
	MUL // *
	QUO // /
	REM // %
	POW // **

	FLOOR_QUO // //
	AT        // @
	BIT_AND   // &
	BIT_OR    // |
	BIT_XOR   // ^
	SHL       // <<
	SHR       // >>
	TILDE     // ~

	ADD_ASSIGN       // +=
	SUB_ASSIGN       // -=
	MUL_ASSIGN       // *=
	QUO_ASSIGN       // /=
	REM_ASSIGN       // %=
	POW_ASSIGN       // **=
	FLOOR_QUO_ASSIGN // //=
	AT_ASSIGN        // @=
	BIT_AND_ASSIGN   // &=
	BIT_OR_ASSIGN    // |=
	BIT_XOR_ASSIGN   // ^=
	SHL_ASSIGN       // <<=
	SHR_ASSIGN       // >>=

	EQL // ==
	NEQ // !=
	LSS // <
	LEQ // <=
	GTR // >
	GEQ // >=

	ASSIGN      // =
	DEFINE      // :=
	EXCLAMATION // !

	LPAREN // (
	LBRACK // [
	LBRACE // {
	RPAREN // )
	RBRACK // ]
	RBRACE // }

	COMMA     // ,
	PERIOD    // .
	SEMICOLON // ;
	COLON     // :
	ELLIPSIS  // ...
	ARROW     // ->

	FALSE
	NONE
	TRUE
	AND
	AS
	ASSERT
	ASYNC
	AWAIT
	BREAK
	CLASS
	CONTINUE
	DEF
	DEL
	ELIF
	ELSE
	EXCEPT
	FINALLY
	FOR
	FROM
	GLOBAL
	IF
	IMPORT
	IN
	IS
	LAMBDA
	NONLOCAL
	NOT
	OR
	PASS
	RAISE
	RETURN
	TRY
	WHILE
	WITH
	YIELD
)

// Operator precedences returned by Precedence. Only binary arithmetic and
// bitwise operators have one: "**", comparisons and "not", "and", "or" are
// grammar rules of the parser, not precedence levels. UnaryPrec is where
// unary "+", "-" and "~" bind (tighter than "*", looser than "**").
const (
	LowestPrec  = 0
	UnaryPrec   = 7
	HighestPrec = 8
)

var tokens = [...]string{
	ILLEGAL: "ILLEGAL",
	EOF:     "EOF",
	COMMENT: "COMMENT",

	NAME:           "NAME",
	NUMBER:         "NUMBER",
	STRING:         "STRING",
	FSTRING_START:  "FSTRING_START",
	FSTRING_MIDDLE: "FSTRING_MIDDLE",
	FSTRING_END:    "FSTRING_END",
	TSTRING_START:  "TSTRING_START",
	TSTRING_MIDDLE: "TSTRING_MIDDLE",
	TSTRING_END:    "TSTRING_END",

	NEWLINE: "NEWLINE",
	NL:      "NL",
	INDENT:  "INDENT",
	DEDENT:  "DEDENT",

	ADD:       "+",
	SUB:       "-",
	MUL:       "*",
	QUO:       "/",
	REM:       "%",
	POW:       "**",
	FLOOR_QUO: "//",
	AT:        "@",
	BIT_AND:   "&",
	BIT_OR:    "|",
	BIT_XOR:   "^",
	SHL:       "<<",
	SHR:       ">>",
	TILDE:     "~",

	ADD_ASSIGN:       "+=",
	SUB_ASSIGN:       "-=",
	MUL_ASSIGN:       "*=",
	QUO_ASSIGN:       "/=",
	REM_ASSIGN:       "%=",
	POW_ASSIGN:       "**=",
	FLOOR_QUO_ASSIGN: "//=",
	AT_ASSIGN:        "@=",
	BIT_AND_ASSIGN:   "&=",
	BIT_OR_ASSIGN:    "|=",
	BIT_XOR_ASSIGN:   "^=",
	SHL_ASSIGN:       "<<=",
	SHR_ASSIGN:       ">>=",

	EQL: "==",
	NEQ: "!=",
	LSS: "<",
	LEQ: "<=",
	GTR: ">",
	GEQ: ">=",

	ASSIGN:      "=",
	DEFINE:      ":=",
	EXCLAMATION: "!",

	LPAREN: "(",
	LBRACK: "[",
	LBRACE: "{",
	RPAREN: ")",
	RBRACK: "]",
	RBRACE: "}",

	COMMA:     ",",
	PERIOD:    ".",
	SEMICOLON: ";",
	COLON:     ":",
	ELLIPSIS:  "...",
	ARROW:     "->",

	FALSE:    "False",
	NONE:     "None",
	TRUE:     "True",
	AND:      "and",
	AS:       "as",
	ASSERT:   "assert",
	ASYNC:    "async",
	AWAIT:    "await",
	BREAK:    "break",
	CLASS:    "class",
	CONTINUE: "continue",
	DEF:      "def",
	DEL:      "del",
	ELIF:     "elif",
	ELSE:     "else",
	EXCEPT:   "except",
	FINALLY:  "finally",
	FOR:      "for",
	FROM:     "from",
	GLOBAL:   "global",
	IF:       "if",
	IMPORT:   "import",
	IN:       "in",
	IS:       "is",
	LAMBDA:   "lambda",
	NONLOCAL: "nonlocal",
	NOT:      "not",
	OR:       "or",
	PASS:     "pass",
	RAISE:    "raise",
	RETURN:   "return",
	TRY:      "try",
	WHILE:    "while",
	WITH:     "with",
	YIELD:    "yield",
}

var keywords = func() map[string]Token {
	m := make(map[string]Token, YIELD-FALSE+1)
	for t := FALSE; t <= YIELD; t++ {
		m[tokens[t]] = t
	}
	return m
}()

var softKeywords = map[string]struct{}{
	"match": {},
	"case":  {},
	"type":  {},
	"_":     {},
}

// Lookup maps an identifier to its keyword token or to NAME. Soft keywords
// are names.
func Lookup(ident string) Token {
	if t, ok := keywords[ident]; ok {
		return t
	}
	return NAME
}

// IsKeyword reports whether name is a hard keyword of Python 3.14.
func IsKeyword(name string) bool {
	_, ok := keywords[name]
	return ok
}

// IsSoftKeyword reports whether name is a soft keyword: match, case, type or
// "_". Soft keywords are ordinary names outside the syntactic contexts that
// give them meaning.
func IsSoftKeyword(name string) bool {
	_, ok := softKeywords[name]
	return ok
}

// IsIdentifier reports whether name is a valid Python identifier: PEP 3131
// shape and not a hard keyword. Soft keywords are identifiers.
//
// It approximates XID_Start and XID_Continue with Go's unicode tables and
// does not apply NFKC normalization. The exact Python Unicode tables live in
// the scanner's generated tables.
func IsIdentifier(name string) bool {
	if name == "" || IsKeyword(name) {
		return false
	}
	for i, r := range name {
		if i == 0 && !isIdentifierStart(r) || i > 0 && !isIdentifierContinue(r) {
			return false
		}
	}
	return true
}

// Precedence returns the binary operator precedence of t, or LowestPrec if t
// is not a binary arithmetic or bitwise operator. From loosest to tightest:
// "|", "^", "&", "<<" ">>", "+" "-", and "*" "@" "/" "//" "%".
func (t Token) Precedence() int {
	switch t {
	case BIT_OR:
		return 1
	case BIT_XOR:
		return 2
	case BIT_AND:
		return 3
	case SHL, SHR:
		return 4
	case ADD, SUB:
		return 5
	case MUL, AT, QUO, FLOOR_QUO, REM:
		return 6
	}
	return LowestPrec
}

// IsLiteral reports whether t is a name or a literal token, including the
// f-string and t-string parts.
func (t Token) IsLiteral() bool {
	return NAME <= t && t <= TSTRING_END
}

// IsOperator reports whether t is an operator or delimiter.
func (t Token) IsOperator() bool {
	return ADD <= t && t <= ARROW
}

// IsKeyword reports whether t is a hard keyword.
func (t Token) IsKeyword() bool {
	return FALSE <= t && t <= YIELD
}

// String returns the source spelling of an operator or keyword, such as "+"
// or "def", and the name of any other token, such as "NAME". An unknown token
// yields "token(N)".
func (t Token) String() string {
	if 0 <= t && int(t) < len(tokens) && tokens[t] != "" {
		return tokens[t]
	}
	return "token(" + strconv.Itoa(int(t)) + ")"
}

func isIdentifierContinue(r rune) bool {
	return isIdentifierStart(r) ||
		unicode.In(r, unicode.Mn, unicode.Mc, unicode.Nd, unicode.Pc, unicode.Other_ID_Continue)
}

func isIdentifierStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.Is(unicode.Nl, r) || unicode.Is(unicode.Other_ID_Start, r)
}
