def run(xs):
    if (n := len(xs)) > 2:
        return n
    total = 0
    while (m := total + 1) < 5:
        total = m
    ys = [y for x in xs if (y := x * 2) > 2]
    return total + (z := 3) + z + len(ys)


print(run([1, 2, 3]), run([1]))
