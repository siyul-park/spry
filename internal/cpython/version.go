package cpython

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Version is the CPython release the corpus and goldens are pinned to.
const Version = "3.14.9"

// ErrVersion reports an interpreter whose major.minor is not 3.14.
var ErrVersion = errors.New("cpython: interpreter is not python 3.14")

// Check asks the interpreter for its version. It returns the actual
// major.minor.micro string; ErrVersion means major.minor is not 3.14. A micro
// release other than Version is not an error, so the caller can report it.
func Check(ctx context.Context, python string) (string, error) {
	res, err := Run(ctx, python, "", nil, "-c", "import sys;print('%d.%d.%d' % sys.version_info[:3])")
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("cpython: %s: version query failed: %s", python, strings.TrimSpace(res.Stderr))
	}
	actual := strings.TrimSpace(res.Stdout)
	if !strings.HasPrefix(actual, "3.14.") {
		return actual, fmt.Errorf("%w: %s is %s", ErrVersion, python, actual)
	}
	return actual, nil
}
