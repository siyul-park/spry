def count(n):
    i = 0
    while i < n:
        yield i
        i += 1
    return "done"


def echo():
    total = 0
    while total < 10:
        got = yield total
        total += got


def pairs(xs):
    for x in xs:
        yield x, x * 2


class Bag:
    def __init__(self, items):
        self.items = items

    def each(self):
        for it in self.items:
            yield it


def consume(n):
    total = 0
    for v in count(n):
        total += v
    g = count(3)
    first = next(g)
    rest = list(g)
    return total + first + len(rest)


def collect():
    g = pairs([1, 2])
    return [a + b for a, b in g]


def method(b):
    return sum(b.each()), next(b.each(), 0)


def sender():
    g = echo()
    next(g)
    return g.send(4)


print(sender(), consume(4), collect(), method(Bag([1, 2, 3])))
