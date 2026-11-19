a = f"x{y}z"
b = f"{{literal}} {x!r:>{width}} {x=}"
c = f'{"nested"}' f"""multi
{line}"""
d = rf"\d{n}" + fr'\{n}'
e = f"\N{BULLET} {a:{b}.{c}}"
f = f"{x:%Y-%m-%d}" f"{lambda: 1}"
