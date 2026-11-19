"""Parse driver: batch ast.parse / compile outcomes of CPython 3.14.

usage: parse.py <cache-root>

stdin  JSONL requests {"id", "mode": exec|eval|single, "src": str | "file": path
       relative to cache-root}. A file is parsed as bytes so coding
       cookies and BOMs apply, and its expect carries the dump hash, not the dump.
stdout JSONL {"id", "expect": {dump|hash, error, stage, warnings}}. The stage
       is "parse" when ast.parse fails and "compile" when only compile fails.
"""

import ast
import hashlib
import json
import os
import sys
import warnings


def syntax_error(e):
    if isinstance(e, SyntaxError):
        return {
            "type": type(e).__name__,
            "msg": e.msg or "",
            "lineno": e.lineno or 0,
            "offset": e.offset or 0,
            "end_lineno": e.end_lineno or 0,
            "end_offset": e.end_offset or 0,
        }
    return {"type": type(e).__name__, "msg": str(e)}


def attempt(stage, fn, expect, seen):
    """Run fn, record its error and warnings (once per distinct warning)."""
    result = None
    with warnings.catch_warnings(record=True) as caught:
        warnings.simplefilter("always")
        try:
            result = fn()
        except Exception as e:
            expect["error"] = syntax_error(e)
            expect["stage"] = stage
    for w in caught:
        key = (w.category.__name__, w.lineno, str(w.message))
        if key not in seen:
            seen.add(key)
            expect.setdefault("warnings", []).append(
                "%s:%s:%d: %s" % (stage, key[0], w.lineno or 0, w.message)
            )
    return result


def outcome(req, cache):
    src = req.get("src")
    if src is None:
        with open(os.path.join(cache, req["file"]), "rb") as f:
            src = f.read()
    mode = req["mode"]
    expect, seen = {}, set()
    tree = attempt("parse", lambda: ast.parse(src, "<case>", mode), expect, seen)
    if tree is None:
        return expect
    dump = attempt(
        "dump", lambda: ast.dump(tree, include_attributes=True, indent=1), expect, seen
    )
    if dump is None:
        return expect
    if "file" in req:
        expect["hash"] = hashlib.sha256(dump.encode("utf-8")).hexdigest()
    else:
        expect["dump"] = dump
    attempt("compile", lambda: compile(src, "<case>", mode), expect, seen)
    return expect


def main():
    sys.stdin.reconfigure(encoding="utf-8")
    cache = sys.argv[1]
    for line in sys.stdin:
        req = json.loads(line)
        try:
            expect = outcome(req, cache)
        except Exception as e:  # unreadable file, unexpected failure
            expect = {"error": syntax_error(e), "stage": "driver"}
        print(json.dumps({"id": req["id"], "expect": expect}, sort_keys=True))


main()
