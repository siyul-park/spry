"""Repr driver: repr() of constants in CPython 3.14, one JSON object per line.

usage: repr.py <cache-root>

Each line is {"kind", "value", "repr"}. The value encoding per kind:
  float    float.hex() ("inf", "-inf", "nan" spelled as such)
  complex  "<real hex>,<imag hex>"
  str      code points as space-separated upper-case hex
  bytes    bytes.hex()
The sample is edge cases plus the float, complex, str and bytes constants of
the corpus modules below, deduplicated and sorted by (kind, value).
"""

import ast
import json
import math
import os
import sys

MODULES = [
    "test_ast/snippets.py",
    "test_ast/test_ast.py",
    "test_float.py",
    "test_complex.py",
    "test_grammar.py",
    "test_string_literals.py",
    "test_unparse.py",
    "test_fstring.py",
    "test_bytes.py",
    "test_str.py",
]
MAX_LEN = 64
FLOAT_EDGES = [
    0.0,
    -0.0,
    1.0,
    -1.0,
    0.1,
    0.5,
    1.5,
    2.5,
    1e15,
    1e16,
    1e17,
    1e21,
    1e22,
    1e-4,
    1e-5,
    1e-6,
    123456789.0,
    1e100,
    5e-324,
    2.2250738585072014e-308,
    1.7976931348623157e308,
    math.inf,
    -math.inf,
    math.nan,
    0.30000000000000004,
    3.141592653589793,
    2.718281828459045,
    1e300 * 10,
    9007199254740993.0,
    1.1102230246251565e-16,
    100000000000000000.0,
    12345678901234567890.0,
]
CODE_POINT_EDGES = list(range(0x100)) + [
    0x2FF,
    0x300,
    0x36F,
    0x61C,
    0x1680,
    0x180E,
    0x2000,
    0x200B,
    0x200E,
    0x2028,
    0x2029,
    0x202F,
    0x205F,
    0x2060,
    0x3000,
    0xD7FF,
    0xE000,
    0xF8FF,
    0xFDD0,
    0xFEFF,
    0xFFFD,
    0xFFFE,
    0xFFFF,
    0x10000,
    0x1F600,
    0xE0001,
    0xE0100,
    0xF0000,
    0x10FFFD,
    0x10FFFF,
]
STR_EDGES = [
    "",
    "'",
    '"',
    "'\"",
    "a'b\"c",
    "it's",
    'say "hi"',
    "\\",
    "\n",
    "\r\n",
    "\t",
    "a\\b",
    "tab\there",
    "\x00",
    "\x7f",
    "café",
    " ",
    "'''",
    '"""',
    "\\'",
    '\\"',
    "ሴ噸",
]
BYTES_EDGES = [b"", b"'", b'"', b"'\"", b"a'b\"c", b"\\", b"\n\r\t", b"\x00\xff"]


def encode(value):
    if isinstance(value, float):
        if math.isnan(value):
            return "nan"
        if math.isinf(value):
            return "inf" if value > 0 else "-inf"
        return value.hex()
    if isinstance(value, complex):
        return "%s,%s" % (encode(value.real), encode(value.imag))
    if isinstance(value, str):
        return " ".join("%X" % ord(c) for c in value)
    return value.hex()


def constants(cache):
    for rel in MODULES:
        path = os.path.join(cache, "Lib", "test", rel)
        if not os.path.exists(path):
            continue
        with open(path, "rb") as f:
            tree = ast.parse(f.read())
        for n in ast.walk(tree):
            if isinstance(n, ast.Constant) and type(n.value) in (
                float,
                complex,
                str,
                bytes,
            ):
                if (
                    len(n.value) <= MAX_LEN
                    if isinstance(n.value, (str, bytes))
                    else True
                ):
                    yield n.value


def sample(cache):
    values = list(FLOAT_EDGES) + [
        complex(a, b)
        for a in (0.0, -0.0, 1.0, 1e16, math.inf)
        for b in (0.0, 1.0, -2.5, math.nan)
    ]
    values += [chr(c) for c in CODE_POINT_EDGES if not 0xD800 <= c <= 0xDFFF]
    values += STR_EDGES + BYTES_EDGES + [bytes([b]) for b in range(256)]
    values += [
        v
        for v in constants(cache)
        if not isinstance(v, str) or v.isascii() or _no_surrogates(v)
    ]
    out = {}
    for v in values:
        out[(type(v).__name__, encode(v))] = repr(v)
    return out


def _no_surrogates(s):
    return not any(0xD800 <= ord(c) <= 0xDFFF for c in s)


def main():
    for (kind, value), r in sorted(sample(sys.argv[1]).items()):
        print(json.dumps({"kind": kind, "value": value, "repr": r}, sort_keys=True))


main()
