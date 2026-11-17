a if b else c
not a and b or c
a < b <= c != d is not e not in f is g in h
-x ** -y
~a | b ^ c & d << e >> f
a + b - c * d / e // f % g @ h
await x
(yield)
(a := 1)
f(x for x in y)
f(a, *b, c=1, **d)
f(a)(b)[c].d
x[1:2, ::3, *a]
x[:]
x[a:=1]
(1, 2,)
()
(a,)
[a, *b]
{1, *a}
{a: b, **c}
{}
{k: v for k, v in x if k if v}
[x async for x in y]
(x for x in y for z in x)
{x for x in y}
"a" "b"
b"a" b"b"
u"x" "y"
r"\d" "\n"
"\N{DIGIT ONE}\x41B\U00000043\101\
d"
1_000 + 0x1F + 0o17 + 0b11 + 1e3 + 1.5j + .5 + 5. + 10**100
...
None, True, False
a.b.c
(a).b
(a)(b)
f"{x}" "y" f"{z!r:>{w}}"
f"{x=}" f"{x = !s}" f"{{}}" f"a{{b" f'{ "a" if x else "b" }'
t"{x}" t"{x=}" t"{x!r:^10}"
f"""multi
{x}
line"""
rf"\{x}" fr'\n{y}'
x = "héllo" + "wörld" ; y = f"é{ñ}ü"
-1 ** 2
*a, b
a if b else (yield)
