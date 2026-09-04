# Escrita tipada pelo namespace e identidade de struct por declaração — plano de implementação (issue #133)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** o tipo de um struct no compilador passa a ser a declaração (`Decl`), não a grafia, então um valor que o programa não sabe nomear continua totalmente tipado; e `m.x = v` pelo namespace vira escrita legal, tipada e viva no store do módulo.

**Architecture:** `ast.PrimitiveType` ganha `Decl *StructStatement` (identidade; `Name` é só exibição). Um passe in place em `resolveAnnotation` preenche `Decl` em toda anotação; `typesEquivalent` compara ponteiros; `programViewType` deixa de falhar e escolhe só a grafia (alias visível → canônico `base.V`). No VM, `OP_SET_PROPERTY` e `REF_PROPERTY` ganham ramo `ObjMap` (o objeto do namespace compartilha o `bindingStore` do módulo). Ordem: item 2 (identidade) antes do item 1 (escrita), porque a raiz `m.a` de `m.a.b = v` precisa do tipo traduzido.

**Tech Stack:** Go 1.25, módulo `noxy-vm`; testes `go test` por pacote; programas Noxy de string nos testes (`compileSourceAtRoot`, `runModuleProgram`).

**Spec:** `docs/superpowers/specs/2026-09-04-issue-133-namespace-write-declaration-identity-design.md` — o plano argumenta a partir dela; leia as duas.

## Global Constraints

- Verificação obrigatória após qualquer modificação (AGENTS.md): `go build ./... && go vet ./...`, `go test ./internal/... -count=1`, `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx`.
- `gofmt -d` limpo nos arquivos tocados; `git diff --numstat` sem arquivo reescrito por EOL (checkout pode ser CRLF).
- Commits: `tipo(escopo): descrição em português (issue #133)`, com o rodapé `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` e `Claude-Session: https://claude.ai/code/session_01Ua8f3ncdebyQc17FTgnDk1`.
- **Decl é a identidade; Name nunca.** Nenhum site novo pode comparar struct por `Name`/`String()` quando há `Decl` (spec §1.3).
- **Um único ponto preenche `Decl`**: `bindStructDecls` dentro de `resolveAnnotation`, in place, sem alocar (spec §1.4). `needsAnnotationResolution` **não muda**.
- **Instância genérica** (`isGenericInstanceName`, nome com `::`) nunca recebe `Decl` e continua dinâmica na tradução (spec §1.6).
- **Runtime fora do escopo**: `ObjStruct.Name`/`RuntimeTypeInfo.Name` continuam o nome cru da declaração (spec §1.6).
- O compilador nunca escreve em stdout/stderr; erros são `fmt.Errorf("[line %d] ...")`.
- Nenhum opcode, builtin ou pilha nova: as guardas de arquitetura (`architecture_test.go`, `inline_guard_test.go`, `builtins_registry_test.go`) não mudam.
- Um implementador por vez na árvore (memória do projeto: implementadores paralelos colidem no índice do git).

---

## Mapa de arquivos

| Arquivo | Responsabilidade neste plano |
|---|---|
| `internal/ast/ast.go`, `internal/ast/clone.go` | campo `Decl` em `PrimitiveType`; clone copia o ponteiro (Task 1) |
| `internal/compiler/generics_substitute.go` | `substituteType` copia `Decl` (Task 1) |
| `internal/compiler/struct_identity.go` (novo) | `structDeclarationOf`, `bindStructDecls` (Task 2) |
| `internal/compiler/generics_structs.go` | `resolveAnnotation` chama `bindStructDecls` (Task 2) |
| `internal/compiler/compiler.go` | `typesEquivalent` por ponteiro; atalho por string sai de `areTypesCompatible`; `newStructFunctionType(n, …)`; ramo de atribuição a membro (Tasks 2, 3, 8) |
| `internal/compiler/function_types.go` | atalho por string sai de `areStrictTypesCompatible`; `containsCallableType` visita por ponteiro; `structOperandName`; `newStructFunctionType(decl, params)` (Tasks 2, 3) |
| `internal/compiler/generics.go` | atalho por string sai da checagem de template importado (Task 2) |
| `internal/compiler/member_types.go`, `field_index.go`, `default_init.go`, `runtime_types.go` | consumidores resolvem por `structDeclarationOf`; `firstUnknownTypeName` recebe o nó; `programViewType`/`programStructName` só exibição (Tasks 3, 4, 6) |
| `internal/compiler/module_exports.go` | `newStructFunctionType(declaration, …)`; `importBindingFrom` traduz; `declaringModule` (Tasks 3, 5) |
| `internal/compiler/namespace_member_types.go` | reexport pelo namespace (Task 5) |
| `internal/compiler/namespace_write.go` (novo) | `pureNamespaceAlias`, `compileNamespaceMemberAssignment` (Task 8) |
| `internal/compiler/cow_lowering.go`, `borrow_place.go` | raiz tipada de lvalue pelo namespace (Task 8) |
| `internal/vm/field_ops.go`, `internal/vm/references.go`, `internal/vm/executor.go` | ramos `ObjMap` de `OP_SET_PROPERTY` e `REF_PROPERTY` (Task 7) |
| `docs/NOXY_LANGUAGE_SPEC.md`, `CHANGELOG.md`, `noxy_examples/` | documentação e exemplo (Task 9) |

Testes: `internal/compiler/struct_identity_test.go` (novo), `namespace_write_test.go` (novo), edições em `member_access_typing_test.go`, `namespace_member_typing_test.go`, `unknown_type_test.go`; `internal/vm/namespace_write_test.go` (novo), `map_property_test.go` (novo), edições em `module_exports_test.go`, `namespace_ref_target_test.go`.

Helpers de teste existentes (use-os; não crie duplicatas):

- compilador (`member_access_typing_test.go`): `writeModuleFile(t, root, "m.nx", src)`, `compileSourceAtRoot(t, root, src) error`, `requireErrorMentions(t, err, "trecho"...)`, `requireErrorLacks(t, err, "trecho"...)`, `requireNoError(t, err)`; módulos prontos `rollModule`/`rollRoot(t)` (namespace_member_typing_test.go: `struct V{x,y: float}`, `let total: int`, `let limit = 10`, `roll`, `norm`, `bump`) e `dbModule`/`dbRoot(t)` (`struct Row`, `struct QueryResult{rows: Row[], count, by_name: map[string, Row]}`, `q()`).
- VM (`module_exports_test.go`): `writeModuleFiles(t, map[string]string{...}) root`, `runModuleProgram(t, root, src) (value.Value, error)`; o programa chama `test_report(x)` e o teste lê `reported.Int()` / `reported.Obj.(string)`.

---

### Task 1: `Decl` em `ast.PrimitiveType`; clone e substituição preservam o ponteiro

**Files:**
- Modify: `internal/ast/ast.go:50-54`
- Modify: `internal/ast/clone.go:157-158`
- Modify: `internal/compiler/generics_substitute.go:43-44`
- Test: `internal/compiler/struct_identity_test.go` (novo)

**Interfaces:**
- Produces: `ast.PrimitiveType{Name string; Decl *ast.StructStatement}`. Todo código posterior lê `prim.Decl`.

- [ ] **Step 1: Escrever o teste que falha**

```go
package compiler

import (
	"testing"

	"noxy-vm/internal/ast"
)

// Issue #133: o tipo de struct carrega a identidade da declaracao (Decl);
// Name e so a grafia. Clonar ou substituir um tipo copia o PONTEIRO — clonar
// a declaracao quebraria a identidade em silencio.
func TestCloneAndSubstitutePreserveStructDecl(t *testing.T) {
	decl := &ast.StructStatement{Name: "P"}
	original := &ast.ArrayType{ElementType: &ast.PrimitiveType{Name: "P", Decl: decl}}

	cloned := ast.CloneType(original).(*ast.ArrayType).ElementType.(*ast.PrimitiveType)
	if cloned.Decl != decl {
		t.Fatalf("CloneType lost Decl: got %p, want %p", cloned.Decl, decl)
	}
	substituted := substituteType(original, map[string]ast.NoxyType{}).(*ast.ArrayType).ElementType.(*ast.PrimitiveType)
	if substituted.Decl != decl {
		t.Fatalf("substituteType lost Decl: got %p, want %p", substituted.Decl, decl)
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/compiler -run TestCloneAndSubstitutePreserveStructDecl -count=1`
Expected: erro de compilação `unknown field Decl in struct literal`.

- [ ] **Step 3: Implementar**

`internal/ast/ast.go`:

```go
type PrimitiveType struct {
	Name string
	// Decl e a identidade da declaracao de struct que este tipo designa
	// (issue #133): nil para primitivos (int, string, any...), para
	// instancias genericas monomorfizadas (`main::Caixa<int>`) e para
	// anotacoes que nenhum ponto de resolucao tocou ainda. Name e SO a grafia
	// de exibicao/anotacao: dois nos com o mesmo Decl sao o mesmo tipo mesmo
	// com Name diferente (`V` e `base.V`), e dois Decl distintos nunca sao o
	// mesmo tipo mesmo com Name igual.
	Decl *StructStatement
}
```

`internal/ast/clone.go`, case `*PrimitiveType`:

```go
	case *PrimitiveType:
		return &PrimitiveType{Name: n.Name, Decl: n.Decl}
```

`internal/compiler/generics_substitute.go`, case `*ast.PrimitiveType`:

```go
	case *ast.PrimitiveType:
		return &ast.PrimitiveType{Name: n.Name, Decl: n.Decl}
```

- [ ] **Step 4: Rodar e ver passar**

Run: `go test ./internal/compiler -run TestCloneAndSubstitutePreserveStructDecl -count=1` → PASS. Depois `go build ./... && go test ./internal/ast ./internal/compiler -count=1` → PASS (nada mais muda ainda).

- [ ] **Step 5: Commit**

```bash
git add internal/ast/ast.go internal/ast/clone.go internal/compiler/generics_substitute.go internal/compiler/struct_identity_test.go
git commit -m "feat(ast): PrimitiveType carrega Decl, a identidade da declaração de struct; clone e substituição copiam o ponteiro (issue #133)"
```

---

### Task 2: identidade por `Decl` — preenchimento único, `typesEquivalent` por ponteiro, atalhos por string fora

**Files:**
- Create: `internal/compiler/struct_identity.go`
- Modify: `internal/compiler/generics_structs.go:45-47` (`resolveAnnotation`)
- Modify: `internal/compiler/compiler.go:3466-3481` (`typesEquivalent`), `:3441-3443` (`areTypesCompatible`)
- Modify: `internal/compiler/function_types.go:265` (`areStrictTypesCompatible`)
- Modify: `internal/compiler/generics.go:886`
- Test: `internal/compiler/struct_identity_test.go`

**Interfaces:**
- Produces: `func (c *Compiler) structDeclarationOf(prim *ast.PrimitiveType) *ast.StructStatement` — `prim.Decl` se houver; senão `structDeclaration(prim.Name)`; nil para builtin. `func (c *Compiler) bindStructDecls(t ast.NoxyType)` — passe in place.
- Consumes: `structDeclaration(name)`, `isBuiltinTypeName`, `isGenericInstanceName` (member_types.go).

- [ ] **Step 1: Escrever os testes que falham** (acrescentar a `struct_identity_test.go`)

```go
func TestResolveAnnotationBindsDeclInPlaceWithoutAllocating(t *testing.T) {
	// Um `let` anotado com composto de struct (P[], ref P, P?, map, func):
	// o no da anotacao volta pelo MESMO ponteiro (fast path intacto) e cada
	// PrimitiveType de struct dentro dele sai com Decl preenchido.
	src := `struct P
    x: int
