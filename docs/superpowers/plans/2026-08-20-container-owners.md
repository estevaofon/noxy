# Construtores de contêiner retêm filhos (issue #55) — Plano de Implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Os construtores de contêiner de `internal/value` (`NewArray`, `NewMapWithData`, novo `NewInstanceWith`) passam a reter cada filho composto por padrão — a mesma regra que `OP_ARRAY`/`OP_MAP`/construtor de struct já aplicam no bytecode — fechando os vazamentos de semântica de valor em `slice`, `sqlite.query`, `task_await`, `io.read_lines`, `strings.split` e plugins.

**Architecture:** Regra do runtime: *todo contêiner é dono durável de cada filho composto que guarda*. Hoje o bytecode cumpre e os natives esquecem; em vez de consertar ~30 call sites um a um, o retain entra nos construtores (`value.Retain` é no-op em escalares/strings, então é seguro para todos os sites sem allowlist — inclusive `internal/plugin`, que não tem `*VM`). Os quatro lugares que já entregam filhos retidos ("moves") usam `NewArrayAdopting` (OP_ARRAY, `copyValue`, merge de `causes`) ou deixam de reter à mão (`invokeBoundaryCall` → envelope `ok` do `call_result`). Builders que escrevem compostos em campos de instância crua migram para `NewInstanceWith`. Nada de opcode novo, nada em `ObjMap.Set/Replace`, nada em `json_loads` (#53 item 1).

**Tech Stack:** Go 1.x (`go test`, `go vet`, `gofmt`), VM Noxy (`internal/vm`, `internal/value`), runner de exemplos `noxy_examples/run_all_tests_concurrent.nx`.

**Spec:** `docs/superpowers/specs/2026-08-20-container-owners-design.md` (cópia literal da issue #55; a issue é a fonte de verdade).

## Global Constraints

- Escopo fechado da issue: **não** tocar em `NewMap()`/`NewInstance(def)` (vazios), `ObjMap.Set/Replace/Delete`, `ObjArray.Elements`, `ObjInstance.Fields`, opcodes (além da troca por `NewArrayAdopting` em `OP_ARRAY`), `json_loads` (#53 item 1), nem converter `NewMap()+Replace`/`NewInstance()+escrita` em outros builders. Builders com campos só escalares ficam como estão — diff mínimo.
- Os laços `for … { value.Retain(created) }` de `internal/vm/json_population.go` que antecedem `mapObject.Replace(...)`/`instance.Fields[...] =` **continuam** (constroem via `NewMap()`/`NewInstance()`, fora deste PR). Só os usos de `retainingArray`/`retainingMap` mudam.
- Suíte verde a cada commit: `go test ./internal/value ./internal/vm` (mín.) antes de cada commit; `go test ./...` **sem `| tail`** na verificação final.
- Programas Noxy embutidos em testes Go vivem em raw string com crase: **nunca** use `` ` `` dentro de comentários Noxy do programa (quebra o arquivo Go). Use `//` com aspas simples.
- Não editar fontes Go enquanto um `go test`/captura roda em background (memória: ~12 exemplos registraram o erro de compilação do meio da edição).
- Comandos de shell simples (o harness recusa compostos com `cd`+git no worktree); binários Noxy ad hoc via `go run ./cmd/noxy arquivo.nx` (CrowdStrike apaga .exe frescos).
- Repositório com `autocrlf`: arquivos novos via Write tool; conferir `git diff --numstat` (sem reescrita de arquivo inteiro).
- Mensagens de commit no padrão do repo (`feat(vm): …`, `fix(vm): …`, `test(vm): …`, `chore(version): …`, `docs(plan): …`), em português, terminando com `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

## Mapa de arquivos

| Arquivo | Responsabilidade nesta entrega |
|---|---|
| `internal/value/value.go` | `NewArray` retém; `NewArrayAdopting` (novo, não retém); `NewMapWithData` retém; `NewInstanceWith` (novo, retém) |
| `internal/value/constructors_test.go` (novo) | testes unitários de `Owners` dos 4 construtores |
| `internal/vm/executor.go:1292-1302` | `OP_ARRAY` → `NewArrayAdopting` (move) |
| `internal/vm/calls.go:168-182` | `copyValue` array → `NewArrayAdopting` (move) |
| `internal/vm/builtins_call_result.go` | `causes` merge → `NewArrayAdopting`; `invokeBoundaryCall` deixa de reter `result`; `retainingArray/Map` apagados; comentários RC atualizados |
| `internal/vm/builtins_json.go:186,192`, `internal/vm/json_population.go:334,480` | `retainingArray/Map` → `value.NewArray`/`value.NewMapWithData` |
| `internal/vm/builtins_sqlite.go:340-351,437-445` | `row`/`result`/`sqliteQueryError` via `NewInstanceWith` |
| `internal/vm/builtins_io.go:384-390` | `newIOReadResult` via `NewInstanceWith` |
| `internal/vm/builtins_strings.go:229-238` | `strings_split` via `NewInstanceWith` |
| `internal/vm/container_owners_test.go` (novo) | sondas `Owners` (literal, `copyValue`, `call_result`), reproduções (slice, task_await, sqlite, io, strings), não-regressão de clones |
| `internal/vm/cow_test.go:20-28` | remover o `value.Retain(inner)` manual (agora `NewArray` retém) e atualizar comentário |
| `internal/plugin/plugin_test.go` (novo) | `InterfaceToValue` aninhado → filhos com `Owners == 1` |
| `CHANGELOG.md`, `AGENTS.md` (§E), `internal/version/version.go` | entrada `### Fixed`, regra para builtins, `v0.10.1` |

---

### Task 1: `NewArrayAdopting` + `NewInstanceWith` em `internal/value`; migrar os 3 moves de array para `NewArrayAdopting`

Sem mudança de comportamento ainda: `NewArray` continua sem reter, `NewArrayAdopting` é idêntico a ele hoje. Esta task só prepara o terreno para que a Task 2 (retain no construtor) não gere double-retain nos moves.

**Files:**
- Modify: `internal/value/value.go:618-644`
- Create: `internal/value/constructors_test.go`
- Modify: `internal/vm/executor.go:1296-1302`
- Modify: `internal/vm/calls.go:175-182`
- Modify: `internal/vm/builtins_call_result.go:338-358`

**Interfaces:**
- Produces: `func NewArrayAdopting(elements []Value) Value` (não retém), `func NewInstanceWith(def *ObjStruct, fields map[string]Value) Value` (retém cada campo composto; `fields == nil` vira map vazio).

- [ ] **Step 1: Escrever os testes unitários (falham: funções não existem)**

Criar `internal/value/constructors_test.go`:

```go
package value

import "testing"

// Os construtores de contêiner são a única fronteira pela qual natives e
// plugins criam arrays/maps/instâncias; a regra do runtime — "todo contêiner
// é dono durável de cada filho composto" — é aplicada aqui para valer para
// todos os ~30 call sites sem allowlist (Retain em escalar/string é no-op).

func structForTest(fields ...string) *ObjStruct {
	return NewStruct("Envelope", fields).Obj.(*ObjStruct)
}

func TestNewArrayAdoptingKeepsOwnersAsReceived(t *testing.T) {
	child := NewArray(nil)
	Retain(child) // o chamador já reteve em nome do array (move)
	adopted := NewArrayAdopting([]Value{child, NewInt(7)})
	if got := OwnersCount(child); got != 1 {
		t.Fatalf("NewArrayAdopting nao pode reter de novo: Owners=%d, esperado 1", got)
	}
	if got := OwnersCount(adopted); got != 0 {
		t.Fatalf("o array novo nasce sem dono: Owners=%d", got)
	}
	if adopted.Obj.(*ObjArray).Elements[0].Obj != child.Obj {
		t.Fatal("NewArrayAdopting deve guardar o mesmo objeto filho")
	}
}

func TestNewInstanceWithRetainsCompositeFields(t *testing.T) {
	definition := structForTest("data", "ok", "meta")
	data := NewArray(nil)
	meta := NewMap()
	instance := NewInstanceWith(definition, map[string]Value{
		"data": data,
		"ok":   NewBool(true),
		"meta": meta,
	})
	if got := OwnersCount(data); got != 1 {
		t.Fatalf("campo array deve ter a instancia como dono: Owners=%d", got)
	}
	if got := OwnersCount(meta); got != 1 {
		t.Fatalf("campo map deve ter a instancia como dono: Owners=%d", got)
	}
	object := instance.Obj.(*ObjInstance)
	if object.Struct != definition {
		t.Fatal("NewInstanceWith deve apontar para a definicao recebida")
	}
	if object.Fields["data"].Obj != data.Obj || !object.Fields["ok"].AsBool {
		t.Fatal("NewInstanceWith deve guardar os campos recebidos")
	}
	if got := OwnersCount(instance); got != 0 {
		t.Fatalf("a instancia nova nasce sem dono: Owners=%d", got)
	}
}

func TestNewInstanceWithNilFieldsIsWritable(t *testing.T) {
	instance := NewInstanceWith(structForTest("x"), nil)
	instance.Obj.(*ObjInstance).Fields["x"] = NewInt(1) // nao pode entrar em panico (map nil)
}
```

- [ ] **Step 2: Rodar e confirmar a falha**

Run: `go test ./internal/value -run 'TestNewArrayAdopting|TestNewInstanceWith' 2>&1 | head -20`
Expected: erro de compilação `undefined: NewArrayAdopting` / `undefined: NewInstanceWith`.

- [ ] **Step 3: Implementar em `internal/value/value.go`**

Substituir o bloco `NewArray` … `NewInstance` (linhas 618-644) por:

```go
// NewArray cria um array a partir dos elementos dados. A partir da Task 2
// deste plano ele e DONO DURAVEL de cada elemento composto (Retain; no-op em
// escalares e strings) — a mesma regra de OP_ARRAY no executor. Quem ja
// reteve os elementos em nome do array usa NewArrayAdopting.
func NewArray(elements []Value) Value {
	return Value{Type: VAL_OBJ, Obj: &ObjArray{Elements: elements}}
}

// NewArrayAdopting cria um array ADOTANDO elementos que o chamador JA reteve
// em nome do array (move): nao retem de novo. Uso restrito aos sites que
// transferem posse — OP_ARRAY (executor.go), copyValue (calls.go) e o merge
// de causes do call_result (builtins_call_result.go); qualquer outro uso
// precisa de comentario `// RC: move` explicando quem reteve.
func NewArrayAdopting(elements []Value) Value {
	return Value{Type: VAL_OBJ, Obj: &ObjArray{Elements: elements}}
}

func NewMap() Value {
	mapping := &ObjMap{store: newBindingStore(nil)}
	mapping.ensureStore()
	return Value{Type: VAL_OBJ, Obj: mapping}
}

func NewMapWithData(data map[string]Value) Value {
	values := make(map[interface{}]Value, len(data))
	for k, v := range data {
		values[k] = v
	}
	mapping := NewMap()
	mapping.Obj.(*ObjMap).Replace(values)
	return mapping
}

func NewStruct(name string, fields []string) Value {
	return Value{Type: VAL_OBJ, Obj: &ObjStruct{Name: name, Fields: fields}}
}

// NewInstance cria uma instancia vazia; quem escreve compostos em Fields
// depois precisa reter a mao (como calls.go:callPreparedValue faz) ou usar
// NewInstanceWith.
func NewInstance(def *ObjStruct) Value {
	return Value{Type: VAL_OBJ, Obj: &ObjInstance{Struct: def, Fields: make(map[string]Value)}}
}

// NewInstanceWith cria uma instancia ja com os campos dados, retendo cada
// valor composto — a mesma regra do construtor de struct em bytecode
// (calls.go:callPreparedValue, "campo e dono duravel"). Escalares e strings
// sao no-op em Retain. O map recebido passa a pertencer a instancia.
func NewInstanceWith(def *ObjStruct, fields map[string]Value) Value {
	if fields == nil {
		fields = make(map[string]Value)
	}
	for _, field := range fields {
		Retain(field)
	}
	return Value{Type: VAL_OBJ, Obj: &ObjInstance{Struct: def, Fields: fields}}
}
```

(`NewArray`/`NewMapWithData` ficam com o corpo de hoje nesta task; o retain entra na Task 2.)

- [ ] **Step 4: Rodar os testes de `internal/value`**

Run: `go test ./internal/value`
Expected: `ok`.

- [ ] **Step 5: Migrar os três moves de array para `NewArrayAdopting`**

`internal/vm/executor.go` (OP_ARRAY, ~l.1296-1302):

```go
			elements := make([]value.Value, count)
			for i := count - 1; i >= 0; i-- {
				elements[i] = vm.pop()
				// RC: elemento pode continuar referenciado pela origem
				value.Retain(elements[i]) // elemento e dono duravel
			}
			// RC: move — os elementos ja foram retidos acima em nome do array.
			vm.push(value.NewArrayAdopting(elements))
```

`internal/vm/calls.go` (`copyValue`, caso `*value.ObjArray`, ~l.175-182):

```go
		newElems := make([]value.Value, len(obj.Elements))
		copy(newElems, obj.Elements)
		for _, el := range newElems {
			value.Retain(el) // RC: filho ganha dono duravel no clone
		}
		// RC: move — filhos retidos acima em nome do clone.
		copied := value.NewArrayAdopting(newElems)
		copied.Obj.(*value.ObjArray).RuntimeType.Store(obj.RuntimeType.Load())
		return copied
```

`internal/vm/builtins_call_result.go` (`deferredFailureMap`, ~l.353):

```go
		// RC: move — os herdados transferem o retain do array antigo, os
		// irmaos novos foram retidos no laco acima; NewArrayAdopting nao
		// retem de novo (um dono a mais que ninguem solta deixaria o
		// composto IsShared para sempre).
		replacement := value.NewArrayAdopting(causes)
		value.Retain(replacement)
		mapping.Set("causes", replacement)
```

- [ ] **Step 6: Rodar `internal/vm` inteiro (comportamento idêntico a hoje)**

Run: `go test ./internal/value ./internal/vm`
Expected: `ok` nos dois (≈65 s no vm).

- [ ] **Step 7: Commit**

```bash
git add internal/value/value.go internal/value/constructors_test.go internal/vm/executor.go internal/vm/calls.go internal/vm/builtins_call_result.go
git commit -m "feat(value): NewArrayAdopting (move) e NewInstanceWith (retém campos); OP_ARRAY, copyValue e merge de causes adotam elementos já retidos (#55 passo 1-2)"
```

---

### Task 2: `NewArray`/`NewMapWithData` retêm filhos compostos; `invokeBoundaryCall` deixa de reter `result`; reproduções de `slice`, `task_await` e plugin

Esta é a mudança de comportamento. Com os moves já adotantes (Task 1), o único lugar que ainda reteria em dobro é o envelope `ok` do `call_result`: `invokeBoundaryCall` (`builtins_call_result.go:143-162`) retém `result` **em nome do envelope** porque `NewMapWithData` não retinha. Agora o construtor retém, então esse retain manual sai (a regra única: quem registra a posse é o construtor).

**Files:**
- Modify: `internal/value/value.go` (`NewArray`, `NewMapWithData`)
- Modify: `internal/value/constructors_test.go`
- Modify: `internal/vm/builtins_call_result.go:139-162,241-251`
- Modify: `internal/vm/cow_test.go:20-28`
- Create: `internal/vm/container_owners_test.go`
- Create: `internal/plugin/plugin_test.go`

**Interfaces:**
- Consumes: `value.NewArrayAdopting`, `value.OwnersCount`, `markProbeReadonly` (`rc_uniqueness_test.go:22`), `captureVMSource` (`vm_test_helpers_test.go:56`), `interpretVMSource`.
- Produces: helper de teste `vmWithOwnersProbe(t) (*VM, *int32)` em `container_owners_test.go` (native `probe_owners(x)` grava `value.OwnersCount(x)`), reutilizado nas Tasks 4-5.

- [ ] **Step 1: Testes unitários dos construtores (falham)**

Acrescentar a `internal/value/constructors_test.go`:

```go
func TestNewArrayRetainsCompositeElementsOnly(t *testing.T) {
	child := NewArray(nil)
	grand := NewMap()
	array := NewArray([]Value{child, NewInt(1), NewString("s"), grand})
	if got := OwnersCount(child); got != 1 {
		t.Fatalf("array deve ser dono duravel do elemento composto: Owners=%d, esperado 1", got)
	}
	if got := OwnersCount(grand); got != 1 {
		t.Fatalf("map filho deve ter o array como dono: Owners=%d", got)
	}
	if got := OwnersCount(array); got != 0 {
		t.Fatalf("o proprio array nasce sem dono: Owners=%d", got)
	}
	elements := array.Obj.(*ObjArray).Elements
	if OwnersCount(elements[1]) != -1 || OwnersCount(elements[2]) != -1 {
		t.Fatal("escalares e strings nao tem contador (Retain e no-op)")
	}
	if OwnersCount(NewArray(nil)) != 0 || len(NewArray(nil).Obj.(*ObjArray).Elements) != 0 {
		t.Fatal("NewArray(nil) continua valendo como array vazio")
	}
}

func TestNewMapWithDataRetainsCompositeValues(t *testing.T) {
	child := NewArray(nil)
	mapping := NewMapWithData(map[string]Value{"rows": child, "count": NewInt(2)})
	if got := OwnersCount(child); got != 1 {
		t.Fatalf("map deve ser dono duravel do valor composto: Owners=%d, esperado 1", got)
	}
	stored, ok := mapping.Obj.(*ObjMap).Get("rows")
	if !ok || stored.Obj != child.Obj {
		t.Fatal("NewMapWithData deve guardar o mesmo objeto filho")
	}
	if got := OwnersCount(mapping); got != 0 {
		t.Fatalf("o proprio map nasce sem dono: Owners=%d", got)
	}
}
```

- [ ] **Step 2: Testes de VM — sondas e reproduções (falham)**

Criar `internal/vm/container_owners_test.go`:

```go
package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

// Sondas e reproducoes da issue #55: contêineres criados por natives passam
// a ser donos duraveis dos filhos compostos (value.NewArray/NewMapWithData/
// NewInstanceWith retem), com NewArrayAdopting nos moves para nao reter em
// dobro. Os programas Noxy abaixo falhavam no develop 1680266 (a copia por
// valor mutava o original).

// vmWithOwnersProbe registra o native de teste probe_owners(x), que grava
// value.OwnersCount(x) sem reter o argumento (ReadonlyArgs, como as sondas
// de rc_uniqueness_test.go).
func vmWithOwnersProbe(t *testing.T) (*VM, *int32) {
	t.Helper()
	machine := New()
	observed := int32(-99)
	machine.DefineNative("probe_owners", func(args []value.Value) value.Value {
		if len(args) == 1 {
			observed = value.OwnersCount(args[0])
		}
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "probe_owners")
	return machine, &observed
}

// Literal de array: OP_ARRAY retem e entrega ao construtor adotante — o
// elemento tem exatamente UM dono (o array). Um NewArray que retivesse de
// novo deixaria 2 e todo elemento de literal nasceria "compartilhado".
func TestArrayLiteralElementHasExactlyOneOwner(t *testing.T) {
	machine, observed := vmWithOwnersProbe(t)
	src := `
struct Pair
    a: int
    b: int
end
let t: Pair[] = [Pair(1, 1)]
probe_owners(t[0])
`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	if *observed != 1 {
		t.Fatalf("elemento de literal deve ter Owners=1 (sem double-retain em OP_ARRAY), veio %d", *observed)
	}
}

// copyValue (clone CoW raso): o array novo ganha posse dos filhos — cada
// filho passa a ter 2 donos (original + clone), nem 1 nem 3.
func TestCopyValueCloneGivesChildrenASecondOwner(t *testing.T) {
	machine := New()
	inner := value.NewArray(nil)
	outer := value.NewArray([]value.Value{inner}) // NewArray retem: inner Owners=1
	if got := value.OwnersCount(inner); got != 1 {
		t.Fatalf("pre-condicao: Owners=%d, esperado 1", got)
	}
	clone := machine.copyValue(outer)
	if got := value.OwnersCount(inner); got != 2 {
		t.Fatalf("apos copyValue o filho deve ter 2 donos (original + clone), veio %d", got)
	}
	if clone.Obj.(*value.ObjArray).Elements[0].Obj != inner.Obj {
		t.Fatal("clone raso deve compartilhar o filho (mesmo ponteiro)")
	}
}

// Envelope ok do call_result: a posse de r.value pelo envelope e registrada
// UMA vez (pelo NewMapWithData em callResultOkEnvelope); o retain manual de
// invokeBoundaryCall saiu. Owners=2 aqui significaria valor eternamente
// IsShared (clone a cada mutacao).
func TestCallResultOkValueHasExactlyOneOwner(t *testing.T) {
	machine, observed := vmWithOwnersProbe(t)
	src := `
func faz_array() -> int[]
    return [1, 2, 3]
end
let r: any = call_result(faz_array)
probe_owners(r["value"])
`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	if *observed != 1 {
		t.Fatalf("r.value deve ter o envelope como unico dono (Owners=1), veio %d", *observed)
	}
}

// slice (builtins_collections.go): a copia e dona dos elementos que leva;
// mutar a copia nao pode alcancar o original.
func TestSliceCopyDoesNotAliasOriginal(t *testing.T) {
	reported := captureVMSource(t, `
struct Pair
    a: int
    b: int
end
let t: Pair[] = [Pair(0, 0), Pair(1, 1)]
let s: Pair[] = slice(t, 0, 2)
s[0].a = 9
test_report(to_str(t[0].a) + "|" + to_str(s[0].a))
`)
	if text, _ := reported.Obj.(string); text != "0|9" {
		t.Fatalf("slice deve copiar por valor (original intacto): %q, esperado \"0|9\"", text)
	}
}

// task_await (builtins_tasks.go): o envelope e dono de value e de error.
func TestTaskAwaitEnvelopeOwnsValueAndError(t *testing.T) {
	reported := captureVMSource(t, `
func mk() -> int[]
    return [1, 2, 3]
end
func boom() -> int
    let xs: int[] = [1]
    return xs[5]
end
let t1: any = spawn_task(mk)
let r1: any = task_await(t1)
let v: any = r1["value"]
v[0] = 99
let rv: any = r1["value"]
let t2: any = spawn_task(boom)
let r2: any = task_await(t2)
let e: any = r2["error"]
e["kind"] = "hacked"
let re: any = r2["error"]
test_report(to_str(rv[0]) + "|" + to_str(v[0]) + "|" + re["kind"] + "|" + e["kind"])
`)
	if text, _ := reported.Obj.(string); text != "1|99|runtime|hacked" {
		t.Fatalf("envelope de task_await deve ficar intacto (CoW na copia): %q", text)
	}
}
```

Criar `internal/plugin/plugin_test.go`:

```go
package plugin

import (
	"testing"

	"noxy-vm/internal/value"
)

// InterfaceToValue constroi via value.NewArray/NewMapWithData: o contêiner e
// dono duravel de cada filho composto sem que o plugin precise de *VM.
func TestInterfaceToValueContainersOwnNestedChildren(t *testing.T) {
	converted := InterfaceToValue([]interface{}{
		map[string]interface{}{"itens": []interface{}{1.0, 2.0}},
		"texto",
	})
	outer, ok := converted.Obj.(*value.ObjArray)
	if !ok || len(outer.Elements) != 2 {
		t.Fatalf("esperado array de 2 elementos, veio %#v", converted)
	}
	nested := outer.Elements[0]
	if got := value.OwnersCount(nested); got != 1 {
		t.Fatalf("map aninhado deve ter o array como unico dono: Owners=%d", got)
	}
	items, found := nested.Obj.(*value.ObjMap).Get("itens")
	if !found {
		t.Fatal("map aninhado deve conter a chave itens")
	}
	if got := value.OwnersCount(items); got != 1 {
		t.Fatalf("array aninhado deve ter o map como unico dono: Owners=%d", got)
	}
	if got := value.OwnersCount(outer.Elements[1]); got != -1 {
		t.Fatalf("string nao tem contador: Owners=%d", got)
	}
	if got := value.OwnersCount(converted); got != 0 {
		t.Fatalf("o contêiner raiz nasce sem dono: Owners=%d", got)
	}
}
```

- [ ] **Step 3: Rodar e confirmar as falhas**

Run: `go test ./internal/value -run 'TestNewArrayRetains|TestNewMapWithData'`
Expected: FAIL (`Owners=0, esperado 1`).

Run: `go test ./internal/vm -run 'TestArrayLiteralElement|TestCopyValueClone|TestCallResultOkValue|TestSliceCopy|TestTaskAwaitEnvelope'`
Expected: `TestSliceCopy` FAIL (`"9|9"`), `TestTaskAwaitEnvelope` FAIL (`"99|99|hacked|hacked"`), `TestCopyValueClone` FAIL na pré-condição (`Owners=0`); `TestArrayLiteralElement` e `TestCallResultOkValue` PASS (são guardas contra double-retain — passam antes e depois).

Run: `go test ./internal/plugin`
Expected: FAIL (`Owners=0`).

- [ ] **Step 4: Implementar o retain nos construtores**

Em `internal/value/value.go`:

```go
// NewArray cria um array que e DONO DURAVEL de cada elemento composto
// (Retain; no-op em escalares e strings) — a mesma regra de OP_ARRAY no
// executor. Quem ja reteve os elementos em nome do array usa NewArrayAdopting.
func NewArray(elements []Value) Value {
	for _, element := range elements {
		Retain(element)
	}
	return Value{Type: VAL_OBJ, Obj: &ObjArray{Elements: elements}}
}
```

```go
// NewMapWithData cria um map que e DONO DURAVEL de cada valor composto
// (Retain; no-op em escalares e strings) — a mesma regra de OP_MAP.
func NewMapWithData(data map[string]Value) Value {
	values := make(map[interface{}]Value, len(data))
	for k, v := range data {
		Retain(v)
		values[k] = v
	}
	mapping := NewMap()
	mapping.Obj.(*ObjMap).Replace(values)
	return mapping
}
```

Apagar, no comentário de `NewArray` da Task 1, a frase "A partir da Task 2 deste plano".

- [ ] **Step 5: Remover o retain manual de `invokeBoundaryCall` e atualizar comentários**

`internal/vm/builtins_call_result.go`, l.139-162, passa a:

```go
	if vm.frameCount > ownerFrameCount {
		if runErr := vm.run(ownerFrameCount+1, &result); runErr != nil {
			return value.NewNull(), runErr
		}
		// RC: a posse de `result` pelo envelope ok e registrada pelo
		// construtor (value.NewMapWithData retem em callResultOkEnvelope);
		// reter aqui tambem deixaria r.value com 2 donos e IsShared para
		// sempre. Ver TestCallResultOkValueHasExactlyOneOwner.
		return result, nil
	}
	// native/construtor: sem frame novo; resultado no topo da pilha.
	result = vm.peek(0)
	return result, nil
}
```

E o comentário de `callResultOkEnvelope` (l.241-244) vira:

```go
// callResultOkEnvelope: NewMapWithData retem `result` (unico campo
// composto) — e isso, e so isso, que da ao envelope a posse de r.value.
// Sem esse dono, uma composta devolvida fresca (Owners=0) chegaria a
// Owners=1 no primeiro `let` do lado Noxy, IsShared ficaria falso e a
// mutacao nesse binding vazaria para r.value (TestCallResultValueSemantics
// lia "100|100|3" em vez de "1|100|3").
func callResultOkEnvelope(result value.Value) value.Value {
```

- [ ] **Step 6: Ajustar `internal/vm/cow_test.go:20-28`**

O teste retinha `inner` à mão porque `NewArray` não retinha. Substituir:

```go
	inner := value.NewArray([]value.Value{value.NewInt(1)})
	outer := value.NewArray([]value.Value{inner})
	// inner é elemento durável de outer — o OP_ARRAY real teria retido
	// (Task 5); aqui o array foi montado fora do bytecode, então contamos à
	// mão para que o clone raso de outer o leve de 1 para 2 donos.
	value.Retain(inner)
```

por:

```go
	inner := value.NewArray([]value.Value{value.NewInt(1)})
	outer := value.NewArray([]value.Value{inner}) // NewArray retém: inner tem outer como dono (1)
```

(o resto do teste continua válido: o clone raso leva `inner` a 2 donos → `IsShared`).

- [ ] **Step 7: Rodar os testes-alvo e as suítes**

Run: `go test ./internal/value ./internal/plugin`
Expected: `ok` nos dois.

Run: `go test ./internal/vm`
Expected: `ok` — em particular `TestCallResultValueSemantics` (`"1|100|3"`), `TestCallResultCauseAlias*`, `TestCallResultFailureAlias*`, `TestShareByOwnersAndUnicize`, toda a `rc_uniqueness_test.go` e `value_semantics_test.go`. Se algum teste de RC antigo medir `Owners` absoluto de um filho construído por `value.NewArray` em Go, o oráculo dele sobe em 1 — **ler o teste antes de ajustar** e registrar no commit o porquê (é a nova posse real, não acomodação).

- [ ] **Step 8: Commit**

```bash
git add internal/value/value.go internal/value/constructors_test.go internal/vm/builtins_call_result.go internal/vm/cow_test.go internal/vm/container_owners_test.go internal/plugin/plugin_test.go
git commit -m "fix(value): NewArray/NewMapWithData são donos duráveis dos filhos compostos — fecha slice, task_await e plugins; invokeBoundaryCall deixa de reter result (#55 passo 1)"
```

---

### Task 3: Remover `retainingArray`/`retainingMap`

Com `NewArray`/`NewMapWithData` retendo, os helpers de `builtins_call_result.go:215-239` viram double-retain (Task 2 os deixou errados por um commit? **Não**: `retainingArray` = Retain + NewArray(que agora retém) = 2 donos — por isso esta task vem imediatamente a seguir; se quiser zero janela, fundir com a Task 2 no mesmo commit).

> Nota ao executor: para evitar a janela de double-retain entre os commits 2 e 3, **execute a Task 3 antes de rodar a suíte da Task 2 Step 7** e faça um commit único com as duas, OU aceite a janela (os testes `TestCallResultFailureAlias*` continuam verdes com 2 donos — só ficariam "eternamente shared"). Recomendado: commit único (Tasks 2+3), mensagem da Task 2 acrescida de "; retainingArray/Map removidos".

**Files:**
- Modify: `internal/vm/builtins_call_result.go:215-239,253-258,266-273,302-307`
- Modify: `internal/vm/builtins_json.go:186,192`
- Modify: `internal/vm/json_population.go:334,480`

- [ ] **Step 1: Apagar os helpers e o comentário-bloco (l.215-239) de `builtins_call_result.go`** e trocar os usos:

```go
func callResultFailureEnvelope(err error) value.Value {
	return value.NewMapWithData(map[string]value.Value{
		"ok":      value.NewBool(false),
		"value":   value.NewNull(),
		"failure": failureMap(err),
	})
}
```

```go
	if panicErr, ok := err.(*boundaryPanicError); ok {
		return value.NewMapWithData(map[string]value.Value{
			"kind":    value.NewString("panic"),
			"message": value.NewString(panicErr.payload),
			"stack":   value.NewString(panicErr.stack),
			"causes":  value.NewArray([]value.Value{}),
		})
	}
```

```go
	return value.NewMapWithData(map[string]value.Value{
		"kind":    value.NewString("runtime"),
		"message": value.NewString(message),
		"stack":   value.NewString(deepestRuntimeStack(primary)),
		"causes":  value.NewArray(causes),
	})
```

No lugar do comentário-bloco apagado, deixar uma nota curta acima de `callResultFailureEnvelope`:

```go
// A arvore de Failure e construida com value.NewMapWithData/NewArray, que
// retem cada filho composto (o pai e dono duravel — mesma regra de
// OP_MAP/OP_ARRAY). Sem esse dono o primeiro `let f: any = r.failure` do
// lado Noxy levaria o filho a Owners=1, IsShared falso, e a mutacao
// reescreveria o envelope (TestCallResultFailureAliasDoesNotMutateEnvelope,
// TestCallResultCauseAliasDoesNotMutateEnvelope).
```

- [ ] **Step 2: `builtins_json.go:186,192`**

```go
		return value.NewArray(arr) // RC: o array e dono duravel de cada elemento (construtor retem)
```
```go
		return value.NewMapWithData(m) // RC: o map e dono duravel de cada valor (construtor retem)
```

- [ ] **Step 3: `json_population.go:334,480`**

```go
		array := value.NewArray(elements) // RC: o array e dono duravel de cada elemento (construtor retem; espelha OP_ARRAY)
```
```go
		return value.NewArray(elements), true // RC: o array e dono duravel de cada elemento (construtor retem)
```

- [ ] **Step 4: Verificar que não sobrou referência**

Run: `grep -rn "retainingArray\|retainingMap" internal`
Expected: saída vazia.

- [ ] **Step 5: Suíte**

Run: `go test ./internal/vm`
Expected: `ok` (testes de RC de `json_loads`/`json_parse` da #50 e de `call_result` verdes).

- [ ] **Step 6: Commit** (ou fundido com a Task 2)

```bash
git add internal/vm/builtins_call_result.go internal/vm/builtins_json.go internal/vm/json_population.go
git commit -m "refactor(vm): retainingArray/retainingMap removidos — o construtor já retém (#55 passo 3)"
```

---

### Task 4: `NewInstanceWith` nos envelopes `sqlite.query`, `io.read_lines`, `strings.split`

Estes builders escrevem arrays em campos de instância crua (`inst.Fields["x"] = value.NewArray(...)`) sem retain — a instância não é dona do array.

**Files:**
- Modify: `internal/vm/builtins_sqlite.go:340-351,437-445`
- Modify: `internal/vm/builtins_io.go:384-390`
- Modify: `internal/vm/builtins_strings.go:229-238`
- Modify: `internal/vm/container_owners_test.go`

- [ ] **Step 1: Testes (falham)**

Acrescentar a `internal/vm/container_owners_test.go` (imports extras: `"os"`, `"path/filepath"`, `"strconv"`):

```go
// sqlite.query (builtins_sqlite.go): QueryResult e dono de columns e rows;
// cada Row e dona de values.
func TestSQLiteQueryEnvelopeOwnsColumnsAndRowValues(t *testing.T) {
	reported := captureVMSource(t, `
use sqlite
let db: sqlite.Database = sqlite.open(":memory:")
sqlite.exec(db, "CREATE TABLE t (id INTEGER, nome TEXT)")
sqlite.exec(db, "INSERT INTO t VALUES (1, 'a')")
let res: sqlite.QueryResult = sqlite.query(db, "SELECT * FROM t")
let cols: string[] = res.columns
cols[0] = "ZZZ"
let vals: any[] = res.rows[0].values
vals[0] = 999
sqlite.close(db)
test_report(res.columns[0] + "|" + cols[0] + "|" + to_str(res.rows[0].values[0]) + "|" + to_str(vals[0]))
`)
	if text, _ := reported.Obj.(string); text != "id|ZZZ|1|999" {
		t.Fatalf("envelope de sqlite.query deve ficar intacto (CoW nas copias): %q", text)
	}
}

// io.read_lines (builtins_io.go): IOLinesResult e dono de data.
func TestIOReadLinesEnvelopeOwnsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "linhas.txt")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reported := captureVMSource(t, "use io\nlet f: io.File = io.open("+strconv.Quote(path)+", \"r\")\n"+
		"let r: io.IOLinesResult = io.read_lines(f)\nio.close(f)\n"+
		"let d: string[] = r.data\nd[0] = \"ZZZ\"\n"+
		"test_report(r.data[0] + \"|\" + d[0])\n")
	if text, _ := reported.Obj.(string); text != "a|ZZZ" {
		t.Fatalf("envelope de io.read_lines deve ficar intacto (CoW na copia): %q", text)
	}
}

// strings.split (builtins_strings.go): SplitResult e dono de parts.
func TestStringsSplitEnvelopeOwnsParts(t *testing.T) {
	reported := captureVMSource(t, `
use strings
let r: strings.SplitResult = strings.split("a,b", ",")
let p: string[] = r.parts
p[0] = "ZZZ"
test_report(r.parts[0] + "|" + p[0])
`)
	if text, _ := reported.Obj.(string); text != "a|ZZZ" {
		t.Fatalf("envelope de strings.split deve ficar intacto (CoW na copia): %q", text)
	}
}
```

- [ ] **Step 2: Rodar e confirmar as falhas**

Run: `go test ./internal/vm -run 'TestSQLiteQueryEnvelope|TestIOReadLinesEnvelope|TestStringsSplitEnvelope'`
Expected: 3× FAIL com a cópia vazando (`"ZZZ|ZZZ|999|999"`, `"ZZZ|ZZZ"`, `"ZZZ|ZZZ"`). Se a forma da API estiver errada (ex.: modo de `io.open`, nome de tipo), o erro será de compilação/runtime do programa — ajustar o programa, não a asserção.

- [ ] **Step 3: Implementar**

`internal/vm/builtins_sqlite.go` (~l.340-351):

```go
			// RC: NewInstanceWith retem values — a Row e dona duravel do array.
			row := value.NewInstanceWith(rowTemplate.Struct, map[string]value.Value{
				"values": value.NewArray(values),
			})
			rowValues = append(rowValues, row)
		}

		// RC: NewInstanceWith retem columns e rows (compostos); os escalares sao no-op.
		return value.NewInstanceWith(resultTemplate.Struct, map[string]value.Value{
			"columns":   value.NewArray(columnValues),
			"rows":      value.NewArray(rowValues),
			"row_count": value.NewInt(int64(len(rowValues))),
			"ok":        value.NewBool(true),
			"error":     value.NewString(""),
		}), nil
```

`sqliteQueryError` (~l.437-445):

```go
func sqliteQueryError(definition *value.ObjStruct, errorText string) value.Value {
	// RC: NewInstanceWith retem os arrays vazios (campos compostos).
	return value.NewInstanceWith(definition, map[string]value.Value{
		"columns":   value.NewArray(nil),
		"rows":      value.NewArray(nil),
		"row_count": value.NewInt(0),
		"ok":        value.NewBool(false),
		"error":     value.NewString(errorText),
	})
}
```

`internal/vm/builtins_io.go:384-390`:

```go
func newIOReadResult(definition *value.ObjStruct, ok bool, data value.Value, errorText string) value.Value {
	// RC: NewInstanceWith retem data quando composto (array de read_lines);
	// string/bytes sao no-op.
	return value.NewInstanceWith(definition, map[string]value.Value{
		"ok":    value.NewBool(ok),
		"data":  data,
		"error": value.NewString(errorText),
	})
}
```

`internal/vm/builtins_strings.go:229-238`:

```go
		partValues := make([]value.Value, len(parts))
		for i, p := range parts {
			partValues[i] = value.NewString(p)
		}
		// RC: NewInstanceWith retem parts — SplitResult e dono duravel do array.
		return value.NewInstanceWith(structDef, map[string]value.Value{
			"count": value.NewInt(int64(len(parts))),
			"parts": value.NewArray(partValues),
		}), nil
```

(`sqliteExecError`, `sqlite` open/exec, `io` open/close/write, `json_dumps_result`, `sys`, `time` têm campos só escalares — **não migrar**.)

- [ ] **Step 4: Rodar**

Run: `go test ./internal/vm -run 'TestSQLiteQueryEnvelope|TestIOReadLinesEnvelope|TestStringsSplitEnvelope|SQLite|IO|Strings'`
Expected: PASS.

Run: `go test ./internal/vm`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/vm/builtins_sqlite.go internal/vm/builtins_io.go internal/vm/builtins_strings.go internal/vm/container_owners_test.go
git commit -m "fix(vm): envelopes de sqlite.query, io.read_lines e strings.split são donos dos arrays (NewInstanceWith) (#55 passo 4)"
```

---

### Task 5: Não-regressão de clones (`CloneCountValue`)

Critério de aceite: mutar um contêiner com dono único vindo desses natives não clona; a única clonagem nova é a CoW correta quando o filho é de fato compartilhado (ex.: elemento de `slice` ainda presente no original).

**Files:**
- Modify: `internal/vm/container_owners_test.go`

**Interfaces:**
- Consumes: `vmWithCloneReset()` (`value_semantics_test.go:244`, native `test_reset_clones`), `CloneCountValue()`, `ResetCloneCount()`.

- [ ] **Step 1: Teste**

```go
// Nao introduzimos clone desnecessario: r.parts tem dono unico (a instancia)
// e muta in-place; s[0] e compartilhado com t (slice retem) e clona UMA vez
// — a segunda escrita ja e in-place. Oraculo: "0|1|1|0".
func TestNativeContainersDoNotCloneWithSingleOwner(t *testing.T) {
	machine := vmWithCloneReset()
	machine.DefineNative("clones_now", func(args []value.Value) value.Value {
		return value.NewInt(CloneCountValue())
	})
	var reported value.Value
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) == 1 {
			reported = args[0]
		}
		return value.NewNull()
	})
	src := `
use strings
struct Pair
    a: int
    b: int
end
let r: strings.SplitResult = strings.split("a,b", ",")
let t: Pair[] = [Pair(0, 0), Pair(1, 1)]
let s: Pair[] = slice(t, 0, 2)
test_reset_clones()
r.parts[0] = "x"
let c1: int = clones_now()
s[0].a = 9
let c2: int = clones_now()
s[0].b = 8
let c3: int = clones_now()
test_report(to_str(c1) + "|" + to_str(c2) + "|" + to_str(c3) + "|" + to_str(t[0].a))
`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	if text, _ := reported.Obj.(string); text != "0|1|1|0" {
		t.Fatalf("clones (parts in-place | 1 clone do Pair compartilhado | sem clone extra | original intacto): %q", text)
	}
}
```

- [ ] **Step 2: Rodar**

Run: `go test ./internal/vm -run TestNativeContainersDoNotCloneWithSingleOwner -v`
Expected: PASS com `"0|1|1|0"`. Se vier `"0|1|2|0"`, há clone a mais na segunda escrita — investigar antes de ajustar o oráculo (provável retain a mais na escrita de volta em `s[0]`). Se vier `"1|…"`, `r.parts` está IsShared indevidamente (double-retain em `NewInstanceWith`+`NewArray`? — `NewArray` retém os *elementos*, não o array; `NewInstanceWith` retém o array: isso dá 1 dono, correto).

- [ ] **Step 3: Commit**

```bash
git add internal/vm/container_owners_test.go
git commit -m "test(vm): contêineres de natives com dono único não clonam; slice clona o Pair compartilhado uma vez (#55 critério de aceite)"
```

---

### Task 6: CHANGELOG, AGENTS.md (§E) e versão `v0.10.1`

**Files:**
- Modify: `CHANGELOG.md:1-3`
- Modify: `AGENTS.md:103-123`
- Modify: `internal/version/version.go:3`

- [ ] **Step 1: CHANGELOG** — inserir acima de `## [0.10.0] - 2026-08-20`:

```markdown
## [0.10.1] - 2026-08-20

### Fixed — contêineres criados por natives/plugins são donos dos filhos (issue #55)

- **`slice`, `sqlite.query` (`columns`, `rows[i].values`), `task_await` (`value`,
  `error`), `io.read_lines` (`data`), `strings.split` (`parts`) e plugins
  (`InterfaceToValue`) devolviam contêineres que não eram donos dos filhos
  compostos**: a cópia por valor (`let s: Pair[] = slice(t, 0, 2); s[0].a = 9`)
  mutava o original (`t[0].a` lia 9). Regra do runtime — *todo contêiner é dono
  durável de cada filho composto* — já valia no bytecode (`OP_ARRAY`/`OP_MAP`/
  construtor de struct) e passa a valer nos natives: `value.NewArray`/
  `NewMapWithData` retêm cada filho composto, `NewInstanceWith` constrói
  instâncias retendo os campos. Os sites que já entregavam filhos retidos
  (`OP_ARRAY`, clone CoW, merge de `causes` do `call_result`) usam
  `NewArrayAdopting`; o retain manual do envelope `ok` do `call_result` saiu
  (o construtor registra a posse). Efeito visível: código que dependia do
  aliasing acidental passa a ver a cópia independente (e um clone CoW na
  primeira escrita ao filho ainda compartilhado). Sem mudança de API Noxy.
- Fase A da #54; itens 1b/1c/1d da #53.
```

- [ ] **Step 2: AGENTS.md §E "Adicionar Função Builtin"** — acrescentar após o item 4 da lista (antes do bloco de código):

```markdown
5. **Contêineres devolvidos são donos dos filhos**: construa com `value.NewArray`,
   `value.NewMapWithData` ou `value.NewInstanceWith(def, fields)` — eles retêm cada
   filho composto (array/map/instância; no-op em escalares/strings). Nunca escreva
   um composto em `inst.Fields[...]`/`ObjMap.Set` cru sem `value.Retain`. Só use
   `value.NewArrayAdopting` para elementos que **você** já reteve em nome do array,
   com comentário `// RC: move` (sites atuais: `OP_ARRAY`, `copyValue`, merge de
   `causes` do `call_result`).
