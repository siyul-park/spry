# 0005: Lighthouse for design enforcement

- Status: Accepted

## Context

Package dependency rules and conventions are easy to violate in a multi-agent workflow. Documentation alone drifts. Lighthouse (https://github.com/siyul-park/lighthouse) turns design decisions into executable checks and is already used by minivm.

## Decision

The dependency rules in docs/architecture.md are encoded as lighthouse decisions under `.lighthouse/decisions/` with examples validated by `lighthouse decision test`. `lighthouse check` is part of `make lint` and `make check`; `lighthouse check --changed` is the fast loop. Test-data directories are excluded.

## Consequences

- Layering violations (for example `ssa` importing minivm) fail the gate.
- Contributors install the lighthouse tool; CI installs it or skips the step when unavailable.
- New architectural rules start as an ADR, then a decision.

## Alternatives

- `go-arch-lint` or custom `go list` scripts: workable but separate from the decision records and agent hooks.
- Documentation and review only: drift risk.
