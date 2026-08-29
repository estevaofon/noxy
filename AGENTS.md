# AGENTS.md - Guia para Agentes de IA no Projeto Noxy VM

## 🎯 Visão Geral

**Noxy VM** é uma máquina virtual baseada em bytecode para a linguagem Noxy,
escrita em Go (módulo `noxy-vm`, Go 1.25). Versão corrente: `v0.22.0`
(`internal/version/version.go`, `CHANGELOG.md`).

### Características
- Estaticamente tipada — o compilador fala primeiro: tipo errado, `null` em
  tipo não-anulável, global inexistente, parâmetro duplicado e nome redeclarado
  são erros de **compilação**
- Semântica de valor com copy-on-write; compartilhar é só por `ref`, no tipo e
  no call site (spec §2.3, `docs/REF_SEMANTICS.md`)
- Nulidade explícita (`T?`) com narrowing; erros como dado (`Result<T>`, `try`)
- Genéricos (monomorfização), structs, arrays, maps, closures, `defer`,
  concorrência (`spawn`/channels/`when`/tasks supervisionadas)
- Stdlib embutida (`io`, `sys`, `strings`, `json`, `net`, `http`, `sqlite`,
  `crypto`, `time`, `errors`, ...), gerenciador de pacotes (`noxy --get`),
  extensões WASM (experimental) e plugins por processo

### Arquitetura
```
Source Code → Lexer → Parser → AST → Compiler → Bytecode (Chunk) → VM
```

| Camada | Pacote | Função |
|--------|--------|--------|
| **Frontend** | `internal/token` | Tokens |
| | `internal/lexer` | Tokenização |
| | `internal/ast` | Nós da AST (`ast.go`), `walk.go`, `clone.go` |
| | `internal/parser` | Parser recursivo → AST |
| **Compiler** | `internal/compiler` | AST → bytecode + checagem estática. `compiler.go` (~3,8k linhas) + ~30 arquivos por tema: `generics_*.go`, `nullable.go`, `narrowing.go`, `try.go`, `typed_index.go`, `field_index.go`, `let_inference.go`, `known_globals.go`, `builtin_return_types.go`, `warnings.go`, `cow_lowering.go`, `explicit_ref.go`, `borrow_place.go`, ... |
| | `internal/chunk` | Opcodes, constantes, `Disassemble` |
| **Runtime** | `internal/value` | `Value` (32 B), objetos, RC/CoW (`cow.go`), ambiente global, natives |
| | `internal/vm` | Loop de execução (`executor.go`), pilhas (`stack.go`, `calls.go`, `unwind.go`), builtins (`builtins_*.go`), módulos (`modules.go`, `module_cache.go`), CoW (`cow.go`), rede (`network_poller_*.go`), extensões (`extensions.go`) |
| | `internal/stdlib` | Módulos `.nx` embutidos (`//go:embed *.nx`) |
| **Periferia** | `cmd/noxy` | CLI, REPL, flags |
| | `internal/lineedit` | Editor de linha do REPL (tty POSIX) |
| | `internal/console` | Conserta o modo raw vazado no console Windows (`EnsureLineInput`) |
| | `internal/pkgmanager` | `noxy.mod` / `noxy.sum`, `noxy --get` |
| | `internal/ext` | Extensões: wasm (wazero) e por processo (`noxy-plugin/1` sobre stdio); manifesto `noxy_ext.toml`, loader, `Process`, codec de quadros |
| | `internal/plugin` | Plugins JSON legados (`sys_load_plugin`, deprecado — sai na v0.25.0) |
| | `sdk/noxyplugin` | SDK Go para extensões por processo — módulo aninhado `github.com/estevaofon/noxy/sdk/noxyplugin`, sem dependência de `noxy-vm` |
| | `internal/version` | `version.Version` — fonte única de `noxy --version` e `sys.version` |

`internal/vm/vm.go` tem ~200 linhas (tipos, tetos, construtores,
`DefineNative*`). O `switch` de opcodes mora em `internal/vm/executor.go`
(`run()`, ~2k linhas).

---

## 🔧 Regras Fundamentais

