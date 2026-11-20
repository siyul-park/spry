package types

// Module is a Python module: a package of declarations and, as a Type, the
// type of the module object.
type Module struct {
	path    string
	name    string
	scope   *Scope
	imports []*Module
	init    *Func
}

// Path returns the import path of the module.
func (m *Module) Path() string {
	return m.path
}

// Name returns the dotted module name, such as collections.abc.
func (m *Module) Name() string {
	return m.name
}

// Scope returns the module scope.
func (m *Module) Scope() *Scope {
	return m.scope
}

// Imports returns the modules imported by m.
func (m *Module) Imports() []*Module {
	return append([]*Module(nil), m.imports...)
}

// Init returns the function that stands for the body of the module, which
// Info.Specs gives the specializations of. It is nil for a module that spry
// ships.
func (m *Module) Init() *Func {
	return m.init
}

// Underlying returns m.
func (m *Module) Underlying() Type {
	return m
}

// String implements Type.
func (m *Module) String() string {
	return TypeString(m, nil)
}

func newModule(path, name string) *Module {
	m := &Module{path: path, name: name}
	m.scope = newScope(ModuleScope, Universe)
	return m
}
