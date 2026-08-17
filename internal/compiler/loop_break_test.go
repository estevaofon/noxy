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
