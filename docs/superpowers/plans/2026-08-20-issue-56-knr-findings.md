# Issue #56 — Achados do K&R em Noxy (v0.11.0) — Plano de Implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Entregar os 16 itens da issue #56 num único PR v0.11.0 (branch `fix/issue-56-knr-findings`, base `develop`): pilhas dinâmicas, `OP_ARRAY_FILL`, `continue`, `return` inline, `1e3`, linha do erro de atribuição, checagens estáticas de `*`/`!`/`~`/bitwise, condição só `bool`, f-string `{{ }}`, nativas de `io` (`read_line`/`list_dir`/`rename`/`write_bytes`/`stdin`), `input()` corrigido, `read_lines` sem `""` final, `eprint` + diagnósticos em stderr, exit code 1, `m.x = v` erro, tipos nominais de módulo unificados, docs.

**Architecture:** Cada item é uma tarefa fechada com teste primeiro (Go `_test.go` no pacote tocado; e2e via fonte Noxy com os helpers `captureVMSource`/`traceVMSource`/`interpretOrCompileErr` de `internal/vm`). Mudanças de VM (pilhas, opcode, saltos) ficam em `internal/vm` + `internal/value`; mudanças de linguagem em `internal/{token,lexer,ast,parser,compiler}`; stdlib em `internal/stdlib/io.nx` + `internal/vm/builtins_io.go`; CLI em `cmd/noxy/main.go`. Docs/CHANGELOG/versão são a última tarefa, depois do runner de exemplos.

**Tech Stack:** Go 1.24 (módulo `noxy-vm`), testes `go test`, runner de integração `go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx`, Python 3 só para o diff de saída dos exemplos.

**Spec:** `docs/superpowers/specs/2026-08-20-issue-56-knr-findings-design.md` (leia antes; cada tarefa cita a seção).

## Global Constraints

- Branch `fix/issue-56-knr-findings` já existe (commits `2d08031`, `d523105` = spec). **Nunca** commitar em `develop`. Um commit por passo de commit abaixo; mensagens em português no formato `tipo(escopo): descrição (#56 item N)`, terminando com `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Repositório é **CRLF**: escreva arquivos novos com a ferramenta Write (nunca heredoc no Bash); `gofmt -l` não é confiável em CRLF — rode `gofmt -w` só nos arquivos tocados e confira `git diff --numstat` (um arquivo com centenas de linhas alteradas = conversão de fim de linha indevida → reverter).
- Executáveis Go recém-compilados podem sumir (antivírus): rode Noxy ad hoc com `go run ./cmd/noxy arquivo.nx`, nunca com um `.exe` no scratchpad.
- Tetos e constantes (spec §1): `FramesMax = 100_000`, `StackMax = 1 << 20`, `framesInitial = 64`, `stackInitial = 2048`, `stackReserve = 256`. Mensagens: `stack overflow: call depth exceeds %d frames` e `stack overflow: operand stack exceeds %d slots`.
- Mensagens de erro novas (copiar literalmente): `condition must be bool, got %s` (+ hint `use an explicit comparison, e.g. 'x != 0', 'x != ""', 'x != null' or 'length(x) > 0'`), `operand of '!' must be bool, got %s`, `operand of '~' must be int, got %s`, `cannot dereference non-reference value of type %s`, `operands for %s must be integers or bytes, got %s and %s` (`& | ^`) / `operands for %s must be integers, got %s and %s` (`<< >>`), `continue outside of loop`, `cannot assign to '%s.%s': module variables are read-only outside the module` (+ hint `expose a function in '%s' that updates it`), f-string: `unexpected %q in f-string expression` (+ hint `format specs are not supported; use fmt("%10s", x) for width/precision` quando o token é `:`).
- Ao final de CADA tarefa: `go build ./... && go vet ./internal/... ./cmd/... && go test ./internal/<pacotes tocados>`. Ao final do plano: `go test ./...` + runner 100 % + diff de exemplos + benchmark.
- Não tocar em: `internal/vm/references.go` (só leitura), semântica de `select` (documentar), `io.read`/`read_lines` em arquivo comum (continuam "do início").

---

## Mapa de arquivos

| Arquivo | Responsabilidade nesta entrega |
|---|---|
| `internal/vm/vm.go` | constantes/tetos, `VM.frames`/`VM.stack` como slices, `SharedState.stdin*` |
| `internal/vm/calls.go` | `ensureCallCapacity` (único ponto de checagem de teto), remoção da checagem duplicada em `call` |
| `internal/vm/stack.go` | `push` com crescimento + sentinela `stackOverflowPanic`, `growStack`, `growFrames` |
| `internal/vm/executor.go` | recover do sentinela em `run()`, `OP_ARRAY_FILL`, `OP_JUMP_IF_FALSE/TRUE` exigem bool |
| `internal/vm/unwind.go` | `finalizeCurrentFrame` por índice |
| `internal/value/value.go` | `(*ObjUpvalue).Relocate` |
| `internal/chunk/chunk.go` | `OP_ARRAY_FILL` (const, String, disassembly) |
| `internal/token/token.go`, `internal/ast/ast.go`, `internal/ast/clone.go`, `internal/parser/parser.go` | `continue`; `return` inline; f-string `{{ }}` + erro de sobra |
| `internal/lexer/lexer.go` | expoente em literais float |
| `internal/compiler/compiler.go` | `emitLocalsExit`, `ContinueStmt`, `Loop.ContinueTarget/ContinueJumps`, `setLine` no `AssignStmt`, checagens de `*`/`!`/`~`/bitwise, `checkCondition`, `emitDefaultInit` com `OP_ARRAY_FILL`, erro `m.x = v`, `typesEquivalent`/`structDeclaration` |
| `internal/compiler/function_types.go`, `generics_unify.go`, `generics_target.go`, `generics.go`, `generics_substitute.go` | sites de comparação nominal; walkers com `ContinueStmt` |
| `internal/vm/builtins_io.go`, `internal/vm/resources.go` | `io_read_line`, `io_list_dir`, `io_rename`, `io_stdin`, `input` com leitor único, `splitLines`, `FileResource.reader/stdin` |
| `internal/stdlib/io.nx` | wrappers `read_line`, `list_dir -> IOLinesResult`, `rename`, `write_bytes`, `write_bytes_result`, `stdin` |
| `internal/vm/builtins_core.go`, `builtins_concurrency.go`, `builtins_sys.go` | `eprint`/`eiprint`; prints de erro em stderr |
| `cmd/noxy/main.go` (+ `main_test.go` novo) | `diagOut`, `loadScript`, exit 1 |
| `internal/vm/stdlib_hygiene_test.go` | teste "wrapper da stdlib resolve para nativa" |
| `docs/NOXY_LANGUAGE_SPEC.md`, `README.md`, `AGENTS.md`, `CHANGELOG.md`, `internal/version/version.go`, `noxy_examples/` | docs, versão, exemplos novos/migrados |

---

### Task 1: Pilhas dinâmicas — frames e operandos crescem sob demanda, `stack overflow` sempre limpo (spec §1)

**Files:**
- Modify: `internal/vm/vm.go:11-12` (constantes), `:55-90` (struct `VM`), `:117-125` (`NewWithShared`)
- Modify: `internal/vm/calls.go:114-145` (`call`, `callPreparedClosure`)
- Modify: `internal/vm/stack.go:139-145` (`push`), `:245-263` (sem mudança, leitura)
- Modify: `internal/vm/executor.go:46-58` (`run` defer)
- Modify: `internal/vm/unwind.go:22-104` (`finalizeCurrentFrame`)
- Modify: `internal/value/value.go` (depois de `Close`, ~l.252)
- Modify: `internal/vm/defer_test.go:282-325`, `internal/vm/builtins_tasks_test.go:471-490` (adaptar índices)
- Test: `internal/vm/stack_growth_test.go` (novo)

**Interfaces:**
- Produces: `vm.ensureCallCapacity(c *chunk.Chunk, ip int) error`, `vm.growFrames()`, `vm.growStack() bool`, `type stackOverflowPanic struct{}`, `(*value.ObjUpvalue).Relocate(old, grown []value.Value)`; constantes `framesInitial`, `stackInitial`, `stackReserve`; `VM.frames []CallFrame`, `VM.stack []value.Value`.

- [ ] **Step 1: Escrever os testes que falham (crescimento, tetos, relocação, unwind por índice, task pequena)**

Criar `internal/vm/stack_growth_test.go`:

```go
package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

// depth(n) recursivo: 10000 niveis exigem ~10001 frames e dezenas de milhares
// de slots — bem alem dos 64/2048 iniciais. Deve crescer e devolver o valor.
func TestDeepRecursionGrowsFramesAndStack(t *testing.T) {
	reported := captureVMSource(t, `
func depth(n: int) -> int
    if n == 0 then return 0 end
    return 1 + depth(n - 1)
end
test_report(depth(10000))`)
	if reported.Type != value.VAL_INT || reported.AsInt != 10000 {
		t.Fatalf("depth(10000) = %v, want 10000", reported)
	}
}

// Recursao infinita morre na ENTRADA do frame com erro de runtime limpo
// (nunca panic Go), com a mensagem do teto de frames.
func TestInfiniteRecursionReportsCallDepthOverflow(t *testing.T) {
	err := interpretOrCompileErr(t, New(), `
func forever() -> int
    return forever()
end
forever()`)
	if err == nil || !strings.Contains(err.Error(), "stack overflow: call depth exceeds") {
		t.Fatalf("error=%v, want call depth overflow", err)
	}
}

// Um VM novo (inclusive o de cada task) nasce com as capacidades iniciais —
// o crescimento e sob demanda, nao no construtor.
func TestNewVMStartsWithInitialCapacities(t *testing.T) {
	machine := New()
	if len(machine.frames) != framesInitial || len(machine.stack) != stackInitial {
		t.Fatalf("frames=%d stack=%d, want %d/%d", len(machine.frames), len(machine.stack), framesInitial, stackInitial)
	}
	worker := NewWithShared(machine.shared, machine.Config)
	if len(worker.frames) != framesInitial || len(worker.stack) != stackInitial {
		t.Fatalf("worker frames=%d stack=%d, want %d/%d", len(worker.frames), len(worker.stack), framesInitial, stackInitial)
	}
}

// Closure captura um local ANTES de uma recursao que forca a pilha de
// operandos a crescer; depois le e escreve pelo upvalue. Sem Relocate o
// upvalue aberto apontaria para o array velho e a escrita se perderia.
func TestOpenUpvalueSurvivesStackGrowth(t *testing.T) {
	reported := captureVMSource(t, `
func depth(n: int) -> int
    if n == 0 then return 0 end
    return 1 + depth(n - 1)
end
func run() -> int
    let contador: int = 1
    let inc: func() -> void = func() -> void
        contador = contador + 1
    end
    depth(5000)
    inc()
    return contador
end
test_report(run())`)
	if reported.Type != value.VAL_INT || reported.AsInt != 2 {
		t.Fatalf("contador = %v, want 2", reported)
	}
}

// Defer cujo corpo recursa fundo o bastante para realocar vm.frames: o frame
// que esta sendo finalizado precisa ser reobtido por indice — senao a posse e
// o Closure ficariam no array velho e o slot novo guardaria lixo.
func TestDeferThatGrowsFramesIsFinalizedByIndex(t *testing.T) {
	machine := New()
	reported := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			reported = args[0]
		}
		return value.NewNull()
	})
	if err := interpretVMSource(t, machine, `
func depth(n: int) -> int
    if n == 0 then return 0 end
    return 1 + depth(n - 1)
end
func com_defer() -> int
    let dados: int[] = [1, 2, 3]
    defer depth(500)
    return length(dados)
end
test_report(com_defer())`); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if reported.Type != value.VAL_INT || reported.AsInt != 3 {
		t.Fatalf("reported=%v, want 3", reported)
	}
	if machine.frameCount != 0 || machine.currentFrame != nil {
		t.Fatalf("frameCount=%d current=%p, want 0/nil", machine.frameCount, machine.currentFrame)
	}
	for i := range machine.frames {
		frame := &machine.frames[i]
		if frame.Closure != nil || frame.Environment != nil || len(frame.Owned) != 0 || len(frame.Deferred) != 0 {
			t.Fatalf("frame %d nao foi finalizado: closure=%p env=%p owned=%d deferred=%d", i, frame.Closure, frame.Environment, len(frame.Owned), len(frame.Deferred))
		}
	}
}

// O sentinela de operandos so e alcancavel por um unico frame que empilhe
// mais do que cabe ate o teto de uma vez; aqui forcamos o cenario
// diretamente: pilha no teto, push -> panic do sentinela -> run() converte.
func TestOperandStackAtCapIsRuntimeErrorNotPanic(t *testing.T) {
	machine := New()
	machine.DefineContextualNative("fill_stack", func(context value.NativeContext, _ []value.Value) (value.Value, error) {
		worker, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		worker.stack = make([]value.Value, StackMax)
		worker.stackTop = StackMax
		return value.NewNull(), nil
	})
	err := interpretVMSource(t, machine, "fill_stack()\nprint(1)\n")
	if err == nil || !strings.Contains(err.Error(), "stack overflow: operand stack exceeds") {
		t.Fatalf("error=%v, want operand stack overflow", err)
	}
}
```

- [ ] **Step 2: Rodar para ver falhar**

Run: `go test ./internal/vm -run 'TestDeepRecursion|TestInfiniteRecursion|TestNewVMStarts|TestOpenUpvalueSurvives|TestDeferThatGrows|TestOperandStackAtCap' -v`
Expected: falha de compilação (`framesInitial`/`stackInitial` indefinidos, `worker.stack = make(...)` não atribuível a array).

- [ ] **Step 3: Constantes e struct `VM` como slices (`vm.go`)**

Substituir as linhas 11-12 por:

```go
// Tetos por VM (spec #56 §1). As pilhas NASCEM pequenas (framesInitial /
// stackInitial) e dobram sob demanda ate estes valores; no teto o erro e
// sempre o runtime error `stack overflow: ...`, nunca um panic Go.
const StackMax = 1 << 20 // slots da pilha de operandos
const FramesMax = 100_000

const framesInitial = 64
const stackInitial = 2048

// stackReserve e a folga de operandos que ensureCallCapacity garante na
// ENTRADA de cada frame: com ela, recursao profunda sempre esbarra no teto
// ali (erro limpo), e push() so panica com o sentinela se UM frame empilhar
// mais do que resta ate o teto de uma vez.
const stackReserve = 256
```

No struct `VM`, trocar o comentário das linhas 57-65 e os dois campos:

```go
	// frames e um slice de VALORES reusado por indice a cada chamada:
	// callPreparedClosure escreve em &frames[frameCount] em vez de heap-alocar
	// um *CallFrame novo. As capacidades de Owned/Deferred de cada slot sao
	// load-bearing para isso — finalizeCurrentFrame (unwind.go) trunca as duas
	// com `[:0]`, nunca com `= nil` (ver BenchmarkNoxyCallOverhead). O slice
	// CRESCE (growFrames, dobro ate FramesMax) apenas em ensureCallCapacity,
	// que reaponta vm.currentFrame; qualquer *CallFrame segurado em variavel
	// Go atraves de uma chamada reentrante tem de ser reobtido por indice
	// (finalizeCurrentFrame faz isso; o loop de run() recarrega apos OP_CALL).
	frames       []CallFrame
	frameCount   int
	currentFrame *CallFrame
