package types_test

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/internal/cpython/corpus"
	"github.com/siyul-park/spry/parser"
	"github.com/siyul-park/spry/scanner"
	"github.com/siyul-park/spry/token"
	"github.com/siyul-park/spry/types"
)

type stubEntry struct {
	typ  types.Type
	text string
}

type checked struct {
	fset *token.FileSet
	file *ast.Module
	mod  *types.Module
	info *types.Info
	err  error
}

type anyWalker struct {
	t    *testing.T
	mod  *types.Module
	seen map[any]bool
	anys int
}

const sharedStubSource = `
import math
import sys
import typing
from typing import Any, Callable

def f(x: Any, ys: list[Any], g: Callable[[Any], Any]) -> Any:
    return math.floor(x) + len(ys) + sys.maxsize

xs: list[Any] = [1, "a"]
d: dict[str, Any] = {"k": xs}
n = f(1, xs, lambda a: a)
t: typing.Optional[Any] = None
print(sorted(xs), str(n), d.get("k"))
`

const canonicalAnySource = `
from typing import Any, Callable, TypeVar

T = TypeVar("T", bound=Any, default=Any)
Alias = list[Any]

class Box:
    item: Any
    items: list[Any]

    def get(self, key: Any) -> Any:
        return self.item.attr(key)

def explicit(x: Any, ys: list[Any], f: Callable[[Any], Any]) -> Any:
    local: Any = x
    return local.field

def provenance(x: Any):
    return x.field

def generic(v: T) -> T:
    return v

g: Any = 1
r = provenance(g)
b = Box()
n = explicit(g, [g], lambda a: a)
`

// checkCompile parses the case as CPython's compile does and runs Check on
// the tree. Single-mode bodies are checked as a module body: CPython applies
// the same symbol table and code generator checks to both modes.
func checkCompile(c corpus.Case) error {
	fset := token.NewFileSet()
	var body []ast.Stmt
	switch c.Mode {
	case "single":
		tree, err := parser.ParseInteractive(fset, "<case>", strings.NewReader(c.Src))
		if err != nil {
			return err
		}
		body = tree.Body
	default:
		tree, err := parser.ParseFile(fset, "<case>", strings.NewReader(c.Src))
		if err != nil {
			return err
		}
		body = tree.Body
	}
	_, _, err := types.Check(fset, "<case>", &ast.Module{Body: body})
	return err
}

func compileError(c corpus.Case, err error) error {
	var list scanner.ErrorList
	if !errors.As(err, &list) || len(list) == 0 {
		if c.Expect.Error == nil {
			return nil
		}
		return fmt.Errorf("accepted; CPython: %s", c.Expect.Error.Msg)
	}
	e := list[0]
	if c.Expect.Error == nil {
		return fmt.Errorf("rejected: %s", e.Msg)
	}
	want := c.Expect.Error
	text := []byte(strings.ReplaceAll(c.Src, "\r\n", "\n"))
	offset, end := charColumn(text, e.Pos), charColumn(text, e.End)-1
	switch {
	case e.Msg != want.Msg:
		return fmt.Errorf("message %q, want %q", e.Msg, want.Msg)
	case e.Pos.Line != want.Lineno || offset != want.Offset:
		return fmt.Errorf("%q: position (%d, %d), want (%d, %d)", e.Msg, e.Pos.Line, offset, want.Lineno, want.Offset)
	case e.End.Line != want.EndLineno || end != want.EndOffset:
		return fmt.Errorf("%q: end (%d, %d), want (%d, %d)", e.Msg, e.End.Line, end, want.EndLineno, want.EndOffset)
	}
	return nil
}

func charColumn(src []byte, p token.Position) int {
	if p.Column == 0 {
		return 0
	}
	end := min(p.Offset, len(src))
	return utf8.RuneCount(bytes.Clone(src[p.Offset-(p.Column-1):end])) + 1 + p.Offset - end
}

// withoutSubsetErrors drops the diagnostics of the subset (TYP110, TYP112,
// TYP150) from err: the tests of the type system build classes the checker
// does not compile.
func withoutSubsetErrors(err error) error {
	var list types.ErrorList
	if !errors.As(err, &list) {
		return err
	}
	var rest types.ErrorList
	for _, e := range list {
		if !slices.Contains([]string{"TYP110", "TYP112", "TYP150"}, e.Code) {
			rest = append(rest, e)
		}
	}
	return rest.Err()
}

