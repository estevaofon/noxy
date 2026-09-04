package compiler

import "testing"

// Issue #133 (revisao adversarial, caso 3): `m.chave = v` sobre um valor de
// tipo estatico map[K, V] e a MESMA escrita que `m["chave"] = v` e recebe a
// mesma checagem. O ramo de mapa que o #133 acrescentou ao OP_SET_PROPERTY
// tornou a forma com ponto executavel para qualquer mapa; sem checagem, um
// `map[string, int]` passava a guardar uma string, e o `int` lido dela
// contaminava o programa inteiro. A LEITURA com ponto continua dinamica
// (memberType devolve nil para dono map) — fora de escopo aqui.

func TestMapDotWriteChecksTheValueType(t *testing.T) {
	_, err := compileFunctionSource(t, "let mm: map[string, int] = {\"a\": 1}\nmm.a = \"boom\"\n")
	requireErrorMentions(t, err, "[line 2]", "type mismatch in map value: expected int, got string")
}

func TestMapDotWriteAcceptsTheDeclaredValueType(t *testing.T) {
	_, err := compileFunctionSource(t, "let mm: map[string, int] = {\"a\": 1}\nmm.a = 2\n")
	requireNoError(t, err)
}

func TestMapDotWriteChecksTheKeyType(t *testing.T) {
	// A chave escrita com ponto e sempre uma string: um mapa de chave int
	// nao tem como recebe-la.
	_, err := compileFunctionSource(t, "let mk: map[int, int] = {}\nmk.a = 1\n")
	requireErrorMentions(t, err, "[line 2]", "type mismatch in map key: expected int, got string")
}

func TestMapDotWriteRejectsPlainValueInReferenceSlot(t *testing.T) {
	// Mesma regra do caminho indexado: slot de valor `ref T` so aceita ref.
	_, err := compileFunctionSource(t, "let n: int = 1\nlet mr: map[string, ref int] = {\"a\": ref n}\nmr.a = 5\n")
	requireErrorMentions(t, err, "[line 3]", "cannot assign int to ref int")
}
