// Package optimize runs SSA-to-SSA passes (SCCP, DCE, GVN, inlining,
// devirtualization, unboxing, range analysis) selected by levels O0 to O3.
//
// Input: an ssa program. Output: an equivalent, optimized ssa program.
// Allowed spry imports: ssa only. It never imports minivm.
package optimize
