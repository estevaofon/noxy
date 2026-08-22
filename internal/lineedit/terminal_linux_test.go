//go:build linux

package lineedit

import (
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// openPTY abre um par pty (mestre/escravo) via /dev/ptmx. O escravo faz o
// papel do terminal do usuario; o mestre e "o teclado e a tela".
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	mfd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no /dev/ptmx: %v", err)
	}
	if err := unix.IoctlSetPointerInt(mfd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatalf("unlockpt: %v", err)
	}
	n, err := unix.IoctlGetInt(mfd, unix.TIOCGPTN)
	if err != nil {
		t.Fatalf("ptsname: %v", err)
	}
	master = os.NewFile(uintptr(mfd), "ptmx")
	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open slave: %v", err)
	}
	t.Cleanup(func() { slave.Close(); master.Close() })
	return master, slave
}

func TestNewUnixTerminalRejectsNonTTY(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if _, err := newUnixTerminal(int(r.Fd())); err == nil {
		t.Fatal("pipe accepted as terminal")
	}
}

func TestMakeRawDisablesCanonicalModeAndRestoreUndoesIt(t *testing.T) {
	_, slave := openPTY(t)
	fd := int(slave.Fd())
	before, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	term, err := newUnixTerminal(fd)
	if err != nil {
		t.Fatal(err)
	}
	restore, err := term.MakeRaw()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	if raw.Lflag&(unix.ICANON|unix.ECHO|unix.ISIG|unix.IEXTEN) != 0 {
		t.Fatalf("raw Lflag=%#x still has ICANON/ECHO/ISIG/IEXTEN", raw.Lflag)
	}
	if raw.Iflag&(unix.ICRNL|unix.IXON) != 0 {
		t.Fatalf("raw Iflag=%#x still has ICRNL/IXON", raw.Iflag)
	}
	// OPOST fica ligado: saida de outras goroutines (tasks Noxy) durante a
	// edicao continua com "\n" -> "\r\n".
	if raw.Oflag&unix.OPOST == 0 {
		t.Fatalf("raw mode must keep OPOST; Oflag=%#x", raw.Oflag)
	}
	if raw.Cc[unix.VMIN] != 1 || raw.Cc[unix.VTIME] != 0 {
		t.Fatalf("VMIN/VTIME = %d/%d, want 1/0", raw.Cc[unix.VMIN], raw.Cc[unix.VTIME])
	}
	if err := restore(); err != nil {
		t.Fatal(err)
	}
	after, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	if *after != *before {
		t.Fatalf("termios not restored:\nbefore=%+v\nafter =%+v", *before, *after)
	}
}

func TestWidthReadsWindowSize(t *testing.T) {
	_, slave := openPTY(t)
	fd := int(slave.Fd())
	if err := unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Row: 24, Col: 57}); err != nil {
		t.Fatal(err)
	}
	term, err := newUnixTerminal(fd)
	if err != nil {
		t.Fatal(err)
	}
	if got := term.Width(); got != 57 {
		t.Fatalf("Width() = %d, want 57", got)
	}
}

func TestStdinReturnsNilWhenStdinIsNotATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	previous := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = previous })
	if ed := Stdin(); ed != nil {
		t.Fatal("Stdin() must be nil for a pipe")
	}
	_, slave := openPTY(t)
	os.Stdin = slave
	if ed := Stdin(); ed == nil {
		t.Fatal("Stdin() must return an editor for a pty")
	}
}

func TestReadLineThroughRealPTYHonorsArrowKeys(t *testing.T) {
	master, slave := openPTY(t)
	fd := int(slave.Fd())
	term, err := newUnixTerminal(fd)
	if err != nil {
		t.Fatal(err)
	}
	// Entra em raw antes de "digitar", para a disciplina de linha nao
	// processar os bytes (ICRNL, eco) no modo canonico enquanto esperam.
	restore, err := term.MakeRaw()
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	if _, err := master.WriteString("ab\x1b[Dc\r"); err != nil {
		t.Fatal(err)
	}
	ed := New(slave, slave, term)
	line, err := ed.ReadLine(">>> ")
	if err != nil {
		t.Fatal(err)
	}
	if line != "acb" {
		t.Fatalf("line = %q, want %q", line, "acb")
	}
}
