class Node:
    def __init__(self, v, nxt):
        self.v = v
        self.nxt = nxt


def length(n):
    k = 0
    while n is not None:
        k += 1
        n = n.nxt
    return k


def first(n):
    if n:
        return n.v
    return -1


def last(n):
    if n is None:
        return 0
    if n.nxt is None:
        return n.v
    return last(n.nxt)


def maybe(flag):
    if flag:
        return Node(1, None)
    return None


head = Node(1, Node(2, None))
print(length(head), first(head), last(head), first(None))
m = maybe(True)
print(m.v)
