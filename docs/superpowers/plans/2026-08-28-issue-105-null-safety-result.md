# Issue #105 — nulidade `T?`, `Result<T>` + `try`, solidez de nomes (#47) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Entregar a issue #105 inteira em `v0.22.0`: (C) nomes sólidos — parâmetro duplicado, colisão no escopo global e global inexistente viram erro de compilação; (A) `T?`/`ref T?` como única grafia de nulidade, struct e `ref` **não-nulos por padrão**, narrowing em `if e != null` / early return / `&&` / `while`; (B) um `Result<T>` tipado em `errors`, `call_result` tipado pela assinatura do callee e devolvendo instância real de struct, `try expr` propagando a falha.

**Architecture:** Tipos são nós AST (`ast.NoxyType`); entra um nó `ast.NullableType`, um token `?` e um caso novo em cada walker de tipo (21 `switch` com `case *ast.RefType`). `null` passa por um único funil, `acceptsNull` (`function_types.go:41`), e pelo espelho de runtime em `runtime_type_validation.go:112` — os dois mudam em lockstep, primeiro aceitando `NullableType` (fase 1), depois deixando de aceitar struct/`ref` nus (fase 2). O narrowing é um mapa de "chaves estáveis provadas não-nulas" (`x`, `*r`, `a.b.c`) no compilador, aplicado na leitura de identificador/membro/deref e invalidado por atribuição, chamada e laço — sem nenhuma mudança na VM. `Result<T>` é um struct genérico comum em `errors.nx`; `call_result` recebe do compilador dois argumentos ocultos (construtores de `Result<R>` e `Failure`) e monta instâncias reais; `try` abaixa para `OP_DUP`/`OP_SWAP`/salto/`OP_RETURN` — os `defer` já rodam em `OP_RETURN`. O check de global inexistente é ativado pelo embutidor (CLI/VM) que semeia os nomes de nativos vivos no compilador; `compiler.New()` puro continua permissivo.

**Tech Stack:** Go 1.25 (`go test ./...`, `go test -race ./internal/vm`), Noxy (corpus `noxy_examples/run_all_tests_concurrent.nx`, fixtures negativas `noxy_examples/type_errors/`), `gh` CLI. Python 3 só para codemods descartáveis no scratchpad.

**Spec:** issue #105 (https://github.com/estevaofon/noxy/issues/105) — seções "1. Não-nulo por padrão", "2. Result<T> único + propagação", "3. Solidez de nomes", "Como fica o código", "Modelo de referência: Kotlin (e Dart)". Desvios da issue decididos neste plano (registrar no comentário final da issue, Task 21): `value: T?` em vez de "zero de T" (struct não-nulo não tem zero); `Result<bool>` para `close_result` (não há tipo unit); `T` **pode** bindar `Node?` (só `ref` continua proibido); narrowing cobre caminhos estáveis `a.b` e `*r` além de locais.

## Global Constraints

