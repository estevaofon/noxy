# RC-Uniqueness Fase 1 — Plano de Implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Substituir o bit sticky `Shared` por contagem de referências
duráveis (`Owners`), eliminando os O(N²) de compartilhamento morto em forma
de chamada (fase 1 da spec).

**Architecture:** Migração por **rastreamento duplo**: o contador `Owners`
entra primeiro como bookkeeping paralelo (o bit sticky continua governando o
comportamento), cada tarefa adiciona os inc/dec de um mecanismo e os audita
por testes de contagem; a penúltima tarefa vira a chave (`IsShared` passa a
ler `Owners > 1`) com os testes de aceite; a última limpa o mecanismo velho.
Assim toda tarefa termina verde e é rejeitável isoladamente.

**Tech Stack:** Go (interpretador bytecode em `internal/vm`, compilador em
`internal/compiler`), testes `go test`, benchmarks PowerShell do repo.

**Spec:** `docs/superpowers/specs/2026-08-17-cow-rc-uniqueness-design.md`

## Global Constraints

- Contrato semântico da spec CoW 0.4.0 INALTERADO (spec §3); corpus de 130
  exemplos com saídas idênticas é critério de aceite.
- Direção de segurança: dec a menos é proibido; dec a mais (inflação) é
  degradação aceitável. Em dúvida, não decremente.
- Ordem retain-antes-de-release em toda substituição de ocupante de slot.
- `Retain`/`Release` saturam: sem inc acima de `math.MaxInt32/2`, sem dec
  abaixo de 0.
- Não renumerar opcodes existentes em `internal/chunk/chunk.go` (novos
  opcodes só no fim da lista; opcodes mortos ficam definidos).
- O repo usa CRLF e não é gofmt-limpo — não rodar `gofmt -w`; seguir o
  estilo local (comentários em pt-BR).
- Branch: `perf/cow-uniqueness-rc`. Commits em conventional commits pt-BR
  com o rodapé `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Baseline: `go test ./...` verde no início de cada tarefa.

---

### Task 1: Contador `Owners` no pacote value

**Files:**
- Modify: `internal/value/value.go` (structs `ObjArray`, `ObjMap`,
  `ObjInstance` — localizar campo `Shared atomic.Bool` em cada um)
- Modify: `internal/value/cow.go`
- Test: `internal/value/owners_test.go` (novo)

**Interfaces:**
- Produces: `value.Retain(v Value) bool` (true se compôsto: incrementa e
  informa que é rastreável), `value.Release(v Value)`,
  `value.OwnersCount(v Value) int32` (introspecção para testes; -1 para
  não-compostos). Campo novo `Owners atomic.Int32` ao lado de `Shared`
  (que permanece intocado nesta tarefa).

- [ ] **Step 1: Escrever os testes falhando**

```go
package value

import "testing"

func TestRetainReleaseCountsComposites(t *testing.T) {
	arr := NewArray([]Value{NewInt(1)})
	if OwnersCount(arr) != 0 {
		t.Fatalf("array novo deve nascer com Owners=0, veio %d", OwnersCount(arr))
	}
	if !Retain(arr) {
		t.Fatal("Retain de array deve retornar true")
	}
	if !Retain(arr) {
		t.Fatal("segundo Retain deve retornar true")
	}
	if OwnersCount(arr) != 2 {
		t.Fatalf("esperado Owners=2, veio %d", OwnersCount(arr))
	}
	Release(arr)
	if OwnersCount(arr) != 1 {
		t.Fatalf("esperado Owners=1 apos Release, veio %d", OwnersCount(arr))
	}
}

func TestRetainIgnoresScalarsAndStrings(t *testing.T) {
	if Retain(NewInt(7)) {
		t.Fatal("Retain de int deve retornar false")
	}
	if Retain(NewString("s")) {
		t.Fatal("Retain de string deve retornar false")
	}
	if OwnersCount(NewInt(7)) != -1 {
		t.Fatal("OwnersCount de escalar deve ser -1")
	}
	Release(NewInt(7)) // nao deve entrar em panico
}

func TestReleaseClampsAtZero(t *testing.T) {
	m := NewMap()
	Release(m)
	Release(m)
	if OwnersCount(m) != 0 {
		t.Fatalf("Release nao pode ir abaixo de 0, veio %d", OwnersCount(m))
	}
}

