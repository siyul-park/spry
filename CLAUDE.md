@AGENTS.md

# Claude Code Notes

- Design docs under `docs/` are the source of truth. If code and docs disagree, fix whichever is wrong in the same change and note it.
- Prefer `lighthouse check --changed` while iterating; run `make check` before declaring completion.
- The local `python3` may not be 3.14. Use `SPRY_PYTHON` (or `uv python install 3.14`) when a task needs the CPython oracle.
- minivm (https://github.com/siyul-park/minivm) has an unimportable `internal/ssa`. Read its instruction-set doc before citing opcodes.
- minipy (https://github.com/siyul-park/minipy) is reference only; do not copy code from it.
