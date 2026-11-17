match x:
    case 1 | 2: pass
    case -1.5 | 1+2j | -3-4j: pass
    case "a" "b" | b"c": pass
    case None | True | False: pass
    case a.b.c: pass
    case [a, b, *rest]: pass
    case (a, b) | (a,) | (): pass
    case a, b: pass
    case *a, b: pass
    case {"k": v, **rest}: pass
    case {1: _, a.b: [c]}: pass
    case {}: pass
    case C(): pass
    case C(1, 2, k=v) if v > 0: pass
    case a.C(x=1,): pass
    case (1 | 2) as z: pass
    case x if x: pass
    case _: pass
match a, b:
    case _: pass
match (x := 1):
    case _: pass
match = 1
match.x = 2
match(x)
case = 3
type = 4
