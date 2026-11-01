// Package ast defines the Python syntax tree as Go structs, one node per
// CPython ast node (mod, stmt, expr, pattern, type_param), together with
// Walk, Inspect and a Dump compatible with CPython ast.dump.
//
// Input: none. Output: the tree produced by parser and consumed by types and ssa.
// Allowed spry imports: token only. It is part of the frontend layer.
package ast
