package types

import (
	"bytes"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/siyul-park/spry/ast"
	"github.com/siyul-park/spry/parser"
	"github.com/siyul-park/spry/token"
)

// program is the set of modules reachable from the main module: parsed,
// symbol-tabled and linked by import, in the deterministic depth-first
// post-order in which Python would finish importing them, main last.
type program struct {
	fset     *token.FileSet
	roots    []root
	info     *Info
	mods     map[string]*source
	failed   map[string]importResult
	order    []*source
	declared int
	decl     *declarer
	errs     ErrorList
	stubs    bool
}

// root is one sys.path entry.
type root struct {
	fsys fs.FS
	dir  string
}

// source is a loaded module together with its tree and symbol table.
type source struct {
	mod  *Module
	file *ast.Module
	st   *symtab
	pkg  bool
	stub bool
}

// importResult is what an import site resolved to. A site that cannot
// import is not an error of the program: the builder lowers it to the raise
// Python would execute.
type importResult struct {
	kind   ImportKind
	chain  []*Module
	err    error
	module *Module
}

type errString string

var errorCodes = map[string]string{"importlib": "TYP121", "inspect": "TYP130", "gc": "TYP130"}

func (e errString) Error() string {
	return string(e)
}

func newProgram(fset *token.FileSet, info *Info, roots []root) *program {
	return &program{fset: fset, roots: roots, info: info, mods: map[string]*source{}, failed: map[string]importResult{}}
}

// addMain parses nothing: it registers the already parsed main module, then
// loads everything it imports. It returns the compile-stage error of the
// main module, if any.
func (p *program) addMain(filename string, file *ast.Module) (*source, error) {
	st, err := analyzeSyntax(p.fset, file, file.Body)
	m := newModule(filename, "__main__")
	src := &source{mod: m, file: file, st: st}
	if err != nil {
		return src, scannerList(err)
	}
	p.mods["__main__"] = src
	p.scan(src)
	p.order = append(p.order, src)
	return src, nil
}

func (p *program) scan(src *source) {
	p.scanStmts(src, src.file.Body, false)
}

func (p *program) scanStmts(src *source, list []ast.Stmt, guarded bool) {
	for _, s := range list {
		switch s := s.(type) {
		case *ast.Import:
			for _, a := range s.Names {
				res := p.require(a.Name)
				p.record(src, a, a, res, guarded)
			}
		case *ast.ImportFrom:
			p.importFrom(src, s, guarded)
		case *ast.Try:
			p.scanStmts(src, s.Body, guarded || catchesImport(s.Handlers))
			p.scanHandlers(src, s.Handlers, s.OrElse, s.FinalBody, guarded)
		case *ast.TryStar:
			p.scanStmts(src, s.Body, guarded || catchesImport(s.Handlers))
			p.scanHandlers(src, s.Handlers, s.OrElse, s.FinalBody, guarded)
		default:
			for _, body := range nestedBodies(s) {
				p.scanStmts(src, body, guarded)
			}
		}
	}
}

func (p *program) scanHandlers(src *source, handlers []*ast.ExceptHandler, orElse, final []ast.Stmt, guarded bool) {
	for _, h := range handlers {
		p.scanStmts(src, h.Body, guarded)
	}
	p.scanStmts(src, orElse, guarded)
	p.scanStmts(src, final, guarded)
}

func (p *program) importFrom(src *source, s *ast.ImportFrom, guarded bool) {
	level, name := 0, ""
	if s.Level != nil {
		level = *s.Level
	}
	if s.Module != nil {
		name = *s.Module
	}
	res := p.fromModule(src, level, name)
	p.record(src, s, s, res, guarded)
	if res.kind != ImportOK {
		return
	}
	base := res.module
	for _, a := range s.Names {
		if a.Name == "*" || !p.isPackage(base) || p.definesName(base, a.Name) {
			continue
		}
		sub := p.require(base.name + "." + a.Name)
		if sub.kind == ImportOK {
			p.info.imports[a] = sub
			p.link(src.mod, sub.chain)
		}
	}
}

func (p *program) fromModule(src *source, level int, name string) importResult {
	if level == 0 {
		return p.require(name)
	}
	pkg := src.mod.name
	if !src.pkg {
		pkg = parentName(pkg)
	}
	if src.mod.name == "__main__" || pkg == "" {
		return importResult{kind: ImportRelative}
	}
	for range level - 1 {
		if pkg == "" {
			return importResult{kind: ImportRelative}
		}
		pkg = parentName(pkg)
	}
	if pkg == "" {
		return importResult{kind: ImportRelative}
	}
	if name != "" {
		pkg += "." + name
	}
	return p.require(pkg)
}

// require imports the dotted name, parents first.
func (p *program) require(name string) importResult {
	parts := strings.Split(name, ".")
	var chain []*Module
	for i := range parts {
		m, res := p.module(strings.Join(parts[:i+1], "."), name)
		if res.kind != ImportOK {
			return res
		}
		chain = append(chain, m)
	}
	return importResult{kind: ImportOK, chain: chain, module: chain[len(chain)-1]}
}

func (p *program) module(name, full string) (*Module, importResult) {
	if s := p.mods[name]; s != nil {
		return s.mod, importResult{}
	}
	if res, ok := p.failed[name]; ok {
		return nil, res
	}
	m, res := p.find(name, full)
	if res.kind != ImportOK {
		p.failed[name] = res
	}
	return m, res
}

