package types

// Scope is a lexical scope: the declarations of a module, class, function,
// comprehension, type-parameter list or annotation, in declaration order.
type Scope struct {
	kind   ScopeKind
	parent *Scope
	names  []string
	objs   map[string]Object
	syms   map[string]storage
}

// ScopeKind classifies a Scope.
type ScopeKind uint8

// Scope kinds.
const (
	UniverseScope ScopeKind = iota
	ModuleScope
	ClassScope
	FunctionScope
	ComprehensionScope
	TypeParamScope
	AnnotationScope
)

// Universe is the scope of the builtins module, the parent of every module
// scope.
var Universe = newScope(UniverseScope, nil)

// Kind returns the kind of s.
func (s *Scope) Kind() ScopeKind {
	return s.kind
}

// Parent returns the enclosing scope, or nil for the universe.
func (s *Scope) Parent() *Scope {
	return s.parent
}

// Lookup returns the object declared in s under name, or nil.
func (s *Scope) Lookup(name string) Object {
	return s.objs[name]
}

// LookupParent looks name up in s and then in the enclosing scopes the way
// CPython resolves names, and returns the scope that declares it. A class
// scope is skipped unless every scope between it and s is a type-parameter or
// annotation scope.
func (s *Scope) LookupParent(name string) (*Scope, Object) {
	sees := true
	for c := s; c != nil; c = c.parent {
		if c.kind != ClassScope || c == s || sees {
			if obj := c.objs[name]; obj != nil {
				return c, obj
			}
		}
		sees = sees && (c.kind == TypeParamScope || c.kind == AnnotationScope)
	}
	return nil, nil
}

// Names returns the declared names in declaration order.
func (s *Scope) Names() []string {
	return append([]string(nil), s.names...)
}

func newScope(kind ScopeKind, parent *Scope) *Scope {
	return &Scope{kind: kind, parent: parent, objs: map[string]Object{}}
}

func (s *Scope) insert(obj Object) Object {
	if prev := s.objs[obj.Name()]; prev != nil {
		return prev
	}
	s.objs[obj.Name()] = obj
	s.names = append(s.names, obj.Name())
	obj.setParent(s)
	return nil
}

func (s *Scope) bindAlias(name string, obj Object) {
	if _, ok := s.objs[name]; !ok {
		s.names = append(s.names, name)
	}
	s.objs[name] = obj
}

func (s *Scope) replace(obj Object) {
	s.objs[obj.Name()] = obj
	obj.setParent(s)
}

func (s *Scope) storage(name string) storage {
	return s.syms[name]
}
