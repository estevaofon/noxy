# Changelog

## [Unreleased]

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

### Fixed

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
