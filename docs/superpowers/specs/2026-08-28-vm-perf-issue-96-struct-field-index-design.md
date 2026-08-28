# VM Perf — Campo de struct por índice em compilação (issue #96)

**Data:** 2026-08-28 · **Issue:** [#96](https://github.com/estevaofon/noxy/issues/96) · **Branch:** `perf/issue-96-struct-field-index` · **Base:** `develop` (2a627bc, pós-#95/#97/#98)

## 1. Contexto (medido nesta sessão, binário de 2a627bc)

Desde o #95 uma instância guarda os campos em `Slots []Value` na ordem da
declaração e `ObjStruct` tem o índice nome→slot. O caminho quente de
`OP_GET_PROPERTY`/`OP_SET_PROPERTY`/`OP_GET_PROP_MUT` continua fazendo **um
lookup por string** por acesso (`Struct.FieldIndex(name)` = `mapaccess2_faststr`
+ `aeshashbody`).

**`bench_path_update`** (`cells[i].hits = cells[i].hits + 1`, carga ×10 para
amostra): `FieldIndex` **15,2 % cum**, `mapaccess2_faststr` 14,2 %, `aeshashbody`
4,3 %, `memequal` 1,9 %; `Retain`+`Release` 10 % (no-op para `int`, mas chamadas
reais); `push`/`pop` 15 %.

**`bench_struct_records`** (5 campos, 60k construções, duas funções por valor —
resgatado de 93625ac, nunca tinha chegado a `develop`): o topo **não é** o
hashing de campo. `validateStructConstructorArguments` **45,9 % cum**, via
`runtimeTypeComplete` 40,7 % e `mapassign_fast64ptr` 27,3 % — o `make(map...)`
de memoização é refeito a cada construção porque o cache do construtor do PR #70
(`ctorCache` em `ObjStruct`, commit 131c352) **nunca chegou a `develop`**: o #70
foi mergeado em `perf/issue-66-call-protocol` 23 s depois de o #69 entrar em
`develop`. Achado colateral; fica como follow-up próprio (§7.4). Neste bench o
ganho do índice de campo é limitado às leituras `r.ativo`/`r.valor`/`r.grupo`.

## 2. Objetivo e não-objetivos

**Objetivo:** quando o tipo estático da base é um struct do programa, resolver
`p.x` para o índice de slot em compilação e acessar `Slots[idx]` sem hashing.
Semântica, saída, mensagens e linhas de erro, contagem RC e CoW **idênticas**;
opcodes só por append; bytecode com base `any`/módulo **inalterado**.

**Não-objetivos:** `ref p.x` (`OP_REF_PROPERTY` fica: o `ObjRef` é resolvido
por NOME em `references.go` — `descend`, `referenceStorageMode`, `sameBorrowBase`;
é a parte do #93b); formas fundidas com slot local (`OP_GET_LOCAL_FIELD`);
fundir o `OP_DEREF` de base `ref`; structs de módulo (ficam por nome, como a
issue pede); o cache do construtor (§7.4).

## 3. Desenho

### 3.1 A premissa da issue está errada: precisa de guarda

A issue diz "struct de JSON dinâmico tem a própria definição, então a base
tipada nunca aponta para ele". Falso: `buildTypedJSONValue`
(`json_population.go:357-397`) cria uma `ObjStruct` NOVA com os campos em
**ordem alfabética** (`sort.Strings`) para todo valor de struct que o
`json_loads` **cria** (elemento novo de `Node[]`, campo que era `null`, valor
novo de map). Essa instância entra num contêiner tipado: `let n: Node = xs[0]`
tem tipo estático `Node` (slot 0 = `valor`) e instância com slot 0 = `proximo`.
Um `OP_GET_FIELD 0` cego leria o campo errado.

Por isso o operando carrega o índice **e** o nome, e o caminho rápido confere
`idx < len(Slots) && Struct.Fields[idx] == name` — uma comparação de string
curta (Go compara tamanho, depois ponteiro, depois bytes) no lugar do hash.
Falhou a guarda → funil genérico por nome, o mesmo que o opcode antigo. A
guarda também cobre definições montadas por natives/bytecode de teste sem
índice. Corrigir o JSON para reutilizar a definição declarada não cabe aqui:
`RuntimeTypeInfo.Fields` é map sem ordem, e a definição própria é onde o JSON
guarda `JSONDynamicFields` por instância.

### 3.2 Opcodes (append ao fim de `internal/chunk/chunk.go`, após `OP_GET_LOCAL_2`)

| opcode | operando | pilha | espelha |
|---|---|---|---|
| `OP_GET_FIELD` | `[idx u8][nome u16]` | `[inst] → [val]` | `OP_GET_PROPERTY` |
| `OP_SET_FIELD` | `[idx u8][nome u16]` | `[inst, val] → []` | `OP_SET_PROPERTY` + `OP_POP` |
| `OP_GET_FIELD_MUT` | `[idx u8][nome u16]` | `[inst] → [val unicizado]` | `OP_GET_PROP_MUT` |

`OP_SET_FIELD` é statement (não empilha; o compilador não emite `OP_POP`
depois). Estruturas com mais de 255 campos ficam por nome.

### 3.3 VM — caminho rápido e fallback exato

Os corpos de `OP_GET_PROPERTY`, `OP_SET_PROPERTY` e `OP_GET_PROP_MUT` saem
para métodos `getPropertyGeneric(c, ip, name)`, `setPropertyGeneric(c, ip, name)`
e `getPropMutGeneric(c, ip, name)`; os `case` genéricos passam a chamá-los (uma
chamada a mais por opcode genérico — `any`, módulos — o mesmo custo que a #66
aceitou com `getIndexGeneric`/`setIndexGeneric`; sentinela `bench_generic_vs_hand`).

- **`OP_GET_FIELD`**: topo é `*ObjInstance` (assertion), guarda §3.1 →
  `stack[top-1] = Slots[idx]` no lugar. Senão `getPropertyGeneric` (base ainda
  na pilha): ref auto-deref, `ObjMap`, `null`, mensagens e linha idênticas —
  `Lines[ip-1]` é o último byte de operando, gravado com a linha do opcode.
- **`OP_SET_FIELD`**: `[inst, val]`; instância + guarda; campo `ref T`
  (`RefFields != nil && RefFields[name]` — nil quando o struct não tem campo ref,
  então sem custo no caso comum) → genérico, que aplica `refSlotWriteError`;
  senão `old := Slots[idx]`; se `NeverTracked(val) && NeverTracked(old)` grava
  sem `Retain`/`Release` (são no-op nesses valores — não é uma variante NORC,
  é a mesma semântica sem a chamada); senão `Retain(val); Slots[idx] = val;
  Release(old)`. Limpa os dois slots da pilha. Fallback: `setPropertyGeneric` +
  `vm.LastPopped = vm.pop()` (o `OP_POP` da sequência genérica). O caminho
  rápido não grava `LastPopped`, como as escritas fundidas da #66.
- **`OP_GET_FIELD_MUT`**: instância + guarda; `fieldVal := Slots[idx]`; se
  `IsShared(fieldVal)` clona e grava de volta com retain-antes-de-release (o
  corpo do genérico); resultado no lugar. Senão `getPropMutGeneric`.

Base `ref` (`r.x` com `r: ref Ponto`): o compilador já emite `OP_DEREF`
(leitura) / `OP_DEREF_MUT` (lvalue) antes, então o opcode novo vê a instância;
não muda.

### 3.4 Compilador

`fieldSlot(owner, member) (idx int, ok bool)` em `member_types.go`, irmã de
`memberType`: `unwrapRefType(owner)` é `*PrimitiveType`, `structDeclaration`
existe, `structOrigin(decl) == ""` (struct do programa — inclui instâncias
genéricas monomorfizadas `main::Stack<int>` e structs de escopo local, cuja
`FieldsList` é a mesma que vira `ObjStruct.Fields`), campo achado em
`FieldsList` na posição `idx ≤ 255`. Tudo o mais → `ok=false` → opcode por nome.

Sites: leitura (`compiler.go` `case *ast.MemberAccessExpression`) → `OP_GET_FIELD`;
atribuição (`compiler.go` alvo `MemberAccessExpression`) → `OP_SET_FIELD` sem
`OP_POP`; base de lvalue aninhado (`cow_lowering.go` `compileLValueBase`) →
`OP_GET_FIELD_MUT`. `use m select` e `ref p.x` inalterados. Checagens de tipo,
mensagens e ordem inalteradas (o índice é decidido depois delas).

## 4. Invariantes e guards executáveis

- `chunk_test.go`: sentinela de `TestEveryOpcodeHasASymbolicNameWithoutGaps` →
  `OP_GET_FIELD_MUT`; disassembler ganha `fieldInstruction` (3 bytes) e
  `disassemblyProgram` já tem `p.x = soma(p.x, 10)` com base tipada.
- `cow_lowering_test.go`: walker `collectOpcodes` ganha o caso de 3 bytes;
  allowlist do lowering ganha `OP_GET_FIELD_MUT`/`OP_SET_FIELD`.
- Bytecode (pacote `compiler`, via disassembler): local tipado → `OP_GET_FIELD`
  com o índice certo; `any` → `OP_GET_PROPERTY`; namespace `mod.f` → por nome;
  `ref Ponto` → `OP_DEREF` + `OP_GET_FIELD`; `p.x = v` → `OP_SET_FIELD` sem
  `OP_POP`; `p.a.b = v` → `OP_GET_FIELD_MUT` + `OP_SET_FIELD`; instância
  genérica → por índice; struct de módulo via `select` → por nome.
- Comportamento (pacote `vm`): **instância JSON com ordem invertida lida e
  escrita por base tipada** (a guarda); tabela de erros idênticos base tipada ×
  `any` (null, ref nula, campo inexistente via `any`); RC (`OwnersCount` após
  `box.value = y` com composto — `TestSetPropertyReleasesOldRetainsNew` passa a
  exercitar o opcode novo); CoW (`p.inner.x = 1` com `inner` compartilhado
  clona uma vez, cópia intacta); campo `ref T` escrito por base tipada com ref e
  com null; `-race` no pacote `vm` inteiro.
- `go test ./...`, corpus `run_all_tests_concurrent.nx` 180/180,
  `compare_examples.ps1` 0 divergentes.

## 5. Medição

Binários no scratchpad: `noxy_base.exe` (2a627bc) × head.
`interleaved_compare.ps1 -Runs 9`; perfil de `bench_path_update` e
`bench_struct_records` (carga ×10/×15) sem `mapaccess2_faststr`/`aeshashbody`
vindos de `FieldIndex`; gates CoW ≤ +5 % (`bench_typed_call_map`,
`bench_share_mutate`, `bench_call_light`, `bench_conway`);
`bench_generic_vs_hand` como sentinela do despacho. Seção nova no topo de
`RESULTS.md` + `results/2026-08-28-issue-96-struct-field-index-raw.md`.

## 6. Riscos

| risco | mitigação |
|---|---|
| definição com outra ordem (JSON, natives) | guarda `Fields[idx] == name` em runtime + teste com instância JSON invertida |
| campo `ref T` escrito sem a checagem do slot | fast path recusa quando `RefFields[name]`; genérico decide |
| pular RC de um composto | `NeverTracked(val) && NeverTracked(old)`; oráculo `OwnersCount` |
| fallback divergir do genérico | funil único: os `case` genéricos chamam os mesmos métodos |
| mexer em `run()` regredir o despacho | `bench_generic_vs_hand` |

## 7. Decisões tomadas sem consulta (para a review)

1. Guarda por nome no operando em vez de identidade da definição (§3.1): não
   exige que o compilador tenha o `*ObjStruct` no site de acesso e cobre
   qualquer definição.
2. Sem variante NORC: `OP_SET_FIELD` decide o RC em runtime por `NeverTracked`.
3. `OP_REF_PROPERTY` fora do escopo (o custo do empréstimo é o #93b).
4. Cache do construtor (PR #70) ausente em `develop` — não resgatado aqui; é
   45 % de `bench_struct_records` e merece PR próprio (issue a abrir).
