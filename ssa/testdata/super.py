class Base:
    def __init__(self, x):
        self.x = x

    def show(self):
        return self.x


class Left(Base):
    def show(self):
        return super().show() + 1


class Right(Base):
    def show(self):
        return super().show() + 2


class Both(Left, Right):
    def show(self):
        return super().show() * 10


class Plain(Left):
    def __init__(self, x):
        super().__init__(x)
        self.y = x + 1

    def show(self):
        return super(Plain, self).show() + self.y


b = Both(1)
print(b.show(), Plain(3).show(), Left(4).show())
