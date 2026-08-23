# VM Perf — Maps e structs (issue #66, item 4; #40 item 1)

**Data:** 2026-08-22 · **Issues:** [#66](https://github.com/estevaofon/noxy/issues/66) item 4, [#40](https://github.com/estevaofon/noxy/issues/40) · **Branch:** `perf/issue-66-maps-structs` (empilhada sobre `perf/issue-66-call-protocol`, PR #69; base do PR passa a `develop` quando #69 mergear) · **Base de medição:** 4ac27f5 (v0.15.2 = head do item 3)

## 1. Contexto (medido nesta sessão, binário de 4ac27f5)

**Structs** — `struct_records.nx` (o bench da #40, que não existia: struct de 5
campos, 600 k construções em laço, duas funções por valor; 1,78 s de amostras):

| componente | cum | o que é |
|---|---|---|
| `validateStructConstructorArguments` | **35,4 %** | por construção: `validStructConstructorType` → `runtimeTypeComplete` com `make(map[*RuntimeTypeInfo]bool)` (`mapassign_fast64ptr` 22,5 %, `growToTable` 14,6 %), `make([]ParamInfo)` + `String()` por campo |
| `callPreparedValue` | 11,8 % | `NewInstance` = `make(map[string]Value)` **sem tamanho** + 5 `mapassign_faststr` (`growToSmall`/`newTable` 7 %) |
| GC (`gcBgMarkWorker`, `scanObject`…) | ~14 % | pressão das alocações acima |
| `(*VM).run` | 8,4 % flat | o despacho é irrelevante aqui |
| `OP_GET_PROPERTY` (`getWithoutKeySmallFastStr`) | ~2 % | acesso a campo não é o gargalo (como a #40 previa) |

**Maps** — `map_churn.nx` 10x (1,18 s):

| componente | cum | o que é |
|---|---|---|
| `setIndexGeneric` | **28,8 %** | `mapObj.Get(key)` + `mapObj.Set(key, val)`: dois ciclos de RWMutex, dois hashes |
| `getIndexGeneric` / `ObjMap.Get` | 14,4 / 15,3 % | um RLock + hash de `interface{}` |
| `atomic.Int32.Add` | 8,5 % | RLock/RUnlock/Lock/Unlock + `gen.Add` |
| `nilinterhash` + `nilinterequal` | ~9 % | chave `interface{}` |
| `convTstring` | 5 % | **re-boxing da chave string** (`key = str` após `indexVal.Obj.(string)`), que já está boxada em `indexVal.Obj` |
| `callNative` (`to_str`) + `concatstrings` | 19 % | itens 2/3, não map |

Colateral: `ObjMap.Len()` é `len(mapping.Snapshot())` — **copia o map inteiro**
(alocação + RLock + range) para contar; é o que `length(m)` e a validação de
tipo de map chamam.

## 2. Objetivo e não-objetivos

**Objetivo:** tirar do caminho de construção de struct o trabalho que é função
pura de um dado imutável (`ConstructorType`) e as alocações de crescimento do
map de campos; tirar do caminho de `m[k]`/`m[k] = v`/`has_key`/`length` o
segundo ciclo de lock, o re-boxing da chave e a cópia para contar. Semântica,
saída, **mensagens e momento de erro idênticos**; RC idêntico (retain/release
do valor velho do map continua igual); nenhum opcode novo.

**Não-objetivos (registrados com número):** campos de struct por índice
(`ObjInstance.Fields` é `map[string]Value` em 148 sites + campos dinâmicos de
JSON; o perfil dá ~2 % ao acesso); store de map tipado `map[string]Value`
(só com número, depois destes — o RWMutex e o `gen` ficam); `to_str`/concat
(itens 2/3); `OP_CALL` genérico.

## 3. Desenho

### 3.1 (4a) Cache do schema validado do construtor — #40 item 1

Em `internal/value/value.go`:

```go
// ValidatedCtor e o resultado, imutavel, de validStructConstructorType para
// um ObjStruct: o schema aceito e os ParamInfo (IsRef, TypeName) ja prontos.
type ValidatedCtor struct {
	Schema *RuntimeTypeInfo
	Params []ParamInfo
}
// ObjStruct ganha: ctorCache atomic.Pointer[ValidatedCtor]  (+ CtorCache()/StoreCtorCache())
```

`validStructConstructorType(definition)` consulta o cache primeiro: entrada
cuja `Schema == definition.ConstructorType` (guarda contra troca do ponteiro)
devolve `(Schema, true)` sem `runtimeTypeComplete`; senão calcula como hoje e,
**só se válido**, grava. `validateStructConstructorArguments` usa
`cache.Params` em vez de montar `[]ParamInfo` + `String()` por campo. As
checagens por argumento (`validateParameterModes`, `runtimeValueMatchesType`)
continuam iguais — mesmas mensagens. Inválido nunca é cacheado (caminho de
erro, recalcula). `atomic.Pointer` porque structs são lidos por tasks
paralelas (`-race` cobre). Beneficia também `ref_slots.go` e os outros dois
chamadores de `validStructConstructorType`.

### 3.2 (4b) `NewInstance` pré-dimensionado

`make(map[string]Value, len(def.Fields))` em `NewInstance`; `copyValue` de
instância (`calls.go`) idem com `len(obj.Fields)`.

### 3.3 (4c) Maps sem trabalho redundante

- `bindingStore.swap(key, item) (old Value, existed bool)`: uma seção crítica
  (`Lock`, leitura do velho, escrita, `Unlock`, `gen.Add`) — `ObjMap.Swap`.
  `setIndexGeneric` (map) passa a: `old, existed := mapObj.Swap(key, val)`;
  RC **na mesma ordem de hoje**: `Retain(val)` antes do swap; `Release(old)`
  depois, só se `existed`. (Retain-antes-de-release preservado.)
- Chave: `key = indexVal.Obj` quando `indexVal.Obj` é `string` (já é o
  `interface{}` boxado — zero `convTstring`), em `getIndexGeneric`,
  `setIndexGeneric`, `has_key`, `delete` e os outros builtins de map que fazem
  `key = str`. Int continua `indexVal.Int()`.
- `bindingStore.count()` sob `RLock`; `ObjMap.Len()` usa isso.

### 3.4 (4d) Construtor por `OP_CALL_STATIC` sem validação por argumento — estágio medido

Só entra se, depois de 3.1/3.2, a validação por argumento ainda aparecer. Ao
implementar, conferir em `compileCallExpression` o que `OP_CALL_STATIC` prova
para um call site de construtor (`isExact`/`modesProven` + tipos de argumento
estritamente compatíveis) antes de pular `validateStructConstructorArguments`
em `callValueStatic`; se a prova estática não cobrir `any`/fronteira dinâmica,
não entra e fica registrado.

### 3.5 O que NÃO muda

Representação de `ObjMap` (`map[interface{}]Value`, RWMutex, `gen`), de
`ObjInstance` e de `ObjStruct.Fields`; `OP_GET_PROPERTY`/`OP_SET_PROPERTY`;
mensagens (`struct constructor has incomplete runtime type metadata`, `function
'X' argument N: expected T, got U`, `map key must be int or string`); RC.

## 4. Invariantes e guards executáveis

- Construtor mal-tipado (por `any`) continua com a **mesma mensagem** e no
  mesmo momento, na 1ª e na 2ª construção (cache não muda o erro).
- Struct sem `ConstructorType` (builder JSON) continua falhando igual; cache
  nunca guarda inválido.
- `-race` em `internal/value`/`internal/vm` (cache lido por tasks).
- `m[k] = v` sobre chave existente libera o velho exatamente uma vez
  (`TestMap...` de RC existentes + um dirigido com composto); `length(m)` igual
  antes/depois de `delete`.
- `go test ./...`, corpus, `compare_examples.ps1` 0 divergentes, gates CoW
  (`bench_typed_call_map` e `bench_share_mutate` passam por map/CoW), sentinela
  `bench_generic_vs_hand`, guards de inline inalterados.

## 5. Medição

Bench novo **`benchmarks/bench_struct_records.nx`** (o da §1; entra na suíte —
pedido no Test Plan da #40). Binários: `base` = 4ac27f5 · `s0` = +3.1+3.2 ·
`s1` = +3.3 · (`s2` = +3.4 se entrar). `interleaved_compare.ps1 -Runs 9`
base × head; A/B por estágio (11) em `bench_struct_records`, `cross/map_churn`,
`bench_typed_call_map`; `run_cross_runtime.ps1 -NoxyBaseline`; perfis antes/depois.

**Meta (hipótese):** `bench_struct_records` ≥ −35 % (a #40 pedia ≥ −40 % no
bench dela); `map_churn` 2,1x → ~1,7x do CPython nesta máquina.

## 6. Riscos

- Cache e `ConstructorType` trocado depois (módulos, REPL): guarda por
  igualdade de ponteiro do schema — cache errado vira recálculo, nunca resposta
  errada.
- `Swap` e RC: a ordem retain → swap → release(old) é a de hoje; teste dirigido.
- `key = indexVal.Obj`: igualdade/hash de `interface{}` com `string` dinâmico é
  a mesma de antes (o valor boxado é a mesma string) — `Snapshot`/JSON/iteração
  não mudam.

## 7. Decisões tomadas sem consulta (para a review)

- Branch empilhada sobre o item 3 (medir contra o develop *que vai existir*);
  PR retargetado para `develop` após #69.
- Bench de struct adicionado à suíte neste PR.
- Store tipado de map e campos por índice ficam como follow-up com número.
