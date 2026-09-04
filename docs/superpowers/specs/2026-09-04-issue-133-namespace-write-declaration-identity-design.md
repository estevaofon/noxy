# Escrita tipada pelo namespace (`m.x = v`) e "tipo é a declaração, não a grafia" (issue #133)

**Data:** 2026-09-04 · **Branch:** `feat/issue-133-namespace-write-declaration-identity`, a partir de `develop` (pós #127, v0.23.3)
**Status:** aprovado, em implementação neste PR · **Issue:** [#133](https://github.com/estevaofon/noxy/issues/133) (consolida #129, #130, #131) · **Relação:** #126 item 2 (namespace tipado, `programViewType`), #58 item 1 (regra "não nomeável ⇒ dinâmico", que sai), #56 §8b (regra "somente-leitura", que sai) e §8c (identidade por declaração em `typesEquivalent`), #122 (fronteira dinâmica encolhe para plugin e `any`).

Dois follow-ups do namespace tipado, os dois na direção de **expandir** o que a linguagem aceita. Item 1 é local (compilador + dois ramos no VM). Item 2 é mudança arquitetural na representação de tipo de struct do compilador; o item 1 depende dele para tipar a raiz `m.a` de `m.a.b = v` quando `a` é struct de terceiro módulo, então a ordem de implementação é **2 → 1**.

## 0. Fatos verificados antes do design (2026-09-04, `develop` em f7502c6)

Probes no scratchpad, módulo `m.nx` com `struct P`, `let origin: P`, `let count: int`, `let xs: int[]` e funções que leem cada um de dentro do módulo:

| Programa | Hoje |
|---|---|
| `m.origin.x = 99` / `m.xs[0] = 10` | compila e **muta o estado vivo** do módulo (`read_origin_x()` devolve 99) — via `OP_GET_GLOBAL_MUT m` → `OP_GET_PROP_MUT`, que já tem ramo `ObjMap` com RC correto |
| `m.count = 9` | `cannot assign to 'm.count': module variables are read-only outside the module` (guarda em `compiler.go` ~890) |
| `let r = ref m.count` | `cannot infer type for 'r'`; com anotação `ref int`: `expected ref int, got ref any` |
| `bump(ref m.count)` com `bump(c: ref int)` | compila e morre em runtime: **`Target is not an instance`** — `REF_PROPERTY` só resolve sobre `ObjInstance` (`references.go`, `descend` e `resolveReference`) |
| `append(ref m.xs, 3)` | `append expects an array, got unknown` |
| `OP_SET_PROPERTY` com base `ObjMap` | erro de runtime `only instances have properties` (`field_ops.go`, `setPropertyGeneric`) — nunca alcançado hoje porque o compilador recusa antes |
| `use mid` (`mid.nx` = `use base select *`) + `let v = mid.mkv()` | `cannot infer type for 'v'` (`moduleTopLevelBindings(mid)` não tem `mkv`) |
| `use mid select mkv` + `let v = mkv()` | `variable 'v': unknown type 'V'` — `importBindingFrom` registra `func() -> V` com o nome **cru** do módulo, sem tradução |
| `let v: base.V = ...` sem `use base` | `cannot resolve type 'base.V': 'base' is not an imported module` + `hint: add 'use base' at the top of the file` (bom; fica) |
| dois módulos `x.nx`/`y.nx` com `struct V` de layouts diferentes; `let z: any = y.V("s")`, `let v: x.V = z` | runtime `expected V, got V` — o guard compara **nome cru e layout**; homônimos com o mesmo layout passam. Pré-existente, fora do escopo (§5) |

Corrige a issue em dois pontos: `ref m.x` e `append(ref m.xs, v)` **não** "continuam legais" — hoje não funcionam — e precisam de suporte no VM (§2.3).

## 1. Item 2 — tipo de struct carrega a identidade da declaração

### 1.1 Precedente e decisão

Go: `types.Named` guarda o objeto da declaração (`obj *TypeName`, com pacote) e a grafia é decidida só na impressão por um `Qualifier` (`types.TypeString(t, qual)`); `v := mid.F()` compila mesmo que `F` devolva um tipo de pacote nunca importado, `v.X` é checado, a mensagem imprime `base.V`, e importar `base` só é preciso para **escrever** o tipo. Rust: `DefId` + impressão do caminho (`base::V`), idem. Nos dois, **o tipo é a declaração; o nome é só como se escreve**. A regra da #58 ("não inventar nome que o usuário não consegue escrever ⇒ dinâmico") foi solução conservadora para a representação por string; é ela que sai.

Alternativas descartadas (revisão de design, 2026-09-04):

- **Nó novo `*ast.StructType{Decl, Origin, Display}`**: mais limpo, mas todo `switch` sobre `*ast.PrimitiveType` (~40 sites: narrowing, generics, `runtime_types`, nullable, `function_types`) ganha um caso novo — risco de regressão maior pelo mesmo ganho semântico.
- **Nome canônico `módulo::Nome` como identidade string**: exigiria trocar os ~200 `t.String()` de mensagens por um `c.typeString(t)` com qualificador de escopo e voltaria a fazer da string a identidade.

### 1.2 Representação

`ast.PrimitiveType` ganha dois campos:

```go
type PrimitiveType struct {
    Name   string           // grafia de exibição / anotação escrita; NUNCA identidade quando Decl != nil
    Decl   *StructStatement // identidade da declaração (nil para primitivos e para instância genérica, ver 1.6)
    Origin string           // módulo declarante ("" = programa); mesma string de namespaceImports / moduleDiscovery.origins
}
```

`String()` continua devolvendo `Name`. `walk.go` não muda (tipos não são visitados). `ast.CloneType` e `substituteType` (`generics_substitute.go`) **copiam o ponteiro** `Decl` e `Origin` — clonar a declaração quebraria a identidade em silêncio.

### 1.3 Decl é a identidade; Name nunca

- `c.structDeclarationOf(prim *ast.PrimitiveType) *ast.StructStatement`: devolve `prim.Decl` quando presente; senão o caminho atual, `structDeclaration(prim.Name)` (rede de segurança para nós que nenhum ponto de resolução tocou — ver 1.4; a revisão adversarial procura exatamente esses). Passam por ele: `memberType`, `fieldSlot`, `typeWithoutDefault`, `containsCallableType` (que passa a marcar visitados por `*StructStatement`, não por nome — hoje dois homônimos de módulos distintos compartilham a marca de ciclo), `isPureBuiltinCall` (nome é identificador, não anotação: continua por `structDeclaration`), `requiresRuntimeValueType`, `runtimeTypeInfoWithStructs`, `resultTypeArgs`/`resultInstance` (`call_result.go`, que hoje sniffa o prefixo `errors::Result<`).
- `typesEquivalent` (caso `*ast.PrimitiveType`): se os dois lados têm `Decl`, compara ponteiro e **ignora `Name`**; se um lado não tem, resolve por `structDeclarationOf` de cada lado e compara ponteiros; só quando nenhum lado designa struct vale `x.Name == y.Name` (primitivos).
- `areTypesCompatible` / `areStrictTypesCompatible`: o atalho `expected.String() == actual.String()` **sai** — dois `Decl` distintos podem exibir a mesma string (alias `use other as base` + `base.V` canônico de um módulo `base` não importado). O primeiro teste passa a ser `typesEquivalent`; o custo é desprezível (o atalho só evitava a recursão estrutural).
- `looselySameType`/`stripTypeQualifiers` (unificador puro de genéricos, sem `*Compiler`): inalterados — continuam comparando grafia sem qualificador; a pass 2 decide com o compilador completo (`typesEquivalent`). Documentar no comentário que `Decl` não é consultado ali de propósito.

### 1.4 Um único ponto que preenche Decl

`resolveAnnotation` (`generics_structs.go`) — já chamado em toda posição de anotação: `let` (`compiler.go` ~412), parâmetros e retorno (`resolveSignatureAnnotations`, inclusive literal de função e instanciação de template), campos de struct (`resolveStructFieldAnnotations`) — ganha o caso `*ast.PrimitiveType` com nome que não é builtin e `Decl == nil`: resolve por `structDeclaration(Name)` (nome simples via `c.structs`, `ns.T` via namespace) e preenche `Decl` e `Origin = c.structOrigin(decl)` **in place, no mesmo nó, sem alocar** — preserva o fast path de custo zero (§5 dos genéricos) e o contrato "mesmo ponteiro de volta". Nome que não resolve fica com `Decl == nil` e é reportado por `checkDeclaredType` como hoje (`unknown type 'V'`, `cannot resolve type 'base.V'`), com o hint de 1.7. Depois da resolução, `Decl == nil` só existe em primitivo, em instância genérica (1.6) e em anotação inválida já reportada.

Efeito colateral desejado: a mutação in place acontece no AST memoizado do módulo durante o `validator.Compile` de `loadModuleDeclarations` — a mesma disciplina que `resolveStructFieldAnnotations` já pratica. O `Decl` gravado ali aponta para o mesmo `*ast.StructStatement` que `discoverModuleStructs` devolve ao importador (mesmo AST), então a identidade atravessa unidades de compilação. `Origin` gravado pelo validador é a visão do módulo sobre si (`""`), por isso `programViewType` (1.5) **recalcula** `Name` e `Origin` a partir de `Decl` na visão do importador; `Decl` é a única coisa carregada.

### 1.5 `programViewType` e `programStructName` viram só exibição

`programViewType(t, origin)` deixa de devolver `(nil, false)` para struct. Para `*ast.PrimitiveType` que designa struct (por `Decl` ou, se ausente, por `lookupStructFrom(origin, Name)`), devolve **sempre** um nó novo `{Name: programStructName(decl), Decl: decl, Origin: c.structOrigin(decl)}`. `programStructName(decl)` passa a devolver sempre uma grafia, nesta ordem:

1. `decl.Name`, se o programa importou **essa** declaração por `select` (`c.structs[decl.Name] == decl`) — ou se é struct do próprio programa;
2. `alias.Nome` para o primeiro `use m [as alias]` de `namespaceOrder` cujo módulo exporta a declaração **e cujo alias não está sombreado por local ou upvalue no ponto de uso** (`isShadowedByLocal`) — é o caso (c) da issue: dentro de `func f(m: Box)`, o tipo de `w.make()` exibe `w.V`, não `m.V`;
3. o caminho canônico `origem.Nome` (`base.V`; para módulo de diretório `src.geo.V`) — como Go imprime `base.V`. Não é grafia que o programa consegue escrever sem `use base`; é só como o tipo se exibe.

`ok=false` continua existindo apenas para o que não é struct nem primitivo (1.6): instância genérica interna do módulo, `GenericType`/`TypeParamType` residual, `nil`.

`newStructFunctionType` (construtor) passa a receber a declaração e produzir retorno `{Name: decl.Name, Decl: decl, Origin}` — os quatro chamadores (declaração local, predeclare, instância genérica, `importBindingFrom`/`importedBindingType`) têm a declaração à mão.

**Caminho `select` também traduz.** `importBindingFrom` (func, struct, `let`) passa o tipo registrado em `c.globals` por `programViewType(tipo, módulo)`, como `namespaceMemberType` já faz. Fecha o buraco documentado em `vm/module_exports_test.go` ~641 (assinatura importada com nome cru `Point` capturada por um `struct Point` local do importador) e o sintoma "`use mid select mkv` → `unknown type 'V'`".

**Reexport pelo namespace.** `namespaceMemberType`: quando `moduleTopLevelBindings(módulo)` não tem o nome, resolve por `reexportSource(módulo, nome, visited)` e repete no módulo declarante — o mesmo fallback de `importBindingFrom`. `importedBindingType` ganha esse fallback (é o ponto comum). A assimetria documentada na spec §11 some.

### 1.6 O que fica como está (anotado, não resolvido)

- **Instância genérica interna do módulo** (`c: Caixa<int>` em `g.nx`, nome achatado `main::Caixa<int>`): continua dinâmica pela regra de instância da §6.4 — tipá-la exigiria instanciar o template no importador (`g::Caixa<int>`) e o nome de runtime das instâncias divergiria. `isGenericInstanceName` continua o critério. Nota nova na spec §11.
- **Identidade nominal em runtime** é o nome cru (`ObjStruct.Name`, `RuntimeTypeInfo.Name`, comparados por `==` mais layout). `runtimeTypeInfoWithStructs` passa a partir de `Decl` (memo já é por ponteiro) mas continua emitindo `Name: decl.Name`. Homônimos de módulos distintos com o mesmo layout são indistinguíveis no VM, e a mensagem `expected V, got V` é defeito de exibição: **bug pré-existente, registrado na issue como follow-up**, não resolvido aqui (qualificar o nome de runtime quebraria `validStructConstructorType`, `json_population` e `type()`).

### 1.7 Anotação escrita: só ela exige grafia

- `let v = mid.mkv()` infere `v: base.V` (exibição canônica); `v.x` é checado; `mid.usa(v)` confere o argumento; `let s: string = v` é `expected string, got base.V`; `res.rows` com `Row` não importado é `Row[]` tipado — `let s: string = res.rows` é `expected string, got db.Row[]`.
- Anotação que não resolve continua erro. `unknown type 'V'` ganha hint que nomeia o módulo real quando alguma dependência já carregada declara `V`: busca em `moduleDiscovery.origins` por `decl.Name == "V"`; com origem `base` e reexportador `mid` importado por namespace, o hint é `add 'use base' or 'use mid select V' to name this type`; sem candidato, o hint atual (`declare 'struct V' or import it with 'use m select V'`). `cannot resolve type 'base.V'` fica como está.
- `let q: m.V` dentro de `func f(m: Box)` continua compilando: anotação é espaço de tipos, `structDeclaration("m.V")` não consulta sombreamento de valor (comportamento atual, preservado). Só a **exibição** (1.5 regra 2) pula alias sombreado.

### 1.8 Quebra deliberada

Programa que hoje escreve tipo errado sobre um valor não grafável compila e falha em runtime; passa a ser erro de compilação. `Changed (BREAKING)` com tabela Antes/Agora e migração (corrigir o tipo; se o valor era `any` de fato, anotar `any`). Oráculo: runner dos exemplos, `tests/`, `noxy_libs/`, `internal/stdlib/`, suíte Go.

## 2. Item 1 — escrita tipada pelo namespace

### 2.1 Precedente e decisão

Python, Go (variável exportada de pacote), Nim, Swift (`public var`) permitem escrever num global de outro módulo; só Rust (`static` com `unsafe`) e linguagens funcionais proíbem. A regra "read-only outside the module" (0.11.0, #56 §8b) entrou como remendo porque `m.x = v` "gravava num binding que ninguém lê"; desde o #126 `m.x` tem tipo estático e o objeto do namespace compartilha o `bindingStore` do módulo (`ExportMap`), então a escrita cai na variável viva — a leitura já é "live" pela spec §11; a escrita passa a ser também. `select` continua snapshot.

### 2.2 Compilador

No ramo de atribuição a membro (`compiler.go` ~875), a guarda "read-only" sai. Quando a base é `*ast.Identifier` que é alias **puro** de namespace (`c.globals[alias]` presente com tipo `nil`, em `namespaceImports`, não sombreado por local/upvalue — mesma tripla da guarda atual):

1. Resolve a declaração: `moduleTopLevelBindings(módulo)[membro]`, com fallback em `reexportSource` (o mesmo de 1.5). Ausente: `[line N] 'm' has no member 'y'`. `*ast.FunctionStatement`: `cannot assign to 'm.f': it is a function` + `hint: only module variables ('let') can be assigned`; `*ast.StructStatement`: idem com `it is a struct`. Template genérico cai nos mesmos dois.
2. `*ast.LetStmt`: tipo do membro = `namespaceMemberType(access)` (declarado no módulo, traduzido). Emite `OP_GET_GLOBAL m` (leitura simples — o mapa do namespace não precisa de `_MUT`: a escrita é no store compartilhado, não numa cópia), compila o valor, aplica **o mesmo protocolo da atribuição a global** (`compiler.go` ~719): membro `ref T` só aceita rebind por `ref`/`null` (`referenceAssignmentTypeError` caso contrário); membro comum exige `areTypesCompatible` — mensagem `type mismatch in assignment to 'm.count': expected int, got string` com `derefReadHint`/`nullMismatchHint`; `emitSlotGuards(tipo, valType)`; `OP_SET_PROPERTY nome` + `OP_POP`. Membro cujo tipo traduzido é desconhecido (instância genérica, módulo não carregável): escrita dinâmica sem checagem estática, como um global `any` — nunca recusa.
3. `rewriteIfGenericValue(n.Value, tipo)` antes de compilar o valor (target-typing posição 4, como no campo de struct).

**Raiz tipada de lvalue.** `compileLValueBase`, `compileBorrowBase` e `lvalueStaticType` (caso `*ast.MemberAccessExpression`) ganham a mesma raiz: quando `n.Left` é alias puro de namespace não sombreado, o tipo do nível é `namespaceMemberType(n)` em vez de `memberType(nil, ...)`. O bytecode desses caminhos não muda (`OP_GET_GLOBAL_MUT m` → `OP_GET_PROP_MUT a` já funciona, §0). Com isso `m.a.b = v`, `m.xs[i] = v`, `ref m.x`, `append(ref m.xs, v)`, `pop(ref m.xs)` entram no funil de CoW/borrow-as-place existente com o tipo do lvalue conhecido; `let r = ref m.count` infere `ref int`.

### 2.3 VM

Sem opcode novo. Dois ramos `ObjMap` espelhando os que os caminhos de leitura já têm:

- `setPropertyGeneric` (`field_ops.go`): base `*value.ObjMap` → `old, exists := m.Get(name)`; ausente é `undefined property '%s' in module/map` (mesma mensagem de `getPropMutGeneric`); `value.Retain(new)`, `m.Set(name, new)`, `value.Release(old)` — retain-antes-de-release, como o ramo de instância. `ObjMap.Set` sobre o store compartilhado é mutexado e avança `gen` (`map.go`), então a garantia "operações individuais em global são sincronizadas" de `docs/concurrency.md` se mantém e o cache de leitura por geração invalida.
- `REF_PROPERTY` com contêiner `*value.ObjMap` (`references.go`): `descend` (passo intermediário, `forWrite` uniciza o filho e regrava com Retain/Set/Release) e `resolveReference` (passo final, devolve `referenceSetter{kind: setterMap, mapping, key: name}`) ganham o ramo, calcado no que `REF_INDEX` já faz para mapa com chave string. `borrowBaseAddr` já cobre. A guarda R1 de "slot já guarda referência" para `ObjMap` consulta o valor guardado (`VAL_REF`), como `REF_INDEX` faz.

`unicize` do mapa do namespace na raiz (`OP_GET_GLOBAL_MUT m` / `derefPlace` para escrita) não copia: cada `use` cria seu `ObjMap` com RC 1 sobre o store compartilhado (§0 mostra a escrita aninhada chegando ao módulo).

### 2.4 Sem quebra

Só programas que não compilavam passam a compilar: `Added` no CHANGELOG. `TestModuleVariableAssignmentViaNamespaceIsCompileError` inverte (vira o teste de escrita tipada); a mensagem "read-only" some do compilador e da spec.

## 3. Documentação

| Onde | O quê |
|---|---|
| spec §3 | parágrafo do membro por namespace (~731): sai "Only a member whose type the program cannot name … stays dynamic"; entra "the value is fully typed even when the program cannot write its type (it prints as `base.V`); only a written annotation needs a name". Tabela do `cannot infer`: sem linha nova (a causa deixa de existir) |
| spec §11 "Module state is read-only from outside" | vira **"Module state is writable through the namespace"**: `m.x = v` legal, com o tipo declarado pelo membro (`expected int, got string`), escrita viva vista pelo módulo; `select` continua snapshot; erros só para membro inexistente e para função/struct |
| spec §11 tabela de tradução | linha "neither (the program cannot name `Row`) → dynamic" vira "neither → `sqlite.Row[]` (canonical path; the value is typed, the name is display only)"; sai "partially nameable becomes dynamic as a whole"; fica a nota da instância genérica; entra a regra **"a value's type is its declaration; a name is needed only to write an annotation"**, com o exemplo `let v = mid.mkv()` / `let s: string = v` erro |
| spec §11 "Member access through a namespace" | sai o parágrafo da assimetria do reexport e "a member whose type the program cannot name … stays dynamic"; entra: reexport resolve ao módulo declarante; mensagens usam o alias visível não sombreado, senão o caminho canônico |
| spec §11 "Unknown type names" | hint com módulo real (1.7) |
| CHANGELOG | `Added` (item 1, `ref m.x`, `append(ref m.xs, v)`), `Changed (BREAKING)` (item 2) com Antes/Agora e migração; nota do bug pré-existente de runtime (1.6) |
| issue #133 | comentário de fechamento com o follow-up de identidade nominal em runtime |

Sem bump de versão neste PR.

## 4. Testes

TDD por item (vermelho → verde). Compilador (`internal/compiler`):

- `struct_identity_test.go` (novo): `Decl` preenchido por `resolveAnnotation` em `let`/parâmetro/retorno/campo; `typesEquivalent` por ponteiro ignorando `Name`; dois `Decl` distintos com a mesma grafia não são compatíveis; `CloneType`/`substituteType` preservam o ponteiro; struct local `V` ≠ `base.V`.
- `member_access_typing_test.go`: `TestModuleFieldTypeUnnameableStructStaysDynamic`, `…PartiallyUnnameableStaysDynamic` invertem (passam a exigir `expected string, got db.Row[]`); `…OwnGenericInstanceStaysDynamic` fica.
- `namespace_member_typing_test.go`: `TestNamespaceMemberStaysDynamicWhenStructIsUnnameable` e `TestNamespaceMemberReexportedByWildcardStaysDynamic` invertem; novos: `mid.mkv("x")` e `mid.mkv(1, 2)` são erros, `let s: string = v` é erro, `v.x` checado, `mid.usa(v)` confere; alias sombreado no ponto de uso exibe `w.V`; `let q: m.V` dentro de `f(m: Box)` compila.
- `unknown_type_test.go`: hint com módulo real; `let r: Row = res.rows[0]` sem `select Row` é `unknown type 'Row'` + hint; `let v: base.V` com `use base` ou `select V` designa a mesma declaração.
- `namespace_write_test.go` (novo): `m.x = v` compila; `m.x = "a"` com `x: int` é mismatch; `m.y = 1` inexistente; `m.f = 1` função; `m.P = 1` struct; membro `ref T` só rebind; `m.a.b = v`, `m.xs[i] = v`, `ref m.x`, `append(ref m.xs, v)` tipados (mismatch recusado em cada um); `let r = ref m.count` infere `ref int`.

VM (`internal/vm`):

- `namespace_write_test.go` (novo): escrita direta vista por função do módulo (`read_count()` devolve 9); escrita aninhada; `ref m.x` mutando via `bump(ref m.count)`; `append(ref m.xs, v)` visível no módulo; `use m select x` continua snapshot após `m.x = v`; RC: escrever composto em `m.xs` e substituir (`container_owners`-style: contador do antigo liberado, do novo retido); escrita concorrente de `spawn` em `m.count` não corrompe o runtime Go (`-race`).
- `module_exports_test.go`: `TestModuleVariableAssignmentViaNamespaceIsCompileError` inverte; `TestLocalStructIsNotTheModuleStructOfTheSameName` passa a valer também pelo caminho `select` de função (o buraco de ~641).
- `namespace_ref_target_test.go`: os casos "unnameable ⇒ checado em runtime" viram "tipado em compilação" (`OP_CALL_STATIC` legítimo agora que o alvo tem tipo), mantendo o controle negativo do #126 (`ref` com alvo **realmente** desconhecido, `any`, continua `OP_CALL`).

Guardas de arquitetura (`architecture_test.go`, `inline_guard_test.go`, `builtins_registry_test.go`) não mudam: nenhum builtin, opcode ou pilha nova.

## 5. Verificação

Por item: `go build ./... && go vet ./...`, `go test ./internal/... -count=1`, `go test ./cmd/... -count=1`, `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx`, `gofmt -d` nos arquivos tocados, `git diff --numstat` sem arquivo reescrito por EOL. Ao final do item 2, adicionalmente:

- varredura de compilação de todos os `.nx` fora do runner (`tests/`, `noxy_libs/`, `internal/stdlib/`), diff de diagnósticos antes/depois — o gate filtra por **todas** as mensagens novas desta spec (memória: gate que filtra por substring só mede o que já conhece);
- `grep -n 'PrimitiveType{Name:' internal/compiler` como lista de suspeitos: cada site construído à mão sem `Decl` é revisado (é struct? então passa a receber a declaração);
- **revisão adversarial independente obrigatória** (memória do projeto: #83 e #126): um agente sem o contexto desta spec procura (a) um `PrimitiveType` de struct que chega a `typesEquivalent`/`emitSlotGuards` sem `Decl`, (b) uma escrita pelo namespace que escape do tipo declarado ou vaze RC, (c) um caso em que a exibição use alias sombreado ou nome que o programa não escreveu onde poderia. Achar caso conta como sucesso; caso sem correção vira teste de caracterização.
