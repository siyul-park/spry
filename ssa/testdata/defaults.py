def greet(name, greeting="hello", punct="!", *, times=1):
    return greeting + " " + name + punct


def scaled(x, factor=-2, offset=len("ab")):
    return x * factor + offset


print(greet("bob"), greet("amy", "hi", times=2))
print(scaled(3), scaled(3, 4), scaled(3, offset=0))
