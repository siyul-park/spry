import os.path as p, sys
from . import x
from ..a.b import (c as d, e,)
from .m import *
x = y = 1
a, *b = c = [1, 2, 3]
(a) = 1
x: int
(y): int = 3
z.w: "T" = 4
n += 1
n @= m
del a, (b, c), d[0], e.f
assert x, "message"
raise ValueError("bad") from None
global g1, g2
pass; break; continue
def f(a, b=1, /, c=2, *args, d, e=3, **kw) -> int:
    return a
async def g(*, k): await k
@dec
@dec2(1)
class C(Base, metaclass=M):
    x: int = 1
    def m(self): ...
async def h():
    async with a as b, c: pass
    async for i in j: pass
    else: pass
for i, (j, *k) in x: pass
while x: break
else: pass
if a: pass
elif b: pass
else: pass
with (open(f) as f1, open(g) as g1,): pass
with (a, b): pass
with (a, b) as c: pass
with a as (b, c): pass
try: pass
except* (A, B) as e: pass
finally: pass
try: pass
except A, B: pass
except: pass
else: pass
type X[T: int = str, *Ts = *tuple[int], **P = [int]] = list[T]
def gen[T](x: T) -> T: return x
class K[T]: pass
yield
x = yield from y
lambda: 0
lambda a, /, b=1, *c, d, **e: (a, b)
