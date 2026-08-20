# Erro de Redeclaração de `let` + Docs de Escopo — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redeclarar um nome com `let` no mesmo escopo vira erro de compilação (estilo Go, apontando a declaração anterior) — inclusive no REPL, onde a sessão se comporta como um arquivo digitado linha a linha; a spec ganha documentação breve de redeclaração×reatribuição, shadowing e escopo da variável do `for`.

**Architecture:** Dois checks no compilador: locais no case `*ast.LetStmt` de `internal/compiler/compiler.go` (scan da pilha `c.locals` na mesma `scopeDepth`), globais em `predeclareGlobalBindings` de `internal/compiler/function_types.go` (mapa `letSeen` por Program, ao lado do check existente de `duplicate function`). Para o REPL — que compila cada linha como um `Program` novo num `Compiler` novo (`cmd/noxy/main.go:257`) — a memória entre linhas vem de um mapa de sessão `sessionLets` injetado via setter (mesmo padrão de `replStructs`/`replGenerics`): o compilador **checa** contra ele no predeclare, mas quem **registra** é o loop do REPL, somente após a linha compilar com sucesso (uma linha rejeitada não queima o nome). O scratch do pass 1 dos genéricos (`newPass1Compiler`) não recebe o campo → fica `nil` → sem checagem nem registro duplicado no two-pass.

**Tech Stack:** Go (compilador noxy), testes `go test` no padrão de `internal/compiler/assign_deref_hint_test.go` (helper `parse()` de `compiler_test.go:28`).

**Spec:** Inline — seção "Resoluções aprovadas" abaixo (design fechado em conversa, 2026-08-20).

## Resoluções aprovadas

1. **Única mudança de comportamento:** `let` de nome já declarado no mesmo escopo → erro de compilação, mensagem apontando a declaração anterior + hint sugerindo atribuição. Vale para global, local **e para o REPL** (decisão do usuário em 2026-08-20: sem exceção — a sessão simula a realidade, linha a linha, de forma interativa; re-`let` entre linhas é rejeitado e o valor se atualiza com `x = ...`).
2. **Documentação breve (sem mudança de comportamento):** spec §3 ganha redeclaração×reatribuição e shadowing em escopo interno; seção do `for ... in` ganha escopo da variável de loop + rebinding por iteração (atribuir à variável no corpo não afeta a sequência).
3. Shadowing entre escopos distintos permanece legal (for-var sobre `let` externo, `let` em bloco interno, corpo sombreando parâmetro — no noxy parâmetros ficam em depth 1 e o corpo, sendo `BlockStatement`, em depth 2).

## Global Constraints

- Verificação obrigatória (AGENTS.md): `go test ./internal/...` e `go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx` passando.
- Mensagens de erro do compilador em inglês, formato `[line %d] ...`; hints no padrão `; hint: ...` (ver `assign_deref_hint_test.go`).
- Comentários de código em português (padrão do repo).
- Sem dependências novas.
- Branch `feat/let-redeclaration` a partir de `develop`; PR depois via skill open-pr (base develop, título `<branch> - Descrição PT`, label "not available to review").
- Release: bump para `v0.9.0` (BREAKING → minor, padrão do projeto: v0.8.0 foi minor com breaking), CHANGELOG datado 2026-08-20 em português.
- Commits terminam com `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

**Mensagem de erro canônica (usada nos dois checks):**

```
[line 3] variable 'x' redeclared in this scope (previous declaration at line 2); hint: to update the value, use 'x = ...' without 'let'
```

---

### Task 1: Check de redeclaração LOCAL

**Files:**
- Create: `internal/compiler/let_redeclaration_test.go`
- Modify: `internal/compiler/compiler.go` (struct `Local` ~linha 12; `addLocal`/`addOwnedLocal` ~linhas 2596–2605; case `*ast.LetStmt` ~linha 240)

**Interfaces:**
- Consumes: helper `parse(input string) *ast.Program` de `compiler_test.go:28`; `New()` de `compiler.go:111`.
- Produces: campo `Local.Line int`; erro com substring `"redeclared in this scope"` (Task 2 reusa os helpers `requireRedeclarationError`/`requireCompiles` deste arquivo de teste).

- [x] **Step 1: Escrever os testes que falham**

Criar `internal/compiler/let_redeclaration_test.go`:

```go
package compiler

