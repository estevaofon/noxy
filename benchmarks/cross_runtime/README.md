# Cross-runtime: Noxy × CPython × Lua × Go

Comparação pontual de performance entre o VM do Noxy e outros runtimes, para
ter uma referência externa de onde o interpretador está hoje. Não é uma suíte
extensa: são seis cargas CPU-bound curtas, escolhidas para isolar subsistemas
diferentes do VM, mais um piso de processo.

## Como rodar

```powershell
./run_cross_runtime.ps1 -Noxy C:\caminho\local\noxy.exe

# comparando duas versões do próprio Noxy na mesma janela intercalada
./run_cross_runtime.ps1 -Noxy C:\local\noxy_novo.exe `
                        -NoxyBaseline C:\local\noxy_antigo.exe -BaselineLabel v060
```

Saída em `results/cross_runtime.md`. `lua` e `go` são opcionais — se não
estiverem no `PATH`, viram `-` na tabela em vez de erro.

`-NoxyBaseline` adiciona uma segunda versão do Noxy como coluna extra,
intercalada junto com os outros runtimes. É a **única forma válida de comparar
duas versões**: ver "Por que a coluna da versão antiga é medida junto" abaixo.

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

Quatro decisões, cada uma por um efeito medido neste repo:

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
4. **A versão antiga é medida junto, não citada de outra rodada.** Ver a seção
   seguinte — é o conserto da limitação que as versões anteriores deste
   documento carregavam.

### Por que a coluna da versão antiga é medida junto

Até a v0.6.0 este README comparava versões colocando lado a lado colunas de
**rodadas diferentes** (`v0.6.0` × `v0.5.0`). A rodada da v0.13.0 mostrou o
tamanho do erro que isso embute: com a máquina mais carregada, o piso de
processo do **Lua** saiu de 38 ms para 80 ms e o do **Go** de 38 ms para 83 ms
— runtimes que não mudaram uma linha entre as duas medições.

Como o número líquido é uma razão entre *diferenças*, um piso inflado encolhe o
líquido do denominador e infla a razão. Medindo a v0.6.0 **na mesma janela** da
v0.13.0 dá para separar as duas coisas:

| bench | ÷ python publicado na v0.6.0 | v0.6.0 re-medida hoje | veredito |
|---|---|---|---|
| `mandelbrot` | 2,7x | 2,70x | reproduz |
| `map_churn` | 2,5x | 2,43x | reproduz |
| `loop_arith` | 1,1x | 1,05x | reproduz |
| `fib` | 3,4x | 3,84x | inflado |
| `bubblesort` | 6,2x | 7,60x | inflado |
| `string_ops` | 3,3x | 4,72x | inflado |

Os três que reproduzem são exatamente aqueles em que o trabalho do CPython
domina o piso dele; os três que inflam são aqueles em que o líquido do CPython
é pequeno perto do piso (`string_ops`: ~40 ms de trabalho sob um piso de
~109 ms). Isso **confirma quantitativamente** a limitação que este documento
já declarava por suspeita — e é a razão de a comparação entre versões usar
agora a coluna intercalada, não a coluna histórica.

## Resultados

Noxy v0.13.0 (develop @ 63ab106) · **Noxy v0.6.0 (68209be) medido junto** ·
CPython 3.13.1 · Lua 5.4.7 · Go 1.24.11 · i7-1165G7 (4C/8T) · Windows 11 ·
**três suítes** de 9 execuções intercaladas cada, mínimo por suíte e mediana
entre as três. Números da última suíte em
[`results/cross_runtime.md`](results/cross_runtime.md).

Mesmas versões de CPython, Lua e Go da rodada da v0.6.0 — mas **a máquina
estava mais carregada** (Zoom, Chrome e Slack ativos), e por isso os ms
absolutos desta seção não se comparam com os da v0.6.0 publicados antes. É
justamente para isso que a v0.6.0 foi re-medida junto.

### Tempo total (ms)

| bench | noxy v0.13.0 | noxy v0.6.0 | python | lua | go |
|---|---|---|---|---|---|
| `startup` | **116,3** | 139,7 | 109,0 | 79,8 | 82,6 |
| `loop_arith` | 489,0 | **465,5** | 418,1 | 123,8 | 88,2 |
| `map_churn` | **370,9** | 390,1 | 212,2 | – | – |
| `mandelbrot` | **380,1** | 405,7 | 207,4 | – | – |
| `string_ops` | **305,6** | 329,8 | 149,3 | – | – |
| `bubblesort` | 809,0 | **759,9** | 190,6 | – | – |
| `fib` | **578,1** | 590,5 | 226,3 | 135,0 | 78,5 |

### Tempo de execução, descontado o piso de `startup` (ms)

| bench | noxy v0.13.0 | noxy v0.6.0 | python | lua | go | ÷ python | ÷ lua |
|---|---|---|---|---|---|---|---|
| `loop_arith` | 372,7 | 325,8 | 309,1 | 44,0 | ~0 | **1,2x** | **8,5x** |
| `map_churn` | 254,6 | 250,4 | 103,2 | – | – | **2,5x** | – |
| `mandelbrot` | 263,8 | 266,0 | 98,4 | – | – | **2,7x** | – |
| `fib` | 461,8 | 450,8 | 117,3 | 55,2 | ~0 | **3,9x** | **8,4x** |
| `string_ops` | 189,3 | 190,1 | 40,3 | – | – | **4,7x** | – |
| `bubblesort` | 692,7 | 620,2 | 81,6 | – | – | **8,5x** | – |

`~0` = o trabalho cabe dentro do ruído do piso de processo do runtime.

### v0.13.0 × v0.6.0 — sete versões, mesma janela

Mediana das razões das três suítes. Menor é melhor; `< 1` = a v0.13.0 ganhou.

| bench | tempo total | tempo líquido | leitura |
|---|---|---|---|
| `startup` | **0,85x** | – | −15%, único ganho grande e consistente (0,81 / 0,85 / 0,85) |
| `string_ops` | 0,93x | 1,02x | empate no líquido; o ganho total é o startup |
| `map_churn` | 0,93x | 0,98x | empate |
| `mandelbrot` | 0,94x | 0,98x | empate |
| `bubblesort` | 0,95x | 0,97x | bench mais ruidoso da suíte (0,94 / 0,95 / 1,15) |
| `fib` | 1,00x | 1,05x | empate |
| `loop_arith` | 1,03x | 1,13x | única regressão consistente nas três suítes |

**O trabalho do VM ficou estável entre a v0.6.0 e a v0.13.0.** As sete versões
no meio entregaram semântica (CoW por valor, invariante de slot `ref`,
genéricos monomorfizados, tipagem estática de membro qualificado, `io` com
cursor) e não velocidade de interpretação — o que é coerente: nenhuma delas
teve fase de perf, e várias adicionaram guards de RC/CoW no caminho quente.

**Cuidado: os 15% de startup medidos aqui são o piso do ganho real, não o
teto.** O `startup.nx` desta suíte é um `print` dentro de `main()`, sem nenhum
`use` — de propósito, para isolar o piso de processo. Mas o maior ganho das
sete versões está justamente na carga de módulos, que este bench não toca.
Medido com as sondas de `../startup_use_*.nx`, mesmo par de binários, mínimo de
15 intercaladas:

| sonda | v0.6.0 | v0.13.0 | delta |
|---|---|---|---|
| `use http select *` | 1873,2 ms | 190,5 ms | **−89,8%** (9,8x) |
| `use http` | 437,2 ms | 148,5 ms | **−66,0%** (2,9x) |
| sem `use` (este bench) | 154,9 ms | 130,0 ms | −16,1% |

Causa: memoização de `loadModuleDeclarations` (`19156a7`) — antes cada `use`
era recarregado por chamador. **Qualquer script que importe a stdlib ganhou de
2,9x a 9,8x no startup**, e essa é a melhoria mais consequente do intervalo
para o caso de uso real (script curto, Lambda). Que ela não apareça na tabela
acima é limitação da suíte, não ausência de ganho — ver
[`../RESULTS.md`](../RESULTS.md).

As duas colunas discordam de propósito. O **total** é o que o usuário sente e
inclui o startup, que melhorou ~15%. O **líquido** desconta o piso de cada
binário — e como a v0.13.0 tem piso menor, descontar o piso dela mesma joga o
líquido dela para cima. Onde as duas discordam (`string_ops`, `map_churn`,
`mandelbrot`), a diferença **é** o startup.

O detalhamento por subsistema, com os micro-benches de `../`, está na seção
"v0.6.0 × v0.13.0" de [`../RESULTS.md`](../RESULTS.md). Lá aparecem ganhos que
este cross-runtime não isola, porque nenhum bench daqui exercita só esse
caminho: chamada tipada com map (−15,3%) e chamada por valor com mutação
(−12,9%).

**Atenção a um conflito aparente:** o `bench_bubblesort.nx` de `../` marca
**+4,8%** (regressão) enquanto o `bubblesort.nx` daqui marca 0,95x (ganho). Não
é contradição — são programas diferentes, e o daqui é o bench mais ruidoso
desta suíte (0,94 / 0,95 / 1,15 nas três). O de `../` roda com mediana de 9 e
tem o sinal confirmado em duas sessões, então **é ele que vale** para o
veredito sobre indexação de array; o daqui serve para a comparação contra
CPython/Lua, não para deltas de versão de poucos por cento.

### Estabilidade entre rodadas (re-medida na v0.13.0)

Três suítes completas, mesma sessão. **O ranking é idêntico nas três.** Faixa
do `÷ python` sobre o líquido:

| bench | noxy ÷ python (faixa nas 3 suítes) | dispersão |
|---|---|---|
| `fib` | 3,7x – 3,8x | ±2% |
| `loop_arith` | 1,1x – 1,3x | ±7% |
| `map_churn` | 2,3x – 2,6x | ±5% |
| `mandelbrot` | 2,4x – 2,9x | ±9% |
| `string_ops` | 3,4x – 5,2x | ±21% |
| `bubblesort` | 6,6x – 9,4x | ±17% |

Contra o Lua: `loop_arith` 7,9x – 8,5x, `fib` 8,1x – 9,6x. No `startup`, o Noxy
ficou **1,06x – 1,14x atrás** do CPython nas três suítes — inversão em relação
à v0.6.0, que estava 1,3x – 1,5x à frente.

A inversão **não é regressão do Noxy**: contra a própria v0.6.0, medida na
mesma janela, a v0.13.0 sobe 15% no startup. O que aconteceu é que o piso de
processo do CPython é o menos sensível a carga de máquina de todos os quatro:

| runtime | piso publicado (v0.6.0) | piso hoje | inflação |
|---|---|---|---|
| CPython | 75,4 ms | 109,0 ms | **1,45x** |
| Lua | 38,2 ms | 79,8 ms | 2,09x |
| Go | 38,5 ms | 82,6 ms | 2,15x |
| Noxy v0.6.0 | 49,6 ms | 139,7 ms | 2,82x |

Faz sentido: o startup do CPython é dominado por trabalho dele mesmo (init do
interpretador, imports), enquanto o dos outros três é quase todo criação de
processo — exatamente o que disputa com a carga de fundo. A leitura correta é
que **a vantagem de startup do Noxy sobre o CPython só existe em máquina
ociosa**, e isso é novo: a rodada da v0.6.0 não tinha como saber.

**A limitação conhecida segue sem conserto, e agora está quantificada:** o
número líquido é razão entre *diferenças*, então amplifica qualquer variação no
piso de processo. Os dois benches com a maior dispersão (`string_ops` ±21%,
`bubblesort` ±17%) são exatamente os dois em que o trabalho do CPython é
pequeno perto do piso dele. O conserto continua sendo aumentar as contagens de
iteração até o trabalho de cada runtime ser ~5x o piso dele; ainda não foi
feito, e a comparação `v0.6.0 publicado × v0.6.0 re-medido` acima mostra que
ele vale até 1,4x de erro no ratio.

Trate o resultado como **ordem de grandeza com ranking confiável**, não como
número de três dígitos significativos.

## Leitura

**O CPython é uma régua enganosa.** Ele não é o piso da comparação — é o meio
da tabela. O Lua 5.4, com um VM mais simples e sem tipos estáticos, é ~2x mais
rápido que o CPython no `fib` e ~7x no loop apertado. Nesta rodada o Noxy fica
**~8,4x mais lento que o Lua** e **1,2x a 8,5x mais lento que o CPython**.

Esses números parecem piores que os da v0.6.0 (~7x do Lua, 1,1x–6,2x do
CPython) e **não são**: a v0.6.0 re-medida na mesma janela dá 1,05x–7,6x contra
o CPython. A diferença é a carga da máquina inflando o piso de processo, não
regressão do Noxy — ver "Por que a coluna da versão antiga é medida junto".

**O custo não está distribuído por igual — ele se concentra:**

- `loop_arith` é o piso: laço `while` com aritmética inteira e atribuição de
  local, sem chamada, sem índice, sem alocação. A **1,2x** do CPython, o
  despacho de bytecode puro do Noxy empata com o dele. Mas o mesmo bench segue
  a 8,5x do Lua — "empatar com o CPython" aqui continua sendo fraqueza do
  CPython em loop numérico, não força do Noxy.
- O gap abre com o *tipo* de operação, e o ranking não mudou desde a v0.6.0:
  hashmap 2,5x, ponto flutuante 2,7x, **chamada de função 3,9x**, string 4,7x,
  e o pior ponto segue sendo **acesso indexado a array (8,5x)**. A fase 1 de
  perf atacou globais, chamadas e despacho de laço; **indexação de array nunca
  foi tocada**, e continua sendo o alvo óbvio.

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
caiu de 1,8x–9,6x para 1,1x–6,2x, e contra o Lua pela metade.

**E parou aí.** Da v0.7.0 à v0.13.0 não houve fase 2: a camada de
especialização segue no mesmo ponto em que a fase 1 a deixou — indexação de
array, campos de struct por índice e maps tipados continuam sem especialização.
O que essas sete versões fizeram foi *semântica* (CoW por valor, invariante de
slot `ref`, genéricos, tipagem de membro qualificado), parte dela adicionando
guards no caminho quente. O saldo no VM é empate, com `loop_arith` ~3% mais
lento: **as features foram absorvidas quase de graça, mas o interpretador não
ficou mais rápido.**

A velocidade que apareceu veio do **compilador**, não do VM — carga de módulos
2,9x a 9,8x mais rápida (seção de estabilidade). São eixos independentes: um
melhora o custo fixo por processo, o outro o custo por instrução. O próximo
ganho no eixo do VM tem de vir de uma fase 2 explícita, não de arrasto.

## Onde o Noxy ganha

- **Startup de programa que importa a stdlib:** é aqui que está o maior ganho
  das sete versões — `use http select *` caiu de 1873 ms para 190 ms (**9,8x**)
  com a memoização de carga de módulos. Para o caso Lambda e para script curto,
  essa é a melhoria que mais importa, e nenhum bench desta suíte a mede.
- **Startup puro (sem `use`), com uma ressalva nova:** contra a própria v0.6.0
  (139,7 ms → 116,3 ms) a v0.13.0 melhorou 15%. Contra o CPython, porém, o Noxy
  ficou 1,06x–1,14x *atrás* nesta rodada, em vez dos 1,3x–1,5x à frente da
  v0.6.0. Motivo na seção de estabilidade: o piso do CPython é o menos sensível
  a carga dos quatro runtimes. A vantagem de piso de processo sobre o CPython
  **só existe em máquina ociosa** — condição que não estava documentada antes.
- **Carga I/O-bound:** nos casos que o Noxy realmente atende hoje (servidor
  HTTP, SQLite, NoxyDB), o gargalo é I/O e a velocidade do interpretador quase
  não aparece.
- **Ordem de grandeza:** estar 1,2x a 8,5x atrás do CPython é resultado
  respeitável em termos absolutos. A maioria das implementações jovens fica 50x
  a 100x atrás.

Sendo justo com o Lua: são 30 anos de tuning em C, VM baseada em registradores,
sem GC concorrente e sem bounds check do Go. Não é meta de curto prazo — é a
referência de onde dá para chegar. A distância caiu de ~15x para ~7x numa
única fase e ficou parada aí por sete versões, o que sugere que o ganho
estrutural restante ainda está na mesa, esperando uma fase 2.

## Próximos passos sugeridos

1. **Escalar as cargas até o trabalho dominar o piso de processo** e re-medir.
   Continua sendo o item nº 1 e agora tem número: a comparação
   `v0.6.0 publicado × v0.6.0 re-medido` mostrou até **1,4x de erro** no ratio
   só por carga de máquina, e a dispersão entre suítes chega a ±21%
   (`string_ops`). Alvo: trabalho ≥ ~5x o piso de cada runtime.
2. ~~`pprof` em `fib` e `bubblesort`~~ — **feito** na fase 1 (flags
   `--cpuprofile`/`--memprofile` na CLI; baseline e perfil pós-fase em
   [`../../docs/superpowers/specs/2026-08-17-vm-perf-static-typing-research.md`](../../docs/superpowers/specs/2026-08-17-vm-perf-static-typing-research.md)).
3. Portar os quatro benches restantes para Lua, fechando a calibração.
4. **Fase 2 de perf, na ordem que a tabela indica:** indexação de array
   (8,5x do CPython, pior ponto e nunca tocada), depois `push`/`pop` da pilha
   de operandos (24,4% do tempo de `fib` no perfil pós-fase 1), depois campos
   de struct por índice e maps tipados.
5. ~~Re-medir a estabilidade entre rodadas~~ — **feito** nesta rodada (três
   suítes, faixas acima).
6. **Investigar as duas regressões dos micro-benches de `../`** entre a v0.6.0
   e a v0.13.0: `bench_call_readonly.nx` (+4,2%) e `bench_bubblesort.nx`
   (+4,8%), ambas leitura O(n) de array e ambas com sinal confirmado em duas
   sessões — a suspeita é guard de `Shared`/RC por acesso no caminho de
   leitura. Detalhe em [`../RESULTS.md`](../RESULTS.md).
