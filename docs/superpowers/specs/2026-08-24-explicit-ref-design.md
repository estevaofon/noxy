# `ref` explícito — `*r` para ler, `ref x` em todo call site (issue #82)

**Data:** 2026-08-24 · **Branch:** `feature/issue-82-explicit-ref` (a partir de `main` @ `3c579ae`, v0.18.0) · **Status:** design aprovado pelo usuário em conversa (seções 1–6); implementação pendente
**Issue:** [#82](https://github.com/estevaofon/noxy/issues/82) · fecha também [#81](https://github.com/estevaofon/noxy/issues/81) (tempo de vida de local referenciada) · **Release:** v0.19.0 (BREAKING; precedentes v0.9.0 / v0.10.0)
**Substitui:** o modelo "Automatic Dereference + Type-Based Assignment" de `specs/2026-08-08-consistent-reference-semantics-design.md` e a "conversão contextual" de `specs/2026-08-08-typed-function-signatures-design.md` §4.2. Não toca o invariante do slot `ref T` (`specs/2026-08-20-ref-slot-invariant-design.md`), que continua valendo.

## 1. Objetivo

Hoje a §2.3 descreve `ref` por **duas decisões implícitas** e um conjunto de exceções que as remendam:

| Decisão implícita | Remendos que ela exigiu |
|---|---|
| Um `ref T` é lido como `T` sem escrever `*` ("auto-dereference") | exceção 1 (`==`/`!=` nunca lê; `ref == valor` é erro); exceção 2 (RHS de `=` nunca lê — mas `let x: T = r` lê, `*r = s` lê, `*r = ref z` lê); §7 (`ref bool` em condição lê; `&&`/`\|\|` não); tabela "Type-Based Assignment" |
| Um `ref` é criado sem escrever `ref` ("conversão contextual" em assinaturas exatas) | fronteira dinâmica (`func` bare, plugin, nativo sem contrato) exige `ref x` — a mesma expressão `f(x)` significa duas coisas; "forwarding form" (`ref r` com `r: ref T` encaminha); proibição estrutural de `ref ref T` |

O efeito para quem **lê** o código: em `n = r`, `r == 1`, `f(x)`, não dá para saber se está lendo o valor ou a referência, nem se `x` pode ser mutado pela chamada, sem reconstruir os tipos de cabeça. Isso contradiz o Zen ("Sharing is ref. There is no other way"; "Dynamic exists, but it is explicit").

**Objetivo:** remover as duas decisões implícitas. Depois disto, **uma referência nunca é criada nem lida sem `ref` ou `*` no código-fonte**, com um único atalho (`.`/`[]` atravessam). As exceções deixam de existir porque não há mais nada implícito a excepcionar.

Fato que dimensiona o trabalho (levantamento de 2026-08-24 sobre `3c579ae`): **a VM já é explícita**. Aritmética (`OP_ADD` etc.), `OP_EQUAL`, `print`/`to_str`, `length`, `OP_LEN`, `getIndexGeneric` — nenhum lê um `VAL_REF`. `val * 2` e `print(r)` funcionam hoje só porque o compilador insere `OP_DEREF` (19 sites, 17 implícitos). A mudança é quase toda no compilador.

## 2. Escopo

**Dentro:**

- R1–R10 da §3 (regras), diagnósticos da §4, compilador/VM da §5, corpus da §6, docs/versão da §7, testes da §8.
- Issue #81: a reescrita da §2.3 incorpora o tempo de vida de local referenciada (R9).
- Texto do Zen e da §2.2 (regras 6 e 8) sobre closures/globais (R10).
- Bug latente: `for x in r` com `r: ref T[]` compila e itera zero vezes em silêncio (`OP_LEN` devolve 0 para `VAL_REF`); vira erro de compilação.

**Fora (issues de follow-up, abertas pelo plano):**

- `ref` tomado para dentro de container **antes** de uma cópia — a cópia posterior enxerga escritas pelo ref pré-existente ("documented edge" da §2.2). É bug de CoW na VM, não de sintaxe. — issue #83
- `ref T` não-nulo por padrão / `ref T?`. — issue #84

**Não muda:** opcodes existentes e suas variantes `_BORROW`; RC/CoW/`Owned`; `validateParameterModes`; `OP_EQUAL` e `valuesEqual` (identidade de slot); invariante do slot `ref T`; `addr()`; generics (`T` não liga a `ref X`; `ref T` no parâmetro); `null` como valor de `ref T`.

## 3. Regras da linguagem (nova §2.3)

**R1. `ref x` cria uma referência.** `x` deve ser endereçável: variável local/global, campo, índice de array, entrada de map, variável capturada. Literal não-nulo ou temporário (resultado de chamada cujo tipo não é `ref T`) → erro `reference argument '…' is not addressable`. Se o tipo estático de `x` já é `ref T` → erro `'x' is already a reference` (hint `pass 'x' directly`). Consequência: `ref ref T` não pode ser produzido; a anotação `ref ref T` é rejeitada no parser.

**R2. Um `ref T` nunca é lido implicitamente.** Onde o compilador espera `T` e encontra `ref T` → erro de tipo com hint `use '*r' to read the referenced value`. Posições: operandos de operador binário e unário (`+ - * / % == != < > <= >= && || ! - ~` e bitwise), condição de `if`/`while`, coleção de `for … in`, índice em `a[i]`, argumento para parâmetro não-`ref` (função, `func` tipado, construtor, builtin, nativo), `return` para tipo de retorno não-`ref`, inicializador `let x: T = r`, RHS de `x = r`, RHS de `*r = s`. Um parâmetro sem assinatura (`print`, `to_str`, nativos legados) recebe o `ref` como valor — `print(r)`, `to_str(r)` e `f"{r}"` (que desugara para `to_str(r)`) mostram `<ref …>`; para ver o valor, `*r`. Um parâmetro ou slot `any` **aceita** um `ref T` como valor (a referência viaja, como em `chan_send(ch, r)`); isso não é leitura.

**R3. `*r` é a única leitura e escrita do valor apontado.** `*r` em expressão lê; `*r = v` escreve. `*x` com `x` de tipo estático não-ref → erro `cannot dereference <tipo>`. Via `any` (tipo estático desconhecido) a mesma checagem ocorre em runtime, com a mesma mensagem. `*r` com `r == null` continua erro de runtime (null reference).

**R4. `.` e `[]` atravessam a referência.** `r.f`, `r[i]`, `r.f = v`, `r[i] = v`, `ref r.f`, `ref r[i]` valem como `(*r).f` etc. É o único atalho da linguagem. O índice `i` segue R2 (`a[ri]` com `ri: ref int` → erro, hint `use 'a[*ri]'`). Aplica-se em qualquer profundidade (`r.f.g`, `r[i].f`) e através de base `any` em runtime (comportamento atual de `OP_GET_PROPERTY`/`OP_SET_PROPERTY`/`_MUT`, mantido).

**R5. Um `ref` nunca é criado implicitamente.** Parâmetro, campo de construtor ou slot declarado `ref T` aceita exatamente: `ref x` (R1), uma expressão cujo tipo estático já é `ref T` (variável, campo, elemento, entrada de map, chamada que retorna `ref T`), ou `null`. `f(x)` com `x: T` → erro `argument N of 'f' expects ref T, got T` (hint `use 'ref x'`). Sem distinção entre função do usuário, valor `func` tipado, `func` bare, construtor de struct, instância de generic e builtin: `append(ref xs, v)`, `pop(ref xs)`, `delete(ref m, k)`, `json_loads(s, ref alvo)`. Uma expressão já `ref T` é passada como qualquer outro valor — não existe mais "forwarding" como conceito, é só passar um valor.

**R6. Rebind é `=`, update é `*… =`.** Com `r: ref T`: `r = ref y` rebinda; `r = v` com `v: T` → erro (hint `use '*r = …' to update the referenced value`); `*r = ref y` → erro `cannot assign ref T to T` (hint `use 'r = ref y' to rebind, or '*r = y' to write the value`). Igual para campo/elemento/entrada declarados `ref T` (`x.next = ref n` rebinda; `x.next = n` erro; `*x.next = ref n` erro). Rebind de parâmetro `ref` muda só a referência local, não a do chamador (inalterado).

**R7. `==`/`!=`.** ref × ref → identidade de slot; ref × `null` → "a referência é null?"; ref × valor → erro (é R2, hint `use '*r'`). `*r == v` compara valores. Comportamento idêntico ao atual; deixa de ser "exceção 1" e passa a ser consequência de R2 + "ninguém espera `T` quando os dois lados são ref".

**R8. `null` é valor válido de `ref T`.** Pode ser armazenado, passado, retornado, comparado e substituído por rebind. Inalterado.

**R9. Tempo de vida de uma local referenciada** (fecha #81). `ref x` sobre uma local promove o slot de `x` a uma célula no heap (upvalue); a célula vive enquanto existir qualquer referência para ela, inclusive depois de a função retornar. Isso é o mecanismo de alocação de nós da linguagem — não há `new`:

```noxy
let novo: Node = Node(v, null)   // uma variável: `ref` exige um l-value (R1)
node.next = ref novo             // `novo` vira célula; sobrevive ao fim da função
```

Custo: uma alocação de célula por local referenciada, e a partir daí a variável participa da contagem de posse (`Owned`); locais nunca referenciadas continuam na pilha. `==` entre refs compara essas células (R7). Uma closure que captura `ref` a uma local e é passada a `spawn`/`spawn_task` compartilha a célula entre routines — coordenação necessária, como para globais (ver `docs/concurrency.md`).

**R10. Compartilhamento.** Zen e §2.2 passam a dizer: *"`ref` is the only sharing mechanism visible in a type. Closures capture variables by name and globals are shared by name — those are the only other places where two names can see one slot, and the only ones that need coordination under concurrency."* A regra 8 da §2.2 deixa de dizer "orthogonal to value semantics".

**Consequências diretas:** `print(r)`, `f"{r}"` e `to_str(r)` com `r: ref T` — `print` e `to_str` recebem a referência e imprimem `<ref …>`/`null` (é o que `ObjRef.String()` já faz); f-string mostra `<ref …>` como `to_str` (é `to_str(r)` depois do parser). `addr(ref x)` fica. `let v = r` (inferência) dá `v: ref T`. `let v: T = r` → erro (R2).

### 3.1 Tabela antes × depois

`x, z, n: int`; `r, s: ref int`; `xs: int[]`; `f(p: ref int)`; `g(p: int)`.

| Código | v0.18.0 | v0.19.0 |
|---|---|---|
| `let m: int = r` | lê → 10 | **erro**, hint `*r` |
| `let v = r` | `v: ref int` | igual |
| `n = r` | erro, hint `*r` | igual |
| `n = *r` | lê | igual |
| `r + 1`, `-r`, `!b` (`b: ref bool`) | lê | **erro**, hint `*r` |
| `if r then` (`r: ref bool`) | lê | **erro**, hint `*r` |
| `for x in r` (`r: ref int[]`) | itera 0× | **erro**, hint `*r` |
| `xs[ri]` (`ri: ref int`) | lê | **erro**, hint `*ri` |
| `r == 10` | erro, hint `*r` | igual |
| `r == s`, `r == null` | identidade / ref é null | igual |
| `*r = 20`, `*r = *s` | escreve | igual |
| `*r = s` | lê `s` | **erro**, hint `*s` |
| `*r = ref z` | lê `z` (sem efeito) | **erro**, hint rebind/update |
| `r = ref z` | rebind | igual |
| `r = 50` | erro, hint `*r = 50` | igual |
| `f(x)` | empresta `x` | **erro**, hint `ref x` |
| `f(ref x)`, `f(r)`, `f(null)` | ok | igual |
| `f(ref r)` | encaminha `r` | **erro**, hint `pass 'r' directly` |
| `g(r)` | lê | **erro**, hint `*r` |
| `append(xs, 1)` | empresta `xs` | **erro**, hint `ref xs` |
| `append(ref xs, 1)` | ok | igual |
| `print(r)` | imprime 10 | imprime `<ref …>` |
| `f"{r}"` | interpola 10 | interpola `<ref …>` |
| `return r` (retorno `int`) | lê | **erro**, hint `*r` |
| `r.f`, `r[i]`, `r.f = v`, `ref r.f` | atravessa | igual |
| `let q: ref ref int` | parser aceita | **erro** de parser |
| `*x` (`x: int`) | erro estático | igual |
| `*a` (`a: any` com int) | passa adiante | **erro** runtime `cannot dereference int` |
| `ref a.f` (`a: any`, slot já ref) | encaminha | **erro** runtime `already holds a reference` |

## 4. Diagnósticos

Formato existente: mensagem + linha `hint:` (ver `derefReadHint`, `referenceAssignmentTypeError`, `referenceSlotAssignmentTypeError` em `compiler.go`). Todo erro novo aponta a correção.

| Situação | Mensagem | Hint |
|---|---|---|
| R2, qualquer posição com `r` identificador | `type mismatch …: expected T, got ref T` (texto da posição, como hoje em assignment) | `use '*r' to read the referenced value` |
| R2, RHS não-identificador (`a.f`, `xs[i]`) | idem | `use '*' to read the referenced value` (forma genérica já existente) |
| R2, `for x in r` | `cannot iterate over ref T[]` | `use 'for x in *r'` |
| R2, índice | `array index must be int, got ref int` | `use 'xs[*ri]'` |
| R5, função/construtor/`func` | `argument N of 'f' expects ref T, got T` | `use 'ref x'` |
| R5, builtin | `append expects ref T[] as argument 1, got T[]` | `use 'append(ref xs, …)'` |
| R5, temporário | `reference argument '…' is not addressable` (existente) | existente |
| R1, `ref r` | `'r' is already a reference` | `pass 'r' directly` |
| R1, anotação `ref ref T` | `'ref ref' is not a type` (parser) | `a reference is never taken to a reference` |
| R6, `*r = ref y` | `cannot assign ref T to T` | `use 'r = ref y' to rebind, or '*r = y' to write the value` |
| R3, `*x` estático | `cannot dereference int` (existente) | — |
| R3, runtime | `cannot dereference int` | — |
| `ref` em ref-slot via `any` (runtime) | `slot 'f' already holds a reference` | `pass it directly` |

`r == 1`, `r = 50`, `x.next = n`, `n = r` continuam com as mensagens e hints atuais. `validateParameterModes` (runtime, `OP_CALL`) inalterado: continua a barreira para `func` bare e `any`.

## 5. Compilador e VM

### 5.1 Compilador (`internal/compiler`) — remoção

| O quê | Onde (em `3c579ae`) | Ação |
|---|---|---|
| Auto-deref em `let` | `compiler.go:422` | vira erro R2 |
| Auto-deref do RHS de `*r =` | `compiler.go:542`; e `:541` (`*r = ref z`) | erro R2 / R6 |
| Auto-deref de índice | `compiler.go:732, 1089, 1117, 2638`; `cow_lowering.go:56`; `typed_index.go:143` | erro R2 (índice) |
| Auto-deref de operando binário/unário | `compiler.go:1267, 1280, 1494` | erro R2 |
| Auto-deref de condição | `compiler.go:1553, 1617` (+ caminhos fundidos, se aceitam `ref bool`) | erro R2 |
| Auto-deref de `return` | `compiler.go:2140` | erro R2 |
| Auto-deref de argumento | `compiler.go:2507` (geral), `builtin_calls.go:23` | erro R2 |
| **Mantidos** | `compiler.go:1487` (`*r`), `:991` (base de `.`), `:1102` (base de `[]`); `OP_DEREF_MUT` em `cow_lowering.go:85-92`; fused `typed_index.go:88-130` | R3/R4 |
| Criação implícita de ref | `compileReferenceArgumentValue` `compiler.go:2567-2684`: remover o strip do prefixo `ref` (`:2568-2570`) e os ramos de forwarding (`:2576, :2591, :2600, :2621, :2659, :2667`); chamadores `compiler.go:2457` e `builtin_calls.go:67/76/111/122/159` passam a **checar** `argType` é `ref T`/`null` e compilar o argumento como valor comum; só `PrefixExpression "ref"` (`:1466`) chama o criador | R5 |
| Forwarding em `ref e` | `compiler.go:1466-1471` | `e` de tipo `ref T` → erro R1 |
| `for … in` | `compiler.go:1652-1735` + `forEachElementType` (`generics_structs.go:541`) | coleção `ref` → erro R2; `forEachElementType` deixa de desembrulhar |
| Parser `ref ref T` | `parser.go:551-557` | erro R1 |
| Helpers | `unwrapRefType`, `indexElementType`, `arrayTypeOf`, `memberType` | ficam onde servem R4 (base); somem onde serviam operando |

`OP_CONTEXT_REF_PROPERTY`/`OP_CONTEXT_REF_INDEX` continuam sendo emitidos para **ler** um slot `ref T` (é uma leitura de valor que por acaso é ref) — o opcode fica; o que some é o compilador escolhê-lo *em vez de* criar um ref quando o usuário escreveu `ref`.

`modesProven`/`OP_CALL_STATIC` (`compiler.go:2449, 2510-2531`): inalterados; com R5, `isExact` continua provando modos porque os tipos estáticos dos argumentos passam a bater exatamente.

### 5.2 VM (`internal/vm`) — dois ajustes

| Opcode | Onde | Hoje | Depois |
|---|---|---|---|
| `OP_DEREF`, `OP_DEREF_MUT` | `executor.go:646-660`, `:1752-1768` | não-ref passa adiante ("tolerância herdada do auto-deref antigo") | `VAL_NULL` → null reference error (como hoje); qualquer outro não-`VAL_REF` → `cannot dereference <tipo>` |
| `OP_REF_PROPERTY`, `OP_REF_INDEX` | `executor.go:426-433`, `:479-487` | se o slot é ref-slot (`RefFields`/tag `(ref T)[]`), espelha `OP_CONTEXT_REF_*` (encaminha) | ref-slot → erro `slot '…' already holds a reference` (hint `pass it directly`). O auto-deref **da base** (`ref a.f` com `a` ref) permanece: é R4 |

Sem mudança em: `OP_EQUAL`/`valuesEqual`, `OP_GET_PROPERTY`/`OP_SET_PROPERTY`/`_MUT` (R4 via `any`), `OP_LEN`, `index_ops.go`, builtins, `validateParameterModes`, RC/CoW, fast paths (`ParamsUntracked`, `popSimpleFrame`, superinstruções — nenhum depende de auto-deref).

**Perf:** esperado delta zero; verificado com `benchmarks/interleaved_compare.ps1` base × head antes do merge (protocolo do `RESULTS.md`).

## 6. Corpus e migração

Ordem: compilador com testes Go primeiro (§8); corpus depois, guiado pelos erros.

1. **Codemod** (script no scratchpad, não commitado) para os 4 builtins sobre `noxy_examples/`, `noxy_libs/`, `benchmarks/`, `tests/`: `append(IDENT,` → `append(ref IDENT,`; `pop(IDENT)`; `delete(IDENT,`; `json_loads(…, IDENT)` — só quando o argumento é identificador simples ou caminho `a.b`/`a[i]` e não começa por `ref `/`null`. Cada arquivo tocado é revisado no diff: o codemod erra quando o identificador já é `ref T[]` (parâmetro `xs: ref int[]` → `append(xs, v)` **fica sem `ref`**, R5). O compilador pega os erros do codemod (`'xs' is already a reference`).
2. **Manual, guiado pelo compilador**: `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx`; para cada FAIL, aplicar o hint. Estimativa (levantamento): ~60–90 linhas de `*` em ~20 arquivos; ~27 call sites escalares e ~120–180 compostos com `ref`. Arquivos que **demonstram** o comportamento antigo (`test_autoderef.nx`, `pattern_dynamic_aliases.nx`, `smart_pointers.nx`, `pattern_mutable_bindings.nx`, `consistency_demo.nx`, `KandR_in_noxy/ch05_*`) são reescritos para a forma nova — viram a demonstração de R1–R6, não são apagados. `test_autoderef.nx` é renomeado para `test_explicit_read.nx`.
3. **Exemplos de erro** (`noxy_examples/type_errors/`): atualizar `typed_function_invalid_ref_argument.nx` e adicionar um por diagnóstico novo (R1 `ref r`, R2 `for`, R2 f-string, R5 builtin, R6 `*r = ref`), com a mensagem esperada em `function_conformance_examples_test.go`.
4. `noxy_libs/github_com/estevaofon/quicksort/quicksort.nx`, `benchmarks/*.nx`, `benchmarks/cross_runtime/*.nx`: idem — precisam compilar para a medição de perf.
5. **Critério de aceite**: runner com 0 FAIL fora da lista `should_ignore`; e, como o runner só olha exit code, **diff de saída base × head** (v0.18.0 × head, mesmo subconjunto que `RESULTS.md` usa: "146 iguais") — as saídas devem ser idênticas, exceto nos arquivos onde `print(r)` passou a mostrar `<ref …>` intencionalmente (lista explícita no PR).

## 7. Docs e versão

| Arquivo | Ação |
|---|---|
| `docs/NOXY_LANGUAGE_SPEC.md` | **§2.3 reescrita inteira** (R1–R9, tabela 3.1, diagnósticos §4; meia página em vez de duas); §2.2 regras 6 e 8 + Zen (R10); §3 (`let v: T = r`); **§4.2** remover "contextual conversion" e forwarding (linhas 678-698), ajustar 728, 749; §4.3; §6.5 (`ref T` em generics, só o exemplo); §7 (condição `ref bool`, `&&`/`\|\|`); §8 (:1738 "read as T (auto-dereference)", :1754-1757); "Pattern C: Smart Pointers" e demais exemplos com `(auto-deref)` reescritos. A nota "Take refs after, not before, sharing" (§2.2) **fica**, com link para a issue de follow-up |
| `docs/REF_SEMANTICS.md` | reescrever: as 3 formas (`ref x`, `r`, `*r`) + R4 + R9; sai "conversão contextual", "fronteiras dinâmicas" (não há mais distinção), "leitura automática" |
| `README.md` | `checkout(ref mine)`, `append(ref …)` nos exemplos; Zen (R10); "Features" idem |
| `docs/SHOWCASE.md`, `docs/concurrency.md` | exemplos com `append`/call sites; concurrency ganha o exemplo de R9 (closure + `ref` a local + `spawn`) |
| `AGENTS.md` | convenção de `ref`/`*` para quem escreve exemplos; corrigir `docs/CONCURRENCY.md` → `docs/concurrency.md` (:320) |
| `CHANGELOG.md` | `## [0.19.0] - <data>`; `### Changed (BREAKING) — ref explícito: \`*r\` para ler, \`ref x\` em todo call site (issue #82)`; tabela 3.1 resumida; lista de hints; "Removed": auto-dereference, conversão contextual, forwarding form; "Fixed": `for x in r` zero-iteração |
| `docs/index.html` e site | meta/descrição se citar auto-deref (levantamento: não cita); exemplos com `append` |
| `internal/version/version.go` | `v0.19.0` — commit próprio `chore(version): …` no padrão de `26a44e6` |
| Issues | #82 (principal) fechada pelo PR; #81 fechada pelo PR (R9); **follow-ups abertos pelo plano**: (a) `ref` em container × CoW, (b) `ref T` não-nulo / `ref T?` |

Branch `feature/issue-82-explicit-ref` a partir de `main`; PR para `main`; `develop` sincronizado depois (convenção observada: `main` e `develop` idênticos em `3c579ae`).

## 8. Testes

TDD: cada diagnóstico entra RED (teste do erro + hint) **antes** de remover o caminho implícito correspondente.

**Compilador (`internal/compiler`):**

| Arquivo | Ação |
|---|---|
| `ref_operand_semantics_test.go` (VM) e novo `explicit_read_test.go` (compiler) | hoje travam o auto-deref; passam a travar R2 por construto: binário, unário, `if`/`while`, `for`, índice, argumento, `return`, `let x: T = r`, `*r = s`; e que `print`/`to_str`/f-string mostram a referência e `any` aceita um ref como valor |
| `reference_arguments_test.go` | `:78` (forwarding via `OP_CONTEXT_REF_*` quando o usuário escreveu `ref`) → erro R1; novos: `f(x)` → erro R5 com hint; `f(r)`, `f(null)`, `f(ref x)`, `f(a.next)`, `f(g())` com `g -> ref T` compilam; builtins R5 |
| `assign_deref_hint_test.go`, `ref_equality_strict_test.go` | ficam (comportamento igual); acrescentar `let n: int = r` e `*r = s` ao primeiro |
| `ref_base_field_assignment_test.go` | fica; acrescentar `*x.next = ref n` → erro R6 |
| novo `ref_of_ref_test.go` | `ref r` erro R1; parser `ref ref T` erro; `*r = ref y` erro R6; `*x` estático |
| `function_conformance_examples_test.go` | mensagens novas dos fixtures de `type_errors/` |
| `cow_lowering_test.go`, `typed_index_compile_test.go` | asserções de `OP_DEREF`/`OP_DEREF_MUT` ajustadas: índice `ref int` deixa de existir nos fixtures |

**VM (`internal/vm`):** `OP_DEREF`/`OP_DEREF_MUT` em não-ref → erro; `OP_REF_PROPERTY`/`OP_REF_INDEX` em ref-slot → erro; `print(r)`/`to_str(r)` → `<ref …>`; `ref_null_forwarding_test.go` fica (null continua passando); `malformed_reference_test.go` fica.

**Corpus:** runner 0 FAIL; diff de saída base × head (§6, item 5).

**Perf:** `interleaved_compare.ps1` sem regressão; `go test -race ./internal/value ./internal/vm` verde; guards de arquitetura verdes.

**Comandos de validação (AGENTS.md):** `go test ./...`; `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx`; `go build -o noxy ./cmd/noxy`.

## 9. Riscos

| Risco | Mitigação |
|---|---|
| Codemod insere `ref` em argumento já `ref T[]` | compilador acusa R1; revisão do diff por arquivo |
| Site de auto-deref não listado (caminho fundido, superinstrução) aceita `ref` silenciosamente | testes R2 por construto **na VM** (valor em runtime), não só no chunk — lição do `ParamsUntracked` (`RESULTS.md`) |
| Mensagem de erro sem hint em alguma posição nova | teste por posição exige o hint |
| Exemplo de doc desatualizado | grep final por `auto-deref`, `contextual`, `forward` em `docs/`, `README.md`, `AGENTS.md`, `CHANGELOG` (só entradas antigas podem citar) |
| Regressão de perf por checagem nova em caminho quente | todas as checagens novas são de compilação; VM só troca "passa adiante" por erro em caminho que já era o lento |
