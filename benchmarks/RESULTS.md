# Benchmarks

Registro corrido das comparações de performance, mais recente primeiro. Cada
seção compara dois binários pelo protocolo intercalado (ver Reprodução no fim).

## develop (fac7542) × RC-uniqueness fase 1 (perf/cow-uniqueness-rc)

**Data:** 2026-08-17 · Windows 11 · medição intercalada final, mediana de 9
execuções (repetida em Runs=5 e Runs=9 para checar ruído — ver nota ² abaixo).
Spec: `docs/superpowers/specs/2026-08-17-cow-rc-uniqueness-design.md`.

**Corpus de exemplos 130/130 idêntico** em todas as verificações — rodado
após o flip do mecanismo (bit sticky `Shared` → contador `Owners`), após
cada round de correção relevante e após a limpeza final do bit morto.
`go test ./...` verde; `-race` verde em `internal/value` e na suíte completa
de `internal/vm` (`go test ./internal/value -race`: 1,9s; `go test
./internal/vm -race`, sem filtro: 150,3s — o contador `Owners` é atômico
justamente pelo requisito de tasks paralelas).

| bench | perfil | develop_ms | rc_ms | delta | veredito |
|---|---|---|---|---|---|
| bench_value_call_mutate¹ | helper por valor em laço de mutação, map crescendo | 1558.5 | 101.6 | **−93,5%** | ✅ alvo |
| bench_typed_call_map | chamada tipada in-module, map 2,5k via ref | 110.7 | 109.9 | −0,7% | ✅ gate |
| bench_share_mutate | compartilha e muta em loop | 322 | 332 | +3,1% | ✅ gate |
| bench_conway | grid mutado via ref, 60 gerações | 3734.1 | 3861.5 | +3,4% | ✅ |
| bench_bubblesort | sort in-place via ref | 6045.6 | 6285.7 | +4% | ✅ |
| bench_call_ref | mutação in-place via ref | 6139.3 | 6447.3 | +5% | ✅ |
| bench_call_light | chamada O(1), array 10k por valor | 100.3 | 105.4 | +5,1% | ✅ |
| bench_call_readonly | leitor puro O(n), array 20k | 2362.8 | 2519.8 | +6,6% | ⚠️ ruído² |
| bench_path_update | `a[i].x = v` em loop, dono único | 880.4 | 943.2 | +7,1% | ⚠️ ruído² |
| bench_spawn_sum | soma paralela via spawn_task | 1325.9 | 1463.5 | **+10,4%** | ❌ acima do gate |
| bench_map_churn | escrita intensa em map | 601.8 | 667.5 | **+10,9%** | ❌ acima do gate |

¹ Benchmark novo, adicionado nesta fase: ancora o padrão emblemático (NoxyDB
`database_file(db)` por valor dentro do laço de puts) que motivou a spec —
o mesmo formato do `bench_typed_call_map`, mas sem `ref` no helper.

² `bubblesort` já tinha variância documentada de até ±10% entre sessões (ver
seção baseline × CoW abaixo); `call_readonly` e `path_update` oscilaram
dentro de ~1-4 pontos percentuais entre as duas rodadas (Runs=5 e Runs=9)
desta seção, consistente com o mesmo ruído de máquina relatado nos rounds de
correção da fase 1 — não são benches rastreados pelo gate (ver Interpretação).

### Perfil de cada bench

- **`bench_value_call_mutate`** — o alvo da fase 1. `struct State{payloads:
  map[...]}`, `struct Db{state: State}`; um `helper(db: Db) -> int` recebido
  **por valor** é chamado a cada volta de um laço de 2500 `put`s que também
  escreve em `db.state.payloads` via `ref db`. Sob o bit sticky, o bind por
  valor do helper marcava `db` (e, por clone raso, `state` e `payloads`)
  como compartilhados para sempre: cada `put` reclonava os três — 3 clones
  por put, O(N²) porque o clone do map cresce com N. Sob RC, o retain do
  parâmetro do helper morre no fim do frame dele; os donos voltam a 1 e as
  escritas seguintes mutam no lugar.
- **`bench_typed_call_map`** — espelho do bench acima, mas o helper recebe
  `ref` em vez de valor: já não tinha o custo de compartilhamento morto
  (nem no bit sticky, nem no RC) — fica dentro do gate como esperado.
