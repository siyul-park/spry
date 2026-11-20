class Temp:
    scale = 10

    def __init__(self, deg):
        self._deg = deg

    @property
    def deg(self):
        return self._deg

    @deg.setter
    def deg(self, value):
        self._deg = value

    @property
    def double(self):
        return self._deg * 2

    @staticmethod
    def zero():
        return 0

    @classmethod
    def make(cls, d):
        return cls(d * cls.scale)

    def shifted(self, by):
        return Temp(self._deg + by)


t = Temp.make(2)
t.deg = 7
t.deg += 1
u = t.shifted(Temp.zero())
print(t.deg, u.double, Temp.zero())
