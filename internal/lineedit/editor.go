// Package lineedit e um editor de linha minimo, no espirito do readline, para
// o REPL em terminais POSIX. No Windows o console em modo cooked ja edita a
// linha e guarda historico sozinho; no Linux/macOS o modo canonico do tty so
// entende Backspace/^U/^W — uma seta chega como os bytes literais "ESC [ A".
// O editor poe o tty em modo raw enquanto le UMA linha, desenha o prompt e o
// texto por conta propria e devolve o terminal ao modo anterior antes de
// retornar, para o programa Noxy (input(), io.read_line) e o shell verem o
// modo cooked de sempre.
//
// A logica de edicao e independente de SO (reader/writer/Terminal injetados),
// o que permite testa-la sem tty; so a troca de modo e a largura ficam no
// Terminal da plataforma.
package lineedit

import (
	"errors"
	"io"
	"strconv"
	"unicode/utf8"
)

// ErrInterrupt e devolvido por ReadLine quando o usuario digita Ctrl-C: a
// linha em edicao foi descartada e o terminal ja esta restaurado.
var ErrInterrupt = errors.New("lineedit: interrupted")

// Terminal abstrai o que o editor precisa do tty: entrar em modo raw (com a
// funcao que desfaz a troca) e a largura em colunas.
type Terminal interface {
	MakeRaw() (restore func() error, err error)
	Width() int
}

// Editor le linhas editaveis de um terminal. Nao e seguro para uso
// concorrente.
type Editor struct {
	in   io.Reader
	out  io.Writer
	term Terminal

	// pending guarda bytes lidos alem da linha atual (colagem de varias
	// linhas): a proxima ReadLine consome-os antes de voltar ao reader.
	pending []byte

	// history guarda as linhas aceitas na sessao (sem vazias nem repeticao
	// consecutiva); histIdx == len(history) e a linha nova em edicao, e
	// draft preserva essa linha enquanto o usuario navega pelo historico.
	history []string
	histIdx int
	draft   []rune

	// Estado da linha em edicao.
	prompt      string
	promptWidth int
	line        []rune
	cursor      int // indice em runas
	start       int // primeira runa visivel (scroll horizontal)
}

// New cria um editor que le de in, desenha em out e controla o modo via term.
func New(in io.Reader, out io.Writer, term Terminal) *Editor {
	return &Editor{in: in, out: out, term: term}
}

// ReadLine desenha o prompt, edita uma linha ate Enter e a devolve sem o
// terminador. Erros: io.EOF quando a entrada acaba (ou Ctrl-D numa linha
// vazia), ErrInterrupt em Ctrl-C. Em qualquer saida o terminal volta ao modo
// em que estava.
func (e *Editor) ReadLine(prompt string) (string, error) {
	restore, err := e.term.MakeRaw()
	if err != nil {
		return "", err
	}
	defer restore()

	e.prompt = prompt
	e.promptWidth = visibleWidth(prompt)
	e.line = e.line[:0]
	e.cursor = 0
	e.start = 0
	e.histIdx = len(e.history)
	e.refresh()

	for {
		r, err := e.readRune()
		if err != nil {
			e.write("\r\n")
			return string(e.line), err
		}
		switch r {
		case '\r', '\n':
			e.write("\r\n")
			line := string(e.line)
			e.remember(line)
			return line, nil
		case 0x03: // Ctrl-C: descarta a linha, sem historico
			e.write("^C\r\n")
			return "", ErrInterrupt
		case 0x04: // Ctrl-D: EOF na linha vazia, senao Delete
			if len(e.line) == 0 {
				e.write("\r\n")
				return "", io.EOF
			}
			e.deleteUnderCursor()
		case 0x10: // Ctrl-P
			e.historyPrev()
		case 0x0e: // Ctrl-N
			e.historyNext()
		case 0x7f, 0x08: // DEL, BS
			e.backspace()
		case 0x1b: // ESC
			if err := e.escape(); err != nil {
				e.write("\r\n")
				return string(e.line), err
			}
		case 0x01: // Ctrl-A
			e.cursor = 0
		case 0x05: // Ctrl-E
			e.cursor = len(e.line)
		case 0x02: // Ctrl-B
			e.moveLeft()
		case 0x06: // Ctrl-F
			e.moveRight()
		case 0x0b: // Ctrl-K: apaga do cursor ao fim
			e.line = e.line[:e.cursor]
		case 0x15: // Ctrl-U: apaga do inicio ao cursor
			e.line = append(e.line[:0], e.line[e.cursor:]...)
			e.cursor = 0
		case 0x17: // Ctrl-W
			e.deleteWord()
		case 0x0c: // Ctrl-L
			e.write("\x1b[H\x1b[2J")
		case '\t':
			for i := 0; i < 4; i++ {
				e.insert(' ')
			}
		default:
			if r >= 0x20 {
				e.insert(r)
			}
		}
		e.refresh()
	}
}

