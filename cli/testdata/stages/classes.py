class Counter:
    def __init__(self, start: int) -> None:
        self.value = start

    def bump(self, by: int) -> int:
        self.value = self.value + by
        return self.value


c = Counter(1)
print(c.bump(2))
