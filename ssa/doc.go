// Package ssa defines spry's own SSA intermediate representation
// (Program, Function, BasicBlock, Value, Instruction), a builder from the typed
// AST, a verifier, and a text printer and parser.
//
// Input: typed ast from package types. Output: an SSA program.
// Allowed spry imports: ast, types (and token for positions). It never
// imports minivm.
package ssa
