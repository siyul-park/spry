def grow(n):
    if n == 0:
        return 0
    return grow(n - 1) + 0.5


def pick(flag, a, b):
    x = a if flag else b
    return x or 1


print(grow(2), pick(True, 1, 2.5), pick(False, "s", "t"))
