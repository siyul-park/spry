package types

// The reasons of a Fallback (TYP200). The set is closed: every site where an
// operation is dispatched at run time on an Any operand names one of these,
// and docs/typing.md lists them. An explicit Any annotation is not a reason:
// it opts out of the report.
const (
	// reasonParamConflict: a parameter was widened because its call sites
	// disagree beyond the caps.
	reasonParamConflict = "param-conflict"
	// reasonCapExceeded: a type was widened to Any past the union or depth cap.
	reasonCapExceeded = "cap-exceeded"
	// reasonUnion: the operation is dispatched on a union of operand types.
	reasonUnion = "union"
	// reasonEscape: the value flows through code the analysis cannot follow, so
	// the function version that handles it takes Any parameters.
	reasonEscape = "escape"
	// reasonDynamicAttr: the class computes its attributes (__getattr__) or
	// the attribute is a property that cannot be resolved.
	reasonDynamicAttr = "dynamic-attr"
	// reasonUnresolvedAttr: the module or class has no such member.
	reasonUnresolvedAttr = "unresolved-attr"
	// reasonDecorator: the result of a decorator is not known.
	reasonDecorator = "decorator"
	// reasonDynamicExcept: the classes an except clause catches, or a raised
	// value, are not known.
	reasonDynamicExcept = "dynamic-except"
	// reasonDynamicCall: no signature binds the arguments, or the callee has no
	// static target (starred arguments, a closure, __new__, super()).
	reasonDynamicCall = "dynamic-call"
	// reasonDynamicItem: an item the checker cannot shape statically: a
	// variable index of a tuple, an unpacking with the wrong number of items,
	// a starred item of a set or tuple display, or a ** item of a dict display.
	reasonDynamicItem = "dynamic-item"
	// reasonAnyValue: an operand is an Any that an earlier site or a library
	// signature produced.
	reasonAnyValue = "any-value"
)
