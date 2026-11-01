// Command genexpected regenerates fixtures/<name>.expected from CPython 3.14.
//
//	go run ./cmd/genexpected [-dir fixtures]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/siyul-park/spry/benchmarks"
	"github.com/siyul-park/spry/benchmarks/runner"
)

func main() {
	dir := flag.String("dir", "fixtures", "fixture directory")
	flag.Parse()
	if err := run(context.Background(), *dir); err != nil {
		fmt.Fprintln(os.Stderr, "genexpected:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, dir string) error {
	python := runner.CPython()
	if err := python.Available(); err != nil {
		return err
	}
	sources, err := filepath.Glob(filepath.Join(dir, "*.py"))
	if err != nil {
		return err
	}
	for _, src := range sources {
		body, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(filepath.Base(src), ".py")
		res, err := python.Run(ctx, fixture(name, string(body)))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name+".expected"), []byte(res.Output), 0o644); err != nil {
			return err
		}
		fmt.Println("wrote", name+".expected")
	}
	return nil
}

func fixture(name, source string) benchmarks.Fixture {
	return benchmarks.Fixture{Name: name, Source: source}
}