func stubSnapshot(mod *types.Module) map[string]stubEntry {
	out := map[string]stubEntry{}
	var scope func(prefix string, s *types.Scope, depth int)
	scope = func(prefix string, s *types.Scope, depth int) {
		for _, name := range s.Names() {
			o := s.Lookup(name)
			key := prefix + name
			if _, dup := out[key]; dup {
				continue
			}
			if t := o.Type(); t != nil && !reflect.ValueOf(t).IsNil() {
				out[key] = stubEntry{o.Type(), o.Type().String()}
			}
			if depth == 0 {
				continue
			}
			if _, ok := o.(*types.TypeName); ok {
				if n, ok := o.Type().(*types.Named); ok && n.Origin() == n {
					if c, ok := n.Underlying().(*types.Class); ok && c != nil {
						scope(key+".", c.Members(), depth-1)
					}
				}
			}
		}
	}
	scope("builtins.", types.Universe, 1)
	for _, name := range []string{"math", "typing", "sys"} {
		if mn, ok := mod.Scope().Lookup(name).(*types.ModuleName); ok && mn.Imported() != nil {
			scope(name+".", mn.Imported().Scope(), 1)
		}
	}
	return out
}

func TestSite(t *testing.T) {
	c := check(t, "x = 1\n")
	stmt := c.file.Body[0]

	site := types.Site{Node: stmt, Kind: types.SiteTruth}

	require.Equal(t, stmt.Pos(), site.Pos())
	require.Equal(t, stmt.End(), site.End())
}

// TestCheck runs the checks of Check: symbol tables, compile-stage syntax
// errors, the loader, annotation evaluation and the CPython corpus.
func TestCheck(t *testing.T) {
	t.Run("symtable", checkSymtable)
	t.Run("syntax errors", checkSyntaxErrors)
	t.Run("load", checkLoad)
	t.Run("annotations", checkAnnotations)
	t.Run("subset", checkSubset)
	t.Run("fallbacks", checkFallbacks)
	t.Run("rejections", checkRejections)
	t.Run("redefined name", checkRedefinedName)
	t.Run("class inheriting itself", checkClassInheritingItself)
	t.Run("self-containing list", checkSelfContainingList)
	t.Run("class subset", checkClassSubset)
	t.Run("canonical any", checkCanonicalAny)
	t.Run("shared stubs", checkSharedStubs)
	t.Run("corpus", checkCorpus)
}

// checkCanonicalAny checks that no public value exposes the Any that the
// checker uses to remember an explicit annotation: every Any reachable from
// the declared objects and the facts is Typ[Any] itself.
func checkCanonicalAny(t *testing.T) {
	c := check(t, canonicalAnySource)
	w := &anyWalker{t: t, mod: c.mod, seen: map[any]bool{}}
	w.scope(c.mod.Scope())
	for _, tv := range c.info.Types {
		w.typ("expression", tv.Type)
	}
	for _, o := range c.info.Defs {
		w.object(o)
	}
	for _, o := range c.info.Uses {
		w.object(o)
	}
	for f, specs := range c.info.Specs {
		w.object(f)
		for _, s := range specs {
			if s.Signature != nil {
				w.typ("spec signature", s.Signature)
			}
			for v, vt := range s.Vars {
				w.object(v)
				w.typ("spec var", vt)
			}
			for _, tv := range s.Types {
				w.typ("spec expression", tv.Type)
			}
			for _, vt := range s.Cells {
				w.typ("spec cell", vt)
			}
			for i := range s.TypeArgs.Len() {
				w.typ("spec type argument", s.TypeArgs.At(i))
			}
		}
	}
	require.NotZero(t, w.anys, "the program must reach some Any")
}

func checkSharedStubs(t *testing.T) {
	t.Run("second check sees identical stub types", func(t *testing.T) {
		first := mustCheck(t, sharedStubSource)
		before := stubSnapshot(first.mod)
		require.NotEmpty(t, before)
		second := mustCheck(t, sharedStubSource)
		after := stubSnapshot(second.mod)
		require.Equal(t, len(before), len(after))
		for k, b := range before {
			require.Same(t, b.typ, after[k].typ, k)
			require.Equal(t, b.text, after[k].text, k)
		}
	})
	t.Run("concurrent checks", func(t *testing.T) {
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				fset := token.NewFileSet()
				file, err := parser.ParseFile(fset, "/app/main.py", strings.NewReader(sharedStubSource))
				if err != nil {
					t.Error(err)
					return
				}
				_, _, err = types.Check(fset, "/app/main.py", file)
				if err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
	})
}

func checkCorpus(t *testing.T) {
	xfail, err := corpus.LoadXFail("testdata/cpython/xfail.txt")
	require.NoError(t, err)
	for _, manifest := range []string{"snippets.jsonl", "syntax.jsonl"} {
		cases, err := corpus.Load("../parser/testdata/cpython/" + manifest)
		require.NoError(t, err)
		for _, c := range cases {
			if c.Mode != "exec" && c.Mode != "single" {
				continue
			}
			if c.Expect.Error != nil && c.Expect.Stage != "compile" || c.ID == "test_source_encoding/call/0001" {
				continue
			}
			t.Run(c.ID, func(t *testing.T) {
				xfail.Check(t, c.ID, compileError(c, checkCompile(c)))
			})
		}
	}
}

