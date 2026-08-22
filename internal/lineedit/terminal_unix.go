//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package lineedit

import (
	"os"

	"golang.org/x/sys/unix"
)

// unixTerminal controla um tty POSIX via termios/ioctl (x/sys/unix, ja
// dependencia do projeto — sem x/term).
type unixTerminal struct {
	fd int
}

// newUnixTerminal devolve o terminal do descritor, ou erro se ele nao e um
// tty (pipe, arquivo): tcgetattr falha com ENOTTY.
func newUnixTerminal(fd int) (*unixTerminal, error) {
	if _, err := unix.IoctlGetTermios(fd, ioctlReadTermios); err != nil {
		return nil, err
	}
	return &unixTerminal{fd: fd}, nil
}

// MakeRaw desliga o modo canonico, o eco e os sinais de teclado (Ctrl-C vira
// byte), mantendo OPOST: saida de outras goroutines durante a edicao continua
// traduzindo "\n" em "\r\n". VMIN=1/VTIME=0: Read bloqueia ate haver ao menos
// um byte. A funcao devolvida restaura exatamente o termios anterior.
func (t *unixTerminal) MakeRaw() (func() error, error) {
	saved, err := unix.IoctlGetTermios(t.fd, ioctlReadTermios)
	if err != nil {
		return nil, err
	}
	raw := *saved
	raw.Iflag &^= unix.BRKINT | unix.ICRNL | unix.INLCR | unix.IGNCR | unix.ISTRIP | unix.IXON
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(t.fd, ioctlWriteTermios, &raw); err != nil {
		return nil, err
	}
	return func() error { return unix.IoctlSetTermios(t.fd, ioctlWriteTermios, saved) }, nil
}

// Width devolve as colunas do terminal, ou 0 se o ioctl falhar (o editor
// assume 80).
func (t *unixTerminal) Width() int {
	ws, err := unix.IoctlGetWinsize(t.fd, unix.TIOCGWINSZ)
	if err != nil {
		return 0
	}
	return int(ws.Col)
}

// Stdin devolve um editor sobre os.Stdin/os.Stdout quando stdin e um tty, ou
// nil quando nao e (pipe, arquivo) — nesse caso o chamador deve ler linhas
// do jeito convencional.
func Stdin() *Editor {
	term, err := newUnixTerminal(int(os.Stdin.Fd()))
	if err != nil {
		return nil
	}
	return New(os.Stdin, os.Stdout, term)
}
