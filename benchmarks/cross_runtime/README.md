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

### Fase 2 de perf (layout do `Value`, issue #37) × v0.14.2 — 2026-08-22

Rodada de uma suíte (mínimo de 9, intercalado, máquina sem outra carga) com
o binário da fase 2 (`perf/issue-37-value-layout` @ ba7f85d: `Value` 48 → 32 B,
header comum nos compostos, `pop` inlinada) e **v0.14.2 medido junto**.
Números completos em [`results/cross_runtime.md`](results/cross_runtime.md)
e o contexto por estágio em
[`../results/2026-08-22-issue-37-value-layout-raw.md`](../results/2026-08-22-issue-37-value-layout-raw.md)
/ [`../RESULTS.md`](../RESULTS.md).

| bench | fase 2 (líquido) | v0.14.2 (líquido) | fase 2 ÷ v0.14.2 | ÷ python | ÷ lua |
|---|---|---|---|---|---|
| `loop_arith` | 272,5 | 327,6 | 0,83x | **1,1x** | 6,5x |
| `mandelbrot` | 176,9 | 219,1 | 0,81x | **1,9x** | – |
| `map_churn` | 173,7 | 226,6 | 0,77x | **2,1x** | – |
| `fib` | 296,0 | 420,7 | **0,70x** | **2,9x** | 5,6x |
| `string_ops` | 134,5 | 145,8 | 0,92x | **4,2x** | – |
| `bubblesort` | 423,1 | 625,4 | **0,68x** | **5,5x** | – |

O quadro da seção seguinte (v0.14.1 × v0.6.0) continua valendo como
histórico; a partir daqui, a referência é esta rodada. A ordem dos piores
pontos não muda — `bubblesort` (indexação) e `string_ops` seguem os mais
distantes do CPython — mas todos encurtam.

Noxy v0.14.1 (develop @ 4874048) · **Noxy v0.6.0 (68209be) medido junto** ·
CPython 3.13.1 · Lua 5.4.7 · Go 1.24.11 · i7-1165G7 (4C/8T) · Windows 11 ·
**três suítes** de 9 execuções intercaladas cada, mínimo por suíte e mediana
entre as três. Números da última suíte em
[`results/cross_runtime.md`](results/cross_runtime.md); as três em
[`results/2026-08-22-v0141-3suites.md`](results/2026-08-22-v0141-3suites.md).

Esta é a **re-medição com a máquina ociosa** da rodada de 2026-08-21 (que
tinha Zoom, Chrome e Slack ativos): mesmo protocolo, mesmas versões de
CPython/Lua/Go, mesmo binário v0.6.0 byte a byte; o candidato subiu de
v0.13.0 para v0.14.1, que não muda o VM (sanidade intercalada em
[`../results/2026-08-22-v0141-idle-raw.md`](../results/2026-08-22-v0141-idle-raw.md)).
Condição: CPU total ~4% antes de começar, nenhum processo de fundo acima de
~7% de um core, na tomada. Os dados da rodada carregada ficam em
[`results/2026-08-21-v0130-3suites.md`](results/2026-08-21-v0130-3suites.md)
como registro; onde as duas discordam, **vale esta**.

### Tempo total (ms)

| bench | noxy v0.14.1 | noxy v0.6.0 | python | lua | go |
|---|---|---|---|---|---|
| `startup` | 84,8 | **81,1** | 86,0 | 61,2 | 64,6 |
| `loop_arith` | **385,3** | 390,6 | 343,3 | 99,5 | 72,0 |
| `map_churn` | 293,5 | **289,2** | 161,9 | – | – |
| `mandelbrot` | 305,2 | **298,5** | 168,2 | – | – |
| `string_ops` | **237,4** | 241,4 | 124,7 | – | – |
| `bubblesort` | 664,2 | **608,0** | 165,3 | – | – |
| `fib` | 481,6 | **447,9** | 199,2 | 114,8 | 67,3 |

### Tempo de execução, descontado o piso de `startup` (ms)

Colunas de ms: mediana das três suítes. Colunas `÷`: mediana das razões
calculadas dentro de cada suíte (é a razão, não os ms, que se cita fora daqui).

| bench | noxy v0.14.1 | noxy v0.6.0 | python | lua | go | ÷ python | ÷ lua |
|---|---|---|---|---|---|---|---|
| `loop_arith` | 300,5 | 312,4 | 257,3 | 39,0 | 8,7 | **1,2x** | **7,7x** |
| `mandelbrot` | 220,4 | 215,7 | 82,2 | – | – | **2,7x** | – |
| `map_churn` | 207,3 | 211,0 | 75,9 | – | – | **2,8x** | – |
| `fib` | 396,8 | 369,7 | 113,2 | 53,8 | ~0 | **3,5x** | **7,4x** |
| `string_ops` | 148,8 | 160,3 | 41,1 | – | – | **3,6x** | – |
| `bubblesort` | 575,5 | 529,8 | 78,4 | – | – | **7,3x** | – |

