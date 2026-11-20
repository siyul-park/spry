class P:
    def __init__(self, n):
        self.n = n

    def __lt__(self, o):
        return self.n < o.n

    def __add__(self, o):
        return P(self.n + o.n)

    def __radd__(self, o):
        return P(self.n + o)


ps = [P(3), P(1), P(2)]
a = sorted(ps)
b = sorted(ps, key=lambda p: p.n, reverse=True)
c = sorted([3, 1, 2], key=lambda v: -v)
d = min(ps)
e = max(ps, key=lambda p: p.n)
f = max(ps[0], ps[1])
g = sum(ps, P(0))
h = sorted([3, 1, 2])
print(a[0].n, b[0].n, c, d.n, e.n, f.n, g.n, h)
