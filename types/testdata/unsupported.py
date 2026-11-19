class Point:
    def __getattr__(self, name):
        return 1


def make(n):
    return [n, n]


print(make(2), Point().x)
