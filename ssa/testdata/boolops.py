def logic(a, b):
    if a and b:
        return 1
    if a or not b:
        return 2
    return 3


def value(a, b):
    return a or b


def both(a, b):
    return a and b


def pick(a, b):
    return a if a > b else b


print(logic(1, 0))
print(value(0, 5))
print(both(2, 3))
print(pick(4, 9))
