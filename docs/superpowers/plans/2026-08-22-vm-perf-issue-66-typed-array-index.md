# VM Perf — Indexação tipada de array (issue #66, item 1) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Emitir opcodes de indexação tipada de array (formas genéricas `OP_GET_INDEX_ARRAY`/`OP_SET_INDEX_ARRAY_NORC` e formas fundidas por slot de local plano e de local `ref`) quando o compilador conhece o tipo `T[]`, sem mudar semântica, saída, mensagens de erro ou contagem RC — e medir o impacto (issue #66, item 1).

**Architecture:** Seis opcodes anexados ao fim de `chunk.go`. No VM, cada um tem caminho rápido (array + índice int + bounds, resultado gravado no lugar; escrita sem Retain/Release só quando `value.NeverTracked(val) && value.NeverTracked(old)` e a tag do array não é `(ref T)[]`; formas com slot exigem `Owners <= 1`; a forma `ref` resolve `REF_UPVALUE` com uma `Upvalue.Load()`) e fallback exato: leituras re-despacham o `OP_GET_INDEX` genérico via `goto redispatch`; escritas materializam a pilha e chamam `setIndexGeneric` (o corpo do `OP_SET_INDEX` extraído em método) depois de `unicizeOwnedSlot`/`unicizeBorrowedSlot`/`unicizeThroughRefValue`. No compilador, a forma fundida só sai quando índice (e valor) são sintaticamente livres de efeito colateral (`isSideEffectFree`).

**Tech Stack:** Go 1.24, PowerShell (benchmarks de `benchmarks/`), `go build -gcflags=-m=2` (guards de inline), `noxy --cpuprofile`.

**Spec:** `docs/superpowers/specs/2026-08-22-vm-perf-issue-66-typed-array-index-design.md`

## Global Constraints

- Branch `perf/issue-66-typed-array-index`, worktree `.claude/worktrees/perf-issue-66-arrays`, base `develop` 7eed082 (v0.14.3). Um commit por task; os commits das Tasks 3, 4, 5 e 6 geram binários de estágio (`noxy_s0/s1/s2/s3.exe`) — nunca squash entre eles.
- **Semântica, saída e mensagens de erro idênticas**; corpus `noxy_examples/` sem falhas (`go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx`) e diff de saída base × head vazio (`benchmarks/compare_examples.ps1`).
- **RC intocado nos compostos** (spec CoW-RC §4.2): só elemento sem contador pula Retain/Release, e só depois da conferência em runtime (`NeverTracked`). Nenhum funil muda de lugar.
- **Opcodes só por APPEND** ao fim de `internal/chunk/chunk.go`; tags `ValueType` inalteradas.
- Guards de inline (`internal/vm/inline_guard_test.go`): `push` ≤ 20 (≥ 100 sites), `pop` ≤ 20 (≥ 70 sites), `Retain`/`Release` ≤ 80, `ensureCallCapacity` ≤ 80 — e o novo `value.NeverTracked` ≤ 20. Conferir com `go build -gcflags=-m=2 ./internal/vm 2>&1 | grep -E "can inline"`.
- `go test ./...` verde; `go test -race ./internal/value ./internal/vm` verde; `go vet ./internal/value ./internal/vm ./internal/compiler ./internal/chunk` limpo.
- Repo com `core.autocrlf=true` (índice LF, working tree CRLF): arquivos novos via Write tool (LF) são normalizados no commit; edições em arquivos existentes via Edit tool; conferir diffs com `git diff --numstat`, nunca por diff de arquivo inteiro.
- Binários de benchmark em disco local: `$S\bench\` onde `$S = C:\Users\estev\AppData\Local\Temp\claude\D--OneDrive-Documentos-go-projects-noxy\ead4c52f-5869-403e-a45b-22421c6f07b9\scratchpad` (`noxy_base.exe` já está lá, buildado de 7eed082). Se o EDR apagar um .exe, rebuildar; Noxy ad hoc com `go run ./cmd/noxy arquivo.nx`.
- Máquina sem `go test` nem build concorrente durante as medições.
- Todos os comandos assumem cwd = raiz do worktree, Git Bash.

---

### Task 1: `value.NeverTracked` + guard de inline

**Files:**
- Modify: `internal/value/cow.go` (depois de `IsShared`)
- Create: `internal/value/never_tracked_test.go`
- Modify: `internal/vm/inline_guard_test.go` (função `TestRetainReleaseStayInlinable`)

**Interfaces:**
- Produces: `func NeverTracked(v Value) bool` — true quando `v` certamente não tem contador RC (`Type != VAL_OBJ`, ou `VAL_OBJ` carimbado `objKindNoOwners`: string/struct/RTI dos construtores); false para array/map/instância E para `VAL_OBJ` sem carimbo (conservador).

- [ ] **Step 1: Teste (falha: função não existe)**

Criar `internal/value/never_tracked_test.go`:

```go
package value

import "testing"

