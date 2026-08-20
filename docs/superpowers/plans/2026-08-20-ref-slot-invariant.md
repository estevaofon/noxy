# Invariante do slot `ref T` (issue #50) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fazer valer "um slot declarado `ref T` contém uma referência ou `null`, nunca um `T` cru": o compilador checa atribuição a campo através de base `ref` como checa com base valor; `json_loads` preenche slot `ref T` nulo com célula heap + ref; o runtime para de embrulhar valor cru (shim) e trata base `any` como base tipada; os builders JSON passam a contar posse (RC) como o bytecode.

**Architecture:** (1) `internal/compiler/compiler.go` perde o bypass `if baseWasRef { leftType = nil }` e ganha um hint para slots; (2) `internal/value` ganha `NewClosedUpvalue` (célula heap) e `ObjStruct.RefFields`/`FieldIsRef` (schema de runtime "este campo é `ref`?"); (3) `internal/vm/executor.go`: os opcodes contextuais viram erro explícito para valor cru, `OP_REF_PROPERTY`/`OP_REF_INDEX` consultam o schema e encaminham quando o slot é `ref T`, `OP_SET_PROPERTY`/`OP_SET_INDEX` rejeitam valor cru em slot `ref T`; (4) `internal/vm/json_population.go`/`builtins_json.go`: célula+ref para slot nulo e retain/release espelhando `OP_ARRAY`/`OP_MAP`/construtor, com toda escrita através de ref passando por `storeReferenceValue`.

**Tech Stack:** Go 1.x (módulo `noxy-vm`), `go test`, linguagem Noxy (`go run ./cmd/noxy arquivo.nx`), Python 3 para edições com CRLF.

**Spec:** `docs/superpowers/specs/2026-08-20-ref-slot-invariant-design.md` (ler antes de cada task; as seções são citadas como §N).

## Global Constraints

- **Branch/worktree:** branch `fix/ref-slot-invariant` (já existe, parte do `develop` local `4ef1777`). Trabalhar no worktree `.claude/worktrees/fix-ref-slot-invariant` (Task 0). **Não fazer push; não abrir PR** — a PR #51 ainda está aberta no GitHub e o usuário vai resolver isso depois.
- **Outras sessões Claude rodam em paralelo neste repositório** (5 processos). Antes de cada task: `git status -sb` e `git log --oneline -3` no worktree; se aparecer commit ou mudança que esta sessão não fez, parar e perguntar.
- **CRLF:** os arquivos do repo são CRLF. Arquivos **novos** (testes Go, scripts) são criados com o Write tool. Edições em arquivos existentes: Edit tool (strings exatas) ou script Python lendo/gravando com `newline=''` e preservando `\r\n`. Conferir sempre com `git diff --numstat` (diff por linha; um arquivo inteiro reescrito = CRLF perdido → desfazer). Não usar heredoc com código Go/Python no Bash.
- **Binários:** nunca `go build -o` em scratchpad/worktree (CrowdStrike apaga). Rodar Noxy com `go run ./cmd/noxy arquivo.nx`. `gofmt` em arquivo CRLF lista tudo: checar formatação com `sed 's/\r$//' arquivo.go | gofmt -l` (imprime `<standard input>` só se precisar de formatação).
- **Suíte:** `go test ./... -count=1` **sem `| tail`** — gravar em arquivo no scratchpad e `grep -E "^(FAIL|--- FAIL|ok|panic)"`. Testes direcionados: `go test ./internal/vm -run 'Nome' -count=1 -v`.
- **Scratchpad:** `C:/Users/estev/AppData/Local/Temp/claude/D--OneDrive-Documentos-go-projects-noxy/76068ccc-620f-44c0-b10c-0c94cfc4890c/scratchpad` (abaixo `$S`). Baseline de saída dos exemplos já capturado em `$S/baseline/*.out` (171 arquivos: stdout+stderr e linha final `exit=N`) com o script `$S/capture_examples.sh <outdir>` (rodar a partir da raiz do worktree).
- **Mensagens exatas** (compilação): `type mismatch in field assignment: expected int, got string`; `cannot assign Node to ref Node`; hint de slot: `hint: to point the field at a new value, bind it to a variable first and use 'x.proximo = ref novo'; to overwrite the referenced value use '*x.proximo = ...'` (`field`/`element`/`entry` conforme o alvo). (Runtime): `reference slot 'proximo' holds a non-reference value`, `reference slot at index 0 holds a non-reference value`, `reference slot for key "k" holds a non-reference value`, `cannot assign Node to ref Node`, `cannot update null reference`.
- **Versão:** `v0.10.0` (BREAKING), CHANGELOG datado `2026-08-20`.
- **Commits:** um por task, mensagem em PT no padrão `tipo(escopo): descrição`, terminando com a linha `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- **Asserções dos 6 testes RC migrados ficam inalteradas** (§4.2): se uma contagem de `Owners` divergir, investigar, não acomodar.

---

## Mapa de arquivos

| Arquivo | Responsabilidade nesta mudança |
|---|---|
| `internal/value/value.go` | `NewClosedUpvalue(v)`; `ObjStruct.RefFields` + `FieldIsRef(name)` |
| `internal/value/upvalue_cell_test.go` (novo), `internal/value/struct_ref_fields_test.go` (novo) | testes unitários do acima |
| `internal/compiler/compiler.go` | remover bypass (~706-713); preencher `RefFields` no case `*ast.StructStatement` (~818-824); `referenceSlotAssignmentTypeError` + 3 call sites |
| `internal/compiler/cow_lowering.go` | doc de `compileLValueBase` |
| `internal/compiler/ref_base_field_assignment_test.go` (novo) | erros/formas via base `ref`; consistência `RefFields`×`ConstructorType` |
| `internal/compiler/function_types_test.go` | expectativas do hint (campo/elemento/entrada) |
| `internal/vm/ref_slots.go` (novo) | helpers: `forwardRefSlot`, `describeRefSlotIndex`, `arrayElementIsRefSlot`, `mapValueIsRefSlot`, `refSlotWriteError` |
| `internal/vm/executor.go` | `OP_CONTEXT_REF_PROPERTY/INDEX` (shim → erro), `OP_REF_PROPERTY/INDEX` (consulta schema), `OP_SET_PROPERTY/INDEX` (guard) |
| `internal/vm/ref_slot_invariant_test.go` (novo) | estado impossível via native de teste; base `any` ≡ tipada; guards |
| `internal/vm/json_population.go` | `RefFields` em structs JSON; `buildReferentCell`; retain/release nos builders e setters; `jsonReferenceStorage` via `storeReferenceValue` |
| `internal/vm/builtins_json.go` | `populateRef` com setter via `storeReferenceValue`; `goValToNoxy` retém filhos |
| `internal/vm/json_ref_cell_test.go` (novo), `internal/vm/json_rc_test.go` (novo) | contrato da célula; RC dos builders |
| `internal/vm/native_signatures_test.go` | renomear/reforçar subteste "fill null slot with referent value" |
| `internal/vm/rc_uniqueness_test.go`, `noxy_examples/stack.nx` | migrar `_append` |
| `docs/NOXY_LANGUAGE_SPEC.md`, `docs/JSON_SUPPORT.md`, `CHANGELOG.md`, `README.md`, `internal/version/version.go` | docs e versão |

---

### Task 0: Worktree isolado e sanidade

**Files:** nenhum do repo (só git).

- [ ] **Step 1: Liberar a branch no diretório principal e criar o worktree**

No diretório principal (`D:/OneDrive/Documentos/go_projects/noxy`) a branch `fix/ref-slot-invariant` está checked out; um branch não pode estar em dois worktrees. Usar o skill `superpowers:using-git-worktrees`; na prática:

```bash
cd "D:/OneDrive/Documentos/go_projects/noxy"
git status --short            # deve estar vazio; se não, PARAR (outra sessão mexeu)
git checkout develop
git worktree add .claude/worktrees/fix-ref-slot-invariant fix/ref-slot-invariant
cd .claude/worktrees/fix-ref-slot-invariant
git log --oneline -3          # 0f2bbc9, 9d5b78b, 4ef1777
```

Todos os comandos das tasks seguintes rodam **dentro de `.claude/worktrees/fix-ref-slot-invariant`** (abaixo `$W`).

- [ ] **Step 2: Sanidade**

```bash
cd "$W" && go build ./... && go test ./internal/value ./internal/compiler -count=1 2>&1 | grep -E "^(ok|FAIL|---)"
```
Esperado: `ok` para os dois pacotes.

---

### Task 1: `value.NewClosedUpvalue` e `ObjStruct.RefFields`/`FieldIsRef`

**Files:**
- Modify: `internal/value/value.go` (struct `ObjUpvalue` ~145-157; `ObjStruct` ~363-368)
- Create: `internal/value/upvalue_cell_test.go`, `internal/value/struct_ref_fields_test.go`

**Interfaces:**
- Produces: `func NewClosedUpvalue(v Value) *ObjUpvalue` — caixa já fechada (`location == &closed`), não emprestada; `ObjStruct.RefFields map[string]bool` (nil quando o struct não tem campo `ref`); `func (os *ObjStruct) FieldIsRef(name string) bool` (nil-safe).

- [ ] **Step 1: Escrever os testes (Write tool)**

`internal/value/upvalue_cell_test.go`:
```go
package value

import "testing"

// A celula fechada e a "variavel anonima na heap" que json_loads usa para
// preencher um slot `ref T` nulo: possuidora, desligada da pilha, com
// Load/Store funcionando como numa caixa fechada por closeUpvalue.
func TestNewClosedUpvalueIsDetachedOwnedCell(t *testing.T) {
	cell := NewClosedUpvalue(NewInt(42))
	if !cell.IsValid() {
		t.Fatal("celula fechada deve ser valida")
	}
	if cell.IsBorrowed() {
		t.Fatal("celula fechada e possuidora, nao emprestada")
	}
	got, ok := cell.Load()
	if !ok || got.Type != VAL_INT || got.AsInt != 42 {
		t.Fatalf("Load = %#v, %v; esperado 42", got, ok)
	}
	var stackSlot Value
	if cell.PointsTo(&stackSlot) {
		t.Fatal("celula fechada nunca aponta para um slot de pilha")
	}
	if !cell.Store(NewInt(7)) {
		t.Fatal("Store deve funcionar numa celula fechada")
	}
	if got, _ := cell.Load(); got.AsInt != 7 {
		t.Fatalf("Load apos Store = %d, esperado 7", got.AsInt)
	}
}
```

`internal/value/struct_ref_fields_test.go`:
```go
package value

import "testing"

