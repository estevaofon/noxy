# Recursao pura: custo de chamada de funcao + aritmetica de inteiros.
def fib(n: int) -> int:
    if n <= 1:
        return n
    return fib(n - 1) + fib(n - 2)


def main():
    print(f"CHECKSUM:{fib(30)}")


main()
