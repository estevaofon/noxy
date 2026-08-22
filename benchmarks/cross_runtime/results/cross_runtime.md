# Cross-runtime: Noxy x CPython x Lua x Go

- noxy: `C:\Users\estev\AppData\Local\Temp\claude\D--OneDrive-Documentos-go-projects-noxy\ef3672bf-bc7a-4367-818c-fb10c1f93a42\scratchpad\bench\noxy_s12p.exe` (Noxy v0.14.3)
- v0142: `C:\Users\estev\AppData\Local\Temp\claude\D--OneDrive-Documentos-go-projects-noxy\ef3672bf-bc7a-4367-818c-fb10c1f93a42\scratchpad\bench\noxy_base.exe` (Noxy v0.14.2)
- python: Python 3.13.1
- lua: Lua 5.4.7  Copyright (C) 1994-2024 Lua.org, PUC-Rio
- go: go version go1.24.11 windows/amd64
- Data: 2026-08-22T16:43:10
- Runs por bench: 9, intercalados; **minimo** reportado

## Tempo total (ms)

| bench | noxy | v0142 | python | lua | go |
|---|---|---|---|---|---|
| `bubblesort` | 514,4 | 716,4 | 165,4 | - | - |
| `fib` | 387,3 | 511,7 | 191,9 | 114,7 | 75,0 |
| `loop_arith` | 363,8 | 418,6 | 346,4 | 103,9 | 79,7 |
| `mandelbrot` | 268,2 | 310,1 | 179,1 | - | - |
| `map_churn` | 265,0 | 317,6 | 171,2 | - | - |
| `startup` | 91,3 | 91,0 | 88,5 | 61,9 | 71,4 |
| `string_ops` | 225,8 | 236,8 | 120,4 | - | - |

## Tempo de execucao, descontado o piso de `startup` (ms)

| bench | noxy | v0142 | python | lua | go |
|---|---|---|---|---|---|
| `bubblesort` | 423,1 | 625,4 | 76,9 | - | - |
| `fib` | 296,0 | 420,7 | 103,4 | 52,8 | ~0 |
| `loop_arith` | 272,5 | 327,6 | 257,9 | 42,0 | 8,3 |
| `mandelbrot` | 176,9 | 219,1 | 90,6 | - | - |
| `map_churn` | 173,7 | 226,6 | 82,7 | - | - |
| `string_ops` | 134,5 | 145,8 | 31,9 | - | - |

`~0` = o trabalho cabe dentro do ruido do piso de processo do runtime.

## Razoes sobre o tempo liquido (noxy / outro)

| bench | / v0142 | / python | / lua | / go |
|---|---|---|---|---|
| `bubblesort` | 0,68x | 5,50x | - | - |
| `fib` | 0,70x | 2,86x | 5,61x | - |
| `loop_arith` | 0,83x | 1,06x | 6,49x | 32,83x |
| `mandelbrot` | 0,81x | 1,95x | - | - |
| `map_churn` | 0,77x | 2,10x | - | - |
| `string_ops` | 0,92x | 4,22x | - | - |

Menor e melhor. `-` = um dos lados cai dentro do ruido do piso e a razao nao tem significado.
