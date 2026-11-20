class Ctx:
    def __init__(self, log):
        self.log = log

    def __enter__(self):
        self.log.append("enter")
        return self

    def __exit__(self, kind, exc, tb):
        self.log.append("exit")
        return False


class Quiet:
    def __enter__(self):
        return 1

    def __exit__(self, kind, exc, tb):
        return True


def run(log):
    with Ctx(log) as c:
        log.append("body")
    return log


def swallowed():
    with Quiet() as q:
        raise ValueError("lost")
    return 1


def leave(log, n):
    for i in range(n):
        with Ctx(log):
            if i == 1:
                continue
            if i == 3:
                break
            log.append(i)
    with Ctx(log):
        return log


def two(log):
    with Ctx(log) as a, Ctx(log) as b:
        log.append("inner")
    return log


print(run([]), swallowed(), leave([], 5), two([]))
