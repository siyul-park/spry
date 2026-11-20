// Package ssa defines spry's own typed SSA intermediate representation: the
// data model (Program, Module, Class, Function, Block, Instr), a verifier, and
// a text printer and parser. Blocks take parameters instead of phi nodes, and
// instructions that can raise carry an exception edge.
//
// Input: text (Parse) or the checked program (Build, from types.Check).
// Output: a *Program, which Verify checks and Print writes.
// Allowed imports: ast, token and types, and the standard library. It never
// imports minivm. Its tests also import parser.
package ssa
