class Shape:
    def __init__(self, name):
        self.name = name

    def area(self):
        return 0.0

    def describe(self):
        return self.area() + 1.0


class Square(Shape):
    def __init__(self, side):
        Shape.__init__(self, "square")
        self.side = side

    def area(self):
        return self.side * self.side


class Circle(Shape):
    def __init__(self, r):
        Shape.__init__(self, "circle")
        self.r = r

    def area(self):
        return 3.0 * self.r * self.r


class Unit(Square):
    def __init__(self):
        Square.__init__(self, 1.0)


def total(shapes):
    t = 0.0
    for s in shapes:
        t = t + s.describe()
    return t


def pick(flag):
    if flag:
        return Square(2.0)
    return Circle(1.0)


def side(s):
    if isinstance(s, Square):
        return s.side
    return 0.0


items = [Square(2.0), Circle(1.0), Unit()]
print(total(items), side(pick(True)), side(pick(False)))
