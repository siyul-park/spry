// Package cpython locates and runs a CPython 3.14 interpreter. It serves
// tests and tools that record or compare reference behavior.
//
// The interpreter is found through SPRY_PYTHON, then python3.14 on PATH, then
// "uv python find 3.14". Allowed spry imports: none.
package cpython
