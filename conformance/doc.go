// Package conformance loads and runs the Python conformance corpus.
//
// The corpus lives in testdata/<category>/<name>.py with a sibling
// <name>.expected holding the stdout CPython 3.14 produces. A case may declare
// that spry is allowed to differ with a directive line:
//
//	# spry-divergence: <reason>
//
// Input: a directory tree and an interpreter command. Output: per-case
// pass/fail results. Allowed spry imports: internal/cpython (test tooling only).
package conformance
