# Changelog

## [0.23.1] - 2026-08-29

Issue #118 — dois atritos do wrapper de extensão (`noxy_dynamodb`): uma
escolha de design da 0.7.0 que ficou obsoleta (`any` só entrava por `let`
anotado) e uma lacuna de implementação (a f-string encerrava o narrowing de
um global).

### Changed
- **`any` entra num slot tipado em qualquer posição** — `let` anotado,
  argumento de assinatura exata e `return` — e o runtime confere o valor ali:
  `expected int, got string`, `expected map[string, any][], got int[]`. Antes
  só o `let` aceitava (`return nativo()` e `f(nativo())` eram `expected T,
  got any`) e o wrapper precisava de um `let` temporário por função; agora
  `func scan(...) -> map[string, any][]` pode `return dynamodb_scan(...)`
  direto. A exceção continua o callable exato: `any` nunca vira
  `func(int) -> int` (nem tipo que o contenha).
- **A guarda do `let` anotado passou a valer para primitivos e structs.**
  `let x: int = nativo()` com `nativo() -> any` devolvendo `"texto"` rodava
  e deixava uma string em `x`; agora é erro de runtime `expected int, got
  string`. Compostos (array/map/chan) já eram checados. Código tipado não
  paga nada: a guarda só é emitida onde o tipo estático é `any` ou
  desconhecido.
- **Builtins centrais não encerram narrowing.** `print`, `to_str`, `length`,
  `fmt`, `keys`, `slice`, `range` e os demais da spec §10, quando não
  sombreados, são nativos puros que nunca rodam código Noxy — o fato
  `m != null` de um global sobrevive a eles, inclusive na entrada de laço.
  Em particular `f"{m['a']} {m['b']}"` (duas chamadas a `to_str` geradas
  pelo parser) e `print(m["a"])` seguido de `print(m["b"])` compilam com `m`
  global. Chamada a função do programa, `call_result`, `spawn_task`,
  `task_await` e `func` bare continuam encerrando.
- **Diagnóstico do fato perdido.** Ler um `T?` cujo fato foi derrubado por
  uma chamada diz o porquê, em vez de sugerir o `if` que já está ali:
  `'m' may be null: it was tested, but 'm' is a global and a call came
  between the test and this use` + `hint: test it again after the call, bind
  it first ('let v = m' before the 'if') and use 'v', or move the code into a
  function` (raiz `ref`, capturada, upvalue ou endereçada tem a frase
  própria).
- A marcação de runtime que falha (`OP_MARK_RUNTIME_VALUE_TYPE`) nomeia os
  dois tipos — `expected chan int, got chan string`, `expected int[4], got
  int[]`, `expected Envelope, got map` — no lugar de `runtime value metadata
  conflicts with static context`.

### Docs
- Spec §2.4 (builtins puros, diagnóstico, código em nível de arquivo), §4.2
  (fronteira `any` checada em toda posição, wrapper sem `let` temporário),
  §9 (f-string é `to_str`), §10.

Follow-ups: atribuição (`x = v`, `s.f = v`, `xs[i] = v`) ainda não emite a
guarda para primitivos vindos de `any` — três sub-caminhos com regras de
`ref` próprias; simplificar o wrapper `noxy_dynamodb.nx` (outro repo).

## [0.23.0] - 2026-08-29

