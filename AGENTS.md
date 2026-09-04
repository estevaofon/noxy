# AGENTS.md — Guia para agentes de IA no Noxy VM

Máquina virtual de bytecode para a linguagem Noxy, em Go (módulo `noxy-vm`,
Go 1.25). Versão corrente: `v0.23.5` (`internal/version/version.go`).

**Fonte da verdade da linguagem: `docs/NOXY_LANGUAGE_SPEC.md`.** Regra de
linguagem vem da spec ou de teste no binário — nunca de um exemplo. Este
arquivo cobre só o que não está lá: layout do código, verificação,
convenções e armadilhas.

## Pipeline e pacotes

```
Source → Lexer → Parser → AST → Compiler → Bytecode (Chunk) → VM
```

| Pacote | Função |
|--------|--------|
| `internal/token`, `internal/lexer`, `internal/ast`, `internal/parser` | Frontend; parser recursivo → AST (`ast.go`, `walk.go`, `clone.go`) |
| `internal/compiler` | AST → bytecode + checagem estática. `compiler.go` + ~30 arquivos por tema (`generics_*.go`, `nullable.go`, `narrowing.go`, `try.go`, `typed_index.go`, `cow_lowering.go`, ...) |
| `internal/chunk` | Opcodes, constantes, `Disassemble` |
| `internal/value` | `Value` (32 B), objetos, RC/CoW (`cow.go`), natives |
| `internal/vm` | `run()` em `executor.go`; pilhas em `stack.go`/`calls.go`/`unwind.go`; `builtins_*.go`; `modules.go`; `cow.go`; `resources.go`; `extensions.go` |
| `internal/stdlib` | Módulos `.nx` embutidos (`//go:embed *.nx`, sem registro) |
| `cmd/noxy` | CLI, REPL (`runREPL`), `diagOut` (destino único dos diagnósticos da CLI) |
| `internal/ext`, `sdk/noxyplugin` | Extensões wasm e por processo (`noxy-plugin/1`); o SDK é módulo Go aninhado, testado à parte (`go test ./...` dentro dele) |
| `internal/pkgmanager`, `internal/lineedit`, `internal/console`, `internal/version`, `internal/plugin` (deprecado, sai na v0.25.0) | Periferia |

## Verificação obrigatória

Da raiz do repositório, após qualquer modificação:

```bash
go build ./... && go vet ./...
go test ./internal/... -count=1
go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx
```

`go run ./cmd/noxy arquivo.nx` roda um exemplo sem binário no PATH. O runner
usa `argv()[0]` e lista só `noxy_examples/*.nx` (sem subdiretórios); exemplo
interativo, de erro proposital ou longo entra na lista `exclusions` de
`should_ignore()`, com o motivo no comentário. `assert` não é builtin: cada
exemplo declara o seu e sai com `exit(1)` — é o exit code que o runner lê.
Regressão pequena vai em teste Go que roda um programa Noxy de string
(`internal/vm/*_test.go`, `cmd/noxy/*_test.go`); programa que **deve** falhar
na compilação vai em `noxy_examples/type_errors/`.

Guardas de arquitetura — testes que leem o código-fonte. Se um quebrar, o
lugar errado foi tocado; não afrouxe o teste:

| Teste | O que trava |
|-------|-------------|
| `internal/vm/architecture_test.go` | Cada `define<Area>Builtins()` no seu `builtins_<area>.go`; teardown de frame só em `unwind.go`; pilhas em `stack.go`/`calls.go`; módulos em `modules.go`; sem maps globais crus no runtime |
| `internal/vm/inline_guard_test.go` | `push()` inlinável em `run()` (custo ≤ 20); `ensureCallCapacity` custa exatamente 80 |
| `internal/vm/builtins_registry_test.go` | Snapshot **ordenado** dos nativos globais |
| `internal/vm/native_signatures_test.go`, `internal/compiler/known_globals_test.go` | Contratos de tipo dos nativos; nomes da stdlib conhecidos do compilador |