// NeverTracked e o teste que as escritas NORC da indexacao tipada fazem em
// runtime antes de pular Retain/Release (issue #66, item 1): so pode dizer
// "true" para o que o RC comprovadamente nao rastreia. A direcao segura e
// "false" — o chamador cai no caminho generico, que retem.
func TestNeverTrackedIsTrueOnlyForValuesWithoutOwners(t *testing.T) {
	inst := NewInstanceWith(&ObjStruct{Name: "P"}, map[string]Value{})
	cases := []struct {
		name string
		v    Value
		want bool
	}{
		{"int", NewInt(1), true},
		{"float", NewFloat(1.5), true},
		{"bool", NewBool(true), true},
		{"null", NewNull(), true},
		{"string (carimbada)", NewString("x"), true},
		{"bytes", Value{Type: VAL_BYTES, Obj: "ab"}, true},
		{"ref", Value{Type: VAL_REF, Obj: &ObjRef{}}, true},
		{"array", NewArray(nil), false},
		{"map", NewMap(), false},
		{"instance", inst, false},
		// VAL_OBJ montado fora dos construtores (kind zero): nao se sabe,
		// entao false — o caminho generico decide por ownersOf.
		{"string sem carimbo", Value{Type: VAL_OBJ, Obj: "x"}, false},
		{"array sem carimbo", Value{Type: VAL_OBJ, Obj: &ObjArray{}}, false},
	}
	for _, tc := range cases {
		if got := NeverTracked(tc.v); got != tc.want {
			t.Errorf("%s: NeverTracked = %v, esperado %v", tc.name, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/value -run TestNeverTracked`
Expected: FAIL — `undefined: NeverTracked`.

- [ ] **Step 3: Implementar**

Em `internal/value/cow.go`, logo depois de `IsShared`:

```go
// NeverTracked responde se v CERTAMENTE nao tem contador RC: escalares
// (Type != VAL_OBJ) e os VAL_OBJ que os construtores carimbaram como sem dono
// (string, *ObjStruct, *RuntimeTypeInfo). E a conferencia que as escritas NORC
// da indexacao tipada (issue #66, item 1) fazem antes de pular Retain/Release:
// se o valor novo ou o velho nao for comprovadamente sem contador (composto,
// ou VAL_OBJ sem carimbo — kind zero), o chamador cai no caminho generico, que
// retem. Conservador por construcao: nunca devolve true para um composto.
// Chamada de dentro de run(): tem de caber em 20 no inliner
// (inline_guard_test.go).
func NeverTracked(v Value) bool {
	return v.Type != VAL_OBJ || v.kind == objKindNoOwners
}
```

- [ ] **Step 4: Rodar e ver passar**

Run: `go test ./internal/value -run TestNeverTracked`
Expected: PASS.

- [ ] **Step 5: Guard de inline**

Em `internal/vm/inline_guard_test.go`, no fim de `TestRetainReleaseStayInlinable` (antes do `}` final), acrescentar:

```go
	// NeverTracked (cow.go) e chamada de DENTRO de run() pelas escritas NORC da
	// indexacao tipada (issue #66): orcamento de 20, nao 80.
	neverTrackedPattern := regexp.MustCompile(`can inline NeverTracked with cost (\d+)`)
	ntMatch := neverTrackedPattern.FindStringSubmatch(report)
	if ntMatch == nil {
		t.Fatalf("o compilador nao inlina value.NeverTracked — procure por 'cannot inline NeverTracked' em `go build -gcflags=-m=2 ./internal/value`")
	}
	ntCost, ntErr := strconv.Atoi(ntMatch[1])
	if ntErr != nil {
		t.Fatalf("custo ilegivel em %q: %v", ntMatch[0], ntErr)
	}
	if ntCost > inlineBigFunctionMaxCost {
		t.Errorf("value.NeverTracked tem custo de inline %d, maximo %d para ser inlinada dentro de run()", ntCost, inlineBigFunctionMaxCost)
	}
```

Run: `go test ./internal/vm -run TestRetainReleaseStayInlinable`
Expected: PASS (custo esperado ~6).

- [ ] **Step 6: Commit**

```bash
git add internal/value/cow.go internal/value/never_tracked_test.go internal/vm/inline_guard_test.go
git commit -m "perf(value): NeverTracked — conferencia barata de 'sem contador RC' para as escritas NORC da indexacao tipada, com guard de inline <= 20 (issue #66, item 1)"
```

---

### Task 2: Opcodes, `String()`, disassembler e programa de desmontagem

**Files:**
- Modify: `internal/chunk/chunk.go` (fim da lista de opcodes; `String()`; `disassembleInstruction`)
- Modify: `internal/chunk/chunk_test.go` (sentinela em `TestEveryOpcodeHasASymbolicNameWithoutGaps`; `disassemblyProgram`)

**Interfaces:**
- Produces: `chunk.OP_GET_INDEX_ARRAY`, `chunk.OP_SET_INDEX_ARRAY_NORC` (sem operando), `chunk.OP_GET_LOCAL_INDEX_ARRAY`, `chunk.OP_SET_LOCAL_INDEX_ARRAY_NORC`, `chunk.OP_GET_REF_LOCAL_INDEX_ARRAY`, `chunk.OP_SET_REF_LOCAL_INDEX_ARRAY_NORC` (operando `[slot u8]`), nessa ordem, depois de `OP_GREATER_FLOAT`.

- [ ] **Step 1: Teste (falha: sentinela aponta para o novo último opcode, que não existe)**

Em `internal/chunk/chunk_test.go`, trocar a sentinela:

```go
	if last := int(chunk.OP_SET_REF_LOCAL_INDEX_ARRAY_NORC); firstUnnamed >= 0 && firstUnnamed <= last {
```

E acrescentar ao fim de `disassemblyProgram` (depois de `xs[0] = 9`) um trecho que, quando o compilador emitir (Tasks 4–6), passa pelos seis opcodes — hoje compila para os genéricos:

```
func soma_local(v: int[]) -> int
    let acc: int = 0
    let i: int = 0
    while i < length(v) do
        acc = acc + v[i]
        i = i + 1
    end
    v[0] = acc
    return acc
end

func zera_ref(r: ref int[]) -> void
    let k: int = 0
    while k < length(r) do
        r[k] = r[k] - r[0]
        k = k + 1
    end
end

func grade(g: int[][]) -> int
    g[0][0] = g[1][1] + 1
    return g[0][0]
end

for item in xs do
    print(item)
end
soma_local(xs)
zera_ref(ref xs)
grade([[1, 2], [3, 4]])
```

Run: `go test ./internal/chunk`
Expected: FAIL — `undefined: chunk.OP_SET_REF_LOCAL_INDEX_ARRAY_NORC`.

- [ ] **Step 2: Declarar os opcodes**

Em `internal/chunk/chunk.go`, depois de `OP_GREATER_FLOAT` e antes do `)`:

```go
	// perf issue #66 (item 1): indexacao tipada de array. Emitidos so quando o
	// compilador sabe que a base e T[] (ou que o local e `ref T[]`). Semantica
	// e mensagens de erro identicas as dos genericos; container que nao e
	// array em runtime cai no caminho generico (leituras re-despacham
	// OP_GET_INDEX; escritas chamam setIndexGeneric). NORC = elemento
	// estaticamente sem contador RC (int/float/bool/string/bytes): o VM confere
	// em runtime (value.NeverTracked do valor novo e do velho, tag do array nao
	// e (ref T)[]) antes de pular Retain/Release. Os de escrita NAO empilham
	// resultado (atribuicao e statement; o compilador nao emite OP_POP depois).
	OP_GET_INDEX_ARRAY                // pops index, container; push elemento
	OP_SET_INDEX_ARRAY_NORC           // pops val, index, container
	OP_GET_LOCAL_INDEX_ARRAY          // [slot]; pops index; container = local T[]
	OP_SET_LOCAL_INDEX_ARRAY_NORC     // [slot]; pops val, index; local T[] possuidor (Owners<=1, senao uniciza como GET_LOCAL_MUT)
	OP_GET_REF_LOCAL_INDEX_ARRAY      // [slot]; pops index; local `ref T[]` (REF_UPVALUE resolvido com uma Load(); outros refs pelo caminho de OP_DEREF)
	OP_SET_REF_LOCAL_INDEX_ARRAY_NORC // [slot]; pops val, index; local `ref T[]` (Owners<=1, senao unicizeThroughRefValue)
```

- [ ] **Step 3: `String()` e disassembler**

Em `String()`, depois de `case OP_GREATER_FLOAT:`:

```go
	case OP_GET_INDEX_ARRAY:
		return "OP_GET_INDEX_ARRAY"
	case OP_SET_INDEX_ARRAY_NORC:
		return "OP_SET_INDEX_ARRAY_NORC"
	case OP_GET_LOCAL_INDEX_ARRAY:
		return "OP_GET_LOCAL_INDEX_ARRAY"
	case OP_SET_LOCAL_INDEX_ARRAY_NORC:
		return "OP_SET_LOCAL_INDEX_ARRAY_NORC"
	case OP_GET_REF_LOCAL_INDEX_ARRAY:
		return "OP_GET_REF_LOCAL_INDEX_ARRAY"
	case OP_SET_REF_LOCAL_INDEX_ARRAY_NORC:
		return "OP_SET_REF_LOCAL_INDEX_ARRAY_NORC"
```

Em `disassembleInstruction`, depois de `case OP_GREATER_FLOAT:`:

```go
	case OP_GET_INDEX_ARRAY:
		return c.simpleInstruction("OP_GET_INDEX_ARRAY", offset)
	case OP_SET_INDEX_ARRAY_NORC:
		return c.simpleInstruction("OP_SET_INDEX_ARRAY_NORC", offset)
	case OP_GET_LOCAL_INDEX_ARRAY:
		return c.byteInstruction("OP_GET_LOCAL_INDEX_ARRAY", offset)
	case OP_SET_LOCAL_INDEX_ARRAY_NORC:
		return c.byteInstruction("OP_SET_LOCAL_INDEX_ARRAY_NORC", offset)
	case OP_GET_REF_LOCAL_INDEX_ARRAY:
		return c.byteInstruction("OP_GET_REF_LOCAL_INDEX_ARRAY", offset)
	case OP_SET_REF_LOCAL_INDEX_ARRAY_NORC:
		return c.byteInstruction("OP_SET_REF_LOCAL_INDEX_ARRAY_NORC", offset)
```

- [ ] **Step 4: Rodar**

Run: `go test ./internal/chunk`
Expected: PASS (os três testes).

- [ ] **Step 5: Commit**

```bash
git add internal/chunk/chunk.go internal/chunk/chunk_test.go
git commit -m "feat(chunk): opcodes de indexacao tipada de array — GET/SET_INDEX_ARRAY(_NORC) e formas fundidas por slot de local plano e ref, por append (issue #66, item 1)"
```

---

### Task 3: Handlers no VM, `setIndexGeneric`, `unicizeOwnedSlot`/`unicizeBorrowedSlot`, `goto redispatch`

> **Nota pós-execução (2026-08-22):** a task foi executada como escrita (commit bf4f995), mas a medição por estágio pegou o `goto redispatch` custando +10–14 % no despacho genérico (`bench_generic_vs_hand`, laço sem indexação — relógio interno: base 623 ms, com goto 714, sem goto 631). O commit seguinte trocou os três fallbacks de leitura por uma chamada a `getIndexGeneric(c, ip)` (corpo de `OP_GET_INDEX` extraído em método, como `setIndexGeneric`) e removeu o rótulo; o `case OP_GET_INDEX` genérico passa a chamar o método. Spec §3.2 atualizada. O texto abaixo é o plano original.

**Files:**
- Create: `internal/vm/index_ops.go` (`setIndexGeneric`)
- Modify: `internal/vm/cow.go` (`unicizeOwnedSlot`, `unicizeBorrowedSlot`)
- Modify: `internal/vm/executor.go` (rótulo `redispatch`; `case OP_SET_INDEX`, `case OP_GET_LOCAL_MUT`, `case OP_GET_LOCAL_MUT_BORROW` passam a chamar os métodos; seis `case` novos)
- Create: `internal/vm/typed_index_test.go`
- Modify: `internal/vm/inline_guard_test.go` (sites de `NeverTracked` em executor.go)

**Interfaces:**
- Consumes: `value.NeverTracked` (Task 1); opcodes (Task 2); `vm.unicizeThroughRefValue`, `vm.resolveReferenceValue`, `vm.copyValue`, `frame.ownSlot` (existentes).
- Produces: `func (vm *VM) setIndexGeneric(c *chunk.Chunk, ip int) error` — corpo exato do antigo `case OP_SET_INDEX` (pops val/index/container, array/map/erros, push do val); `func (vm *VM) unicizeOwnedSlot(frame *CallFrame, idx int) value.Value` (corpo de `OP_GET_LOCAL_MUT`); `func (vm *VM) unicizeBorrowedSlot(idx int) value.Value` (corpo de `OP_GET_LOCAL_MUT_BORROW`).

- [ ] **Step 1: Testes de bytecode montado à mão (falham: opcodes não tratados = no-op, pilha errada)**

Criar `internal/vm/typed_index_test.go`:

```go
package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

// Frame raiz: LocalBase = 1, slot local 0 = stack[1]. Os testes deste arquivo
// montam o bytecode a mao para exercitar cada opcode de indexacao tipada
// (issue #66, item 1) e o seu fallback, sem depender do compilador; o
// comportamento ponta a ponta (fonte -> compilador -> VM) e coberto em
// typed_index_e2e_test.go.

func typedIndexChunk() *chunk.Chunk {
	code := &chunk.Chunk{}
	arr := code.AddConstant(value.NewArray([]value.Value{value.NewInt(10), value.NewInt(20), value.NewInt(30)}))
	code.Write(byte(chunk.OP_CONSTANT), 1) // slot 0 = array
	code.Write(byte(arr), 1)
	return code
}

func writeConstInt(code *chunk.Chunk, n int64) {
	k := code.AddConstant(value.NewInt(n))
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(k), 1)
}

func TestGetLocalIndexArrayReadsInPlace(t *testing.T) {
	code := typedIndexChunk()
	writeConstInt(code, 2)
	code.Write(byte(chunk.OP_GET_LOCAL_INDEX_ARRAY), 1)
	code.Write(0, 1) // slot do array
	machine := New()
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if got := machine.stack[2]; got.Type != value.VAL_INT || got.Int() != 30 {
		t.Fatalf("esperado 30 no topo, obtido %s", got.String())
	}
	if machine.stackTop != 3 {
		t.Fatalf("stackTop esperado 3 (array, elemento), obtido %d", machine.stackTop)
	}
}

func TestGetLocalIndexArrayErrorsMatchGeneric(t *testing.T) {
	cases := []struct {
		name string
		idx  value.Value
		want string
	}{
		{"fora da faixa", value.NewInt(3), "array index out of bounds"},
		{"negativo", value.NewInt(-1), "array index out of bounds"},
		{"nao inteiro", value.NewString("x"), "array index must be integer"},
	}
	for _, tc := range cases {
		code := typedIndexChunk()
		k := code.AddConstant(tc.idx)
		code.Write(byte(chunk.OP_CONSTANT), 1)
		code.Write(byte(k), 1)
		code.Write(byte(chunk.OP_GET_LOCAL_INDEX_ARRAY), 1)
		code.Write(0, 1)
		err := New().Interpret(code)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: esperava %q, obtido %v", tc.name, tc.want, err)
		}
	}
}

// Container que nao e array (null) cai no OP_GET_INDEX generico via
// redispatch: a mensagem e a do generico.
func TestGetLocalIndexArrayFallsBackToGenericForNonArray(t *testing.T) {
	code := &chunk.Chunk{}
	code.Write(byte(chunk.OP_NULL), 1) // slot 0 = null
	writeConstInt(code, 0)
	code.Write(byte(chunk.OP_GET_LOCAL_INDEX_ARRAY), 1)
	code.Write(0, 1)
	err := New().Interpret(code)
	if err == nil || !strings.Contains(err.Error(), "cannot index non-array/map/bytes") {
		t.Fatalf("esperava erro do OP_GET_INDEX generico, obtido %v", err)
	}
}

func TestGetIndexArrayReadsInPlace(t *testing.T) {
	code := typedIndexChunk()
	code.Write(byte(chunk.OP_GET_LOCAL), 1) // empilha o array
	code.Write(0, 1)
	writeConstInt(code, 1)
	code.Write(byte(chunk.OP_GET_INDEX_ARRAY), 1)
	machine := New()
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if got := machine.stack[2]; got.Type != value.VAL_INT || got.Int() != 20 {
		t.Fatalf("esperado 20, obtido %s", got.String())
	}
	if machine.stackTop != 3 {
		t.Fatalf("stackTop esperado 3, obtido %d", machine.stackTop)
	}
}

// Map no lugar do array: redispatch para o generico, que indexa o map.
func TestGetIndexArrayFallsBackToGenericMap(t *testing.T) {
	code := &chunk.Chunk{}
	m := code.AddConstant(value.NewMapWithData(map[string]value.Value{"k": value.NewInt(7)}))
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(m), 1)
	key := code.AddConstant(value.NewString("k"))
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(key), 1)
	code.Write(byte(chunk.OP_GET_INDEX_ARRAY), 1)
	machine := New()
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if got := machine.stack[1]; got.Type != value.VAL_INT || got.Int() != 7 {
		t.Fatalf("esperado 7 (leitura do map pelo generico), obtido %s", got.String())
	}
}

func TestSetLocalIndexArrayNorcWritesWithoutPushing(t *testing.T) {
	code := typedIndexChunk()
	writeConstInt(code, 1)
	writeConstInt(code, 99)
	code.Write(byte(chunk.OP_SET_LOCAL_INDEX_ARRAY_NORC), 1)
	code.Write(0, 1)
	machine := New()
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	arr := machine.stack[1].Obj.(*value.ObjArray)
	if arr.Elements[1].Int() != 99 {
		t.Fatalf("elemento 1 esperado 99, obtido %s", arr.Elements[1].String())
	}
	if machine.stackTop != 2 {
		t.Fatalf("escrita fundida nao deve empilhar: stackTop esperado 2, obtido %d", machine.stackTop)
	}
}

// Array compartilhado no slot (Owners > 1): a escrita fundida tem de clonar
// como OP_GET_LOCAL_MUT faria — o slot passa a guardar o clone (escrito) e o
// array original fica intacto.
func TestSetLocalIndexArrayNorcClonesSharedArray(t *testing.T) {
	code := typedIndexChunk()
	writeConstInt(code, 0)
	writeConstInt(code, 5)
	code.Write(byte(chunk.OP_SET_LOCAL_INDEX_ARRAY_NORC), 1)
	code.Write(0, 1)
	original := code.Constants[0]
	value.Retain(original)
	value.Retain(original) // Owners = 2: compartilhado
	ResetCloneCount()
	machine := New()
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if CloneCountValue() != 1 {
		t.Fatalf("esperava exatamente 1 clone CoW, obtido %d", CloneCountValue())
	}
	if original.Obj.(*value.ObjArray).Elements[0].Int() != 10 {
		t.Fatalf("array compartilhado foi mutado no lugar")
	}
	clone := machine.stack[1].Obj.(*value.ObjArray)
	if clone == original.Obj.(*value.ObjArray) || clone.Elements[0].Int() != 5 {
		t.Fatalf("slot deveria guardar o clone com a escrita: %v", machine.stack[1].String())
	}
}

// Valor composto (vindo por `any`) num NORC: nao pode pular o Retain — cai no
// generico, e o composto ganha um dono (o elemento do array).
func TestSetLocalIndexArrayNorcRetainsCompositeValueViaGeneric(t *testing.T) {
	code := typedIndexChunk()
	writeConstInt(code, 0)
	inner := value.NewArray([]value.Value{value.NewInt(1)})
	k := code.AddConstant(inner)
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(k), 1)
	code.Write(byte(chunk.OP_SET_LOCAL_INDEX_ARRAY_NORC), 1)
	code.Write(0, 1)
	before := value.OwnersCount(inner)
	if err := New().Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if got := value.OwnersCount(inner); got != before+1 {
		t.Fatalf("composto escrito em array deve ganhar 1 dono (generico retem): antes %d, depois %d", before, got)
	}
}

