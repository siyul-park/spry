# 0006: Baseline and benchmark harness

- Status: Accepted

## Context

Claims about compatibility and speed need comparable measurements against CPython, PyPy and gpython, from the first phase, not only at the end.

## Decision

Conformance and benchmarks run programs as separate processes through a `Runner` interface (`cpython`, `pypy3`, `gpython`, `spry`) with identical argv. Conformance compares stdout and exit code with CPython 3.14 output stored as `.expected`; benchmarks use warmup, repetitions and medians with environment capture. Fixtures derive from minivm's benchmark kernels. Benchmarks are a separate Go module. Details: docs/testing.md, docs/benchmarks.md.

## Consequences

- Unavailable runners are skipped, so contributors need only Go and (optionally) Python 3.14.
- Process-level timing includes startup; kernel timing separates it.
- Results are machine-specific; the regression gate is relative to a baseline from the same machine class.

## Alternatives

- In-process Go benchmarks: not possible for CPython/PyPy.
- Only micro-benchmarks from minivm: too narrow to evidence compatibility.
