# Cross-runtime: Noxy x CPython x Lua x Go

- noxy: `C:\Users\sandr\AppData\Local\Temp\claude\C--Users-sandr-Documents-noxy\58670b25-86ee-451c-a716-ecd4cec33bde\scratchpad\bench\noxy4_s1.exe` (Noxy v0.15.2)
- v0152: `C:\Users\sandr\AppData\Local\Temp\claude\C--Users-sandr-Documents-noxy\58670b25-86ee-451c-a716-ecd4cec33bde\scratchpad\bench\noxy_base4.exe` (Noxy v0.15.1)
- python: Python 3.14.7
- lua: Lua 5.4.6  Copyright (C) 1994-2023 Lua.org, PUC-Rio
- go: go version go1.26.6 windows/amd64
- Data: 2026-08-22T22:03:21
- Runs por bench: 9, intercalados; **minimo** reportado

## Tempo total (ms)

| bench | noxy | v0152 | python | lua | go |
|---|---|---|---|---|---|
| `bubblesort` | 117,1 | 113,5 | 84,6 | - | - |
| `fib` | 119,0 | 118,7 | 108,2 | 48,8 | 9,8 |
| `loop_arith` | 200,1 | 210,1 | 201,6 | 46,9 | 14,0 |
| `mandelbrot` | 142,3 | 144,1 | 92,6 | - | - |
| `map_churn` | 113,0 | 127,6 | 77,1 | - | - |
| `startup` | 9,4 | 9,6 | 18,8 | 7,2 | 6,6 |
| `string_ops` | 76,8 | 78,8 | 50,6 | - | - |

## Tempo de execucao, descontado o piso de `startup` (ms)

| bench | noxy | v0152 | python | lua | go |
|---|---|---|---|---|---|
| `bubblesort` | 107,7 | 103,9 | 65,8 | - | - |
| `fib` | 109,6 | 109,1 | 89,4 | 41,6 | ~0 |
| `loop_arith` | 190,7 | 200,5 | 182,8 | 39,7 | 7,4 |
| `mandelbrot` | 132,9 | 134,5 | 73,8 | - | - |
| `map_churn` | 103,6 | 118,0 | 58,3 | - | - |
| `string_ops` | 67,4 | 69,2 | 31,8 | - | - |

`~0` = o trabalho cabe dentro do ruido do piso de processo do runtime.

## Razoes sobre o tempo liquido (noxy / outro)

| bench | / v0152 | / python | / lua | / go |
|---|---|---|---|---|
| `bubblesort` | 1,04x | 1,64x | - | - |
| `fib` | 1,00x | 1,23x | 2,63x | - |
| `loop_arith` | 0,95x | 1,04x | 4,80x | 25,77x |
| `mandelbrot` | 0,99x | 1,80x | - | - |
| `map_churn` | 0,88x | 1,78x | - | - |
| `string_ops` | 0,97x | 2,12x | - | - |

Menor e melhor. `-` = um dos lados cai dentro do ruido do piso e a razao nao tem significado.
