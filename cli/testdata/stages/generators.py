def countdown(n: int):
    while n > 0:
        yield n
        n -= 1


print(sum(countdown(3)))
