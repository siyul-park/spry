// Package corpus reads and writes the CPython test-corpus manifests under
// <package>/testdata/cpython and resolves the pinned CPython source cache
// they refer to. A manifest is JSON lines of Case, sorted by ID. Allowed spry
// imports: none (internal/cpython only).
package corpus
