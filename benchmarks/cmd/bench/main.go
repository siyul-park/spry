// Command bench measures the benchmark fixtures under every available runner.
//
//	go run ./cmd/bench [-runner cpython,pypy3] [-fixture recursive-fib] [-count 3] [-warmup 1] [-o ../docs/benchmarks.md]
//
// Unavailable runners are reported as skipped, never as failures. With -o the
// table replaces the text between the generated-results markers of an existing
// markdown file; the file is never created.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/siyul-park/spry/benchmarks/bench"
	"github.com/siyul-park/spry/benchmarks/runner"
)

func main() {
	if err := bench.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, runner.All()); err != nil {
		fmt.Fprintln(os.Stderr, "bench:", err)
		os.Exit(1)
	}
}