- **`bench_share_mutate`** — pior caso do CoW por construção
  (`let b = a` seguido de mutação): +3,1%, o custo adicional é só o inc/dec
  do bind do `let`, que já é O(1) por vínculo.
- **`bench_conway`/`bench_bubblesort`/`bench_call_ref`** — mutação in-place
  via `ref` sobre grid/array grandes: pagam só o branch de checagem de
  unicidade (agora contador em vez de bit) por escrita — dentro do gate.
- **`bench_call_light`** — chamada O(1) com array de 10k por valor: paga o
  inc/dec de um único bind por chamada — dentro do gate.
- **`bench_call_readonly`/`bench_path_update`** — leitor O(n) e mutação de
  dono único em loop: acima de 5% nesta medição, mas variam mais que os
  quatro benches efetivamente rastreados pelo gate ao longo dos rounds de
  correção da fase 1 (ver nota ² acima) — tratados como ruído, não regressão
  do RC.
- **`bench_map_churn`/`bench_spawn_sum`** — os dois que **seguem acima do
  gate** mesmo após a limpeza do bit sticky morto (ver Interpretação).

### Interpretação

**Onde o RC ganha:** o compartilhamento morto — clone pago por um alias que
já não existe no momento da mutação — deixa de existir por construção.
`bench_value_call_mutate` vai de curva quadrática a flat (~1,5s → ~100ms a
N=2500): o padrão do NoxyDB (helper de validação chamado por valor dentro do
laço de puts) deixa de custar 3 clones por iteração e passa a custar O(1)
clones no laço inteiro.

**Onde fica neutro (dentro do gate ≤~5%):** os quatro benches rastreados ao
longo dos rounds de correção da fase 1 — `bench_share_mutate` (+3,1%) e
`bench_typed_call_map` (−0,7%) fecham dentro do gate; `bench_conway`,
`bench_bubblesort`, `bench_call_ref` e `bench_call_light` também ficam
dentro ou na borda dele. `bench_call_readonly` (+6,6%) e `bench_path_update`
(+7,1%) passam de 5% nesta medição, mas com variância maior que os benches
gate ao longo da fase — não foram tratados como regressão formal pelo
controller.

**Onde paga, sem maquiagem:** `bench_map_churn` (+10,9%) e `bench_spawn_sum`
(+10,4%) **seguem acima do gate de ~5% mesmo depois da limpeza do bit sticky
morto** (Task 8, commit `ae45d8f`) — não é a marcação morta que sobrava, é o
bookkeeping de RC em si: `map_churn` faz muitas inserções/remoções de chave
(inc/dec por elemento a cada operação) e `spawn_sum` faz handoff de
argumentos primitivos para várias goroutines (retain/release pareado no
preparo da task). Comparado ao round 4 da Task 7 (map_churn +9,8%, spawn_sum
+14,9%, já com o bookkeeping completo mas antes da limpeza do bit sticky), a
remoção do bit reduziu `spawn_sum` (14,9%→10,4%) e manteve `map_churn` na
mesma faixa (9,8%→10,9%, dentro do ruído) — a limpeza **não** fecha esses
dois deltas. **Aceito e documentado como o preço do RC nesta fase**; as
válvulas para quando isso for revisitado: drops precisos da fase 2
(Perceus-lite, §5) e elisão de pares inc/dec no mesmo bytecode (ambas
nomeadas na spec §8, risco 3), mais um fast path para stores de valores
escalares apontado na investigação da fase 1 (Task 7), fora do texto da
spec.

**O que resta:** fase 1 libera locais de bloco mortos só no fim do frame
(inflação temporária segura, nunca unsound); a fase 1.5 (drops de escopo de
bloco emitidos pelo compilador) e a fase 2 (drops de último uso,
Perceus-lite) atacam o resíduo de compartilhamento morto que sobrevive
dentro de um frame (`do let b = a end; a[i] = x` em laço) — spec §5. A
estrutura persistente (HAMT) para snapshots genuinamente vivos continua fora
de escopo (spec §9): um programa que de fato consome N cópias paga
O(rodadas×n) em qualquer linguagem de semântica de valor.

### Divergência corrigida

