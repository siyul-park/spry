import math
from math import sqrt, pi
import pkg.util
from pkg.util import add, STEP


def area(r):
    return pi * r * r


print(math.sqrt(16.0), sqrt(2.0), area(2.0))
print(add(1, STEP), pkg.util.add(2, 3))
