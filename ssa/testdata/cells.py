def f(flag: bool) -> int:
    def g() -> int:
        return x
    if flag:
        x = 1
    return g()


print(f(True))
