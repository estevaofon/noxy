# Dados brutos — campo de struct por índice em compilação (issue #96): base × head

**Data:** 2026-08-28 · Windows 11 · Intel Core 7 150U · pwsh 7.6.5 · laptop, na
tomada. Máquina sem `go test` nem build durante as medições (o runner do
corpus e o `compare_examples.ps1` rodaram ANTES do A/B, nunca junto).

**Binários** (em disco local, scratchpad da sessão):

| rótulo | commit | o que é |
|---|---|---|
| `base` | 2a627bc | `develop` depois de #95 (slots), #97 (corpus) e #98 (#93a) |
| `head` | 79af956 | + `OP_GET_FIELD` / `OP_SET_FIELD` / `OP_GET_FIELD_MUT` emitidos para base tipada; funis `getPropertyGeneric`/`setPropertyGeneric`/`getPropMutGeneric` |

Spec: `docs/superpowers/specs/2026-08-28-vm-perf-issue-96-struct-field-index-design.md`.

**Protocolo:** `benchmarks/interleaved_compare.ps1 -Runs 9` (`pwsh -NoProfile
-File`), execuções intercaladas na mesma janela, guard de `CHECKSUM:` entre
binários, mediana de 9. `bench_struct_records.nx` entrou na suíte neste PR
(993b464, resgatado de 93625ac — o comentário de fechamento da #40 registra que
o original nunca foi commitado).

## 1. Headline — base × head (mediana de 9)

| bench | base_2a627bc_ms | head_79af956_ms | delta |
|---|---|---|---|
| bench_bubblesort.nx | 659.8 | 655.6 | -0.6% |
| bench_call_light.nx | 27 | 27 | 0% |
| bench_call_readonly.nx | 448.3 | 450.9 | 0.6% |
| bench_call_ref.nx | 995.5 | 999.5 | 0.4% |
| bench_conway.nx | 1212.1 | 1196.3 | -1.3% |
| bench_generic_vs_hand.nx | 419.7 | 420.6 | 0.2% |
| bench_map_churn.nx | 210.2 | 208.7 | -0.7% |
| bench_path_update.nx | 238.8 | 160.7 | **-32.7%** |
| bench_share_mutate.nx | 111.9 | 109.7 | -2% |
| bench_spawn_sum.nx | 411.6 | 414.7 | 0.8% |
| bench_struct_records.nx | 145.9 | 136.1 | **-6.7%** |
| bench_typed_call_map.nx | 27.6 | 28.4 | 2.9% |
| bench_value_call_mutate.nx | 27.9 | 27.8 | -0.4% |

Pulados pelo guard de equivalência — **nenhum dos dois binários** imprime
`CHECKSUM:` neles (pré-existente, não é deste PR): `bench_borrow_path.nx`
(imprime `200000` duas vezes) e `bench_hash31_bytes.nx` (imprime
`bytes=… hash31=… ms=…`).

## 2. Bytecode dos benches (`noxy -disassembly`)

| bench | opcodes de campo no head |
|---|---|
| bench_path_update | 4× `OP_GET_FIELD`, 2× `OP_SET_FIELD`; nenhum `OP_GET_PROPERTY`/`OP_SET_PROPERTY` |
| bench_struct_records | 9× `OP_GET_FIELD`; nenhum por nome |

## 3. Perfis (`noxy --cpuprofile`, carga ampliada para juntar amostra, uma execução)

`prof_path_update.nx` = `bench_path_update` com `round < 5000` (×10);
`prof_struct_records.nx` = `bench_struct_records` com `i < 900000` (×15).
`go tool pprof -top`.

### 3.1 `bench_path_update` ×10

**Base 2a627bc** — 2,18 s, 2,11 s de amostras:

