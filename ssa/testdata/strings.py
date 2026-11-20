def label(name, n, x):
    return f"{name}: {n} {x!s} {n:>4}"


def join(a, b):
    return a + b + "!"


def empty():
    return f""


print(label("n", 3, 1.5))
print(join("a", "b"), empty())
print(len("abc"), len(join("x", "y")))
word = "spry"
shout = word.upper() + "!" * 3
width = 8
text = f"{word!r:>{width}} {shout!s} {word!a} {width:04d} {width * 2}"
first = word[0]
rest = word[1:]
every = word[::2]
parts = "a,b,c".split(",")
joined = "-".join(parts)
same = word == "spry" and word < "z"
has = "p" in word
count = len(word) + word.count("r")
