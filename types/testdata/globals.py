counter = 0
scale = 1.5


def bump():
    global counter
    counter += 1
    return counter


def scaled():
    return scale * counter


print(bump(), scaled(), __name__)
