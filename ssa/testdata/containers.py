xs = [3, 1, 2]
xs.append(4)
xs.extend([5, 6])
xs.insert(0, 9)
xs[1] = 7
xs[-1] += 1
del xs[0]
last = xs.pop()
head = xs[:2]
tail = xs[1:]
rev = xs[::-1]
both = xs + head
twice = head * 2
size = len(xs) + len(both)
found = 7 in xs
missing = 7 not in xs
xs.sort()
xs.reverse()
best = max(xs)
total = sum(xs)
ordered = sorted(xs, reverse=True)
d = {"a": 1, "b": 2}
d["c"] = 3
n = d["a"] + d.get("z", 0)
for key in d:
    n += d[key]
for key, value in d.items():
    n += value
keys = list(d.keys())
s = {1, 2}
s.add(3)
s.discard(1)
pair = (1, "x")
first = pair[0]
last_item = pair[-1]
grid = [[0] * 3 for _ in range(3)]
grid[1][2] = 5
empty = []
empty.append("a")
t = tuple(xs)
copy = list(xs)
for i, v in enumerate(xs):
    n += i * v
for a, b in zip(xs, copy):
    n += a - b
