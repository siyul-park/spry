def ints(a, b):
    return (a + b) * (a - b) // 3 % 7


def floats(x, y):
    return x * y - x / y + x**2.0


def mixed(n, x):
    return n + x


def bits(a, b):
    return a & b | a ^ b << 2 >> 1


def unary(a, x, t):
    return -a, +a, ~a, -x, -t


print(ints(7, 3))
print(floats(2.0, 4.0))
print(mixed(1, 2.5))
print(bits(12, 10))
print(unary(3, 1.5, True))
