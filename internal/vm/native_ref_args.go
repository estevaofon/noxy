package vm

import (
	"fmt"

	"github.com/estevaofon/noxy/internal/compiler"
	"github.com/estevaofon/noxy/internal/value"
)

// rejectRefArgs e o gemeo dinamico de rejectRefArgumentsForValueNatives
// (internal/compiler/explicit_ref.go, R2): as nativas de valor sao nativas
// sem NativeSignature, entao um `ref T` que atravessa a fronteira dinamica
// (base any — o compilador ja barra o caso estatico) nao passa por
// validateParameterModes. Sem esta checagem o native cairia no default
// silencioso do tipo nao reconhecido (0, [], false) ou codificaria o texto
// "<ref ...>" — o bug do Task 10a.
//
// QUAIS posicoes sao checadas vem da MESMA tabela do compilador
// (compiler.ValueNativeChecksArg): nas cinco colecoes so o argumento 1 (a
// colecao) e valor obrigatorio — o argumento 2 de contains/has_key pode ser
// um ref, procurado por identidade; nas nativas de codificacao/cripto todas
// as posicoes sao checadas. Um nome fora da tabela nao chega aqui; se
// chegasse, nada seria checado, entao acrescentar native nova exige tocar a
// tabela do compilador junto.
func rejectRefArgs(name string, args []value.Value) error {
	for i, arg := range args {
		if arg.Type != value.VAL_REF {
			continue
		}
		if !compiler.ValueNativeChecksArg(name, i) {
			continue
		}
		return fmt.Errorf("%s: argument %d expected a value, got ref\n  hint: a ref is never read implicitly; use '*r'", name, i+1)
	}
	return nil
}

// defineValueNative registra uma native de valor (R2) preservando byte a byte
// o registro de DefineNative — DefineLocalIfAbsent + stampReadonlyArgs — e so
// interpondo rejectRefArgs antes de fn. A forma contextual e necessaria
// porque value.NativeFunc nao tem como devolver erro; o Name e preenchido a
// mao porque value.NewContextualNative nao o guarda, e sem ele
// stampReadonlyArgs perderia o flag CoW da native convertida.
func (vm *VM) defineValueNative(name string, fn value.NativeFunc) {
	vm.registerValueNative(name, func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if err := rejectRefArgs(name, args); err != nil {
			return value.NewNull(), err
		}
		return fn(args), nil
	})
}

// defineValueNativeErr e defineValueNative para a native de valor que
// devolve erro tipado (issue #121: argumento invalido nas natives de cripto
// e erro, nao null) — mesmo registro, mesma rejectRefArgs antes de fn. Cada
// variante monta o proprio closure: o caminho por chamada de fmt/to_bytes/
// hex*/json_* (defineValueNative) nao ganha um salto a mais por causa desta.
func (vm *VM) defineValueNativeErr(name string, fn func(args []value.Value) (value.Value, error)) {
	vm.registerValueNative(name, func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if err := rejectRefArgs(name, args); err != nil {
			return value.NewNull(), err
		}
		return fn(args)
	})
}

func (vm *VM) registerValueNative(name string, fn value.ContextualNativeFunc) {
	native := value.NewContextualNative(name, fn)
	if obj, ok := native.Obj.(*value.ObjNative); ok {
		obj.Name = name
	}
	vm.shared.Root.DefineLocalIfAbsent(name, stampReadonlyArgs(native))
}
