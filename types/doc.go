// Package types is the Python type system of spry, shaped after go/types, and
// the whole-program checker that infers it. Check takes the parsed main module
// and the sys.path entries given by WithFS, loads every module it reaches and
// returns the *Module, an *Info (generalized facts plus one Spec per
// specialization of each function) and an ErrorList of TYPnnn diagnostics.
// The library is Python stubs embedded from lib. Types are opaque structs behind accessor methods: Basic, Named and
// its Class, Union, Tuple, Signature, Overload, Literal, Guard, Meta,
// TypeParam, Alias and Module. Objects (TypeName, Var, Func, ModuleName,
// Builtin) live in Scopes. Identical, AssignableTo, Join, Default, Unalias,
// Instantiate and LookupAttr relate and transform types with Python typing
// semantics; String follows python3.14 typing._type_repr.
//
// Allowed spry imports: token, ast, scanner and parser (the frontend) only.
// Code that also needs minivm types must alias them as vmtypes; this package
// itself never imports minivm. Its tests also import parser.
package types
