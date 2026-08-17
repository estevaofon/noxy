# Posse Única por Contagem de Referências Duráveis (RC-uniqueness)

**Data:** 2026-08-17 · **Branch:** `perf/cow-uniqueness-rc` · **Status:** proposta
**Relação:** evolução da implementação da spec
`2026-08-16-cow-value-semantics-design.md` (CoW 0.4.0). O contrato semântico
daquela spec permanece integralmente válido; esta spec troca só o mecanismo
que decide *quando clonar*.

## 1. Objetivo

Eliminar por construção os O(N²) do CoW causados por **compartilhamento
morto** — clones pagos quando o alias que motivou a marca já não existe.
Hoje o bit `Shared` é sticky (só liga, nunca desliga): qualquer passagem por
valor condena o contêiner a clonar na próxima mutação, para sempre.

Caso emblemático (NoxyDB, reproduzido e medido): helper `database_file(db)`
por valor chamado em laço de puts → 3 clones por put (instância, state e o
map de payloads inteiro) → O(N²). Medido no repro `struct→struct→map`:
N=4000 puts em 4.658ms com curva quadrática (pós-PR #31, que removeu o outro
quadrático — a validação de tipos), contra ~130ms flat da variante sem a
passagem por valor. Contador de clones confirma: 3N clones na variante por
valor, 3 no total nas demais.

**Meta:** nenhum clone jamais acontece por compartilhamento morto. Clones
passam a ocorrer somente quando existe **outro dono vivo** no momento da
mutação.

**Não-meta:** reduzir o custo de snapshots genuinamente vivos (programa que
de fato consome N cópias paga O(rodadas×n) em qualquer linguagem de semântica
de valor, inclusive Swift — o remédio seria estrutura persistente, fora de
escopo, ver §9).

## 2. Modelo de referência: Swift

O desenho é o do CoW do Swift, adaptado de compilador AOT com ARC para
interpretador bytecode com GC:

| Swift | noxy (esta spec) |
|---|---|
| refcount do ARC no buffer | contador `Owners` no composto |
| `isKnownUniquelyReferenced()` antes de mutar | `Owners == 1` → in-place; `> 1` → clona |
| parâmetro normal `+0 guaranteed` (empréstimo) | temporário de pilha não conta |
| retain quando o callee armazena | incremento só em vínculo durável |
| releases inseridos pelo compilador | fase 1: dec central no fim do frame; fases seguintes: drops emitidos pelo compilador |

Diferença estrutural a nosso favor: o ARC também gerencia memória, então
errar release corrompe. Aqui a memória é do GC do Go — o contador é **apenas
oráculo de unicidade**. Contar a mais (ex.: dec pulado por unwind, ciclos)
degrada para uma cópia extra, nunca corrompe. O único erro grave é contar a
menos (mutação in-place com alias vivo); todo o desenho e o plano de testes
giram em torno de impedir essa direção.

## 3. Contrato semântico (visível ao usuário): INALTERADO

Nenhuma mudança observável. Valem todos os itens da spec CoW §2:
independência de cópias em qualquer vínculo sem `ref`, `ref` como único
mecanismo de compartilhamento, igualdade estrutural, semântica de canais e
tasks. A fronteira do snapshot continua sendo o momento do **vínculo**
(chamada/atribuição/inserção), não o momento do push na pilha — paridade
exata com hoje, onde a marcação ocorre em `vm.call` após avaliar os
argumentos.

Critério operacional de paridade: o corpus de exemplos
(`benchmarks/compare_examples.ps1`, 130 programas) e a suíte de
caracterização (`internal/vm/value_semantics_test.go`) devem produzir os
mesmos resultados. Testes que ancoram **contagem exata de clones** podem
mudar para menos clones — cada mudança dessas é revisada uma a uma como
melhoria esperada, nunca aceita em lote.

## 4. Design

### 4.1 O contador `Owners`

Em `ObjArray`, `ObjMap` e `ObjInstance`: `Shared atomic.Bool` é substituído
por `Owners atomic.Int32`. Escalares e strings (imutáveis) ficam fora, como
hoje.

- `IsShared(v)` → `Owners.Load() > 1`.
- `unicize`/opcodes `*_MUT`/builtins mutantes: inalterados na lógica — só o
  predicado muda. Clona quando compartilhado; agora "compartilhado" é
  preciso.
- Contador atômico porque tasks são paralelas (mesmo requisito do ARC).

### 4.2 A regra única: vínculo durável conta, empréstimo não

**Incrementa** quando o composto entra num lugar que sobrevive à expressão
corrente; **decrementa** quando esse lugar morre ou é sobrescrito.

