# Benchmarks

Registro corrido das comparações de performance, mais recente primeiro. Cada
seção compara dois binários pelo protocolo intercalado (ver Reprodução no fim).

## Guards de slot `ref` no caminho `any` — issue #53 item 2 (medido em 2026-08-28, v0.21.0 + #104)

A #50 pôs um guard em toda escrita genérica: `arrayElementIsRefSlot` (um
`Load()` atômico da tag) em `OP_SET_INDEX` e `FieldIsRef` (lookup em map nil
para struct sem campo `ref`) em `OP_SET_PROPERTY`. Estava estimado em ~1 ns e
nunca tinha sido medido. Desde a #66 (`*_NORC`) e a #96 (`OP_SET_FIELD`) o
caminho **tipado** não passa por esses opcodes — é exatamente a alternativa da
spec §6.3 ("guard só quando o compilador não conhece a base") — então o custo
restante é só das escritas por `any`.

Micro `benchmarks/bench_dyn_write.nx` (novo na suíte; 1,2 M escritas por base
`any`: `xs[k] = …` e `c.hits = …`), binário com guard × binário com o guard
desligado nos dois funis, intercalado, mediana de 9, duas rodadas:

| rodada | guard | sem guard | delta |
|---|---|---|---|
| 1 | 105,2 ms | 99,9 ms | +5,3 % |
| 2 | 96,7 ms | 94,6 ms | +2,1 % |

≈ 2–4 ns por escrita dinâmica. `bench_path_update` (caminho tipado) nas mesmas
condições: 165,1 × 165,0 ms — **0 %**. Trocar `FieldIsRef` por teste de nil
antes do lookup não moveu o ponteiro (+2,3 % na rodada 2) e foi descartado.
Conclusão: o guard custa o que a fronteira dinâmica custa e só onde ela é
necessária; nada a fazer no compilador.

## feature/issue-queue-2026-08-28 (a52c7e1) × empréstimo aninhado com slot — issue #93 (b) (4026c43)

**Data:** 2026-08-28 · Windows 11 · Intel Core 7 150U · pwsh 7.6.5 · protocolo
intercalado, mediana de 9; sessão mais carregada que a do #96 (só o delta
intra-rodada é comparável). Dados brutos e perfis em
[`results/2026-08-28-issue-93b-borrow-path-raw.md`](results/2026-08-28-issue-93b-borrow-path-raw.md).
Spec: `docs/superpowers/specs/2026-08-28-vm-perf-issue-93b-borrow-path-design.md`.

O que mudou: `OP_REF_PROPERTY` resolve o índice do campo UMA vez, na criação do
ref (`ObjRef.Slot`); `descend`, `referenceStorageMode` e o setter de
propriedade usam `Slots[Slot]` quando a definição da instância no lugar tem
esse nome nesse slot (`fieldSlotOf`, a mesma guarda do #96 — definições de
`json_loads` vêm em ordem alfabética, ObjRef montado à mão deixa zero), senão
`FieldIndex` por nome como antes. `validateReferencedValue` (o `defer` de todo
acesso por ref) decide os payloads comuns por type switch antes do `reflect`.
O modelo de lugar da #83 não muda: o custo continua O(profundidade) por acesso,
com constante menor; "fast path quando `Owners == 1`" é inseguro sem validar a
cadeia (contra-exemplo na spec §2) e a cache por época fica como follow-up (§6).

**Verificação completa:** `go test ./...` verde; `go test -race ./internal/vm`
verde; **corpus 180/180**; **diff de saída base × head: 149 iguais, 0 divergentes**.

### Headline — base × head (`interleaved_compare.ps1 -Runs 9`)

| bench | base_ms | head_ms | delta | veredito |
|---|---|---|---|---|
| bench_bst_owned (novo: BST por posse, 20k) | 780,9 | 565,8 | **−27,5 %** | ✅ o caso da issue |
| bench_borrow_path (agora com `CHECKSUM:`) | 564,7 | 507,7 | **−10,1 %** | ✅ `ref root.b.c.xs[0]` |
| bench_share_mutate | 115,4 | 107,4 | −6,9 % | ➖ ruído/`validateReferencedValue` |
| bench_call_readonly | 572,5 | 546,5 | −4,5 % | ➖ ruído |
| bench_map_churn | 212,3 | 207,0 | −2,5 % | ➖ ruído |
| bench_call_ref | 1109,4 | 1091,6 | −1,6 % | ➖ ruído |
| bench_spawn_sum | 400,7 | 394,4 | −1,6 % | ➖ ruído |
| bench_struct_records | 127,4 | 126,0 | −1,1 % | ➖ ruído |
| bench_generic_vs_hand | 431,6 | 432,9 | +0,3 % | ✅ sentinela de `run()` |
| bench_bst_ref (novo: gêmeo com `ref TreeNode`) | 216,7 | 217,9 | +0,6 % | ➖ não passa pelo caminho |
| bench_bubblesort | 740,0 | 744,5 | +0,6 % | ➖ ruído |
| bench_path_update | 162,0 | 163,1 | +0,7 % | ➖ ruído |
| bench_conway | 1264,3 | 1283,4 | +1,5 % | ✅ gate CoW (≤ +5 %) |
| bench_call_light / typed_call_map / value_call_mutate | ~28–32 | ~27–29 | −4…−11 % | ➖ piso¹ |

¹ ~30 ms com piso de processo ~10 ms: não decidem nada.

### Perfil (`--cpuprofile`, BST por posse 50k, máquina livre)

2,20 s → **1,60 s**. `FieldIndex`/`mapaccess2_faststr`/`aeshashbody` 16,4 % →
~1 %; `validateReferencedValue` (reflect) → 0,5 %. Sobra a caminhada
(`borrowContainer` + `descend` + `referenceStorageMode` ≈ 50 %), agora
dominada pela estrutura do laço e pelo RC da unicização; `fieldSlotOf` 11 % (a
comparação do nome é 4,6 %).

## develop (2a627bc) × campo de struct por índice — issue #96 (perf/issue-96-struct-field-index, 79af956)

**Data:** 2026-08-28 · Windows 11 · Intel Core 7 150U · pwsh 7.6.5 · protocolo
intercalado, mediana de 9. Máquina sem `go test` nem build durante o A/B.
Dados brutos e perfis em
[`results/2026-08-28-issue-96-struct-field-index-raw.md`](results/2026-08-28-issue-96-struct-field-index-raw.md).
Spec: `docs/superpowers/specs/2026-08-28-vm-perf-issue-96-struct-field-index-design.md`.

