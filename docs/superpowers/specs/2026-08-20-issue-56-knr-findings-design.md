# Achados do K&R em Noxy (issue #56) — Design v0.11.0

> Spec da entrega dos 16 itens de [estevaofon/noxy#56](https://github.com/estevaofon/noxy/issues/56)
> (relatório de validação da VM 0.10.0 ao reescrever o K&R em Noxy). Todos os itens foram
> **reproduzidos em `develop` `691d902` (v0.10.1)** em 2026-08-20 antes deste design —
> o único que se revelou diferente do relatado é o 7 (ver §7). A issue continua sendo a
> fonte de verdade para o *sintoma*; este documento fixa a *solução* escolhida.
> Decisões tomadas com o autor em 2026-08-20: item 3 → `io.stdin()` + `input()` corrigido;
> item 8b → erro de compilação; item 14 → erro + escapes `{{ }}`; entrega → um único
> branch/PR **v0.11.0** (com quebras documentadas).
> Revisão independente (2026-08-20, timebox 15 min) incorporada: `break`/`continue` fecham
> upvalues (§9), `finalizeCurrentFrame` por índice (§1), `Relocate` sob `mu` (§1), reserva de
> pilha na entrada do frame + mensagens distintas (§1), `*` em tipo desconhecido (§16, afirmação
> anterior estava errada), `typesEquivalent` em cinco sites (§8c), sub-achados do 14 e contrato
> de `input()`/`read` explicitados (§3, §14, Fora de escopo).

---

## Summary

Um branch `fix/issue-56-knr-findings` (base `develop`), um commit por item (TDD), versão
**v0.11.0** porque três itens quebram compatibilidade:

- **BREAKING 5** — condição de `if`/`elif`/`while` (e operandos de `!`, `&&`, `||`) tem de ser `bool`: erro de compilação quando o tipo estático é conhecido e não é `bool`, erro de runtime para `any` não-booleano. Hoje `if 0`/`if ""`/`if null` entram no `then` em silêncio.
- **BREAKING 6** — diagnósticos da VM/CLI (erros de parser, compilador, runtime, hints, "Error reading file", erros de thread/plugin) vão para **stderr**; `print` continua em stdout. Novos builtins `eprint`/`eiprint`.
- **BREAKING 12** — `io.read_lines` deixa de devolver um `""` final quando o arquivo termina em `\n`.

Os demais são correções ou adições sem quebra: pilha de frames/operandos crescem sob demanda (1), `let a: T[N]` grande (2), `input()` com stdin redirecionado + `io.stdin()` (3), `io.read_line`/`io.list_dir`/`io.rename` (4), `io.write_bytes` (7), `select`/tipos nominais de módulo (8), `continue` (9), `if c then return end` (10), `1.5e3` (11), linha do erro de atribuição (13), f-string `{{`/`}}` e erro para `:spec` (14), exit code 1 para script inexistente (15), checagens estáticas de `*`/`!`/`~`/bitwise e docs de overflow/`exec_output`/ordem de `map` (16).

## Princípios que guiam as escolhas

1. **Erro em compilação > erro em runtime > silêncio** (AGENTS.md "Safety First") — todo lugar onde o compilador já conhece o tipo estático passa a recusar o programa errado; o runtime cobre `any`.
2. **Nenhum panic Go chega ao usuário** — limites de recurso viram erro de runtime `stack overflow` com linha do script.
3. **Modelo do C para E/S**: `stdin` é um `io.File` (`io.stdin()`); leitura incremental por `io.read_line` com EOF explícito (`ok=false`, `error="EOF"`); `eprint` é o `fprintf(stderr, …)`.
4. **YAGNI**: nada de format-spec em f-string (já existe `fmt("%6.2f", x)`), nada de alias vivo em `select`, nada de escrita em variável de módulo.

---

## Escopo — item a item

### 1. Profundidade de chamadas — frames e pilha de operandos crescem sob demanda

**Hoje.** `frames [FramesMax=64]CallFrame` e `stack [StackMax=2048]value.Value` são arrays fixos embutidos em `VM` (`internal/vm/vm.go:66,82`); `call`/`callPreparedClosure` (`calls.go:121,139`) dão `stack overflow` em 62 chamadas aninhadas; `push` (`stack.go:140`) **panica** em Go ("Stack overflow") quando a pilha de operandos enche. Upvalues abertos guardam `*value.Value` apontando para dentro de `vm.stack` (`executor.go:334,1208`; `ObjUpvalue.location`); `vm.currentFrame` aponta para dentro de `vm.frames`.

**Depois.**

- `frames []CallFrame` e `stack []value.Value` são **slices** com `len == cap` corrente, alocados em `NewWithShared` com `framesInitial = 64` e `stackInitial = 2048` (mesmo custo por VM de hoje — importa porque cada `task`/`spawn` cria um VM, `builtins_tasks.go:29`, `builtins_concurrency.go:33`).
- **Tetos** (constantes exportadas, mantendo os nomes): `FramesMax = 100_000` e `StackMax = 1 << 20` (1 048 576 slots ≈ 48 MB no pior caso). Crescimento por **dobro** até o teto (`min(2*len, Max)`).
- **Entrada de frame é o único ponto onde os tetos são verificados no caminho normal.** `ensureCallCapacity()` roda em `callPreparedClosure` **antes** de `&vm.frames[vm.frameCount]` e faz duas coisas: (a) frames — se `frameCount == len(frames)`: `len == FramesMax` → `runtimeError("stack overflow: call depth exceeds %d frames", FramesMax)`; senão realoca (dobro), copia e **reaponta `vm.currentFrame = &vm.frames[frameCount-1]`**; (b) operandos — garante uma **reserva** de `stackReserve = 256` slots livres acima de `stackTop` (cresce se preciso; se no teto ainda não couber → `runtimeError("stack overflow: operand stack exceeds %d slots", StackMax)`). Os dois textos começam por `stack overflow` (testes existentes casam pelo prefixo) e distinguem recursão de literal gigante. A checagem duplicada de `FramesMax` em `call` (`calls.go:121`) sai — só `callPreparedClosure` verifica. Copiar `CallFrame` por valor preserva os headers de `Owned`/`Deferred` (capacidade reaproveitada — ver comentário atual em `vm.go:57-65`, que é atualizado).
- `push` cresce a pilha quando `stackTop == len(stack)` (dobro até `StackMax`); só no teto **panica com um sentinela tipado** (`errStackOverflow`) que `run()` (`executor.go:46`) recupera com `defer` e converte no erro de runtime `stack overflow: operand stack exceeds …` (posição do frame corrente). Com a reserva na entrada do frame, esse caminho só é alcançável por um **único frame** que empilhe mais do que sobra até o teto de uma vez (literal/expressão com centenas de milhares de temporários) — recursão profunda sempre morre limpa em `ensureCallCapacity`. Só esse sentinela é recuperado; qualquer outro panic continua subindo como hoje. O `call_result` (`builtins_call_result.go:123`) já recupera panics na fronteira — o estouro dentro de `call_result` vira falha capturável (decisão: aceitável, como `RecursionError` em Python; documentar em §7 da spec da linguagem). Risco residual assumido: um native que reentra a VM segurando um mutex sem `defer Unlock` ficaria com o lock preso nesse caminho raro — o plano grepa `invokeBoundaryCall`/`runCallBoundary`/`callValue` nos natives para confirmar que nenhum segura `operationMu`/`fileMetaMu` ao reentrar.
- **Realocação da pilha reaponta os upvalues abertos**: `vm.openUpvalues` é a lista de todos (e só) os `ObjUpvalue` abertos, e todos apontam para `vm.stack`; `growStack` percorre a lista e chama um novo `(*ObjUpvalue).Relocate(old, new []Value)` (pacote `value`) que recalcula `location = &new[idx]` com `idx` derivado do ponteiro antigo — **sob `mu.Lock()`**, como `Store`/`Close` (`value.go:227-252`), porque `Load`/`IsValid`/`PointsTo` leem `location` sob `RLock` e uma task pode segurar a mesma caixa. `REF_UPVALUE` refs guardam o `*ObjUpvalue`, não o ponteiro cru — nada a fazer. `REF_PTR` não tem site de criação (só leituras em `executor.go:224`, `references.go:92`, `stack.go:108`) — permanece morto e inalterado. Nota: `IsValid` (`value.go:197`) só exige `location != nil`, então uma task pode receber closure com upvalue **aberto** apontando para a pilha do VM pai — hazard pré-existente de leitura concorrente (a memória antiga continua válida para o GC; `Relocate` sob `mu` não o piora). Fora de escopo.
- **Auditoria obrigatória de `*CallFrame` e de fatias de `vm.stack` mantidos em variáveis Go através de uma chamada que pode reentrar na VM** (crescimento invalida o ponteiro/fatia antiga):
  - `executor.go`: o loop já faz `frame = vm.currentFrame` depois de `OP_CALL`/`OP_CALL_STATIC`/`OP_RETURN`/`OP_IMPORT` (l.1132, 1147, 1287, 1349) — manter; verificar os demais opcodes que chamam `callValue`/natives reentrantes.
  - `unwind.go:22-104` (`finalizeCurrentFrame`): segura `frame := vm.currentFrame` durante o laço de `Deferred` (que reentra a VM via `invokePreparedCall`) **e depois dele** usa `frame.StackBase`, zera/trunca `frame.Owned`, nil'a `frame.Closure`/`Environment` — com `frames` realocado por um defer, tudo isso cairia no array velho (posse não liberada, defers "fantasma" no slot novo). Passa a trabalhar **por índice**: `idx := vm.frameCount-1`, `frame = &vm.frames[idx]` reobtido **depois de cada `invokePreparedCall` e antes da seção RC**.
  - `calls.go:143` (`frame := &vm.frames[...]` depois de `ensureCallCapacity` — ok), `task_execution.go:84`, `references.go:176,217`, `runtime_errors.go:119` (laços sem chamada — ok).
  - `callNative` passa `args := vm.stack[a:b]` a natives que podem reentrar (comparadores, `call_result`): leitura da fatia antiga continua correta (os valores são cópias por valor e ninguém escreve nela); `callNative` já ajusta `stackTop` por aritmética de índice depois da reentrada (`calls.go:109-110`) — documentar no código; não copiar.
  - `architecture_test.go:780-820` tem um matcher estático que classifica escritas em `vm.frames`/`frameCount` (“resets/clears frames”) — `ensureCallCapacity` (`vm.frames = grown`) tem de passar por ele ou o matcher ganha a exceção explícita; o plano roda esse teste cedo.
- Documentação: spec §13 ("profundidade de chamada limitada só por memória: 100 000 frames / 1 048 576 slots por VM (tetos, alocação sob demanda a partir de 64/2048) → erro `stack overflow`"), AGENTS.md §Segurança (trocar `vm.sp >= StackMax` pela descrição nova).

**Testes.** `depth(10000)` (issue) e `depth(50_000)` devolvem o valor; recursão infinita → erro de runtime `stack overflow: call depth…` (não panic, exit 1, sem trace Go); literal/`int[N]` que ultrapasse o teto de operandos num único frame → `stack overflow: operand stack…` via sentinela; lista encadeada recursiva (`noxy_examples/linked_list.nx` padrão `push_back`) com 1 000 nós; closure que captura um local **antes** de uma recursão profunda (força crescimento da pilha) e lê/escreve o upvalue depois (valor correto — prova do `Relocate`); `ref` a local (REF_UPVALUE) através do crescimento; **defer cujo corpo recursa fundo o bastante para crescer `frames` (prova do reobter por índice em `finalizeCurrentFrame`: posse liberada, `Deferred` do slot vazio, sem defer executado duas vezes)**; defer registrado em frame externo que roda após recursão profunda; `call_result` em volta de recursão infinita devolve falha `stack overflow`; VM de task nasce com `len(stack) == stackInitial`; adaptar `defer_test.go:304-323` e `builtins_tasks_test.go:481` (indexam `StackMax-…` direto) para `len(machine.stack)`; `TestUnwindArchitecture*` (`architecture_test.go`) continua verde com o crescimento. **Benchmark**: `BenchmarkNoxyCallOverhead` (`call_alloc_bench_test.go`) antes/depois, rodadas intercaladas — custo por chamada não pode regredir além do ruído (o teste de capacidade é uma comparação por chamada/push).

### 2. `let a: T[N]` sem inicializador — `OP_ARRAY_FILL`

**Hoje.** `emitDefaultInit` (`compiler.go:2716-2727`) emite o default N vezes e `OP_ARRAY` com operando de 16 bits: N > ~2047 estoura a pilha de operandos (panic), N > 65535 trunca o operando em silêncio.

**Depois.** Novo opcode `OP_ARRAY_FILL` (sem operando): desempilha `count` (int) e `default`, empilha `value.NewArray` com `count` cópias do default. Para default **composto** (`int[3][3]`, `map<…>[N]`) cada slot recebe o **mesmo** objeto com `Owners = N` — a CoW garante independência na primeira escrita (`g[0][0] = 1` não altera `g[1][0]`). O compilador emite `default; CONSTANT N; OP_ARRAY_FILL` para `Size > 0` (sem limite de N além do `int`); `Size == 0` continua `OP_ARRAY 0`. Tipo estático inalterado (`ArrayType{Size: N}`). `count < 0` é impossível pela sintaxe (literal), mas o opcode valida como `OP_ZEROS` (`executor.go:1026`).

**Testes.** `let buf: int[10000]; length(buf) == 10000`; `int[100000]`; defaults de `float[5]`/`string[3]`/`bool[2]`/`bytes[2]`/`Point[4]` (null); `int[3][3]` independência CoW; disassembly mostra `OP_ARRAY_FILL`; o runner não regride.

### 3. `input()` com stdin redirecionado + `io.stdin()`

**Hoje.** `input` (`builtins_io.go:341-351`) cria `bufio.NewReader(os.Stdin)` a cada chamada: o primeiro lê 4 KB e descarta o resto; erro ignorado; EOF indistinguível de linha vazia.

**Depois.**

- `SharedState` ganha `stdin *bufio.Reader` criado uma vez (`sync.Once`) sobre `os.Stdin`; `input()` usa esse leitor. Semântica de `input()`: imprime o prompt (se dado) sempre, inclusive no EOF; devolve a linha sem `\r\n`/`\n`; no EOF **com** texto parcial devolve o parcial; no EOF **sem** texto devolve `""`. **Decisão explícita: `input()` não sinaliza EOF** (manter a assinatura `-> string` e o contrato atual); quem precisa distinguir EOF de linha vazia usa `io.read_line(io.stdin())` — isto vai para §10 da spec da linguagem e para "Fora de escopo".
- `io.stdin() -> File` (nativa `io_stdin(File)`): devolve um `io.File` (`path="<stdin>"`, `mode="r"`, `open=true`) cujo `FileResource` embrulha `os.Stdin` **e o mesmo `bufio.Reader` de `input()`** (misturar `input()` e `io.read_line(io.stdin())` não perde dados). Registrado uma vez por `SharedState` (chamadas repetidas devolvem o mesmo `fd`). `io.close` nele só marca fechado (não fecha `os.Stdin`). `io.write` nele → sem efeito (`write_result` devolve `success=false`, `error="stdin is read-only"`).
- `FileResource` ganha `reader *bufio.Reader` criado sob demanda (usado por `read_line`, §4). Contrato único de `io.read`/`io.read_lines`: **devolvem o conteúdo inteiro do arquivo** — em arquivo comum isso é "do início" (`Seek(0,0)`, como hoje); em stdin, onde não existe início reposicionável, é "tudo o que ainda não foi consumido" (`io.ReadAll(reader)`, sem `Stat().Size()`, inútil em pipe). `read_line` é incremental a partir da posição corrente. Documentar em §12 da spec com esta redação e o aviso "não misture `read_line` com `read`/`read_lines` no mesmo handle de arquivo comum (o segundo relê do início)".

**Testes.** Substituindo `os.Stdin` por um pipe no teste Go: três `input()` devolvem as três linhas; `input()` no EOF devolve `""`; laço `io.read_line(io.stdin())` devolve `ok=true` para cada linha e `ok=false`/`error="EOF"` no fim; `input()` depois de `read_line` continua da posição certa; `io.read(io.stdin())` devolve o restante. Um exemplo `noxy_examples/wc_stdin.nx` (contagem de linhas/palavras/chars à la K&R) **excluído do runner** (interativo) mas executado no plano com `printf … | noxy`.

### 4. Nativas de `io` ausentes — `read_line`, `list_dir`, `rename` + teste de higiene

- `io_read_line(file, IOResult)`: lê até `\n` (inclusive) pelo `reader` do recurso; devolve `ok=true, data=linha sem \r\n/\n`; no EOF sem dados `ok=false, data="", error="EOF"`; última linha sem `\n` é devolvida com `ok=true` e a chamada seguinte dá EOF; UTF-8 inválido → `ok=false` com a mesma mensagem de `io.read` (`requireValidUTF8`); arquivo fechado → `ok=false, error="File not open"` (padrão dos demais).
- `io_list_dir(path, IOLinesResult)`: `os.ReadDir` → `data` = nomes (sem caminho, sem distinção arquivo/diretório — para isso `io.stat(path + "/" + nome).is_dir`, que já existe; é assim que o `dirwalk` do K&R se escreve), ordem de `ReadDir` = ordenada por nome, `ok=false, error=<os error>` em falha. **`io.nx` muda a assinatura** de `list_dir(path) -> IOResult` (quebrada, `data: string` nunca funcionou) para `-> IOLinesResult` (reuso deliberado do tipo "lista de strings"; documentado em §12).
- `io_rename(src, dst) -> bool` (`os.Rename == nil`).
- `io.nx`: `read_line`/`list_dir`/`rename` mantêm os wrappers (só `list_dir` muda o tipo de retorno).
- **Teste de higiene** (`stdlib_hygiene_test.go`): para cada `internal/stdlib/*.nx`, parsear com o parser real e coletar os identificadores usados como **alvo de chamada** (`CallExpression.Function` é `*ast.Identifier`) que não são declarados no módulo (func/struct/let de topo, parâmetros, `let` locais) nem trazidos por `use X select *` de outro módulo da stdlib; cada nome restante tem de estar em `collectNativeRegistrations` (já existe no arquivo). Hoje esse teste falharia exatamente em `io_read_line`, `io_list_dir`, `io_rename`.

**Testes.** Unitários em `builtins_io_test.go` (padrão `testFileDefinition`/`cleanupFileResources`): `read_line` em arquivo de 3 linhas com e sem `\n` final, CRLF, vazio, fechado; `list_dir` em diretório temporário (nomes ordenados, `ok=false` em caminho inexistente); `rename` ok/erro; higiene passa.

### 5. Condição não-booleana — erro de compilação / runtime

**Hoje.** `IfStatement`/`WhileStatement` (`compiler.go:1344-1452`) não checam o tipo da condição; `OP_JUMP_IF_FALSE`/`OP_JUMP_IF_TRUE` (`executor.go:123-137`) só testam "é `false` literal" — `0`, `""`, `[]`, `null` são verdadeiros. `!` aceita qualquer tipo estático (erro só em runtime), `&&` checa operandos, `||` **não** checa (`compiler.go:1072-1085`).

**Depois.**

- **Compilação** (caminho não fusionado de `if`/`elif`/`while`, e operandos de `!`, `&&`, `||`): depois de compilar a expressão (e de `OP_DEREF` se for `ref T`), o tipo estático tem de ser `bool`, `any` ou desconhecido (`nil`); senão:
  `[line N] condition must be bool, got int` + `hint: use an explicit comparison, e.g. 'x != 0', 'x != ""', 'x != null' or 'length(x) > 0'` (a mesma mensagem/hint para `while`; para `!`: `operand of '!' must be bool, got int`; `||` passa a dar o mesmo erro que `&&`: `logical operators require boolean operands, got …`). `null` literal como condição é erro de compilação (tipo `null`). Struct/array/map/ref como condição → erro (escreva `!= null`).
- **Runtime**: `OP_JUMP_IF_FALSE`/`OP_JUMP_IF_TRUE` com operando cujo `Type != VAL_BOOL` → `runtimeError("condition must be bool, got %s", runtimeTypeName(v))`. Cobre `any`. Custo: uma comparação no caminho não tomado. Os usos internos (`for` via `OP_LESS_INT`, `when` via `OP_EQUAL_INT`, fusão int) produzem bool por construção.
- `isFalsey` (`stack.go:11`, trata `null` como falso — os saltos nunca o usaram, daí a inconsistência) — verificar os usos restantes; remover se ficar morto.
- Docs: spec §7 (If/While: "a condição é obrigatoriamente `bool`; não há valores truthy/falsy") e §8 Logical (`!`, `&&`, `||` exigem `bool`). CHANGELOG BREAKING com a migração (`if n` → `if n != 0`).
- **Migração — stdlib e exemplos**: a regra vale também para `internal/stdlib/*.nx` (há dezenas de `if x.ok`/`if !x.valid` em `http_client`/`http_parser`/`http_server`; quando o campo é de struct conhecido o tipo é `bool` e nada muda; quando resolve para `nil` passa na compilação e o runtime cobre). `go test ./...` compila toda a stdlib; depois rodar o runner; todo `.nx` que falhar é corrigido com a comparação explícita (listar no CHANGELOG). Verificado: `when` (`compiler.go:1715`, `OP_EQUAL_INT`) e `for` (`OP_LESS_INT`) produzem `bool` por construção — os únicos outros emissores de `OP_JUMP_IF_FALSE`.

**Testes.** Compilador: `if 0`, `if ""`, `if null`, `if arr`, `if p` (struct), `while n` (int), `if r` (`ref int`), `!0`, `true && 1`, `1 || true` → erros com as mensagens; `if r` (`ref bool`), `if x == 0`, `if any_value` compilam. VM: `any` não-bool em condição → erro de runtime com o nome do tipo; `any` bool funciona.

### 6. `stderr` — `eprint`/`eiprint` e diagnósticos da VM

- Builtins `eprint(args...)` e `eiprint(args...)` (`builtins_core.go`), idênticos a `print`/`iprint` mas em `os.Stderr`.
- `cmd/noxy/main.go`: um `var diagOut io.Writer = os.Stderr` de pacote; **tudo que é diagnóstico** (`"[l:c] SyntaxError…"` do parser, `Compiler error:`, `Runtime error:` + `hint:`, `Error reading file:`, erros de profile/pkg, `Recovered from panic` + trace) passa a `fmt.Fprintf(diagOut, …)`; saída do programa (`print`, prompt do REPL, `--version`, disassembly) continua em stdout. Mesma regra no REPL (erros de linha em stderr, como Python).
- VM: `builtins_concurrency.go:26,42,50,98,103` ("Runtime Error: spawn…", "Thread Panic", "Thread Error") e `builtins_sys.go:264,270` (plugin) → `os.Stderr`.
- Docs: spec §10 I/O (`print`, `iprint`, `eprint`, `eiprint`, `input`), README Builtin Functions; CHANGELOG BREAKING ("quem capturava erros por stdout precisa ler stderr / `2>&1`").

**Testes.** `cmd/noxy/main_test.go` (novo): com `diagOut` apontando para um buffer, `runWithConfig` de um programa com erro de runtime devolve 1, o buffer contém `Runtime error:` e stdout (capturado por pipe) não; erro de parser e de compilador idem. VM: `eprint` escreve em stderr e não em stdout (captura por pipe como em `builtins_io_test.go`).

### 7. `io.write_bytes` / `io.write_bytes_result`

**Achado na reprodução**: `io.write(f, b)` com `b: bytes` **funciona pelo namespace** (`use io` + `io.write(...)` não checa o tipo do argumento — o binding de namespace é opaco) e a nativa `io_write` já grava bytes crus (`builtins_io.go:118-123`); mas `bytes` não é atribuível a `string` (`use io select write; write(f, b)` → `argument 1 to 'write': expected string, got bytes`). Falta a API explícita.

**Depois.** `io.nx` ganha `write_bytes(file: File, data: bytes) -> void` e `write_bytes_result(file: File, data: bytes) -> IOWriteResult`, apontando para as nativas existentes. Sem mudança nas nativas. Docs §12.

**Testes.** Noxy: escreve `b"\x00\xff"` com `write_bytes`, relê com `read_bytes`, igual; `write_bytes_result.bytes_written == 2`; `use io select write_bytes` compila.

### 8. Estado de módulo: `select` copia; `m.x = v` é erro de compilação; tipo nominal único

- **8a (documentar)**: spec §11 Selective Import — "`select` vincula funções e structs pelo nome; para uma variável (`let`) de topo, `select` **copia o valor no momento do import** (snapshot) — alterações posteriores feitas pelo módulo não aparecem no nome importado. Para observar o estado vivo use a forma de namespace (`m.x`)". Sem mudança de código.
- **8b (erro de compilação)**: `AssignStmt` cujo alvo é `MemberAccessExpression` com `Left` identificador que resolve a um **namespace import** (`c.namespaceImports`, não sombreado por local/upvalue) → `[line N] cannot assign to 'calc_stack.sp': module variables are read-only outside the module` + `hint: expose a function in 'calc_stack' that updates it`. Só a forma direta `m.x = v`; `m.x.f = v`/`m.a[i] = v` ficam como hoje (fora de escopo).
- **8c (tipo nominal único)**: novo helper `c.typesEquivalent(a, b ast.NoxyType) bool` — comparação **estrutural** (Array: tamanho + elemento; Map: chave + valor; Ref; Function: params + retorno; Generic: nome + args; TypeParam/Chan: nome/elemento) cujas folhas `*ast.PrimitiveType` são iguais se o nome é igual **ou** se nomeiam a **mesma declaração de struct**. Identidade de struct = mesmo `*ast.StructStatement`: nome simples → `c.structs[name]`; qualificado `ns.Name` → `c.discoverModuleStructs(c.namespaceImports[ns])[Name]` (o memo de `loadModuleDeclarations` em `moduleDiscoveryState` garante o mesmo ponteiro dentro de uma compilação; reexport via `use X select *` dentro de `M` resolve para a declaração de `X` nos dois caminhos). Struct local só é igual a si mesmo. O helper substitui a comparação por `String()` em **cinco** sites: `areTypesCompatible` (`compiler.go:2849`), `areStrictTypesCompatible` default (`function_types.go:234` — `func(Point)` ≡ `func(geometry.Point)`), unificação de `T` (`generics_unify.go:88`, `generics_target.go:378` — ligar `T=Point` e depois receber `geometry.Point` não é conflito) e a checagem de tipo de template importado (`generics.go:881`). Mensagens de erro inalteradas. Em runtime o `ObjStruct` já é o mesmo objeto (módulo carregado uma vez), então nada muda na validação de tipo de runtime. Fora: `c.structs` não passa a indexar nomes qualificados (acesso a campo de `geometry.Point` continua com o tipo que tem hoje).

**Testes.** 8b: erro com a mensagem; `m.x` leitura continua viva (teste existente/novo). 8c: o exemplo `geometry` da issue nas duas direções (`let a: geometry.Point = geometry.Point(0,0)`, `let b: Point = Point(3,4)`, `dist2(a,b)` e `geometry.dist2(a,b)`, campo `a.x`), e o negativo: struct local `Point` ≠ `geometry.Point`.

### 9. `continue`

- `token.CONTINUE` (`"continue"`), `ast.ContinueStmt{Token}` (ao lado de `BreakStmt`), `parseContinueStatement` com o **mesmo contrato do `parseBreakStatement`** (não avança token — `parser.go:365-373`).
- Compilador: `Loop` ganha `ContinueJumps []int`. `case *ast.ContinueStmt`: `continue outside of loop` se `len(c.loops)==0`; **descarta os locais além de `EnclosingLocals` com a mesma regra do `endScope` (`compiler.go:2630-2636`): `OP_CLOSE_UPVALUE` para local `IsCaptured`, `OP_POP` para os demais** — fatorado num helper `emitLocalsExit(n)` usado por `endScope`, `break` e `continue`. Isso corrige de passagem o `break` (`compiler.go:1762-1776`), que hoje emite `OP_POP` cru e deixa aberto o upvalue de um `let` do corpo capturado por closure (o slot é reusado pela próxima iteração/statement e a closure passa a ler outro valor). A ordem textual garante a correção: um upvalue só existe em runtime depois do `OP_CLOSURE` que o captura, que é textualmente posterior ao `let`; um `continue` textualmente **antes** da closure vê `IsCaptured=false` mas nessa iteração nenhuma caixa foi aberta ainda (POP correto); um `continue` textualmente **depois** vê `IsCaptured=true` (CLOSE). No **`while`** o alvo é `loopStart` (reavaliar a condição) → `emitLoop(loopStart)` direto; no **`for`** o alvo é o início do passo "10. Increment Index" (`compiler.go:~1552`, logo depois do `endScope()` do corpo — a variável do laço já foi descartada pelo `continue`) → `emitJump(OP_JUMP)` registrado em `ContinueJumps` e patchado ali. Laços aninhados: `c.loops` topo.
- Walkers de generics: `generics.go:954,1070` e `generics_substitute.go:144` ganham `case *ast.ContinueStmt:` (sem nome/tipo); `generics_walkers_guard_test.go` (se enumera nós) atualizado.
- Docs: spec §1.2 keywords (+`continue`) e §7 (While/For: `break`/`continue`).

**Testes.** Compilador: `continue` em `while`, em `for` (array, map, string), aninhado, dentro de `if` com `let` no corpo (pops corretos — pilha balanceada), fora de laço (erro). VM e2e: saídas esperadas (ímpares de 1..9, etc.); **closures**: `let` do corpo capturado por closure guardada num array, com `continue` (e com `break`) textualmente **depois** da closure → cada closure devolve o valor da sua iteração; variante com a closure textualmente **depois** do `continue` (o `continue` pula a criação) → só as iterações que criaram closure aparecem, com os valores certos; `loop_break_test.go` dos dois pacotes serve de modelo.

### 10. `if c then return end` em uma linha

`parseReturnStatement` (`parser.go:326-344`): se `peekToken` é `END`/`ELSE`/`ELIF` → devolve o `ReturnStmt` vazio **sem avançar** (curToken fica em `return`; o bloco chamador faz o `nextToken`). Se `peekToken` é `NEWLINE`/`EOF` → comportamento atual. Testes de parser: `if x > 0 then return end`, `if c then return else … end`, `if c then return elif …`, e o caso multilinha inalterado.

### 11. Literais em notação científica

`readNumber` (`lexer.go:267`): depois dos dígitos (e fração opcional), se `ch ∈ {e,E}` e (`peek` é dígito, ou `peek ∈ {+,-}` e o caractere seguinte é dígito) → consome expoente e devolve `FLOAT`. `1e3`, `1.5e3`, `2E-10`, `1e+2` → float; `1e` seguido de não-dígito não consome o `e`. Hex/bin inalterados; `.5` continua não suportado (fora de escopo). Spec §2.1 exemplos. Testes de lexer e parser (`FloatLiteral.Value == 1500.0`).

### 12. `io.read_lines` sem `""` final

`io_read_lines` (`builtins_io.go:272-303`): depois de normalizar CRLF e `Split`, **se o conteúdo termina em `\n`, descarta o último elemento (vazio)**; conteúdo vazio → `[]` (hoje `[""]`). Logo `"a\nb\n"` → `[a, b]`, `"a\nb"` → `[a, b]`, `"\n"` → `[""]`, `""` → `[]`. Docs §12 + CHANGELOG BREAKING. Teste unitário com os quatro casos.

### 13. Linha do erro de atribuição a global

`case *ast.AssignStmt` (`compiler.go:418`) ganha `c.setLine(n.Token.Line)` no início (o token é o `=`: `parser.go:182-185`, `tokenAssign := p.curToken` depois de comer `ASSIGN`). Teste de compilador: o erro de `x = 3.14` aponta a linha 2.

### 14. F-strings — `{{`/`}}` e erro para spec

`parseFString` (`parser.go:970-1040`): `{{` → `{` literal e `}}` → `}` literal fora de expressão; dentro de `{…}`, depois de `parseExpression(LOWEST)` o sub-parser tem de estar em `EOF` — senão `[l:c] SyntaxError: unexpected ':' in f-string expression (format specs are not supported)` + `hint: use fmt("%10s", x) for width/precision` (o hint só quando o token sobrando é `:`; caso geral: `unexpected 'tok' in f-string expression`). Um `}` isolado fora de expressão continua literal (comportamento atual; não quebrar). O escape `\{` do lexer continua produzindo `{` (inalterado — o plano verifica se `f"\{x}"` é tratado como expressão hoje e registra o comportamento no teste, sem mudá-lo). **Quebra aceita e documentada**: uma expressão que *começa* por `{` (map literal, `parser.go:65`) dentro de f-string passa a precisar de espaço — `f"{ {"a": 1}["a"] }"` — a mesma regra do Python; hoje o `braceCount` (`parser.go:988-998`) aceita `{{"a":1}["a"]}`. **Terceiro sub-achado da issue — string literal com aspas duplas dentro de `{}` (`f"{"a"}"`)** — é limitação do lexer (a `"` interna fecha a f-string) e fica **fora de escopo**; a saída é alternar as aspas (`f'{"a"}'`, já aceito pelo lexer) — documentar em §9. Spec §9 (escape `{{`/`}}`; "sem format spec — use `fmt`"; regra do espaço; aspas alternadas). Testes de parser/VM: `f"{{x}}"` → `{x}`, `f"{{{x}}}"` → `{1}`, `f"{name:>10}"` → erro com hint, `f"{a b}"` → erro, `f"{ {"a": 1}["a"] }"` → `1`, `f'{"a"}'` → `a`.

### 15. Exit code ≠ 0 para script inexistente

`main.go:84-88`: `Error reading file: …` em `diagOut` (stderr) e `os.Exit(1)`. Extrair `loadScript(filename) (string, bool)` para testar em `main_test.go`.

### 16. Miscelânea — checagens estáticas e docs

- `*` unário em tipo estático **conhecido e não-ref** (`PrefixExpression`, `compiler.go:1292-1323`; inclui `any`, que nunca guarda ref) → `[line N] cannot dereference non-reference value of type int` (`2 ** 3` passa a ser erro). **Fato verificado**: hoje `*x` com tipo não-ref é um no-op no compilador (só emite `OP_DEREF` para `RefType`) e `OP_DEREF` em runtime **passa não-ref adiante sem erro** (`executor.go:609-622`) — não existe caminho de erro de runtime. Decisão para tipo **desconhecido** (`nil`, ex.: membro de namespace): emitir `OP_DEREF` (deref se for ref em runtime, passthrough se não — leniência documentada no código); a checagem estática é a única guarda real.
- `!` em não-bool e `~` em não-int (tipo estático conhecido) → erro de compilação (`operand of '~' must be int, got bool`).
- Bitwise binário `& | ^ << >>` com algum operando de tipo estático conhecido que não seja `int`/`bytes`/`any` → `[line N] operands for & must be integers or bytes, got int and bool` (mesmo texto do runtime; `1 & 3 == 1` vira erro de compilação).
- **Docs**: spec §8 Mathematical — "aritmética de `int` dá a volta (complemento de dois, como Go); não há erro de overflow em `+ - *`"; AGENTS.md §Segurança — trocar o snippet "Overflow protection" por essa regra (e o de "Resource limits" pelo §1); spec §12 `sys.exec_output` — nota Windows ("o comando já roda via `cmd /C`; não aninhar `cmd /c`; saída não-UTF-8 dá `ok=false`"); spec §2.2/§7 — "ordem de iteração e de impressão de `map` é indefinida (Go)".

**Testes.** Compilador: `2 ** 3`, `!0`, `~true`, `1 & true`, `"a" << 1` → erros; `*r` com `r: ref int`, `~5`, `b"\x0f" & b"\x01"` compilam.

---

## Fora de escopo (não fazer neste PR)

- `select` como alias vivo (8a) e escrita em variável de módulo (8b) — decididos contra; `m.x.f = v` / `m.a[i] = v` ficam como estão.
- `input()` sinalizar EOF (3) — decidido contra: mantém `-> string` e `""` no EOF; EOF explícito só via `io.read_line(io.stdin())`.
- Format spec em f-string (14); string com aspas duplas dentro de `{}` em f-string de aspas duplas (14 — use `f'{"a"}'`); `.5` e semântica octal `031` (11); `io.stdout()`/`io.stderr()` como `File` (6 — `eprint` cobre); overflow checado (16); erro de parser para `}` isolado em f-string.
- Mudar `io.read`/`read_lines` de "conteúdo inteiro" para "posição corrente" em arquivos comuns (só stdin, sem início reposicionável, lê o restante — §3).
- Upvalue aberto compartilhado entre VM pai e task (hazard pré-existente, §1); cálculo de profundidade máxima de pilha por função no compilador (alternativa ao sentinela — a reserva na entrada do frame basta).
- `c.structs` indexar nomes qualificados / tipar acesso a campo de `ns.Struct` (8c só unifica a comparação de tipos).
- #47 (global inexistente só explode em runtime) — família citada pela issue, não incluída.

## Critérios de aceite

1. Os 21 programas de reprodução da issue (`scratchpad/repro/*.nx` desta sessão, reproduzidos na seção "Detalhes" da issue) produzem o comportamento "Depois" descrito acima — exit codes e mensagens.
2. `go build ./... && go vet ./... && go test ./...` verdes; `gofmt` limpo nos arquivos tocados.
3. `go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx` — todos os exemplos passam (171/171 hoje; exemplos migrados pelo item 5 listados no CHANGELOG); diff da saída dos exemplos antes/depois só mostra não-determinismo/ambiente e as mudanças intencionais (12).
4. `BenchmarkNoxyCallOverhead` sem regressão além do ruído (rodadas intercaladas).
5. Spec, README, AGENTS.md e CHANGELOG atualizados; `internal/version/version.go` = `v0.11.0`.
6. Nenhum `fmt.Print*` de diagnóstico restante em `cmd/noxy` ou `internal/vm` (grep no plano).

## Components

- `internal/vm/vm.go`, `calls.go`, `stack.go`, `executor.go`, `unwind.go` — pilhas dinâmicas, sentinela de overflow, `OP_ARRAY_FILL`, checagem bool nos saltos (itens 1, 2, 5)
- `internal/value/value.go` (`ObjUpvalue.Relocate`) — item 1
- `internal/chunk/chunk.go` — `OP_ARRAY_FILL` + disassembly (2)
- `internal/vm/builtins_io.go`, `resources.go`, `vm.go` (`SharedState.stdin`) — itens 3, 4, 12
- `internal/vm/builtins_core.go` (`eprint`/`eiprint`), `builtins_concurrency.go`, `builtins_sys.go` — item 6
- `internal/stdlib/io.nx` — itens 3, 4, 7
- `internal/compiler/compiler.go`, `module_exports.go` — itens 2, 5, 8b, 8c, 9, 13, 16
- `internal/compiler/generics.go`, `generics_substitute.go` — item 9
- `internal/parser/parser.go`, `internal/ast/ast.go`, `internal/token/token.go` — itens 9, 10, 14
- `internal/lexer/lexer.go` — item 11
- `cmd/noxy/main.go` (+ `main_test.go` novo) — itens 6, 15
- `internal/vm/stdlib_hygiene_test.go` — item 4
- `docs/NOXY_LANGUAGE_SPEC.md` §1.2, §2.1, §2.2, §7, §8, §9, §10, §11, §12, §13; `README.md`; `AGENTS.md`; `CHANGELOG.md`; `internal/version/version.go`
- `noxy_examples/` — exemplos migrados (5) + `wc_stdin.nx` (3, excluído do runner) + `continue`/`read_line` em exemplo existente ou novo

## Test Plan

- [x] Reproduções dos 16 itens executadas em `develop` 691d902 (2026-08-20) — todas confirmadas, exceto a nuance do item 7
- [ ] Item 1: `depth(10000)`/`depth(50000)`; overflow infinito → erro de runtime (`call depth`); estouro num único frame → erro (`operand stack`) via sentinela; upvalue/ref através do crescimento; defer que cresce `frames` (unwind por índice); `call_result` captura o estouro; tasks nascem pequenas; `TestUnwindArchitecture*` verde; benchmark sem regressão
- [ ] Item 2: `int[10000]`, `int[100000]`, defaults por tipo, `int[3][3]` CoW
- [ ] Item 3: pipe de 3 linhas → `input()` ×3; EOF; `io.stdin()` + `read_line` até `ok=false`; mistura `input`/`read_line`
- [ ] Item 4: `read_line`/`list_dir`/`rename` unitários; higiene de wrappers da stdlib
- [ ] Item 5: matriz de erros de compilação; `any` em runtime; exemplos migrados
- [ ] Item 6: `eprint` em stderr; diagnósticos da CLI em stderr (`main_test.go`)
- [ ] Item 7: `write_bytes` round-trip
- [ ] Item 8: erro `m.x = v`; `geometry.Point` ≡ `Point` em `areTypesCompatible`, em tipo de função (`func(Point)` vs `func(geometry.Point)`), em inferência de `T` e em reexport via `select *`; negativo struct local
- [ ] Item 9: `continue` em while/for/aninhado/fora de laço; closures capturando `let` do corpo com `continue`/`break` (upvalues fechados); walkers de generics
- [ ] Item 10: `if c then return end`/`else`/`elif`
- [ ] Item 11: `1e3`, `1.5e-3`, `2E+10`, `1e` não consumido
- [ ] Item 12: quatro casos de `read_lines`
- [ ] Item 13: linha correta
- [ ] Item 14: `{{`/`}}`; erro para `:spec` com hint; map literal com espaço; `f'{"a"}'`
- [ ] Item 15: exit 1 + stderr para arquivo inexistente
- [ ] Item 16: erros estáticos de `*`/`!`/`~`/bitwise; docs de overflow/`exec_output`/`map`
- [ ] `go test ./...`, runner 100 %, diff dos exemplos, CHANGELOG/spec/README/AGENTS/version
