// Command pycorpus fetches the pinned CPython source and derives the
// checked-in test-corpus manifests from it.
//
//	pycorpus fetch     download and verify the CPython source cache
//	pycorpus extract   regenerate the manifests (needs the cache and python3.14)
//
// Run it from the module root.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/siyul-park/spry/internal/cpython"
	"github.com/siyul-park/spry/internal/cpython/corpus"
)

var errUsage = errors.New("usage: pycorpus fetch|extract")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "pycorpus:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return errUsage
	}
	dir, exists := corpus.Dir()
	if dir == "" {
		return errors.New("no go.mod above the working directory; run from the module root")
	}
	switch args[0] {
	case "fetch":
		if err := corpus.Fetch(ctx, dir); err != nil {
			return err
		}
		fmt.Println("cpython", cpython.Version, "source cached at", dir)
		return nil
	case "extract":
		if !exists {
			return corpus.ErrNoCache
		}
		return extract(ctx, dir)
	}
	return errUsage
}
