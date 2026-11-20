class Vec:
    def __init__(self, x, y):
        self.x = x
        self.y = y

    def __add__(self, o):
        return Vec(self.x + o.x, self.y + o.y)

    def __radd__(self, o):
        return Vec(self.x + o, self.y + o)

    def __neg__(self):
        return Vec(-self.x, -self.y)

    def __eq__(self, o):
        return self.x == o.x and self.y == o.y

    def __lt__(self, o):
        return self.x < o.x

    def __bool__(self):
        return self.x != 0 or self.y != 0

    def __len__(self):
        return 2

    def __contains__(self, v):
        return v == self.x or v == self.y

    def __getitem__(self, i):
        if i == 0:
            return self.x
        return self.y

    def __setitem__(self, i, v):
        if i == 0:
            self.x = v
        else:
            self.y = v

    def __iadd__(self, o):
        self.x += o.x
        self.y += o.y
        return self

    def __repr__(self):
        return "Vec"

    def __str__(self):
        return f"({self.x}, {self.y})"


class Count:
    def __init__(self, n):
        self.i = 0
        self.n = n

    def __iter__(self):
        return self

    def __next__(self):
        if self.i >= self.n:
            raise StopIteration
        self.i += 1
        return self.i


a = Vec(1, 2)
b = a + a
c = 1 + b
d = -c
v = a[0] + b[1]
a[1] = 9
a += b
if a == b or a < b or not a != b:
    print(len(a), 1 in a, bool(a))
total = 0
for k in Count(3):
    total += k
print(total, a, repr(b), f"{c}")
