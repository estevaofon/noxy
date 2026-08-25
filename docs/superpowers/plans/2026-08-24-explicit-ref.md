# `ref` explícito (issue #82) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Uma referência nunca é criada nem lida sem `ref` ou `*` no código-fonte: `*r` é a única leitura, `ref x` é obrigatório em todo call site (builtins inclusos), `.`/`[]` são o único atalho; somem auto-deref, exceção 1, exceção 2, conversão contextual e forwarding.

**Architecture:** Mudança quase toda no compilador (`internal/compiler`): os 16 sites de `OP_DEREF` implícito viram erros com hint (helper `rejectRefRead`), `compileReferenceArgumentValue` deixa de aceitar operando já `ref T` (R1) e os call sites passam por um helper novo `compileRefArgument` (R5). Na VM só dois ajustes: `OP_DEREF`/`OP_DEREF_MUT` deixam de tolerar não-ref e `OP_REF_PROPERTY`/`OP_REF_INDEX` erram sobre slot que já guarda referência. Corpus migrado por codemod (4 builtins) + hints do compilador; a forma nova é subconjunto da linguagem v0.18.0, então o binário base roda o corpus migrado para o diff de saída.

**Tech Stack:** Go 1.25 (`go test ./...`), Noxy (corpus `noxy_examples/run_all_tests_concurrent.nx`), `gh` CLI, PowerShell 7 (`benchmarks/interleaved_compare.ps1`), Python 3 só para o codemod (scratchpad, não commitado).

**Spec:** `docs/superpowers/specs/2026-08-24-explicit-ref-design.md` — regras R1–R10 (§3), tabela antes × depois (§3.1), diagnósticos (§4), sites (§5).

## Global Constraints

- Branch `feature/issue-82-explicit-ref` (já criada a partir de `main` @ `3c579ae`); PR para `main`. Commits Conventional-Commits com escopo (`feat(compiler):`, `fix(vm):`, `docs(spec):`, `chore(version):`), terminando com `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- TDD: cada diagnóstico entra RED (teste do erro **e** do hint) antes de remover o caminho implícito. Testes de R2 na VM verificam pelo **valor em runtime** (`captureVMSource` + `test_report`), não só pelo chunk.
- Todo erro novo tem `\n  hint: …` na linha seguinte, formato dos helpers existentes (`derefReadHint`, `referenceAssignmentTypeError`).
- Comentários em Go sem acentos (convenção do repo); docs em PT-BR (`REF_SEMANTICS.md`, `CHANGELOG.md`, plans/specs) ou EN (`NOXY_LANGUAGE_SPEC.md`, `README.md`) conforme o arquivo.
- `go test ./...` verde ao fim de **cada** task. Um teste existente que quebre só porque o fonte Noxy dele dependia de leitura/ref implícito é reescrito para a forma nova (`*r`, `ref x`) mantendo a asserção; um que trava o comportamento antigo por design é reescrito para travar o novo (listados por task).
- Validação final (AGENTS.md): `go test ./...`; `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx`; `go build -o noxy ./cmd/noxy`.
- Versão final `v0.19.0` (BREAKING). Não mexer em RC/CoW/`Owned`, `validateParameterModes`, `OP_EQUAL`, fast paths.
- Helpers de teste: compilador → `New().Compile(parse(src))` ou `compileFunctionSource(t, src)`; VM → `captureVMSource(t, src)` com `test_report(...)`, `semArray(t, v)`; erro de runtime → `interpretVMSource(t, New(), src)` devolve `error`.

---

### Task 1: Ajustes finais de spec e issues de follow-up

**Files:**
- Modify: `docs/superpowers/specs/2026-08-24-explicit-ref-design.md`

**Interfaces:**
- Produces: números das issues de follow-up (usados no CHANGELOG da Task 12 e na nota da §2.2 da Task 11).

Duas correções na spec descobertas ao ler o código: (1) `when … case` não tem condição booleana (os cases são operações de canal) — sai de R2; (2) f-string `{r}` desugara para `to_str(r)` no parser (`parser.go:1013-1020`) e `to_str`/`print` são nativos sem assinatura que recebem o valor como está — pela decisão (a) (`print(r)` mostra a referência), `f"{r}"` e `to_str(r)` mostram `<ref …>` também, sem caso especial. Remove um marcador de AST e uma checagem só para f-string.

- [ ] **Step 1: Corrigir R2 e a tabela na spec**

Em `docs/superpowers/specs/2026-08-24-explicit-ref-design.md`:

1. Em **R2**, trocar `condição de \`if\`/\`while\`/\`when case\`` por `condição de \`if\`/\`while\`` e trocar `, interpolação \`{r}\` em f-string.` por `.` (fim da lista). Acrescentar ao fim do parágrafo de R2: `Um parâmetro sem assinatura (\`print\`, \`to_str\`, nativos legados) recebe o \`ref\` como valor — \`print(r)\`, \`to_str(r)\` e \`f"{r}"\` (que desugara para \`to_str(r)\`) mostram \`<ref …>\`; para ver o valor, \`*r\`.`
2. Em **Consequências diretas**, trocar `f-string é R2 (erro, \`{*r}\`)` por `f-string mostra \`<ref …>\` como \`to_str\` (é \`to_str(r)\` depois do parser)`.
3. Na **tabela 3.1**, linha `| \`f"{r}"\` | interpola 10 | **erro**, hint \`{*r}\` |` → `| \`f"{r}"\` | interpola 10 | interpola \`<ref …>\` |`.
4. Na **§4 Diagnósticos**, remover a linha `| R2, f-string | \`cannot interpolate ref T\` | \`use '{*r}'\` |`.
5. Na **§8**, na linha de `explicit_read_test.go`, trocar `\`*r = s\`, f-string; e que` por `\`*r = s\`; e que \`print\`/\`to_str\`/f-string mostram a referência e`.

- [ ] **Step 2: Abrir as duas issues de follow-up**

```bash
S="/c/Users/sandr/AppData/Local/Temp/claude/C--Users-sandr-Documents-noxy/48ba0ea6-59b2-4835-898f-7be31323e2da/scratchpad"
cat > "$S/fu_cow.md" <<'EOF'
## Contexto

`docs/NOXY_LANGUAGE_SPEC.md` §2.2 documenta como "edge":

> a `ref` taken *into* a container (`ref arr[0]`, a `ref` field) pins that container's identity at creation time. If the container is copied *afterwards*, writes through the pre-existing `ref` are visible to copies that have not yet materialized. Take refs after, not before, sharing.

Isso é um furo na semântica de valor (regras 1–4 da §2.2): uma cópia feita depois de existir um `ref` para dentro do container **não é independente**. Quem copia `arr` não tem como saber se alguém tomou `ref arr[0]` antes.

Registrado como fora de escopo da spec `docs/superpowers/specs/2026-08-24-explicit-ref-design.md` (issue #82): é bug de CoW na VM, não de sintaxe.

## Proposta

Um `ref` para dentro de um container conta como *owner* na contagem CoW (`Owners`/`Retain` já existem desde a série #66), de modo que a próxima cópia do container materialize na hora (clone eager) em vez de lazy. Custo só nesse caminho; a nota "Take refs after, not before, sharing" sai da spec.

## Critério

```noxy
let arr: int[] = [1, 2]
let r: ref int = ref arr[0]
let copia: int[] = arr      // cópia DEPOIS do ref
*r = 99
print(copia[0])             // hoje: 99; esperado: 1
```
EOF
gh issue create --repo estevaofon/noxy --label bug --title "CoW: \`ref\` para dentro de container tomado antes de uma cópia vaza escrita para a cópia (§2.2 \"documented edge\")" --body-file "$S/fu_cow.md"

cat > "$S/fu_null.md" <<'EOF'
## Contexto

`ref T` é implicitamente nullable (spec §2.3, R8 da spec `2026-08-24-explicit-ref-design.md`) e `T` nunca é. É uma regra só, mas importa o "billion-dollar mistake": todo `node.valor` com `node: ref Node` pode falhar em runtime com null reference, e a assinatura `func f(n: ref Node)` não diz se `n` pode ser `null`.

Registrado como fora de escopo da issue #82.

## Proposta (a discutir)

- `ref T` **não-nulo** por padrão; `ref T?` (ou `?ref T`) onde `null` é permitido — campos de lista/árvore, retornos de busca.
- Narrowing em `if x != null then … end` (dentro do bloco `x: ref T`).
- `null` só é aceito onde o tipo declarado é `ref T?`; `f(null)` para `ref T` vira erro de compilação.

É a mudança mais cara da série de `ref`; só vale antes do 1.0.
EOF
gh issue create --repo estevaofon/noxy --label enhancement --title "\`ref T\` não-nulo por padrão e \`ref T?\` para referências que admitem null" --body-file "$S/fu_null.md"
```

Anotar os dois números devolvidos (`#NN_COW`, `#NN_NULL`) — a Task 11 os cita na spec §2.2 e a Task 12 no CHANGELOG.

- [ ] **Step 3: Registrar os números na spec e commitar**

Na spec, seção **2. Escopo → Fora**, acrescentar ` — issue #NN_COW` ao fim do primeiro item e ` — issue #NN_NULL` ao fim do segundo.

```bash
git add docs/superpowers/specs/2026-08-24-explicit-ref-design.md
git commit -m "docs(spec): f-string e to_str mostram a referencia; when fora de R2; issues de follow-up (issue #82)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: R2 em `let` e em `*r = …` (+ R6 `*r = ref y`)

**Files:**
- Create: `internal/compiler/explicit_ref.go`
- Create: `internal/compiler/explicit_read_test.go`
- Modify: `internal/compiler/compiler.go:418-428` (let), `:539-548` (`*r = v`)
- Modify: `internal/compiler/assign_deref_hint_test.go` (2 testes novos)
- Modify: `internal/vm/ref_operand_semantics_test.go` (teste `TestDerefAssignmentFromRefRHSCopiesPointedValue`)

**Interfaces:**
- Produces: `func refReadHint(expr ast.Expression) string` e `func (c *Compiler) rejectRefRead(t ast.NoxyType, expr ast.Expression, where string) error` em `explicit_ref.go`, usados pelas Tasks 3–7.

- [ ] **Step 1: Escrever os testes RED**

`internal/compiler/explicit_read_test.go`:

```go
package compiler

// R2 (spec 2026-08-24-explicit-ref §3): um `ref T` nunca e lido
// implicitamente. Cada teste fixa o erro E o hint em uma posicao.

import (
	"strings"
	"testing"
)

func requireCompileError(t *testing.T, src string, wants ...string) {
	t.Helper()
	_, _, err := New().Compile(parse(src))
	if err == nil {
		t.Fatalf("deveria falhar na compilacao:\n%s", src)
	}
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("erro sem %q:\n%v", want, err)
		}
	}
}

func requireCompiles(t *testing.T, src string) {
	t.Helper()
	if _, _, err := New().Compile(parse(src)); err != nil {
		t.Fatalf("deveria compilar: %v\n%s", err, src)
	}
}

func TestLetAnnotatedFromRefIsError(t *testing.T) {
	requireCompileError(t, `let x: int = 10
let r: ref int = ref x
let m: int = r`, "type mismatch in 'm' declaration: expected int, got ref int", "hint: use '*r' to read the referenced value")
}

func TestLetAnnotatedFromDerefCompiles(t *testing.T) {
	requireCompiles(t, `let x: int = 10
let r: ref int = ref x
let m: int = *r`)
}

func TestLetInferredFromRefKeepsRefType(t *testing.T) {
	requireCompileError(t, `let x: int = 10
let r: ref int = ref x
let v = r
let n: int = v`, "expected int, got ref int", "hint: use '*v'")
}

func TestDerefAssignmentFromRefRHSIsError(t *testing.T) {
	requireCompileError(t, `let x: int = 10
let z: int = 99
let r: ref int = ref x
let s: ref int = ref z
*r = s`, "type mismatch in assignment: expected int, got ref int", "hint: use '*s' to read the referenced value")
}

func TestDerefAssignmentFromRefPrefixIsRebindOrValueHint(t *testing.T) {
	requireCompileError(t, `let x: int = 10
let z: int = 99
let r: ref int = ref x
*r = ref z`, "cannot assign ref int to int through '*r'", "hint: use 'r = ref z' to rebind the reference, or '*r = z' to write the value")
}

func TestDerefAssignmentFromDerefCompiles(t *testing.T) {
	requireCompiles(t, `let x: int = 10
let z: int = 99
let r: ref int = ref x
let s: ref int = ref z
*r = *s`)
}
```

Acrescentar ao fim de `internal/compiler/assign_deref_hint_test.go`:

```go
// R2: `let x: T = r` deixa de ler implicitamente — mesma mensagem e hint
// da atribuicao.
func TestLetDeclarationFromRefSuggestsDeref(t *testing.T) {
	requireDerefReadHint(t, `let x: int = 10
let r: ref int = ref x
let m: int = r`, "hint: use '*r' "+derefReadHintText)
}