`~0` = o trabalho cabe dentro do ruído do piso de processo do runtime.

### v0.14.1 × v0.6.0 — oito versões, mesma janela

Mediana das razões das três suítes. Menor é melhor; `< 1` = a v0.14.1 ganhou.

| bench | tempo total | tempo líquido | leitura |
|---|---|---|---|
| `startup` | 1,07x | – | +3 a +7 ms nas três suítes; é o mesmo deslocamento que um build fresco do próprio 63ab106 mostra contra o binário da rodada anterior (ver `../RESULTS.md`) — o piso de código empata |
| `loop_arith` | 0,99x | 0,96x | empate (0,95 / 0,96 / 1,03) — o 1,13x da rodada carregada não reproduz |
| `map_churn` | 1,02x | 0,99x | empate |
| `mandelbrot` | 1,01x | 0,99x | empate |
| `string_ops` | 0,97x | 0,93x | empate com viés a favor, ruidoso (0,89 / 0,93 / 1,01) |
| `fib` | 1,07x | **1,07x** | regressão consistente nas três (1,05 / 1,07 / 1,08) |
| `bubblesort` | 1,09x | **1,10x** | regressão consistente nas três (1,06 / 1,10 / 1,10) |

**O VM ficou 5–10% mais lento em dois caminhos e empatou no resto.** Chamada
de função (`fib`) e indexação de array (`bubblesort`) regrediram nas três
suítes, sem exceção; despacho puro, hashmap, float e string empataram. É o
mesmo quadro que os micro-benches de `../` dão na mesma data
(`bench_call_readonly` +10% em duas sessões, `bench_call_ref` +2–3%), e é
coerente com a história do intervalo: oito versões de semântica (CoW por
valor, invariante de slot `ref`, genéricos, tipagem de membro qualificado,
`io` com cursor, `range`), várias delas adicionando guards de RC/CoW no
caminho de leitura e de chamada, e nenhuma fase de perf.

A rodada carregada de 2026-08-21 lia esse mesmo par como "empate, com startup
−15% e `loop_arith` +13%". As duas afirmações eram artefato de carga: com a
máquina ociosa o piso dos dois binários é o mesmo e `loop_arith` fica em
0,96x; em compensação as regressões de `fib` e `bubblesort`, que lá se
perdiam no ruído (0,99–1,16 e 0,94–1,22), aparecem limpas.

Os dois `bubblesort` discordam de novo, agora no outro sentido: o daqui marca
1,06x–1,10x nas três suítes, enquanto o `bench_bubblesort.nx` de `../`
(mediana de 9) marca +0,1% e +3,9%. São programas diferentes; o sinal limpo
de leitura indexada em `../` é `bench_call_readonly` (+10%), que concorda com
o daqui.

**O maior ganho do intervalo continua fora desta tabela: carga de módulos.**
O `startup.nx` desta suíte não tem `use`, de propósito. Medido com as sondas
de `../startup_use_*.nx`, mesmo par de binários, mínimo de 15 intercaladas,
fontes em disco local:

| sonda | v0.6.0 | v0.14.1 | delta | (2026-08-21, sob carga) |
|---|---|---|---|---|
| `use http select *` | 1370,2 ms | 135,5 ms | **−90,1%** (10,1x) | −89,8% (9,8x) |
| `use http` | 346,2 ms | 129,6 ms | **−62,6%** (2,7x) | −66,0% (2,9x) |
| sem `use` (este bench) | 83,3 ms | 88,1 ms | +5,8% | −16,1% |

Causa: memoização de `loadModuleDeclarations` (`19156a7`) — antes cada `use`
era recarregado por chamador. **Qualquer script que importe a stdlib ganhou
de 2,7x a 10,1x no startup**: reproduz dentro de 1–3 pontos da rodada
carregada, e é a melhoria mais consequente do intervalo para o caso de uso
real (script curto, Lambda). O −16% no piso puro, esse não reproduz — era
carga. Detalhe em [`../RESULTS.md`](../RESULTS.md).

### Estabilidade entre rodadas (re-medida na v0.14.1, máquina ociosa)

Três suítes completas, mesma sessão. **O ranking é idêntico nas três.** Faixa
do `÷ python` sobre o líquido:

