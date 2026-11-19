def compare(a, b, x, s, t):
    return a < b, a == x, x >= a, s == t, s < t, a is None, s is not None


def chain(a, b, c):
    return a < b < c


def flags(p, q):
    return p == q, p < 2


print(compare(1, 2, 1.5, "a", "b"))
print(chain(1, 2, 3))
print(flags(True, False))
