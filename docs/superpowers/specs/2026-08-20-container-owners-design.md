# Construtores de contêiner retêm filhos por padrão (fase A da #54) — Design

> Cópia literal da issue [estevaofon/noxy#55](https://github.com/estevaofon/noxy/issues/55)
> (aberta em 2026-08-20), que é o spec desta entrega. O corpo abaixo é o texto da issue
> no momento em que o plano `docs/superpowers/plans/2026-08-20-container-owners.md` foi
> escrito; a issue continua sendo a fonte de verdade. Relacionadas: #54 (design completo,
> fases B/C adiadas), #53 (itens 1b/1c/1d), #50.

---

## Summary

Fase 1 (e única por ora) da #54, recortada para ser **pequena, fechada e sem ambiguidade**: os construtores de contêiner de `internal/value` passam a **reter os filhos compostos por padrão**, com um construtor "adotante" explícito para os três lugares que entregam filhos já retidos. Isso fecha de uma vez quatro bugs observáveis de semântica de valor (cópia por valor mutando o original) sem tocar em opcode, em `json_loads` (#53 item 1) nem em API de mutação.

- **Regra do runtime que este PR faz valer nos natives:** *todo contêiner (array, map, instância) é dono durável de cada filho composto que guarda* — o bytecode já faz isso (`OP_ARRAY`/`OP_MAP` retêm cada elemento, o construtor de struct retém cada campo, `executor.go`), os natives esquecem.
- **Por que nos construtores:** `value.Retain` em escalar é no-op (`internal/value/cow.go:ownersOf` só rastreia array/map/instância), então reter dentro de `NewArray`/`NewMapWithData` é correto para **todos** os ~30 call sites sem allowlist — inclusive `internal/plugin`, que não tem `*VM`.
- **Bugs que isto corrige** (todos reproduzidos no `develop` `1680266`, programas abaixo): `slice`, envelope do `sqlite.query` (`rows`/`columns`/`values`), envelope do `task_await` (`value`/`error`), `io` (`read_lines` → `data`), `strings` (`parts`), e qualquer plugin que devolva composto aninhado (`InterfaceToValue`).

## Escopo — faça exatamente isto

### 1. `internal/value/value.go`

```go
// NewArray cria um array que e DONO DURAVEL de cada elemento composto
// (Retain; no-op em escalares) — a mesma regra de OP_ARRAY no executor.
func NewArray(elements []Value) Value {
	for _, element := range elements {
		Retain(element)
	}
	return Value{Type: VAL_OBJ, Obj: &ObjArray{Elements: elements}}
}

// NewArrayAdopting cria um array ADOTANDO elementos que o chamador JA reteve
// em nome do array (move) — nao retem de novo. Uso restrito aos tres sites
// listados na issue; qualquer outro uso precisa de comentario `// RC: move`.
func NewArrayAdopting(elements []Value) Value {
	return Value{Type: VAL_OBJ, Obj: &ObjArray{Elements: elements}}
}

// NewMapWithData cria um map que e DONO DURAVEL de cada valor composto.
func NewMapWithData(data map[string]Value) Value { /* como hoje + Retain(v) por valor */ }