| bench | noxy ÷ python (faixa nas 3 suítes) | dispersão | (2026-08-21, sob carga) |
|---|---|---|---|
| `loop_arith` | 1,2x – 1,2x | ±3% | ±7% |
| `fib` | 3,2x – 3,5x | ±4% | ±2% |
| `map_churn` | 2,5x – 2,8x | ±7% | ±5% |
| `mandelbrot` | 2,4x – 2,8x | ±9% | ±9% |
| `bubblesort` | 6,5x – 7,5x | ±7% | ±17% |
| `string_ops` | 3,2x – 4,5x | ±17% | ±21% |

Contra o Lua: `loop_arith` 7,7x – 7,9x, `fib` 6,9x – 7,4x. A máquina ociosa
apertou a dispersão onde ela era pior (`bubblesort` ±17% → ±7%) e deixou
`string_ops` como o bench ruidoso da suíte — é o de menor trabalho líquido do
CPython (~41 ms sobre um piso de ~86 ms), exatamente o caso que a limitação
conhecida (razão entre *diferenças*) amplifica.

**O piso de processo mudou de novo, e não por código.** Contra a rodada da
v0.6.0 (2026-08-18, também sem carga) e a carregada (2026-08-21):

| runtime | 2026-08-18 | 2026-08-21 (carga) | 2026-08-22 (ociosa) |
|---|---|---|---|
| CPython | 75,4 ms | 109,0 ms | 86,0 ms |
| Lua | 38,2 ms | 79,8 ms | 61,2 ms |
| Go | 38,5 ms | 82,6 ms | 64,6 ms |
| Noxy v0.6.0 | 49,6 ms | 139,7 ms | 81,1 ms |
| Noxy v0.13.0 / v0.14.1 | – | 116,3 ms | 84,8 ms |

Lua e Go não mudaram uma linha e estão 1,6x acima de 2026-08-18 — algo na
máquina encareceu criar processo entre as duas datas (hipótese: o
EDR/antivírus instalado nesse intervalo; não verificado aqui). O CPython
subiu só 1,14x, porque o piso dele é dominado por trabalho próprio (init do
interpretador, imports), não por criação de processo. Consequência: no
`startup`, o Noxy **empata com o CPython** hoje (0,99x–1,02x; a v0.6.0 fica
0,91x–0,97x), em vez dos 1,3x–1,5x à frente de 2026-08-18 — e isso não é
regressão do Noxy (v0.6.0 e v0.14.1 têm o mesmo piso), é a régua que mudou.
Terceira confirmação de que ms absolutos não se comparam entre datas, nem com
a máquina ociosa nas duas.

**A limitação conhecida segue sem conserto:** o número líquido é razão entre
*diferenças*, então amplifica qualquer variação no piso de processo. O
conserto continua sendo aumentar as contagens de iteração até o trabalho de
cada runtime ser ~5x o piso dele; ainda não foi feito.

Trate o resultado como **ordem de grandeza com ranking confiável**, não como
número de três dígitos significativos.

## Leitura

**O CPython é uma régua enganosa.** Ele não é o piso da comparação — é o meio
da tabela. O Lua 5.4, com um VM mais simples e sem tipos estáticos, é ~2x mais
rápido que o CPython no `fib` e ~7x no loop apertado. Nesta rodada o Noxy fica
**~7,5x mais lento que o Lua** e **1,2x a 7,3x mais lento que o CPython**.

Esses números são um pouco piores que os da v0.6.0 (~7x do Lua, 1,1x–6,2x do
CPython), e desta vez **parte é real**: a v0.6.0 re-medida na mesma janela dá
1,2x–6,7x contra o CPython, ou seja, 5–10% da distância em `fib` e
`bubblesort` é regressão do Noxy; o resto é a régua (piso de processo) que
mudou entre as datas — ver "Por que a coluna da versão antiga é medida junto"
e a tabela de pisos na seção de estabilidade.

**O custo não está distribuído por igual — ele se concentra:**

- `loop_arith` é o piso: laço `while` com aritmética inteira e atribuição de
  local, sem chamada, sem índice, sem alocação. A **1,2x** do CPython, o
  despacho de bytecode puro do Noxy empata com o dele. Mas o mesmo bench segue
  a 7,8x do Lua — "empatar com o CPython" aqui continua sendo fraqueza do
  CPython em loop numérico, não força do Noxy.
- O gap abre com o *tipo* de operação, e o ranking não mudou desde a v0.6.0:
  ponto flutuante 2,7x, hashmap 2,8x, **chamada de função 3,5x**, string 3,6x,
  e o pior ponto segue sendo **acesso indexado a array (7,3x)**. A fase 1 de
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

