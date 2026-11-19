"""Token driver: batch tokenize dumps of CPython 3.14.

usage: tokens.py <cache-root>

stdin  JSONL requests {"id", "src": str | "file": path relative to cache-root}.
       A str is tokenized with tokenize.generate_tokens, a file as bytes with
       tokenize.tokenize (so the ENCODING token is included, and the expect
       carries the dump hash, not the dump).
stdout JSONL {"id", "expect": {dump|hash, error, stage}}. The dump lines use
       the format of stringify_tokens_from_source in Lib/test/test_tokenize.py:
       the ENDMARKER and a final NEWLINE of input without a trailing newline are
       omitted. A tokenizer failure keeps the tokens produced so far and sets
       the error with stage "tokenize".
"""

import hashlib
import io
import json
import os
import sys
import tokenize


def error(e):
    if isinstance(e, SyntaxError):
        return {
            "type": type(e).__name__,
            "msg": e.msg or "",
            "lineno": e.lineno or 0,
            "offset": e.offset or 0,
            "end_lineno": e.end_lineno or 0,
            "end_offset": e.end_offset or 0,
        }
    if isinstance(e, tokenize.TokenError) and len(e.args) == 2:
        return {
            "type": "TokenError",
            "msg": str(e.args[0]),
            "lineno": e.args[1][0],
            "offset": e.args[1][1],
        }
    return {"type": type(e).__name__, "msg": str(e)}


def stringify(tokens, text, lines, expect):
    num_lines = len(text.splitlines())
    missing_nl = bool(text) and text[-1] not in "\r\n"
    try:
        for tok in tokens:
            type_, string, start, end, _ = tok
            if type_ == tokenize.ENDMARKER:
                break
            if missing_nl and type_ == tokenize.NEWLINE and end[0] == num_lines:
                continue
            lines.append(
                f"    {tokenize.tok_name[type_]:10} {string!r:13} {start} {end}"
            )
    except Exception as e:
        expect["error"] = error(e)
        expect["stage"] = "tokenize"


def outcome(req, cache):
    expect, lines = {}, []
    if "src" in req:
        text = req["src"]
        stringify(
            tokenize.generate_tokens(io.StringIO(text).readline), text, lines, expect
        )
    else:
        with open(os.path.join(cache, req["file"]), "rb") as f:
            raw = f.read()
        try:
            encoding, _ = tokenize.detect_encoding(io.BytesIO(raw).readline)
            text = raw.decode(encoding, "replace")
        except Exception as e:
            return {"error": error(e), "stage": "tokenize"}
        stringify(tokenize.tokenize(io.BytesIO(raw).readline), text, lines, expect)
    dump = "\n".join(lines)
    if "file" in req:
        expect["hash"] = hashlib.sha256(dump.encode("utf-8")).hexdigest()
    else:
        expect["dump"] = dump
    return expect


def main():
    sys.stdin.reconfigure(encoding="utf-8")
    cache = sys.argv[1]
    for line in sys.stdin:
        req = json.loads(line)
        try:
            expect = outcome(req, cache)
        except Exception as e:
            expect = {"error": error(e), "stage": "driver"}
        print(json.dumps({"id": req["id"], "expect": expect}, sort_keys=True))


main()
