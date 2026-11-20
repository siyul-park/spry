def check(n):
    assert n > 0
    assert n < 100, "too big"
    assert n != 7, n
    return n


print(check(3))
