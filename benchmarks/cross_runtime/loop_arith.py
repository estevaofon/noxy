# Loop apertado: despacho de bytecode e aritmetica inteira, sem alocacao.
def main():
    i = 0
    s = 0
    while i < 3000000:
        s = (s + i * 3) % 1000003
        i = i + 1
    print(f"CHECKSUM:{s}")


main()