end
let a: P[] = []
let b: map[string, P] = {}
let f: func(P) -> P? = func(p: P) -> P? return null end
`
	program := parseForTest(t, src)
	c := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), "main.nx")
	if _, _, err := c.Compile(program); err != nil {
		t.Fatalf("compile: %v", err)
	}
	decl := c.structs["P"]
	if decl == nil {
		t.Fatal("struct P not registered")
	}
	for _, statement := range program.Statements {
		let, ok := statement.(*ast.LetStmt)
		if !ok {
			continue
		}
		var prims []*ast.PrimitiveType
		collectStructPrimitives(let.Type, &prims)
		if len(prims) == 0 {
			t.Fatalf("let %s: no struct primitives found in %s", let.Name.Value, let.Type)
		}
		for _, prim := range prims {
			if prim.Decl != decl {
				t.Fatalf("let %s: %s has Decl %p, want %p", let.Name.Value, prim.Name, prim.Decl, decl)
			}
		}
	}
}

// collectStructPrimitives junta os PrimitiveType nao-builtin de t.
func collectStructPrimitives(t ast.NoxyType, out *[]*ast.PrimitiveType) {
	switch typed := t.(type) {
	case *ast.PrimitiveType:
		if !isBuiltinTypeName(typed.Name) {
			*out = append(*out, typed)
		}
	case *ast.ArrayType:
		collectStructPrimitives(typed.ElementType, out)
	case *ast.MapType:
		collectStructPrimitives(typed.KeyType, out)
		collectStructPrimitives(typed.ValueType, out)
	case *ast.RefType:
		collectStructPrimitives(typed.ElementType, out)
	case *ast.NullableType:
		collectStructPrimitives(typed.ElementType, out)
	case *ast.ChanType:
		collectStructPrimitives(typed.ElementType, out)
	case *ast.FunctionType:
		for _, p := range typed.Params {
			collectStructPrimitives(p, out)
		}
		collectStructPrimitives(typed.Return, out)
	}
}

func parseForTest(t *testing.T, src string) *ast.Program {
	t.Helper()
	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	return program
}

func TestGenericInstanceAnnotationHasNoDecl(t *testing.T) {
	src := `struct Caixa<T>
    v: T
end
let c: Caixa<int> = Caixa(1)
`
	program := parseForTest(t, src)
	c := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), "main.nx")
	if _, _, err := c.Compile(program); err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, statement := range program.Statements {
		if let, ok := statement.(*ast.LetStmt); ok {
			prim, isPrim := let.Type.(*ast.PrimitiveType)
			if !isPrim || !isGenericInstanceName(prim.Name) {
				t.Fatalf("annotation not flattened to instance name: %s", let.Type)
			}
			if prim.Decl != nil {
				t.Fatalf("generic instance must not carry Decl (spec §1.6), got %p", prim.Decl)
			}
		}
	}
}

func TestTypesEquivalentUsesDeclNotName(t *testing.T) {
	c := New()
	a := &ast.StructStatement{Name: "V"}
	b := &ast.StructStatement{Name: "V"}
	if !c.typesEquivalent(&ast.PrimitiveType{Name: "V", Decl: a}, &ast.PrimitiveType{Name: "base.V", Decl: a}) {
		t.Fatal("same Decl with different Name must be equivalent")
	}
	if c.typesEquivalent(&ast.PrimitiveType{Name: "V", Decl: a}, &ast.PrimitiveType{Name: "V", Decl: b}) {
		t.Fatal("different Decl with the same Name must NOT be equivalent")
	}
	if !c.areTypesCompatible(&ast.PrimitiveType{Name: "V", Decl: a}, &ast.PrimitiveType{Name: "w.V", Decl: a}) {
		t.Fatal("areTypesCompatible must follow Decl")
	}
	if c.areTypesCompatible(&ast.PrimitiveType{Name: "V", Decl: a}, &ast.PrimitiveType{Name: "V", Decl: b}) {
		t.Fatal("areTypesCompatible must not unify two declarations by Name")
	}
	if c.areStrictTypesCompatible(&ast.PrimitiveType{Name: "V", Decl: a}, &ast.PrimitiveType{Name: "V", Decl: b}) {
		t.Fatal("areStrictTypesCompatible must not unify two declarations by Name")
	}
	if !c.typesEquivalent(&ast.PrimitiveType{Name: "int"}, &ast.PrimitiveType{Name: "int"}) {
		t.Fatal("primitives still compare by name")
	}
}
```

Imports do arquivo de teste: `testing`, `noxy-vm/internal/ast`, `noxy-vm/internal/lexer`, `noxy-vm/internal/parser`.

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/compiler -run 'TestResolveAnnotationBindsDecl|TestGenericInstanceAnnotationHasNoDecl|TestTypesEquivalentUsesDeclNotName' -count=1`
Expected: `TestResolveAnnotationBindsDecl…` falha com `has Decl 0x0`; `TestTypesEquivalentUsesDeclNotName` falha em "different Decl with the same Name must NOT be equivalent" (hoje `x.Name == y.Name` vence); o de instância genérica passa (ninguém preenche nada ainda) — é o controle.

- [ ] **Step 3: Implementar `struct_identity.go`**

```go
package compiler

import "noxy-vm/internal/ast"

// Issue #133: o tipo de um struct e a sua DECLARACAO, nao a grafia. Este
// arquivo concentra as duas operacoes de identidade:
//
//   - structDeclarationOf: a declaracao que um PrimitiveType designa —
//     prim.Decl quando preenchido; senao a resolucao por nome de sempre
//     (structDeclaration), que fica como rede de seguranca para nos que
//     nenhum ponto de resolucao tocou (a revisao adversarial procura esses);
//   - bindStructDecls: o UNICO ponto que preenche Decl. Roda dentro de
//     resolveAnnotation, in place e sem alocar — o fast path de custo zero
//     das anotacoes sem genericos (needsAnnotationResolution) fica intacto.
//
// Instancia generica (`main::Caixa<int>`, isGenericInstanceName) nunca recebe
// Decl: o nome achatado nao e identidade entre unidades de compilacao (spec
// §1.6) e programViewType a trata antes de olhar Decl.
func (c *Compiler) structDeclarationOf(prim *ast.PrimitiveType) *ast.StructStatement {
	if prim == nil {
		return nil
	}
	if prim.Decl != nil {
		return prim.Decl
	}
	if isBuiltinTypeName(prim.Name) {
		return nil
	}
	return c.structDeclaration(prim.Name)
}

