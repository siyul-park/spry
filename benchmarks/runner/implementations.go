package runner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// PythonEnv overrides CPython discovery, like internal/cpython in the main module.
const PythonEnv = "SPRY_PYTHON"

// uvTimeout bounds the `uv python find` fallback so discovery cannot hang.
const uvTimeout = 10 * time.Second

// CPython runs fixtures with $SPRY_PYTHON, python3.14, or `uv python find 3.14`.
func CPython() Runner {
	return command{name: "cpython", lookup: lookupCPython}
}

// PyPy3 runs fixtures with pypy3 from PATH.
func PyPy3() Runner {
	return command{name: "pypy3", lookup: func() (string, error) {
		return exec.LookPath("pypy3")
	}}
}

// GPython runs fixtures with the gpython binary from PATH.
func GPython() Runner {
	return command{name: "gpython", lookup: func() (string, error) {
		return exec.LookPath("gpython")
	}}
}

// Spry stays unavailable until phase P3 lets `spry file.py` execute programs.
func Spry() Runner {
	return unavailable{name: "spry", reason: "phase P3"}
}

func lookupCPython() (string, error) {
	if override := os.Getenv(PythonEnv); override != "" {
		return exec.LookPath(override)
	}
	if path, err := exec.LookPath("python3.14"); err == nil {
		return path, nil
	}
	uv, err := exec.LookPath("uv")
	if err != nil {
		return "", errors.New("python3.14 not on PATH and uv not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), uvTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, uv, "python", "find", "3.14").Output()
	if err != nil {
		return "", errors.New("python3.14 not on PATH and `uv python find 3.14` failed")
	}
	return strings.TrimSpace(string(out)), nil
}
