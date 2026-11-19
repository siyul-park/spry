def f(x):
    if x:
        # comment on its own line

        return [
            1,
            2,  # trailing
        ]
    else:
        return x \
        + 1


class C:
    pass
