# AGENTS.md - Guia para Agentes de IA no Projeto Noxy VM

## 🎯 Visão Geral

**Noxy VM** é uma máquina virtual baseada em bytecode para a linguagem Noxy, escrita em Go.

### Características
- Linguagem estaticamente tipada com tipos imutáveis
- Compilação para bytecode de alto desempenho
- Suporte a structs, arrays, maps, referências e concorrência
- Biblioteca padrão rica (SQLite, HTTP, crypto, etc.)

### Arquitetura
```
Source Code → Lexer → Parser → AST → Compiler → Bytecode → VM
```

| Camada | Pacote | Função |
|--------|--------|--------|
| **Frontend** | `internal/lexer` | Tokenização |
| | `internal/parser` | Parser → AST |
| **Compiler** | `internal/compiler` | AST → Bytecode |
| | `internal/chunk` | Storage de bytecode |
| **Runtime** | `internal/vm` | Execução stack-based |
| | `internal/stdlib` | Módulos nativos |

---

## 🔧 Regras Fundamentais

### 1. Compatibilidade de Tipos
Noxy é **estaticamente tipada** - nunca quebre a verificação de tipos:
```noxy
let x: int = 42
x = 100       // ✓ OK
x = 3.14      // ✗ ERRO
```

### 2. Pipeline de Compilação
Sempre execute após modificações:
```bash
go test ./internal/...
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

### 3. Especificação da Linguagem
📖 Consulte: `docs/NOXY_LANGUAGE_SPEC.md`

---

## 🛠️ Tarefas Comuns

### A. Adicionar Token/Operador (Lexer)

1. Adicione token em `internal/token/token.go`
2. Implemente lógica em `internal/lexer/lexer.go`
3. Teste em `internal/lexer/lexer_test.go`

```go
case '?':
    if l.peek() == '?' {
        l.advance()
        return l.makeToken(token.NULL_COALESCE)
    }
```

### B. Nova Construção Sintática (Parser)

1. Defina nó AST em `internal/ast/ast.go`
2. Implemente parsing recursivo em `internal/parser/parser.go`
3. Garanta precedência correta de operadores
4. Teste em `internal/parser/parser_test.go`

### C. Gerar Bytecode (Compiler)

1. Adicione opcode em `internal/chunk/chunk.go` (se necessário)
2. Implemente `compile<NodeType>()` 
3. Verifique tipos
4. Emita bytecode e atualize stack tracking

**Opcodes principais:**
```go
OP_CONSTANT, OP_ADD, OP_SUB, OP_JUMP, OP_CALL
OP_GET_LOCAL, OP_SET_GLOBAL
```

### D. Executar Opcode (VM)

Padrão de implementação em `internal/vm/vm.go`:
```go
case chunk.OP_NEW_OPCODE:
    b := vm.pop()
    a := vm.pop()
    
    if !vm.checkType(a, value.INT) {
        return vm.runtimeError(...)
    }
    
    result := operate(a, b)
    vm.push(result)
```

### E. Adicionar Função Builtin

1. Implemente em `internal/vm/vm.go`
2. Registre em `defineNatives()`
3. Valide argumentos
4. Documente na spec
5. **Contêineres devolvidos são donos dos filhos**: construa com `value.NewArray`,
   `value.NewMapWithData` ou `value.NewInstanceWith(def, fields)` — eles retêm cada
   filho composto (array/map/instância; no-op em escalares/strings). Nunca escreva
   um composto em `inst.Fields[...]`/`ObjMap.Set` cru sem `value.Retain`. Só use
   `value.NewArrayAdopting` para elementos que **você** já reteve em nome do array,
   com comentário `// RC: move` (sites atuais: `OP_ARRAY`, `copyValue`, merge de
   `causes` do `call_result`).
6. **Saída: stdout é do programa, stderr é do diagnóstico.** `print`/`iprint`
   escrevem em stdout; `eprint`/`eiprint` em stderr. Nunca use `fmt.Print*` para
   erro, aviso ou trace — escreva em `os.Stderr` (na VM) ou em `diagOut`
   (`cmd/noxy/main.go`), o único destino dos diagnósticos da CLI. No
   **compilador** não escreva em lugar nenhum: aviso é `c.warn(msg)`
   (`internal/compiler/warnings.go`), que acumula em `Compiler.Warnings()`;
   quem chama `Compile` imprime — CLI/REPL em `diagOut`, loader de módulos
   da VM (`internal/vm/modules.go`) em `os.Stderr`.