### 1. Compatibilidade de Tipos
Noxy é **estaticamente tipada** - nunca quebre a verificação de tipos:
```noxy
let x: int = 42
x = 100       // ✓ OK
x = 3.14      // ✗ ERRO
let y = 42    // tipo inferido do RHS (int) — igualmente estável (spec §3, issue #41)
y = "a"       // ✗ ERRO
let p: Point = null   // ✗ ERRO — struct e ref nus nunca sao null (spec §2.4, issue #105)
let q: Point? = null  // ✓ `T?` e a unica grafia de nulidade; leia so apos `if q != null then`
```

Erros como dado: `errors.Result<T>` (`Ok`/`Err`/`Fail`), `if r.ok then`
estreita `r.value`, `try expr` propaga a falha numa funcao que devolve
`Result` (spec §7). Nomes globais resolvem em compilacao (`undefined global`)
e o escopo global e um namespace so (let/func/struct/import colidem).

### 2. Pipeline de Compilação
Sempre execute após modificações (da raiz do repositório):
```bash
go test ./internal/...
go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx
```

### 3. Especificação da Linguagem
📖 Consulte: `docs/NOXY_LANGUAGE_SPEC.md`. Regra de linguagem vem da spec ou
de teste no binário — nunca de um exemplo.

### 4. Guardas de arquitetura (testes que travam convenções)
Há testes que perguntam ao próprio código-fonte se as convenções continuam
valendo. Se um deles quebrar, o lugar errado foi tocado — não afrouxe o teste:

| Teste | O que trava |
|-------|-------------|
| `internal/vm/architecture_test.go` | Layout de fontes: cada `define<Area>Builtins()` no seu `builtins_*.go`, teardown terminal de frame só em `unwind.go`, pilhas em `stack.go`/`calls.go`, módulos em `modules.go`, sem maps globais crus na posse de runtime, registries de recursos com dono compartilhado |
| `internal/vm/inline_guard_test.go` | `push()` continua inlinável dentro de `run()` (custo ≤ 20) |
| `internal/vm/builtins_registry_test.go` | Snapshot **ordenado** de todos os nativos globais + assinaturas de `delete`/`append`/`pop`/`json_loads` |
| `internal/vm/native_signatures_test.go` | Contratos de tipo dos nativos/stdlib visíveis do Noxy |
| `internal/compiler/known_globals_test.go` | Nomes da stdlib e imports por namespace são conhecidos do compilador |

---

## 🛠️ Tarefas Comuns

### A. Adicionar Token/Operador (Lexer)

1. Adicione o token em `internal/token/token.go`
2. Implemente em `internal/lexer/lexer.go`
3. Teste em `internal/lexer/*_test.go` (um arquivo por tema: `literals_test.go`, `escapes_test.go`, `question_token_test.go`, ...)

```go
case '?':
    tok = newToken(token.QUESTION, l.ch) // sufixo de tipo anulavel: T?
```

### B. Nova Construção Sintática (Parser)

1. Defina o nó AST em `internal/ast/ast.go` (e ensine `walk.go`/`clone.go` a atravessá-lo, se tiver filhos)
2. Implemente o parsing recursivo em `internal/parser/parser.go`
3. Garanta precedência correta de operadores
4. Teste em `internal/parser/*_test.go`

### C. Gerar Bytecode (Compiler)

1. Adicione o opcode em `internal/chunk/chunk.go` (constante **e** nome em `String()`, senão o disassembly imprime número)
2. Implemente `compile<NodeType>()` — em `compiler.go` ou num arquivo por tema
3. Verifique tipos (o compilador é o primeiro a falar)
4. Emita bytecode e atualize o stack tracking
5. Teste com `*_compile_test.go` (padrão dos existentes: `typed_index_compile_test.go`, `superinstr_compile_test.go`)

**Opcodes principais:**
```go
OP_CONSTANT, OP_CONSTANT_LONG, OP_ADD, OP_SUB, OP_JUMP, OP_CALL
OP_GET_LOCAL, OP_SET_GLOBAL
// variantes tipadas/fundidas emitidas quando o tipo estatico permite:
OP_ADD_INT, OP_CALL_STATIC, opcodes tipados de indice de array e de campo
```

Aviso no compilador é `c.warn(msg)` (`internal/compiler/warnings.go`), que
acumula em `Compiler.Warnings()`. O compilador **nunca** escreve em stdout/stderr.

### D. Executar Opcode (VM)

