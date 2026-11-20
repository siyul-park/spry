def swap(a, b):
    a, b = b, a
    return a, b


def head_tail(xs):
    first, *rest = xs
    *init, last = xs
    a, *mid, z = xs
    return first, rest, init, last, a, mid, z


def nested():
    (a, b), c = (1, 2), 3
    [x, [y, z]] = (10, [20, 30])
    return a + b + c + x + y + z


def from_tuple():
    t = (1, "two", 3.0, 4)
    n, s, f, m = t
    h, *r = t
    return n, s, f, m, h, r


def loops(pairs):
    total = 0
    for i, (a, b) in enumerate(pairs):
        total += i * a + b
    for k, *v in [(1, 2, 3), (4, 5, 6)]:
        total += k + len(v)
    return total


print(swap(1, 2), head_tail([1, 2, 3, 4]), nested(), from_tuple(), loops([(1, 2), (3, 4)]))