func TestSetIndexArrayNorcWritesAndFallsBackForNonArray(t *testing.T) {
	code := typedIndexChunk()
	code.Write(byte(chunk.OP_GET_LOCAL), 1)
	code.Write(0, 1)
	writeConstInt(code, 2)
	writeConstInt(code, -7)
	code.Write(byte(chunk.OP_SET_INDEX_ARRAY_NORC), 1)
	machine := New()
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if got := machine.stack[1].Obj.(*value.ObjArray).Elements[2].Int(); got != -7 {
		t.Fatalf("esperado -7, obtido %d", got)
	}
	if machine.stackTop != 2 {
		t.Fatalf("stackTop esperado 2, obtido %d", machine.stackTop)
	}

	bad := &chunk.Chunk{}
	bad.Write(byte(chunk.OP_NULL), 1)
	writeConstInt(bad, 0)
	writeConstInt(bad, 1)
	bad.Write(byte(chunk.OP_SET_INDEX_ARRAY_NORC), 1)
	err := New().Interpret(bad)
	if err == nil || !strings.Contains(err.Error(), "cannot set index on non-array/map") {
		t.Fatalf("esperava erro do OP_SET_INDEX generico, obtido %v", err)
	}
}

// Slot 1 = ref (REF_UPVALUE via OP_REF_LOCAL) para o slot 0 (array).
func refTypedIndexChunk() *chunk.Chunk {
	code := typedIndexChunk()
	code.Write(byte(chunk.OP_REF_LOCAL), 1)
	code.Write(0, 1)
	return code
}

func TestGetRefLocalIndexArrayResolvesUpvalueRef(t *testing.T) {
	code := refTypedIndexChunk()
	writeConstInt(code, 1)
	code.Write(byte(chunk.OP_GET_REF_LOCAL_INDEX_ARRAY), 1)
	code.Write(1, 1) // slot do ref
	machine := New()
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if got := machine.stack[3]; got.Type != value.VAL_INT || got.Int() != 20 {
		t.Fatalf("esperado 20, obtido %s", got.String())
	}
}

// Slot de tipo estatico ref que guarda null (parametro `ref T[]` recebendo
// null): OP_DEREF passa null adiante e OP_GET_INDEX reclama — a forma fundida
// tem de produzir a mesma mensagem.
func TestGetRefLocalIndexArrayNullRefMatchesGeneric(t *testing.T) {
	code := &chunk.Chunk{}
	code.Write(byte(chunk.OP_NULL), 1) // slot 0 = null
	writeConstInt(code, 0)
	code.Write(byte(chunk.OP_GET_REF_LOCAL_INDEX_ARRAY), 1)
	code.Write(0, 1)
	err := New().Interpret(code)
	if err == nil || !strings.Contains(err.Error(), "cannot index non-array/map/bytes") {
		t.Fatalf("esperava erro do generico para ref null, obtido %v", err)
	}
}

func TestSetRefLocalIndexArrayNorcWritesThroughRef(t *testing.T) {
	code := refTypedIndexChunk()
	writeConstInt(code, 0)
	writeConstInt(code, 42)
	code.Write(byte(chunk.OP_SET_REF_LOCAL_INDEX_ARRAY_NORC), 1)
	code.Write(1, 1)
	machine := New()
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if got := machine.stack[1].Obj.(*value.ObjArray).Elements[0].Int(); got != 42 {
		t.Fatalf("escrita via ref esperada 42, obtido %d", got)
	}
	if machine.stackTop != 3 {
		t.Fatalf("stackTop esperado 3 (array, ref), obtido %d", machine.stackTop)
	}
}

// Ref para array compartilhado: clona e grava o clone DE VOLTA pelo ref
// (unicizeThroughRefValue), como a sequencia GET_LOCAL_MUT_BORROW + DEREF_MUT.
func TestSetRefLocalIndexArrayNorcClonesSharedThroughRef(t *testing.T) {
	code := refTypedIndexChunk()
	writeConstInt(code, 0)
	writeConstInt(code, 42)
	code.Write(byte(chunk.OP_SET_REF_LOCAL_INDEX_ARRAY_NORC), 1)
	code.Write(1, 1)
	original := code.Constants[0]
	value.Retain(original)
	value.Retain(original)
	ResetCloneCount()
	machine := New()
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if CloneCountValue() != 1 {
		t.Fatalf("esperava 1 clone, obtido %d", CloneCountValue())
	}
	if original.Obj.(*value.ObjArray).Elements[0].Int() != 10 {
		t.Fatalf("array compartilhado foi mutado no lugar atraves do ref")
	}
	if got := machine.stack[1].Obj.(*value.ObjArray); got == original.Obj.(*value.ObjArray) || got.Elements[0].Int() != 42 {
		t.Fatalf("o slot apontado pelo ref deveria guardar o clone escrito")
	}
}

