def pick(x: int | None, y: str | int):
    if x is None:
        return 0
    if isinstance(y, str):
        return len(y) + x
    return y + x


def truthy(x: int | None):
    if x:
        return x
    return 0


def either(a, b):
    return a or b


print(pick(1, "s"), truthy(None), either(0, 2.5))
