# Cross-runtime: Noxy x CPython x Lua x Go

- noxy: `C:\Users\estev\AppData\Local\Temp\noxy_perf_local.exe`
- python: Python 3.13.1
- lua: Lua 5.4.7  Copyright (C) 1994-2024 Lua.org, PUC-Rio
- go: go version go1.24.11 windows/amd64
- Data: 2026-08-18T04:30:06
- Runs por bench: 9, intercalados; **minimo** reportado

## Tempo total (ms)

| bench | noxy | python | lua | go |
|---|---|---|---|---|
| `bubblesort` | 529,8 | 152,3 | - | - |
| `fib` | 380,8 | 171,8 | 85,9 | 40,8 |
| `loop_arith` | 319,4 | 321,9 | 73,1 | 45,0 |
| `mandelbrot` | 246,2 | 149,2 | - | - |
| `map_churn` | 243,8 | 152,3 | - | - |
| `startup` | 49,6 | 75,4 | 38,2 | 38,5 |
| `string_ops` | 194,9 | 119,2 | - | - |

## Tempo de execucao, descontado o piso de `startup` (ms)

| bench | noxy | python | lua | go |
|---|---|---|---|---|
| `bubblesort` | 480,2 | 76,9 | - | - |
| `fib` | 331,2 | 96,4 | 47,7 | ~0 |
| `loop_arith` | 269,8 | 246,5 | 34,9 | 6,5 |
| `mandelbrot` | 196,6 | 73,8 | - | - |
| `map_churn` | 194,2 | 76,9 | - | - |
| `string_ops` | 145,3 | 43,8 | - | - |

`~0` = o trabalho cabe dentro do ruido do piso de processo do runtime.
