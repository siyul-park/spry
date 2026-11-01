package runner

import (
	"context"
	"fmt"

	"github.com/siyul-park/spry/benchmarks"
)

// unavailable is a runner that cannot run yet and says why.
type unavailable struct {
	name   string
	reason string
}

func (u unavailable) Name() string {
	return u.name
}

func (u unavailable) Available() error {
	return fmt.Errorf("%w: %s: %s", ErrUnavailable, u.name, u.reason)
}

func (u unavailable) Run(context.Context, benchmarks.Fixture) (Result, error) {
	return Result{}, u.Available()
}
