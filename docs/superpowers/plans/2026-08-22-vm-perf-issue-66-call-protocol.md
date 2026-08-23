# VM Perf — Protocolo de chamada (issue #66, item 3) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fast path inline de retorno e de chamada para frames simples, flag `ParamsUntracked` no `ObjFunction`, e duas superinstruções (`OP_GET_LOCAL_ADD_IMM_INT`, `OP_GET_LOCAL_2`) — sem mudar semântica, saída, mensagens de erro ou RC — e medir por estágio (issue #66, item 3).

**Architecture:** Três estágios, um commit-binário cada: s0 = `OP_RETURN` com fast path inline quando `Deferred`/`Owned` vazios, sem upvalue aberto e acima de `minFrameCount`; s1 = `OP_CALL_STATIC` com fast path inline (capacidade escrita à mão + frame montado sem `ownSlot`) quando o callee é closure com `ParamsUntracked` e aridade certa; s2 = superinstruções emitidas em nível de AST. Caminhos lentos intocados.

**Tech Stack:** Go 1.26, pwsh 7.6 (`benchmarks/*.ps1` direto), `go build -gcflags=-m=2`, `noxy --cpuprofile`, `go test -bench`.

**Spec:** `docs/superpowers/specs/2026-08-22-vm-perf-issue-66-call-protocol-design.md`

## Global Constraints

- Branch `perf/issue-66-call-protocol`, worktree `.claude/worktrees/perf-issue-66-call-protocol`, base `origin/develop` c1cc12a (v0.15.1). Um commit por task; os commits das Tasks 2, 3 e 4 geram `noxy_s0/s1/s2.exe`.
- Semântica, saída, mensagens de erro e RC idênticos; corpus 0 falhas; `compare_examples.ps1` 0 divergentes; opcodes só por APPEND em `internal/chunk/chunk.go`.
- Nenhum helper novo chamado de `run()`; guards de `inline_guard_test.go` verdes.
- Repo CRLF: Edit tool em arquivos existentes; conferir `git diff --numstat`.
- Binários em `$S\bench\` (`S = C:\Users\sandr\AppData\Local\Temp\claude\C--Users-sandr-Documents-noxy\58670b25-86ee-451c-a716-ecd4cec33bde\scratchpad`); `noxy_base3.exe` já buildado de c1cc12a — renomear para `noxy_base.exe`. Máquina sem `go test`/build durante medições.
- cwd = raiz do worktree, Git Bash.

---

### Task 1: `ObjFunction.ParamsUntracked` calculado pelo compilador

**Files:** Modify `internal/value/value.go` (struct `ObjFunction`); Modify `internal/compiler/compiler.go` (`compileFunction`); Create `internal/compiler/params_untracked_test.go`.

**Produces:** `ObjFunction.ParamsUntracked bool`; `func paramsUntracked(params []*ast.Parameter) bool` em compiler.go.

- [ ] **Step 1: Teste** (`params_untracked_test.go`, usa `compiledFunction(t, src, name)` de `inc_local_compile_test.go`):

```go
package compiler

import "testing"

