class MyError(Exception):
    def __init__(self, code):
        self.code = code


def risky(n):
    if n > 2:
        raise MyError(n)
    if n < 0:
        raise ValueError("negative")
    return n


def guarded(n):
    try:
        v = risky(n)
    except MyError as e:
        v = e.code * 10
    except (ValueError, KeyError):
        v = -1
    else:
        v = v + 1
    return v


def reraise(n):
    try:
        return risky(n)
    except ValueError:
        raise
    except Exception as e:
        raise RuntimeError("wrapped") from e


def bare(n):
    try:
        return risky(n)
    except:
        return 0


def nested(n):
    total = 0
    try:
        try:
            total = risky(n)
        except ValueError:
            total = 1
        total += 1
    except MyError:
        total = 100
    return total


def always():
    raise KeyError("k")


def cls_raise(n):
    if n:
        raise ValueError
    return 1


print(guarded(1), reraise(1), bare(5), nested(2), cls_raise(0))
