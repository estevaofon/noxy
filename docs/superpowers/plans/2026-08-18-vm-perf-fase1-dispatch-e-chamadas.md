# VM Perf Fase 1 — Dispatch e Chamadas — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fechar parte do gap Noxy×CPython eliminando os custos dominantes do caminho quente da VM — resolução de globais por nome+mutex, alocação de frame por chamada, validação redundante e dispatches não fundidos — explorando os tipos estáticos que o compilador já conhece.

**Architecture:** Nenhuma mudança de semântica ou de sintaxe. Quatro frentes no pipeline existente: (1) cache de globais com contador de geração (invalidação por escrita, sem lock no caminho de leitura); (2) opcode `OP_CALL_STATIC` para chamadas cujos modos de parâmetro o compilador já provou; (3) `CallFrame` em array fixo reusado (zero alocação por chamada); (4) opcodes fundidos/tipados novos (comparação+salto int, incremento de local int, aritmética float), todos **anexados ao fim** da lista de opcodes.

**Tech Stack:** Go 1.24 (repo atual), `runtime/pprof`, suíte `go test` + corpus `noxy_examples/`, protocolo A/B intercalado de `benchmarks/`.

**Spec:** `docs/superpowers/specs/2026-08-17-vm-perf-static-typing-research.md` (diagnóstico com anchors de código; este plano implementa as fases 0–3 de lá; fases 4–6 — structs por índice, maps tipados, layout do Value — ficam para planos futuros informados pelo re-profile).

## Global Constraints

- **Opcodes só por APPEND** ao fim do bloco `const` de `internal/chunk/chunk.go` — nunca renumerar (comentário em `OP_MARK_SHARED` confirma a regra; módulos cacheados dependem disso).
- **Semântica idêntica**: mesmos outputs, mesmos erros com as mesmas mensagens. O corpus `noxy_examples/` (164/164 via `go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx` — baseline medido em 2026-08-18 na Task 1; o juiz é "0 falhas", o total cresce quando exemplos novos entram) é o juiz.
- **RC intocado**: nenhum funil retain/release muda de lugar ou de contagem (spec CoW-RC §4.2). Os opcodes novos deste plano só operam sobre escalares (int/float), que não participam de RC.
- **Gates de benchmark** (protocolo de `benchmarks/RESULTS.md`, mediana de 9 execuções intercaladas): `bench_typed_call_map`, `bench_share_mutate`, `bench_call_light`, `bench_conway` não podem regredir >5% vs `develop`.
- `go test ./...` verde; `go test ./internal/value -race` e `go test ./internal/vm -race` verdes; `go vet ./...` limpo (o `sync.Once` novo em `Chunk` exige checagem copylocks).
- Branch de trabalho: `perf/vm-dispatch-fase1` a partir de `develop`.
- Todos os comandos abaixo assumem cwd = raiz do repo, PowerShell no Windows.

---

### Task 1: Flags de profiling no CLI + baseline pprof

**Files:**
- Modify: `cmd/noxy/main.go:32-45` (flags), `cmd/noxy/main.go:73-81` (execução)
- Modify: `docs/superpowers/specs/2026-08-17-vm-perf-static-typing-research.md` (registrar baseline)

**Interfaces:**
- Consumes: nada de tasks anteriores.
- Produces: `noxy --cpuprofile=<file> --memprofile=<file> <script.nx>`; baseline pprof registrado na spec. Tasks seguintes citam este baseline para validar impacto.

- [ ] **Step 1: Construir o baseline e criar o branch**

O binário de comparação A/B (`noxy_develop.exe`) precisa refletir o `develop` atual — o que existe na raiz do repo pode estar velho:

```powershell
git checkout develop && git pull
go build -o noxy_develop.exe ./cmd/noxy
git checkout -b perf/vm-dispatch-fase1
```

- [ ] **Step 2: Adicionar flags e hooks de pprof em main.go**

Em `cmd/noxy/main.go`, adicionar aos imports `"runtime"` e `"runtime/pprof"`. Logo abaixo da declaração de `getPkg` (linha 44), adicionar:

```go
cpuProfile := flag.String("cpuprofile", "", "Write CPU profile to file")
memProfile := flag.String("memprofile", "", "Write memory profile to file")
```

No fim de `main()`, substituir a chamada final

```go
runWithConfig(filename, string(content), getDir(filename), *showDisassembly)
```

por:

```go
if *cpuProfile != "" {
	f, err := os.Create(*cpuProfile)
	if err != nil {
		fmt.Printf("Error creating CPU profile: %s\n", err)
		os.Exit(1)
	}
	defer f.Close()
	if err := pprof.StartCPUProfile(f); err != nil {
		fmt.Printf("Error starting CPU profile: %s\n", err)
		os.Exit(1)
	}
	defer pprof.StopCPUProfile()
}

runWithConfig(filename, string(content), getDir(filename), *showDisassembly)

if *memProfile != "" {
	f, err := os.Create(*memProfile)
	if err != nil {
		fmt.Printf("Error creating memory profile: %s\n", err)
		os.Exit(1)
	}
	runtime.GC()
	if err := pprof.WriteHeapProfile(f); err != nil {
		fmt.Printf("Error writing memory profile: %s\n", err)
	}
	f.Close()
}
```

Nota: `runWithConfig` chama `os.Exit(1)` em erro de runtime — o profile só é escrito em execução bem-sucedida, o que é o comportamento desejado. Efeito colateral aceito: nesse caminho de erro o arquivo `--cpuprofile` fica criado porém vazio/truncado (o `os.Create` já aconteceu e `os.Exit` pula os defers).

- [ ] **Step 3: Build e verificação manual**

```powershell
go build -o noxy_perf.exe ./cmd/noxy
.\noxy_perf.exe --cpuprofile=fib.prof benchmarks\cross_runtime\fib.nx
go tool pprof -top -nodecount=15 noxy_perf.exe fib.prof
```

Esperado: `CHECKSUM:832040` no stdout do script; a tabela do pprof lista as funções mais quentes (esperamos ver `bindingStore.get`, `runtime.mapaccess2`, `sync.(*RWMutex)`, `(*VM).run`, alocação de `CallFrame`).

- [ ] **Step 4: Coletar baseline dos três benches e registrar**

```powershell
.\noxy_perf.exe --cpuprofile=loop.prof benchmarks\cross_runtime\loop_arith.nx
.\noxy_perf.exe --cpuprofile=bubble.prof benchmarks\cross_runtime\bubblesort.nx
go tool pprof -top -nodecount=15 noxy_perf.exe loop.prof
go tool pprof -top -nodecount=15 noxy_perf.exe bubble.prof
```

Colar as três tabelas `-top` numa seção nova **"Baseline pprof (Task 1)"** no fim de `docs/superpowers/specs/2026-08-17-vm-perf-static-typing-research.md`, com data e hash do commit. Se o profile CONTRADIZER o diagnóstico da spec (ex.: globais não aparecem no top de fib), anotar isso na spec e reordenar as tasks seguintes de acordo antes de prosseguir.

- [ ] **Step 5: Testes e commit**

```powershell
go build ./... && go test ./internal/vm -run TestVM -count=1
git add cmd/noxy/main.go docs/superpowers/specs/2026-08-17-vm-perf-static-typing-research.md
git commit -m "feat(cli): flags --cpuprofile/--memprofile e baseline pprof da fase 1"
```

---

### Task 2: Cache de globais com contador de geração

Elimina RLock + boxing de string + hash de `map[interface{}]` de todo `OP_GET_GLOBAL` estável (o caso de `fib`, que resolve o próprio nome 2× por chamada). Escritas invalidam por um contador de geração somado ao longo da cadeia de ambientes; o contador vive no `bindingStore` para que escritas via `ObjMap` que compartilha o store de um módulo (`ExportMap`, environment.go:71) também invalidem.

Limitação conhecida (aceita): há UMA entrada de cache por índice de constante por chunk. Duas tasks paralelas executando o mesmo chunk sob ambientes diferentes alternam a entrada (last-writer-wins do `atomic.Pointer`) — correto sempre (cada leitor valida Env+Gen), mas sub-cacheia em cargas spawn-heavy. Se `bench_spawn_sum` regredir na Task 8, este é o primeiro suspeito.

**Files:**
- Modify: `internal/value/map.go` (gen no `bindingStore`)
- Modify: `internal/value/environment.go` (método `Generation()`)
- Modify: `internal/chunk/chunk.go` (struct `GlobalCacheEntry`, campo+método `GlobalCache()`)
- Modify: `internal/vm/executor.go` (fast path no `OP_GET_GLOBAL`; recarga do cache nos 4 pontos de troca de frame)
- Test: `internal/value/environment_test.go`, `internal/vm/executor_test.go` (novo arquivo pequeno; padrão de `vm_test_helpers_test.go`)