Extensões por processo (tier B) como meio principal de I/O — issue #80
(spec `docs/superpowers/specs/2026-08-29-process-extensions-design.md`,
decisão na #110). Uma fronteira, dois transportes: o wasm continua para
computação pura; I/O, SO, drivers e SDKs vão para um executável por
plataforma que o usuário nunca compila.

### Added
- `kind = "process"` em `noxy_ext.toml`: a extensão é um executável falando
  `noxy-plugin/1` — quadros NXB por stdin/stdout, handshake com binding de
  exports por nome, `id` multiplexado, LOG, CANCEL. Mesmo `use`, mesmo
  wrapper `.nx`, mesmos erros do wasm. Chaves novas: `[binaries]`,
  `call_timeout_ms`, `handshake_timeout_ms`, `restart`, `timeout_ms` por
  export, `concurrency = "concurrent"` (multiplexa e permite handles),
  `capabilities` declarativa (só exibida no `--get`).
- Ciclo de vida: o processo sobe no primeiro export chamado; prazo por
  chamada com cancelamento cooperativo (`extension 'x' timed out: …`) —
  só quem ignora o CANCEL é morto (`trapped` + poison); processo morto =
  `trapped: process exited (status N)`, `restart = true` só com
  `stateless`; todo backend é fechado na saída da VM (`runFile`, REPL,
  `sys_exit`), com `PDEATHSIG` no Linux e job object no Windows.
- SDK Go `github.com/estevaofon/noxy/sdk/noxyplugin` (módulo aninhado em
  `sdk/noxyplugin`, sem dependência de `noxy-vm`): `New/Handle/Serve/Main`,
  `Func0..Func5`, `Args`, `Logf`; protege o stdout do protocolo, recupera
  panics em `failed`, cancela o `context` no CANCEL e no EOF.
- `noxy --get` para extensões por processo: sem `@versão`, resolve a tag
  semver mais nova; baixa `checksums.txt` e **só** o asset da plataforma
  para `noxy_libs/<pkg>/bin/`, verifica o sha256 e grava em `noxy.sum` os
  hashes de **todos** os assets publicados (lockfile portável entre SOs).
  Sem asset para a plataforma = erro no `--get`, nunca em runtime.
- Benchmarks `BenchmarkProcess*` em `internal/ext`; números em
  `docs/EXTENSIONS.md`.

### Changed
- `noxy --get` substitui o diretório do pacote a cada execução (clone
  fresco): o caminho "existe → git pull" não atualizava nada sem `.git`.
- `noxy --get` faz clone/checkout com `core.autocrlf=false` e `core.eol=lf`:
  todo pacote (não só extensões) chega com os bytes do repositório em qualquer
  SO — o hash do `noxy_ext.toml` no `noxy.sum` passa a ser portável entre
  Windows e Linux (antes seguia o `autocrlf` da máquina).
- `noxy_ext.toml` presente mas inválido faz o `--get` falhar (antes só
  pulava o registro no `noxy.sum`).
- A verificação do `noxy.sum` cobre manifesto + o artefato que o backend
  executa (`.wasm` ou `bin/<asset>`).

### Deprecated
- `sys_load_plugin` (JSON por linha): aviso no stderr na primeira chamada;
  remoção na v0.25.0 junto com `internal/plugin` e
  `compiler.PluginNativeNames`. Migração: `kind = "process"` + SDK
  (`docs/EXTENSIONS.md`).

## [0.22.0] - 2026-08-28

A distância entre o pitch e o produto fechada nos dois lugares apontados na
issue #105: **`null` no sistema de tipos** e **ergonomia de erros** — mais a
solidez de nomes da #47. Struct e `ref` nus nunca são `null`; `T?` é a única
grafia de nulidade e o compilador estreita (`if x != null then`) no modelo de
Kotlin/Dart. Um `Result<T>` genérico único em `errors`, `call_result` tipado
pelo callee e devolvendo instância real, `try expr` propagando a falha.
Parâmetro duplicado, colisão no escopo global e global inexistente viram erro
de compilação. Spec §2.4 (nova), §3, §5, §7 reescritas; plano em
`docs/superpowers/plans/2026-08-28-issue-105-null-safety-result.md`.

### Changed (BREAKING) — nulidade (issue #105 item 1, #84 ampliada)

| Antes (0.21) | Agora (0.22) |
|---|---|
| `let p: Point` valia `null`; `let r: ref int` idem | erro `needs an initializer … or declare it as 'Point?'`; `let p: Point?` / `let r: ref int?` valem `null` |
| `let p: Point = null`, `f(null)` com `n: ref Node`, `Node(1, null)` com `next: ref Node` | erro de compilação com hint `declare it as 'Point?' to allow null` / `declare the parameter as 'ref Node?' to accept null` |
| `p.x`, `*r`, `r.f`, `xs[i]` sobre valor possivelmente nulo explodiam em runtime | erro de compilação `'p' may be null; test it first` + hint; dentro de `if p != null then … end` o tipo é `Point` |
| `null` vindo por `any` entrava em struct/ref/array | runtime `expected Point, got null` + hint; entra só em `T?` |
| `json_loads` punha `null` em qualquer campo struct/ref | só em campo/elemento `T?` |
| `net.socket_set() -> Socket[64]`, `SelectResult.read: Socket[64]` | `Socket?[64]` na entrada (slot vazio é `null`); `SelectResult.read/write/error: Socket[]` com exatamente `*_count` sockets |

Gramática: `?` é o pós-fixo mais externo — `ref Node?` é referência anulável,
`ref (Node?)` referência a slot anulável, `Node?[]` array de anuláveis,
`(func(int) -> int)?` para callables. `T??`, `any?`, `void?` são erros.
Narrowing vale para expressões estáveis (`x`, `*r`, `a.b.c`) a partir de
`!= null`/`== null` (com early return), `&&`/`||`, `while e != null do` e
`if r.ok then` (estreita `r.value`); acaba em atribuição ao prefixo, em
chamada quando a raiz é `ref`/global/upvalue/capturada/endereçada, em escrita
através de `ref`, e na entrada de laço que atribui a raiz. `T` genérico pode
bindar `X?` (nunca `ref`).

### Changed (BREAKING) — erros como dado (issue #105 item 2, #39 item 2)

| Antes (0.21) | Agora (0.22) |
|---|---|
| `errors.CallResult {ok, value: any, failure}` (um map em runtime) | `errors.Result<T> {ok, value: T?, failure: Failure}` — instância real; `fmt("%T")` dá o nome da instância, compara igual a `Result(true, v, empty_failure())` |
| `let r: CallResult = call_result(f, x)` | `let r = call_result(f, x)` é `Result<R>` com `R` o retorno estático de `f` (`Result<any>` se desconhecido/void); exige `use errors select *` em escopo |
| `to_int_result -> IntResult`, `to_float_result -> FloatResult` | `-> Result<int>` / `Result<float>`; `IntResult`/`FloatResult` removidos |
| `io.close_result -> IOCloseResult`, `write_result`/`write_bytes_result -> IOWriteResult` | `-> Result<bool>` / `Result<int>` (bytes escritos); `IOWriteResult`/`IOCloseResult` viram shape interno dos nativos |
| `json.dumps_result -> EncodeResult` | `-> Result<string>` |
| `r.error` / `r.data` / `r.success` / `r.bytes_written` | `r.failure.message` / `r.value` / `r.ok` / `r.value` |
| módulo `result` (deprecado desde 0.7.2) | removido |
| — | `try expr`: `expr: Result<U>` vale `U` ou a função corrente (que devolve `Result<V>`) retorna a mesma `Failure`; `try` é palavra reservada |
| — | construtoras `Ok(v)`, `Err(msg)`, `Fail(failure)`, `empty_failure()` — capitalizadas para não colidir com `let ok: bool` via `select *` |

### Changed (BREAKING) — solidez de nomes (issue #47)

- `func f(x: int, x: int)` → `duplicate parameter 'x' in function 'f'`.
- `let`, `func`, `struct` e nomes importados dividem um namespace: `let x`
  sobre `func x`, `struct P` sobre `func P`, `let range` sobre `use m select
  range` → `'x' redeclared in this scope (previous declaration as function at
  line N)`. No REPL, redefinir função por função entre linhas continua
  permitido.
- Global inexistente é erro de compilação mesmo atrás de branch que nunca
  roda: `undefined global 'typo'` + hint; `x = 1` sem `let` →
  `cannot assign to undeclared name 'x'`. O embutidor semeia os nativos
  (`vm.GlobalNames()` → `compiler.SetKnownGlobals`); `compiler.New()` puro
  continua permissivo. `bst_owned.nx` e `test_fmt_types.nx` chamavam
  `exit(1)` sem importar `sys` — o check pegou.

### Added

- `ast.NullableType`, token `?`, helpers `isNullable`/`nonNull`/`nullable`/
  `asRefType` (toda pergunta de modo "é ref?" desembrulha `?`).
- `internal/compiler/narrowing.go`: fatos por chave estável, `conditionFacts`,
  invalidação por atribuição/chamada/laço, `blockTerminates`.
- `OP_SWAP` (lowering de `try`); `ast.TryExpression`; `ast.Inspect` (walker).
- `compiler.SetKnownGlobals`, `compiler.PluginNativeNames`, `vm.GlobalNames`,
  `compiler.GlobalDecl`/`SetSessionBindings`/`ProgramBindings` (REPL).
- Re-export transitivo por `select *` resolve o nome no módulo que o declara
  (tipo e template certos) em vez de apagar o tipo já conhecido.
- Exemplos: `try_config.nx`; `result_pattern.nx` na forma tipada; fixtures
  `type_errors/nullable_*.nx`, `try_*.nx`.

### Fixed

- Mensagem de runtime para `null` em slot não-anulável nomeia o tipo
  (`expected Point, got null` + hint) em vez de "runtime value metadata
  conflicts with static context".
- Um `let` global homônimo a um `use` não sombreia mais o namespace em
  silêncio (era aceito e mudava o significado de `calc.x`).

### Migração

1. `let p: P` sem valor → dê o valor ou `let p: P?`.
2. Campo/param/retorno que recebe `null` → `T?` / `ref T?` (o hint diz qual).
3. Leitura de `T?` → `if x != null then … end` (ou `let n = a.b` e teste).
4. `let r: CallResult = call_result(…)` → `let r = call_result(…)`;
   `IntResult`/`IOWriteResult`/`EncodeResult` → `Result<T>`; `r.error` →
   `r.failure.message`; `r.data`/`r.bytes_written` → `r.value`.
5. `if r.ok == false then return r end` em cadeia → `try`.
6. `ok`/`err` como nome de função própria em módulo importado por `select *`
   colidem com nada (as construtoras são `Ok`/`Err`); `try` como identificador
   precisa ser renomeado.

## [0.21.0] - 2026-08-28

Campos de struct por **índice**, dos dois lados. A instância guarda os campos
em `Slots` na ordem da declaração (issue #86, PR #95) e o compilador resolve
`p.x` para o índice do slot quando conhece o struct da base (issue #96); o
empréstimo `ref p.x` resolve o índice uma vez, na criação, em vez de um hash
por nível a cada acesso (issue #93 b). O teto de 256 níveis no caminho de um
empréstimo, que matava uma BST por posse degenerada, sai (issue #93 a). Tudo
com semântica, mensagens e contagem RC idênticas; `-race` no pacote `vm`
inteiro no CI.

### Changed (BREAKING) — só para quem embute a VM em Go (issue #86)

| Antes (0.20) | Agora (0.21) |
|---|---|
| `ObjInstance.Fields map[string]Value` | `ObjInstance.Slots []Value`, na ordem de `ObjStruct.Fields`; `Get/Set/MustSet/Field/Range/Snapshot` por nome continuam, `FieldIndex(name)` em `ObjStruct` |
| `NewInstanceWith(def, map)` | continua; novo `NewInstanceAdopting(def, slots)` (move: o chamador já reteve) |

Nada muda na linguagem. O `fatal error: concurrent map read and map write` em
`b.inner.value = …` concorrente some junto com o map por instância; a spec
nunca prometeu atomicidade para escritas concorrentes no mesmo campo, e o
teste (`!race`) fixa só que o runtime não cai.

### Added

- `OP_GET_FIELD` / `OP_SET_FIELD` / `OP_GET_FIELD_MUT` `[idx u8][nome u16]`
  (issue #96), emitidos quando o tipo estático da base é um struct do programa
  (inclusive instâncias genéricas e structs de escopo local). Base `any`,
  módulos e `ref p.x` ficam nos opcodes por nome; bytecode com base `any`
  inalterado. O nome viaja no operando porque **`json_loads` monta definições
  de struct em ordem alfabética** e essas instâncias entram em contêineres
  tipados — o caminho rápido confere `Fields[idx] == nome` e cai no funil por
  nome quando não bate. `OP_SET_FIELD` é statement (sem `OP_POP`).
- `ObjRef.Slot` (issue #93 b): `OP_REF_PROPERTY` resolve o índice do campo na
  criação; `descend`/`referenceStorageMode`/setter usam o slot sob a mesma
  guarda. Zero é seguro para `ObjRef` montado à mão.
- Benchmarks: `bench_struct_records.nx` (5 campos, 60k construções — o bench
  da #40 que nunca tinha chegado a `develop`), `bench_bst_owned.nx` /
  `bench_bst_ref.nx` (as fixtures da #93, 20k chaves); `bench_borrow_path.nx`
  ganha `CHECKSUM:` (o guard de equivalência o pulava).
- Specs `2026-08-28-vm-perf-issue-96-struct-field-index-design.md` e
  `2026-08-28-vm-perf-issue-93b-borrow-path-design.md` (a segunda registra por
  que "fast path quando `Owners == 1`" é inseguro sem validar a cadeia, e o
  desenho da cache por época que fica como follow-up).

### Fixed

- **#93 (a)** — `maxBorrowPathDepth = 256` limitava a cadeia de `Base` de um
  empréstimo com a justificativa de que "um caminho real tem a profundidade do
  lvalue escrito no fonte", falsa desde a 0.20: `insert(ref node.esquerda, v)`
  recursivo e `r = ref r.prox` num laço encadeiam um nível por passo, e uma BST
  por posse degenerada com ~256 nós morria com `reference path too deep`. O
  teto sai (a cadeia não cicla por construção); `maxRefHopDepth` fica só em
  `derefPlace`.
- **#87** — `noxy_examples/KandR_in_noxy/`: sete arquivos não compilavam desde
  a migração para `ref` explícito (`let i: int = pos`, `length(v)` com
  `v: ref int[]`, `append(s.items, …)`); só `ch02_mismatch.nx` e
  `ch02_redeclare.nx`, exemplos de erro intencionais, seguem não compilando.
- **#53 item 4** — `test_crypto_debug.nx` imprimia `FAIL` em toda execução e
  saía com 0: o `hex_to_bytes` do exemplo passava por `strings.from_char_code`,
  que codifica valores ≥ 0x80 em dois bytes UTF-8. `to_bytes(int[])` fecha o
  round-trip, e cada FAIL termina com `sys.exit(1)` para o runner enxergar.

### Performance

`develop` (0.20.0 + #95) × 0.21.0, intercalado, mediana de 9, mesma máquina:

| bench | antes | depois | delta |
|---|---|---|---|
| `bench_path_update` (`cells[i].hits = …`) | 238,9 ms | 163,0 ms | **−31,8 %** |
| `bench_bst_owned` (BST por posse, 20k) | 816,7 | 575,8 | **−29,5 %** |
| `bench_borrow_path` (`ref root.b.c.xs[0]`) | 594,4 | 531,8 | **−10,5 %** |
| `bench_bst_ref` | 227,0 | 212,0 | −6,6 % |
| `bench_struct_records` | 134,7 | 126,5 | −6,1 % |
| demais (bubblesort, conway, call_*, map_churn, spawn_sum, generic_vs_hand) | | | −1,7 … +1,5 % (ruído) |

BST por posse com 50k chaves aleatórias: 2,35 s → 1,62 s. Perfis: em
`bench_path_update` somem `FieldIndex`/`mapaccess2_faststr`/`aeshashbody`
(15 %) e `Retain`/`Release` de `int` (10 %); na BST por posse o hashing por
nível cai de 16 % para 1 % e o `reflect` de `validateReferencedValue` sai do
caminho de todo acesso por ref. Corpus 180/180; diff de saída antes × depois
0 divergentes. Detalhes em `benchmarks/RESULTS.md`.

### Docs

- Spec §2.2 regra 6 e §5 (issue #92): um campo `ref T` é uma **aresta
  compartilhada** — a cópia de um struct é independente através dos campos de
  valor e continua apontando para o mesmo referente através dos campos `ref`;
  os dois idiomas (posse e `ref`) ficam documentados lado a lado; exemplo
  novo `noxy_examples/bst_owned.nx` (BST por posse) ao lado de `bst.nx`.

### Conhecido, não consertado

- BST por posse **degenerada** continua O(profundidade²): 1000 chaves
  ordenadas passam (antes morriam) mas levam ~12 s, contra ~0,4 s com
  `ref TreeNode` nos filhos. O que resta é a caminhada do modelo de lugar
  (0.20), agora sem hash; matar o quadrático exige a cache por época da spec do
  #93 b (§6) — follow-up de arquitetura.
- #100 — o cache do construtor de struct do PR #70 nunca chegou a `develop`:
  `validateStructConstructorArguments` é 46 % de `bench_struct_records`.
- #53 item 1(a) — `json_loads` em struct dentro de array compartilhado ainda
  vaza.
## [0.20.0] - 2026-08-25

Uma referência para dentro de um contêiner denota um **LUGAR**, não um objeto
(issue #83). `ref a[i]` / `ref p.x` congelava o `*ObjArray`/`*ObjInstance` do
instante da criação, e a partir daí a referência só estava certa enquanto
ninguém mexesse no caminho: quem copiasse a raiz depois recebia a escrita feita
através do empréstimo (vazamento), e quando o copy-on-write bifurcava o caminho
o objeto congelado ficava órfão e a escrita sumia. Agora a base é compilada como
referência ao lugar do pai, recursivamente até um ref de célula
(`OP_REF_LOCAL`/`_UPVALUE`/`_GLOBAL`), que o CoW não move; na escrita o caminho
inteiro é re-resolvido, unicizando e gravando o clone de volta em cada nível.
É a mesma caminhada que a família `*_MUT` já fazia para `a[i].x = v` — o que
muda é o instante em que ela roda. Spec: §2.3 R10.

### Changed (BREAKING) — o empréstimo é um lugar, resolvido a cada acesso (issue #83)

| Antes (0.19) | Agora (0.20) |
|---|---|
| `let c = a` *depois* de `ref a[0]`: a escrita através do empréstimo vazava para `c` | `c` fica isolada — semântica de valor vale independente de quando a cópia foi feita |
| CoW bifurca o caminho: a escrita através do empréstimo sumia num objeto órfão | a escrita chega no lugar atual (o clone toma o lugar do original dentro do pai, até a raiz) |
| raiz reatribuída (`arr = [7,7,7]` com `ref arr[0]` vivo): escrita sumia | a escrita chega no lugar atual |
| entrada de map apagada (`delete(ref m, "a")` com `ref m["a"]` vivo): a escrita **ressuscitava** a chave em silêncio (`mapping.Set` insere) | erro `reference target no longer exists` — e o mesmo quando o lugar apagado está no **meio** do caminho, em vez do sintoma um nível adiante (`Target is not an instance`) |
| `ref a[0].x == ref b[0].x` com `b = a` ainda compartilhado: `true` (comparava `Container.Obj`, um retrato do instante da criação) | `false` — a identidade de um empréstimo é o **caminho** |
| `addr(ref a[0].x)` → `<index 0>` (omitia o contêiner; não era identidade nenhuma) | `<prop x of <index 0 of <global a>>>` |

`ObjRef.Base` fica zerado para os `ObjRef` construídos fora do compilador
(natives, JSON, bytecode de teste), que caem no comportamento antigo via
`Container`.

### Added

- Spec §2.3 **R10** ("A reference into a container denotes a place"), com linha
  própria na tabela de diagnósticos.
- `benchmarks/bench_borrow_path.nx` — não havia benchmark para `ref` **dentro**
  de contêiner (todos usavam `ref <variável>`, que é outro mecanismo), e foi
  por isso que uma regressão de ~2x passou batida pela suíte inteira. Raso,
  fundo e só-escrita.
- Spec `docs/superpowers/specs/2026-08-25-issue-83-exclusive-access-design.md`
  (marcada como superada: o desenho estático que ela propunha foi descartado
  pelo repro G, e o caminho path-based que ela havia descartado é o que
  consertou) e plano `docs/superpowers/plans/2026-08-25-issue-83-handoff.md`.

### Fixed

- Os **sete** repros do #83 — inclusive o G, o alias num ancestral do
  contêiner, que nenhuma checagem centrada no contêiner via (`Retain` é por
  objeto).
- Caminho que **atravessa** um campo `ref T` (`setit(ref n1.next.data)`, o
  idioma de lista ligada) morria com `slot 'next' already holds a reference`:
  o compilador agora vê o nível `ref` antes de emitir e compila aquele nível
  como valor. Fecha também `ref n1.next.xs[0]`, o mesmo dentro de um parâmetro
  `ref Node`, e `ref refs[0].data` sobre `(ref Node)[]`.
- Site de escrita tipado `any` (`func setx(p: any) -> void  p.x = 99 end`
  chamada com `setx(ref arr[0])`) vazava: `OP_SET_PROPERTY` e
  `OP_SET_PROPERTY_DEREF` resolviam a referência em modo de **leitura** e
  mutavam o objeto compartilhado no lugar. Alvo de escrita resolve em modo de
  escrita.
- `json_loads` através de referência vazava nos **três** caminhos de população
  — inclusive com `ref` a um local simples, sem empréstimo nenhum: `populateRef`
  e `prepareJSONMutation`/`jsonReferenceStorage` mutavam o alvo no lugar sem
  unicizar. Bug pré-existente de semântica de valor, fechado junto; era o
  último site de escrita que resolvia referência sem escolher modo.

### Performance

- Empréstimo: **-68% de alocações, -11% de tempo** (`BenchmarkDeepBorrow`,
  6.232.764 → 5.549.561 ns/op, 58.443 → 18.443 allocs/op). A re-resolução do
  caminho cobrou caro num caminho que nenhum benchmark media (`ref root.b.c.xs[0]`
  chegou a +198%); o profile apontou `referenceStorageMode` sozinho em 68,8%
  das alocações. `referenceSetter` deixa de ser closure — vira struct de valor
  que descreve onde gravar, não escapa, não aloca (era alocado por nível e por
  acesso, inclusive na **leitura**, que descarta o setter sem chamá-lo) — e
  `borrowContainer` coleta a cadeia num array de pilha e desce num laço em vez
  de recorrer pelo despacho inteiro a cada nível. Duas alternativas medidas e
  descartadas ficam registradas em comentário no código.
- A escrita através de empréstimo passa a custar o que a atribuição direta
  equivalente já custava, e a unicização ansiosa da criação deixa de acontecer.

### Removed

- R11/R12 e `own ref` (protótipos desta mesma sessão, nunca lançados): as duas
  regras existiam para contornar o bug que agora está consertado na raiz, e
  mantê-las passaria a significar avisar sobre código **correto** — medido nos
  próprios repros da spec, e custando uma migração obrigatória de 42 parâmetros
  no corpus. A superfície da linguagem volta ao que era na 0.19.0.

### Conhecido, não consertado

- Escritas concorrentes através de empréstimos perdem atualizações quando outra
  routine lê a raiz. O controle — escrita **direta**, sem nenhum `ref` — perde
  nos dois binários: a corrida é do CoW sob concorrência e é anterior a isto
  (#86 é a ponta visível). O que mudou é que empréstimos perderam uma imunidade
  que **era o bug** (escreviam direto no objeto congelado, sem passar pelo CoW)
  e agora herdam o problema que já existia.
- #87 — `ch06_keycount_ref.nx` não compila desde o #82.

## [0.19.0] - 2026-08-25

`ref` explícito (issue #82): uma referência nunca é criada nem lida sem
`ref` ou `*` no código. `*r` é a única leitura e escrita do valor apontado;
`ref x` é obrigatório em todo call site com parâmetro `ref T` — funções,
valores `func`, construtores, generics e builtins (`append(ref xs, v)`);
`.`/`[]` atravessam a referência e são o único atalho. Somem da linguagem o
auto-dereference, as exceções 1 e 2 de `==`/`=`, a conversão contextual em
chamadas exatas e a "forwarding form" (`ref r` com `r` já `ref`). A VM já
era explícita (aritmética, `==`, `print`, `length` nunca liam um ref); a
mudança é quase toda no compilador. Spec:
`docs/superpowers/specs/2026-08-24-explicit-ref-design.md`.

### Changed (BREAKING) — `ref` explícito: `*r` para ler, `ref x` em todo call site (issue #82)

| Antes (0.18) | Agora (0.19) |
|---|---|
| `let n: int = r`, `r + 1`, `-r`, `if rb`, `xs[ri]`, `return r`, `g(r)` liam o ref | erro `… expected T, got ref T` / `operand of '+' cannot be ref int: a ref is never read implicitly` — hint `use '*r'` |
| `for x in r` com `r: ref T[]` compilava e iterava **zero vezes** | erro `cannot iterate over ref T[]` — hint `for x in *r` |
| `*r = s` lia `s`; `*r = ref z` lia `z` (sem efeito) | `*r = *s`; `*r = ref z` é erro — hint `r = ref z` (rebind) ou `*r = z` |
| `f(x)` com parâmetro `ref T` emprestava `x` (só em assinatura exata) | erro `argument 1 to 'f': expected ref T, got T` — hint `use 'ref x'`; igual para `func` bare, construtor, generic |
| `append(xs, v)`, `pop(xs)`, `delete(m, k)`, `json_loads(s, alvo)` emprestavam | `append(ref xs, v)`, `pop(ref xs)`, `delete(ref m, k)`, `json_loads(s, ref alvo)` |
| `ref r` com `r: ref T` encaminhava (`ref ref T` proibido por construção) | erro `'r' is already a reference` — hint `pass 'r' directly`; `let q: ref ref int` é `SyntaxError` |
| `print(r)`, `to_str(r)`, `f"{r}"` mostravam o valor | mostram `<ref …>`; `print(*r)` mostra o valor |
| `*x` com `x` de tipo desconhecido não-ref passava adiante | erro de runtime `cannot dereference int` |
| `*x` com `x: any` era erro de compilação (`cannot dereference non-reference value of type any`) | compila — `any` guarda ref (R2) — e é checado em runtime: lê o valor apontado, ou erra `cannot dereference int` |
| `*r` com `r == null` devolvia `null` em silêncio | erro de runtime `cannot dereference null reference` (na escrita, `cannot write through a null reference`), inclusive pelos opcodes fundidos de índice via ref |
| `guarda(r)` com parâmetro `v: any` falhava em runtime (`expected any, got ref`) | roda: um parâmetro `any` recebe a referência **como valor**, igual a `print`/`to_str` (R2) |
| `for v in a` com `a: any` guardando um ref iterava **zero vezes** em silêncio | erro de runtime `cannot iterate over a ref: a ref is never read implicitly` — hint `use '*r'` |
| `ref a.f` via `any` sobre campo `ref T` encaminhava | erro de runtime `slot 'f' already holds a reference` |
| `length(r)`, `keys(r)`, `slice(r, …)`, `contains(r, …)`, `has_key(r, …)` com `r` ref liam o valor | erro de compilação `argument 1 to 'length': expected a value, got ref int[]` — hint `use '*r'`; em runtime via `any`, `length: argument 1 expected a value, got ref`. Só o **argumento 1** (a coleção) é checado: `contains(ys, r)` sobre `(ref int)[]` e `has_key(m, rk)` continuam valendo (busca por identidade) |
| `json_dumps(r)`, `base64_encode(r)`, `fmt("%d", r)` e companhia codificavam o texto `<ref …>` em silêncio | erro de compilação `argument N to 'json_dumps': expected a value, got ref int[]` — hint `use '*r'`; em runtime via `any`, `json_dumps: argument 1 expected a value, got ref`. Valem para **todos** os argumentos de `json_dumps`, `json_dumps_result`, `json_parse`, `base64_encode`, `base64_decode`, `hex`, `hex_encode`, `hex_decode`, `base62_encode`, `base62_decode`, `to_bytes`, `fmt`, `crypto_pbkdf2_sha256`, `crypto_aes256_gcm_encrypt` e `crypto_aes256_gcm_decrypt` |

Inalterados: `r == s` (identidade), `r == null`, `r == 1` (erro, hint `*r`),
`r = 50` (erro, hint `*r = 50`), `x.next = n` (erro), `r.f`/`r[i]`,
`f(r)`/`f(a.next)`/`f(null)`, `let v = r` → `v: ref T`, generics (`T` não
liga a `ref X`).

`json_loads(s, x)` com `x: any` compila mas falha em runtime a menos que
se escreva `ref x` (R5: `any` é validado em runtime). O caminho de
encaminhamento de uma referência já existente deixa de reemitir
`OP_MARK_REF_TARGET_TYPE` (a referência foi marcada onde foi criada).

### Added

- Diagnósticos com hint para cada posição de R1/R2/R5/R6 (spec §2.3
  "Diagnostics"); fixtures em `noxy_examples/type_errors/ref_*.nx`.
- Spec §2.3 reescrita como R1–R9; R9 documenta o tempo de vida de uma local
  referenciada (fecha #81); `docs/REF_SEMANTICS.md` reescrito.
- Spec `docs/superpowers/specs/2026-08-24-explicit-ref-design.md` e plano
  `docs/superpowers/plans/2026-08-24-explicit-ref.md`.

### Fixed

- `for x in r` com `r: ref T[]` era um laço vazio silencioso (`OP_LEN`
  devolve 0 para `VAL_REF`); agora é erro de compilação com hint — e, quando
  o ref chega pela fronteira dinâmica (`a: any`), `OP_LEN` erra em runtime
  em vez de devolver 0.
- `*r` com `r == null` seguia adiante com um `null` silencioso onde o tipo
  estático prometia um `T`; agora erra, e os caminhos fundidos
  (`OP_GET_REF_LOCAL_INDEX_ARRAY`, `OP_SET_REF_LOCAL_INDEX_ARRAY_NORC`) dão
  a mesma mensagem do caminho genérico.
- Um parâmetro `any` recusava em runtime a referência que o compilador
  deixava passar (R2 diz que `any` a recebe como valor) — `validateParameterModes`
  só barra `ref` em parâmetro de tipo concreto.
- `docs/index.html` e `docs/HTTP_SERVER.md` ainda descreviam a conversão
  automática de 0.18 nos exemplos de `append`/`pop` e de chamada com
  parâmetro `ref` (todo trecho Noxy do site foi extraído e executado).

### Removed

- Auto-dereference, "Type-Based Assignment" e suas exceções 1 e 2,
  conversão contextual (§4.2), forwarding form — da spec e do compilador
  (`compileReferenceArgumentValue` só cria; `compileRefArgument` é o único
  caminho de argumento `ref`). Os opcodes `OP_CONTEXT_REF_PROPERTY`/`_INDEX`
  deixam de ser emitidos (os cases na VM ficam).
- O diagnóstico específico `reference slot at index N holds a non-reference
  value` deixa de ser alcançável a partir de código com base tipada (só via
  backdoor de teste; o invariante do slot é garantido nos escritores).

### Follow-ups registrados

- #83 — `ref` para dentro de container tomado antes de uma cópia vaza
  escrita para a cópia (edge da §2.2).
- #84 — `ref T` não-nulo por padrão / `ref T?`.

## [0.18.0] - 2026-08-23

Mecanismo experimental de extensões WASM (issue #78, fase M1): um pacote
nativo compilado para `wasm32` pode ser carregado dentro de um processo
Noxy via `use`, e suas exports viram natives Noxy comuns, chamáveis como
qualquer função da stdlib. Runtime embutido via wazero; `go.mod` sobe para
`go 1.25` (piso do wazero v1.12.0). Sem capabilities nesta fase — toda
extensão é função pura dos argumentos declarados.

### Added

- **Mecanismo de extensões WASM, fase M1** (issue #78): pacote novo
  `internal/ext` — codec NXB (troca de valores por cópia entre host e
  guest: tags null, bool, int, float, string, bytes, array, map, struct;
  funções, channels, tasks e `ref` não cruzam a fronteira), parser de
  `noxy_ext.toml` (nome, `abi`, `min_noxy`, `concurrency`, `memory_max_mb`,
  `capabilities`, `[[export]]` com params/returns tipados; chave
  desconhecida é erro de carga), loader sobre wazero com gate de imports
  (só `noxy:host/v1` é permitido; qualquer outro import recusa a carga —
  WASI não é fornecido, o que também é o modelo de permissão: uma extensão
  sem capabilities é função pura dos argumentos), modos `concurrency =
  "single"` (uma instância, chamadas serializadas — obrigatório para
  extensões com estado por handle) e `"stateless"` (pool de instâncias,
  chamadas concorrentes), poisoning de instância em trap (out-of-bounds,
  unreachable, estouro de memória, timeout de chamada — 30s padrão por
  `nx_call`, cancelamento de contexto do lado do host) e cache de
  compilação persistente em `<user cache dir>/noxy/wazero` (best-effort:
  falha ao criar o diretório não impede a carga).
  - Um pacote com `noxy_ext.toml` é carregado automaticamente ao ser
    importado com `use`; cada `[[export]]` vira uma native contextual
    assinada (arity, tipo de cada parâmetro, tipo de retorno). Antes de
    registrar qualquer export, o loader confere todos contra os globais já
    existentes — nome de export que colida com uma native da stdlib ou com
    export de outra extensão falha a carga inteira, atomicamente, em vez
    de sombrear em silêncio.
  - `noxy --get` grava o sha256 do manifesto e do `.wasm` em `noxy.sum`
    (mínimo viável: uma linha por artefato). A verificação só se aplica a
    pacotes sob `noxy_libs`; havendo entrada, o manifesto é verificado
    primeiro e só depois o wasm (o manifesto escolhe qual arquivo `.wasm` é
    o verificado, então precisa ser verificado ele mesmo antes). Pacote sem
    entrada correspondente, ou extensão carregada fora de `noxy_libs` em
    desenvolvimento, roda sem verificação (trust-on-first-use); spec de
    integridade completa fica para depois.
  - Sem capabilities nesta fase: `capabilities` não-vazio no manifesto é
    erro de carga, não aviso.
  - Benchmarks em `internal/ext` (guest de referência em Rust,
    `internal/ext/testdata/rustguest/`, além de um guest Go de teste):
    ida-e-volta de 1 KB de `bytes` na fronteira ~3.5–4.0 µs; `sha256` de
    1 MB no guest ~7–8x o tempo do `crypto/sha256` nativo (asm/SHA-NI) para
    a mesma entrada; delta de tamanho de binário ao embutir o wazero ~4.0
    MiB; carga de módulo ~29 ms com cache de compilação quente, ~565 ms
    frio (sem cache).
  - `go.mod` passa a exigir `go 1.25` (piso do
    `github.com/tetratelabs/wazero` v1.12.0).

## [0.17.1] - 2026-08-23

Patch de checagem estática (issue #75, PR #76): operandos aritméticos e de
comparação de tipo incompatível viram erro de compilação, não de runtime.
Nenhuma sintaxe nova, nenhum opcode novo, bytecode de programa válido
idêntico ao de 0.17.0 (só programas que já falhariam em runtime deixam de
compilar). Corpus 179/179.

### Fixed

- **Checagem estática de operandos aritméticos e de comparação** (issue
  #75): `int + string`, `bool * int`, `string - string`, `float % int`,
  `bytes < bytes`, `string < int`... passam a ser erro de **compilação**,
  com o texto do runtime mais os dois tipos (`[line N] operands must be
  numbers or strings or bytes, got int and string`; `operands must be
  numbers, got bool and int`; `operands for % must be integers, got float
  and int`; `operands must be numbers or strings, got bytes and bytes`) —
  mesmo padrão do #56 para `!`, `~` e bitwise. Antes o compilador emitia o
  opcode genérico e só a VM reclamava; valia para anotado (`let x: int`) e
  inferido (`let x = 10`, #41). `any`/tipo desconhecido continuam no caminho
  genérico com checagem em runtime; `ref T` é lido como `T`; `==`/`!=` têm
  regra própria e não mudam. Zero custo em runtime: a checagem lê os tipos
  que o compilador já calcula para escolher `OP_ADD_INT`/`OP_ADD_FLOAT` —
  bytecode de programa válido idêntico (hash dos chunks de 354 arquivos do
  corpus antes × depois sem diferença; o único que muda é
  `noxy_examples/error_type.nx`, erro intencional que agora falha em
  compile-time). Como toda checagem estática, também rejeita código morto
  (`if false then print(1 + "a") end`). Spec §8 ganha a subseção *Static
  operand checks*.

## [0.17.0] - 2026-08-23

Inferência local de tipo em `let` (issue #41, PR #73): `let x = expr` sem
anotação, com o tipo do binding inferido do tipo estático do RHS — type-stable
como sempre. Minor porque é sintaxe nova aceita; superset puro de 0.16.0 (todo
código anotado continua válido, com o mesmo bytecode). Junto, os builtins
centrais ganharam tipo de retorno estático e `~` passou a aceitar `bytes` no
checador (a VM já aceitava). Corpus 178/178.

### Added

- **Inferência local de tipo em `let`** (issue #41): `let x = expr` sem
  anotação é aceito, com o tipo declarado **inferido do tipo estático do
  lado direito** — a variável continua type-stable (`let x = 10` seguido de
  `x = "a"` é erro de compilação), exatamente como se o tipo tivesse sido
  escrito. Inferência unidirecional (RHS → binding) e só em `let`: nada de
  Hindley-Milner, parâmetros, retornos e campos de struct continuam
  anotados. Superset puro — todo código anotado segue válido e idêntico.
  - O tipo inferido é o do binding, não o do literal: `[1, 2, 3]` infere
    `int[]` (dinâmico), não `int[3]`; `ref n` infere `ref int` (empréstimo,
    como com anotação); chamada genérica infere a instância primeiro
    (`let y = id(5)` → `int`).
  - Continuam exigindo anotação, com erro `cannot infer type for 'x' from
    its initializer: ...` + hint da forma anotada: literal vazio (`[]`, `{}`,
    também aninhado), `null`, expressão de tipo `any` no topo (fronteira
    dinâmica explícita; `any` aninhado como `map[string, any]` é inferido
    fielmente) e nome cujo tipo ainda não é conhecido (`let a = b` com `b`
    declarado depois).
  - `let x` sem tipo nem valor vira `SyntaxError: missing type annotation or
    initializer for 'x'` (hint: `let x: <type>` ou `let x = <value>`);
    `let x int = 1` mantém o `expected ':'` genérico.
  - **Builtins centrais com tipo de retorno estático** (`length`, `to_str`,
    `to_int`, `to_float`, `to_bytes`, `type`, `input`, `fmt`, `hex`,
    `hex_encode`, `hex_decode`, `ord`, `contains`, `has_key`, `json_dumps`,
    `keys(map[K, V]) -> K[]`, `slice`): antes compilavam como global
    desconhecido (tipo nil, só o runtime conferia); agora `let n =
    length(xs)` infere `int` e a checagem vale em toda posição — `let s:
    string = length(xs)` passa a ser erro de compilação, não de runtime.
    `func length(...)` declarado pelo programa continua sombreando o
    builtin. Membro de namespace (`m.x`, `m.f()`) e builtins de fronteira
    dinâmica (`json_parse`, `task_await`, ...) continuam sem tipo estático:
    `let` sobre eles pede anotação (ou `use m select f`).
  - Consequência: `~` em `bytes` passa no checador estático (`operand of
    '~' must be int or bytes, got bool`) — a VM sempre aceitou (`~` inverte
    cada byte, `test_bytes_full.nx`), mas nenhum valor de tipo estático
    `bytes` chegava ao check antes de `hex_decode`/`to_bytes` terem tipo.
    Spec §8 corrigida.
  - Globais de topo inferidos entram na pré-declaração (corpo de função
    declarado antes do `let` já enxerga o tipo certo), módulos exportam o
    tipo inferido e o REPL infere linha a linha.
  - Spec §3 (nova subseção *Local type inference*) e §6.2, README, exemplo
    `noxy_examples/let_inference.nx` no runner; `test_let_error.nx` passa a
    demonstrar o `let x` nu.

## [0.16.0] - 2026-08-22

Versão de marco: nenhuma mudança de código, sintaxe, semântica, saída ou
mensagem de erro em relação a 0.15.2 — quem está em 0.15.2 não precisa migrar
nada. O minor fecha o ciclo de performance pós-fase 2 (issue #66, itens 1–3,
entregues como 0.15.0–0.15.2: indexação tipada de array, fast path ASCII de
strings e protocolo de chamada) e marca o quanto a linguagem acumulou desde
0.7.0: genéricos por monomorfização, `call_result`, `type` e target typing em
`return`, `let` sem redeclaração, invariante do slot `ref T` com contêineres
donos dos filhos, os 16 achados do K&R em Noxy, `io` com cursor e campo
`m.T`, checagem estática de membro de `m.T` e de tipo desconhecido, `range`
builtin e `sys.version`, REPL com editor de linha em tty POSIX, layout de
`Value` em 32 B. Cross-runtime ao fim do ciclo (tempo líquido ÷ CPython, cada
par medido intercalado na própria rodada): `bubblesort` 5,5x → 1,8x (0.15.0),
`string_ops` 3,1x → 2,4x e `map_churn` 2,3x → 2,0x (0.15.1), `fib` 2,0x →
1,16x (0.15.2). O detalhe de cada entrega está nas seções abaixo; números em
`benchmarks/RESULTS.md`.

## [0.15.2] - 2026-08-22

Protocolo de chamada (issue #66, item 3 do roadmap pós-fase 2). Nenhuma
mudança de sintaxe, semântica, saída, mensagem de erro ou contagem RC; dois
opcodes novos por append ao fim de `internal/chunk/chunk.go`. Corpus 177/177 e
diff de saída base × head sem divergência. Números em `benchmarks/RESULTS.md`
(seção do topo) e `benchmarks/results/2026-08-22-issue-66-call-protocol-raw.md`.

### Performance

- **Retorno com fast path** (`OP_RETURN`): frame sem `defer`, sem vínculo RC
  registrado, sem upvalue aberto e acima do `minFrameCount` do `run()` corrente
  passa por `popSimpleFrame` (`unwind.go`) — o mesmo teardown terminal, sem a
  cópia dupla de `frameOutcome` nem a segunda chamada. Era o maior termo do
  perfil de `fib` (24 %).
- **Chamada com fast path** (`OP_CALL_STATIC`): callee closure com
  `ParamsUntracked` (flag nova em `ObjFunction`, calculada pelo compilador —
  todo parâmetro sem `ref` e de tipo `int/float/bool/string/bytes`) e aridade
  certa monta o frame inline, sem `callValueStatic`/`callPreparedClosure` nem o
  laço `ownSlot`; capacidade conferida à mão, `growForCall` continua o único
  dono das mensagens de overflow. Qualquer outro callee segue o caminho atual.
- **Superinstruções** emitidas em nível de AST: `OP_GET_LOCAL_ADD_IMM_INT
  [slot][imm i8]` para `local ± K` como expressão (`fib(n - 1)`) e
  `OP_GET_LOCAL_2 [a][b]` para `local OP local` (`a + b`, `i < n`, inclusive
  na condição fundida de `while`); só local plano de tipo primitivo, nunca
  `ref`/upvalue/global.
- **Resultado (v0.15.1 × v0.15.2, mesma máquina, intercalado):** cross-runtime
  `fib` **185,1 → 105,4 ms líquidos (0,57x; ÷ CPython 2,0x → 1,16x; ÷ Lua
  4,4x → 2,5x)**, `bubblesort` 0,91x (1,7x → 1,55x); `bench_bubblesort` −10,7 %,
  `bench_spawn_sum` −7,3 %; `BenchmarkNoxyCallOverhead` **−28 %** (meta ≥ −25 %);
  demais benches ±3 %, gates CoW verdes, sentinela `bench_generic_vs_hand`
  −1,7 %. Por estágio em `fib`: retorno −21 %, chamada −7 %, superinstruções
  −18 %.

### Corrigido

- `OP_CLOSURE`/`OP_CONSTANT`/`OP_CONSTANT_LONG` copiam o `ObjFunction` ao
  vincular ao ambiente e perdiam o flag novo — o fast path de chamada nunca
  disparava (sem efeito funcional); `TestClosureKeepsParamsUntracked` trava
  pelo valor da closure.

## [0.15.1] - 2026-08-22

Strings: fast path ASCII e `to_str(int)` sem `fmt` (issue #66, item 2 do
roadmap pós-fase 2, etapa 1). Nenhuma mudança de sintaxe, semântica, saída ou
mensagem de erro — em ASCII e em não-ASCII; nenhum opcode novo, `run()` e a
representação de string intocados. Corpus 177/177 e diff de saída base × head
sem divergência. Números em `benchmarks/RESULTS.md` (seção do topo) e
`benchmarks/results/2026-08-22-issue-66-strings-raw.md`.

### Performance

- **Fast path ASCII** em `strings.substring`, `strings.char_at`, `s[i]` e
  `slice()` de string: uma varredura por byte (`isASCII`, sem alocar) e, se a
  string é ASCII, índices em bytes e fatia de `string` compartilhada — sem
  `[]rune(s)` nem cópia por chamada. Qualquer byte ≥ 0x80 (acento, emoji,
  inválido) cai no caminho por runes de antes, intocado; o clamp de
  `substring` (negativos do fim, `[0, n]`) é um helper único para os dois
  ramos. `length` fica como estava: `utf8.RuneCountInString` já é varredura por
  byte sem alocar (1,3 % do perfil).
- **`to_str(int)` e `Value.String()` de `int`** via `strconv.FormatInt` em vez
  de `fmt.Sprintf("%d")` (saída idêntica, inclusive `MinInt64`) — o maior
  termo isolado de `string_ops` no perfil (22 %); beneficia `print`,
  interpolação (`f"k{i}"`) e concatenação. `to_str` de escalar
  (`int`/`float`/`bool`/`null`) pula `requireValidUTF8`: escalares renderizam
  ASCII por construção; a validação continua onde pode haver bytes.
- **Resultado (v0.15.0 × v0.15.1, mesma máquina, intercalado):** cross-runtime
  `string_ops` **96,2 → 74,2 ms líquidos (0,77x; ÷ CPython 3,1x → 2,4x)**,
  `map_churn` 132,3 → 115,0 (0,87x; 2,3x → 2,0x); `bench_map_churn` −11,5 %;
  demais benches ±3 % (ruído); gates CoW verdes; sentinela
  `bench_generic_vs_hand` +0,1 %. Por estágio: só o fast path ASCII dá −10 %
  em `string_ops`, `to_str(int)` mais −11 %.

### Interno

- `internal/vm/strings_ascii.go`: `isASCII` (custo de inline 23, guard em
  `inline_guard_test.go`); `clampSubstringRange` em `builtins_strings.go`.

## [0.15.0] - 2026-08-22

Indexação tipada de array (issue #66, item 1 do roadmap pós-fase 2). Nenhuma
mudança de sintaxe, semântica, saída ou mensagem de erro — o compilador passa a
emitir opcodes especializados onde já sabe que a base é `T[]`, e o VM executa
o mesmo que antes com menos despacho, menos tráfego de pilha e, no caso do
parâmetro `ref T[]`, sem a maquinaria genérica de resolução de referência.
Corpus 177/177 e diff de saída base × head sem divergência. Números em
`benchmarks/RESULTS.md` (seção do topo) e
`benchmarks/results/2026-08-22-issue-66-typed-arrays-raw.md`.

### Performance

- **Seis opcodes novos, por append ao fim de `internal/chunk/chunk.go`**
  (módulos cacheados continuam válidos): `OP_GET_INDEX_ARRAY` /
  `OP_SET_INDEX_ARRAY_NORC` para base estaticamente `T[]` em posição genérica
  (global, upvalue, nível de dentro de `a[i][j]`, índice com chamada);
  `OP_GET_LOCAL_INDEX_ARRAY` / `OP_SET_LOCAL_INDEX_ARRAY_NORC` para local `T[]`
  com índice (e valor) sintaticamente sem efeito colateral — o array vem do
  slot, sem empilhar o seu `Value`; o for-each sobre array usa a mesma
  leitura fundida; `OP_GET_REF_LOCAL_INDEX_ARRAY` / `OP_SET_REF_LOCAL_INDEX_ARRAY_NORC`
  para parâmetro `ref T[]`, que resolvem a caixa do ref com uma
  `Upvalue.Load()` em vez de `referenceStorage` (um `defer`, uma closure
  alocada e `reflect` por acesso). `NORC` = elemento `int/float/bool/string/bytes`:
  pula `Retain`/`Release` só depois de conferir em runtime que o valor novo e o
  velho não têm contador (`value.NeverTracked`) e que o array não é
  `(ref T)[]`; composto vindo por `any`, array compartilhado (CoW clona como
  antes) e qualquer container inesperado caem nos funis genéricos
  (`getIndexGeneric`/`setIndexGeneric`, os corpos de `OP_GET_INDEX`/`OP_SET_INDEX`
  extraídos em método) — mesmas mensagens, mesma linha.
- **Resultado (mediana de 9, intercalado, v0.14.3 × v0.15.0):**
  `bench_bubblesort` **−64,5 %**, `bench_call_ref` **−43,4 %**,
  `bench_call_readonly` −12,1 %, `bench_path_update` −9,2 %, `bench_conway`
  −3,7 %; gates CoW (`share_mutate`, `typed_call_map`, `call_light`,
  `conway`) dentro de ±2 % em rodada focada; `bench_map_churn` −0,2 % (os
  funis genéricos como chamada não aparecem). Cross-runtime (mínimo de 9,
  tempo líquido): `bubblesort` 430,6 → **153,6 ms (0,36x; ÷ CPython 5,5x → 1,8x)**; `fib`/`loop_arith`/`mandelbrot`/
  `map_churn`/`string_ops` entre 0,96x e 1,05x de v0.14.3 (ruído; A/B focado
  de `map_churn` dá −6,5 %).
- **Achado da rodada:** o primeiro desenho do fallback de leitura — re-despachar
  o `case` genérico por `goto` dentro de `run()` — custou +10–14 % no despacho
  genérico (`bench_generic_vs_hand`, laço sem indexação nenhuma): o rótulo e
  o corpo extra mudaram o codegen do laço inteiro. Trocado por chamada a
  `getIndexGeneric` antes do merge; o bench voltou ao ruído (+0,7 %).
- **Guards novos** em `internal/vm/inline_guard_test.go`: `value.NeverTracked`
  ≤ 20 e `arrayTagIsRefSlot` ≤ 20 (custa exatamente 20 — sem folga) inlinados
  nos sites de `executor.go`; `push`/`pop` seguem ≤ 20 (121/85 sites).

### Follow-ups observados (não entram nesta versão)

- `length(x)` compila como chamada de builtin genérica (`callNative`) — 37 %
  de `bench_call_readonly` é isso, não indexação; um `OP_LEN` estático quando
  `length` não está sombreado é o próximo passo natural desse bench.
- O custo restante do acesso via `ref T[]` é a `Upvalue.Load()` (RWMutex da
  caixa, partilhada com tasks); `bubblesort` fica em ~1,8x do
  CPython por causa dela.
- `a[i][j] = v` mantém `OP_GET_INDEX_MUT` genérico no nível de fora;
  elemento composto (`nodes[i] = n`) segue `OP_SET_INDEX`.

## [0.14.3] - 2026-08-22

Fase 2 de performance do VM (issue #37, estágios 1 e 2 mais o "extra barato"
do `pop`). Nenhuma mudança de sintaxe, semântica, bytecode, saída ou mensagem
de erro — só o layout interno do operando da VM e o caminho do contador de
referências. Corpus 177/177 e diff de saída base × head sem divergência.
Números por estágio em `benchmarks/RESULTS.md`.

### Performance

- **`value.Value` de 48 para 32 bytes.** Tag em `uint8`, um único campo para
  `int64`/`float64` (bits) e o `bool` no padding; a pilha de operandos cai de
  96 KB para 64 KB e cada `push`/`pop`/cópia de operando move um terço a
  menos. Leitura pelos acessores `Int()`/`Float()`/`Bool()` (os campos
  `AsInt`/`AsFloat`/`AsBool` deixaram de existir — afeta só quem embute a VM
  em Go; o único escritor in-place, `OP_INC_LOCAL_INT`, usa `SetInt`).
  Ler o acessor errado para a tag devolve os bits do campo, não zero — só
  alcançável chamando uma native crua diretamente com tipo errado
  (`time_sleep(1.5)`), o que o compilador já tolerava; os wrappers da stdlib
  são tipados. Isolado: −10 a −14 % em `fib`, `bubblesort`, `call_ref`,
  `call_readonly`, `share_mutate`.
- **Header comum nos compostos.** `ObjArray`, `ObjMap` e `ObjInstance`
  embutem `ObjHeader{Owners}` no offset 0 (layout travado por teste), e
  `ownersOf` — chamado por todo `Retain`/`Release`/`IsShared` — sai em uma
  comparação de byte para `string`/struct/`RuntimeTypeInfo` pela dica
  carimbada nos construtores; compostos seguem pelo type switch, que no `gc`
  já é comparação de ponteiro (microbench: indistinguível de `unsafe`).
  `Release` reescrito com comparação única para continuar inlinado (custo
  80 exato). Isolado: −0,3 a −4,7 %, ruído na maior parte dos benches.
- **`pop()` volta a ser inlinada dentro de `run()`.** Custava 22 no inliner
  (orçamento 20 para callee de função grande) e era chamada real em todos os
  ~84 sites do laço de despacho — 17 % do perfil de `fib`. Mesmo trabalho
  (zera o `Value` inteiro), forma nova (atribuição dupla com resultado
  nomeado, custo 18, 79 sites inlinados). Isolado: −6 a −25 % por cima dos
  dois estágios; guardado em `inline_guard_test.go` junto com `push`,
  `Retain` e `Release`.
- **Resultado (mediana de 9, intercalado, v0.14.2 × v0.14.3):** `fib`
  −17,7 % (2,9x do CPython, antes 4,1x), `bubblesort` −24 a −26 %,
  `call_readonly` −27 a −33 %, `call_ref` −26 %, `path_update` −27 a −31 %,
  `loop_arith` −20 %, `generic_vs_hand` −19 %, `share_mutate` −17 %,
  `conway` −15 %, `mandelbrot` −15 %, `string_ops` −12 %, `map_churn` −5 a
  −8 %, `spawn_sum` −8 a −13 %; startup neutro.

## [0.14.2] - 2026-08-22

Os quatro achados do perfil de cobertura de v0.14.1 (issue #61): uma brecha
do `let` sem inicializador, uma brecha do struct na fronteira `any`, um aviso
do compilador no lugar errado e código morto nos `Format()`. Duas das
correções fecham comportamento que programas podiam estar usando — as seções
BREAKING abaixo dizem como migrar.

### Changed (BREAKING)

- **`let nome: chan T` / `let nome: func...` sem `= valor` é erro de
  compilação.** A forma `let nome: tipo` (sem inicializador) sempre existiu
  e usava o "default" do tipo — `0`, `0.0`, `false`, `""`, `b""`, `[]`, `{}`
  e `null` para o resto. Só que `chan` e os tipos função **não são
  nuláveis** (`let c: chan int = null` já era "type mismatch"), e o `null`
  fabricado pelo `let` sem valor era a única brecha: em `chan` o marcador de
  runtime estourava com `runtime value metadata conflicts with static
  context` (um programa bem-formado falhava em runtime); em `func` passava
  em silêncio, um `null` num tipo que o checador nunca aceitaria. Agora o
  compilador diz `variable 'c' needs an initializer: chan int has no default
  value; hint: write 'let c: chan int = ...'` — também para o elemento de um
  array dimensionado (`let cs: (chan int)[2]`). Struct, `ref T` e `any`
  continuam começando em `null`; escalares, arrays dinâmicos e maps, no seu
  default. Spec §3, "Declaration without an initializer". **Migração**:
  `let c: chan int` → `let c: chan int = make_chan(n)`; `let f: func` →
  declare com o valor (ou `let f: any` se precisa de "ainda não tem").
- **Escrever num campo não declarado através de `any` é erro.** `let d: any
  = Point(1, 2)` seguido de `d.zzz = 1` criava o campo `zzz` na instância
  em silêncio (ler `d.zzz` já era `undefined property 'zzz'`, e com base
  tipada o compilador rejeita com `unknown field`). Struct é nominal, de
  campos fixos (spec §5): `OP_SET_PROPERTY` passa a devolver o mesmo
  `undefined property 'zzz'` da leitura quando o nome não está na
  declaração. Campos declarados que uma native tenha deixado por preencher
  continuam aceitando escrita. Caminho quente inalterado — a declaração só é
  consultada quando o nome não está na instância.

### Fixed

- **Aviso `rebinding ref parameter ... has no effect outside function` saía
  em stdout.** O compilador escrevia com `fmt.Printf`, misturado à saída do
  programa (contra a regra "stdout é do programa, stderr é do diagnóstico").
  O compilador agora não escreve em lugar nenhum: acumula `Warning`s
  estruturados (`Compiler.Warnings()` — mensagem, arquivo, linha) e quem
  chama decide o destino: `noxy arquivo.nx` e o REPL imprimem em `diagOut`
  (stderr), um módulo carregado por `use` imprime em stderr pela VM. Mesmo
  texto de antes (`warning: ...` + `  --> arquivo:linha`), mesmo código de
  saída (aviso não é erro). Os exemplos `ref_pointer_semantics.nx` e
  `test_ref_case.nx`, que demonstram o rebind local de propósito, só mudam
  de canal.
- **`ArrayType.String()` de array de `chan`/`ref` ganha parênteses.**
  `(chan int)[]` imprimia `chan int[]`, que re-parseia como canal de `int[]`
  (o prefixo captura o `[]`); agora imprime `(chan int)[]`, como já fazia
  para funções — é o que aparece no hint do `let` sem inicializador e em
  mensagens de tipo.

### Removed

- **`case 'T'` nos `Format()` de `internal/value`** (`ObjArray`, `ObjMap`,
  `ObjStruct`, `ObjInstance`, `ObjChannel`, `ObjWaitGroup`, `ObjRef`,
  `BytesWrapper`): código morto — o pacote `fmt` resolve `%T` antes de
  consultar `Formatter`, e o builtin `fmt` da Noxy troca `%T` por
  `runtimeTypeName` antes de formatar. Os nomes dos dois lugares já
  divergiam sem ninguém perceber; agora há uma fonte só
  (`runtimeTypeName`). Sem mudança visível.

## [0.14.1] - 2026-08-22

Ergonomia do REPL fora do Windows: no Linux/macOS (WSL incluído) uma seta
no prompt imprimia `^[[A` em vez de andar pelo histórico, porque o modo
canônico do tty só entende Backspace/^U/^W e o REPL lia a linha crua. O REPL
ganha um editor de linha próprio em terminais POSIX; no Windows, onde o
console em modo cooked já edita a linha e guarda histórico, nada muda.

### Added

- **Edição de linha e histórico no REPL (Linux/macOS).** Quando stdin é um
  terminal, o REPL lê cada linha com um editor próprio (`internal/lineedit`,
  no espírito do readline): ←/→ (Ctrl-B/Ctrl-F) movem o cursor, Home/End
  (Ctrl-A/Ctrl-E) vão às pontas, Backspace/Delete apagam (Ctrl-D com texto =
  Delete), Ctrl-K/Ctrl-U apagam até o fim/início, Ctrl-W apaga a palavra
  anterior, Ctrl-L limpa a tela, Tab insere 4 espaços. ↑/↓ (Ctrl-P/Ctrl-N)
  percorrem o histórico da sessão — só em memória; linha vazia e repetição
  imediata não entram — preservando a linha que estava sendo digitada ao
  voltar. Ctrl-C **encerra o REPL** (eco `^C`, código de saída 130 =
  128+SIGINT), exatamente como o SIGINT encerrava antes e como no Windows —
  para limpar a linha sem sair use Ctrl-U; Ctrl-D numa linha vazia (ou
  `exit`) sai com 0. Texto UTF-8 é editado por rune (uma coluna por rune; sem
  tratamento de largura dupla) e linha mais larga que o terminal rola
  horizontalmente em vez de quebrar. O tty só fica em modo raw durante a
  leitura de UMA linha e volta ao modo anterior antes de cada execução —
  `input()`/`io.read_line` e o shell continuam vendo o modo cooked; `OPOST`
  fica ligado, então saída de tasks durante a edição não sai "em escada". Em
  pipe/arquivo (stdin não é tty) e no Windows o REPL lê linhas como antes.
  Sem dependência nova: termios via `golang.org/x/sys/unix`, já usado.

## [0.14.0] - 2026-08-22

Dois pedidos pequenos, no espírito do Python: `range` vira builtin do runtime
(sem import) e `sys.version` diz qual Noxy está rodando o programa. A mudança
em `range` quebra quem importava a versão antiga de `array_utils`.

### Added

- **`range` é builtin do runtime.** `range(stop)`, `range(start, stop)` e
  `range(start, stop, step)` devolvem `int[]` com a semântica do Python:
  `stop` exclusivo, `step` negativo conta para trás, intervalo vazio dá `[]`
  (`range(5)` → `[0, 1, 2, 3, 4]`, `range(10, 0, -3)` → `[10, 7, 4, 1]`,
  `range(3, 0)` → `[]`). Não precisa de `use`. O compilador tipa a chamada
  (spec §10): aridade fora de 1–3 (`range expects 1 to 3 arguments, got 4`)
  e argumento que não é `int` (`argument 2 to 'range': expected int, got
  float`) são erros de compilação, e o retorno é `int[]` — `for i in
  range(n)` dá `i: int`, como a versão antiga de `array_utils`, em vez de
  cair na brecha das natives sem assinatura (retorno desconhecido). Em
  runtime, `step == 0` é erro (`range: step must not be zero`), na linha do
  `zeros(-5)`; uma sequência com mais de 2³¹−1 elementos também (`range:
  sequence too large`) — o array é materializado, e o erro é do script em vez
  de um estouro de memória do lado Go. Uma `func range` do próprio programa
  continua vencendo o builtin (mesma regra de `append`/`pop`).
- **`sys.version`.** `use sys` + `print(sys.version)` imprime a mesma string
  de `noxy --version` (`v0.14.0`, fonte única `internal/version`); também via
  `use sys select version`, que entra tipado como `string`. Por baixo, o
  native `sys_version()`; no `sys.nx` é um `let` de topo, como
  `http_parser.HTTP_200_OK`.

### Changed (BREAKING)

- **`range` saiu de `array_utils`.** `use array_utils select range` passa a
  falhar (`undefined property 'range' in module/map`). **Migração**: remova
  o `select range` — o builtin já está no escopo — e, se quiser,
  `range(a, b, 1)` vira `range(a, b)`. `use array_utils select *` seguido de
  `range(...)` continua funcionando (resolve no builtin). Diferença de
  comportamento: `range(a, b, 0)` devolvia `[]` na versão do módulo e agora é
  erro de runtime. Os exemplos `KandR_in_noxy/ch01_fahr_const.nx`,
  `ch01_fahr_for.nx`, `ch02_cond.nx` e `test_array_util.nx` foram ajustados;
  `array_utils.slice` fica onde estava.

## [0.13.0] - 2026-08-21

Os dois follow-ups da 0.12.0 registrados na issue #58, ambos sobre checagem
estática do compilador: o acesso a membro de um valor tipado por struct de
módulo (`m.T`) passa a ser tipado, e um nome de tipo que não existe passa a ser
erro de compilação em campo de struct, parâmetro, retorno e `let`. Os dois
itens quebram compatibilidade — sempre de programas que já estavam errados
(compilavam e falhavam em runtime) ou que nomeavam um struct de módulo sem
tê-lo importado.

### Changed (BREAKING)

- **Acesso a membro de um valor tipado por struct de módulo é tipado
  estaticamente.** `a.f.path` com `f: io.File`, `res.rows` com
  `res: sqlite.QueryResult` (e o mesmo com a forma por `select`) têm tipo
  conhecido pelo compilador. O tipo do campo é **traduzido para a visão do
  programa** (spec §11): `rows: Row[]`, escrito dentro de `sqlite.nx`, vira
  `Row[]` se o programa fez `use sqlite select Row`; `sqlite.Row[]` (ou
  `db.Row[]` com `use sqlite as db` — vale o primeiro alias declarado) se fez
  `use sqlite`; e fica **dinâmico**, como antes, se o programa não tem como
  nomear o struct — um tipo só parcialmente nomeável (`map[string, Row]` sem
  `Row` acessível) fica inteiro dinâmico, nunca meio-tipado. Consequência:
  código que escrevia algo errado sobre um campo desses (`let x: string =
  a.f.fd`, `a.f.fd = "x"`, `inc(ref a.f.path)` com `inc(r: ref int)`)
  compilava e falhava em runtime; agora é erro de compilação (`type mismatch
  in 'x' declaration: expected string, got int`). Vale para leitura,
  atribuição de campo, encadeamento (`w.o.i.v`) e `ref campo`. Um programa com
  `use sqlite select QueryResult` (sem `Row`) que lia `res.rows` como `Row[]`
  cru — nome que nunca importou — passa a vê-lo dinâmico; para tipá-lo,
  importe `Row` ou use `use sqlite`.
- **Tipo inexistente em anotação é erro de compilação** — `[line N]
  <posição>: unknown type 'Inexistente'` + `hint: declare 'struct Inexistente'
  or import it with 'use m select Inexistente'`, em campo de struct (`struct
  'A' field 'b'`), parâmetro (`function 'f' parameter 'x'`), retorno
  (`function 'f' return type`) e `let` (`variable 'x'`). Antes,
  campo/parâmetro/retorno compilavam em silêncio (o construtor do struct
  falhava em toda chamada com `struct constructor has incomplete runtime type
  metadata`) e `let` rejeitava pela via errada (`type mismatch: expected
  Inexistente, got int`, apontando para o valor). A forma qualificada mantém a
  mensagem da 0.12.0 (`cannot resolve type 'io.Nope': module 'io' has no
  struct 'Nope'` / `'foo' is not an imported module`), agora também em
  parâmetro, retorno e `let` — inclusive `m.T` com módulo que não carrega
  (`module 'm' could not be loaded`), que antes compilava e só falhava no `use`
  em runtime. **Migração**: um struct de módulo só pode ser nomeado se o
  programa o importou — `use io` + `let f: File = io.open(...)` passa a exigir
  `let f: io.File` (ou `use io select File`); `use sys select getenv` + `let e:
  EnvResult` exige `select getenv, EnvResult`. A varredura dos `.nx` do
  repositório corrigiu três exemplos (`test_import_sugar.nx`,
  `cadastro_usuarios.nx`, `form_app.nx`); os exemplos de servidor
  `http_server_basic.nx`, `http_server_docs.nx` e `form_app.nx` já estavam
  desatualizados em relação à API atual (`server_poll`/`RequestEvent` não
  existem) e passam a falhar na compilação em vez de em runtime; o livro *A
  Linguagem de Programação Noxy* não tem nenhum bloco afetado. Instância de
  struct genérico importado (`use m select Caixa`) cujo campo nomeia outro
  struct de `m` exige importar esse struct junto (`select Caixa, Meta` — a
  regra do §6.4), com o mesmo erro. Seguem compilando: referência adiantada
  entre structs de topo, auto-referência (`next: ref Node`), structs
  declarados dentro de função, instâncias genéricas (`Caixa<int>`) e `T`
  dentro de template; um `use … select` de topo vale para o arquivo inteiro
  (campo ou assinatura declarado antes da linha do `use` já enxerga o struct).
  A assinatura de uma função é resolvida no escopo em que a função é
  declarada: um struct declarado **dentro do corpo** não pode aparecer no
  parâmetro/retorno (`func make() -> Pair` com `struct Pair` no corpo era
  aceito, com retorno dinâmico; agora `function 'make' return type: unknown
  type 'Pair'` — mova o struct para o top level). Um campo de struct de módulo
  cujo tipo é uma **instância de template do próprio módulo** (`c: Caixa<int>`
  dentro de `g.nx`) continua dinâmico no acesso a membro (o nome interno da
  instância não é identidade entre unidades de compilação).

### Fixed

- **`null` em campo de struct nomeado por `ns.T`** (`a.f = null` com
  `f: io.File`, `let g: io.File = null`) e em campo traduzido de struct de
  módulo (`w.o.i = null`) é aceito: a checagem de `null` resolve pela
  declaração do struct, não por nome simples.
- **REPL lembra os `use` de linhas anteriores** (aliases de namespace e o
  cache de módulos): `use io` numa linha e `let f: io.File = io.open(...)` na
  seguinte funcionam, e os structs importados por `select` mantêm a origem
  para o acesso a membro tipado. Antes, um campo `f: io.File` numa linha
  posterior ao `use` era recusado (`'io' is not an imported module`).
- **Struct reexportado por `select` é conhecido pelo importador.** Se `a.nx`
  faz `use b select T` e uma função de `a` devolve `T`, `use a select *` (ou
  `use a select T, mk`) no programa já ligava o construtor `T` como valor, mas
  não a declaração — `let t: T = mk()` só compilava por igualdade de nome (e
  `t.v` era dinâmico). A declaração acompanha o valor.

### Docs

- Spec §2.0 (tipo desconhecido em anotação é erro), §11 (acesso a membro de
  `m.T` tipado e a regra de tradução do tipo do campo para a visão do
  programa; `unknown type` nas quatro posições); exemplo
  `noxy_examples/test_member_access_qualified.nx`; exemplos corrigidos acima.

## [0.12.0] - 2026-08-21

Dois achados pós-0.11.0 da releitura do K&R em Noxy: o campo de struct
tipado com nome qualificado de módulo (`file: io.File`) quebrava o construtor
em runtime, e o módulo `io` não tinha posicionamento (`seek`/`tell`) nem
leitura parcial a partir do cursor — o `get(fd, pos, buf, n)` da seção 8.4
era o único programa do livro que não podia ser reproduzido. Um item quebra
compatibilidade.

### Changed (BREAKING)

- **`io.read`, `io.read_bytes` e `io.read_lines` leem do cursor até o fim**,
  não mais "sempre do início". Num handle recém-aberto nada muda (é o arquivo
  inteiro); depois de `read_line`/`read_n`/`seek` devolvem o **resto**, e um
  segundo `read` devolve `""` (`[]`, `b""`) com `ok=true` — a regra que
  `stdin()` já seguia, agora única para arquivo e stdin. Antes, `read` depois
  de `read_line` recomeçava do zero (e ignoraria um `seek`). Vale também
  depois de `write` no **mesmo** handle (`"rw"`/`"a"`): o cursor fica depois
  do que foi escrito, então `read` logo após `write` devolve `""` — antes
  devolvia o arquivo inteiro. Para reler do início: `io.seek(f, 0,
  io.SEEK_SET)` antes do `read`. `read_line` depois de um `read` continua
  reportando `EOF` (o cursor fica no fim).

### Added

- **Posicionamento em `io`** (spec §12): `io.seek(f, offset, whence) ->
  IOPositionResult` (`SEEK_SET`/`SEEK_CUR`/`SEEK_END` como constantes do
  módulo, `position` absoluta, `ok=false` + `error` para stdin, `whence`
  inválido, posição negativa ou arquivo fechado), `io.tell(f) ->
  IOPositionResult` e `io.read_n(f, n) -> IOBytesResult` (até *n* bytes a
  partir do cursor; menos só no fim; `EOF` explícito; funciona em stdin).
  Struct novo `IOPositionResult {ok, position, error}`.
- **`io.write`/`io.write_bytes` (e `_result`) escrevem na posição do cursor**:
  em `"rw"`/`"r+"` sobrescrevem no lugar sem truncar — `seek` + `write`
  atualiza um registro no meio do arquivo; `read_line` + `write` escreve logo
  depois da linha lida (o buffer do leitor de linha é re-sincronizado com o
  offset do SO antes de qualquer escrita, `seek` ou leitura até o fim).
  `tell` reporta a posição lógica (o que o programa consumiu), e `read_line`
  depois de `seek` lê da nova posição.

### Fixed

- **Campo de struct tipado com nome qualificado (`file: io.File`,
  `quando: time.DateTime`, `a: geometry2.Point`) constrói normalmente.** O
  construtor do struct local falhava em toda chamada com `struct constructor
  has incomplete runtime type metadata`: o compilador só conhecia structs
  importados por `select`. Agora o nome do campo resolve pela declaração que
  designa (a mesma de `use m select T`, #56 item 8) e os campos de um struct
  importado são resolvidos no escopo do **módulo que o declarou** — o que
  também conserta `use m select Outer` quando `Outer` tem um campo de outro
  struct de `m` nunca importado pelo programa. Um `ns.T` que não resolve
  (módulo não importado, módulo sem o struct) é **erro de compilação** no
  struct, com hint (spec §11). O acesso a membro de um valor tipado `m.T`
  continua dinâmico (tipá-lo exige traduzir os nomes do módulo para a visão do
  programa — follow-up).

### Docs

- Spec §11 (identidade de struct entre formas de importação cobre campos de
  struct; erro de `ns.T` não resolvido), §12 (modelo de cursor do `io`,
  tabela com `seek`/`tell`/`read_n`/`IOPositionResult`, contratos de
  `read*`/`write*` reescritos; exemplo do `get` do K&R); exemplos
  `noxy_examples/test_struct_field_qualified.nx`, `test_io_seek.nx`,
  `knr_get.nx`.

## [0.11.0] - 2026-08-21

Entrega dos 16 achados do relatório de validação da VM ao reescrever o K&R em
Noxy (issue #56); design em
`docs/superpowers/specs/2026-08-20-issue-56-knr-findings-design.md`. Três itens
quebram compatibilidade.

### Changed (BREAKING)

- **A condição de `if`/`elif`/`while` — e os operandos de `!`, `&&`, `||` —
  tem de ser `bool`.** Não há valores truthy/falsy: antes `if 0`, `if ""` e
  `if null` entravam no `then` em silêncio. Com o tipo estático conhecido é
  erro de compilação (`[line N] condition must be bool, got int` +
  `hint: use an explicit comparison, e.g. 'x != 0', 'x != ""', 'x != null' or 'length(x) > 0'`);
  para `any`, erro de runtime (`condition must be bool, got int`). Migração:

  | Antes | Depois |
  |-------|--------|
  | `if n then` (`int`/`float`) | `if n != 0 then` |
  | `if s then` (`string`) | `if s != "" then` |
  | `if p then` (struct, array, map, `ref`) | `if p != null then` |
  | `if xs then` ("não vazio") | `if length(xs) > 0 then` |

  A varredura de todos os `.nx` do repositório (`noxy_examples/`, `tests/`,
  `internal/stdlib/`, `noxy_libs/`) encontrou **um** arquivo a migrar:
  `noxy_examples/binary_tree.nx:33`, que usava o bitwise `|` como "ou" lógico
  entre dois `bool` (`posicao < 0 | posicao >= arr.tamanho` → `||`) — bug
  latente exposto pela checagem estática de bitwise (abaixo). Nota: `if r` com
  `r: ref bool` continua compilando (deref automático), mas `&&`/`||` nunca
  derefam, então `r || x` passa a ser erro (`logical operators require boolean
  operands, got ref bool and bool`) — já era assim para `&&`, e `||`, que não
  checava nada, herda a regra; escreva `*r || x`.
- **Os diagnósticos da VM e da CLI saem em stderr.** Erros de parser, de
  compilador e de runtime (com o `hint:`), `Error reading file:` e os erros de
  thread/plugin passam a ser escritos em **stderr**; `print`/`iprint` continuam
  em stdout. Quem capturava mensagens de erro pela saída padrão precisa de
  `2>&1` (`noxy programa.nx > tudo.txt 2>&1`).
- **`io.read_lines` não devolve mais um `""` final.** `"a\nb\n"` agora é
  `[a, b]` (era `[a, b, ""]`), `"a\nb"` é `[a, b]`, `"\n"` é `[""]` e `""` é
  `[]` (era `[""]`). Código que descartava a última linha vazia à mão (ou que
  fazia `length(linhas) - 1`) precisa parar de fazê-lo.
- **`io.list_dir` devolve `IOLinesResult`** em vez de `IOResult`: `data` é
  `string[]` com os nomes das entradas, ordenados por nome e sem o caminho do
  diretório. A assinatura antiga (`data: string`) nunca chegou a funcionar — a
  nativa não existia. Para distinguir arquivo de diretório use
  `io.stat(path + "/" + nome).is_dir`.
- **F-string: `{{` e `}}` são escapes de chave literal**, e uma expressão que
  *começa* por `{` (um map literal) precisa de espaço: `f'{ {"a": 1}["a"] }'`
  — antes `f'{{"a": 1}["a"]}'` era aceito. É a mesma regra do Python. Sobra
  depois da expressão interpolada também deixa de ser aceita em silêncio:
  `f"{x:>10}"` é erro de sintaxe (`unexpected ":" in f-string expression` +
  `hint: format specs are not supported; use fmt("%10s", x) for width/precision`).

### Added

- **`continue`** em `while` e `for` (nova keyword): pula para a próxima
  iteração — reavalia a condição no `while`, avança o elemento no `for`.
  Aninhamento segue o laço mais interno; fora de laço é erro de compilação.
- **`eprint(args...)` e `eiprint(args...)`** — `print`/`iprint` em stderr, o
  `fprintf(stderr, ...)` do C.
- **`io.stdin() -> File`** — o stdin do processo como `io.File`
  (`path="<stdin>"`, só leitura, não fechável: `close_result` devolve
  `"stdin cannot be closed"` e `write_result` `"stdin is read-only"`), para ler
  com `io.read_line` e ter EOF explícito. As leituras de stdin de tasks
  concorrentes são serializadas pelo mesmo recurso.
- **`io.read_line(file) -> IOResult`** — leitura incremental: devolve a próxima
  linha sem `\r\n`; no fim, `ok=false, data="", error="EOF"`; a última linha sem
  `\n` é devolvida normalmente e a chamada seguinte é que dá EOF.
- **`io.list_dir`, `io.rename`, `io.write_bytes` e `io.write_bytes_result`** —
  as nativas que faltavam (`list_dir`/`rename`) e a API explícita para gravar
  `bytes` (antes só funcionava pela forma de namespace, que não checa o tipo do
  argumento).
- **Literais float em notação científica**: `1e3`, `1.5e-3`, `2E+10`. O
  expoente faz o literal ser `float` mesmo sem ponto decimal; `.5` continua não
  sendo aceito.
- **Escapes `{{`/`}}` em f-strings** (ver a quebra acima): `f"{{x}}"` produz
  `{x}`.
- **`OP_ARRAY_FILL`** — `let a: T[N]` sem inicializador passa a emitir
  `default; N; OP_ARRAY_FILL` em vez de empilhar N elementos: `int[100000]`
  funciona, e não há mais o operando de 16 bits truncando N > 65535 em
  silêncio.
- **Pilhas de frames e de operandos dinâmicas**: cada VM nasce com 64 frames e
  4096 slots e cresce sob demanda até os tetos de **100 000 frames** e
  **1 048 576 slots**; no teto o erro é de runtime
  (`stack overflow: call depth exceeds 100000 frames` /
  `stack overflow: operand stack exceeds 1048576 slots`), nunca um panic Go, e
  dentro de `call_result` vira um `Failure` capturável. Custo medido em
  rodadas intercaladas (mediana de 9): corpus `benchmarks/bench_*.nx` com
  **mediana +0,95 %** (faixa −6,8 %…+8,3 %) e `BenchmarkNoxyCallOverhead` em
  **+2–3 %** — piso de ruído do ambiente.
- **Exemplos** `noxy_examples/test_continue.nx` e `test_read_line.nx`, mais
  `wc_stdin.nx` (o `wc` do K&R, excluído do runner por ler stdin). Runner:
  173/173.

### Fixed

- **Recursão deixa de morrer em 62 níveis.** As pilhas eram arrays fixos
  (`FramesMax = 64`, `StackMax = 2048`) embutidos na `VM`: `depth(10000)` da
  issue dava `stack overflow` com 62 chamadas aninhadas. Agora crescem sob
  demanda (acima).
- **`let a: int[N]` com N grande não derruba mais a VM.** N > ~2047 estourava a
  pilha de operandos com um panic Go ("Stack overflow"); N > 65535 truncava o
  operando de `OP_ARRAY` em silêncio.
- **`input()` com stdin redirecionado lê todas as linhas.** Cada chamada criava
  um `bufio.Reader` novo sobre `os.Stdin`, e a primeira engolia até 4 KB — a
  segunda chamada via EOF. Agora existe um leitor único por `SharedState`,
  compartilhado com `io.stdin()`, e o erro de leitura não é mais ignorado.
  `input()` continua sem sinalizar EOF (devolve `""`, indistinguível de linha
  vazia) — para distinguir, use `io.read_line(io.stdin())`.
- **Wrappers de `io` sem nativa por trás.** `io.read_line`, `io.list_dir` e
  `io.rename` existiam em `internal/stdlib/io.nx` apontando para nativas
  inexistentes. Um teste de higiene (`stdlib_hygiene_test.go`) passa a exigir
  que todo identificador chamado por um módulo da stdlib seja declarado no
  módulo, importado de outro módulo ou registrado como nativa.
- **`if c then return end` em uma linha** deixa de ser erro de sintaxe: um
  `return` sem valor seguido de `end`/`else`/`elif` é aceito.
- **O erro de tipo em atribuição aponta a linha da atribuição**, não a última
  linha compilada antes dela.
- **`noxy arquivo_inexistente.nx` sai com código 1** (saía com 0 depois de
  imprimir `Error reading file:`).
- **`*` em não-ref, `!` em não-bool, `~` em não-int e bitwise com tipos errados
  são erros de compilação** quando o tipo estático é conhecido: `2 ** 3` (não
  existe operador de exponenciação) reporta
  `cannot dereference non-reference value of type int`; `~true` reporta
  `operand of '~' must be int, got bool`; `1 & true` reporta
  `operands for & must be integers or bytes, got int and bool`. `&`, `|` e `^`
  aceitam `int` ou `bytes`; `<<` e `>>` só `int`.
- **`break` e `continue` fecham os upvalues dos locais capturados.** `break`
  emitia `OP_POP` cru e deixava aberta a caixa de um `let` do corpo capturado
  por uma closure: o slot era reusado pela iteração seguinte e a closure
  passava a ler outro valor. Os dois agora usam a mesma regra do `endScope`.
- **`m.x = v` tem erro claro.** Atribuir a uma variável de módulo pela forma de
  namespace é erro de compilação
  (`cannot assign to 'm.x': module variables are read-only outside the module`
  + `hint: expose a function in 'm' that updates it`), em vez de gravar num
  binding que ninguém lê.
- **`geometry.Point` e `Point` (via `select`) são o mesmo tipo nominal.**
  Misturar as duas formas de import do mesmo struct dava erro de tipo; a
  comparação passa a ser estrutural com identidade de declaração, inclusive em
  tipos de função (`func(Point)` ≡ `func(geometry.Point)`) e na inferência de
  genéricos. Um struct local de mesmo nome continua sendo um tipo diferente.

### Docs

- Spec §1.2 (`continue`), §2.1 (notação científica; `.5` não é aceito), §2.2
  (**a ordem de iteração e de impressão de um `map` é indefinida**), §7
  (condição obrigatoriamente `bool` com tabela de migração, `break`/`continue`,
  `if c then return end`, subseção *Limites de chamada*), §8 (**a aritmética de
  `int` dá a volta em overflow** — complemento de dois, como Go, sem erro em
  `+ - *`; `!`/`&&`/`||` exigem `bool`; tipos dos bitwise; `*` unário só em
  `ref`), §9 (escapes, ausência de format spec, regra do espaço, aspas
  simples), §10 (`print`/`iprint`/`eprint`/`eiprint` e o contrato de `input`),
  §11 (**`select` de variável copia o valor** — snapshot no import; `m.x = v`
  é erro; identidade nominal entre namespace e `select`), §12 (tabela completa
  da API `io`, incluindo quando **não** misturar `read`/`read_lines` com
  `read_line`; nota de `sys.exec_output` no Windows: já roda via `cmd /C`, não
  aninhar `cmd /c`, saída não-UTF-8 dá `ok=false`) e §13 (pilhas dinâmicas).
- README (builtins `eprint`/`input`/`fmt`, stderr e exit code em *Usage*) e
  AGENTS.md (§E: stdout do programa × stderr do diagnóstico; §Segurança:
  overflow não é checado e `ensureCallCapacity` é o único ponto de checagem dos
  tetos).

## [0.10.1] - 2026-08-20

### Fixed — contêineres criados por natives/plugins são donos dos filhos (issue #55)

- **`slice`, `sqlite.query` (`columns`, `rows[i].values`), `task_await` (`value`,
  `error`), `io.read_lines` (`data`), `strings.split` (`parts`) e plugins
  (`InterfaceToValue`) devolviam contêineres que não eram donos dos filhos
  compostos**: a cópia por valor (`let s: Pair[] = slice(t, 0, 2); s[0].a = 9`)
  mutava o original (`t[0].a` lia 9). Regra do runtime — *todo contêiner é dono
  durável de cada filho composto* — já valia no bytecode (`OP_ARRAY`/`OP_MAP`/
  construtor de struct) e passa a valer nos natives: `value.NewArray`/
  `NewMapWithData` retêm cada filho composto, `NewInstanceWith` constrói
  instâncias retendo os campos. Os sites que já entregavam filhos retidos
  (`OP_ARRAY`, clone CoW, merge de `causes` do `call_result`) usam
  `NewArrayAdopting`; o retain manual do envelope `ok` do `call_result` saiu
  (o construtor registra a posse). Efeito visível: código que dependia do
  aliasing acidental passa a ver a cópia independente (e um clone CoW na
  primeira escrita ao filho ainda compartilhado). Sem mudança de API Noxy.
- Fase A da #54; itens 1b/1c/1d da #53.

## [0.10.0] - 2026-08-20

### Changed (BREAKING) — invariante do slot `ref T`: checagem de campo vale através de base `ref`, `json_loads` cria célula, fim do shim (issue #50)

- **Atribuição a campo com base `ref` é checada como com base valor.**
  `node.valor = "texto"` com `node: ref Node` era aceito pelo compilador — a
  checagem de campo inteira era pulada quando a base do L-value era `ref`
  (herança pré-0.4) — e gravava `string` num campo `int`;
  `node.proximo = Node(9, null)` gravava um `Node` *cru* num campo `ref Node`.
  Agora são os mesmos erros da base valor:
  `type mismatch in field assignment: expected int, got string` e
  `cannot assign Node to ref Node`. Com isso também passam a valer via `ref`
  o target typing do campo (§3, `node.f = identity`) e a validação de runtime
  de campos compostos (`OP_MARK_RUNTIME_VALUE_TYPE`), como já valia via valor —
  superfície nova de erro para programas que só rodavam via `ref`.
- **Hint novo para campo/elemento/entrada `ref T`:**
  `hint: to point the field at a new value, bind it to a variable first and use 'x.proximo = ref novo'; to overwrite the referenced value use '*x.proximo = ...'`
  (variável `ref T` mantém `use '*r = ...'`).
- **Migração** (alcançou `noxy_examples/stack.nx` e 6 testes de
  `internal/vm/rc_uniqueness_test.go`; `linked_list.nx` já estava assim desde
  a 0.9.1):

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

  Posse: antes o campo era o dono durável do nó; agora o dono é a célula heap
  do `let novo` e o campo guarda a ref — `campo = null` não solta mais o nó (o
  GC recolhe a célula quando nada mais a alcança); as contagens de `Owners`
  observáveis não mudam (1 dono nos dois casos). **Efeito visível:** depois da
  migração, `let u: ref Node = no.proximo; u.valor = X` altera o nó da lista
  (semântica de referência, a mesma de `Node(0, ref n)`); com o `Node` cru no
  slot, `u` virava uma cópia e a escrita se perdia — dois testes RC tiveram o
  oráculo re-derivado por isso (20 → 77). Dentro de laços com `break`, lembrar
  da issue #52: prefira a forma recursiva.
- **`json_loads` com slot `ref T` nulo cria uma célula heap + ref** (opção (a)
  da #50). Payload não-nulo para um elemento/campo/valor `ref T` que está
  `null` (ou é novo) constrói o `T` pelo schema do referente, cria uma célula
  nova que o possui e grava no slot uma ref para ela — o análogo de
  `let novo = T; slot = ref novo`. Depois, `let viz: ref T = slot; type(ref viz)`
  é `"ref"`, `*viz` lê o valor e `slot` passa a parâmetro `ref T` pelo
  encaminhamento normal. Antes, o `T` cru ia direto para o slot
  ("legacy-filled"). Slot já apontando: escreve através (inalterado); payload
  `null`: limpa (inalterado). **Alvo direto** `json_loads(s, h.child)` com
  `child` nulo continua `false` — o null é encaminhado, não há slot por trás
  (0.9.1): passe o dono (`json_loads(s, h)`).
- **Shim removido.** `OP_CONTEXT_REF_PROPERTY`/`OP_CONTEXT_REF_INDEX` não
  embrulham mais valor cru numa ref para o slot:
  `reference slot 'proximo' holds a non-reference value` (ou `at index N` /
  `for key "k"`) é erro de runtime explícito — estado que nenhum programa Noxy
  produz mais.
- **Base `any` se comporta como base tipada para slots `ref T`.**
  `ref a.proximo`, `f(a.proximo)` e `json_loads(s, a.proximo)` com `a: any`
  encaminham a ref/null armazenada (antes fabricavam ref para o slot e
  `*n = Node(...)` gravava cru); `a.proximo = Node(9, null)` via `any` é erro
  de runtime `cannot assign Node to ref Node` (o gêmeo dinâmico do erro de
  compilação), e o mesmo vale para elemento/valor `ref T` de array/map
  etiquetado (`d[0] = 5` → `cannot assign int to ref int`). Campo comum via
  `any` (`a.valor = "texto"`) segue sendo fronteira dinâmica sem checagem.

### Fixed — contagem de donos (RC) dos valores construídos por `json_loads`/`json_parse`

- Compostos criados pelos builders JSON entravam em arrays/maps/structs **sem
  `Retain`**, e substituições não soltavam o ocupante anterior:
  `let t: Pair[] = []; json_loads("[{\"a\":1,\"b\":2}]", t); let p: Pair = t[0]; p.a = 99`
  mutava `t[0]` no lugar (IsShared falso com 2 donos reais). Agora os builders
  espelham `OP_ARRAY`/`OP_MAP`/construtor (todo contêiner que guarda um
  composto é um dono), as substituições fazem retain-novo/release-velho,
  posições descartadas são soltas, e toda escrita *através* de uma ref (alvo
  top-level e slot `ref T` já apontando) passa por `storeReferenceValue`.
  `json_parse` idem.

### Docs

- Spec §2.3 (checagem de campo através de base `ref` + hint), §4.2 (valor cru
  em slot `ref T` é erro explícito; base `any`), §5 (campo `ref` se preenche
  por rebind), §12 (subseção JSON: contrato de `json_loads` para slot `ref T`);
  `docs/JSON_SUPPORT.md` ("Reference slots"); design em
  `docs/superpowers/specs/2026-08-20-ref-slot-invariant-design.md`.

## [0.9.1] - 2026-08-20

### Changed (BREAKING) — campo/índice `ref T` nulo passado a parâmetro `ref T` encaminha `null`

- Um argumento cujo tipo estático já é `ref T` — campo de struct, elemento
  de `(ref T)[]`, valor de `map[K, ref T]` — é **encaminhado como está**,
  inclusive quando contém `null`, exatamente como uma variável `ref T` já
  era (spec §2.3 regra 2 e §4.2: a conversão contextual existe para
  expressões de tipo `T`, não `ref T`). Antes, `OP_CONTEXT_REF_PROPERTY` e
  `OP_CONTEXT_REF_INDEX` fabricavam uma ref para o *slot* quando ele
  continha `null`: dentro da função `n == null` era `false` para um campo
  nulo (uma ref válida para um slot que contém null), enquanto o mesmo
  `a.proximo` lido em `let`/atribuição dava `null`; e um `_append` que
  recursa por `_append(node.proximo, v)` morria com "contextual property
  reference base must be an instance" em vez de uma mensagem sobre ref
  nula. Chave ausente em `map[K, ref T]` encaminha `null`, igual à leitura
  plana `m[k]`.

- **O padrão fill-null-slot deixa de existir.** `*node == null` /
  `*node = Node(v, null)` sobre um slot recebido contextualmente preenchia
  o campo `ref Node` com um `Node` *cru* (um `let viz: ref Node = no.proximo`
  seguido de `*viz = ...` falhava com "expected reference value, got
  object"). Agora o `null` chega como ref nula e `*node = ...` é o erro
  claro `cannot update null reference`. Migração: a função recebe o **pai**
  e liga pelo campo —
  `if node.proximo == null then let novo: Node = Node(v, null)`
  `node.proximo = ref novo` (o nó novo é uma variável porque `ref` exige
  L-value; o compilador o promove para a heap). Para elemento de
  `(ref T)[]` e valor de `map[K, ref T]` a regra é a mesma: quem preenche é
  o dono (`lista[0] = ref novo`, `m["k"] = ref novo`); `*param = ...` sobre
  o `null` encaminhado é `cannot update null reference` tanto para elemento
  nulo quanto para chave ausente. No repositório isso alcançou
  `noxy_examples/linked_list.nx` (migrado) e os testes
  `TestReferenceFieldArgumentCanFillNullSlot` /
  `TestContextualReferenceCallsCanFillNullIndexSlots` (reescritos para a
  semântica nova). Travessias (`cur != null`, `no.proximo != null`) não
  mudam; `bst.nx` trocou `*node == null` por `node == null` (a pergunta
  certa para um parâmetro `ref` que recebe um campo `ref`; `binary_tree.nx`
  usa campos `Node` por valor, onde o parâmetro é uma ref para o slot e
  `*node != null` segue sendo a pergunta certa). O `_append` de
  `stack.nx`, que grava `Node` cru via base `ref` (lacuna da #50), segue
  funcionando pelo shim descrito abaixo.

- **Vale para toda posição que recebe a referência contextualmente**, não
  só argumento de função script: `ref a.campo` explícito, argumento de
  construtor para campo `ref` (`Node(2, a.proximo)`), `return ref n.campo`,
  `append(lista, a.campo)` em `(ref T)[]` e o alvo de `json_loads`. Em todas,
  um `ref T` armazenado como `null` chega como `null` — antes chegava como
  ref para o slot de origem (em `Node(2, a.proximo)`, por exemplo, o nó novo
  ficava *apontando para o slot* `a.proximo`, um alias acidental).

- **`json_loads` com alvo `ref T` nulo vindo de campo/índice** (ex.:
  `json_loads(s, h.child)` com `child: ref any` nulo) passa a devolver
  `false` sem preencher nada — o alvo chega como `null` e não há slot por
  trás; `OP_MARK_REF_JSON_DYNAMIC` deixa o `null` passar (antes, sem o
  passthrough, seria erro de runtime). Antes do encaminhamento o slot era
  preenchido com o payload cru. Para preencher, passe o **dono**
  (`json_loads(s, h)` com schema do struct) ou pré-aponte o slot; a decisão
  definitiva sobre slot `ref T` nulo em `json_loads` fica na #50 (Parte 3).

- **Atenção na migração dentro de laços com `break`** (issue #52,
  pré-existente): `break` não fecha os upvalues dos locais do corpo do laço,
  então `let novo: Node = ...; campo = ref novo` seguido de `break` deixa a
  ref apontando para um slot de pilha reaproveitado. Até a #52, prefira a
  forma recursiva do `_append`, ou deixe o laço terminar pela condição em
  vez de `break` quando houver `ref` a um local do corpo.

- Inalterado, e registrado como pendência (issue #50): um valor referente
  **cru** num slot `ref T` — alcançável por `json_loads` com payload
  compatível e por `campo = T` através de uma base `ref`, que o compilador
  hoje não rejeita (via base valor é erro "cannot assign T to ref T") —
  segue sendo embrulhado numa ref para o slot ao ser passado adiante, como
  antes. A #50 fecha a checagem de campo com base `ref`, decide o
  `json_loads` e remove esse shim.

- Testes: `internal/vm/ref_null_forwarding_test.go` (campo, campo via base
  `ref`, índice, chave ausente de map, guarda do caso não-nulo). Spec §4.2
  ganha o parágrafo sobre encaminhamento de `ref T` (inclusive `null`) com o
  `append_node` idiomático.

### Docs

- README ganha badge de versão no topo (`noxy | 0.9.0`, shields.io estático,
  linkando para este CHANGELOG). Estático de propósito: as tags git (v1.x) e
  a release do GitHub (v0.1.0) não acompanham `internal/version/version.go`,
  então um badge dinâmico mostraria o número errado — bumpar o badge junto
  com `version.go` a cada release.

## [0.9.0] - 2026-08-20

### Changed (BREAKING) — redeclarar `let` no mesmo escopo é erro de compilação

- Um segundo `let` com o mesmo nome no mesmo escopo criava silenciosamente um
  binding novo — inclusive com outro tipo, furando a regra da §2.0 ("o tipo é
  definido na declaração e não pode mudar"). Agora é erro de compilação no
  molde do Go, apontando a declaração anterior (`variable 'x' redeclared in
  this scope (previous declaration at line N)`) e com hint sugerindo a
  atribuição. Reatribuição (`x = valor`) segue como o caminho para atualizar
  o valor; shadowing em escopo interno (bloco, corpo sobre parâmetro,
  variável de `for`) continua permitido. O REPL segue a mesma regra: a
  sessão se comporta como um arquivo digitado linha a linha, então re-`let`
  de um nome de linha anterior é rejeitado (`previously declared in this
  session`) — e uma linha rejeitada não queima o nome.

### Docs

- Spec §3 documenta redeclaração × reatribuição e shadowing em escopos
  internos; a seção do `for ... in` documenta que a variável de loop é
  escopada ao loop e re-vinculada da coleção a cada iteração (atribuir a ela
  no corpo não afeta a sequência).

## [0.8.0] - 2026-08-19

### Added — `type`, target typing em `return` (issue #44)

- Novo nativo global `type(v: any) -> string` devolve o nome do tipo em
  runtime (`"int"`, `"map"`, `"Caixa<int>"`, `"ref"`, ...). Uso principal:
  inspecionar `any` nas fronteiras dinâmicas (envelopes de
  `call_result`/`task_await`, JSON, payloads de canal). A tabela de nomes é
  única e compartilhada com o verbo `%T` do `fmt` — e com isso o `%T` mudou
  em dois pontos: instância de struct genérico imprime o nome de exibição
  (`Caixa<int>`; antes vazava a identidade interna `main::Caixa<int>`,
  inclusive nos argumentos aninhados) e ref/channel/waitgroup deixam de
  imprimir `"unknown"`.
- Target typing em posição de `return`: a anotação de retorno da função
  envolvente ancora o `T` que só aparece no retorno do template chamado
  (`return vazia()` numa função `-> int[]`), simétrico à âncora do `let`
  anotado. Argumentos continuam sendo a âncora primária; construtor de
  struct genérico também é coberto (`return Stack([])` com `-> Stack<int>`).

### Fixed (BREAKING) — literal int fora da faixa é erro de compilação

- Literal inteiro fora da faixa de int64 (decimal, hex ou binário) era
  saturado silenciosamente para o máximo — `9223372036854775808` compilava
  valendo `...807` — e agora é `SyntaxError` com posição. O menos unário
  diretamente sobre um literal funde o sinal no literal, então o mínimo do
  tipo (`-9223372036854775808`) passou a ser escrevível e exato (antes
  valia `-...807`, um off-by-one silencioso). O tamanho em anotação de
  array (`int[N]`) passa pela mesma validação (antes um `N` estourado
  virava silenciosamente array sem tamanho).
- O hint de target typing não é mais armado quando um local sombreia o nome
  do template: ele vazava para a primeira chamada genérica aninhada nos
  argumentos, aceitando programa que deveria ser erro de inferência
  (pré-existente no caminho do `let`; o guard endurece `let` e `return`).
- `zeros(n)` com tamanho negativo panicava no lado Go (`makeslice: len out
  of range`), imprimia stack trace do interpretador e o processo saía com
  código 0. Agora é erro de runtime do noxy ("zeros size must be
  non-negative"), com linha do script e capturável por `call_result` — e um
  panic que ainda alcance o recover do topo passa a terminar o processo com
  código 1, nunca mais como sucesso.

### Docs

- Spec: `strings.char_code`/`from_char_code`/`codes` documentados — o
  `ord`/`chr` pedido na issue #44 já existia, a lacuna era de documentação.
  Regra de faixa de literais inteiros em §2.1, âncora de `return` em §6.2,
  tabela de nomes de `type()`/`%T` em §10, e correção da afirmação falsa de
  que não havia escape `\u` (o lexer implementa `\u{...}` e `\uXXXX`).

## [0.7.2] - 2026-08-19

### Added — `call_result`: fronteira síncrona de erro

- Novo nativo global `call_result(fn, ...args)` converte uma falha de runtime
  que desenrola de `fn` em valor: envelope `{ok, value, failure}` com
  `failure = {kind, message, stack, causes}` — o mesmo vocabulário da
  fronteira de task, estendido com as falhas de defer agregadas (`causes`,
  ordem LIFO, localização de registro no stack). Misuse (não-callable,
  aridade/modos/campos errados onde há metadata) levanta síncrono no
  chamador. Panics de Go viram `kind="panic"`; fatais do runtime Go seguem
  fatais. Sem rollback: mutações via `ref`/globais/upvalues permanecem.
- Novo módulo stdlib `errors` com os shapes `Failure` e `CallResult`
  (fisicamente o envelope é um map na fronteira dinâmica, como `IntResult`).
- O validador de tipo em runtime passou a aceitar, na fronteira dinâmica, um
  map estruturalmente compatível onde um struct é esperado (todo campo do
  schema precisa existir no map com tipo recursivamente compatível) — sem
  isso `let r: CallResult = call_result(...)` não tiparia: `CallResult`
  aninha `failure: Failure`, que por sua vez tem `causes: Failure[]`, campo
  composto que o precedente `IntResult` nunca tinha e por isso nunca
  exercitou esse caminho do validador.
- Gêmeos `_result` agora são escrevíveis em noxy puro (ver
  `noxy_examples/result_pattern.nx`).

### Changed

- `to_int`/`to_float`: o sufixo "; use to_int_result to handle failure" saiu
  da mensagem de erro (agora limpa e capturável) e virou `hint:` impresso
  apenas na saída fatal do topo.

### Deprecated

- Módulo `result` (`use result`): substituído pela convenção `_result` +
  módulo `errors`. Remoção na próxima release.

## [0.7.1] - 2026-08-19

### Fixed (BREAKING) — Igualdade estrita de ref: `==`/`!=` nunca dereferencia implicitamente

- **O caso misto `ref` vs valor virou erro de compilação com hint.** Completa
  a regra iniciada pela correção de identidade abaixo: em `==`/`!=` um
  operando `ref` nunca é dereferenciado implicitamente. O `=` já recusava
  conversão implícita de ref nas duas direções pelo mesmo motivo — o
  significado deve ser evidente na sintaxe, nunca decidido por tipos que não
  aparecem no código. Antes, `x == y` perguntava identidade ou valor
  dependendo dos tipos estáticos dos dois lados; agora cada pergunta tem sua
  sintaxe:

  ```noxy
  ra == rb     // identidade de slot
  ra == null   // o próprio ref é nulo?
  *ra == 1     // valor apontado, explícito
  ra == 1      // ERRO: cannot compare ref int with int: a ref is never
               //       implicitly dereferenced in '=='
               //   hint: use '*ra' to compare the referenced value
  ```

- **A ambiguidade do null foi resolvida de graça pela mesma regra**: um ref
  VÁLIDO apontando para um slot que contém `null` não é mais "igual a null"
  — `r == null` pergunta sobre o próprio ref e `*r == null` sobre o valor
  apontado, duas perguntas que o deref implícito tornava indistinguíveis. O
  padrão comum `no.proximo != null` continua funcionando idêntico (o
  terminador é um ref nulo de verdade).

- Em runtime, `OP_EQUAL` deixou de resolver o caso misto: na fronteira
  dinâmica (ex.: campo `ref` lido via membro de `any`), ref vs valor é
  simplesmente diferente (`false`), e ref vs ref segue por identidade.

- Migração: comparações mistas quebram **em compilação**, com o hint
  apontando o conserto (`*r`). Código que usava `r == null` para perguntar
  "o slot apontado está vazio?" (padrão fill-null-slot) migra para
  `*r == null` — no repositório, isso alcançou exatamente 4 dos 170
  exemplos (`bst.nx`, `binary_tree.nx`, `linked_list.nx`,
  `test_explicit_deref.nx`), migrados nesta release; travessias com
  `cur != null`/`no.proximo != null` não mudam. Spec atualizada: §2.2
  (regra 7) e §2.3 (exceção 1 reescrita, com o par
  `r == null`/`*r == null`). Testes:
  `internal/compiler/ref_equality_strict_test.go`,
  `internal/vm/ref_equality_strict_runtime_test.go` e as suítes de
  semântica em `noxy_examples/`.

### Fixed (BREAKING) — `==`/`!=` entre dois `ref` compara identidade

- **`ra == rb` com ambos os lados `ref T` agora compara identidade de slot**,
  não o valor apontado — a semântica que a spec já descrevia em §2.2 regra 7
  (*"`ref` values compare by slot identity and are not dereferenced"*) e que
  a VM já implementava corretamente em `valuesEqual`
  (`internal/vm/stack.go`), com teste unitário próprio
  (`TestRefEqualityBySlotIdentity`). O bug estava um andar acima: o
  compilador emitia `OP_DEREF` nos **dois** operandos de qualquer operador
  binário, inclusive `==`/`!=`, então a comparação de identidade era
  inalcançável a partir de código Noxy. Na prática, dois refs para variáveis
  distintas que por acaso guardassem o mesmo valor davam `true`, e a
  igualdade passava a seguir o conteúdo das variáveis ao longo do tempo.

  ```noxy
  let a: int = 1
  let b: int = 1
  let ra: ref int = ref a
  let rb: ref int = ref b

  ra == rb    // antes: true   agora: false  (slots distintos)
  ```

  O caso **misto** (ref contra não-ref) chegou a ser preservado com
  auto-deref em runtime, mas ainda nesta release a regra foi completada e
  ele passou a ser rejeitado em compilação — ver "Igualdade estrita de ref"
  abaixo, que descreve a semântica final: `no.proximo != null` continua
  intacto (nulidade do próprio ref), e `contador == 10` migra para
  `*contador == 10`.

  Migração: código que dependia de `ref == ref` como comparação de valor deve
  dereferenciar explicitamente um dos lados (`*ra == *rb`) ou comparar contra
  o valor (`ra == b`). Spec atualizada em §2.2 (regra 7) e §2.3, que agora
  registra `==`/`!=` entre dois refs como exceção ao auto-deref (ao lado da
  segunda exceção, explicitada nesta mesma release — ver "Hint de deref na
  atribuição" abaixo).

### Added

- `noxy_examples/language_semantics_test.nx`: suíte de testes unitários de
  **semântica** da linguagem (134 asserções em 12 grupos — aritmética,
  curto-circuito, semântica de valor/CoW, `ref`, igualdade estrutural,
  strings/bytes, closures, funções de primeira classe, `defer`, genéricos,
  coleções, controle de fluxo). Diferente dos demais exemplos, que provam
  apenas que o programa executa sem erro, cada asserção afirma um
  comportamento observável e o arquivo sai com código 1 quando alguma falha,
  reportando todas de uma vez. Entra automaticamente no
  `run_all_tests_concurrent.nx`.

- **Ordenação lexicográfica de strings**: `<`, `>`, `<=`, `>=` agora aceitam
  duas strings, comparando byte a byte — dentro do invariante UTF-8, isso é
  idêntico à ordem por code point, como em Python. Antes, `"abc" < "abd"`
  compilava e estourava em **runtime** com `operands must be numbers`, embora
  a spec listasse os operadores de comparação sem restringi-los a números.
  Misturar string com número, ou ordenar `bytes`, continua erro de runtime —
  agora com a mensagem `operands must be numbers or strings`; `bytes` seguem
  deliberadamente fora da ordenação (a ponte explícita é `to_str`). `ref
  string` participa pelo valor apontado (auto-deref de expressão, já emitido
  pelo compilador). Spec atualizada em §8 (Comparison) e §12 (comparação
  byte-exata). Testes: `internal/vm/string_ordering_test.go` e o grupo
  "ordenacao de strings" da suíte de semântica.

- `noxy_examples/language_semantics_test2.nx`: parte 2 da suíte de semântica
  (131 asserções em 12 grupos — conversões numéricas, structs de resultado do
  `convert`, `fmt`, as três formas de import, stdlib `strings` por code
  point, ordenação de strings, `bytes` por octeto, arrays fixos e containers
  aninhados, listas ligadas com `ref Node`/`GNode<T>`, `ref` avançado
  (entrada de map, forwarding, escape de frame, closures compartilhando
  slot, `defer` com `ref`), `when`/`case` e a fronteira `any`). Mesmo
  contrato da parte 1: cada asserção afirma comportamento observável e o
  arquivo sai com código 1 em falha. Entra automaticamente no
  `run_all_tests_concurrent.nx`.

### Changed

- **Hint de deref na atribuição `x = r`**: atribuir um `ref T` a um alvo que
  espera `T` (variável local, global ou capturada, índice de array, valor de
  map, campo de struct) continua erro de compilação — atribuição não faz
  auto-deref, agora explicitado na spec (§2.3, exceção 2, com linha nova na
  tabela de Type-Based Assignment) — mas a mensagem passa a apontar o
  conserto: `hint: use '*r' to read the referenced value`, espelhando o hint
  já existente da direção inversa (`r = 50` → `use '*r = ...' to update the
  referenced value`). O hint só aparece quando o deref de fato consertaria o
  programa. A spec também corrige o exemplo `*r = ref z` de "Strict Type
  Safety", que documentava um erro que a implementação nunca emitiu: no alvo
  `*r =` o `*` já desfez a ambiguidade update/rebind, então um RHS `ref` é
  lido como em qualquer expressão (`*r = s` equivale a `*r = *s`) —
  comportamento agora fixado em teste na suíte de semântica. Testes:
  `internal/compiler/assign_deref_hint_test.go`.

## [0.7.0] - 2026-08-18

### Added

- **Genéricos por monomorfização**: funções e structs no top level podem
  declarar parâmetros de tipo entre `<>` (`func first<T>(arr: T[]) -> T`,
  `struct Stack<T>`), usáveis em qualquer posição de tipo (parâmetros,
  retorno, campos, corpo). Toda instanciação é por **inferência a partir do
  uso** — não existe sintaxe de instanciação explícita em posição de
  expressão (`first<int>(x)` não existe), o que mantém `<`/`>` sem
  ambiguidade com os operadores de comparação. Cada instanciação
  (`Stack<int>`, `Stack<string>`) é um tipo/função nominal **distinto**,
  monomorfizado em tempo de compilação: o bytecode gerado é idêntico ao de
  código especializado escrito à mão (provado por teste de igualdade de
  opcodes), sem overhead de runtime e **nenhuma mudança na VM**. Funções
  genéricas também circulam como valores de primeira classe via
  target-typing — anotação de `let`, retorno declarado, elemento de array,
  campo de struct, argumento de chamada, com unificação bidirecional quando
  o argumento também é genérico (`aplica(nums, identity)`). Cross-módulo,
  templates são importáveis via `select`/`select *` (dependências do corpo
  do template precisam estar visíveis no importador); acessar um template
  pelo namespace (`use m` seguido de `m.f(...)`) é erro de compilação
  dedicado. V1 não tem constraints (`T` é irrestrito; o corpo é checado por
  instanciação), é restrita ao top level (sem genérico aninhado em função) e
  não permite `T` bindar um tipo `ref` (idioma: declarar o parâmetro como
  `ref T`). Documentado em `docs/NOXY_LANGUAGE_SPEC.md` §6; spec de design em
  `docs/superpowers/specs/2026-08-18-generics-design.md`.

  - **Limitação documentada (v1)**: `print`/`%T` de uma instância de struct
    genérico mostra o nome **qualificado** (`<main::Caixa<int> instance>`),
    não o nome de exibição (`<Caixa instance>`) — vazamento cosmético
    aceito, `value.go` usa `Struct.Name` sem distinguir display name de
    identidade interna. Comportamento de v1 documentado, não um bug.

- Módulo `collections` (`noxy_examples/collections.nx`), escrito em Noxy
  puro usando os genéricos novos: `map_arr<A, B>`, `filter<T>`, `reduce<T,
  R>`, `contains_val<T>` — a mesma classe de abstração que antes só existia
  como builtin (`append`/`length`/`contains`), agora escrevível em código de
  usuário. (`map` é palavra reservada de tipo — `map[K, V]` — por isso a
  função de transformação chama-se `map_arr`, não `map`.)

### Changed (BREAKING) — imports tipados

Nomes importados (`use m select ...`/`select *`) passam a carregar o **tipo
declarado** dos exports em vez de entrar apagados (`nil`) no compilador —
pré-requisito para a inferência de tipo genérico funcionar sobre dados e
funções vindos de outro módulo (`primeiro(numeros_importado)`).

- **Código cross-módulo dinâmico que hoje compila com tipo apagado pode
  passar a falhar em compile-time.** Um argumento cujo tipo estático era
  `nil` (permissivo por padrão) agora carrega um tipo real — inclusive `any`
  explícito, que a checagem **estrita** de argumento de chamada (assinatura
  exata) rejeita onde antes não havia checagem nenhuma. Migração: anotar o
  valor num `let` intermediário com tipo concreto antes de passá-lo a um
  parâmetro de assinatura exata (a checagem de `let` é permissiva e emite
  guarda de tipo em runtime), ou corrigir o erro de tipo latente que a
  checagem revelou.
- Um exemplo do corpus foi corrigido por essa via:
  `noxy_examples/mergesort_with_slice.nx` passava o retorno de
  `array_utils.slice` (declarado `any`, curry dinâmico) direto como
  argumento de `merge_sort(arr: int[])`; o fix introduz `let left_slice:
  int[] = slice(...)` antes da chamada, em vez de mudar `array_utils.nx`
  (stdlib empacotada com o compilador, fora do escopo de um fix de corpus).
- Critério de aceite: o corpus `.nx` existente inteiro (167 arquivos)
  continua compilando e passando após o fix acima —
  `noxy_examples/run_all_tests_concurrent.nx` reporta 167/167.

### Changed

- A variável de `for ... in` passa a receber o **tipo estático do elemento**
  da coleção quando ele é conhecido (array → tipo do elemento; map → tipo da
  chave, que é o que o laço produz). Antes ela entrava sempre sem tipo.
  Requisito dos genéricos — sem isso, `identity(v)` dentro de um for-each
  chega à unificação sem âncora e `T` fica sem binding —, mas o efeito é
  geral: **pode revelar erros de tipo latentes** em código que hoje compila,
  mesma classe (e mesma migração) da mudança de imports tipados acima.
  Coleção de tipo desconhecido continua produzindo variável sem tipo.

### Fixed

- **REPL preserva structs declarados entre linhas.** Cada linha recebia um
  mapa de structs novo, então um `struct Point ... end` digitado numa linha
  simplesmente não existia na linha seguinte. Bug **pré-existente** (não
  introduzido pelos genéricos), corrigido junto porque a mesma linha de
  código passou a persistir também o registry de templates genéricos da
  sessão (`cmd/noxy/main.go`, spec §5).

- Operadores aritméticos (`+`, `-`, `*`, `/`, `%`) sobre structs agora são
  erro de **compilação**, não mais crash de runtime. Antes, `a + b` com
  `a`/`b` struct compilava silenciosamente e só estourava em runtime
  (`operands must be numbers, strings or bytes`, `internal/vm/executor.go`)
  quando a linha executava — inclusive dentro do corpo de uma instância
  genérica monomorfizada, onde o erro escaparia por completo da cadeia de
  instanciação (que só envolve erros de compilação). Nenhum programa válido
  existente depende desse comportamento — a VM sempre crashava nesses casos
  — então nenhum programa quebra; a checagem nova (`internal/compiler`)
  só torna o erro visível mais cedo, com a linha do próprio operador.

## [0.6.0] - 2026-08-18

### Added

- Suíte de benchmark cross-runtime em `benchmarks/cross_runtime/`, comparando o
  VM com CPython 3.13, Lua 5.4 e Go nativo na mesma carga. Sete benches
  (`startup`, `loop_arith`, `map_churn`, `mandelbrot`, `string_ops`,
  `bubblesort`, `fib`) escritos em Noxy e Python; `startup`, `loop_arith` e
  `fib` também em Lua e Go, como calibração — o Lua é o comparável direto
  (bytecode puro, sem JIT, sem inline cache) e o Go é o teto do hospedeiro.
  Cada implementação imprime a mesma linha `CHECKSUM:` e o harness
  (`run_cross_runtime.ps1`) aborta se divergirem, então a comparação é sempre
  da mesma carga. Medição intercalada entre runtimes, mínimo de N execuções em
  vez de mediana (sob carga a distribuição só tem cauda à direita) e cópia dos
  fontes para disco local — medir de dentro do repo, que fica em OneDrive,
  inflava os tempos em ~2x por filtro de sync e antivírus no read. Resultado em
  `benchmarks/cross_runtime/results/`: descontado o piso de processo, o Noxy
  está 1,8x a 9,6x atrás do CPython e ~14x a ~15x atrás do Lua, com o custo
  concentrado em chamada de função, acesso indexado a array e operação de
  string; o despacho de bytecode puro fica a 1,8x do CPython e o startup ganha
  dele (63ms contra 94ms). O ranking se repetiu em cinco rodadas; as
  magnitudes variam, porque o número líquido amplifica ruído do piso de
  processo. Não altera comportamento da linguagem.

- Flags `--cpuprofile`/`--memprofile` na CLI (`cmd/noxy/main.go`), para
  gravar profile de CPU e de heap do programa Noxy em execução — parte da
  infraestrutura de medição da fase 1 de perf de dispatch/chamadas (ver
  seção Performance abaixo).

### Performance

- Fase 1 de perf de dispatch e chamadas (branch `perf/vm-dispatch-fase1`,
  spec `docs/superpowers/specs/2026-08-17-vm-perf-static-typing-research.md`).
  `OP_CALL_STATIC` (`internal/vm/calls.go`) pula `validateParameterModes` em
  chamadas de closure cujo call site o compilador provou estaticamente
  (`isExact`, `internal/compiler/compiler.go`); o `CallFrame` de cada chamada
  passou a reusar um array de valores em vez de alocar um novo a cada
  chamada (allocs/op de uma chamada típica: 1012 → 10). Mais 13 opcodes
  especializados de despacho, todos por APPEND ao fim do bloco de constantes
  de `internal/chunk/chunk.go`: seis pares comparação+salto fundidos para
  inteiro (`OP_JUMP_IF_LT_INT`/`LE`/`GT`/`GE`/`EQ`/`NE_INT`),
  `OP_INC_LOCAL_INT` (funde `i = i +- K` numa soma direta no slot, sem
  tráfego de pilha) e seis opcodes `_FLOAT` espelhando os `_INT` de
  aritmética e comparação quando os dois lados são estaticamente float.

  Medido nos três benches alvo (`benchmarks/cross_runtime/`, mínimo de 9
  execuções intercaladas): `fib` 804,9→380,8ms (**~2,1x**), `loop_arith`
  519,4→319,4ms (**~1,63x**), `mandelbrot` 428,6→246,2ms (**~1,74x**) —
  todos acima da estimativa por task. No comparativo cross-runtime contra
  CPython 3.13, `fib` foi de 7,9x atrás para 3,4x, e `loop_arith` de 1,8x
  para ~1,1x (gap praticamente fechado). RC intocado — nenhum funil de
  retain/release muda de lugar ou de contagem; os opcodes novos só operam
  sobre escalares (int/float), que não participam de RC.
  `go vet ./...` limpo, `go test ./...` verde, `-race` verde em
  `internal/vm` e `internal/value`, corpus de exemplos 164/164 idêntico.

  **Regressão aceita e rastreada, não corrigida nesta fase:**
  `bench_share_mutate` (pior caso de CoW por construção — `let b = a`
  seguido de mutação de um array grande) piorou em média **+8,7%** numa
  bissecção dedicada (3 sessões intercaladas, piso de ruído medido em
  ~1,2-1,4%, então o efeito é real e reproduzível, não ruído). O reuso de
  `CallFrame` é o maior contribuinte isolado, mas não fecha a conta sozinho —
  o padrão é compatível com pressão de GC difusa espalhada por vários
  commits da fase, e nenhum símbolo da fase 1 aparece no profile do bench.
  Detalhes completos: `benchmarks/RESULTS.md`, seção "develop (f107508) ×
  fase 1 de dispatch e chamadas".

### Fixed

- `break` dentro de `for ... in` voltou a sair do laço. O branch do
  `ForStatement` em `internal/compiler/compiler.go` empilhava o laço em
  `c.loops` mas nunca fazia o pareamento que o `while` já fazia: não patchava
  os `loop.BreakJumps` nem desempilhava o laço no fim. O jump do `break`
  ficava com o operando de placeholder (`0xffff`), o `ip` saltava para fora do
  chunk e o laço principal do executor (`internal/vm/executor.go`, guarda
  `ip >= len(c.Code)`) retornava sucesso — o programa terminava em silêncio,
  com exit 0 e saída truncada, em vez de continuar depois do laço. Como o laço
  também nunca era desempilhado, um `break` posterior mirava o `for` já
  encerrado (mesmo jump nunca patchado) e `break` fora de qualquer laço deixava
  de ser erro de compilação. Bug pré-existente (reproduz em 0.4.0 e 0.5.0),
  descoberto na verificação final do PR #33. `#compiler` @estevaofon

- `if cond then break end` na mesma linha voltou a fechar no `end` correto.
  `parseBreakStatement` (`internal/parser/parser.go`) avançava um token depois
  do `break`, violando o contrato dos laços de statement
  (`ParseProgram`/`parseBlockStatement`/`parseCaseBody` esperam `curToken` no
  último token do statement e fazem o `nextToken` eles mesmos). Na forma
  multilinha o `NEWLINE` absorvia o excesso e o bug não aparecia; na forma
  inline o `end` do `if` era engolido e o bloco só terminava no `end` do laço
  externo — `SyntaxError: expected 'end' after for loop, found EOF`. Afetava
  também `while` (o idioma usado nos exemplos de `docs/concurrency.md`).
  `#parser` @estevaofon

## [0.5.0] - 2026-08-17

### Performance

- Validação de tipos em runtime passou a confiar na tag `RuntimeType` em O(1)
  (`internal/vm/runtime_type_validation.go`). Antes, toda chamada com
  assinatura estaticamente conhecida varria o contêiner inteiro do argumento a
  cada chamada — duas vezes (prova + aplicação), inclusive através de `ref` e
  em funções que só leem — o que tornava O(N²) qualquer laço quente que
  passasse um map/array grande para função tipada do mesmo módulo. Era o custo
  que mascarava o ganho do `ref` no NoxyDB: corrigido o CoW, o laço de puts
  continuava quadrático pela varredura. Agora, tag presente e aceita vale como
  prova (os elementos foram validados quando a tag foi gravada e as escritas
  tipadas validam na entrada); a primeira marcação (tag ausente) continua
  varrendo tudo antes de gravar a tag, e conflito de tag continua rejeitado.
  Medido no repro (struct→struct→map, helper `ref` chamado antes de cada put):
  N=4000 caiu de 6.715ms para 157ms — de quadrático para flat. Benchmark novo
  `benchmarks/bench_typed_call_map.nx` ancora o padrão (2.689ms → 145ms a
  N=2500, checksums idênticos); contrato fixado em
  `internal/vm/runtime_type_validation_test.go` (confiança na tag, primeira
  marcação ainda varre, conflito rejeitado).

- Unicidade de arrays/maps/instâncias passou a ser decidida por um contador
  `Owners` de referências duráveis (`internal/value/cow.go`,
  `IsShared = Owners > 1`) no lugar do bit sticky `Shared`, que só ligava e
  nunca desligava — qualquer passagem por valor condenava o contêiner a
  clonar para sempre, mesmo depois de o alias que motivou a marca deixar de
  existir (compartilhamento morto). O clone agora só acontece quando existe
  um segundo dono vivo no momento da mutação; o modelo é o CoW do Swift
  adaptado a bytecode com GC (spec
  `docs/superpowers/specs/2026-08-17-cow-rc-uniqueness-design.md`, fase 1).
  Caso emblemático (NoxyDB): um helper `database_file(db)` chamado por valor
  dentro do laço de puts marcava `db`/`state`/o map de payloads como
  compartilhados para sempre — 3 clones por put, O(N²). Benchmark novo
  `benchmarks/bench_value_call_mutate.nx` ancora o padrão do NoxyDB (helper
  por valor em laço de mutação com map crescendo): **−93,5%** na intercalada
  final (mediana de 9), de quadrático para flat (~1,5s → ~100ms a N=2500); o
  laço de puts por valor caiu de 3 clones/put para O(1) clones no laço
  inteiro (`TestByValueCallLoopClonesO1AfterFlip`: 600 → ≤8 clones em 200
  iterações). Corpus de exemplos 130/130 idêntico em todas as verificações
  (após o flip e após cada round de correção); `go test ./...` verde,
  `-race` verde em `internal/value` e na suíte completa de `internal/vm`
  (contador `Owners` é atômico; o requisito é o mesmo do ARC sob tasks
  paralelas). Custo do bookkeeping: mesmo após a limpeza do bit morto,
  `bench_map_churn` (+10,9%) e `bench_spawn_sum` (+10,4%) seguem acima do
  gate ≤~5% da suíte intercalada — escrita intensa em map paga inc/dec por
  elemento em cada operação, e os laços quentes dos workers de task pagam a
  passagem pelos funis de RC no rebind de locais escalares (Retain/Release
  são no-ops em primitivos; o custo é o funil por iteração, não contagem).
  Aceito e documentado como o preço do RC nesta fase; as válvulas
  apontadas para quando isso for revisitado: drops precisos da fase 2 e
  elisão de pares inc/dec no mesmo bytecode (spec §8, risco 3), mais um
  fast path para stores de valores escalares apontado na investigação da
  fase 1 (fora do texto da spec). Tabela completa e interpretação em
  `benchmarks/RESULTS.md`.

### Changed

- `benchmarks/RESULTS.md` virou registro corrido de comparações (mais recente
  primeiro): ganhou a seção da validação O(1) pela tag (PR #31) com a tabela
  intercalada develop × candidato — `bench_call_light` −97,1%,
  `bench_typed_call_map` −94,3% (mediana de 5 intercaladas), `share_mutate`
  −65,7%, sem regressões — e o "achado colateral" da seção CoW foi anotado
  como resolvido, apontando para a seção nova.

### Added

- Site publicado (`docs/index.html`) ganhou a seção **What's New in v0.4.0**
  (`#whats-new`, com link no menu e badge no hero): seis cartões de migração
  com o antes/depois de cada breaking change da semântica de valor —
  atribuição não aliasa, leitura de contêiner copia, mutação aninhada não
  vaza, `==` estrutural, `spawn` sem exceção de identidade e `append`
  guardando valor independente. Entraram também um cartão de feature
  (*Value Semantics*), um cartão de sintaxe (*Value Semantics & ref*) e uma
  aba de exemplo executável, além de links para spec, `REF_SEMANTICS.md`,
  guia de concorrência e CHANGELOG no rodapé (que apontavam para `#`).
  Todos os números citados nos cartões foram medidos rodando os trechos.

- `docs/SHOWCASE.md`: vitrine dos projetos reais escritos em Noxy, começando
  pelo [NoxyDB](https://github.com/estevaofon/NoxyDB) — banco de dados
  documento-chave/valor persistente escrito inteiramente em Noxy. Cada entrada
  descreve o projeto e mapeia quais áreas da linguagem e da stdlib ele exercita
  (CoW, JSON nativo, `http_server`, concorrência), com um template no fim para
  novos projetos. O arquivo entrou no `exclude:` do Jekyll (`docs/_config.yml`):
  fica no repositório, fora do site publicado.

### Fixed

- Exemplos do site voltaram a compilar na 0.4.0. Quatro dos oito exemplos da
  landing page estavam quebrados — não era regressão da CoW, mas código que
  nunca foi reexecutado depois da semântica de referência: *Binary Tree*
  (`cannot assign Node to ref Node`), *Linked List* (`ref Node(...)` não é
  endereçável), *HashMap* (`...` literal no array e o mesmo `ref` de
  temporário) e *HTTP Server* (`expected ref HttpServer, got object` em
  runtime, faltava `ref server`). O exemplo de *Concurrency* definia `main()`
  sem nunca chamar e imprimia nada, e o cartão *Self-Referencing* repetia o
  `ref` de temporário. Todos foram reescritos no idioma 0.4.0 (campos de
  struct por valor, travessia por `ref`, `ref current.next` para o cursor) e
  verificados executando cada trecho extraído do HTML: dos 27 blocos, os 23
  que são programa completo rodam e os 4 restantes são fragmentos ilustrativos
  de cartão (sem struct/função ao redor, por design).

- Build do GitHub Pages voltou a passar: `superpowers/` entrou no `exclude:`
  do Jekyll (`docs/_config.yml`). Os planos internos em
  `docs/superpowers/plans` contêm literais de struct no estilo Go (`{{...}}`)
  que o Liquid interpretava como variável não terminada, derrubando o build
  (`Liquid::SyntaxError` em `2026-08-14-runtime-defer-unwind.md:308`). Os
  documentos continuam no repositório; apenas saem do site publicado.

- `has_key` e `keys` entraram na allowlist de natives só-leitura do CoW
  (`internal/vm/cow_natives.go`). Sem elas, passar um map para qualquer um dos
  dois o marcava `Shared` e a mutação seguinte clonava a estrutura inteira —
  o padrão ler-antes-de-escrever (o laço normal de banco/cache/índice) ficava
  O(N²). Medido no repro: `has_key`+`put` a N=5000 caiu de 5.807ms para 15ms,
  agora linear e igual à leitura por índice. Regressão ancorada no contador de
  clones (`TestHasKeyThenWriteDoesNotClone`, `TestKeysThenWriteDoesNotClone`),
  com caso negativo garantindo o default conservador para natives fora da
  allowlist (`TestUnlistedNativeStillMarksArgs`).

- Escrita através de `ref` para um nó com exatamente um dono durável agora
  acontece **in-place e é visível** — sob o bit sticky antigo, o bind por
  valor que criava um segundo dono temporário ligava a marca para sempre, e a
  mutação seguinte através do `ref` podia clonar em vez de mutar, perdendo a
  escrita. O teste committado pina o valor correto: **107** (antes: 50 —
  escrita perdida) para o mesmo programa (lista encadeada, escrita via
  `setit(ref n, v)` seguida de escrita via `let u: ref Node = ...;
  u.valor = 77`). A investigação da Task 7 confirmou adicionalmente que o
  comportamento antigo era dependente da forma do vínculo (o próprio
  merge-base já imprimia 107 quando o mesmo alias era escrito só via
  parâmetro `ref`, sem a passagem por valor intermediária) — variantes
  registradas no relatório da task, não na suíte. O resultado correto pelo
  contrato CoW 0.4.0 é 107 em qualquer forma (§2, regra 6: mutação através
  de `ref` é sempre visível). Pinado por
  `TestRefWriteToUniquelyOwnedNodeMutatesInPlace`.

## [0.4.0] - 2026-08-16

### Changed (BREAKING) — Semântica de valor com copy-on-write

Arrays, maps e structs passam a se comportar como **cópias profundas
independentes em qualquer vínculo sem `ref`** — atribuição, chamada, leitura
e escrita de contêiner, canais, `spawn`/`spawn_task` e captura de `defer`.
A implementação é copy-on-write: nada é copiado no vínculo; o clone acontece
lazily, um nível por vez, na primeira mutação de um valor compartilhado.
`ref` vira o único mecanismo de compartilhamento da linguagem. Spec de design:
`docs/superpowers/specs/2026-08-16-cow-value-semantics-design.md`.

- **`let b = a` e `x = y` deixam de aliasar.** Antes, mutar `b` era visível
  em `a`. Migração: quem dependia do aliasing usa `ref`.
- **Ler de contêiner deixa de aliasar** (`let p = arr[0]; p.x = 1` não altera
  mais `arr[0]`). Migração: mutar pelo caminho (`arr[0].x = 1`) ou `ref`.
- **Mutação aninhada via parâmetro não vaza mais.** A antiga cópia rasa
  copiava só o contêiner externo; `f(a)` seguido de `a[0].x = 1` dentro de
  `f` era visível no chamador. Agora o parâmetro é independente em qualquer
  profundidade. Migração: declarar o parâmetro `ref`.
- **`append(dest, item)` guarda um valor independente** — mutar `item` depois
  não altera `dest`. O alvo de `append`/`pop`/`delete` compartilhado é
  clonado antes da mutação (CoW pelo slot).
- **`spawn` perde a exceção de identidade**: seus argumentos seguem a mesma
  semântica de valor de `spawn_task` e chamadas normais. `chan_send` entrega
  valor independente — dados passados por canal ficam livres de race por
  construção. Migração para estado compartilhado: globals coordenados ou
  canais.
- **`==`/`!=` de compostos vira estrutural** (recursivo por conteúdo);
  `[1, 2] == [1, 2]` agora é `true`. Refs comparam por identidade de slot e
  não são dereferenciados. Antes, compostos comparavam por identidade de
  ponteiro — instável através de chamadas. Migração para identidade: comparar
  refs ou uma chave própria.
- **Natives com assinatura mantêm a cópia ansiosa** dos args compostos
  não-ref: o corpo em Go muta fora do copy-on-write do bytecode, e a cópia é
  a única proteção do chamador. Natives sem assinatura marcam os args
  conservadoramente; uma allowlist auditada de natives só-leitura
  (`internal/vm/cow_natives.go`) evita o custo onde é provado desnecessário.
- **Limitação documentada**: um `ref` criado para dentro de um contêiner
  (`ref arr[0]`, campo) fixa a identidade do contêiner na criação (a base é
  unicizada nesse momento). Se o contêiner for copiado DEPOIS, escrita
  através do ref pré-existente ainda é visível pela cópia não materializada.
  Crie refs depois de compartilhar. Ver `docs/REF_SEMANTICS.md` §8.
- **Leniência preservada**: campos tipados `ref T` que recebiam valores
  planos (o checker antigo não validava campos quando a base era ref)
  continuam aceitos, e o caminho de mutação tolera valor plano em slot de
  tipo ref — programas como `noxy_examples/stack.nx` e `linked_list.nx`
  seguem rodando sem alteração.

### Added

- Bit `Shared` atômico em arrays/maps/instâncias + opcodes de caminho de
  mutação (`OP_GET_LOCAL_MUT`, `OP_GET_GLOBAL_MUT`, `OP_GET_UPVALUE_MUT`,
  `OP_GET_INDEX_MUT`, `OP_GET_PROP_MUT`, `OP_DEREF_MUT`, `OP_MARK_SHARED`)
  com lowering de lvalues no compilador (`compileLValueBase`).
- Contador de clones CoW (`vm.CloneCountValue`) para testes e diagnóstico —
  chamadas só-leitura custam 0 clones (coberto por teste).
- Suite de benchmarks em `benchmarks/` com harness (`run_benchmarks.ps1`),
  comparação de corpus (`compare_examples.ps1`) e resultados antes/depois
  commitados (`benchmarks/RESULTS.md`).
- `noxy_examples/shallow_copy.nx` reescrito como demonstração da semântica
  de valor.

## [0.3.0] - 2026-08-16

### Changed (BREAKING)
- **`to_int` e `to_float` levantam erro** em vez de devolver `0` / `0.0` quando
  a conversão é impossível. `to_int("abc")` era indistinguível de
  `to_int("0")`. A forma leniente por `strconv.ParseFloat` também some:
  `to_int("12.75")` devolvia `12` e agora levanta, como qualquer outra string
  decimal. Migração: chamadas sobre entrada não confiável passam a usar
  `to_int_result` / `to_float_result` do módulo `convert`, com ramo explícito
  de falha.
- **`index_of` devolve índice em caractere**, não em byte, alinhado a
  `substring`, `char_at`, `length` e `slice`. Texto ASCII não é afetado.
- **Funções de `strings` recusam `bytes`** e apontam `to_str`. Antes operavam
  sobre a forma de exibição `b"..."`.
- **`ord` devolve o code point** de uma string de um caractere e exige
  exatamente um caractere. Antes devolvia o primeiro byte UTF-8.
- **Toda `string` Noxy contém UTF-8 válido.** `to_str` levanta erro sobre bytes
  inválidos em vez de retaggear sem inspeção. Antes, o byte inválido sobrevivia
  na string mas decodificava como U+FFFD em toda operação por caractere:
  fatiar `h` + `0xFF` + `i` reescrevia três bytes como cinco, em silêncio e sem
  volta. Migração: use `io.read_bytes`, mantenha o valor como `bytes`, ou use
  `strings.is_valid_utf8` antes de decodificar.
- **`io.read`, `io.read_lines` e `sqlite.query`** reportam conteúdo não-UTF-8
  pelos campos `ok` e `error` que já possuíam, em vez de levantar.
  `io.read_bytes` e `net.recv` seguem inalterados como as saídas brutas.
- **`sys.exec_output` e `sys.getenv`** passam a reportar conteúdo não-UTF-8 da
  mesma forma, mas por um campo `error` que **não existia antes** — ver o item
  seguinte sobre a forma dos structs.
- **`SysResult` e `EnvResult` ganham um campo `error: string`** (em
  `internal/stdlib/sys.nx`). `SysResult` passa de
  `(exit_code, output, ok)` para `(exit_code, output, ok, error)` e
  `EnvResult` de `(value, ok)` para `(value, ok, error)`. Qualquer código que
  construa um desses structs posicionalmente precisa passar o campo novo;
  acesso por campo (`r.ok`, `r.output`) não é afetado.
- **Carregar um `.nx` que não seja UTF-8 válido falha** com erro nomeando o
  arquivo, em vez de lexar bytes mal formados.
- **O script de entrada passado na linha de comando não está coberto por este
  invariante.** `cmd/noxy/main.go` lê o arquivo principal por um caminho
  separado do carregamento de módulos; um `.nx` de entrada com bytes
  inválidos ainda é lexado sem checagem. Módulos importados via `use` são
  validados. Esta lacuna fica registrada como trabalho futuro.

### Removed (BREAKING)
- **A palavra-chave `global` foi removida do léxico.** Já não fazia parte da
  sintaxe da linguagem — o parser sempre a rejeitava — mas o lexer ainda a
  reconhecia como palavra reservada, produzindo `invalid syntax "global"` em
  vez do diagnóstico comum de `let` ausente. Declare variáveis de topo com
  `let`; uma função pode reatribuir um `let` de topo normalmente.

### Added

- `defer call(...)` with immediate argument capture and frame-level LIFO cleanup across functions, scripts, modules, loops, and spawned functions.
- Portable positive TCP read, write, and accept timeouts through `net.settimeout`.
- **Módulo `convert`** com `to_int_result`, `to_float_result`, `IntResult` e
  `FloatResult`.
- **`strings.char_code(s)`**, inverso de `from_char_code(code)`.
- **Guards de arquitetura**: nenhum native registrado duas vezes, nenhum marcador
  de debug em fonte de produção, fontes embarcados da stdlib em UTF-8 válido.
- **`strings.is_valid_utf8(b: bytes)`**, o caminho de checar-antes-de-decodificar.
  O parâmetro é estritamente `bytes`: passar uma `string` — inclusive através
  de `use strings select *` — levanta erro de runtime nomeando o tipo
  recebido.
- **`http_server` agora faz framing incremental** (ponto 13 do PR #17): lê o
  bloco de headers e o corpo `Content-Length` até completar, em vez de um
  único `socket_recv`. Ver `docs/HTTP_SERVER.md`.
  - `HttpServer` ganha `max_header_bytes`, `max_body_bytes`,
    `header_timeout_ms`, `body_timeout_ms`, `write_timeout_ms` e
    `read_chunk_bytes`, com defaults documentados e proteção contra
    slowloris por deadline absoluto de cada fase.
  - **`bind_server(server: ref HttpServer) -> bool`** separa o bind do loop de
    accept e escreve a porta real de volta em `server.port`, tornando a porta
    `0` utilizável.
  - Requisições inválidas recebem 400, 408, 413, 414, 431, 501 ou 505 com
    `Content-Length` byte-exato, em vez de desconexão silenciosa.
  - **`count_header(headers, count, name)`** em `http_parser`.
- **Escapes `\uNNNN` e `\u{...}`** em todo literal de string, com o codepoint
  validado — surrogates e valores acima de `0x10FFFF` são rejeitados na
  léxica.
- **Escape `\xNN`** em literais de bytes, para escrever um byte bruto.
  Recusado em literal de string, onde construiria UTF-8 inválido.
- **`strings.codes(s) -> int[]`**, que decodifica a string uma vez e devolve
  todos os codepoints. Um laço com `char_at` redecodifica a string inteira a
  cada chamada e é quadrático no seu tamanho.

### Fixed

- **REPL e `input()` congelavam em terminais com raw mode vazado no Windows.**
  O modo de entrada do console é estado compartilhado do terminal: quando um
  programa em raw mode (ex.: um jogo via `noxy-plugin-terminal`) morria sem
  restaurar, `ENABLE_LINE_INPUT`/`ENABLE_ECHO_INPUT` ficavam desligados e o
  próximo REPL naquele terminal mostrava `>>> ` mas nunca recebia a linha —
  digitação invisível, Enter sem efeito, "resolvia" só abrindo outro terminal
  (o PowerShell mascara o problema porque o PSReadLine redefine o modo a cada
  prompt). Agora `internal/console.EnsureLineInput` normaliza o modo do console
  antes de cada prompt do REPL e de cada `input()`.
- Normal returns and runtime failures now share safe frame unwinding, preserving primary errors while collecting observable cleanup failures.
- `net.setblocking(sock, true)` now restores indefinite blocking, while the deprecated `false` branch remains a compatibility no-op.
- `net.poll`/`net_select` now reports non-consuming readiness through independent 64-entry read, write, and error sets, with immediate zero-time polls, one global positive timeout, portable EOF/hangup projection, and concurrent-close wakeups that omit detached resources.
- `net.listen(host, 0)` now returns the operating-system-assigned port in `Socket.port`, allowing collision-free loopback listeners.
- **`parse_url` cortava host e path no lugar errado** para autoridade com
  caractere não-ASCII: `http://münchen.de/path` devolvia host `münchen.de/` e
  path `path`.
- **Saída de debug embarcada foi removida de quatro pontos.** `net_send`
  imprimia uma linha em stdout para um argumento malformado, e o cliente HTTP
  imprimia uma linha a cada requisição, corrompendo a saída de qualquer
  programa que o usasse. Havia ainda dois resquícios mortos: um comentário
  marcado como debug em `executor.go` e um `printf` de debug comentado em
  `parser.go`. Um guard de arquitetura agora falha o build se qualquer
  marcador voltar.
- **`strings_contains` e `strings_replace` estavam registrados duas vezes**, com
  a segunda cópia inalcançável — uma correção aplicada a ela seria
  silenciosamente descartada.
- **24 linhas de comentário da stdlib**, em `http.nx`, `strings.nx`, `time.nx`
  e `io.nx`, tiveram os acentos restaurados após uma conversão de encoding
  com perda.
- **Vincular um valor `bytes` como parâmetro SQL corrompia o conteúdo.**
  `sqliteParameter` não tinha caso para `bytes` e caía no formato de exibição
  de depuração, `b"..."`, adicionando um prefixo e um sufixo espúrios a todo
  valor gravado. Independente de validade UTF-8. Corrigido para passar o
  conteúdo bruto.
- **Struct com campo de tipo importado nunca compilava.** `use pkg select *`
  vinculava o tipo importado como valor, mas nunca ensinava ao compilador o
  layout de campos desse tipo em outra unidade de compilação. Uma struct
  local com um campo desse tipo — exatamente a forma de
  `HttpServer.listener: Socket` — construía metadado de tipo em runtime
  incompleto, e toda chamada ao construtor levantava
  `struct constructor has incomplete runtime type metadata`,
  incondicionalmente. `new_server()` nunca funcionou antes desta correção.
- **Resolução de global/propriedade/import/closure truncava acima de 255
  constantes no pool.** `OP_GET_GLOBAL`, `OP_SET_GLOBAL`, `OP_GET_PROPERTY`,
  `OP_SET_PROPERTY`, `OP_IMPORT`, `OP_CLOSURE`, `OP_REF_GLOBAL`,
  `OP_REF_PROPERTY` e `OP_CONTEXT_REF_PROPERTY` codificavam o índice no pool
  de constantes em um único byte. Como `AddConstant` nunca deduplica — cada
  literal de string e cada referência a um nome global reivindica seu
  próprio slot — um chunk moderadamente grande já ultrapassa 255 constantes;
  a partir daí o índice `byte(256)` volta para `0` silenciosamente, e a
  instrução lê a constante errada. Observado como
  `undefined global variable 'strings'` num exemplo padrão de 85 linhas, e
  como panic de asserção de tipo em casos mais adversos. Todos os nove
  opcodes agora codificam um índice de 16 bits.
- **`bind_server` reescrevia `server.running` para o mesmo valor já
  presente**, uma escrita desnecessária que corria (`-race`) contra leitura
  concorrente de outro campo da mesma struct no padrão bind-depois-spawn que
  a própria função existe para viabilizar. O loop de accept de `serve()`
  também deixou de reler `server.running` a cada iteração; ele reage à
  falha de `accept()` — já sincronizada no registro de sockets — quando
  `stop_server` fecha o listener de outra goroutine, em vez de repetir a
  leitura de um campo comum de struct, que não é sincronizado entre threads
  como globals e maps.
- **`response_error` declarava contagem de runas como `Content-Length`**,
  subestimando o tamanho de qualquer mensagem não-ASCII.
- **Handler que falhava vazava o socket do cliente.** A conexão agora fecha
  por `defer` em todo caminho de saída, incluindo falha do handler.
- **`get_header` cortava valores contendo `:`**, então
  `Host: example.com:8080` devolvia `example.com`.
- **Escape unicode de quatro dígitos era lexado como texto literal.** Uma
  sequência ANSI de limpar tela escrita como ESC escapado por `\u` saía
  como oito caracteres visíveis. `conway.nx`, `conway_random.nx` e
  `langtons_ant.nx` usavam esse padrão e os três imprimiam o texto de escape
  em vez de limpar a tela.

## [0.2.0] - 2026-08-13

### Added

- Observable strict JSON encoding through `json.dumps_result`, with explicit
  success, payload, and error fields. `#stdlib` @estevaofon
- Experimental external terminal package backed by a Go plugin.
- Complete Space Invaders example using the external terminal package.
- Neo Arcade Space Invaders 2 example with colored terminal sprites, deterministic starfield, smoke validation, and safe interactive terminal cleanup. `#examples` @estevaofon

### Fixed

- Stateful native calls now receive the actively executing VM instead of a VM captured during builtin registration.
- File, network, SQLite database, and SQLite statement handles now have shared ownership and synchronized lifecycle state across VMs sharing one runtime.
- Concurrent requests for the same module now share one successful initialization, with coordinated failure retry and import-cycle detection.
- Global bindings, module exports, maps, registries, and per-resource mutable state no longer expose the migrated concurrent Go-map crash paths.
- Strict JSON encoding now rejects unsupported values, non-finite floats,
  cycles, typed nil containers, and invalid UTF-8 without lossy fallback.
  `#vm` @estevaofon

### Follow-up

- Corrected `net_select` polling semantics remain follow-up work; this foundation only moves the existing buffered state into shared synchronized resources.
- Supervised `spawn` and task values remain follow-up work; this foundation does not change current public spawn behavior.

## [0.1.0] - 2026-08-10

### Added

- Repository-local skill for safe, transactional Noxy version updates by major, minor, patch, or explicit target, with default-minor behavior, dry-run validation, and rollback coverage. `#tooling` @estevaofon

## [1.5.0] - 2026-08-09

### Added

- Typed public native contracts and runtime reference-mode validation.
- Safe references to captured upvalues.

### Fixed

- Reference documentation now distinguishes contextual passing, update, and rebind.
- Dynamic calls reject incompatible reference modes before entering the callee.

## [1.4.0] - 2026-08-08

## Added

- Assinaturas exatas `func(T...) -> R` com verificação de aridade, argumentos, referências e retornos em compile time. `#compiler` @estevaofon

## Changed

- `func` sem assinatura permanece como fronteira dinâmica, enquanto funções nomeadas, closures e upvalues preservam seus tipos exatos. `#compiler` @estevaofon
- Exemplos, biblioteca padrão e especificação foram migrados para a nova sintaxe tipada. `#docs` @estevaofon

## Fixed

- Separação entre `null` e tipos dinâmicos desconhecidos, incluindo validação recursiva de callables e predeclaração de structs. `#compiler` @estevaofon
