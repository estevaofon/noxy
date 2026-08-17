# Cross-runtime: Noxy × CPython × Lua × Go

Comparação pontual de performance entre o VM do Noxy e outros runtimes, para
ter uma referência externa de onde o interpretador está hoje. Não é uma suíte
extensa: são seis cargas CPU-bound curtas, escolhidas para isolar subsistemas
diferentes do VM, mais um piso de processo.

## Como rodar

```powershell
./run_cross_runtime.ps1 -Noxy C:\caminho\local\noxy.exe
```

Saída em `results/cross_runtime.md`. `lua` e `go` são opcionais — se não
estiverem no `PATH`, viram `-` na tabela em vez de erro.

## Cobertura

| bench | subsistema exercitado | noxy | python | lua | go |
|---|---|---|---|---|---|
| `startup` | subir processo, compilar, imprimir uma linha | ✅ | ✅ | ✅ | ✅ |
| `loop_arith` | despacho de bytecode e aritmética inteira | ✅ | ✅ | ✅ | ✅ |
| `fib` | chamada de função (~2,7M chamadas) | ✅ | ✅ | ✅ | ✅ |
| `bubblesort` | leitura/escrita indexada em array, O(n²) | ✅ | ✅ | – | – |
| `map_churn` | hashmap com chave string | ✅ | ✅ | – | – |
| `string_ops` | construção, concatenação e fatiamento | ✅ | ✅ | – | – |
| `mandelbrot` | aritmética de ponto flutuante | ✅ | ✅ | – | – |

Noxy e CPython são a comparação principal. **Lua 5.4 e Go entraram como
calibração** em três benches, para responder "longe do quê?":

- **Lua** é o comparável mais direto que existe para o Noxy — interpretador de
  bytecode puro, sem JIT, sem inline cache. A diferença é que ele é
  *dinamicamente* tipado, então não tem a informação que o Noxy tem em tempo
  de compilação.
- **Go** é o teto do hospedeiro: quanto do tempo é o trabalho em si e quanto é
  a camada de interpretação.

Cada implementação de um bench roda **o mesmo algoritmo**, escrito da mesma
forma: mesmos laços, mesmas operações, sem substituir o corpo por builtin
nativo de um lado só. Todas imprimem uma linha `CHECKSUM:` e o runner aborta se
divergirem — isso garante que a comparação é da mesma carga, não de programas
parecidos.

Os quatro rodam dentro de uma função, e não em escopo de módulo: no CPython,
variável local é acesso por índice em array e global é lookup em dict, o que
sozinho já valeria ~2x de vantagem indevida ao Python.

## Metodologia

Três decisões, cada uma por um efeito medido neste repo:

1. **Intercalado.** Os runtimes alternam dentro da mesma janela de tempo, como
   em `interleaved_compare.ps1`. Rodar todas as amostras de um runtime e depois
   do outro deixa a comparação exposta a drift térmico e carga de background.
2. **Mínimo, não mediana.** Sob carga a distribuição é assimétrica à direita:
   interferência só adiciona tempo, nunca remove; o mínimo é a amostra menos
   contaminada. Em dois lotes idênticos aqui, a mediana variou ~12% enquanto o
   mínimo manteve o ratio estável.
3. **Disco local.** O runner copia os fontes para `%TEMP%` antes de medir.
   Medir com os fontes no diretório do repo (que fica em OneDrive) inflou os
   tempos em ~2x, por filtro de sync e antivírus no read. Aponte `-Noxy` para
   um binário em disco local também.

## Resultados

Noxy v0.5.0 (sobre develop @ 21e7b96) · CPython 3.13.1 · Lua 5.4.7 · Go 1.24.11
· i7-1165G7 (4C/8T) · Windows 11 · mínimo de 9 execuções intercaladas. Números
completos em [`results/cross_runtime.md`](results/cross_runtime.md).

### Tempo total (ms)

| bench | noxy | python | lua | go |
|---|---|---|---|---|
| `startup` | 63,0 | 94,3 | 45,5 | 47,6 |
| `loop_arith` | 519,4 | 350,0 | 77,7 | 48,9 |
| `map_churn` | 324,4 | 182,7 | – | – |
| `mandelbrot` | 428,6 | 166,7 | – | – |
| `string_ops` | 314,4 | 135,3 | – | – |
| `bubblesort` | 666,9 | 157,1 | – | – |
| `fib` | 804,9 | 187,7 | 93,8 | 47,8 |

### Tempo de execução, descontado o piso de `startup` (ms)

| bench | noxy | python | lua | go | noxy ÷ python | noxy ÷ lua |
|---|---|---|---|---|---|---|
| `loop_arith` | 456,4 | 255,7 | 32,2 | ~0 | **1,8x** | **14,2x** |
| `map_churn` | 261,4 | 88,4 | – | – | **3,0x** | – |
| `mandelbrot` | 365,6 | 72,4 | – | – | **5,1x** | – |
| `string_ops` | 251,4 | 41,0 | – | – | **6,1x** | – |
| `fib` | 741,9 | 93,4 | 48,3 | ~0 | **7,9x** | **15,4x** |
| `bubblesort` | 603,9 | 62,8 | – | – | **9,6x** | – |