// R2: o RHS de `*r = s` tambem nao le.
func TestDerefAssignmentFromRefRhsSuggestsDeref(t *testing.T) {
	requireDerefReadHint(t, `let x: int = 10
let z: int = 99
let r: ref int = ref x
let s: ref int = ref z
*r = s`, "hint: use '*s' "+derefReadHintText)
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/compiler -run 'TestLetAnnotatedFromRef|TestLetInferred|TestDerefAssignmentFrom|TestLetDeclarationFromRef' -v`
Expected: FAIL — `TestLetAnnotatedFromRefIsError`, `TestLetInferredFromRefKeepsRefType`, `TestDerefAssignmentFromRefRHSIsError`, `TestDerefAssignmentFromRefPrefixIsRebindOrValueHint`, `TestLetDeclarationFromRefSuggestsDeref`, `TestDerefAssignmentFromRefRhsSuggestsDeref` ("deveria falhar na compilacao"); os `…Compiles` passam.

- [ ] **Step 3: Criar o helper e remover os dois auto-derefs**

Criar `internal/compiler/explicit_ref.go`:

```go
package compiler

import (
	"fmt"

	"noxy-vm/internal/ast"
)

// refReadHint e o hint que acompanha todo erro "esperava T, veio ref T"
// (spec 2026-08-24-explicit-ref, R2): a leitura e sempre explicita com '*'.
func refReadHint(expr ast.Expression) string {
	if ident, ok := expr.(*ast.Identifier); ok {
		return fmt.Sprintf("\n  hint: use '*%s' to read the referenced value", ident.Value)
	}
	return "\n  hint: use '*' to read the referenced value"
}

// rejectRefRead aplica R2 nas posicoes que nao passam por areTypesCompatible
// (operando de operador, condicao, indice, colecao de for): onde o compilador
// espera um valor, um `ref T` estatico e erro. `where` nomeia a posicao na
// mensagem ("operand of '+'", "condition", "index").
func (c *Compiler) rejectRefRead(t ast.NoxyType, expr ast.Expression, where string) error {
	if _, isRef := t.(*ast.RefType); !isRef {
		return nil
	}
	return fmt.Errorf("[line %d] %s cannot be %s: a ref is never read implicitly%s",
		c.currentLine, where, noxyTypeName(t), refReadHint(expr))
}
```

Em `compiler.go`, no `LetStmt`, substituir o bloco

```go
		// Type Check
		// Auto-Deref if Value is Reference and Target is NOT Reference
		if n.Type != nil {
			if refType, isRef := valType.(*ast.RefType); isRef {
				if _, targetIsRef := n.Type.(*ast.RefType); !targetIsRef {
					// We have Ref, want Value -> Deref
					c.emitByte(byte(chunk.OP_DEREF))
					valType = refType.ElementType
				}
			}

			if !c.areTypesCompatible(n.Type, valType) {
				return nil, nil, fmt.Errorf("[line %d] type mismatch in '%s' declaration: expected %s, got %s", c.currentLine, n.Name.Value, n.Type.String(), noxyTypeName(valType))
			}
```

por

```go
		// Type Check. R2 (spec 2026-08-24-explicit-ref): `let x: T = r` com
		// r: ref T NAO le — o hint aponta '*r', como na atribuicao.
		if n.Type != nil {
			if !c.areTypesCompatible(n.Type, valType) {
				return nil, nil, fmt.Errorf("[line %d] type mismatch in '%s' declaration: expected %s, got %s%s", c.currentLine, n.Name.Value, n.Type.String(), noxyTypeName(valType), c.derefReadHint(n.Type, valType, n.Value))
			}
```

No `AssignStmt` com alvo `*…`, substituir

```go
			// Auto-deref RHS if it is a RefType but we need a Value.
			if valRef, valIsRef := valType.(*ast.RefType); valIsRef {
				// If target is value type, dereference the RHS reference.
				if _, targetIsRef := refT.ElementType.(*ast.RefType); !targetIsRef {
					c.emitByte(byte(chunk.OP_DEREF))
					valType = valRef.ElementType
				}
			}

			if !c.areTypesCompatible(refT.ElementType, valType) {
				return nil, nil, fmt.Errorf("[line %d] type mismatch in assignment: expected %s, got %s", c.currentLine, refT.ElementType.String(), valType.String())
			}
```

por

```go
			// R2/R6 (spec 2026-08-24-explicit-ref): o RHS de `*r = ...` nao
			// le um ref implicitamente. `*r = ref y` e quase sempre um rebind
			// escrito no lugar errado — hint com as duas formas legitimas.
			if _, valIsRef := valType.(*ast.RefType); valIsRef {
				if prefix, ok := n.Value.(*ast.PrefixExpression); ok && prefix.Operator == "ref" {
					target := prefixExp.Right.String()
					return nil, nil, fmt.Errorf(
						"[line %d] cannot assign %s to %s through '*%s'\n  hint: use '%s = ref %s' to rebind the reference, or '*%s = %s' to write the value",
						c.currentLine, noxyTypeName(valType), noxyTypeName(refT.ElementType), target, target, prefix.Right.String(), target, prefix.Right.String(),
					)
				}
			}

			if !c.areTypesCompatible(refT.ElementType, valType) {
				return nil, nil, fmt.Errorf("[line %d] type mismatch in assignment: expected %s, got %s%s", c.currentLine, refT.ElementType.String(), valType.String(), c.derefReadHint(refT.ElementType, valType, n.Value))
			}
```

Atualizar o comentário de `derefReadHint` em `compiler.go:3099-3102`: trocar `Atribuicao nao faz auto-deref (spec §2.3, Type-Based Assignment) — a leitura pede '*' explicito.` por `Nenhuma posicao le um ref implicitamente (spec 2026-08-24-explicit-ref, R2) — a leitura pede '*' explicito.`

- [ ] **Step 4: Rodar os testes novos**

Run: `go test ./internal/compiler -run 'TestLetAnnotatedFromRef|TestLetInferred|TestDerefAssignmentFrom|TestLetDeclarationFromRef' -v`
Expected: PASS (6 novos + os `…Compiles`).

- [ ] **Step 5: Atualizar o teste da VM que travava `*r = s`**

Em `internal/vm/ref_operand_semantics_test.go`, no teste `TestDerefAssignmentFromRefRHSCopiesPointedValue`, trocar a linha `*r = s` do fonte Noxy por `*r = *s` e o comentário acima dele por:

```go
// R2: `*r = *s` copia o valor apontado por s (o RHS `s` sem '*' e erro de
// compilacao — explicit_read_test.go). E copia, nao aliasing: mudar y
// depois nao alcanca x.
```

Run: `go test ./internal/compiler ./internal/vm`
Expected: PASS em `internal/compiler`; em `internal/vm` só podem falhar testes cujo fonte Noxy usa `let x: T = r` ou `*r = s` — reescrever com `*r`/`*s` (regra global). `go test ./...` deve ficar verde.

- [ ] **Step 6: Commit**

```bash
git add internal/compiler/explicit_ref.go internal/compiler/explicit_read_test.go internal/compiler/compiler.go internal/compiler/assign_deref_hint_test.go internal/vm/ref_operand_semantics_test.go
git commit -m "feat(compiler): R2 em let e em *r = ... — ref nunca e lido implicitamente; hint de rebind em *r = ref y (issue #82)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: R2 em operadores e condições

**Files:**
- Modify: `internal/compiler/compiler.go:1258-1284` (infix), `:1493-1497` (unário), ramos `&&`/`||` (`:1180-1236`), `:1550-1556` (if), `:1614-1620` (while)
- Modify: `internal/compiler/explicit_read_test.go`
- Modify: `internal/vm/ref_operand_semantics_test.go` (`TestWhileAndIfConditionsDereferenceRefBool`, `TestInfixRightOperandAndUnaryOperandDereferenceRefs`)
- Modify: `noxy_examples/type_errors/typed_function_invalid_ref_argument.nx`

**Interfaces:**
- Consumes: `rejectRefRead` (Task 2).

- [ ] **Step 1: Testes RED**

Acrescentar a `internal/compiler/explicit_read_test.go`:

```go
func TestInfixOperandRefIsError(t *testing.T) {
	requireCompileError(t, `let x: int = 10
let r: ref int = ref x
let y: int = r + 1`, "operand of '+' cannot be ref int: a ref is never read implicitly", "hint: use '*r' to read the referenced value")
	requireCompileError(t, `let x: int = 10
let r: ref int = ref x
let y: int = 1 + r`, "operand of '+' cannot be ref int", "hint: use '*r'")
	requireCompileError(t, `let x: int = 10
let r: ref int = ref x
let b: bool = r < 5`, "operand of '<' cannot be ref int", "hint: use '*r'")
}

func TestUnaryOperandRefIsError(t *testing.T) {
	requireCompileError(t, `let x: int = 10
let r: ref int = ref x
let y: int = -r`, "operand of '-' cannot be ref int", "hint: use '*r'")
	requireCompileError(t, `let f: bool = true
let rb: ref bool = ref f
let g: bool = !rb`, "operand of '!' cannot be ref bool", "hint: use '*rb'")
}

func TestLogicalOperandRefIsError(t *testing.T) {
	requireCompileError(t, `let f: bool = true
let rb: ref bool = ref f
let g: bool = rb && true`, "operand of '&&' cannot be ref bool", "hint: use '*rb'")
	requireCompileError(t, `let f: bool = true
let rb: ref bool = ref f
let g: bool = false || rb`, "operand of '||' cannot be ref bool", "hint: use '*rb'")
}

func TestConditionRefIsError(t *testing.T) {
	requireCompileError(t, `let f: bool = true
let rb: ref bool = ref f
if rb then
    print(1)
end`, "condition cannot be ref bool: a ref is never read implicitly", "hint: use '*rb'")
	requireCompileError(t, `let f: bool = true
let rb: ref bool = ref f
while rb do
    f = false
end`, "condition cannot be ref bool", "hint: use '*rb'")
}

func TestEqualityBetweenRefsStillCompiles(t *testing.T) {
	requireCompiles(t, `let x: int = 10
let r: ref int = ref x
let r2: ref int = ref x
let same: bool = r == r2
let isnull: bool = r == null
let val: bool = *r == 10`)
}

func TestExplicitReadsInOperatorsCompile(t *testing.T) {
	requireCompiles(t, `let x: int = 10
let f: bool = true
let r: ref int = ref x
let rb: ref bool = ref f
let a: int = *r + 1
let b: int = -*r
let c: bool = !*rb && *rb
if *rb then
    x = 1
end
while *rb do
    f = false
end`)
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/compiler -run 'TestInfixOperandRef|TestUnaryOperandRef|TestLogicalOperandRef|TestConditionRef|TestEqualityBetweenRefsStill|TestExplicitReadsInOperators' -v`
Expected: os quatro `…IsError` FAIL ("deveria falhar na compilacao"); `TestEqualityBetweenRefsStillCompiles` e `TestExplicitReadsInOperatorsCompile` PASS.

- [ ] **Step 3: Implementar**

Em `compiler.go`, caso `*ast.InfixExpression`, substituir os dois blocos

```go
			if _, ok := leftType.(*ast.RefType); ok && !identityComparison {
				// Always deref ref types before comparison (including null comparison)
				// This ensures 'ref Node == null' compares the pointed-to value, not the ref itself
				c.emitByte(byte(chunk.OP_DEREF))
				if ref, ok := leftType.(*ast.RefType); ok {
					leftType = ref.ElementType
				}
			}
```

e

```go
			if _, ok := rightType.(*ast.RefType); ok && !identityComparison {
				// Always deref ref types before comparison (including null comparison)
				c.emitByte(byte(chunk.OP_DEREF))
				if ref, ok := rightType.(*ast.RefType); ok {
					rightType = ref.ElementType
				}
			}
```

por, respectivamente,

```go
			// R2: operando ref nunca e lido; em `==`/`!=` os refs seguem
			// inteiros ate OP_EQUAL (identidade de slot, R7) e o caso misto
			// e rejectMixedRefComparison abaixo.
			if !identityComparison {
				if err := c.rejectRefRead(leftType, n.Left, "operand of '"+n.Operator+"'"); err != nil {
					return nil, nil, err
				}
			}
```

e

```go
			if !identityComparison {
				if err := c.rejectRefRead(rightType, n.Right, "operand of '"+n.Operator+"'"); err != nil {
					return nil, nil, err
				}
			}
```

Atualizar o comentário acima de `identityComparison := …`: trocar `Todos os demais operadores seguem dereferenciando neste ponto.` por `Nos demais operadores um ref e erro (R2).`

Nos ramos `&&` e `||` (mesmo caso `InfixExpression`, antes do bloco de `identityComparison`): imediatamente após cada `_, leftType, err := c.Compile(n.Left)` + checagem de `err`, inserir

```go
			if err := c.rejectRefRead(leftType, n.Left, "operand of '"+n.Operator+"'"); err != nil {
				return nil, nil, err
			}
```

e após cada `_, rightType, err := c.Compile(n.Right)` + checagem de `err`, inserir o mesmo com `rightType`/`n.Right`. (Quatro inserções: left/right em `&&`, left/right em `||`.)

No caso `*ast.PrefixExpression`, substituir

```go
		if ref, ok := rightType.(*ast.RefType); ok {
			c.emitByte(byte(chunk.OP_DEREF))
			rightType = ref.ElementType
		}
		if n.Operator == "-" {
```

por

```go
		if err := c.rejectRefRead(rightType, n.Right, "operand of '"+n.Operator+"'"); err != nil {
			return nil, nil, err
		}
		if n.Operator == "-" {
```

No `IfStatement` e no `WhileStatement` (ramos não fundidos), substituir cada

```go
			if ref, ok := condType.(*ast.RefType); ok {
				c.emitByte(byte(chunk.OP_DEREF))
				condType = ref.ElementType
			}
			if err := c.checkCondition(condType); err != nil {
```

por

```go
			if err := c.rejectRefRead(condType, n.Condition, "condition"); err != nil {
				return nil, nil, err
			}
			if err := c.checkCondition(condType); err != nil {
```

- [ ] **Step 4: Rodar os testes novos**

Run: `go test ./internal/compiler -run 'TestInfixOperandRef|TestUnaryOperandRef|TestLogicalOperandRef|TestConditionRef|TestEqualityBetweenRefsStill|TestExplicitReadsInOperators' -v`
Expected: PASS.

- [ ] **Step 5: Reescrever os testes da VM e o fixture que travavam o auto-deref**

`internal/vm/ref_operand_semantics_test.go`:

- Comentário de cabeçalho do arquivo: substituir por

```go
// Refs como operandos (spec 2026-08-24-explicit-ref, R2/R3): a leitura e
// sempre `*r`. Estes testes verificam PELO VALOR EM RUNTIME que `*r` em
// condicao de while/if, como operando de infix e de unario, no RHS de
// `*r = *s` e como indice (`arr[*ri]`, `ref arr[*ri]`) produz o valor
// apontado — o compilador nao pode ter deixado passar nenhum OP_DEREF
// implicito nem ter perdido o explicito.
```

- `TestWhileAndIfConditionsDereferenceRefBool` → renomear para `TestWhileAndIfConditionsReadDerefRefBool`; no fonte, `while rf do` → `while *rf do` e `if rf then` → `if *rf then`. Asserções iguais.
- `TestInfixRightOperandAndUnaryOperandDereferenceRefs` → renomear para `TestInfixAndUnaryOperandsReadDerefRefs`; no fonte, `[1 + rx, rx + 1, rx * rx, -rx]` → `[1 + *rx, *rx + 1, *rx * *rx, -*rx]` e `[5 > rx, rx < 5, !rb]` → `[5 > *rx, *rx < 5, !*rb]`. Asserções iguais.

`noxy_examples/type_errors/typed_function_invalid_ref_argument.nx`: trocar `*value = value + 1` por `*value = *value + 1` (o fixture deve continuar falhando **só** em `increment(41)`; `function_conformance_examples_test.go` espera o diagnóstico de `increment(41)`, que a Task 6 atualiza).

Run: `go test ./...`
Expected: PASS. Se falhar um teste de `internal/vm` ou `cmd/noxy` cujo fonte Noxy usa `r + 1`, `-r`, `!rb`, `if rb`, `while rb` com ref: reescrever com `*`.

- [ ] **Step 6: Commit**

```bash
git add internal/compiler/compiler.go internal/compiler/explicit_read_test.go internal/vm/ref_operand_semantics_test.go noxy_examples/type_errors/typed_function_invalid_ref_argument.nx
git commit -m "feat(compiler): R2 em operadores binarios, unarios, logicos e condicoes — operando ref e erro com hint '*r' (issue #82)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: R2 em índices e na coleção de `for … in`

**Files:**
- Modify: `internal/compiler/compiler.go:730-733` (índice em `arr[i] = v`), `:1088-1090` (fused read), `:1113-1121` (índice genérico), `:1660-1663` (for), `:2634-2640` (índice em `ref arr[i]`)
- Modify: `internal/compiler/cow_lowering.go:54-56`
- Modify: `internal/compiler/typed_index.go:141-150`
- Modify: `internal/compiler/generics_structs.go:540-547` (`forEachElementType`)
- Modify: `internal/compiler/explicit_read_test.go`
- Modify: `internal/vm/ref_operand_semantics_test.go` (teste de índice `ref int`), `internal/compiler/cow_lowering_test.go:160-173`, `internal/compiler/typed_index_compile_test.go:306-329`

**Interfaces:**
- Consumes: `rejectRefRead`.

- [ ] **Step 1: Testes RED**

Acrescentar a `internal/compiler/explicit_read_test.go`:

```go
func TestIndexOperandRefIsError(t *testing.T) {
	base := `let i: int = 0
let ri: ref int = ref i
let xs: int[] = [1, 2]
`
	requireCompileError(t, base+`let v: int = xs[ri]`, "index cannot be ref int: a ref is never read implicitly", "hint: use '*ri'")
	requireCompileError(t, base+`xs[ri] = 5`, "index cannot be ref int", "hint: use '*ri'")
	requireCompileError(t, base+`func f(t: ref int)
    *t = 1
end
f(ref xs[ri])`, "index cannot be ref int", "hint: use '*ri'")
	requireCompileError(t, `func g()
    let i: int = 0
    let ri: ref int = ref i
    let xs: int[] = [1, 2]
    let v: int = xs[ri]
end`, "index cannot be ref int", "hint: use '*ri'")
	requireCompileError(t, `func h()
    let i: int = 0
    let ri: ref int = ref i
    let xs: int[] = [1, 2]
    xs[ri] = 5
end`, "index cannot be ref int", "hint: use '*ri'")
}

func TestIndexOperandDerefCompiles(t *testing.T) {
	requireCompiles(t, `func g()
    let i: int = 0
    let ri: ref int = ref i
    let xs: int[] = [1, 2]
    let v: int = xs[*ri]
    xs[*ri] = 5
end
g()`)
}

func TestForOverRefCollectionIsError(t *testing.T) {
	requireCompileError(t, `let xs: int[] = [1, 2]
let r: ref int[] = ref xs
for x in r
    print(x)
end`, "cannot iterate over ref int[]: a ref is never read implicitly", "hint: use 'for x in *r'")
}

func TestForOverDerefCollectionTypesLoopVariable(t *testing.T) {
	requireCompileError(t, `let xs: int[] = [1, 2]
let r: ref int[] = ref xs
for x in *r
    let s: string = x
end`, "type mismatch in 's' declaration: expected string, got int")
}
```

Acrescentar a `internal/vm/ref_operand_semantics_test.go`:

```go
// R2: `for x in *r` itera o array apontado. (Antes, `for x in r` compilava
// e iterava zero vezes: OP_LEN devolve 0 para VAL_REF.)
func TestForOverDerefRefArrayIteratesPointedArray(t *testing.T) {
	got := captureVMSource(t, `
func soma(r: ref int[]) -> int
    let total: int = 0
    for x in *r
        total = total + x
    end
    return total
end
func main()
    let xs: int[] = [1, 2, 3]
    test_report(soma(ref xs))
end
main()
`)
	if got.Int() != 6 {
		t.Fatalf("soma = %s, want 6", got.String())
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/compiler -run 'TestIndexOperand|TestForOver' -v && go test ./internal/vm -run TestForOverDerefRefArray -v`
Expected: `TestIndexOperandRefIsError` e `TestForOverRefCollectionIsError` FAIL; `TestForOverDerefCollectionTypesLoopVariable` FAIL (hoje `for x in *r` — verificar: se já passa, ok); `TestForOverDerefRefArrayIteratesPointedArray` PASS ou FAIL — anotar.

- [ ] **Step 3: Implementar os seis sites de índice**

Em cada site abaixo, substituir o `if … OP_DEREF` pelo `rejectRefRead` com `where = "index"`:

`compiler.go` (assign `arr[i] = v`, ~`:730`):
```go
			if _, ok := idxType.(*ast.RefType); ok {
				c.emitByte(byte(chunk.OP_DEREF))
			}
```
→
```go
			if err := c.rejectRefRead(idxType, indexExp.Index, "index"); err != nil {
				return nil, nil, err
			}
```

`compiler.go` (fused read, ~`:1088`):
```go
			if _, isRef := idxType.(*ast.RefType); isRef {
				c.emitByte(byte(chunk.OP_DEREF))
			}
```
→
```go
			if err := c.rejectRefRead(idxType, n.Index, "index"); err != nil {
				return nil, nil, err
			}
```

`compiler.go` (índice genérico, ~`:1115-1123`): substituir os dois blocos
```go
		// Auto-dereference index if Ref
		if _, ok := idxType.(*ast.RefType); ok {
			c.emitByte(byte(chunk.OP_DEREF))
		}

		// Unwrap RefType in index
		if ref, ok := idxType.(*ast.RefType); ok {
			idxType = ref.ElementType
		}
```
por
```go
		// R2: indice ref e erro (hint '*ri').
		if err := c.rejectRefRead(idxType, n.Index, "index"); err != nil {
			return nil, nil, err
		}
```
e, no comentário `perf #66` logo abaixo, trocar `(ou ref T[], ja dereferenciada acima)` por `(ou ref T[], base atravessada acima — R4)`.

`compiler.go` (`compileReferenceArgumentValue`, caso `IndexExpression`, ~`:2637-2641`):
```go
		if _, ok := indexType.(*ast.RefType); ok {
			c.emitByte(byte(chunk.OP_DEREF))
		}
		indexType = unwrapRefType(indexType)
```
→
```go
		if err := c.rejectRefRead(indexType, target.Index, "index"); err != nil {
			return nil, err
		}
```

`cow_lowering.go` (~`:54`):
```go
		if _, ok := idxType.(*ast.RefType); ok {
			c.emitByte(byte(chunk.OP_DEREF))
		}
```
→
```go
		if err := c.rejectRefRead(idxType, n.Index, "index"); err != nil {
			return nil, false, err
		}
```

`typed_index.go` (`tryFuseLocalIndexAssign`, ~`:141-150`): substituir
```go
	if _, isRef := idxType.(*ast.RefType); isRef {
		c.emitByte(byte(chunk.OP_DEREF))
	}
	_, valType, err := c.Compile(valueExpr)
	if err != nil {
		return true, err
	}
	if ref, isRef := idxType.(*ast.RefType); isRef {
		idxType = ref.ElementType
	}
```
por
```go
	if err := c.rejectRefRead(idxType, target.Index, "index"); err != nil {
		return true, err
	}
	_, valType, err := c.Compile(valueExpr)
	if err != nil {
		return true, err
	}
```

- [ ] **Step 4: Implementar `for … in`**

`compiler.go`, caso `*ast.ForStatement`, logo após

```go
		_, colType, err := c.Compile(n.Collection)
		if err != nil {
			return nil, nil, err
		}
```

inserir

```go
		// R2: a colecao de um for-each nunca e um ref (antes compilava e
		// iterava zero vezes: OP_LEN devolve 0 para VAL_REF).
		if _, isRef := colType.(*ast.RefType); isRef {
			return nil, nil, fmt.Errorf(
				"[line %d] cannot iterate over %s: a ref is never read implicitly\n  hint: use 'for %s in *%s'",
				c.currentLine, noxyTypeName(colType), n.Identifier, n.Collection.String(),
			)
		}
```

`generics_structs.go`, `forEachElementType`: trocar `switch typed := unwrapRefType(collection).(type) {` por `switch typed := collection.(type) {` e atualizar o comentário da função: acrescentar `A colecao nunca e ref aqui (R2 rejeita antes).`

- [ ] **Step 5: Rodar os testes novos**

Run: `go test ./internal/compiler -run 'TestIndexOperand|TestForOver' -v && go test ./internal/vm -run TestForOverDerefRefArray -v`
Expected: PASS.

- [ ] **Step 6: Reescrever os testes que travavam o índice `ref int`**

- `internal/vm/ref_operand_semantics_test.go`: no teste de índice (`ref arr[ri]` / `arr[ri] = v` — o último do arquivo), trocar cada `arr[ri]` por `arr[*ri]` e `ref arr[ri]` por `ref arr[*ri]`; renomear o teste para `TestDerefIndexReadsPointedInt`. Asserções iguais.
- `internal/compiler/cow_lowering_test.go:160-173`: o fixture que asserta `OP_DEREF` de índice `ref` — trocar o índice `ri` por `*ri` no fonte e a asserção de "contém `OP_DEREF`" continua válida (o `*` explícito emite `OP_DEREF`). Se a asserção era "não contém", manter e conferir.
- `internal/compiler/typed_index_compile_test.go:306-329`: mesma regra — fixtures com índice `ref int` passam a usar `*ri`; a forma fundida continua sem `OP_DEREF_MUT`, e a não-fundida passa a ter o `OP_DEREF` do `*ri` explícito. Ajustar as asserções exatamente para isso.

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/compiler internal/vm/ref_operand_semantics_test.go
git commit -m "feat(compiler): R2 em indices e na colecao de for-each — for x in r era laco vazio silencioso (issue #82)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: R2 em `return` e em argumentos para parâmetros não-`ref`; `print(r)` mostra a referência

**Files:**
- Modify: `internal/compiler/compiler.go:2137-2149` (return), `:2497-2525` (argumento não-ref)
- Modify: `internal/compiler/builtin_calls.go:13-27` (`compileBuiltinValueArgument`) e os três erros `argument 2 to 'append'/'delete'` que citam `explicitRef`
- Modify: `internal/compiler/explicit_read_test.go`
- Create: `internal/vm/explicit_ref_runtime_test.go`

- [ ] **Step 1: Testes RED**

Acrescentar a `internal/compiler/explicit_read_test.go`:

```go
func TestReturnRefForValueTypeIsError(t *testing.T) {
	requireCompileError(t, `func f(r: ref int) -> int
    return r
end`, "return type mismatch in 'f': expected int, got ref int", "hint: use '*r'")
}

func TestReturnDerefCompiles(t *testing.T) {
	requireCompiles(t, `func f(r: ref int) -> int
    return *r
end
func g(r: ref int) -> ref int
    return r
end`)
}

func TestValueParameterRefArgumentIsError(t *testing.T) {
	requireCompileError(t, `func dobro(n: int) -> int
    return n * 2
end
let x: int = 2
let r: ref int = ref x
let y: int = dobro(r)`, "argument 1 to 'dobro': expected int, got ref int", "hint: use '*r'")
	requireCompileError(t, `func dobro(n: int) -> int
    return n * 2
end
let x: int = 2
let y: int = dobro(ref x)`, "argument 1 to 'dobro': expected int, got ref int", "hint: use '*'")
}

func TestValueParameterDerefArgumentCompiles(t *testing.T) {
	requireCompiles(t, `func dobro(n: int) -> int
    return n * 2
end
let x: int = 2
let r: ref int = ref x
let y: int = dobro(*r)`)
}

// Parametro any e nativo sem assinatura recebem o ref como valor (R2, ultimo
// paragrafo): compila, e print/to_str mostram a referencia.
func TestAnyAndUnsignedNativeAcceptRefAsValue(t *testing.T) {
	requireCompiles(t, `func guarda(v: any) -> any
    return v
end
let x: int = 2
let r: ref int = ref x
let kept: any = guarda(r)
print(r)
let s: string = to_str(r)
let f: string = f"{r}"`)
}

func TestAppendValueArgumentRefIsError(t *testing.T) {
	requireCompileError(t, `let xs: int[] = []
let x: int = 1
let r: ref int = ref x
append(ref xs, r)`, "argument 2 to 'append': expected int, got ref int", "hint: use '*r'")
}
```

Criar `internal/vm/explicit_ref_runtime_test.go`:

```go
package vm

import (
	"strings"
	"testing"
)

// print/to_str/f-string recebem o ref como valor e mostram a referencia
// (spec 2026-08-24-explicit-ref, decisao (a)); `*r` mostra o valor.
func TestToStrOfRefShowsReferenceNotValue(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let x: int = 42
    let r: ref int = ref x
    test_report([to_str(r), to_str(*r), f"{r}", f"{*r}"])
end
main()
`)
	cells := semArray(t, got)
	if len(cells) != 4 {
		t.Fatalf("esperava 4 celulas, obtido %s", got.String())
	}
	asRef, _ := cells[0].Obj.(string)
	asVal, _ := cells[1].Obj.(string)
	fRef, _ := cells[2].Obj.(string)
	fVal, _ := cells[3].Obj.(string)
	if !strings.HasPrefix(asRef, "<ref") || !strings.HasPrefix(fRef, "<ref") {
		t.Fatalf("to_str(r)=%q f\"{r}\"=%q, want prefix <ref", asRef, fRef)
	}
	if asVal != "42" || fVal != "42" {
		t.Fatalf("to_str(*r)=%q f\"{*r}\"=%q, want 42", asVal, fVal)
	}
}

// `return *r` de composto continua devolvendo um valor independente
// (o OP_COPY do caminho antigo de return-deref e preservado).
func TestReturnDerefCompositeIsIndependentCopy(t *testing.T) {
	got := captureVMSource(t, `
func le(r: ref int[]) -> int[]
    return *r
end
func main()
    let xs: int[] = [1, 2]
    let copia: int[] = le(ref xs)
    append(ref copia, 3)
    test_report([length(xs), length(copia)])
end
main()
`)
	cells := semArray(t, got)
	if len(cells) != 2 || cells[0].Int() != 2 || cells[1].Int() != 3 {
		t.Fatalf("[len xs, len copia] = %s, want [2, 3]", got.String())
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/compiler -run 'TestReturnRef|TestReturnDeref|TestValueParameter|TestAnyAndUnsigned|TestAppendValueArgument' -v && go test ./internal/vm -run 'TestToStrOfRef|TestReturnDerefComposite' -v`
Expected: `TestReturnRefForValueTypeIsError`, `TestValueParameterRefArgumentIsError`, `TestAppendValueArgumentRefIsError` FAIL (compila hoje); `TestToStrOfRefShowsReferenceNotValue` FAIL (hoje `to_str(r)` dá `"42"`); os demais PASS.

- [ ] **Step 3: Implementar `return`**

`compiler.go`, caso `ReturnStatement`, substituir

```go
		if ref, ok := actual.(*ast.RefType); ok {
			if _, expectsRef := expected.(*ast.RefType); !expectsRef {
				c.emitByte(byte(chunk.OP_DEREF))
				c.emitByte(byte(chunk.OP_COPY))
				actual = ref.ElementType
			}
		}
		if !c.areStrictTypesCompatible(expected, actual) {
			return nil, nil, fmt.Errorf(
				"[line %d] return type mismatch in '%s': expected %s, got %s",
				n.Token.Line, functionName, expected.String(), noxyTypeName(actual),
			)
		}
```

por

```go
		// R2: `return r` com retorno T e erro; `return *r` de composto
		// preserva o OP_COPY que o antigo return-deref emitia (valor
		// independente do slot apontado).
		if prefix, ok := n.ReturnValue.(*ast.PrefixExpression); ok && prefix.Operator == "*" {
			if _, primitive := expected.(*ast.PrimitiveType); !primitive {
				c.emitByte(byte(chunk.OP_COPY))
			}
		}
		if !c.areStrictTypesCompatible(expected, actual) {
			return nil, nil, fmt.Errorf(
				"[line %d] return type mismatch in '%s': expected %s, got %s%s",
				n.Token.Line, functionName, expected.String(), noxyTypeName(actual),
				c.derefReadHint(expected, actual, n.ReturnValue),
			)
		}
```

- [ ] **Step 4: Implementar argumento não-ref (chamada geral)**

`compiler.go`, `compileCallExpression`, substituir

```go
		_, argType, err := c.Compile(arg)
		if err != nil {
			return nil, nil, err
		}
		explicitReference := false
		if prefix, ok := arg.(*ast.PrefixExpression); ok {
			explicitReference = prefix.Operator == "ref"
		}
		if ref, ok := argType.(*ast.RefType); ok && !explicitReference {
			c.emitByte(byte(chunk.OP_DEREF))
			argType = ref.ElementType
		}
		argTypes = append(argTypes, argType)
```

por

```go
		// R2: um argumento `ref T` para parametro T e erro (abaixo, com
		// hint); para parametro any ou callee sem assinatura o ref passa
		// como valor (print(r) mostra a referencia).
		_, argType, err := c.Compile(arg)
		if err != nil {
			return nil, nil, err
		}
		argTypes = append(argTypes, argType)
```

e, mais abaixo no mesmo laço, substituir

```go
		if isExact && !c.areStrictTypesCompatible(funcType.Params[i], argType) {
			return nil, nil, fmt.Errorf(
				"[line %d] argument %d to '%s': expected %s, got %s",
				c.currentLine, i+1, callableName(call.Function),
				funcType.Params[i].String(), noxyTypeName(argType),
			)
		}
```

por

```go
		if isExact && !c.areStrictTypesCompatible(funcType.Params[i], argType) {
			return nil, nil, fmt.Errorf(
				"[line %d] argument %d to '%s': expected %s, got %s%s",
				c.currentLine, i+1, callableName(call.Function),
				funcType.Params[i].String(), noxyTypeName(argType),
				c.derefReadHint(funcType.Params[i], argType, arg),
			)
		}
```

- [ ] **Step 5: Implementar argumento de valor dos builtins**

`builtin_calls.go`, substituir `compileBuiltinValueArgument` por

```go
// compileBuiltinValueArgument compila um argumento de VALOR de builtin
// (item de append, chave de delete, texto de json_loads, limites de range).
// R2: um `ref T` aqui e devolvido como esta — o chamador rejeita com hint.
func (c *Compiler) compileBuiltinValueArgument(expression ast.Expression) (ast.NoxyType, error) {
	_, actual, err := c.Compile(expression)
	return actual, err
}
```

e nos dois erros que citam `explicitRef` (append arg 2 e delete arg 2), acrescentar o hint:

```go
			if _, explicitRef := item.(*ast.RefType); explicitRef {
				return true, nil, fmt.Errorf(
					"[line %d] argument 2 to 'append': expected %s, got %s%s",
					c.currentLine, noxyTypeName(array.ElementType), noxyTypeName(item),
					c.derefReadHint(array.ElementType, item, call.Arguments[1]),
				)
			}
```

```go
		if _, explicitRef := key.(*ast.RefType); explicitRef {
			return true, nil, fmt.Errorf(
				"[line %d] argument 2 to 'delete': expected %s, got %s%s",
				c.currentLine, noxyTypeName(mapping.KeyType), noxyTypeName(key),
				c.derefReadHint(mapping.KeyType, key, call.Arguments[1]),
			)
		}
```

- [ ] **Step 6: Rodar os testes**

Run: `go test ./internal/compiler -run 'TestReturnRef|TestReturnDeref|TestValueParameter|TestAnyAndUnsigned|TestAppendValueArgument' -v && go test ./internal/vm -run 'TestToStrOfRef|TestReturnDerefComposite' -v`
Expected: PASS.

Run: `go test ./...`
Expected: PASS. Testes que quebrem por `print(r)`/`to_str(r)`/`length(r)`/`f(r)` com ref em fonte Noxy → reescrever com `*r` (regra global). `length(r)` com `r: ref int[]` agora é `length(*r)`.

- [ ] **Step 7: Commit**

```bash
git add internal/compiler internal/vm/explicit_ref_runtime_test.go
git commit -m "feat(compiler): R2 em return e em argumento para parametro nao-ref; print/to_str mostram a referencia (issue #82)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: R5 em call sites (função, `func` tipado, construtor, generic) e R1 (`ref` sobre ref; `ref ref T` no parser)

**Files:**
- Modify: `internal/compiler/explicit_ref.go` (novo helper `compileRefArgument`, `refArgumentHint`, `alreadyReferenceError`)
- Modify: `internal/compiler/compiler.go:2455-2475` (laço de argumentos), `:2567-2684` (`compileReferenceArgumentValue`)
- Modify: `internal/parser/parser.go:549-557` (`parseType`)
- Create: `internal/compiler/explicit_ref_argument_test.go`
- Modify: `internal/parser/syntax_errors_test.go` (1 caso)
- Modify: `internal/compiler/reference_arguments_test.go` (teste de forwarding `:78`), `internal/compiler/function_conformance_examples_test.go` (mensagem de `invalid ref argument`), `noxy_examples/typed_function_conformance.nx` (call site `increment(count)`)

**Interfaces:**
- Produces: `type refArgument struct{ element, plain ast.NoxyType; proven bool }`, `func (c *Compiler) compileRefArgument(arg ast.Expression) (refArgument, error)`, `func refArgumentHint(arg ast.Expression) string` — usados pela Task 7.

- [ ] **Step 1: Testes RED**

Criar `internal/compiler/explicit_ref_argument_test.go`:

```go
package compiler

// R5 (spec 2026-08-24-explicit-ref §3): um ref nunca e criado
// implicitamente — `f(x)` para parametro `ref T` e erro com hint `ref x`.
// R1: `ref` sobre algo que ja e ref e erro; `ref ref T` nao e um tipo.

import "testing"

const refParamPrelude = `func inc(v: ref int) -> void
    *v = *v + 1
end
`

func TestRefParameterPlainArgumentIsError(t *testing.T) {
	requireCompileError(t, refParamPrelude+`let x: int = 1
inc(x)`, "argument 1 to 'inc': expected ref int, got int", "hint: use 'ref x'")
}

func TestRefParameterPlainFieldArgumentIsError(t *testing.T) {
	requireCompileError(t, refParamPrelude+`struct P
    n: int
end
let p: P = P(1)
inc(p.n)`, "argument 1 to 'inc': expected ref int, got int", "hint: use 'ref p.n'")
}

func TestRefParameterLiteralArgumentIsError(t *testing.T) {
	requireCompileError(t, refParamPrelude+`inc(41)`, "argument 1 to 'inc': expected ref int, got int", "hint: bind the value to a variable and pass 'ref <name>'")
}

func TestRefParameterAcceptsRefForms(t *testing.T) {
	requireCompiles(t, refParamPrelude+`struct Node
    valor: int
    next: ref Node
end
func avanca(n: ref Node) -> void
    return
end
func acha() -> ref int
    let z: int = 0
    return ref z
end
let x: int = 1
let r: ref int = ref x
let nd: Node = Node(1, null)
inc(ref x)
inc(r)
inc(null)
inc(acha())
avanca(nd.next)
avanca(ref nd)`)
}

func TestRefParameterViaTypedFuncValueRequiresRef(t *testing.T) {
	requireCompileError(t, refParamPrelude+`let f: func(ref int) -> void = inc
let x: int = 1
f(x)`, "expected ref int, got int", "hint: use 'ref x'")
	requireCompiles(t, refParamPrelude+`let f: func(ref int) -> void = inc
let x: int = 1
f(ref x)`)
}

func TestStructConstructorRefFieldRequiresRef(t *testing.T) {
	requireCompileError(t, `struct Obs
    alvo: ref int
end
let t: int = 20
let o: Obs = Obs(t)`, "argument 1 to 'Obs': expected ref int, got int", "hint: use 'ref t'")
	requireCompiles(t, `struct Obs
    alvo: ref int
end
let t: int = 20
let o: Obs = Obs(ref t)
let o2: Obs = Obs(null)`)
}

func TestGenericRefParameterRequiresRef(t *testing.T) {
	requireCompileError(t, `func bump<T>(v: ref T, by: T) -> void
    return
end
let x: int = 1
bump(x, 1)`, "expected ref int, got int", "hint: use 'ref x'")
	requireCompiles(t, `func bump<T>(v: ref T, by: T) -> void
    return
end
let x: int = 1
bump(ref x, 1)`)
}

func TestAnyArgumentForRefParameterIsRuntimeChecked(t *testing.T) {
	// Tipo any: o modo nao e provado em compilacao — validateParameterModes
	// decide em runtime. Compila.
	requireCompiles(t, refParamPrelude+`let a: any = 1
inc(a)`)
}

func TestRefOfRefIsError(t *testing.T) {
	requireCompileError(t, refParamPrelude+`let x: int = 1
let r: ref int = ref x
inc(ref r)`, "'r' is already a reference", "hint: pass 'r' directly, without 'ref'")
	requireCompileError(t, `struct Node
    next: ref Node
end
func f(n: ref Node) -> void
    return
end
let nd: Node = Node(null)
f(ref nd.next)`, "'nd.next' is already a reference", "hint: pass 'nd.next' directly")
	requireCompileError(t, `func acha() -> ref int
    let z: int = 0
    return ref z
end
let q: ref int = ref acha()`, "'acha()' is already a reference")
	requireCompileError(t, refParamPrelude+`inc(ref null)`, "'null' is not addressable", "hint: pass null directly, without 'ref'")
}

func TestRefOfRefUpvalueAndGlobalIsError(t *testing.T) {
	requireCompileError(t, refParamPrelude+`let g: int = 1
let rg: ref int = ref g
func usa() -> void
    inc(ref rg)
end`, "'rg' is already a reference")
	requireCompileError(t, refParamPrelude+`func outer() -> void
    let x: int = 1
    let r: ref int = ref x
    let f: func() -> void = func() -> void
        inc(ref r)
    end
    f()
end`, "'r' is already a reference")
}
```

Acrescentar em `internal/parser/syntax_errors_test.go`, na tabela `cases` de `TestSyntaxErrorMessages`:

```go
		{
			name:   "ref of ref type",
			source: "let q: ref ref int\n",
			want:   []string{"SyntaxError: 'ref ref' is not a type", "hint: a reference is never taken to a reference"},
		},
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/compiler -run 'TestRefParameter|TestStructConstructorRefField|TestGenericRefParameter|TestAnyArgumentForRef|TestRefOfRef' -v && go test ./internal/parser -run TestSyntaxErrorMessages -v`
Expected: `…IsError`/`…RequiresRef` FAIL (compilam hoje), `TestRefOfRefIsError`/`…UpvalueAndGlobal` FAIL (forwarding compila hoje), o caso do parser FAIL; `TestRefParameterAcceptsRefForms`, `TestAnyArgumentForRefParameterIsRuntimeChecked` PASS.

- [ ] **Step 3: Helpers em `explicit_ref.go`**

Acrescentar a `internal/compiler/explicit_ref.go` (import `noxy-vm/internal/chunk` não é necessário):

```go
// refArgument e o resultado de compileRefArgument.
type refArgument struct {
	element ast.NoxyType // tipo apontado; nil para null ou tipo desconhecido
	plain   ast.NoxyType // != nil quando o argumento e um valor T conhecido (R5 violada)
	proven  bool         // modo provado em compilacao (false: any/desconhecido -> validateParameterModes)
}

// compileRefArgument compila um argumento destinado a um parametro ou slot
// `ref T` (R5): `ref x` cria a referencia (R1, compileReferenceArgument);
// qualquer outra expressao e compilada como valor comum e precisa JA ter
// tipo `ref T` (variavel, campo, elemento, chamada que retorna ref), ser
// `null`, ou ter tipo desconhecido/any (fronteira dinamica). Um valor T
// conhecido volta em `plain` para o chamador montar o erro com a posicao.
func (c *Compiler) compileRefArgument(arg ast.Expression) (refArgument, error) {
	if prefix, ok := arg.(*ast.PrefixExpression); ok && prefix.Operator == "ref" {
		element, err := c.compileReferenceArgument(prefix.Right)
		if err != nil {
			return refArgument{}, err
		}
		return refArgument{element: element, proven: true}, nil
	}
	_, actual, err := c.Compile(arg)
	if err != nil {
		return refArgument{}, err
	}
	if ref, ok := actual.(*ast.RefType); ok {
		return refArgument{element: ref.ElementType, proven: true}, nil
	}
	if isNullType(actual) {
		return refArgument{proven: true}, nil
	}
	if actual == nil || isAny(actual) {
		return refArgument{}, nil
	}
	return refArgument{plain: actual}, nil
}

// refArgumentHint diz como consertar um valor T passado onde se esperava
// ref T: `ref x` para o que e enderecavel; para literal/temporario, uma
// variavel antes.
func refArgumentHint(arg ast.Expression) string {
	switch arg.(type) {
	case *ast.Identifier, *ast.MemberAccessExpression, *ast.IndexExpression:
		return fmt.Sprintf("\n  hint: use 'ref %s'", arg.String())
	}
	return "\n  hint: bind the value to a variable and pass 'ref <name>'"
}

// alreadyReferenceError e R1: `ref e` com e ja de tipo `ref T`.
func alreadyReferenceError(line int, expr ast.Expression) error {
	return fmt.Errorf("[line %d] '%s' is already a reference\n  hint: pass '%s' directly, without 'ref'",
		line, expr.String(), expr.String())
}
```

- [ ] **Step 4: Laço de argumentos (R5)**

`compiler.go`, `compileCallExpression`, substituir

```go
		if isExact {
			if expectedRef, ok := funcType.Params[i].(*ast.RefType); ok {
				actualElement, err := c.compileReferenceArgument(arg)
				if err != nil {
					return nil, nil, err
				}
				if _, isNull := arg.(*ast.NullLiteral); isNull {
					continue
				}
				if !c.areStrictTypesCompatible(expectedRef.ElementType, actualElement) {
					actual := &ast.RefType{ElementType: actualElement}
					return nil, nil, fmt.Errorf(
						"[line %d] argument %d to '%s': expected %s, got %s",
						c.currentLine, i+1, callableName(call.Function), expectedRef.String(), actual.String(),
					)
				}
				if err := c.emitRuntimeValueType(funcType.Params[i]); err != nil {
					return nil, nil, err
				}
				continue
			}
		}
```

por

```go
		if isExact {
			if expectedRef, ok := funcType.Params[i].(*ast.RefType); ok {
				// R5: parametro ref T recebe `ref x`, uma expressao ja ref T,
				// null, ou any/desconhecido (modo validado em runtime).
				refArg, err := c.compileRefArgument(arg)
				if err != nil {
					return nil, nil, err
				}
				if refArg.plain != nil {
					return nil, nil, fmt.Errorf(
						"[line %d] argument %d to '%s': expected %s, got %s%s",
						c.currentLine, i+1, callableName(call.Function), expectedRef.String(), noxyTypeName(refArg.plain), refArgumentHint(arg),
					)
				}
				if !refArg.proven {
					modesProven = false
				}
				if _, isNull := arg.(*ast.NullLiteral); isNull {
					continue
				}
				if refArg.element != nil && !c.areStrictTypesCompatible(expectedRef.ElementType, refArg.element) {
					actual := &ast.RefType{ElementType: refArg.element}
					return nil, nil, fmt.Errorf(
						"[line %d] argument %d to '%s': expected %s, got %s",
						c.currentLine, i+1, callableName(call.Function), expectedRef.String(), actual.String(),
					)
				}
				if err := c.emitRuntimeValueType(funcType.Params[i]); err != nil {
					return nil, nil, err
				}
				continue
			}
		}
```

- [ ] **Step 5: `compileReferenceArgumentValue` só cria (R1)**

Em `compiler.go`, `compileReferenceArgumentValue`:

1. Remover as três primeiras linhas do corpo:
```go
	if prefix, ok := expression.(*ast.PrefixExpression); ok && prefix.Operator == "ref" {
		expression = prefix.Right
	}
```
2. Atualizar o comentário/doc: acima da função inserir
```go
// compileReferenceArgumentValue emite a CRIACAO de uma referencia para o
// operando de `ref` (R1): l-value de tipo T -> OP_REF_*. Operando que ja e
// `ref T` e erro (alreadyReferenceError) — nao existe forwarding: uma
// expressao ref T e passada como qualquer valor (compileRefArgument).
```
3. No caso `*ast.Identifier`, trocar cada um dos três blocos
```go
			if ref, ok := declared.(*ast.RefType); ok {
				c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(slot))
				return ref.ElementType, nil
			}
```
```go
			if ref, ok := declared.(*ast.RefType); ok {
				c.emitBytes(byte(chunk.OP_GET_UPVALUE), byte(upvalue))
				return ref.ElementType, nil
			}
```
```go
			if ref, ok := declared.(*ast.RefType); ok {
				c.emitOpWithConstantIndex(chunk.OP_GET_GLOBAL, name)
				return ref.ElementType, nil
			}
```
por, respectivamente,
```go
			if _, ok := declared.(*ast.RefType); ok {
				return nil, alreadyReferenceError(c.currentLine, target)
			}
```
(três vezes; o de global fica **antes** de `name := c.makeConstant(...)` ser usado — a ordem atual já resolve o tipo antes de emitir, então basta a troca).
4. No caso `*ast.MemberAccessExpression`, trocar
```go
		if ref, ok := element.(*ast.RefType); ok {
			c.emitOpWithConstantIndex(chunk.OP_CONTEXT_REF_PROPERTY, name)
			return ref.ElementType, nil
		}
```
por
```go
		if _, ok := element.(*ast.RefType); ok {
			return nil, alreadyReferenceError(c.currentLine, target)
		}
```
5. No caso `*ast.IndexExpression`, trocar
```go
		if ref, ok := element.(*ast.RefType); ok {
			c.emitByte(byte(chunk.OP_CONTEXT_REF_INDEX))
			return ref.ElementType, nil
		}
```
por
```go
		if _, ok := element.(*ast.RefType); ok {
			return nil, alreadyReferenceError(c.currentLine, target)
		}
```
6. Substituir o caso `*ast.NullLiteral` inteiro por
```go
	case *ast.NullLiteral:
		return nil, fmt.Errorf("[line %d] 'null' is not addressable\n  hint: pass null directly, without 'ref'", c.currentLine)
```
7. No caso `*ast.CallExpression`, trocar
```go
		if ref, ok := result.(*ast.RefType); ok {
			return ref.ElementType, nil
		}
```
por
```go
		if _, ok := result.(*ast.RefType); ok {
			return nil, alreadyReferenceError(c.currentLine, target)
		}
```

`OP_CONTEXT_REF_PROPERTY`/`OP_CONTEXT_REF_INDEX` deixam de ser emitidos pelo compilador (uma leitura de slot ref é `OP_GET_PROPERTY`/`OP_GET_INDEX` comum). Os opcodes e seus cases na VM **ficam** (fora de escopo removê-los; a spec §5.1 os mantém).

- [ ] **Step 6: Parser — `ref ref T` (R1)**

`internal/parser/parser.go`, `parseType`, substituir

```go
	if p.curToken.Type == token.REF {
		p.nextToken()
		elementType := p.parseType()
		if elementType == nil {
			return nil
		}
		return &ast.RefType{ElementType: elementType}
	}
```

por

```go
	if p.curToken.Type == token.REF {
		line, column := p.curToken.Line, p.curToken.Column
		p.nextToken()
		elementType := p.parseType()
		if elementType == nil {
			return nil
		}
		// R1 (spec 2026-08-24-explicit-ref): nao existe ref ref T — cobre
		// `ref ref int` e `ref (ref int)` (o grupo volta por parseAtomicType).
		if _, isRef := elementType.(*ast.RefType); isRef {
			p.errors = append(p.errors, fmt.Sprintf("[%d:%d] SyntaxError: 'ref ref' is not a type
  hint: a reference is never taken to a reference", line, column))
			return nil
		}
		return &ast.RefType{ElementType: elementType}
	}
```

(`fmt` já é importado em `parser.go`.)

- [ ] **Step 7: Rodar os testes novos**

Run: `go test ./internal/compiler -run 'TestRefParameter|TestStructConstructorRefField|TestGenericRefParameter|TestAnyArgumentForRef|TestRefOfRef' -v && go test ./internal/parser -run TestSyntaxErrorMessages -v`
Expected: PASS.

- [ ] **Step 8: Atualizar testes e fixtures que travavam forwarding/conversão contextual**

- `internal/compiler/reference_arguments_test.go`, teste em `:78` (asserta `OP_CONTEXT_REF_PROPERTY`/`OP_CONTEXT_REF_INDEX` para `ref` sobre slot ref): reescrever para afirmar o novo contrato — passar `a.next`/`xs[0]` **sem** `ref` compila e o chunk contém `OP_GET_PROPERTY`/`OP_GET_INDEX` (não `OP_CONTEXT_REF_*`); e `ref a.next` é erro `is already a reference`. Renomear para `TestStoredRefSlotIsPassedAsValueNotReferenced`.
- `internal/compiler/function_conformance_examples_test.go`: o caso `{"invalid ref argument", …}` passa a esperar `"argument 1 to 'increment': expected ref int, got int\n  hint: bind the value to a variable and pass 'ref <name>'"`.
- `noxy_examples/typed_function_conformance.nx` (lido por `TestTypedFunctionValidConformanceExampleCompiles`): `increment(count)` → `increment(ref count)`, e qualquer leitura de ref escalar sem `*` no arquivo → `*`.
- `internal/vm/ref_null_forwarding_test.go`: fontes com `f(ref a.next)` → `f(a.next)`; asserções iguais (o `null` armazenado continua chegando como `null`).

Run: `go test ./...`
Expected: PASS. Qualquer outro teste cujo fonte Noxy chama parâmetro `ref` sem `ref` no call site, ou usa `ref` sobre algo já ref → aplicar o hint.

- [ ] **Step 9: Commit**

```bash
git add internal/compiler internal/parser internal/vm noxy_examples/typed_function_conformance.nx
git commit -m "feat(compiler): R5 — ref obrigatorio em todo call site; R1 — ref sobre ref e erro, ref ref T rejeitado no parser (issue #82)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 7: R5 nos builtins (`append`, `pop`, `delete`, `json_loads`, `append` em `(ref T)[]`)

**Files:**
- Modify: `internal/compiler/builtin_calls.go` (casos `append`, `pop`, `delete`, `json_loads`)
- Modify: `internal/compiler/explicit_ref_argument_test.go`

**Interfaces:**
- Consumes: `compileRefArgument`, `refArgumentHint` (Task 6).

- [ ] **Step 1: Testes RED**

Acrescentar a `internal/compiler/explicit_ref_argument_test.go`:

```go
func TestBuiltinRefArgumentRequiresRef(t *testing.T) {
	requireCompileError(t, `let xs: int[] = []
append(xs, 1)`, "argument 1 to 'append': expected ref T[], got int[]", "hint: use 'ref xs'")
	requireCompileError(t, `let xs: int[] = [1]
let v: int = pop(xs)`, "argument 1 to 'pop': expected ref T[], got int[]", "hint: use 'ref xs'")
	requireCompileError(t, `let m: map[string, int] = {"a": 1}
delete(m, "a")`, "argument 1 to 'delete': expected ref map, got map[string, int]", "hint: use 'ref m'")
	requireCompileError(t, `let alvo: int = 0
let ok: bool = json_loads("1", alvo)`, "argument 2 to 'json_loads': expected ref T, got int", "hint: use 'ref alvo'")
	requireCompileError(t, `let xs: (ref int)[] = []
let n: int = 1
append(ref xs, n)`, "argument 2 to 'append': expected ref int, got int", "hint: use 'ref n'")
}

func TestBuiltinRefArgumentAcceptsRefForms(t *testing.T) {
	requireCompiles(t, `struct Bag
    itens: int[]
end
let xs: int[] = []
let m: map[string, int] = {"a": 1}
let alvo: int = 0
let b: Bag = Bag([])
append(ref xs, 1)
append(ref b.itens, 2)
let v: int = pop(ref xs)
delete(ref m, "a")
let ok: bool = json_loads("1", ref alvo)
let rxs: (ref int)[] = []
let n: int = 1
let rn: ref int = ref n
append(ref rxs, ref n)
append(ref rxs, rn)
append(ref rxs, null)
func enche(p: ref int[]) -> void
    append(p, 9)
end`)
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/compiler -run 'TestBuiltinRefArgument' -v`
Expected: `TestBuiltinRefArgumentRequiresRef` FAIL; `…AcceptsRefForms` PASS (todas as formas já são válidas hoje, exceto `append(p, 9)` com `p: ref int[]` — hoje é forwarding e compila; deve continuar compilando).

- [ ] **Step 3: Implementar**

`builtin_calls.go`: adicionar o helper

```go
// compileBuiltinRefArgument aplica R5 aos builtins com parametro ref:
// append/pop/delete (arg 1), json_loads (arg 2), append em (ref T)[] (arg 2).
// `position` e "argument N to 'nome'"; `expected` e o texto do tipo esperado.
func (c *Compiler) compileBuiltinRefArgument(arg ast.Expression, position, expected string) (ast.NoxyType, error) {
	refArg, err := c.compileRefArgument(arg)
	if err != nil {
		return nil, err
	}
	if refArg.plain != nil {
		return nil, fmt.Errorf("[line %d] %s: expected %s, got %s%s",
			c.currentLine, position, expected, noxyTypeName(refArg.plain), refArgumentHint(arg))
	}
	return refArg.element, nil
}
```

e trocar, em cada caso:

- `append`: `container, err := c.compileReferenceArgument(call.Arguments[0])` → `container, err := c.compileBuiltinRefArgument(call.Arguments[0], "argument 1 to 'append'", "ref T[]")`; e dentro do ramo `expectedRef` (elemento `ref T`): `actualElement, err := c.compileReferenceArgument(call.Arguments[1])` → `actualElement, err := c.compileBuiltinRefArgument(call.Arguments[1], "argument 2 to 'append'", noxyTypeName(array.ElementType))`; a checagem seguinte `!c.areStrictTypesCompatible(expectedRef.ElementType, actualElement)` passa a ser guardada por `actualElement != nil &&` (null/desconhecido não tem elemento).
- `pop`: → `c.compileBuiltinRefArgument(call.Arguments[0], "argument 1 to 'pop'", "ref T[]")`.
- `delete`: → `c.compileBuiltinRefArgument(call.Arguments[0], "argument 1 to 'delete'", "ref map")`.
- `json_loads`: `targetType, err := c.compileReferenceArgument(call.Arguments[1])` → `targetType, err := c.compileBuiltinRefArgument(call.Arguments[1], "argument 2 to 'json_loads'", "ref T")`.

- [ ] **Step 4: Rodar**

Run: `go test ./internal/compiler -run 'TestBuiltinRefArgument' -v`
Expected: PASS.

Run: `go test ./...`
Expected: só falham testes cujo fonte Noxy chama `append(xs, …)`/`pop(xs)`/`delete(m, …)`/`json_loads(s, alvo)` sem `ref` — reescrever com `ref` (se o argumento já é parâmetro `ref T[]`, fica **sem** `ref`). Esperar muitos em `internal/vm` (`builtins_collections_test.go`, `builtins_json_test.go`, `json_*_test.go`) e `cmd/noxy`. Fazer a substituição com a mesma regex do codemod da Task 10 restrita a `internal/ internal/vm cmd/`:

```bash
python "$S/codemod_ref_builtins.py" internal cmd
```
(o script é criado na Task 10, Step 1 — criar agora se ainda não existe, é o mesmo arquivo) e revisar o diff.

- [ ] **Step 5: Commit**

```bash
git add internal cmd
git commit -m "feat(compiler): R5 nos builtins — append/pop/delete/json_loads exigem ref no call site (issue #82)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 8: VM — `OP_DEREF`/`OP_DEREF_MUT` estritos; `ref` sobre slot que já guarda referência

**Files:**
- Modify: `internal/vm/executor.go:646-660` (`OP_DEREF`), `:1752-1768` (`OP_DEREF_MUT`), `:426-449` (`OP_REF_PROPERTY`), `:479-520` (`OP_REF_INDEX`)
- Modify: `internal/vm/explicit_ref_runtime_test.go`

- [ ] **Step 1: Testes RED**

Acrescentar a `internal/vm/explicit_ref_runtime_test.go`:

```go
// R3 em runtime: `*x` sobre um valor que nao e ref (so alcancavel por tipo
// estatico desconhecido — membro dinamico de modulo/any nested) e erro.
func TestDerefOfNonRefAtRuntimeIsError(t *testing.T) {
	err := interpretVMSource(t, New(), `
struct Caixa
    v: any
end
func main()
    let c: Caixa = Caixa(7)
    let d: any = c
    let n: int = *d.v
end
main()
`)
	if err == nil || !strings.Contains(err.Error(), "cannot dereference int") {
		t.Fatalf("err = %v, want 'cannot dereference int'", err)
	}
}

// R1 em runtime: `ref` sobre um slot ref T alcancado por base any nao
// encaminha — e erro, espelhando o estatico 'is already a reference'.
func TestRefOfRefSlotThroughAnyIsError(t *testing.T) {
	err := interpretVMSource(t, New(), `
struct Node
    valor: int
    next: ref Node
end
func toca(n: ref Node) -> void
    return
end
func main()
    let seg: Node = Node(2, null)
    let a: any = Node(1, ref seg)
    toca(ref a.next)
end
main()
`)
	if err == nil || !strings.Contains(err.Error(), "slot 'next' already holds a reference") || !strings.Contains(err.Error(), "pass it directly") {
		t.Fatalf("err = %v, want 'slot 'next' already holds a reference ... pass it directly'", err)
	}
}

func TestRefOfRefIndexSlotThroughAnyIsError(t *testing.T) {
	err := interpretVMSource(t, New(), `
func toca(n: ref int) -> void
    return
end
func main()
    let x: int = 1
    let xs: (ref int)[] = [ref x]
    let a: any = xs
    toca(ref a[0])
end
main()
`)
	if err == nil || !strings.Contains(err.Error(), "already holds a reference") {
		t.Fatalf("err = %v, want 'already holds a reference'", err)
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/vm -run 'TestDerefOfNonRefAtRuntime|TestRefOfRefSlotThroughAny|TestRefOfRefIndexSlotThroughAny' -v`
Expected: FAIL nos três (hoje: passa adiante / encaminha). Se `*d.v` for rejeitado em compilação ("cannot dereference non-reference value of type any"), trocar `let n: int = *d.v` por passar `d.v` a uma função `func le(p: any) -> int … return *p` — ajustar o teste até o erro vir da VM.

- [ ] **Step 3: Implementar**

`executor.go`, `OP_DEREF`: substituir

```go
			} else if refVal.Type != value.VAL_REF {
				// Not a ref - pass through as-is (already dereferenced)
				vm.push(refVal)
			} else {
```

por

```go
			} else if refVal.Type != value.VAL_REF {
				// R3 (spec 2026-08-24-explicit-ref): `*x` de nao-ref e erro
				// tambem em runtime (tipo estatico desconhecido).
				return vm.runtimeError(c, ip, "cannot dereference %s", runtimeTypeName(refVal))
			} else {
```

`OP_DEREF_MUT`: substituir

```go
			if refVal.Type != value.VAL_REF {
				// Tolerância herdada do auto-deref antigo: slots com tipo
				// estático ref podem conter valores planos (checker leniente
				// pré-0.4). O valor já foi unicizado no nível anterior da
				// cadeia MUT — segue adiante como contêiner.
				vm.push(refVal)
				continue
			}
```

por

```go
			if refVal.Type == value.VAL_NULL {
				return vm.runtimeError(c, ip, "cannot write through a null reference")
			}
			if refVal.Type != value.VAL_REF {
				// R3: slot de tipo estatico ref T guarda ref ou null
				// (invariante do slot, spec 2026-08-20) — outra coisa e erro.
				return vm.runtimeError(c, ip, "cannot dereference %s", runtimeTypeName(refVal))
			}
```

(Conferir antes se `unicizeThroughRefValue` já trata `VAL_NULL` com mensagem própria; se sim, omitir o primeiro `if` para não duplicar.)

`OP_REF_PROPERTY`: substituir o bloco

```go
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

por

```go
			// R1 em runtime (base any): um slot declarado ref T ja guarda uma
			// referencia — `ref a.f` e erro, como o estatico
			// 'is already a reference'. Espelha alreadyReferenceError.
			if instance, ok := container.Obj.(*value.ObjInstance); ok && instance != nil && instance.Struct.FieldIsRef(name) {
				return vm.runtimeError(c, ip, "slot '%s' already holds a reference\n  hint: pass it directly, without 'ref'", name)
			}
```

e atualizar o comentário acima (`Base que o compilador nao conhecia…`) para `Base que o compilador nao conhecia (any, struct de outro modulo): campo declarado ref T e erro (R1).`

`OP_REF_INDEX`: no ramo `if arrayElementIsRefSlot(collection) {` (array) e no ramo equivalente de map (`mapValueIsRefSlot` ou o nome usado no case `*value.ObjMap`), substituir todo o corpo que encaminha por

```go
						return vm.runtimeError(c, ip, "slot %s already holds a reference\n  hint: pass it directly, without 'ref'", describeRefSlotIndex(idx, false))
```

(para map, `describeRefSlotIndex(idx, true)`). As checagens de tipo/faixa do índice que vinham **antes** do encaminhamento podem ser removidas junto — o erro de R1 vem primeiro. Ler o case inteiro antes de editar para não deixar variáveis não usadas.

- [ ] **Step 4: Rodar**

Run: `go test ./internal/vm -run 'TestDerefOfNonRefAtRuntime|TestRefOfRefSlotThroughAny|TestRefOfRefIndexSlotThroughAny' -v`
Expected: PASS.

Run: `go test ./...`
Expected: PASS. Testes de `internal/vm` que travavam o espelhamento (`ref_slot_invariant_test.go`, `json_ref_slot_population_test.go`, `malformed_reference_test.go` — os que passam `ref a.f` com `a: any` esperando forwarding) → reescrever para `a.f` (sem `ref`), ou para esperar o erro novo quando o teste é sobre `ref a.f`. Também `go test -race ./internal/value ./internal/vm`.

- [ ] **Step 5: Commit**

```bash
git add internal/vm
git commit -m "fix(vm): OP_DEREF/OP_DEREF_MUT nao toleram nao-ref; ref sobre slot ref via any e erro em vez de encaminhar (issue #82)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 9: Fixtures de erro novos e limpeza de referências ao modelo antigo no código Go

**Files:**
- Create: `noxy_examples/type_errors/ref_read_without_star.nx`, `ref_for_without_star.nx`, `ref_builtin_without_ref.nx`, `ref_of_ref.nx`, `deref_assign_ref_prefix.nx`
- Modify: `internal/compiler/function_conformance_examples_test.go`
- Modify: comentários em `internal/compiler/compiler.go` (`rejectMixedRefComparison`, `identityComparison`), `internal/vm/executor.go:1126-1138` (`OP_EQUAL`), `internal/compiler/function_types.go` (`unwrapRefType` doc)

- [ ] **Step 1: Criar os fixtures**

`noxy_examples/type_errors/ref_read_without_star.nx`:
```noxy
func dobro(r: ref int) -> int
    return r * 2
end
```

`noxy_examples/type_errors/ref_for_without_star.nx`:
```noxy
func soma(r: ref int[]) -> int
    let t: int = 0
    for x in r
        t = t + x
    end
    return t
end
```

`noxy_examples/type_errors/ref_builtin_without_ref.nx`:
```noxy
let xs: int[] = []
append(xs, 1)
```

`noxy_examples/type_errors/ref_of_ref.nx`:
```noxy
func inc(v: ref int) -> void
    *v = *v + 1
end
let x: int = 1
let r: ref int = ref x
inc(ref r)
```

`noxy_examples/type_errors/deref_assign_ref_prefix.nx`:
```noxy
let x: int = 1
let z: int = 2
let r: ref int = ref x
*r = ref z
```

- [ ] **Step 2: Registrar no teste de conformance**

Em `function_conformance_examples_test.go`, tabela de `TestTypedFunctionInvalidConformanceExamplesFail`, acrescentar:

```go
		{"ref read without star", "ref_read_without_star.nx", "operand of '*' cannot be ref int: a ref is never read implicitly\n  hint: use '*r' to read the referenced value"},
		{"ref for without star", "ref_for_without_star.nx", "cannot iterate over ref int[]: a ref is never read implicitly\n  hint: use 'for x in *r'"},
		{"ref builtin without ref", "ref_builtin_without_ref.nx", "argument 1 to 'append': expected ref T[], got int[]\n  hint: use 'ref xs'"},
		{"ref of ref", "ref_of_ref.nx", "'r' is already a reference\n  hint: pass 'r' directly, without 'ref'"},
		{"deref assign ref prefix", "deref_assign_ref_prefix.nx", "cannot assign ref int to int through '*r'\n  hint: use 'r = ref z' to rebind the reference, or '*r = z' to write the value"},
```

Run: `go test ./internal/compiler -run TestTypedFunctionInvalidConformanceExamplesFail -v`
Expected: PASS (todos os casos).

- [ ] **Step 3: Comentários que citam o modelo antigo**

- `compiler.go`, comentário de `rejectMixedRefComparison`: trocar `aplica a regra "em `==`/`!=` um ref nunca e dereferenciado implicitamente" ao caso misto estatico` por `aplica R2/R7 (spec 2026-08-24-explicit-ref) ao caso misto estatico de ==/!=`.
- `compiler.go`, o comentário acima de `identityComparison`: já ajustado na Task 3; conferir que não cita "excecao 1".
- `executor.go:1126-1138` (`OP_EQUAL`): trocar `(spec §2.3, excecao 1)` por `(spec 2026-08-24-explicit-ref, R7)`.
- `function_types.go`, acima de `unwrapRefType`, inserir: `// unwrapRefType serve as posicoes que ATRAVESSAM a referencia (R4: base de '.' e '[]', memberType); nao e usado para ler um ref como valor.`

Run: `grep -rn "auto-deref\|autoderef\|excecao 1\|excecao 2\|exceção 1\|exceção 2\|contextual\|forwarding" internal/ --include=*.go | grep -v _test.go`
Expected: nenhuma ocorrência que descreva comportamento vigente (menções históricas em comentários de CHANGELOG-style podem ficar; o grep deve listar zero linhas em código de compilador/VM fora de comentários de "antes era assim").

- [ ] **Step 4: Commit**

```bash
git add noxy_examples/type_errors internal
git commit -m "test(compiler): fixtures de erro para R1/R2/R5/R6; comentarios sem o modelo antigo (issue #82)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 10: Migração do corpus (`noxy_examples/`, `noxy_libs/`, `benchmarks/`, `tests/`)

**Files:**
- Create (scratchpad, não commitado): `$S/codemod_ref_builtins.py`, `$S/diff_outputs.sh`
- Modify: dezenas de `.nx` (guiado pelo compilador)
- Rename: `noxy_examples/test_autoderef.nx` → `noxy_examples/test_explicit_read.nx`
- Modify: `noxy_examples/run_all_tests_concurrent.nx` e `run_all_tests.nx` (só se algum arquivo renomeado estiver em `should_ignore` — não está)

- [ ] **Step 1: Codemod dos 4 builtins**

Criar `$S/codemod_ref_builtins.py` (`S` = scratchpad desta sessão):

```python
"""append(x, ...) -> append(ref x, ...); pop(x); delete(m, ...); json_loads(s, x).
Pula argumentos que ja comecam por `ref `/`null`, e identificadores declarados
como parametro `ref` no mesmo arquivo (`nome: ref ...`) — esses passam direto (R5)."""
import re, sys, pathlib

PATH = r'[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*|\[[^\]\[\n]*\])*'
CALL = re.compile(r'\b(append|pop|delete)\(\s*(?!ref\b|null\b)(' + PATH + r')\s*(,|\))')
JSON = re.compile(r'\bjson_loads\(([^,\n]+),\s*(?!ref\b|null\b)(' + PATH + r')\s*\)')
REF_PARAM = re.compile(r'\b([A-Za-z_][A-Za-z0-9_]*)\s*:\s*ref\b')

def migrate(text):
    ref_names = set(REF_PARAM.findall(text))
    def call_sub(m):
        name, arg, tail = m.group(1), m.group(2), m.group(3)
        root = re.split(r'[.\[]', arg, 1)[0]
        if root in ref_names:
            return m.group(0)
        return f"{name}(ref {arg}{tail}"
    def json_sub(m):
        arg = m.group(2)
        root = re.split(r'[.\[]', arg, 1)[0]
        if root in ref_names:
            return m.group(0)
        return f"json_loads({m.group(1)}, ref {arg})"
    out = []
    for line in text.splitlines(keepends=True):
        stripped = line.lstrip()
        if stripped.startswith("//"):
            out.append(line); continue
        line = CALL.sub(call_sub, line)
        line = JSON.sub(json_sub, line)
        out.append(line)
    return "".join(out)

changed = 0
for root in sys.argv[1:]:
    for p in pathlib.Path(root).rglob("*"):
        if p.suffix not in (".nx", ".go") or ".claude" in p.parts:
            continue
        src = p.read_text(encoding="utf-8")
        new = migrate(src)
        if new != src:
            p.write_text(new, encoding="utf-8", newline="")
            changed += 1
            print("migrated", p)
print("files changed:", changed)
```

Run: `python "$S/codemod_ref_builtins.py" noxy_examples noxy_libs benchmarks tests`
Expected: lista de arquivos migrados. Revisar `git diff --stat` e amostrar 5 diffs: cada `append(ref X,` deve ter `X` como valor possuído (não parâmetro `ref`). Reverter à mão qualquer linha dentro de string literal.

- [ ] **Step 2: Rodar o corpus e corrigir pelos hints**

```bash
go build -o noxy.exe ./cmd/noxy && ./noxy.exe noxy_examples/run_all_tests_concurrent.nx 2>&1 | tee "$S/runner1.txt" | grep -c "^FAIL"
```

Para cada `FAIL:<arquivo>`: `./noxy.exe noxy_examples/<arquivo>` mostra o erro com hint; aplicar **exatamente** o hint (`*r`, `ref x`, `for x in *r`, remover `ref` de já-ref, `r = ref y`). Regras de estilo ao editar:
- `*val = val * 2` → `*val = *val * 2`; `let t: int = x` com `x: ref int` → `*x`; `fmt(..., x)` → `*x`; `print(r)` → `print(*r)` **exceto** onde o arquivo demonstra a referência.
- `swap(a, b)` → `swap(ref a, ref b)`; `increment(active)` → `increment(ref active)`.
- `f(ref n.next)` com `next: ref T` → `f(n.next)`.
- Arquivos-demonstração do modelo antigo (`pattern_dynamic_aliases.nx`, `pattern_mutable_bindings.nx`, `smart_pointers.nx`, `consistency_demo.nx`, `consistency_check.nx`, `ref_example.nx`, `debug_swap.nx`, `KandR_in_noxy/ch05_ref_basics.nx`, `KandR_in_noxy/ch05_swap.nx`): reescrever comentários que dizem "auto-deref" para "leitura explícita com `*`"; manter a saída.
- `noxy_examples/test_autoderef.nx`: `git mv noxy_examples/test_autoderef.nx noxy_examples/test_explicit_read.nx`; reescrever com `*` e o comentário de cabeçalho `// Leitura explicita: r e a referencia, *r e o valor (spec 2026-08-24-explicit-ref)`.

Repetir até `grep -c "^FAIL"` = 0 (fora de `should_ignore`). Depois `./noxy.exe noxy_examples/run_all_tests.nx` (sequencial) para confirmar.

Também compilar o que o runner não cobre:
```bash
for f in noxy_libs/github_com/estevaofon/quicksort/quicksort.nx benchmarks/*.nx benchmarks/cross_runtime/*.nx tests/test_features/*.nx; do ./noxy.exe "$f" >/dev/null 2>"$S/err.txt" || { echo "FAIL $f"; head -3 "$S/err.txt"; }; done
```
(benchmarks imprimem `CHECKSUM:`; os de `tests/test_errors/` falham de propósito e ficam fora.) Corrigir pelos hints até zero FAIL.

- [ ] **Step 3: Diff de saída base × head**

Construir o binário base (v0.18.0) fora da árvore:

```bash
git worktree add "$S/base" main && (cd "$S/base" && go build -o "$S/noxy_base.exe" ./cmd/noxy) && git worktree remove "$S/base"
```

Criar `$S/diff_outputs.sh`:

```bash
#!/usr/bin/env bash
# Roda cada exemplo nos dois binarios e compara stdout+exit. A forma nova e
# subconjunto da v0.18.0, entao o base compila o corpus migrado.
S="$(dirname "$0")"; same=0; diff=0; skip=0
ignore="run_all_tests.nx run_all_tests_concurrent.nx http_server.nx simple_client.nx simple_server.nx web_app.nx http_server_basic.nx http_server_docs.nx form_app.nx http_server_sockets.nx todo_app2.nx todo_app.nx cadastro_usuarios.nx md2html.nx watch_file.nx stress_test.nx division_error.nx error_type.nx error_arity.nx test_let_error.nx error_index.nx to_int_conversion_demo.nx test_let_error_function.nx test_unclosed.nx benchmark_parallel.nx concurrency.nx concurrency_parallel_sum.nx concurrency_producer_consumer.nx fibonacci_spinner.nx test_typed_chan_error.nx test_web_server.nx langtons_ant.nx conway.nx conway_random.nx brainfuck.nx space_invaders.nx space_invaders2.nx signal_demo.nx gracefully_exit.nx wc_stdin.nx sqlite_demo.nx"
for f in noxy_examples/*.nx; do
  b="$(basename "$f")"
  case " $ignore " in *" $b "*) skip=$((skip+1)); continue;; esac
  "$S/noxy_base.exe" "$f" >"$S/out_base.txt" 2>&1 </dev/null; eb=$?
  ./noxy.exe "$f" >"$S/out_head.txt" 2>&1 </dev/null; eh=$?
  if [ $eb -eq $eh ] && cmp -s "$S/out_base.txt" "$S/out_head.txt"; then same=$((same+1)); else diff=$((diff+1)); echo "DIFF $b (base=$eb head=$eh)"; fi
done
echo "iguais=$same divergentes=$diff ignorados=$skip"
```

Run: `bash "$S/diff_outputs.sh" | tee "$S/diff_report.txt"`
Expected: `divergentes` só em arquivos onde (a) `print(r)` passou a mostrar `<ref …>` de propósito, (b) a saída é não-determinística (tempo, random, concorrência), ou (c) o base **rejeita** algo que só o head aceita — não deve existir (c). Para cada `DIFF`, olhar `diff` das saídas e classificar; anotar a lista (a) para o PR. Qualquer divergência fora de (a)/(b) é bug — investigar antes de seguir.

- [ ] **Step 4: Testes Go de novo e commit**

Run: `go test ./...`
Expected: PASS (fixtures lidos por `cmd/noxy` e `internal/compiler` já migrados).

```bash
git add -A noxy_examples noxy_libs benchmarks tests
git commit -m "refactor(examples): corpus migrado para ref explicito — *r para ler, ref x em todo call site (issue #82)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 11: Documentação — spec §2.3 e afins, `REF_SEMANTICS.md`, README, concurrency, AGENTS

**Files:**
- Modify: `docs/NOXY_LANGUAGE_SPEC.md` (§2.2 regras 6–8 e edge, Zen, §2.3 inteira, §3 :563, §4.2 :678-698 e :728/:749, §4.3, §6.5 exemplo, §7 :1222-1223, §8 :1738 e :1754-1757)
- Rewrite: `docs/REF_SEMANTICS.md`
- Modify: `README.md`, `docs/SHOWCASE.md`, `docs/concurrency.md`, `AGENTS.md`

- [ ] **Step 1: §2.3 inteira**

Substituir tudo de `### 2.3 References (\`ref\`)` até a linha antes de `## 3. Variable Declarations` por:

````markdown
### 2.3 References (`ref`)

A reference is never created or read without `ref` or `*` in the source.
Three forms, and one shortcut:

| Form | Meaning |
|---|---|
| `ref x` | create a reference to the slot of `x` |
| `r` | the reference itself (where it points) |
| `*r` | the referenced value — read it, or write it with `*r = v` |
| `r.f`, `r[i]` | shortcut for `(*r).f`, `(*r)[i]` — reads and writes |

#### R1. `ref x` creates a reference

`x` must be **addressable**: a local or global variable, a struct field, an
array element, a map entry, or a captured variable. A non-null literal or a
temporary (the result of a call whose type is not `ref T`) is not:

```noxy
let err: Error = Error("msg")
let r: ref Error = ref err          // OK
let bad: ref Error = ref Error("m") // ERROR: reference argument 'Error("m")' is not addressable
```

If `x` already has type `ref T`, `ref x` is an error — a reference is passed
as any other value, never re-referenced. There is no `ref ref T`; the
annotation is rejected by the parser.

```noxy
let r: ref int = ref x
f(ref r)      // ERROR: 'r' is already a reference
              //   hint: pass 'r' directly, without 'ref'
f(r)          // OK
```

#### R2. A `ref T` is never read implicitly

Wherever the compiler expects a `T` and finds a `ref T`, it is an error, and
the hint says `*r`. This holds in every position: operands of binary and
unary operators, `if`/`while` conditions, the collection of `for … in`, an
index, an argument for a non-`ref` parameter, a `return` for a non-`ref`
return type, `let x: T = r`, `x = r`, and the right-hand side of `*r = s`.

```noxy
let x: int = 10
let r: ref int = ref x

let y: int = r + 1     // ERROR: operand of '+' cannot be ref int: a ref is never read implicitly
                       //   hint: use '*r' to read the referenced value
let y: int = *r + 1    // OK: 11
let n: int = r         // ERROR: type mismatch in 'n' declaration: expected int, got ref int
let n: int = *r        // OK
if rb then … end       // ERROR (rb: ref bool) — use 'if *rb then'
for v in ra … end      // ERROR (ra: ref int[]) — use 'for v in *ra'
```

A parameter or slot typed `any`, and a native without a signature (`print`,
`to_str`), accept a `ref T` **as a value**: the reference travels. So
`print(r)`, `to_str(r)` and `f"{r}"` (which is `to_str(r)`) show `<ref …>`;
write `print(*r)` to see the value. `let v = r` (inferred) gives `v: ref T`.

#### R3. `*r` is the only read and the only write of the referenced value

```noxy
let v: int = *r     // read
*r = 20             // write: x is now 20
*r = *s             // copy the value s points to into x
```

`*x` where `x` is statically not a reference is a compile error (`cannot
dereference int`); when the static type is unknown, the same check happens
at runtime with the same message. `*r` with `r == null` is a runtime null
reference error.

#### R4. `.` and `[]` go through the reference

`r.f`, `r[i]`, `r.f = v`, `r[i] = v`, `ref r.f`, `ref r[i]` are shortcuts
for `(*r).f` and so on, at any depth (`r.f.g`, `r[i].f`), and through an
`any` base at runtime. This is the language's one shortcut. The index itself
follows R2: `xs[ri]` with `ri: ref int` is an error — `xs[*ri]`.

```noxy
func insert(node: ref TreeNode, valor: int)
    if valor < node.valor then          // '.' goes through node
        if node.esquerda == null then   // the stored ref, compared to null (R7)
            let novo: TreeNode = TreeNode(valor, null, null)
            node.esquerda = ref novo    // rebind of the field (R6)
        else
            insert(node.esquerda, valor)  // the stored ref, passed as a value (R5)
        end
    end
end
```

#### R5. A reference is never created implicitly

A parameter, a constructor field, or a slot typed `ref T` accepts exactly:
`ref x` (R1); an expression whose static type is already `ref T` (a `ref`
variable, field, element, map entry, or a call returning `ref T`); or
`null`. Passing a plain `T` is an error with the hint `use 'ref x'`. The
rule is the same for user functions, typed `func` values, bare `func`,
struct constructors, generic instantiations, and builtins:

```noxy
func checkout(c: ref Cart) -> void … end
checkout(mine)          // ERROR: argument 1 to 'checkout': expected ref Cart, got Cart
                        //   hint: use 'ref mine'
checkout(ref mine)      // OK — and the call site shows that mine may change

append(xs, 1)           // ERROR — hint: use 'ref xs'
append(ref xs, 1)       // OK
pop(ref xs)             // OK
delete(ref m, "k")      // OK
json_loads(text, ref target)   // OK

func push(p: ref int[]) -> void
    append(p, 9)        // OK: p is already ref int[] — passed as is
end
```

An argument of type `any` is accepted at compile time; the runtime checks
the parameter mode (`function 'f' argument 1: expected ref int, got int`).

#### R6. Rebind is `=`, update is `*… =`

With `r: ref T`: `r = ref y` rebinds (`r` now points to `y`); `r = v` with
`v: T` is an error (hint: `*r = v`); `*r = ref y` is an error (hint: `r =
ref y` to rebind, or `*r = y` to write the value). The same holds for a
field, element, or map entry typed `ref T`: `x.next = ref n` rebinds;
`x.next = n` is an error. Rebinding a `ref` parameter changes only the
callee's reference, never the caller's.

#### R7. `==` and `!=`

Two references compare by **slot identity**; a reference compared with
`null` asks whether the reference itself is null; a reference compared with
a plain value is an error (R2) — write `*r == v`.

```noxy
let ra: ref int = ref a
let ra2: ref int = ref a
let rb: ref int = ref b   // b == a == 1
ra == ra2    // true  — same slot
ra == rb     // false — different slots
ra == null   // false — the reference is valid
*ra == 1     // true  — the values
ra == 1      // ERROR — hint: use '*ra'
```

`addr(ref x)` gives the identity as a printable value.

#### R8. `null` is a valid `ref T`

It can be stored, passed, returned, compared, and replaced by rebind.
Writing through it (`*r = v`, `r.f = v`) is a runtime error.

#### R9. Lifetime of a referenced local

`ref x` on a local promotes the slot of `x` to a heap cell (an upvalue). The
cell lives as long as any reference to it exists — including after the
function that declared `x` has returned. This is how nodes are allocated;
there is no `new`:

```noxy
let novo: Node = Node(v, null)   // a variable: `ref` needs an l-value (R1)
node.next = ref novo             // `novo` becomes a cell; it outlives this function
```

Cost: one cell allocation per referenced local, and from then on the variable
takes part in ownership counting; locals that are never referenced stay on
the stack. `==` between references compares these cells (R7). A closure that
captures a `ref` to a local and is handed to `spawn`/`spawn_task` shares the
cell between routines — coordinate, as for globals ([docs/concurrency.md](concurrency.md)).

#### Diagnostics

| Situation | Message | Hint |
|---|---|---|
| `ref T` where `T` expected (R2) | `… expected T, got ref T` / `operand of '+' cannot be ref int: a ref is never read implicitly` | `use '*r' to read the referenced value` |
| `for x in r` | `cannot iterate over ref T[]: a ref is never read implicitly` | `use 'for x in *r'` |
| `xs[ri]` | `index cannot be ref int: a ref is never read implicitly` | `use '*ri'` |
| `f(x)` for `ref T` param (R5) | `argument N to 'f': expected ref T, got T` | `use 'ref x'` |
| `append(xs, v)` | `argument 1 to 'append': expected ref T[], got T[]` | `use 'ref xs'` |
| `f(41)` for `ref T` param | `argument N to 'f': expected ref T, got int` | `bind the value to a variable and pass 'ref <name>'` |
| `ref r` with `r: ref T` (R1) | `'r' is already a reference` | `pass 'r' directly, without 'ref'` |
| `let q: ref ref int` | `SyntaxError: 'ref ref' is not a type` | `a reference is never taken to a reference` |
| `r = v` (R6) | `cannot assign T to ref T` | `use '*r = …' to update the referenced value` |
| `*r = ref y` (R6) | `cannot assign ref T to T through '*r'` | `use 'r = ref y' to rebind the reference, or '*r = y' to write the value` |
| `r == v` (R7) | `cannot compare ref T with T: a ref is never implicitly dereferenced in '=='` | `use '*r' to compare the referenced value` |
| `*x`, `x: int` (R3) | `cannot dereference int` | — |
| `ref a.f` through `any`, slot already ref (runtime) | `slot 'f' already holds a reference` | `pass it directly, without 'ref'` |

---
````

- [ ] **Step 2: §2.2, Zen e demais seções da spec**

`§2.2` regras 6 e 8:

- Regra 6 → `6. **\`ref\` is the only sharing mechanism visible in a type.** A \`ref\` points to a *slot* (variable, field, index, map entry); writes through any alias of the slot are visible to all aliases of that slot. Closures capture variables by name and globals are shared by name — those are the only other places where two names can see one slot, and the only ones that need coordination under concurrency (see §2.3 R9).`
- Regra 7: manter, trocando `see §2.3` por `see §2.3 R7`.
- Regra 8 → `8. Closures capture *variables* (slots) by name; that is the second way two names can see one slot (rule 6).`
- O parágrafo `One documented edge: …` fica, acrescentando ao fim: ` Tracked as issue #NN_COW.`

Zen (§ "The Zen of Noxy", nas duas ocorrências — spec e README): trocar a linha `Sharing is ref. There is no other way.` por `Sharing is ref — in the type and at the call site. Closures and globals share by name; nothing else does.`

`§3` (:563): trocar a frase que diz que `let v: T = r` auto-dereferencia por `\`let v = r\` with \`r: ref T\` gives \`v: ref T\` (the same slot, no copy). To copy the value, annotate and read: \`let v: T = *r\` — \`let v: T = r\` is an error (§2.3 R2).`

`§4.2` (:678-698): remover o bloco de "contextual conversion" e o de forwarding inteiros (de `When an exact parameter is \`ref T\`, an addressable expression…` até o exemplo `append_node`), substituindo por:

```markdown
A `ref T` parameter takes a reference and nothing else — `ref x`, a value
that already has type `ref T`, or `null` (§2.3 R5). That is true for exact
signatures, bare `func`, natives, and plugins alike; there is no contextual
conversion at any boundary:

```noxy
let value: int = 10
double_it(ref value)   // OK
double_it(value)       // ERROR — hint: use 'ref value'

func append_node(node: ref Node, valor: int)
    if node.proximo == null then
        let novo: Node = Node(valor, null)   // a variable: `ref` needs an l-value
        node.proximo = ref novo              // rebind of the owner's field
    else
        append_node(node.proximo, valor)     // the stored reference, passed as a value
    end
end
```

A slot declared `ref T` always holds a reference or `null`, and the runtime
never wraps anything else. Through a base typed `any`, `a.proximo` reads the
stored reference (or `null`) exactly as the typed base does; `ref a.proximo`
on such a slot is the runtime error `slot 'proximo' already holds a reference`.
```

Nas linhas :728 e :749 (bare `func` e "Explicit `ref` arguments remain references across this dynamic boundary"): trocar `Because bare \`func\` has no exact signature, a reference argument must be written explicitly as \`ref value\`; \`dynamic(value)\` passes a plain value and never infers or manufactures a reference.` por `A reference argument is always written \`ref value\` (§2.3 R5); through bare \`func\` the runtime checks the mode.`

`§4.3` Pass-by-Reference: no exemplo, `modify(list)` → `modify(ref list)`, e a frase `To share the caller's value and let the function mutate it, use \`ref\` in the parameter type — the only sharing mechanism in the language.` → `… use \`ref\` in the parameter type and \`ref\` at the call site — the signature and the call both say so.`

`§6.5`: exemplo `identity_ref` inalterado; `let v: ref int = identity(r)` continua erro.

`§7` (:1222-1223): trocar a frase de que a condição `ref bool` é dereferenciada por `A condition of type \`ref bool\` is an error — write \`if *rb then\` (§2.3 R2). \`&&\`/\`||\` follow the same rule.`

`§8` (:1738): trocar `A \`ref T\` operand is read as \`T\` (auto-dereference)` por `A \`ref T\` operand is an error — read it with \`*r\` (§2.3 R2)`; (:1754-1757) manter a descrição de `*` como dereference explícito.

Grep final na spec: `grep -n -i "auto-deref\|automatic deref\|contextual\|forward" docs/NOXY_LANGUAGE_SPEC.md` → só menções em contexto "não existe mais"/histórico.

- [ ] **Step 3: `docs/REF_SEMANTICS.md`**

Substituir o arquivo inteiro por:

````markdown
# Referências em Noxy

`ref T` é uma referência de primeira classe a um slot que contém um `T`. A
regra inteira cabe em uma frase: **uma referência nunca é criada nem lida
sem `ref` ou `*` no código** — e `.`/`[]` são o único atalho. A
especificação completa (R1–R9, diagnósticos) está em
[`NOXY_LANGUAGE_SPEC.md` §2.3](NOXY_LANGUAGE_SPEC.md#23-references-ref).

## As três formas

| Escrita | Significado |
|---|---|
| `ref x` | cria uma referência para o slot de `x` (variável, campo, índice, entrada de map, capturada) |
| `r` | a referência em si — para onde aponta |
| `*r` | o valor apontado: lê em expressão, escreve com `*r = v` |
| `r.f`, `r[i]` | atalho para `(*r).f`, `(*r)[i]` — leitura e escrita |

```noxy
let x: int = 10
let r: ref int = ref x

let n: int = *r        // le: 10          (`let n: int = r` e erro)
*r = 20                // escreve em x
r = ref y              // rebind: r passa a apontar para y
print(r)               // <ref ...>: a referencia
print(*r)              // o valor
```

## Em chamadas

Um parâmetro `ref T` recebe `ref x`, uma expressão que já é `ref T`, ou
`null`. Nunca um `T` cru — o call site mostra o que pode mudar:

```noxy
func increment(value: ref int) -> void
    *value = *value + 1
end

let answer: int = 41
increment(ref answer)   // answer passa a ser 42
increment(answer)       // erro: expected ref int, got int — hint: use 'ref answer'

append(ref xs, 1)       // builtins seguem a mesma regra
pop(ref xs)
delete(ref m, "k")
json_loads(texto, ref alvo)

func push(p: ref int[]) -> void
    append(p, 9)        // p ja e ref int[]: passa direto, sem `ref`
end
```

Não há distinção entre função tipada, `func` bare, construtor, generic ou
nativo. `ref` sobre algo que já é referência é erro (`'p' is already a
reference`); não existe `ref ref T`.

## Update × rebind

| LHS | RHS | Escrita | Ação |
|---|---|---|---|
| `ref T` | `T` | `*r = v` | escreve no slot apontado |
| `ref T` | `ref T` | `r = ref y` | troca para onde `r` aponta |
| `T` | `ref T` | `x = *r` | lê (`x = r` é erro) |

`*r = ref y` é erro com hint: `r = ref y` para rebind, ou `*r = y` para
escrever o valor.

## Comparação

Dois refs comparam **identidade de slot**; `r == null` pergunta se a
referência é nula; `r == 1` é erro — `*r == 1` compara valores.

## `null`

`null` é valor válido de `ref T`: pode ser guardado, passado, retornado,
comparado e substituído por rebind. Escrever através de `null` é erro de
runtime.

## Tempo de vida de uma local referenciada

`ref x` sobre uma local promove o slot de `x` a uma célula no heap. A célula
vive enquanto houver qualquer referência para ela — inclusive depois de a
função retornar. É assim que se alocam nós; não há `new`:

```noxy
let novo: Node = Node(v, null)   // uma variavel: `ref` exige um l-value
node.next = ref novo             // `novo` vira celula; sobrevive a funcao
```

Custo: uma alocação por local referenciada; locais nunca referenciadas ficam
na pilha. Uma closure que captura `ref` a uma local e vai para `spawn`
compartilha a célula entre routines — coordene, como com globais
([concurrency.md](concurrency.md)).

## Parâmetros comuns e semântica de valor

Sem `ref`, arrays, maps e structs são passados por **valor** — independentes
em qualquer profundidade (copy-on-write). A assinatura sem `ref` garante que
o chamador não é tocado; o call site sem `ref` garante o mesmo.
````

- [ ] **Step 4: README, SHOWCASE, concurrency, AGENTS**

`README.md`:
- Exemplo de abertura: `checkout(mine)` → `checkout(ref mine)`; comentário `// mine changes — and the signature says so` → `// mine changes — the signature and the call site say so`. `append(c.items, "gift")` → `append(ref c.items, "gift")`; `append(c.items, "receipt")` → `append(ref c.items, "receipt")` (dentro de `checkout`, `c` é `ref Cart` — `c.items` atravessa e é um slot comum, então **precisa** de `ref`); `append(yours.items, "pen")` → `append(ref yours.items, "pen")`.
- Regra 1: após `\`ref\` is the single, visible mechanism for sharing, and it is part of the type.` acrescentar ` It is written at the call site too — \`push(ref xs)\` — so a call that can mutate your value looks different from one that cannot.`
- Zen: mesma troca do Step 2.
- Features: `- ✅ Reference system (\`ref\`)` → `- ✅ Explicit references (\`ref x\` to create, \`*r\` to read, \`ref\` at every call site)`.
- "Quick Example": `append(nums, 1)` → `append(ref nums, 1)`, idem `2`.
- Qualquer outro `append(`/`pop(`/`delete(`/`json_loads(` no README → forma com `ref` (o codemod da Task 10 pode rodar sobre `README.md docs/*.md` — fazer isso e revisar o diff).

`docs/SHOWCASE.md`: rodar o codemod e revisar; leitura de ref escalar sem `*` → `*`.

`docs/concurrency.md`: na seção que fala de `ref` e globais precisarem coordenação, acrescentar o exemplo:

````markdown
A closure that captures a `ref` to a local shares that local's cell with
every routine that runs the closure — it is a local only in name:

```noxy
func main()
    let counter: int = 0
    let rc: ref int = ref counter       // counter is now a heap cell
    let bump: func() -> void = func() -> void
        *rc = *rc + 1                   // shared with main through rc
    end
    spawn(bump)
    spawn(bump)
    // *rc is read and written by three routines: coordinate through a
    // channel, or hand each routine its own value instead.
end
```
````

`AGENTS.md`:
- Em "📚 Recursos → Documentação": `docs/CONCURRENCY.md` → `docs/concurrency.md`.
- Acrescentar antes de "## 🎯 Workflow para Nova Feature":

```markdown
## `ref` nos exemplos e testes

Uma referência nunca é criada nem lida sem `ref` ou `*` (spec §2.3):
`*r` para ler/escrever o valor, `ref x` em **todo** call site com parâmetro
`ref T` (builtins inclusos: `append(ref xs, v)`), `r.f`/`r[i]` atravessam.
Uma expressão que já é `ref T` (parâmetro `ref`, campo `ref`) é passada
sem `ref`. `print(r)` mostra a referência; `print(*r)` o valor.
```

- [ ] **Step 5: Verificar e commitar**

Run: `grep -rn -i "auto-deref\|autoderef\|contextual conversion\|conversão contextual\|conversao contextual\|forwarding form" README.md docs/*.md AGENTS.md`
Expected: zero linhas (fora de `docs/superpowers/` e de trechos explicitamente históricos).

Run: `go test ./cmd/... ./internal/compiler` (fixtures lidos de docs, se houver) e `./noxy.exe noxy_examples/run_all_tests_concurrent.nx | grep -c ^FAIL` → `0`.

```bash
git add docs README.md AGENTS.md
git commit -m "docs: spec §2.3 reescrita (R1–R9), REF_SEMANTICS.md novo, README/concurrency/AGENTS com ref explicito (issue #82, fecha #81)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 12: Perf, CHANGELOG, versão e verificação final

**Files:**
- Modify: `CHANGELOG.md`, `internal/version/version.go`, `README.md` (badge), `docs/NOXY_LANGUAGE_SPEC.md` (versão no cabeçalho, se houver), `docs/index.html` (versão, se houver)

- [ ] **Step 1: Perf base × head**

```powershell
pwsh -File benchmarks/interleaved_compare.ps1 -Baseline "$env:S\noxy_base.exe" -Candidate .\noxy.exe -BaselineLabel v0.18.0 -CandidateLabel explicit-ref -Runs 9
```
(`$env:S` = scratchpad; `noxy_base.exe` da Task 10 Step 3; se não existir, reconstruir do `main` como lá.) Expected: todos os benches com `CHECKSUM` iguais (o base compila o corpus migrado) e delta dentro do ruído (±3 %). Guardar a tabela em `$S/perf.txt` para o PR. Uma regressão > 3 % em qualquer bench é bug — investigar antes de seguir (a mudança não toca caminho quente; suspeitar do `OP_COPY` de `return *r` em Task 5 se o bench devolve compostos por ref).

- [ ] **Step 2: CHANGELOG**

Inserir após `# Changelog` (antes de `## [0.18.0]`):

````markdown
## [0.19.0] - 2026-08-24

`ref` explícito (issue #82): uma referência nunca é criada nem lida sem
`ref` ou `*` no código. `*r` é a única leitura e escrita do valor apontado;
`ref x` é obrigatório em todo call site com parâmetro `ref T` — funções,
valores `func`, construtores, generics e builtins (`append(ref xs, v)`);
`.`/`[]` atravessam a referência e são o único atalho. Somem da linguagem o
auto-dereference, as exceções 1 e 2 de `==`/`=`, a conversão contextual em
chamadas exatas e a "forwarding form" (`ref r` com `r` já `ref`). A VM já
era explícita (aritmética, `==`, `print`, `length` nunca liam um ref); a
mudança é quase toda no compilador. Spec:
`docs/superpowers/specs/2026-08-24-explicit-ref-design.md`.

### Changed (BREAKING) — `ref` explícito: `*r` para ler, `ref x` em todo call site (issue #82)

| Antes (0.18) | Agora (0.19) |
|---|---|
| `let n: int = r`, `r + 1`, `-r`, `if rb`, `xs[ri]`, `return r`, `g(r)` liam o ref | erro `… expected T, got ref T` / `operand of '+' cannot be ref int: a ref is never read implicitly` — hint `use '*r'` |
| `for x in r` com `r: ref T[]` compilava e iterava **zero vezes** | erro `cannot iterate over ref T[]` — hint `for x in *r` |
| `*r = s` lia `s`; `*r = ref z` lia `z` (sem efeito) | `*r = *s`; `*r = ref z` é erro — hint `r = ref z` (rebind) ou `*r = z` |
| `f(x)` com parâmetro `ref T` emprestava `x` (só em assinatura exata) | erro `argument 1 to 'f': expected ref T, got T` — hint `use 'ref x'`; igual para `func` bare, construtor, generic |
| `append(xs, v)`, `pop(xs)`, `delete(m, k)`, `json_loads(s, alvo)` emprestavam | `append(ref xs, v)`, `pop(ref xs)`, `delete(ref m, k)`, `json_loads(s, ref alvo)` |
| `ref r` com `r: ref T` encaminhava (`ref ref T` proibido por construção) | erro `'r' is already a reference` — hint `pass 'r' directly`; `let q: ref ref int` é `SyntaxError` |
| `print(r)`, `to_str(r)`, `f"{r}"` mostravam o valor | mostram `<ref …>`; `print(*r)` mostra o valor |
| `*x` com `x` de tipo desconhecido não-ref passava adiante | erro de runtime `cannot dereference int` |
| `ref a.f` via `any` sobre campo `ref T` encaminhava | erro de runtime `slot 'f' already holds a reference` |

Inalterados: `r == s` (identidade), `r == null`, `r == 1` (erro, hint `*r`),
`r = 50` (erro, hint `*r = 50`), `x.next = n` (erro), `r.f`/`r[i]`,
`f(r)`/`f(a.next)`/`f(null)`, `let v = r` → `v: ref T`, generics (`T` não
liga a `ref X`).

### Added

- Diagnósticos com hint para cada posição de R1/R2/R5/R6 (spec §2.3
  "Diagnostics"); fixtures em `noxy_examples/type_errors/ref_*.nx`.
- Spec §2.3 reescrita como R1–R9; R9 documenta o tempo de vida de uma local
  referenciada (fecha #81); `docs/REF_SEMANTICS.md` reescrito.

### Fixed

- `for x in r` com `r: ref T[]` era um laço vazio silencioso (`OP_LEN`
  devolve 0 para `VAL_REF`); agora é erro de compilação com hint.

### Removed

- Auto-dereference, "Type-Based Assignment" e suas exceções 1 e 2,
  conversão contextual (§4.2), forwarding form — da spec e do compilador
  (`compileReferenceArgumentValue` só cria; `compileRefArgument` é o único
  caminho de argumento `ref`). Os opcodes `OP_CONTEXT_REF_PROPERTY`/`_INDEX`
  deixam de ser emitidos (os cases na VM ficam).

### Follow-ups registrados

- #NN_COW — `ref` para dentro de container tomado antes de uma cópia vaza
  escrita para a cópia (edge da §2.2).
- #NN_NULL — `ref T` não-nulo por padrão / `ref T?`.
````

- [ ] **Step 3: Versão**

- `internal/version/version.go`: `const Version = "v0.19.0"`.
- `README.md`: badge `noxy-0.18.0` → `noxy-0.19.0` (as duas ocorrências do link/label) e `Noxy REPL v0.18.0` → `v0.19.0`.
- `grep -rn "0\.18\.0" README.md docs/index.html docs/NOXY_LANGUAGE_SPEC.md AGENTS.md` → atualizar cada ocorrência de versão corrente para `0.19.0` (não tocar o CHANGELOG histórico).

Run: `go test ./internal/vm -run Version` (há `builtins_sys_version_test.go`) e `go build -o noxy.exe ./cmd/noxy && ./noxy.exe --version 2>/dev/null || ./noxy.exe -v`
Expected: `v0.19.0`.

- [ ] **Step 4: Verificação completa**

```bash
go test ./... && go test -race ./internal/value ./internal/vm && go build -o noxy.exe ./cmd/noxy && ./noxy.exe noxy_examples/run_all_tests_concurrent.nx | grep -c "^FAIL"
```
Expected: todos PASS, `0`.

- [ ] **Step 5: Commit da versão**

```bash
git add CHANGELOG.md internal/version/version.go README.md docs AGENTS.md
git commit -m "chore(version): noxy v0.19.0 — ref explicito: *r para ler, ref x em todo call site (issue #82); CHANGELOG, README, AGENTS, spec, site, version.go

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

Depois: `superpowers:finishing-a-development-branch` (PR para `main` com a tabela de perf de `$S/perf.txt` e a lista (a) de arquivos cuja saída mudou por `print(r)`, da Task 10 Step 3; `Closes #82`, `Closes #81`).
