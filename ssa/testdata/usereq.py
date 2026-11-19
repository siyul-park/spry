class Key:
    def __init__(self, n):
        self.n = n

    def __eq__(self, other):
        return self.n == other.n

    def __hash__(self):
        return self.n


class Plain:
    pass


def lookup(items, k):
    d = {Key(1): "a", Key(2): "b"}
    d[k] = "c"
    s = {Key(3)}
    s.add(k)
    found = k in items
    pos = items.index(k)
    return d[k], len(s), found, pos


def untouched(a, b):
    xs = [Plain(), Plain()]
    return a in xs, {a: 1}[b]


print(lookup([Key(1), Key(2)], Key(2)))
