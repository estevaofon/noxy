# Semântica de Valor com Copy-on-Write — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Substituir os três regimes de semântica de compostos do Noxy (aliasing em atribuição, shallow copy em chamada, `ref`) por semântica de valor uniforme via copy-on-write, com benchmark antes/depois.

**Architecture:** Bit `Shared` (sticky, atômico) em `ObjArray`/`ObjMap`/`ObjInstance`; marcação em pontos de aliasing (compilador emite `OP_MARK_SHARED`; fronteiras de runtime marcam em Go); escrita in-place só acontece em objeto único — lvalues são rebaixados (lowering) para uma cadeia de opcodes `*_MUT` que uniciza cada nível do caminho, gravando clones de volta no slot pai; `==` de compostos vira estrutural.

**Tech Stack:** Go 1.x (VM bytecode em `internal/`), testes `go test`, benchmarks em PowerShell + programas `.nx`.

**Spec:** `docs/superpowers/specs/2026-08-16-cow-value-semantics-design.md` (ler antes de qualquer task).

## Global Constraints

- Branch: `feat/cow-value-semantics` (worktree `.claude/worktrees/cow-value-semantics`), base c429bd7 = `origin/develop`.
- Binário baseline pré-compilado (c429bd7): `C:\Users\estev\AppData\Local\Temp\claude\D--OneDrive-Documentos-go-projects-noxy\2d6f8779-9d00-4123-844a-b740ca560894\scratchpad\noxy-baseline.exe`.
- Todos os comandos de teste rodam do root do worktree. Suite completa: `go test ./internal/...` (~70s; o pacote vm leva ~55s — para iteração use `go test ./internal/vm -run <Nome>`).
- TDD: teste falhando antes de implementação, em toda task.
- Commits frequentes, mensagem em português, rodapé `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Os benchmarks `.nx` devem rodar **idênticos** no binário baseline e no CoW (mesma sintaxe, mesmos checksums) — nunca usar nos benchmarks um padrão cujo *resultado* dependa da semântica que muda (ex.: nunca imprimir `a` depois de mutar `b` vindo de `let b = a`).
- Nada de flag de runtime para ligar/desligar CoW: a comparação de performance é binário baseline × binário do branch.

---

### Task 1: Suite de benchmarks + resultados baseline

**Files:**
- Create: `benchmarks/bench_call_readonly.nx`, `benchmarks/bench_call_ref.nx`, `benchmarks/bench_share_mutate.nx`, `benchmarks/bench_path_update.nx`, `benchmarks/bench_bubblesort.nx`, `benchmarks/bench_conway.nx`, `benchmarks/bench_map_churn.nx`, `benchmarks/bench_spawn_sum.nx`
- Create: `benchmarks/run_benchmarks.ps1`
- Create: `benchmarks/results/baseline.md` (gerado pelo script, commitado)

**Interfaces:**
- Produces: `run_benchmarks.ps1 -Binary <exe> -Label <nome> [-Runs 5]` → grava `benchmarks/results/<nome>.md` com tabela `| bench | median_ms | runs_ms | checksum |`. Task 9 consome `results/baseline.md` e o formato da tabela.

- [ ] **Step 1: Escrever os 8 programas de benchmark**

Cada programa imprime UMA linha final `CHECKSUM:<valor>` e nada mais que dependa de timing/aleatoriedade. Conteúdo:

`benchmarks/bench_call_readonly.nx` — pior caso atual (cópia O(n) por chamada em leitor puro):

```noxy
func sum_all(data: int[]) -> int
    let s: int = 0
    let i: int = 0
    while i < length(data) do
        s = s + data[i]
        i = i + 1
    end
    return s
end

func main()
    let data: int[]
    let i: int = 0
    while i < 20000 do
        append(data, i)
        i = i + 1
    end
    let total: int = 0
    let call: int = 0
    while call < 300 do
        total = total + sum_all(data)
        call = call + 1
    end
    print(f"CHECKSUM:{total}")
end
main()
```

`benchmarks/bench_call_ref.nx` — mutação in-place via `ref` (deve ficar neutro):

```noxy
func bump(data: ref int[], rounds: int) -> void
    let i: int = 0
    while i < length(data) do
        data[i] = data[i] + rounds
        i = i + 1
    end
end

func main()
    let data: int[]
    let i: int = 0
    while i < 20000 do
        append(data, i)
        i = i + 1
    end
    let r: int = 0
    while r < 300 do
        bump(data, r)
        r = r + 1
    end
    let s: int = 0
    i = 0
    while i < length(data) do
        s = s + data[i]
        i = i + 1
    end
    print(f"CHECKSUM:{s}")
end
main()
```

`benchmarks/bench_share_mutate.nx` — pior caso CoW: compartilha e muta em loop. Checksum SÓ sobre `b` (conteúdo idêntico nas duas semânticas):

```noxy
func main()
    let a: int[]
    let i: int = 0
    while i < 5000 do
        append(a, i)
        i = i + 1
    end
    let s: int = 0
    let round: int = 0
    while round < 400 do
        let b: int[] = a
        b[round % 5000] = round
        s = s + b[round % 5000]
        round = round + 1
    end
    print(f"CHECKSUM:{s}")
end
main()
```

`benchmarks/bench_path_update.nx` — custo do lowering `a[i].x = v` (single-owner, nunca compartilhado):

```noxy
struct Cell
    hits: int
    score: int
end

func main()
    let cells: Cell[]
    let i: int = 0
    while i < 2000 do
        append(cells, Cell(0, 0))
        i = i + 1
    end
    let round: int = 0
    while round < 500 do
        i = 0
        while i < 2000 do
            cells[i].hits = cells[i].hits + 1
            cells[i].score = cells[i].score + cells[i].hits
            i = i + 1
        end
        round = round + 1
    end
    let s: int = 0
    i = 0
    while i < 2000 do
        s = s + cells[i].score
        i = i + 1
    end
    print(f"CHECKSUM:{s}")
end
main()
```

`benchmarks/bench_bubblesort.nx` — sort in-place com `ref`:

```noxy
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

func main()
    let data: int[]
    let i: int = 0
    while i < 3000 do
        append(data, (i * 7919) % 104729)
        i = i + 1
    end
    bubble(data)
    let s: int = 0
    i = 0
    while i < 3000 do
        s = s + data[i] * (i % 13)
        i = i + 1
    end
    print(f"CHECKSUM:{s}")
end
main()
```

`benchmarks/bench_conway.nx` — grid 1D determinístico, gerações via `ref`:

```noxy
func idx(x: int, y: int, w: int) -> int
    return y * w + x
end

func step(grid: ref int[], next: ref int[], w: int, h: int) -> void
    let y: int = 0
    while y < h do
        let x: int = 0
        while x < w do
            let n: int = 0
            let dy: int = -1
            while dy <= 1 do
                let dx: int = -1
                while dx <= 1 do
                    if dx != 0 | dy != 0 then
                        let nx: int = (x + dx + w) % w
                        let ny: int = (y + dy + h) % h
                        n = n + grid[idx(nx, ny, w)]
                    end
                    dx = dx + 1
                end
                dy = dy + 1
            end
            let alive: int = grid[idx(x, y, w)]
            if alive == 1 then
                if n == 2 | n == 3 then
                    next[idx(x, y, w)] = 1
                else
                    next[idx(x, y, w)] = 0
                end
            else
                if n == 3 then
                    next[idx(x, y, w)] = 1
                else
                    next[idx(x, y, w)] = 0
                end
            end
            x = x + 1
        end
        y = y + 1
    end
