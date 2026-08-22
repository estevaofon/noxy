# Cross-runtime: Noxy x CPython x Lua x Go

- noxy: `noxy_0141.exe` (Noxy v0.14.1)
- noxy_v060: `noxy_060.exe` (Noxy v0.6.0)
- python: Python 3.13.1
- lua: Lua 5.4.7  Copyright (C) 1994-2024 Lua.org, PUC-Rio
- go: go version go1.24.11 windows/amd64
- Data: 2026-08-22T12:40:38
- Runs por bench: 9, intercalados; **minimo** reportado

## Tempo total (ms)

| bench | noxy | noxy_v060 | python | lua | go |
|---|---|---|---|---|---|
| `bubblesort` | 646,2 | 608,0 | 161,1 | - | - |
| `fib` | 481,6 | 447,9 | 199,2 | 114,3 | 68,4 |
| `loop_arith` | 385,3 | 390,6 | 343,3 | 99,5 | 72,0 |
| `mandelbrot` | 305,2 | 301,0 | 168,2 | - | - |
| `map_churn` | 293,9 | 289,2 | 161,9 | - | - |
| `startup` | 84,8 | 78,2 | 86,0 | 60,5 | 64,6 |
| `string_ops` | 243,2 | 234,4 | 120,9 | - | - |

## Tempo de execucao, descontado o piso de `startup` (ms)

| bench | noxy | noxy_v060 | python | lua | go |
|---|---|---|---|---|---|
| `bubblesort` | 561,4 | 529,8 | 75,1 | - | - |
| `fib` | 396,8 | 369,7 | 113,2 | 53,8 | ~0 |
| `loop_arith` | 300,5 | 312,4 | 257,3 | 39,0 | 7,4 |
| `mandelbrot` | 220,4 | 222,8 | 82,2 | - | - |
| `map_churn` | 209,1 | 211,0 | 75,9 | - | - |
| `string_ops` | 158,4 | 156,2 | 34,9 | - | - |

`~0` = o trabalho cabe dentro do ruido do piso de processo do runtime.

## Razoes sobre o tempo liquido (noxy / outro)

| bench | / noxy_v060 | / python | / lua | / go |
|---|---|---|---|---|
| `bubblesort` | 1,06x | 7,48x | - | - |
| `fib` | 1,07x | 3,51x | 7,38x | - |
| `loop_arith` | 0,96x | 1,17x | 7,71x | 40,61x |
| `mandelbrot` | 0,99x | 2,68x | - | - |
| `map_churn` | 0,99x | 2,75x | - | - |
| `string_ops` | 1,01x | 4,54x | - | - |

Menor e melhor. `-` = um dos lados cai dentro do ruido do piso e a razao nao tem significado.