- Branch `feature/issue-105-null-safety-result` (criada a partir de `develop` @ `d0ed9b6`); PR contra `develop`. **O dono mergeia; nunca fechar issues.** Versão final **`v0.22.0`** — nunca 1.0 (decisão do dono).
- Commits Conventional-Commits com escopo (`feat(compiler):`, `feat(vm):`, `fix(compiler):`, `docs(spec):`, `chore(version):`), terminando com `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- TDD: todo diagnóstico novo entra RED (teste do erro **e** do hint) antes da implementação. Testes de VM verificam pelo valor em runtime (`captureVMSource` + `test_report`).
- Todo erro novo tem `\n  hint: …` na linha seguinte, formato dos helpers existentes.
- Comentários em Go **sem acentos** (convenção do repo); docs em PT-BR (`CHANGELOG.md`, plans) ou EN (`NOXY_LANGUAGE_SPEC.md`, `README.md`) conforme o arquivo.
- `go test ./...` verde ao fim de **cada** task. Um teste existente que quebre porque fixava o comportamento antigo por design é reescrito para fixar o novo (listados por task).
- Validação final (AGENTS.md): `go test ./...`; `go test -race ./internal/vm -count=1`; `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx`; `go build -o noxy ./cmd/noxy`.
- Não mexer em RC/CoW/`Owned`, fast paths de índice, `validateParameterModes`, `OP_EQUAL`.
- Helpers de teste: compilador → `compileFunctionSource(t, src)` (`function_types_test.go:11`) ou `New().Compile(parse(src))`; diagnósticos exatos → tabela em `static_diagnostics_test.go:13`; fixtures negativas → `noxy_examples/type_errors/*.nx` + tabela em `function_conformance_examples_test.go:38` (match por `strings.HasSuffix`); VM → `captureVMSource(t, src)` com `test_report(...)`, `interpretVMSource`, `interpretOrCompileErr` (`vm_test_helpers_test.go`).
- Guardas de AST: `internal/ast/clone_test.go:17` exige caso em `clone.go` para todo nó; `string_golden_test.go` pode exigir entrada golden — rodar `go test ./internal/ast` após cada nó novo.
- Ordem de execução: **Parte C (#47) → Parte A (nulidade) → Parte B (Result/try) → Parte D (docs, corpus, versão, PR)**. C é independente e barato; B depende de A (`value: T?`, narrowing em `r.value`).

---

## Parte C — Solidez de nomes (#47)

### Task 1: Parâmetro duplicado é erro de compilação

**Files:**
- Modify: `internal/compiler/runtime_types.go:211` (`checkSignatureTypes`)
- Test: `internal/compiler/static_diagnostics_test.go`

**Interfaces:**
- Produces: mensagem `[line N] duplicate parameter 'x' in function 'f'` (literal de função: nome `<anonymous>`).

- [ ] **Step 1: Teste RED**

Em `internal/compiler/static_diagnostics_test.go`, na tabela `cases` de `TestStaticDiagnosticsAreReportedWithTheirExactMessage`, acrescentar:

```go
{
    name:   "duplicate parameter",
    source: "func f(x: int, x: int) -> int\n    return x\nend\n",
    want:   "[line 1] duplicate parameter 'x' in function 'f'",
},
{
    name:   "duplicate parameter in function literal",
    source: "let g: func(int, int) -> int = func(a: int, a: int) -> int\n    return a\nend\n",
    want:   "duplicate parameter 'a' in function '<anonymous>'",
},
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/compiler -run TestStaticDiagnosticsAreReportedWithTheirExactMessage -v`
Expected: FAIL — os dois casos compilam sem erro.

- [ ] **Step 3: Implementar em `checkSignatureTypes`**

`checkSignatureTypes(name string, params []*ast.Parameter, returnType ast.NoxyType, line int) error` já itera `params` (é chamado em `compiler.go:2198` para `FunctionStatement` e `:2230` para `FunctionLiteral`). Antes do laço existente:

```go
display := name
if display == "" {
    display = "<anonymous>"
}
seen := make(map[string]struct{}, len(params))
for _, param := range params {
    if _, dup := seen[param.Name]; dup {
        return fmt.Errorf("[line %d] duplicate parameter '%s' in function '%s'", line, param.Name, display)
    }
    seen[param.Name] = struct{}{}
}
```

Confirmar que o literal passa `""`/`"<anonymous>"` como `name` em `compiler.go:2230`; se passar outro placeholder, usar o mesmo no teste.

- [ ] **Step 4: Rodar e ver passar** — `go test ./internal/compiler -run TestStaticDiagnostics -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/compiler/runtime_types.go internal/compiler/static_diagnostics_test.go
git commit -m "feat(compiler): parametro duplicado e erro de compilacao (issue #47 parte 1)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 2: Namespace global unificado — colisão let × func × struct × import

**Files:**
- Modify: `internal/compiler/function_types.go:255-341` (`predeclareGlobalBindings`), `:350` (`predeclareStructs`)
- Modify: `internal/compiler/compiler.go:112-121, 225-235` (`sessionLets`/`programLets` → bindings com espécie)
- Modify: `cmd/noxy/main.go:246, 338, 357-359` (REPL registra todas as espécies)
- Test: `internal/compiler/let_redeclaration_test.go`

**Interfaces:**
- Produces: `type globalDecl struct { Kind string; Line int }` (Kind ∈ `"variable" | "function" | "struct" | "import"`); `func (c *Compiler) SetSessionBindings(map[string]globalDecl)` e `func (c *Compiler) ProgramBindings() map[string]globalDecl`; `SetSessionLets`/`ProgramLets` continuam existindo (adaptadores sobre os novos, Kind `"variable"`).
- Mensagem: `[line N] 'x' redeclared in this scope (previous declaration as <kind> at line M)`. As mensagens já fixadas por teste **não mudam**: let×let mantém `variable 'x' redeclared in this scope (previous declaration at line M); hint: …` (`function_types.go:296-305`) e func×func mantém `duplicate function 'f'` (`:266`).

- [ ] **Step 1: Testes RED** em `internal/compiler/let_redeclaration_test.go`:

```go
func TestGlobalLetOverFunctionFails(t *testing.T) {
    requireRedeclarationError(t, "func x() -> int\n    return 1\nend\nlet x: int = 42\n", "previous declaration as function at line 1")
}

func TestGlobalFunctionOverLetFails(t *testing.T) {
    requireRedeclarationError(t, "let x: int = 42\nfunc x() -> int\n    return 1\nend\n", "previous declaration as variable at line 1")
}

func TestGlobalStructOverFunctionFails(t *testing.T) {
    requireRedeclarationError(t, "func P() -> int\n    return 1\nend\nstruct P\n    x: int\nend\n", "previous declaration as function at line 1")
}

func TestGlobalLetOverSelectiveImportFails(t *testing.T) {
    requireRedeclarationError(t, "use array_utils select range\nlet range: int = 5\n", "previous declaration as import at line 1")
}

func TestGlobalLetOverNamespaceImportFails(t *testing.T) {
    requireRedeclarationError(t, "use strings\nlet strings: int = 5\n", "previous declaration as import at line 1")
}

func TestReplSessionFunctionThenLetFails(t *testing.T) {
    first := New()
    if _, _, err := first.Compile(parse("func f() -> int\n    return 1\nend\n")); err != nil {
        t.Fatalf("first line: %v", err)
    }
    session := first.ProgramBindings()
    second := NewWithState(first.GetGlobals(), first.GetStructs(), "REPL")
    second.SetSessionBindings(session)
    _, _, err := second.Compile(parse("let f: int = 2\n"))
    if err == nil || !strings.Contains(err.Error(), "previous declaration as function") {
        t.Fatalf("want function/let collision across REPL lines, got %v", err)
    }
}

func TestReplSessionFunctionRedefinitionAllowed(t *testing.T) {
    first := New()
    if _, _, err := first.Compile(parse("func f() -> int\n    return 1\nend\n")); err != nil {
        t.Fatalf("first line: %v", err)
    }
    second := NewWithState(first.GetGlobals(), first.GetStructs(), "REPL")
    second.SetSessionBindings(first.ProgramBindings())
    if _, _, err := second.Compile(parse("func f() -> int\n    return 2\nend\n")); err != nil {
        t.Fatalf("redefining a function across REPL lines must stay allowed, got %v", err)
    }
}
```

(Verificar o nome real do getter de structs em `compiler.go` — `GetStructs()` ou equivalente usado por `cmd/noxy/main.go:353`; usar o existente.)

- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/compiler -run 'TestGlobal|TestReplSession' -v` → os 7 novos falham (compilam sem erro ou método inexistente).

- [ ] **Step 3: Implementar**

Em `function_types.go`, substituir `seen`/`letSeen` por um único mapa:

```go
type globalDecl struct {
    Kind string
    Line int
}

func (c *Compiler) declareGlobalName(declared map[string]globalDecl, name, kind string, line int) error {
    if prev, ok := declared[name]; ok {
        if kind == "variable" && prev.Kind == "variable" {
            return fmt.Errorf("[line %d] variable '%s' redeclared in this scope (previous declaration at line %d); hint: to update the value, use '%s = ...' without 'let'", line, name, prev.Line, name)
        }
        if kind == "function" && prev.Kind == "function" {
            return fmt.Errorf("[line %d] duplicate function '%s'", line, name)
        }
        return fmt.Errorf("[line %d] '%s' redeclared in this scope (previous declaration as %s at line %d)", line, name, prev.Kind, prev.Line)
    }
    if prev, ok := c.sessionBindings[name]; ok && !(kind == "function" && prev.Kind == "function") {
        if kind == "variable" && prev.Kind == "variable" {
            return fmt.Errorf("[line %d] variable '%s' redeclared in this scope (previous declaration at line %d); hint: to update the value, use '%s = ...' without 'let'", line, name, prev.Line, name)
        }
        return fmt.Errorf("[line %d] '%s' redeclared in this scope (previous declaration as %s at line %d)", line, name, prev.Kind, prev.Line)
    }
    declared[name] = globalDecl{Kind: kind, Line: line}
    c.programBindingsByKind[name] = globalDecl{Kind: kind, Line: line}
    return nil
}
```

Regra de sessão (REPL): redefinir **função por função** entre linhas continua permitido (iteração no REPL, documentar na spec §3 "Redeclaration vs. reassignment"); tudo o mais colide. Em `predeclareGlobalBindings`:
- `UseStmt`: para `select a, b` → `declareGlobalName(declared, sel, "import", line)` por seletor; `select *` → nomes de `c.discoverModuleExports(module)` (já existe, `module_exports.go:89`); forma simples/alias → o nome local (`Alias` ou último segmento). Chamar **antes** de `c.predeclareImport`.
- `FunctionStatement` → `"function"` (templates genéricos também declaram o nome).
- `LetStmt` → `"variable"` (manter o registro em `c.programLets`).
- `StructStatement` → `"struct"` (templates também).

Em `compiler.go`: campo `sessionBindings map[string]globalDecl` e `programBindingsByKind map[string]globalDecl` (inicializar em `New`/`NewWithState`), `SetSessionBindings`, `ProgramBindings`; `SetSessionLets(m map[string]int)` vira adaptador (`Kind: "variable"`), `ProgramLets` filtra `Kind == "variable"`. Manter os testes `TestReplSessionReLetFails`/`TestReplFailedLineDoesNotBurnTheName` passando.

Em `cmd/noxy/main.go`: `replLets := make(map[string]int)` → `replBindings := make(map[string]compiler.GlobalDecl)` (exportar o tipo como `GlobalDecl`), `c.SetSessionBindings(replBindings)`, e no sucesso `for name, d := range c.ProgramBindings() { replBindings[name] = d }`.

- [ ] **Step 4: Rodar** — `go test ./internal/compiler ./cmd/... -count=1` → PASS (incluindo `TestRejectsDuplicateTopLevelFunctionNames` e os de #46).

- [ ] **Step 5: Commit** — `feat(compiler): namespace global unico — let x func x struct x import colidem em compilacao (issue #47 parte 2)`.

### Task 3: Global inexistente é erro de compilação (semeado pelo embutidor)

**Files:**
- Create: `internal/compiler/known_globals.go`
- Modify: `internal/compiler/compiler.go:1154-1180` (Identifier), `:696-698` (atribuição a nome desconhecido)
- Modify: `internal/vm/vm.go` (novo `GlobalNames()`), `internal/vm/modules.go:191-231`, `cmd/noxy/main.go:229-340, 386-426`
- Modify: `internal/vm/dynamic_boundary_test.go:93`, `internal/vm/references_test.go:137`
- Test: `internal/compiler/known_globals_test.go`

**Interfaces:**
- Produces: `func (c *Compiler) SetKnownGlobals(names []string)` — ativa o check; `func PluginNativeNames(program *ast.Program) []string` — para cada `sys_load_plugin("<lit>", …)` no AST devolve `"<lit>_request"`; `func (vm *VM) GlobalNames() []string` — nomes do `shared.Root` (adicionar `func (e *GlobalEnvironment) LocalNames() []string` em `internal/value/environment.go`).
- Mensagens: `[line N] undefined global 'x'\n  hint: declare it with 'let x = ...' or check the spelling` (leitura) e `[line N] cannot assign to undeclared name 'x'\n  hint: declare it with 'let x = ...'` (escrita).

- [ ] **Step 1: Testes RED** em `internal/compiler/known_globals_test.go`:

```go
package compiler

import (
    "strings"
    "testing"
)

func compileKnown(t *testing.T, src string, known ...string) error {
    t.Helper()
    c := New()
    c.SetKnownGlobals(append([]string{"print", "length"}, known...))
    _, _, err := c.Compile(parse(src))
    return err
}

func TestUndefinedGlobalBehindBranchIsCompileError(t *testing.T) {
    err := compileKnown(t, "let cond: bool = false\nif cond then\n    print(typo_global)\nend\n")
    want := "[line 3] undefined global 'typo_global'\n  hint: declare it with 'let typo_global = ...' or check the spelling"
    if err == nil || !strings.Contains(err.Error(), want) {
        t.Fatalf("want %q, got %v", want, err)
    }
}

func TestAssignToUndeclaredNameIsCompileError(t *testing.T) {
    err := compileKnown(t, "i = 0\n")
    if err == nil || !strings.Contains(err.Error(), "cannot assign to undeclared name 'i'") {
        t.Fatalf("got %v", err)
    }
}

func TestForwardReferenceInsideFunctionStillCompiles(t *testing.T) {
    if err := compileKnown(t, "func f() -> int\n    return later\nend\nlet later: int = 1\nprint(f())\n"); err != nil {
        t.Fatalf("forward reference must compile: %v", err)
    }
}

func TestKnownNativeAndPluginNamesCompile(t *testing.T) {
    src := "let loaded: bool = sys_load_plugin(\"dynamodb\", \"bin\")\nlet r: any = dynamodb_request(\"connect\", 1)\n"
    program := parse(src)
    c := New()
    c.SetKnownGlobals(append([]string{"sys_load_plugin"}, PluginNativeNames(program)...))
    if _, _, err := c.Compile(program); err != nil {
        t.Fatalf("plugin native must be known: %v", err)
    }
}

func TestWithoutSeedCompilerStaysPermissive(t *testing.T) {
    if _, _, err := New().Compile(parse("print(whatever)\n")); err != nil {
        t.Fatalf("unseeded compiler must not check globals: %v", err)
    }
}
```

- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/compiler -run 'Undefined|Undeclared|ForwardReference|KnownNative|WithoutSeed' -v`.

- [ ] **Step 3: Implementar**

`internal/compiler/known_globals.go`:

```go
package compiler

import (
    "fmt"

    "noxy-vm/internal/ast"
)

// knownGlobals e o conjunto de nomes que o embutidor garante existir em
// runtime (nativos da VM, extensoes, plugins). nil = check desligado:
// compiler.New() puro continua permissivo, como sempre foi.
func (c *Compiler) SetKnownGlobals(names []string) {
    c.knownGlobals = make(map[string]struct{}, len(names))
    for _, name := range names {
        c.knownGlobals[name] = struct{}{}
    }
}

func (c *Compiler) globalIsKnown(name string) bool {
    if c.knownGlobals == nil {
        return true
    }
    if _, ok := c.globals[name]; ok {
        return true
    }
    if _, ok := c.programBindings[name]; ok {
        return true
    }
    if _, ok := c.knownGlobals[name]; ok {
        return true
    }
    if _, ok := c.namespaceImports[name]; ok {
        return true
    }
    return false
}

func (c *Compiler) undefinedGlobalError(name string, line int) error {
    return fmt.Errorf("[line %d] undefined global '%s'\n  hint: declare it with 'let %s = ...' or check the spelling", line, name, name)
}

// PluginNativeNames devolve os nativos que sys_load_plugin("<nome>", ...)
// registra em runtime ("<nome>_request"), lendo o literal no AST — e o unico
// caso em que um global nasce depois da compilacao do proprio arquivo.
func PluginNativeNames(program *ast.Program) []string {
    var names []string
    ast.Walk(program, func(node ast.Node) bool {
        call, ok := node.(*ast.CallExpression)
        if !ok {
            return true
        }
        ident, ok := call.Function.(*ast.Identifier)
        if !ok || ident.Value != "sys_load_plugin" || len(call.Arguments) == 0 {
            return true
        }
        if lit, ok := call.Arguments[0].(*ast.StringLiteral); ok {
            names = append(names, lit.Value+"_request")
        }
        return true
    })
    return names
}
```

Se `ast.Walk` não existir, escrever um walker mínimo em `internal/ast/walk.go` cobrindo statements/expressions (ou reutilizar o walker de `validateImportedTemplateScope`, `generics.go:829`, que já percorre identificadores livres — verificar o nome do helper e reaproveitar).

Herança: `NewChild` (`compiler.go:206`) e o compilador de instâncias genéricas (`generics.go:659`) devem copiar `knownGlobals` do pai.

`compiler.go:1154-1180` (Identifier): antes de `c.makeConstant` no ramo global: `if !c.globalIsKnown(n.Value) { return nil, nil, c.undefinedGlobalError(n.Value, c.currentLine) }`. `compiler.go:696-698` (atribuição): `if !c.globalIsKnown(name) { return ... fmt.Errorf("[line %d] cannot assign to undeclared name '%s'\n  hint: declare it with 'let %s = ...'", ...) }`. Também `OP_REF_GLOBAL`/`OP_GET_GLOBAL_MUT` (busca por `OP_REF_GLOBAL` e `OP_GET_GLOBAL_MUT` em compiler.go/explicit_ref.go — mesma guarda).

`internal/value/environment.go`: `func (environment *GlobalEnvironment) LocalNames() []string` (chaves do mapa local, sob o mesmo lock de `LocalSnapshot`). `internal/vm/vm.go`: `func (vm *VM) GlobalNames() []string { return vm.shared.Root.LocalNames() }`.

`internal/vm/modules.go:205`: após criar `c`, `c.SetKnownGlobals(append(vm.GlobalNames(), compiler.PluginNativeNames(program)...))` — os nativos de extensão já foram registrados em `:139`.

`cmd/noxy/main.go`: em `runWithConfig`, criar a VM **antes** do compilador (mover `:416` para antes de `:398`) e chamar `c.SetKnownGlobals(append(machine.GlobalNames(), compiler.PluginNativeNames(program)...))`; no REPL (`:336`) idem por linha (a VM já existe em `:229`).

Testes da VM que fixavam o erro de runtime: `dynamic_boundary_test.go:93` e `references_test.go:137` continuam válidos (usam `compiler.New()` sem semente) — só conferir; **não** remover: eles agora documentam o contrato do embutidor.

- [ ] **Step 4: Rodar** — `go test ./... -count=1`; `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx`. Se algum exemplo referenciar um nome que a VM não define (typo real no corpus), corrigir o exemplo — é o efeito desejado.

- [ ] **Step 5: Commit** — `feat(compiler): global inexistente e erro de compilacao quando o embutidor semeia os nativos (issue #47 parte 3)`.

---

## Parte A — Nulidade

### Task 4: Token `?`, nó `ast.NullableType`, parser

**Files:**
- Modify: `internal/token/token.go` (const `QUESTION`), `internal/lexer/lexer.go:42` (case `'?'`), `internal/ast/ast.go:118` (nó), `internal/ast/clone.go:150`, `internal/parser/parser.go:548-600, 624-648`
- Test: `internal/parser/parser_test.go`, `internal/lexer/lexer_test.go`

**Interfaces:**
- Produces: `ast.NullableType{ElementType NoxyType}`; `String()` = `elem.String() + "?"`, parenthesizando `func`/`chan` (`(func(int) -> int)?`); `RefType.String()` com elemento `NullableType` imprime `ref (Node?)`. Gramática: `?` é pós-fixo; **sem** `ref`, alterna livremente com `[]` (`Node?[]`, `Node[]?`); **com** `ref`, só depois do `ref` inteiro (`ref Node?` = referência anulável; `ref (Node?)` = referência a slot anulável).
- Erros de sintaxe: `SyntaxError: type is already nullable` (`Node??`), `SyntaxError: 'any' already admits null` (`any?`), `SyntaxError: 'void' cannot be nullable`, `null?` → `SyntaxError: expected type, found null` (já existe).

- [ ] **Step 1: Testes RED**

`internal/lexer/lexer_test.go`: caso `"Node?"` → tokens `IDENTIFIER Node`, `QUESTION ?`.

`internal/parser/parser_test.go`:

```go
func TestNullableTypeSyntax(t *testing.T) {
    cases := []struct{ src, want string }{
        {"let a: Node? = null", "Node?"},
        {"let b: ref Node? = null", "ref Node?"},
        {"let c: ref (Node?) = ref x", "ref (Node?)"},
        {"let d: Node?[] = []", "Node?[]"},
        {"let e: int[]? = null", "int[]?"},
        {"let f: (func(int) -> int)? = null", "(func(int) -> int)?"},
        {"let g: map[string, Node?] = {}", "map[string, Node?]"},
    }
    for _, tc := range cases {
        program := parseOK(t, tc.src)
        let := program.Statements[0].(*ast.LetStmt)
        if got := let.Type.String(); got != tc.want {
            t.Errorf("%s: type %q, want %q", tc.src, got, tc.want)
        }
    }
}

func TestNullableTypeErrors(t *testing.T) {
    cases := []struct{ src, want string }{
        {"let a: Node?? = null", "type is already nullable"},
        {"let b: any? = null", "'any' already admits null"},
        {"func f() -> void?\nend", "'void' cannot be nullable"},
    }
    for _, tc := range cases {
        errs := parseErrors(t, tc.src)
        if len(errs) == 0 || !strings.Contains(errs[0], tc.want) {
            t.Errorf("%s: want %q, got %v", tc.src, tc.want, errs)
        }
    }
}
```

(`parseOK`/`parseErrors`: usar os helpers existentes de `parser_test.go`; se não houver, criar com `lexer.New` + `parser.New` + `ParseProgram` + `p.Errors()`.)

- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/lexer ./internal/parser ./internal/ast -count=1`.

- [ ] **Step 3: Implementar**

`token.go`: `QUESTION TokenType = "QUESTION"` junto dos delimitadores. `lexer.go` (switch de `NextToken`): `case '?': tok = l.makeToken(token.QUESTION)` (mesmo padrão de `','`).

`ast.go`, após `RefType`:

```go
// NullableType e `T?`: o tipo T ou null. `T` nu nunca e null (fase 2).
type NullableType struct {
    ElementType NoxyType
}

func (t *NullableType) String() string {
    switch t.ElementType.(type) {
    case *FunctionType, *ChanType:
        return "(" + t.ElementType.String() + ")?"
    }
    return t.ElementType.String() + "?"
}
```

`RefType.String()`: se `ElementType` é `*NullableType`, devolver `"ref (" + elem + ")"`. `ArrayType.String()` já parenteseia `ref`; `Node?[]` sai natural.

`clone.go:150`: `case *NullableType: return &NullableType{ElementType: CloneType(t.ElementType)}`.

`parser.go` — em `parseType()` (`:548`):

```go
func (p *Parser) parseType() ast.NoxyType {
    hasRef := false
    if p.curTokenIs(token.REF) {
        hasRef = true
        p.nextToken()
        if p.curTokenIs(token.REF) { /* erro ref ref existente */ }
    }
    result := p.parseAtomicType()
    // pos-fixos: [] sempre; ? so quando nao ha ref (com ref, ? fica para o fim)
    for {
        if p.peekTokenIs(token.LBRACKET) { /* codigo existente de array */ continue }
        if !hasRef && p.peekTokenIs(token.QUESTION) {
            p.nextToken()
            result = p.wrapNullable(result)
            if result == nil { return nil }
            continue
        }
        break
    }
    if hasRef {
        result = &ast.RefType{ElementType: result}
        for p.peekTokenIs(token.QUESTION) {
            p.nextToken()
            result = p.wrapNullable(result)
            if result == nil { return nil }
        }
    }
    return result
}