**Interfaces:**
- Consumes: nada.
- Produces: `chunk.GlobalCacheEntry{Env *value.GlobalEnvironment; Gen uint64; Val value.Value}`; `(*chunk.Chunk).GlobalCache() []atomic.Pointer[GlobalCacheEntry]`; `(*value.GlobalEnvironment).Generation() uint64`. Nenhuma task posterior depende destes nomes, mas o executor passa a manter a variável local `gcache` em `run()`.

- [ ] **Step 1: Testes que falham — invalidação de geração (pacote value)**

Adicionar a `internal/value/environment_test.go`:

```go
func TestGenerationBumpsOnSetLocal(t *testing.T) {
	env := NewGlobalEnvironment(nil)
	g0 := env.Generation()
	env.SetLocal("x", NewInt(1))
	if env.Generation() == g0 {
		t.Fatal("SetLocal deve avançar a geração")
	}
}

func TestGenerationSeesParentWrites(t *testing.T) {
	root := NewGlobalEnvironment(nil)
	child := NewGlobalEnvironment(root)
	g0 := child.Generation()
	root.SetLocal("x", NewInt(1))
	if child.Generation() == g0 {
		t.Fatal("escrita no pai deve avançar a geração vista pelo filho")
	}
}

func TestGenerationSeesExportedMapWrites(t *testing.T) {
	env := NewGlobalEnvironment(nil)
	env.SetLocal("x", NewInt(1))
	g0 := env.Generation()
	exported := env.ExportMap().Obj.(*ObjMap)
	exported.Set("x", NewInt(2))
	if env.Generation() == g0 {
		t.Fatal("escrita via ObjMap que compartilha o store deve avançar a geração")
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

```powershell
go test ./internal/value -run TestGeneration -count=1
```

Esperado: FAIL de compilação (`env.Generation` indefinido).

- [ ] **Step 3: Implementar gen no bindingStore e Generation() no environment**

Em `internal/value/map.go`, adicionar `"sync/atomic"` ao import e o campo:

```go
type bindingStore struct {
	mu     sync.RWMutex
	gen    atomic.Uint64
	values map[interface{}]Value
}
```

Bumpar em toda mutação (os quatro funis; `get`/`snapshot` não mudam):

```go
func (store *bindingStore) set(key interface{}, item Value) {
	store.mu.Lock()
	store.values[key] = item
	store.mu.Unlock()
	store.gen.Add(1)
}
```

Em `defineIfAbsent`, adicionar `store.gen.Add(1)` imediatamente antes do `return true` (não bumpar no caminho `exists`). Em `delete`, idem antes do `return true`. Em `replace`, adicionar `store.gen.Add(1)` como última linha da função.

Em `internal/value/environment.go`, adicionar:

```go
// Generation soma as gerações dos stores da cadeia de ambientes. Qualquer
// escrita em qualquer nível (inclusive via ObjMap exportado que compartilha o
// store — ver ExportMap) avança a soma; os contadores só incrementam, então a
// soma é estritamente crescente e serve de token de invalidação de cache.
func (environment *GlobalEnvironment) Generation() uint64 {
	var sum uint64
	for current := environment; current != nil; current = current.parent {
		sum += current.local.gen.Load()
	}
	return sum
}
```

- [ ] **Step 4: Rodar os testes de value**

```powershell
go test ./internal/value -count=1 && go test ./internal/value -race -count=1
```

Esperado: PASS (incluindo os três novos).

- [ ] **Step 5: Teste que falha — comportamento no VM (reatribuição de global vista através de função)**

Criar `internal/vm/global_cache_test.go`:

```go
package vm

