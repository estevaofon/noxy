package lineedit

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// fakeTerm registra as trocas de modo e devolve uma largura fixa, para os
// testes exercitarem o editor sem um tty de verdade.
type fakeTerm struct {
	width    int
	raw      int
	restored int
}

func (t *fakeTerm) MakeRaw() (func() error, error) {
	t.raw++
	return func() error { t.restored++; return nil }, nil
}

func (t *fakeTerm) Width() int { return t.width }

func newTestEditor(input string, width int) (*Editor, *bytes.Buffer, *fakeTerm) {
	out := &bytes.Buffer{}
	term := &fakeTerm{width: width}
	return New(strings.NewReader(input), out, term), out, term
}

func readOne(t *testing.T, input string) string {
	t.Helper()
	ed, _, _ := newTestEditor(input, 80)
	line, err := ed.ReadLine(">>> ")
	if err != nil {
		t.Fatalf("ReadLine(%q) error: %v", input, err)
	}
	return line
}

func TestReadLineReturnsTypedLineOnEnter(t *testing.T) {
	ed, out, _ := newTestEditor("print(1)\r", 80)
	line, err := ed.ReadLine(">>> ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if line != "print(1)" {
		t.Fatalf("line = %q, want %q", line, "print(1)")
	}
	if !strings.Contains(out.String(), ">>> ") {
		t.Fatalf("prompt not written; output = %q", out.String())
	}
	if !strings.Contains(out.String(), "print(1)") {
		t.Fatalf("typed text not echoed; output = %q", out.String())
	}
	if !strings.HasSuffix(out.String(), "\r\n") {
		t.Fatalf("Enter must end the line with CRLF; output = %q", out.String())
	}
}

func TestReadLineAcceptsLineFeedAsEnter(t *testing.T) {
	if got := readOne(t, "x\n"); got != "x" {
		t.Fatalf("line = %q, want %q", got, "x")
	}
}

func TestLeftArrowInsertsBeforeCursor(t *testing.T) {
	if got := readOne(t, "ac\x1b[Db\r"); got != "abc" {
		t.Fatalf("line = %q, want %q", got, "abc")
	}
}

func TestRightArrowMovesCursorForward(t *testing.T) {
	// "ac", volta duas, avanca uma, insere: a b c
	if got := readOne(t, "ac\x1b[D\x1b[D\x1b[Cb\r"); got != "abc" {
		t.Fatalf("line = %q, want %q", got, "abc")
	}
}

func TestBackspaceDeletesBeforeCursor(t *testing.T) {
	if got := readOne(t, "abc\x7f\r"); got != "ab" {
		t.Fatalf("DEL: line = %q, want %q", got, "ab")
	}
	if got := readOne(t, "abc\x08\r"); got != "ab" {
		t.Fatalf("BS: line = %q, want %q", got, "ab")
	}
	if got := readOne(t, "\x7f\x7fok\r"); got != "ok" {
		t.Fatalf("backspace on empty line must be a no-op; line = %q", got)
	}
}

func TestRawModeIsRestoredAfterReadLine(t *testing.T) {
	ed, _, term := newTestEditor("a\r", 80)
	if _, err := ed.ReadLine(">>> "); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if term.raw != 1 || term.restored != 1 {
		t.Fatalf("raw=%d restored=%d, want 1/1", term.raw, term.restored)
	}
}

func TestHomeAndEndMoveCursorToLineEdges(t *testing.T) {
	cases := map[string]string{
		"CSI H / CSI F": "bc\x1b[Ha\x1b[Fd\r",
		"CSI 1~ / 4~":   "bc\x1b[1~a\x1b[4~d\r",
		"CSI 7~ / 8~":   "bc\x1b[7~a\x1b[8~d\r",
		"SS3 H / F":     "bc\x1bOHa\x1bOFd\r",
		"Ctrl-A / E":    "bc\x01a\x05d\r",
	}
	for name, input := range cases {
		if got := readOne(t, input); got != "abcd" {
			t.Errorf("%s: line = %q, want %q", name, got, "abcd")
		}
	}
}

func TestCtrlBAndCtrlFMoveCursor(t *testing.T) {
	if got := readOne(t, "ac\x02b\x06d\r"); got != "abcd" {
		t.Fatalf("line = %q, want %q", got, "abcd")
	}
}

func TestDeleteRemovesCharUnderCursor(t *testing.T) {
	if got := readOne(t, "abc\x1b[D\x1b[D\x1b[3~\r"); got != "ac" {
		t.Fatalf("line = %q, want %q", got, "ac")
	}
	if got := readOne(t, "abc\x1b[3~\r"); got != "abc" {
		t.Fatalf("Delete at end must be a no-op; line = %q", got)
	}
}

