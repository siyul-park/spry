def cleanup(log, n):
    for i in range(n):
        try:
            if i == 1:
                continue
            if i == 3:
                break
            log.append(i)
        finally:
            log.append(-1)
    return log


def early(n):
    try:
        if n:
            return 1
        x = 2
    finally:
        n = 0
    return x


def swallow(n):
    try:
        return 10 // n
    except ZeroDivisionError:
        return -1
    finally:
        print("done")


def nested_finally(log):
    try:
        try:
            log.append(1)
        finally:
            log.append(2)
    finally:
        log.append(3)
    return log


print(cleanup([], 5), early(1), swallow(0), nested_finally([]))