func TestParamsUntrackedFlag(t *testing.T) {
	cases := []struct {
		name, source string
		want         bool
	}{
		{"int", "func f(n: int) -> int\n    return n\nend\n", true},
		{"scalars and string", "func f(s: string, b: bytes, x: float, ok: bool) -> int\n    return 0\nend\n", true},
		{"no params", "func f() -> int\n    return 1\nend\n", true},
		{"array", "func f(a: int[]) -> int\n    return 0\nend\n", false},
		{"any", "func f(x: any) -> int\n    return 0\nend\n", false},
		{"ref", "func f(r: ref int) -> int\n    return 0\nend\n", false},
		{"struct", "struct P\n    x: int\nend\nfunc f(p: P) -> int\n    return 0\nend\n", false},
		{"func type", "func f(g: func(int) -> int) -> int\n    return 0\nend\n", false},
		{"mixed", "func f(n: int, a: int[]) -> int\n    return 0\nend\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fn := compiledFunction(t, tc.source, "f")
			if fn.ParamsUntracked != tc.want {
				t.Fatalf("ParamsUntracked = %v, want %v", fn.ParamsUntracked, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2:** `go test ./internal/compiler -run TestParamsUntrackedFlag` → FAIL (campo não existe).
- [ ] **Step 3:** `value.go`: campo `ParamsUntracked bool` com comentário (perf #66 item 3). `compiler.go` antes de `compileFunction`:

```go
// paramsUntracked diz se NENHUM parametro pode carregar contador RC: todos sem
// `ref` e de tipo primitivo escalar/string/bytes (value.Retain e no-op para
// eles). Com isso o fast path de OP_CALL_STATIC pula o laco ownSlot (issue
// #66, item 3). Conservador: qualquer outra coisa (any, T[], struct, func,
// ref) devolve false e a chamada segue por callPreparedClosure.
func paramsUntracked(params []*ast.Parameter) bool {
	for _, param := range params {
		prim, ok := param.Type.(*ast.PrimitiveType)
		if !ok {
			return false
		}
		switch prim.Name {
		case "int", "float", "bool", "string", "bytes":
		default:
			return false
		}
	}
	return true
}
```
e em `compileFunction`, depois de criar `fnObj`: `fnObj.Obj.(*value.ObjFunction).ParamsUntracked = paramsUntracked(params)`.
- [ ] **Step 4:** teste → PASS; `go test ./internal/compiler ./internal/value` → PASS.
- [ ] **Step 5: Commit** `perf(compiler,value): ObjFunction.ParamsUntracked — parametros sem contador RC, calculado em compileFunction (issue #66, item 3)`.

---

### Task 2: `OP_RETURN` fast path (s0)

**Files:** Modify `internal/vm/executor.go` (handler `OP_RETURN`); Create `internal/vm/call_protocol_test.go`.

- [ ] **Step 1: Testes e2e** (`call_protocol_test.go`) — passam no baseline; travam o contrato:

```go
package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

func TestCallProtocolFastPathsKeepSemantics(t *testing.T) {
	src := `
func fib(n: int) -> int
    if n <= 1 then
        return n
    end
    return fib(n - 1) + fib(n - 2)
end
func withDefer(n: int) -> int
    let acc: int[] = []
    defer append(acc, 1)
    return n + 1
end
func makeAdder(base: int) -> func(int) -> int
    return func(x: int) -> int
        return base + x
    end
end
func sumArr(a: int[]) -> int
    let s: int = 0
    let i: int = 0
    while i < length(a) do
        s = s + a[i]
        i = i + 1
    end
    return s
end
let arr: int[] = [1, 2, 3]
let total: int = sumArr(arr)
append(arr, 4)
let add5: func(int) -> int = makeAdder(5)
test_report([fib(20), fib(1), fib(0), withDefer(41), add5(10), total, length(arr)])
`
	got := semArray(t, captureVMSource(t, src))
	want := []int64{6765, 1, 0, 42, 15, 6, 4}
	for i, w := range want {
		if got[i].Type != value.VAL_INT || got[i].Int() != w {
			t.Fatalf("celula %d: got %s, want %d", i, got[i].String(), w)
		}
	}
}

func TestCallProtocolErrorsUnchanged(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"deep recursion", "func down(n: int) -> int\n    return down(n + 1)\nend\ndown(0)\n", "stack overflow: call depth exceeds"},
		{"arity via any", "func f(a: int, b: int) -> int\n    return a + b\nend\nlet g: any = f\ng(1)\n", "expected 2 arguments but got 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := interpretVMSource(t, New(), tc.source)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want contendo %q", err, tc.want)
			}
		})
	}
}
```
- [ ] **Step 2:** `go test ./internal/vm -run TestCallProtocol` → PASS (baseline).
- [ ] **Step 3: Implementar** o fast path no handler `OP_RETURN` (código da spec §3.1, antes de `result := vm.pop()` atual; o caminho lento fica igual).
- [ ] **Step 4:** `go test ./internal/vm` → PASS; `go vet ./internal/vm`; guards de inline verdes (`-run 'StaysInlin'`).
- [ ] **Step 5: Commit** `perf(vm): OP_RETURN com fast path inline para frame sem defer/Owned/upvalue aberto (issue #66, item 3, s0)`.

---

### Task 3: `OP_CALL_STATIC` fast path (s1)

**Files:** Modify `internal/vm/executor.go` (handler `OP_CALL_STATIC`); Modify `internal/vm/call_protocol_test.go`.

- [ ] **Step 1: Teste adicional** — parâmetro composto segue caminho lento e retém (CoW intacto):

```go
func TestCallWithCompositeParamStillRetains(t *testing.T) {
	src := `
func touch(a: int[]) -> int
    a[0] = 99
    return a[0]
end
let base: int[] = [1, 2]
let r: int = touch(base)
test_report([r, base[0]])
`
	got := semArray(t, captureVMSource(t, src))
	if got[0].Int() != 99 || got[1].Int() != 1 {
		t.Fatalf("got %s/%s, want 99/1 (CoW: base nao muda)", got[0].String(), got[1].String())
	}
}
```
- [ ] **Step 2:** PASS no baseline.
- [ ] **Step 3: Implementar** (spec §3.2) no handler `OP_CALL_STATIC` antes de `frame.IP = ip` atual.
- [ ] **Step 4:** `go test ./internal/vm` + `go test -race ./internal/vm -run 'TestCallProtocol|TestParamRetain|TestSpawn|TestDefer'` → PASS; guards.
- [ ] **Step 5: Commit** `perf(vm): OP_CALL_STATIC com fast path inline (capacidade a mao, frame sem ownSlot) para closure ParamsUntracked (issue #66, item 3, s1)`.

---

### Task 4: Superinstruções `OP_GET_LOCAL_ADD_IMM_INT` e `OP_GET_LOCAL_2` (s2)

**Files:** Modify `internal/chunk/chunk.go` (append + `String()` + disassembler); Modify `internal/vm/executor.go` (2 handlers); Modify `internal/compiler/compiler.go` (helpers + 2 sites); Create `internal/compiler/superinstr_compile_test.go`; Modify `internal/vm/call_protocol_test.go` (e2e + emissão via `chunkTreeEmits`).

- [ ] **Step 1: Testes de emissão** (`superinstr_compile_test.go`):

```go
package compiler

import (
	"testing"

	"noxy-vm/internal/chunk"
)

func TestLocalAddImmFuses(t *testing.T) {
	fn := compiledFunction(t, "func f(n: int) -> int\n    return n - 1 + (n + 2)\nend\n", "f")
	code := fn.Chunk.(*chunk.Chunk).Code
	if !containsOpcode(code, chunk.OP_GET_LOCAL_ADD_IMM_INT) {
		t.Fatalf("n - 1 / n + 2 nao fundiram")
	}
	if containsOpcode(code, chunk.OP_SUB_INT) {
		t.Fatalf("OP_SUB_INT presente: n - 1 caiu no generico")
	}
}

func TestLocalAddImmDoesNotFuse(t *testing.T) {
	cases := map[string]string{
		"global":      "let x: int = 0\nfunc f() -> int\n    return x + 1\nend\n",
		"float":       "func f(x: float) -> float\n    return x + 1.0\nend\n",
		"big imm":     "func f(n: int) -> int\n    return n + 1000\nend\n",
		"literal left": "func f(n: int) -> int\n    return 1 + n\nend\n",
		"ref":         "func f(r: ref int) -> int\n    return *r + 1\nend\n",
		"upvalue":     "func f(n: int) -> func() -> int\n    return func() -> int\n        return n + 1\n    end\nend\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			fn := compiledFunction(t, src, "f")
			if containsOpcodeTree(fn, chunk.OP_GET_LOCAL_ADD_IMM_INT) {
				t.Fatalf("%s fundiu indevidamente", name)
			}
		})
	}
}

func TestLocalPairFuses(t *testing.T) {
	fn := compiledFunction(t, "func f(a: int, b: int) -> int\n    let i: int = 0\n    while i < b do\n        i = i + 1\n    end\n    return a + b\nend\n", "f")
	code := fn.Chunk.(*chunk.Chunk).Code
	if !containsOpcode(code, chunk.OP_GET_LOCAL_2) {
		t.Fatalf("a + b / i < b nao fundiram em OP_GET_LOCAL_2")
	}
}

func TestLocalPairDoesNotFuseForRefOrGlobal(t *testing.T) {
	cases := map[string]string{
		"global": "let g: int = 1\nfunc f(a: int) -> int\n    return a + g\nend\n",
		"ref":    "func f(a: int, r: ref int) -> bool\n    return r == null\nend\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			fn := compiledFunction(t, src, "f")
			if containsOpcodeTree(fn, chunk.OP_GET_LOCAL_2) {
				t.Fatalf("%s fundiu indevidamente", name)
			}
		})
	}
}
```
(`containsOpcodeTree` = variante recursiva de `containsOpcode` sobre constantes função; criar no mesmo arquivo se não existir.)

- [ ] **Step 2:** FAIL (opcodes não existem).
- [ ] **Step 3: `chunk.go`** — append após `OP_SET_REF_LOCAL_INDEX_ARRAY_NORC`:

```go
	// perf issue #66 (item 3): superinstrucoes emitidas em nivel de AST.
	OP_GET_LOCAL_ADD_IMM_INT // [slot u8][imm i8]; push local(int)+imm (== GET_LOCAL+CONSTANT+ADD_INT/SUB_INT; wrappa como OP_ADD_INT)
	OP_GET_LOCAL_2           // [a u8][b u8]; push local a, push local b (== GET_LOCAL a + GET_LOCAL b)
```
`String()`: dois cases. Disassembler: `OP_GET_LOCAL_ADD_IMM_INT` → `slotDeltaInstruction`; `OP_GET_LOCAL_2` → novo `twoByteInstruction(name, offset)` imprimindo os dois slots (modelar em `byteInstruction`).
- [ ] **Step 4: VM** (`executor.go`, perto de `OP_INC_LOCAL_INT`):

```go
		case chunk.OP_GET_LOCAL_ADD_IMM_INT:
			slot := c.Code[ip]
			imm := int8(c.Code[ip+1])
			ip += 2
			vm.push(value.NewInt(vm.stack[frame.LocalBase+int(slot)].Int() + int64(imm)))

		case chunk.OP_GET_LOCAL_2:
			a := c.Code[ip]
			b := c.Code[ip+1]
			ip += 2
			vm.push(vm.stack[frame.LocalBase+int(a)])
			vm.push(vm.stack[frame.LocalBase+int(b)])
```
- [ ] **Step 5: Compilador** — helpers perto de `tryFuseLocalIntIncrement`:

```go
// tryEmitLocalAddImm funde `local ± K` (local PLANO de tipo int — resolveLocal,
// nunca upvalue/global/ref; K literal int com o sinal aplicado em [-128,127])
// em OP_GET_LOCAL_ADD_IMM_INT. Devolve true se emitiu (o resultado e int).
func (c *Compiler) tryEmitLocalAddImm(infix *ast.InfixExpression) bool {
	if infix.Operator != "+" && infix.Operator != "-" {
		return false
	}
	ident, ok := infix.Left.(*ast.Identifier)
	if !ok {
		return false
	}
	lit, ok := infix.Right.(*ast.IntegerLiteral)
	if !ok {
		return false
	}
	slot, localType := c.resolveLocal(ident.Value)
	if slot == -1 || slot > 255 {
		return false
	}
	prim, ok := localType.(*ast.PrimitiveType)
	if !ok || prim.Name != "int" {
		return false
	}
	imm := lit.Value
	if infix.Operator == "-" {
		imm = -imm
	}
	if imm < -128 || imm > 127 {
		return false
	}
	c.emitBytes(byte(chunk.OP_GET_LOCAL_ADD_IMM_INT), byte(slot))
	c.emitByte(byte(int8(imm)))
	return true
}

// tryEmitLocalPair funde dois operandos que sao locais PLANOS de tipo primitivo
// (sem ref: o site do infix emite OP_DEREF para RefType e isso tem de
// continuar acontecendo pelo caminho normal) em OP_GET_LOCAL_2. Devolve os
// tipos como resolveLocal os da, para o chamador seguir igual.
func (c *Compiler) tryEmitLocalPair(left, right ast.Expression) (ast.NoxyType, ast.NoxyType, bool) {
	li, ok := left.(*ast.Identifier)
	if !ok {
		return nil, nil, false
	}
	ri, ok := right.(*ast.Identifier)
	if !ok {
		return nil, nil, false
	}
	ls, lt := c.resolveLocal(li.Value)
	rs, rt := c.resolveLocal(ri.Value)
	if ls == -1 || rs == -1 || ls > 255 || rs > 255 {
		return nil, nil, false
	}
	if _, ok := lt.(*ast.PrimitiveType); !ok {
		return nil, nil, false
	}
	if _, ok := rt.(*ast.PrimitiveType); !ok {
		return nil, nil, false
	}
	c.emitBytes(byte(chunk.OP_GET_LOCAL_2), byte(ls))
	c.emitByte(byte(rs))
	return lt, rt, true
}
```
Sites: (i) no `case *ast.InfixExpression` genérico, logo antes de `_, leftType, err := c.Compile(n.Left)`: `if c.tryEmitLocalAddImm(n) { return c.currentChunk, &ast.PrimitiveType{Name: "int"}, nil }` e em seguida `var leftType, rightType ast.NoxyType; if lt, rt, ok := c.tryEmitLocalPair(n.Left, n.Right); ok { leftType, rightType = lt, rt } else { ...compila os dois como hoje, com os dois OP_DEREF... }` — o resto do case segue inalterado usando `leftType`/`rightType`. (ii) em `tryCompileFusedCondition`, depois de obter `jumpOp`: `if lt, rt, ok := c.tryEmitLocalPair(infix.Left, infix.Right); ok { if lt.String()=="int" && rt.String()=="int" { return jumpOp, true, nil }; c.currentChunk.TruncateTo(checkpoint); /* segue generico */ }` — o checkpoint já existe antes.
- [ ] **Step 6:** testes do compilador PASS; `go test ./internal/vm ./internal/compiler ./internal/chunk`; e2e em `call_protocol_test.go` (TestCallProtocolFastPathsKeepSemantics já cobre `fib`, `i < length(a)`, `s + a[i]`); `noxy --disassembly` de `fib.nx` mostra os opcodes novos; corpus.
- [ ] **Step 7: Commit** `perf(compiler,vm): superinstrucoes OP_GET_LOCAL_ADD_IMM_INT e OP_GET_LOCAL_2 emitidas em nivel de AST (issue #66, item 3, s2)`.

---

### Task 5: Verificação + medição + relatório

- [ ] `go test ./...`; `go test -race ./internal/value ./internal/vm`; corpus; `compare_examples.ps1` base × head = 0 divergentes; guards.
- [ ] Binários: `noxy_base.exe` (c1cc12a, = `noxy_base3.exe`), `noxy_s0.exe` (Task 2), `noxy_s1.exe` (Task 3), `noxy_s2.exe` = head (Task 4) — via `git worktree add --detach $S/wt_sN <commit>`.
- [ ] `pwsh -NoProfile -File benchmarks/interleaved_compare.ps1 -Baseline base -Candidate head -Runs 9`; A/B por estágio (4 binários × 11) em `fib.nx` e `loop_arith.nx` (+ `bubblesort.nx`); `run_cross_runtime.ps1 -NoxyBaseline`; `go test ./internal/vm -run xxx -bench BenchmarkNoxyCallOverhead -count 10` base × head (base via worktree detach); perfil `fib(34)` head.
- [ ] `benchmarks/results/2026-08-22-issue-66-call-protocol-raw.md` + seção nova no topo de `RESULTS.md`.
- [ ] Commit `docs(bench): protocolo de chamada medido contra v0.15.1 ... (issue #66, item 3)`.

### Task 6: Bump v0.15.2 (7 pontos) + CHANGELOG — mesmo procedimento do item 2.

### Task 7: finishing-a-development-branch — push, PR base develop (título `perf/issue-66-call-protocol - ...`, label "not available to review", assignee @me, Refs #66), comentário na #66; depois seguir para o item 4 conforme combinado (opção A).