```go
func (vm *VM) builtinNewFunc(args []value.Value) (value.Value, error) {
    if len(args) != 2 {
        return value.Value{}, fmt.Errorf("expects 2 arguments")
    }
    
    if args[0].Type != value.STRING {
        return value.Value{}, fmt.Errorf("arg 1 must be string")
    }
    
    // Lógica...
    return value.MakeString(result), nil
}
```

### F. Adicionar Módulo Stdlib

1. Crie `.nx` em `internal/stdlib/`
2. Registre em `internal/stdlib/embed.go`
3. Implemente funções nativas na VM
4. Adicione exemplos

---

## 🧪 Testing

### Testes Unitários (Go)
```bash
go test ./internal/lexer -v
go test ./internal/parser -v
go test ./internal/compiler -v
go test ./internal/vm -v
```

### Testes de Integração (Noxy)

O teste de integração principal é **`run_all_tests_concurrent.nx`**, que executa todos os exemplos `.nx` em paralelo usando o sistema de concorrência do Noxy:

```bash
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

#### 🔍 Como Funciona

1. **Detecção de Ambiente**: Identifica o SO (Windows/Unix) para executar comandos apropriados
2. **Listagem de Arquivos**: Usa `exec_output()` para listar `noxy_examples/`
3. **Filtragem**: Remove arquivos excluídos (servers, testes com erro intencional, etc.)
4. **Workers Concorrentes**: Cria 8 goroutines (noxy routines) que executam testes em paralelo
5. **Execução**: Cada worker executa `noxy.exe arquivo.nx` e captura exit code
6. **Agregação**: Coleta resultados via channels e reporta estatísticas

#### 📊 Output Exemplo
```
=== NOXY CONCURRENT TEST RUNNER ===
Encontrados 120 arquivos de teste.
Iniciando 8 workers...
  [PASS] algoritmos.nx
  [PASS] binary_tree.nx
  [FAIL] test_error.nx:1
  ...
=== RELATÓRIO ===
Total: 120
Passou: 118
Falhou: 2
Tempo Total: 3450 ms
```

#### 🚫 Arquivos Excluídos

O runner automaticamente ignora:
- **Servers/Interativos**: `http_server.nx`, `web_app.nx`, `todo_app.nx`, `wc_stdin.nx` (lê stdin)
- **Erros Intencionais**: `division_error.nx`, `test_let_error.nx`
- **Benchmarks/Stress**: `benchmark_parallel.nx`, `stress_test.nx`
- **Visualizações**: `conway.nx`, `langtons_ant.nx`, `brainfuck.nx`
- **Runners**: `run_all_tests.nx`, `run_all_tests_concurrent.nx`

#### 🔧 Adicionar Teste ao Runner

1. Crie seu arquivo `.nx` em `noxy_examples/`
2. Garanta que o programa retorna exit code 0 em sucesso
3. Se não deve rodar automaticamente, adicione à lista `exclusions` (linha 27)

#### 🐛 Debug de Falhas

```bash
# Rodar teste individual
go run cmd/noxy/main.go noxy_examples/seu_teste.nx

# Ver output detalhado
./noxy noxy_examples/seu_teste.nx

# Verificar exit code (Unix)
./noxy noxy_examples/seu_teste.nx
echo $?
```

### Template de Teste
```noxy
func test_feature()
    let result: int = new_feature(42)
    assert(result == 84, "Expected 84")
    print("✓ test passed")
end
```

---

## 🚨 Armadilhas Comuns

### 1. Stack Imbalance
```go
// ✗ ERRADO
vm.push(a)
return vm.runtimeError(...)  // Esqueceu de pop!

// ✓ CORRETO
a := vm.pop()
if error {
    return vm.runtimeError(...)
}
vm.push(result)
```

### 2. Type Checking
```go
// ✗ ERRADO
str := val.Str  // Pode ser nil!

