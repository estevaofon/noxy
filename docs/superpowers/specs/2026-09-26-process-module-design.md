# Módulo `process`: processo filho supervisionado

**Data:** 2026-09-26 · **Branch:** `develop` (v0.26.0, ainda sem tag)
**Status:** implementado na v0.26.0 · **Origem:** achado 1 do Noxy-Editor (`docs/ACHADOS.md` daquele projeto) · **Relação:** spec de extensões por processo (`2026-08-29-process-extensions-design.md`, §4.5 guarda de morte e job object — o mesmo mecanismo, reaproveitado); `sys.exec`/`sys.exec_output` (spec da linguagem §12, `sys`).

Um programa Noxy que precisa rodar outro programa e **continuar vivo enquanto ele roda** — um editor executando o arquivo aberto, um runner de testes, um supervisor — não tem hoje nenhuma primitiva: `sys.exec` e `sys.exec_output` bloqueiam até o filho terminar e não devolvem handle. O editor contornou com `setsid sh -c '...' & echo $!`, saída num arquivo lido aos pedaços, um arquivo-sentinela para o fim e `kill -TERM -PGID`; no Windows, `Start-Process -PassThru` por PowerShell e `taskkill /T`. É o que este módulo entrega de forma nativa e portátil.

## 0. Fatos verificados antes do design (2026-09-26, `develop` pós-achados 2–12)