```

e

```go
	// stack cresce em growStack (dobro ate StackMax); os unicos ponteiros para
	// dentro dela que sobrevivem a uma instrucao sao os upvalues ABERTOS
	// (vm.openUpvalues), reapontados por Relocate na realocacao.
	stack    []value.Value
	stackTop int
```

Em `NewWithShared` (l.119-122):

```go
	vm := &VM{
		shared: shared,
		Config: cfg,
		frames: make([]CallFrame, framesInitial),
		stack:  make([]value.Value, stackInitial),
	}
```

- [ ] **Step 4: `push`, `growStack`, `growFrames`, sentinela (`stack.go`)**

Substituir `push` (l.139-145):

```go
// stackOverflowPanic e o sentinela que push() lanca quando a pilha de
// operandos ja esta no teto; run() recupera SO este tipo e o converte no
// runtime error padrao. Qualquer outro panic continua subindo.
type stackOverflowPanic struct{}

func (vm *VM) push(v value.Value) {
	if vm.stackTop >= len(vm.stack) {
		if !vm.growStack() {
			panic(stackOverflowPanic{})
		}
	}
	vm.stack[vm.stackTop] = v
	vm.stackTop++
}

// growStack dobra a pilha de operandos (ate StackMax) e reaponta os upvalues
// ABERTOS — os unicos ponteiros para dentro de vm.stack que sobrevivem a uma
// instrucao (fatias `args` passadas a natives sao lidas, nunca escritas, e os
// indices de Owned/StackBase nao mudam). Devolve false se ja esta no teto.
func (vm *VM) growStack() bool {
	if len(vm.stack) >= StackMax {
		return false
	}
	newLen := len(vm.stack) * 2
	if newLen > StackMax {
		newLen = StackMax
	}
	old := vm.stack
	grown := make([]value.Value, newLen)
	copy(grown, old)
	vm.stack = grown
	for upvalue := vm.openUpvalues; upvalue != nil; upvalue = upvalue.Next() {
		upvalue.Relocate(old, grown)
	}
	return true
}

// growFrames dobra o slice de frames (ate FramesMax) e reaponta
// vm.currentFrame, que sempre e &frames[frameCount-1] fora de uma chamada em
// andamento. Chamado so por ensureCallCapacity, ANTES de tomar &frames[n].
func (vm *VM) growFrames() {
	newLen := len(vm.frames) * 2
	if newLen > FramesMax {
		newLen = FramesMax
	}
	grown := make([]CallFrame, newLen)
	copy(grown, vm.frames)
	vm.frames = grown
	if vm.frameCount > 0 {
		vm.currentFrame = &vm.frames[vm.frameCount-1]
	}
}
```

- [ ] **Step 5: `ensureCallCapacity` e remoção da checagem duplicada (`calls.go`)**

Em `call` (l.121-123) **apagar**:

```go
	if vm.frameCount == FramesMax {
		return false, vm.runtimeError(c, ip, "stack overflow")
	}
```

Em `callPreparedClosure` (l.139-141) substituir o mesmo bloco por:

```go
	if err := vm.ensureCallCapacity(c, ip); err != nil {
		return false, err
	}
```

e adicionar ao fim do arquivo:

```go
// ensureCallCapacity garante espaco para UM frame novo e uma folga de
// stackReserve slots de operandos acima de stackTop, crescendo as duas pilhas
// sob demanda. E o unico ponto do caminho normal onde os tetos sao
// verificados: recursao profunda morre aqui com erro de runtime limpo
// (mensagens distintas para frames e operandos), nunca com panic Go.
func (vm *VM) ensureCallCapacity(c *chunk.Chunk, ip int) error {
	if vm.frameCount == len(vm.frames) {
		if len(vm.frames) >= FramesMax {
			return vm.runtimeError(c, ip, "stack overflow: call depth exceeds %d frames", FramesMax)
		}
		vm.growFrames()
	}
	for len(vm.stack)-vm.stackTop < stackReserve {
		if !vm.growStack() {
			return vm.runtimeError(c, ip, "stack overflow: operand stack exceeds %d slots", StackMax)
		}
	}
	return nil
}
```

- [ ] **Step 6: `run()` recupera o sentinela (`executor.go:52-58`)**

Substituir o `defer func() {...}()` existente por:

```go
	defer func() {
		if recovered := recover(); recovered != nil {
			if _, isOverflow := recovered.(stackOverflowPanic); !isOverflow {
				panic(recovered)
			}
			// Sentinela de push(): um unico frame empilhou mais do que restava
			// ate StackMax (ensureCallCapacity cobre o caso comum, a recursao).
			err = vm.runtimeErrorAtCurrentFrame("stack overflow: operand stack exceeds %d slots", StackMax)
		}
		if vm.currentFrame == frame {
			frame.IP = ip
		}
		if err != nil {
			err = vm.unwindTo(minFrameCount-1, frameOutcome{Err: err}).Err
		}
	}()
