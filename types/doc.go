// Package types holds scopes and symbols and runs whole-program type
// inference with call-site specialization, falling back to Any where a type
// cannot be settled. Its diagnostics report every dynamic fallback.
//
// Input: ast trees. Output: a typed program (symbols and types per node).
// Allowed spry imports: token, ast, scanner and parser (the frontend) only.
// Code that also needs minivm types must alias them as vmtypes; this package
// itself never imports minivm.
package types
