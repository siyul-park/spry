def sign(n):
    if n < 0:
        return -1
    elif n == 0:
        return 0
    else:
        return 1


def count(n):
    total = 0
    while n > 0:
        n -= 1
        if n % 2 == 0:
            continue
        if n > 7:
            break
        total += n
    else:
        total = -total
    return total


print(sign(-4), sign(0), sign(9))
print(count(10))
