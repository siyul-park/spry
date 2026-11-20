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


def grow(n):
    if n == 0:
        return 0
    return grow(n - 1) + 0.5


print(fib(10), even(4), grow(3))