Benchmarks em `benchmarks/`: `pwsh -NoProfile -File interleaved_compare.ps1`
(dois binários intercalados — a única comparação que vale), máquina ociosa,
delta só na mesma sessão; `compare_examples.ps1` captura a saída dos exemplos
antes/depois ao mexer no runtime. CI (`.github/workflows/network-deadlines.yml`):
testes Go em ubuntu+windows, `-race` em `internal/vm`, o runner, cross-build
com `CGO_ENABLED=0`.

## Regras que não estão na spec

**Compilador fala primeiro.** Tipo errado, `null` em não-anulável, global
inexistente, redeclaração — erros de compilação. Aviso é `c.warn(msg)`
(`warnings.go`); o compilador nunca escreve em stdout/stderr — quem chama
`Compile` imprime (CLI/REPL em `diagOut`, loader de módulos em `os.Stderr`).
Opcode novo precisa da constante **e** do nome em `String()` (`internal/chunk/chunk.go`).

**Saída.** stdout é do programa (`print`/`iprint`); stderr é do diagnóstico
(`eprint`/`eiprint`, erros, avisos, traces). Nunca `fmt.Print*` para
diagnóstico: `os.Stderr` na VM, `diagOut` na CLI.

**Opcodes** (`switch` de `run()`, `executor.go`). `return vm.runtimeError(c, ip, ...)`
sai de `run()`; o `defer` chama `unwindTo` (roda `defer`s Noxy, libera o RC
dos slots, restaura `stackTop`) — sem limpeza manual no caminho de erro. O
efeito líquido de pilha tem de bater com o que o compilador contabiliza
("stack imbalance"). Em nativos, erro é `runtimeErrorAtCurrentFrame` ou o
`error` devolvido por um `DefineContextualNative`.

**Builtins** (`internal/vm/builtins_<area>.go`, registrados em `define<Area>Builtins()`):
- `vm.DefineNative` (puro) / `vm.DefineContextualNative` (precisa da VM, CoW,
  erro com stack Noxy) / `...WithSignature` — obrigatório com parâmetro `ref`
  (`ParamInfo{IsRef: true}`): é a assinatura que faz o compilador exigir
  `ref x` no call site.
- Valide `len(args)` e `args[i].Type`. `str, ok := val.Obj.(string)`, nunca
  asserção nua; `bytes` é `VAL_BYTES`, não `VAL_OBJ`; `any` não garante tipo
  (`runtime_type_validation.go`).
- Atualize o snapshot de `builtins_registry_test.go`. Retorno fixo de nome
  disponível sem `use` vai em `coreBuiltinReturnTypes`
  (`internal/compiler/builtin_return_types.go`).
- Nativo de módulo é `<mod>_nome` e só é exposto pelo wrapper tipado em
  `internal/stdlib/<mod>.nx`. API que pode falhar devolve `Result<T>` de
  `errors`, não struct ad hoc.
- Resolução de `use`: `$NOXY_PATH` → `noxy_libs/` → `stdlib/` no disco →
  relativo → embutido (um `.nx` local sombreia a stdlib); cache por
  `SharedState` (`module_cache.go`).
- Documente na spec (§10 builtins, §12 stdlib). Ao remover módulo, grep em
  `noxy_examples/`, `*_test.go`, spec e site — a suíte roda os exemplos.

**RC/CoW.** Contêiner devolvido é dono dos filhos: construa com
`value.NewArray`, `NewMapWithData`, `NewInstanceWith` (retêm cada filho
composto). Nunca escreva composto em `inst.Fields[...]`/`ObjMap.Set` cru sem
`value.Retain`; `NewArrayAdopting` só para elementos que **você** já reteve
(comentário `// RC: move`). Retain/release a menos é vazamento, a mais é
double free; rode `cow_*_test.go` e `container_owners_test.go` ao mexer em
qualquer coisa que guarde um composto.

**Pilhas.** `ensureCallCapacity` (`calls.go`) é o único ponto que checa
`FramesMax`/`StackMax` (erro `stack overflow`, nunca panic Go); código novo
vai em `growForCall` (`//go:noinline`), nunca no corpo dela. `push` nunca
cresce a pilha. `vm.frames` realoca ao dobrar: nunca guarde `*CallFrame`
através de chamada Noxy reentrante — reobtenha por índice
(`&vm.frames[vm.frameCount-1]`).