end

func main()
    let w: int = 60
    let h: int = 60
    let grid: int[]
    let next: int[]
    let i: int = 0
    while i < w * h do
        append(grid, (i * 2654435761) % 7 % 2)
        append(next, 0)
        i = i + 1
    end
    let gen: int = 0
    while gen < 60 do
        step(grid, next, w, h)
        let j: int = 0
        while j < w * h do
            grid[j] = next[j]
            j = j + 1
        end
        gen = gen + 1
    end
    let s: int = 0
    i = 0
    while i < w * h do
        s = s + grid[i] * (i % 97)
        i = i + 1
    end
    print(f"CHECKSUM:{s}")
end
main()
```

`benchmarks/bench_map_churn.nx` — escrita/leitura intensa de map single-owner:

```noxy
func main()
    let m: map[string, int] = {}
    let round: int = 0
    let s: int = 0
    while round < 40 do
        let i: int = 0
        while i < 3000 do
            let k: string = f"k{i % 500}"
            m[k] = round * i
            s = s + m[k] % 7
            i = i + 1
        end
        round = round + 1
    end
    print(f"CHECKSUM:{s}")
end
main()
```

`benchmarks/bench_spawn_sum.nx` — paralelismo com canais transportando escalares:

```noxy
func worker(c: any, base: int, count: int)
    let s: int = 0
    let i: int = 0
    while i < count do
        s = s + (base + i) % 1000003
        i = i + 1
    end
    chan_send(c, s)
end

func main()
    let c: any = make_chan(4)
    spawn_task(worker, c, 0, 2000000)
    spawn_task(worker, c, 500000, 2000000)
    spawn_task(worker, c, 1000000, 2000000)
    spawn_task(worker, c, 1500000, 2000000)
    let total: int = 0
    let got: int = 0
    while got < 4 do
        total = total + to_int(chan_recv(c))
        got = got + 1
    end
    print(f"CHECKSUM:{total}")
end
main()
```

Nota: se `spawn_task` não aceitar essa forma de chamada, consultar `internal/vm/builtins_tasks.go` e `noxy_examples/concurrency_parallel_sum.nx` e ajustar para a API real (mantendo canais só com escalares).

- [ ] **Step 2: Escrever o harness**

`benchmarks/run_benchmarks.ps1`:

```powershell
param(
    [Parameter(Mandatory)][string]$Binary,
    [Parameter(Mandatory)][string]$Label,
    [int]$Runs = 5
)
$ErrorActionPreference = "Stop"
$programs = Get-ChildItem $PSScriptRoot -Filter "bench_*.nx" | Sort-Object Name
$outDir = Join-Path $PSScriptRoot "results"
New-Item -ItemType Directory -Force $outDir | Out-Null
$lines = @(
    "# Benchmark results: $Label",
    "",
    "- Binary: ``$Binary``",
    "- Date: $(Get-Date -Format s)",
    "- Runs per bench: $Runs (median reported)",
    "",
    "| bench | median_ms | runs_ms | checksum |",
    "|---|---|---|---|"
)
foreach ($p in $programs) {
    $out = & $Binary $p.FullName
    $chk = ($out | Where-Object { $_ -match "^CHECKSUM:" }) -join ";"
    if (-not $chk) { throw "$($p.Name): sem linha CHECKSUM (saida: $out)" }
    $times = @()
    for ($i = 0; $i -lt $Runs; $i++) {
        $t = Measure-Command { & $Binary $p.FullName | Out-Null }
        $times += [math]::Round($t.TotalMilliseconds, 1)
    }
    $sorted = $times | Sort-Object
    $median = $sorted[[int](($sorted.Count - 1) / 2)]
    $lines += "| $($p.Name) | $median | $($times -join ' ') | $chk |"
    Write-Host "$($p.Name): median=${median}ms checksum=$chk"
}
$lines | Set-Content (Join-Path $outDir "$Label.md")
Write-Host "wrote results/$Label.md"
```

- [ ] **Step 3: Calibrar e gerar baseline**

Rodar: `powershell -File benchmarks/run_benchmarks.ps1 -Binary <scratchpad>\noxy-baseline.exe -Label baseline`
Expected: cada bench entre ~500ms e ~5s no baseline. Se algum ficar fora, ajustar as constantes de iteração do `.nx` (só as constantes) e rodar de novo.

- [ ] **Step 4: Commit**

```bash
git add benchmarks/
git commit -m "bench: suite de benchmarks CoW + resultados baseline (c429bd7)"
```

---

### Task 2: Bit `Shared`, clone que marca filhos, contador de clones

**Files:**
- Modify: `internal/value/value.go` (structs `ObjArray:257`, `ObjMap:295`, `ObjInstance:349`)
- Modify: `internal/vm/calls.go:128-155` (`copyValue`)
- Create: `internal/vm/cow.go`
- Test: `internal/vm/cow_test.go`

**Interfaces:**
- Produces (pacote `value`): campo `Shared atomic.Bool` nos 3 structs; `func MarkShared(v Value)` (liga o bit se v é composto; no-op senão); `func IsShared(v Value) bool`.
- Produces (pacote `vm`): `func (vm *VM) unicize(v value.Value) (value.Value, bool)` — devolve (clone, true) se composto e `Shared`, senão (v, false); `var CloneCount atomic.Int64` + `func ResetCloneCount()` / `func CloneCountValue() int64`.
- `copyValue` passa a: incrementar `CloneCount`, marcar `Shared` em todo filho imediato composto do clone (os filhos são os mesmos objetos do original), e devolver clone com `Shared` desligado.

- [ ] **Step 1: Teste falhando**

`internal/vm/cow_test.go`:

```go
package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

func TestMarkSharedAndUnicize(t *testing.T) {
	machine := New()
	inner := value.NewArray([]value.Value{value.NewInt(1)})
	outer := value.NewArray([]value.Value{inner})

	if value.IsShared(outer) {
		t.Fatal("array novo não deve nascer Shared")
	}
	v, cloned := machine.unicize(outer)
	if cloned || v.Obj != outer.Obj {
		t.Fatal("unicize de objeto não-Shared deve devolver o mesmo objeto sem clonar")
	}

	value.MarkShared(outer)
	ResetCloneCount()
	v, cloned = machine.unicize(outer)
	if !cloned || v.Obj == outer.Obj {
		t.Fatal("unicize de objeto Shared deve clonar")
	}
	if value.IsShared(v) {
		t.Fatal("clone deve nascer com Shared desligado")
	}
	if !value.IsShared(inner) {
		t.Fatal("clone raso deve marcar os filhos compostos como Shared")
	}
	if CloneCountValue() != 1 {
		t.Fatalf("esperado 1 clone, contador = %d", CloneCountValue())
	}
	if v.Obj.(*value.ObjArray).Elements[0].Obj != inner.Obj {
		t.Fatal("clone raso deve compartilhar os filhos (mesmo ponteiro)")
	}
}

