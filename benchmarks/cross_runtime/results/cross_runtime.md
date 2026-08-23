# Cross-runtime: Noxy x CPython x Lua x Go

- noxy: `C:\Users\sandr\AppData\Local\Temp\claude\C--Users-sandr-Documents-noxy\58670b25-86ee-451c-a716-ecd4cec33bde\scratchpad\bench\noxy_call.exe` (Noxy v0.15.1)
- v0151: `C:\Users\sandr\AppData\Local\Temp\claude\C--Users-sandr-Documents-noxy\58670b25-86ee-451c-a716-ecd4cec33bde\scratchpad\bench\noxy_base.exe` (Noxy v0.15.1)
- python: Python 3.14.7
- lua: Lua 5.4.6  Copyright (C) 1994-2023 Lua.org, PUC-Rio
- go: go version go1.26.6 windows/amd64
- Data: 2026-08-22T21:40:04
- Runs por bench: 9, intercalados; **minimo** reportado

## Tempo total (ms)

| bench | noxy | v0151 | python | lua | go |
|---|---|---|---|---|---|
| `bubblesort` | 116,4 | 126,4 | 87,6 | - | - |
| `fib` | 115,0 | 194,4 | 109,4 | 48,8 | 9,7 |
| `loop_arith` | 204,3 | 203,0 | 202,3 | 45,6 | 13,9 |
| `mandelbrot` | 143,1 | 142,2 | 93,0 | - | - |
| `map_churn` | 127,1 | 121,6 | 75,6 | - | - |
| `startup` | 9,6 | 9,3 | 18,7 | 6,8 | 6,5 |
| `string_ops` | 78,6 | 83,0 | 50,5 | - | - |

## Tempo de execucao, descontado o piso de `startup` (ms)

| bench | noxy | v0151 | python | lua | go |
|---|---|---|---|---|---|
| `bubblesort` | 106,8 | 117,1 | 68,9 | - | - |
| `fib` | 105,4 | 185,1 | 90,7 | 42,0 | ~0 |
| `loop_arith` | 194,7 | 193,7 | 183,6 | 38,8 | 7,4 |
| `mandelbrot` | 133,5 | 132,9 | 74,3 | - | - |
| `map_churn` | 117,5 | 112,3 | 56,9 | - | - |
| `string_ops` | 69,0 | 73,7 | 31,8 | - | - |

`~0` = o trabalho cabe dentro do ruido do piso de processo do runtime.

## Razoes sobre o tempo liquido (noxy / outro)

| bench | / v0151 | / python | / lua | / go |
|---|---|---|---|---|
| `bubblesort` | 0,91x | 1,55x | - | - |
| `fib` | 0,57x | 1,16x | 2,51x | - |
| `loop_arith` | 1,01x | 1,06x | 5,02x | 26,31x |
| `mandelbrot` | 1,00x | 1,80x | - | - |
| `map_churn` | 1,05x | 2,07x | - | - |
| `string_ops` | 0,94x | 2,17x | - | - |

Menor e melhor. `-` = um dos lados cai dentro do ruido do piso e a razao nao tem significado.
