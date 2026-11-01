// Package builtins implements the Python object model and the builtin types
// and functions on top of minivm: the bigint slow path, str, list, dict and
// tuple, the exception hierarchy, and builtins exposed as host functions.
//
// Input: none (registered with the minivm interpreter). Output: host
// functions and value layouts used by compiler-generated programs.
// Allowed spry imports: none of the pipeline stages; it may import
// github.com/siyul-park/minivm.
package builtins
