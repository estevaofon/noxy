# Changelog

## [Unreleased]

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
