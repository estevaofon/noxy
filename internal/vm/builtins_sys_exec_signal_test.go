package vm

import (
	"io"
	"os"
	"testing"
)

// sys.exec (stdout herdado, só exit code/ok) e sys.signal_notify/signal_stop
// (assinatura de SIGINT/SIGTERM num canal Noxy) não tinham teste. `go` está no
// PATH de quem roda `go test`, então `go version` é um comando portátil de
// sucesso e `go nao-existe` um de falha (exit 2, como documenta o próprio go).

func TestSysExecReportsExitCodeAndOkWithoutCapturingOutput(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan string)
	go func() {
		out, _ := io.ReadAll(reader)
		done <- string(out)
	}()
	previous := os.Stdout
	os.Stdout = writer
	got := captureVMSource(t, `
use sys
let ok: sys.SysResult = sys.exec("go version")
let bad: sys.SysResult = sys.exec("go nao-existe-este-subcomando")
test_report([to_str(ok.ok), to_str(ok.exit_code), ok.output, ok.error, to_str(bad.ok), to_str(bad.exit_code), bad.output])
`)
	_ = writer.Close()
	os.Stdout = previous
	stdout := <-done
	want := []string{"true", "0", "", "", "false", "2", ""}
	cells := semArray(t, got)
	if len(cells) != len(want) {
		t.Fatalf("células=%d, want %d: %s", len(cells), len(want), got.String())
	}
	for i, cell := range cells {
		if s, ok := cell.Obj.(string); !ok || s != want[i] {
			t.Fatalf("célula %d: got %s, want %q (tudo: %s)", i, cell.String(), want[i], got.String())
		}
	}
	// A saída do filho vai para o stdout do processo, não para o resultado.
	if len(stdout) == 0 {
		t.Fatal("sys.exec deveria ter deixado a saída de 'go version' no stdout herdado")
	}
}

func TestSysSignalNotifyAndStopLifecycle(t *testing.T) {
	got := captureVMSource(t, `
use sys
let ch: chan int = make_chan(1)
let first: bool = sys.signal_notify(ch)
let again: bool = sys.signal_notify(ch)
let stopped: bool = sys.signal_stop()
let stopped_again: bool = sys.signal_stop()
let not_a_chan: any = 1
let rejected: bool = sys.signal_notify(not_a_chan)
test_report([first, again, stopped, stopped_again, rejected])
`)
	want := []bool{true, true, true, false, false}
	cells := semArray(t, got)
	if len(cells) != len(want) {
		t.Fatalf("células=%d, want %d: %s", len(cells), len(want), got.String())
	}
	for i, cell := range cells {
		if cell.AsBool != want[i] {
			t.Fatalf("célula %d: got %s, want %v (tudo: %s)", i, cell.String(), want[i], got.String())
		}
	}
}
