package types

// ParamSpecArgs is the type P.args or P.kwargs of the variadic parameters of a
// signature that forwards the ParamSpec P.
type ParamSpecArgs struct {
	param  *TypeParam
	kwargs bool
}

// TypeParam is a type variable, parameter specification or variadic type
// variable, declared with TypeVar(...) or PEP 695 syntax, or implicit in a
// specialized function. Each class also has a synthesized Self parameter.
type TypeParam struct {
	obj         *TypeName
	index       int
	kind        TypeParamKind
	bound       Type
	constraints []Type
	def         Type
	variance    Variance
	inferred    bool
	implicit    bool
	isSelf      bool
}

// TypeParamKind distinguishes the three kinds of type parameter.
type TypeParamKind uint8

// Variance is the declared or inferred variance of a type parameter.
type Variance uint8

// Type parameter kinds.
const (
	TypeVar TypeParamKind = iota
	ParamSpec
	TypeVarTuple
)

// Variances.
const (
	Invariant Variance = iota
	Covariant
	Contravariant
)

// ParamSpec returns the ParamSpec that p forwards.
func (a *ParamSpecArgs) ParamSpec() *TypeParam {
	return a.param
}

// Kwargs reports whether a is P.kwargs rather than P.args.
func (a *ParamSpecArgs) Kwargs() bool {
	return a.kwargs
}

// Underlying returns a.
func (a *ParamSpecArgs) Underlying() Type {
	return a
}

// Obj returns the declaration of the parameter.
func (p *TypeParam) Obj() *TypeName {
	return p.obj
}

// Index returns the position of p in its parameter list.
func (p *TypeParam) Index() int {
	return p.index
}

// Kind returns the kind of p.
func (p *TypeParam) Kind() TypeParamKind {
	return p.kind
}

// Bound returns the upper bound, or nil.
func (p *TypeParam) Bound() Type {
	return p.bound
}

// Constraints returns the value restriction of a constrained TypeVar.
func (p *TypeParam) Constraints() []Type {
	return append([]Type(nil), p.constraints...)
}

// Default returns the PEP 696 default, or nil.
func (p *TypeParam) Default() Type {
	return p.def
}

// Variance returns the variance, including one inferred from the uses.
func (p *TypeParam) Variance() Variance {
	return p.variance
}

// Implicit reports whether p stands for a parameter or capture without a
// declared type in a specialized function.
func (p *TypeParam) Implicit() bool {
	return p.implicit
}

// Underlying returns the underlying type of the bound, or of object.
func (p *TypeParam) Underlying() Type {
	if p.bound != nil {
		return p.bound.Underlying()
	}
	return underlyingOf(builtin("object"), p)
}

// String implements Type.
func (a *ParamSpecArgs) String() string {
	return TypeString(a, nil)
}

// String implements Type.
func (p *TypeParam) String() string {
	return TypeString(p, nil)
}

func newTypeParam(module *Module, name string, kind TypeParamKind) *TypeParam {
	p := &TypeParam{kind: kind}
	p.obj = newTypeName(0, module, name, p)
	return p
}

func newParamSpecArgs(p *TypeParam, kwargs bool) *ParamSpecArgs {
	return &ParamSpecArgs{param: p, kwargs: kwargs}
}