// Redeclaracao de `let` no mesmo escopo e erro de compilacao (spec §3): o
// segundo `let` criava um binding novo silenciosamente — inclusive trocando
// o tipo, furando a regra nº1 da §2.0 ("o tipo e definido na declaracao e
// nao pode mudar"). Reatribuicao (`x = ...`) segue sendo o caminho para
// atualizar o valor. Shadowing em escopo INTERNO continua legal: for-var
// sobre let externo, let em bloco if, corpo sombreando parametro (params em
// depth 1, corpo BlockStatement em depth 2).

import (
	"strings"
	"testing"

	"noxy-vm/internal/ast"
)

const redeclaredText = "redeclared in this scope"

func requireRedeclarationError(t *testing.T, src string, wantPrev string) {
	t.Helper()
	_, _, err := New().Compile(parse(src))
	if err == nil {
		t.Fatal("redeclaracao de let no mesmo escopo deveria falhar na compilacao")
	}
	if !strings.Contains(err.Error(), redeclaredText) {
		t.Fatalf("erro deveria citar %q: %v", redeclaredText, err)
	}
	if !strings.Contains(err.Error(), wantPrev) {
		t.Fatalf("erro deveria apontar a declaracao anterior (%q): %v", wantPrev, err)
	}
}

func requireCompiles(t *testing.T, src string) {
	t.Helper()
	if _, _, err := New().Compile(parse(src)); err != nil {
		t.Fatalf("programa valido nao deveria falhar: %v", err)
	}
}

func TestLocalLetRedeclarationSameScopeFails(t *testing.T) {
	requireRedeclarationError(t, `func f()
    let x: int = 1
    let x: string = "virei string"
end`, "previous declaration at line 2")
}

func TestLocalLetRedeclarationSameTypeFails(t *testing.T) {
	requireRedeclarationError(t, `func f()
    let x: int = 1
    let x: int = 2
end`, "previous declaration at line 2")
}

func TestLetShadowingInInnerBlockAllowed(t *testing.T) {
	requireCompiles(t, `func f()
    let x: int = 1
    if x > 0 then
        let x: string = "sombra interna"
        print(x)
    end
    print(x)
end`)
}

func TestLetAfterForLoopVarAllowed(t *testing.T) {
	requireCompiles(t, `func f()
    for i in [1, 2, 3] do
        print(i)
    end
    let i: int = 2
    print(i)
end`)
}

func TestLetShadowingParamAllowed(t *testing.T) {
	requireCompiles(t, `func f(x: int)
    let x: string = "corpo sombreia parametro"
    print(x)
end`)
}
```

Nota: o import de `ast` só é usado pelos testes da Task 2 no mesmo arquivo; se o Go reclamar de import não usado neste passo, adicione `var _ ast.NoxyType` temporário ou deixe o import para a Task 2.

- [x] **Step 2: Rodar e confirmar que falham**

Run: `go test ./internal/compiler -run 'LetRedeclaration|LetShadowing|LetAfterFor' -v`
Expected: `TestLocalLetRedeclarationSameScopeFails` e `TestLocalLetRedeclarationSameTypeFails` FALHAM ("deveria falhar na compilacao"); os três `Allowed` PASSAM (comportamento atual já permite).

- [x] **Step 3: Implementar o check local**

Em `internal/compiler/compiler.go`:

3a. Struct `Local` (linha ~12) ganha o campo `Line`:

```go
type Local struct {
	Name       string
	Depth      int
	// Line e a linha da declaracao — so e lida pelo erro de redeclaracao,
	// que aponta a primeira ocorrencia do nome no escopo.
	Line       int
	Type       ast.NoxyType
	IsCaptured bool
	IsParam    bool
	// ... (campo Owns e comentario existentes, inalterados)
}
```

3b. `addLocal` e `addOwnedLocal` (linhas ~2596–2605) preenchem `Line: c.currentLine`:

```go
func (c *Compiler) addLocal(name string, t ast.NoxyType) {
	c.locals = append(c.locals, Local{Name: name, Depth: c.scopeDepth, Line: c.currentLine, Type: t})
}