`~0` = o trabalho cabe dentro do ruído do piso de processo do runtime.

### Estabilidade entre rodadas

Cinco suítes completas em condições de carga diferentes. **O ranking é idêntico
nas cinco**; as magnitudes variam:

| bench | noxy ÷ python (faixa nas 5 rodadas) |
|---|---|
| `loop_arith` | 1,6x – 1,9x |
| `map_churn` | 2,4x – 3,2x |
| `mandelbrot` | 4,6x – 5,1x |
| `string_ops` | 4,4x – 7,1x |
| `fib` | 6,5x – 7,9x |
| `bubblesort` | 7,4x – 9,6x |

Contra o Lua (duas rodadas): `loop_arith` 10,7x – 14,2x, `fib` 14,0x – 15,4x.
No `startup`, o Noxy fica 1,3x – 1,5x à frente do CPython.

**Limitação conhecida, e é a primeira coisa a corrigir nesta suíte:** o número
líquido é razão entre *diferenças*, então ele amplifica qualquer variação no
piso de processo. Onde o trabalho do runtime é pequeno perto do próprio piso, a
barra de erro fica grande — em `string_ops` o CPython faz ~41 ms de trabalho com
um piso de ~75–94 ms, e por isso o ratio oscila de 4,4x a 7,1x. `loop_arith` e
`mandelbrot`, cujo trabalho domina o piso, ficam dentro de ±10%. O conserto é
aumentar as contagens de iteração até o trabalho de cada runtime ser pelo menos
~5x o piso dele; ainda não foi feito.

Trate o resultado como **ordem de grandeza com ranking confiável**, não como
número de três dígitos significativos.

## Leitura

**O CPython é uma régua enganosa.** Ele não é o piso da comparação — é o meio
da tabela. O Lua 5.4, com um VM mais simples e sem tipos estáticos, é ~2x mais
rápido que o CPython no `fib` e ~8x no loop apertado. Contra o Lua, o Noxy fica
**14x a 15x mais lento**; contra o CPython, 1,8x a 9,6x.

**O custo não está distribuído por igual — ele se concentra:**

- `loop_arith` é o piso: laço `while` com aritmética inteira e atribuição de
  local, sem chamada, sem índice, sem alocação. A 1,8x do CPython, o despacho
  de bytecode puro do Noxy é competitivo com o dele. Mas o mesmo bench está a
  14,2x do Lua — ou seja, "competitivo com o CPython" aqui é fraqueza do
  CPython em loop numérico, não força do Noxy.
- A partir daí o gap abre com o *tipo* de operação, não com o volume:
  hashmap 3,0x, ponto flutuante 5,1x, string 6,1x, e o pior ponto em
  **chamada de função (7,9x)** e **acesso indexado a array (9,6x)**.

**A pista mais acionável:** `fib` não toca em nenhum tipo composto — são só
ints. O custo ali é do protocolo de chamada em si (setup de frame), não da
validação de tipos O(n)/chamada já mapeada para maps/structs/refs.

**A inversão estrutural.** O Noxy é estaticamente tipado e compila para
bytecode: os tipos são conhecidos em tempo de compilação, que é exatamente a
informação que CPython e Lua pagam para descobrir em runtime. Hoje isso não
está sendo convertido em velocidade. Some-se que o CPython 3.11+ tem um
interpretador especializador adaptativo com inline caches nas mesmas operações
onde o Noxy mais perde, e o formato do resultado fica consistente com **"falta
camada de especialização"**, não com "o loop principal é ruim".

**Ressalva:** essa atribuição de causa é hipótese lida a partir do formato dos
números, **não de profiling**. Confirmar exige um `pprof` em `fib` e
`bubblesort` antes de otimizar qualquer coisa.

## Onde o Noxy ganha

- **Startup contra o CPython:** 63 ms contra 94 ms, ~1,5x mais rápido — real
  para script curto e para o caso Lambda, onde o piso de processo é boa parte
  do custo. A vantagem, porém, é sobre o
  CPython, não sobre todo mundo (Lua e Go sobem em ~46 ms).
- **Carga I/O-bound:** nos casos que o Noxy realmente atende hoje (servidor
  HTTP, SQLite, NoxyDB), o gargalo é I/O e a velocidade do interpretador quase
  não aparece.
- **Ordem de grandeza:** estar 2x a 10x atrás do CPython em v0.5.0 é resultado
  respeitável em termos absolutos. A maioria das implementações jovens fica
  50x a 100x atrás.

Sendo justo com o Lua: são 30 anos de tuning em C, VM baseada em registradores,
sem GC concorrente e sem bounds check do Go. Não é meta de curto prazo — é a
referência de onde dá para chegar.

## Próximos passos sugeridos

1. Escalar as cargas até o trabalho dominar o piso de processo (ver limitação
   acima) e re-medir.
2. `pprof` em `fib` e `bubblesort` para confirmar onde o tempo vai.
3. Portar os quatro benches restantes para Lua, fechando a calibração.
