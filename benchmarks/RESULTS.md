# Benchmarks

Registro corrido das comparações de performance, mais recente primeiro. Cada
seção compara dois binários pelo protocolo intercalado (ver Reprodução no fim).

## v0.6.0 (68209be) × v0.13.0 (63ab106) — sete versões de saldo

**Data:** 2026-08-21 · Windows 11 · i7-1165G7 · protocolo intercalado. Duas
sessões: mediana de 9 (**a reportada**) e mediana de 5 (corroboração). Máquina
com carga de fundo (Zoom/Chrome/Slack) — o que valida a comparação é ela ser
intercalada, não a máquina estar limpa.

Esta seção não é o A/B de uma mudança: é o **saldo acumulado** da v0.6.0 (fim
da fase 1 de perf de dispatch e chamadas) até a v0.13.0, atravessando CoW por
valor, invariante de slot `ref`, genéricos monomorfizados, `io` com cursor e
tipagem estática de membro qualificado. Nenhuma dessas versões teve fase de
perf; várias adicionaram guards de RC/CoW no caminho quente.

**Resumo:** o trabalho do VM ficou **empatado** — ganhos de 5% a 15% no caminho
de chamada tipada, regressões de ~4% na leitura O(n) de array, e o resto no
ruído. O ganho grande do intervalo está em outro eixo e não aparece nos
`bench_*.nx`: **carga de módulos, 2,9x a 9,8x mais rápida** (seção própria
abaixo).

| bench | v060_ms | v0130_ms | delta (n=9) | delta (n=5) | veredito |
|---|---|---|---|---|---|
| bench_typed_call_map | 175,0 | 148,2 | **−15,3%** | −16,3% | ✅ ganho |
| bench_value_call_mutate | 169,2 | 147,4 | **−12,9%** | −17,2% | ✅ ganho |
| bench_call_light | 166,6 | 156,5 | **−6,1%** | −3,9% | ✅ ganho |
| bench_path_update | 935,9 | 891,3 | **−4,8%** | −4,3% | ✅ ganho |
| bench_spawn_sum | 947,7 | 933,2 | −1,5% | −2,4% | ✅ ganho pequeno |
| bench_conway | 2615,6 | 2637,2 | +0,8% | −1,3% | ➖ ruído (sinal troca) |
| bench_share_mutate | 405,3 | 406,4 | +0,3% | −12,9% | ➖ ruído (sinal troca) |
| bench_map_churn | 938,9 | 913,8 | −2,7% | +1,5% | ➖ ruído (sinal troca) |
| bench_call_ref | 4593,3 | 4674,2 | +1,8% | +2,9% | ⚠️ regressão pequena |
| bench_call_readonly | 1510,7 | 1573,7 | **+4,2%** | +9,1% | ⚠️ regressão |
| bench_bubblesort | 4377,0 | 4587,3 | **+4,8%** | +9,0% | ⚠️ regressão |

`bench_generic_vs_hand` foi **pulado**: usa `<T>`, que a v0.6.0 não faz parse.
Ver a nota sobre o guard de checksum abaixo.

### Leitura