func TestSetRefLocalIndexArrayNorcNullRefMatchesGeneric(t *testing.T) {
	code := &chunk.Chunk{}
	code.Write(byte(chunk.OP_NULL), 1)
	writeConstInt(code, 0)
	writeConstInt(code, 1)
	code.Write(byte(chunk.OP_SET_REF_LOCAL_INDEX_ARRAY_NORC), 1)
	code.Write(0, 1)
	err := New().Interpret(code)
	if err == nil || !strings.Contains(err.Error(), "cannot set index on non-array/map") {
		t.Fatalf("esperava erro do generico para ref null, obtido %v", err)
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/vm -run 'TypedIndex|LocalIndexArray|IndexArray|RefLocalIndex'`
Expected: FAIL (opcodes não tratados no `switch` são no-op: valores errados na pilha / sem erro).

- [ ] **Step 3: `setIndexGeneric`**

Criar `internal/vm/index_ops.go` com o corpo EXATO do `case chunk.OP_SET_INDEX` atual (`executor.go`, do `val := vm.pop()` até o `return vm.runtimeError(c, ip, "cannot set index on non-array/map")`), trocando cada `continue` por `return nil`:

```go
package vm

import (
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

// setIndexGeneric e o corpo de OP_SET_INDEX: desempilha valor, indice e
// container, escreve em array (guarda de slot ref, retain-antes-de-release)
// ou map (retain/release so se a chave existia) e EMPILHA o valor (resultado
// da atribuicao; o compilador emite OP_POP em seguida). Virou metodo na
// indexacao tipada (issue #66, item 1) para ser o funil unico dos fallbacks
// dos opcodes *_NORC — que materializam a pilha generica, chamam isto e
// desempilham o resultado — sem duplicar a logica de erro; o custo e uma
// chamada a mais por OP_SET_INDEX generico (maps, any, elemento composto),
// medido em bench_map_churn.
func (vm *VM) setIndexGeneric(c *chunk.Chunk, ip int) error {
	val := vm.pop()
	indexVal := vm.pop()
	collectionVal := vm.pop() // The array/map itself is on stack (pointer)

	if collectionVal.Type == value.VAL_OBJ {
		if arr, ok := collectionVal.Obj.(*value.ObjArray); ok {
			if indexVal.Type != value.VAL_INT {
				return vm.runtimeError(c, ip, "array index must be integer")
			}
			idx := int(indexVal.Int())
			if idx < 0 || idx >= len(arr.Elements) {
				return vm.runtimeError(c, ip, "array index out of bounds")
			}
			// Guard do slot ref (spec §6.3): elemento `ref T` (tag
			// RuntimeType) so aceita ref/null; via base tipada o
			// compilador ja rejeitou. O teste de val.Type vem antes
			// para o Load() atomico so rodar em escritas nao-ref.
			if val.Type != value.VAL_REF && val.Type != value.VAL_NULL && arrayElementIsRefSlot(arr) {
				return vm.runtimeError(c, ip, "%s", refSlotWriteError(arr.RuntimeType.Load().Element.String(), val))
			}
			// RC: retain-antes-de-release (elemento e dono duravel)
			old := arr.Elements[idx]
			value.Retain(val)
			arr.Elements[idx] = val
			value.Release(old)
			vm.push(val) // Assignment expression result
			return nil
		} else if mapObj, ok := collectionVal.Obj.(*value.ObjMap); ok {
			var key interface{}
			if indexVal.Type == value.VAL_INT {
				key = indexVal.Int()
			} else if indexVal.Type == value.VAL_OBJ {
				if str, ok := indexVal.Obj.(string); ok {
					key = str
				} else {
					return vm.runtimeError(c, ip, "map key must be int or string")
				}
			} else {
				return vm.runtimeError(c, ip, "map key must be int or string")
			}
			// Guard do slot ref (spec §6.3): valor `ref T` (tag
			// RuntimeType) so aceita ref/null.
			if val.Type != value.VAL_REF && val.Type != value.VAL_NULL && mapValueIsRefSlot(mapObj) {
				return vm.runtimeError(c, ip, "%s", refSlotWriteError(mapObj.RuntimeType.Load().Value.String(), val))
			}
			// RC: so libera o velho se a chave ja existia (dec a
			// menos e proibido); retain-antes-de-release quando existe.
			if old, exists := mapObj.Get(key); exists {
				value.Retain(val)
				mapObj.Set(key, val)
				value.Release(old)
			} else {
				value.Retain(val)
				mapObj.Set(key, val)
			}
			vm.push(val)
			return nil
		}
	}
	return vm.runtimeError(c, ip, "cannot set index on non-array/map")
}
```

(Conferir linha a linha contra o `case` atual antes de apagar o original — a cópia tem de ser literal.)

Em `executor.go`, o `case chunk.OP_SET_INDEX:` inteiro vira:

```go
		case chunk.OP_SET_INDEX:
			if err := vm.setIndexGeneric(c, ip); err != nil {
				return err
			}
```

- [ ] **Step 4: `unicizeOwnedSlot` / `unicizeBorrowedSlot`**

Em `internal/vm/cow.go`, depois de `unicize`:

```go
// unicizeOwnedSlot e a semantica de OP_GET_LOCAL_MUT para um slot POSSUIDOR:
// se o composto no slot esta compartilhado, grava um clone no slot (ownSlot
// mantem o slot registrado em frame.Owned, como OP_SET_LOCAL) e solta o
// velho; devolve o ocupante (unico) do slot. Metodo para ser o funil comum do
// case generico e do fallback de OP_SET_LOCAL_INDEX_ARRAY_NORC (issue #66).
func (vm *VM) unicizeOwnedSlot(frame *CallFrame, idx int) value.Value {
	v := vm.stack[idx]
	if value.IsShared(v) {
		old := v
		v = vm.copyValue(v)
		vm.stack[idx] = v
		// RC: usa ownSlot (mantem o slot registrado em frame.Owned)
		// em vez de Retain cru — mesmo padrao do OP_SET_LOCAL.
		frame.ownSlot(vm, idx)
		value.Release(old)
	}
	return v
}

// unicizeBorrowedSlot e o gemeo de EMPRESTIMO (OP_GET_LOCAL_MUT_BORROW): slot
// de tipo `ref T` nao possui o que guarda, entao o clone fica no slot sem
// retain do novo nem release do velho (soltar o que nunca se reteve e dec a
// menos). Fallback de OP_SET_REF_LOCAL_INDEX_ARRAY_NORC quando o slot guarda
// um valor plano (tolerancia herdada do auto-deref antigo).
func (vm *VM) unicizeBorrowedSlot(idx int) value.Value {
	v := vm.stack[idx]
	if value.IsShared(v) {
		v = vm.copyValue(v)
		vm.stack[idx] = v
	}
	return v
}
```

Em `executor.go`, os dois cases passam a:

```go
		case chunk.OP_GET_LOCAL_MUT:
			slot := c.Code[ip]
			ip++
			vm.push(vm.unicizeOwnedSlot(frame, frame.LocalBase+int(slot)))

		case chunk.OP_GET_LOCAL_MUT_BORROW:
			slot := c.Code[ip]
			ip++
			// RC: gemeo de EMPRESTIMO do acima, emitido quando o tipo declarado
			// do local e `ref T` — ver unicizeBorrowedSlot (cow.go).
			vm.push(vm.unicizeBorrowedSlot(frame.LocalBase + int(slot)))
```

(Mover os comentários longos de RC dos dois cases para os métodos, como acima.)

- [ ] **Step 5: Rótulo `redispatch` e os seis handlers**

Em `run()`, o topo do laço passa a:

```go
	for {
		if ip >= len(c.Code) {
			return nil
		}

		instruction := chunk.OpCode(c.Code[ip])
		ip++

		// redispatch: os opcodes de indexacao tipada (issue #66) re-entram aqui
		// com `instruction` trocada pelo generico quando o container nao e o
		// array que o tipo estatico prometia (null por `any`, ref de outro
		// tipo) — depois de materializar na pilha exatamente o que a sequencia
		// generica teria. Zero custo no caminho comum; so os fallbacks saltam.
	redispatch:
		switch instruction {
```

E, ao fim do `switch` (depois do `case chunk.OP_COPY:`), os seis cases:

```go
		// perf issue #66 (item 1): indexacao tipada de array. Caminho rapido
		// grava o resultado NO LUGAR na pilha (sem pop/push); mensagens de erro
		// identicas as do generico; container inesperado cai no generico.
		case chunk.OP_GET_INDEX_ARRAY:
			top := vm.stackTop
			if arr, ok := vm.stack[top-2].Obj.(*value.ObjArray); ok {
				indexVal := vm.stack[top-1]
				if indexVal.Type != value.VAL_INT {
					return vm.runtimeError(c, ip, "array index must be integer")
				}
				idx := int(indexVal.Int())
				if idx < 0 || idx >= len(arr.Elements) {
					return vm.runtimeError(c, ip, "array index out of bounds")
				}
				vm.stack[top-2] = arr.Elements[idx]
				vm.stack[top-1] = value.Value{}
				vm.stackTop = top - 1
				continue
			}
			instruction = chunk.OP_GET_INDEX
			goto redispatch

		case chunk.OP_GET_LOCAL_INDEX_ARRAY:
			slot := c.Code[ip]
			ip++
			if arr, ok := vm.stack[frame.LocalBase+int(slot)].Obj.(*value.ObjArray); ok {
				top := vm.stackTop
				indexVal := vm.stack[top-1]
				if indexVal.Type != value.VAL_INT {
					return vm.runtimeError(c, ip, "array index must be integer")
				}
				idx := int(indexVal.Int())
				if idx < 0 || idx >= len(arr.Elements) {
					return vm.runtimeError(c, ip, "array index out of bounds")
				}
				vm.stack[top-1] = arr.Elements[idx]
				continue
			}
			// Fallback: a sequencia generica GET_LOCAL + GET_INDEX.
			indexVal := vm.pop()
			vm.push(vm.stack[frame.LocalBase+int(slot)])
			vm.push(indexVal)
			instruction = chunk.OP_GET_INDEX
			goto redispatch

		case chunk.OP_GET_REF_LOCAL_INDEX_ARRAY:
			// O slot guarda o ref de um parametro `ref T[]`. REF_UPVALUE (a
			// forma que OP_REF_LOCAL cria para todo ref a local) resolve com
			// uma Load() da caixa em vez de referenceStorage (defer + closure
			// do setter + reflect). arr != nil espelha o validateReferencedValue
			// do caminho generico (typed nil cairia no erro de la).
			slot := c.Code[ip]
			ip++
			refVal := vm.stack[frame.LocalBase+int(slot)]
			if ref, ok := refVal.Obj.(*value.ObjRef); ok && ref.RefType == value.REF_UPVALUE {
				if stored, ok := ref.Upvalue.Load(); ok {
					if arr, ok := stored.Obj.(*value.ObjArray); ok && arr != nil {
						top := vm.stackTop
						indexVal := vm.stack[top-1]
						if indexVal.Type != value.VAL_INT {
							return vm.runtimeError(c, ip, "array index must be integer")
						}
						idx := int(indexVal.Int())
						if idx < 0 || idx >= len(arr.Elements) {
							return vm.runtimeError(c, ip, "array index out of bounds")
						}
						vm.stack[top-1] = arr.Elements[idx]
						continue
					}
				}
			}
			// Fallback: GET_LOCAL + OP_DEREF (null e nao-ref passam; ref
			// resolve) + GET_INDEX.
			container := refVal
			if refVal.Type == value.VAL_REF {
				resolved, err := vm.resolveReferenceValue(refVal)
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				container = resolved
			}
			indexVal := vm.pop()
			vm.push(container)
			vm.push(indexVal)
			instruction = chunk.OP_GET_INDEX
			goto redispatch

		case chunk.OP_SET_INDEX_ARRAY_NORC:
			// [arr, i, v] -> []. Pula Retain/Release SO se valor novo e velho
			// sao comprovadamente sem contador e o array nao e (ref T)[] —
			// senao o generico (setIndexGeneric) decide, como OP_SET_INDEX +
			// OP_POP fariam.
			top := vm.stackTop
			if arr, ok := vm.stack[top-3].Obj.(*value.ObjArray); ok {
				indexVal := vm.stack[top-2]
				if indexVal.Type != value.VAL_INT {
					return vm.runtimeError(c, ip, "array index must be integer")
				}
				idx := int(indexVal.Int())
				if idx < 0 || idx >= len(arr.Elements) {
					return vm.runtimeError(c, ip, "array index out of bounds")
				}
				val := vm.stack[top-1]
				if value.NeverTracked(val) && value.NeverTracked(arr.Elements[idx]) && !arrayTagIsRefSlot(arr.RuntimeType.Load()) {
					arr.Elements[idx] = val
					vm.stack[top-1] = value.Value{}
					vm.stack[top-2] = value.Value{}
					vm.stack[top-3] = value.Value{}
					vm.stackTop = top - 3
					continue
				}
			}
			if err := vm.setIndexGeneric(c, ip); err != nil {
				return err
			}
			vm.LastPopped = vm.pop()

		case chunk.OP_SET_LOCAL_INDEX_ARRAY_NORC:
			// [i, v] -> []; container no slot do local possuidor. Caminho
			// rapido = array unico (Owners <= 1 e o teste de IsShared, sem a
			// chamada) + elemento sem contador; senao a sequencia generica
			// GET_LOCAL_MUT + SET_INDEX + POP (unicizeOwnedSlot clona e
			// registra posse exatamente como o case generico).
			slot := c.Code[ip]
			ip++
			localIdx := frame.LocalBase + int(slot)
			top := vm.stackTop
			if arr, ok := vm.stack[localIdx].Obj.(*value.ObjArray); ok && arr.Owners.Load() <= 1 {
				indexVal := vm.stack[top-2]
				if indexVal.Type != value.VAL_INT {
					return vm.runtimeError(c, ip, "array index must be integer")
				}
				idx := int(indexVal.Int())
				if idx < 0 || idx >= len(arr.Elements) {
					return vm.runtimeError(c, ip, "array index out of bounds")
				}
				val := vm.stack[top-1]
				if value.NeverTracked(val) && value.NeverTracked(arr.Elements[idx]) && !arrayTagIsRefSlot(arr.RuntimeType.Load()) {
					arr.Elements[idx] = val
					vm.stack[top-1] = value.Value{}
					vm.stack[top-2] = value.Value{}
					vm.stackTop = top - 2
					continue
				}
			}
			val := vm.pop()
			indexVal := vm.pop()
			vm.push(vm.unicizeOwnedSlot(frame, localIdx))
			vm.push(indexVal)
			vm.push(val)
			if err := vm.setIndexGeneric(c, ip); err != nil {
				return err
			}
			vm.LastPopped = vm.pop()

		case chunk.OP_SET_REF_LOCAL_INDEX_ARRAY_NORC:
			// [i, v] -> []; o slot guarda o ref de um parametro `ref T[]`
			// (slot de emprestimo). Caminho rapido = REF_UPVALUE cujo array e
			// unico + elemento sem contador; senao GET_LOCAL_MUT_BORROW +
			// DEREF_MUT (unicizeThroughRefValue clona e grava de volta pelo
			// setter do ref) + SET_INDEX + POP.
			slot := c.Code[ip]
			ip++
			localIdx := frame.LocalBase + int(slot)
			refVal := vm.stack[localIdx]
			top := vm.stackTop
			if ref, ok := refVal.Obj.(*value.ObjRef); ok && ref.RefType == value.REF_UPVALUE {
				if stored, ok := ref.Upvalue.Load(); ok {
					if arr, ok := stored.Obj.(*value.ObjArray); ok && arr != nil && arr.Owners.Load() <= 1 {
						indexVal := vm.stack[top-2]
						if indexVal.Type != value.VAL_INT {
							return vm.runtimeError(c, ip, "array index must be integer")
						}
						idx := int(indexVal.Int())
						if idx < 0 || idx >= len(arr.Elements) {
							return vm.runtimeError(c, ip, "array index out of bounds")
						}
						val := vm.stack[top-1]
						if value.NeverTracked(val) && value.NeverTracked(arr.Elements[idx]) && !arrayTagIsRefSlot(arr.RuntimeType.Load()) {
							arr.Elements[idx] = val
							vm.stack[top-1] = value.Value{}
							vm.stack[top-2] = value.Value{}
							vm.stackTop = top - 2
							continue
						}
					}
				}
			}
			val := vm.pop()
			indexVal := vm.pop()
			var container value.Value
			if refVal.Type == value.VAL_REF {
				uniq, err := vm.unicizeThroughRefValue(refVal)
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				container = uniq
			} else {
				container = vm.unicizeBorrowedSlot(localIdx)
			}
			vm.push(container)
			vm.push(indexVal)
			vm.push(val)
			if err := vm.setIndexGeneric(c, ip); err != nil {
				return err
			}
			vm.LastPopped = vm.pop()
```

E em `internal/vm/ref_slots.go`, ao lado de `arrayElementIsRefSlot`, a forma por tag (barata o bastante para inline em `run()`):

```go
// arrayTagIsRefSlot e arrayElementIsRefSlot sobre a tag ja carregada — a
// forma que os opcodes NORC da indexacao tipada usam no caminho quente
// (uma Load() feita pelo chamador, sem a chamada).
func arrayTagIsRefSlot(tag *value.RuntimeTypeInfo) bool {
	return tag != nil && tag.Kind == value.TYPE_ARRAY && tag.Element != nil && tag.Element.Kind == value.TYPE_REF
}
```

- [ ] **Step 6: Rodar os testes novos e os guards**

Run: `go test ./internal/vm -run 'TypedIndex|LocalIndexArray|IndexArray|RefLocalIndex|Inline'`
Expected: PASS. Conferir no relatório do inliner que o caminho quente só chama `Upvalue.Load`, `NeverTracked` (inline), `arrayTagIsRefSlot` (inline, custo ≤ 20 — se não inlinar, escrever a condição inline no handler):

```bash
go build -gcflags=-m=2 ./internal/vm 2>&1 | grep -E "can inline (arrayTagIsRefSlot|\(\*VM\)\.(push|pop))|inlining call to value.NeverTracked" | sort | uniq -c | head
```

- [ ] **Step 7: Guard de sites de `NeverTracked`**

Em `TestPushStaysInlinedInsideRun` (`inline_guard_test.go`), depois do bloco de `pop`:

```go
	// NeverTracked e chamada no caminho quente dos tres opcodes NORC (issue
	// #66); se sair do inline, cada escrita tipada paga uma chamada.
	neverTrackedInlined := 0
	for _, line := range strings.Split(report, "\n") {
		if strings.Contains(line, "executor.go") && strings.Contains(line, "inlining call to value.NeverTracked") {
			neverTrackedInlined++
		}
	}
	if neverTrackedInlined < 6 {
		t.Errorf("value.NeverTracked foi inlinada em %d sites de executor.go, esperado >= 6 (dois por opcode NORC)", neverTrackedInlined)
	}
```

Run: `go test ./internal/vm -run Inline` → PASS.

- [ ] **Step 8: Suíte inteira do vm e gofmt/vet**

Run: `gofmt -l internal/vm internal/value; go vet ./internal/vm ./internal/value; go test ./internal/vm ./internal/value`
Expected: sem arquivos listados, vet limpo, PASS.

- [ ] **Step 9: Commit e binário de estágio s0**

```bash
git add internal/vm/index_ops.go internal/vm/cow.go internal/vm/executor.go internal/vm/ref_slots.go internal/vm/typed_index_test.go internal/vm/inline_guard_test.go
git commit -m "perf(vm): handlers da indexacao tipada de array — caminho rapido no lugar, fallback exato por redispatch (leitura) e setIndexGeneric (escrita), unicizeOwnedSlot/BorrowedSlot (issue #66, item 1)"
go build -o "$S/bench/noxy_s0.exe" ./cmd/noxy
```

(`$S` = scratchpad, ver Global Constraints.) Nenhum bytecode novo é emitido ainda: `noxy_s0.exe` mede só o custo dos refactors (`setIndexGeneric` como chamada; `unicize*Slot`).

---

### Task 4: Compilador — formas genéricas `OP_GET_INDEX_ARRAY` / `OP_SET_INDEX_ARRAY_NORC`

**Files:**
- Create: `internal/compiler/typed_index.go` (`isUntrackedElementType`, `arrayTypeOf`; `isSideEffectFree` entra na Task 5)
- Modify: `internal/compiler/compiler.go` (`case *ast.IndexExpression:` ~l.1090; atribuição a `IndexExpression` ~l.772)
- Create: `internal/compiler/typed_index_compile_test.go`
- Create: `internal/vm/typed_index_e2e_test.go`

**Interfaces:**
- Produces: `func isUntrackedElementType(t ast.NoxyType) bool` (PrimitiveType com Name em int/float/bool/string/bytes); `func arrayTypeOf(t ast.NoxyType) (*ast.ArrayType, bool)` (desembrulha um nível de `RefType`).
- Test helper (pacote compiler): `func opcodeNames(t *testing.T, code *chunk.Chunk) []string` — nomes dos opcodes na ordem, lidos do disassembler.

- [ ] **Step 1: Testes de bytecode (falham: genéricos emitidos)**

Criar `internal/compiler/typed_index_compile_test.go`:

```go
package compiler

import (
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"noxy-vm/internal/chunk"
)

var opNamePattern = regexp.MustCompile(`\bOP_[A-Z_]+\b`)

// opcodeNames devolve os nomes dos opcodes de um chunk NA ORDEM, lidos da
// saida do disassembler (que conhece a largura de cada instrucao) —
// containsOpcode varre bytes e confundiria um operando com um opcode.
func opcodeNames(t *testing.T, code *chunk.Chunk) []string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan string)
	go func() {
		out, _ := io.ReadAll(reader)
		done <- string(out)
	}()
	previous := os.Stdout
	os.Stdout = writer
	code.Disassemble("t")
	_ = writer.Close()
	os.Stdout = previous
	return opNamePattern.FindAllString(<-done, -1)
}

func functionOpcodes(t *testing.T, source, name string) []string {
	t.Helper()
	fn := compiledFunction(t, source, name)
	return opcodeNames(t, fn.Chunk.(*chunk.Chunk))
}

func topLevelOpcodes(t *testing.T, source string) []string {
	t.Helper()
	code, _, err := New().Compile(parse(source))
	if err != nil {
		t.Fatal(err)
	}
	return opcodeNames(t, code)
}

func assertHas(t *testing.T, ops []string, want string) {
	t.Helper()
	if !slices.Contains(ops, want) {
		t.Fatalf("esperava %s no bytecode, obtido: %s", want, strings.Join(ops, " "))
	}
}

func assertLacks(t *testing.T, ops []string, unwanted string) {
	t.Helper()
	if slices.Contains(ops, unwanted) {
		t.Fatalf("nao esperava %s no bytecode, obtido: %s", unwanted, strings.Join(ops, " "))
	}
}

// Base T[] em posicao generica (global): leitura tipada, escrita NORC sem OP_POP.
func TestGlobalArrayUsesTypedIndexOpcodes(t *testing.T) {
	ops := topLevelOpcodes(t, "let xs: int[] = [1, 2]\nlet i: int = 0\nxs[i] = xs[i] + 1\n")
	assertHas(t, ops, "OP_GET_INDEX_ARRAY")
	assertHas(t, ops, "OP_SET_INDEX_ARRAY_NORC")
	assertLacks(t, ops, "OP_GET_INDEX")
	assertLacks(t, ops, "OP_SET_INDEX")
	// O OP_POP que seguia OP_SET_INDEX nao existe mais: o unico OP_POP
	// restante e de outra statement, nao o da atribuicao indexada.
	for k, op := range ops {
		if op == "OP_SET_INDEX_ARRAY_NORC" && k+1 < len(ops) && ops[k+1] == "OP_POP" {
			t.Fatalf("OP_SET_INDEX_ARRAY_NORC seguido de OP_POP: a forma NORC nao empilha")
		}
	}
}

// Nested: o nivel de fora e MUT generico, o de dentro e tipado.
func TestNestedArrayWriteUsesTypedInnerOpcode(t *testing.T) {
	ops := functionOpcodes(t, `
func f(g: int[][]) -> int
    g[0][1] = g[1][0] + 1
    return g[0][1]
end
`, "f")
	assertHas(t, ops, "OP_GET_INDEX_MUT")
	assertHas(t, ops, "OP_SET_INDEX_ARRAY_NORC")
	assertHas(t, ops, "OP_GET_INDEX_ARRAY")
	assertLacks(t, ops, "OP_SET_INDEX")
}

// Elemento composto: escrita segue OP_SET_INDEX (RC do generico); leitura e tipada.
func TestCompositeElementKeepsGenericSetIndex(t *testing.T) {
	ops := functionOpcodes(t, `
struct P
    x: int
end
func f(ps: P[][]) -> P
    ps[0][0] = P(1)
    return ps[0][0]
end
`, "f")
	assertHas(t, ops, "OP_SET_INDEX")
	assertLacks(t, ops, "OP_SET_INDEX_ARRAY_NORC")
	assertHas(t, ops, "OP_GET_INDEX_ARRAY")
}

// Map e any: tudo generico.
func TestMapAndAnyKeepGenericIndexOpcodes(t *testing.T) {
	ops := functionOpcodes(t, `
func f(m: map[string, int], a: any) -> int
    m["k"] = a[0]
    return m["k"]
end
`, "f")
	assertHas(t, ops, "OP_GET_INDEX")
	assertHas(t, ops, "OP_SET_INDEX")
	assertLacks(t, ops, "OP_GET_INDEX_ARRAY")
	assertLacks(t, ops, "OP_SET_INDEX_ARRAY_NORC")
}

// Upvalue de tipo T[] (closure): forma generica tipada, nunca a fundida por slot.
func TestUpvalueArrayUsesGenericTypedOpcodes(t *testing.T) {
	ops := functionOpcodes(t, `
func outer() -> int
    let xs: int[] = [1, 2]
    func inner() -> int
        xs[0] = 5
        return xs[1]
    end
    return inner()
end
`, "inner")
	assertHas(t, ops, "OP_GET_UPVALUE_MUT")
	assertHas(t, ops, "OP_SET_INDEX_ARRAY_NORC")
	assertHas(t, ops, "OP_GET_INDEX_ARRAY")
	assertLacks(t, ops, "OP_GET_LOCAL_INDEX_ARRAY")
	assertLacks(t, ops, "OP_SET_LOCAL_INDEX_ARRAY_NORC")
}
```

Run: `go test ./internal/compiler -run 'TypedIndex|GlobalArray|NestedArray|CompositeElement|MapAndAny|UpvalueArray'`
Expected: FAIL (OP_GET_INDEX/OP_SET_INDEX genéricos).

- [ ] **Step 2: Helpers do compilador**

Criar `internal/compiler/typed_index.go`:

```go
package compiler

import "noxy-vm/internal/ast"

// Indexacao tipada de array (issue #66, item 1): o compilador sabe quando a
// base e T[] e emite os opcodes especializados de internal/chunk. Este arquivo
// reune os predicados da decisao; a emissao mora em compiler.go (leitura em
// `case *ast.IndexExpression`, escrita na atribuicao a IndexExpression e no
// for-each).

// isUntrackedElementType responde se um ELEMENTO desse tipo estatico nunca
// tem contador RC — os unicos casos em que a escrita pode usar a forma NORC.
// Lista fechada por nome: struct tambem e PrimitiveType (pelo nome da
// declaracao) e `any` pode guardar composto, entao ambos respondem false.
func isUntrackedElementType(t ast.NoxyType) bool {
	prim, ok := t.(*ast.PrimitiveType)
	if !ok {
		return false
	}
	switch prim.Name {
	case "int", "float", "bool", "string", "bytes":
		return true
	}
	return false
}

// arrayTypeOf desembrulha um nivel de `ref` e devolve o ArrayType da base,
// se for um — o tipo que decide entre OP_GET_INDEX_ARRAY e OP_GET_INDEX.
func arrayTypeOf(t ast.NoxyType) (*ast.ArrayType, bool) {
	if ref, ok := t.(*ast.RefType); ok {
		t = ref.ElementType
	}
	arr, ok := t.(*ast.ArrayType)
	return arr, ok
}
```

- [ ] **Step 3: Emissão na leitura**

Em `compiler.go`, `case *ast.IndexExpression:`, trocar

```go
		c.emitByte(byte(chunk.OP_GET_INDEX))
```

por

```go
		// perf #66: base estaticamente T[] (ou ref T[], ja dereferenciada
		// acima) indexa sem despacho dinamico; o VM cai no OP_GET_INDEX
		// generico se o container em runtime nao for array.
		if _, isArray := arrayTypeOf(leftType); isArray {
			c.emitByte(byte(chunk.OP_GET_INDEX_ARRAY))
		} else {
			c.emitByte(byte(chunk.OP_GET_INDEX))
		}
```

(`leftType` ali ainda é o tipo antes do desembrulho feito logo abaixo — `arrayTypeOf` desembrulha.)

- [ ] **Step 4: Emissão na escrita**

Na atribuição a `IndexExpression` (~l.772), trocar

```go
			c.emitByte(byte(chunk.OP_SET_INDEX))
			c.emitByte(byte(chunk.OP_POP))
```

por

```go
			// perf #66: elemento sem contador RC em base T[] escreve pela forma
			// NORC, que e statement (nao empilha; sem OP_POP). O VM confere em
			// runtime antes de pular Retain/Release.
			if arrType, isArray := leftType.(*ast.ArrayType); isArray && isUntrackedElementType(arrType.ElementType) {
				c.emitByte(byte(chunk.OP_SET_INDEX_ARRAY_NORC))
			} else {
				c.emitByte(byte(chunk.OP_SET_INDEX))
				c.emitByte(byte(chunk.OP_POP))
			}
```

(`leftType` nesse ponto já foi desembrulhado de `ref` pelo código existente.)

- [ ] **Step 5: Rodar os testes de bytecode**

Run: `go test ./internal/compiler -run 'TypedIndex|GlobalArray|NestedArray|CompositeElement|MapAndAny|UpvalueArray'`
Expected: PASS.

- [ ] **Step 6: Testes ponta a ponta (comportamento e erros idênticos)**

Criar `internal/vm/typed_index_e2e_test.go`:

```go
package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

// Ponta a ponta (fonte -> compilador -> VM) da indexacao tipada de array
// (issue #66, item 1): mesmo resultado, mesma semantica CoW/RC e mesmas
// mensagens de erro do caminho generico. Cada caso "tipado" tem o gemeo
// "any" (base dinamica), que continua no generico: os dois tem de concordar.

func TestTypedIndexGlobalArrayReadWrite(t *testing.T) {
	got := captureVMSource(t, `
let xs: int[] = [1, 2, 3]
let i: int = 1
xs[i] = xs[i] * 10
xs[0] = xs[2]
test_report(xs)
`)
	arr := got.Obj.(*value.ObjArray)
	if arr.Elements[0].Int() != 3 || arr.Elements[1].Int() != 20 || arr.Elements[2].Int() != 3 {
		t.Fatalf("esperado [3, 20, 3], obtido %s", got.String())
	}
}

func TestTypedIndexNestedAndStringElements(t *testing.T) {
	got := captureVMSource(t, `
func f() -> string
    let g: string[][] = [["a", "b"], ["c", "d"]]
    g[0][1] = g[1][0] + "!"
    return g[0][1] + g[1][1]
end
test_report(f())
`)
	if s, ok := got.Obj.(string); !ok || s != "c!d" {
		t.Fatalf("esperado \"c!d\", obtido %s", got.String())
	}
}

// Elemento composto vindo por `any` numa base int[]: o NORC ve que o valor e
// rastreado e cai no generico, que retem — o composto ganha o dono do
// elemento, como no caminho generico.
func TestTypedIndexNorcRetainsCompositeFromAny(t *testing.T) {
	got := captureVMSource(t, `
let inner: int[] = [7]
let xs: int[] = [1, 2]
let v: any = inner
xs[0] = v
test_report(xs)
`)
	outer := got.Obj.(*value.ObjArray)
	if value.OwnersCount(outer.Elements[0]) < 2 {
		t.Fatalf("composto escrito via any deve ter o dono do elemento alem do global: owners=%d", value.OwnersCount(outer.Elements[0]))
	}
}

func TestTypedIndexErrorsMatchGenericPath(t *testing.T) {
	cases := []struct{ name, typed, dynamic, want string }{
		{"leitura fora da faixa", "let a: int[] = [1]\nlet i: int = 5\nprint(a[i])\n", "let a: any = [1]\nlet i: int = 5\nprint(a[i])\n", "array index out of bounds"},
		{"escrita fora da faixa", "let a: int[] = [1]\nlet i: int = 5\na[i] = 2\n", "let a: any = [1]\nlet i: int = 5\na[i] = 2\n", "array index out of bounds"},
		{"indice nao inteiro via any", "let a: int[] = [1]\nlet i: any = \"x\"\nprint(a[i])\n", "let a: any = [1]\nlet i: any = \"x\"\nprint(a[i])\n", "array index must be integer"},
		{"escrita com indice nao inteiro via any", "let a: int[] = [1]\nlet i: any = \"x\"\na[i] = 2\n", "let a: any = [1]\nlet i: any = \"x\"\na[i] = 2\n", "array index must be integer"},
		{"nested fora da faixa", "let a: int[][] = [[1]]\nlet i: int = 5\na[0][i] = 1\n", "let a: any = [[1]]\nlet i: int = 5\na[0][i] = 1\n", "array index out of bounds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			typedErr := interpretVMSource(t, New(), tc.typed)
			dynErr := interpretVMSource(t, New(), tc.dynamic)
			if typedErr == nil || !strings.Contains(typedErr.Error(), tc.want) {
				t.Fatalf("tipado: esperava %q, obtido %v", tc.want, typedErr)
			}
			if dynErr == nil || !strings.Contains(dynErr.Error(), tc.want) {
				t.Fatalf("dinamico: esperava %q, obtido %v", tc.want, dynErr)
			}
		})
	}
}
```

Run: `go test ./internal/vm -run TypedIndex`
Expected: PASS.

- [ ] **Step 7: Suíte e commit; binário s1**

Run: `gofmt -l internal/compiler internal/vm; go vet ./internal/compiler ./internal/vm; go test ./internal/compiler ./internal/vm ./internal/chunk`
Expected: PASS.

```bash
git add internal/compiler/typed_index.go internal/compiler/compiler.go internal/compiler/typed_index_compile_test.go internal/vm/typed_index_e2e_test.go
git commit -m "perf(compiler): emite OP_GET_INDEX_ARRAY / OP_SET_INDEX_ARRAY_NORC quando a base e estaticamente T[] e o elemento nao tem contador RC (issue #66, item 1, estagio 1)"
go build -o "$S/bench/noxy_s1.exe" ./cmd/noxy
```

---

### Task 5: Compilador — formas fundidas por slot de local plano e for-each

**Files:**
- Modify: `internal/compiler/typed_index.go` (`isSideEffectFree`, `fusedLocalIndexRead`, `tryFuseLocalIndexAssign`)
- Modify: `internal/compiler/compiler.go` (`case *ast.IndexExpression:` início; atribuição a `IndexExpression` início; for-each ~l.1657)
- Modify: `internal/compiler/typed_index_compile_test.go`
- Modify: `internal/vm/typed_index_e2e_test.go`

**Interfaces:**
- Produces: `func isSideEffectFree(expr ast.Expression) bool`; `func (c *Compiler) fusedLocalIndexRead(n *ast.IndexExpression) (op chunk.OpCode, slot int, elem ast.NoxyType, ok bool)`; `func (c *Compiler) tryFuseLocalIndexAssign(target *ast.IndexExpression, valueExpr ast.Expression) (bool, error)`. Nesta task só o ramo `ArrayType` dos dois; a Task 6 acrescenta o ramo `RefType{ArrayType}`.

- [ ] **Step 1: Testes de bytecode (falham)**

Acrescentar a `typed_index_compile_test.go`:

```go
// Local plano T[] com indice puro: forma fundida por slot, sem GET_LOCAL do
// array e sem OP_POP depois da escrita.
func TestLocalArrayFusesIndexIntoSlotForm(t *testing.T) {
	ops := functionOpcodes(t, `
func f() -> int
    let xs: int[] = [1, 2, 3]
    let i: int = 1
    xs[i + 1] = xs[i] + xs[0]
    return xs[2]
end
`, "f")
	assertHas(t, ops, "OP_GET_LOCAL_INDEX_ARRAY")
	assertHas(t, ops, "OP_SET_LOCAL_INDEX_ARRAY_NORC")
	assertLacks(t, ops, "OP_GET_INDEX_ARRAY")
	assertLacks(t, ops, "OP_SET_INDEX_ARRAY_NORC")
	assertLacks(t, ops, "OP_GET_LOCAL_MUT")
	for k, op := range ops {
		if op == "OP_SET_LOCAL_INDEX_ARRAY_NORC" && k+1 < len(ops) && ops[k+1] == "OP_POP" {
			t.Fatalf("forma fundida seguida de OP_POP")
		}
	}
}

// Indice ou valor com chamada: a forma fundida NAO sai (le o slot depois de
// avaliar os operandos; uma chamada poderia rebindar o local via closure ou
// ref). Fica a forma generica tipada, com o container avaliado primeiro.
func TestLocalArrayDoesNotFuseWhenOperandHasCall(t *testing.T) {
	ops := functionOpcodes(t, `
func idx() -> int
    return 0
end
func f() -> int
    let xs: int[] = [1, 2, 3]
    xs[idx()] = 5
    xs[0] = idx()
    return xs[idx()]
end
`, "f")
	assertLacks(t, ops, "OP_GET_LOCAL_INDEX_ARRAY")
	assertLacks(t, ops, "OP_SET_LOCAL_INDEX_ARRAY_NORC")
	assertHas(t, ops, "OP_GET_INDEX_ARRAY")
	assertHas(t, ops, "OP_SET_INDEX_ARRAY_NORC")
	assertHas(t, ops, "OP_GET_LOCAL_MUT")
}

// Parametro T[] (sem ref) e local possuidor: funde.
func TestArrayParameterFusesIndex(t *testing.T) {
	ops := functionOpcodes(t, `
func sum(data: int[]) -> int
    let s: int = 0
    let i: int = 0
    while i < 3 do
        s = s + data[i]
        i = i + 1
    end
    data[0] = s
    return s
end
`, "sum")
	assertHas(t, ops, "OP_GET_LOCAL_INDEX_ARRAY")
	assertHas(t, ops, "OP_SET_LOCAL_INDEX_ARRAY_NORC")
}

// for-each sobre array: o item e lido pela forma fundida no slot $collection.
func TestForEachOverArrayUsesFusedRead(t *testing.T) {
	ops := functionOpcodes(t, `
func f(xs: int[]) -> int
    let s: int = 0
    for x in xs do
        s = s + x
    end
    return s
end
`, "f")
	assertHas(t, ops, "OP_GET_LOCAL_INDEX_ARRAY")
	assertLacks(t, ops, "OP_GET_INDEX")
}

// for-each sobre map continua generico (a colecao iterada e o array de chaves
// sem tipo estatico).
func TestForEachOverMapKeepsGenericRead(t *testing.T) {
	ops := functionOpcodes(t, `
func f(m: map[string, int]) -> int
    let n: int = 0
    for k in m do
        n = n + 1
    end
    return n
end
`, "f")
	assertHas(t, ops, "OP_GET_INDEX")
	assertLacks(t, ops, "OP_GET_LOCAL_INDEX_ARRAY")
}

// Elemento composto em local: leitura funde (leitura nao tem RC), escrita nao.
func TestLocalCompositeArrayFusesReadOnly(t *testing.T) {
	ops := functionOpcodes(t, `
struct P
    x: int
end
func f() -> P
    let ps: P[] = [P(1), P(2)]
    ps[0] = ps[1]
    return ps[0]
end
`, "f")
	assertHas(t, ops, "OP_GET_LOCAL_INDEX_ARRAY")
	assertLacks(t, ops, "OP_SET_LOCAL_INDEX_ARRAY_NORC")
	assertHas(t, ops, "OP_SET_INDEX")
}
```

Run: `go test ./internal/compiler -run 'LocalArray|ArrayParameter|ForEachOver|LocalComposite'`
Expected: FAIL.

- [ ] **Step 2: Predicado e decisões**

Acrescentar a `internal/compiler/typed_index.go` (imports passam a incluir `"fmt"` e `"noxy-vm/internal/chunk"`):

```go
// isSideEffectFree e o predicado sintatico que libera as formas FUNDIDAS por
// slot (spec §3.3): elas leem o slot do local DEPOIS de avaliar indice (e
// valor), entao so valem quando nada ali pode rodar codigo — chamada
// (inclusive builtin e f-string, que viram chamadas), closure, `ref`, literal
// composto, zeros — que rebindasse ou compartilhasse o local no meio da
// statement. Com operandos assim, a ordem observavel e a da sequencia
// generica. Conservador: o que nao esta na lista e impuro.
func isSideEffectFree(expr ast.Expression) bool {
	switch n := expr.(type) {
	case *ast.Identifier, *ast.IntegerLiteral, *ast.FloatLiteral, *ast.StringLiteral,
		*ast.BytesLiteral, *ast.Boolean, *ast.NullLiteral:
		return true
	case *ast.PrefixExpression:
		switch n.Operator {
		case "-", "!", "~", "*":
			return isSideEffectFree(n.Right)
		}
		return false
	case *ast.InfixExpression:
		return isSideEffectFree(n.Left) && isSideEffectFree(n.Right)
	case *ast.IndexExpression:
		return isSideEffectFree(n.Left) && isSideEffectFree(n.Index)
	case *ast.MemberAccessExpression:
		return isSideEffectFree(n.Left)
	}
	return false
}

// fusedLocalIndexRead decide se `n` (X[i]) pode ser lida pela forma fundida
// por slot: X e identificador resolvido a local (slot <= 255) de tipo T[]
// (OP_GET_LOCAL_INDEX_ARRAY) e i e livre de efeito colateral. Devolve o
// opcode, o slot e o tipo do elemento.
func (c *Compiler) fusedLocalIndexRead(n *ast.IndexExpression) (chunk.OpCode, int, ast.NoxyType, bool) {
	ident, ok := n.Left.(*ast.Identifier)
	if !ok {
		return 0, 0, nil, false
	}
	arg, localType := c.resolveLocal(ident.Value)
	if arg == -1 || arg > 255 || !isSideEffectFree(n.Index) {
		return 0, 0, nil, false
	}
	if arr, ok := localType.(*ast.ArrayType); ok {
		return chunk.OP_GET_LOCAL_INDEX_ARRAY, arg, arr.ElementType, true
	}
	return 0, 0, nil, false
}

// tryFuseLocalIndexAssign funde `x[i] = v` quando x e local T[] POSSUIDOR
// (OP_SET_LOCAL_INDEX_ARRAY_NORC), T sem contador RC, e i e v sao livres de
// efeito colateral. Devolve (true, nil) se emitiu; (true, err) num erro de
// compilacao — as MESMAS checagens e mensagens do ramo ArrayType do caminho
// generico, so que sem compileLValueBase antes (para identificador local ele
// nunca erra: so emitiria o GET_LOCAL_MUT que a forma fundida substitui);
// (false, nil) para seguir o caminho generico. Owns continua a fonte de
// verdade (spec CoW-RC §4.2): slot T[] nao-possuidor vai pelo generico.
func (c *Compiler) tryFuseLocalIndexAssign(target *ast.IndexExpression, valueExpr ast.Expression) (bool, error) {
	ident, ok := target.Left.(*ast.Identifier)
	if !ok {
		return false, nil
	}
	arg, localType := c.resolveLocal(ident.Value)
	if arg == -1 || arg > 255 {
		return false, nil
	}
	var op chunk.OpCode
	var arrType *ast.ArrayType
	switch t := localType.(type) {
	case *ast.ArrayType:
		if !c.localOwns(arg) {
			return false, nil
		}
		op, arrType = chunk.OP_SET_LOCAL_INDEX_ARRAY_NORC, t
	default:
		return false, nil
	}
	if !isUntrackedElementType(arrType.ElementType) || !isSideEffectFree(target.Index) || !isSideEffectFree(valueExpr) {
		return false, nil
	}
	_, idxType, err := c.Compile(target.Index)
	if err != nil {
		return true, err
	}
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
	if idxType != nil && idxType.String() != "int" {
		return true, fmt.Errorf("[line %d] array index must be int, got %s", c.currentLine, idxType.String())
	}
	if !c.areTypesCompatible(arrType.ElementType, valType) {
		return true, fmt.Errorf("[line %d] type mismatch in array assignment: expected %s, got %s%s", c.currentLine, arrType.ElementType.String(), valType.String(), c.derefReadHint(arrType.ElementType, valType, valueExpr))
	}
	if err := c.emitRuntimeValueType(arrType.ElementType); err != nil {
		return true, err
	}
	c.emitBytes(byte(op), byte(arg))
	return true, nil
}
```

- [ ] **Step 3: Ligar no compilador**

Em `compiler.go`, início de `case *ast.IndexExpression:` (antes de `_, leftType, err := c.Compile(n.Left)`):

```go
	case *ast.IndexExpression:
		// perf #66: local T[] (ou ref T[]) com indice puro le pela forma
		// fundida por slot — sem empilhar o Value do array.
		if op, slot, elem, ok := c.fusedLocalIndexRead(n); ok {
			_, idxType, err := c.Compile(n.Index)
			if err != nil {
				return nil, nil, err
			}
			if _, isRef := idxType.(*ast.RefType); isRef {
				c.emitByte(byte(chunk.OP_DEREF))
			}
			c.emitBytes(byte(op), byte(slot))
			return c.currentChunk, elem, nil
		}
```

Na atribuição, logo depois de `} else if indexExp, ok := n.Target.(*ast.IndexExpression); ok {` e antes do comentário "Array/Map Assignment":

```go
			// perf #66: `x[i] = v` com x local T[] (ou ref T[]), elemento sem
			// contador RC e operandos puros — forma fundida por slot, statement.
			if fused, err := c.tryFuseLocalIndexAssign(indexExp, n.Value); fused {
				if err != nil {
					return nil, nil, err
				}
				return c.currentChunk, nil, nil
			}
```

No for-each (~l.1657), trocar

```go
		c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(len(c.locals)-3)) // $collection
		c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(len(c.locals)-2)) // $index
		c.emitByte(byte(chunk.OP_GET_INDEX))