func (p *Parser) wrapNullable(t ast.NoxyType) ast.NoxyType {
    switch typed := t.(type) {
    case *ast.NullableType:
        p.errors = append(p.errors, fmt.Sprintf("SyntaxError: type is already nullable (line %d)", p.curToken.Line))
        return nil
    case *ast.PrimitiveType:
        if typed.Name == "any" {
            p.errors = append(p.errors, fmt.Sprintf("SyntaxError: 'any' already admits null (line %d)", p.curToken.Line))
            return nil
        }
        if typed.Name == "void" {
            p.errors = append(p.errors, fmt.Sprintf("SyntaxError: 'void' cannot be nullable (line %d)", p.curToken.Line))
            return nil
        }
    }
    return &ast.NullableType{ElementType: t}
}
```

Adaptar ao formato real de erro do parser (ver como `parseAtomicType` reporta em `:758`). `hasInvalidVoidPosition` (`:624`): `case *ast.NullableType: return hasInvalidVoidPosition(typed.ElementType, false)`.

- [ ] **Step 4: Rodar** — `go test ./internal/lexer ./internal/parser ./internal/ast -count=1` → PASS (incluindo `TestClonerCoversEveryNode`; se `TestNodeStringGolden` pedir entrada, adicionar `NullableType`).

- [ ] **Step 5: Commit** — `feat(parser): tipo anulavel T? / ref T? (issue #105 item 1, fase 1)`.

### Task 5: `NullableType` em todos os walkers de tipo + compatibilidade (fase 1: `T?` entra, default não muda)

**Files:**
- Create: `internal/compiler/nullable.go`
- Modify: os 20 `switch` com `case *ast.RefType` em `internal/compiler/` (lista abaixo), `function_types.go:41` (`acceptsNull`), `compiler.go:3194-3233` (`areTypesCompatible`), `function_types.go:198-246` (`areStrictTypesCompatible`), `compiler.go:3238` (`typesEquivalent`), `generics_unify.go:75-150`, `generics_target.go:370`
- Modify: `internal/value/value.go:105-170` (`RuntimeTypeInfo.Nullable`), `internal/compiler/runtime_types.go:45-186`, `internal/vm/runtime_type_validation.go:32, 105-114`
- Test: `internal/compiler/nullable_test.go`, `internal/vm/nullable_runtime_test.go`

**Interfaces:**
- Produces (`nullable.go`): `func isNullable(t ast.NoxyType) bool`; `func nonNull(t ast.NoxyType) (ast.NoxyType, bool)` (elemento, true se era `T?`); `func nullable(t ast.NoxyType) ast.NoxyType` (idempotente; `any`/`null` voltam iguais); `func asRefType(t ast.NoxyType) (*ast.RefType, bool)` (desembrulha `NullableType`).
- Regras: `areTypesCompatible(T?, T)` ✓, `(T?, null)` ✓, `(T?, U?)` ⇔ `(T, U)`, `(T, T?)` ✗ (e `areStrict…` idem); `typesEquivalent(T?, U?)` ⇔ `(T, U)`; unify: padrão `T?` × atual `X` ou `X?` binda `T = X`; padrão `T` × atual `X?` binda `T = X?` (permitido); `null` não binda. Runtime: `RuntimeTypeInfo.Nullable bool`, `String()` acrescenta `?`; `runtimeValueMatchesType` para `VAL_NULL` = `expected.Nullable || Kind ∈ {TYPE_NULL, TYPE_ANY, TYPE_REF, TYPE_STRUCT}` (os dois últimos saem na Task 8).

- [ ] **Step 1: Testes RED** — `internal/compiler/nullable_test.go`:

```go
func TestNullableAssignmentsCompile(t *testing.T) {
    src := `struct Node
    valor: int
    prox: Node?
end
let a: Node? = null
let b: Node? = Node(1, null)
let c: Node = Node(2, null)
a = c
let r: ref Node? = null
r = ref c
let xs: Node?[] = [null, c]
func busca(k: int) -> Node?
    return null
end
let d = busca(1)
`
    if _, err := compileFunctionSource(t, src); err != nil {
        t.Fatalf("%v", err)
    }
}

func TestNullableIntoNonNullIsRejected(t *testing.T) {
    src := "struct P\n    x: int\nend\nlet a: P? = null\nlet b: P = a\n"
    _, err := compileFunctionSource(t, src)
    want := "[line 5] type mismatch in 'b' declaration: expected P, got P?\n  hint: 'a' may be null; test it first"
    if err == nil || !strings.Contains(err.Error(), want) {
        t.Fatalf("want %q, got %v", want, err)
    }
}

func TestGenericBindsNullable(t *testing.T) {
    src := "struct P\n    x: int\nend\nfunc first<T>(xs: T[]) -> T\n    return xs[0]\nend\nlet ps: P?[] = [null]\nlet p: P? = first(ps)\n"
    if _, err := compileFunctionSource(t, src); err != nil {
        t.Fatalf("T must bind P?: %v", err)
    }
}
```

`internal/vm/nullable_runtime_test.go`:

```go
func TestNullableRuntimeAcceptsNullAndValue(t *testing.T) {
    src := `struct P
    x: int
end
let a: any = null
let b: P? = a
let c: any = P(3)
let d: P? = c
test_report(to_str(b == null) + "|" + to_str(d.x))
`
    got := captureVMSource(t, src)
    if got.String() != "true|3" {
        t.Fatalf("got %s", got.String())
    }
}
```

(`d.x` com `d: P?` só compila com narrowing — na Task 5 ainda não há erro "may be null"; esse teste é reescrito na Task 6 com `if d != null then`. Deixar como está aqui.)

- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/compiler -run Nullable -v; go test ./internal/vm -run NullableRuntime -v`.

- [ ] **Step 3: Implementar**

`nullable.go` com os quatro helpers (`nullable` devolve `t` se `isAny(t)`, `isNullType(t)` ou já `*ast.NullableType`).

Checklist dos walkers — em cada `switch` abaixo, acrescentar `case *ast.NullableType:` que recorre no elemento (ou constrói `&ast.NullableType{ElementType: <resultado da recursao>}` quando o walker devolve um tipo):
`compiler.go:3259` (`typesEquivalent`), `:3345` (`stripTypeQualifiers` — **não** remove `?`; `looselySameType` compara elementos), `function_types.go:92` (`containsCallableType`), `:179` (`sameExactType`), `:240` (strict compat — ver abaixo), `generics_structs.go:96/142/457/504`, `generics_substitute.go:49` (`substituteType`: normalizar `Nullable(Nullable)` e `Nullable(any)` via `nullable()`), `generics_target.go:313`, `generics_unify.go:138/215`, `let_inference.go:94/120`, `member_types.go:110` (`programViewType`), `runtime_types.go:51` (`requiresRuntimeValueType`: `T?` exige se `T` exige **ou** sempre? → sempre que o elemento exigir), `:147` (`runtimeTypeInfoWithStructs`: `info := recurse(elem); info.Nullable = true`), `:299`, `typed_index.go:124`.

`resolveAnnotation` (`generics_structs.go:45-73`) e `resolveStructFieldAnnotations` devem recorrer em `NullableType` para resolver `Result<int>?`.

`acceptsNull` (`function_types.go:41`): `if _, ok := t.(*ast.NullableType); ok { return true }` no topo (fase 1 mantém o resto).

`areTypesCompatible(expected, actual)` — no início:

```go
if expectedElem, ok := nonNull(expected); ok {
    if isNullType(actual) { return true }
    if actualElem, ok := nonNull(actual); ok { return c.areTypesCompatible(expectedElem, actualElem) }
    return c.areTypesCompatible(expectedElem, actual)
}
if _, actualNullable := nonNull(actual); actualNullable && !isAny(expected) {
    return false
}
```

Idem em `areStrictTypesCompatible`. Nos sites de mensagem de mismatch (`compiler.go:420` let, `:620/:689` atribuição, `:875` campo, `:763` índice, `:2164` return, `:2544` argumento), acrescentar o hint quando `isNullable(actualType)` e `!isNullable(expected)`: helper `c.mayBeNullHint(expr ast.Expression) string` → `"\n  hint: '<key>' may be null; test it first"` (key = `stableKey(expr)` da Task 6; se não estável, `"\n  hint: the value may be null; bind it with 'let' and test it"`). Na Task 5, `stableKey` ainda não existe: usar `expr.String()` para identificador e refinar na Task 6.

`generics_unify.go:75` (`unify`): `case *ast.NullableType` no padrão esperado: `actual` `null` → sem binding; `actual` `NullableType` → unify(elem, elem); senão unify(elem, actual). Em `bindTypeParam` (`generics_target.go:370`) o mesmo; **não** proibir `T = X?`.

`value.go:105`: campo `Nullable bool` em `RuntimeTypeInfo`; `String()` (`:122-170`) acrescenta `"?"` ao fim (parentesear `func`/`chan` como no AST). `runtime_type_validation.go:112`: `if actual.Type == value.VAL_NULL { return expected.Nullable || expected.Kind == value.TYPE_NULL || expected.Kind == value.TYPE_ANY || expected.Kind == value.TYPE_REF || expected.Kind == value.TYPE_STRUCT }`; `appendItemCompatible` (`:32`) usa a mesma regra.

`compiler.go:3533` (`ParamInfo.TypeName`) recebe `"Node?"` — `call_validation.go:52` compara só com `"any"`; nada a fazer.

- [ ] **Step 4: Rodar** — `go test ./... -count=1` → PASS; `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx` → 100% (nada no corpus usa `?` ainda).

- [ ] **Step 5: Commit** — `feat(compiler): NullableType em todos os walkers, compatibilidade T?/T e espelho de runtime (issue #105 item 1, fase 1)`.

### Task 6: Narrowing por expressões estáveis

**Files:**
- Create: `internal/compiler/narrowing.go`, `internal/compiler/narrowing_test.go`
- Modify: `internal/compiler/compiler.go:12-35` (`Local.RefTaken`), `:1154` (Identifier), `:982-1007` (MemberAccess), `:1483-1500` (`*`), `:1202/1234` (`&&`/`||`), `:1547-1602` (`if`), laços `while`/`for` (buscar `case *ast.WhileStatement` / `*ast.ForStatement`), atribuições (`:560-700`, `:740-880`), `compileCallExpression` (`:2288`, ao fim), `compileReferenceArgument` (marca `RefTaken`)
- Fixtures: `noxy_examples/type_errors/nullable_member_without_test.nx`, `nullable_deref_without_test.nx`, `nullable_after_call_through_ref.nx`

**Interfaces:**
- Produces: `func stableKey(expr ast.Expression) (string, bool)` — `Identifier` → nome; `PrefixExpression{"*", stable}` → `"*"+key`; `MemberAccessExpression{stable, f}` → `key+"."+f`; qualquer outra coisa → `("", false)`.
- `func conditionFacts(cond ast.Expression) (then, els []string)`: `e != null`/`null != e` → then=[k]; `e == null` → els=[k]; `a && b` → then = then(a)+then(b), els = ∅; `a || b` → els = els(a)+els(b), then = ∅; `!e` → troca; `k.ok` com base de tipo `Result<…>` → then=[`k.value`] (usado na Parte B; deixar o gancho `c.resultValueKey(expr)` devolvendo `""` até a Task 12).
- `c.narrowed map[string]struct{}`; `func (c *Compiler) narrowType(key string, declared ast.NoxyType) ast.NoxyType` — se `key ∈ narrowed`, `nonNull(declared)`; `func (c *Compiler) pushFacts(keys []string) (restore func())` (snapshot + add; restore repõe o mapa); `func (c *Compiler) dropKey(key string)` (remove `key` e todo `k` com prefixo `key+"."`, `"*"+key`); `func (c *Compiler) dropCompound()` (remove toda chave com `.` ou `*`); `func (c *Compiler) dropAfterCall()` — remove chaves compostas **e** identificadores cujo Local é `ref` (`asRefType(Type)`), capturado (`IsCaptured`), com `RefTaken`, upvalue ou global; **preserva** chaves compostas cuja raiz é local valor não-capturado sem `RefTaken`; `func (c *Compiler) dropForLoop(body *ast.BlockStatement)` — remove raízes atribuídas no corpo (`assignedRoots(body)`: alvos de `AssignStatement`, `ref x…` como argumento, `for x in`), e se o corpo tiver `CallExpression`, aplica `dropAfterCall`.
- `func blockTerminates(block *ast.BlockStatement) bool` — último statement é `ReturnStmt`, `BreakStatement`, `ContinueStatement`, `ExpressionStatement` chamando `exit`/`sys_exit`, `IfStatement` com os dois ramos terminando, `WhenStatement` com todos os cases terminando, ou `BlockStatement` que termina.
- Erros: `[line N] '<key>' may be null; test it first\n  hint: use 'if <key> != null then ... end'` (estável) e `[line N] value of type <T?> may be null; test it first\n  hint: bind it with 'let' and test for null` (não estável). Emitidos em: acesso a membro com base `T?` (inclui atalho `r.f` com `r: ref T?`), `*r` com `r: ref T?`, índice `x[i]` com `x: T[]?`, `for v in x` com `x` anulável, chamada de valor `func?`, condição `bool?`, operando de operador (via mismatch + hint).

- [ ] **Step 1: Testes RED** — `narrowing_test.go`:

```go
func TestNarrowingIfNotNull(t *testing.T) {
    src := "struct P\n    x: int\nend\nfunc f(p: P?) -> int\n    if p != null then\n        return p.x\n    end\n    return 0\nend\n"
    if _, err := compileFunctionSource(t, src); err != nil { t.Fatal(err) }
}

func TestNarrowingEarlyReturn(t *testing.T) {
    src := "struct P\n    x: int\nend\nfunc f(p: P?) -> int\n    if p == null then\n        return -1\n    end\n    return p.x\nend\n"
    if _, err := compileFunctionSource(t, src); err != nil { t.Fatal(err) }
}

func TestNarrowingAndOperator(t *testing.T) {
    src := "struct P\n    x: int\nend\nfunc f(p: P?) -> bool\n    return p != null && p.x > 0\nend\n"
    if _, err := compileFunctionSource(t, src); err != nil { t.Fatal(err) }
}

func TestNarrowingWhileTraversal(t *testing.T) {
    src := "struct Node\n    valor: int\n    prox: Node?\nend\nfunc soma(inicio: Node?) -> int\n    let total: int = 0\n    let atual: Node? = inicio\n    while atual != null do\n        total = total + atual.valor\n        atual = atual.prox\n    end\n    return total\nend\n"
    if _, err := compileFunctionSource(t, src); err != nil { t.Fatal(err) }
}

func TestNarrowingFieldPathThroughRef(t *testing.T) {
    src := "struct Node\n    valor: int\n    prox: Node?\nend\nfunc insere(no: ref Node, v: int) -> void\n    if no.prox == null then\n        no.prox = Node(v, null)\n    else\n        insere(ref no.prox, v)\n    end\nend\n"
    if _, err := compileFunctionSource(t, src); err != nil { t.Fatal(err) }
}

func TestNarrowingLostAfterAssignment(t *testing.T) {
    src := "struct P\n    x: int\nend\nfunc f(p: P?, q: P?) -> int\n    if p != null then\n        p = q\n        return p.x\n    end\n    return 0\nend\n"
    _, err := compileFunctionSource(t, src)
    want := "[line 6] 'p' may be null; test it first\n  hint: use 'if p != null then ... end'"
    if err == nil || !strings.Contains(err.Error(), want) { t.Fatalf("want %q, got %v", want, err) }
}

func TestNarrowingPathLostAfterCallThroughRef(t *testing.T) {
    src := "struct Node\n    valor: int\n    prox: Node?\nend\nfunc toca(n: ref Node) -> void\n    n.prox = null\nend\nfunc f(no: ref Node) -> int\n    if no.prox != null then\n        toca(no)\n        return no.prox.valor\n    end\n    return 0\nend\n"
    _, err := compileFunctionSource(t, src)
    if err == nil || !strings.Contains(err.Error(), "'no.prox' may be null") { t.Fatalf("got %v", err) }
}

func TestNarrowingPathSurvivesCallOnValueLocal(t *testing.T) {
    src := "struct Node\n    valor: int\n    prox: Node?\nend\nfunc f(no: Node) -> int\n    if no.prox != null then\n        print(no.prox.valor)\n        return no.prox.valor\n    end\n    return 0\nend\n"
    if _, err := compileFunctionSource(t, src); err != nil { t.Fatal(err) }
}

func TestDerefNullableRefNeedsTest(t *testing.T) {
    src := "func f(r: ref int?) -> int\n    return *r\nend\n"
    _, err := compileFunctionSource(t, src)
    if err == nil || !strings.Contains(err.Error(), "'r' may be null; test it first") { t.Fatalf("got %v", err) }
}

func TestUnstableNullableNeedsLet(t *testing.T) {
    src := "struct P\n    x: int\nend\nfunc g() -> P?\n    return null\nend\nlet v: int = g().x\n"
    _, err := compileFunctionSource(t, src)
    want := "value of type P? may be null; test it first\n  hint: bind it with 'let' and test for null"
    if err == nil || !strings.Contains(err.Error(), want) { t.Fatalf("want %q, got %v", want, err) }
}
```

Fixtures em `noxy_examples/type_errors/` e entradas na tabela de `TestTypedFunctionInvalidConformanceExamplesFail`:
- `nullable_member_without_test.nx` → sufixo `'p' may be null; test it first\n  hint: use 'if p != null then ... end'`
- `nullable_deref_without_test.nx` → `'r' may be null; test it first\n  hint: use 'if r != null then ... end'`

- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/compiler -run 'Narrowing|Deref|Unstable|Conformance' -v`.

- [ ] **Step 3: Implementar `narrowing.go`**

```go
package compiler

import (
    "fmt"
    "strings"

    "noxy-vm/internal/ast"
)

func stableKey(expr ast.Expression) (string, bool) {
    switch e := expr.(type) {
    case *ast.Identifier:
        return e.Value, true
    case *ast.PrefixExpression:
        if e.Operator != "*" { return "", false }
        inner, ok := stableKey(e.Right)
        if !ok { return "", false }
        return "*" + inner, true
    case *ast.MemberAccessExpression:
        base, ok := stableKey(e.Left)
        if !ok { return "", false }
        return base + "." + e.Member, true
    }
    return "", false
}

func isNullLiteral(e ast.Expression) bool { _, ok := e.(*ast.NullLiteral); return ok }

func (c *Compiler) conditionFacts(cond ast.Expression) (then, els []string) {
    switch e := cond.(type) {
    case *ast.InfixExpression:
        switch e.Operator {
        case "!=", "==":
            var subject ast.Expression
            if isNullLiteral(e.Right) { subject = e.Left } else if isNullLiteral(e.Left) { subject = e.Right }
            if subject == nil { return nil, nil }
            key, ok := stableKey(subject)
            if !ok { return nil, nil }
            if e.Operator == "!=" { return []string{key}, nil }
            return nil, []string{key}
        case "&&":
            lt, _ := c.conditionFacts(e.Left)
            rt, _ := c.conditionFacts(e.Right)
            return append(lt, rt...), nil
        case "||":
            _, le := c.conditionFacts(e.Left)
            _, re := c.conditionFacts(e.Right)
            return nil, append(le, re...)
        }
    case *ast.PrefixExpression:
        if e.Operator == "!" {
            t, f := c.conditionFacts(e.Right)
            return f, t
        }
    case *ast.MemberAccessExpression:
        if key := c.resultValueKey(e); key != "" { // Parte B: `if r.ok then` estreita r.value
            return []string{key}, nil
        }
    }
    return nil, nil
}

func (c *Compiler) narrowType(key string, declared ast.NoxyType) ast.NoxyType {
    if declared == nil { return nil }
    if _, ok := c.narrowed[key]; !ok { return declared }
    if elem, ok := nonNull(declared); ok { return elem }
    return declared
}

func (c *Compiler) pushFacts(keys []string) func() {
    saved := make(map[string]struct{}, len(c.narrowed))
    for k := range c.narrowed { saved[k] = struct{}{} }
    for _, k := range keys { c.narrowed[k] = struct{}{} }
    return func() { c.narrowed = saved }
}

func (c *Compiler) dropKey(key string) {
    for k := range c.narrowed {
        if k == key || strings.HasPrefix(k, key+".") || strings.HasPrefix(k, "*"+key) || strings.Contains(k, "."+key+".") {
            delete(c.narrowed, k)
        }
    }
}

func (c *Compiler) dropCompound() {
    for k := range c.narrowed {
        if strings.ContainsAny(k, ".*") { delete(c.narrowed, k) }
    }
}

func rootOf(key string) string {
    key = strings.TrimLeft(key, "*")
    if i := strings.IndexByte(key, '.'); i >= 0 { return key[:i] }
    return key
}

// rootIsShared: a raiz pode mudar por fora deste frame (ref, global, upvalue,
// capturada, ou local cujo endereco ja foi tomado com `ref`).
func (c *Compiler) rootIsShared(root string) bool {
    for i := len(c.locals) - 1; i >= 0; i-- {
        if c.locals[i].Name != root { continue }
        l := c.locals[i]
        if _, isRef := asRefType(l.Type); isRef { return true }
        return l.IsCaptured || l.RefTaken
    }
    return true // upvalue ou global
}

func (c *Compiler) dropAfterCall() {
    for k := range c.narrowed {
        compound := strings.ContainsAny(k, ".*")
        if c.rootIsShared(rootOf(k)) || (compound && strings.HasPrefix(k, "*")) {
            delete(c.narrowed, k)
        }
    }
}

func (c *Compiler) dropForLoop(body *ast.BlockStatement) {
    roots, hasCall := loopEffects(body)
    for _, r := range roots { c.dropKey(r) }
    if hasCall { c.dropAfterCall() }
}

func (c *Compiler) mayBeNullError(expr ast.Expression, t ast.NoxyType) error {
    if key, ok := stableKey(expr); ok {
        return fmt.Errorf("[line %d] '%s' may be null; test it first\n  hint: use 'if %s != null then ... end'", c.currentLine, key, key)
    }
    return fmt.Errorf("[line %d] value of type %s may be null; test it first\n  hint: bind it with 'let' and test for null", c.currentLine, t.String())
}

func (c *Compiler) mayBeNullHint(expr ast.Expression) string {
    if key, ok := stableKey(expr); ok {
        return fmt.Sprintf("\n  hint: '%s' may be null; test it first", key)
    }
    return "\n  hint: the value may be null; bind it with 'let' and test it"
}
```

`loopEffects(body)` e `blockTerminates(block)`: walkers sobre o AST (mesmo estilo de `blockGuaranteesReturn`, `function_types.go:375`), o primeiro colhendo raízes de `AssignStatement` (via `stableKey` do alvo → `rootOf`), `for` var, `ref e` em argumentos (raiz de `e`) e se há `CallExpression`.

Ganchos no compilador:
- `Local`: campo `RefTaken bool`; em `compileReferenceArgument` (`explicit_ref.go`) marcar o local raiz do operando.
- Identifier (`compiler.go:1154`): `t = c.narrowType(n.Value, t)` para local, upvalue e global.
- MemberAccess (`:982`): antes do auto-deref, `if isNullable(leftType) { return nil, nil, c.mayBeNullError(n.Left, leftType) }`; o auto-deref usa `asRefType` só depois desse teste; ao fim, `t := c.memberType(leftType, n.Member); if key, ok := stableKey(n); ok { t = c.narrowType(key, t) }`.
- `*` (`:1483`): `if isNullable(rightType) { return ..., c.mayBeNullError(n.Right, rightType) }` antes do `asRefType`; o tipo resultante passa por `narrowType("*"+key, …)`.
- `&&` (`:1202`): compilar `Left`; `restore := c.pushFacts(thenFacts(Left))`; compilar `Right`; `restore()`. `||` (`:1234`) com `elseFacts(Left)`.
- `if` (`:1547`): `then, els := c.conditionFacts(n.Condition)`; `restore := c.pushFacts(then)` em volta de `c.Compile(n.Consequence)`; `restore()`; `restoreElse := c.pushFacts(els)` em volta de `Alternative`; `restoreElse()`; depois do `patchJump(jumpToEnd)`: `if blockTerminates(n.Consequence) { c.pushFacts(els) /* sem restore */ }`, `if n.Alternative != nil && blockTerminates(n.Alternative) { c.pushFacts(then) }`.
- `while`: `c.dropForLoop(n.Body)` antes de compilar a condição; `then, _ := conditionFacts(cond)`; `pushFacts(then)` em volta do corpo. `for … in`: `dropForLoop(body)` antes.
- Atribuições (todas as formas em `:560-700` e `:740-880`): após compilar, `if key, ok := stableKey(target); ok { c.dropKey(key) }`; se a raiz é `ref`/compartilhada (`rootIsShared`) ou o alvo é `*r`/`r.f`/`r[i]` com `r` ref, `c.dropCompound()`. Índice `x[i] = v`: `dropKey(stableKey(x))` + `dropCompound()` se raiz compartilhada.
- Fim de `compileCallExpression` (`:2288`, em todos os retornos de sucesso, inclusive builtins): `c.dropAfterCall()`.
- Índice `x[i]` (leitura), `for v in x`, chamada de valor, condição de `if`/`while`: `if isNullable(t) → mayBeNullError`.
- Início de `compileFunction`: `fnCompiler.narrowed = map[string]struct{}{}`; ao fechar cada `beginScope/endScope` **não** é preciso restaurar (facts são por chave e a chave sai de escopo com o local — mas um local interno com o mesmo nome de um externo estreitado herdaria o fact: em `addLocal`/`addOwnedLocal`, `c.dropKey(name)`).

- [ ] **Step 4: Rodar** — `go test ./internal/compiler -count=1` → PASS; `go test ./... -count=1`; corpus 100%.

- [ ] **Step 5: Commit** — `feat(compiler): narrowing de nulidade por expressoes estaveis — if/early return/&&/while, invalidacao por atribuicao, chamada e laco (issue #105 item 1)`.

### Task 7: Migração do corpus e da stdlib para `?` (fase 1, sem quebrar nada)

**Files:**
- Modify: os ~60 `.nx` em `noxy_examples/`, `tests/test_features/` que usam `null` (lista: `grep -rl "\bnull\b" --include=*.nx noxy_examples tests internal/stdlib`), `internal/stdlib/errors.nx`
- Modify: `noxy_examples/run_all_tests_concurrent.nx` (nenhuma exclusão nova)

**Interfaces:** nenhuma nova. Regra de migração: campo/variável/retorno que **de fato** recebe `null` ganha `?`; o resto fica nu. Nesta task o default ainda aceita null, então o corpus continua rodando em qualquer estado intermediário.

- [ ] **Step 1: Gerar a lista e migrar por arquivo**

Para cada arquivo: campos auto-referentes (`prox: Node`, `esquerda: TreeNode`, `topo: ref Cell`, `next: ref Node`) → `Node?`, `ref Cell?`; retornos de busca que devolvem `null` → `T?`; `let x: T = null` / `let x: T` sem inicializador (só 2 no corpus) → `T?`; parâmetros que recebem `null` em algum call site → `T?`; comparações `x == null` sobre `x: T` nu → o `x` vira `T?` na origem. Priorizar os mais densos: `language_semantics_test2.nx`, `bst_owned.nx`, `bst.nx`, `stack3.nx`, `stack.nx`, `mergesortll.nx`, `linked_list.nx`, `binary_tree.nx`, `KandR_in_noxy/ch06_hashtab.nx`, `ch06_tree.nx`, `test_merge_*.nx`, `test_ref_*.nx`, `ref_pointer_semantics.nx`, `test_explicit_deref.nx`.

Onde um campo `T?` é lido depois de testado por um caminho que o narrowing não cobre (índice `xs[i].campo`), introduzir `let n = xs[i]` + `if n != null then`.

- [ ] **Step 2: Rodar o corpus** — `go build -o noxy_dev.exe ./cmd/noxy && go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx` → 100%.

- [ ] **Step 3: Diff de saída** — para os arquivos migrados, comparar stdout do binário base (`git stash`-free: build de `develop` em `noxy.exe`) × `noxy_dev.exe`; script `benchmarks/interleaved_compare.ps1` não serve — usar um laço PowerShell simples salvando `out/<f>.base.txt` e `out/<f>.head.txt` no scratchpad e `Compare-Object`. Esperado: 0 divergências.

- [ ] **Step 4: Commit** — `refactor(examples): corpus e stdlib migrados para T?/ref T? onde null e usado de fato (issue #105 item 1, fase 1)`.

### Task 8: Flip do default — struct e `ref` não-nulos (fase 2)

**Files:**
- Modify: `internal/compiler/function_types.go:41` (`acceptsNull`), `internal/compiler/default_init.go:18-43`, `internal/compiler/compiler.go:2494-2496` (short-circuit de `null` em arg `ref`), `:3045` (`emitDefaultInit`), `internal/vm/runtime_type_validation.go:32, 112`
- Modify: mensagens com hint em `compiler.go:420, 620, 689, 875, 763, 2164, 2544` e `explicit_ref.go`
- Test: `internal/compiler/nullable_default_test.go`, `internal/vm/nullable_runtime_test.go`; fixtures `noxy_examples/type_errors/null_into_struct.nx`, `null_into_ref_param.nx`, `let_struct_without_initializer.nx`
- Modify: testes existentes que fixavam null implícito (`grep -rn "= null" internal --include=*_test.go` — reescrever com `?` mantendo a asserção; os que fixam `let p: Point` valendo `null` viram `let p: Point?`)

**Interfaces:**
- `acceptsNull(t)` = `isAny(t) || isNullType(t) || isNullable(t)`.
- `typeWithoutDefault`: struct (`structDeclaration != nil`), `RefType`, `chan`, `func` → sem default; `NullableType` → tem (null).
- Mensagens: `variable 'p' needs an initializer: Point has no default value; hint: write 'let p: Point = ...' or declare it as 'Point?'`; mismatch com `null`: `<mensagem existente>\n  hint: declare it as '<expected>?' to allow null`; argumento: `argument N to 'f': expected ref Node, got null\n  hint: declare the parameter as 'ref Node?' to accept null`; campo em construtor: `argument N to 'Point': expected int, got null` + mesmo hint com o tipo do campo.
- Runtime: `VAL_NULL` só casa com `Nullable`, `TYPE_NULL`, `TYPE_ANY`.

- [ ] **Step 1: Testes RED** (`nullable_default_test.go`):

```go
func TestStructWithoutInitializerNeedsNullable(t *testing.T) {
    src := "struct P\n    x: int\nend\nlet p: P\n"
    _, err := compileFunctionSource(t, src)
    want := "variable 'p' needs an initializer: P has no default value; hint: write 'let p: P = ...' or declare it as 'P?'"
    if err == nil || !strings.Contains(err.Error(), want) { t.Fatalf("want %q, got %v", want, err) }
}

func TestNullIntoStructIsRejected(t *testing.T) {
    src := "struct P\n    x: int\nend\nlet p: P = null\n"
    _, err := compileFunctionSource(t, src)
    if err == nil || !strings.Contains(err.Error(), "expected P, got null\n  hint: declare it as 'P?' to allow null") { t.Fatalf("got %v", err) }
}

func TestNullIntoRefParamIsRejected(t *testing.T) {
    src := "struct Node\n    v: int\nend\nfunc soma(n: ref Node) -> int\n    return n.v\nend\nlet x: int = soma(null)\n"
    _, err := compileFunctionSource(t, src)
    want := "argument 1 to 'soma': expected ref Node, got null\n  hint: declare the parameter as 'ref Node?' to accept null"
    if err == nil || !strings.Contains(err.Error(), want) { t.Fatalf("want %q, got %v", want, err) }
}

func TestNullIntoConstructorFieldIsRejected(t *testing.T) {
    src := "struct Node\n    v: int\n    prox: Node\nend\nlet n: Node = Node(1, null)\n"
    _, err := compileFunctionSource(t, src)
    if err == nil || !strings.Contains(err.Error(), "expected Node, got null\n  hint: declare it as 'Node?' to allow null") { t.Fatalf("got %v", err) }
}

func TestNullableFieldStillAcceptsNull(t *testing.T) {
    src := "struct Node\n    v: int\n    prox: Node?\nend\nlet n: Node = Node(1, null)\nlet r: ref Node? = null\n"
    if _, err := compileFunctionSource(t, src); err != nil { t.Fatal(err) }
}
```

VM (`nullable_runtime_test.go`): `let a: any = null; let p: P = a` → `interpretOrCompileErr` contém `expected P, got null`; `let q: P? = a` → ok.

- [ ] **Step 2: Rodar e ver falhar.**

- [ ] **Step 3: Implementar** — `acceptsNull` reduzido; `typeWithoutDefault` + `defaultInitError` com o hint novo; remover o short-circuit `compiler.go:2494-2496` (um `null` para `ref T` nu passa a cair no mismatch; para `ref T?` continua aceito via `acceptsNull`); `emitDefaultInit` só emite `OP_NULL` para `NullableType`/`any` (struct/ref nunca chegam aqui — `defaultInitError` barra antes); helper `nullIntoNonNullHint(expected ast.NoxyType) string` → `"\n  hint: declare it as '%s?' to allow null"` (parâmetro: `"\n  hint: declare the parameter as '%s?' to accept null"`) anexado nos sites listados quando `isNullType(actual)`. Runtime: `runtime_type_validation.go:112` → `expected.Nullable || Kind == TYPE_NULL || Kind == TYPE_ANY`; `:32` idem.

- [ ] **Step 4: Rodar tudo** — `go test ./... -count=1`; corrigir testes Go que fixavam o comportamento antigo (reescrever com `?`); corpus 100% (Task 7 já migrou; o que sobrar quebrando aqui é o "que ficou sem `?`" — corrigir com o hint). `go test -race ./internal/vm -count=1`.

- [ ] **Step 5: Commit** — `feat(compiler,vm): struct e ref nao-nulos por padrao; null so entra em T?/ref T?/any (issue #105 item 1, fase 2) BREAKING`.

### Task 9: `ref` sobre slot anulável e modo de referência com `T?`

**Files:**
- Modify: `internal/compiler/explicit_ref.go`, `compiler.go:949-954` (`RefFields`), `:3517-3536` (`isRef` em parâmetros), `:2478-2508` (argumento `ref`), `:587/613/664` (rebind de `ref`), `:759-776/864` (slots ref em índice/campo), `internal/compiler/typed_index.go`
- Test: `internal/compiler/nullable_ref_test.go`; `internal/vm/nullable_runtime_test.go`

**Interfaces:**
- Toda pergunta "este slot é de referência?" usa `asRefType` (desembrulha `?`): `prox: ref Cell?` entra em `RefFields`; parâmetro `n: ref Node?` é modo ref (`ParamInfo.IsRef = true`, `Owns = false`); `r = ref x` com `r: ref T?` é rebind.
- `ref e` onde `e: T?` **sem** narrowing tem tipo `ref (T?)`; com narrowing, `ref T`. `*r` com `r: ref (T?)` tem tipo `T?`.
- Passar `ref (T?)` onde se espera `ref T` (e vice-versa) é mismatch (invariância), mensagem existente de argumento `ref`.

- [ ] **Step 1: Testes RED**

```go
func TestRefFieldNullableIsRefSlot(t *testing.T) {
    src := "struct Cell\n    v: int\n    prox: ref Cell?\nend\nlet a: Cell = Cell(1, null)\nlet b: Cell = Cell(2, ref a)\nb.prox = null\n"
    if _, err := compileFunctionSource(t, src); err != nil { t.Fatal(err) }
}

func TestRefOfNarrowedNullableIsRefT(t *testing.T) {
    src := "struct Node\n    v: int\n    prox: Node?\nend\nfunc bump(n: ref Node) -> void\n    n.v = n.v + 1\nend\nfunc f(raiz: Node?) -> void\n    if raiz != null then\n        bump(ref raiz)\n    end\nend\n"
    if _, err := compileFunctionSource(t, src); err != nil { t.Fatal(err) }
}

func TestRefOfNullableWithoutTestIsRefNullable(t *testing.T) {
    src := "struct Node\n    v: int\nend\nfunc bump(n: ref Node) -> void\n    n.v = n.v + 1\nend\nfunc f(raiz: Node?) -> void\n    bump(ref raiz)\nend\n"
    _, err := compileFunctionSource(t, src)
    if err == nil || !strings.Contains(err.Error(), "expected ref Node, got ref (Node?)") { t.Fatalf("got %v", err) }
}
```

VM: lista compartilhada `Cell` com `prox: ref Cell?` percorrida com `while atual != null do … atual = atual.prox end` (com `atual: ref Cell?`) somando valores → `test_report` = soma.

- [ ] **Step 2: Rodar e ver falhar.**

- [ ] **Step 3: Implementar** — `grep -n "(\*ast\.RefType)" internal/compiler/*.go` (57 sites): classificar cada um como **modo** (usar `asRefType`) ou **leitura** (já protegido por `mayBeNullError` na Task 6); ajustar os de modo. `compileReferenceArgument`: tipo devolvido = tipo estreitado do operando (`narrowType`), então `ref raiz` após `if raiz != null` é `ref Node`.

- [ ] **Step 4: Rodar** — `go test ./... -count=1`; `go test -race ./internal/vm`; corpus.

- [ ] **Step 5: Commit** — `feat(compiler): ref T? e ref (T?) — modo de referencia desembrulha ?, ref de local estreitado e ref T (issue #105 item 1)`.

---

## Parte B — `Result<T>` + `try`

### Task 10: `Result<T>` em `errors.nx`; `result.nx` removido; `call_result` devolve instância real e é tipado

**Files:**
- Modify: `internal/stdlib/errors.nx` (remove `CallResult`, adiciona `Result<T>`, `ok`, `err`, `err_failure`, `empty_failure`)
- Delete: `internal/stdlib/result.nx`
- Modify: `internal/vm/builtins_call_result.go:12-60, 228-260` (assinatura oculta `(resultCtor, failureCtor, fn, ...args)`; envelopes por `NewInstanceWith`; `failureMap` → `failureInstance`)
- Modify: `internal/compiler/builtin_calls.go:39-57` (caso `call_result`), novo `internal/compiler/call_result.go`
- Modify: `internal/compiler/module_exports.go:183-198` (`buildModuleStructExports` **pula** templates — hazard 1 do mapa)
- Test: `internal/vm/builtins_call_result_test.go` (`TestCallResultEnvelopeIsMap` → `…IsResultInstance`), `internal/compiler/call_result_test.go`

**Interfaces:**
- `errors.nx`:

```noxy
struct Failure
    kind: string
    message: string
    stack: string
    causes: Failure[]
end

struct Result<T>
    ok: bool
    value: T?
    failure: Failure
end

func empty_failure() -> Failure
    return Failure("", "", "", [])
end

func ok<T>(value: T) -> Result<T>
    return Result(true, value, empty_failure())
end

func err<T>(message: string) -> Result<T>
    return Result(false, null, Failure("runtime", message, "", []))
end

func err_failure<T>(failure: Failure) -> Result<T>
    return Result(false, null, failure)
end
```

- Compilador: `call_result(fn, args...)` exige o template `Result` de `errors` registrado no programa (via `use errors select *` ou `select Result, Failure, …`); senão erro `[line N] call_result needs 'use errors select *' in scope: its result is errors.Result<T>`. Tipo de retorno `R`: `fn` identificador global com `*ast.FunctionType` (função, construtor, valor de função exato) → `Return`; `fn` em `coreBuiltinReturnTypes` → esse; `FunctionLiteral` → seu retorno; `void` ou desconhecido → `any`. O compilador chama `ensureStructInstance(tpl, [R])` e emite os dois construtores como argumentos ocultos **antes** de `fn`: `OP_GET_GLOBAL "<instanceName>"`, `OP_GET_GLOBAL "Failure"`. Tipo estático da chamada: `&ast.PrimitiveType{Name: instanceName}`.
- VM: `args[0]` = `*ObjStruct` de `Result<R>`, `args[1]` = `*ObjStruct` de `Failure`; `callResultOkEnvelope(def, failureDef, result)` → `NewInstanceWith(def, {"ok": true, "value": result, "failure": emptyFailure(failureDef)})`; falha → `{"ok": false, "value": null, "failure": failureInstance(failureDef, err)}` com `causes` como array de instâncias `Failure`. Preservar a disciplina de RC descrita em `builtins_call_result.go:222-243` (`NewInstanceWith` retém como `NewMapWithData`).

- [ ] **Step 1: Testes RED**

`internal/compiler/call_result_test.go`:

```go
func TestCallResultIsTypedByCallee(t *testing.T) {
    src := "use errors select *\nfunc dobro(x: int) -> int\n    return x * 2\nend\nlet r = call_result(dobro, 21)\nlet n: int = 0\nif r.ok then\n    n = r.value\nend\n"
    // r.value estreitado por r.ok entra na Task 12; aqui basta o tipo de r
    c, err := compileFunctionSource(t, "use errors select *\nfunc dobro(x: int) -> int\n    return x * 2\nend\nlet r = call_result(dobro, 21)\n")
    if err != nil { t.Fatal(err) }
    if got := c.GetGlobals()["r"].String(); got != "errors::Result<int>" {
        t.Fatalf("r: %s", got)
    }
    _ = src
}

func TestCallResultWithoutErrorsImportFails(t *testing.T) {
    _, err := compileFunctionSource(t, "func f() -> int\n    return 1\nend\nlet r = call_result(f)\n")
    want := "call_result needs 'use errors select *' in scope: its result is errors.Result<T>"
    if err == nil || !strings.Contains(err.Error(), want) { t.Fatalf("want %q, got %v", want, err) }
}

func TestCallResultOnNativeUsesCoreReturnType(t *testing.T) {
    c, err := compileFunctionSource(t, "use errors select *\nlet r = call_result(to_int, \"1\")\n")
    if err != nil { t.Fatal(err) }
    if got := c.GetGlobals()["r"].String(); got != "errors::Result<int>" { t.Fatalf("r: %s", got) }
}
```

`internal/vm/builtins_call_result_test.go`: renomear `TestCallResultEnvelopeIsMap` → `TestCallResultEnvelopeIsResultInstance` esperando `fmt("%T", r)` = `errors::Result<int>` (confirmar o nome exato impresso por `fmt("%T")` para instâncias genéricas — a spec nota `<main::Caixa<int> instance>` para `print`); os demais testes do arquivo trocam `let r: CallResult = …` por `let r = …` e `r.failure.message` continua igual.

- [ ] **Step 2: Rodar e ver falhar.**

- [ ] **Step 3: Implementar** conforme as interfaces. `internal/compiler/call_result.go`:

```go
func (c *Compiler) compileCallResult(call *ast.CallExpression) (*chunk.Chunk, ast.NoxyType, error) {
    if len(call.Arguments) == 0 {
        return nil, nil, fmt.Errorf("[line %d] call_result expects a callable as its first argument", c.currentLine)
    }
    tpl, ok := c.generics.structTemplate("Result")
    if !ok || tpl.Module != "errors" {
        return nil, nil, fmt.Errorf("[line %d] call_result needs 'use errors select *' in scope: its result is errors.Result<T>", c.currentLine)
    }
    resultType := c.calleeReturnType(call.Arguments[0]) // any quando desconhecido/void
    instance, err := c.ensureStructInstance(tpl, []ast.NoxyType{resultType}, c.currentLine)
    if err != nil { return nil, nil, err }
    // hidden ctor args, then the native call as usual
    c.emitOpWithConstantIndex(chunk.OP_GET_GLOBAL, c.makeConstant(value.NewString("call_result")))
    c.emitOpWithConstantIndex(chunk.OP_GET_GLOBAL, c.makeConstant(value.NewString(instance.Name)))
    c.emitOpWithConstantIndex(chunk.OP_GET_GLOBAL, c.makeConstant(value.NewString("Failure")))
    for _, arg := range call.Arguments {
        if _, _, err := c.compileCallResultArgument(arg); err != nil { return nil, nil, err } // ref args via compileRefArgument
    }
    c.emitBytes(byte(chunk.OP_CALL), byte(len(call.Arguments)+2))
    c.dropAfterCall()
    return c.currentChunk, &ast.PrimitiveType{Name: instance.Name}, nil
}
```

Adaptar aos nomes reais (`c.generics`/registro de templates em `generics.go:27`, `ensureStructInstance` em `generics_structs.go:209`, como `compileCallExpression` emite `OP_CALL` e trata argumentos `ref` em `:2478-2508`). Registrar `call_result` no allowlist de `compileBuiltinCall` (`builtin_calls.go:46`). O pass 1 de genéricos precisa rodar quando há `call_result` mesmo sem outro genérico no programa: `hasGenerics()` (`generics.go:229`) já é verdadeiro se há template importado — confirmar que `use errors select *` registra o template antes da decisão (`predeclareImportedTemplates`, `module_exports.go:477`).

`http_parser.nx` chama `convert_to_int_result` direto (map) — não muda.

- [ ] **Step 4: Rodar** — `go test ./... -count=1`; corpus (atualizar `noxy_examples/result_pattern.nx` para a forma nova).

- [ ] **Step 5: Commit** — `feat(stdlib,vm,compiler): Result<T> unico em errors; call_result tipado pelo callee e devolvendo instancia real; result.nx removido (issue #105 item 2)`.

### Task 11: Gêmeas `_result` da stdlib sobre `Result<T>`

**Files:**
- Modify: `internal/stdlib/convert.nx` (`to_int_result` → `Result<int>`, `to_float_result` → `Result<float>`; remove `IntResult`/`FloatResult`), `internal/stdlib/io.nx` (`close_result` → `Result<bool>`, `write_result`/`write_bytes_result` → `Result<int>`; remove `IOCloseResult`/`IOWriteResult`), `internal/stdlib/json.nx` (`dumps_result` → `Result<string>`; remove `EncodeResult`)
- Delete natives só usados pelas gêmeas: `io_close_result` (`builtins_io.go:83`), `io_write_result` (`:159`), `json_dumps_result` (`builtins_json.go:26`) e seus testes; **manter** `convert_to_int_result`/`convert_to_float_result` (usados por `http_parser.nx`)
- Modify: exemplos que usam `IntResult`/`to_int_result` (`form_app.nx`, `int_to_result_noxy.nx`, `KandR_in_noxy/ch07_parse.nx`, `language_semantics_test2.nx`, `password_manager/server.nx`, `test_convert.nx`, `todo_app.nx`, `to_int_conversion_demo.nx`, `web_app.nx`), `test_generics_result.nx` (renomear o struct local para `Resultado<T>` — `Result` passa a ser nome da stdlib)
- Test: `internal/vm/stdlib_result_twins_test.go`

**Interfaces:**

```noxy
// convert.nx
use errors select *
func to_int_result(v: any) -> Result<int>
    return call_result(to_int, v)
end
// io.nx
func close_result(file: File) -> Result<bool>
    let r = call_result(close, file)
    if r.ok then
        return ok(true)
    end
    return err_failure(r.failure)
end
func write_result(file: File, content: string) -> Result<int>
    let r = call_result(write, file, content)
    if r.ok then
        return ok(strlen(content))
    end
    return err_failure(r.failure)
end
// json.nx
func dumps_result(value: any) -> Result<string>
    return call_result(json_dumps, value)
end
```

(`write_bytes_result` → `ok(length(data))`. `call_result(write, …)` sobre função `void` dá `Result<any>` — por isso o `ok(...)` explícito. Se `close`/`write` de `io.nx` referenciam `io_close`/`io_write` sem tipo de retorno, o resultado continua correto.)

- [ ] **Step 1: Teste RED** (`stdlib_result_twins_test.go`): `use convert select *` + `to_int_result("12")` → `ok|12`; `to_int_result("x")` → `false|` + `failure.message` não vazio; `use json select *` + `dumps_result({"a": 1})` → `true|{"a":1}`; `use io select *` + `close_result` sobre arquivo aberto → `true`.

- [ ] **Step 2: Rodar e ver falhar** (structs antigos ainda existem).

- [ ] **Step 3: Implementar** e migrar os exemplos (`r.value` já é `int?` — com `if r.ok then` a Task 12 estreita; nesta task, escrever `if r.ok then let v = r.value; if v != null then … end end` **ou** adiar a migração dos exemplos para depois da Task 12 — preferir a segunda: marcar os exemplos migrados só na Task 12, e nesta task deixar o corpus temporariamente excluído? **Não**: fazer a Task 12 antes de migrar exemplos — executar Task 12 imediatamente após o Step 3 desta task, e voltar para o Step 4).

- [ ] **Step 4: Rodar** — `go test ./... -count=1`; corpus 100%; `TestStdlibWrappersCallOnlyRegisteredNatives` e `TestEveryNativeIsRegisteredExactlyOnce` verdes.

- [ ] **Step 5: Commit** — `refactor(stdlib): gemeas _result devolvem Result<T>; IntResult/FloatResult/IOCloseResult/IOWriteResult/EncodeResult removidos (issue #105 item 2)`.

### Task 12: `if r.ok then` estreita `r.value`

**Files:**
- Modify: `internal/compiler/narrowing.go` (`resultValueKey`)
- Test: `internal/compiler/narrowing_test.go`

**Interfaces:**
- `func (c *Compiler) resultValueKey(e *ast.MemberAccessExpression) string`: se `e.Member == "ok"`, base estável `k`, e o tipo estático da base é instância de `errors::Result<…>` (`structInstanceKey`, `generics_structs.go:23`, base `Result` + módulo `errors`), devolve `k + ".value"`; senão `""`. `conditionFacts` já consulta o gancho (Task 6).

- [ ] **Step 1: Teste RED**

```go
func TestResultOkNarrowsValue(t *testing.T) {
    src := "use errors select *\nfunc dobro(x: int) -> int\n    return x * 2\nend\nlet r = call_result(dobro, 21)\nlet n: int = 0\nif r.ok then\n    n = r.value\nend\n"
    if _, err := compileFunctionSource(t, src); err != nil { t.Fatal(err) }
}

func TestResultValueWithoutOkTestNeedsNullCheck(t *testing.T) {
    src := "use errors select *\nfunc dobro(x: int) -> int\n    return x * 2\nend\nlet r = call_result(dobro, 21)\nlet n: int = r.value\n"
    _, err := compileFunctionSource(t, src)
    if err == nil || !strings.Contains(err.Error(), "expected int, got int?\n  hint: 'r.value' may be null; test it first") { t.Fatalf("got %v", err) }
}
```

- [ ] **Step 2–4:** RED → implementar (`resultValueKey` precisa do tipo estático da base: compilar `n.Left` num compilador descartável não serve — usar `c.staticTypeOf(expr)` se existir; senão, resolver só bases que são identificadores (`resolveLocal`/`resolveUpvalue`/`resolveGlobalType`) e caminhos de membro via `memberType` recursivo, sem emitir código) → PASS → migrar os exemplos listados na Task 11 → corpus 100%.

- [ ] **Step 5: Commit** — `feat(compiler): if r.ok then estreita r.value para T em errors.Result<T> (issue #105 item 2)`.

### Task 13: `OP_SWAP` na VM

**Files:**
- Modify: `internal/chunk/chunk.go` (opcode + nome em `String`), `internal/vm/executor.go:1407` (ao lado de `OP_DUP`)
- Test: `internal/vm/opcode_swap_test.go`

**Interfaces:** `OP_SWAP` troca os dois valores do topo; sem efeito em RC.

- [ ] **Step 1: Teste RED** — montar um chunk à mão (`chunk.New()`, `OP_CONSTANT 1`, `OP_CONSTANT 2`, `OP_SWAP`, `OP_SUBTRACT`, `OP_RETURN`) e interpretar: resultado `1` (2−1 vira 1−2 = −1 sem swap). Seguir o padrão de teste de chunk manual existente (`grep -l "OP_DUP" internal/vm/*_test.go`).
- [ ] **Step 2–4:** RED → `case chunk.OP_SWAP: top := vm.stackTop; vm.stack[top-1], vm.stack[top-2] = vm.stack[top-2], vm.stack[top-1]` (usar os acessores reais de `stack.go`) → PASS.
- [ ] **Step 5: Commit** — `feat(vm): OP_SWAP (suporte ao lowering de try)`.

### Task 14: `try expr`

**Files:**
- Modify: `internal/token/token.go` (`TRY` + keyword `"try"`), `internal/ast/ast.go` (`TryExpression{Token, Value}`), `internal/ast/clone.go`, `internal/parser/parser.go:44-65` (prefixo `TRY` → `parseTryExpression`), `internal/compiler/compiler.go:1472` (caso `*ast.TryExpression`), novo `internal/compiler/try.go`
- Test: `internal/compiler/try_test.go`, `internal/vm/try_test.go`; fixtures `noxy_examples/type_errors/try_outside_result_function.nx`, `try_on_non_result.nx`

**Interfaces:**
- Gramática: `try` é palavra reservada; `try <expr>` é prefixo com a precedência de `PREFIX` (`try f(x).ok` = `try (f(x).ok)`).
- Tipos: `Value` deve ser `errors::Result<U>` (instância) → `try` tem tipo `U`; a função corrente deve devolver `errors::Result<V>`. Erros: `[line N] 'try' expects a Result<T>, got <tipo>`; `[line N] 'try' requires the enclosing function to return Result<T> (found <tipo>)`; `[line N] 'try' outside a function`.
- Lowering (stack à direita):

```
<Value>                         ; r
OP_DUP                          ; r r
OP_GET_PROPERTY "ok"            ; r ok
OP_JUMP_IF_FALSE Lfail          ; r ok
OP_POP                          ; r
OP_GET_PROPERTY "value"         ; u
OP_JUMP Lend
Lfail:
OP_POP                          ; r
OP_GET_PROPERTY "failure"       ; f
OP_GET_GLOBAL "<errors::Result<V>>" ; f ctor
OP_SWAP                         ; ctor f
OP_FALSE                        ; ctor f false
OP_SWAP                         ; ctor false f
OP_NULL                         ; ctor false f null
OP_SWAP                         ; ctor false null f
OP_CALL 3                       ; Result<V>(false, null, f)
<emitRuntimeValueType(Result<V>)>
OP_RETURN
Lend:                           ; u
```

`OP_GET_PROPERTY` por nome (não `OP_GET_FIELD` por índice) porque `Result<V>` pode ter vindo de `json_loads`… não: instâncias de `Result` nunca vêm de `json_loads`; ainda assim, por nome é o caminho já validado pelo funil genérico e o custo é irrelevante no `try`. `ensureStructInstance(tpl, [V])` garante que o construtor de `Result<V>` existe como global.

- [ ] **Step 1: Testes RED**

`internal/compiler/try_test.go`:

```go
func TestTryPropagatesInResultFunction(t *testing.T) {
    src := "use errors select *\nfunc parse(s: string) -> Result<int>\n    let n: int = try to_int_result(s)\n    return ok(n * 2)\nend\n"
    if _, err := compileFunctionSource(t, "use convert select *\n"+src); err != nil { t.Fatal(err) }
}

func TestTryOutsideResultFunctionFails(t *testing.T) {
    src := "use errors select *\nuse convert select *\nfunc main() -> void\n    let n: int = try to_int_result(\"1\")\nend\n"
    _, err := compileFunctionSource(t, src)
    want := "'try' requires the enclosing function to return Result<T> (found void)"
    if err == nil || !strings.Contains(err.Error(), want) { t.Fatalf("want %q, got %v", want, err) }
}

func TestTryOnNonResultFails(t *testing.T) {
    src := "use errors select *\nfunc f() -> Result<int>\n    let n: int = try 42\n    return ok(n)\nend\n"
    _, err := compileFunctionSource(t, src)
    if err == nil || !strings.Contains(err.Error(), "'try' expects a Result<T>, got int") { t.Fatalf("got %v", err) }
}

func TestTryAtTopLevelFails(t *testing.T) {
    _, err := compileFunctionSource(t, "use errors select *\nuse convert select *\nlet n: int = try to_int_result(\"1\")\n")
    if err == nil || !strings.Contains(err.Error(), "'try' outside a function") { t.Fatalf("got %v", err) }
}
```

`internal/vm/try_test.go`:

```go
func TestTryReturnsFailureAndRunsDefer(t *testing.T) {
    src := `use errors select *
use convert select *
let log: string[] = []
func parse(s: string) -> Result<int>
    defer append(ref log, "defer " + s)
    let n: int = try to_int_result(s)
    return ok(n * 2)
end
let a = parse("21")
let b = parse("x")
let av: int = 0
if a.ok then
    av = a.value
end
test_report(to_str(a.ok) + "|" + to_str(av) + "|" + to_str(b.ok) + "|" + to_str(b.failure.message != "") + "|" + to_str(length(log)))
`
    got := captureVMSource(t, src).String()
    if got != "true|42|false|true|2" { t.Fatalf("got %s", got) }
}

func TestTryAsStatementPropagatesVoidResult(t *testing.T) {
    src := `use errors select *
func falha() -> Result<bool>
    return err("nope")
end
func usa() -> Result<int>
    try falha()
    return ok(1)
end
let r = usa()
test_report(to_str(r.ok) + "|" + r.failure.message)
`
    if got := captureVMSource(t, src).String(); got != "false|nope" { t.Fatalf("got %s", got) }
}
```

Fixtures: `try_outside_result_function.nx` (sufixo `'try' requires the enclosing function to return Result<T> (found void)`), `try_on_non_result.nx` (`'try' expects a Result<T>, got int`).

- [ ] **Step 2: Rodar e ver falhar.**

- [ ] **Step 3: Implementar** — token/keyword; `ast.TryExpression` (+ clone + golden); parser: `p.registerPrefix(token.TRY, p.parseTryExpression)` construindo `&ast.TryExpression{Token: tok, Value: p.parseExpression(PREFIX)}`; `internal/compiler/try.go`:

```go
func (c *Compiler) compileTry(n *ast.TryExpression) (*chunk.Chunk, ast.NoxyType, error) {
    if c.funcReturnType == nil && c.scopeDepth == 0 && c.currentFunctionName == "" { // top level
        return nil, nil, fmt.Errorf("[line %d] 'try' outside a function", c.currentLine)
    }
    expected := c.funcReturnType
    outerArgs, isResult := c.resultTypeArgs(expected)
    if !isResult {
        return nil, nil, fmt.Errorf("[line %d] 'try' requires the enclosing function to return Result<T> (found %s)", c.currentLine, typeNameOrVoid(expected))
    }
    _, valueType, err := c.Compile(n.Value)
    if err != nil { return nil, nil, err }
    innerArgs, ok := c.resultTypeArgs(valueType)
    if !ok {
        return nil, nil, fmt.Errorf("[line %d] 'try' expects a Result<T>, got %s", c.currentLine, typeNameOrVoid(valueType))
    }
    c.emitByte(byte(chunk.OP_DUP))
    c.emitOpWithConstantIndex(chunk.OP_GET_PROPERTY, c.makeConstant(value.NewString("ok")))
    jumpFail := c.emitJump(chunk.OP_JUMP_IF_FALSE)
    c.emitByte(byte(chunk.OP_POP))
    c.emitOpWithConstantIndex(chunk.OP_GET_PROPERTY, c.makeConstant(value.NewString("value")))
    jumpEnd := c.emitJump(chunk.OP_JUMP)
    c.patchJump(jumpFail)
    c.emitByte(byte(chunk.OP_POP))
    c.emitOpWithConstantIndex(chunk.OP_GET_PROPERTY, c.makeConstant(value.NewString("failure")))
    c.emitOpWithConstantIndex(chunk.OP_GET_GLOBAL, c.makeConstant(value.NewString(expected.String())))
    c.emitByte(byte(chunk.OP_SWAP))
    c.emitByte(byte(chunk.OP_FALSE))
    c.emitByte(byte(chunk.OP_SWAP))
    c.emitByte(byte(chunk.OP_NULL))
    c.emitByte(byte(chunk.OP_SWAP))
    c.emitBytes(byte(chunk.OP_CALL), 3)
    c.emitRuntimeValueType(expected)
    c.emitByte(byte(chunk.OP_RETURN))
    c.patchJump(jumpEnd)
    _ = outerArgs
    return c.currentChunk, innerArgs[0], nil
}
```

`resultTypeArgs(t)`: via `structInstanceKey` — instância cujo template é `Result` do módulo `errors` → `[]ast.NoxyType{U}`. Verificar que `expected.String()` é exatamente o nome global da instância (`errors::Result<V>`); se a instância for referida por outro nome no programa (`expandInstanceNames`), usar `instanceName(tpl.Module, "Result", args)`. Se `OP_RETURN` do executor exigir stack limpo (verificar `finalizeCurrentFrame`, `unwind.go:42`), confirmar com o teste de `defer` que temporários acima do frame não vazam RC (`rc_uniqueness_test.go` roda no `go test ./...`).

- [ ] **Step 4: Rodar** — `go test ./... -count=1`; `go test -race ./internal/vm`; corpus.

- [ ] **Step 5: Commit** — `feat(lang): try expr propaga a falha de Result<T> com early return (issue #105 item 2)`.

### Task 15: Exemplos e showcase da forma nova

**Files:**
- Modify: `noxy_examples/result_pattern.nx`, `noxy_examples/int_to_result_noxy.nx`, `noxy_examples/test_generics_result.nx` (já com `Resultado<T>`), novo `noxy_examples/try_config.nx` (o `le_config` da issue)
- Test: corpus

- [ ] **Step 1:** Escrever `try_config.nx` exatamente como a seção "Erros — propagação com `try`" da issue (com `io.read_lines`/`to_int_result`), imprimindo os dois caminhos.
- [ ] **Step 2:** `go run ./cmd/noxy noxy_examples/try_config.nx` → saída esperada anotada em comentário no topo do arquivo; corpus 100%.
- [ ] **Step 3: Commit** — `docs(examples): result_pattern, int_to_result e try_config na forma Result<T> + try`.

---

## Parte D — Docs, versão, PR

### Task 16: Spec, REF_SEMANTICS, README, AGENTS

**Files:**
- Modify: `docs/NOXY_LANGUAGE_SPEC.md` — §1.2 keywords (`try`), §1.4 delimitadores (`?`), §2.0 regras (nulidade), nova §2.4 "Nullable types `T?`" (regras 1–5 da tabela Kotlin, gramática `ref T?`/`ref (T?)`, narrowing: chaves estáveis, invalidação, `&&`, `while`, early return), §2.2 regra 6 e §5 self-reference (`next: Node?`, `next: ref Node?`), §2.3 R8 (`null` só para `ref T?`), §3 tabela de defaults (struct/`ref` sem default; `T?` → null) e "Redeclaration" (namespace único; REPL redefine função), §4.2/§4.3 (`ref (T?)` invariante), §6.5 (`T` binda `X?`), §7 "Errors"/"The error boundary" (`Result<T>` tipado, envelope é instância, `try`, `if r.ok` estreita `value`), §10 (`call_result` requer `use errors`), §11 (global inexistente é erro de compilação; embutidores semeiam nativos), §12 (gêmeas `_result` → `Result<T>`; `result` removido)
- Modify: `docs/REF_SEMANTICS.md` (nulidade de `ref`), `README.md` (badge 0.22.0; "Three rules" ganha a linha sobre `?`; exemplo de `try`; Features: `T?`), `AGENTS.md` (regra 1: `let p: Point` é erro; `try`)

- [ ] **Step 1:** Escrever as seções (EN na spec/README, PT-BR no resto), copiando os exemplos da issue já validados pelos testes.
- [ ] **Step 2:** `go test ./internal/... -run 'Golden|Spec' -count=1` (se houver testes que leem a spec) e revisão de links.
- [ ] **Step 3: Commit** — `docs(spec): tipos anulaveis T?, Result<T> + try, namespace global unico (issue #105)`.

### Task 17: CHANGELOG + versão v0.22.0

**Files:**
- Modify: `CHANGELOG.md` (entrada `## [0.22.0] - <data>` com seções Changed (BREAKING), Added, Removed, Fixed — modelo da 0.21.0), `internal/version/version.go` (`v0.22.0`), `README.md:1,208`, `docs/index.html`/site se citarem a versão (`grep -rn "0\.21\.0" docs README.md`)

- [ ] **Step 1:** Escrever a entrada: BREAKING — struct e `ref` não-nulos, `CallResult`/`IntResult`/… removidos, `result` removido, `try` reservada, global inexistente/colisão/parâmetro duplicado erram; Added — `T?`, narrowing, `Result<T>`, `ok/err/err_failure`, `try`, `OP_SWAP`, `SetKnownGlobals`/`GlobalNames`; migração em 6 linhas (o que fazer com cada erro novo, apontando os hints).
- [ ] **Step 2:** `go build -o noxy.exe ./cmd/noxy && ./noxy.exe --version` (ou o REPL banner) mostra `v0.22.0`.
- [ ] **Step 3: Commit** — `chore(version): noxy v0.22.0 — T?/ref T? nao-nulos por padrao, Result<T> + try, solidez de nomes (#47); CHANGELOG, README, spec, version.go`.

### Task 18: Verificação final, PR e comentário na issue

- [ ] **Step 1:** `go test ./... -count=1`; `go test -race ./internal/vm -count=1 -timeout=600s`; `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx`; `go build ./...`; `gofmt -l internal cmd` vazio.
- [ ] **Step 2:** Bench de sanidade (não muda semântica de caminho quente, mas `dropAfterCall`/`narrowType` rodam em compilação — medir só o tempo de compilação do corpus antes × depois; e `bench_call_readonly`/`bench_path_update` via `benchmarks/interleaved_compare.ps1 -Runs 5` para confirmar 0% em runtime).
- [ ] **Step 3:** `git push -u origin feature/issue-105-null-safety-result`; `gh pr create --base develop --title "feat: T?/ref T? nao-nulos por padrao, Result<T> + try, solidez de nomes — v0.22.0 (issue #105)" --body-file <scratchpad>/pr_105.md` com: resumo por item, tabela antes×depois, desvios da issue (`value: T?`, `Result<bool>` para close, `T` binda `X?`, narrowing de caminhos), migração, verificação, e o rodapé `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.
- [ ] **Step 4:** Comentar na #105 com o link do PR e os desvios. **Não** fechar a issue, **não** mergear.

---

## Self-review (feito ao escrever)

- Cobertura da issue: item 1 (Tasks 4–9), item 2 (10–15), item 3 (1–3), "Como fica o código" (todos os exemplos viraram testes ou fixtures), Kotlin/Dart 5 pontos (Task 6/8), moratória de módulos (nenhum módulo novo aqui), versão v0.22.0 (Task 17).
- Nomes cruzados: `nullable`/`nonNull`/`isNullable`/`asRefType` (Task 5) usados nas 6, 8, 9; `stableKey`/`conditionFacts`/`narrowType`/`pushFacts`/`dropKey`/`dropCompound`/`dropAfterCall`/`dropForLoop`/`mayBeNullError`/`mayBeNullHint`/`resultValueKey` (Task 6) usados nas 10, 12, 14; `Result<T>{ok, value: T?, failure}`/`ok`/`err`/`err_failure`/`empty_failure` (Task 10) usados nas 11, 12, 14, 15; `OP_SWAP` (13) usado na 14; `SetKnownGlobals`/`GlobalNames`/`PluginNativeNames`/`GlobalDecl`/`SetSessionBindings`/`ProgramBindings` (Tasks 2–3) usados no `cmd/noxy`.
- Pontos que o executor deve confirmar no código antes de codar (marcados "verificar" nas tasks): nome do getter de structs do compilador (Task 2), formato de erro do parser (Task 4), API do registro de templates e `ensureStructInstance` (Task 10), string de `fmt("%T")` para instância genérica (Task 10), nome global da instância no `try` (Task 14), acessores de stack em `executor.go` (Task 13).