| lugar durável | inc | dec |
|---|---|---|
| slot de parâmetro (sem `ref`) | bind na chamada (`vm.call`, onde hoje há `MarkShared`) | fim do frame (retorno E unwind, funil único em `finishFrame`) |
| slot de local (`let`, reatribuição) | no vínculo (sites de `emitMarkSharedForStore`, generalizados: **todo** composto, fresco ou alias) | sobrescrita do slot; fim do frame; fim de bloco (fase 1.5) |
| global (`Environment`) | `SetLocal` | sobrescrita |
| upvalue (box) | captura/store | sobrescrita; box coletado é irrelevante (GC) |
| elemento de contêiner | `OP_ARRAY`/`OP_MAP`/`OP_SET_INDEX`/`append`/campo de struct (constructor e `OP_SET_PROPERTY`) | sobrescrita do elemento, `delete`, `pop` |
| buffer de canal | `chan_send` | `chan_recv` (o valor sai do buffer) |
| captura de task (`spawn`/`spawn_task`) | preparação dos args | handoff para os slots de parâmetro da task (que fazem seu próprio inc) |
| captura de `defer` | captura | após execução do defer |

**Não conta (empréstimo, +0):**

- Temporários da pilha de operandos (push/pop) — nunca.
- `ref`: um `VAL_REF` aliasa um *slot*, não cria dono novo do objeto; o slot
  já conta. Escrita através do ref = sobrescrita do slot (dec velho, inc
  novo).
- Natives da allowlist só-leitura (`cow_natives.go`): empréstimo puro.
- Natives **com** assinatura: cópia ansiosa mantida (spec CoW §4.6) — fora
  do RC.
- Natives **sem** assinatura fora da allowlist: inc permanente sem dec
  (tradução fiel do sticky de hoje: "assuma retenção"). Mesma perf de hoje,
  nunca pior.

Consequência de simplificação: a distinção fresco/alias do
`emitMarkSharedForStore` (cow_lowering) desaparece — todo vínculo de
composto incrementa, e um literal fresco vinculado a um `let` simplesmente
nasce com `Owners == 1` (dono único, mutação in-place), que é o
comportamento desejado sem caso especial.

### 4.3 Mutação e clone

- `Owners > 1` na mutação: clona no slot mutante (como hoje). O slot solta o
  objeto antigo (**dec 1** nele) e passa a dono único do clone
  (`Owners = 1`). Cada filho imediato do clone ganha **+1** (o clone é um
  novo dono durável deles) — substitui o `MarkShared` dos filhos que o
  `copyValue` faz hoje, dentro do O(n) que o clone já custa.
- `Owners <= 1`: muta in-place. (`0` ocorre só em valores em trânsito puro
  na pilha; mutação sempre passa por slot, que incrementou antes.)

### 4.4 Retorno de função

`OP_RETURN` entrega o resultado como temporário (+0). Se o resultado é um
local do frame, o dec do fim do frame o leva a `Owners-1` enquanto em
trânsito — correto: o vínculo do frame morreu; o caller incrementa ao
vincular. Não há promoção especial: o caso "retornei meu próprio parâmetro"
resolve por aritmética (caller: 1 do slot original + 1 do novo vínculo = 2 →
primeira mutação de qualquer lado clona — independência preservada).

### 4.5 O que morre com esta spec

- `MarkShared`/bit sticky e toda a classe de auditoria "esqueceram de marcar
  no caminho X" — os sites viram inc/dec da tabela §4.2, e a allowlist de
  natives vira só a lista de empréstimos.
- `OP_MARK_SHARED`/`OP_COPY` como marcadores de compartilhamento (o
  lowering emite os incs nos mesmos pontos).

### 4.6 Observabilidade

`cloneCount` (spec CoW §4.9) permanece — é a âncora dos testes de regressão
de perf. Adicional para testes: leitura de `Owners` de um valor
(test-only/interno), permitindo ancorar "após o retorno, `Owners == 1`".

## 5. Fases de implementação

A ordem ataca primeiro a forma de compartilhamento morto que causou o bug
real, com a propriedade de segurança de que **dec faltante nunca é unsound**
(só custa uma cópia):

- **Fase 1 — granularidade de frame (o alvo real).** Incs completos da
  tabela §4.2; decs centralizados em `finalizeCurrentFrame` (funil único de
  retorno normal E unwind, após os defers). O frame mantém uma lista dos
  **slots que ele reteve** (lista de pares slot→objeto retido: parâmetros no
  bind, locais via opcode de posse no let; o release libera o OBJETO
  GRAVADO — liberar o ocupante atual do slot seria unsound sob reuso de slot
  por temporários após a morte de locais de bloco); o release percorre só
  essa lista. Varrer a região de slots do frame seria unsound no unwind
  (temporários nunca retidos seriam liberados — dec a menos); a lista
  elimina esse risco e dispensa metadado de contagem de locais no
  compilador. Elimina o caso `database_file(db)` e todo dead-share em forma
  de chamada, inclusive cross-module.
