// Command genexpected regenerates the .expected file of every conformance case
// from CPython 3.14.
//
//	go run ./cmd/genexpected [-dir testdata]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/siyul-park/spry/conformance"
	"github.com/siyul-park/spry/internal/cpython"
)

func main() {
	dir := flag.String("dir", "testdata", "corpus directory")
	flag.Parse()
	if err := run(context.Background(), *dir); err != nil {
		fmt.Fprintln(os.Stderr, "genexpected:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, dir string) error {
	python, err := cpython.Lookup()
	if err != nil {
		return err
	}
	var sources []string
	err = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Ext(p) == conformance.SourceExt {
			sources = append(sources, p)
		}
		return err
	})
	if err != nil {
		return err
	}
	for _, src := range sources {
		res, err := cpython.Run(ctx, python, "", nil, src)
		if err != nil {
			return err
		}
		if res.ExitCode != 0 {
			return fmt.Errorf("%s: exit %d: %s", src, res.ExitCode, strings.TrimSpace(res.Stderr))
		}
		out := strings.TrimSuffix(src, conformance.SourceExt) + conformance.ExpectedExt
		if err := os.WriteFile(out, []byte(res.Stdout), 0o644); err != nil {
			return err
		}
		fmt.Println("wrote", out)
	}
	return nil
}
