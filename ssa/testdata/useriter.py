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


class Bag:
    def __init__(self, n):
        self.n = n

    def __iter__(self):
        return Count(self.n)


def squares(bag):
    return [x * x for x in bag]


def spread(bag):
    first, *rest = bag
    return first, rest


def nested(bag):
    try:
        return [x for x in bag if x > 1]
    except ValueError:
        return []


def star(bag):
    return [*bag, 0]


print(squares(Bag(3)), spread(Bag(3)), nested(Bag(3)), star(Bag(2)))
