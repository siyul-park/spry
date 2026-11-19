package scanner_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/siyul-park/spry/internal/cpython/corpus"
	"github.com/siyul-park/spry/scanner"
	"github.com/siyul-park/spry/token"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite the testdata/*.tokens goldens")

var cookieRE = regexp.MustCompile(`^[ \t\f]*#.*?coding[:=][ \t]*([-\w.]+)`)

// show renders a token as KIND, or KIND:"text" for kinds whose text carries
// information. A NEWLINE or NL shows its text only when it is not "\n".
func show(tok token.Token, lit string) string {
	switch tok {
	case token.NEWLINE, token.NL:
		if lit == "\n" {
			return tok.String()
		}
	case token.NAME, token.NUMBER, token.STRING, token.COMMENT, token.INDENT, token.ILLEGAL,
		token.FSTRING_START, token.FSTRING_MIDDLE, token.FSTRING_END,
		token.TSTRING_START, token.TSTRING_MIDDLE, token.TSTRING_END:
	default:
		return tok.String()
	}
	return tok.String() + ":" + strconv.Quote(lit)
}

// checkTokenize compares the scanner with the tokenize module: the dump of a
// Src case, the hash of the dump of a File case, or a rejection.
func checkTokenize(c corpus.Case, raw []byte) error {
	src, name, err := raw, "", error(nil)
	if c.File != "" {
		name = strings.TrimSuffix(detectEncoding(raw), "-sig")
		src, err = scanner.Decode(raw)
	}
	var dump string
	if err == nil {
		dump, err = tokenDump(src, name)
	}
	if c.Expect.Error != nil {
		if err == nil {
			return fmt.Errorf("want error %q, got none", c.Expect.Error.Msg)
		}
		if !errors.Is(err, scanner.ErrSyntax) {
			return fmt.Errorf("error %v is not a SyntaxError", err)
		}
		return nil
	}
	if err != nil {
		return err
	}
	if c.File == "" {
		return diff(c.Expect.Dump, dump)
	}
	sum := sha256.Sum256([]byte(dump))
	if got := hex.EncodeToString(sum[:]); got != c.Expect.Hash {
		return fmt.Errorf("dump hash %s, want %s", got, c.Expect.Hash)
	}
	return nil
}

// checkEncoding compares Decode and the scanner with CPython's acceptance of
// the file and its SyntaxError.
func checkEncoding(c corpus.Case, raw []byte) error {
	src, err := scanner.Decode(raw)
	var pos, end token.Position
	if err != nil {
		src = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	} else {
		fset := token.NewFileSet()
		s := scanner.New(fset.AddFile("", -1, len(src)), src)
		for _, tok, _ := s.Scan(); tok != token.EOF; _, tok, _ = s.Scan() {
		}
		err = s.Err()
	}
	var list scanner.ErrorList
	if errors.As(err, &list) {
		err = list[0]
	}
	var e *scanner.Error
	if errors.As(err, &e) {
		pos, end = e.Pos, e.End
	}
	want := c.Expect.Error
	switch {
	case want == nil && err != nil:
		return err
	case want == nil:
		return nil
	case err == nil:
		return fmt.Errorf("want error %q, got none", want.Msg)
	case e == nil || e.Msg != want.Msg:
		return fmt.Errorf("error %v, want %q", err, want.Msg)
	case want.Lineno > 0 && pos.Line != want.Lineno:
		return fmt.Errorf("error line %d, want %d", pos.Line, want.Lineno)
	case want.Offset > 0 && charColumn(src, pos) != want.Offset:
		return fmt.Errorf("error offset %d, want %d", charColumn(src, pos), want.Offset)
	case want.EndOffset > 0 && charColumn(src, end)-1 != want.EndOffset:
		return fmt.Errorf("error end offset %d, want %d", charColumn(src, end)-1, want.EndOffset)
	}
	return nil
}

func diff(want, got string) error {
	if want == got {
		return nil
	}
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < max(len(w), len(g)); i++ {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			b = g[i]
		}
		if a != b {
			return fmt.Errorf("line %d:\nwant %s\ngot  %s", i+1, a, b)
		}
	}
	return nil
}

// charColumn converts a byte column to CPython's code point offset.
func charColumn(src []byte, p token.Position) int {
	if p.Column == 0 || p.Offset > len(src) {
		return 0
	}
	return utf8.RuneCount(src[p.Offset-(p.Column-1):p.Offset]) + 1
}

// detectEncoding is tokenize.detect_encoding for the encoding name.
func detectEncoding(raw []byte) string {
	bom := bytes.HasPrefix(raw, []byte("\xef\xbb\xbf"))
	body := bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	def := "utf-8"
	if bom {
		def = "utf-8-sig"
	}
	for n := 0; n < 2 && len(body) > 0; n++ {
		line := body
		if i := bytes.IndexByte(body, '\n'); i >= 0 {
			line = body[:i+1]
		}
		if m := cookieRE.FindSubmatch(line); m != nil {
			name := normalName(string(m[1]))
			if bom {
				name += "-sig"
			}
			return name
		}
		if trimmed := bytes.TrimLeft(line, " \t\f"); len(trimmed) > 0 && trimmed[0] != '#' && trimmed[0] != '\r' && trimmed[0] != '\n' {
			break
		}
		body = body[len(line):]
	}
	return def
}

func normalName(name string) string {
	enc := strings.ToLower(strings.ReplaceAll(name[:min(len(name), 12)], "_", "-"))
	switch {
	case enc == "utf-8" || strings.HasPrefix(enc, "utf-8-"):
		return "utf-8"
	case enc == "latin-1" || enc == "iso-8859-1" || enc == "iso-latin-1",
		strings.HasPrefix(enc, "latin-1-"), strings.HasPrefix(enc, "iso-8859-1-"), strings.HasPrefix(enc, "iso-latin-1-"):
		return "iso-8859-1"
	}
	return name
}

// tokenDump renders the token stream like internal/cpython/driver/tokens.py:
// the tokenize module's token names, string reprs and (line, column)
// positions in code points. A non-empty encoding adds the ENCODING token.
func tokenDump(src []byte, encoding string) (string, error) {
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(src))
	s := scanner.New(file, src, scanner.WithExtraTokens())

	var out strings.Builder
	out.Grow(4 * len(src))
	if encoding != "" {
		appendRow(&out, "ENCODING", encoding, [2]int{0, 0}, [2]int{0, 0})
	}
	numLines := splitLines(string(src))
	starts := lineStarts(src)
	missingNL := len(src) > 0 && src[len(src)-1] != '\n' && src[len(src)-1] != '\r'
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.ILLEGAL && s.Err() != nil {
			return out.String(), s.Err()
		}
		off := file.Offset(pos)
		start, end := lineCol(src, starts, off, tok), lineCol(src, starts, off+len(lit), tok)
		if tok == token.NEWLINE || tok == token.NL {
			end = [2]int{start[0], start[1] + max(utf8.RuneCountInString(lit), 1)}
		}
		if missingNL && tok == token.NEWLINE && end[0] == numLines {
			continue
		}
		appendRow(&out, tokenName(tok), lit, start, end)
	}
	return out.String(), nil
}

// appendRow adds a dump line, separating it from the previous one with "\n".
func appendRow(out *strings.Builder, name, lit string, start, end [2]int) {
	if out.Len() > 0 {
		out.WriteByte('\n')
	}
	repr := pyRepr(lit)
	out.WriteString("    ")
	out.WriteString(name)
	out.WriteString(strings.Repeat(" ", max(10-len(name), 0)))
	out.WriteByte(' ')
	out.WriteString(repr)
	out.WriteString(strings.Repeat(" ", max(13-utf8.RuneCountInString(repr), 0)))
	for _, p := range [][2]int{start, end} {
		out.WriteString(" (")
		out.WriteString(strconv.Itoa(p[0]))
		out.WriteString(", ")
		out.WriteString(strconv.Itoa(p[1]))
		out.WriteByte(')')
	}
}

// lineCol returns the 1-based line and 0-based code point column of a byte
// offset. Zero-width layout tokens at the end of the source sit on the line
// after the last.
func lineCol(src []byte, starts []int, off int, tok token.Token) [2]int {
	if off >= len(src) && (tok == token.DEDENT || tok == token.EOF) {
		n := splitLines(string(src))
		return [2]int{n + 1, 0}
	}
	off = min(off, len(src))
	line := sort.Search(len(starts), func(i int) bool { return starts[i] > off })
	return [2]int{line, utf8.RuneCount(src[starts[line-1]:off])}
}

// lineStarts returns the offset of each line start, first line at 0.
func lineStarts(src []byte) []int {
	starts := []int{0}
	for i, c := range src {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// splitLines is len(str.splitlines()).
func splitLines(s string) int {
	n := 0
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		switch rs[i] {
		case '\r':
			if i+1 < len(rs) && rs[i+1] == '\n' {
				i++
			}
			n++
		case '\n', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
			n++
		default:
			if i == len(rs)-1 {
				n++
			}
		}
	}
	return n
}

func tokenName(tok token.Token) string {
	switch {
	case tok == token.EOF:
		return "ENDMARKER"
	case tok == token.NAME || tok.IsKeyword():
		return "NAME"
	case tok.IsOperator() || tok == token.ILLEGAL:
		return "OP"
	}
	return tok.String()
}

// pyRepr is repr() of a Python str.
func pyRepr(s string) string {
	quote := '\''
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var b strings.Builder
	b.WriteRune(quote)
	for _, r := range s {
		switch {
		case r == quote || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x7f || unicode.IsPrint(r):
			b.WriteRune(r)
		case r <= 0xff:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r <= 0xffff:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	b.WriteRune(quote)
	return b.String()
}

func TestDecode(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
		msg  string // expected error message, if any
		line int    // expected error line, 0 when CPython reports none
	}{
		{name: "utf-8 passes through", src: "x = 'é'\n", want: "x = 'é'\n"},
		{name: "byte order mark is dropped", src: "\xef\xbb\xbfx = 1\n", want: "x = 1\n"},
		{name: "empty source", src: "", want: ""},
		{name: "latin-1 cookie transcodes", src: "# coding: latin-1\nx = '\xe9'\n", want: "# coding: latin-1\nx = 'é'\n"},
		{name: "cookie on the second line", src: "#!/usr/bin/python\n# -*- coding: koi8-r -*-\nx = '\xc1'\n", want: "#!/usr/bin/python\n# -*- coding: koi8-r -*-\nx = 'а'\n"},
		{name: "cookie spelled fileencoding", src: "# vim: set fileencoding=latin1 :\nx = '\xe9'\n", want: "# vim: set fileencoding=latin1 :\nx = 'é'\n"},
		{name: "cookie matching the byte order mark", src: "\xef\xbb\xbf# coding: utf-8\nx = 1\n", want: "# coding: utf-8\nx = 1\n"},
		{name: "cookie after code is ignored", src: "x = 1\n# coding: latin-1\n", want: "x = 1\n# coding: latin-1\n"},
		{name: "cookie on the third line is ignored", src: "#\n#\n# coding: latin-1\n", want: "#\n#\n# coding: latin-1\n"},
		{name: "unknown encoding", src: "# coding: foo\n", msg: "unknown encoding: foo"},
		{name: "cookie disagrees with the byte order mark", src: "\xef\xbb\xbf# coding: latin-1\nx = 1\n", msg: "encoding problem: iso-8859-1 with BOM", line: 1},
		{name: "invalid UTF-8", src: "x = '\xf6'\n", msg: "Non-UTF-8 code starting with '\\xf6' on line 1, but no encoding declared; see https://peps.python.org/pep-0263/ for details", line: 1},
		{name: "invalid UTF-8 on a later line", src: "x = 1\ny = '\xff'\n", msg: "Non-UTF-8 code starting with '\\xff' on line 2, but no encoding declared; see https://peps.python.org/pep-0263/ for details", line: 2},
		{name: "utf-8 cookie does not excuse invalid UTF-8", src: "# coding: utf-8\nx = '\xff'\n", msg: "Non-UTF-8 code starting with '\\xff' on line 2, but no encoding declared; see https://peps.python.org/pep-0263/ for details", line: 2},
		{name: "ascii cookie rejects high bytes", src: "# coding: ascii\nx='\xe9'\n", msg: "'ascii' codec can't decode byte 0xe9 in position 19: ordinal not in range(128)"},
		{name: "utf8 alias reports the codec error", src: "# coding: utf8\nx='\xff'\n", msg: "'utf-8' codec can't decode byte 0xff in position 18: invalid start byte"},
		{name: "undefined byte of a single-byte codec", src: "# coding: cp1252\nx='\x81'\n", msg: "'charmap' codec can't decode byte 0x81 in position 20: character maps to <undefined>"},
		{name: "invalid multibyte sequence", src: "# coding: shift_jis\nx='\x81'\n", msg: "'shift_jis' codec can't decode byte 0x81 in position 23: illegal multibyte sequence"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := scanner.Decode([]byte(tt.src))

			if tt.msg == "" {
				require.NoError(t, err)
				require.Equal(t, tt.want, string(got))
				return
			}
			var e *scanner.Error
			require.ErrorAs(t, err, &e)
			require.ErrorIs(t, err, scanner.ErrSyntax)
			require.Equal(t, tt.msg, e.Msg)
			require.Equal(t, tt.line, e.Pos.Line)
		})
	}
}

func TestWithExtraTokens(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"comment after code", "x = 1  # one\n", `NAME:"x" = NUMBER:"1" COMMENT:"# one" NEWLINE EOF`},
		{"comment inside brackets", "(a, # c\n b)\n", `( NAME:"a" , COMMENT:"# c" NL NAME:"b" ) NEWLINE EOF`},
		{"comment at the end of the source", "x # c", `NAME:"x" COMMENT:"# c" NEWLINE:"" EOF`},
		{"blank line", "a\n\nb\n", `NAME:"a" NEWLINE NL NAME:"b" NEWLINE EOF`},
		{"newline inside brackets", "(a,\n b)\n", `( NAME:"a" , NL NAME:"b" ) NEWLINE EOF`},
		{"comment-only line", "# c\nx\n", `COMMENT:"# c" NL NAME:"x" NEWLINE EOF`},
		{"implicit newline of a comment line", "# c", `COMMENT:"# c" NL:"" EOF`},
		{"numbers may run into names", "1abc 0123\n", `NUMBER:"1" NAME:"abc" NUMBER:"0123" NEWLINE EOF`},
		{"brackets need not match", ")(]\n", `) ( ] NEWLINE EOF`},
		{"names are not verified", "a€\n", `NAME:"a€" NEWLINE EOF`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			s := scanner.New(fset.AddFile("t.py", -1, len(tt.src)), []byte(tt.src), scanner.WithExtraTokens())

			var got []string
			for {
				_, tok, lit := s.Scan()
				got = append(got, show(tok, lit))
				if tok == token.EOF {
					break
				}
			}

			require.NoError(t, s.Err())
			require.Equal(t, tt.want, strings.Join(got, " "))
		})
	}
}

func TestWithoutImplicitNewline(t *testing.T) {
	scan := func(src string) (string, error) {
		fset := token.NewFileSet()
		s := scanner.New(fset.AddFile("t.py", -1, len(src)), []byte(src), scanner.WithoutImplicitNewline())
		var got []string
		for {
			_, tok, lit := s.Scan()
			got = append(got, show(tok, lit))
			if tok == token.EOF {
				return strings.Join(got, " "), s.Err()
			}
		}
	}

	t.Run("adds no NEWLINE to a source without a final line break", func(t *testing.T) {
		got, err := scan("x = 1")
		require.NoError(t, err)
		require.Equal(t, `NAME:"x" = NUMBER:"1" EOF`, got)
	})

	t.Run("keeps the NEWLINE of a terminated source", func(t *testing.T) {
		got, err := scan("x\n")
		require.NoError(t, err)
		require.Equal(t, `NAME:"x" NEWLINE EOF`, got)
	})

	t.Run("rejects a final backslash", func(t *testing.T) {
		_, err := scan("a = \\")
		var list scanner.ErrorList
		require.ErrorAs(t, err, &list)
		require.Equal(t, "unexpected character after line continuation character", list[0].Msg)
	})

	t.Run("judges a last line of only white space", func(t *testing.T) {
		_, err := scan("if x:\n  y\n ")
		var list scanner.ErrorList
		require.ErrorAs(t, err, &list)
		require.ErrorIs(t, list[0], scanner.ErrIndentation)
		require.Equal(t, "unindent does not match any outer indentation level", list[0].Msg)
	})
}

func TestNew(t *testing.T) {
	t.Run("panics when the file size differs from the source", func(t *testing.T) {
		fset := token.NewFileSet()
		file := fset.AddFile("a.py", -1, 3)

		require.Panics(t, func() { scanner.New(file, []byte("x = 1")) })
	})

	t.Run("records line starts into the file while scanning", func(t *testing.T) {
		src := []byte("a\nb\n\nc")
		fset := token.NewFileSet()
		file := fset.AddFile("a.py", -1, len(src))
		s := scanner.New(file, src)

		for _, tok, _ := s.Scan(); tok != token.EOF; _, tok, _ = s.Scan() {
		}

		require.Equal(t, 4, file.LineCount())
		require.Equal(t, token.Position{Filename: "a.py", Offset: 5, Line: 4, Column: 1}, file.Position(file.Pos(5)))
	})
}

func TestScanner_Scan(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"empty source", "", "EOF"},
		{"name and keywords", "if x:\n", `if NAME:"x" : NEWLINE EOF`},
		{"soft keywords are names", "match case type _\n", `NAME:"match" NAME:"case" NAME:"type" NAME:"_" NEWLINE EOF`},
		{"unicode name", "naïve = π\n", `NAME:"naïve" = NAME:"π" NEWLINE EOF`},
		{"operators", "a <= b // c ** d >>= e -> f := g ... h != i\n",
			`NAME:"a" <= NAME:"b" // NAME:"c" ** NAME:"d" >>= NAME:"e" -> NAME:"f" := NAME:"g" ... NAME:"h" != NAME:"i" NEWLINE EOF`},
		{"diamond is two tokens", "a <> b\n", `NAME:"a" < > NAME:"b" NEWLINE EOF`},
		{"stray character is ILLEGAL without an error", "a $ b\n", `NAME:"a" ILLEGAL:"$" NAME:"b" NEWLINE EOF`},

		{"decimal and underscores", "0 00 0_0 1_000\n", `NUMBER:"0" NUMBER:"00" NUMBER:"0_0" NUMBER:"1_000" NEWLINE EOF`},
		{"radix", "0xAB_cd 0o17 0B101\n", `NUMBER:"0xAB_cd" NUMBER:"0o17" NUMBER:"0B101" NEWLINE EOF`},
		{"floats", "1.5 .5 1. 1e10 1E-5 1_0.0_1e+1_0\n",
			`NUMBER:"1.5" NUMBER:".5" NUMBER:"1." NUMBER:"1e10" NUMBER:"1E-5" NUMBER:"1_0.0_1e+1_0" NEWLINE EOF`},
		{"imaginary", "1j 1.5J 0e0j\n", `NUMBER:"1j" NUMBER:"1.5J" NUMBER:"0e0j" NEWLINE EOF`},
		{"keyword right after a number", "1if x else 2\n0x1for\n", `NUMBER:"1" if NAME:"x" else NUMBER:"2" NEWLINE NUMBER:"0x1f" or NEWLINE EOF`},
		{"attribute of a number needs a space", "1 .real\n", `NUMBER:"1" . NAME:"real" NEWLINE EOF`},

		{"string prefixes", "'a' b'b' rB'c' U'd'\n", `STRING:"'a'" STRING:"b'b'" STRING:"rB'c'" STRING:"U'd'" NEWLINE EOF`},
		{"empty strings", "'' \"\" '''''' \"\"\"\"\"\"\n", `STRING:"''" STRING:"\"\"" STRING:"''''''" STRING:"\"\"\"\"\"\"" NEWLINE EOF`},
		{"triple quoted spans lines", "'''a\nb'''\n", `STRING:"'''a\nb'''" NEWLINE EOF`},
		{"escaped quote and continued line", "'a\\'b' 'c\\\nd'\n", `STRING:"'a\\'b'" STRING:"'c\\\nd'" NEWLINE EOF`},

		{"f-string", "f'a{b}c'\n", `FSTRING_START:"f'" FSTRING_MIDDLE:"a" { NAME:"b" } FSTRING_MIDDLE:"c" FSTRING_END:"'" NEWLINE EOF`},
		{"f-string doubled braces", "f'{{x}}'\n", `FSTRING_START:"f'" FSTRING_MIDDLE:"{" FSTRING_MIDDLE:"x}" FSTRING_END:"'" NEWLINE EOF`},
		{"f-string conversion and nested format spec", "f'{a!r:>{w}}'\n",
			`FSTRING_START:"f'" { NAME:"a" ! NAME:"r" : FSTRING_MIDDLE:">" { NAME:"w" } FSTRING_MIDDLE:"" } FSTRING_END:"'" NEWLINE EOF`},
		{"f-string debug", "f'{x=}'\n", `FSTRING_START:"f'" { NAME:"x" = } FSTRING_END:"'" NEWLINE EOF`},
		{"f-string reuses its quote inside", "f'{f'{1}'}'\n",
			`FSTRING_START:"f'" { FSTRING_START:"f'" { NUMBER:"1" } FSTRING_END:"'" } FSTRING_END:"'" NEWLINE EOF`},
		{"f-string triple quoted spans lines", "f\"\"\"a\n{b}\"\"\"\n",
			`FSTRING_START:"f\"\"\"" FSTRING_MIDDLE:"a\n" { NAME:"b" } FSTRING_END:"\"\"\"" NEWLINE EOF`},
		{"f-string named escape keeps its braces", "f'\\N{BULLET}{x}'\n",
			`FSTRING_START:"f'" FSTRING_MIDDLE:"\\N{BULLET}" { NAME:"x" } FSTRING_END:"'" NEWLINE EOF`},
		{"f-string not-equal is not a conversion", "f'{x!=y}'\n", `FSTRING_START:"f'" { NAME:"x" != NAME:"y" } FSTRING_END:"'" NEWLINE EOF`},
		{"f-string lambda colon starts the format spec", "f'{lambda x: 1}'\n",
			`FSTRING_START:"f'" { lambda NAME:"x" : FSTRING_MIDDLE:" 1" } FSTRING_END:"'" NEWLINE EOF`},
		{"raw f-string prefixes", "rf'\\d{b}' Fr'x'\n",
			`FSTRING_START:"rf'" FSTRING_MIDDLE:"\\d" { NAME:"b" } FSTRING_END:"'" FSTRING_START:"Fr'" FSTRING_MIDDLE:"x" FSTRING_END:"'" NEWLINE EOF`},
		{"t-string", "t'{a}' tr'y'\n",
			`TSTRING_START:"t'" { NAME:"a" } TSTRING_END:"'" TSTRING_START:"tr'" TSTRING_MIDDLE:"y" TSTRING_END:"'" NEWLINE EOF`},

		{"indent and dedent", "if x:\n  y\n    z\n  w\nv\n",
			`if NAME:"x" : NEWLINE INDENT:"  " NAME:"y" NEWLINE INDENT:"    " NAME:"z" NEWLINE DEDENT NAME:"w" NEWLINE DEDENT NAME:"v" NEWLINE EOF`},
		{"several dedents at once", "if x:\n\ty\n\t\tz\nw\n",
			`if NAME:"x" : NEWLINE INDENT:"\t" NAME:"y" NEWLINE INDENT:"\t\t" NAME:"z" NEWLINE DEDENT DEDENT NAME:"w" NEWLINE EOF`},
		{"blank and comment lines do not change indentation", "if x:\n  y\n\n  # c\n  z\n",
			`if NAME:"x" : NEWLINE INDENT:"  " NAME:"y" NEWLINE NAME:"z" NEWLINE DEDENT EOF`},
		{"dedents at the end of the source", "if x:\n  if y:\n    z\n",
			`if NAME:"x" : NEWLINE INDENT:"  " if NAME:"y" : NEWLINE INDENT:"    " NAME:"z" NEWLINE DEDENT DEDENT EOF`},
		{"a form feed resets the indentation", "if x:\n  y\n\f  z\n", `if NAME:"x" : NEWLINE INDENT:"  " NAME:"y" NEWLINE NAME:"z" NEWLINE DEDENT EOF`},

		{"brackets join lines", "(a,\n b)\n[\n]\n", `( NAME:"a" , NAME:"b" ) NEWLINE [ ] NEWLINE EOF`},
		{"backslash joins lines", "x = 1 \\\n  + 2\n", `NAME:"x" = NUMBER:"1" + NUMBER:"2" NEWLINE EOF`},
		{"blank lines produce no tokens", "a\n\n\nb\n", `NAME:"a" NEWLINE NAME:"b" NEWLINE EOF`},
		{"comments produce no tokens", "a  # c\n# d\nb\n", `NAME:"a" NEWLINE NAME:"b" NEWLINE EOF`},
		{"carriage return line feed", "a\r\nb\r\n", `NAME:"a" NEWLINE:"\r\n" NAME:"b" NEWLINE:"\r\n" EOF`},

		{"implicit newline at the end of the source", "x", `NAME:"x" NEWLINE:"" EOF`},
		{"implicit newline after a comment", "  # c\nb", `NAME:"b" NEWLINE:"" EOF`},
		{"implicit newline then dedent", "if 1:\n  x", `if NUMBER:"1" : NEWLINE INDENT:"  " NAME:"x" NEWLINE:"" DEDENT EOF`},
		{"only a comment", "# c", `EOF`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			s := scanner.New(fset.AddFile("t.py", -1, len(tt.src)), []byte(tt.src))

			var got []string
			for {
				_, tok, lit := s.Scan()
				got = append(got, show(tok, lit))
				if tok == token.EOF {
					break
				}
			}

			require.NoError(t, s.Err())
			require.Equal(t, tt.want, strings.Join(got, " "))
		})
	}
}

func TestScanner_Err(t *testing.T) {
	var deep strings.Builder
	for i := range 101 {
		deep.WriteString(strings.Repeat(" ", i) + "if 1:\n")
	}
	deep.WriteString(strings.Repeat(" ", 101) + "x\n")

	tests := []struct {
		name string
		src  string
		msg  string
		kind error
		line int
		col  int // CPython offset, in code points
		end  int // CPython end offset, 0 when CPython reports none
	}{
		{"tab after spaces", "if 1:\n    x\n\ty\n", "inconsistent use of tabs and spaces in indentation", scanner.ErrTab, 3, 1, 0},
		{"spaces after tab", "if 1:\n\tx\n        y\n", "inconsistent use of tabs and spaces in indentation", scanner.ErrTab, 3, 1, 0},
		{"dedent to an unknown level", "if 1:\n    x\n  y\n", "unindent does not match any outer indentation level", scanner.ErrIndentation, 3, 4, 0},
		{"too many levels", deep.String(), "too many levels of indentation", scanner.ErrIndentation, 101, 1, 0},

		{"name after decimal", "x = 1abc\n", "invalid decimal literal", scanner.ErrSyntax, 1, 5, 5},
		{"trailing underscore", "x = 1_\n", "invalid decimal literal", scanner.ErrSyntax, 1, 6, 6},
		{"missing hex digits", "x = 0x\n", "invalid hexadecimal literal", scanner.ErrSyntax, 1, 6, 6},
		{"non-hex digit", "x = 0xg\n", "invalid hexadecimal literal", scanner.ErrSyntax, 1, 6, 6},
		{"digit 8 in octal", "x = 0o8\n", "invalid digit '8' in octal literal", scanner.ErrSyntax, 1, 7, 7},
		{"missing octal digits", "x = 0o\n", "invalid octal literal", scanner.ErrSyntax, 1, 6, 6},
		{"digit 2 in binary", "x = 0b2\n", "invalid digit '2' in binary literal", scanner.ErrSyntax, 1, 7, 7},
		{"leading zeros", "x = 0123\n", "leading zeros in decimal integer literals are not permitted; use an 0o prefix for octal integers", scanner.ErrSyntax, 1, 5, 6},
		{"leading zeros with underscore", "x = 00_1\n", "leading zeros in decimal integer literals are not permitted; use an 0o prefix for octal integers", scanner.ErrSyntax, 1, 5, 8},
		{"exponent sign without digits", "x = 1e+\n", "invalid decimal literal", scanner.ErrSyntax, 1, 7, 7},
		{"name after exponent letter", "x = 1ex\n", "invalid decimal literal", scanner.ErrSyntax, 1, 5, 5},
		{"name after imaginary", "x = 1jx\n", "invalid imaginary literal", scanner.ErrSyntax, 1, 6, 6},
		{"name after float", "x = 1.5x\n", "invalid decimal literal", scanner.ErrSyntax, 1, 7, 7},

		{"invalid character", "x = €\n", "invalid character '€' (U+20AC)", scanner.ErrSyntax, 1, 5, 5},
		{"invalid character inside a name", "x = a€b c\n", "invalid character '€' (U+20AC)", scanner.ErrSyntax, 1, 6, 6},
		{"non-printable character in a name", "x = a\u00a0b\n", "invalid non-printable character U+00A0", scanner.ErrSyntax, 1, 6, 6},
		{"control character", "x = \x01\n", "invalid non-printable character U+0001", scanner.ErrSyntax, 1, 5, 5},
		{"delete character", "x = \x7f\n", "invalid non-printable character U+007F", scanner.ErrSyntax, 1, 5, 5},
		{"null byte", "x = 1\x00\n", "source code cannot contain null bytes", scanner.ErrSyntax, 1, 7, 0},

		{"unterminated string", "x = 'abc\n", "unterminated string literal (detected at line 1)", scanner.ErrSyntax, 1, 5, 5},
		{"unterminated string at the end of the source", "x = 'abc", "unterminated string literal (detected at line 1)", scanner.ErrSyntax, 1, 5, 5},
		{"escaped end quote", "x = 'abc\\'\n", "unterminated string literal (detected at line 1); perhaps you escaped the end quote?", scanner.ErrSyntax, 1, 5, 5},
		{"unterminated triple quoted string", "x = \"\"\"abc\n\n", "unterminated triple-quoted string literal (detected at line 2)", scanner.ErrSyntax, 1, 5, 5},
		{"incompatible prefixes u and b", "x = ub'a'\n", "'u' and 'b' prefixes are incompatible", scanner.ErrSyntax, 1, 5, 7},
		{"incompatible prefixes b and f", "x = bf'a'\n", "'b' and 'f' prefixes are incompatible", scanner.ErrSyntax, 1, 5, 7},
		{"incompatible prefixes f and t", "x = ft'a'\n", "'f' and 't' prefixes are incompatible", scanner.ErrSyntax, 1, 5, 7},

		{"mismatched closing bracket", "x = (1, 2]\n", "closing parenthesis ']' does not match opening parenthesis '('", scanner.ErrSyntax, 1, 10, 10},
		{"mismatch across lines", "x = (1,\n 2]\n", "closing parenthesis ']' does not match opening parenthesis '(' on line 1", scanner.ErrSyntax, 2, 3, 3},
		{"unmatched closing bracket", "x = 1)\n", "unmatched ')'", scanner.ErrSyntax, 1, 6, 6},
		{"unclosed bracket", "x = (1,\n", "'(' was never closed", scanner.ErrSyntax, 1, 5, 0},
		{"unclosed inner bracket", "x = [(1,\n", "'(' was never closed", scanner.ErrSyntax, 1, 6, 0},
		{"too many nested brackets", strings.Repeat("(", 201), "too many nested parentheses", scanner.ErrSyntax, 1, 201, 201},
		{"text after a backslash", "x = 1 \\ y\n", "unexpected character after line continuation character", scanner.ErrSyntax, 1, 8, 0},
		{"backslash at the end of the source", "x = 1 \\\n", "unexpected EOF while parsing", scanner.ErrSyntax, 1, 8, 0},
		{"backslash at the end without newline", "x = 1 \\", "unexpected EOF while parsing", scanner.ErrSyntax, 1, 8, 0},

		{"f-string without closing brace", "x = f'{a'\n", "f-string: expecting '}'", scanner.ErrSyntax, 1, 9, 9},
		{"f-string with triple quotes without closing brace", "x = f'''{a'''\n", "f-string: expecting '}'", scanner.ErrSyntax, 1, 11, 11},
		{"f-string single closing brace", "x = f'a}'\n", "f-string: single '}' is not allowed", scanner.ErrSyntax, 1, 8, 8},
		{"f-string unmatched bracket", "x = f'{a)}'\n", "f-string: unmatched ')'", scanner.ErrSyntax, 1, 9, 9},
		{"unterminated f-string", "x = f'abc\n", "unterminated f-string literal (detected at line 1)", scanner.ErrSyntax, 1, 5, 5},
		{"unterminated triple quoted f-string", "x = f'''abc\n", "unterminated triple-quoted f-string literal (detected at line 1)", scanner.ErrSyntax, 1, 5, 5},
		{"newline in a single quoted format spec", "x = f'{a:\n}'\n", "f-string: newlines are not allowed in format specifiers for single quoted f-strings", scanner.ErrSyntax, 1, 10, 10},
		{"f-string expressions nested too deeply", "x = f'{a:{b:{c:{d}}}}'\n", "f-string: expressions nested too deeply", scanner.ErrSyntax, 1, 15, 15},
		{"t-string without closing brace", "x = t'{a'\n", "t-string: expecting '}'", scanner.ErrSyntax, 1, 9, 9},
		{"unterminated t-string", "x = t'abc\n", "unterminated t-string literal (detected at line 1)", scanner.ErrSyntax, 1, 5, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			s := scanner.New(fset.AddFile("t.py", -1, len(tt.src)), []byte(tt.src))

			var kinds []token.Token
			for len(kinds) < 1000 {
				_, tok, _ := s.Scan()
				kinds = append(kinds, tok)
				if tok == token.EOF {
					break
				}
			}

			var list scanner.ErrorList
			require.ErrorAs(t, s.Err(), &list)
			require.Len(t, list, 1)
			e := list[0]
			require.Equal(t, tt.msg, e.Msg)
			require.ErrorIs(t, e, tt.kind)
			require.ErrorIs(t, e, scanner.ErrSyntax)
			require.Equal(t, tt.line, e.Pos.Line)
			require.Equal(t, tt.col, charColumn([]byte(tt.src), e.Pos))
			if tt.end > 0 {
				require.Equal(t, tt.end, charColumn([]byte(tt.src), e.End)-1)
			}
			require.Equal(t, "t.py", e.Pos.Filename)
			require.GreaterOrEqual(t, len(kinds), 2)
			require.Equal(t, []token.Token{token.ILLEGAL, token.EOF}, kinds[len(kinds)-2:])
		})
	}

	t.Run("a clean source has no error", func(t *testing.T) {
		fset := token.NewFileSet()
		s := scanner.New(fset.AddFile("t.py", -1, 6), []byte("x = 1\n"))
		for _, tok, _ := s.Scan(); tok != token.EOF; _, tok, _ = s.Scan() {
		}

		require.NoError(t, s.Err())
	})
}

func TestCorpus_Tokenize(t *testing.T) {
	cases, err := corpus.Load("testdata/cpython/tokenize.jsonl")
	require.NoError(t, err)
	xfail, err := corpus.LoadXFail("testdata/cpython/xfail.txt")
	require.NoError(t, err)
	_, haveCache := corpus.Dir()

	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			if c.File != "" && !haveCache {
				t.Skip("cpython corpus not fetched: run make corpus")
			}
			raw, err := c.Source()
			require.NoError(t, err)
			xfail.Check(t, c.ID, checkTokenize(c, raw))
		})
	}
}

func TestCorpus_Encoding(t *testing.T) {
	cases, err := corpus.Load("testdata/cpython/encoding.jsonl")
	require.NoError(t, err)
	xfail, err := corpus.LoadXFail("testdata/cpython/xfail.txt")
	require.NoError(t, err)
	_, haveCache := corpus.Dir()

	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			if !haveCache {
				t.Skip("cpython corpus not fetched: run make corpus")
			}
			raw, err := c.Source()
			require.NoError(t, err)
			xfail.Check(t, c.ID, checkEncoding(c, raw))
		})
	}
}

// TestGolden_Tokens compares testdata/*.py with the token dumps checked in
// next to them. Each golden was verified against python3.14's tokenize module
// when it was generated with -update.
func TestGolden_Tokens(t *testing.T) {
	files, err := filepath.Glob("testdata/*.py")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			src, err := os.ReadFile(file)
			require.NoError(t, err)
			got, err := tokenDump(src, "")
			require.NoError(t, err)
			golden := strings.TrimSuffix(file, ".py") + ".tokens"
			if *update {
				require.NoError(t, os.WriteFile(golden, []byte(got+"\n"), 0o644))
			}
			want, err := os.ReadFile(golden)
			require.NoError(t, err)
			require.Equal(t, strings.TrimSuffix(string(want), "\n"), got)
		})
	}
}

func FuzzScanner(f *testing.F) {
	cases, err := corpus.Load("testdata/cpython/tokenize.jsonl")
	require.NoError(f, err)
	for _, c := range cases {
		if c.File == "" {
			f.Add([]byte(c.Src))
		}
	}
	for _, seed := range []string{"", "if x:\n  y\n", "f'{a!r:>{w}}'", "t'''{x", "(\n", "\\\n", "0x_1", "\x00", "\t\n \n"} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, src []byte) {
		for _, opts := range [][]scanner.Option{nil, {scanner.WithExtraTokens()}} {
			fset := token.NewFileSet()
			file := fset.AddFile("f.py", -1, len(src))
			s := scanner.New(file, src, opts...)

			depth, prev := 0, token.NoPos
			for i := 0; ; i++ {
				require.LessOrEqual(t, i, 8*len(src)+1000, "scanner does not terminate on %q", src)
				pos, tok, lit := s.Scan()
				require.GreaterOrEqual(t, pos, prev, "positions go backwards in %q", src)
				require.LessOrEqual(t, int(pos)+len(lit), file.Base()+file.Size(), "token ends past the file in %q", src)
				prev = pos
				switch tok {
				case token.INDENT:
					depth++
				case token.DEDENT:
					depth--
				}
				if tok == token.EOF {
					break
				}
			}
			require.True(t, s.Err() != nil || depth == 0, "unbalanced indentation in %q", src)
			if s.Err() != nil {
				require.ErrorIs(t, s.Err(), scanner.ErrSyntax)
			}
		}
	})
}
