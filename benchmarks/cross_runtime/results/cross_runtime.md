# Cross-runtime: Noxy x CPython x Lua x Go

- noxy: `C:\Users\sandr\AppData\Local\Temp\claude\C--Users-sandr-Documents-noxy\58670b25-86ee-451c-a716-ecd4cec33bde\scratchpad\bench\noxy_str.exe` (Noxy v0.15.0)
- v0150: `C:\Users\sandr\AppData\Local\Temp\claude\C--Users-sandr-Documents-noxy\58670b25-86ee-451c-a716-ecd4cec33bde\scratchpad\bench\noxy_base.exe` (Noxy v0.15.0)
- python: Python 3.14.7
- lua: ausente
- go: go version go1.26.6 windows/amd64
- Data: 2026-08-22T20:47:49
- Runs por bench: 9, intercalados; **minimo** reportado

## Tempo total (ms)

| bench | noxy | v0150 | python | go |
|---|---|---|---|---|
| `bubblesort` | 140,0 | 137,1 | 91,8 | - |
| `fib` | 208,5 | 213,5 | 118,5 | 10,4 |
| `loop_arith` | 209,7 | 213,4 | 212,0 | 15,0 |
| `mandelbrot` | 149,0 | 146,7 | 96,5 | - |
| `map_churn` | 126,0 | 143,0 | 78,6 | - |
| `startup` | 11,0 | 10,7 | 20,6 | 7,2 |
| `string_ops` | 85,2 | 106,9 | 51,3 | - |

## Tempo de execucao, descontado o piso de `startup` (ms)

| bench | noxy | v0150 | python | go |
|---|---|---|---|---|
| `bubblesort` | 129,0 | 126,4 | 71,2 | - |
| `fib` | 197,5 | 202,8 | 97,9 | ~0 |
| `loop_arith` | 198,7 | 202,7 | 191,4 | 7,8 |
| `mandelbrot` | 138,0 | 136,0 | 75,9 | - |
| `map_churn` | 115,0 | 132,3 | 58,0 | - |
| `string_ops` | 74,2 | 96,2 | 30,7 | - |

`~0` = o trabalho cabe dentro do ruido do piso de processo do runtime.

## Razoes sobre o tempo liquido (noxy / outro)

| bench | / v0150 | / python | / go |
|---|---|---|---|
| `bubblesort` | 1,02x | 1,81x | - |
| `fib` | 0,97x | 2,02x | - |
| `loop_arith` | 0,98x | 1,04x | 25,47x |
| `mandelbrot` | 1,01x | 1,82x | - |
| `map_churn` | 0,87x | 1,98x | - |
| `string_ops` | 0,77x | 2,42x | - |

Menor e melhor. `-` = um dos lados cai dentro do ruido do piso e a razao nao tem significado.