Padrão em `internal/vm/executor.go`, dentro do `switch` de `run()` (`c` é o
chunk corrente e `ip` já aponta para o próximo byte):
```go
case chunk.OP_NEW_OPCODE:
    b := vm.pop()
    a := vm.pop()
    if a.Type != value.VAL_INT || b.Type != value.VAL_INT {
        return vm.runtimeError(c, ip, "operands must be integers")
    }
    vm.push(value.NewInt(a.Int() + b.Int()))
```

- `return vm.runtimeError(...)` sai de `run()`; o `defer` de `run()` chama
  `unwindTo` (`unwind.go`), que roda `defer`s Noxy, libera o RC dos slots do
  frame e restaura `stackTop`. Não há limpeza manual de pilha no caminho de erro.
- O efeito líquido de pilha de cada opcode tem de bater com o que o compilador
  contabiliza — é isso que "stack imbalance" significa aqui.
- Em nativos (fora de `run()`), erro é `runtimeErrorAtCurrentFrame(...)` ou
  devolver `error` de um `DefineContextualNative`.

### E. Adicionar Função Builtin

1. Implemente em `internal/vm/builtins_<area>.go` (`core`, `strings`, `io`,
   `net`, `json`, `sys`, `time`, `crypto`, `sqlite`, `collections`, `convert`,
   `concurrency`, `tasks`) — `architecture_test.go` espera cada `define<Area>Builtins()` no seu arquivo.
2. Registre no `define<Area>Builtins()` do arquivo (encadeados em
   `defineBuiltins()`, `builtins.go`):
   - `vm.DefineNative(name, func(args []value.Value) value.Value)` — puro
   - `vm.DefineContextualNative(name, func(ctx value.NativeContext, args) (value.Value, error))` — precisa da VM (`nativeVM(ctx)`), CoW, erro com stack Noxy
   - `...WithSignature(name, value.NativeSignature{Arity, Params, ReturnType}, fn)` — obrigatório quando há parâmetro `ref` (`ParamInfo{IsRef: true, TypeName: "ref array"}`): é a assinatura que faz o compilador exigir `ref x` no call site
3. Valide argumentos (`len(args)`, `args[i].Type`)
4. Atualize o snapshot de `builtins_registry_test.go` (lista ordenada) — ele falha até você adicionar o nome
5. Se o tipo de retorno é fixo e o nome existe sem `use` (`length`, `to_str`, ...), adicione em `coreBuiltinReturnTypes` (`internal/compiler/builtin_return_types.go`) para `let n = f(x)` inferir. Nada a fazer para o check de global inexistente: a CLI semeia `vm.GlobalNames()` no compilador.
6. Nativo de módulo usa prefixo `<mod>_` (`strings_trim`, `io_read`) e ganha wrapper tipado em `internal/stdlib/<mod>.nx` — é o wrapper que o usuário chama. API que pode falhar devolve `Result<T>` de `errors` (`Ok`/`Err`/`Fail`), não struct ad hoc.
7. Documente na spec (§10 builtins centrais, §12 stdlib)
8. **Contêineres devolvidos são donos dos filhos**: construa com `value.NewArray`,
   `value.NewMapWithData` ou `value.NewInstanceWith(def, fields)` — eles retêm cada
   filho composto (array/map/instância; no-op em escalares/strings). Nunca escreva
   um composto em `inst.Fields[...]`/`ObjMap.Set` cru sem `value.Retain`. Só use
   `value.NewArrayAdopting` para elementos que **você** já reteve em nome do array,
   com comentário `// RC: move` (sites atuais: `OP_ARRAY`, `copyValue`, merge de
   `causes` do `call_result`).
9. **Saída: stdout é do programa, stderr é do diagnóstico.** `print`/`iprint`
   escrevem em stdout; `eprint`/`eiprint` em stderr. Nunca use `fmt.Print*` para
   erro, aviso ou trace — escreva em `os.Stderr` (na VM) ou em `diagOut`
   (`cmd/noxy/main.go`), o único destino dos diagnósticos da CLI. No
   **compilador** não escreva em lugar nenhum: aviso é `c.warn(msg)`;
   quem chama `Compile` imprime — CLI/REPL em `diagOut`, loader de módulos
   da VM (`internal/vm/modules.go`) em `os.Stderr`.

