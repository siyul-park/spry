def squares(n):
    total = 0
    for i in range(n):
        total += i * i
    return total


def search(limit):
    for i in range(2, limit):
        if i * i > limit:
            break
    else:
        return -1
    return i


def nested():
    count = 0
    for i in range(1, 4):
        for j in range(i, 4, 2):
            count += j
    return count


def text(s):
    n = 0
    for ch in s:
        if ch == "a":
            n += 1
    return n


print(squares(10), search(50), nested(), text("banana"))
