counter = 0
LIMIT = 3
label: str = "n"


def bump(by):
    global counter
    counter += by
    return counter


def show():
    return label + str(counter)


for i in range(LIMIT):
    bump(i)
print(show(), __name__)