```go
vm.DefineContextualNative("strings_shout", func(ctx value.NativeContext, args []value.Value) (value.Value, error) {
    if len(args) != 1 {
        return value.NewNull(), fmt.Errorf("strings_shout: expects exactly 1 argument, got %d", len(args))
    }
    s, ok := args[0].Obj.(string)
    if !ok || args[0].Type != value.VAL_OBJ {
        return value.NewNull(), fmt.Errorf("strings_shout: arg 1 must be string")
    }
    return value.NewString(strings.ToUpper(s)), nil
})
```

Construtores de `value`: `NewInt`, `NewFloat`, `NewBool`, `NewNull`,
`NewString`, `NewBytes`, `NewArray`, `NewMapWithData`, `NewInstanceWith`.

### F. Adicionar Módulo Stdlib

1. Crie `internal/stdlib/<mod>.nx` — o embed é automático (`embed.go` faz `//go:embed *.nx`), não há registro
2. Implemente os nativos `<mod>_*` na VM (tarefa E) e exponha só via wrapper tipado no `.nx`
3. Resolução de `use <mod>` (`internal/vm/modules.go`): `$NOXY_PATH` → `noxy_libs/` → `stdlib/` no disco → caminho relativo → **por último** o embutido. Um `.nx` local com o mesmo nome sombreia a stdlib. O cache de módulos é por `SharedState` (`module_cache.go`) — uma instância por processo, compartilhada por tasks.
4. Testes: contratos em `internal/vm/native_signatures_test.go`; se o módulo exporta nomes que precisam ser conhecidos do compilador, `internal/compiler/known_globals_test.go`
5. Exemplos em `noxy_examples/` + spec §12
6. Ao **remover** um módulo, grep em `noxy_examples/`, `*_test.go`, spec e site: os exemplos rodam na suíte e os testes Go importam módulos por nome

---

## 🧪 Testing

### Testes Unitários (Go)
```bash
go test ./internal/...          # tudo (CI, ambos os SOs)
go test ./internal/vm -v        # um pacote
go test -race ./internal/vm -count=1 -timeout=600s   # CI: pacote inteiro sob -race (~30 s)
```
Vários testes rodam programas Noxy inteiros a partir de strings Go
(`internal/vm/*_test.go`, `cmd/noxy/*_test.go`); é o lugar certo para um
caso de regressão pequeno. Fixtures de compilação em
`noxy_examples/type_errors/` (usadas por
`internal/compiler/function_conformance_examples_test.go`).

### Testes de Integração (Noxy)

O teste de integração principal é **`run_all_tests_concurrent.nx`**, que executa
os exemplos `.nx` em paralelo usando a própria concorrência do Noxy:

```bash
go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx   # da raiz do repo
```

#### 🔍 Como Funciona

1. **Interpretador**: usa `argv()[0]` — o binário que está rodando o script, nunca um `noxy` do PATH (por isso `go run` serve)
2. **Listagem**: `exec_output("dir /B noxy_examples")` no Windows, `ls noxy_examples` no resto — tem de rodar da raiz do repositório
3. **Filtragem**: `should_ignore()` remove os arquivos da lista `exclusions`
4. **Workers**: 8 `spawn`s consomem um canal de jobs (poison pill `"-1"`)
5. **Execução**: cada worker roda `<noxy> noxy_examples/arquivo.nx` e captura o exit code
6. **Agregação**: resultados por canal; `exit(1)` se qualquer exemplo falhar

#### 📊 Output Exemplo
```
=== NOXY CONCURRENT TEST RUNNER ===
Encontrados 179 arquivos de teste.
Iniciando 8 workers...
  [PASS] algoritmos.nx
  [PASS] binary_tree.nx
  [FAIL] test_error.nx:1
  ...
=== RELATÓRIO ===
Total: 179
Passou: 178
Falhou: 1
Tempo Total: 3450 ms
ALGUNS TESTES FALHARAM:
FAIL:test_error.nx:1
```

#### 🚫 Arquivos Excluídos

