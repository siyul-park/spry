package parser_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/internal/cpython/corpus"
	"github.com/siyul-park/spry/parser"
	"github.com/siyul-park/spry/scanner"
	"github.com/siyul-park/spry/token"
)

var update = flag.Bool("update", false, "rewrite the testdata/ast/*.ast goldens")

// divergences are corpus cases that are intentionally not compared, with the
// reason. They are documented in docs/frontend.md.
var divergences = map[string]string{
	// The case source is a str, for which CPython ignores the coding cookie;
	// the parser reads bytes and honors the cookie as compile does for bytes.
	"test_source_encoding/call/0001": "str source ignores the coding cookie",
}

// dump parses src in the mode of c and formats the tree as the oracle driver
// does: ast.dump(tree, include_attributes=True, indent=1).
func dump(mode string, src []byte) (string, error) {
	fset := token.NewFileSet()
	var node ast.Node
	var err error
	switch mode {
	case "eval":
		node, err = parser.ParseExpr(fset, "<case>", strings.NewReader(string(src)))
	case "single":
		node, err = parser.ParseInteractive(fset, "<case>", strings.NewReader(string(src)))
	default:
		node, err = parser.ParseFile(fset, "<case>", strings.NewReader(string(src)))
	}
	if err != nil {
		return "", err
	}
	return ast.Dump(fset, node, ast.WithIndent(" "), ast.WithAttributes()), nil
}

// checkParse compares spry with the recorded CPython outcome: accepted inputs
// must dump identically (by hash for whole files), rejected ones must fail
// with the same exception type, message and position. Compile-stage errors are
// parse successes.
func checkParse(c corpus.Case, src []byte) error {
	got, err := dump(c.Mode, src)
	if c.Expect.Error != nil && c.Expect.Stage != "compile" {
		if err == nil {
			return fmt.Errorf("accepted; CPython: %s: %s", c.Expect.Error.Type, c.Expect.Error.Msg)
		}
		return checkError(c.Expect.Error, src, err)
	}
	if err != nil {
		return err
	}
	if c.Expect.Hash != "" {
		sum := sha256.Sum256([]byte(got))
		if h := hex.EncodeToString(sum[:]); h != c.Expect.Hash {
			return fmt.Errorf("dump hash %s, want %s", h, c.Expect.Hash)
		}
		return nil
	}
	if got != c.Expect.Dump {
		return fmt.Errorf("dump differs:\n%s\nwant:\n%s", got, c.Expect.Dump)
	}
	return nil
}

// category returns the CPython exception class name of err.
func category(err error) string {
	switch {
	case errors.Is(err, scanner.ErrTab):
		return "TabError"
	case errors.Is(err, scanner.ErrIndentation):
		return "IndentationError"
	case errors.Is(err, scanner.ErrSyntax):
		return "SyntaxError"
	}
	return "other"
}

// checkError compares the first error of err with the recorded exception: the
// class, the message and the position CPython reports. Positions of the
// manifest that are absent or not positive are not compared.
func checkError(want *corpus.SyntaxError, src []byte, err error) error {
	var list scanner.ErrorList
	if !errors.As(err, &list) || len(list) == 0 {
		return fmt.Errorf("error is not a scanner.ErrorList: %w", err)
	}
	e := list[0]
	if got := category(e); got != want.Type {
		return fmt.Errorf("error type %s, want %s (%s)", got, want.Type, e.Msg)
	}
	if e.Msg != want.Msg {
		return fmt.Errorf("error message %q, want %q", e.Msg, want.Msg)
	}
	text := normalize(src)
	offset, end := charColumn(text, e.Pos), charColumn(text, e.End)
	end-- // scanner.Error.End: CPython's end_offset is End.Column-1
	switch {
	case want.Lineno > 0 && e.Pos.Line != want.Lineno:
		return fmt.Errorf("%q: line %d, want %d", e.Msg, e.Pos.Line, want.Lineno)
	case want.Offset > 0 && offset != want.Offset:
		return fmt.Errorf("%q: offset %d, want %d", e.Msg, offset, want.Offset)
	case want.EndLineno > 0 && e.End.Line != want.EndLineno:
		return fmt.Errorf("%q: end line %d, want %d", e.Msg, e.End.Line, want.EndLineno)
	case want.EndOffset > 0 && end != want.EndOffset:
		return fmt.Errorf("%q: end offset %d, want %d", e.Msg, end, want.EndOffset)
	}
	return nil
}