// bindStructDecls preenche Decl em todo PrimitiveType de struct dentro de t
// que ainda nao o tenha, resolvendo o nome no escopo ATUAL do compilador
// (c.structs para nome simples, namespaceImports para `ns.T`). Nome que nao
// resolve fica com Decl nil e e reportado por checkDeclaredType. Idempotente:
// resolveStructFieldAnnotations roda mais de uma vez de proposito.
func (c *Compiler) bindStructDecls(t ast.NoxyType) {
	switch typed := t.(type) {
	case *ast.PrimitiveType:
		if typed.Decl != nil || isBuiltinTypeName(typed.Name) || isGenericInstanceName(typed.Name) {
			return
		}
		typed.Decl = c.structDeclaration(typed.Name)
	case *ast.ArrayType:
		c.bindStructDecls(typed.ElementType)
	case *ast.MapType:
		c.bindStructDecls(typed.KeyType)
		c.bindStructDecls(typed.ValueType)
	case *ast.RefType:
		c.bindStructDecls(typed.ElementType)
	case *ast.NullableType:
		c.bindStructDecls(typed.ElementType)
	case *ast.ChanType:
		c.bindStructDecls(typed.ElementType)
	case *ast.FunctionType:
		for _, param := range typed.Params {
			c.bindStructDecls(param)
		}
		c.bindStructDecls(typed.Return)
	case *ast.GenericType:
		for _, arg := range typed.Args {
			c.bindStructDecls(arg)
		}
	}
}
```

`generics_structs.go`, início de `resolveAnnotation`:

```go
func (c *Compiler) resolveAnnotation(t ast.NoxyType, line int) (ast.NoxyType, error) {
	// Issue #133: identidade da declaracao em todo struct da anotacao, in
	// place, ANTES do fast path — que nao muda.
	c.bindStructDecls(t)
	if !needsAnnotationResolution(t) {
		return t, nil
	}
```

`compiler.go`, `typesEquivalent`, caso `*ast.PrimitiveType`:

```go
	case *ast.PrimitiveType:
		y, ok := b.(*ast.PrimitiveType)
		if !ok {
			return false
		}
		// Issue #133: Decl e a identidade; Name nunca. Se qualquer lado
		// designa uma declaracao, decide o ponteiro (o outro lado resolve por
		// Decl ou, sem ele, pelo nome — structDeclarationOf). So dois nomes
		// que nao designam struct (primitivos, nomes ainda nao resolvidos)
		// comparam por Name.
		da, db := c.structDeclarationOf(x), c.structDeclarationOf(y)
		if da != nil || db != nil {
			return da == db
		}
		return x.Name == y.Name
```

`compiler.go` `areTypesCompatible` (~3441): **remover** o bloco

```go
	if expected.String() == actual.String() {
		return true
	}
```

`function_types.go:265` (dentro de `areStrictTypesCompatible`): trocar `return expected.String() == actual.String() || c.typesEquivalent(expected, actual)` por `return c.typesEquivalent(expected, actual)`.

`generics.go:886`: trocar `!(definedType.String() == importerType.String() || c.typesEquivalent(definedType, importerType))` por `!c.typesEquivalent(definedType, importerType)`.

Atualizar o comentário de `looselySameType` (`compiler.go` ~3548) acrescentando: `Decl NAO e consultado aqui de proposito: e a comparacao pura do unificador; a pass 2 decide com typesEquivalent.`

- [ ] **Step 4: Rodar e ver passar**

Run: `go test ./internal/compiler -run 'TestResolveAnnotationBindsDecl|TestGenericInstanceAnnotationHasNoDecl|TestTypesEquivalentUsesDeclNotName' -count=1` → PASS.
Run: `go build ./... && go test ./internal/... -count=1` → PASS. Se algum teste de genéricos (`generics_*_test.go`, `internal/vm/generics_modules_e2e_test.go`) quebrar por causa do atalho removido em `generics.go:886`, o caso é um `definedType` sem `Decl`: confira que `moduleTopLevelBindings` do módulo passou pelo `validator.Compile` (é o que preenche `Decl` no AST do módulo) — não reintroduza o atalho.
Run: `grep -n 'String() == ' internal/compiler/*.go | grep -v _test` → só as comparações com literais (`"void"`, `"int"`, `"float"`, `"any"`) e `compiler.go` ~3630 (lista de primitivos) devem restar; nenhuma `expected.String() == actual.String()`.

- [ ] **Step 5: Commit**

```bash
git add internal/compiler/struct_identity.go internal/compiler/struct_identity_test.go internal/compiler/generics_structs.go internal/compiler/compiler.go internal/compiler/function_types.go internal/compiler/generics.go
git commit -m "feat(compiler): identidade de struct por Decl — bindStructDecls em resolveAnnotation, typesEquivalent por ponteiro, atalhos por string removidos (issue #133)"
```

---

### Task 3: consumidores resolvem pela declaração; `newStructFunctionType` recebe a declaração

**Files:**
- Modify: `internal/compiler/member_types.go:26-31` (`memberType`)
- Modify: `internal/compiler/field_index.go:28-34` (`fieldSlot`)
- Modify: `internal/compiler/default_init.go:26-30` (`typeWithoutDefault`)
- Modify: `internal/compiler/function_types.go:50-76` (`containsCallableType`), `:131-145` (`structOperandName`), `:443-448` (`newStructFunctionType`), `:434`
- Modify: `internal/compiler/runtime_types.go:107-108`, `:158-160` (struct por `Decl`), `:321-324` e `:360-365` (`firstUnknownTypeName` recebe o nó)
- Modify: `internal/compiler/compiler.go:1040`, `:3417`
- Modify: `internal/compiler/module_exports.go:586`, `:668`
- Modify: `internal/compiler/generics_structs.go:292`
- Test: `internal/compiler/struct_identity_test.go`

**Interfaces:**
- Produces: `newStructFunctionType(decl *ast.StructStatement, params []ast.NoxyType) *ast.FunctionType` (retorno `{Name: decl.Name, Decl: decl}`; `Decl` nil se `isGenericInstanceName(decl.Name)`); `containsCallableType(t, visiting map[*ast.StructStatement]bool)`; `firstUnknownTypeName(t, resolves func(*ast.PrimitiveType) bool)`.

- [ ] **Step 1: Escrever os testes que falham**

```go
func TestMemberTypeFollowsDeclNotName(t *testing.T) {
	// Um no com Decl e Name canonico (`base.V`, que structDeclaration NAO
	// resolve: `base` nao e alias) tem de resolver campo, slot e default
	// pela declaracao.
	program := parseForTest(t, "struct V\n    x: int\nend\n")
	c := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), "main.nx")
	if _, _, err := c.Compile(program); err != nil {
		t.Fatalf("compile: %v", err)
	}
	decl := c.structs["V"]
	canonical := &ast.PrimitiveType{Name: "base.V", Decl: decl}
	if got := c.memberType(canonical, "x"); got == nil || got.String() != "int" {
		t.Fatalf("memberType via Decl: got %v, want int", got)
	}
	if _, ok := c.fieldSlot(canonical, "x"); !ok {
		t.Fatal("fieldSlot must resolve via Decl for a program struct")
	}
	if c.typeWithoutDefault(canonical) == nil {
		t.Fatal("struct via Decl has no default value (spec §3)")
	}
	if err := c.checkDeclaredType(canonical, 1, "variable 'v'"); err != nil {
		t.Fatalf("a type with Decl is known regardless of its Name: %v", err)
	}
}

// Caracterizacao (spec §1.5, no orfao): um no com grafia canonica e SEM Decl
// nao resolve — e por isso que todo site que reconstroi um PrimitiveType a
// partir de outro tem de carregar Decl junto.
func TestCanonicalNameWithoutDeclDoesNotResolve(t *testing.T) {
	program := parseForTest(t, "struct V\n    x: int\nend\n")
	c := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), "main.nx")
	if _, _, err := c.Compile(program); err != nil {
		t.Fatalf("compile: %v", err)
	}
	orphan := &ast.PrimitiveType{Name: "base.V"}
	if c.structDeclarationOf(orphan) != nil {
		t.Fatal("an orphan canonical name must not resolve by name")
	}
	if c.typesEquivalent(orphan, &ast.PrimitiveType{Name: "V", Decl: c.structs["V"]}) {
		t.Fatal("orphan must not be equivalent to the declaration")
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/compiler -run 'TestMemberTypeFollowsDeclNotName|TestCanonicalNameWithoutDeclDoesNotResolve' -count=1`
Expected: o primeiro falha em `memberType via Decl: got <nil>`; o segundo passa (controle: hoje já não resolve).

- [ ] **Step 3: Implementar**

`member_types.go`, `memberType`: trocar `definition := c.structDeclaration(primitive.Name)` por `definition := c.structDeclarationOf(primitive)`.

`field_index.go`, `fieldSlot`: idem (`c.structDeclarationOf(primitive)`).

`default_init.go`, `typeWithoutDefault`: `if c.structDeclarationOf(typed) != nil {`.

`function_types.go`, `containsCallableType`:

```go
func (c *Compiler) containsCallableType(t ast.NoxyType, visiting map[*ast.StructStatement]bool) bool {
	switch typed := t.(type) {
	case *ast.FunctionType:
		return true
	case *ast.PrimitiveType:
		if typed.Name == "func" {
			return true
		}
		definition := c.structDeclarationOf(typed)
		if definition == nil {
			return false
		}
		if visiting == nil {
			visiting = make(map[*ast.StructStatement]bool)
		}
		// Issue #133: marca de ciclo por DECLARACAO — dois homonimos de
		// modulos distintos nao compartilham a marca.
		if visiting[definition] {
			return false
		}
		visiting[definition] = true
		defer delete(visiting, definition)
		for _, field := range definition.FieldsList {
			if c.containsCallableType(field.Type, visiting) {
				return true
			}
		}
		return false
```

(os demais `case` seguem iguais, passando `visiting`). Os chamadores `c.containsCallableType(expected, nil)` (`compiler.go:3417`, `function_types.go:208`, `:233`) compilam sem mudança.

`function_types.go`, `structOperandName`: `if c.structDeclarationOf(prim) != nil { return prim.Name, true }` (o `Name` é exibição — é o que a mensagem quer).

`function_types.go`, `newStructFunctionType`:

```go
// newStructFunctionType e o tipo do construtor de decl: o retorno carrega a
// DECLARACAO (issue #133), exceto para instancia generica, que segue por
// nome (spec §1.6).
func newStructFunctionType(decl *ast.StructStatement, params []ast.NoxyType) *ast.FunctionType {
	result := &ast.PrimitiveType{Name: decl.Name}
	if !isGenericInstanceName(decl.Name) {
		result.Decl = decl
	}
	return &ast.FunctionType{Params: params, Return: result}
}
```

Chamadores: `compiler.go:1040` → `newStructFunctionType(n, paramTypes)`; `function_types.go:434` → `newStructFunctionType(declaration, params)`; `module_exports.go:586` e `:668` → `newStructFunctionType(declaration, params)`; `generics_structs.go:292` → `newStructFunctionType(instance, params)`.

`runtime_types.go`, `requiresRuntimeValueType`, caso `*ast.PrimitiveType`:

```go
	case *ast.PrimitiveType:
		definition := typed.Decl
		if definition == nil {
			definition = c.lookupStructFrom(origin, typed.Name)
		}
		if definition == nil || visiting[definition] {
			return false
		}
```

`runtime_types.go`, `runtimeTypeInfoWithStructs`, após o `switch` de primitivos:

```go
		definition := typed.Decl
		if definition == nil {
			definition = c.lookupStructFrom(origin, typed.Name)
		}
		if definition == nil {
			return nil, false
		}
```

(o `Name: definition.Name` do `RuntimeTypeInfo` **fica** — identidade de runtime é o nome cru, spec §1.6.)

`runtime_types.go`, `checkDeclaredTypeFrom`:

```go
	name, found := firstUnknownTypeName(t, func(candidate *ast.PrimitiveType) bool {
		return c.structDeclarationOf(candidate) != nil
	})
```

e `firstUnknownTypeName(t ast.NoxyType, resolves func(*ast.PrimitiveType) bool)` com o caso

```go
	case *ast.PrimitiveType:
		if !isBuiltinTypeName(typed.Name) && !resolves(typed) {
			return typed.Name, true
		}
```

(as chamadas recursivas só repassam `resolves`).

- [ ] **Step 4: Rodar e ver passar**

Run: `go test ./internal/compiler -run 'TestMemberTypeFollowsDeclNotName|TestCanonicalNameWithoutDeclDoesNotResolve' -count=1` → PASS.
Run: `go build ./... && go vet ./... && go test ./internal/... -count=1` → PASS.
Run: `grep -n 'structDeclaration(primitive.Name)\|structDeclaration(typed.Name)\|structDeclaration(prim.Name)' internal/compiler/*.go | grep -v _test` → deve restar só `narrowing.go` (`isPureBuiltinCall`, nome de identificador, não de tipo) e `call_result.go` (instância por nome, §1.6). Qualquer outro site é um consumidor esquecido: troque por `structDeclarationOf`.

- [ ] **Step 5: Commit**

```bash
git add internal/compiler
git commit -m "refactor(compiler): campo, slot, default, callable, runtime type e unknown type resolvem pela declaração; construtor carrega Decl (issue #133)"
```

---

### Task 4: `programViewType`/`programStructName` só exibição — alias visível não sombreado, senão canônico `base.V`

**Files:**
- Modify: `internal/compiler/member_types.go:63-98` (`programViewType`, caso `*ast.PrimitiveType`), `:163-181` (`programStructName`)
- Test: `internal/compiler/member_access_typing_test.go:218-237`, `internal/compiler/namespace_member_typing_test.go`

**Interfaces:**
- Produces: `programStructName(decl) string` nunca devolve `""`; `programViewType` devolve `(nil,false)` só para instância genérica, `GenericType`/`TypeParamType` residual e `nil`.
- Consumes: `isShadowedByLocal(name)` (generics_target.go), `structOrigin(decl)`.

- [ ] **Step 1: Inverter os testes que fixam "não nomeável ⇒ dinâmico" e escrever os novos**

`member_access_typing_test.go`, substituir os dois testes:

```go
func TestModuleFieldTypeUnnameableStructIsTypedWithCanonicalName(t *testing.T) {
	// Issue #133: sem `use db` e sem `select Row` o programa nao tem grafia
	// para `Row`, mas o VALOR continua tipado — a identidade e a declaracao;
	// a mensagem exibe o caminho canonico `db.Row`.
	err := compileSourceAtRoot(t, dbRoot(t), `use db select QueryResult, q
let res: QueryResult = q()
let s: string = res.rows
`)
	requireErrorMentions(t, err, "expected string, got db.Row[]")
}

func TestModuleFieldTypePartiallyUnnameableIsFullyTyped(t *testing.T) {
	err := compileSourceAtRoot(t, dbRoot(t), `use db select QueryResult, q
let res: QueryResult = q()
let s: string = res.by_name
`)
	requireErrorMentions(t, err, "expected string, got map[string, db.Row]")
}

func TestUnnameableStructValueIsCheckedOnFieldAccess(t *testing.T) {
	err := compileSourceAtRoot(t, dbRoot(t), `use db select QueryResult, q
let res: QueryResult = q()
let r = res.rows[0]
let s: string = r.v
`)
	requireErrorMentions(t, err, "expected string, got int")
}
```

`namespace_member_typing_test.go`, acrescentar:

```go
func TestNamespaceStructNameSkipsAliasShadowedWhereTypeIsInferred(t *testing.T) {
	// Item (c) da issue: dentro de f, `m` e o parametro Box; a grafia do
	// tipo de w.make() e escolhida no ponto em que o tipo e PRODUZIDO, entao
	// pula o alias sombreado e exibe `w.V`.
	root := t.TempDir()
	writeModuleFile(t, root, "m.nx", rollModule)
	writeModuleFile(t, root, "w.nx", "use m select V, norm\nfunc make() -> V\n    return norm(V(0.0, 0.0))\nend\n")
	err := compileSourceAtRoot(t, root, `use m
use w
struct Box
    q: int
end
func f(m: Box) -> int
    let p = w.make()
    let s: string = p
    return 0
end
`)
	requireErrorMentions(t, err, "expected string, got w.V")
	requireErrorLacks(t, err, "got m.V")
}

func TestNamespaceStructNameIsFixedWhereInferredNotWherePrinted(t *testing.T) {
	// Caracterizacao (spec §1.5): o `let` de topo grava `m.V`; a mensagem
	// dentro de f, onde `m` esta sombreado, ainda imprime `m.V` — nao ha
	// re-traducao na impressao.
	root := t.TempDir()
	writeModuleFile(t, root, "m.nx", rollModule)
	writeModuleFile(t, root, "w.nx", "use m select V, norm\nfunc make() -> V\n    return norm(V(0.0, 0.0))\nend\n")
	err := compileSourceAtRoot(t, root, `use m
use w
struct Box
    q: int
end
let v = w.make()
func f(m: Box) -> int
    let s: string = v
    return 0
end
`)
	requireErrorMentions(t, err, "expected string, got m.V")
}

func TestQualifiedAnnotationStillResolvesWhenAliasIsShadowedByParameter(t *testing.T) {
	// Anotacao e espaco de tipos: `m.V` dentro de f(m: Box) continua o struct
	// do modulo (comportamento atual, preservado).
	requireNoError(t, compileSourceAtRoot(t, rollRoot(t), `use m
struct Box
    q: int
end
func f(m: Box) -> int
    let q: m.V = m.V(1.0, 2.0)
    return 0
end
`))
}
```

Atenção: no último teste `m.V(1.0, 2.0)` dentro de `f` é chamada sobre o parâmetro `m: Box`, que não tem membro `V` — se o compilador recusar isso, troque a linha por `let q: m.V = w_make()`-equivalente: declare `func mk() -> m.V return m.V(1.0, 2.0) end` **fora** de `f` e use `let q: m.V = mk()` dentro. O que o teste fixa é a anotação, não a chamada.

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/compiler -run 'TestModuleFieldTypeUnnameable|TestModuleFieldTypePartially|TestUnnameableStructValueIsChecked|TestNamespaceStructNameSkips|TestNamespaceStructNameIsFixed|TestQualifiedAnnotationStillResolves' -count=1`
Expected: os três primeiros e `…SkipsAliasShadowed` falham (hoje compila sem erro / exibe `m.V`); `…IsFixedWhereInferred` e `…StillResolves` passam (controle).

- [ ] **Step 3: Implementar**

`member_types.go`, `programViewType`, caso `*ast.PrimitiveType`:

```go
	case *ast.PrimitiveType:
		if isBuiltinTypeName(typed.Name) {
			return typed, true
		}
		if isGenericInstanceName(typed.Name) {
			// Instancia de template resolvida DENTRO do modulo (`main::Caixa<int>`):
			// o nome achatado nao e identidade entre unidades de compilacao e o
			// tipo estruturado nao sobrevive nele — continua dinamico (spec
			// §1.6 da #133; era assim desde a #58). Testado ANTES de Decl.
			return nil, false
		}
		definition := typed.Decl
		if definition == nil {
			definition = c.lookupStructFrom(origin, typed.Name)
		}
		if definition == nil {
			return nil, false
		}
		// Issue #133: sempre um no novo com a identidade; Name e so a grafia
		// escolhida por programStructName. Nunca devolve typed (AST do modulo).
		return &ast.PrimitiveType{Name: c.programStructName(definition), Decl: definition}, true
```

`programStructName`:

```go
// programStructName devolve a GRAFIA pela qual o programa exibe definition
// (issue #133: so exibicao — a identidade e o Decl), nesta ordem:
//
//  1. o nome simples, se o programa importou ESSA declaracao por `select`
//     (c.structs[nome] e o mesmo ponteiro) ou se e struct do proprio programa;
//  2. `alias.Nome`, para o PRIMEIRO `use m [as alias]` de namespaceOrder cujo
//     modulo exporta a declaracao e cujo alias NAO esta sombreado por local
//     ou upvalue no ponto em que o tipo e produzido (item (c) da #133);
//  3. o caminho canonico `origem.Nome` (`base.V`), como Go imprime — nao e
//     grafia que o programa consegue escrever sem `use base`; e so exibicao.
//
// A grafia e fixada quando o tipo e PRODUZIDO (inferencia, traducao), nao
// quando e impresso: um `let` de topo guarda `m.V` mesmo que uma funcao
// sombreie `m` depois.
func (c *Compiler) programStructName(definition *ast.StructStatement) string {
	if selected, ok := c.structs[definition.Name]; ok && selected == definition {
		return definition.Name
	}
	for _, alias := range c.namespaceOrder {
		module, isNamespace := c.namespaceImports[alias]
		if !isNamespace || c.isShadowedByLocal(alias) {
			continue
		}
		exported, loadable := c.discoverModuleStructs(module)
		if !loadable {
			continue
		}
		if exported[definition.Name] == definition {
			return alias + "." + definition.Name
		}
	}
	if origin := c.structOrigin(definition); origin != "" {
		return origin + "." + definition.Name
	}
	return definition.Name
}
```

Ajuste os comentários de `programViewType` e de `memberType` (member_types.go) que ainda dizem "devolve ok=false quando alguma parte do tipo nao e nomeavel": passa a ser "ok=false so para instancia generica e tipo residual; struct sempre traduz, com Decl".

- [ ] **Step 4: Rodar e ver passar**

Run: o mesmo `-run` do Step 2 → PASS. Depois `go test ./internal/compiler -count=1`: `TestModuleFieldTypedAsModuleOwnGenericInstanceStaysDynamic` **continua** passando (instância). Se `TestNamespaceMemberStaysDynamicWhenStructIsUnnameable` (namespace_member_typing_test.go:99) já falhar aqui, é esperado — ele inverte na Task 5; marque-o `t.Skip("inverte na Task 5")` **apenas nesta task** e remova o skip na Task 5.
Run: `go test ./internal/... -count=1` → PASS (mesma ressalva).

- [ ] **Step 5: Commit**

```bash
git add internal/compiler
git commit -m "feat(compiler): valor de struct não nomeável continua tipado — programStructName exibe alias visível não sombreado ou o caminho canônico base.V (issue #133)"
```

---

### Task 5: caminho `select` traduz; reexport resolve ao módulo declarante (namespace e `select`)

**Files:**
- Modify: `internal/compiler/module_exports.go:571-607` (`importBindingFrom`), `:649-682` (`importedBindingType`); função nova `declaringModule`
- Modify: `internal/compiler/namespace_member_types.go:41-59`
- Test: `internal/compiler/namespace_member_typing_test.go:99-111`, `:256-270`; `internal/vm/module_exports_test.go:630-661`; `internal/vm/namespace_ref_target_test.go`

**Interfaces:**
- Produces: `func (c *Compiler) declaringModule(module, name string) string` — o módulo que declara `name` entre `module` e o que ele reexporta (cadeia de `use X select *`/`select name`), `""` se nenhum; `func (c *Compiler) importedView(module string, declared ast.NoxyType) ast.NoxyType` — `programViewType(declared, module)`, ou `declared` cru quando não traduz (instância genérica: comportamento de hoje).
- Consumes: `reexportSource`, `moduleTopLevelBindings`, `programViewType`.

- [ ] **Step 1: Inverter os dois testes de caracterização e escrever os novos**

`namespace_member_typing_test.go`, substituir `TestNamespaceMemberStaysDynamicWhenStructIsUnnameable`:

```go
func TestNamespaceMemberIsTypedWhenStructIsUnnameable(t *testing.T) {
	// n.nx usa m pela forma de NAMESPACE: V nao entra nos exports de n, e o
	// programa que so faz `use n` nao tem grafia para o retorno de n.make().
	// Issue #133: o valor e tipado mesmo assim; a mensagem exibe `m.V`
	// (caminho canonico: modulo declarante + nome).
	root := t.TempDir()
	writeModuleFile(t, root, "m.nx", rollModule)
	writeModuleFile(t, root, "n.nx", "use m\nfunc make() -> m.V\n    return m.norm(m.V(0.0, 0.0))\nend\n")
	err := compileSourceAtRoot(t, root, "use n\nlet s: string = n.make()\n")
	requireErrorMentions(t, err, "expected string, got m.V")
	requireNoError(t, compileSourceAtRoot(t, root, "use n\nlet p = n.make()\nlet x: float = p.x\n"))
	err = compileSourceAtRoot(t, root, "use n\nlet p = n.make()\nlet x: string = p.x\n")
	requireErrorMentions(t, err, "expected string, got float")
}
```

substituir `TestNamespaceMemberReexportedByWildcardStaysDynamic`:

```go
func TestNamespaceMemberReexportedByWildcardIsTyped(t *testing.T) {
	// Issue #133: `g` chega a m so por REEXPORTACAO (`use x select *`);
	// namespaceMemberType segue reexportSource ate o modulo declarante, como
	// `use m select g` ja fazia.
	root := t.TempDir()
	writeModuleFile(t, root, "x.nx", "func g(n: int) -> int\n    return n\nend\n")
	writeModuleFile(t, root, "m.nx", "use x select *\n")
	err := compileSourceAtRoot(t, root, "use m\nlet s: string = m.g(1)\n")
	requireErrorMentions(t, err, "expected string, got int")
	err = compileSourceAtRoot(t, root, "use m\nlet v: int = m.g(\"x\")\n")
	requireErrorMentions(t, err, "argument 1 to 'm.g': expected int, got string")
	err = compileSourceAtRoot(t, root, "use m\nlet v: int = m.g(1, 2)\n")
	requireErrorMentions(t, err, "function 'm.g' expects 1 arguments, got 2")
}

func TestReexportedStructReturnIsTypedThroughNamespaceAndSelect(t *testing.T) {
	// Caso (a) da issue: mid reexporta base por `select *`; nem `use mid`
	// nem `use mid select mkv` dao ao programa uma grafia para V, e os dois
	// caminhos tipam o valor mesmo assim.
	root := t.TempDir()
	writeModuleFile(t, root, "base.nx", "struct V\n    x: int\nend\nfunc mkv() -> V\n    return V(1)\nend\nfunc usa(v: V) -> int\n    return v.x\nend\n")
	writeModuleFile(t, root, "mid.nx", "use base select *\n")
	requireNoError(t, compileSourceAtRoot(t, root, "use mid\nlet v = mid.mkv()\nlet n: int = v.x\nlet k: int = mid.usa(v)\n"))
	err := compileSourceAtRoot(t, root, "use mid\nlet v = mid.mkv()\nlet s: string = v\n")
	requireErrorMentions(t, err, "expected string, got base.V")
	err = compileSourceAtRoot(t, root, "use mid select mkv\nlet v = mkv()\nlet s: string = v\n")
	requireErrorMentions(t, err, "expected string, got base.V")
	requireNoError(t, compileSourceAtRoot(t, root, "use mid select mkv, usa\nlet v = mkv()\nlet k: int = usa(v)\n"))
}

func TestSelectedFunctionSignatureDoesNotCaptureLocalHomonym(t *testing.T) {
	// O buraco documentado em vm/module_exports_test.go (~641): a assinatura
	// importada por select carregava o nome CRU `V`, que um `struct V` local
	// capturava. Com Decl, `use m select norm` + `struct V` local recusa
	// passar o V local para norm.
	root := rollRoot(t)
	err := compileSourceAtRoot(t, root, "use m select norm\nstruct V\n    x: float\n    y: float\nend\nlet p: V = norm(V(1.0, 2.0))\n")
	requireErrorMentions(t, err, "argument 1 to 'norm': expected m.V, got V")
}

func TestSelectDisplayNameFollowsTextualOrderOfUses(t *testing.T) {
	// Caracterizacao (spec §1.5, achado 4): a traducao do select roda no
	// predeclare, em ordem textual — so `use` ANTERIORES contam para o alias.
	root := t.TempDir()
	writeModuleFile(t, root, "base.nx", "struct V\n    x: int\nend\nfunc mkv() -> V\n    return V(1)\nend\n")
	writeModuleFile(t, root, "mid.nx", "use base select *\n")
	err := compileSourceAtRoot(t, root, "use mid select mkv\nuse base as b\nlet s: string = mkv()\n")
	requireErrorMentions(t, err, "expected string, got base.V")
	err = compileSourceAtRoot(t, root, "use base as b\nuse mid select mkv\nlet s: string = mkv()\n")
	requireErrorMentions(t, err, "expected string, got b.V")
}
```

`internal/vm/module_exports_test.go`, `TestLocalStructIsNotTheModuleStructOfTheSameName`: apagar o parágrafo do comentário que começa em "O caminho inverso — `use geometry select dist2` com um `Point` local" e acrescentar ao teste, após o primeiro `runModuleProgram`:

```go
	// Issue #133: o caminho inverso tambem e recusado — a assinatura importada
	// por select carrega a DECLARACAO de geometry.Point, nao o nome cru.
	_, err = runModuleProgram(t, root, `use geometry select dist2
struct Point
    x: int
    y: int
end
let local: Point = Point(0, 0)
dist2(local, local)
`)
	if err == nil || !strings.Contains(err.Error(), "argument 1 to 'dist2': expected geometry.Point, got Point") {
		t.Fatalf("error=%v, want nominal mismatch on the select path", err)
	}
```

`internal/vm/namespace_ref_target_test.go`: `TestNamespaceRefArgumentWithUnnameableTargetIsCheckedAtRuntime` e `TestSelectRefArgumentWithUnnameableTargetIsCheckedAtRuntime` passam a esperar **erro de compilação**, porque `h.b` agora é `base.B` tipado. Renomear para `…IsCheckedAtCompileTime` e trocar `requireRefTargetError(t, err)` por:

```go
	if err == nil || !strings.Contains(err.Error(), "argument 1 to 'mid.setstr': expected ref string, got ref base.B") {
		t.Fatalf("error=%v, want compile-time ref target mismatch", err)
	}
```

(no caso `select`, `'setstr'` em vez de `'mid.setstr'`). Os testes com instância genérica (`…IntoGenericInstanceField…`) e os de "target correto ainda muta" ficam como estão; ajuste o comentário de cabeçalho do arquivo: o caso "struct que o programa não consegue nomear" saiu da classe dinâmica com a #133 e só a instância genérica ficou.

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/compiler -run 'TestNamespaceMemberIsTypedWhenStructIsUnnameable|TestNamespaceMemberReexportedByWildcardIsTyped|TestReexportedStructReturnIsTyped|TestSelectedFunctionSignatureDoesNotCapture|TestSelectDisplayNameFollowsTextualOrder' -count=1` → todos falham.
Run: `go test ./internal/vm -run 'TestLocalStructIsNotTheModuleStructOfTheSameName|TestNamespaceRefArgumentWithUnnameableTargetIsCheckedAtCompileTime|TestSelectRefArgumentWithUnnameableTargetIsCheckedAtCompileTime' -count=1` → falham.

- [ ] **Step 3: Implementar**

`module_exports.go`, acrescentar:

```go
// declaringModule devolve o modulo que DECLARA name no top level: module,
// ou o modulo que module reexporta (`use X select *` / `use X select name`),
// seguindo a cadeia com corte de ciclo. "" quando nenhum declara. E o mesmo
// passo que importBindingFrom ja dava para o select; namespace e escrita
// pelo namespace (issue #133) passam a usa-lo tambem.
func (c *Compiler) declaringModule(module, name string) string {
	visited := map[string]bool{module: true}
	for {
		bindings, ok := c.moduleTopLevelBindings(module)
		if !ok {
			return ""
		}
		if _, declared := bindings[name]; declared {
			return module
		}
		source, ok := c.reexportSource(module, name, visited)
		if !ok {
			return ""
		}
		visited[source] = true
		module = source
	}
}

// importedView traduz um tipo escrito dentro de module para a visao do
// programa (programViewType). O que nao traduz — instancia generica interna
// do modulo, spec §1.6 — volta CRU, exatamente como antes da #133.
func (c *Compiler) importedView(module string, declared ast.NoxyType) ast.NoxyType {
	if declared == nil {
		return nil
	}
	if translated, ok := c.programViewType(declared, module); ok {
		return translated
	}
	return declared
}
```

`importBindingFrom`: nos três casos concretos, envolver o tipo:

```go
		c.globals[name] = c.importedView(module, newFunctionType(declaration.Parameters, declaration.ReturnType))
	...
		c.globals[name] = c.importedView(module, newStructFunctionType(declaration, params))
	case *ast.LetStmt:
		c.globals[name] = c.importedView(module, declaration.Type)
```

`importedBindingType`: sem mudança de assinatura; continua "a declaração no módulo definidor".

`namespace_member_types.go`, corpo de `namespaceMemberType` a partir de `module, isNamespace := ...`:

```go
	module, isNamespace := c.namespaceImports[base.Value]
	if !isNamespace {
		return nil
	}
	// Issue #133: um nome que chega a `module` por reexportacao resolve no
	// modulo DECLARANTE — e o mesmo passo que o select ja dava.
	origin := c.declaringModule(module, access.Member)
	if origin == "" {
		return nil
	}
	declared, ok := c.importedBindingType(origin, access.Member)
	if !ok || declared == nil {
		return nil
	}
	translated, ok := c.programViewType(declared, origin)
	if !ok {
		return nil
	}
	return translated
```

Apagar do comentário de cabeçalho o parágrafo "Assimetria conhecida com `select`…" e o "segue documentado como follow-up".

- [ ] **Step 4: Rodar e ver passar**

Run: os dois `-run` do Step 2 → PASS. Remova o `t.Skip` deixado na Task 4, se houver.
Run: `go build ./... && go vet ./... && go test ./internal/... -count=1` → PASS. Atenção a `TestNamespaceMemberOfUnloadableModuleStaysDynamic` (módulo inexistente: `declaringModule` devolve `""` e o membro fica dinâmico — sem erro novo) e aos testes de genéricos entre módulos (`importedView` devolve cru para instância).

- [ ] **Step 5: Commit**

```bash
git add internal/compiler internal/vm/module_exports_test.go internal/vm/namespace_ref_target_test.go
git commit -m "feat(compiler): select traduz a assinatura importada com Decl; reexport resolve ao módulo declarante pelo namespace e pelo select (issue #133)"
```

---

### Task 6: `unknown type` com hint que nomeia o módulo real

**Files:**
- Modify: `internal/compiler/runtime_types.go:321-336` (`checkDeclaredTypeFrom`)
- Test: `internal/compiler/unknown_type_test.go`

**Interfaces:**
- Produces: `func (c *Compiler) importHintFor(name string) string` — `""` quando nenhuma dependência carregada declara `name`.
- Consumes: `c.moduleDiscovery.origins` (`map[*ast.StructStatement]string`), `c.moduleDiscovery.exported` (`map[string]map[string]*ast.StructStatement`).

- [ ] **Step 1: Escrever o teste que falha**

```go
func TestUnknownTypeHintNamesTheDeclaringAndReexportingModules(t *testing.T) {
	// Issue #133 (spec §1.7): so a anotacao ESCRITA exige grafia; quando
	// falta, o hint diz de onde importar — o declarante e quem reexporta.
	root := t.TempDir()
	writeModuleFile(t, root, "base.nx", "struct V\n    x: int\nend\nfunc mkv() -> V\n    return V(1)\nend\n")
	writeModuleFile(t, root, "mid.nx", "use base select *\n")
	err := compileSourceAtRoot(t, root, "use mid select mkv\nlet v: V = mkv()\n")
	requireErrorMentions(t, err, "variable 'v': unknown type 'V'", "add 'use base' or 'use mid select V' to name this type")
	err = compileSourceAtRoot(t, root, "use base select mkv\nlet v: V = mkv()\n")
	requireErrorMentions(t, err, "unknown type 'V'", "add 'use base' or 'use base select V' to name this type")
	// Sem candidato: hint generico de hoje.
	err = compileSourceAtRoot(t, root, "let v: Nada = 1\n")
	requireErrorMentions(t, err, "unknown type 'Nada'", "declare 'struct Nada' or import it with 'use m select Nada'")
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/compiler -run TestUnknownTypeHintNamesTheDeclaringAndReexportingModules -count=1` → falha (hint genérico).

- [ ] **Step 3: Implementar**

`runtime_types.go`, em `checkDeclaredTypeFrom`, no ramo `!isQualifiedTypeName(name)`:

```go
	if !isQualifiedTypeName(name) {
		if hint := c.importHintFor(name); hint != "" {
			return fmt.Errorf("[line %d] %s: unknown type '%s'\n  hint: %s", line, position, name, hint)
		}
		module := importFrom
		if module == "" {
			module = "m"
		}
		return fmt.Errorf("[line %d] %s: unknown type '%s'\n  hint: declare 'struct %s' or import it with 'use %s select %s'",
			line, position, name, name, module, name)
	}
```

e a função nova (no mesmo arquivo, com `sort` importado):

```go
// importHintFor monta o hint de `unknown type 'name'` quando alguma
// dependencia ja carregada declara um struct chamado name (issue #133,
// spec §1.7): `add 'use <origem>' or 'use <reexportador> select name' to
// name this type`. Dois passos, porque origins aponta sempre para o modulo
// DECLARANTE: (1) a origem vem de moduleDiscovery.origins; (2) os
// reexportadores sao os modulos descobertos (moduleDiscovery.exported) cujos
// exports contem a MESMA declaracao. "" sem candidato. Ordem deterministica
// (sort) porque os dois mapas nao tem ordem.
func (c *Compiler) importHintFor(name string) string {
	if c.moduleDiscovery == nil {
		return ""
	}
	var origins []string
	declarations := make(map[*ast.StructStatement]bool)
	for decl, module := range c.moduleDiscovery.origins {
		if decl.Name == name {
			origins = append(origins, module)
			declarations[decl] = true
		}
	}
	if len(origins) == 0 {
		return ""
	}
	sort.Strings(origins)
	origin := origins[0]
	var reexporters []string
	for module, exported := range c.moduleDiscovery.exported {
		if module != origin && declarations[exported[name]] {
			reexporters = append(reexporters, module)
		}
	}
	sort.Strings(reexporters)
	options := []string{fmt.Sprintf("'use %s'", origin)}
	if len(reexporters) > 0 {
		options = append(options, fmt.Sprintf("'use %s select %s'", reexporters[0], name))
	} else {
		options = append(options, fmt.Sprintf("'use %s select %s'", origin, name))
	}
	return fmt.Sprintf("add %s to name this type", strings.Join(options, " or "))
}
```

- [ ] **Step 4: Rodar e ver passar**

Run: `go test ./internal/compiler -run 'TestUnknownTypeHint|Unknown' -count=1` → PASS. Confira que os testes existentes de `unknown_type_test.go` que fixam o hint genérico (`use m select X`) continuam verdes — eles não têm dependência carregada que declare o nome; se um deles carregar um módulo que declara o nome, atualize a expectativa para o hint novo e diga isso no commit.
Run: `go test ./internal/... -count=1` → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/compiler/runtime_types.go internal/compiler/unknown_type_test.go
git commit -m "feat(compiler): unknown type aponta o módulo declarante e o reexportador no hint de import (issue #133)"
```

---

### Task 7: VM — `OP_SET_PROPERTY` e `REF_PROPERTY` sobre `ObjMap`

**Files:**
- Modify: `internal/vm/field_ops.go:55-100` (`setPropertyGeneric`)
- Modify: `internal/vm/references.go:257-284` (`descend`, passo `REF_PROPERTY`), `:368-380` (caso `REF_PROPERTY` do resolvedor — `grep -n 'case value.REF_PROPERTY' internal/vm/references.go` para achar os dois)
- Modify: `internal/vm/executor.go:441-447` (guarda R1 de `OP_REF_PROPERTY`)
- Test: `internal/vm/map_property_test.go` (novo)

**Interfaces:**
- Produces: ramo `*value.ObjMap` nos três pontos; mensagens `undefined property '%s' in module/map` (escrita) e `undefined property '%s'` (referência), `only instances and maps have properties`.
- Consumes: `mapping.Get/Set`, `value.Retain/Release`, `vm.unicize`, `referenceSetter{kind: setterMap, mapping, key}`.

Testável **sem** a mudança de compilador: um map guardado num `any` chega a `OP_SET_PROPERTY`/`OP_REF_PROPERTY` pelo caminho genérico.

- [ ] **Step 1: Escrever os testes que falham**

```go
package vm

import (
	"strings"
	"testing"
)

// Issue #133: o objeto do namespace de um modulo e um ObjMap sobre o
// bindingStore do modulo (GlobalEnvironment.ExportMap). Escrever nele pelo
// OP_SET_PROPERTY e tomar `ref` de um membro (REF_PROPERTY) precisam de um
// ramo de mapa — testados aqui pela fronteira `any`, que chega aos mesmos
// opcodes sem depender do compilador.

func TestSetPropertyOnMapThroughAnyWritesTheEntry(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{})
	reported, err := runModuleProgram(t, root, `let m: any = {"x": 1}
m.x = 2
test_report(m.x)
`)
	if err != nil {
		t.Fatal(err)
	}
	if reported.Int() != 2 {
		t.Fatalf("got %v, want 2", reported)
	}
}

func TestSetPropertyOnMapThroughAnyRejectsMissingKey(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{})
	_, err := runModuleProgram(t, root, "let m: any = {\"x\": 1}\nm.y = 2\n")
	if err == nil || !strings.Contains(err.Error(), "undefined property 'y' in module/map") {
		t.Fatalf("error=%v", err)
	}
}

func TestSetPropertyOnMapThroughAnyReleasesTheOldComposite(t *testing.T) {
	// RC: o array antigo e liberado, o novo retido — o programa continua
	// lendo o novo depois de a variavel original sair de escopo.
	root := writeModuleFiles(t, map[string]string{})
	reported, err := runModuleProgram(t, root, `let m: any = {"xs": [1, 2]}
func swap() -> void
    let fresh: int[] = [7, 8, 9]
    m.xs = fresh
end
swap()
let xs: int[] = m.xs
test_report(length(xs))
`)
	if err != nil {
		t.Fatal(err)
	}
	if reported.Int() != 3 {
		t.Fatalf("got %v, want 3", reported)
	}
}

func TestRefPropertyOnMapThroughAnyMutatesTheEntry(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{})
	reported, err := runModuleProgram(t, root, `let m: any = {"x": 1}
func bump(n: ref int) -> void
    *n = *n + 1
end
bump(ref m.x)
test_report(m.x)
`)
	if err != nil {
		t.Fatal(err)
	}
	if reported.Int() != 2 {
		t.Fatalf("got %v, want 2", reported)
	}
}

func TestRefPropertyOnMapThroughAnyIntermediateStep(t *testing.T) {
	// `ref m.p.x`: o passo intermediario (descend) atravessa o mapa.
	root := writeModuleFiles(t, map[string]string{})
	reported, err := runModuleProgram(t, root, `struct P
    x: int
end
let m: any = {"p": P(1)}
func bump(n: ref int) -> void
    *n = *n + 1
end
bump(ref m.p.x)
let p: P = m.p
test_report(p.x)
`)
	if err != nil {
		t.Fatal(err)
	}
	if reported.Int() != 2 {
		t.Fatalf("got %v, want 2", reported)
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/vm -run 'OnMapThroughAny' -count=1`
Expected: `only instances have properties` nos de escrita; `Target is not an instance` nos de `ref`.

- [ ] **Step 3: Implementar**

`field_ops.go`, `setPropertyGeneric`, substituir o trecho entre a resolução do `VAL_REF` e `slot, declared := instance.Struct.FieldIndex(name)`:

```go
	if instanceVal.Type != value.VAL_OBJ {
		return vm.runtimeError(c, ip, "only instances and maps have properties")
	}
	if mapping, isMap := instanceVal.Obj.(*value.ObjMap); isMap && mapping != nil {
		// Issue #133: membro de modulo pelo namespace (ObjMap sobre o
		// bindingStore do modulo) e map acessado como propriedade. Chave
		// inexistente e erro como na leitura; RC: retain-antes-de-release —
		// ObjMap.Set nao toca contadores. Set e mutexado e avanca a geracao
		// do store (invalida caches de leitura do modulo).
		old, exists := mapping.Get(name)
		if !exists {
			return vm.runtimeError(c, ip, "undefined property '%s' in module/map", name)
		}
		if old.Type == value.VAL_REF && val.Type != value.VAL_REF && val.Type != value.VAL_NULL {
			return vm.runtimeError(c, ip, "slot '%s' already holds a reference\n  hint: pass it directly, without 'ref'", name)
		}
		value.Retain(val)
		mapping.Set(name, val)
		value.Release(old)
		vm.push(val)
		return nil
	}
	instance, ok := instanceVal.Obj.(*value.ObjInstance)
	if !ok {
		return vm.runtimeError(c, ip, "only instances and maps have properties")
	}
```

Atualize também a mensagem do `if instanceVal.Type != value.VAL_OBJ` acima (era `only instances have properties`). Se algum teste fixar a mensagem antiga (`grep -rn "only instances have properties" internal cmd`), atualize a expectativa.

`references.go`, `descend`, no início do bloco `if step.RefType == value.REF_PROPERTY {`:

```go
	if step.RefType == value.REF_PROPERTY {
		if mapping, isMap := container.Obj.(*value.ObjMap); isMap && mapping != nil {
			// Issue #133: propriedade de mapa (membro de modulo pelo
			// namespace) como passo do caminho — mesmo protocolo do ramo de
			// mapa de REF_INDEX abaixo: para escrita, uniciza o filho e
			// regrava com retain-antes-de-release.
			child, exists := mapping.Get(step.Name)
			if !exists {
				return value.Value{}, fmt.Errorf("undefined property '%s'", step.Name)
			}
			if !forWrite {
				return child, nil
			}
			if unique, changed := vm.unicize(child); changed {
				value.Retain(unique)
				mapping.Set(step.Name, unique)
				value.Release(child)
				return unique, nil
			}
			return child, nil
		}
		instance, ok := container.Obj.(*value.ObjInstance)
```

`references.go`, caso `REF_PROPERTY` do resolvedor (após `container, err := vm.borrowContainer(ref, forWrite)`):

```go
		if mapping, isMap := container.Obj.(*value.ObjMap); container.Type == value.VAL_OBJ && isMap && mapping != nil {
			// Issue #133: passo final sobre mapa — o setter e o de mapa com
			// chave string; o RC da escrita fica no funil de
			// storeReferenceValue, que ja atende setterMap.
			stored, exists := mapping.Get(ref.Name)
			if !exists {
				return value.Value{}, false, referenceSetter{}, fmt.Errorf("undefined property '%s'", ref.Name)
			}
			return stored, true, referenceSetter{kind: setterMap, mapping: mapping, key: ref.Name}, nil
		}
		instance, ok := container.Obj.(*value.ObjInstance)
```

`executor.go`, `OP_REF_PROPERTY`, logo após a guarda R1 de instância:

```go
			if mapping, ok := container.Obj.(*value.ObjMap); ok && mapping != nil {
				if stored, exists := mapping.Get(name); exists && stored.Type == value.VAL_REF {
					return vm.runtimeError(c, ip, "slot '%s' already holds a reference\n  hint: pass it directly, without 'ref'", name)
				}
			}
```

- [ ] **Step 4: Rodar e ver passar**

Run: `go test ./internal/vm -run 'OnMapThroughAny' -count=1` → PASS.
Run: `go test ./internal/vm -count=1 -race` → PASS (o CI roda `-race` neste pacote). `go test ./internal/... -count=1` → PASS. Rode também `go test ./internal/vm -run 'Cow|ContainerOwners' -count=1` explicitamente (AGENTS.md: mexeu em quem guarda composto).

- [ ] **Step 5: Commit**

```bash
git add internal/vm/field_ops.go internal/vm/references.go internal/vm/executor.go internal/vm/map_property_test.go
git commit -m "feat(vm): OP_SET_PROPERTY e REF_PROPERTY sobre ObjMap — escrita e referência a membro de módulo pelo namespace, com RC (issue #133)"
```

---

### Task 8: compilador — `m.x = v` tipado; raiz de lvalue pelo namespace tipada

**Files:**
- Create: `internal/compiler/namespace_write.go`
- Modify: `internal/compiler/compiler.go:875-900` (ramo de atribuição a membro: a guarda "read-only" sai)
- Modify: `internal/compiler/cow_lowering.go:83-100` (`compileLValueBase`, caso `MemberAccessExpression`)
- Modify: `internal/compiler/borrow_place.go:64-75` (`compileBorrowBase`), `:100-110` (`lvalueStaticType`)
- Test: `internal/compiler/namespace_write_test.go` (novo), `internal/vm/namespace_write_test.go` (novo), `internal/vm/module_exports_test.go:663-673`

**Interfaces:**
- Produces: `func (c *Compiler) pureNamespaceAlias(name string) (module string, ok bool)`; `func (c *Compiler) compileNamespaceMemberAssignment(n *ast.AssignStmt, alias, module string, target *ast.MemberAccessExpression) (*chunk.Chunk, ast.NoxyType, error)`.
- Consumes: `declaringModule`, `moduleTopLevelBindings`, `namespaceMemberType`, `areTypesCompatible`, `emitSlotGuards`, `referenceAssignmentTypeError(line, name, declared, valType)`, `derefReadHint`, `nullMismatchHint`, `rewriteIfGenericValue`.

- [ ] **Step 1: Escrever os testes que falham**

`internal/compiler/namespace_write_test.go`:

```go
package compiler

import "testing"

// Issue #133 item 1: escrita pelo namespace e legal, tipada com o tipo
// declarado pelo membro, e cai no store vivo do modulo. A regra
// "module variables are read-only outside the module" (0.11.0) sai.

const stateModule = `struct P
    x: int
end
let origin: P = P(0)
let count: int = 0
let xs: int[] = [1, 2]
let name = "a"
let link: ref int = ref count
func read_count() -> int
    return count
end
`

func stateRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeModuleFile(t, root, "st.nx", stateModule)
	return root
}

func TestNamespaceWriteCompilesWithDeclaredType(t *testing.T) {
	requireNoError(t, compileSourceAtRoot(t, stateRoot(t), "use st\nst.count = 9\nst.name = \"b\"\nst.origin = st.P(3)\nst.xs = [4]\n"))
}

func TestNamespaceWriteRejectsTypeMismatch(t *testing.T) {
	err := compileSourceAtRoot(t, stateRoot(t), "use st\nst.count = \"a\"\n")
	requireErrorMentions(t, err, "type mismatch in assignment to 'st.count': expected int, got string")
	err = compileSourceAtRoot(t, stateRoot(t), "use st\nst.origin = 1\n")
	requireErrorMentions(t, err, "expected st.P, got int")
}

func TestNamespaceWriteRejectsMissingMemberFunctionAndStruct(t *testing.T) {
	err := compileSourceAtRoot(t, stateRoot(t), "use st\nst.nope = 1\n")
	requireErrorMentions(t, err, "'st' has no member 'nope'")
	err = compileSourceAtRoot(t, stateRoot(t), "use st\nst.read_count = 1\n")
	requireErrorMentions(t, err, "cannot assign to 'st.read_count': it is a function", "only module variables ('let') can be assigned")
	err = compileSourceAtRoot(t, stateRoot(t), "use st\nst.P = 1\n")
	requireErrorMentions(t, err, "cannot assign to 'st.P': it is a struct")
	requireErrorLacks(t, err, "read-only outside the module")
}

func TestNamespaceWriteToRefMemberOnlyRebinds(t *testing.T) {
	requireNoError(t, compileSourceAtRoot(t, stateRoot(t), "use st\nlet other: int = 1\nst.link = ref other\n"))
	err := compileSourceAtRoot(t, stateRoot(t), "use st\nst.link = 5\n")
	requireErrorMentions(t, err, "st.link")
	requireErrorLacks(t, err, "has no member")
}

func TestNamespaceNestedWritesAreTyped(t *testing.T) {
	root := stateRoot(t)
	requireNoError(t, compileSourceAtRoot(t, root, "use st\nst.origin.x = 5\nst.xs[0] = 7\n"))
	err := compileSourceAtRoot(t, root, "use st\nst.origin.x = \"a\"\n")
	requireErrorMentions(t, err, "type mismatch in field assignment: expected int, got string")
	err = compileSourceAtRoot(t, root, "use st\nst.xs[0] = \"a\"\n")
	requireErrorMentions(t, err, "type mismatch in array assignment: expected int, got string")
}

func TestNamespaceRefAndBuiltinsAreTyped(t *testing.T) {
	root := stateRoot(t)
	requireNoError(t, compileSourceAtRoot(t, root, "use st\nlet r = ref st.count\n*r = 3\nappend(ref st.xs, 3)\nlet last: int = pop(ref st.xs)\n"))
	err := compileSourceAtRoot(t, root, "use st\nlet r: ref string = ref st.count\n")
	requireErrorMentions(t, err, "expected ref string, got ref int")
	err = compileSourceAtRoot(t, root, "use st\nappend(ref st.xs, \"a\")\n")
	requireErrorMentions(t, err, "expected int, got string")
}

func TestNamespaceWriteWithShadowedAliasIsAFieldWrite(t *testing.T) {
	// `st` local sombreia o alias: a atribuicao e num campo do struct local.
	err := compileSourceAtRoot(t, stateRoot(t), `use st
struct Box
    count: string
end
func f() -> int
    let st: Box = Box("x")
    st.count = 1
    return 0
end
`)
	requireErrorMentions(t, err, "type mismatch in field assignment: expected string, got int")
}

func TestNamespaceWriteToUnloadableModuleIsDynamic(t *testing.T) {
	// Modulo inexistente nao e erro de compilacao (cf.
	// TestNamespaceMemberOfUnloadableModuleStaysDynamic); a escrita fica
	// dinamica, sem erro novo.
	requireNoError(t, compileSourceAtRoot(t, stateRoot(t), "use nope\nnope.x = 1\n"))
}
```

`internal/vm/namespace_write_test.go`:

```go
package vm

import (
	"strings"
	"testing"
)

// Issue #133 item 1: a escrita pelo namespace cai na variavel VIVA do modulo
// (ExportMap compartilha o bindingStore), como a leitura ja era.

const liveStateModule = `struct P
    x: int
end
let origin: P = P(0)
let count: int = 0
let xs: int[] = [1, 2]
func read_count() -> int
    return count
end
func read_origin_x() -> int
    return origin.x
end
func read_xs_len() -> int
    return length(xs)
end
`

func TestNamespaceDirectWriteIsSeenInsideTheModule(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{"st.nx": liveStateModule})
	reported, err := runModuleProgram(t, root, "use st\nst.count = 9\ntest_report(st.read_count())\n")
	if err != nil || reported.Int() != 9 {
		t.Fatalf("reported=%v err=%v", reported, err)
	}
}

func TestNamespaceNestedAndIndexedWritesAreSeenInsideTheModule(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{"st.nx": liveStateModule})
	reported, err := runModuleProgram(t, root, "use st\nst.origin.x = 99\nst.xs[0] = 10\ntest_report(st.read_origin_x() * 100 + st.xs[0])\n")
	if err != nil || reported.Int() != 9910 {
		t.Fatalf("reported=%v err=%v", reported, err)
	}
}

func TestNamespaceRefMemberMutatesTheModule(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{"st.nx": liveStateModule})
	reported, err := runModuleProgram(t, root, `use st
func bump(c: ref int) -> void
    *c = *c + 1
end
bump(ref st.count)
bump(ref st.count)
append(ref st.xs, 3)
test_report(st.read_count() * 10 + st.read_xs_len())
`)
	if err != nil || reported.Int() != 23 {
		t.Fatalf("reported=%v err=%v", reported, err)
	}
}

func TestSelectRemainsASnapshotAfterNamespaceWrite(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{"st.nx": liveStateModule})
	reported, err := runModuleProgram(t, root, "use st\nuse st select count\nst.count = 9\ntest_report(count * 100 + st.count)\n")
	if err != nil || reported.Int() != 9 {
		t.Fatalf("select must stay a snapshot: reported=%v err=%v", reported, err)
	}
}

func TestNamespaceWriteReplacesCompositeWithoutLeakOrDoubleFree(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{"st.nx": liveStateModule})
	reported, err := runModuleProgram(t, root, `use st
func replace() -> void
    let fresh: int[] = [7, 8, 9]
    st.xs = fresh
end
replace()
replace()
let copy: int[] = st.xs
append(ref st.xs, 1)
test_report(length(copy) * 10 + st.read_xs_len())
`)
	if err != nil || reported.Int() != 34 {
		t.Fatalf("reported=%v err=%v", reported, err)
	}
}

func TestNamespaceWriteRejectsWrongTypeAtCompileTime(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{"st.nx": liveStateModule})
	_, err := runModuleProgram(t, root, "use st\nst.count = \"a\"\n")
	if err == nil || !strings.Contains(err.Error(), "type mismatch in assignment to 'st.count': expected int, got string") {
		t.Fatalf("error=%v", err)
	}
}

func TestConcurrentNamespaceWritesDoNotCorruptTheRuntime(t *testing.T) {
	// docs/concurrency.md: operacoes individuais em global sao sincronizadas
	// (o bindingStore e mutexado); uma sequencia leitura-escrita nao e
	// atomica, entao o teste so exige que o runtime Go nao quebre (roda sob
	// -race no CI) e que a ultima escrita seja um dos valores escritos.
	root := writeModuleFiles(t, map[string]string{"st.nx": liveStateModule})
	reported, err := runModuleProgram(t, root, `use st
func worker(id: int, c: any) -> void
    let i: int = 0
    while i < 200 do
        st.count = id
        i = i + 1
    end
    chan_send(c, id)
end
let c: any = make_chan(4)
spawn(worker, 1, c)
spawn(worker, 2, c)
spawn(worker, 3, c)
spawn(worker, 4, c)
let done: int = 0
while done < 4 do
    let got: any = chan_recv(c)
    done = done + 1
end
test_report(st.read_count())
`)
	if err != nil {
		t.Fatal(err)
	}
	if got := reported.Int(); got < 1 || got > 4 {
		t.Fatalf("count must be one of the written ids, got %d", got)
	}
}
```

`internal/vm/module_exports_test.go`, substituir `TestModuleVariableAssignmentViaNamespaceIsCompileError` por:

```go
func TestModuleVariableAssignmentViaNamespaceIsLiveAndTyped(t *testing.T) {
	// Issue #133: a regra "read-only outside the module" (0.11.0) saiu; a
	// escrita e tipada e vista pelo modulo.
	root := writeModuleFiles(t, map[string]string{"calc.nx": "let sp: int = 0\nfunc push() -> void\n    sp = sp + 1\nend\nfunc read() -> int\n    return sp\nend\n"})
	reported, err := runModuleProgram(t, root, "use calc\ncalc.push()\ncalc.sp = 5\ncalc.push()\ntest_report(calc.read() * 10 + calc.sp)\n")
	if err != nil || reported.Int() != 66 {
		t.Fatalf("live typed write via namespace: %v / %v", reported, err)
	}
	_, err = runModuleProgram(t, root, "use calc\ncalc.sp = \"x\"\n")
	if err == nil || !strings.Contains(err.Error(), "type mismatch in assignment to 'calc.sp': expected int, got string") {
		t.Fatalf("error=%v", err)
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/compiler -run 'TestNamespaceWrite|TestNamespaceNestedWrites|TestNamespaceRefAndBuiltins' -count=1` → falham com `read-only outside the module` / `cannot infer type` / `append expects an array, got unknown`.
Run: `go test ./internal/vm -run 'TestNamespace.*Write|TestSelectRemainsASnapshot|TestNamespaceRefMemberMutates|TestModuleVariableAssignmentViaNamespaceIsLiveAndTyped' -count=1` → falham.

- [ ] **Step 3: Implementar**

`internal/compiler/namespace_write.go`:

```go
package compiler

import (
	"fmt"

	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

// Issue #133 item 1: `m.x = v` pelo namespace. Precedente: Python, Go
// (variavel exportada de pacote), Nim, Swift (`public var`) permitem escrever
// num global de outro modulo. A regra "module variables are read-only
// outside the module" (0.11.0, #56 §8b) era remendo para uma escrita que
// gravava num binding que ninguem lia; desde o #126 o membro tem tipo
// estatico e o objeto do namespace compartilha o bindingStore do modulo
// (GlobalEnvironment.ExportMap), entao a escrita cai na variavel viva —
// a leitura ja era "live" pela spec §11; a escrita passa a ser tambem.
// `select` continua snapshot.

// pureNamespaceAlias reporta se name e um alias de `use m [as name]` nao
// sombreado por local/upvalue nem por um global tipado homonimo — o global
// tem de ser o marcador que importNamespace deixou (presente, tipo nil).
func (c *Compiler) pureNamespaceAlias(name string) (string, bool) {
	globalType, isGlobal := c.globals[name]
	if !isGlobal || globalType != nil || c.isShadowedByLocal(name) {
		return "", false
	}
	module, isNamespace := c.namespaceImports[name]
	return module, isNamespace
}

// compileNamespaceMemberAssignment compila `alias.member = valor`:
//
//   - membro inexistente: `'m' has no member 'y'`; funcao/struct: `cannot
//     assign to 'm.f': it is a function`;
//   - `let` do modulo: o MESMO protocolo da atribuicao a global (compiler.go,
//     ramo Identifier): membro `ref T` so aceita rebind por ref/null;
//     membro comum exige areTypesCompatible; emitSlotGuards; e a escrita e
//     OP_SET_PROPERTY no objeto do namespace (ramo ObjMap do VM);
//   - modulo nao carregavel ou tipo nao traduzivel: escrita dinamica, sem
//     checagem estatica e sem erro novo (como um global `any`).
//
// Pilha: OP_GET_GLOBAL alias (leitura simples — a escrita e no store
// compartilhado, nao numa copia), valor, OP_SET_PROPERTY ([base, val] ->
// [val]), OP_POP.
func (c *Compiler) compileNamespaceMemberAssignment(n *ast.AssignStmt, alias, module string, target *ast.MemberAccessExpression) (*chunk.Chunk, ast.NoxyType, error) {
	member := target.Member
	targetName := alias + "." + member
	var memberType ast.NoxyType
	if origin := c.declaringModule(module, member); origin != "" {
		bindings, _ := c.moduleTopLevelBindings(origin)
		switch bindings[member].(type) {
		case *ast.FunctionStatement:
			return nil, nil, fmt.Errorf("[line %d] cannot assign to '%s': it is a function\n  hint: only module variables ('let') can be assigned", c.currentLine, targetName)
		case *ast.StructStatement:
			return nil, nil, fmt.Errorf("[line %d] cannot assign to '%s': it is a struct\n  hint: only module variables ('let') can be assigned", c.currentLine, targetName)
		case *ast.LetStmt:
			memberType = c.namespaceMemberType(target)
		}
	} else if _, loadable := c.moduleTopLevelBindings(module); loadable {
		return nil, nil, fmt.Errorf("[line %d] '%s' has no member '%s'", c.currentLine, alias, member)
	}

	c.emitOpWithConstantIndex(chunk.OP_GET_GLOBAL, c.makeConstant(value.NewString(alias)))

	// §3 target-typing, posicao 4: o tipo declarado do membro e o alvo de um
	// template de funcao nu no valor.
	if err := c.rewriteIfGenericValue(n.Value, memberType); err != nil {
		return nil, nil, err
	}
	_, valType, err := c.Compile(n.Value)
	if err != nil {
		return nil, nil, err
	}

	if memberType != nil {
		if refType, isRef := asRefType(memberType); isRef {
			_, isRefVal := asRefType(valType)
			if !(isRefVal || valType == nil || isNullType(valType)) {
				if c.areTypesCompatible(refType.ElementType, valType) {
					return nil, nil, referenceAssignmentTypeError(c.currentLine, targetName, memberType, valType)
				}
				return nil, nil, fmt.Errorf("[line %d] type mismatch in assignment to '%s': expected %s, got %s", c.currentLine, targetName, memberType.String(), valType.String())
			}
		}
		if !c.areTypesCompatible(memberType, valType) {
			return nil, nil, fmt.Errorf("[line %d] type mismatch in assignment to '%s': expected %s, got %s%s%s", c.currentLine, targetName, memberType.String(), valType.String(), c.derefReadHint(memberType, valType, n.Value), c.nullMismatchHint(memberType, valType, n.Value))
		}
		if err := c.emitSlotGuards(memberType, valType); err != nil {
			return nil, nil, err
		}
	}

	c.emitOpWithConstantIndex(chunk.OP_SET_PROPERTY, c.makeConstant(value.NewString(member)))
	c.emitByte(byte(chunk.OP_POP))
	return c.currentChunk, nil, nil
}
```

`compiler.go`, ramo `memberExp` da atribuição: **substituir** todo o bloco que começa em `// Variavel de modulo via namespace (`calc.sp = 5`): leitura e viva,` até o `}` que fecha `if leftIdent, isIdent := ...` por:

```go
			// Issue #133: `m.x = v` pelo namespace e escrita tipada no store
			// vivo do modulo (namespace_write.go). A regra "read-only outside
			// the module" (0.11.0) saiu.
			if leftIdent, isIdent := memberExp.Left.(*ast.Identifier); isIdent {
				if module, isNamespace := c.pureNamespaceAlias(leftIdent.Value); isNamespace {
					return c.compileNamespaceMemberAssignment(n, leftIdent.Value, module, memberExp)
				}
			}
```

`cow_lowering.go`, `compileLValueBase`, caso `*ast.MemberAccessExpression`, após `t := c.memberType(leftType, n.Member)`:

```go
		if t == nil && leftType == nil {
			// Issue #133: raiz `m.a` de um lvalue pelo namespace — o tipo do
			// membro traduzido, para que `m.a.b = v` e `m.xs[i] = v` entrem
			// no funil tipado. namespaceMemberType ja exige alias nao
			// sombreado; leftType nil garante que o global e o marcador de
			// namespace (um global tipado homonimo teria tipo).
			t = c.namespaceMemberType(n)
		}
```

`borrow_place.go`, `compileBorrowBase`, caso `*ast.MemberAccessExpression`, trocar o `return`:

```go
		fieldType := c.memberType(owner, n.Member)
		if fieldType == nil && owner == nil {
			fieldType = c.namespaceMemberType(n) // issue #133: `ref m.x`
		}
		return unwrapRefType(fieldType), nil
```

`borrow_place.go`, `lvalueStaticType`, caso `*ast.MemberAccessExpression`:

```go
	case *ast.MemberAccessExpression:
		owner, ok := c.lvalueStaticType(n.Left)
		if !ok {
			// Issue #133: `m.x` com m alias de namespace (sem tipo proprio).
			t := c.namespaceMemberType(n)
			return t, t != nil
		}
```

Se, depois disso, `TestNamespaceRefAndBuiltinsAreTyped` ainda acusar `append expects an array, got unknown`: o tipo do argumento `ref` de um builtin é calculado em `builtin_calls.go` — `grep -n 'expects an array, got' internal/compiler/builtin_calls.go` e siga de onde vem o tipo do alvo; ele tem de terminar em `compileBorrowBase` ou `lvalueStaticType` (os dois acima). Se houver um terceiro caminho (por exemplo um `switch` próprio sobre `*ast.MemberAccessExpression` com `memberType`), acrescente o mesmo fallback `namespaceMemberType` lá e cite o site no commit.

- [ ] **Step 4: Rodar e ver passar**

Run: os dois `-run` do Step 2 → PASS.
Run: `go build ./... && go vet ./... && go test ./internal/... -count=1 && go test ./cmd/... -count=1` → PASS.
Run: `grep -rn "read-only outside the module" internal cmd docs --include=*.go` → nenhum resultado em código Go (a spec e o CHANGELOG antigo ficam; a Task 9 trata a spec).
Run: `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx` → todos verdes.

- [ ] **Step 5: Commit**

```bash
git add internal/compiler internal/vm
git commit -m "feat(compiler): m.x = v pelo namespace é escrita tipada e viva; raiz de lvalue e de ref pelo namespace tipada; some a regra read-only (issue #133)"
```

---

### Task 9: documentação — spec §3 e §11, CHANGELOG, exemplo

**Files:**
- Modify: `docs/NOXY_LANGUAGE_SPEC.md:731-736` (§3), `:2440-2452`, `:2489-2503`, `:2512-2531`, `:2545-2547` (§11)
- Modify: `CHANGELOG.md:1-3`
- Create: `noxy_examples/namespace_state_module.nx`, `noxy_examples/test_namespace_state.nx`

- [ ] **Step 1: Spec §3** — substituir o parágrafo das linhas 731-736 por:

```markdown
A member reached through a namespace import — `m.f(...)`, `m.x`, `m.T(...)`
after `use m` — has the type the module declared for it, translated to the
program's view exactly as a field of a module struct is (§11), so `let v =
m.roll(6)` binds `v: int` and `let p = vec.norm(v)` binds `p: vec.V`. The
value is fully typed even when the program has no way to *write* its type
(a struct of a module it never imported): `let v = mid.mkv()` binds `v` to
the declaration of `V` in `base.nx`, `v.x` is checked, and a mismatch prints
the canonical path (`expected string, got base.V`). Only a written
annotation needs a name (§11, *Unknown type names*).
```

- [ ] **Step 2: Spec §11 — escrita pelo namespace.** Substituir a seção `### Module state is read-only from outside` (linhas 2440-2452) por:

```markdown
### Module state is writable through the namespace

Assigning to a module variable through the namespace is legal and typed
with the member's declared type, and the write lands in the module's live
state — reads through the namespace were already live; writes are too.
`select` still binds a snapshot.

```noxy
use counter
counter.total = 9          // OK: total is `let total: int` in counter.nx
print(counter.read())      // 9 — the module sees the new value
counter.total = "a"        // ERROR: type mismatch in assignment to
                           // 'counter.total': expected int, got string
counter.nope = 1           // ERROR: 'counter' has no member 'nope'
counter.read = 1           // ERROR: cannot assign to 'counter.read': it is a function
```

Nested writes (`m.origin.x = 99`, `m.xs[i] = v`), `ref m.x`, `append(ref
m.xs, v)` and `pop(ref m.xs)` follow the same rule with the member's type.
A `ref T` member accepts only a rebind (`m.link = ref other`), like a `ref`
global. A global `let` that shadows the namespace name is a different
binding and is unaffected. As with any shared global, concurrent writers
must coordinate (docs/concurrency.md): a single write is synchronized, a
read-modify-write is not.
```

- [ ] **Step 3: Spec §11 — tabela de tradução e regra.** Na tabela das linhas 2489-2493, substituir a linha `| neither (the program cannot name `Row`) | dynamic (unknown), as before |` por:

```markdown
| neither (the program cannot write `Row`) | `sqlite.Row[]` — the canonical path (module + name); the value is fully typed, the name is display only |
```

No parágrafo seguinte (2495-2503), apagar a frase `A type that is only *partially* nameable (`map[string, Row]` with no way to write `Row`) becomes dynamic as a whole — never a half-typed `map[string, ???]`.` e inserir, antes de `A field whose type is an instance of a generic struct`:

```markdown
**A value's type is its declaration; a name is needed only to write an
annotation.** Two names that designate the same declaration are the same
type (`Row`, `sqlite.Row`, `db.Row`, and the canonical `sqlite.Row` printed
for a program that imported none of them); a local `struct Row` is a
different type from any of them. Messages print the alias the program can
see at the point where the type was inferred — skipping an alias shadowed
by a local or parameter there, and counting only `use` lines that precede
that point — and fall back to the canonical path.
```

- [ ] **Step 4: Spec §11 — namespace.** Em `### Member access through a namespace` (2512-2531): trocar `A member whose type the program cannot name (a struct of a module it never imported, an instance of a module's generic struct) stays dynamic.` por `A member whose type is an instance of a module's own generic struct stays dynamic (§6.4); a struct the program merely cannot name is typed as above.` e **apagar** o parágrafo final `A member that only reaches `m` by re-export … still resolves `g` to its declared type.`, substituindo por:

```markdown
A member that only reaches `m` by re-export (`m` does `use x select *` and
never declares it) resolves to its declaration in `x`, through `m.g` and
through `use m select g` alike.
```

- [ ] **Step 5: Spec §11 — unknown type names.** Após o bloco de código do erro (~2545-2547), acrescentar:

```markdown
When a loaded dependency declares that name, the hint says where to get it:
`add 'use base' or 'use mid select V' to name this type`.
```

- [ ] **Step 6: CHANGELOG.** Inserir após `# Changelog` (linha 1):

```markdown
## [Unreleased]

Issue #133 — namespace tipado, parte 2: escrita pelo namespace e "tipo é a
declaração, não a grafia".

### Added
- **`m.x = v` pelo namespace é escrita tipada e viva** (§11): atribuir a um
  `let` de módulo pela forma de namespace compila com o tipo declarado
  (`type mismatch in assignment to 'm.count': expected int, got string`) e
  escreve no estado vivo do módulo — uma função do módulo lê o valor novo.
  Erro só para membro inexistente (`'m' has no member 'y'`) e para
  função/struct (`cannot assign to 'm.f': it is a function`). `m.a.b = v`,
  `m.xs[i] = v`, `ref m.x`, `append(ref m.xs, v)` e `pop(ref m.xs)` passam a
  ter o tipo do lvalue conhecido (`ref m.x` morria em runtime com "Target is
  not an instance"). `select` continua snapshot. Sai a regra "module
  variables are read-only outside the module" da 0.11.0 (precedente:
  Python, Go, Nim, Swift permitem escrever num global de outro módulo).
- **Hint de `unknown type` nomeia o módulo real** (§11): `add 'use base' or
  'use mid select V' to name this type` quando alguma dependência carregada
  declara o struct.

### Changed (BREAKING)
- **Tipo é a declaração, não a grafia** (§3, §11). O compilador passa a
  identificar um struct pela declaração (`Decl`), não pelo nome; um valor
  cujo tipo o programa não sabe escrever continua totalmente tipado e é
  exibido pelo caminho canônico (`base.V`), como Go e Rust fazem. Só a
  anotação escrita exige grafia. Cobre o reexport pelo namespace
  (`use mid` com `mid.nx` = `use base select *`: `let v = mid.mkv()` infere,
  `mid.mkv("x")` é erro de aridade/tipo) e pelo `select` (`use mid select
  mkv` deixa de dar `unknown type 'V'`), o campo de struct de módulo com
  tipo de terceiro módulo (`res.rows` é `db.Row[]`, não dinâmico) e a
  assinatura importada por `select` (um `struct Point` local já não captura
  o `Point` de `geometry` numa função importada). Mensagens usam o alias
  visível não sombreado no ponto em que o tipo foi inferido.

  | Antes | Agora |
  |---|---|
  | `let s: string = res.rows` compila e falha em runtime (`Row` não importado) | `expected string, got db.Row[]` em compilação |
  | `let v = mid.mkv()` → `cannot infer type for 'v'` | `v: base.V` inferido; `v.x` checado |
  | `use mid select mkv; let v = mkv()` → `unknown type 'V'` | compila; `let s: string = v` é erro |
  | `use geometry select dist2` + `struct Point` local: `dist2(local, local)` compila | `expected geometry.Point, got Point` |

  Migração: programa que escrevia tipo errado sobre um valor não grafável
  passa a não compilar — corrija o tipo; se o valor era `any` de fato,
  anote `any`. Instância genérica interna de um módulo (`c: Caixa<int>` em
  `g.nx`) continua dinâmica (§6.4). Identidade nominal em **runtime**
  continua sendo o nome cru mais o layout (dois módulos com `struct V` de
  mesmo layout são indistinguíveis no VM; a mensagem `expected V, got V`
  é defeito de exibição) — bug pré-existente, follow-up registrado na #133.

```

- [ ] **Step 7: Exemplo.** `noxy_examples/namespace_state_module.nx`:

```noxy
// Módulo de estado para test_namespace_state.nx (issue #133).
struct P
    x: int
end
let origin: P = P(0)
let count: int = 0
let xs: int[] = [1, 2]
func read_count() -> int
    return count
end
func read_origin_x() -> int
    return origin.x
end
```

`noxy_examples/test_namespace_state.nx`:

```noxy
// Issue #133: escrita tipada pelo namespace cai no estado vivo do módulo.
use namespace_state_module as st

func assert(cond: bool, msg: string) -> void
    if !cond then
        print("namespace state: FAIL - " + msg)
        exit(1)
    end
end

st.count = 9
assert(st.read_count() == 9, "direct write is live")
st.origin.x = 99
assert(st.read_origin_x() == 99, "nested write is live")
st.xs[0] = 10
append(ref st.xs, 3)
assert(length(st.xs) == 3 && st.xs[0] == 10, "index write and append through ref")

func bump(c: ref int) -> void
    *c = *c + 1
end
bump(ref st.count)
assert(st.read_count() == 10, "ref to module member mutates the module")
print("namespace state: ok")
```

- [ ] **Step 8: Verificar**

Run: `go run ./cmd/noxy noxy_examples/test_namespace_state.nx` → `namespace state: ok`.
Run: `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx` → verde (o módulo novo roda sozinho como exemplo: só declara).
Run: `grep -n "read-only" docs/NOXY_LANGUAGE_SPEC.md` → nenhuma ocorrência sobre módulo.

- [ ] **Step 9: Commit**

```bash
git add docs/NOXY_LANGUAGE_SPEC.md CHANGELOG.md noxy_examples/namespace_state_module.nx noxy_examples/test_namespace_state.nx
git commit -m "docs(spec,changelog): escrita pelo namespace, tipo é a declaração, hint de import; exemplo test_namespace_state (issue #133)"
```

---

### Task 10: verificação final — suíte, corpus como oráculo, greps de suspeitos, revisão adversarial

**Files:** nenhum novo (correções que a revisão achar entram com teste próprio, no molde das tasks anteriores).

- [ ] **Step 1: Suíte completa e formatação**

```bash
go build ./... && go vet ./...
go test ./internal/... -count=1
go test ./internal/vm -count=1 -race
go test ./cmd/... -count=1
go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx
gofmt -l internal cmd
git diff --numstat develop...HEAD
```

Expected: tudo verde; `gofmt -l` vazio nos arquivos tocados; nenhum arquivo com contagem de linhas igual ao tamanho inteiro (reescrita por EOL).

- [ ] **Step 2: Corpus fora do runner como oráculo da quebra**

```bash
BIN=$(mktemp -d)/noxy && go build -o "$BIN" ./cmd/noxy
for f in $(find tests noxy_libs internal/stdlib -name '*.nx'); do
  out=$("$BIN" --check "$f" 2>&1) || echo "== $f"; echo "$out" | grep -E "Compiler error|unknown type|expected .*, got|has no member|cannot assign" 
done
```

Se `--check` não existir na CLI (`go run ./cmd/noxy --help`), use `git worktree add /tmp/noxy-base develop` e compare a saída de compilação dos mesmos arquivos entre os dois binários (`diff`), como `benchmarks/compare_examples.ps1` faz para o runtime. Todo diagnóstico **novo** tem de ser um dos desta spec (tipo errado sobre valor antes não grafável; `expected X, got base.V`); qualquer outro é regressão — corrija com teste antes de seguir. **Estenda o filtro do grep** com cada mensagem nova desta issue (memória do projeto: gate que filtra por substring só mede o que já conhece).

- [ ] **Step 3: Greps de suspeitos (spec §5)**

```bash
grep -n 'PrimitiveType{Name:' internal/compiler/*.go | grep -v _test
grep -n 'Name: [a-zA-Z]*\.\(Name\|String()\)' internal/compiler/*.go | grep -v _test
grep -n 'String() == ' internal/compiler/*.go | grep -v _test
grep -n 'visiting\[' internal/compiler/*.go | grep -v _test
```

Para cada site da primeira lista que constrói um tipo **de struct** a partir de outro tipo: tem de carregar `Decl` (`Decl: other.Decl`) — os que constroem primitivos (`"int"`, `"bool"`, `"any"`, `"null"`, `"void"`) ficam. A segunda lista é a classe do "nó órfão" (`TestCanonicalNameWithoutDeclDoesNotResolve` documenta por quê). A terceira só pode ter comparações com literais. Registre no commit final a lista revisada.

- [ ] **Step 4: Revisão adversarial independente (obrigatória — memória do projeto)**

Despache um agente **sem o contexto desta spec e deste plano**, com o repositório na branch, e o pedido:

> Encontre um programa Noxy (com módulos, se precisar) em que (a) um valor de struct chegue a uma comparação de tipo ou a um guard de runtime com `Decl` nulo e produza tipo errado ou "unknown type" indevido; (b) uma escrita pelo namespace (`m.x = v`, `m.a.b = v`, `ref m.x`, `append(ref m.xs, v)`) escape do tipo declarado, vaze RC (composto liberado a mais/a menos) ou não seja vista pelo módulo; (c) uma mensagem exiba um alias sombreado no ponto da inferência, ou `expected X, got X` para declarações distintas. Varie o que os testes existentes mantêm constante: profundidade do caminho, alias duplo (`use m as a` + `use m as b`), REPL (linhas separadas), struct declarado dentro de função, instância genérica de módulo, módulo de diretório, `select *` encadeado em três níveis, `spawn` escrevendo pelo namespace. Reporte cada caso como programa mínimo + saída observada + saída esperada. Achar caso conta como sucesso.

Cada caso reproduzido vira teste (vermelho) e correção; caso sem correção nesta rodada vira **teste de caracterização** (expectativa = comportamento de hoje, com comentário `// caracterizacao: ...`) e nota no CHANGELOG.

- [ ] **Step 5: Comentário na issue e commit final**

`gh issue comment 133 --body-file -` com: o que entrou (item 1 e 2), a quebra e migração (link para o CHANGELOG), o que ficou fora com motivo (instância genérica de módulo; identidade nominal em runtime — bug pré-existente `expected V, got V`), e os achados da revisão adversarial.

```bash
git add -A internal cmd docs CHANGELOG.md noxy_examples
git commit -m "test(compiler,vm): casos da revisão adversarial da #133 e varredura do corpus (issue #133)"
```

Depois: `superpowers:finishing-a-development-branch` (PR de `feat/issue-133-namespace-write-declaration-identity` para `develop`, título `feat(compiler,vm): escrita tipada pelo namespace e tipo é a declaração (issue #133)`, corpo com o resumo do CHANGELOG e o rodapé `🤖 Generated with [Claude Code](https://claude.com/claude-code)` + link da sessão).
