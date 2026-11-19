def counter(start):
    count = start

    def tick(step=1):
        nonlocal count
        count += step
        return count

    return tick


def compose(f, g):
    def run(x):
        return f(g(x))

    return run


def outer(n):
    def middle(m):
        def inner(k):
            return n + m + k

        return inner(1)

    return middle(2)


def apply(f, x):
    return f(f(x))


def fact(n):
    def go(k, acc):
        if k <= 1:
            return acc
        return go(k - 1, acc * k)

    return go(n, 1)


def adder(n):
    return lambda x: x + n


c = counter(10)
print(c(), c(5), compose(adder(1), adder(2))(3), outer(1), apply(adder(3), 1), fact(5))
