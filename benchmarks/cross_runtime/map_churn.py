# Escrita e leitura intensa em hashmap de chave string.
def main():
    m = {"seed": 0}
    rnd = 0
    s = 0
    while rnd < 100:
        i = 0
        while i < 3000:
            k = f"k{i % 500}"
            m[k] = rnd * i
            s = s + m[k] % 7
            i = i + 1
        rnd = rnd + 1
    print(f"CHECKSUM:{s}")


main()
