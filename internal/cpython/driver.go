package cpython

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrDriver reports a driver script that failed to run or exited non-zero.
var ErrDriver = errors.New("cpython: driver failed")

//go:embed driver/*.py
var drivers embed.FS

// RunDriver runs the embedded driver script name (for example "parse.py")
// under `python -I` with args and stdin. The script is written to a scratch
// directory and runs with a different, empty working directory, so it never
// reads from the caller's tree. A non-zero exit status is an ErrDriver error
// that carries the driver's stderr.
func RunDriver(ctx context.Context, python, name string, stdin []byte, args ...string) (Result, error) {
	script, err := drivers.ReadFile("driver/" + name)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %s: %v", ErrDriver, name, err)
	}
	root, err := os.MkdirTemp("", "spry-cpython-*")
	if err != nil {
		return Result{}, fmt.Errorf("cpython: scratch directory: %w", err)
	}
	defer os.RemoveAll(root)

	path := filepath.Join(root, name)
	work := filepath.Join(root, "work")
	if err := os.Mkdir(work, 0o755); err != nil {
		return Result{}, fmt.Errorf("cpython: scratch directory: %w", err)
	}
	if err := os.WriteFile(path, script, 0o644); err != nil {
		return Result{}, fmt.Errorf("cpython: write driver: %w", err)
	}
	res, err := Run(ctx, python, work, stdin, append([]string{"-I", path}, args...)...)
	if err != nil {
		return res, err
	}
	if res.ExitCode != 0 {
		return res, fmt.Errorf("%w: %s exit %d: %s", ErrDriver, name, res.ExitCode, strings.TrimSpace(res.Stderr))
	}
	return res, nil
}