import (
	"testing"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

// Guarda o cache de globais contra staleness: a função lê o global duas
// vezes com uma reatribuição no meio — a segunda leitura TEM de ver o valor
// novo mesmo com o site de OP_GET_GLOBAL cacheado pela primeira.
func TestGlobalReadSeesReassignmentBetweenCalls(t *testing.T) {
	result := captureVMSource(t, `
let x: int = 1
func get() -> int
    return x
end
let first: int = get()
x = 2
let second: int = get()
test_report(first * 10 + second)
`)
	if result.Type != value.VAL_INT || result.AsInt != 12 {
		t.Fatalf("esperado 12 (first=1, second=2), obtido %s", result.String())
	}
}

// Mesmo chunk executado sob DOIS ambientes diferentes (o caso de módulo /
// InterpretWithEnvironment): a entrada cacheada do primeiro ambiente não pode
// vazar para o segundo — a comparação entry.Env tem de falhar e re-resolver.
// Chunk montado à mão (padrão cow_mut_opcodes_test.go): OP_GET_GLOBAL "x" sem
// OP_RETURN — o loop cai fora e deixa o valor lido em stack[1] para inspeção.
func TestGlobalCacheKeyedByEnvironment(t *testing.T) {
	machine := New()
	code := &chunk.Chunk{}
	nameIdx := code.AddConstant(value.NewString("x"))
	code.Write(byte(chunk.OP_GET_GLOBAL), 1)
	code.Write(byte(nameIdx>>8), 1)
	code.Write(byte(nameIdx&0xff), 1)

	envA := value.NewGlobalEnvironmentFrom(map[string]value.Value{"x": value.NewInt(1)}, nil)
	envB := value.NewGlobalEnvironmentFrom(map[string]value.Value{"x": value.NewInt(2)}, nil)

	if err := machine.InterpretWithEnvironment(code, envA); err != nil {
		t.Fatalf("vm error (envA): %v", err)
	}
	if got := machine.stack[1]; got.Type != value.VAL_INT || got.AsInt != 1 {
		t.Fatalf("envA: esperado 1, obtido %s", got.String())
	}

	if err := machine.InterpretWithEnvironment(code, envB); err != nil {
		t.Fatalf("vm error (envB): %v", err)
	}
	if got := machine.stack[1]; got.Type != value.VAL_INT || got.AsInt != 2 {
		t.Fatalf("envB: cache vazou o valor do envA — esperado 2, obtido %s", got.String())
	}
}
```

Rodar: `go test ./internal/vm -run TestGlobal -count=1`. Esperado: PASS já hoje (é característica de regressão — registra o contrato ANTES do cache; se falhar aqui, o teste está errado, corrigir antes de seguir). Nota: `TestGlobalCacheKeyedByEnvironment` inspeciona `machine.stack[1]` após o loop cair fora do fim do código — mesmo padrão de `cow_mut_opcodes_test.go`.

- [ ] **Step 6: Implementar o cache no chunk e no executor**

Em `internal/chunk/chunk.go`, adicionar `"sync"` e `"sync/atomic"` aos imports e, logo após a struct `Chunk`:

```go
// GlobalCacheEntry cacheia a resolução de um OP_GET_GLOBAL: válida enquanto o
// mesmo ambiente estiver ativo E a geração somada da cadeia não mudar. Env e
// Gen juntos tornam impossível servir valor stale: qualquer escrita bumpa a
// geração; chunk rodando sob outro ambiente falha a comparação de Env.
type GlobalCacheEntry struct {
	Env interface{} // *value.GlobalEnvironment (interface evita import cycle se houver)
	Gen uint64
	Val value.Value
}

func (c *Chunk) GlobalCache() []atomic.Pointer[GlobalCacheEntry] {
	c.globalCacheOnce.Do(func() {
		c.globalCache = make([]atomic.Pointer[GlobalCacheEntry], len(c.Constants))
	})
	return c.globalCache
}
```

e os campos na struct (append, sem mexer nos existentes):

```go
type Chunk struct {
	Code      []byte
	Constants []value.Value
	Lines     []int
	FileName  string

	globalCacheOnce sync.Once
	globalCache     []atomic.Pointer[GlobalCacheEntry]
}
```

Nota: `chunk` já importa `value`, então `Env` pode ser tipado como `*value.GlobalEnvironment` diretamente — preferir isso; o comentário acima só vale se o import criar ciclo (não cria: value não importa chunk).

Em `internal/vm/executor.go`, função `run()`: junto de cada ponto que faz `c = frame.Closure.Function.Chunk.(*chunk.Chunk)` (4 lugares: linha 49 na entrada, e os cases `OP_CALL`, `OP_RETURN`, `OP_IMPORT`), adicionar na linha seguinte:

```go
gcache := c.GlobalCache()
```

(na entrada é `gcache := ...`; nos cases é `gcache = ...`).

Substituir o corpo do case `OP_GET_GLOBAL` (executor.go:185-195) por:

```go
case chunk.OP_GET_GLOBAL:
	index := int(c.Code[ip])<<8 | int(c.Code[ip+1])
	ip += 2
	gen := frame.Environment.Generation()
	if entry := gcache[index].Load(); entry != nil && entry.Env == frame.Environment && entry.Gen == gen {
		vm.push(entry.Val)
		continue
	}
	nameVal := c.Constants[index]
	name := nameVal.Obj.(string)

	val, ok := frame.Environment.Resolve(name)
	if !ok {
		return vm.runtimeError(c, ip, "undefined global variable '%s'", name)
	}
	gcache[index].Store(&chunk.GlobalCacheEntry{Env: frame.Environment, Gen: gen, Val: val})
	vm.push(val)
```

Soundness da corrida gen/Resolve: a geração é lida ANTES do Resolve; uma escrita concorrente entre os dois bumpa a geração, então a entrada gravada com a geração antiga falha a comparação na próxima leitura e re-resolve — o cache pode sub-cachear, nunca servir stale.

- [ ] **Step 7: Rodar a suíte inteira + race + corpus**

```powershell
go vet ./... && go test ./... -count=1
go test ./internal/vm -race -count=1
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

Esperado: tudo verde, corpus 164/164.

- [ ] **Step 8: Medir fib A/B**

```powershell
go build -o noxy_perf.exe ./cmd/noxy
# 5 execuções intercaladas de cada binário, anotar o mínimo:
1..5 | ForEach-Object { Measure-Command { .\noxy_develop.exe benchmarks\cross_runtime\fib.nx } | Select -Expand TotalMilliseconds; Measure-Command { .\noxy_perf.exe benchmarks\cross_runtime\fib.nx } | Select -Expand TotalMilliseconds }
```

Esperado: queda clara em fib (o baseline da Task 1 diz quanto do tempo era `bindingStore.get`). Se não houver queda, parar e re-perfilar antes de seguir.

- [ ] **Step 9: Commit**

```powershell
git add internal/value/map.go internal/value/environment.go internal/chunk/chunk.go internal/vm/executor.go internal/value/environment_test.go internal/vm/global_cache_test.go
git commit -m "perf(vm): cache de globais com contador de geracao (OP_GET_GLOBAL sem lock no caminho estavel)"
```

---

### Task 3: OP_CALL_STATIC — pular validação de modos já provada pelo compilador

Quando `compileCallExpression` tem `isExact == true` (compiler.go:1800), cada argumento já foi checado contra o modo (ref/valor) e o tipo do parâmetro em compile time. `validateParameterModes` (call_validation.go:39) re-verifica isso a cada chamada em runtime. Tipos são estáveis (`docs/NOXY_LANGUAGE_SPEC.md` §2.0, "Static Typing and Type-Stable Variables"), então a prova estática vale para qualquer função que o slot tipado contenha.

A cadeia de soundness que sustenta o skip (verificada na revisão independente do plano): (1) `isExact` só nasce de `FunctionType` exato — bare `func` é `PrimitiveType{"func"}` e nunca ativa (function_types.go:23-26); (2) `areStrictTypesCompatible` rejeita `any` como fonte para tipos função (function_types.go:177-178), então nenhum valor não provado entra num slot de tipo função sem erro de compilação; (3) fronteiras dinâmicas validadas (maps/tasks) comparam `ParamIsRef` em `runtimeTypesEqual` (runtime_type_validation.go:481-485). Vetor a fechar no Step 1: membro de módulo re-atribuído em runtime (`ObjMap` mutável) — o teste de rebind cobre o análogo local, e se membros de módulo compilarem como `FunctionType` exato, adicionar variante com módulo.

**Files:**
- Modify: `internal/chunk/chunk.go` (novo opcode `OP_CALL_STATIC` — APPEND no fim do bloco const, após `OP_REF_LOCAL_BORROW`; case no `String()`; case no disassembler espelhando o de `OP_CALL` em chunk.go:451)
- Modify: `internal/compiler/compiler.go:1679-1688` (`emitCall` ganha parâmetro `static bool`) e `compiler.go:1858` (call site passa `isExact`)
- Modify: `internal/vm/executor.go` (case novo) e `internal/vm/calls.go` (função `callValueStatic`)
- Test: `internal/vm/call_static_test.go`

**Interfaces:**
- Consumes: nada.
- Produces: `chunk.OP_CALL_STATIC` (operando: 1 byte argCount, layout idêntico a `OP_CALL`); `(*VM).callValueStatic(callee value.Value, argCount int, c *chunk.Chunk, ip int) (bool, error)`; `emitCall(argCount int, emission callEmission, static bool)`.

- [ ] **Step 1: Testes de contrato (passam hoje; guardam contra over-skip)**

Criar `internal/vm/call_static_test.go`:

```go
package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

// Chamada tipada in-module: caminho que a Task 3 acelera. O resultado não
// pode mudar.
func TestTypedCallStillWorks(t *testing.T) {
	result := captureVMSource(t, `
func double(n: int) -> int
    return n * 2
end
test_report(double(21))
`)
	if result.Type != value.VAL_INT || result.AsInt != 42 {
		t.Fatalf("esperado 42, obtido %s", result.String())
	}
}

// Caminho dinâmico (any): validateParameterModes TEM de continuar disparando —
// passar valor plano onde a função espera ref é erro de runtime aqui.
func TestDynamicCallStillValidatesModes(t *testing.T) {
	machine := New()
	err := interpretVMSource(t, machine, `
func mutate(target: ref int) -> void
    target = 99
end
let f: any = mutate
f(5)
`)
	if err == nil {
		t.Fatal("chamada dinâmica com modo errado deveria falhar em runtime")
	}
	if !strings.Contains(err.Error(), "expected") {
		t.Fatalf("mensagem inesperada: %v", err)
	}
}

// Slot tipado re-atribuído para OUTRA função de mesmo tipo: o call site
// OP_CALL_STATIC continua correto porque a prova é por TIPO, não por valor
// (invariantes 1-3 do preâmbulo da task). Sintaxe do tipo função conforme
// docs/NOXY_LANGUAGE_SPEC.md §4.2 — ajustar a grafia se o parser divergir.
func TestStaticCallSurvivesSameTypedRebind(t *testing.T) {
	result := captureVMSource(t, `
func inc(n: int) -> int
    return n + 1
end
func dec(n: int) -> int
    return n - 1
end
let op: func(int) -> int = inc
let a: int = op(10)
op = dec
let b: int = op(10)
test_report(a * 100 + b)
`)
	if result.Type != value.VAL_INT || result.AsInt != 1109 {
		t.Fatalf("esperado 1109 (11*100+9), obtido %s", result.String())
	}
}
```

Rodar: `go test ./internal/vm -run "TestTypedCall|TestDynamicCall|TestStaticCall" -count=1`. Esperado: PASS. (Se `TestDynamicCallStillValidatesModes` falhar porque o COMPILADOR já rejeita, trocar a construção do valor dinâmico por uma que compile — ex.: guardar em `map[string, any]` e chamar — o objetivo é ter um caminho que chegue ao runtime sem prova estática. Ajustar até o teste passar pelo motivo certo: erro em runtime, não em compile.)

- [ ] **Step 2: Adicionar o opcode**

Em `internal/chunk/chunk.go`, no FIM do bloco const (após `OP_REF_LOCAL_BORROW`):

```go
	// perf fase 1: chamada cujos modos de parametro (ref/valor) o compilador
	// provou no call site (isExact) — o VM pula validateParameterModes para
	// closures. Layout de operando identico ao OP_CALL: [argCount u8].
	OP_CALL_STATIC
```

No `String()`, adicionar `case OP_CALL_STATIC: return "OP_CALL_STATIC"`. No `disassembleInstruction`, adicionar um case espelhando exatamente o de `OP_CALL` (chunk.go:451 — mesmo helper, nome `"OP_CALL_STATIC"`).

- [ ] **Step 3: VM — handler e callValueStatic**

Em `internal/vm/calls.go`, após `callValue`:

```go
// callValueStatic é o caminho de OP_CALL_STATIC: o compilador provou os modos
// dos argumentos no call site (isExact), e tipos são estáveis, então closures
// pulam validateParameterModes. Struct constructors e natives seguem pelo
// callValue normal — as validações deles são de outra natureza (aridade de
// struct, assinatura de native) e continuam valendo.
//
// O skip depende de três invariantes do compilador — se qualquer um mudar,
// este opcode deixa de ser sound:
//  1. isExact só nasce de FunctionType exato; bare `func` é PrimitiveType e
//     nunca ativa (function_types.go:23-26);
//  2. areStrictTypesCompatible rejeita `any` como fonte para tipos função
//     (function_types.go:177-178);
//  3. fronteiras dinâmicas validadas comparam ParamIsRef em
//     runtimeTypesEqual (runtime_type_validation.go:481-485).
func (vm *VM) callValueStatic(callee value.Value, argCount int, c *chunk.Chunk, ip int) (bool, error) {
	if callee.Type == value.VAL_FUNCTION {
		closure := callee.Obj.(*value.ObjClosure)
		if argCount != closure.Function.Arity {
			return false, vm.runtimeError(c, ip, "expected %d arguments but got %d", closure.Function.Arity, argCount)
		}
		return vm.callPreparedClosure(closure, argCount, c, ip)
	}
	return vm.callValue(callee, argCount, c, ip)
}
```

Em `internal/vm/executor.go`, adicionar o case (ao lado de `OP_CALL`, corpo idêntico exceto a função chamada):

```go
case chunk.OP_CALL_STATIC:
	argCount := int(c.Code[ip])
	ip++
	frame.IP = ip
	if ok, err := vm.callValueStatic(vm.peek(argCount), argCount, c, ip); !ok {
		return err
	}
	frame = vm.currentFrame
	c = frame.Closure.Function.Chunk.(*chunk.Chunk)
	gcache = c.GlobalCache()
	ip = frame.IP
```

- [ ] **Step 4: Compilador — emitir quando provado**

Em `internal/compiler/compiler.go:1679`, mudar a assinatura e o corpo de `emitCall`:

```go
func (c *Compiler) emitCall(argCount int, emission callEmission, static bool) {
	op := chunk.OP_CALL
	if static {
		op = chunk.OP_CALL_STATIC
	}
	line := c.currentLine
	if emission.deferred {
		op = chunk.OP_DEFER
		line = emission.registrationLine
	}
	c.currentChunk.Write(byte(op), line)
	c.currentChunk.Write(byte(argCount), line)
}
```

Atualizar TODOS os call sites de `emitCall` (buscar `c.emitCall(`): os de `chan_send`/`chan_recv` passam `false`; o site final de `compileCallExpression` (linha ~1858) passa `isExact`:

```go
c.emitCall(len(call.Arguments), emission, isExact)
```

- [ ] **Step 5: Suíte + corpus + disassembly manual**

```powershell
go vet ./... && go test ./... -count=1
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
'func f(n: int) -> int
    return n
end
f(1)' | Out-File -Encoding utf8 tmp_static.nx
go run ./cmd/noxy --disassembly tmp_static.nx | Select-String "OP_CALL"
Remove-Item tmp_static.nx
```

Esperado: suíte verde, corpus 164/164, e a linha do disassembly mostra `OP_CALL_STATIC` para a chamada tipada.

- [ ] **Step 6: Commit**

```powershell
git add internal/chunk/chunk.go internal/compiler/compiler.go internal/vm/calls.go internal/vm/executor.go internal/vm/call_static_test.go
git commit -m "perf(vm,compiler): OP_CALL_STATIC pula validateParameterModes quando o call site foi provado (isExact)"
```

---

### Task 4: CallFrame sem alocação de heap

`callPreparedClosure` faz `&CallFrame{...}` por chamada (calls.go:118). O array `vm.frames` vira array de VALORES reusados; `Owned`/`Deferred` mantêm capacidade entre reusos.

**Files:**
- Modify: `internal/vm/vm.go:57` (`frames [FramesMax]*CallFrame` → `frames [FramesMax]CallFrame`)
- Modify: `internal/vm/calls.go:113-140`, `internal/vm/executor.go:39-41`, `internal/vm/unwind.go:61-64,71,81`, `internal/vm/references.go:176,220`, `internal/vm/runtime_errors.go:107`, `internal/vm/task_execution.go:106`, `internal/vm/builtins_concurrency.go:87`
- Test: `internal/vm/call_alloc_bench_test.go` + suíte existente (characterization tests já cobrem semântica)

**Interfaces:**
- Consumes: nada.
- Produces: `vm.frames` passa a ser `[FramesMax]CallFrame`; todo código que precisar de `*CallFrame` usa `&vm.frames[i]`. `vm.currentFrame` continua `*CallFrame` (aponta para dentro do array, que tem tamanho fixo — ponteiros estáveis).

- [ ] **Step 1: Benchmark que expõe a alocação (falha por medida, não por assert)**

Criar `internal/vm/call_alloc_bench_test.go`:

```go
package vm

import "testing"

// Mede alocações por chamada de função Noxy. Antes da Task 4: >=2 allocs/op
// no caminho de chamada (CallFrame + Owned). Depois: 0 allocs/op de frame em
// regime estacionário (capacidades de Owned/Deferred reusadas).
func BenchmarkNoxyCallOverhead(b *testing.B) {
	machine := New()
	code := compileVMSourceForBench(b, `
func leaf(n: int) -> int
    return n
end
func run(times: int) -> int
    let i: int = 0
    let acc: int = 0
    while i < times do
        acc = leaf(i)
        i = i + 1
    end
    return acc
end
run(1000)
`)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := machine.Interpret(code); err != nil {
			b.Fatalf("vm error: %v", err)
		}
	}
}
```

E o helper (mesmo arquivo — `compileVMSource` existente exige `*testing.T`):

```go
import (
	"testing"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
)

func compileVMSourceForBench(b *testing.B, source string) *chunk.Chunk {
	b.Helper()
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		b.Fatalf("parser errors: %v", p.Errors())
	}
	code, _, err := compiler.New().Compile(program)
	if err != nil {
		b.Fatalf("compiler error: %v", err)
	}
	return code
}
```

- [ ] **Step 2: Rodar e registrar o número de antes**

```powershell
go test ./internal/vm -bench BenchmarkNoxyCallOverhead -benchmem -run xxx -count=1
```

Anotar `allocs/op` no output (esperado: milhares — ~1 CallFrame por chamada × 1000 chamadas/op).

- [ ] **Step 3: Trocar o array para valores e ajustar todos os usos**

Em `internal/vm/vm.go:57`: `frames [FramesMax]CallFrame`.

Em `internal/vm/calls.go`, substituir o miolo de `callPreparedClosure` (linhas 118-138):

```go
frame := &vm.frames[vm.frameCount]
frame.Closure = closure
frame.IP = 0
frame.StackBase = vm.stackTop - argCount - 1
frame.LocalBase = vm.stackTop - argCount - 1
frame.Environment = closure.Environment
frame.Deferred = frame.Deferred[:0]
frame.Owned = frame.Owned[:0]

// RC: parametros sem ref sao vinculos duraveis do frame novo
params := closure.Function.Params
for i := 0; i < argCount; i++ {
	if i < len(params) && params[i].IsRef {
		continue
	}
	frame.ownSlot(vm, frame.LocalBase+1+i)
}

vm.frameCount++
vm.currentFrame = frame
return true, nil
```

Em `internal/vm/executor.go:39-41` (`InterpretWithEnvironment`):

```go
frame := &vm.frames[0]
*frame = CallFrame{Closure: scriptClosure, IP: 0, StackBase: 0, LocalBase: 1, Environment: environment, Deferred: frame.Deferred[:0], Owned: frame.Owned[:0]}
vm.frameCount = 1
vm.currentFrame = frame
```

Em `internal/vm/unwind.go` (`finalizeCurrentFrame`), DOIS pontos — atenção: a revisão independente pegou que o ponto crítico é a linha 64, não a 71:

1. **Linha 61-64** — o loop de release já pagou cada objeto; o `frame.Owned = nil` da linha 64 é o que joga a capacidade fora (e faria a meta de zero-alloc falhar: todo `ownSlot` da próxima chamada realocaria). Substituir por limpeza que preserva o backing array:

```go
for i := range frame.Owned {
	value.Release(frame.Owned[i].obj)
	frame.Owned[i] = ownedEntry{}
}
frame.Owned = frame.Owned[:0]
```

(O zerar de cada entrada é obrigatório: o backing array sobrevive ao frame e não pode reter `value.Value` de objetos já liberados.)

2. **Linha 71** — `vm.frames[frameIndex] = nil` deixa de compilar com o array de valores. Substituir por reset de campos que preserva `Owned`/`Deferred` (o `Deferred` já foi esvaziado entrada a entrada pelo loop das linhas 27-31, que zera cada `PreparedCall` e mantém a capacidade):

```go
frame.Closure = nil
frame.Environment = nil
```

Em `unwind.go:81`: `vm.currentFrame = &vm.frames[vm.frameCount-1]`.

Em `references.go:176,220` e `runtime_errors.go:107`: `frame := &vm.frames[i]`.
Em `task_execution.go:106` e `builtins_concurrency.go:87`: mesmo padrão do `InterpretWithEnvironment` acima (atribuir campos via `*frame = CallFrame{...}` preservando `Deferred`/`Owned` com `[:0]`).

Compilar e caçar o resto: `go build ./...` aponta cada uso restante de `*CallFrame` incompatível — ajustar um a um com `&vm.frames[i]`.

- [ ] **Step 4: Suíte completa + race + corpus (o RC é o risco aqui)**

```powershell
go vet ./... && go test ./... -count=1
go test ./internal/vm -race -count=1
go test ./internal/value -race -count=1
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

Esperado: tudo verde, 164/164. Atenção especial a `rc_uniqueness_test.go`, `reference_ownership_test.go`, `defer_test.go`, `unwind_test.go` — se qualquer um falhar, o reuso de `Owned`/`Deferred` vazou estado entre frames; revisar a limpeza do Step 3.

- [ ] **Step 5: Re-rodar o benchmark de alocação**

```powershell
go test ./internal/vm -bench BenchmarkNoxyCallOverhead -benchmem -run xxx -count=1
```

Esperado: `allocs/op` cai para perto de zero por chamada (sobra só o que `ownSlot` aloca na primeira expansão de `Owned`, amortizado). Registrar antes/depois no commit message.

- [ ] **Step 6: Commit**

```powershell
git add internal/vm
git commit -m "perf(vm): CallFrame em array de valores reusado - zero alocacao por chamada (allocs/op: <antes> -> <depois>)"
```

---

### Task 5: Comparação+salto fundidos para int (while e if)

`while i < N do` hoje custa por iteração: `OP_LESS_INT` + `OP_JUMP_IF_FALSE` + `OP_POP` (3 dispatches + um bool de ida e volta na pilha). `if n <= 1 then` (fib!) idem. Funde num opcode único que consome os dois ints e salta.

**Files:**
- Modify: `internal/chunk/chunk.go` (6 opcodes novos por APPEND + `TruncateTo` + String/disasm)
- Modify: `internal/compiler/compiler.go` (helper `tryCompileFusedCondition` + integração no `WhileStatement` (linha 1142) e `IfStatement` (linha 1100))
- Modify: `internal/vm/executor.go` (6 cases)
- Test: `internal/vm/fused_jump_test.go`, `internal/compiler/fused_jump_compile_test.go`

**Interfaces:**
- Consumes: nada.
- Produces: opcodes `OP_JUMP_IF_LT_INT`, `OP_JUMP_IF_LE_INT`, `OP_JUMP_IF_GT_INT`, `OP_JUMP_IF_GE_INT`, `OP_JUMP_IF_EQ_INT`, `OP_JUMP_IF_NE_INT` — todos com operando `[hi][lo]` (mesmo layout de `OP_JUMP`, compatíveis com `emitJump`/`patchJump`); **saltam quando a condição NOMEADA vale**, consumindo dois ints da pilha. `(*Chunk).TruncateTo(n int)`. Task 6 e 7 assumem esses opcodes já no enum (ordem de append: os 6 desta task vêm ANTES dos da Task 6/7).

- [ ] **Step 1: Testes de VM que falham (chunks montados à mão, padrão cow_mut_opcodes_test.go)**

Criar `internal/vm/fused_jump_test.go`:

```go
package vm

import (
	"testing"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

// Frame raiz: stack[0] = script closure; empilhamos a,b e o opcode fundido
// salta (ou não) sobre um OP_CONSTANT sentinela. Sem OP_RETURN de propósito
// (o loop cai fora no fim do código e deixa a pilha para inspeção).
func runFusedJump(t *testing.T, op chunk.OpCode, a, b int64) (jumped bool) {
	t.Helper()
	machine := New()
	code := &chunk.Chunk{}
	ca := code.AddConstant(value.NewInt(a))
	cb := code.AddConstant(value.NewInt(b))
	sentinel := code.AddConstant(value.NewInt(777))
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(ca), 1)
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(cb), 1)
	code.Write(byte(op), 1)
	code.Write(0, 1) // offset hi
	code.Write(2, 1) // offset lo: pula o OP_CONSTANT sentinela (2 bytes)
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(sentinel), 1)
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	// Se saltou, a pilha ficou só com a closure (stackTop==1); senão, o
	// sentinela 777 está no topo.
	if machine.stackTop == 1 {
		return true
	}
	top := machine.stack[machine.stackTop-1]
	if top.Type != value.VAL_INT || top.AsInt != 777 {
		t.Fatalf("pilha inesperada: top=%s stackTop=%d", top.String(), machine.stackTop)
	}
	return false
}

