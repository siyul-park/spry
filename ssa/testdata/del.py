total = 5
extra = 1
del extra


def locals_del(n):
    x = n + 1
    del x
    y = [1, 2, 3]
    del y[0]
    d = {"a": 1, "b": 2}
    del d["a"]
    return len(y) + len(d)


def maybe(flag):
    v = 1
    if flag:
        del v
    return 2


def pair():
    a = 1
    b = 2
    del a, b
    return 0


print(locals_del(1), maybe(False), pair(), total)