- **Fase 1.5 — escopo de bloco.** Compilador emite drops (dec) na saída de
  blocos para locais compostos que morrem ali (ele conhece tipos e escopos).
  Elimina `do let b = a end; a[i] = x` em laço.
- **Fase 2 — último uso (Perceus-lite).** Drops movidos do fim do escopo
  para o último uso. Elimina a forma restante: `let b = a; a[i] = x` com `b`
  nunca lido depois. Otimização pura sobre a mesma arquitetura.

Fases 1.5 e 2 só mexem em *onde* o compilador emite decs; o modelo de
runtime não muda mais depois da fase 1.

## 6. Performance: previsões e critérios de aceitação

Benchmarks novos (entram na suíte e no RESULTS.md):

- `bench_value_call_mutate.nx`: helper **por valor** em laço de mutação com
  map crescendo (espelho do `bench_typed_call_map`, que usa `ref`). Hoje:
  quadrático (~4,7s @ N=4000). **Aceite fase 1: flat, no nível da variante
  sem helper (~130ms), e `cloneCount` O(1) no laço.**
- `bench_alias_scope_dies.nx`: `do let b = a end` + mutação de `a` em laço.
  **Aceite fase 1.5: flat.**
- (fase 2) variante com `let b = a` sem bloco e `b` não lido. **Aceite: flat.**

Critérios globais (mesmo arnês do PR #31):

1. Suíte intercalada (`interleaved_compare.ps1`): benches que não são alvo
   ficam **≤ ~5%** (o overhead novo é inc/dec O(aridade/locais compostos)
   por chamada — é aqui que ele é julgado).
2. Corpus de exemplos: **saídas idênticas** binário antigo × novo.
3. `go test ./...` verde; testes de caracterização de independência
   inalterados; mudanças de contagem de clones revisadas caso a caso (§3).

## 7. Testes

- **Red tests da fase 1 (TDD):** laço chamada-por-valor + mutação com
  `cloneCount` ancorado em O(1); `Owners` volta a 1 após retorno; retorno do
  próprio parâmetro mantém independência (§4.4); passagem aninhada do mesmo
  objeto por valor em profundidade k decrementa corretamente no unwind das
  k chamadas.
- **Semântica preservada:** mutação de global que aliasa o argumento durante
  a chamada (o argumento continua contando o slot do caller → clone
  acontece); canais entregam valor independente; `defer` vê o valor da
  captura.
- **Unwind:** erro no meio da chamada passa pelo funil de dec
  (`finishFrame`); teste com erro + retry em laço não acumula clones.
- **Concorrência:** os testes existentes de tasks/canais + `-race` (contador
  atômico).

## 8. Riscos

1. **Dec a mais (unsound).** Mitigação: direção segura por desenho (incs
   completos primeiro; decs só nos funis centrais da fase 1), arnês de
   independência já existente, corpus de 130 programas com diff de saída, e
   revisão caso-a-caso de qualquer teste de clones que mude.
2. **Slots duplicados na lista `Owned`.** O mesmo slot listado duas vezes
   causaria release dobrado do ocupante final (dec a menos — unsound).
   Mitigação: inserção com verificação de presença (lista é pequena,
   varredura linear) e teste dedicado de reatribuição sobre slot possuído.
3. **Overhead do inc/dec.** O(compostos vinculados) por chamada; julgado
   pelo critério ≤ ~5% nos benches neutros. Se estourar, fase 2 (drops
   precisos) e elisão de pares inc/dec no mesmo bytecode são as válvulas.
4. **Contenção atômica.** Contadores quentes compartilhados entre tasks são
   raros (travessia de fronteira já copia por semântica); `-race` + bench
   `spawn_sum` cobrem.
5. **Inflação por unwind (fases 1.5/2).** Drops emitidos pelo compilador são
   pulados por unwind → contador inflado → cópias extras, nunca corrupção.
   Aceito e documentado; o funil da fase 1 minimiza a janela.

## 9. Fora de escopo

- **Estruturas persistentes (HAMT):** atacam snapshots *vivos* (outra
  família de custo), ao preço de piorar constantes do caso comum. Só com
  evidência de workload real.
- **Inferência estática de borrow:** com RC preciso ela vira só elisão de
  inc/dec (otimização de fase 2+), não mecanismo de correção.
- **Gerência de memória:** o contador não libera nada; GC do Go permanece o
  dono da memória.
- **Mudanças de linguagem:** nenhuma sintaxe nova (`borrowing`/`consuming` à
  la Swift ficam como possibilidade futura, não necessidade).