Escrita através de `ref` para um nó com exatamente um dono durável agora
acontece in-place e é visível. O teste committado
(`TestRefWriteToUniquelyOwnedNodeMutatesInPlace`,
`internal/vm/rc_uniqueness_test.go`) pina o valor correto para o programa
(lista encadeada, escrita via `setit(ref n, v)` seguida de escrita via
`let u: ref Node = ...; u.valor = 77`): **107** com a unicidade por contagem
de donos, contra **50** no binário pré-chave (bit sticky ligado pelo bind
por valor intermediário, mutação seguinte via `ref` clonava em vez de
mutar — escrita perdida).

A investigação da Task 7 (repros à mão, não parte da suíte) confirmou
adicionalmente que o comportamento antigo era dependente da forma do
vínculo, não um bug de contagem isolado: o próprio merge-base já respondia
107 quando o mesmo alias era escrito só via parâmetro `ref` (forma
canônica, sem a passagem por valor intermediária que liga o bit sticky) —
variante `rv_h1_paramform`, registrada em
`.superpowers/sdd/2026-08-17-cow-rc-uniqueness-fase1/task-7-report.md`, não
na suíte de testes. O 107 é o valor correto pelo contrato CoW 0.4.0 em
qualquer forma (§2, regra 6: mutação através de `ref` é sempre visível).
Nenhum exemplo do corpus muda (130/130) — é dead-share, não um caso
exercitado pelos exemplos existentes.

## develop (c0a89c9) × validação O(1) pela tag `RuntimeType` (PR #31)

**Data:** 2026-08-16/17 · Windows 11 · medições intercaladas (mediana de 5),
mesma máquina e protocolo da seção CoW abaixo.

**Checksums idênticos em todos os benchmarks; corpus de exemplos 130/130 com
saídas iguais** — as duas versões computam os mesmos resultados.

| bench | perfil | develop_ms | o1_ms | delta | veredito |
|---|---|---|---|---|---|
| bench_call_light | chamada O(1), array 10k por valor | 4269 | 122 | **−97,1%** | ✅ |
| bench_typed_call_map¹ | chamada tipada in-module, map 2,5k via ref | 2156 | 123 | **−94,3%** | ✅ |
| bench_share_mutate | compartilha e muta em loop | 1105 | 379 | **−65,7%** | ✅ |
| bench_call_readonly | leitor puro O(n), array 20k | 2815 | 2529 | −10,2% | ✅ |
| bench_call_ref | mutação in-place via ref | 7287 | 6898 | −5,3% | ✅ |
| bench_map_churn | escrita intensa em map | 692 | 665 | −3,9% | ✅ |
| bench_bubblesort | sort in-place via ref | 7258 | 6988 | −3,7% | ✅ |
| bench_conway | grid mutado via ref, 60 gerações | 4374 | 4270 | −2,4% | ✅ |
| bench_path_update | `a[i].x = v` em loop, dono único | 977 | 960 | −1,8% | ✅ |
| bench_spawn_sum | soma paralela via spawn_task | 1395 | 1411 | +1,1% | ✅ ruído |

¹ Benchmark novo, adicionado junto com a mudança: ancora o padrão que motivou
o trabalho (função tipada do mesmo módulo recebendo `ref` para struct com map
grande, chamada em laço de mutação — o formato do put/get do NoxyDB).

### Interpretação

A validação de tipos em runtime varria o contêiner inteiro do argumento a
cada chamada com assinatura estaticamente conhecida — duas vezes (prova +
aplicação), inclusive através de `ref` e em funções que só leem. Isso tornava
O(N²) qualquer laço quente que passasse um map/array grande para função tipada
do mesmo módulo, e era o custo que mascarava o ganho do `ref` no NoxyDB: com
o CoW corrigido, o laço de puts continuava quadrático só pela varredura. Com
a tag `RuntimeType` aceita valendo como prova em O(1), o padrão vira linear —
no repro do NoxyDB (N=4000 puts), de 6.715ms para 157ms.

Onde o ganho aparece: quanto maior o contêiner e mais quente o laço de
chamadas tipadas, maior o corte (`call_light` −97%, `typed_call_map` −94%).
`share_mutate`, o pior caso documentado do CoW, caiu 66% porque também pagava
varredura de validação por cima do clone. O resto da suite fica entre o ruído
e ganhos de poucos por cento — não há caso de regressão.

A varredura completa continua existindo onde é necessária: na primeira
marcação de um contêiner ainda sem tag (é ela que grava a tag) e em
contêineres que nunca ganham tag (ex.: `any`). Conflito de tag continua
rejeitado. Contrato fixado em `internal/vm/runtime_type_validation_test.go`.