// ✓ CORRETO
if val.Type != value.STRING {
    return vm.runtimeError("Expected string")
}
str := val.Str
```

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
```go
db, err := sql.Open("sqlite", file)
if err != nil {
    return err
}
defer db.Close()  // Sempre limpe recursos!
```

---

## 🔍 Debugging

### Disassembly
```go
c.currentChunk.Disassemble("== chunk name ==")
```

### Stack Trace
```go
fmt.Printf("Stack: %v\n", vm.stack[:vm.sp])
fmt.Printf("IP: %d, Opcode: %s\n", frame.ip, opcode)
```

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
testável sem tty (`editor_test.go`) e os
testes de termios/pty rodam só no Linux (`terminal_linux_test.go`). Em pipe e
no Windows (console cooked já edita a linha) o REPL usa `bufio.Scanner`, como
sempre — o loop em si é `runREPL(src lineSource, ...)` em `cmd/noxy/main.go`.

---

## 📚 Recursos

### Documentação
- `docs/NOXY_LANGUAGE_SPEC.md` - Especificação completa
- `docs/CONCURRENCY.md` - Sistema de concorrência
- `docs/PACKAGE_MANAGER.md` - Gerenciador de pacotes
- `docs/REF_SEMANTICS.md` - Sistema de referências

### Código de Referência
```
internal/vm/vm.go          // Loop de execução (5000+ linhas)
internal/compiler/compiler.go  // Compilação (2000+ linhas)
internal/parser/parser.go      // Parser recursivo
```

### Exemplos
- `noxy_examples/` - Exemplos funcionais
- `tests/test_features/` - Testes de features

---

## 🎯 Workflow para Nova Feature

1. **Planejamento**
   - Ler spec da linguagem
   - Verificar exemplos existentes
   - Identificar componentes afetados

2. **Implementação**
   - Tokens → AST → Parsing → Opcodes → Compiler → VM

3. **Testing**
   - Testes unitários (Go)
   - Testes de integração (Noxy)
   - Edge cases

4. **Validação**
   ```bash
   go test ./...
   go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
   go build -o noxy ./cmd/noxy
   ```

---

## 🔐 Segurança

### Validações Essenciais
```go
// Input validation
if len(args) == 0 {
    return value.Value{}, fmt.Errorf("expected at least 1 argument")
}

// Bounds checking
if index < 0 || index >= len(array) {
    return vm.runtimeError("index out of bounds")
}
```

**Overflow de `int` NÃO é checado**: a aritmética dá a volta (complemento de
dois, como Go) — decisão de linguagem, documentada na spec §8. Não adicione
checagem de overflow em `+ - *`.

**Resource limits**: `ensureCallCapacity` (`internal/vm/calls.go`) é o **único**
ponto que verifica os tetos `FramesMax`/`StackMax` no caminho normal — as duas
pilhas crescem sob demanda e o erro é sempre um runtime error
(`stack overflow: ...`), nunca um panic Go. `push` **nunca** cresce a pilha:
ela precisa caber no orçamento de inline de `run()` (custo ≤ 20; qualquer nó a
mais desinlina `push` em ~117 call sites e custa ~20 % no interpretador). Esse
contrato é travado por `internal/vm/inline_guard_test.go` — se você mexer em
`push`, rode esse teste.

---

## 🎓 Filosofia

> **"Clareza sobre cleverness, simplicidade sobre features excessivas,
> compatibilidade com código existente."**

### Princípios
1. **Explicitness** - Tipos explícitos, sem conversões mágicas
2. **Safety First** - Erros em compile-time > runtime
3. **Performance Matters** - Mas não a custo de legibilidade
4. **Educational Value** - Código compreensível para estudantes

---

## 📋 Checklist de PR

- [ ] `go build ./...` - Compila sem erros
- [ ] `go test ./...` - Todos os testes passam
- [ ] `gofmt -w .` - Código formatado
- [ ] `go vet ./...` - Sem warnings
- [ ] Documentação atualizada
- [ ] Exemplos funcionais adicionados

---

**Última Atualização**: 2026-08-22  
**Versão**: 1.0 (Noxy VM 0.15.0)

*Este documento é vivo - atualize ao adicionar features significativas.*