func TestMarkSharedIgnoresScalars(t *testing.T) {
	n := value.NewInt(7)
	value.MarkShared(n) // não deve entrar em pânico
	if value.IsShared(n) {
		t.Fatal("escalares nunca são Shared")
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/vm -run "TestMarkShared" -v`
Expected: FAIL de compilação (`MarkShared`, `unicize`, `CloneCount` não existem).

- [ ] **Step 3: Implementar**

Em `internal/value/value.go`, adicionar `Shared atomic.Bool` a `ObjArray`, `ObjMap`, `ObjInstance` e:

```go
// MarkShared liga o bit sticky de compartilhamento em compostos (CoW).
func MarkShared(v Value) {
	if v.Type != VAL_OBJ {
		return
	}
	switch obj := v.Obj.(type) {
	case *ObjArray:
		obj.Shared.Store(true)
	case *ObjMap:
		obj.Shared.Store(true)
	case *ObjInstance:
		obj.Shared.Store(true)
	}
}

func IsShared(v Value) bool {
	if v.Type != VAL_OBJ {
		return false
	}
	switch obj := v.Obj.(type) {
	case *ObjArray:
		return obj.Shared.Load()
	case *ObjMap:
		return obj.Shared.Load()
	case *ObjInstance:
		return obj.Shared.Load()
	}
	return false
}
```

Criar `internal/vm/cow.go`:

```go
package vm

import (
	"sync/atomic"

	"noxy-vm/internal/value"
)

// CloneCount conta clones CoW; visível para testes e diagnóstico.
var cloneCount atomic.Int64

func ResetCloneCount()        { cloneCount.Store(0) }
func CloneCountValue() int64  { return cloneCount.Load() }

// unicize garante posse exclusiva: clona se o composto está Shared.
func (vm *VM) unicize(v value.Value) (value.Value, bool) {
	if !value.IsShared(v) {
		return v, false
	}
	return vm.copyValue(v), true
}
```

Em `copyValue` (`calls.go:128`): no início de cada branch de composto, `cloneCount.Add(1)`; após copiar elementos/entradas/campos, iterar marcando `value.MarkShared` em cada filho. Exemplo do branch de array:

```go
case *value.ObjArray:
	cloneCount.Add(1)
	newElems := make([]value.Value, len(obj.Elements))
	copy(newElems, obj.Elements)
	for _, el := range newElems {
		value.MarkShared(el)
	}
	copied := value.NewArray(newElems)
	copied.Obj.(*value.ObjArray).RuntimeType.Store(obj.RuntimeType.Load())
	return copied
```

Mesmo padrão nos branches de map (após `Snapshot`, marcar cada valor antes de `Replace`) e instance (marcar cada `Fields[k]`).

- [ ] **Step 4: Rodar testes**

Run: `go test ./internal/vm -run "TestMarkShared" -v` → PASS.
Run: `go test ./internal/...` → tudo verde (nenhum comportamento visível mudou: nada marca Shared ainda; `copyValue` nas chamadas agora marca filhos, mas o bit não é lido por ninguém fora de `unicize`).

- [ ] **Step 5: Commit**

```bash
git add internal/value/value.go internal/vm/cow.go internal/vm/cow_test.go internal/vm/calls.go
git commit -m "feat(cow): bit Shared, unicize e contador de clones (infra, sem mudança de semântica)"
```

---

### Task 3: Opcodes `*_MUT` no VM

**Files:**
- Modify: `internal/chunk/chunk.go` (novos opcodes após `OP_MARK_RUNTIME_VALUE_TYPE:88` + casos no `String()`)
- Modify: `internal/vm/executor.go` (novos cases no switch)
- Test: `internal/vm/cow_mut_opcodes_test.go`

**Interfaces:**
- Produces: opcodes `OP_GET_LOCAL_MUT` (operando: 1 byte slot), `OP_GET_GLOBAL_MUT` (2 bytes const index), `OP_GET_UPVALUE_MUT` (1 byte slot), `OP_GET_INDEX_MUT` (sem operando; pops index+container), `OP_GET_PROP_MUT` (2 bytes const index do nome), `OP_DEREF_MUT` (sem operando; pops ref), `OP_MARK_SHARED` (sem operando; marca `peek(0)`).
- Contrato de cada `GET_*_MUT`: carrega o valor do slot; se composto `Shared`, clona via `unicize`, grava o clone de volta **no slot de origem** e empilha o clone; senão empilha o original. `OP_GET_INDEX_MUT`/`OP_GET_PROP_MUT` exigem container único (garantido pelo lowering da Task 5); se o container popped for `VAL_REF`, resolver+unicizar através de `referenceStorage`.
- Task 5 (compilador) consome esses opcodes.

- [ ] **Step 1: Teste falhando**

`internal/vm/cow_mut_opcodes_test.go` — montar chunks à mão (mesmo estilo de `executor_characterization_test.go`; consultar lá os helpers de construção de chunk):

```go
package vm

import (
	"testing"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

// Executa um chunk mínimo: [OP_GET_LOCAL_MUT 1] com um array Shared no slot 1.
func TestGetLocalMutClonesSharedAndWritesBack(t *testing.T) {
	machine := New()
	arr := value.NewArray([]value.Value{value.NewInt(10)})
	value.MarkShared(arr)

	code := &chunk.Chunk{}
	code.Write(byte(chunk.OP_GET_LOCAL_MUT), 1)
	code.Write(1, 1)
	code.Write(byte(chunk.OP_RETURN), 1)

	// Frame raiz: slot 0 = script, slot 1 = local
	machine.push(value.NewNull())
	machine.push(arr)
	// Executar chunk com frame apontando LocalBase=0 — usar o mesmo
	// mecanismo de interpretação dos testes de caracterização do executor.
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}

	slotVal := machine.stack[1]
	if slotVal.Obj == arr.Obj {
		t.Fatal("slot deveria conter o clone, não o original Shared")
	}
	if value.IsShared(slotVal) {
		t.Fatal("clone no slot deve estar unshared")
	}
}
```

Nota de execução: se `Interpret` resetar a pilha, seguir o padrão exato de `executor_characterization_test.go` para executar um chunk com estado de pilha pré-montado (existe teste lá que faz isso; copiar o mecanismo). O ponto do teste é: **clone gravado de volta no slot + empilhado**.

Acrescentar testes equivalentes para: `OP_GET_INDEX_MUT` (array Shared dentro de array único → filho clonado e gravado em `Elements[i]`), `OP_GET_PROP_MUT` (campo Shared de instância única), `OP_DEREF_MUT` (ref para global com composto Shared → clone gravado via setter, verificado com `GetLocal` do owner), `OP_MARK_SHARED` (empilha array, executa, `IsShared` == true).

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/vm -run "Mut" -v`
Expected: FAIL de compilação (opcodes não existem).

- [ ] **Step 3: Implementar opcodes**

`internal/chunk/chunk.go` — adicionar após `OP_MARK_RUNTIME_VALUE_TYPE`:

```go
	OP_GET_LOCAL_MUT
	OP_GET_GLOBAL_MUT
	OP_GET_UPVALUE_MUT
	OP_GET_INDEX_MUT
	OP_GET_PROP_MUT
	OP_DEREF_MUT
	OP_MARK_SHARED
```

E os casos correspondentes no `String()` do opcode (mesmo padrão dos existentes a partir da linha 93).

`internal/vm/executor.go` — novos cases (colar junto dos GET/SET correspondentes):

```go
		case chunk.OP_GET_LOCAL_MUT:
			slot := c.Code[ip]
			ip++
			idx := frame.LocalBase + int(slot)
			v, changed := vm.unicize(vm.stack[idx])
			if changed {
				vm.stack[idx] = v
			}
			vm.push(v)

		case chunk.OP_GET_GLOBAL_MUT:
			index := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			name := c.Constants[index].Obj.(string)
			owner, ok := frame.Environment.ResolveOwner(name)
			if !ok {
				return vm.runtimeError(c, ip, "undefined global variable '%s'", name)
			}
			stored, ok := owner.GetLocal(name)
			if !ok {
				return vm.runtimeError(c, ip, "undefined global variable '%s'", name)
			}
			v, changed := vm.unicize(stored)
			if changed {
				owner.SetLocal(name, v)
			}
			vm.push(v)

		case chunk.OP_GET_UPVALUE_MUT:
			slot := c.Code[ip]
			ip++
			upv := frame.Closure.Upvalues[slot]
			stored, ok := upv.Load()
			if !ok {
				return vm.runtimeError(c, ip, "invalid upvalue")
			}
			v, changed := vm.unicize(stored)
			if changed {
				upv.Store(v)
			}
			vm.push(v)

		case chunk.OP_GET_INDEX_MUT:
			indexVal := vm.pop()
			containerVal := vm.pop()
			if containerVal.Type == value.VAL_REF {
				ref, err := extractReferenceValue(containerVal)
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				stored, _, store, err := vm.referenceStorage(ref)
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				uniq, changed := vm.unicize(stored)
				if changed {
					store(uniq)
				}
				containerVal = uniq
			}
			if containerVal.Type == value.VAL_OBJ {
				if arr, ok := containerVal.Obj.(*value.ObjArray); ok {
					if indexVal.Type != value.VAL_INT {
						return vm.runtimeError(c, ip, "array index must be integer")
					}
					idx := int(indexVal.AsInt)
					if idx < 0 || idx >= len(arr.Elements) {
						return vm.runtimeError(c, ip, "array index out of bounds")
					}
					v, changed := vm.unicize(arr.Elements[idx])
					if changed {
						arr.Elements[idx] = v
					}
					vm.push(v)
					continue
				}
				if mapObj, ok := containerVal.Obj.(*value.ObjMap); ok {
					key, err := referenceMapKey(indexVal)
					if err != nil {
						return vm.runtimeError(c, ip, "%s", err)
					}
					stored, ok := mapObj.Get(key)
					if !ok {
						return vm.runtimeError(c, ip, "map key not found in mutation path")
					}
					v, changed := vm.unicize(stored)
					if changed {
						mapObj.Set(key, v)
					}
					vm.push(v)
					continue
				}
			}
			return vm.runtimeError(c, ip, "cannot index non-array/map in mutation path")

		case chunk.OP_GET_PROP_MUT:
			index := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			name := c.Constants[index].Obj.(string)
			instanceVal := vm.pop()
			if instanceVal.Type == value.VAL_REF {
				ref, err := extractReferenceValue(instanceVal)
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				stored, _, store, err := vm.referenceStorage(ref)
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				uniq, changed := vm.unicize(stored)
				if changed {
					store(uniq)
				}
				instanceVal = uniq
			}
			instance, ok := instanceVal.Obj.(*value.ObjInstance)
			if instanceVal.Type != value.VAL_OBJ || !ok {
				return vm.runtimeError(c, ip, "only instances have properties")
			}
			fieldVal, ok := instance.Fields[name]
			if !ok {
				return vm.runtimeError(c, ip, "undefined property '%s'", name)
			}
			v, changed := vm.unicize(fieldVal)
			if changed {
				instance.Fields[name] = v
			}
			vm.push(v)

		case chunk.OP_DEREF_MUT:
			refVal := vm.pop()
			ref, err := extractReferenceValue(refVal)
			if err != nil {
				return vm.runtimeError(c, ip, "%s", err)
			}
			stored, _, store, err := vm.referenceStorage(ref)
			if err != nil {
				return vm.runtimeError(c, ip, "%s", err)
			}
			v, changed := vm.unicize(stored)
			if changed {
				store(v)
			}
			vm.push(v)

		case chunk.OP_MARK_SHARED:
			value.MarkShared(vm.peek(0))
```

Se houver um disassembler com switch de operandos (procurar por `OP_GET_LOCAL` em arquivos de debug/disassembly), adicionar os novos opcodes lá com os mesmos formatos de operando.

- [ ] **Step 4: Rodar testes**

Run: `go test ./internal/vm -run "Mut" -v` → PASS.
Run: `go test ./internal/...` → verde (opcodes novos não são emitidos por ninguém ainda).

- [ ] **Step 5: Commit**

```bash
git add internal/chunk/chunk.go internal/vm/executor.go internal/vm/cow_mut_opcodes_test.go
git commit -m "feat(cow): opcodes *_MUT e OP_MARK_SHARED no VM"
```

---

### Task 4: Builtins mutantes unicizam via slot (`append`, `pop`, `delete`)

**Files:**
- Modify: `internal/vm/builtins_collections.go` (natives `append:101`, `pop:132`, `delete`)
- Test: `internal/vm/cow_builtins_test.go`

**Interfaces:**
- Consumes: `vm.unicize` (Task 2), `referenceStorage` (existente).
- Produces: helper `func (vm *VM) unicizeThroughRef(refArg value.Value) (value.Value, error)` em `internal/vm/cow.go` — resolve o `ObjRef`, uniciza o valor armazenado, grava o clone de volta pelo setter, devolve o valor único. Usado por qualquer native mutante.

- [ ] **Step 1: Teste falhando**

Em `internal/vm/cow_builtins_test.go`:

```go
package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

// append sobre array Shared deve clonar antes de mutar (o "co-dono" não vê o push).
func TestAppendUnicizesSharedTarget(t *testing.T) {
	machine := New()
	if err := interpretVMSource(t, machine, `
let a: int[]
append(a, 1)
`); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	stored, ok := machine.GetGlobal("a")
	if !ok {
		t.Fatal("global a não encontrado")
	}
	original := stored.Obj
	value.MarkShared(stored)

	if err := interpretVMSource(t, machine, `append(a, 2)`); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	after, _ := machine.GetGlobal("a")
	if after.Obj == original {
		t.Fatal("append em array Shared deveria ter clonado (CoW)")
	}
	if len(original.(*value.ObjArray).Elements) != 1 {
		t.Fatal("o objeto original não pode ter sido mutado")
	}
	if len(after.Obj.(*value.ObjArray).Elements) != 2 {
		t.Fatal("o clone deve conter o elemento novo")
	}
}
```

(Se `machine.GetGlobal` não existir com esse nome, usar o acessor real — ver `requireBuiltin` em `vm_test_helpers_test.go:48`, que usa `machine.GetGlobal`.) Repetir o padrão para `pop` e `delete`.

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/vm -run "TestAppendUnicizes|TestPopUnicizes|TestDeleteUnicizes" -v`
Expected: FAIL ("append em array Shared deveria ter clonado").

- [ ] **Step 3: Implementar**

Em `internal/vm/cow.go`:

```go
// unicizeThroughRef resolve um ObjRef de slot, garante posse exclusiva do
// composto armazenado e grava o clone de volta no slot quando clona.
func (vm *VM) unicizeThroughRef(refArg value.Value) (value.Value, error) {
	ref, err := extractReferenceValue(refArg)
	if err != nil {
		return value.Value{}, err
	}
	stored, _, store, err := vm.referenceStorage(ref)
	if err != nil {
		return value.Value{}, err
	}
	v, changed := vm.unicize(stored)
	if changed {
		store(v)
	}
	return v, nil
}
```

Em `append` (`builtins_collections.go:110`), trocar `arrVal, err := machine.resolveReferenceValue(args[0])` por `arrVal, err := machine.unicizeThroughRef(args[0])`. Mesma troca em `pop` e `delete`. Além disso, em `append`, marcar o item inserido: `value.MarkShared(item)` antes de `arr.Elements = append(...)` (o chamador ainda segura um ponteiro para ele).

- [ ] **Step 4: Rodar testes**

Run: `go test ./internal/vm -run "Unicizes" -v` → PASS.
Run: `go test ./internal/...` → verde (nada marca Shared em programas reais ainda; o item-mark do append pode disparar clones apenas se algo mutar o item depois — comportamento coberto pela suite completa).

- [ ] **Step 5: Commit**

```bash
git add internal/vm/builtins_collections.go internal/vm/cow.go internal/vm/cow_builtins_test.go
git commit -m "feat(cow): append/pop/delete unicizam o alvo através do slot ref"
```

---

### Task 5: Lowering do compilador — lvalues MUT + emissão de OP_MARK_SHARED

Esta task **muda a semântica de atribuição** (`let b = a` vira valor). É o primeiro flip visível.

**Files:**
- Modify: `internal/compiler/compiler.go` (assignment de identifier ~linhas 300-423; index assignment `424-509`; member assignment `511-581`; deref-assignments que emitem `OP_STORE_VIA_REF`/`OP_SET_PROPERTY_DEREF` — localizar com `grep -n "OP_STORE_VIA_REF\|OP_SET_PROPERTY_DEREF" internal/compiler/*.go`)
- Test: `internal/compiler/cow_lowering_test.go`, `internal/vm/value_semantics_test.go`

**Interfaces:**
- Consumes: opcodes da Task 3.
- Produces:
  - `func (c *Compiler) compileLValueBase(expr ast.Expression) (ast.NoxyType, error)` — compila a base de um lvalue emitindo a cadeia MUT: `*ast.Identifier` → `OP_GET_LOCAL_MUT`/`OP_GET_GLOBAL_MUT`/`OP_GET_UPVALUE_MUT` (mesma resolução local/upvalue/global do caso de leitura do identifier — copiar a lógica de resolução do case `*ast.Identifier` existente, trocando os opcodes); `*ast.IndexExpression` → recursão na base + compilar índice normal + `OP_GET_INDEX_MUT`; `*ast.MemberAccessExpression` → recursão na base + `OP_GET_PROP_MUT`; se o tipo resolvido for `*ast.RefType`, emitir `OP_DEREF_MUT` e devolver o tipo do elemento.
  - `func isFreshComposite(expr ast.Expression) bool` — true para `*ast.ArrayLiteral`, `*ast.MapLiteral`, `*ast.ZerosLiteral`.
  - Regra de emissão: em toda atribuição cujo RHS compila para composto possivelmente não-fresco, emitir `OP_MARK_SHARED` imediatamente após compilar o RHS (antes do `OP_SET_*`).

- [ ] **Step 1: Testes de contrato do VM (falhando)**

`internal/vm/value_semantics_test.go`:

```go
package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

func expectInt(t *testing.T, got value.Value, want int64, msg string) {
	t.Helper()
	if got.Type != value.VAL_INT || got.AsInt != want {
		t.Fatalf("%s: esperado %d, veio %#v", msg, want, got)
	}
}

func TestAssignmentIsValueCopy(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let a: int[]
    append(a, 1)
    let b: int[] = a
    b[0] = 99
    test_report(a[0])
end
main()
`)
	expectInt(t, got, 1, "atribuição deve ser cópia (a[0] intacto)")
}

func TestNestedAssignmentIsDeepIndependent(t *testing.T) {
	got := captureVMSource(t, `
struct P
    x: int
end

func main()
    let a: P[]
    append(a, P(1))
    let b: P[] = a
    b[0].x = 99
    test_report(a[0].x)
end
main()
`)
	expectInt(t, got, 1, "mutação aninhada via cópia não pode vazar")
}

func TestReadFromContainerIsValueCopy(t *testing.T) {
	got := captureVMSource(t, `
struct P
    x: int
end

func main()
    let a: P[]
    append(a, P(1))
    let p: P = a[0]
    p.x = 99
    test_report(a[0].x)
end
main()
`)
	expectInt(t, got, 1, "ler de contêiner e mutar o alias não pode vazar")
}

func TestPathMutationStillWorks(t *testing.T) {
	got := captureVMSource(t, `
struct P
    x: int
end

func main()
    let a: P[]
    append(a, P(1))
    a[0].x = 42
    test_report(a[0].x)
end
main()
`)
	expectInt(t, got, 42, "mutação pelo caminho deve funcionar")
}

func TestRefStillShares(t *testing.T) {
	got := captureVMSource(t, `
func bump(data: ref int[]) -> void
    data[0] = 77
end

func main()
    let a: int[]
    append(a, 1)
    bump(ref a)
    test_report(a[0])
end
main()
`)
	expectInt(t, got, 77, "ref continua compartilhando")
}

func TestSingleOwnerPathMutationDoesNotClone(t *testing.T) {
	ResetCloneCount()
	captureVMSource(t, `
struct P
    x: int
end

func main()
    let a: P[]
    append(a, P(0))
    let i: int = 0
    while i < 100 do
        a[0].x = a[0].x + 1
        i = i + 1
    end
    test_report(a[0].x)
end
`+"main()\n")
	if n := CloneCountValue(); n > 2 {
		t.Fatalf("mutação single-owner deveria custar no máximo os clones de construção, veio %d", n)
	}
}
```

Nota sobre a sintaxe de `ref` em chamada: confirmar em `docs/REF_SEMANTICS.md` / `noxy_examples/binary_tree.nx` a forma real de passar ref (nos exemplos, o parâmetro é `ref` e a chamada é direta: `adicionar_elemento(arr, ...)`). Ajustar `bump(ref a)` → `bump(a)` se for o caso.

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/vm -run "TestAssignmentIsValue|TestNestedAssignment|TestReadFrom|TestPathMutation|TestRefStill|TestSingleOwner" -v`
Expected: FAIL — `TestAssignmentIsValueCopy` e vizinhos devolvem 99 (aliasing atual). `TestPathMutationStillWorks` e `TestRefStillShares` devem PASSAR já (guardas de regressão).

- [ ] **Step 3: Implementar lowering + marcação**

No case de atribuição do compilador:

1. **Identifier target** (`x = val`): após compilar `n.Value`, se o tipo é composto (array/map/struct — checar `valType`) e `!isFreshComposite(n.Value)`, emitir `c.emitByte(byte(chunk.OP_MARK_SHARED))` antes do `OP_SET_LOCAL`/`OP_SET_GLOBAL`/`OP_SET_UPVALUE`. Também no caminho de `let` com inicializador não-fresco (localizar o case de `*ast.LetStatement`; `let b: int[] = a` também aliasa hoje).
2. **Index target** (`424-509`): trocar `c.Compile(indexExp.Left)` por `compileLValueBase(indexExp.Left)`; remover o `OP_DEREF` das linhas 437-439 (o `compileLValueBase` já emite `OP_DEREF_MUT` para RefType). Índice e RHS seguem compilação normal; aplicar a regra de `OP_MARK_SHARED` ao RHS composto não-fresco antes de `OP_SET_INDEX`.
3. **Member target** (`511-581`): idem — `compileLValueBase(memberExp.Left)`, remover `OP_DEREF` de 523-525, `OP_MARK_SHARED` no RHS composto não-fresco antes de `OP_SET_PROPERTY`.
4. **Deref-assignments** (`*r = val`, `OP_STORE_VIA_REF`, `OP_SET_PROPERTY_DEREF`): apenas `OP_MARK_SHARED` no RHS composto não-fresco (são rebinds de slot; não precisam de cadeia MUT).

`compileLValueBase` e `isFreshComposite` conforme a seção Interfaces. A recursão de `compileLValueBase` devolve o `NoxyType` para os type-checks existentes (linhas 437/458/467/486/523) continuarem funcionando sem mudança.

- [ ] **Step 4: Teste de bytecode do compilador (guard)**

`internal/compiler/cow_lowering_test.go` — compilar cada forma de lvalue e verificar os opcodes emitidos (padrão de `compiler_test.go` para compilar fonte; escanear `chunk.Code` por byte de opcode):

```go
package compiler

// Para cada fonte, o bytecode DEVE conter os opcodes MUT esperados e NÃO
// pode conter o GET simples correspondente alimentando o SET final.
// Casos mínimos:
//   "func f()\n    let a: int[]\n    a[0] = 1\nend"            → OP_GET_LOCAL_MUT
//   global: "let a: int[]\nfunc f()\n    a[0] = 1\nend"        → OP_GET_GLOBAL_MUT
//   "...\n    a[0][1] = 1\n..."                                → OP_GET_LOCAL_MUT + OP_GET_INDEX_MUT
//   struct: "...\n    a[0].x = 1\n..."                          → OP_GET_LOCAL_MUT + OP_GET_INDEX_MUT (base) + OP_SET_PROPERTY
//   ref param: "func f(a: ref int[])\n    a[0] = 1\nend"       → OP_DEREF_MUT
//   atribuição de alias: "let b: int[] = a"                     → OP_MARK_SHARED
//   literal fresco: "let b: int[] = [1, 2]"                     → SEM OP_MARK_SHARED
```

Escrever as asserções reais (compilar, iterar `Code` respeitando os tamanhos de operando, coletar opcodes presentes). Rodar: PASS.

- [ ] **Step 5: Rodar a suite inteira e atualizar testes da semântica antiga**

Run: `go test ./internal/... 2>&1 | tee /tmp/task5_failures.txt`
Cada teste existente que falhar por codificar aliasing de atribuição é atualizado **individualmente** para o contrato novo, com a justificativa no nome/comentário. Não silenciar nenhuma falha sem entender a causa; falha que não seja claramente "semântica antiga codificada" é bug do lowering — voltar ao Step 3.

- [ ] **Step 6: Commit**

```bash
git add internal/compiler/ internal/vm/value_semantics_test.go
git commit -m "feat(cow)!: atribuição e leitura de contêiner viram cópia de valor (lowering MUT + OP_MARK_SHARED)"
```

---

### Task 6: Cutover das fronteiras de runtime (chamadas, defer, spawn, canais, natives, construção)

Segundo flip visível: parâmetros deixam de ser shallow copy e viram valor profundo (lazy).

**Files:**
- Modify: `internal/vm/calls.go:96-105` (args de closure), `calls.go:24-40` (natives), `callPreparedValue:44-56` (construção de struct)
- Modify: `internal/vm/defer.go:94-103` (`copyPreparedArguments`)
- Modify: `internal/vm/task_execution.go:70-80`
- Modify: `internal/vm/builtins_concurrency.go` (spawn `:14`, chan_send `:100`)
- Modify: `internal/vm/executor.go` (`OP_COPY:1262`, `OP_ARRAY:972`, `OP_MAP:982`)
- Create: `internal/vm/cow_natives.go` (allowlist)
- Test: ampliar `internal/vm/value_semantics_test.go`

**Interfaces:**
- Consumes: `value.MarkShared`, `vm.unicize` (Task 2).
- Produces: `var readonlyNatives = map[string]bool{...}` em `cow_natives.go` — natives que comprovadamente não retêm nem mutam args; consultada nos dois caminhos de chamada de native.

- [ ] **Step 1: Testes de contrato (falhando)**

Adicionar a `internal/vm/value_semantics_test.go`:

```go
func TestCallArgIsDeepIndependent(t *testing.T) {
	got := captureVMSource(t, `
struct P
    x: int
end

func poke(items: P[]) -> void
    items[0].x = 99
end

func main()
    let a: P[]
    append(a, P(1))
    poke(a)
    test_report(a[0].x)
end
main()
`)
	expectInt(t, got, 1, "mutação aninhada via parâmetro não pode mais vazar")
}

func TestReadOnlyCallDoesNotClone(t *testing.T) {
	ResetCloneCount()
	captureVMSource(t, `
func total(data: int[]) -> int
    let s: int = 0
    let i: int = 0
    while i < length(data) do
        s = s + data[i]
        i = i + 1
    end
    return s
end

func main()
    let data: int[]
    let i: int = 0
    while i < 50 do
        append(data, i)
        i = i + 1
    end
    let s: int = 0
    i = 0
    while i < 20 do
        s = s + total(data)
        i = i + 1
    end
    test_report(s)
end
main()
`)
	if n := CloneCountValue(); n != 0 {
		t.Fatalf("20 chamadas só-leitura deveriam custar 0 clones, veio %d", n)
	}
}

func TestChanSendDeliversIndependentValue(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let c: any = make_chan(1)
    let a: int[]
    append(a, 1)
    chan_send(c, a)
    a[0] = 99
    let b: any = chan_recv(c)
    test_report(b[0])
end
main()
`)
	expectInt(t, got, 1, "payload de canal deve ser valor independente")
}

func TestStoredElementIsIndependent(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let inner: int[]
    append(inner, 1)
    let outer: int[][]
    append(outer, inner)
    inner[0] = 99
    test_report(outer[0][0])
end
main()
`)
	expectInt(t, got, 1, "valor guardado em contêiner deve ser independente do original")
}
```

(Se indexar `any` (`b[0]`) não compilar, receber em variável tipada ou reportar via `length`; ajustar mantendo a asserção de independência.)

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/vm -run "TestCallArg|TestReadOnlyCall|TestChanSend|TestStoredElement" -v`
Expected: `TestCallArgIsDeepIndependent` FAIL (shallow copy deixa vazar aninhado). `TestReadOnlyCall` FAIL (copyValue ansioso conta clones). Os outros dois FAIL por aliasing.

- [ ] **Step 3: Implementar o cutover**

1. `calls.go:96-105`: trocar `vm.stack[baseArgs+i] = vm.copyValue(val)` por `value.MarkShared(val)` (sem reatribuir).
2. `defer.go:94` (`copyPreparedArguments`): trocar cada `copyValue` por `value.MarkShared` (renomear para `markPreparedArguments` e atualizar os 3 call sites: `calls.go:36`, `defer.go:35`, `defer.go:53`).
3. `task_execution.go:75`: `copyValue` → `value.MarkShared`.
4. `builtins_concurrency.go` spawn: antes de `threadVM.push(arg)` no loop de args, `value.MarkShared(arg)` — mata a exceção de identidade do spawn.
5. `builtins_concurrency.go:100` chan_send: `value.MarkShared(args[1])` antes de enviar.
6. `executor.go:1262` `OP_COPY`: trocar `vm.push(vm.copyValue(val))` por `value.MarkShared(val); vm.push(val)`.
7. `executor.go:972` `OP_ARRAY`: após montar `elements`, `for _, el := range elements { value.MarkShared(el) }` — exceto se vier de literal puro… não distinguimos no runtime; marcar sempre (custo: 1 clone extra em literal aninhado mutado depois — aceito na spec §4.2).
8. `executor.go:982` `OP_MAP`: `value.MarkShared(val)` para cada valor inserido.
9. `calls.go:44-56` construção de struct: `value.MarkShared` em cada arg composto antes de gravar em `Fields`.
10. `cow_natives.go`: allowlist inicial (verificar nomes reais com `grep -rn "DefineNative(\|DefineContextualNative(\|DefineContextualNativeWithSignature(" internal/vm/ | grep -o '"[a-z_0-9]*"' | sort`): começar com `length`, `to_str`, `to_int`, `to_float`, `fmt`, `contains`, `index_of`, `typeof`. Em `callValue` (VAL_NATIVE, `calls.go:24-40`): se `!readonlyNatives[native.Name]`, marcar args compostos (no caminho com signature isso já acontece via `markPreparedArguments` para params não-ref; no caminho sem signature, marcar todos os compostos).

- [ ] **Step 4: Rodar suite e atualizar testes da semântica antiga**

Run: `go test ./internal/... 2>&1 | tee /tmp/task6_failures.txt`
Falhas esperadas conhecidas (atualizar para o contrato novo, uma a uma): `native_signatures_test.go:1815` e `:1870` (shallow copy + identidade aninhada), `defer_test.go:191/205` (shallow copy em defer), `reference_ownership_test.go`, `calls_characterization_test.go`, possivelmente `builtins_concurrency_test.go` (spawn/chan identidade). Regra da Task 5 vale: falha inexplicada = bug, não "teste velho".

- [ ] **Step 5: Commit**

```bash
git add internal/vm/
git commit -m "feat(cow)!: fronteiras de chamada/defer/spawn/canal/native marcam em vez de copiar"
```

---

### Task 7: `==` estrutural para compostos, identidade de slot para refs

**Files:**
- Modify: `internal/vm/stack.go:15-46` (`valuesEqual`)
- Test: `internal/vm/cow_equality_test.go`

**Interfaces:**
- Produces: `valuesEqual` estrutural — Task 8 depende para o corpus.

- [ ] **Step 1: Teste falhando**

```go
package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

func TestStructuralEqualityArrays(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let a: int[]
    append(a, 1)
    append(a, 2)
    let b: int[]
    append(b, 1)
    append(b, 2)
    if a == b then
        test_report(1)
    else
        test_report(0)
    end
end
main()
`)
	expectInt(t, got, 1, "[1,2] == [1,2] deve ser true (estrutural)")
}

func TestStructuralEqualityNestedAndNegative(t *testing.T) {
	a := value.NewArray([]value.Value{value.NewArray([]value.Value{value.NewInt(1)})})
	b := value.NewArray([]value.Value{value.NewArray([]value.Value{value.NewInt(1)})})
	c := value.NewArray([]value.Value{value.NewArray([]value.Value{value.NewInt(2)})})
	if !valuesEqual(a, b) {
		t.Fatal("estrutural profundo deve ser igual")
	}
	if valuesEqual(a, c) {
		t.Fatal("conteúdo diferente deve ser diferente")
	}
}
```

Acrescentar: maps iguais/diferentes (mesmas chaves/valores), instâncias (mesmo struct + campos), instância × array → false, e struct diferente com mesmos campos → false.

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/vm -run "TestStructuralEquality" -v`
Expected: FAIL (identidade de ponteiro atual).

- [ ] **Step 3: Implementar**

Reescrever o case `value.VAL_OBJ` de `valuesEqual` (`stack.go:26-27`):

```go
		case value.VAL_OBJ:
			switch ao := a.Obj.(type) {
			case *value.ObjArray:
				bo, ok := b.Obj.(*value.ObjArray)
				if !ok {
					return false
				}
				if ao == bo {
					return true
				}
				if len(ao.Elements) != len(bo.Elements) {
					return false
				}
				for i := range ao.Elements {
					if !valuesEqual(ao.Elements[i], bo.Elements[i]) {
						return false
					}
				}
				return true
			case *value.ObjMap:
				bo, ok := b.Obj.(*value.ObjMap)
				if !ok {
					return false
				}
				if ao == bo {
					return true
				}
				as, bs := ao.Snapshot(), bo.Snapshot()
				if len(as) != len(bs) {
					return false
				}
				for k, av := range as {
					bv, ok := bs[k]
					if !ok || !valuesEqual(av, bv) {
						return false
					}
				}
				return true
			case *value.ObjInstance:
				bo, ok := b.Obj.(*value.ObjInstance)
				if !ok {
					return false
				}
				if ao == bo {
					return true
				}
				if ao.Struct != bo.Struct || len(ao.Fields) != len(bo.Fields) {
					return false
				}
				for k, av := range ao.Fields {
					bv, ok := bo.Fields[k]
					if !ok || !valuesEqual(av, bv) {
						return false
					}
				}
				return true
			default:
				return a.Obj == b.Obj // strings, closures, canais…
			}
```

E adicionar case `value.VAL_REF` (identidade de slot):

```go
		case value.VAL_REF:
			ar, aok := a.Obj.(*value.ObjRef)
			br, bok := b.Obj.(*value.ObjRef)
			if !aok || !bok || ar.RefType != br.RefType {
				return false
			}
			switch ar.RefType {
			case value.REF_GLOBAL:
				return ar.GlobalOwner == br.GlobalOwner && ar.Name == br.Name
			case value.REF_UPVALUE:
				return ar.Upvalue == br.Upvalue
			case value.REF_PTR:
				return ar.Ptr == br.Ptr
			case value.REF_PROPERTY:
				return ar.Container.Obj == br.Container.Obj && ar.Name == br.Name
			case value.REF_INDEX:
				return ar.Container.Obj == br.Container.Obj && valuesEqual(ar.Index, br.Index)
			}
			return false
```

Nota: campos compostos contendo refs são comparados pelo case VAL_REF (sem deref) — sem ciclos, conforme spec §4.8.

- [ ] **Step 4: Rodar suite**

Run: `go test ./internal/vm -run "TestStructuralEquality" -v` → PASS; depois `go test ./internal/...` — atualizar qualquer teste que dependa de `==` por identidade (documentar cada um).

- [ ] **Step 5: Commit**

```bash
git add internal/vm/stack.go internal/vm/cow_equality_test.go
git commit -m "feat(cow)!: == estrutural para compostos, identidade de slot para refs"
```

---

### Task 8: Suite completa verde + corpus de exemplos baseline × CoW

**Files:**
- Create: `benchmarks/compare_examples.ps1`
- Test: suite inteira + corpus

- [ ] **Step 1: Suite completa**

Run: `go test ./internal/...`
Expected: 100% verde. Qualquer falha remanescente é tratada aqui (mesma regra: entender antes de atualizar).

- [ ] **Step 2: Script de comparação do corpus**

`benchmarks/compare_examples.ps1`:

```powershell
param(
    [Parameter(Mandatory)][string]$Baseline,
    [Parameter(Mandatory)][string]$Candidate
)
$exclude = @(
    "conway.nx", "conway_random.nx",          # loop infinito/aleatório
    "debug_http.nx", "cadastro_usuarios.nx",  # interativo/rede
    "cli_example.nx", "benchmark_parallel.nx" # interativo/timing
    # ampliar conforme necessário; documentar cada exclusão
)
$diffs = @()
Get-ChildItem "noxy_examples" -Filter "*.nx" | Where-Object { $exclude -notcontains $_.Name } | ForEach-Object {
    $b = & $Baseline $_.FullName 2>&1 | Out-String
    $c = & $Candidate $_.FullName 2>&1 | Out-String
    if ($b -ne $c) { $diffs += $_.Name }
}
if ($diffs) {
    Write-Host "DIVERGENTES:"; $diffs | ForEach-Object { Write-Host "  $_" }
    exit 1
}
Write-Host "corpus identico ($((Get-ChildItem noxy_examples -Filter *.nx).Count - $exclude.Count) exemplos)"
```

Antes de rodar: revisar `noxy_examples/` e ampliar `$exclude` com tudo que usa rede, sleep longo, aleatoriedade, DB em disco (`dynamodb*`, `aws_lambda/`, sqlite que grava `.db`) — cada exclusão com comentário.

- [ ] **Step 3: Rodar corpus**

```powershell
go build -o noxy-cow.exe ./cmd/noxy
powershell -File benchmarks/compare_examples.ps1 -Baseline <scratchpad>\noxy-baseline.exe -Candidate .\noxy-cow.exe
```

Cada divergência: classificar como (a) migração esperada da semântica (documentar no CHANGELOG da Task 10 e aceitar) ou (b) bug (corrigir antes de seguir). A stdlib embarcada roda dentro de vários exemplos — divergência em exemplo de stdlib é sinal forte de (b).

- [ ] **Step 4: Commit**

```bash
git add benchmarks/compare_examples.ps1
git commit -m "test(cow): comparação do corpus de exemplos baseline × CoW"
```

---

### Task 9: Benchmark depois + RESULTS.md

**Files:**
- Create: `benchmarks/results/cow.md` (gerado)
- Create: `benchmarks/RESULTS.md`

- [ ] **Step 1: Rodar com o binário CoW**

```powershell
go build -o noxy-cow.exe ./cmd/noxy
powershell -File benchmarks/run_benchmarks.ps1 -Binary .\noxy-cow.exe -Label cow
```

- [ ] **Step 2: Verificar critérios da spec §5.3**

- Checksums idênticos a `results/baseline.md` em TODOS os benches (divergência = bug, voltar).
- `bench_call_readonly`: melhora esperada (≥2x é a expectativa; qualquer regressão aqui é bug de marcação espúria — investigar com `CloneCountValue`).
- `bench_call_ref`, `bench_path_update`, `bench_bubblesort`, `bench_conway`, `bench_map_churn`: regressão ≤ ~5%. Acima disso: perfilar (provável marcação espúria ou unicize em hot path; conferir emissão de `OP_MARK_SHARED` em falsos positivos).
- `bench_share_mutate`: regressão livre, registrada.

- [ ] **Step 3: Escrever `benchmarks/RESULTS.md`**

Tabela consolidada: bench | baseline_ms | cow_ms | delta % | veredito, mais um parágrafo interpretando (onde ganhou, onde pagou, e a migração `ref` para o pior caso). Copiar os números reais dos dois arquivos de results.

- [ ] **Step 4: Commit**

```bash
git add benchmarks/results/cow.md benchmarks/RESULTS.md
git commit -m "bench: resultados CoW vs baseline e análise consolidada"
```

---

### Task 10: Documentação e CHANGELOG

**Files:**
- Modify: `docs/NOXY_LANGUAGE_SPEC.md` (§2.0 e §2.2 — remover shallow copy, escrever o contrato de valor; atualizar "Pass-by-Value Behavior" de arrays/maps/structs)
- Modify: `docs/REF_SEMANTICS.md` (ref = único mecanismo de compartilhamento)
- Modify: `docs/concurrency.md` e `docs/CONCURRENCY.md` se ambos existirem (canais transportam valores; exceção do spawn removida)
- Modify: `CHANGELOG.md` (seção `[Unreleased]`)

- [ ] **Step 1: CHANGELOG**

Em `[Unreleased]`, seção `### Changed (BREAKING)` no padrão da 0.3.0 — um item por linha da tabela da spec §3 (atribuição, leitura de contêiner, parâmetro profundo, spawn, `==`), cada um com o porquê e a migração (`ref`, mutação pelo caminho, canais). Mais `### Added`: semântica de valor CoW, benchmarks em `benchmarks/`.

- [ ] **Step 2: Specs de linguagem**

Reescrever em `docs/NOXY_LANGUAGE_SPEC.md` os blocos "Shallow-Copy Semantics" e os três "Pass-by-Value Behavior" com o contrato da spec de design §2 (copiar as 8 regras, adaptando o texto ao inglês do documento). Atualizar o parágrafo "Concurrency and composite values".

- [ ] **Step 3: Rodar tudo uma última vez**

```
go test ./internal/...
powershell -File benchmarks/compare_examples.ps1 -Baseline <scratchpad>\noxy-baseline.exe -Candidate .\noxy-cow.exe
```

- [ ] **Step 4: Commit**

```bash
git add docs/ CHANGELOG.md
git commit -m "docs(cow): contrato de semântica de valor, ref, concorrência e CHANGELOG 0.4.0"
```

---

## Self-review (feito na escrita)

- Cobertura da spec: §4.1-4.4→Task 2/3; §4.5→Task 5; §4.6→Task 4; §4.2/4.7→Task 6; §4.8→Task 7; §4.9→Task 2; §5→Tasks 1/9; §6→Tasks 5/6/8; §7→Task 10. Ordem garante que `append` uniciza ANTES de existirem objetos Shared em programas reais (Task 4 < Task 5).
- Tipos consistentes: `MarkShared(v Value)`/`IsShared(v Value) bool` (pacote value), `unicize(v) (Value, bool)` e `unicizeThroughRef(refArg) (Value, error)` (pacote vm), usados com essas assinaturas em todas as tasks.
- Riscos deixados explícitos nos steps: sintaxe real de `spawn_task`/`ref` em chamada (verificar exemplos), nome real de `GetGlobal`, mecanismo de teste com pilha pré-montada (copiar de characterization), disassembler.
