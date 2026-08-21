package compiler

import (
	"strings"
	"testing"
)

// Depois que o for termina ele nao pode continuar sendo o laco corrente: um
// break solto depois dele tem de ser rejeitado como qualquer break fora de
// laco (antes o laco ficava empilhado e o break gerava um jump nunca patchado).
func TestCompileBreakAfterForLoopIsRejectedOutsideLoop(t *testing.T) {
	_, _, err := New().Compile(parse("for item in [1, 2] do\n    print(item)\nend\nbreak\n"))
	if err == nil || !strings.Contains(err.Error(), "break outside of loop") {
		t.Fatalf("error=%v, want \"break outside of loop\"", err)
	}
}

func TestCompileContinueOutsideLoopIsRejected(t *testing.T) {
	_, _, err := New().Compile(parse("continue\n"))
	if err == nil || !strings.Contains(err.Error(), "continue outside of loop") {
		t.Fatalf("error=%v, want \"continue outside of loop\"", err)
	}
}

func TestCompileContinueInsideWhileAndFor(t *testing.T) {
	for _, source := range []string{
		"let i: int = 0\nwhile i < 3 do\n    i = i + 1\n    if i == 2 then continue end\n    print(i)\nend\n",
		"for item in [1, 2, 3] do\n    let dobro: int = item * 2\n    if dobro == 4 then continue end\n    print(dobro)\nend\n",
	} {
		if _, _, err := New().Compile(parse(source)); err != nil {
			t.Fatalf("source %q: %v", source, err)
		}
	}
}
