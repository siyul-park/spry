// Package compiler lowers an ssa program to a minivm program (out-of-SSA,
// stack scheduling, type mapping), then runs minivm optimize and Verify.
// It is the only compiler backend.
//
// Input: an ssa program. Output: a minivm program.
// Allowed spry imports: ssa and builtins, and github.com/siyul-park/minivm.
package compiler
