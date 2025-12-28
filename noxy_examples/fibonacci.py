import time

start_time = time.time_ns() // 1_000_000  # now_ms()

def fibonacci(n: int) -> int:
    if n <= 1:
        return n
    return fibonacci(n - 1) + fibonacci(n - 2)

def imprimir_fibonacci(qtd: int) -> None:
    i = 0
    while qtd >= 0:
        print(f"F({i}) = {fibonacci(i)}")
        i = i + 1
        qtd = qtd - 1

imprimir_fibonacci(30)

end_time = time.time_ns() // 1_000_000
elapsed_time = end_time - start_time
print(f"Tempo de execução: {elapsed_time} ms")
