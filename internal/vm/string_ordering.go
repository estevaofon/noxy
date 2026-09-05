package vm

import "github.com/estevaofon/noxy/internal/value"

// stringOperands reconhece o par (string, string) para os opcodes de
// ordenacao OP_GREATER/OP_LESS. Segue o mesmo criterio do branch de strings
// de OP_ADD: ambos VAL_OBJ carregando Go string. bytes (VAL_BYTES) ficam
// deliberadamente de fora — ordena-los exige a ponte explicita to_str,
// como no resto da stdlib.
func stringOperands(a, b value.Value) (string, string, bool) {
	if a.Type != value.VAL_OBJ || b.Type != value.VAL_OBJ {
		return "", "", false
	}
	strA, okA := a.Obj.(string)
	strB, okB := b.Obj.(string)
	if !okA || !okB {
		return "", "", false
	}
	return strA, strB, true
}
