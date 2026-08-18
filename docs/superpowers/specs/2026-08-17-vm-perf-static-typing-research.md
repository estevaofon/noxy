# Pesquisa: performance da VM explorando tipagem estática

**Data:** 2026-08-17 · **Status:** fases 0-3 implementadas e medidas
(`perf/vm-dispatch-fase1`, Tasks 1-8) — ver seção "Baseline pprof (Task 1)" e
"Pprof pós-fase 1 (Task 8)" no fim deste documento. Fases 4-6 seguem como
pesquisa/planos futuros.

**Contexto:** cross-runtime (benchmarks/cross_runtime/results/cross_runtime.md) mostra Noxy
4–10x mais lenta que CPython no trabalho descontado o startup (fib: 742ms vs 93ms;
bubblesort: 604ms vs 63ms; loop_arith: 456ms vs 256ms). Meta: ficar entre Python e Lua.

## Custos identificados no caminho quente (por ordem estimada de impacto)

1. **Globais por nome + mutex** — `OP_GET_GLOBAL` → `Environment.Resolve(name)` →
   `bindingStore.get` (internal/value/map.go): **RLock + lookup em
   `map[interface{}]Value` com boxing de string** a cada acesso. `fib` paga isso a
   CADA chamada recursiva. Fix: resolução de slot de global em compile-time
   (índice em array por módulo) ou inline cache no bytecode; globais do
   interpretador single-thread não precisam de mutex (tasks têm environments
   próprios — verificar fronteira).

2. **Alocação por chamada** — `callPreparedClosure` (internal/vm/calls.go:113):
   `&CallFrame{...}` no heap por chamada + `frame.Owned` (slice) via `ownSlot` por
   parâmetro composto + `validateParameterModes` (loop sobre params, sempre — checa
   ref-mode que o compilador já garantiu em chamadas tipadas in-module). Fix:
   frames em array fixo reusado (`vm.frames` já existe como array de ponteiros —
   tornar array de valores), pool para `Owned`, e flag no `ObjFunction` "call site
   verificado estaticamente" para pular validação.

3. **`Value` gordo + pop que zera** — `Value` = 48 bytes (Type + AsBool + AsInt +
   AsFloat + Obj interface{}); `vm.pop()` zera o slot (48B extra de escrita por pop).
   Os opcodes `_INT` inlined (OP_ADD_INT, OP_LESS_INT, OP_MOD_INT em executor.go)
   também gastam um store zerando. Fix incremental: só limpar slots quando o valor
   é VAL_OBJ/REF (GC-liveness), ou limpar em lote no fim do frame; fix maior
   (arriscado): NaN-boxing ou Value de 24B {Type uint8, bits uint64, Obj unsafe.Pointer}.

4. **Campos de struct por string map** — `ObjInstance.Fields` é
   `map[string]Value`; `OP_GET_PROPERTY`/`OP_SET_PROPERTY` fazem lookup por string.
   Structs são **fechadas e estáticas** (ObjStruct.Fields ordenada) → campos podiam
   ser `[]Value` com índice resolvido em compile-time (`OP_GET_FIELD idx`). O
   compilador conhece o tipo estático do receptor (Compile retorna `ast.Type`).
   Atenção: JSONDynamicFields e cópia rasa em copyValue precisam acompanhar.

5. **`ObjMap` sobre `bindingStore` com mutex** — todo map de usuário paga
   RWMutex por operação + `Len()` faz Snapshot (cópia inteira!) só para contar +
   chave `interface{}` (boxing). Fix: `Len()` sem snapshot é trivial; tipar chave
   (string OU int64, conhecidos em compile-time: `map[string,T]`/`map[int,T]`)
   permite duas implementações sem interface{}; mutex só é necessário quando o
   map cruza fronteira de task (Owners/atomics já existem como precedente).

6. **Opcodes `_INT` subemitidos** — compilador só emite `_INT` em BinaryExpr com
   ambos os lados int (compiler.go:930-1018). Não há: `OP_ADD_FLOAT` etc.,
   incremento fundido (`i = i + 1` vira GET_LOCAL/CONST/ADD_INT/SET_LOCAL — 4
   dispatches; um `OP_INC_LOCAL_INT slot` faria 1), comparação+jump fundido
   (`OP_JUMP_IF_LESS_INT`), nem `OP_GET_INDEX_INT` (indexação de array com índice
   int estático, sem checar tipo do container — bubblesort é isso em loop).

