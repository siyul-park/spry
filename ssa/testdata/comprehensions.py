def squares(n):
    return [i * i for i in range(n) if i % 2 == 0]


def pairs(xs, ys):
    return [(x, y) for x in xs for y in ys if x != y]


def table(n):
    return {i: str(i) for i in range(n)}


def unique(xs):
    return {x % 3 for x in xs}


def nested(n):
    return [[j for j in range(i)] for i in range(n)]


def total(xs):
    return sum(x * 2 for x in xs) + len([y for y in xs if y > 1])


def joined(xs):
    return ",".join(str(x) for x in xs)


def capture(n):
    fs = [lambda: i for i in range(n)]
    return fs[0]()


print(squares(5), pairs([1, 2], [2, 3]), table(3), unique([1, 4, 7]), nested(3), total([1, 2, 3]), joined([1, 2]), capture(2))
