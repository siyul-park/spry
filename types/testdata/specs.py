def ident(x):
    return x


def add(a, b=1):
    return a + b


def pair(a, b):
    return a, b


ident(1)
ident(2.5)
ident("s")
ident(True)
ident(b"b")
ident(None)
add(1)
add(1.5, 2)
pair(1, "x")