func TestCtrlKKillsToEndOfLine(t *testing.T) {
	if got := readOne(t, "abcd\x1b[D\x1b[D\x0b\r"); got != "ab" {
		t.Fatalf("line = %q, want %q", got, "ab")
	}
}

func TestCtrlUKillsToStartOfLine(t *testing.T) {
	if got := readOne(t, "abcd\x1b[D\x15\r"); got != "d" {
		t.Fatalf("line = %q, want %q", got, "d")
	}
}

func TestCtrlWDeletesWordBeforeCursor(t *testing.T) {
	if got := readOne(t, "let x = foo\x17bar\r"); got != "let x = bar" {
		t.Fatalf("line = %q, want %q", got, "let x = bar")
	}
	// Espacos antes do cursor sao pulados antes de apagar a palavra.
	if got := readOne(t, "print(a)  \x17\r"); got != "" {
		t.Fatalf("line = %q, want empty", got)
	}
	if got := readOne(t, "a b c\x1b[D\x1b[D\x17\r"); got != "a  c" {
		t.Fatalf("mid-line: line = %q, want %q", got, "a  c")
	}
}

func TestCtrlLClearsScreenAndRedraws(t *testing.T) {
	ed, out, _ := newTestEditor("ab\x0c\r", 80)
	line, err := ed.ReadLine(">>> ")
	if err != nil || line != "ab" {
		t.Fatalf("line=%q err=%v", line, err)
	}
	if !strings.Contains(out.String(), "\x1b[H\x1b[2J") {
		t.Fatalf("clear-screen sequence missing; output = %q", out.String())
	}
	if !strings.HasSuffix(out.String(), "\x1b[H\x1b[2J\r>>> ab\x1b[K\r\x1b[6C\r\n") {
		t.Fatalf("line not redrawn after clear; output = %q", out.String())
	}
}

func TestTabInsertsFourSpaces(t *testing.T) {
	if got := readOne(t, "\tx\r"); got != "    x" {
		t.Fatalf("line = %q, want %q", got, "    x")
	}
}

// readAll le linhas ate erro e devolve o que veio, para testar historico e
// colagem (varias ReadLine no mesmo editor).
func readAll(t *testing.T, ed *Editor) ([]string, error) {
	t.Helper()
	var lines []string
	for {
		line, err := ed.ReadLine(">>> ")
		if err != nil {
			return lines, err
		}
		lines = append(lines, line)
	}
}