| símbolo | flat | cum |
|---|---|---|
| `(*VM).run` | 47,4 % | 100 % |
| `push` + `pop` | 8,5 + 6,6 % | 15,2 % |
| `value.Retain` + `value.Release` | 5,2 + 2,4 % | 6,6 + 3,3 % |
| `value.IsShared` (+ `ownersOf`) | 4,7 % (+ 5,2 %) | 7,6 % |
| **`(*ObjStruct).FieldIndex`** | 0,9 % | **15,2 %** |
| ↳ `runtime.mapaccess2_faststr` | 2,8 % | 14,2 % |
| ↳ `aeshashbody` | 4,3 % | 4,3 % |
| ↳ `memequal` + `memeqbody` | 1,9 + 1,4 % | — |
| `(*ObjInstance).Get` | 1,4 % | 11,4 % |

**Head 79af956** — 1,44 s, 1,41 s de amostras:

| símbolo | flat | cum |
|---|---|---|
| `(*VM).run` | 68,1 % | 99,3 % |
| `value.IsShared` (+ `ownersOf`) | 9,9 % (+ 6,4 %) | 16,3 % |
| `push` + `pop` | 5,0 + 1,4 % | 6,4 % |
| `memeqbody` (a guarda `Fields[idx] == nome`) | 2,8 % | 2,8 % |
| `unicizeOwnedSlot` | 2,8 % | 5,0 % |
| `FieldIndex` / `mapaccess2_faststr` / `aeshashbody` / `Retain` / `Release` | — | **ausentes** |

Leitura: o hashing de campo (15 %) e as chamadas `Retain`/`Release` (10 %, no-op
para `int`) saíram do caminho; o que sobra é `IsShared` do `OP_GET_LOCAL_MUT`
+ `OP_GET_INDEX_MUT` da cadeia de mutação (`cells[i].hits = …`), fora do
escopo desta issue.

### 3.2 `bench_struct_records` ×15

**Base 2a627bc** — 1,76 s, 2,09 s de amostras:

| símbolo | flat | cum |
|---|---|---|
| `(*VM).run` | 10,5 % | 78,0 % |
| **`validateStructConstructorArguments`** | 1,4 % | **45,9 %** |
| ↳ `runtimeTypeComplete` | 2,4 % | 40,7 % |
| ↳ `runtime.mapassign_fast64ptr` (o `make(map)` de memoização, por construção) | 1,9 % | 27,3 % |
| `callPreparedValue` | 1,9 % | 7,2 % |
| `FieldIndex` / `mapaccess2_faststr` | — | não aparecem no top-22 |

**Head 79af956** — 1,56 s, 1,77 s de amostras:

| símbolo | flat | cum |
|---|---|---|
| `(*VM).run` | 7,9 % | 80,8 % |
| `validateStructConstructorArguments` | 1,7 % | 46,9 % |
| ↳ `runtimeTypeComplete` | 1,7 % | 39,0 % |
| `FieldIndex` / `mapaccess2_faststr` / `aeshashbody` / `(*ObjInstance).Get` | — | **ausentes** (grep em `-nodecount=200`) |

Leitura: neste bench o índice de campo só acelera as leituras de `pontuar`/
`promover` (−6,7 %); o custo dominante é a validação do construtor, que refaz
`runtimeTypeComplete(schema, make(map…))` a cada `Registro(...)`. O cache do
construtor do PR #70 (`ctorCache` em `ObjStruct`, commit 131c352) **não está em
`develop`**: o #70 foi mergeado em `perf/issue-66-call-protocol` 23 s depois de
o #69 entrar em `develop` (`git merge-base --is-ancestor 131c352 develop` →
não). Follow-up próprio.

## 4. Verificação

- `go test ./...` verde; `go test -race ./internal/vm -count=1` verde (24 s).
- Corpus `run_all_tests_concurrent.nx` com o head: **180/180** (3,1 s).
- `compare_examples.ps1` base × head: **149 iguais, 0 divergentes, 70 excluídos**.
- gofmt limpo nos arquivos tocados (checado em cópias sem CR).
- Mutação: sem a guarda `Fields[idx] == nome`, `TestFieldIndexGuardsReorderedJSONInstance`
  falha (`n.valor` lê o slot de `proximo`).