func (c *Compiler) addOwnedLocal(name string, t ast.NoxyType) {
	c.locals = append(c.locals, Local{Name: name, Depth: c.scopeDepth, Line: c.currentLine, Type: t, Owns: true})
}
```

3c. No case `*ast.LetStmt` (linha ~240), logo depois de `c.setLine(n.Token.Line)` e antes de `resolveAnnotation`:

```go
		// Redeclaracao no mesmo escopo e erro (spec §3): `let` cria vinculo
		// novo, e um segundo `let` do mesmo nome no mesmo depth poderia ate
		// trocar o tipo por baixo da §2.0. Escopos internos (depth maior)
		// continuam livres para sombrear — a pilha de locals e ordenada por
		// depth, entao a varredura para na primeira mudanca de profundidade.
		// Globais tem o proprio check em predeclareGlobalBindings.
		if c.scopeDepth > 0 {
			for i := len(c.locals) - 1; i >= 0 && c.locals[i].Depth == c.scopeDepth; i-- {
				if c.locals[i].Name == n.Name.Value {
					return nil, nil, fmt.Errorf(
						"[line %d] variable '%s' redeclared in this scope (previous declaration at line %d); hint: to update the value, use '%s = ...' without 'let'",
						n.Token.Line, n.Name.Value, c.locals[i].Line, n.Name.Value)
				}
			}
		}
```

- [x] **Step 4: Rodar e confirmar que passam**

Run: `go test ./internal/compiler -run 'LetRedeclaration|LetShadowing|LetAfterFor' -v`
Expected: os 5 testes PASSAM.

Run: `go test ./internal/...`
Expected: PASS geral — se algum teste existente quebrar com o erro novo, é um re-let genuíno em fixture: corrigir a fixture (renomear ou trocar por atribuição), nunca afrouxar o check.

- [x] **Step 5: Commit**

```bash
git add internal/compiler/let_redeclaration_test.go internal/compiler/compiler.go
git commit -m "feat(compiler): let redeclarado no mesmo escopo local é erro de compilação

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: Check de redeclaração GLOBAL + sessão do REPL

**Files:**
- Modify: `internal/compiler/function_types.go` (`predeclareGlobalBindings`, case `*ast.LetStmt` linhas ~278–284)
- Modify: `internal/compiler/compiler.go` (campo `sessionLets` + setter `SetSessionLets` + getter `ProgramLets`, junto dos demais setters de estado)
- Modify: `cmd/noxy/main.go` (`startREPL`, ~linhas 167 e 255–267)
- Modify: `internal/compiler/let_redeclaration_test.go` (novos testes)

**Interfaces:**
- Consumes: helpers `requireRedeclarationError`/`requireCompiles` da Task 1; `NewWithState(globals, structs, fileName)` de `compiler.go:115`; `GetGlobals()` (usado em `cmd/noxy/main.go:267`).
- Produces: mesmo formato de erro da Task 1 para duplicata no mesmo Program; para duplicata de sessão REPL, a variante `(previously declared in this session)`. `SetSessionLets(map[string]int)` arma a checagem de sessão; `ProgramLets() map[string]int` expõe os `let` top-level da última compilação para o REPL registrar após sucesso.

- [x] **Step 1: Escrever os testes que falham**

Acrescentar em `internal/compiler/let_redeclaration_test.go`:

```go
func TestGlobalLetRedeclarationFails(t *testing.T) {
	requireRedeclarationError(t, `let x: int = 1
let x: string = "virei string"`, "previous declaration at line 1")
}

func TestGlobalLetAfterTopLevelForAllowed(t *testing.T) {
	// O padrao do crivo.nx: a variavel do for e escopada ao loop, entao o
	// let seguinte e declaracao nova, nao redeclaracao.
	requireCompiles(t, `for i in [1, 2, 3] do
    print(i)
