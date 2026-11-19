"""Stub of the typing module: special forms are declared by annotation and
recognized by name (see types/special.go); the rest is ordinary Python."""

from collections.abc import (
    Awaitable,
    Callable,
    Collection,
    Container,
    Coroutine,
    Generator,
    Hashable,
    Iterable,
    Iterator,
    Mapping,
    MutableMapping,
    MutableSequence,
    Sequence,
    Sized,
)


class _SpecialForm: ...


Any: _SpecialForm
Never: _SpecialForm
NoReturn: _SpecialForm
LiteralString: _SpecialForm
Self: _SpecialForm
Optional: _SpecialForm
Union: _SpecialForm
Literal: _SpecialForm
Final: _SpecialForm
ClassVar: _SpecialForm
Annotated: _SpecialForm
TypeAlias: _SpecialForm
Concatenate: _SpecialForm
TypeGuard: _SpecialForm
TypeIs: _SpecialForm
Unpack: _SpecialForm
Required: _SpecialForm
NotRequired: _SpecialForm
ReadOnly: _SpecialForm
InitVar: _SpecialForm


class Protocol: ...


class Generic: ...


class TypeVar: ...


class ParamSpec: ...


class TypeVarTuple: ...


class NamedTuple: ...


class TypedDict: ...


def NewType(name: str, tp: object, /) -> object: ...


def cast(typ: object, val: object, /) -> Any: ...


def overload(func: Any, /) -> Any: ...


def final(f: Any, /) -> Any: ...


def runtime_checkable(cls: Any, /) -> Any: ...


TYPE_CHECKING: bool = False

List = list
Dict = dict
Set = set
FrozenSet = frozenset
Tuple = tuple
Type = type