```

por

```go
		if _, isArray := colType.(*ast.ArrayType); isArray {
			// perf #66: leitura fundida — o indice vai para a pilha e o array
			// vem do slot $collection, sem empilhar o seu Value.
			c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(len(c.locals)-2))             // $index
			c.emitBytes(byte(chunk.OP_GET_LOCAL_INDEX_ARRAY), byte(len(c.locals)-3)) // $collection
		} else {
			c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(len(c.locals)-3)) // $collection
			c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(len(c.locals)-2)) // $index
			c.emitByte(byte(chunk.OP_GET_INDEX))
		}
```

- [ ] **Step 4: Rodar os testes de bytecode**

Run: `go test ./internal/compiler`
Expected: PASS (inclusive os testes existentes de CoW/bytecode).

- [ ] **Step 5: Testes ponta a ponta**

Acrescentar a `internal/vm/typed_index_e2e_test.go`:

```go
func TestTypedIndexLocalBubbleSortByValue(t *testing.T) {
	got := captureVMSource(t, `
func sorted() -> int[]
    let data: int[] = [5, 1, 4, 2, 3]
    let n: int = length(data)
    let i: int = 0
    while i < n do
        let j: int = 0
        while j < n - i - 1 do
            if data[j] > data[j + 1] then
                let tmp: int = data[j]
                data[j] = data[j + 1]
                data[j + 1] = tmp
            end
            j = j + 1
        end
        i = i + 1
    end
    return data
end
test_report(sorted())
`)
	arr := got.Obj.(*value.ObjArray)
	for k := range 5 {
		if arr.Elements[k].Int() != int64(k+1) {
			t.Fatalf("esperado [1..5], obtido %s", got.String())
		}
	}
}