// escape consome uma sequencia de escape (CSI "ESC [ ... final" ou SS3
// "ESC O x") e aplica a acao correspondente; sequencias desconhecidas sao
// engolidas em silencio.
func (e *Editor) escape() error {
	b, err := e.readByte()
	if err != nil {
		return err
	}
	switch b {
	case '[':
		var params []byte
		for {
			c, err := e.readByte()
			if err != nil {
				return err
			}
			if c >= 0x40 && c <= 0x7e {
				e.csi(params, c)
				return nil
			}
			params = append(params, c)
		}
	case 'O':
		c, err := e.readByte()
		if err != nil {
			return err
		}
		e.csi(nil, c)
	}
	return nil
}

// csi executa o byte final de uma sequencia CSI/SS3. params sao os bytes
// entre "ESC [" e o final ("3" em "ESC [ 3 ~").
func (e *Editor) csi(params []byte, final byte) {
	switch final {
	case 'A':
		e.historyPrev()
	case 'B':
		e.historyNext()
	case 'C':
		e.moveRight()
	case 'D':
		e.moveLeft()
	case 'H':
		e.cursor = 0
	case 'F':
		e.cursor = len(e.line)
	case '~':
		switch string(params) {
		case "1", "7": // Home (vt/rxvt)
			e.cursor = 0
		case "4", "8": // End (vt/rxvt)
			e.cursor = len(e.line)
		case "3": // Delete
			e.deleteUnderCursor()
		}
	}
}

func (e *Editor) insert(r rune) {
	e.line = append(e.line, 0)
	copy(e.line[e.cursor+1:], e.line[e.cursor:])
	e.line[e.cursor] = r
	e.cursor++
}

func (e *Editor) backspace() {
	if e.cursor == 0 {
		return
	}
	e.line = append(e.line[:e.cursor-1], e.line[e.cursor:]...)
	e.cursor--
}

func (e *Editor) deleteUnderCursor() {
	if e.cursor >= len(e.line) {
		return
	}
	e.line = append(e.line[:e.cursor], e.line[e.cursor+1:]...)
}

// remember acrescenta a linha aceita ao historico, pulando vazias e a
// repeticao imediata da ultima entrada.
func (e *Editor) remember(line string) {
	if line == "" {
		return
	}
	if n := len(e.history); n > 0 && e.history[n-1] == line {
		return
	}
	e.history = append(e.history, line)
}

// historyPrev troca a linha em edicao pela entrada anterior do historico,
// guardando a linha nova (draft) na primeira subida para o historyNext
// devolve-la.
func (e *Editor) historyPrev() {
	if e.histIdx == 0 {
		return
	}
	if e.histIdx == len(e.history) {
		e.draft = append(e.draft[:0], e.line...)
	}
	e.histIdx--
	e.setLine([]rune(e.history[e.histIdx]))
}

// historyNext anda para a entrada seguinte; ao passar da ultima volta a
// linha nova que estava em edicao (draft).
func (e *Editor) historyNext() {
	if e.histIdx >= len(e.history) {
		return
	}
	e.histIdx++
	if e.histIdx == len(e.history) {
		e.setLine(e.draft)
		return
	}
	e.setLine([]rune(e.history[e.histIdx]))
}