**O caminho de chamada tipada ficou materialmente mais rápido.** Os três
maiores ganhos — `typed_call_map` (−15,3%), `value_call_mutate` (−12,9%) e
`call_light` (−6,1%) — são todos chamada com composto tipado atravessando a
fronteira. O candidato mais provável é `58f2cad` (#55, v0.10.1): construtores
de `internal/value` viraram donos duráveis dos filhos, `invokeBoundaryCall`
**deixou de reter o result** e `retainingArray`/`retainingMap` sumiram — ou
seja, retain/release a menos exatamente por chamada que cruza a fronteira.
Atribuição por coincidência de perfil, não por bisect: 155 commits separam os
dois binários e nenhum bisect foi feito nesta rodada.

Não é a validação de tipos O(1) por tag (PR #31): ela foi mergeada em
2026-08-16 e **já está na v0.6.0** (tag de 2026-08-18) — os dois binários a
têm.

**As duas maiores regressões estão no lado da leitura.** `call_readonly` (+4,2%) e
`bench_bubblesort` (+4,8%) são leitura O(n) de array — indexação repetida,
sem mutação. É o perfil onde os guards de `Shared`/RC por acesso aparecem sem
nada para compensar. **É o item a investigar**, e coincide com o que o
cross-runtime aponta como pior ponto absoluto (indexação de array, 8,5x do
CPython).

**Três benches ficaram no ruído com troca de sinal** (`conway`, `share_mutate`,
`map_churn`). `share_mutate` foi de −12,9% para +0,3% entre as duas sessões, e
o baseline de `map_churn` variou 53% (612,9 → 938,9 ms) só por carga — lembrete
de que o piso de ruído desta suíte não é ±1%, e que delta abaixo de ~3% em uma
única sessão não é conclusivo.

### O maior ganho do intervalo não está na tabela acima: carga de módulos

Os `bench_*.nx` são todos de um arquivo só, então nenhum deles exercita `use`.
As sondas de startup (`startup_use_*.nx`, que o `interleaved_compare.ps1` não
varre) medem exatamente isso — mínimo de 15 execuções intercaladas:

| sonda | v060_ms | v0130_ms | delta |
|---|---|---|---|
| `startup_use_selectall.nx` (`use http select *`) | 1873,2 | 190,5 | **−89,8%** (9,8x) |
| `startup_use_namespace.nx` (`use http`) | 437,2 | 148,5 | **−66,0%** (2,9x) |
| `startup.nx` (nenhum `use`, controle) | 154,9 | 130,0 | −16,1% |

Causa: `19156a7`, memoização de `loadModuleDeclarations`
(`internal/compiler/module_exports.go`). Antes, cada `use` era recarregado por
chamador e dobrado pelo two-pass; agora é uma carga por módulo.

O comentário em `startup_use_selectall.nx` atribui a amplificação ao branch de
genéricos, mas **a v0.6.0 é anterior a genéricos e mesmo assim paga 1,87 s** —
ou seja, o recarregamento por chamador já existia antes, e genéricos apenas o
dobraram. A memoização consertou os dois.

Isto é custo de **compilador**, não de VM: aparece uma vez por processo, e é o
que domina o tempo de qualquer script curto que use a stdlib — o caso Lambda,
por exemplo. O controle sem `use` (−16,1%) confirma que há também um ganho
menor e separado no piso de processo puro, esse ainda sem causa atribuída.

**Contexto de sistema:** o cross-runtime da mesma data mede o mesmo par de
binários contra CPython/Lua/Go e conclui empate no trabalho do VM com startup
−15% — ver
[`cross_runtime/README.md`](cross_runtime/README.md).

### Correção de metodologia aplicada nesta rodada

`interleaved_compare.ps1` **não validava equivalência** entre os dois binários.
Um bench que o baseline não compila sai do erro em ~30 ms e entrava na tabela
como regressão gigante do candidato — a comparação seria entre um programa e
uma mensagem de erro. `bench_generic_vs_hand` é exatamente esse caso contra a
v0.6.0.

O script agora exige linha `CHECKSUM:` idêntica dos dois lados no warmup (mesmo
guard que `cross_runtime/run_cross_runtime.ps1` já tinha) e **pula** o bench,
listando-o no relatório, em vez de medi-lo. Os rótulos das colunas também
deixaram de ser fixos em `baseline`/`cow` (herança da rodada do CoW) e passaram
a `-BaselineLabel`/`-CandidateLabel`.

## develop (bff429a) × genéricos paramétricos (feat/generics)

**Data:** 2026-08-18 · Windows 11 · protocolo intercalado, mediana de 9
execuções por sessão. Spec: `docs/superpowers/specs/2026-08-18-generics-design.md`
§11. Task: `.superpowers/sdd/2026-08-18-generics/task-16-brief.md`. Gates
novos desta seção: `generic_vs_hand` e regressão do corpus não-genérico são
gates duros; `startup_generics` é informativo (spec §11: "sem regressão
material do startup", sem limiar numérico fixado — este relatório adota
sinalização em `> +15ms` como critério operacional, per orientação da task
brief).

**Verificação completa:** `go vet ./...` sem saída (limpo); `go test ./...
-count=1` verde em todos os pacotes (`internal/compiler` 68,117s,
`internal/vm` 145,820s, resto sub-segundo — a suíte de `internal/vm` inclui
os testes novos de genéricos: unificação, cloner de AST com guard por
reflexão, igualdade de bytecode, E2E, interações de runtime, negativos).
**Corpus de exemplos 164/164** (`run_all_tests_concurrent.nx`, 20187ms) no
binário `feat/generics` (`noxy_generics_bench.exe`, `c93cfb3`) — mesmo número
já reportado na seção da fase 1 de dispatch, confirmando que o corpus
existente continua passando por inteiro com genéricos ativos (gate de
regressão do corpus, spec §11).

### Gate 1 — `generic_vs_hand`: razão ≤ 1,05 (Ruling R4 do controller)

**Divergência registrada:** a spec §11 declara gate de razão **1,0x**; o
controller (Ruling R4, binding) definiu **≤ 1,05** como folga de ruído sobre
esse 1,0x — a garantia dura de custo-zero continua sendo o teste de
igualdade de bytecode (Task 8: `first<int>` monomorfizado emite a mesma
sequência de opcodes que a versão escrita à mão), não este benchmark de
parede. Este relatório usa 1,05 por autoridade de R4.

`benchmarks/bench_generic_vs_hand.nx` só roda no binário `feat/generics` (o
binário `develop` não faz parse de `<T>`), então não há segundo binário para
intercalar contra. Em vez disso a medição é **interna ao processo**
(`time_now_ms()`), em ordem **ABBA** (gen, hand, hand, gen) dentro da mesma
execução — necessário porque uma medição AB simples (gen sempre primeiro)
mostrou deriva sistemática de até +9% entre sessões¹, e o gate de 5% é
apertado demais para o piso de ruído documentado no resto da suíte
(~1-4%). ABBA dá às duas funções a mesma posição média (2,5) na sequência,
cancelando deriva linear dentro do processo. O binário inteiro roda 9 vezes
por sessão; mediana de `GEN_MS`/`HAND_MS` calculada separadamente, razão das
duas medianas.

| sessão | gen_ms (mediana) | hand_ms (mediana) | razão |
|---|---|---|---|
| 1 | 387 | 400 | 0,968 |
| 2 | 378 | 387 | 0,977 |
| 3 | 377 | 374 | 1,008 |

**Agregado (mediana das 3 sessões, por lado):** gen_ms=378, hand_ms=387,
**razão = 0,977** → ✅ **dentro do gate** (≤ 1,05). Checksum idêntico
(`384128000`) em todas as 27 execuções (3 sessões × 9), confirmando que
`soma_gen<int>` e `soma_hand` computam o mesmo resultado.

### Gate 2 — regressão do corpus não-genérico (develop × genéricos)

Roda 3 benchmarks existentes não-genéricos (`bench_bubblesort.nx`,
`bench_call_light.nx`, `bench_typed_call_map.nx` — os três já rastreados
como gate na seção da fase 1 de dispatch) via `interleaved_compare.ps1`
apontado para um diretório temporário só com esses três arquivos (copiados;
o script varre `bench_*.nx` do próprio diretório e os dois arquivos novos
desta tarefa usam sintaxe `<T>` que o binário `develop` não faz parse —
incluí-los quebraria a varredura inteira no lado `develop`). **5 sessões**
rodadas (uma sessão inicial cujo `results/interleaved.md` falhou por
diretório ausente, valores só no console — mantida porque a medição em si é
válida — mais 4 sessões limpas), porque `bench_typed_call_map` deu um
outlier de +12,6% na sessão 2, acima do limiar de ~5% usado no resto do
projeto — ver nota².

| bench | develop_ms | generics_ms | delta agregado | veredito |
|---|---|---|---|---|
| bench_bubblesort | 4013,8 | 4050,0 | +0,90% | ✅ |
| bench_call_light | 78,0 | 75,9 | −2,69% | ✅ |
| bench_typed_call_map | 87,1 | 86,6 | −0,57%² | ✅ |

Valores agregados: mediana das 5 sessões, calculada separadamente para
`develop_ms` e `generics_ms`, delta recalculado a partir das duas medianas
(mesma convenção da seção da fase 1 de dispatch).

### Info — `startup_generics` (sem gate duro; sinalizar > +15ms)

`benchmarks/startup_generics.nx` (1 template `Caixa<T>` + 2 instanciações,
`Caixa<int>` e `Caixa<string>`, + print) contra
`benchmarks/cross_runtime/startup.nx` (piso de processo sem genéricos),
**ambos no binário `feat/generics`** — isola o custo marginal da detecção
"há genéricos?" e do pass 1 de monomorfização sobre o mesmo binário, sem
confundir com qualquer outra diferença entre binários. Script ad hoc
(`interleaved_files.ps1`, mesma técnica de `interleaved_compare.ps1` com o
eixo trocado: binário fixo, arquivo variável), 3 sessões de 9 execuções.

| sessão | startup_ms (sem genéricos) | startup_generics_ms | delta |
|---|---|---|---|
| 1 | 51,3 | 53,3 | +2,0ms |
| 2 | 52,2 | 52,8 | +0,6ms |
| 3 | 51,4 | 55,4 | +4,0ms |

**Agregado:** 51,4ms → 53,3ms, **delta = +1,9ms** — bem abaixo do limiar de
sinalização de +15ms. Custo de compilação dobrando sobre uma base de poucos
ms (spec §11: "a compilação ~dobra, sobre uma base de poucos ms") é
consistente com a ordem de grandeza aqui: o programa inteiro (parse + duas
passes + compile + run + print) segue no mesmo patamar do piso de processo
(~50-55ms), a monomorfização de 1 template com 2 instanciações não é visível
acima do ruído de spawn de processo nesta escala.

¹ Medição AB descartada (não commitada): mesmo bench, ordem fixa gen-depois-
hand, 2 sessões de 9 execuções — sessão 1 deu razão 1,008 (dentro do gate),
sessão 2 deu **1,094** (acima do gate de 1,05). A causa aparente é a segunda
função chamada dentro do processo herdar cache/branch-predictor "quente" da
primeira, não custo real de despacho genérico — a versão ABBA committada
elimina essa fonte de viés por construção (ver corpo do gate 1 acima).

² O outlier da sessão 2 (`bench_typed_call_map` +12,6%, base=79,9ms
cand=90ms) não se repete nas outras 4 sessões (deltas: +0,9%, −0,6%, −0,1%,
−3,2%) — é o bench de menor escala absoluta dos três (~80-90ms), mesmo
padrão de sensibilidade a ruído de sistema já documentado para benches
curtos na seção da fase 1 de dispatch (nota¹ daquela seção). A mediana
agregada das 5 sessões (−0,57%) fica bem dentro do gate; sem esse outlier a
leitura seria idêntica.

### Interpretação

**Gate 1 confirma o custo-zero em runtime além do teste de bytecode.** A
razão agregada (0,977, faixa 0,968–1,008 nas 3 sessões) está centrada em
torno de 1,0, sem viés sistemático para nenhum lado — exatamente o esperado
quando `soma_gen<int>` e `soma_hand` emitem a mesma sequência de opcodes
(Task 8). A divergência do gate declarado na spec (1,0x → ≤1,05x, Ruling R4)
existe para dar folga ao ruído de medição de parede, não porque exista custo
real: o teste de bytecode é quem carrega a prova, este benchmark é
confirmação empírica adicional.

**Gate 2 confirma que o corpus não-genérico não paga nada pela existência de
genéricos no compilador.** As três magnitudes agregadas (+0,90%, −2,69%,
−0,57%) ficam dentro do mesmo piso de ruído (~1-4%) documentado nas seções
anteriores deste arquivo para essa máquina — nenhum sinal de regressão real.
Consistente com a spec §11: "programa sem genéricos continua pulando o pass
1 inteiro".

**O startup paga um custo pequeno e não sinalizável.** +1,9ms agregados (faixa
+0,6 a +4,0ms nas 3 sessões) para compilar um programa com 1 template e 2
instanciações, contra um piso de processo de ~51ms — ordem de grandeza
consistente com a spec (compilação "~dobra... sobre uma base de poucos ms").
Não há indício de que a detecção "há genéricos?" ou o pass 1 de
monomorfização introduzam custo que se acumule com o tamanho do programa
nesta escala; ficaria para benchmarks futuros com mais templates/instâncias
verificar se o custo permanece sub-linear.

### Adendo — startup de `use` (finding C1 da revisão final de branch)

**Data:** 2026-08-18, sessão posterior às medições acima. Motivo: a revisão
final de branch mediu um caso que `startup_generics` **não** cobria —
startup de um programa que só faz `use`, **sem genérico nenhum** — e achou
uma amplificação de ~8x. Gate 2 (corpus não-genérico) não a pegou porque os
três benches daquele gate não importam módulo.

**Causa** (`internal/compiler/module_exports.go`): `loadModuleDeclarations`
re-parseava **e** re-compilava (validator descartável) o módulo a cada
chamada, recursivamente, sem cache algum. O branch de genéricos somou ~4
chamadas por `use` (`predeclareImportedTemplates` →
`discoverModuleExports` + `moduleTopLevelBindings`; `predeclareImport`; o
case `*ast.UseStmt`), tudo dobrado pelo two-pass. Custo pré-existente
(base já pagava 1,4s num `select *`), amplificado pelo branch.

**Correção:** memoização por `moduleDiscoveryState` (módulo → programa
parseado + submódulos), com o estado agora criado **uma vez por compilador**
(`discoveryState()`) e propagado por `NewChild`/`newPass1Compiler` — antes,
cada chamador fabricava um estado descartável e nenhum memo sobreviveria.
Só sucessos entram no cache: uma falha pode ser contextual (guard de ciclo),
um sucesso nunca é.

Sondas versionadas: `benchmarks/startup_use_selectall.nx` e
`benchmarks/startup_use_namespace.nx`. Mediana de 9 execuções por célula,
mesmo processo/binário por linha. `head (pré-C1)` = `314428d`, último commit
do branch antes desta rodada de fixes.

| sonda | base (`bff429a`) | head (pré-C1) | head (pós-C1) |
|---|---|---|---|
| `startup_use_selectall.nx` (`use http select *`) | 1445,8 ms | 11681,2 ms | **96,2 ms** |
| `startup_use_namespace.nx` (`use http`) | 361,5 ms | 1474,4 ms | **101,2 ms** |
| `startup_generics.nx` (controle, com genéricos) | 62,3 ms | 65,8 ms | 60,2 ms |

O head pós-fix não só volta à ordem de grandeza da base como fica **~15x
mais rápido que a base** no `select *` — a memoização paga também o custo
pré-existente, que a base sempre teve. A sonda de controle
(`startup_generics`) não se move: o caminho de genéricos não foi tocado.

Efeito colateral na suíte de testes (mesma causa, mesmo fix):
`go test ./internal/compiler` 48,7s → **1,3s**; `go test ./internal/vm`
128,8s → **41,6s**.

**Corpus:** 167/167 (`run_all_tests_concurrent.nx`, 11502ms) no binário
pós-fix — sem regressão.

## develop (f107508) × fase 1 de dispatch e chamadas (perf/vm-dispatch-fase1)

**Data:** 2026-08-18 · Windows 11 · protocolo intercalado, mediana de 9
execuções por sessão — **4 sessões limpas repetidas** (nenhum outro processo
de benchmark ativo) porque `bench_share_mutate` e `bench_call_light` ficaram
perto da fronteira do gate/com dispersão notável entre sessões; ver nota¹
sobre o ruído e nota² sobre duas sessões adicionais descartadas por
contaminação de medição. Spec:
`docs/superpowers/specs/2026-08-17-vm-perf-static-typing-research.md`.
Commits do branch: cache de globais com geração (`67e9d52`), `OP_CALL_STATIC`
(`bb8a773`), `CallFrame` em array de valores reusado, allocs/chamada 1012→10
(`1ca26e7`+`d460e5a`), seis opcodes fundidos de comparação+salto inteiro
(`c43276c`+`32b33c7`), `OP_INC_LOCAL_INT` (`56882ca`), seis opcodes `_FLOAT`
especializados (`4d43cdf`+`6d991e5`).

**Verificação completa (Step 1):** `go vet ./...` sem saída (limpo);
`go test ./... -count=1` verde em todos os pacotes (`internal/compiler`
5,072s, `internal/vm` 58,068s, resto sub-segundo); `go test ./internal/value
-race -count=1` verde (1,093s); `go test ./internal/vm -race -count=1` verde
(74,934s). **Corpus de exemplos 164/164** (`run_all_tests_concurrent.nx`,
10849ms) — número corrigido de 130 para 164 no commit `e7c533d`, confirmado
aqui na íntegra.

| bench | perfil | develop_ms | perf_ms | delta | veredito |
|---|---|---|---|---|---|
| bench_call_readonly | leitor puro O(n), array 20k | 1732,3 | 1121,3 | **−35,3%** | ✅ |
| bench_spawn_sum | soma paralela via spawn_task | 1018,2 | 696,8 | **−31,6%** | ✅ |
| bench_conway | grid mutado via ref, 60 gerações | 2554,3 | 2063,0 | **−19,2%** | ✅ gate |
| bench_call_ref | mutação in-place via ref | 4206,5 | 3622,0 | −13,9% | ✅ |
| bench_map_churn | escrita intensa em map | 443,8 | 394,1 | −11,2% | ✅ |
| bench_bubblesort | sort in-place via ref | 3897,5 | 3493,5 | −10,4% | ✅ |
| bench_path_update | `a[i].x = v` em loop, dono único | 621,1 | 580,0 | −6,6% | ✅ |
| bench_typed_call_map | chamada tipada in-module, map 2,5k via ref | 76,9 | 73,4 | −4,6% | ✅ gate |
| bench_call_light | chamada O(1), array 10k por valor | 65,5 | 62,6 | −4,5%¹ | ✅ gate |
| bench_value_call_mutate | helper por valor em laço de mutação | 70,1 | 67,1 | −4,3% | ✅ |
| bench_share_mutate | compartilha e muta em loop | 232,9 | 249,1 | **+7,0%**³ | ❌ **acima do gate** |

Valores agregados: mediana das 4 sessões limpas, calculada separadamente para
`develop_ms` e `perf_ms` (cada sessão já é mediana de 9 execuções
intercaladas), delta recalculado a partir das duas medianas.

¹ `bench_call_light` variou de −12,9% a +0,6% entre as 4 sessões (mediana de
deltas por sessão: −8,3%); a sessão que deu +0,6% é a mesma que deu o pior
número de `bench_share_mutate` (+10,5%, nota³) — indício de uma perturbação
pontual de sistema naquela sessão específica, não um padrão. Mesmo no pior
caso agregado (−4,5%), o bench segue dentro do gate.

² Duas sessões adicionais foram descartadas: por um erro de execução, um
processo de benchmark em background (`interleaved_compare.ps1` intercalando
os mesmos binários) ficou rodando **ao mesmo tempo** que uma segunda
invocação em primeiro plano — os dois competiam por CPU. Os deltas dessas
duas sessões contaminadas (`bench_call_readonly` −33% a −36%,
`bench_conway` −17% a −21%, `bench_spawn_sum` −31% a −33%) são consistentes
em direção com as 4 sessões limpas usadas na tabela acima, então a
contaminação não parece ter invertido nenhum resultado — mas foram
descartadas mesmo assim por não seguirem o protocolo (nenhum outro processo
de benchmark ativo), e por terem escrito no mesmo arquivo
`results/interleaved.md` uma da outra.

³ **Gate excedido.** `bench_share_mutate` — pior caso do CoW por construção
(`let b = a` seguido de mutação de um array de 5000 elementos, 2500 rodadas)
— fica **acima dos +5%** nas 4 sessões limpas: +6,4%, +6,1%, +2,8%, +10,5%
(mediana das 4 deltas por sessão: +6,25%; delta das medianas agregadas:
+7,0%). Só 1 das 4 sessões (+2,8%) ficaria dentro do gate; as outras 3
excedem. Um profile do candidato (`noxy_perf.exe --cpuprofile`, 15 repetições
da carga do bench para juntar amostra) mostra o tempo extra concentrado no
caminho de clone/CoW já existente — `(*VM).copyValue` (42,3% cum),
`runtime.scanobject`/`scanblock` (GC, ~27% cum), `value.Retain`,
`value.ownersOf`, `runtime.bulkBarrierPreWrite` — **nenhum símbolo da fase 1**
(cache de globais, `OP_CALL_STATIC`, `CallFrame`, opcodes fundidos) aparece
nesse profile. Não há profile do baseline para comparar lado a lado (a flag
`--cpuprofile` é ela própria um commit deste branch — `noxy_develop.exe` não
tem a flag), então a causa não pôde ser isolada: hipótese em aberto é efeito
colateral de pressão de GC do novo layout do `CallFrame` (array de valores
reusado pode manter o slot anterior vivo por mais tempo antes de ser
sobrescrito, mudando o que o GC varre por chamada); alternativa é ruído puro
— é o bench de menor escala absoluta da suíte (~230-250ms), o que amplia
qualquer jitter de agendamento em pontos percentuais.

**Bissecção posterior (2026-08-18)** resolveu a dúvida ruído-vs-real e
descartou a hipótese de ruído puro. Protocolo: worktree destacada, quatro
commits medidos intercalados (`f107508` baseline, `bb8a773` = tudo antes do
reuso de `CallFrame`, `d460e5a` = reuso de frame já corrigido, `6d991e5` =
head), 3 sessões (N=15, N=20, N=20), com **controle de rótulo duplicado
apontando para o mesmo binário** nas sessões 2-3 para medir o piso de ruído.

- **Piso de ruído** (mediana × mediana, mesmo executável, mesma sessão):
  **~1,2-1,4%**. Amostras cruas são bem mais dispersas (desvio ~6-12% da
  mediana em trechos limpos, até ~20-28% de CV quando houve contaminação de
  fundo), o que explica a dispersão entre sessões da tabela acima.
- **A regressão é real e reprodutível**: baseline→head deu +9,97%, +9,78% e
  +6,47% nas três sessões (média **+8,7%**), ordem de grandeza acima do piso
  — consistente com os +7,0% da tabela.
- **A atribuição a um único commit NÃO fecha**: o passo
  `bb8a773`→`d460e5a` (reuso de `CallFrame`) concentrou todo o salto na
  sessão 1 (+14,9%), mas só +1,4% e +4,0% nas sessões 2-3, onde a regressão
  se acumulou gradualmente ao longo dos commits seguintes. Na média o reuso
  de frame ainda é o maior contribuinte isolado (+6,8pp de +8,7pp) e é por
  onde começar, mas provavelmente não é a causa única — o padrão é
  compatível com efeito difuso de alocação/GC espalhado por vários commits
  da fase, o que também explica o profile não mostrar nenhum símbolo da
  fase 1 no caminho quente.

Detalhes e medições cruas (protocolo, os 4 commits medidos, todas as
amostras individuais e o controle de rótulo duplicado):
`benchmarks/results/2026-08-18-share-mutate-bisect.md`. **Regressão aceita e
rastreada, não corrigida nesta fase** — ver Interpretação.

### Perfil de cada bench

- **`bench_call_readonly`/`bench_spawn_sum`/`bench_conway`** — os três
  maiores ganhos (−35,3%, −31,6%, −19,2%) surpreendem porque nenhum dos três
  é o alvo declarado da fase (fib/loop_arith/mandelbrot). Motivo: todos têm
  laço quente `while i < N do ... i = i + 1 end` (ganha com os seis opcodes
  fundidos de comparação+salto e `OP_INC_LOCAL_INT`) **e** chamada de função
  in-module (`spawn_sum` chama a task por closure; `conway` chama `step`/`idx`
  por iteração; `call_readonly` chama o leitor a cada rodada) — ganha também
  com `OP_CALL_STATIC` e o `CallFrame` sem alocação. A fase mirava fib para
  #1/#2 e loop_arith/mandelbrot para #3, mas as duas otimizações de chamada
  (#1/#2) valem para **qualquer** chamada in-module tipada, não só fib — e é
  isso que aparece aqui.
- **`bench_call_ref`/`bench_map_churn`/`bench_bubblesort`/`bench_path_update`**
  — mesma explicação em menor grau (−13,9% a −6,6%): laço com incremento
  inteiro fundido, mas o custo dominante desses benches é o caminho de
  referência/CoW (`resolveReferenceValue`, unicidade), que esta fase não
  tocou — por isso o ganho é menor que nos três benches acima.
- **`bench_typed_call_map`/`bench_call_light`** (gates) **e
  `bench_value_call_mutate`** (não é gate, mesmo padrão) — chamada
  O(1)/O(2500) com container grande por `ref`/valor, ganho modesto (−4,3% a
  −4,6%): já estavam perto do piso depois da validação O(1) por tag (PR
  #31), então sobra pouco para os opcodes desta fase cortarem.
- **`bench_share_mutate`** — ver nota³ acima: único bench que regride, e o
  único cujo perfil não mostra nenhuma infraestrutura desta fase.

### Interpretação

**O alcance da fase foi maior que o previsto.** O plano mirava
fib/loop_arith/mandelbrot para as otimizações de globais, chamadas e opcodes
fundidos (fases 0-3 da spec). Nos benches CoW — nenhum deles no escopo
original — 9 de 11 melhoraram, vários de forma expressiva
(`call_readonly` −35%, `spawn_sum` −32%, `conway` −19%): o custo de chamada
in-module (#1/#2) e o incremento fundido de laço (#3) atravessam qualquer
programa com esse formato, que é a maioria do corpus real.

**Onde fica neutro:** `bench_typed_call_map`, `bench_call_light` e
`bench_value_call_mutate` — os três já operavam perto do piso pós-validação
O(1) (PR #31); ganho pequeno mas real, dentro do gate.

**Onde paga, sem maquiagem:** `bench_share_mutate` excede o gate de 5% em 3
das 4 sessões limpas (mediana +6,25% a +7,0%, ver nota³). Ao contrário da
rodada da fase RC-uniqueness (onde `map_churn`/`spawn_sum` excediam o gate de
forma consistente e explicável pelo bookkeeping de RC), aqui a causa não está
clara: o profile do bench não mostra nenhum código desta fase, só o caminho
de clone/GC que já existia antes dela. **Gate formalmente reprovado** —
reportado sem tentativa de correção, para decisão do controller.

**O que resta:** o pprof pós-fase de `fib` (ver spec de pesquisa) mostra que,
com globais e alocação de chamada resolvidos, `push`/`pop` (Value de 48
bytes, item #3 da pesquisa) voltaram a ser a maior fração isolada depois do
dispatch puro — candidato natural da próxima fase. `bench_share_mutate`
citado acima é o outro fio solto: precisa de um profile do baseline (fora do
alcance sem recompilar `noxy_develop.exe` com a flag `--cpuprofile`) para
isolar a causa.

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
(inc/dec por elemento a cada operação) e `spawn_sum` paga nos laços quentes
dos workers, onde cada rebind de local escalar (`s = ...`, `i = ...`) passa
pelos funis de RC do OP_SET_LOCAL (ownSlot + Release por iteração). Os args
do bench são primitivos e canal — Retain/Release são no-ops neles, então o
custo é a passagem pelos funis em si, não contagem; o preparo da task
(retain de captura + bind de posse dos parâmetros por valor no frame da
task) roda 4 vezes no bench inteiro e não aparece na conta.
Comparado ao round 4 da Task 7 (map_churn +9,8%, spawn_sum
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
powershell -File benchmarks/interleaved_compare.ps1 -Baseline <exe> -Candidate <exe> `
           -BaselineLabel v060 -CandidateLabel v0130 -Runs 9

# corpus de exemplos baseline × candidato
powershell -File benchmarks/compare_examples.ps1 -Baseline <exe> -Candidate <exe>

# referência externa (Noxy × CPython × Lua × Go), com as duas versões juntas
powershell -File benchmarks/cross_runtime/run_cross_runtime.ps1 -Noxy <exe> `
           -NoxyBaseline <exe-antigo> -BaselineLabel v060
```

Os binários têm de estar em **disco local**: este repo vive em OneDrive e medir
de lá infla os tempos em ~2x (filtro de sync + antivírus no read). Benches que
não rodam nos dois binários são pulados e listados no relatório, não medidos.