**E parou aí.** Da v0.7.0 à v0.14.1 não houve fase 2: a camada de
especialização segue no mesmo ponto em que a fase 1 a deixou — indexação de
array, campos de struct por índice e maps tipados continuam sem especialização.
O que essas oito versões fizeram foi *semântica* (CoW por valor, invariante de
slot `ref`, genéricos, tipagem de membro qualificado, `io` com cursor, `range`),
parte dela adicionando guards no caminho quente. O saldo no VM, medido com a
máquina ociosa, é 5–10% de regressão em chamada (`fib`) e indexação de array
(`bubblesort`) e empate no resto: **as features custaram pouco, mas custaram —
e o interpretador não ficou mais rápido.**

A velocidade que apareceu veio do **compilador**, não do VM — carga de módulos
2,7x a 10,1x mais rápida (seção "v0.14.1 × v0.6.0"). São eixos independentes: um
melhora o custo fixo por processo, o outro o custo por instrução. O próximo
ganho no eixo do VM tem de vir de uma fase 2 explícita, não de arrasto.

## Onde o Noxy ganha

- **Startup de programa que importa a stdlib:** é aqui que está o maior ganho
  das oito versões — `use http select *` caiu de 1370 ms para 136 ms (**10,1x**)
  com a memoização de carga de módulos. Para o caso Lambda e para script curto,
  essa é a melhoria que mais importa, e nenhum bench desta suíte a mede.
- **Startup puro (sem `use`): empate com o CPython, não mais vantagem.** v0.6.0
  e v0.14.1 têm o mesmo piso (~81–85 ms hoje; o −15% da rodada carregada era
  artefato). Contra o CPython, o Noxy fica em 0,99x–1,02x com a máquina
  ociosa, em vez dos 1,3x–1,5x à frente de 2026-08-18. O que mudou foi o custo
  de criar processo nesta máquina (Lua e Go subiram 1,6x no mesmo intervalo),
  não o Noxy — ver a tabela de pisos na seção de estabilidade. A vantagem de
  piso sobre o CPython depende da máquina e não deve ser citada como
  propriedade do runtime.
- **Carga I/O-bound:** nos casos que o Noxy realmente atende hoje (servidor
  HTTP, SQLite, NoxyDB), o gargalo é I/O e a velocidade do interpretador quase
  não aparece.
- **Ordem de grandeza:** estar 1,2x a 7,3x atrás do CPython é resultado
  respeitável em termos absolutos. A maioria das implementações jovens fica 50x
  a 100x atrás.

Sendo justo com o Lua: são 30 anos de tuning em C, VM baseada em registradores,
sem GC concorrente e sem bounds check do Go. Não é meta de curto prazo — é a
referência de onde dá para chegar. A distância caiu de ~15x para ~7x numa
única fase e ficou parada aí por oito versões (hoje 6,9x–7,9x), o que sugere
que o ganho estrutural restante ainda está na mesa, esperando uma fase 2.

## Próximos passos sugeridos

1. **Escalar as cargas até o trabalho dominar o piso de processo** e re-medir.
   Continua sendo o item nº 1 e agora tem número: a comparação
   `v0.6.0 publicado × v0.6.0 re-medido` mostrou até **1,4x de erro** no ratio
   só por carga de máquina, e a dispersão entre suítes chega a ±17%
   (`string_ops`) mesmo com a máquina ociosa. Alvo: trabalho ≥ ~5x o piso de
   cada runtime.
2. ~~`pprof` em `fib` e `bubblesort`~~ — **feito** na fase 1 (flags
   `--cpuprofile`/`--memprofile` na CLI; baseline e perfil pós-fase em
   [`../../docs/superpowers/specs/2026-08-17-vm-perf-static-typing-research.md`](../../docs/superpowers/specs/2026-08-17-vm-perf-static-typing-research.md)).
3. Portar os quatro benches restantes para Lua, fechando a calibração.
4. **Fase 2 de perf, na ordem que a tabela indica:** indexação de array
   (8,5x do CPython, pior ponto e nunca tocada), depois `push`/`pop` da pilha
   de operandos (24,4% do tempo de `fib` no perfil pós-fase 1), depois campos
   de struct por índice e maps tipados.
5. ~~Re-medir a estabilidade entre rodadas~~ — **feito** duas vezes (três
   suítes sob carga em 2026-08-21, três com a máquina ociosa em 2026-08-22;
   faixas acima).
6. **Investigar a regressão de 5–10% em leitura indexada e chamada** entre a
   v0.6.0 e a v0.14.1, agora com sinal limpo nas duas suítes:
   `bench_call_readonly.nx` +10% (duas sessões), e aqui `bubblesort.nx`
   1,06x–1,10x e `fib.nx` 1,05x–1,08x nas três suítes. A suspeita é guard de
   `Shared`/RC por acesso no caminho de leitura e de chamada. Detalhe em
   [`../RESULTS.md`](../RESULTS.md).
