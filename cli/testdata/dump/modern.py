async def g():
    async with a as b, c:
        await d
match p:
    case [1, *r] if r:
        pass
    case {"k": v}:
        pass
type T[X] = list[X]
