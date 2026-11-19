package types

// formKind identifies a special form or function that a stub declaration
// cannot express: it is checked by the checker, not called.
type formKind uint8

const (
	formNone formKind = iota
	formAny
	formNever
	formLiteralString
	formSelf
	formOptional
	formUnion
	formLiteral
	formFinal
	formClassVar
	formReadOnly
	formRequired
	formNotRequired
	formInitVar
	formAnnotated
	formTypeAlias
	formCallable
	formConcatenate
	formTypeGuard
	formTypeIs
	formUnpack
	formProtocol
	formGeneric
	formTypeVar
	formParamSpec
	formTypeVarTuple
	formNewType
	formNamedTuple
	formTypedDict
	formCast
	formOverload
	formFinalDecorator
	formRuntimeCheckable
	formIsinstance
	formIssubclass
	formSuper
)

// forms maps "module.name" of a stub declaration to the special form it is.
var forms = map[string]formKind{
	"typing.Any":               formAny,
	"typing.Never":             formNever,
	"typing.NoReturn":          formNever,
	"typing.LiteralString":     formLiteralString,
	"typing.Self":              formSelf,
	"typing.Optional":          formOptional,
	"typing.Union":             formUnion,
	"typing.Literal":           formLiteral,
	"typing.Final":             formFinal,
	"typing.ClassVar":          formClassVar,
	"typing.ReadOnly":          formReadOnly,
	"typing.Required":          formRequired,
	"typing.NotRequired":       formNotRequired,
	"typing.InitVar":           formInitVar,
	"typing.Annotated":         formAnnotated,
	"typing.TypeAlias":         formTypeAlias,
	"collections.abc.Callable": formCallable,
	"typing.Concatenate":       formConcatenate,
	"typing.TypeGuard":         formTypeGuard,
	"typing.TypeIs":            formTypeIs,
	"typing.Unpack":            formUnpack,
	"typing.Protocol":          formProtocol,
	"typing.Generic":           formGeneric,
	"typing.TypeVar":           formTypeVar,
	"typing.ParamSpec":         formParamSpec,
	"typing.TypeVarTuple":      formTypeVarTuple,
	"typing.NewType":           formNewType,
	"typing.NamedTuple":        formNamedTuple,
	"typing.TypedDict":         formTypedDict,
	"typing.cast":              formCast,
	"typing.overload":          formOverload,
	"typing.final":             formFinalDecorator,
	"typing.runtime_checkable": formRuntimeCheckable,
	"builtins.isinstance":      formIsinstance,
	"builtins.issubclass":      formIssubclass,
	"builtins.super":           formSuper,
}

func formOf(module, name string) formKind {
	return forms[module+"."+name]
}