end
let i: int = 2
print(i)`)
}

func TestReplSessionReLetFails(t *testing.T) {
	// REPL sem excecao (decisao 2026-08-20): a sessao se comporta como um
	// arquivo digitado linha a linha. O compilador CHECA contra sessionLets;
	// quem registra e o loop do REPL, apos a linha compilar com sucesso.
	structs := make(map[string]*ast.StructStatement)
	session := make(map[string]int)

	c1 := NewWithState(make(map[string]ast.NoxyType), structs, "REPL")
	c1.SetSessionLets(session)
	if _, _, err := c1.Compile(parse(`let x: int = 1`)); err != nil {
		t.Fatalf("primeira linha do REPL falhou: %v", err)
	}
	for name, line := range c1.ProgramLets() {
		session[name] = line
	}

	c2 := NewWithState(c1.GetGlobals(), structs, "REPL")
	c2.SetSessionLets(session)
	_, _, err := c2.Compile(parse(`let x: string = "re-let de sessao"`))
	if err == nil {
		t.Fatal("re-let entre linhas da sessao deveria falhar")
	}
	if !strings.Contains(err.Error(), "previously declared in this session") {
		t.Fatalf("erro deveria citar a sessao: %v", err)
	}
}

func TestReplFailedLineDoesNotBurnTheName(t *testing.T) {
	// Uma linha rejeitada nao registra o nome: ProgramLets so e lido pelo
	// REPL quando a compilacao inteira da linha teve sucesso, entao aqui o
	// contrato e que a linha seguinte com o MESMO nome compila.
	structs := make(map[string]*ast.StructStatement)
	session := make(map[string]int)

	c1 := NewWithState(make(map[string]ast.NoxyType), structs, "REPL")
	c1.SetSessionLets(session)
	if _, _, err := c1.Compile(parse(`let x: int = "tipo errado"`)); err == nil {
		t.Fatal("linha com type mismatch deveria falhar")
	}
	// REPL nao faz merge apos erro — session continua vazio.

	c2 := NewWithState(make(map[string]ast.NoxyType), structs, "REPL")
	c2.SetSessionLets(session)
	if _, _, err := c2.Compile(parse(`let x: int = 1`)); err != nil {
		t.Fatalf("nome de linha rejeitada nao deveria estar queimado: %v", err)
	}
}

func TestSharedGlobalsWithoutSessionAllowed(t *testing.T) {
	// Fora do REPL ninguem arma SetSessionLets: duas compilacoes que por
	// acaso compartilhem o mapa de globals (fronteiras de modulo) nao se
	// enxergam — o check global e por Program.
	structs := make(map[string]*ast.StructStatement)
	c1 := NewWithState(make(map[string]ast.NoxyType), structs, "a.nx")
	if _, _, err := c1.Compile(parse(`let x: int = 1`)); err != nil {
		t.Fatalf("primeira compilacao falhou: %v", err)
	}
	c2 := NewWithState(c1.GetGlobals(), structs, "b.nx")
	if _, _, err := c2.Compile(parse(`let x: string = "outro programa"`)); err != nil {
		t.Fatalf("sem sessao armada nao ha memoria entre Programs: %v", err)
	}
}
```

- [x] **Step 2: Rodar e confirmar que falham**

Run: `go test ./internal/compiler -run 'GlobalLet|ReplSession|ReplFailed|SharedGlobals' -v`
Expected: `TestGlobalLetRedeclarationFails` FALHA na asserção ("deveria falhar"); `TestReplSessionReLetFails` e `TestReplFailedLineDoesNotBurnTheName` nem COMPILAM (SetSessionLets/ProgramLets não existem) — é o vermelho esperado; `TestSharedGlobalsWithoutSessionAllowed` passa assim que o pacote compilar.

- [x] **Step 3: Implementar campo de sessão + check global**

3a. Em `internal/compiler/compiler.go`, struct `Compiler` ganha dois campos (junto de `namespaceImports`) e os métodos:

```go
	// sessionLets e a memoria de sessao do REPL (nil fora dele): nomes de
	// `let` global de linhas ANTERIORES. O predeclare so CHECA contra ele;
	// quem registra e o loop do REPL apos a linha compilar com sucesso —
	// linha rejeitada nao queima o nome. O scratch do pass 1
	// (newPass1Compiler) nao recebe o campo: fica nil, sem check nem
	// registro duplicado no two-pass.
	sessionLets map[string]int
	// programLets acumula os `let` top-level da compilacao corrente
	// (preenchido pelo predeclare) para o REPL ler via ProgramLets.
	programLets map[string]int
```

