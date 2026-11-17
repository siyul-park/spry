package cli

import (
	"bytes"
	"errors"
	"io"

	"github.com/spf13/cobra"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/parser"
	"github.com/siyul-park/spry/scanner"
	"github.com/siyul-park/spry/token"
)

// Names CPython gives the source of a program that is not a file.
const (
	commandName = "<string>"
	stdinName   = "<stdin>"
)

// compile runs the stages after reading the source of a program. It prints
// the stage output that -X spry.dump asks for and a SyntaxError as CPython
// does, and returns ErrNotImplemented when execution would be next (nil when
// spry.dump.exit stops there).
func compile(cmd *cobra.Command, name string, src []byte) error {
	x := xoptionsOf(cmd)
	stderr := cmd.ErrOrStderr()
	if x.dumps(dumpTokens) {
		if text, err := scanner.Decode(src); err == nil {
			dumpTokenStream(stderr, text)
		}
	}
	fset := token.NewFileSet()
	mod, err := parser.ParseFile(fset, name, bytes.NewReader(src))
	if err != nil {
		return syntaxFailure(name, src, err)
	}
	if x.dumps(dumpAST) {
		_, _ = io.WriteString(stderr, ast.Dump(fset, mod, ast.WithIndent("   "))+"\n")
	}
	if x.exits() {
		return nil
	}
	return ErrNotImplemented
}

// syntaxFailure turns a parse failure into the SyntaxError report CPython
// prints, or returns err itself when it is not a syntax error.
func syntaxFailure(name string, src []byte, err error) error {
	var list scanner.ErrorList
	if !errors.As(err, &list) || len(list) == 0 {
		return err
	}
	return &syntaxError{text: renderSyntaxError(name, src, list[0])}
}