A lista `exclusions` em `should_ignore()` (~45 entradas) ignora:
- **Servers/Interativos**: `web_app.nx`, `todo_app.nx`, `form_app.nx`, `http_server_*.nx`, `simple_server.nx`, `wc_stdin.nx` (lê stdin), `signal_demo.nx`, `watch_file.nx`, ...
- **Erros Intencionais**: `division_error.nx`, `error_*.nx`, `test_let_error*.nx`, `test_unclosed.nx`, `test_typed_chan_error.nx`
- **Benchmarks/Stress**: `benchmark_parallel.nx`, `stress_test.nx`, `concurrency*.nx`
- **Visualizações**: `conway*.nx`, `langtons_ant.nx`, `brainfuck.nx`, `space_invaders*.nx`, `fibonacci_spinner.nx`
- **Compartilham `loja.db`**: `sqlite_demo.nx` (falha intermitente por concorrência no mesmo arquivo)
- **Runners**: `run_all_tests_concurrent.nx`

Subdiretórios (`KandR_in_noxy/`, `password_manager/`, `pkg_test/`,
`type_errors/`) não entram na listagem — só `.nx` direto em `noxy_examples/`.
`tests/test_features/` e `tests/test_errors/` são fixtures avulsas: nada as roda em CI.

#### 🔧 Adicionar Teste ao Runner

1. Crie seu arquivo `.nx` em `noxy_examples/`
2. Garanta que o programa retorna exit code 0 em sucesso e `exit(1)` em falha (declare um `assert` local — ver template abaixo)
3. Se não deve rodar automaticamente (interativo, erro proposital, longo), adicione à lista `exclusions` de `should_ignore()` — e diga por quê no comentário

#### 🐛 Debug de Falhas

```bash
# Rodar teste individual (sem depender de um binario no PATH)
go run ./cmd/noxy noxy_examples/seu_teste.nx

# Com binario
go build -o noxy ./cmd/noxy
./noxy noxy_examples/seu_teste.nx
echo $?           # Unix; no PowerShell: $LASTEXITCODE
```

### Template de Teste
`assert` **não** é builtin: cada exemplo declara o seu, saindo com `exit(1)`
(é o exit code que o runner lê):
```noxy
use sys select exit

func assert(condition: bool, message: string) -> void
    if !condition then
        print("feature: FAIL - " + message)
        exit(1)
    end
end

func test_feature()
    let result: int = new_feature(42)
    assert(result == 84, "Expected 84")
    print("✓ test passed")
end

test_feature()
```

### Benchmarks
`benchmarks/` tem os `bench_*.nx`, `RESULTS.md` e os scripts
`run_benchmarks.ps1`, `interleaved_compare.ps1` (dois binários intercalados —
a única comparação que vale) e `compare_examples.ps1`. Rode com
`pwsh -NoProfile -File` (PowerShell 7); máquina ociosa; delta só dentro da
mesma sessão. Performance nunca muda semântica (Zen).

### CI
`.github/workflows/network-deadlines.yml`, em push/PR para `develop` e `main`:
- **Runtime semantics** (ubuntu + windows): testes de rede filtrados, `go test -race ./internal/vm`, `go test ./internal/...`
- **Noxy examples** (ubuntu + windows): o runner concorrente acima
- **Cross-build** (linux/darwin/freebsd amd64, `CGO_ENABLED=0`): `go build ./...`

---

## 🚨 Armadilhas Comuns

### 1. Efeito de pilha
```go
// ✗ ERRADO — opcode que consome 2 e produz 0 quando o compilador contou 1
b := vm.pop()
a := vm.pop()
if a.Type != value.VAL_INT { return vm.runtimeError(c, ip, "...") }
// esqueceu o push do resultado

// ✓ CORRETO
b := vm.pop()
a := vm.pop()
if a.Type != value.VAL_INT { return vm.runtimeError(c, ip, "...") }
vm.push(result)
```
O caminho de erro é limpo pelo unwind; o caminho de sucesso é sua responsabilidade.

### 2. Type Checking
```go
// ✗ ERRADO
str := val.Obj.(string)  // panic se nao for string

// ✓ CORRETO
str, ok := val.Obj.(string)
if !ok || val.Type != value.VAL_OBJ {
    return vm.runtimeError(c, ip, "expected string")
}
```
`bytes` é `VAL_BYTES` (payload `string`), não `VAL_OBJ`. Nunca confie que
`any` traz um tipo: valide antes de usar (`runtime_type_validation.go`).

### 3. Scope Depth
```go
func (c *Compiler) beginScope() {
    c.scopeDepth++  // Não esqueça!
}

func (c *Compiler) endScope() {
    c.scopeDepth--
    // Pop locals...
}
```

