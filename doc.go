// Package spry is the public driver of the pipeline: it runs parse, infer,
// build ssa, optimize and compile, and exposes stage dumps.
//
// Input: a source file set, an entry module and options. Output: a minivm
// program or diagnostics.
// Allowed spry imports: every pipeline package, and github.com/siyul-park/minivm.
package spry
