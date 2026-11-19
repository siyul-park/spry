inc = lambda x: x + 1
add = lambda a, b=2: a + b
nums = sorted([3, 1, 2], key=lambda v: -v)
total = inc(1) + add(1) + add(1, 5)
pick = (lambda: 7)()


def run(fs, x):
    return [f(x) for f in fs]


print(total, nums, pick, run([inc, lambda y: y * 2], 3))