func TestMapAndInstanceAlsoCount(t *testing.T) {
	m := NewMap()
	Retain(m)
	if OwnersCount(m) != 1 {
		t.Fatalf("map: esperado 1, veio %d", OwnersCount(m))
	}
	inst := Value{Type: VAL_OBJ, Obj: &ObjInstance{Fields: map[string]Value{}}}
	Retain(inst)
	if OwnersCount(inst) != 1 {
		t.Fatalf("instance: esperado 1, veio %d", OwnersCount(inst))
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/value -run "TestRetain|TestRelease|TestMapAndInstance" -v`
Expected: FAIL (undefined: Retain, Release, OwnersCount)

- [ ] **Step 3: Implementação mínima**

Em cada um dos três structs (`ObjArray`, `ObjMap`, `ObjInstance`) em
`value.go`, adicionar ao lado do campo `Shared`:

```go
	// Owners conta referências duráveis (RC-uniqueness, spec 2026-08-17).
	// Durante a migração convive com Shared; a chave vira no fim da fase 1.
	Owners atomic.Int32
```

Em `cow.go`, adicionar (mantendo MarkShared/IsShared intocados):

```go
// ownersSaturation impede overflow do contador; acima disso o valor se
// comporta como permanentemente compartilhado (equivalente ao sticky).
const ownersSaturation = math.MaxInt32 / 2

func ownersOf(v Value) *atomic.Int32 {
	if v.Type != VAL_OBJ {
		return nil
	}
	switch obj := v.Obj.(type) {
	case *ObjArray:
		return &obj.Owners
	case *ObjMap:
		return &obj.Owners
	case *ObjInstance:
		return &obj.Owners
	}
	return nil
}

// Retain registra um dono durável novo. Retorna true se o valor é um
// composto rastreável (chamador decide se registra o slot para release).
func Retain(v Value) bool {
	owners := ownersOf(v)
	if owners == nil {
		return false
	}
	if owners.Load() < ownersSaturation {
		owners.Add(1)
	}
	return true
}

// Release solta um dono durável. Nunca desce abaixo de zero (dec a mais é
// proibido por design; o clamp protege contra funis duplicados).
func Release(v Value) {
	owners := ownersOf(v)
	if owners == nil {
		return
	}
	for {
		current := owners.Load()
		if current <= 0 || current >= ownersSaturation {
			return
		}
		if owners.CompareAndSwap(current, current-1) {
			return
		}
	}
}

// OwnersCount é introspecção para testes; -1 para não-compostos.
func OwnersCount(v Value) int32 {
	owners := ownersOf(v)
	if owners == nil {
		return -1
	}
	return owners.Load()
}
```

Import `math` e `sync/atomic` conforme necessário.

- [ ] **Step 4: Rodar e ver passar**

Run: `go test ./internal/value -v` — todos PASS, inclusive os antigos.

- [ ] **Step 5: Commit**

`perf(value): contador Owners de referencias duraveis (RC fase 1, bookkeeping paralelo)`

---

### Task 2: Retain de parâmetros + release central no fim do frame

**Files:**
- Modify: `internal/vm/stack.go` (struct `CallFrame` — localizar via grep
  `type CallFrame struct`)
- Modify: `internal/vm/calls.go:117-134` (`callPreparedClosure`)
- Modify: `internal/vm/unwind.go:21-63` (`finalizeCurrentFrame`)
- Test: `internal/vm/rc_uniqueness_test.go` (novo)

**Interfaces:**
- Consumes: `value.Retain/Release/OwnersCount` (Task 1).
- Produces: campo `Owned []int` em `CallFrame` (índices absolutos de slots
  de `vm.stack` retidos pelo frame); helper
  `(f *CallFrame) ownSlot(vm *VM, slot int)` que retém e registra sem
  duplicar. Tasks 3-6 usam `frame.ownSlot`.

- [ ] **Step 1: Testes falhando**

Usar o runner de programas dos testes existentes (grep
`func runTypedFunctionProgram` em `internal/vm` para o helper; ele executa
fonte noxy e devolve o valor de `test_report`). Os testes auditam contagem,
não comportamento (sticky ainda governa):

```go
package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

// O programa passa um map por valor a um helper e reporta depois do
// retorno. O teste intercepta o valor via native de teste para medir
// Owners em tres momentos.
func TestParamRetainReleasedAfterReturn(t *testing.T) {
	machine := New()
	var during, after int32
	machine.DefineNative("probe_during", func(args []value.Value) value.Value {
		during = value.OwnersCount(args[0])
		return value.NewNull()
	})
	// probe_during e native SEM assinatura fora da allowlist: hoje marca
	// sticky, e nesta fase nao mexe em Owners — o que medimos e o retain
	// do parametro de reader(), nao o do native.
	machine.DefineNative("probe_after", func(args []value.Value) value.Value {
		after = value.OwnersCount(args[0])
		return value.NewNull()
	})
	src := `
func reader(m: map[string, int]) -> int
    probe_during(m)
    return 1
end

func main()
    let m: map[string, int] = {"a": 1}
    reader(m)
    probe_after(m)
end

main()`
	if err := machine.InterpretSource(src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	// during: slot do parametro de reader retido => >= 1
	if during < 1 {
		t.Fatalf("durante a chamada esperado Owners >= 1, veio %d", during)
	}
	// after: o retain do parametro foi liberado no fim do frame => o valor
	// caiu em exatamente 1 em relacao ao pico medido durante
	if after != during-1 {
		t.Fatalf("apos o retorno esperado %d, veio %d", during-1, after)
	}
}

func TestParamReleaseRunsOnUnwind(t *testing.T) {
	machine := New()
	var after int32
	machine.DefineNative("probe_after", func(args []value.Value) value.Value {
		after = value.OwnersCount(args[0])
		return value.NewNull()
	})
	src := `
func boom(m: map[string, int]) -> int
    let arr: int[] = [1]
    return arr[99]
end

func main()
    let m: map[string, int] = {"a": 1}
    boom(m)
    probe_after(m)
end

main()`
	_ = machine.InterpretSource(src) // erro esperado (index out of bounds)
	_ = after
	// Se o runtime aborta o programa inteiro no erro, este teste vira:
	// unwind até o topo deve ter passado por finalizeCurrentFrame de boom.
	// Verificação alternativa robusta: chamar de novo num programa com
	// captura de erro se a linguagem tiver; senão, validar via teste Go
	// direto de unwindTo com frame montado à mão (ver Step 1b).
}
```

**Step 1b (obrigatório se o runtime abortar no erro):** teste Go direto —
montar VM, `callPreparedClosure` com um map como arg, forçar
`vm.unwindTo(0, frameOutcome{Err: fmt.Errorf("x")})` e verificar
`value.OwnersCount(m) == 0`.

Nota: se `InterpretSource` não existir com esse nome, localizar o
equivalente usado por `runTypedFunctionProgram` e adaptar a chamada — o
formato do programa e das asserções não muda.

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/vm -run TestParamRetain -v`
Expected: FAIL — `after == during` (release não existe ainda).

- [ ] **Step 3: Implementação**

Em `CallFrame` adicionar campo e helper:

```go
	// Owned: slots absolutos de vm.stack retidos por este frame
	// (parametros e lets). Liberados em finalizeCurrentFrame.
	Owned []int
```

```go
// ownSlot retém o composto no slot e o registra para release no fim do
// frame. Idempotente por slot: duplicata causaria release dobrado (dec a
// menos — proibido pela spec §8.2).
func (f *CallFrame) ownSlot(vm *VM, slot int) {
	v := vm.stack[slot]
	if !value.Retain(v) {
		return
	}
	for _, existing := range f.Owned {
		if existing == slot {
			// Slot ja possuido: o retain acima cobre o ocupante novo; o
			// release do ocupante velho e responsabilidade do site que
			// sobrescreveu (Task 3+). Nao registrar de novo.
			return
		}
	}
	f.Owned = append(f.Owned, slot)
}
```

Em `callPreparedClosure` (calls.go), após criar o `frame` e antes do push
em `vm.frames`:

```go
	// RC: parametros sem ref sao vinculos duraveis do frame novo
	params := closure.Function.Params
	for i := 0; i < argCount; i++ {
		if i < len(params) && params[i].IsRef {
			continue
		}
		frame.ownSlot(vm, frame.LocalBase+1+i)
	}
```

(Slot do parâmetro i é `LocalBase+1+i`: `LocalBase = stackTop-argCount-1`
aponta para o callee; args começam em `LocalBase+1` — conferir com
`baseArgs := vm.stackTop - argCount` em calls.go:99.)

ATENÇÃO: `frame.ownSlot` precisa do `vm` antes de o frame estar em
`vm.frames` — assinatura recebe `vm` explicitamente por isso.

Em `finalizeCurrentFrame` (unwind.go), **depois** do loop de defers (os
defers ainda usam os valores) e **antes** do loop de `closeUpvalue`/zeragem
(linha 41):

```go
	// RC: solta os vinculos duraveis do frame (retorno normal e unwind
	// passam ambos por aqui). Le o ocupante ATUAL do slot: sobrescritas
	// durante a vida do frame ja fizeram seu proprio release/retain.
	for _, slot := range frame.Owned {
		if slot < vm.stackTop {
			value.Release(vm.stack[slot])
		}
	}
	frame.Owned = nil
```

- [ ] **Step 4: Rodar e ver passar**

Run: `go test ./internal/vm -run "TestParamRetain|TestParamRelease" -v` → PASS
Run: `go test ./internal/vm` → PASS (nada de comportamento mudou; sticky governa)

- [ ] **Step 5: Teste adicional — aninhamento**

```go
func TestNestedByValueCallsStackAndUnstack(t *testing.T) {
	// f(m) chama g(m) chama probe_during(m); apos tudo, probe_after.
	// during >= 2 (dois frames retendo); after == during - 2.
}
```

Escrever no mesmo formato do Step 1 (dois helpers aninhados por valor),
rodar, passar.

- [ ] **Step 6: Commit**

`perf(vm): retain de parametros por frame com release central em finalizeCurrentFrame`

---

### Task 3: Locais — `OP_OWN_LOCAL` no let e release/retain no `OP_SET_LOCAL`

**Files:**
- Modify: `internal/chunk/chunk.go` (novo opcode NO FIM da lista iota +
  case em `String()` e no disassembler: `OP_OWN_LOCAL`, sem operando)
- Modify: `internal/compiler/compiler.go:201-209` (branch local do
  LetStatement)
- Modify: `internal/vm/executor.go:210-213` (`OP_SET_LOCAL`) + novo case
  `OP_OWN_LOCAL`
- Test: `internal/vm/rc_uniqueness_test.go`

**Interfaces:**
- Consumes: `frame.ownSlot` (Task 2).
- Produces: `OP_OWN_LOCAL` — retém topo da pilha e registra o slot
  `vm.stackTop-1` no frame corrente.

- [ ] **Step 1: Testes falhando**

```go
func TestLetBindRetainsComposite(t *testing.T) {
	// programa: let m = {"a": 1} ; probe(m)
	// probe (native de teste) le OwnersCount: esperado 1 (o slot do let).
}

func TestAssignReleasesOldRetainsNew(t *testing.T) {
	// programa:
	//   let a: map[string, int] = {"x": 1}
	//   let b: map[string, int] = {"y": 2}
	//   probe_a1(a)          // Owners(a) == 1
	//   a = b                // slot de a solta o velho, retem b
	//   probe_a2(a)          // objeto de b: Owners == 2 (slots a e b)
	// e o objeto velho de a: capturar numa native antes e checar que caiu
	// para 0 depois da reatribuicao.
}
```

Escrever com natives de probe como na Task 2 (capturar `args[0]` num
`value.Value` do teste e medir `OwnersCount` depois).

- [ ] **Step 2: Ver falhar** (`go test ./internal/vm -run "TestLetBind|TestAssignReleases" -v`)

- [ ] **Step 3: Implementação**

Compiler (LetStatement, branch `c.scopeDepth > 0`, logo após
`emitMarkSharedForStore` e antes de `addLocal`) — emitir sempre (o handler
filtra escalares dinamicamente; tipos `any` podem carregar compostos):

```go
			// RC: o let e um vinculo duravel do frame (spec §4.2)
			c.emitByte(byte(chunk.OP_OWN_LOCAL))
```

Executor:

```go
		case chunk.OP_OWN_LOCAL:
			frame.ownSlot(vm, vm.stackTop-1)
```

`OP_SET_LOCAL` vira:

```go
		case chunk.OP_SET_LOCAL:
			slot := c.Code[ip]
			ip++
			idx := frame.LocalBase + int(slot)
			old := vm.stack[idx]
			vm.stack[idx] = vm.peek(0)
			// RC: retain-antes-de-release (auto-atribuicao x = x)
			frame.ownSlot(vm, idx)
			value.Release(old)
```

Nota: `ownSlot` já não duplica o registro; o retain dele cobre o ocupante
novo, e o `Release(old)` solta o antigo. Para escalares ambos são no-op.

- [ ] **Step 4: Ver passar + suíte vm inteira verde**

- [ ] **Step 5: Commit** — `perf(vm): posse de locais (OP_OWN_LOCAL) e troca contada no OP_SET_LOCAL`

---

### Task 4: Globais, upvalues e o funil de escrita via ref

**Files:**
- Modify: `internal/vm/executor.go:197-202` (`OP_SET_GLOBAL`), `:942-947`
  (`OP_SET_UPVALUE`), `:1270-1286` (`OP_GET_GLOBAL_MUT`), `:1288-1303`
  (`OP_GET_UPVALUE_MUT`)
- Modify: `internal/vm/references.go:154` (`storeReferenceValue`)
- Modify: `internal/vm/cow.go:28-42` (`unicizeThroughRefValue`)
- Modify: `internal/vm/executor.go` (`vm.closeUpvalue` — localizar via grep
  `func (vm *VM) closeUpvalue`)
- Test: `internal/vm/rc_uniqueness_test.go`

**Interfaces:**
- Consumes: `value.Retain/Release`.
- Produces: invariante "toda escrita durável = retain novo + release velho"
  para globais/upvalues/refs. O box de upvalue retém ao fechar
  (`closeUpvalue`), compensando o release do slot no fim do frame.

- [ ] **Step 1: Testes falhando** (mesmo formato de probes):
  - global: `let` global de composto → Owners 1; reatribuição → velho 0,
    novo +1.
  - escrita via `ref` (função com `ref` param fazendo `*r = novo`):
    velho released, novo retained.
  - closure capturando composto sobrevive ao frame: depois que a função
    externa retorna, `OwnersCount` do capturado ≥ 1 (box retém).

- [ ] **Step 2: Ver falhar**

- [ ] **Step 3: Implementação**

`OP_SET_GLOBAL`:

```go
			name := nameVal.Obj.(string)
			// RC: troca contada no ambiente
			if old, ok := frame.Environment.GetLocal(name); ok {
				value.Retain(vm.peek(0))
				value.Release(old)
			} else {
				value.Retain(vm.peek(0))
			}
			frame.Environment.SetLocal(name, vm.peek(0))
```

(Conferir a assinatura real de `Environment.GetLocal` — usada em
executor.go OP_GET_GLOBAL_MUT:1278.)

`OP_SET_UPVALUE`: ler o valor atual antes do `Store` (o tipo do upvalue tem
`Load()` — ver OP_GET_UPVALUE_MUT:1295), `Retain(novo)`, `Release(velho)`.

`storeReferenceValue` (funil de STORE_REF/STORE_VIA_REF/
SET_PROPERTY_DEREF): antes de gravar,

```go
	stored, _, store, err := vm.referenceStorage(ref)
	if err != nil {
		return err
	}
	value.Retain(updated)
	value.Release(stored)
	store(updated)
```

(Adaptar ao corpo real da função — a estrutura com `referenceStorage` já
existe; adicionar o par retain/release imediatamente antes do `store`.)

`unicizeThroughRefValue`: no branch `if changed { store(v) }` →

```go
	if changed {
		value.Retain(v)
		value.Release(stored)
		store(v)
	}
```

`OP_GET_GLOBAL_MUT` e `OP_GET_UPVALUE_MUT`: mesmos pares em torno de
`owner.SetLocal(name, v)` / `upv.Store(v)` quando `changed`.

`closeUpvalue`: ao mover o valor do slot para o box, `value.Retain(v)` —
o box é dono durável; o slot será released pelo frame.

- [ ] **Step 4: Ver passar + suíte vm verde**

- [ ] **Step 5: Commit** — `perf(vm): troca contada em globais, upvalues e escrita via ref`

---

### Task 5: Contêineres — construção, escrita, clone e caminhos MUT

**Files:**
- Modify: `internal/vm/executor.go:972-982` (`OP_ARRAY`), `:984-1012`
  (`OP_MAP`), `:1114-1149` (`OP_SET_INDEX`), `:1189-1217`
  (`OP_SET_PROPERTY`), `:1259-1268` (`OP_GET_LOCAL_MUT`), `:1305-1349`
  (`OP_GET_INDEX_MUT`), `:1351-1389` (`OP_GET_PROP_MUT`)
- Modify: `internal/vm/calls.go:50-64` (`callPreparedValue` construtor),
  `:138-175` (`copyValue`)
- Test: `internal/vm/rc_uniqueness_test.go`

**Interfaces:**
- Consumes: `value.Retain/Release`.
- Produces: invariante "elemento/campo de contêiner é dono durável" em
  todos os caminhos de escrita e no clone.

- [ ] **Step 1: Testes falhando**:
  - `let arr = [x]` onde x é map: Owners do map == 2 (slot de x + elemento).
  - `arr[0] = y`: velho released, novo retained.
  - `m["k"] = v` sobre chave existente: velho released.
  - `s.campo = v` (OP_SET_PROPERTY): idem.
  - construtor `Box(m)`: campo retém (+1).
  - clone: programa que compartilha e muta; após o clone, filhos com +1
    (medir por probe no filho).
  - caminho MUT: `a[0].x = 1` com `a[0]` compartilhado — o clone gravado de
    volta retém e o velho released (auditar por contagem no velho).

- [ ] **Step 2: Ver falhar**

- [ ] **Step 3: Implementação** — padrão único em cada site:

`OP_ARRAY` (o MarkShared da linha 980 ganha companhia; NÃO remover o
MarkShared até a Task 8):

```go
				elements[i] = vm.pop()
				value.MarkShared(elements[i]) // sticky: sai na Task 8
				value.Retain(elements[i])     // RC: elemento e dono duravel
```

`OP_MAP` idem na linha 1009. `OP_SET_INDEX` array:

```go
					old := arr.Elements[idx]
					value.Retain(val)
					arr.Elements[idx] = val
					value.Release(old)
```

`OP_SET_INDEX` map (e todos os `mapObj.Set` de escrita):

```go
					if old, exists := mapObj.Get(key); exists {
						defer-free: value.Retain(val); mapObj.Set(key, val); value.Release(old)
					} else {
						value.Retain(val); mapObj.Set(key, val)
					}
```

(escrever inline sem defer — o pseudocódigo acima só indica a ordem).

`OP_SET_PROPERTY`: old = `instance.Fields[name]`; retain novo, gravar,
release velho. `callPreparedValue` (linha 57): adicionar `value.Retain(arg)`
ao lado do MarkShared. `copyValue`: nos três branches, ao lado de cada
`value.MarkShared(el/val)` de filho, `value.Retain(...)`. Caminhos MUT
(`GET_LOCAL_MUT`, `GET_INDEX_MUT`, `GET_PROP_MUT`, e o branch de map de
`GET_PROP_MUT`): em cada store-back de clone (`vm.stack[idx] = v`,
`arr.Elements[idx] = v`, `mapObj.Set(key, v)`, `instance.Fields[name] = fieldVal`),
retain do clone + release do velho. Em `GET_LOCAL_MUT` usar
`frame.ownSlot(vm, idx)` em vez de retain cru (mantém o slot listado).

- [ ] **Step 4: Ver passar + suíte vm verde**

- [ ] **Step 5: Commit** — `perf(vm): posse contada em conteineres, construtor, clone e caminhos MUT`

---

### Task 6: Fronteiras — append/pop/delete, canais, spawn/tasks, defer, natives

**Files:**
- Modify: `internal/vm/builtins_collections.go:101-125` (`append`),
  `:133-...` (`pop`), delete (grep `"delete"` no mesmo arquivo)
- Modify: `internal/vm/builtins_concurrency.go:59-62` (spawn args),
  `:103-114` (`chan_send`), `:150-163` (`chan_recv`)
- Modify: `internal/vm/defer.go:94-106` (`markPreparedArguments`) e o
  release pós-invocação em `invokePreparedCall`/preparação (grep
  `PreparedCall{` para o ponto de captura)
- Modify: `internal/vm/task_execution.go:70-80` (args de task)
- Modify: `internal/vm/calls.go:38-44` (natives sem assinatura)
- Test: `internal/vm/rc_uniqueness_test.go`

**Interfaces:**
- Consumes: `value.Retain/Release`, `frame.ownSlot`.
- Produces: fronteiras completas da tabela §4.2 da spec.

- [ ] **Step 1: Testes falhando**:
  - `append(ref a, item)`: item +1; `pop(ref a)`: elemento removido −1.
  - `delete(m, k)`: valor removido −1.
  - `chan_send`/`chan_recv` no mesmo task: envia (+1), recebe (−1) — medir
    antes/depois com probes.
  - defer capturando composto: +1 na captura; após rodar o defer
    (fim da função externa), −1.
  - native sem assinatura fora da allowlist: +1 permanente (e o teste
    existente `TestUnlistedNativeStillMarksArgs` continua verde até a
    Task 8).

- [ ] **Step 2: Ver falhar**

- [ ] **Step 3: Implementação**

`append` (linha 120): `value.Retain(item)` ao lado do MarkShared.
`pop`: após remover `last := arr.Elements[len-1]`, `value.Release(last)`.
`delete`: obter o valor antes de deletar; `value.Release(old)`.
`chan_send` (linha 111): `value.Retain(args[1])` ao lado do MarkShared.
`chan_recv` (linha 162): antes de `return val`, `value.Release(val)` (o
valor saiu do buffer; o vínculo seguinte retém).
Spawn (linha 60): `value.Retain(arg)` ao lado do MarkShared **e** registrar
o slot no frame da thread após criá-lo:

```go
	for i := range threadArgs {
		if value.OwnersCount(threadArgs[i]) >= 0 {
			frame.Owned = append(frame.Owned, 1+i)
		}
	}
```

(slots 1..argCount porque LocalBase=0 e slot 0 é a função; o retain já
aconteceu no push acima — NÃO usar ownSlot aqui, que retém de novo.)
CUIDADO: este é o único lugar onde registro e retain ficam separados;
comentar no código.

`markPreparedArguments` (defer/tasks): `value.Retain(args[i])` ao lado do
MarkShared — a captura em `PreparedCall.Arguments`/args de task é durável.
Release da captura: em `invokePreparedCall`, após a invocação completar
(no defer de limpeza que já zera os slots, adicionar
`value.Release(call.Arguments[i])` para cada arg — a invocação já retomou
posse via `callPreparedClosure`). Em `task_execution.go`, espelhar: release
dos `preparedArguments` quando a task conclui (grep pelo ponto onde a task
termina e os argumentos saem de escopo).
Natives sem assinatura (calls.go:42): `value.Retain(args[i])` ao lado do
MarkShared (retenção permanente conservadora — sem release; comentar).

- [ ] **Step 4: Ver passar + suíte vm verde + `go test ./internal/vm -race -run "Chan|Spawn|Task"`**

- [ ] **Step 5: Commit** — `perf(vm): posse contada nas fronteiras (builtins mutantes, canais, tasks, defer, natives)`

---

### Task 7: A CHAVE — `IsShared` passa a ler `Owners`

**Files:**
- Modify: `internal/value/cow.go` (`IsShared`)
- Create: `benchmarks/bench_value_call_mutate.nx`
- Test: `internal/vm/rc_uniqueness_test.go`

**Interfaces:**
- Consumes: tudo acima.
- Produces: comportamento novo — clones só com dono vivo.

- [ ] **Step 1: Red test de aceite (ANTES da chave)**

```go
func TestByValueCallLoopClonesO1AfterFlip(t *testing.T) {
	machine := New()
	ResetCloneCount()
	src := `
struct State
    payloads: map[string, string]
end

struct Db
    state: State
end

func helper(db: Db) -> int
    return 1
end

func put(db: ref Db, key: string, val: string) -> void
    let x: int = helper(db)
    db.state.payloads[key] = val
end

func main()
    let payloads: map[string, string] = {}
    let db: Db = Db(State(payloads))
    let i: int = 0
    while i < 200 do
        put(ref db, f"k{i}", "v")
        i = i + 1
    end
    test_report(length(keys(db.state.payloads)))
end

main()`
	got := interpretForTest(t, machine, src) // usar o runner real do repo
	if asInt(got) != 200 {
		t.Fatalf("programa incorreto: %v", got)
	}
	if n := CloneCountValue(); n > 8 {
		t.Fatalf("laco por-valor deveria clonar O(1); clonou %d", n)
	}
}
```

(Adaptar `interpretForTest`/`asInt` aos helpers reais; o contrato do teste
é: 200 puts com helper por valor → `CloneCountValue() <= 8`.)

- [ ] **Step 2: Ver falhar** — hoje clona ~3×200.

- [ ] **Step 3: A chave em `value/cow.go`**

```go
func IsShared(v Value) bool {
	owners := ownersOf(v)
	return owners != nil && owners.Load() > 1
}
```

(`MarkShared` continua existindo e escrevendo o bit — vira dead-weight até
a Task 8; NÃO remover ainda.)

- [ ] **Step 4: Rodar TUDO e tratar fallout caso a caso**

Run: `go test ./... 2>&1 | tail -20`
Regra da spec §3: testes de caracterização que ancoram contagem exata de
clones podem mudar **para menos** — revisar um a um, atualizar a asserção
com comentário citando a spec. Qualquer teste de INDEPENDÊNCIA que falhe é
bug de contagem (dec a menos em algum funil): parar, achar o funil, corrigir
na task correspondente, nunca "ajustar o teste".

- [ ] **Step 5: Benchmark de aceite**

Criar `benchmarks/bench_value_call_mutate.nx` (mesmo shape do red test com
N=2500 e `print(f"CHECKSUM:{...}")` no padrão da suíte, helper por valor).
Rodar contra o binário do develop e o do branch (best-of-3 manual):
esperado quadrático→flat (~4,7s → ~150ms na escala do repro N=4000).

- [ ] **Step 6: Verificação completa do repo**

- `go test ./...` verde; `go vet ./internal/...` limpo.
- `benchmarks/compare_examples.ps1` (develop × branch): 130/130 idênticos.
- `benchmarks/interleaved_compare.ps1`: benches não-alvo ≤ ~5%.
- `go test ./internal/vm -race` nos grupos de concorrência.

- [ ] **Step 7: Commit** — `perf(vm)!: unicidade por contagem de donos — clones so com dono vivo (RC fase 1)`
(sem `!` se nada observável mudou — decidir pelo fallout do Step 4; a spec
prevê paridade, então provavelmente `perf(vm):` simples.)

---

### Task 8: Limpeza — aposentar o bit sticky

**Files:**
- Modify: `internal/value/value.go` (remover `Shared atomic.Bool` dos três
  structs), `internal/value/cow.go` (remover `MarkShared`; `IsShared` fica)
- Modify: todos os call sites de `MarkShared` (grep — são os mesmos das
  Tasks 5-6 + executor 1408/1420) e `internal/compiler/cow_lowering.go`
  (parar de emitir `OP_MARK_SHARED`/`OP_COPY` de marcação; opcodes ficam
  DEFINIDOS em chunk.go, sem renumerar)
- Modify: testes que referenciam `MarkShared` diretamente
  (`cow_test.go`, `cow_mut_opcodes_test.go`, `cow_builtins_test.go` etc.):
  trocar por `value.Retain` onde o teste monta o estado "compartilhado"
- Test: suíte inteira

- [ ] **Step 1:** Remover emissões e call sites; adaptar testes (montar
  compartilhamento com dois Retains em vez de MarkShared).
- [ ] **Step 2:** `go test ./...` verde; corpus 130/130 de novo (a remoção
  não pode mudar comportamento — IsShared já lia Owners).
- [ ] **Step 3: Commit** — `refactor(vm): remove o bit sticky Shared; Owners e a unica fonte de unicidade`

---

### Task 9: Documentação e PR

**Files:**
- Modify: `CHANGELOG.md` (entrada em `### Performance` no `[Unreleased]`,
  no estilo da entrada do PR #31: mecanismo, números medidos, testes)
- Modify: `benchmarks/RESULTS.md` (nova seção no topo do registro corrido:
  develop × RC fase 1, tabela intercalada completa incluindo
  `bench_value_call_mutate`, interpretação, o que resta — fases 1.5/2 e
  snapshots vivos)
- Modify: `docs/superpowers/specs/2026-08-17-cow-rc-uniqueness-design.md`
  (status: proposta → implementada fase 1)

- [ ] **Step 1:** Rodar a suíte intercalada completa + medição do bench
  novo (mediana de 5 intercaladas), preencher RESULTS.md com números reais.
- [ ] **Step 2:** CHANGELOG + status da spec.
- [ ] **Step 3:** Commit `docs: registra RC fase 1 (CHANGELOG, RESULTS.md, spec)`.
- [ ] **Step 4:** PR para `develop` via convenções do repo (`gh pr create`,
  título `perf/cow-uniqueness-rc - Unicidade por contagem de donos elimina
  O(N²) de compartilhamento morto`, label `not available to review`,
  assignee `@me`, template Summary/Components/Test Plan com checkboxes;
  rodapé `🤖 Generated with [Claude Code](https://claude.com/claude-code)`).

---

## Self-review (executado na escrita do plano)

- **Cobertura da spec:** §4.1→Task 1; §4.2 parâmetros→Task 2, locais→Task 3,
  globais/upvalues/ref→Task 4, contêineres→Task 5, fronteiras→Task 6;
  §4.3→Task 5; §4.4→coberto por Task 2 (release lê ocupante atual; sem
  promoção especial); §4.5→Task 8; §4.6→Tasks 1 e 7; §5 fase 1→Tasks 2-7;
  §6→Task 7 Steps 5-6 e Task 9; §7→red tests distribuídos; §8.1→regra do
  Step 4 da Task 7; §8.2→`ownSlot` idempotente + teste de reatribuição.
  Fases 1.5/2 ficam explicitamente fora (spec §5).
- **Riscos apontados no plano:** spawn é o único ponto com retain/registro
  separados (comentário obrigatório); `chan_recv` release antes do bind
  seguinte deixa o valor em trânsito com count reduzido — seguro (+0 em
  trânsito, mutação sempre via slot).
- **Consistência de nomes:** `Retain/Release/OwnersCount/ownersOf`
  (value), `Owned`/`ownSlot` (frame), `OP_OWN_LOCAL` (chunk) — usados
  uniformemente nas tasks.