7. **`OP_CONSTANT` checa VAL_FUNCTION toda vez** — branch por constante carregada
   (executor.go:76). Fix: opcode dedicado `OP_FUNCTION` emitido pelo compilador
   quando a constante é função; OP_CONSTANT vira load puro.

8. **Validação runtime de tipos por chamada** — runtime_type_validation.go (536
   linhas), O(n)/chamada para maps/structs/refs em fronteiras tipadas (2x por
   marcador, só in-module) — já tem spike O(1)-por-tag validado (memória do
   projeto; ver docs/superpowers/specs/ do CoW-RC).

## Infraestrutura já existente (aproveitar)

- `c.Compile(node)` retorna `(chunk, ast.Type, error)` — tipo estático disponível
  em todo ponto de emissão.
- Precedente de opcodes especializados e de gates de benchmark (benchmarks/RESULTS.md,
  protocolo intercalado, mediana de 9, corpus 130/130, `-race` em internal/value e
  internal/vm).
- `noxy_develop.exe` vs `noxy.exe` para comparação A/B; scripts
  benchmarks/*.ps1 e benchmarks/cross_runtime/run_cross_runtime.ps1.

## Restrições

- RC-uniqueness (Owners atômico) é recente e delicado: qualquer mudança em
  push/pop/frames não pode alterar os funis retain/release (spec §4.2 do CoW-RC).
- Gates: benches rastreados não podem regredir >5%; corpus de exemplos
  164/164 (número corrigido de 130 no commit `e7c533d`);
  `go test ./...` e `-race` verdes.
- Semântica não muda: mesmos outputs, mesmos erros em runtime (mensagens iguais).

## Forma sugerida do plano (fases, cada uma com A/B intercalado)

0. ✅ **Implementada** (Task 1, commit `60ed8d7`). Flag `-cpuprofile`/`-memprofile` no CLI + baseline pprof de fib/loop_arith/bubblesort.
1. ✅ **Implementada** (Task 2, commit `67e9d52`). Globais por slot/cache + remover mutex do caminho single-task (alvo: fib).
2. ✅ **Implementada** (Tasks 3-4, commits `bb8a773`, `1ca26e7`+`d460e5a`). Chamadas: frame sem alocação (allocs/chamada 1012→10) + pular validateParameterModes estaticamente provada (`OP_CALL_STATIC`) (alvo: fib).
3. ✅ **Implementada** (Tasks 5-7, commits `c43276c`+`32b33c7`, `56882ca`, `4d43cdf`+`6d991e5`). Fusões int: OP_INC_LOCAL_INT, seis opcodes de comparação+salto inteiro fundidos, variantes FLOAT (alvo: loop_arith, mandelbrot).

Resultado agregado das fases 0-3 (Task 8, ver `benchmarks/RESULTS.md` e
`benchmarks/cross_runtime/results/cross_runtime.md`): `fib` 804,9ms →
380,8ms (−52,7%, ~2,1x), `loop_arith` 519,4ms → 319,4ms (−38,5%, ~1,63x),
`mandelbrot` 428,6ms → 246,2ms (−42,6%, ~1,74x) — todos além da estimativa
original de cada task. Nos benches CoW (fora do escopo original, mas gates
de regressão), 9 de 11 melhoraram (alguns >30%) porque o custo de chamada
in-module e o incremento fundido de laço atravessam qualquer programa nesse
formato; `bench_share_mutate` excedeu o gate de 5% (ver `benchmarks/RESULTS.md`,
seção "develop (f107508) × fase 1 de dispatch e chamadas" — gate reprovado,
não corrigido, decisão do controller).

4. **Não implementada** — plano futuro. Structs por índice (alvo: bench_path_update, conway). Não confirmada por profile desta fase: nem `fib` nem `bench_conway` usam structs, então nenhum profile coletado até aqui sustenta ou refuta esse alvo diretamente — continua válido pela leitura de código original (`ObjInstance.Fields` como `map[string]Value`).
5. **Não implementada** — plano futuro. Maps tipados + Len() O(1) (alvo: map_churn). Mesma ressalva: não re-confirmada por profile nesta rodada (o profile pós-fase coletado foi de `fib`, que não usa maps).
6. **Próximo alvo indicado pelo profile pós-fase 1** (ver seção "Pprof pós-fase 1 (Task 8)" abaixo). Pop sem zerar escalares / Value layout — era "maior risco, só com pprof provando"; o pprof pós-fase agora mostra `push`+`pop` como 24,4% do tempo de `fib` (14,63%+9,76%), a maior fração isolada depois do dispatch puro (`(*VM).run`, 43,90%), com o custo de globais e alocação de chamada já cortado. Essa é a prova que o item pedia.

## Baseline pprof (Task 1)

**Data:** 2026-08-18 · **Commit (VM/develop na base do branch `perf/vm-dispatch-fase1`):**
`f107508827f8cd20fbf3b56531dcaec837d272d7` (o único diff do branch neste ponto é
`cmd/noxy/main.go` — flags `--cpuprofile`/`--memprofile`; nenhum código de VM mudou,
então o binário profilado (`noxy_perf.exe`) reflete o mesmo caminho de execução do
`develop`).

Coletado com:
```powershell
.\noxy_perf.exe --cpuprofile=fib.prof benchmarks\cross_runtime\fib.nx
.\noxy_perf.exe --cpuprofile=loop.prof benchmarks\cross_runtime\loop_arith.nx
.\noxy_perf.exe --cpuprofile=bubble.prof benchmarks\cross_runtime\bubblesort.nx
```

### fib.nx — `CHECKSUM:832040`

```
File: noxy_perf.exe
Type: cpu
Duration: 938.22ms, Total samples = 870ms (92.73%)
Showing nodes accounting for 690ms, 79.31% of 870ms total
Showing top 15 nodes out of 96
      flat  flat%   sum%        cum   cum%
     280ms 32.18% 32.18%      750ms 86.21%  noxy-vm/internal/vm.(*VM).run
     120ms 13.79% 45.98%      120ms 13.79%  noxy-vm/internal/vm.(*VM).pop
      40ms  4.60% 50.57%       40ms  4.60%  sync/atomic.(*Int32).Add (inline)
      30ms  3.45% 54.02%      120ms 13.79%  noxy-vm/internal/vm.(*VM).callPreparedClosure
      30ms  3.45% 57.47%       30ms  3.45%  noxy-vm/internal/vm.(*VM).finalizeCurrentFrame
      20ms  2.30% 59.77%      130ms 14.94%  noxy-vm/internal/value.(*GlobalEnvironment).Resolve
      20ms  2.30% 62.07%      150ms 17.24%  noxy-vm/internal/vm.(*VM).call
      20ms  2.30% 64.37%       20ms  2.30%  noxy-vm/internal/vm.(*VM).push (inline)
      20ms  2.30% 66.67%       60ms  6.90%  runtime.mallocgcSmallScanNoHeader
      20ms  2.30% 68.97%       20ms  2.30%  runtime.memclrNoHeapPointers
      20ms  2.30% 71.26%       40ms  4.60%  runtime.nilinterequal
      20ms  2.30% 73.56%       20ms  2.30%  runtime.stdcall1
      20ms  2.30% 75.86%       20ms  2.30%  runtime.stdcall2
      20ms  2.30% 78.16%       30ms  3.45%  runtime.typePointers.next
      10ms  1.15% 79.31%      100ms 11.49%  noxy-vm/internal/value.(*bindingStore).get
```

### loop_arith.nx — `CHECKSUM:135`

```
File: noxy_perf.exe
Type: cpu
Duration: 634.93ms, Total samples = 510ms (80.32%)
Showing nodes accounting for 510ms, 100% of 510ms total
Showing top 15 nodes out of 42
      flat  flat%   sum%        cum   cum%
     310ms 60.78% 60.78%      470ms 92.16%  noxy-vm/internal/vm.(*VM).run
      60ms 11.76% 72.55%       60ms 11.76%  noxy-vm/internal/value.Release
      40ms  7.84% 80.39%       40ms  7.84%  noxy-vm/internal/vm.(*VM).pop
      40ms  7.84% 88.24%       40ms  7.84%  noxy-vm/internal/vm.(*VM).push (inline)
      30ms  5.88% 94.12%       30ms  5.88%  runtime.cgocall
      10ms  1.96% 96.08%       10ms  1.96%  noxy-vm/internal/value.Retain (inline)
      10ms  1.96% 98.04%       20ms  3.92%  noxy-vm/internal/vm.(*CallFrame).ownSlot
      10ms  1.96%   100%       10ms  1.96%  runtime.stdcall1
         0     0%   100%       30ms  5.88%  internal/syscall/windows/registry.Key.GetMUIStringValue
         0     0%   100%       30ms  5.88%  internal/syscall/windows/registry.regLoadMUIString
         0     0%   100%      470ms 92.16%  main.main
         0     0%   100%      470ms 92.16%  main.runWithConfig
         0     0%   100%      470ms 92.16%  noxy-vm/internal/vm.(*VM).Interpret (inline)
         0     0%   100%      470ms 92.16%  noxy-vm/internal/vm.(*VM).InterpretWithEnvironment
         0     0%   100%       10ms  1.96%  runtime.gopreempt_m
```

### bubblesort.nx — `CHECKSUM:376520193`

```
File: noxy_perf.exe
Type: cpu
Duration: 843.66ms, Total samples = 680ms (80.60%)
Showing nodes accounting for 630ms, 92.65% of 680ms total
Showing top 15 nodes out of 74
      flat  flat%   sum%        cum   cum%
     320ms 47.06% 47.06%      620ms 91.18%  noxy-vm/internal/vm.(*VM).run
      40ms  5.88% 52.94%       40ms  5.88%  sync/atomic.(*Int32).Add (inline)
      30ms  4.41% 57.35%       30ms  4.41%  noxy-vm/internal/value.IsShared (partial-inline)
      30ms  4.41% 61.76%       40ms  5.88%  noxy-vm/internal/vm.(*VM).pop
      30ms  4.41% 66.18%       30ms  4.41%  noxy-vm/internal/vm.(*VM).push (inline)
      30ms  4.41% 70.59%       30ms  4.41%  noxy-vm/internal/vm.extractReferenceValue
      30ms  4.41% 75.00%       30ms  4.41%  runtime.cgocall
      20ms  2.94% 77.94%      100ms 14.71%  noxy-vm/internal/vm.(*VM).referenceStorage
      20ms  2.94% 80.88%      120ms 17.65%  noxy-vm/internal/vm.(*VM).resolveReferenceValue
      20ms  2.94% 83.82%       60ms  8.82%  noxy-vm/internal/vm.(*VM).unicizeThroughRefValue
      20ms  2.94% 86.76%       40ms  5.88%  runtime.mallocgc
      10ms  1.47% 88.24%       10ms  1.47%  noxy-vm/internal/value.Release
      10ms  1.47% 89.71%       10ms  1.47%  noxy-vm/internal/value.ownersOf (inline)
      10ms  1.47% 91.18%       10ms  1.47%  runtime.bulkBarrierPreWrite
      10ms  1.47% 92.65%       10ms  1.47%  runtime.gogo
```

### Leitura vs. diagnóstico

- **Confirma custo #1 (globais):** em `fib`, `(*GlobalEnvironment).Resolve` (14.94%
  cum) e `(*bindingStore).get` (11.49% cum) aparecem no top 15 — a chamada
  recursiva de `fib` de fato paga resolução de global a cada invocação, como
  previsto. `bindingStore.mu` é `sync.RWMutex` (confirmado em
  `internal/value/map.go:6`); o lock/unlock em si não aparece como linha própria
  no top — provável inlining/custo pequeno demais para o bucket de 10ms da
  amostragem, não evidência de que o mutex tenha sumido.
- **Confirma custo #2 (chamadas):** `(*VM).call` (17.24% cum) e
  `callPreparedClosure` (13.79% cum, sobrepostos) somam a segunda maior fatia
  depois do dispatch puro em `(*VM).run`; `runtime.mallocgcSmallScanNoHeader`
  (6.90% cum) aparece consistente com alocação de `CallFrame`/`Owned` por
  chamada.
- **`loop_arith` não usa globais** (nenhuma entrada de `Resolve`/`bindingStore`
  no top 15) — esperado, o benchmark opera só sobre locais em loop apertado;
  aqui o custo dominante é dispatch (`(*VM).run`, 60.78% flat) e
  `value.Release`/push/pop, alinhado ao item #3 (Value gordo, zerado em pop).
- **`bubblesort` é dominado por caminho de referência/CoW**
  (`resolveReferenceValue` 17.65% cum, `referenceStorage` 14.71% cum,
  `unicizeThroughRefValue` 8.82% cum, `IsShared`) — não é nenhum dos itens #1/#2
  isoladamente, é o custo de indexação+troca de elementos de array via
  referência. Não contradiz o diagnóstico (bubblesort não é o alvo de fase 1;
  plano já mira fib para #1/#2), mas confirma que otimizar só globais+chamadas
  não vai mover bubblesort — consistente com a fase 3/4 do plano (fusão de
  opcodes int / structs por índice) serem os alvos certos para esse bench.
- **Nenhuma contradição que exija reordenar as tasks seguintes.** `runtime.mapaccess2`
  não aparece nominalmente no top 15 de nenhum profile — mas os símbolos que o
  envolvem (`bindingStore.get`, `GlobalEnvironment.Resolve`) aparecem com custo
  cumulativo relevante em `fib`, então a hipótese de custo de globais permanece
  válida para orientar a Task seguinte (fase 1: globais por slot).

## Pprof pós-fase 1 (Task 8)

**Data:** 2026-08-18 · Binário: `noxy_perf.exe` no HEAD do branch
(`6d991e5`, fases 0-3 completas: cache de globais, `OP_CALL_STATIC`,
`CallFrame` sem alocação, opcodes fundidos int/float). Coletado com:

```powershell
.\noxy_perf.exe --cpuprofile=fib_pos.prof benchmarks\cross_runtime\fib.nx
go tool pprof -top -nodecount=15 noxy_perf.exe fib_pos.prof
```

### fib.nx — `CHECKSUM:832040`

```
File: noxy_perf.exe
Type: cpu
Duration: 531.23ms, Total samples = 410ms (77.18%)
Showing nodes accounting for 410ms, 100% of 410ms total
Showing top 15 nodes out of 36
      flat  flat%   sum%        cum   cum%
     180ms 43.90% 43.90%      390ms 95.12%  noxy-vm/internal/vm.(*VM).run
      60ms 14.63% 58.54%       60ms 14.63%  noxy-vm/internal/vm.(*VM).push (inline)
      40ms  9.76% 68.29%       60ms 14.63%  noxy-vm/internal/vm.(*VM).callPreparedClosure
      40ms  9.76% 78.05%       50ms 12.20%  noxy-vm/internal/vm.(*VM).finalizeCurrentFrame
      40ms  9.76% 87.80%       40ms  9.76%  noxy-vm/internal/vm.(*VM).pop
      20ms  4.88% 92.68%       20ms  4.88%  runtime.cgocall
      10ms  2.44% 95.12%       10ms  2.44%  noxy-vm/internal/value.Retain (inline)
      10ms  2.44% 97.56%       20ms  4.88%  noxy-vm/internal/vm.(*CallFrame).ownSlot
      10ms  2.44%   100%       60ms 14.63%  noxy-vm/internal/vm.(*VM).finishFrame
         0     0%   100%       20ms  4.88%  internal/syscall/windows/registry.Key.GetMUIStringValue
         0     0%   100%       20ms  4.88%  internal/syscall/windows/registry.regLoadMUIString
         0     0%   100%      390ms 95.12%  main.main
         0     0%   100%      390ms 95.12%  main.runWithConfig
         0     0%   100%      390ms 95.12%  noxy-vm/internal/vm.(*VM).Interpret (inline)
         0     0%   100%      390ms 95.12%  noxy-vm/internal/vm.(*VM).InterpretWithEnvironment
```

### Leitura vs. baseline (Task 1)

- **Custo #1 (globais) desapareceu do top 15.** `(*GlobalEnvironment).Resolve`
  (14,94% cum no baseline) e `(*bindingStore).get` (11,49% cum) não aparecem
  mais — nem sequer abaixo do corte. O cache de globais com geração (Task 2)
  removeu o custo por completo do caminho quente de `fib`, não só reduziu.
- **Custo #2 (alocação por chamada) também sumiu.**
  `runtime.mallocgcSmallScanNoHeader` (6,90% cum no baseline),
  `runtime.memclrNoHeapPointers`, `runtime.nilinterequal`,
  `runtime.typePointers.next` — todos ligados a alocação/GC de `CallFrame` —
  não aparecem mais. Consistente com o allocs/chamada 1012→10 medido nas
  Tasks 3-4. `sync/atomic.(*Int32).Add` (4,60% no baseline, plausivelmente o
  retain/release de `Owners` no caminho de chamada) também não aparece mais.
- **`(*VM).pop` — verificação pedida explicitamente.** Caiu de 13,79% flat
  (120ms, 2º maior nó do baseline) para 9,76% flat (40ms, 5º maior nó): tanto
  a fração relativa quanto o tempo absoluto caíram, e a queda absoluta (3x) é
  maior que a queda do tempo total (870ms→410ms amostrados, ~2,1x) — ou seja,
  `pop` ficou mais barato mesmo descontando que o programa inteiro ficou mais
  rápido. O item #3 da pesquisa ("Value gordo + pop que zera") **não foi
  atacado diretamente** nesta fase (nenhuma task tocou o zeramento de slot em
  `pop`); a queda é efeito colateral provável de `fib` agora executar menos
  push/pop por chamada (`OP_CALL_STATIC` evita parte do trabalho que
  `callPreparedClosure`/`validateParameterModes` faziam antes ao redor da
  chamada). `pop` **continua no top 15** e não foi eliminado — o zeramento em
  si (48 bytes por slot) segue intocado.
- **`push` subiu de 2,30% (20ms) para 14,63% (60ms) — ressalva de amostragem.**
  Em termos absolutos `push` triplicou, o que à primeira vista parece uma
  regressão; mas o profile pós-fase tem só 41 amostras de 10ms no total
  (410ms/10ms), então 60ms são 6 amostras — contagem baixa demais para
  distinguir "ficou mais caro" de ruído de amostragem/inlining diferente
  entre os dois binários. Não tratado como achado confiável, só registrado.
- **O que domina agora:** `(*VM).run` (43,90% flat, 95,12% cum) — o dispatch
  puro, que só cai fundindo mais opcodes ou reduzindo o Value por
  instrução — e o cluster de setup/teardown de frame
  (`callPreparedClosure`+`finalizeCurrentFrame`+`finishFrame`+`ownSlot`,
  ~14-25% cum cada, parcialmente sobrepostos) ainda não zerado apesar da
  Task 3-4. `push`+`pop` juntos são 24,4% do tempo total — a maior fração
  isolada depois do dispatch, contra 16,1% no baseline (2,30%+13,79%): com
  globais e alocação cortados, o que sobra sobe de posição relativa mesmo
  sem ficar mais caro em si.

### Próximo alvo indicado por este profile

**Fase 6 do plano (Value layout / pop sem zerar escalar), item #3 da lista de
custos** — não fase 4 (structs por índice) nem fase 5 (maps tipados): `fib`
não usa structs nem maps, então este profile não fala sobre esses dois
alvos, só sobre o dispatch geral e o formato de `Value`. O gate textual da
pesquisa era "medir antes; maior risco, só com pprof provando" — este é esse
pprof: com #1/#2 resolvidos, `push`+`pop` (24,4%) e o cluster de
frame-setup/teardown (ainda não zerado) são o que resta de maior massa em
`fib`. Fase 4/5 continuam válidas como planos futuros para os benches que
elas miravam originalmente (`bench_path_update`/`bench_conway` para structs,
`bench_map_churn` para maps) — mas isso exigiria profiles desses benches
especificamente, não feitos nesta rodada (só `fib` foi perfilado
pós-fase, conforme o Step 3 desta task; `bench_share_mutate` também foi
perfilado à parte, ver `benchmarks/RESULTS.md`, mas é sobre o gate que
regrediu, não sobre o próximo alvo de otimização).