```

- [ ] **Step 3: versão** — `internal/version/version.go`: `const Version = "v0.10.1"`.

- [ ] **Step 4: Conferir que nada mais cita a versão antiga como atual**

Run: `grep -rn "v0.10.0\|0\.10\.0" --include=*.go --include=*.md --include=*.nx . | grep -v CHANGELOG | grep -v docs/superpowers | head`
Expected: nada além de referências históricas (se `README.md` ou `noxy.mod` citarem a versão corrente, atualizar do mesmo jeito que o commit `c55c39d` fez — `git show c55c39d --stat` mostra os arquivos tocados no bump anterior).

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md AGENTS.md internal/version/version.go
git commit -m "chore(version): noxy v0.10.1 — contêineres de natives/plugins são donos dos filhos (slice, sqlite.query, task_await, io, strings); CHANGELOG e AGENTS §E"
```

---

### Task 7: Verificação final (critérios de aceite da issue)

- [ ] **Step 1: formatação e vet**

Run: `gofmt -l internal/value internal/vm internal/plugin`
Expected: vazio (se listar `rc_uniqueness_test.go`, é o gofmt pré-existente da #53 item 3 — não tocar; qualquer outro arquivo listado deve ser formatado).

Run: `go vet ./...`
Expected: sem saída.

- [ ] **Step 2: suíte completa sem `| tail`**

Run (background, saída para o scratchpad): `go test ./... > <scratchpad>/final_test.log 2>&1`
Expected: `ok` em todos os pacotes com testes; conferir com `grep -v "no test files" <scratchpad>/final_test.log`.

- [ ] **Step 3: runner dos exemplos**

Run: `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx > <scratchpad>/runner_after.log 2>&1`
Expected: `171/171` (ou o total atual do runner) passando; comparar com o mesmo comando rodado no checkout `develop` (`D:\OneDrive\Documentos\go_projects\noxy`, `1680266`).

- [ ] **Step 4: diff de saída dos exemplos contra o `develop`**

Escrever no scratchpad `capture_examples.py` que, para cada `noxy_examples/*.nx` fora da lista de exclusão do runner (ler `run_all_tests_concurrent.nx` para a lista: interativos/rede/longos), executa `go run ./cmd/noxy <arquivo>` com timeout de 60 s e grava stdout+stderr em `<out_dir>/<nome>.txt`; rodar uma vez com cwd no checkout `develop` (`out_dir=before`) e uma vez no worktree (`out_dir=after`), com a árvore **parada** (sem edição concorrente); depois `diff -r before after`.
Expected: só diferenças de não-determinismo (ordem de map, timestamps, rand/uuid, endereços de memória, cwd/nome do binário, rede). Qualquer diferença de valor é regressão — investigar.

- [ ] **Step 5: grep dos helpers removidos**

Run: `grep -rn "retainingArray\|retainingMap" internal`
Expected: vazio.

- [ ] **Step 6: revisão de código** (skill `superpowers:requesting-code-review`) sobre `git diff develop...HEAD`, com foco em: double-retain nos moves, retain esquecido em algum builder que escreve composto cru, comentários RC obsoletos, testes com oráculo acomodado.

---

### Task 8: Entrega

- [ ] **Step 1: Registrar execução no plano** (seção "Registro da execução" no fim deste arquivo: o que mudou em relação ao planejado, achados da revisão, tempos) e commitar: `docs(plan): registro da execução da #55`.
- [ ] **Step 2: Texto do PR** no template do usuário (Summary / Components / Test Plan com checkboxes), título `fix/container-owners - Contêineres de natives/plugins são donos dos filhos (#55)`, base `develop`, label `not available to review` (convenção em memória `noxy-pr-conventions`) — **push e abertura do PR só com confirmação do usuário** (ação externa).
- [ ] **Step 3: Comentários nas issues** #53 (itens 1b/1c/1d fechados aqui) e #54 (fase A feita) — também só após confirmação, junto com o PR.

---

## Self-review (feito ao escrever)

- **Cobertura do spec:** §1 construtores → Tasks 1-2; §2 três moves → Task 1 (+ o quarto move implícito, `invokeBoundaryCall`, → Task 2 — não listado na issue, descoberto na leitura: sem ele `r.value` ficaria com 2 donos); §3 remover paliativos → Task 3; §4 `NewInstanceWith` → Task 4; reproduções → Tasks 2 e 4; critérios de aceite: `Owners` unitários (T1/T2), sem double-retain (T2 sondas + `TestCallResultCauseAlias*`), cópia não vaza (T2/T4), `CloneCountValue` (T5), grep (T3/T7), suíte/vet/runner/diff (T7), CHANGELOG+AGENTS (T6), comentários #53/#54 (T8).
- **Fora de escopo respeitado:** nenhum opcode novo, `ObjMap.Set/Replace` intactos, laços de retain do `json_population.go` intactos, `NewMap()`/`NewInstance()` intactos, builders só-escalares não migrados.
- **Consistência de nomes:** `NewArrayAdopting(elements []Value)`, `NewInstanceWith(def *ObjStruct, fields map[string]Value)`, `vmWithOwnersProbe`, `probe_owners`, `clones_now`, `test_reset_clones` — iguais em todas as tasks.

---

## Registro da execução (2026-08-20)

**Branch:** `fix/container-owners` (worktree `.claude/worktrees/fix+container-owners`, base `1680266` = `origin/develop` = `origin/main`). **Commits:** `ecb835b` plano+spec → `6d915e8` Task 1 → `58f2cad` Tasks 2+3 (commit único, sem janela de double-retain) → `a1c5e20` Task 4 → `7269cc6` Task 5 → `7ee4d4e` Task 6 (v0.10.1) → `cc0366f` ajustes da revisão → (este) registro.

**Desvios do planejado (todos para melhor):**
- Tasks 2 e 3 foram fundidas num commit, como a nota do plano recomendava.
- Task 6 tocou também `README.md` (badge e banner do REPL), espelhando o bump anterior (`c55c39d`).
- Revisão de código (Task 7 passo 6, subagente independente, 9,5 min): **sem Critical**; Important (1) asserção de `Owners == 1` dos herdados e do irmão em `TestFailureMapMergesInnerCausesWithSiblingsOnPromotion` — única lacuna do critério "Owners dos herdados inalterado" (feito em `cc0366f`); Important (2) evidência do runner/diff (abaixo); Minor: comentário obsoleto em `builtins_call_result_test.go:461-469` (reescrito), bloco de comentário órfão em `builtins_call_result.go` (virou doc de `callResultFailureEnvelope`), nota na spec da #50 §5.3 sobre `retainingArray/Map` (adicionada). O revisor varreu os ~40 call sites dos construtores e todos os `value.Retain(` não-teste e confirmou: nenhum double-retain, nenhuma escrita crua de composto remanescente, `invokeBoundaryCall` exatamente compensado, release symmetry preservada (`pop`/`delete`/`OP_SET_*`).

**TDD — falhas observadas antes de cada mudança (valores previstos no plano):** Task 1 `undefined: NewArrayAdopting/NewInstanceWith`; Task 2 `Owners=0, esperado 1` (value e plugin), `copyValue` pré-condição `Owners=0`, slice `"9|9"`, task_await `"99|99|hacked|hacked"`; Task 4 sqlite `"ZZZ|ZZZ|999|999"`, io/strings `"ZZZ|ZZZ"`; Task 5 passou de primeira com `"0|1|1|0"`. Guardas `TestArrayLiteralElementHasExactlyOneOwner` e `TestCallResultOkValueHasExactlyOneOwner` passam antes e depois (protegem contra double-retain futuro).

**Verificação (Task 7):**
- `go vet ./...` limpo; gofmt dos arquivos tocados limpo (checado com CRLF normalizado — `gofmt -l` lista todo o checkout Windows, é ruído).
- `go test ./...` completo, sem `| tail`: 9 pacotes `ok` (`internal/vm` 56 s).
- `grep -rn "retainingArray\|retainingMap" internal` vazio.
- Runner `noxy_examples/run_all_tests_concurrent.nx` no worktree: **171/171** (17 s).
- Diff de saída dos exemplos (script `capture_examples.py`: `go run ./cmd/noxy` por exemplo, exclusões do runner, 171 exemplos em cada árvore, 0 com exit ≠ 0 nas duas): **24 arquivos diferem, todos por não-determinismo ou ambiente** — caminho do binário `go run`/cwd (`cli_example`, `test_basic_import`, `test_sys`), tempos (`fibonacci`, `quicksort_in_place`, `stress_test_json`, `time_demo`), ordem de map (`json_map_test`, `json_test`, `multiline_test`, `test_for_loop`, `json_analogy_test`), rand/uuid/salt (`password_generator`, `rand_demo`, `uuid_demo`, `test_crypto_aes`, `test_crypto_debug` — este já imprime FAIL com exit 0 nas duas árvores, #53 item 4), endereços (`test_addr_struct`, `test_addr_trust`), porta efêmera (`test_http_server`). Dois exigiram verificação: `read_passwords.nx` ("Found 2 passwords" → "Error executing query") é porque `passwords.db` (gitignored) só existe no checkout principal — copiado para o worktree, a saída ficou **idêntica** (o exemplo lê `res.rows[i].values[j]` do envelope novo); `dynamodb_*`/`test_dynamodb_plugin` ("Plugin Load Error: command not found: noxy-plugin-dynamodb") é o binário do plugin presente só no ambiente do checkout principal — e no baseline esses exemplos já terminam em erro 400 da AWS. Nenhuma diferença de valor atribuível à mudança.

**Pendências (Task 8, ações externas — aguardam confirmação do usuário):** push da branch, PR (`fix/container-owners - …`, base `develop`, label `not available to review`, corpo no template Summary/Components/Test Plan), comentários em #53 (itens 1b/1c/1d fechados) e #54 (fase A feita).