func TestFusedJumpOpcodes(t *testing.T) {
	cases := []struct {
		name string
		op   chunk.OpCode
		a, b int64
		want bool
	}{
		{"LT salta quando a<b", chunk.OP_JUMP_IF_LT_INT, 1, 2, true},
		{"LT nao salta quando a>=b", chunk.OP_JUMP_IF_LT_INT, 2, 2, false},
		{"LE salta quando a<=b", chunk.OP_JUMP_IF_LE_INT, 2, 2, true},
		{"LE nao salta quando a>b", chunk.OP_JUMP_IF_LE_INT, 3, 2, false},
		{"GT salta quando a>b", chunk.OP_JUMP_IF_GT_INT, 3, 2, true},
		{"GT nao salta quando a<=b", chunk.OP_JUMP_IF_GT_INT, 2, 2, false},
		{"GE salta quando a>=b", chunk.OP_JUMP_IF_GE_INT, 2, 2, true},
		{"GE nao salta quando a<b", chunk.OP_JUMP_IF_GE_INT, 1, 2, false},
		{"EQ salta quando a==b", chunk.OP_JUMP_IF_EQ_INT, 5, 5, true},
		{"EQ nao salta quando a!=b", chunk.OP_JUMP_IF_EQ_INT, 5, 6, false},
		{"NE salta quando a!=b", chunk.OP_JUMP_IF_NE_INT, 5, 6, true},
		{"NE nao salta quando a==b", chunk.OP_JUMP_IF_NE_INT, 5, 5, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runFusedJump(t, tc.op, tc.a, tc.b); got != tc.want {
				t.Fatalf("jumped=%v, esperado %v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

```powershell
go test ./internal/vm -run TestFusedJump -count=1
```

Esperado: FAIL de compilação (opcodes indefinidos). Após o Step 3 parcial (só o enum), falham por comportamento: opcode sem case é no-op — o sentinela sempre aparece.

- [ ] **Step 3: Opcodes no chunk + TruncateTo**

Em `internal/chunk/chunk.go`, APPEND no fim do bloco const (depois de `OP_CALL_STATIC` da Task 3):

```go
	// perf fase 1: comparacao int + salto condicional fundidos. Consomem dois
	// VAL_INT da pilha (sem zerar: escalares nao carregam ponteiros) e saltam
	// [hi][lo] adiante quando a condicao NOMEADA vale. Emitidos pelo
	// compilador so quando ambos os lados sao estaticamente int; o salto e o
	// jump-if-false da condicao de origem (`<` emite GE, `<=` emite GT, ...).
	OP_JUMP_IF_LT_INT
	OP_JUMP_IF_LE_INT
	OP_JUMP_IF_GT_INT
	OP_JUMP_IF_GE_INT
	OP_JUMP_IF_EQ_INT
	OP_JUMP_IF_NE_INT
```

`String()`: um case por opcode devolvendo o próprio nome. `disassembleInstruction`: um case por opcode espelhando o de `OP_JUMP` (mesmo helper de jump, chunk.go região da linha ~445).

E o truncate (após `Write`):

```go
// TruncateTo descarta bytecode ja emitido a partir do offset n (Code e Lines
// andam em paralelo). Usado pelo compilador para desfazer a emissao
// especulativa dos operandos de uma condicao que nao pode ser fundida.
func (c *Chunk) TruncateTo(n int) {
	c.Code = c.Code[:n]
	c.Lines = c.Lines[:n]
}
```

- [ ] **Step 4: Cases no executor**

Em `internal/vm/executor.go` (os seis são iguais salvo o operador; sem zerar slots — os operandos são VAL_INT, `Obj == nil`, nada para o GC reter):

```go
case chunk.OP_JUMP_IF_LT_INT:
	offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
	ip += 2
	vm.stackTop -= 2
	if vm.stack[vm.stackTop].AsInt < vm.stack[vm.stackTop+1].AsInt {
		ip += offset
	}

case chunk.OP_JUMP_IF_LE_INT:
	offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
	ip += 2
	vm.stackTop -= 2
	if vm.stack[vm.stackTop].AsInt <= vm.stack[vm.stackTop+1].AsInt {
		ip += offset
	}

case chunk.OP_JUMP_IF_GT_INT:
	offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
	ip += 2
	vm.stackTop -= 2
	if vm.stack[vm.stackTop].AsInt > vm.stack[vm.stackTop+1].AsInt {
		ip += offset
	}

case chunk.OP_JUMP_IF_GE_INT:
	offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
	ip += 2
	vm.stackTop -= 2
	if vm.stack[vm.stackTop].AsInt >= vm.stack[vm.stackTop+1].AsInt {
		ip += offset
	}

case chunk.OP_JUMP_IF_EQ_INT:
	offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
	ip += 2
	vm.stackTop -= 2
	if vm.stack[vm.stackTop].AsInt == vm.stack[vm.stackTop+1].AsInt {
		ip += offset
	}

case chunk.OP_JUMP_IF_NE_INT:
	offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
	ip += 2
	vm.stackTop -= 2
	if vm.stack[vm.stackTop].AsInt != vm.stack[vm.stackTop+1].AsInt {
		ip += offset
	}
```

Rodar `go test ./internal/vm -run TestFusedJump -count=1` → PASS.

- [ ] **Step 5: Compilador — tryCompileFusedCondition com rollback**

Em `internal/compiler/compiler.go`, adicionar (perto de `emitJump`, ~linha 2064):

```go
// fusedIntCompareJump mapeia o operador da CONDICAO para o opcode que salta
// quando a condicao FALHA (e o jump-if-false fundido): `i < n` continua no
// corpo quando vale e salta para fora com GE.
func fusedIntCompareJump(operator string) (chunk.OpCode, bool) {
	switch operator {
	case "<":
		return chunk.OP_JUMP_IF_GE_INT, true
	case "<=":
		return chunk.OP_JUMP_IF_GT_INT, true
	case ">":
		return chunk.OP_JUMP_IF_LE_INT, true
	case ">=":
		return chunk.OP_JUMP_IF_LT_INT, true
	case "==":
		return chunk.OP_JUMP_IF_NE_INT, true
	case "!=":
		return chunk.OP_JUMP_IF_EQ_INT, true
	}
	return 0, false
}

// tryCompileFusedCondition tenta compilar cond como (left, right) ints puros e
// devolve o opcode de salto fundido a emitir com emitJump. Emissao
// especulativa: se um dos lados nao for estaticamente int, o bytecode dos
// operandos e DESFEITO (TruncateTo) e o chamador segue o caminho generico.
// Constantes adicionadas na especulacao ficam orfas no pool — inofensivo.
func (c *Compiler) tryCompileFusedCondition(cond ast.Expression) (chunk.OpCode, bool, error) {
	infix, ok := cond.(*ast.InfixExpression)
	if !ok {
		return 0, false, nil
	}
	jumpOp, ok := fusedIntCompareJump(infix.Operator)
	if !ok {
		return 0, false, nil
	}
	checkpoint := len(c.currentChunk.Code)
	_, leftType, err := c.Compile(infix.Left)
	if err != nil {
		return 0, false, err
	}
	if leftType == nil || leftType.String() != "int" {
		c.currentChunk.TruncateTo(checkpoint)
		return 0, false, nil
	}
	_, rightType, err := c.Compile(infix.Right)
	if err != nil {
		return 0, false, err
	}
	if rightType == nil || rightType.String() != "int" {
		c.currentChunk.TruncateTo(checkpoint)
		return 0, false, nil
	}
	return jumpOp, true, nil
}
```

No `WhileStatement` (linha 1142), substituir o trecho condição→`jumpToExit` (linhas 1150-1161) por:

```go
fusedOp, fused, err := c.tryCompileFusedCondition(n.Condition)
if err != nil {
	return nil, nil, err
}
var jumpToExit int
if fused {
	jumpToExit = c.emitJump(fusedOp)
} else {
	_, condType, err := c.Compile(n.Condition)
	if err != nil {
		return nil, nil, err
	}
	if _, ok := condType.(*ast.RefType); ok {
		c.emitByte(byte(chunk.OP_DEREF))
	}
	jumpToExit = c.emitJump(chunk.OP_JUMP_IF_FALSE)
	c.emitByte(byte(chunk.OP_POP)) // Pop condition
}
```

E na saída (linha 1172), condicionar o segundo POP:

```go
c.patchJump(jumpToExit)
if !fused {
	c.emitByte(byte(chunk.OP_POP)) // Pop condition at exit
}
```

No `IfStatement` (linha 1100), mesma cirurgia: trecho condição→`jumpToElse` vira o bloco fused/genérico acima (com `jumpToElse` no lugar de `jumpToExit`), o `OP_POP` de entrada do THEN (linha 1116) e o `OP_POP` pós-`patchJump(jumpToElse)` (linha 1128) ficam ambos dentro de `if !fused { ... }`.

- [ ] **Step 6: Testes de comportamento do compilador**

Criar `internal/compiler/fused_jump_compile_test.go` (o pacote compiler tem testes que compilam e inspecionam erro; aqui validamos que programas com while/if fundidos e não-fundíveis compilam — o comportamento em execução é coberto pelo corpus e pelo teste de VM abaixo):

```go
package compiler

import (
	"testing"

	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
)

func compileFusedSource(t *testing.T, source string) error {
	t.Helper()
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	_, _, err := New().Compile(program)
	return err
}

func TestFusedWhileCompiles(t *testing.T) {
	if err := compileFusedSource(t, `
func f() -> int
    let i: int = 0
    while i < 10 do
        i = i + 1
    end
    return i
end
f()
`); err != nil {
		t.Fatalf("compile error: %v", err)
	}
}

// Condição float NÃO pode fundir: o rollback (TruncateTo) tem de deixar o
// caminho genérico intacto.
func TestNonIntConditionFallsBack(t *testing.T) {
	if err := compileFusedSource(t, `
func f() -> int
    let x: float = 0.0
    let n: int = 0
    while x < 10.0 do
        x = x + 1.0
        n = n + 1
    end
    return n
end
f()
`); err != nil {
		t.Fatalf("compile error (fallback quebrado): %v", err)
	}
}

// Condição com curto-circuito dentro do operando (jumps internos) sob
// rollback: `(a < b) == flag` compila o InfixExpression externo `==` — o lado
// esquerdo é bool, então o fuse desiste após compilar o operando esquerdo.
func TestBoolOperandRollsBack(t *testing.T) {
	if err := compileFusedSource(t, `
func f(a: int, b: int, flag: bool) -> int
    if (a < b) == flag then
        return 1
    end
    return 0
end
f(1, 2, true)
`); err != nil {
		t.Fatalf("compile error (rollback com operando bool): %v", err)
	}
}
```

E o teste de execução ponta-a-ponta em `internal/vm/fused_jump_test.go` (append):

```go
func TestFusedWhileAndIfBehavior(t *testing.T) {
	result := captureVMSource(t, `
func fib(n: int) -> int
    if n <= 1 then
        return n
    end
    return fib(n - 1) + fib(n - 2)
end
let i: int = 0
let acc: int = 0
while i < 5 do
    acc = acc + fib(i)
    i = i + 1
end
test_report(acc)
`)
	if result.Type != value.VAL_INT || result.AsInt != 7 {
		t.Fatalf("esperado 7 (fib 0..4 = 0+1+1+2+3), obtido %s", result.String())
	}
}
```

- [ ] **Step 7: Suíte + corpus + medida**

```powershell
go vet ./... && go test ./... -count=1
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
go build -o noxy_perf.exe ./cmd/noxy
1..5 | ForEach-Object { Measure-Command { .\noxy_develop.exe benchmarks\cross_runtime\loop_arith.nx } | Select -Expand TotalMilliseconds; Measure-Command { .\noxy_perf.exe benchmarks\cross_runtime\loop_arith.nx } | Select -Expand TotalMilliseconds }
```

Esperado: verde, 164/164, queda visível em loop_arith e fib.

- [ ] **Step 8: Commit**

```powershell
git add internal/chunk/chunk.go internal/compiler/compiler.go internal/compiler/fused_jump_compile_test.go internal/vm/executor.go internal/vm/fused_jump_test.go
git commit -m "perf(vm,compiler): comparacao int + salto fundidos em while/if (6 opcodes, rollback especulativo no codegen)"
```

---

### Task 6: OP_INC_LOCAL_INT — incremento de local sem tráfego de pilha

`i = i + 1` hoje emite GET_LOCAL + CONSTANT + ADD_INT + SET_LOCAL + POP (5 dispatches, ~4 cópias de 48 bytes). Vira 1 opcode que soma um delta int8 direto no slot.

**Files:**
- Modify: `internal/chunk/chunk.go` (opcode `OP_INC_LOCAL_INT [slot u8][delta i8]` por APPEND, String, disasm com 2 operandos de byte — espelhar um case de 1 byte e imprimir os dois)
- Modify: `internal/compiler/compiler.go:340` (fusão no início do branch de atribuição a identifier)
- Modify: `internal/vm/executor.go` (case novo)
- Test: `internal/vm/inc_local_test.go`

**Interfaces:**
- Consumes: enum já contém os opcodes da Task 5 (append depois deles).
- Produces: `chunk.OP_INC_LOCAL_INT`; `(*Compiler).tryFuseLocalIntIncrement(ident *ast.Identifier, valueExpr ast.Expression) bool`.

- [ ] **Step 1: Teste de VM que falha (chunk à mão)**

Criar `internal/vm/inc_local_test.go`:

```go
package vm

import (
	"testing"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

// Frame raiz: LocalBase = 1, slot local 0 = stack[1]. Empilha 5 no slot e
// aplica dois incrementos fundidos (+3, -1). Sem OP_RETURN de propósito.
func TestIncLocalInt(t *testing.T) {
	machine := New()
	code := &chunk.Chunk{}
	five := code.AddConstant(value.NewInt(5))
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(five), 1)
	code.Write(byte(chunk.OP_INC_LOCAL_INT), 1)
	code.Write(0, 1)                // slot
	code.Write(byte(int8(3)), 1)    // delta +3
	code.Write(byte(chunk.OP_INC_LOCAL_INT), 1)
	code.Write(0, 1)                // slot
	code.Write(byte(int8(-1)), 1)   // delta -1
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	got := machine.stack[1]
	if got.Type != value.VAL_INT || got.AsInt != 7 {
		t.Fatalf("esperado slot=7, obtido %s", got.String())
	}
}
```

Rodar `go test ./internal/vm -run TestIncLocalInt -count=1` → FAIL (opcode indefinido; depois do enum, no-op deixa o slot em 5).

- [ ] **Step 2: Opcode + handler**

`internal/chunk/chunk.go`, APPEND após `OP_JUMP_IF_NE_INT`:

```go
	// perf fase 1: `i = i + K` / `i = i - K` (i local int possuidor, K literal
	// que cabe em i8) fundido em soma direta no slot — sem trafego de pilha.
	// Operandos: [slot u8][delta i8]. Overflow wrappa como OP_ADD_INT.
	OP_INC_LOCAL_INT
```

String + case de disassembler (2 operandos: imprimir slot e delta; seguir o padrão do case de `OP_GET_LOCAL` estendido para o segundo byte).

`internal/vm/executor.go`:

```go
case chunk.OP_INC_LOCAL_INT:
	slot := c.Code[ip]
	delta := int8(c.Code[ip+1])
	ip += 2
	vm.stack[frame.LocalBase+int(slot)].AsInt += int64(delta)
```

Rodar o teste do Step 1 → PASS.

- [ ] **Step 3: Fusão no compilador**

Em `internal/compiler/compiler.go`, adicionar o helper (perto de `localOwns`, linha 2150):

```go
// tryFuseLocalIntIncrement funde `i = i + K` / `i = i - K` (i local int
// POSSUIDOR — slot ref nunca funde; K literal int em [-128,127]) em
// OP_INC_LOCAL_INT. Retorna true se emitiu (nada mais a fazer no site).
// Sem emissao especulativa: todas as checagens sao sintaticas/de simbolo.
func (c *Compiler) tryFuseLocalIntIncrement(ident *ast.Identifier, valueExpr ast.Expression) bool {
	arg, localType := c.resolveLocal(ident.Value)
	if arg == -1 || arg > 255 || !c.localOwns(arg) {
		return false
	}
	prim, ok := localType.(*ast.PrimitiveType)
	if !ok || prim.Name != "int" {
		return false
	}
	infix, ok := valueExpr.(*ast.InfixExpression)
	if !ok || (infix.Operator != "+" && infix.Operator != "-") {
		return false
	}
	left, ok := infix.Left.(*ast.Identifier)
	if !ok || left.Value != ident.Value {
		return false
	}
	lit, ok := infix.Right.(*ast.IntegerLiteral)
	if !ok {
		return false
	}
	delta := lit.Value
	if infix.Operator == "-" {
		delta = -delta
	}
	if delta < -128 || delta > 127 {
		return false
	}
	c.emitBytes(byte(chunk.OP_INC_LOCAL_INT), byte(arg))
	c.emitByte(byte(int8(delta)))
	return true
}
```

No branch de atribuição a identifier (linha 340), ANTES de compilar o valor (linha 343):

```go
} else if ident, ok := n.Target.(*ast.Identifier); ok {
	// Fusao de incremento: emite OP_INC_LOCAL_INT e nao empilha nada
	// (atribuicao e statement; o POP do caminho generico tambem cai fora).
	if c.tryFuseLocalIntIncrement(ident, n.Value) {
		return c.currentChunk, nil, nil
	}
	// Identifier Assignment: x = val
	// 1. Compile Value (pushed to stack)
	_, valType, err := c.Compile(n.Value)
	...
```

Atenção: se `IntegerLiteral.Value` não for `int64` em `internal/ast/ast.go:244`, ajustar a comparação de faixa ao tipo real.

- [ ] **Step 4: Teste de comportamento + suíte + corpus**

Append em `internal/vm/inc_local_test.go`:

```go
func TestIncLocalIntBehavior(t *testing.T) {
	result := captureVMSource(t, `
func count() -> int
    let i: int = 0
    let downs: int = 100
    while i < 10 do
        i = i + 1
        downs = downs - 2
    end
    return i * 1000 + downs
end
test_report(count())
`)
	if result.Type != value.VAL_INT || result.AsInt != 10080 {
		t.Fatalf("esperado 10080 (i=10, downs=80), obtido %s", result.String())
	}
}
```

```powershell
go vet ./... && go test ./... -count=1
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

- [ ] **Step 5: Commit**

```powershell
git add internal/chunk/chunk.go internal/compiler/compiler.go internal/vm/executor.go internal/vm/inc_local_test.go
git commit -m "perf(vm,compiler): OP_INC_LOCAL_INT funde i = i +- K em soma direta no slot"
```

---

### Task 7: Aritmética e comparação FLOAT especializadas

Espelho float dos opcodes `_INT` existentes, para mandelbrot e código numérico. Mistos int/float continuam no caminho genérico.

**Files:**
- Modify: `internal/chunk/chunk.go` (APPEND: `OP_ADD_FLOAT`, `OP_SUB_FLOAT`, `OP_MUL_FLOAT`, `OP_DIV_FLOAT`, `OP_LESS_FLOAT`, `OP_GREATER_FLOAT` + String/disasm)
- Modify: `internal/compiler/compiler.go:930-1018` (bloco `isInt` ganha irmão `isFloat`)
- Modify: `internal/vm/executor.go` (6 cases)
- Test: `internal/vm/float_ops_test.go`

**Interfaces:**
- Consumes: enum com os opcodes das Tasks 3/5/6 já anexados (append depois).
- Produces: os 6 opcodes float. A Task 5 NÃO é estendida para floats neste plano (comparação float em condição continua genérica — anotar como follow-up).

- [ ] **Step 1: Teste de comportamento (falha só após emissão; escrever primeiro mesmo assim)**

Criar `internal/vm/float_ops_test.go`:

```go
package vm

import (
	"math"
	"testing"

	"noxy-vm/internal/value"
)

func TestFloatArithmeticSpecialized(t *testing.T) {
	result := captureVMSource(t, `
func mandel_step(cr: float, ci: float) -> float
    let zr: float = 0.0
    let zi: float = 0.0
    let i: int = 0
    while i < 10 do
        let tmp: float = zr * zr - zi * zi + cr
        zi = 2.0 * zr * zi + ci
        zr = tmp
        i = i + 1
    end
    return zr * zr + zi * zi
end
test_report(mandel_step(-0.5, 0.25))
`)
	if result.Type != value.VAL_FLOAT {
		t.Fatalf("esperado float, obtido %s", result.String())
	}
	if math.IsNaN(result.AsFloat) || math.IsInf(result.AsFloat, 0) {
		t.Fatalf("resultado invalido: %v", result.AsFloat)
	}
}

func TestFloatDivisionByZeroStillErrors(t *testing.T) {
	machine := New()
	err := interpretVMSource(t, machine, `
func f(a: float, b: float) -> float
    return a / b
end
f(1.0, 0.0)
`)
	if err == nil {
		t.Fatal("divisao float por zero deveria continuar sendo erro de runtime")
	}
}
```

Rodar: PASS já hoje (caminho genérico) — são testes de contrato; a regressão que eles caçam é o VM handler divergir do genérico.

- [ ] **Step 2: Opcodes + handlers**

`internal/chunk/chunk.go`, APPEND após `OP_INC_LOCAL_INT`:

```go
	// perf fase 1: irmaos float dos opcodes _INT (ambos os lados
	// estaticamente float). Mistos int/float continuam no caminho generico.
	OP_ADD_FLOAT
	OP_SUB_FLOAT
	OP_MUL_FLOAT
	OP_DIV_FLOAT
	OP_LESS_FLOAT
	OP_GREATER_FLOAT
```

String + disasm (cases simples). `internal/vm/executor.go` (estilo inline do `OP_ADD_INT`; sem zerar — escalares):

```go
case chunk.OP_ADD_FLOAT:
	vm.stack[vm.stackTop-2] = value.NewFloat(vm.stack[vm.stackTop-2].AsFloat + vm.stack[vm.stackTop-1].AsFloat)
	vm.stackTop--

case chunk.OP_SUB_FLOAT:
	vm.stack[vm.stackTop-2] = value.NewFloat(vm.stack[vm.stackTop-2].AsFloat - vm.stack[vm.stackTop-1].AsFloat)
	vm.stackTop--

case chunk.OP_MUL_FLOAT:
	vm.stack[vm.stackTop-2] = value.NewFloat(vm.stack[vm.stackTop-2].AsFloat * vm.stack[vm.stackTop-1].AsFloat)
	vm.stackTop--

case chunk.OP_DIV_FLOAT:
	if vm.stack[vm.stackTop-1].AsFloat == 0 {
		return vm.runtimeError(c, ip, "division by zero")
	}
	vm.stack[vm.stackTop-2] = value.NewFloat(vm.stack[vm.stackTop-2].AsFloat / vm.stack[vm.stackTop-1].AsFloat)
	vm.stackTop--

case chunk.OP_LESS_FLOAT:
	vm.stack[vm.stackTop-2] = value.NewBool(vm.stack[vm.stackTop-2].AsFloat < vm.stack[vm.stackTop-1].AsFloat)
	vm.stackTop--

case chunk.OP_GREATER_FLOAT:
	vm.stack[vm.stackTop-2] = value.NewBool(vm.stack[vm.stackTop-2].AsFloat > vm.stack[vm.stackTop-1].AsFloat)
	vm.stackTop--
```

- [ ] **Step 3: Emissão no compilador**

Em `compiler.go:930`, ao lado de `isInt`:

```go
isFloat := false
if leftType != nil && rightType != nil {
	if leftType.String() == "float" && rightType.String() == "float" {
		isFloat = true
	}
}
```

E em cada case do switch de operador (940-1018), transformar o if binário em cadeia — exemplo para `+` (repetir o padrão para `-`, `*`, `/`, `>`, `<`; `==`/`!=`/`>=`/`<=` float ficam no genérico):

```go
case "+":
	if isInt {
		c.emitByte(byte(chunk.OP_ADD_INT))
	} else if isFloat {
		c.emitByte(byte(chunk.OP_ADD_FLOAT))
	} else {
		c.emitByte(byte(chunk.OP_ADD))
	}
```

- [ ] **Step 4: Suíte + corpus + medida mandelbrot**

```powershell
go vet ./... && go test ./... -count=1
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
go build -o noxy_perf.exe ./cmd/noxy
1..5 | ForEach-Object { Measure-Command { .\noxy_develop.exe benchmarks\cross_runtime\mandelbrot.nx } | Select -Expand TotalMilliseconds; Measure-Command { .\noxy_perf.exe benchmarks\cross_runtime\mandelbrot.nx } | Select -Expand TotalMilliseconds }
```

- [ ] **Step 5: Commit**

```powershell
git add internal/chunk/chunk.go internal/compiler/compiler.go internal/vm/executor.go internal/vm/float_ops_test.go
git commit -m "perf(vm,compiler): opcodes _FLOAT especializados para aritmetica e comparacao"
```

---

### Task 8: Rodada A/B completa, gates e registro

**Files:**
- Modify: `benchmarks/RESULTS.md` (seção nova no topo)
- Modify: `benchmarks/cross_runtime/results/cross_runtime.md` (rerun)
- Modify: `docs/superpowers/specs/2026-08-17-vm-perf-static-typing-research.md` (status + pprof pós)

**Interfaces:**
- Consumes: todas as tasks anteriores commitadas.
- Produces: números finais registrados; decisão de merge.

- [ ] **Step 1: Verificação completa**

```powershell
go vet ./... && go test ./... -count=1
go test ./internal/value -race -count=1
go test ./internal/vm -race -count=1
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

Tudo verde, corpus 164/164 — anotar os números reais do output.

- [ ] **Step 2: A/B intercalado nos benches CoW (gates)**

```powershell
go build -o noxy_perf.exe ./cmd/noxy
# usar o protocolo intercalado do repo (mediana de 9), binários develop x perf:
.\benchmarks\interleaved_compare.ps1 -BaselineExe .\noxy_develop.exe -CandidateExe .\noxy_perf.exe -Runs 9
```

(Se os parâmetros do script diferirem, abrir `benchmarks/interleaved_compare.ps1` e usar a invocação que `benchmarks/RESULTS.md` documenta na seção "Reprodução".) Gates: `bench_typed_call_map`, `bench_share_mutate`, `bench_call_light`, `bench_conway` ≤ +5%. Se algum estourar, fazer bissecção por task (`git stash`/checkout parcial) e corrigir antes de seguir.

- [ ] **Step 3: Cross-runtime + pprof pós**

```powershell
.\benchmarks\cross_runtime\run_cross_runtime.ps1
.\noxy_perf.exe --cpuprofile=fib_pos.prof benchmarks\cross_runtime\fib.nx
go tool pprof -top -nodecount=15 noxy_perf.exe fib_pos.prof
```

- [ ] **Step 4: Registrar**

Nova seção no TOPO de `benchmarks/RESULTS.md` seguindo o formato das existentes (`## develop (<hash>) × perf/vm-dispatch-fase1`, tabela bench × antes × depois × delta × veredito, subseções "Perfil de cada bench" quando o delta surpreender e "Interpretação"). Atualizar a tabela do cross-runtime em `benchmarks/cross_runtime/results/cross_runtime.md` com a data nova. Na spec de pesquisa: marcar fases 0–3 como implementadas, colar o pprof pós, e listar o que o novo profile aponta como próximo alvo (candidatos: structs por índice, maps tipados, layout do Value — planos futuros).

- [ ] **Step 5: Commit final**

```powershell
git add benchmarks/RESULTS.md benchmarks/cross_runtime/results/cross_runtime.md docs/superpowers/specs/2026-08-17-vm-perf-static-typing-research.md
git commit -m "bench: rodada A/B da fase 1 de perf (globais, chamadas, opcodes fundidos)"
```

Depois, seguir a skill `superpowers:finishing-a-development-branch` (PR para `develop` com o template de 3 seções do usuário: Summary / Components / Test Plan).

---

## Self-Review (executada na escrita do plano)

1. **Cobertura da spec:** fases 0–3 da pesquisa ↔ Tasks 1–8 (fase 0→Task 1; fase 1→Task 2; fase 2→Tasks 3–4; fase 3→Tasks 5–7; gates→Task 8). Fases 4–6 explicitamente fora do escopo, para planos próprios — decisão de Scope Check, registrada no header.
2. **Placeholders:** nenhum "TBD"/"similar à task N"; todos os steps de código têm o código. Dois pontos dependem de leitura no local (helper exato do disassembler; nome do campo em `unwind.go` linha 71 se estiver dentro de outra função) e estão marcados com a instrução exata de espelhamento e o invariante a preservar.
3. **Consistência de tipos:** `GlobalCacheEntry`/`GlobalCache()`/`Generation()` usados na Task 2 e no case de `OP_CALL_STATIC` da Task 3 (`gcache = c.GlobalCache()`) conferem; `emitCall(argCount, emission, static)` atualizado em todos os call sites nomeados; ordem de append dos opcodes fixada (Task 3 → 5 → 6 → 7) e respeitada nas instruções "APPEND após X".

## Revisão externa (2026-08-18)

Revisor independente (agente sem contexto desta sessão, time box de 15 min) validou as âncoras e os designs de maior risco e devolveu 3 achados IMPORTANTES + 3 MENORES — **todos aplicados neste texto**: (1) a limpeza de `Owned` movida para `unwind.go:61-64` (o `frame.Owned = nil` da linha 64 anularia a meta de zero-alloc da Task 4); (2) cadeia de soundness do `OP_CALL_STATIC` documentada no preâmbulo da Task 3 e no comentário de `callValueStatic`, com teste novo de rebind de função de mesmo tipo; (3) `TestGlobalCacheKeyedByEnvironment` reescrito para rodar o mesmo chunk sob dois ambientes de verdade; (4) limitação de thrash da entrada única entre tasks anotada na Task 2; (5) build do baseline `noxy_develop.exe` adicionado ao Step 1 da Task 1; (6) nota sobre profile órfão em erro de runtime. Confirmações do revisor: todos os funis de escrita de globais bumpariam a geração (incl. `ExportMap` e `REF_GLOBAL`), interleavings do gen/Resolve nunca servem stale, rollback do `TruncateTo` seguro nos cenários analisados (jumps de `&&`/`||` internos à região truncada), pointer-identity de frames sound com array de valores, gates da Task 8 batem com o protocolo real. Pendências que o time box não cobriu (verificar durante a execução das tasks correspondentes): tipo estático de membro de módulo (Task 3 — se compilar como `FunctionType` exato, adicionar teste com módulo), idempotência de `resolveUpvalue` sob recompilação pós-rollback com lambda no operando esquerdo (Task 5 — cobrir com um teste se o caso compilar).
