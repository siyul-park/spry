// Package runner runs benchmark fixtures under a Python implementation.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/siyul-park/spry/benchmarks"
)

// Result is one measured execution.
type Result struct {
	Output  string        // stdout of the driver
	Elapsed time.Duration // wall clock of the whole process, interpreter startup included
}

// Runner executes fixtures under one implementation.
type Runner interface {
	// Name identifies the implementation in reports.
	Name() string
	// Available returns nil, or an error wrapping ErrUnavailable that says why not.
	Available() error
	// Run executes the fixture once.
	Run(ctx context.Context, fixture benchmarks.Fixture) (Result, error)
}

// command runs fixtures by writing the driver to a temporary file and handing
// it to an external interpreter.
type command struct {
	name string
	// lookup resolves the interpreter path or explains why it is missing.
	lookup func() (string, error)
}

// ErrUnavailable is wrapped by Available when an implementation cannot run.
var ErrUnavailable = errors.New("runner unavailable")

// All returns every known runner, available or not.
func All() []Runner {
	return []Runner{CPython(), PyPy3(), GPython(), Spry()}
}

func (c command) Name() string {
	return c.name
}

func (c command) Available() error {
	if _, err := c.lookup(); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrUnavailable, c.name, err)
	}
	return nil
}

func (c command) Run(ctx context.Context, fixture benchmarks.Fixture) (Result, error) {
	exe, err := c.lookup()
	if err != nil {
		return Result{}, fmt.Errorf("%w: %s: %v", ErrUnavailable, c.name, err)
	}
	dir, err := os.MkdirTemp("", "spry-bench-")
	if err != nil {
		return Result{}, fmt.Errorf("runner: temp dir: %w", err)
	}
	defer os.RemoveAll(dir)
	script := filepath.Join(dir, fixture.Name+".py")
	if err := os.WriteFile(script, []byte(fixture.Driver()), 0o600); err != nil {
		return Result{}, fmt.Errorf("runner: write driver: %w", err)
	}

	cmd := exec.CommandContext(ctx, exe, script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	err = cmd.Run()
	elapsed := time.Since(start)
	if err != nil {
		return Result{}, fmt.Errorf("runner: %s on %s: %w: %s", c.name, fixture.Name, err, bytes.TrimSpace(stderr.Bytes()))
	}
	return Result{Output: stdout.String(), Elapsed: elapsed}, nil
}
