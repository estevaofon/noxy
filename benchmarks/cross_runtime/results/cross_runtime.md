# Cross-runtime: Noxy x CPython x Lua x Go

- noxy: Noxy v0.5.0, build de `bench/cross-runtime` sobre develop @ 21e7b96, em disco local
- python: Python 3.13.1
- lua: Lua 5.4.7  Copyright (C) 1994-2024 Lua.org, PUC-Rio
- go: go version go1.24.11 windows/amd64
- Data: 2026-08-17T23:18:06
- Runs por bench: 9, intercalados; **minimo** reportado

## Tempo total (ms)

| bench | noxy | python | lua | go |
|---|---|---|---|---|
| `bubblesort` | 666,9 | 157,1 | - | - |
| `fib` | 804,9 | 187,7 | 93,8 | 47,8 |
| `loop_arith` | 519,4 | 350,0 | 77,7 | 48,9 |
| `mandelbrot` | 428,6 | 166,7 | - | - |
| `map_churn` | 324,4 | 182,7 | - | - |
| `startup` | 63,0 | 94,3 | 45,5 | 47,6 |
| `string_ops` | 314,4 | 135,3 | - | - |

## Tempo de execucao, descontado o piso de `startup` (ms)

| bench | noxy | python | lua | go |
|---|---|---|---|---|
| `bubblesort` | 603,9 | 62,8 | - | - |
| `fib` | 741,9 | 93,4 | 48,3 | ~0 |
| `loop_arith` | 456,4 | 255,7 | 32,2 | ~0 |
| `mandelbrot` | 365,6 | 72,4 | - | - |
| `map_churn` | 261,4 | 88,4 | - | - |
| `string_ops` | 251,4 | 41,0 | - | - |

`~0` = o trabalho cabe dentro do ruido do piso de processo do runtime.
