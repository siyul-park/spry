package cpython

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Result is the observable outcome of one interpreter run.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// EnvVar names the environment variable that overrides interpreter discovery.
const EnvVar = "SPRY_PYTHON"

// uvTimeout bounds the `uv python find` fallback so discovery cannot hang.
const uvTimeout = 10 * time.Second

// ErrNotFound reports that no CPython 3.14 interpreter could be located.
var ErrNotFound = errors.New("cpython: python 3.14 not found")

// Lookup finds a CPython 3.14 interpreter: $SPRY_PYTHON, then python3.14 on
// PATH, then `uv python find 3.14`.
func Lookup() (string, error) {
	return lookup(os.Getenv(EnvVar), exec.LookPath, uvFind)
}

// Run executes the interpreter with args and stdin in directory dir (the
// current directory when empty). A non-zero exit status is reported through
// Result.ExitCode, not as an error.
func Run(ctx context.Context, python, dir string, stdin []byte, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, python, args...)
	cmd.Dir = dir
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		res.ExitCode = exit.ExitCode()
	default:
		return res, fmt.Errorf("cpython: run %s: %w", python, err)
	}
	return res, nil
}

func lookup(override string, lookPath func(string) (string, error), uv func() (string, error)) (string, error) {
	if override != "" {
		path, err := lookPath(override)
		if err != nil {
			return "", fmt.Errorf("%w: %s=%q: %v", ErrNotFound, EnvVar, override, err)
		}
		return path, nil
	}
	if path, err := lookPath("python3.14"); err == nil {
		return path, nil
	}
	if path, err := uv(); err == nil && path != "" {
		return path, nil
	}
	return "", ErrNotFound
}

func uvFind() (string, error) {
	uv, err := exec.LookPath("uv")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), uvTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, uv, "python", "find", "3.14").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
