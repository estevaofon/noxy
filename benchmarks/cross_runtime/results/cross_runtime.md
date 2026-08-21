# Cross-runtime: Noxy x CPython x Lua x Go

- noxy: `noxy_0130.exe` (Noxy v0.13.0)
- noxy_v060: `noxy_060.exe` (Noxy v0.6.0)
- python: Python 3.13.1
- lua: Lua 5.4.7  Copyright (C) 1994-2024 Lua.org, PUC-Rio
- go: go version go1.24.11 windows/amd64
- Data: 2026-08-21T15:46:06
- Runs por bench: 9, intercalados; **minimo** reportado

## Tempo total (ms)

| bench | noxy | noxy_v060 | python | lua | go |
|---|---|---|---|---|---|
| `bubblesort` | 876,5 | 759,9 | 190,6 | - | - |
| `fib` | 590,5 | 590,5 | 233,7 | 135,0 | 87,6 |
| `loop_arith` | 512,1 | 516,8 | 417,0 | 127,0 | 91,5 |
| `mandelbrot` | 380,1 | 405,7 | 210,1 | - | - |
| `map_churn` | 361,6 | 390,1 | 212,2 | - | - |
| `startup` | 116,3 | 137,4 | 109,3 | 76,7 | 82,6 |
| `string_ops` | 304,3 | 320,8 | 149,3 | - | - |

## Tempo de execucao, descontado o piso de `startup` (ms)

| bench | noxy | noxy_v060 | python | lua | go |
|---|---|---|---|---|---|
| `bubblesort` | 760,2 | 622,5 | 81,3 | - | - |
| `fib` | 474,2 | 453,1 | 124,4 | 58,3 | ~0 |
| `loop_arith` | 395,8 | 379,4 | 307,7 | 50,3 | 8,9 |
| `mandelbrot` | 263,8 | 268,3 | 100,8 | - | - |
| `map_churn` | 245,3 | 252,7 | 102,9 | - | - |
| `string_ops` | 188,0 | 183,4 | 40,0 | - | - |

`~0` = o trabalho cabe dentro do ruido do piso de processo do runtime.

## Razoes sobre o tempo liquido (noxy / outro)

| bench | / noxy_v060 | / python | / lua | / go |
|---|---|---|---|---|
| `bubblesort` | 1,22x | 9,35x | - | - |
| `fib` | 1,05x | 3,81x | 8,13x | - |
| `loop_arith` | 1,04x | 1,29x | 7,87x | 44,47x |
| `mandelbrot` | 0,98x | 2,62x | - | - |
| `map_churn` | 0,97x | 2,38x | - | - |
| `string_ops` | 1,03x | 4,70x | - | - |

Menor e melhor. `-` = um dos lados cai dentro do ruido do piso e a razao nao tem significado.
