package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/parser"
	"github.com/siyul-park/spry/scanner"
	"github.com/siyul-park/spry/ssa"
	"github.com/siyul-park/spry/token"
	"github.com/siyul-park/spry/types"
)

// Names CPython gives the source of a program that is not a file.
const (
	commandName = "<string>"
	stdinName   = "<stdin>"
)

// compile runs the stages after reading the source of a program: parse,
// type check and SSA construction. It prints the stage output that -X
// spry.dump asks for, in pipeline order, and reports a SyntaxError as CPython
// does and a type diagnostic as "file:line:col: error TYPnnn: message". It
// returns ErrNotImplemented when execution would be next. With spry.dump.exit
// it stops after the last stage dumped (after parse when none is) and returns
// nil.
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
	last := x.lastStage()
	if x.exits() && (last == "" || last == dumpTokens || last == dumpAST) {
		return nil
	}

	dir, err := programDir(cmd, name)
	if err != nil {
		return err
	}
	checked, info, err := types.Check(fset, name, mod, types.WithFS(os.DirFS(dir), dir))
	if x.dumps(dumpTypes) && checked != nil && info != nil {
		_, _ = io.WriteString(stderr, types.Dump(checked, info))
	}
	if err != nil {
		return typeFailure(name, src, err)
	}
	if x.exits() && last == dumpTypes {
		return nil
	}

	prog, err := ssa.Build(fset, checked, info)
	if err != nil {
		return fmt.Errorf("internal error: %w", err)
	}
	if x.dumps(dumpSSA) {
		if err := ssa.Print(stderr, prog); err != nil {
			return fmt.Errorf("internal error: %w", err)
		}
	}
	if x.exits() {
		return nil
	}
	return ErrNotImplemented
}

// programDir is the directory the program's imports are searched in first:
// the directory of a script, and the working directory for -c and stdin.
func programDir(cmd *cobra.Command, name string) (string, error) {
	if name != commandName && name != stdinName {
		return filepath.Dir(name), nil
	}
	if dir := processOf(cmd.Context()).Dir; dir != "" {
		return dir, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine working directory: %w", err)
	}
	return dir, nil
}

// typeFailure reports the failure of types.Check: a compile-stage SyntaxError
// of the main module as CPython prints it, or each type diagnostic on its own
// line as types.Error renders it, without the "spry: " prefix.
func typeFailure(name string, src []byte, err error) error {
	var syntax scanner.ErrorList
	if errors.As(err, &syntax) {
		// The error comes from the compiler, not the parser: CPython reports
		// no source text for it, and traceback finds none for a program
		// that is not a file.
		if name == commandName || name == stdinName {
			src = nil
		}
		return syntaxFailure(name, src, err)
	}
	var list types.ErrorList
	if !errors.As(err, &list) {
		return err
	}
	var out strings.Builder
	for _, e := range list {
		out.WriteString(e.Error() + "\n")
	}
	return &syntaxError{text: out.String()}
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
