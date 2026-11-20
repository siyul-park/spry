def twice(x):
    return x + x


def fib(n):
    if n < 2:
        return n
    return fib(n - 1) + fib(n - 2)


def even(n):
    if n == 0:
        return True
    return odd(n - 1)


def odd(n):
    if n == 0:
        return False
    return even(n - 1)


def total(*values, scale=1, **extra):
    t = 0
    for v in values:
        t += v
    return t * scale


print(twice(2), twice(1.5), twice("ab"))
print(fib(10), even(10))
print(total(1, 2, 3, scale=2))