// CoW: escrita fundida num local cujo array esta compartilhado clona — a copia
// nao ve a mutacao e o clone e exatamente um.
func TestTypedIndexLocalWriteClonesSharedArray(t *testing.T) {
	ResetCloneCount()
	got := captureVMSource(t, `
func f() -> int[]
    let a: int[] = [1, 2, 3]
    let b: int[] = a
    a[0] = 99
    return [a[0], b[0]]
end
test_report(f())
`)
	arr := got.Obj.(*value.ObjArray)
	if arr.Elements[0].Int() != 99 || arr.Elements[1].Int() != 1 {
		t.Fatalf("esperado [99, 1] (b intacto), obtido %s", got.String())
	}
	if CloneCountValue() != 1 {
		t.Fatalf("esperava exatamente 1 clone CoW, obtido %d", CloneCountValue())
	}
}

// for-each sobre array com mutacao durante a iteracao: o comportamento de hoje
// (a iteracao continua no mesmo array e ve a escrita) e preservado.
func TestTypedIndexForEachSeesMutationDuringIteration(t *testing.T) {
	got := captureVMSource(t, `
func f() -> int
    let xs: int[] = [1, 2, 3]
    let s: int = 0
    for x in xs do
        if x == 1 then
            xs[2] = 30
        end
        s = s + x
    end
    return s
end
test_report(f())
`)
	if got.Int() != 33 {
		t.Fatalf("esperado 33 (1 + 2 + 30), obtido %s", got.String())
	}
}