### 4. Resource Cleanup
Recursos do runtime (arquivos, sockets, listeners, bancos, statements) vivem
nos registries de `SharedState` (`resources.go`) e são referenciados por
handle inteiro no Noxy. Sempre remova do registry ao fechar; em Go, `defer`
para o que for local.

### 5. Reter/soltar RC
`Retain`/release a menos é vazamento; a mais é double free — os dois são
proibidos pela spec. Testes com oráculo de clones (`cow_*_test.go`,
`container_owners_test.go`) pegam isso; rode-os ao mexer em qualquer coisa
que guarde um composto.

### 6. `*CallFrame` através de chamada reentrante
`vm.frames` cresce (dobra) em `ensureCallCapacity` e realoca; um `*CallFrame`
guardado numa variável Go antes de uma chamada Noxy reentrante fica pendurado.
Reobtenha por índice (`&vm.frames[vm.frameCount-1]`), como `finalizeCurrentFrame` faz.

---

## 🔍 Debugging

### CLI
```bash
noxy --help
noxy --version                 # imprime version.Version
noxy --disassembly arquivo.nx  # bytecode de cada chunk antes de rodar
noxy --cpuprofile cpu.prof arquivo.nx
noxy --memprofile mem.prof arquivo.nx
noxy --get github.com/user/repo@v1.0.0   # instala em noxy_libs/, registra em noxy.mod/noxy.sum
```

### Disassembly (em código)
```go
c.Disassemble("== chunk name ==")     // um chunk
c.DisassembleAll("== program ==")     // chunk + funcoes aninhadas
```

### Stack Trace
```go
fmt.Fprintf(os.Stderr, "Stack: %v\n", vm.stack[:vm.stackTop])
fmt.Fprintf(os.Stderr, "IP: %d, Opcode: %s\n", ip, instruction)
```
(sempre em stderr — stdout é do programa).

### REPL
```bash
./noxy
>>> let x: int = 42
>>> x + 10
52
```

Em tty POSIX (Linux/macOS/WSL) o REPL lê cada linha pelo editor de linha de
`internal/lineedit` (setas, Home/End, histórico da sessão; Ctrl-C encerra o
REPL com 130, como o SIGINT de sempre; Ctrl-D/`exit` saem com 0); a lógica é
testável sem tty (`editor_test.go`) e os testes de termios/pty rodam só no
Linux (`terminal_linux_test.go`). Em pipe e no Windows (console cooked já
edita a linha) o REPL usa `bufio.Scanner`, como sempre — o loop em si é
`runREPL(src lineSource, ...)` em `cmd/noxy/main.go`. No Windows,
`internal/console.EnsureLineInput` conserta antes um modo raw vazado por
outro programa (sintoma: REPL "travado", teclas sem eco).

O REPL segue a mesma regra do arquivo: a sessão é um arquivo lido linha a
linha (re-`let` no mesmo escopo é erro; redefinir `func` entre linhas é permitido).

---

## 📚 Recursos

### Documentação
- `docs/NOXY_LANGUAGE_SPEC.md` - Especificação completa (fonte da verdade da linguagem)
- `CHANGELOG.md` - Histórico por versão, com tabelas "Antes/Agora" nas quebras
- `docs/REF_SEMANTICS.md` - Referências, CoW e posse (RC)
- `docs/concurrency.md` - Sistema de concorrência
- `docs/JSON_SUPPORT.md` - JSON: `json_loads` tipado, `Result<string>` em `dumps_result`
- `docs/PACKAGE_MANAGER.md` - `noxy --get`, `noxy.mod`/`noxy.sum`, `noxy_libs/`
- `docs/EXTENSIONS.md` - Extensões WASM (manifesto, ABI 1, `stateless`/`single`)
- `docs/HTTP_SERVER.md`, `docs/CRYPTO_MODULE.md` - Módulos específicos
- `docs/SHOWCASE.md`, `docs/complex_declarations.md`
- `docs/superpowers/specs/` - Designs datados de cada mudança grande (CoW, genéricos, `call_result`, pilhas dinâmicas, extensões, nulidade...)
- `docs/superpowers/plans/` - Planos de implementação correspondentes
- `README.md` - Visão geral, instalação, Zen
- `docs/index.html` (+ `styles.css`, `script.js`) - Site (GitHub Pages, `noxylang.com`). Todo `docs/*.md` passa pelo Liquid do Pages: `{{` literal só dentro de `<!-- {% raw %} -->`