```go
// SetSessionLets arma a checagem de redeclaracao entre linhas de uma sessao
// interativa (REPL). Fora do REPL ninguem chama e o campo fica nil.
func (c *Compiler) SetSessionLets(m map[string]int) {
	c.sessionLets = m
}

// ProgramLets devolve os `let` top-level vistos pela ultima compilacao —
// o REPL faz o merge para a sessao somente apos sucesso.
func (c *Compiler) ProgramLets() map[string]int {
	return c.programLets
}
```

3b. Em `internal/compiler/function_types.go`, dentro de `predeclareGlobalBindings` (linha ~245): declarar `letSeen := make(map[string]int)` junto do `seen` existente, e no case `*ast.LetStmt` (linha ~278), antes do `resolveAnnotation`:

```go
		case *ast.LetStmt:
			// Mesma regra do check local do LetStmt (compiler.go): dois `let`
			// do mesmo nome no MESMO escopo global e redeclaracao — dentro de
			// um Program via letSeen, entre linhas do REPL via sessionLets
			// (spec §3: a sessao se comporta como um arquivo digitado linha a
			// linha). letSeen e por chamada; sessionLets e so leitura aqui.
			if prevLine, duplicate := letSeen[declaration.Name.Value]; duplicate {
				return fmt.Errorf(
					"[line %d] variable '%s' redeclared in this scope (previous declaration at line %d); hint: to update the value, use '%s = ...' without 'let'",
					declaration.Token.Line, declaration.Name.Value, prevLine, declaration.Name.Value)
			}
			if c.sessionLets != nil {
				if _, duplicate := c.sessionLets[declaration.Name.Value]; duplicate {
					return fmt.Errorf(
						"[line %d] variable '%s' redeclared in this scope (previously declared in this session); hint: to update the value, use '%s = ...' without 'let'",
						declaration.Token.Line, declaration.Name.Value, declaration.Name.Value)
				}
			}
			letSeen[declaration.Name.Value] = declaration.Token.Line
			if c.programLets == nil {
				c.programLets = make(map[string]int)
			}
			c.programLets[declaration.Name.Value] = declaration.Token.Line
			resolved, err := c.resolveAnnotation(declaration.Type, declaration.Token.Line)
			// ... (restante do case inalterado)
```

3c. Em `cmd/noxy/main.go`, `startREPL`: junto de `replGenerics` (linha ~169) criar a memória; ao compilar cada linha (linha ~257) armar; após sucesso (linha ~267) fazer o merge:

```go
	replLets := make(map[string]int) // let globais de linhas anteriores (spec §3)
```

```go
		c := compiler.NewWithState(replGlobals, replStructs, "REPL")
		c.SetGenericState(replGenerics)
		c.SetSessionLets(replLets)
		chunk, _, err := c.Compile(program)
		if err != nil {
			fmt.Printf("Compiler error: %s\n", err)
			inputBuffer = "" // Reset
			continue
		}

		// Update globals
		replGlobals = c.GetGlobals()
		// Sessao lembra os let desta linha — SO apos compilar com sucesso,
		// para uma linha rejeitada nao queimar o nome.
		for name, line := range c.ProgramLets() {
			replLets[name] = line
		}
```

- [x] **Step 4: Rodar e confirmar que passam**

Run: `go test ./internal/compiler -run 'GlobalLet|ReplSession|ReplFailed|SharedGlobals' -v`
Expected: 5 PASSAM.

Run: `go test ./internal/...` e `go build ./...`
Expected: PASS geral (mesma regra da Task 1 para fixtures quebradas).

- [x] **Step 5: Commit**

```bash
git add internal/compiler/let_redeclaration_test.go internal/compiler/function_types.go internal/compiler/compiler.go cmd/noxy/main.go
git commit -m "feat(compiler): redeclaração de let global é erro — inclusive entre linhas do REPL

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: Migração dos .nx do repositório

**Files:**
- Create (temporário, deletar antes do commit): `internal/compiler/repo_redecl_scan_test.go`
- Modify: quaisquer `.nx` em `noxy_examples/`, `noxy_libs/`, `tests/`, `benchmarks/` que o scan acusar

**Interfaces:**
- Consumes: `NewWithStateAndRoot(globals, structs, fileName, rootPath)` (mesma sequência de `cmd/noxy/main.go:321-334`: `lexer.New` → `parser.New` → `ParseProgram` → `Compile`).
- Produces: repo inteiro compilando sob a regra nova.

Contexto: um scan heurístico prévio (mesma indentação/arquivo) achou 148 candidatos, mas superconta muito — funções diferentes têm corpos na mesma indentação. O detector real é o compilador; só compilar (sem executar) evita os efeitos colaterais dos exemplos (servidores http, demos interativos).

- [x] **Step 1: Criar o scan temporário**

Criar `internal/compiler/repo_redecl_scan_test.go`:

```go
package compiler

