package cli

import (
	"bytes"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/scanner"
	"github.com/siyul-park/spry/token"
)

// dumpTokenStream prints the tokens of src, decoded text, in the format of
// `python -m tokenize` (Lib/tokenize.py main): "start-end:" padded to 20,
// the tokenize type name padded to 15 and the repr of the text padded to 15,
// with 0-based code point columns. A scan error ends the dump; the parse
// that follows reports it.
func dumpTokenStream(w io.Writer, src []byte) {
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(src))
	s := scanner.New(file, src, scanner.WithExtraTokens())
	starts := lineStarts(src)
	last := len(starts) // line of the zero-width tokens at the end
	if len(src) > 0 && !bytes.HasSuffix(src, []byte("\n")) {
		last++
	}
	for {
		pos, tok, lit := s.Scan()
		if tok == token.ILLEGAL && s.Err() != nil {
			return
		}
		offset := file.Offset(pos)
		start := tokenPosition(src, starts, last, offset, tok)
		end := tokenPosition(src, starts, last, offset+len(lit), tok)
		if tok == token.NEWLINE || tok == token.NL {
			end = [2]int{start[0], start[1] + max(utf8.RuneCountInString(lit), 1)}
		}
		fmt.Fprintf(w, "%-20s%-15s%-15s\n", fmt.Sprintf("%d,%d-%d,%d:", start[0], start[1], end[0], end[1]), tokenizeName(tok), ast.Repr(lit))
		if tok == token.EOF {
			return
		}
	}
}

// tokenPosition is the 1-based line and 0-based code point column of a byte
// offset. DEDENT and ENDMARKER at the end of src sit on line last.
func tokenPosition(src []byte, starts []int, last, offset int, tok token.Token) [2]int {
	if offset >= len(src) && (tok == token.DEDENT || tok == token.EOF) {
		return [2]int{last, 0}
	}
	offset = min(offset, len(src))
	line := 1
	for line < len(starts) && starts[line] <= offset {
		line++
	}
	return [2]int{line, utf8.RuneCount(src[starts[line-1]:offset])}
}

func lineStarts(src []byte) []int {
	starts := []int{0}
	for i, c := range src {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// tokenizeName is the name of the token type in the tokenize module.
func tokenizeName(tok token.Token) string {
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
