// Package benchmarks embeds the benchmark fixtures: Python kernels that define
// run() and are measured under CPython, PyPy, gpython and spry.
//
// The kernels are copied from
// https://github.com/siyul-park/minivm/tree/main/benchmarks/fixtures.
// Each <name>.py has a <name>.expected file holding what print(run()) prints
// under CPython 3.14; regenerate with `go run ./cmd/genexpected`.
//
// Allowed spry imports: none. This module only shells out to interpreters.
package benchmarks
