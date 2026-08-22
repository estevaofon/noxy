# VM Perf Fase 2 — Layout do `Value` e header comum dos compostos (issue #37, estágios 1 e 2)

**Data:** 2026-08-22 · **Issue:** [#37](https://github.com/estevaofon/noxy/issues/37) · **Branch:** `perf/issue-37-value-layout` · **Base:** `develop` (cb8efcb, v0.14.2)

## 1. Contexto

Depois da fase 1 de perf (#36, v0.6.0), o perfil de `fib` mostra `push`+`pop` com
24,4 % do tempo e `value.Value` com **48 bytes** (tag `int` de 8 B, `bool` com
7 B de padding, `int64` e `float64` mutuamente exclusivos em campos separados,
`interface{}` de 16 B). A pilha de 2048 slots ocupa 96 KB. `ownersOf`
(`internal/value/cow.go`), chamado por todo `Retain`/`Release`/`IsShared`, faz
type switch sobre `interface{}` — no assembly atual: carrega o hash do tipo
dinâmico, compara com até três constantes e depois o ponteiro do tipo.

A issue propõe três estágios; os comentários fixam a ordem **1 → 2 → 3** e
condicionam o 3 (`unsafe.Pointer`, 24 B) a números dos dois primeiros. Este
documento cobre **só os estágios 1 e 2** mais o "extra barato" do `pop`.

Achado de baseline desta sessão (não estava na issue): `pop()` custa **22** no
inliner e, como `run()` é "big function" (orçamento 20), **não é inlinada em
nenhum dos ~84 call sites de `executor.go`** — toda `vm.pop()` dentro de
`run()` é chamada real. `push` custa 20 e é inlinada em 113 sites
(`inline_guard_test.go` trava isso).

## 2. Objetivo e não-objetivos

**Objetivo:** reduzir o custo estrutural do interpretador (cópia de `Value`,
pressão de cache da pilha, `ownersOf`) sem mudar semântica, bytecode, saída
ou mensagens de erro. Hipótese calibrada (comentário 2 da issue): `fib`
**15–25 % melhor**, não "alcançar o CPython".

**Não-objetivos:** estágio 3 (`unsafe.Pointer`, boxing de string); mudar o
despacho; mudar a contagem ou os funis de retain/release (spec CoW-RC §4.2);
reordenar/renumerar tags `ValueType` ou opcodes.

## 3. Desenho

### 3.1 Estágio 1 — `Value` de 48 → 32 bytes

```go
type ValueType uint8

type Value struct {
	Type ValueType   // offset 0
	kind objKind     // offset 1 — dica para ownersOf (§3.2); 0 = desconhecido
	b    bool        // offset 2 — valor de VAL_BOOL (no padding; sem branch)
	num  uint64      // offset 8 — int64 (VAL_INT) | bits de float64 (VAL_FLOAT)
	Obj  interface{} // offset 16
}                    // unsafe.Sizeof == 32
```

- **API:** `v.Int() int64`, `v.Float() float64`, `v.Bool() bool` (leitura);
  `(*Value).SetInt(int64)` para o único escritor in-place do repo
  (`OP_INC_LOCAL_INT`, `executor.go`: `AsInt += delta`). Construtores
  `NewInt/NewFloat/NewBool` inalterados na assinatura.
- **Blast radius (medido, fora de `.claude/worktrees`):** `.AsInt` 323,
  `.AsFloat` 55, `.AsBool` 49 leituras; 1 escrita; 3 literais (os próprios
  construtores). Rename por script (Python, CRLF preservado), não à mão.
- **Mudança de semântica dos acessores em tipo errado:** antes, `v.AsInt` de
  um `VAL_FLOAT` era 0 (campo separado); agora `v.Int()` devolve os bits do
  float. Todo site auditado precisa estar sob guarda de `Type` (os de
  `executor.go`, `builtins_core.go`, `json_strict.go`, `plugin.go`, `stack.go`
  já estão — verificado na exploração). Auditoria semiautomática: listar
  leituras sem `VAL_*`/`Type`/`switch` nas ±4 linhas e revisar à mão.
- **Zero value** preservado: `Value{}` = `VAL_BOOL false`, como hoje.
- `Value` não é serializado em lugar nenhum (plugins falam JSON; cache de
  módulos guarda `Value` em memória) e não é comparado com `==`/`DeepEqual`
  — campos não exportados são seguros.

### 3.2 Estágio 2 — header comum e `ownersOf` sem type switch no caminho comum

```go
// ObjHeader é o prefixo comum dos compostos rastreados por RC. Tem de ser o
// PRIMEIRO campo (offset 0) — layout travado por teste.
type ObjHeader struct {
	Owners atomic.Int32
}

type ObjArray struct { ObjHeader; Elements []Value; RuntimeType atomic.Pointer[RuntimeTypeInfo] }
type ObjMap struct   { ObjHeader; store *bindingStore; storeOnce sync.Once; RuntimeType ... }
type ObjInstance struct { ObjHeader; Struct *ObjStruct; Fields map[string]Value }
```

`kind` (byte de `Value`) é **dica carimbada pelos construtores** de
`internal/value`:

| construtor | kind |
|---|---|
| `NewArray`, `NewArrayAdopting` | `objKindArray` |
| `NewMap`, `NewMapWithData`, view do `GlobalEnvironment` | `objKindMap` |
| `NewInstance`, `NewInstanceWith` | `objKindInstance` |
| `NewString`, `NewStruct`, `NewRuntimeTypeInfo` | `objKindNoOwners` |
| qualquer outro (`Value{Type: VAL_OBJ, Obj: x}` fora do pacote, escalares) | `0` = desconhecido |

```go
func ownersOf(v Value) *atomic.Int32 {
	switch v.kind {
	case objKindArray:    return &v.Obj.(*ObjArray).Owners     // 1 cmp de ponteiro de tipo
	case objKindMap:      return &v.Obj.(*ObjMap).Owners
	case objKindInstance: return &v.Obj.(*ObjInstance).Owners
	case objKindNoOwners: return nil
	}
	return ownersOfSlow(v) // o type switch de hoje: Type != VAL_OBJ → nil; array/map/instance
}
```

- **Correto por construção:** `kind == 0` cai no caminho de hoje. Os ~30
  sites `value.Value{Type: value.VAL_OBJ, Obj: inst}` (io/sqlite/sys/json,
  `calls.go:198`) continuam corretos, só não aceleram; o de `calls.go` (clone
  CoW de instância, semi-quente) passa a usar construtor. Um site novo que
  esqueça o carimbo **não** produz under-count de RC — só perde a aceleração.
- **Sem `unsafe`** no caminho quente. A variante "cast direto para
  `*ObjHeader` via data word do eface" economizaria uma comparação de
  ponteiro; é medida em microbench Go (§5) e registrada, não embarcada —
  critério de confinamento do comentário 2 da issue (unsafe só com ganho
  mensurável e atrás de asserção executável).
- `Retain` (custo 64) e `Release` (78) hoje são inlinados em 25/13 sites de
  `internal/vm` (orçamento 80 fora de `run()`); `ownersOf` novo não pode
  estourar isso — guard em `inline_guard_test.go`.

### 3.3 Extra — `pop()` inlinável

`vm.stack[vm.stackTop] = value.Value{}` vira `vm.stack[vm.stackTop].Obj = nil`:
só o ponteiro interessa ao GC; `Type/num/b` mortos acima de `stackTop` são
inertes (os fused jumps já leem slots acima do topo logo após `stackTop -= 2`,
sem limpar). Meta: custo ≤ 20 → inlinada nos ~84 sites de `run()`. Guard:
`inline_guard_test.go` passa a travar `pop` como trava `push`.

Commit separado e medido à parte — o usuário pediu estágios 1 e 2; este item
é o "extra barato, independente dos três" da issue, promovido a "primeiro item
medido" no comentário 3. Reversível com um `git revert`.

## 4. Invariantes e guards executáveis

- `unsafe.Sizeof(Value{}) == 32`; `unsafe.Offsetof(ObjArray{}.ObjHeader) == 0`
  (idem map/instance) — `internal/value/layout_test.go`.
- `ownersOf(NewArray(...)) == &arr.Owners` e idem para map/instance e para o
  caminho lento (`Value{Type: VAL_OBJ, Obj: arr}` sem carimbo) —
  `internal/value/owners_test.go`.
- `push` custo ≤ 20 e ≥ 100 sites inlinados em `executor.go` (já existe);
  `pop` idem (novo); `Retain`/`Release` custo ≤ 80 (novo).
- `go test ./...` verde; `go test -race ./internal/value ./internal/vm` verde
  (`Owners` segue `atomic.Int32` — tasks paralelas).
- Corpus `noxy_examples/` sem falhas (`run_all_tests_concurrent.nx`) e
  **diff de saída ponta a ponta** base × head (`benchmarks/compare_examples.ps1`).

## 5. Medição

Binários em disco local (scratchpad): `noxy_base.exe` (develop cb8efcb),
`noxy_s1.exe`, `noxy_s12.exe`, `noxy_s12p.exe` (pop). Máquina o mais ociosa
possível; carga registrada no início.

1. `benchmarks/interleaved_compare.ps1 -Runs 9` base × final (headline, mesmo
   protocolo de `RESULTS.md`), mais uma intercalação dos quatro binários na
   mesma janela para isolar cada estágio (script de sessão; dados brutos em
   `benchmarks/results/2026-08-22-issue-37-value-layout-raw.md`).
2. `benchmarks/cross_runtime/run_cross_runtime.ps1 -NoxyBaseline` base × final
   (`fib`, `loop_arith`, `bubblesort`, `string_ops`, `mandelbrot`, `map_churn`,
   `startup`; mínimo de 9).
3. `go test ./internal/vm -run '^$' -bench NoxyCallOverhead -count 10` por estágio.
4. Perfil de `fib` (`noxy --cpuprofile`) base × final: share de `push`/`pop`/
   `ownersOf` (a métrica da issue).
5. Microbench Go de `ownersOf`: type switch × kind+assert × kind+unsafe (registro).

Gates: `bench_typed_call_map`, `bench_share_mutate`, `bench_call_light`,
`bench_conway` ≤ +5 % (os três primeiros estão no piso de processo, ~100 ms
— delta deles é citado, não decide; ver RESULTS.md 2026-08-22).

## 6. Riscos

| risco | mitigação |
|---|---|
| leitura de campo errado passava por zero e agora devolve lixo | auditoria de guardas (§3.1) + suíte + diff de saída do corpus |
| `push`/`pop`/`Retain`/`Release` saírem do inline sem aviso | guards no `inline_guard_test.go` |
| `kind` errado num construtor → RC rastreia o objeto errado | só construtores do pacote carimbam; teste por construtor em `owners_test.go`; `kind` errado é impossível fora do pacote (campo não exportado) |
| ganho não se materializar | é hipótese a validar; o relatório publica os números como estão |

## 7. Decisões tomadas sem consulta (para a review)

1. Ordem 1 → 2 → pop (comentários 1 e 3 da issue).
2. Bool no próprio byte do padding, não dentro de `num` (evita branch em `NewBool`).
3. `kind` como dica de 3 estados + tipo, caminho lento preservado; **sem `unsafe`**.
4. `pop` incluído como commit separado (fora do pedido literal "1 e 2").
5. Versão **v0.14.3** (patch: perf interna, sem mudança de linguagem —
   regra "minor só para mudanças maiores").