**Recursos** (arquivos, sockets, bancos, statements) vivem nos registries de
`SharedState` (`resources.go`), referenciados por handle inteiro; remova do
registry ao fechar.

**Overflow de `int` não é checado** — dá a volta (spec §8). Não adicione.

**`ref` em exemplos e testes** (spec §2.3): `ref x` em todo call site com
parâmetro `ref T` (builtins inclusos: `append(ref xs, v)`); `*r` lê/escreve;
expressão que já é `ref T` passa sem `ref`; `ref T` nunca é `null`.

**REPL** é um arquivo lido linha a linha, sem carve-outs (re-`let` no mesmo
escopo é erro). REPL "travado" sem eco no Windows = modo raw vazado por outro
programa; `internal/console.EnsureLineInput` conserta.

**Extensões.** wasm (wazero sem WASI) só para computação pura; processo
(`kind = "process"`) é o meio principal para I/O, SO e SDKs: um executável por
plataforma como asset de release, `noxy --get` baixa o da máquina e grava os
hashes de todos em `noxy.sum`. Ver `docs/EXTENSIONS.md` e
`docs/superpowers/specs/2026-08-29-*.md`.

## Debug

`noxy --disassembly arquivo.nx` (bytecode de cada chunk antes de rodar),
`--cpuprofile`, `--memprofile`; em código, `c.Disassemble(...)` /
`c.DisassembleAll(...)`. Trace ad hoc sempre em `os.Stderr`.

## Convenções

- Antes de chamar um comportamento de bug: grep em `CHANGELOG.md`,
  `noxy_examples/` e `*_test.go` — costuma ser decisão registrada. Designs
  datados em `docs/superpowers/specs/`, planos em `docs/superpowers/plans/`.
- Docs por tema em `docs/`: `REF_SEMANTICS.md`, `language_references.md`
  (precedentes das escolhas arquiteturais — leia antes de propor trocar o
  modelo de valor/COW/`ref`), `concurrency.md`,
  `JSON_SUPPORT.md`, `PACKAGE_MANAGER.md`, `EXTENSIONS.md`, `HTTP_SERVER.md`,
  `CRYPTO_MODULE.md`. Todo `docs/*.md` passa pelo Liquid do Pages: `{{`
  literal só dentro de `<!-- {% raw %} -->`.
- Commits/PRs: `tipo(escopo): descrição em português (issue #N)`; branches
  saem de `develop`, `main` recebe releases.
- `CHANGELOG.md` (Keep a Changelog): toda versão tem seção datada; quebra vai
  em `Changed (BREAKING)` com tabela Antes/Agora e migração.
- Versão: patch para correções, minor para mudanças maiores. Um bump toca
  `internal/version/version.go`, `CHANGELOG.md`, `README.md` (banner do
  REPL), spec §12 (`sys.version`), `docs/index.html` (badge e exemplo) e o
  rodapé deste arquivo. Release: título `vX.Y.Z (dd/mm/aaaa)`, push só da tag.
- EOL: checkout Windows é CRLF com índice LF; `gofmt -l` lista o repositório
  inteiro — use `gofmt -d` nos arquivos tocados e confira com
  `git diff --numstat` que nenhum arquivo foi reescrito por completo.
- Zen (README e abertura da spec) é a bússola, não regulamento: erro em
  compile-time > runtime; consistência antes de performance (performance
  nunca muda semântica); quebrar é aceitável antes da 1.0, **com** migração
  no CHANGELOG.

## Checklist de PR

- [ ] `go build ./...`, `go vet ./...`, `go test ./internal/... -count=1`, runner dos exemplos
- [ ] `gofmt -d` limpo nos arquivos tocados; snapshot de `builtins_registry_test.go` se houver nativo novo
- [ ] `CHANGELOG.md` (e versão, se release); spec/README/site quando a mudança é visível ao usuário
- [ ] Exemplo em `noxy_examples/` (excluído do runner se interativo)

---

**Versão**: 1.2 (Noxy VM 0.23.5) — atualizado em 2026-09-04
