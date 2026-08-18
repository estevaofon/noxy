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

Noxy v0.6.0 (branch `perf/vm-dispatch-fase1`) · CPython 3.13.1 · Lua 5.4.7 ·
Go 1.24.11 · i7-1165G7 (4C/8T) · Windows 11 · mínimo de 9 execuções
intercaladas. Números completos em
[`results/cross_runtime.md`](results/cross_runtime.md).

As colunas `v0.5.0` abaixo são a rodada anterior (sobre develop @ 21e7b96),
mantidas para mostrar o efeito da fase 1 de perf de dispatch e chamadas — o
A/B commit a commit está em [`../RESULTS.md`](../RESULTS.md).

### Tempo total (ms)

| bench | noxy v0.6.0 | noxy v0.5.0 | python | lua | go |
|---|---|---|---|---|---|
| `startup` | **49,6** | 63,0 | 75,4 | 38,2 | 38,5 |
| `loop_arith` | **319,4** | 519,4 | 321,9 | 73,1 | 45,0 |
| `map_churn` | **243,8** | 324,4 | 152,3 | – | – |
| `mandelbrot` | **246,2** | 428,6 | 149,2 | – | – |
| `string_ops` | **194,9** | 314,4 | 119,2 | – | – |
| `bubblesort` | **529,8** | 666,9 | 152,3 | – | – |
| `fib` | **380,8** | 804,9 | 171,8 | 85,9 | 40,8 |

No tempo total o `loop_arith` do Noxy (319,4 ms) passou o do CPython
(321,9 ms) — empate técnico, mas ajudado pelo piso de processo mais baixo; o
número que interessa é o líquido abaixo.

### Tempo de execução, descontado o piso de `startup` (ms)

| bench | noxy v0.6.0 | python | lua | go | ÷ python (v0.5.0 →) | ÷ lua |
|---|---|---|---|---|---|---|
| `loop_arith` | 269,8 | 246,5 | 34,9 | 6,5 | 1,8x → **1,1x** | **7,7x** |
| `map_churn` | 194,2 | 76,9 | – | – | 3,0x → **2,5x** | – |
| `mandelbrot` | 196,6 | 73,8 | – | – | 5,1x → **2,7x** | – |
| `string_ops` | 145,3 | 43,8 | – | – | 6,1x → **3,3x** | – |
| `fib` | 331,2 | 96,4 | 47,7 | ~0 | 7,9x → **3,4x** | **6,9x** |
| `bubblesort` | 480,2 | 76,9 | – | – | 9,6x → **6,2x** | – |

`~0` = o trabalho cabe dentro do ruído do piso de processo do runtime.

Queda no trabalho líquido do próprio Noxy entre as duas versões: `fib` −55,4%,
`mandelbrot` −46,2%, `string_ops` −42,2%, `loop_arith` −40,9%, `map_churn`
−25,7%, `bubblesort` −20,5%. O `string_ops` não foi alvo de nenhuma otimização
desta fase — o ganho ali vem das chamadas e do laço, que qualquer programa
paga.

### Estabilidade entre rodadas (medida na v0.5.0)

Cinco suítes completas em condições de carga diferentes. **O ranking é idêntico
nas cinco**; as magnitudes variam. Estes números são da v0.5.0 e **não foram
re-medidos na v0.6.0** — a rodada da v0.6.0 é uma só suíte de 9 execuções
intercaladas, então as faixas abaixo continuam sendo a melhor estimativa
disponível de dispersão, e devem ser lidas como tal:

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
rápido que o CPython no `fib` e ~7x no loop apertado. Na v0.6.0 o Noxy fica
**~7x mais lento que o Lua** (era 14x–15x) e **1,1x a 6,2x mais lento que o
CPython** (era 1,8x a 9,6x).

**O custo não está distribuído por igual — ele se concentra:**

- `loop_arith` é o piso: laço `while` com aritmética inteira e atribuição de
  local, sem chamada, sem índice, sem alocação. A **1,1x** do CPython, o
  despacho de bytecode puro do Noxy agora empata com o dele. Mas o mesmo bench
  segue a 7,7x do Lua — "empatar com o CPython" aqui continua sendo fraqueza do
  CPython em loop numérico, não força do Noxy.
