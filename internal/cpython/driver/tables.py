"""Table driver: token and ast tables of CPython 3.14 as JSON.

usage: tables.py token|ast

token: tok_name, EXACT_TOKEN_TYPES (operator string to token name), kwlist,
       softkwlist.
ast:   every concrete node class with its category base (mod, stmt, expr, ...;
       empty when the class derives from ast.AST directly), _fields with
       _field_types rendered as strings, and _attributes.
"""

import ast
import json
import keyword
import sys
import token

DEPRECATED = {
    "Num",
    "Str",
    "Bytes",
    "NameConstant",
    "Ellipsis",
    "Index",
    "ExtSlice",
    "Suite",
    "AugLoad",
    "AugStore",
    "Param",
}


def render(t):
    if isinstance(t, type):
        return t.__name__
    return str(t).replace("ast.", "")


def node(cls):
    base = cls.__bases__[0]
    types = getattr(cls, "_field_types", {})
    return {
        "name": cls.__name__,
        "category": "" if base is ast.AST else base.__name__,
        "fields": [
            {"name": f, "type": render(types.get(f, object))} for f in cls._fields
        ],
        "attributes": list(cls._attributes),
    }


def token_tables():
    return {
        "tok_name": [[n, token.tok_name[n]] for n in sorted(token.tok_name)],
        "exact_token_types": {
            s: token.tok_name[n] for s, n in sorted(token.EXACT_TOKEN_TYPES.items())
        },
        "kwlist": list(keyword.kwlist),
        "softkwlist": list(keyword.softkwlist),
    }


def ast_schema():
    classes = [
        c
        for name, c in vars(ast).items()
        if isinstance(c, type)
        and issubclass(c, ast.AST)
        and c is not ast.AST
        and name not in DEPRECATED
    ]
    abstract = {c.__bases__[0] for c in classes if c.__bases__[0] is not ast.AST}
    concrete = [c for c in classes if c not in abstract]
    return {
        "categories": sorted(b.__name__ for b in abstract),
        "nodes": sorted((node(c) for c in concrete), key=lambda n: n["name"]),
    }


def main():
    which = {"token": token_tables, "ast": ast_schema}[sys.argv[1]]
    print(json.dumps(which(), indent=1, sort_keys=True))


main()
