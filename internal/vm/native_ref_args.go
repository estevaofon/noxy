package vm

import (
	"fmt"

	"noxy-vm/internal/value"
)

// rejectRefArgs e o gemeo dinamico de rejectRefArgumentsForValueNatives
// (internal/compiler/explicit_ref.go, R2): length/keys/slice/contains/
// has_key sao nativas sem NativeSignature, entao um `ref T` que atravessa a
// fronteira dinamica (base any — o compilador ja barra o caso estatico) nao
// passa por validateParameterModes. Sem esta checagem o native cairia no
// default silencioso do tipo nao reconhecido (0, [], false) — o bug do
// Task 10a. Confere TODOS os argumentos (slice/contains/has_key tem mais de
// um) e nomeia o primeiro ref encontrado pela posicao 1-based.
func rejectRefArgs(name string, args []value.Value) error {
	for i, arg := range args {
		if arg.Type == value.VAL_REF {
			return fmt.Errorf("%s: argument %d expected a value, got ref\n  hint: a ref is never read implicitly; use '*r'", name, i+1)
		}
	}
	return nil
}