func (p *program) find(name, full string) (*Module, importResult) {
	if parent := parentName(name); parent != "" && !p.isPackage(p.mods[parent].modOrNil()) {
		return nil, importResult{kind: ImportNotFound}
	}
	if (name == "builtins" || name == "sys") && !p.stubs {
		return p.adopt(name, shippedModule(name))
	}
	rel := strings.ReplaceAll(name, ".", "/")
	for _, r := range p.roots {
		for _, cand := range []string{rel + "/__init__.py", rel + ".py"} {
			if data, err := fs.ReadFile(r.fsys, cand); err == nil {
				return p.parse(name, path.Join(r.dir, cand), data, strings.HasSuffix(cand, "__init__.py"))
			}
		}
	}
	if !p.stubs {
		if m := shippedModule(name); m != nil {
			return p.adopt(name, m)
		}
	}
	if top := strings.SplitN(full, ".", 2)[0]; stdlibModules[top] && !p.stubs {
		return nil, importResult{kind: ImportUnshipped, err: errString(top + " is a standard library module")}
	}
	return nil, importResult{kind: ImportNotFound}
}

func (p *program) parse(name, filename string, data []byte, pkg bool) (*Module, importResult) {
	file, err := parser.ParseFile(p.fset, filename, bytes.NewReader(data))
	if err != nil {
		return nil, importResult{kind: ImportSyntax, err: err}
	}
	st, serr := analyzeSyntax(p.fset, file, file.Body)
	if serr != nil {
		return nil, importResult{kind: ImportSyntax, err: scannerList(serr)}
	}
	m := newModule(filename, name)
	if p.stubs && name == "builtins" {
		m.scope = Universe
	}
	src := &source{mod: m, file: file, st: st, pkg: pkg}
	p.mods[name] = src
	p.scan(src)
	p.order = append(p.order, src)
	return m, importResult{}
}

func nestedBodies(s ast.Stmt) [][]ast.Stmt {
	switch s := s.(type) {
	case *ast.FunctionDef:
		return [][]ast.Stmt{s.Body}
	case *ast.AsyncFunctionDef:
		return [][]ast.Stmt{s.Body}
	case *ast.ClassDef:
		return [][]ast.Stmt{s.Body}
	case *ast.If:
		return [][]ast.Stmt{s.Body, s.OrElse}
	case *ast.While:
		return [][]ast.Stmt{s.Body, s.OrElse}
	case *ast.For:
		return [][]ast.Stmt{s.Body, s.OrElse}
	case *ast.AsyncFor:
		return [][]ast.Stmt{s.Body, s.OrElse}
	case *ast.With:
		return [][]ast.Stmt{s.Body}
	case *ast.AsyncWith:
		return [][]ast.Stmt{s.Body}
	case *ast.Match:
		var out [][]ast.Stmt
		for _, c := range s.Cases {
			out = append(out, c.Body)
		}
		return out
	}
	return nil
}

func catchesImport(handlers []*ast.ExceptHandler) bool {
	for _, h := range handlers {
		if h.Type == nil {
			return true
		}
		types := []ast.Expr{h.Type}
		if t, ok := h.Type.(*ast.Tuple); ok {
			types = t.Elts
		}
		for _, e := range types {
			if n, ok := e.(*ast.Name); ok && slices.Contains([]string{"ImportError", "ModuleNotFoundError", "Exception", "BaseException"}, n.ID) {
				return true
			}
		}
	}
	return false
}

func (p *program) record(src *source, site ast.Node, at ast.Node, res importResult, guarded bool) {
	if res.kind == ImportUnshipped {
		p.reject(src, at, res, guarded)
	}
	p.info.imports[site] = res
	p.link(src.mod, res.chain)
}

func (p *program) reject(src *source, at ast.Node, res importResult, guarded bool) {
	top, _, _ := strings.Cut(res.err.Error(), " ")
	code, forced := errorCodes[top]
	if !forced && guarded {
		return
	}
	if !forced {
		code = "TYP121"
	}
	msg := "module " + top + " is not shipped with spry"
	if code == "TYP130" {
		msg = "module " + top + " inspects frames or the collector, which the compiled program does not have"
	}
	p.errs.Add(p.fset.Position(at.Pos()), p.fset.Position(at.End()), code, msg)
}

func (p *program) link(m *Module, chain []*Module) {
	for _, c := range chain {
		if c != m && !slices.Contains(m.imports, c) {
			m.imports = append(m.imports, c)
		}
	}
}

func (p *program) isPackage(m *Module) bool {
	for _, s := range p.mods {
		if s.mod == m {
			return s.pkg
		}
	}
	return isShippedPackage(m)
}

func (p *program) definesName(m *Module, name string) bool {
	for _, s := range p.mods {
		if s.mod == m && s.st != nil && s.st.top != nil {
			return s.st.top.flagsOf(name)&defBound != 0
		}
	}
	return m.scope.Lookup(name) != nil
}

func (p *program) adopt(name string, m *Module) (*Module, importResult) {
	if m == nil {
		return nil, importResult{kind: ImportNotFound}
	}
	p.mods[name] = &source{mod: m, stub: true, pkg: isShippedPackage(m)}
	return m, importResult{}
}

func (s *source) modOrNil() *Module {
	if s == nil {
		return nil
	}
	return s.mod
}

func parentName(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i < 0 {
		return ""
	}
	return name[:i]
}