func TestUpArrowRecallsPreviousLines(t *testing.T) {
	ed, _, _ := newTestEditor("first\rsecond\r\x1b[A\r\x1b[A\x1b[A\r", 80)
	lines, err := readAll(t, ed)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
	want := []string{"first", "second", "second", "first"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
}

func TestDownArrowReturnsToLineBeingEdited(t *testing.T) {
	// Digita "new", sobe duas no historico, desce duas: volta a "new".
	ed, _, _ := newTestEditor("a\rb\rnew\x1b[A\x1b[A\x1b[B\x1b[B\r", 80)
	lines, _ := readAll(t, ed)
	if got := lines[len(lines)-1]; got != "new" {
		t.Fatalf("last line = %q, want %q (lines=%q)", got, "new", lines)
	}
}

func TestHistoryNavigationViaCtrlPAndCtrlNAndSS3(t *testing.T) {
	ed, _, _ := newTestEditor("a\rb\r\x10\x10\x0e\r\x1bOA\r", 80)
	lines, _ := readAll(t, ed)
	want := []string{"a", "b", "b", "b"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
}

func TestRecalledLineCanBeEdited(t *testing.T) {
	ed, _, _ := newTestEditor("print(1)\r\x1b[A\x7f\x7f2)\r", 80)
	lines, _ := readAll(t, ed)
	want := []string{"print(1)", "print(2)"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
}

func TestHistorySkipsEmptyAndConsecutiveDuplicates(t *testing.T) {
	ed, _, _ := newTestEditor("x\rx\r\r\x1b[A\x1b[A\r", 80)
	lines, _ := readAll(t, ed)
	// Depois de "x", "x", "" o historico e ["x"]: duas setas acima nao
	// passam dele.
	want := []string{"x", "x", "", "x"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
}

func TestUpArrowWithEmptyHistoryIsNoop(t *testing.T) {
	if got := readOne(t, "\x1b[A\x1b[Bok\r"); got != "ok" {
		t.Fatalf("line = %q, want %q", got, "ok")
	}
}

func TestCtrlCDiscardsLineAndReturnsErrInterrupt(t *testing.T) {
	ed, out, term := newTestEditor("abc\x03", 80)
	line, err := ed.ReadLine(">>> ")
	if !errors.Is(err, ErrInterrupt) {
		t.Fatalf("err = %v, want ErrInterrupt", err)
	}
	if line != "" {
		t.Fatalf("line = %q, want empty", line)
	}
	if !strings.HasSuffix(out.String(), "^C\r\n") {
		t.Fatalf("output must end with ^C and newline; got %q", out.String())
	}
	if term.restored != 1 {
		t.Fatalf("terminal not restored on Ctrl-C (restored=%d)", term.restored)
	}
}

func TestInterruptedLineIsNotAddedToHistory(t *testing.T) {
	ed, _, _ := newTestEditor("keep\rdrop\x03\x1b[A\r", 80)
	_, _ = ed.ReadLine(">>> ")
	_, err := ed.ReadLine(">>> ")
	if !errors.Is(err, ErrInterrupt) {
		t.Fatalf("err = %v, want ErrInterrupt", err)
	}
	line, err := ed.ReadLine(">>> ")
	if err != nil || line != "keep" {
		t.Fatalf("line=%q err=%v, want %q", line, err, "keep")
	}
}

func TestCtrlDOnEmptyLineReturnsEOF(t *testing.T) {
	ed, out, _ := newTestEditor("\x04", 80)
	_, err := ed.ReadLine(">>> ")
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
	if !strings.HasSuffix(out.String(), "\r\n") {
		t.Fatalf("Ctrl-D exit must move to a fresh line; got %q", out.String())
	}
}

func TestCtrlDWithTextDeletesUnderCursor(t *testing.T) {
	if got := readOne(t, "abc\x1b[D\x1b[D\x04\r"); got != "ac" {
		t.Fatalf("line = %q, want %q", got, "ac")
	}
}

func TestUTF8RunesAreInsertedAndDeletedWhole(t *testing.T) {
	if got := readOne(t, "çã\x7f\r"); got != "ç" {
		t.Fatalf("line = %q, want %q", got, "ç")
	}
	ed, out, _ := newTestEditor("é\r", 80)
	if _, err := ed.ReadLine(">>> "); err != nil {
		t.Fatal(err)
	}
	// Uma runa = uma coluna: cursor na coluna 5 (prompt 4 + 1).
	if !strings.HasSuffix(out.String(), "\r>>> é\x1b[K\r\x1b[5C\r\n") {
		t.Fatalf("cursor column must count runes, not bytes; got %q", out.String())
	}
}

func TestUnknownEscapeSequencesAreIgnored(t *testing.T) {
	// Bracketed paste start/end, F5, ESC+letra: nada disso vira texto.
	if got := readOne(t, "\x1b[200~a\x1b[201~\x1b[15~\x1bxb\r"); got != "ab" {
		t.Fatalf("line = %q, want %q", got, "ab")
	}
}

func TestPastedLinesAreServedAcrossCalls(t *testing.T) {
	ed, _, _ := newTestEditor("one\rtwo\rthree\r", 80)
	lines, err := readAll(t, ed)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
	want := []string{"one", "two", "three"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
}

func TestColoredPromptCountsOnlyVisibleColumns(t *testing.T) {
	ed, out, _ := newTestEditor("a\r", 80)
	prompt := "\x1b[1;35m>>> \x1b[0m"
	if _, err := ed.ReadLine(prompt); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out.String(), "\r"+prompt+"a\x1b[K\r\x1b[5C\r\n") {
		t.Fatalf("cursor must land at column 5 (4 visible + 1); got %q", out.String())
	}
}

func TestLongLineScrollsHorizontally(t *testing.T) {
	// Largura 10, prompt 4 => 5 colunas uteis. "abcdefgh" com o cursor no
	// fim mostra "efgh"; Home volta ao inicio e mostra "abcde".
	ed, out, _ := newTestEditor("abcdefgh\x1b[H\r", 10)
	if _, err := ed.ReadLine(">>> "); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "\r>>> efgh\x1b[K\r\x1b[8C") {
		t.Fatalf("end of long line not scrolled into view; got %q", s)
	}
	if !strings.HasSuffix(s, "\r>>> abcde\x1b[K\r\x1b[4C\r\n") {
		t.Fatalf("Home must scroll back to the start; got %q", s)
	}
}

func TestReadLineReturnsEOFWhenInputEnds(t *testing.T) {
	ed, _, term := newTestEditor("", 80)
	line, err := ed.ReadLine(">>> ")
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
	if line != "" {
		t.Fatalf("line = %q, want empty", line)
	}
	if term.restored != 1 {
		t.Fatalf("terminal not restored on EOF (restored=%d)", term.restored)
	}
}
