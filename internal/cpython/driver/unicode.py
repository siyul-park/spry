"""Unicode driver: code point tables of CPython 3.14 as JSON.

usage: unicode.py

id_start    ranges where c.isidentifier() holds (XID_Start and "_")
id_continue ranges where ("a" + c).isidentifier() holds (XID_Continue)
printable   ranges where c.isprintable() holds
Ranges are inclusive [lo, hi] pairs; surrogates are never members.
"""

import json
import unicodedata


def ranges(pred):
    out, lo = [], None
    for cp in range(0x110000):
        if 0xD800 <= cp <= 0xDFFF or not pred(chr(cp)):
            if lo is not None:
                out.append([lo, cp - 1])
                lo = None
        elif lo is None:
            lo = cp
    if lo is not None:
        out.append([lo, 0x10FFFF])
    return out


def main():
    print(
        json.dumps(
            {
                "unidata_version": unicodedata.unidata_version,
                "id_start": ranges(lambda c: c.isidentifier()),
                "id_continue": ranges(lambda c: ("a" + c).isidentifier()),
                "printable": ranges(lambda c: c.isprintable()),
            },
            separators=(",", ":"),
        )
    )


main()