// NewInstanceWith cria uma instancia com os campos dados, retendo cada valor
// composto — a mesma regra do construtor de struct (calls.go:callPreparedValue).
func NewInstanceWith(def *ObjStruct, fields map[string]Value) Value
```

`NewMap()` e `NewInstance(def)` (vazios) **não mudam**. `ObjMap.Set/Replace`, `ObjArray.Elements`, `ObjInstance.Fields` **não mudam** (fora de escopo — #54 fase C).

### 2. Os três **moves** (filhos já retidos por quem entrega) → `NewArrayAdopting`, mantendo o retain manual que já existe

| Site | Hoje | Depois |
|---|---|---|
| `internal/vm/executor.go` ~l.1302 (`OP_ARRAY`: `value.Retain(elements[i])` no laço e depois `value.NewArray(elements)`) | retain manual + NewArray | retain manual + `NewArrayAdopting(elements)` |
| `internal/vm/calls.go` ~l.170-180 (`copyValue`: `value.Retain(el)` no laço e depois `value.NewArray(newElems)`) | idem | idem |
| `internal/vm/builtins_call_result.go` ~l.340-357 (`causes`: herdados **transferem** o retain do array antigo, irmãos novos recebem `value.Retain(sibling)`; depois `value.NewArray(causes)`) | retain manual dos irmãos + NewArray | retain manual dos irmãos + `NewArrayAdopting(causes)` — os herdados **não** podem ser retidos de novo |

Nenhum outro site é move. Em particular **não** são moves (a nova posse é real, o retain do construtor é o correto): `slice` (`builtins_collections.go:204`, elementos copiados de outro array que continua dono), `network_poller.go:589` (`fixedNetworkSet`), `plugin.go:228/234`, sqlite/tasks/net/convert/io/strings/sys.

### 3. Remover os paliativos que viram redundantes

- `retainingArray`/`retainingMap` (`internal/vm/builtins_call_result.go` ~l.227-239) → apagar; chamadores usam `value.NewArray`/`value.NewMapWithData` direto (`builtins_call_result.go`, `builtins_json.go:186,192`, `json_population.go:334,480`).
- **Não mexer** nos laços `for … { value.Retain(created) }` de `json_population.go` que antecedem `mapObject.Replace(...)` / `instance.Fields` — esses maps/instâncias são construídos por `NewMap()`+`Replace` e `NewInstance()`+escrita, caminhos fora deste PR; os laços continuam corretos.

### 4. Compostos colocados em **campo de instância** por builders de envelope → `NewInstanceWith`

Os únicos sites onde um builder põe um **composto** num campo via escrita crua (sem `Retain`):

| Site | Campo composto |
|---|---|
| `internal/vm/builtins_sqlite.go` ~l.335-350 (`row`: `values`; `result`: `columns`, `rows`) e ~l.436-443 (`columns`/`rows` vazios no erro) | arrays |
| `internal/vm/builtins_io.go` ~l.384-389 (`newIOReadResult`: `data`, que em `newIOLinesResult` é array) | array |
| `internal/vm/builtins_strings.go` ~l.236 (`parts`) | array |

Construir essas instâncias com `value.NewInstanceWith(definition, map[string]value.Value{...})` (retém os compostos; escalares no-op). Builders cujos campos são **todos escalares** (`sqlite` `exec`/`open`, `io` open/close/write, `json_dumps` result, etc.) podem ficar como estão — **não migrar** por "consistência"; diff mínimo.

## Fora de escopo (não fazer neste PR)

- API de mutação (`Mutate*`/`Prepare*`), acessores de escrita (`SetElements`/`SetField`), teste de arquitetura — #54 fases B/C, adiadas.
- CoW do `json_loads` em alvo compartilhado (#53 item 1), benchmarks dos guards (#53 item 2), higiene (#53 item 3), `test_crypto_debug.nx` (#53 item 4).
- Qualquer mudança em opcodes além da troca por `NewArrayAdopting` em `OP_ARRAY`; qualquer mudança em `ObjMap.Set/Replace/Delete`; mover funções de arquivo; renomear `NewMapWithData`.
- Converter `NewMap()`+`Replace` ou `NewInstance()`+escrita em outros builders.

## Reproduções (hoje falham; depois passam) — usar como base dos testes

```noxy
// slice — builtins_collections.go:204
struct Pair
    a: int
    b: int
end
let t: Pair[] = [Pair(0, 0), Pair(1, 1)]
let s: Pair[] = slice(t, 0, 2)
s[0].a = 9
print(t[0].a)                     // hoje 9; esperado 0
```

```noxy
// sqlite — builtins_sqlite.go:341-347
use sqlite select *
use sqlite
let db: Database = sqlite.open(":memory:")
sqlite.exec(db, "CREATE TABLE t (id INTEGER, nome TEXT)")
sqlite.exec(db, "INSERT INTO t VALUES (1, 'a')")
let res: QueryResult = sqlite.query(db, "SELECT * FROM t")
let cols: string[] = res.columns
cols[0] = "ZZZ"
print(res.columns[0])             // hoje ZZZ; esperado id
let vals: any[] = res.rows[0].values
vals[0] = 999
print(res.rows[0].values[0])      // hoje 999; esperado 1
```

```noxy
// task_await — builtins_tasks.go:157-177
func mk() -> int[]
    return [1, 2, 3]
