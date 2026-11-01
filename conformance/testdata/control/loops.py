total = 0
for i in range(10):
    if i % 2:
        continue
    total += i
print(total)

n = 5
fact = 1
while n > 1:
    fact *= n
    n -= 1
print(fact)

for i in range(3):
    for j in range(3):
        if j == 2:
            break
        print(i, j)
else:
    print("done")

print([x * x for x in range(6) if x % 2 == 0])
print({k: k + 1 for k in range(3)})