// normalize returns the text the parser tokenizes: decoded, with line breaks
// translated.
func normalize(src []byte) []byte {
	if text, err := scanner.Decode(src); err == nil {
		src = text
	} else {
		src = bytes.TrimPrefix(src, []byte("\xef\xbb\xbf"))
	}
	src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(src, []byte("\r"), []byte("\n"))
}

// charColumn converts a byte column to CPython's code point offset.
func charColumn(src []byte, p token.Position) int {
	if p.Column == 0 {
		return 0
	}
	end := min(p.Offset, len(src))
	return utf8.RuneCount(src[p.Offset-(p.Column-1):end]) + 1 + p.Offset - end
}

// describe formats the first error of err as the goldens do.
func describe(src []byte, err error) string {
	var list scanner.ErrorList
	if !errors.As(err, &list) {
		return err.Error()
	}
	e := list[0]
	text := normalize(src)
	end := charColumn(text, e.End) - 1
	if strings.HasSuffix(e.Msg, " was never closed") {
		end = 0 // CPython reports no end for an unclosed bracket
	}
	return fmt.Sprintf("%s: %s (%d, %d)-(%d, %d)", category(e), e.Msg, e.Pos.Line, charColumn(text, e.Pos), e.End.Line, end)
}

func TestCorpus_Snippets(t *testing.T) {
	testCorpus(t, "snippets.jsonl")
}

func TestCorpus_Syntax(t *testing.T) {
	testCorpus(t, "syntax.jsonl")
}

func TestCorpus_Files(t *testing.T) {
	testCorpus(t, "files.jsonl")
}

func testCorpus(t *testing.T, manifest string) {
	cases, err := corpus.Load("testdata/cpython/" + manifest)
	require.NoError(t, err)
	xfail, err := corpus.LoadXFail("testdata/cpython/xfail.txt")
	require.NoError(t, err)
	_, haveCache := corpus.Dir()

	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			if reason, ok := divergences[c.ID]; ok {
				t.Skip(reason)
			}
			if c.File != "" && !haveCache {
				t.Skip("cpython corpus not fetched: run make corpus")
			}
			src, err := c.Source()
			require.NoError(t, err)
			xfail.Check(t, c.ID, checkParse(c, src))
		})
	}
}

// TestGolden_AST compares testdata/ast/*.py with the dumps checked in next to
// them. Each golden was verified against python3.14's ast.dump when it was
// generated with -update.
func TestGolden_AST(t *testing.T) {
	files, err := filepath.Glob("testdata/ast/*.py")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			src, err := os.ReadFile(file)
			require.NoError(t, err)
			got, err := dump("exec", src)
			require.NoError(t, err)
			golden := strings.TrimSuffix(file, ".py") + ".ast"
			if *update {
				require.NoError(t, os.WriteFile(golden, []byte(got+"\n"), 0o644))
			}
			want, err := os.ReadFile(golden)
			require.NoError(t, err)
			require.Equal(t, strings.TrimSuffix(string(want), "\n"), got)
		})
	}
}

// TestGolden_Errors compares testdata/errors/*.py with the first error
// checked in next to them, in the form "Type: msg (lineno, offset)-(end_lineno,
// end_offset)" with CPython's code point columns. Each golden was verified
// against python3.14 when it was generated with -update.
func TestGolden_Errors(t *testing.T) {
	files, err := filepath.Glob("testdata/errors/*.py")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			src, err := os.ReadFile(file)
			require.NoError(t, err)
			_, err = dump("exec", src)
			require.Error(t, err)
			got := describe(src, err)
			golden := strings.TrimSuffix(file, ".py") + ".err"
			if *update {
				require.NoError(t, os.WriteFile(golden, []byte(got+"\n"), 0o644))
			}
			want, err := os.ReadFile(golden)
			require.NoError(t, err)
			require.Equal(t, strings.TrimSuffix(string(want), "\n"), got)
		})
	}
}