### Código de Referência
```
internal/vm/executor.go        // run(): switch de opcodes (~2k linhas)
internal/vm/stack.go           // push/pop, crescimento da pilha de operandos
internal/vm/calls.go           // protocolo de chamada, ensureCallCapacity
internal/vm/unwind.go          // teardown de frame, defer, propagacao de erro
internal/vm/cow.go             // copy-on-write / unicizacao
internal/vm/builtins_*.go      // nativos por area
internal/compiler/compiler.go  // nucleo do compilador
internal/compiler/generics*.go // monomorfizacao, unificacao, target typing
internal/compiler/nullable.go, narrowing.go, try.go   // T?, narrowing, try
internal/value/value.go        // Value, objetos, construtores
internal/value/cow.go          // Retain/Release, contadores
internal/parser/parser.go      // Parser recursivo
cmd/noxy/main.go               // CLI, REPL, diagOut
```

### Exemplos
- `noxy_examples/` - ~220 programas; é o corpus da suíte de integração
- `noxy_examples/KandR_in_noxy/` - Exercícios do K&R portados
- `noxy_examples/password_manager/` - App completo (sqlite + crypto + http)
- `noxy_examples/type_errors/` - Programas que **devem** falhar na compilação
- `benchmarks/` - `bench_*.nx` + scripts de comparação

---

## `ref` nos exemplos e testes

Uma referência nunca é criada nem lida sem `ref` ou `*` (spec §2.3):
`*r` para ler/escrever o valor, `ref x` em **todo** call site com parâmetro
`ref T` (builtins inclusos: `append(ref xs, v)`), `r.f`/`r[i]` atravessam.
Uma expressão que já é `ref T` (parâmetro `ref`, campo `ref`) é passada
sem `ref`. `print(r)` mostra a referência; `print(*r)` o valor.
`ref T` nunca é `null`; só `ref T?` aceita (spec §2.4).

---

## 🎯 Workflow para Nova Feature

1. **Planejamento**
   - Ler a spec e o CHANGELOG (o comportamento atual costuma ser decisão registrada, não acidente — grep antes de chamar algo de bug)
   - Verificar exemplos existentes e `docs/superpowers/specs/`
   - Identificar componentes afetados

2. **Implementação**
   - Tokens → AST → Parsing → Opcodes → Compiler → VM
   - Teste antes do código: os `*_test.go` por tema mostram o padrão

3. **Testing**
   - Testes unitários (Go), incluindo os guardas de arquitetura
   - Testes de integração (Noxy) — e capture a saída dos exemplos antes/depois (`benchmarks/compare_examples.ps1`) quando mexer no runtime
   - Edge cases

4. **Validação**
   ```bash
   go build ./...
   go vet ./...
   go test ./internal/... -count=1
   go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx
   ```

5. **Documentação**
   - `CHANGELOG.md` (seção da versão; quebra = tabela Antes/Agora + migração)
   - Spec, `README.md`, site (`docs/index.html`) e este arquivo quando a mudança for visível

---

## 🔐 Segurança

### Validações Essenciais
```go
// Input validation
if len(args) == 0 {
    return value.NewNull(), fmt.Errorf("expected at least 1 argument")
}

// Bounds checking
if index < 0 || index >= len(array) {
    return vm.runtimeError(c, ip, "index out of bounds")
}
```

**Overflow de `int` NÃO é checado**: a aritmética dá a volta (complemento de
dois, como Go) — decisão de linguagem, documentada na spec §8. Não adicione
checagem de overflow em `+ - *`.

**Resource limits**: `ensureCallCapacity` (`internal/vm/calls.go`) é o **único**
ponto que verifica os tetos `FramesMax`/`StackMax` no caminho normal — as duas
pilhas nascem pequenas (64 frames / 4096 slots), dobram sob demanda e o erro é
sempre um runtime error (`stack overflow: ...`), nunca um panic Go. Ela custa
**exatamente 80** no inliner (o orçamento): código novo vai para `growForCall`
(o caminho frio, `//go:noinline`), nunca para o corpo dela. `push` **nunca**
cresce a pilha: ela precisa caber no orçamento de inline de `run()` (custo ≤ 20;
qualquer nó a mais desinlina `push` em ~117 call sites e custa ~20 % no
interpretador). Os dois contratos são travados por
`internal/vm/inline_guard_test.go` — se você mexer em `push` ou em
`ensureCallCapacity`, rode esse teste (ou `go build -gcflags='-m -m' ./internal/vm`).