func mustCheck(t *testing.T, src string, opts ...types.Option) *checked {
	t.Helper()
	c := check(t, src, opts...)
	require.NoError(t, withoutSubsetErrors(c.err))
	return c
}

func check(t *testing.T, src string, opts ...types.Option) *checked {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "/app/main.py", strings.NewReader(src))
	require.NoError(t, err)
	mod, info, err := types.Check(fset, "/app/main.py", file, opts...)
	return &checked{fset: fset, file: file, mod: mod, info: info, err: err}
}

// node returns the first node named name in the tree: a function, class,
// lambda, "listcomp" or "genexpr".
func (c *checked) node(t *testing.T, name string) ast.Node {
	t.Helper()
	var found ast.Node
	ast.Inspect(c.file, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		switch n := n.(type) {
		case *ast.FunctionDef:
			if n.Name == name {
				found = n
			}
		case *ast.AsyncFunctionDef:
			if n.Name == name {
				found = n
			}
		case *ast.ClassDef:
			if n.Name == name {
				found = n
			}
		case *ast.Lambda:
			if name == "lambda" {
				found = n
			}
		case *ast.ListComp:
			if name == "listcomp" {
				found = n
			}
		case *ast.GeneratorExp:
			if name == "genexpr" {
				found = n
			}
		}
		return true
	})
	require.NotNil(t, found, "no %s in the tree", name)
	return found
}

// scope returns the scope of the first node named name in the tree.
func (c *checked) scope(t *testing.T, name string) *types.Scope {
	t.Helper()
	s := c.info.Scopes[c.node(t, name)]
	require.NotNil(t, s, "no scope for %s", name)
	return s
}

func (c *checked) typeOf(t *testing.T, name string) types.Type {
	t.Helper()
	obj := c.mod.Scope().Lookup(name)
	require.NotNil(t, obj, "%s is not declared", name)
	return obj.Type()
}

func syntaxError(t *testing.T, err error) *scanner.Error {
	t.Helper()
	var list scanner.ErrorList
	require.True(t, errors.As(err, &list), "error %v is not a scanner.ErrorList", err)
	return list[0]
}

func (w *anyWalker) scope(s *types.Scope) {
	if s == nil || w.seen[s] {
		return
	}
	w.seen[s] = true
	for _, name := range s.Names() {
		w.object(s.Lookup(name))
	}
}

func (w *anyWalker) object(o types.Object) {
	if o == nil || w.seen[o] {
		return
	}
	w.seen[o] = true
	w.typ(o.Name(), o.Type())
	switch o := o.(type) {
	case *types.Func:
		if sig := o.Signature(); sig != nil {
			w.typ(o.Name()+" signature", sig)
		}
	case *types.Var:
		if v := o.Origin(); v != o {
			w.object(v)
		}
	}
}

func (w *anyWalker) typ(what string, t types.Type) {
	switch t := t.(type) {
	case nil:
		return
	case *types.Basic:
		if t.Kind() == types.Any {
			w.anys++
			require.Same(w.t, types.Typ[types.Any], t, "%s exposes a provenance Any", what)
		}
		return
	}
	if w.seen[t] {
		return
	}
	w.seen[t] = true
	switch t := t.(type) {
	case *types.Alias:
		w.typ(what, t.Rhs())
		w.list(what, t.TypeArgs())
	case *types.Named:
		w.list(what, t.TypeArgs())
		if t.Obj().Module() == w.mod {
			w.typ(what, t.Underlying())
		}
	case *types.Class:
		for _, b := range t.Bases() {
			w.typ(what+" base", b)
		}
		for _, f := range t.Fields() {
			w.object(f)
		}
		w.scope(t.Members())
	case *types.Union:
		for i := range t.Len() {
			w.typ(what, t.Term(i))
		}
	case *types.Tuple:
		for i := range t.Len() {
			w.object(t.At(i))
		}
	case *types.Signature:
		w.typ(what+" params", t.Params())
		w.typ(what+" result", t.Result())
		for i := range t.TypeParams().Len() {
			w.typ(what, t.TypeParams().At(i))
		}
	case *types.Overload:
		for i := range t.Len() {
			w.typ(what, t.At(i))
		}
	case *types.Meta:
		w.typ(what, t.Elem())
	case *types.Guard:
		w.typ(what, t.Type())
	case *types.TypeParam:
		w.typ(what+" bound", t.Bound())
		w.typ(what+" default", t.Default())
		for _, c := range t.Constraints() {
			w.typ(what+" constraint", c)
		}
	}
}

func (w *anyWalker) list(what string, l *types.TypeList) {
	for i := range l.Len() {
		w.typ(what, l.At(i))
	}
}
