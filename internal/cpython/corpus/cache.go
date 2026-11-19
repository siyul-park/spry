package corpus

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/siyul-park/spry/internal/cpython"
)

// EnvCache names the environment variable that overrides the cache location.
const EnvCache = "SPRY_CPYTHON_CACHE"

var (
	// ErrNoCache reports a File case read while the CPython cache is absent;
	// `make corpus` fetches it.
	ErrNoCache = errors.New("corpus: cpython cache missing (run `make corpus`)")
	// ErrDrift reports a cached file whose hash differs from the manifest.
	ErrDrift = errors.New("corpus: cpython source drifted from manifest")
)

// Dir returns the cache directory and whether it exists: $SPRY_CPYTHON_CACHE,
// else <module root>/.cache/cpython/<cpython.Version>, where the module root
// is the nearest ancestor of the working directory holding go.mod. The path is
// empty when neither applies.
func Dir() (string, bool) {
	dir := os.Getenv(EnvCache)
	if dir == "" {
		root, ok := moduleRoot()
		if !ok {
			return "", false
		}
		dir = filepath.Join(root, ".cache", "cpython", cpython.Version)
	}
	info, err := os.Stat(dir)
	return dir, err == nil && info.IsDir()
}

func moduleRoot() (string, bool) {
	dir, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