// SCAN TEMPORARIO de migracao — NAO COMMITAR. Compila (sem executar) todos
// os .nx do repo e lista os que caem no erro novo de redeclaracao.
// tests/test_errors fica de fora: aqueles arquivos falham de proposito.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"noxy-vm/internal/ast"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
)

func TestScanRepoForLetRedeclarations(t *testing.T) {
	roots := []string{"../../noxy_examples", "../../noxy_libs", "../../tests", "../../benchmarks"}
	for _, root := range roots {
		filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil || info.IsDir() || !strings.HasSuffix(path, ".nx") {
				return nil
			}
			if strings.Contains(filepath.ToSlash(path), "/test_errors/") {
				return nil
			}
			src, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			p := parser.New(lexer.New(string(src)))
			prog := p.ParseProgram()
			if len(p.Errors()) > 0 {
				return nil // erro de parse nao e alvo deste scan
			}
			c := NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), path, filepath.Dir(path))
			if _, _, cerr := c.Compile(prog); cerr != nil && strings.Contains(cerr.Error(), "redeclared in this scope") {
				t.Errorf("%s: %v", path, cerr)
			}
			return nil
		})
	}
}
```

- [x] **Step 2: Rodar o scan**

Run: `go test ./internal/compiler -run TestScanRepoForLetRedeclarations -v`
Expected: lista (possivelmente vazia) de arquivos com re-let genuíno de mesmo escopo.

- [x] **Step 3: Corrigir cada arquivo acusado**

Critério por caso: se o segundo `let` tem o mesmo tipo e a intenção é reset, trocar por atribuição (`x = valor`); se o tipo difere ou a intenção é outra variável, renomear a segunda. Não alterar a lógica dos exemplos.

- [x] **Step 4: Re-rodar o scan até zerar, depois deletar o arquivo temporário**

Run: `go test ./internal/compiler -run TestScanRepoForLetRedeclarations -v`
Expected: PASS sem `t.Errorf`.

```powershell
Remove-Item internal\compiler\repo_redecl_scan_test.go
```

- [x] **Step 5: Commit (só se houve correção de .nx)**

```bash
git add -A noxy_examples noxy_libs tests benchmarks
git commit -m "fix(examples): migra re-lets de mesmo escopo para atribuição/rename

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: Verificação integrada (suite e2e + REPL + crivo)

**Files:** nenhum novo — só execução.

- [x] **Step 1: Suite Go completa**

Run: `go test ./internal/...`
Expected: PASS em todos os pacotes.

- [x] **Step 2: Runner e2e noxy (AGENTS.md)**

Run: `go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx`
Expected: saída final do runner sem falhas (mesmo resultado do develop antes da mudança).

- [x] **Step 3: Smoke do REPL via stdin (sessão = arquivo linha a linha)**

Run (PowerShell):
```powershell
"let x: int = 1`nlet x: string = `"re-let`"`nx = 42`nprint(x)" | go run ./cmd/noxy
```
Expected: a linha 2 imprime `Compiler error: ... variable 'x' redeclared in this scope (previously declared in this session) ...`; a sessão continua e as linhas 3–4 imprimem `42` (atribuição segue sendo o caminho). (EOF do pipe encerra o REPL. Se o pipe travar por manuseio de console — ver histórico de console mode no Windows — validar manualmente e registrar no PR.)

- [x] **Step 4: Programas de referência da sessão**

Run: `go run ./cmd/noxy noxy_examples/crivo.nx`
Expected: primos até 50 (2, 3, 5, ..., 47), exit 0 — o `let i` pós-for continua legal.

Criar `$env:TEMP\redecl_neg.nx` com:
```noxy
let x: int = 1
let x: string = "virei string"
```
Run: `go run ./cmd/noxy $env:TEMP\redecl_neg.nx`
Expected: `Compiler error: [line 2] variable 'x' redeclared in this scope (previous declaration at line 1); hint: to update the value, use 'x = ...' without 'let'`, exit code 1.

---

### Task 5: Documentação breve na spec

**Files:**
- Modify: `docs/NOXY_LANGUAGE_SPEC.md` — §3 "Variable Declarations" (linhas ~445–451) e "### For Loop" (linhas ~976–998)

- [x] **Step 1: Reescrever a §3**

Substituir o bloco atual da §3 (do `## 3. Variable Declarations` até antes do `---` que precede `## 4. Functions`) por:

