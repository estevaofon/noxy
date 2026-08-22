# VM Perf — Strings: fast path ASCII + `to_str(int)` (issue #66, item 2, etapa 1)

**Data:** 2026-08-22 · **Issue:** [#66](https://github.com/estevaofon/noxy/issues/66) item 2 · **Branch:** `perf/issue-66-string-ascii-fastpath` · **Base:** `develop` (73cf11a, v0.15.0)

## 1. Contexto (medido nesta sessão, binário de 73cf11a)

Perfil de `cross_runtime/string_ops.nx` com 10x as iterações (1,57 s, amostras
de 10 ms; a versão de 200 k itera em ~290 ms e dá 36 amostras — inútil):

| componente | % do tempo | o que é |
|---|---|---|
| `to_str(i)` (`defineCoreBuiltins.func5`) | **24 %** | `Value.String()` → `fmt.Sprintf("%d")` (11,5 %, com boxing do int e o `pp` do fmt) + `requireValidUTF8` sobre o resultado + `NewString` |
| `strings_substring` (`defineStringBuiltins.func17`) | 11,5 % | `[]rune(s)` (alocação), `string(runes[a:b])` (segunda alocação); mais o wrapper Noxy de `stdlib/strings.nx` (`substring` → `strings_substring`), que custa um frame inteiro |
| `callNative`/`ObjNative.Invoke` | 43 % cum | protocolo de chamada de builtin — inclui os dois acima; é o item 3 |
| `length(s)` → `utf8.RuneCountInString` | **1,3 %** | já é varredura por byte sem alocar, com caminho ASCII interno |
| `mallocgc` | 16,5 % | `NewString` (boxing da string em `interface{}` — etapa 2), `[]rune`, `Sprintf` |

A hipótese da issue (`length` O(n) + `[]rune` em `substring`) cobre ~12 % do
bench; o maior termo isolado é `to_str(int)` via `fmt.Sprintf`. `length` não
tem o que ganhar: trocar `RuneCountInString` por "varre e usa `len(s)`" faz a
mesma varredura.

## 2. Objetivo e não-objetivos

**Objetivo:** sem mudar a representação de string (Go `string` em `Value.Obj`),
eliminar as alocações de `[]rune` nas operações indexadas por rune quando a
string é ASCII, e tirar o `fmt` do caminho de `to_str(int)`. Semântica, saída e
mensagens de erro **idênticas** em ASCII e não-ASCII.

**Não-objetivos:** string boxada com tamanho/flag cacheados (etapa 2 — só com
número, noutro PR); `length` (medido, não ganha); eliminar o wrapper Noxy de
`substring`/`char_at` (protocolo de chamada, item 3); `to_str(float)` (risco de
divergência de formatação, ganho não medido); `reverse`/`ord`/`codes` (não
estão em caminho quente).

## 3. Desenho

### 3.1 `isASCII(s string) bool` (`internal/vm/strings_ascii.go`)

Varredura por byte, `s[i] >= utf8.RuneSelf` → false. Sem alocação. É chamada
de builtins e de `getIndexGeneric` (método), nunca de dentro de `run()` — o
orçamento de inline relevante é o normal (80), não os 20 da big function;
conferir com `go build -gcflags=-m=2` e travar em `inline_guard_test.go`.

Propriedade que sustenta a equivalência: **todo byte inválido em UTF-8 é
≥ 0x80**, logo nenhuma string não-ASCII (válida ou não) entra no fast path; o
caminho lento é o código atual, intocado.

### 3.2 Sites com fast path (se `isASCII(s)`; senão, código atual sem mudança)

| site | hoje | fast path |
|---|---|---|
| `strings_substring` (`builtins_strings.go`) | `runes := []rune(s)`; `n = len(runes)`; clamp; `string(runes[start:end])` | `n = len(s)`; mesmo clamp; `s[start:end]` (substring compartilha bytes, zero alocação além do box) |
| `strings_char_at` | `[]rune(s)`; bounds; `string(runes[idx])` | bounds em `len(s)`; `s[idx:idx+1]` |
| `s[i]` (`index_ops.go`, `getIndexGeneric`) | `[]rune(str)`; bounds; `NewString(string(runes[idx]))`; erro "string index out of bounds" | bounds em `len(str)`; `NewString(str[idx:idx+1])`; **mesma mensagem de erro** |
| `slice(s, a, b)` (`builtins_collections.go`, caso string) | `[]rune(str)`; clamp; `string(runes[start:end])` | `len(str)`; mesmo clamp; `str[start:end]` |

O clamp/negativo/vazia é o mesmo código nos dois ramos — a única diferença é
`n` e a fatia. Para garantir isso sem duplicar a lógica, o clamp de
`substring` vira um helper puro `clampSubstringRange(start, end, n) (lo, hi, ok)`
usado pelos dois ramos (o de `slice` já é um helper local `clamp`).

### 3.3 `to_str(int)` (`internal/value/value.go`, `internal/vm/builtins_core.go`)

- `Value.String()` caso `VAL_INT`: `strconv.FormatInt(v.Int(), 10)` no lugar de
  `fmt.Sprintf("%d", v.Int())`. Saída idêntica para todo `int64` (`%d` sem
  flags é exatamente `FormatInt` base 10, inclusive `MinInt64`). Beneficia
  `print`, interpolação e concatenação também.
- `to_str`: quando `args[0].Type` é `VAL_INT`/`VAL_FLOAT`/`VAL_BOOL`/`VAL_NULL`,
  devolve `NewString(args[0].String())` sem `requireValidUTF8` — escalares
  renderizam ASCII por construção (o comentário no código já afirma isso; a
  validação só continua onde pode haver bytes: containers e o caso `VAL_BYTES`).

### 3.4 O que NÃO muda

Representação de string; `length`; RC (nenhum Retain/Release tocado — strings
nunca têm contador); opcodes (nenhum novo — nada aqui passa pelo compilador);
mensagens de erro; `requireTextArgument` (bytes continuam rejeitados antes do
fast path); comportamento com bytes inválidos (nunca entram no fast path).

## 4. Invariantes e guards executáveis

- Tabelas em `builtins_strings_test.go` / `index_ops` test / `builtins_collections` test cobrindo,
  para cada site: ASCII normal, vazia, fronteiras (`0`, `n-1`, `n`, `n+1`),
  negativos onde aceitos (`substring`/`slice`), não-ASCII com acento (2 bytes),
  emoji (4 bytes) e misto — com o resultado esperado escrito à mão, não derivado
  do código.
- `s[i]` fora de faixa: mensagem `string index out of bounds` nos dois ramos.
- `Value.String()` de `int`: `0`, `-1`, `MaxInt64`, `MinInt64` iguais a `%d`.
- `to_str(int)`/`to_str(bool)`/`to_str(null)` iguais a antes; `to_str(bytes inválidos)` continua a falhar com a mesma mensagem e offset.
- `inline_guard_test.go`: `isASCII` inlinável (≤ 80).
- `go test ./...`, `go test -race ./internal/value ./internal/vm`, corpus
  `run_all_tests_concurrent.nx` 0 falhas, `compare_examples.ps1` 0 divergentes.

## 5. Medição (protocolo de `benchmarks/RESULTS.md`)

Binários em disco local (scratchpad): `noxy_base.exe` (73cf11a) × `noxy_str.exe`
(head). `interleaved_compare.ps1 -Runs 9`; `run_cross_runtime.ps1 -NoxyBaseline`
(mínimo de 9); `compare_examples.ps1`; gates CoW ≤ +5 % (`bench_typed_call_map`,
`bench_share_mutate`, `bench_call_light`, `bench_conway`); `bench_generic_vs_hand`
como sentinela de `run()` (não deve mexer: nada aqui toca `run()`). A/B focado
de `string_ops` (11 intercaladas) e, se útil, um estágio intermediário (só
ASCII, sem `to_str`) para atribuir o ganho a cada parte.

**pwsh 7 não está nesta máquina**: os `.ps1` são UTF-8 sem BOM e o Windows
PowerShell 5.1 não os parseia (travessão em string). Cópias com BOM ao lado
dos originais (`benchmarks/_bom_*.ps1`, em `.git/info/exclude`, apagadas antes
do PR) rodam no 5.1 sem mudança de conteúdo — `$PSScriptRoot` continua
apontando para `benchmarks/`.

**Meta (hipótese):** `string_ops` 3,3x → ~2–2,5x do CPython. O que sobra depois
é protocolo de chamada (item 3) e boxing de string (etapa 2).

## 6. Riscos

- Divergência sutil entre ramo ASCII e ramo rune em clamps/negativos → helper
  compartilhado + tabela com fronteiras nos dois ramos.
- `FormatInt` vs `%d`: iguais por definição; teste com extremos trava.
- `to_str` sem validação para escalar: só escalares de fato (`Type` checado),
  nunca `VAL_OBJ`.

## 7. Decisões tomadas sem consulta (para a review)

- `slice()` de string entra (mesma família, mesmo padrão, custo zero); `length`
  fica de fora com a medição como justificativa.
- `to_str(float)` fica de fora.
- O wrapper Noxy de `substring` fica como está (item 3).
