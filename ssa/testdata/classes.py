class Counter:
    step = 2
    label: str = "n"

    def __init__(self, start):
        self.value = start
        self.history = []

    def bump(self):
        self.value += self.step
        self.history.append(self.value)
        return self.value

    def reset(self, to=0):
        self.value = to


class Slotted:
    __slots__ = ("a", "b")

    def __init__(self, a, b):
        self.a = a
        self.b = b

    def total(self):
        return self.a + self.b


c = Counter(1)
c.bump()
c.reset()
c.reset(5)
print(c.value, Counter.step, c.label)
s = Slotted(1, 2.5)
print(s.total())