## baseline (0.3.0, c429bd7) × CoW (feat/cow-value-semantics)

**Data:** 2026-08-16 · Windows 11 · medições intercaladas (os dois binários
alternados dentro da mesma janela — rodadas sequenciais por rótulo mostraram
drift térmico de até ±10% nesta máquina e foram descartadas para o veredito;
ficam em `results/baseline.md` e `results/cow.md` como registro do harness).

**Checksums idênticos em todos os benchmarks** — as duas versões computam os
mesmos resultados.

### Tabela consolidada (mediana de 5 execuções intercaladas)

| bench | perfil | baseline_ms | cow_ms | delta | critério (spec §5.3) | veredito |
|---|---|---|---|---|---|---|
| bench_call_light | chamada O(1), array 10k por valor | 3412 | 2659 | **−22,1%** | ganho | ✅ |
| bench_call_readonly | leitor puro O(n), array 20k | 2223 | 2020 | **−9,1%** | melhora mensurável | ✅ |
| bench_spawn_sum | soma paralela via spawn_task | 1055 | 988 | −6,3% | neutro/ganho | ✅ |
| bench_conway | grid mutado via ref, 60 gerações | 2862 | 2817 | −1,6% | ≤ ~5% | ✅ |
| bench_map_churn | escrita intensa em map | 444 | 438 | −1,4% | ≤ ~5% | ✅ |
| bench_call_ref | mutação in-place via ref | 4495 | 4594 | +2,2% | neutro | ✅ |
| bench_bubblesort | sort in-place via ref | 4059 | 4173 | +2,8%¹ | ≤ ~5% | ✅ |
| bench_path_update | `a[i].x = v` em loop, dono único | 620 | 649 | +4,7% | ≤ ~5% | ✅ |
| bench_share_mutate | compartilha e muta em loop | 509 | 633 | **+24,3%** | livre, documentada | ✅² |

¹ Bubblesort tem a maior variância da suite (−2% a +7% entre sessões); o
valor reportado é a mediana de 9 execuções intercaladas dedicadas.

² Pior caso do CoW por construção: `let b = a` seguido de mutação paga um
clone O(n) por iteração — exatamente o custo que a semântica promete nesse
padrão. Migração: quem quer compartilhamento usa `ref` (custo zero, ver
`bench_call_ref`).

### Interpretação

**Onde o CoW ganha:** chamadas que só leem o composto deixam de pagar a cópia
rasa ansiosa O(n) por chamada. No caso assintótico (`call_light`: função O(1)
com array de 10k elementos), −22%; no leitor O(n) (`call_readonly`), −9%.

**Onde fica neutro:** mutação in-place via `ref` (`bubblesort`, `call_ref`,
`conway`) e contêineres de dono único (`map_churn`, `path_update`) pagam só o
branch de checagem `Shared` por escrita — dentro do ruído ou ≤5%.

**Onde paga:** o padrão compartilha-e-muta (`share_mutate`, +24%) — o preço
explícito da garantia de independência, com `ref` como válvula de escape.

**Achado colateral:** o ganho das chamadas só-leitura é limitado por um custo
pré-existente e independente do CoW — a validação de tipos em runtime varre
todos os elementos do array a cada chamada tipada
(`internal/vm/runtime_type_validation.go`, caso `TYPE_ARRAY`). Em
`call_light`, ela domina o tempo restante nas duas versões. Validar pela tag
`RuntimeType` em O(1) quando presente é a próxima otimização natural, fora do
escopo desta mudança. *(Resolvido no PR #31 — ver a seção da validação O(1)
no topo deste arquivo: a varredura valia para maps/structs/refs também, não
só arrays, e em `call_light` era 97% do tempo.)*

Os números absolutos desta tabela e da tabela do PR #31 não são comparáveis
entre si (sessões diferentes; o aviso de drift térmico acima vale entre
seções) — dentro de cada seção, a comparação é intercalada e válida.

## Reprodução

```powershell
# suite completa por binário (grava results/<label>.md)
powershell -File benchmarks/run_benchmarks.ps1 -Binary <exe> -Label <label>

# comparação intercalada (grava results/interleaved.md) — preferir esta
powershell -File benchmarks/interleaved_compare.ps1 -Baseline <exe> -Candidate <exe>

# corpus de exemplos baseline × candidato
powershell -File benchmarks/compare_examples.ps1 -Baseline <exe> -Candidate <exe>
```