**Extensões e plugins**: duas fronteiras, um `Backend` (`internal/ext/backend.go`).
wasm (`kind = "wasm"`, default) roda no wazero sem WASI, para computação pura.
Processo (`kind = "process"`, spec `docs/superpowers/specs/2026-08-29-process-extensions-design.md`)
é o meio principal para I/O, SO, drivers e SDKs: um executável por plataforma
publicado como asset de release, `noxy --get` baixa só o da máquina e grava
os hashes de todos em `noxy.sum`; protocolo `noxy-plugin/1` (quadros NXB por
stdio, start lazy, CANCEL cooperativo, poison/restart); SDK em `sdk/noxyplugin`
(testes rodam à parte: `go test ./...` dentro do módulo). `sys_load_plugin`
(JSON) está deprecado e sai na v0.25.0 junto com `internal/plugin` e
`compiler.PluginNativeNames`. Invariantes em
`docs/superpowers/specs/2026-08-29-extensibility-invariants-revision.md`.

---

## 🎓 Filosofia

O Zen da Noxy (README e abertura da spec) é a bússola das decisões — não um
regulamento:

```text
Simplicity is sophistication.
Typing is safety — and the compiler speaks first.
Dynamic exists, but it is explicit: any says what it is.
Variables are copies, unless explicitly stated otherwise.
Sharing is ref — in the type and at the call site. Closures and globals share by name; nothing else does.
CoW + ref is one heck of a duo!
An error is a value, not an exception.
One rule, everywhere: file, module, REPL.
Consistency comes before performance.
Performance is measured afterwards — without changing semantics.
Lean core, vast ecosystem.
Fixing beats staying compatible, until 1.0 says otherwise.
```

Na prática: erro em compile-time > runtime; sem carve-outs para o REPL;
"core enxuto" = runtime + stdlib; quebra de compatibilidade é aceitável antes
da 1.0, **com** tabela de migração no CHANGELOG.

---

## 📝 Convenções de commit, CHANGELOG e versão

- **Commits/PRs**: `tipo(escopo): descrição em português (issue #N)` —
  `feat`, `fix`, `docs`, `chore`, `perf`, `refactor`. Branches saem de
  `develop`; `main` recebe releases.
- **CHANGELOG.md**: Keep a Changelog; toda versão tem seção datada; quebra
  vai em `Changed (BREAKING)` com tabela Antes/Agora.
- **Versão** (`vX.Y.Z`): patch para correções, minor para mudanças maiores.
  Um bump toca: `internal/version/version.go`, `CHANGELOG.md`, `README.md`
  (banner do REPL), spec §12 (`sys.version`), `docs/index.html` (badge e
  exemplo) e o rodapé deste arquivo. Release no GitHub: título
  `vX.Y.Z (dd/mm/aaaa)`, push só da tag nominal.
- **EOL**: o checkout Windows é CRLF no worktree com índice LF (autocrlf).
  `gofmt -l` lista o repositório inteiro por causa disso — use `gofmt -d`
  nos arquivos que tocou e confira com `git diff --numstat` que nenhum
  arquivo foi reescrito por completo.

---

## 📋 Checklist de PR

- [ ] `go build ./...` - Compila sem erros
- [ ] `go vet ./...` - Sem warnings
- [ ] `go test ./internal/... -count=1` - Todos os testes passam (guardas de arquitetura inclusos)
- [ ] `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx` - Exemplos passam
- [ ] `gofmt -d` limpo nos arquivos tocados
- [ ] Snapshot de `builtins_registry_test.go` atualizado se houver nativo novo
- [ ] `CHANGELOG.md` atualizado (e versão, se for release)
- [ ] Spec/README/site atualizados quando a mudança é visível ao usuário
- [ ] Exemplos funcionais adicionados (e excluídos do runner se forem interativos)

---

**Última Atualização**: 2026-08-28  
**Versão**: 1.1 (Noxy VM 0.22.0)

*Este documento é vivo - atualize ao adicionar features significativas.*