func TestTypedIndexLocalErrorsMatchGenericPath(t *testing.T) {
	cases := []struct{ name, typed, dynamic, want string }{
		{"leitura fora da faixa", "func f() -> int\n    let a: int[] = [1]\n    let i: int = 5\n    return a[i]\nend\nprint(f())\n", "func f() -> any\n    let a: any = [1]\n    let i: int = 5\n    return a[i]\nend\nprint(f())\n", "array index out of bounds"},
		{"escrita fora da faixa", "func f() -> int\n    let a: int[] = [1]\n    let i: int = 5\n    a[i] = 2\n    return a[0]\nend\nprint(f())\n", "func f() -> any\n    let a: any = [1]\n    let i: int = 5\n    a[i] = 2\n    return a[0]\nend\nprint(f())\n", "array index out of bounds"},
		{"indice nao inteiro via any", "func f() -> int\n    let a: int[] = [1]\n    let i: any = \"x\"\n    return a[i]\nend\nprint(f())\n", "func f() -> any\n    let a: any = [1]\n    let i: any = \"x\"\n    return a[i]\nend\nprint(f())\n", "array index must be integer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			typedErr := interpretVMSource(t, New(), tc.typed)
			dynErr := interpretVMSource(t, New(), tc.dynamic)
			if typedErr == nil || !strings.Contains(typedErr.Error(), tc.want) {
				t.Fatalf("tipado: esperava %q, obtido %v", tc.want, typedErr)
			}
			if dynErr == nil || !strings.Contains(dynErr.Error(), tc.want) {
				t.Fatalf("dinamico: esperava %q, obtido %v", tc.want, dynErr)
			}
		})
	}
}
```

Run: `go test ./internal/vm -run TypedIndex`
Expected: PASS.

- [ ] **Step 6: Suíte, commit e binário s2**

Run: `gofmt -l internal/compiler internal/vm; go vet ./internal/compiler ./internal/vm; go test ./internal/compiler ./internal/vm ./internal/chunk`
Expected: PASS.

```bash
git add internal/compiler/typed_index.go internal/compiler/compiler.go internal/compiler/typed_index_compile_test.go internal/vm/typed_index_e2e_test.go
git commit -m "perf(compiler): formas fundidas por slot para local T[] — OP_GET_LOCAL_INDEX_ARRAY (inclusive for-each) e OP_SET_LOCAL_INDEX_ARRAY_NORC, so com operandos sem efeito colateral (issue #66, item 1, estagio 2)"
go build -o "$S/bench/noxy_s2.exe" ./cmd/noxy
```

---

### Task 6: Compilador — formas fundidas para local `ref T[]`

**Files:**
- Modify: `internal/compiler/typed_index.go` (`fusedLocalIndexRead`, `tryFuseLocalIndexAssign`: ramo `RefType`)
- Modify: `internal/compiler/typed_index_compile_test.go`
- Modify: `internal/vm/typed_index_e2e_test.go`

**Interfaces:**
- Consumes: tudo das Tasks 3–5.

- [ ] **Step 1: Testes de bytecode (falham)**

Acrescentar a `typed_index_compile_test.go`:

```go
// Parametro `ref T[]`: leitura e escrita pela forma fundida de ref — sem
// OP_DEREF/OP_DEREF_MUT (o opcode resolve a caixa), sem OP_POP.
func TestRefArrayParameterFusesIndex(t *testing.T) {
	ops := functionOpcodes(t, `
func bubble(data: ref int[]) -> void
    let j: int = 0
    if data[j] > data[j + 1] then
        let tmp: int = data[j]
        data[j] = data[j + 1]
        data[j + 1] = tmp
    end
end
`, "bubble")
	assertHas(t, ops, "OP_GET_REF_LOCAL_INDEX_ARRAY")
	assertHas(t, ops, "OP_SET_REF_LOCAL_INDEX_ARRAY_NORC")
	assertLacks(t, ops, "OP_DEREF")
	assertLacks(t, ops, "OP_DEREF_MUT")
	assertLacks(t, ops, "OP_GET_LOCAL_MUT_BORROW")
	assertLacks(t, ops, "OP_GET_INDEX")
	assertLacks(t, ops, "OP_SET_INDEX")
	for k, op := range ops {
		if op == "OP_SET_REF_LOCAL_INDEX_ARRAY_NORC" && k+1 < len(ops) && ops[k+1] == "OP_POP" {
			t.Fatalf("forma fundida de ref seguida de OP_POP")
		}
	}
}

// Com chamada no operando, o ref segue o caminho de hoje (DEREF / DEREF_MUT)
// com os opcodes tipados genericos.
func TestRefArrayParameterDoesNotFuseWithCall(t *testing.T) {
	ops := functionOpcodes(t, `
func g() -> int
    return 0
end
func f(data: ref int[]) -> int
    data[g()] = 1
    return data[g()]
end
`, "f")
	assertLacks(t, ops, "OP_GET_REF_LOCAL_INDEX_ARRAY")
	assertLacks(t, ops, "OP_SET_REF_LOCAL_INDEX_ARRAY_NORC")
	assertHas(t, ops, "OP_DEREF")
	assertHas(t, ops, "OP_DEREF_MUT")
	assertHas(t, ops, "OP_GET_INDEX_ARRAY")
	assertHas(t, ops, "OP_SET_INDEX_ARRAY_NORC")
}

// `ref P[]` (elemento composto): leitura funde, escrita fica no generico.
func TestRefCompositeArrayFusesReadOnly(t *testing.T) {
	ops := functionOpcodes(t, `
struct P
    x: int
end
func f(ps: ref P[]) -> P
    ps[0] = ps[1]
    return ps[0]
end
`, "f")
	assertHas(t, ops, "OP_GET_REF_LOCAL_INDEX_ARRAY")
	assertLacks(t, ops, "OP_SET_REF_LOCAL_INDEX_ARRAY_NORC")
	assertHas(t, ops, "OP_SET_INDEX")
}
```

Run: `go test ./internal/compiler -run 'RefArrayParameter|RefComposite'`
Expected: FAIL.

- [ ] **Step 2: Ramo `RefType`**

Em `fusedLocalIndexRead`, antes do `return 0, 0, nil, false` final:

```go
	if ref, ok := localType.(*ast.RefType); ok {
		if arr, ok := ref.ElementType.(*ast.ArrayType); ok {
			return chunk.OP_GET_REF_LOCAL_INDEX_ARRAY, arg, arr.ElementType, true
		}
	}
```

Em `tryFuseLocalIndexAssign`, no `switch t := localType.(type)`, acrescentar:

```go
	case *ast.RefType:
		// Slot `ref T[]` EMPRESTA (spec CoW-RC §4.2): a forma fundida de ref
		// reproduz GET_LOCAL_MUT_BORROW + DEREF_MUT. Um slot ref possuidor
		// seria estado inesperado — vai pelo generico.
		inner, isArr := t.ElementType.(*ast.ArrayType)
		if !isArr || c.localOwns(arg) {
			return false, nil
		}
		op, arrType = chunk.OP_SET_REF_LOCAL_INDEX_ARRAY_NORC, inner
```

Atualizar os comentários de `fusedLocalIndexRead`/`tryFuseLocalIndexAssign` para mencionar o ramo `ref T[]`.

- [ ] **Step 3: Rodar os testes de bytecode**

Run: `go test ./internal/compiler`
Expected: PASS.

- [ ] **Step 4: Testes ponta a ponta**

Acrescentar a `internal/vm/typed_index_e2e_test.go`:

```go
func TestTypedIndexRefBubbleSortMutatesCaller(t *testing.T) {
	got := captureVMSource(t, `
func bubble(data: ref int[]) -> void
    let n: int = length(data)
    let i: int = 0
    while i < n do
        let j: int = 0
        while j < n - i - 1 do
            if data[j] > data[j + 1] then
                let tmp: int = data[j]
                data[j] = data[j + 1]
                data[j + 1] = tmp
            end
            j = j + 1
        end
        i = i + 1
    end
end
func main() -> int[]
    let data: int[] = [5, 1, 4, 2, 3]
    bubble(ref data)
    return data
end
test_report(main())
`)
	arr := got.Obj.(*value.ObjArray)
	for k := range 5 {
		if arr.Elements[k].Int() != int64(k+1) {
			t.Fatalf("esperado [1..5] no chamador, obtido %s", got.String())
		}
	}
}

// CoW atraves do ref: o array apontado esta compartilhado com `copy`; a
// escrita fundida clona, grava o clone de volta no slot do chamador (o ref
// continua valido) e `copy` fica intacta. Exatamente 1 clone.
func TestTypedIndexRefWriteClonesSharedTarget(t *testing.T) {
	ResetCloneCount()
	got := captureVMSource(t, `
func set0(data: ref int[]) -> void
    data[0] = 99
end
func main() -> int[]
    let data: int[] = [1, 2]
    let copy: int[] = data
    set0(ref data)
    return [data[0], copy[0]]
end
test_report(main())
`)
	arr := got.Obj.(*value.ObjArray)
	if arr.Elements[0].Int() != 99 || arr.Elements[1].Int() != 1 {
		t.Fatalf("esperado [99, 1], obtido %s", got.String())
	}
	if CloneCountValue() != 1 {
		t.Fatalf("esperava exatamente 1 clone CoW, obtido %d", CloneCountValue())
	}
}