| O quê | Hoje |
|---|---|
| `sys.exec(cmd)` / `sys.exec_output(cmd)` (`internal/vm/builtins_sys.go`) | `shellCommand(cmd)`: `sh -c` em Unix, `cmd /S /C "<linha>"` no Windows (`shell_command_{other,windows}.go`); `Run`/`CombinedOutput` bloqueantes; sem handle, sem kill |
| Recursos (`internal/vm/resources.go`, `vm.go`) | `SharedState` tem `Files`, `Listeners`, `Sockets`, `Databases`, `Statements` — `*handleRegistry[*T]` com `add/get/remove/snapshot`; handle inteiro no struct Noxy (`File.fd`, `Socket.fd`); guarda de arquitetura `TestResourceRegistriesAndModuleCacheHaveSharedOwners` exige cada registry no `SharedState` |
| Ciclo de vida na saída | `sys_exit` chama `vm.shared.CloseExtensions()` antes de `os.Exit`; a CLI e o REPL fazem `defer machine.CloseExtensions()`. Não há gancho equivalente para outros recursos |
| Extensões por processo (`internal/ext/process_spawn*.go`) | Linux: `Pdeathsig: SIGKILL` com repetição sem a guarda quando o sandbox recusa (`EPERM`, AWS Lambda); Windows: job object `KILL_ON_JOB_CLOSE` anexado após o `Start` (`golang.org/x/sys/windows`, já dependência); demais Unix: nada. É privado do pacote `ext` |
| Convenção de handle inválido | `io.*` devolve `ok=false, error="File not open"`; `net_accept` num listener inexistente devolve erro de runtime; `pop` em array vazio é erro de runtime (regra da #121: misuse é erro) |
| Strings | toda `string` Noxy é UTF-8 válido; `to_str(bytes)` levanta em bytes inválidos; `io.read_bytes`/`net.recv` são as saídas brutas |
| Sombreamento por `select *` | só `strings.contains` e `http_client.delete` colidem com nativos globais; `spawn` **é** nativo global — um módulo que exportasse `spawn` sombrearia o builtin de routines (achado 4) |

## 1. Objetivo, escopo e não-escopo

**Objetivo.** `process.start(cmd)` sobe o comando pelo shell da plataforma e devolve um handle; o programa lê a saída aos pedaços sem bloquear, consulta ou espera o fim, pede parada (`terminate`) ou força (`kill`) da **árvore inteira**, e o handle é o dono: fechá-lo — ou o próprio Noxy sair — encerra o que ainda roda.

**Escopo.** Módulo `process` (stdlib embutida) com `start`, `read`, `poll`, `wait`, `wait_for`, `terminate`, `kill`, `close`; registry `Processes` no `SharedState`; grupo de processos (Unix) e job object (Windows); limpeza na saída do Noxy; spec §12, CHANGELOG, exemplo.

**Fora de escopo** (continuações): stdin do filho (terminal interativo é o domínio da extensão `noxy_pty`); stdout e stderr separados; `argv` sem shell (`exec_args`); variáveis de ambiente e diretório por chamada (herda os do Noxy, como `sys.exec`); sinal arbitrário (`signal(p, n)`); decodificação de codepage do console do Windows.

## 2. Decisões e precedentes

| Decisão | Precedente | Alternativa descartada |
|---|---|---|
| Módulo próprio `process`, não funções em `sys` | Python `subprocess`, Node `child_process` — um recurso com ciclo de vida merece namespace | `sys.spawn_process` + `sys.process_*`: seis funções com prefixo dentro de `sys` |
| `start`, não `spawn` | evita sombrear o builtin `spawn` (routines) em `use process select *` — a colisão do achado 4 | `spawn`: nome natural, mas colide |
| Comando é uma linha de shell, como `sys.exec` | `sys.exec`/`exec_output` (spec §12); Python `shell=True` | `argv: string[]` sem shell: mais seguro, mas outra superfície; fica para `exec_args` |
| stdout+stderr num único fluxo | `sys.exec_output` já combina; o caso de uso (mostrar a saída de um programa) quer a ordem de chegada | dois fluxos: dois buffers, duas leituras, ordem perdida entre eles |
| `read(p) -> bytes`, não bloqueante, corta só em fronteira UTF-8 | `io.read_bytes`/`net.recv` devolvem bruto; o invariante UTF-8 proíbe decodificação com perda | `read -> string`: um pedaço que parte um caractere no meio seria inválido; U+FFFD foi removido de propósito (CHANGELOG, invariante UTF-8) |
| `terminate` = SIGTERM ao grupo; `kill` = SIGKILL ao grupo; Windows: os dois terminam o job. Alcançam o grupo enquanto o líder roda **ou** o pipe está aberto (revisão: senão um `server &` só pararia por `close`) | Python `Popen.terminate()/kill()`; Go `os/exec` não mata a árvore — o editor precisou de `kill -PGID` e `taskkill /T` | um só `kill(p, force: bool)`: dois nomes leem melhor e Noxy não tem parâmetro opcional |
| Ctrl+C/SIGTERM no `noxy` (modo script) passam por `CloseProcesses` + `CloseExtensions` e saem com 128+sinal, salvo programa dono dos sinais (`sys.signal_notify`) | shells e supervisores; sem tratador o Go sai sem `defer` e, com `Setpgid`, o filho nem recebe o Ctrl+C do terminal (revisão, achado 2) | documentar e deixar a árvore órfã: é o sintoma do achado 1 na forma mais comum de parar um programa |
| `close` mata o que ainda roda e libera o handle; a saída do Noxy fecha todos | spec de extensões §4.5 (EOF + kill na saída); "recursos vivem em registries; remova ao fechar" (AGENTS.md) | `close` só libera: um processo que ninguém consegue mais parar é exatamente o achado 1. Daemon desacoplado é `sys.exec("... &")` |
| Handle inválido (fechado, `ok=false`) é erro de runtime em `read`/`poll`/`wait`/`wait_for`/`terminate`/`kill`; `close` é idempotente | `pop` em vazio, `net_accept` em listener inexistente (misuse é erro, #121) | `io`-style `ok=false`: `read` devolve `bytes`, não struct; esconderia o bug |
| Guarda de morte best effort (Linux `Pdeathsig` no líder; Windows job `KILL_ON_JOB_CLOSE`) | spec de extensões §4.5, inclusive a repetição sem a guarda em `EPERM` | garantia forte (subreaper, cgroup): fora de escopo |

## 3. API (`internal/stdlib/process.nx`)

```noxy
struct Process
    handle: int      // chave no registry; -1 quando ok=false
    pid: int         // do líder (o shell); -1 quando ok=false
    ok: bool         // o processo subiu
    error: string    // por que não subiu ("" em sucesso)
end

struct ProcessStatus
    running: bool
    exit_code: int   // -1 enquanto roda ou se morreu por sinal (Unix); 1 quando o job foi terminado (Windows)
end

func start(cmd: string) -> Process               // sobe pelo shell da plataforma; stdin é o dispositivo nulo
func read(p: Process) -> bytes                   // o que chegou desde a última leitura; b"" se nada; nunca bloqueia
func poll(p: Process) -> ProcessStatus           // nunca bloqueia
func wait(p: Process) -> int                     // bloqueia até o fim; exit code
func wait_for(p: Process, timeout_ms: int) -> ProcessStatus   // bloqueia até o fim ou o prazo; running=true no prazo
func terminate(p: Process) -> bool               // pede parada à árvore (SIGTERM ao grupo; Windows: termina o job); false se líder saiu E fluxo acabou
func kill(p: Process) -> bool                    // força (SIGKILL ao grupo; Windows: termina o job); mesma regra
func close(p: Process) -> void                   // mata o que ainda roda, libera o handle; idempotente
```

Nativos: `process_start(cmd, Process)`, `process_read(p)`, `process_poll(p, ProcessStatus)`, `process_wait(p)`, `process_wait_for(p, ms, ProcessStatus)`, `process_terminate(p)`, `process_kill(p)`, `process_close(p)` — só expostos pelos wrappers, como manda o AGENTS.md.

## 4. Semântica

### 4.1 Saída

stdout e stderr do filho vão para **um** pipe (`os.Pipe`, o mesmo descritor nos dois), que uma goroutine drena para um buffer do recurso; `read` entrega o buffer e o esvazia. Enquanto o fluxo não chegou ao fim, um pedaço **nunca termina no meio de uma sequência UTF-8**: até 3 bytes de uma sequência incompleta ficam retidos para a próxima leitura (`incompleteUTF8Tail`). Bytes inválidos (não incompletos) saem como estão — o fluxo é bruto. Consequência: para um produtor UTF-8, `to_str(process.read(p))` sempre funciona; para um produtor de outra codepage, acumule os `bytes`.

O fim do fluxo (EOF) é independente do fim do líder: um neto que herdou o pipe (`sh -c 'servidor &'`) o mantém aberto. `poll`/`wait` olham o líder (`cmd.Wait`), nunca o pipe.

### 4.2 Fim, parada e árvore

`cmd.Wait` roda numa goroutine própria desde o `start`; `poll` é um `select` não bloqueante no `done`, `wait` bloqueia nele, `wait_for` corre contra um timer. `exit_code` é `ProcessState.ExitCode()`: −1 quando o líder morreu por sinal.

Unix: o líder sobe com `Setpgid` (grupo = pid do líder); `terminate`/`kill` enviam `SIGTERM`/`SIGKILL` a `-pid` enquanto o líder roda **ou** o pipe continua aberto (um neto vive e, com ele, o grupo — um pgid não é reutilizado enquanto tem membro); com o líder reaped e o fluxo encerrado, ou depois de `close`, devolvem `false` sem sinalizar. Se o grupo já não existe (`ESRCH`), o sinal vai ao pid do líder como fallback. Windows: o líder é posto num job object com `KILL_ON_JOB_CLOSE` logo após o `Start`; `terminate` e `kill` são `TerminateJobObject(job, 1)` (não há SIGTERM no Windows; fallback `Process.Kill` se o job não pôde ser criado).

`close`: remove do registry; se o líder ainda roda, `kill` e espera o `Wait`; se o líder já saiu mas o pipe ainda está aberto (neto vivo), `kill` ao grupo/job; libera o job (Windows), marca fechado e **fecha o lado de leitura do pipe** — um escritor que saiu do grupo (`setsid`) sobrevive ao kill e, sem isso, seguraria a goroutine de drain, o fd e um buffer sem leitor pelo resto do programa. Idempotente; num handle desconhecido não faz nada.

A carência de 100 ms para a saída drenar depois do fim do líder é paga **uma vez** por processo, na primeira observação do fim por `poll`/`wait`/`wait_for`; as seguintes voltam na hora (`poll` nunca bloqueia, mesmo com um neto segurando o pipe).

### 4.3 Saída do Noxy

`SharedState.CloseProcesses()` fecha (mata) todo processo ainda registrado. É chamado em `sys_exit` (antes de `os.Exit`, ao lado de `CloseExtensions`), por `defer` na CLI (`runWithConfig`) e no REPL (`runREPL`), e pelo tratador de Ctrl+C/`SIGTERM` que a CLI instala em modo script (`cmd/noxy/signals.go`): limpa e sai com 128+sinal, a menos que o programa tenha assumido os sinais com `sys.signal_notify` (aí o programa decide, e `sys.exit` limpa). Sem o tratador o Go sairia sem rodar os `defer`, e como o filho está no próprio grupo o Ctrl+C do terminal não o alcança. Morte dura do Noxy (SIGKILL, crash do Go): Linux mata o líder pelo `Pdeathsig`; Windows fecha o job com o processo. Netos em Unix sobrevivem a uma morte dura — best effort, como as extensões.

### 4.4 Erros

- `start` que não sobe (shell ausente, limite de processos): `ok=false`, `error` com a causa, `handle=-1`; nada registrado.
- `read`/`poll`/`wait`/`wait_for`/`terminate`/`kill` sobre `ok=false` ou handle já fechado: erro de runtime `process: handle N is not an open process`.
- `wait_for` com `timeout_ms < 0`: erro de runtime `process: wait_for: timeout must be >= 0`.

## 5. Implementação

| Arquivo | Estado | Responsabilidade |
|---|---|---|
| `internal/vm/builtins_process.go` | novo | `defineProcessBuiltins()` (nativos), `ProcessResource`, `startChildProcess`, `drain`/`reap`/`take`, `incompleteUTF8Tail`, `SharedState.CloseProcesses` |
| `internal/vm/process_tree_unix.go` (`unix`) | novo | `processTree` por pgid (`signal`, `release`), `configureProcessGroupUnguarded` |
| `internal/vm/process_tree_linux.go` (`linux`) | novo | `configureProcessGroup` (`Setpgid` + `Pdeathsig`), `deathGuardRefused` |
| `internal/vm/process_tree_unix_other.go` (`unix && !linux`) | novo | `configureProcessGroup` sem guarda |
| `cmd/noxy/signals.go` | novo | tratador de Ctrl+C/`SIGTERM` (`installExitSignalHandler`, `exitOnSignal`) |
| `internal/vm/process_tree_windows.go` | novo | job object (`x/sys/windows`), `processTree` por job |
| `internal/vm/vm.go`, `builtins.go` | modificar | `Processes *handleRegistry[*ProcessResource]` no `SharedState` e sua construção; `defineProcessBuiltins` na lista |
| `internal/vm/builtins_sys.go` | modificar | `sys_exit` chama `CloseProcesses` |
| `cmd/noxy/main.go` | modificar | `defer machine.CloseProcesses()` ao lado de `CloseExtensions` (CLI e REPL) |
| `internal/stdlib/process.nx` | novo | wrappers tipados |
| `internal/vm/builtins_registry_test.go`, `architecture_test.go` | modificar | snapshot (`process_*`), `builtins_process.go`/`defineProcessBuiltins`, registry `Processes` |
| `docs/NOXY_LANGUAGE_SPEC.md` §12, `CHANGELOG.md`, `noxy_examples/process_supervise.nx` | modificar/novo | documentação e exemplo (no runner) |

## 6. Testes

Go, em `internal/vm/builtins_process_test.go`, com comandos de shell portáteis (`sleep N` / `ping -n N+1 127.0.0.1 >nul`): saída completa após `wait`; `read` incremental e não bloqueante durante o `sleep`; `poll` → `terminate` → `wait_for` com exit code de sinal; `close` mata um neto (Unix, pid lido da saída de `sleep 30 & echo $!`); operações num handle fechado são erro de runtime; `incompleteUTF8Tail` e `take` (unidade); `CloseProcesses` esvazia o registry e o líder está morto; `-race` no CI já cobre `internal/vm`.

## 7. Documentação

Spec §12: linha na tabela de módulos e seção `### Processes (`process`)` com a API, a regra da fronteira UTF-8, a diferença EOF × fim do líder, a semântica de `close` e o best effort da morte dura. CHANGELOG 0.26.0: entrada em *Added* e o parágrafo de abertura deixa de dizer que o achado 1 ficou para depois.