func TestStructFieldIsRef(t *testing.T) {
	definition := NewStruct("Node", []string{"valor", "proximo"}).Obj.(*ObjStruct)
	if definition.FieldIsRef("proximo") || definition.FieldIsRef("valor") {
		t.Fatal("sem RefFields nenhum campo e ref")
	}
	definition.RefFields = map[string]bool{"proximo": true}
	if !definition.FieldIsRef("proximo") || definition.FieldIsRef("valor") || definition.FieldIsRef("inexistente") {
		t.Fatal("FieldIsRef deve refletir RefFields")
	}
	var nilDefinition *ObjStruct
	if nilDefinition.FieldIsRef("x") {
		t.Fatal("FieldIsRef em nil deve ser false")
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

```bash
cd "$W" && go test ./internal/value -run 'TestNewClosedUpvalueIsDetachedOwnedCell|TestStructFieldIsRef' -count=1
```
Esperado: erro de compilação `undefined: NewClosedUpvalue` / `definition.RefFields undefined`.

- [ ] **Step 3: Implementar**

Em `internal/value/value.go`, logo após `func NewOpenUpvalue(...)`:
```go
// NewClosedUpvalue cria uma caixa ja fechada sobre um valor que nunca morou
// em slot de pilha — a "variavel anonima na heap" que json_loads usa para
// preencher um slot `ref T` nulo (spec 2026-08-20-ref-slot-invariant, §5.2):
// o analogo exato de `let novo: T = ...; slot = ref novo` depois que o frame
// fecha. A caixa e possuidora (nao emprestada); o chamador retem o valor em
// nome dela. PointsTo(slot de pilha) e sempre falso, entao retargetOwnedSlot
// a ignora.
func NewClosedUpvalue(v Value) *ObjUpvalue {
	upvalue := &ObjUpvalue{closed: v}
	upvalue.location = &upvalue.closed
	return upvalue
}
```

Em `ObjStruct`:
```go
type ObjStruct struct {
	Name              string
	Fields            []string
	JSONDynamicFields map[string]bool
	// RefFields marca os campos declarados `ref T`. E a fonte unica de
	// runtime para a pergunta "este slot e ref?" (OP_REF_PROPERTY,
	// OP_SET_PROPERTY — spec 2026-08-20-ref-slot-invariant §6.1): O(1) por
	// nome e presente tambem nos structs que o builder JSON cria sem
	// ConstructorType. Nil quando o struct nao tem campo ref (lookup em mapa
	// nil e valido e barato).
	RefFields       map[string]bool
	ConstructorType *RuntimeTypeInfo
}

// FieldIsRef informa se o campo foi declarado `ref T` (nil-safe).
func (os *ObjStruct) FieldIsRef(name string) bool {
	return os != nil && os.RefFields[name]
}
```

- [ ] **Step 4: Rodar e ver passar**

```bash
cd "$W" && go test ./internal/value -count=1
```
Esperado: `ok`.

- [ ] **Step 5: Commit**

```bash
cd "$W" && git add internal/value/value.go internal/value/upvalue_cell_test.go internal/value/struct_ref_fields_test.go && git commit -q -m "feat(value): NewClosedUpvalue (célula heap possuidora) e ObjStruct.RefFields/FieldIsRef (schema de runtime do slot ref)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" && git log --oneline -1
```

---

### Task 2: Checagem de campo com base `ref` (compilador) + migração de `stack.nx` e dos 6 testes RC

**Files:**
- Modify: `internal/compiler/compiler.go:703-713` (bypass), `:815-825` (RefFields), `internal/compiler/cow_lowering.go:11-16` (doc)
- Create: `internal/compiler/ref_base_field_assignment_test.go`
- Modify: `internal/vm/rc_uniqueness_test.go` (6× `_append`), `noxy_examples/stack.nx:6-12`

**Interfaces:**
- Consumes: `ObjStruct.RefFields`, `FieldIsRef` (Task 1).
- Produces: structs compilados têm `RefFields` preenchido (Tasks 4/5 dependem); `compileFunctionSource(t, src) (*Compiler, error)` e `parse(src)` já existem em `function_types_test.go`/`compiler_test.go`.

- [ ] **Step 1: Escrever os testes do compilador (Write tool)**

`internal/compiler/ref_base_field_assignment_test.go`:
```go
package compiler

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

// Issue #50, Parte 1: atribuicao a campo atraves de uma base `ref` e checada
// exatamente como atraves de uma base valor (spec §2.0 regras 1-2; o
// compilador conhece o tipo da base e do campo — nao e fronteira dinamica).
const refBasePrelude = `
struct Node
    valor: int
    proximo: ref Node
end
`

func TestFieldAssignmentThroughRefBaseIsTypeChecked(t *testing.T) {
	_, err := compileFunctionSource(t, refBasePrelude+`
func estraga(node: ref Node)
    node.valor = "texto"
end`)
	if err == nil {
		t.Fatal("esperava erro de tipo via base ref")
	}
	want := "type mismatch in field assignment: expected int, got string"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("erro = %q, esperava conter %q", err, want)
	}
}

func TestRefFieldThroughRefBaseRejectsRawReferent(t *testing.T) {
	cases := map[string]string{
		"construtor": `
func liga(node: ref Node)
    node.proximo = Node(9, null)
end`,
		"variavel por valor": `
func liga(node: ref Node, outro: Node)
    node.proximo = outro
end`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := compileFunctionSource(t, refBasePrelude+src)
			if err == nil {
				t.Fatal("esperava 'cannot assign Node to ref Node'")
			}
			if !strings.Contains(err.Error(), "cannot assign Node to ref Node") {
				t.Fatalf("erro = %q", err)
			}
		})
	}
}

func TestRefBaseFieldAssignmentAcceptsTypedForms(t *testing.T) {
	_, err := compileFunctionSource(t, refBasePrelude+`
func formas(node: ref Node, outro: ref Node)
    node.valor = 5
    let novo: Node = Node(7, null)
    node.proximo = ref novo
    node.proximo = null
    node.proximo = outro.proximo
    node.proximo = outro
    *node.proximo = Node(8, null)
end`)
	if err != nil {
		t.Fatalf("formas validas via base ref devem compilar: %v", err)
	}
}

// RefFields e a fonte de runtime da pergunta "este campo e ref?" e tem de
// bater com ConstructorType.ParamIsRef (spec §6.1) para todo struct que o
// compilador emite.
func TestStructDefinitionMarksRefFieldsConsistentlyWithConstructorType(t *testing.T) {
	code, _, err := New().Compile(parse(refBasePrelude + `
struct Plain
    a: int
    b: string
end
let n: Node = Node(1, null)
let p: Plain = Plain(1, "x")`))
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, constant := range code.Constants {
		definition, ok := constant.Obj.(*value.ObjStruct)
		if !ok {
			continue
		}
		seen++
		if definition.ConstructorType == nil || len(definition.ConstructorType.ParamIsRef) != len(definition.Fields) {
			t.Fatalf("struct %s sem ConstructorType alinhado aos campos", definition.Name)
		}
		for i, field := range definition.Fields {
			wantRef := definition.ConstructorType.ParamIsRef[i]
			if definition.FieldIsRef(field) != wantRef {
				t.Fatalf("struct %s campo %s: FieldIsRef=%v, ConstructorType.ParamIsRef=%v", definition.Name, field, definition.FieldIsRef(field), wantRef)
			}
		}
		if definition.Name == "Node" && !definition.FieldIsRef("proximo") {
			t.Fatal("Node.proximo deveria estar em RefFields")
		}
		if definition.Name == "Plain" && definition.RefFields != nil {
			t.Fatalf("Plain nao tem campo ref; RefFields deveria ser nil, veio %v", definition.RefFields)
		}
	}
	if seen != 2 {
		t.Fatalf("esperava 2 definicoes de struct nas constantes, vi %d", seen)
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

```bash
cd "$W" && go test ./internal/compiler -run 'ThroughRefBase|RefBaseField|MarksRefFields' -count=1
```
Esperado: `TestFieldAssignmentThroughRefBaseIsTypeChecked` e `TestRefFieldThroughRefBaseRejectsRawReferent` FAIL ("esperava erro"); `TestStructDefinitionMarksRefFields...` FAIL ("Node.proximo deveria estar em RefFields"); `TestRefBaseFieldAssignmentAcceptsTypedForms` PASS.

- [ ] **Step 3: Remover o bypass e preencher `RefFields`**

Em `internal/compiler/compiler.go`, no case `*ast.MemberAccessExpression` do assignment, substituir

```go
			leftType, baseWasRef, err := c.compileLValueBase(memberExp.Left)
			if err != nil {
				return nil, nil, err
			}
			// Compatibilidade pré-0.4: com base ref, o checker antigo nunca
			// resolvia o tipo do campo (a asserção sobre RefType falhava em
			// silêncio) e o assignment passava sem checagem. Replicado aqui
			// para não rejeitar programas existentes (ex.: stack.nx).
			if baseWasRef {
				leftType = nil
			}
```
por
```go
			// compileLValueBase ja devolve o tipo DESEMBRULHADO (emite
			// OP_DEREF_MUT quando a base e ref), entao o campo resolve igual
			// com base `ref` ou valor e a checagem abaixo vale para as duas
			// (issue #50, Parte 1; spec 2026-08-20-ref-slot-invariant §3).
			leftType, _, err := c.compileLValueBase(memberExp.Left)
			if err != nil {
				return nil, nil, err
			}
```

No case `*ast.StructStatement`, substituir o laço que preenche `JSONDynamicFields`:
```go
		structDefinition.JSONDynamicFields = make(map[string]bool)
		for _, field := range n.FieldsList {
			if primitive, ok := field.Type.(*ast.PrimitiveType); ok && primitive.Name == "any" {
				structDefinition.JSONDynamicFields[field.Name] = true
			}
			// RefFields: schema de runtime do slot ref (spec §6.1); nil quando
			// o struct nao tem campo ref.
			if _, isRef := field.Type.(*ast.RefType); isRef {
				if structDefinition.RefFields == nil {
					structDefinition.RefFields = make(map[string]bool)
				}
				structDefinition.RefFields[field.Name] = true
			}
		}
```

Em `internal/compiler/cow_lowering.go`, na doc de `compileLValueBase`, trocar a frase final "e um flag indicando se o nível final era ref — o branch de member-assignment usa o flag para replicar a leniência do checker pré-0.4 com bases ref." por "e um flag indicando se o nível final era ref (informativo: desde a #50 o member-assignment checa o campo do mesmo jeito com base ref ou valor)."

- [ ] **Step 4: Rodar os testes do compilador**

```bash
cd "$W" && go test ./internal/compiler -count=1 2>&1 | grep -E "^(ok|FAIL|--- FAIL)"
```
Esperado: `ok` (os 4 testes novos passam; nenhum outro quebra — o hint antigo ainda vale até a Task 3).

- [ ] **Step 5: Ver os testes RC quebrarem pelo motivo certo**

```bash
cd "$W" && go test ./internal/vm -run 'TestRefLocalBindingIsBorrowNotOwner|TestRefGlobalAndCapturedRefLocalAreBorrows|TestCapturedAndBorrowedRefSlotsNeverReleaseWhatTheyDoNotOwn|TestBorrowedUpvalueRebindKeepsOwnersOfSharedNode|TestBorrowConditionIsStaticNotInferredFromOwnedList|TestRefWriteToUniquelyOwnedNodeMutatesInPlace' -count=1 2>&1 | grep -E "^(--- FAIL|FAIL|ok)|cannot assign Node"
```
Esperado: os 6 falham com `cannot assign Node to ref Node` (compilação do programa do teste).

- [ ] **Step 6: Migrar o `_append` (script Python via Write tool)**

`$S/migrate_append.py`:
```python
import sys

OLD = """func _append(node: ref Node, valor: int)
    if node.proximo == null then
        node.proximo = Node(valor, null)
    else
        _append(node.proximo, valor)
    end
end"""
NEW = """func _append(node: ref Node, valor: int)
    if node.proximo == null then
        let novo: Node = Node(valor, null)   // variavel: `ref` exige L-value; vai para a heap
        node.proximo = ref novo              // REBIND do campo do pai
    else
        _append(node.proximo, valor)
    end
end"""

for path, expected in [("internal/vm/rc_uniqueness_test.go", 6), ("noxy_examples/stack.nx", 1)]:
    with open(path, "r", encoding="utf-8", newline="") as f:
        raw = f.read()
    crlf = "\r\n" in raw
    text = raw.replace("\r\n", "\n")
    n = text.count(OLD)
    if n != expected:
        sys.exit(f"{path}: esperava {expected} ocorrencias, achei {n}")
    text = text.replace(OLD, NEW)
    if crlf:
        text = text.replace("\n", "\r\n")
    with open(path, "w", encoding="utf-8", newline="") as f:
        f.write(text)
    print(path, "ok", n)
```
Rodar: `cd "$W" && python "$S/migrate_append.py" && git diff --numstat` — esperado `+12 -6` em `rc_uniqueness_test.go` (2 linhas a mais por ocorrência) e `+2 -1` em `stack.nx` (não um arquivo inteiro).

- [ ] **Step 7: Rodar os 6 testes RC e o stack.nx**

```bash
cd "$W" && go test ./internal/vm -run 'TestRefLocalBindingIsBorrowNotOwner|TestRefGlobalAndCapturedRefLocalAreBorrows|TestCapturedAndBorrowedRefSlotsNeverReleaseWhatTheyDoNotOwn|TestBorrowedUpvalueRebindKeepsOwnersOfSharedNode|TestBorrowConditionIsStaticNotInferredFromOwnedList|TestRefWriteToUniquelyOwnedNodeMutatesInPlace' -count=1 -v 2>&1 | grep -E "^(--- |ok|FAIL)"
cd "$W" && (go run ./cmd/noxy noxy_examples/stack.nx; echo "exit=$?") > "$S/stack.branch.out" 2>&1; diff "$S/baseline/stack.nx.out" "$S/stack.branch.out" && echo "stack.nx: saida identica"
```
Esperado: 6× `--- PASS`, `ok`; `stack.nx: saida identica`. (Se um teste de `Owners` divergir: **não** ajustar o número — §4.2 prevê contagens iguais com a célula no lugar do campo; divergência é bug a investigar.)

- [ ] **Step 8: Suíte dos dois pacotes**

```bash
cd "$W" && go test ./internal/compiler ./internal/vm -count=1 2>&1 | grep -E "^(ok|FAIL|--- FAIL)"
```
Esperado: `ok` nos dois. Se algum outro teste quebrar com `cannot assign ... to ref ...` ou `type mismatch in field assignment` via base `ref`, migrá-lo para a forma idiomática (mesmo `let novo` + `= ref novo`) e citá-lo na mensagem de commit — **nunca** afrouxar a checagem.

- [ ] **Step 9: Commit**

```bash
cd "$W" && git add internal/compiler/compiler.go internal/compiler/cow_lowering.go internal/compiler/ref_base_field_assignment_test.go internal/vm/rc_uniqueness_test.go noxy_examples/stack.nx && git commit -q -m "fix(compiler): atribuição a campo via base ref é checada como via base valor (BREAKING); RefFields no ObjStruct; _append de stack.nx e dos 6 testes RC migrados para 'let novo / = ref novo' (#50 Parte 1-2)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" && git log --oneline -1
```

---

### Task 3: Hint de slot `ref T` (campo/elemento/entrada)

**Files:**
- Modify: `internal/compiler/compiler.go` (~665, ~678, ~757: call sites; ~2751: nova função ao lado de `referenceAssignmentTypeError`)
- Modify: `internal/compiler/function_types_test.go:222-295` (`TestReferenceSlotValueAssignmentsSuggestDereference`)

**Interfaces:**
- Produces: `func referenceSlotAssignmentTypeError(line int, name, slotKind string, expected, actual ast.NoxyType) error`.

- [ ] **Step 1: Atualizar o teste existente (Edit tool)**

Em `function_types_test.go`, substituir a função `TestReferenceSlotValueAssignmentsSuggestDereference` inteira por:
```go
func TestReferenceSlotValueAssignmentsSuggestDereference(t *testing.T) {
	tests := []struct {
		name, input string
		hints       []string
	}{
		{
			name: "local reference parameter",
			input: `
func increment(value: ref int) -> void
    value = value + 1
end`,
			hints: []string{"use '*value = ...'"},
		},
		{
			name: "global",
			input: `
let number: int = 0
let value: ref int = ref number
value = 1`,
			hints: []string{"use '*value = ...'"},
		},
		{
			name: "captured reference parameter",
			input: `
func outer(value: ref int) -> void
    func inner() -> void
        value = value + 1
    end
end`,
			hints: []string{"use '*value = ...'"},
		},
		{
			name: "field",
			input: `
struct Holder
    field: ref int
end
let number: int = 0
let holder: Holder = Holder(ref number)
holder.field = 1`,
			hints: []string{"to point the field at a new value", "use 'holder.field = ref novo'", "use '*holder.field = ...'"},
		},
		{
			name: "field through ref base",
			input: `
struct Holder
    field: ref int
end
func f(holder: ref Holder)
    holder.field = 1
end`,
			hints: []string{"to point the field at a new value", "use 'holder.field = ref novo'", "use '*holder.field = ...'"},
		},
		{
			name: "array element",
			input: `
let number: int = 0
let items: (ref int)[] = [ref number]
items[0] = 1`,
			hints: []string{"to point the element at a new value", "use 'items[0] = ref novo'", "use '*items[0] = ...'"},
		},
		{
			name: "map value",
			input: `
let number: int = 0
let items: map[string, ref int] = {"item": ref number}
items["item"] = 1`,
			hints: []string{"to point the entry at a new value", "use 'items[\"item\"] = ref novo'", "use '*items[\"item\"] = ...'"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := compileFunctionSource(t, tt.input)
			if err == nil {
				t.Fatal("expected reference assignment error")
			}
			if !strings.Contains(err.Error(), "cannot assign int to ref int") {
				t.Fatalf("error=%q, want reference assignment diagnostic", err)
			}
			for _, hint := range tt.hints {
				if !strings.Contains(err.Error(), hint) {
					t.Fatalf("error=%q, want hint %q", err, hint)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

```bash
cd "$W" && go test ./internal/compiler -run 'TestReferenceSlotValueAssignmentsSuggestDereference' -count=1
```
Esperado: subtestes `field`, `field through ref base`, `array element`, `map value` FAIL (hint novo ausente); os três de variável PASS.

- [ ] **Step 3: Implementar**

Em `compiler.go`, logo após `referenceAssignmentTypeError`:
```go
// referenceSlotAssignmentTypeError e a variante para alvos que sao SLOTS
// (campo de struct = "field", elemento de array = "element", valor de map =
// "entry"). O hint cobre os dois caminhos legitimos da tabela de §2.3: apontar
// o slot para um valor novo ('x = ref novo' — uma variavel, porque `ref`
// exige L-value e o compilador a promove para a heap) e sobrescrever o
// referente ('*x = ...'). Variavel `ref T` segue com
// referenceAssignmentTypeError (spec §2.3 documenta aquele hint).
func referenceSlotAssignmentTypeError(line int, name, slotKind string, expected, actual ast.NoxyType) error {
	return fmt.Errorf(
		"[line %d] cannot assign %s to %s\n  hint: to point the %s at a new value, bind it to a variable first and use '%s = ref novo'; to overwrite the referenced value use '*%s = ...'",
		line, noxyTypeName(actual), noxyTypeName(expected), slotKind, name, name,
	)
}
```
Trocar os três call sites:
- array (`arrType.ElementType` é ref): `return nil, nil, referenceSlotAssignmentTypeError(c.currentLine, assignmentTargetName(indexExp), "element", arrType.ElementType, valType)`
- map (`mapType.ValueType` é ref): `return nil, nil, referenceSlotAssignmentTypeError(c.currentLine, assignmentTargetName(indexExp), "entry", mapType.ValueType, valType)`
- member: `return nil, nil, referenceSlotAssignmentTypeError(c.currentLine, assignmentTargetName(memberExp), "field", fieldType, valType)`

(O call site de variável — `referenceAssignmentTypeError(c.currentLine, name, ...)` no assignment a identificador — fica como está.)

- [ ] **Step 4: Rodar e ver passar**

```bash
cd "$W" && go test ./internal/compiler -count=1 2>&1 | grep -E "^(ok|FAIL|--- FAIL)"
```
Esperado: `ok`.

- [ ] **Step 5: Commit**

```bash
cd "$W" && git add internal/compiler/compiler.go internal/compiler/function_types_test.go && git commit -q -m "feat(compiler): hint de slot ref T (campo/elemento/entrada) mostra 'x = ref novo' e '*x = ...' (#50 Parte 2)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" && git log --oneline -1
```

---

### Task 4: Runtime — shim removido, `OP_REF_PROPERTY/INDEX` consultam o schema, estado impossível é erro explícito

**Files:**
- Create: `internal/vm/ref_slots.go`, `internal/vm/ref_slot_invariant_test.go`
- Modify: `internal/vm/executor.go` (`OP_REF_PROPERTY` ~379-419, `OP_REF_INDEX` ~421-433, `OP_CONTEXT_REF_PROPERTY` ~435-471, `OP_CONTEXT_REF_INDEX` ~473-518), `internal/vm/json_population.go` (`buildTypedJSONValue` `TYPE_STRUCT` ~330-350: `RefFields`)

**Interfaces:**
- Consumes: `ObjStruct.FieldIsRef` (Task 1), `RefFields` preenchido pelo compilador (Task 2), `vm.resolveReferenceValue`, `referenceMapKey`, `markProbeReadonly`/`interpretVMSource`/`requireBoolResults`/`runTypedFunctionProgram[Error]`/`testExpectedObject` (helpers de teste já existentes no pacote `vm`).
- Produces (em `ref_slots.go`): `func forwardRefSlot(stored value.Value, slot string) (value.Value, error)`; `func describeRefSlotIndex(index value.Value) string`; `func arrayElementIsRefSlot(array *value.ObjArray) bool`; `func mapValueIsRefSlot(mapping *value.ObjMap) bool`.

- [ ] **Step 1: Escrever os testes (Write tool)**

`internal/vm/ref_slot_invariant_test.go`:
```go
package vm

// Invariante do slot `ref T` (issue #50; spec
// docs/superpowers/specs/2026-08-20-ref-slot-invariant-design.md): um slot
// declarado `ref T` contem ref ou null. O runtime nao embrulha mais valor
// cru numa ref para o slot (shim da #51 removido) — e erro explicito — e a
// base `any` se comporta como a base tipada para slots ref.

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

const refSlotPrelude = `
struct Node
    valor: int
    proximo: ref Node
end
func eh_nulo(n: ref Node) -> bool
    return n == null
end
`

// newCorruptingVM registra natives de teste que gravam um valor CRU direto no
// slot, por baixo dos guards — depois deste PR e o unico jeito de fabricar o
// estado impossivel.
func newCorruptingVM(t *testing.T) *VM {
	t.Helper()
	machine := New()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		return value.NewNull()
	})
	machine.DefineNative("corrupt_ref_field", func(args []value.Value) value.Value {
		instance := args[0].Obj.(*value.ObjInstance)
		instance.Fields[args[1].Obj.(string)] = args[2]
		return value.NewNull()
	})
	machine.DefineNative("corrupt_ref_index", func(args []value.Value) value.Value {
		array := args[0].Obj.(*value.ObjArray)
		array.Elements[int(args[1].AsInt)] = args[2]
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "corrupt_ref_field")
	markProbeReadonly(t, machine, "corrupt_ref_index")
	return machine
}

func TestRawValueInRefFieldIsExplicitRuntimeError(t *testing.T) {
	cases := map[string]string{
		"argumento contextual": `
let a: Node = Node(1, null)
corrupt_ref_field(a, "proximo", Node(2, null))
let r: bool = eh_nulo(a.proximo)`,
		"ref explicito": `
let a: Node = Node(1, null)
corrupt_ref_field(a, "proximo", Node(2, null))
let r: bool = eh_nulo(ref a.proximo)`,
		"base any": `
let a: Node = Node(1, null)
corrupt_ref_field(a, "proximo", Node(2, null))
let d: any = a
let r: bool = eh_nulo(d.proximo)`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			machine := newCorruptingVM(t)
			err := interpretVMSource(t, machine, refSlotPrelude+src)
			if err == nil || !strings.Contains(err.Error(), "reference slot 'proximo' holds a non-reference value") {
				t.Fatalf("esperava erro explicito de slot ref com valor cru, veio %v", err)
			}
		})
	}
}

func TestRawValueInRefArrayElementIsExplicitRuntimeError(t *testing.T) {
	machine := newCorruptingVM(t)
	err := interpretVMSource(t, machine, refSlotPrelude+`
let arr: (ref Node)[] = [null]
corrupt_ref_index(arr, 0, Node(2, null))
let r: bool = eh_nulo(arr[0])`)
	if err == nil || !strings.Contains(err.Error(), "reference slot at index 0 holds a non-reference value") {
		t.Fatalf("esperava erro explicito de elemento ref com valor cru, veio %v", err)
	}
}

// Emenda 1 da #50: via base `any` o compilador emite OP_REF_PROPERTY (nao
// conhece o campo); o runtime consulta RefFields e encaminha como o opcode
// contextual — `*n = ...` sobre campo nulo e "cannot update null reference",
// igual a base tipada; campo nao-nulo escreve atraves da ref existente.
func TestAnyBaseRefFieldForwardsLikeTypedBase(t *testing.T) {
	err := runTypedFunctionProgramError(t, refSlotPrelude+`
func preenche(n: ref Node)
    *n = Node(7, null)
end
let a: any = Node(1, null)
preenche(a.proximo)`)
	if err == nil || !strings.Contains(err.Error(), "cannot update null reference") {
		t.Fatalf("via base any, campo ref nulo deve chegar como null: %v", err)
	}

	requireBoolResults(t, refSlotPrelude+`
func preenche(n: ref Node)
    *n = Node(7, null)
end
let b: Node = Node(2, null)
let a: any = Node(1, ref b)
preenche(a.proximo)
test_report([b.valor == 7, eh_nulo(a.proximo), eh_nulo(ref a.proximo)])`, []bool{true, false, false})
}

func TestAnyBaseNullRefFieldJSONLoadsReturnsFalse(t *testing.T) {
	got := runTypedFunctionProgram(t, `
struct Holder
    child: ref any
end
let h: Holder = Holder(null)
let d: any = h
let ok: bool = json_loads("{\"a\": 1}", d.child)
if !ok && h.child == null then
    test_report(42)
else
    test_report(0)
end`)
	testExpectedObject(t, 42, got)
}

// Array/map etiquetados (RuntimeType) alcancados por `any`: OP_REF_INDEX
// encaminha o elemento/valor ref (null incluido; chave ausente le null).
func TestAnyBaseRefArrayAndMapSlotsForwardNull(t *testing.T) {
	requireBoolResults(t, `
func eh_nulo_int(r: ref int) -> bool
    return r == null
end
let arr: (ref int)[] = [null]
let m: map[string, ref int] = {}
let da: any = arr
let dm: any = m
test_report([eh_nulo_int(da[0]), eh_nulo_int(dm["x"])])`, []bool{true, true})
}

// Slot comum via `any` continua dando ref para o slot (comportamento antigo).
func TestAnyBasePlainFieldStillReferencesTheSlot(t *testing.T) {
	requireBoolResults(t, refSlotPrelude+`
func soma(n: ref int)
    *n = n + 10
end
let a: any = Node(1, null)
soma(a.valor)
let b: Node = a
test_report([b.valor == 11])`, []bool{true})
}
```

- [ ] **Step 2: Rodar e ver falhar**

```bash
cd "$W" && go test ./internal/vm -run 'RawValueInRef|AnyBaseRef|AnyBaseNullRefField|AnyBasePlainField' -count=1 2>&1 | grep -E "^(--- |ok|FAIL)"
```
Esperado: `TestRawValueInRefFieldIsExplicitRuntimeError` (3 subtestes) e `TestRawValueInRefArrayElement...` FAIL (hoje o shim encaminha sem erro); `TestAnyBaseRefFieldForwardsLikeTypedBase` FAIL (hoje `*n = ...` grava cru e nao erra); `TestAnyBaseNullRefFieldJSONLoadsReturnsFalse` FAIL (hoje preenche o slot cru e devolve true); `TestAnyBaseRefArrayAndMapSlotsForwardNull` FAIL (`false,false`); `TestAnyBasePlainFieldStillReferencesTheSlot` PASS.

- [ ] **Step 3: Criar `internal/vm/ref_slots.go` (Write tool)**

```go
package vm

import (
	"fmt"

	"noxy-vm/internal/value"
)

// Invariante do slot `ref T` (spec docs/superpowers/specs/
// 2026-08-20-ref-slot-invariant-design.md): um slot declarado `ref T` —
// campo de struct, elemento de `(ref T)[]`, valor de `map[K, ref T]` —
// contem uma referencia ou null. O compilador garante isso para bases
// tipadas; em fronteira dinamica (base `any`) o runtime consulta o schema
// (ObjStruct.RefFields, tag RuntimeType de array/map) e aplica a mesma
// regra. Qualquer outra coisa num slot ref e estado impossivel e vira erro
// explicito em vez de ser embrulhado numa ref para o slot (shim da #51).

// forwardRefSlot devolve o conteudo de um slot `ref T` para encaminhamento
// (spec §2.3 regra 2, §4.2): ref ou null passam como estao.
func forwardRefSlot(stored value.Value, slot string) (value.Value, error) {
	if stored.Type == value.VAL_REF || stored.Type == value.VAL_NULL {
		return stored, nil
	}
	return value.Value{}, fmt.Errorf("reference slot %s holds a non-reference value", slot)
}

// describeRefSlotIndex nomeia o slot de um indice nas mensagens de erro:
// `at index 3` para array, `for key "k"` para map com chave string.
func describeRefSlotIndex(index value.Value) string {
	if index.Type == value.VAL_OBJ {
		if key, ok := index.Obj.(string); ok {
			return fmt.Sprintf("for key %q", key)
		}
	}
	return fmt.Sprintf("at index %s", index.String())
}

// arrayElementIsRefSlot: o array passou por um contexto tipado que o etiquetou
// com `(ref T)[]`. Sem tag nao ha informacao (fronteira dinamica pura).
func arrayElementIsRefSlot(array *value.ObjArray) bool {
	if array == nil {
		return false
	}
	tag := array.RuntimeType.Load()
	return tag != nil && tag.Kind == value.TYPE_ARRAY && tag.Element != nil && tag.Element.Kind == value.TYPE_REF
}

// mapValueIsRefSlot: idem para `map[K, ref T]`.
func mapValueIsRefSlot(mapping *value.ObjMap) bool {
	if mapping == nil {
		return false
	}
	tag := mapping.RuntimeType.Load()
	return tag != nil && tag.Kind == value.TYPE_MAP && tag.Value != nil && tag.Value.Kind == value.TYPE_REF
}
```

- [ ] **Step 4: Editar `executor.go`**

(a) `OP_CONTEXT_REF_PROPERTY`: substituir tudo a partir do comentário `// O tipo estatico do campo ja e `ref T`: ...` até o fechamento do `vm.push(value.Value{...})` do shim por:
```go
			// Invariante do slot ref (spec 2026-08-20-ref-slot-invariant):
			// ref ou null e encaminhado como esta (spec §2.3 regra 2, §4.2) —
			// igual a uma variavel `ref T`; valor cru e estado impossivel e
			// erro explicito (o shim da #51 que o embrulhava saiu na #50).
			forwarded, err := forwardRefSlot(stored, "'"+name+"'")
			if err != nil {
				return vm.runtimeError(c, ip, "%s", err)
			}
			vm.push(forwarded)
```

(b) `OP_CONTEXT_REF_INDEX`: substituir tudo a partir do comentário `// Elemento/valor de tipo estatico `ref T`: ...` até o `vm.push(value.Value{... REF_INDEX ...})` por:
```go
			// Mesmo invariante do OP_CONTEXT_REF_PROPERTY: ref/null
			// encaminha, valor cru e erro explicito.
			forwarded, err := forwardRefSlot(stored, describeRefSlotIndex(idx))
			if err != nil {
				return vm.runtimeError(c, ip, "%s", err)
			}
			vm.push(forwarded)
```

(c) `OP_REF_PROPERTY`: logo depois do bloco
```go
			if container.Type != value.VAL_OBJ {
				return vm.runtimeError(c, ip, "Property reference base must be an object")
			}
```
inserir:
```go
			// Base que o compilador nao conhecia (`any`, struct de outro
			// modulo): se o campo e declarado `ref T`, comporta como
			// OP_CONTEXT_REF_PROPERTY — encaminha a ref/null armazenada em vez
			// de fabricar uma ref para o slot (que deixaria `*n = T` gravar
			// cru). Spec 2026-08-20-ref-slot-invariant §6.2.
			if instance, ok := container.Obj.(*value.ObjInstance); ok && instance != nil && instance.Struct.FieldIsRef(name) {
				stored, exists := instance.Fields[name]
				if !exists {
					return vm.runtimeError(c, ip, "undefined property '%s'", name)
				}
				forwarded, err := forwardRefSlot(stored, "'"+name+"'")
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				vm.push(forwarded)
				continue
			}
```
(e remover o bloco comentado `/*( if inst, ok := ... fmt.Printf("VM REF_PROPERTY ...") */` logo abaixo — código morto.)

(d) `OP_REF_INDEX`: substituir o case inteiro por:
```go
		case chunk.OP_REF_INDEX:
			// Pop Index, then Container
			idx := vm.pop()
			container := vm.pop()

			// Base que o compilador nao conhecia (`any`): resolve um
			// eventual ref (como OP_REF_PROPERTY) e, se o array/map esta
			// etiquetado com elemento/valor `ref T`, espelha
			// OP_CONTEXT_REF_INDEX por inteiro (encaminha ref/null; chave
			// ausente le null; valor cru e erro). Spec §6.2.
			if container.Type == value.VAL_REF {
				resolved, err := vm.resolveReferenceValue(container)
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				container = resolved
			}
			if container.Type == value.VAL_OBJ {
				switch collection := container.Obj.(type) {
				case *value.ObjArray:
					if arrayElementIsRefSlot(collection) {
						if idx.Type != value.VAL_INT {
							return vm.runtimeError(c, ip, "array index must be integer")
						}
						arrayIndex := int(idx.AsInt)
						if arrayIndex < 0 || arrayIndex >= len(collection.Elements) {
							return vm.runtimeError(c, ip, "array index out of bounds")
						}
						forwarded, err := forwardRefSlot(collection.Elements[arrayIndex], describeRefSlotIndex(idx))
						if err != nil {
							return vm.runtimeError(c, ip, "%s", err)
						}
						vm.push(forwarded)
						continue
					}
				case *value.ObjMap:
					if mapValueIsRefSlot(collection) {
						key, err := referenceMapKey(idx)
						if err != nil {
							return vm.runtimeError(c, ip, "%s", err)
						}
						stored, found := collection.Get(key)
						if !found {
							stored = value.NewNull()
						}
						forwarded, err := forwardRefSlot(stored, describeRefSlotIndex(idx))
						if err != nil {
							return vm.runtimeError(c, ip, "%s", err)
						}
						vm.push(forwarded)
						continue
					}
				}
			}

			vm.push(value.Value{
				Type: value.VAL_REF,
				Obj: &value.ObjRef{
					RefType:   value.REF_INDEX,
					Container: container,
					Index:     idx,
				},
			})
```

(e) `internal/vm/json_population.go`, em `buildTypedJSONValue` `case value.TYPE_STRUCT`, dentro do `for _, name := range fieldNames {` logo após o `if ... TYPE_ANY { definition.JSONDynamicFields[name] = true }`:
```go
			if schema.Fields[name] != nil && schema.Fields[name].Kind == value.TYPE_REF {
				if definition.RefFields == nil {
					definition.RefFields = make(map[string]bool)
				}
				definition.RefFields[name] = true
			}
```

- [ ] **Step 5: Rodar e ver passar**

```bash
cd "$W" && go build ./... && go test ./internal/vm -run 'RawValueInRef|AnyBaseRef|AnyBaseNullRefField|AnyBasePlainField|RefNullForwarding|NullRef|MissingMapKey|NonNullRefField' -count=1 2>&1 | grep -E "^(--- |ok|FAIL)"
```
Esperado: todos PASS (inclusive os 5 de `ref_null_forwarding_test.go`). Depois o pacote inteiro: `go test ./internal/vm -count=1 2>&1 | grep -E "^(ok|FAIL|--- FAIL)"` → `ok`. (Se `TestAnyBaseRefArrayAndMapSlotsForwardNull` falhar com `false`, conferir que `let arr: (ref int)[] = [null]` emitiu `OP_MARK_RUNTIME_VALUE_TYPE` — `compiler.go` ~342 — e que a tag chegou ao array; não afrouxar o teste.)

- [ ] **Step 6: Commit**

```bash
cd "$W" && git add internal/vm/ref_slots.go internal/vm/ref_slot_invariant_test.go internal/vm/executor.go internal/vm/json_population.go && git commit -q -m "fix(vm): shim dos opcodes contextuais removido — valor cru em slot ref T é erro explícito; OP_REF_PROPERTY/OP_REF_INDEX consultam o schema e encaminham como a base tipada (#50 emenda 1)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" && git log --oneline -1
```

---

### Task 5: Guards de escrita em `OP_SET_PROPERTY` / `OP_SET_INDEX` (rota 5, base `any`)

**Files:**
- Modify: `internal/vm/ref_slots.go` (+ `refSlotWriteError`), `internal/vm/executor.go` (`OP_SET_INDEX` ~1393-1445, `OP_SET_PROPERTY` ~1481-1514), `internal/vm/ref_slot_invariant_test.go` (+ testes)

**Interfaces:**
- Consumes: `validStructConstructorType(definition)` (`runtime_type_validation.go`), `runtimeTypeName(val)` (`runtime_type_name.go`), `arrayElementIsRefSlot`/`mapValueIsRefSlot` (Task 4).
- Produces: `func refSlotWriteError(expected string, val value.Value) string`; `func structRefFieldTypeName(definition *value.ObjStruct, name string) string`.

- [ ] **Step 1: Acrescentar os testes (Edit tool, no fim de `ref_slot_invariant_test.go`)**

```go
// Rota 5 (nao listada na issue): OP_SET_PROPERTY/OP_SET_INDEX nao validavam
// nada em base `any` e gravavam T cru em slot `ref T`. Agora e o gemeo
// dinamico do erro de compilacao; ref/null seguem aceitos.
func TestAnyBaseWriteOfRawValueIntoRefFieldIsRuntimeError(t *testing.T) {
	err := runTypedFunctionProgramError(t, refSlotPrelude+`
let a: any = Node(1, null)
a.proximo = Node(9, null)`)
	if err == nil || !strings.Contains(err.Error(), "cannot assign Node to ref Node") {
		t.Fatalf("esperava 'cannot assign Node to ref Node' em runtime, veio %v", err)
	}
}

func TestAnyBaseWriteOfRefOrNullIntoRefFieldIsAllowed(t *testing.T) {
	requireBoolResults(t, refSlotPrelude+`
let b: Node = Node(2, null)
let a: any = Node(1, null)
a.proximo = ref b
let ligado: bool = !eh_nulo(a.proximo)
a.proximo = null
test_report([ligado, eh_nulo(a.proximo)])`, []bool{true, true})
}

func TestAnyBaseWriteOfRawValueIntoRefElementIsRuntimeError(t *testing.T) {
	cases := map[string]string{
		"array": `
let arr: (ref int)[] = [null]
let d: any = arr
d[0] = 5`,
		"map": `
let m: map[string, ref int] = {}
let d: any = m
d["k"] = 5`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			err := runTypedFunctionProgramError(t, src)
			if err == nil || !strings.Contains(err.Error(), "cannot assign int to ref int") {
				t.Fatalf("esperava 'cannot assign int to ref int' em runtime, veio %v", err)
			}
		})
	}
}

// Campo comum via `any` segue fronteira dinamica sem checagem (inalterado).
func TestAnyBasePlainFieldWriteIsStillUnchecked(t *testing.T) {
	got := runTypedFunctionProgram(t, refSlotPrelude+`
let a: any = Node(1, null)
a.valor = "texto"
test_report(type(a.valor))`)
	if got.Type != value.VAL_OBJ || got.Obj.(string) != "string" {
		t.Fatalf("campo comum via any continua sem checagem; veio %v", got)
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

```bash
cd "$W" && go test ./internal/vm -run 'AnyBaseWrite|AnyBasePlainFieldWrite' -count=1 2>&1 | grep -E "^(--- |ok|FAIL)"
```
Esperado: `...RawValueIntoRefField...` e `...RawValueIntoRefElement...` FAIL (hoje gravam sem erro); os outros dois PASS.

- [ ] **Step 3: Implementar**

Em `ref_slots.go`, acrescentar:
```go
// structRefFieldTypeName devolve o tipo declarado do campo (`ref Node`) a
// partir de ConstructorType.Params — so para mensagens de erro (caminho
// frio); sem ConstructorType valido cai em "reference field 'nome'".
func structRefFieldTypeName(definition *value.ObjStruct, name string) string {
	if schema, ok := validStructConstructorType(definition); ok {
		for i, field := range definition.Fields {
			if field == name && i < len(schema.Params) && schema.Params[i] != nil {
				return schema.Params[i].String()
			}
		}
	}
	return fmt.Sprintf("reference field '%s'", name)
}

// refSlotWriteError e o gemeo dinamico do erro de compilacao
// "cannot assign T to ref T" (spec §2.3), usado quando a escrita chega por
// fronteira dinamica (base `any`).
func refSlotWriteError(expected string, val value.Value) string {
	return fmt.Sprintf("cannot assign %s to %s", runtimeTypeName(val), expected)
}
```

Em `executor.go`, `OP_SET_PROPERTY`, logo após obter `instance` (antes do comentário `// RC: retain-antes-de-release`):
```go
			// Guard do slot ref (spec §6.3): via base tipada o compilador ja
			// rejeitou; aqui so dispara em fronteira dinamica (`any`).
			if instance.Struct.FieldIsRef(name) && val.Type != value.VAL_REF && val.Type != value.VAL_NULL {
				return vm.runtimeError(c, ip, "%s", refSlotWriteError(structRefFieldTypeName(instance.Struct, name), val))
			}
```

Em `OP_SET_INDEX`, branch do array, logo após o bounds check (`if idx < 0 || idx >= len(arr.Elements) {...}`):
```go
					if val.Type != value.VAL_REF && val.Type != value.VAL_NULL && arrayElementIsRefSlot(arr) {
						return vm.runtimeError(c, ip, "%s", refSlotWriteError(arr.RuntimeType.Load().Element.String(), val))
					}
```
Branch do map, logo após resolver `key` (antes do comentário `// RC: so libera o velho se a chave ja existia`):
```go
					if val.Type != value.VAL_REF && val.Type != value.VAL_NULL && mapValueIsRefSlot(mapObj) {
						return vm.runtimeError(c, ip, "%s", refSlotWriteError(mapObj.RuntimeType.Load().Value.String(), val))
					}
```
(A ordem `val.Type != ...` primeiro deixa o `Load()` atômico só para escritas não-ref, que são a maioria — custo ~1 ns; ver §6.3.)

- [ ] **Step 4: Rodar e ver passar; benchmark rápido de sanidade**

```bash
cd "$W" && go test ./internal/vm -count=1 2>&1 | grep -E "^(ok|FAIL|--- FAIL)"
cd "$W" && go test ./internal/vm -run 'XXX' -bench 'Array|Index|Property|Set' -benchtime=200ms 2>&1 | grep -E "^(Benchmark|ok|PASS)" | head -20
```
Esperado: `ok`; benchmarks (se existirem com esses nomes) na mesma ordem de grandeza do `develop` (comparar rodando o mesmo comando no diretório principal em `develop` se houver dúvida; regressão >5% em escrita de array → aplicar a alternativa de §6.3 e registrar).

- [ ] **Step 5: Commit**

```bash
cd "$W" && git add internal/vm/ref_slots.go internal/vm/executor.go internal/vm/ref_slot_invariant_test.go && git commit -q -m "fix(vm): OP_SET_PROPERTY/OP_SET_INDEX rejeitam valor cru em slot ref T via base any — gêmeo dinâmico de 'cannot assign T to ref T' (#50 rota 5)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" && git log --oneline -1
```

---

### Task 6: `json_loads` — slot `ref T` nulo recebe célula heap + ref (opção a)

**Files:**
- Modify: `internal/vm/json_population.go` (`prepareJSONMutation` ~26-45; `buildTypedJSONValue` `case value.TYPE_REF` ~300-304; + `buildReferentCell`)
- Modify: `internal/vm/native_signatures_test.go:533-545` (subteste "fill null slot with referent value")
- Create: `internal/vm/json_ref_cell_test.go`

**Interfaces:**
- Consumes: `value.NewClosedUpvalue` (Task 1), `buildTypedJSONValue`.
- Produces: `func buildReferentCell(referent *value.RuntimeTypeInfo, data interface{}) (value.Value, bool)` — devolve `VAL_REF{REF_UPVALUE}` para uma célula nova que possui o `T` construído.

- [ ] **Step 1: Renomear/reforçar o subteste existente e escrever os novos**

Em `native_signatures_test.go`, substituir o subteste:
```go
	t.Run("fill null slot with referent value", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let target: (ref int)[] = [null]
let ok: bool = json_loads("[42]", target)
if ok then
    test_report(target[0])
else
    test_report(999)
end`)
		testExpectedObject(t, 42, got)
	})
```
por
```go
	// Issue #50 Parte 3 (opcao a): slot `ref T` nulo recebe uma CELULA heap
	// nova com o referente + ref para ela — o analogo de `let novo = T;
	// slot = ref novo`. A sonda `type(ref viz)` distingue ref ("ref") de
	// valor cru (erro do marcador); `type(slot)` nao serve (auto-deref).
	t.Run("null ref slot gets a fresh referent cell", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
func le(r: ref int) -> int
    return r
end
let target: (ref int)[] = [null]
let ok: bool = json_loads("[42]", target)
let viz: ref int = target[0]
if ok && type(ref viz) == "ref" && *viz == 42 && le(target[0]) == 42 then
    test_report(42)
else
    test_report(999)
end`)
		testExpectedObject(t, 42, got)
	})
```

`internal/vm/json_ref_cell_test.go` (Write tool):
```go
package vm

// Contrato de json_loads para slots `ref T` (spec 2026-08-20-ref-slot-
// invariant §5.1): slot ja apontando escreve ATRAVES; payload null limpa;
// slot nulo/novo com payload nao-nulo ganha celula heap + ref; alvo direto
// nulo devolve false.

import "testing"

func TestJSONLoadsNewRefElementIsAReference(t *testing.T) {
	got := runTypedFunctionProgram(t, `
struct Pair
    a: int
    b: int
end
let target: (ref Pair)[] = []
let ok: bool = json_loads("[{\"a\":3,\"b\":4}]", target)
let viz: ref Pair = target[0]
if ok && type(ref viz) == "ref" && viz.a * 10 + viz.b == 34 then
    test_report(34)
else
    test_report(999)
end`)
	testExpectedObject(t, 34, got)
}

func TestJSONLoadsNullRefFieldViaOwnerGetsCell(t *testing.T) {
	got := runTypedFunctionProgram(t, `
struct Holder
    child: ref int
end
let h: Holder = Holder(null)
let ok: bool = json_loads("{\"child\": 5}", h)
let viz: ref int = h.child
if ok && type(ref viz) == "ref" && *viz == 5 then
    test_report(5)
else
    test_report(999)
end`)
	testExpectedObject(t, 5, got)
}

func TestJSONLoadsRefSlotAlreadyPointingWritesThrough(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let backing: int = 7
let target: (ref int)[] = [ref backing]
let ok: bool = json_loads("[42]", target)
let viz: ref int = target[0]
if ok && backing == 42 && *viz == 42 then
    test_report(42)
else
    test_report(999)
end`)
	testExpectedObject(t, 42, got)
}

func TestJSONLoadsDirectNullRefIntSlotReturnsFalse(t *testing.T) {
	got := runTypedFunctionProgram(t, `
struct Holder
    child: ref int
end
let h: Holder = Holder(null)
let ok: bool = json_loads("5", h.child)
if !ok && h.child == null then
    test_report(1)
else
    test_report(0)
end`)
	testExpectedObject(t, 1, got)
}

// A celula e possuidora (Owners=1): mutar uma copia por valor do referente
// nao altera o que o slot aponta (o `let` da copia leva Owners a 2 e clona).
func TestJSONLoadsCellOwnsItsReferent(t *testing.T) {
	got := runTypedFunctionProgram(t, `
struct Pair
    a: int
    b: int
end
let target: (ref Pair)[] = [null]
let ok: bool = json_loads("[{\"a\":1,\"b\":2}]", target)
let copia: Pair = target[0]
copia.a = 99
let viz: ref Pair = target[0]
if ok && viz.a == 1 then
    test_report(1)
else
    test_report(999)
end`)
	testExpectedObject(t, 1, got)
}
```

- [ ] **Step 2: Rodar e ver falhar**

```bash
cd "$W" && go test ./internal/vm -run 'TestTypedJSONLoadsAcceptsCompatibleReferenceElementPayloads|TestJSONLoads(NewRefElement|NullRefFieldViaOwner|RefSlotAlreadyPointing|DirectNullRefInt|CellOwns)' -count=1 2>&1 | grep -E "^(--- |    --- |ok|FAIL|.*Runtime error)"
```
Esperado: "null ref slot gets a fresh referent cell", `NewRefElementIsAReference`, `NullRefFieldViaOwnerGetsCell`, `CellOwnsItsReferent` FAIL (hoje o slot recebe valor cru: `type(ref viz)` morre com "reference target marker requires a reference"); `RefSlotAlreadyPointingWritesThrough` e `DirectNullRefIntSlotReturnsFalse` PASS.

- [ ] **Step 3: Implementar**

Em `json_population.go`, substituir o bloco `if schema != nil && schema.Kind == value.TYPE_REF { ... }` de `prepareJSONMutation` por:
```go
	if schema != nil && schema.Kind == value.TYPE_REF {
		if data == nil {
			if set == nil {
				return nil, false
			}
			return func() { set(value.NewNull()) }, true
		}
		if current.Type == value.VAL_REF {
			ref, ok := current.Obj.(*value.ObjRef)
			if !ok || ref == nil {
				return nil, false
			}
			stored, store, ok := jsonReferenceStorage(vm, ref)
			if !ok {
				return nil, false
			}
			return prepareJSONMutation(vm, stored, schema.Element, data, store)
		}
		// Slot `ref T` nulo com payload nao-nulo: celula heap nova + ref
		// (spec 2026-08-20-ref-slot-invariant §5). Valor cru no slot e estado
		// impossivel depois da #50 — recusa em vez de sobrescrever.
		if current.Type != value.VAL_NULL || set == nil {
			return nil, false
		}
		cell, ok := buildReferentCell(schema.Element, data)
		if !ok {
			return nil, false
		}
		return func() { set(cell) }, true
	}
```
Em `buildTypedJSONValue`, substituir
```go
	case value.TYPE_REF:
		if data == nil {
			return value.NewNull(), true
		}
		return buildTypedJSONValue(schema.Element, data)
```
por
```go
	case value.TYPE_REF:
		if data == nil {
			return value.NewNull(), true
		}
		return buildReferentCell(schema.Element, data)
```
E acrescentar (logo após `buildTypedJSONValue`):
```go
// buildReferentCell constroi o T pelo schema do referente e devolve uma ref
// para uma CELULA heap nova que o possui — o analogo exato de
// `let novo: T = ...; slot = ref novo` depois que o frame fecha (caixa
// REF_UPVALUE fechada, Owners do valor = 1, como closeUpvalue deixa).
func buildReferentCell(referent *value.RuntimeTypeInfo, data interface{}) (value.Value, bool) {
	built, ok := buildTypedJSONValue(referent, data)
	if !ok {
		return value.Value{}, false
	}
	value.Retain(built) // RC: a celula e o dono duravel do referente
	cell := value.NewClosedUpvalue(built)
	return value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_UPVALUE, Upvalue: cell}}, true
}
```
Atualizar o comentário antigo `// Null and legacy-filled ref slots retain their declared referent type.` (removido junto com a linha que ele anotava).

- [ ] **Step 4: Rodar e ver passar**

```bash
cd "$W" && go test ./internal/vm -count=1 2>&1 | grep -E "^(ok|FAIL|--- FAIL|    --- FAIL)"
```
Esperado: `ok` (inclui `TestTypedJSONLoadsPreservesReferenceElementTypeForNullSlot`, "json null clears/creates reference slot", `TestTypedJSONLoadsRequiresAllFieldsWhenBuildingNewStruct`).

- [ ] **Step 5: Commit**

```bash
cd "$W" && git add internal/vm/json_population.go internal/vm/native_signatures_test.go internal/vm/json_ref_cell_test.go && git commit -q -m "feat(vm): json_loads preenche slot ref T nulo com célula heap + ref (opção a da #50 Parte 3); valor cru no slot recusa

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" && git log --oneline -1
```

---

### Task 7: RC dos builders JSON (`json_loads`/`json_parse`) — contêiner é dono; escrita através de ref pelo funil único

**Files:**
- Modify: `internal/vm/json_population.go` (`prepareJSONArrayMutation`, `prepareJSONMapMutation`, `prepareJSONStructMutation`, `buildTypedJSONValue` array/map/struct, `dynamicJSONValue` array/map, `jsonReferenceStorage`), `internal/vm/builtins_json.go` (`goValToNoxy` ~181-192, `populateTarget`/`populateRef` ~198-242)
- Create: `internal/vm/json_rc_test.go`

**Interfaces:**
- Consumes: `retainingArray(elements []value.Value) value.Value`, `retainingMap(data map[string]value.Value) value.Value` (já existem em `builtins_call_result.go`, mesmo pacote), `vm.storeReferenceValue(input, updated value.Value) error`, `value.Retain/Release`.
- Produces: `func jsonStoreThrough(vm *VM, target value.Value) jsonSetter`; `populateRef(vm *VM, target value.Value, ref *value.ObjRef, data interface{}) bool` (assinatura nova).

- [ ] **Step 1: Escrever os testes (Write tool)**

`internal/vm/json_rc_test.go`:
```go
package vm

// Under-count de RC dos builders JSON (achado lateral da #50, spec §5.3):
// valores construidos por json_loads/json_parse entravam nos conteineres sem
// Retain e substituicoes nao soltavam o ocupante anterior, entao uma copia
// por valor (`let p: Pair = t[0]`) chegava a Owners=1, IsShared falso, e a
// mutacao da copia acontecia no lugar — vazando para o conteiner. O modelo a
// espelhar e o do bytecode: todo conteiner que guarda um composto e um dono.

import "testing"

const jsonRCPrelude = `
struct Pair
    a: int
    b: int
end
`

func TestJSONLoadsNewArrayElementIsOwnedByArray(t *testing.T) {
	got := runTypedFunctionProgram(t, jsonRCPrelude+`
let t: Pair[] = []
let ok: bool = json_loads("[{\"a\":1,\"b\":2}]", t)
let p: Pair = t[0]
p.a = 99
if ok then
    test_report(t[0].a)
else
    test_report(999)
end`)
	testExpectedObject(t, 1, got)
}

func TestJSONLoadsNewMapValueIsOwnedByMap(t *testing.T) {
	got := runTypedFunctionProgram(t, jsonRCPrelude+`
let m: map[string, Pair] = {}
let ok: bool = json_loads("{\"k\":{\"a\":1,\"b\":2}}", m)
let p: Pair = m["k"]
p.a = 99
if ok then
    test_report(m["k"].a)
else
    test_report(999)
end`)
	testExpectedObject(t, 1, got)
}

func TestJSONLoadsNewStructFieldIsOwnedByInstance(t *testing.T) {
	got := runTypedFunctionProgram(t, jsonRCPrelude+`
struct Outer
    inner: Pair
end
let o: Outer = Outer(null)
let ok: bool = json_loads("{\"inner\":{\"a\":1,\"b\":2}}", o)
let p: Pair = o.inner
p.a = 99
if ok then
    test_report(o.inner.a)
else
    test_report(999)
end`)
	testExpectedObject(t, 1, got)
}

func TestJSONLoadsReplacedElementReleasesOldAndRetainsNew(t *testing.T) {
	got := runTypedFunctionProgram(t, jsonRCPrelude+`
let t: Pair[] = [Pair(0, 0)]
let antigo: Pair = t[0]
let ok: bool = json_loads("[{\"a\":1,\"b\":2}]", t)
let p: Pair = t[0]
p.a = 99
antigo.a = 55
if ok then
    test_report(t[0].a * 100 + antigo.a)
else
    test_report(999)
end`)
	// t[0] mutado no lugar (a=1 → json atualizou o proprio Pair(0,0), que
	// tinha 2 donos: array + `antigo`) — p (3o dono) clona ao escrever;
	// `antigo` tambem clona: 1*100 + 55.
	testExpectedObject(t, 155, got)
}

func TestJSONLoadsShrunkArrayReleasesDroppedElements(t *testing.T) {
	got := runTypedFunctionProgram(t, jsonRCPrelude+`
let t: Pair[] = [Pair(0, 0), Pair(5, 5)]
let solto: Pair = t[1]
let ok: bool = json_loads("[{\"a\":1,\"b\":2}]", t)
solto.a = 77
if ok && length(t) == 1 then
    test_report(solto.a)
else
    test_report(999)
end`)
	// Depois do encolhimento `solto` e o unico dono de Pair(5,5): muta no
	// lugar sem clonar (Owners voltou a 1 — o array soltou).
	testExpectedObject(t, 77, got)
}

func TestJSONLoadsThroughRefIntoVariableRetains(t *testing.T) {
	got := runTypedFunctionProgram(t, jsonRCPrelude+`
let backing: Pair = null
let t: (ref Pair)[] = [ref backing]
let ok: bool = json_loads("[{\"a\":1,\"b\":2}]", t)
let p: Pair = backing
p.a = 99
if ok then
    test_report(backing.a)
else
    test_report(999)
end`)
	testExpectedObject(t, 1, got)
}

func TestJSONLoadsDynamicTopLevelRetains(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let d: any = [1, 2]
let ok: bool = json_loads("[5, 6]", d)
let e: any = d
e[0] = 99
if ok then
    test_report(d[0])
else
    test_report(999)
end`)
	testExpectedObject(t, 5, got)
}

