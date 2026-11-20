"""Stub of sys: the process-level values a program may read."""

from collections.abc import Sequence
from typing import Any, NoReturn


class TextIO:
    def write(self, text: str, /) -> int: ...
    def flush(self) -> None: ...
    def readline(self, size: int = -1, /) -> str: ...
    def read(self, size: int = -1, /) -> str: ...


class VersionInfo(tuple[int, int, int, str, int]):
    major: int
    minor: int
    micro: int
    releaselevel: str
    serial: int


class Implementation:
    name: str


argv: list[str]
stdout: TextIO
stderr: TextIO
stdin: TextIO
maxsize: int
version: str
version_info: VersionInfo
platform: str
implementation: Implementation


def exit(status: object = None, /) -> NoReturn: ...
