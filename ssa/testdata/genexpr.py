def squares(n):
    return (i * i for i in range(n))


def scaled(xs, k):
    return (x * k for x in xs if x > 0)


def total(xs):
    t = 0
    for v in scaled(xs, 3):
        t += v
    inline = sum(x for x in xs)
    lazy = any(x > 2 for x in xs)
    return t + inline + len(list(squares(4))) + lazy


print(total([1, 2, 3]))
