package cli

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/siyul-park/spry/scanner"
)

// syntaxError is a rendered SyntaxError: the text CPython prints to stderr
// for it, without the "spry: " prefix other failures get.
type syntaxError struct {
	text string
}

// errorRange is the offset and end offset of a SyntaxError.
type errorRange struct {
	offset, end int
	sameLine    bool
	textLen     int // code points of the line
}

func (e *syntaxError) Error() string {
	return e.text
}

// renderSyntaxError formats e like TracebackException._format_syntax_error of
// CPython 3.14 (Lib/traceback.py): the File line, the source line without its
// leading blanks, a caret range under the error, and "Type: message".
// Offsets are code points, converted from the byte columns of e.
func renderSyntaxError(name string, src []byte, e *scanner.Error) string {
	var out strings.Builder
	out.WriteString("  File \"" + name + "\", line " + strconv.Itoa(e.Pos.Line) + "\n")
	if text, ok := sourceLine(src, e.Pos.Line); ok {
		out.WriteString(renderSourceLine(text, offsets(text, e)))
	}
	msg := e.Msg
	if msg == "" {
		msg = "<no detail available>"
	}
	out.WriteString(errorTypeName(e) + ": " + msg + "\n")
	return out.String()
}

// errorTypeName is the CPython exception class of e.
func errorTypeName(e *scanner.Error) string {
	switch {
	case errors.Is(e, scanner.ErrTab):
		return "TabError"
	case errors.Is(e, scanner.ErrIndentation):
		return "IndentationError"
	}
	return "SyntaxError"
}

// offsets maps the byte columns of e onto the code point offset and end
// offset of the SyntaxError CPython would raise (End is exclusive; the end offset is End.Column-1, see scanner.Error).
func offsets(text string, e *scanner.Error) errorRange {
	r := errorRange{textLen: utf8.RuneCountInString(text), sameLine: e.End.Line == e.Pos.Line}
	if e.Pos.Column > 0 {
		r.offset = codePoints(text, e.Pos.Column-1) + 1
	}
	if r.sameLine && e.End.Column > 1 {
		r.end = codePoints(text, e.End.Column-2) + 1
	}
	return r
}

// renderSourceLine prints the source line and the carets as traceback.py does:
// blanks (space, newline, form feed) are stripped from the left and the caret
// column follows, keeping tabs of the line so they stay aligned.
func renderSourceLine(text string, r errorRange) string {
	ltext := []rune(strings.TrimLeft(text, " \n\f"))
	spaces := r.textLen - len(ltext)
	line := "    " + string(ltext) + "\n"
	offset, end := r.offset, r.end
	if r.sameLine {
		if end == 0 {
			end = offset
		}
	} else {
		end = r.textLen + 1
	}
	offset = min(offset, r.textLen+1)
	end = min(end, r.textLen+1)
	if offset >= end || end < 0 {
		end = offset + 1
	}
	colno, endColno := offset-1-spaces, end-1-spaces
	if colno < 0 {
		return line
	}
	pad := make([]rune, 0, colno)
	for _, c := range ltext[:min(colno, len(ltext))] {
		if unicode.IsSpace(c) {
			pad = append(pad, c)
		} else {
			pad = append(pad, ' ')
		}
	}
	return line + "    " + string(pad) + strings.Repeat("^", endColno-colno) + "\n"
}

// sourceLine returns line number n (1-based) of the program text as the
// tokenizer sees it: decoded and with every line break translated to "\n".
// Undecodable text is shown with U+FFFD for each bad byte.
func sourceLine(src []byte, n int) (string, bool) {
	if decoded, err := scanner.Decode(src); err == nil {
		src = decoded
	}
	src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
	src = bytes.ReplaceAll(src, []byte("\r"), []byte("\n"))
	lines := strings.Split(strings.ToValidUTF8(string(src), "�"), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if n < 1 || n > len(lines) {
		return "", false
	}
	return lines[n-1], true
}

// codePoints counts the code points in the first n bytes of text.
func codePoints(text string, n int) int {
	return utf8.RuneCountInString(text[:max(0, min(n, len(text)))])
}
