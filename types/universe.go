package types

import (
	"embed"
	"io/fs"
	"strings"
	"sync"

	"github.com/siyul-park/spry/token"
)

//go:embed all:lib
var libFS embed.FS

// stubs is the program that holds the modules shipped with spry. Builtins and
// the modules they import are declared when the package is initialized, so
// that Universe and the builtin classes exist for every other file; the
// other shipped modules are declared when a program first imports them.
// Initializing eagerly costs one parse of the builtins stub, which every
// program needs anyway; a lazy Universe would have to be a function and
// change the API.
var stubs struct {
	mu   sync.Mutex
	prog *program
}

func init() {
	sub, err := fs.Sub(libFS, "lib")
	if err != nil {
		panic(err)
	}
	p := newProgram(token.NewFileSet(), newInfo(), []root{{fsys: sub, dir: "<stub>"}})
	p.stubs = true
	stubs.prog = p
	if res := p.require("builtins"); res.kind != ImportOK {
		panic(stubFailure("builtins", res))
	}
	p.declare()
	if len(p.errs) > 0 {
		panic(stubErrors(p.errs))
	}
}

func stubErrors(list ErrorList) string {
	var b strings.Builder
	for _, e := range list {
		b.WriteString(e.Error() + "\n")
	}
	return b.String()
}

func stubFailure(name string, res importResult) error {
	if res.err != nil {
		return res.err
	}
	return errString("stub module " + name + " not found")
}

// shippedModule returns the module of that name shipped with spry, declaring
// it first if needed, or nil.
func shippedModule(name string) *Module {
	stubs.mu.Lock()
	defer stubs.mu.Unlock()
	res := stubs.prog.require(name)
	if res.kind != ImportOK {
		return nil
	}
	stubs.prog.declare()
	if err := stubs.prog.errs.Err(); err != nil {
		panic(err)
	}
	return res.module
}

func isShippedPackage(m *Module) bool {
	if m == nil {
		return false
	}
	stubs.mu.Lock()
	defer stubs.mu.Unlock()
	return stubs.prog.isPackage(m)
}
