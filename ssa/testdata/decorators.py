def ident(f):
    return f


def twice(f):
    def wrapper(x):
        return f(f(x))
    return wrapper


@ident
def inc(x: int) -> int:
    return x + 1


@twice
def dec(x: int) -> int:
    return x - 1


def outer(n: int) -> int:
    @ident
    def local(y: int) -> int:
        return y * n
    return local(2)


print(inc(1), dec(5), outer(3))


def unknown(f):
    return print


@unknown
def lost(x: int) -> int:
    return x
