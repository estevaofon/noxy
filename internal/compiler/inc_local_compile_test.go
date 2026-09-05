package compiler

import (
	"testing"

	"github.com/estevaofon/noxy/internal/chunk"
)

// TestIncLocalIntFuses prova, por bytecode, que `i = i + 1` sobre um local int
// possuidor emite OP_INC_LOCAL_INT e NAO cai no caminho generico
// (OP_SET_LOCAL/OP_ADD_INT ausentes). O comportamento (valor final correto) e
// coberto por TestIncLocalIntBehavior no pacote vm; aqui so travamos que a
// fusao de fato dispara.
func TestIncLocalIntFuses(t *testing.T) {
	source := `
func f() -> int
    let i: int = 0
    i = i + 1
    i = i - 3
    return i
end
`
	fn := compiledFunction(t, source, "f")
	code := fn.Chunk.(*chunk.Chunk).Code
	if !containsOpcode(code, chunk.OP_INC_LOCAL_INT) {
		t.Fatalf("`i = i +- K` nao fundiu: OP_INC_LOCAL_INT ausente do bytecode")
	}
	if containsOpcode(code, chunk.OP_SET_LOCAL) {
		t.Fatalf("`i = i +- K` caiu no caminho generico (OP_SET_LOCAL presente) apesar de i ser int possuidor")
	}
}

// TestIncLocalIntDoesNotFuseForGlobal confirma que a mesma forma sintatica
// `x = x + 1` sobre uma variavel GLOBAL (sem slot local) nao aciona a fusao —
// resolveLocal nao encontra slot, entao tryFuseLocalIntIncrement desiste
// antes de qualquer checagem de tipo/posse.
func TestIncLocalIntDoesNotFuseForGlobal(t *testing.T) {
	source := `
let x: int = 0
func f() -> int
    x = x + 1
    return x
end
`
	fn := compiledFunction(t, source, "f")
	code := fn.Chunk.(*chunk.Chunk).Code
	if containsOpcode(code, chunk.OP_INC_LOCAL_INT) {
		t.Fatalf("`x = x + 1` sobre global fundiu incorretamente em OP_INC_LOCAL_INT")
	}
}

// TestIncLocalIntDoesNotFuseAcrossDifferentIdentifiers garante que a checagem
// sintatica do lado esquerdo do infix (left.Value == ident.Value) rejeita
// `i = j + 1` — nao e um incremento de i, e fundir aqui perderia o valor de j.
func TestIncLocalIntDoesNotFuseAcrossDifferentIdentifiers(t *testing.T) {
	source := `
func f() -> int
    let i: int = 0
    let j: int = 5
    i = j + 1
    return i
end
`
	fn := compiledFunction(t, source, "f")
	code := fn.Chunk.(*chunk.Chunk).Code
	if containsOpcode(code, chunk.OP_INC_LOCAL_INT) {
		t.Fatalf("`i = j + 1` fundiu incorretamente em OP_INC_LOCAL_INT (lado direito nao e o proprio i)")
	}
}

// TestIncLocalIntDoesNotFuseDeltaOutOfRange garante que um delta fora de
// [-128,127] cai no caminho generico em vez de estourar o operando i8.
func TestIncLocalIntDoesNotFuseDeltaOutOfRange(t *testing.T) {
	source := `
func f() -> int
    let i: int = 0
    i = i + 200
    return i
end
`
	fn := compiledFunction(t, source, "f")
	code := fn.Chunk.(*chunk.Chunk).Code
	if containsOpcode(code, chunk.OP_INC_LOCAL_INT) {
		t.Fatalf("`i = i + 200` fundiu incorretamente em OP_INC_LOCAL_INT (delta fora de [-128,127])")
	}
	if !containsOpcode(code, chunk.OP_SET_LOCAL) {
		t.Fatalf("`i = i + 200` nao caiu no caminho generico (OP_SET_LOCAL ausente)")
	}
}
