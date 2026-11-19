def inner(n):
    for i in range(n):
        yield i
    return n * 10


def outer(n):
    r = yield from inner(n)
    yield r
    yield from [100, 200]
    yield from (x for x in range(2))


def chain():
    out = []
    for v in outer(2):
        out.append(v)
    return out


print(chain())