````markdown
## 3. Variable Declarations

```noxy
let name: type = value
```

Variables can be reassigned, but the new value **MUST** be of the same type as declared.

### Redeclaration vs. reassignment

`let` introduces a new binding; `=` updates an existing one. Declaring a
name that already exists **in the same scope** is a compile-time error —
assignment is the only way to change a variable's value, and no sequence
of statements can change its type:

```noxy
let x: int = 1
let x: int = 2       // ERROR: variable 'x' redeclared in this scope
let x: string = "s"  // ERROR: same rule — redeclaration can never change the type
x = 2                // ✓ OK — assignment updates the value
```

The REPL applies the same rule: a session behaves like a single file typed
line by line, so re-declaring a name from an earlier line is rejected —
assign to it instead. A rejected line does not claim its name: after an
error you may still `let` that name.

### Shadowing in inner scopes

A `let` in an **inner** scope (a block body, or a function body shadowing a
parameter) creates a distinct variable that hides the outer one until the
block ends. The outer variable is untouched:

```noxy
let x: int = 99
if x > 0 then
    let x: string = "inner"  // ✓ OK: new variable, dies at 'end'
end
print(x)                     // 99
```
````

- [x] **Step 2: Anotar o escopo da variável do For Loop**

Logo após o exemplo de **Strings** da seção `### For Loop` (antes de `### Defer and deterministic cleanup`), inserir:

````markdown
The loop variable is **scoped to the loop**: it is created by the `for`,
may shadow an outer variable of the same name (left untouched), and no
longer exists after `end`. It is also **rebound from the collection on
every iteration** — assigning to it inside the body is allowed but never
affects the sequence:

```noxy
for i in [1, 2, 3] do
    i = 5        // allowed; next iteration rebinds i from the collection
end
print(i)         // ERROR: 'i' is not defined here
```
````

- [x] **Step 3: Conferir renderização e commit**

Verificar que os fences aninhados fecharam certo (buscar por ```` ``` ```` desbalanceado na região editada).

```bash
git add docs/NOXY_LANGUAGE_SPEC.md
git commit -m "docs(spec): redeclaração vs reatribuição, shadowing e escopo da variável do for

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: CHANGELOG + bump de versão

**Files:**
- Modify: `internal/version/version.go` (`const Version = "v0.8.0"` → `"v0.9.0"`)
- Modify: `CHANGELOG.md` (nova seção no topo)

- [x] **Step 1: Bump**

`internal/version/version.go`:
```go
const Version = "v0.9.0"
```

- [x] **Step 2: Entrada no CHANGELOG**

Inserir no topo de `CHANGELOG.md` (acima de `## [0.8.0]`):

```markdown
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
```

- [x] **Step 3: Verificação final e commit**

Run: `go run ./cmd/noxy --version`
Expected: `Noxy v0.9.0`

Run: `go test ./internal/...` — Expected: PASS.

```bash
git add internal/version/version.go CHANGELOG.md
git commit -m "chore(version): noxy v0.9.0 — redeclaração de let vira erro (BREAKING)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Follow-ups anotados (fora deste plano)

- Parâmetros duplicados (`func f(x: int, x: int)`) não são cobertos pelo check (vivem fora do case LetStmt) — candidata a issue própria.
- `let x` colidindo com `func x`/import top-level não é flagrado (o check é let×let) — decidir depois se merece regra.
- Erro de global inexistente é de runtime (`undefined global variable`) — resolução compile-time de globais é outra conversa.