func (e *Editor) setLine(text []rune) {
	e.line = append(e.line[:0], text...)
	e.cursor = len(e.line)
}

// deleteWord apaga a palavra antes do cursor (espacos imediatamente antes do
// cursor incluidos), como o Ctrl-W do readline/tty.
func (e *Editor) deleteWord() {
	end := e.cursor
	i := e.cursor
	for i > 0 && e.line[i-1] == ' ' {
		i--
	}
	for i > 0 && e.line[i-1] != ' ' {
		i--
	}
	e.line = append(e.line[:i], e.line[end:]...)
	e.cursor = i
}

func (e *Editor) moveLeft() {
	if e.cursor > 0 {
		e.cursor--
	}
}

func (e *Editor) moveRight() {
	if e.cursor < len(e.line) {
		e.cursor++
	}
}

// refresh redesenha a linha inteira: volta ao inicio da linha fisica,
// escreve prompt + trecho visivel, apaga o resto e posiciona o cursor. Linhas
// mais largas que o terminal rolam horizontalmente (o trecho visivel segue o
// cursor), o que evita lidar com quebra de linha do terminal.
func (e *Editor) refresh() {
	cols := e.term.Width()
	if cols <= 0 {
		cols = 80
	}
	avail := cols - e.promptWidth - 1
	if avail < 1 {
		avail = 1
	}
	if e.cursor < e.start {
		e.start = e.cursor
	}
	if e.cursor >= e.start+avail {
		e.start = e.cursor - avail + 1
	}
	end := e.start + avail
	if end > len(e.line) {
		end = len(e.line)
	}
	buf := make([]byte, 0, 64)
	buf = append(buf, '\r')
	buf = append(buf, e.prompt...)
	buf = append(buf, string(e.line[e.start:end])...)
	buf = append(buf, "\x1b[K\r"...)
	if col := e.promptWidth + e.cursor - e.start; col > 0 {
		buf = append(buf, "\x1b["...)
		buf = strconv.AppendInt(buf, int64(col), 10)
		buf = append(buf, 'C')
	}
	e.out.Write(buf)
}

func (e *Editor) write(s string) {
	io.WriteString(e.out, s)
}

// readRune le uma runa UTF-8 (ou um byte de controle) da entrada.
func (e *Editor) readRune() (rune, error) {
	var buf [utf8.UTFMax]byte
	n := 0
	for {
		b, err := e.readByte()
		if err != nil {
			return 0, err
		}
		buf[n] = b
		n++
		if utf8.FullRune(buf[:n]) {
			r, _ := utf8.DecodeRune(buf[:n])
			return r, nil
		}
		if n == len(buf) {
			return utf8.RuneError, nil
		}
	}
}

// readByte devolve o proximo byte, servindo primeiro o que sobrou de uma
// leitura anterior. Em modo raw (VMIN=1) Read bloqueia ate haver ao menos um
// byte e devolve o que estiver disponivel — numa colagem, varias linhas de
// uma vez.
func (e *Editor) readByte() (byte, error) {
	for len(e.pending) == 0 {
		var buf [64]byte
		n, err := e.in.Read(buf[:])
		if n > 0 {
			e.pending = append(e.pending, buf[:n]...)
			break
		}
		if err != nil {
			return 0, err
		}
	}
	b := e.pending[0]
	e.pending = e.pending[1:]
	return b, nil
}

// visibleWidth conta as colunas que o prompt ocupa, ignorando sequencias de
// escape ANSI (cor) — uma coluna por runa.
func visibleWidth(s string) int {
	const (
		text = iota
		afterESC
		inCSI
	)
	width, state := 0, text
	for _, r := range s {
		switch state {
		case afterESC:
			if r == '[' {
				state = inCSI
			} else {
				state = text // ESC x: sequencia de um caractere
			}
		case inCSI:
			if r >= 0x40 && r <= 0x7e {
				state = text
			}
		default:
			if r == 0x1b {
				state = afterESC
			} else {
				width++
			}
		}
	}
	return width
}
