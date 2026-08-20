# Invariante do slot `ref T` — checagem de campo com base `ref`, `json_loads` e fim do shim (issue #50)

**Data:** 2026-08-20 · **Branch:** `fix/ref-slot-invariant` (a partir do `develop` local, que já contém a #51 em `4ef1777`) · **Status:** design aprovado pelo usuário; revisão independente (timebox 20 min) = "aprovar com ajustes", ajustes incorporados (§3 superfície extra, §4.2 deslocamento de posse, §5.3 escrita através de ref armazenada, §6.1 fonte única `RefFields`, §6.2 `OP_REF_INDEX`, §6.3 custo, §9 `REF_PTR`); implementação pendente
**Issue:** [#50](https://github.com/estevaofon/noxy/issues/50) (3 partes + 2 emendas) · **Release:** v0.10.0 (BREAKING, precedente PR #46 / v0.9.0 e PR #51 / v0.9.1)
**Relação:** fecha a pendência deixada pelo branch `fix/ref-null-forwarding` (PR #51): o *shim* dos opcodes contextuais em `internal/vm/executor.go` e o entry "Inalterado, e registrado como pendência (issue #50)" do CHANGELOG 0.9.1.

## 1. Objetivo

Fazer valer o invariante

> **um slot declarado `ref T` (campo de struct, elemento de `(ref T)[]`, valor de `map[K, ref T]`) contém uma referência (`VAL_REF`) ou `null` — nunca um `T` cru.**

Hoje ele é violado por cinco rotas, todas verificadas no `develop` (`4ef1777`) em 2026-08-20 com `go run ./cmd/noxy`:

| # | Rota | Onde | Hoje |
|---|------|------|------|
| 1 | `node.valor = "texto"` com `node: ref Node` | `compiler.go:711` (`if baseWasRef { leftType = nil }`) pula **toda** a checagem de campo | compila; `type(a.valor)` → `string`, `a.valor + 1` → `1`, exit 0 |
| 2 | `node.proximo = Node(9, null)` com `node: ref Node` | mesma lacuna | compila; grava `Node` cru num campo `ref Node` |
| 3 | `json_loads("[42]", target)` com `target: (ref int)[] = [null]` | `json_population.go:26-45` ("legacy-filled ref slots") | grava `42` cru no slot `ref int` (por design, apoiado no invariante quebrado) |
| 4 | `preenche(a.proximo)` com `a: any` e `*n = Node(7, null)` | `compileReferenceArgumentValue`: `memberType(any)` é nil → `OP_REF_PROPERTY` (ref para o slot) | `*n = ...` grava `Node` cru (emenda 1 da issue) |
| 5 | `a.proximo = Node(9, null)` / `a.valor = "texto"` com `a: any` | `OP_SET_PROPERTY` não valida nada em base dinâmica | grava cru (rota **não listada** na issue; sem fechá-la o critério "para qualquer slot `ref T`…" não vale) |

Em todas, a sonda que distingue é `let viz: ref T = slot; type(ref viz)` → `"ref"`/`"null"` para slot são; para valor cru morre com `reference target marker requires a reference`. (`type(slot)` **não** serve: auto-dereferencia.)

Enquanto o invariante não vale, o runtime mascara o estado: a leitura auto-deref lê o cru como valor, e `OP_CONTEXT_REF_PROPERTY`/`OP_CONTEXT_REF_INDEX` embrulham o cru numa ref-para-o-slot ao encaminhá-lo (shim). A fachada cai em `*viz = ...` ("expected reference value, got object") e em `type(ref viz)`.

## 2. Escopo

**Dentro:**

1. Checagem de campo via base `ref` (Parte 1 da issue).
2. Campo `ref T` recebendo `T` cru via base `ref` passa a ser erro; hint estendido; migração de `noxy_examples/stack.nx` e dos 6 testes de `internal/vm/rc_uniqueness_test.go` (Parte 2).
3. `json_loads` com slot `ref T` nulo: opção **(a)** célula heap + ref; alvo direto nulo continua `false` (Parte 3 + emenda 2).
4. Remoção do shim; `OP_REF_PROPERTY`/`OP_REF_INDEX` consultam o schema de runtime (emenda 1); guard de runtime em `OP_SET_PROPERTY`/`OP_SET_INDEX` para a rota 5.
5. **Under-count de RC do `json_loads`/`json_parse`** (achado lateral, aprovado para este PR): valores construídos pelos builders JSON entram nos contêineres sem `Retain`, então `let t: Pair[] = []; json_loads("[{\"a\":1,\"b\":2}]", t); let p: Pair = t[0]; p.a = 99` muta `t[0]` no lugar (imprime 99; o caminho por valor imprime 1). A célula heap da Parte 3 não pode herdar isso.
6. Spec, `docs/JSON_SUPPORT.md`, CHANGELOG 0.10.0 com guia de migração, bump de versão (`internal/version/version.go`, badge linha 1 e banner do REPL do README).

**Fora:**

- Issue #52 (`break` não fecha upvalues dos locais do laço). A migração recomendada usa a forma **recursiva** do `_append`, imune à #52; o CHANGELOG repete o aviso.
- Mudar o hint de variável (`r = 50` → `use '*r = ...'`): está na spec §2.3 e em testes; só campo/índice ganham o hint estendido.
- Validar em runtime escritas em arrays/maps **sem** tag `RuntimeType` via base `any` (não há schema para consultar; continua fronteira dinâmica).
- Push do `develop` local / fechamento da PR #51: decisão do usuário; a branch parte do `develop` local e a PR da #50 só é aberta depois que a #51 for resolvida no GitHub.

## 3. Parte 1 — checagem de campo com base `ref` (compilador)

**Arquivo:** `internal/compiler/compiler.go`, case de atribuição a membro (`n.Target.(*ast.MemberAccessExpression)`, ~linhas 700-775).

**Mudança:** remover o bloco

```go
if baseWasRef {
    leftType = nil
}
```

`compileLValueBase` (`cow_lowering.go`) já devolve o tipo **desembrulhado** (`derefMutIfRef` emite `OP_DEREF_MUT` e devolve `ElementType`), então `fieldType` resolve para struct conhecido e o bloco "TYPE-BASED ASSIGNMENT LOGIC" existente se aplica sem alteração: rebind (`ref`/`null`/ref compatível), `referenceAssignmentTypeError` para `T` cru em campo `ref T`, `type mismatch in field assignment` com `derefReadHint` para campo comum, e `emitRuntimeValueType(fieldType)` (validação de runtime de compostos, que hoje também era pulada).

O segundo retorno de `compileLValueBase` (`baseWasRef`) deixa de ter consumidor no member-assignment; a função continua devolvendo-o (o comentário da doc de `compileLValueBase` é atualizado para não citar mais a "leniência pré-0.4").

**Comportamento esperado via base `ref`** (idêntico à base valor):

| Statement (`node: ref Node`) | Resultado |
|---|---|
| `node.valor = "texto"` | `[line N] type mismatch in field assignment: expected int, got string` |
| `node.proximo = Node(9, null)` | `[line N] cannot assign Node to ref Node` + hint estendido (§4) |
| `node.proximo = outro` com `outro: Node` | idem |
| `node.proximo = ref novo` | OK (rebind) |
| `node.proximo = null` | OK |
| `node.proximo = outro.proximo` (tipo `ref Node`) | OK (rebind) |
| `*node.proximo = Node(9, null)` com `proximo` não-nulo | OK (update, `OP_SET_PROPERTY_DEREF`, inalterado) |
| `node.valor = 5` | OK |

Base `any` (`a.campo = ...` com `a: any`) continua sem checagem estática — é fronteira dinâmica (§2.0); a rota 5 é fechada em runtime (§6.3).

**Superfície extra habilitada (registrar no CHANGELOG):** com `fieldType` resolvido, o mesmo statement via base `ref` passa também por `rewriteIfGenericValue(n.Value, fieldType)` (target-typing §3, posição 4 — `node.transform = identity` com campo de tipo função genérica passa a inferir pelo campo, como na base valor) e por `emitRuntimeValueType(fieldType)` → `OP_MARK_RUNTIME_VALUE_TYPE` para campos compostos (array/map/chan), que pode falhar em runtime com `runtime value metadata conflicts with static context` quando um composto já etiquetado com outro tipo é atribuído — idêntico ao que a base valor já faz hoje, mas é erro novo para programas via `ref` que hoje rodam sem validação.

## 4. Parte 2 — hint e migração

### 4.1 Hint

`referenceAssignmentTypeError(line, name, expected, actual)` é chamado para variável (`r = 50`), campo (`holder.field = 1`), elemento (`items[0] = 1`) e valor de map (`items["k"] = 1`). O hint atual — `use '*<name>' = ...' to update the referenced value` — fica para **variável**. Para **campo/índice** (o chamador sabe qual é), o hint passa a:

```
[line N] cannot assign Node to ref Node
  hint: to point the field at a new value, bind it to a variable first and use 'x.proximo = ref novo'; to overwrite the referenced value use '*x.proximo = ...'
```

(para índice: "to point the element at a new value … 'lista[0] = ref novo' … '*lista[0] = ...'"). Implementação: uma função irmã `referenceSlotAssignmentTypeError(line, name, kind, expected, actual)` com `kind ∈ {"field", "element"}`, chamada nos três sites de campo/índice; a de variável não muda. `TestReferenceSlotValueAssignmentsSuggestDereference` (`internal/compiler/function_types_test.go`) é ajustado: os subtestes de campo/índice passam a esperar os dois trechos do hint novo (`= ref` e `'*x.campo = ...'`).

### 4.2 Migração (BREAKING)

Padrão antigo (rota 2):

```noxy
func _append(node: ref Node, valor: int)
    if node.proximo == null then
        node.proximo = Node(valor, null)     // ERRO a partir de 0.10.0
    else
        _append(node.proximo, valor)
    end
end
```

Forma idiomática (já usada em `noxy_examples/linked_list.nx` desde a #51):

```noxy
func _append(node: ref Node, valor: int)
    if node.proximo == null then
        let novo: Node = Node(valor, null)   // variável: `ref` exige L-value; vai para a heap
        node.proximo = ref novo              // REBIND do campo do pai
    else
        _append(node.proximo, valor)
    end
end
```

Alcance no repositório: `noxy_examples/stack.nx:6-12` e os 6 testes de `internal/vm/rc_uniqueness_test.go` que usam esse `_append` como veículo (`TestRefLocalBindingIsBorrowNotOwner`, `TestRefGlobalAndCapturedRefLocalAreBorrows`, `TestCapturedAndBorrowedRefSlotsNeverReleaseWhatTheyDoNotOwn`, `TestBorrowedUpvalueRebindKeepsOwnersOfSharedNode`, `TestBorrowConditionIsStaticNotInferredFromOwnedList`, `TestRefWriteToUniquelyOwnedNodeMutatesInPlace`). Só o `_append` muda. A saída de `stack.nx` tem de ser byte-a-byte a mesma do baseline.

**Deslocamento de posse (previsível, registrar no CHANGELOG):** na forma velha o **campo** `proximo` era o dono durável do nó (`OP_SET_PROPERTY` retém, `executor.go` ~1511); na forma nova o dono é a **célula fechada** do `let novo` (`closeUpvalue`, `stack.go` ~271-291) e o campo guarda um `VAL_REF` (retain/release no-op em `ownersOf`). Consequências: (i) `campo = null` / rebind do campo **não solta mais o nó** — quem o mantém vivo é a célula, que o GC do Go recolhe quando nada mais a alcança; (ii) as contagens de `Owners` sondadas pelos testes **não mudam** — o nó continua com exatamente 1 dono, só que a célula em vez do campo (`TestBorrowedUpvalueRebindKeepsOwnersOfSharedNode` espera 2 = célula + `let second`; `TestRefWriteToUniquelyOwnedNodeMutatesInPlace` espera p1..p4 = 1,2,1,1 — derivação em §8). As contagens de `Owners` ficaram **inalteradas** (verificado na implementação). **Achado da implementação:** em 2 dos 6 testes (`TestCapturedAndBorrowedRefSlotsNeverReleaseWhatTheyDoNotOwn` cenários B/C e `TestBorrowConditionIsStaticNotInferredFromOwnedList` cenários f/f2) a asserção de *valor* mudou de 20 para 77 — e isso **não** é bug do caminho novo: o programa faz `let second: Node = head.proximo` (cópia por valor) + `let u: ref Node = head.proximo` + `u.valor = 77`. Com o `Node` cru no slot (forma velha), `u` recebia o cru e virava **cópia** (o box clonava), então a escrita nunca alcançava a lista (20). Com a ref legítima, `u` é ref de verdade e a escrita através dela alcança o nó que `head.proximo` vê (77), enquanto `second` segue independente — exatamente o que o `develop` responde para uma ref legítima montada à mão (`Node(0, ref n20)`: oráculo `77/77/20` em `scratchpad/oracle_ref_write.nx`). Os oráculos foram re-derivados com comentário no teste; o que eles continuam guardando é a independência da cópia por valor (se `second.valor = 99` vazasse, viria 99). Efeito visível para usuários — registrar no CHANGELOG: depois de migrar o `_append`, `let u: ref Node = no.proximo; u.valor = X` passa a alterar o nó da lista (semântica de referência), não uma cópia.

## 5. Parte 3 — `json_loads` com slot `ref T`

### 5.1 Contrato novo

Para schema `TYPE_REF` (slot declarado `ref T`) em **alvo populado recursivamente** (array/struct/map que o `json_loads` recebe inteiro):

| Slot atual | Payload | Ação |
|---|---|---|
| ref válida | não-nulo | escreve **através** da ref (inalterado) |
| ref válida / null | `null` | grava `null` no slot (inalterado: "json null clears reference slot") |
| `null` | não-nulo | **(a)** constrói o `T` pelo schema do referente, cria uma **célula heap** nova com ele e grava no slot uma ref para a célula |
| elemento/campo **novo** (array cresce, struct novo) | não-nulo | idem: célula + ref (hoje `buildTypedJSONValue` devolve o `T` cru para `TYPE_REF`) |
| elemento/campo novo | `null` | `null` (inalterado: "json null creates reference slot") |
| valor cru (estado impossível após este PR) | qualquer | `false` |

Depois de qualquer `json_loads` que devolva `true`: `let viz: ref T = slot; type(ref viz)` ∈ {`"ref"`, `"null"`}; `*target[0] == 42`; passar `target[0]` a um parâmetro `ref int` funciona pelo encaminhamento normal (sem shim).

**Alvo direto** `json_loads(s, slot)` com `slot` um campo/elemento `ref T` **nulo**: o slot chega como `null` encaminhado (#51); não há slot por trás para preencher → `false`, sem efeito (comportamento atual, a documentar). Orientação: passe o **dono** (`json_loads(s, h)` com schema do struct) ou pré-aponte o slot.

### 5.2 A célula heap

É o análogo exato de `let novo: T = ...; slot = ref novo` depois que o frame fecha: um `value.ObjUpvalue` **fechado** (`location == &closed`), possuidor (não-borrowed), com `Owners` do valor = 1 (a célula é o dono durável; `closeUpvalue` chega ao mesmo estado para `let novo`). O slot recebe `Value{Type: VAL_REF, Obj: &ObjRef{RefType: REF_UPVALUE, Upvalue: célula}}`.

Novo construtor em `internal/value/value.go`:

```go
// NewClosedUpvalue cria uma caixa já fechada sobre um valor que não mora em
// nenhum slot de pilha — a "variável anônima na heap" que json_loads usa para
// preencher um slot `ref T` nulo (spec §12). A caixa é possuidora: o chamador
// retém o valor em nome dela.
func NewClosedUpvalue(v Value) *ObjUpvalue
```

`PointsTo(&vm.stack[i])` é falso para ela (nunca aponta para a pilha), então `retargetOwnedSlot` a ignora; `storeReferenceValue` através dela faz retain-novo/release-velho normalmente (`refStorageBorrows` é falso).

### 5.3 RC dos builders JSON (under-count pré-existente)

Modelo a espelhar (já é o do bytecode): **todo contêiner que guarda um composto conta como um dono** — `OP_ARRAY`/`OP_MAP` retêm cada elemento/valor, o construtor de struct retém cada campo, `OP_SET_INDEX`/`OP_SET_PROPERTY` fazem retain-novo/release-velho, `storeReferenceValue` idem com consciência de empréstimo. Precedente em Go: `retainingArray`/`retainingMap` de `internal/vm/builtins_call_result.go` (reutilizados como estão — mesmo pacote; a mudança de arquivo foi dispensada na implementação para manter o diff focado). *(Nota posterior: na v0.10.1 — issue #55, fase A da #54 — esses helpers foram substituídos pelos próprios construtores `value.NewArray`/`value.NewMapWithData`, que passaram a reter os filhos compostos por padrão; ver `docs/superpowers/specs/2026-08-20-container-owners-design.md`.)*

Mudanças em `internal/vm/json_population.go` e `builtins_json.go`:

- **Builders** (`buildTypedJSONValue`, `dynamicJSONValue`, `goValToNoxy`): ao colocar um filho num array/map/struct recém-construído, `Retain(filho)`. Strings/escalares são no-op em `ownersOf`, então o retain incondicional é barato e correto.
- **Setters de substituição** nas mutações preparadas (`prepareJSONArrayMutation`, `prepareJSONMapMutation`, `prepareJSONStructMutation`): quando o commit troca o ocupante de uma posição existente (`newElements[i] = updated`, `newData[k] = updated`, `newFields[name] = updated`), `Retain(updated)` e `Release(antigo)` — na ordem retain-antes-de-release. Quando a mutação é in-place (commit do filho não chama `set`), nada a contar.
- **Posições novas** (array cresce, chave nova no map): `Retain(criado)`. **Posições descartadas** (array encolhe: `len(dataArray) < len(array.Elements)`): `Release(antigo)` no commit.
- **Escrita através de uma ref** — nos **dois** lugares que hoje usam o `store` cru de `referenceStorage`: (i) **top-level** (`populateRef`, caminho `JSONDynamic` e `prepareJSONMutation` quando o alvo é escalar/`any`/substituído inteiro) e (ii) **slot `ref T` já apontando** (`prepareJSONMutation` `TYPE_REF` com `current.Type == VAL_REF` → `jsonReferenceStorage`, `json_population.go` ~32-41 e ~440-447). Ambos passam a usar um setter que chama `vm.storeReferenceValue(Value{VAL_REF, ref}, v)` — retain-novo/release-velho + `refStorageBorrows` + `retargetOwnedSlot`, como qualquer escrita via ref. Sem (ii), um slot `ref Pair` apontando para uma variável que contém `null` receberia um struct construído sem `Retain` e sem reapontar a entrada `frame.Owned` (achado do revisor independente). `jsonReferenceStorage` devolve esse setter; o `store` cru não é mais exposto ao módulo JSON.
- A célula de §5.2 nasce com `Retain(valor)` (a célula é o dono). A ref gravada no slot é `VAL_REF` — `ownersOf` não a rastreia, então `Retain`/`Release` sobre ela são no-op (slot `ref T` não possui, §4.2).

Teste de regressão (vm): o programa de §2 item 5 tem de imprimir `1`; versões para map (`map[string, Pair]`), struct aninhado, `json_parse` (`let d: any = json_parse(...)`, `let e: any = d`, mutar `e`, `d` intacto) e alvo `any` via `JSONDynamic`.

## 6. Parte 4 — runtime: fechar o invariante e remover o shim

**Arquivo:** `internal/vm/executor.go` (+ `internal/value/value.go`, `internal/compiler/compiler.go` para o schema).

### 6.1 Schema de runtime de "slot declarado `ref T`"

- **Struct:** novo campo `ObjStruct.RefFields map[string]bool`, ao lado de `JSONDynamicFields`, preenchido (i) no compilador, no case `*ast.StructStatement` (`compiler.go` ~815-825), para todo campo cujo tipo é `*ast.RefType`; (ii) em `buildTypedJSONValue` `TYPE_STRUCT` a partir de `schema.Fields[name].Kind == TYPE_REF`. `value.NewStruct` não muda de assinatura; lookup em mapa `nil` é válido em Go (devolve `false`, sem hash) — sem custo para structs sem campo `ref`. A informação já existe em `ConstructorType.ParamIsRef[i]` (alinhado a `Fields[i]`), mas structs criados pelo builder JSON nascem **sem** `ConstructorType`, e a pergunta "este campo é `ref`?" por nome precisa ser O(1) no caminho quente; por isso `RefFields` é a **única fonte de runtime para a pergunta do slot** (acessada por um helper `func (s *ObjStruct) FieldIsRef(name string) bool`), e `ConstructorType` segue servindo só à validação do construtor. Um teste Go garante a consistência: para todo struct criado pelo compilador, `RefFields[Fields[i]] == ConstructorType.ParamIsRef[i]`.
- **Array/map:** a tag `ObjArray.RuntimeType` / `ObjMap.RuntimeType` (`atomic.Pointer`, já existente) quando presente com `Element`/`Value` de `Kind == TYPE_REF`. Sem tag → sem informação → comportamento dinâmico atual.

Helper único: `func refSlotDeclared(container value.Value, name string / index) bool` (duas variantes, propriedade e índice).

### 6.2 Criação de refs (shim e emenda 1)

- `OP_CONTEXT_REF_PROPERTY` / `OP_CONTEXT_REF_INDEX`: `VAL_REF`/`VAL_NULL` → encaminha (inalterado); **qualquer outra coisa → erro de runtime** `reference slot '<campo>' holds a non-reference value` (propriedade) / `reference slot at index <i> holds a non-reference value` (array) / `reference slot for key <k> holds a non-reference value` (map). O bloco do shim e seus comentários saem; o comentário passa a citar o invariante e esta spec.
- `OP_REF_PROPERTY` / `OP_REF_INDEX` (base `any` ou struct desconhecido pelo compilador): antes de fabricar `REF_PROPERTY`/`REF_INDEX`, consultar §6.1. Slot declarado `ref T` → **mesmo comportamento do opcode contextual** (encaminha ref/null; cru → mesmo erro). Slot comum → ref para o slot, como hoje. Efeito: `preenche(a.proximo)` com `a: any` e campo nulo dá `cannot update null reference` em `*n = ...` — idêntico à base tipada; `ref a.proximo` e `json_loads(s, a.proximo)` idem.
  - Detalhe de `OP_REF_INDEX` (hoje ele **não** auto-deref nem valida o container, ao contrário de `OP_REF_PROPERTY`): a consulta ao schema só acontece quando o container, **depois de resolver um eventual `VAL_REF`** (mesmo `resolveReferenceValue` que `OP_REF_PROPERTY` usa), é `*ObjArray`/`*ObjMap` com tag `RuntimeType` cujo `Element`/`Value` é `TYPE_REF`. Nesse caso o opcode espelha `OP_CONTEXT_REF_INDEX` por inteiro: índice fora da faixa → erro; **chave ausente em map → `null`** (igual à leitura plana `m[k]`); ref/null armazenado → encaminha; cru → erro de §6.2. Em qualquer outro caso (sem tag, elemento não-`ref`, container não-coleção) o comportamento atual é mantido sem nova validação — **com uma exceção entregue e registrada (revisão de código):** a resolução do container `VAL_REF` acontece *antes* da decisão, então a `REF_INDEX` fabricada no caminho sem tag passa a apontar para o contêiner resolvido (antes, uma `REF_INDEX` com `Container` `VAL_REF` morria em `referenceStorage` com "Target is not indexable"). É defensivo: um programa Noxy não chega a esse estado (`any` guarda a cópia dereferenciada, e parâmetro `any` rejeita ref), por isso não há teste Noxy para ele; fica documentado aqui e no comentário do opcode.

### 6.3 Escrita em slots (rota 5)

- `OP_SET_PROPERTY`: depois de resolver a instância, `if instance.Struct.RefFields[name] && val.Type != VAL_REF && val.Type != VAL_NULL` → erro de runtime `cannot assign <tipo-de-val> to ref <T>` (gêmeo dinâmico do erro de compilação; `<tipo-de-val>` via `runtimeValueMode`/`type` do valor; `<T>` via `ConstructorType.Params[i]` quando disponível, senão só `cannot assign <tipo> to a reference field '<campo>'`). Custo: um lookup em mapa (nil para a maioria dos structs).
- `OP_SET_INDEX`: para array/map com tag `RuntimeType` cujo elemento/valor é `TYPE_REF`, mesma regra. Custo: um `Load()` atômico + comparação de `Kind`.
- `OP_SET_PROPERTY_DEREF` / `OP_STORE_REF` / `OP_STORE_VIA_REF` não mudam: escrevem **através** de uma ref que, após §6.2, nunca aponta para um slot `ref T`.

Via base tipada o compilador já rejeita antes, então esses guards só **disparam** em fronteira dinâmica — defesa em profundidade. O **custo**, porém, é pago em toda escrita: o lookup em `RefFields` (mapa nil → retorno imediato para a maioria dos structs) em todo `OP_SET_PROPERTY` e o `Load()` atômico da tag em todo `OP_SET_INDEX` tipado ou não (um `MOV` em x86; ~1 ns). Aceito. Se o benchmark de escrita em array (`benchmarks/`) mostrar regressão mensurável, a alternativa sem custo é o compilador emitir a variante com guard só quando **não** conhece o tipo da base (`any`), deixando o opcode tipado intocado.

### 6.4 Outros produtores

- Construtor de struct: `Node(1, no2)` com `no2: Node` cru em parâmetro `ref Node` já é rejeitado (compilador + `validateStructConstructorArguments`/`validateParameterModes`); nada a fazer.
- `append(lista, x)` em `(ref T)[]`: já exige ref (`appendItemCompatible`); nada a fazer.
- Variáveis `ref T` (local/global/upvalue): `ref r` encaminha; `r = 50` já é erro; nada a fazer.

## 7. Docs e versão

- **Spec `docs/NOXY_LANGUAGE_SPEC.md`:** §2.3 (tabela Type-Based Assignment ou parágrafo logo após) — nota de que a checagem de atribuição a campo vale igualmente quando a base é `ref` (`node.campo = v` com `node: ref Node` é checado contra o tipo declarado do campo; `campo: ref T` só aceita `ref`/`null`), com o hint novo; §5 Structs (Self-Reference) — exemplo do `_append` idiomático ou link para §4.2; §12 — subseção **JSON** curta com o contrato de `json_loads` para slot `ref T` (§5.1 desta spec, incluindo alvo direto nulo → `false`) e link para `docs/JSON_SUPPORT.md`; §4.2 — uma frase dizendo que o runtime rejeita valor cru num slot `ref T` (erro explícito) em vez de embrulhar.
- **`docs/JSON_SUPPORT.md`:** seção "Reference slots (`ref T`)" com a tabela de §5.1 e exemplo com `(ref int)[]`.
- **CHANGELOG.md:** nova seção `## [0.10.0] - 2026-08-20` (ou a data do merge) com `### Changed (BREAKING)` (checagem de campo via base `ref`; `T` cru em campo `ref T` é erro; `json_loads` célula + ref; shim removido e erros explícitos; guards via base `any`) + `### Fixed` (under-count de RC dos builders JSON) + guia de migração (o `_append` acima, aviso da #52). A seção 0.9.1 **não** é editada (histórico); a 0.10.0 diz explicitamente que fecha a pendência que 0.9.1 registrou.
- **Versão:** `internal/version/version.go` → `v0.10.0`; README badge (linha 1) e banner do REPL (~linha 79).
- Comentários em `executor.go` (shim), `cow_lowering.go` (doc de `compileLValueBase`), `json_population.go` ("legacy-filled") atualizados.

## 8. Testes e verificação

**Compilador** (`internal/compiler/*_test.go`, padrão `compileFunctionSource`): rota 1 e 2 via base `ref` (mensagens exatas); as formas permitidas de §3 compilam; hint novo em campo/índice; hint de variável inalterado.

**VM** (`internal/vm/*_test.go`, padrões `requireBoolResults`/`runTypedFunctionProgram`/`runTypedFunctionProgramError`):

- `json_loads`: subteste "fill null slot with referent value" **renomeado** ("null ref slot gets a fresh referent cell") e reforçado com a sonda `type(ref viz) == "ref"`, `*target[0] == 42`, passagem a `ref int`; elemento novo em `(ref Pair)[]` vazio é ref (sonda); `json null clears/creates` inalterados; alvo direto nulo → `false` (já existe `TestTypedJSONLoadsIntoNullRefAnyFieldReturnsFalse`; adicionar variante `ref int`).
- Shim: `TestNullRefFieldThroughRefBaseForwardsNull` e irmãos (`ref_null_forwarding_test.go`) continuam verdes; novo teste de que um valor cru num slot `ref` produz o erro explícito nos opcodes contextuais e em `OP_REF_PROPERTY`/`OP_REF_INDEX`. Como depois deste PR nenhum programa Noxy consegue produzir esse estado, o teste o fabrica por um native de teste registrado com `machine.DefineNative` (ex.: `corrupt_ref_field(inst, "proximo", valor)` escrevendo direto em `instance.Fields` / `array.Elements`, sem passar pelos guards) e depois roda Noxy que encaminha o slot (`eh_nulo(a.proximo)`, `ref a.proximo`, `let viz: ref Node = a.proximo; type(ref viz)`), esperando o erro `reference slot 'proximo' holds a non-reference value`.
- Base `any` ≡ tipada: `preenche(a.proximo)` → `cannot update null reference`; `a.proximo = Node(...)` → erro de runtime; `a.valor = "texto"` com `a: any` — **atenção:** campo comum via `any` segue fronteira dinâmica sem checagem (não muda); documentar no teste que só o slot `ref` é guardado.
- Invariante universal: para campo, elemento e valor de map, depois de cada produtor legítimo (rebind `ref novo`, `null`, `json_loads`), `type(ref viz)` ∈ {ref, null}.
- RC: os testes de §5.3.
- Migração: os 6 testes de `rc_uniqueness_test.go` com o `_append` novo, asserções iguais.

**Integração:** `go test ./...` completo, **sem `| tail`**; `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx`; **diff de saída** de todos os exemplos determinísticos entre o baseline (`develop` local `4ef1777`, via `git worktree`) e a branch — 0 diferenças além das esperadas (nenhuma: `stack.nx` migrado tem de imprimir o mesmo). `gofmt`, `go vet`.

## 9. Riscos e decisões registradas

- **BREAKING real:** programas que gravavam `T` cru em campo `ref T` via base `ref` param de compilar com mensagem clara e hint de migração. Decisão do usuário: aceito, como nos precedentes #46/#51; versão 0.10.0.
- **Guard de runtime em `OP_SET_PROPERTY`/`OP_SET_INDEX`:** custo de um lookup em mapa nil / load atômico por escrita; aceito (aprovado) porque sem ele o invariante não fecha pela rota 5. Se benchmarks mostrarem regressão mensurável, alternativa é gatear em `len(RefFields) > 0` / flag booleana no `ObjStruct`.
- **Opção (a) do `json_loads`** em vez de (b): mais útil; a célula é exatamente o estado que `let novo` + `ref novo` produz depois que o frame fecha (`REF_UPVALUE` sobre caixa fechada), então não se introduz um novo tipo de `ObjRef`. As formas existentes são `REF_GLOBAL`, `REF_UPVALUE`, `REF_PTR`, `REF_PROPERTY` e `REF_INDEX`; `REF_PTR` (ponteiro cru para slot de pilha, "unsafe if escapes", varrido por `retargetOwnedSlot`) seria a alternativa errada para um valor que nunca morou na pilha.
- **RC dos builders JSON no mesmo PR:** aprovado pelo usuário; toca `json_parse` (`goValToNoxy`) além de `json_loads` porque é a mesma família de builders e o mesmo bug.
- **Git:** branch nasce do `develop` local (com a #51); PR aguarda a #51 no GitHub. Não fazer push do `develop`.
- **Não tocado:** #52; hint de variável; arrays/maps sem tag via `any`.
