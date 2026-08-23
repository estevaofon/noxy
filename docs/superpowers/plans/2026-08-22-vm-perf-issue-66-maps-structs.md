# VM Perf — Maps e structs (issue #66, item 4; #40 item 1) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cachear o schema validado do construtor de struct (+ `NewInstance` pré-dimensionado) e tirar de `m[k]`/`m[k]=v`/`has_key`/`length` o segundo lock, o re-boxing da chave e a cópia para contar — sem mudar semântica, mensagens ou RC — e medir por estágio.

**Architecture:** s0 = `ObjStruct.ctorCache atomic.Pointer[ValidatedCtor]` consultado por `validStructConstructorType`, `validateStructConstructorArguments` usa os `ParamInfo` prontos; `NewInstance`/`copyValue` com map pré-dimensionado. s1 = `bindingStore.swap`/`count`, `ObjMap.Swap`/`Len`, chave `indexVal.Obj` reaproveitada. s2 (condicional) = construtor via `OP_CALL_STATIC` sem validação por argumento.

**Tech Stack:** Go 1.26, pwsh 7.6, `go test -race`, `noxy --cpuprofile`.

**Spec:** `docs/superpowers/specs/2026-08-22-vm-perf-issue-66-maps-structs-design.md`

## Global Constraints

- Branch `perf/issue-66-maps-structs` (base `perf/issue-66-call-protocol` 4ac27f5), worktree `.claude/worktrees/perf-issue-66-maps-structs`. Um commit por task; Tasks 2–3 = s0, Task 4 = s1.
- Semântica, saída, mensagens e momento de erro idênticos; RC idêntico; nenhum opcode novo.
- Repo CRLF (Edit tool; `git diff --numstat`). Binários em `$S\bench\` (`noxy_call.exe` = 4ac27f5 é a base desta rodada → copiar para `noxy_base4.exe`). Máquina sem `go test`/build durante medições.

---

### Task 1: `benchmarks/bench_struct_records.nx`

- [ ] Copiar `struct_records.nx` (scratchpad) para `benchmarks/bench_struct_records.nx` com cabeçalho no padrão dos outros (`// ...` + `CHECKSUM:`); `go run ./cmd/noxy benchmarks/bench_struct_records.nx` → `CHECKSUM:6089998`.
- [ ] Commit `bench: bench_struct_records.nx — struct de 5 campos construida em laco, por valor (issue #40 Test Plan)`.

### Task 2: Cache do schema validado (s0, parte 1)

**Files:** Modify `internal/value/value.go` (`ObjStruct` + `ValidatedCtor`); Modify `internal/vm/runtime_type_validation.go` (`validStructConstructorType`, `validateStructConstructorArguments`); Create `internal/vm/struct_ctor_cache_test.go`.

- [ ] **Teste** (mesma mensagem na 1ª e 2ª construção mal-tipada; struct sem ConstructorType continua falhando; construção válida repetida): 

```go
func TestStructConstructorErrorsUnchangedWithCache(t *testing.T) {
	src := "struct P\n    x: int\nend\nlet a: any = \"s\"\nlet i: int = 0\nwhile i < 2 do\n    let p: P = P(a)\n    i = i + 1\nend\n"
	err := interpretVMSource(t, New(), src)
	if err == nil || !strings.Contains(err.Error(), "function 'P' argument 1: expected int, got string") { t.Fatalf("err = %v", err) }
}
func TestStructConstructorCacheKeepsResults(t *testing.T) {
	got := semArray(t, captureVMSource(t, "struct P\n    x: int\n    y: string\nend\nlet i: int = 0\nlet s: int = 0\nwhile i < 1000 do\n    let p: P = P(i, \"a\")\n    s = s + p.x\n    i = i + 1\nend\ntest_report([s])\n"))
	if got[0].Int() != 499500 { t.Fatalf("got %s", got[0].String()) }
}
```
(+ checar em `calls_characterization_test.go:117` que o erro `Box` continua igual — já existe.)
- [ ] Implementar §3.1 da spec; `go test ./internal/vm ./internal/value`; `go test -race ./internal/vm -run 'Struct|Spawn|Task'`.
- [ ] Commit `perf(vm,value): cache do schema validado do construtor de struct em ObjStruct (atomic.Pointer) — sem make(map)/walk/String() por construcao (issue #40 item 1, #66 item 4, s0)`.

### Task 3: `NewInstance`/`copyValue` pré-dimensionados (s0, parte 2)

- [ ] `value.go` `NewInstance`: `make(map[string]Value, len(def.Fields))`; `calls.go` `copyValue` instância: `make(map[string]Value, len(obj.Fields))`. `go test ./internal/vm ./internal/value`.
- [ ] Commit `perf(value,vm): map de campos de instancia pre-dimensionado em NewInstance e no clone CoW (issue #66, item 4, s0)`.

### Task 4: Maps — `Swap`, chave sem re-boxing, `Len` sem cópia (s1)

**Files:** Modify `internal/value/map.go` (`swap`, `count`, `ObjMap.Swap`, `ObjMap.Len`); Modify `internal/vm/index_ops.go` (get/set map), `internal/vm/builtins_collections.go` (`has_key`, `delete`, outros `key = str`); Create `internal/vm/map_fastpath_test.go`.

- [ ] **Teste**: `m[k] = v` sobre chave existente com composto libera o velho uma vez (contagem via `Owners` de um array guardado no map, substituído e depois o original ainda usado — `IsShared` false após a substituição); `length(m)` antes/depois de `delete`; chave string/int round-trip; `has_key`.
- [ ] Implementar §3.3; `go test ./internal/vm ./internal/value`; `-race` idem.
- [ ] Commit `perf(value,vm): map set com Swap sob um lock, chave string sem re-boxing, Len sem Snapshot (issue #66, item 4, s1)`.

### Task 5: (condicional) construtor via `OP_CALL_STATIC` sem validação — ler `compileCallExpression` (modesProven/isExact para construtor) antes; só se sound e se o perfil pós-s0 ainda mostrar a validação por argumento. Commit separado (s2).

### Task 6: Verificação + medição + relatório (raw `benchmarks/results/2026-08-22-issue-66-maps-structs-raw.md`, seção em `RESULTS.md`). Task 7: bump v0.15.3 + CHANGELOG. Task 8: PR (base `perf/issue-66-call-protocol` → `develop` após #69), comentário na #66 e na #40.