```

- [ ] **Step 7: `Relocate` (`value.go`, logo após `Close`)**

Adicionar `"unsafe"` aos imports de `value.go` e:

```go
// Relocate reaponta uma caixa ABERTA depois que a pilha do VM foi realocada:
// location sai do slot no array antigo para o MESMO indice no novo. Caixa
// fechada (location aponta para closed) ou que nao aponta para dentro de old
// nao muda. Sob mu.Lock como Store/Close — tasks podem ler a caixa
// concorrentemente (Load/IsValid/PointsTo tomam RLock).
func (upvalue *ObjUpvalue) Relocate(old, grown []Value) {
	if upvalue == nil || len(old) == 0 || len(grown) < len(old) {
		return
	}
	upvalue.mu.Lock()
	defer upvalue.mu.Unlock()
	base := uintptr(unsafe.Pointer(&old[0]))
	addr := uintptr(unsafe.Pointer(upvalue.location))
	size := unsafe.Sizeof(Value{})
	if addr < base || addr >= base+uintptr(len(old))*size {
		return
	}
	upvalue.location = &grown[(addr-base)/size]
}
```

- [ ] **Step 8: `finalizeCurrentFrame` por índice (`unwind.go:22-33`)**

Substituir o início da função até o fim do laço de defers por:

```go
func (vm *VM) finalizeCurrentFrame(outcome frameOutcome) frameOutcome {
	if vm.currentFrame == nil || vm.frameCount == 0 {
		return outcome
	}
	// Por INDICE, nao por ponteiro: invokePreparedCall reentra a VM e pode
	// realocar vm.frames (growFrames); um *CallFrame segurado atraves da
	// chamada apontaria para o array velho — a posse e o Closure seriam
	// "liberados" na copia morta e o slot novo ficaria sujo.
	index := vm.frameCount - 1
	frame := &vm.frames[index]

	for len(frame.Deferred) > 0 {
		last := len(frame.Deferred) - 1
		call := frame.Deferred[last]
		frame.Deferred[last] = PreparedCall{}
		frame.Deferred = frame.Deferred[:last]

		if err := vm.invokePreparedCall(call); err != nil {
			outcome.Err = appendDeferredError(outcome.Err, DeferredError{
				Registration: call.Registration,
				Cause:        err,
			})
		}
		frame = &vm.frames[index]
	}
```

O restante da função continua usando `frame` (agora sempre o ponteiro válido). Atualizar o comentário "vm.frames e um array de VALORES reusado entre chamadas (nao realocado por chamada)" para "vm.frames e um slice de VALORES reusado entre chamadas (so realocado por growFrames, em ensureCallCapacity)".

- [ ] **Step 9: Adaptar os dois testes que indexam `StackMax` direto**

`internal/vm/defer_test.go` — em `TestFinishFrameAggregatesPreparedCallHeadroomFailureAndContinues`, imediatamente antes de `machine.stackTop = StackMax - 1` (l.304) inserir:

```go
	// Pilha no teto: ensureCallCapacity nao consegue a folga e a chamada
	// diferida falha com stack overflow (o cenario "sem headroom" do teste).
	machine.stack = make([]value.Value, StackMax)
```

`internal/vm/builtins_tasks_test.go` — dentro de `fill_task_stack` (l.481), antes de `worker.stackTop = StackMax`:

```go
		worker.stack = make([]value.Value, StackMax)
```

- [ ] **Step 10: Rodar os testes novos + pacote vm inteiro**

Run: `go test ./internal/vm -run 'TestDeepRecursion|TestInfiniteRecursion|TestNewVMStarts|TestOpenUpvalueSurvives|TestDeferThatGrows|TestOperandStackAtCap|TestFinishFrameAggregates|TestSpawnTaskDeferredHeadroom|TestCallResultCapturesStackOverflow|TestUnwindArchitecture' -v`
Expected: PASS. Depois `go test ./internal/vm` → PASS (se `TestUnwindArchitecture*` reclamar de `vm.frames = grown` em `growFrames`, o matcher em `architecture_test.go:780-820` só classifica escritas DENTRO das funções de teardown que ele inspeciona — confirme o nome da função inspecionada e, se `growFrames` estiver na lista, acrescente-a à exceção explícita do matcher com comentário).

- [ ] **Step 11: Benchmark de chamada (intercalado, 3 rodadas)**

Run: `git stash` **NÃO** — use o binário de referência: `git worktree add ../noxy-bench develop` (ou `git show develop:internal/vm/...` não serve); alternativa simples: rodar `go test ./internal/vm -run xxx -bench BenchmarkNoxyCallOverhead -benchtime=2s -count=3` no branch, anotar; `git checkout develop -- internal/vm internal/value` **em um worktree separado** e rodar igual; comparar ns/op (diferença < 5 % = ok). Registrar os números no commit.

- [ ] **Step 12: Commit**

```bash
git add internal/vm/vm.go internal/vm/calls.go internal/vm/stack.go internal/vm/executor.go internal/vm/unwind.go internal/value/value.go internal/vm/stack_growth_test.go internal/vm/defer_test.go internal/vm/builtins_tasks_test.go
git commit -m "fix(vm): pilhas de frames e operandos crescem sob demanda (64/2048 -> 100k/1M); stack overflow sempre runtime error; Relocate de upvalues; finalizeCurrentFrame por índice (#56 item 1)"
```

---

### Task 2: `OP_ARRAY_FILL` — `let a: T[N]` sem empilhar N elementos (spec §2)

**Files:**
- Modify: `internal/chunk/chunk.go:61` (const após `OP_ZEROS`), `:345-346` (String), `:584-585` (disassembly)
- Modify: `internal/vm/executor.go:1020-1036` (após `OP_ZEROS`)
- Modify: `internal/compiler/compiler.go:2716-2727` (`emitDefaultInit`, caso `ArrayType`)
- Test: `internal/vm/zeros_test.go` (acrescentar) e `internal/compiler/compiler_test.go` (acrescentar)

**Interfaces:**
- Produces: `chunk.OP_ARRAY_FILL` (pops `count`, `default`; pushes array).

- [ ] **Step 1: Testes que falham**

Em `internal/vm/zeros_test.go` acrescentar:

```go
func TestFixedArrayDeclarationDoesNotUseOperandStack(t *testing.T) {
	reported := captureVMSource(t, "let buf: int[10000]\ntest_report(length(buf))\n")
	if reported.Type != value.VAL_INT || reported.AsInt != 10000 {
		t.Fatalf("length = %v, want 10000", reported)
	}
	reported = captureVMSource(t, "let big: int[100000]\ntest_report(big[99999])\n")
	if reported.Type != value.VAL_INT || reported.AsInt != 0 {
		t.Fatalf("big[99999] = %v, want 0", reported)
	}
}

func TestFixedArrayDefaultsPerElementType(t *testing.T) {
	reported := captureVMSource(t, `
struct P
    x: int
end
let fs: float[2]
let ss: string[2]
let bs: bool[2]
let ps: P[2]
test_report(to_str(fs[1]) + "|" + ss[1] + "|" + to_str(bs[1]) + "|" + to_str(ps[1] == null))`)
	if got := reported.Obj.(string); got != "0.000000||false|true" {
		t.Fatalf("defaults = %q", got)
	}
}

// Default composto: os N slots comecam compartilhando o mesmo objeto
// (Owners = N); a CoW clona na primeira escrita, entao escrever em g[0]
// NAO pode aparecer em g[1].
func TestFixedNestedArrayElementsAreIndependentUnderCoW(t *testing.T) {
	reported := captureVMSource(t, "let g: int[3][3]\ng[0][0] = 1\ntest_report(g[1][0] + g[0][0] * 10)\n")
	if reported.Type != value.VAL_INT || reported.AsInt != 10 {
		t.Fatalf("g[1][0] + 10*g[0][0] = %v, want 10", reported)
	}
}
```

Em `internal/compiler/compiler_test.go` acrescentar (imports `bytes`, `noxy-vm/internal/chunk` se faltarem):

```go
func TestFixedArrayLetEmitsArrayFill(t *testing.T) {
	code, _, err := New().Compile(parse("let a: int[5000]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(code.Code, []byte{byte(chunk.OP_ARRAY_FILL)}) {
		t.Fatal("let T[N] should compile to OP_ARRAY_FILL")
	}
	if len(code.Code) > 32 {
		t.Fatalf("bytecode has %d bytes; N elements were emitted individually", len(code.Code))
	}
}
```

- [ ] **Step 2: Rodar para ver falhar**

Run: `go test ./internal/vm -run 'TestFixedArray|TestFixedNested' -v && go test ./internal/compiler -run TestFixedArrayLetEmitsArrayFill -v`
Expected: VM: panic recuperado/erro; compiler: `chunk.OP_ARRAY_FILL` indefinido.

- [ ] **Step 3: Opcode no chunk**

`chunk.go`: após a linha `OP_ZEROS` na lista de constantes adicionar `OP_ARRAY_FILL`; em `String()` após o caso `OP_ZEROS`:

```go
	case OP_ARRAY_FILL:
		return "OP_ARRAY_FILL"
```

no disassembler após o caso `OP_ZEROS`:

```go
	case OP_ARRAY_FILL:
		return c.simpleInstruction("OP_ARRAY_FILL", offset)
```

- [ ] **Step 4: Executor**

Após o `case chunk.OP_ZEROS:` (l.1020-1036) adicionar:

```go
		case chunk.OP_ARRAY_FILL:
			countVal := vm.pop()
			fill := vm.pop()
			if countVal.Type != value.VAL_INT {
				return vm.runtimeError(c, ip, "array size must be integer")
			}
			count := int(countVal.AsInt)
			if count < 0 {
				return vm.runtimeError(c, ip, "array size must be non-negative, got %d", count)
			}
			elements := make([]value.Value, count)
			for i := range elements {
				elements[i] = fill
			}
			// RC: NewArray retem cada slot — um default composto fica com
			// Owners = count e a CoW clona na primeira escrita a um elemento.
			vm.push(value.NewArray(elements))
```

- [ ] **Step 5: Compilador**

Em `emitDefaultInit`, substituir o bloco `if typ.Size > 0 { ... }` por:

```go
		if typ.Size > 0 {
			// Um default + N -> OP_ARRAY_FILL: nao empilha N elementos (antes
			// estourava a pilha de operandos em N > ~2047 e truncava o operando
			// de 16 bits de OP_ARRAY em N > 65535).
			if err := c.emitDefaultInit(typ.ElementType); err != nil {
				return err
			}
			c.emitConstant(value.NewInt(int64(typ.Size)))
			c.emitByte(byte(chunk.OP_ARRAY_FILL))
		} else {
```

- [ ] **Step 6: Rodar testes**

Run: `go test ./internal/vm -run 'TestFixedArray|TestFixedNested|Zeros' -v && go test ./internal/compiler -run TestFixedArrayLetEmitsArrayFill -v && go test ./internal/chunk`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/chunk/chunk.go internal/vm/executor.go internal/compiler/compiler.go internal/vm/zeros_test.go internal/compiler/compiler_test.go
git commit -m "feat(vm): OP_ARRAY_FILL — let a: T[N] nao empilha N elementos; int[100000] funciona (#56 item 2)"
```

---

### Task 3: `continue` + `break` fecham upvalues (spec §9)

**Files:**
- Modify: `internal/token/token.go:33` (const), `:129` (keyword)
- Modify: `internal/ast/ast.go:223-229` (após `BreakStmt`), `internal/ast/clone.go:21-22`
- Modify: `internal/parser/parser.go:154-155` (dispatch), `:365-373` (após `parseBreakStatement`)
- Modify: `internal/compiler/compiler.go:37-40` (`Loop`), `:1398-1450` (while), `:1510-1556` (for), `:1762-1776` (break), `:2617-2638` (endScope — não muda)
- Modify: `internal/compiler/generics.go:954,1070`, `internal/compiler/generics_substitute.go:144`
- Test: `internal/compiler/loop_break_test.go`, `internal/vm/loop_break_test.go`, `internal/parser/parser_test.go`

**Interfaces:**
- Produces: `token.CONTINUE`, `ast.ContinueStmt{Token}`, `Loop.ContinueTarget int` (−1 = salto adiante), `Loop.ContinueJumps []int`, `(c *Compiler) emitLocalsExit(keep int)`.

- [ ] **Step 1: Testes que falham**

`internal/compiler/loop_break_test.go` acrescentar:

```go
func TestCompileContinueOutsideLoopIsRejected(t *testing.T) {
	_, _, err := New().Compile(parse("continue\n"))
	if err == nil || !strings.Contains(err.Error(), "continue outside of loop") {
		t.Fatalf("error=%v, want \"continue outside of loop\"", err)
	}
}

func TestCompileContinueInsideWhileAndFor(t *testing.T) {
	for _, source := range []string{
		"let i: int = 0\nwhile i < 3 do\n    i = i + 1\n    if i == 2 then continue end\n    print(i)\nend\n",
		"for item in [1, 2, 3] do\n    let dobro: int = item * 2\n    if dobro == 4 then continue end\n    print(dobro)\nend\n",
	} {
		if _, _, err := New().Compile(parse(source)); err != nil {
			t.Fatalf("source %q: %v", source, err)
		}
	}
}
```

`internal/vm/loop_break_test.go` acrescentar:

```go
func TestContinueInWhileSkipsRestOfBody(t *testing.T) {
	trace := traceVMSource(t, `
let i: int = 0
while i < 6 do
    i = i + 1
    if i % 2 == 0 then continue end
    test_trace(i)
end
test_trace(99)`)
	requireTrace(t, trace, 1, 3, 5, 99)
}

func TestContinueInForEachSkipsToNextElement(t *testing.T) {
	trace := traceVMSource(t, `
for item in [1, 2, 3, 4] do
    let dobro: int = item * 2
    if dobro == 4 then continue end
    test_trace(dobro)
end
test_trace(99)`)
	requireTrace(t, trace, 2, 6, 8, 99)
}

func TestContinueInNestedLoopsTargetsInnermost(t *testing.T) {
	trace := traceVMSource(t, `
for a in [1, 2] do
    let j: int = 0
    while j < 3 do
        j = j + 1
        if j == 2 then continue end
        test_trace(a * 10 + j)
    end
end`)
	requireTrace(t, trace, 11, 13, 21, 23)
}

// let do corpo capturado por closure + continue DEPOIS da closure: o continue
// tem de fechar a caixa (como endScope) — senao o slot e reusado pela
// iteracao seguinte e a closure passa a ler o valor dela.
func TestContinueClosesUpvalueOfCapturedBodyLocal(t *testing.T) {
	trace := traceVMSource(t, `
let saved: func() -> int = func() -> int return -1 end
let i: int = 0
while i < 2 do
    i = i + 1
    let x: int = i
    if i == 1 then
        saved = func() -> int return x end
        continue
    end
end
test_trace(saved())`)
	requireTrace(t, trace, 1)
}

// Mesmo bug latente no break: closure capturada no corpo e break depois dela.
func TestBreakClosesUpvalueOfCapturedBodyLocal(t *testing.T) {
	trace := traceVMSource(t, `
let saved: func() -> int = func() -> int return -1 end
for item in [7, 8, 9] do
    let x: int = item
    saved = func() -> int return x end
    break
end
let other: int[] = [1, 2, 3]
test_trace(saved())`)
	requireTrace(t, trace, 7)
}

// Closure textualmente DEPOIS do continue: a iteracao que continua nunca cria
// a caixa; as outras guardam o valor certo.
func TestContinueBeforeClosureCreationKeepsOtherIterationsIntact(t *testing.T) {
	trace := traceVMSource(t, `
let saved1: func() -> int = func() -> int return -1 end
let saved3: func() -> int = func() -> int return -1 end
let i: int = 0
while i < 3 do
    i = i + 1
    let x: int = i
    if i == 2 then continue end
    if i == 1 then saved1 = func() -> int return x end end
    if i == 3 then saved3 = func() -> int return x end end
end
test_trace(saved1())
test_trace(saved3())`)
	requireTrace(t, trace, 1, 3)
}
```

`internal/parser/parser_test.go` acrescentar:

```go
func TestParseContinueStatementKeepsInlineEnd(t *testing.T) {
	p := New(lexer.New("while true do\n    if x then continue end\n    print(1)\nend\n"))
	program := p.ParseProgram()
	checkParserErrors(t, p)
	loop, ok := program.Statements[0].(*ast.WhileStatement)
	if !ok || len(loop.Body.Statements) != 2 {
		t.Fatalf("while body should have 2 statements, got %#v", program.Statements[0])
	}
	cond := loop.Body.Statements[0].(*ast.IfStatement)
	if _, ok := cond.Consequence.Statements[0].(*ast.ContinueStmt); !ok {
		t.Fatalf("expected ContinueStmt, got %T", cond.Consequence.Statements[0])
	}
}
```

- [ ] **Step 2: Rodar para ver falhar**

Run: `go test ./internal/parser -run TestParseContinue && go test ./internal/compiler -run 'TestCompileContinue' && go test ./internal/vm -run 'TestContinue|TestBreakClosesUpvalue'`
Expected: falhas (`ast.ContinueStmt` indefinido; runtime `undefined global variable 'continue'`).

- [ ] **Step 3: Token, AST, cloner**

`token.go`: após `BREAK   TokenType = "BREAK"` inserir `CONTINUE TokenType = "CONTINUE"`; no mapa de keywords após `"break":   BREAK,` inserir `"continue": CONTINUE,`.

`ast.go` após `BreakStmt`:

```go
type ContinueStmt struct {
	Token token.Token
}

func (cs *ContinueStmt) statementNode()       {}
func (cs *ContinueStmt) TokenLiteral() string { return cs.Token.Literal }
func (cs *ContinueStmt) String() string       { return "continue" }
```

`clone.go` após o caso `*BreakStmt`:

```go
	case *ContinueStmt:
		return &ContinueStmt{Token: n.Token}
```

- [ ] **Step 4: Parser**

Em `parseStatement` após `case token.BREAK:` adicionar `case token.CONTINUE: return p.parseContinueStatement()`; após `parseBreakStatement`:

```go
// parseContinueStatement segue o contrato de parseBreakStatement: nao avanca
// token — o laco chamador faz o nextToken, entao `if c then continue end`
// nao engole o 'end'.
func (p *Parser) parseContinueStatement() *ast.ContinueStmt {
	return &ast.ContinueStmt{Token: p.curToken}
}
```

- [ ] **Step 5: Compilador — `Loop`, helper, break, continue, while, for**

`Loop` (l.37-40):

```go
type Loop struct {
	EnclosingLocals int
	BreakJumps      []int
	// ContinueTarget >= 0: alvo para tras (while: inicio da condicao), emitido
	// como OP_LOOP direto; -1: alvo adiante (for: passo de incremento),
	// registrado em ContinueJumps e patchado quando o alvo e emitido.
	ContinueTarget int
	ContinueJumps  []int
}
```

Novo helper (colocar logo antes de `endScope`):

```go
// emitLocalsExit emite o descarte dos locais a partir do indice keep SEM
// remove-los da tabela do compilador — para break/continue, que saem do
// escopo em runtime mas nao em compilacao. Mesma regra do endScope: local
// capturado fecha a caixa (OP_CLOSE_UPVALUE), os demais OP_POP. Com OP_POP
// cru o upvalue de um `let` do corpo ficava aberto sobre um slot que a
// proxima iteracao reusa (a closure passava a ler o valor dela).
func (c *Compiler) emitLocalsExit(keep int) {
	for i := len(c.locals) - 1; i >= keep; i-- {
		if c.locals[i].IsCaptured {
			c.emitByte(byte(chunk.OP_CLOSE_UPVALUE))
		} else {
			c.emitByte(byte(chunk.OP_POP))
		}
	}
}
```

`case *ast.BreakStmt` — substituir o laço `toPop` por `c.emitLocalsExit(loop.EnclosingLocals)`. Logo após esse case adicionar:

```go
	case *ast.ContinueStmt:
		if len(c.loops) == 0 {
			return nil, nil, fmt.Errorf("continue outside of loop")
		}
		loop := c.loops[len(c.loops)-1]
		c.emitLocalsExit(loop.EnclosingLocals)
		if loop.ContinueTarget >= 0 {
			c.emitLoop(loop.ContinueTarget)
		} else {
			loop.ContinueJumps = append(loop.ContinueJumps, c.emitJump(chunk.OP_JUMP))
		}
		return c.currentChunk, nil, nil
```

`WhileStatement`: `loop := &Loop{EnclosingLocals: len(c.locals), BreakJumps: []int{}, ContinueTarget: loopStart}`.

`ForStatement` (passo 6): `loop := &Loop{EnclosingLocals: len(c.locals), BreakJumps: []int{}, ContinueTarget: -1}`; e logo após `c.endScope() // Pops User Variable` e ANTES de `// 10. Increment Index`:

```go
		// continue: chega aqui com a mesma pilha da saida normal do corpo
		// ([$collection, $index, $len]) — a variavel do laco e os locais do
		// corpo ja foram descartados pelo emitLocalsExit do ContinueStmt.
		for _, jump := range loop.ContinueJumps {
			c.patchJump(jump)
		}
```

- [ ] **Step 6: Walkers de generics**

`generics.go` l.954 e l.1070: após cada `case *ast.BreakStmt:` (com seu comentário/linha vazia) adicionar `case *ast.ContinueStmt:` com o mesmo tratamento (sem nome, sem sub-nó). `generics_substitute.go` l.144 idem.

- [ ] **Step 7: Rodar**

Run: `go test ./internal/parser ./internal/compiler ./internal/vm ./internal/ast`
Expected: PASS (inclui `TestClonerCoversEveryNode` e `generics_walkers_guard_test`).

- [ ] **Step 8: Commit**

```bash
git add internal/token/token.go internal/ast/ast.go internal/ast/clone.go internal/parser/parser.go internal/compiler/compiler.go internal/compiler/generics.go internal/compiler/generics_substitute.go internal/compiler/loop_break_test.go internal/vm/loop_break_test.go internal/parser/parser_test.go
git commit -m "feat(lang): continue em while/for; break e continue fecham upvalues de locais capturados (#56 item 9)"
```

---

### Task 4: `if c then return end` em uma linha (spec §10)

**Files:**
- Modify: `internal/parser/parser.go:326-344`
- Test: `internal/parser/parser_test.go`

- [ ] **Step 1: Teste que falha**

```go
func TestParseVoidReturnBeforeInlineEndElseElif(t *testing.T) {
	for _, source := range []string{
		"func f(x: int)\n    if x > 0 then return end\n    print(1)\nend\n",
		"func f(x: int)\n    if x > 0 then return else print(2) end\nend\n",
		"func f(x: int)\n    if x > 0 then return elif x < 0 then print(3) end\nend\n",
		"func f(x: int)\n    if x > 0 then\n        return\n    end\nend\n",
	} {
		p := New(lexer.New(source))
		p.ParseProgram()
		if len(p.Errors()) != 0 {
			t.Fatalf("source %q: %v", source, p.Errors())
		}
	}
}
```

- [ ] **Step 2: Rodar para ver falhar** — `go test ./internal/parser -run TestParseVoidReturnBefore` → `expected 'end', found EOF`.

- [ ] **Step 3: Implementar**

```go
func (p *Parser) parseReturnStatement() *ast.ReturnStmt {
	stmt := &ast.ReturnStmt{Token: p.curToken}

	// `return` vazio seguido de end/else/elif na MESMA linha: NAO avancar — o
	// contrato de parseBlockStatement e que o statement termine com curToken
	// no seu ULTIMO token e o bloco faca o nextToken. Avancar aqui comia o
	// 'end' (mesmo bug corrigido no break).
	if p.peekTokenIs(token.END) || p.peekTokenIs(token.ELSE) || p.peekTokenIs(token.ELIF) {
		return stmt
	}

	p.nextToken()

	// Handle return void
	if p.curToken.Type == token.NEWLINE || p.curToken.Type == token.EOF {
		return stmt
	}

	stmt.ReturnValue = p.parseExpression(LOWEST)

	// Optional newline
	if p.peekToken.Type == token.NEWLINE {
		p.nextToken()
	}

	return stmt
}
```

- [ ] **Step 4: Rodar** — `go test ./internal/parser ./internal/compiler ./internal/vm` → PASS.

- [ ] **Step 5: Commit** — `git commit -am "fix(parser): return vazio antes de end/else/elif na mesma linha (#56 item 10)"` (adicionando os arquivos tocados).

---

### Task 5: Literais float com expoente (spec §11)

**Files:**
- Modify: `internal/lexer/lexer.go:267-306` (`readNumber`)
- Test: `internal/lexer/lexer_test.go`, `internal/parser/parser_test.go`

- [ ] **Step 1: Testes que falham**

`lexer_test.go`:

```go
func TestNumberLiteralsWithExponent(t *testing.T) {
	lex := New("1e3 1.5e3 2E-10 1e+2 1e x")
	want := []struct {
		typ token.TokenType
		lit string
	}{
		{token.FLOAT, "1e3"}, {token.FLOAT, "1.5e3"}, {token.FLOAT, "2E-10"}, {token.FLOAT, "1e+2"},
		{token.INT, "1"}, {token.IDENTIFIER, "e"}, {token.IDENTIFIER, "x"},
	}
	for i, w := range want {
		got := lex.NextToken()
		if got.Type != w.typ || got.Literal != w.lit {
			t.Fatalf("token %d = %#v, want %s %q", i, got, w.typ, w.lit)
		}
	}
}
```

`parser_test.go`:

```go
func TestParseScientificFloatLiteral(t *testing.T) {
	p := New(lexer.New("1.5e3\n"))
	program := p.ParseProgram()
	checkParserErrors(t, p)
	literal, ok := program.Statements[0].(*ast.ExpressionStmt).Expression.(*ast.FloatLiteral)
	if !ok || literal.Value != 1500.0 {
		t.Fatalf("got %#v, want FloatLiteral 1500", program.Statements[0])
	}
}
```

- [ ] **Step 2: Rodar para ver falhar** — `go test ./internal/lexer -run TestNumberLiteralsWithExponent; go test ./internal/parser -run TestParseScientificFloat`.

- [ ] **Step 3: Implementar**

Em `readNumber`, entre o bloco do `.` e o `if isFloat {`:

```go
	// Expoente: [eE][+-]?digitos — so quando de fato ha digito depois, para
	// que `1e` seguido de outra coisa continue lexando INT + identificador.
	if (l.ch == 'e' || l.ch == 'E') && l.exponentAhead() {
		isFloat = true
		l.readChar() // e
		if l.ch == '+' || l.ch == '-' {
			l.readChar()
		}
		for isDigit(l.ch) {
			l.readChar()
		}
	}
```

e o helper (após `isHexDigit`):

```go
// exponentAhead responde se, com l.ch em 'e'/'E', os proximos caracteres
// formam um expoente valido: digito, ou sinal seguido de digito.
func (l *Lexer) exponentAhead() bool {
	next := l.peekChar()
	if isDigit(next) {
		return true
	}
	if next != '+' && next != '-' {
		return false
	}
	if l.readPosition+1 >= len(l.input) {
		return false
	}
	return isDigit(l.input[l.readPosition+1])
}
```

- [ ] **Step 4: Rodar** — `go test ./internal/lexer ./internal/parser ./internal/compiler` → PASS (o `strconv.ParseFloat` do parser já aceita expoente).

- [ ] **Step 5: Commit** — `feat(lexer): notação científica em literais float (1e3, 1.5e-3) (#56 item 11)`.

---

### Task 6: Linha correta no erro de atribuição (spec §13)

**Files:**
- Modify: `internal/compiler/compiler.go:418` (início de `case *ast.AssignStmt`)
- Test: `internal/compiler/compiler_test.go`

- [ ] **Step 1: Teste que falha**

```go
func TestAssignmentTypeErrorReportsAssignmentLine(t *testing.T) {
	_, _, err := New().Compile(parse("let x: int = 42\nx = 3.14\n"))
	if err == nil || !strings.HasPrefix(err.Error(), "[line 2]") {
		t.Fatalf("error=%v, want it to start with [line 2]", err)
	}
}
```

- [ ] **Step 2: Rodar para ver falhar** — erro começa com `[line 1]`.

- [ ] **Step 3: Implementar** — primeira linha dentro de `case *ast.AssignStmt:`: `c.setLine(n.Token.Line)` (o token é o `=`, `parser.go:182-185`).

- [ ] **Step 4: Rodar** — `go test ./internal/compiler` → PASS.

- [ ] **Step 5: Commit** — `fix(compiler): erro de atribuição aponta a linha da atribuição (#56 item 13)`.

---

### Task 7: Checagens estáticas de `*` unário, `!`, `~` e bitwise (spec §16)

**Files:**
- Modify: `internal/compiler/compiler.go:1292-1323` (`PrefixExpression`), `:1097-1170` (infix, antes do `switch n.Operator`)
- Test: `internal/compiler/compiler_test.go`

- [ ] **Step 1: Testes que falham**

```go
func TestStaticOperandChecksForUnaryAndBitwise(t *testing.T) {
	cases := []struct{ source, want string }{
		{"print(2 ** 3)\n", "cannot dereference non-reference value of type int"},
		{"print(!0)\n", "operand of '!' must be bool, got int"},
		{"print(~true)\n", "operand of '~' must be int, got bool"},
		{"print(1 & 3 == 1)\n", "operands for & must be integers or bytes, got int and bool"},
		{"print(\"a\" << 1)\n", "operands for << must be integers, got string and int"},
	}
	for _, tc := range cases {
		_, _, err := New().Compile(parse(tc.source))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("source %q: error=%v, want %q", tc.source, err, tc.want)
		}
	}
	for _, source := range []string{
		"let v: int = 1\nlet r: ref int = ref v\nprint(*r)\n",
		"print(~5)\n",
		"print(b\"\\x0f\" & b\"\\x01\")\n",
		"print(!true)\n",
		"print(1 << 3)\n",
	} {
		if _, _, err := New().Compile(parse(source)); err != nil {
			t.Fatalf("source %q should compile: %v", source, err)
		}
	}
}
```

- [ ] **Step 2: Rodar para ver falhar** — `go test ./internal/compiler -run TestStaticOperandChecks`.

- [ ] **Step 3: Implementar — prefixo**

Substituir o trecho de `case *ast.PrefixExpression:` a partir de `// For other operators (-, !, ~), compile Right first` por:

```go
		// For other operators (*, -, !, ~), compile Right first
		_, rightType, err := c.Compile(n.Right)
		if err != nil {
			return nil, nil, err
		}
		if n.Operator == "*" {
			// Deref explicito. Tipo estatico conhecido e nao-ref (inclui any,
			// que nunca guarda ref) e erro aqui: OP_DEREF em runtime PASSA um
			// nao-ref adiante sem erro (executor), entao esta e a unica guarda.
			// Tipo desconhecido (nil) mantem a leniencia: emite OP_DEREF.
			ref, isRef := rightType.(*ast.RefType)
			if !isRef && rightType != nil {
				return nil, nil, fmt.Errorf("[line %d] cannot dereference non-reference value of type %s", c.currentLine, rightType.String())
			}
			c.emitByte(byte(chunk.OP_DEREF))
			if isRef {
				return c.currentChunk, ref.ElementType, nil
			}
			return c.currentChunk, nil, nil
		}
		if ref, ok := rightType.(*ast.RefType); ok {
			c.emitByte(byte(chunk.OP_DEREF))
			rightType = ref.ElementType
		}
		if n.Operator == "-" {
			c.emitByte(byte(chunk.OP_NEGATE))
			return c.currentChunk, rightType, nil
		} else if n.Operator == "!" {
			if rightType != nil && !isAny(rightType) && rightType.String() != "bool" {
				return nil, nil, fmt.Errorf("[line %d] operand of '!' must be bool, got %s", c.currentLine, rightType.String())
			}
			c.emitByte(byte(chunk.OP_NOT))
			return c.currentChunk, &ast.PrimitiveType{Name: "bool"}, nil
		} else if n.Operator == "~" {
			if rightType != nil && !isAny(rightType) && rightType.String() != "int" {
				return nil, nil, fmt.Errorf("[line %d] operand of '~' must be int, got %s", c.currentLine, rightType.String())
			}
			c.emitByte(byte(chunk.OP_BIT_NOT))
			return c.currentChunk, rightType, nil
		}
		return c.currentChunk, rightType, nil
```

- [ ] **Step 4: Implementar — bitwise**

Antes do `switch n.Operator {` do caminho genérico do infix (depois do cálculo de `isFloat`) inserir:

```go
		if err := c.checkBitwiseOperands(n.Operator, leftType, rightType); err != nil {
			return nil, nil, err
		}
```

e o helper (perto de `isAny`):

```go
// checkBitwiseOperands rejeita em compilacao o que o runtime rejeitaria nos
// operadores bitwise quando os tipos estaticos sao conhecidos: & | ^ aceitam
// int ou bytes; << >> so int. any/nil passam para o runtime.
func (c *Compiler) checkBitwiseOperands(operator string, left, right ast.NoxyType) error {
	var allowed []string
	switch operator {
	case "&", "|", "^":
		allowed = []string{"int", "bytes"}
	case "<<", ">>":
		allowed = []string{"int"}
	default:
		return nil
	}
	ok := func(t ast.NoxyType) bool {
		if t == nil || isAny(t) {
			return true
		}
		for _, name := range allowed {
			if t.String() == name {
				return true
			}
		}
		return false
	}
	if ok(left) && ok(right) {
		return nil
	}
	describe := func(t ast.NoxyType) string {
		if t == nil {
			return "unknown"
		}
		return t.String()
	}
	if len(allowed) == 2 {
		return fmt.Errorf("[line %d] operands for %s must be integers or bytes, got %s and %s", c.currentLine, operator, describe(left), describe(right))
	}
	return fmt.Errorf("[line %d] operands for %s must be integers, got %s and %s", c.currentLine, operator, describe(left), describe(right))
}
```

- [ ] **Step 5: Rodar** — `go test ./internal/compiler ./internal/vm` → PASS. Se algum teste existente da VM usava `*` em não-ref ou `!` em não-bool de propósito para provocar erro de runtime, migre-o para `interpretOrCompileErr` (mesma mensagem não se aplica — ajuste o `Contains` para o texto novo).

- [ ] **Step 6: Commit** — `feat(compiler): * em não-ref, ! em não-bool, ~ e bitwise em tipos errados são erros de compilação (#56 item 16)`.

---

### Task 8: Condição só `bool` — compilação e runtime (spec §5)

**Files:**
- Modify: `internal/compiler/compiler.go:1344-1452` (if/while), `:1046-1085` (`&&`/`||`)
- Modify: `internal/vm/executor.go:123-137`
- Test: `internal/compiler/compiler_test.go`, `internal/vm/vm_test.go`

- [ ] **Step 1: Testes que falham**

`compiler_test.go`:

```go
func TestConditionsMustBeBool(t *testing.T) {
	rejected := []struct{ source, want string }{
		{"if 0 then print(1) end\n", "condition must be bool, got int"},
		{"if \"\" then print(1) end\n", "condition must be bool, got string"},
		{"if null then print(1) end\n", "condition must be bool, got null"},
		{"let xs: int[] = []\nif xs then print(1) end\n", "condition must be bool, got int[]"},
		{"struct P\n    x: int\nend\nlet p: P = P(1)\nif p then print(1) end\n", "condition must be bool, got P"},
		{"let n: int = 3\nwhile n do n = n - 1 end\n", "condition must be bool, got int"},
		{"let v: int = 1\nlet r: ref int = ref v\nif r then print(1) end\n", "condition must be bool, got int"},
		{"if 1 || true then print(1) end\n", "logical operators require boolean operands, got int and bool"},
		{"let x: int = 1\nif x > 0 then print(1) elif x then print(2) end\n", "condition must be bool, got int"},
	}
	for _, tc := range rejected {
		_, _, err := New().Compile(parse(tc.source))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("source %q: error=%v, want %q", tc.source, err, tc.want)
		}
		if !strings.Contains(err.Error(), "hint: use an explicit comparison") && !strings.Contains(err.Error(), "logical operators") {
			t.Fatalf("source %q: missing hint in %v", tc.source, err)
		}
	}
	for _, source := range []string{
		"let v: bool = true\nlet r: ref bool = ref v\nif r then print(1) end\n",
		"let x: int = 0\nif x == 0 then print(1) end\nwhile x < 0 do x = x + 1 end\n",
		"let a: any = true\nif a then print(1) end\n",
		"if true && false then print(1) end\n",
	} {
		if _, _, err := New().Compile(parse(source)); err != nil {
			t.Fatalf("source %q should compile: %v", source, err)
		}
	}
}
```

`internal/vm/vm_test.go` acrescentar:

```go
func TestRuntimeConditionOnAnyMustBeBool(t *testing.T) {
	err := interpretOrCompileErr(t, New(), "let a: any = 0\nif a then print(1) end\n")
	if err == nil || !strings.Contains(err.Error(), "condition must be bool, got int") {
		t.Fatalf("error=%v, want runtime bool check", err)
	}
	reported := captureVMSource(t, "let a: any = true\nlet n: int = 0\nif a then n = 1 end\ntest_report(n)\n")
	if reported.AsInt != 1 {
		t.Fatalf("any bool condition should be taken, got %v", reported)
	}
}
```

- [ ] **Step 2: Rodar para ver falhar** — `go test ./internal/compiler -run TestConditionsMustBeBool; go test ./internal/vm -run TestRuntimeConditionOnAny`.

- [ ] **Step 3: Helper e uso em if/while**

Helper (perto de `isAny`):

```go
// checkCondition exige bool numa posicao de condicao quando o tipo estatico e
// conhecido; any/nil ficam para o runtime (OP_JUMP_IF_FALSE/TRUE). Noxy nao
// tem truthy/falsy — `if n` com n: int e erro, nao "n != 0".
func (c *Compiler) checkCondition(t ast.NoxyType) error {
	if t == nil || isAny(t) {
		return nil
	}
	if primitive, ok := t.(*ast.PrimitiveType); ok && primitive.Name == "bool" {
		return nil
	}
	return fmt.Errorf("[line %d] condition must be bool, got %s\n  hint: use an explicit comparison, e.g. 'x != 0', 'x != \"\"', 'x != null' or 'length(x) > 0'", c.currentLine, t.String())
}
```

Em `IfStatement` e `WhileStatement`, no caminho não fusionado, substituir

```go
			if _, ok := condType.(*ast.RefType); ok {
				c.emitByte(byte(chunk.OP_DEREF))
			}
```

por

```go
			if ref, ok := condType.(*ast.RefType); ok {
				c.emitByte(byte(chunk.OP_DEREF))
				condType = ref.ElementType
			}
			if err := c.checkCondition(condType); err != nil {
				return nil, nil, err
			}
```

(o `elif` é um `IfStatement` aninhado — coberto).

- [ ] **Step 4: `||` passa a checar como `&&`**

No bloco `if n.Operator == "||"`, capturar `leftType` e `rightType` (hoje descartados com `_`) e, após `c.patchJump(endJump)`, copiar a mesma checagem do `&&` (mesma mensagem `logical operators require boolean operands, got %s and %s`).

- [ ] **Step 5: Runtime**

`executor.go`:

```go
		case chunk.OP_JUMP_IF_FALSE:
			offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			condition := vm.peek(0)
			if condition.Type != value.VAL_BOOL {
				return vm.runtimeError(c, ip, "condition must be bool, got %s", runtimeTypeName(condition))
			}
			if !condition.AsBool {
				ip += offset
			}

		case chunk.OP_JUMP_IF_TRUE:
			offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			condition := vm.peek(0)
			if condition.Type != value.VAL_BOOL {
				return vm.runtimeError(c, ip, "condition must be bool, got %s", runtimeTypeName(condition))
			}
			if condition.AsBool {
				ip += offset
			}
```

- [ ] **Step 6: Rodar todos os pacotes e corrigir o que a regra nova pegar**

Run: `go test ./internal/...`
Expected: a stdlib embutida compila (`go test ./internal/vm` carrega `io`, `http*`, etc. em vários testes). Qualquer teste/fonte que usava `if x` com `x` não-bool é migrado para comparação explícita (liste os arquivos no commit). Depois: `go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx` — cada `[FAIL]` com `condition must be bool` é corrigido no `.nx` com `!= 0`/`!= ""`/`!= null`/`length(x) > 0`; anotar os nomes para o CHANGELOG (Task 15).

- [ ] **Step 7: Commit** — `feat(lang)!: condição de if/elif/while e operandos de !/&&/|| exigem bool — erro de compilação (tipo estático) e de runtime (any) (#56 item 5)` + um commit `chore(examples): migra condições não-bool dos exemplos (#56 item 5)` se houver.

---

### Task 9: F-strings — `{{`/`}}` e erro para sobra na expressão (spec §14)

**Files:**
- Modify: `internal/parser/parser.go:970-1062` (`parseFString`)
- Test: `internal/parser/parser_test.go`, `internal/vm/vm_test.go`

- [ ] **Step 1: Testes que falham**

`parser_test.go`:

```go
func TestFStringBraceEscapesAndTrailingTokenError(t *testing.T) {
	for _, source := range []string{"f\"{{x}}\"\n", "f\"{{{x}}}\"\n", "f'{\"a\"}'\n", "f\"{ {\"a\": 1}[\"a\"] }\"\n"} {
		p := New(lexer.New(source))
		p.ParseProgram()
		if len(p.Errors()) != 0 {
			t.Fatalf("source %q: %v", source, p.Errors())
		}
	}
	p := New(lexer.New("f\"{name:>10}|\"\n"))
	p.ParseProgram()
	if len(p.Errors()) == 0 || !strings.Contains(p.Errors()[0], "unexpected \":\" in f-string expression") || !strings.Contains(p.Errors()[0], "format specs are not supported") {
		t.Fatalf("errors=%v, want format-spec rejection with hint", p.Errors())
	}
	p = New(lexer.New("f\"{a b}\"\n"))
	p.ParseProgram()
	if len(p.Errors()) == 0 || !strings.Contains(p.Errors()[0], "unexpected \"b\" in f-string expression") {
		t.Fatalf("errors=%v, want trailing-token rejection", p.Errors())
	}
}
```

`vm_test.go`:

```go
func TestFStringBraceEscapesRender(t *testing.T) {
	reported := captureVMSource(t, "let x: int = 1\ntest_report(f\"{{x}}|{{{x}}}|{ {\"a\": 2}[\"a\"] }|\" + f'{\"a\"}')\n")
	if got := reported.Obj.(string); got != "{x}|{1}|2|a" {
		t.Fatalf("got %q", got)
	}
}
```

- [ ] **Step 2: Rodar para ver falhar** — `go test ./internal/parser -run TestFStringBrace; go test ./internal/vm -run TestFStringBraceEscapesRender`.

- [ ] **Step 3: Reescrever `parseFString`**

```go
func (p *Parser) parseFString() ast.Expression {
	// Quebra o literal em partes fixas e expressoes `{...}` e as concatena
	// com `+`. `{{` e `}}` sao chaves literais; uma expressao que COMECA por
	// `{` (map literal) precisa de espaco: f"{ {"a": 1}["a"] }" — mesma regra
	// do Python. Dentro de `{...}` a expressao tem de consumir todos os
	// tokens: sobra (ex.: `:>10`) e erro — nao ha format spec, use fmt().
	literal := p.curToken.Literal
	line, column := p.curToken.Line, p.curToken.Column
	var exprs []ast.Expression
	var pending []byte

	flush := func() {
		if len(pending) == 0 {
			return
		}
		text := string(pending)
		exprs = append(exprs, &ast.StringLiteral{
			Token: token.Token{Type: token.STRING, Literal: text},
			Value: text,
		})
		pending = nil
	}

	for i := 0; i < len(literal); i++ {
		ch := literal[i]
		switch {
		case ch == '{' && i+1 < len(literal) && literal[i+1] == '{':
			pending = append(pending, '{')
			i++
		case ch == '}' && i+1 < len(literal) && literal[i+1] == '}':
			pending = append(pending, '}')
			i++
		case ch == '{':
			braceCount := 1
			j := i + 1
			for ; j < len(literal); j++ {
				if literal[j] == '{' {
					braceCount++
				} else if literal[j] == '}' {
					braceCount--
					if braceCount == 0 {
						break
					}
				}
			}
			if j >= len(literal) {
				p.errors = append(p.errors, fmt.Sprintf("[%d:%d] SyntaxError: unclosed brace in f-string", line, column))
				return nil
			}
			flush()
			exprContent := literal[i+1 : j]
			par := New(lexer.New(exprContent))
			innerExpr := par.parseExpression(LOWEST)
			if len(par.Errors()) > 0 {
				for _, msg := range par.Errors() {
					p.errors = append(p.errors, fmt.Sprintf("f-string expr error: %s", msg))
				}
				return nil
			}
			if !par.peekTokenIs(token.EOF) && !par.peekTokenIs(token.NEWLINE) {
				leftover := par.peekToken.Literal
				hint := ""
				if par.peekTokenIs(token.COLON) {
					hint = "\n  hint: format specs are not supported; use fmt(\"%10s\", x) for width/precision"
				}
				p.errors = append(p.errors, fmt.Sprintf("[%d:%d] SyntaxError: unexpected %q in f-string expression%s", line, column, leftover, hint))
				return nil
			}
			exprs = append(exprs, &ast.CallExpression{
				Token: token.Token{Type: token.IDENTIFIER, Literal: "("},
				Function: &ast.Identifier{
					Token: token.Token{Type: token.IDENTIFIER, Literal: "to_str"},
					Value: "to_str",
				},
				Arguments: []ast.Expression{innerExpr},
			})
			i = j
		default:
			pending = append(pending, ch)
		}
	}
	flush()

	if len(exprs) == 0 {
		return &ast.StringLiteral{Token: p.curToken, Value: ""}
	}
	combined := exprs[0]
	for i := 1; i < len(exprs); i++ {
		combined = &ast.InfixExpression{
			Token:    token.Token{Type: token.PLUS, Literal: "+"},
			Left:     combined,
			Operator: "+",
			Right:    exprs[i],
		}
	}
	return combined
}
```

- [ ] **Step 4: Rodar** — `go test ./internal/parser ./internal/vm ./internal/compiler` → PASS. Se algum teste existente dependia de `f"{{...}}"` com map literal sem espaço, ajuste o teste (quebra documentada na spec).

- [ ] **Step 5: Commit** — `feat(parser): f-string aceita {{ }} como chaves literais e rejeita sobra na expressão (sem format spec; use fmt) (#56 item 14)`.

---

### Task 10: `io` — `read_line`, `list_dir`, `rename`, `write_bytes`, `read_lines` sem `""` final, teste de higiene (spec §4, §7, §12)

**Files:**
- Modify: `internal/vm/resources.go:69-95` (`FileResource`)
- Modify: `internal/vm/builtins_io.go` (novas nativas após `io_read_lines`; `splitLines`)
- Modify: `internal/stdlib/io.nx`
- Modify: `internal/vm/builtins_io_test.go:343` (expectativa de `read_lines`)
- Test: `internal/vm/builtins_io_test.go`, `internal/vm/stdlib_hygiene_test.go`

**Interfaces:**
- Produces: nativas `io_read_line(file, IOResult)`, `io_list_dir(path, IOLinesResult)`, `io_rename(src, dst) -> bool`; `FileResource.reader *bufio.Reader` + `lineReader(file)`; `splitLines(content string) []string`; wrappers Noxy `io.read_line`, `io.list_dir -> IOLinesResult`, `io.rename`, `io.write_bytes`, `io.write_bytes_result`.

- [ ] **Step 1: Testes que falham**

`builtins_io_test.go` — trocar na l.343 `value.NewString("alpha"), value.NewString("beta"), value.NewString("")` por `value.NewString("alpha"), value.NewString("beta")` e acrescentar:

```go
func TestIOReadLineIsIncrementalWithExplicitEOF(t *testing.T) {
	machine := New()
	cleanupFileResources(t, machine)
	path := filepath.Join(t.TempDir(), "lines.txt")
	if err := os.WriteFile(path, []byte("um\r\ndois\n\ntres"), 0o600); err != nil {
		t.Fatal(err)
	}
	handle := callBuiltin(t, machine, "io_open", value.NewString(path), value.NewString("r"), testFileDefinition())
	ioResult := value.NewStruct("IOResult", []string{"ok", "data", "error"})
	for _, want := range []string{"um", "dois", "", "tres"} {
		result := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read_line", handle, ioResult), ioResult)
		assertBuiltinValue(t, result.Fields["ok"], value.NewBool(true))
		assertBuiltinValue(t, result.Fields["data"], value.NewString(want))
	}
	eof := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read_line", handle, ioResult), ioResult)
	assertBuiltinValue(t, eof.Fields["ok"], value.NewBool(false))
	assertBuiltinValue(t, eof.Fields["error"], value.NewString("EOF"))
	callBuiltin(t, machine, "io_close", handle)
	assertIOErrorResult(t, callBuiltin(t, machine, "io_read_line", handle, ioResult), ioResult)
}

func TestIOListDirAndRename(t *testing.T) {
	machine := New()
	root := t.TempDir()
	for _, name := range []string{"b.txt", "a.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	linesDef := value.NewStruct("IOLinesResult", []string{"ok", "data", "error"})
	listed := requireBuiltinInstance(t, callBuiltin(t, machine, "io_list_dir", value.NewString(root), linesDef), linesDef)
	assertBuiltinValue(t, listed.Fields["ok"], value.NewBool(true))
	assertBuiltinArray(t, listed.Fields["data"], []value.Value{value.NewString("a.txt"), value.NewString("b.txt"), value.NewString("sub")})
	missing := requireBuiltinInstance(t, callBuiltin(t, machine, "io_list_dir", value.NewString(filepath.Join(root, "nope")), linesDef), linesDef)
	assertBuiltinValue(t, missing.Fields["ok"], value.NewBool(false))
	assertBuiltinValue(t, callBuiltin(t, machine, "io_rename", value.NewString(filepath.Join(root, "a.txt")), value.NewString(filepath.Join(root, "c.txt"))), value.NewBool(true))
	assertBuiltinValue(t, callBuiltin(t, machine, "io_exists", value.NewString(filepath.Join(root, "c.txt"))), value.NewBool(true))
	assertBuiltinValue(t, callBuiltin(t, machine, "io_rename", value.NewString(filepath.Join(root, "nope.txt")), value.NewString(filepath.Join(root, "d.txt"))), value.NewBool(false))
}

func TestSplitLinesDropsOnlyTheTrailingEmptyLine(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a\nb\n", []string{"a", "b"}},
		{"a\nb", []string{"a", "b"}},
		{"\n", []string{""}},
		{"", []string{}},
		{"a\n\n", []string{"a", ""}},
	}
	for _, tc := range cases {
		got := splitLines(tc.in)
		if len(got) != len(tc.want) {
			t.Fatalf("splitLines(%q) = %q, want %q", tc.in, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("splitLines(%q) = %q, want %q", tc.in, got, tc.want)
			}
		}
	}
}

func TestIOWriteBytesWrappersRoundTrip(t *testing.T) {
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "bin.dat"))
	reported := captureVMSource(t, `
use io
let f: io.File = io.open("`+path+`", "w")
let r: io.IOWriteResult = io.write_bytes_result(f, b"\x00\xff")
io.write_bytes(f, b"\x01")
io.close(f)
let g: io.File = io.open("`+path+`", "r")
let data: io.IOBytesResult = io.read_bytes(g)
io.close(g)
test_report(to_str(r.bytes_written) + "|" + hex_encode(data.data))`)
	if got := reported.Obj.(string); got != "2|00ff01" {
		t.Fatalf("got %q", got)
	}
}
```

`stdlib_hygiene_test.go` acrescentar (imports `sort`, `noxy-vm/internal/ast`, `noxy-vm/internal/lexer`, `noxy-vm/internal/parser`, `noxy-vm/internal/token`):

```go
// Todo identificador que a stdlib CHAMA e que nao declara (nem importa de
// outro modulo da stdlib) tem de ser uma nativa registrada — io.nx declarou
// read_line/list_dir/rename por dois releases sem nativa (#56 item 4).
func TestStdlibWrappersCallOnlyRegisteredNatives(t *testing.T) {
	registrations := collectNativeRegistrations(t)
	entries, err := stdlib.FS.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{}
	topLevel := map[string]map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".nx") {
			continue
		}
		content, readErr := stdlib.FS.ReadFile(entry.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		module := strings.TrimSuffix(entry.Name(), ".nx")
		sources[module] = string(content)
		topLevel[module] = stdlibTopLevelNames(t, module, string(content))
	}
	checked := 0
	for module, source := range sources {
		for _, callee := range stdlibFreeCallees(source, module, topLevel) {
			checked++
			if _, registered := registrations[callee]; !registered {
				t.Errorf("stdlib/%s.nx calls %q, which is neither declared in the module nor a registered native", module, callee)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no free callees were checked; the scanner is broken")
	}
}

func stdlibTopLevelNames(t *testing.T, module, source string) map[string]bool {
	t.Helper()
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("stdlib/%s.nx: %v", module, p.Errors())
	}
	names := map[string]bool{}
	for _, statement := range program.Statements {
		switch declaration := statement.(type) {
		case *ast.FunctionStatement:
			names[declaration.Name] = true
		case *ast.StructStatement:
			names[declaration.Name] = true
		case *ast.LetStmt:
			names[declaration.Name.Value] = true
		}
	}
	return names
}

// stdlibFreeCallees devolve (ordenados, sem repeticao) os identificadores
// usados como alvo de chamada `nome(` que nao sao: declarados no topo do
// modulo; parametros/campos/lets (qualquer `IDENT :` ou `let IDENT`);
// membros (`x.nome(`); nem trazidos por `use m select a, b` / `select *`.
func stdlibFreeCallees(source, module string, topLevel map[string]map[string]bool) []string {
	declared := map[string]bool{}
	for name := range topLevel[module] {
		declared[name] = true
	}
	var tokens []token.Token
	lex := lexer.New(source)
	for {
		tok := lex.NextToken()
		tokens = append(tokens, tok)
		if tok.Type == token.EOF {
			break
		}
	}
	for i, tok := range tokens {
		if tok.Type != token.IDENTIFIER {
			continue
		}
		if i+1 < len(tokens) && tokens[i+1].Type == token.COLON {
			declared[tok.Literal] = true
		}
		if i > 0 && tokens[i-1].Type == token.LET {
			declared[tok.Literal] = true
		}
	}
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i].Type != token.USE || tokens[i+1].Type != token.IDENTIFIER || tokens[i+2].Type != token.SELECT {
			continue
		}
		imported := tokens[i+1].Literal
		for j := i + 3; j < len(tokens) && tokens[j].Type != token.NEWLINE && tokens[j].Type != token.EOF; j++ {
			if tokens[j].Literal == "*" {
				for name := range topLevel[imported] {
					declared[name] = true
				}
			} else if tokens[j].Type == token.IDENTIFIER {
				declared[tokens[j].Literal] = true
			}
		}
	}
	seen := map[string]bool{}
	var callees []string
	for i := 0; i+1 < len(tokens); i++ {
		tok := tokens[i]
		if tok.Type != token.IDENTIFIER || tokens[i+1].Type != token.LPAREN {
			continue
		}
		if i > 0 && tokens[i-1].Type == token.DOT {
			continue
		}
		if declared[tok.Literal] || seen[tok.Literal] {
			continue
		}
		seen[tok.Literal] = true
		callees = append(callees, tok.Literal)
	}
	sort.Strings(callees)
	return callees
}
```

(Se `token.USE`/`token.DOT`/`token.LET` tiverem outro nome em `token.go`, use o nome real — confira com `grep -n '"use"\|"let"\|DOT ' internal/token/token.go`.)

- [ ] **Step 2: Rodar para ver falhar** — `go test ./internal/vm -run 'TestIOReadLine|TestIOListDir|TestSplitLines|TestIOWriteBytes|TestStdlibWrappersCall|TestIOBuiltins' -v` → falhas (nativas ausentes; higiene acusa `io_read_line`, `io_list_dir`, `io_rename`; `read_lines` com `""`).

- [ ] **Step 3: `FileResource.reader`**

`resources.go`:

```go
type FileResource struct {
	stateMu     sync.Mutex
	operationMu sync.Mutex
	file        *os.File
	closed      bool
	// reader e o leitor bufferizado de read_line, criado sob demanda; para o
	// recurso de stdin (Task 11) e o MESMO leitor de input(). Acesso so
	// dentro de use() (operationMu).
	reader *bufio.Reader
}

// lineReader devolve o leitor bufferizado do recurso (cria na primeira
// chamada). Chamar dentro de use().
func (resource *FileResource) lineReader(file *os.File) *bufio.Reader {
	if resource.reader == nil {
		resource.reader = bufio.NewReader(file)
	}
	return resource.reader
}
```

(adicionar `"bufio"` ao import).

- [ ] **Step 4: Nativas e `splitLines` (`builtins_io.go`)**

Adicionar `"io"` aos imports. Após o bloco de `io_read_lines`:

```go
	vm.DefineContextualNative("io_read_line", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 2 {
			return value.NewNull(), nil
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}
		resultStruct, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull(), nil
		}
		result := newIOReadResult(resultStruct, false, value.NewString(""), "File not open")
		resource, exists := machine.shared.Files.get(fileHandle(machine.shared, inst))
		if !exists {
			return result, nil
		}
		operationResult, used := resource.use(func(file *os.File) value.Value {
			line, readErr := resource.lineReader(file).ReadString('\n')
			if readErr != nil && readErr != io.EOF {
				return newIOReadResult(resultStruct, false, value.NewString(""), readErr.Error())
			}
			if line == "" && readErr == io.EOF {
				return newIOReadResult(resultStruct, false, value.NewString(""), "EOF")
			}
			line = strings.TrimRight(line, "\r\n")
			if err := requireValidUTF8("io.read_line", line); err != nil {
				return newIOReadResult(resultStruct, false, value.NewString(""), err.Error())
			}
			return newIOReadResult(resultStruct, true, value.NewString(line), "")
		})
		if !used {
			return result, nil
		}
		return operationResult, nil
	})

	vm.DefineNative("io_list_dir", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		resultStruct, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}
		entries, err := os.ReadDir(args[0].String())
		if err != nil {
			return newIOLinesResult(resultStruct, false, nil, err.Error())
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		return newIOLinesResult(resultStruct, true, names, "")
	})

	vm.DefineNative("io_rename", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewBool(false)
		}
		return value.NewBool(os.Rename(args[0].String(), args[1].String()) == nil)
	})
```

Em `io_read_lines`, substituir `lines = strings.Split(normalized, "\n")` por `lines = splitLines(normalized)` e adicionar ao fim do arquivo:

```go
// splitLines separa em linhas sem produzir o "" fantasma de um conteudo
// terminado em \n (#56 item 12): "a\nb\n" -> [a b], "a\nb" -> [a b],
// "\n" -> [""], "" -> [].
func splitLines(content string) []string {
	if content == "" {
		return []string{}
	}
	lines := strings.Split(content, "\n")
	if strings.HasSuffix(content, "\n") {
		lines = lines[:len(lines)-1]
	}
	return lines
}
```

- [ ] **Step 5: `io.nx`**

Trocar `func list_dir(path: string) -> IOResult` / `io_list_dir(path, IOResult)` por:

```noxy
func list_dir(path: string) -> IOLinesResult
    return io_list_dir(path, IOLinesResult)
end
```

e, após `write_result`, adicionar:

```noxy
func write_bytes(file: File, data: bytes) -> void
    io_write(file, data)
end

func write_bytes_result(file: File, data: bytes) -> IOWriteResult
    return io_write_result(file, data, IOWriteResult)
end
```

- [ ] **Step 6: Rodar** — `go test ./internal/vm` → PASS (inclui `TestEveryNativeIsRegisteredExactlyOnce`).

- [ ] **Step 7: Commit** — `feat(io): read_line incremental com EOF explícito, list_dir -> IOLinesResult, rename, write_bytes[_result]; read_lines sem "" final; teste de higiene dos wrappers (#56 itens 4, 7, 12)`.

---

### Task 11: `input()` com leitor único e `io.stdin()` (spec §3)

**Files:**
- Modify: `internal/vm/vm.go:40-54` (`SharedState`), `internal/vm/builtins.go:5-15`
- Modify: `internal/vm/resources.go` (`FileResource.stdin`, `close`)
- Modify: `internal/vm/builtins_io.go` (`input`, `io_stdin`, `io_read*` em stdin, `io_write*` em stdin, `io_close*`)
- Modify: `internal/stdlib/io.nx`
- Test: `internal/vm/builtins_io_test.go`

**Interfaces:**
- Produces: `(*SharedState).stdin() *bufio.Reader`, `(*SharedState).stdinHandle() int`, `FileResource.stdin bool`, `(*FileResource).readAll(file *os.File) ([]byte, bool, string)`, nativa `io_stdin(File)`, wrapper `io.stdin() -> File`.

- [ ] **Step 1: Testes que falham**

```go
func withStdin(t *testing.T, content string, run func()) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdin
	os.Stdin = reader
	defer func() {
		os.Stdin = previous
		_ = reader.Close()
	}()
	go func() {
		_, _ = writer.WriteString(content)
		_ = writer.Close()
	}()
	run()
}

func TestInputReadsEveryLineFromRedirectedStdin(t *testing.T) {
	withStdin(t, "um\ndois\ntres\n", func() {
		reported := captureVMSource(t, `
let a: string = input()
let b: string = input()
let c: string = input()
let d: string = input()
test_report(a + "|" + b + "|" + c + "|" + d)`)
		if got := reported.Obj.(string); got != "um|dois|tres|" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestIOStdinReadLineSignalsEOFAndSharesBufferWithInput(t *testing.T) {
	withStdin(t, "primeira\nsegunda\nterceira", func() {
		reported := captureVMSource(t, `
use io
let first: string = input()
let in: io.File = io.stdin()
let out: string = first
let r: io.IOResult = io.read_line(in)
while r.ok do
    out = out + "|" + r.data
    r = io.read_line(in)
end
test_report(out + "|" + r.error + "|" + to_str(in.open) + "|" + in.path)`)
		if got := reported.Obj.(string); got != "primeira|segunda|terceira|EOF|true|<stdin>" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestIOStdinReadReturnsRemainingAndIsReadOnly(t *testing.T) {
	withStdin(t, "x\nresto1\nresto2\n", func() {
		reported := captureVMSource(t, `
use io
let skip: string = input()
let in: io.File = io.stdin()
let all: io.IOResult = io.read(in)
let w: io.IOWriteResult = io.write_result(in, "nao")
let c: io.IOCloseResult = io.close_result(in)
test_report(all.data + "|" + to_str(w.success) + "|" + w.error + "|" + to_str(c.success) + "|" + c.error)`)
		if got := reported.Obj.(string); got != "resto1\nresto2\n|false|stdin is read-only|false|stdin cannot be closed" {
			t.Fatalf("got %q", got)
		}
	})
}
```

- [ ] **Step 2: Rodar para ver falhar** — `go test ./internal/vm -run 'TestInputReads|TestIOStdin' -v`.

- [ ] **Step 3: `SharedState`**

Em `vm.go`, dentro de `SharedState` adicionar:

```go
	// stdin e o leitor UNICO de os.Stdin para todos os VMs deste estado:
	// input() e io.stdin() leem do mesmo buffer (senao a primeira chamada
	// engoliria ate 4 KB das linhas seguintes). Criado na primeira leitura —
	// depois de qualquer troca de os.Stdin feita por quem embute a VM.
	stdinOnce       sync.Once
	stdinReader     *bufio.Reader
	stdinHandleOnce sync.Once
	stdinFD         int
```

(import `bufio`). Em `builtins.go`, após `initializeState`:

```go
func (shared *SharedState) stdin() *bufio.Reader {
	shared.stdinOnce.Do(func() { shared.stdinReader = bufio.NewReader(os.Stdin) })
	return shared.stdinReader
}

// stdinHandle registra (uma vez) o FileResource de os.Stdin — mesmo leitor de
// input(), marcado stdin (close nao fecha, write recusa) — e devolve o fd.
func (shared *SharedState) stdinHandle() int {
	shared.stdinHandleOnce.Do(func() {
		shared.stdinFD = shared.Files.add(&FileResource{file: os.Stdin, reader: shared.stdin(), stdin: true})
	})
	return shared.stdinFD
}
```

(imports `bufio`, `os` em `builtins.go`).

- [ ] **Step 4: `FileResource.stdin`, `close`, `readAll`**

```go
type FileResource struct {
	stateMu     sync.Mutex
	operationMu sync.Mutex
	file        *os.File
	closed      bool
	reader      *bufio.Reader
	// stdin marca o recurso de os.Stdin: close() nao fecha o descritor e
	// read/read_lines leem "o restante" pelo reader (pipe nao tem Stat/Seek).
	stdin bool
}

func (resource *FileResource) close() error {
	resource.stateMu.Lock()
	if resource.closed || resource.file == nil {
		resource.stateMu.Unlock()
		return os.ErrClosed
	}
	if resource.stdin {
		resource.stateMu.Unlock()
		return nil
	}
	resource.closed = true
	file := resource.file
	resource.stateMu.Unlock()
	return file.Close()
}

// readAll devolve o conteudo "inteiro": do inicio em arquivo comum
// (readFileContents), o que ainda nao foi consumido em stdin.
func (resource *FileResource) readAll(file *os.File) ([]byte, bool, string) {
	if !resource.stdin {
		return readFileContents(file)
	}
	content, err := io.ReadAll(resource.lineReader(file))
	if err != nil {
		return nil, false, err.Error()
	}
	return content, true, ""
}
```

(import `io`). Em `builtins_io.go`, nos três sites `content, ok, errorText := readFileContents(file)` (`io_read`, `io_read_bytes`, `io_read_lines`) trocar por `resource.readAll(file)`.

- [ ] **Step 5: `input`, `io_stdin`, recusas em stdin**

Substituir a nativa `input` por:

```go
	vm.DefineContextualNative("input", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		// Repair a raw console mode leaked by a crashed program before a
		// line-oriented read, which would otherwise block forever.
		console.EnsureLineInput()
		if len(args) > 0 {
			fmt.Print(args[0].String())
		}
		// Leitor unico (SharedState): em pipe/arquivo le TODAS as linhas. No
		// EOF devolve o parcial, ou "" — input() nao sinaliza EOF; para isso
		// use io.read_line(io.stdin()).
		text, _ := machine.shared.stdin().ReadString('\n')
		return value.NewString(strings.TrimRight(text, "\r\n")), nil
	})

	vm.DefineContextualNative("io_stdin", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 1 {
			return value.NewNull(), nil
		}
		structDef, ok := args[0].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull(), nil
		}
		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		machine.shared.fileMetaMu.Lock()
		inst.Fields["fd"] = value.NewInt(int64(machine.shared.stdinHandle()))
		inst.Fields["path"] = value.NewString("<stdin>")
		inst.Fields["mode"] = value.NewString("r")
		inst.Fields["open"] = value.NewBool(true)
		machine.shared.fileMetaMu.Unlock()
		return value.Value{Type: value.VAL_OBJ, Obj: inst}, nil
	})
```

Em `io_close`: antes de `machine.shared.Files.remove(...)`, `if resource, ok := machine.shared.Files.get(handle); ok && resource.stdin { return value.NewNull(), nil }` (stdin fica registrado). Em `io_close_result`: mesmo guard devolvendo `success=false, error="stdin cannot be closed"`. Em `io_write`: dentro de `use`, `if resource.stdin { return value.NewNull() }`; em `io_write_result`: `if resource.stdin { result.Fields["error"] = value.NewString("stdin is read-only"); return result }` antes de escrever.

- [ ] **Step 6: `io.nx`** — após `open`:

```noxy
func stdin() -> File
    return io_stdin(File)
end
```

- [ ] **Step 7: Rodar** — `go test ./internal/vm` → PASS.

- [ ] **Step 8: Commit** — `feat(io): input() com leitor único de stdin (lê todas as linhas em pipe); io.stdin() como File para read_line/read com EOF explícito (#56 item 3)`.

---

### Task 12: `eprint`/`eiprint`, diagnósticos em stderr, exit code 1 (spec §6, §15)

**Files:**
- Modify: `internal/vm/builtins_core.go:13-32`, `internal/vm/builtins_concurrency.go:26,42,50,98,103`, `internal/vm/builtins_sys.go:264,270`
- Modify: `cmd/noxy/main.go`
- Create: `cmd/noxy/main_test.go`
- Test: `internal/vm/builtins_core_test.go`

- [ ] **Step 1: Testes que falham**

`builtins_core_test.go`:

```go
func TestEprintWritesToStderrOnly(t *testing.T) {
	stdoutReader, stdoutWriter, _ := os.Pipe()
	stderrReader, stderrWriter, _ := os.Pipe()
	prevOut, prevErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdoutWriter, stderrWriter
	machine := New()
	err := interpretVMSource(t, machine, "eprint(\"erro\", 42)\neiprint(\"x\")\nprint(\"ok\")\n")
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	os.Stdout, os.Stderr = prevOut, prevErr
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(stdoutReader)
	errText, _ := io.ReadAll(stderrReader)
	if string(out) != "ok\n" || string(errText) != "erro 42\nx" {
		t.Fatalf("stdout=%q stderr=%q", out, errText)
	}
}
```

`cmd/noxy/main_test.go` (novo):

```go
package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func captureStdout(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	run()
	_ = writer.Close()
	os.Stdout = previous
	out, _ := io.ReadAll(reader)
	return string(out)
}

func withDiagBuffer(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buffer bytes.Buffer
	previous := diagOut
	diagOut = &buffer
	t.Cleanup(func() { diagOut = previous })
	return &buffer
}

func TestDiagnosticsGoToStderrWriterAndExitCodeIsOne(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"runtime", "print(\"antes\")\nlet z: int = 0\nprint(1 / z)\n", "Runtime error:"},
		{"compiler", "let x: int = \"s\"\n", "Compiler error:"},
		{"parser", "let x: int = \n", "SyntaxError"},
	}
	for _, tc := range cases {
		diag := withDiagBuffer(t)
		var code int
		stdout := captureStdout(t, func() { code = runWithConfig(tc.name+".nx", tc.source, ".", false) })
		if code != 1 {
			t.Fatalf("%s: exit code=%d, want 1", tc.name, code)
		}
		if !strings.Contains(diag.String(), tc.want) {
			t.Fatalf("%s: diagnostics=%q, want %q", tc.name, diag.String(), tc.want)
		}
		if strings.Contains(stdout, tc.want) {
			t.Fatalf("%s: diagnostic leaked to stdout: %q", tc.name, stdout)
		}
	}
}

func TestLoadScriptMissingFileReportsAndFails(t *testing.T) {
	diag := withDiagBuffer(t)
	if _, ok := loadScript("nao_existe_56.nx"); ok {
		t.Fatal("missing file should not load")
	}
	if !strings.Contains(diag.String(), "Error reading file:") {
		t.Fatalf("diagnostics=%q", diag.String())
	}
}
```

- [ ] **Step 2: Rodar para ver falhar** — `go test ./internal/vm -run TestEprint; go test ./cmd/noxy`.

- [ ] **Step 3: Builtins**

`builtins_core.go` após `iprint`:

```go
	// eprint/eiprint: print/iprint em stderr — o fprintf(stderr, ...) do C.
	vm.DefineNative("eprint", func(args []value.Value) value.Value {
		var parts []string
		for _, arg := range args {
			parts = append(parts, arg.String())
		}
		fmt.Fprintln(os.Stderr, strings.Join(parts, " "))
		return value.NewNull()
	})
	vm.DefineNative("eiprint", func(args []value.Value) value.Value {
		var parts []string
		for _, arg := range args {
			parts = append(parts, arg.String())
		}
		fmt.Fprint(os.Stderr, strings.Join(parts, " "))
		return value.NewNull()
	})
```

(import `os`). `builtins_concurrency.go`: as cinco mensagens passam a `fmt.Fprintln(os.Stderr, ...)`/`fmt.Fprintf(os.Stderr, ...)`. `builtins_sys.go:264,270`: idem.

- [ ] **Step 4: `main.go`**

- Adicionar `"io"` ao import e, após os imports: `// diagOut recebe TODO diagnostico da CLI (parser, compilador, runtime, hints, leitura de arquivo, profiles); a saida do programa continua em stdout. Variavel para os testes redirecionarem.` + `var diagOut io.Writer = os.Stderr`.
- Trocar `ioutil.ReadFile` por `loadScript`:

```go
// loadScript le o programa; em falha escreve o diagnostico em diagOut e
// devolve ok=false — main sai com 1 (antes devolvia 0 e so imprimia).
func loadScript(filename string) (string, bool) {
	content, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(diagOut, "Error reading file: %s\n", err)
		return "", false
	}
	return string(content), true
}
```

e em `main`: `content, ok := loadScript(filename); if !ok { os.Exit(1) }`; remover o import `io/ioutil`.
- Todos os `fmt.Printf("Error ...")`, `fmt.Printf("Compiler error: ...")`, `fmt.Printf("Runtime error: ...")`, `fmt.Printf("hint: ...")`, os `fmt.Printf("%s\n", msg)` de erros do parser (em `runWithConfig` E no REPL) e `fmt.Println("Recovered from panic:", r)` + `debug.PrintStack()` → `fmt.Fprintf(diagOut, ...)` / `fmt.Fprintln(diagOut, ...)` / `diagOut.Write(debug.Stack())`. O prompt do REPL, `--version`, disassembly e `print` continuam em stdout.

- [ ] **Step 5: Rodar** — `go test ./cmd/noxy ./internal/vm` → PASS; `grep -n "fmt.Print" cmd/noxy/main.go` só deve listar prompt/version/disassembly.

- [ ] **Step 6: Commit** — `feat(cli)!: eprint/eiprint; diagnósticos da VM/CLI em stderr; exit code 1 para script inexistente (#56 itens 6, 15)`.

---

### Task 13: Módulos — `m.x = v` é erro de compilação; tipo nominal único (spec §8)

**Files:**
- Modify: `internal/compiler/compiler.go:696` (AssignStmt/MemberAccess), `:2849-2883` (`areTypesCompatible`), helpers novos
- Modify: `internal/compiler/function_types.go:234`, `internal/compiler/generics_unify.go:88`, `internal/compiler/generics_target.go:378`, `internal/compiler/generics.go:881`
- Test: `internal/vm/module_exports_test.go`, `internal/compiler/module_exports_test.go`

**Interfaces:**
- Produces: `(c *Compiler) typesEquivalent(a, b ast.NoxyType) bool`, `(c *Compiler) structDeclaration(name string) *ast.StructStatement`, `looselySameType(a, b ast.NoxyType) bool`, `unqualifiedTypeString(t ast.NoxyType) string`.

- [ ] **Step 1: Testes que falham**

`internal/vm/module_exports_test.go` acrescentar:

```go
func writeModuleFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func runModuleProgram(t *testing.T, root, source string) (value.Value, error) {
	t.Helper()
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	code, _, err := compiler.NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"), root).Compile(program)
	if err != nil {
		return value.NewNull(), err
	}
	machine := NewWithConfig(VMConfig{RootPath: root})
	reported := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			reported = args[0]
		}
		return value.NewNull()
	})
	return reported, machine.Interpret(code)
}

const geometryModule = `struct Point
    x: int
    y: int
end
func dist2(a: Point, b: Point) -> int
    return (a.x - b.x) * (a.x - b.x) + (a.y - b.y) * (a.y - b.y)
end
func apply(f: func(Point) -> int, p: Point) -> int
    return f(p)
end
func first<T>(a: T, b: T) -> T
    return a
end
`

func TestNamespaceAndSelectNameTheSameStruct(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{"geometry.nx": geometryModule})
	reported, err := runModuleProgram(t, root, `use geometry
use geometry select dist2, Point, apply, first
let a: geometry.Point = geometry.Point(0, 0)
let b: Point = Point(3, 4)
let viaSelect: int = dist2(a, b)
let viaNamespace: int = geometry.dist2(b, a)
let viaFunc: int = apply(func(p: geometry.Point) -> int return p.x end, b)
let viaGeneric: Point = first(a, b)
test_report(to_str(viaSelect) + "|" + to_str(viaNamespace) + "|" + to_str(viaFunc) + "|" + to_str(viaGeneric.x))
`)
	if err != nil {
		t.Fatal(err)
	}
	if got := reported.Obj.(string); got != "25|25|3|0" {
		t.Fatalf("got %q", got)
	}
}

func TestLocalStructIsNotTheModuleStructOfTheSameName(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{"geometry.nx": geometryModule})
	_, err := runModuleProgram(t, root, `use geometry select dist2
struct Point
    x: int
    y: int
end
let local: Point = Point(1, 1)
dist2(local, local)
`)
	if err == nil || !strings.Contains(err.Error(), "expected Point, got Point") {
		t.Fatalf("error=%v, want nominal mismatch", err)
	}
}

func TestModuleVariableAssignmentViaNamespaceIsCompileError(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{"calc.nx": "let sp: int = 0\nfunc push() -> void\n    sp = sp + 1\nend\n"})
	_, err := runModuleProgram(t, root, "use calc\ncalc.push()\ncalc.sp = 5\n")
	if err == nil || !strings.Contains(err.Error(), "cannot assign to 'calc.sp': module variables are read-only outside the module") || !strings.Contains(err.Error(), "hint: expose a function in 'calc'") {
		t.Fatalf("error=%v", err)
	}
	reported, err := runModuleProgram(t, root, "use calc\ncalc.push()\ntest_report(calc.sp)\n")
	if err != nil || reported.AsInt != 1 {
		t.Fatalf("live read via namespace: %v / %v", reported, err)
	}
}
```

(Se a mensagem de mismatch local vs módulo sair com outro texto — ex.: `expected Point, got Point` pode ser ambíguo para o usuário —, o teste aceita o texto real; melhorar a mensagem está fora de escopo.)

- [ ] **Step 2: Rodar para ver falhar** — `go test ./internal/vm -run 'TestNamespaceAndSelect|TestLocalStructIsNot|TestModuleVariableAssignment' -v`.

- [ ] **Step 3: 8b — erro de atribuição a variável de módulo**

Em `compiler.go`, no ramo `else if memberExp, ok := n.Target.(*ast.MemberAccessExpression); ok {` (l.696), antes de `leftType, _, err := c.compileLValueBase(memberExp.Left)`:

```go
			// Variavel de modulo via namespace (`calc.sp = 5`): leitura e viva,
			// escrita de fora e recusada — o modulo expoe uma funcao (#56 §8b).
			if leftIdent, isIdent := memberExp.Left.(*ast.Identifier); isIdent {
				if module, isNamespace := c.namespaceImports[leftIdent.Value]; isNamespace && !c.isShadowedByLocal(leftIdent.Value) {
					return nil, nil, fmt.Errorf(
						"[line %d] cannot assign to '%s.%s': module variables are read-only outside the module\n  hint: expose a function in '%s' that updates it",
						c.currentLine, leftIdent.Value, memberExp.Member, module,
					)
				}
			}
```

- [ ] **Step 4: 8c — helpers**

Em `compiler.go` (perto de `areTypesCompatible`; import `strings` já existe):

```go
// typesEquivalent compara dois tipos estruturalmente, tratando como iguais
// dois nomes que designam a MESMA declaracao de struct — `Point` importado por
// select e `geometry.Point` via namespace (#56 §8c). nil so equivale a nil.
func (c *Compiler) typesEquivalent(a, b ast.NoxyType) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	switch x := a.(type) {
	case *ast.PrimitiveType:
		y, ok := b.(*ast.PrimitiveType)
		if !ok {
			return false
		}
		if x.Name == y.Name {
			return true
		}
		da := c.structDeclaration(x.Name)
		return da != nil && da == c.structDeclaration(y.Name)
	case *ast.ArrayType:
		y, ok := b.(*ast.ArrayType)
		return ok && x.Size == y.Size && c.typesEquivalent(x.ElementType, y.ElementType)
	case *ast.MapType:
		y, ok := b.(*ast.MapType)
		return ok && c.typesEquivalent(x.KeyType, y.KeyType) && c.typesEquivalent(x.ValueType, y.ValueType)
	case *ast.RefType:
		y, ok := b.(*ast.RefType)
		return ok && c.typesEquivalent(x.ElementType, y.ElementType)
	case *ast.ChanType:
		y, ok := b.(*ast.ChanType)
		return ok && c.typesEquivalent(x.ElementType, y.ElementType)
	case *ast.FunctionType:
		y, ok := b.(*ast.FunctionType)
		if !ok || len(x.Params) != len(y.Params) {
			return false
		}
		for i := range x.Params {
			if !c.typesEquivalent(x.Params[i], y.Params[i]) {
				return false
			}
		}
		return c.typesEquivalent(x.Return, y.Return)
	case *ast.GenericType:
		y, ok := b.(*ast.GenericType)
		if !ok || x.Name != y.Name || len(x.Args) != len(y.Args) {
			return false
		}
		for i := range x.Args {
			if !c.typesEquivalent(x.Args[i], y.Args[i]) {
				return false
			}
		}
		return true
	default:
		return a.String() == b.String()
	}
}

// structDeclaration resolve um nome de tipo para a declaracao de struct que
// ele designa: nome simples via c.structs; `ns.Name` via o modulo que `ns`
// importou (namespaceImports) e a descoberta MEMOIZADA de structs desse
// modulo (mesmo ponteiro que importModuleStructs guardou em c.structs). nil
// quando nao e struct conhecido.
func (c *Compiler) structDeclaration(name string) *ast.StructStatement {
	if decl, ok := c.structs[name]; ok {
		return decl
	}
	ns, base, found := strings.Cut(name, ".")
	if !found {
		return nil
	}
	module, isNamespace := c.namespaceImports[ns]
	if !isNamespace {
		return nil
	}
	discovered, loadable := c.discoverModuleStructs(module)
	if !loadable {
		return nil
	}
	return discovered[base]
}

// looselySameType e a comparacao das funcoes PURAS de unificacao de genericos
// (unify/bindTypeParam, sem *Compiler): iguais quando as strings coincidem
// apos remover qualificadores de namespace dos nomes (geometry.Point ~ Point).
// Mais permissiva que typesEquivalent — unify nunca e mais estrita que a
// checagem da pass 2, que decide com o compilador completo.
func looselySameType(a, b ast.NoxyType) bool {
	return unqualifiedTypeString(a) == unqualifiedTypeString(b)
}

func unqualifiedTypeString(t ast.NoxyType) string {
	if t == nil {
		return "<nil>"
	}
	clone := ast.CloneType(t)
	stripTypeQualifiers(clone)
	return clone.String()
}

func stripTypeQualifiers(t ast.NoxyType) {
	switch x := t.(type) {
	case *ast.PrimitiveType:
		if i := strings.LastIndex(x.Name, "."); i >= 0 {
			x.Name = x.Name[i+1:]
		}
	case *ast.ArrayType:
		stripTypeQualifiers(x.ElementType)
	case *ast.MapType:
		stripTypeQualifiers(x.KeyType)
		stripTypeQualifiers(x.ValueType)
	case *ast.RefType:
		stripTypeQualifiers(x.ElementType)
	case *ast.ChanType:
		stripTypeQualifiers(x.ElementType)
	case *ast.FunctionType:
		for _, param := range x.Params {
			stripTypeQualifiers(param)
		}
		stripTypeQualifiers(x.Return)
	case *ast.GenericType:
		for _, arg := range x.Args {
			stripTypeQualifiers(arg)
		}
	}
}
```

- [ ] **Step 5: 8c — usar nos cinco sites**

- `areTypesCompatible`: logo após `if expected.String() == actual.String() { return true }` inserir `if c.typesEquivalent(expected, actual) { return true }`.
- `function_types.go:234` (`areStrictTypesCompatible`, `default:`): `return expected.String() == actual.String() || c.typesEquivalent(expected, actual)`.
- `generics_unify.go:88`: `if !looselySameType(existing, actual) {`.
- `generics_target.go:378`: `if !looselySameType(existing, value) {`.
- `generics.go:881`: `if importerType == nil || !c.typesEquivalent(definedType, importerType) {`.

- [ ] **Step 6: Rodar** — `go test ./internal/compiler ./internal/vm` → PASS.

- [ ] **Step 7: Commit** — `fix(compiler): struct importado por namespace e por select é o mesmo tipo nominal (typesEquivalent em 5 sites); m.x = v é erro de compilação com hint (#56 item 8)`.

---

### Task 14: Exemplos, runner, diff de saída, benchmark final (spec: critérios 3-4)

**Files:**
- Create: `noxy_examples/test_continue.nx`, `noxy_examples/wc_stdin.nx`, `noxy_examples/test_read_line.nx`
- Modify: `noxy_examples/run_all_tests_concurrent.nx:27-33` (excluir `wc_stdin.nx`), exemplos migrados na Task 8

- [ ] **Step 1: Capturar a saída de referência dos exemplos ANTES (no commit `d523105`)**

Criar no scratchpad `capture_examples.py` (Python): para cada `noxy_examples/*.nx` fora da lista de exclusões do runner, rodar `go run ./cmd/noxy <arquivo>` com timeout 60 s, salvar `stdout`, `stderr` e exit code em `<outdir>/<nome>.txt`. Rodar uma vez com `git stash`-free: `git worktree add <scratch>/ref d523105` e executar o script DENTRO do worktree de referência (saída em `<scratch>/ref_out`), depois no branch (saída em `<scratch>/new_out`), e `diff -r`. Diferenças aceitas: timestamps/aleatoriedade/ambiente, `read_lines` sem `""`, mensagens que mudaram de stdout para stderr.

- [ ] **Step 2: Exemplos novos**

`noxy_examples/test_continue.nx`:

```noxy
// continue em while e for (#56 item 9)
let impares: int[] = []
let i: int = 0
while i < 10 do
    i = i + 1
    if i % 2 == 0 then continue end
    append(impares, i)
end
assert(length(impares) == 5, "5 impares")
assert(impares[4] == 9, "ultimo impar = 9")

let soma: int = 0
for n in [1, 2, 3, 4, 5, 6] do
    let dobro: int = n * 2
    if dobro % 4 == 0 then continue end
    soma = soma + dobro
end
assert(soma == 2 + 6 + 10, "soma dos dobros nao multiplos de 4")
print("continue ok")
```

`noxy_examples/test_read_line.nx` (escreve um arquivo temporário no próprio diretório, lê linha a linha, apaga):

```noxy
use io
let path: string = "test_read_line.tmp"
let w: io.File = io.open(path, "w")
io.write(w, "alfa\nbeta\ngama\n")
io.close(w)
let r: io.File = io.open(path, "r")
let count: int = 0
let line: io.IOResult = io.read_line(r)
while line.ok do
    count = count + 1
    line = io.read_line(r)
end
io.close(r)
assert(count == 3, "3 linhas")
assert(line.error == "EOF", "EOF explicito")
let lines: io.IOLinesResult = io.read_lines(io.open(path, "r"))
assert(length(lines.data) == 3, "read_lines sem linha vazia final")
assert(io.remove(path), "remove")
print("read_line ok")
```

`noxy_examples/wc_stdin.nx` (excluído do runner; K&R 1.5.4):

```noxy
// wc: conta linhas, palavras e caracteres de stdin (K&R 1.5.4)
// uso: printf 'a b\nc\n' | noxy noxy_examples/wc_stdin.nx
use io
let in: io.File = io.stdin()
let nl: int = 0
let nw: int = 0
let nc: int = 0
let r: io.IOResult = io.read_line(in)
while r.ok do
    nl = nl + 1
    nc = nc + length(r.data) + 1
    let in_word: bool = false
    for ch in r.data do
        if ch == " " || ch == "\t" then
            in_word = false
        elif !in_word then
            in_word = true
            nw = nw + 1
        end
    end
    r = io.read_line(in)
end
print(f"{nl} {nw} {nc}")
```

Adicionar `"wc_stdin.nx"` à lista `exclusions` do runner (l.27-33).

- [ ] **Step 3: Rodar o runner e o diff**

Run: `go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx` → todos `[PASS]` (171 + 2 novos). Rodar `printf 'um dois\ntres\n' | go run ./cmd/noxy noxy_examples/wc_stdin.nx` → `2 3 12`. Rodar `capture_examples.py` no branch e `diff -r` com a referência; registrar o resumo no commit.

- [ ] **Step 4: Benchmark final** — repetir o Step 11 da Task 1 (3 rodadas intercaladas) e registrar.

- [ ] **Step 5: Commit** — `test(examples): test_continue, test_read_line, wc_stdin (excluído do runner); runner 100 % (#56)`.

---

### Task 15: Documentação, CHANGELOG, versão (spec: todos os itens de docs)

**Files:**
- Modify: `docs/NOXY_LANGUAGE_SPEC.md` §1.2, §2.1, §2.2, §7, §8, §9, §10, §11, §12, §13; `README.md` (Builtin Functions, badge); `AGENTS.md` (§E, §Segurança); `CHANGELOG.md`; `internal/version/version.go`

- [ ] **Step 1: Spec da linguagem**

- §1.2 Keywords: `continue` na linha Control Flow.
- §2.1: exemplos de float `1.5e3`, `2E-10`; nota "`.5` não é aceito".
- §2.2 (map): "A ordem de iteração e de impressão de um `map` é indefinida (Go)."
- §7 If/While: parágrafo novo — "A condição é obrigatoriamente `bool`. Não há valores truthy/falsy: `if n` com `n: int` é erro de compilação (`condition must be bool, got int`); escreva `n != 0`, `s != ""`, `p != null`, `length(xs) > 0`. Para `any` a checagem é em runtime." + `break`/`continue` (`continue` pula para a próxima iteração: reavalia a condição no `while`, avança o elemento no `for`) + "`if c then return end` em uma linha é válido". Subseção "Limites de chamada": "a profundidade de chamada é limitada só pela memória (100 000 frames / 1 048 576 slots de operandos por VM, alocados sob demanda a partir de 64/2048) — além disso `Runtime error: stack overflow: call depth exceeds 100000 frames` / `... operand stack exceeds 1048576 slots`; dentro de `call_result` vira `Failure` de runtime, como qualquer erro."
- §8 Mathematical: "Aritmética de `int` dá a volta em overflow (complemento de dois, como Go) — `9223372036854775807 + 1` é `-9223372036854775808`, sem erro." Logical: "`!`, `&&`, `||` exigem `bool` (erro de compilação com tipo estático conhecido)". Bitwise: "`& | ^` aceitam `int` ou `bytes`; `<< >>` só `int`; erro de compilação com tipos estáticos errados; `*` unário só em `ref` (`2 ** 3` é erro)".
- §9 F-Strings: "`{{` e `}}` produzem chaves literais; uma expressão que começa por `{` (map literal) precisa de espaço: `f"{ {"a": 1}["a"] }"`; não há format spec — `f"{x:>10}"` é erro (`unexpected ":" in f-string expression`); use `fmt("%10s", x)`; string com aspas duplas dentro de `{}` exige f-string de aspas simples: `f'{"a"}'`."
- §10 I/O: `print`, `iprint` (sem newline), `eprint`/`eiprint` (stderr), `input(prompt?) -> string` ("lê uma linha de stdin; em pipe/arquivo lê todas as linhas; no EOF devolve `""` — não distingue de linha vazia; para EOF explícito use `io.read_line(io.stdin())`").
- §11 Selective Import: "`select` vincula funções e structs pelo nome; para uma variável (`let`) de topo, `select` **copia o valor no momento do import** (snapshot). Para ler estado vivo use a forma de namespace (`m.x`). Atribuir por namespace (`m.x = v`) é erro de compilação — o módulo expõe uma função. Um struct importado por `use m` (`m.Point`) e por `use m select Point` (`Point`) é o mesmo tipo."
- §12 (junto de `io.read_lines`): tabela da API `io`: `open`, `close`, `close_result`, `read`, `read_bytes`, `read_line -> IOResult (ok=false, error="EOF" no fim)`, `read_lines -> IOLinesResult (sem "" final)`, `write`, `write_result`, `write_bytes`, `write_bytes_result`, `exists`, `stat`, `remove`, `rename`, `mkdir`, `list_dir -> IOLinesResult (nomes, sem caminho; `stat(...).is_dir` para distinguir)`, `stdin() -> File` ("<stdin>", não fechável, só leitura; `read`/`read_lines` devolvem o restante); aviso "não misture `read_line` com `read`/`read_lines` no mesmo handle de arquivo comum (o segundo relê do início)". Nota `sys.exec_output` no Windows ("já roda via `cmd /C`; não aninhar `cmd /c`; saída não-UTF-8 → `ok=false`").
- §13: "Pilhas de frames e operandos crescem sob demanda (64/2048 → 100k/1M)". Rodapé: `*Version: 0.11.0*`.

- [ ] **Step 2: README e AGENTS**

README: tabela Builtin Functions ganha `eprint(expr)` (stderr), `input(prompt)`, `fmt(...)`; seção Usage: "diagnósticos em stderr; exit code 1 em erro"; badge/versão `v0.11.0` se houver. AGENTS.md §E: nota "print/iprint em stdout, eprint/eiprint em stderr; nunca `fmt.Print` para erro — use `os.Stderr`"; §Segurança: trocar o snippet "Overflow protection" por "Aritmética de int dá a volta (sem checagem) — decisão de linguagem, spec §8" e "Resource limits" por "ensureCallCapacity (calls.go) é o único ponto de checagem dos tetos FramesMax/StackMax; push() só panica com o sentinela recuperado em run()".

- [ ] **Step 3: CHANGELOG `[0.11.0] - <data>`**

Seções: `### Changed (BREAKING)` — condição só bool (com tabela de migração `if n` → `if n != 0`, `if s` → `if s != ""`, `if p` → `if p != null`, `if xs` → `if length(xs) > 0`; lista dos exemplos migrados), diagnósticos em stderr (`2>&1` para capturar como antes), `io.read_lines` sem `""` final, `io.list_dir` → `IOLinesResult`, f-string: map literal no início de `{}` precisa de espaço. `### Added` — `continue`; `eprint`/`eiprint`; `io.stdin()`, `io.read_line`, `io.list_dir`, `io.rename`, `io.write_bytes[_result]`; literais `1e3`; f-string `{{`/`}}`; `OP_ARRAY_FILL`; pilhas dinâmicas (tetos). `### Fixed` — recursão limitada a 62 (#1), panic em `int[N>2047]` (#2), `input()` com stdin redirecionado (#3), nativas de io ausentes (#4), `if c then return end` (#10), linha do erro de atribuição (#13), exit code do script inexistente (#15), `*`/`!`/`~`/bitwise estáticos (#16), `break`/`continue` fecham upvalues, `m.x = v` erro claro, `geometry.Point` ≡ `Point` (#8). `### Docs` — overflow, `exec_output`, ordem de `map`, `select` snapshot. Referência: issue #56.

- [ ] **Step 4: Versão** — `internal/version/version.go`: `const Version = "v0.11.0"`.

- [ ] **Step 5: Verificação final**

Run: `go build ./... && go vet ./... && go test ./... && go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx` → tudo verde; `git diff --numstat develop..HEAD | sort -rn | head` (nenhum arquivo com milhares de linhas por CRLF); `grep -rn "fmt.Printf(\"Runtime error\|fmt.Println(\"Runtime Error\|fmt.Printf(\"Thread" cmd internal | grep -v _test` → vazio.

- [ ] **Step 6: Commit** — `chore(version): noxy v0.11.0 — achados do K&R (#56): spec §1.2/§2/§7-§13, README, AGENTS, CHANGELOG`.

---

## Self-review do plano

- **Cobertura da spec**: item 1 → Task 1; 2 → Task 2; 3 → Task 11; 4/7/12 → Task 10; 5 → Task 8; 6/15 → Task 12; 8 → Task 13; 9 → Task 3; 10 → Task 4; 11 → Task 5; 13 → Task 6; 14 → Task 9; 16 → Task 7 (+ docs em Task 15); exemplos/runner/diff/benchmark → Task 14; docs/CHANGELOG/versão → Task 15. Hygiene test (§4) → Task 10. `architecture_test` → Task 1 Step 10. `call_result` captura overflow → teste existente `TestCallResultCapturesStackOverflow` (Task 1 Step 10).
- **Placeholders**: nenhum "TBD"; todos os passos de código trazem o código. As mensagens de erro estão literais em Global Constraints e nos passos.
- **Consistência de nomes**: `ensureCallCapacity`/`growFrames`/`growStack`/`stackOverflowPanic`/`Relocate` (Task 1); `OP_ARRAY_FILL` (Task 2); `ContinueStmt`/`emitLocalsExit`/`ContinueTarget`/`ContinueJumps` (Task 3); `exponentAhead` (Task 5); `checkBitwiseOperands` (Task 7); `checkCondition` (Task 8); `splitLines`/`lineReader`/`io_read_line`/`io_list_dir`/`io_rename` (Task 10); `stdin()`/`stdinHandle()`/`readAll`/`io_stdin` (Task 11); `diagOut`/`loadScript` (Task 12); `typesEquivalent`/`structDeclaration`/`looselySameType`/`unqualifiedTypeString`/`stripTypeQualifiers` (Task 13). A Task 10 cria `FileResource.reader`/`lineReader` que a Task 11 consome; a Task 7 muda `PrefixExpression` e a Task 8 muda `if/while` — sem sobreposição de trechos.