func TestTypedIndexRefErrorsMatchGenericPath(t *testing.T) {
	cases := []struct{ name, typed, dynamic, want string }{
		{"leitura fora da faixa via ref", "func f(d: ref int[]) -> int\n    let i: int = 5\n    return d[i]\nend\nlet a: int[] = [1]\nprint(f(ref a))\n", "func f(d: any) -> any\n    let i: int = 5\n    return d[i]\nend\nlet a: any = [1]\nprint(f(a))\n", "array index out of bounds"},
		{"escrita fora da faixa via ref", "func f(d: ref int[]) -> void\n    let i: int = 5\n    d[i] = 2\nend\nlet a: int[] = [1]\nf(ref a)\n", "func f(d: any) -> void\n    let i: int = 5\n    d[i] = 2\nend\nlet a: any = [1]\nf(a)\n", "array index out of bounds"},
		{"ref null leitura", "func f(d: ref int[]) -> int\n    let i: int = 0\n    return d[i]\nend\nprint(f(null))\n", "func f(d: any) -> any\n    let i: int = 0\n    return d[i]\nend\nprint(f(null))\n", "cannot index non-array/map/bytes"},
		{"ref null escrita", "func f(d: ref int[]) -> void\n    let i: int = 0\n    d[i] = 1\nend\nf(null)\n", "func f(d: any) -> void\n    let i: int = 0\n    d[i] = 1\nend\nf(null)\n", "cannot set index on non-array/map"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			typedErr := interpretVMSource(t, New(), tc.typed)
			dynErr := interpretVMSource(t, New(), tc.dynamic)
			if typedErr == nil || !strings.Contains(typedErr.Error(), tc.want) {
				t.Fatalf("tipado: esperava %q, obtido %v", tc.want, typedErr)
			}
			if dynErr == nil || !strings.Contains(dynErr.Error(), tc.want) {
				t.Fatalf("dinamico: esperava %q, obtido %v", tc.want, dynErr)
			}
		})
	}
}
```

Run: `go test ./internal/vm -run TypedIndex`
Expected: PASS.

- [ ] **Step 5: Suíte completa, race, corpus, commit e binário s3 (head)**

```bash
gofmt -l internal; go vet ./internal/... && go test ./... 2>&1 | tail -15
go test -race ./internal/value ./internal/vm 2>&1 | tail -3
go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx 2>&1 | tail -5
```

Expected: tudo verde; corpus 0 falhas.

```bash
git add internal/compiler/typed_index.go internal/compiler/typed_index_compile_test.go internal/vm/typed_index_e2e_test.go
git commit -m "perf(compiler): formas fundidas para parametro ref T[] — OP_GET_REF_LOCAL_INDEX_ARRAY / OP_SET_REF_LOCAL_INDEX_ARRAY_NORC resolvem a caixa com uma Load(), sem referenceStorage (issue #66, item 1, estagio 3)"
go build -o "$S/bench/noxy_s3.exe" ./cmd/noxy
```

---

### Task 7: Medição

**Files:**
- Create (scratch, não commitado): `$S/bench/stages.ps1`
- Create: `benchmarks/results/2026-08-22-issue-66-typed-arrays-raw.md`
- Modify: `benchmarks/RESULTS.md` (nova seção no topo), `benchmarks/cross_runtime/results/cross_runtime.md` (sobrescrito pelo script)

- [ ] **Step 1: Checar carga e diff de saída do corpus**

```powershell
Get-Counter '\Processor(_Total)\% Processor Time' -SampleInterval 1 -MaxSamples 3 | % { $_.CounterSamples.CookedValue }
powershell -File benchmarks/compare_examples.ps1 -Baseline $S\bench\noxy_base.exe -Candidate $S\bench\noxy_s3.exe
```

Expected: CPU < ~20 %; "N iguais, 0 divergentes".

- [ ] **Step 2: Headline intercalado base × head**

```powershell
powershell -File benchmarks/interleaved_compare.ps1 -Baseline $S\bench\noxy_base.exe -Candidate $S\bench\noxy_s3.exe -BaselineLabel v0143 -CandidateLabel typed -Runs 9
```

Guardar a tabela (`benchmarks/results/interleaved.md`) para o raw.

- [ ] **Step 3: Por estágio (cinco binários na mesma janela)**

`$S/bench/stages.ps1`:

```powershell
param([int]$Runs = 5)
$S = "C:\Users\estev\AppData\Local\Temp\claude\D--OneDrive-Documentos-go-projects-noxy\ead4c52f-5869-403e-a45b-22421c6f07b9\scratchpad\bench"
$bins = [ordered]@{ base = "$S\noxy_base.exe"; s0 = "$S\noxy_s0.exe"; s1 = "$S\noxy_s1.exe"; s2 = "$S\noxy_s2.exe"; s3 = "$S\noxy_s3.exe" }
$root = Split-Path (Split-Path $PSScriptRoot -Parent) -Parent
$benches = Get-ChildItem "$root\benchmarks" -Filter "bench_*.nx" | Sort-Object Name
$local = "$S\nx"; New-Item -ItemType Directory -Force $local | Out-Null
$benches | % { Copy-Item $_.FullName $local -Force }
"| bench | " + (($bins.Keys | % { "$($_)_ms" }) -join " | ") + " |"
"|---|" + (($bins.Keys | % { "---" }) -join "|") + "|"
foreach ($b in $benches) {
    $times = @{}; $bins.Keys | % { $times[$_] = @() }
    for ($r = 0; $r -lt $Runs; $r++) {
        foreach ($k in $bins.Keys) {
            $sw = [Diagnostics.Stopwatch]::StartNew()
            & $bins[$k] "$local\$($b.Name)" *> $null
            $sw.Stop(); $times[$k] += $sw.Elapsed.TotalMilliseconds
        }
    }
    $cells = $bins.Keys | % { $s = $times[$_] | Sort-Object; "{0:N1}" -f $s[[int]($s.Count / 2)] }
    "| $($b.BaseName) | " + ($cells -join " | ") + " |"
}
```

Copiar para `$S/bench/stages.ps1` (o `$root` acima assume o script em `<repo>/x/y/`; mais simples: rodar com cwd na raiz do worktree e trocar `$root` por `(Get-Location).Path`). Run: `powershell -File $S/bench/stages.ps1 -Runs 5` a partir da raiz do worktree. Guardar a tabela.

- [ ] **Step 4: Cross-runtime**

```powershell
powershell -File benchmarks/cross_runtime/run_cross_runtime.ps1 -Noxy $S\bench\noxy_s3.exe -NoxyBaseline $S\bench\noxy_base.exe -BaselineLabel v0143
```

Saída em `benchmarks/cross_runtime/results/cross_runtime.md` (commitar).

- [ ] **Step 5: Perfil de bubblesort base × head**

```bash
cd $S/bench && ./noxy_base.exe --cpuprofile bubble_base.prof bubblesort.nx && go tool pprof -top -nodecount=12 noxy_base.exe bubble_base.prof | sed -n 6,20p
./noxy_s3.exe --cpuprofile bubble_head.prof bubblesort.nx && go tool pprof -top -nodecount=12 noxy_s3.exe bubble_head.prof | sed -n 6,20p
```

(`bubblesort.nx` = cópia de `benchmarks/cross_runtime/bubblesort.nx` já em `$S/bench`.) Registrar os shares de `referenceStorage`, `Upvalue.Load`, `mallocgc`.

- [ ] **Step 6: Raw + seção em RESULTS.md**

Criar `benchmarks/results/2026-08-22-issue-66-typed-arrays-raw.md` com: binários (commit de cada estágio), carga, tabela headline, tabela por estágio, cross-runtime, perfis, e o script `stages.ps1`. Escrever a nova seção no topo de `benchmarks/RESULTS.md` no mesmo formato da seção da fase 2 (cabeçalho `## v0.14.3 (7eed082) × indexação tipada de array (perf/issue-66-typed-array-index, <sha>)`, verificação completa, headline, por estágio, cross-runtime, leitura, gates). Os números são os medidos — inclusive se algum gate falhar ou a meta não se confirmar.

- [ ] **Step 7: Commit**

```bash
git add benchmarks/RESULTS.md benchmarks/results/2026-08-22-issue-66-typed-arrays-raw.md benchmarks/cross_runtime/results/cross_runtime.md
git commit -m "docs(bench): indexacao tipada de array medida por estagio contra v0.14.3 — opcodes tipados, formas fundidas por slot e ref (issue #66, item 1)"
```

---

### Task 8: Versão v0.15.0 (minor, decisão do usuário de 2026-08-22), CHANGELOG, docs

**Files:**
- Modify: `internal/version/version.go` (`const Version = "v0.15.0"`), `CHANGELOG.md` (nova entrada `## [0.15.0] - 2026-08-22`), `README.md:1` (badge) e `:108` (banner REPL), `AGENTS.md:414`, `docs/NOXY_LANGUAGE_SPEC.md` (linha do `sys.version`), `docs/index.html:58` (hero badge) e `:385` (`print(sys.version)`).

- [ ] **Step 1: Bump nos seis pontos**

`grep -rn "0\.14\.3" README.md AGENTS.md internal/version/version.go docs/index.html docs/NOXY_LANGUAGE_SPEC.md` → trocar cada ocorrência por `0.15.0` (Edit tool, CRLF preservado). Conferir com o mesmo grep que não sobrou nenhuma, e `go test ./internal/vm -run Version`.

- [ ] **Step 2: CHANGELOG**

Entrada no topo, no formato da 0.14.3: parágrafo de contexto (item 1 da #66; nenhuma mudança de sintaxe/semântica/saída/erro; corpus e diff de saída), subseção `### Performance` com: os seis opcodes e quando saem; o que o caminho rápido pula e o que confere em runtime; o caso `ref T[]` (sem `referenceStorage`); `setIndexGeneric`; números headline (bubblesort, call_readonly, cross-runtime ÷ CPython antes → depois) apontando para `benchmarks/RESULTS.md`; follow-ups observados (`length()` como chamada nativa; `Upvalue.Load` com RWMutex como custo restante do ref).

- [ ] **Step 3: Commit**

```bash
git add internal/version/version.go CHANGELOG.md README.md AGENTS.md docs/NOXY_LANGUAGE_SPEC.md docs/index.html
git commit -m "chore(version): noxy v0.15.0 — CHANGELOG (indexacao tipada de array, issue #66 item 1), README, AGENTS, spec, site, version.go"
```

---

### Task 9: Finalização — branch, PR, comentário na issue

- [ ] **Step 1: Verificação final (superpowers:verification-before-completion)**

```bash
go test ./... 2>&1 | tail -15
go test -race ./internal/value ./internal/vm 2>&1 | tail -3
go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx 2>&1 | tail -3
git status --short; git log --oneline origin/develop..HEAD
```

- [ ] **Step 2: superpowers:finishing-a-development-branch → PR**

Push `perf/issue-66-typed-array-index`; PR base `develop`, título `perf/issue-66-typed-array-index - Indexação tipada de array (issue #66, item 1)`, label `not available to review`, `--assignee @me`, body Summary / Components / Test Plan com checkboxes, `Refs #66`, sem reviewers/Jira (memória `noxy-pr-conventions`).

- [ ] **Step 3: Comentário na #66** com a tabela headline + cross-runtime, a leitura (o que se confirmou da hipótese e o que não), e os follow-ups (`length()` → `OP_LEN`; `Upvalue.Load`). Não fechar a issue.

---

## Self-Review

- **Spec coverage:** §3.1 opcodes → Task 2; §3.2 handlers/fallbacks/`setIndexGeneric`/`unicize*Slot`/redispatch → Task 3; §3.3 predicado, leitura, escrita, for-each → Tasks 4–6; §3.4 `NeverTracked` → Task 1; §4 guards (sentinela, disassembler, bytecode, comportamento, erros, inline, race, corpus, diff de saída) → Tasks 1–7; §5 medição por estágio/cross/perfil/gates/RESULTS → Task 7; decisões 5 (versão) → Task 8.
- **Placeholders:** nenhum "TBD"; cada step de código traz o código.
- **Type consistency:** `setIndexGeneric(c *chunk.Chunk, ip int) error`, `unicizeOwnedSlot(frame *CallFrame, idx int) value.Value`, `unicizeBorrowedSlot(idx int) value.Value`, `arrayTagIsRefSlot(tag *value.RuntimeTypeInfo) bool`, `value.NeverTracked(v Value) bool`, `isUntrackedElementType(ast.NoxyType) bool`, `arrayTypeOf(ast.NoxyType) (*ast.ArrayType, bool)`, `isSideEffectFree(ast.Expression) bool`, `fusedLocalIndexRead(*ast.IndexExpression) (chunk.OpCode, int, ast.NoxyType, bool)`, `tryFuseLocalIndexAssign(*ast.IndexExpression, ast.Expression) (bool, error)` — usados com essas assinaturas em todas as tasks.
