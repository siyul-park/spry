def make_counter():
    count = 0

    def inc(step=1):
        nonlocal count
        count += step
        return count

    return inc


c = make_counter()
print(c(), c(), c(5))


def fib(n):
    return n if n < 2 else fib(n - 1) + fib(n - 2)


print([fib(i) for i in range(10)])
print((lambda a, *rest, **kw: (a, rest, kw))(1, 2, 3, k=4))
