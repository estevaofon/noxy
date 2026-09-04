# stdlib `math`, `m.f()` tipado por namespace, `remove_at`/`swap_remove`, aspas em `{}` de f-string e keywords do §1.2 (issue #126)

**Data:** 2026-09-03 · **Branch:** `feat/issue-126-math-namespace-typing`, a partir de `develop` (pós #125, v0.23.2)
**Status:** aprovado, em implementação neste PR · **Issue:** [#126](https://github.com/estevaofon/noxy/issues/126) · **Relação:** #58 item 1 (tradução de tipo módulo → programa, `programViewType`), #121 (argumento inválido é erro tipado, nunca sentinela), #122 (contrato de retorno dos natives; a classe "tipo desconhecido" encolhe com o item 2).

Cinco achados do Deadrail (jogo em Noxy sobre o `noxy_game_engine`), cada um com decisão ancorada em precedente de linguagem estabelecida. Os itens são independentes entre si e podem ser implementados e testados em qualquer ordem; a ordem sugerida no plano é 5 → 4 → 3 → 1 → 2 (do mais local ao mais transversal).

## 1. Módulo `math` na stdlib

### Forma

Mesmo molde de `strings`: `internal/vm/builtins_math.go` registra natives `math_<nome>` em `defineMathBuiltins()` (chamado em `builtins.go`), e `internal/stdlib/math.nx` expõe wrappers tipados. Nenhum bytecode novo. Constantes são `let` de topo do wrapper, como `io.SEEK_SET`.

### Conjunto

| Função | Assinatura | Native |
|---|---|---|
| `sqrt`, `cbrt` | `(x: float) -> float` | `math_sqrt`, `math_cbrt` |
| `pow` | `(x: float, y: float) -> float` | `math_pow` |
| `abs` | `(x: float) -> float` | `math_abs` |
| `floor`, `ceil`, `round`, `trunc` | `(x: float) -> float` | `math_floor`, `math_ceil`, `math_round`, `math_trunc` |
| `fmod` | `(x: float, y: float) -> float` | `math_fmod` |
| `sin`, `cos`, `tan`, `asin`, `acos`, `atan` | `(x: float) -> float` (radianos) | `math_sin` … `math_atan` |
| `atan2` | `(y: float, x: float) -> float` | `math_atan2` |
| `hypot` | `(x: float, y: float) -> float` | `math_hypot` |
| `exp`, `log`, `log2`, `log10` | `(x: float) -> float` | `math_exp`, `math_log`, `math_log2`, `math_log10` |
| `min`, `max` | `(a: float, b: float) -> float` | `math_min`, `math_max` |
| `clamp` | `(x: float, lo: float, hi: float) -> float` | `math_clamp` |
| `abs_int` | `(x: int) -> int` | Noxy puro, sem native |
| `min_int`, `max_int` | `(a: int, b: int) -> int` | Noxy puro |
| `clamp_int` | `(x: int, lo: int, hi: int) -> int` | `math_clamp_int` (native: código Noxy não levanta erro de runtime, e `lo > hi` tem de errar como no `clamp` de float) |
| `PI`, `E` | `float` | `let` no wrapper |

Precedentes: Go `math` (float-only; `round` afasta de zero na metade), C# `Math.Abs/Min/Max/Clamp` e Rust `i64::abs/min/max/clamp` para as versões inteiras. Noxy não tem sobrecarga, então o sufixo `_int` segue o que Go faz com `strconv.Itoa`/`FormatInt`. `clamp(x, lo, hi)` com `lo > hi` é erro de domínio (Rust faz `panic`; C# lança).

### Domínio inválido é erro de runtime tipado, não NaN

Regra: cada native checa a precondição do domínio antes de chamar o Go e devolve `error` (vira `native 'math_sqrt' failed: math.sqrt: domain error, got -1`). Precedentes: o próprio Noxy faz `1.0 / 0.0` ser erro de runtime, não `+Inf` (`executor.go`, `OP_DIV_FLOAT`); a #121 decidiu que argumento inválido é erro, nunca sentinela; Python `math` lança `ValueError: math domain error`.

| Função | Precondição |
|---|---|
| `sqrt` | `x >= 0` |
| `log`, `log2`, `log10` | `x > 0` |
| `asin`, `acos` | `-1 <= x <= 1` |
| `fmod` | `y != 0` |
| `pow` | não (`x == 0 && y < 0`) e não (`x < 0 && y` não inteiro) |
| `clamp`, `clamp_int` | `lo <= hi` |

Overflow para `±Inf` (`exp(1000.0)`, `pow(10.0, 400.0)`) **não** é checado, como o overflow de `int` da spec §8. Consequência: NaN não nasce do `math`; só de aritmética com `Inf`. Por isso não há `is_nan`/`is_inf` nesta rodada.

Tipo do argumento: os natives exigem `VAL_FLOAT` (`math.sqrt: x must be a float, got int`), como `crypto` pós-#121. O wrapper `x: float` já rejeita `int` em compilação pelo `select` e, após o item 2, também pelo namespace.

### Colisão com `noxy_examples/math.nx`

Um `.nx` local sombreia a stdlib (`AGENTS.md`, resolução de `use`). `noxy_examples/math.nx` existe (`add`, `multiply`, `factorial`) e é importado por `test_import.nx` e `test_import_all.nx`. Renomear para `noxy_examples/math_module_example.nx` e atualizar os dois `use`; o exemplo da stdlib nova é `noxy_examples/test_math_stdlib.nx`.

### Registros a atualizar

`builtins_registry_test.go` (snapshot ordenado), `architecture_test.go` (`builtins_math.go` → `defineMathBuiltins`), `stdlib_hygiene_test.go` (só confere; wrapper só chama native registrado).

## 2. `m.f(...)` e `m.x` com tipo estático pelo namespace

### Hoje

`importNamespace` põe o módulo em `c.globals[alias] = nil`; `MemberAccessExpression` com base `nil` devolve tipo `nil`; `compileCallExpression` com `fnType == nil` não confere aridade, argumentos nem retorno e devolve `nil`. `let v = m.roll(6)` cai em "cannot infer type". O `select` conhece a assinatura por `importBindingFrom` → `newFunctionType`.

### Mecanismo

Em `MemberAccessExpression` (leitura, `compiler.go` ~1059), antes de compilar a base: se `n.Left` é `*ast.Identifier` que **não** resolve para local nem upvalue e está em `c.namespaceImports`, o tipo do membro é

```
importedBindingType(módulo, membro)  →  programViewType(tipo, módulo)
```

`importedBindingType` já devolve o mesmo `ast.NoxyType` que o `select` registra (função, construtor de struct, `let` de topo com anotação ou inferido). `programViewType` (regra da #58 item 1) traduz nome a nome para a visão do programa: `V` → `vec.V` (primeiro alias declarado), mantém `V` se houve `select V`, e devolve `(nil, false)` quando qualquer parte do tipo não é nomeável ou é instância genérica interna do módulo — o tipo inteiro fica desconhecido, como hoje. Template genérico exportado (`c.moduleExportsGenericTemplateName`) continua sem tipo pelo namespace: a chamada já é erro de compilação com hint de `select`.

O bytecode não muda: continua `OP_GET_PROPERTY` no objeto módulo. Só o tipo estático devolvido muda.

### Efeito em cadeia

Com `c.Compile(call.Function)` devolvendo `*ast.FunctionType`, `compileCallExpression` entra no caminho `isExact`: aridade, `areStrictTypesCompatible` por argumento, `emitSlotGuards`, e tipo de retorno. Nada de novo é escrito ali; o caminho já existe para o `select`. Único ajuste de mensagem: `callableName` passa a imprimir `m.roll` (hoje `MemberAccessExpression.String()` dá `(m.roll)`), para `argument 1 to 'm.roll': expected int, got string`.

| Programa | Antes | Depois |
|---|---|---|
| `let v = m.roll(6)` (`roll -> int`) | erro "cannot infer" | `v: int` |
| `let s: string = m.roll(6)` | compila, falha em runtime | `type mismatch … expected string, got int` |
| `m.roll("x")` | compila, falha em runtime | `argument 1 to 'm.roll': expected int, got string` |
| `m.roll(1, 2)` | compila, falha em runtime | erro de aridade |
| `let p = vec.norm(v)` (`norm -> V`) | erro "cannot infer" | `p: vec.V` |
| `let t: bytes = crypto.aes256_gcm_decrypt(k, d)` | compila, recebe null em silêncio | `expected bytes, got bytes?` |
| `let n = counter.total` (`let total: int` no módulo) | erro "cannot infer" | `n: int` (leitura viva, como antes) |
| `m.f()` com retorno que o programa não sabe nomear | dinâmico | dinâmico (inalterado) |

### Quebra deliberada

Programa que hoje escreve tipo errado por namespace e "funciona" porque nunca executa o caminho errado passa a não compilar. É o "compilador fala primeiro" do AGENTS.md. Entra em `Changed (BREAKING)` no CHANGELOG com tabela Antes/Agora e migração (`let x: T? = m.f()` para os `T?` da stdlib; corrigir o tipo nos demais). Oráculo: os 229 exemplos do runner, os `.nx` de `tests/`, `noxy_libs/` e `internal/stdlib/` (módulos da stdlib chamam uns aos outros por namespace: `http` → `net`, `strings`), e a suíte Go. Os testes de `member_access_typing_test.go` que hoje documentam "via namespace é dinâmico, por isso o resultado passa por `let` anotado" mudam de expectativa.

### Interações a cobrir em teste

- alias duplo (`use m as a` + `use m as b`): retorno de struct nomeado pelo **primeiro** alias; ainda compatível com o segundo (`typesEquivalent` por ponteiro).
- namespace + `select` do mesmo struct: retorno nomeado pela forma do `select` (regra 1 da #58).
- `m.x` onde `x` é `let` inferido no módulo (`let total = 0`): tipo `int` pelo `inferGlobalLetTypes` do módulo.
- `m.T(...)` construtor: parâmetros traduzidos; retorno `m.T`.
- `m.f` como valor (sem chamar): `let g = m.f` bind com `func(...) -> ...` traduzido; se não nomeável, erro de inferência como antes.
- local ou upvalue que sombreia o alias: sem mudança (o namespace não é consultado).
- REPL: `use m` numa linha e `let v = m.f()` na seguinte (`ModuleState` carrega `namespaceImports`).
- corpo de função genérica (compilador de rascunho `generics.go` copia `namespaceImports`).
- módulo não carregável (`TestFunctionBodyOnlyWildcardDoesNotAffectModuleLoadability`): tipo `nil`, sem erro novo.
- `m.f()` dentro de condição de narrowing: chamada exata de função declarada não é builtin puro; comportamento igual ao `select`.

**Validação adversarial obrigatória** (memória do projeto): antes de fechar, um agente sem o contexto desta spec procura um caso a mais em que o namespace tipado produza tipo errado, nome que o programa não consegue escrever, ou regressão de compilação. Achar caso conta como sucesso.

## 3. Aspas dentro de `{}` de f-string

Precedente: PEP 701 (Python 3.12) — dentro de uma expressão interpolada, aspas iguais às do delimitador não fecham a string.

`readQuoted(quote, literalFString)` em `lexer.go` passa a contar profundidade de chaves **só para `literalFString`**:

- profundidade 0: `{{` e `}}` são copiados como estão (o parser já os trata como escape) e **não** alteram a profundidade; `{` isolado incrementa; a aspa delimitadora encerra o literal, como hoje.
- profundidade > 0: `{` incrementa, `}` decrementa; `"` ou `'` abre um literal aninhado copiado verbatim (inclusive `\"` e demais escapes, sem interpretar) até a aspa igual; a aspa delimitadora externa **não** encerra.
- EOF com profundidade > 0 ou dentro de aninhado: `unterminated f-string`, como hoje.

O parser (`parseFString`) não muda: continua recebendo o literal inteiro e re-lexando o conteúdo de cada `{...}`. Como o conteúdo aninhado é copiado verbatim, `f"{fmt("%03d", n)}"` chega ao sub-lexer como `fmt("%03d", n)`.

Mensagem residual: `unclosed brace in f-string` ganha `hint: every '{' that starts an expression needs a matching '}'; write '{{' for a literal brace`. Spec §9: o parágrafo "Double quotes inside `{}`" sai; entra o exemplo `f"{fmt("%03d", n)}"`. `TestFStringBraceEscapesAndTrailingTokenError` perde o comentário sobre a limitação e ganha os casos novos.

Escapes `\{` já existentes seguem iguais (não entram na contagem: são consumidos por `readEscape`).

## 4. `remove_at(ref arr, i) -> T` e `swap_remove(ref arr, i) -> T`

Precedentes: Rust `Vec::remove(i)` / `Vec::swap_remove(i)` (devolvem o elemento; `panic` fora do intervalo), Swift `remove(at:)`, C# `RemoveAt`. Go mantém `delete` só de map e põe remoção de slice em `slices.Delete`, então `delete` polimórfico fica descartado. Devolver o elemento é consistente com `pop`.

- **VM** (`builtins_collections.go`, `defineCollectionBuiltins`): `DefineContextualNativeWithSignature` com `Params: [{IsRef: true, TypeName: "ref array"}, {TypeName: "int"}]`, `ReturnType: "any"`. Corpo: `unicizeThroughRefValue(args[0])` (único funil de CoW, como `pop`), índice `VAL_INT`, `i < 0 || i >= len` → `error("array index out of bounds")` (mesma mensagem da indexação), remove (`remove_at`: `copy` + encolhe; `swap_remove`: troca com o último e encolhe), `value.Release` do removido (`// RC: o array solta a posse durável`), devolve o valor.
- **Compilador** (`builtin_calls.go`): entram no filtro de nomes e na tabela de aridade (2). Argumento 1 por `compileBuiltinRefArgument(..., "ref T[]")`, erro `remove_at expects an array, got map[string, int]`; argumento 2 por `compileBuiltinValueArgument` com `int` estrito. Retorno `array.ElementType`, como `pop`. Entram em `pureBuiltins` (narrowing) e no snapshot do registry. Um `func remove_at` do programa sombreia o builtin (regra do `range`).
- **Testes**: `builtins_collections_test.go` (unidade, inclusive fora do intervalo e negativo), `cow_builtins_test.go` (`TestRemoveAtUnicizesSharedTarget`, `TestSwapRemoveUnicizesSharedTarget`, molde de `TestPopUnicizesSharedTarget`), `native_signatures_test.go` (assinaturas fixadas), `builtin_calls_test.go` (contratos de aridade, endereçabilidade e tipo), `container_owners_test.go` para RC de elemento composto removido, `noxy_examples/type_errors/remove_at_without_ref.nx`.
- **Spec §10 Collections**: duas linhas novas, com a nota de ordem (`swap_remove` não preserva ordem, O(1)).

## 5. Keywords fora do §1.2 e keyword em posição de nome

- **`str` deixa de ser keyword.** `TYPE_STR` só existe em `token.go` e `display.go`; o parser nunca o consome. Reserva morta que só custa o identificador. Remover a entrada de `keywords`, a constante e o `display`. Afrouxamento seguro: nenhum programa válido muda.
- **§1.2**: linha "Types" passa a listar `any`, `map`, `chan` (com `map` e `chan` como construtores de tipo, `any` como tipo dinâmico).
- **Mensagem**: em `expectPeek(token.IDENTIFIER)`, quando o `peekToken` é uma keyword (`token.LookupIdent(literal) != IDENTIFIER`), o erro vira `[l:c] SyntaxError: 'map' is a keyword and cannot be used as a name` com `hint: rename it (e.g. 'level' or 'game_map')`.
- **Sem cascata**: no mesmo caso, o parser marca `syncToNextLine`; `ParseProgram`, ao receber statement `nil` com a marca ligada, avança tokens até o primeiro cujo `Line` seja maior que o do erro. Precedente: recuperação em modo pânico com ponto de sincronização (Crafting Interpreters §6.3.3; o parser do Go sincroniza em início de statement). A marca é específica deste erro para não alterar as mensagens que `syntax_errors_test.go` já fixa.
- Testes: `syntax_errors_test.go` (`use src.map as map` e `let map: int = 1` produzem **um** erro cada), `lexer_test.go` (`str` é `IDENTIFIER`), `expect_peek_errors_test.go`.

## 6. Documentação

| Onde | O quê |
|---|---|
| spec §1.2 | `any`, `map`, `chan` na linha Types; `str` fora |
| spec §3 | linha da tabela "Write instead" do namespace: some (o caso deixa de ser erro); nota de que `m.f()` e `m.x` têm o tipo declarado pelo módulo, traduzido |
| spec §8 | ponteiro: `%` é de `int`; `fmod`, `floor` etc. em `math` (§12) |
| spec §9 | remove "Double quotes inside `{}`"; exemplo com aspas iguais; hint novo |
| spec §10 | `remove_at`, `swap_remove` |
| spec §11 | seção "Member access through a namespace": tipo do membro, tabela de tradução (reusa a da #58), quebra |
| spec §12 | linha `math` na tabela; seção `### Math (\`math\`)` com assinaturas, domínio e `round` |
| CHANGELOG | `Added` (math, remove_at/swap_remove, f-string, `str` livre, mensagem de keyword) e `Changed (BREAKING)` (item 2) com Antes/Agora e migração |
| README, `docs/index.html` | `math` nas listas de módulos |

Sem bump de versão neste PR (fica para a release).

## 7. Verificação

Por item, TDD: teste vermelho → implementação → verde. Ao final: `go build ./... && go vet ./...`, `go test ./internal/... -count=1`, `go test ./cmd/... -count=1`, `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx`, `gofmt -d` nos arquivos tocados, `git diff --numstat` sem arquivo reescrito por EOL. Para o item 2, adicionalmente a revisão adversarial descrita acima e uma varredura de compilação de todos os `.nx` do repositório fora do runner (`tests/`, `noxy_libs/`, `internal/stdlib/`).
