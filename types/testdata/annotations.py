def half(x: float) -> float:
    return x / 2


def label(n: int) -> str:
    return str(n)


limit: int = 5
ratio: float = limit
name: str | None = None
print(half(3), label(limit), ratio, name)
