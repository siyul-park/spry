"""Type repr driver: typing._type_repr of canonical annotation objects, one
JSON object per line.

usage: typerepr.py

Each line is {"id", "kind", "src", "repr"}. src runs in a namespace named
__main__ and defines T. kind "annotation" records typing._type_repr(T);
kind "signature" records str(inspect.signature(T)) for a def T. Lines keep
the order of CASES.
"""

import inspect
import json
import typing

TV = "from typing import *\nfrom collections.abc import *\n"
CLASS_C = "class C: ...\n"
CLASS_D = "class D[X]: ...\n"
LEGACY = TV + "T0 = TypeVar('T0')\n"
SIG_C = "class C: ...\n"

CASES = [
    ("named-int", "annotation", "T = int"),
    ("named-none", "annotation", "T = None"),
    ("named-class", "annotation", CLASS_C + "T = C"),
    ("named-generic", "annotation", CLASS_D + "T = D[int]"),
    ("named-generic-nested", "annotation", CLASS_D + "T = D[tuple[int, ...]]"),
    ("named-list", "annotation", "T = list[int]"),
    ("named-dict", "annotation", "T = dict[str, int]"),
    ("named-list-optional", "annotation", "T = list[int | None]"),
    ("named-list-any", "annotation", TV + "T = list[Any]"),
    ("named-bare-generic", "annotation", "T = list"),
    ("named-abc", "annotation", TV + "T = Iterable[int]"),
    ("named-abc-three", "annotation", TV + "T = Generator[int, None, str]"),
    ("union-optional", "annotation", "T = int | None"),
    ("union-three", "annotation", "T = int | str | None"),
    ("literal-str", "annotation", TV + "T = Literal['a']"),
    ("literal-str-quote", "annotation", TV + 'T = Literal["it\'s"]'),
    ("literal-bytes", "annotation", TV + "T = Literal[b'x']"),
    ("literal-bool", "annotation", TV + "T = Literal[True]"),
    ("literal-int", "annotation", TV + "T = Literal[-3]"),
    ("literal-merged", "annotation", TV + "T = Literal[1, 2] | None"),
    ("literal-in-list", "annotation", TV + "T = list[Literal[1]]"),
    ("tuple-fixed", "annotation", "T = tuple[int, str]"),
    ("tuple-unbounded", "annotation", "T = tuple[int, ...]"),
    ("tuple-empty", "annotation", "T = tuple[()]"),
    ("tuple-unbounded-inner", "annotation", "T = tuple[int, *tuple[str, ...]]"),
    (
        "tuple-typevartuple",
        "annotation",
        TV + "Ts = TypeVarTuple('Ts')\nT = tuple[int, *Ts]",
    ),
    (
        "tuple-typevartuple-only",
        "annotation",
        TV + "Ts = TypeVarTuple('Ts')\nT = tuple[*Ts]",
    ),
    ("meta-class", "annotation", CLASS_C + "T = type[C]"),
    ("meta-any", "annotation", TV + "T = type[Any]"),
    ("callable-params", "annotation", TV + "T = Callable[[int], str]"),
    ("callable-empty", "annotation", TV + "T = Callable[[], int]"),
    ("callable-gradual", "annotation", TV + "T = Callable[..., str]"),
    ("callable-nested", "annotation", TV + "T = Callable[[int | None], list[str]]"),
    (
        "callable-concatenate",
        "annotation",
        TV + "P = ParamSpec('P')\nT = Callable[Concatenate[int, P], int]",
    ),
    (
        "callable-paramspec",
        "annotation",
        TV + "P = ParamSpec('P')\nT = Callable[P, int]",
    ),
    ("basic-any", "annotation", TV + "T = Any"),
    ("basic-never", "annotation", TV + "T = Never"),
    ("basic-literalstring", "annotation", TV + "T = LiteralString"),
    ("guard-typeis", "annotation", TV + "T = TypeIs[int]"),
    ("guard-typeguard", "annotation", TV + "T = TypeGuard[int]"),
    ("typeparam-legacy", "annotation", LEGACY + "T = T0"),
    ("typeparam-legacy-in-list", "annotation", LEGACY + "T = list[T0]"),
    ("typeparam-covariant", "annotation", TV + "T = TypeVar('T0', covariant=True)"),
    (
        "typeparam-contravariant",
        "annotation",
        TV + "T = TypeVar('T0', contravariant=True)",
    ),
    ("typeparam-pep695", "annotation", "type G[X] = list[X]\nT = G.__type_params__[0]"),
    ("typeparam-paramspec-legacy", "annotation", TV + "T = ParamSpec('P0')"),
    (
        "typeparam-paramspec-pep695",
        "annotation",
        "type H[**P0] = int\nT = H.__type_params__[0]",
    ),
    ("typeparam-typevartuple", "annotation", TV + "T = TypeVarTuple('Ts')"),
    ("typeparam-self", "annotation", TV + "T = Self"),
    (
        "typeparam-paramspec-args",
        "annotation",
        TV + "P0 = ParamSpec('P0')\nT = P0.args",
    ),
    (
        "typeparam-paramspec-kwargs",
        "annotation",
        TV + "P0 = ParamSpec('P0')\nT = P0.kwargs",
    ),
    ("alias-plain", "annotation", "type IntList = list[int]\nT = IntList"),
    ("alias-instance", "annotation", "type G[X] = list[X]\nT = G[int]"),
    (
        "signature-all-kinds",
        "signature",
        SIG_C
        + "def T(a: int, /, b: str, *args: int, c: bool, **kw: str) -> list[int]: ...",
    ),
    ("signature-keyword-only", "signature", "def T(*, a: int) -> None: ..."),
    ("signature-named", "signature", SIG_C + "def T(a: int, b: C) -> int: ..."),
]


def evaluate(src, kind):
    ns = {"__name__": "__main__"}
    exec(compile(src, "<case>", "exec"), ns)
    value = ns["T"]
    if kind == "signature":
        return str(inspect.signature(value))
    return typing._type_repr(value)


def main():
    for case_id, kind, src in CASES:
        print(
            json.dumps(
                {"id": case_id, "kind": kind, "src": src, "repr": evaluate(src, kind)},
                sort_keys=True,
            )
        )


main()
