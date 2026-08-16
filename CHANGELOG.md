# Changelog

## [Unreleased]

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
