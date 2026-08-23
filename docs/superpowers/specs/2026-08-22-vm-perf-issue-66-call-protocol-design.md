# VM Perf — Protocolo de chamada (issue #66, item 3)

**Data:** 2026-08-22 · **Issue:** [#66](https://github.com/estevaofon/noxy/issues/66) item 3 · **Branch:** `perf/issue-66-call-protocol` · **Base:** `develop` (c1cc12a, v0.15.1)

## 1. Contexto (medido nesta sessão, binário de c1cc12a, `fib(34)`, 1,25 s de amostras)

| componente | flat | cum | o que é |
|---|---|---|---|
| `(*VM).run` | 40,8 % | 99 % | despacho: 15 opcodes por chamada não-folha de `fib` |
| `finishFrame` + `finalizeCurrentFrame` | 15,2 + 10,4 % | **26,4 %** | o retorno: `frameOutcome` (48 B) copiado na ida e na volta, laço de `Deferred`, guard de upvalues, laço de `Owned`, zerar slots, `push` — em `fib` **tudo vazio** |
| `callValueStatic` → `callPreparedClosure` → `ownSlot` | 4,8 + 2,4 + 3,2 % | **12,0 %** | a chamada: duas chamadas Go reais + `ownSlot` por parâmetro (para `int`, só descobre que não há o que reter) |
| `push` / `pop` | 15,2 / 4,0 % | | |
| `GlobalCache` | 1,6 % | | lookup de `fib` já cacheado — barato |
| `ensureCallCapacity` | 0,8 % | | inlinado (custo 80 exato) |

Bytecode de `fib` (não-folha): `GET_LOCAL 1 · CONSTANT 1 · JUMP_IF_GT_INT · GET_GLOBAL fib · GET_LOCAL 1 · CONSTANT 1 · SUB_INT · CALL_STATIC 1 · GET_GLOBAL fib · GET_LOCAL 1 · CONSTANT 2 · SUB_INT · CALL_STATIC 1 · ADD_INT · RETURN`.

## 2. Objetivo e não-objetivos

**Objetivo:** tirar do caminho quente de chamada/retorno de funções com frame "simples" (sem `defer`, sem upvalue aberto, sem vínculo RC) as duas chamadas Go, a cópia de `frameOutcome` e o laço `ownSlot`; e reduzir o número de despachos das expressões que todo laço/chamada tem (`local ± K`, `local OP local`). Semântica, saída, mensagens de erro, linhas de erro e contagem RC **idênticas**; opcodes só por append.

**Não-objetivos:** pular o lookup de global do callee com operando de constante (o "(a) literal" da issue — `GlobalCache` é 1,6 % e exigiria o callee embaixo dos args ou mudar o layout do frame); fusão `GET_LOCAL+CONST+JUMP_IF_*` (follow-up se o número pedir); mexer em `OP_CALL` genérico, natives, struct constructors, `defer`, `spawn`; `Owned` ou RC.

## 3. Desenho

### 3.1 (d) `OP_RETURN` escalar — fast path inline no handler

No handler de `OP_RETURN`, **antes** de `finishFrame`:

```
if len(frame.Deferred) == 0 && len(frame.Owned) == 0 && vm.openUpvalues == nil && vm.frameCount-1 >= minFrameCount {
    result := vm.pop()
    for i := frame.StackBase; i < vm.stackTop; i++ { vm.stack[i] = value.Value{} }
    frame.Closure = nil; frame.Environment = nil
    vm.frameCount--
    vm.stackTop = frame.StackBase
    vm.currentFrame = &vm.frames[vm.frameCount-1]
    vm.push(result)
    frame = vm.currentFrame; c = ...; gcache = ...; ip = frame.IP
    continue
}
```

É exatamente o que `finalizeCurrentFrame` faria com esses quatro guardas verdadeiros (os três laços vazios/pulados, o `if frameCount == 0` impossível porque `frameCount-1 >= minFrameCount >= 1`), sem a cópia dupla de `frameOutcome` e sem as duas chamadas. `vm.openUpvalues == nil` é conservador (qualquer upvalue aberto em qualquer frame → caminho lento). Nenhum opcode novo.

### 3.2 (a)+(b) `OP_CALL_STATIC` — fast path inline + `ParamsUntracked`

**Flag no `ObjFunction`:** `ParamsUntracked bool`, calculada em `compileFunction`: verdadeira sse todo parâmetro é sem `ref` e de tipo `int`, `float`, `bool`, `string` ou `bytes` (valores que nunca têm contador RC — `Retain` é no-op; ver `value.NeverTracked`). Cache de módulos é em memória (`moduleCache`), sem serialização — campo novo não muda formato nenhum.

**Handler de `OP_CALL_STATIC`**, antes de `callValueStatic`:

```
callee := vm.stack[vm.stackTop-argCount-1]
if callee.Type == value.VAL_FUNCTION {
    if closure, ok := callee.Obj.(*value.ObjClosure); ok && closure.Function.ParamsUntracked && argCount == closure.Function.Arity {
        if vm.frameCount == len(vm.frames) || len(vm.stack)-vm.stackTop < stackReserve {   // = ensureCallCapacity, escrito a mao
            if err := vm.growForCall(c, ip); err != nil { return err }
        }
        frame.IP = ip
        nf := &vm.frames[vm.frameCount]
        nf.Closure = closure; nf.IP = 0; nf.StackBase = vm.stackTop-argCount-1; nf.LocalBase = nf.StackBase
        nf.Environment = closure.Environment; nf.Deferred = nf.Deferred[:0]; nf.Owned = nf.Owned[:0]
        vm.frameCount++; vm.currentFrame = nf
        frame = nf; c = ...; gcache = ...; ip = 0
        continue
    }
}
// caminho atual: frame.IP = ip; callValueStatic(...)
```

Igual a `callPreparedClosure` menos o laço `ownSlot` — que com `ParamsUntracked` só faria `Retain` no-op em cada argumento (e, como `Owned` está vazio e o ocupante não é retível, nem entrada removeria). Aridade errada, native, struct, `any` → caminho atual, mesmas mensagens. `ensureCallCapacity` custa 80 e não cabe em `run()` (orçamento 20) — por isso a condição é copiada; `growForCall` continua `//go:noinline` e é o único dono das mensagens de overflow.

### 3.3 (c) Superinstruções (append ao fim de `internal/chunk/chunk.go`)

| opcode | operandos | pilha | emitido quando |
|---|---|---|---|
| `OP_GET_LOCAL_ADD_IMM_INT` | `[slot u8][imm i8]` | `[] → [local+imm]` | infix `+`/`-` com **esquerda** identificador resolvido por `resolveLocal` de tipo `PrimitiveType int` e **direita** `IntegerLiteral` com `±K ∈ [-128,127]` (sinal já aplicado) |
| `OP_GET_LOCAL_2` | `[a u8][b u8]` | `[] → [local a, local b]` | os dois operandos de um infix (ou da condição fundida de `tryCompileFusedCondition`) são identificadores resolvidos por `resolveLocal`, ambos de tipo `PrimitiveType` (nunca `ref`, nunca upvalue/global) |

Emissão **em nível de AST** (helpers `tryEmitLocalAddImm` / `tryEmitLocalPair`, sem emissão especulativa nem peephole — não existe alvo de salto no meio de uma expressão). Depois de `OP_GET_LOCAL_2` o compilador segue exatamente como se tivesse compilado os dois operandos (tipos de `resolveLocal`), emitindo o operador de sempre. Overflow de `local+imm` wrappa como `OP_ADD_INT`. Semântica idêntica: os dois são apenas `OP_GET_LOCAL`(+`OP_CONSTANT`+`OP_ADD_INT`) fundidos.

### 3.4 O que NÃO muda

`callValue`/`callValueStatic`/`callPreparedClosure`/`finishFrame`/`finalizeCurrentFrame` (continuam sendo os caminhos lentos e os únicos com `defer`/upvalue/`Owned`); `OP_CALL`; RC (`Owned` vazio ⇒ nada a soltar; `ParamsUntracked` ⇒ nada a reter); mensagens de erro (aridade, overflow de frames/operandos, "can only call functions"); `OP_INC_LOCAL_INT`; layout do frame.

## 4. Invariantes e guards executáveis

- **Nenhum helper novo chamado de `run()`**; tudo é código inline de handler. Guards existentes (`push` 20, `pop` 18, `Retain` 67, `Release` 80, `NeverTracked`, `arrayTagIsRefSlot` 20, `ensureCallCapacity` 80, `isASCII` 23) continuam verdes.
- Compilador: `ParamsUntracked` true para `(n: int)`, `(s: string, b: bytes, f: float, ok: bool)`; false para `(a: int[])`, `(x: any)`, `(r: ref int)`, `(p: Ponto)`, `(f: func(int) -> int)`; zero params → true.
- Emissão: `OP_GET_LOCAL_ADD_IMM_INT` presente em `n - 1`/`i + 2` (local int), **ausente** para global, upvalue, `ref int`, float, `K` fora de i8, `1 + n` (literal à esquerda); `OP_GET_LOCAL_2` presente em `a + b`/`i < n` (dois locais int) e na condição de `while`, ausente com upvalue/global/`ref`.
- E2E: `fib(20)` = 6765, `fib(1)`, função com `defer` retornando, closure capturando local (upvalue aberto no retorno), função com parâmetro `int[]` mutado depois da chamada (CoW intacto — caminho lento de chamada), recursão profunda → mesma mensagem `stack overflow: call depth exceeds N frames`, aridade errada via `any` → mesma mensagem; testes de RC existentes (`TestParamRetainReleasedAfterReturn`, etc.).
- `go test ./...`, `-race` value/vm, corpus 0 falhas, `compare_examples.ps1` 0 divergentes, gates CoW ≤ +5 %, sentinela `bench_generic_vs_hand`.

## 5. Medição (protocolo de `benchmarks/RESULTS.md`)

Binários em disco local: `noxy_base.exe` (c1cc12a) · `s0` = +3.1 (return) · `s1` = +3.2 (call) · `s2` = +3.3 (superinstruções) = head. `interleaved_compare.ps1 -Runs 9` base × head; A/B por estágio (4 binários, 11 intercaladas) em `cross_runtime/fib.nx` e `loop_arith.nx`; `run_cross_runtime.ps1 -NoxyBaseline` (mín. de 9); `BenchmarkNoxyCallOverhead` base × head (`go test -bench`, `-count=10`); perfil de `fib(34)` antes/depois. pwsh 7 instalado (7.6.5): scripts rodam direto.

**Meta (hipótese):** `fib` 2,0x → ~1,5x do CPython nesta máquina; `BenchmarkNoxyCallOverhead` ≥ −25 %.

## 6. Riscos

- **Codegen de `run()`** (lição do item 1: +12 % num bench sem relação): cada estágio é medido contra o sentinela; estágio que regrida o sentinela é desfeito e registrado.
- Fast path de retorno e `minFrameCount`: um native que reentra a VM (`invokePreparedCall`, `spawn`) roda `run(minFrameCount=k)` — o guard `frameCount-1 >= minFrameCount` garante que o retorno que encerra esse `run` continua pelo caminho lento, que é quem devolve `terminalResult`.
- `ParamsUntracked` e fronteira dinâmica: se um valor composto chegar num parâmetro `int` (impossível com o checker; via `any` o tipo do param seria `any` → flag false), o `Retain` pulado seria um vazamento, não corrupção — e `callValueStatic` já pulava `validateParameterModes` com a mesma confiança no checker.

## 7. Decisões tomadas sem consulta (para a review)

- Ordem dos estágios: (d) → (a/b) → (c), pelo tamanho no perfil.
- `OP_GET_LOCAL_2` também na condição fundida (`while i < n`) — é onde `loop_arith`/`bubblesort` passam.
- Imediato i8 (não índice de constante) em `OP_GET_LOCAL_ADD_IMM_INT`, espelhando `OP_INC_LOCAL_INT`.
