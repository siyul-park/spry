def assign():
    a = b = 1
    a, b = b + 1, a
    c: float = 2
    c += a
    d = 1
    d *= 3
    d -= 1
    return a, b, c, d


def unpack(p):
    x, y = p
    return x + y


print(assign())
print(unpack((1, 2)))