- O gap ainda abre com o *tipo* de operação, mas a ordem mudou: hashmap 2,5x,
  ponto flutuante 2,7x, string 3,3x, **chamada de função 3,4x** — e o pior
  ponto passou a ser **acesso indexado a array (6,2x)**, que era o segundo. A
  fase 1 atacou globais, chamadas e despacho de laço; indexação de array não
  foi tocada, e por isso subiu no ranking.

**A pista mais acionável era `fib`** — só ints, nenhum tipo composto, então o
custo tinha de estar no protocolo de chamada em si. **Confirmado por profiling
e corrigido:** resolução de globais por nome sob mutex (~15% cum) e alocação de
`CallFrame` por chamada (1012 → 10 allocs/op) eram o grosso; `fib` caiu 55%.
O que sobrou no perfil pós-fase é `push`/`pop` da pilha de operandos, hoje
**24,4% do tempo de `fib`** — a maior fatia depois do despacho puro.

**A inversão estrutural, parcialmente resolvida.** O Noxy é estaticamente
tipado e compila para bytecode: os tipos são conhecidos em tempo de compilação,
que é exatamente a informação que CPython e Lua pagam para descobrir em runtime.
A v0.5.0 não convertia isso em velocidade; a hipótese registrada aqui era
**"falta camada de especialização"**, não "o loop principal é ruim".

**A hipótese se confirmou.** A fase 1 (v0.6.0) construiu a primeira camada:
chamada com modos provados estaticamente, resolução de globais cacheada,
comparação+salto e incremento de local fundidos quando ambos os lados são
estaticamente `int`, e aritmética `float` especializada. O gap contra o CPython
caiu de 1,8x–9,6x para 1,1x–6,2x, e contra o Lua pela metade. A camada está
longe de completa — indexação de array, campos de struct por índice e maps
tipados seguem sem especialização.

## Onde o Noxy ganha

- **Startup contra o CPython:** 49,6 ms contra 75,4 ms, ~1,5x mais rápido —
  real para script curto e para o caso Lambda, onde o piso de processo é boa
  parte do custo. A vantagem, porém, é sobre o CPython, não sobre todo mundo
  (Lua e Go sobem em ~38 ms).
- **Carga I/O-bound:** nos casos que o Noxy realmente atende hoje (servidor
  HTTP, SQLite, NoxyDB), o gargalo é I/O e a velocidade do interpretador quase
  não aparece.
- **Ordem de grandeza:** estar 1,1x a 6,2x atrás do CPython na v0.6.0 (era 2x
  a 10x na v0.5.0) é resultado respeitável em termos absolutos. A maioria das
  implementações jovens fica 50x a 100x atrás.

Sendo justo com o Lua: são 30 anos de tuning em C, VM baseada em registradores,
sem GC concorrente e sem bounds check do Go. Não é meta de curto prazo — é a
referência de onde dá para chegar. A distância caiu de ~15x para ~7x numa
única fase, o que sugere que ainda há ganho estrutural na mesa antes de a
comparação virar disputa de qualidade de codegen.

## Próximos passos sugeridos

1. Escalar as cargas até o trabalho dominar o piso de processo (ver limitação
   acima) e re-medir. **Ainda pendente** — e ficou mais urgente: com o Noxy
   mais rápido, o trabalho líquido de vários benches encolheu em relação ao
   piso, o que amplia a barra de erro dos ratios.
2. ~~`pprof` em `fib` e `bubblesort`~~ — **feito** na fase 1 (flags
   `--cpuprofile`/`--memprofile` na CLI; baseline e perfil pós-fase em
   [`../../docs/superpowers/specs/2026-08-17-vm-perf-static-typing-research.md`](../../docs/superpowers/specs/2026-08-17-vm-perf-static-typing-research.md)).
3. Portar os quatro benches restantes para Lua, fechando a calibração.
4. Próximo alvo indicado pelo perfil pós-fase: layout do `Value` e `pop` sem
   zerar escalares (`push`+`pop` = 24,4% do tempo de `fib`). Depois,
   indexação de array — que esta rodada promoveu a pior ponto da tabela.
5. Re-medir a estabilidade entre rodadas na v0.6.0 (as faixas registradas
   acima são da v0.5.0).
