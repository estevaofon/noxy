# VM Perf Fase 2 — Layout do `Value` e header comum — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Encolher `value.Value` de 48 para 32 bytes, dar aos compostos um header comum com `Owners` no offset 0 e tirar o type switch do caminho comum de `ownersOf`, medindo cada estágio isoladamente (issue #37, estágios 1 e 2 + extra do `pop`).

**Architecture:** Nenhuma mudança de semântica, sintaxe, bytecode ou saída. (1) `Value{Type uint8, kind uint8, b bool, num uint64, Obj interface{}}` com acessores `Int()/Float()/Bool()` e `SetInt()`; rename mecânico por script. (2) `ObjHeader{Owners}` embutido primeiro em `ObjArray/ObjMap/ObjInstance`; `kind` carimbado pelos construtores de `internal/value`; `ownersOf` decide pelo byte e cai no type switch de hoje quando `kind == 0`. (3) `pop()` limpa só `Obj` para caber no orçamento de inline de `run()`.

**Tech Stack:** Go 1.24, Python 3 (script de rename, CRLF preservado), PowerShell (benchmarks intercalados de `benchmarks/`), `go build -gcflags=-m=2` (guards de inline), `noxy --cpuprofile`.

**Spec:** `docs/superpowers/specs/2026-08-22-vm-perf-fase2-value-layout-design.md`

## Global Constraints

- Branch `perf/issue-37-value-layout` (worktree `.claude/worktrees/perf-issue-37`), base `develop` cb8efcb (v0.14.2). Commits por estágio, nunca squash dos estágios — cada um tem binário e medição próprios.
- **Semântica idêntica**: mesmos outputs, mesmos erros. Corpus `noxy_examples/` sem falhas e diff de saída base × head vazio (`benchmarks/compare_examples.ps1`).
- **RC intocado**: nenhum funil retain/release muda de lugar ou de contagem; `Owners` continua `atomic.Int32`.
- **Tags `ValueType` e opcodes não mudam de valor nem de ordem.**
- Guards de inline (`internal/vm/inline_guard_test.go`): `push` ≤ 20 e ≥ 100 sites em `executor.go`; `pop` idem (Task 3); `Retain`/`Release` ≤ 80 (Task 2). Conferir com `go build -gcflags=-m=2 ./internal/vm 2>&1 | grep -E "can inline \(\*VM\)\.(push|pop)|can inline .*(Retain|Release|ownersOf)"`.
- `go test ./...` verde; `go test -race ./internal/value ./internal/vm` verde; `go vet ./internal/value ./internal/vm` limpo.
- Arquivos Go do repo são **CRLF**: scripts de edição leem/gravam com `newline=''` e preservam `\r\n`; conferir diffs com `git diff --numstat` (nunca diff de arquivo inteiro por EOL).
- Binários de benchmark ficam em disco local: `$SCRATCH\bench\` onde `$SCRATCH = C:\Users\estev\AppData\Local\Temp\claude\D--OneDrive-Documentos-go-projects-noxy\ef3672bf-bc7a-4367-818c-fb10c1f93a42\scratchpad` (`noxy_base.exe` já está lá, buildado de cb8efcb). Se o EDR apagar um .exe, rebuildar; para rodar Noxy ad hoc, `go run ./cmd/noxy arquivo.nx`.
- Todos os comandos assumem cwd = raiz do worktree, Git Bash ou PowerShell no Windows.

---

### Task 1: Estágio 1 — `Value` de 48 → 32 bytes, acessores e rename mecânico

**Files:**
- Modify: `internal/value/value.go:11-33` (tipo `ValueType`, struct `Value`), `:648-658` (construtores)
- Create: `internal/value/layout_test.go`
- Create (scratch, não commitado): `$SCRATCH\rename_accessors.py`
- Modify (pelo script): todo `*.go` fora de `.claude/` que leia `.AsInt/.AsFloat/.AsBool` (323/55/49 sites) — e à mão `internal/vm/executor.go:221` (`AsInt +=`)

**Interfaces:**
- Consumes: nada.
- Produces: `type ValueType uint8`; `func (v Value) Int() int64`; `func (v Value) Float() float64`; `func (v Value) Bool() bool`; `func (v *Value) SetInt(n int64)`; campos `kind objKind` (tipo `objKind uint8`, usado na Task 2), `b bool`, `num uint64` não exportados. `NewInt/NewFloat/NewBool/NewNull` mantêm assinatura.

- [ ] **Step 1: Teste de layout e acessores (falha: Sizeof é 48 e os métodos não existem)**

Criar `internal/value/layout_test.go`:

```go
package value

import (
	"math"
	"testing"
	"unsafe"
)

// O tamanho do Value e o custo de cada push/pop e de cada copia de operando
// em run(): 48 B (tag int + bool + int64 + float64 + interface) virou 32 B na
// fase 2 de perf (issue #37, estagio 1). Este teste e a assercao executavel
// do layout — se alguem acrescentar um campo, o build avisa aqui, nao num
// benchmark meses depois.
func TestValueIs32Bytes(t *testing.T) {
	if got := unsafe.Sizeof(Value{}); got != 32 {
		t.Fatalf("unsafe.Sizeof(Value{}) = %d, esperado 32 (ver spec 2026-08-22-vm-perf-fase2-value-layout)", got)
	}
	if got := unsafe.Sizeof(ValueType(0)); got != 1 {
		t.Fatalf("ValueType deve ocupar 1 byte, ocupa %d", got)
	}
}

