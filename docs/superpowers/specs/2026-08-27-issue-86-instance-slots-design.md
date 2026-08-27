# Campos de instância como slots indexados pela declaração (issue #86)

**Data:** 2026-08-27 · **Branch:** `fix/issue-86-instance-slots`, a partir de `develop` (pós #94, v0.20.0)
**Status:** implementado neste PR · **Issue:** [#86](https://github.com/estevaofon/noxy/issues/86) · **Relação:** #92 (caminho de alcance sem `ref` visível), #93 (`mapaccess2_faststr` em 16 % do perfil do empréstimo aninhado), item 4 de `2026-08-17-vm-perf-static-typing-research.md` ("structs por índice — plano futuro").

## 1. O problema

`ObjInstance.Fields` é `map[string]Value` cru. `OP_GET_PROPERTY` lê e `OP_SET_PROPERTY` escreve nele sem sincronização; duas routines que alcancem a mesma instância — por `ref`, global, upvalue, ou por um campo `ref` dentro de um valor passado por valor (#92) — batem no map do Go concorrentemente. Resultado não é panic: é `fatal error: concurrent map read and map write`, que nem `call_result` nem `spawn_task` seguram — o processo inteiro morre (reproduzido 3/3 com o programa da #92; `go test -race` acusa `DATA RACE` em `executor.go:1466` × `:1540`).

A CI não pegava porque o passo `-race` de `network-deadlines.yml` filtra por `-run 'Test.*(Network|…)'` e o teste que expõe a corrida não casa com o filtro.

## 2. O que a linguagem promete

`docs/concurrency.md` (l. 15–17) e a spec §2.2: **maps e globais** têm operações individuais sincronizadas ("safe from the Go runtime's concurrent-map crash"); **arrays, structs e compostos aninhados não são recursivamente sincronizados** — coordene com channels. Ou seja, a doc já coloca struct no mesmo status de array: sem sincronização por operação, mas a barra mínima é **não derrubar o runtime Go**. É essa barra que `ObjInstance` violava.

## 3. Decisão: slots, não mutex

| | Slice de slots (adotado) | `RWMutex` por instância (como `ObjMap`) |
|---|---|---|
| `fatal error` de map | some (não há map por instância) | some |
| Corrida em campo | continua corrida (como `ObjArray.Elements`) — documentada | operação individual atômica |
| Custo no caminho quente | igual ao atual: 1 lookup em map **só-leitura** (nome→índice, em `ObjStruct`) + índice em slice | + RLock/RUnlock em **todo** `OP_GET_PROPERTY`, mais 24 B por instância |
| Promessa da doc | exatamente a existente (struct = array) | mais do que a doc promete |
| Futuro | base do "structs por índice" (índice resolvido em compilação, `OP_GET_FIELD idx`) — item 4 da pesquisa de perf | nada |

Um mutex por instância seria a única forma de dar a structs a garantia dos maps; mas a série #66 acabou de ganhar 11 % nesse caminho e a doc não promete isso. Slots cumprem a barra documentada sem regressão, e removem uma classe inteira de crash.

## 4. Desenho

```go
type ObjStruct struct {
    Name   string
    Fields []string            // ordem de declaração = ordem dos slots
    index  map[string]int      // nome → slot; construído UMA vez (NewStruct / BuildFieldIndex); só leitura depois
    …
}
func (os *ObjStruct) FieldIndex(name string) (int, bool)   // index se existir, senão varredura linear (literais de teste)
func (os *ObjStruct) HasField(name string) bool             // = FieldIndex ok

type ObjInstance struct {
    ObjHeader
    Struct *ObjStruct
    Slots  []Value             // len == len(Struct.Fields); null enquanto não preenchido
}
func (oi *ObjInstance) Get(name string) (Value, bool)      // false só para nome NÃO declarado
func (oi *ObjInstance) Field(name string) Value            // null para nome não declarado (leitura em native/teste)
func (oi *ObjInstance) Set(name string, v Value) bool      // false para nome não declarado; NÃO retém (RC é do chamador)
func (oi *ObjInstance) MustSet(name string, v Value)       // panic em nome não declarado: bug do native Go, não erro do usuário
func (oi *ObjInstance) Len() int
func (oi *ObjInstance) Range(fn func(name string, v Value))   // ordem de declaração
func (oi *ObjInstance) Snapshot() map[string]Value
```

Construtores: `NewInstance(def)` (todos os slots `null`), `NewInstanceWith(def, map)` (retém e posiciona por nome; nome não declarado → panic), `NewInstanceAdopting(def, slots []Value)` (move, para o clone CoW — `calls.go:copyValue`).

**Regras que ficam iguais:** struct nominal de campos fixos (§5) — escrever nome não declarado continua `undefined property` (agora é `FieldIndex` falhando, não um `HasField` no caminho frio); invariante do slot `ref` (`RefFields`); RC/CoW (`Retain`/`Release` nos mesmos pontos); `valuesEqual` estrutural (agora por índice, mesmo `Struct`).

**O que muda de observável:**
- Um native que deixe um campo por preencher: o campo lê `null` em vez de `undefined property`. Só acontece com bug em código Go; o construtor de bytecode sempre preenche todos.
- Iteração de campos (`Range`) passa a ser em **ordem de declaração** — `json_dumps` de instância e `Snapshot` ficam determinísticos.
- `len(instance)` deixa de refletir "quantos campos foram preenchidos".

## 5. Sites tocados

`grep -rn "\.Fields" internal --include=*.go` (excluindo `Struct.Fields`/`schema.Fields`): 176 em código, 172 em testes. Executor (8), `references.go` (4), `calls.go` (construtor + clone), `stack.go` (igualdade), `runtime_type_validation.go`, `json_population.go` (substituição do map inteiro → slots), `builtins_json.go` (serialização), natives que montam instâncias (`time`, `io`, `sqlite`, `sys`, `net`: `inst.Fields["x"] = v` → `inst.MustSet("x", v)`; `inst.Fields["x"]` → `inst.Field("x")`).

## 6. Testes

- `instance_slots_crash_test.go` (`//go:build !race`): dois `spawn_task` escrevendo o mesmo `ObjInstance` via campo `ref` (programa da #92) terminam sem derrubar o processo. Antes: `fatal error` mata o binário de teste. Sob `-race` a corrida continua sendo corrida — o arquivo fica fora do detector de propósito.
- `instance_slots_test.go`: `Get`/`Set`/`MustSet`/`Range`, nome não declarado, `null` em slot não preenchido, clone CoW por slots com `Retain`, igualdade por índice, `json_dumps` em ordem de declaração.
- Suíte inteira + `run_all_tests_concurrent.nx` + bench A/B (`bench_path_update`, `bench_conway`, `bench_borrow_path`): critério = sem regressão fora do ruído.

## 7. CI

`network-deadlines.yml`: o passo `-race` passa a rodar `go test -race ./internal/vm -count=1 -timeout=600s` inteiro (28 s local). O filtro por nome sai.

## 8. Docs

Spec §2.2 "Concurrency and composite values" e `concurrency.md`: struct fields — como elementos de array — não derrubam o runtime Go, mas uma leitura concorrente a uma escrita continua sendo corrida (valor rasgado é possível): coordene. `CHANGELOG` no bump.

## 9. Fora de escopo

Índice resolvido em compilação (`OP_GET_FIELD idx`, sem lookup por nome) — item 4 da pesquisa de perf, PR próprio sobre esta base. Sincronização por operação em struct — só se a doc passar a prometer.