end
let t: any = spawn_task(mk)
let r: any = task_await(t)
let v: any = r["value"]
v[0] = 99
print(r["value"][0])              // hoje 99; esperado 1
```

## Critérios de aceite

- [ ] `internal/value`: testes unitários — `NewArray` deixa elemento composto com `Owners == 1` e não toca escalar; `NewArrayAdopting` deixa `Owners` como recebeu; `NewMapWithData` retém valores compostos; `NewInstanceWith` retém campos compostos.
- [ ] Sem double-retain nos três moves: literal `[Pair(1,1)]` → elemento com `Owners == 1` (sonda `OwnersCount` via native de teste, padrão de `internal/vm/rc_uniqueness_test.go`); `copyValue` (clone CoW) → filho com `Owners == 2`; `call_result` com `causes` herdados — testes existentes `TestCallResultCauseAlias*`/`TestCallResultFailureAlias*` verdes e `Owners` dos herdados inalterado.
- [ ] Testes "cópia por valor não vaza" em `internal/vm`: `slice`, `sqlite.query` (`columns` e `rows[i].values`), `task_await` (`value` e `error`), `io` `read_lines` (`data`), `strings` (`parts`); e em `internal/plugin`: `InterfaceToValue` com array/map aninhado → filho com `Owners == 1`.
- [ ] `CloneCountValue()` não cresce ao mutar um contêiner com dono único vindo desses natives (não introduzimos clone desnecessário).
- [ ] `retainingArray`/`retainingMap` removidos; `grep -rn "retainingArray\|retainingMap" internal` vazio.
- [ ] `go test ./...` verde sem `| tail`; `go vet`; `noxy_examples/run_all_tests_concurrent.nx` 171/171; diff de saída dos exemplos contra o `develop` só com não-determinismo (ordem de map, tempo, rand/uuid, endereços).
- [ ] CHANGELOG `### Fixed` (uma entrada: "contêineres criados por natives/plugins são donos dos filhos — `slice`, `sqlite.query`, `task_await`, `io`, `strings`, plugins") + `AGENTS.md` seção "Adicionar Função Builtin": *construa com `value.NewArray`/`NewMapWithData`/`NewInstanceWith`; só use `NewArrayAdopting` para filhos que você já reteve, com comentário `// RC: move`*.
- [ ] Comentar na #53 que os itens 1b/1c/1d foram fechados aqui; na #54 que a fase A está feita.

## Components
- `internal/value/value.go` (+ testes em `internal/value/`)
- `internal/vm/executor.go` (`OP_ARRAY`), `internal/vm/calls.go` (`copyValue`), `internal/vm/builtins_call_result.go` (`causes`, remoção dos helpers)
- `internal/vm/builtins_json.go`, `internal/vm/json_population.go` (só troca de helper)
- `internal/vm/builtins_sqlite.go`, `internal/vm/builtins_io.go`, `internal/vm/builtins_strings.go` (`NewInstanceWith`)
- `internal/plugin/plugin.go` (sem mudança de código — coberto pelo construtor; só o teste)
- `CHANGELOG.md`, `AGENTS.md`

## Test Plan
- [x] Bugs reproduzidos no `develop` `1680266` (programas acima; `slice`, sqlite, `task_await`).
- [x] Moves identificados por inspeção dos 31 call sites de `NewArray`/`NewMapWithData` em `internal/` (lista fechada acima).
- [ ] Implementar na ordem 1 → 2 → 3 → 4 com a suíte verde a cada commit (TDD: teste de `Owners` primeiro).
- [ ] Verificações do critério de aceite (suíte, runner, diff de exemplos).

Relacionadas: #54 (design completo; este é a fase A, B/C adiadas), #53 (itens 1b/1c/1d), #50 (onde a classe ficou evidente).

