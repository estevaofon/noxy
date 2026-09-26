//go:build unix

package vm

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// processGone diz se pid nao existe mais — ou e um zumbi que ninguem
// colheu (num container sem init, kill(pid, 0) ainda responde por ele).
func processGone(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return true
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false // sem /proc (macOS): so o kill(0) decide
	}
	// "pid (comm) S ..." — o estado vem depois do ultimo ')'.
	text := string(stat)
	if i := strings.LastIndex(text, ")"); i >= 0 && len(text) > i+2 {
		return text[i+2] == 'Z'
	}
	return false
}

func waitProcessGone(t *testing.T, pid int, what string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if processGone(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s (pid %d) continua vivo", what, pid)
}

// firstLinePid le a primeira linha da saida (o pid ecoado pelo shell) com
// um laco limitado em vez de um sleep fixo: numa maquina carregada o shell
// pode levar mais de 300 ms para escrever.
const readFirstLine = `
let first: bytes = b""
let tries: int = 0
while length(first) == 0 && tries < 200 do
    first = first + process.read(p)
    tries = tries + 1
    sys.sleep(25)
end
`

func pidFromCell(t *testing.T, cell string) int {
	t.Helper()
	line := strings.TrimSpace(strings.SplitN(cell, "\n", 2)[0])
	pid, err := strconv.Atoi(line)
	if err != nil {
		t.Fatalf("pid do neto ilegivel em %q: %v", cell, err)
	}
	return pid
}

// close mata a arvore, nao so o shell: o pid de um `sleep` em background
// lido da saida do lider deixa de existir depois do close — com o lider
// ainda vivo (`wait`).
func TestProcessCloseKillsTheWholeGroup(t *testing.T) {
	cells := processCells(t, `
use process
use sys
let p: process.Process = process.start("sleep 30 & echo $!; wait")`+readFirstLine+`
process.close(p)
test_report([to_str(first)])`)
	waitProcessGone(t, pidFromCell(t, cells[0]), "o neto")
}

// Spec §4.2, a condicao que o close distingue: lider ja saiu (wait = 0,
// poll nao esta running), o neto herdou o pipe e continua vivo; close
// ainda alcanca o grupo.
func TestProcessCloseKillsAGrandchildAfterTheLeaderExited(t *testing.T) {
	cells := processCells(t, `
use process
use sys
let p: process.Process = process.start("sleep 30 & echo $!")
let code: int = process.wait(p)
let running: bool = process.poll(p).running`+readFirstLine+`
process.close(p)
test_report([to_str(first), to_str(code), to_str(running)])`)
	if cells[1] != "0" || cells[2] != "false" {
		t.Fatalf("wait/poll com neto vivo = %v, want [_ 0 false]", cells)
	}
	waitProcessGone(t, pidFromCell(t, cells[0]), "o neto")
}

// terminate tambem alcanca o grupo depois que o lider saiu, enquanto o
// pipe (e portanto o grupo) continua aberto — senao um `server &` so
// poderia ser parado por close, que e SIGKILL.
func TestProcessTerminateReachesAGrandchildAfterTheLeaderExited(t *testing.T) {
	cells := processCells(t, `
use process
use sys
let p: process.Process = process.start("sleep 30 & echo $!")
let code: int = process.wait(p)`+readFirstLine+`
let asked: bool = process.terminate(p)
test_report([to_str(first), to_str(code), to_str(asked)])`)
	if cells[1] != "0" || cells[2] != "true" {
		t.Fatalf("wait/terminate com neto vivo = %v, want [_ 0 true]", cells)
	}
	waitProcessGone(t, pidFromCell(t, cells[0]), "o neto")
}

// poll nunca bloqueia: a carencia de 100 ms para o pipe drenar depois que
// o lider saiu e paga uma vez por processo, nao a cada chamada — com um
// neto segurando o pipe, dez polls levavam um segundo.
func TestExitStatusPaysTheDrainGraceOnlyOnce(t *testing.T) {
	resource, err := startChildProcess("sleep 30 & echo $!")
	if err != nil {
		t.Fatal(err)
	}
	defer resource.shutdown()
	<-resource.done
	resource.exitStatus() // primeira observacao: pode esperar a carencia
	started := time.Now()
	for i := 0; i < 10; i++ {
		if running, _ := resource.exitStatus(); running {
			t.Fatal("lider reaped mas exitStatus diz running")
		}
	}
	if elapsed := time.Since(started); elapsed > 300*time.Millisecond {
		t.Fatalf("10 polls levaram %v; a carencia esta sendo paga a cada chamada", elapsed)
	}
}

// Um escritor que saiu do grupo (setsid) e ficou com o pipe nao pode
// deixar a goroutine de drain, o fd e o buffer vivos depois do close:
// shutdown fecha o lado de leitura e a drain termina.
func TestShutdownReleasesTheReaderWhenAWriterEscapedTheGroup(t *testing.T) {
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("setsid indisponivel")
	}
	resource, err := startChildProcess("setsid sh -c 'echo $$; exec sleep 30' & wait")
	if err != nil {
		t.Fatal(err)
	}
	var escaped int
	deadline := time.Now().Add(3 * time.Second)
	for escaped == 0 && time.Now().Before(deadline) {
		if line := strings.TrimSpace(string(resource.take())); line != "" {
			escaped, _ = strconv.Atoi(strings.SplitN(line, "\n", 2)[0])
		}
		time.Sleep(25 * time.Millisecond)
	}
	if escaped == 0 {
		t.Fatal("o pid do escritor fora do grupo nao chegou")
	}
	defer func() { _ = syscall.Kill(escaped, syscall.SIGKILL) }()
	resource.shutdown()
	select {
	case <-resource.drained:
	case <-time.After(2 * time.Second):
		t.Fatal("drain continua bloqueada no pipe depois do shutdown")
	}
}
