# Cross-runtime: Noxy x CPython x Lua x Go

- noxy: `C:\Users\estev\AppData\Local\Temp\claude\D--OneDrive-Documentos-go-projects-noxy\ead4c52f-5869-403e-a45b-22421c6f07b9\scratchpad\bench\noxy_s3.exe` (Noxy v0.15.0)
- v0143: `C:\Users\estev\AppData\Local\Temp\claude\D--OneDrive-Documentos-go-projects-noxy\ead4c52f-5869-403e-a45b-22421c6f07b9\scratchpad\bench\noxy_base.exe` (Noxy v0.14.3)
- python: Python 3.13.1
- lua: Lua 5.4.7  Copyright (C) 1994-2024 Lua.org, PUC-Rio
- go: go version go1.24.11 windows/amd64
- Data: 2026-08-22T18:55:12
- Runs por bench: 9, intercalados; **minimo** reportado

## Tempo total (ms)

| bench | noxy | v0143 | python | lua | go |
|---|---|---|---|---|---|
| `bubblesort` | 249,7 | 526,3 | 169,2 | - | - |
| `fib` | 406,0 | 417,7 | 209,7 | 112,2 | 76,1 |
| `loop_arith` | 379,3 | 364,2 | 371,7 | 102,0 | 76,6 |
| `mandelbrot` | 283,4 | 280,8 | 164,2 | - | - |
| `map_churn` | 291,4 | 269,3 | 164,1 | - | - |
| `startup` | 96,1 | 95,7 | 83,7 | 60,1 | 72,2 |
| `string_ops` | 230,5 | 234,5 | 124,1 | - | - |

## Tempo de execucao, descontado o piso de `startup` (ms)

| bench | noxy | v0143 | python | lua | go |
|---|---|---|---|---|---|
| `bubblesort` | 153,6 | 430,6 | 85,5 | - | - |
| `fib` | 309,9 | 322,0 | 126,0 | 52,1 | ~0 |
| `loop_arith` | 283,2 | 268,5 | 288,0 | 41,9 | ~0 |
| `mandelbrot` | 187,3 | 185,1 | 80,5 | - | - |
| `map_churn` | 195,3 | 173,6 | 80,4 | - | - |
| `string_ops` | 134,4 | 138,8 | 40,4 | - | - |

`~0` = o trabalho cabe dentro do ruido do piso de processo do runtime.

## Razoes sobre o tempo liquido (noxy / outro)

| bench | / v0143 | / python | / lua | / go |
|---|---|---|---|---|
| `bubblesort` | 0,36x | 1,80x | - | - |
| `fib` | 0,96x | 2,46x | 5,95x | - |
| `loop_arith` | 1,05x | 0,98x | 6,76x | - |
| `mandelbrot` | 1,01x | 2,33x | - | - |
| `map_churn` | 1,12x | 2,43x | - | - |
| `string_ops` | 0,97x | 3,33x | - | - |

Menor e melhor. `-` = um dos lados cai dentro do ruido do piso e a razao nao tem significado.
