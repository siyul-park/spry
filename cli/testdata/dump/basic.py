import os


def f(x, *, y=1):
    # comment
    """doc"""
    return [x + y for x in range(3) if x]


class C(Base):
    z: int = 0b101 + 1_0