O que mudou: quando o tipo estático da base é um struct do programa, o
compilador resolve `p.x` para o índice de slot (posição na declaração — a
ordem dos `Slots` desde o #95) e emite `OP_GET_FIELD` / `OP_SET_FIELD` /
`OP_GET_FIELD_MUT` com operando `[idx u8][nome u16]`. O nome vai junto porque
`json_loads` monta definições de struct em ordem alfabética e essas instâncias
entram em contêineres tipados: o caminho rápido confere `Fields[idx] == nome`
(comparação de string curta) e cai no funil por nome quando não bate. Base
`any`, módulos e `ref p.x` inalterados; `OP_SET_FIELD` é statement e só chama
`Retain`/`Release` quando um dos lados pode ter contador. Os `case` por nome
passaram a chamar os mesmos métodos que os fallbacks (`field_ops.go`).

**Verificação completa:** `go test ./...` verde; `go test -race ./internal/vm`
verde; **corpus 180/180**; **diff de saída base × head: 149 iguais, 0
divergentes**; sem a guarda, o teste da instância JSON reordenada falha.

### Headline — base × head (`interleaved_compare.ps1 -Runs 9`)

| bench | base_ms | head_ms | delta | veredito |
|---|---|---|---|---|
| bench_path_update | 238,8 | 160,7 | **−32,7 %** | ✅ `cells[i].hits = cells[i].hits + 1`: sem hash de campo, sem `Retain`/`Release` de `int` |
| bench_struct_records | 145,9 | 136,1 | **−6,7 %** | ✅ só as leituras; 47 % do bench é a validação do construtor (cache do PR #70 nunca chegou a `develop` — follow-up) |
| bench_share_mutate | 111,9 | 109,7 | −2,0 % | ✅ gate CoW |
| bench_conway | 1212,1 | 1196,3 | −1,3 % | ✅ gate CoW (≤ +5 %) |
| bench_map_churn | 210,2 | 208,7 | −0,7 % | ➖ ruído |
| bench_bubblesort | 659,8 | 655,6 | −0,6 % | ➖ ruído |
| bench_generic_vs_hand | 419,7 | 420,6 | +0,2 % | ✅ sentinela de `run()`: sem regressão de codegen |
| bench_call_ref | 995,5 | 999,5 | +0,4 % | ➖ ruído |
| bench_call_readonly | 448,3 | 450,9 | +0,6 % | ➖ ruído |
| bench_spawn_sum | 411,6 | 414,7 | +0,8 % | ➖ ruído |
| bench_typed_call_map | 27,6 | 28,4 | +2,9 % | ➖ piso¹ (gate CoW ok) |
| bench_call_light | 27,0 | 27,0 | 0 % | ➖ piso¹ |
| bench_value_call_mutate | 27,9 | 27,8 | −0,4 % | ➖ piso¹ |

¹ ~27 ms com piso de processo ~10 ms: não decidem nada. `bench_borrow_path` e
`bench_hash31_bytes` foram pulados pelo guard de `CHECKSUM:` (nenhum dos dois
binários imprime a linha — pré-existente).

### Perfil (`--cpuprofile`, carga ×10/×15)

`bench_path_update`: `FieldIndex` 15,2 % cum (`mapaccess2_faststr` 14,2 %,
`aeshashbody` 4,3 %) e `Retain`+`Release` 10 % na base → **ausentes** no head;
sobra `IsShared` da cadeia `_MUT` (16 %) e `memeqbody` 2,8 % (a guarda).
`bench_struct_records`: hashing de campo ausente no head; o topo continua
`validateStructConstructorArguments` (46,9 % cum), fora do escopo.

## v0.15.1 (c1cc12a) × protocolo de chamada — v0.15.2 (perf/issue-66-call-protocol, 868d435)

**Data:** 2026-08-22 · Windows 11 · Intel Core 7 150U (mesma máquina do item
2; só o delta intra-sessão é comparável com as seções de item 1/fase 2) ·
Python 3.14.7 · Lua 5.4.6 · Go 1.26.6 · pwsh 7.6.5 · protocolo intercalado,
mediana de 9 (headline), mediana de 11 (A/B por estágio, quatro binários na
mesma janela), mínimo de 9 no cross-runtime, `-count 8` no bench Go. Máquina
sem `go test` nem build durante as medições; CPU 0–21 % no início de cada
passo. Dados brutos, perfis e carga por passo em
[`results/2026-08-22-issue-66-call-protocol-raw.md`](results/2026-08-22-issue-66-call-protocol-raw.md).
Spec: `docs/superpowers/specs/2026-08-22-vm-perf-issue-66-call-protocol-design.md`
(issue #66, item 3).

O que mudou: **(d)** `OP_RETURN` com fast path quando o frame não tem `defer`,
não tem vínculo RC (`Owned` vazio), não há upvalue aberto e o frame de baixo
ainda pertence ao `run()` corrente — `popSimpleFrame` em `unwind.go` (o guard
de arquitetura exige teardown terminal só lá) faz o mesmo teardown sem a
cópia dupla de `frameOutcome` nem a segunda chamada; **(a)+(b)** `OP_CALL_STATIC`
com fast path inline quando o callee é closure com `ParamsUntracked` (flag nova
do `ObjFunction`, calculada pelo compilador: todo parâmetro sem `ref` e de tipo
`int/float/bool/string/bytes`) e aridade certa — capacidade conferida à mão
(`ensureCallCapacity` custa 80 e não cabe em `run()`), frame montado sem o
laço `ownSlot`; **(c)** duas superinstruções por append em `chunk.go`,
emitidas em nível de AST: `OP_GET_LOCAL_ADD_IMM_INT [slot][imm i8]` (`n - 1`,
`i + 2` como expressão) e `OP_GET_LOCAL_2 [a][b]` (`a + b`, `i < n` — também
na condição fundida de `while`). O "(a) literal" da issue (pular o lookup de
global do callee com operando de constante) ficou de fora: era 1,6 % do perfil
de base. Caminhos lentos, `OP_CALL`, RC e mensagens de erro intocados.

**Verificação completa:** `go test ./...` verde (12 pacotes); `go test -race
./internal/value ./internal/vm` verde; **corpus 177/177**; **diff de saída
base × head: 146 iguais, 0 divergentes**; guards de inline inalterados
(`popSimpleFrame` custa 60 — uma chamada real, de propósito); guard de
arquitetura de teardown verde.

### Achado da rodada (custou um commit a mais)

A primeira medição por estágio deu **s1 ≈ s0** apesar de o perfil atribuir
12 % ao lado da chamada: `OP_CLOSURE`/`OP_CONSTANT`/`OP_CONSTANT_LONG` copiam o
`ObjFunction` campo a campo e **perderam `ParamsUntracked`** — o fast path
nunca disparava, e nenhum teste funcional pega isso (o caminho lento é
correto). Fix em 868d435 com `TestClosureKeepsParamsUntracked`, que pergunta
ao valor da closure. Lição: flag novo em `ObjFunction` precisa de teste pelo
**valor em runtime**, não pelo constant do chunk.

### Headline — base × head (`interleaved_compare.ps1 -Runs 9`)

| bench | v0151_ms | call_ms | delta | veredito |
|---|---|---|---|---|
| bench_bubblesort | 776,9 | 694,1 | **−10,7 %** | ✅ `OP_GET_LOCAL_2` em `j < n - i - 1`/`a[j] > a[j+1]` |
| bench_spawn_sum | 398,8 | 369,8 | **−7,3 %** | ✅ chamadas com parâmetro `int` |
| bench_generic_vs_hand | 456,6 | 448,7 | −1,7 % | ✅ sentinela de `run()`: sem regressão de codegen |
| bench_call_light | 19,9 | 19,7 | −1,0 % | ➖ piso¹ (gate CoW ok) |
| bench_typed_call_map | 22,3 | 22,5 | +0,9 % | ➖ piso¹ (gate CoW ok) |
| bench_path_update | 229,8 | 231,8 | +0,9 % | ➖ ruído |
| bench_call_readonly | 531,3 | 537,4 | +1,1 % | ➖ parâmetro `int[]` → caminho lento, como desenhado |
| bench_call_ref | 1137,1 | 1149,9 | +1,1 % | ➖ idem (`ref int[]`) |
| bench_conway | 1253,2 | 1275,3 | +1,8 % | ✅ gate CoW (≤ +5 %) |
| bench_share_mutate | 100,6 | 103,3 | +2,7 % | ✅ gate CoW |
| bench_map_churn | 194,3 | 200,1 | +3,0 % | ➖ ruído (cross `map_churn` +5 %, ver abaixo) |
| bench_value_call_mutate | 21,1 | 21,2 | +0,5 % | ➖ piso¹ |

¹ ~20 ms com piso de processo ~10 ms: não decidem nada.

### A/B por estágio — quatro binários intercalados (mediana de 11, parede; piso 10 ms)

`s0` = (d) retorno · `s1` = + (a/b) chamada (com o fix do flag) · `call` =
head (+ (c) superinstruções).

| bench | base | s0 | s1 | head | s0 vs base | s1 vs s0 | head vs s1 | **head vs base** |
|---|---|---|---|---|---|---|---|---|
| `cross_runtime/fib` | 203,4 | 161,1 | 149,5 | **122,0** | −20,8 % | −7,2 % | −18,4 % | **−40,0 %** (líquido −42 %) |
| `cross_runtime/bubblesort` | 133,3 | 132,7 | 133,8 | **118,9** | −0,5 % | +0,8 % | −11,1 % | **−10,8 %** |
| `cross_runtime/loop_arith` | 213,3 | 214,5 | 220,5 | 216,2 | +0,6 % | +2,8 % | −2,0 % | +1,4 % (ruído) |

### Cross-runtime (mínimo de 9, intercalado com CPython 3.14.7 / Lua 5.4.6 / Go 1.26.6)

Tempo líquido (descontado o piso de `startup`, ~9,5 ms nos dois Noxy); tabela
completa em [`cross_runtime/results/cross_runtime.md`](cross_runtime/results/cross_runtime.md).

| bench | v0.15.1 | v0.15.2 | v0.15.2 ÷ v0.15.1 | ÷ python (antes → agora, esta máquina) | ÷ lua |
|---|---|---|---|---|---|
| `fib` | 185,1 | **105,4** | **0,57x** | 2,0x → **1,16x** | 4,4x → **2,5x** |
| `bubblesort` | 117,1 | **106,8** | **0,91x** | 1,7x → **1,55x** | – |
| `string_ops` | 73,7 | 69,0 | 0,94x | 2,3x → 2,2x | – |
| `loop_arith` | 193,7 | 194,7 | 1,01x | 1,06x → 1,06x | 5,0x |
| `mandelbrot` | 132,9 | 133,5 | 1,00x | 1,8x → 1,8x | – |
| `map_churn` | 112,3 | 117,5 | 1,05x | 2,0x → 2,1x | – |

**`BenchmarkNoxyCallOverhead`** (`-count 8`, mediana): 73 762 → **53 076 ns/op
(−28,0 %)**, 560 B/op e 10 allocs/op nos dois — meta da issue (≥ −25 %) ✅.

### Leitura

**A meta da issue ("`fib` 2,9x → ~2x do CPython; `BenchmarkNoxyCallOverhead`
≥ −25 %") confirma com folga: `fib` 0,57x de v0.15.1 (−42 % líquido), 1,16x do
CPython nesta máquina; bench Go −28 %.** A decomposição por estágio diz onde
estava o custo: o **retorno** (s0: −21 %) era o maior item — `finishFrame` +
`finalizeCurrentFrame` eram 24 % do perfil por causa da cópia dupla de
`frameOutcome` e das duas chamadas, não dos laços (vazios em `fib`); a
**chamada** (s1: −7 %) pagava `callValueStatic` → `callPreparedClosure` →
`ownSlot` por parâmetro; as **superinstruções** (−18 % sobre s1) tiram 4 dos
15 despachos por chamada não-folha de `fib` e são o que move `bubblesort`
(−11 %, `j < n - i - 1`). `loop_arith` não mexe: o laço compara `i < 5000000`
(literal, sem par de locais) e o corpo já usa `OP_INC_LOCAL_INT`.

**O que resta em `fib` (perfil do head):** despacho puro (`run` 60 % flat,
11 opcodes por chamada não-folha), `push`/`pop` 17 %, `popSimpleFrame` 6 % e —
agora visível — o lookup cacheado do callee em `OP_GET_GLOBAL` (`Generation()`
atômico + compare de entrada, ~12 %). Esse último é exatamente o "(a) literal"
da issue que ficou de fora por ser 1,6 % na base; com o protocolo cortado
virou o próximo candidato (callee in-module resolvido em compilação, ou
`GET_GLOBAL + CALL_STATIC` fundido).

**Gates CoW (≤ +5 %):** `conway` +1,8 %, `share_mutate` +2,7 %,
`typed_call_map` +0,9 %, `call_light` −1,0 % — verdes. Sentinela
`bench_generic_vs_hand` −1,7 %: os dois blocos inline novos em `run()` não
pioraram o codegen do laço (o risco real desta rodada, lição do item 1).

## v0.15.0 (73cf11a) × strings: fast path ASCII + `to_str(int)` — v0.15.1 (perf/issue-66-string-ascii-fastpath, 0bc9e5d)

**Data:** 2026-08-22 · Windows 11 · **Intel Core 7 150U** (máquina diferente das
seções abaixo, que rodaram num i7-1165G7 — só o delta intra-sessão é
comparável; piso de processo aqui ≈ 11 ms, lá ≈ 90 ms) · Python 3.14.7 · Go
1.26.6 · Lua ausente · protocolo intercalado, mediana de 9 (headline), mediana
de 11 (A/B por estágio), mínimo de 9 no cross-runtime. Máquina sem `go test`
nem build durante as medições; CPU 2–26 % no início de cada passo. Três
binários compilados na mesma sessão (base, estágio 1 = só fast path ASCII,
head). Dados brutos, perfis e carga por passo em
[`results/2026-08-22-issue-66-strings-raw.md`](results/2026-08-22-issue-66-strings-raw.md).
Spec: `docs/superpowers/specs/2026-08-22-vm-perf-issue-66-string-ascii-fastpath-design.md`
(issue #66, item 2, etapa 1). Scripts rodados no Windows PowerShell 5.1 via
cópias com BOM (pwsh 7 não instalado nesta máquina; os `.ps1` são UTF-8 sem BOM).

O que mudou: **nenhum opcode, nenhuma mudança em `run()`, representação de
string intocada.** Um helper `isASCII(s)` (varredura por byte `>= 0x80`, sem
alocar, custo de inline 23) decide em `strings_substring`, `strings_char_at`,
`s[i]` (`getIndexGeneric`) e `slice()` de string entre o ramo novo (índices
em bytes, fatia de `string` compartilhada — zero alocação além do box) e o
ramo atual (`[]rune`), que fica como estava; bytes ≥ 0x80 — inclusive
inválidos — nunca entram no ramo novo. O clamp de `substring` virou helper
compartilhado pelos dois ramos (`clampSubstringRange`). Mais: `Value.String()`
de `int` via `strconv.FormatInt` em vez de `fmt.Sprintf("%d")` (saída idêntica,
beneficia `print`, interpolação e concatenação) e `to_str` de escalar sem
`requireValidUTF8`. `length` ficou como estava: `RuneCountInString` já é
varredura por byte sem alocar (1,3 % do perfil).

**Verificação completa:** `go test ./...` verde (12 pacotes); `go test -race
./internal/value ./internal/vm` verde; **corpus 177/177**
(`run_all_tests_concurrent.nx`); **diff de saída base × head: 146 iguais, 0
divergentes** (`compare_examples.ps1`); guards de inline verdes e inalterados
(`push` 20, `pop` 18, `Retain` 67, `Release` 80, `NeverTracked`,
`arrayTagIsRefSlot` 20, `ensureCallCapacity` 80), novo `isASCII` 23 (≤ 80).

### Achado que mudou o desenho

O perfil de `string_ops` (10x iterações, 940 ms de amostras) **não** confirma
a hipótese da issue como maior termo: `length` → `RuneCountInString` é 1,3 %;
`strings_substring` (`[]rune` + cópia) 13,8 %; e **`to_str(i)` é 22,3 %** —
`fmt.Sprintf("%d")` com o `pp` do fmt, boxing do int e `requireValidUTF8`
sobre o resultado. Por isso o PR inclui `to_str(int)`/`Value.String()` (decisão
tomada com o usuário antes de codar) e deixa `length` de fora.

### Headline — base × head (`interleaved_compare.ps1 -Runs 9`)

| bench | v0150_ms | str_ms | delta | veredito |
|---|---|---|---|---|
| bench_map_churn | 230,1 | 203,6 | **−11,5 %** | ✅ `f"k{i % 500}"` interpola um int por iteração → `FormatInt` |
| bench_spawn_sum | 431,5 | 419,4 | −2,8 % | ➖ ruído |
| bench_conway | 1348,9 | 1330,3 | −1,4 % | ✅ gate CoW (≤ +5 %) |
| bench_call_ref | 1252,2 | 1240,7 | −0,9 % | ➖ ruído |
| bench_generic_vs_hand | 474,4 | 474,8 | +0,1 % | ✅ sentinela de `run()`: nada mudou em `run()` e o bench confirma |
| bench_call_readonly | 587,7 | 591,6 | +0,7 % | ➖ ruído |
| bench_share_mutate | 144,4 | 146,5 | +1,5 % | ✅ gate CoW |
| bench_bubblesort | 862,7 | 878,8 | +1,9 % | ➖ ruído (não toca string) |
| bench_path_update | 239,1 | 244,6 | +2,3 % | ➖ ruído |
| bench_typed_call_map | 22,4 | 21,5 | −4,0 % | ➖ piso¹ (gate CoW ok) |
| bench_call_light | 20,3 | 19,8 | −2,5 % | ➖ piso¹ (gate CoW ok) |
| bench_value_call_mutate | 20,2 | 20,1 | −0,5 % | ➖ piso¹ |

¹ ~20 ms com piso de processo ~11 ms: não decidem nada.

### A/B por estágio — três binários intercalados (mediana de 11, parede; piso 11 ms)

`s1` = só fast path ASCII (`isASCII`, `substring`/`char_at`, `s[i]`/`slice`) ·
`str` = head (+ `to_str(int)`/`FormatInt`).

| bench | base | s1 | str | s1 vs base | str vs base | líquido str vs base |
|---|---|---|---|---|---|---|
| `cross_runtime/string_ops` | 120,5 | 109,7 | **97,1** | −9,0 % | **−19,4 %** | **−21 %** (109,5 → 86,1) |
| `cross_runtime/map_churn` | 151,8 | 150,6 | **133,8** | −0,8 % | **−11,9 %** | −12,8 % (todo do `to_str`) |

### Cross-runtime (mínimo de 9, intercalado com CPython 3.14.7 / Go 1.26.6)

Tempo líquido (descontado o piso de `startup`, 11 ms nos dois Noxy); tabela
completa em [`cross_runtime/results/cross_runtime.md`](cross_runtime/results/cross_runtime.md).

| bench | v0.15.0 | v0.15.1 | v0.15.1 ÷ v0.15.0 | ÷ python (antes → agora, esta máquina) |
|---|---|---|---|---|
| `string_ops` | 96,2 | **74,2** | **0,77x** | 3,1x → **2,4x** |
| `map_churn` | 132,3 | **115,0** | **0,87x** | 2,3x → **2,0x** |
| `fib` | 202,8 | 197,5 | 0,97x | 2,1x → 2,0x |
| `loop_arith` | 202,7 | 198,7 | 0,98x | 1,1x → 1,0x |
| `mandelbrot` | 136,0 | 138,0 | 1,01x | 1,8x → 1,8x |
| `bubblesort` | 126,4 | 129,0 | 1,02x | 1,8x → 1,8x |

### Leitura

**A meta da issue ("`string_ops` 4,2x → ~2x do CPython com a etapa 1") fica a
meio caminho: 3,1x → 2,4x nesta máquina (−23 % líquido), e só um terço disso
vem do fast path ASCII que a issue propunha** (`s1`: −10 %); o resto é
`to_str(int)` sem `fmt` (−11 % a mais), que a issue não listava. `map_churn`
vem de carona (−13 %), pela interpolação de int nas chaves. Nenhum outro bench
mexe (±3 %), como esperado de uma mudança que não toca `run()`, opcodes ou RC.

**O que resta em `string_ops` (perfil do head):** protocolo de chamada de
builtin — `callNative` 27 % das amostras, com o wrapper Noxy de `substring`
(`stdlib/strings.nx`: `substring` → `strings_substring`) custando um frame
inteiro por chamada — é o item 3; o box da string em `interface{}`
(`convTstring` 5 %) é a etapa 2 do item 2 (string boxada), que **não se
justifica por `length`** (1,3 %) e só faria sentido medida contra o boxing.
`formatBase10` 5 % é irredutível.

**Gates CoW (≤ +5 %):** `conway` −1,4 %, `share_mutate` +1,5 %,
`typed_call_map` −4,0 %, `call_light` −2,5 % — verdes. Sentinela
`bench_generic_vs_hand` +0,1 %.

## v0.14.3 (7eed082) × indexação tipada de array — v0.15.0 (perf/issue-66-typed-array-index, d870a02)

**Data:** 2026-08-22 · Windows 11 · i7-1165G7 · protocolo intercalado, mediana
de 9 (headline) e de 5 (por estágio), mínimo de 9 no cross-runtime. Máquina
sem `go test` nem build durante as medições; CPU 4–11 % no início de cada
passo. Seis binários compilados na mesma sessão (um por estágio, mais o head
com e sem o `goto` — ver abaixo). Dados brutos, carga por passo, A/Bs focados,
perfis e scripts em
[`results/2026-08-22-issue-66-typed-arrays-raw.md`](results/2026-08-22-issue-66-typed-arrays-raw.md).
Spec: `docs/superpowers/specs/2026-08-22-vm-perf-issue-66-typed-array-index-design.md`
(issue #66, item 1). Scripts rodados com `pwsh -NoProfile -File`
(`interleaved_compare.ps1` não parseia no Windows PowerShell 5.1 — UTF-8 sem BOM).

O que mudou: seis opcodes anexados ao fim de `chunk.go` — `OP_GET_INDEX_ARRAY` /
`OP_SET_INDEX_ARRAY_NORC` (base estaticamente `T[]` em posição genérica) e as
formas fundidas por slot `OP_GET_LOCAL_INDEX_ARRAY` / `OP_SET_LOCAL_INDEX_ARRAY_NORC`
(local `T[]`, inclusive o `$collection` do for-each) e `OP_GET_REF_LOCAL_INDEX_ARRAY` /
`OP_SET_REF_LOCAL_INDEX_ARRAY_NORC` (parâmetro `ref T[]`, que resolve a caixa
do ref com uma `Upvalue.Load()` em vez de `referenceStorage`). Caminho rápido
grava o resultado no lugar na pilha; `NORC` pula `Retain`/`Release` só depois
de conferir em runtime que valor novo e velho não têm contador
(`value.NeverTracked`) e que o array não é `(ref T)[]`; container inesperado
cai nos funis genéricos (`getIndexGeneric`/`setIndexGeneric`, os corpos de
`OP_GET_INDEX`/`OP_SET_INDEX` extraídos em método). Formas fundidas só com
índice (e valor) sintaticamente sem efeito colateral.

**Verificação completa:** `go test ./...` verde (12 pacotes); `go test -race
./internal/value ./internal/vm` verde; **corpus 177/177**
(`run_all_tests_concurrent.nx`); **diff de saída base × head: 146 iguais, 0
divergentes** (`compare_examples.ps1`); guards de inline verdes (`push` 20 /
121 sites, `pop` 18 / 85 sites, `Retain` 67, `Release` 80, `NeverTracked` 10,
`arrayTagIsRefSlot` 20 — sem folga, travado).

### Headline — base × head (`interleaved_compare.ps1 -Runs 9`)

| bench | v0143_ms | typed_ms | delta | veredito |
|---|---|---|---|---|
| bench_bubblesort | 3071,2 | 1089,4 | **−64,5 %** | ✅ (meta ≥ −30 %) |
| bench_call_ref | 3010,6 | 1702,8 | **−43,4 %** | ✅ |
| bench_call_readonly | 1018,9 | 895,5 | **−12,1 %** | ➖ meta era ≥ −30 %: 37 % desse bench é `callNative` (`length(data)` no `while`), não indexação |
| bench_path_update | 495,1 | 449,6 | −9,2 % | ✅ |
| bench_conway | 1871,3 | 1801,2 | −3,7 % | ✅ gate CoW (≤ +5 %) |
| bench_map_churn | 408,7 | 408,0 | −0,2 % | ✅ (`setIndexGeneric`/`getIndexGeneric` como chamada: sem custo visível) |
| bench_generic_vs_hand | 730,7 | 736,0 | +0,7 % | ✅ (era **+12,8 %** com o `goto redispatch`, ver abaixo) |
| bench_spawn_sum | 668,7 | 677,0 | +1,2 % | ➖ ruído |
| bench_share_mutate | 218,1 | 236,4 | +8,4 % | ✅ gate CoW: rodada focada de 15 dá **+1,6 %** (233,0 → 236,7; a base oscilou 265 → 218 entre rodadas) |
| bench_typed_call_map | 151,7 | 149,1 | −1,7 % | ➖ piso¹ (gate CoW ok) |
| bench_call_light | 130,7 | 128,6 | −1,6 % | ➖ piso¹ (gate CoW ok) |
| bench_value_call_mutate | 139,6 | 140,9 | +0,9 % | ➖ piso¹ |

¹ ~130 ms com piso de processo ~90 ms: não decidem nada (seção de 2026-08-22).

### Por estágio — seis binários na mesma janela (mediana de 5)

`s0` = só o VM (handlers, funis, rótulo `goto`; nada emitido) · `s1` = +
formas genéricas tipadas · `s2` = + fundidas de local plano e for-each ·
`s3goto` = + fundidas de `ref` (head com `goto`) · `s3` = head final (fallback
de leitura por `getIndexGeneric`, sem rótulo). Delta contra `base`.

| bench | s0 | s1 | s2 | s3goto | **s3** |
|---|---|---|---|---|---|
| bench_bubblesort | +2,2 % | −2,2 % | +0,8 % | −62,3 % | **−62,3 %** |
| bench_call_ref | +0,6 % | −3,2 % | −5,5 % | −43,4 % | **−44,0 %** |
| bench_call_readonly | +1,3 % | −4,1 % | −4,1 % | −5,0 % | **−12,2 %** |
| bench_path_update | +6,5 % | −5,3 % | −9,4 % | −7,7 % | **−10,4 %** |
| bench_conway | +0,8 % | −4,8 % | −1,0 % | −3,1 % | **−3,7 %** |
| bench_map_churn | −4,1 % | −0,2 % | −3,1 % | +0,2 % | **−6,5 %** |
| bench_generic_vs_hand | **+14,9 %** | +13,1 % | +12,9 % | +13,7 % | **+5,6 %**² |
| bench_share_mutate | −4,9 % | −7,4 % | −6,9 % | −7,2 % | **+1,3 %** |
| bench_spawn_sum | +2,1 % | +2,6 % | −5,1 % | −5,1 % | **+2,4 %** |

² tempo de parede; pelo relógio interno do próprio bench (`GEN_MS+HAND_MS`,
9 intercaladas) `s3` dá +1,3 % (623 → 631 ms) contra +14,6 % do `s3goto`
(714 ms).

### Cross-runtime (mínimo de 9, intercalado com CPython 3.13.1 / Lua 5.4.7 / Go 1.24.11)

Tempo líquido (descontado o piso de `startup`, 96 ms nos dois Noxy) e razões;
tabela completa em [`cross_runtime/results/cross_runtime.md`](cross_runtime/results/cross_runtime.md).

| bench | v0.14.3 | v0.15.0 | v0.15.0 ÷ v0.14.3 | ÷ python (antes → agora) | ÷ lua |
|---|---|---|---|---|---|
| `bubblesort` | 430,6 | **153,6** | **0,36x** | 5,5x → **1,8x** | – |
| `fib` | 322,0 | 309,9 | 0,96x | 2,9x → 2,5x | 5,9x |
| `loop_arith` | 268,5 | 283,2 | 1,05x³ | 1,1x → 1,0x | 6,8x |
| `mandelbrot` | 185,1 | 187,3 | 1,01x | 1,9x → 2,3x⁴ | – |
| `map_churn` | 173,6 | 195,3 | 1,12x³ | 2,1x → 2,4x⁴ | – |
| `string_ops` | 138,8 | 134,4 | 0,97x | 4,2x → 3,3x⁴ | – |

³ A/B focado de 11 intercaladas (raw §3): `loop_arith` empata (372,6 × 372,0 ms,
mín. 348,7 × 345,1) e `map_churn` sai −6,5 % (316,4 × 295,7) — os dois
"aumentos" do mínimo-de-9 são ruído; `map_churn.nx` nem passa pelos opcodes
novos. ⁴ As razões ÷ python de `mandelbrot`/`map_churn`/`string_ops` mudaram
porque o CPython desta rodada mediu diferente da rodada da fase 2 (80,5 / 80,4
/ 40,4 ms líquidos contra 90,6 / 82,7 / 31,9), não o Noxy — a coluna
"v0.15.0 ÷ v0.14.3" é a comparação válida.

### Leitura

**A hipótese da issue ("bubblesort 5,5x → ~2–2,5x do CPython; bench_bubblesort
≥ −30 %") confirma com folga: 1,8x e −64,5 %.** O ganho é quase todo do
estágio 3 — a forma fundida de `ref T[]`: o perfil de base mostrava
`resolveReferenceValue`/`referenceStorage` (um `defer`, uma closure do setter
alocada por acesso, `validateReferencedValue` com `reflect`) e
`unicizeThroughRefValue` como **metade do tempo** do bubblesort, e o bench da
issue passa por `data: ref int[]` — os anchors da issue descreviam o local
plano, onde o custo é menor. `bench_call_ref` (mutação via ref) vem junto,
−43 %.

**A segunda meta da issue ("bench_call_readonly ≥ −30 %") não confirma: −12 %.**
Não é indexação: 37 % desse bench é `callNative` — o `length(data)` da
condição do `while` compila como chamada de builtin genérica. Um `OP_LEN`
estático é follow-up (fora do item 1). Os estágios 1 e 2 (formas genéricas
tipadas e fundidas de local plano, sem `ref`) valem −4 a −10 % em
`call_readonly`/`path_update`/`conway` — onde o despacho do índice nunca foi
o gargalo.

**Achado da rodada (custou um commit a mais):** o primeiro desenho do fallback
de leitura re-despachava o `case` genérico com `instruction = OP_GET_INDEX;
goto redispatch` — "zero custo no genérico" no papel. `bench_generic_vs_hand`,
um laço de `length()` **sem indexação nenhuma**, subiu **+10–14 %** já no
estágio 0 (que não emite opcode novo): o rótulo (que faz de `instruction` um
phi) e os fallbacks inline mudaram o codegen do laço de `run()` inteiro. A
chamada a um método (`getIndexGeneric`) no fallback não aparece no mesmo
bench. Lição para os próximos itens: qualquer mudança em `run()` passa por
esse bench como sentinela, não só pelos benches do item.

**Gates CoW (≤ +5 %):** `conway` −3,7 %, `typed_call_map` −1,7 %, `call_light`
−1,6 %, `share_mutate` +1,6 % na rodada focada — verdes. `map_churn`
(`setIndexGeneric`/`getIndexGeneric` viraram chamada no genérico): −0,2 %.

**O que resta do acesso via `ref`:** `Upvalue.Load()` e os atômicos do seu
`RWMutex` (~25 % das amostras do head) — é o que separa `bubblesort` de 1,8x
do CPython; candidato a item do roadmap se precisar.

## v0.14.2 (cb8efcb) × fase 2 de perf — layout do `Value` (perf/issue-37-value-layout, ba7f85d)

**Data:** 2026-08-22 · Windows 11 · i7-1165G7 · protocolo intercalado, mediana
de 9 execuções (mínimo também registrado). Máquina sem `go test` nem
benchmark concorrente; CPU total entre 1 % e 21 % no início de cada passo
(Firefox, Slack e o Claude Code abertos, sem Zoom/Chrome). **Os quatro
binários foram compilados na mesma sessão**, então não há a assimetria de
piso "build fresco +4–5 ms" da seção anterior — `startup` empata (106–110 ms
nos quatro). Dados brutos, carga por passo e script em
[`results/2026-08-22-issue-37-value-layout-raw.md`](results/2026-08-22-issue-37-value-layout-raw.md).
Spec: `docs/superpowers/specs/2026-08-22-vm-perf-fase2-value-layout-design.md`
(issue #37, estágios 1 e 2 + "extra barato" do `pop`).

Commits medidos, um binário por estágio: **s1** = `Value` 48 → 32 B
(`41638e8`); **s1+2** = + `ObjHeader{Owners}` no offset 0 de
array/map/instância e dica `kind` em `ownersOf` (`e4d5a5f`); **s1+2+pop** =
+ `pop()` inlinada em `run()` (custo 22 → 18, 0 → 79 sites; `ba7f85d`).

**Verificação completa:** `go test ./...` verde (12 pacotes); `go test -race
./internal/value ./internal/vm` verde (2,0 s / 69,7 s); **corpus 177/177**
(`run_all_tests_concurrent.nx`); **diff de saída base × head: 146 iguais, 0
divergentes** (`compare_examples.ps1`); guards de inline verdes (`push` 20,
`pop` 18, `Retain` 67, `Release` 80).

### Headline — base × s1+2+pop (`interleaved_compare.ps1 -Runs 9`)

| bench | v0142_ms | s12p_ms | delta | veredito |
|---|---|---|---|---|
| bench_bubblesort | 4046,3 | 3002,8 | **−25,8 %** | ✅ |
| bench_call_readonly | 1386,8 | 929,4 | **−33,0 %** | ✅ (a regressão de +10 % da seção anterior fica mais que desfeita) |
| bench_call_ref | 4160,8 | 3052,1 | **−26,6 %** | ✅ |
| bench_path_update | 682,4 | 498,2 | **−27,0 %** | ✅ |
| bench_generic_vs_hand | 904,5 | 732,4 | **−19,0 %** | ✅ |
| bench_share_mutate | 302,1 | 251,4 | **−16,8 %** | ✅ gate CoW (≤ +5 %) |
| bench_conway | 2214,9 | 1878,8 | **−15,2 %** | ✅ gate CoW |
| bench_spawn_sum | 767,1 | 703,7 | −8,3 % | ✅ |
| bench_map_churn | 479,1 | 442,0 | −7,7 % | ✅ |
| bench_call_light | 120,3 | 110,7 | −8,0 % | ➖ piso¹ (gate CoW ok) |
| bench_typed_call_map | 124,5 | 122,9 | −1,3 % | ➖ piso¹ (gate CoW ok) |
| bench_value_call_mutate | 119,3 | 121,1 | +1,5 % | ➖ piso¹ |

¹ ~100 ms com piso de processo ~84 ms: não decidem nada (seção de 2026-08-22).

### Por estágio — quatro binários na mesma janela (mediana de 9)

"passo" = delta contra o binário imediatamente anterior.

| bench | s1 vs base | s1+2 (passo) | s1+2+pop (passo) | total |
|---|---|---|---|---|
| bench_bubblesort | −13,4 % | −2,8 % | −12,8 % | **−26,6 %** |
| bench_call_ref | −14,3 % | −0,3 % | −13,4 % | **−26,0 %** |
| bench_call_readonly | −11,7 % | +1,8 % | −19,0 % | **−27,3 %** |
| bench_path_update | −5,7 % | −1,3 % | −25,5 % | **−30,7 %** |
| bench_share_mutate | −14,0 % | −4,7 % | +1,5 % | **−16,9 %** |
| bench_generic_vs_hand | −7,2 % | +1,0 % | −11,0 % | **−16,6 %** |
| bench_spawn_sum | −7,4 % | +4,1 % | −10,2 % | **−13,5 %** |
| bench_conway | −1,8 % | −4,0 % | −6,0 % | **−11,4 %** |
| bench_map_churn | −2,1 % | +1,1 % | −4,2 % | **−5,2 %** |
| cross `fib` | −10,0 % | −3,2 % | −5,6 % | **−17,7 %** |
| cross `bubblesort` | −11,5 % | −2,7 % | −11,7 % | **−24,0 %** |
| cross `loop_arith` | −3,6 % | −3,1 % | −14,6 % | **−20,2 %** |
| cross `mandelbrot` | −3,3 % | +0,4 % | −12,9 % | **−15,4 %** |
| cross `string_ops` | −6,5 % | +5,7 % | −10,6 % | **−11,6 %** |
| cross `map_churn` | +4,1 % | −1,2 % | −9,2 % | **−6,6 %** |
| cross `startup` | +4,3 % | −0,5 % | −1,2 % | +2,5 % (piso, ruído) |

### Cross-runtime (mínimo de 9, intercalado com CPython 3.13.1 / Lua 5.4.7 / Go 1.24.11)

Tempo líquido (descontado o piso de `startup`, 91 ms nos dois Noxy) e razões;
tabela completa em [`cross_runtime/results/cross_runtime.md`](cross_runtime/results/cross_runtime.md).

| bench | v0.14.2 | fase 2 | fase 2 ÷ v0.14.2 | ÷ python (antes → agora) | ÷ lua |
|---|---|---|---|---|---|
| `fib` | 420,7 | 296,0 | **0,70x** | 4,1x → **2,9x** | 5,6x |
| `bubblesort` | 625,4 | 423,1 | **0,68x** | 8,1x → **5,5x** | – |
| `loop_arith` | 327,6 | 272,5 | 0,83x | 1,3x → **1,1x** | 6,5x |
| `mandelbrot` | 219,1 | 176,9 | 0,81x | 2,4x → **1,9x** | – |
| `map_churn` | 226,6 | 173,7 | 0,77x | 2,7x → **2,1x** | – |
| `string_ops` | 145,8 | 134,5 | 0,92x | 4,6x → **4,2x** | – |

### Leitura

**A hipótese da issue ("fib 15–25 % melhor") confirma: −17,7 % na mediana
intercalada, 0,70x no tempo líquido do cross-runtime.** E o ganho é maior
onde a linguagem passa o dia — leitura/escrita indexada de array e chamada
com composto (`bubblesort`, `call_readonly`, `call_ref`, `path_update`: −26 a
−33 %). A regressão de +10 % em `call_readonly` que a seção anterior apontava
como "item a investigar" não só some como vira −33 %.

**De onde veio, por estágio:**

- **Estágio 1 (`Value` 32 B): −10 a −14 % em tudo que é chamada/array**
  (`fib`, `bubblesort`, `call_ref`, `call_readonly`, `share_mutate`), −2 a −7 %
  no resto. É o efeito previsto: um terço a menos de bytes por `push`/`pop`/
  cópia de operando e a pilha em 64 KB em vez de 96 KB.
- **Estágio 2 (header + dica `kind`): −0,3 a −4,7 % nos benches de RC
  intenso (`share_mutate` −4,7 %, `conway` −4,0 %, `fib` −3,2 %), ruído nos
  demais (−1 a +6 %).** O microbench Go de `ownersOf` (type switch × dica ×
  cast `unsafe` do header) dá ~4–5 ns/op para as três formas, indistinguíveis
  no ruído: no `gc`, o type switch sobre `interface{}` já é "carrega o hash do
  tipo + ≤ 3 comparações", e o que custa no RC são os atômicos. O estágio 2
  entrega o header no offset 0 (layout travado por teste) e a saída antecipada
  de string/struct/RTI; **não vale o `unsafe`** — e a forma "switch no `kind` +
  assertion por caso" nem cabe no orçamento de inline de `Retain`/`Release`
  (detalhe na spec §3.2).
- **`pop()` inlinada: −6 a −25 % por cima dos dois estágios** — o maior item
  isolado da rodada. Era o achado que a issue não tinha: `pop` custava 22 no
  inliner e, com `run()` em regime de "big function" (orçamento 20), era
  chamada real em todos os ~84 sites; o perfil de `fib` a mostrava com **17 %
  flat** (v0.14.2) contra 3 % depois. O corpo novo faz exatamente o mesmo
  trabalho (zera o `Value` inteiro) — só a forma muda (atribuição dupla com
  resultado nomeado, 18 nós). `path_update` (−25,5 % só deste passo) e
  `call_readonly` (−19 %) são os mais sensíveis porque empilham/desempilham
  muito por iteração.

**Gates CoW (≤ +5 %):** `typed_call_map` −1,3 %, `share_mutate` −16,8 %,
`call_light` −8,0 %, `conway` −15,2 % — todos verdes.

**Microbench de chamada** (`BenchmarkNoxyCallOverhead`, `go test -bench`,
8 repetições por estágio): ver tabela no fim do arquivo bruto; mede o custo
fixo de uma chamada `leaf(i)` em laço de 1000.

**Sobre o estágio 3 (pré-condição 1 da issue: "1+2 confirmam a tese").**
Confirmam *pela metade*: a redução de bytes virou tempo (estágio 1), mas
a parte do `ownersOf` não (estágio 2 é ~ruído). Os 8 bytes restantes
(`interface{}` → `unsafe.Pointer`) só pagariam pelo mesmo mecanismo do
estágio 1 (menos bytes copiados, mais `Value` por linha de cache) — o ganho
marginal de 32 → 24 B é menor que o de 48 → 32 B e vem com 482 type assertions
a converter e o boxing de string. Com `fib` a 2,9x do CPython e `string_ops`
a 4,2x, a aposta de melhor relação custo/benefício continua sendo a que a
própria issue aponta: indexação de array (ainda 5,5x) e o custo de chamada
(`callPreparedClosure`/`finishFrame` são 26 % do perfil novo de `fib`).

## v0.6.0 (68209be) × v0.14.1 (4874048) — re-medição com a máquina ociosa

**Data:** 2026-08-22 · Windows 11 · i7-1165G7 · protocolo intercalado. Duas
sessões: mediana de 9 (**a reportada**) e mediana de 5 (corroboração). Máquina
**ociosa**: CPU total ~4% antes de começar, nenhum processo de fundo acima de
~7% de um core durante as sessões (Firefox e Slack abertos e parados; sem
Zoom/Chrome), na tomada. Dados brutos de todas as sessões e sondas em
`results/2026-08-22-v0141-idle-raw.md`.

Re-medição da seção seguinte (2026-08-21, feita com Zoom/Chrome/Slack
ativos): mesmo protocolo, mesmos scripts e **mesmo binário v0.6.0 byte a
byte**; o candidato passou a ser a v0.14.1 (HEAD de `develop`). As versões
0.14.x não tocam o caminho quente do VM (`range` builtin, `sys.version`,
editor de linha do REPL), e a sanidade v0.13.0 × v0.14.1 intercalada (n=5)
confirma: benches longos entre −3,4% e +3,2%, sem padrão — as conclusões
abaixo valem, portanto, para o par v0.6.0 × v0.13.0 da seção anterior.

**Resumo:** com a máquina ociosa, o saldo das oito versões **não é o da seção
anterior**. Não há ganho no caminho de chamada tipada — os −15%/−13%/−6% eram
o piso de processo da v0.6.0 inflado pela carga, não trabalho do VM. O que
sobra é **uma regressão real de ~10% na leitura O(n) de array com chamada**
(`call_readonly`), regressões pequenas e de sinal consistente em `call_ref` e
`conway` (+2–4%), e empate no resto. O ganho grande do intervalo continua
sendo o de **carga de módulos (2,7x a 10,1x)**, confirmado.

| bench | v060_ms | v0141_ms | delta (n=9) | delta (n=5) | veredito |
|---|---|---|---|---|---|
| bench_call_readonly | 1240,0 | 1369,3 | **+10,4%** | +9,3% | ⚠️ regressão confirmada (antes +4,2% / +9,1%) |
| bench_call_ref | 3711,0 | 3838,7 | **+3,4%** | +1,8% | ⚠️ regressão pequena (antes +1,8% / +2,9%) |
| bench_conway | 2119,2 | 2168,9 | +2,3% | +3,6% | ⚠️ regressão pequena, sinal agora consistente (antes ruído) |
| bench_bubblesort | 4137,6 | 4141,8 | +0,1% | +3,9% | ➖ inconclusivo (antes +4,8% / +9,0%) |
| bench_path_update | 634,3 | 641,9 | +1,2% | −2,5% | ➖ ruído (antes −4,8% / −4,3%, "ganho") |
| bench_map_churn | 451,4 | 449,0 | −0,5% | −0,1% | ➖ empate |
| bench_share_mutate | 266,0 | 264,7 | −0,5% | −3,5% | ➖ empate |
| bench_spawn_sum | 725,6 | 726,8 | +0,2% | −1,7% | ➖ empate |
| bench_call_light | 114,5 | 113,5 | −0,9% | +0,8% | ➖ no piso¹ (antes −6,1%) |
| bench_typed_call_map | 109,0 | 121,9 | +11,8% | +5,6% | ➖ no piso¹ (antes **−15,3%**) |
| bench_value_call_mutate | 120,7 | 115,8 | −4,1% | +21,3% | ➖ no piso¹ (antes **−12,9%**) |

`bench_generic_vs_hand` segue **pulado** (a v0.6.0 não faz parse de `<T>`); na
sanidade v0.13.0 × v0.14.1 deu −1,2%.

¹ Ver "Três benches estão no piso de processo" abaixo.

### Leitura

**A regressão na leitura de array é real e maior do que a seção anterior
dizia.** `call_readonly` (+10,4% e +9,3%, duas sessões, bench de 1,2 s) é o
sinal mais limpo da rodada. O cross-runtime da mesma data concorda: o
`bubblesort.nx` de lá (indexação pura, sem chamada) dá a v0.14.1 **1,06x–1,10x**
atrás da v0.6.0 nas três suítes, e `fib` (chamada pura) **1,05x–1,08x** — ver
[`cross_runtime/README.md`](cross_runtime/README.md). O quadro coerente é:
**o caminho de leitura indexada e o protocolo de chamada ficaram 5–10% mais
lentos entre a v0.6.0 e a v0.14.1**, o resto do VM empatou. É o perfil
esperado dos guards de `Shared`/RC por acesso que as versões de CoW e de slot
`ref` adicionaram sem fase de perf — continua sendo o item a investigar,
agora com sinal mais forte. Atribuição por perfil, não por bisect.

`bench_bubblesort.nx` daqui (+0,1% e +3,9%) **não** confirma com clareza o que
a seção anterior chamava de regressão de +4,8%; fica inconclusivo. É o bench
mais longo da suíte (~4 s) e o mais exposto à deriva de frequência dentro da
sessão — o sinal de leitura indexada que vale é o de `call_readonly`.

**Os "ganhos" de chamada tipada da seção anterior não existem.**
`typed_call_map` (−15,3% → agora +11,8% / +5,6%), `value_call_mutate` (−12,9%
→ −4,1% / +21,3%) e `call_light` (−6,1% → −0,9% / +0,8%) trocaram de sinal ou
viraram ruído. A atribuição a `58f2cad` (#55, retain/release a menos por
chamada que cruza a fronteira) **fica retirada**: o que aquelas medições
captaram foi o piso de processo da v0.6.0 sob carga (139,7 ms contra 116,3 ms
da v0.13.0 no cross-runtime de 2026-08-21), e a máquina ociosa desfaz isso —
hoje os dois binários têm o mesmo piso (ver sondas abaixo).

**Três benches estão no piso de processo.** `call_light`, `typed_call_map` e
`value_call_mutate` somam 95–125 ms com piso de ~84 ms: medem 10–35 ms de
trabalho, e um jitter de 5 ms vira ±5% do total. Estão abaixo da resolução do
protocolo (mediana de 9 entre processos). Medidos à parte em três vias
(v0.6.0 / v0.13.0 / v0.14.1, 15 execuções intercaladas, mínimo): 92,2 / 93,7
/ 99,0 · 96,2 / 98,6 / 108,2 · 93,1 / 90,9 / 99,6 ms — v0.6.0 e v0.13.0
empatam; a v0.14.1 aparece 5–10 ms acima, **o mesmo deslocamento que um build
fresco do próprio 63ab106 mostra contra o binário da rodada anterior** (89,4
vs 84,4 ms no piso puro), portanto artefato de arquivo/ruído de piso, não
código. Para esses três decidirem alguma coisa, a contagem de iterações tem
de subir até o trabalho ser ≥ 5x o piso — o mesmo follow-up já aberto no
cross-runtime.

**Ruído, mesmo com a máquina ociosa:** o tempo absoluto derivou ~15% entre
sessões (`bench_bubblesort` v0.6.0: 4137,6 / 3590,4 / 3721,1 ms em três
sessões consecutivas, sem carga de fundo), e `value_call_mutate` foi de −4,1%
a +21,3% só trocando n=9 por n=5. Vale o que já estava escrito: delta abaixo
de ~3% numa sessão não é conclusivo, e a única coisa comparável é o delta
dentro da janela intercalada — nunca ms entre sessões ou datas.

### Carga de módulos: o ganho grande confirma

Mesmas sondas, mínimo de 15 execuções intercaladas, fontes copiados para
disco local:

| sonda | v060_ms | v0141_ms | delta | (2026-08-21, sob carga) |
|---|---|---|---|---|
| `startup_use_selectall.nx` (`use http select *`) | 1370,2 | 135,5 | **−90,1%** (10,1x) | −89,8% (9,8x) |
| `startup_use_namespace.nx` (`use http`) | 346,2 | 129,6 | **−62,6%** (2,7x) | −66,0% (2,9x) |
| `startup.nx` (nenhum `use`, controle) | 83,3 | 88,1 | +5,8% | −16,1% |

Os dois ganhos de carga de módulos (`19156a7`, memoização de
`loadModuleDeclarations`) reproduzem dentro de 1–3 pontos. O **−16,1% no piso
puro não reproduz**: ociosa, a máquina dá o mesmo piso aos dois binários
antigos (84,1 ms v0.6.0, 84,4 ms v0.13.0, em quatro vias), e os +4–5 ms da
v0.14.1 são o deslocamento de build fresco citado acima. O "ganho menor e
separado no piso de processo puro, sem causa atribuída" da seção anterior
era carga, não código.

### O que muda em relação à seção de 2026-08-21

| afirmação de 2026-08-21 | hoje (máquina ociosa) |
|---|---|
| caminho de chamada tipada −6% a −15%, atribuído ao #55 | não existe; era piso sob carga — atribuição retirada |
| `call_readonly` +4,2% e `bubblesort` +4,8%, regressões | `call_readonly` **+10%** confirmada; `bubblesort` inconclusivo (+0,1% / +3,9%) |
| startup puro −15% a −16% | empate (piso idêntico nos dois binários) |
| carga de módulos 2,9x–9,8x | **confirma**: 2,7x–10,1x |
| `loop_arith` +13% no cross-runtime, "única regressão consistente" | não reproduz (0,95x–1,03x); as regressões consistentes lá são `bubblesort` 1,06x–1,10x e `fib` 1,05x–1,08x |
| VM "empatado" no saldo | **5–10% mais lento em leitura indexada e chamada**, empate no resto |

Lição de método, além da que a seção anterior já registrou: intercalar
protege a comparação contra deriva **dentro** da janela, mas não contra um
efeito que atinge um só lado de forma sistemática. Sob carga, o piso de
processo inflou de modo diferente para os dois binários (139,7 vs 116,3 ms),
e isso se disfarça de ganho de VM em qualquer bench curto — bench com trabalho
abaixo de ~5x o piso não decide nada, com ou sem carga.

## v0.6.0 (68209be) × v0.13.0 (63ab106) — sete versões de saldo

> **Superada pela re-medição de 2026-08-22 (seção acima).** Esta rodada foi
> feita com Zoom/Chrome/Slack ativos; com a máquina ociosa, os ganhos de
> chamada tipada (−15,3% / −12,9% / −6,1%) e o startup −15% **não se
> reproduziram** (eram piso de processo sob carga), a regressão de
> `call_readonly` ficou maior (+10%) e a de `bubblesort` inconclusiva; a
> carga de módulos 2,9x–9,8x confirma. Mantida como registro; a atribuição
> ao #55 feita abaixo fica retirada.

**Data:** 2026-08-21 · Windows 11 · i7-1165G7 · protocolo intercalado. Duas
sessões: mediana de 9 (**a reportada**) e mediana de 5 (corroboração). Máquina
com carga de fundo (Zoom/Chrome/Slack) — o que valida a comparação é ela ser
intercalada, não a máquina estar limpa.

Esta seção não é o A/B de uma mudança: é o **saldo acumulado** da v0.6.0 (fim
da fase 1 de perf de dispatch e chamadas) até a v0.13.0, atravessando CoW por
valor, invariante de slot `ref`, genéricos monomorfizados, `io` com cursor e
tipagem estática de membro qualificado. Nenhuma dessas versões teve fase de
perf; várias adicionaram guards de RC/CoW no caminho quente.

**Resumo:** o trabalho do VM ficou **empatado** — ganhos de 5% a 15% no caminho
de chamada tipada, regressões de ~4% na leitura O(n) de array, e o resto no
ruído. O ganho grande do intervalo está em outro eixo e não aparece nos
`bench_*.nx`: **carga de módulos, 2,9x a 9,8x mais rápida** (seção própria
abaixo).

| bench | v060_ms | v0130_ms | delta (n=9) | delta (n=5) | veredito |
|---|---|---|---|---|---|
| bench_typed_call_map | 175,0 | 148,2 | **−15,3%** | −16,3% | ✅ ganho |
| bench_value_call_mutate | 169,2 | 147,4 | **−12,9%** | −17,2% | ✅ ganho |
| bench_call_light | 166,6 | 156,5 | **−6,1%** | −3,9% | ✅ ganho |
| bench_path_update | 935,9 | 891,3 | **−4,8%** | −4,3% | ✅ ganho |
| bench_spawn_sum | 947,7 | 933,2 | −1,5% | −2,4% | ✅ ganho pequeno |
| bench_conway | 2615,6 | 2637,2 | +0,8% | −1,3% | ➖ ruído (sinal troca) |
| bench_share_mutate | 405,3 | 406,4 | +0,3% | −12,9% | ➖ ruído (sinal troca) |
| bench_map_churn | 938,9 | 913,8 | −2,7% | +1,5% | ➖ ruído (sinal troca) |
| bench_call_ref | 4593,3 | 4674,2 | +1,8% | +2,9% | ⚠️ regressão pequena |
| bench_call_readonly | 1510,7 | 1573,7 | **+4,2%** | +9,1% | ⚠️ regressão |
| bench_bubblesort | 4377,0 | 4587,3 | **+4,8%** | +9,0% | ⚠️ regressão |

`bench_generic_vs_hand` foi **pulado**: usa `<T>`, que a v0.6.0 não faz parse.
Ver a nota sobre o guard de checksum abaixo.

### Leitura

**O caminho de chamada tipada ficou materialmente mais rápido.** Os três
maiores ganhos — `typed_call_map` (−15,3%), `value_call_mutate` (−12,9%) e
`call_light` (−6,1%) — são todos chamada com composto tipado atravessando a
fronteira. O candidato mais provável é `58f2cad` (#55, v0.10.1): construtores
de `internal/value` viraram donos duráveis dos filhos, `invokeBoundaryCall`
**deixou de reter o result** e `retainingArray`/`retainingMap` sumiram — ou
seja, retain/release a menos exatamente por chamada que cruza a fronteira.
Atribuição por coincidência de perfil, não por bisect: 155 commits separam os
dois binários e nenhum bisect foi feito nesta rodada.

Não é a validação de tipos O(1) por tag (PR #31): ela foi mergeada em
2026-08-16 e **já está na v0.6.0** (tag de 2026-08-18) — os dois binários a
têm.

**As duas maiores regressões estão no lado da leitura.** `call_readonly` (+4,2%) e
`bench_bubblesort` (+4,8%) são leitura O(n) de array — indexação repetida,
sem mutação. É o perfil onde os guards de `Shared`/RC por acesso aparecem sem
nada para compensar. **É o item a investigar**, e coincide com o que o
cross-runtime aponta como pior ponto absoluto (indexação de array, 8,5x do
CPython).

**Três benches ficaram no ruído com troca de sinal** (`conway`, `share_mutate`,
`map_churn`). `share_mutate` foi de −12,9% para +0,3% entre as duas sessões, e
o baseline de `map_churn` variou 53% (612,9 → 938,9 ms) só por carga — lembrete
de que o piso de ruído desta suíte não é ±1%, e que delta abaixo de ~3% em uma
única sessão não é conclusivo.

### O maior ganho do intervalo não está na tabela acima: carga de módulos

Os `bench_*.nx` são todos de um arquivo só, então nenhum deles exercita `use`.
As sondas de startup (`startup_use_*.nx`, que o `interleaved_compare.ps1` não
varre) medem exatamente isso — mínimo de 15 execuções intercaladas:

| sonda | v060_ms | v0130_ms | delta |
|---|---|---|---|
| `startup_use_selectall.nx` (`use http select *`) | 1873,2 | 190,5 | **−89,8%** (9,8x) |
| `startup_use_namespace.nx` (`use http`) | 437,2 | 148,5 | **−66,0%** (2,9x) |
| `startup.nx` (nenhum `use`, controle) | 154,9 | 130,0 | −16,1% |

Causa: `19156a7`, memoização de `loadModuleDeclarations`
(`internal/compiler/module_exports.go`). Antes, cada `use` era recarregado por
chamador e dobrado pelo two-pass; agora é uma carga por módulo.

O comentário em `startup_use_selectall.nx` atribui a amplificação ao branch de
genéricos, mas **a v0.6.0 é anterior a genéricos e mesmo assim paga 1,87 s** —
ou seja, o recarregamento por chamador já existia antes, e genéricos apenas o
dobraram. A memoização consertou os dois.

Isto é custo de **compilador**, não de VM: aparece uma vez por processo, e é o
que domina o tempo de qualquer script curto que use a stdlib — o caso Lambda,
por exemplo. O controle sem `use` (−16,1%) confirma que há também um ganho
menor e separado no piso de processo puro, esse ainda sem causa atribuída.

**Contexto de sistema:** o cross-runtime da mesma data mede o mesmo par de
binários contra CPython/Lua/Go e conclui empate no trabalho do VM com startup
−15% — ver
[`cross_runtime/README.md`](cross_runtime/README.md).

### Correção de metodologia aplicada nesta rodada

`interleaved_compare.ps1` **não validava equivalência** entre os dois binários.
Um bench que o baseline não compila sai do erro em ~30 ms e entrava na tabela
como regressão gigante do candidato — a comparação seria entre um programa e
uma mensagem de erro. `bench_generic_vs_hand` é exatamente esse caso contra a
v0.6.0.

O script agora exige linha `CHECKSUM:` idêntica dos dois lados no warmup (mesmo
guard que `cross_runtime/run_cross_runtime.ps1` já tinha) e **pula** o bench,
listando-o no relatório, em vez de medi-lo. Os rótulos das colunas também
deixaram de ser fixos em `baseline`/`cow` (herança da rodada do CoW) e passaram
a `-BaselineLabel`/`-CandidateLabel`.

## develop (bff429a) × genéricos paramétricos (feat/generics)

**Data:** 2026-08-18 · Windows 11 · protocolo intercalado, mediana de 9
execuções por sessão. Spec: `docs/superpowers/specs/2026-08-18-generics-design.md`
§11. Task: `.superpowers/sdd/2026-08-18-generics/task-16-brief.md`. Gates
novos desta seção: `generic_vs_hand` e regressão do corpus não-genérico são
gates duros; `startup_generics` é informativo (spec §11: "sem regressão
material do startup", sem limiar numérico fixado — este relatório adota
sinalização em `> +15ms` como critério operacional, per orientação da task
brief).

**Verificação completa:** `go vet ./...` sem saída (limpo); `go test ./...
-count=1` verde em todos os pacotes (`internal/compiler` 68,117s,
`internal/vm` 145,820s, resto sub-segundo — a suíte de `internal/vm` inclui
os testes novos de genéricos: unificação, cloner de AST com guard por
reflexão, igualdade de bytecode, E2E, interações de runtime, negativos).
**Corpus de exemplos 164/164** (`run_all_tests_concurrent.nx`, 20187ms) no
binário `feat/generics` (`noxy_generics_bench.exe`, `c93cfb3`) — mesmo número
já reportado na seção da fase 1 de dispatch, confirmando que o corpus
existente continua passando por inteiro com genéricos ativos (gate de
regressão do corpus, spec §11).

### Gate 1 — `generic_vs_hand`: razão ≤ 1,05 (Ruling R4 do controller)

**Divergência registrada:** a spec §11 declara gate de razão **1,0x**; o
controller (Ruling R4, binding) definiu **≤ 1,05** como folga de ruído sobre
esse 1,0x — a garantia dura de custo-zero continua sendo o teste de
igualdade de bytecode (Task 8: `first<int>` monomorfizado emite a mesma
sequência de opcodes que a versão escrita à mão), não este benchmark de
parede. Este relatório usa 1,05 por autoridade de R4.

`benchmarks/bench_generic_vs_hand.nx` só roda no binário `feat/generics` (o
binário `develop` não faz parse de `<T>`), então não há segundo binário para
intercalar contra. Em vez disso a medição é **interna ao processo**
(`time_now_ms()`), em ordem **ABBA** (gen, hand, hand, gen) dentro da mesma
execução — necessário porque uma medição AB simples (gen sempre primeiro)
mostrou deriva sistemática de até +9% entre sessões¹, e o gate de 5% é
apertado demais para o piso de ruído documentado no resto da suíte
(~1-4%). ABBA dá às duas funções a mesma posição média (2,5) na sequência,
cancelando deriva linear dentro do processo. O binário inteiro roda 9 vezes
por sessão; mediana de `GEN_MS`/`HAND_MS` calculada separadamente, razão das
duas medianas.

| sessão | gen_ms (mediana) | hand_ms (mediana) | razão |
|---|---|---|---|
| 1 | 387 | 400 | 0,968 |
| 2 | 378 | 387 | 0,977 |
| 3 | 377 | 374 | 1,008 |

**Agregado (mediana das 3 sessões, por lado):** gen_ms=378, hand_ms=387,
**razão = 0,977** → ✅ **dentro do gate** (≤ 1,05). Checksum idêntico
(`384128000`) em todas as 27 execuções (3 sessões × 9), confirmando que
`soma_gen<int>` e `soma_hand` computam o mesmo resultado.

### Gate 2 — regressão do corpus não-genérico (develop × genéricos)

Roda 3 benchmarks existentes não-genéricos (`bench_bubblesort.nx`,
`bench_call_light.nx`, `bench_typed_call_map.nx` — os três já rastreados
como gate na seção da fase 1 de dispatch) via `interleaved_compare.ps1`
apontado para um diretório temporário só com esses três arquivos (copiados;
o script varre `bench_*.nx` do próprio diretório e os dois arquivos novos
desta tarefa usam sintaxe `<T>` que o binário `develop` não faz parse —
incluí-los quebraria a varredura inteira no lado `develop`). **5 sessões**
rodadas (uma sessão inicial cujo `results/interleaved.md` falhou por
diretório ausente, valores só no console — mantida porque a medição em si é
válida — mais 4 sessões limpas), porque `bench_typed_call_map` deu um
outlier de +12,6% na sessão 2, acima do limiar de ~5% usado no resto do
projeto — ver nota².

| bench | develop_ms | generics_ms | delta agregado | veredito |
|---|---|---|---|---|
| bench_bubblesort | 4013,8 | 4050,0 | +0,90% | ✅ |
| bench_call_light | 78,0 | 75,9 | −2,69% | ✅ |
| bench_typed_call_map | 87,1 | 86,6 | −0,57%² | ✅ |

Valores agregados: mediana das 5 sessões, calculada separadamente para
`develop_ms` e `generics_ms`, delta recalculado a partir das duas medianas
(mesma convenção da seção da fase 1 de dispatch).

### Info — `startup_generics` (sem gate duro; sinalizar > +15ms)

`benchmarks/startup_generics.nx` (1 template `Caixa<T>` + 2 instanciações,
`Caixa<int>` e `Caixa<string>`, + print) contra
`benchmarks/cross_runtime/startup.nx` (piso de processo sem genéricos),
**ambos no binário `feat/generics`** — isola o custo marginal da detecção
"há genéricos?" e do pass 1 de monomorfização sobre o mesmo binário, sem
confundir com qualquer outra diferença entre binários. Script ad hoc
(`interleaved_files.ps1`, mesma técnica de `interleaved_compare.ps1` com o
eixo trocado: binário fixo, arquivo variável), 3 sessões de 9 execuções.

| sessão | startup_ms (sem genéricos) | startup_generics_ms | delta |
|---|---|---|---|
| 1 | 51,3 | 53,3 | +2,0ms |
| 2 | 52,2 | 52,8 | +0,6ms |
| 3 | 51,4 | 55,4 | +4,0ms |

**Agregado:** 51,4ms → 53,3ms, **delta = +1,9ms** — bem abaixo do limiar de
sinalização de +15ms. Custo de compilação dobrando sobre uma base de poucos
ms (spec §11: "a compilação ~dobra, sobre uma base de poucos ms") é
consistente com a ordem de grandeza aqui: o programa inteiro (parse + duas
passes + compile + run + print) segue no mesmo patamar do piso de processo
(~50-55ms), a monomorfização de 1 template com 2 instanciações não é visível
acima do ruído de spawn de processo nesta escala.

¹ Medição AB descartada (não commitada): mesmo bench, ordem fixa gen-depois-
hand, 2 sessões de 9 execuções — sessão 1 deu razão 1,008 (dentro do gate),
sessão 2 deu **1,094** (acima do gate de 1,05). A causa aparente é a segunda
função chamada dentro do processo herdar cache/branch-predictor "quente" da
primeira, não custo real de despacho genérico — a versão ABBA committada
elimina essa fonte de viés por construção (ver corpo do gate 1 acima).

² O outlier da sessão 2 (`bench_typed_call_map` +12,6%, base=79,9ms
cand=90ms) não se repete nas outras 4 sessões (deltas: +0,9%, −0,6%, −0,1%,
−3,2%) — é o bench de menor escala absoluta dos três (~80-90ms), mesmo
padrão de sensibilidade a ruído de sistema já documentado para benches
curtos na seção da fase 1 de dispatch (nota¹ daquela seção). A mediana
agregada das 5 sessões (−0,57%) fica bem dentro do gate; sem esse outlier a
leitura seria idêntica.

### Interpretação

**Gate 1 confirma o custo-zero em runtime além do teste de bytecode.** A
razão agregada (0,977, faixa 0,968–1,008 nas 3 sessões) está centrada em
torno de 1,0, sem viés sistemático para nenhum lado — exatamente o esperado
quando `soma_gen<int>` e `soma_hand` emitem a mesma sequência de opcodes
(Task 8). A divergência do gate declarado na spec (1,0x → ≤1,05x, Ruling R4)
existe para dar folga ao ruído de medição de parede, não porque exista custo
real: o teste de bytecode é quem carrega a prova, este benchmark é
confirmação empírica adicional.

**Gate 2 confirma que o corpus não-genérico não paga nada pela existência de
genéricos no compilador.** As três magnitudes agregadas (+0,90%, −2,69%,
−0,57%) ficam dentro do mesmo piso de ruído (~1-4%) documentado nas seções
anteriores deste arquivo para essa máquina — nenhum sinal de regressão real.
Consistente com a spec §11: "programa sem genéricos continua pulando o pass
1 inteiro".

**O startup paga um custo pequeno e não sinalizável.** +1,9ms agregados (faixa
+0,6 a +4,0ms nas 3 sessões) para compilar um programa com 1 template e 2
instanciações, contra um piso de processo de ~51ms — ordem de grandeza
consistente com a spec (compilação "~dobra... sobre uma base de poucos ms").
Não há indício de que a detecção "há genéricos?" ou o pass 1 de
monomorfização introduzam custo que se acumule com o tamanho do programa
nesta escala; ficaria para benchmarks futuros com mais templates/instâncias
verificar se o custo permanece sub-linear.

### Adendo — startup de `use` (finding C1 da revisão final de branch)

**Data:** 2026-08-18, sessão posterior às medições acima. Motivo: a revisão
final de branch mediu um caso que `startup_generics` **não** cobria —
startup de um programa que só faz `use`, **sem genérico nenhum** — e achou
uma amplificação de ~8x. Gate 2 (corpus não-genérico) não a pegou porque os
três benches daquele gate não importam módulo.

**Causa** (`internal/compiler/module_exports.go`): `loadModuleDeclarations`
re-parseava **e** re-compilava (validator descartável) o módulo a cada
chamada, recursivamente, sem cache algum. O branch de genéricos somou ~4
chamadas por `use` (`predeclareImportedTemplates` →
`discoverModuleExports` + `moduleTopLevelBindings`; `predeclareImport`; o
case `*ast.UseStmt`), tudo dobrado pelo two-pass. Custo pré-existente
(base já pagava 1,4s num `select *`), amplificado pelo branch.

**Correção:** memoização por `moduleDiscoveryState` (módulo → programa
parseado + submódulos), com o estado agora criado **uma vez por compilador**
(`discoveryState()`) e propagado por `NewChild`/`newPass1Compiler` — antes,
cada chamador fabricava um estado descartável e nenhum memo sobreviveria.
Só sucessos entram no cache: uma falha pode ser contextual (guard de ciclo),
um sucesso nunca é.

Sondas versionadas: `benchmarks/startup_use_selectall.nx` e
`benchmarks/startup_use_namespace.nx`. Mediana de 9 execuções por célula,
mesmo processo/binário por linha. `head (pré-C1)` = `314428d`, último commit
do branch antes desta rodada de fixes.

| sonda | base (`bff429a`) | head (pré-C1) | head (pós-C1) |
|---|---|---|---|
| `startup_use_selectall.nx` (`use http select *`) | 1445,8 ms | 11681,2 ms | **96,2 ms** |
| `startup_use_namespace.nx` (`use http`) | 361,5 ms | 1474,4 ms | **101,2 ms** |
| `startup_generics.nx` (controle, com genéricos) | 62,3 ms | 65,8 ms | 60,2 ms |

O head pós-fix não só volta à ordem de grandeza da base como fica **~15x
mais rápido que a base** no `select *` — a memoização paga também o custo
pré-existente, que a base sempre teve. A sonda de controle
(`startup_generics`) não se move: o caminho de genéricos não foi tocado.

Efeito colateral na suíte de testes (mesma causa, mesmo fix):
`go test ./internal/compiler` 48,7s → **1,3s**; `go test ./internal/vm`
128,8s → **41,6s**.

**Corpus:** 167/167 (`run_all_tests_concurrent.nx`, 11502ms) no binário
pós-fix — sem regressão.

## develop (f107508) × fase 1 de dispatch e chamadas (perf/vm-dispatch-fase1)

**Data:** 2026-08-18 · Windows 11 · protocolo intercalado, mediana de 9
execuções por sessão — **4 sessões limpas repetidas** (nenhum outro processo
de benchmark ativo) porque `bench_share_mutate` e `bench_call_light` ficaram
perto da fronteira do gate/com dispersão notável entre sessões; ver nota¹
sobre o ruído e nota² sobre duas sessões adicionais descartadas por
contaminação de medição. Spec:
`docs/superpowers/specs/2026-08-17-vm-perf-static-typing-research.md`.
Commits do branch: cache de globais com geração (`67e9d52`), `OP_CALL_STATIC`
(`bb8a773`), `CallFrame` em array de valores reusado, allocs/chamada 1012→10
(`1ca26e7`+`d460e5a`), seis opcodes fundidos de comparação+salto inteiro
(`c43276c`+`32b33c7`), `OP_INC_LOCAL_INT` (`56882ca`), seis opcodes `_FLOAT`
especializados (`4d43cdf`+`6d991e5`).

**Verificação completa (Step 1):** `go vet ./...` sem saída (limpo);
`go test ./... -count=1` verde em todos os pacotes (`internal/compiler`
5,072s, `internal/vm` 58,068s, resto sub-segundo); `go test ./internal/value
-race -count=1` verde (1,093s); `go test ./internal/vm -race -count=1` verde
(74,934s). **Corpus de exemplos 164/164** (`run_all_tests_concurrent.nx`,
10849ms) — número corrigido de 130 para 164 no commit `e7c533d`, confirmado
aqui na íntegra.

| bench | perfil | develop_ms | perf_ms | delta | veredito |
|---|---|---|---|---|---|
| bench_call_readonly | leitor puro O(n), array 20k | 1732,3 | 1121,3 | **−35,3%** | ✅ |
| bench_spawn_sum | soma paralela via spawn_task | 1018,2 | 696,8 | **−31,6%** | ✅ |
| bench_conway | grid mutado via ref, 60 gerações | 2554,3 | 2063,0 | **−19,2%** | ✅ gate |
| bench_call_ref | mutação in-place via ref | 4206,5 | 3622,0 | −13,9% | ✅ |
| bench_map_churn | escrita intensa em map | 443,8 | 394,1 | −11,2% | ✅ |
| bench_bubblesort | sort in-place via ref | 3897,5 | 3493,5 | −10,4% | ✅ |
| bench_path_update | `a[i].x = v` em loop, dono único | 621,1 | 580,0 | −6,6% | ✅ |
| bench_typed_call_map | chamada tipada in-module, map 2,5k via ref | 76,9 | 73,4 | −4,6% | ✅ gate |
| bench_call_light | chamada O(1), array 10k por valor | 65,5 | 62,6 | −4,5%¹ | ✅ gate |
| bench_value_call_mutate | helper por valor em laço de mutação | 70,1 | 67,1 | −4,3% | ✅ |
| bench_share_mutate | compartilha e muta em loop | 232,9 | 249,1 | **+7,0%**³ | ❌ **acima do gate** |

Valores agregados: mediana das 4 sessões limpas, calculada separadamente para
`develop_ms` e `perf_ms` (cada sessão já é mediana de 9 execuções
intercaladas), delta recalculado a partir das duas medianas.

¹ `bench_call_light` variou de −12,9% a +0,6% entre as 4 sessões (mediana de
deltas por sessão: −8,3%); a sessão que deu +0,6% é a mesma que deu o pior
número de `bench_share_mutate` (+10,5%, nota³) — indício de uma perturbação
pontual de sistema naquela sessão específica, não um padrão. Mesmo no pior
caso agregado (−4,5%), o bench segue dentro do gate.

² Duas sessões adicionais foram descartadas: por um erro de execução, um
processo de benchmark em background (`interleaved_compare.ps1` intercalando
os mesmos binários) ficou rodando **ao mesmo tempo** que uma segunda
invocação em primeiro plano — os dois competiam por CPU. Os deltas dessas
duas sessões contaminadas (`bench_call_readonly` −33% a −36%,
`bench_conway` −17% a −21%, `bench_spawn_sum` −31% a −33%) são consistentes
em direção com as 4 sessões limpas usadas na tabela acima, então a
contaminação não parece ter invertido nenhum resultado — mas foram
descartadas mesmo assim por não seguirem o protocolo (nenhum outro processo
de benchmark ativo), e por terem escrito no mesmo arquivo
`results/interleaved.md` uma da outra.

³ **Gate excedido.** `bench_share_mutate` — pior caso do CoW por construção
(`let b = a` seguido de mutação de um array de 5000 elementos, 2500 rodadas)
— fica **acima dos +5%** nas 4 sessões limpas: +6,4%, +6,1%, +2,8%, +10,5%
(mediana das 4 deltas por sessão: +6,25%; delta das medianas agregadas:
+7,0%). Só 1 das 4 sessões (+2,8%) ficaria dentro do gate; as outras 3
excedem. Um profile do candidato (`noxy_perf.exe --cpuprofile`, 15 repetições
da carga do bench para juntar amostra) mostra o tempo extra concentrado no
caminho de clone/CoW já existente — `(*VM).copyValue` (42,3% cum),
`runtime.scanobject`/`scanblock` (GC, ~27% cum), `value.Retain`,
`value.ownersOf`, `runtime.bulkBarrierPreWrite` — **nenhum símbolo da fase 1**
(cache de globais, `OP_CALL_STATIC`, `CallFrame`, opcodes fundidos) aparece
nesse profile. Não há profile do baseline para comparar lado a lado (a flag
`--cpuprofile` é ela própria um commit deste branch — `noxy_develop.exe` não
tem a flag), então a causa não pôde ser isolada: hipótese em aberto é efeito
colateral de pressão de GC do novo layout do `CallFrame` (array de valores
reusado pode manter o slot anterior vivo por mais tempo antes de ser
sobrescrito, mudando o que o GC varre por chamada); alternativa é ruído puro
— é o bench de menor escala absoluta da suíte (~230-250ms), o que amplia
qualquer jitter de agendamento em pontos percentuais.

**Bissecção posterior (2026-08-18)** resolveu a dúvida ruído-vs-real e
descartou a hipótese de ruído puro. Protocolo: worktree destacada, quatro
commits medidos intercalados (`f107508` baseline, `bb8a773` = tudo antes do
reuso de `CallFrame`, `d460e5a` = reuso de frame já corrigido, `6d991e5` =
head), 3 sessões (N=15, N=20, N=20), com **controle de rótulo duplicado
apontando para o mesmo binário** nas sessões 2-3 para medir o piso de ruído.

- **Piso de ruído** (mediana × mediana, mesmo executável, mesma sessão):
  **~1,2-1,4%**. Amostras cruas são bem mais dispersas (desvio ~6-12% da
  mediana em trechos limpos, até ~20-28% de CV quando houve contaminação de
  fundo), o que explica a dispersão entre sessões da tabela acima.
- **A regressão é real e reprodutível**: baseline→head deu +9,97%, +9,78% e
  +6,47% nas três sessões (média **+8,7%**), ordem de grandeza acima do piso
  — consistente com os +7,0% da tabela.
- **A atribuição a um único commit NÃO fecha**: o passo
  `bb8a773`→`d460e5a` (reuso de `CallFrame`) concentrou todo o salto na
  sessão 1 (+14,9%), mas só +1,4% e +4,0% nas sessões 2-3, onde a regressão
  se acumulou gradualmente ao longo dos commits seguintes. Na média o reuso
  de frame ainda é o maior contribuinte isolado (+6,8pp de +8,7pp) e é por
  onde começar, mas provavelmente não é a causa única — o padrão é
  compatível com efeito difuso de alocação/GC espalhado por vários commits
  da fase, o que também explica o profile não mostrar nenhum símbolo da
  fase 1 no caminho quente.

Detalhes e medições cruas (protocolo, os 4 commits medidos, todas as
amostras individuais e o controle de rótulo duplicado):
`benchmarks/results/2026-08-18-share-mutate-bisect.md`. **Regressão aceita e
rastreada, não corrigida nesta fase** — ver Interpretação.

### Perfil de cada bench

- **`bench_call_readonly`/`bench_spawn_sum`/`bench_conway`** — os três
  maiores ganhos (−35,3%, −31,6%, −19,2%) surpreendem porque nenhum dos três
  é o alvo declarado da fase (fib/loop_arith/mandelbrot). Motivo: todos têm
  laço quente `while i < N do ... i = i + 1 end` (ganha com os seis opcodes
  fundidos de comparação+salto e `OP_INC_LOCAL_INT`) **e** chamada de função
  in-module (`spawn_sum` chama a task por closure; `conway` chama `step`/`idx`
  por iteração; `call_readonly` chama o leitor a cada rodada) — ganha também
  com `OP_CALL_STATIC` e o `CallFrame` sem alocação. A fase mirava fib para
  #1/#2 e loop_arith/mandelbrot para #3, mas as duas otimizações de chamada
  (#1/#2) valem para **qualquer** chamada in-module tipada, não só fib — e é
  isso que aparece aqui.
- **`bench_call_ref`/`bench_map_churn`/`bench_bubblesort`/`bench_path_update`**
  — mesma explicação em menor grau (−13,9% a −6,6%): laço com incremento
  inteiro fundido, mas o custo dominante desses benches é o caminho de
  referência/CoW (`resolveReferenceValue`, unicidade), que esta fase não
  tocou — por isso o ganho é menor que nos três benches acima.
- **`bench_typed_call_map`/`bench_call_light`** (gates) **e
  `bench_value_call_mutate`** (não é gate, mesmo padrão) — chamada
  O(1)/O(2500) com container grande por `ref`/valor, ganho modesto (−4,3% a
  −4,6%): já estavam perto do piso depois da validação O(1) por tag (PR
  #31), então sobra pouco para os opcodes desta fase cortarem.
- **`bench_share_mutate`** — ver nota³ acima: único bench que regride, e o
  único cujo perfil não mostra nenhuma infraestrutura desta fase.

### Interpretação

**O alcance da fase foi maior que o previsto.** O plano mirava
fib/loop_arith/mandelbrot para as otimizações de globais, chamadas e opcodes
fundidos (fases 0-3 da spec). Nos benches CoW — nenhum deles no escopo
original — 9 de 11 melhoraram, vários de forma expressiva
(`call_readonly` −35%, `spawn_sum` −32%, `conway` −19%): o custo de chamada
in-module (#1/#2) e o incremento fundido de laço (#3) atravessam qualquer
programa com esse formato, que é a maioria do corpus real.

**Onde fica neutro:** `bench_typed_call_map`, `bench_call_light` e
`bench_value_call_mutate` — os três já operavam perto do piso pós-validação
O(1) (PR #31); ganho pequeno mas real, dentro do gate.

**Onde paga, sem maquiagem:** `bench_share_mutate` excede o gate de 5% em 3
das 4 sessões limpas (mediana +6,25% a +7,0%, ver nota³). Ao contrário da
rodada da fase RC-uniqueness (onde `map_churn`/`spawn_sum` excediam o gate de
forma consistente e explicável pelo bookkeeping de RC), aqui a causa não está
clara: o profile do bench não mostra nenhum código desta fase, só o caminho
de clone/GC que já existia antes dela. **Gate formalmente reprovado** —
reportado sem tentativa de correção, para decisão do controller.

**O que resta:** o pprof pós-fase de `fib` (ver spec de pesquisa) mostra que,
com globais e alocação de chamada resolvidos, `push`/`pop` (Value de 48
bytes, item #3 da pesquisa) voltaram a ser a maior fração isolada depois do
dispatch puro — candidato natural da próxima fase. `bench_share_mutate`
citado acima é o outro fio solto: precisa de um profile do baseline (fora do
alcance sem recompilar `noxy_develop.exe` com a flag `--cpuprofile`) para
isolar a causa.

## develop (fac7542) × RC-uniqueness fase 1 (perf/cow-uniqueness-rc)

**Data:** 2026-08-17 · Windows 11 · medição intercalada final, mediana de 9
execuções (repetida em Runs=5 e Runs=9 para checar ruído — ver nota ² abaixo).
Spec: `docs/superpowers/specs/2026-08-17-cow-rc-uniqueness-design.md`.

**Corpus de exemplos 130/130 idêntico** em todas as verificações — rodado
após o flip do mecanismo (bit sticky `Shared` → contador `Owners`), após
cada round de correção relevante e após a limpeza final do bit morto.
`go test ./...` verde; `-race` verde em `internal/value` e na suíte completa
de `internal/vm` (`go test ./internal/value -race`: 1,9s; `go test
./internal/vm -race`, sem filtro: 150,3s — o contador `Owners` é atômico
justamente pelo requisito de tasks paralelas).

| bench | perfil | develop_ms | rc_ms | delta | veredito |
|---|---|---|---|---|---|
| bench_value_call_mutate¹ | helper por valor em laço de mutação, map crescendo | 1558.5 | 101.6 | **−93,5%** | ✅ alvo |
| bench_typed_call_map | chamada tipada in-module, map 2,5k via ref | 110.7 | 109.9 | −0,7% | ✅ gate |
| bench_share_mutate | compartilha e muta em loop | 322 | 332 | +3,1% | ✅ gate |
| bench_conway | grid mutado via ref, 60 gerações | 3734.1 | 3861.5 | +3,4% | ✅ |
| bench_bubblesort | sort in-place via ref | 6045.6 | 6285.7 | +4% | ✅ |
| bench_call_ref | mutação in-place via ref | 6139.3 | 6447.3 | +5% | ✅ |
| bench_call_light | chamada O(1), array 10k por valor | 100.3 | 105.4 | +5,1% | ✅ |
| bench_call_readonly | leitor puro O(n), array 20k | 2362.8 | 2519.8 | +6,6% | ⚠️ ruído² |
| bench_path_update | `a[i].x = v` em loop, dono único | 880.4 | 943.2 | +7,1% | ⚠️ ruído² |
| bench_spawn_sum | soma paralela via spawn_task | 1325.9 | 1463.5 | **+10,4%** | ❌ acima do gate |
| bench_map_churn | escrita intensa em map | 601.8 | 667.5 | **+10,9%** | ❌ acima do gate |

¹ Benchmark novo, adicionado nesta fase: ancora o padrão emblemático (NoxyDB
`database_file(db)` por valor dentro do laço de puts) que motivou a spec —
o mesmo formato do `bench_typed_call_map`, mas sem `ref` no helper.

² `bubblesort` já tinha variância documentada de até ±10% entre sessões (ver
seção baseline × CoW abaixo); `call_readonly` e `path_update` oscilaram
dentro de ~1-4 pontos percentuais entre as duas rodadas (Runs=5 e Runs=9)
desta seção, consistente com o mesmo ruído de máquina relatado nos rounds de
correção da fase 1 — não são benches rastreados pelo gate (ver Interpretação).

### Perfil de cada bench

- **`bench_value_call_mutate`** — o alvo da fase 1. `struct State{payloads:
  map[...]}`, `struct Db{state: State}`; um `helper(db: Db) -> int` recebido
  **por valor** é chamado a cada volta de um laço de 2500 `put`s que também
  escreve em `db.state.payloads` via `ref db`. Sob o bit sticky, o bind por
  valor do helper marcava `db` (e, por clone raso, `state` e `payloads`)
  como compartilhados para sempre: cada `put` reclonava os três — 3 clones
  por put, O(N²) porque o clone do map cresce com N. Sob RC, o retain do
  parâmetro do helper morre no fim do frame dele; os donos voltam a 1 e as
  escritas seguintes mutam no lugar.
- **`bench_typed_call_map`** — espelho do bench acima, mas o helper recebe
  `ref` em vez de valor: já não tinha o custo de compartilhamento morto
  (nem no bit sticky, nem no RC) — fica dentro do gate como esperado.
- **`bench_share_mutate`** — pior caso do CoW por construção
  (`let b = a` seguido de mutação): +3,1%, o custo adicional é só o inc/dec
  do bind do `let`, que já é O(1) por vínculo.
- **`bench_conway`/`bench_bubblesort`/`bench_call_ref`** — mutação in-place
  via `ref` sobre grid/array grandes: pagam só o branch de checagem de
  unicidade (agora contador em vez de bit) por escrita — dentro do gate.
- **`bench_call_light`** — chamada O(1) com array de 10k por valor: paga o
  inc/dec de um único bind por chamada — dentro do gate.
- **`bench_call_readonly`/`bench_path_update`** — leitor O(n) e mutação de
  dono único em loop: acima de 5% nesta medição, mas variam mais que os
  quatro benches efetivamente rastreados pelo gate ao longo dos rounds de
  correção da fase 1 (ver nota ² acima) — tratados como ruído, não regressão
  do RC.
- **`bench_map_churn`/`bench_spawn_sum`** — os dois que **seguem acima do
  gate** mesmo após a limpeza do bit sticky morto (ver Interpretação).

### Interpretação

**Onde o RC ganha:** o compartilhamento morto — clone pago por um alias que
já não existe no momento da mutação — deixa de existir por construção.
`bench_value_call_mutate` vai de curva quadrática a flat (~1,5s → ~100ms a
N=2500): o padrão do NoxyDB (helper de validação chamado por valor dentro do
laço de puts) deixa de custar 3 clones por iteração e passa a custar O(1)
clones no laço inteiro.

**Onde fica neutro (dentro do gate ≤~5%):** os quatro benches rastreados ao
longo dos rounds de correção da fase 1 — `bench_share_mutate` (+3,1%) e
`bench_typed_call_map` (−0,7%) fecham dentro do gate; `bench_conway`,
`bench_bubblesort`, `bench_call_ref` e `bench_call_light` também ficam
dentro ou na borda dele. `bench_call_readonly` (+6,6%) e `bench_path_update`
(+7,1%) passam de 5% nesta medição, mas com variância maior que os benches
gate ao longo da fase — não foram tratados como regressão formal pelo
controller.

**Onde paga, sem maquiagem:** `bench_map_churn` (+10,9%) e `bench_spawn_sum`
(+10,4%) **seguem acima do gate de ~5% mesmo depois da limpeza do bit sticky
morto** (Task 8, commit `ae45d8f`) — não é a marcação morta que sobrava, é o
bookkeeping de RC em si: `map_churn` faz muitas inserções/remoções de chave
(inc/dec por elemento a cada operação) e `spawn_sum` paga nos laços quentes
dos workers, onde cada rebind de local escalar (`s = ...`, `i = ...`) passa
pelos funis de RC do OP_SET_LOCAL (ownSlot + Release por iteração). Os args
do bench são primitivos e canal — Retain/Release são no-ops neles, então o
custo é a passagem pelos funis em si, não contagem; o preparo da task
(retain de captura + bind de posse dos parâmetros por valor no frame da
task) roda 4 vezes no bench inteiro e não aparece na conta.
Comparado ao round 4 da Task 7 (map_churn +9,8%, spawn_sum
+14,9%, já com o bookkeeping completo mas antes da limpeza do bit sticky), a
remoção do bit reduziu `spawn_sum` (14,9%→10,4%) e manteve `map_churn` na
mesma faixa (9,8%→10,9%, dentro do ruído) — a limpeza **não** fecha esses
dois deltas. **Aceito e documentado como o preço do RC nesta fase**; as
válvulas para quando isso for revisitado: drops precisos da fase 2
(Perceus-lite, §5) e elisão de pares inc/dec no mesmo bytecode (ambas
nomeadas na spec §8, risco 3), mais um fast path para stores de valores
escalares apontado na investigação da fase 1 (Task 7), fora do texto da
spec.

**O que resta:** fase 1 libera locais de bloco mortos só no fim do frame
(inflação temporária segura, nunca unsound); a fase 1.5 (drops de escopo de
bloco emitidos pelo compilador) e a fase 2 (drops de último uso,
Perceus-lite) atacam o resíduo de compartilhamento morto que sobrevive
dentro de um frame (`do let b = a end; a[i] = x` em laço) — spec §5. A
estrutura persistente (HAMT) para snapshots genuinamente vivos continua fora
de escopo (spec §9): um programa que de fato consome N cópias paga
O(rodadas×n) em qualquer linguagem de semântica de valor.

### Divergência corrigida

Escrita através de `ref` para um nó com exatamente um dono durável agora
acontece in-place e é visível. O teste committado
(`TestRefWriteToUniquelyOwnedNodeMutatesInPlace`,
`internal/vm/rc_uniqueness_test.go`) pina o valor correto para o programa
(lista encadeada, escrita via `setit(ref n, v)` seguida de escrita via
`let u: ref Node = ...; u.valor = 77`): **107** com a unicidade por contagem
de donos, contra **50** no binário pré-chave (bit sticky ligado pelo bind
por valor intermediário, mutação seguinte via `ref` clonava em vez de
mutar — escrita perdida).

A investigação da Task 7 (repros à mão, não parte da suíte) confirmou
adicionalmente que o comportamento antigo era dependente da forma do
vínculo, não um bug de contagem isolado: o próprio merge-base já respondia
107 quando o mesmo alias era escrito só via parâmetro `ref` (forma
canônica, sem a passagem por valor intermediária que liga o bit sticky) —
variante `rv_h1_paramform`, registrada em
`.superpowers/sdd/2026-08-17-cow-rc-uniqueness-fase1/task-7-report.md`, não
na suíte de testes. O 107 é o valor correto pelo contrato CoW 0.4.0 em
qualquer forma (§2, regra 6: mutação através de `ref` é sempre visível).
Nenhum exemplo do corpus muda (130/130) — é dead-share, não um caso
exercitado pelos exemplos existentes.

## develop (c0a89c9) × validação O(1) pela tag `RuntimeType` (PR #31)

**Data:** 2026-08-16/17 · Windows 11 · medições intercaladas (mediana de 5),
mesma máquina e protocolo da seção CoW abaixo.

**Checksums idênticos em todos os benchmarks; corpus de exemplos 130/130 com
saídas iguais** — as duas versões computam os mesmos resultados.

| bench | perfil | develop_ms | o1_ms | delta | veredito |
|---|---|---|---|---|---|
| bench_call_light | chamada O(1), array 10k por valor | 4269 | 122 | **−97,1%** | ✅ |
| bench_typed_call_map¹ | chamada tipada in-module, map 2,5k via ref | 2156 | 123 | **−94,3%** | ✅ |
| bench_share_mutate | compartilha e muta em loop | 1105 | 379 | **−65,7%** | ✅ |
| bench_call_readonly | leitor puro O(n), array 20k | 2815 | 2529 | −10,2% | ✅ |
| bench_call_ref | mutação in-place via ref | 7287 | 6898 | −5,3% | ✅ |
| bench_map_churn | escrita intensa em map | 692 | 665 | −3,9% | ✅ |
| bench_bubblesort | sort in-place via ref | 7258 | 6988 | −3,7% | ✅ |
| bench_conway | grid mutado via ref, 60 gerações | 4374 | 4270 | −2,4% | ✅ |
| bench_path_update | `a[i].x = v` em loop, dono único | 977 | 960 | −1,8% | ✅ |
| bench_spawn_sum | soma paralela via spawn_task | 1395 | 1411 | +1,1% | ✅ ruído |

¹ Benchmark novo, adicionado junto com a mudança: ancora o padrão que motivou
o trabalho (função tipada do mesmo módulo recebendo `ref` para struct com map
grande, chamada em laço de mutação — o formato do put/get do NoxyDB).

### Interpretação

A validação de tipos em runtime varria o contêiner inteiro do argumento a
cada chamada com assinatura estaticamente conhecida — duas vezes (prova +
aplicação), inclusive através de `ref` e em funções que só leem. Isso tornava
O(N²) qualquer laço quente que passasse um map/array grande para função tipada
do mesmo módulo, e era o custo que mascarava o ganho do `ref` no NoxyDB: com
o CoW corrigido, o laço de puts continuava quadrático só pela varredura. Com
a tag `RuntimeType` aceita valendo como prova em O(1), o padrão vira linear —
no repro do NoxyDB (N=4000 puts), de 6.715ms para 157ms.

Onde o ganho aparece: quanto maior o contêiner e mais quente o laço de
chamadas tipadas, maior o corte (`call_light` −97%, `typed_call_map` −94%).
`share_mutate`, o pior caso documentado do CoW, caiu 66% porque também pagava
varredura de validação por cima do clone. O resto da suite fica entre o ruído
e ganhos de poucos por cento — não há caso de regressão.

A varredura completa continua existindo onde é necessária: na primeira
marcação de um contêiner ainda sem tag (é ela que grava a tag) e em
contêineres que nunca ganham tag (ex.: `any`). Conflito de tag continua
rejeitado. Contrato fixado em `internal/vm/runtime_type_validation_test.go`.

## baseline (0.3.0, c429bd7) × CoW (feat/cow-value-semantics)

**Data:** 2026-08-16 · Windows 11 · medições intercaladas (os dois binários
alternados dentro da mesma janela — rodadas sequenciais por rótulo mostraram
drift térmico de até ±10% nesta máquina e foram descartadas para o veredito;
ficam em `results/baseline.md` e `results/cow.md` como registro do harness).

**Checksums idênticos em todos os benchmarks** — as duas versões computam os
mesmos resultados.

### Tabela consolidada (mediana de 5 execuções intercaladas)

| bench | perfil | baseline_ms | cow_ms | delta | critério (spec §5.3) | veredito |
|---|---|---|---|---|---|---|
| bench_call_light | chamada O(1), array 10k por valor | 3412 | 2659 | **−22,1%** | ganho | ✅ |
| bench_call_readonly | leitor puro O(n), array 20k | 2223 | 2020 | **−9,1%** | melhora mensurável | ✅ |
| bench_spawn_sum | soma paralela via spawn_task | 1055 | 988 | −6,3% | neutro/ganho | ✅ |
| bench_conway | grid mutado via ref, 60 gerações | 2862 | 2817 | −1,6% | ≤ ~5% | ✅ |
| bench_map_churn | escrita intensa em map | 444 | 438 | −1,4% | ≤ ~5% | ✅ |
| bench_call_ref | mutação in-place via ref | 4495 | 4594 | +2,2% | neutro | ✅ |
| bench_bubblesort | sort in-place via ref | 4059 | 4173 | +2,8%¹ | ≤ ~5% | ✅ |
| bench_path_update | `a[i].x = v` em loop, dono único | 620 | 649 | +4,7% | ≤ ~5% | ✅ |
| bench_share_mutate | compartilha e muta em loop | 509 | 633 | **+24,3%** | livre, documentada | ✅² |

¹ Bubblesort tem a maior variância da suite (−2% a +7% entre sessões); o
valor reportado é a mediana de 9 execuções intercaladas dedicadas.

² Pior caso do CoW por construção: `let b = a` seguido de mutação paga um
clone O(n) por iteração — exatamente o custo que a semântica promete nesse
padrão. Migração: quem quer compartilhamento usa `ref` (custo zero, ver
`bench_call_ref`).

### Interpretação

**Onde o CoW ganha:** chamadas que só leem o composto deixam de pagar a cópia
rasa ansiosa O(n) por chamada. No caso assintótico (`call_light`: função O(1)
com array de 10k elementos), −22%; no leitor O(n) (`call_readonly`), −9%.

**Onde fica neutro:** mutação in-place via `ref` (`bubblesort`, `call_ref`,
`conway`) e contêineres de dono único (`map_churn`, `path_update`) pagam só o
branch de checagem `Shared` por escrita — dentro do ruído ou ≤5%.

**Onde paga:** o padrão compartilha-e-muta (`share_mutate`, +24%) — o preço
explícito da garantia de independência, com `ref` como válvula de escape.

**Achado colateral:** o ganho das chamadas só-leitura é limitado por um custo
pré-existente e independente do CoW — a validação de tipos em runtime varre
todos os elementos do array a cada chamada tipada
(`internal/vm/runtime_type_validation.go`, caso `TYPE_ARRAY`). Em
`call_light`, ela domina o tempo restante nas duas versões. Validar pela tag
`RuntimeType` em O(1) quando presente é a próxima otimização natural, fora do
escopo desta mudança. *(Resolvido no PR #31 — ver a seção da validação O(1)
no topo deste arquivo: a varredura valia para maps/structs/refs também, não
só arrays, e em `call_light` era 97% do tempo.)*

Os números absolutos desta tabela e da tabela do PR #31 não são comparáveis
entre si (sessões diferentes; o aviso de drift térmico acima vale entre
seções) — dentro de cada seção, a comparação é intercalada e válida.

## Reprodução

```powershell
# suite completa por binário (grava results/<label>.md)
powershell -File benchmarks/run_benchmarks.ps1 -Binary <exe> -Label <label>

# comparação intercalada (grava results/interleaved.md) — preferir esta
powershell -File benchmarks/interleaved_compare.ps1 -Baseline <exe> -Candidate <exe> `
           -BaselineLabel v060 -CandidateLabel v0141 -Runs 9

# corpus de exemplos baseline × candidato
powershell -File benchmarks/compare_examples.ps1 -Baseline <exe> -Candidate <exe>

# referência externa (Noxy × CPython × Lua × Go), com as duas versões juntas
powershell -File benchmarks/cross_runtime/run_cross_runtime.ps1 -Noxy <exe> `
           -NoxyBaseline <exe-antigo> -BaselineLabel v060
```

Em Linux/macOS, os mesmos três runners em bash (mesmo protocolo, mesma
saída; `compare_examples` continua só em PowerShell):

```bash
benchmarks/run_benchmarks.sh --binary <bin> --label <label>
benchmarks/interleaved_compare.sh --baseline <bin> --candidate <bin> \
           --baseline-label v060 --candidate-label v0141 --runs 9
benchmarks/cross_runtime/run_cross_runtime.sh --noxy <bin> \
           --noxy-baseline <bin-antigo> --baseline-label v060
```

Os binários têm de estar em **disco local**: este repo vive em OneDrive e medir
de lá infla os tempos em ~2x (filtro de sync + antivírus no read). Benches que
não rodam nos dois binários são pulados e listados no relatório, não medidos.
