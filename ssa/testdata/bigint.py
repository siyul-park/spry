BIG = 1 << 100


def grow(n):
    return n * 340282366920938463463374607431768211456 + BIG


def power(n):
    return n**64


print(grow(3))
print(power(2))
print(7**2, 2**-1)
