def maybe(c):
    if c:
        x = 1
    return x


def never():
    return y
    y = 2


def late():
    for i in range(0):
        pass
    return i


print(maybe(True), late())
print(never())