func TestValueAccessorsRoundTrip(t *testing.T) {
	if got := NewInt(-42).Int(); got != -42 {
		t.Fatalf("Int(): %d", got)
	}
	if got := NewInt(math.MaxInt64).Int(); got != math.MaxInt64 {
		t.Fatalf("Int() MaxInt64: %d", got)
	}
	if got := NewFloat(3.5).Float(); got != 3.5 {
		t.Fatalf("Float(): %v", got)
	}
	if got := NewFloat(math.Inf(-1)).Float(); !math.IsInf(got, -1) {
		t.Fatalf("Float() -Inf: %v", got)
	}
	if got := NewFloat(math.NaN()).Float(); !math.IsNaN(got) {
		t.Fatalf("Float() NaN: %v", got)
	}
	if got := NewFloat(math.Copysign(0, -1)).Float(); !math.Signbit(got) {
		t.Fatalf("Float() -0: perdeu o sinal")
	}
	if !NewBool(true).Bool() || NewBool(false).Bool() {
		t.Fatal("Bool() nao faz round-trip")
	}
	var zero Value
	if zero.Type != VAL_BOOL || zero.Bool() || zero.Int() != 0 || zero.Obj != nil {
		t.Fatalf("zero value deve continuar sendo VAL_BOOL false: %+v", zero)
	}
	v := NewInt(1)
	v.SetInt(41)
	v.SetInt(v.Int() + 1)
	if v.Type != VAL_INT || v.Int() != 42 {
		t.Fatalf("SetInt: %+v", v)
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/value -run 'TestValueIs32Bytes|TestValueAccessorsRoundTrip'`
Expected: erro de compilação (`NewInt(-42).Int undefined`) — o teste ainda não compila; é a falha esperada.

- [ ] **Step 3: Novo struct, acessores e construtores em `value.go`**

Substituir as linhas 11 e 26-33 (`type ValueType int` e o struct) por:

```go
type ValueType uint8

// objKind e a dica que os construtores de internal/value carimbam em Value
// para ownersOf (cow.go) decidir sem type switch. Zero = "desconhecido":
// um Value{Type: VAL_OBJ, Obj: x} montado fora dos construtores cai no
// caminho lento (o type switch de sempre) e continua correto — a dica so
// acelera, nunca decide sozinha.
type objKind uint8

const (
	objKindUnknown  objKind = iota
	objKindNoOwners         // string, *ObjStruct, *RuntimeTypeInfo: VAL_OBJ sem contador
	objKindArray
	objKindMap
	objKindInstance
)

// Value e o operando universal da VM: 32 bytes (fase 2 de perf, issue #37).
// Type e a tag; num guarda int64 (VAL_INT) ou os bits de um float64
// (VAL_FLOAT); b guarda VAL_BOOL; Obj, o objeto alocado dos demais tipos.
// Leia pelos acessores Int()/Float()/Bool() — ler o campo errado para a tag
// devolve lixo (num e compartilhado entre int e float), nunca zero.
// layout_test.go trava o tamanho.
type Value struct {
	Type ValueType
	kind objKind
	b    bool
	num  uint64
	Obj  interface{} // Heap allocated object
}

// Int devolve o inteiro de um VAL_INT. Em qualquer outra tag o resultado e
// indefinido (os bits de num) — o chamador garante a tag.
func (v Value) Int() int64 { return int64(v.num) }

// Float devolve o float de um VAL_FLOAT (bits em num).
func (v Value) Float() float64 { return math.Float64frombits(v.num) }

// Bool devolve o valor de um VAL_BOOL.
func (v Value) Bool() bool { return v.b }

// SetInt grava o inteiro no lugar, sem tocar na tag — e o `AsInt +=` de
// OP_INC_LOCAL_INT (8 bytes escritos em vez dos 32 de um Value novo).
func (v *Value) SetInt(n int64) { v.num = uint64(n) }
```

Acrescentar `"math"` aos imports. Substituir os construtores (linhas 648-658):

```go
func NewInt(v int64) Value {
	return Value{Type: VAL_INT, num: uint64(v)}
}

func NewFloat(v float64) Value {
	return Value{Type: VAL_FLOAT, num: math.Float64bits(v)}
}

func NewBool(v bool) Value {
	return Value{Type: VAL_BOOL, b: v}
}
```

Atualizar também `String()` (`value.go:591` usa `v.AsBool`, `:597` usa `v.AsFloat`) — o script do Step 4 cobre esses dois.

- [ ] **Step 4: Script de rename (Write tool, não heredoc) e execução**

`$SCRATCH\rename_accessors.py`:

```python
import pathlib, re, sys
ROOT = pathlib.Path(sys.argv[1])
PAT = {
    re.compile(r'\.AsInt\b'):   '.Int()',
    re.compile(r'\.AsFloat\b'): '.Float()',
    re.compile(r'\.AsBool\b'):  '.Bool()',
}
changed = 0
for p in ROOT.rglob('*.go'):
    if '.claude' in p.parts or '.git' in p.parts:
        continue
    raw = p.read_bytes()
    text = raw.decode('utf-8')
    new = text
    for rx, rep in PAT.items():
        new = rx.sub(rep, new)
    if new != text:
        p.write_bytes(new.encode('utf-8'))   # bytes: CRLF preservado
        changed += 1
print('arquivos alterados:', changed)
```

Run: `python "$SCRATCH/rename_accessors.py" .` → esperado ~80 arquivos. Depois, à mão:

- `internal/vm/executor.go:221`: `vm.stack[frame.LocalBase+int(slot)].Int() += int64(delta)` (inválido) → 
  ```go
  slotValue := &vm.stack[frame.LocalBase+int(slot)]
  slotValue.SetInt(slotValue.Int() + int64(delta))
  ```
- `grep -rn "Int() +=\|Float() +=\|Bool() =" --include=*.go .` tem de voltar vazio.

- [ ] **Step 5: Compilar, auditar leituras sem guarda e rodar o pacote**

Run: `go build ./... && go vet ./internal/value ./internal/vm`
Expected: limpo.

Auditoria (a única mudança de semântica dos acessores é em tag errada — antes zero, agora lixo): listar leituras sem `VAL_`/`Type`/`switch`/`checkType`/`case` nas 4 linhas anteriores e revisar cada uma à mão:

```bash
grep -rn -B4 '\.\(Int\|Float\|Bool\)()' --include=*.go --exclude-dir=.claude internal cmd \
  | awk -F'[-:]' '/\.(Int|Float|Bool)\(\)/{print}' > /dev/null
```

(Na prática: `grep -rn -B4 ... | grep -v -- '--' ` e ler os blocos cujo contexto não menciona `VAL_`, `Type`, `switch`, `case`, `checkType`, `IsInt`, `isInt`, `expect`, `want`, `got`. Registrar no commit os sites revisados que dependiam de zero implícito, se houver — a expectativa, pela exploração, é nenhum.)

Run: `go test ./internal/value ./internal/vm`
Expected: PASS (inclui `TestPushStaysInlinedInsideRun` — `NewBool`/`NewInt` seguem custo ≤ 20).

- [ ] **Step 6: Commit e binário do estágio**

```bash
git add -A internal cmd
git commit -m "perf(value): Value de 48 para 32 bytes — tag uint8, num único p/ int|float, bool no padding; acessores Int()/Float()/Bool()/SetInt (issue #37, estágio 1)"
go build -o "$SCRATCH/bench/noxy_s1.exe" ./cmd/noxy
```

---

### Task 2: Estágio 2 — `ObjHeader`, dica `kind` e `ownersOf` sem type switch

**Files:**
- Modify: `internal/value/value.go` (`ObjArray:348`, `ObjMap:388`, `ObjInstance:472`, construtores `:664-733`, `environment.go:72`)
- Modify: `internal/value/cow.go:21-34`
- Modify: `internal/value/owners_test.go`, `internal/value/layout_test.go`
- Modify: `internal/vm/calls.go:198`
- Modify: `internal/vm/inline_guard_test.go`

**Interfaces:**
- Consumes: `objKind` e campo `kind` da Task 1.
- Produces: `type ObjHeader struct{ Owners atomic.Int32 }` embutido (campo promovido `obj.Owners` continua válido); `func NewInstanceAdopting(def *ObjStruct, fields map[string]Value) Value` (instância cujos campos o chamador JÁ reteve — o análogo de `NewArrayAdopting`); `ownersOf` com caminho rápido.

- [ ] **Step 1: Testes (falham: `ObjHeader` não existe; `kind` não é carimbado)**

Acrescentar a `internal/value/layout_test.go`:

```go
// O header comum tem de ser o PRIMEIRO campo dos tres compostos: e o que
// permite a um eventual estagio 3 (unsafe.Pointer) alcancar Owners sem saber
// o tipo concreto. Hoje ownersOf usa type assertion checada, mas o layout ja
// fica travado — invariante implicito em codigo de aparencia inocente e o
// modo de falha mais provavel.
func TestObjHeaderIsAtOffsetZero(t *testing.T) {
	if off := unsafe.Offsetof(ObjArray{}.ObjHeader); off != 0 {
		t.Fatalf("ObjArray.ObjHeader no offset %d, esperado 0", off)
	}
	if off := unsafe.Offsetof(ObjMap{}.ObjHeader); off != 0 {
		t.Fatalf("ObjMap.ObjHeader no offset %d, esperado 0", off)
	}
	if off := unsafe.Offsetof(ObjInstance{}.ObjHeader); off != 0 {
		t.Fatalf("ObjInstance.ObjHeader no offset %d, esperado 0", off)
	}
}
```

Acrescentar a `internal/value/owners_test.go`:

```go
// Cada construtor carimba a dica kind; ownersOf tem de chegar ao MESMO
// contador pelo caminho rapido (dica) e pelo lento (Value montado a mao,
// kind zero) — se divergissem, Retain por um caminho e Release pelo outro
// contariam em objetos diferentes.
func TestOwnersOfFastAndSlowPathsAgree(t *testing.T) {
	arr := NewArray(nil)
	arrObj := arr.Obj.(*ObjArray)
	if arr.kind != objKindArray {
		t.Fatalf("NewArray deve carimbar objKindArray, veio %d", arr.kind)
	}
	if ownersOf(arr) != &arrObj.Owners {
		t.Fatal("ownersOf(array) nao aponta para o Owners do header")
	}
	if ownersOf(Value{Type: VAL_OBJ, Obj: arrObj}) != &arrObj.Owners {
		t.Fatal("caminho lento (kind zero) diverge do rapido para array")
	}

	m := NewMap()
	mObj := m.Obj.(*ObjMap)
	if m.kind != objKindMap || ownersOf(m) != &mObj.Owners {
		t.Fatal("NewMap: dica ou ownersOf errados")
	}
	if ownersOf(Value{Type: VAL_OBJ, Obj: mObj}) != &mObj.Owners {
		t.Fatal("caminho lento diverge para map")
	}

	inst := NewInstance(&ObjStruct{Name: "P"})
	instObj := inst.Obj.(*ObjInstance)
	if inst.kind != objKindInstance || ownersOf(inst) != &instObj.Owners {
		t.Fatal("NewInstance: dica ou ownersOf errados")
	}
	if ownersOf(Value{Type: VAL_OBJ, Obj: instObj}) != &instObj.Owners {
		t.Fatal("caminho lento diverge para instance")
	}

	for _, v := range []Value{NewString("s"), NewStruct("S", nil), NewRuntimeTypeInfo(&RuntimeTypeInfo{})} {
		if v.kind != objKindNoOwners {
			t.Fatalf("%s deve carimbar objKindNoOwners, veio %d", v.String(), v.kind)
		}
		if ownersOf(v) != nil {
			t.Fatalf("%s nao tem contador", v.String())
		}
	}
	if ownersOf(NewInt(1)) != nil || ownersOf(NewNull()) != nil || ownersOf(Value{}) != nil {
		t.Fatal("escalares nao tem contador")
	}
	if ownersOf(Value{Type: VAL_OBJ, Obj: "crua"}) != nil {
		t.Fatal("string crua sem carimbo nao tem contador")
	}
}

func TestNewInstanceAdoptingDoesNotRetainAgain(t *testing.T) {
	child := NewArray(nil)
	Retain(child) // o chamador ja reteve em nome da instancia
	inst := NewInstanceAdopting(&ObjStruct{Name: "P"}, map[string]Value{"a": child})
	if got := OwnersCount(child); got != 1 {
		t.Fatalf("NewInstanceAdopting nao pode reter de novo: Owners=%d, esperado 1", got)
	}
	if inst.kind != objKindInstance {
		t.Fatal("NewInstanceAdopting deve carimbar objKindInstance")
	}
}
```

Em `TestMapAndInstanceAlsoCount` (já existe), trocar `inst := Value{Type: VAL_OBJ, Obj: &ObjInstance{Fields: map[string]Value{}}}` por `inst := NewInstance(&ObjStruct{Name: "P"})` — o caminho lento agora tem teste próprio.

- [ ] **Step 2: Rodar e ver falhar**

Run: `go test ./internal/value -run 'TestObjHeaderIsAtOffsetZero|TestOwnersOfFastAndSlowPathsAgree|TestNewInstanceAdoptingDoesNotRetainAgain'`
Expected: erro de compilação (`ObjHeader`, `NewInstanceAdopting` indefinidos).

- [ ] **Step 3: Header e carimbo nos construtores (`value.go`, `environment.go`)**

Antes de `type ObjArray struct`:

```go
// ObjHeader e o prefixo comum dos compostos que o RC rastreia (array, map,
// instancia). Owners conta referencias duraveis (RC-uniqueness, spec
// 2026-08-17) e e a unica fonte de unicidade. TEM de ser o primeiro campo
// das tres structs (offset 0 — layout_test.go trava): e o que um estagio 3
// com unsafe.Pointer usaria para alcancar o contador sem o tipo concreto.
type ObjHeader struct {
	Owners atomic.Int32
}
```

Nas três structs, trocar o campo `Owners atomic.Int32` (com o comentário de 3 linhas) por `ObjHeader` **como primeiro campo**:

```go
type ObjArray struct {
	ObjHeader
	Elements    []Value
	RuntimeType atomic.Pointer[RuntimeTypeInfo]
}

type ObjMap struct {
	ObjHeader
	store       *bindingStore
	storeOnce   sync.Once
	RuntimeType atomic.Pointer[RuntimeTypeInfo]
}

type ObjInstance struct {
	ObjHeader
	Struct *ObjStruct
	Fields map[string]Value
}
```

Construtores — carimbar `kind`:

```go
func NewString(v string) Value {
	return Value{Type: VAL_OBJ, kind: objKindNoOwners, Obj: v}
}

func NewRuntimeTypeInfo(v *RuntimeTypeInfo) Value {
	return Value{Type: VAL_OBJ, kind: objKindNoOwners, Obj: v}
}

func NewArray(elements []Value) Value {
	for _, element := range elements {
		Retain(element)
	}
	return Value{Type: VAL_OBJ, kind: objKindArray, Obj: &ObjArray{Elements: elements}}
}

func NewArrayAdopting(elements []Value) Value {
	return Value{Type: VAL_OBJ, kind: objKindArray, Obj: &ObjArray{Elements: elements}}
}

func NewMap() Value {
	mapping := &ObjMap{store: newBindingStore(nil)}
	mapping.ensureStore()
	return Value{Type: VAL_OBJ, kind: objKindMap, Obj: mapping}
}

func NewStruct(name string, fields []string) Value {
	return Value{Type: VAL_OBJ, kind: objKindNoOwners, Obj: &ObjStruct{Name: name, Fields: fields}}
}

func NewInstance(def *ObjStruct) Value {
	return Value{Type: VAL_OBJ, kind: objKindInstance, Obj: &ObjInstance{Struct: def, Fields: make(map[string]Value)}}
}

func NewInstanceWith(def *ObjStruct, fields map[string]Value) Value {
	if fields == nil {
		fields = make(map[string]Value)
	}
	for _, field := range fields {
		Retain(field)
	}
	return NewInstanceAdopting(def, fields)
}

// NewInstanceAdopting cria uma instancia ADOTANDO campos que o chamador JA
// reteve em nome dela (move): nao retem de novo — o analogo de
// NewArrayAdopting. Uso restrito aos sites que transferem posse (o clone CoW
// de instancia em calls.go); qualquer outro uso precisa de comentario
// `// RC: move` explicando quem reteve.
func NewInstanceAdopting(def *ObjStruct, fields map[string]Value) Value {
	return Value{Type: VAL_OBJ, kind: objKindInstance, Obj: &ObjInstance{Struct: def, Fields: fields}}
}
```

`NewMapWithData` já passa por `NewMap()` (herda o carimbo). `environment.go:72`:

```go
func (environment *GlobalEnvironment) ExportMap() Value {
	return Value{Type: VAL_OBJ, kind: objKindMap, Obj: &ObjMap{store: environment.local}}
}
```

- [ ] **Step 4: `ownersOf` com caminho rápido (`cow.go`)**

```go
// ownersOf devolve o contador de donos do composto, ou nil para tudo o que
// o RC nao rastreia. Caminho rapido: a dica kind carimbada pelos
// construtores de internal/value decide com uma comparacao de byte e uma
// type assertion checada (uma comparacao de ponteiro de tipo). Caminho
// lento (kind zero: Value{Type: VAL_OBJ, Obj: x} montado fora dos
// construtores): o type switch de sempre — correto, so nao acelerado. A
// dica nunca decide sozinha: um carimbo ausente nao pode virar under-count
// (owners_test.go compara os dois caminhos).
//
// Retain (64) e Release (78) embutem este corpo e sao inlinados em
// internal/vm com orcamento 80 (fora de run()); inline_guard_test.go trava.
func ownersOf(v Value) *atomic.Int32 {
	switch v.kind {
	case objKindArray:
		return &v.Obj.(*ObjArray).Owners
	case objKindMap:
		return &v.Obj.(*ObjMap).Owners
	case objKindInstance:
		return &v.Obj.(*ObjInstance).Owners
	case objKindNoOwners:
		return nil
	}
	return ownersOfSlow(v)
}

// ownersOfSlow e o caminho dos Values sem carimbo (kind zero).
func ownersOfSlow(v Value) *atomic.Int32 {
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
```

Se `go build -gcflags=-m=2 ./internal/value 2>&1 | grep -E "can inline (Retain|Release)"` mostrar custo > 80 (ou "cannot inline"), reduzir: marcar `ownersOfSlow` como não-inlinável (`//go:noinline`) já tira o switch grande do corpo embutido; se ainda estourar, converter `Release` para `for { cur := owners.Load(); if cur <= 0 || cur >= ownersSaturation { return }; if owners.CompareAndSwap(cur, cur-1) { return } }` sem variável intermediária — e registrar o custo final no commit.

- [ ] **Step 5: `calls.go:198` usa o construtor**

```go
	case *value.ObjInstance:
		cloneCount.Add(1)
		newFields := make(map[string]value.Value)
		for k, val := range obj.Fields {
			value.Retain(val) // RC: filho ganha dono duravel no clone
			newFields[k] = val
		}
		return value.NewInstanceAdopting(obj.Struct, newFields) // RC: move — retidos acima
```

- [ ] **Step 6: Guard de inline para `Retain`/`Release`**

Em `internal/vm/inline_guard_test.go`, acrescentar um teste:

```go
// Retain e Release (internal/value/cow.go) embutem ownersOf e sao inlinados
// nos sites de internal/vm fora de run() (ownSlot, bindOwnedSlot, calls.go…)
// com o orcamento normal de 80. A fase 2 de perf (issue #37) reescreveu
// ownersOf com caminho rapido pela dica kind; este teste garante que o corpo
// novo nao tirou os dois do inline.
func TestRetainReleaseStayInlinable(t *testing.T) {
	build := exec.Command("go", "build", "-gcflags=-m=2", "../value")
	output, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("go build -gcflags=-m=2 ../value failed: %v\n%s", err, output)
	}
	report := string(output)
	for _, name := range []string{"Retain", "Release"} {
		pattern := regexp.MustCompile(`can inline ` + name + ` with cost (\d+)`)
		match := pattern.FindStringSubmatch(report)
		if match == nil {
			t.Errorf("o compilador nao inlina value.%s — procure por 'cannot inline %s' em `go build -gcflags=-m=2 ./internal/value`", name, name)
			continue
		}
		cost, convErr := strconv.Atoi(match[1])
		if convErr != nil {
			t.Fatalf("custo ilegivel em %q: %v", match[0], convErr)
		}
		if cost > inlineNormalMaxCost {
			t.Errorf("value.%s tem custo de inline %d, maximo %d — enxugue ownersOf (ver cow.go)", name, cost, inlineNormalMaxCost)
		}
	}
}
```

- [ ] **Step 7: Rodar, verificar custos e commitar**

Run: `go build ./... && go vet ./internal/value ./internal/vm && go test ./internal/value ./internal/vm`
Expected: PASS. Anotar `go build -gcflags=-m=2 ./internal/value 2>&1 | grep -E "can inline (ownersOf|Retain|Release|IsShared)" | sed 's/ as: .*//'` no corpo do commit.

```bash
git add internal
git commit -m "perf(value): header comum ObjHeader{Owners} no offset 0 de array/map/instance; ownersOf decide pela dica kind carimbada nos construtores, type switch só no caminho lento (issue #37, estágio 2)"
go build -o "$SCRATCH/bench/noxy_s12.exe" ./cmd/noxy
```

---

### Task 3: Extra — `pop()` inlinável dentro de `run()`

**Files:**
- Modify: `internal/vm/stack.go:236-241`
- Modify: `internal/vm/inline_guard_test.go`

**Interfaces:**
- Consumes: `Value` de 32 B (Task 1).
- Produces: `pop` com custo ≤ 20 inlinada em `executor.go`.

- [ ] **Step 1: Guard (falha: pop custa 22 e tem 0 sites inlinados)**

Em `TestPushStaysInlinedInsideRun`, depois do bloco de `push` (antes do de `ensureCallCapacity`), acrescentar:

```go
	// pop e a segunda operacao mais quente; custava 22 (zerar os 48 bytes do
	// Value inteiro) e nao era inlinada em NENHUM dos ~84 sites de run().
	// Limpar so Obj (o unico campo que o GC enxerga) a traz para o orcamento.
	popCostPattern := regexp.MustCompile(`can inline \(\*VM\)\.pop with cost (\d+)`)
	popMatch := popCostPattern.FindStringSubmatch(report)
	if popMatch == nil {
		t.Fatalf("o compilador nao inlina (*VM).pop de jeito nenhum — procure por 'cannot inline (*VM).pop'")
	}
	popCost, popConvErr := strconv.Atoi(popMatch[1])
	if popConvErr != nil {
		t.Fatalf("custo de inline ilegivel em %q: %v", popMatch[0], popConvErr)
	}
	if popCost > inlineBigFunctionMaxCost {
		t.Errorf("pop tem custo de inline %d, maximo %d para ser inlinada dentro de run() — tire nos do corpo de pop (ver stack.go)", popCost, inlineBigFunctionMaxCost)
	}
	popInlined := 0
	for _, line := range strings.Split(report, "\n") {
		if strings.Contains(line, "executor.go") && strings.Contains(line, "inlining call to (*VM).pop") {
			popInlined++
		}
	}
	if popInlined < minPopInlineSitesInRun {
		t.Errorf("pop foi inlinada em %d call sites de executor.go, esperado >= %d (custo reportado: %d)", popInlined, minPopInlineSitesInRun, popCost)
	}
```

e a constante:

```go
// minPopInlineSitesInRun e uma margem sob os ~84 call sites de pop em
// executor.go (mesma logica de minPushInlineSitesInRun).
const minPopInlineSitesInRun = 70
```

Run: `go test ./internal/vm -run TestPushStaysInlinedInsideRun`
Expected: FAIL (`pop tem custo de inline 22` e `0 call sites`).

- [ ] **Step 2: `pop` limpa só `Obj`**

```go
// pop desempilha e limpa SO o ponteiro do slot. Zerar o Value inteiro
// (48 bytes antes da fase 2, 32 depois) custava 22 no inliner e deixava pop
// fora do inline em TODOS os ~84 sites de run() (orcamento 20 — ver push);
// para o GC so Obj interessa, e Type/num/b mortos acima de stackTop sao
// inertes (os fused jumps ja leem slots acima do topo logo depois de
// `stackTop -= 2`, sem limpar nada). inline_guard_test.go trava o custo.
func (vm *VM) pop() value.Value {
	vm.stackTop--
	val := vm.stack[vm.stackTop]
	vm.stack[vm.stackTop].Obj = nil
	return val
}
```

Se o custo ainda for > 20 (checar com `go build -gcflags=-m=2 ./internal/vm 2>&1 | grep 'can inline (\*VM).pop'`), a alternativa que cabe é não limpar nada em `pop` e limpar em `finalizeCurrentFrame`/`OP_RETURN` o trecho acima do novo topo — mas isso muda a retenção de memória; **não fazer sem medir**: se não couber, registrar o custo, deixar a variante `Obj = nil` (já mais barata: 16 B de escrita) e seguir.

- [ ] **Step 3: Rodar, commitar, binário**

Run: `go test ./internal/vm`
Expected: PASS.

```bash
git add internal/vm/stack.go internal/vm/inline_guard_test.go
git commit -m "perf(vm): pop limpa só Obj e volta a caber no orçamento de inline de run() (issue #37, extra barato)"
go build -o "$SCRATCH/bench/noxy_s12p.exe" ./cmd/noxy
```

---

### Task 4: Verificação completa (suíte, race, corpus, diff de saída)

**Files:** nenhum alterado (se algo falhar, corrigir no estágio certo com commit `fixup` descritivo).

- [ ] **Step 1: Suíte inteira e race**

Run (pode levar alguns minutos; rodar em background e **confirmar com `Get-Process go`/saída final**, nunca assumir):
```bash
go test ./... 2>&1 | tail -40
go test -race ./internal/value ./internal/vm 2>&1 | tail -10
```
Expected: `ok` em todos os pacotes.

- [ ] **Step 2: Corpus de exemplos**

Run: `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx 2>&1 | tail -15`
Expected: 0 falhas (o total cresce com exemplos novos; o juiz é "0 falhas").

- [ ] **Step 3: Diff de saída ponta a ponta base × head**

Run (PowerShell): `./benchmarks/compare_examples.ps1 -Baseline "$SCRATCH\bench\noxy_base.exe" -Candidate "$SCRATCH\bench\noxy_s12p.exe"`
Expected: `divergentes: 0`. Qualquer divergência é bug até prova em contrário (só não-determinismo pode diferir — e esses já estão na lista de exclusão do script).

---

### Task 5: Medição e registro

**Files:**
- Create: `benchmarks/results/2026-08-22-issue-37-value-layout-raw.md`
- Modify: `benchmarks/RESULTS.md` (nova seção no topo, "mais recente primeiro")
- Modify: `benchmarks/cross_runtime/README.md` (seção Resultados, se o protocolo de lá registrar rodadas — seguir o formato existente)
- Create (scratch): `$SCRATCH\bench\interleave4.ps1`

- [ ] **Step 1: Registrar a carga da máquina**

Run (PowerShell): `(Get-Counter '\Processor(_Total)\% Processor Time' -SampleInterval 1 -MaxSamples 5).CounterSamples.CookedValue | Measure-Object -Average | Select-Object -ExpandProperty Average`
Anotar no raw.

- [ ] **Step 2: Headline — protocolo do repo, base × final, mediana de 9**

Run: `./benchmarks/interleaved_compare.ps1 -Baseline "$SCRATCH\bench\noxy_base.exe" -Candidate "$SCRATCH\bench\noxy_s12p.exe" -BaselineLabel v0142 -CandidateLabel s12p -Runs 9`
Copiar `benchmarks/results/interleaved.md` para o raw (seção "headline").

- [ ] **Step 3: Isolamento por estágio — 4 binários na mesma janela**

`$SCRATCH\bench\interleave4.ps1` (Write tool):

```powershell
param([string[]]$Exes, [string[]]$Labels, [string[]]$Files, [int]$Runs = 9)
$ErrorActionPreference = "Stop"
$rows = @("| bench | " + ($Labels -join " | ") + " |", "|---|" + ("---|" * $Labels.Count))
foreach ($f in $Files) {
    $times = @{}; foreach ($l in $Labels) { $times[$l] = @() }
    for ($i = 0; $i -lt $Runs; $i++) {
        for ($k = 0; $k -lt $Exes.Count; $k++) {
            $ms = (Measure-Command { & $Exes[$k] $f | Out-Null }).TotalMilliseconds
            $times[$Labels[$k]] += [math]::Round($ms, 1)
        }
    }
    $cells = @()
    foreach ($l in $Labels) {
        $sorted = $times[$l] | Sort-Object
        $med = $sorted[[int](($Runs - 1) / 2)]; $min = $sorted[0]
        $cells += "$med (min $min)"
    }
    $rows += "| $(Split-Path $f -Leaf) | " + ($cells -join " | ") + " |"
    Write-Host ($rows[-1])
}
$rows
```

Run, com os benches do repo + `fib.nx`/`loop_arith.nx`/`bubblesort.nx`/`string_ops.nx` de `cross_runtime` copiados para `$SCRATCH\bench\src\` (disco local — OneDrive infla ~2x):

```powershell
./interleave4.ps1 -Exes @("$SCRATCH\bench\noxy_base.exe","$SCRATCH\bench\noxy_s1.exe","$SCRATCH\bench\noxy_s12.exe","$SCRATCH\bench\noxy_s12p.exe") -Labels @("base","s1","s1+2","s1+2+pop") -Files (Get-ChildItem "$SCRATCH\bench\src\*.nx").FullName -Runs 9
```

Expected: tabela mediana (min) por binário; deltas calculados contra `base` e contra o estágio anterior no raw.

- [ ] **Step 4: Cross-runtime base × final**

Run: `./benchmarks/cross_runtime/run_cross_runtime.ps1 -Noxy "$SCRATCH\bench\noxy_s12p.exe" -NoxyBaseline "$SCRATCH\bench\noxy_base.exe" -BaselineLabel v0142 -Runs 9`
Copiar `benchmarks/cross_runtime/results/cross_runtime.md` para o raw.

- [ ] **Step 5: Microbench de chamada e perfil de `fib`**

```bash
git stash list >/dev/null  # nada pendente
for rev in cb8efcb HEAD~2 HEAD~1 HEAD; do git checkout -q $rev -- internal 2>/dev/null; go test ./internal/vm -run '^$' -bench NoxyCallOverhead -count 10 2>&1 | grep Benchmark; git checkout -q HEAD -- internal; done
```
(Mais simples e seguro: rodar o bench em `develop` no diretório principal e em cada commit via `git worktree`/`git stash`; o que importa é registrar ns/op × estágio.)

Perfil: `"$SCRATCH\bench\noxy_base.exe" --cpuprofile "$SCRATCH\bench\fib_base.prof" benchmarks/cross_runtime/fib.nx` e idem com `noxy_s12p.exe`; `go tool pprof -top -nodecount=15 <exe> <prof>`. Registrar o share de `run`/`push`/`pop`/`ownersOf`/`Retain`/`Release`.

- [ ] **Step 6: Microbench Go de `ownersOf` (registro da variante `unsafe`)**

Em `$SCRATCH\ownersbench\` (módulo Go descartável) comparar três corpos sobre um slice de Values misturado (array/map/instance/string/int): type switch, `kind` + assertion checada (o embarcado), `kind` + cast `(*ObjHeader)` via data word do eface. Registrar ns/op no raw. Não entra no repo.

- [ ] **Step 7: Escrever o raw, a seção de RESULTS.md e commitar**

Seção nova no topo de `benchmarks/RESULTS.md`, no formato das anteriores: cabeçalho `## v0.14.2 (cb8efcb) × fase 2 de perf (<sha>) — layout do Value`, data, máquina, protocolo, carga; tabela headline; tabela por estágio; cross-runtime; leitura (o que confirmou/refutou a hipótese "fib 15–25 %"); gates CoW; perfil. Números como estão — "hipótese a validar, não promessa".

```bash
git add benchmarks/RESULTS.md benchmarks/results/2026-08-22-issue-37-value-layout-raw.md benchmarks/cross_runtime/README.md
git commit -m "docs(bench): fase 2 de perf — Value 32 B, header comum e pop inlinável medidos por estágio contra v0.14.2 (issue #37)"
```

---

### Task 6: CHANGELOG, versão v0.14.3 e docs

**Files:**
- Modify: `CHANGELOG.md` (nova seção `## [0.14.3] - 2026-08-22`), `internal/version/version.go`, `README.md` (badge linha 1 + banner do REPL), `AGENTS.md` (linha "Versão"), `docs/NOXY_LANGUAGE_SPEC.md` (`sys.version`), `docs/index.html` (`print(sys.version)` ~l.385 **e** `<span class="version-badge-tag">` ~l.58)

- [ ] **Step 1: Localizar os seis pontos**

Run: `grep -rn "v0\.14\.2" README.md AGENTS.md docs/NOXY_LANGUAGE_SPEC.md docs/index.html internal/version/version.go CHANGELOG.md | head -20`

- [ ] **Step 2: Editar**

`version.go`: `const Version = "v0.14.3"`. Demais: trocar `v0.14.2` → `v0.14.3` nos pontos listados. CHANGELOG, acima de `## [0.14.2]`:

```markdown
## [0.14.3] - 2026-08-22

Fase 2 de performance do VM (issue #37, estágios 1 e 2). Nenhuma mudança de
sintaxe, semântica, bytecode ou saída — só o layout interno do operando da VM
e o caminho do contador de referências. Números por estágio em
`benchmarks/RESULTS.md`.

### Performance

- **`value.Value` de 48 para 32 bytes.** Tag em `uint8`, um único campo para
  `int64`/`float64` e o `bool` no padding; a pilha de operandos cai de 96 KB
  para 64 KB e cada `push`/`pop`/cópia de operando move um terço a menos.
  Leitura pelos acessores `Int()`/`Float()`/`Bool()` (os campos `AsInt`/
  `AsFloat`/`AsBool` deixaram de existir — afeta só quem embute a VM em Go).
- **Header comum nos compostos.** `ObjArray`, `ObjMap` e `ObjInstance`
  embutem `ObjHeader{Owners}` no offset 0 (layout travado por teste), e
  `ownersOf` — chamado por todo `Retain`/`Release`/`IsShared` — decide pela
  dica carimbada nos construtores em vez do type switch, que fica só no
  caminho lento.
- **`pop()` volta a ser inlinada dentro de `run()`.** Limpa só o ponteiro do
  slot (o que o GC enxerga) e cabe no orçamento de inline de função grande;
  antes custava 22 e era chamada real em todos os ~84 sites do laço de
  despacho. Guardado em `inline_guard_test.go` junto com `push`.
- Resultado medido (mediana de 9, intercalado, máquina ociosa): <preencher
  com os números da Task 5 — fib, loop_arith, bubblesort e a suíte bench_*>.
```

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md internal/version/version.go README.md AGENTS.md docs/NOXY_LANGUAGE_SPEC.md docs/index.html
git commit -m "chore(version): noxy v0.14.3 — CHANGELOG (fase 2 de perf, issue #37), README, AGENTS, spec, site, version.go"
```

---

### Task 7: PR

- [ ] **Step 1: Push e PR (convenções do repo: título `<branch> - Descrição PT`, base `develop`, label `not available to review`, `--assignee @me`, sem reviewers)**

```bash
git push -u origin perf/issue-37-value-layout
gh pr create --base develop --assignee @me --label "not available to review" \
  --title "perf/issue-37-value-layout - Value 48→32 B, header comum nos compostos e pop inlinável (issue #37, estágios 1 e 2)" \
  --body-file "$SCRATCH/pr_body.md"
```

`pr_body.md` segue o template global (Summary / Components / Test Plan com checkboxes; implementados `[x]`, validações pendentes `[ ]`), referencia a issue (`Refs #37` — **não** `Closes`: o estágio 3 continua em aberto lá), e traz a tabela de números por estágio e as decisões tomadas sem consulta (spec §7). Terminar com `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.

- [ ] **Step 2: Comentário na issue #37** com o link do PR e o resumo dos números (uma tabela), mencionando explicitamente se a pré-condição 1 do estágio 3 ("1+2 confirmam a tese") foi atendida ou não.
