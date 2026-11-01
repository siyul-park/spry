s = "Hello, World"
print(s.upper(), s.lower(), len(s))
print(s[0], s[-1], s[2:5], s[::-1])
print("a,b,c".split(","), "-".join(["x", "y", "z"]))
print(f"{s!r} {len(s):>4} {3.14159:.2f}")
print("abc" * 2, "b" in "abc", "abc".replace("b", "B"))
print("  pad ".strip() + "|", "x".center(5, "*"))
