class Point:
    __match_args__ = ("x", "y")

    def __init__(self, x, y):
        self.x = x
        self.y = y


class Circle:
    def __init__(self, r):
        self.r = r


def where(p):
    match p:
        case Point(0, 0):
            return "origin"
        case Point(x, 0):
            return x
        case Point(x=0, y=yy) if yy > 5:
            return yy
        case Circle(r=r):
            return r
        case _:
            return -1


def shape(s):
    match s:
        case Point() | Circle() if False:
            return 0
        case Point():
            return 1
    return 2


def seq(items):
    match items:
        case []:
            return 0
        case [a]:
            return a
        case [a, b]:
            return a + b
        case [first, *rest]:
            return first + len(rest)


def tup(t):
    match t:
        case (1, y):
            return y
        case (x, *others, z):
            return x + z + len(others)
    return 0


def mapping(d):
    match d:
        case {"a": 1}:
            return 1
        case {"b": v, **rest}:
            return v + len(rest)
    return 0


def literal(n, s):
    match n:
        case 1 | 2:
            r = "small"
        case 3:
            r = "three"
        case other:
            r = str(other)
    match s:
        case "x":
            return r + "!"
        case None:
            return r
    return s


print(where(Point(0, 0)), shape(Point(1, 2)), seq([1, 2, 3]), tup((1, 2)), mapping({"b": 1}), literal(3, "x"))