func TestJSONParseBuildsOwnedChildren(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let d: any = json_parse("{\"k\": [1, 2]}")
let e: any = d["k"]
e[0] = 99
test_report(d["k"][0])`)
	testExpectedObject(t, 1, got)
}
```

- [ ] **Step 2: Rodar e ver falhar**

```bash
cd "$W" && go test ./internal/vm -run 'TestJSONLoads(NewArrayElementIsOwned|NewMapValueIsOwned|NewStructFieldIsOwned|ReplacedElement|ShrunkArray|ThroughRefInto|DynamicTopLevel)|TestJSONParseBuildsOwnedChildren' -count=1 2>&1 | grep -E "^(--- |ok|FAIL)"
```
Esperado: `NewArrayElementIsOwnedByArray`, `NewMapValueIsOwnedByMap`, `NewStructFieldIsOwnedByInstance`, `ThroughRefIntoVariableRetains`, `DynamicTopLevelRetains`, `JSONParseBuildsOwnedChildren` FAIL (veem 99); `ReplacedElement...` e `ShrunkArray...` podem passar ou falhar hoje — anotar o resultado real no commit.

- [ ] **Step 3: Implementar — builders**

Em `json_population.go`, `buildTypedJSONValue`:
- `case value.TYPE_ARRAY`: trocar `array := value.NewArray(elements)` por `array := retainingArray(elements) // RC: o array e dono duravel de cada elemento (espelha OP_ARRAY)`.
- `case value.TYPE_MAP`: depois do laço que preenche `mapData` e antes de `mapObject.Replace(mapData)`: 
  ```go
  		for _, created := range mapData {
  			value.Retain(created) // RC: o map e dono duravel de cada valor (espelha OP_MAP)
  		}
  ```
- `case value.TYPE_STRUCT`: depois do laço que preenche `fields` e antes de `return instance, true`:
  ```go
  		for _, created := range fields {
  			value.Retain(created) // RC: campo e dono duravel (espelha o construtor)
  		}
  ```
`dynamicJSONValue`:
- `case []interface{}`: `return retainingArray(elements), true`.
- `case map[string]interface{}`: antes de `mapObject.Replace(dataMap)`: `for _, converted := range dataMap { value.Retain(converted) }`.

Em `builtins_json.go`, `goValToNoxy`: `return retainingArray(arr)` e `return retainingMap(m)`.

- [ ] **Step 4: Implementar — setters de substituição e posições novas/descartadas**

Substituir `prepareJSONArrayMutation` por:
```go
func prepareJSONArrayMutation(vm *VM, array *value.ObjArray, elementSchema *value.RuntimeTypeInfo, data interface{}) (jsonCommit, bool) {
	dataArray, ok := data.([]interface{})
	if !ok {
		return nil, false
	}
	oldElements := array.Elements
	newElements := make([]value.Value, len(dataArray))
	commits := make([]jsonCommit, 0, len(dataArray))
	added := make([]value.Value, 0)
	for i, item := range dataArray {
		index := i
		if i < len(oldElements) {
			previous := oldElements[i]
			newElements[i] = previous
			commit, ok := prepareJSONMutation(vm, previous, elementSchema, item, func(updated value.Value) {
				// RC: a posicao troca de ocupante — retain-novo antes de
				// release-velho (so roda quando o filho foi SUBSTITUIDO; a
				// mutacao in-place de um filho composto nao passa por aqui).
				value.Retain(updated)
				newElements[index] = updated
				value.Release(previous)
			})
			if !ok {
				return nil, false
			}
			commits = append(commits, commit)
			continue
		}
		var created value.Value
		if elementSchema == nil {
			created, ok = dynamicJSONValue(item)
		} else {
			created, ok = buildTypedJSONValue(elementSchema, item)
		}
		if !ok {
			return nil, false
		}
		newElements[i] = created
		added = append(added, created)
	}
	return func() {
		for _, commit := range commits {
			commit()
		}
		// RC: posicoes novas ganham o array como dono; posicoes descartadas
		// (payload menor que o array) perdem.
		for _, created := range added {
			value.Retain(created)
		}
		for j := len(dataArray); j < len(oldElements); j++ {
			value.Release(oldElements[j])
		}
		array.Elements = newElements
	}, true
}
```
Substituir `prepareJSONMapMutation` por:
```go
func prepareJSONMapMutation(vm *VM, mapping *value.ObjMap, valueSchema *value.RuntimeTypeInfo, data interface{}) (jsonCommit, bool) {
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return nil, false
	}
	newData := mapping.Snapshot()
	commits := make([]jsonCommit, 0, len(dataMap))
	added := make([]value.Value, 0)
	for key, item := range dataMap {
		mapKey := key
		if current, exists := newData[mapKey]; exists {
			previous := current
			commit, ok := prepareJSONMutation(vm, current, valueSchema, item, func(updated value.Value) {
				// RC: troca de ocupante — retain-novo antes de release-velho
				value.Retain(updated)
				newData[mapKey] = updated
				value.Release(previous)
			})
			if !ok {
				return nil, false
			}
			commits = append(commits, commit)
			continue
		}
		var created value.Value
		if valueSchema == nil {
			created, ok = dynamicJSONValue(item)
		} else {
			created, ok = buildTypedJSONValue(valueSchema, item)
		}
		if !ok {
			return nil, false
		}
		newData[mapKey] = created
		added = append(added, created)
	}
	return func() {
		for _, commit := range commits {
			commit()
		}
		for _, created := range added {
			value.Retain(created) // RC: chave nova — o map vira dono
		}
		mapping.Replace(newData)
	}, true
}
```
Em `prepareJSONStructMutation`, trocar o setter `func(updated value.Value) { newFields[name] = updated }` por:
```go
		previous := current
		commit, ok := prepareJSONMutation(vm, current, fieldSchema, dataValue, func(updated value.Value) {
			// RC: troca de ocupante do campo — retain-novo antes de release-velho
			value.Retain(updated)
			newFields[name] = updated
			value.Release(previous)
		})
```

- [ ] **Step 5: Implementar — escrita através de ref pelo funil único**

Em `json_population.go`, substituir `jsonReferenceStorage` por:
```go
// jsonStoreThrough devolve o setter que escreve ATRAVES de uma ref pelo funil
// unico de escrita via ref (storeReferenceValue): retain-novo/release-velho,
// consciencia de caixa emprestada (refStorageBorrows) e reaponte da lista de
// posse do frame (retargetOwnedSlot). O erro e descartado: referenceStorage
// acabou de validar o alvo, e uma falha aqui so viria de invalidacao
// concorrente, que a escrita crua tampouco detectaria.
func jsonStoreThrough(vm *VM, target value.Value) jsonSetter {
	return func(updated value.Value) { _ = vm.storeReferenceValue(target, updated) }
}

func jsonReferenceStorage(vm *VM, ref *value.ObjRef) (value.Value, jsonSetter, bool) {
	stored, exists, store, err := vm.referenceStorage(ref)
	if err != nil || !exists || store == nil {
		return value.Value{}, nil, false
	}
	return stored, jsonStoreThrough(vm, value.Value{Type: value.VAL_REF, Obj: ref}), true
}
```
Em `builtins_json.go`, substituir `populateTarget` e `populateRef` por:
```go
func populateTarget(vm *VM, target value.Value, data interface{}) bool {
	if target.Type == value.VAL_REF {
		ref, ok := target.Obj.(*value.ObjRef)
		if !ok || ref == nil {
			return false
		}
		return populateRef(vm, target, ref, data)
	} else if target.Type == value.VAL_OBJ {
		// Populate Object In-Place
		return populateObj(vm, target, data)
	}
	// Cannot populate primitive value passed by value
	return false
}

// populateRef popula o alvo atraves da ref. Toda substituicao do valor
// apontado passa por jsonStoreThrough (storeReferenceValue), nunca pelo
// store cru de referenceStorage — spec 2026-08-20-ref-slot-invariant §5.3.
func populateRef(vm *VM, target value.Value, ref *value.ObjRef, data interface{}) bool {
	currentVal, exists, store, err := vm.referenceStorage(ref)
	if err != nil || !exists || store == nil {
		return false
	}
	set := jsonStoreThrough(vm, target)
	if ref.JSONDynamic.Load() {
		replacement, ok := dynamicJSONValue(data)
		if !ok {
			return false
		}
		set(replacement)
		return true
	}
	commit, ok := prepareJSONMutation(vm, currentVal, ref.TargetType.Load(), data, set)
	if !ok {
		return false
	}
	commit()
	return true
}
```

- [ ] **Step 6: Rodar e ver passar**

```bash
cd "$W" && go vet ./internal/vm && go test ./internal/vm -count=1 2>&1 | grep -E "^(ok|FAIL|--- FAIL|    --- FAIL)"
```
Esperado: `ok` — todos os testes novos e os antigos de JSON (`builtins_json_test.go`, `native_signatures_test.go`), call_result, e RC.

- [ ] **Step 7: Commit**

```bash
cd "$W" && git add internal/vm/json_population.go internal/vm/builtins_json.go internal/vm/json_rc_test.go && git commit -q -m "fix(vm): builders JSON contam posse — contêiner é dono dos filhos, substituição faz retain/release, escrita através de ref via storeReferenceValue (json_loads/json_parse; under-count achado na #50)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" && git log --oneline -1
```

---

### Task 8: Docs, CHANGELOG 0.10.0, versão

**Files:**
- Modify: `docs/NOXY_LANGUAGE_SPEC.md` (§2.3 após a tabela ~432; §4.2 após o bloco do `append_node` ~570; §5 após o exemplo `Node` ~675; §12 após a tabela de módulos ~1625), `docs/JSON_SUPPORT.md` (nova seção antes de `## Type Mapping` ~104), `CHANGELOG.md` (nova seção no topo, antes de `## [0.9.1]`), `README.md:1` e `:79`, `internal/version/version.go`

- [ ] **Step 1: Spec — §2.3 (Edit tool, inserir logo após a tabela "Summary Table: Type-Based Assignment", antes de `#### Memory Safety`)**

```markdown
The field rules apply identically when the assignment **base** is itself a
reference: with `node: ref Node`, `node.valor = "texto"` is `type mismatch in
field assignment: expected int, got string`, and `node.proximo = Node(9, null)`
is `cannot assign Node to ref Node` — the compiler resolves the field through
the dereferenced base and checks it exactly as it checks `a.valor` /
`a.proximo` with `a: Node`. For a `ref T` field, array element, or map value
the error names the two legitimate paths:

```
hint: to point the field at a new value, bind it to a variable first and use 'x.proximo = ref novo'; to overwrite the referenced value use '*x.proximo = ...'
```
```

- [ ] **Step 2: Spec — §4.2 (inserir logo após o bloco de código do `append_node`)**

```markdown
A slot declared `ref T` always holds a reference or `null`, and the runtime
never wraps anything else: should a slot hold a raw `T` (which no Noxy program
can produce), forwarding it is the explicit error `reference slot 'field'
holds a non-reference value`. Through a base typed `any` the same forwarding
applies — `ref a.proximo`, `f(a.proximo)`, `json_loads(s, a.proximo)` with
`a: any` forward the stored reference or `null` exactly as the typed base does
— and writing a raw `T` into a `ref T` field, element, or map value through
`any` is the runtime error `cannot assign T to ref T`.
```

- [ ] **Step 3: Spec — §5 (inserir após o exemplo `struct Node ... next: ref Node end`)**

```markdown
A `ref` field is filled by rebinding it to a variable (`let novo: Node = ...;
node.next = ref novo`) or cleared with `null`; assigning a raw `Node` to it is
a compile error whether `node` is a `Node` or a `ref Node` (§2.3, §4.2).
```

- [ ] **Step 4: Spec — §12 (inserir após a tabela de módulos, antes de `### Strings`)**

```markdown
### JSON

`json_dumps`, `json_parse` and `json_loads` are documented in
[`JSON_SUPPORT.md`](JSON_SUPPORT.md). `json_loads(text, target)` populates an
existing typed target in place and returns `false` (with no partial writes)
when the payload does not fit. For a slot declared `ref T` **inside** the
target (array element, struct field, map value): a slot that already holds a
reference is written **through** it; a JSON `null` stores `null`; a non-null
payload for a slot that is `null` (or for a new element/field) builds the `T`
from the referent schema, allocates a fresh heap cell that owns it, and stores
a reference to that cell — afterwards `let viz: ref T = slot; type(ref viz)`
is `"ref"` and `*viz` reads the value. A `ref T` field or element passed
**directly** as the target while it is `null` arrives as `null` (§4.2) and
`json_loads` returns `false`; pass the owner instead (`json_loads(text, h)`).
```

- [ ] **Step 5: `docs/JSON_SUPPORT.md` (inserir antes de `## Type Mapping`)**

```markdown
### Reference slots (`ref T`)

A slot declared `ref T` inside the target — `(ref T)[]` element, `ref T` struct
field, `map[K, ref T]` value — always ends up holding a reference or `null`:

| Slot before | JSON payload | Result |
| :--- | :--- | :--- |
| reference | non-null | written **through** the reference (the referent changes) |
| reference or `null` | `null` | slot becomes `null` |
| `null`, or a new element/field | non-null | `T` is built from the referent schema, a fresh heap cell owns it, and the slot gets a reference to the cell — like `let novo: T = ...; slot = ref novo` |

```noxy
let target: (ref int)[] = [null]
json_loads("[42]", target)
let viz: ref int = target[0]
print(type(ref viz))   // ref
print(*viz)            // 42
```

Passing a `null` `ref T` field or element **directly** as the target
(`json_loads(s, h.child)`) forwards the stored `null`; there is no slot behind
it, so the call returns `false`. Pass the owner (`json_loads(s, h)`) or point
the slot first.
```

- [ ] **Step 6: CHANGELOG (inserir no topo, após a linha `# Changelog` e a linha em branco)**

```markdown
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
  observáveis não mudam (1 dono nos dois casos). Dentro de laços com `break`,
  lembrar da issue #52: prefira a forma recursiva.
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

```

- [ ] **Step 7: Versão**

- `internal/version/version.go`: `const Version = "v0.10.0"`.
- `README.md` linha 1: `[![noxy 0.10.0](https://img.shields.io/badge/noxy-0.10.0-blue)](CHANGELOG.md)`; linha ~79: `Noxy REPL v0.10.0`.
  ```bash
  cd "$W" && grep -n "0\.9\.1" README.md internal/version/version.go   # só essas 2-3 linhas devem aparecer; trocar com Edit tool
  ```

- [ ] **Step 8: Conferir e commitar**

```bash
cd "$W" && git diff --numstat && go build ./... && go test ./internal/version -count=1 2>&1 | grep -E "^(ok|FAIL|\?)"
cd "$W" && git add docs/NOXY_LANGUAGE_SPEC.md docs/JSON_SUPPORT.md CHANGELOG.md README.md internal/version/version.go && git commit -q -m "chore(version): noxy v0.10.0 — invariante do slot ref T (checagem via base ref, json_loads célula+ref, fim do shim, RC dos builders JSON); spec §2.3/§4.2/§5/§12 e JSON_SUPPORT

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>" && git log --oneline -1
```

---

### Task 9: Verificação completa e fechamento da branch

**Files:** nenhum (só verificação; correções pontuais se algo falhar, com commit próprio).

- [ ] **Step 1: Formatação e vet**

```bash
cd "$W" && for f in $(git diff --name-only develop..HEAD -- '*.go'); do printf '%s: ' "$f"; sed 's/\r$//' "$f" | gofmt -l | sed 's/<standard input>/PRECISA gofmt/'; echo; done
cd "$W" && go vet ./... 2>&1 | grep -v "^#" | head -20
```
Esperado: nenhum `PRECISA gofmt`; `go vet` sem saída. (Se um arquivo precisar: `sed 's/\r$//' f | gofmt > /tmp/x` **não** — use `gofmt -d` para ver o trecho e corrija com Edit tool, preservando CRLF; confira `git diff --numstat`.)

- [ ] **Step 2: Suíte Go completa, sem tail**

```bash
cd "$W" && go test ./... -count=1 > "$S/gotest.log" 2>&1; echo "exit=$?"; grep -E "^(FAIL|--- FAIL|panic:)" "$S/gotest.log" | head -40; grep -c "^ok" "$S/gotest.log"
```
Esperado: `exit=0`, nenhum `FAIL`. Qualquer falha: investigar com `superpowers:systematic-debugging` (não ajustar asserções de RC — §4.2).

- [ ] **Step 3: Runner concorrente dos exemplos**

```bash
cd "$W" && go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx 2>&1 | grep -E "FAIL|Total|Passou|Falhou"
```
Esperado: `Falhou: 0` (flakes conhecidos: `sqlite_*` compartilham `loja.db` — rerodar o arquivo isolado se for só isso).

- [ ] **Step 4: Diff de saída dos exemplos contra o baseline**

```bash
cd "$W" && bash "$S/capture_examples.sh" "$S/branch" 2>&1 | tail -1
diff -rq "$S/baseline" "$S/branch" | sort
```
Esperado: nenhuma diferença. Para cada arquivo que diferir: `diff "$S/baseline/X.out" "$S/branch/X.out"`; aceitar só não-determinismo evidente (timestamps, random, ordem de goroutines — confirmar rodando o baseline de novo para aquele arquivo no diretório principal em `develop` com `go run`); qualquer outra diferença é regressão a corrigir.

- [ ] **Step 5: Grep de pendências**

```bash
cd "$W" && grep -rn "issue #50\|#50\|legacy-filled\|fill-null-slot\|leniência\|leniencia" internal/ --include=*.go | grep -v _test | head
```
Esperado: só referências históricas/explicativas (nenhum "shim temporario: sai quando a issue #50"). Ajustar comentários residuais se houver (commit `docs(vm): ...`).

- [ ] **Step 6: Revisão final e entrega**

Invocar `superpowers:requesting-code-review` (revisor com o diff `develop..HEAD` e o spec) e tratar os achados com `superpowers:receiving-code-review`. Depois `superpowers:finishing-a-development-branch` — **opção "manter a branch, não abrir PR ainda"**: a PR #51 segue aberta no GitHub e o `develop` local não foi enviado; relatar ao usuário: commits da branch (`git log --oneline develop..HEAD`), resultado da suíte/runner/diff, e o texto de PR pronto (template Summary/Components/Test Plan do `~/.claude/CLAUDE.md`, título `fix/ref-slot-invariant - <descrição PT>`, base `develop`, label "not available to review") para ele abrir quando quiser.

---

## Self-review do plano (feito ao escrever)

- **Cobertura do spec:** §3 → Task 2; §4.1 → Task 3; §4.2 → Task 2 (migração + nota de posse no CHANGELOG, Task 8); §5.1/5.2 → Task 6; §5.3 → Task 7 (inclui o item (ii) do revisor: `jsonReferenceStorage` via `storeReferenceValue`); §6.1 → Tasks 1/2/4 (`RefFields` no compilador e no builder JSON + teste de consistência); §6.2 → Task 4 (inclui o detalhe de `OP_REF_INDEX`); §6.3 → Task 5; §6.4 → sem mudança (verificado pelo revisor); §7 → Task 8; §8 → testes distribuídos + Task 9; §9 → Task 9 Step 6 (git, sem push).
- **Placeholders:** nenhum "TBD"; todo passo de código traz o código; mensagens exatas nas Global Constraints.
- **Consistência de nomes:** `forwardRefSlot`, `describeRefSlotIndex`, `arrayElementIsRefSlot`, `mapValueIsRefSlot` (Task 4) usados em Task 5; `refSlotWriteError`/`structRefFieldTypeName` (Task 5); `buildReferentCell` (Task 6); `jsonStoreThrough` e `populateRef(vm, target, ref, data)` (Task 7); `NewClosedUpvalue`/`FieldIsRef`/`RefFields` (Task 1) em Tasks 2/4/5/6.
