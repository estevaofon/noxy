# Construcao, concatenacao e fatiamento de strings curtas.
def main():
    i = 0
    acc = 0
    while i < 200000:
        s = "item_" + str(i)
        acc = acc + len(s)
        if s[5:6] == "1":
            acc = acc + 1
        i = i + 1
    print(f"CHECKSUM:{acc}")


main()
